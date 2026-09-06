package app

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// 冲突在预览里解决（ADR 0017 补记四）：手工建的团队与成员在 IM 首次同步时撞车 →
// 候选进「需要你确认」、同步被挡；决定合并 / 新建 / 跳过后同步放行；跳过的列进 skipped、可撤回；
// 定时同步遇到未决定的候选记一次失败运行。
func TestDirectoryConflictsResolvedInPreview(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	fake := fakeDirectoryApp(t, a)
	// 手工：产品部 ─ 研发组；乙在研发组；另有手工成员「小王」在研发组
	prod, err := a.SaveTeam(ctx, jia, "", TeamPatch{Name: strp("产品部")})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := a.SaveTeam(ctx, jia, "", TeamPatch{Name: strp("研发组"), ParentID: strp(prod.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "move_team", TeamID: dev.ID}); err != nil {
		t.Fatal(err)
	}
	var wang *domain.Member
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		acc, err := a.Store.CreateAccount(ctx, tx, "wang-"+orgID+"@t.local", "!nopw", "小王")
		if err != nil {
			return err
		}
		wang = &domain.Member{OrgID: orgID, AccountID: acc.ID, Name: "小王", Roles: []string{"developer"}, Active: true, Source: domain.SourceManual}
		if err := a.Store.CreateMemberFull(ctx, tx, wang); err != nil {
			return err
		}
		return a.Store.AddTeamMember(ctx, tx, orgID, dev.ID, wang.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Provider: strp("fake"), Credentials: creds("app_id", "cli_x", "app_secret", "app-secret-plain"), Schedule: strp("hourly")}); err != nil {
		t.Fatal(err)
	}
	// IM：产品部 ─ 研发组、设计组；小王（无邮箱，研发组）、小赵（设计组）、小李
	fake.SetDepts([]directory.Dept{{ID: "od-a", Name: "产品部", ParentID: "0"}, {ID: "od-b", Name: "研发组", ParentID: "od-a"}, {ID: "od-c", Name: "设计组", ParentID: "od-a"}})
	fake.SetUsers([]directory.User{
		{ID: "ou-wang", Name: "小王", DeptIDs: []string{"od-b"}, Active: true},
		{ID: "ou-zhao", Name: "小赵", DeptIDs: []string{"od-c"}, Active: true},
		{ID: "ou-li", Name: "小李", Email: "li-" + orgID + "@x.local", DeptIDs: []string{"od-b"}, Active: true},
	})

	pv, err := a.PreviewDirectory(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	// 产品部（顶层同名）是候选；研发组要等产品部决定后才对得上层级；小王姓名相同且部门与主团队同名
	if pv.BlockedByConfirmations != 2 || len(pv.Confirmations) != 2 {
		t.Fatalf("应有 2 条待确认（产品部、小王），实际 %d: %+v", pv.BlockedByConfirmations, pv.Confirmations)
	}
	byExt := map[string]ConfirmationView{}
	for _, c := range pv.Confirmations {
		byExt[c.External.ID] = c
	}
	if c := byExt["od-a"]; c.Kind != "team" || len(c.Candidates) != 1 || c.Candidates[0].LocalID != prod.ID || c.Candidates[0].ReasonText != "团队同名同层级" || c.Candidates[0].SourceTitle != "手工" {
		t.Fatalf("产品部候选不符 %+v", c)
	}
	if c := byExt["ou-wang"]; c.Kind != "member" || c.External.Dept != "研发组" || len(c.Candidates) != 1 || c.Candidates[0].LocalID != wang.ID || c.Candidates[0].ReasonText != "姓名相同且都在「研发组」下" || c.Candidates[0].TeamPath != "产品部 / 研发组" {
		t.Fatalf("小王候选不符 %+v", c)
	}
	if _, err := a.SyncDirectory(ctx, jia); err == nil || !strings.Contains(err.Error(), "还有 2 条要先确认，再同步。") {
		t.Fatalf("同步应被挡，实际 %v", err)
	}
	// 定时同步：记一次失败运行，不报错
	res := a.RunScheduledDirectorySyncs(ctx, timeNow().Add(48*3600*1e9))
	var mine *ScheduledSyncResult
	for i := range res {
		if res[i].OrgID == orgID {
			mine = &res[i]
		}
	}
	if mine == nil || mine.Err != nil || mine.Run == nil || mine.Run.Status != "failed" || !strings.Contains(mine.Run.Errors[0], "还有 2 条要先确认") {
		t.Fatalf("定时同步遇到未决定的候选应记失败运行，实际 %+v", mine)
	}

	// 非法决定
	if _, err := a.PutDirectoryDecisions(ctx, jia, DecisionsInput{Items: []DecisionInput{{Kind: "member", ExternalID: "ou-wang", Decision: "maybe"}}}); err == nil {
		t.Fatal("非法决定应被拒")
	}
	if _, err := a.PutDirectoryDecisions(ctx, jia, DecisionsInput{Items: []DecisionInput{{Kind: "member", ExternalID: "ou-wang", Decision: "merge"}}}); err == nil || !strings.Contains(err.Error(), "并入哪个本地对象") {
		t.Fatalf("合并不指明目标应被拒，实际 %v", err)
	}
	if _, err := a.PutDirectoryDecisions(ctx, jia, DecisionsInput{Items: []DecisionInput{{Kind: "robot", ExternalID: "x", Decision: "skip"}}}); err == nil {
		t.Fatal("非法类型应被拒")
	}
	// 决定：产品部合并到手工产品部；小王合并到手工小王
	if _, err := a.PutDirectoryDecisions(ctx, jia, DecisionsInput{Items: []DecisionInput{
		{Kind: "team", ExternalID: "od-a", ExternalName: "产品部", Decision: "merge", LocalID: prod.ID},
		{Kind: "member", ExternalID: "ou-wang", ExternalName: "小王", Decision: "merge", LocalID: wang.ID},
	}}); err != nil {
		t.Fatal(err)
	}
	pv, err = a.PreviewDirectory(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	// 产品部对上后，研发组同名同层级成为候选
	if pv.BlockedByConfirmations != 1 || pv.Confirmations[0].External.ID != "od-b" || pv.Confirmations[0].External.Parent != "产品部" || pv.Confirmations[0].Candidates[0].LocalID != dev.ID {
		t.Fatalf("研发组应成为候选，实际 %+v", pv.Confirmations)
	}
	if len(pv.Decided) != 2 {
		t.Fatalf("应有 2 条已决定，实际 %+v", pv.Decided)
	}
	// 研发组：作为新建；小赵没人撞；再把设计组跳过
	if _, err := a.PutDirectoryDecisions(ctx, jia, DecisionsInput{Items: []DecisionInput{
		{Kind: "team", ExternalID: "od-b", ExternalName: "研发组", Decision: "create"},
		{Kind: "team", ExternalID: "od-c", ExternalName: "设计组", Decision: "skip"},
	}}); err != nil {
		t.Fatal(err)
	}
	pv, err = a.PreviewDirectory(ctx, jia)
	if err != nil || pv.BlockedByConfirmations != 0 || len(pv.Skipped) != 1 || pv.Skipped[0].ExternalID != "od-c" || pv.Skipped[0].ExternalName != "设计组" {
		t.Fatalf("全部决定后应无待确认、设计组在已跳过里，实际 %+v %v", pv, err)
	}
	for _, tp := range pv.Teams {
		if tp.ExternalID == "od-c" {
			t.Fatal("跳过的部门不应进计划")
		}
	}
	before := eventTypes(t, a, ctx, orgID)
	run, err := a.SyncDirectory(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	// 产品部：更新（接上身份）；研发组：新建（第二个同名团队）；小王更新、小赵与小李新建
	if run.UpdatedTeams != 1 || run.AddedTeams != 1 || run.UpdatedMembers != 1 || run.AddedMembers != 2 {
		t.Fatalf("同步数量不符 %+v", run)
	}
	teams, _ := a.ListTeams(ctx, jia)
	prodNow := findTeam(teams, "产品部")
	if prodNow == nil || prodNow.ID != prod.ID || prodNow.Source != "fake" || prodNow.ExternalName != "产品部" {
		t.Fatalf("手工产品部应接上 IM 身份，实际 %+v", prodNow)
	}
	devs := 0
	for _, tv := range teams {
		if tv.Name == "研发组" && !tv.Inactive {
			devs++
		}
	}
	if devs != 2 || findTeam(teams, "设计组") != nil {
		t.Fatalf("研发组应新建第二个、设计组不应出现，实际 %+v", teams)
	}
	ms, _ := a.OrgMembers(ctx, jia)
	w := findMember(ms, "小王")
	if w == nil || w.ID != wang.ID || w.Source != "fake" || len(w.Roles) != 1 || w.Roles[0] != "developer" {
		t.Fatalf("小王应合并到手工成员并保留角色，实际 %+v", w)
	}
	after := eventTypes(t, a, ctx, orgID)
	if after["DirectoryDecided"]-before["DirectoryDecided"] != 0 || after["DirectoryDecided"] < 4 {
		t.Fatalf("每条决定应有一条动态 %v", after)
	}

	// 对应关系面板：已绑定含产品部与小王；已跳过含设计组；手工研发组与同步研发组是疑似重复
	mp, err := a.DirectoryMappings(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	bound := map[string]BoundView{}
	for _, b := range mp.Bound {
		bound[b.ExternalID] = b
	}
	if bound["od-a"].LocalID != prod.ID || bound["ou-wang"].LocalID != wang.ID || bound["ou-wang"].LocalName != "小王" || len(mp.Bound) != 5 {
		t.Fatalf("已绑定不符 %+v", mp.Bound)
	}
	if len(mp.Skipped) != 1 || mp.Skipped[0].ExternalName != "设计组" {
		t.Fatalf("已跳过不符 %+v", mp.Skipped)
	}
	if len(mp.Duplicates) != 1 || mp.Duplicates[0].Kind != "team" || mp.Duplicates[0].A.ID != dev.ID || mp.Duplicates[0].ReasonText != "团队同名同层级" || mp.Duplicates[0].B.Source != "fake" {
		t.Fatalf("疑似重复应是两个研发组，实际 %+v", mp.Duplicates)
	}
	// 成员页提示：乙在手工研发组，IM 研发组里没有同名的人 → 没有提示
	for _, m := range ms {
		if len(m.PossibleDuplicateOf) != 0 {
			t.Fatalf("不应有成员疑似重复 %+v", m)
		}
	}

	// 撤回对设计组的决定 → 再预览时设计组按新建进计划（没人撞）
	if err := a.DeleteDirectoryDecision(ctx, jia, "team", "od-c"); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteDirectoryDecision(ctx, jia, "team", "od-c"); err == nil {
		t.Fatal("重复撤回应报没有这条决定")
	}
	pv, err = a.PreviewDirectory(ctx, jia)
	if err != nil || len(pv.Skipped) != 0 {
		t.Fatalf("撤回后不应再有已跳过 %+v %v", pv.Skipped, err)
	}
	seen := false
	for _, tp := range pv.Teams {
		seen = seen || tp.ExternalID == "od-c" && tp.Action == domain.PlanCreate
	}
	if !seen {
		t.Fatal("撤回后设计组应按新建进计划")
	}

	// 解绑小王：外部身份与决定都删，小王改为手工；再预览时又成为候选
	if err := a.UnbindDirectoryIdentity(ctx, jia, "member", "ou-wang"); err != nil {
		t.Fatal(err)
	}
	if err := a.UnbindDirectoryIdentity(ctx, jia, "member", "ou-wang"); err == nil {
		t.Fatal("重复解绑应报没有这条对应关系")
	}
	if w := memberByID(t, a, ctx, jia, wang.ID); w.Source != domain.SourceManual {
		t.Fatalf("解绑后应为手工来源 %+v", w)
	}
	pv, err = a.PreviewDirectory(ctx, jia)
	if err != nil || pv.BlockedByConfirmations != 1 || pv.Confirmations[0].External.ID != "ou-wang" {
		t.Fatalf("解绑后小王应重新成为候选 %+v %v", pv.Confirmations, err)
	}
	ev := eventTypes(t, a, ctx, orgID)
	if ev["DirectoryUnbound"] != 1 || ev["DirectoryDecided"] < 5 {
		t.Fatalf("解绑与撤回应有动态 %v", ev)
	}
}

// 团队合并：成员、目标（任务经目标）、迭代、共享边界、下级团队整体并入，旧团队停用，外部身份搬到目标；
// 目标在旧团队子树里时不成环。成员合并：任务负责人 / 验收人 / 参与角色、目标负责人、Agent 所有者改到目标，
// 目标保留角色，旧成员停用；负责人不能被并入。
func TestMergeTeamsAndMembers(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	f := setupMembersFixture(t, a, ctx, orgID, jia) // 产品事业部 ─ 研发组、测试组；同步团队「外部部门」与同步成员「丙」
	// 研发组里有乙；研发组有目标、迭代、下级团队「前端」并标为共享边界
	if _, err := a.BulkMembers(ctx, jia, BulkMembersInput{MemberIDs: []string{yi.MemberID}, Action: "move_team", TeamID: f.dev.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveTeam(ctx, jia, f.dev.ID, TeamPatch{IsBoundary: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	fe, err := a.SaveTeam(ctx, jia, "", TeamPatch{Name: strp("前端"), ParentID: strp(f.dev.ID)})
	if err != nil {
		t.Fatal(err)
	}
	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "研发目标", TeamID: f.dev.ID})
	if err != nil {
		t.Fatal(err)
	}
	sp, err := a.CreateSprint(ctx, jia, CreateSprintInput{Name: "第一迭代", TeamID: f.dev.ID, StartsOn: dayp(0), EndsOn: dayp(7)})
	if err != nil {
		t.Fatal(err)
	}
	// 给同步团队「外部部门」一条外部身份
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		return a.Store.UpsertExternalIdentity(ctx, tx, store.ExternalIdentity{OrgID: orgID, Provider: "fake", Kind: store.IdentityTeam, ExternalID: "od-ext", LocalID: f.synced.ID})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MergeTeams(ctx, jia, f.dev.ID, f.dev.ID); err == nil {
		t.Fatal("并入自己应被拒")
	}
	// 把研发组并入「外部部门」（同步团队）
	before := eventTypes(t, a, ctx, orgID)
	target, err := a.MergeTeams(ctx, jia, f.dev.ID, f.synced.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(target.MemberIDs, yi.MemberID) || !contains(target.MemberIDs, f.bing.ID) || !target.IsBoundary || target.Source != "fake" {
		t.Fatalf("成员与共享边界应并入目标 %+v", target)
	}
	teams, _ := a.ListTeams(ctx, jia)
	if d := findTeam(teams, "研发组"); d == nil || !d.Inactive || d.Source != domain.SourceManual {
		t.Fatalf("旧团队应停用不删除 %+v", d)
	}
	if feNow := findTeam(teams, "前端"); feNow == nil || feNow.ParentID != f.synced.ID {
		t.Fatalf("下级团队应挂到目标下面 %+v", feNow)
	}
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		g, err := a.Store.GoalByID(ctx, tx, goal.ID)
		if err != nil {
			return err
		}
		if g.TeamID != f.synced.ID {
			t.Fatalf("目标应并入目标团队 %+v", g)
		}
		s, err := a.Store.SprintByID(ctx, tx, sp.ID)
		if err != nil {
			return err
		}
		if s.TeamID != f.synced.ID {
			t.Fatalf("迭代应并入目标团队 %+v", s)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after := eventTypes(t, a, ctx, orgID)
	if after["TeamMerged"]-before["TeamMerged"] != 1 {
		t.Fatalf("应有 TeamMerged 动态 %v", after)
	}
	_ = fe
	// 目标在旧团队子树里：把「外部部门」并入「前端」（前端是它的下级）→ 前端提为顶层，不成环
	target2, err := a.MergeTeams(ctx, jia, f.synced.ID, fe.ID)
	if err != nil {
		t.Fatal(err)
	}
	if target2.ParentID != "" || target2.Source != "fake" || target2.Name != "外部部门" {
		t.Fatalf("目标应提到旧团队的位置并接上 IM 身份与名称，实际 %+v", target2)
	}
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		ids, err := a.Store.ListExternalIdentities(ctx, tx, "fake")
		if err != nil {
			return err
		}
		for _, x := range ids {
			if x.ExternalID == "od-ext" && x.LocalID != fe.ID {
				t.Fatalf("外部身份应搬到目标 %+v", x)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MergeTeams(ctx, jia, fe.ID, f.dev.ID); err == nil || !strings.Contains(err.Error(), "已停用") {
		t.Fatalf("并入已停用团队应被拒，实际 %v", err)
	}

	// ---- 成员合并：丙（同步）并入乙（手工）----
	bingSess := sessionOfMember(ctx, t, a, orgID, f.bing.ID)
	agent, _, err := a.RegisterAgent(ctx, bingSess, RegisterAgentInput{Name: "丙的 Agent"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		return a.Store.UpsertExternalIdentity(ctx, tx, store.ExternalIdentity{OrgID: orgID, Provider: "fake", Kind: store.IdentityMember, ExternalID: "ou-bing", LocalID: f.bing.ID})
	}); err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "丙的任务", AssigneeID: f.bing.ID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	goal2, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "丙的目标", OwnerMemberID: f.bing.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.MergeMembers(ctx, jia, jia.MemberID, yi.MemberID); err == nil || !strings.Contains(err.Error(), "组织负责人") {
		t.Fatalf("负责人不能被并入，实际 %v", err)
	}
	before = eventTypes(t, a, ctx, orgID)
	merged, err := a.MergeMembers(ctx, jia, f.bing.ID, yi.MemberID)
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != yi.MemberID || merged.Name != "丙" || merged.Source != "fake" || len(merged.Roles) != 1 || merged.Roles[0] != "developer" {
		t.Fatalf("目标应接上 IM 身份与姓名、保留自己的角色，实际 %+v", merged)
	}
	if b := memberByID(t, a, ctx, jia, f.bing.ID); b.Active || b.Source != domain.SourceManual {
		t.Fatalf("旧成员应停用 %+v", b)
	}
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		tk, err := a.Store.TaskByID(ctx, tx, task.ID)
		if err != nil {
			return err
		}
		if tk.AssigneeID != yi.MemberID || tk.CreatorID != jia.MemberID {
			t.Fatalf("任务负责人应改到目标、创建者不变 %+v", tk)
		}
		for slot, ex := range tk.Participants {
			if ex == f.bing.ID {
				t.Fatalf("参与角色 %s 仍指向旧成员", slot)
			}
		}
		g, err := a.Store.GoalByID(ctx, tx, goal2.ID)
		if err != nil {
			return err
		}
		if g.OwnerMemberID != yi.MemberID {
			t.Fatalf("目标负责人应改到目标 %+v", g)
		}
		ag, err := a.Store.AgentByID(ctx, tx, agent.ID)
		if err != nil {
			return err
		}
		if ag.OwnerMemberID != yi.MemberID || ag.RevokedAt != nil {
			t.Fatalf("Agent 所有者应改到目标且不吊销 %+v", ag)
		}
		ids, err := a.Store.ListExternalIdentities(ctx, tx, "fake")
		if err != nil {
			return err
		}
		for _, x := range ids {
			if x.ExternalID == "ou-bing" && x.LocalID != yi.MemberID {
				t.Fatalf("外部身份应搬到目标 %+v", x)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after = eventTypes(t, a, ctx, orgID)
	if after["MemberMerged"]-before["MemberMerged"] != 1 {
		t.Fatalf("应有 MemberMerged 动态 %v", after)
	}
}

// 成员页的「可能与 X 重复」：手工成员与同步成员姓名相同且主团队同名时两边都提示；可能重复面板给出同一对。
func TestMemberDuplicateHints(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	f := setupMembersFixture(t, a, ctx, orgID, jia)
	// 手工成员乙改名「丙」并放进手工团队「外部部门」（同名于同步团队，但这里直接放进同步团队的同名手工团队）
	manualDept, err := a.SaveTeam(ctx, jia, "", TeamPatch{Name: strp("外部部门")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateMember(ctx, jia, yi.MemberID, MemberPatch{Name: strp("丙"), TeamID: strp(manualDept.ID)}); err != nil {
		t.Fatal(err)
	}
	ms, err := a.OrgMembers(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	yiM, bing := findMemberByID(ms, yi.MemberID), findMemberByID(ms, f.bing.ID)
	if len(yiM.PossibleDuplicateOf) != 1 || yiM.PossibleDuplicateOf[0].ID != f.bing.ID || yiM.PossibleDuplicateOf[0].ReasonText != "姓名相同且都在「外部部门」下" {
		t.Fatalf("乙应提示可能与丙重复，实际 %+v", yiM.PossibleDuplicateOf)
	}
	if len(bing.PossibleDuplicateOf) != 1 || bing.PossibleDuplicateOf[0].ID != yi.MemberID {
		t.Fatalf("丙应提示可能与乙重复，实际 %+v", bing.PossibleDuplicateOf)
	}
	mp, err := a.DirectoryMappings(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	// 没配置提供方：只有可能重复（两个外部部门团队 + 乙丙）
	if mp.Provider != "" || len(mp.Bound) != 0 || len(mp.Duplicates) != 2 {
		t.Fatalf("未配置时只应有可能重复，实际 %+v", mp)
	}
	_ = context.Background
}

func findMemberByID(ms []MemberDetail, id string) *MemberDetail {
	for i := range ms {
		if ms[i].ID == id {
			return &ms[i]
		}
	}
	return nil
}
