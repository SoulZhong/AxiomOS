package directory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/i18n"
)

// 注册表：三个代码平台只出现在 CodeHostProviders 里，不混进 IM 集成与通知通道。
func TestCodeHostRegistry(t *testing.T) {
	ps := CodeHostProviders()
	want := map[string]bool{"gitee": true, "github": true, "gitlab": true}
	if len(ps) != 3 {
		t.Fatalf("应有三个代码平台，实际 %v", keys(ps))
	}
	for _, p := range ps {
		if !want[p.Key] {
			t.Fatalf("意外的代码平台 %s", p.Key)
		}
		if p.Title.In(i18n.EnUS) == "" || len(p.Fields) == 0 || len(p.Prerequisites) == 0 || p.Tip.Text.IsZero() || p.ConsoleURL == nil {
			t.Fatalf("代码平台 %s 的声明不完整 %+v", p.Key, p)
		}
		if !hasField(p, "token", true) {
			t.Fatalf("代码平台 %s 应有一个保密的令牌字段", p.Key)
		}
		if p.CanSync() || p.CanMessage() {
			t.Fatalf("代码平台 %s 不该同时是 IM 集成或通知通道", p.Key)
		}
	}
	for _, p := range Providers() {
		if want[p.Key] {
			t.Fatalf("代码平台 %s 不该出现在 IM 集成列表里", p.Key)
		}
	}
	for _, p := range MessagingProviders() {
		if want[p.Key] {
			t.Fatalf("代码平台 %s 不该出现在通知通道列表里", p.Key)
		}
	}
	if gh, ok := Lookup("github"); !ok || !gh.CanHostCode() || !hasField(gh, "api_base", false) {
		t.Fatalf("GitHub 声明不符 %+v", gh)
	}
}

// GitHub：签名校验 + 六种事件的映射。
func TestGitHubWebhook(t *testing.T) {
	g, _ := NewGitHub(map[string]string{"token": "t"}, Options{})
	body := func(v any) []byte { b, _ := json.Marshal(v); return b }
	head := func(event, sig string) http.Header {
		h := http.Header{}
		h.Set("X-GitHub-Event", event)
		h.Set("X-GitHub-Delivery", "d-1")
		h.Set("X-Hub-Signature-256", sig)
		return h
	}
	pr := func(action string, draft, merged bool) []byte {
		return body(map[string]any{"action": action, "repository": map[string]any{"full_name": "acme/app"},
			"sender": map[string]any{"login": "alice"},
			"pull_request": map[string]any{"number": 12, "title": "修 #7 的登录问题", "body": "见 https://axiom.example/tasks/tsk_1",
				"html_url": "https://github.com/acme/app/pull/12", "draft": draft, "merged": merged, "head": map[string]any{"ref": "fix/login"}}})
	}
	// 签名不对就拒收
	b := pr("opened", false, false)
	if _, err := g.VerifyWebhook(head("pull_request", "sha256=deadbeef"), b, "s3cret"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("签名不对应被拒收，实际 %v", err)
	}
	sign := func(b []byte) string { return "sha256=" + hmacHex("s3cret", b) }
	ev, err := g.VerifyWebhook(head("pull_request", sign(b)), b, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != "pr_opened" || ev.Status != "open" || ev.Number != 12 || ev.Repo != "acme/app" || ev.Branch != "fix/login" ||
		ev.ActorName != "alice" || ev.DeliveryID != "d-1" || ev.ExternalID != "github:acme/app#12" || ev.LinkKind != "pr" {
		t.Fatalf("PR 打开的映射不符 %+v", ev)
	}
	if ev.Ref() != "PR #12" {
		t.Fatalf("句子里的说法应是 PR #12，实际 %s", ev.Ref())
	}
	cases := []struct {
		action        string
		draft, merged bool
		kind, status  string
	}{
		{"opened", true, false, "pr_opened", "draft"},
		{"ready_for_review", false, false, "pr_ready", "open"},
		{"converted_to_draft", true, false, "", "draft"},
		{"closed", false, true, "pr_merged", "merged"},
		{"closed", false, false, "pr_closed", "closed"},
		{"synchronize", false, false, "", "open"},
	}
	for _, c := range cases {
		b := pr(c.action, c.draft, c.merged)
		ev, err := g.VerifyWebhook(head("pull_request", sign(b)), b, "s3cret")
		if err != nil || ev.Kind != c.kind || ev.Status != c.status {
			t.Fatalf("%s 应映射成 %q/%q，实际 %q/%q（%v）", c.action, c.kind, c.status, ev.Kind, ev.Status, err)
		}
	}
	// 检查套件
	cs := body(map[string]any{"action": "completed", "repository": map[string]any{"full_name": "acme/app"},
		"sender": map[string]any{"login": "ci-bot"},
		"check_suite": map[string]any{"conclusion": "failure", "head_branch": "fix/login",
			"pull_requests": []any{map[string]any{"number": 12}}}})
	if ev, err := g.VerifyWebhook(head("check_suite", sign(cs)), cs, "s3cret"); err != nil || ev.Kind != "ci_failed" || ev.Number != 12 || ev.LinkKind != "" {
		t.Fatalf("检查失败的映射不符 %+v %v", ev, err)
	}
	ok := body(map[string]any{"action": "completed", "repository": map[string]any{"full_name": "acme/app"},
		"check_suite": map[string]any{"conclusion": "success", "head_branch": "main"}})
	if ev, err := g.VerifyWebhook(head("check_suite", sign(ok)), ok, "s3cret"); err != nil || ev.Kind != "ci_passed" {
		t.Fatalf("检查通过的映射不符 %+v %v", ev, err)
	}
	ping := body(map[string]any{"zen": "hi"})
	if _, err := g.VerifyWebhook(head("ping", sign(ping)), ping, "s3cret"); !errors.Is(err, ErrNotForUs) {
		t.Fatalf("ping 应被跳过，实际 %v", err)
	}
}

// GitLab：密钥头校验 + 合并请求与流水线的映射。
func TestGitLabWebhook(t *testing.T) {
	g, _ := NewGitLab(map[string]string{"token": "t"}, Options{})
	head := func(event, token string) http.Header {
		h := http.Header{}
		h.Set("X-Gitlab-Event", event)
		h.Set("X-Gitlab-Token", token)
		h.Set("X-Gitlab-Event-UUID", "u-1")
		return h
	}
	mr := func(action string, draft bool, changes map[string]any) []byte {
		p := map[string]any{"object_kind": "merge_request", "user": map[string]any{"username": "bob"},
			"project":           map[string]any{"path_with_namespace": "acme/app"},
			"object_attributes": map[string]any{"iid": 5, "title": "修 #7", "description": "", "source_branch": "fix/7", "url": "https://gitlab.com/acme/app/-/merge_requests/5", "action": action, "draft": draft}}
		if changes != nil {
			p["changes"] = changes
		}
		b, _ := json.Marshal(p)
		return b
	}
	if _, err := g.VerifyWebhook(head("Merge Request Hook", "wrong"), mr("open", false, nil), "s3cret"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("密钥不对应被拒收，实际 %v", err)
	}
	ev, err := g.VerifyWebhook(head("Merge Request Hook", "s3cret"), mr("open", false, nil), "s3cret")
	if err != nil || ev.Kind != "pr_opened" || ev.Number != 5 || ev.ExternalID != "gitlab:acme/app#5" || ev.ActorName != "bob" || ev.DeliveryID != "u-1" {
		t.Fatalf("合并请求打开的映射不符 %+v %v", ev, err)
	}
	for _, c := range []struct{ action, kind, status string }{
		{"merge", "pr_merged", "merged"}, {"close", "pr_closed", "closed"}, {"update", "", "open"},
	} {
		ev, err := g.VerifyWebhook(head("Merge Request Hook", "s3cret"), mr(c.action, false, nil), "s3cret")
		if err != nil || ev.Kind != c.kind || ev.Status != c.status {
			t.Fatalf("%s 应映射成 %q/%q，实际 %q/%q", c.action, c.kind, c.status, ev.Kind, ev.Status)
		}
	}
	// 草稿转正
	ready := mr("update", false, map[string]any{"draft": map[string]any{"previous": true, "current": false}})
	if ev, err := g.VerifyWebhook(head("Merge Request Hook", "s3cret"), ready, "s3cret"); err != nil || ev.Kind != "pr_ready" {
		t.Fatalf("草稿转正应映射成 pr_ready，实际 %q %v", ev.Kind, err)
	}
	if ev, err := g.VerifyWebhook(head("Merge Request Hook", "s3cret"), mr("open", true, nil), "s3cret"); err != nil || ev.Status != "draft" {
		t.Fatalf("草稿合并请求的状态应是 draft，实际 %q", ev.Status)
	}
	// 流水线
	pipe, _ := json.Marshal(map[string]any{"object_kind": "pipeline", "user": map[string]any{"username": "ci"},
		"project": map[string]any{"path_with_namespace": "acme/app"}, "object_attributes": map[string]any{"status": "failed", "ref": "fix/7"},
		"merge_request": map[string]any{"iid": 5, "url": "https://gitlab.com/acme/app/-/merge_requests/5", "source_branch": "fix/7"}})
	if ev, err := g.VerifyWebhook(head("Pipeline Hook", "s3cret"), pipe, "s3cret"); err != nil || ev.Kind != "ci_failed" || ev.Number != 5 {
		t.Fatalf("流水线失败的映射不符 %+v %v", ev, err)
	}
}

// Gitee：密码模式与签名模式都认；只产生 PR 类事件。
func TestGiteeWebhook(t *testing.T) {
	g, _ := NewGitee(map[string]string{"token": "t"}, Options{})
	mr := func(action string, merged bool) []byte {
		b, _ := json.Marshal(map[string]any{"action": action, "repository": map[string]any{"full_name": "acme/app"},
			"sender":       map[string]any{"login": "carol"},
			"pull_request": map[string]any{"number": 3, "title": "修 #7", "body": "", "html_url": "https://gitee.com/acme/app/pulls/3", "merged": merged, "head": map[string]any{"ref": "fix/7"}}})
		return b
	}
	h := http.Header{}
	h.Set("X-Gitee-Event", "Merge Request Hook")
	h.Set("X-Gitee-Token", "s3cret")
	if ev, err := g.VerifyWebhook(h, mr("open", false), "s3cret"); err != nil || ev.Kind != "pr_opened" || ev.ExternalID != "gitee:acme/app#3" || ev.ActorName != "carol" {
		t.Fatalf("密码模式应通过 %+v %v", ev, err)
	}
	if _, err := g.VerifyWebhook(h, mr("open", false), "other"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("密钥不对应被拒收，实际 %v", err)
	}
	// 签名模式
	sh := http.Header{}
	sh.Set("X-Gitee-Event", "Merge Request Hook")
	sh.Set("X-Gitee-Timestamp", "1700000000")
	sh.Set("X-Gitee-Token", giteeSignature("s3cret", "1700000000"))
	if ev, err := g.VerifyWebhook(sh, mr("merge", true), "s3cret"); err != nil || ev.Kind != "pr_merged" || ev.Status != "merged" {
		t.Fatalf("签名模式应通过并映射成 pr_merged %+v %v", ev, err)
	}
	push := http.Header{}
	push.Set("X-Gitee-Event", "Push Hook")
	push.Set("X-Gitee-Token", "s3cret")
	if _, err := g.VerifyWebhook(push, []byte(`{}`), "s3cret"); !errors.Is(err, ErrNotForUs) {
		t.Fatalf("推送事件应被跳过，实际 %v", err)
	}
	// 没有投递编号时用载荷指纹去重
	nod := http.Header{}
	nod.Set("X-Gitee-Event", "Merge Request Hook")
	nod.Set("X-Gitee-Token", "s3cret")
	ev, _ := g.VerifyWebhook(nod, mr("open", false), "s3cret")
	if ev.DeliveryID == "" || ev.DeliveryID != BodyDigest(mr("open", false)) {
		t.Fatalf("没有投递编号时应用载荷指纹，实际 %q", ev.DeliveryID)
	}
}

// GitHub 的仓库列表、建回调与三项接入检查（httptest 假服务）。
func TestGitHubReposAndChecks(t *testing.T) {
	created := 0
	hooks := []map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(401)
			w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		switch {
		case r.URL.Path == "/user/repos":
			json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "full_name": "acme/app", "html_url": "https://github.com/acme/app"}})
		case r.URL.Path == "/repos/acme/app/hooks" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(hooks)
		case r.URL.Path == "/repos/acme/app/hooks/9" && r.Method == http.MethodPatch:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			cfg, _ := in["config"].(map[string]any)
			hooks[0]["config"] = cfg
			w.Write([]byte(`{"id":9}`))
		case r.URL.Path == "/repos/acme/app/hooks" && r.Method == http.MethodPost:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			cfg, _ := in["config"].(map[string]any)
			hooks = append(hooks, map[string]any{"id": 9, "config": cfg})
			created++
			w.WriteHeader(201)
			w.Write([]byte(`{"id":9}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	callback := "https://axiom.example/api/v1/hooks/code/github/org_1"

	bad, _ := NewGitHub(map[string]string{"token": "wrong", "api_base": srv.URL}, Options{})
	cs, err := bad.DiagnoseCode(ctx, []string{"acme/app"}, callback)
	if err != nil {
		t.Fatal(err)
	}
	if b := FirstBlocked(cs); b == nil || b.Key != "credentials" || !strings.Contains(b.Detail.In(i18n.ZhCN), "Bad credentials") {
		t.Fatalf("令牌被拒时第一项检查应阻塞并带上平台原话 %+v", cs)
	}

	g, _ := NewGitHub(map[string]string{"token": "good", "api_base": srv.URL}, Options{})
	rs, err := g.ListRepos(ctx)
	if err != nil || len(rs) != 1 || rs[0].FullName != "acme/app" || rs[0].ID != "1" {
		t.Fatalf("仓库列表不符 %+v %v", rs, err)
	}
	// 还没建回调：webhook 一项阻塞
	cs, _ = g.DiagnoseCode(ctx, []string{"acme/app"}, callback)
	if b := FirstBlocked(cs); b == nil || b.Key != "webhook" {
		t.Fatalf("回调没建时应阻塞在 webhook 一项 %+v", cs)
	}
	if err := g.EnsureWebhook(ctx, "acme/app", callback, "s3cret"); err != nil || created != 1 {
		t.Fatalf("应建一条回调 %v %d", err, created)
	}
	if err := g.EnsureWebhook(ctx, "acme/app", callback, "s3cret"); err != nil || created != 1 {
		t.Fatalf("回调已存在时应改而不是再建一条 %v %d", err, created)
	}
	cs, _ = g.DiagnoseCode(ctx, []string{"acme/app"}, callback)
	if FirstBlocked(cs) != nil || len(cs) != 3 || cs[2].Status != CheckOK {
		t.Fatalf("建好回调后三项都该通过 %+v", cs)
	}
	// 还没选仓库：待处理，不阻塞
	cs, _ = g.DiagnoseCode(ctx, nil, callback)
	if FirstBlocked(cs) != nil || cs[2].Status != CheckTodo {
		t.Fatalf("没选仓库时 webhook 一项应是待处理 %+v", cs)
	}
}
