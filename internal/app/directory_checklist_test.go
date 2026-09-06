package app

import (
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

func checkStatus(v *ChecklistView, key string) string {
	for _, c := range v.Checks {
		if c.Key == key {
			return c.Status
		}
	}
	return ""
}

// 接入检查清单（ADR 0017 补记二）：未配置 → 填凭据；凭据错 → 阻塞；提供方报权限缺项 → 阻塞且预览 / 同步被挡；
// 权限范围只含部分部门 → 待处理、列出可选部门、下一步是选范围；全通 → 预览 → 定时 → 完成。
func TestDirectoryChecklist(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	fake := fakeDirectoryApp(t, a)
	fake.SetDepts([]directory.Dept{{ID: "od-a", Name: "产品部", ParentID: "0"}})
	fake.SetUsers([]directory.User{{ID: "ou-li", Name: "小李", DeptIDs: []string{"od-a"}, Active: true}})

	if _, err := a.DirectoryChecklist(ctx, yi); err == nil {
		t.Fatal("无组织设置权限者不应能看检查清单")
	}
	// 未配置
	v, err := a.DirectoryChecklist(ctx, jia)
	if err != nil || v.Ready || v.Next.Step != "credentials" || len(v.Checks) != 0 || v.Next.Text != "选择提供方并填写凭据。" {
		t.Fatalf("未配置时应指向填凭据 %+v %v", v, err)
	}
	// 凭据错
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Provider: strp("fake"), Credentials: creds("app_id", "cli_x", "app_secret", "wrong")}); err != nil {
		t.Fatal(err)
	}
	v, err = a.DirectoryChecklist(ctx, jia)
	if err != nil || v.Ready || v.Next.Step != "checks" || checkStatus(v, "credentials") != "blocked" || checkStatus(v, "structure") != "skipped" || v.Provider != "fake" || v.ProviderTitle != "测试目录" {
		t.Fatalf("凭据错应阻塞 %+v %v", v, err)
	}
	if v.Checks[0].Blocking != true || !strings.Contains(v.Checks[0].Detail, "invalid app_secret") || v.Checks[0].Fix == "" {
		t.Fatalf("阻塞项应带原话与修法 %+v", v.Checks[0])
	}
	// 修好凭据；提供方再报一项阻塞的权限缺项（模拟飞书部门没有名称）
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Credentials: creds("app_secret", "app-secret-plain")}); err != nil {
		t.Fatal(err)
	}
	fake.SetChecks([]directory.Check{
		{Key: "dept_names", Title: i18n.T("部门名称权限", "Department names"), Status: directory.CheckBlocked, Blocking: true,
			Detail: i18n.T("读到了 3 个部门，但都没有名称。", "Read 3 departments without names."), Fix: i18n.T("在「权限管理」开通「获取部门基础信息」。", "Enable department basic info."), FixURL: "https://example.test/app/cli_x/auth"},
		{Key: "user_emails", Title: i18n.T("邮箱权限", "Emails"), Status: directory.CheckTodo, Detail: i18n.T("读不到邮箱。", "No emails."), Fix: i18n.T("开通「获取用户邮箱信息」。", "Enable user email.")},
	})
	v, err = a.DirectoryChecklist(ctx, jia)
	if err != nil || v.Ready || v.Next.Step != "checks" || !strings.Contains(v.Next.Text, "获取部门基础信息") || len(v.Checks) != 4 || checkStatus(v, "dept_names") != "blocked" || checkStatus(v, "user_emails") != "todo" {
		t.Fatalf("权限缺项应阻塞并指向修法 %+v %v", v, err)
	}
	if v.Checks[2].FixURL != "https://example.test/app/cli_x/auth" {
		t.Fatalf("应带控制台链接 %+v", v.Checks[2])
	}
	if _, err := a.PreviewDirectory(ctx, jia); err == nil || !strings.Contains(err.Error(), "接入还没完成：") || !strings.Contains(err.Error(), "都没有名称") || !strings.Contains(err.Error(), "获取部门基础信息") {
		t.Fatalf("有阻塞项时预览应被拒并说明: %v", err)
	}
	run, err := a.SyncDirectory(ctx, jia)
	if err != nil || run.Status != "failed" || len(run.Errors) != 1 || !strings.Contains(run.Errors[0], "接入还没完成") || run.AddedTeams != 0 {
		t.Fatalf("有阻塞项时同步应记为失败且不写任何东西 %+v %v", run, err)
	}
	res, err := a.TestDirectory(ctx, jia)
	if err != nil || res.OK || !strings.Contains(res.Error, "都没有名称") || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "读不到邮箱") {
		t.Fatalf("连通性测试应与检查清单一致 %+v %v", res, err)
	}
	if teams, _ := a.ListTeams(ctx, jia); findTeam(teams, "产品部") != nil {
		t.Fatal("被挡住的同步不应建团队")
	}

	// 权限范围只含部分部门：待处理、列出部门、下一步是选范围（不阻塞）
	fake.SetChecks([]directory.Check{
		{Key: "scope", Title: i18n.T("通讯录权限范围", "Scope"), Status: directory.CheckTodo, Detail: i18n.T("权限范围只包含 2 个部门。", "2 departments."), Fix: i18n.T("改成全部成员，或在下一步选范围。", "..."),
			Roots: []directory.Dept{{ID: "od-a", Name: "产品部"}, {ID: "od-b", Name: "行政部"}}},
	})
	v, err = a.DirectoryChecklist(ctx, jia)
	if err != nil || !v.Ready || v.Next.Step != "scope" || len(v.SuggestedRoots) != 2 || v.SuggestedRoots[1].Name != "行政部" || len(v.RootDepartmentIDs) != 0 {
		t.Fatalf("部分权限范围应指向选范围并列出部门 %+v %v", v, err)
	}
	// 选了范围后：下一步是预览
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{RootDepartmentIDs: &[]string{"od-a", " od-b ", "od-a", "0", ""}}); err != nil {
		t.Fatal(err)
	}
	v, err = a.DirectoryChecklist(ctx, jia)
	if err != nil || !v.Ready || v.Next.Step != "preview" || strings.Join(v.RootDepartmentIDs, ",") != "od-a,od-b" || v.RootDepartmentID != "od-a" {
		t.Fatalf("选完范围应指向预览 %+v %v", v, err)
	}
	res, err = a.TestDirectory(ctx, jia)
	if err != nil || !res.OK || len(res.SuggestedRoots) != 2 || len(res.Warnings) != 1 {
		t.Fatalf("待处理项不挡连通性测试 %+v %v", res, err)
	}

	// 全通：预览 → 同步一次 → 下一步是定时 → 设了频率 → 完成
	fake.SetChecks(nil)
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{RootDepartmentIDs: &[]string{}}); err != nil {
		t.Fatal(err)
	}
	v, _ = a.DirectoryChecklist(ctx, jia)
	if !v.Ready || v.Next.Step != "preview" || len(v.Checks) != 2 || len(v.SuggestedRoots) != 0 || len(v.RootDepartmentIDs) != 0 || v.RootDepartmentID != "0" {
		t.Fatalf("全通且未同步过应指向预览 %+v", v)
	}
	if _, err := a.PreviewDirectory(ctx, jia); err != nil {
		t.Fatal(err)
	}
	if run, err := a.SyncDirectory(ctx, jia); err != nil || run.Status != "partial" || run.AddedTeams != 1 {
		t.Fatalf("全通后应能同步 %+v %v", run, err)
	}
	v, _ = a.DirectoryChecklist(ctx, jia)
	if v.Next.Step != "schedule" {
		t.Fatalf("同步过且频率为手动应指向定时 %+v", v.Next)
	}
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Schedule: strp("daily")}); err != nil {
		t.Fatal(err)
	}
	v, _ = a.DirectoryChecklist(ctx, jia)
	if v.Next.Step != "sync" || v.Next.Text != "接入已完成，可以随时同步。" {
		t.Fatalf("全部就位应为完成态 %+v", v.Next)
	}
	// 连不上：合成一项阻塞的「连接提供方」
	fake.TokenErr = &directory.UnreachableError{Err: errString("dial tcp: connection refused")}
	v, err = a.DirectoryChecklist(ctx, jia)
	if err != nil || v.Ready || len(v.Checks) != 1 || v.Checks[0].Key != "connection" || v.Checks[0].Status != "blocked" || !strings.Contains(v.Checks[0].Detail, "连不上测试目录") {
		t.Fatalf("连不上应合成阻塞项 %+v %v", v, err)
	}
	fake.TokenErr = nil
}

type errString string

func (e errString) Error() string { return string(e) }

// 多个同步根（ADR 0017 补记二）：勾选的每个部门各自成为顶层团队，子部门挂在下面，其他部门不进来；再同步幂等。
func TestDirectoryMultiRootSync(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)
	fake := fakeDirectoryApp(t, a)
	fake.SetDepts([]directory.Dept{
		{ID: "od-a", Name: "产品部", ParentID: "0"}, {ID: "od-a1", Name: "研发组", ParentID: "od-a"},
		{ID: "od-b", Name: "行政部", ParentID: "0"},
		{ID: "od-c", Name: "不同步的部门", ParentID: "0"},
	})
	fake.SetUsers([]directory.User{
		{ID: "ou-1", Name: "小李", DeptIDs: []string{"od-a1"}, Active: true},
		{ID: "ou-2", Name: "小王", DeptIDs: []string{"od-b"}, Active: true},
		{ID: "ou-3", Name: "外人", DeptIDs: []string{"od-c"}, Active: true},
	})
	v, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Provider: strp("fake"), Credentials: creds("app_id", "cli_x", "app_secret", "app-secret-plain"), RootDepartmentIDs: &[]string{"od-a", "od-b"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(v.RootDepartmentIDs, ",") != "od-a,od-b" || v.RootDepartmentID != "od-a" {
		t.Fatalf("多根应存成列表且首个兼容旧字段 %+v", v)
	}
	pv, err := a.PreviewDirectory(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.Teams) != 3 || pv.MembersTotal != 2 {
		t.Fatalf("预览应只含两个根及其子部门 %+v", pv)
	}
	parents := map[string]string{}
	for _, tp := range pv.Teams {
		parents[tp.Name] = tp.ParentExternalID
	}
	if parents["产品部"] != "" || parents["行政部"] != "" || parents["研发组"] != "od-a" {
		t.Fatalf("两个根应为顶层、子部门挂在下面 %v", parents)
	}
	if !strings.Contains(strings.Join(pv.Notes, "\n"), "按 2 个同步根部门同步") {
		t.Fatalf("应说明按几个根同步 %v", pv.Notes)
	}
	run, err := a.SyncDirectory(ctx, jia)
	if err != nil || run.AddedTeams != 3 || run.AddedMembers != 2 {
		t.Fatalf("同步数量不符 %+v %v", run, err)
	}
	teams, _ := a.ListTeams(ctx, jia)
	prod, dev, adm := findTeam(teams, "产品部"), findTeam(teams, "研发组"), findTeam(teams, "行政部")
	if prod == nil || dev == nil || adm == nil || prod.ParentID != "" || adm.ParentID != "" || dev.ParentID != prod.ID || findTeam(teams, "不同步的部门") != nil {
		t.Fatalf("团队树不符 %+v %+v %+v", prod, dev, adm)
	}
	run2, err := a.SyncDirectory(ctx, jia)
	if err != nil || run2.AddedTeams != 0 || run2.UpdatedTeams != 0 || run2.DeactivatedTeams != 0 || run2.AddedMembers != 0 || run2.DeactivatedMembers != 0 {
		t.Fatalf("再同步应幂等 %+v %v", run2, err)
	}
	// 单个根（旧字段）仍然可用：只同步行政部，产品部与研发组停用
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{RootDepartmentID: strp("od-b")}); err != nil {
		t.Fatal(err)
	}
	pv3, err := a.PreviewDirectory(ctx, jia)
	if err != nil || len(pv3.TeamsToDeactivate) != 2 || len(pv3.Teams) != 1 {
		t.Fatalf("改为单根后另两个团队应停用 %+v %v", pv3, err)
	}
	_ = domain.PlanCreate
}
