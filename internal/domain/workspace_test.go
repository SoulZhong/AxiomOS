package domain

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/i18n"
)

func TestWorkspaceCatalogIsConsistent(t *testing.T) {
	if len(Blocks) != 12 {
		t.Fatalf("应有 12 个区块，实际 %d", len(Blocks))
	}
	// 补记四：待我处理与组织概况是目录的前两项
	if Blocks[0].Key != "inbox" || Blocks[0].DefaultW != 12 || Blocks[0].DefaultH != 2 || Blocks[0].MinW != 6 || Blocks[0].Title.In(i18n.ZhCN) != "待我处理" || Blocks[0].Title.In(i18n.EnUS) != "For me" {
		t.Fatalf("目录第一项应是待我处理（12×2，最窄 6）: %+v", Blocks[0])
	}
	if Blocks[1].Key != "readouts" || Blocks[1].DefaultW != 12 || Blocks[1].DefaultH != 1 || Blocks[1].MinW != 6 || Blocks[1].Title.In(i18n.ZhCN) != "组织概况" || Blocks[1].Title.In(i18n.EnUS) != "Organization readouts" {
		t.Fatalf("目录第二项应是组织概况（12×1，最窄 6）: %+v", Blocks[1])
	}
	if _, ok := BlockByKey("proposals"); ok {
		t.Fatal("待确认操作已并入待我处理，目录里不应再有 proposals 区块")
	}
	for _, p := range Presets {
		if contains(LayoutKeys(p.Blocks), "proposals") {
			t.Fatalf("预设 %s 不应再含 proposals", p.Key)
		}
	}
	seen := map[string]bool{}
	for _, b := range Blocks {
		if seen[b.Key] {
			t.Fatalf("区块键 %s 重复", b.Key)
		}
		seen[b.Key] = true
		if b.Title.In(i18n.ZhCN) == "" || b.Title.In(i18n.EnUS) == "" || b.Description.In(i18n.EnUS) == "" {
			t.Fatalf("区块 %s 缺少中英文案", b.Key)
		}
		// 默认尺寸必须自己就合法：最小宽度 ≥ 1，默认宽度在最小宽度到 12 之间，高度在 1..LayoutMaxH
		if b.MinW < 1 || b.DefaultW < b.MinW || b.DefaultW > LayoutCols {
			t.Fatalf("区块 %s 的默认宽度 %d / 最小宽度 %d 不合法", b.Key, b.DefaultW, b.MinW)
		}
		if b.DefaultH < 1 || b.DefaultH > LayoutMaxH {
			t.Fatalf("区块 %s 的默认高度 %d 不合法", b.Key, b.DefaultH)
		}
		if !i18n.Has("width." + strconv.Itoa(b.MinW)) {
			t.Fatalf("最小宽度 %d 没有 width.N 词条", b.MinW)
		}
	}
	if len(Presets) != 5 {
		t.Fatalf("应有 5 个预设，实际 %d", len(Presets))
	}
	for _, p := range Presets {
		if err := ValidateLayout(p.Blocks); err != nil {
			t.Fatalf("预设 %s 的区块不合法: %v", p.Key, err)
		}
		if p.Title.In(i18n.EnUS) == "" || p.Description.In(i18n.ZhCN) == "" {
			t.Fatalf("预设 %s 缺少中英文案", p.Key)
		}
		// 预设自带坐标：互不重叠、已压实、按阅读顺序排列
		if LayoutOverlaps(p.Blocks) {
			t.Fatalf("预设 %s 的区块有重叠: %v", p.Key, p.Blocks)
		}
		if got := CompactLayout(p.Blocks); !reflect.DeepEqual(got, p.Blocks) {
			t.Fatalf("预设 %s 应已压实并按先 y 后 x 排列，压实后成了 %v", p.Key, got)
		}
		if p.Blocks[0].X != 0 || p.Blocks[0].Y != 0 {
			t.Fatalf("预设 %s 的第一块应在左上角: %+v", p.Key, p.Blocks[0])
		}
		// 补记四：每个预设以 inbox 开头；除执行视角外紧跟 readouts
		if p.Blocks[0] != (LayoutBlock{"inbox", 0, 0, 12, 2}) {
			t.Fatalf("预设 %s 应以待我处理开头: %+v", p.Key, p.Blocks[0])
		}
		if p.Key == PresetDoer {
			if contains(LayoutKeys(p.Blocks), "readouts") {
				t.Fatalf("执行视角不应带组织概况: %v", p.Blocks)
			}
		} else if p.Blocks[1] != (LayoutBlock{"readouts", 0, 2, 12, 1}) {
			t.Fatalf("预设 %s 的第二块应是组织概况: %+v", p.Key, p.Blocks[1])
		}
	}
	global, _ := PresetByKey(PresetGlobal)
	want := []LayoutBlock{{"inbox", 0, 0, 12, 2}, {"readouts", 0, 2, 12, 1}, {"overview_summary", 0, 3, 12, 1}, {"exceptions", 0, 4, 6, 2}, {"cost_budget", 6, 4, 6, 2}, {"trend", 0, 6, 12, 2}}
	if !reflect.DeepEqual(global.Blocks, want) {
		t.Fatalf("全局视角的坐标不符: %v", global.Blocks)
	}
	doer, _ := PresetByKey(PresetDoer)
	want = []LayoutBlock{{"inbox", 0, 0, 12, 2}, {"my_tasks", 0, 2, 12, 2}, {"my_review", 0, 4, 12, 2}, {"backlog", 0, 6, 6, 2}, {"sprint", 6, 6, 6, 2}, {"events", 0, 8, 12, 3}}
	if !reflect.DeepEqual(doer.Blocks, want) {
		t.Fatalf("执行视角的坐标不符: %v", doer.Blocks)
	}
}

// 三种输入形状都收：字符串数组、{key, w, h}、{key, x, y, w, h}，还可以混用。
func TestNormalizeLayoutAcceptsThreeShapes(t *testing.T) {
	// 最早的形状：字符串数组，宽高取默认，坐标按顺序致密排布
	got, err := NormalizeLayout([]string{"my_tasks", "events"})
	if err != nil {
		t.Fatal(err)
	}
	want := []LayoutBlock{{"my_tasks", 0, 0, 12, 2}, {"events", 0, 2, 6, 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("字符串数组应补默认宽高并排布: %+v", got)
	}
	// 补记二的形状：{key, w, h}，可以缺 w 或 h，也可以与字符串混用；坐标按顺序排布
	got, err = NormalizeLayout([]any{
		map[string]any{"key": "my_tasks", "w": float64(6), "h": float64(1)},
		"events",
		map[string]any{"key": "exceptions"},
		map[string]any{"key": "trend", "h": float64(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want = []LayoutBlock{{"my_tasks", 0, 0, 6, 1}, {"events", 6, 0, 6, 3}, {"exceptions", 0, 1, 6, 2}, {"trend", 0, 3, 12, 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("{key, w, h} 混合形状排布不对: %+v", got)
	}
	// 补记三的形状：{key, x, y, w, h}，坐标原样保留，结果按先 y 后 x 排列
	got, err = NormalizeLayout([]any{
		map[string]any{"key": "exceptions", "x": float64(6), "y": float64(0), "w": float64(6), "h": float64(2)},
		map[string]any{"key": "cost_budget", "x": float64(0), "y": float64(0), "w": float64(6), "h": float64(2)},
		map[string]any{"key": "trend", "x": float64(0), "y": float64(2), "w": float64(12), "h": float64(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want = []LayoutBlock{{"cost_budget", 0, 0, 6, 2}, {"exceptions", 6, 0, 6, 2}, {"trend", 0, 2, 12, 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("显式坐标应原样保留并按阅读顺序排列: %+v", got)
	}
	// 显式坐标里留了空洞：落库前压实
	got, err = NormalizeLayout([]any{map[string]any{"key": "trend", "x": float64(0), "y": float64(4), "w": float64(12), "h": float64(2)}})
	if err != nil || !reflect.DeepEqual(got, []LayoutBlock{{"trend", 0, 0, 12, 2}}) {
		t.Fatalf("有空洞的布局应被压实: %v %+v", err, got)
	}
	// 带坐标的与不带坐标的混用：不带的绕开带的
	got, err = NormalizeLayout([]any{
		map[string]any{"key": "sprint", "x": float64(6), "y": float64(0), "w": float64(6), "h": float64(2)},
		"my_tasks",
	})
	if err != nil || !reflect.DeepEqual(got, []LayoutBlock{{"sprint", 6, 0, 6, 2}, {"my_tasks", 0, 2, 12, 2}}) {
		t.Fatalf("缺坐标的区块应绕开已有坐标的区块: %v %+v", err, got)
	}
	// 只给 x 不给 y 算没有坐标
	got, err = NormalizeLayout([]any{map[string]any{"key": "sprint", "x": float64(6)}})
	if err != nil || !reflect.DeepEqual(got, []LayoutBlock{{"sprint", 0, 0, 6, 2}}) {
		t.Fatalf("只有 x 没有 y 应按无坐标排布: %v %+v", err, got)
	}
	// Go 侧直接给 []LayoutBlock：X、Y 都为 0 视为没有坐标
	got, err = NormalizeLayout([]LayoutBlock{{Key: "sprint"}, {Key: "backlog", W: 12}})
	if err != nil || !reflect.DeepEqual(got, []LayoutBlock{{"sprint", 0, 0, 6, 2}, {"backlog", 0, 2, 12, 2}}) {
		t.Fatalf("[]LayoutBlock 应补默认并排布: %v %+v", err, got)
	}
	// 已移除的区块丢掉，也不占格子
	got, err = NormalizeLayout([]any{"proposals", map[string]any{"key": "my_tasks"}})
	if err != nil || !reflect.DeepEqual(got, []LayoutBlock{{"my_tasks", 0, 0, 12, 2}}) {
		t.Fatalf("proposals 应被丢弃且不占位: %v %+v", err, got)
	}
	// 形状不对
	for _, bad := range []any{
		[]any{float64(3)},
		[]any{map[string]any{"w": float64(6)}},
		[]any{map[string]any{"key": "my_tasks", "w": "6"}},
		[]any{map[string]any{"key": "my_tasks", "w": 6.5}},
		[]any{map[string]any{"key": "my_tasks", "x": "0", "y": float64(0)}},
		"my_tasks",
	} {
		_, err := NormalizeLayout(bad)
		we, ok := err.(*WorkspaceError)
		if !ok || we.Msg.Key != "err.block_shape" {
			t.Fatalf("%v 应报 err.block_shape，实际 %v", bad, err)
		}
	}
	// nil 是空
	if got, err := ParseLayout(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nil 应解析成空切片: %v %#v", err, got)
	}
}

// 致密排布是确定的：同样的输入永远得到同样的坐标，从上到下、从左到右填第一个放得下的格子。
func TestPlaceLayoutIsDenseAndDeterministic(t *testing.T) {
	in := []LayoutBlock{{"exceptions", 0, 0, 6, 2}, {"cost_budget", 0, 0, 3, 1}, {"team_load", 0, 0, 3, 1}, {"trend", 0, 0, 12, 2}, {"sprint", 0, 0, 3, 1}}
	want := []LayoutBlock{{"exceptions", 0, 0, 6, 2}, {"cost_budget", 6, 0, 3, 1}, {"team_load", 9, 0, 3, 1}, {"trend", 0, 2, 12, 2}, {"sprint", 6, 1, 3, 1}}
	first := PlaceLayout(in, nil)
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("致密排布不对: %+v", first)
	}
	for i := 0; i < 5; i++ {
		if again := PlaceLayout(in, nil); !reflect.DeepEqual(again, first) {
			t.Fatalf("排布不确定: %+v vs %+v", again, first)
		}
	}
	if in[1].X != 0 || in[3].Y != 0 {
		t.Fatal("PlaceLayout 不应修改输入")
	}
	if LayoutOverlaps(first) {
		t.Fatalf("排布结果不应重叠: %+v", first)
	}
	// 带坐标但撞上别人的区块重新找位置；不撞的原地不动
	got := PlaceLayout([]LayoutBlock{{"exceptions", 0, 0, 6, 2}, {"cost_budget", 0, 0, 6, 2}, {"trend", 0, 2, 12, 2}}, []bool{true, true, true})
	if !reflect.DeepEqual(got, []LayoutBlock{{"exceptions", 0, 0, 6, 2}, {"cost_budget", 6, 0, 6, 2}, {"trend", 0, 2, 12, 2}}) {
		t.Fatalf("撞格的区块应重新排布: %+v", got)
	}
	// 坏尺寸不会让排布死循环（留给 ValidateLayout 报错）
	got = PlaceLayout([]LayoutBlock{{"events", 0, 0, 0, 0}, {"trend", 0, 0, 40, 9}}, nil)
	if len(got) != 2 || got[1].X != 0 {
		t.Fatalf("坏尺寸也要能排布: %+v", got)
	}
	// SizedBlocks 就是默认宽高加致密排布
	if got := SizedBlocks("overview_summary", "exceptions", "cost_budget"); !reflect.DeepEqual(got, []LayoutBlock{{"overview_summary", 0, 0, 12, 1}, {"exceptions", 0, 1, 6, 2}, {"cost_budget", 6, 1, 6, 2}}) {
		t.Fatalf("SizedBlocks 排布不对: %+v", got)
	}
}

// 纵向压实：每块尽量上移直到碰到别的区块，结果按先 y 后 x 稳定排序，没有纵向空洞。
func TestCompactLayout(t *testing.T) {
	in := []LayoutBlock{{"exceptions", 0, 3, 6, 2}, {"team_load", 6, 1, 6, 1}, {"cost_budget", 0, 0, 6, 1}}
	got := CompactLayout(in)
	want := []LayoutBlock{{"cost_budget", 0, 0, 6, 1}, {"team_load", 6, 0, 6, 1}, {"exceptions", 0, 1, 6, 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("压实不对: %+v", got)
	}
	if in[0].Y != 3 {
		t.Fatal("CompactLayout 不应修改输入")
	}
	// 上方被挡住就停在那里，不会绕到别的栏
	got = CompactLayout([]LayoutBlock{{"trend", 0, 0, 12, 2}, {"sprint", 6, 5, 6, 1}, {"backlog", 0, 2, 3, 3}})
	if !reflect.DeepEqual(got, []LayoutBlock{{"trend", 0, 0, 12, 2}, {"backlog", 0, 2, 3, 3}, {"sprint", 6, 2, 6, 1}}) {
		t.Fatalf("被挡住的区块应停在障碍下方: %+v", got)
	}
	// 已压实的布局再压实不变（幂等）
	if again := CompactLayout(got); !reflect.DeepEqual(again, got) {
		t.Fatalf("压实应幂等: %+v", again)
	}
	// 同一行按 x 排序
	got = CompactLayout([]LayoutBlock{{"team_load", 6, 0, 6, 1}, {"cost_budget", 0, 0, 6, 1}})
	if got[0].Key != "cost_budget" || got[1].Key != "team_load" {
		t.Fatalf("同一行应按 x 排序: %+v", got)
	}
	if got := CompactLayout(nil); got == nil || len(got) != 0 {
		t.Fatalf("空输入应返回空切片: %#v", got)
	}
}

func TestValidateLayout(t *testing.T) {
	cases := []struct {
		name   string
		blocks []LayoutBlock
		key    string // 期望的词条键；空表示合法
	}{
		{"合法", SizedBlocks("my_tasks", "events"), ""},
		{"任意整数宽高合法", []LayoutBlock{{"events", 0, 0, 5, 4}, {"exceptions", 5, 0, 7, 6}}, ""},
		{"最窄最矮合法", []LayoutBlock{{"my_tasks", 0, 0, 6, 1}, {"events", 6, 0, 3, 1}}, ""},
		{"贴着右边界合法", []LayoutBlock{{"events", 9, 0, 3, 1}}, ""},
		{"已移除的区块被忽略", SizedBlocks("my_tasks", "proposals"), ""},
		{"只有已移除的区块等于空", SizedBlocks("proposals"), "err.blocks_empty"},
		{"空", nil, "err.blocks_empty"},
		{"未知键", SizedBlocks("my_tasks", "dashboard"), "err.block_unknown"},
		{"重复", SizedBlocks("my_tasks", "events", "my_tasks"), "err.block_duplicate"},
		{"宽度超过 12", []LayoutBlock{{"events", 0, 0, 13, 2}}, "err.block_width"},
		{"宽度为零", []LayoutBlock{{"events", 0, 0, 0, 2}}, "err.block_width"},
		{"窄于最小宽度", []LayoutBlock{{"my_tasks", 0, 0, 4, 2}}, "err.block_min_w"},
		{"高度过高", []LayoutBlock{{"events", 0, 0, 6, 7}}, "err.block_height"},
		{"高度为零", []LayoutBlock{{"events", 0, 0, 6, 0}}, "err.block_height"},
		{"越过右边界", []LayoutBlock{{"events", 8, 0, 6, 2}}, "err.block_bounds"},
		{"x 为负", []LayoutBlock{{"events", -1, 0, 6, 2}}, "err.block_bounds"},
		{"y 为负", []LayoutBlock{{"events", 0, -1, 6, 2}}, "err.block_bounds"},
	}
	for _, c := range cases {
		err := ValidateLayout(c.blocks)
		if c.key == "" {
			if err != nil {
				t.Fatalf("%s: 不应报错，实际 %v", c.name, err)
			}
			continue
		}
		we, ok := err.(*WorkspaceError)
		if !ok || we.Msg.Key != c.key {
			t.Fatalf("%s: 应报 %s，实际 %v", c.name, c.key, err)
		}
		zh, en := we.Msg.Render(i18n.ZhCN), we.Msg.Render(i18n.EnUS)
		if zh == c.key || en == c.key || !strings.HasSuffix(zh, "。") || !strings.HasSuffix(en, ".") || strings.Contains(zh, "%!") || strings.Contains(en, "%!") {
			t.Fatalf("%s: 词条 %s 应是完整中英文句子，实际 %q / %q", c.name, c.key, zh, en)
		}
	}
	// 报错要说人话：区块标题按语言，宽度范围带该区块的最小宽度
	sentence := func(blocks []LayoutBlock) (string, string) {
		we := ValidateLayout(blocks).(*WorkspaceError)
		return we.Msg.Render(i18n.ZhCN), we.Msg.Render(i18n.EnUS)
	}
	if zh, en := sentence([]LayoutBlock{{"my_tasks", 0, 0, 4, 2}}); zh != "区块「我的任务」最窄只能占一半宽度。" || !strings.Contains(en, "My tasks") || !strings.Contains(en, "half the width") {
		t.Fatalf("最小宽度报错措辞不对: %q / %q", zh, en)
	}
	if zh, en := sentence([]LayoutBlock{{"my_tasks", 0, 0, 13, 2}}); zh != "区块「我的任务」的宽度要在 6 到 12 栏之间。" || en != "Block \"My tasks\" must be between 6 and 12 columns wide." {
		t.Fatalf("宽度报错措辞不对: %q / %q", zh, en)
	}
	if zh, en := sentence([]LayoutBlock{{"events", 0, 0, 6, 7}}); zh != "区块「最近动态」的高度要在 1 到 6 行之间。" || en != "Block \"Recent activity\" must be between 1 and 6 rows tall." {
		t.Fatalf("高度报错措辞不对: %q / %q", zh, en)
	}
	if zh, en := sentence([]LayoutBlock{{"events", 8, 0, 6, 2}}); zh != "区块「最近动态」超出了网格的右边界。" || en != "Block \"Recent activity\" extends past the right edge of the grid." {
		t.Fatalf("边界报错措辞不对: %q / %q", zh, en)
	}
}

// 并集按首次出现去重，坐标与宽高取首次出现的那份；来自不同布局而撞格的区块重新排布，结果没有重叠。
func TestMergeLayoutsKeepsFirstAppearanceAndAvoidsOverlaps(t *testing.T) {
	got := MergeLayouts([][]LayoutBlock{
		{{"my_tasks", 0, 0, 6, 1}, {"my_review", 0, 1, 12, 2}},
		{{"my_review", 6, 0, 6, 1}, {"team_load", 6, 0, 6, 2}, {"my_tasks", 0, 5, 12, 3}, {"sprint", 0, 3, 6, 2}},
		nil,
		{{"events", 6, 0, 6, 1}, {"exceptions", 0, 0, 6, 1}},
	})
	want := []LayoutBlock{{"my_tasks", 0, 0, 6, 1}, {"my_review", 0, 1, 12, 2}, {"team_load", 6, 3, 6, 2}, {"sprint", 0, 3, 6, 2}, {"events", 6, 0, 6, 1}, {"exceptions", 0, 5, 6, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("并集顺序、宽高或重排不对: %v", got)
	}
	if LayoutOverlaps(got) {
		t.Fatalf("并集不应有重叠: %v", got)
	}
	if got := MergeLayouts(nil); len(got) != 0 || got == nil {
		t.Fatalf("空输入应返回空切片而非 nil: %#v", got)
	}
	// 任意两个预设并集都不重叠
	for _, p := range Presets {
		for _, q := range Presets {
			if m := MergeLayouts([][]LayoutBlock{p.Blocks, q.Blocks}); LayoutOverlaps(m) {
				t.Fatalf("预设 %s + %s 并集有重叠: %v", p.Key, q.Key, m)
			}
		}
	}
}

func TestResolveWorkspaceOrder(t *testing.T) {
	doer, _ := PresetByKey(PresetDoer)
	global, _ := PresetByKey(PresetGlobal)
	roles := [][]LayoutBlock{SizedBlocks("my_review", "team_load"), SizedBlocks("my_tasks", "team_load")}

	if b, src := ResolveWorkspace([]LayoutBlock{{"events", 0, 0, 12, 1}}, roles, true); src != WorkspaceSourcePersonal || !reflect.DeepEqual(b, []LayoutBlock{{"events", 0, 0, 12, 1}}) {
		t.Fatalf("个人微调应优先并保留坐标宽高: %v %s", b, src)
	}
	// 角色并集：撞格的 my_tasks 重新排到下方，结果已压实
	wantRoles := []LayoutBlock{{"my_review", 0, 0, 12, 2}, {"team_load", 0, 2, 6, 2}, {"my_tasks", 0, 4, 12, 2}}
	if b, src := ResolveWorkspace(nil, roles, true); src != WorkspaceSourceRoles || !reflect.DeepEqual(b, wantRoles) {
		t.Fatalf("角色并集应次之且无重叠: %v %s", b, src)
	}
	if b, src := ResolveWorkspace(nil, nil, true); src != WorkspaceSourceDefault || !reflect.DeepEqual(b, global.Blocks) {
		t.Fatalf("负责人默认应为全局视角: %v %s", b, src)
	}
	if b, src := ResolveWorkspace(nil, [][]LayoutBlock{nil}, false); src != WorkspaceSourceDefault || !reflect.DeepEqual(b, doer.Blocks) {
		t.Fatalf("其他人默认应为执行视角: %v %s", b, src)
	}
	// 返回值不能与预设共享底层数组
	b, _ := ResolveWorkspace(nil, nil, false)
	b[0].Key = "x"
	if doer.Blocks[0].Key == "x" {
		t.Fatal("解析结果不应修改预设本身")
	}
}

func TestBuiltinRolePreset(t *testing.T) {
	want := map[string]string{"admin": PresetGlobal, "ops": PresetOps, "workflow_admin": PresetOps, "pm": PresetDoer, "designer": PresetDoer, "developer": PresetDoer, "tester": PresetDoer, "releaser": PresetDoer, "analyst": ""}
	for role, p := range want {
		if got := BuiltinRolePreset(role); got != p {
			t.Fatalf("%s 应映射到 %q，实际 %q", role, p, got)
		}
	}
	for _, r := range BuiltinRoles {
		if BuiltinRolePreset(r.Name) == "" {
			t.Fatalf("内置角色 %s 没有默认预设", r.Name)
		}
	}
}

// 旧布局里的 proposals（已并入待我处理）解析时静默丢弃；只剩它时等于没有布局。
func TestResolveWorkspaceDropsRetiredBlocks(t *testing.T) {
	if b, src := ResolveWorkspace(SizedBlocks("proposals", "my_tasks", "events"), nil, false); src != WorkspaceSourcePersonal || !reflect.DeepEqual(LayoutKeys(b), []string{"my_tasks", "events"}) {
		t.Fatalf("个人布局里的 proposals 应被丢弃: %v %s", b, src)
	}
	if b, src := ResolveWorkspace(nil, [][]LayoutBlock{SizedBlocks("proposals", "my_review"), SizedBlocks("proposals")}, false); src != WorkspaceSourceRoles || !reflect.DeepEqual(LayoutKeys(b), []string{"my_review"}) {
		t.Fatalf("角色布局里的 proposals 应被丢弃: %v %s", b, src)
	}
	doer, _ := PresetByKey(PresetDoer)
	if b, src := ResolveWorkspace(SizedBlocks("proposals"), nil, false); src != WorkspaceSourceDefault || !reflect.DeepEqual(b, doer.Blocks) {
		t.Fatalf("只有 proposals 的个人布局应视为没有布局: %v %s", b, src)
	}
	if got := DropRetiredBlocks(nil); got == nil || len(got) != 0 {
		t.Fatalf("空输入应返回空切片: %#v", got)
	}
	if got := DropRetiredLayout(nil); got == nil || len(got) != 0 {
		t.Fatalf("空输入应返回空切片: %#v", got)
	}
}
