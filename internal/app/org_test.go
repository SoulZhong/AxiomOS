package app

import (
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 平台后台建组织 → 负责人通过邀请加入 → 组织设置：角色、邀请成员、语言。
func TestAdminOrgAndInvitationFlow(t *testing.T) {
	a, ctx := testApp(t)
	slug := "t" + NewTestSlug()
	created, err := a.AdminCreateOrganization(ctx, CreateOrgInput{Slug: slug, Name: "Test Org", Currency: "USD", DefaultLocale: "en-US", OwnerEmail: slug + "-owner@t.local", OwnerName: "Owner"})
	if err != nil {
		t.Fatal(err)
	}
	if created.OwnerInviteURL == "" || created.Owner != nil {
		t.Fatalf("新负责人账号不存在时应返回邀请链接且暂无负责人，实际 %+v", created)
	}
	token := strings.TrimSuffix(created.OwnerInviteURL[strings.LastIndex(created.OwnerInviteURL, "/invite/")+len("/invite/"):], "/")
	info, err := a.LookupInvitation(ctx, token)
	if err != nil || info.OrganizationName != "Test Org" || info.HasAccount {
		t.Fatalf("邀请查询不符: %+v %v", info, err)
	}
	if _, _, err := a.AcceptInvitation(ctx, token, "Owner", "short", i18n.EnUS); err == nil {
		t.Fatal("短密码应被拒")
	}
	_, sess, err := a.AcceptInvitation(ctx, token, "Owner", "longenough1", i18n.EnUS)
	if err != nil {
		t.Fatal(err)
	}
	if !sess.IsOwner || sess.Loc() != i18n.EnUS {
		t.Fatalf("接受邀请后应成为负责人且语言为 en-US，实际 owner=%v loc=%s", sess.IsOwner, sess.Loc())
	}
	if _, _, err := a.AcceptInvitation(ctx, token, "Owner", "longenough1", i18n.EnUS); err == nil {
		t.Fatal("重复接受应被拒")
	}

	// 角色：新建、内置不可删、有人持有不可删
	role, err := a.SaveRole(ctx, sess, "analyst", i18n.T("分析师", "Analyst"), []string{"manage_workflows"})
	if err != nil {
		t.Fatal(err)
	}
	if role.Title.In(i18n.EnUS) != "Analyst" {
		t.Fatalf("角色名应为 Analyst，实际 %s", role.Title.In(i18n.EnUS))
	}
	if err := a.DeleteRole(ctx, sess, "admin"); err == nil {
		t.Fatal("内置角色不应可删")
	}

	// 邀请普通成员并接受，持有 analyst 角色后不能删该角色
	inv, err := a.Invite(ctx, sess, slug+"-dev@t.local", "Dev", []string{"analyst"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tok2 := strings.TrimSuffix(inv.URL[strings.LastIndex(inv.URL, "/invite/")+len("/invite/"):], "/")
	_, devSess, err := a.AcceptInvitation(ctx, tok2, "Dev", "longenough2", "")
	if err != nil {
		t.Fatal(err)
	}
	if devSess.Loc() != i18n.EnUS {
		t.Fatalf("未设语言的新成员应沿用组织默认 en-US，实际 %s", devSess.Loc())
	}
	if !devSess.Can("manage_workflows") || devSess.Can("org_settings") {
		t.Fatalf("analyst 应有 manage_workflows 而无 org_settings，实际 %+v", devSess.Permissions)
	}
	if err := a.DeleteRole(ctx, sess, "analyst"); err == nil {
		t.Fatal("有人持有的角色不应可删")
	}
	if _, err := a.OrgMembers(ctx, devSess); err == nil {
		t.Fatal("无组织设置权限者不应能读成员列表")
	}

	// 语言影响拒绝理由
	goal, err := a.CreateGoal(ctx, sess, CreateGoalInput{Title: "G"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, sess, CreateTaskInput{GoalID: goal.ID, Title: "T", AssigneeID: devSess.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Transition(ctx, sess, task.ID, "start", TransitionPayload{})
	ue, ok := err.(*UserError)
	if !ok {
		t.Fatalf("应返回用户错误，实际 %v", err)
	}
	if !strings.Contains(ue.Render(i18n.EnUS), "Only the") || !strings.Contains(ue.Render(i18n.ZhCN), "只能由") {
		t.Fatalf("拒绝理由应能双语渲染，实际 en=%q zh=%q", ue.Render(i18n.EnUS), ue.Render(i18n.ZhCN))
	}

	// 平台后台：停用组织后负责人无法登录
	if _, err := a.AdminUpdateOrganization(ctx, created.ID, OrgAdminPatch{Deactivated: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Login(ctx, slug+"-owner@t.local", "longenough1", ""); err == nil {
		t.Fatal("组织停用后不应能登录")
	}
	_ = domain.GrantExecute
}

func boolp(b bool) *bool { return &b }

// NewTestSlug 生成短随机后缀。
func NewTestSlug() string { return strings.ToLower(strings.TrimPrefix(newSlugID(), "x_"))[:8] }

func newSlugID() string { return "x_" + strings.ReplaceAll(strings.ToLower(store.NewID("s")), "_", "") }
