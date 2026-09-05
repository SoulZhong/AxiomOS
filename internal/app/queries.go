package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// TaskDetail 是任务详情：任务 + 类型 + 执行记录 + 流程可用性 + 关联任务摘要。
type TaskDetail struct {
	Task     *domain.Task      `json:"task"`
	Type     *domain.TaskType  `json:"type"`
	State    StateView         `json:"state"`
	Progress int               `json:"progress"`
	Runs     []*store.RunRow   `json:"runs"`
	Cost     float64           `json:"cost"`
	Workflow *WorkflowView     `json:"workflow"`
	Related  []TaskSummary     `json:"related"`
	Subtasks []TaskSummary     `json:"subtasks"`
	Names    map[string]string `json:"names"`
	Overdue  bool              `json:"overdue"`
}

// TaskSummary 是列表里的任务摘要。
type TaskSummary struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	TypeName     string     `json:"type_name"`
	TypeTitle    string     `json:"type_title"`
	State        StateView  `json:"state"`
	Progress     int        `json:"progress"`
	AssigneeID   string     `json:"assignee_id,omitempty"`
	Assignee     string     `json:"assignee,omitempty"`
	CreatorID    string     `json:"creator_id,omitempty"`
	ReviewerID   string     `json:"reviewer_id,omitempty"`
	GoalID       string     `json:"goal_id,omitempty"`
	ParentID     string     `json:"parent_id,omitempty"`
	Priority     int        `json:"priority"`
	PlannedStart *time.Time `json:"planned_start,omitempty"`
	PlannedEnd   *time.Time `json:"planned_end,omitempty"`
	ActualStart  *time.Time `json:"actual_start,omitempty"`
	ActualEnd    *time.Time `json:"actual_end,omitempty"`
	RequiredRole string     `json:"required_role,omitempty"`
	Relation     string     `json:"relation,omitempty"` // 与当前任务的关系（related 列表用）
	Overdue      bool       `json:"overdue"`
	Cost         float64    `json:"cost"`
	HumanOnly    bool       `json:"human_only"`
	Blockers     []string   `json:"blockers,omitempty"` // 前置任务 ID（甘特图画线用）
	Points       *int       `json:"points,omitempty"`   // 工作量
	SprintID     string     `json:"sprint_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func summarize(t *domain.Task, tt *domain.TaskType, names map[string]string, cost float64, now time.Time, loc i18n.Locale) TaskSummary {
	s := TaskSummary{ID: t.ID, Title: t.Title, TypeName: t.TypeName, AssigneeID: t.AssigneeID, Assignee: names[t.AssigneeID], CreatorID: t.CreatorID, ReviewerID: t.ReviewerID, GoalID: t.GoalID, ParentID: t.ParentID, Priority: t.Priority,
		PlannedStart: t.PlannedStart, PlannedEnd: t.PlannedEnd, ActualStart: t.ActualStart, ActualEnd: t.ActualEnd, RequiredRole: t.RequiredRole, Cost: cost, HumanOnly: t.HumanOnly, Points: t.Points, SprintID: t.SprintID, CreatedAt: t.CreatedAt}
	if tt != nil {
		s.TypeTitle = tt.Title.In(loc)
		s.Progress = domain.Progress(t, tt)
		if st := tt.Workflow.State(t.State); st != nil {
			s.State = StateView{Name: st.Name, Title: st.Title.In(loc), Label: st.Label, LabelTitle: domain.LabelTitle(st.Label, loc)}
			s.Overdue = t.PlannedEnd != nil && t.PlannedEnd.Before(now) && !st.Label.IsTerminal()
		}
	}
	return s
}

// GetTaskDetail 返回任务详情。
func (a *App) GetTaskDetail(ctx context.Context, sess *Session, id string) (*TaskDetail, error) {
	var d *TaskDetail
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.loadContext(ctx, tx, sess, id)
		if err != nil {
			return err
		}
		names, err := a.Store.ExecutorNames(ctx, tx)
		if err != nil {
			return err
		}
		runs, err := a.Store.RunsOfTask(ctx, tx, id)
		if err != nil {
			return err
		}
		if runs == nil {
			runs = []*store.RunRow{}
		}
		cost := 0.0
		for _, r := range runs {
			cost += r.Cost
		}
		d = &TaskDetail{Task: c.Task, Type: c.Type, Progress: domain.Progress(c.Task, c.Type), Runs: runs, Cost: cost, Workflow: a.workflowView(c, sess), Names: names, Related: []TaskSummary{}, Subtasks: []TaskSummary{}}
		d.State = d.Workflow.State
		now := time.Now()
		d.Overdue = c.Task.PlannedEnd != nil && c.Task.PlannedEnd.Before(now) && !d.State.Label.IsTerminal()
		// 关联：本任务指向的 + 指向本任务的
		var ids []string
		for _, r := range c.Task.Relations {
			ids = append(ids, r.OtherID)
		}
		others, err := a.Store.TasksByIDs(ctx, tx, ids)
		if err != nil {
			return err
		}
		all := append(append(others, c.Predecessors...), c.Bugs...)
		types, err := a.Store.TaskTypesFor(ctx, tx, all)
		if err != nil {
			return err
		}
		loc := sess.Loc()
		relTitle := map[domain.RelationType]string{domain.RelationBlocks: i18n.Tr(loc, "rel.blocks"), domain.RelationFoundIn: i18n.Tr(loc, "rel.found_in"), domain.RelationRelatesTo: i18n.Tr(loc, "rel.relates_to")}
		for _, r := range c.Task.Relations {
			for _, o := range others {
				if o.ID == r.OtherID {
					s := summarize(o, types[o.TypeName], names, 0, now, sess.Loc())
					s.Relation = i18n.Trf(loc, "rel.mine_to", relTitle[r.Type])
					d.Related = append(d.Related, s)
				}
			}
		}
		for _, p := range c.Predecessors {
			s := summarize(p, types[p.TypeName], names, 0, now, sess.Loc())
			s.Relation = i18n.Tr(loc, "rel.pred")
			d.Related = append(d.Related, s)
		}
		for _, b := range c.Bugs {
			s := summarize(b, types[b.TypeName], names, 0, now, sess.Loc())
			s.Relation = i18n.Tr(loc, "rel.bug")
			d.Related = append(d.Related, s)
		}
		subs, err := a.Store.ListTasks(ctx, tx, store.TaskFilter{ParentID: id})
		if err != nil {
			return err
		}
		subTypes, _ := a.Store.TaskTypesFor(ctx, tx, subs)
		for _, s := range subs {
			d.Subtasks = append(d.Subtasks, summarize(s, subTypes[s.TypeName], names, 0, now, sess.Loc()))
		}
		return nil
	})
	return d, err
}

// TaskBrief 是给执行者（尤其是 Agent）的完整任务说明。
type TaskBrief struct {
	Task               *domain.Task        `json:"task"`
	TypeTitle          string              `json:"type_title"`
	State              StateView           `json:"state"`
	AgentInstructions  string              `json:"agent_instructions"`
	TaskSchema         map[string]any      `json:"task_schema,omitempty"`
	ResultSchema       map[string]any      `json:"result_schema,omitempty"`
	GoalChain          []string            `json:"goal_chain"` // 自上而下的目标标题
	Predecessors       []TaskSummary       `json:"predecessors"`
	PredecessorResults []PredecessorResult `json:"predecessor_results"`
	Workflow           *WorkflowView       `json:"workflow"`
	Names              map[string]string   `json:"names"`
}

// PredecessorResult 是前置任务的结果与交付物。
type PredecessorResult struct {
	TaskID    string            `json:"task_id"`
	Title     string            `json:"title"`
	Result    any               `json:"result,omitempty"`
	Artifacts []domain.Artifact `json:"artifacts"`
}

// Brief 组装任务说明。
func (a *App) Brief(ctx context.Context, sess *Session, id string) (*TaskBrief, error) {
	var b *TaskBrief
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.loadContext(ctx, tx, sess, id)
		if err != nil {
			return err
		}
		names, _ := a.Store.ExecutorNames(ctx, tx)
		b = &TaskBrief{Task: c.Task, TypeTitle: c.Type.Title.In(sess.Loc()), AgentInstructions: c.Type.AgentInstructions, TaskSchema: c.Type.TaskSchema, ResultSchema: c.Type.ResultSchema, Workflow: a.workflowView(c, sess), Names: names, GoalChain: []string{}, Predecessors: []TaskSummary{}, PredecessorResults: []PredecessorResult{}}
		b.State = b.Workflow.State
		for gid := c.Task.GoalID; gid != ""; {
			g, err := a.Store.GoalByID(ctx, tx, gid)
			if err != nil {
				break
			}
			b.GoalChain = append([]string{g.Title}, b.GoalChain...)
			gid = g.ParentID
		}
		now := time.Now()
		for _, p := range c.Predecessors {
			b.Predecessors = append(b.Predecessors, summarize(p, c.Types[p.TypeName], names, 0, now, sess.Loc()))
			b.PredecessorResults = append(b.PredecessorResults, PredecessorResult{TaskID: p.ID, Title: p.Title, Result: p.Fields["result"], Artifacts: p.Artifacts})
		}
		return nil
	})
	return b, err
}

// ListTaskSummaries 列表（带名字、状态、成本），按本次请求的范围筛选。
func (a *App) ListTaskSummaries(ctx context.Context, sess *Session, f store.TaskFilter) ([]TaskSummary, error) {
	out := []TaskSummary{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, ix, err := a.scopedIndex(ctx, tx, sess)
		if err != nil {
			return err
		}
		tasks, err := a.Store.ListTasks(ctx, tx, f)
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
		runs, err := a.Store.AllRuns(ctx, tx)
		if err != nil {
			return err
		}
		cost := map[string]float64{}
		for _, r := range runs {
			cost[r.TaskID] += r.Cost
		}
		now := time.Now()
		for _, t := range tasks {
			s := summarize(t, types[t.TypeName], names, cost[t.ID], now, sess.Loc())
			for _, r := range t.Relations {
				_ = r
			}
			out = append(out, s)
		}
		return nil
	})
	return out, err
}

// MyTasks 返回我（含我的 Agent）名下的非终止任务。
func (a *App) MyTasks(ctx context.Context, sess *Session) ([]TaskSummary, error) {
	ids := []string{sess.Actor.ID}
	if !sess.IsAgent() {
		me, err := a.Me(ctx, sess)
		if err != nil {
			return nil, err
		}
		for _, ag := range me.Agents {
			ids = append(ids, ag.ID)
		}
	} else {
		ids = append(ids, sess.MemberID)
	}
	all, err := a.ListTaskSummaries(ctx, sess, store.TaskFilter{Executors: ids})
	if err != nil {
		return nil, err
	}
	out := []TaskSummary{}
	for _, s := range all {
		if !s.State.Label.IsTerminal() {
			out = append(out, s)
		}
	}
	return out, nil
}

// Backlog 返回待领取任务：无负责人、且状态可领取或处于进行中阶段。
func (a *App) Backlog(ctx context.Context, sess *Session) ([]TaskSummary, error) {
	all, err := a.ListTaskSummaries(ctx, sess, store.TaskFilter{Backlog: true})
	if err != nil {
		return nil, err
	}
	out := []TaskSummary{}
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		for _, s := range all {
			t, err := a.Store.TaskByID(ctx, tx, s.ID)
			if err != nil {
				return err
			}
			tt, err := a.Store.TaskType(ctx, tx, t.TypeName, t.TypeVersion)
			if err != nil {
				return err
			}
			st := tt.Workflow.State(t.State)
			if st != nil && (st.Claimable || st.Label == domain.LabelActive) {
				out = append(out, s)
			}
		}
		return nil
	})
	return out, err
}

// ---------- 甘特图 ----------

// GanttRow 是甘特图的一行分组。
type GanttRow struct {
	Key      string        `json:"key"`
	Title    string        `json:"title"`
	Kind     string        `json:"kind"` // goal | team | executor | type
	Start    *time.Time    `json:"start,omitempty"`
	End      *time.Time    `json:"end,omitempty"`
	Deadline *time.Time    `json:"deadline,omitempty"`
	Progress int           `json:"progress"`
	Tasks    []TaskSummary `json:"tasks"`
	Children []*GanttRow   `json:"children,omitempty"`
	// Milestones 只在目标行上有：目标自己的里程碑，甘特图画成横条上的菱形（ADR 0016）。
	Milestones []*MilestoneView `json:"milestones,omitempty"`
}

// Gantt 按分组轴返回甘特图数据。
func (a *App) Gantt(ctx context.Context, sess *Session, group string) ([]*GanttRow, error) {
	var rows []*GanttRow
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, ixs, err := a.scopedIndex(ctx, tx, sess)
		if err != nil {
			return err
		}
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		tasks = ixs.filterTasks(scope, tasks)
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		names, err := a.Store.ExecutorNames(ctx, tx)
		if err != nil {
			return err
		}
		runs, err := a.Store.AllRuns(ctx, tx)
		if err != nil {
			return err
		}
		cost := map[string]float64{}
		for _, r := range runs {
			cost[r.TaskID] += r.Cost
		}
		now := time.Now()
		blockers := map[string][]string{}
		for _, t := range tasks {
			for _, r := range t.Relations {
				if r.Type == domain.RelationBlocks {
					blockers[r.OtherID] = append(blockers[r.OtherID], t.ID)
				}
			}
		}
		sums := map[string]TaskSummary{}
		for _, t := range tasks {
			s := summarize(t, types[t.TypeName], names, cost[t.ID], now, sess.Loc())
			s.Blockers = blockers[t.ID]
			sums[t.ID] = s
		}
		switch group {
		case "team":
			teams, err := a.Store.ListTeams(ctx, tx)
			if err != nil {
				return err
			}
			// 按团队分组用同一套归口（负责人所属团队，Agent 算所有者的），范围外的团队不出现
			byTeam := map[string]*GanttRow{}
			for _, tm := range teams {
				if scope.HasTeam(tm.ID) {
					byTeam[tm.ID] = &GanttRow{Key: tm.ID, Title: tm.Name, Kind: "team", Tasks: []TaskSummary{}}
				}
			}
			none := &GanttRow{Key: "", Title: i18n.Tr(sess.Loc(), "scope.unassigned"), Kind: "team", Tasks: []TaskSummary{}}
			for _, t := range tasks {
				row := byTeam[ixs.TeamOfTask(t)]
				if row == nil {
					row = none
				}
				row.Tasks = append(row.Tasks, sums[t.ID])
			}
			for _, tm := range teams {
				if r := byTeam[tm.ID]; r != nil {
					rows = append(rows, r)
				}
			}
			if len(none.Tasks) > 0 {
				rows = append(rows, none)
			}
		case "executor":
			by := map[string]*GanttRow{}
			var keys []string
			for _, t := range tasks {
				k := t.AssigneeID
				if by[k] == nil {
					title := names[k]
					if k == "" {
						title = i18n.Tr(sess.Loc(), "group.unclaimed")
					}
					by[k] = &GanttRow{Key: k, Title: title, Kind: "executor", Tasks: []TaskSummary{}}
					keys = append(keys, k)
				}
				by[k].Tasks = append(by[k].Tasks, sums[t.ID])
			}
			sort.Strings(keys)
			for _, k := range keys {
				rows = append(rows, by[k])
			}
		case "type":
			by := map[string]*GanttRow{}
			var keys []string
			for _, t := range tasks {
				k := t.TypeName
				if by[k] == nil {
					title := k
					if tt := types[k]; tt != nil {
						title = tt.Title.In(sess.Loc())
					}
					by[k] = &GanttRow{Key: k, Title: title, Kind: "type", Tasks: []TaskSummary{}}
					keys = append(keys, k)
				}
				by[k].Tasks = append(by[k].Tasks, sums[t.ID])
			}
			sort.Strings(keys)
			for _, k := range keys {
				rows = append(rows, by[k])
			}
		default: // goal
			goals, err := a.Store.ListGoals(ctx, tx)
			if err != nil {
				return err
			}
			tree := buildGoalTree(goals, tasks, types, runs)
			if err := a.attachMilestones(ctx, tx, tree, tasks, types); err != nil {
				return err
			}
			var conv func(v *GoalView) *GanttRow
			conv = func(v *GoalView) *GanttRow {
				r := &GanttRow{Key: v.ID, Title: v.Title, Kind: "goal", Start: v.Start, End: v.End, Deadline: v.Deadline, Progress: v.Progress, Tasks: []TaskSummary{}, Milestones: v.Milestones}
				for _, t := range tasks {
					if t.GoalID == v.ID && t.ParentID == "" {
						r.Tasks = append(r.Tasks, sums[t.ID])
					}
				}
				for _, c := range v.Children {
					r.Children = append(r.Children, conv(c))
				}
				return r
			}
			for _, v := range tree {
				rows = append(rows, conv(v))
			}
			none := &GanttRow{Key: "", Title: i18n.Tr(sess.Loc(), "group.no_goal"), Kind: "goal", Tasks: []TaskSummary{}}
			for _, t := range tasks {
				if t.GoalID == "" && t.ParentID == "" {
					none.Tasks = append(none.Tasks, sums[t.ID])
				}
			}
			if len(none.Tasks) > 0 {
				rows = append(rows, none)
			}
		}
		for _, r := range rows {
			fillRowSpan(r)
		}
		return nil
	})
	if rows == nil {
		rows = []*GanttRow{}
	}
	return rows, err
}

func fillRowSpan(r *GanttRow) {
	for _, t := range r.Tasks {
		r.Start = minTime(r.Start, t.PlannedStart)
		r.End = maxTime(r.End, t.PlannedEnd)
	}
	for _, c := range r.Children {
		fillRowSpan(c)
		r.Start = minTime(r.Start, c.Start)
		r.End = maxTime(r.End, c.End)
	}
}

// ---------- 统计 ----------

// StatBucket 是一个分组的汇总。
type StatBucket struct {
	Key    string  `json:"key"`
	Title  string  `json:"title"`
	Value  float64 `json:"value"`
	Count  int     `json:"count"`
	Tokens int64   `json:"tokens"`
}

// CostStats 成本按 goal|team|executor|model 分组，按本次请求的范围裁剪。
// 这是财务数据：范围越权直接 403（ADR 0013，具体规则看组织的 finance_visibility 策略）。
func (a *App) CostStats(ctx context.Context, sess *Session, group string) ([]StatBucket, error) {
	out := []StatBucket{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, err := a.financeScope(ctx, tx, sess)
		if err != nil {
			return err
		}
		ix, err := a.OrgIndex(ctx, tx)
		if err != nil {
			return err
		}
		runs, err := a.Store.AllRuns(ctx, tx)
		if err != nil {
			return err
		}
		runs = ix.filterRuns(scope, runs)
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		taskByID := map[string]*domain.Task{}
		for _, t := range tasks {
			taskByID[t.ID] = t
		}
		names, _ := a.Store.ExecutorNames(ctx, tx)
		goals, _ := a.Store.ListGoals(ctx, tx)
		goalTitle := map[string]string{}
		for _, g := range goals {
			goalTitle[g.ID] = g.Title
		}
		buckets := map[string]*StatBucket{}
		add := func(key, title string, cost float64, tokens int64) {
			b := buckets[key]
			if b == nil {
				b = &StatBucket{Key: key, Title: title}
				buckets[key] = b
			}
			b.Value += cost
			b.Tokens += tokens
			b.Count++
		}
		for _, r := range runs {
			var tokens int64
			for _, u := range r.Usage {
				tokens += u.TotalTokens()
			}
			t := taskByID[r.TaskID]
			switch group {
			case "model":
				for _, u := range r.Usage {
					add(u.ModelID, u.ModelID, u.Cost, u.TotalTokens())
				}
			case "team":
				// 成本归口：执行者所属团队（Agent 算所有者的），没有团队算「未分组」。
				tid := ix.TeamOfExecutor(r.ExecutorID)
				title := ix.TeamName[tid]
				if title == "" {
					title = i18n.Tr(sess.Loc(), "scope.unassigned")
				}
				add(tid, title, r.Cost, tokens)
			case "executor":
				add(r.ExecutorID, names[r.ExecutorID], r.Cost, tokens)
			default:
				gid := ""
				if t != nil {
					gid = t.GoalID
				}
				title := goalTitle[gid]
				if title == "" {
					title = i18n.Tr(sess.Loc(), "group.no_goal")
				}
				add(gid, title, r.Cost, tokens)
			}
		}
		for _, b := range buckets {
			out = append(out, *b)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Value > out[j].Value })
		return nil
	})
	return out, err
}

// CycleStats 周期：创建→完成、待验收→完成的平均时长（小时），按任务类型。
type CycleStat struct {
	TypeName    string  `json:"type_name"`
	TypeTitle   string  `json:"type_title"`
	DoneCount   int     `json:"done_count"`
	LeadHours   float64 `json:"lead_hours"`   // 创建 → 完成
	ReviewHours float64 `json:"review_hours"` // 最后一次提交 → 完成
}

func (a *App) CycleStats(ctx context.Context, sess *Session) ([]CycleStat, error) {
	out := []CycleStat{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, ix, err := a.scopedIndex(ctx, tx, sess)
		if err != nil {
			return err
		}
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		tasks = ix.filterTasks(scope, tasks)
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		events, err := a.Store.ListEvents(ctx, tx, "", 100000)
		if err != nil {
			return err
		}
		lastSubmit := map[string]time.Time{}
		for i := len(events) - 1; i >= 0; i-- { // 正序
			e := events[i]
			if e.Type == "TaskTransitioned" {
				to, _ := e.Data["to"].(string)
				if to == "submitted" || to == "awaiting_acceptance" || to == "fixed" {
					lastSubmit[e.TaskID] = e.At
				}
			}
		}
		by := map[string]*CycleStat{}
		for _, t := range tasks {
			tt := types[t.TypeName]
			if tt == nil || t.ActualEnd == nil {
				continue
			}
			st := tt.Workflow.State(t.State)
			if st == nil || st.Label != domain.LabelTerminalSuccess {
				continue
			}
			c := by[t.TypeName]
			if c == nil {
				c = &CycleStat{TypeName: t.TypeName, TypeTitle: tt.Title.In(sess.Loc())}
				by[t.TypeName] = c
			}
			c.DoneCount++
			c.LeadHours += t.ActualEnd.Sub(t.CreatedAt).Hours()
			if s, ok := lastSubmit[t.ID]; ok {
				c.ReviewHours += t.ActualEnd.Sub(s).Hours()
			}
		}
		for _, c := range by {
			if c.DoneCount > 0 {
				c.LeadHours /= float64(c.DoneCount)
				c.ReviewHours /= float64(c.DoneCount)
			}
			out = append(out, *c)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].TypeName < out[j].TypeName })
		return nil
	})
	return out, err
}

// ThroughputStats 每周完成任务数（最近 12 周）。
func (a *App) ThroughputStats(ctx context.Context, sess *Session) ([]StatBucket, error) {
	out := []StatBucket{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, ix, err := a.scopedIndex(ctx, tx, sess)
		if err != nil {
			return err
		}
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		tasks = ix.filterTasks(scope, tasks)
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		now := time.Now()
		weekStart := func(t time.Time) time.Time {
			t = t.Truncate(24 * time.Hour)
			wd := int(t.Weekday())
			if wd == 0 {
				wd = 7
			}
			return t.AddDate(0, 0, -(wd - 1))
		}
		counts := map[string]int{}
		for _, t := range tasks {
			tt := types[t.TypeName]
			if tt == nil || t.ActualEnd == nil {
				continue
			}
			if st := tt.Workflow.State(t.State); st == nil || st.Label != domain.LabelTerminalSuccess {
				continue
			}
			counts[weekStart(*t.ActualEnd).Format("2006-01-02")]++
		}
		for i := 11; i >= 0; i-- {
			ws := weekStart(now.AddDate(0, 0, -7*i)).Format("2006-01-02")
			out = append(out, StatBucket{Key: ws, Title: ws, Value: float64(counts[ws]), Count: counts[ws]})
		}
		return nil
	})
	return out, err
}

// AgentStat 是 Agent 质量统计。
type AgentStat struct {
	AgentID     string  `json:"agent_id"`
	Name        string  `json:"name"`
	Owner       string  `json:"owner"`
	Runs        int     `json:"runs"`
	Completed   int     `json:"completed"`
	Cancelled   int     `json:"cancelled"`
	TimedOut    int     `json:"timed_out"`
	Rejected    int     `json:"rejected"` // 验收打回次数（提交后被打回）
	Cost        float64 `json:"cost"`
	Tokens      int64   `json:"tokens"`
	SuccessRate float64 `json:"success_rate"`
}

func (a *App) AgentStats(ctx context.Context, sess *Session) ([]AgentStat, error) {
	out := []AgentStat{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, err := a.financeScope(ctx, tx, sess)
		if err != nil {
			return err
		}
		ix, err := a.OrgIndex(ctx, tx)
		if err != nil {
			return err
		}
		agents, err := a.Store.ListAgents(ctx, tx)
		if err != nil {
			return err
		}
		names, _ := a.Store.ExecutorNames(ctx, tx)
		runs, err := a.Store.AllRuns(ctx, tx)
		if err != nil {
			return err
		}
		events, err := a.Store.ListEvents(ctx, tx, "", 100000)
		if err != nil {
			return err
		}
		// 打回：某任务的 reject 动态，归到该任务上一段结束的执行记录的执行者
		lastExecutor := map[string]string{}
		rejected := map[string]int{}
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			if e.Type == "RunEnded" {
				for _, r := range runs {
					if id, _ := e.Data["run_id"].(string); id == r.ID {
						lastExecutor[e.TaskID] = r.ExecutorID
					}
				}
			}
			if e.Type == "TaskTransitioned" {
				if tr, _ := e.Data["transition"].(string); tr == "reject" || tr == "test_fail" || tr == "reopen" {
					rejected[lastExecutor[e.TaskID]]++
				}
			}
		}
		for _, ag := range agents {
			// Agent 的成本归到所有者所属团队（成本归口），范围外的 Agent 不出现。
			if !scope.HasTeam(ix.TeamOfExecutor(ag.ID)) {
				continue
			}
			s := AgentStat{AgentID: ag.ID, Name: ag.Name, Owner: names[ag.OwnerMemberID], Rejected: rejected[ag.ID]}
			for _, r := range runs {
				if r.ExecutorID != ag.ID {
					continue
				}
				s.Runs++
				s.Cost += r.Cost
				for _, u := range r.Usage {
					s.Tokens += u.TotalTokens()
				}
				switch r.Outcome {
				case domain.RunCompleted:
					s.Completed++
				case domain.RunCancelled:
					s.Cancelled++
				case domain.RunTimedOut:
					s.TimedOut++
				}
			}
			if s.Runs > 0 {
				s.SuccessRate = float64(s.Completed) / float64(s.Runs)
			}
			out = append(out, s)
		}
		return nil
	})
	return out, err
}

// ---------- 其他查询 ----------

// ListTaskTypes 列出当前版本的任务类型。
func (a *App) ListTaskTypes(ctx context.Context, sess *Session) ([]*domain.TaskType, error) {
	var out []*domain.TaskType
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		out, err = a.Store.ListCurrentTaskTypes(ctx, tx)
		return
	})
	if out == nil {
		out = []*domain.TaskType{}
	}
	return out, err
}

// GetTaskType 读取任务类型当前版本。
func (a *App) GetTaskType(ctx context.Context, sess *Session, name string) (*domain.TaskType, error) {
	var out *domain.TaskType
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		out, err = a.Store.CurrentTaskType(ctx, tx, name)
		return
	})
	return out, err
}

// SaveTaskType 保存任务类型新版本。需要「管理流程」权限；Agent 的这项授权只能是「需要人确认」，
// 所以 Agent 调用时一律转成待确认操作（ADR 0003）。
func (a *App) SaveTaskType(ctx context.Context, sess *Session, tt *domain.TaskType) (*domain.TaskType, error) {
	if errs := domain.Validate(tt); len(errs) > 0 {
		return nil, Bad("err.workflow_invalid", strings.Join(domain.RenderErrors(sess.Loc(), errs), "；"))
	}
	if sess.IsAgent() {
		if !sess.Actor.HasGrant(domain.GrantManageWorkflows) {
			return nil, Forbidden("err.agent_no_grant", i18n.Key("grant.manage_workflows"))
		}
		if !sess.Can("manage_workflows") {
			return nil, Forbidden("err.workflow_forbidden")
		}
		if domain.NeedsApproval(sess.Actor, domain.GrantManageWorkflows) {
			return nil, a.proposeOrFail(ctx, sess, func(tx pgx.Tx) (proposalDraft, error) {
				return proposalDraft{Action: ActionTaskTypeSave, Grant: domain.GrantManageWorkflows,
					TargetKind: "task_type", TargetID: tt.Name, TargetTitle: tt.Title.In(sess.Loc()),
					Payload: tt, Summary: i18n.M("proposal.summary.task_type.save", tt.Title)}, nil
			})
		}
	}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		if org.OwnerMemberID != sess.MemberID && !sess.Can("manage_workflows") {
			return Forbidden("err.workflow_forbidden")
		}
		if err := a.Store.SaveTaskType(ctx, tx, sess.OrgID, tt); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "TaskTypeSaved", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"name": tt.Name, "version": tt.Workflow.Version}}})
	})
	return tt, err
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Events 动态列表，按本次请求的范围筛选（范围只是筛选，不是权限）。
func (a *App) Events(ctx context.Context, sess *Session, taskID string, limit int) ([]*store.EventRow, error) {
	out := []*store.EventRow{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, ix, err := a.scopedIndex(ctx, tx, sess)
		if err != nil {
			return err
		}
		rows, err := a.Store.ListEvents(ctx, tx, taskID, limit)
		if err != nil {
			return err
		}
		if rows == nil {
			return nil
		}
		if scope.All || taskID != "" {
			out = rows
			return nil
		}
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		inScope := map[string]bool{}
		for _, t := range ix.filterTasks(scope, tasks) {
			inScope[t.ID] = true
		}
		for _, e := range rows {
			// 没挂任务的动态（组织级）在收窄范围时不出现，避免串到别的团队去。
			if e.TaskID != "" && inScope[e.TaskID] {
				out = append(out, e)
			}
		}
		return nil
	})
	return out, err
}

// scopedIndex 一次拿到本次请求的范围与归口索引（协作口径）。
func (a *App) scopedIndex(ctx context.Context, tx pgx.Tx, sess *Session) (*Scope, *OrgIndex, error) {
	scope, err := a.scope(ctx, tx, sess)
	if err != nil {
		return nil, nil, err
	}
	ix, err := a.OrgIndex(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	return scope, ix, nil
}

// Notifications 站内通知。
func (a *App) Notifications(ctx context.Context, sess *Session, markRead bool) ([]*domain.Notification, error) {
	out := []*domain.Notification{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		rows, err := a.Store.ListNotifications(ctx, tx, sess.MemberID, 50)
		if err != nil {
			return err
		}
		if rows != nil {
			out = rows
		}
		if markRead {
			return a.Store.MarkNotificationsRead(ctx, tx, sess.MemberID)
		}
		return nil
	})
	return out, err
}
