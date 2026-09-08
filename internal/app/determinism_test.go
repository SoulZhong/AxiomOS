package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// ADR 0025：只看不做、幂等键、精确指代。三件事共同的验收标准是"系统不替人猜，也不重复做"。

// countedTables 是只看不做必须一行都不多的表。
var countedTables = []string{"tasks", "goals", "milestones", "sprints", "events", "comments", "artifacts",
	"runs", "task_relations", "proposals", "notifications", "idempotency_keys"}

func rowCounts(t *testing.T, a *App, ctx context.Context, orgID string) map[string]int {
	t.Helper()
	out := map[string]int{}
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		for _, table := range countedTables {
			n := 0
			if err := tx.QueryRow(ctx, `select count(*) from `+table).Scan(&n); err != nil {
				return err
			}
			out[table] = n
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertSameCounts(t *testing.T, what string, before, after map[string]int) {
	t.Helper()
	for _, table := range countedTables {
		if before[table] != after[table] {
			t.Fatalf("%s 的只看不做写了东西：%s 从 %d 变成 %d", what, table, before[table], after[table])
		}
	}
}

// wantWill 断言这次调用只回了一句「会……」，并且句子里带上该带的内容。
func wantWill(t *testing.T, what string, err error, must ...string) string {
	t.Helper()
	d, ok := AsDryRun(err)
	if !ok {
		t.Fatalf("%s 的只看不做应返回一句「会……」，实际 %v", what, err)
	}
	for _, m := range must {
		if !strings.Contains(d.Will, m) {
			t.Fatalf("%s 的句子里应有「%s」，实际「%s」", what, m, d.Will)
		}
	}
	return d.Will
}

func dry(sess *Session) *Session { return sess.WithWrite(WriteOptions{DryRun: true}) }
func key(sess *Session, k string) *Session {
	return sess.WithWrite(WriteOptions{IdempotencyKey: k})
}

// 每一条写路径的只看不做：句子说得准，库里一行不动。
func TestDryRunWritesNothing(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)

	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "只看不做的目标"})
	if err != nil {
		t.Fatal(err)
	}
	mine, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "登录页改版", AssigneeID: jia.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "订单导出", AssigneeID: jia.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	free, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "没人领的活", Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	day := time.Now().AddDate(0, 0, 7)
	sp, err := a.CreateSprint(ctx, jia, CreateSprintInput{Name: "第一个迭代", StartsOn: dayp(0), EndsOn: dayp(14)})
	if err != nil {
		t.Fatal(err)
	}
	next, err := a.CreateSprint(ctx, jia, CreateSprintInput{Name: "第二个迭代", StartsOn: dayp(15), EndsOn: dayp(29)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddTasksToSprint(ctx, jia, sp.ID, []string{other.ID}); err != nil {
		t.Fatal(err)
	}
	ms, err := a.CreateMilestone(ctx, jia, CreateMilestoneInput{GoalID: goal.ID, Title: "上线", DueOn: &day})
	if err != nil {
		t.Fatal(err)
	}

	before := rowCounts(t, a, ctx, orgID)

	cases := []struct {
		name string
		run  func() error
		must []string
	}{
		{"创建任务", func() error {
			_, err := a.CreateTask(ctx, dry(jia), CreateTaskInput{GoalID: goal.ID, Title: "不该出现的任务", AssigneeID: jia.MemberID, Ready: true})
			return err
		}, []string{"不该出现的任务", "指派给", "就绪"}},
		{"创建目标", func() error {
			_, err := a.CreateGoal(ctx, dry(jia), CreateGoalInput{Title: "不该出现的目标"})
			return err
		}, []string{"新建一个目标", "不该出现的目标"}},
		{"创建里程碑", func() error {
			_, err := a.CreateMilestone(ctx, dry(jia), CreateMilestoneInput{GoalID: goal.ID, Title: "不该出现的里程碑", DueOn: &day})
			return err
		}, []string{"不该出现的里程碑", goal.Title}},
		{"确认里程碑", func() error {
			_, err := a.ReachMilestone(ctx, dry(jia), ms.ID)
			return err
		}, []string{"已达到", ms.Title}},
		{"领取任务", func() error {
			_, err := a.Claim(ctx, dry(jia), free.ID)
			return err
		}, []string{"领取任务", free.Title}},
		{"推进流程", func() error {
			_, err := a.Transition(ctx, dry(jia), mine.ID, "start", TransitionPayload{})
			return err
		}, []string{"推进到", mine.Title}},
		{"附交付物", func() error {
			_, err := a.AddArtifact(ctx, dry(jia), mine.ID, domain.Artifact{Type: "result", Title: "结果说明"})
			return err
		}, []string{"交付物", "结果说明"}},
		{"评论", func() error {
			_, err := a.AddComment(ctx, dry(jia), mine.ID, "一句话", false)
			return err
		}, []string{"评论", mine.Title}},
		{"工作日志", func() error {
			_, err := a.AddComment(ctx, dry(jia), mine.ID, "一句话", true)
			return err
		}, []string{"工作日志"}},
		{"建立关联", func() error {
			_, err := a.Link(ctx, dry(jia), mine.ID, domain.RelationBlocks, other.ID)
			return err
		}, []string{"关联", mine.Title, other.Title}},
		{"指派", func() error {
			_, err := a.Assign(ctx, dry(jia), free.ID, yi.MemberID)
			return err
		}, []string{"指派给"}},
		{"改任务", func() error {
			title := "改过的标题"
			_, err := a.UpdateTask(ctx, dry(jia), mine.ID, UpdateTaskInput{Title: &title})
			return err
		}, []string{"改任务"}},
		{"加入迭代", func() error {
			_, err := a.AddTasksToSprint(ctx, dry(jia), sp.ID, []string{mine.ID})
			return err
		}, []string{"加进迭代", sp.Name}},
		{"移出迭代", func() error {
			return a.RemoveTaskFromSprint(ctx, dry(jia), sp.ID, other.ID)
		}, []string{"移出迭代", sp.Name}},
		{"开始迭代", func() error {
			_, err := a.StartSprint(ctx, dry(jia), sp.ID)
			return err
		}, []string{"开始迭代", sp.Name}},
		{"结束迭代", func() error {
			_, err := a.CloseSprint(ctx, dry(jia), sp.ID, "next", next.ID)
			return err
		}, []string{"结束迭代", next.Name}},
	}
	for _, c := range cases {
		will := wantWill(t, c.name, c.run(), c.must...)
		if !strings.HasPrefix(will, "会") || !strings.HasSuffix(will, "。") {
			t.Fatalf("%s 的句子应是一句「会……。」，实际「%s」", c.name, will)
		}
		assertSameCounts(t, c.name, before, rowCounts(t, a, ctx, orgID))
	}
	// 任务本身也没被改动
	got, err := a.GetTask(ctx, jia, mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "登录页改版" || got.State != "todo" {
		t.Fatalf("只看不做改动了任务：%s / %s", got.Title, got.State)
	}
}

// 会被拒绝的操作，只看不做也要照样拒绝——预演说"行"、真做说"不行"是最坏的结果。
func TestDryRunOfRefusedCallReturnsTheRefusal(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "拒绝验证任务", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	// 只有「执行任务」授权的 Agent：评论会被拒（缺授权）
	_, tok, err := a.RegisterAgent(ctx, yi, RegisterAgentInput{Name: "无评论授权", Grants: map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect}})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := a.SessionFromAgentToken(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	refusals := []struct {
		name string
		real func(*Session) error
	}{
		{"缺授权的评论", func(s *Session) error { _, e := a.AddComment(ctx, s, task.ID, "你好", false); return e }},
		{"还没到进行中就开始执行", func(s *Session) error { _, e := a.Begin(ctx, s, task.ID); return e }},
		{"指代不明的任务", func(s *Session) error { _, e := a.Claim(ctx, s, "根本没有的任务"); return e }},
	}
	for _, r := range refusals {
		realErr, dryErr := r.real(ag), r.real(dry(ag))
		if _, ok := AsDryRun(dryErr); ok {
			t.Fatalf("%s：会被拒绝的操作，只看不做也必须拒绝，实际给了一句「会……」", r.name)
		}
		if realErr == nil || dryErr == nil || realErr.Error() != dryErr.Error() {
			t.Fatalf("%s：两次的拒绝理由应完全一样：真做「%v」只看「%v」", r.name, realErr, dryErr)
		}
	}

	// 需要人确认的授权：只看不做要说清楚"不会立刻生效"，且不留下待确认操作
	_, tok2, err := a.RegisterAgent(ctx, yi, RegisterAgentInput{Name: "需确认", Grants: map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantWithApproval}})
	if err != nil {
		t.Fatal(err)
	}
	ag2, err := a.SessionFromAgentToken(ctx, tok2)
	if err != nil {
		t.Fatal(err)
	}
	before := rowCounts(t, a, ctx, orgID)
	will := wantWill(t, "需要人确认", func() error {
		_, e := a.Transition(ctx, dry(ag2), task.ID, "start", TransitionPayload{})
		return e
	}(), "不会立刻生效", "待确认操作")
	if !strings.Contains(will, "乙") {
		t.Fatalf("应说清等谁确认，实际「%s」", will)
	}
	assertSameCounts(t, "需要人确认", before, rowCounts(t, a, ctx, orgID))
}

// 幂等键：同一个键 24 小时内只生效一次；换了参数直接拒绝，不许悄悄返回旧结果。
func TestIdempotencyKey(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, _ := newTestOrg(t, a, ctx)

	first, err := a.CreateTask(ctx, key(jia, "abc-1"), CreateTaskInput{Title: "只该建一次的任务"})
	if err != nil {
		t.Fatal(err)
	}
	after := rowCounts(t, a, ctx, orgID)

	// 同键同参数：不再写一次，返回第一次的结果
	_, err = a.CreateTask(ctx, key(jia, "abc-1"), CreateTaskInput{Title: "只该建一次的任务"})
	rp, ok := AsRepeated(err)
	if !ok {
		t.Fatalf("重复提交应命中幂等键，实际 %v", err)
	}
	if rp.Message != i18n.Tr(i18n.ZhCN, "idem.repeated") {
		t.Fatalf("句子不对：%s", rp.Message)
	}
	var got domain.Task
	if err := json.Unmarshal(rp.Result, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != first.ID || rp.Ref != first.ID {
		t.Fatalf("返回的应是第一次的结果：%s / %s，第一次是 %s", got.ID, rp.Ref, first.ID)
	}
	again := rowCounts(t, a, ctx, orgID)
	if again["tasks"] != after["tasks"] || again["events"] != after["events"] {
		t.Fatalf("重复提交又写了一遍：任务 %d→%d 动态 %d→%d", after["tasks"], again["tasks"], after["events"], again["events"])
	}

	// 同键换参数：拒绝，且不是返回旧结果
	_, err = a.CreateTask(ctx, key(jia, "abc-1"), CreateTaskInput{Title: "换了个标题"})
	if err == nil {
		t.Fatal("同一个键换了参数应被拒绝")
	}
	if _, ok := AsRepeated(err); ok {
		t.Fatal("同一个键换了参数不能返回上次的结果")
	}
	want := i18n.Trf(i18n.ZhCN, "err.idem_conflict", "abc-1")
	if err.Error() != want {
		t.Fatalf("拒绝的句子应为「%s」，实际「%s」", want, err.Error())
	}

	// 太长的键
	if _, err := a.CreateTask(ctx, key(jia, strings.Repeat("x", 65)), CreateTaskInput{Title: "键太长"}); err == nil ||
		!strings.Contains(err.Error(), "最长") {
		t.Fatalf("超长的键应被拒绝，实际 %v", err)
	}

	// 只看不做不留键：预演之后同一个键还能真做
	if _, err := a.CreateTask(ctx, jia.WithWrite(WriteOptions{DryRun: true, IdempotencyKey: "abc-2"}),
		CreateTaskInput{Title: "先看一眼"}); err == nil {
		t.Fatal("只看不做应返回一句「会……」")
	}
	real2, err := a.CreateTask(ctx, key(jia, "abc-2"), CreateTaskInput{Title: "先看一眼"})
	if err != nil {
		t.Fatalf("预演不该挡住真做：%v", err)
	}
	if real2 == nil {
		t.Fatal("真做应该建出任务")
	}

	// 别的写路径同样认键
	if _, err := a.CreateGoal(ctx, key(jia, "goal-1"), CreateGoalInput{Title: "只该建一次的目标"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateGoal(ctx, key(jia, "goal-1"), CreateGoalInput{Title: "只该建一次的目标"}); err == nil {
		t.Fatal("重复建目标应命中幂等键")
	} else if _, ok := AsRepeated(err); !ok {
		t.Fatalf("应命中幂等键，实际 %v", err)
	}

	// 过期的键清得掉，清掉之后同一个键可以重新用
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update idempotency_keys set expires_at = now() - interval '1 hour'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	n, err := a.SweepIdempotencyKeys(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("过期的键应该被清掉")
	}
	if rowCounts(t, a, ctx, orgID)["idempotency_keys"] != 0 {
		t.Fatal("清完之后不该还有键")
	}
}

// 精确指代：四类对象都认确切写法，认不准就列候选，绝不替人挑一个。
func TestPreciseReferences(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, _ := newTestOrg(t, a, ctx)

	t1, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "登录页改版"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "登录失败率排查"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "登录页 A/B"}); err != nil {
		t.Fatal(err)
	}

	// 序号、ID、唯一片段都能认出同一个任务
	for _, ref := range []string{"#" + itoa(t1.Number), itoa(t1.Number), t1.ID, "改版"} {
		id, err := a.ResolveTaskRef(ctx, jia, ref)
		if err != nil {
			t.Fatalf("「%s」应解析出任务：%v", ref, err)
		}
		if id != t1.ID {
			t.Fatalf("「%s」解析成了 %s，应为 %s", ref, id, t1.ID)
		}
	}
	// 不明确：列候选，句子里带编号
	_, err = a.ResolveTaskRef(ctx, jia, "登录")
	if err == nil {
		t.Fatal("三个任务都带「登录」，不该猜一个")
	}
	msg := err.Error()
	for _, want := range []string{"有 3 个任务名字里带「登录」", "#" + itoa(t1.Number) + " 登录页改版", "告诉我编号"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("候选句子里应有「%s」，实际「%s」", want, msg)
		}
	}
	// 一个都没有
	if _, err := a.ResolveTaskRef(ctx, jia, "根本没有的东西"); err == nil ||
		!strings.Contains(err.Error(), "没有名字里带「根本没有的东西」的任务") {
		t.Fatalf("没匹配上应照实说，实际 %v", err)
	}

	// 目标
	g1, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "用户增长"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "收入增长"}); err != nil {
		t.Fatal(err)
	}
	if id, err := a.ResolveRef(ctx, jia, RefGoal, "用户"); err != nil || id != g1.ID {
		t.Fatalf("唯一片段应解析出目标：%s %v", id, err)
	}
	if id, err := a.ResolveRef(ctx, jia, RefGoal, g1.ID); err != nil || id != g1.ID {
		t.Fatalf("目标编号应原样认出：%s %v", id, err)
	}
	if _, err := a.ResolveRef(ctx, jia, RefGoal, "增长"); err == nil ||
		!strings.Contains(err.Error(), "有 2 个目标名字里带「增长」") || !strings.Contains(err.Error(), g1.ID) {
		t.Fatalf("两个目标都带「增长」，应列候选，实际 %v", err)
	}
	if _, err := a.ResolveRef(ctx, jia, RefGoal, "没有这个目标"); err == nil ||
		!strings.Contains(err.Error(), "没有名字里带「没有这个目标」的目标") {
		t.Fatalf("没匹配上应照实说，实际 %v", err)
	}

	// 成员：@名字、编号、邮箱
	var email string
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		m, err := a.Store.MemberByID(ctx, tx, jia.MemberID)
		if err != nil {
			return err
		}
		acc, err := a.Store.AccountByID(ctx, tx, m.AccountID)
		if err != nil {
			return err
		}
		email = acc.Email
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"@甲", jia.MemberID, email} {
		id, err := a.ResolveRef(ctx, jia, RefMember, ref)
		if err != nil || id != jia.MemberID {
			t.Fatalf("「%s」应解析出成员甲：%s %v", ref, id, err)
		}
	}
	// 两个 Agent 的名字都带「助手」：不许猜
	for _, n := range []string{"写作助手", "测试助手"} {
		if _, _, err := a.RegisterAgent(ctx, jia, RegisterAgentInput{Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.ResolveRef(ctx, jia, RefMember, "@助手"); err == nil ||
		!strings.Contains(err.Error(), "有 2 个人名字里带「助手」") || !strings.Contains(err.Error(), "告诉我编号或邮箱") {
		t.Fatalf("两个助手应列候选，实际 %v", err)
	}
	if _, err := a.ResolveRef(ctx, jia, RefMember, "@查无此人"); err == nil ||
		!strings.Contains(err.Error(), "没有名字里带「查无此人」的人") {
		t.Fatalf("没匹配上应照实说，实际 %v", err)
	}

	// 迭代
	s1, err := a.CreateSprint(ctx, jia, CreateSprintInput{Name: "九月第一迭代", StartsOn: dayp(0), EndsOn: dayp(13)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateSprint(ctx, jia, CreateSprintInput{Name: "九月第二迭代", StartsOn: dayp(14), EndsOn: dayp(27)}); err != nil {
		t.Fatal(err)
	}
	if id, err := a.ResolveRef(ctx, jia, RefSprint, "第一"); err != nil || id != s1.ID {
		t.Fatalf("唯一片段应解析出迭代：%s %v", id, err)
	}
	if id, err := a.ResolveRef(ctx, jia, RefSprint, s1.ID); err != nil || id != s1.ID {
		t.Fatalf("迭代编号应原样认出：%s %v", id, err)
	}
	if _, err := a.ResolveRef(ctx, jia, RefSprint, "九月"); err == nil ||
		!strings.Contains(err.Error(), "有 2 个迭代名字里带「九月」") {
		t.Fatalf("两个迭代都带「九月」，应列候选，实际 %v", err)
	}
	if _, err := a.ResolveRef(ctx, jia, RefSprint, "十月"); err == nil ||
		!strings.Contains(err.Error(), "没有名字里带「十月」的迭代") {
		t.Fatalf("没匹配上应照实说，实际 %v", err)
	}

	// 候选最多列 5 个，超出的用「等」带过
	for i := 0; i < 6; i++ {
		if _, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "批量任务 " + itoa(i)}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = a.ResolveTaskRef(ctx, jia, "批量任务")
	if err == nil || !strings.Contains(err.Error(), "有 6 个任务名字里带「批量任务」") || !strings.Contains(err.Error(), "等") {
		t.Fatalf("超过 5 个候选应截断并带「等」，实际 %v", err)
	}
	_ = store.IdempotencyTTL
}

// 幂等键在并发下也只让一件事发生一次（ADR 0025 第 3 条）。
//
// 这是"先占位再干活"要挡的那一幕：同一个键的 N 个请求同时进来，只有一个能真做，
// 其余的要么等到结果（重复提交），要么被告知"上一次还在进行中"——不管是哪一种，
// 副作用只能有一份：任务多一条、动态多一批、键多一条。
func TestIdempotencyKeyUnderConcurrency(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, _ := newTestOrg(t, a, ctx)

	// 先老老实实建一次，量出"建一个任务"该有多少行、多少条动态，后面拿它当标尺。
	base := rowCounts(t, a, ctx, orgID)
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "量标尺用的任务", Ready: true}); err != nil {
		t.Fatal(err)
	}
	one := rowCounts(t, a, ctx, orgID)
	wantTasks := one["tasks"] - base["tasks"]
	wantEvents := one["events"] - base["events"]

	const n = 8
	before := rowCounts(t, a, ctx, orgID)
	type result struct {
		task *domain.Task
		err  error
	}
	out := make([]result, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			tk, err := a.CreateTask(ctx, key(jia, "并发同一个键"), CreateTaskInput{Title: "只该建一次的任务", Ready: true})
			out[i] = result{tk, err}
		}(i)
	}
	close(start)
	wg.Wait()

	okCount, repeatCount, busyCount := 0, 0, 0
	var made *domain.Task
	inProgress := i18n.Tr(i18n.ZhCN, "err.idem_in_progress")
	for i, r := range out {
		switch {
		case r.err == nil:
			okCount++
			made = r.task
		case func() bool { _, ok := AsRepeated(r.err); return ok }():
			repeatCount++
		case r.err.Error() == inProgress:
			busyCount++
		default:
			t.Fatalf("第 %d 个并发调用给了意料之外的结果：%v", i, r.err)
		}
	}
	if okCount != 1 {
		t.Fatalf("同一个键的 %d 个并发调用应当只有一个真做成，实际 %d 个", n, okCount)
	}
	if repeatCount+busyCount != n-1 {
		t.Fatalf("其余 %d 个应当是「重复提交」或「上一次还在进行中」，实际重复 %d、进行中 %d",
			n-1, repeatCount, busyCount)
	}
	// 重复提交拿回来的必须是第一次的那个任务，不是另建的
	for _, r := range out {
		if rp, ok := AsRepeated(r.err); ok && rp.Ref != made.ID {
			t.Fatalf("重复提交返回的应是第一次建的任务 %s，实际 %s", made.ID, rp.Ref)
		}
	}

	after := rowCounts(t, a, ctx, orgID)
	if got := after["tasks"] - before["tasks"]; got != wantTasks {
		t.Fatalf("%d 个并发调用建出了 %d 个任务，应当只有 %d 个", n, got, wantTasks)
	}
	if got := after["events"] - before["events"]; got != wantEvents {
		t.Fatalf("%d 个并发调用写了 %d 条动态，应当只有 %d 条", n, got, wantEvents)
	}
	if got := after["idempotency_keys"] - before["idempotency_keys"]; got != 1 {
		t.Fatalf("同一个键应当只留下 1 条记录，实际 %d 条", got)
	}
	// 干完之后那条记录是"干完了"，重复提交才拿得到结果
	rec := idemRow(t, a, ctx, orgID, jia.Actor.ID, "并发同一个键")
	if rec.State != store.IdemDone {
		t.Fatalf("做完之后占位应改成「干完了」，实际「%s」", rec.State)
	}
	if rec.ResultRef != made.ID {
		t.Fatalf("记下的结果应指向第一次建的任务：%s / %s", rec.ResultRef, made.ID)
	}
}

// idemRow 直接读一条幂等记录（测试用）。
func idemRow(t *testing.T, a *App, ctx context.Context, orgID, executorID, key string) *store.IdempotencyRecord {
	t.Helper()
	var rec *store.IdempotencyRecord
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		r, err := a.Store.IdempotencyRecordByKey(ctx, tx, executorID, key)
		rec = r
		return err
	}); err != nil {
		t.Fatalf("读幂等记录失败：%v", err)
	}
	return rec
}

// 占位的三种下场：还在租约里的挡住后来者；租约断了的可以接手；干砸了的直接撤掉。
func TestIdempotencyReservationLease(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, _ := newTestOrg(t, a, ctx)

	// 手工放一条"刚占上、还在干"的记录：同键的下一次调用应当被挡住，且什么都不写
	reserve := func(k string, startedAgo time.Duration) {
		t.Helper()
		if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `insert into idempotency_keys(id,org_id,executor_id,key,endpoint,args_hash,state,result_ref,result,created_at,started_at,expires_at)
                values($1,$2,$3,$4,'create_task',$5,'pending','','null',now(),now()-$6::interval,now()+interval '24 hours')`,
				store.NewID("idm"), orgID, jia.Actor.ID, k, argsHash("create_task", CreateTaskInput{Title: "占位的任务", Ready: true}),
				startedAgo.String())
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}

	reserve("租约里", 5*time.Second)
	before := rowCounts(t, a, ctx, orgID)
	_, err := a.CreateTask(ctx, key(jia, "租约里"), CreateTaskInput{Title: "占位的任务", Ready: true})
	if err == nil || err.Error() != i18n.Tr(i18n.ZhCN, "err.idem_in_progress") {
		t.Fatalf("上一次还在进行中时应当被挡住，实际 %v", err)
	}
	assertSameCounts(t, "上一次还在进行中", before, rowCounts(t, a, ctx, orgID))

	// 租约断了：上一次的进程死了，后来者接手继续做
	reserve("租约断了", store.IdempotencyLease+time.Minute)
	tk, err := a.CreateTask(ctx, key(jia, "租约断了"), CreateTaskInput{Title: "占位的任务", Ready: true})
	if err != nil {
		t.Fatalf("租约断了应当接手继续做，实际 %v", err)
	}
	if rec := idemRow(t, a, ctx, orgID, jia.Actor.ID, "租约断了"); rec.State != store.IdemDone || rec.ResultRef != tk.ID {
		t.Fatalf("接手做完之后应当记成「干完了」并指向新任务：%+v", rec)
	}

	// 干砸了：占位要撤掉，同一个键还能重试
	before = rowCounts(t, a, ctx, orgID)
	if _, err := a.CreateTask(ctx, key(jia, "先失败一次"), CreateTaskInput{Title: ""}); err == nil {
		t.Fatal("空标题应当被拒绝")
	}
	if got := rowCounts(t, a, ctx, orgID)["idempotency_keys"] - before["idempotency_keys"]; got != 0 {
		t.Fatalf("没干成的占位应当撤掉，实际留下了 %d 条", got)
	}
	if _, err := a.CreateTask(ctx, key(jia, "先失败一次"), CreateTaskInput{Title: "这次成了", Ready: true}); err != nil {
		t.Fatalf("失败之后同一个键应当还能重试，实际 %v", err)
	}
}

// 批量改时间桶与批量重排也要认「只看不做」：句子里说清几个改得动、几个改不动，库里一行不动。
func TestBulkGoalDryRun(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)

	var mine []*domain.Goal
	for _, title := range []string{"乙的目标一", "乙的目标二", "乙的目标三"} {
		g, err := a.CreateGoal(ctx, yi, CreateGoalInput{Title: title})
		if err != nil {
			t.Fatal(err)
		}
		mine = append(mine, g)
	}
	others, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "甲的目标"})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{mine[0].ID, mine[1].ID, mine[2].ID, others.ID}

	before := rowCounts(t, a, ctx, orgID)
	wantWill(t, "批量改时间桶", func() error {
		_, err := a.BulkGoalHorizon(ctx, dry(yi), BulkGoalHorizonInput{IDs: ids, Horizon: domain.HorizonNow})
		return err
	}(), "会把 3 个目标的时间桶改成「现在」", "另有 1 个目标你没有权限改")
	assertSameCounts(t, "批量改时间桶", before, rowCounts(t, a, ctx, orgID))

	wantWill(t, "批量重排", func() error {
		_, err := a.SetGoalRanks(ctx, dry(yi), GoalRankInput{IDs: ids})
		return err
	}(), "会重新排 3 个目标的次序", "另有 1 个目标你没有权限改")
	assertSameCounts(t, "批量重排", before, rowCounts(t, a, ctx, orgID))

	// 预演之后目标本身也没变：时间桶还空着，次序还没发
	for _, g := range mine {
		got, err := a.GetGoal(ctx, yi, g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Horizon != "" || got.Rank != nil {
			t.Fatalf("只看不做改动了目标「%s」：时间桶 %q 次序 %v", got.Title, got.Horizon, got.Rank)
		}
	}

	// 真做一次，句子说的就是真发生的事
	out, err := a.BulkGoalHorizon(ctx, yi, BulkGoalHorizonInput{IDs: ids, Horizon: domain.HorizonNow})
	if err != nil {
		t.Fatal(err)
	}
	if out.Updated != 3 || len(out.Skipped) != 1 {
		t.Fatalf("真做应当改 3 个跳过 1 个，实际 %+v", out)
	}

	// 一个都改不动时不说「会把 0 个目标……」，只说跳过的部分
	will := wantWill(t, "全都改不动", func() error {
		_, err := a.BulkGoalHorizon(ctx, dry(yi), BulkGoalHorizonInput{IDs: []string{others.ID}, Horizon: domain.HorizonNow})
		return err
	}(), "另有 1 个目标你没有权限改")
	if strings.Contains(will, "0 个") {
		t.Fatalf("一个都改不动时不该说「0 个」：%s", will)
	}
}
