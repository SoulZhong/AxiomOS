package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// fakeDirectoryApp 把内存目录登记成测试提供方 fake（只 Lookup 得到，不进生产列表）；app_secret 不等于 app-secret-plain 时取令牌被拒。
func fakeDirectoryApp(t *testing.T, a *App) *directory.Fake {
	fake := directory.NewFake()
	directory.UseFake(fake, "app-secret-plain")
	return fake
}

func creds(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func strp(s string) *string { return &s }

func findMember(ms []MemberDetail, name string) *MemberDetail {
	for i := range ms {
		if ms[i].Name == name {
			return &ms[i]
		}
	}
	return nil
}

func findTeam(ts []TeamView, name string) *TeamView {
	for i := range ts {
		if ts[i].Name == name {
			return &ts[i]
		}
	}
	return nil
}

func eventTypes(t *testing.T, a *App, ctx context.Context, orgID string) map[string]int {
	out := map[string]int{}
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := a.Store.ListEvents(ctx, tx, "", 500)
		if err != nil {
			return err
		}
		for _, e := range rows {
			out[e.Type]++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// 外部目录同步（ADR 0017）：配置与凭据 → 首次同步建树与待激活成员 → 再同步幂等 → 改名传播 →
// 人消失停用并吊销 Agent → 部门消失团队停用 → 邮箱匹配已有成员 → 预览等于同步。
func TestDirectorySyncWithFake(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	fake := fakeDirectoryApp(t, a)

	// 未配置的形状
	v, err := a.GetDirectoryConfig(ctx, jia)
	if err != nil || v.Provider != nil || v.Configured || len(v.SecretsSet) != 0 || len(v.Providers) != 2 || v.Providers[0].Key != "feishu" || v.Providers[1].Key != "wecom" {
		t.Fatalf("未配置时应为空形状且列出两个生产提供方: %+v %v", v, err)
	}
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Credentials: creds("app_id", "x")}); err == nil {
		t.Fatal("首次保存不选提供方应被拒")
	}
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Provider: strp("nope")}); err == nil {
		t.Fatal("不存在的提供方应被拒")
	}
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Provider: strp("fake"), Credentials: creds("app_secret", "s")}); err == nil || !strings.Contains(err.Error(), "App ID") {
		t.Fatalf("缺非保密字段应报「请填写App ID」: %v", err)
	}
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Provider: strp("fake"), Credentials: creds("app_id", "cli_x")}); err == nil || !strings.Contains(err.Error(), "第一次保存需要填写App Secret") {
		t.Fatalf("首次保存缺保密字段应报整句: %v", err)
	}
	if _, err := a.GetDirectoryConfig(ctx, yi); err == nil {
		t.Fatal("无组织设置权限者不应能读配置")
	}
	if _, err := a.SyncDirectory(ctx, jia); err == nil {
		t.Fatal("未配置时同步应被拒")
	}

	// 配置：坏密钥 → 测试给出中文整句；好密钥 → 通
	v, err = a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Provider: strp("fake"), Credentials: creds("app_id", "cli_x", "app_secret", "wrong"), DefaultRole: strp("developer"), Schedule: strp("hourly")})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Configured || !v.SecretsSet["app_secret"] || v.Credentials["app_id"] != "cli_x" || v.Schedule != "hourly" || v.DefaultRole != "developer" || v.ProviderTitle != "测试目录" || v.RootDepartmentID != "0" {
		t.Fatalf("配置视图不符 %+v", v)
	}
	if _, ok := v.Credentials["app_secret"]; ok || len(v.SecretsSet) != 1 {
		t.Fatalf("保密字段不应出现在 credentials 里 %+v", v)
	}
	res, err := a.TestDirectory(ctx, jia)
	if err != nil || res.OK || !strings.Contains(res.Error, "测试目录拒绝了这组凭据") || !strings.Contains(res.Error, "invalid app_secret") {
		t.Fatalf("坏凭据应返回中文整句: %+v %v", res, err)
	}
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Schedule: strp("weekly")}); err == nil {
		t.Fatal("非法频率应被拒")
	}
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{DefaultRole: strp("nope")}); err == nil {
		t.Fatal("不存在的角色应被拒")
	}
	// 省略 provider 与非保密字段表示沿用；只改保密字段
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Credentials: creds("app_secret", "app-secret-plain")}); err != nil {
		t.Fatal(err)
	}
	// 省略保密字段表示不改
	if v, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Schedule: strp("daily")}); err != nil || !v.Configured || v.Schedule != "daily" || v.Credentials["app_id"] != "cli_x" {
		t.Fatalf("省略凭据应沿用: %+v %v", v, err)
	}
	res, err = a.TestDirectory(ctx, jia)
	if err != nil || !res.OK || res.TenantName != "测试企业" {
		t.Fatalf("好凭据应通: %+v %v", res, err)
	}
	// 密文入库、明文不出现在视图与动态里
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		cfg, err := a.Store.DirectoryConfig(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if strings.Contains(string(cfg.SecretsEnc), "app-secret-plain") || cfg.Credentials["app_secret"] != "" {
			t.Fatal("库里不应有明文")
		}
		got, err := directory.DecryptSecrets(a.SecretKey, cfg.SecretsEnc)
		if err != nil || got["app_secret"] != "app-secret-plain" || len(got) != 1 {
			t.Fatalf("解密不符 %q %v", got, err)
		}
		rows, err := a.Store.ListEvents(ctx, tx, "", 50)
		if err != nil {
			return err
		}
		saw := false
		for _, e := range rows {
			if e.Type == "DirectoryConfigured" {
				saw = true
				raw, _ := json.Marshal(e.Data)
				if strings.Contains(string(raw), "app-secret-plain") {
					t.Fatalf("动态泄露了密钥: %s", raw)
				}
			}
		}
		if !saw {
			t.Fatal("应有 DirectoryConfigured 动态")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 外部目录：产品部 ─ 研发组；小李（有邮箱）、小王（无邮箱）在研发组；乙（已有手工成员，邮箱匹配）在产品部
	yiEmail := ""
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		m, err := a.Store.MemberByID(ctx, tx, yi.MemberID)
		if err != nil {
			return err
		}
		acc, err := a.Store.AccountByID(ctx, tx, m.AccountID)
		yiEmail = acc.Email
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fake.SetDepts([]directory.Dept{{ID: "od-a", Name: "产品部", ParentID: "0"}, {ID: "od-b", Name: "研发组", ParentID: "od-a"}})
	fake.SetUsers([]directory.User{
		{ID: "ou-li", Name: "小李", Email: "li-" + orgID + "@x.local", DeptIDs: []string{"od-b"}, Active: true},
		{ID: "ou-wang", Name: "小王", DeptIDs: []string{"od-b"}, Active: true},
		{ID: "ou-yi", Name: "乙（飞书）", Email: strings.ToUpper(yiEmail), DeptIDs: []string{"od-a"}, Active: true},
		{ID: "ou-gone", Name: "已离职", DeptIDs: []string{"od-a"}, Active: false},
	})

	// 预览 = 同步会做的事
	pv, err := a.PreviewDirectory(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.Teams) != 2 || pv.Teams[0].Action != domain.PlanCreate || pv.MembersTotal != 3 || pv.MembersNew != 2 || pv.MembersExisting != 0 || len(pv.MembersToDeactivate) != 0 {
		t.Fatalf("预览不符 %+v", pv)
	}
	// 邮箱相同只是候选（ADR 0017 补记四）：进「需要你确认」，同步被挡
	if pv.BlockedByConfirmations != 1 || len(pv.Confirmations) != 1 || pv.Confirmations[0].Kind != "member" || pv.Confirmations[0].External.ID != "ou-yi" || pv.Confirmations[0].External.EmailMasked != maskEmail(strings.ToLower(yiEmail)) {
		t.Fatalf("乙应作为邮箱相同的候选等人确认，实际 %+v", pv.Confirmations)
	}
	if c := pv.Confirmations[0].Candidates; len(c) != 1 || c[0].LocalID != yi.MemberID || c[0].Reason != domain.ReasonEmail || c[0].ReasonText != "邮箱相同" {
		t.Fatalf("候选应是乙、认法邮箱相同，实际 %+v", c)
	}
	if _, err := a.SyncDirectory(ctx, jia); err == nil || !strings.Contains(err.Error(), "还有 1 条要先确认，再同步。") {
		t.Fatalf("有未决定的候选时同步应被拒，实际 %v", err)
	}
	// 决定合并 → 预览里进「已决定」，同步放行
	if _, err := a.PutDirectoryDecisions(ctx, jia, DecisionsInput{Items: []DecisionInput{{Kind: "member", ExternalID: "ou-yi", ExternalName: "乙（飞书）", Decision: "merge", LocalID: yi.MemberID}}}); err != nil {
		t.Fatal(err)
	}
	pv, err = a.PreviewDirectory(ctx, jia)
	if err != nil || pv.BlockedByConfirmations != 0 || len(pv.Decided) != 1 || pv.Decided[0].Decision != "merge" || pv.Decided[0].DecidedLocalID != yi.MemberID || pv.MembersExisting != 1 {
		t.Fatalf("决定后预览应无待确认、乙在已决定里，实际 %+v %v", pv, err)
	}
	before := eventTypes(t, a, ctx, orgID)

	run, err := a.SyncDirectory(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "partial" || run.AddedTeams != 2 || run.AddedMembers != 2 || run.UpdatedMembers != 1 || run.DeactivatedMembers != 0 {
		t.Fatalf("首次同步数量不符 %+v", run)
	}
	if len(run.Errors) != 1 || !strings.Contains(run.Errors[0], "小王") || !strings.Contains(run.Errors[0], "需要手工邀请") {
		t.Fatalf("无邮箱成员应提示手工邀请，实际 %+v", run.Errors)
	}
	if len(run.Invitations) != 1 || !strings.Contains(run.Invitations[0].URL, "/invite/") {
		t.Fatalf("有邮箱的新成员应得到邀请链接，实际 %+v", run.Invitations)
	}
	after := eventTypes(t, a, ctx, orgID)
	if after["DirectorySyncRan"]-before["DirectorySyncRan"] != 1 || after["TeamCreated"]-before["TeamCreated"] != 2 || after["MemberSynced"]-before["MemberSynced"] != 2 || after["MemberInvited"]-before["MemberInvited"] != 1 {
		t.Fatalf("动态不符 before=%v after=%v", before, after)
	}

	teams, _ := a.ListTeams(ctx, jia)
	prod, dev := findTeam(teams, "产品部"), findTeam(teams, "研发组")
	if prod == nil || dev == nil || dev.ParentID != prod.ID || prod.Source != "fake" || prod.Inactive {
		t.Fatalf("团队树不符 %+v %+v", prod, dev)
	}
	if len(dev.MemberIDs) != 2 || len(prod.MemberIDs) != 1 || prod.MemberIDs[0] != yi.MemberID {
		t.Fatalf("团队成员不符 dev=%v prod=%v", dev.MemberIDs, prod.MemberIDs)
	}
	ms, _ := a.OrgMembers(ctx, jia)
	li := findMember(ms, "小李")
	if li == nil || li.DerivedStatus() != domain.MemberPendingActivation || li.Source != "fake" || li.Roles[0] != "developer" || li.Invitation == nil {
		t.Fatalf("小李应为待激活的外部目录成员并带邀请，实际 %+v", li)
	}
	yiM := findMember(ms, "乙（飞书）")
	if yiM == nil || yiM.ID != yi.MemberID || yiM.DerivedStatus() != domain.MemberActive {
		t.Fatalf("乙应被邮箱认出并改名，不应新建，实际 %+v", yiM)
	}
	if _, _, err := a.Login(ctx, li.Email, "anything", ""); err == nil {
		t.Fatal("待激活成员没有可用密码，不应能登录")
	}

	// 第二次：幂等
	run2, err := a.SyncDirectory(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if run2.AddedTeams != 0 || run2.AddedMembers != 0 || run2.UpdatedTeams != 0 || run2.UpdatedMembers != 0 || run2.DeactivatedMembers != 0 || run2.DeactivatedTeams != 0 {
		t.Fatalf("第二次同步应无变化 %+v", run2)
	}
	if run2.Status != "ok" {
		t.Fatalf("第二次同步应为 ok（小王已存在不再提示），实际 %s %v", run2.Status, run2.Errors)
	}
	ms2, _ := a.OrgMembers(ctx, jia)
	if len(ms2) != len(ms) {
		t.Fatalf("不应产生重复成员 %d → %d", len(ms), len(ms2))
	}
	teams2, _ := a.ListTeams(ctx, jia)
	if len(teams2) != len(teams) {
		t.Fatal("不应产生重复团队")
	}

	// 小李通过邀请链接激活：设密码后能登录，状态变正常
	tok := strings.TrimSuffix(run.Invitations[0].URL[strings.LastIndex(run.Invitations[0].URL, "/invite/")+len("/invite/"):], "/")
	if _, _, err := a.AcceptInvitation(ctx, tok, "小李", "short", i18n.ZhCN); err == nil {
		t.Fatal("短密码应被拒")
	}
	_, liSess, err := a.AcceptInvitation(ctx, tok, "小李", "longenough1", i18n.ZhCN)
	if err != nil || liSess.MemberID != li.ID {
		t.Fatalf("激活失败 %v %+v", err, liSess)
	}
	if _, _, err := a.Login(ctx, li.Email, "longenough1", ""); err != nil {
		t.Fatalf("激活后应能登录: %v", err)
	}
	ms3, _ := a.OrgMembers(ctx, jia)
	if findMember(ms3, "小李").DerivedStatus() != domain.MemberActive {
		t.Fatal("激活后状态应为正常")
	}
	// 小李注册一个 Agent，等会验证停用会吊销
	liAgent, _, err := a.RegisterAgent(ctx, liSess, RegisterAgentInput{Name: "小李的 Agent"})
	if err != nil {
		t.Fatal(err)
	}

	// 第三次：研发组改名、小李离开、研发组被撤
	fake.SetDepts([]directory.Dept{{ID: "od-a", Name: "产品中心", ParentID: "0"}})
	fake.SetUsers([]directory.User{
		{ID: "ou-wang", Name: "小王", DeptIDs: []string{"od-a"}, Active: true},
		{ID: "ou-yi", Name: "乙（飞书）", Email: yiEmail, DeptIDs: []string{"od-a"}, Active: true},
	})
	pv3, err := a.PreviewDirectory(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if len(pv3.MembersToDeactivate) != 1 || pv3.MembersToDeactivate[0].ID != li.ID || len(pv3.TeamsToDeactivate) != 1 || pv3.Teams[0].Action != domain.PlanUpdate {
		t.Fatalf("预览应指出小李停用、研发组停用、产品部改名，实际 %+v", pv3)
	}
	run3, err := a.SyncDirectory(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if run3.UpdatedTeams != 1 || run3.DeactivatedTeams != 1 || run3.DeactivatedMembers != 1 || run3.AddedMembers != 0 {
		t.Fatalf("第三次同步数量不符 %+v", run3)
	}
	teams3, _ := a.ListTeams(ctx, jia)
	if findTeam(teams3, "产品部") != nil || findTeam(teams3, "产品中心") == nil || findTeam(teams3, "产品中心").ID != prod.ID {
		t.Fatal("改名应传播到同一个团队")
	}
	if devNow := findTeam(teams3, "研发组"); devNow == nil || !devNow.Inactive {
		t.Fatalf("消失的部门应标记停用而不删除，实际 %+v", devNow)
	}
	ms4, _ := a.OrgMembers(ctx, jia)
	if findMember(ms4, "小李") == nil || findMember(ms4, "小李").DerivedStatus() != domain.MemberInactive {
		t.Fatal("离开的人应停用不删除")
	}
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		ag, err := a.Store.AgentByID(ctx, tx, liAgent.ID)
		if err != nil {
			return err
		}
		if ag.RevokedAt == nil {
			t.Fatal("停用成员的 Agent 应被吊销")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Login(ctx, li.Email, "longenough1", ""); err == nil {
		t.Fatal("停用后不应能登录")
	}

	// 运行记录
	runs, err := a.DirectoryRuns(ctx, jia, 10)
	if err != nil || len(runs) != 3 || runs[0].ID != run3.ID {
		t.Fatalf("应有三次运行记录，最新在前: %d %v", len(runs), err)
	}
	v, _ = a.GetDirectoryConfig(ctx, jia)
	if v.LastRun == nil || v.LastRun.ID != run3.ID {
		t.Fatalf("配置里应带最近一次运行 %+v", v.LastRun)
	}

	// 拉取失败：记一条 failed 运行与动态，不报错
	fake.TokenErr = &directory.RejectedError{Code: 99991663, Msg: "app access token invalid"}
	run4, err := a.SyncDirectory(ctx, jia)
	if err != nil || run4.Status != "failed" || len(run4.Errors) != 1 || !strings.Contains(run4.Errors[0], "测试目录拒绝了这组凭据") {
		t.Fatalf("拉取失败应记为 failed: %+v %v", run4, err)
	}
	fake.TokenErr = nil

	// 切换提供方（ADR 0017 补记）：凭据与根部门重置；旧来源的团队与成员在预览里列成「来自旧来源、将改为手工维护」，同步后改为手工来源，不停用。
	directory.UseFakeProvider("fake2", "1", directory.NewFake(), "app-secret-plain",
		directory.CredentialField{Key: "corp_id", Title: i18n.T("企业 ID", "Corp ID")},
		directory.CredentialField{Key: "corp_secret", Title: i18n.T("Secret", "Secret"), Secret: true})
	v, err = a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Provider: strp("fake2"), Credentials: creds("corp_id", "ww1", "corp_secret", "app-secret-plain")})
	if err != nil {
		t.Fatal(err)
	}
	if *v.Provider != "fake2" || v.RootDepartmentID != "1" || v.Credentials["corp_id"] != "ww1" || v.Credentials["app_id"] != "" || !v.Configured {
		t.Fatalf("切换提供方后应重置凭据与根部门 %+v", v)
	}
	pv5, err := a.PreviewDirectory(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if len(pv5.MembersToDeactivate) != 0 || len(pv5.TeamsToDeactivate) != 0 {
		t.Fatalf("旧来源的对象不应被停用 %+v", pv5)
	}
	joined := strings.Join(pv5.Notes, "\n")
	if !strings.Contains(joined, "团队「产品中心」来自旧来源（测试目录）") || !strings.Contains(joined, "成员「小王」来自旧来源") || !strings.Contains(joined, "将改为手工维护") {
		t.Fatalf("预览应列出旧来源对象 %v", pv5.Notes)
	}
	run5, err := a.SyncDirectory(ctx, jia)
	if err != nil || run5.DeactivatedMembers != 0 || run5.DeactivatedTeams != 0 {
		t.Fatalf("切换后首次同步不应停用任何对象 %+v %v", run5, err)
	}
	teams5, _ := a.ListTeams(ctx, jia)
	if pc := findTeam(teams5, "产品中心"); pc == nil || pc.Source != domain.SourceManual || pc.Inactive {
		t.Fatalf("旧来源团队应改为手工维护 %+v", pc)
	}
	ms5, _ := a.OrgMembers(ctx, jia)
	if w := findMember(ms5, "小王"); w == nil || w.Source != domain.SourceManual || w.DerivedStatus() == domain.MemberInactive {
		t.Fatalf("旧来源成员应改为手工维护且不停用 %+v", w)
	}
	ev5 := eventTypes(t, a, ctx, orgID)
	if ev5["DirectoryDetached"] < 2 {
		t.Fatalf("改为手工维护应有动态 %v", ev5)
	}
	// 再同步一次：不再重复提示
	run6, err := a.SyncDirectory(ctx, jia)
	if err != nil || run6.Status != "ok" {
		t.Fatalf("第二次同步不应再提示旧来源 %+v %v", run6, err)
	}
	_ = store.ErrNotFound
}

// 0009 期旧格式的配置行（app_id / app_secret_enc）第一次被读到时就地升级成凭据对象。
func TestDirectoryConfigLazyUpgrade(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, _ := newTestOrg(t, a, ctx)
	fakeDirectoryApp(t, a)
	enc, err := directory.Encrypt(a.SecretKey, "app-secret-plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into org_directory(org_id,provider,app_id,app_secret_enc,root_department_id,schedule) values($1,'fake','cli_old',$2,'0','manual')`, orgID, enc)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	v, err := a.GetDirectoryConfig(ctx, jia)
	if err != nil || !v.Configured || v.Credentials["app_id"] != "cli_old" || !v.SecretsSet["app_secret"] {
		t.Fatalf("旧行应被读成已配置 %+v %v", v, err)
	}
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		cfg, err := a.Store.DirectoryConfig(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if cfg.NeedsUpgrade() || cfg.LegacyAppID != "" || cfg.LegacyAppSecretEnc != nil || cfg.Credentials["app_id"] != "cli_old" {
			t.Fatalf("应已写回新格式并清空旧列 %+v", cfg)
		}
		got, err := directory.DecryptSecrets(a.SecretKey, cfg.SecretsEnc)
		if err != nil || got["app_secret"] != "app-secret-plain" {
			t.Fatalf("升级后的密文应能解出原值 %v %v", got, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	res, err := a.TestDirectory(ctx, jia)
	if err != nil || !res.OK {
		t.Fatalf("升级后的凭据应能直接用 %+v %v", res, err)
	}
}

// 定时同步：hourly 的组织在没有运行记录时立刻到点，跑过之后一小时内不再跑。
func TestScheduledDirectorySync(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, _ := newTestOrg(t, a, ctx)
	fake := fakeDirectoryApp(t, a)
	fake.SetDepts([]directory.Dept{{ID: "od-a", Name: "行政部", ParentID: "0"}})
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Provider: strp("fake"), Credentials: creds("app_id", "cli_x", "app_secret", "app-secret-plain"), Schedule: strp("hourly")}); err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, r := range a.RunScheduledDirectorySyncs(ctx, timeNow()) {
		if r.OrgID == orgID {
			if r.Err != nil {
				t.Fatal(r.Err)
			}
			ran++
			if r.Run.AddedTeams != 1 {
				t.Fatalf("定时同步应建一个团队 %+v", r.Run)
			}
		}
	}
	if ran != 1 {
		t.Fatalf("应跑一次，实际 %d", ran)
	}
	for _, r := range a.RunScheduledDirectorySyncs(ctx, timeNow()) {
		if r.OrgID == orgID {
			t.Fatal("一小时内不应再跑")
		}
	}
	// 系统执行者的动态
	ev := eventTypes(t, a, ctx, orgID)
	if ev["DirectorySyncRan"] != 1 || ev["TeamCreated"] != 1 {
		t.Fatalf("动态不符 %v", ev)
	}
}
