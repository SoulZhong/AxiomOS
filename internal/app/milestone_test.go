package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// finishTask 把一个通用任务走到已完成：负责人开始、附交付物、提交，创建者验收。
func finishTask(t *testing.T, a *App, ctx context.Context, doer, reviewer *Session, taskID string) {
	t.Helper()
	if _, err := a.Transition(ctx, doer, taskID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddArtifact(ctx, doer, taskID, domain.Artifact{Type: "result", Title: "结果"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, doer, taskID, "submit", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, reviewer, taskID, "accept", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
}

func userErrText(t *testing.T, err error) string {
	t.Helper()
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("应是给用户看的错误，实际 %v", err)
	}
	return ue.Error()
}

// 里程碑全程：增改查、确认与撤销、状态与逾期、可以确认了的提示、目标树摘要、甘特图行、动态、删除。
func TestMilestoneLifecycle(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)

	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "上线新官网", PlannedStart: dayp(-10), PlannedEnd: dayp(30)})
	if err != nil {
		t.Fatal(err)
	}
	child, err := a.CreateGoal(ctx, jia, CreateGoalInput{ParentID: goal.ID, Title: "登录页"})
	if err != nil {
		t.Fatal(err)
	}

	// 校验：名称与日期都必填，理由是完整句子
	if _, err := a.CreateMilestone(ctx, jia, CreateMilestoneInput{GoalID: goal.ID, Title: " ", DueOn: dayp(5)}); err == nil {
		t.Fatal("空名称应被拒")
	} else if !strings.Contains(userErrText(t, err), "名称") {
		t.Fatalf("拒绝理由应说明名称问题，实际 %q", err)
	}
	if _, err := a.CreateMilestone(ctx, jia, CreateMilestoneInput{GoalID: goal.ID, Title: "无日期"}); err == nil {
		t.Fatal("缺日期应被拒")
	}
	if _, err := a.CreateMilestone(ctx, jia, CreateMilestoneInput{GoalID: "goal_nope", Title: "x", DueOn: dayp(5)}); err == nil {
		t.Fatal("目标不存在应被拒")
	}

	// 乙不是目标负责人也不是组织负责人：不能加
	if _, err := a.CreateMilestone(ctx, yi, CreateMilestoneInput{GoalID: goal.ID, Title: "乙想加的", DueOn: dayp(5)}); err == nil {
		t.Fatal("非目标负责人不应能新增里程碑")
	} else if !strings.Contains(userErrText(t, err), "目标负责人") {
		t.Fatalf("拒绝理由应是完整句子，实际 %q", err)
	}

	m1, err := a.CreateMilestone(ctx, jia, CreateMilestoneInput{GoalID: goal.ID, Title: "登录页提测", Description: "登录页开发完成", DueOn: dayp(5)})
	if err != nil {
		t.Fatal(err)
	}
	if m1.Status != domain.MilestoneUpcoming || m1.ReadyHint {
		t.Fatalf("新建的里程碑应为 upcoming 且没有提示，实际 %s %v", m1.Status, m1.ReadyHint)
	}
	if m1.CreatedBy != jia.MemberID || m1.DueOn.Hour() != 0 {
		t.Fatalf("创建者与日期归一不对：%+v", m1.Milestone)
	}
	m2, err := a.CreateMilestone(ctx, jia, CreateMilestoneInput{GoalID: goal.ID, Title: "整站上线", DueOn: dayp(25)})
	if err != nil {
		t.Fatal(err)
	}
	// 一个已经逾期的
	m0, err := a.CreateMilestone(ctx, jia, CreateMilestoneInput{GoalID: goal.ID, Title: "方案评审", DueOn: dayp(-2)})
	if err != nil {
		t.Fatal(err)
	}
	if m0.Status != domain.MilestoneOverdue {
		t.Fatalf("日期已过且未确认应为 overdue，实际 %s", m0.Status)
	}

	// 列表按日期升序，不含子目标的
	if _, err := a.CreateMilestone(ctx, jia, CreateMilestoneInput{GoalID: child.ID, Title: "子目标的刻度", DueOn: dayp(3)}); err != nil {
		t.Fatal(err)
	}
	list, err := a.ListMilestones(ctx, jia, goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].ID != m0.ID || list[1].ID != m1.ID || list[2].ID != m2.ID {
		t.Fatalf("应按日期升序列出 3 条，实际 %d 条", len(list))
	}

	// 提示：子目标里一个计划结束在 m1 日期前的任务，完成后 m1 才提示；m2 不受影响（还有别的任务没完）
	early, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: child.ID, Title: "登录页开发", AssigneeID: yi.MemberID, PlannedStart: dayp(-5), PlannedEnd: dayp(3), Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "全站回归", AssigneeID: yi.MemberID, PlannedStart: dayp(10), PlannedEnd: dayp(20), Ready: true}); err != nil {
		t.Fatal(err)
	}
	list, _ = a.ListMilestones(ctx, jia, goal.ID)
	if list[1].ReadyHint {
		t.Fatal("任务还没完成时不应提示可以确认")
	}
	finishTask(t, a, ctx, yi, jia, early.ID)
	list, _ = a.ListMilestones(ctx, jia, goal.ID)
	if !list[1].ReadyHint {
		t.Fatal("子目标里日期前的任务都完成后应提示可以确认")
	}
	if list[2].ReadyHint {
		t.Fatal("还有日期前未完成任务的里程碑不应提示")
	}
	if list[0].ReadyHint {
		t.Fatal("日期前没有任务的里程碑不应提示")
	}

	// 确认与撤销
	r, err := a.ReachMilestone(ctx, jia, m1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != domain.MilestoneReached || r.ReachedAt == nil || r.ReadyHint {
		t.Fatalf("确认后应为 reached，实际 %+v", r)
	}
	if _, err := a.ReachMilestone(ctx, jia, m1.ID); err == nil {
		t.Fatal("重复确认应被拒")
	} else if !strings.Contains(userErrText(t, err), "已经确认") {
		t.Fatalf("拒绝理由应是完整句子，实际 %q", err)
	}
	if _, err := a.UnreachMilestone(ctx, jia, m2.ID); err == nil {
		t.Fatal("没确认过的不能撤销")
	}
	if _, err := a.ReachMilestone(ctx, yi, m2.ID); err == nil {
		t.Fatal("非目标负责人不应能确认")
	}
	u, err := a.UnreachMilestone(ctx, jia, m1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Status != domain.MilestoneUpcoming || u.ReachedAt != nil {
		t.Fatalf("撤销后应回到 upcoming，实际 %+v", u)
	}
	// 逾期的确认后不再逾期
	if r, err := a.ReachMilestone(ctx, jia, m0.ID); err != nil || r.Status != domain.MilestoneReached {
		t.Fatalf("逾期的确认后应为 reached，实际 %v %v", r, err)
	}

	// 修改：改日期到过去 → 逾期
	title := "整站上线（改）"
	upd, err := a.UpdateMilestone(ctx, jia, m2.ID, UpdateMilestoneInput{Title: &title, DueOn: dayp(-1)})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Title != title || upd.Status != domain.MilestoneOverdue {
		t.Fatalf("修改后应改名并逾期，实际 %+v", upd)
	}
	empty := ""
	if _, err := a.UpdateMilestone(ctx, jia, m2.ID, UpdateMilestoneInput{Title: &empty}); err == nil {
		t.Fatal("改成空名称应被拒")
	}

	// 目标树：里程碑与摘要挂在目标上；子目标的不算进父目标摘要
	gv, err := a.GetGoal(ctx, jia, goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gv.Milestones) != 3 || gv.Milestones[0].ID != m0.ID {
		t.Fatalf("目标树应带 3 条按日期排序的里程碑，实际 %d", len(gv.Milestones))
	}
	s := gv.MilestoneSummary
	if s.Total != 3 || s.Reached != 1 || s.Overdue != 1 || s.Next == nil || s.Next.ID != m1.ID {
		t.Fatalf("摘要不对：%+v", s)
	}
	if len(gv.Children) != 1 || len(gv.Children[0].Milestones) != 1 || gv.Children[0].MilestoneSummary.Total != 1 {
		t.Fatalf("子目标应带自己的一条里程碑，实际 %+v", gv.Children)
	}

	// 甘特图按目标分组：目标行带里程碑
	rows, err := a.Gantt(ctx, jia, "goal")
	if err != nil {
		t.Fatal(err)
	}
	var row *GanttRow
	for _, r := range rows {
		if r.Key == goal.ID {
			row = r
		}
	}
	if row == nil || len(row.Milestones) != 3 || len(row.Children) != 1 || len(row.Children[0].Milestones) != 1 {
		t.Fatalf("甘特图目标行应带里程碑，实际 %+v", row)
	}

	// 删除：留下动态
	if err := a.DeleteMilestone(ctx, yi, m0.ID); err == nil {
		t.Fatal("非目标负责人不应能删")
	}
	if err := a.DeleteMilestone(ctx, jia, m0.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = a.ListMilestones(ctx, jia, goal.ID)
	if len(list) != 2 {
		t.Fatalf("删除后应剩 2 条，实际 %d", len(list))
	}
	if err := a.DeleteMilestone(ctx, jia, m0.ID); err == nil {
		t.Fatal("删除不存在的应报错")
	}

	// 每次写操作都有动态，数据里带目标标题
	events, err := a.Events(ctx, jia, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"MilestoneCreated": false, "MilestoneUpdated": false, "MilestoneReached": false, "MilestoneUnreached": false, "MilestoneDeleted": false}
	for _, e := range events {
		if _, ok := want[e.Type]; ok {
			want[e.Type] = true
			if e.Data["goal_title"] != goal.Title && e.Data["goal_title"] != child.Title {
				t.Fatalf("动态 %s 应带目标标题，实际 %+v", e.Type, e.Data)
			}
			if _, ok := e.Data["due_on"].(string); !ok {
				t.Fatalf("动态 %s 应带日期，实际 %+v", e.Type, e.Data)
			}
		}
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("缺少动态 %s", k)
		}
	}

	// 只剩里程碑的目标可以删，里程碑随之消失；有任务的目标仍然不能删
	if err := a.DeleteGoal(ctx, jia, goal.ID); err == nil {
		t.Fatal("有子目标和任务的目标不应能删")
	}
	lone, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "只有刻度的目标"})
	if err != nil {
		t.Fatal(err)
	}
	lm, err := a.CreateMilestone(ctx, jia, CreateMilestoneInput{GoalID: lone.ID, Title: "刻度", DueOn: dayp(1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteGoal(ctx, jia, lone.ID); err != nil {
		t.Fatalf("只有里程碑的目标应能删，实际 %v", err)
	}
	if err := a.Store.WithOrg(ctx, jia.OrgID, func(tx pgx.Tx) error {
		_, err := a.Store.MilestoneByID(ctx, tx, lm.ID)
		return err
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("目标删除后里程碑应随之删除，实际 %v", err)
	}
}

// Agent 对里程碑的写操作受「创建目标」授权约束：直接生效的能做；需要人确认的转成待确认操作；没有授权的被拒。
// 有效权限 = 所有者权限 ∩ 授权：所有者本人不能编辑的目标，Agent 也不能。
func TestMilestoneAgentGating(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "Agent 参与的目标"})
	if err != nil {
		t.Fatal(err)
	}

	// 直接生效
	_, direct := agentSessionFor(t, a, ctx, jia, "甲的直接 Agent", map[domain.Grant]domain.GrantMode{domain.GrantCreateGoal: domain.GrantDirect})
	m, err := a.CreateMilestone(ctx, direct, CreateMilestoneInput{GoalID: goal.ID, Title: "Agent 加的", DueOn: dayp(7)})
	if err != nil {
		t.Fatalf("有直接生效授权的 Agent 应能新增，实际 %v", err)
	}
	if m.CreatedBy != direct.Actor.ID {
		t.Fatalf("创建者应记为 Agent，实际 %s", m.CreatedBy)
	}

	// 需要人确认：只记不改
	_, approval := agentSessionFor(t, a, ctx, jia, "甲的待确认 Agent", map[domain.Grant]domain.GrantMode{domain.GrantCreateGoal: domain.GrantWithApproval})
	_, err = a.CreateMilestone(ctx, approval, CreateMilestoneInput{GoalID: goal.ID, Title: "等确认的", DueOn: dayp(9)})
	pp := mustPending(t, err)
	if pp.Proposal.Action != ActionMilestoneCreate || pp.Proposal.TargetKind != "goal" || pp.Proposal.TargetID != goal.ID {
		t.Fatalf("应是新增里程碑的待确认操作，指向目标，实际 %+v", pp.Proposal.Proposal)
	}
	if !strings.Contains(pp.Proposal.SummaryText, goal.Title) || !strings.Contains(pp.Proposal.SummaryText, "等确认的") {
		t.Fatalf("摘要应带目标与里程碑名称，实际 %q", pp.Proposal.SummaryText)
	}
	list, _ := a.ListMilestones(ctx, jia, goal.ID)
	if len(list) != 1 {
		t.Fatalf("待确认期间不应新增，实际 %d 条", len(list))
	}
	res, err := a.ApproveProposal(ctx, jia, pp.Proposal.ID)
	if err != nil {
		t.Fatalf("确认应成功，实际 %v", err)
	}
	created, ok := res.Result.(*MilestoneView)
	if !ok || created.Title != "等确认的" {
		t.Fatalf("确认后应真的建出来，实际 %+v", res.Result)
	}
	list, _ = a.ListMilestones(ctx, jia, goal.ID)
	if len(list) != 2 {
		t.Fatalf("确认后应有 2 条，实际 %d", len(list))
	}

	// 确认已达到：同样走待确认操作
	_, err = a.ReachMilestone(ctx, approval, created.ID)
	rp := mustPending(t, err)
	if rp.Proposal.Action != ActionMilestoneReach {
		t.Fatalf("应是确认里程碑的待确认操作，实际 %s", rp.Proposal.Action)
	}
	if got, _ := a.ListMilestones(ctx, jia, goal.ID); got[1].Reached() {
		t.Fatal("待确认期间不应标为已达到")
	}
	if _, err := a.ApproveProposal(ctx, jia, rp.Proposal.ID); err != nil {
		t.Fatalf("确认应成功，实际 %v", err)
	}
	if got, _ := a.ListMilestones(ctx, jia, goal.ID); !got[1].Reached() {
		t.Fatal("确认后应标为已达到")
	}
	// 修改与删除也走待确认操作
	desc := "补个说明"
	_, err = a.UpdateMilestone(ctx, approval, created.ID, UpdateMilestoneInput{Description: &desc})
	if up := mustPending(t, err); up.Proposal.Action != ActionMilestoneUpdate {
		t.Fatalf("应是修改里程碑的待确认操作，实际 %s", up.Proposal.Action)
	}
	err = a.DeleteMilestone(ctx, approval, created.ID)
	dp := mustPending(t, err)
	if dp.Proposal.Action != ActionMilestoneDelete {
		t.Fatalf("应是删除里程碑的待确认操作，实际 %s", dp.Proposal.Action)
	}
	if _, err := a.ApproveProposal(ctx, jia, dp.Proposal.ID); err != nil {
		t.Fatalf("确认删除应成功，实际 %v", err)
	}
	if got, _ := a.ListMilestones(ctx, jia, goal.ID); len(got) != 1 {
		t.Fatalf("确认删除后应剩 1 条，实际 %d", len(got))
	}

	// 没有授权：一句完整的拒绝
	_, none := agentSessionFor(t, a, ctx, jia, "甲的无权 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect})
	_, err = a.CreateMilestone(ctx, none, CreateMilestoneInput{GoalID: goal.ID, Title: "不该成功", DueOn: dayp(1)})
	if err == nil {
		t.Fatal("没有「创建目标」授权的 Agent 应被拒")
	}
	if _, pending := AsProposalPending(err); pending {
		t.Fatal("没有授权时不应转成待确认操作")
	}
	if msg := userErrText(t, err); !strings.Contains(msg, "创建目标") {
		t.Fatalf("拒绝理由应点名缺的授权，实际 %q", msg)
	}
	if _, err := a.ReachMilestone(ctx, none, m.ID); err == nil {
		t.Fatal("没有授权的 Agent 不应能确认")
	}

	// 所有者权限 ∩ 授权：乙不能编辑甲的目标，乙的 Agent 即使有直接授权也不能
	_, yiAgent := agentSessionFor(t, a, ctx, yi, "乙的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantCreateGoal: domain.GrantDirect})
	if _, err := a.CreateMilestone(ctx, yiAgent, CreateMilestoneInput{GoalID: goal.ID, Title: "越权", DueOn: dayp(1)}); err == nil {
		t.Fatal("乙的 Agent 不应能改甲的目标的里程碑")
	}
	// 读是开放的
	if got, err := a.ListMilestones(ctx, yiAgent, goal.ID); err != nil || len(got) != 1 {
		t.Fatalf("Agent 应能读里程碑，实际 %v %d", err, len(got))
	}
}
