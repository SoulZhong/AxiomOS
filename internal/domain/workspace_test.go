package domain

import (
	"reflect"
	"testing"

	"github.com/teemo/axiomos/internal/i18n"
)

func TestWorkspaceCatalogIsConsistent(t *testing.T) {
	if len(Blocks) != 11 {
		t.Fatalf("应有 11 个区块，实际 %d", len(Blocks))
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
	}
	if len(Presets) != 5 {
		t.Fatalf("应有 5 个预设，实际 %d", len(Presets))
	}
	for _, p := range Presets {
		if err := ValidateBlocks(p.Blocks); err != nil {
			t.Fatalf("预设 %s 的区块不合法: %v", p.Key, err)
		}
		if p.Title.In(i18n.EnUS) == "" || p.Description.In(i18n.ZhCN) == "" {
			t.Fatalf("预设 %s 缺少中英文案", p.Key)
		}
	}
}

func TestValidateBlocks(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		key  string // 期望的词条键；空表示合法
	}{
		{"合法", []string{"my_tasks", "events"}, ""},
		{"空", nil, "err.blocks_empty"},
		{"未知键", []string{"my_tasks", "dashboard"}, "err.block_unknown"},
		{"重复", []string{"my_tasks", "events", "my_tasks"}, "err.block_duplicate"},
	}
	for _, c := range cases {
		err := ValidateBlocks(c.keys)
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
		if we.Msg.Render(i18n.ZhCN) == c.key || we.Msg.Render(i18n.EnUS) == c.key {
			t.Fatalf("%s: 词条 %s 未翻译", c.name, c.key)
		}
	}
}

func TestMergeLayoutsKeepsFirstAppearanceOrder(t *testing.T) {
	got := MergeLayouts([][]string{
		{"my_tasks", "my_review", "events"},
		{"my_review", "team_load", "my_tasks", "sprint"},
		nil,
		{"events", "exceptions"},
	})
	want := []string{"my_tasks", "my_review", "events", "team_load", "sprint", "exceptions"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("并集顺序不对: %v", got)
	}
	if got := MergeLayouts(nil); len(got) != 0 || got == nil {
		t.Fatalf("空输入应返回空切片而非 nil: %#v", got)
	}
}

func TestResolveWorkspaceOrder(t *testing.T) {
	doer, _ := PresetByKey(PresetDoer)
	global, _ := PresetByKey(PresetGlobal)
	roles := [][]string{{"my_review", "team_load"}, {"my_tasks", "team_load"}}

	if b, src := ResolveWorkspace([]string{"events"}, roles, true); src != WorkspaceSourcePersonal || !reflect.DeepEqual(b, []string{"events"}) {
		t.Fatalf("个人微调应优先: %v %s", b, src)
	}
	if b, src := ResolveWorkspace(nil, roles, true); src != WorkspaceSourceRoles || !reflect.DeepEqual(b, []string{"my_review", "team_load", "my_tasks"}) {
		t.Fatalf("角色并集应次之: %v %s", b, src)
	}
	if b, src := ResolveWorkspace(nil, nil, true); src != WorkspaceSourceDefault || !reflect.DeepEqual(b, global.Blocks) {
		t.Fatalf("负责人默认应为全局视角: %v %s", b, src)
	}
	if b, src := ResolveWorkspace(nil, [][]string{nil}, false); src != WorkspaceSourceDefault || !reflect.DeepEqual(b, doer.Blocks) {
		t.Fatalf("其他人默认应为执行视角: %v %s", b, src)
	}
	// 返回值不能与预设共享底层数组
	b, _ := ResolveWorkspace(nil, nil, false)
	b[0] = "x"
	if doer.Blocks[0] == "x" {
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
