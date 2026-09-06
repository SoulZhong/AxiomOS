package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/store"
)

// 这一组是 Codex 安全审查的回归：设备码令牌只能被领走一次、轮询限流不能被并发绕过、
// 验证码不能被枚举、出网地址默认只许公网、回调只认接进来的仓库、回调密钥默认不回显、
// 外部链接地址要干净、配置动态里不留配置值。

func eventsOfType(t *testing.T, a *App, ctx context.Context, orgID, typ string) []*store.EventRow {
	t.Helper()
	var out []*store.EventRow
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := a.Store.ListEvents(ctx, tx, "", 500)
		if err != nil {
			return err
		}
		for _, e := range rows {
			if e.Type == typ {
				out = append(out, e)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// 令牌只能被领走一次：并发轮询里只有一个拿到令牌，其余要么 429、要么只回 approved。
func TestDeviceTokenDeliveredOnceUnderConcurrentPolling(t *testing.T) {
	a, ctx := testApp(t)
	_, _, yi := newTestOrg(t, a, ctx)

	req, err := a.RequestDeviceCode(ctx, "codex", "并发轮询")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.ApproveDevice(ctx, yi, req.UserCode, ApproveDeviceInput{}); err != nil {
		t.Fatal(err)
	}
	backdatePoll(t, a, req.UserCode)

	var mu sync.Mutex
	var wg sync.WaitGroup
	tokens, throttled, approved := []string{}, 0, 0
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := a.PollDevice(ctx, req.DeviceCode)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				var ue *UserError
				if asUserError(err, &ue) && ue.Status == 429 {
					throttled++
					return
				}
				t.Errorf("轮询不该出别的错：%v", err)
				return
			}
			if p.Status == "approved" {
				approved++
			}
			if p.Token != "" {
				tokens = append(tokens, p.Token)
			}
		}()
	}
	wg.Wait()
	if len(tokens) != 1 {
		t.Fatalf("并发轮询里令牌只能给一次，实际给了 %d 次", len(tokens))
	}
	if throttled == 0 {
		t.Fatalf("并发轮询应有被限流的（限流不能被并发绕过），实际 %d 次成功 %d 次限流", approved, throttled)
	}
	// 令牌能用，说明给出去的那一份是真的
	if as, err := a.SessionFromAgentToken(ctx, tokens[0]); err != nil || as == nil {
		t.Fatalf("拿到的令牌应能登录：%v", err)
	}
	// 之后再问只回状态，不再给令牌
	backdatePoll(t, a, req.UserCode)
	p, err := a.PollDevice(ctx, req.DeviceCode)
	if err != nil || p.Status != "approved" || p.Token != "" {
		t.Fatalf("第二次轮询只该回 approved，实际 %+v（%v）", p, err)
	}

	// 领取本身是一条原子语句：直接并发调它，只有一次能成
	req2, _ := a.RequestDeviceCode(ctx, "cursor", "领取原子性")
	if _, _, err := a.ApproveDevice(ctx, yi, req2.UserCode, ApproveDeviceInput{}); err != nil {
		t.Fatal(err)
	}
	d2, err := a.Store.DeviceByUserCode(ctx, a.Store.Pool, req2.UserCode)
	if err != nil {
		t.Fatal(err)
	}
	won := 0
	wg = sync.WaitGroup{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := a.Store.TakeDeviceToken(ctx, a.Store.Pool, d2.ID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Errorf("领取不该出错：%v", err)
			}
			if ok {
				won++
			}
		}()
	}
	wg.Wait()
	if won != 1 {
		t.Fatalf("并发领取只能有一次成功，实际 %d 次", won)
	}
}

// 验证码不能被登录用户逐个枚举：同一个账号连续查错 10 次就冷却，一段时间内不再受理。
func TestDeviceUserCodeLookupThrottled(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)

	req, err := a.RequestDeviceCode(ctx, "claude-code", "枚举测试")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := a.DeviceRequestByUserCode(ctx, yi, fmt.Sprintf("ZZZZ-%04d", 2000+i)); err == nil {
			t.Fatal("猜的验证码不该查到东西")
		}
	}
	_, err = a.DeviceRequestByUserCode(ctx, yi, "ZZZZ-9999")
	var ue *UserError
	if err == nil || !asUserError(err, &ue) || ue.Status != 429 || !strings.Contains(err.Error(), "验证码试得太多了") {
		t.Fatalf("查错够多次后应冷却并给整句，实际 %v", err)
	}
	// 冷却期内连真的验证码也不受理，批准这条路同样挡住
	if _, err := a.DeviceRequestByUserCode(ctx, yi, req.UserCode); err == nil || !strings.Contains(err.Error(), "验证码试得太多了") {
		t.Fatalf("冷却期内应一律拒绝，实际 %v", err)
	}
	if _, _, err := a.ApproveDevice(ctx, yi, req.UserCode, ApproveDeviceInput{}); err == nil || !strings.Contains(err.Error(), "验证码试得太多了") {
		t.Fatalf("批准也要过同一道限制，实际 %v", err)
	}
	// 别人不受影响，批准仍然要求申请是待批准且没过期
	if _, _, err := a.ApproveDevice(ctx, jia, req.UserCode, ApproveDeviceInput{}); err != nil {
		t.Fatalf("没超限的人应能正常批准，实际 %v", err)
	}
}

// 出网地址默认只许公网：代码平台的接口地址、出网代理、通知 webhook 的接收地址指向内网时当场拒绝，
// 私有化部署设置 AXIOMOS_ALLOW_PRIVATE_EGRESS=1 之后才能填。
func TestSavingPrivateEgressURLsRefused(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)
	t.Setenv(directory.AllowPrivateEgressEnv, "")

	_, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Provider: strp("github"),
		Credentials: creds("token", "ghp_x", "api_base", "http://127.0.0.1:8080/api/v3")})
	if err == nil || !strings.Contains(err.Error(), "这个地址指向内网") || !strings.Contains(err.Error(), "AXIOMOS_ALLOW_PRIVATE_EGRESS=1") {
		t.Fatalf("内网接口地址应被拒并说清怎么放开，实际 %v", err)
	}
	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Provider: strp("github"),
		Credentials: creds("token", "ghp_x", "api_base", "https://user:pw@ghe.example.com/api/v3")}); err == nil ||
		!strings.Contains(err.Error(), "不能带用户名和密码") {
		t.Fatalf("地址里带凭据应被拒，实际 %v", err)
	}
	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Provider: strp("github"),
		Credentials: creds("token", "ghp_x"), ProxyURL: strp("http://127.0.0.1:3128")}); err == nil ||
		!strings.Contains(err.Error(), "这个地址指向内网") {
		t.Fatalf("内网代理应被拒，实际 %v", err)
	}
	// 公网地址照常保存
	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Provider: strp("github"),
		Credentials: creds("token", "ghp_x", "api_base", "https://ghe.example.com/api/v3")}); err != nil {
		t.Fatalf("公网接口地址应能保存，实际 %v", err)
	}
	// 通知 webhook 的接收地址同样
	if _, err := a.SetNotifyPolicy(ctx, jia, NotifyPolicyInput{Channels: map[string]NotifyChannelInput{
		"webhook": {Config: map[string]string{"url": "http://127.0.0.1:9/hook"}}}}); err == nil ||
		!strings.Contains(err.Error(), "这个地址指向内网") {
		t.Fatalf("内网 webhook 接收地址应被拒，实际 %v", err)
	}
	if _, err := a.SetNotifyPolicy(ctx, jia, NotifyPolicyInput{Channels: map[string]NotifyChannelInput{
		"webhook": {Config: map[string]string{"url": "https://hooks.example.com/axiom"}}}}); err != nil {
		t.Fatalf("公网 webhook 接收地址应能保存，实际 %v", err)
	}

	// 私有化部署：打开开关后内网地址可以填
	t.Setenv(directory.AllowPrivateEgressEnv, "1")
	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Provider: strp("gitlab"),
		Credentials: creds("token", "glpat-x", "api_base", "http://127.0.0.1:8080/api/v4")}); err != nil {
		t.Fatalf("打开内网出网后应能保存，实际 %v", err)
	}
	if _, err := a.SetNotifyPolicy(ctx, jia, NotifyPolicyInput{Channels: map[string]NotifyChannelInput{
		"webhook": {Config: map[string]string{"url": "http://127.0.0.1:9/hook"}}}}); err != nil {
		t.Fatalf("打开内网出网后 webhook 地址应能保存，实际 %v", err)
	}
}

// 回调只认组织接进来并且还启用着的仓库：验过签也不能拿一个没接入的仓库伪造事件。
func TestCodeWebhookIgnoresRepoNotEnabled(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi, _ := codeTestOrg(t, a, ctx)
	secret := webhookSecretOf(t, a, ctx, jia)

	task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "改登录", AssigneeID: yi.MemberID})
	if err != nil {
		t.Fatal(err)
	}
	before := task.State

	res, err := codeHook(t, a, ctx, orgID, secret, "e1", directory.FakeCodePayload{Event: "pr_opened", Repo: "evil/other", Number: 1,
		Title: "修 #" + itoa(task.Number), Link: true, URL: "https://code.example/pr/1", Status: "open", Actor: "mallory"})
	if err != nil {
		t.Fatalf("没接入的仓库应被忽略而不是报错，实际 %v", err)
	}
	if !strings.Contains(res.Note, "evil/other") || len(res.Tasks) != 0 || len(res.Applied) != 0 {
		t.Fatalf("没接入的仓库应什么都不做并给一句说明，实际 %+v", res)
	}
	if ls, err := a.TaskLinks(ctx, jia, task.ID); err != nil || len(ls) != 0 {
		t.Fatalf("没接入的仓库不该挂上外部链接，实际 %+v（%v）", ls, err)
	}
	// 已接入的仓库照常处理
	if res, err = codeHook(t, a, ctx, orgID, secret, "e2", directory.FakeCodePayload{Event: "pr_opened", Repo: "acme/app", Number: 2,
		Title: "修 #" + itoa(task.Number), Link: true, URL: "https://code.example/pr/2", Status: "open", Actor: "alice"}); err != nil || len(res.Tasks) != 1 {
		t.Fatalf("接进来的仓库应正常处理，实际 %+v（%v）", res, err)
	}
	// 把仓库从接入列表里摘掉之后，同一个仓库的事件也不再处理
	none := []string{}
	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Repos: &none}); err != nil {
		t.Fatal(err)
	}
	res, err = codeHook(t, a, ctx, orgID, webhookSecretOf(t, a, ctx, jia), "e3", directory.FakeCodePayload{Event: "pr_merged", Repo: "acme/app", Number: 2,
		Title: "修 #" + itoa(task.Number), Link: true, URL: "https://code.example/pr/2", Status: "merged", Actor: "alice"})
	if err != nil || res.Note == "" || len(res.Applied) != 0 {
		t.Fatalf("摘掉的仓库不该再推进任务，实际 %+v（%v）", res, err)
	}
	_ = before

	// 自动建的链接必须落在这个平台的域名上
	repos := []string{"acme/app"}
	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Repos: &repos}); err != nil {
		t.Fatal(err)
	}
	res, err = codeHook(t, a, ctx, orgID, webhookSecretOf(t, a, ctx, jia), "e4", directory.FakeCodePayload{Repo: "acme/app", Number: 3,
		Title: "修 #" + itoa(task.Number), Link: true, URL: "https://phish.example/pr/3", Status: "open", Actor: "mallory"})
	if err != nil || !strings.Contains(res.Note, "phish.example") {
		t.Fatalf("不属于这个平台的链接地址应被拒，实际 %+v（%v）", res, err)
	}
	ls, err := a.TaskLinks(ctx, jia, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range ls {
		if strings.Contains(l.URL, "phish.example") {
			t.Fatalf("外站地址不该被挂成外部链接：%+v", l)
		}
	}
}

// 回调密钥默认不回显；显式要看的那一次记一条动态，还能换一把新的。
func TestCodeWebhookSecretRevealAndRotate(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, _, _ := codeTestOrg(t, a, ctx)

	plain, err := a.GetCodePlatform(ctx, jia)
	if err != nil || plain.WebhookSecret != "" || !plain.WebhookSecretSet {
		t.Fatalf("平常的读取只说密钥设没设，不给值：%+v（%v）", plain, err)
	}
	shown, err := a.RevealCodeWebhookSecret(ctx, jia)
	if err != nil || shown.WebhookSecret == "" {
		t.Fatalf("显式要看时应给出密钥：%+v（%v）", shown, err)
	}
	if len(eventsOfType(t, a, ctx, orgID, "CodeWebhookSecretRevealed")) != 1 {
		t.Fatal("查看回调密钥要留一条动态")
	}
	old := shown.WebhookSecret

	// 换一把新的
	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{RotateWebhookSecret: true}); err != nil {
		t.Fatal(err)
	}
	fresh := webhookSecretOf(t, a, ctx, jia)
	if fresh == old {
		t.Fatal("换密钥后应是新的一把")
	}
	if len(eventsOfType(t, a, ctx, orgID, "CodeWebhookSecretRotated")) != 1 {
		t.Fatal("换回调密钥要留一条动态")
	}
	// 旧密钥不再认
	if _, err := codeHook(t, a, ctx, orgID, old, "r1", directory.FakeCodePayload{Event: "pr_opened", Repo: "acme/app", Number: 1}); err == nil {
		t.Fatal("旧密钥不该还能验过签")
	}
}

// 外部链接地址：只认 http(s)，不能带用户名密码，不能有控制字符。
func TestTaskLinkURLHygiene(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "写文档"})
	if err != nil {
		t.Fatal(err)
	}
	bad := []string{"javascript:alert(1)", "JavaScript:alert(1)", "data:text/html,<script>", "file:///etc/passwd", "https://x.example/a\nb"}
	for _, u := range bad {
		if _, err := a.AddTaskLink(ctx, jia, task.ID, LinkInput{Kind: "doc", URL: u}); err == nil {
			t.Fatalf("%q 不该被接受", u)
		}
	}
	if _, err := a.AddTaskLink(ctx, jia, task.ID, LinkInput{Kind: "doc", URL: "https://user:pw@x.example/a"}); err == nil ||
		!strings.Contains(err.Error(), "不能带用户名和密码") {
		t.Fatalf("带凭据的链接应被拒并给整句，实际 %v", err)
	}
	if _, err := a.AddTaskLink(ctx, jia, task.ID, LinkInput{Kind: "doc", URL: "https://docs.example/a"}); err != nil {
		t.Fatalf("正常的 https 链接应能加，实际 %v", err)
	}
}

// 配置动态只记改了哪些字段的名字，不记配置值（接口地址、代理地址都可能暴露内网拓扑）。
func TestConfigEventsRecordFieldNamesNotValues(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, _ := newTestOrg(t, a, ctx)
	t.Setenv(directory.AllowPrivateEgressEnv, "1")

	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Provider: strp("github"),
		Credentials: creds("token", "ghp_x", "api_base", "https://ghe.internal.example/api/v3"),
		ProxyURL:    strp("http://proxy.internal.example:3128")}); err != nil {
		t.Fatal(err)
	}
	evs := eventsOfType(t, a, ctx, orgID, "CodePlatformConfigured")
	if len(evs) == 0 {
		t.Fatal("配置代码平台要产生动态")
	}
	last := evs[0]
	if _, ok := last.Data["credentials"]; ok {
		t.Fatalf("动态里不该有配置值：%v", last.Data)
	}
	if v, ok := last.Data["proxy_url"]; ok {
		t.Fatalf("动态里不该有代理地址：%v", v)
	}
	names := fmt.Sprint(last.Data["fields"])
	if !strings.Contains(names, "api_base") || !strings.Contains(names, "token") || !strings.Contains(names, "proxy_url") {
		t.Fatalf("动态应记下改了哪些字段：%v", last.Data)
	}
	if strings.Contains(fmt.Sprint(last.Data), "ghe.internal.example") || strings.Contains(fmt.Sprint(last.Data), "proxy.internal.example") {
		t.Fatalf("动态里不该出现任何地址：%v", last.Data)
	}

	// IM 集成那边同一条规矩
	fakeDirectoryApp(t, a)
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Provider: strp("fake"),
		Credentials: creds("app_id", "cli_1", "app_secret", "app-secret-plain"),
		ProxyURL:    strp("http://proxy.internal.example:3128")}); err != nil {
		t.Fatal(err)
	}
	devs := eventsOfType(t, a, ctx, orgID, "DirectoryConfigured")
	if len(devs) == 0 {
		t.Fatal("配置 IM 集成要产生动态")
	}
	d := devs[0]
	if _, ok := d.Data["credentials"]; ok {
		t.Fatalf("动态里不该有配置值：%v", d.Data)
	}
	if strings.Contains(fmt.Sprint(d.Data), "proxy.internal.example") || strings.Contains(fmt.Sprint(d.Data), "cli_1") {
		t.Fatalf("动态里不该出现配置值：%v", d.Data)
	}
	if !strings.Contains(fmt.Sprint(d.Data["fields"]), "app_id") {
		t.Fatalf("动态应记下改了哪些字段：%v", d.Data)
	}
}
