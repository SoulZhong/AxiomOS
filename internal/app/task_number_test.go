package app

import (
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 任务序号：组织内从 1 递增、组织之间互不影响；「#n」「n」都能换成 ID，不存在的序号给整句理由。
func TestTaskNumbers(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)
	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "序号目标"})
	if err != nil {
		t.Fatal(err)
	}
	t1, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "第一个"})
	if err != nil {
		t.Fatal(err)
	}
	t2, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "第二个"})
	if err != nil {
		t.Fatal(err)
	}
	if t1.Number != 1 || t2.Number != 2 {
		t.Fatalf("新组织的任务应从 1 递增，实际 %d、%d", t1.Number, t2.Number)
	}
	// 另一个组织从 1 开始
	_, jia2, _ := newTestOrg(t, a, ctx)
	o1, err := a.CreateTask(ctx, jia2, CreateTaskInput{Title: "别的组织"})
	if err != nil {
		t.Fatal(err)
	}
	if o1.Number != 1 {
		t.Fatalf("别的组织应从 1 开始，实际 %d", o1.Number)
	}
	for _, ref := range []string{"#2", "2", " #2 "} {
		id, err := a.ResolveTaskRef(ctx, jia, ref)
		if err != nil || id != t2.ID {
			t.Fatalf("%q 应解析成 %s，实际 %q（%v）", ref, t2.ID, id, err)
		}
	}
	if id, err := a.ResolveTaskRef(ctx, jia, t1.ID); err != nil || id != t1.ID {
		t.Fatalf("ID 应原样返回，实际 %q（%v）", id, err)
	}
	if _, err := a.ResolveTaskRef(ctx, jia, "#99"); err == nil || !strings.Contains(err.Error(), i18n.Trf(i18n.ZhCN, "err.task_number", "99")) {
		t.Fatalf("不存在的序号应给整句理由，实际 %v", err)
	}
	// 组织之间不串号：甲的组织里没有第 3 号，乙的组织的 1 号不是甲的
	if id, _ := a.ResolveTaskRef(ctx, jia2, "#1"); id != o1.ID {
		t.Fatalf("别的组织的 #1 应是它自己的任务，实际 %s", id)
	}
	sums, err := a.ListTaskSummaries(ctx, jia, store.TaskFilter{GoalID: goal.ID})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, s := range sums {
		seen[s.Number] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("列表摘要应带序号，实际 %+v", seen)
	}
	d, err := a.GetTaskDetail(ctx, jia, t2.ID)
	if err != nil || d.Task.Number != 2 {
		t.Fatalf("详情应带序号 2，实际 %+v（%v）", d.Task.Number, err)
	}
	b, err := a.Brief(ctx, jia, t2.ID)
	if err != nil || b.Task.Number != 2 {
		t.Fatalf("任务说明应带序号 2（%v）", err)
	}
}
