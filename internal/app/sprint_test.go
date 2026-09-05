package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// newTestOrg 建一个组织：甲（admin，组织负责人）、乙（developer）。
func newTestOrg(t *testing.T, a *App, ctx context.Context) (orgID string, jia, yi *Session) {
	t.Helper()
	slug := "s-" + store.NewID("x")[2:10]
	org, err := a.Store.CreateOrganization(ctx, a.Store.Pool, slug, "迭代测试组织", "CNY")
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := HashPassword("x")
	var owner, dev *domain.Member
	if err := a.Store.WithOrg(ctx, org.ID, func(tx pgx.Tx) error {
		acc1, _ := a.Store.CreateAccount(ctx, tx, slug+"-a@t.local", pw, "甲")
		acc2, _ := a.Store.CreateAccount(ctx, tx, slug+"-b@t.local", pw, "乙")
		owner, _ = a.Store.CreateMember(ctx, tx, org.ID, acc1.ID, "甲", []string{"admin"})
		dev, _ = a.Store.CreateMember(ctx, tx, org.ID, acc2.ID, "乙", []string{"developer"})
		return a.Store.SetOrganizationOwner(ctx, tx, org.ID, owner.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureOrgDefaults(ctx, org.ID); err != nil {
		t.Fatal(err)
	}
	jia = sessionOfMember(ctx, t, a, org.ID, owner.ID)
	jia.IsOwner, jia.Permissions = true, map[string]bool{"manage_workflows": true, "org_settings": true, "cancel_any_task": true}
	yi = sessionOfMember(ctx, t, a, org.ID, dev.ID)
	yi.Permissions = map[string]bool{}
	return org.ID, jia, yi
}

func dayp(d int) *time.Time {
	t := time.Now().AddDate(0, 0, d).Truncate(24 * time.Hour)
	return &t
}

// 迭代全程：创建 → 加任务 → 开始 → 第二个进行中被拒 → 完成一个任务 → 燃尽 → 结束并转入下一个迭代 → 动态齐全。
func TestSprintLifecycle(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)

	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(title string, points int) *domain.Task {
		task, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: title, AssigneeID: yi.MemberID, Points: &points, Ready: true})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	t1, t2, t3 := mk("任务一", 5), mk("任务二", 3), mk("任务三", 8)

	sp, err := a.CreateSprint(ctx, jia, CreateSprintInput{Name: "9 月第 1 迭代", Goal: "把三个任务做完", StartsOn: dayp(-1), EndsOn: dayp(6)})
	if err != nil {
		t.Fatal(err)
	}
	if sp.Status != domain.SprintPlanning {
		t.Fatalf("新迭代应为规划中，实际 %s", sp.Status)
	}
	if _, err := a.CreateSprint(ctx, jia, CreateSprintInput{Name: "坏日期", StartsOn: dayp(3), EndsOn: dayp(1)}); err == nil || !strings.Contains(err.Error(), "结束日期") {
		t.Fatalf("结束早于开始应被拒并说明，实际 %v", err)
	}
	n, err := a.AddTasksToSprint(ctx, jia, sp.ID, []string{t1.ID, t2.ID, t3.ID})
	if err != nil || n != 3 {
		t.Fatalf("加入三个任务应成功，实际 n=%d err=%v", n, err)
	}
	if n, _ := a.AddTasksToSprint(ctx, jia, sp.ID, []string{t1.ID}); n != 0 {
		t.Fatalf("重复加入应计 0，实际 %d", n)
	}

	// 权限：乙没有管理流程权限；Agent 需要人确认
	if _, err := a.StartSprint(ctx, yi, sp.ID); err == nil || !strings.Contains(err.Error(), "管理流程") {
		t.Fatalf("无权限开始应被拒并说明，实际 %v", err)
	}
	_, token, err := a.RegisterAgent(ctx, jia, RegisterAgentInput{Name: "甲的 Agent", Grants: map[domain.Grant]domain.GrantMode{domain.GrantManageWorkflows: domain.GrantWithApproval}})
	if err != nil {
		t.Fatal(err)
	}
	agSess, err := a.SessionFromAgentToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	// Agent 开始迭代必须经人确认：转成一条待确认操作，迭代本身不动（ADR 0003）
	_, err = a.StartSprint(ctx, agSess, sp.ID)
	pp, ok := AsProposalPending(err)
	if !ok {
		t.Fatalf("Agent 开始迭代应转成待确认操作，实际 %v", err)
	}
	if pp.Proposal.Action != ActionSprintStart || pp.Proposal.Status != domain.ProposalPending {
		t.Fatalf("待确认操作不对：%+v", pp.Proposal.Proposal)
	}
	if again, err := a.ListSprints(ctx, jia, store.SprintFilter{Status: domain.SprintPlanning}); err != nil || len(again) == 0 {
		t.Fatalf("待确认期间迭代应还在规划中，实际 %v %v", again, err)
	}
	if _, err := a.RejectProposal(ctx, jia, pp.Proposal.ID, "这个迭代由我自己来开始。"); err != nil {
		t.Fatal(err)
	}

	if _, err := a.StartSprint(ctx, jia, sp.ID); err != nil {
		t.Fatal(err)
	}
	sp2, err := a.CreateSprint(ctx, jia, CreateSprintInput{Name: "9 月第 2 迭代", StartsOn: dayp(7), EndsOn: dayp(14)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.StartSprint(ctx, jia, sp2.ID)
	if err == nil {
		t.Fatal("同一范围已有进行中的迭代，第二个开始应被拒")
	}
	var ue *UserError
	if ue, _ = err.(*UserError); ue == nil || ue.Status != 409 || !strings.Contains(err.Error(), "已经有一个进行中的迭代「9 月第 1 迭代」") {
		t.Fatalf("拒绝应为 409 且理由是完整中文句子，实际 %v", err)
	}

	// 完成任务一；给任务二改工作量
	for _, step := range []struct {
		s    *Session
		name string
	}{{yi, "start"}, {yi, "submit"}, {jia, "accept"}} {
		if step.name == "submit" {
			if _, err := a.AddArtifact(ctx, yi, t1.ID, domain.Artifact{Type: "result", Title: "结果"}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := a.Transition(ctx, step.s, t1.ID, step.name, TransitionPayload{}); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
	}
	p := 2
	if _, err := a.UpdateTask(ctx, jia, t2.ID, UpdateTaskInput{Points: &p, SetPoints: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTask(ctx, jia, t2.ID, UpdateTaskInput{Points: &p, SetPoints: true}); err != nil {
		t.Fatal(err)
	}

	d, err := a.GetSprint(ctx, jia, sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Stats.TaskCount != 3 || d.Stats.PointsTotal != 15 || d.Stats.PointsDone != 5 {
		t.Fatalf("汇总应为 3 个任务、15 点、完成 5 点，实际 %+v", d.Stats)
	}
	if d.Burndown.Unit != "points" || len(d.Burndown.Actual) == 0 || d.Burndown.Actual[len(d.Burndown.Actual)-1].Value != 10 {
		t.Fatalf("燃尽图今天剩余应为 10 点，实际 %+v", d.Burndown)
	}
	if len(d.Burndown.Ideal) != 8 || d.Burndown.Ideal[0].Value != 16 || d.Burndown.Ideal[7].Value != 0 {
		t.Fatalf("理想线应从开始时的 16 点降到 0 共 8 天，实际 %+v", d.Burndown.Ideal)
	}

	// 结束：未完成的转入下一个迭代
	if _, err := a.CloseSprint(ctx, jia, sp.ID, "next", ""); err == nil {
		t.Fatal("选 next 却不给下一个迭代应被拒")
	}
	res, err := a.CloseSprint(ctx, jia, sp.ID, "next", sp2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Moved != 2 || res.Returned != 0 || res.Sprint.Status != domain.SprintClosed {
		t.Fatalf("应转入 2 个、退回 0 个并结束，实际 %+v", res)
	}
	for _, id := range []string{t2.ID, t3.ID} {
		task, _ := a.GetTask(ctx, jia, id)
		if task.SprintID != sp2.ID {
			t.Fatalf("未完成任务应在下一个迭代里，实际 %q", task.SprintID)
		}
	}
	if task, _ := a.GetTask(ctx, jia, t1.ID); task.SprintID != sp.ID {
		t.Fatal("已完成任务应留在原迭代")
	}
	if _, err := a.AddTasksToSprint(ctx, jia, sp.ID, []string{t2.ID}); err == nil || !strings.Contains(err.Error(), "已经结束") {
		t.Fatalf("向已结束迭代加任务应被拒，实际 %v", err)
	}
	if _, err := a.StartSprint(ctx, jia, sp2.ID); err != nil {
		t.Fatalf("上一个结束后下一个应能开始：%v", err)
	}

	// 迭代速度
	v, err := a.Velocity(ctx, jia, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Sprints) != 1 || v.Sprints[0].PointsDone != 5 || v.AveragePoints != 5 {
		t.Fatalf("速度应为 1 个样本、5 点，实际 %+v", v)
	}

	// 动态齐全
	events, err := a.Events(ctx, jia, "", 500)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, e := range events {
		kinds[e.Type]++
	}
	for _, k := range []string{"SprintCreated", "SprintStarted", "SprintClosed", "TaskAddedToSprint", "TaskRemovedFromSprint", "PointsChanged"} {
		if kinds[k] == 0 {
			t.Errorf("应有 %s 动态，实际 %v", k, kinds)
		}
	}
	if kinds["PointsChanged"] != 1 {
		t.Errorf("同值修改不应再记 PointsChanged，实际 %d 条", kinds["PointsChanged"])
	}
	if kinds["TaskRemovedFromSprint"] != 2 || kinds["TaskAddedToSprint"] != 5 {
		t.Errorf("结束时应逐个产生移出 / 加入动态：移出 %d、加入 %d", kinds["TaskRemovedFromSprint"], kinds["TaskAddedToSprint"])
	}
}

// 看板：列来自类型的流程状态；不指定类型时是五种状态类型；可拖到哪里由内核按当前登录者算。
func TestBoardColumns(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	goal, _ := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "看板任务", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, TypeName: "bug", Title: "一个 Bug"}); err != nil {
		t.Fatal(err)
	}

	b, err := a.Board(ctx, yi, BoardFilter{TypeName: "generic"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range b.Columns {
		names = append(names, c.State.Name)
	}
	if strings.Join(names, ",") != "draft,todo,in_progress,waiting,blocked,submitted,done,cancelled" {
		t.Fatalf("通用任务的列应按流程状态顺序，实际 %v", names)
	}
	var card *BoardCard
	for i := range b.Columns {
		if b.Columns[i].State.Name == "todo" {
			if b.Columns[i].Count != 1 {
				t.Fatalf("待办列应有 1 张卡片，实际 %d", b.Columns[i].Count)
			}
			card = &b.Columns[i].Cards[0]
		}
	}
	if card == nil || card.ID != task.ID {
		t.Fatal("卡片应是那个任务")
	}
	moves := strings.Join(card.CanMoveTo, ",")
	if !strings.Contains(moves, "in_progress") || strings.Contains(moves, "cancelled") {
		t.Fatalf("乙作为负责人应能拖到进行中、不能取消，实际 %s", moves)
	}
	b2, _ := a.Board(ctx, jia, BoardFilter{TypeName: "generic"})
	for _, c := range b2.Columns {
		if c.State.Name == "todo" && !strings.Contains(strings.Join(c.Cards[0].CanMoveTo, ","), "cancelled") {
			t.Fatal("甲作为创建者应能拖到已取消")
		}
	}

	all, err := a.Board(ctx, jia, BoardFilter{Lane: "goal"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Columns) != 5 || all.Columns[0].State.Name != "pending" || all.Columns[0].Count != 2 {
		t.Fatalf("混合类型应为五列且未开始列有 2 张卡，实际 %+v", all.Columns)
	}
	if len(all.Lanes) != 1 || all.Lanes[0].Key != goal.ID {
		t.Fatalf("按目标分泳道应有 1 条，实际 %+v", all.Lanes)
	}
	if _, err := a.Board(ctx, jia, BoardFilter{Lane: "team"}); err == nil {
		t.Fatal("不支持的泳道应被拒")
	}
}
