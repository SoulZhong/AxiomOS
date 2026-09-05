package domain

import (
	"testing"
	"time"
)

func mday(d int) time.Time {
	return time.Date(2026, 9, 5, 0, 0, 0, 0, time.Local).AddDate(0, 0, d)
}

func TestMilestoneStatus(t *testing.T) {
	today := mday(0)
	past := mday(-1)
	cases := []struct {
		name string
		m    *Milestone
		want MilestoneStatus
	}{
		{"未来", &Milestone{DueOn: mday(3)}, MilestoneUpcoming},
		{"当天仍算未到", &Milestone{DueOn: mday(0)}, MilestoneUpcoming},
		{"过期未确认", &Milestone{DueOn: mday(-1)}, MilestoneOverdue},
		{"过期但已确认", &Milestone{DueOn: mday(-1), ReachedAt: &past}, MilestoneReached},
		{"提前确认", &Milestone{DueOn: mday(5), ReachedAt: &past}, MilestoneReached},
	}
	for _, c := range cases {
		if got := StatusOfMilestone(c.m, today); got != c.want {
			t.Errorf("%s: 应为 %s，实际 %s", c.name, c.want, got)
		}
	}
	// 「今天」带时分秒也按天比较
	if got := StatusOfMilestone(&Milestone{DueOn: mday(0)}, mday(0).Add(23*time.Hour)); got != MilestoneUpcoming {
		t.Errorf("当天晚上仍应为 upcoming，实际 %s", got)
	}
}

func TestValidateMilestone(t *testing.T) {
	if errs := ValidateMilestone(&Milestone{Title: "  ", DueOn: mday(1)}); len(errs) != 1 {
		t.Fatalf("空名称应报一条错误，实际 %v", errs)
	}
	if errs := ValidateMilestone(&Milestone{Title: "上线"}); len(errs) != 1 {
		t.Fatalf("缺日期应报一条错误，实际 %v", errs)
	}
	if errs := ValidateMilestone(&Milestone{Title: "上线", DueOn: mday(1)}); len(errs) != 0 {
		t.Fatalf("合法里程碑不应报错，实际 %v", errs)
	}
}

func TestSummarizeMilestones(t *testing.T) {
	today := mday(0)
	reached := mday(-4)
	ms := []*Milestone{
		{ID: "c", Title: "第三", DueOn: mday(20), CreatedAt: mday(-10)},
		{ID: "a", Title: "第一", DueOn: mday(-5), ReachedAt: &reached, CreatedAt: mday(-10)},
		{ID: "b", Title: "逾期", DueOn: mday(-2), CreatedAt: mday(-10)},
		{ID: "d", Title: "第二", DueOn: mday(10), CreatedAt: mday(-10)},
	}
	s := SummarizeMilestones(ms, today)
	if s.Total != 4 || s.Reached != 1 || s.Overdue != 1 {
		t.Fatalf("汇总不对：%+v", s)
	}
	if s.Next == nil || s.Next.ID != "d" {
		t.Fatalf("下一个应是 10 天后的「第二」，实际 %+v", s.Next)
	}
	sorted := SortMilestones(ms)
	if sorted[0].ID != "a" || sorted[1].ID != "b" || sorted[2].ID != "d" || sorted[3].ID != "c" {
		t.Fatalf("应按日期升序，实际 %s %s %s %s", sorted[0].ID, sorted[1].ID, sorted[2].ID, sorted[3].ID)
	}
	if s := SummarizeMilestones(nil, today); s.Total != 0 || s.Next != nil {
		t.Fatalf("空集合的摘要应为零值，实际 %+v", s)
	}
	// 只剩逾期的：没有「下一个」
	if s := SummarizeMilestones([]*Milestone{{ID: "x", DueOn: mday(-1)}}, today); s.Next != nil || s.Overdue != 1 {
		t.Fatalf("只剩逾期时不应有下一个，实际 %+v", s)
	}
}

func TestMilestoneReady(t *testing.T) {
	labels := map[string]Label{}
	labelOf := func(t *Task) Label { return labels[t.ID] }
	pe := func(d int) *time.Time { x := mday(d); return &x }
	m := &Milestone{DueOn: mday(10)}

	// 没有任何计划结束在日期前的任务：不提示
	if MilestoneReady(m, []*Task{{ID: "later", PlannedEnd: pe(15)}}, labelOf) {
		t.Fatal("没有相关任务时不应提示")
	}
	// 有一个未完成的：不提示
	tasks := []*Task{{ID: "t1", PlannedEnd: pe(5)}, {ID: "t2", PlannedEnd: pe(10)}, {ID: "later", PlannedEnd: pe(15)}, {ID: "nodate"}}
	labels["t1"], labels["t2"], labels["later"] = LabelTerminalSuccess, LabelActive, LabelPending
	if MilestoneReady(m, tasks, labelOf) {
		t.Fatal("还有进行中的任务时不应提示")
	}
	// 全部完成（含当天结束的）：提示
	labels["t2"] = LabelTerminalSuccess
	if !MilestoneReady(m, tasks, labelOf) {
		t.Fatal("日期前的任务都完成后应提示")
	}
	// 已终止的任务不算：仍提示
	tasks = append(tasks, &Task{ID: "cancelled", PlannedEnd: pe(3)})
	labels["cancelled"] = LabelTerminalFailure
	if !MilestoneReady(m, tasks, labelOf) {
		t.Fatal("已终止的任务不应拦住提示")
	}
	// 只有已终止的任务：没有「至少一个完成」，不提示
	if MilestoneReady(m, []*Task{{ID: "cancelled", PlannedEnd: pe(3)}}, labelOf) {
		t.Fatal("只有已终止任务时不应提示")
	}
	// 已确认的不再提示
	now := mday(0)
	if MilestoneReady(&Milestone{DueOn: mday(10), ReachedAt: &now}, tasks, labelOf) {
		t.Fatal("已确认的里程碑不应再提示")
	}
}
