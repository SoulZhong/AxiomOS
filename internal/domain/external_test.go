package domain

import (
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/i18n"
)

func typeByName(name string) *TaskType {
	for _, tt := range BuiltinTaskTypes() {
		if tt.Name == name {
			return tt
		}
	}
	return nil
}

func gitTrigger(event string) ExternalTrigger {
	return ExternalTrigger{Source: SourceGit, Event: event, Provider: "github", UserName: "alice", Ref: "PR #12", URL: "https://github.com/a/b/pull/12"}
}

func eventOf(o *Outcome, typ string) *Event {
	for i := range o.Events {
		if o.Events[i].Type == typ {
			return &o.Events[i]
		}
	}
	return nil
}

// 内置需求流程：PR 合并把「开发中」推到「测试中」，执行者是外部事件，不给谁开执行记录。
func TestExternalEventAppliesTransition(t *testing.T) {
	tt := typeByName("requirement")
	task := &Task{ID: "T1", TypeName: "requirement", State: "developing", AssigneeID: "m1",
		Artifacts: []Artifact{{Type: "pr", Title: "PR #12"}}, Participants: map[string]string{"tester": "m2"}}
	c := &Context{Task: task, Type: tt}
	o := ApplyExternal(c, gitTrigger(EventPRMerged))
	if task.State != "testing" {
		t.Fatalf("PR 合并应把任务推到测试中，实际 %s", task.State)
	}
	applied := eventOf(o, "ExternalEventApplied")
	if applied == nil || applied.Data["transition"] != "dev_done" || applied.Data["external_user_name"] != "alice" {
		t.Fatalf("应写一条外部事件动态 %+v", o.Events)
	}
	if eventOf(o, "TaskTransitioned") == nil {
		t.Fatal("状态变化本身仍要有 TaskTransitioned（统计与燃尽图靠它）")
	}
	if o.OpenedRun != nil {
		t.Fatal("外部事件不是执行者，不该开执行记录")
	}
	if task.AssigneeID != "m2" {
		t.Fatalf("负责人应按步骤切到测试参与角色，实际 %s", task.AssigneeID)
	}
}

// 前提不满足：缺「代码 PR」交付物时不迁移，只写一条说明动态。
func TestExternalEventIgnoredWhenPreconditionFails(t *testing.T) {
	tt := typeByName("requirement")
	task := &Task{ID: "T1", TypeName: "requirement", State: "developing", AssigneeID: "m1"}
	c := &Context{Task: task, Type: tt}
	o := ApplyExternal(c, gitTrigger(EventPRMerged))
	if task.State != "developing" {
		t.Fatalf("前提不满足时状态不该变，实际 %s", task.State)
	}
	ig := eventOf(o, "ExternalEventIgnored")
	if ig == nil {
		t.Fatalf("应写一条被忽略的动态 %+v", o.Events)
	}
	reason, _ := ig.Data["reason"].(i18n.Text)
	if !strings.Contains(reason.In(i18n.ZhCN), "缺少交付物") {
		t.Fatalf("原因要是一句完整的中文，实际 %v", ig.Data["reason"])
	}
	if len(o.Events) != 1 {
		t.Fatalf("除说明动态外不该有别的副作用 %+v", o.Events)
	}
}

// 当前状态没有这件事的步骤：一句说明，什么都不改。
func TestExternalEventNoTransitionInState(t *testing.T) {
	tt := typeByName("requirement")
	task := &Task{ID: "T1", TypeName: "requirement", State: "draft"}
	c := &Context{Task: task, Type: tt}
	o := ApplyExternal(c, gitTrigger(EventPRMerged))
	ig := eventOf(o, "ExternalEventIgnored")
	if ig == nil || task.State != "draft" {
		t.Fatalf("应只写说明动态 %+v", o.Events)
	}
	reason, _ := ig.Data["reason"].(i18n.Text)
	if !strings.Contains(reason.In(i18n.ZhCN), "没有由") {
		t.Fatalf("应说清当前状态没有对应步骤，实际 %q", reason.In(i18n.ZhCN))
	}
	// 已结束的任务同样不动
	task.State = "done"
	o = ApplyExternal(c, gitTrigger(EventPRMerged))
	if eventOf(o, "ExternalEventIgnored") == nil || task.State != "done" {
		t.Fatalf("已结束的任务不该被外部事件改动 %+v", o.Events)
	}
}

// 检查失败把需求推进「已阻塞」；Bug 流程没有阻塞状态，同一件事什么都不做。
func TestExternalCIFailedBlocks(t *testing.T) {
	req := typeByName("requirement")
	task := &Task{ID: "T1", TypeName: "requirement", State: "developing", AssigneeID: "m1"}
	c := &Context{Task: task, Type: req}
	if o := ApplyExternal(c, gitTrigger(EventCIFailed)); eventOf(o, "ExternalEventApplied") == nil || task.State != "blocked" {
		t.Fatalf("检查失败应把需求标记阻塞，实际 %s %+v", task.State, o.Events)
	}
	bug := typeByName("bug")
	bt := &Task{ID: "T2", TypeName: "bug", State: "fixing", AssigneeID: "m1"}
	bc := &Context{Task: bt, Type: bug}
	if o := ApplyExternal(bc, gitTrigger(EventCIFailed)); eventOf(o, "ExternalEventIgnored") == nil || bt.State != "fixing" {
		t.Fatalf("Bug 流程没有阻塞状态，不该被推走 %s", bt.State)
	}
}

// Bug 流程的内置触发：PR 打开开始修复，PR 合并进入待验证。
func TestExternalEventBugDefaults(t *testing.T) {
	tt := typeByName("bug")
	task := &Task{ID: "T1", TypeName: "bug", State: "confirmed", AssigneeID: "m1", Participants: map[string]string{"tester": "m2"}}
	c := &Context{Task: task, Type: tt}
	if o := ApplyExternal(c, gitTrigger(EventPROpened)); eventOf(o, "ExternalEventApplied") == nil || task.State != "fixing" {
		t.Fatalf("PR 打开应开始修复，实际 %s %+v", task.State, o.Events)
	}
	task.Artifacts = append(task.Artifacts, Artifact{Type: "pr"})
	if o := ApplyExternal(c, gitTrigger(EventPRMerged)); eventOf(o, "ExternalEventApplied") == nil || task.State != "fixed" {
		t.Fatalf("PR 合并应进入待验证，实际 %s %+v", task.State, o.Events)
	}
}

// 外部操作者绑定了成员时，动态挂在那个人名下。
func TestExternalActorBoundToMember(t *testing.T) {
	tt := typeByName("bug")
	task := &Task{ID: "T1", TypeName: "bug", State: "confirmed", AssigneeID: "m1"}
	c := &Context{Task: task, Type: tt}
	tg := gitTrigger(EventPROpened)
	tg.MemberID = "mem_1"
	o := ApplyExternal(c, tg)
	if e := eventOf(o, "ExternalEventApplied"); e == nil || e.ActorID != "mem_1" {
		t.Fatalf("绑定了成员时动态应挂在他名下 %+v", o.Events)
	}
	if o.OpenedRun != nil {
		t.Fatal("即使绑定到负责人本人也不开执行记录：外部事件不是执行者")
	}
}

// 流程定义校验：触发来源与事件名必须是内核认识的。
func TestValidateTrigger(t *testing.T) {
	base := func() *TaskType {
		return &TaskType{Name: "x", Title: tt2("测试|Test"), Workflow: Workflow{Initial: "todo",
			States: []State{st("todo", "待办|To do", LabelPending), st("doing", "进行中|Doing", LabelActive), st("done", "完成|Done", LabelTerminalSuccess)},
			Transitions: []Transition{
				tr("start", "开始|Start", []string{"todo"}, "doing", []string{"assignee"}),
				tr("finish", "完成|Finish", []string{"doing"}, "done", []string{"assignee"}),
			}}}
	}
	ok := base()
	ok.Workflow.Transitions[1] = ok.Workflow.Transitions[1].onGit(EventPRMerged)
	if errs := Validate(ok); len(errs) > 0 {
		t.Fatalf("认识的事件应通过校验 %v", RenderErrors(i18n.ZhCN, errs))
	}
	bad := base()
	bad.Workflow.Transitions[1].TriggeredBy = &Trigger{Source: "git", Event: "pr_reviewed"}
	errs := Validate(bad)
	if len(errs) != 1 || !strings.Contains(RenderErrors(i18n.ZhCN, errs)[0], "pr_reviewed") {
		t.Fatalf("不认识的事件名应被拒绝 %v", RenderErrors(i18n.ZhCN, errs))
	}
	badSrc := base()
	badSrc.Workflow.Transitions[1].TriggeredBy = &Trigger{Source: "jira", Event: EventPRMerged}
	if errs := Validate(badSrc); len(errs) != 1 {
		t.Fatalf("不认识的来源应被拒绝 %v", RenderErrors(i18n.ZhCN, errs))
	}
}

// 内置需求与 Bug 的触发声明就是文档写的那几条。
func TestBuiltinTriggers(t *testing.T) {
	want := map[string]map[string]string{
		"requirement": {"design_done": EventPROpened, "dev_done": EventPRMerged, "block": EventCIFailed},
		"bug":         {"start_fix": EventPROpened, "fixed": EventPRMerged},
	}
	for name, m := range want {
		tt := typeByName(name)
		got := map[string]string{}
		for _, tr := range tt.Workflow.Transitions {
			if tr.TriggeredBy != nil {
				if tr.TriggeredBy.Source != SourceGit {
					t.Fatalf("%s.%s 的触发来源应是 git", name, tr.Name)
				}
				got[tr.Name] = tr.TriggeredBy.Event
			}
		}
		if len(got) != len(m) {
			t.Fatalf("%s 的触发声明不符：期望 %v，实际 %v", name, m, got)
		}
		for k, v := range m {
			if got[k] != v {
				t.Fatalf("%s.%s 应由 %s 触发，实际 %s", name, k, v, got[k])
			}
		}
	}
	// 通用任务与发布不带触发声明
	for _, name := range []string{"generic", "release"} {
		for _, tr := range typeByName(name).Workflow.Transitions {
			if tr.TriggeredBy != nil {
				t.Fatalf("%s.%s 不该带触发声明", name, tr.Name)
			}
		}
	}
	if errs := Validate(typeByName("requirement")); len(errs) > 0 {
		t.Fatalf("内置需求应通过校验 %v", RenderErrors(i18n.ZhCN, errs))
	}
}
