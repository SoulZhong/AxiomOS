package domain

import (
	"encoding/json"
	"fmt"

	"github.com/teemo/axiomos/internal/i18n"
)

// 显示偏好（功能规划第 4 项）：任务列表显示哪些列、看板卡片显示哪些字段、默认打开哪个任务视图、
// 是否紧凑、侧栏默认收起。这些是"每个人怎么看"的选择，不是权限，也不改数据。解析顺序与工作台一致
// （ADR 0015）：个人 → 角色 → 系统默认；每条记录都是部分对象，只存明确设过的字段，逐层补齐。
// 列与字段的集合是系统定义的（ADR 0014：客户不能自造列）。

// PreferenceItem 是目录里的一列 / 一个字段 / 一个视图。
type PreferenceItem struct {
	Key   string
	Title i18n.Text
}

// TaskListColumns 是任务列表可显示的列（目录顺序即默认排列顺序）。
var TaskListColumns = []PreferenceItem{
	{"number", i18n.T("序号", "Number")},
	{"title", i18n.T("标题", "Title")},
	{"type", i18n.T("类型", "Type")},
	{"state", i18n.T("状态", "State")},
	{"assignee", i18n.T("负责人", "Assignee")},
	{"reviewer", i18n.T("验收人", "Reviewer")},
	{"goal", i18n.T("目标", "Goal")},
	{"sprint", i18n.T("迭代", "Sprint")},
	{"priority", i18n.T("优先级", "Priority")},
	{"points", i18n.T("工作量", "Points")},
	{"planned", i18n.T("计划起止", "Planned dates")},
	{"due", i18n.T("截止日", "Due")},
	{"cost", i18n.T("成本", "Cost")},
}

// TaskCardFields 是看板卡片可显示的字段。
var TaskCardFields = []PreferenceItem{
	{"number", i18n.T("序号", "Number")},
	{"assignee", i18n.T("负责人", "Assignee")},
	{"due", i18n.T("截止日", "Due")},
	{"priority", i18n.T("优先级", "Priority")},
	{"points", i18n.T("工作量", "Points")},
	{"goal", i18n.T("目标", "Goal")},
}

// TaskViews 是「任务」入口下的页签。
var TaskViews = []PreferenceItem{
	{"list", i18n.T("列表", "List")},
	{"board", i18n.T("看板", "Board")},
	{"gantt", i18n.T("甘特图", "Gantt")},
	{"sprints", i18n.T("迭代", "Sprints")},
	{"backlog", i18n.T("待领取", "Unclaimed")},
}

// 偏好字段名（JSON 键）。
const (
	PrefTaskListColumns  = "task_list_columns"
	PrefTaskCardFields   = "task_card_fields"
	PrefDefaultTaskView  = "default_task_view"
	PrefCompact          = "compact"
	PrefSidebarCollapsed = "sidebar_collapsed"
)

// PreferenceFields 是全部字段名，按固定顺序。
var PreferenceFields = []string{PrefTaskListColumns, PrefTaskCardFields, PrefDefaultTaskView, PrefCompact, PrefSidebarCollapsed}

// Preferences 是解析完成的一套偏好。
type Preferences struct {
	TaskListColumns  []string `json:"task_list_columns"`
	TaskCardFields   []string `json:"task_card_fields"`
	DefaultTaskView  string   `json:"default_task_view"`
	Compact          bool     `json:"compact"`
	SidebarCollapsed bool     `json:"sidebar_collapsed"`
}

// DefaultPreferences 是系统默认（组织与个人都没设时）。
func DefaultPreferences() Preferences {
	return Preferences{
		TaskListColumns:  []string{"number", "title", "type", "state", "assignee", "goal", "priority", "due"},
		TaskCardFields:   []string{"number", "assignee", "due", "priority"},
		DefaultTaskView:  "list",
		Compact:          false,
		SidebarCollapsed: false,
	}
}

// PreferencePatch 是一条记录（个人或角色）存下来的部分对象：nil 表示没设。
type PreferencePatch struct {
	TaskListColumns  []string `json:"task_list_columns,omitempty"`
	TaskCardFields   []string `json:"task_card_fields,omitempty"`
	DefaultTaskView  *string  `json:"default_task_view,omitempty"`
	Compact          *bool    `json:"compact,omitempty"`
	SidebarCollapsed *bool    `json:"sidebar_collapsed,omitempty"`
}

// IsEmpty 判断一条记录是否什么都没设。
func (p PreferencePatch) IsEmpty() bool {
	return p.TaskListColumns == nil && p.TaskCardFields == nil && p.DefaultTaskView == nil && p.Compact == nil && p.SidebarCollapsed == nil
}

// Fields 返回设了的字段名。
func (p PreferencePatch) Fields() []string {
	var out []string
	if p.TaskListColumns != nil {
		out = append(out, PrefTaskListColumns)
	}
	if p.TaskCardFields != nil {
		out = append(out, PrefTaskCardFields)
	}
	if p.DefaultTaskView != nil {
		out = append(out, PrefDefaultTaskView)
	}
	if p.Compact != nil {
		out = append(out, PrefCompact)
	}
	if p.SidebarCollapsed != nil {
		out = append(out, PrefSidebarCollapsed)
	}
	return out
}

// PreferenceError 是校验错误（可翻译）。
type PreferenceError struct{ Msg i18n.Msg }

func (e *PreferenceError) Error() string { return e.Msg.Render(i18n.Default) }

func prefErr(key string, args ...any) error { return &PreferenceError{Msg: i18n.M(key, args...)} }

func itemKnown(items []PreferenceItem, key string) bool {
	for _, it := range items {
		if it.Key == key {
			return true
		}
	}
	return false
}

func itemKeys(items []PreferenceItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Key)
	}
	return out
}

// ValidatePreferencePatch 校验一条部分对象：列 / 字段必须在目录里、不重复；列表至少要有标题；视图必须存在。
func ValidatePreferencePatch(p PreferencePatch) error {
	if p.TaskListColumns != nil {
		if len(p.TaskListColumns) == 0 {
			return prefErr("err.pref_columns_empty")
		}
		seen := map[string]bool{}
		hasTitle := false
		for _, c := range p.TaskListColumns {
			if !itemKnown(TaskListColumns, c) {
				return prefErr("err.pref_column_unknown", c, itemKeys(TaskListColumns))
			}
			if seen[c] {
				return prefErr("err.pref_column_duplicate", c)
			}
			seen[c] = true
			if c == "title" {
				hasTitle = true
			}
		}
		if !hasTitle {
			return prefErr("err.pref_columns_title")
		}
	}
	if p.TaskCardFields != nil {
		seen := map[string]bool{}
		for _, f := range p.TaskCardFields {
			if !itemKnown(TaskCardFields, f) {
				return prefErr("err.pref_field_unknown", f, itemKeys(TaskCardFields))
			}
			if seen[f] {
				return prefErr("err.pref_field_duplicate", f)
			}
			seen[f] = true
		}
	}
	if p.DefaultTaskView != nil && !itemKnown(TaskViews, *p.DefaultTaskView) {
		return prefErr("err.pref_view_unknown", *p.DefaultTaskView, itemKeys(TaskViews))
	}
	return nil
}

// ParsePreferencePatch 从 JSON 解一条部分对象：只认目录里的字段名，值为 null 的字段视为"清掉这一项"，
// 返回时放进 cleared。不认识的键报错，避免把打错的字段名静默吞掉。
func ParsePreferencePatch(raw []byte) (patch PreferencePatch, cleared []string, err error) {
	if len(raw) == 0 {
		return patch, nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return patch, nil, prefErr("err.pref_shape")
	}
	for k, v := range m {
		known := false
		for _, f := range PreferenceFields {
			if f == k {
				known = true
			}
		}
		if !known {
			return patch, nil, prefErr("err.pref_key_unknown", k, PreferenceFields)
		}
		if string(v) == "null" {
			cleared = append(cleared, k)
			continue
		}
		var target any
		switch k {
		case PrefTaskListColumns:
			target = &patch.TaskListColumns
		case PrefTaskCardFields:
			target = &patch.TaskCardFields
		case PrefDefaultTaskView:
			target = &patch.DefaultTaskView
		case PrefCompact:
			target = &patch.Compact
		case PrefSidebarCollapsed:
			target = &patch.SidebarCollapsed
		}
		if err := json.Unmarshal(v, target); err != nil {
			return patch, nil, prefErr("err.pref_value", k)
		}
		// 传了空数组的列表字段算"设了"（列表不能为空会在校验里拒绝；卡片字段允许空）
		if k == PrefTaskListColumns && patch.TaskListColumns == nil {
			patch.TaskListColumns = []string{}
		}
		if k == PrefTaskCardFields && patch.TaskCardFields == nil {
			patch.TaskCardFields = []string{}
		}
	}
	return patch, cleared, nil
}

// MergePreferencePatch 把新设的字段并进已有记录（没提到的保留，cleared 里的清掉）。
func MergePreferencePatch(base, in PreferencePatch, cleared []string) PreferencePatch {
	out := base
	if in.TaskListColumns != nil {
		out.TaskListColumns = in.TaskListColumns
	}
	if in.TaskCardFields != nil {
		out.TaskCardFields = in.TaskCardFields
	}
	if in.DefaultTaskView != nil {
		out.DefaultTaskView = in.DefaultTaskView
	}
	if in.Compact != nil {
		out.Compact = in.Compact
	}
	if in.SidebarCollapsed != nil {
		out.SidebarCollapsed = in.SidebarCollapsed
	}
	for _, k := range cleared {
		switch k {
		case PrefTaskListColumns:
			out.TaskListColumns = nil
		case PrefTaskCardFields:
			out.TaskCardFields = nil
		case PrefDefaultTaskView:
			out.DefaultTaskView = nil
		case PrefCompact:
			out.Compact = nil
		case PrefSidebarCollapsed:
			out.SidebarCollapsed = nil
		}
	}
	return out
}

// 偏好的来源。
const (
	PreferenceSourcePersonal = "personal"
	PreferenceSourceRole     = "role"
	PreferenceSourceDefault  = "default"
)

// ResolvePreferences 逐字段解析：个人设了用个人的；否则按成员的角色顺序取第一个设了这项的角色；都没有用系统默认。
// 返回每个字段的来源与整体来源（有任一字段来自个人即 personal，否则有任一来自角色即 role，否则 default）。
func ResolvePreferences(personal *PreferencePatch, roleOrder []string, byRole map[string]PreferencePatch) (out Preferences, sources map[string]string, overall string) {
	out = DefaultPreferences()
	sources = map[string]string{}
	overall = PreferenceSourceDefault
	pick := func(field string, fromPersonal func(PreferencePatch) bool, apply func(PreferencePatch)) {
		if personal != nil && fromPersonal(*personal) {
			apply(*personal)
			sources[field] = PreferenceSourcePersonal
			overall = PreferenceSourcePersonal
			return
		}
		for _, r := range roleOrder {
			if p, ok := byRole[r]; ok && fromPersonal(p) {
				apply(p)
				sources[field] = PreferenceSourceRole
				if overall == PreferenceSourceDefault {
					overall = PreferenceSourceRole
				}
				return
			}
		}
		sources[field] = PreferenceSourceDefault
	}
	pick(PrefTaskListColumns, func(p PreferencePatch) bool { return p.TaskListColumns != nil }, func(p PreferencePatch) { out.TaskListColumns = append([]string{}, p.TaskListColumns...) })
	pick(PrefTaskCardFields, func(p PreferencePatch) bool { return p.TaskCardFields != nil }, func(p PreferencePatch) { out.TaskCardFields = append([]string{}, p.TaskCardFields...) })
	pick(PrefDefaultTaskView, func(p PreferencePatch) bool { return p.DefaultTaskView != nil }, func(p PreferencePatch) { out.DefaultTaskView = *p.DefaultTaskView })
	pick(PrefCompact, func(p PreferencePatch) bool { return p.Compact != nil }, func(p PreferencePatch) { out.Compact = *p.Compact })
	pick(PrefSidebarCollapsed, func(p PreferencePatch) bool { return p.SidebarCollapsed != nil }, func(p PreferencePatch) { out.SidebarCollapsed = *p.SidebarCollapsed })
	return out, sources, overall
}

// PreferenceTitle 返回目录项的名字（找不到时返回键本身）。
func PreferenceTitle(items []PreferenceItem, key string, loc i18n.Locale) string {
	for _, it := range items {
		if it.Key == key {
			return it.Title.In(loc)
		}
	}
	return key
}

// TaskRef 把任务序号写成界面上的形式：#123。
func TaskRef(number int) string { return fmt.Sprintf("#%d", number) }
