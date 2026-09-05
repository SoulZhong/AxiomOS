package domain

import (
	"sort"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// Milestone 是目标上的一个有日期的节点：到这一天应该达到什么（ADR 0016）。
// 它不是任务：没有执行者、没有流程、没有执行记录与成本。「已达到」由能编辑该目标的人确认，
// 系统不自动判定。
type Milestone struct {
	ID          string     `json:"id"`
	OrgID       string     `json:"org_id"`
	GoalID      string     `json:"goal_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	DueOn       time.Time  `json:"due_on"` // 日期（当地零点）
	ReachedAt   *time.Time `json:"reached_at,omitempty"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// MilestoneStatus 是里程碑的状态，由日期与 reached_at 算出，不落库。
type MilestoneStatus string

const (
	// MilestoneUpcoming 还没到日期（或今天就是日期），也还没确认。
	MilestoneUpcoming MilestoneStatus = "upcoming"
	// MilestoneReached 已由人确认达到。
	MilestoneReached MilestoneStatus = "reached"
	// MilestoneOverdue 日期已过还没确认。
	MilestoneOverdue MilestoneStatus = "overdue"
)

// Reached 判断是否已确认达到。
func (m *Milestone) Reached() bool { return m != nil && m.ReachedAt != nil }

// StatusOfMilestone 按「今天」算里程碑的状态：已确认 → reached；日期早于今天 → overdue；否则 upcoming。
// 日期当天仍算 upcoming，因为当天还来得及确认。
func StatusOfMilestone(m *Milestone, today time.Time) MilestoneStatus {
	if m.Reached() {
		return MilestoneReached
	}
	if dayOf(m.DueOn).Before(dayOf(today)) {
		return MilestoneOverdue
	}
	return MilestoneUpcoming
}

// ValidateMilestone 校验一个里程碑：名称非空、必须有日期。
func ValidateMilestone(m *Milestone) []error {
	var errs []error
	add := func(key string, a ...any) { errs = append(errs, &ValidationError{Msg: i18n.M(key, a...)}) }
	if m == nil {
		add("val.milestone_no_title")
		return errs
	}
	if strings.TrimSpace(m.Title) == "" {
		add("val.milestone_no_title")
	}
	if m.DueOn.IsZero() {
		add("val.milestone_no_date")
	}
	return errs
}

// MilestoneNext 是摘要里的「下一个里程碑」。
type MilestoneNext struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	DueOn time.Time `json:"due_on"`
}

// MilestoneSummary 是一个目标的里程碑摘要：总数、已达到数、逾期数、下一个还没到日期的里程碑。
type MilestoneSummary struct {
	Total   int            `json:"total"`
	Reached int            `json:"reached"`
	Overdue int            `json:"overdue"`
	Next    *MilestoneNext `json:"next"`
}

// SummarizeMilestones 是纯函数：按今天汇总一组里程碑。Next 是日期最早、还没确认、日期不早于今天的那一个；
// 逾期的不算「下一个」，它们已经在 Overdue 里提醒了。
func SummarizeMilestones(ms []*Milestone, today time.Time) MilestoneSummary {
	s := MilestoneSummary{Total: len(ms)}
	sorted := SortMilestones(ms)
	for _, m := range sorted {
		switch StatusOfMilestone(m, today) {
		case MilestoneReached:
			s.Reached++
		case MilestoneOverdue:
			s.Overdue++
		case MilestoneUpcoming:
			if s.Next == nil {
				s.Next = &MilestoneNext{ID: m.ID, Title: m.Title, DueOn: m.DueOn}
			}
		}
	}
	return s
}

// SortMilestones 返回按日期升序（同日按创建时间）的副本。
func SortMilestones(ms []*Milestone) []*Milestone {
	out := make([]*Milestone, len(ms))
	copy(out, ms)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].DueOn.Equal(out[j].DueOn) {
			return out[i].DueOn.Before(out[j].DueOn)
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// MilestoneReady 判断一个里程碑「可以确认了」：目标子树里所有计划结束不晚于该日期的任务都已完成，
// 且至少有一个这样的任务。labelOf 给出任务的状态类型（内核只认类型，不认状态名）。
// 已终止（terminal_failure）的任务不再算在内：它们不会再完成，也不该拦住提示。已确认的里程碑不再提示。
func MilestoneReady(m *Milestone, tasks []*Task, labelOf func(*Task) Label) bool {
	if m.Reached() {
		return false
	}
	due := dayOf(m.DueOn)
	n := 0
	for _, t := range tasks {
		if t.PlannedEnd == nil || dayOf(*t.PlannedEnd).After(due) {
			continue
		}
		switch labelOf(t) {
		case LabelTerminalFailure:
			continue
		case LabelTerminalSuccess:
			n++
		default:
			return false
		}
	}
	return n > 0
}
