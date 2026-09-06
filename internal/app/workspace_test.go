package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 工作台测试夹具：负责人（无角色）、一线成员（开发 + 测试）、只有自定义角色的成员。
type wsFixture struct {
	orgID              string
	owner, dev, custom *Session
	devMember          *domain.Member
}

func newWorkspaceOrg(t *testing.T, a *App, ctx context.Context) *wsFixture {
	t.Helper()
	slug := "ws-" + store.NewID("x")[2:10]
	org, err := a.Store.CreateOrganization(ctx, a.Store.Pool, slug, "工作台测试组织", "CNY")
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := HashPassword("x")
	f := &wsFixture{orgID: org.ID}
	var ownerM, devM, customM *domain.Member
	if err := a.Store.WithOrg(ctx, org.ID, func(tx pgx.Tx) error {
		mk := func(suffix, name string, roles []string) *domain.Member {
			acc, err := a.Store.CreateAccount(ctx, tx, slug+"-"+suffix+"@t.local", pw, name)
			if err != nil {
				t.Fatal(err)
			}
			m, err := a.Store.CreateMember(ctx, tx, org.ID, acc.ID, name, roles)
			if err != nil {
				t.Fatal(err)
			}
			return m
		}
		ownerM = mk("owner", "老板", nil)
		devM = mk("dev", "开发", []string{"developer", "tester"})
		customM = mk("custom", "分析师", []string{"analyst"})
		return a.Store.SetOrganizationOwner(ctx, tx, org.ID, ownerM.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureOrgDefaults(ctx, org.ID); err != nil {
		t.Fatal(err)
	}
	f.devMember = devM
	f.owner = fullSession(ctx, t, a, org.ID, ownerM.ID)
	f.dev = fullSession(ctx, t, a, org.ID, devM.ID)
	f.custom = fullSession(ctx, t, a, org.ID, customM.ID)
	if _, err := a.SaveRole(ctx, f.owner, "analyst", i18n.T("分析师", "Analyst"), nil); err != nil {
		t.Fatal(err)
	}
	return f
}

// fullSession 像登录一样补齐负责人身份、权限与范围。
func fullSession(ctx context.Context, t *testing.T, a *App, orgID, memberID string) *Session {
	t.Helper()
	var s *Session
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		m, err := a.Store.MemberByID(ctx, tx, memberID)
		if err != nil {
			return err
		}
		s = &Session{OrgID: orgID, MemberID: memberID, Actor: &domain.Executor{ID: m.ID, Kind: domain.ExecutorMember, Name: m.Name, Roles: m.Roles}}
		return a.fillSession(ctx, tx, s, m)
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func keysOf(ws *Workspace) []string {
	out := make([]string, 0, len(ws.Blocks))
	for _, b := range ws.Blocks {
		out = append(out, b.Key)
	}
	return out
}

func presetBlocks(t *testing.T, key string) []domain.LayoutBlock {
	p, ok := domain.PresetByKey(key)
	if !ok {
		t.Fatalf("预设 %s 不存在", key)
	}
	return p.Blocks
}

func presetKeys(t *testing.T, key string) []string { return domain.LayoutKeys(presetBlocks(t, key)) }

func sizesOf(ws *Workspace) []domain.LayoutBlock {
	out := make([]domain.LayoutBlock, 0, len(ws.Blocks))
	for _, b := range ws.Blocks {
		out = append(out, domain.LayoutBlock{Key: b.Key, X: b.X, Y: b.Y, W: b.W, H: b.H})
	}
	return out
}

func TestWorkspaceSeedMapsBuiltinRolesOnce(t *testing.T) {
	a, ctx := testApp(t)
	f := newWorkspaceOrg(t, a, ctx)

	layouts, err := a.OrgWorkspaceLayouts(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]RoleLayoutView{}
	for _, l := range layouts {
		got[l.Role] = l
	}
	want := map[string]string{"admin": "global", "ops": "ops", "workflow_admin": "ops", "pm": "doer", "designer": "doer", "developer": "doer", "tester": "doer", "releaser": "doer"}
	for role, preset := range want {
		l, ok := got[role]
		if !ok || l.Preset == nil || *l.Preset != preset {
			t.Fatalf("内置角色 %s 应套用预设 %s，实际 %+v", role, preset, l)
		}
		if !reflect.DeepEqual(l.Blocks, presetBlocks(t, preset)) {
			t.Fatalf("%s 的区块应与预设一致: %v", role, l.Blocks)
		}
	}
	// 自定义角色没有布局：区块是默认执行视角，preset 为 null，成员数是 1
	an := got["analyst"]
	if an.Preset != nil || !reflect.DeepEqual(an.Blocks, presetBlocks(t, "doer")) || an.MemberCount != 1 || an.RoleTitle != "分析师" {
		t.Fatalf("未配置角色的视图不符: %+v", an)
	}
	if got["developer"].MemberCount != 1 || got["tester"].MemberCount != 1 || got["admin"].MemberCount != 0 {
		t.Fatalf("成员数不符: dev=%d tester=%d admin=%d", got["developer"].MemberCount, got["tester"].MemberCount, got["admin"].MemberCount)
	}

	// 组织改过的布局，再跑一次默认值补齐不能被覆盖
	if _, err := a.SetRoleWorkspace(ctx, f.owner, "developer", nil, "team"); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureOrgDefaults(ctx, f.orgID); err != nil {
		t.Fatal(err)
	}
	layouts, _ = a.OrgWorkspaceLayouts(ctx, f.owner)
	for _, l := range layouts {
		if l.Role == "developer" && (l.Preset == nil || *l.Preset != "team") {
			t.Fatalf("EnsureOrgDefaults 不应覆盖已有布局: %+v", l)
		}
	}
}

func TestWorkspaceResolutionOrder(t *testing.T) {
	a, ctx := testApp(t)
	f := newWorkspaceOrg(t, a, ctx)

	// 负责人没有角色、没有配置 → 默认全局视角
	ws, err := a.MyWorkspace(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Source != "default" || !reflect.DeepEqual(sizesOf(ws), presetBlocks(t, "global")) || len(ws.RolesUsed) != 0 || !ws.CanCustomize {
		t.Fatalf("负责人默认应为全局视角并带预设宽高: %+v", ws)
	}
	if ws.Blocks[0].Title != "待我处理" || ws.Blocks[1].Title != "组织概况" {
		t.Fatalf("区块标题应按语言解析，实际 %q / %q", ws.Blocks[0].Title, ws.Blocks[1].Title)
	}

	// 只有自定义角色（无布局）的普通成员 → 默认执行视角
	ws, err = a.MyWorkspace(ctx, f.custom)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Source != "default" || !reflect.DeepEqual(keysOf(ws), presetKeys(t, "doer")) {
		t.Fatalf("普通成员默认应为执行视角: %+v", ws)
	}

	// 两个角色：开发 → 执行视角（种子），测试 → 改成小组视角；并集按成员角色顺序、首次出现去重
	if _, err := a.SetRoleWorkspace(ctx, f.owner, "tester", nil, "team"); err != nil {
		t.Fatal(err)
	}
	ws, err = a.MyWorkspace(ctx, f.dev)
	if err != nil {
		t.Fatal(err)
	}
	wantMerged := domain.LayoutKeys(domain.CompactLayout(domain.MergeLayouts([][]domain.LayoutBlock{presetBlocks(t, "doer"), presetBlocks(t, "team")})))
	if ws.Source != "roles" || !reflect.DeepEqual(keysOf(ws), wantMerged) || !reflect.DeepEqual(ws.RolesUsed, []string{"developer", "tester"}) {
		t.Fatalf("两个角色应并集: %+v（期望 %v）", ws, wantMerged)
	}
	if domain.LayoutOverlaps(sizesOf(ws)) {
		t.Fatalf("并集后的工作台不应有重叠: %+v", ws.Blocks)
	}

	// 逐块配置一个角色，并校验产生了动态
	if _, err := a.SetRoleWorkspace(ctx, f.owner, "developer", []string{"events", "my_tasks"}, ""); err != nil {
		t.Fatal(err)
	}
	ws, _ = a.MyWorkspace(ctx, f.dev)
	// 逐块配置时缺坐标：按顺序致密排布，events 在左上角、my_tasks 紧贴其下（测试角色的区块绕开它们）
	if got := sizesOf(ws); got[0] != (domain.LayoutBlock{Key: "events", X: 0, Y: 0, W: 6, H: 3}) || !contains(keysOf(ws), "my_tasks") || domain.LayoutOverlaps(got) {
		t.Fatalf("角色布局应按顺序排布且无重叠: %v", got)
	}
	for _, b := range ws.Blocks {
		if b.Key == "my_tasks" && (b.X != 0 || b.Y != 3 || b.W != 12) {
			t.Fatalf("my_tasks 应紧贴 events 之下: %+v", b)
		}
	}
	var evTypes []string
	if err := a.Store.WithOrg(ctx, f.orgID, func(tx pgx.Tx) error {
		rows, err := a.Store.ListEvents(ctx, tx, "", 20)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.Type == "WorkspaceLayoutUpdated" {
				evTypes = append(evTypes, r.Type)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(evTypes) < 2 {
		t.Fatalf("每次配置角色布局都应产生动态，实际 %d 条", len(evTypes))
	}

	// 个人微调压过角色
	ws, err = a.SetMyWorkspace(ctx, f.dev, []string{"sprint", "my_tasks"})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Source != "personal" || !reflect.DeepEqual(keysOf(ws), []string{"sprint", "my_tasks"}) || len(ws.RolesUsed) != 0 {
		t.Fatalf("个人微调应优先: %+v", ws)
	}
	// 清除个人微调回到角色
	ws, err = a.ClearMyWorkspace(ctx, f.dev)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Source != "roles" || keysOf(ws)[0] != "events" {
		t.Fatalf("清除后应回到角色布局: %+v", ws)
	}
	// 清除角色布局后回到默认
	if _, err := a.ClearRoleWorkspace(ctx, f.owner, "developer"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ClearRoleWorkspace(ctx, f.owner, "tester"); err != nil {
		t.Fatal(err)
	}
	ws, _ = a.MyWorkspace(ctx, f.dev)
	if ws.Source != "default" || !reflect.DeepEqual(keysOf(ws), presetKeys(t, "doer")) {
		t.Fatalf("角色布局清空后应回到默认执行视角: %+v", ws)
	}
}

// 区块宽高（ADR 0015 补记二）：两种输入形状都收，尺寸落库后原样读回，角色并集时宽高取首次出现的那份，
// 旧的字符串数组行按目录默认补齐。
func TestWorkspaceBlockSizesRoundTrip(t *testing.T) {
	a, ctx := testApp(t)
	f := newWorkspaceOrg(t, a, ctx)

	// 个人微调：混合形状（对象 + 字符串 + 缺 w/h），像 JSON 解出来那样
	ws, err := a.SetMyWorkspace(ctx, f.dev, []any{
		map[string]any{"key": "my_tasks", "w": float64(6), "h": float64(3)},
		"events",
		map[string]any{"key": "sprint", "h": float64(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.LayoutBlock{{Key: "my_tasks", X: 0, Y: 0, W: 6, H: 3}, {Key: "events", X: 6, Y: 0, W: 6, H: 3}, {Key: "sprint", X: 0, Y: 3, W: 6, H: 1}}
	if ws.Source != "personal" || !reflect.DeepEqual(sizesOf(ws), want) {
		t.Fatalf("个人微调的宽高应原样回来、缺省补默认、缺坐标的致密排布: %+v", ws.Blocks)
	}
	if ws.Blocks[0].Title != "我的任务" {
		t.Fatalf("带宽高的区块仍应有标题: %+v", ws.Blocks[0])
	}
	// 再读一次，落库的就是对象形状
	ws, _ = a.MyWorkspace(ctx, f.dev)
	if !reflect.DeepEqual(sizesOf(ws), want) {
		t.Fatalf("重新读取后宽高应一致: %+v", ws.Blocks)
	}

	// 角色布局带尺寸；并集时宽高取首次出现的那份（开发在测试前面）
	if _, err := a.ClearMyWorkspace(ctx, f.dev); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetRoleWorkspace(ctx, f.owner, "developer", []domain.LayoutBlock{{Key: "events", W: 12, H: 1}, {Key: "my_tasks"}}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetRoleWorkspace(ctx, f.owner, "tester", []any{map[string]any{"key": "events", "w": float64(4), "h": float64(3)}, "backlog"}, ""); err != nil {
		t.Fatal(err)
	}
	ws, err = a.MyWorkspace(ctx, f.dev)
	if err != nil {
		t.Fatal(err)
	}
	want = []domain.LayoutBlock{{Key: "events", X: 0, Y: 0, W: 12, H: 1}, {Key: "my_tasks", X: 0, Y: 1, W: 12, H: 2}, {Key: "backlog", X: 0, Y: 3, W: 6, H: 2}}
	if ws.Source != "roles" || !reflect.DeepEqual(sizesOf(ws), want) {
		t.Fatalf("角色并集应取首次出现的宽高，撞格的重排到下方: %+v", ws.Blocks)
	}
	views, err := a.OrgWorkspaceLayouts(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.Role == "tester" && !reflect.DeepEqual(v.Blocks, []domain.LayoutBlock{{Key: "events", X: 0, Y: 0, W: 4, H: 3}, {Key: "backlog", X: 4, Y: 0, W: 6, H: 2}}) {
			t.Fatalf("角色布局视图应带坐标与宽高: %+v", v.Blocks)
		}
	}

	// 旧行：直接写字符串数组，读出来按目录默认补齐宽高
	if err := a.Store.WithOrg(ctx, f.orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update workspace_layouts set blocks='["my_review","proposals","team_load"]' where role_name='developer'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ws, _ = a.MyWorkspace(ctx, f.dev)
	if got := sizesOf(ws)[:2]; !reflect.DeepEqual(got, []domain.LayoutBlock{{Key: "my_review", X: 0, Y: 0, W: 12, H: 2}, {Key: "team_load", X: 0, Y: 2, W: 6, H: 2}}) {
		t.Fatalf("旧的字符串数组行应按默认宽高解析、按顺序排布并丢掉 proposals: %+v", ws.Blocks)
	}
	if domain.LayoutOverlaps(sizesOf(ws)) {
		t.Fatalf("旧行与另一角色并集后不应重叠: %+v", ws.Blocks)
	}
	// 补记二的 {key, w, h} 行：缺 x / y，读出来按顺序排布
	if err := a.Store.WithOrg(ctx, f.orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update workspace_layouts set blocks='[{"key":"exceptions","w":6,"h":2},{"key":"cost_budget","w":6,"h":2}]' where role_name='developer'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ws, _ = a.MyWorkspace(ctx, f.dev)
	if got := sizesOf(ws)[:2]; !reflect.DeepEqual(got, []domain.LayoutBlock{{Key: "exceptions", X: 0, Y: 0, W: 6, H: 2}, {Key: "cost_budget", X: 6, Y: 0, W: 6, H: 2}}) {
		t.Fatalf("{key, w, h} 行应按顺序致密排布: %+v", ws.Blocks)
	}

	// 显式坐标原样往返；留了空洞的落库前压实
	ws, err = a.SetMyWorkspace(ctx, f.dev, []any{
		map[string]any{"key": "trend", "x": float64(0), "y": float64(5), "w": float64(12), "h": float64(2)},
		map[string]any{"key": "exceptions", "x": float64(6), "y": float64(0), "w": float64(5), "h": float64(4)},
		map[string]any{"key": "sprint", "x": float64(0), "y": float64(0), "w": float64(6), "h": float64(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want = []domain.LayoutBlock{{Key: "sprint", X: 0, Y: 0, W: 6, H: 2}, {Key: "exceptions", X: 6, Y: 0, W: 5, H: 4}, {Key: "trend", X: 0, Y: 4, W: 12, H: 2}}
	if !reflect.DeepEqual(sizesOf(ws), want) {
		t.Fatalf("显式坐标应按阅读顺序原样回来、任意宽高被接受: %+v", ws.Blocks)
	}
	ws, _ = a.MyWorkspace(ctx, f.dev)
	if !reflect.DeepEqual(sizesOf(ws), want) {
		t.Fatalf("重新读取后坐标应一致: %+v", ws.Blocks)
	}
	if _, err := a.ClearMyWorkspace(ctx, f.dev); err != nil {
		t.Fatal(err)
	}

	// 目录带默认尺寸与最小宽度，预设带宽高
	cat := a.WorkspaceCatalog(f.dev)
	for _, b := range cat.Blocks {
		if b.DefaultW == 0 || b.DefaultH == 0 || b.MinW == 0 {
			t.Fatalf("目录区块 %s 应声明 default_w/default_h/min_w: %+v", b.Key, b)
		}
		if b.Key == "my_tasks" && (b.DefaultW != 12 || b.DefaultH != 2 || b.MinW != 6) {
			t.Fatalf("我的任务的默认尺寸不符: %+v", b)
		}
	}
	for _, p := range cat.Presets {
		if !reflect.DeepEqual(p.Blocks, presetBlocks(t, p.Key)) {
			t.Fatalf("预设 %s 应带宽高: %+v", p.Key, p.Blocks)
		}
	}
}

// 宽高不合法要被拒，理由是完整的中英文句子。
func TestWorkspaceRejectsBadSizes(t *testing.T) {
	a, ctx := testApp(t)
	f := newWorkspaceOrg(t, a, ctx)

	_, err := a.SetMyWorkspace(ctx, f.dev, []any{map[string]any{"key": "my_tasks", "w": float64(4)}})
	ue, ok := err.(*UserError)
	if !ok || ue.Status != 400 {
		t.Fatalf("窄于最小宽度应被拒（400），实际 %v", err)
	}
	if zh := ue.Render(i18n.ZhCN); zh != "区块「我的任务」最窄只能占一半宽度。" {
		t.Fatalf("最小宽度的拒绝理由不对: %q", zh)
	}
	if en := ue.Render(i18n.EnUS); !strings.HasPrefix(en, "Block \"My tasks\"") || !strings.HasSuffix(en, ".") {
		t.Fatalf("最小宽度的英文理由不对: %q", en)
	}
	// 越过右边界：理由说的是边界
	_, err = a.SetMyWorkspace(ctx, f.dev, []any{map[string]any{"key": "events", "x": float64(8), "y": float64(0), "w": float64(6), "h": float64(2)}})
	if ue, ok := err.(*UserError); !ok || ue.Status != 400 || ue.Render(i18n.ZhCN) != "区块「最近动态」超出了网格的右边界。" {
		t.Fatalf("越过右边界应被拒且说明边界，实际 %v", err)
	}
	for name, bad := range map[string]any{
		"宽度超过 12": []any{map[string]any{"key": "events", "w": float64(13)}},
		"高度过高":    []any{map[string]any{"key": "events", "h": float64(7)}},
		"形状不对":    []any{map[string]any{"w": float64(6)}},
		"w 不是整数":  []any{map[string]any{"key": "events", "w": "6"}},
		"x 不是整数":  []any{map[string]any{"key": "events", "x": 1.5, "y": float64(0)}},
	} {
		_, err := a.SetMyWorkspace(ctx, f.dev, bad)
		ue, ok := err.(*UserError)
		if !ok || ue.Status != 400 {
			t.Fatalf("%s 应被拒（400），实际 %v", name, err)
		}
		if zh, en := ue.Render(i18n.ZhCN), ue.Render(i18n.EnUS); !strings.HasSuffix(zh, "。") || strings.HasPrefix(en, "err.") || !strings.HasSuffix(en, ".") {
			t.Fatalf("%s 的理由应是完整中英文句子: %q / %q", name, zh, en)
		}
	}
	// 角色布局走同一套校验
	if _, err := a.SetRoleWorkspace(ctx, f.owner, "developer", []any{map[string]any{"key": "trend", "w": float64(3)}}, ""); err == nil {
		t.Fatal("角色布局窄于最小宽度应被拒")
	}
	// 被拒的写入不留痕：布局还是原来的
	ws, _ := a.MyWorkspace(ctx, f.dev)
	if ws.Source != "roles" {
		t.Fatalf("被拒的微调不应落库: %+v", ws)
	}
}

func TestWorkspaceRejectsAgentsAndBadInput(t *testing.T) {
	a, ctx := testApp(t)
	f := newWorkspaceOrg(t, a, ctx)

	agent := *f.dev
	agent.AgentID = "agt_fake"
	if _, err := a.MyWorkspace(ctx, &agent); err == nil {
		t.Fatal("Agent 不应有工作台")
	} else if ue, ok := err.(*UserError); !ok || ue.Status != 403 || !strings.HasSuffix(ue.Render(i18n.ZhCN), "。") {
		t.Fatalf("应是 403 且是完整中文句子，实际 %v", err)
	}
	if _, err := a.SetMyWorkspace(ctx, &agent, []string{"my_tasks"}); err == nil {
		t.Fatal("Agent 不应能微调工作台")
	}

	// 非法区块键、重复、空
	for _, bad := range [][]string{{"my_tasks", "dashboard"}, {"my_tasks", "my_tasks"}, {}} {
		_, err := a.SetMyWorkspace(ctx, f.dev, bad)
		ue, ok := err.(*UserError)
		if !ok || ue.Status != 400 {
			t.Fatalf("区块 %v 应被拒（400），实际 %v", bad, err)
		}
		if en := ue.Render(i18n.EnUS); strings.HasPrefix(en, "err.") {
			t.Fatalf("错误应有英文翻译，实际 %q", en)
		}
	}
	if _, err := a.SetRoleWorkspace(ctx, f.owner, "developer", nil, "cockpit"); err == nil {
		t.Fatal("未知预设应被拒")
	}
	if _, err := a.SetRoleWorkspace(ctx, f.owner, "developer", []string{"my_tasks"}, "doer"); err == nil {
		t.Fatal("同时给 blocks 与 preset 应被拒")
	}
	if _, err := a.SetRoleWorkspace(ctx, f.owner, "nobody", nil, "doer"); err == nil {
		t.Fatal("不存在的角色应被拒")
	} else if ue, ok := err.(*UserError); !ok || ue.Status != 404 {
		t.Fatalf("不存在的角色应是 404，实际 %v", err)
	}

	// 目录按语言
	cat := a.WorkspaceCatalog(f.dev)
	for _, b := range cat.Blocks {
		if b.Key == "proposals" {
			t.Fatal("目录不应再提供 proposals 区块（已并入待我处理）")
		}
	}
	if len(cat.Blocks) != 12 || len(cat.Presets) != 5 || cat.Blocks[0].Title != "待我处理" || cat.Blocks[1].Title != "组织概况" || cat.Presets[0].Description == "" {
		t.Fatalf("目录不符: %d 区块 %d 预设 %+v", len(cat.Blocks), len(cat.Presets), cat.Blocks[0])
	}
	if cat.Blocks[0].Key != "inbox" || cat.Blocks[0].DefaultW != 12 || cat.Blocks[0].DefaultH != 2 || cat.Blocks[0].MinW != 6 || cat.Blocks[1].Key != "readouts" || cat.Blocks[1].DefaultH != 1 {
		t.Fatalf("目录前两项应是待我处理与组织概况: %+v %+v", cat.Blocks[0], cat.Blocks[1])
	}
	en := *f.dev
	en.Locale = i18n.EnUS
	if a.WorkspaceCatalog(&en).Blocks[0].Title != "For me" || a.WorkspaceCatalog(&en).Blocks[1].Title != "Organization readouts" {
		t.Fatal("目录标题应按语言")
	}
}

func TestOrgWorkspaceRequiresPermission(t *testing.T) {
	a, ctx := testApp(t)
	f := newWorkspaceOrg(t, a, ctx)

	if _, err := a.OrgWorkspaceLayouts(ctx, f.dev); err == nil {
		t.Fatal("没有「组织设置」权限不应能看各角色布局")
	}
	if _, err := a.SetRoleWorkspace(ctx, f.dev, "developer", nil, "team"); err == nil {
		t.Fatal("没有「组织设置」权限不应能配角色布局")
	}
	if _, err := a.ClearRoleWorkspace(ctx, f.dev, "developer"); err == nil {
		t.Fatal("没有「组织设置」权限不应能清角色布局")
	}
	// 给自定义角色加上 org_settings 后就可以
	if _, err := a.SaveRole(ctx, f.owner, "analyst", i18n.T("分析师", "Analyst"), []string{"org_settings"}); err != nil {
		t.Fatal(err)
	}
	custom := fullSession(ctx, t, a, f.orgID, f.custom.MemberID)
	v, err := a.SetRoleWorkspace(ctx, custom, "analyst", nil, "unit")
	if err != nil {
		t.Fatal(err)
	}
	if v.Preset == nil || *v.Preset != "unit" || v.MemberCount != 1 || !reflect.DeepEqual(v.Blocks, presetBlocks(t, "unit")) {
		t.Fatalf("角色布局返回值不符: %+v", v)
	}
	ws, _ := a.MyWorkspace(ctx, custom)
	if ws.Source != "roles" || !reflect.DeepEqual(ws.RolesUsed, []string{"analyst"}) {
		t.Fatalf("分析师应看到自己角色的布局: %+v", ws)
	}
	// 删除角色时连带清掉它的布局
	if _, err := a.UpdateMember(ctx, f.owner, f.custom.MemberID, MemberPatch{Roles: []string{}}); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteRole(ctx, f.owner, "analyst"); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.WithOrg(ctx, f.orgID, func(tx pgx.Tx) error {
		_, err := a.Store.RoleLayout(ctx, tx, "analyst")
		if err != store.ErrNotFound {
			return errOrMsg(err, "删除角色后布局应一并删除")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func errOrMsg(err error, msg string) error {
	if err != nil {
		return err
	}
	return &UserError{Status: 500, Reasons: []i18n.Msg{i18n.M(msg)}}
}

// 管理员明确清掉内置角色的布局后，重启时的默认补齐不能把预设填回来（空布局是"明确清除"的记录）。
func TestClearedRoleLayoutSurvivesDefaults(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, _ := newTestOrg(t, a, ctx)
	if _, err := a.ClearRoleWorkspace(ctx, jia, "developer"); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureOrgDefaults(ctx, orgID); err != nil {
		t.Fatal(err)
	}
	rows, err := a.OrgWorkspaceLayouts(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Role == "developer" && r.Preset != nil {
			t.Fatalf("清除后的内置角色布局被默认补齐重新填回：%+v", r)
		}
	}
}

// 迁移 0010（ADR 0015 补记四）：已存的三种形状的布局都在最上方补上 inbox 与 readouts，其余区块下移；
// 套用执行视角的角色行只补 inbox；空布局（明确清除）与已含 inbox 的行不动；重复执行不变。
func TestMigration0010PrependsInboxAndReadouts(t *testing.T) {
	a, ctx := testApp(t)
	f := newWorkspaceOrg(t, a, ctx)

	// 造出迁移前的旧行：字符串数组（含已移除的 proposals）、{key, w, h}、{key, x, y, w, h}、明确清除的空数组
	if err := a.Store.WithOrg(ctx, f.orgID, func(tx pgx.Tx) error {
		for _, q := range []string{
			`update workspace_layouts set blocks='["my_tasks","my_review","proposals","events"]', preset='doer' where role_name='developer'`,
			`update workspace_layouts set blocks='[{"key":"exceptions","w":6,"h":2},{"key":"cost_budget","w":6,"h":2}]', preset=null where role_name='tester'`,
			`update workspace_layouts set blocks='[]', preset=null where role_name='admin'`,
		} {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `insert into workspace_layouts(org_id,member_id,blocks) values($1,$2,'[{"key":"sprint","x":6,"y":0,"w":6,"h":2},{"key":"trend","x":0,"y":2,"w":12,"h":2}]')`, f.orgID, f.dev.MemberID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	before, err := a.OrgWorkspaceLayouts(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	roleView := func(views []RoleLayoutView, role string) RoleLayoutView {
		for _, v := range views {
			if v.Role == role {
				return v
			}
		}
		t.Fatalf("没有角色 %s 的布局视图", role)
		return RoleLayoutView{}
	}
	pmBefore := roleView(before, "pm") // 种子已带 inbox，迁移不该碰它

	sql, err := store.MigrationSQL("0010_workspace_inbox_readouts.sql")
	if err != nil {
		t.Fatal(err)
	}
	check := func(round string) {
		t.Helper()
		if _, err := a.Store.Pool.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: 迁移执行失败: %v", round, err)
		}
		views, err := a.OrgWorkspaceLayouts(ctx, f.owner)
		if err != nil {
			t.Fatal(err)
		}
		// 字符串数组 + 执行视角预设：只补 inbox，其余按顺序排布在其下，proposals 丢掉
		dev := roleView(views, "developer")
		want := []domain.LayoutBlock{{Key: "inbox", X: 0, Y: 0, W: 12, H: 2}, {Key: "my_tasks", X: 0, Y: 2, W: 12, H: 2}, {Key: "my_review", X: 0, Y: 4, W: 12, H: 2}, {Key: "events", X: 0, Y: 6, W: 6, H: 3}}
		if !reflect.DeepEqual(dev.Blocks, want) || dev.Preset == nil || *dev.Preset != "doer" {
			t.Fatalf("%s: 字符串数组行应补 inbox（执行视角不补 readouts）并保持预设: %+v %v", round, dev.Blocks, dev.Preset)
		}
		// {key, w, h} 行：补 inbox 与 readouts，原区块排在第 3 行起
		tester := roleView(views, "tester")
		want = []domain.LayoutBlock{{Key: "inbox", X: 0, Y: 0, W: 12, H: 2}, {Key: "readouts", X: 0, Y: 2, W: 12, H: 1}, {Key: "exceptions", X: 0, Y: 3, W: 6, H: 2}, {Key: "cost_budget", X: 6, Y: 3, W: 6, H: 2}}
		if !reflect.DeepEqual(tester.Blocks, want) || tester.Preset != nil {
			t.Fatalf("%s: {key, w, h} 行应补两块并下移: %+v", round, tester.Blocks)
		}
		// 明确清除的空布局保持为空（视图回落到默认，preset 为 null）
		admin := roleView(views, "admin")
		if admin.Preset != nil || !reflect.DeepEqual(admin.Blocks, presetBlocks(t, "doer")) {
			t.Fatalf("%s: 空布局不应被迁移填充: %+v", round, admin)
		}
		// 已含 inbox 的行原样不动
		if pm := roleView(views, "pm"); !reflect.DeepEqual(pm, pmBefore) {
			t.Fatalf("%s: 已含 inbox 的行不应改变: %+v vs %+v", round, pm, pmBefore)
		}
		// 带坐标的个人布局：两块在上，原区块 y 各加 3、x 不变
		ws, err := a.MyWorkspace(ctx, f.dev)
		if err != nil {
			t.Fatal(err)
		}
		want = []domain.LayoutBlock{{Key: "inbox", X: 0, Y: 0, W: 12, H: 2}, {Key: "readouts", X: 0, Y: 2, W: 12, H: 1}, {Key: "sprint", X: 6, Y: 3, W: 6, H: 2}, {Key: "trend", X: 0, Y: 5, W: 12, H: 2}}
		if ws.Source != "personal" || !reflect.DeepEqual(sizesOf(ws), want) {
			t.Fatalf("%s: 带坐标的个人布局应整体下移 3 行: %+v", round, ws.Blocks)
		}
		if ws.Blocks[0].Title != "待我处理" || ws.Blocks[1].Title != "组织概况" {
			t.Fatalf("%s: 补上的区块应有标题: %+v", round, ws.Blocks[:2])
		}
	}
	check("第一次")
	check("第二次（幂等）")

	// 用户仍可移除 inbox：不带它的布局照样接受，清除个人微调后回到角色 / 默认
	ws, err := a.SetMyWorkspace(ctx, f.dev, []string{"my_tasks", "events"})
	if err != nil || !reflect.DeepEqual(keysOf(ws), []string{"my_tasks", "events"}) {
		t.Fatalf("不带 inbox 的布局应被接受: %v %+v", err, ws)
	}
	if ws, err = a.ClearMyWorkspace(ctx, f.dev); err != nil || keysOf(ws)[0] != "inbox" {
		t.Fatalf("清除后应回到角色布局且 inbox 在最上: %v %+v", err, ws)
	}
	// 确认落库的就是写进去的形状（不是靠读取时兜底）
	var raw string
	if err := a.Store.WithOrg(ctx, f.orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select blocks::text from workspace_layouts where role_name='developer'`).Scan(&raw)
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, `[{"h": 2, "w": 12, "x": 0, "y": 0, "key": "inbox"}, {"key": "my_tasks"}`) || strings.Contains(raw, "readouts") {
		t.Fatalf("字符串数组行落库形状不符: %s", raw)
	}
}
