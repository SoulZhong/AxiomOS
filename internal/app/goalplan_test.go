package app

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// 目标方案（ADR 0026）：只有 Agent 能提、提交时把整份校验完、一条待确认操作、目标负责人拍板、
// 逐条勾选后一次落库（上级先于子任务、前置先于后置、跳过的引用一并去掉）、Agent 能查进展。
func TestGoalPlanProposal(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	// 第三个人丙做目标负责人，好区分「Agent 的所有者」与「目标负责人」
	var bingMember *domain.Member
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		pw, _ := HashPassword("x")
		acc, err := a.Store.CreateAccount(ctx, tx, "plan-"+store.NewID("x")[2:8]+"@t.local", pw, "丙")
		if err != nil {
			return err
		}
		bingMember, err = a.Store.CreateMember(ctx, tx, orgID, acc.ID, "丙", []string{"developer"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	bing := sessionOfMember(ctx, t, a, orgID, bingMember.ID)
	bing.Permissions = map[string]bool{}
	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "上线新版登录", OwnerMemberID: bingMember.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, ag := agentSessionFor(t, a, ctx, yi, "规划 Agent", map[domain.Grant]domain.GrantMode{
		domain.GrantCreateTask: domain.GrantDirect, domain.GrantCreateSubtask: domain.GrantDirect, domain.GrantCreateGoal: domain.GrantDirect, domain.GrantLink: domain.GrantDirect})
	plan := GoalPlanInput{GoalID: goal.ID, Rationale: "按登录链路拆", Tasks: []PlanTask{
		{Key: "design", Title: "出设计稿"},
		{Key: "api", Title: "写接口", DependsOn: []string{"design"}},
		{Key: "web", Title: "写页面", DependsOn: []string{"design", "api"}},
		{Key: "web-form", Title: "表单校验", ParentKey: "web"},
	}, Milestones: []PlanMilestone{{Title: "灰度上线", DueOn: dayp(20)}}}

	// 人不能提；没授权不能提；校验在提交时就做完
	if _, err := a.ProposeGoalPlan(ctx, yi, plan); err == nil || !strings.Contains(err.Error(), "只能由 Agent 提交") {
		t.Fatalf("人提方案应被拒，实际 %v", err)
	}
	_, none := agentSessionFor(t, a, ctx, yi, "没授权的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect})
	if _, err := a.ProposeGoalPlan(ctx, none, plan); err == nil || !strings.Contains(err.Error(), "「创建任务」授权") {
		t.Fatalf("没授权应说缺「创建任务」授权，实际 %v", err)
	}
	bad := plan
	bad.Tasks = []PlanTask{{Key: "a", Title: "甲", DependsOn: []string{"b"}}, {Key: "b", Title: "乙", DependsOn: []string{"a"}}}
	if _, err := a.ProposeGoalPlan(ctx, ag, bad); err == nil || !strings.Contains(err.Error(), "绕成了圈") {
		t.Fatalf("成环应被拒，实际 %v", err)
	}
	bad.Tasks = []PlanTask{{Key: "a", Title: "甲"}, {Key: "a", Title: "乙"}}
	if _, err := a.ProposeGoalPlan(ctx, ag, bad); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("键重复应被拒，实际 %v", err)
	}
	bad.Tasks = []PlanTask{{Key: "a", Title: "甲", ParentKey: "zzz"}}
	if _, err := a.ProposeGoalPlan(ctx, ag, bad); err == nil || !strings.Contains(err.Error(), "不存在的键") {
		t.Fatalf("引用不存在的键应被拒，实际 %v", err)
	}
	bad.Tasks = []PlanTask{{Key: "a", Title: "  "}}
	if _, err := a.ProposeGoalPlan(ctx, ag, bad); err == nil || !strings.Contains(err.Error(), "没有标题") {
		t.Fatalf("没标题应被拒，实际 %v", err)
	}
	bad.Tasks = nil
	if _, err := a.ProposeGoalPlan(ctx, ag, bad); err == nil || !strings.Contains(err.Error(), "至少要有一个任务") {
		t.Fatalf("空方案应被拒，实际 %v", err)
	}
	// 只看不做：说清会挂起等丙确认，一行不落
	if _, err := a.ProposeGoalPlan(ctx, ag.WithWrite(WriteOptions{DryRun: true}), plan); err == nil {
		t.Fatal("只看不做应返回预演结果")
	} else if d, ok := AsDryRun(err); !ok || !strings.Contains(d.Will, "丙") {
		t.Fatalf("预演应说等丙确认，实际 %v", err)
	}

	// 提交：一条待确认操作，确认人是目标负责人丙
	v, err := a.ProposeGoalPlan(ctx, ag, plan)
	if err != nil {
		t.Fatal(err)
	}
	if v.Action != ActionGoalPlan || v.Status != domain.ProposalPending || !strings.Contains(v.SummaryText, "4 个任务") || !strings.Contains(v.SummaryText, "1 个里程碑") {
		t.Fatalf("方案应是一条 goal.plan 待确认操作，实际 %+v %q", v.Action, v.SummaryText)
	}
	mine, err := a.ListProposals(ctx, bing, ProposalFilter{Mine: true})
	if err != nil || len(mine) != 1 || !mine[0].CanDecide {
		t.Fatalf("目标负责人丙应能拍板这条方案，实际 %v %+v", err, mine)
	}
	notes, _ := a.MyNotificationLines(ctx, bing, true, 10)
	if len(notes) == 0 {
		t.Fatal("目标负责人应收到通知")
	}
	// 拿到的方案能查进展
	st, err := a.GoalPlanStatusOf(ctx, ag, v.ID)
	if err != nil || st.Proposal.Status != domain.ProposalPending || st.Result != nil {
		t.Fatalf("进展应是等人确认，实际 %v %+v", err, st)
	}
	// 别的待确认操作不收选择
	other, _ := a.CreateTask(ctx, jia, CreateTaskInput{Title: "别的任务", AssigneeID: yi.MemberID, Ready: true})
	_, approvalAg := agentSessionFor(t, a, ctx, yi, "需确认的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantComment: domain.GrantWithApproval})
	_, err = a.AddComment(ctx, approvalAg, other.ID, "你好", false)
	pp := mustPending(t, err)
	if _, err := a.ApproveProposalWith(ctx, yi, pp.Proposal.ID, ApproveOptions{Skip: []string{"x"}}); err == nil || !strings.Contains(err.Error(), "只有目标方案") {
		t.Fatalf("普通待确认操作带选择应被拒，实际 %v", err)
	}
	// 跳过不在方案里的键；全部跳过
	if _, err := a.ApproveProposalWith(ctx, bing, v.ID, ApproveOptions{Skip: []string{"nope"}}); err == nil || !strings.Contains(err.Error(), "不在方案里") {
		t.Fatalf("跳过未知键应被拒，实际 %v", err)
	}
	if _, err := a.ApproveProposalWith(ctx, bing, v.ID, ApproveOptions{Skip: []string{"design", "api", "web", "web-form"}}); err == nil || !strings.Contains(err.Error(), "都被跳过") {
		t.Fatalf("全部跳过应被拒，实际 %v", err)
	}
	// 丙勾掉「写接口」再批准：其余三个任务与里程碑一次落库；写页面少了一条前置；表单校验挂在写页面下；都指派给 Agent
	res, err := a.ApproveProposalWith(ctx, bing, v.ID, ApproveOptions{Skip: []string{"api"}})
	if err != nil {
		t.Fatalf("批准失败：%v", err)
	}
	r, ok := res.Result.(*GoalPlanResult)
	if !ok || len(r.Tasks) != 3 || len(r.Milestones) != 1 || len(r.Skipped) != 1 || r.Skipped[0] != "api" {
		t.Fatalf("落库结果不对：%+v", res.Result)
	}
	byKey := map[string]*domain.Task{}
	for _, c := range r.Tasks {
		tk, err := a.GetTask(ctx, jia, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		byKey[c.Key] = tk
	}
	if byKey["design"].AssigneeID != ag.Actor.ID || byKey["design"].GoalID != goal.ID || byKey["design"].State != "todo" {
		t.Fatalf("任务应挂在目标下、指派给 Agent、直接就绪，实际 %+v", byKey["design"])
	}
	if byKey["web-form"].ParentID != byKey["web"].ID {
		t.Fatal("表单校验应是写页面的子任务")
	}
	if rels := byKey["design"].Relations; len(rels) != 1 || rels[0].Type != domain.RelationBlocks || rels[0].OtherID != byKey["web"].ID {
		t.Fatalf("出设计稿应只前置于写页面（写接口被跳过了），实际 %+v", rels)
	}
	if ms, _ := a.ListMilestones(ctx, jia, goal.ID); len(ms) != 1 || ms[0].Title != "灰度上线" {
		t.Fatalf("里程碑应建好，实际 %+v", ms)
	}
	applied := eventsOfType(t, a, ctx, orgID, "GoalPlanApplied")
	if len(applied) != 1 || applied[0].Data["proposal_id"] != v.ID || applied[0].Data["approved_by_name"] != "丙" {
		t.Fatalf("应有一条带确认人的 GoalPlanApplied 动态，实际 %+v", applied)
	}
	st, err = a.GoalPlanStatusOf(ctx, ag, v.ID)
	if err != nil || st.Proposal.Status != domain.ProposalApproved || st.Result == nil || len(st.Result.Tasks) != 3 || st.Result.Tasks[0].Number == 0 {
		t.Fatalf("进展应带落库结果，实际 %v %+v", err, st)
	}
	// 落库是幂等的：确认的第二个事务失败、人再点一次时，不能把整份方案再建一遍
	prop, err := a.GetProposal(ctx, jia, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := a.agentSession(ctx, orgID, ag.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	replay.Actor = withDirectGrants(replay.Actor)
	replay.ApprovedByID, replay.ApprovedByName = bing.MemberID, "丙"
	again, err := a.applyGoalPlan(ctx, replay, prop.Proposal, ApproveOptions{Skip: []string{"api"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Tasks) != 3 || again.Tasks[0].ID != r.Tasks[0].ID {
		t.Fatalf("重复落库应返回第一次的结果，实际 %+v", again)
	}
	if list, _ := a.ListTaskSummaries(ctx, jia, store.TaskFilter{GoalID: goal.ID}); len(list) != 3 {
		t.Fatalf("重复落库不该再建任务，实际 %d 个", len(list))
	}
	// 目标视图里能看到这些任务；改派给别人
	b, _ := a.GoalBriefDetail(ctx, ag, goal.ID)
	if len(b.Tasks) != 2 { // 顶层两个：出设计稿、写页面
		t.Fatalf("目标下应有两个顶层任务，实际 %+v", b.Tasks)
	}
	v2, err := a.ProposeGoalPlan(ctx, ag, GoalPlanInput{GoalID: goal.ID, Tasks: []PlanTask{{Title: "补测试"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApproveProposalWith(ctx, jia, v2.ID, ApproveOptions{AssigneeID: "mem_nope"}); err == nil {
		t.Fatal("改派给不存在的人应被拒")
	}
	res, err = a.ApproveProposalWith(ctx, jia, v2.ID, ApproveOptions{AssigneeID: yi.MemberID})
	if err != nil {
		t.Fatal(err)
	}
	if tk, _ := a.GetTask(ctx, jia, res.Result.(*GoalPlanResult).Tasks[0].ID); tk.AssigneeID != yi.MemberID {
		t.Fatalf("改派后负责人应是乙，实际 %s", tk.AssigneeID)
	}
	// 已达成的目标不收方案
	if _, err := a.ChangeGoalStatus(ctx, jia, goal.ID, domain.GoalAchieved, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ProposeGoalPlan(ctx, ag, GoalPlanInput{GoalID: goal.ID, Tasks: []PlanTask{{Title: "多余的"}}}); err == nil || !strings.Contains(err.Error(), "不再收方案") {
		t.Fatalf("已达成的目标应不收方案，实际 %v", err)
	}
}
