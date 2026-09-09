package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teemo/axiomos/internal/api"
	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// MCP 与界面同一套解释（功能规划第 9 项）：越权、范围不可见、需要确认、前置未完成、并发已满、任务已结束
// 六类拒绝，在 HTTP 接口（网页读 error.message）与 MCP 工具（Agent 读返回文本）上必须是同一句话，
// 且等于词条渲染出来的句子。本测试把每个场景在两个面上各走一遍，中英文都比。

type world struct {
	a      *app.App
	ctx    context.Context
	orgID  string
	slug   string
	pw     string
	jia    *app.Session // 组织负责人
	yi     *app.Session // 开发（Agent 的所有者）
	bing   *app.Session // 另一个团队的人
	yiMail string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("未设置 DATABASE_URL，跳过数据库集成测试")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	a := app.New(st)
	if err := a.EnsureGlobals(ctx); err != nil {
		t.Fatal(err)
	}
	w := &world{a: a, ctx: ctx, slug: "p-" + store.NewID("x")[2:10], pw: "password1"}
	org, err := st.CreateOrganization(ctx, st.Pool, w.slug, "一致性测试组织", "CNY")
	if err != nil {
		t.Fatal(err)
	}
	w.orgID = org.ID
	hash, _ := app.HashPassword(w.pw)
	var owner, dev, other *domain.Member
	if err := st.WithOrg(ctx, org.ID, func(tx pgx.Tx) error {
		acc1, _ := st.CreateAccount(ctx, tx, w.slug+"-a@t.local", hash, "甲")
		acc2, _ := st.CreateAccount(ctx, tx, w.slug+"-b@t.local", hash, "乙")
		acc3, _ := st.CreateAccount(ctx, tx, w.slug+"-c@t.local", hash, "丙")
		owner, _ = st.CreateMember(ctx, tx, org.ID, acc1.ID, "甲", []string{"admin"})
		dev, _ = st.CreateMember(ctx, tx, org.ID, acc2.ID, "乙", []string{"developer"})
		other, _ = st.CreateMember(ctx, tx, org.ID, acc3.ID, "丙", []string{"developer"})
		if err := st.SetOrganizationOwner(ctx, tx, org.ID, owner.ID); err != nil {
			return err
		}
		// 两个团队，协作数据只看自己团队树：乙在 A，丙在 B
		ta := &domain.Team{OrgID: org.ID, Name: "A 组"}
		tb := &domain.Team{OrgID: org.ID, Name: "B 组"}
		if err := st.CreateTeam(ctx, tx, ta); err != nil {
			return err
		}
		if err := st.CreateTeam(ctx, tx, tb); err != nil {
			return err
		}
		if err := st.AddTeamMember(ctx, tx, org.ID, ta.ID, dev.ID); err != nil {
			return err
		}
		if err := st.AddTeamMember(ctx, tx, org.ID, tb.ID, other.ID); err != nil {
			return err
		}
		org.CollaborationVisibility = domain.VisibilityTeamTree
		return st.UpdateOrganization(ctx, tx, org)
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureOrgDefaults(ctx, org.ID); err != nil {
		t.Fatal(err)
	}
	w.yiMail = w.slug + "-b@t.local"
	w.jia = w.login(t, w.slug+"-a@t.local")
	w.yi = w.login(t, w.yiMail)
	w.bing = w.login(t, w.slug+"-c@t.local")
	_ = other
	return w
}

func (w *world) login(t *testing.T, email string) *app.Session {
	t.Helper()
	_, s, err := w.a.Login(w.ctx, email, w.pw, w.slug)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (w *world) agent(t *testing.T, owner *app.Session, name string, grants map[domain.Grant]domain.GrantMode, max int) (string, *app.Session) {
	t.Helper()
	_, tok, err := w.a.RegisterAgent(w.ctx, owner, app.RegisterAgentInput{Name: name, Grants: grants, MaxConcurrent: max})
	if err != nil {
		t.Fatal(err)
	}
	s, err := w.a.SessionFromAgentToken(w.ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	return tok, s
}

func (w *world) task(t *testing.T, by *app.Session, title, assignee string) *domain.Task {
	t.Helper()
	tk, err := w.a.CreateTask(w.ctx, by, app.CreateTaskInput{Title: title, AssigneeID: assignee, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

// callMCP 通过内存传输调用一个工具，返回文本与是否错误。
func callMCP(t *testing.T, a *app.App, sess *app.Session, tool string, args map[string]any) (string, bool) {
	t.Helper()
	ctx := context.Background()
	ct, st := sdk.NewInMemoryTransports()
	srv := newServer(a, sess)
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	text := ""
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*sdk.TextContent); ok {
			text = tc.Text
		}
	}
	return text, res.IsError
}

// callHTTP 用 Agent 令牌调 HTTP 接口，返回状态码与 message（202 时是 body.message，其余是 error.message）。
func callHTTP(t *testing.T, h http.Handler, token, method, path string, body any) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code == 202 {
		return rec.Code, out.Message
	}
	return rec.Code, out.Error.Message
}

func TestRejectionsSayTheSameThingOnBothSurfaces(t *testing.T) {
	w := newWorld(t)
	a, ctx := w.a, w.ctx
	h := (&api.Server{App: a}).Handler()

	// 1. 越权：没有「评论」授权
	tokNoGrant, agNoGrant := w.agent(t, w.yi, "无评论授权", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect}, 1)
	tGrant := w.task(t, w.jia, "越权任务", agNoGrant.Actor.ID)

	// 2. 范围不可见：任务归 B 组的丙，乙的 Agent 看不到
	tokExec, agExec := w.agent(t, w.yi, "执行 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect, domain.GrantClaimBacklog: domain.GrantDirect}, 1)
	tHidden := w.task(t, w.jia, "别人团队的任务", w.bing.MemberID)

	// 3. 需要确认：「执行任务」授权是需要人确认
	tokApproval, agApproval := w.agent(t, w.yi, "需确认 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantWithApproval}, 1)
	tApproval := w.task(t, w.jia, "需确认任务", agApproval.Actor.ID)
	// 人指派给 Agent 即发委托（ADR 0028），委托内开始不问人；这里要的是「需要人确认」，先把委托收回
	if ms, err := a.TaskMandates(ctx, w.jia, tApproval.ID); err != nil || len(ms) != 1 {
		t.Fatalf("指派给 Agent 应发一份委托：%v %+v", err, ms)
	} else if _, err := a.RevokeMandate(ctx, w.jia, ms[0].ID, "先逐条确认"); err != nil {
		t.Fatal(err)
	}

	// 4. 前置未完成：前置任务还在待办
	tPre := w.task(t, w.jia, "前置任务", w.yi.MemberID)
	tDeps := w.task(t, w.jia, "后置任务", agExec.Actor.ID)
	if _, err := a.Link(ctx, w.jia, tPre.ID, domain.RelationBlocks, tDeps.ID); err != nil {
		t.Fatal(err)
	}

	// 5. 并发已满：Agent 并发上限 1，已经在执行一个
	tBusy := w.task(t, w.jia, "正在做的任务", agExec.Actor.ID)
	if _, err := a.Transition(ctx, agExec, tBusy.ID, "start", app.TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	tSecond := w.task(t, w.jia, "第二个任务", agExec.Actor.ID)

	// 6. 任务已结束
	tokDone, agDone := w.agent(t, w.yi, "收尾 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect}, 1)
	tDone := w.task(t, w.jia, "已结束的任务", agDone.Actor.ID)
	if _, err := a.Transition(ctx, agDone, tDone.ID, "start", app.TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddArtifact(ctx, agDone, tDone.ID, domain.Artifact{Type: "result", Title: "结果"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, agDone, tDone.ID, "submit", app.TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, w.jia, tDone.ID, "accept", app.TransitionPayload{}); err != nil {
		t.Fatal(err)
	}

	type scenario struct {
		name     string
		token    string
		sess     func() *app.Session
		expect   func(loc i18n.Locale) string
		httpCode int
		method   string
		path     string
		body     any
		tool     string
		args     map[string]any
		prefix   bool // MCP 文本以 HTTP 句子开头即可（需要确认时 MCP 多一句提示）
	}
	scenarios := []scenario{
		{name: "越权", token: tokNoGrant, expect: func(l i18n.Locale) string { return i18n.Trf(l, "reject.agent_no_grant", i18n.Key("grant.comment")) },
			httpCode: 409, method: "POST", path: "/tasks/" + tGrant.ID + "/comments", body: map[string]any{"body": "你好"}, tool: "add_comment", args: map[string]any{"task_id": tGrant.ID, "text": "你好"}},
		{name: "范围不可见", token: tokExec, expect: func(l i18n.Locale) string { return i18n.Tr(l, "err.task_hidden") },
			httpCode: 403, method: "GET", path: "/tasks/" + tHidden.ID, tool: "get_task", args: map[string]any{"task_id": tHidden.ID}},
		{name: "范围不可见·动态", token: tokExec, expect: func(l i18n.Locale) string { return i18n.Tr(l, "err.task_hidden") },
			httpCode: 403, method: "GET", path: "/events?task=" + tHidden.ID, tool: "list_events", args: map[string]any{"task_id": tHidden.ID}},
		{name: "不存在", token: tokExec, expect: func(l i18n.Locale) string { return i18n.Tr(l, "err.not_found") },
			httpCode: 404, method: "GET", path: "/tasks/tsk_nope", tool: "get_task_brief", args: map[string]any{"task_id": "tsk_nope"}},
		{name: "需要确认", token: tokApproval, expect: func(l i18n.Locale) string { return i18n.Trf(l, "proposal.pending_msg", "乙") }, prefix: true,
			httpCode: 202, method: "POST", path: "/tasks/" + tApproval.ID + "/transitions/start", body: map[string]any{}, tool: "transition_task", args: map[string]any{"task_id": tApproval.ID, "name": "start"}},
		{name: "前置未完成", token: tokExec, expect: func(l i18n.Locale) string { return i18n.Trf(l, "reject.deps_open", []string{"前置任务"}) },
			httpCode: 409, method: "POST", path: "/tasks/" + tDeps.ID + "/transitions/start", body: map[string]any{}, tool: "transition_task", args: map[string]any{"task_id": tDeps.ID, "name": "start"}},
		{name: "并发已满", token: tokExec, expect: func(l i18n.Locale) string { return i18n.Tr(l, "reject.concurrency") },
			httpCode: 409, method: "POST", path: "/tasks/" + tSecond.ID + "/transitions/start", body: map[string]any{}, tool: "transition_task", args: map[string]any{"task_id": tSecond.ID, "name": "start"}},
		{name: "任务已结束", token: tokDone, expect: func(l i18n.Locale) string { return i18n.Tr(l, "reject.task_closed") },
			httpCode: 409, method: "POST", path: "/tasks/" + tDone.ID + "/transitions/start", body: map[string]any{}, tool: "transition_task", args: map[string]any{"task_id": tDone.ID, "name": "start"}},
		{name: "任务已结束·开始执行", token: tokDone, expect: func(l i18n.Locale) string { return i18n.Tr(l, "reject.task_closed") },
			httpCode: 409, method: "POST", path: "/tasks/" + tDone.ID + "/begin", body: map[string]any{}, tool: "begin_task", args: map[string]any{"task_id": tDone.ID}},
	}

	for _, loc := range []i18n.Locale{i18n.ZhCN, i18n.EnUS} {
		// 所有者的语言决定 Agent 看到的语言（HTTP 与 MCP 同一规则）
		if err := a.SetMyLocale(ctx, w.yi, string(loc)); err != nil {
			t.Fatal(err)
		}
		for _, sc := range scenarios {
			want := sc.expect(loc)
			code, httpMsg := callHTTP(t, h, sc.token, sc.method, sc.path, sc.body)
			if code != sc.httpCode {
				t.Fatalf("[%s/%s] HTTP 状态应为 %d，实际 %d（%s）", loc, sc.name, sc.httpCode, code, httpMsg)
			}
			if httpMsg != want {
				t.Fatalf("[%s/%s] HTTP 句子应为「%s」，实际「%s」", loc, sc.name, want, httpMsg)
			}
			sess, err := a.SessionFromAgentToken(ctx, sc.token)
			if err != nil {
				t.Fatal(err)
			}
			text, isErr := callMCP(t, a, sess, sc.tool, sc.args)
			if sc.prefix {
				if isErr || !strings.HasPrefix(text, want) {
					t.Fatalf("[%s/%s] MCP 应以「%s」开头且不算错误，实际 isError=%v「%s」", loc, sc.name, want, isErr, text)
				}
			} else if !isErr || text != want {
				t.Fatalf("[%s/%s] MCP 应报错「%s」，实际 isError=%v「%s」", loc, sc.name, want, isErr, text)
			}
			if text[:len(want)] != httpMsg {
				t.Fatalf("[%s/%s] 两个面上的句子不一致：HTTP「%s」 MCP「%s」", loc, sc.name, httpMsg, text)
			}
		}
	}
	_ = i18n.Default
}

// 未登录时 MCP 与 HTTP 同样按 Accept-Language 说话。
func TestUnauthenticatedSentence(t *testing.T) {
	err := app.Unauthorized("err.agent_token")
	if got := RenderError(i18n.EnUS, err); got != "Invalid agent token" {
		t.Fatalf("英文渲染不对：%s", got)
	}
	if got := RenderError(i18n.ZhCN, store.ErrNotFound); got != i18n.Tr(i18n.ZhCN, "err.not_found") {
		t.Fatalf("没有找到应按词条渲染：%s", got)
	}
}
