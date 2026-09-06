package app

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

func backdatePoll(t *testing.T, a *App, userCode string) {
	t.Helper()
	if _, err := a.Store.Pool.Exec(t.Context(), `update agent_device_codes set last_polled_at = now() - interval '1 minute' where user_code=$1`, userCode); err != nil {
		t.Fatal(err)
	}
}

// 设备码授权全程：申请 → 网页看到 → 批准（创建 Agent、动态）→ 轮询拿令牌（只一次）→ 令牌能用 → 连接检查。
func TestDeviceAuthorizationFlow(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	zh := i18n.ZhCN

	req, err := a.RequestDeviceCode(ctx, "claude-code", "我的 Claude")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[BCDFGHJKLMNPQRSTVWXZ]{4}-[2-9]{4}$`).MatchString(req.UserCode) || !strings.HasPrefix(req.DeviceCode, "dvc_") || req.Interval != 5 || req.ExpiresIn != 900 {
		t.Fatalf("申请结果形状不对：%+v", req)
	}
	if !strings.Contains(req.VerificationURL, "/agents/connect/?code="+req.UserCode) {
		t.Fatalf("验证地址应带验证码：%s", req.VerificationURL)
	}
	if _, err := a.RequestDeviceCode(ctx, "vim", ""); err == nil || !strings.Contains(err.Error(), "运行环境只能是") {
		t.Fatalf("未知运行环境应被拒并给整句，实际 %v", err)
	}

	// 轮询：先 pending，紧接着再问就是 429
	p, err := a.PollDevice(ctx, req.DeviceCode)
	if err != nil || p.Status != "pending" || p.Token != "" {
		t.Fatalf("批准前应是 pending，实际 %+v（%v）", p, err)
	}
	_, err = a.PollDevice(ctx, req.DeviceCode)
	var ue *UserError
	if err == nil || !asUserError(err, &ue) || ue.Status != 429 {
		t.Fatalf("轮询过快应 429，实际 %v", err)
	}

	// 网页按验证码查（小写、无横线也认）
	dv, err := a.DeviceRequestByUserCode(ctx, yi, strings.ToLower(strings.ReplaceAll(req.UserCode, "-", "")))
	if err != nil || dv.Status != "pending" || dv.Client != "claude-code" || dv.ClientTitle != "Claude Code" || dv.Name != "我的 Claude" {
		t.Fatalf("网页应看到待批准的申请，实际 %+v（%v）", dv, err)
	}
	if _, err := a.DeviceRequestByUserCode(ctx, yi, "ZZZZ-9999"); err == nil {
		t.Fatal("不存在的验证码应 404")
	}

	// Agent 不能批准
	_, tok0, _ := a.RegisterAgent(ctx, yi, RegisterAgentInput{Name: "旁观 Agent"})
	as0, _ := a.SessionFromAgentToken(ctx, tok0)
	if _, _, err := a.ApproveDevice(ctx, as0, req.UserCode, ApproveDeviceInput{}); err == nil || !strings.Contains(err.Error(), i18n.Tr(zh, "err.device_agent")) {
		t.Fatalf("Agent 批准应被拒，实际 %v", err)
	}

	// 乙批准：三选一的授权
	grants, err := ParseGrantChoices(map[string]string{"execute": "allow", "comment": "with_approval", "review": "deny"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseGrantChoices(map[string]string{"execute": "maybe"}); err == nil || !strings.Contains(err.Error(), "allow、with_approval 或 deny") {
		t.Fatalf("坏的取值应给整句，实际 %v", err)
	}
	ag, dv, err := a.ApproveDevice(ctx, yi, req.UserCode, ApproveDeviceInput{Grants: grants, MaxConcurrent: 2})
	if err != nil {
		t.Fatal(err)
	}
	if ag.OwnerMemberID != yi.MemberID || ag.Runtime != "claude-code" || ag.Name != "我的 Claude" || ag.MaxConcurrent != 2 {
		t.Fatalf("Agent 应归批准人所有、运行时按申请，实际 %+v", ag)
	}
	if ag.Grants[domain.GrantExecute] != domain.GrantDirect || ag.Grants[domain.GrantComment] != domain.GrantWithApproval {
		t.Fatalf("授权映射不对：%v", ag.Grants)
	}
	if _, ok := ag.Grants[domain.GrantReview]; ok {
		t.Fatalf("deny 的授权不该进表：%v", ag.Grants)
	}
	if dv.Status != "approved" || dv.Agent == nil || dv.Agent.ID != ag.ID || dv.ApprovedBy == nil || dv.ApprovedBy.ID != yi.MemberID {
		t.Fatalf("批准后的申请视图不对：%+v", dv)
	}
	// 再批准 / 拒绝都不行
	if _, _, err := a.ApproveDevice(ctx, yi, req.UserCode, ApproveDeviceInput{}); err == nil || !strings.Contains(err.Error(), "已经处理过了") {
		t.Fatalf("重复批准应被拒，实际 %v", err)
	}
	// 动态
	connected := false
	_ = a.Store.WithOrg(ctx, yi.OrgID, func(tx pgx.Tx) error {
		evs, _ := a.Store.ListEvents(ctx, tx, "", 50)
		for _, e := range evs {
			if e.Type == "AgentConnected" && e.Data["agent_id"] == ag.ID && e.Data["client"] == "claude-code" {
				connected = true
			}
		}
		return nil
	})
	if !connected {
		t.Fatal("批准应产生 AgentConnected 动态")
	}

	// 轮询拿令牌：只给一次
	backdatePoll(t, a, req.UserCode)
	p, err = a.PollDevice(ctx, req.DeviceCode)
	if err != nil || p.Status != "approved" || !strings.HasPrefix(p.Token, "axm_") || p.Agent == nil || p.Agent.Name != "我的 Claude" || !strings.HasSuffix(p.MCPURL, "/mcp") {
		t.Fatalf("批准后应给令牌，实际 %+v（%v）", p, err)
	}
	token := p.Token
	backdatePoll(t, a, req.UserCode)
	p, err = a.PollDevice(ctx, req.DeviceCode)
	if err != nil || p.Status != "approved" || p.Token != "" {
		t.Fatalf("令牌只给一次，第二次不该再有，实际 %+v（%v）", p, err)
	}
	as, err := a.SessionFromAgentToken(ctx, token)
	if err != nil || as.AgentID != ag.ID || as.MemberID != yi.MemberID {
		t.Fatalf("令牌应能登录为该 Agent，实际 %+v（%v）", as, err)
	}

	// 连接检查：令牌解析时已算一次心跳；记一次工具调用后能看到工具名
	chk, err := a.CheckAgent(ctx, yi, ag.ID)
	if err != nil || !chk.Connected || !chk.Online {
		t.Fatalf("令牌用过后应视为已连接，实际 %+v（%v）", chk, err)
	}
	a.RecordAgentTool(ctx, as, "whoami")
	chk, _ = a.CheckAgent(ctx, yi, ag.ID)
	if chk.LastTool != "whoami" || !strings.Contains(chk.Hint, "whoami") {
		t.Fatalf("连接检查应带最近的工具名，实际 %+v", chk)
	}
	// 别人的 Agent 看不到
	_, _, zhSess := newTestOrg(t, a, ctx)
	if _, err := a.CheckAgent(ctx, zhSess, ag.ID); err == nil {
		t.Fatal("别的组织不该能查这个 Agent")
	}
	_ = jia

	// 拒绝路径
	req2, _ := a.RequestDeviceCode(ctx, "cursor", "")
	if dv, err := a.DenyDevice(ctx, yi, req2.UserCode); err != nil || dv.Status != "denied" {
		t.Fatalf("拒绝应成功，实际 %+v（%v）", dv, err)
	}
	backdatePoll(t, a, req2.UserCode)
	if p, err := a.PollDevice(ctx, req2.DeviceCode); err != nil || p.Status != "denied" || p.Token != "" {
		t.Fatalf("拒绝后轮询应是 denied，实际 %+v（%v）", p, err)
	}

	// 过期路径
	d3, code3, err := a.Store.CreateDeviceCode(ctx, a.Store.Pool, "codex", "", -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if dv, err := a.DeviceRequestByUserCode(ctx, yi, d3.UserCode); err != nil || dv.Status != "expired" {
		t.Fatalf("过期的申请应显示 expired，实际 %+v（%v）", dv, err)
	}
	if _, _, err := a.ApproveDevice(ctx, yi, d3.UserCode, ApproveDeviceInput{}); err == nil || !strings.Contains(err.Error(), "已经过期") {
		t.Fatalf("过期后批准应被拒，实际 %v", err)
	}
	if p, err := a.PollDevice(ctx, code3); err != nil || p.Status != "expired" {
		t.Fatalf("过期后轮询应是 expired，实际 %+v（%v）", p, err)
	}
	if n, err := a.ExpireDeviceCodes(ctx); err != nil || n < 1 {
		t.Fatalf("巡检应把过期申请标掉，实际 %d（%v）", n, err)
	}
	if _, err := a.PollDevice(ctx, "dvc_nope"); err == nil {
		t.Fatal("无效设备码应 404")
	}
}

func asUserError(err error, target **UserError) bool {
	ue, ok := err.(*UserError)
	if ok {
		*target = ue
	}
	return ok
}
