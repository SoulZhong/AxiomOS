package app

import (
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/domain"
)

// 「开始执行」需要人确认时，确认后的重放要以 Agent 身份开执行记录（所有者在网页里点确认那一刻）。
func TestBeginProposalReplay(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	// #12 那样的老任务不在委托内：关掉委托，走「需要人确认」那条路
	mandateEnabled = false
	defer func() { mandateEnabled = true }()
	// 和线上 #12 一样的路径：直接生效的 Agent 开始执行 → 执行记录超时结束 → 授权改成需要确认 → 再「开始执行」要人确认
	agent, ag := agentSessionFor(t, a, ctx, yi, "要确认的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect})
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "确认后再开始", AssigneeID: ag.Actor.ID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, ag, task.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	old := a.HeartbeatTimeout
	a.HeartbeatTimeout = -1
	if _, err := a.ExpireStaleRuns(ctx, yi.OrgID); err != nil {
		t.Fatal(err)
	}
	a.HeartbeatTimeout = old
	if _, err := a.UpdateAgent(ctx, yi, agent.ID, RegisterAgentInput{Name: agent.Name, Capabilities: agent.Capabilities, Grants: map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantWithApproval}, MaxConcurrent: 3}); err != nil {
		t.Fatal(err)
	}
	ag2, err := a.agentSession(ctx, yi.OrgID, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Begin(ctx, ag2, task.ID)
	pend, ok := err.(*ProposalPending)
	if !ok {
		t.Fatalf("应记成待确认操作，实际 %v", err)
	}
	if _, err := a.ApproveProposal(ctx, yi, pend.Proposal.ID); err != nil {
		t.Fatalf("所有者确认后应能开始执行，实际 %v", err)
	}
	wf, err := a.Workflow(ctx, ag2, task.ID)
	if err != nil || wf.ActiveRun == nil || wf.ActiveRun.ExecutorID != ag.Actor.ID {
		t.Fatalf("确认后应有一段以 Agent 为执行者的执行记录: %+v %v", wf, err)
	}
}

// 等着确认的「开始执行」，在任务经这个 Agent 自己（确认后重放）推进到下一阶段、负责人被清空之后，要作废而不是报「只有负责人才能开始」。
func TestBeginProposalStaleAfterOwnTransition(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	mandateEnabled = false
	defer func() { mandateEnabled = true }()
	_, ag := agentSessionFor(t, a, ctx, yi, "先提后变的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantWithApproval})
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "先提后变", AssigneeID: ag.Actor.ID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	// 起步要确认：确认后任务进入进行中并开了执行记录；把执行记录超时掉，再提一条「开始执行」等着
	_, err = a.Transition(ctx, ag, task.ID, "start", TransitionPayload{})
	startPend, ok := err.(*ProposalPending)
	if !ok {
		t.Fatalf("起步应记成待确认操作，实际 %v", err)
	}
	if _, err := a.ApproveProposal(ctx, yi, startPend.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	old := a.HeartbeatTimeout
	a.HeartbeatTimeout = -1
	if _, err := a.ExpireStaleRuns(ctx, yi.OrgID); err != nil {
		t.Fatal(err)
	}
	a.HeartbeatTimeout = old
	_, err = a.Begin(ctx, ag, task.ID)
	beginPend, ok := err.(*ProposalPending)
	if !ok {
		t.Fatalf("开始执行应记成待确认操作，实际 %v", err)
	}
	// 同一个 Agent 再提「提交」，所有者先确认了它：任务进入验收、负责人不再是它
	if _, err := a.AddArtifact(ctx, jia, task.ID, domain.Artifact{Type: "result", Title: "结果", Ref: "x"}); err != nil {
		t.Fatal(err)
	}
	_, err = a.Transition(ctx, ag, task.ID, "submit", TransitionPayload{})
	submitPend, ok := err.(*ProposalPending)
	if !ok {
		t.Fatalf("提交应记成待确认操作，实际 %v", err)
	}
	if _, err := a.ApproveProposal(ctx, yi, submitPend.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	// 现在确认那条旧的「开始执行」：应作废并说清楚，而不是「只有任务负责人…」
	_, err = a.ApproveProposal(ctx, yi, beginPend.Proposal.ID)
	if err == nil || !strings.Contains(err.Error(), "已失效") {
		t.Fatalf("旧的开始执行应作废，实际 %v", err)
	}
	stale, _ := a.ListProposals(ctx, yi, ProposalFilter{Status: domain.ProposalStale})
	found := false
	for _, p := range stale {
		if p.ID == beginPend.Proposal.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("那条开始执行应标为失效")
	}
}
