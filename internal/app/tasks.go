package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
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

// commentOp 是评论 / 工作日志在幂等键里记的操作名（两者是两个工具，不该互相顶替）。
func commentOp(isNote bool) string {
	if isNote {
		return "add_note"
	}
	return "add_comment"
}

// CreateTask 创建任务。
func (a *App) CreateTask(ctx context.Context, sess *Session, in CreateTaskInput) (*domain.Task, error) {
	return idempotent(ctx, a, sess, createTaskOp(in), in, func() (*domain.Task, error) {
		return a.createTask(ctx, sess, in)
	})
}

// createTaskOp 区分创建任务与创建子任务：同一个幂等键换了其中一种，参数指纹本来就不同，
// 名字分开只是让"这个键上次用在哪个工具上"看得明白。
func createTaskOp(in CreateTaskInput) string {
	if in.ParentID != "" {
		return "create_subtask"
	}
	return "create_task"
}

func (a *App) createTask(ctx context.Context, sess *Session, in CreateTaskInput) (*domain.Task, error) {
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
		// 只看不做（ADR 0025）：类型、上级、目标、迭代、参与角色都已经校验过了，就在写库之前停住。
		if sess.Write.DryRun {
			clauses := []i18n.Msg{i18n.M("will.task.create_new", tt.Title, task.Title)}
			if task.AssigneeID != "" {
				clauses = append(clauses, i18n.M("will.task.assign_to", a.executorName(ctx, tx, task.AssigneeID)))
			}
			if sprint != nil {
				clauses = append(clauses, i18n.M("will.task.into_sprint", sprint.Name))
			}
			if in.Ready {
				clauses = append(clauses, i18n.M("will.task.ready"))
			}
			return dryRun(sess, clauses...)
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

// UpdateTaskInput 是可编辑字段。指针为 nil 表示不改；要清空预估工时或计划起止时把字段名写进 Clear。
// ParentID / SprintID 指向空字符串分别表示提为顶级任务 / 移出迭代。
type UpdateTaskInput struct {
	Title                *string           `json:"title"`
	Description          *string           `json:"description"`
	ReviewerID           *string           `json:"reviewer_id"`
	Priority             *int              `json:"priority"`
	EstimateHours        *float64          `json:"estimate_hours"`
	PlannedStart         *time.Time        `json:"planned_start"`
	PlannedEnd           *time.Time        `json:"planned_end"`
	HumanOnly            *bool             `json:"human_only"`
	GoalID               *string           `json:"goal_id"`
	ParentID             *string           `json:"parent_id"`
	RequiredCapabilities *[]string         `json:"required_capabilities"`
	Participants         map[string]string `json:"participants"`
	Fields               map[string]any    `json:"fields"`
	Points               *int              `json:"points"`          // 与 SetPoints 配合：SetPoints 为真时生效
	SetPoints            bool              `json:"set_points"`      // 请求里带了 points（可能是 null）
	SprintID             *string           `json:"sprint_id"`       // 空字符串表示移出迭代
	Clear                []string          `json:"clear,omitempty"` // estimate_hours | planned_start | planned_end
}

// agentForbidden 判断 Agent 是否碰了只能由人决定的字段（ADR 0003）。
func (in UpdateTaskInput) agentForbidden() bool {
	return in.ReviewerID != nil || in.Priority != nil || in.GoalID != nil || in.ParentID != nil || in.SprintID != nil ||
		in.Participants != nil || in.HumanOnly != nil || in.RequiredCapabilities != nil
}

// UpdateTask 就地修改任务。每个真正变了的字段各记一条 TaskFieldChanged 动态（带旧值与新值）；
// 工作量与迭代沿用各自的动态（PointsChanged / TaskAddedToSprint / TaskRemovedFromSprint），不重复记。
// 已结束的任务只能改描述与自定义字段。人改任务要与任务有关（负责人、创建者、验收人、参与人、所属目标的负责人）或是组织负责人；
// Agent 受「所有者权限 ∩ 授权」约束，验收人、优先级、归属目标、上级、迭代、参与角色、所需能力、仅限人工一律不能改。
func (a *App) UpdateTask(ctx context.Context, sess *Session, id string, in UpdateTaskInput) (*domain.Task, error) {
	return idempotent(ctx, a, sess, "update_task", map[string]any{"task_id": id, "input": in}, func() (*domain.Task, error) {
		return a.updateTask(ctx, sess, id, in)
	})
}

func (a *App) updateTask(ctx context.Context, sess *Session, id string, in UpdateTaskInput) (*domain.Task, error) {
	var task *domain.Task
	var prop *ProposalView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		t, err := a.Store.TaskByID(ctx, tx, id)
		if err != nil {
			return err
		}
		ix, err := a.OrgIndex(ctx, tx)
		if err != nil {
			return err
		}
		if sess.IsAgent() {
			if in.agentForbidden() {
				return Forbidden("err.agent_task_field")
			}
			if t.AssigneeID != sess.Actor.ID && t.CreatorID != sess.Actor.ID {
				return Forbidden("err.agent_task_not_mine")
			}
			if !sess.Actor.HasGrant(domain.GrantExecute) {
				return Forbidden("err.agent_no_grant", i18n.Key("grant.execute"))
			}
			if domain.NeedsApproval(sess.Actor, domain.GrantExecute) {
				if sess.Write.DryRun {
					return dryRunPending(sess, a.executorName(ctx, tx, sess.MemberID), i18n.M("proposal.summary.task.update", t.Title))
				}
				v, err := a.createProposal(ctx, tx, sess, proposalDraft{Action: ActionTaskUpdate, Grant: domain.GrantExecute,
					TargetKind: "task", TargetID: t.ID, TargetTitle: t.Title, Payload: in,
					Summary: i18n.M("proposal.summary.task.update", t.Title)})
				if err != nil {
					return err
				}
				prop = v
				return nil
			}
		} else if !a.canEditTask(ctx, tx, sess, ix, t) {
			return Forbidden("err.task_edit_forbidden")
		}
		tt, err := a.Store.TaskType(ctx, tx, t.TypeName, t.TypeVersion)
		if err != nil {
			return err
		}
		closed := false
		if st := tt.Workflow.State(t.State); st != nil {
			closed = st.Label.IsTerminal()
		}
		var names map[string]string // 执行者名字，按需装载
		nameOf := func(id string) string {
			if id == "" {
				return ""
			}
			if names == nil {
				names, _ = a.Store.ExecutorNames(ctx, tx)
			}
			if n := names[id]; n != "" {
				return n
			}
			return id
		}

		var changes []fieldChange // 除描述与自定义字段之外的改动（已结束的任务不允许）
		var soft []fieldChange    // 描述与自定义字段
		if in.Title != nil && *in.Title != t.Title {
			if *in.Title == "" {
				return Bad("err.title_required")
			}
			changes = append(changes, fieldChange{Field: "title", From: t.Title, To: *in.Title})
			t.Title = *in.Title
		}
		if in.Description != nil && *in.Description != t.Description {
			soft = append(soft, fieldChange{Field: "description", From: t.Description, To: *in.Description})
			t.Description = *in.Description
		}
		if in.ReviewerID != nil && *in.ReviewerID != t.ReviewerID {
			if *in.ReviewerID == "" || nameOf(*in.ReviewerID) == *in.ReviewerID {
				return Bad("err.member_missing")
			}
			changes = append(changes, fieldChange{Field: "reviewer_id", From: t.ReviewerID, To: *in.ReviewerID, FromTitle: nameOf(t.ReviewerID), ToTitle: nameOf(*in.ReviewerID)})
			t.ReviewerID = *in.ReviewerID
		}
		if in.Priority != nil && *in.Priority != t.Priority {
			changes = append(changes, fieldChange{Field: "priority", From: t.Priority, To: *in.Priority})
			t.Priority = *in.Priority
		}
		switch {
		case contains(in.Clear, "estimate_hours"):
			if t.EstimateHours != nil {
				changes = append(changes, fieldChange{Field: "estimate_hours", From: floatVal(t.EstimateHours), To: nil})
				t.EstimateHours = nil
			}
		case in.EstimateHours != nil && !sameFloat(in.EstimateHours, t.EstimateHours):
			changes = append(changes, fieldChange{Field: "estimate_hours", From: floatVal(t.EstimateHours), To: *in.EstimateHours})
			t.EstimateHours = in.EstimateHours
		}
		dates := []struct {
			field string
			cur   **time.Time
			in    *time.Time
		}{{"planned_start", &t.PlannedStart, in.PlannedStart}, {"planned_end", &t.PlannedEnd, in.PlannedEnd}}
		for _, d := range dates {
			switch {
			case contains(in.Clear, d.field):
				if *d.cur != nil {
					changes = append(changes, fieldChange{Field: d.field, From: dateVal(*d.cur), To: nil})
					*d.cur = nil
				}
			case d.in != nil && !sameTime(d.in, *d.cur):
				changes = append(changes, fieldChange{Field: d.field, From: dateVal(*d.cur), To: dateVal(d.in)})
				*d.cur = d.in
			}
		}
		if in.HumanOnly != nil && *in.HumanOnly != t.HumanOnly {
			changes = append(changes, fieldChange{Field: "human_only", From: t.HumanOnly, To: *in.HumanOnly})
			t.HumanOnly = *in.HumanOnly
		}
		goalTitle := func(id string) string {
			for _, g := range ix.Goals {
				if g.ID == id {
					return g.Title
				}
			}
			return ""
		}
		if in.GoalID != nil && *in.GoalID != t.GoalID {
			if *in.GoalID != "" && goalTitle(*in.GoalID) == "" {
				if _, err := a.Store.GoalByID(ctx, tx, *in.GoalID); err != nil {
					return Bad("err.goal_missing")
				}
			}
			changes = append(changes, fieldChange{Field: "goal_id", From: nilIfEmpty(t.GoalID), To: nilIfEmpty(*in.GoalID), FromTitle: goalTitle(t.GoalID), ToTitle: goalTitle(*in.GoalID)})
			t.GoalID = *in.GoalID
		}
		if in.ParentID != nil && *in.ParentID != t.ParentID {
			all, err := a.Store.AllTasks(ctx, tx)
			if err != nil {
				return err
			}
			titles := map[string]string{}
			for _, x := range all {
				titles[x.ID] = x.Title
			}
			if *in.ParentID != "" {
				var parent *domain.Task
				for _, x := range all {
					if x.ID == *in.ParentID {
						parent = x
					}
				}
				if parent == nil {
					return Bad("err.parent_missing")
				}
				if team := ix.TeamOfTask(parent); team != "" && !sess.CanSeeCollabTeam(team) {
					return Forbidden("err.parent_task_hidden")
				}
				if parent.ID == t.ID || taskDescendants(all, t.ID)[parent.ID] {
					return Bad("err.task_parent_cycle")
				}
				ptt, err := a.Store.TaskType(ctx, tx, parent.TypeName, parent.TypeVersion)
				if err != nil {
					return err
				}
				if st := ptt.Workflow.State(parent.State); st != nil && st.Label.IsTerminal() {
					return Bad("err.task_parent_closed")
				}
			}
			changes = append(changes, fieldChange{Field: "parent_id", From: nilIfEmpty(t.ParentID), To: nilIfEmpty(*in.ParentID), FromTitle: titles[t.ParentID], ToTitle: titles[*in.ParentID]})
			t.ParentID = *in.ParentID
		}
		if in.RequiredCapabilities != nil {
			want := *in.RequiredCapabilities
			if want == nil {
				want = []string{}
			}
			if !sameStrings(want, t.RequiredCapabilities) {
				caps, err := a.Store.ListCapabilities(ctx, tx)
				if err != nil {
					return err
				}
				titlesOf := func(list []string) []i18n.Text {
					out := make([]i18n.Text, 0, len(list))
					for _, c := range list {
						if tt, ok := caps[c]; ok && !tt.IsZero() {
							out = append(out, tt)
						} else {
							out = append(out, i18n.Text{i18n.Default: c})
						}
					}
					return out
				}
				for _, c := range want {
					if _, ok := caps[c]; !ok {
						return Bad("err.cap_unknown", c)
					}
				}
				changes = append(changes, fieldChange{Field: "required_capabilities", From: t.RequiredCapabilities, To: want, FromTitle: titlesOf(t.RequiredCapabilities), ToTitle: titlesOf(want)})
				t.RequiredCapabilities = want
			}
		}
		for k, v := range in.Participants {
			p := tt.Participant(k)
			if p == nil {
				return Bad("err.no_participant", tt.Title, k)
			}
			old := t.Participants[k]
			if old == v {
				continue
			}
			if v == "" {
				delete(t.Participants, k)
			} else {
				if nameOf(v) == v {
					return Bad("err.member_missing")
				}
				t.Participants[k] = v
			}
			changes = append(changes, fieldChange{Field: "participants", From: nilIfEmpty(old), To: nilIfEmpty(v), FromTitle: nameOf(old), ToTitle: nameOf(v), Label: p.Title, Extra: map[string]any{"slot": k}})
		}
		for k, v := range in.Fields {
			if t.Fields == nil {
				t.Fields = map[string]any{}
			}
			old, had := t.Fields[k]
			if had && reflect.DeepEqual(old, v) {
				continue
			}
			soft = append(soft, fieldChange{Field: "fields", From: old, To: v, Extra: map[string]any{"key": k}})
			t.Fields[k] = v
		}
		var extra []domain.Event
		if in.SetPoints {
			if in.Points != nil && *in.Points < 0 {
				return Bad("err.points_invalid")
			}
			old, nw := t.Points, in.Points
			if (old == nil) != (nw == nil) || (old != nil && nw != nil && *old != *nw) {
				if closed {
					return Bad("err.task_closed_edit")
				}
				t.Points = nw
				extra = append(extra, domain.Event{Type: "PointsChanged", TaskID: t.ID, ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"points": nw, "old": old}})
			}
		}
		if closed && len(changes) > 0 {
			return Bad("err.task_closed_edit")
		}
		if in.SprintID != nil && *in.SprintID != t.SprintID {
			if closed {
				return Bad("err.task_closed_edit")
			}
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
			sprintNames, err := a.Store.SprintNames(ctx, tx)
			if err != nil {
				return err
			}
			evs, err := a.moveTaskToSprint(ctx, tx, sess, t, tt, sp, sprintNames)
			if err != nil {
				return err
			}
			extra = append(extra, evs...)
		}
		if len(changes) == 0 && len(soft) == 0 && len(extra) == 0 {
			if sess.Write.DryRun {
				return dryRun(sess)
			}
			task = t
			return nil
		}
		// 只看不做：改动都算清楚了，就在写库之前停住。
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.task.update", taskRefText(t)))
		}
		if err := a.Store.UpdateTask(ctx, tx, t); err != nil {
			return err
		}
		base := map[string]any{"title": t.Title}
		if t.GoalID != "" {
			base["goal_id"] = t.GoalID
		}
		events := make([]domain.Event, 0, len(changes)+len(soft)+len(extra))
		for _, c := range append(changes, soft...) {
			events = append(events, c.event("TaskFieldChanged", sess, t.ID, base))
		}
		events = append(events, extra...)
		if err := a.insertEvents(ctx, tx, sess, events); err != nil {
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

// canEditTask 判断一个人能不能就地修改任务：任务的负责人、创建者、验收人、参与人（含他们名下 Agent 的所有者）、
// 所属目标的负责人链（同 canEditGoal），以及组织负责人。这不是新权限，只是把「与任务有关的人」说清楚。
func (a *App) canEditTask(ctx context.Context, tx pgx.Tx, sess *Session, ix *OrgIndex, t *domain.Task) bool {
	if sess.IsOwner {
		return true
	}
	mine := func(id string) bool {
		if id == "" {
			return false
		}
		return id == sess.MemberID || ix.ownerOfAgent[id] == sess.MemberID
	}
	if mine(t.AssigneeID) || mine(t.CreatorID) || mine(t.ReviewerID) {
		return true
	}
	for _, id := range t.Participants {
		if mine(id) {
			return true
		}
	}
	if t.GoalID != "" {
		for _, g := range ix.Goals {
			if g.ID == t.GoalID {
				return a.canEditGoal(ctx, tx, sess, g)
			}
		}
	}
	org, _ := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
	return org != nil && org.OwnerMemberID == sess.MemberID
}

// taskDescendants 返回某个任务的全部子孙（不含自己）。
func taskDescendants(tasks []*domain.Task, rootID string) map[string]bool {
	children := map[string][]string{}
	for _, t := range tasks {
		if t.ParentID != "" {
			children[t.ParentID] = append(children[t.ParentID], t.ID)
		}
	}
	out := map[string]bool{}
	stack := []string{rootID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range children[id] {
			if !out[c] {
				out[c] = true
				stack = append(stack, c)
			}
		}
	}
	return out
}

// parseTaskNumber 认「#123」「123」（可带空白）。
func parseTaskNumber(ref string) (int, bool) {
	s := strings.TrimSpace(ref)
	s = strings.TrimPrefix(s, "#")
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return 0, false
		}
	}
	return n, n > 0
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
	return a.taskCommand(ctx, sess, writeOp{"transition_task", map[string]any{"task_id": taskID, "name": name, "comment": p.Comment, "result": p.Result}}, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
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
	return a.taskCommand(ctx, sess, writeOp{"claim_task", map[string]any{"task_id": taskID}}, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
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
	return a.taskCommand(ctx, sess, writeOp{"begin_task", map[string]any{"task_id": taskID}}, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
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
	return a.taskCommand(ctx, sess, writeOp{"assign_task", map[string]any{"task_id": taskID, "executor_id": executorID}}, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
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
	return a.taskCommand(ctx, sess, writeOp{"attach_artifact", map[string]any{"task_id": taskID, "type": art.Type, "title": art.Title, "ref": art.Ref}}, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
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
	return a.taskCommand(ctx, sess, writeOp{commentOp(isNote), map[string]any{"task_id": taskID, "text": text, "is_note": isNote}}, taskID, draft, func(c *domain.Context) (*domain.Outcome, error) {
		return domain.AddComment(c, sess.Actor, text, isNote)
	})
}

// Link 建立关联。
func (a *App) Link(ctx context.Context, sess *Session, taskID string, typ domain.RelationType, otherID string) (*domain.Task, error) {
	return idempotent(ctx, a, sess, "link_tasks", map[string]any{"task_id": taskID, "type": string(typ), "other_id": otherID}, func() (*domain.Task, error) {
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
					summary := i18n.M("proposal.summary.task.link", c.Task.Title, i18n.Key("rel."+string(typ)), other.Title)
					if sess.Write.DryRun {
						return dryRunPending(sess, a.executorName(ctx, tx, sess.MemberID), summary)
					}
					v, err = a.createProposal(ctx, tx, sess, proposalDraft{
						Action: ActionTaskLink, Grant: na.Grant, TargetKind: "task", TargetID: c.Task.ID, TargetTitle: c.Task.Title,
						Payload: map[string]any{"task_id": c.Task.ID, "type": string(typ), "other_id": otherID},
						Summary: summary})
					return err
				}
				return err
			}
			if sess.Write.DryRun {
				return dryRun(sess, i18n.M("will.task.link_to", taskRefText(c.Task), taskRefText(other), i18n.Key("rel."+string(typ))))
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
	})
}

// Heartbeat 是 Agent 的心跳，顺带上报累计用量。
func (a *App) Heartbeat(ctx context.Context, sess *Session, taskID string, usage []domain.Usage) (*domain.Task, error) {
	// 只看不做连"最后活跃时间"都不碰（ADR 0025）：说好一行不落就是一行不落。
	if sess.IsAgent() && !sess.Write.DryRun {
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
