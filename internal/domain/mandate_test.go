package domain

import (
	"testing"
)

// 委托之内：需要人确认的授权不再拦白名单动作；验收、取消仍拦；委托只对那一个任务有效。
func TestMandateCoversWhitelistOnly(t *testing.T) {
	w := newWorld(t)
	task := w.create("generic", "写文档", "wang", nil)
	must(t, w.do(task.ID, "wang", "ready", ""), "ready")
	must(t, w.assign(task.ID, "wang", "li-agent"), "assign")
	other := w.create("generic", "另一个", "wang", nil)

	agent := w.actors["li-agent"]
	agent.Grants[GrantExecute] = GrantWithApproval
	agent.Grants[GrantComment] = GrantWithApproval
	agent.Grants[GrantLink] = GrantWithApproval

	// 没有委托：开始与推进都要人确认
	if asNeedsApproval(w.do(task.ID, "li-agent", "start", "")) == nil {
		t.Fatal("没有委托时推进应需要人确认")
	}

	agent.Mandate = &MandateScope{ID: "mdt_1", TaskID: task.ID}
	// 有委托：推进直接生效，并且为自己开了执行记录
	must(t, w.do(task.ID, "li-agent", "start", ""), "start in mandate")
	wantState(t, w.tasks[task.ID], "in_progress")
	if w.activeRun(task.ID) == nil {
		t.Fatal("委托内进入进行中应开执行记录")
	}
	// 评论、关联直接生效
	if _, err := AddComment(w.ctx(task.ID, "li-agent"), agent, "进展", true); err != nil {
		t.Fatalf("委托内写进展不应被拦：%v", err)
	}
	must(t, w.link(task.ID, "relates_to", other.ID, "li-agent"), "link in mandate")
	// 委托对另一个任务无效
	if asNeedsApproval(w.link(other.ID, "relates_to", task.ID, "li-agent")) == nil {
		t.Fatal("委托只对那一个任务有效")
	}
	// 提交是 assignee 的执行步，直接生效；验收不是
	w.artifact(task.ID, "li-agent", "result")
	must(t, w.do(task.ID, "li-agent", "submit", ""), "submit in mandate")
	agent.Grants[GrantReview] = GrantWithApproval
	if err := w.do(task.ID, "li-agent", "accept", ""); err == nil {
		t.Fatal("验收步骤不在委托白名单里，且 Agent 不是验收人")
	}
	// 取消是失败终态，不在名单里（且 by 不含 assignee）
	for _, a := range Available(w.ctx(task.ID, "li-agent"), agent, Payload{}) {
		if a.Transition.Name == "cancel" && a.Available && !a.NeedsApproval {
			t.Fatal("取消不该被委托放行")
		}
	}
}

// 自动验收：标了 auto 且要求都满足时由系统执行者走验收步；没标的拒绝；系统执行者不开执行记录。
func TestAutoAccept(t *testing.T) {
	w := newWorld(t)
	task := w.create("generic", "写文档", "wang", nil)
	must(t, w.do(task.ID, "wang", "ready", ""), "ready")
	must(t, w.assign(task.ID, "wang", "li"), "assign")
	must(t, w.do(task.ID, "li", "start", ""), "start")
	w.artifact(task.ID, "li", "result")
	must(t, w.do(task.ID, "li", "submit", ""), "submit")

	if _, err := AutoAccept(w.ctx(task.ID, "wang")); err == nil {
		t.Fatal("没选自动验收的任务不能自动验收")
	}
	w.tasks[task.ID].AcceptanceMode = AcceptanceAuto
	o, err := AutoAccept(w.ctx(task.ID, "wang"))
	if err != nil {
		t.Fatalf("自动验收失败：%v", err)
	}
	must(t, w.apply(o, nil), "persist")
	if lbl := w.ctx(task.ID, "wang").labelOf(w.tasks[task.ID]); lbl != LabelTerminalSuccess {
		t.Fatalf("自动验收后应是成功终态，实际 %s", w.tasks[task.ID].State)
	}
	if o.Events[0].Type != "TaskAutoAccepted" || o.Events[0].ActorID != SystemActorID {
		t.Fatalf("第一条动态应是系统的 TaskAutoAccepted，实际 %+v", o.Events[0])
	}
	if o.OpenedRun != nil {
		t.Fatal("系统执行者不开执行记录")
	}
}

// 撤回：状态回到指定状态，进行中的执行记录被结束，只追加动态。
func TestRevert(t *testing.T) {
	w := newWorld(t)
	task := w.create("generic", "写文档", "wang", nil)
	must(t, w.do(task.ID, "wang", "ready", ""), "ready")
	must(t, w.assign(task.ID, "wang", "li"), "assign")
	must(t, w.do(task.ID, "li", "start", ""), "start")
	before := len(w.events)
	o, err := Revert(w.ctx(task.ID, "wang"), w.actors["wang"], "todo", "做错了")
	if err != nil {
		t.Fatalf("撤回失败：%v", err)
	}
	must(t, w.apply(o, nil), "persist")
	wantState(t, w.tasks[task.ID], "todo")
	if o.ClosedRun == nil || o.ClosedRun.Outcome != RunCancelled {
		t.Fatal("撤回离开进行中应结束执行记录")
	}
	if len(w.events) <= before || w.events[len(w.events)-1].Type != "TaskReverted" {
		t.Fatal("撤回应只追加一条 TaskReverted")
	}
	if _, err := Revert(w.ctx(task.ID, "wang"), w.actors["wang"], "nowhere", ""); err == nil {
		t.Fatal("不存在的状态不能撤回到")
	}
}
