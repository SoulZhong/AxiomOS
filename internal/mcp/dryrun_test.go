package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teemo/axiomos/internal/api"
	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// ADR 0025 第 2 条的原话：只看不做「不落行、不写动态、不发通知、不生成待确认操作」。
// 这两个测试把这句话当成验收标准，逐个走遍**每一个**收 dry_run 的 MCP 工具与 HTTP 写接口：
// 调用前后把这个组织能看到的每张表都数一遍，差一行都算没做到。
// 名单不是手抄的：MCP 那边从服务器自己报的工具清单里取，HTTP 那边从路由表的源码里取，
// 新加了工具或接口而没在这里登记，测试直接失败，不会悄悄漏掉。

// orgRowCounts 数一遍这个组织能看到的所有表。表名是现问数据库要的（凡是带 org_id 的表都算），
// 所以以后加了新表也自动纳入。
func orgRowCounts(t *testing.T, w *world) map[string]int {
	t.Helper()
	out := map[string]int{}
	if err := w.a.Store.WithOrg(w.ctx, w.orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(w.ctx, `select table_name from information_schema.columns
            where table_schema='public' and column_name='org_id' order by table_name`)
		if err != nil {
			return err
		}
		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return err
			}
			names = append(names, n)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(names) < 10 {
			t.Fatalf("只找到 %d 张带 org_id 的表，数据库大概没迁移好", len(names))
		}
		for _, n := range names {
			c := 0
			// 明确按 org_id 过滤：不是每张表都开了行级安全，而 go test ./... 会几个包一起跑，
			// 只靠 RLS 会数进别的包正在建的行。
			if err := tx.QueryRow(w.ctx, `select count(*) from "`+n+`" where org_id=$1`, w.orgID).Scan(&c); err != nil {
				return err
			}
			out[n] = c
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// assertNothingWritten 是这两个测试唯一的判据：一行都不许多，一行都不许少。
func assertNothingWritten(t *testing.T, what string, before, after map[string]int) {
	t.Helper()
	for table, n := range after {
		if before[table] != n {
			t.Errorf("%s 的只看不做写了东西：%s 从 %d 变成 %d", what, table, before[table], n)
		}
	}
}

// ---------- MCP：每一个收 dry_run 的工具 ----------

// dryRunTools 列出服务器上所有把 dry_run 写进入参的工具（也就是"写类工具"）。
func dryRunTools(t *testing.T, w *world, sess *app.Session) []string {
	t.Helper()
	ctx := context.Background()
	ct, st := sdk.NewInMemoryTransports()
	srv := newServer(w.a, sess)
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, tl := range res.Tools {
		b, err := json.Marshal(tl.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		_ = json.Unmarshal(b, &schema)
		if _, ok := schema.Properties["dry_run"]; ok {
			out = append(out, tl.Name)
		}
	}
	sort.Strings(out)
	return out
}

func TestEveryMCPWriteToolDryRunWritesNothing(t *testing.T) {
	w := newWorld(t)
	a, ctx := w.a, w.ctx

	goal, err := a.CreateGoal(ctx, w.jia, app.CreateGoalInput{Title: "只看不做的目标"})
	if err != nil {
		t.Fatal(err)
	}
	mine := w.task(t, w.jia, "登录页改版", w.jia.MemberID)
	other := w.task(t, w.jia, "订单导出", w.jia.MemberID)
	free := w.task(t, w.jia, "没人领的活", "")
	sp, err := a.CreateSprint(ctx, w.jia, app.CreateSprintInput{Name: "只看不做的迭代", StartsOn: dayFrom(0), EndsOn: dayFrom(13)})
	if err != nil {
		t.Fatal(err)
	}
	next, err := a.CreateSprint(ctx, w.jia, app.CreateSprintInput{Name: "下一个迭代", StartsOn: dayFrom(14), EndsOn: dayFrom(27)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddTasksToSprint(ctx, w.jia, sp.ID, []string{other.ID}); err != nil {
		t.Fatal(err)
	}
	ms, err := a.CreateMilestone(ctx, w.jia, app.CreateMilestoneInput{GoalID: goal.ID, Title: "上线", DueOn: dayFrom(20)})
	if err != nil {
		t.Fatal(err)
	}
	// 一个已经在进行中、执行记录还没开的任务，begin_task 才有东西可开
	beginnable := w.task(t, w.jia, "进行中的任务", w.jia.MemberID)
	if _, err := a.Transition(ctx, w.jia, beginnable.ID, "start", app.TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.Pool.Exec(ctx, `update runs set ended_at=now() where task_id=$1 and ended_at is null`, beginnable.ID); err != nil {
		t.Fatal(err)
	}
	// 另一个正在跑的，用来附交付物
	running := w.task(t, w.jia, "正在做的任务", w.jia.MemberID)
	if _, err := a.Transition(ctx, w.jia, running.ID, "start", app.TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	// 第一批补齐的工具各要一份现成的对象：第二个目标（重排）、已达成 / 已放弃的目标（撤销 / 重新开始）、
	// 已达到的里程碑（撤销）、一条外部链接（摘掉）、一个等验收的任务（验收）
	g2, err := a.CreateGoal(ctx, w.jia, app.CreateGoalInput{Title: "第二个目标"})
	if err != nil {
		t.Fatal(err)
	}
	achieved, err := a.CreateGoal(ctx, w.jia, app.CreateGoalInput{Title: "已达成的目标"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ChangeGoalStatus(ctx, w.jia, achieved.ID, "achieved", nil); err != nil {
		t.Fatal(err)
	}
	abandoned, err := a.CreateGoal(ctx, w.jia, app.CreateGoalInput{Title: "已放弃的目标"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ChangeGoalStatus(ctx, w.jia, abandoned.ID, "abandoned", nil); err != nil {
		t.Fatal(err)
	}
	reached, err := a.CreateMilestone(ctx, w.jia, app.CreateMilestoneInput{GoalID: goal.ID, Title: "已经达到的里程碑", DueOn: dayFrom(10)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReachMilestone(ctx, w.jia, reached.ID); err != nil {
		t.Fatal(err)
	}
	link, err := a.AddTaskLink(ctx, w.jia, mine.ID, app.LinkInput{Kind: "doc", URL: "https://example.com/doc/1", Title: "设计稿"})
	if err != nil {
		t.Fatal(err)
	}
	linked := w.task(t, w.jia, "有关联的任务", w.jia.MemberID)
	if _, err := a.Link(ctx, w.jia, linked.ID, domain.RelationBlocks, other.ID); err != nil {
		t.Fatal(err)
	}
	submitted := w.task(t, w.jia, "等验收的任务", w.jia.MemberID)
	if _, err := a.Transition(ctx, w.jia, submitted.ID, "start", app.TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddArtifact(ctx, w.jia, submitted.ID, domain.Artifact{Type: "result", Title: "结果"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, w.jia, submitted.ID, "submit", app.TransitionPayload{}); err != nil {
		t.Fatal(err)
	}

	// 每个写类工具一份能真正走到写回那一步的参数
	args := map[string]map[string]any{
		"update_goal":             {"goal_id": goal.ID, "title": "改过的目标"},
		"achieve_goal":            {"goal_id": goal.ID},
		"unachieve_goal":          {"goal_id": achieved.ID},
		"abandon_goal":            {"goal_id": goal.ID},
		"restart_goal":            {"goal_id": abandoned.ID},
		"set_goal_horizon":        {"goal_ids": []any{goal.ID, g2.ID}, "horizon": "now"},
		"rank_goals":              {"goal_ids": []any{g2.ID, goal.ID}},
		"add_goal_note":           {"goal_id": goal.ID, "text": "一句进展"},
		"update_task":             {"task_id": mine.ID, "title": "改过的任务"},
		"review_task":             {"task_id": submitted.ID, "decision": "accept", "checked_deliverables": []any{"result"}},
		"create_subtask":          {"parent_id": mine.ID, "title": "不该出现的子任务"},
		"add_external_link":       {"task_id": mine.ID, "url": "https://example.com/pr/1", "kind": "pr"},
		"remove_external_link":    {"task_id": mine.ID, "link_id": link.ID},
		"update_milestone":        {"milestone_id": ms.ID, "title": "改过的里程碑"},
		"create_sprint":           {"name": "不该出现的迭代", "starts_on": dayFrom(30).Format("2006-01-02"), "ends_on": dayFrom(43).Format("2006-01-02")},
		"update_sprint":           {"sprint_id": sp.ID, "name": "改过的迭代"},
		"unlink_tasks":            {"task_id": linked.ID, "type": "blocks", "other_id": other.ID},
		"propose_goal_plan":       {"goal_id": goal.ID, "tasks": []any{map[string]any{"title": "不该出现的方案任务"}}},
		"delete_milestone":        {"milestone_id": ms.ID},
		"unreach_milestone":       {"milestone_id": reached.ID},
		"claim_task":              {"task_id": free.ID},
		"begin_task":              {"task_id": beginnable.ID},
		"transition_task":         {"task_id": mine.ID, "name": "start"},
		"assign_task":             {"task_id": free.ID, "executor_id": w.yi.MemberID},
		"create_task":             {"title": "不该出现的任务", "goal_id": goal.ID},
		"create_goal":             {"title": "不该出现的目标"},
		"create_milestone":        {"goal_id": goal.ID, "title": "不该出现的里程碑", "due_on": dayFrom(25).Format("2006-01-02")},
		"reach_milestone":         {"milestone_id": ms.ID},
		"add_comment":             {"task_id": mine.ID, "text": "一句话"},
		"add_note":                {"task_id": mine.ID, "text": "一句话"},
		"attach_artifact":         {"task_id": running.ID, "type": "result", "title": "结果说明", "ref": "https://example.com/r"},
		"link_tasks":              {"task_id": mine.ID, "type": "blocks", "other_id": other.ID},
		"add_tasks_to_sprint":     {"sprint_id": sp.ID, "task_ids": []any{mine.ID}},
		"remove_task_from_sprint": {"sprint_id": sp.ID, "task_id": other.ID},
		"start_sprint":            {"sprint_id": sp.ID},
		"close_sprint":            {"sprint_id": sp.ID, "unfinished": "next", "next_sprint_id": next.ID},
	}
	// 唯一不收 dry_run 的写类工具：心跳是 Agent 每 60 秒自动打的，"先念给人听再做"这件事
	// 对它没有意义。它写的那点东西（执行记录的最后心跳、累计用量）走的是 persist，真要
	// 从 HTTP 那侧带着 dry_run 过来也一行不落。
	noDryRunTool := map[string]string{"heartbeat": "Agent 自动打的心跳，不需要预演", "mark_notifications_read": "把通知标成已读，读状态不记动态（与 HTTP 同一条例外）"}

	tools := dryRunTools(t, w, w.jia)
	if len(tools) == 0 {
		t.Fatal("一个收 dry_run 的工具都没找到，测试自己坏了")
	}
	var missing []string
	for _, name := range tools {
		if _, ok := args[name]; !ok {
			missing = append(missing, name)
		}
	}
	_ = noDryRunTool
	if len(missing) > 0 {
		t.Fatalf("这些写类工具收 dry_run 却没在本测试里走一遍，补上参数：%s", strings.Join(missing, "、"))
	}
	for name := range args {
		if !contains(tools, name) {
			t.Fatalf("工具 %s 已经不收 dry_run 了（或改名了），本测试的名单要跟着改", name)
		}
	}

	// 目标方案只有 Agent 能提（ADR 0026）：这一个工具用甲的 Agent 调，其余仍用甲本人
	_, planner := w.agent(t, w.jia, "提方案的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantCreateTask: domain.GrantDirect}, 1)
	sessionFor := map[string]*app.Session{"propose_goal_plan": planner}

	for _, name := range tools {
		in := map[string]any{}
		for k, v := range args[name] {
			in[k] = v
		}
		in["dry_run"] = true
		before := orgRowCounts(t, w)
		by := w.jia
		if s := sessionFor[name]; s != nil {
			by = s
		}
		text, isErr := callMCP(t, a, by, name, in)
		if isErr {
			t.Errorf("%s 的只看不做报错了：%s", name, text)
		} else if !strings.HasPrefix(text, "会") && !strings.Contains(text, "不会立刻生效") {
			t.Errorf("%s 的只看不做应当回一句「会……」，实际「%s」", name, text)
		}
		assertNothingWritten(t, "MCP 工具 "+name, before, orgRowCounts(t, w))
	}
}

// dayFrom 是从今天算起第 n 天。
func dayFrom(n int) *time.Time {
	d := time.Now().AddDate(0, 0, n)
	return &d
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ---------- HTTP：每一条写路由 ----------

var routeRe = regexp.MustCompile(`\bauth\("(GET|POST|PUT|PATCH|DELETE) (/api/v1/[^"]*)"`)

// authRoutes 从 internal/api 的源码里把所有需要登录的路由抠出来（"方法 路径"）。
// 路由表分散在好几个文件里（s.milestoneRoutes(auth) 之类），所以整个包一起扫。
func authRoutes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../api/*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range routeRe.FindAllStringSubmatch(string(b), -1) {
			r := m[1] + " " + m[2]
			if m[1] == "GET" || seen[r] {
				continue
			}
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	if len(out) < 20 {
		t.Fatalf("只扫出 %d 条写路由，正则大概失效了", len(out))
	}
	return out
}

// 不收 dry_run 的写路由，每一条都要写明为什么。它们不是"忘了做"，是"按 ADR 0025 不在范围里"：
// dry_run 是给 Agent 代人做事那一路准备的（任务、目标、里程碑、迭代），组织配置类的改动是人
// 在网页上按按钮，网页自己就有确认框。这类接口收到 dry_run 会返回 400「还不支持」而不是偷偷写
// （兜底在 app.insertEvents 里），本测试只保证名单是齐的。
var notDryRunnable = map[string]string{
	"PATCH /api/v1/auth/me":                                       "改自己的语言设置，网页表单",
	"POST /api/v1/agents":                                         "注册 Agent，网页向导",
	"PATCH /api/v1/agents/{id}":                                   "改 Agent 授权，网页表单",
	"DELETE /api/v1/agents/{id}":                                  "吊销 Agent，网页有确认框",
	"PUT /api/v1/task-types/{name}":                               "改流程定义，网页编辑器",
	"POST /api/v1/agent-auth/device/{user_code}/approve":          "设备码授权，人在网页上点同意",
	"POST /api/v1/agent-auth/device/{user_code}/deny":             "设备码授权，人在网页上点拒绝",
	"POST /api/v1/proposals/{id}/approve":                         "人裁决待确认操作，本身就是确认动作",
	"POST /api/v1/proposals/{id}/reject":                          "人裁决待确认操作，本身就是确认动作",
	"POST /api/v1/notifications/read":                             "把通知标成已读，读状态不记动态",
	"PUT /api/v1/me/code-identity":                                "绑定自己的代码平台账号，网页表单",
	"PUT /api/v1/me/notifications":                                "个人通知设置，网页表单",
	"DELETE /api/v1/me/notifications":                             "个人通知设置，网页表单",
	"PUT /api/v1/me/preferences":                                  "个人偏好，网页表单",
	"DELETE /api/v1/me/preferences":                               "个人偏好，网页表单",
	"PUT /api/v1/workspace/me":                                    "工作台布局，网页拖完即存",
	"DELETE /api/v1/workspace/me":                                 "工作台布局，网页拖完即存",
	"PUT /api/v1/org/workspace/{role}":                            "角色工作台布局，网页拖完即存",
	"DELETE /api/v1/org/workspace/{role}":                         "角色工作台布局，网页拖完即存",
	"PATCH /api/v1/org":                                           "组织信息，网页表单",
	"PATCH /api/v1/org/settings":                                  "组织策略，网页表单",
	"PATCH /api/v1/org/members/{id}":                              "组织管理，网页表单",
	"POST /api/v1/org/members/{id}/make-owner":                    "转让组织负责人，网页有确认框",
	"POST /api/v1/org/members/{id}/merge":                         "合并成员，网页有确认框",
	"POST /api/v1/org/members/bulk":                               "批量改成员，网页有确认框",
	"POST /api/v1/org/members/import":                             "导入成员，导入前有 preview 接口",
	"POST /api/v1/org/members/import/preview":                     "导入成员的预览，本身就是先看一眼",
	"POST /api/v1/org/teams":                                      "组织管理，网页表单",
	"PATCH /api/v1/org/teams/{id}":                                "组织管理，网页表单",
	"DELETE /api/v1/org/teams/{id}":                               "组织管理，网页有确认框",
	"POST /api/v1/org/teams/{id}/merge":                           "合并团队，网页有确认框",
	"POST /api/v1/org/invitations":                                "邀请，网页表单",
	"DELETE /api/v1/org/invitations/{id}":                         "撤回邀请，网页有确认框",
	"PUT /api/v1/org/roles/{name}":                                "角色与权限，网页编辑器",
	"DELETE /api/v1/org/roles/{name}":                             "角色与权限，网页有确认框",
	"PUT /api/v1/org/capabilities/{name}":                         "能力标签词表，网页表单",
	"DELETE /api/v1/org/capabilities/{name}":                      "能力标签词表，网页有确认框",
	"POST /api/v1/org/goal-types":                                 "目标类型词表（ADR 0023），网页表单",
	"PATCH /api/v1/org/goal-types/{id}":                           "目标类型词表，网页表单",
	"DELETE /api/v1/org/goal-types/{id}":                          "目标类型词表，网页有确认框",
	"PUT /api/v1/org/pricing/models/{model}":                      "计价，网页表单",
	"DELETE /api/v1/org/pricing/models/{model}":                   "计价，网页有确认框",
	"PUT /api/v1/org/pricing/rates":                               "汇率，网页表单",
	"PUT /api/v1/org/notifications":                               "组织通知策略，网页表单",
	"PUT /api/v1/org/preferences/{role}":                          "角色偏好，网页表单",
	"DELETE /api/v1/org/preferences/{role}":                       "角色偏好，网页表单",
	"POST /api/v1/org/notifications/test":                         "发一条测试消息，本身就是试一下",
	"PUT /api/v1/org/code-platform":                               "代码平台配置，网页向导",
	"DELETE /api/v1/org/code-platform":                            "断开代码平台，网页有确认框",
	"POST /api/v1/org/code-platform/test":                         "代码平台连通性自检，只读外部系统",
	"PUT /api/v1/org/directory":                                   "外部目录配置（ADR 0017），网页向导",
	"DELETE /api/v1/org/directory":                                "断开外部目录，网页有确认框",
	"POST /api/v1/org/directory/test":                             "外部目录连通性自检，只读外部系统",
	"POST /api/v1/org/directory/sync":                             "同步组织架构，网页上先看冲突再同步",
	"PUT /api/v1/org/directory/decisions":                         "同步冲突的处置，网页表单",
	"DELETE /api/v1/org/directory/decisions/{kind}/{external_id}": "同步冲突的处置，网页表单",
	"DELETE /api/v1/org/directory/bindings/{kind}/{external_id}":  "解绑外部身份，网页有确认框",
}

func TestEveryHTTPWriteEndpointDryRunWritesNothing(t *testing.T) {
	w := newWorld(t)
	a, ctx := w.a, w.ctx
	h := (&api.Server{App: a}).Handler()
	human, _, err := a.Login(ctx, w.slug+"-a@t.local", w.pw, w.slug)
	if err != nil {
		t.Fatal(err)
	}

	goal, err := a.CreateGoal(ctx, w.jia, app.CreateGoalInput{Title: "接口层只看不做"})
	if err != nil {
		t.Fatal(err)
	}
	mine := w.task(t, w.jia, "接口层的任务", w.jia.MemberID)
	other := w.task(t, w.jia, "接口层的另一个任务", w.jia.MemberID)
	free := w.task(t, w.jia, "接口层没人领的活", "")
	// 一个进行中但执行记录已经关掉的任务（begin 有东西可开），一个正在跑的（附交付物、心跳）
	beginnable := w.task(t, w.jia, "接口层进行中的任务", w.jia.MemberID)
	if _, err := a.Transition(ctx, w.jia, beginnable.ID, "start", app.TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.Pool.Exec(ctx, `update runs set ended_at=now() where task_id=$1 and ended_at is null`, beginnable.ID); err != nil {
		t.Fatal(err)
	}
	running := w.task(t, w.jia, "接口层正在做的任务", w.jia.MemberID)
	if _, err := a.Transition(ctx, w.jia, running.ID, "start", app.TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	sp, err := a.CreateSprint(ctx, w.jia, app.CreateSprintInput{Name: "接口层的迭代", StartsOn: dayFrom(0), EndsOn: dayFrom(13)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddTasksToSprint(ctx, w.jia, sp.ID, []string{other.ID}); err != nil {
		t.Fatal(err)
	}
	ms, err := a.CreateMilestone(ctx, w.jia, app.CreateMilestoneInput{GoalID: goal.ID, Title: "接口层的里程碑", DueOn: dayFrom(20)})
	if err != nil {
		t.Fatal(err)
	}
	g2, err := a.CreateGoal(ctx, w.jia, app.CreateGoalInput{Title: "第二个目标"})
	if err != nil {
		t.Fatal(err)
	}
	// 一个已经确认达到的里程碑，unreach 才有东西可撤
	reached, err := a.CreateMilestone(ctx, w.jia, app.CreateMilestoneInput{GoalID: goal.ID, Title: "已经达到的里程碑", DueOn: dayFrom(10)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReachMilestone(ctx, w.jia, reached.ID); err != nil {
		t.Fatal(err)
	}
	// 一条外部链接，DELETE 才有东西可摘
	link, err := a.AddTaskLink(ctx, w.jia, mine.ID, app.LinkInput{Kind: "doc", URL: "https://example.com/doc/1", Title: "设计稿"})
	if err != nil {
		t.Fatal(err)
	}
	// 委托：人指派给 Agent 即发委托；Agent 在委托内走一步，才有撤回可撤；收回委托之后再发才有得发
	_, delegateAgent := w.agent(t, w.jia, "受委托的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantWithApproval}, 1)
	delegated := w.task(t, w.jia, "委托出去的任务", delegateAgent.Actor.ID)
	if _, err := a.Transition(ctx, delegateAgent, delegated.ID, "start", app.TransitionPayload{}); err != nil {
		t.Fatalf("委托内开始不该问人：%v", err)
	}
	mandates, err := a.TaskMandates(ctx, w.jia, delegated.ID)
	if err != nil || len(mandates) != 1 {
		t.Fatalf("应有一份委托：%v %+v", err, mandates)
	}
	mandate := mandates[0]
	// 一条任务关联，解除才有东西可解
	linked := w.task(t, w.jia, "接口层有关联的任务", w.jia.MemberID)
	if _, err := a.Link(ctx, w.jia, linked.ID, domain.RelationBlocks, other.ID); err != nil {
		t.Fatal(err)
	}

	covered := map[string]struct {
		path string
		body any
	}{
		"POST /api/v1/goals":                                    {"/goals", map[string]any{"title": "不该出现的目标"}},
		"PUT /api/v1/goals/horizon":                             {"/goals/horizon", map[string]any{"ids": []string{goal.ID, g2.ID}, "horizon": "now"}},
		"PUT /api/v1/goals/rank":                                {"/goals/rank", map[string]any{"ids": []string{goal.ID, g2.ID}}},
		"PATCH /api/v1/goals/{id}":                              {"/goals/" + goal.ID, map[string]any{"title": "改过的目标"}},
		"DELETE /api/v1/goals/{id}":                             {"/goals/" + g2.ID, nil},
		"POST /api/v1/goals/{id}/milestones":                    {"/goals/" + goal.ID + "/milestones", map[string]any{"title": "不该出现的里程碑", "due_on": dayFrom(25).Format("2006-01-02")}},
		"PATCH /api/v1/milestones/{id}":                         {"/milestones/" + ms.ID, map[string]any{"title": "改过的里程碑"}},
		"DELETE /api/v1/milestones/{id}":                        {"/milestones/" + ms.ID, nil},
		"POST /api/v1/milestones/{id}/reach":                    {"/milestones/" + ms.ID + "/reach", nil},
		"POST /api/v1/milestones/{id}/unreach":                  {"/milestones/" + reached.ID + "/unreach", nil},
		"POST /api/v1/tasks":                                    {"/tasks", map[string]any{"title": "不该出现的任务", "goal_id": goal.ID}},
		"PATCH /api/v1/tasks/{id}":                              {"/tasks/" + mine.ID, map[string]any{"title": "改过的任务"}},
		"POST /api/v1/tasks/{id}/transitions/{name}":            {"/tasks/" + mine.ID + "/transitions/start", nil},
		"POST /api/v1/tasks/{id}/claim":                         {"/tasks/" + free.ID + "/claim", nil},
		"POST /api/v1/tasks/{id}/begin":                         {"/tasks/" + beginnable.ID + "/begin", nil},
		"POST /api/v1/tasks/{id}/assign":                        {"/tasks/" + free.ID + "/assign", map[string]any{"executor_id": w.yi.MemberID}},
		"POST /api/v1/tasks/{id}/artifacts":                     {"/tasks/" + running.ID + "/artifacts", map[string]any{"type": "result", "title": "结果说明", "ref": "https://example.com/r"}},
		"POST /api/v1/tasks/{id}/heartbeat":                     {"/tasks/" + running.ID + "/heartbeat", map[string]any{}},
		"POST /api/v1/tasks/{id}/comments":                      {"/tasks/" + mine.ID + "/comments", map[string]any{"text": "一句话"}},
		"POST /api/v1/tasks/{id}/relations":                     {"/tasks/" + mine.ID + "/relations", map[string]any{"type": "blocks", "other_id": other.ID}},
		"DELETE /api/v1/tasks/{id}/relations/{type}/{other_id}": {"/tasks/" + linked.ID + "/relations/blocks/" + other.ID, nil},
		"POST /api/v1/tasks/{id}/links":                         {"/tasks/" + mine.ID + "/links", map[string]any{"kind": "pr", "url": "https://example.com/pr/1"}},
		"DELETE /api/v1/tasks/{id}/links/{link_id}":             {"/tasks/" + mine.ID + "/links/" + link.ID, nil},
		"POST /api/v1/sprints":                                  {"/sprints", map[string]any{"name": "不该出现的迭代", "starts_on": dayFrom(30).Format("2006-01-02"), "ends_on": dayFrom(43).Format("2006-01-02")}},
		"PATCH /api/v1/sprints/{id}":                            {"/sprints/" + sp.ID, map[string]any{"name": "改过的迭代"}},
		"POST /api/v1/sprints/{id}/start":                       {"/sprints/" + sp.ID + "/start", nil},
		"POST /api/v1/sprints/{id}/close":                       {"/sprints/" + sp.ID + "/close", map[string]any{"unfinished": "backlog"}},
		"POST /api/v1/sprints/{id}/tasks":                       {"/sprints/" + sp.ID + "/tasks", map[string]any{"task_ids": []string{mine.ID}}},
		"DELETE /api/v1/sprints/{id}/tasks/{task_id}":           {"/sprints/" + sp.ID + "/tasks/" + other.ID, nil},
		// 委托与撤回（ADR 0028）
		"POST /api/v1/tasks/{id}/mandates": {"/tasks/" + delegated.ID + "/mandates", map[string]any{"agent_id": delegateAgent.Actor.ID}},
		"DELETE /api/v1/mandates/{id}":     {"/mandates/" + mandate.ID, nil},
		"POST /api/v1/tasks/{id}/revert":   {"/tasks/" + delegated.ID + "/revert", map[string]any{"reason": "先别开始"}},
	}

	// 名单要齐：路由表里每一条写路由，要么在这里走一遍，要么在 notDryRunnable 里写明为什么不走。
	for _, r := range authRoutes(t) {
		_, ok1 := covered[r]
		_, ok2 := notDryRunnable[r]
		if !ok1 && !ok2 {
			t.Errorf("写路由 %s 既没走只看不做，也没写明为什么不走——ADR 0025 说写操作都要能先看后做", r)
		}
	}
	routes := authRoutes(t)
	for r := range covered {
		if !contains(routes, r) {
			t.Errorf("路由 %s 已经不在路由表里了，本测试的名单要跟着改", r)
		}
	}
	for r := range notDryRunnable {
		if !contains(routes, r) {
			t.Errorf("notDryRunnable 里的 %s 已经不是一条写路由了，名单要跟着改", r)
		}
	}

	for _, r := range sortedKeys(covered) {
		c := covered[r]
		method, _, _ := strings.Cut(r, " ")
		before := orgRowCounts(t, w)
		code, body := callDryRun(t, h, human, method, c.path, c.body)
		if code != 200 {
			t.Errorf("%s 的只看不做应当返回 200，实际 %d：%s", r, code, body)
		}
		var out struct {
			DryRun bool   `json:"dry_run"`
			Will   string `json:"will"`
		}
		_ = json.Unmarshal([]byte(body), &out)
		if !out.DryRun || out.Will == "" {
			t.Errorf("%s 的只看不做应当回 {dry_run:true, will:\"会……\"}，实际 %s", r, body)
		}
		assertNothingWritten(t, "接口 "+r, before, orgRowCounts(t, w))
	}
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// callDryRun 带 ?dry_run=1 调一次接口，返回状态码与原始 body。
func callDryRun(t *testing.T, h http.Handler, token, method, path string, body any) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/api/v1"+path+"?dry_run=1", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// 不支持只看不做的写路径，收到 dry_run 要照实拒绝，而不是偷偷把东西写进去。
// 挑的是"不记动态、兜底够不着"的那几条（角色、能力标签、计价、组织信息、待确认操作的裁决、
// 设备码、语言设置、已读状态），它们各自在入口处挡了一下。
func TestUnsupportedDryRunRefusesInsteadOfWriting(t *testing.T) {
	w := newWorld(t)
	h := (&api.Server{App: w.a}).Handler()
	human, _, err := w.a.Login(w.ctx, w.slug+"-a@t.local", w.pw, w.slug)
	if err != nil {
		t.Fatal(err)
	}
	want := i18n.Tr(i18n.ZhCN, "err.dry_run_unsupported")
	cases := []struct {
		name, method, path string
		body               any
	}{
		{"裁决待确认操作（同意）", "POST", "/proposals/prp_nope/approve", nil},
		{"裁决待确认操作（拒绝）", "POST", "/proposals/prp_nope/reject", map[string]any{"reason": "这个理由够长了"}},
		{"角色与权限", "PUT", "/org/roles/tester", map[string]any{"title": map[string]string{"zh-CN": "测试"}, "permissions": []string{}}},
		{"删角色", "DELETE", "/org/roles/tester", nil},
		{"能力标签", "PUT", "/org/capabilities/coding", map[string]any{"title": map[string]string{"zh-CN": "编码"}}},
		{"删能力标签", "DELETE", "/org/capabilities/coding", nil},
		{"计价", "PUT", "/org/pricing/models/gpt-x", map[string]any{"input_per_1k": 1.0, "output_per_1k": 2.0, "currency": "CNY"}},
		{"汇率", "PUT", "/org/pricing/rates", map[string]any{"from": "USD", "to": "CNY", "rate": 7.1}},
		{"组织信息", "PATCH", "/org", map[string]any{"name": "改过的组织名"}},
		{"转让负责人", "POST", "/org/members/" + w.yi.MemberID + "/make-owner", nil},
		{"设备码同意", "POST", "/agent-auth/device/ABCD-EFGH/approve", map[string]any{"name": "某个 Agent"}},
		{"设备码拒绝", "POST", "/agent-auth/device/ABCD-EFGH/deny", nil},
		{"改语言", "PATCH", "/auth/me", map[string]any{"locale": "en-US"}},
		{"标记已读", "POST", "/notifications/read", map[string]any{}},
	}
	for _, c := range cases {
		before := orgRowCounts(t, w)
		code, body := callDryRun(t, h, human, c.method, c.path, c.body)
		if code != 400 || !strings.Contains(body, want) {
			t.Errorf("%s 收到 dry_run 应当回 400「%s」，实际 %d：%s", c.name, want, code, body)
		}
		assertNothingWritten(t, c.name, before, orgRowCounts(t, w))
	}
}
