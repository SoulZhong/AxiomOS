package app

import (
	"context"
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// 委托（ADR 0028）端到端：一个目标从批准方案到方案验收，人只出手两次。
// 方案批准即委托 → Agent 在委托内开始、写进展、附交付物、提交都不再问人，执行记录自动开 →
// 自动验收 → 方案里最后一个任务收尾生成方案验收 → 目标负责人一次答完；中途改负责人仍要问，撤回只追加动态。
func TestMandateTwoDecisions(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "两次决定", OwnerMemberID: jia.MemberID})
	if err != nil {
		t.Fatal(err)
	}
	// 乙的 Agent：执行 / 评论 / 指派都「需要人确认」，建任务与目标直接生效
	agRec, ag := agentSessionFor(t, a, ctx, yi, "乙的 Agent", map[domain.Grant]domain.GrantMode{
		domain.GrantExecute: domain.GrantWithApproval, domain.GrantComment: domain.GrantWithApproval, domain.GrantAssign: domain.GrantWithApproval,
		domain.GrantCreateTask: domain.GrantDirect, domain.GrantCreateGoal: domain.GrantDirect, domain.GrantLink: domain.GrantDirect})

	plan := GoalPlanInput{GoalID: goal.ID, Tasks: []PlanTask{
		{Key: "a", Title: "第一件"},
		{Key: "b", Title: "第二件", DependsOn: []string{"a"}},
	}, Milestones: []PlanMilestone{{Title: "都做完", DueOn: dayp(10)}}}
	v, err := a.ProposeGoalPlan(ctx, ag, plan)
	if err != nil {
		t.Fatal(err)
	}
	// 第一次决定：批准方案
	res, err := a.ApproveProposal(ctx, jia, v.ID)
	if err != nil {
		t.Fatalf("批准方案失败：%v", err)
	}
	r := res.Result.(*GoalPlanResult)
	if len(r.Tasks) != 2 {
		t.Fatalf("应建 2 个任务，实际 %+v", r.Tasks)
	}
	ta, tb := r.Tasks[0].ID, r.Tasks[1].ID
	// 批准即委托：两个任务各一份，验收方式默认自动
	for _, id := range []string{ta, tb} {
		ms, err := a.TaskMandates(ctx, jia, id)
		if err != nil || len(ms) != 1 || !ms[0].Active || ms[0].AgentID != agRec.ID || ms[0].OwnerID != jia.MemberID || ms[0].PlanID != v.ID {
			t.Fatalf("批准方案应给任务发一份委托，实际 %v %+v", err, ms)
		}
		full, _ := a.GetTask(ctx, jia, id)
		if full.AcceptanceMode != domain.AcceptanceAuto || full.PlanID != v.ID {
			t.Fatalf("方案里的任务应记验收方式与方案编号，实际 %q %q", full.AcceptanceMode, full.PlanID)
		}
	}

	// 白名单之外：改负责人仍要人确认
	_, err = a.Assign(ctx, ag, ta, yi.MemberID)
	mustPending(t, err)
	// 委托之内：开始、写进展、附交付物、提交都直接生效；执行记录自动开
	if _, err := a.Transition(ctx, ag, ta, "start", TransitionPayload{}); err != nil {
		t.Fatalf("委托内开始不应问人：%v", err)
	}
	wf, _ := a.Workflow(ctx, ag, ta)
	if wf.ActiveRun == nil {
		t.Fatal("进入进行中应自动开执行记录")
	}
	if _, err := a.AddComment(ctx, ag, ta, "做了一半", true); err != nil {
		t.Fatalf("委托内写进展不应问人：%v", err)
	}
	if _, err := a.AddArtifact(ctx, ag, ta, domain.Artifact{Type: "result", Title: "结果", Ref: "r"}); err != nil {
		t.Fatalf("委托内附交付物不应问人：%v", err)
	}
	if _, err := a.Transition(ctx, ag, ta, "submit", TransitionPayload{}); err != nil {
		t.Fatalf("委托内提交不应问人：%v", err)
	}
	// 自动验收：提交后系统按验收标准直接走验收步，任务到终态，委托随之结束
	full, _ := a.GetTask(ctx, jia, ta)
	if full.State != "done" {
		t.Fatalf("自动验收后应是已完成，实际 %s", full.State)
	}
	evs, _ := a.Events(ctx, jia, ta, 50)
	if !hasEvent(evs, "TaskAutoAccepted") || !hasEvent(evs, "MandateIssued") || !hasEvent(evs, "RunStarted") || hasEvent(evs, "MandateStale") {
		t.Fatalf("应有自动验收、委托发出、执行记录的动态，任务结束不算失效，实际 %v", typesOf(evs))
	}
	if ms, _ := a.TaskMandates(ctx, jia, ta); ms[0].Active || ms[0].Status != domain.MandateDone {
		t.Fatalf("任务结束后委托应自然结束，实际 %+v", ms[0].Mandate)
	}

	// 第二个任务：开始后所有者撤回一步（只追加动态，执行记录被取消），再重新开始做完
	if _, err := a.Transition(ctx, ag, tb, "start", TransitionPayload{}); err != nil {
		t.Fatalf("第二件开始失败：%v", err)
	}
	if _, err := a.RevertTask(ctx, ag, tb, "不对"); err == nil {
		t.Fatal("Agent 不能撤回")
	}
	if _, err := a.RevertTask(ctx, yi, tb, "不对"); err == nil {
		t.Fatal("不是发委托的人不能撤回")
	}
	if _, err := a.RevertTask(ctx, jia, tb, "先别开始"); err != nil {
		t.Fatalf("发委托的人应能撤回：%v", err)
	}
	full, _ = a.GetTask(ctx, jia, tb)
	if full.State != "todo" {
		t.Fatalf("撤回后应回到待办，实际 %s", full.State)
	}
	evs, _ = a.Events(ctx, jia, tb, 50)
	if !hasEvent(evs, "TaskReverted") || !hasEvent(evs, "TaskTransitioned") {
		t.Fatal("撤回应只追加 TaskReverted，原推进动态保留")
	}
	if _, err := a.RevertTask(ctx, jia, tb, "再撤"); err == nil {
		t.Fatal("已撤回的不能再撤")
	}
	if _, err := a.Transition(ctx, ag, tb, "start", TransitionPayload{}); err != nil {
		t.Fatalf("撤回后 Agent 应能再开始：%v", err)
	}
	if _, err := a.AddArtifact(ctx, ag, tb, domain.Artifact{Type: "result", Title: "结果", Ref: "r"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, ag, tb, "submit", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}

	// 方案收尾：生成一份方案验收，确认人是目标负责人甲；这是第二次决定
	reviews, err := a.ListProposals(ctx, jia, ProposalFilter{Mine: true, Status: domain.ProposalPending})
	if err != nil {
		t.Fatal(err)
	}
	var review *ProposalView
	for _, p := range reviews {
		if p.Action == ActionPlanReview {
			review = p
		}
	}
	if review == nil {
		t.Fatalf("方案收尾后应有一份方案验收等甲确认，实际 %+v", reviews)
	}
	if !strings.Contains(review.SummaryText, "两次决定") {
		t.Fatalf("方案验收的句子应点名目标，实际 %q", review.SummaryText)
	}
	rr, err := a.ApproveProposal(ctx, jia, review.ID)
	if err != nil {
		t.Fatalf("方案验收失败：%v", err)
	}
	out := rr.Result.(map[string]any)
	if got := out["milestones_reached"].([]string); len(got) != 1 {
		t.Fatalf("方案验收应把里程碑标为已达到，实际 %+v", out)
	}
	if !hasEventOfType(t, a, ctx, orgID, "PlanReviewed") || !hasEventOfType(t, a, ctx, orgID, "PlanReviewRequested") {
		t.Fatal("方案验收应留下动态")
	}
	// 人一共只答了两次：批准方案、方案验收（改负责人那条是 Agent 越出白名单时提的，人没答）
	all, _ := a.ListProposals(ctx, jia, ProposalFilter{})
	decided := 0
	for _, p := range all {
		if p.Status == domain.ProposalApproved {
			decided++
		}
	}
	if decided != 2 {
		t.Fatalf("人应只确认了两次，实际 %d", decided)
	}
}

// 待确认操作的捆、版本与快照（ADR 0028 第 7 条）：同一任务上连续提的合成一捆；重放用提出时的授权快照；
// 委托失效的触发：换负责人；收回委托后回到逐项判定。
func TestMandateBundleAndLifecycle(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	agRec, ag := agentSessionFor(t, a, ctx, yi, "乙的 Agent", map[domain.Grant]domain.GrantMode{
		domain.GrantExecute: domain.GrantWithApproval, domain.GrantComment: domain.GrantWithApproval, domain.GrantClaimBacklog: domain.GrantDirect})
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "捆", AssigneeID: agRec.ID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	// 人指派给 Agent 即发委托
	ms, _ := a.TaskMandates(ctx, jia, task.ID)
	if len(ms) != 1 || !ms[0].Active || ms[0].OwnerID != jia.MemberID {
		t.Fatalf("人指派给 Agent 应自动发委托，实际 %+v", ms)
	}
	// 收回委托：回到逐项判定
	if _, err := a.RevokeMandate(ctx, jia, ms[0].ID, "先不交给它"); err != nil {
		t.Fatal(err)
	}
	_, err = a.Transition(ctx, ag, task.ID, "start", TransitionPayload{})
	p1 := mustPending(t, err)
	_, err = a.AddComment(ctx, ag, task.ID, "先说一句", false)
	p2 := mustPending(t, err)
	if p1.Proposal.ID == p2.Proposal.ID {
		t.Fatal("两条待确认操作")
	}
	full1, _ := a.GetProposal(ctx, jia, p1.Proposal.ID)
	full2, _ := a.GetProposal(ctx, jia, p2.Proposal.ID)
	if full1.BundleID == "" || full1.BundleID != full2.BundleID {
		t.Fatalf("同一任务上连续提的应合成一捆，实际 %q %q", full1.BundleID, full2.BundleID)
	}
	if full1.Proposal.TargetVersion == 0 || len(full1.Proposal.GrantsSnapshot) == 0 {
		t.Fatalf("待确认操作应记对象版本与授权快照，实际 %+v", full1.Proposal)
	}
	// 所有者收回「评论」授权后再确认那条评论：重放用快照里的授权，仍按人确认的那件事执行
	if _, err := a.UpdateAgent(ctx, yi, agRec.ID, RegisterAgentInput{Grants: map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantWithApproval, domain.GrantClaimBacklog: domain.GrantDirect}}); err != nil {
		t.Fatal(err)
	}
	// 先答第一条（开始），再答第二条：同一捆里前一条的重放不算「别人改过」
	if _, err := a.ApproveProposal(ctx, jia, p1.Proposal.ID); err != nil {
		t.Fatalf("确认第一条失败：%v", err)
	}
	if _, err := a.ApproveProposal(ctx, jia, p2.Proposal.ID); err != nil {
		t.Fatalf("同一捆里第二条不该因第一条的重放而失效：%v", err)
	}
	// 重新发委托，然后所有者收回委托里用到的「执行任务」授权 → 委托失效
	m, err := a.IssueMandate(ctx, jia, task.ID, agRec.ID, MandateOptions{})
	if err != nil || !m.Active {
		t.Fatalf("重新发委托失败：%v", err)
	}
	if _, err := a.UpdateAgent(ctx, yi, agRec.ID, RegisterAgentInput{Grants: map[domain.Grant]domain.GrantMode{domain.GrantClaimBacklog: domain.GrantDirect}}); err != nil {
		t.Fatal(err)
	}
	ms, _ = a.TaskMandates(ctx, jia, task.ID)
	if ms[0].Active || ms[0].Status != domain.MandateStale || ms[0].Reason != "mandate.stale.grants" {
		t.Fatalf("收回授权后委托应失效，实际 %+v", ms[0].Mandate)
	}
	// 只看不做不发委托、不留动态
	before := len(mustEvents(t, a, ctx, jia, task.ID))
	if _, err := a.Assign(ctx, jia.WithWrite(WriteOptions{DryRun: true}), task.ID, agRec.ID); err == nil {
		t.Fatal("只看不做应返回预演")
	}
	if len(mustEvents(t, a, ctx, jia, task.ID)) != before {
		t.Fatal("只看不做不该留下任何动态")
	}
}

func typesOf(evs []*store.EventRow) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

func mustEvents(t *testing.T, a *App, ctx context.Context, sess *Session, taskID string) []*store.EventRow {
	t.Helper()
	evs, err := a.Events(ctx, sess, taskID, 100)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}
