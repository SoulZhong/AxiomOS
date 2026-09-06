package app

import (
	"strings"
	"testing"
	"time"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

func timeNow() time.Time { return time.Now() }

// Agent 页可见范围（CONTEXT.md「Agent」）：普通成员只看到自己的 + 公共 Agent，不能吊销别人的；
// 组织负责人 / 持组织设置权限者看到全部。
func TestAgentVisibility(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)

	jiaAgent, _, err := a.RegisterAgent(ctx, jia, RegisterAgentInput{Name: "甲的私有 Agent"})
	if err != nil {
		t.Fatal(err)
	}
	shared, _, err := a.RegisterAgent(ctx, jia, RegisterAgentInput{Name: "公共 Agent", Shared: true})
	if err != nil {
		t.Fatal(err)
	}
	yiAgent, _, err := a.RegisterAgent(ctx, yi, RegisterAgentInput{Name: "乙的 Agent"})
	if err != nil {
		t.Fatal(err)
	}

	// 乙：只看到自己的 + 公共
	list, err := a.ListAgents(ctx, yi)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, ag := range list {
		ids[ag.ID] = true
	}
	if len(list) != 2 || !ids[yiAgent.ID] || !ids[shared.ID] || ids[jiaAgent.ID] {
		t.Fatalf("乙应只看到自己的与公共 Agent，实际 %v", ids)
	}
	if yi.CanManageAgent(shared) || yi.CanManageAgent(jiaAgent) || !yi.CanManageAgent(yiAgent) {
		t.Fatal("乙只能管理自己的 Agent")
	}

	// 乙不能吊销 / 修改别人的
	err = a.RevokeAgent(ctx, yi, jiaAgent.ID)
	ue, ok := err.(*UserError)
	if !ok || ue.Status != 403 || !strings.HasSuffix(ue.Render(i18n.ZhCN), "。") || !strings.Contains(ue.Render(i18n.ZhCN), "不是你的") {
		t.Fatalf("应被完整中文句子拒绝，实际 %v", err)
	}
	if _, err := a.UpdateAgent(ctx, yi, shared.ID, RegisterAgentInput{Name: "改名"}); err == nil {
		t.Fatal("乙不应能修改公共 Agent")
	}
	if _, err := a.UpdateAgent(ctx, yi, yiAgent.ID, RegisterAgentInput{Grants: map[domain.Grant]domain.GrantMode{domain.GrantComment: domain.GrantDirect}}); err != nil {
		t.Fatalf("乙应能改自己的 Agent: %v", err)
	}

	// 甲（负责人）：全部可见、可管理
	all, err := a.ListAgents(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("负责人应看到全部 3 个，实际 %d", len(all))
	}
	if !jia.CanManageAgent(yiAgent) {
		t.Fatal("负责人应能管理任何 Agent")
	}

	// 持组织设置权限但不是负责人的成员：也看到全部
	admin := *yi
	admin.IsOwner = false
	admin.Permissions = map[string]bool{"org_settings": true}
	all2, err := a.ListAgents(ctx, &admin)
	if err != nil || len(all2) != 3 {
		t.Fatalf("持组织设置权限者应看到全部，实际 %d %v", len(all2), err)
	}
	if err := a.RevokeAgent(ctx, &admin, jiaAgent.ID); err != nil {
		t.Fatalf("持组织设置权限者应能吊销: %v", err)
	}
}
