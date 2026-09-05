package app

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// CreateTaskInput 是创建任务的输入。
type CreateTaskInput struct {
	GoalID               string            `json:"goal_id"`
	ParentID             string            `json:"parent_id"`
	TypeName             string            `json:"type_name"`
	Title                string            `json:"title"`
	Description          string            `json:"description"`
	AssigneeID           string            `json:"assignee_id"`
	ReviewerID           string            `json:"reviewer_id"`
	Participants         map[string]string `json:"participants"`
	RequiredCapabilities []string          `json:"required_capabilities"`
	HumanOnly            bool              `json:"human_only"`
	Priority             *int              `json:"priority"`
	EstimateHours        *float64          `json:"estimate_hours"`
	PlannedStart         *time.Time        `json:"planned_start"`
	PlannedEnd           *time.Time        `json:"planned_end"`
	Fields               map[string]any    `json:"fields"`
	Points               *int              `json:"points"`
	SprintID             string            `json:"sprint_id"`
	Ready                bool              `json:"ready"` // 创建后直接就绪（跳过草稿）
}

// CreateTask 创建任务。
func (a *App) CreateTask(ctx context.Context, sess *Session, in CreateTaskInput) (*domain.Task, error) {
	if in.Title == "" {
		return nil, Bad("err.title_required")
	}
	if in.TypeName == "" {
		in.TypeName = "generic"
	}
	if in.Points != nil && *in.Points < 0 {
		return nil, Bad("err.points_invalid")
	}
	if sess.IsAgent() {
		g := domain.GrantCreateTask
		action := ActionTaskCreate
		if in.ParentID != "" {
			g, action = domain.GrantCreateSubtask, ActionTaskCreateSubtask
		}
		if !sess.Actor.HasGrant(g) {
			return nil, Forbidden("err.agent_no_grant", i18n.Key("grant."+string(g)))
		}
		if domain.NeedsApproval(sess.Actor, g) {
			return nil, a.proposeOrFail(ctx, sess, func(tx pgx.Tx) (proposalDraft, error) {
				d := proposalDraft{Action: action, Grant: g, Payload: in,
					Summary: i18n.M("proposal.summary.task.create", in.Title)}
				if in.ParentID != "" {
					parent, err := a.Store.TaskByID(ctx, tx, in.ParentID)
					if err != nil {
						return d, Bad("err.parent_missing")
					}
					d.TargetKind, d.TargetID, d.TargetTitle = "task", parent.ID, parent.Title
					d.Summary = i18n.M("proposal.summary.task.create_subtask", parent.Title, in.Title)
				}
				return d, nil
			})
		}
	}
	var task *domain.Task
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		tt, err := a.Store.CurrentTaskType(ctx, tx, in.TypeName)
		if err != nil {
			return Bad("err.type_missing", in.TypeName)
		}
		if in.ParentID != "" {
			parent, err := a.Store.TaskByID(ctx, tx, in.ParentID)
			if err != nil {
				return Bad("err.parent_missing")
			}
			if in.GoalID == "" {
				in.GoalID = parent.GoalID
			}
		}
		if in.GoalID != "" {
			if _, err := a.Store.GoalByID(ctx, tx, in.GoalID); err != nil {
				return Bad("err.goal_missing")
			}
		}
		reviewer := in.ReviewerID
		if reviewer == "" {
			reviewer = sess.Actor.ID
			if sess.IsAgent() {
				reviewer = sess.MemberID
			}
		}
		priority := 2
		if in.Priority != nil {
			priority = *in.Priority
		}
		task = &domain.Task{
			OrgID: sess.OrgID, GoalID: in.GoalID, ParentID: in.ParentID, TypeName: tt.Name, TypeVersion: tt.Workflow.Version,
			Title: in.Title, Description: in.Description, CreatorID: sess.Actor.ID, ReviewerID: reviewer, AssigneeID: in.AssigneeID,
			RequiredCapabilities: in.RequiredCapabilities, HumanOnly: in.HumanOnly, State: tt.Workflow.Initial,
			Participants: map[string]string{}, Fields: in.Fields, Priority: priority, EstimateHours: in.EstimateHours,
			PlannedStart: in.PlannedStart, PlannedEnd: in.PlannedEnd, Points: in.Points,
		}
		var sprint *domain.Sprint
		if in.SprintID != "" {
			sprint, err = a.Store.SprintByID(ctx, tx, in.SprintID)
			if err != nil {
				return NotFound("err.sprint_missing")
			}
			if sprint.Status == domain.SprintClosed {
				return Bad("err.sprint_closed_add")
			}
			task.SprintID = sprint.ID
		}
		for k, v := range in.Participants {
			if tt.Participant(k) == nil {
				return Bad("err.no_participant", tt.Title, k)
			}
			task.Participants[k] = v
		}
		if err := a.Store.InsertTask(ctx, tx, task); err != nil {
			return err
		}
		events := []domain.Event{{Type: "TaskCreated", TaskID: task.ID, ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"title": task.Title, "type": tt.Title}}}
		if task.AssigneeID != "" {
			events = append(events, domain.Event{Type: "TaskAssigned", TaskID: task.ID, ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"to": task.AssigneeID}})
		}
		if sprint != nil {
			ev := sprintEvent(sess, "TaskAddedToSprint", sprint, map[string]any{"points": task.Points, "done": false})
			ev.TaskID = task.ID
			events = append(events, ev)
		}
		if err := a.insertEvents(ctx, tx, sess, events); err != nil {
			return err
		}
		if err := a.notify(ctx, tx, sess, tt, task, events); err != nil {
			return err
		}
		if in.Ready {
			// 内置类型的第一步由创建者触发；自定义类型没有这一步就保持初始状态
			c, err := a.loadContext(ctx, tx, sess, task.ID)
			if err != nil {
				return err
			}
			for _, av := range domain.Available(c, sess.Actor, domain.Payload{}) {
				if av.Available && c.Type.Workflow.State(av.Transition.To) != nil && c.Type.Workflow.State(av.Transition.To).Label == domain.LabelPending {
					o, err := domain.Apply(c, sess.Actor, av.Transition.Name, domain.Payload{})
					if err != nil {
						return err
					}
					if err := a.persist(ctx, tx, sess, c, o); err != nil {
						return err
					}
					task = o.Task
					break
				}
			}
		}
		return nil
	})
	return task, err
}

// UpdateTaskInput 是可编辑字段。
type UpdateTaskInput struct {
	Title         *string           `json:"title"`
	Description   *string           `json:"description"`
	ReviewerID    *string           `json:"reviewer_id"`
	Priority      *int              `json:"priority"`
	EstimateHours *float64          `json:"estimate_hours"`
	PlannedStart  *time.Time        `json:"planned_start"`
	PlannedEnd    *time.Time        `json:"planned_end"`
	HumanOnly     *bool             `json:"human_only"`
	GoalID        *string           `json:"goal_id"`
	Participants  map[string]string `json:"participants"`
	Fields        map[string]any    `json:"fields"`
	Points        *int              `json:"points"`    // 与 ClearPoints 配合：SetPoints 为真时生效
	SetPoints     bool              `json:"-"`         // 请求里带了 points（可能是 null）
	SprintID      *string           `json:"sprint_id"` // 空字符串表示移出迭代
}

// UpdateTask 修改任务的描述性字段。时间变更记录动态。
func (a *App) UpdateTask(ctx context.Context, sess *Session, id string, in UpdateTaskInput) (*domain.Task, error) {
	var task *domain.Task
	var prop *ProposalView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		t, err := a.Store.TaskByID(ctx, tx, id)
		if err != nil {
			return err
		}
		// Agent 改任务字段同样受「所有者权限 ∩ 授权」约束（ADR 0003）：只能改自己负责或自己创建的任务，
		// 需要「执行任务」授权；验收人、优先级、归属目标、迭代、参与角色、仅限人工是人的决定，Agent 一律不能改。
		if sess.IsAgent() {
			if in.ReviewerID != nil || in.Priority != nil || in.GoalID != nil || in.SprintID != nil || in.Participants != nil || in.HumanOnly != nil {
				return Forbidden("err.agent_task_field")
			}
			if t.AssigneeID != sess.Actor.ID && t.CreatorID != sess.Actor.ID {
				return Forbidden("err.agent_task_not_mine")
			}
			if !sess.Actor.HasGrant(domain.GrantExecute) {
				return Forbidden("err.agent_no_grant", i18n.Key("grant.execute"))
			}
			if domain.NeedsApproval(sess.Actor, domain.GrantExecute) {
				v, err := a.createProposal(ctx, tx, sess, proposalDraft{Action: ActionTaskUpdate, Grant: domain.GrantExecute,
					TargetKind: "task", TargetID: t.ID, TargetTitle: t.Title, Payload: in,
					Summary: i18n.M("proposal.summary.task.update", t.Title)})
				if err != nil {
					return err
				}
				prop = v
				return nil
			}
		}
		changes := map[string]any{}
		if in.Title != nil {
			t.Title = *in.Title
		}
		if in.Description != nil {
			t.Description = *in.Description
		}
		if in.ReviewerID != nil {
			t.ReviewerID = *in.ReviewerID
		}
		if in.Priority != nil {
			t.Priority = *in.Priority
		}
		if in.EstimateHours != nil {
			t.EstimateHours = in.EstimateHours
			changes["estimate_hours"] = *in.EstimateHours
		}
		if in.PlannedStart != nil {
			t.PlannedStart = in.PlannedStart
			changes["planned_start"] = in.PlannedStart.Format("2006-01-02")
		}
		if in.PlannedEnd != nil {
			t.PlannedEnd = in.PlannedEnd
			changes["planned_end"] = in.PlannedEnd.Format("2006-01-02")
		}
		if in.HumanOnly != nil {
			t.HumanOnly = *in.HumanOnly
		}
		if in.GoalID != nil {
			t.GoalID = *in.GoalID
		}
		var extra []domain.Event
		if in.SetPoints {
			if in.Points != nil && *in.Points < 0 {
				return Bad("err.points_invalid")
			}
			old, nw := t.Points, in.Points
			if (old == nil) != (nw == nil) || (old != nil && nw != nil && *old != *nw) {
				t.Points = nw
				ev := domain.Event{Type: "PointsChanged", TaskID: t.ID, ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"points": nw, "old": old}}
				extra = append(extra, ev)
			}
		}
		if in.SprintID != nil && *in.SprintID != t.SprintID {
			var sp *domain.Sprint
			if *in.SprintID != "" {
				sp, err = a.Store.SprintByID(ctx, tx, *in.SprintID)
				if err != nil {
					return NotFound("err.sprint_missing")
				}
				if sp.Status == domain.SprintClosed {
					return Bad("err.sprint_closed_add")
				}
			}
			tt, err := a.Store.TaskType(ctx, tx, t.TypeName, t.TypeVersion)
			if err != nil {
				return err
			}
			names, err := a.Store.SprintNames(ctx, tx)
			if err != nil {
				return err
			}
			evs, err := a.moveTaskToSprint(ctx, tx, sess, t, tt, sp, names)
			if err != nil {
				return err
			}
			extra = append(extra, evs...)
		}
		for k, v := range in.Participants {
			if v == "" {
				delete(t.Participants, k)
			} else {
				t.Participants[k] = v
			}
		}
		for k, v := range in.Fields {
			if t.Fields == nil {
				t.Fields = map[string]any{}
			}
			t.Fields[k] = v
		}
		if err := a.Store.UpdateTask(ctx, tx, t); err != nil {
			return err
		}
		if len(changes) > 0 {
			extra = append(extra, domain.Event{Type: "TaskUpdated", TaskID: t.ID, ActorID: sess.Actor.ID, At: time.Now(), Data: changes})
		}
		if err := a.insertEvents(ctx, tx, sess, extra); err != nil {
			return err
		}
		task = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	if prop != nil {
		return nil, pending(sess, prop)
	}
	return task, nil
}

// GetTask 读取任务。
func (a *App) GetTask(ctx context.Context, sess *Session, id string) (*domain.Task, error) {
	var task *domain.Task
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		task, err = a.Store.TaskByID(ctx, tx, id)
		return
	})
	return task, err
}

// ListTasks 列表。
func (a *App) ListTasks(ctx context.Context, sess *Session, f store.TaskFilter) ([]*domain.Task, error) {
	var out []*domain.Task
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		out, err = a.Store.ListTasks(ctx, tx, f)
		return
	})
	return out, err
}

// TransitionPayload 是触发一步的输入。
type TransitionPayload struct {
	Comment string         `json:"comment"`
	Result  map[string]any `json:"result"`
}

// Transition 触发流程里的一步。Agent 的这项授权是「需要人确认」时改记一条待确认操作（ADR 0003）。
func (a *App) Transition(ctx context.Context, sess *Session, taskID, name string, p TransitionPayload) (*domain.Task, error) {
	draft := func(tx pgx.Tx, c *domain.Context, g domain.Grant) (proposalDraft, error) {
		from, to := transitionTitles(c, name)
		return proposalDraft{Action: ActionTaskTransition, Grant: g, TargetKind: "task", TargetID: c.Task.ID, TargetTitle: c.Task.Title,
			Payload: map[string]any{"task_id": c.Task.ID, "transition": name, "comment": p.Comment, "result": p.Result},
			Summary: i18n.M("proposal.summary.task.transition", c.Task.Title, from, to)}, nil
	}
	return a.taskCommand(ctx, sess, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
		return domain.Apply(c, sess.Actor, name, domain.Payload{Comment: p.Comment, Result: p.Result})
	})
}

// Claim 领取任务。
func (a *App) Claim(ctx context.Context, sess *Session, taskID string) (*domain.Task, error) {
	draft := func(tx pgx.Tx, c *domain.Context, g domain.Grant) (proposalDraft, error) {
		return proposalDraft{Action: ActionTaskClaim, Grant: g, TargetKind: "task", TargetID: c.Task.ID, TargetTitle: c.Task.Title,
			Payload: map[string]any{"task_id": c.Task.ID},
			Summary: i18n.M("proposal.summary.task.claim", sess.Actor.Name, c.Task.Title)}, nil
	}
	return a.taskCommand(ctx, sess, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
		return domain.Claim(c, sess.Actor)
	})
}

// Begin 开始执行。
func (a *App) Begin(ctx context.Context, sess *Session, taskID string) (*domain.Task, error) {
	draft := func(tx pgx.Tx, c *domain.Context, g domain.Grant) (proposalDraft, error) {
		return proposalDraft{Action: ActionTaskBegin, Grant: g, TargetKind: "task", TargetID: c.Task.ID, TargetTitle: c.Task.Title,
			Payload: map[string]any{"task_id": c.Task.ID},
			Summary: i18n.M("proposal.summary.task.begin", sess.Actor.Name, c.Task.Title)}, nil
	}
	return a.taskCommand(ctx, sess, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
		return domain.Begin(c, sess.Actor)
	})
}

// Assign 指派负责人。
func (a *App) Assign(ctx context.Context, sess *Session, taskID, executorID string) (*domain.Task, error) {
	draft := func(tx pgx.Tx, c *domain.Context, g domain.Grant) (proposalDraft, error) {
		d := proposalDraft{Action: ActionTaskAssign, Grant: g, TargetKind: "task", TargetID: c.Task.ID, TargetTitle: c.Task.Title,
			Payload: map[string]any{"task_id": c.Task.ID, "executor_id": executorID},
			Summary: i18n.M("proposal.summary.task.assign_clear", c.Task.Title)}
		if executorID != "" {
			d.Summary = i18n.M("proposal.summary.task.assign", c.Task.Title, a.executorName(ctx, tx, executorID))
		}
		return d, nil
	}
	return a.taskCommand(ctx, sess, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
		return domain.Assign(c, sess.Actor, executorID)
	})
}

// AddArtifact 附上交付物。交付物是执行的一部分，按「执行任务」授权判断要不要人确认。
func (a *App) AddArtifact(ctx context.Context, sess *Session, taskID string, art domain.Artifact) (*domain.Task, error) {
	if art.Type == "" {
		return nil, Bad("err.artifact_type")
	}
	title := art.Title
	if title == "" {
		title = domain.ArtifactTitle(art.Type).In(sess.Loc())
	}
	draft := func(tx pgx.Tx, c *domain.Context, g domain.Grant) (proposalDraft, error) {
		return proposalDraft{Action: ActionTaskArtifact, Grant: g, TargetKind: "task", TargetID: c.Task.ID, TargetTitle: c.Task.Title,
			Payload: art,
			Summary: i18n.M("proposal.summary.task.artifact", c.Task.Title, title)}, nil
	}
	return a.taskCommand(ctx, sess, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
		if domain.NeedsApproval(sess.Actor, domain.GrantExecute) {
			return nil, &domain.ErrNeedsApproval{Grant: domain.GrantExecute}
		}
		return domain.AddArtifact(c, sess.Actor, art), nil
	})
}

// AddComment 写评论或工作日志。
func (a *App) AddComment(ctx context.Context, sess *Session, taskID, text string, isNote bool) (*domain.Task, error) {
	if text == "" {
		return nil, Bad("err.empty_text")
	}
	draft := func(tx pgx.Tx, c *domain.Context, g domain.Grant) (proposalDraft, error) {
		d := proposalDraft{Action: ActionTaskComment, Grant: g, TargetKind: "task", TargetID: c.Task.ID, TargetTitle: c.Task.Title,
			Payload: map[string]any{"task_id": c.Task.ID, "text": text, "is_note": isNote},
			Summary: i18n.M("proposal.summary.task.comment", sess.Actor.Name, c.Task.Title, text)}
		if isNote {
			d.Action = ActionTaskNote
			d.Summary = i18n.M("proposal.summary.task.note", sess.Actor.Name, c.Task.Title)
		}
		return d, nil
	}
	return a.taskCommand(ctx, sess, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
		return domain.AddComment(c, sess.Actor, text, isNote)
	})
}

// Link 建立关联。
func (a *App) Link(ctx context.Context, sess *Session, taskID string, typ domain.RelationType, otherID string) (*domain.Task, error) {
	var task *domain.Task
	var v *ProposalView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.loadContext(ctx, tx, sess, taskID)
		if err != nil {
			return err
		}
		other, err := a.Store.TaskByID(ctx, tx, otherID)
		if err != nil {
			return Bad("err.other_task_missing")
		}
		blocksOf := func(id string) []string {
			ids, _ := a.Store.BlocksOf(ctx, tx, id)
			return ids
		}
		o, err := domain.Link(c, sess.Actor, typ, otherID, blocksOf)
		if err != nil {
			var na *domain.ErrNeedsApproval
			if errors.As(err, &na) {
				v, err = a.createProposal(ctx, tx, sess, proposalDraft{
					Action: ActionTaskLink, Grant: na.Grant, TargetKind: "task", TargetID: c.Task.ID, TargetTitle: c.Task.Title,
					Payload: map[string]any{"task_id": c.Task.ID, "type": string(typ), "other_id": otherID},
					Summary: i18n.M("proposal.summary.task.link", c.Task.Title, i18n.Key("rel."+string(typ)), other.Title)})
				return err
			}
			return err
		}
		if err := a.persist(ctx, tx, sess, c, o); err != nil {
			return err
		}
		task = o.Task
		return nil
	})
	if err != nil {
		return nil, err
	}
	if v != nil {
		return nil, pending(sess, v)
	}
	return task, nil
}

// Heartbeat 是 Agent 的心跳，顺带上报累计用量。
func (a *App) Heartbeat(ctx context.Context, sess *Session, taskID string, usage []domain.Usage) (*domain.Task, error) {
	if sess.IsAgent() {
		_ = a.Store.WithOrg(ctx, sess.OrgID, func(tx pgx.Tx) error { return a.Store.TouchAgent(ctx, tx, sess.AgentID) })
	}
	if taskID == "" {
		return nil, nil
	}
	return a.simple(ctx, sess, taskID, func(c *domain.Context) (*domain.Outcome, error) {
		if len(usage) == 0 {
			if c.ActiveRun == nil {
				return nil, &domain.Rejection{Reasons: []domain.Reason{i18n.M("reject.no_run")}}
			}
			c.ActiveRun.LastBeat = c.Now
			return &domain.Outcome{Task: c.Task}, nil
		}
		return domain.ReportUsage(c, sess.Actor, usage)
	})
}

func (a *App) simple(ctx context.Context, sess *Session, taskID string, fn func(c *domain.Context) (*domain.Outcome, error)) (*domain.Task, error) {
	var task *domain.Task
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.loadContext(ctx, tx, sess, taskID)
		if err != nil {
			return err
		}
		o, err := fn(c)
		if err != nil {
			return err
		}
		if err := a.persist(ctx, tx, sess, c, o); err != nil {
			return err
		}
		task = o.Task
		return nil
	})
	return task, err
}

// WorkflowView 是 get_workflow 的返回（docs/spec §10）。
type WorkflowView struct {
	TaskID      string           `json:"task_id"`
	State       StateView        `json:"state"`
	ActiveRun   *domain.Run      `json:"active_run"`
	Transitions []TransitionView `json:"transitions"`
	CanBegin    bool             `json:"can_begin"`
	BeginWhyNot []string         `json:"begin_reasons,omitempty"`
	CanClaim    bool             `json:"can_claim"`
	ClaimWhyNot []string         `json:"claim_reasons,omitempty"`
	Progress    int              `json:"progress"`
}

// StateView 是状态的展示形式（已按请求者语言渲染）。
type StateView struct {
	Name       string       `json:"name"`
	Title      string       `json:"title"`
	Label      domain.Label `json:"label"`
	LabelTitle string       `json:"label_title"`
}

// TransitionView 是一步的可用性（已渲染）。
type TransitionView struct {
	Name      string   `json:"name"`
	Title     string   `json:"title"`
	To        string   `json:"to"`
	Requires  []string `json:"requires"`
	Available bool     `json:"available"`
	Reasons   []string `json:"reasons"`
	// NeedsApproval 为真时这一步可以走，但会先记成一条待确认操作，等人确认（ADR 0003）。
	NeedsApproval bool `json:"needs_approval,omitempty"`
}

// Workflow 返回触发者视角的流程可用性。
func (a *App) Workflow(ctx context.Context, sess *Session, taskID string) (*WorkflowView, error) {
	var v *WorkflowView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.loadContext(ctx, tx, sess, taskID)
		if err != nil {
			return err
		}
		v = a.workflowView(c, sess)
		return nil
	})
	return v, err
}

func (a *App) workflowView(c *domain.Context, sess *Session) *WorkflowView {
	loc, actor := sess.Loc(), sess.Actor
	st := c.Type.Workflow.State(c.Task.State)
	v := &WorkflowView{TaskID: c.Task.ID, ActiveRun: c.ActiveRun, Progress: domain.Progress(c.Task, c.Type), Transitions: []TransitionView{}}
	if st != nil {
		v.State = StateView{Name: st.Name, Title: st.Title.In(loc), Label: st.Label, LabelTitle: domain.LabelTitle(st.Label, loc)}
	}
	// comment/result 条件由调用时提供，这里不作为不可用原因
	for _, av := range domain.Available(c, actor, domain.Payload{Comment: "x", Result: map[string]any{}}) {
		to := av.Transition.To
		if to == "$previous" {
			to = c.Task.PreviousState
		}
		requires := []string{}
		for _, r := range av.Transition.Requires {
			if r == "comment" || r == "result" {
				requires = append(requires, r)
			}
		}
		v.Transitions = append(v.Transitions, TransitionView{Name: av.Transition.Name, Title: av.Transition.Title.In(loc), To: to, Requires: requires, Available: av.Available, NeedsApproval: av.NeedsApproval, Reasons: domain.RenderReasons(loc, av.Reasons)})
	}
	v.BeginWhyNot = domain.RenderReasons(loc, domain.CanBegin(c, actor))
	v.CanBegin = len(v.BeginWhyNot) == 0
	v.ClaimWhyNot = domain.RenderReasons(loc, domain.CanClaim(c, actor))
	v.CanClaim = len(v.ClaimWhyNot) == 0
	return v
}
