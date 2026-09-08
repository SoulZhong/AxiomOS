package mcp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// next_actions（ADR 0025 第 4 条）：带编号的可执行动作清单来自真实状态，
// 每类来源各出一条、顺序按紧急度、同样的状态排出同样的清单，而且完全只读。

type actionsResult struct {
	Actions []nextAction `json:"actions"`
	Note    string       `json:"note"`
}

func callNextActions(t *testing.T, a *app.App, sess *app.Session, args map[string]any) actionsResult {
	t.Helper()
	text, isErr := callMCP(t, a, sess, "next_actions", args)
	if isErr {
		t.Fatalf("next_actions 报错：%s", text)
	}
	var out actionsResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("next_actions 返回的不是 JSON：%v\n%s", err, text)
	}
	return out
}

// find 按 task_id 参数找一条动作，返回它的下标。
func (r actionsResult) find(ref string) (int, *nextAction) {
	for i := range r.Actions {
		if r.Actions[i].Arguments["task_id"] == ref {
			return i, &r.Actions[i]
		}
	}
	return -1, nil
}

func (r actionsResult) byTool(tool string) (int, *nextAction) {
	for i := range r.Actions {
		if r.Actions[i].Tool == tool {
			return i, &r.Actions[i]
		}
	}
	return -1, nil
}

// nextActionsWorld 是一份覆盖每类来源的种子数据。
type nextActionsWorld struct {
	*world
	agent                            *app.Session
	overdue, active, running, review *domain.Task
	backlog                          *domain.Task
}

func seedNextActions(t *testing.T) *nextActionsWorld {
	t.Helper()
	w := newWorld(t)
	a, ctx := w.a, w.ctx
	_, ag := w.agent(t, w.yi, "清单 Agent", map[domain.Grant]domain.GrantMode{
		domain.GrantExecute:      domain.GrantDirect,
		domain.GrantClaimBacklog: domain.GrantDirect,
		domain.GrantReview:       domain.GrantDirect,
		domain.GrantComment:      domain.GrantDirect,
		domain.GrantCreateTask:   domain.GrantDirect,
		domain.GrantAssign:       domain.GrantDirect,
		domain.GrantCreateGoal:   domain.GrantWithApproval,
	}, 5)

	mk := func(by *app.Session, in app.CreateTaskInput) *domain.Task {
		t.Helper()
		in.Ready = true
		tk, err := a.CreateTask(ctx, by, in)
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	step := func(by *app.Session, id, name string, p app.TransitionPayload) {
		t.Helper()
		if _, err := a.Transition(ctx, by, id, name, p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	yesterday := time.Now().AddDate(0, 0, -3)
	nw := &nextActionsWorld{world: w, agent: ag}
	// 1. 逾期、还没开始的在手任务
	nw.overdue = mk(w.jia, app.CreateTaskInput{Title: "逾期任务", AssigneeID: ag.Actor.ID, PlannedEnd: &yesterday})
	// 2. 进行中、但执行记录已经结束（问过一轮、由创建者答复恢复，恢复的人不是负责人所以没开新的执行记录）
	nw.active = mk(w.jia, app.CreateTaskInput{Title: "进行中任务", AssigneeID: ag.Actor.ID})
	step(ag, nw.active.ID, "start", app.TransitionPayload{})
	step(ag, nw.active.ID, "ask_for_input", app.TransitionPayload{Comment: "用哪个仓库？"})
	step(w.jia, nw.active.ID, "resume", app.TransitionPayload{Comment: "用主仓库"})
	// 3. 进行中、执行记录已开（负责人自己 start 会顺带开一段）：交付物没齐，只剩「提问等待」可走
	nw.running = mk(w.jia, app.CreateTaskInput{Title: "已在执行的任务", AssigneeID: ag.Actor.ID})
	step(ag, nw.running.ID, "start", app.TransitionPayload{})
	// 4. 等我验收：Agent 创建（因此是验收人），乙执行并提交
	nw.review = mk(ag, app.CreateTaskInput{Title: "等我验收的任务", AssigneeID: w.yi.MemberID})
	step(w.yi, nw.review.ID, "start", app.TransitionPayload{})
	if _, err := a.AddArtifact(ctx, w.yi, nw.review.ID, domain.Artifact{Type: "result", Title: "结果"}); err != nil {
		t.Fatal(err)
	}
	step(w.yi, nw.review.ID, "submit", app.TransitionPayload{})
	// 5. 待领取
	nw.backlog = mk(w.jia, app.CreateTaskInput{Title: "待领取任务"})
	// 6. 我提交的待确认操作（「创建目标」是需要人确认）
	if _, err := a.CreateGoal(ctx, ag, app.CreateGoalInput{Title: "要确认的目标"}); err == nil {
		t.Fatal("创建目标应转成待确认操作")
	} else if _, ok := app.AsProposalPending(err); !ok {
		t.Fatalf("应是待确认操作，实际 %v", err)
	}
	return nw
}

func tref(tk *domain.Task) string { return taskRef(tk.Number) }

func TestNextActionsCoversEverySourceInOrder(t *testing.T) {
	nw := seedNextActions(t)
	res := callNextActions(t, nw.a, nw.agent, nil)
	if len(res.Actions) == 0 {
		t.Fatal("清单不该为空")
	}
	if len(res.Actions) > maxNextActions {
		t.Fatalf("清单最多 %d 条，实际 %d 条", maxNextActions, len(res.Actions))
	}
	for i, act := range res.Actions {
		if act.N != i+1 {
			t.Fatalf("第 %d 条的编号应是 %d，实际 %d", i+1, i+1, act.N)
		}
		if act.Label == "" || act.Tool == "" {
			t.Fatalf("第 %d 条缺一句话或工具名：%+v", act.N, act)
		}
	}

	// 逾期的排第一，且带「已逾期」
	iOverdue, aOverdue := res.find(tref(nw.overdue))
	if iOverdue != 0 {
		t.Fatalf("逾期任务应排第一，实际第 %d 位：%+v", iOverdue+1, res.Actions)
	}
	if aOverdue.Tool != "transition_task" || aOverdue.Arguments["name"] != "start" {
		t.Fatalf("逾期任务的动作应是 transition_task/start，实际 %+v", aOverdue)
	}
	if !strings.Contains(aOverdue.Label, "已逾期") {
		t.Fatalf("逾期任务的一句话应说明已逾期：%s", aOverdue.Label)
	}

	// 进行中、还没开执行记录 → begin_task
	iActive, aActive := res.find(tref(nw.active))
	if aActive == nil || aActive.Tool != "begin_task" {
		t.Fatalf("进行中且没有执行记录时应建议 begin_task，实际 %+v", aActive)
	}
	// 执行记录已开 → 现在只剩提问等待
	iRunning, aRunning := res.find(tref(nw.running))
	if aRunning == nil || aRunning.Tool != "transition_task" || aRunning.Arguments["name"] != "ask_for_input" {
		t.Fatalf("执行记录已开且交付物还没附时应建议提问等待，实际 %+v", aRunning)
	}
	if _, ok := aRunning.Arguments["comment"]; !ok {
		t.Fatal("要写说明的一步应把 comment 参数一起带上")
	}
	// 等我验收 → 验收通过那一步
	iReview, aReview := res.find(tref(nw.review))
	if aReview == nil || aReview.Tool != "transition_task" || aReview.Arguments["name"] != "accept" {
		t.Fatalf("等我验收的任务应建议走 accept，实际 %+v", aReview)
	}
	// 待领取 → claim_task
	iBacklog, aBacklog := res.find(tref(nw.backlog))
	if aBacklog == nil || aBacklog.Tool != "claim_task" {
		t.Fatalf("待领取任务应建议 claim_task，实际 %+v", aBacklog)
	}
	// 待确认操作排最后
	iProp, aProp := res.byTool("list_my_proposals")
	if aProp == nil || iProp != len(res.Actions)-1 {
		t.Fatalf("待确认操作应排最后，实际第 %d 位：%+v", iProp+1, res.Actions)
	}

	// 顺序：逾期 → 进行中 → 等我验收 → 可领取 → 待确认操作
	order := []int{iOverdue, iActive, iRunning, iReview, iBacklog, iProp}
	for k := 1; k < len(order); k++ {
		if order[k] <= order[k-1] {
			t.Fatalf("顺序不对（逾期 → 进行中 → 等我验收 → 可领取 → 待确认操作）：%v", order)
		}
	}
	if res.Note == "" || !strings.Contains(res.Note, "编号") {
		t.Fatalf("结尾应有一句「念给人听、让人报编号」：%s", res.Note)
	}

	// 附上交付物之后，同一个任务的建议变成提交那一步（要交付物的一步排最前）
	if _, err := nw.a.AddArtifact(nw.ctx, nw.agent, nw.running.ID, domain.Artifact{Type: "result", Title: "结果"}); err != nil {
		t.Fatal(err)
	}
	res2 := callNextActions(t, nw.a, nw.agent, nil)
	_, aRunning2 := res2.find(tref(nw.running))
	if aRunning2 == nil || aRunning2.Arguments["name"] != "submit" {
		t.Fatalf("交付物齐了应建议 submit，实际 %+v", aRunning2)
	}
}

// 同样的状态排出同样的清单。
func TestNextActionsIsDeterministic(t *testing.T) {
	nw := seedNextActions(t)
	first := callNextActions(t, nw.a, nw.agent, nil)
	for i := 0; i < 3; i++ {
		again := callNextActions(t, nw.a, nw.agent, nil)
		x, _ := json.Marshal(first)
		y, _ := json.Marshal(again)
		if string(x) != string(y) {
			t.Fatalf("两次清单不一样：\n%s\n%s", x, y)
		}
	}
}

// 给了 task 就正好是 get_workflow 说现在能走的每一步。
func TestNextActionsForOneTaskMatchesWorkflow(t *testing.T) {
	nw := seedNextActions(t)
	res := callNextActions(t, nw.a, nw.agent, map[string]any{"task": tref(nw.running)})
	w, err := nw.a.Workflow(nw.ctx, nw.agent, nw.running.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, tv := range w.Transitions {
		if tv.Available {
			want[tv.Name] = true
		}
	}
	got := map[string]bool{}
	for _, act := range res.Actions {
		if act.Tool != "transition_task" {
			t.Fatalf("给了 task 时只该出现流程步骤（外加领取 / 开始执行），实际 %+v", act)
		}
		if act.Arguments["task_id"] != tref(nw.running) {
			t.Fatalf("参数里的任务不对：%+v", act)
		}
		got[act.Arguments["name"].(string)] = true
	}
	if len(want) == 0 {
		t.Fatal("这个任务现在应该有可走的步骤")
	}
	for n := range want {
		if !got[n] {
			t.Fatalf("缺少可走的步骤 %s：%+v", n, res.Actions)
		}
	}
	for n := range got {
		if !want[n] {
			t.Fatalf("多出了走不了的步骤 %s", n)
		}
	}
}

// 只读：调多少次都不写动态。
func TestNextActionsWritesNoEvents(t *testing.T) {
	nw := seedNextActions(t)
	last := func() int64 {
		rows, err := nw.a.Events(nw.ctx, nw.jia, "", 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			return 0
		}
		return rows[0].ID
	}
	before := last()
	callNextActions(t, nw.a, nw.agent, nil)
	callNextActions(t, nw.a, nw.agent, map[string]any{"task": tref(nw.running)})
	if after := last(); after != before {
		t.Fatalf("next_actions 不该产生动态：调用前最后一条 %d，调用后 %d", before, after)
	}
}

// 拒绝理由和别处是同一句话（六类之一：范围不可见）。
func TestNextActionsRejectionSentence(t *testing.T) {
	nw := seedNextActions(t)
	hidden := nw.task(t, nw.jia, "别人团队的任务", nw.bing.MemberID)
	text, isErr := callMCP(t, nw.a, nw.agent, "next_actions", map[string]any{"task": hidden.ID})
	want := i18n.Tr(nw.agent.Loc(), "err.task_hidden")
	if !isErr || text != want {
		t.Fatalf("应报「%s」，实际 isError=%v「%s」", want, isErr, text)
	}
}

// 没有「领取任务」授权的 Agent，清单里不出现待领取任务。
func TestNextActionsHidesBacklogWithoutClaimGrant(t *testing.T) {
	nw := seedNextActions(t)
	_, noClaim := nw.world.agent(t, nw.yi, "不能领的 Agent", map[domain.Grant]domain.GrantMode{
		domain.GrantExecute: domain.GrantDirect,
	}, 1)
	res := callNextActions(t, nw.a, noClaim, nil)
	if _, act := res.find(tref(nw.backlog)); act != nil {
		t.Fatalf("没有领取授权时不该出现待领取任务：%+v", act)
	}
}
