package directory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 飞书发消息：卡片正文与按钮、receive_id_type=open_id；自检把权限类错误码翻成待处理，把"收件人不存在"当作权限已具备。
func TestFeishuMessaging(t *testing.T) {
	var got map[string]string
	mode := "ok"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "tenant_access_token": "t-1", "expire": 7200})
		case "/open-apis/im/v1/messages":
			if r.URL.Query().Get("receive_id_type") != "open_id" || r.Header.Get("Authorization") != "Bearer t-1" {
				t.Errorf("参数不符 %v %v", r.URL.Query(), r.Header)
			}
			json.NewDecoder(r.Body).Decode(&got)
			switch mode {
			case "noperm":
				json.NewEncoder(w).Encode(map[string]any{"code": 99991672, "msg": "Access denied: no permission im:message"})
			case "nouser":
				json.NewEncoder(w).Encode(map[string]any{"code": 230001, "msg": "invalid receive_id"})
			default:
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"message_id": "om_1"}})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f, _ := NewFeishu(map[string]string{"app_id": "cli_1", "app_secret": "good"}, Options{})
	f.BaseURL = srv.URL
	ctx := context.Background()
	msg := Message{Kind: "proposal", Title: "小李的 Agent 提交了一个待确认操作，等你确认", Text: "确认后会把任务改为已完成", URL: "https://axiom.example/", Open: "打开"}
	if err := f.SendDirect(ctx, "ou-1", msg); err != nil {
		t.Fatal(err)
	}
	if got["receive_id"] != "ou-1" || got["msg_type"] != "interactive" {
		t.Fatalf("应发卡片给 ou-1 %+v", got)
	}
	var card map[string]any
	json.Unmarshal([]byte(got["content"]), &card)
	if !strings.Contains(got["content"], "https://axiom.example/") || !strings.Contains(got["content"], "等你确认") || !strings.Contains(got["content"], "\"打开\"") {
		t.Fatalf("卡片应带标题、链接与按钮 %s", got["content"])
	}
	if err := f.SendDirect(ctx, "ou-1", Message{Title: "只有一句话"}); err != nil || got["msg_type"] != "text" || !strings.Contains(got["content"], "只有一句话") {
		t.Fatalf("没链接时应发纯文本 %+v %v", got, err)
	}
	// 自检
	if c := f.DiagnoseMessaging(ctx); c.Status != CheckOK || c.Key != "messaging" {
		t.Fatalf("发得出去应通过 %+v", c)
	}
	mode = "nouser"
	if c := f.DiagnoseMessaging(ctx); c.Status != CheckOK || !strings.Contains(c.Detail.In("zh-CN"), "invalid receive_id") {
		t.Fatalf("收件人不存在说明权限已有 %+v", c)
	}
	mode = "noperm"
	c := f.DiagnoseMessaging(ctx)
	if c.Status != CheckTodo || c.Blocking || !strings.Contains(c.Detail.In("zh-CN"), "im:message") || !strings.Contains(c.Fix.In("zh-CN"), "以应用的身份发消息") || c.FixURL != "https://open.feishu.cn/app/cli_1/auth" {
		t.Fatalf("没权限应是待处理并指向权限页 %+v", c)
	}
	if !strings.Contains(c.Title.In("en-US"), "as the app") {
		t.Fatalf("要有英文标题 %+v", c.Title)
	}
}

// 企业微信发消息：用应用 Secret 换令牌、textcard 带按钮、invaliduser 当作拒绝；自检读 agent/get。
func TestWeComMessaging(t *testing.T) {
	var got map[string]any
	mode := "ok"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			if q.Get("corpsecret") != "app-secret" {
				json.NewEncoder(w).Encode(map[string]any{"errcode": 40001, "errmsg": "invalid credential"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "access_token": "at-1", "expires_in": 7200})
		case "/cgi-bin/agent/get":
			if q.Get("access_token") != "at-1" || q.Get("agentid") != "1000002" {
				json.NewEncoder(w).Encode(map[string]any{"errcode": 301002, "errmsg": "no privilege to access/modify contact/party/agent"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "name": "AxiomOS 提醒"})
		case "/cgi-bin/message/send":
			if r.Method != http.MethodPost || q.Get("access_token") != "at-1" {
				t.Errorf("应带令牌 POST %v", q)
			}
			json.NewDecoder(r.Body).Decode(&got)
			if mode == "invalid" {
				json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok", "invaliduser": "zhangsan"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	w, _ := NewWeComMessenger(map[string]string{"corp_id": "ww1", "corp_secret": "contacts-secret", "agent_id": "1000002", "app_secret": "app-secret"}, Options{})
	w.BaseURL = srv.URL
	ctx := context.Background()
	if err := w.SendDirect(ctx, "zhangsan", Message{Title: "待你验收：写文档", Text: "小李「提交」", URL: "https://axiom.example/tasks/t1/", Open: "打开"}); err != nil {
		t.Fatal(err)
	}
	card, _ := got["textcard"].(map[string]any)
	if got["touser"] != "zhangsan" || got["agentid"] != "1000002" || got["msgtype"] != "textcard" || card["url"] != "https://axiom.example/tasks/t1/" || card["btntxt"] != "打开" || card["title"] != "待你验收：写文档" {
		t.Fatalf("textcard 不对 %+v", got)
	}
	mode = "invalid"
	if err := w.SendDirect(ctx, "zhangsan", Message{Title: "x"}); err == nil || !strings.Contains(err.Error(), "invaliduser") {
		t.Fatalf("invaliduser 应算失败: %v", err)
	}
	if c := w.DiagnoseMessaging(ctx); c.Status != CheckOK || !strings.Contains(c.Detail.In("zh-CN"), "AxiomOS 提醒") {
		t.Fatalf("agent/get 通过即可发 %+v", c)
	}
	bad, _ := NewWeComMessenger(map[string]string{"corp_id": "ww1", "corp_secret": "contacts-secret"}, Options{})
	bad.BaseURL = srv.URL
	if c := bad.DiagnoseMessaging(ctx); c.Status != CheckTodo || !strings.Contains(c.Fix.In("zh-CN"), "AgentId") {
		t.Fatalf("没填应用凭据应是待处理 %+v", c)
	}
	wrong, _ := NewWeComMessenger(map[string]string{"corp_id": "ww1", "corp_secret": "contacts-secret", "agent_id": "1000002", "app_secret": "nope"}, Options{})
	wrong.BaseURL = srv.URL
	if c := wrong.DiagnoseMessaging(ctx); c.Status != CheckTodo || c.Blocking || !strings.Contains(c.Detail.In("zh-CN"), "invalid credential") {
		t.Fatalf("Secret 错应是待处理并带原话 %+v", c)
	}
}

// webhook：JSON 载荷形状、HMAC-SHA256 签名头、非 2xx 算拒绝。
func TestWebhook(t *testing.T) {
	var body []byte
	var sig, kindHdr string
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		sig = r.Header.Get("X-AxiomOS-Signature")
		kindHdr = r.Header.Get("X-AxiomOS-Kind")
		w.WriteHeader(status)
		w.Write([]byte("nope"))
	}))
	defer srv.Close()
	wh, err := NewWebhook(map[string]string{"url": srv.URL + "/hook", "secret": "s3cret"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	msg := Message{Kind: "assigned", Title: "你有一个新任务：写文档", Text: "由甲指派", URL: "https://axiom.example/tasks/t1/", Recipient: Recipient{ID: "mem_1", Name: "乙"}, At: at}
	if err := wh.SendDirect(context.Background(), "", msg); err != nil {
		t.Fatal(err)
	}
	var p WebhookPayload
	if err := json.Unmarshal(body, &p); err != nil || p.Kind != "assigned" || p.Title != msg.Title || p.Text != "由甲指派" || p.URL != msg.URL || p.Recipient.ID != "mem_1" || p.Recipient.Name != "乙" || !p.At.Equal(at) {
		t.Fatalf("载荷不对 %s %v", body, err)
	}
	// 签名时间是发出去的这一刻（不是消息生成时间），并且要落在接收方的新鲜度窗口里
	if kindHdr != "assigned" || !strings.HasPrefix(sig, "t=") || !VerifySignature("s3cret", sig, body, 0) || VerifySignature("other", sig, body, 0) {
		t.Fatalf("签名不对 %q", sig)
	}
	if strings.HasPrefix(sig, "t="+strconv.FormatInt(at.Unix(), 10)+",") {
		t.Fatalf("签名时间不该用消息生成时间 %q", sig)
	}
	// 旧签名重放：时间戳超出窗口就不认
	oldTS := time.Now().Add(-10 * time.Minute).Unix()
	oldSig := "t=" + strconv.FormatInt(oldTS, 10) + ",v1=" + Signature("s3cret", oldTS, body)
	if VerifySignature("s3cret", oldSig, body, 0) {
		t.Fatal("超出新鲜度窗口的签名应被拒（重放）")
	}
	if !VerifySignature("s3cret", oldSig, body, 30*time.Minute) {
		t.Fatal("窗口放宽后同一条签名应通过")
	}
	status = 500
	if err := wh.SendDirect(context.Background(), "", msg); err == nil || !strings.Contains(err.Error(), "HTTP 500: nope") {
		t.Fatalf("非 2xx 应算拒绝: %v", err)
	}
	if _, err := NewWebhook(map[string]string{"url": "ftp://x"}, Options{}); err == nil {
		t.Fatal("非 http(s) 地址应被拒")
	}
	plain, _ := NewWebhook(map[string]string{"url": srv.URL}, Options{})
	status = 204
	if err := plain.SendDirect(context.Background(), "", msg); err != nil || sig != "" {
		t.Fatalf("没密钥就不签名 %v %q", err, sig)
	}
}

type fakeSMTP struct {
	cfg SMTPConfig
	to  string
	raw []byte
	err error
}

func (f *fakeSMTP) Send(ctx context.Context, cfg SMTPConfig, to string, raw []byte) error {
	f.cfg, f.to, f.raw = cfg, to, raw
	return f.err
}

// 邮件：组织配置优先于环境变量、信件格式（主题 = 那句话、正文 = 那句话 + 链接）、没邮箱算拒绝。
func TestEmail(t *testing.T) {
	old := SendMail
	f := &fakeSMTP{}
	SendMail = f
	defer func() { SendMail = old }()
	t.Setenv("SMTP_URL", "smtp://ops:pw@mail.example:2525")
	t.Setenv("SMTP_FROM", "AxiomOS <no-reply@example.com>")
	e, err := NewEmail(map[string]string{}, Options{})
	if err != nil || e.Config.Host != "mail.example" || e.Config.Port != 2525 || e.Config.Username != "ops" || e.Config.Password != "pw" || e.Config.From != "AxiomOS <no-reply@example.com>" {
		t.Fatalf("应用环境变量的默认 %+v %v", e, err)
	}
	e, err = NewEmail(map[string]string{"host": "smtp.corp", "port": "465", "username": "u", "password": "p"}, Options{})
	if err != nil || e.Config.Host != "smtp.corp" || e.Config.Port != 465 || e.Config.From != "AxiomOS <no-reply@example.com>" {
		t.Fatalf("组织填了主机就用组织的，发件人回落到环境变量 %+v %v", e, err)
	}
	msg := Message{Kind: "review", Title: "待你验收：写文档", Text: "乙「提交」", URL: "https://axiom.example/tasks/t1/", Open: "打开", At: time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)}
	if err := e.SendDirect(context.Background(), "yi@corp.test", msg); err != nil {
		t.Fatal(err)
	}
	raw := string(f.raw)
	if f.to != "yi@corp.test" || f.cfg.Host != "smtp.corp" || !strings.Contains(raw, "To: yi@corp.test\r\n") || !strings.Contains(raw, "Subject: =?utf-8?q?") || !strings.Contains(raw, "\r\n\r\n乙「提交」\r\n\r\n打开：https://axiom.example/tasks/t1/\r\n") || !strings.Contains(raw, "X-AxiomOS-Kind: review") {
		t.Fatalf("信件格式不对:\n%s", raw)
	}
	if err := e.SendDirect(context.Background(), "", msg); err == nil {
		t.Fatal("没邮箱应算拒绝")
	}
	t.Setenv("SMTP_URL", "")
	if _, err := NewEmail(map[string]string{}, Options{}); err == nil {
		t.Fatal("既没组织配置也没环境变量时建不出客户端")
	}
	p, _ := Lookup("email")
	if p.MessagingReady(map[string]string{}, nil) {
		t.Fatal("没主机也没环境变量时不算已配置")
	}
	if !p.MessagingReady(map[string]string{"host": "x"}, nil) || p.CanSync() || !p.CanMessage() {
		t.Fatal("填了主机就算已配置；邮件不能当 IM 集成")
	}
}

// 注册表：只发消息的提供方不进 Providers()，进 MessagingProviders()；飞书、企业微信两边都在。
func TestMessagingRegistry(t *testing.T) {
	keys := func(ps []Provider) []string {
		out := []string{}
		for _, p := range ps {
			out = append(out, p.Key)
		}
		return out
	}
	if got := strings.Join(keys(Providers()), ","); got != "feishu,wecom" {
		t.Fatalf("IM 集成提供方应只有飞书与企业微信 %s", got)
	}
	if got := strings.Join(keys(MessagingProviders()), ","); got != "feishu,wecom,email,webhook" {
		t.Fatalf("发消息提供方 %s", got)
	}
	if p, _ := Lookup("wecom"); len(p.MessagingFields) != 2 || p.MessagingFields[0].Key != "agent_id" || !p.MessagingFields[1].Secret {
		t.Fatalf("企业微信发消息要多填 AgentId 与 Secret %+v", p.MessagingFields)
	}
}
