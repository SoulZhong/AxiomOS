package app

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// GoalView 是带汇总信息的目标。
type GoalView struct {
	*domain.Goal
	Progress   int         `json:"progress"`
	TaskCount  int         `json:"task_count"`
	DoneCount  int         `json:"done_count"`
	Cost       float64     `json:"cost"`
	OverBudget bool        `json:"over_budget"`
	Children   []*GoalView `json:"children"`
	Start      *time.Time  `json:"start,omitempty"` // 子项聚合的计划开始
	End        *time.Time  `json:"end,omitempty"`   // 子项聚合的计划结束
	// 里程碑（ADR 0016）：目标自己的，按日期升序；摘要只算自己的里程碑，提示按子树任务算。
	Milestones       []*MilestoneView        `json:"milestones"`
	MilestoneSummary domain.MilestoneSummary `json:"milestone_summary"`
}

// CreateGoalInput 是创建/修改目标的输入。
type CreateGoalInput struct {
	ParentID      string     `json:"parent_id"`
	TeamID        string     `json:"team_id"`
	OwnerMemberID string     `json:"owner_member_id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Budget        *float64   `json:"budget"`
	Deadline      *time.Time `json:"deadline"`
	PlannedStart  *time.Time `json:"planned_start"`
	PlannedEnd    *time.Time `json:"planned_end"`
}

// CreateGoal 创建目标。
func (a *App) CreateGoal(ctx context.Context, sess *Session, in CreateGoalInput) (*domain.Goal, error) {
	if in.Title == "" {
		return nil, Bad("err.title_required")
	}
	if sess.IsAgent() {
		if !sess.Actor.HasGrant(domain.GrantCreateGoal) {
			return nil, Forbidden("err.agent_no_grant", i18n.Key("grant.create_goal"))
		}
		if domain.NeedsApproval(sess.Actor, domain.GrantCreateGoal) {
			return nil, a.proposeOrFail(ctx, sess, func(tx pgx.Tx) (proposalDraft, error) {
				return proposalDraft{Action: ActionGoalCreate, Grant: domain.GrantCreateGoal, TargetKind: "goal", TargetTitle: in.Title,
					Payload: in, Summary: i18n.M("proposal.summary.goal.create", in.Title)}, nil
			})
		}
	}
	owner := in.OwnerMemberID
	if owner == "" {
		owner = sess.MemberID
	}
	g := &domain.Goal{OrgID: sess.OrgID, ParentID: in.ParentID, TeamID: in.TeamID, OwnerMemberID: owner, Title: in.Title, Description: in.Description, Budget: in.Budget, Deadline: in.Deadline, PlannedStart: in.PlannedStart, PlannedEnd: in.PlannedEnd}
	// 没指定团队时按就近原则归队：上级目标的团队 → 当前正看着的那个团队 → 创建者自己的团队。
	// 否则新目标会落成"未分组"，在按团队筛选的范围里直接消失。
	defaultTeam := in.TeamID == ""
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		var parent *domain.Goal
		if in.ParentID != "" {
			p, err := a.Store.GoalByID(ctx, tx, in.ParentID)
			if err != nil {
				return Bad("err.parent_goal_missing")
			}
			parent = p
		}
		if defaultTeam {
			switch {
			case parent != nil && parent.TeamID != "":
				g.TeamID = parent.TeamID
			case sess.Scope != "" && sess.Scope != "all" && sess.Scope != "mine":
				g.TeamID = sess.Scope
			default:
				if ix, err := a.OrgIndex(ctx, tx); err == nil {
					g.TeamID = ix.TeamOfExecutor(sess.MemberID)
				}
			}
		}
		if err := a.Store.CreateGoal(ctx, tx, g); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "GoalCreated", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"goal_id": g.ID, "title": g.Title}}})
	})
	return g, err
}

// UpdateGoalInput 是可改字段。指针为 nil 表示不改；要清空可空字段（预算、截止日、计划起止、进度）时把字段名写进 Clear。
// ParentID 指向空字符串表示提为顶级目标。
type UpdateGoalInput struct {
	Title            *string            `json:"title"`
	Description      *string            `json:"description"`
	Status           *domain.GoalStatus `json:"status"`
	OwnerMemberID    *string            `json:"owner_member_id"`
	TeamID           *string            `json:"team_id"`
	ParentID         *string            `json:"parent_id"`
	Budget           *float64           `json:"budget"`
	Deadline         *time.Time         `json:"deadline"`
	PlannedStart     *time.Time         `json:"planned_start"`
	PlannedEnd       *time.Time         `json:"planned_end"`
	ProgressOverride *int               `json:"progress_override"`
	Clear            []string           `json:"clear,omitempty"` // budget | deadline | planned_start | planned_end | progress_override
}

// UpdateGoal 修改目标；只有目标负责人、上级目标负责人或组织负责人可以。
// 每个真正变了的字段各记一条 GoalFieldChanged 动态（带旧值与新值），没有变化就不记。
// 换上级（parent_id）时整棵子树跟着走：目标不能挂到自己或自己的子目标下面，上级必须在调用者看得到的范围里。
func (a *App) UpdateGoal(ctx context.Context, sess *Session, id string, in UpdateGoalInput) (*domain.Goal, error) {
	var g *domain.Goal
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cur, err := a.Store.GoalByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if !a.canEditGoal(ctx, tx, sess, cur) {
			return Forbidden("err.goal_edit_forbidden")
		}
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		var changes []fieldChange
		if in.Title != nil && *in.Title != cur.Title {
			if *in.Title == "" {
				return Bad("err.title_required")
			}
			changes = append(changes, fieldChange{Field: "title", From: cur.Title, To: *in.Title})
			cur.Title = *in.Title
		}
		if in.Description != nil && *in.Description != cur.Description {
			changes = append(changes, fieldChange{Field: "description", From: cur.Description, To: *in.Description})
			cur.Description = *in.Description
		}
		if in.Status != nil && *in.Status != cur.Status {
			changes = append(changes, fieldChange{Field: "status", From: string(cur.Status), To: string(*in.Status)})
			cur.Status = *in.Status
		}
		if in.OwnerMemberID != nil && *in.OwnerMemberID != cur.OwnerMemberID {
			if *in.OwnerMemberID == "" {
				return Bad("err.member_missing")
			}
			if _, err := a.Store.MemberByID(ctx, tx, *in.OwnerMemberID); err != nil {
				return Bad("err.member_missing")
			}
			changes = append(changes, fieldChange{Field: "owner_member_id", From: cur.OwnerMemberID, To: *in.OwnerMemberID,
				FromTitle: a.executorName(ctx, tx, cur.OwnerMemberID), ToTitle: a.executorName(ctx, tx, *in.OwnerMemberID)})
			cur.OwnerMemberID = *in.OwnerMemberID
		}
		if in.TeamID != nil && *in.TeamID != cur.TeamID {
			names, err := a.teamNames(ctx, tx)
			if err != nil {
				return err
			}
			if *in.TeamID != "" && names[*in.TeamID] == "" {
				return Bad("err.team_missing")
			}
			changes = append(changes, fieldChange{Field: "team_id", From: nilIfEmpty(cur.TeamID), To: nilIfEmpty(*in.TeamID),
				FromTitle: names[cur.TeamID], ToTitle: names[*in.TeamID]})
			cur.TeamID = *in.TeamID
		}
		if in.ParentID != nil && *in.ParentID != cur.ParentID {
			goals, err := a.Store.ListGoals(ctx, tx)
			if err != nil {
				return err
			}
			titles := map[string]string{}
			for _, x := range goals {
				titles[x.ID] = x.Title
			}
			if *in.ParentID != "" {
				var parent *domain.Goal
				for _, x := range goals {
					if x.ID == *in.ParentID {
						parent = x
					}
				}
				if parent == nil {
					return Bad("err.parent_goal_missing")
				}
				if parent.TeamID != "" && !sess.CanSeeCollabTeam(parent.TeamID) {
					return Forbidden("err.parent_goal_hidden")
				}
				if parent.ID == cur.ID || goalDescendants(goals, cur.ID)[parent.ID] {
					return Bad("err.goal_parent_cycle")
				}
			}
			changes = append(changes, fieldChange{Field: "parent_id", From: nilIfEmpty(cur.ParentID), To: nilIfEmpty(*in.ParentID),
				FromTitle: titles[cur.ParentID], ToTitle: titles[*in.ParentID]})
			cur.ParentID = *in.ParentID
		}
		money := map[string]any{"currency": org.Currency}
		switch {
		case contains(in.Clear, "budget"):
			if cur.Budget != nil {
				changes = append(changes, fieldChange{Field: "budget", From: floatVal(cur.Budget), To: nil, Extra: money})
				cur.Budget = nil
			}
		case in.Budget != nil && !sameFloat(in.Budget, cur.Budget):
			changes = append(changes, fieldChange{Field: "budget", From: floatVal(cur.Budget), To: *in.Budget, Extra: money})
			cur.Budget = in.Budget
		}
		dates := []struct {
			field string
			cur   **time.Time
			in    *time.Time
		}{{"deadline", &cur.Deadline, in.Deadline}, {"planned_start", &cur.PlannedStart, in.PlannedStart}, {"planned_end", &cur.PlannedEnd, in.PlannedEnd}}
		for _, d := range dates {
			switch {
			case contains(in.Clear, d.field):
				if *d.cur != nil {
					changes = append(changes, fieldChange{Field: d.field, From: dateVal(*d.cur), To: nil})
					*d.cur = nil
				}
			case d.in != nil && !sameTime(d.in, *d.cur):
				// 截止日在接口层会跟着计划结束一起送来；两者同值同变时只记计划结束这一条
				if d.field == "deadline" && in.PlannedEnd != nil && in.PlannedEnd.Equal(*d.in) {
					*d.cur = d.in
					continue
				}
				changes = append(changes, fieldChange{Field: d.field, From: dateVal(*d.cur), To: dateVal(d.in)})
				*d.cur = d.in
			}
		}
		switch {
		case contains(in.Clear, "progress_override"):
			if cur.ProgressOverride != nil {
				changes = append(changes, fieldChange{Field: "progress_override", From: *cur.ProgressOverride, To: nil})
				cur.ProgressOverride = nil
			}
		case in.ProgressOverride != nil && (cur.ProgressOverride == nil || *cur.ProgressOverride != *in.ProgressOverride):
			var from any
			if cur.ProgressOverride != nil {
				from = *cur.ProgressOverride
			}
			changes = append(changes, fieldChange{Field: "progress_override", From: from, To: *in.ProgressOverride})
			cur.ProgressOverride = in.ProgressOverride
		}
		if len(changes) == 0 {
			g = cur
			return nil
		}
		if err := a.Store.UpdateGoal(ctx, tx, cur); err != nil {
			return err
		}
		g = cur
		base := map[string]any{"goal_id": cur.ID, "title": cur.Title}
		events := make([]domain.Event, 0, len(changes))
		for _, c := range changes {
			events = append(events, c.event("GoalFieldChanged", sess, "", base))
		}
		return a.insertEvents(ctx, tx, sess, events)
	})
	return g, err
}

// goalDescendants 返回某个目标的全部子孙（不含自己）。
func goalDescendants(goals []*domain.Goal, rootID string) map[string]bool {
	children := map[string][]string{}
	for _, g := range goals {
		if g.ParentID != "" {
			children[g.ParentID] = append(children[g.ParentID], g.ID)
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

// DeleteGoal 删除一个目标。只有空目标（没有子目标、没有任务）才能删；有内容的目标应当「放弃」，
// 这样历史与动态都留着（与成员注销、任务取消一致：可以停用，不做物理删除）。
func (a *App) DeleteGoal(ctx context.Context, sess *Session, id string) error {
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		g, err := a.Store.GoalByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if !a.canEditGoal(ctx, tx, sess, g) {
			return Forbidden("err.goal_edit_forbidden")
		}
		children, tasks, err := a.Store.GoalUsage(ctx, tx, id)
		if err != nil {
			return err
		}
		if children > 0 || tasks > 0 {
			return Bad("err.goal_not_empty", children, tasks)
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "GoalDeleted", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"goal_id": g.ID, "title": g.Title}}}); err != nil {
			return err
		}
		return a.Store.DeleteGoal(ctx, tx, id)
	})
}

func (a *App) canEditGoal(ctx context.Context, tx pgx.Tx, sess *Session, g *domain.Goal) bool {
	if sess.IsAgent() && !sess.Actor.HasGrant(domain.GrantCreateGoal) {
		return false
	}
	org, _ := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
	if org != nil && org.OwnerMemberID == sess.MemberID {
		return true
	}
	for cur := g; cur != nil; {
		if cur.OwnerMemberID == sess.MemberID {
			return true
		}
		if cur.ParentID == "" {
			break
		}
		p, err := a.Store.GoalByID(ctx, tx, cur.ParentID)
		if err != nil {
			break
		}
		cur = p
	}
	return false
}

// GoalTree 返回带进度、成本、时间聚合的目标树，按本次请求的范围筛选（范围只是筛选）。
func (a *App) GoalTree(ctx context.Context, sess *Session) ([]*GoalView, error) {
	var roots []*GoalView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, err := a.scope(ctx, tx, sess)
		if err != nil {
			return err
		}
		goals, err := a.Store.ListGoals(ctx, tx)
		if err != nil {
			return err
		}
		goals = filterGoals(scope, goals)
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		runs, err := a.Store.AllRuns(ctx, tx)
		if err != nil {
			return err
		}
		roots = buildGoalTree(goals, tasks, types, runs)
		return a.attachMilestones(ctx, tx, roots, tasks, types)
	})
	return roots, err
}

// GetGoal 返回单个目标及其子树。
func (a *App) GetGoal(ctx context.Context, sess *Session, id string) (*GoalView, error) {
	tree, err := a.GoalTree(ctx, sess)
	if err != nil {
		return nil, err
	}
	var find func(vs []*GoalView) *GoalView
	find = func(vs []*GoalView) *GoalView {
		for _, v := range vs {
			if v.ID == id {
				return v
			}
			if f := find(v.Children); f != nil {
				return f
			}
		}
		return nil
	}
	v := find(tree)
	if v == nil {
		return nil, NotFound("err.goal_missing")
	}
	return v, nil
}

// buildGoalTree 是纯函数：自下而上聚合。
func buildGoalTree(goals []*domain.Goal, tasks []*domain.Task, types map[string]*domain.TaskType, runs []*store.RunRow) []*GoalView {
	views := map[string]*GoalView{}
	for _, g := range goals {
		views[g.ID] = &GoalView{Goal: g, Children: []*GoalView{}, Milestones: []*MilestoneView{}}
	}
	costByTask := map[string]float64{}
	for _, r := range runs {
		costByTask[r.TaskID] += r.Cost
	}
	// 每个目标直接挂的任务：只算顶层任务（子任务的进度由父任务表达）
	type agg struct {
		weight, weighted float64
		count, done      int
		cost             float64
		start, end       *time.Time
	}
	direct := map[string]*agg{}
	for _, t := range tasks {
		if t.GoalID == "" {
			continue
		}
		ag := direct[t.GoalID]
		if ag == nil {
			ag = &agg{}
			direct[t.GoalID] = ag
		}
		ag.cost += costByTask[t.ID]
		if t.ParentID != "" {
			continue
		}
		w := 1.0
		if t.EstimateHours != nil && *t.EstimateHours > 0 {
			w = *t.EstimateHours
		}
		p := 0
		if tt := types[t.TypeName]; tt != nil {
			p = domain.Progress(t, tt)
			if s := tt.Workflow.State(t.State); s != nil && s.Label == domain.LabelTerminalSuccess {
				ag.done++
			}
		}
		ag.weight += w
		ag.weighted += w * float64(p)
		ag.count++
		ag.start = minTime(ag.start, t.PlannedStart)
		ag.end = maxTime(ag.end, t.PlannedEnd)
	}
	// 建树
	var roots []*GoalView
	for _, g := range goals {
		v := views[g.ID]
		if p := views[g.ParentID]; g.ParentID != "" && p != nil {
			p.Children = append(p.Children, v)
		} else {
			roots = append(roots, v)
		}
	}
	var fill func(v *GoalView) (weight, weighted float64)
	fill = func(v *GoalView) (float64, float64) {
		wSum, wdSum := 0.0, 0.0
		if ag := direct[v.ID]; ag != nil {
			wSum, wdSum = ag.weight, ag.weighted
			v.TaskCount, v.DoneCount, v.Cost = ag.count, ag.done, ag.cost
			v.Start, v.End = ag.start, ag.end
		}
		sort.Slice(v.Children, func(i, j int) bool { return v.Children[i].CreatedAt.Before(v.Children[j].CreatedAt) })
		for _, c := range v.Children {
			cw, cwd := fill(c)
			wSum += cw
			wdSum += cwd
			v.TaskCount += c.TaskCount
			v.DoneCount += c.DoneCount
			v.Cost += c.Cost
			v.Start = minTime(v.Start, c.Start)
			v.End = maxTime(v.End, c.End)
		}
		if v.PlannedStart != nil {
			v.Start = minTime(v.Start, v.PlannedStart)
		}
		if v.PlannedEnd != nil {
			v.End = maxTime(v.End, v.PlannedEnd)
		}
		switch {
		case v.Status == domain.GoalAchieved:
			v.Progress = 100
		case v.ProgressOverride != nil:
			v.Progress = *v.ProgressOverride
		case wSum > 0:
			v.Progress = int(wdSum/wSum + 0.5)
		}
		v.OverBudget = v.Budget != nil && v.Cost > *v.Budget
		return wSum, wdSum
	}
	for _, r := range roots {
		fill(r)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].CreatedAt.Before(roots[j].CreatedAt) })
	if roots == nil {
		roots = []*GoalView{}
	}
	return roots
}

// filterGoals 按范围裁剪目标：目标自己的团队在范围里就留下，上级目标为了保住树形也留下。
func filterGoals(scope *Scope, goals []*domain.Goal) []*domain.Goal {
	if scope == nil || scope.All {
		return goals
	}
	byID := map[string]*domain.Goal{}
	for _, g := range goals {
		byID[g.ID] = g
	}
	keep := map[string]bool{}
	for _, g := range goals {
		if !scope.Includes(g.TeamID) {
			continue
		}
		for cur := g; cur != nil && !keep[cur.ID]; cur = byID[cur.ParentID] {
			keep[cur.ID] = true
			if cur.ParentID == "" {
				break
			}
		}
	}
	out := make([]*domain.Goal, 0, len(goals))
	for _, g := range goals {
		if keep[g.ID] {
			out = append(out, g)
		}
	}
	return out
}

func minTime(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b == nil || a.Before(*b) {
		return a
	}
	return b
}

func maxTime(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b == nil || a.After(*b) {
		return a
	}
	return b
}
