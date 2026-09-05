package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 全局默认价格表（每百万 token，USD）。数字随发布更新，组织可覆盖。
var defaultPrices = []domain.Price{
	{ModelID: "claude-fable-5-1", InputPerMillion: 15, OutputPerMillion: 75, CacheReadPerMillion: 1.5, CacheWritePerMillion: 18.75, Currency: "USD"},
	{ModelID: "claude-opus-5", InputPerMillion: 5, OutputPerMillion: 25, CacheReadPerMillion: 0.5, CacheWritePerMillion: 6.25, Currency: "USD"},
	{ModelID: "claude-sonnet-5", InputPerMillion: 3, OutputPerMillion: 15, CacheReadPerMillion: 0.3, CacheWritePerMillion: 3.75, Currency: "USD"},
	{ModelID: "claude-haiku-4-5-20251001", InputPerMillion: 1, OutputPerMillion: 5, CacheReadPerMillion: 0.1, CacheWritePerMillion: 1.25, Currency: "USD"},
}

var defaultCapabilities = map[string]i18n.Text{
	"coding": i18n.T("编码", "Coding"), "testing": i18n.T("测试", "Testing"), "writing": i18n.T("写作", "Writing"), "data-analysis": i18n.T("数据分析", "Data analysis"),
	"design": i18n.T("设计", "Design"), "review": i18n.T("评审", "Review"), "ops": i18n.T("运维", "Operations"),
}

// EnsureGlobals 写入全局价格表。
func (a *App) EnsureGlobals(ctx context.Context) error {
	for _, p := range defaultPrices {
		if err := a.Store.UpsertGlobalPrice(ctx, a.Store.Pool, p); err != nil {
			return err
		}
	}
	return nil
}

// EnsureOrgDefaults 为组织写入内置任务类型、能力标签、汇率（幂等）。
func (a *App) EnsureOrgDefaults(ctx context.Context, orgID string) error {
	return a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		for _, tt := range domain.BuiltinTaskTypes() {
			if _, err := a.Store.CurrentTaskType(ctx, tx, tt.Name); err == store.ErrNotFound {
				if err := a.Store.SaveTaskType(ctx, tx, orgID, tt); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		existing, err := a.Store.ListCapabilities(ctx, tx)
		if err != nil {
			return err
		}
		for n, t := range defaultCapabilities {
			if _, ok := existing[n]; ok {
				continue
			}
			if err := a.Store.UpsertCapability(ctx, tx, orgID, n, t); err != nil {
				return err
			}
		}
		roles, err := a.Store.ListRoles(ctx, tx)
		if err != nil {
			return err
		}
		byName := map[string]*domain.Role{}
		for _, r := range roles {
			byName[r.Name] = r
		}
		for _, br := range domain.BuiltinRoles {
			cur := byName[br.Name]
			if cur == nil {
				if err := a.Store.UpsertRole(ctx, tx, orgID, &domain.Role{Name: br.Name, Title: br.Title, Permissions: br.Permissions, BuiltIn: true}); err != nil {
					return err
				}
				continue
			}
			// 内置角色补齐新增的内置权限（如「查看全部成本」），组织自己加的权限与改过的
			// 名称都留着，不覆盖。
			missing := false
			for _, p := range br.Permissions {
				if !contains(cur.Permissions, p) {
					cur.Permissions = append(cur.Permissions, p)
					missing = true
				}
			}
			if missing {
				if err := a.Store.UpsertRole(ctx, tx, orgID, cur); err != nil {
					return err
				}
			}
		}
		// 工作台：还没有布局的内置角色套上默认预设，已有的（包括组织改过的）不动（ADR 0015）。
		if err := a.ensureRoleLayoutDefaults(ctx, tx, orgID); err != nil {
			return err
		}
		org, err := a.Store.OrganizationByID(ctx, tx, orgID)
		if err != nil {
			return err
		}
		// 可见性策略：老组织升级上来时列是空的，这里补上默认值（ADR 0013）。
		if org.CollaborationVisibility != org.Collaboration() || org.FinanceVisibility != org.Finance() {
			org.CollaborationVisibility, org.FinanceVisibility = org.Collaboration(), org.Finance()
			if err := a.Store.UpdateOrganization(ctx, tx, org); err != nil {
				return err
			}
		}
		if org.Currency == "CNY" {
			if _, err := a.Store.ExchangeRate(ctx, tx, "USD", "CNY"); err == store.ErrNotFound {
				if err := a.Store.UpsertExchangeRate(ctx, tx, orgID, "USD", "CNY", 7.2); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// EnsurePlatformAdmin 按环境变量创建首个平台管理员（幂等）。
func (a *App) EnsurePlatformAdmin(ctx context.Context, email, password, name string) error {
	if email == "" || password == "" {
		return nil
	}
	if name == "" {
		name = "Platform admin"
	}
	acc, _, err := a.Store.AccountByEmail(ctx, a.Store.Pool, email)
	if err == store.ErrNotFound {
		hash, err := HashPassword(password)
		if err != nil {
			return err
		}
		acc, err = a.Store.CreateAccount(ctx, a.Store.Pool, strings.ToLower(email), hash, name)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return a.Store.SetPlatformAdmin(ctx, a.Store.Pool, acc.ID, true)
}

// SeedDemo 创建演示组织与数据（只在组织不存在时执行）。返回组织 ID。
func (a *App) SeedDemo(ctx context.Context) (string, error) {
	if org, err := a.Store.OrganizationBySlug(ctx, a.Store.Pool, "demo"); err == nil {
		return org.ID, nil
	}
	pw, err := HashPassword("demo1234")
	if err != nil {
		return "", err
	}
	org, err := a.Store.CreateOrganization(ctx, a.Store.Pool, "demo", "演示公司", "CNY")
	if err != nil {
		return "", err
	}
	type person struct {
		email, name string
		roles       []string
	}
	people := []person{
		{"zhong@demo.local", "仲维建", []string{"pm", "designer", "admin"}},
		{"li@demo.local", "小李", []string{"developer"}},
		{"zhang@demo.local", "小张", []string{"tester"}},
		{"zhao@demo.local", "小赵", []string{"releaser", "admin"}},
	}
	members := map[string]*domain.Member{}
	var rd *domain.Team
	err = a.Store.WithOrg(ctx, org.ID, func(tx pgx.Tx) error {
		for _, p := range people {
			acc, err := a.Store.CreateAccount(ctx, tx, p.email, pw, p.name)
			if err != nil {
				return err
			}
			m, err := a.Store.CreateMember(ctx, tx, org.ID, acc.ID, p.name, p.roles)
			if err != nil {
				return err
			}
			members[p.name] = m
		}
		if err := a.Store.SetOrganizationOwner(ctx, tx, org.ID, members["仲维建"].ID); err != nil {
			return err
		}
		// 团队树做两层，其中产品事业部标成共享边界（ADR 0013）：
		// 演示公司
		// ├─ 产品事业部[边界]  仲维建（直属）
		// │  ├─ 研发组  小李
		// │  └─ 测试组  小张
		// └─ 服务事业部
		//    ├─ 运维组  小赵
		//    └─ 客服组
		rd = &domain.Team{OrgID: org.ID, Name: "产品事业部", LeadMemberID: members["仲维建"].ID, IsBoundary: true}
		if err := a.Store.CreateTeam(ctx, tx, rd); err != nil {
			return err
		}
		dev := &domain.Team{OrgID: org.ID, Name: "研发组", ParentID: rd.ID, LeadMemberID: members["小李"].ID}
		if err := a.Store.CreateTeam(ctx, tx, dev); err != nil {
			return err
		}
		qa := &domain.Team{OrgID: org.ID, Name: "测试组", ParentID: rd.ID, LeadMemberID: members["小张"].ID}
		if err := a.Store.CreateTeam(ctx, tx, qa); err != nil {
			return err
		}
		svc := &domain.Team{OrgID: org.ID, Name: "服务事业部", LeadMemberID: members["小赵"].ID}
		if err := a.Store.CreateTeam(ctx, tx, svc); err != nil {
			return err
		}
		ops := &domain.Team{OrgID: org.ID, Name: "运维组", ParentID: svc.ID, LeadMemberID: members["小赵"].ID}
		if err := a.Store.CreateTeam(ctx, tx, ops); err != nil {
			return err
		}
		if err := a.Store.CreateTeam(ctx, tx, &domain.Team{OrgID: org.ID, Name: "客服组", ParentID: svc.ID}); err != nil {
			return err
		}
		if err := a.Store.AddTeamMember(ctx, tx, org.ID, rd.ID, members["仲维建"].ID); err != nil {
			return err
		}
		if err := a.Store.AddTeamMember(ctx, tx, org.ID, dev.ID, members["小李"].ID); err != nil {
			return err
		}
		if err := a.Store.AddTeamMember(ctx, tx, org.ID, qa.ID, members["小张"].ID); err != nil {
			return err
		}
		return a.Store.AddTeamMember(ctx, tx, org.ID, ops.ID, members["小赵"].ID)
	})
	if err != nil {
		return "", err
	}
	if err := a.EnsureOrgDefaults(ctx, org.ID); err != nil {
		return "", err
	}
	// 演示组织的财务按共享边界看：小李（研发组）能看到整个产品事业部的成本，
	// 看不到服务事业部；小赵在服务事业部但持管理员角色，跨边界看得到（ADR 0013）。
	if err := a.Store.WithOrg(ctx, org.ID, func(tx pgx.Tx) error {
		o, err := a.Store.OrganizationByID(ctx, tx, org.ID)
		if err != nil {
			return err
		}
		o.FinanceVisibility = domain.VisibilityBoundary
		return a.Store.UpdateOrganization(ctx, tx, o)
	}); err != nil {
		return "", err
	}

	// 用应用服务本身造数据，保证走同一套规则
	// 种子里的会话要和登录后的会话一样带上 is_owner 与权限，否则「开始迭代」这类需要权限的动作会失败
	sessOf := func(name string) *Session {
		m := members[name]
		sess := &Session{OrgID: org.ID, MemberID: m.ID, Actor: &domain.Executor{ID: m.ID, Kind: domain.ExecutorMember, Name: m.Name, Roles: m.Roles}}
		sess.IsOwner = org.OwnerMemberID == m.ID
		sess.Permissions = map[string]bool{}
		_ = a.Store.WithOrg(ctx, org.ID, func(tx pgx.Tx) error {
			roles, err := a.Store.ListRoles(ctx, tx)
			if err != nil {
				return err
			}
			for _, r := range roles {
				if !hasRole(m.Roles, r.Name) {
					continue
				}
				for _, p := range r.Permissions {
					sess.Permissions[p] = true
				}
			}
			o, err := a.Store.OrganizationByID(ctx, tx, org.ID)
			if err != nil {
				return err
			}
			return a.loadScopeContext(ctx, tx, sess, o)
		})
		return sess
	}
	wang, li, zhang, zhao := sessOf("仲维建"), sessOf("小李"), sessOf("小张"), sessOf("小赵")

	g := func(gs ...domain.Grant) map[domain.Grant]domain.GrantMode {
		m := map[domain.Grant]domain.GrantMode{}
		for _, x := range gs {
			m[x] = domain.GrantDirect
		}
		return m
	}
	// 小李的 Agent 另有两项「需要人确认」的授权：指派、管理流程（后者对 Agent 只能是需要人确认，ADR 0003）
	liGrants := g(domain.GrantExecute, domain.GrantClaimBacklog, domain.GrantComment, domain.GrantCreateSubtask, domain.GrantLink)
	liGrants[domain.GrantAssign] = domain.GrantWithApproval
	liGrants[domain.GrantManageWorkflows] = domain.GrantWithApproval
	liAgent, liToken, err := a.RegisterAgent(ctx, li, RegisterAgentInput{Name: "小李的 Claude Code", Runtime: "claude-code", Capabilities: []string{"coding"}, Grants: liGrants, MaxConcurrent: 2})
	if err != nil {
		return "", err
	}
	zhangAgent, _, err := a.RegisterAgent(ctx, zhang, RegisterAgentInput{Name: "小张的测试 Agent", Runtime: "custom", Capabilities: []string{"testing"}, Grants: g(domain.GrantExecute, domain.GrantClaimBacklog, domain.GrantReview, domain.GrantComment, domain.GrantCreateTask, domain.GrantLink), MaxConcurrent: 2})
	if err != nil {
		return "", err
	}
	fmt.Printf("演示 Agent 令牌（小李的 Claude Code）：%s\n", liToken)

	day := func(d int) *time.Time { t := time.Now().AddDate(0, 0, d).Truncate(24 * time.Hour); return &t }
	budget := 5000.0
	q3, err := a.CreateGoal(ctx, wang, CreateGoalInput{Title: "Q3：把新版官网上线", Description: "包含登录页改版与支付页优化", Budget: &budget, Deadline: day(45), PlannedStart: day(-20), PlannedEnd: day(45)})
	if err != nil {
		return "", err
	}
	login, err := a.CreateGoal(ctx, wang, CreateGoalInput{ParentID: q3.ID, Title: "登录页改版", PlannedStart: day(-20), PlannedEnd: day(10)})
	if err != nil {
		return "", err
	}
	pay, err := a.CreateGoal(ctx, wang, CreateGoalInput{ParentID: q3.ID, Title: "支付页优化", PlannedStart: day(-5), PlannedEnd: day(30)})
	if err != nil {
		return "", err
	}
	admin, err := a.CreateGoal(ctx, zhao, CreateGoalInput{Title: "行政：季度例行事项", PlannedStart: day(-10), PlannedEnd: day(60)})
	if err != nil {
		return "", err
	}
	parts := map[string]string{"designer": wang.MemberID, "developer": li.MemberID, "tester": zhang.MemberID, "releaser": zhao.MemberID}
	est := func(h float64) *float64 { return &h }

	// 需求 1：已走到测试中，有一个 Bug
	r1, err := a.CreateTask(ctx, wang, CreateTaskInput{GoalID: login.ID, TypeName: "requirement", Title: "登录页视觉与交互改版", Description: "按新品牌规范重做登录页，支持手机号验证码登录。", Participants: parts, EstimateHours: est(40), PlannedStart: day(-18), PlannedEnd: day(8), Priority: intp(1)})
	if err != nil {
		return "", err
	}
	liAgentSess := &Session{OrgID: org.ID, MemberID: li.MemberID, AgentID: liAgent.ID}
	zhangAgentSess := &Session{OrgID: org.ID, MemberID: zhang.MemberID, AgentID: zhangAgent.ID}
	if err := a.Store.WithOrg(ctx, org.ID, func(tx pgx.Tx) (err error) {
		liAgentSess.Actor, err = a.Store.ExecutorByID(ctx, tx, liAgent.ID)
		if err != nil {
			return
		}
		zhangAgentSess.Actor, err = a.Store.ExecutorByID(ctx, tx, zhangAgent.ID)
		return
	}); err != nil {
		return "", err
	}
	step := func(sess *Session, id, name, comment string) error {
		_, err := a.Transition(ctx, sess, id, name, TransitionPayload{Comment: comment})
		return err
	}
	must := func(err error) error { return err }
	if err := must(step(wang, r1.ID, "start_design", "")); err != nil {
		return "", err
	}
	if _, err := a.AddArtifact(ctx, wang, r1.ID, domain.Artifact{Type: "prd", Title: "登录页改版 PRD", Ref: "https://docs.example.com/prd/login"}); err != nil {
		return "", err
	}
	if err := step(wang, r1.ID, "design_done", ""); err != nil {
		return "", err
	}
	if _, err := a.Begin(ctx, liAgentSess, r1.ID); err != nil {
		return "", err
	}
	if _, err := a.Heartbeat(ctx, liAgentSess, r1.ID, []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 210000, OutputTokens: 38000, CacheReadTokens: 90000, ToolCalls: 140}}); err != nil {
		return "", err
	}
	if err := step(liAgentSess, r1.ID, "ask_for_input", "验证码登录是否需要支持海外手机号？"); err != nil {
		return "", err
	}
	if err := step(wang, r1.ID, "resume", "一期只支持大陆手机号。"); err != nil {
		return "", err
	}
	if _, err := a.Begin(ctx, liAgentSess, r1.ID); err != nil {
		return "", err
	}
	if _, err := a.Heartbeat(ctx, liAgentSess, r1.ID, []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 320000, OutputTokens: 51000, CacheReadTokens: 150000, ToolCalls: 210}}); err != nil {
		return "", err
	}
	if _, err := a.AddComment(ctx, liAgentSess, r1.ID, "已完成表单校验与验证码倒计时，单测 32 个通过。", true); err != nil {
		return "", err
	}
	if _, err := a.AddArtifact(ctx, liAgentSess, r1.ID, domain.Artifact{Type: "pr", Title: "feat: 登录页改版 #128", Ref: "https://github.com/example/web/pull/128"}); err != nil {
		return "", err
	}
	if err := step(liAgentSess, r1.ID, "dev_done", ""); err != nil {
		return "", err
	}
	if _, err := a.Begin(ctx, zhangAgentSess, r1.ID); err != nil {
		return "", err
	}
	if _, err := a.Heartbeat(ctx, zhangAgentSess, r1.ID, []domain.Usage{{ModelID: "claude-haiku-4-5-20251001", InputTokens: 80000, OutputTokens: 9000, ToolCalls: 60}}); err != nil {
		return "", err
	}
	bug, err := a.CreateTask(ctx, zhangAgentSess, CreateTaskInput{GoalID: login.ID, TypeName: "bug", Title: "验证码输入框在 iOS Safari 下被键盘遮挡", Description: "iPhone 15 / iOS 18，点击输入框后键盘弹出，输入框不上移。", Participants: map[string]string{"developer": li.MemberID, "tester": zhang.MemberID}, EstimateHours: est(3), PlannedStart: day(0), PlannedEnd: day(3), Priority: intp(0)})
	if err != nil {
		return "", err
	}
	if _, err := a.Link(ctx, zhangAgentSess, bug.ID, domain.RelationFoundIn, r1.ID); err != nil {
		return "", err
	}
	if err := step(zhang, bug.ID, "confirm", ""); err != nil {
		return "", err
	}

	// 需求 2：支付页优化，刚开始设计
	r2, err := a.CreateTask(ctx, wang, CreateTaskInput{GoalID: pay.ID, TypeName: "requirement", Title: "支付页金额展示与优惠券选择优化", Participants: map[string]string{"designer": wang.MemberID, "developer": li.MemberID, "releaser": zhao.MemberID}, RequiredCapabilities: []string{"testing"}, EstimateHours: est(32), PlannedStart: day(-3), PlannedEnd: day(25), Priority: intp(2)})
	if err != nil {
		return "", err
	}
	if err := step(wang, r2.ID, "start_design", ""); err != nil {
		return "", err
	}

	// 发布任务，前置两个需求
	rel, err := a.CreateTask(ctx, zhao, CreateTaskInput{GoalID: q3.ID, TypeName: "release", Title: "官网 v2.0 上线", Participants: map[string]string{"releaser": zhao.MemberID}, PlannedStart: day(28), PlannedEnd: day(30), EstimateHours: est(4)})
	if err != nil {
		return "", err
	}
	if _, err := a.Link(ctx, zhao, r1.ID, domain.RelationBlocks, rel.ID); err != nil {
		return "", err
	}
	if _, err := a.Link(ctx, zhao, r2.ID, domain.RelationBlocks, rel.ID); err != nil {
		return "", err
	}
	if err := step(zhao, rel.ID, "ready", ""); err != nil {
		return "", err
	}

	// 通用任务：一个已完成、一个待领取、一个进行中
	t1, err := a.CreateTask(ctx, zhao, CreateTaskInput{GoalID: admin.ID, TypeName: "generic", Title: "整理 Q3 供应商合同清单", AssigneeID: li.MemberID, EstimateHours: est(4), PlannedStart: day(-8), PlannedEnd: day(-5), Ready: true})
	if err != nil {
		return "", err
	}
	if err := step(liAgentSess, t1.ID, "start", ""); err != nil {
		return "", err
	}
	if _, err := a.Heartbeat(ctx, liAgentSess, t1.ID, []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 60000, OutputTokens: 12000, ToolCalls: 30}}); err != nil {
		return "", err
	}
	if _, err := a.AddArtifact(ctx, liAgentSess, t1.ID, domain.Artifact{Type: "result", Title: "合同清单.xlsx", Ref: "https://drive.example.com/q3-contracts"}); err != nil {
		return "", err
	}
	if err := step(liAgentSess, t1.ID, "submit", ""); err != nil {
		return "", err
	}
	if err := step(zhao, t1.ID, "accept", ""); err != nil {
		return "", err
	}
	if _, err := a.CreateTask(ctx, zhao, CreateTaskInput{GoalID: admin.ID, TypeName: "generic", Title: "编写 9 月全员周报模板", RequiredCapabilities: []string{"writing"}, EstimateHours: est(2), PlannedStart: day(1), PlannedEnd: day(4), Ready: true}); err != nil {
		return "", err
	}
	t3, err := a.CreateTask(ctx, zhao, CreateTaskInput{GoalID: admin.ID, TypeName: "generic", Title: "面谈两位候选人并出评估", AssigneeID: wang.MemberID, HumanOnly: true, EstimateHours: est(6), PlannedStart: day(-2), PlannedEnd: day(-1), Ready: true})
	if err != nil {
		return "", err
	}
	if err := step(wang, t3.ID, "start", ""); err != nil {
		return "", err
	}
	_ = zhangAgent

	// 迭代（ADR 0012）：一个已结束（给迭代速度当样本）、一个进行中、一个规划中；任务带工作量
	setPoints := func(id string, p int) error {
		_, err := a.UpdateTask(ctx, wang, id, UpdateTaskInput{Points: &p, SetPoints: true})
		return err
	}
	for _, x := range []struct {
		id string
		p  int
	}{{r1.ID, 8}, {bug.ID, 3}, {r2.ID, 5}, {t1.ID, 3}, {t3.ID, 2}, {rel.ID, 5}} {
		if err := setPoints(x.id, x.p); err != nil {
			return "", err
		}
	}
	past, err := a.CreateSprint(ctx, wang, CreateSprintInput{Name: "8 月第 2 迭代", Goal: "收尾季度行政事项", TeamID: rd.ID, StartsOn: day(-17), EndsOn: day(-4)})
	if err != nil {
		return "", err
	}
	if _, err := a.AddTasksToSprint(ctx, wang, past.ID, []string{t1.ID}); err != nil {
		return "", err
	}
	if _, err := a.StartSprint(ctx, wang, past.ID); err != nil {
		return "", err
	}
	if _, err := a.CloseSprint(ctx, wang, past.ID, "backlog", ""); err != nil {
		return "", err
	}
	cur, err := a.CreateSprint(ctx, wang, CreateSprintInput{Name: "9 月第 1 迭代", Goal: "登录页改版进入测试，支付页完成设计", TeamID: rd.ID, StartsOn: day(-3), EndsOn: day(11)})
	if err != nil {
		return "", err
	}
	if _, err := a.AddTasksToSprint(ctx, wang, cur.ID, []string{r1.ID, bug.ID, r2.ID, t3.ID}); err != nil {
		return "", err
	}
	if _, err := a.StartSprint(ctx, wang, cur.ID); err != nil {
		return "", err
	}
	next, err := a.CreateSprint(ctx, wang, CreateSprintInput{Name: "9 月第 2 迭代", Goal: "官网 v2.0 上线", TeamID: rd.ID, StartsOn: day(12), EndsOn: day(25)})
	if err != nil {
		return "", err
	}
	if _, err := a.AddTasksToSprint(ctx, wang, next.ID, []string{rel.ID}); err != nil {
		return "", err
	}

	// 里程碑（ADR 0016）：Q3 目标上一个已达到（日期已过）、一个未到；行政目标上一个逾期未确认
	reached, err := a.CreateMilestone(ctx, wang, CreateMilestoneInput{GoalID: q3.ID, Title: "登录页改版进入测试", Description: "登录页需求开发完成并提测。", DueOn: day(-3)})
	if err != nil {
		return "", err
	}
	if _, err := a.ReachMilestone(ctx, wang, reached.ID); err != nil {
		return "", err
	}
	if _, err := a.CreateMilestone(ctx, wang, CreateMilestoneInput{GoalID: q3.ID, Title: "官网 v2.0 上线", Description: "登录页与支付页一起随 v2.0 发布。", DueOn: day(30)}); err != nil {
		return "", err
	}
	if _, err := a.CreateMilestone(ctx, zhao, CreateMilestoneInput{GoalID: admin.ID, Title: "供应商合同归档完成", DueOn: day(-4)}); err != nil {
		return "", err
	}

	// 一条待确认操作：小李的 Agent 想把 Bug 转给小张，「指派」这项授权是需要人确认（ADR 0003）
	if _, err := a.Assign(ctx, liAgentSess, bug.ID, zhang.MemberID); err != nil {
		if _, ok := AsProposalPending(err); !ok {
			return "", err
		}
	}
	return org.ID, nil
}

func intp(n int) *int { return &n }

func hasRole(roles []string, name string) bool {
	for _, r := range roles {
		if r == name {
			return true
		}
	}
	return false
}
