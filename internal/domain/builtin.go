package domain

import "github.com/teemo/axiomos/internal/i18n"

func w(n int) *int { return &n }

// tt 是内置定义里中英文本的简写："中文|English"。
func tt2(s string) i18n.Text {
	if i := indexPipe(s); i >= 0 {
		return i18n.T(s[:i], s[i+1:])
	}
	return i18n.T(s, s)
}

func indexPipe(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '|' {
			return i
		}
	}
	return -1
}

func st(name, title string, label Label) State {
	return State{Name: name, Title: tt2(title), Label: label}
}
func stw(name, title string, label Label, weight int) State {
	return State{Name: name, Title: tt2(title), Label: label, Weight: w(weight)}
}

func tr(name, title string, from []string, to string, by []string) Transition {
	return Transition{Name: name, Title: tt2(title), From: from, To: to, By: by}
}

func (t Transition) req(c ...string) Transition { t.Requires = c; return t }
func (t Transition) grant(g Grant) Transition   { t.Grant = g; return t }
func (t Transition) assign(a string) Transition { t.AssignTo = a; return t }

func commonTail(activeStates []string) []Transition {
	return []Transition{
		tr("block", "标记阻塞|Mark blocked", activeStates, "blocked", []string{"assignee", "creator"}),
		tr("cancel", "取消|Cancel", []string{"*"}, "cancelled", []string{"creator", "role:admin"}),
	}
}

// 内置交付物类型的中文名。
var BuiltinArtifactTitles = map[string]i18n.Text{
	"result":       i18n.T("结果摘要", "Result summary"),
	"prd":          i18n.T("需求文档", "PRD"),
	"pr":           i18n.T("代码 PR", "Pull request"),
	"test_report":  i18n.T("测试报告", "Test report"),
	"release_note": i18n.T("发布记录", "Release note"),
	"document":     i18n.T("文档", "Document"),
	"file":         i18n.T("文件", "File"),
	"link":         i18n.T("链接", "Link"),
}

// BuiltinRoles 是内置角色：代码名 → 名称 / 权限。
var BuiltinRoles = []struct {
	Name        string
	Title       i18n.Text
	Permissions []string
}{
	{"admin", i18n.T("管理员", "Administrator"), []string{"org_settings", "manage_workflows", "cancel_any_task", "view_all_cost"}},
	{"workflow_admin", i18n.T("流程管理", "Workflow manager"), []string{"manage_workflows"}},
	{"ops", i18n.T("运营", "Operations"), []string{"view_all_work", "view_all_cost"}},
	{"pm", i18n.T("产品", "Product manager"), nil},
	{"designer", i18n.T("设计", "Designer"), nil},
	{"developer", i18n.T("开发", "Developer"), nil},
	{"tester", i18n.T("测试", "Tester"), nil},
	{"releaser", i18n.T("发布", "Release manager"), nil},
}

// Permissions 是角色可持有的权限。
var Permissions = []string{"org_settings", "manage_workflows", "cancel_any_task", "view_all_work", "view_all_cost"}

// BuiltinTaskTypes 返回四种内置任务类型（docs/spec §8）。
func BuiltinTaskTypes() []*TaskType {
	generic := &TaskType{
		Name: "generic", Title: tt2("通用任务|Task"), BuiltIn: true,
		AgentInstructions: "阅读任务说明，完成后附上「结果摘要」交付物再提交。遇到不清楚的地方用「提问等待」向创建者提问。",
		Workflow: Workflow{
			Version: 1, Initial: "draft",
			States: []State{
				st("draft", "草稿|Draft", LabelPending),
				{Name: "todo", Title: tt2("待办|To do"), Label: LabelPending, Claimable: true},
				st("in_progress", "进行中|In progress", LabelActive),
				st("waiting", "等待答复|Awaiting reply", LabelWaiting),
				st("blocked", "已阻塞|Blocked", LabelWaiting),
				st("submitted", "待验收|Awaiting review", LabelWaiting),
				st("done", "已完成|Done", LabelTerminalSuccess),
				st("cancelled", "已取消|Cancelled", LabelTerminalFailure),
			},
			Transitions: append([]Transition{
				tr("ready", "就绪|Ready", []string{"draft"}, "todo", []string{"creator"}),
				tr("start", "开始|Start", []string{"todo"}, "in_progress", []string{"assignee"}).req("deps_done"),
				tr("ask_for_input", "提问等待|Ask and wait", []string{"in_progress"}, "waiting", []string{"assignee"}).req("comment"),
				tr("resume", "答复并恢复|Reply and resume", []string{"waiting"}, "in_progress", []string{"creator", "reviewer"}).req("comment"),
				tr("submit", "提交结果|Submit result", []string{"in_progress"}, "submitted", []string{"assignee"}).req("artifact:result"),
				tr("accept", "验收通过|Accept", []string{"submitted"}, "done", []string{"reviewer"}).grant(GrantReview),
				tr("reject", "验收打回|Send back", []string{"submitted"}, "todo", []string{"reviewer"}).req("comment").grant(GrantReview),
				tr("reopen", "重新打开|Reopen", []string{"done"}, "todo", []string{"reviewer", "creator"}),
				tr("unblock", "解除阻塞|Unblock", []string{"blocked"}, "todo", []string{"assignee", "creator"}),
			}, commonTail([]string{"todo", "in_progress", "waiting"})...),
		},
	}

	reqActive := []string{"designing", "developing", "testing", "releasing"}
	requirement := &TaskType{
		Name: "requirement", Title: tt2("需求|Requirement"), BuiltIn: true,
		Participants: []Participant{
			{Slot: "designer", Title: tt2("设计|Designer"), Role: "designer"},
			{Slot: "developer", Title: tt2("开发|Developer"), Role: "developer"},
			{Slot: "tester", Title: tt2("测试|Tester"), Role: "tester"},
			{Slot: "releaser", Title: tt2("发布|Release manager"), Role: "releaser"},
		},
		AgentInstructions: "按当前阶段工作：设计阶段产出「需求文档」，开发阶段产出「代码 PR」，测试阶段产出「测试报告」并为发现的问题创建 Bug 任务（关联「发现于」本任务），发布阶段产出「发布记录」。",
		Workflow: Workflow{
			Version: 1, Initial: "draft",
			States: []State{
				st("draft", "草稿|Draft", LabelPending),
				stw("designing", "设计中|Designing", LabelActive, 10),
				stw("developing", "开发中|Developing", LabelActive, 30),
				stw("testing", "测试中|Testing", LabelActive, 70),
				stw("releasing", "发布中|Releasing", LabelActive, 90),
				stw("awaiting_acceptance", "待验收|Awaiting acceptance", LabelWaiting, 95),
				st("waiting", "等待答复|Awaiting reply", LabelWaiting),
				st("blocked", "已阻塞|Blocked", LabelWaiting),
				st("done", "已上线|Released", LabelTerminalSuccess),
				st("cancelled", "已取消|Cancelled", LabelTerminalFailure),
			},
			Transitions: append([]Transition{
				tr("start_design", "开始设计|Start design", []string{"draft"}, "designing", []string{"creator"}).assign("participant:designer"),
				tr("design_done", "设计完成|Design done", []string{"designing"}, "developing", []string{"assignee"}).req("artifact:prd").assign("participant:developer"),
				tr("dev_done", "开发完成|Development done", []string{"developing"}, "testing", []string{"assignee"}).req("artifact:pr").assign("participant:tester"),
				tr("test_fail", "测试不通过|Test failed", []string{"testing"}, "developing", []string{"assignee"}).req("comment").assign("participant:developer"),
				tr("test_pass", "测试通过|Test passed", []string{"testing"}, "releasing", []string{"assignee"}).req("artifact:test_report", "no_open_bugs").assign("participant:releaser"),
				tr("release_done", "发布完成|Release done", []string{"releasing"}, "awaiting_acceptance", []string{"assignee"}).req("artifact:release_note"),
				tr("accept", "验收通过|Accept", []string{"awaiting_acceptance"}, "done", []string{"reviewer"}).grant(GrantReview),
				tr("reject", "验收打回|Send back", []string{"awaiting_acceptance"}, "developing", []string{"reviewer"}).req("comment").grant(GrantReview).assign("participant:developer"),
				tr("ask_for_input", "提问等待|Ask and wait", reqActive, "waiting", []string{"assignee"}).req("comment"),
				tr("resume", "答复并恢复|Reply and resume", []string{"waiting"}, "$previous", []string{"creator", "reviewer"}).req("comment"),
				tr("unblock", "解除阻塞|Unblock", []string{"blocked"}, "$previous", []string{"assignee", "creator"}),
			}, commonTail(reqActive)...),
		},
	}

	bug := &TaskType{
		Name: "bug", Title: tt2("Bug|Bug"), BuiltIn: true,
		Participants: []Participant{
			{Slot: "developer", Title: tt2("修复|Fixer"), Role: "developer"},
			{Slot: "tester", Title: tt2("验证|Verifier"), Role: "tester"},
		},
		AgentInstructions: "修复阶段：复现、修复并附上「代码 PR」。验证阶段：按复现步骤验证，通过则「验证通过」，否则「重新打开」并说明原因。",
		Workflow: Workflow{
			Version: 1, Initial: "new",
			States: []State{
				st("new", "新建|New", LabelPending),
				{Name: "confirmed", Title: tt2("已确认|Confirmed"), Label: LabelPending, Claimable: true},
				stw("fixing", "修复中|Fixing", LabelActive, 40),
				stw("fixed", "待验证|Awaiting verification", LabelWaiting, 80),
				st("verified", "已验证|Verified", LabelTerminalSuccess),
				st("rejected", "不是 Bug|Not a bug", LabelTerminalFailure),
				st("duplicate", "重复|Duplicate", LabelTerminalFailure),
				st("wont_fix", "不修复|Won't fix", LabelTerminalFailure),
			},
			Transitions: []Transition{
				tr("confirm", "确认|Confirm", []string{"new"}, "confirmed", []string{"creator", "role:tester"}).assign("participant:developer"),
				tr("reject", "判定非 Bug|Not a bug", []string{"new", "confirmed"}, "rejected", []string{"creator", "role:tester", "role:developer"}).req("comment"),
				tr("duplicate", "判定重复|Duplicate", []string{"new", "confirmed"}, "duplicate", []string{"creator", "role:tester", "role:developer"}).req("comment"),
				tr("wont_fix", "不修复|Won't fix", []string{"new", "confirmed", "fixing"}, "wont_fix", []string{"creator", "role:admin"}).req("comment"),
				tr("start_fix", "开始修复|Start fixing", []string{"confirmed"}, "fixing", []string{"assignee"}),
				tr("fixed", "修复完成|Fixed", []string{"fixing"}, "fixed", []string{"assignee"}).req("artifact:pr").assign("participant:tester"),
				tr("verify", "验证通过|Verified", []string{"fixed"}, "verified", []string{"assignee", "reviewer"}).grant(GrantReview),
				tr("reopen", "重新打开|Reopen", []string{"fixed", "verified"}, "fixing", []string{"assignee", "reviewer", "creator"}).req("comment").assign("participant:developer"),
			},
		},
	}

	release := &TaskType{
		Name: "release", Title: tt2("发布|Release"), BuiltIn: true,
		Participants:      []Participant{{Slot: "releaser", Title: tt2("发布|Release manager"), Role: "releaser"}},
		AgentInstructions: "确认所有前置需求已上线后执行发布，附上「发布记录」。",
		Workflow: Workflow{
			Version: 1, Initial: "draft",
			States: []State{
				st("draft", "草稿|Draft", LabelPending),
				{Name: "ready", Title: tt2("就绪|Ready"), Label: LabelPending, Claimable: true},
				stw("preparing", "发布中|Releasing", LabelActive, 50),
				stw("awaiting_acceptance", "待验收|Awaiting acceptance", LabelWaiting, 90),
				st("done", "已发布|Released", LabelTerminalSuccess),
				st("cancelled", "已取消|Cancelled", LabelTerminalFailure),
			},
			Transitions: []Transition{
				tr("ready", "就绪|Ready", []string{"draft"}, "ready", []string{"creator"}).assign("participant:releaser"),
				tr("start", "开始发布|Start release", []string{"ready"}, "preparing", []string{"assignee"}).req("deps_done"),
				tr("release_done", "发布完成|Release done", []string{"preparing"}, "awaiting_acceptance", []string{"assignee"}).req("artifact:release_note"),
				tr("accept", "验收通过|Accept", []string{"awaiting_acceptance"}, "done", []string{"reviewer"}).grant(GrantReview),
				tr("reject", "验收打回|Send back", []string{"awaiting_acceptance"}, "ready", []string{"reviewer"}).req("comment").grant(GrantReview),
				tr("cancel", "取消|Cancel", []string{"*"}, "cancelled", []string{"creator", "role:admin"}),
			},
		},
	}
	return []*TaskType{generic, requirement, bug, release}
}
