package app

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 迭代是时间盒容器，不是流程（ADR 0012）。这里的每个写操作都产生动态。

// Conflict 是 409：状态冲突（如同一团队已有进行中的迭代）。
func Conflict(key string, args ...any) error { return userErr(409, key, args...) }

// SprintView 是迭代 + 汇总。
type SprintView struct {
	*domain.Sprint
	TeamName string             `json:"team_name,omitempty"`
	Stats    domain.SprintStats `json:"stats"`
}

// SprintDetail 是迭代详情：任务、燃尽图、迭代速度。
type SprintDetail struct {
	SprintView
	Tasks    []TaskSummary   `json:"tasks"`
	Burndown domain.Burndown `json:"burndown"`
	Velocity VelocityView    `json:"velocity"`
}

// VelocityView 是迭代速度：最近几个已结束迭代的完成量与平均值。
type VelocityView struct {
	Sprints       []domain.VelocitySample `json:"sprints"`
	AveragePoints float64                 `json:"average_points"`
}

// CreateSprintInput 是创建迭代的输入。
type CreateSprintInput struct {
	Name     string
	Goal     string
	TeamID   string
	StartsOn *time.Time
	EndsOn   *time.Time
}

// UpdateSprintInput 是可修改字段。
type UpdateSprintInput struct {
	Name     *string
	Goal     *string
	StartsOn *time.Time
	EndsOn   *time.Time
}

func sprintErr(errs []error, loc i18n.Locale) error {
	if len(errs) == 0 {
		return nil
	}
	return &UserError{Status: 400, Reasons: reasonsOf(errs)}
}

func reasonsOf(errs []error) []i18n.Msg {
	out := make([]i18n.Msg, 0, len(errs))
	for _, e := range errs {
		if ve, ok := e.(*domain.ValidationError); ok {
			out = append(out, ve.Msg)
		} else {
			out = append(out, i18n.M("ev.generic", e.Error(), ""))
		}
	}
	return out
}

func sprintEvent(sess *Session, typ string, sp *domain.Sprint, extra map[string]any) domain.Event {
	data := map[string]any{"sprint_id": sp.ID, "name": sp.Name}
	for k, v := range extra {
		data[k] = v
	}
	return domain.Event{Type: typ, ActorID: sess.Actor.ID, At: time.Now(), Data: data}
}

// isDoneFn 返回"任务是否处于已完成类型状态"的判断（按各自的类型版本解释，内核只认状态类型）。
func isDoneFn(types map[string]*domain.TaskType) func(*domain.Task) bool {
	return func(t *domain.Task) bool {
		tt := types[t.TypeName]
		if tt == nil {
			return false
		}
		st := tt.Workflow.State(t.State)
		return st != nil && st.Label == domain.LabelTerminalSuccess
	}
}

func isTerminalFn(types map[string]*domain.TaskType) func(*domain.Task) bool {
	return func(t *domain.Task) bool {
		tt := types[t.TypeName]
		if tt == nil {
			return false
		}
		st := tt.Workflow.State(t.State)
		return st != nil && st.Label.IsTerminal()
	}
}

// CreateSprint 创建迭代（规划中）。
func (a *App) CreateSprint(ctx context.Context, sess *Session, in CreateSprintInput) (*domain.Sprint, error) {
	return idempotent(ctx, a, sess, "create_sprint", in, func() (*domain.Sprint, error) {
		return a.createSprint(ctx, sess, in)
	})
}

func (a *App) createSprint(ctx context.Context, sess *Session, in CreateSprintInput) (*domain.Sprint, error) {
	sp := &domain.Sprint{OrgID: sess.OrgID, TeamID: in.TeamID, Name: strings.TrimSpace(in.Name), Goal: in.Goal, Status: domain.SprintPlanning, CreatedBy: sess.Actor.ID}
	if in.StartsOn != nil {
		sp.StartsOn = *in.StartsOn
	}
	if in.EndsOn != nil {
		sp.EndsOn = *in.EndsOn
	}
	if err := sprintErr(domain.ValidateSprint(sp), sess.Loc()); err != nil {
		return nil, err
	}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		if sp.TeamID != "" {
			if _, err := teamByID(ctx, tx, a, sp.TeamID); err != nil {
				return Bad("err.team_missing")
			}
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.sprint.create", sp.Name))
		}
		if err := a.Store.InsertSprint(ctx, tx, sp); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{sprintEvent(sess, "SprintCreated", sp, nil)})
	})
	return sp, err
}

// UpdateSprint 修改名称、迭代目标、日期；已结束的不可改。
func (a *App) UpdateSprint(ctx context.Context, sess *Session, id string, in UpdateSprintInput) (*domain.Sprint, error) {
	var sp *domain.Sprint
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		var err error
		sp, err = a.Store.SprintByID(ctx, tx, id)
		if err != nil {
			return NotFound("err.sprint_missing")
		}
		if sp.Status == domain.SprintClosed {
			return Bad("err.sprint_closed")
		}
		if in.Name != nil {
			sp.Name = strings.TrimSpace(*in.Name)
		}
		if in.Goal != nil {
			sp.Goal = *in.Goal
		}
		if in.StartsOn != nil {
			sp.StartsOn = *in.StartsOn
		}
		if in.EndsOn != nil {
			sp.EndsOn = *in.EndsOn
		}
		if err := sprintErr(domain.ValidateSprint(sp), sess.Loc()); err != nil {
			return err
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.sprint.update", sp.Name))
		}
		if err := a.Store.UpdateSprint(ctx, tx, sp); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{sprintEvent(sess, "SprintUpdated", sp, nil)})
	})
	return sp, err
}

// requireSprintControl 判断能否开始 / 结束迭代：需要「管理流程」权限（Agent 还要有对应授权，
// 且它的「管理流程」授权只能是「需要人确认」，因此会转成待确认操作，见 StartSprint / CloseSprint）。
func (a *App) requireSprintControl(sess *Session) error {
	if !sess.Can("manage_workflows") {
		return Forbidden("err.sprint_forbidden")
	}
	if sess.IsAgent() && !sess.Actor.HasGrant(domain.GrantManageWorkflows) {
		return Forbidden("err.agent_no_grant", i18n.Key("grant.manage_workflows"))
	}
	return nil
}

// sprintNeedsApproval 判断 Agent 的这次迭代操作要不要先经人确认。
func (a *App) sprintNeedsApproval(sess *Session) bool {
	return sess.IsAgent() && domain.NeedsApproval(sess.Actor, domain.GrantManageWorkflows)
}

// StartSprint 开始迭代。同一团队（无团队时按组织）同时只能有一个进行中的迭代。
func (a *App) StartSprint(ctx context.Context, sess *Session, id string) (*domain.Sprint, error) {
	return idempotent(ctx, a, sess, "start_sprint", map[string]any{"sprint_id": id}, func() (*domain.Sprint, error) {
		return a.startSprint(ctx, sess, id)
	})
}

func (a *App) startSprint(ctx context.Context, sess *Session, id string) (*domain.Sprint, error) {
	if err := a.requireSprintControl(sess); err != nil {
		return nil, err
	}
	if a.sprintNeedsApproval(sess) {
		return nil, a.proposeOrFail(ctx, sess, func(tx pgx.Tx) (proposalDraft, error) {
			sp, err := a.Store.SprintByID(ctx, tx, id)
			if err != nil {
				return proposalDraft{}, NotFound("err.sprint_missing")
			}
			if sp.Status == domain.SprintClosed {
				return proposalDraft{}, Bad("err.sprint_already_closed")
			}
			if sp.Status != domain.SprintPlanning {
				return proposalDraft{}, Bad("err.sprint_not_planning")
			}
			return proposalDraft{Action: ActionSprintStart, Grant: domain.GrantManageWorkflows,
				TargetKind: "sprint", TargetID: sp.ID, TargetTitle: sp.Name,
				Payload: map[string]any{"sprint_id": sp.ID},
				Summary: i18n.M("proposal.summary.sprint.start", sp.Name)}, nil
		})
	}
	var sp *domain.Sprint
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		var err error
		sp, err = a.Store.SprintByID(ctx, tx, id)
		if err != nil {
			return NotFound("err.sprint_missing")
		}
		if sp.Status != domain.SprintPlanning {
			if sp.Status == domain.SprintClosed {
				return Bad("err.sprint_already_closed")
			}
			return Bad("err.sprint_not_planning")
		}
		active, err := a.Store.ActiveSprint(ctx, tx, sp.TeamID)
		if err != nil {
			return err
		}
		if active != nil {
			if sp.TeamID == "" {
				return Conflict("err.sprint_active_org", active.Name)
			}
			teamName := sp.TeamID
			if tm, err := teamByID(ctx, tx, a, sp.TeamID); err == nil {
				teamName = tm.Name
			}
			return Conflict("err.sprint_active_team", teamName, active.Name)
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.sprint.start", sp.Name))
		}
		now := time.Now()
		sp.Status, sp.StartedAt = domain.SprintActive, &now
		if err := a.Store.UpdateSprint(ctx, tx, sp); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{sprintEvent(sess, "SprintStarted", sp, nil)})
	})
	return sp, err
}

// CloseResult 是结束迭代的结果。
type CloseResult struct {
	Sprint   *domain.Sprint `json:"sprint"`
	Moved    int            `json:"moved"`    // 转入下一个迭代的任务数
	Returned int            `json:"returned"` // 退回待办的任务数
}

// CloseSprint 结束迭代。未完成（非终止）的任务按 unfinished 退回待办（backlog）或转入下一个迭代（next），逐个产生动态。
func (a *App) CloseSprint(ctx context.Context, sess *Session, id, unfinished, nextID string) (*CloseResult, error) {
	return idempotent(ctx, a, sess, "close_sprint", map[string]any{"sprint_id": id, "unfinished": unfinished, "next_sprint_id": nextID}, func() (*CloseResult, error) {
		return a.closeSprint(ctx, sess, id, unfinished, nextID)
	})
}

func (a *App) closeSprint(ctx context.Context, sess *Session, id, unfinished, nextID string) (*CloseResult, error) {
	if err := a.requireSprintControl(sess); err != nil {
		return nil, err
	}
	if unfinished == "" {
		unfinished = "backlog"
	}
	if unfinished != "backlog" && unfinished != "next" {
		return nil, Bad("err.unfinished_option")
	}
	if unfinished == "next" && nextID == "" {
		return nil, Bad("err.next_sprint_required")
	}
	if unfinished == "next" && nextID == id {
		return nil, Bad("err.next_sprint_same")
	}
	if a.sprintNeedsApproval(sess) {
		return nil, a.proposeOrFail(ctx, sess, func(tx pgx.Tx) (proposalDraft, error) {
			sp, err := a.Store.SprintByID(ctx, tx, id)
			if err != nil {
				return proposalDraft{}, NotFound("err.sprint_missing")
			}
			if sp.Status == domain.SprintClosed {
				return proposalDraft{}, Bad("err.sprint_already_closed")
			}
			return proposalDraft{Action: ActionSprintClose, Grant: domain.GrantManageWorkflows,
				TargetKind: "sprint", TargetID: sp.ID, TargetTitle: sp.Name,
				Payload: map[string]any{"sprint_id": sp.ID, "unfinished": unfinished, "next_sprint_id": nextID},
				Summary: i18n.M("proposal.summary.sprint.close", sp.Name, i18n.Key("proposal.unfinished."+unfinished))}, nil
		})
	}
	res := &CloseResult{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		sp, err := a.Store.SprintByID(ctx, tx, id)
		if err != nil {
			return NotFound("err.sprint_missing")
		}
		if sp.Status == domain.SprintClosed {
			return Bad("err.sprint_already_closed")
		}
		var next *domain.Sprint
		if unfinished == "next" {
			next, err = a.Store.SprintByID(ctx, tx, nextID)
			if err != nil {
				return NotFound("err.sprint_missing")
			}
			if next.Status == domain.SprintClosed {
				return Bad("err.next_sprint_closed", next.Name)
			}
		}
		tasks, err := a.Store.ListTasks(ctx, tx, store.TaskFilter{SprintID: sp.ID})
		if err != nil {
			return err
		}
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		terminal, done := isTerminalFn(types), isDoneFn(types)
		if sess.Write.DryRun {
			open := 0
			for _, t := range tasks {
				if !terminal(t) {
					open++
				}
			}
			if next != nil {
				return dryRun(sess, i18n.M("will.sprint.close_next", sp.Name, open, next.Name))
			}
			return dryRun(sess, i18n.M("will.sprint.close_backlog", sp.Name, open))
		}
		now := time.Now()
		var events []domain.Event
		for _, t := range tasks {
			if terminal(t) {
				continue
			}
			target := ""
			if next != nil {
				target = next.ID
			}
			if err := a.Store.SetTaskSprint(ctx, tx, t.ID, target); err != nil {
				return err
			}
			ev := sprintEvent(sess, "TaskRemovedFromSprint", sp, map[string]any{"reason": "sprint_closed"})
			ev.TaskID, ev.At = t.ID, now
			events = append(events, ev)
			if next != nil {
				add := sprintEvent(sess, "TaskAddedToSprint", next, map[string]any{"points": t.Points, "done": done(t), "reason": "carried_over", "from_sprint_id": sp.ID})
				add.TaskID, add.At = t.ID, now
				events = append(events, add)
				res.Moved++
			} else {
				res.Returned++
			}
		}
		sp.Status, sp.ClosedAt = domain.SprintClosed, &now
		if err := a.Store.UpdateSprint(ctx, tx, sp); err != nil {
			return err
		}
		events = append(events, sprintEvent(sess, "SprintClosed", sp, map[string]any{"moved": res.Moved, "returned": res.Returned, "unfinished": unfinished, "next_sprint_id": nextID}))
		res.Sprint = sp
		return a.insertEvents(ctx, tx, sess, events)
	})
	return res, err
}

// AddTasksToSprint 把任务加入迭代（已结束的拒绝）。任务原本在别的迭代里时先移出。返回实际加入的数量。
func (a *App) AddTasksToSprint(ctx context.Context, sess *Session, id string, taskIDs []string) (int, error) {
	return idempotent(ctx, a, sess, "add_tasks_to_sprint", map[string]any{"sprint_id": id, "task_ids": taskIDs}, func() (int, error) {
		return a.addTasksToSprint(ctx, sess, id, taskIDs)
	})
}

func (a *App) addTasksToSprint(ctx context.Context, sess *Session, id string, taskIDs []string) (int, error) {
	added := 0
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		sp, err := a.Store.SprintByID(ctx, tx, id)
		if err != nil {
			return NotFound("err.sprint_missing")
		}
		if sp.Status == domain.SprintClosed {
			return Bad("err.sprint_closed_add")
		}
		names, err := a.Store.SprintNames(ctx, tx)
		if err != nil {
			return err
		}
		var events []domain.Event
		for _, tid := range taskIDs {
			t, err := a.Store.TaskByID(ctx, tx, tid)
			if err != nil {
				return err
			}
			if t.SprintID == sp.ID {
				continue
			}
			tt, err := a.Store.TaskType(ctx, tx, t.TypeName, t.TypeVersion)
			if err != nil {
				return err
			}
			if sess.Write.DryRun {
				added++
				continue
			}
			evs, err := a.moveTaskToSprint(ctx, tx, sess, t, tt, sp, names)
			if err != nil {
				return err
			}
			events = append(events, evs...)
			added++
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.sprint.add_tasks", added, sp.Name))
		}
		return a.insertEvents(ctx, tx, sess, events)
	})
	return added, err
}

// moveTaskToSprint 把任务放进 sp（nil 表示移出迭代），返回要写的动态；不写库以外的东西。
func (a *App) moveTaskToSprint(ctx context.Context, tx pgx.Tx, sess *Session, t *domain.Task, tt *domain.TaskType, sp *domain.Sprint, names map[string]string) ([]domain.Event, error) {
	now := time.Now()
	var events []domain.Event
	if t.SprintID != "" {
		old := &domain.Sprint{ID: t.SprintID, Name: names[t.SprintID]}
		ev := sprintEvent(sess, "TaskRemovedFromSprint", old, nil)
		ev.TaskID, ev.At = t.ID, now
		events = append(events, ev)
	}
	target := ""
	if sp != nil {
		target = sp.ID
		done := false
		if st := tt.Workflow.State(t.State); st != nil {
			done = st.Label == domain.LabelTerminalSuccess
		}
		ev := sprintEvent(sess, "TaskAddedToSprint", sp, map[string]any{"points": t.Points, "done": done})
		ev.TaskID, ev.At = t.ID, now
		events = append(events, ev)
	}
	t.SprintID = target
	return events, a.Store.SetTaskSprint(ctx, tx, t.ID, target)
}

// RemoveTaskFromSprint 把任务移出迭代。
func (a *App) RemoveTaskFromSprint(ctx context.Context, sess *Session, id, taskID string) error {
	_, err := idempotent(ctx, a, sess, "remove_task_from_sprint", map[string]any{"sprint_id": id, "task_id": taskID}, func() (struct{}, error) {
		return struct{}{}, a.removeTaskFromSprint(ctx, sess, id, taskID)
	})
	return err
}

func (a *App) removeTaskFromSprint(ctx context.Context, sess *Session, id, taskID string) error {
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		sp, err := a.Store.SprintByID(ctx, tx, id)
		if err != nil {
			return NotFound("err.sprint_missing")
		}
		t, err := a.Store.TaskByID(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if t.SprintID != sp.ID {
			return Bad("err.task_not_in_sprint", sp.Name)
		}
		tt, err := a.Store.TaskType(ctx, tx, t.TypeName, t.TypeVersion)
		if err != nil {
			return err
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.sprint.remove_task", taskRefText(t), sp.Name))
		}
		names := map[string]string{sp.ID: sp.Name}
		events, err := a.moveTaskToSprint(ctx, tx, sess, t, tt, nil, names)
		if err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, events)
	})
}

// sprintView 装配迭代汇总。
func (a *App) sprintView(ctx context.Context, tx pgx.Tx, sp *domain.Sprint, teams map[string]string) (*SprintView, []*domain.Task, map[string]*domain.TaskType, error) {
	tasks, err := a.Store.ListTasks(ctx, tx, store.TaskFilter{SprintID: sp.ID})
	if err != nil {
		return nil, nil, nil, err
	}
	types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
	if err != nil {
		return nil, nil, nil, err
	}
	v := &SprintView{Sprint: sp, TeamName: teams[sp.TeamID], Stats: domain.SummarizeSprint(tasks, isDoneFn(types))}
	return v, tasks, types, nil
}

func (a *App) teamNames(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	teams, err := a.Store.ListTeams(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, t := range teams {
		out[t.ID] = t.Name
	}
	return out, nil
}

// ListSprints 列出迭代（含汇总）。
func (a *App) ListSprints(ctx context.Context, sess *Session, f store.SprintFilter) ([]*SprintView, error) {
	out := []*SprintView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		// 范围（ADR 0013）：迭代按它自己的团队筛，没有团队的迭代只在全公司范围里出现。
		scope, err := a.scope(ctx, tx, sess)
		if err != nil {
			return err
		}
		sprints, err := a.Store.ListSprints(ctx, tx, f)
		if err != nil {
			return err
		}
		if !scope.All {
			kept := sprints[:0]
			for _, sp := range sprints {
				if scope.HasTeam(sp.TeamID) {
					kept = append(kept, sp)
				}
			}
			sprints = kept
		}
		teams, err := a.teamNames(ctx, tx)
		if err != nil {
			return err
		}
		for _, sp := range sprints {
			v, _, _, err := a.sprintView(ctx, tx, sp, teams)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

var burndownKinds = []string{"TaskAddedToSprint", "TaskRemovedFromSprint", "TaskTransitioned", "PointsChanged"}

// burndownEvents 装载并归一化燃尽图需要的动态：旧的 TaskTransitioned 没有状态类型时按任务类型补上。
func (a *App) burndownEvents(ctx context.Context, tx pgx.Tx, sp *domain.Sprint) ([]domain.Event, error) {
	rows, err := a.Store.EventsOfKinds(ctx, tx, burndownKinds)
	if err != nil {
		return nil, err
	}
	// 只关心曾进入这个迭代的任务
	involved := map[string]bool{}
	for _, e := range rows {
		if e.Type == "TaskAddedToSprint" {
			if sid, _ := e.Data["sprint_id"].(string); sid == sp.ID {
				involved[e.TaskID] = true
			}
		}
	}
	var ids []string
	for id := range involved {
		ids = append(ids, id)
	}
	tasks, err := a.Store.TasksByIDs(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	typeOf := map[string]*domain.TaskType{}
	for _, t := range tasks {
		tt, err := a.Store.TaskType(ctx, tx, t.TypeName, t.TypeVersion)
		if err == nil {
			typeOf[t.ID] = tt
		}
	}
	var out []domain.Event
	for _, e := range rows {
		if e.TaskID != "" && !involved[e.TaskID] {
			continue
		}
		ev := e.Event
		if ev.Type == "TaskTransitioned" {
			if _, ok := ev.Data["to_label"]; !ok {
				if tt := typeOf[ev.TaskID]; tt != nil {
					to, _ := ev.Data["to"].(string)
					from, _ := ev.Data["from"].(string)
					if st := tt.Workflow.State(to); st != nil {
						ev.Data["to_label"] = string(st.Label)
					}
					if st := tt.Workflow.State(from); st != nil {
						ev.Data["from_label"] = string(st.Label)
					}
				}
			}
		}
		out = append(out, ev)
	}
	return out, nil
}

// velocity 取最近 n 个已结束迭代（team 空表示全组织）的完成量。
func (a *App) velocity(ctx context.Context, tx pgx.Tx, teamID string, n int) (VelocityView, error) {
	v := VelocityView{Sprints: []domain.VelocitySample{}}
	closed, err := a.Store.ListSprints(ctx, tx, store.SprintFilter{TeamID: teamID, Status: domain.SprintClosed})
	if err != nil {
		return v, err
	}
	// ListSprints 按开始日期倒序；取最近 n 个，再按时间正序输出
	if len(closed) > n {
		closed = closed[:n]
	}
	for i := len(closed) - 1; i >= 0; i-- {
		sp := closed[i]
		tasks, err := a.Store.ListTasks(ctx, tx, store.TaskFilter{SprintID: sp.ID})
		if err != nil {
			return v, err
		}
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return v, err
		}
		st := domain.SummarizeSprint(tasks, isDoneFn(types))
		v.Sprints = append(v.Sprints, domain.VelocitySample{SprintID: sp.ID, Name: sp.Name, PointsDone: st.PointsDone, TasksDone: st.TasksDone})
	}
	v.AveragePoints = domain.AverageVelocity(v.Sprints)
	return v, nil
}

// Velocity 返回最近 5 个已结束迭代的速度。
func (a *App) Velocity(ctx context.Context, sess *Session, teamID string) (*VelocityView, error) {
	var v VelocityView
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		v, err = a.velocity(ctx, tx, teamID, 5)
		return
	})
	return &v, err
}

// GetSprint 返回迭代详情：任务、燃尽图、迭代速度。
func (a *App) GetSprint(ctx context.Context, sess *Session, id string) (*SprintDetail, error) {
	var d *SprintDetail
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		sp, err := a.Store.SprintByID(ctx, tx, id)
		if err != nil {
			return NotFound("err.sprint_missing")
		}
		teams, err := a.teamNames(ctx, tx)
		if err != nil {
			return err
		}
		v, tasks, types, err := a.sprintView(ctx, tx, sp, teams)
		if err != nil {
			return err
		}
		names, err := a.Store.ExecutorNames(ctx, tx)
		if err != nil {
			return err
		}
		d = &SprintDetail{SprintView: *v, Tasks: []TaskSummary{}}
		now := time.Now()
		for _, t := range tasks {
			d.Tasks = append(d.Tasks, summarize(t, types[t.TypeName], names, 0, now, sess.Loc()))
		}
		events, err := a.burndownEvents(ctx, tx, sp)
		if err != nil {
			return err
		}
		d.Burndown = domain.ComputeBurndown(domain.BurndownInput{Sprint: sp, Events: events, Now: now})
		d.Velocity, err = a.velocity(ctx, tx, sp.TeamID, 5)
		return err
	})
	return d, err
}

// SprintNames 返回迭代 ID → 名称（任务视图里显示所属迭代用）。
func (a *App) SprintNames(ctx context.Context, sess *Session) (map[string]string, error) {
	out := map[string]string{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		out, err = a.Store.SprintNames(ctx, tx)
		return
	})
	return out, err
}
