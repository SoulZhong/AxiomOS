package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// codeTestOrg 建一个接了测试代码平台的组织：仓库 acme/app 已选中，回调已建。
func codeTestOrg(t *testing.T, a *App, ctx context.Context) (orgID string, jia, yi *Session, host *directory.FakeCodeHost) {
	t.Helper()
	orgID, jia, yi = newTestOrg(t, a, ctx)
	host = directory.NewFakeCodeHost()
	directory.UseFakeCodeHost(host, "good-token")
	repos := []string{"acme/app"}
	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Provider: strp(directory.FakeCodeKey),
		Credentials: creds("token", "good-token"), Repos: &repos}); err != nil {
		t.Fatal(err)
	}
	return
}

// codeHook 造一条测试代码平台的回调请求。
func codeHook(t *testing.T, a *App, ctx context.Context, orgID, secret, delivery string, p directory.FakeCodePayload) (*WebhookResult, error) {
	t.Helper()
	body, _ := json.Marshal(p)
	h := http.Header{}
	h.Set("X-Fake-Token", secret)
	h.Set("X-Fake-Delivery", delivery)
	return a.HandleCodeWebhook(ctx, directory.FakeCodeKey, orgID, h, body)
}

func webhookSecretOf(t *testing.T, a *App, ctx context.Context, sess *Session) string {
	t.Helper()
	// 平常的 GET 不回显密钥，只说设没设；要拿到值得显式来一次
	plain, err := a.GetCodePlatform(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if plain.WebhookSecret != "" || !plain.WebhookSecretSet {
		t.Fatalf("默认不该回显回调密钥，只说设没设 %+v", plain)
	}
	v, err := a.RevealCodeWebhookSecret(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if v.WebhookSecret == "" || !v.WebhookSecretSet {
		t.Fatalf("显式要看时应给出回调签名密钥 %+v", v)
	}
	return v.WebhookSecret
}

// 配置：凭据按声明校验、保密字段不回显、令牌错时检查清单第一项阻塞并带平台原话、选仓库时建回调。
func TestCodePlatformConfigAndChecklist(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	host := directory.NewFakeCodeHost()
	directory.UseFakeCodeHost(host, "good-token")

	if _, err := a.GetCodePlatform(ctx, yi); err == nil {
		t.Fatal("没有组织设置权限的人不该能看代码平台配置")
	}
	// 未配置时向导停在第一步
	cl, err := a.CodePlatformChecklist(ctx, jia)
	if err != nil || cl.Next.Step != "credentials" || len(cl.Checks) != 0 {
		t.Fatalf("未配置时应停在填凭据 %+v %v", cl, err)
	}
	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Provider: strp(directory.FakeCodeKey), Credentials: creds()}); err == nil {
		t.Fatal("第一次保存必须填令牌")
	}
	// 令牌错：凭据一项阻塞，句子里带平台原话
	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Provider: strp(directory.FakeCodeKey), Credentials: creds("token", "wrong")}); err != nil {
		t.Fatal(err)
	}
	cl, err = a.CodePlatformChecklist(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if cl.Ready || len(cl.Checks) == 0 || cl.Checks[0].Key != "credentials" || cl.Checks[0].Status != "blocked" ||
		!strings.Contains(cl.Checks[0].Detail, "Bad credentials") {
		t.Fatalf("令牌错时第一项应阻塞并带平台原话 %+v", cl.Checks)
	}
	if res, err := a.TestCodePlatform(ctx, jia); err != nil || res.OK || !strings.Contains(res.Error, "Bad credentials") {
		t.Fatalf("连通性测试应报同一句话 %+v %v", res, err)
	}
	// 换成好令牌但还没选仓库
	if _, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Credentials: creds("token", "good-token")}); err != nil {
		t.Fatal(err)
	}
	cl, _ = a.CodePlatformChecklist(ctx, jia)
	if !cl.Ready || cl.Next.Step != "repos" {
		t.Fatalf("凭据好了但没选仓库时应提示挑仓库 %+v", cl)
	}
	if cl.WebhookURL == "" || !strings.Contains(cl.WebhookURL, "/hooks/code/"+directory.FakeCodeKey+"/"+orgID) {
		t.Fatalf("回调地址不符 %q", cl.WebhookURL)
	}
	// 现场读仓库
	rs, err := a.CodePlatformRepos(ctx, jia)
	if err != nil || len(rs) != 1 || rs[0].FullName != "acme/app" || rs[0].Enabled {
		t.Fatalf("仓库列表不符 %+v %v", rs, err)
	}
	// 选仓库 → 建回调
	repos := []string{"acme/app"}
	v, err := a.SaveCodePlatform(ctx, jia, CodePlatformInput{Repos: &repos})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Repos) != 1 || !v.Repos[0].HookOK || v.Repos[0].HookError != "" {
		t.Fatalf("应把回调建好 %+v", v.Repos)
	}
	if host.Hooks["acme/app"] == "" || host.Hooks["acme/app"] != webhookSecretOf(t, a, ctx, jia) {
		t.Fatalf("回调应带上本组织的签名密钥 %+v", host.Hooks)
	}
	if v.SecretsSet["token"] != true || v.Credentials["token"] != "" {
		t.Fatalf("保密字段只说是否已设置，不回显 %+v", v)
	}
	if !v.Configured {
		t.Fatalf("凭据齐全时应算已配置 %+v", v)
	}
	cl, _ = a.CodePlatformChecklist(ctx, jia)
	if !cl.Ready || cl.Next.Step != "done" || len(cl.Checks) != 3 {
		t.Fatalf("三项都通过后向导应完成 %+v", cl)
	}
	// 选一个看不到的仓库：不算失败，记在那一行上
	bad := []string{"acme/nope"}
	v, err = a.SaveCodePlatform(ctx, jia, CodePlatformInput{Repos: &bad})
	if err != nil || len(v.Repos) != 1 || v.Repos[0].HookOK || !strings.Contains(v.Repos[0].HookError, "acme/nope") {
		t.Fatalf("看不到的仓库应记一句说明 %+v %v", v.Repos, err)
	}
	// 动态
	if !hasEventOfType(t, a, ctx, orgID, "CodePlatformConfigured") {
		t.Fatal("配置代码平台要产生动态")
	}
}

func hasEventOfType(t *testing.T, a *App, ctx context.Context, orgID, typ string) bool {
	t.Helper()
	found := false
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := a.Store.ListEvents(ctx, tx, "", 300)
		if err != nil {
			return err
		}
		for _, e := range rows {
			if e.Type == typ {
				found = true
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return found
}

// 处理中途出错时要撤销去重占位，否则代码平台重投会被当成重复而永远丢掉这个事件（Codex 审查 P1）。
func TestCodeWebhookReleasesDedupeOnFailure(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi, _ := codeTestOrg(t, a, ctx)
	secret := webhookSecretOf(t, a, ctx, jia)

	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "修登录", TypeName: "bug", GoalID: goal.ID, AssigneeID: yi.MemberID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jia, task.ID, "confirm", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	payload := directory.FakeCodePayload{Event: "pr_opened", Repo: "acme/app", Number: 1,
		Title: "修 #" + itoa(task.Number), Link: true, URL: "https://code.example/pr/1", Status: "open", Actor: "alice"}

	// 第一次处理时人为让它失败：占位要被撤销
	a.failCodeEvent = true
	if _, err := codeHook(t, a, ctx, orgID, secret, "dz", payload); err == nil {
		t.Fatal("这一次应该报错")
	}
	a.failCodeEvent = false

	// 平台重投同一个投递编号：应该照常处理，而不是被当成重复
	res, err := codeHook(t, a, ctx, orgID, secret, "dz", payload)
	if err != nil {
		t.Fatal(err)
	}
	if res.Duplicate {
		t.Fatal("出错后重投不该被当成重复")
	}
	if len(res.Applied) != 1 {
		t.Fatalf("重投应把任务推到修复中 %+v", res)
	}
	// 再投一次（这次前面成功了）才算重复
	res2, err := codeHook(t, a, ctx, orgID, secret, "dz", payload)
	if err != nil || !res2.Duplicate {
		t.Fatalf("成功之后重投才算重复 %+v %v", res2, err)
	}
}

// 回调：验签、去重、按 #序号 认任务、建外部链接、内核推进状态、外部操作者写进动态。
func TestCodeWebhookAppliesTransition(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi, _ := codeTestOrg(t, a, ctx)
	secret := webhookSecretOf(t, a, ctx, jia)

	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "修登录", TypeName: "bug", GoalID: goal.ID, AssigneeID: yi.MemberID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jia, task.ID, "confirm", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	num := task.Number

	// 签名不对：拒收
	if _, err := codeHook(t, a, ctx, orgID, "wrong", "d1", directory.FakeCodePayload{Event: "pr_opened", Repo: "acme/app", Number: 1, Title: "修 #" + itoa(num), Link: true, URL: "https://code.example/pr/1", Status: "open", Actor: "alice"}); err == nil {
		t.Fatal("签名不对应被拒收")
	}
	// PR 打开：建链接 + 开始修复
	res, err := codeHook(t, a, ctx, orgID, secret, "d1", directory.FakeCodePayload{Event: "pr_opened", Repo: "acme/app", Number: 1,
		Title: "修 #" + itoa(num), Link: true, URL: "https://code.example/pr/1", Status: "open", Actor: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tasks) != 1 || res.Tasks[0] != task.ID || len(res.Applied) != 1 {
		t.Fatalf("应认出任务并走一步 %+v", res)
	}
	links, err := a.TaskLinks(ctx, jia, task.ID)
	if err != nil || len(links) != 1 || links[0].Kind != "pr" || links[0].Status != "open" || links[0].StatusTitle != "打开" || links[0].ActorName != "alice" {
		t.Fatalf("应建一条 PR 链接 %+v %v", links, err)
	}
	d, err := a.GetTaskDetail(ctx, jia, task.ID)
	if err != nil || d.Task.State != "fixing" || len(d.Links) != 1 {
		t.Fatalf("PR 打开应把 Bug 推到修复中 %v %+v", err, d.Task.State)
	}
	// 同一投递重发：什么都不做
	res2, err := codeHook(t, a, ctx, orgID, secret, "d1", directory.FakeCodePayload{Event: "pr_merged", Repo: "acme/app", Number: 1, Title: "修 #" + itoa(num), Link: true, URL: "https://code.example/pr/1", Status: "merged", Actor: "alice"})
	if err != nil || !res2.Duplicate {
		t.Fatalf("重发的投递应被识别出来 %+v %v", res2, err)
	}
	if d, _ := a.GetTaskDetail(ctx, jia, task.ID); d.Task.State != "fixing" {
		t.Fatalf("重发不该改状态，实际 %s", d.Task.State)
	}
	// PR 合并：链接更新 + 进入待验证（「代码 PR」交付物由回调补上）
	if _, err := codeHook(t, a, ctx, orgID, secret, "d2", directory.FakeCodePayload{Event: "pr_merged", Repo: "acme/app", Number: 1,
		Title: "修 #" + itoa(num), Link: true, URL: "https://code.example/pr/1", Status: "merged", Actor: "alice"}); err != nil {
		t.Fatal(err)
	}
	d, _ = a.GetTaskDetail(ctx, jia, task.ID)
	if d.Task.State != "fixed" {
		t.Fatalf("PR 合并应把 Bug 推到待验证，实际 %s", d.Task.State)
	}
	if len(d.Links) != 1 || d.Links[0].Status != "merged" || d.Links[0].StatusTitle != "已合并" {
		t.Fatalf("同一个 PR 应更新而不是新建一条 %+v", d.Links)
	}
	// 列表行上的 PR 小标
	ss, err := a.ListTaskSummaries(ctx, jia, store.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	got := false
	for _, s := range ss {
		if s.ID == task.ID {
			if s.PR == nil || s.PR.Status != "merged" || s.PR.Count != 1 {
				t.Fatalf("列表行应带 PR 小标 %+v", s.PR)
			}
			got = true
		}
	}
	if !got {
		t.Fatal("列表里应有这个任务")
	}
	// 动态：外部事件有句子，写清来源与外部用户
	if !hasEventOfType(t, a, ctx, orgID, "ExternalEventApplied") || !hasEventOfType(t, a, ctx, orgID, "ExternalLinkAdded") {
		t.Fatal("外部事件与外部链接都要产生动态")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// 认任务：标题里的 #序号、分支名里的 #序号、描述里的任务链接；认不出就什么都不做。
func TestCodeWebhookTaskMatching(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, _, _ := codeTestOrg(t, a, ctx)
	secret := webhookSecretOf(t, a, ctx, jia)
	goal, _ := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	t1, _ := a.CreateTask(ctx, jia, CreateTaskInput{Title: "甲任务", GoalID: goal.ID})
	t2, _ := a.CreateTask(ctx, jia, CreateTaskInput{Title: "乙任务", GoalID: goal.ID})

	// 标题里的序号
	res, err := codeHook(t, a, ctx, orgID, secret, "m1", directory.FakeCodePayload{Repo: "acme/app", Number: 7,
		Title: "修 #" + itoa(t1.Number) + " 的问题", Link: true, URL: "https://code.example/pr/7", Status: "open"})
	if err != nil || len(res.Tasks) != 1 || res.Tasks[0] != t1.ID {
		t.Fatalf("标题里的序号应认出任务 %+v %v", res, err)
	}
	// 分支名里的序号 + 描述里的任务链接：两个任务都算
	res, err = codeHook(t, a, ctx, orgID, secret, "m2", directory.FakeCodePayload{Repo: "acme/app", Number: 8,
		Branch: "fix/#" + itoa(t1.Number) + "-login", Body: "见 " + a.PublicURL + "/tasks/" + t2.ID + "/ 的说明",
		Link: true, URL: "https://code.example/pr/8", Status: "open"})
	if err != nil || len(res.Tasks) != 2 {
		t.Fatalf("分支名与描述里的引用都该算 %+v %v", res, err)
	}
	// 认不出：不动任何任务
	res, err = codeHook(t, a, ctx, orgID, secret, "m3", directory.FakeCodePayload{Repo: "acme/app", Number: 9,
		Title: "顺手改点格式", Link: true, URL: "https://code.example/pr/9", Status: "open"})
	if err != nil || len(res.Tasks) != 0 {
		t.Fatalf("认不出任务时什么都不该做 %+v %v", res, err)
	}
	// CI 事件不带序号，靠已经挂上的 PR 找回任务
	res, err = codeHook(t, a, ctx, orgID, secret, "m4", directory.FakeCodePayload{Event: "ci_failed", Repo: "acme/app", Number: 7, Branch: "fix/login"})
	if err != nil || len(res.Tasks) != 1 || res.Tasks[0] != t1.ID {
		t.Fatalf("CI 事件应靠 PR 找回任务 %+v %v", res, err)
	}
}

// 检查失败把需求推进「已阻塞」并给负责人一条「任务被阻塞」的通知。
func TestCodeWebhookBlockedNotification(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi, _ := codeTestOrg(t, a, ctx)
	secret := webhookSecretOf(t, a, ctx, jia)
	goal, _ := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "登录改版", TypeName: "requirement", GoalID: goal.ID,
		Participants: map[string]string{"designer": jia.MemberID, "developer": yi.MemberID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jia, task.ID, "start_design", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddArtifact(ctx, jia, task.ID, domain.Artifact{Type: "prd", Title: "需求文档"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jia, task.ID, "design_done", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := codeHook(t, a, ctx, orgID, secret, "b1", directory.FakeCodePayload{Event: "ci_failed", Repo: "acme/app",
		Title: "改 #" + itoa(task.Number), Actor: "ci-bot"}); err != nil {
		t.Fatal(err)
	}
	d, _ := a.GetTaskDetail(ctx, jia, task.ID)
	if d.Task.State != "blocked" {
		t.Fatalf("检查失败应把需求标记阻塞，实际 %s", d.Task.State)
	}
	ns, err := a.Notifications(ctx, yi, false)
	if err != nil {
		t.Fatal(err)
	}
	found := ""
	for _, n := range ns {
		if strings.Contains(n.Title, "任务被阻塞") {
			found = n.Title + " / " + n.Body
		}
	}
	if found == "" {
		t.Fatalf("负责人应收到一条任务被阻塞的通知 %+v", ns)
	}
	if !strings.Contains(found, "登录改版") {
		t.Fatalf("通知里要写清是哪个任务 %q", found)
	}
}

// 手工外部链接：增删、种类与地址校验、Agent 按「执行任务」授权走同一套规则。
func TestTaskLinksCRUDAndAgentRules(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	goal, _ := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	task, _ := a.CreateTask(ctx, jia, CreateTaskInput{Title: "写文档", GoalID: goal.ID, AssigneeID: yi.MemberID})

	if _, err := a.AddTaskLink(ctx, jia, task.ID, LinkInput{Kind: "wiki", URL: "https://x.example/a"}); err == nil {
		t.Fatal("不认识的种类应被拒绝")
	}
	if _, err := a.AddTaskLink(ctx, jia, task.ID, LinkInput{Kind: "doc", URL: "not a url"}); err == nil {
		t.Fatal("不是 http 地址应被拒绝")
	}
	l, err := a.AddTaskLink(ctx, jia, task.ID, LinkInput{Kind: "design", URL: "https://figma.example/f/1", Title: "登录页设计稿"})
	if err != nil || l.Kind != "design" || l.KindTitle != "设计稿" {
		t.Fatalf("应挂上一条设计稿链接 %+v %v", l, err)
	}
	// 任务说明与详情都带上链接
	b, err := a.Brief(ctx, yi, task.ID)
	if err != nil || len(b.Links) != 1 || b.Links[0].Title != "登录页设计稿" {
		t.Fatalf("任务说明里应带外部链接 %+v %v", b.Links, err)
	}
	// Agent：没有「执行任务」授权时被拒
	agNo, tokNo, err := a.RegisterAgent(ctx, yi, RegisterAgentInput{Name: "只会评论的 Agent",
		Grants: map[domain.Grant]domain.GrantMode{domain.GrantComment: domain.GrantDirect}})
	if err != nil {
		t.Fatal(err)
	}
	_ = agNo
	sessNo, _ := a.SessionFromAgentToken(ctx, tokNo)
	if _, err := a.AddTaskLink(ctx, sessNo, task.ID, LinkInput{Kind: "pr", URL: "https://code.example/pr/1"}); err == nil ||
		!strings.Contains(err.Error(), "执行任务") {
		t.Fatalf("没有执行任务授权的 Agent 应被拒并说清缺哪项 %v", err)
	}
	// Agent：授权是「需要人确认」时记一条待确认操作
	_, tokAsk, err := a.RegisterAgent(ctx, yi, RegisterAgentInput{Name: "要确认的 Agent",
		Grants: map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantWithApproval}})
	if err != nil {
		t.Fatal(err)
	}
	sessAsk, _ := a.SessionFromAgentToken(ctx, tokAsk)
	_, err = a.AddTaskLink(ctx, sessAsk, task.ID, LinkInput{Kind: "pr", URL: "https://code.example/pr/2", Title: "PR #2"})
	pp, ok := AsProposalPending(err)
	if !ok || !strings.Contains(pp.Proposal.SummaryText, "PR #2") || pp.Proposal.ActionTitle != "添加外部链接" {
		t.Fatalf("需要人确认时应记一条待确认操作 %v", err)
	}
	if _, err := a.ApproveProposal(ctx, yi, pp.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	links, _ := a.TaskLinks(ctx, jia, task.ID)
	if len(links) != 2 {
		t.Fatalf("确认后链接应真的挂上 %+v", links)
	}
	// Agent：直接授权时直接挂
	_, tokOK, _ := a.RegisterAgent(ctx, yi, RegisterAgentInput{Name: "能执行的 Agent",
		Grants: map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect}})
	sessOK, _ := a.SessionFromAgentToken(ctx, tokOK)
	if _, err := a.AddTaskLink(ctx, sessOK, task.ID, LinkInput{Kind: "issue", URL: "https://code.example/issues/3"}); err != nil {
		t.Fatal(err)
	}
	// 删除
	links, _ = a.TaskLinks(ctx, jia, task.ID)
	if len(links) != 3 {
		t.Fatalf("现在应有三条链接 %+v", links)
	}
	if err := a.RemoveTaskLink(ctx, jia, task.ID, links[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := a.RemoveTaskLink(ctx, jia, task.ID, links[0].ID); err == nil {
		t.Fatal("删过的链接再删应报没有这条")
	}
	links, _ = a.TaskLinks(ctx, jia, task.ID)
	if len(links) != 2 {
		t.Fatalf("删掉一条后应剩两条 %+v", links)
	}
	for _, typ := range []string{"ExternalLinkAdded", "ExternalLinkRemoved"} {
		if !hasEventOfType(t, a, ctx, orgID, typ) {
			t.Fatalf("%s 要产生动态", typ)
		}
	}
}

// 代码平台身份：绑定、改绑、解绑，占用时报清楚；绑定后外部事件挂在那个人名下。
func TestCodeIdentityBinding(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi, _ := codeTestOrg(t, a, ctx)
	secret := webhookSecretOf(t, a, ctx, jia)

	v, err := a.MyCodeIdentity(ctx, yi)
	if err != nil || v.Bound || v.Provider != directory.FakeCodeKey {
		t.Fatalf("默认没绑定 %+v %v", v, err)
	}
	if v, err = a.BindMyCodeIdentity(ctx, yi, "alice"); err != nil || !v.Bound || v.Login != "alice" {
		t.Fatalf("应绑上 alice %+v %v", v, err)
	}
	if _, err := a.BindMyCodeIdentity(ctx, jia, "alice"); err == nil || !strings.Contains(err.Error(), "alice") {
		t.Fatalf("别人已经绑了应说清楚 %v", err)
	}
	// 外部事件挂在绑定的人名下
	goal, _ := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	task, _ := a.CreateTask(ctx, jia, CreateTaskInput{Title: "修登录", TypeName: "bug", GoalID: goal.ID, AssigneeID: yi.MemberID})
	a.Transition(ctx, jia, task.ID, "confirm", TransitionPayload{})
	if _, err := codeHook(t, a, ctx, orgID, secret, "i1", directory.FakeCodePayload{Event: "pr_opened", Repo: "acme/app", Number: 1,
		Title: "修 #" + itoa(task.Number), Link: true, URL: "https://code.example/pr/1", Status: "open", Actor: "alice"}); err != nil {
		t.Fatal(err)
	}
	var actor string
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := a.Store.ListEvents(ctx, tx, "", 300)
		if err != nil {
			return err
		}
		for _, e := range rows {
			if e.Type == "ExternalEventApplied" {
				actor = e.ActorID
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if actor != yi.MemberID {
		t.Fatalf("绑定了成员的外部操作者，动态应挂在他名下，实际 %q", actor)
	}
	// 解绑
	if v, err = a.BindMyCodeIdentity(ctx, yi, ""); err != nil || v.Bound {
		t.Fatalf("应解绑 %+v %v", v, err)
	}
	if !hasEventOfType(t, a, ctx, orgID, "CodeIdentityBound") {
		t.Fatal("绑定 / 解绑要产生动态")
	}
}

// 断开：配置删掉、回到未接入，外部链接与动态保留。
func TestDisconnectCodePlatform(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _, _ := codeTestOrg(t, a, ctx)
	v, err := a.GetCodePlatform(ctx, jia)
	if err != nil || !v.Configured {
		t.Fatalf("测试组织应已接入 %v %+v", err, v)
	}
	out, err := a.DisconnectCodePlatform(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if out.Configured || (out.Provider != nil && *out.Provider != "") {
		t.Fatalf("断开后应回到未接入 %+v", out)
	}
	// 再断一次不报错
	if _, err := a.DisconnectCodePlatform(ctx, jia); err != nil {
		t.Fatalf("重复断开应当无事发生 %v", err)
	}
}
