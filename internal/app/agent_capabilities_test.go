package app

import (
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// Agent 能力补齐第一批（docs/plans/2026-09-agent-capabilities.md）：
// 目标的读与改、闭环动作、批量改时间桶与重排、进展说明、验收、外部链接、里程碑的删除与撤销。
// 每条规则各走一遍：直接生效 / 需要人确认 / 一律待确认 / 没有授权，拒绝理由都是完整句子。

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// 目标编辑：普通字段按授权模式；负责人、上级、状态一律待确认；没授权直接拒；确认后真的生效并署名。
func TestAgentGoalUpdateGating(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	goal, err := a.CreateGoal(ctx, yi, CreateGoalInput{Title: "乙的目标"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.CreateGoal(ctx, yi, CreateGoalInput{Title: "另一个目标"})
	if err != nil {
		t.Fatal(err)
	}

	// 直接生效的授权：改说明立刻生效
	_, direct := agentSessionFor(t, a, ctx, yi, "直接的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantCreateGoal: domain.GrantDirect})
	desc := "改过的说明"
	if _, err := a.UpdateGoal(ctx, direct, goal.ID, UpdateGoalInput{Description: &desc}); err != nil {
		t.Fatalf("直接生效的授权改说明应成功：%v", err)
	}
	// 负责人是问责链：即便授权直接生效也要人确认
	_, err = a.UpdateGoal(ctx, direct, goal.ID, UpdateGoalInput{OwnerMemberID: &jia.MemberID})
	pp := mustPending(t, err)
	if pp.Proposal.Action != ActionGoalUpdate || !strings.Contains(pp.Proposal.SummaryText, "乙的目标") {
		t.Fatalf("改负责人应记成 goal.update 待确认操作，实际 %s %q", pp.Proposal.Action, pp.Proposal.SummaryText)
	}
	g, _ := a.GetGoal(ctx, jia, goal.ID)
	if g.OwnerMemberID != yi.MemberID {
		t.Fatal("待确认操作不该改动负责人")
	}
	// 上级同理
	parent := other.ID
	if _, err := a.UpdateGoal(ctx, direct, goal.ID, UpdateGoalInput{ParentID: &parent}); AsProposalPendingOK(err) == false {
		t.Fatalf("改上级应待确认，实际 %v", err)
	}
	// 达成走专门的入口：一律待确认，动作名是 goal.achieve；确认后目标变成已达成，动态带确认人
	_, err = a.ChangeGoalStatus(ctx, direct, goal.ID, domain.GoalAchieved, []domain.GoalStatus{domain.GoalActive, domain.GoalDraft})
	pp = mustPending(t, err)
	if pp.Proposal.Action != ActionGoalAchieve || !strings.Contains(pp.Proposal.SummaryText, "已达成") {
		t.Fatalf("达成应是 goal.achieve，实际 %s %q", pp.Proposal.Action, pp.Proposal.SummaryText)
	}
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err != nil {
		t.Fatalf("确认达成失败：%v", err)
	}
	g, _ = a.GetGoal(ctx, jia, goal.ID)
	if g.Status != domain.GoalAchieved || g.Progress != 100 {
		t.Fatalf("确认后目标应已达成，实际 %s %d", g.Status, g.Progress)
	}
	found := false
	for _, e := range eventsOfType(t, a, ctx, orgID, "GoalFieldChanged") {
		if e.Data["goal_id"] == goal.ID && e.Data["field"] == "status" && e.Data["approved_by_name"] == "乙" {
			found = true
		}
	}
	if !found {
		t.Fatal("确认后的动态应带「经乙确认」")
	}
	// 状态不对时的理由是完整句子
	if _, err := a.ChangeGoalStatus(ctx, yi, goal.ID, domain.GoalAchieved, []domain.GoalStatus{domain.GoalActive}); err == nil || !strings.Contains(errText(err), "已经是「已达成」") {
		t.Fatalf("重复达成应说明已经是已达成，实际 %v", err)
	}
	if _, err := a.ChangeGoalStatus(ctx, yi, goal.ID, domain.GoalActive, []domain.GoalStatus{domain.GoalAbandoned}); err == nil || !strings.Contains(errText(err), "现在是「已达成」") {
		t.Fatalf("从已达成重新开始应被拒并说明现状，实际 %v", err)
	}
	// 撤销达成（人）直接生效
	if _, err := a.ChangeGoalStatus(ctx, yi, goal.ID, domain.GoalActive, []domain.GoalStatus{domain.GoalAchieved}); err != nil {
		t.Fatal(err)
	}
	// 待确认操作重放时按那时的状态再校验：Agent 提了「放弃」，人先把目标标成已达成，确认时应被拒，不能拿旧结论盖掉新状态
	_, err = a.ChangeGoalStatus(ctx, direct, goal.ID, domain.GoalAbandoned, []domain.GoalStatus{domain.GoalActive, domain.GoalDraft})
	pp = mustPending(t, err)
	if _, err := a.ChangeGoalStatus(ctx, yi, goal.ID, domain.GoalAchieved, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err == nil || !strings.Contains(errText(err), "现在是「已达成」") {
		t.Fatalf("状态已变的待确认操作确认时应被拒并说明现状，实际 %v", err)
	}
	if g, _ := a.GetGoal(ctx, jia, goal.ID); g.Status != domain.GoalAchieved {
		t.Fatalf("被拒的确认不该改动状态，实际 %s", g.Status)
	}
	if _, err := a.ChangeGoalStatus(ctx, yi, goal.ID, domain.GoalActive, []domain.GoalStatus{domain.GoalAchieved}); err != nil {
		t.Fatal(err)
	}

	// 需要人确认的授权：连标题都要确认；没有授权：一句「没有「创建目标」授权」
	_, approval := agentSessionFor(t, a, ctx, yi, "需确认的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantCreateGoal: domain.GrantWithApproval})
	title := "改过的标题"
	if _, err := a.UpdateGoal(ctx, approval, goal.ID, UpdateGoalInput{Title: &title}); !AsProposalPendingOK(err) {
		t.Fatalf("需要人确认的授权改标题应待确认，实际 %v", err)
	}
	// 一个字段都没变：不挂待确认操作，也不报错
	if _, err := a.UpdateGoal(ctx, approval, goal.ID, UpdateGoalInput{Description: &desc}); err != nil {
		t.Fatalf("没有变化不该挂待确认操作，实际 %v", err)
	}
	_, none := agentSessionFor(t, a, ctx, yi, "没授权的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantComment: domain.GrantDirect})
	if _, err := a.UpdateGoal(ctx, none, goal.ID, UpdateGoalInput{Title: &title}); err == nil || !strings.Contains(errText(err), "「创建目标」授权") {
		t.Fatalf("没授权应说缺哪项授权，实际 %v", err)
	}

	// 批量改时间桶与重排：需要人确认时整批记成一条；确认后真的改了
	_, err = a.BulkGoalHorizon(ctx, approval, BulkGoalHorizonInput{IDs: []string{goal.ID, other.ID}, Horizon: domain.HorizonNow})
	pp = mustPending(t, err)
	if pp.Proposal.Action != ActionGoalHorizon || !strings.Contains(pp.Proposal.SummaryText, "2 个目标") {
		t.Fatalf("批量改时间桶应是一条 goal.horizon，实际 %s %q", pp.Proposal.Action, pp.Proposal.SummaryText)
	}
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	g, _ = a.GetGoal(ctx, jia, other.ID)
	if g.Horizon != domain.HorizonNow {
		t.Fatalf("确认后时间桶应是「现在」，实际 %q", g.Horizon)
	}
	_, err = a.SetGoalRanks(ctx, approval, GoalRankInput{IDs: []string{other.ID, goal.ID}})
	if pp = mustPending(t, err); pp.Proposal.Action != ActionGoalRank {
		t.Fatalf("重排应是 goal.rank，实际 %s", pp.Proposal.Action)
	}
	// 这批里一个都改不动（甲的目标，乙的 Agent 无权）：不挂待确认操作，结果里逐个写明理由
	jiaGoal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "甲的目标"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.BulkGoalHorizon(ctx, approval, BulkGoalHorizonInput{IDs: []string{jiaGoal.ID, "goal_nope"}, Horizon: domain.HorizonLater})
	if err != nil || res.Updated != 0 || len(res.Skipped) != 2 {
		t.Fatalf("全都改不动的批次应直接返回跳过清单而不挂待确认操作，实际 %v %+v", err, res)
	}
	// 混着能改与不能改的：挂一条，说明里写的是改得动的个数
	_, err = a.BulkGoalHorizon(ctx, approval, BulkGoalHorizonInput{IDs: []string{jiaGoal.ID, goal.ID}, Horizon: domain.HorizonLater})
	if pp = mustPending(t, err); !strings.Contains(pp.Proposal.SummaryText, "1 个目标") {
		t.Fatalf("说明应只数改得动的目标，实际 %q", pp.Proposal.SummaryText)
	}
	if _, err := a.SetGoalRanks(ctx, direct, GoalRankInput{IDs: []string{other.ID, goal.ID}}); err != nil {
		t.Fatalf("直接生效的授权重排应成功：%v", err)
	}
}

// AsProposalPendingOK 只看是不是待确认。
func AsProposalPendingOK(err error) bool {
	_, ok := AsProposalPending(err)
	return ok
}

// 给 Agent 的目标视图：没有内部字段，有名字、上级链、直接任务与进展说明；进展说明与评论同一套授权。
func TestGoalBriefAndNotes(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	root, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "年度目标"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "季度目标", ParentID: root.ID, OwnerMemberID: yi.MemberID})
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: child.ID, Title: "季度任务", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{ParentID: task.ID, Title: "子任务", AssigneeID: yi.MemberID, Ready: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGoalNote(ctx, yi, child.ID, "接口已联调，等设计稿"); err != nil {
		t.Fatal(err)
	}
	b, err := a.GoalBriefDetail(ctx, yi, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Owner != "乙" || b.StatusTitle != "进行中" || !b.CanEdit {
		t.Fatalf("视图应带负责人名字、状态名与可编辑，实际 %+v", b)
	}
	if len(b.Parents) != 1 || b.Parents[0].Title != "年度目标" || b.Parents[0].StatusTitle != "进行中" {
		t.Fatalf("上级链应是年度目标，实际 %+v", b.Parents)
	}
	// 上级链带的是树上算好的进度：把季度任务做完，年度目标的进度也跟着变
	if _, err := a.Transition(ctx, yi, task.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddArtifact(ctx, yi, task.ID, domain.Artifact{Type: "result", Title: "结果"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, yi, task.ID, "submit", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jia, task.ID, "accept", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if b, _ = a.GoalBriefDetail(ctx, yi, child.ID); b.Parents[0].Progress != 100 || b.Progress != 100 {
		t.Fatalf("上级链应带算好的进度，实际上级 %d 自己 %d", b.Parents[0].Progress, b.Progress)
	}
	if len(b.Tasks) != 1 || b.Tasks[0].Title != "季度任务" {
		t.Fatalf("直接任务应只有顶层的一个，实际 %+v", b.Tasks)
	}
	if len(b.Notes) != 1 || b.Notes[0].Text != "接口已联调，等设计稿" || b.Notes[0].By != "乙" {
		t.Fatalf("进展说明应带在详情里，实际 %+v", b.Notes)
	}
	tree, err := a.GoalBriefTree(ctx, yi)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || len(tree[0].Children) != 1 || tree[0].Children[0].Tasks != nil {
		t.Fatalf("树里的节点不带任务清单，实际 %+v", tree)
	}
	// 空内容拒绝；Agent 要「评论」授权，需要人确认时挂起
	if _, err := a.AddGoalNote(ctx, yi, child.ID, "  "); err == nil {
		t.Fatal("空进展说明应被拒")
	}
	_, ag := agentSessionFor(t, a, ctx, yi, "记进展的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantComment: domain.GrantWithApproval})
	_, err = a.AddGoalNote(ctx, ag, child.ID, "Agent 的进展")
	pp := mustPending(t, err)
	if pp.Proposal.Action != ActionGoalNote {
		t.Fatalf("应是 goal.note，实际 %s", pp.Proposal.Action)
	}
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	b, _ = a.GoalBriefDetail(ctx, yi, child.ID)
	if len(b.Notes) != 2 || b.Notes[0].Text != "Agent 的进展" {
		t.Fatalf("确认后进展说明应出现且新的在前，实际 %+v", b.Notes)
	}
	_, noGrant := agentSessionFor(t, a, ctx, yi, "没评论授权的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect})
	if _, err := a.AddGoalNote(ctx, noGrant, child.ID, "x"); err == nil || !strings.Contains(errText(err), "「评论」授权") {
		t.Fatalf("没授权应说缺「评论」授权，实际 %v", err)
	}
}

// 验收契约：get_workflow 说清我是不是验收人、能走哪两步；review_task 按决定挑步骤，打回要说明，核对的交付物必须真的在。
func TestReviewTask(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	_, reviewer := agentSessionFor(t, a, ctx, jia, "验收 Agent", map[domain.Grant]domain.GrantMode{domain.GrantReview: domain.GrantDirect})
	_, worker := agentSessionFor(t, a, ctx, yi, "干活 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect, domain.GrantReview: domain.GrantDirect})
	mk := func(title string) *domain.Task {
		task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: title, AssigneeID: yi.MemberID, Ready: true})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	submit := func(task *domain.Task) {
		if _, err := a.Transition(ctx, worker, task.ID, "start", TransitionPayload{}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.AddArtifact(ctx, worker, task.ID, domain.Artifact{Type: "result", Title: "结果说明"}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Transition(ctx, worker, task.ID, "submit", TransitionPayload{}); err != nil {
			t.Fatal(err)
		}
	}
	task := mk("要验收的任务")
	// 还没提交：没有验收步骤
	if _, err := a.ReviewTask(ctx, reviewer, task.ID, ReviewInput{Decision: "accept"}); err == nil || !strings.Contains(errText(err), "没有可走的验收步骤") {
		t.Fatalf("未提交的任务验收应说没有验收步骤，实际 %v", err)
	}
	submit(task)
	wf, err := a.Workflow(ctx, reviewer, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !wf.IsReviewer || wf.ReviewAccept != "accept" || wf.ReviewReject != "reject" {
		t.Fatalf("验收人视角应看到 is_reviewer 与两步的名字，实际 %+v", wf)
	}
	if wf2, _ := a.Workflow(ctx, worker, task.ID); wf2.IsReviewer {
		t.Fatal("负责人的 Agent 不是验收人")
	}
	if _, err := a.ReviewTask(ctx, reviewer, task.ID, ReviewInput{Decision: "maybe"}); err == nil || !strings.Contains(errText(err), "accept") {
		t.Fatalf("决定写错应给完整句子，实际 %v", err)
	}
	if _, err := a.ReviewTask(ctx, reviewer, task.ID, ReviewInput{Decision: "accept", CheckedDeliverables: []string{"test_report"}}); err == nil || !strings.Contains(errText(err), "没有交付物「test_report」") {
		t.Fatalf("核对不存在的交付物应被拒，实际 %v", err)
	}
	// 不是验收人：内核的那句「只能由验收人来做」
	if _, err := a.ReviewTask(ctx, worker, task.ID, ReviewInput{Decision: "accept"}); err == nil || !strings.Contains(errText(err), "验收人") {
		t.Fatalf("非验收人应被内核拒绝，实际 %v", err)
	}
	// 打回要有人写的说明：只列核对过的交付物不算
	if _, err := a.ReviewTask(ctx, reviewer, task.ID, ReviewInput{Decision: "reject"}); err == nil || !strings.Contains(errText(err), "评论") {
		t.Fatalf("打回不写说明应被拒，实际 %v", err)
	}
	if _, err := a.ReviewTask(ctx, reviewer, task.ID, ReviewInput{Decision: "reject", CheckedDeliverables: []string{"result"}}); err == nil || !strings.Contains(errText(err), "评论") {
		t.Fatalf("只列核对项、不写说明的打回应被拒，实际 %v", err)
	}
	got, err := a.ReviewTask(ctx, reviewer, task.ID, ReviewInput{Decision: "reject", Comment: "缺测试", CheckedDeliverables: []string{"result"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "todo" {
		t.Fatalf("打回后应回到待办，实际 %s", got.State)
	}
	last := got.Comments[len(got.Comments)-1].Text
	if !strings.Contains(last, "已核对交付物：结果说明") || !strings.Contains(last, "缺测试") {
		t.Fatalf("说明里应带核对过的交付物与理由，实际 %q", last)
	}
	// 再提交，通过；授权是需要人确认时先挂起
	submit(task)
	_, pendingReviewer := agentSessionFor(t, a, ctx, jia, "需确认的验收 Agent", map[domain.Grant]domain.GrantMode{domain.GrantReview: domain.GrantWithApproval})
	_, err = a.ReviewTask(ctx, pendingReviewer, task.ID, ReviewInput{Decision: "accept"})
	if pp := mustPending(t, err); pp.Proposal.Action != ActionTaskTransition {
		t.Fatalf("需确认的验收应是推进任务的待确认操作，实际 %s", pp.Proposal.Action)
	}
	keyed := reviewer.WithWrite(WriteOptions{IdempotencyKey: "review-once"})
	got, err = a.ReviewTask(ctx, keyed, task.ID, ReviewInput{Decision: "accept", Comment: "可以"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "done" {
		t.Fatalf("通过后应已完成，实际 %s", got.State)
	}
	// 同一个幂等键再发一次：返回第一次的结果，而不是「没有可走的验收步骤」
	_, err = a.ReviewTask(ctx, keyed, task.ID, ReviewInput{Decision: "accept", Comment: "可以"})
	if rp, ok := AsRepeated(err); !ok || rp.Ref != task.ID {
		t.Fatalf("同键重发应命中幂等键并带回第一次的结果，实际 %v", err)
	}
}

// 外部链接：挂与摘同一套规则；里程碑：删除与撤销达到对 Agent 一律待确认，确认后真的做了。
func TestAgentLinksAndMilestoneConfirmation(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "挂链接的任务", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	_, ag := agentSessionFor(t, a, ctx, yi, "需确认的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantWithApproval, domain.GrantCreateGoal: domain.GrantDirect})
	_, err = a.AddTaskLink(ctx, ag, task.ID, LinkInput{Kind: "pr", URL: "https://example.com/pr/1", Title: "PR 1"})
	pp := mustPending(t, err)
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	links, err := a.TaskLinks(ctx, ag, task.ID)
	if err != nil || len(links) != 1 {
		t.Fatalf("确认后应有一条链接，实际 %v %d", err, len(links))
	}
	err = a.RemoveTaskLink(ctx, ag, task.ID, links[0].ID)
	pp = mustPending(t, err)
	if pp.Proposal.Action != ActionTaskExternalLinkRemove || !strings.Contains(pp.Proposal.SummaryText, "PR 1") {
		t.Fatalf("摘链接应是 task.external_link_remove 并带标题，实际 %s %q", pp.Proposal.Action, pp.Proposal.SummaryText)
	}
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	if links, _ = a.TaskLinks(ctx, ag, task.ID); len(links) != 0 {
		t.Fatal("确认后链接应已摘掉")
	}
	if n := len(eventsOfType(t, a, ctx, orgID, "ExternalLinkRemoved")); n != 1 {
		t.Fatalf("摘链接应留一条动态，实际 %d", n)
	}

	// 里程碑：授权直接生效，改日期立刻生效；删除与撤销达到仍要人确认
	goal, err := a.CreateGoal(ctx, yi, CreateGoalInput{Title: "乙的目标"})
	if err != nil {
		t.Fatal(err)
	}
	ms, err := a.CreateMilestone(ctx, ag, CreateMilestoneInput{GoalID: goal.ID, Title: "上线", DueOn: dayp(10)})
	if err != nil {
		t.Fatalf("直接生效的授权新增里程碑应成功：%v", err)
	}
	if _, err := a.UpdateMilestone(ctx, ag, ms.ID, UpdateMilestoneInput{DueOn: dayp(12)}); err != nil {
		t.Fatalf("直接生效的授权改里程碑应成功：%v", err)
	}
	if _, err := a.ReachMilestone(ctx, ag, ms.ID); err != nil {
		t.Fatal(err)
	}
	_, err = a.UnreachMilestone(ctx, ag, ms.ID)
	if pp = mustPending(t, err); pp.Proposal.Action != ActionMilestoneUnreach {
		t.Fatalf("撤销达到应一律待确认，实际 %v", err)
	}
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	err = a.DeleteMilestone(ctx, ag, ms.ID)
	if pp = mustPending(t, err); pp.Proposal.Action != ActionMilestoneDelete {
		t.Fatalf("删除里程碑应一律待确认，实际 %v", err)
	}
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := a.ListMilestones(ctx, yi, goal.ID); len(list) != 0 {
		t.Fatal("确认后里程碑应已删除")
	}
	_ = store.ErrNotFound
}
