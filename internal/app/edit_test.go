package app

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// goalEvents 取某个目标名下的 GoalFieldChanged 动态（新的在前），只看 field。
func goalEvents(t *testing.T, a *App, ctx context.Context, sess *Session, goalID string) []*store.EventRow {
	t.Helper()
	rows, err := a.Events(ctx, sess, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	var out []*store.EventRow
	for _, e := range rows {
		if e.Type == "GoalFieldChanged" && e.Data["goal_id"] == goalID {
			out = append(out, e)
		}
	}
	return out
}

func taskEvents(t *testing.T, a *App, ctx context.Context, sess *Session, taskID, kind string) []*store.EventRow {
	t.Helper()
	rows, err := a.Events(ctx, sess, taskID, 200)
	if err != nil {
		t.Fatal(err)
	}
	var out []*store.EventRow
	for _, e := range rows {
		if e.Type == kind {
			out = append(out, e)
		}
	}
	return out
}

func fieldsOf(rows []*store.EventRow) []string {
	out := make([]string, 0, len(rows))
	for _, e := range rows {
		f, _ := e.Data["field"].(string)
		out = append(out, f)
	}
	return out
}

// 目标就地编辑：每个字段一条动态（带旧值新值与名字）、换上级、成环、提为顶级、深层链、清空预算、截止日跟随计划结束。
func TestGoalInPlaceEdit(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)

	root, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "年度目标"})
	if err != nil {
		t.Fatal(err)
	}
	mid, err := a.CreateGoal(ctx, jia, CreateGoalInput{ParentID: root.ID, Title: "季度目标"})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := a.CreateGoal(ctx, jia, CreateGoalInput{ParentID: mid.ID, Title: "月度目标"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "另一个顶级目标"})
	if err != nil {
		t.Fatal(err)
	}

	// 一次改标题 + 负责人 + 预算 + 计划结束（接口层会把截止日一起送来）：四条动态，截止日不重复记
	budget := 5000.0
	g, err := a.UpdateGoal(ctx, jia, mid.ID, UpdateGoalInput{Title: strp("Q3 目标"), OwnerMemberID: &yi.MemberID, Budget: &budget, PlannedEnd: dayp(20), Deadline: dayp(20)})
	if err != nil {
		t.Fatal(err)
	}
	if g.Title != "Q3 目标" || g.OwnerMemberID != yi.MemberID || g.Budget == nil || *g.Budget != 5000 || g.Deadline == nil {
		t.Fatalf("字段没改对：%+v", g)
	}
	evs := goalEvents(t, a, ctx, jia, mid.ID)
	if len(evs) != 4 {
		t.Fatalf("应有 4 条 GoalFieldChanged，实际 %d：%v", len(evs), fieldsOf(evs))
	}
	got := strings.Join(fieldsOf(evs), ",")
	for _, f := range []string{"title", "owner_member_id", "budget", "planned_end"} {
		if !strings.Contains(got, f) {
			t.Fatalf("缺少字段 %s 的动态：%s", f, got)
		}
	}
	if strings.Contains(got, "deadline") {
		t.Fatalf("截止日随计划结束一起改时不应单独记动态：%s", got)
	}
	for _, e := range evs {
		switch e.Data["field"] {
		case "title":
			if e.Data["from"] != "季度目标" || e.Data["to"] != "Q3 目标" {
				t.Fatalf("标题动态旧值新值不对：%v", e.Data)
			}
		case "owner_member_id":
			if e.Data["from"] != jia.MemberID || e.Data["to"] != yi.MemberID || e.Data["from_title"] != "甲" || e.Data["to_title"] != "乙" {
				t.Fatalf("负责人动态应带 ID 与名字：%v", e.Data)
			}
		case "budget":
			if e.Data["from"] != nil || e.Data["to"] != 5000.0 || e.Data["currency"] != "CNY" {
				t.Fatalf("预算动态应带货币：%v", e.Data)
			}
		case "planned_end":
			if e.Data["from"] != nil || e.Data["to"] != dayp(20).Format("2006-01-02") {
				t.Fatalf("计划结束动态应记 YYYY-MM-DD：%v", e.Data)
			}
		}
		if e.Data["title"] != "Q3 目标" || e.Data["goal_id"] != mid.ID {
			t.Fatalf("动态应带目标 ID 与标题：%v", e.Data)
		}
	}
	// 同样的值再送一次：没有变化就没有动态
	if _, err := a.UpdateGoal(ctx, jia, mid.ID, UpdateGoalInput{Title: strp("Q3 目标"), Budget: &budget}); err != nil {
		t.Fatal(err)
	}
	if n := len(goalEvents(t, a, ctx, jia, mid.ID)); n != 4 {
		t.Fatalf("没有变化不应产生动态，实际 %d", n)
	}
	// 清空预算
	if _, err := a.UpdateGoal(ctx, jia, mid.ID, UpdateGoalInput{Clear: []string{"budget"}}); err != nil {
		t.Fatal(err)
	}
	evs = goalEvents(t, a, ctx, jia, mid.ID)
	if len(evs) != 5 || evs[0].Data["field"] != "budget" || evs[0].Data["to"] != nil || evs[0].Data["from"] != 5000.0 {
		t.Fatalf("清空预算应记一条 to=nil 的动态：%v", evs[0].Data)
	}

	// 成环：挂到自己 / 挂到子孙
	if _, err := a.UpdateGoal(ctx, jia, mid.ID, UpdateGoalInput{ParentID: &mid.ID}); err == nil || !strings.Contains(userErrText(t, err), "自己") {
		t.Fatalf("挂到自己应被拒，实际 %v", err)
	}
	if _, err := a.UpdateGoal(ctx, jia, root.ID, UpdateGoalInput{ParentID: &leaf.ID}); err == nil || !strings.Contains(userErrText(t, err), "子目标") {
		t.Fatalf("挂到子孙应被拒，实际 %v", err)
	}
	if _, err := a.UpdateGoal(ctx, jia, mid.ID, UpdateGoalInput{ParentID: strp("goal_nope")}); err == nil {
		t.Fatal("不存在的上级应被拒")
	}
	// 换上级：整棵子树跟着走
	if _, err := a.UpdateGoal(ctx, jia, mid.ID, UpdateGoalInput{ParentID: &other.ID}); err != nil {
		t.Fatal(err)
	}
	tree, err := a.GoalTree(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	var find func(vs []*GoalView, id string) *GoalView
	find = func(vs []*GoalView, id string) *GoalView {
		for _, v := range vs {
			if v.ID == id {
				return v
			}
			if f := find(v.Children, id); f != nil {
				return f
			}
		}
		return nil
	}
	ov := find(tree, other.ID)
	if ov == nil || len(ov.Children) != 1 || ov.Children[0].ID != mid.ID || len(ov.Children[0].Children) != 1 || ov.Children[0].Children[0].ID != leaf.ID {
		t.Fatalf("换上级后子树应完整跟随：%+v", ov)
	}
	evs = goalEvents(t, a, ctx, jia, mid.ID)
	if evs[0].Data["field"] != "parent_id" || evs[0].Data["from"] != root.ID || evs[0].Data["to"] != other.ID || evs[0].Data["from_title"] != "年度目标" || evs[0].Data["to_title"] != "另一个顶级目标" {
		t.Fatalf("换上级动态应带两端的标题：%v", evs[0].Data)
	}
	// 提为顶级
	if _, err := a.UpdateGoal(ctx, jia, mid.ID, UpdateGoalInput{ParentID: strp("")}); err != nil {
		t.Fatal(err)
	}
	if gv, _ := a.GetGoal(ctx, jia, mid.ID); gv.ParentID != "" {
		t.Fatalf("应提为顶级，实际 parent=%s", gv.ParentID)
	}
	if evs = goalEvents(t, a, ctx, jia, mid.ID); evs[0].Data["to"] != nil || evs[0].Data["from"] != other.ID {
		t.Fatalf("提为顶级的动态 to 应为空：%v", evs[0].Data)
	}
	// 深层链：目标树没有深度上限，五层之下照样能挂
	cur := other.ID
	for i := 0; i < 5; i++ {
		c, err := a.CreateGoal(ctx, jia, CreateGoalInput{ParentID: cur, Title: "层"})
		if err != nil {
			t.Fatal(err)
		}
		cur = c.ID
	}
	if _, err := a.UpdateGoal(ctx, jia, mid.ID, UpdateGoalInput{ParentID: &cur}); err != nil {
		t.Fatalf("深层挂接应允许：%v", err)
	}
	// 不相关的人不能改
	if _, err := a.UpdateGoal(ctx, yi, root.ID, UpdateGoalInput{Title: strp("越权")}); err == nil {
		t.Fatal("非负责人改目标应被拒")
	}
	// 达成：状态动态
	achieved := domain.GoalAchieved
	if _, err := a.UpdateGoal(ctx, jia, leaf.ID, UpdateGoalInput{Status: &achieved}); err != nil {
		t.Fatal(err)
	}
	if evs = goalEvents(t, a, ctx, jia, leaf.ID); len(evs) != 1 || evs[0].Data["field"] != "status" || evs[0].Data["to"] != "achieved" {
		t.Fatalf("达成应记 status 动态：%v", fieldsOf(evs))
	}
}

// 任务就地编辑：每个字段一条动态、换上级（成环 / 已结束）、已结束只能改描述与字段、人的编辑权、Agent 禁改字段。
func TestTaskInPlaceEdit(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)

	// 第三个人：与任务无关
	var bing *domain.Member
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		pw, _ := HashPassword("x")
		acc, err := a.Store.CreateAccount(ctx, tx, store.NewID("c")+"@t.local", pw, "丙")
		if err != nil {
			return err
		}
		bing, err = a.Store.CreateMember(ctx, tx, orgID, acc.ID, "丙", []string{"developer"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	bingSess := sessionOfMember(ctx, t, a, orgID, bing.ID)
	bingSess.Permissions = map[string]bool{}

	g1, _ := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标一"})
	g2, _ := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标二"})
	parent, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: g1.ID, Title: "父任务", Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	child, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: g1.ID, ParentID: parent.ID, Title: "子任务", Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: g1.ID, Title: "别的任务", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}

	// 一次改多个字段：每个字段一条 TaskFieldChanged
	prio, est := 0, 8.0
	upd, err := a.UpdateTask(ctx, jia, parent.ID, UpdateTaskInput{
		Title: strp("父任务·改"), Priority: &prio, EstimateHours: &est, PlannedEnd: dayp(7), HumanOnly: boolp(true),
		GoalID: &g2.ID, ReviewerID: &yi.MemberID, RequiredCapabilities: &[]string{"coding"}, Description: strp("说明"),
		Fields: map[string]any{"备注": "第一版"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Priority != 0 || upd.GoalID != g2.ID || upd.ReviewerID != yi.MemberID || len(upd.RequiredCapabilities) != 1 || !upd.HumanOnly {
		t.Fatalf("字段没改对：%+v", upd)
	}
	evs := taskEvents(t, a, ctx, jia, parent.ID, "TaskFieldChanged")
	fields := strings.Join(fieldsOf(evs), ",")
	for _, f := range []string{"title", "priority", "estimate_hours", "planned_end", "human_only", "goal_id", "reviewer_id", "required_capabilities", "description", "fields"} {
		if !strings.Contains(fields, f) {
			t.Fatalf("缺少字段 %s 的动态：%s", f, fields)
		}
	}
	if len(evs) != 10 {
		t.Fatalf("应有 10 条动态，实际 %d：%s", len(evs), fields)
	}
	if n := len(taskEvents(t, a, ctx, jia, parent.ID, "TaskUpdated")); n != 0 {
		t.Fatalf("不应再记笼统的 TaskUpdated，实际 %d", n)
	}
	for _, e := range evs {
		if e.Data["title"] != "父任务·改" || e.Data["goal_id"] != g2.ID {
			t.Fatalf("动态应带任务标题与目标：%v", e.Data)
		}
		switch e.Data["field"] {
		case "priority":
			if e.Data["from"] != 2.0 || e.Data["to"] != 0.0 {
				t.Fatalf("优先级旧值新值不对：%v", e.Data)
			}
		case "goal_id":
			if e.Data["from_title"] != "目标一" || e.Data["to_title"] != "目标二" {
				t.Fatalf("归属目标动态应带标题：%v", e.Data)
			}
		case "reviewer_id":
			if e.Data["from_title"] != "甲" || e.Data["to_title"] != "乙" {
				t.Fatalf("验收人动态应带名字：%v", e.Data)
			}
		case "required_capabilities":
			if to, _ := e.Data["to"].([]any); len(to) != 1 || to[0] != "coding" {
				t.Fatalf("所需能力动态 to 应为列表：%v", e.Data)
			}
		case "fields":
			if e.Data["key"] != "备注" || e.Data["to"] != "第一版" {
				t.Fatalf("自定义字段动态应带键：%v", e.Data)
			}
		}
	}
	// 未知能力标签被拒
	if _, err := a.UpdateTask(ctx, jia, parent.ID, UpdateTaskInput{RequiredCapabilities: &[]string{"nope"}}); err == nil {
		t.Fatal("未知能力标签应被拒")
	}

	// 换上级：成环
	if _, err := a.UpdateTask(ctx, jia, parent.ID, UpdateTaskInput{ParentID: &child.ID}); err == nil || !strings.Contains(userErrText(t, err), "子任务") {
		t.Fatalf("挂到子孙应被拒，实际 %v", err)
	}
	if _, err := a.UpdateTask(ctx, jia, parent.ID, UpdateTaskInput{ParentID: &parent.ID}); err == nil {
		t.Fatal("挂到自己应被拒")
	}
	// 换上级 → 别的任务；再提为顶级
	if _, err := a.UpdateTask(ctx, jia, child.ID, UpdateTaskInput{ParentID: &other.ID}); err != nil {
		t.Fatal(err)
	}
	evs = taskEvents(t, a, ctx, jia, child.ID, "TaskFieldChanged")
	if evs[0].Data["field"] != "parent_id" || evs[0].Data["from"] != parent.ID || evs[0].Data["to"] != other.ID || evs[0].Data["to_title"] != "别的任务" {
		t.Fatalf("换上级动态不对：%v", evs[0].Data)
	}
	if _, err := a.UpdateTask(ctx, jia, child.ID, UpdateTaskInput{ParentID: strp("")}); err != nil {
		t.Fatal(err)
	}
	if c, _ := a.GetTask(ctx, jia, child.ID); c.ParentID != "" {
		t.Fatalf("应提为顶级，实际 %s", c.ParentID)
	}

	// 已结束的任务：不能再挂子任务；自己只能改描述与字段
	finishTask(t, a, ctx, yi, jia, other.ID)
	if _, err := a.UpdateTask(ctx, jia, child.ID, UpdateTaskInput{ParentID: &other.ID}); err == nil || !strings.Contains(userErrText(t, err), "已结束") {
		t.Fatalf("已结束的任务不能挂子任务，实际 %v", err)
	}
	if _, err := a.UpdateTask(ctx, jia, other.ID, UpdateTaskInput{Title: strp("改标题")}); err == nil || !strings.Contains(userErrText(t, err), "只能改描述") {
		t.Fatalf("已结束任务改标题应被拒，实际 %v", err)
	}
	if _, err := a.UpdateTask(ctx, jia, other.ID, UpdateTaskInput{PlannedEnd: dayp(3)}); err == nil {
		t.Fatal("已结束任务改计划应被拒")
	}
	p := 3
	if _, err := a.UpdateTask(ctx, jia, other.ID, UpdateTaskInput{Points: &p, SetPoints: true}); err == nil {
		t.Fatal("已结束任务改工作量应被拒")
	}
	if _, err := a.UpdateTask(ctx, jia, other.ID, UpdateTaskInput{Description: strp("补充说明"), Fields: map[string]any{"复盘": "顺利"}}); err != nil {
		t.Fatalf("已结束任务改描述与字段应允许：%v", err)
	}
	// 已结束任务送来未变的标题不算改
	if _, err := a.UpdateTask(ctx, jia, other.ID, UpdateTaskInput{Title: strp("别的任务")}); err != nil {
		t.Fatalf("未变化的字段不应触发已结束限制：%v", err)
	}

	// 人的编辑权：无关的人不能改；负责人可以
	if _, err := a.UpdateTask(ctx, bingSess, parent.ID, UpdateTaskInput{Title: strp("越权")}); err == nil || !strings.Contains(userErrText(t, err), "负责人") {
		t.Fatalf("与任务无关的人改任务应被拒，实际 %v", err)
	}
	if _, err := a.Assign(ctx, jia, parent.ID, bing.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTask(ctx, bingSess, parent.ID, UpdateTaskInput{Title: strp("负责人改的")}); err != nil {
		t.Fatalf("负责人应能改：%v", err)
	}
	// 目标负责人也能改目标下的任务
	if _, err := a.UpdateGoal(ctx, jia, g2.ID, UpdateGoalInput{OwnerMemberID: &yi.MemberID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTask(ctx, yi, parent.ID, UpdateTaskInput{Description: strp("目标负责人改的")}); err != nil {
		t.Fatalf("所属目标的负责人应能改：%v", err)
	}

	// Agent：上级与所需能力也在禁改之列
	_, tok, err := a.RegisterAgent(ctx, jia, RegisterAgentInput{Name: "编辑 Agent", Grants: map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect}})
	if err != nil {
		t.Fatal(err)
	}
	as, err := a.SessionFromAgentToken(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: g1.ID, Title: "Agent 的任务", AssigneeID: as.Actor.ID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTask(ctx, as, mine.ID, UpdateTaskInput{ParentID: &parent.ID}); err == nil || !strings.Contains(userErrText(t, err), "上级任务") {
		t.Fatalf("Agent 改上级应被拒，实际 %v", err)
	}
	if _, err := a.UpdateTask(ctx, as, mine.ID, UpdateTaskInput{RequiredCapabilities: &[]string{"coding"}}); err == nil {
		t.Fatal("Agent 改所需能力应被拒")
	}
	if _, err := a.UpdateTask(ctx, as, mine.ID, UpdateTaskInput{Title: strp("Agent 改标题"), Clear: []string{"estimate_hours"}}); err != nil {
		t.Fatalf("Agent 改自己任务的标题应允许：%v", err)
	}
	if evs = taskEvents(t, a, ctx, jia, mine.ID, "TaskFieldChanged"); len(evs) != 1 || evs[0].Data["field"] != "title" {
		t.Fatalf("Agent 改标题应记一条动态（预估工时本来为空，清空不算变化）：%v", fieldsOf(evs))
	}
}
