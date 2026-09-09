package app

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// agentSessionFor 注册一个 Agent 并返回它的会话。
func agentSessionFor(t *testing.T, a *App, ctx context.Context, owner *Session, name string, grants map[domain.Grant]domain.GrantMode) (*domain.Agent, *Session) {
	t.Helper()
	ag, token, err := a.RegisterAgent(ctx, owner, RegisterAgentInput{Name: name, Capabilities: []string{"coding"}, Grants: grants, MaxConcurrent: 3})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := a.SessionFromAgentToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	return ag, sess
}

func mustPending(t *testing.T, err error) *ProposalPending {
	t.Helper()
	pp, ok := AsProposalPending(err)
	if !ok {
		t.Fatalf("应转成待确认操作，实际 %v", err)
	}
	if pp.Proposal == nil || pp.Proposal.ID == "" {
		t.Fatal("待确认操作应有 ID")
	}
	if !strings.Contains(pp.Message, "确认") {
		t.Fatalf("提示应是完整中文句子，实际 %q", pp.Message)
	}
	return pp
}

// 待确认操作全程：Agent 发起 → 只记不执行 → 确认 → 真的执行并留下动态 → 再确认失败；另有拒绝与过期。
func TestProposalLifecycle(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)

	// 乙的 Agent：执行、评论都要人确认
	_, agent := agentSessionFor(t, a, ctx, yi, "乙的 Agent", map[domain.Grant]domain.GrantMode{
		domain.GrantExecute: domain.GrantWithApproval,
		domain.GrantComment: domain.GrantWithApproval,
	})

	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "写文档", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}

	// ---- Agent 推进：只记一条待确认操作，任务不动 ----
	_, err = a.Transition(ctx, agent, task.ID, "start", TransitionPayload{})
	pp := mustPending(t, err)
	if pp.Proposal.Action != ActionTaskTransition {
		t.Fatalf("动作应是 %s，实际 %s", ActionTaskTransition, pp.Proposal.Action)
	}
	if !strings.Contains(pp.Proposal.SummaryText, "确认后") || !strings.Contains(pp.Proposal.SummaryText, "写文档") {
		t.Fatalf("说明应是完整中文句子并带上任务名，实际 %q", pp.Proposal.SummaryText)
	}
	if en := pp.Proposal.Summary.In(i18n.EnUS); !strings.Contains(en, "Once confirmed") {
		t.Fatalf("说明应同时有英文，实际 %q", en)
	}
	cur, err := a.GetTask(ctx, jia, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.State != "todo" {
		t.Fatalf("待确认期间任务不应改变，实际 %s", cur.State)
	}
	if evs, _ := a.Events(ctx, jia, task.ID, 20); !hasEvent(evs, "ProposalCreated") {
		t.Fatal("提交待确认操作应留下动态")
	}
	// 通知了确认人（Agent 的所有者）
	notes, err := a.Notifications(ctx, yi, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) == 0 || !strings.Contains(notes[0].Title, "待确认操作") {
		t.Fatalf("应通知确认人，实际 %+v", notes)
	}

	// ---- 谁能看到：所有者与组织负责人能，别人不能 ----
	mine, err := a.ListProposals(ctx, yi, ProposalFilter{Mine: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || !mine[0].CanDecide {
		t.Fatalf("所有者应看到 1 条等他确认的，实际 %d", len(mine))
	}
	if owners, _ := a.ListProposals(ctx, jia, ProposalFilter{Mine: true}); len(owners) != 1 {
		t.Fatalf("组织负责人应看到，实际 %d", len(owners))
	}
	if _, err := a.ApproveProposal(ctx, agent, pp.Proposal.ID); err == nil {
		t.Fatal("Agent 不能确认自己提交的待确认操作")
	}

	// ---- 确认：真的执行，动态记在 Agent 名下并带上「经…确认」 ----
	res, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID)
	if err != nil {
		t.Fatalf("确认应成功，实际 %v", err)
	}
	if res.Proposal.Status != domain.ProposalApproved || res.Proposal.DecidedBy != yi.MemberID {
		t.Fatalf("状态应是已确认，实际 %+v", res.Proposal.Proposal)
	}
	cur, _ = a.GetTask(ctx, jia, task.ID)
	if cur.State != "in_progress" {
		t.Fatalf("确认后应真的推进，实际 %s", cur.State)
	}
	evs, _ := a.Events(ctx, jia, task.ID, 30)
	var moved *store.EventRow
	for _, e := range evs { // 动态按时间倒序，取最新的一条
		if e.Type == "TaskTransitioned" {
			moved = e
			break
		}
	}
	if moved == nil {
		t.Fatal("确认后应有推进动态")
	}
	if moved.ActorID != agent.Actor.ID {
		t.Fatalf("动态仍应记在 Agent 名下，实际 %s", moved.ActorID)
	}
	if name, _ := moved.Data["approved_by_name"].(string); name != yi.Actor.Name {
		t.Fatalf("动态里应记下确认人，实际 %v", moved.Data["approved_by_name"])
	}
	if !hasEvent(evs, "ProposalApproved") {
		t.Fatal("确认本身也应留下动态")
	}

	// ---- 重复确认被拒 ----
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err == nil || !strings.Contains(err.Error(), "已经处理过") {
		t.Fatalf("重复确认应被拒并说明，实际 %v", err)
	}

	// ---- 拒绝：理由必填，动作不执行 ----
	_, err = a.AddComment(ctx, agent, task.ID, "这个需求我理解得对吗？", false)
	cp := mustPending(t, err)
	if _, err := a.RejectProposal(ctx, yi, cp.Proposal.ID, "  "); err == nil || !strings.Contains(err.Error(), "理由") {
		t.Fatalf("拒绝必须写理由，实际 %v", err)
	}
	reason := "这个问题上周已经在群里答复过了，不用再问一次。"
	rv, err := a.RejectProposal(ctx, yi, cp.Proposal.ID, reason)
	if err != nil {
		t.Fatal(err)
	}
	if rv.Status != domain.ProposalRejected || rv.Reason != reason {
		t.Fatalf("应记下拒绝理由，实际 %+v", rv.Proposal)
	}
	full, err := a.GetTask(ctx, jia, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range full.Comments {
		if strings.Contains(c.Text, "我理解得对吗") {
			t.Fatal("被拒绝的评论不应写进任务")
		}
	}
	if evs, _ := a.Events(ctx, jia, task.ID, 40); !hasEvent(evs, "ProposalRejected") {
		t.Fatal("拒绝也应留下动态")
	}
	if _, err := a.ApproveProposal(ctx, yi, cp.Proposal.ID); err == nil {
		t.Fatal("已拒绝的不能再确认")
	}

	// ---- 执行失败时保持等人确认 ----
	_, err = a.Transition(ctx, agent, task.ID, "ask_for_input", TransitionPayload{Comment: "口径按哪个季度算？"})
	sp := mustPending(t, err)
	// 人先自己走了这一步，待确认操作里的步骤就走不成了
	if _, err := a.Transition(ctx, yi, task.ID, "ask_for_input", TransitionPayload{Comment: "先等一下"}); err != nil {
		t.Fatal(err)
	}
	// ADR 0028：对象被别人改过，这条待确认操作失效，不再重放
	if _, err := a.ApproveProposal(ctx, yi, sp.Proposal.ID); err == nil || !strings.Contains(err.Error(), "失效") {
		t.Fatalf("对象已被别人改过，确认应报失效，实际 %v", err)
	}
	still, err := a.GetProposal(ctx, yi, sp.Proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if still.Status != domain.ProposalStale {
		t.Fatalf("对象被改过的待确认操作应失效，实际 %s", still.Status)
	}
	if evs, _ := a.Events(ctx, jia, task.ID, 40); !hasEvent(evs, "ProposalStale") {
		t.Fatal("失效也应留下动态")
	}

	// ---- 过期：超过有效期的自动作废 ----
	_, err = a.AddComment(ctx, agent, task.ID, "还有一个问题", false)
	sp = mustPending(t, err)
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update proposals set expires_at = now() - interval '1 day' where id=$1`, sp.Proposal.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	n, err := a.ExpireProposals(ctx, orgID)
	if err != nil || n != 1 {
		t.Fatalf("应作废 1 条，实际 n=%d err=%v", n, err)
	}
	gone, _ := a.GetProposal(ctx, yi, sp.Proposal.ID)
	if gone.Status != domain.ProposalExpired {
		t.Fatalf("应已过期，实际 %s", gone.Status)
	}
	if _, err := a.ApproveProposal(ctx, yi, sp.Proposal.ID); err == nil || !strings.Contains(err.Error(), "过期") {
		t.Fatalf("过期的不能再确认，实际 %v", err)
	}
	if cnt, err := a.PendingProposalCount(ctx, yi); err != nil || cnt != 0 {
		t.Fatalf("不应还有等人确认的，实际 %d err=%v", cnt, err)
	}
}

// 迭代与流程这类改规则的动作，Agent 一律走待确认操作（ADR 0003）。
func TestProposalCoversSprintAndWorkflow(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)

	// 甲是组织负责人（有「管理流程」权限），他的 Agent 拿到的授权会被强制为需要人确认
	_, jiaAgent := agentSessionFor(t, a, ctx, jia, "甲的 Agent", map[domain.Grant]domain.GrantMode{
		domain.GrantManageWorkflows: domain.GrantDirect, // 会被强制成 with_approval
		domain.GrantCreateGoal:      domain.GrantWithApproval,
	})
	if domain.GrantModeOf(jiaAgent.Actor, domain.GrantManageWorkflows) != domain.GrantWithApproval {
		t.Fatal("「管理流程」对 Agent 只能是需要人确认")
	}

	sp, err := a.CreateSprint(ctx, jia, CreateSprintInput{Name: "10 月第 1 迭代", StartsOn: dayp(-1), EndsOn: dayp(6)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.StartSprint(ctx, jiaAgent, sp.ID)
	pp := mustPending(t, err)
	if pp.Proposal.Action != ActionSprintStart || pp.Proposal.TargetKind != "sprint" {
		t.Fatalf("应是开始迭代的待确认操作，实际 %+v", pp.Proposal.Proposal)
	}
	after, err := a.ListSprints(ctx, jia, store.SprintFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Status != domain.SprintPlanning {
		t.Fatalf("待确认期间迭代不应开始，实际 %s", after[0].Status)
	}
	if _, err := a.ApproveProposal(ctx, jia, pp.Proposal.ID); err != nil {
		t.Fatalf("确认应成功，实际 %v", err)
	}
	after, _ = a.ListSprints(ctx, jia, store.SprintFilter{})
	if after[0].Status != domain.SprintActive {
		t.Fatalf("确认后迭代应开始，实际 %s", after[0].Status)
	}

	// 创建目标同理
	_, err = a.CreateGoal(ctx, jiaAgent, CreateGoalInput{Title: "Agent 想建的目标"})
	gp := mustPending(t, err)
	if _, err := a.ApproveProposal(ctx, jia, gp.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	tree, _ := a.GoalTree(ctx, jia)
	found := false
	for _, g := range tree {
		if g.Title == "Agent 想建的目标" {
			found = true
		}
	}
	if !found {
		t.Fatal("确认后目标应真的建出来")
	}

	// 修改任务类型：Agent 也要人确认
	tt, err := a.GetTaskType(ctx, jia, "generic")
	if err != nil {
		t.Fatal(err)
	}
	tt.AgentInstructions = "改过的执行指令"
	_, err = a.SaveTaskType(ctx, jiaAgent, tt)
	wp := mustPending(t, err)
	if wp.Proposal.Action != ActionTaskTypeSave {
		t.Fatalf("应是修改任务类型的待确认操作，实际 %s", wp.Proposal.Action)
	}
	saved, _ := a.GetTaskType(ctx, jia, "generic")
	if saved.AgentInstructions == "改过的执行指令" {
		t.Fatal("待确认期间不应改动任务类型")
	}
	if _, err := a.ApproveProposal(ctx, jia, wp.Proposal.ID); err != nil {
		t.Fatalf("确认应成功，实际 %v", err)
	}
	saved, _ = a.GetTaskType(ctx, jia, "generic")
	if saved.AgentInstructions != "改过的执行指令" {
		t.Fatal("确认后应真的保存新版本")
	}

	// 乙没有「管理流程」权限，他的 Agent 连提都提不了（权限 ∩ 授权）
	_, yiAgent := agentSessionFor(t, a, ctx, yi, "乙的流程 Agent", map[domain.Grant]domain.GrantMode{
		domain.GrantManageWorkflows: domain.GrantWithApproval,
	})
	sp2, err := a.CreateSprint(ctx, jia, CreateSprintInput{Name: "10 月第 2 迭代", StartsOn: dayp(20), EndsOn: dayp(30)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.StartSprint(ctx, yiAgent, sp2.ID); err == nil {
		t.Fatal("所有者没有「管理流程」权限时，Agent 也不能提交待确认操作")
	} else if _, ok := AsProposalPending(err); ok {
		t.Fatal("应直接拒绝，而不是记成待确认操作")
	}
}

func hasEvent(evs []*store.EventRow, typ string) bool {
	for _, e := range evs {
		if e.Type == typ {
			return true
		}
	}
	return false
}
