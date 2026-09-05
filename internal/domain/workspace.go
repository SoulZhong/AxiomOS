package domain

import (
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// 工作台（ADR 0015）：首页由若干区块按顺序组成。区块与预设是系统定义的有限集合，
// 「哪个角色先看什么」是组织自己配的管理策略（ADR 0014）。这里只有纯函数与常量。

// Block 是工作台上的一个区块。
type Block struct {
	Key         string
	Title       i18n.Text
	Description i18n.Text
}

// Blocks 是全部区块，顺序即目录顺序。
var Blocks = []Block{
	{"overview_summary", i18n.T("组织概览摘要", "Organization overview"), i18n.T("当前范围内的任务、成本与负荷一眼看完。", "Tasks, cost and load in the current scope at a glance.")},
	{"exceptions", i18n.T("要我关注的异常", "Exceptions for me"), i18n.T("逾期、阻塞、超预算和长时间没人接的任务。", "Overdue, blocked, over-budget and long-unclaimed tasks.")},
	{"my_tasks", i18n.T("我的任务", "My tasks"), i18n.T("我负责、正在做和等我答复的任务。", "Tasks I own, am working on, or that await my reply.")},
	{"my_review", i18n.T("等我验收", "Awaiting my review"), i18n.T("已提交、等我验收的任务。", "Submitted tasks waiting for my review.")},
	{"team_load", i18n.T("人员与 Agent 负荷", "People and agent load"), i18n.T("当前范围内每个人和每个 Agent 手上有多少活。", "How much work each person and agent in scope is carrying.")},
	{"cost_budget", i18n.T("成本与预算", "Cost and budget"), i18n.T("当前范围内的成本花到哪里、离预算还有多远。", "Where cost is going in scope and how far it is from budget.")},
	{"proposals", i18n.T("待确认操作", "Pending actions"), i18n.T("Agent 发起、等人确认后才会执行的操作。", "Actions agents proposed that wait for a person to confirm.")},
	{"events", i18n.T("最近动态", "Recent activity"), i18n.T("当前范围内最近发生了什么。", "What happened recently in scope.")},
	{"sprint", i18n.T("当前迭代", "Current sprint"), i18n.T("进行中迭代的进度与剩余工作量。", "Progress and remaining work of the active sprint.")},
	{"backlog", i18n.T("待领取任务", "Unclaimed tasks"), i18n.T("没人负责、可以领取的任务。", "Tasks nobody owns yet that can be claimed.")},
	{"trend", i18n.T("趋势与环比", "Trends"), i18n.T("吞吐、周期与成本和上一期相比的变化。", "Throughput, cycle time and cost compared with the previous period.")},
}

// Preset 是系统发的一套区块组合，按「这类人关心什么」命名，不绑定角色名。
type Preset struct {
	Key         string
	Title       i18n.Text
	Description i18n.Text
	Blocks      []string
}

// 预设键。
const (
	PresetGlobal = "global"
	PresetUnit   = "unit"
	PresetTeam   = "team"
	PresetDoer   = "doer"
	PresetOps    = "ops"
)

// Presets 是全部预设，顺序即目录顺序。
var Presets = []Preset{
	{PresetGlobal, i18n.T("全局视角", "Global view"), i18n.T("看全公司的概览、异常、成本与趋势，适合组织负责人。", "Company-wide overview, exceptions, cost and trends, for the organization owner."),
		[]string{"overview_summary", "exceptions", "cost_budget", "trend", "proposals"}},
	{PresetUnit, i18n.T("部门视角", "Unit view"), i18n.T("看本部的概览、异常与负荷，兼顾等我验收与待确认操作，适合部门负责人。", "Unit overview, exceptions and load, plus reviews and pending actions, for unit leads."),
		[]string{"overview_summary", "exceptions", "team_load", "my_review", "proposals", "events"}},
	{PresetTeam, i18n.T("小组视角", "Team view"), i18n.T("先看等我验收和小组负荷，再看异常、我的任务与当前迭代，适合小组负责人。", "Reviews and team load first, then exceptions, my tasks and the sprint, for team leads."),
		[]string{"my_review", "team_load", "exceptions", "my_tasks", "sprint", "events"}},
	{PresetDoer, i18n.T("执行视角", "Doer view"), i18n.T("先看我的任务，再看等我验收、待领取任务与当前迭代，适合一线成员。", "My tasks first, then reviews, unclaimed tasks and the sprint, for hands-on members."),
		[]string{"my_tasks", "my_review", "backlog", "sprint", "events"}},
	{PresetOps, i18n.T("运营视角", "Operations view"), i18n.T("先看成本与趋势，再看待确认操作、异常与负荷，适合运营与流程管理。", "Cost and trends first, then pending actions, exceptions and load, for operations and workflow managers."),
		[]string{"cost_budget", "trend", "proposals", "exceptions", "team_load", "events"}},
}

// 工作台来源。
const (
	WorkspaceSourcePersonal = "personal"
	WorkspaceSourceRoles    = "roles"
	WorkspaceSourceDefault  = "default"
)

// WorkspaceLayout 是一条布局：角色的（RoleName 非空）或成员个人的（MemberID 非空），二者只居其一。
type WorkspaceLayout struct {
	RoleName  string
	MemberID  string
	Blocks    []string
	Preset    string // 套用的预设键；逐块改过则为空
	UpdatedAt time.Time
}

// BlockByKey 按键找区块。
func BlockByKey(key string) (Block, bool) {
	for _, b := range Blocks {
		if b.Key == key {
			return b, true
		}
	}
	return Block{}, false
}

// PresetByKey 按键找预设。
func PresetByKey(key string) (Preset, bool) {
	for _, p := range Presets {
		if p.Key == key {
			return p, true
		}
	}
	return Preset{}, false
}

// WorkspaceError 是区块列表不合法的原因，由应用层按语言渲染成完整句子。
type WorkspaceError struct{ Msg i18n.Msg }

func (e *WorkspaceError) Error() string { return e.Msg.Render(i18n.Default) }

// ValidateBlocks 检查一份区块列表：非空、只用系统区块键、不重复。
func ValidateBlocks(keys []string) error {
	if len(keys) == 0 {
		return &WorkspaceError{i18n.M("err.blocks_empty")}
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if _, ok := BlockByKey(k); !ok {
			return &WorkspaceError{i18n.M("err.block_unknown", k)}
		}
		if seen[k] {
			return &WorkspaceError{i18n.M("err.block_duplicate", k)}
		}
		seen[k] = true
	}
	return nil
}

// MergeLayouts 求多份布局的并集，按首次出现的顺序去重。
func MergeLayouts(layouts [][]string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, l := range layouts {
		for _, k := range l {
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// DefaultPreset 是没有任何配置时的预设：组织负责人用全局视角，其他人用执行视角。
func DefaultPreset(isOwner bool) string {
	if isOwner {
		return PresetGlobal
	}
	return PresetDoer
}

// ResolveWorkspace 按顺序解析一个人的工作台：个人微调 → 各角色布局并集 → 默认。
// roleLayouts 按成员的角色顺序传入（没有布局的角色不要传）。返回区块与来源。
func ResolveWorkspace(personal []string, roleLayouts [][]string, isOwner bool) (blocks []string, source string) {
	if len(personal) > 0 {
		return append([]string{}, personal...), WorkspaceSourcePersonal
	}
	if merged := MergeLayouts(roleLayouts); len(merged) > 0 {
		return merged, WorkspaceSourceRoles
	}
	p, _ := PresetByKey(DefaultPreset(isOwner))
	return append([]string{}, p.Blocks...), WorkspaceSourceDefault
}

// BuiltinRolePreset 是首次种子时内置角色套用的预设；不是内置角色或没有映射时返回空。
// 组织可以随时改，这里只是默认值（ADR 0014）。
func BuiltinRolePreset(role string) string {
	switch role {
	case "admin":
		return PresetGlobal
	case "ops", "workflow_admin":
		return PresetOps
	case "pm", "designer", "developer", "tester", "releaser":
		return PresetDoer
	}
	return ""
}
