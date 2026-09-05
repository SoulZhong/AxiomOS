package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

func day(d int) time.Time { return time.Date(2026, 9, 1+d, 0, 0, 0, 0, time.UTC) }
func at(d int, h int) time.Time {
	return time.Date(2026, 9, 1+d, h, 0, 0, 0, time.UTC)
}
func ip(n int) *int { return &n }

func TestValidateSprint(t *testing.T) {
	s := &Sprint{Name: "  ", StartsOn: day(5), EndsOn: day(2)}
	joined := ""
	for _, e := range ValidateSprint(s) {
		joined += e.Error() + "\n"
	}
	for _, want := range []string{"名称", "结束日期"} {
		if !strings.Contains(joined, want) {
			t.Errorf("校验应报告「%s」，实际：\n%s", want, joined)
		}
	}
	ok := &Sprint{Name: "9 月第 1 迭代", StartsOn: day(0), EndsOn: day(0)}
	if errs := ValidateSprint(ok); len(errs) != 0 {
		t.Fatalf("同一天开始结束应合法，实际 %v", errs)
	}
}

func TestValidateWIPLimit(t *testing.T) {
	tt := BuiltinTaskTypes()[0]
	tt.Workflow.States[2].WIPLimit = -1
	errs := Validate(tt)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "在制品上限") {
		t.Fatalf("负的在制品上限应被拒，实际 %v", errs)
	}
	tt.Workflow.States[2].WIPLimit = 3
	if errs := Validate(tt); len(errs) != 0 {
		t.Fatalf("在制品上限 3 应合法，实际 %v", errs)
	}
}

// 场景 G：燃尽图由动态回放得出
func TestScenarioG_Burndown(t *testing.T) {
	sp := &Sprint{ID: "sp1", Name: "9 月第 1 迭代", StartsOn: day(0), EndsOn: day(4), Status: SprintActive}
	added := func(task string, d, h int, points any) Event {
		return Event{Type: "TaskAddedToSprint", TaskID: task, At: at(d, h), Data: map[string]any{"sprint_id": "sp1", "points": points}}
	}
	done := func(task string, d, h int) Event {
		return Event{Type: "TaskTransitioned", TaskID: task, At: at(d, h), Data: map[string]any{"to_label": "terminal_success", "from_label": "waiting"}}
	}
	events := []Event{
		added("A", -2, 10, float64(5)), // 规划期装入：5 + 3 + 8 = 16
		added("B", -1, 10, float64(3)),
		added("C", -1, 11, float64(8)),
		{Type: "TaskAddedToSprint", TaskID: "X", At: at(-1, 12), Data: map[string]any{"sprint_id": "other", "points": float64(99)}}, // 别的迭代
		done("A", 0, 15), // 第 1 天完成 A：16 → 11
		{Type: "PointsChanged", TaskID: "C", At: at(1, 9), Data: map[string]any{"points": float64(13)}}, // 第 2 天 C 改为 13：11 → 16
		added("D", 1, 10, float64(2)), // 第 2 天加 D：→ 18
		done("B", 2, 9),               // 第 3 天完成 B：→ 15
		{Type: "TaskRemovedFromSprint", TaskID: "D", At: at(2, 10), Data: map[string]any{"sprint_id": "sp1"}},                                // 移出 D：→ 13
		{Type: "TaskTransitioned", TaskID: "A", At: at(3, 9), Data: map[string]any{"to_label": "pending", "from_label": "terminal_success"}}, // 第 4 天重开 A：→ 18
	}
	b := ComputeBurndown(BurndownInput{Sprint: sp, Events: events, Now: at(3, 20)})
	if b.Unit != "points" {
		t.Fatalf("有工作量时单位应为 points，实际 %s", b.Unit)
	}
	wantActual := []int{11, 18, 13, 18}
	if len(b.Actual) != len(wantActual) {
		t.Fatalf("实际线应有 %d 个点（到今天为止），实际 %d：%+v", len(wantActual), len(b.Actual), b.Actual)
	}
	for i, v := range wantActual {
		if b.Actual[i].Value != v {
			t.Errorf("第 %d 天剩余应为 %d，实际 %d", i+1, v, b.Actual[i].Value)
		}
	}
	if b.Actual[0].Date != "2026-09-01" || b.Actual[3].Date != "2026-09-04" {
		t.Errorf("日期不对：%+v", b.Actual)
	}
	wantIdeal := []int{16, 12, 8, 4, 0}
	if len(b.Ideal) != len(wantIdeal) {
		t.Fatalf("理想线应覆盖整个迭代 %d 天，实际 %d", len(wantIdeal), len(b.Ideal))
	}
	for i, v := range wantIdeal {
		if b.Ideal[i].Value != v {
			t.Errorf("理想线第 %d 点应为 %d，实际 %d", i+1, v, b.Ideal[i].Value)
		}
	}

	// 无工作量时按任务数
	plain := []Event{added("A", -1, 10, nil), added("B", -1, 10, nil), done("A", 0, 12)}
	b2 := ComputeBurndown(BurndownInput{Sprint: sp, Events: plain, Now: at(0, 20)})
	if b2.Unit != "tasks" || len(b2.Actual) != 1 || b2.Actual[0].Value != 1 || b2.Ideal[0].Value != 2 {
		t.Fatalf("无工作量应按任务数：%+v", b2)
	}

	// 承诺范围取点"开始"那一刻的剩余量：开始后才装入的不算
	startedAt := at(0, 12)
	late := &Sprint{ID: "sp1", StartsOn: day(0), EndsOn: day(4), Status: SprintActive, StartedAt: &startedAt}
	b4 := ComputeBurndown(BurndownInput{Sprint: late, Events: []Event{added("A", 0, 10, float64(5)), added("B", 0, 14, float64(3))}, Now: at(0, 20)})
	if b4.Ideal[0].Value != 5 || b4.Actual[0].Value != 8 {
		t.Fatalf("理想线起点应为开始时刻的 5 点，实际线第一天 8 点，实际 %+v / %+v", b4.Ideal[0], b4.Actual[0])
	}

	// 已结束的迭代：实际线止于结束当天
	closedAt := at(2, 18)
	closed := &Sprint{ID: "sp1", StartsOn: day(0), EndsOn: day(4), Status: SprintClosed, ClosedAt: &closedAt}
	b3 := ComputeBurndown(BurndownInput{Sprint: closed, Events: events, Now: at(30, 0)})
	if len(b3.Actual) != 3 {
		t.Fatalf("已结束迭代的实际线应止于结束日（3 个点），实际 %d", len(b3.Actual))
	}
}

func TestSummarizeAndVelocity(t *testing.T) {
	tasks := []*Task{{ID: "a", Points: ip(5), State: "done"}, {ID: "b", Points: ip(3), State: "todo"}, {ID: "c", State: "done"}}
	st := SummarizeSprint(tasks, func(t *Task) bool { return t.State == "done" })
	if st.TaskCount != 3 || st.PointsTotal != 8 || st.PointsDone != 5 || st.TasksDone != 2 {
		t.Fatalf("汇总不对：%+v", st)
	}
	if v := AverageVelocity([]VelocitySample{{PointsDone: 10}, {PointsDone: 20}, {PointsDone: 15}}); v != 15 {
		t.Fatalf("迭代速度应为 15，实际 %v", v)
	}
	if v := AverageVelocity(nil); v != 0 {
		t.Fatalf("无样本时应为 0，实际 %v", v)
	}
}

// 看板拖动：可拖到的状态由内核按触发者计算
func TestMovesFollowKernel(t *testing.T) {
	w := newWorld(t)
	task := w.create("generic", "写周报", "wang", nil)
	must(t, w.assign(task.ID, "wang", "li"), "指派")
	must(t, w.do(task.ID, "wang", "ready", ""), "就绪")
	names := func(ms []Move) string {
		var s []string
		for _, m := range ms {
			s = append(s, m.To+"<-"+m.Transition)
		}
		return strings.Join(s, " ")
	}
	got := names(Moves(w.ctx(task.ID, "li"), w.actors["li"]))
	if !strings.Contains(got, "in_progress<-start") || !strings.Contains(got, "blocked<-block") || strings.Contains(got, "cancelled") {
		t.Fatalf("小李应能拖到进行中与已阻塞、不能取消，实际 %s", got)
	}
	got = names(Moves(w.ctx(task.ID, "zhang"), w.actors["zhang"]))
	if got != "" {
		t.Fatalf("小张与任务无关，不应能拖动，实际 %s", got)
	}
	// 状态类型名称可翻译
	if SprintStatusTitle(SprintActive, i18n.EnUS) != "Active" || SprintStatusTitle(SprintPlanning, i18n.ZhCN) != "规划中" {
		t.Fatal("迭代状态名称翻译不对")
	}
}
