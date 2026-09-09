package app

import (
	"strings"
	"testing"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

// 在 Agent 里确认（ADR 0027）：凭证只发给 Agent 会话、只覆盖点名的那几条、每条一次、十分钟过期、
// 裁决以所有者本人的身份记账并注明「由 Agent 转达」。
func TestConfirmTicket(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	_, ag := agentSessionFor(t, a, ctx, yi, "转达 Agent", map[domain.Grant]domain.GrantMode{domain.GrantComment: domain.GrantWithApproval})
	t1, _ := a.CreateTask(ctx, jia, CreateTaskInput{Title: "任务一", AssigneeID: yi.MemberID, Ready: true})
	t2, _ := a.CreateTask(ctx, jia, CreateTaskInput{Title: "任务二", AssigneeID: yi.MemberID, Ready: true})
	_, err := a.AddComment(ctx, ag, t1.ID, "评论一", false)
	p1 := mustPending(t, err)
	_, err = a.AddComment(ctx, ag, t2.ID, "评论二", false)
	p2 := mustPending(t, err)

	// 人不能取凭证；Agent 取，范围只有一条时另一条碰不得
	if _, err := a.MintConfirmTicket(ctx, yi, ConfirmScope{Which: "all"}); err == nil {
		t.Fatal("成员会话不该拿到凭证")
	}
	tk, err := a.MintConfirmTicket(ctx, ag, ConfirmScope{Which: p1.Proposal.ID})
	if err != nil || len(tk.Proposals) != 1 || tk.Proposals[0].ID != p1.Proposal.ID {
		t.Fatalf("点名一条应只覆盖那一条，实际 %v %+v", err, tk)
	}
	if _, err := a.DecideViaAgent(ctx, ag, tk.Nonce, p2.Proposal.ID, "approve", "", ApproveOptions{}); err == nil || !strings.Contains(err.Error(), "不在这次确认的范围") {
		t.Fatalf("范围外的应被拒，实际 %v", err)
	}
	if _, err := a.DecideViaAgent(ctx, ag, "cfm_nope", p1.Proposal.ID, "approve", "", ApproveOptions{}); err == nil || !strings.Contains(err.Error(), "无效或已过期") {
		t.Fatalf("假凭证应被拒，实际 %v", err)
	}
	if _, err := a.DecideViaAgent(ctx, yi, tk.Nonce, p1.Proposal.ID, "approve", "", ApproveOptions{}); err == nil {
		t.Fatal("成员会话不该走转达路径")
	}
	// 用凭证确认：以乙本人的身份执行，动态记「由 Agent 转达」
	if _, err := a.DecideViaAgent(ctx, ag, tk.Nonce, p1.Proposal.ID, "approve", "", ApproveOptions{}); err != nil {
		t.Fatalf("转达确认失败：%v", err)
	}
	v, _ := a.GetProposal(ctx, jia, p1.Proposal.ID)
	if v.Status != domain.ProposalApproved || v.DecidedBy != yi.MemberID {
		t.Fatalf("应由乙本人确认，实际 %+v", v.Proposal)
	}
	found := false
	for _, e := range eventsOfType(t, a, ctx, orgID, "ProposalApproved") {
		if e.Data["proposal_id"] == p1.Proposal.ID && e.Data["via_agent_name"] == "转达 Agent" && e.ActorID == yi.MemberID {
			found = true
		}
	}
	if !found {
		t.Fatal("动态应记在乙名下并注明由 Agent 转达")
	}
	if !strings.Contains(EventSummary(eventsOfType(t, a, ctx, orgID, "ProposalApproved")[0], map[string]string{yi.MemberID: "乙", ag.Actor.ID: "转达 Agent"}, nil, nil, "zh-CN"), "转达") {
		t.Fatal("句子里应带「转达」")
	}
	// 每条只能用一次
	if _, err := a.DecideViaAgent(ctx, ag, tk.Nonce, p1.Proposal.ID, "approve", "", ApproveOptions{}); err == nil {
		t.Fatal("同一条不能用同一张凭证再裁一次")
	}
	// 拒绝要理由；凭证限定了决定就不能换
	tk2, err := a.MintConfirmTicket(ctx, ag, ConfirmScope{Which: "all", Decision: "reject"})
	if err != nil || len(tk2.Proposals) != 1 {
		t.Fatalf("all 应覆盖剩下那一条，实际 %v %+v", err, tk2)
	}
	if _, err := a.DecideViaAgent(ctx, ag, tk2.Nonce, p2.Proposal.ID, "approve", "", ApproveOptions{}); err == nil || !strings.Contains(err.Error(), "不能用来做别的决定") {
		t.Fatalf("限定为拒绝的凭证不能用来确认，实际 %v", err)
	}
	if _, err := a.DecideViaAgent(ctx, ag, tk2.Nonce, p2.Proposal.ID, "reject", "太短", ApproveOptions{}); err == nil {
		t.Fatal("理由太短应被拒")
	}
	if _, err := a.DecideViaAgent(ctx, ag, tk2.Nonce, p2.Proposal.ID, "reject", "这条评论没必要发，先不要。", ApproveOptions{}); err != nil {
		t.Fatalf("转达拒绝失败：%v", err)
	}
	if v, _ := a.GetProposal(ctx, jia, p2.Proposal.ID); v.Status != domain.ProposalRejected {
		t.Fatalf("应已拒绝，实际 %s", v.Status)
	}
	// 没有待确认的就不发凭证；过期的凭证会被清掉
	if _, err := a.MintConfirmTicket(ctx, ag, ConfirmScope{Which: "all"}); err == nil || !strings.Contains(err.Error(), "没有等你确认") {
		t.Fatalf("没有待确认时应拒发，实际 %v", err)
	}
	a.confirmGrants.Store("cfm_old", &confirmGrant{OrgID: orgID, AgentID: ag.AgentID, MemberID: yi.MemberID, Allowed: map[string]bool{"x": true}, ExpiresAt: time.Now().Add(-time.Minute)})
	if n := a.SweepConfirmTickets(time.Now()); n != 1 {
		t.Fatalf("应清掉 1 张过期凭证，实际 %d", n)
	}
}
