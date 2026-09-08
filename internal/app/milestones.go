package app

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// 里程碑（ADR 0016）：属于目标的时间刻度，不是任务。谁能编辑目标（canEditGoal）就能增改删它的里程碑；
// Agent 受「创建目标」授权约束——里程碑是目标的一部分——授权模式是需要人确认时走待确认操作。
// 每次写操作都产生动态，摘要带上目标标题。

// MilestoneView 是带算出状态的里程碑。
type MilestoneView struct {
	*domain.Milestone
	Status domain.MilestoneStatus `json:"status"`
	// ReadyHint 为真表示「可以确认了」：目标子树里计划结束不晚于该日期的任务都已完成（且至少有一个）。
	ReadyHint bool `json:"ready_hint"`
}

// CreateMilestoneInput 是新增里程碑的输入。
type CreateMilestoneInput struct {
	GoalID      string     `json:"goal_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	DueOn       *time.Time `json:"due_on"`
}

// UpdateMilestoneInput 是可改字段。
type UpdateMilestoneInput struct {
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	DueOn       *time.Time `json:"due_on"`
}

// milestonePayload 是待确认操作里记下的「对哪条里程碑做什么」。
type milestonePayload struct {
	MilestoneID string                `json:"milestone_id"`
	Input       *UpdateMilestoneInput `json:"input,omitempty"`
}

// dateOnly 把时间截到当天零点（当地时区）。
func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// ListMilestones 列出一个目标自己的里程碑（不含子目标），按日期升序。
func (a *App) ListMilestones(ctx context.Context, sess *Session, goalID string) ([]*MilestoneView, error) {
	out := []*MilestoneView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		g, err := a.Store.GoalByID(ctx, tx, goalID)
		if err != nil {
			return NotFound("err.goal_missing")
		}
		ms, err := a.Store.MilestonesByGoal(ctx, tx, g.ID)
		if err != nil {
			return err
		}
		out, err = a.milestoneViews(ctx, tx, g, ms)
		return err
	})
	return out, err
}

// CreateMilestone 在目标上新增一条里程碑。
func (a *App) CreateMilestone(ctx context.Context, sess *Session, in CreateMilestoneInput) (*MilestoneView, error) {
	return idempotent(ctx, a, sess, "create_milestone", in, func() (*MilestoneView, error) {
		return a.createMilestone(ctx, sess, in)
	})
}

func (a *App) createMilestone(ctx context.Context, sess *Session, in CreateMilestoneInput) (*MilestoneView, error) {
	m := &domain.Milestone{OrgID: sess.OrgID, GoalID: in.GoalID, Title: strings.TrimSpace(in.Title), Description: in.Description, CreatedBy: sess.Actor.ID}
	if in.DueOn != nil {
		m.DueOn = dateOnly(*in.DueOn)
	}
	if err := a.validMilestone(sess, m); err != nil {
		return nil, err
	}
	if err := a.milestoneGate(ctx, sess, func(tx pgx.Tx) (proposalDraft, error) {
		g, err := a.Store.GoalByID(ctx, tx, in.GoalID)
		if err != nil {
			return proposalDraft{}, NotFound("err.goal_missing")
		}
		return proposalDraft{Action: ActionMilestoneCreate, Grant: domain.GrantCreateGoal, TargetKind: "goal", TargetID: g.ID, TargetTitle: g.Title,
			Payload: in, Summary: i18n.M("proposal.summary.milestone.create", g.Title, m.Title, i18n.Date(m.DueOn))}, nil
	}); err != nil {
		return nil, err
	}
	var v *MilestoneView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		g, err := a.Store.GoalByID(ctx, tx, in.GoalID)
		if err != nil {
			return NotFound("err.goal_missing")
		}
		if !a.canEditGoal(ctx, tx, sess, g) {
			return Forbidden("err.milestone_edit_forbidden")
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.milestone.create", g.Title, m.Title, i18n.Date(m.DueOn)))
		}
		if err := a.Store.InsertMilestone(ctx, tx, m); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{milestoneEvent("MilestoneCreated", sess, g, m)}); err != nil {
			return err
		}
		v, err = a.milestoneView(ctx, tx, g, m)
		return err
	})
	return v, err
}

// UpdateMilestone 修改名称、日期或说明。
func (a *App) UpdateMilestone(ctx context.Context, sess *Session, id string, in UpdateMilestoneInput) (*MilestoneView, error) {
	if err := a.milestoneGate(ctx, sess, func(tx pgx.Tx) (proposalDraft, error) {
		m, g, err := a.loadMilestone(ctx, tx, id)
		if err != nil {
			return proposalDraft{}, err
		}
		return proposalDraft{Action: ActionMilestoneUpdate, Grant: domain.GrantCreateGoal, TargetKind: "goal", TargetID: g.ID, TargetTitle: g.Title,
			Payload: milestonePayload{MilestoneID: m.ID, Input: &in}, Summary: i18n.M("proposal.summary.milestone.update", g.Title, m.Title)}, nil
	}); err != nil {
		return nil, err
	}
	var v *MilestoneView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		m, g, err := a.loadMilestone(ctx, tx, id)
		if err != nil {
			return err
		}
		if !a.canEditGoal(ctx, tx, sess, g) {
			return Forbidden("err.milestone_edit_forbidden")
		}
		if in.Title != nil {
			m.Title = strings.TrimSpace(*in.Title)
		}
		if in.Description != nil {
			m.Description = *in.Description
		}
		if in.DueOn != nil {
			m.DueOn = dateOnly(*in.DueOn)
		}
		if err := a.validMilestone(sess, m); err != nil {
			return err
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.milestone.update", g.Title, m.Title, i18n.Date(m.DueOn)))
		}
		if err := a.Store.UpdateMilestone(ctx, tx, m); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{milestoneEvent("MilestoneUpdated", sess, g, m)}); err != nil {
			return err
		}
		v, err = a.milestoneView(ctx, tx, g, m)
		return err
	})
	return v, err
}

// DeleteMilestone 删除一条里程碑（留下动态）。
func (a *App) DeleteMilestone(ctx context.Context, sess *Session, id string) error {
	if err := a.milestoneGate(ctx, sess, func(tx pgx.Tx) (proposalDraft, error) {
		m, g, err := a.loadMilestone(ctx, tx, id)
		if err != nil {
			return proposalDraft{}, err
		}
		return proposalDraft{Action: ActionMilestoneDelete, Grant: domain.GrantCreateGoal, TargetKind: "goal", TargetID: g.ID, TargetTitle: g.Title,
			Payload: milestonePayload{MilestoneID: m.ID}, Summary: i18n.M("proposal.summary.milestone.delete", g.Title, m.Title, i18n.Date(m.DueOn))}, nil
	}); err != nil {
		return err
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		m, g, err := a.loadMilestone(ctx, tx, id)
		if err != nil {
			return err
		}
		if !a.canEditGoal(ctx, tx, sess, g) {
			return Forbidden("err.milestone_edit_forbidden")
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.milestone.delete", g.Title, m.Title, i18n.Date(m.DueOn)))
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{milestoneEvent("MilestoneDeleted", sess, g, m)}); err != nil {
			return err
		}
		return a.Store.DeleteMilestone(ctx, tx, m.ID)
	})
}

// ReachMilestone 确认里程碑已达到。由人（或经授权的 Agent）确认，系统不自动判定（ADR 0016）。
func (a *App) ReachMilestone(ctx context.Context, sess *Session, id string) (*MilestoneView, error) {
	return idempotent(ctx, a, sess, "reach_milestone", map[string]any{"milestone_id": id}, func() (*MilestoneView, error) {
		return a.setReached(ctx, sess, id, true)
	})
}

// UnreachMilestone 撤销「已达到」的确认。
func (a *App) UnreachMilestone(ctx context.Context, sess *Session, id string) (*MilestoneView, error) {
	return idempotent(ctx, a, sess, "unreach_milestone", map[string]any{"milestone_id": id}, func() (*MilestoneView, error) {
		return a.setReached(ctx, sess, id, false)
	})
}

func (a *App) setReached(ctx context.Context, sess *Session, id string, reached bool) (*MilestoneView, error) {
	action, summaryKey, eventType := ActionMilestoneReach, "proposal.summary.milestone.reach", "MilestoneReached"
	if !reached {
		action, summaryKey, eventType = ActionMilestoneUnreach, "proposal.summary.milestone.unreach", "MilestoneUnreached"
	}
	if err := a.milestoneGate(ctx, sess, func(tx pgx.Tx) (proposalDraft, error) {
		m, g, err := a.loadMilestone(ctx, tx, id)
		if err != nil {
			return proposalDraft{}, err
		}
		if err := checkReachable(m, reached); err != nil {
			return proposalDraft{}, err
		}
		return proposalDraft{Action: action, Grant: domain.GrantCreateGoal, TargetKind: "goal", TargetID: g.ID, TargetTitle: g.Title,
			Payload: milestonePayload{MilestoneID: m.ID}, Summary: i18n.M(summaryKey, g.Title, m.Title, i18n.Date(m.DueOn))}, nil
	}); err != nil {
		return nil, err
	}
	var v *MilestoneView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		m, g, err := a.loadMilestone(ctx, tx, id)
		if err != nil {
			return err
		}
		if !a.canEditGoal(ctx, tx, sess, g) {
			return Forbidden("err.milestone_edit_forbidden")
		}
		if err := checkReachable(m, reached); err != nil {
			return err
		}
		if sess.Write.DryRun {
			key := "will.milestone.reach"
			if !reached {
				key = "will.milestone.unreach"
			}
			return dryRun(sess, i18n.M(key, g.Title, m.Title, i18n.Date(m.DueOn)))
		}
		if reached {
			now := time.Now()
			m.ReachedAt = &now
		} else {
			m.ReachedAt = nil
		}
		if err := a.Store.UpdateMilestone(ctx, tx, m); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{milestoneEvent(eventType, sess, g, m)}); err != nil {
			return err
		}
		v, err = a.milestoneView(ctx, tx, g, m)
		return err
	})
	return v, err
}

// checkReachable 拒绝重复确认与无确认可撤销，理由是完整句子。
func checkReachable(m *domain.Milestone, reached bool) error {
	if reached && m.Reached() {
		return Bad("err.milestone_already_reached", m.Title)
	}
	if !reached && !m.Reached() {
		return Bad("err.milestone_not_reached", m.Title)
	}
	return nil
}

// ---------- 内部 ----------

// milestoneGate 对 Agent 做授权检查：没有「创建目标」授权直接拒绝；授权是「需要人确认」时，
// 用 build 构造一条待确认操作记下来并返回 ProposalPending，什么都不改。成员直接放行（权限在 canEditGoal 里判）。
func (a *App) milestoneGate(ctx context.Context, sess *Session, build func(tx pgx.Tx) (proposalDraft, error)) error {
	if !sess.IsAgent() {
		return nil
	}
	if !sess.Actor.HasGrant(domain.GrantCreateGoal) {
		return Forbidden("err.agent_no_grant", i18n.Key("grant.create_goal"))
	}
	if domain.NeedsApproval(sess.Actor, domain.GrantCreateGoal) {
		return a.proposeOrFail(ctx, sess, build)
	}
	return nil
}

func (a *App) validMilestone(sess *Session, m *domain.Milestone) error {
	if errs := domain.ValidateMilestone(m); len(errs) > 0 {
		return Bad("err.milestone_invalid", strings.Join(domain.RenderErrors(sess.Loc(), errs), "；"))
	}
	return nil
}

// loadMilestone 取出里程碑与它所属的目标。
func (a *App) loadMilestone(ctx context.Context, tx pgx.Tx, id string) (*domain.Milestone, *domain.Goal, error) {
	m, err := a.Store.MilestoneByID(ctx, tx, id)
	if err != nil {
		return nil, nil, NotFound("err.milestone_missing")
	}
	g, err := a.Store.GoalByID(ctx, tx, m.GoalID)
	if err != nil {
		return nil, nil, NotFound("err.goal_missing")
	}
	return m, g, nil
}

// milestoneEvent 构造一条里程碑动态；数据里带目标标题，摘要按语言渲染时直接用。
func milestoneEvent(typ string, sess *Session, g *domain.Goal, m *domain.Milestone) domain.Event {
	return domain.Event{Type: typ, ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{
		"milestone_id": m.ID, "title": m.Title, "due_on": m.DueOn.Format("2006-01-02"),
		"goal_id": g.ID, "goal_title": g.Title,
	}}
}

// milestoneView 给单条里程碑算状态与提示。
func (a *App) milestoneView(ctx context.Context, tx pgx.Tx, g *domain.Goal, m *domain.Milestone) (*MilestoneView, error) {
	vs, err := a.milestoneViews(ctx, tx, g, []*domain.Milestone{m})
	if err != nil {
		return nil, err
	}
	return vs[0], nil
}

// milestoneViews 给一个目标的若干里程碑算状态与提示：装载目标子树里的任务与它们的类型。
func (a *App) milestoneViews(ctx context.Context, tx pgx.Tx, g *domain.Goal, ms []*domain.Milestone) ([]*MilestoneView, error) {
	goals, err := a.Store.ListGoals(ctx, tx)
	if err != nil {
		return nil, err
	}
	sub := subtreeGoalIDs(goals, g.ID)
	all, err := a.Store.AllTasks(ctx, tx)
	if err != nil {
		return nil, err
	}
	var tasks []*domain.Task
	for _, t := range all {
		if sub[t.GoalID] {
			tasks = append(tasks, t)
		}
	}
	types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
	if err != nil {
		return nil, err
	}
	return milestoneViewsOf(ms, tasks, types, time.Now()), nil
}

// subtreeGoalIDs 返回某个目标及其全部下级目标的 ID 集合。
func subtreeGoalIDs(goals []*domain.Goal, root string) map[string]bool {
	children := map[string][]string{}
	for _, g := range goals {
		if g.ParentID != "" {
			children[g.ParentID] = append(children[g.ParentID], g.ID)
		}
	}
	out := map[string]bool{root: true}
	stack := []string{root}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range children[cur] {
			if !out[c] {
				out[c] = true
				stack = append(stack, c)
			}
		}
	}
	return out
}

// milestoneViewsOf 是纯函数：按日期排序，算出每条的状态与「可以确认了」提示。
// tasks 是该目标子树里的全部任务；状态只看类型，不看名字。
func milestoneViewsOf(ms []*domain.Milestone, tasks []*domain.Task, types map[string]*domain.TaskType, now time.Time) []*MilestoneView {
	labelOf := func(t *domain.Task) domain.Label {
		if tt := types[t.TypeName]; tt != nil {
			if st := tt.Workflow.State(t.State); st != nil {
				return st.Label
			}
		}
		return ""
	}
	out := make([]*MilestoneView, 0, len(ms))
	for _, m := range domain.SortMilestones(ms) {
		out = append(out, &MilestoneView{Milestone: m, Status: domain.StatusOfMilestone(m, now), ReadyHint: domain.MilestoneReady(m, tasks, labelOf)})
	}
	return out
}

// attachMilestones 给目标树的每个节点挂上它自己的里程碑与摘要（提示按子树任务算）。
func (a *App) attachMilestones(ctx context.Context, tx pgx.Tx, roots []*GoalView, tasks []*domain.Task, types map[string]*domain.TaskType) error {
	var ids []string
	var collect func(vs []*GoalView)
	collect = func(vs []*GoalView) {
		for _, v := range vs {
			ids = append(ids, v.ID)
			collect(v.Children)
		}
	}
	collect(roots)
	ms, err := a.Store.MilestonesByGoals(ctx, tx, ids)
	if err != nil {
		return err
	}
	attachMilestoneViews(roots, ms, tasks, types, time.Now())
	return nil
}

// attachMilestoneViews 是纯函数：把里程碑分到各目标上，并用子树任务算提示。
func attachMilestoneViews(roots []*GoalView, ms []*domain.Milestone, tasks []*domain.Task, types map[string]*domain.TaskType, now time.Time) {
	byGoal := map[string][]*domain.Milestone{}
	for _, m := range ms {
		byGoal[m.GoalID] = append(byGoal[m.GoalID], m)
	}
	tasksByGoal := map[string][]*domain.Task{}
	for _, t := range tasks {
		if t.GoalID != "" {
			tasksByGoal[t.GoalID] = append(tasksByGoal[t.GoalID], t)
		}
	}
	var walk func(v *GoalView) []*domain.Task
	walk = func(v *GoalView) []*domain.Task {
		sub := append([]*domain.Task{}, tasksByGoal[v.ID]...)
		for _, c := range v.Children {
			sub = append(sub, walk(c)...)
		}
		v.Milestones = milestoneViewsOf(byGoal[v.ID], sub, types, now)
		v.MilestoneSummary = domain.SummarizeMilestones(byGoal[v.ID], now)
		return sub
	}
	for _, r := range roots {
		walk(r)
	}
}
