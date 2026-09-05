package app

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// 需要 DATABASE_URL 指向一个可用的 PostgreSQL；没有就跳过。
func testApp(t *testing.T) (*App, context.Context) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("未设置 DATABASE_URL，跳过数据库集成测试")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	a := New(st)
	if err := a.EnsureGlobals(ctx); err != nil {
		t.Fatal(err)
	}
	return a, ctx
}

func sessionOfMember(ctx context.Context, t *testing.T, a *App, orgID, memberID string) *Session {
	var s *Session
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		ex, err := a.Store.ExecutorByID(ctx, tx, memberID)
		if err != nil {
			return err
		}
		s = &Session{OrgID: orgID, MemberID: memberID, Actor: ex}
		// 范围上下文（ADR 0013）：登录时会算，测试里的会话也要有，否则可见域是空的
		org, err := a.Store.OrganizationByID(ctx, tx, orgID)
		if err != nil {
			return err
		}
		return a.loadScopeContext(ctx, tx, s, org)
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

// 通过应用层走一遍通用任务：创建、指派、Agent 执行、提交、验收，并校验成本与隔离。
func TestGenericTaskThroughAppLayer(t *testing.T) {
	a, ctx := testApp(t)
	slug := "t-" + store.NewID("x")[2:10]
	org, err := a.Store.CreateOrganization(ctx, a.Store.Pool, slug, "测试组织", "CNY")
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := HashPassword("x")
	var owner, dev *domain.Member
	if err := a.Store.WithOrg(ctx, org.ID, func(tx pgx.Tx) error {
		acc1, _ := a.Store.CreateAccount(ctx, tx, slug+"-a@t.local", pw, "甲")
		acc2, _ := a.Store.CreateAccount(ctx, tx, slug+"-b@t.local", pw, "乙")
		owner, _ = a.Store.CreateMember(ctx, tx, org.ID, acc1.ID, "甲", []string{"admin"})
		dev, _ = a.Store.CreateMember(ctx, tx, org.ID, acc2.ID, "乙", []string{"developer"})
		return a.Store.SetOrganizationOwner(ctx, tx, org.ID, owner.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureOrgDefaults(ctx, org.ID); err != nil {
		t.Fatal(err)
	}
	jia := sessionOfMember(ctx, t, a, org.ID, owner.ID)
	yi := sessionOfMember(ctx, t, a, org.ID, dev.ID)

	ag, token, err := a.RegisterAgent(ctx, yi, RegisterAgentInput{Name: "乙的 Agent", Capabilities: []string{"coding"}, Grants: map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect}})
	if err != nil {
		t.Fatal(err)
	}
	agSess, err := a.SessionFromAgentToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if agSess.AgentID != ag.ID || agSess.MemberID != dev.ID {
		t.Fatalf("Agent 会话应绑定到乙，实际 %+v", agSess)
	}

	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "写文档", AssigneeID: dev.ID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if task.State != "todo" {
		t.Fatalf("Ready 创建后应为 todo，实际 %s", task.State)
	}
	if _, err := a.Transition(ctx, agSess, task.ID, "start", TransitionPayload{}); err != nil {
		t.Fatalf("Agent 开始: %v", err)
	}
	if _, err := a.Heartbeat(ctx, agSess, task.ID, []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 1_000_000, OutputTokens: 0}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, agSess, task.ID, "submit", TransitionPayload{}); err == nil {
		t.Fatal("无交付物提交应被拒")
	}
	if _, err := a.AddArtifact(ctx, agSess, task.ID, domain.Artifact{Type: "result", Title: "文档"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, agSess, task.ID, "submit", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, agSess, task.ID, "accept", TransitionPayload{}); err == nil {
		t.Fatal("Agent 无验收授权，验收应被拒")
	}
	if _, err := a.Transition(ctx, jia, task.ID, "accept", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	d, err := a.GetTaskDetail(ctx, jia, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.State.Label != domain.LabelTerminalSuccess || d.Progress != 100 {
		t.Fatalf("应已完成，实际 %+v", d.State)
	}
	// 100 万 input token × 3 USD × 7.2 = 21.6 CNY
	if d.Cost < 21.5 || d.Cost > 21.7 {
		t.Fatalf("成本应约 21.6 CNY，实际 %.2f", d.Cost)
	}
	if len(d.Runs) != 1 || d.Runs[0].ExecutorID != ag.ID {
		t.Fatalf("应有一段 Agent 的执行记录，实际 %+v", d.Runs)
	}

	// 行级安全：另一个组织看不到这个任务
	other, _ := a.Store.CreateOrganization(ctx, a.Store.Pool, slug+"-2", "别的组织", "CNY")
	var acc3 *domain.Account
	var m3 *domain.Member
	_ = a.Store.WithOrg(ctx, other.ID, func(tx pgx.Tx) error {
		acc3, _ = a.Store.CreateAccount(ctx, tx, slug+"-c@t.local", pw, "丙")
		m3, _ = a.Store.CreateMember(ctx, tx, other.ID, acc3.ID, "丙", nil)
		return nil
	})
	bing := sessionOfMember(ctx, t, a, other.ID, m3.ID)
	if _, err := a.GetTask(ctx, bing, task.ID); err == nil {
		t.Fatal("跨组织读取任务应失败")
	}
	tree, _ := a.GoalTree(ctx, bing)
	if len(tree) != 0 {
		t.Fatalf("别的组织不应看到目标，实际 %d 个", len(tree))
	}
}

// Agent 改任务字段同样受授权约束（ADR 0003）：与自己无关的任务改不了，越权字段改不了，
// 自己负责的任务在有「执行任务」授权时才能改。

// Agent 改任务字段同样受「所有者权限 ∩ 授权」约束（ADR 0003）：越权字段改不了，与自己无关的任务也改不了。
func TestAgentTaskEditIsGranted(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "编辑权限验证目标"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "编辑权限验证任务", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	_, tok, err := a.RegisterAgent(ctx, jia, RegisterAgentInput{Name: "编辑权限验证 Agent", Grants: map[domain.Grant]domain.GrantMode{domain.GrantComment: domain.GrantDirect}})
	if err != nil {
		t.Fatal(err)
	}
	as, err := a.SessionFromAgentToken(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}

	prio := 3
	if _, err := a.UpdateTask(ctx, as, task.ID, UpdateTaskInput{Priority: &prio}); err == nil {
		t.Fatal("Agent 改掉了不该由它决定的字段（优先级）")
	}
	title := "被越权改掉的标题"
	if _, err := a.UpdateTask(ctx, as, task.ID, UpdateTaskInput{Title: &title}); err == nil {
		t.Fatal("Agent 改掉了与自己无关的任务")
	}
	d, err := a.GetTaskDetail(ctx, jia, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Task.Title == title || d.Task.Priority == prio {
		t.Fatalf("任务被越权改动：%q 优先级 %d", d.Task.Title, d.Task.Priority)
	}
}
