package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// Agent 能力补齐第二批（观测）：动态、统计、迭代的创建与修改、解除关联、通知、组织上下文。

// 动态按目标（含子目标）与时间起点筛，句子与网页一样；目标 / 任务 / 我的统计各算一遍。
func TestEventLinesAndMetrics(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	_, ag := agentSessionFor(t, a, ctx, yi, "干活 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect})
	budget := 100.0
	root, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "观测目标", Budget: &budget})
	if err != nil {
		t.Fatal(err)
	}
	child, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "观测子目标", ParentID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "别的目标"})
	if err != nil {
		t.Fatal(err)
	}
	yesterday := time.Now().AddDate(0, 0, -1)
	done, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: child.ID, Title: "做完的任务", AssigneeID: ag.Actor.ID, Ready: true, PlannedEnd: &yesterday})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: other.ID, Title: "别的任务", AssigneeID: yi.MemberID, Ready: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: root.ID, Title: "没人领的任务", Ready: true, PlannedEnd: &yesterday}); err != nil {
		t.Fatal(err)
	}
	// Agent 做完一次、被打回一次、再做完
	if _, err := a.Transition(ctx, ag, done.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Heartbeat(ctx, ag, done.ID, []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 1000, OutputTokens: 100}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddArtifact(ctx, ag, done.ID, domain.Artifact{Type: "result", Title: "结果"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, ag, done.ID, "submit", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jia, done.ID, "reject", TransitionPayload{Comment: "再改改"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, ag, done.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, ag, done.ID, "submit", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jia, done.ID, "accept", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}

	// 动态：按目标筛只见子树里的，句子里有人名与任务名
	lines, err := a.ListEventLines(ctx, ag, EventFilter{GoalID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("目标子树下应有动态")
	}
	for _, l := range lines {
		if l.TaskTitle == "别的任务" {
			t.Fatalf("别的目标的动态不该出现：%+v", l)
		}
		if l.Summary == "" {
			t.Fatalf("每条动态都要有句子：%+v", l)
		}
	}
	if !strings.Contains(lines[0].Summary, "甲") || !strings.Contains(lines[0].Summary, "做完的任务") {
		t.Fatalf("最新一条应是甲验收通过做完的任务，实际 %q", lines[0].Summary)
	}
	future := time.Now().Add(time.Hour)
	if lines, _ = a.ListEventLines(ctx, ag, EventFilter{Since: &future}); len(lines) != 0 {
		t.Fatalf("时间起点在将来应一条都没有，实际 %d", len(lines))
	}
	if lines, _ = a.ListEventLines(ctx, ag, EventFilter{TaskID: done.ID, Limit: 2}); len(lines) != 2 || lines[0].TaskNumber != done.Number {
		t.Fatalf("按任务筛并限制条数，实际 %+v", lines)
	}

	// 目标统计：三个任务、一个完成、一个逾期且无人认领、成本对预算、成本归到 Agent
	gm, err := a.GoalMetricsOf(ctx, jia, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gm.SubGoals != 1 || gm.TasksTotal != 2 || gm.Tasks.Done != 1 || gm.Tasks.Overdue != 1 || gm.Tasks.NoOwner != 1 {
		t.Fatalf("目标统计不对：%+v", gm)
	}
	if !gm.Financial || gm.Cost <= 0 || gm.Budget == nil || *gm.Budget != 100 || gm.BudgetUsed == nil || len(gm.CostByExecutor) != 1 || gm.CostByExecutor[0].Key != ag.Actor.ID {
		t.Fatalf("成本对预算与归口不对：%+v", gm)
	}
	if gm.LeadHoursAvg <= 0 {
		t.Fatalf("已完成任务应有平均周期，实际 %v", gm.LeadHoursAvg)
	}

	// 任务统计：被打回一次、两段执行记录都完成、用量按模型；乙看不到这个范围的钱，成本为 0 且 financial 为假
	tm, err := a.TaskMetricsOf(ctx, ag, done.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tm.ReviewRounds != 1 || tm.Runs.Total != 2 || tm.Runs.Completed != 2 || tm.Artifacts != 1 || tm.Comments != 1 {
		t.Fatalf("任务统计不对：%+v", tm)
	}
	if tm.Financial || tm.Cost != 0 || tm.Tokens != 0 {
		t.Fatalf("看不到钱的人不该拿到成本：%+v", tm)
	}
	tm, err = a.TaskMetricsOf(ctx, jia, done.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tm.State.Label != domain.LabelTerminalSuccess || tm.Overdue || !tm.Financial || tm.Cost <= 0 || len(tm.TokensByModel) != 1 || tm.Tokens != 1100 {
		t.Fatalf("任务的状态与用量不对：%+v", tm)
	}

	// 我的统计（Agent）：两段执行记录、被打回一次、并发上限；自己的用量永远给，别人的钱（预算对照）看不到就是空
	mm, err := a.MyMetricsOf(ctx, ag)
	if err != nil {
		t.Fatal(err)
	}
	if mm.Runs.Total != 2 || mm.ReviewRounds != 1 || mm.MaxConcurrent == nil || *mm.MaxConcurrent != 3 || mm.ActiveRuns != 0 || mm.OpenTasks != 0 {
		t.Fatalf("我的统计不对：%+v", mm)
	}
	if mm.Tokens != 1100 || mm.Cost <= 0 || mm.Financial || len(mm.GoalBudgets) != 0 {
		t.Fatalf("我的成本与预算对照不对：%+v", mm)
	}
	// 甲的 Agent 做一段：甲看得到钱，预算对照里有观测目标
	_, jiaAg := agentSessionFor(t, a, ctx, jia, "甲的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect})
	work, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: child.ID, Title: "甲的 Agent 的任务", AssigneeID: jiaAg.Actor.ID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jiaAg, work.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Heartbeat(ctx, jiaAg, work.ID, []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 500}}); err != nil {
		t.Fatal(err)
	}
	mm, err = a.MyMetricsOf(ctx, jiaAg)
	if err != nil {
		t.Fatal(err)
	}
	if !mm.Financial || mm.ActiveRuns != 1 || mm.OpenTasks != 1 || mm.ActiveTasks != 1 || len(mm.GoalBudgets) != 1 || mm.GoalBudgets[0].GoalID != root.ID || mm.GoalBudgets[0].MyCost <= 0 || mm.GoalBudgets[0].Cost < mm.GoalBudgets[0].MyCost {
		t.Fatalf("甲的 Agent 的预算对照不对：%+v", mm)
	}
	// 甲是验收人：等甲验收的数
	if _, err := a.Transition(ctx, yi, mustTask(t, a, ctx, jia, other.ID), "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	hm, err := a.MyMetricsOf(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if hm.Kind != "member" || hm.MaxConcurrent != nil {
		t.Fatalf("成员的统计不带并发上限：%+v", hm)
	}
}

// mustTask 找到某个目标下的第一个任务。
func mustTask(t *testing.T, a *App, ctx interface{ Done() <-chan struct{} }, sess *Session, goalID string) string {
	t.Helper()
	list, err := a.ListTasks(ctxOf(ctx), sess, storeFilterGoal(goalID))
	if err != nil || len(list) == 0 {
		t.Fatalf("目标下应有任务：%v", err)
	}
	return list[0].ID
}

// 迭代的创建与修改按「创建任务」授权；解除关联按「建立关联」授权，前置关系一律待确认。
func TestAgentSprintPlanningAndUnlink(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	_, approval := agentSessionFor(t, a, ctx, yi, "需确认的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantCreateTask: domain.GrantWithApproval, domain.GrantLink: domain.GrantDirect})
	_, none := agentSessionFor(t, a, ctx, yi, "没授权的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect})

	if _, err := a.CreateSprint(ctx, none, CreateSprintInput{Name: "越权迭代", StartsOn: dayp(0), EndsOn: dayp(13)}); err == nil || !strings.Contains(err.Error(), "「创建任务」授权") {
		t.Fatalf("没授权创建迭代应说缺「创建任务」授权，实际 %v", err)
	}
	_, err := a.CreateSprint(ctx, approval, CreateSprintInput{Name: "Agent 的迭代", StartsOn: dayp(0), EndsOn: dayp(13)})
	pp := mustPending(t, err)
	if pp.Proposal.Action != ActionSprintCreate || !strings.Contains(pp.Proposal.SummaryText, "Agent 的迭代") {
		t.Fatalf("创建迭代应是 sprint.create，实际 %s %q", pp.Proposal.Action, pp.Proposal.SummaryText)
	}
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	sprints, err := a.ListSprints(ctx, yi, storeSprintFilter())
	if err != nil || len(sprints) != 1 || sprints[0].Name != "Agent 的迭代" {
		t.Fatalf("确认后迭代应建好，实际 %v %+v", err, sprints)
	}
	name := "改过名的迭代"
	_, err = a.UpdateSprint(ctx, approval, sprints[0].ID, UpdateSprintInput{Name: &name})
	if pp = mustPending(t, err); pp.Proposal.Action != ActionSprintUpdate {
		t.Fatalf("改迭代应是 sprint.update，实际 %s", pp.Proposal.Action)
	}
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	if sp, _ := a.GetSprint(ctx, yi, sprints[0].ID); sp.Name != name {
		t.Fatalf("确认后名字应改掉，实际 %q", sp.Name)
	}

	// 解除关联
	t1, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "前置", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	t2, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "后置", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Link(ctx, jia, t1.ID, domain.RelationBlocks, t2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Link(ctx, jia, t1.ID, domain.RelationRelatesTo, t2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Unlink(ctx, jia, t1.ID, domain.RelationFoundIn, t2.ID); err == nil || !strings.Contains(err.Error(), "没有这条关联") {
		t.Fatalf("解除不存在的关联应说没有这条关联，实际 %v", err)
	}
	if _, err := a.Unlink(ctx, none, t1.ID, domain.RelationRelatesTo, t2.ID); err == nil || !strings.Contains(err.Error(), "「建立关联」授权") {
		t.Fatalf("没授权应说缺「建立关联」授权，实际 %v", err)
	}
	// 授权直接生效：相关关系直接解；前置关系仍要人确认
	got, err := a.Unlink(ctx, approval, t1.ID, domain.RelationRelatesTo, t2.ID)
	if err != nil || len(got.Relations) != 1 {
		t.Fatalf("直接生效的授权解除相关关系应成功，实际 %v %+v", err, got)
	}
	_, err = a.Unlink(ctx, approval, t1.ID, domain.RelationBlocks, t2.ID)
	pp = mustPending(t, err)
	if pp.Proposal.Action != ActionTaskUnlink || !strings.Contains(pp.Proposal.SummaryText, "前置于") {
		t.Fatalf("解除前置关系应一律待确认，实际 %s %q", pp.Proposal.Action, pp.Proposal.SummaryText)
	}
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	if cur, _ := a.GetTask(ctx, jia, t1.ID); len(cur.Relations) != 0 {
		t.Fatalf("确认后前置关系应已解除，实际 %+v", cur.Relations)
	}
	// 人直接解；解掉之后再解一次是「没有这条关联」
	if _, err := a.Link(ctx, jia, t1.ID, domain.RelationBlocks, t2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Unlink(ctx, jia, t1.ID, domain.RelationBlocks, t2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Unlink(ctx, jia, t1.ID, domain.RelationBlocks, t2.ID); err == nil {
		t.Fatal("已解除的关联再解应被拒")
	}
}

// Agent 的通知：只看与自己有关的（挂在自己有份的任务上的），也只能标这些为已读；组织上下文只读。
func TestAgentNotificationsAndOrgContext(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	_, ag := agentSessionFor(t, a, ctx, yi, "看通知的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect, domain.GrantComment: domain.GrantDirect})
	mine, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "Agent 的任务", AssigneeID: ag.Actor.ID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	owners, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "乙自己的任务", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	// 甲在两个任务上各发一条评论：乙收到两条通知，Agent 只该看到自己任务上的那条
	if _, err := a.AddComment(ctx, jia, mine.ID, "Agent 的任务有评论", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddComment(ctx, jia, owners.ID, "乙的任务有评论", false); err != nil {
		t.Fatal(err)
	}
	all, err := a.MyNotificationLines(ctx, yi, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 2 {
		t.Fatalf("乙应至少有两条通知，实际 %+v", all)
	}
	got, err := a.MyNotificationLines(ctx, ag, true, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range got {
		if n.TaskID != mine.ID {
			t.Fatalf("Agent 只该看到自己任务上的通知，实际 %+v", n)
		}
	}
	if len(got) == 0 || got[0].TaskNumber != mine.Number {
		t.Fatalf("Agent 应看到自己任务上的通知并带序号，实际 %+v", got)
	}
	// 标已读：混进乙自己的通知 ID，只有 Agent 有份的那条会被标掉
	var ownersNote int64
	for _, n := range all {
		if n.TaskID == owners.ID {
			ownersNote = n.ID
		}
	}
	remaining, err := a.MarkMyNotificationsRead(ctx, ag, []int64{got[0].ID, ownersNote})
	if err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("Agent 有份的未读应清零，实际 %d", remaining)
	}
	after, _ := a.MyNotificationLines(ctx, yi, true, 50)
	found := false
	for _, n := range after {
		if n.ID == ownersNote {
			found = true
		}
	}
	if !found {
		t.Fatal("乙自己任务上的通知不该被 Agent 标成已读")
	}

	oc, err := a.OrgContextOf(ctx, ag)
	if err != nil {
		t.Fatal(err)
	}
	if oc.Organization == "" || oc.Currency != "CNY" || oc.Me.Kind != "agent" || oc.Me.Owner != "乙" || oc.Me.MaxConcurrent == nil || oc.Me.Grants["execute"] != "direct" {
		t.Fatalf("组织上下文里「我」不对：%+v", oc.Me)
	}
	if oc.Members != 2 || oc.Agents != 1 || len(oc.TaskTypes) == 0 || len(oc.Roles) == 0 || len(oc.Capabilities) == 0 {
		t.Fatalf("组织上下文不全：%+v", oc)
	}
	teams, err := a.ListTeamBriefs(ctx, ag)
	if err != nil {
		t.Fatal(err)
	}
	_ = teams
}

func ctxOf(c interface{ Done() <-chan struct{} }) context.Context { return c.(context.Context) }
func storeFilterGoal(goalID string) store.TaskFilter              { return store.TaskFilter{GoalID: goalID} }
func storeSprintFilter() store.SprintFilter                       { return store.SprintFilter{} }
