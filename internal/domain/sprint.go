package domain

import (
	"sort"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// SprintStatus 是迭代的状态：规划中、进行中、已结束。
type SprintStatus string

const (
	SprintPlanning SprintStatus = "planning"
	SprintActive   SprintStatus = "active"
	SprintClosed   SprintStatus = "closed"
)

// SprintStatusTitle 返回迭代状态在某语言下的名称。
func SprintStatusTitle(s SprintStatus, loc i18n.Locale) string {
	return i18n.Tr(loc, "sprint."+string(s))
}

// Sprint 是迭代：一段固定时间内团队承诺完成的一批任务（ADR 0012）。
// 任务通过 Task.SprintID 归属；迭代本身不是流程，也不是任务。
type Sprint struct {
	ID        string       `json:"id"`
	OrgID     string       `json:"org_id"`
	TeamID    string       `json:"team_id,omitempty"` // 空表示按组织
	Name      string       `json:"name"`
	Goal      string       `json:"goal"`
	StartsOn  time.Time    `json:"starts_on"` // 日期（当地零点）
	EndsOn    time.Time    `json:"ends_on"`   // 日期（当地零点），含当天
	Status    SprintStatus `json:"status"`
	CreatedBy string       `json:"created_by"`
	StartedAt *time.Time   `json:"started_at,omitempty"`
	ClosedAt  *time.Time   `json:"closed_at,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// ValidateSprint 校验迭代的名称与日期。返回全部问题，空切片表示通过。
func ValidateSprint(s *Sprint) []error {
	var errs []error
	add := func(key string, a ...any) { errs = append(errs, &ValidationError{Msg: i18n.M(key, a...)}) }
	if strings.TrimSpace(s.Name) == "" {
		add("val.sprint_no_name")
	}
	if s.StartsOn.IsZero() || s.EndsOn.IsZero() {
		add("val.sprint_no_dates")
	} else if dayOf(s.EndsOn).Before(dayOf(s.StartsOn)) {
		add("val.sprint_dates")
	}
	return errs
}

// dayOf 把时间截到当天零点（保留时区）。
func dayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// SprintStats 是一个迭代的任务数与工作量汇总。
type SprintStats struct {
	TaskCount   int
	PointsTotal int
	PointsDone  int
	TasksDone   int
}

// SummarizeSprint 汇总迭代里的任务：数量、工作量合计、已完成工作量。
// isDone 判断任务是否处于已完成类型的状态（由调用方按任务类型解释）。
func SummarizeSprint(tasks []*Task, isDone func(*Task) bool) SprintStats {
	var st SprintStats
	for _, t := range tasks {
		st.TaskCount++
		p := 0
		if t.Points != nil {
			p = *t.Points
		}
		st.PointsTotal += p
		if isDone(t) {
			st.TasksDone++
			st.PointsDone += p
		}
	}
	return st
}

// ---------- 燃尽图 ----------

// BurndownPoint 是燃尽图上的一个点。
type BurndownPoint struct {
	Date  string `json:"date"` // YYYY-MM-DD
	Value int    `json:"value"`
}

// Burndown 是燃尽图：单位（工作量或任务数）、理想线、实际线。
type Burndown struct {
	Unit   string          `json:"unit"` // points | tasks
	Ideal  []BurndownPoint `json:"ideal"`
	Actual []BurndownPoint `json:"actual"`
}

// BurndownInput 是回放燃尽图所需的材料。
//
// Events 按时间升序，只需要这几种动态：
//   - TaskAddedToSprint   data{sprint_id, points?, done?}
//   - TaskRemovedFromSprint data{sprint_id}
//   - TaskTransitioned    data{to_label, from_label}
//   - PointsChanged       data{points?}
//
// 燃尽由动态回放得出，不另存快照（ADR 0012 第 5 条）。
type BurndownInput struct {
	Sprint *Sprint
	Events []Event
	Unit   string // 空表示自动：迭代里有任务带工作量则按工作量，否则按任务数
	Now    time.Time
}

// burnState 是回放过程中的迭代状态。
type burnState struct {
	in     map[string]bool
	done   map[string]bool
	points map[string]*int
}

func newBurnState() *burnState {
	return &burnState{in: map[string]bool{}, done: map[string]bool{}, points: map[string]*int{}}
}

func (b *burnState) apply(sprintID string, e Event) {
	switch e.Type {
	case "TaskAddedToSprint":
		if sid, _ := e.Data["sprint_id"].(string); sid != sprintID {
			return
		}
		b.in[e.TaskID] = true
		if p, ok := intOf(e.Data["points"]); ok {
			b.points[e.TaskID] = &p
		} else if _, present := e.Data["points"]; present {
			b.points[e.TaskID] = nil
		}
		if d, ok := e.Data["done"].(bool); ok {
			b.done[e.TaskID] = d
		}
	case "TaskRemovedFromSprint":
		if sid, _ := e.Data["sprint_id"].(string); sid != sprintID {
			return
		}
		delete(b.in, e.TaskID)
	case "TaskTransitioned":
		to, _ := e.Data["to_label"].(string)
		if to == "" {
			return
		}
		b.done[e.TaskID] = Label(to) == LabelTerminalSuccess
	case "PointsChanged":
		if p, ok := intOf(e.Data["points"]); ok {
			b.points[e.TaskID] = &p
		} else {
			b.points[e.TaskID] = nil
		}
	}
}

func (b *burnState) hasPoints() bool {
	for id := range b.in {
		if p := b.points[id]; p != nil {
			return true
		}
	}
	return false
}

func (b *burnState) remaining(unit string) int {
	n := 0
	for id := range b.in {
		if b.done[id] {
			continue
		}
		if unit == "tasks" {
			n++
			continue
		}
		if p := b.points[id]; p != nil {
			n += *p
		}
	}
	return n
}

// intOf 把 JSON 解出来的数字（float64 / int / int64）转成 int。
func intOf(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case *int:
		if x != nil {
			return *x, true
		}
	}
	return 0, false
}

// ComputeBurndown 回放动态得到燃尽图。
//
// 实际线：从开始日到 min(结束日, 今天 / 结束时刻) 每天一个点，值是当天结束时剩余的工作量（或任务数）。
// 理想线：从承诺范围线性降到结束日的 0。承诺范围 = 迭代被点"开始"那一刻（没有则开始日零点）的剩余量；
// 若那时还是空的（开始后才装入任务），取实际线第一个非零值。
func ComputeBurndown(in BurndownInput) Burndown {
	s := in.Sprint
	out := Burndown{Unit: in.Unit, Ideal: []BurndownPoint{}, Actual: []BurndownPoint{}}
	if s == nil || s.StartsOn.IsZero() || s.EndsOn.IsZero() {
		return out
	}
	loc := s.StartsOn.Location()
	start, end := dayOf(s.StartsOn), dayOf(s.EndsOn.In(loc))
	if end.Before(start) {
		end = start
	}
	events := append([]Event(nil), in.Events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })

	// 第一遍：决定单位
	if out.Unit == "" {
		probe := newBurnState()
		hasPoints := false
		for _, e := range events {
			probe.apply(s.ID, e)
			if probe.hasPoints() {
				hasPoints = true
				break
			}
		}
		out.Unit = "tasks"
		if hasPoints {
			out.Unit = "points"
		}
	}

	// 实际线终点
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	last := dayOf(now.In(loc))
	if s.Status == SprintClosed && s.ClosedAt != nil {
		last = dayOf(s.ClosedAt.In(loc))
	}
	if last.After(end) {
		last = end
	}

	// 承诺范围
	scopeAt := start
	if s.StartedAt != nil && s.StartedAt.After(start) {
		scopeAt = *s.StartedAt
	}
	sc := newBurnState()
	for _, e := range events {
		if !e.At.Before(scopeAt) {
			break
		}
		sc.apply(s.ID, e)
	}
	scope := sc.remaining(out.Unit)

	// 第二遍：回放
	st := newBurnState()
	i := 0
	for ; i < len(events) && events[i].At.In(loc).Before(start); i++ {
		st.apply(s.ID, events[i])
	}
	for day := start; !day.After(last); day = day.AddDate(0, 0, 1) {
		next := day.AddDate(0, 0, 1)
		for ; i < len(events) && events[i].At.In(loc).Before(next); i++ {
			st.apply(s.ID, events[i])
		}
		out.Actual = append(out.Actual, BurndownPoint{Date: day.Format("2006-01-02"), Value: st.remaining(out.Unit)})
	}
	if scope == 0 {
		for _, p := range out.Actual {
			if p.Value > 0 {
				scope = p.Value
				break
			}
		}
	}

	// 理想线
	days := int(end.Sub(start).Hours()/24 + 0.5)
	for k := 0; k <= days; k++ {
		v := 0
		if days > 0 {
			v = int(float64(scope)*float64(days-k)/float64(days) + 0.5)
		}
		out.Ideal = append(out.Ideal, BurndownPoint{Date: start.AddDate(0, 0, k).Format("2006-01-02"), Value: v})
	}
	return out
}

// ---------- 迭代速度 ----------

// VelocitySample 是一个已结束迭代的完成量。
type VelocitySample struct {
	SprintID   string `json:"id"`
	Name       string `json:"name"`
	PointsDone int    `json:"points_done"`
	TasksDone  int    `json:"tasks_done"`
}

// AverageVelocity 计算最近几个已结束迭代平均完成的工作量；没有样本时为 0。
func AverageVelocity(samples []VelocitySample) float64 {
	if len(samples) == 0 {
		return 0
	}
	sum := 0
	for _, s := range samples {
		sum += s.PointsDone
	}
	return float64(sum) / float64(len(samples))
}

// ---------- 看板 ----------

// Move 是看板上一次可行的拖动：把卡片拖到某个状态要触发的步骤。
type Move struct {
	To         string `json:"to"`         // 状态名
	Transition string `json:"transition"` // 步骤名
}

// Moves 列出触发者现在能把任务拖到哪些状态（按内核 Available 计算）。
// 评论与结果类前提视为可在拖动时补上，不作为不可用原因。
func Moves(c *Context, actor *Executor) []Move {
	var out []Move
	seen := map[string]bool{}
	for _, av := range Available(c, actor, Payload{Comment: "x", Result: map[string]any{}}) {
		if !av.Available {
			continue
		}
		to := av.Transition.To
		if to == "$previous" {
			to = c.Task.PreviousState
		}
		if to == "" || seen[to] {
			continue
		}
		seen[to] = true
		out = append(out, Move{To: to, Transition: av.Transition.Name})
	}
	return out
}
