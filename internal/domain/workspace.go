package domain

import (
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// 工作台（ADR 0015）：首页由若干区块摆在 12 栏网格上组成。区块与预设是系统定义的有限集合，
// 「哪个角色先看什么」是组织自己配的管理策略（ADR 0014）。这里只有纯函数与常量。

// Block 是工作台上的一个区块。DefaultW / DefaultH 是放上去时的默认宽高，MinW 是编辑器不许再窄的下限
// （ADR 0015 补记三、DESIGN.md §13：12 栏网格，宽 MinW…12 栏任意整数，高 1…LayoutMaxH 行任意整数）。
type Block struct {
	Key         string
	Title       i18n.Text
	Description i18n.Text
	DefaultW    int
	DefaultH    int
	MinW        int
}

// LayoutBlock 是布局里的一格：区块键加网格坐标与宽高（ADR 0015 补记三）。
// X 从 0 起且 X+W ≤ LayoutCols，Y 从 0 起；W 在该区块的 MinW 到 LayoutCols 之间，H 在 1 到 LayoutMaxH 之间。
type LayoutBlock struct {
	Key string `json:"key"`
	X   int    `json:"x"`
	Y   int    `json:"y"`
	W   int    `json:"w"`
	H   int    `json:"h"`
}

// LayoutCols 是网格的栏数。
const LayoutCols = 12

// LayoutMaxH 是区块最高的行数（行高单位约 120px）。
const LayoutMaxH = 6

// widthName 是最小宽度的人话名字（词条 width.N），用在报错里。
func widthName(w int) i18n.Key { return i18n.Key("width." + strconv.Itoa(w)) }

// Blocks 是全部区块，顺序即目录顺序。数字类区块最窄可到四分之一（3 栏，紧凑态只显示关键数字），表格类最窄一半（6 栏）。「待确认操作」不再是区块：它已并入「待我处理」（DESIGN.md §12）。
// 「待我处理」与「组织概况」也是区块（ADR 0015 补记四）：页面上不再有布局之外的固定区域，二者同样可拖动、改大小、移除。
var Blocks = []Block{
	{"inbox", i18n.T("待我处理", "For me"), i18n.T("等我确认、验收、答复的事和我负责但逾期的任务，只看本人、不受范围影响。", "Things waiting on me: confirmations, reviews, replies and my overdue tasks; personal, ignores scope."), 12, 2, 6},
	{"readouts", i18n.T("组织概况", "Organization readouts"), i18n.T("进行中、待验收、逾期与今日成本四个实时读数，带 24 小时趋势线。", "Live counts of active, awaiting review and overdue tasks plus today's cost, with 24-hour trends."), 12, 1, 6},
	{"overview_summary", i18n.T("组织概览摘要", "Organization overview"), i18n.T("当前范围内的任务、成本与负荷一眼看完。", "Tasks, cost and load in the current scope at a glance."), 12, 1, 3},
	{"exceptions", i18n.T("要我关注的异常", "Exceptions for me"), i18n.T("逾期、阻塞、超预算和长时间没人接的任务。", "Overdue, blocked, over-budget and long-unclaimed tasks."), 6, 2, 3},
	{"my_tasks", i18n.T("我的任务", "My tasks"), i18n.T("我负责、正在做和等我答复的任务。", "Tasks I own, am working on, or that await my reply."), 12, 2, 6},
	{"my_review", i18n.T("等我验收", "Awaiting my review"), i18n.T("已提交、等我验收的任务。", "Submitted tasks waiting for my review."), 12, 2, 6},
	{"team_load", i18n.T("人员与 Agent 负荷", "People and agent load"), i18n.T("当前范围内每个人和每个 Agent 手上有多少活。", "How much work each person and agent in scope is carrying."), 6, 2, 3},
	{"cost_budget", i18n.T("成本与预算", "Cost and budget"), i18n.T("当前范围内的成本花到哪里、离预算还有多远。", "Where cost is going in scope and how far it is from budget."), 6, 2, 3},
	{"events", i18n.T("最近动态", "Recent activity"), i18n.T("当前范围内最近发生了什么。", "What happened recently in scope."), 6, 3, 3},
	{"sprint", i18n.T("当前迭代", "Current sprint"), i18n.T("进行中迭代的进度与剩余工作量。", "Progress and remaining work of the active sprint."), 6, 2, 3},
	{"backlog", i18n.T("待领取任务", "Unclaimed tasks"), i18n.T("没人负责、可以领取的任务。", "Tasks nobody owns yet that can be claimed."), 6, 2, 3},
	{"trend", i18n.T("趋势与环比", "Trends"), i18n.T("吞吐、周期与成本和上一期相比的变化。", "Throughput, cycle time and cost compared with the previous period."), 12, 2, 6},
}

// Preset 是系统发的一套区块组合，按「这类人关心什么」命名，不绑定角色名。
type Preset struct {
	Key         string
	Title       i18n.Text
	Description i18n.Text
	Blocks      []LayoutBlock // 带坐标与宽高，已致密、无空洞
}

// 预设键。
const (
	PresetGlobal = "global"
	PresetUnit   = "unit"
	PresetTeam   = "team"
	PresetDoer   = "doer"
	PresetOps    = "ops"
)

// Presets 是全部预设，顺序即目录顺序。每个预设的区块按阅读顺序（先 y 后 x）排列，坐标显式给出。
// 每个预设都以 inbox 开头，其后是 readouts（执行视角除外：一线成员不需要组织读数），再是各自的区块（ADR 0015 补记四）。
var Presets = []Preset{
	{PresetGlobal, i18n.T("全局视角", "Global view"), i18n.T("看全公司的概览、异常、成本与趋势，适合组织负责人。", "Company-wide overview, exceptions, cost and trends, for the organization owner."),
		[]LayoutBlock{
			{"inbox", 0, 0, 12, 2},
			{"readouts", 0, 2, 12, 1},
			{"overview_summary", 0, 3, 12, 1},
			{"exceptions", 0, 4, 6, 2},
			{"cost_budget", 6, 4, 6, 2},
			{"trend", 0, 6, 12, 2},
		}},
	{PresetUnit, i18n.T("部门视角", "Unit view"), i18n.T("看本部的概览、异常与负荷，兼顾等我验收，适合部门负责人。", "Unit overview, exceptions and load, plus reviews, for unit leads."),
		[]LayoutBlock{
			{"inbox", 0, 0, 12, 2},
			{"readouts", 0, 2, 12, 1},
			{"overview_summary", 0, 3, 12, 1},
			{"exceptions", 0, 4, 6, 2},
			{"team_load", 6, 4, 6, 2},
			{"my_review", 0, 6, 6, 3},
			{"events", 6, 6, 6, 3},
		}},
	{PresetTeam, i18n.T("小组视角", "Team view"), i18n.T("先看等我验收和小组负荷，再看异常、我的任务与当前迭代，适合小组负责人。", "Reviews and team load first, then exceptions, my tasks and the sprint, for team leads."),
		[]LayoutBlock{
			{"inbox", 0, 0, 12, 2},
			{"readouts", 0, 2, 12, 1},
			{"my_review", 0, 3, 6, 2},
			{"team_load", 6, 3, 6, 2},
			{"exceptions", 0, 5, 6, 2},
			{"sprint", 6, 5, 6, 2},
			{"my_tasks", 0, 7, 12, 2},
			{"events", 0, 9, 12, 3},
		}},
	{PresetDoer, i18n.T("执行视角", "Doer view"), i18n.T("先看我的任务，再看等我验收、待领取任务与当前迭代，适合一线成员。", "My tasks first, then reviews, unclaimed tasks and the sprint, for hands-on members."),
		[]LayoutBlock{
			{"inbox", 0, 0, 12, 2},
			{"my_tasks", 0, 2, 12, 2},
			{"my_review", 0, 4, 12, 2},
			{"backlog", 0, 6, 6, 2},
			{"sprint", 6, 6, 6, 2},
			{"events", 0, 8, 12, 3},
		}},
	{PresetOps, i18n.T("运营视角", "Operations view"), i18n.T("先看成本与趋势，再看异常与负荷，适合运营与流程管理。", "Cost and trends first, then exceptions and load, for operations and workflow managers."),
		[]LayoutBlock{
			{"inbox", 0, 0, 12, 2},
			{"readouts", 0, 2, 12, 1},
			{"cost_budget", 0, 3, 6, 2},
			{"trend", 6, 3, 6, 2},
			{"exceptions", 0, 5, 6, 2},
			{"team_load", 6, 5, 6, 2},
			{"events", 0, 7, 12, 3},
		}},
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
	Blocks    []LayoutBlock
	Preset    string // 套用的预设键；逐块改过则为空
	UpdatedAt time.Time
}

// RetiredBlocks 是曾经存在、现已从目录移除的区块键。已存布局里出现它们时静默丢弃（ADR 0015 §12）。
var RetiredBlocks = []string{"proposals"}

// IsRetiredBlock 判断一个键是否是已移除的区块。
func IsRetiredBlock(key string) bool { return contains(RetiredBlocks, key) }

// DropRetiredBlocks 去掉已移除的区块键，保持其余顺序；总是返回新切片。
func DropRetiredBlocks(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if !IsRetiredBlock(k) {
			out = append(out, k)
		}
	}
	return out
}

// DropRetiredLayout 是 DropRetiredBlocks 的带坐标版本；总是返回新切片。
func DropRetiredLayout(blocks []LayoutBlock) []LayoutBlock {
	out := make([]LayoutBlock, 0, len(blocks))
	for _, b := range blocks {
		if !IsRetiredBlock(b.Key) {
			out = append(out, b)
		}
	}
	return out
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

// SizedBlocks 把区块键按目录默认宽高展开并按顺序致密排布成布局；未知键保留原样、宽高为 0（交给 ValidateLayout 报错）。
func SizedBlocks(keys ...string) []LayoutBlock {
	out := make([]LayoutBlock, 0, len(keys))
	for _, k := range keys {
		out = append(out, withDefaults(LayoutBlock{Key: k}))
	}
	return PlaceLayout(out, nil)
}

// withDefaults 给缺省（0）的宽高补上目录默认值。
func withDefaults(b LayoutBlock) LayoutBlock {
	if def, ok := BlockByKey(b.Key); ok {
		if b.W == 0 {
			b.W = def.DefaultW
		}
		if b.H == 0 {
			b.H = def.DefaultH
		}
	}
	return b
}

// LayoutKeys 取布局里的区块键，保持顺序；总是返回新切片。
func LayoutKeys(blocks []LayoutBlock) []string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.Key)
	}
	return out
}

// ---------- 排布与压实 ----------

// span 把宽高收进网格能表达的范围，只用于碰撞计算；坏尺寸本身留给 ValidateLayout 报错。
func span(n, max int) int {
	if n < 1 {
		return 1
	}
	if n > max {
		return max
	}
	return n
}

// overlaps 判断两块矩形是否相交。
func overlaps(a, b LayoutBlock) bool {
	aw, bw := span(a.W, LayoutCols), span(b.W, LayoutCols)
	ah, bh := span(a.H, LayoutMaxH), span(b.H, LayoutMaxH)
	return a.X < b.X+bw && b.X < a.X+aw && a.Y < b.Y+bh && b.Y < a.Y+ah
}

// collides 判断 b 是否与 others 中任何一块相交。
func collides(b LayoutBlock, others []LayoutBlock) bool {
	for _, o := range others {
		if overlaps(b, o) {
			return true
		}
	}
	return false
}

// LayoutOverlaps 判断一份布局里是否有两块相交。
func LayoutOverlaps(blocks []LayoutBlock) bool {
	for i := range blocks {
		if collides(blocks[i], blocks[i+1:]) {
			return true
		}
	}
	return false
}

// firstFree 从上到下、从左到右扫描，找到第一个放得下 b 且不与 fixed 相交的格子。
// fixed 有限，扫到它们下方总能放下，所以必定返回。
func firstFree(b LayoutBlock, fixed []LayoutBlock) (x, y int) {
	w := span(b.W, LayoutCols)
	for y = 0; ; y++ {
		for x = 0; x+w <= LayoutCols; x++ {
			b.X, b.Y = x, y
			if !collides(b, fixed) {
				return x, y
			}
		}
	}
}

// PlaceLayout 是确定性的致密排布：placed[i] 为真的区块带着坐标来，只要不与前面已固定的区块相交就原地不动；
// 其余区块（没有坐标的，或坐标与别人撞上的）按输入顺序逐个落到第一个放得下的格子——先从上到下扫行，
// 再从左到右扫栏。placed 为 nil 表示全部区块都没有坐标。结果保持输入顺序；总是返回新切片。
func PlaceLayout(blocks []LayoutBlock, placed []bool) []LayoutBlock {
	out := make([]LayoutBlock, len(blocks))
	copy(out, blocks)
	fixed := make([]LayoutBlock, 0, len(out))
	pending := make([]int, 0, len(out))
	for i := range out {
		if placed != nil && placed[i] && !collides(out[i], fixed) {
			fixed = append(fixed, out[i])
			continue
		}
		pending = append(pending, i)
	}
	for _, i := range pending {
		out[i].X, out[i].Y = firstFree(out[i], fixed)
		fixed = append(fixed, out[i])
	}
	return out
}

// sortByPosition 按先 y 后 x 的阅读顺序稳定排序。
func sortByPosition(blocks []LayoutBlock) {
	sort.SliceStable(blocks, func(i, j int) bool {
		if blocks[i].Y != blocks[j].Y {
			return blocks[i].Y < blocks[j].Y
		}
		return blocks[i].X < blocks[j].X
	})
}

// CompactLayout 纵向压实：按先 y 后 x 稳定排序，然后逐块尽量上移，直到碰到上方的区块或顶部。
// 存下来的布局因此没有纵向空洞。结果按阅读顺序（先 y 后 x）排列；总是返回新切片。
func CompactLayout(blocks []LayoutBlock) []LayoutBlock {
	out := make([]LayoutBlock, len(blocks))
	copy(out, blocks)
	sortByPosition(out)
	for i := range out {
		for out[i].Y > 0 {
			probe := out[i]
			probe.Y--
			if collides(probe, out[:i]) {
				break
			}
			out[i] = probe
		}
	}
	sortByPosition(out)
	return out
}

// ---------- 解析与校验 ----------

// WorkspaceError 是区块列表不合法的原因，由应用层按语言渲染成完整句子。
type WorkspaceError struct{ Msg i18n.Msg }

func (e *WorkspaceError) Error() string { return e.Msg.Render(i18n.Default) }

// ParseLayout 把任意形状的区块列表解析成 []LayoutBlock，只看形状、不做校验：
// 接受 []string（最早的形状）、[]LayoutBlock，以及 JSON 解出来的 []any（元素是字符串、{key, w, h} 或
// {key, x, y, w, h} 对象，x / y / w / h 都可省略）。缺省的宽高补目录默认值；缺 x / y 的区块按顺序
// 致密排布（PlaceLayout），坐标撞上别人的也重新排；已移除的区块静默丢弃。
// Go 侧的 []LayoutBlock 里，X 与 Y 都为 0 的区块视为没有坐标（它若真该在左上角，排布也会把它放回去）。
// 元素形状不对时返回 WorkspaceError（err.block_shape）。nil 返回空切片。
func ParseLayout(raw any) ([]LayoutBlock, error) {
	var out []LayoutBlock
	var placed []bool
	switch v := raw.(type) {
	case nil:
	case []string:
		for _, k := range v {
			out = append(out, withDefaults(LayoutBlock{Key: k}))
			placed = append(placed, false)
		}
	case []LayoutBlock:
		for _, b := range v {
			out = append(out, withDefaults(b))
			placed = append(placed, b.X != 0 || b.Y != 0)
		}
	case []any:
		for _, item := range v {
			b, hasXY, ok := layoutBlockOf(item)
			if !ok {
				return nil, &WorkspaceError{i18n.M("err.block_shape")}
			}
			out = append(out, withDefaults(b))
			placed = append(placed, hasXY)
		}
	default:
		return nil, &WorkspaceError{i18n.M("err.block_shape")}
	}
	// 先丢掉已移除的区块，它们不该占格子
	kept := make([]LayoutBlock, 0, len(out))
	keptPlaced := make([]bool, 0, len(out))
	for i, b := range out {
		if !IsRetiredBlock(b.Key) {
			kept = append(kept, b)
			keptPlaced = append(keptPlaced, placed[i])
		}
	}
	return PlaceLayout(kept, keptPlaced), nil
}

// layoutBlockOf 解析一个 JSON 元素：字符串是区块键；对象要有 key 字符串，x / y / w / h 可省略、须是整数。
// 第二个返回值表示对象是否同时带了 x 与 y。
func layoutBlockOf(item any) (b LayoutBlock, hasXY bool, ok bool) {
	switch x := item.(type) {
	case string:
		return LayoutBlock{Key: x}, false, true
	case LayoutBlock:
		return x, x.X != 0 || x.Y != 0, true
	case map[string]any:
		key, _ := x["key"].(string)
		if key == "" {
			return LayoutBlock{}, false, false
		}
		b := LayoutBlock{Key: key}
		for field, dst := range map[string]*int{"x": &b.X, "y": &b.Y, "w": &b.W, "h": &b.H} {
			n, ok := layoutIntOf(x[field])
			if !ok {
				return LayoutBlock{}, false, false
			}
			*dst = n
		}
		return b, x["x"] != nil && x["y"] != nil, true
	}
	return LayoutBlock{}, false, false
}

// layoutIntOf 把 JSON 数字读成整数；缺省（nil）算 0；非整数或别的类型算形状不对。
func layoutIntOf(v any) (int, bool) {
	switch n := v.(type) {
	case nil:
		return 0, true
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

// ValidateLayout 检查一份布局：非空、只用系统区块键、不重复、宽度在 1 到 LayoutCols 之间且不窄于该区块的
// 最小宽度、高度在 1 到 LayoutMaxH 之间、坐标不为负且不越过右边界（X+W ≤ LayoutCols）。
// 调用前应先经 ParseLayout 补齐默认值与坐标。
func ValidateLayout(blocks []LayoutBlock) error {
	blocks = DropRetiredLayout(blocks)
	if len(blocks) == 0 {
		return &WorkspaceError{i18n.M("err.blocks_empty")}
	}
	seen := map[string]bool{}
	for _, b := range blocks {
		def, ok := BlockByKey(b.Key)
		if !ok {
			return &WorkspaceError{i18n.M("err.block_unknown", b.Key)}
		}
		if seen[b.Key] {
			return &WorkspaceError{i18n.M("err.block_duplicate", b.Key)}
		}
		seen[b.Key] = true
		if b.W < 1 || b.W > LayoutCols {
			return &WorkspaceError{i18n.M("err.block_width", def.Title, strconv.Itoa(def.MinW))}
		}
		if b.W < def.MinW {
			return &WorkspaceError{i18n.M("err.block_min_w", def.Title, widthName(def.MinW))}
		}
		if b.H < 1 || b.H > LayoutMaxH {
			return &WorkspaceError{i18n.M("err.block_height", def.Title)}
		}
		if b.X < 0 || b.Y < 0 || b.X+b.W > LayoutCols {
			return &WorkspaceError{i18n.M("err.block_bounds", def.Title)}
		}
	}
	return nil
}

// NormalizeLayout 是 ParseLayout 加 ValidateLayout 再 CompactLayout：接口层收到的 blocks 走这里，
// 得到可落库的布局——坐标齐全、合法、没有纵向空洞、按阅读顺序排列。
func NormalizeLayout(raw any) ([]LayoutBlock, error) {
	blocks, err := ParseLayout(raw)
	if err != nil {
		return nil, err
	}
	if err := ValidateLayout(blocks); err != nil {
		return nil, err
	}
	return CompactLayout(blocks), nil
}

// MergeLayouts 求多份布局的并集，按首次出现的顺序去重；坐标与宽高取首次出现的那份。
// 来自不同布局的区块可能撞在同一格，撞上的那块按 PlaceLayout 重新找位置，结果没有重叠。
func MergeLayouts(layouts [][]LayoutBlock) []LayoutBlock {
	out := []LayoutBlock{}
	placed := []bool{}
	seen := map[string]bool{}
	for _, l := range layouts {
		for _, b := range l {
			if seen[b.Key] {
				continue
			}
			seen[b.Key] = true
			out = append(out, b)
			placed = append(placed, true)
		}
	}
	return PlaceLayout(out, placed)
}

// DefaultPreset 是没有任何配置时的预设：组织负责人用全局视角，其他人用执行视角。
func DefaultPreset(isOwner bool) string {
	if isOwner {
		return PresetGlobal
	}
	return PresetDoer
}

// ResolveWorkspace 按顺序解析一个人的工作台：个人微调 → 各角色布局并集（并集后压实）→ 默认。
// roleLayouts 按成员的角色顺序传入（没有布局的角色不要传）。返回带坐标与宽高的区块与来源。
func ResolveWorkspace(personal []LayoutBlock, roleLayouts [][]LayoutBlock, isOwner bool) (blocks []LayoutBlock, source string) {
	// 已移除的区块（如 proposals）在旧布局里可能还在，读出来时静默丢掉
	if personal = DropRetiredLayout(personal); len(personal) > 0 {
		return personal, WorkspaceSourcePersonal
	}
	if merged := DropRetiredLayout(MergeLayouts(roleLayouts)); len(merged) > 0 {
		return CompactLayout(merged), WorkspaceSourceRoles
	}
	p, _ := PresetByKey(DefaultPreset(isOwner))
	return append([]LayoutBlock{}, p.Blocks...), WorkspaceSourceDefault
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
