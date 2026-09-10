package app

import (
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/domain"
)

// 用量自动采集（ADR 0030）：客户端报累计数 → 服务端算增量；没开执行记录时记为未归口，开着时归到那段执行记录；
// 重报同一份累计数不重复计；累计数变小不倒扣。
func TestReportAgentUsage(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	_, ag := agentSessionFor(t, a, ctx, yi, "记账 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect})

	if _, err := a.ReportAgentUsage(ctx, yi, UsageReportInput{Session: "s1", Cumulative: []domain.Usage{{ModelID: "m", InputTokens: 1}}}); err == nil || !strings.Contains(err.Error(), "只能由 Agent") {
		t.Fatalf("成员不能上报用量，实际 %v", err)
	}
	if _, err := a.ReportAgentUsage(ctx, ag, UsageReportInput{Cumulative: []domain.Usage{{ModelID: "m", InputTokens: 1}}}); err == nil || !strings.Contains(err.Error(), "会话 id") {
		t.Fatalf("没带会话 id 应拒绝，实际 %v", err)
	}
	// 没有开着的执行记录：未归口
	r1, err := a.ReportAgentUsage(ctx, ag, UsageReportInput{Session: "sess-a", Client: "claude-code", Cumulative: []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 1000, OutputTokens: 200}}})
	if err != nil || r1.DeltaTokens != 1200 || !r1.Unattributed || r1.TaskID != "" {
		t.Fatalf("首次上报应全部算增量且未归口: %+v %v", r1, err)
	}
	// 同一份累计数再报：增量 0
	r2, err := a.ReportAgentUsage(ctx, ag, UsageReportInput{Session: "sess-a", Cumulative: []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 1000, OutputTokens: 200}}})
	if err != nil || r2.DeltaTokens != 0 {
		t.Fatalf("重报应无增量: %+v %v", r2, err)
	}
	// 累计数变小（客户端重启、记录被截）：不倒扣
	r3, err := a.ReportAgentUsage(ctx, ag, UsageReportInput{Session: "sess-a", Cumulative: []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 500, OutputTokens: 100}}})
	if err != nil || r3.DeltaTokens != 0 {
		t.Fatalf("累计数变小不该倒扣: %+v %v", r3, err)
	}
	mm, err := a.MyMetricsOf(ctx, ag)
	if err != nil || mm.UnattributedTokens != 1200 || mm.Tokens != 0 {
		t.Fatalf("我的数字里应有 1200 未归口、0 归口: %+v %v", mm, err)
	}

	// 开着执行记录：增量归到它
	task, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "记账任务", AssigneeID: ag.Actor.ID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, ag, task.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	r4, err := a.ReportAgentUsage(ctx, ag, UsageReportInput{Session: "sess-a", Cumulative: []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 1600, OutputTokens: 300}, {ModelID: "claude-haiku-4-5", InputTokens: 50}}})
	if err != nil || r4.DeltaTokens != 750 || r4.Unattributed || r4.TaskID != task.ID || r4.TaskNumber != task.Number {
		t.Fatalf("有执行记录时应归口到它，增量 600+100+50: %+v %v", r4, err)
	}
	tm, err := a.TaskMetricsOf(ctx, jia, task.ID)
	if err != nil || tm.Tokens != 750 || len(tm.TokensByModel) != 2 {
		t.Fatalf("任务的用量应是这次的增量 750、两个模型: %+v %v", tm, err)
	}
	mm, err = a.MyMetricsOf(ctx, ag)
	if err != nil || mm.UnattributedTokens != 1200 || mm.Tokens != 750 {
		t.Fatalf("未归口仍是 1200、归口 750: %+v %v", mm, err)
	}
	// 另一个会话从头报：按它自己的累计算
	r5, err := a.ReportAgentUsage(ctx, ag, UsageReportInput{Session: "sess-b", Cumulative: []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: 10}}})
	if err != nil || r5.DeltaTokens != 10 || r5.TaskID != task.ID {
		t.Fatalf("新会话应从零算并归到开着的执行记录: %+v %v", r5, err)
	}
}
