package app

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// scopeOrg 建一棵两层的团队树：
//
//	集团
//	├─ 产品事业部[边界]
//	│  ├─ 研发组   开发
//	│  └─ 测试组   测试
//	└─ 服务事业部[边界]
//	   └─ 运维组   运维
//
// 负责人在集团这一层（不属于任何事业部）。
type scopeFixture struct {
	orgID                       string
	owner, dev, qa, ops         *Session
	tProd, tDev, tQA, tSvc, tOp string
	devMember, opsMember        *domain.Member
}

func newScopeOrg(t *testing.T, a *App, ctx context.Context, finance, collab domain.Visibility) *scopeFixture {
	t.Helper()
	slug := "sc-" + store.NewID("x")[2:10]
	org, err := a.Store.CreateOrganization(ctx, a.Store.Pool, slug, "范围测试组织", "CNY")
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := HashPassword("x")
	f := &scopeFixture{orgID: org.ID}
	var ownerM, devM, qaM, opsM *domain.Member
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
		devM = mk("dev", "开发", []string{"developer"})
		qaM = mk("qa", "测试", []string{"tester"})
		opsM = mk("ops", "运维", []string{"developer"})
		if err := a.Store.SetOrganizationOwner(ctx, tx, org.ID, ownerM.ID); err != nil {
			return err
		}
		team := func(name, parent string, boundary bool) string {
			tm := &domain.Team{OrgID: org.ID, Name: name, ParentID: parent, IsBoundary: boundary}
			if err := a.Store.CreateTeam(ctx, tx, tm); err != nil {
				t.Fatal(err)
			}
			return tm.ID
		}
		f.tProd = team("产品事业部", "", true)
		f.tDev = team("研发组", f.tProd, false)
		f.tQA = team("测试组", f.tProd, false)
		f.tSvc = team("服务事业部", "", true)
		f.tOp = team("运维组", f.tSvc, false)
		for _, x := range []struct{ team, member string }{{f.tDev, devM.ID}, {f.tQA, qaM.ID}, {f.tOp, opsM.ID}} {
			if err := a.Store.AddTeamMember(ctx, tx, org.ID, x.team, x.member); err != nil {
				return err
			}
		}
		o, err := a.Store.OrganizationByID(ctx, tx, org.ID)
		if err != nil {
			return err
		}
		o.FinanceVisibility, o.CollaborationVisibility = finance, collab
		return a.Store.UpdateOrganization(ctx, tx, o)
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureOrgDefaults(ctx, org.ID); err != nil {
		t.Fatal(err)
	}
	f.devMember, f.opsMember = devM, opsM
	f.owner = sessionOfMember(ctx, t, a, org.ID, ownerM.ID)
	f.owner.IsOwner = true
	f.dev = sessionOfMember(ctx, t, a, org.ID, devM.ID)
	f.qa = sessionOfMember(ctx, t, a, org.ID, qaM.ID)
	f.ops = sessionOfMember(ctx, t, a, org.ID, opsM.ID)
	return f
}

func resolve(t *testing.T, a *App, ctx context.Context, sess *Session, raw string, finance bool) (*Scope, error) {
	t.Helper()
	var sc *Scope
	err := a.Store.WithOrg(ctx, sess.OrgID, func(tx pgx.Tx) (err error) {
		sc, err = a.ResolveScope(ctx, tx, sess, raw, finance)
		return
	})
	return sc, wrapErr(err)
}

// 范围解析：边界内的团队树能切，兄弟事业部切不动。
func TestScopeResolutionOverTeamTree(t *testing.T) {
	a, ctx := testApp(t)
	f := newScopeOrg(t, a, ctx, domain.VisibilityBoundary, domain.VisibilityOrg)

	sc, err := resolve(t, a, ctx, f.dev, f.tProd, true)
	if err != nil {
		t.Fatalf("研发组的人应能看整个产品事业部的成本：%v", err)
	}
	if len(sc.TeamIDs) != 3 || !sc.HasTeam(f.tQA) || !sc.HasTeam(f.tDev) || !sc.HasTeam(f.tProd) {
		t.Fatalf("范围应包含事业部及其两个下级，实际 %v", sc.TeamIDs)
	}
	if !sc.Finance {
		t.Fatal("这个范围应当能看财务")
	}
	if sc.HasTeam(f.tOp) {
		t.Fatal("范围不该包含别的事业部")
	}
	// 协作数据默认全员可见：切到兄弟事业部没问题
	if _, err := resolve(t, a, ctx, f.dev, f.tSvc, false); err != nil {
		t.Fatalf("协作数据策略是 org 时任何范围都能切：%v", err)
	}
	// 空参数：能看全组织财务的人拿到全公司，其他人拿到自己的可见域
	sc, err = resolve(t, a, ctx, f.owner, "", true)
	if err != nil || !sc.All {
		t.Fatalf("组织负责人默认全公司，实际 %+v %v", sc, err)
	}
	sc, err = resolve(t, a, ctx, f.dev, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if sc.All || !sc.HasTeam(f.tQA) || sc.HasTeam(f.tOp) {
		t.Fatalf("默认范围应是自己的共享边界，实际 %+v", sc)
	}
}

// 财务越权：跨共享边界要 403，理由是完整中文句子。
func TestFinanceScopeForbiddenAcrossBoundary(t *testing.T) {
	a, ctx := testApp(t)
	f := newScopeOrg(t, a, ctx, domain.VisibilityBoundary, domain.VisibilityOrg)

	for _, raw := range []string{f.tSvc, f.tOp, ScopeAll} {
		_, err := resolve(t, a, ctx, f.dev, raw, true)
		ue, ok := err.(*UserError)
		if !ok || ue.Status != 403 {
			t.Fatalf("范围 %s 的财务请求应当 403，实际 %v", raw, err)
		}
		if msg := ue.Render("zh-CN"); len([]rune(msg)) < 10 || !hasSuffixRune(msg, '。') {
			t.Fatalf("拒绝理由要是完整中文句子，实际 %q", msg)
		}
	}
	// 同一个范围换成协作口径就没问题
	if _, err := resolve(t, a, ctx, f.dev, f.tSvc, false); err != nil {
		t.Fatalf("协作数据不受财务规则限制：%v", err)
	}
	// 拿到「查看全部成本」就能跨边界
	f.dev.Permissions = map[string]bool{"view_all_cost": true}
	if _, err := resolve(t, a, ctx, f.dev, ScopeAll, true); err != nil {
		t.Fatalf("持「查看全部成本」应能看全公司：%v", err)
	}
}

// 协作策略收紧到 boundary 时，跨边界的协作请求也拒绝，「查看全部工作」可以跨。
func TestCollabScopeBoundaryPolicy(t *testing.T) {
	a, ctx := testApp(t)
	f := newScopeOrg(t, a, ctx, domain.VisibilityBoundary, domain.VisibilityBoundary)

	if _, err := resolve(t, a, ctx, f.dev, f.tQA, false); err != nil {
		t.Fatalf("同一个事业部内应当看得到：%v", err)
	}
	_, err := resolve(t, a, ctx, f.dev, f.tOp, false)
	if ue, ok := err.(*UserError); !ok || ue.Status != 403 {
		t.Fatalf("跨边界的协作请求应当 403，实际 %v", err)
	}
	f.dev.Permissions = map[string]bool{"view_all_work": true}
	if _, err := resolve(t, a, ctx, f.dev, f.tOp, false); err != nil {
		t.Fatalf("持「查看全部工作」应能跨边界：%v", err)
	}
}

// 成本归口：算在执行者所属团队；执行者是 Agent 时算在所有者的团队。
func TestCostAttributionFollowsExecutorTeam(t *testing.T) {
	a, ctx := testApp(t)
	f := newScopeOrg(t, a, ctx, domain.VisibilityBoundary, domain.VisibilityOrg)

	goal, err := a.CreateGoal(ctx, f.owner, CreateGoalInput{Title: "目标", TeamID: f.tSvc})
	if err != nil {
		t.Fatal(err)
	}
	// 任务挂在服务事业部的目标下，但由研发组的开发的 Agent 执行：钱算研发组的
	ag, token, err := a.RegisterAgent(ctx, f.dev, RegisterAgentInput{Name: "开发的 Agent", Grants: map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect}})
	if err != nil {
		t.Fatal(err)
	}
	agSess, err := a.SessionFromAgentToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, f.owner, CreateTaskInput{GoalID: goal.ID, Title: "干活", AssigneeID: ag.ID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, agSess, task.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Heartbeat(ctx, agSess, task.ID, []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 1_000_000}}); err != nil {
		t.Fatal(err)
	}
	var ix *OrgIndex
	if err := a.Store.WithOrg(ctx, f.orgID, func(tx pgx.Tx) (err error) {
		ix, err = a.OrgIndex(ctx, tx)
		return
	}); err != nil {
		t.Fatal(err)
	}
	if got := ix.TeamOfExecutor(ag.ID); got != f.tDev {
		t.Fatalf("Agent 的成本应归到所有者所属团队 %s，实际 %s", f.tDev, got)
	}
	if got := ix.TeamOfExecutor(f.devMember.ID); got != f.tDev {
		t.Fatalf("成员的成本应归到自己团队 %s，实际 %s", f.tDev, got)
	}
	if got := ix.TeamOfExecutor(f.owner.MemberID); got != "" {
		t.Fatalf("没有团队的执行者应归「未分组」，实际 %s", got)
	}

	// 按团队分组的成本：钱落在研发组，不落在目标所属的服务事业部
	f.owner.Scope = ScopeAll
	buckets, err := a.CostStats(ctx, f.owner, "team")
	if err != nil {
		t.Fatal(err)
	}
	var devCost, svcCost float64
	for _, b := range buckets {
		switch b.Key {
		case f.tDev:
			devCost = b.Value
		case f.tSvc:
			svcCost = b.Value
		}
	}
	if devCost <= 0 {
		t.Fatalf("研发组应当有成本，实际 %v", buckets)
	}
	if svcCost != 0 {
		t.Fatalf("成本不该按目标所属团队归口，实际服务事业部有 %v", svcCost)
	}
	// 换成运维的人看：他的边界里没有研发组，这条成本不该出现
	f.ops.Scope = ScopeMine
	buckets, err = a.CostStats(ctx, f.ops, "team")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range buckets {
		if b.Key == f.tDev {
			t.Fatalf("范围外的团队成本不该出现：%+v", b)
		}
	}
}

// 概览：合计等于各单元之和，下钻的单元 id 能直接当范围用。
func TestOverviewTotalsMatchUnits(t *testing.T) {
	a, ctx := testApp(t)
	f := newScopeOrg(t, a, ctx, domain.VisibilityOrg, domain.VisibilityOrg)

	goal, err := a.CreateGoal(ctx, f.owner, CreateGoalInput{Title: "目标", TeamID: f.tProd})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ title, assignee string }{
		{"开发的活", f.dev.MemberID}, {"测试的活", f.qa.MemberID}, {"运维的活", f.ops.MemberID}, {"没人认领", ""},
	} {
		if _, err := a.CreateTask(ctx, f.owner, CreateTaskInput{GoalID: goal.ID, Title: x.title, AssigneeID: x.assignee, Ready: true}); err != nil {
			t.Fatal(err)
		}
	}
	f.owner.Scope = ScopeAll
	ov, err := a.Overview(ctx, f.owner, "week")
	if err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, u := range ov.Units {
		sum += u.TasksTotal
	}
	if sum != ov.Totals.TasksTotal {
		t.Fatalf("各单元任务数之和 %d 应等于合计 %d", sum, ov.Totals.TasksTotal)
	}
	if ov.Totals.TasksTotal != 4 {
		t.Fatalf("四个任务都应算进来，实际 %d", ov.Totals.TasksTotal)
	}
	// 全公司范围下，单元是根团队 + 未分组
	kinds := map[string]int{}
	for _, u := range ov.Units {
		kinds[u.Kind]++
	}
	if kinds["unassigned"] != 1 || kinds["team"] != 2 {
		t.Fatalf("全公司范围的单元应是两个根团队加一格未分组，实际 %+v", kinds)
	}
	// 下钻：拿事业部的 id 当范围，单元变成它的直属与下级
	f.owner.Scope = f.tProd
	sub, err := a.Overview(ctx, f.owner, "week")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Units) != 3 { // 直属 + 研发组 + 测试组
		t.Fatalf("下钻后应有三个单元，实际 %d", len(sub.Units))
	}
	subSum := 0
	for _, u := range sub.Units {
		subSum += u.TasksTotal
	}
	if subSum != sub.Totals.TasksTotal || sub.Totals.TasksTotal != 3 {
		t.Fatalf("事业部范围内应是三个任务（含挂在目标上的无人任务），合计 %d 单元和 %d", sub.Totals.TasksTotal, subSum)
	}
}

// 概览里看不到的单元只是成本留空，请求本身不失败。
func TestOverviewHidesCostOutsideBoundary(t *testing.T) {
	a, ctx := testApp(t)
	f := newScopeOrg(t, a, ctx, domain.VisibilityBoundary, domain.VisibilityOrg)
	f.dev.Scope = ScopeAll
	ov, err := a.Overview(ctx, f.dev, "week")
	if err != nil {
		t.Fatalf("协作口径的概览不该因为看不到钱而失败：%v", err)
	}
	for _, u := range ov.Units {
		switch u.ID {
		case f.tProd:
			if u.Cost == nil {
				t.Fatal("自己边界内的单元应当有成本")
			}
		case f.tSvc, "":
			if u.Cost != nil {
				t.Fatalf("边界外的单元不该给出成本：%+v", u)
			}
		}
	}
	if ov.Totals.Cost != nil {
		t.Fatal("全公司合计的成本对他应当留空")
	}
}

// /auth/me 的范围列表按可见域算，看不到的团队不出现。
func TestScopeOptionsFollowVisibility(t *testing.T) {
	a, ctx := testApp(t)
	f := newScopeOrg(t, a, ctx, domain.VisibilityBoundary, domain.VisibilityBoundary)

	me, err := a.Me(ctx, f.dev)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, s := range me.Scopes {
		ids[s.ID] = true
	}
	if ids[ScopeAll] || ids[f.tSvc] || ids[f.tOp] {
		t.Fatalf("边界外的范围不该出现在选择器里：%+v", me.Scopes)
	}
	if !ids[f.tProd] || !ids[f.tDev] || !ids[f.tQA] {
		t.Fatalf("边界内的团队都应当在，实际 %+v", me.Scopes)
	}
	if me.DefaultScope != f.tDev {
		t.Fatalf("默认范围应是自己所在的团队，实际 %s", me.DefaultScope)
	}
	owner, err := a.Me(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if owner.DefaultScope != ScopeAll {
		t.Fatalf("没有团队的组织负责人默认全公司，实际 %s", owner.DefaultScope)
	}
}

func hasSuffixRune(s string, r rune) bool {
	rs := []rune(s)
	return len(rs) > 0 && rs[len(rs)-1] == r
}

// 未分组的目标在按团队筛选的范围里也要能看到，且新建目标默认归到当前正看着的团队。
func TestUnassignedGoalsStayVisibleInTeamScope(t *testing.T) {
	a, ctx := testApp(t)
	f := newScopeOrg(t, a, ctx, domain.VisibilityBoundary, domain.VisibilityOrg)
	// 没指定团队、也不在某个团队范围里建：落成未分组
	loose, err := a.CreateGoal(ctx, f.owner, CreateGoalInput{Title: "没有团队的目标"})
	if err != nil {
		t.Fatal(err)
	}
	if loose.TeamID != "" && loose.TeamID != f.tProd && loose.TeamID != f.tDev && loose.TeamID != f.tQA && loose.TeamID != f.tOp {
		t.Fatalf("意外的团队归属 %q", loose.TeamID)
	}
	// 在研发组范围里建：默认归到研发组
	inDev := f.owner.WithScope(f.tDev)
	scoped, err := a.CreateGoal(ctx, inDev, CreateGoalInput{Title: "在研发组范围里建的目标"})
	if err != nil {
		t.Fatal(err)
	}
	if scoped.TeamID != f.tDev {
		t.Fatalf("在团队范围里新建的目标应归到该团队，实际 %q", scoped.TeamID)
	}
	// 切到研发组范围看目标树：两个都得在
	tree, err := a.GoalTree(ctx, inDev)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var walk func(gs []*GoalView)
	walk = func(gs []*GoalView) {
		for _, g := range gs {
			seen[g.ID] = true
			walk(g.Children)
		}
	}
	walk(tree)
	if !seen[scoped.ID] {
		t.Fatal("研发组范围里看不到归属研发组的目标")
	}
	if loose.TeamID == "" && !seen[loose.ID] {
		t.Fatal("未分组的目标在团队范围里被筛掉了")
	}
}
