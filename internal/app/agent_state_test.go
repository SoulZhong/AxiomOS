package app

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
)

func backdateSeen(t *testing.T, a *App, agentID string, d time.Duration) {
	t.Helper()
	if _, err := a.Store.Pool.Exec(t.Context(), `update agents set last_seen_at = now() - $2::interval where id=$1`, agentID, d.String()); err != nil {
		t.Fatal(err)
	}
}

// stateOf 重新读一遍这个 Agent 再算状态。注销过的 Agent 不在列表里（列表只给没注销的），
// 所以直接按 ID 取。
func stateOf(t *testing.T, a *App, sess *Session, id string) AgentStatus {
	t.Helper()
	var ag *domain.Agent
	if err := a.Store.WithOrg(t.Context(), sess.OrgID, func(tx pgx.Tx) error {
		var err error
		ag, err = a.Store.AgentByID(t.Context(), tx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	m, err := a.AgentStates(t.Context(), sess, []*domain.Agent{ag})
	if err != nil {
		t.Fatal(err)
	}
	return m[id]
}

// Agent 状态（CONTEXT.md「Agent 状态」）：HTTP MCP 的 Agent 只在有活干时说话，
// 所以「几小时没心跳」是可用，不是故障；有打开的执行记录才是执行中。
func TestAgentStates(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)

	ag, token, err := a.RegisterAgent(ctx, yi, RegisterAgentInput{Name: "乙的 Claude Code",
		Grants: map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect}})
	if err != nil {
		t.Fatal(err)
	}
	// 刚注册、还没说过话：可用
	if st := stateOf(t, a, yi, ag.ID); st.State != domain.AgentReady || st.Title != "可用" {
		t.Fatalf("刚注册应是可用，实际 %+v", st)
	}
	// 令牌一用就是一次心跳：刚刚还在说话 → 执行中
	agSess, err := a.SessionFromAgentToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if st := stateOf(t, a, yi, ag.ID); st.State != domain.AgentRunning || st.Title != "执行中" {
		t.Fatalf("刚有过请求应是执行中，实际 %+v", st)
	}
	// 两小时没请求、手上也没活：可用（这正是所有者报的那个「离线」）
	backdateSeen(t, a, ag.ID, 2*time.Hour)
	if st := stateOf(t, a, yi, ag.ID); st.State != domain.AgentReady {
		t.Fatalf("两小时没心跳、没在干活应是可用，实际 %+v", st)
	}
	// 最近活动取心跳与最近一次工具调用里更晚的那个
	a.RecordAgentTool(ctx, agSess, "whoami")
	st := stateOf(t, a, yi, ag.ID)
	if st.LastActiveAt == nil || time.Since(*st.LastActiveAt) > time.Minute {
		t.Fatalf("最近活动应取更晚的工具调用时间，实际 %+v", st.LastActiveAt)
	}
	// 有打开的执行记录：即使心跳是两小时前也算执行中
	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "写文档", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, agSess, task.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	backdateSeen(t, a, ag.ID, 2*time.Hour)
	if st := stateOf(t, a, yi, ag.ID); st.State != domain.AgentRunning {
		t.Fatalf("有打开的执行记录应是执行中，实际 %+v", st)
	}
	// 注销：已停用
	if err := a.RevokeAgent(ctx, yi, ag.ID); err != nil {
		t.Fatal(err)
	}
	if st := stateOf(t, a, yi, ag.ID); st.State != domain.AgentInactive || st.Title != "已停用" {
		t.Fatalf("注销后应是已停用，实际 %+v", st)
	}

	// 所有者被停用：名下的 Agent 也是已停用
	other, _, err := a.RegisterAgent(ctx, yi, RegisterAgentInput{Name: "乙的第二个 Agent"})
	if err != nil {
		t.Fatal(err)
	}
	no := false
	if _, err := a.UpdateMember(ctx, jia, yi.MemberID, MemberPatch{Active: &no}); err != nil {
		t.Fatal(err)
	}
	if st := stateOf(t, a, jia, other.ID); st.State != domain.AgentInactive {
		t.Fatalf("所有者停用后应是已停用，实际 %+v", st)
	}
}
