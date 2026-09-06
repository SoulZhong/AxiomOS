package api

import (
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 配置变更的动态只带字段名（没有配置值），句子要把字段名念成人话：「改了 GitHub 的访问令牌和接口地址」。
func TestConfigEventSentenceUsesFieldNames(t *testing.T) {
	ev := &store.EventRow{Event: domain.Event{Type: "CodePlatformConfigured", Data: map[string]any{
		"provider": "github", "fields": []any{"api_base", "token", "proxy_url"}}}}
	got := eventSummary(ev, refs{}, map[string]string{}, map[string]i18n.Text{}, i18n.ZhCN)
	for _, want := range []string{"GitHub", "访问令牌", "接口地址", "出网代理地址"} {
		if !strings.Contains(got, want) {
			t.Fatalf("句子应念出字段名，缺「%s」：%s", want, got)
		}
	}
	// 第一次配置时还是「配置了代码平台」
	fresh := &store.EventRow{Event: domain.Event{Type: "CodePlatformConfigured", Data: map[string]any{
		"provider": "github", "fresh": true, "fields": []any{"token"}}}}
	if got := eventSummary(fresh, refs{}, map[string]string{}, map[string]i18n.Text{}, i18n.ZhCN); !strings.Contains(got, "配置了代码平台") {
		t.Fatalf("第一次配置的句子不对：%s", got)
	}
	// 查看回调密钥
	seen := &store.EventRow{Event: domain.Event{Type: "CodeWebhookSecretRevealed", Data: map[string]any{"provider": "github"}}}
	if got := eventSummary(seen, refs{}, map[string]string{}, map[string]i18n.Text{}, i18n.ZhCN); !strings.Contains(got, "查看了") || !strings.Contains(got, "回调密钥") {
		t.Fatalf("查看回调密钥的句子不对：%s", got)
	}
	// IM 集成同一条规矩
	dir := &store.EventRow{Event: domain.Event{Type: "DirectoryConfigured", Data: map[string]any{
		"provider": "feishu", "fields": []any{"app_secret"}}}}
	if got := eventSummary(dir, refs{}, map[string]string{}, map[string]i18n.Text{}, i18n.ZhCN); !strings.Contains(got, "改了") {
		t.Fatalf("IM 集成改字段的句子不对：%s", got)
	}
}
