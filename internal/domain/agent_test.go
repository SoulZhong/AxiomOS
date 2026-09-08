package domain

import (
	"testing"
	"time"
)

func ago(d time.Duration) *time.Time { t := time.Now().Add(d); return &t }

// Agent 状态（CONTEXT.md「Agent 状态」）：HTTP MCP 的 Agent 没活干时本来就不说话，
// 所以「最近没心跳」是正常的休息状态（可用），不是故障。
func TestAgentState(t *testing.T) {
	now := time.Now()
	owner := true
	cases := []struct {
		name       string
		agent      Agent
		activeRuns int
		ownerOK    bool
		want       AgentState
	}{
		{"有打开的执行记录就是执行中", Agent{LastSeenAt: ago(-5 * time.Hour)}, 1, owner, AgentRunning},
		{"刚刚有过请求也是执行中", Agent{LastSeenAt: ago(-30 * time.Second)}, 0, owner, AgentRunning},
		{"两小时没说话、手上没活：可用", Agent{LastSeenAt: ago(-2 * time.Hour)}, 0, owner, AgentReady},
		{"从没说过话也是可用", Agent{}, 0, owner, AgentReady},
		{"已注销：已停用", Agent{RevokedAt: ago(-time.Minute), LastSeenAt: ago(-time.Second)}, 1, owner, AgentInactive},
		{"所有者已停用：已停用", Agent{LastSeenAt: ago(-time.Second)}, 1, false, AgentInactive},
	}
	for _, c := range cases {
		if got := c.agent.State(now, c.activeRuns, c.ownerOK); got != c.want {
			t.Errorf("%s：想要 %s，实际 %s", c.name, c.want, got)
		}
	}
}

// 最近活动 = 心跳与最近一次工具调用里更晚的那个。
func TestAgentLastActiveAt(t *testing.T) {
	seen, tool := ago(-time.Hour), ago(-time.Minute)
	if got := (&Agent{LastSeenAt: seen, LastToolAt: tool}).LastActiveAt(); got != tool {
		t.Errorf("工具调用更晚时应取工具调用，实际 %v", got)
	}
	if got := (&Agent{LastSeenAt: tool, LastToolAt: seen}).LastActiveAt(); got != tool {
		t.Errorf("心跳更晚时应取心跳，实际 %v", got)
	}
	if got := (&Agent{LastSeenAt: seen}).LastActiveAt(); got != seen {
		t.Errorf("只有心跳时应取心跳，实际 %v", got)
	}
	if got := (&Agent{LastToolAt: tool}).LastActiveAt(); got != tool {
		t.Errorf("只有工具调用时应取工具调用，实际 %v", got)
	}
	if got := (&Agent{}).LastActiveAt(); got != nil {
		t.Errorf("都没有时应为空，实际 %v", got)
	}
}
