package app

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
)

// membersFixture 在测试组织里搭一棵树：产品事业部 ─ 研发组、测试组；另有来自 fake 的同步团队「外部部门」与同步成员「丙」。
type membersFixture struct {
	prod, dev, qa, synced *TeamView
	bing                  *domain.Member
}

func setupMembersFixture(t *testing.T, a *App, ctx context.Context, orgID string, jia *Session) membersFixture {
	t.Helper()
	mk := func(name, parent string) *TeamView {
		tv, err := a.SaveTeam(ctx, jia, "", TeamPatch{Name: strp(name), ParentID: strp(parent)})
		if err != nil {
			t.Fatal(err)
		}
		return tv
	}
	f := membersFixture{}
	f.prod = mk("产品事业部", "")
	f.dev = mk("研发组", f.prod.ID)
	f.qa = mk("测试组", f.prod.ID)
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		st := &domain.Team{OrgID: orgID, Name: "外部部门", Source: "fake", ExternalName: "外部部门"}
		if err := a.Store.CreateTeam(ctx, tx, st); err != nil {
			return err
		}
		acc, err := a.Store.CreateAccount(ctx, tx, "bing-"+orgID+"@t.local", "!nopw", "丙")
		if err != nil {
			return err
		}
		f.bing = &domain.Member{OrgID: orgID, AccountID: acc.ID, Name: "丙", Roles: []string{"developer"}, Active: true, Source: "fake", Status: domain.MemberPendingActivation}
		if err := a.Store.CreateMemberFull(ctx, tx, f.bing); err != nil {
			return err
		}
		return a.Store.AddTeamMember(ctx, tx, orgID, st.ID, f.bing.ID)
	}); err != nil {
		t.Fatal(err)
	}
	teams, err := a.ListTeams(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	f.synced = findTeam(teams, "外部部门")
	return f
}

func memberByID(t *testing.T, a *App, ctx context.Context, sess *Session, id string) *MemberDetail {
	t.Helper()
	ms, err := a.OrgMembers(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	for i := range ms {
		if ms[i].ID == id {
			return &ms[i]
		}
	}
	t.Fatalf("成员 %s 不在列表里", id)
	return nil
}

func teamByName(t *testing.T, a *App, ctx context.Context, sess *Session, name string) *TeamView {
	t.Helper()
	teams, err := a.ListTeams(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	tv := findTeam(teams, name)
	if tv == nil {
		t.Fatalf("团队「%s」不在列表里", name)
	}
	return tv
}

// 批量操作：换团队 / 加入团队与 team_ids、人数统计、同步成员的团队由同步决定、负责人与自己不能停用、恢复。
func TestBulkMembersRules(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	f := setupMembersFixture(t, a, ctx, orgID, jia)
	before := eventTypes(t, a, ctx, orgID)

	if _, err := a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "fly"}); err == nil || !strings.Contains(err.Error(), "批量操作只能是") {
		t.Fatalf("未知动作应被拒并说明，实际 %v", err)
	}
	if _, err := a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "set_roles", Roles: []string{"nope"}}); err == nil || !strings.Contains(err.Error(), "角色「nope」不存在") {
		t.Fatalf("未知角色应被拒，实际 %v", err)
	}

	// 乙 → 研发组（move），再加入测试组（add）
	res, err := a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "move_team", TeamID: f.dev.ID})
	if err != nil || res.Updated != 1 || len(res.Skipped) != 0 {
		t.Fatalf("换团队应成功: %+v %v", res, err)
	}
	m := memberByID(t, a, ctx, jia, yi.MemberID)
	if m.TeamID != f.dev.ID || len(m.TeamIDs) != 1 || m.TeamIDs[0] != f.dev.ID {
		t.Fatalf("乙应只在研发组，实际 team_id=%s team_ids=%v", m.TeamID, m.TeamIDs)
	}
	res, err = a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "add_team", TeamID: f.qa.ID})
	if err != nil || res.Updated != 1 {
		t.Fatalf("加入团队应成功: %+v %v", res, err)
	}
	m = memberByID(t, a, ctx, jia, yi.MemberID)
	if len(m.TeamIDs) != 2 || m.TeamID == "" {
		t.Fatalf("乙应在两个团队里且有主团队，实际 %+v", m)
	}
	res, err = a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "add_team", TeamID: f.qa.ID})
	if err != nil || res.Updated != 0 || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "已经在团队「测试组」里") {
		t.Fatalf("重复加入应跳过并说明: %+v %v", res, err)
	}
	// 人数：直属与含下级
	if dev := teamByName(t, a, ctx, jia, "研发组"); dev.MemberCount != 1 || dev.SubtreeMemberCount != 1 {
		t.Fatalf("研发组人数不符 %+v", dev)
	}
	if prod := teamByName(t, a, ctx, jia, "产品事业部"); prod.MemberCount != 0 || prod.SubtreeMemberCount != 1 {
		t.Fatalf("产品事业部含下级应不重复地数到 1 人，实际 direct=%d subtree=%d", prod.MemberCount, prod.SubtreeMemberCount)
	}

	// 同步成员：不能手工挪出同步团队，也不能加进同步团队；可以加进手工团队
	res, err = a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{f.bing.ID}, Action: "move_team", TeamID: f.qa.ID})
	if err != nil || res.Updated != 0 || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "的成员的团队由同步决定。") {
		t.Fatalf("同步成员换团队应跳过并说明: %+v %v", res, err)
	}
	res, err = a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "add_team", TeamID: f.synced.ID})
	if err != nil || res.Updated != 1 {
		t.Fatalf("手工成员加进同步团队应允许: %+v %v", res, err)
	}
	res, err = a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{f.bing.ID}, Action: "add_team", TeamID: f.qa.ID})
	if err != nil || res.Updated != 1 {
		t.Fatalf("同步成员加进手工团队应允许: %+v %v", res, err)
	}
	if bing := memberByID(t, a, ctx, jia, f.bing.ID); len(bing.TeamIDs) != 2 {
		t.Fatalf("丙应在两个团队里，实际 %v", bing.TeamIDs)
	}
	// 单个成员的 PATCH 也走同一条规则
	if _, err := a.UpdateMember(ctx, jia, f.bing.ID, MemberPatch{Name: strp("丙丙")}); err == nil || !strings.Contains(err.Error(), "姓名由同步决定") {
		t.Fatalf("同步成员改名应被拒，实际 %v", err)
	}
	if _, err := a.UpdateMember(ctx, jia, f.bing.ID, MemberPatch{TeamID: strp(f.qa.ID)}); err == nil || !strings.Contains(err.Error(), "团队由同步决定") {
		t.Fatalf("同步成员单独换团队应被拒，实际 %v", err)
	}

	// 改角色
	res, err = a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID, f.bing.ID}, Action: "set_roles", Roles: []string{"tester"}})
	if err != nil || res.Updated != 2 {
		t.Fatalf("改角色应成功: %+v %v", res, err)
	}
	if m := memberByID(t, a, ctx, jia, yi.MemberID); len(m.Roles) != 1 || m.Roles[0] != "tester" {
		t.Fatalf("乙的角色应为 tester，实际 %v", m.Roles)
	}

	// 停用：负责人跳过、乙停用；再停用一次跳过；恢复
	res, err = a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{jia.MemberID, yi.MemberID}, Action: "deactivate"})
	if err != nil || res.Updated != 1 || len(res.Skipped) != 1 || res.Skipped[0].ID != jia.MemberID || res.Skipped[0].Reason != "组织负责人不能被停用。" {
		t.Fatalf("负责人应被跳过、乙应停用: %+v %v", res, err)
	}
	if m := memberByID(t, a, ctx, jia, yi.MemberID); m.Active || m.DerivedStatus() != domain.MemberInactive {
		t.Fatalf("乙应已停用，实际 %+v", m.Member)
	}
	res, err = a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "deactivate"})
	if err != nil || res.Updated != 0 || len(res.Skipped) != 1 || res.Skipped[0].Reason != "这个成员已经停用。" {
		t.Fatalf("重复停用应跳过: %+v %v", res, err)
	}
	if dev := teamByName(t, a, ctx, jia, "研发组"); dev.MemberCount != 0 {
		t.Fatalf("停用的成员不该计入人数，实际 %d", dev.MemberCount)
	}
	res, err = a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "reactivate"})
	if err != nil || res.Updated != 1 {
		t.Fatalf("恢复应成功: %+v %v", res, err)
	}
	if m := memberByID(t, a, ctx, jia, yi.MemberID); !m.Active {
		t.Fatal("乙应已恢复")
	}
	// 自己不能停用自己（乙拿到组织设置权限后试）
	yi.Permissions = map[string]bool{"org_settings": true}
	res, err = a.BulkMembers(ctx, yi, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "deactivate"})
	if err != nil || res.Updated != 0 || len(res.Skipped) != 1 || res.Skipped[0].Reason != "不能停用自己。" {
		t.Fatalf("自己停用自己应跳过: %+v %v", res, err)
	}
	if _, err := a.UpdateMember(ctx, yi, yi.MemberID, MemberPatch{Active: boolp(false)}); err == nil || err.Error() != "不能停用自己。" {
		t.Fatalf("PATCH 停用自己应被拒，实际 %v", err)
	}
	if _, err := a.UpdateMember(ctx, yi, jia.MemberID, MemberPatch{Active: boolp(false)}); err == nil || err.Error() != "组织负责人不能被停用。" {
		t.Fatalf("PATCH 停用负责人应被拒，实际 %v", err)
	}

	after := eventTypes(t, a, ctx, orgID)
	// 换团队 1 + 加入 3（乙→测试组、乙→外部部门、丙→测试组）；改角色 2；停用 1；恢复 1
	if d := after["MemberTeamChanged"] - before["MemberTeamChanged"]; d != 4 {
		t.Fatalf("MemberTeamChanged 应有 4 条，实际 %d", d)
	}
	if after["MemberRolesChanged"]-before["MemberRolesChanged"] != 2 || after["MemberDeactivated"]-before["MemberDeactivated"] != 1 || after["MemberReactivated"]-before["MemberReactivated"] != 1 {
		t.Fatalf("动态不符 before=%v after=%v", before, after)
	}
}

// 团队：挪动成环被拒、停用要先清空、恢复、同步团队不能改名 / 删除、空手工团队才能删。
func TestTeamMoveDeactivateDelete(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	f := setupMembersFixture(t, a, ctx, orgID, jia)
	before := eventTypes(t, a, ctx, orgID)

	if _, err := a.SaveTeam(ctx, jia, f.prod.ID, TeamPatch{ParentID: strp(f.dev.ID)}); err == nil || err.Error() != "不能把团队挪到它自己或它的下级下面。" {
		t.Fatalf("挪到下级下面应被拒，实际 %v", err)
	}
	if _, err := a.SaveTeam(ctx, jia, f.prod.ID, TeamPatch{ParentID: strp(f.prod.ID)}); err == nil || err.Error() != "不能把团队挪到它自己或它的下级下面。" {
		t.Fatalf("挪到自己下面应被拒，实际 %v", err)
	}
	if _, err := a.SaveTeam(ctx, jia, f.prod.ID, TeamPatch{ParentID: strp("team_nope")}); err == nil || !strings.Contains(err.Error(), "团队不存在") {
		t.Fatalf("挪到不存在的团队下应被拒，实际 %v", err)
	}
	qa, err := a.SaveTeam(ctx, jia, f.qa.ID, TeamPatch{ParentID: strp(f.dev.ID)})
	if err != nil || qa.ParentID != f.dev.ID {
		t.Fatalf("测试组挪到研发组下应成功: %+v %v", qa, err)
	}
	if _, err := a.SaveTeam(ctx, jia, f.synced.ID, TeamPatch{Name: strp("改名")}); err == nil || !strings.Contains(err.Error(), "的团队名称由同步决定。") {
		t.Fatalf("同步团队改名应被拒，实际 %v", err)
	}
	if _, err := a.SaveTeam(ctx, jia, f.dev.ID, TeamPatch{Name: strp("研发部")}); err != nil {
		t.Fatalf("手工团队改名应成功: %v", err)
	}

	// 研发部下有测试组 → 不能停用；测试组空 → 能停用；然后研发部里放个人 → 仍不能停用
	if _, err := a.SaveTeam(ctx, jia, f.dev.ID, TeamPatch{Active: boolp(false)}); err == nil || err.Error() != "先把成员和下级团队挪走，再停用这个团队。" {
		t.Fatalf("有下级的团队停用应被拒，实际 %v", err)
	}
	qa, err = a.SaveTeam(ctx, jia, f.qa.ID, TeamPatch{Active: boolp(false)})
	if err != nil || !qa.Inactive {
		t.Fatalf("空团队停用应成功: %+v %v", qa, err)
	}
	if _, err := a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "move_team", TeamID: f.qa.ID}); err == nil || !strings.Contains(err.Error(), "已停用") {
		t.Fatalf("往停用团队里加人应被拒，实际 %v", err)
	}
	if _, err := a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "move_team", TeamID: f.dev.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveTeam(ctx, jia, f.dev.ID, TeamPatch{Active: boolp(false)}); err == nil || err.Error() != "先把成员和下级团队挪走，再停用这个团队。" {
		t.Fatalf("有成员的团队停用应被拒，实际 %v", err)
	}
	if _, err := a.UpdateMember(ctx, jia, yi.MemberID, MemberPatch{TeamID: strp("")}); err != nil {
		t.Fatal(err)
	}
	dev, err := a.SaveTeam(ctx, jia, f.dev.ID, TeamPatch{Active: boolp(false)})
	if err != nil || !dev.Inactive {
		t.Fatalf("清空后停用应成功: %+v %v", dev, err)
	}
	dev, err = a.SaveTeam(ctx, jia, f.dev.ID, TeamPatch{Active: boolp(true)})
	if err != nil || dev.Inactive {
		t.Fatalf("恢复应成功: %+v %v", dev, err)
	}

	// 删除：同步团队不能删；手工团队有成员、有下级也能删——影响范围先算清楚，成员离开、下级上移到它的上级
	if err := a.DeleteTeam(ctx, jia, f.synced.ID); err == nil || !strings.Contains(err.Error(), "不能删除，只能停用。") {
		t.Fatalf("同步团队删除应被拒，实际 %v", err)
	}
	if _, err := a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "move_team", TeamID: f.dev.ID}); err != nil {
		t.Fatal(err)
	}
	impact, err := a.TeamDeleteImpact(ctx, jia, f.dev.ID)
	if err != nil || impact.Members != 1 || impact.OnlyTeam != 1 || impact.SubTeams != 1 || impact.NewParentID != f.prod.ID || impact.NewParent != "产品事业部" {
		t.Fatalf("研发部的影响范围应是 1 人（且只在这一个团队）、1 个下级、上移到产品事业部，实际 %+v %v", impact, err)
	}
	if err := a.DeleteTeam(ctx, jia, f.dev.ID); err != nil {
		t.Fatalf("有成员有下级的手工团队也应能删: %v", err)
	}
	if qa := teamByName(t, a, ctx, jia, "测试组"); qa.ParentID != f.prod.ID {
		t.Fatalf("下级应上移到产品事业部，实际 parent=%q", qa.ParentID)
	}
	if m := memberByID(t, a, ctx, jia, yi.MemberID); m.TeamID != "" || len(m.TeamIDs) != 0 {
		t.Fatalf("成员应不再属于任何团队，实际 team_id=%q team_ids=%v", m.TeamID, m.TeamIDs)
	}
	if err := a.DeleteTeam(ctx, jia, f.qa.ID); err != nil {
		t.Fatalf("空手工团队应能删: %v", err)
	}

	after := eventTypes(t, a, ctx, orgID)
	if after["TeamMoved"]-before["TeamMoved"] != 1 || after["TeamDeactivated"]-before["TeamDeactivated"] != 2 || after["TeamReactivated"]-before["TeamReactivated"] != 1 || after["TeamDeleted"]-before["TeamDeleted"] != 2 || after["TeamUpdated"]-before["TeamUpdated"] != 1 {
		t.Fatalf("动态不符 before=%v after=%v", before, after)
	}
}

// CSV：导出形状；导入预览分出新建 / 更新 / 有问题 / 同步成员；执行后新成员待激活带团队与邀请链接，旧成员改了角色与团队。
func TestMembersCSVExportImport(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	f := setupMembersFixture(t, a, ctx, orgID, jia)
	if _, err := a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "move_team", TeamID: f.dev.ID}); err != nil {
		t.Fatal(err)
	}

	out, err := a.ExportMembersCSV(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.HasPrefix(text, utf8BOM+"姓名,邮箱,团队,角色,状态,来源\n") {
		t.Fatalf("导出表头不符: %q", text[:60])
	}
	yiEmail := memberByID(t, a, ctx, jia, yi.MemberID).Email
	if !strings.Contains(text, "乙,"+yiEmail+",产品事业部 / 研发组,开发,正常,手工") {
		t.Fatalf("导出应含乙的完整路径与角色显示名，实际:\n%s", text)
	}
	if !strings.Contains(text, "丙,") || !strings.Contains(text, ",待激活,") {
		t.Fatalf("导出应含待激活的丙，实际:\n%s", text)
	}

	// 表头缺邮箱
	if _, err := a.PreviewMembersImport(ctx, jia, []byte("姓名,团队\n甲,研发组\n"), nil); err == nil || !strings.Contains(err.Error(), "表头") {
		t.Fatalf("缺邮箱列应被拒，实际 %v", err)
	}
	newEmail := "ding-" + orgID + "@t.local"
	bingEmail := memberByID(t, a, ctx, jia, f.bing.ID).Email
	csvText := utf8BOM + "name,email,team,roles\n" +
		"丁," + newEmail + ",产品事业部/测试组,测试、开发\n" +
		"乙乙," + yiEmail + ",测试组,tester\n" +
		"戊,wu-" + orgID + "@t.local,不存在的组,开发\n" +
		"己,not-an-email,,\n" +
		"庚,geng-" + orgID + "@t.local,,巫师\n" +
		"丁二," + strings.ToUpper(newEmail) + ",,\n" +
		"丙," + bingEmail + ",测试组,开发\n"
	pv, err := a.PreviewMembersImport(ctx, jia, []byte(csvText), nil)
	if err != nil {
		t.Fatal(err)
	}
	if pv.Summary.Create != 1 || pv.Summary.Update != 1 || pv.Summary.Invalid != 5 || len(pv.Rows) != 7 {
		t.Fatalf("预览汇总不符 %+v", pv.Summary)
	}
	r := pv.Rows
	if r[0].Action != "create" || r[0].Line != 2 || r[0].TeamID == nil || *r[0].TeamID != f.qa.ID || r[0].TeamPath != "产品事业部 / 测试组" || strings.Join(r[0].Roles, ",") != "tester,developer" {
		t.Fatalf("第 2 行应为新建、认出团队路径与角色显示名，实际 %+v", r[0])
	}
	if r[1].Action != "update" || r[1].TeamID == nil || *r[1].TeamID != f.qa.ID || r[1].Roles[0] != "tester" {
		t.Fatalf("第 3 行应为更新并按唯一名字认出团队，实际 %+v", r[1])
	}
	if r[2].Action != "invalid" || r[2].Reason != "找不到团队「不存在的组」。" {
		t.Fatalf("第 4 行应为找不到团队，实际 %+v", r[2])
	}
	if r[3].Action != "invalid" || r[3].Reason != "邮箱格式不对。" {
		t.Fatalf("第 5 行应为邮箱格式不对，实际 %+v", r[3])
	}
	if r[4].Action != "invalid" || r[4].Reason != "角色「巫师」不存在。" {
		t.Fatalf("第 6 行应为角色不存在，实际 %+v", r[4])
	}
	if r[5].Action != "invalid" || r[5].Reason != "这个邮箱在第 2 行已经出现过。" {
		t.Fatalf("第 7 行应为重复邮箱，实际 %+v", r[5])
	}
	if r[6].Action != "invalid" || !strings.Contains(r[6].Reason, "的成员由同步维护，导入不会改动。") {
		t.Fatalf("第 8 行同步成员应跳过并说明，实际 %+v", r[6])
	}
	// 预览不落库
	if ms, _ := a.OrgMembers(ctx, jia); len(ms) != 3 {
		t.Fatalf("预览不应新建成员，实际 %d 人", len(ms))
	}

	before := eventTypes(t, a, ctx, orgID)
	res, err := a.ImportMembers(ctx, jia, []byte(csvText), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 1 || res.Updated != 1 || res.Unchanged != 0 || len(res.Skipped) != 5 || len(res.Invitations) != 1 || res.Invitations[0].Email != newEmail || !strings.Contains(res.Invitations[0].URL, "/invite/") {
		t.Fatalf("导入结果不符 %+v", res)
	}
	ms, _ := a.OrgMembers(ctx, jia)
	ding := findMember(ms, "丁")
	if ding == nil || ding.DerivedStatus() != domain.MemberPendingActivation || ding.Source != domain.SourceManual || ding.TeamID != f.qa.ID || len(ding.Roles) != 2 || ding.Invitation == nil {
		t.Fatalf("丁应为带团队与角色的待激活手工成员并带邀请，实际 %+v", ding)
	}
	yiM := memberByID(t, a, ctx, jia, yi.MemberID)
	if yiM.Name != "乙乙" || yiM.TeamID != f.qa.ID || len(yiM.Roles) != 1 || yiM.Roles[0] != "tester" {
		t.Fatalf("乙应改名、换团队、改角色，实际 %+v", yiM)
	}
	after := eventTypes(t, a, ctx, orgID)
	if after["MemberImported"]-before["MemberImported"] != 1 || after["MemberInvited"]-before["MemberInvited"] != 1 || after["MemberRenamed"]-before["MemberRenamed"] != 1 || after["MemberRolesChanged"]-before["MemberRolesChanged"] != 1 || after["MemberTeamChanged"]-before["MemberTeamChanged"] != 1 {
		t.Fatalf("动态不符 before=%v after=%v", before, after)
	}
	inviteURL := res.Invitations[0].URL
	// 再导入一次：全部不变
	res, err = a.ImportMembers(ctx, jia, []byte(csvText), nil)
	if err != nil || res.Created != 0 || res.Updated != 0 || res.Unchanged != 2 {
		t.Fatalf("重复导入应全部不变: %+v %v", res, err)
	}
	// 丁通过邀请链接设密码后是正常成员，团队与角色不变
	tok := strings.TrimSuffix(inviteURL[strings.LastIndex(inviteURL, "/invite/")+len("/invite/"):], "/")
	_, dingSess, err := a.AcceptInvitation(ctx, tok, "", "longenough9", "")
	if err != nil {
		t.Fatal(err)
	}
	if dingSess.MemberID != ding.ID {
		t.Fatalf("接受邀请应激活同一个成员，实际 %s vs %s", dingSess.MemberID, ding.ID)
	}
	if d := memberByID(t, a, ctx, jia, ding.ID); d.DerivedStatus() != domain.MemberActive || d.TeamID != f.qa.ID || len(d.Roles) != 2 {
		t.Fatalf("激活后应为正常成员且团队角色不变，实际 %+v", d)
	}
}

// CSV 导入走同一套认法（ADR 0017 补记四）：邮箱不同、但姓名相同且团队与手工成员的主团队同名 → confirm 带候选；
// 没决定就导入 → 跳过并说明；决定 merge → 更新那个成员；create → 新建；skip → 跳过。
func TestMembersCSVImportConfirm(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	f := setupMembersFixture(t, a, ctx, orgID, jia)
	if _, err := a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "move_team", TeamID: f.dev.ID}); err != nil {
		t.Fatal(err)
	}
	newEmail := "yi2-" + orgID + "@t.local"
	csvText := "name,email,team,roles\n乙," + newEmail + ",研发组,测试\n丙," + "bing2-" + orgID + "@t.local,外部部门,开发\n"
	pv, err := a.PreviewMembersImport(ctx, jia, []byte(csvText), nil)
	if err != nil {
		t.Fatal(err)
	}
	if pv.Summary.Confirm != 1 || pv.Summary.Create != 1 || len(pv.Rows) != 2 {
		t.Fatalf("预览汇总不符 %+v", pv.Summary)
	}
	r := pv.Rows[0]
	if r.Action != "confirm" || len(r.Candidates) != 1 || r.Candidates[0].LocalID != yi.MemberID || r.Candidates[0].ReasonText != "姓名相同且都在「研发组」下" || r.Candidates[0].TeamPath != "产品事业部 / 研发组" {
		t.Fatalf("乙应为 confirm 带候选，实际 %+v", r)
	}
	// 丙是同步成员，不做候选
	if pv.Rows[1].Action != "create" {
		t.Fatalf("同步成员不做候选，实际 %+v", pv.Rows[1])
	}
	// 没决定就导入：跳过并说明
	res, err := a.ImportMembers(ctx, jia, []byte(csvText), nil)
	if err != nil || res.Created != 1 || len(res.Skipped) != 1 || res.Skipped[0].Line != 2 || res.Skipped[0].Reason != "还没确认是不是同一个人，这次没有导入。" {
		t.Fatalf("未决定的候选行应跳过并说明 %+v %v", res, err)
	}
	// 决定并入别人 → 无效
	pv, err = a.PreviewMembersImport(ctx, jia, []byte(csvText), []ImportDecision{{Line: 2, Decision: "merge", LocalID: f.bing.ID}})
	if err != nil || pv.Rows[0].Action != "invalid" || !strings.Contains(pv.Rows[0].Reason, "不在候选里") {
		t.Fatalf("并入非候选应无效 %+v %v", pv.Rows[0], err)
	}
	// 决定 skip
	pv, err = a.PreviewMembersImport(ctx, jia, []byte(csvText), []ImportDecision{{Line: 2, Decision: "skip"}})
	if err != nil || pv.Rows[0].Action != "skip" || pv.Summary.Skip != 1 || pv.Rows[0].Reason != "按你的决定跳过。" {
		t.Fatalf("决定跳过应为 skip %+v %v", pv.Rows[0], err)
	}
	// 决定 merge → 更新乙的角色，邮箱不变
	res, err = a.ImportMembers(ctx, jia, []byte(csvText), []ImportDecision{{Line: 2, Decision: "merge", LocalID: yi.MemberID}})
	if err != nil || res.Updated != 1 || res.Unchanged != 1 || len(res.Skipped) != 0 {
		t.Fatalf("决定合并应更新乙 %+v %v", res, err)
	}
	yiM := memberByID(t, a, ctx, jia, yi.MemberID)
	if len(yiM.Roles) != 1 || yiM.Roles[0] != "tester" || strings.EqualFold(yiM.Email, newEmail) {
		t.Fatalf("乙应改角色、邮箱不变 %+v", yiM)
	}
	// 决定 create → 新建第二个乙
	res, err = a.ImportMembers(ctx, jia, []byte(csvText), []ImportDecision{{Line: 2, Decision: "create"}})
	if err != nil || res.Created != 1 {
		t.Fatalf("决定新建应建成员 %+v %v", res, err)
	}
	ms, _ := a.OrgMembers(ctx, jia)
	n := 0
	for _, m := range ms {
		if m.Name == "乙" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("应有两个乙，实际 %d", n)
	}
}

// 手工邀请与导入、同步一致：新邮箱预建待激活成员（带角色与团队）；作废邀请连成员一起删；接受邀请激活成员；
// 已是正常成员的邮箱拒绝；别的组织的可登录账号只发邀请、不预建成员。
func TestInviteCreatesPendingMember(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	f := setupMembersFixture(t, a, ctx, orgID, jia)
	findMember := func(id string) *MemberDetail {
		ms, err := a.OrgMembers(ctx, jia)
		if err != nil {
			t.Fatal(err)
		}
		for i := range ms {
			if ms[i].ID == id {
				return &ms[i]
			}
		}
		return nil
	}

	// 团队不存在 / 已是成员
	if _, err := a.Invite(ctx, jia, "temp-"+orgID+"@t.local", "", []string{"developer"}, strp("team_nope")); err == nil || !strings.Contains(err.Error(), "团队不存在") {
		t.Fatalf("不存在的团队应被拒，实际 %v", err)
	}
	yiEmail := memberByID(t, a, ctx, jia, yi.MemberID).Email
	if _, err := a.Invite(ctx, jia, strings.ToUpper(yiEmail), "", nil, nil); err == nil || !strings.Contains(err.Error(), "已经是组织成员") {
		t.Fatalf("已是成员的邮箱应被拒，实际 %v", err)
	}

	// 邀请 → 待激活成员出现在成员列表里，带角色、团队与邀请
	email := "temp-" + orgID + "@t.local"
	inv, err := a.Invite(ctx, jia, email, "", []string{"developer"}, strp(f.qa.ID))
	if err != nil {
		t.Fatal(err)
	}
	if inv.MemberID == "" || inv.TeamID != f.qa.ID || !strings.Contains(inv.URL, "/invite/") {
		t.Fatalf("邀请应带 member_id 与 team_id，实际 %+v", inv.Invitation)
	}
	m := findMember(inv.MemberID)
	if m == nil || m.DerivedStatus() != domain.MemberPendingActivation || m.Source != domain.SourceManual || m.TeamID != f.qa.ID || m.Email != email || m.Name != "temp-"+orgID || len(m.Roles) != 1 || m.Invitation == nil || m.Invitation.ID != inv.ID {
		t.Fatalf("邀请后应有带团队的待激活成员，实际 %+v", m)
	}
	list, err := a.ListInvitations(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	var listed *InvitationView
	for i := range list {
		if list[i].ID == inv.ID {
			listed = &list[i]
		}
	}
	if listed == nil || listed.MemberID != inv.MemberID || listed.TeamID != f.qa.ID || listed.URL != "" {
		t.Fatalf("邀请列表应带 member_id、team_id 且不带链接，实际 %+v", listed)
	}

	// 重发邀请：不重复建成员；作废其中一条时成员还在，作废最后一条时成员一起消失
	again, err := a.Invite(ctx, jia, email, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.MemberID != inv.MemberID || again.Name != m.Name || len(again.Roles) != 1 {
		t.Fatalf("重发邀请应指向同一个待激活成员并沿用姓名角色，实际 %+v", again.Invitation)
	}
	if err := a.DeleteInvitation(ctx, jia, again.ID); err != nil {
		t.Fatal(err)
	}
	if findMember(inv.MemberID) == nil {
		t.Fatal("还有别的有效邀请时不应删成员")
	}
	before := eventTypes(t, a, ctx, orgID)
	if err := a.DeleteInvitation(ctx, jia, inv.ID); err != nil {
		t.Fatal(err)
	}
	if findMember(inv.MemberID) != nil {
		t.Fatal("作废最后一条邀请后待激活成员应消失")
	}
	after := eventTypes(t, a, ctx, orgID)
	if after["InvitationRevoked"]-before["InvitationRevoked"] != 1 {
		t.Fatalf("作废邀请应记一条动态，实际 %+v", after)
	}
	if err := a.DeleteInvitation(ctx, jia, inv.ID); err == nil {
		t.Fatal("作废不存在的邀请应报错")
	}

	// 再邀请同一邮箱（占位账号还在）→ 又是待激活成员；接受邀请 → 正常成员，团队保留
	inv2, err := a.Invite(ctx, jia, email, "小临", []string{"tester"}, strp(f.dev.ID))
	if err != nil {
		t.Fatal(err)
	}
	if inv2.MemberID == "" || inv2.MemberID == inv.MemberID {
		t.Fatalf("重新邀请应新建待激活成员，实际 %+v", inv2.Invitation)
	}
	tok := strings.TrimSuffix(inv2.URL[strings.LastIndex(inv2.URL, "/invite/")+len("/invite/"):], "/")
	_, tempSess, err := a.AcceptInvitation(ctx, tok, "", "longenough9", "")
	if err != nil {
		t.Fatal(err)
	}
	if tempSess.MemberID != inv2.MemberID {
		t.Fatalf("接受邀请应激活预建的成员 %s，实际 %s", inv2.MemberID, tempSess.MemberID)
	}
	m = findMember(inv2.MemberID)
	if m == nil || m.DerivedStatus() != domain.MemberActive || m.Name != "小临" || m.TeamID != f.dev.ID || m.Invitation != nil {
		t.Fatalf("接受邀请后应为正常成员且团队保留，实际 %+v", m)
	}
	if err := a.DeleteInvitation(ctx, jia, inv2.ID); err != nil {
		t.Fatal(err)
	}
	if findMember(inv2.MemberID) == nil {
		t.Fatal("作废已接受的邀请不应删掉已激活的成员")
	}

	// 别的组织的可登录账号：只发邀请，不预建成员
	otherEmail := "other-" + orgID + "@t.local"
	pw, _ := HashPassword("longenough8")
	if _, err := a.Store.CreateAccount(ctx, a.Store.Pool, otherEmail, pw, "外人"); err != nil {
		t.Fatal(err)
	}
	n0 := len(mustMembers(t, a, ctx, jia))
	inv3, err := a.Invite(ctx, jia, otherEmail, "", []string{"developer"}, strp(f.qa.ID))
	if err != nil {
		t.Fatal(err)
	}
	if inv3.MemberID != "" || inv3.TeamID != f.qa.ID || len(mustMembers(t, a, ctx, jia)) != n0 {
		t.Fatalf("别的组织的账号应只发邀请，实际 %+v", inv3.Invitation)
	}
}

func mustMembers(t *testing.T, a *App, ctx context.Context, sess *Session) []MemberDetail {
	t.Helper()
	ms, err := a.OrgMembers(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	return ms
}
