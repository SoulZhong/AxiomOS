package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// asNeedsApproval 取出内核的"需要人确认"信号。
func asNeedsApproval(err error) *ErrNeedsApproval {
	var na *ErrNeedsApproval
	if errors.As(err, &na) {
		return na
	}
	return nil
}

func TestGrantModeOf(t *testing.T) {
	w := newWorld(t)
	person := w.actors["li"]
	if GrantModeOf(person, GrantManageWorkflows) != GrantDirect {
		t.Fatal("成员的任何授权都应是直接生效")
	}
	if NeedsApproval(person, GrantManageWorkflows) {
		t.Fatal("成员永远不需要人确认")
	}
	agent := w.actors["li-agent"]
	if GrantModeOf(agent, GrantExecute) != GrantDirect {
		t.Fatalf("直接生效的授权应返回 direct，实际 %q", GrantModeOf(agent, GrantExecute))
	}
	if GrantModeOf(agent, GrantAssign) != "" {
		t.Fatalf("没有的授权应返回空，实际 %q", GrantModeOf(agent, GrantAssign))
	}
	agent.Grants[GrantAssign] = GrantWithApproval
	if !NeedsApproval(agent, GrantAssign) {
		t.Fatal("需要人确认的授权应被识别")
	}
	if !agent.HasGrant(GrantAssign) {
		t.Fatal("需要人确认也是持有这项授权")
	}
}

// 需要人确认的 Agent 不能直接推进流程；直接生效的可以；人不受影响。
func TestWithApprovalAgentCannotTransitionDirectly(t *testing.T) {
	w := newWorld(t)
	task := w.create("generic", "写文档", "wang", nil)
	if err := w.do(task.ID, "wang", "ready", ""); err != nil {
		t.Fatal(err)
	}
	if err := w.assign(task.ID, "wang", "li"); err != nil {
		t.Fatal(err)
	}

	agent := w.actors["li-agent"]
	agent.Grants[GrantExecute] = GrantWithApproval

	// Available 仍然提供这一步，只是标上"需要人确认"，不是拒绝理由
	found := false
	for _, av := range Available(w.ctx(task.ID, "li-agent"), agent, Payload{}) {
		if av.Transition.Name != "start" {
			continue
		}
		found = true
		if !av.Available {
			t.Fatalf("需要人确认不应让这一步变成不可用，理由 %v", RenderReasons(i18n.Default, av.Reasons))
		}
		if !av.NeedsApproval {
			t.Fatal("这一步应标记为需要人确认")
		}
	}
	if !found {
		t.Fatal("没有找到 start 这一步")
	}

	err := w.do(task.ID, "li-agent", "start", "")
	na := asNeedsApproval(err)
	if na == nil {
		t.Fatalf("需要人确认的 Agent 直接推进应返回信号，实际 %v", err)
	}
	if na.Grant != GrantExecute {
		t.Fatalf("信号应带上「执行任务」授权，实际 %s", na.Grant)
	}
	if w.tasks[task.ID].State != "todo" {
		t.Fatalf("任务状态不应改变，实际 %s", w.tasks[task.ID].State)
	}
	if len(w.runs) != 0 {
		t.Fatal("不应开启执行记录")
	}
	if s := na.Render(i18n.Default); !strings.Contains(s, "需要人确认") {
		t.Fatalf("理由应是完整中文句子，实际 %q", s)
	}
	if s := na.Render(i18n.EnUS); !strings.Contains(s, "confirm") {
		t.Fatalf("英文理由缺失，实际 %q", s)
	}

	// 改回直接生效就能走
	agent.Grants[GrantExecute] = GrantDirect
	if err := w.do(task.ID, "li-agent", "start", ""); err != nil {
		t.Fatalf("直接生效的 Agent 应能推进，实际 %v", err)
	}
	if w.tasks[task.ID].State != "in_progress" {
		t.Fatalf("应进入 in_progress，实际 %s", w.tasks[task.ID].State)
	}
}

// 领取、开始执行、指派、评论、关联都受"需要人确认"约束；人不受影响。
func TestWithApprovalCoversOtherCommands(t *testing.T) {
	w := newWorld(t)
	agent := w.actors["li-agent"]
	for _, g := range []Grant{GrantClaimBacklog, GrantExecute, GrantComment, GrantAssign, GrantLink} {
		agent.Grants[g] = GrantWithApproval
	}

	backlog := w.create("generic", "待领取的活", "wang", nil)
	if err := w.do(backlog.ID, "wang", "ready", ""); err != nil {
		t.Fatal(err)
	}
	if na := asNeedsApproval(w.claim(backlog.ID, "li-agent")); na == nil || na.Grant != GrantClaimBacklog {
		t.Fatal("领取应要求人确认")
	}
	if backlog.AssigneeID != "" {
		t.Fatal("领取不应生效")
	}

	mine := w.create("generic", "我的活", "wang", nil)
	if err := w.do(mine.ID, "wang", "ready", ""); err != nil {
		t.Fatal(err)
	}
	if err := w.assign(mine.ID, "wang", "li"); err != nil {
		t.Fatal(err)
	}
	agent.Grants[GrantExecute] = GrantDirect
	if err := w.do(mine.ID, "li-agent", "start", ""); err != nil {
		t.Fatal(err)
	}
	// 已经有执行记录，再 Begin 会被普通规则拒绝；换一个任务测 Begin
	other := w.create("generic", "另一件活", "wang", nil)
	if err := w.do(other.ID, "wang", "ready", ""); err != nil {
		t.Fatal(err)
	}
	if err := w.assign(other.ID, "wang", "li"); err != nil {
		t.Fatal(err)
	}
	// 让 other 停在"进行中"但没有执行记录：负责人开始 → 提问等待 → 创建者答复恢复
	if err := w.do(other.ID, "li", "start", ""); err != nil {
		t.Fatal(err)
	}
	if err := w.do(other.ID, "li", "ask_for_input", "这个怎么算？"); err != nil {
		t.Fatal(err)
	}
	if err := w.do(other.ID, "wang", "resume", "按上季度口径。"); err != nil {
		t.Fatal(err)
	}
	agent.Grants[GrantExecute] = GrantWithApproval
	agent.MaxConcurrent = 3
	if na := asNeedsApproval(w.begin(other.ID, "li-agent")); na == nil || na.Grant != GrantExecute {
		t.Fatal("开始执行应要求人确认")
	}

	if na := asNeedsApproval(w.assign(other.ID, "li-agent", "zhang")); na == nil || na.Grant != GrantAssign {
		t.Fatal("指派应要求人确认")
	}
	if other.AssigneeID != "li" {
		t.Fatalf("指派不应生效，实际负责人 %s", other.AssigneeID)
	}

	before := len(other.Comments)
	if _, err := AddComment(w.ctx(other.ID, "li-agent"), agent, "一条评论", false); asNeedsApproval(err) == nil {
		t.Fatalf("评论应要求人确认，实际 %v", err)
	}
	if len(other.Comments) != before {
		t.Fatal("评论不应写入")
	}

	blocksOf := func(id string) []string { return nil }
	if _, err := Link(w.ctx(other.ID, "li-agent"), agent, RelationBlocks, mine.ID, blocksOf); asNeedsApproval(err) == nil {
		t.Fatalf("建立关联应要求人确认，实际 %v", err)
	}
	if len(other.Relations) != 0 {
		t.Fatal("关联不应写入")
	}

	// 同样的动作，人来做不受影响
	if _, err := AddComment(w.ctx(other.ID, "li"), w.actors["li"], "人的评论", false); err != nil {
		t.Fatalf("人评论不应受影响，实际 %v", err)
	}
	if err := w.assign(other.ID, "wang", "zhang"); err != nil {
		t.Fatalf("人指派不应受影响，实际 %v", err)
	}
}

func TestValidateProposal(t *testing.T) {
	now := time.Now()
	ok := &Proposal{Action: "task.transition", Payload: map[string]any{"task_id": "t1"}, Summary: i18n.T("确认后会推进任务。", "It will advance the task."), CreatedAt: now, ExpiresAt: now.Add(ProposalTTL)}
	if errs := ValidateProposal(ok); len(errs) > 0 {
		t.Fatalf("合法的待确认操作不应报错：%v", RenderErrors(i18n.Default, errs))
	}
	bad := &Proposal{CreatedAt: now, ExpiresAt: now}
	errs := ValidateProposal(bad)
	if len(errs) != 4 {
		t.Fatalf("应报出动作名、输入、说明、有效期四个问题，实际 %v", RenderErrors(i18n.Default, errs))
	}
	for _, s := range RenderErrors(i18n.Default, errs) {
		if s == "" || !strings.Contains(s, "待确认操作") {
			t.Fatalf("校验理由应是完整中文句子，实际 %q", s)
		}
	}
}
