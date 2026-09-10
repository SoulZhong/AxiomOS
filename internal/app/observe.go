package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// Agent 能力补齐第二批（观测）：看动态、看统计、看组织上下文。都是只读，不需确认，
// 范围照 ADR 0013 判（协作数据按范围筛，财务数据越权直接拒）。

// ---------- 动态 ----------

// EventFilter 是动态的筛选条件：按任务、按目标（含子树里的任务）、按时间起点。
type EventFilter struct {
	TaskID string
	GoalID string
	Since  *time.Time
	Limit  int
}

// EventLine 是给 Agent 看的一条动态：一句话加上能回到对象的引用。
type EventLine struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	At         time.Time `json:"at"`
	ActorID    string    `json:"actor_id,omitempty"`
	Actor      string    `json:"actor,omitempty"`
	TaskID     string    `json:"task_id,omitempty"`
	TaskNumber int       `json:"task_number,omitempty"`
	TaskTitle  string    `json:"task_title,omitempty"`
	GoalID     string    `json:"goal_id,omitempty"`
	Summary    string    `json:"summary"`
}

// ListEventLines 按条件列动态，句子与网页上完全一样（同一个 EventSummary）。
func (a *App) ListEventLines(ctx context.Context, sess *Session, f EventFilter) ([]EventLine, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	fetch := limit
	if f.GoalID != "" || f.Since != nil {
		fetch = 2000 // 先多取一些再筛，动态是倒序的
	}
	rows, err := a.Events(ctx, sess, f.TaskID, fetch)
	if err != nil {
		return nil, err
	}
	out := []EventLine{}
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		names, err := a.Store.ExecutorNames(ctx, tx)
		if err != nil {
			return err
		}
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		titles, goalOf, numbers := map[string]string{}, map[string]string{}, map[string]int{}
		for _, t := range tasks {
			titles[t.ID], goalOf[t.ID], numbers[t.ID] = t.Title, t.GoalID, t.Number
		}
		roles := map[string]i18n.Text{}
		if rs, err := a.Store.ListRoles(ctx, tx); err == nil {
			for _, ro := range rs {
				roles[ro.Name] = ro.Title
			}
		}
		var sub map[string]bool
		if f.GoalID != "" {
			goals, err := a.Store.ListGoals(ctx, tx)
			if err != nil {
				return err
			}
			sub = subtreeGoalIDs(goals, f.GoalID)
		}
		loc := sess.Loc()
		for _, e := range rows {
			if f.Since != nil && e.At.Before(*f.Since) {
				continue
			}
			goalID := goalOf[e.TaskID]
			if goalID == "" {
				goalID = eventGoalID(e)
			}
			if sub != nil && !sub[goalID] {
				continue
			}
			line := EventLine{ID: e.ID, Kind: e.Type, At: e.At, ActorID: e.ActorID, Actor: names[e.ActorID], TaskID: e.TaskID,
				TaskNumber: numbers[e.TaskID], TaskTitle: titles[e.TaskID], GoalID: goalID,
				Summary: EventSummary(e, names, titles, roles, loc) + ApprovalNote(e, names, loc)}
			out = append(out, line)
			if len(out) >= limit {
				break
			}
		}
		return nil
	})
	return out, err
}

// ---------- 目标统计 ----------

// LabelCounts 是按状态类型数的任务数。
type LabelCounts struct {
	Pending  int `json:"pending"`
	Active   int `json:"active"`
	Waiting  int `json:"waiting"`
	Done     int `json:"done"`
	Failed   int `json:"failed"`
	Overdue  int `json:"overdue"`
	NoOwner  int `json:"unassigned"`
	Subtasks int `json:"subtasks"`
}

func (lc *LabelCounts) add(t *domain.Task, st *domain.State, now time.Time) {
	if t.ParentID != "" {
		lc.Subtasks++
	}
	if st == nil {
		return
	}
	switch st.Label {
	case domain.LabelPending:
		lc.Pending++
	case domain.LabelActive:
		lc.Active++
	case domain.LabelWaiting:
		lc.Waiting++
	case domain.LabelTerminalSuccess:
		lc.Done++
	case domain.LabelTerminalFailure:
		lc.Failed++
	}
	if !st.Label.IsTerminal() {
		if t.PlannedEnd != nil && t.PlannedEnd.Before(now) {
			lc.Overdue++
		}
		if t.AssigneeID == "" {
			lc.NoOwner++
		}
	}
}

// GoalMetrics 是一个目标（含子树）的数字：进度、任务分布、成本对预算、周期、里程碑、成本归口。
type GoalMetrics struct {
	Goal           GoalRef                 `json:"goal"`
	SubGoals       int                     `json:"sub_goals"`
	Progress       int                     `json:"progress"`
	Tasks          LabelCounts             `json:"tasks"`
	TasksTotal     int                     `json:"tasks_total"`
	Cost           float64                 `json:"cost"`
	Budget         *float64                `json:"budget,omitempty"`
	BudgetUsed     *float64                `json:"budget_used_ratio,omitempty"`
	OverBudget     bool                    `json:"over_budget"`
	Currency       string                  `json:"currency"`
	LeadHoursAvg   float64                 `json:"lead_hours_avg"` // 已完成任务从创建到完成的平均小时数
	Milestones     domain.MilestoneSummary `json:"milestones"`
	CostByExecutor []StatBucket            `json:"cost_by_executor"`
	PlannedStart   string                  `json:"planned_start,omitempty"`
	PlannedEnd     string                  `json:"planned_end,omitempty"`
	Deadline       string                  `json:"deadline,omitempty"`
	DaysToDeadline *int                    `json:"days_to_deadline,omitempty"`
	Financial      bool                    `json:"financial"` // 能不能看这个范围的钱；看不了时成本都是 0
}

// GoalMetricsOf 算一个目标的数字。成本是财务数据：看不到这个范围的钱时成本字段留 0，其余照给。
func (a *App) GoalMetricsOf(ctx context.Context, sess *Session, goalID string) (*GoalMetrics, error) {
	tree, err := a.GoalTree(ctx, sess)
	if err != nil {
		return nil, err
	}
	path := goalPath(tree, goalID)
	if path == nil {
		return nil, NotFound("err.goal_missing")
	}
	v := path[len(path)-1]
	loc := sess.Loc()
	m := &GoalMetrics{Goal: GoalRef{ID: v.ID, Title: v.Title, Status: string(v.Status), StatusTitle: i18n.Tr(loc, "goal.status."+string(v.Status)), Progress: v.Progress},
		Progress: v.Progress, Milestones: v.MilestoneSummary, CostByExecutor: []StatBucket{},
		PlannedStart: briefDate(v.Start), PlannedEnd: briefDate(v.End), Deadline: briefDate(v.Deadline)}
	var count func(vs []*GoalView) int
	count = func(vs []*GoalView) int {
		n := 0
		for _, c := range vs {
			n += 1 + count(c.Children)
		}
		return n
	}
	m.SubGoals = count(v.Children)
	now := time.Now()
	if v.Deadline != nil {
		d := int(v.Deadline.Sub(now).Hours() / 24)
		m.DaysToDeadline = &d
	}
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		m.Currency = org.Currency
		scope, err := a.scope(ctx, tx, sess)
		if err != nil {
			return err
		}
		m.Financial = scope.Finance
		goals, err := a.Store.ListGoals(ctx, tx)
		if err != nil {
			return err
		}
		sub := subtreeGoalIDs(goals, goalID)
		all, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		var tasks []*domain.Task
		for _, t := range all {
			if sub[t.GoalID] {
				tasks = append(tasks, t)
			}
		}
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		leadN, leadSum := 0, 0.0
		for _, t := range tasks {
			var st *domain.State
			if tt := types[t.TypeName]; tt != nil {
				st = tt.Workflow.State(t.State)
			}
			m.TasksTotal++
			m.Tasks.add(t, st, now)
			if st != nil && st.Label == domain.LabelTerminalSuccess && t.ActualEnd != nil {
				leadN++
				leadSum += t.ActualEnd.Sub(t.CreatedAt).Hours()
			}
		}
		if leadN > 0 {
			m.LeadHoursAvg = leadSum / float64(leadN)
		}
		if !scope.Finance {
			return nil
		}
		m.Cost, m.Budget, m.OverBudget = v.Cost, v.Budget, v.OverBudget
		if v.Budget != nil && *v.Budget > 0 {
			r := v.Cost / *v.Budget
			m.BudgetUsed = &r
		}
		runs, err := a.Store.AllRuns(ctx, tx)
		if err != nil {
			return err
		}
		names, _ := a.Store.ExecutorNames(ctx, tx)
		inGoal := map[string]bool{}
		for _, t := range tasks {
			inGoal[t.ID] = true
		}
		by := map[string]*StatBucket{}
		for _, r := range runs {
			if !inGoal[r.TaskID] {
				continue
			}
			b := by[r.ExecutorID]
			if b == nil {
				b = &StatBucket{Key: r.ExecutorID, Title: names[r.ExecutorID]}
				by[r.ExecutorID] = b
			}
			b.Value += r.Cost
			b.Count++
			for _, u := range r.Usage {
				b.Tokens += u.TotalTokens()
			}
		}
		for _, b := range by {
			m.CostByExecutor = append(m.CostByExecutor, *b)
		}
		sort.Slice(m.CostByExecutor, func(i, j int) bool { return m.CostByExecutor[i].Value > m.CostByExecutor[j].Value })
		return nil
	})
	return m, err
}

// ---------- 任务统计 ----------

// TaskMetrics 是一个任务的数字：周期、在当前状态待了多久、验收打回几次、执行记录与用量、成本。
type TaskMetrics struct {
	TaskID        string       `json:"task_id"`
	Number        int          `json:"number"`
	Title         string       `json:"title"`
	State         StateView    `json:"state"`
	Progress      int          `json:"progress"`
	CreatedAt     time.Time    `json:"created_at"`
	LeadHours     float64      `json:"lead_hours"`     // 创建 → 完成（没完成就到现在）
	HoursInState  float64      `json:"hours_in_state"` // 在当前状态待了多久
	ReviewRounds  int          `json:"review_rounds"`  // 被打回的次数
	Overdue       bool         `json:"overdue"`
	DaysOverdue   int          `json:"days_overdue"`
	Runs          RunCounts    `json:"runs"`
	Cost          float64      `json:"cost"`
	Tokens        int64        `json:"tokens"`
	TokensByModel []StatBucket `json:"tokens_by_model"`
	Currency      string       `json:"currency"`
	Comments      int          `json:"comments"`
	Artifacts     int          `json:"artifacts"`
	Subtasks      int          `json:"subtasks"`
	Financial     bool         `json:"financial"`
}

// RunCounts 是执行记录的分布。
type RunCounts struct {
	Total     int `json:"total"`
	Active    int `json:"active"`
	Completed int `json:"completed"`
	Cancelled int `json:"cancelled"`
	TimedOut  int `json:"timed_out"`
}

func (rc *RunCounts) add(r *domain.Run) {
	rc.Total++
	if r.Active() {
		rc.Active++
		return
	}
	switch r.Outcome {
	case domain.RunCompleted:
		rc.Completed++
	case domain.RunCancelled:
		rc.Cancelled++
	case domain.RunTimedOut:
		rc.TimedOut++
	}
}

// TaskMetricsOf 算一个任务的数字。
func (a *App) TaskMetricsOf(ctx context.Context, sess *Session, taskID string) (*TaskMetrics, error) {
	var m *TaskMetrics
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.loadContext(ctx, tx, sess, taskID)
		if err != nil {
			return err
		}
		t, tt := c.Task, c.Type
		now := time.Now()
		loc := sess.Loc()
		m = &TaskMetrics{TaskID: t.ID, Number: t.Number, Title: t.Title, Progress: domain.Progress(t, tt), CreatedAt: t.CreatedAt,
			Comments: len(t.Comments), Artifacts: len(t.Artifacts), TokensByModel: []StatBucket{}}
		st := tt.Workflow.State(t.State)
		if st != nil {
			m.State = StateView{Name: st.Name, Title: st.Title.In(loc), Label: st.Label, LabelTitle: domain.LabelTitle(st.Label, loc)}
			if !st.Label.IsTerminal() && t.PlannedEnd != nil && t.PlannedEnd.Before(now) {
				m.Overdue = true
				m.DaysOverdue = int(now.Sub(*t.PlannedEnd).Hours() / 24)
			}
		}
		end := now
		if t.ActualEnd != nil {
			end = *t.ActualEnd
		}
		m.LeadHours = end.Sub(t.CreatedAt).Hours()
		events, err := a.Store.ListEvents(ctx, tx, t.ID, 1000)
		if err != nil {
			return err
		}
		stateSince := t.CreatedAt
		for _, e := range events { // 倒序：第一条 TaskTransitioned 就是最近一次
			if e.Type == "TaskTransitioned" {
				if stateSince == t.CreatedAt {
					stateSince = e.At
				}
				if tr, _ := e.Data["transition"].(string); tr == "reject" || tr == "test_fail" || tr == "reopen" {
					m.ReviewRounds++
				}
			}
		}
		m.HoursInState = end.Sub(stateSince).Hours()
		all, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		for _, x := range all {
			if x.ParentID == t.ID {
				m.Subtasks++
			}
		}
		runs, err := a.Store.RunsOfTask(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		m.Currency = org.Currency
		scope, err := a.scope(ctx, tx, sess)
		if err != nil {
			return err
		}
		m.Financial = scope.Finance
		byModel := map[string]*StatBucket{}
		for _, r := range runs {
			m.Runs.add(&r.Run)
			if !scope.Finance {
				continue
			}
			m.Cost += r.Cost
			for _, u := range r.Usage {
				b := byModel[u.ModelID]
				if b == nil {
					b = &StatBucket{Key: u.ModelID, Title: u.ModelID}
					byModel[u.ModelID] = b
				}
				b.Tokens += u.TotalTokens()
				b.Value += u.Cost
				b.Count++
				m.Tokens += u.TotalTokens()
			}
		}
		for _, b := range byModel {
			m.TokensByModel = append(m.TokensByModel, *b)
		}
		sort.Slice(m.TokensByModel, func(i, j int) bool { return m.TokensByModel[i].Tokens > m.TokensByModel[j].Tokens })
		return nil
	})
	return m, err
}

// ---------- 我的统计 ----------

// MyMetrics 是「我」（Agent 或成员本人）的数字：手上的活、执行记录、成本与用量、并发余量。
type MyMetrics struct {
	ExecutorID    string    `json:"executor_id"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	OpenTasks     int       `json:"open_tasks"`
	ActiveTasks   int       `json:"active_tasks"`
	Overdue       int       `json:"overdue"`
	AwaitingMe    int       `json:"awaiting_my_review"`
	ActiveRuns    int       `json:"active_runs"`
	MaxConcurrent *int      `json:"max_concurrent,omitempty"`
	Runs          RunCounts `json:"runs"`
	ReviewRounds  int       `json:"rejected"` // 我交上去被打回的次数
	SuccessRate   float64   `json:"success_rate"`
	// 成本与用量：全部、最近 30 天
	Cost          float64      `json:"cost"`
	Tokens        int64        `json:"tokens"`
	Cost30d       float64      `json:"cost_30d"`
	Tokens30d     int64        `json:"tokens_30d"`
	Currency      string       `json:"currency"`
	TokensByModel []StatBucket `json:"tokens_by_model"`
	// 未归口用量（ADR 0030）：客户端自动报上来、但那一刻没有开着的执行记录的用量，只记在我名下
	UnattributedTokens int64   `json:"unattributed_tokens"`
	UnattributedCost   float64 `json:"unattributed_cost"`
	// 预算对照：我有任务在其中、且设了预算的目标各自花了多少
	GoalBudgets []GoalBudgetLine `json:"goal_budgets"`
	Financial   bool             `json:"financial"`
}

// GoalBudgetLine 是一个目标的预算对照。
type GoalBudgetLine struct {
	GoalID     string  `json:"goal_id"`
	Title      string  `json:"title"`
	Budget     float64 `json:"budget"`
	Cost       float64 `json:"cost"`
	MyCost     float64 `json:"my_cost"`
	OverBudget bool    `json:"over_budget"`
}

// MyMetricsOf 算「我」的数字。自己的成本与用量永远给；目标的预算对照按财务范围，看不到就是空的。
func (a *App) MyMetricsOf(ctx context.Context, sess *Session) (*MyMetrics, error) {
	tree, err := a.GoalTree(ctx, sess)
	if err != nil {
		return nil, err
	}
	m := &MyMetrics{ExecutorID: sess.Actor.ID, Name: sess.Actor.Name, Kind: string(sess.Actor.Kind), TokensByModel: []StatBucket{}, GoalBudgets: []GoalBudgetLine{}}
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		m.Currency = org.Currency
		scope, err := a.scope(ctx, tx, sess)
		if err != nil {
			return err
		}
		m.Financial = scope.Finance
		if sess.IsAgent() {
			if ag, err := a.Store.AgentByID(ctx, tx, sess.AgentID); err == nil {
				mc := ag.MaxConcurrent
				m.MaxConcurrent = &mc
			}
		}
		now := time.Now()
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		mine := map[string]*domain.Task{}
		for _, t := range tasks {
			tt := types[t.TypeName]
			if tt == nil {
				continue
			}
			st := tt.Workflow.State(t.State)
			if st == nil || st.Label.IsTerminal() {
				continue
			}
			if domain.IsReviewer(t, sess.Actor) && domain.ReviewStep(&domain.Context{Task: t, Type: tt}, true) != "" {
				m.AwaitingMe++
			}
			if t.AssigneeID != sess.Actor.ID {
				continue
			}
			mine[t.ID] = t
			m.OpenTasks++
			if st.Label == domain.LabelActive {
				m.ActiveTasks++
			}
			if t.PlannedEnd != nil && t.PlannedEnd.Before(now) {
				m.Overdue++
			}
		}
		runs, err := a.Store.AllRuns(ctx, tx)
		if err != nil {
			return err
		}
		since30 := now.AddDate(0, 0, -30)
		byModel := map[string]*StatBucket{}
		myCostByTask := map[string]float64{}
		for _, r := range runs {
			if r.ExecutorID != sess.Actor.ID {
				continue
			}
			m.Runs.add(&r.Run)
			if r.Active() {
				m.ActiveRuns++
			}
			// 自己的用量与成本永远给自己看：那是它自己上报的；别人的钱（目标的预算对照）才按财务范围
			m.Cost += r.Cost
			myCostByTask[r.TaskID] += r.Cost
			if r.StartedAt.After(since30) {
				m.Cost30d += r.Cost
			}
			for _, u := range r.Usage {
				m.Tokens += u.TotalTokens()
				if r.StartedAt.After(since30) {
					m.Tokens30d += u.TotalTokens()
				}
				b := byModel[u.ModelID]
				if b == nil {
					b = &StatBucket{Key: u.ModelID, Title: u.ModelID}
					byModel[u.ModelID] = b
				}
				b.Tokens += u.TotalTokens()
				b.Value += u.Cost
				b.Count++
			}
		}
		if m.Runs.Total > 0 {
			m.SuccessRate = float64(m.Runs.Completed) / float64(m.Runs.Total)
		}
		for _, b := range byModel {
			m.TokensByModel = append(m.TokensByModel, *b)
		}
		sort.Slice(m.TokensByModel, func(i, j int) bool { return m.TokensByModel[i].Tokens > m.TokensByModel[j].Tokens })
		// 被打回：我做过的任务上的打回动态，归到最后一段执行记录是我的那些任务
		events, err := a.Store.ListEvents(ctx, tx, "", 5000)
		if err != nil {
			return err
		}
		lastExecutor := map[string]string{}
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			if e.Type == "RunEnded" {
				if ex, _ := e.Data["executor_id"].(string); ex != "" {
					lastExecutor[e.TaskID] = ex
				} else if id, _ := e.Data["run_id"].(string); id != "" {
					for _, r := range runs {
						if r.ID == id {
							lastExecutor[e.TaskID] = r.ExecutorID
						}
					}
				}
			}
			if e.Type == "TaskTransitioned" {
				if tr, _ := e.Data["transition"].(string); (tr == "reject" || tr == "test_fail" || tr == "reopen") && lastExecutor[e.TaskID] == sess.Actor.ID {
					m.ReviewRounds++
				}
			}
		}
		// 未归口用量（ADR 0030）：自己名下的数字，和自己的用量一样永远给
		if sess.IsAgent() {
			us, cost, err := a.unattributedUsage(ctx, tx, sess.OrgID, sess.AgentID, time.Time{})
			if err != nil {
				return err
			}
			for _, u := range us {
				m.UnattributedTokens += u.TotalTokens()
			}
			m.UnattributedCost = cost
		}
		if !scope.Finance {
			return nil
		}
		// 预算对照：我有任务（含已结束的）在其中且设了预算的目标
		goalsOfMine := map[string]bool{}
		for _, t := range tasks {
			if t.GoalID != "" && (t.AssigneeID == sess.Actor.ID || myCostByTask[t.ID] > 0) {
				goalsOfMine[t.GoalID] = true
			}
		}
		goals, err := a.Store.ListGoals(ctx, tx)
		if err != nil {
			return err
		}
		var walk func(vs []*GoalView)
		walk = func(vs []*GoalView) {
			for _, v := range vs {
				if v.Budget != nil && *v.Budget > 0 {
					sub := subtreeGoalIDs(goals, v.ID)
					hit, my := false, 0.0
					for _, t := range tasks {
						if sub[t.GoalID] && goalsOfMine[t.GoalID] {
							hit = true
						}
						if sub[t.GoalID] {
							my += myCostByTask[t.ID]
						}
					}
					if hit {
						m.GoalBudgets = append(m.GoalBudgets, GoalBudgetLine{GoalID: v.ID, Title: v.Title, Budget: *v.Budget, Cost: v.Cost, MyCost: my, OverBudget: v.OverBudget})
					}
				}
				walk(v.Children)
			}
		}
		walk(tree)
		return nil
	})
	return m, err
}

// ---------- 组织上下文（只读） ----------

// TeamBrief 是给 Agent 看的团队一行。
type TeamBrief struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	ParentID    string   `json:"parent_id,omitempty"`
	Lead        string   `json:"lead,omitempty"`
	MemberCount int      `json:"member_count"`
	Members     []string `json:"members"`
	Inactive    bool     `json:"inactive,omitempty"`
}

// OrgContext 是 Agent 规划与指派要用的组织背景：不开任何写入。
type OrgContext struct {
	Organization string            `json:"organization"`
	Currency     string            `json:"currency"`
	Visibility   string            `json:"collaboration_visibility"`
	VisibilityT  string            `json:"collaboration_visibility_title"`
	Scope        string            `json:"scope"`
	ScopeTitle   string            `json:"scope_title"`
	Financial    bool              `json:"financial"`
	Me           OrgContextMe      `json:"me"`
	Teams        []TeamBrief       `json:"teams"`
	Capabilities map[string]string `json:"capabilities"`
	GoalTypes    []GoalTypeBrief   `json:"goal_types"`
	TaskTypes    []TaskTypeBrief   `json:"task_types"`
	Roles        map[string]string `json:"roles"`
	Members      int               `json:"members"`
	Agents       int               `json:"agents"`
}

// OrgContextMe 是「我」在组织里的位置。
type OrgContextMe struct {
	ExecutorID    string            `json:"executor_id"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	OwnerID       string            `json:"owner_id,omitempty"`
	Owner         string            `json:"owner,omitempty"`
	Teams         []string          `json:"teams"`
	Roles         []string          `json:"roles"`
	Capabilities  []string          `json:"capabilities"`
	Grants        map[string]string `json:"grants,omitempty"`
	MaxConcurrent *int              `json:"max_concurrent,omitempty"`
}

// GoalTypeBrief 是目标类型一行。
type GoalTypeBrief struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Active           bool   `json:"active"`
	DefaultPrecision string `json:"default_precision,omitempty"`
}

// TaskTypeBrief 是任务类型一行。
type TaskTypeBrief struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// ListTeamBriefs 列团队（名字代替 ID）。
func (a *App) ListTeamBriefs(ctx context.Context, sess *Session) ([]TeamBrief, error) {
	out := []TeamBrief{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		var err error
		out, err = a.teamBriefs(ctx, tx)
		return err
	})
	return out, err
}

func (a *App) teamBriefs(ctx context.Context, tx pgx.Tx) ([]TeamBrief, error) {
	teams, err := a.Store.ListTeams(ctx, tx)
	if err != nil {
		return nil, err
	}
	members, err := a.Store.TeamMembers(ctx, tx)
	if err != nil {
		return nil, err
	}
	names, err := a.Store.ExecutorNames(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := make([]TeamBrief, 0, len(teams))
	for _, t := range teams {
		b := TeamBrief{ID: t.ID, Name: t.Name, ParentID: t.ParentID, Lead: names[t.LeadMemberID], Members: []string{}, Inactive: t.Inactive}
		for _, mid := range members[t.ID] {
			if n := names[mid]; n != "" {
				b.Members = append(b.Members, n)
			}
		}
		sort.Strings(b.Members)
		b.MemberCount = len(b.Members)
		out = append(out, b)
	}
	return out, nil
}

// OrgContextOf 组装组织背景。
func (a *App) OrgContextOf(ctx context.Context, sess *Session) (*OrgContext, error) {
	loc := sess.Loc()
	out := &OrgContext{Capabilities: map[string]string{}, GoalTypes: []GoalTypeBrief{}, TaskTypes: []TaskTypeBrief{}, Roles: map[string]string{}, Teams: []TeamBrief{},
		Me: OrgContextMe{ExecutorID: sess.Actor.ID, Name: sess.Actor.Name, Kind: string(sess.Actor.Kind), Teams: []string{}, Roles: sess.Actor.Roles, Capabilities: sess.Actor.Capabilities}}
	if out.Me.Roles == nil {
		out.Me.Roles = []string{}
	}
	if out.Me.Capabilities == nil {
		out.Me.Capabilities = []string{}
	}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		out.Organization, out.Currency = org.Name, org.Currency
		out.Visibility, out.VisibilityT = string(org.CollaborationVisibility), i18n.Tr(loc, "visibility."+string(org.CollaborationVisibility))
		scope, err := a.scope(ctx, tx, sess)
		if err != nil {
			return err
		}
		out.Scope, out.ScopeTitle, out.Financial = scope.ID, scope.Title, scope.Finance
		if out.Teams, err = a.teamBriefs(ctx, tx); err != nil {
			return err
		}
		ix, err := a.OrgIndex(ctx, tx)
		if err != nil {
			return err
		}
		for _, t := range out.Teams {
			if t.ID == ix.TeamOfExecutor(sess.Actor.ID) {
				out.Me.Teams = append(out.Me.Teams, t.Name)
			}
		}
		caps, err := a.Store.ListCapabilities(ctx, tx)
		if err != nil {
			return err
		}
		for k, t := range caps {
			out.Capabilities[k] = t.In(loc)
		}
		gts, err := a.Store.ListGoalTypes(ctx, tx)
		if err != nil {
			return err
		}
		for _, t := range gts {
			out.GoalTypes = append(out.GoalTypes, GoalTypeBrief{ID: t.ID, Name: t.Name, Active: t.Active, DefaultPrecision: string(t.DefaultPrecision)})
		}
		tts, err := a.Store.ListCurrentTaskTypes(ctx, tx)
		if err != nil {
			return err
		}
		for _, t := range tts {
			out.TaskTypes = append(out.TaskTypes, TaskTypeBrief{Name: t.Name, Title: t.Title.In(loc)})
		}
		roles, err := a.Store.ListRoles(ctx, tx)
		if err != nil {
			return err
		}
		for _, r := range roles {
			out.Roles[r.Name] = r.Title.In(loc)
		}
		ms, err := a.Store.ListMembers(ctx, tx)
		if err != nil {
			return err
		}
		for _, mbr := range ms {
			if mbr.Active {
				out.Members++
			}
		}
		ags, err := a.Store.ListAgents(ctx, tx)
		if err != nil {
			return err
		}
		names, _ := a.Store.ExecutorNames(ctx, tx)
		for _, ag := range ags {
			if ag.RevokedAt == nil {
				out.Agents++
			}
			if ag.ID == sess.AgentID {
				mc := ag.MaxConcurrent
				out.Me.MaxConcurrent = &mc
				out.Me.OwnerID, out.Me.Owner = ag.OwnerMemberID, names[ag.OwnerMemberID]
				out.Me.Grants = map[string]string{}
				for g, mode := range ag.Grants {
					out.Me.Grants[string(g)] = string(mode)
				}
			}
		}
		return nil
	})
	return out, err
}

// ---------- 通知 ----------

// NotificationLine 是给 Agent 看的一条通知。
type NotificationLine struct {
	ID         int64      `json:"id"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	TaskID     string     `json:"task_id,omitempty"`
	TaskNumber int        `json:"task_number,omitempty"`
	TaskTitle  string     `json:"task_title,omitempty"`
	ReadAt     *time.Time `json:"read_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// MyNotificationLines 列「我的」通知。成员就是自己的站内通知；Agent 没有自己的收件箱，看的是所有者
// 通知里**与自己有关的那些**：挂在自己负责、自己创建、自己参与或自己验收的任务上的。
// 其余（所有者本人的事）不给 Agent。
func (a *App) MyNotificationLines(ctx context.Context, sess *Session, unreadOnly bool, limit int) ([]NotificationLine, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out := []NotificationLine{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		rows, err := a.Store.ListNotifications(ctx, tx, sess.MemberID, 500)
		if err != nil {
			return err
		}
		mineTask, titles, numbers, err := a.tasksInvolving(ctx, tx, sess)
		if err != nil {
			return err
		}
		for _, n := range rows {
			if unreadOnly && n.ReadAt != nil {
				continue
			}
			if sess.IsAgent() && !mineTask[n.TaskID] {
				continue
			}
			out = append(out, NotificationLine{ID: n.ID, Title: n.Title, Body: n.Body, TaskID: n.TaskID, TaskNumber: numbers[n.TaskID], TaskTitle: titles[n.TaskID], ReadAt: n.ReadAt, CreatedAt: n.CreatedAt})
			if len(out) >= limit {
				break
			}
		}
		return nil
	})
	return out, err
}

// tasksInvolving 列出「我」有份的任务（负责、创建、参与、验收；Agent 按自己算，不含所有者本人的）。
func (a *App) tasksInvolving(ctx context.Context, tx pgx.Tx, sess *Session) (map[string]bool, map[string]string, map[string]int, error) {
	tasks, err := a.Store.AllTasks(ctx, tx)
	if err != nil {
		return nil, nil, nil, err
	}
	mine, titles, numbers := map[string]bool{}, map[string]string{}, map[string]int{}
	me := sess.Actor.ID
	for _, t := range tasks {
		titles[t.ID], numbers[t.ID] = t.Title, t.Number
		if t.AssigneeID == me || t.CreatorID == me || t.ReviewerID == me {
			mine[t.ID] = true
			continue
		}
		for _, p := range t.Participants {
			if p == me {
				mine[t.ID] = true
			}
		}
	}
	return mine, titles, numbers, nil
}

// MarkMyNotificationsRead 把几条通知标为已读，返回剩下的未读数。成员走原来的入口；
// Agent 只能标与自己有关的那些（不是它的一律忽略，不报错）。这是「写操作都要产生动态」的既有例外：
// 已读只是阅读状态。
func (a *App) MarkMyNotificationsRead(ctx context.Context, sess *Session, ids []int64) (int, error) {
	if !sess.IsAgent() {
		return a.MarkNotificationsRead(ctx, sess, ids)
	}
	if err := refuseDryRun(sess); err != nil {
		return 0, err
	}
	remaining := 0
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		rows, err := a.Store.ListNotifications(ctx, tx, sess.MemberID, 500)
		if err != nil {
			return err
		}
		mineTask, _, _, err := a.tasksInvolving(ctx, tx, sess)
		if err != nil {
			return err
		}
		want := map[int64]bool{}
		for _, id := range ids {
			want[id] = true
		}
		var allowed []int64
		for _, n := range rows {
			if want[n.ID] && mineTask[n.TaskID] {
				allowed = append(allowed, n.ID)
			}
		}
		if err := a.Store.MarkNotificationsReadByIDs(ctx, tx, sess.MemberID, allowed); err != nil {
			return err
		}
		for _, n := range rows {
			if n.ReadAt == nil && mineTask[n.TaskID] && !containsInt64(allowed, n.ID) {
				remaining++
			}
		}
		return nil
	})
	return remaining, err
}

func containsInt64(list []int64, x int64) bool {
	for _, v := range list {
		if v == x {
			return true
		}
	}
	return false
}

// ---------- 关联删除 ----------

// Unlink 解除两个任务的一条关联。Agent 要有「建立关联」授权；解除**前置**关系会改变依赖图，
// 对 Agent 一律先经人确认（动作名 task.unlink），其余关系按授权模式。
func (a *App) Unlink(ctx context.Context, sess *Session, taskID string, typ domain.RelationType, otherID string) (*domain.Task, error) {
	return idempotent(ctx, a, sess, "unlink_tasks", map[string]any{"task_id": taskID, "type": string(typ), "other_id": otherID}, func() (*domain.Task, error) {
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
			summary := i18n.M("proposal.summary.task.unlink", c.Task.Title, i18n.Key("rel."+string(typ)), other.Title)
			propose := func() error {
				if sess.Write.DryRun {
					return dryRunPending(sess, a.executorName(ctx, tx, sess.MemberID), summary)
				}
				v, err = a.createProposal(ctx, tx, sess, proposalDraft{
					Action: ActionTaskUnlink, Grant: domain.GrantLink, TargetKind: "task", TargetID: c.Task.ID, TargetTitle: c.Task.Title,
					Payload: map[string]any{"task_id": c.Task.ID, "type": string(typ), "other_id": otherID}, Summary: summary})
				return err
			}
			// 前置关系一律待确认：先让内核把授权与存在性判完（没授权、没这条关联都在这里被拒）
			if sess.IsAgent() && typ == domain.RelationBlocks && sess.ApprovedByID == "" {
				probe := *c.Task
				pc := *c
				pc.Task = &probe
				if _, err := domain.Unlink(&pc, withDirectGrants(sess.Actor), typ, otherID); err != nil {
					return err
				}
				return propose()
			}
			o, err := domain.Unlink(c, sess.Actor, typ, otherID)
			if err != nil {
				var na *domain.ErrNeedsApproval
				if errors.As(err, &na) {
					return propose()
				}
				return err
			}
			if sess.Write.DryRun {
				return dryRun(sess, i18n.M("will.task.unlink_from", taskRefText(c.Task), taskRefText(other), i18n.Key("rel."+string(typ))))
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
