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

func presetBlocks(t *testing.T, key string) []string {
	p, ok := domain.PresetByKey(key)
	if !ok {
		t.Fatalf("预设 %s 不存在", key)
	}
	return p.Blocks
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
	if ws.Source != "default" || !reflect.DeepEqual(keysOf(ws), presetBlocks(t, "global")) || len(ws.RolesUsed) != 0 || !ws.CanCustomize {
		t.Fatalf("负责人默认应为全局视角: %+v", ws)
	}
	if ws.Blocks[0].Title != "组织概览摘要" {
		t.Fatalf("区块标题应按语言解析，实际 %q", ws.Blocks[0].Title)
	}

	// 只有自定义角色（无布局）的普通成员 → 默认执行视角
	ws, err = a.MyWorkspace(ctx, f.custom)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Source != "default" || !reflect.DeepEqual(keysOf(ws), presetBlocks(t, "doer")) {
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
	wantMerged := domain.MergeLayouts([][]string{presetBlocks(t, "doer"), presetBlocks(t, "team")})
	if ws.Source != "roles" || !reflect.DeepEqual(keysOf(ws), wantMerged) || !reflect.DeepEqual(ws.RolesUsed, []string{"developer", "tester"}) {
		t.Fatalf("两个角色应并集: %+v（期望 %v）", ws, wantMerged)
	}

	// 逐块配置一个角色，并校验产生了动态
	if _, err := a.SetRoleWorkspace(ctx, f.owner, "developer", []string{"events", "my_tasks"}, ""); err != nil {
		t.Fatal(err)
	}
	ws, _ = a.MyWorkspace(ctx, f.dev)
	if got := keysOf(ws); got[0] != "events" || got[1] != "my_tasks" {
		t.Fatalf("角色布局顺序应保留: %v", got)
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
	if ws.Source != "default" || !reflect.DeepEqual(keysOf(ws), presetBlocks(t, "doer")) {
		t.Fatalf("角色布局清空后应回到默认执行视角: %+v", ws)
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
	if len(cat.Blocks) != 10 || len(cat.Presets) != 5 || cat.Blocks[0].Title != "组织概览摘要" || cat.Presets[0].Description == "" {
		t.Fatalf("目录不符: %d 区块 %d 预设 %+v", len(cat.Blocks), len(cat.Presets), cat.Blocks[0])
	}
	en := *f.dev
	en.Locale = i18n.EnUS
	if a.WorkspaceCatalog(&en).Blocks[0].Title != "Organization overview" {
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
