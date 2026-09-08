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

// 概览、异常、负荷（ADR 0013）。三个接口都按范围出数：协作口径的数字谁都能看，
// 财务字段（成本、预算）在看不到的单元上留空，而不是让整个请求失败。

// PeriodRange 是一段时间。
type PeriodRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// UnitPrev 是上一个等长周期的对比数字。
type UnitPrev struct {
	Cost       *float64 `json:"cost"`
	Throughput int      `json:"throughput"`
}

// OverviewUnit 是概览里的一个组织单元。
type OverviewUnit struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Kind          string   `json:"kind"` // team | unassigned | self | scope
	GoalProgress  int      `json:"goal_progress"`
	TasksTotal    int      `json:"tasks_total"`
	TasksDone     int      `json:"tasks_done"`
	Overdue       int      `json:"overdue"`
	Cost          *float64 `json:"cost"`
	Budget        *float64 `json:"budget"`
	BudgetUsedPct *float64 `json:"budget_used_pct"`
	Throughput    int      `json:"throughput"`
	Financial     bool     `json:"financial"`
	Prev          UnitPrev `json:"prev"`
}

// TrendPoint 是趋势里的一格。
type TrendPoint struct {
	Bucket  string   `json:"bucket"`
	Cost    *float64 `json:"cost"`
	Done    int      `json:"done"`
	Created int      `json:"created"`
}

// OverviewView 是运营总览。
type OverviewView struct {
	Scope      string         `json:"scope"`
	ScopeTitle string         `json:"scope_title"`
	Period     string         `json:"period"`
	PeriodT    string         `json:"period_title"`
	Currency   string         `json:"currency"`
	Financial  bool           `json:"financial"`
	Range      PeriodRange    `json:"range"`
	PrevRange  PeriodRange    `json:"prev_range"`
	Units      []OverviewUnit `json:"units"`
	Totals     OverviewUnit   `json:"totals"`
	Trend      []TrendPoint   `json:"trend"`
}

// unitDef 是一个组织单元的定义。
type unitDef struct {
	ID    string
	Title string
	Kind  string
}

// units 把当前范围拆成下一级的组织单元，并给出「归口团队 → 单元」的映射。
// 全公司：根团队 + 「未分组」；某个团队：它的直接下级团队 + 团队自己的直属成员；
// 「我的团队」：我所属的每个团队各一格。
func (ix *OrgIndex) units(scope *Scope, loc i18n.Locale) ([]unitDef, map[string]string) {
	defs := []unitDef{}
	unitOf := map[string]string{}
	assign := func(unit string, teamIDs []string) {
		for _, id := range teamIDs {
			unitOf[id] = unit
		}
	}
	switch {
	case scope.All:
		for _, t := range ix.Teams {
			if t.ParentID != "" {
				continue
			}
			defs = append(defs, unitDef{ID: t.ID, Title: t.Name, Kind: "team"})
			assign(t.ID, teamDescendants(ix.Teams, t.ID))
		}
		defs = append(defs, unitDef{ID: "", Title: i18n.Tr(loc, "scope.unassigned"), Kind: "unassigned"})
		unitOf[""] = ""
	case scope.Root != "":
		defs = append(defs, unitDef{ID: scope.Root, Title: i18n.Trf(loc, "scope.direct", ix.TeamName[scope.Root]), Kind: "self"})
		unitOf[scope.Root] = scope.Root
		for _, t := range ix.Teams {
			if t.ParentID != scope.Root {
				continue
			}
			defs = append(defs, unitDef{ID: t.ID, Title: t.Name, Kind: "team"})
			assign(t.ID, teamDescendants(ix.Teams, t.ID))
		}
	default: // 我的团队
		inScope := map[string]bool{}
		for _, id := range scope.TeamIDs {
			inScope[id] = true
		}
		for _, id := range scope.TeamIDs {
			if inScope[ix.Parent[id]] {
				continue // 只有子树顶端单独成格
			}
			defs = append(defs, unitDef{ID: id, Title: ix.TeamName[id], Kind: "team"})
			assign(id, teamDescendants(ix.Teams, id))
		}
	}
	return defs, unitOf
}

func startOfDay(t time.Time) time.Time {
	t = t.Local()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// calendarDay 把「只有日期、没有时刻」的值当成一个日历日：只取年月日，不做时区换算。
// 计划结束日、里程碑日期这些存在 date 列里，驱动读出来带的是 UTC 零点；服务器时区在 UTC 以西时
// 用 startOfDay 换算会整整退一天，于是「昨天到期」被算成逾期两天。日期没有时区，别给它安一个。
func calendarDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func startOfWeek(t time.Time) time.Time {
	d := startOfDay(t)
	wd := int(d.Weekday())
	if wd == 0 {
		wd = 7
	}
	return d.AddDate(0, 0, -(wd - 1))
}

// periodRange 返回统计周期的时间窗与上一个等长窗口。周期是滚动窗口，从今天往回数。
func periodRange(period string, now time.Time) (PeriodRange, PeriodRange, int) {
	days := 7
	switch period {
	case "month":
		days = 30
	case "quarter":
		days = 90
	}
	to := now
	from := startOfDay(now).AddDate(0, 0, -(days - 1))
	prev := PeriodRange{From: from.AddDate(0, 0, -days), To: from}
	return PeriodRange{From: from, To: to}, prev, days
}

func inRange(t time.Time, r PeriodRange) bool {
	return !t.Before(r.From) && !t.After(r.To)
}

// Overview 组装运营总览。
func (a *App) Overview(ctx context.Context, sess *Session, period string) (*OverviewView, error) {
	switch period {
	case "", "week":
		period = "week"
	case "month", "quarter":
	default:
		return nil, Bad("err.period", period)
	}
	loc := sess.Loc()
	v := &OverviewView{Period: period, PeriodT: i18n.Tr(loc, "scope.period."+period), Units: []OverviewUnit{}, Trend: []TrendPoint{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, ix, err := a.scopedIndex(ctx, tx, sess)
		if err != nil {
			return err
		}
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		v.Scope, v.ScopeTitle, v.Currency, v.Financial = scope.ID, scope.Title, org.Currency, scope.Finance
		now := time.Now()
		rng, prev, days := periodRange(period, now)
		v.Range, v.PrevRange = rng, prev

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
		goals, err := a.Store.ListGoals(ctx, tx)
		if err != nil {
			return err
		}

		defs, unitOf := ix.units(scope, loc)
		acc := map[string]*OverviewUnit{}
		for _, d := range defs {
			acc[d.ID] = &OverviewUnit{ID: d.ID, Title: d.Title, Kind: d.Kind}
		}
		totals := &OverviewUnit{ID: scope.ID, Title: scope.Title, Kind: "scope"}
		unit := func(teamID string) (*OverviewUnit, bool) {
			key, ok := unitOf[teamID]
			if !ok {
				return nil, false
			}
			return acc[key], true
		}

		// 任务：总数、已完成、逾期、周期内完成量
		type goalAgg struct{ weight, weighted, budget, cost float64 }
		for _, t := range tasks {
			u, ok := unit(ix.TeamOfTask(t))
			if !ok {
				continue
			}
			tt := types[t.TypeName]
			st := (*domain.State)(nil)
			if tt != nil {
				st = tt.Workflow.State(t.State)
			}
			u.TasksTotal++
			totals.TasksTotal++
			if st == nil {
				continue
			}
			if st.Label == domain.LabelTerminalSuccess {
				u.TasksDone++
				totals.TasksDone++
				if t.ActualEnd != nil {
					if inRange(*t.ActualEnd, rng) {
						u.Throughput++
						totals.Throughput++
					} else if inRange(*t.ActualEnd, prev) {
						u.Prev.Throughput++
						totals.Prev.Throughput++
					}
				}
			}
			if !st.Label.IsTerminal() && t.PlannedEnd != nil && t.PlannedEnd.Before(now) {
				u.Overdue++
				totals.Overdue++
			}
		}

		// 成本：按成本归口（执行者所属团队）落到单元上
		cost := map[string]float64{}
		prevCost := map[string]float64{}
		totalCost, totalPrevCost := 0.0, 0.0
		for _, r := range runs {
			key, ok := unitOf[ix.TeamOfExecutor(r.ExecutorID)]
			if !ok {
				continue
			}
			switch {
			case inRange(r.StartedAt, rng):
				cost[key] += r.Cost
				totalCost += r.Cost
			case inRange(r.StartedAt, prev):
				prevCost[key] += r.Cost
				totalPrevCost += r.Cost
			}
		}

		// 目标：进度按目标树的加权口径，预算按目标所属团队归口
		tree := buildGoalTree(goals, tasks, types, runs)
		views := map[string]*GoalView{}
		var flatten func(vs []*GoalView)
		flatten = func(vs []*GoalView) {
			for _, x := range vs {
				views[x.ID] = x
				flatten(x.Children)
			}
		}
		flatten(tree)
		goalUnit := map[string]string{}
		for _, g := range goals {
			if key, ok := unitOf[g.TeamID]; ok {
				goalUnit[g.ID] = key
			}
		}
		aggs := map[string]*goalAgg{}
		totalAgg := &goalAgg{}
		byID := map[string]*domain.Goal{}
		for _, g := range goals {
			byID[g.ID] = g
		}
		// 进度按目标树的加权口径；预算执行率的分子只算有预算的目标的（子树）成本。
		// 父子目标可能归到不同单元，所以「最上层」分两种算：单元内最上层给单元用，
		// 范围内最上层给合计用，避免同一笔成本或同一个进度被重复计入。
		addGoal := func(ag *goalAgg, g *domain.Goal, gv *GoalView) {
			w := float64(gv.TaskCount)
			if w <= 0 {
				w = 1
			}
			ag.weight += w
			ag.weighted += w * float64(gv.Progress)
			if g.Budget != nil {
				ag.budget += *g.Budget
				ag.cost += gv.Cost
			}
		}
		for _, g := range goals {
			key, ok := goalUnit[g.ID]
			if !ok {
				continue
			}
			gv := views[g.ID]
			if gv == nil {
				continue
			}
			topInUnit, topInScope := true, true
			for p := byID[g.ParentID]; p != nil; p = byID[p.ParentID] {
				if pk, ok := goalUnit[p.ID]; ok {
					topInScope = false
					if pk == key {
						topInUnit = false
					}
				}
			}
			if topInUnit {
				ag := aggs[key]
				if ag == nil {
					ag = &goalAgg{}
					aggs[key] = ag
				}
				addGoal(ag, g, gv)
			}
			if topInScope {
				addGoal(totalAgg, g, gv)
			}
		}

		fin := func(u *OverviewUnit, teamID string, c, pc float64, ag *goalAgg, visible bool) {
			u.Financial = visible
			if !visible {
				return
			}
			cc, pp := c, pc
			u.Cost, u.Prev.Cost = &cc, &pp
			if ag != nil && ag.budget > 0 {
				b := ag.budget
				pct := ag.cost / b * 100
				u.Budget, u.BudgetUsedPct = &b, &pct
			}
		}
		for _, d := range defs {
			u := acc[d.ID]
			if ag := aggs[d.ID]; ag != nil && ag.weight > 0 {
				u.GoalProgress = int(ag.weighted/ag.weight + 0.5)
			}
			visible := sess.FinanceAll()
			if d.ID != "" {
				visible = sess.CanSeeFinanceTeam(d.ID)
			}
			fin(u, d.ID, cost[d.ID], prevCost[d.ID], aggs[d.ID], visible)
			v.Units = append(v.Units, *u)
		}
		if totalAgg.weight > 0 {
			totals.GoalProgress = int(totalAgg.weighted/totalAgg.weight + 0.5)
		}
		fin(totals, "", totalCost, totalPrevCost, totalAgg, scope.Finance)
		v.Totals = *totals

		// 趋势：周按天、月按周、季度按月
		bucketOf := func(t time.Time) string {
			switch period {
			case "month":
				return startOfWeek(t).Format("2006-01-02")
			case "quarter":
				return t.Local().Format("2006-01")
			}
			return startOfDay(t).Format("2006-01-02")
		}
		var buckets []string
		seen := map[string]bool{}
		for d := startOfDay(rng.From); !d.After(now); d = d.AddDate(0, 0, 1) {
			b := bucketOf(d)
			if !seen[b] {
				seen[b] = true
				buckets = append(buckets, b)
			}
		}
		_ = days
		trendCost, trendDone, trendCreated := map[string]float64{}, map[string]int{}, map[string]int{}
		for _, r := range runs {
			if _, ok := unitOf[ix.TeamOfExecutor(r.ExecutorID)]; !ok {
				continue
			}
			if inRange(r.StartedAt, rng) {
				trendCost[bucketOf(r.StartedAt)] += r.Cost
			}
		}
		for _, t := range tasks {
			if _, ok := unitOf[ix.TeamOfTask(t)]; !ok {
				continue
			}
			if inRange(t.CreatedAt, rng) {
				trendCreated[bucketOf(t.CreatedAt)]++
			}
			tt := types[t.TypeName]
			if tt == nil || t.ActualEnd == nil || !inRange(*t.ActualEnd, rng) {
				continue
			}
			if st := tt.Workflow.State(t.State); st != nil && st.Label == domain.LabelTerminalSuccess {
				trendDone[bucketOf(*t.ActualEnd)]++
			}
		}
		for _, b := range buckets {
			p := TrendPoint{Bucket: b, Done: trendDone[b], Created: trendCreated[b]}
			if scope.Finance {
				c := trendCost[b]
				p.Cost = &c
			}
			v.Trend = append(v.Trend, p)
		}
		return nil
	})
	return v, err
}

// ---------- 异常 ----------

// ExceptionTask 是异常列表里的一个任务，字段够界面直接处理。
type ExceptionTask struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	TypeName    string     `json:"type_name"`
	TypeTitle   string     `json:"type_title"`
	State       StateView  `json:"state"`
	AssigneeID  string     `json:"assignee_id"`
	Assignee    string     `json:"assignee"`
	TeamID      string     `json:"team_id"`
	Team        string     `json:"team"`
	GoalID      string     `json:"goal_id"`
	GoalTitle   string     `json:"goal_title"`
	Priority    int        `json:"priority"`
	PlannedEnd  *time.Time `json:"planned_end"`
	DaysOverdue int        `json:"days_overdue"`
}

// ExceptionGoal 是异常列表里的一个目标。
type ExceptionGoal struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	OwnerID     string     `json:"owner_id"`
	Owner       string     `json:"owner"`
	TeamID      string     `json:"team_id"`
	Team        string     `json:"team"`
	Status      string     `json:"status"`
	StatusTitle string     `json:"status_title"`
	Progress    int        `json:"progress"`
	Deadline    *time.Time `json:"deadline"`
	DaysOverdue int        `json:"days_overdue"`
	Budget      *float64   `json:"budget"`
	Cost        *float64   `json:"cost"`
	OverPct     *float64   `json:"over_pct"`
}

// StuckTask 是长时间停在同一个状态的任务。
type StuckTask struct {
	Task        ExceptionTask `json:"task"`
	DaysInState int           `json:"days_in_state"`
}

// ExceptionsView 是要关注的异常。
type ExceptionsView struct {
	Scope            string          `json:"scope"`
	ScopeTitle       string          `json:"scope_title"`
	OverdueTasks     []ExceptionTask `json:"overdue_tasks"`
	OverdueGoals     []ExceptionGoal `json:"overdue_goals"`
	OverBudgetGoals  []ExceptionGoal `json:"over_budget_goals"`
	StuckTasks       []StuckTask     `json:"stuck_tasks"`
	PendingProposals []*ProposalView `json:"pending_proposals"`
}

// StuckDays 是「停太久」的门槛：同一个状态待够这么多天就算卡住。
const StuckDays = 7

// Exceptions 列出当前范围里要关注的异常。
func (a *App) Exceptions(ctx context.Context, sess *Session) (*ExceptionsView, error) {
	loc := sess.Loc()
	v := &ExceptionsView{OverdueTasks: []ExceptionTask{}, OverdueGoals: []ExceptionGoal{}, OverBudgetGoals: []ExceptionGoal{}, StuckTasks: []StuckTask{}, PendingProposals: []*ProposalView{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, ix, err := a.scopedIndex(ctx, tx, sess)
		if err != nil {
			return err
		}
		v.Scope, v.ScopeTitle = scope.ID, scope.Title
		now := time.Now()
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		tasks = ix.filterTasks(scope, tasks)
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		names, err := a.Store.ExecutorNames(ctx, tx)
		if err != nil {
			return err
		}
		goals, err := a.Store.ListGoals(ctx, tx)
		if err != nil {
			return err
		}
		goalTitle := map[string]string{}
		for _, g := range goals {
			goalTitle[g.ID] = g.Title
		}
		events, err := a.Store.ListEvents(ctx, tx, "", 100000)
		if err != nil {
			return err
		}
		lastMove := map[string]time.Time{}
		for _, e := range events { // 倒序，第一条就是最新的
			if e.Type != "TaskTransitioned" || e.TaskID == "" {
				continue
			}
			if _, ok := lastMove[e.TaskID]; !ok {
				lastMove[e.TaskID] = e.At
			}
		}
		item := func(t *domain.Task) ExceptionTask {
			tid := ix.TeamOfTask(t)
			x := ExceptionTask{ID: t.ID, Title: t.Title, TypeName: t.TypeName, AssigneeID: t.AssigneeID, Assignee: names[t.AssigneeID],
				TeamID: tid, Team: ix.TeamName[tid], GoalID: t.GoalID, GoalTitle: goalTitle[t.GoalID], Priority: t.Priority, PlannedEnd: t.PlannedEnd}
			if x.Team == "" {
				x.Team = i18n.Tr(loc, "scope.unassigned")
			}
			if tt := types[t.TypeName]; tt != nil {
				x.TypeTitle = tt.Title.In(loc)
				if st := tt.Workflow.State(t.State); st != nil {
					x.State = StateView{Name: st.Name, Title: st.Title.In(loc), Label: st.Label, LabelTitle: domain.LabelTitle(st.Label, loc)}
				}
			}
			return x
		}
		for _, t := range tasks {
			tt := types[t.TypeName]
			if tt == nil {
				continue
			}
			st := tt.Workflow.State(t.State)
			if st == nil || st.Label.IsTerminal() {
				continue
			}
			if t.PlannedEnd != nil && t.PlannedEnd.Before(now) {
				x := item(t)
				x.DaysOverdue = int(now.Sub(*t.PlannedEnd).Hours() / 24)
				v.OverdueTasks = append(v.OverdueTasks, x)
			}
			since := t.CreatedAt
			if at, ok := lastMove[t.ID]; ok {
				since = at
			}
			if d := int(now.Sub(since).Hours() / 24); d >= StuckDays {
				v.StuckTasks = append(v.StuckTasks, StuckTask{Task: item(t), DaysInState: d})
			}
		}
		sort.Slice(v.OverdueTasks, func(i, j int) bool { return v.OverdueTasks[i].DaysOverdue > v.OverdueTasks[j].DaysOverdue })
		sort.Slice(v.StuckTasks, func(i, j int) bool { return v.StuckTasks[i].DaysInState > v.StuckTasks[j].DaysInState })

		// 目标：逾期、超预算
		allTasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		allTypes, err := a.Store.TaskTypesFor(ctx, tx, allTasks)
		if err != nil {
			return err
		}
		runs, err := a.Store.AllRuns(ctx, tx)
		if err != nil {
			return err
		}
		tree := buildGoalTree(goals, allTasks, allTypes, runs)
		views := map[string]*GoalView{}
		var flatten func(vs []*GoalView)
		flatten = func(vs []*GoalView) {
			for _, x := range vs {
				views[x.ID] = x
				flatten(x.Children)
			}
		}
		flatten(tree)
		for _, g := range goals {
			if !scope.HasTeam(g.TeamID) {
				continue
			}
			gv := views[g.ID]
			if gv == nil {
				continue
			}
			x := ExceptionGoal{ID: g.ID, Title: g.Title, OwnerID: g.OwnerMemberID, Owner: names[g.OwnerMemberID], TeamID: g.TeamID, Team: ix.TeamName[g.TeamID],
				Status: string(g.Status), StatusTitle: i18n.Tr(loc, "goal.status."+string(g.Status)), Progress: gv.Progress, Deadline: g.Deadline}
			if x.Team == "" {
				x.Team = i18n.Tr(loc, "scope.unassigned")
			}
			if g.Status != domain.GoalAchieved && g.Status != domain.GoalAbandoned && g.Deadline != nil && g.Deadline.Before(now) {
				y := x
				y.DaysOverdue = int(now.Sub(*g.Deadline).Hours() / 24)
				v.OverdueGoals = append(v.OverdueGoals, y)
			}
			// 预算是财务数据：看不到的就不列出来
			if g.Budget != nil && *g.Budget > 0 && gv.Cost > *g.Budget && sess.CanSeeFinanceTeam(g.TeamID) {
				y := x
				b, c := *g.Budget, gv.Cost
				pct := (c - b) / b * 100
				y.Budget, y.Cost, y.OverPct = &b, &c, &pct
				v.OverBudgetGoals = append(v.OverBudgetGoals, y)
			}
		}
		sort.Slice(v.OverdueGoals, func(i, j int) bool { return v.OverdueGoals[i].DaysOverdue > v.OverdueGoals[j].DaysOverdue })
		return nil
	})
	if err != nil {
		return nil, err
	}
	props, err := a.ListProposals(ctx, sess, ProposalFilter{Status: domain.ProposalPending, Mine: true})
	if err != nil {
		return nil, err
	}
	for _, p := range props {
		if p.CanDecide {
			v.PendingProposals = append(v.PendingProposals, p)
		}
	}
	return v, nil
}

// ---------- 负荷 ----------

// LoadRow 是一个执行者的负荷。
type LoadRow struct {
	ExecutorID           string  `json:"executor_id"`
	Kind                 string  `json:"kind"` // member | agent
	Name                 string  `json:"name"`
	TeamID               string  `json:"team_id"`
	Team                 string  `json:"team"`
	OpenTasks            int     `json:"open_tasks"`
	ActiveTasks          int     `json:"active_tasks"`
	PointsOpen           int     `json:"points_open"`
	PlannedHoursThisWeek float64 `json:"planned_hours_this_week"`
	Overdue              int     `json:"overdue"`
	CapacityHint         string  `json:"capacity_hint"`
	MaxConcurrent        *int    `json:"max_concurrent"`
	// Online 是旧口径（最近有没有心跳），只为兼容保留；对外的状态看 State（CONTEXT.md「Agent 状态」）。
	Online     *bool  `json:"online"`
	State      string `json:"state,omitempty"`
	StateTitle string `json:"state_title,omitempty"`
	ActiveRuns int    `json:"active_runs"`
}

// Load 返回范围内成员与 Agent 的负荷。
func (a *App) Load(ctx context.Context, sess *Session) ([]LoadRow, error) {
	loc := sess.Loc()
	out := []LoadRow{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, ix, err := a.scopedIndex(ctx, tx, sess)
		if err != nil {
			return err
		}
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
		now := time.Now()
		weekStart, weekEnd := startOfWeek(now), startOfWeek(now).AddDate(0, 0, 7)
		rows := map[string]*LoadRow{}
		order := []string{}
		add := func(id, kind, name string) *LoadRow {
			tid := ix.TeamOfExecutor(id)
			if !scope.HasTeam(tid) {
				return nil
			}
			r := &LoadRow{ExecutorID: id, Kind: kind, Name: name, TeamID: tid, Team: ix.TeamName[tid]}
			if r.Team == "" {
				r.Team = i18n.Tr(loc, "scope.unassigned")
			}
			rows[id] = r
			order = append(order, id)
			return r
		}
		for _, m := range ix.Members {
			if !m.Active {
				continue
			}
			add(m.ID, string(domain.ExecutorMember), m.Name)
		}
		memberActive := map[string]bool{}
		for _, m := range ix.Members {
			memberActive[m.ID] = m.Active
		}
		agentByID := map[string]*domain.Agent{}
		for _, ag := range ix.Agents {
			r := add(ag.ID, string(domain.ExecutorAgent), ag.Name)
			if r == nil {
				continue
			}
			mc := ag.MaxConcurrent
			on := ag.Online(now)
			r.MaxConcurrent, r.Online = &mc, &on
			agentByID[ag.ID] = ag
		}
		for _, t := range tasks {
			r := rows[t.AssigneeID]
			if r == nil {
				continue
			}
			tt := types[t.TypeName]
			if tt == nil {
				continue
			}
			st := tt.Workflow.State(t.State)
			if st == nil || st.Label.IsTerminal() {
				continue
			}
			r.OpenTasks++
			if st.Label == domain.LabelActive {
				r.ActiveTasks++
			}
			if t.Points != nil {
				r.PointsOpen += *t.Points
			}
			if t.EstimateHours != nil && overlapsWeek(t, weekStart, weekEnd) {
				r.PlannedHoursThisWeek += *t.EstimateHours
			}
			if t.PlannedEnd != nil && t.PlannedEnd.Before(now) {
				r.Overdue++
			}
		}
		for _, rr := range runs {
			if rr.EndedAt != nil {
				continue
			}
			if r := rows[rr.ExecutorID]; r != nil {
				r.ActiveRuns++
			}
		}
		for _, id := range order {
			r := rows[id]
			// Agent 的状态要等执行记录数出来才算得准（有打开的执行记录就是执行中）
			if ag := agentByID[id]; ag != nil {
				st := NewAgentStatus(loc, ag, now, r.ActiveRuns, memberActive[ag.OwnerMemberID])
				r.State, r.StateTitle = string(st.State), st.Title
			}
			r.CapacityHint = capacityHint(r, loc)
			out = append(out, *r)
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].OpenTasks > out[j].OpenTasks })
		return nil
	})
	return out, err
}

// overlapsWeek 判断任务的计划区间是否落在本周内（没有计划开始时按结束日算）。
func overlapsWeek(t *domain.Task, from, to time.Time) bool {
	start, end := t.PlannedStart, t.PlannedEnd
	if start == nil && end == nil {
		return false
	}
	if start == nil {
		start = end
	}
	if end == nil {
		end = start
	}
	return start.Before(to) && !end.Before(from)
}

// capacityHint 是一句给人看的负荷判断。没有工作量点数、没有工时的组织也算得出来：
// 只用「在办任务数、逾期数、Agent 并发上限」这几个总是存在的量。
func capacityHint(r *LoadRow, loc i18n.Locale) string {
	switch {
	case r.Overdue > 0 || (r.MaxConcurrent != nil && r.ActiveRuns > *r.MaxConcurrent):
		return i18n.Tr(loc, "load.capacity.over")
	case r.OpenTasks >= 5:
		return i18n.Tr(loc, "load.capacity.tight")
	case r.OpenTasks == 0:
		return i18n.Tr(loc, "load.capacity.idle")
	}
	return i18n.Tr(loc, "load.capacity.steady")
}

var _ = store.RunRow{}
