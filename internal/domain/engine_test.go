package domain

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// world 是测试用的内存世界，模拟应用层的装载与持久化。
type world struct {
	t      *testing.T
	types  map[string]*TaskType
	tasks  map[string]*Task
	runs   []*Run
	events []Event
	seq    map[string]int
	clock  time.Time
	actors map[string]*Executor
}

func newWorld(t *testing.T) *world {
	w := &world{t: t, types: map[string]*TaskType{}, tasks: map[string]*Task{}, seq: map[string]int{}, clock: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), actors: map[string]*Executor{}}
	for _, tt := range BuiltinTaskTypes() {
		if errs := Validate(tt); len(errs) > 0 {
			t.Fatalf("内置任务类型 %s 校验失败: %v", tt.Name, errs)
		}
		w.types[tt.Name] = tt
	}
	roles := func(r ...string) []string { return r }
	w.actors["wang"] = &Executor{ID: "wang", Kind: ExecutorMember, Name: "小王", Roles: roles("pm", "designer")}
	w.actors["li"] = &Executor{ID: "li", Kind: ExecutorMember, Name: "小李", Roles: roles("developer")}
	w.actors["zhang"] = &Executor{ID: "zhang", Kind: ExecutorMember, Name: "小张", Roles: roles("tester")}
	w.actors["zhao"] = &Executor{ID: "zhao", Kind: ExecutorMember, Name: "小赵", Roles: roles("releaser", "admin")}
	g := func(gs ...Grant) map[Grant]GrantMode {
		m := map[Grant]GrantMode{}
		for _, x := range gs {
			m[x] = GrantDirect
		}
		return m
	}
	// Agent 的 Roles 由应用层复制自所有者
	w.actors["li-agent"] = &Executor{ID: "li-agent", Kind: ExecutorAgent, Name: "小李的 Agent", OwnerID: "li", Roles: roles("developer"), Grants: g(GrantExecute, GrantClaimBacklog, GrantComment), Capabilities: []string{"coding"}, MaxConcurrent: 1}
	w.actors["zhang-agent"] = &Executor{ID: "zhang-agent", Kind: ExecutorAgent, Name: "小张的 Agent", OwnerID: "zhang", Roles: roles("tester"), Grants: g(GrantExecute, GrantClaimBacklog, GrantReview), Capabilities: []string{"testing"}, MaxConcurrent: 2}
	w.actors["wang-agent"] = &Executor{ID: "wang-agent", Kind: ExecutorAgent, Name: "小王的 Agent", OwnerID: "wang", Roles: roles("pm", "designer"), Grants: g(GrantExecute, GrantComment), Capabilities: []string{"writing"}}
	return w
}

func (w *world) id(p string) string {
	w.seq[p]++
	return fmt.Sprintf("%s%d", strings.ToUpper(p[:1]), w.seq[p])
}
func (w *world) tick() time.Time { w.clock = w.clock.Add(time.Minute); return w.clock }

func (w *world) create(typ, title, creator string, participants map[string]string) *Task {
	tt := w.types[typ]
	t := &Task{ID: w.id("task"), TypeName: typ, TypeVersion: tt.Workflow.Version, Title: title, CreatorID: creator, ReviewerID: creator, State: tt.Workflow.Initial, Participants: map[string]string{}, CreatedAt: w.tick()}
	for k, v := range participants {
		t.Participants[k] = v
	}
	w.tasks[t.ID] = t
	w.events = append(w.events, Event{Type: "TaskCreated", TaskID: t.ID, ActorID: creator, At: t.CreatedAt})
	return t
}

func (w *world) activeRun(taskID string) *Run {
	for _, r := range w.runs {
		if r.TaskID == taskID && r.Active() {
			return r
		}
	}
	return nil
}

func (w *world) ctx(taskID, actorID string) *Context {
	t := w.tasks[taskID]
	c := &Context{Task: t, Type: w.types[t.TypeName], Types: w.types, ActiveRun: w.activeRun(taskID), Now: w.tick(), NewID: w.id}
	for _, o := range w.tasks {
		for _, r := range o.Relations {
			if r.Type == RelationBlocks && r.OtherID == taskID {
				c.Predecessors = append(c.Predecessors, o)
			}
			if r.Type == RelationFoundIn && r.OtherID == taskID && o.TypeName == "bug" {
				c.Bugs = append(c.Bugs, o)
			}
		}
	}
	for _, r := range w.runs {
		if r.ExecutorID == actorID && r.Active() {
			c.ActorRuns++
		}
	}
	return c
}

func (w *world) apply(o *Outcome, err error) error {
	if err != nil {
		return err
	}
	if o.OpenedRun != nil {
		w.runs = append(w.runs, o.OpenedRun)
	}
	w.events = append(w.events, o.Events...)
	return nil
}

func (w *world) do(taskID, actorID, name, comment string) error {
	return w.apply(Apply(w.ctx(taskID, actorID), w.actors[actorID], name, Payload{Comment: comment}))
}
func (w *world) claim(taskID, actorID string) error {
	return w.apply(Claim(w.ctx(taskID, actorID), w.actors[actorID]))
}
func (w *world) begin(taskID, actorID string) error {
	return w.apply(Begin(w.ctx(taskID, actorID), w.actors[actorID]))
}
func (w *world) usage(taskID, actorID string, tokens int64) error {
	return w.apply(ReportUsage(w.ctx(taskID, actorID), w.actors[actorID], []Usage{{ModelID: "claude-sonnet-5", OutputTokens: tokens}}))
}
func (w *world) artifact(taskID, actorID, typ string) {
	w.apply(AddArtifact(w.ctx(taskID, actorID), w.actors[actorID], Artifact{Type: typ, Ref: "ref"}), nil)
}
func (w *world) assign(taskID, actorID, to string) error {
	return w.apply(Assign(w.ctx(taskID, actorID), w.actors[actorID], to))
}
func (w *world) link(from, typ, to, actorID string) error {
	blocksOf := func(id string) []string {
		var out []string
		for _, r := range w.tasks[id].Relations {
			if r.Type == RelationBlocks {
				out = append(out, r.OtherID)
			}
		}
		return out
	}
	return w.apply(Link(w.ctx(from, actorID), w.actors[actorID], RelationType(typ), to, blocksOf))
}

func must(t *testing.T, err error, step string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s：预期成功，实际被挡：%v", step, err)
	}
}
func mustFail(t *testing.T, err error, step, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s：预期被挡，实际成功", step)
	}
	if want != "" && !strings.Contains(err.Error(), want) {
		t.Fatalf("%s：拒绝理由不符，想要包含「%s」，实际「%s」", step, want, err)
	}
}
func wantState(t *testing.T, task *Task, state string) {
	t.Helper()
	if task.State != state {
		t.Fatalf("状态应为 %s，实际 %s", state, task.State)
	}
}

// 场景 A：通用任务全程
func TestScenarioA_GenericTaskLifecycle(t *testing.T) {
	w := newWorld(t)
	task := w.create("generic", "写周报", "wang", nil)
	must(t, w.assign(task.ID, "wang", "li"), "指派给小李")
	must(t, w.do(task.ID, "wang", "ready", ""), "就绪")
	must(t, w.do(task.ID, "li-agent", "start", ""), "小李的 Agent 开始")
	if r := w.activeRun(task.ID); r == nil || r.ExecutorID != "li-agent" {
		t.Fatalf("开始后应有执行者为 li-agent 的执行记录，实际 %+v", r)
	}
	must(t, w.usage(task.ID, "li-agent", 1200), "上报消耗")
	must(t, w.do(task.ID, "li-agent", "ask_for_input", "周报要含哪几个项目？"), "提问等待")
	if w.activeRun(task.ID) != nil {
		t.Fatal("提问等待后执行记录应结束")
	}
	mustFail(t, w.do(task.ID, "li", "resume", "我自己答"), "小李自己答复", "只能由")
	must(t, w.do(task.ID, "wang", "resume", "含 A、B 两个项目"), "小王答复")
	if w.activeRun(task.ID) != nil {
		t.Fatal("由创建者恢复不应开始执行记录")
	}
	mustFail(t, w.usage(task.ID, "li-agent", 100), "没开始就上报", "没有进行中的执行记录")
	must(t, w.begin(task.ID, "li-agent"), "Agent 开始执行")
	mustFail(t, w.do(task.ID, "li-agent", "submit", ""), "无交付物提交", "缺少交付物")
	w.artifact(task.ID, "li-agent", "result")
	must(t, w.usage(task.ID, "li-agent", 800), "上报消耗")
	must(t, w.do(task.ID, "li-agent", "submit", ""), "提交")
	mustFail(t, w.do(task.ID, "li", "accept", ""), "小李自己验收", "验收人")
	must(t, w.do(task.ID, "wang", "reject", "少了项目 B"), "打回")
	wantState(t, task, "todo")
	if task.AssigneeID != "li" {
		t.Fatalf("打回后负责人应保留为小李，实际 %s", task.AssigneeID)
	}
	must(t, w.do(task.ID, "li-agent", "start", ""), "再开始")
	must(t, w.usage(task.ID, "li-agent", 500), "上报消耗")
	must(t, w.do(task.ID, "li-agent", "submit", ""), "再提交")
	must(t, w.do(task.ID, "wang", "accept", ""), "验收通过")
	wantState(t, task, "done")
	if Progress(task, w.types["generic"]) != 100 {
		t.Fatal("完成后进度应为 100")
	}
	if len(w.runs) != 3 {
		t.Fatalf("应有 3 段执行记录，实际 %d", len(w.runs))
	}
}

// 场景 B：需求跨角色流转
func TestScenarioB_RequirementAcrossRoles(t *testing.T) {
	w := newWorld(t)
	task := w.create("requirement", "登录页改版", "wang", map[string]string{"designer": "wang", "developer": "li", "tester": "zhang", "releaser": "zhao"})
	must(t, w.do(task.ID, "wang", "start_design", ""), "开始设计")
	if r := w.activeRun(task.ID); r == nil || r.ExecutorID != "wang" || r.State != "designing" {
		t.Fatalf("设计阶段应有小王的执行记录，实际 %+v", r)
	}
	w.artifact(task.ID, "wang", "prd")
	must(t, w.do(task.ID, "wang", "design_done", ""), "设计完成")
	if task.AssigneeID != "li" {
		t.Fatalf("负责人应切到小李，实际 %s", task.AssigneeID)
	}
	if w.activeRun(task.ID) != nil {
		t.Fatal("交接后不应自动开始执行记录")
	}
	mustFail(t, w.usage(task.ID, "li-agent", 100), "没开始就上报", "没有进行中的执行记录")
	must(t, w.begin(task.ID, "li-agent"), "小李的 Agent 开始执行")
	must(t, w.usage(task.ID, "li-agent", 12000), "上报")
	must(t, w.do(task.ID, "li-agent", "ask_for_input", "按钮颜色？"), "提问")
	must(t, w.do(task.ID, "wang", "resume", "品牌蓝"), "答复")
	wantState(t, task, "developing")
	must(t, w.begin(task.ID, "li-agent"), "再开始")
	must(t, w.usage(task.ID, "li-agent", 18000), "上报")
	w.artifact(task.ID, "li-agent", "pr")
	must(t, w.do(task.ID, "li-agent", "dev_done", ""), "开发完成")
	if task.AssigneeID != "zhang" {
		t.Fatalf("负责人应切到小张，实际 %s", task.AssigneeID)
	}
	must(t, w.begin(task.ID, "zhang-agent"), "小张的 Agent 开始")
	must(t, w.usage(task.ID, "zhang-agent", 9000), "上报")
	w.artifact(task.ID, "zhang-agent", "test_report")
	must(t, w.do(task.ID, "zhang-agent", "test_pass", ""), "测试通过")
	if task.AssigneeID != "zhao" {
		t.Fatalf("负责人应切到小赵，实际 %s", task.AssigneeID)
	}
	w.artifact(task.ID, "zhao", "release_note")
	must(t, w.do(task.ID, "zhao", "release_done", ""), "发布完成（不开执行记录）")
	wantState(t, task, "awaiting_acceptance")
	if Progress(task, w.types["requirement"]) != 95 {
		t.Fatalf("待验收进度应为 95，实际 %d", Progress(task, w.types["requirement"]))
	}
	must(t, w.do(task.ID, "wang", "accept", ""), "验收")
	wantState(t, task, "done")

	// 执行记录归属：设计=wang，开发两段=li-agent，测试=zhang-agent
	var got []string
	for _, r := range w.runs {
		got = append(got, r.State+":"+r.ExecutorID)
	}
	want := "designing:wang developing:li-agent developing:li-agent testing:zhang-agent"
	if strings.Join(got, " ") != want {
		t.Fatalf("执行记录应为 %s，实际 %s", want, strings.Join(got, " "))
	}
	var dev int64
	for _, r := range w.runs {
		if r.State == "developing" {
			for _, u := range r.Usage {
				dev += u.TotalTokens()
			}
		}
	}
	if dev != 30000 {
		t.Fatalf("开发阶段消耗应为 30000，实际 %d", dev)
	}
}

// 场景 C：Bug 阻塞上线
func TestScenarioC_BugBlocksRelease(t *testing.T) {
	w := newWorld(t)
	req := w.create("requirement", "支付页优化", "wang", map[string]string{"designer": "wang", "developer": "li", "tester": "zhang", "releaser": "zhao"})
	must(t, w.do(req.ID, "wang", "start_design", ""), "")
	w.artifact(req.ID, "wang", "prd")
	must(t, w.do(req.ID, "wang", "design_done", ""), "")
	w.artifact(req.ID, "li", "pr")
	must(t, w.do(req.ID, "li", "dev_done", ""), "")
	wantState(t, req, "testing")

	bug := w.create("bug", "金额显示错位", "zhang", map[string]string{"developer": "li", "tester": "zhang"})
	must(t, w.link(bug.ID, "found_in", req.ID, "zhang"), "关联发现于")
	w.artifact(req.ID, "zhang", "test_report")
	mustFail(t, w.do(req.ID, "zhang", "test_pass", ""), "有 Bug 时测试通过", "未关闭的 Bug")

	must(t, w.do(bug.ID, "zhang", "confirm", ""), "确认 Bug")
	if bug.AssigneeID != "li" {
		t.Fatalf("Bug 负责人应为小李，实际 %s", bug.AssigneeID)
	}
	must(t, w.do(bug.ID, "li-agent", "start_fix", ""), "开始修复")
	if r := w.activeRun(bug.ID); r == nil || r.ExecutorID != "li-agent" {
		t.Fatal("修复阶段应有 li-agent 的执行记录")
	}
	must(t, w.usage(bug.ID, "li-agent", 4000), "")
	w.artifact(bug.ID, "li-agent", "pr")
	must(t, w.do(bug.ID, "li-agent", "fixed", ""), "修复完成")
	if bug.AssigneeID != "zhang" {
		t.Fatal("Bug 负责人应切到小张")
	}
	must(t, w.do(bug.ID, "zhang-agent", "verify", ""), "Agent 验证通过（有验收授权）")
	wantState(t, bug, "verified")
	must(t, w.do(req.ID, "zhang", "test_pass", ""), "Bug 关闭后测试通过")
	wantState(t, req, "releasing")
}

// 场景 D：参与角色空缺进待领取
func TestScenarioD_EmptySlotGoesToBacklog(t *testing.T) {
	w := newWorld(t)
	req := w.create("requirement", "导出报表", "wang", map[string]string{"designer": "wang", "developer": "li", "releaser": "zhao"})
	req.RequiredCapabilities = []string{"testing"}
	must(t, w.do(req.ID, "wang", "start_design", ""), "")
	w.artifact(req.ID, "wang", "prd")
	must(t, w.do(req.ID, "wang", "design_done", ""), "")
	w.artifact(req.ID, "li", "pr")
	must(t, w.do(req.ID, "li", "dev_done", ""), "开发完成")
	wantState(t, req, "testing")
	if req.AssigneeID != "" || req.RequiredRole != "tester" || req.PendingSlot != "tester" {
		t.Fatalf("应进入待领取并要求 tester，实际 assignee=%q role=%q slot=%q", req.AssigneeID, req.RequiredRole, req.PendingSlot)
	}
	if w.activeRun(req.ID) != nil {
		t.Fatal("无负责人时不应有执行记录")
	}
	mustFail(t, w.claim(req.ID, "li-agent"), "小李的 Agent 领取", "需要「测试」角色")
	mustFail(t, w.claim(req.ID, "wang-agent"), "小王的 Agent 领取", "领取任务")
	must(t, w.claim(req.ID, "zhang-agent"), "小张的 Agent 领取")
	if req.AssigneeID != "zhang-agent" || req.Participants["tester"] != "zhang-agent" {
		t.Fatalf("领取后负责人与测试角色应为 zhang-agent，实际 %s / %s", req.AssigneeID, req.Participants["tester"])
	}
	if r := w.activeRun(req.ID); r == nil || r.ExecutorID != "zhang-agent" || r.State != "testing" {
		t.Fatalf("领取处于进行中的任务应立即开始执行记录，实际 %+v", r)
	}
}

// 场景 E：发布前置与绕圈检测、终止自动解除前置
func TestScenarioE_ReleaseDependencies(t *testing.T) {
	w := newWorld(t)
	p := map[string]string{"designer": "wang", "developer": "li", "tester": "zhang", "releaser": "zhao"}
	a := w.create("requirement", "需求甲", "wang", p)
	b := w.create("requirement", "需求乙", "wang", p)
	rel := w.create("release", "v2.0 发布", "zhao", map[string]string{"releaser": "zhao"})
	must(t, w.link(a.ID, "blocks", rel.ID, "zhao"), "甲前置")
	must(t, w.link(b.ID, "blocks", rel.ID, "zhao"), "乙前置")
	mustFail(t, w.link(rel.ID, "blocks", a.ID, "zhao"), "反向前置", "绕成圈")
	must(t, w.do(rel.ID, "zhao", "ready", ""), "就绪")
	mustFail(t, w.do(rel.ID, "zhao", "start", ""), "前置未完成", "需求甲、需求乙")
	must(t, w.do(b.ID, "zhao", "cancel", ""), "取消乙")
	if len(b.Relations) != 0 {
		t.Fatal("取消后乙的前置关联应自动解除")
	}
	mustFail(t, w.do(rel.ID, "zhao", "start", ""), "仍剩甲", "需求甲")
	if strings.Contains(w.lastErr(rel.ID, "zhao", "start"), "需求乙") {
		t.Fatal("乙已不再是前置")
	}
	must(t, w.do(a.ID, "wang", "start_design", ""), "")
	w.artifact(a.ID, "wang", "prd")
	must(t, w.do(a.ID, "wang", "design_done", ""), "")
	w.artifact(a.ID, "li", "pr")
	must(t, w.do(a.ID, "li", "dev_done", ""), "")
	w.artifact(a.ID, "zhang", "test_report")
	must(t, w.do(a.ID, "zhang", "test_pass", ""), "")
	w.artifact(a.ID, "zhao", "release_note")
	must(t, w.do(a.ID, "zhao", "release_done", ""), "")
	must(t, w.do(a.ID, "wang", "accept", ""), "")
	must(t, w.do(rel.ID, "zhao", "start", ""), "甲上线后开始发布")
	wantState(t, rel, "preparing")
}

func (w *world) lastErr(taskID, actorID, name string) string {
	for _, a := range Available(w.ctx(taskID, actorID), w.actors[actorID], Payload{}) {
		if a.Transition.Name == name {
			return strings.Join(RenderReasons(i18n.Default, a.Reasons), "；")
		}
	}
	return ""
}

// 场景 F：越权与授权
func TestScenarioF_Permissions(t *testing.T) {
	w := newWorld(t)
	task := w.create("generic", "整理客户名单", "wang", nil)
	must(t, w.assign(task.ID, "wang", "li"), "")
	must(t, w.do(task.ID, "wang", "ready", ""), "")
	mustFail(t, w.do(task.ID, "zhang-agent", "start", ""), "别人的 Agent 开始", "任务负责人")
	mustFail(t, w.do(task.ID, "li", "cancel", ""), "小李取消", "创建者")
	must(t, w.do(task.ID, "zhao", "cancel", ""), "管理员取消")
	mustFail(t, w.do(task.ID, "wang", "ready", ""), "终止后就绪", "没有「ready」这一步")

	self := w.create("generic", "写月度总结", "wang", nil)
	must(t, w.assign(self.ID, "wang", "wang"), "")
	must(t, w.do(self.ID, "wang", "ready", ""), "")
	must(t, w.do(self.ID, "wang-agent", "start", ""), "小王的 Agent 替他开始")
	w.artifact(self.ID, "wang-agent", "result")
	must(t, w.do(self.ID, "wang-agent", "submit", ""), "")
	mustFail(t, w.do(self.ID, "wang-agent", "accept", ""), "Agent 无验收授权", "验收")
	must(t, w.do(self.ID, "wang", "accept", ""), "创建者自验收")
	wantState(t, self, "done")

	// human_only 任务 Agent 不能开始
	ho := w.create("generic", "面谈候选人", "wang", nil)
	ho.HumanOnly = true
	must(t, w.assign(ho.ID, "wang", "li"), "")
	must(t, w.do(ho.ID, "wang", "ready", ""), "")
	must(t, w.do(ho.ID, "li-agent", "start", ""), "Agent 可以推进状态")
	if w.activeRun(ho.ID) != nil {
		t.Fatal("只能由人做的任务不应为 Agent 开始执行记录")
	}
	mustFail(t, w.begin(ho.ID, "li-agent"), "Agent 显式开始", "只能由人来做")
	must(t, w.begin(ho.ID, "li"), "小李本人开始")
}

func TestValidateRejectsBrokenWorkflow(t *testing.T) {
	tt := &TaskType{Name: "x", Title: i18n.T("x", "x"), Workflow: Workflow{Initial: "nope", States: []State{
		{Name: "a", Title: i18n.T("a", "a"), Label: LabelActive},
		{Name: "b", Title: i18n.T("b", "b"), Label: "weird"},
	}, Transitions: []Transition{{Name: "go", Title: i18n.T("go", "go"), From: []string{"a"}, To: "zzz", By: []string{"nobody"}, Requires: []string{"magic"}, AssignTo: "participant:ghost"}}}}
	errs := Validate(tt)
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	for _, want := range []string{"初始状态", "状态类型", "已完成状态", "终点", "触发者规则", "前提条件", "参与角色位置"} {
		if !strings.Contains(joined, want) {
			t.Errorf("校验应报告「%s」，实际：\n%s", want, joined)
		}
	}
}
