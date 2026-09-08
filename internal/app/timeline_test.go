package app

import (
	"strings"
	"testing"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

// onDay 把一个日期念成 YYYY-MM-DD。按它自己的时区念：刚写进去的还带着本地零点，
// 从库里读回来的是 date 列的 UTC 零点，两种都要念成同一个日子。
func onDay(tp *time.Time) string {
	if tp == nil {
		return "<空>"
	}
	return tp.Format("2006-01-02")
}

func day(t *testing.T, s string) *time.Time {
	t.Helper()
	tt, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return &tt
}

// 时间粒度（ADR 0022）：创建与就地编辑都只认五档，week 与空等价，改动各记一条动态。
func TestGoalDatePrecision(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)

	g, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "第四季度把官网上线",
		PlannedStart: day(t, "2026-10-15"), PlannedEnd: day(t, "2026-11-20"), DatePrecision: domain.PrecisionQuarter})
	if err != nil {
		t.Fatal(err)
	}
	if g.DatePrecision != domain.PrecisionQuarter {
		t.Fatalf("粒度没存上：%q", g.DatePrecision)
	}
	// 到日已经不是一档：路线图最细就到周
	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "x", DatePrecision: "day"}); err == nil {
		t.Fatal("到日应被拒")
	} else if s := userErrText(t, err); !strings.Contains(s, "时间粒度只有到周、到月、到季度、到半年、到年这几档") {
		t.Fatalf("理由不对：%s", s)
	}

	// 在时间线上拖条 = 一次普通 PATCH：改日期并把粒度落实到周
	week := domain.PrecisionWeek
	after, err := a.UpdateGoal(ctx, jia, g.ID, UpdateGoalInput{DatePrecision: &week,
		PlannedStart: day(t, "2026-10-19"), PlannedEnd: day(t, "2026-10-30")})
	if err != nil {
		t.Fatal(err)
	}
	if after.DatePrecision != "" {
		t.Fatalf("到周与空是同一件事，应统一存空，实际 %q", after.DatePrecision)
	}
	if onDay(after.PlannedStart) != "2026-10-19" {
		t.Fatalf("计划起止应原样存着，不被吸附改写：%s", onDay(after.PlannedStart))
	}
	var prec, dates int
	for _, e := range goalEvents(t, a, ctx, jia, g.ID) {
		switch e.Data["field"] {
		case "date_precision":
			prec++
			if e.Data["from"] != "quarter" || e.Data["to"] != nil {
				t.Fatalf("粒度动态应记取值（到周落成空）：%v", e.Data)
			}
		case "planned_start", "planned_end":
			dates++
		default:
			t.Fatalf("多出一条动态：%v", e.Data)
		}
	}
	if prec != 1 || dates != 2 {
		t.Fatalf("应是一条粒度加两条日期，实际 %d/%d", prec, dates)
	}
	// 同样的值再写一遍不记动态
	if _, err := a.UpdateGoal(ctx, jia, g.ID, UpdateGoalInput{DatePrecision: &week}); err != nil {
		t.Fatal(err)
	}
	if n := len(goalEvents(t, a, ctx, jia, g.ID)); n != 3 {
		t.Fatalf("没变化不该记动态，实际 %d 条", n)
	}
	bad := domain.DatePrecision("someday")
	if _, err := a.UpdateGoal(ctx, jia, g.ID, UpdateGoalInput{DatePrecision: &bad}); err == nil {
		t.Fatal("第六档应被拒")
	}
}

// 汇总只在读时算（ADR 0022 第 6 条）：父目标自己没填日期时从子目标与任务推算，推算值不落库。
func TestGoalDerivedDates(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)

	parent, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "官网改版"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{ParentID: parent.ID, Title: "登录改造",
		PlannedStart: day(t, "2026-10-01"), PlannedEnd: day(t, "2026-10-10")}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{ParentID: parent.ID, Title: "支付改造",
		PlannedStart: day(t, "2026-11-01"), PlannedEnd: day(t, "2026-11-20")}); err != nil {
		t.Fatal(err)
	}
	// 直接挂在父目标上的任务也算进推算
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: parent.ID, Title: "定方案",
		PlannedStart: day(t, "2026-09-20"), PlannedEnd: day(t, "2026-09-25")}); err != nil {
		t.Fatal(err)
	}

	v, err := a.GetGoal(ctx, jia, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.PlannedStart != nil || v.PlannedEnd != nil {
		t.Fatalf("推算值不该写回目标自己的计划起止：%v %v", v.PlannedStart, v.PlannedEnd)
	}
	if onDay(v.DerivedStart) != "2026-09-20" {
		t.Fatalf("推算开始应是子树里最早的 2026-09-20，实际 %s", onDay(v.DerivedStart))
	}
	if onDay(v.DerivedEnd) != "2026-11-20" {
		t.Fatalf("推算结束应是子树里最晚的 2026-11-20，实际 %s", onDay(v.DerivedEnd))
	}
	// 子目标自己填了日期：推算值只来自它自己的子树（这里是空的）
	for _, c := range v.Children {
		if c.DerivedStart != nil || c.DerivedEnd != nil {
			t.Fatalf("没有下级也没有任务的目标不该有推算值：%s %v", c.Title, c.DerivedStart)
		}
	}
	// 父目标自己填上开始之后：那一端以自己的为准，推算值照旧算给界面看
	if _, err := a.UpdateGoal(ctx, jia, parent.ID, UpdateGoalInput{PlannedStart: day(t, "2026-10-05")}); err != nil {
		t.Fatal(err)
	}
	v, err = a.GetGoal(ctx, jia, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if onDay(v.PlannedStart) != "2026-10-05" {
		t.Fatalf("自己填的开始应以自己的为准：%s", onDay(v.PlannedStart))
	}
	if onDay(v.DerivedStart) != "2026-09-20" {
		t.Fatalf("推算值应照旧算：%s", onDay(v.DerivedStart))
	}
	// 数据库里没有这两个字段，重新读一遍还是只有自己填的那一个日期
	raw, err := a.Store.GoalByID(ctx, a.Store.Pool, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if raw.PlannedEnd != nil {
		t.Fatalf("推算结束不该落库：%v", raw.PlannedEnd)
	}
}

// 泳道重排（ADR 0022）：一次请求发一串目标，服务端给等间距的排序权重；排不动的跳过并说明理由。
func TestSetGoalRanks(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)

	mine1, err := a.CreateGoal(ctx, yi, CreateGoalInput{Title: "乙的第一个目标"})
	if err != nil {
		t.Fatal(err)
	}
	mine2, err := a.CreateGoal(ctx, yi, CreateGoalInput{Title: "乙的第二个目标"})
	if err != nil {
		t.Fatal(err)
	}
	others, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "甲的目标"})
	if err != nil {
		t.Fatal(err)
	}

	out, err := a.SetGoalRanks(ctx, yi, GoalRankInput{IDs: []string{mine2.ID, others.ID, "goal_nope", mine1.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Updated != 2 || len(out.Skipped) != 2 || len(out.Goals) != 2 {
		t.Fatalf("应排上 2 个跳过 2 个：%+v", out)
	}
	// 排不动的不占位置：剩下的还是首尾相接的等间距
	if out.Goals[0].ID != mine2.ID || out.Goals[0].Rank != RankStep {
		t.Fatalf("第一个应是 %s，权重 %v：%+v", mine2.Title, RankStep, out.Goals)
	}
	if out.Goals[1].ID != mine1.ID || out.Goals[1].Rank != 2*RankStep {
		t.Fatalf("第二个应是 %s，权重 %v：%+v", mine1.Title, 2*RankStep, out.Goals)
	}
	for _, s := range out.Skipped {
		if s.Reason == "" || !strings.Contains(s.Reason, "目标") {
			t.Fatalf("跳过的理由要是一句完整的话：%+v", s)
		}
		switch s.ID {
		case others.ID:
			if s.Code != "err.goal_edit_forbidden" || s.Name != "甲的目标" {
				t.Fatalf("排不动别人的目标应说清楚：%+v", s)
			}
		case "goal_nope":
			if s.Code != "err.goal_gone" {
				t.Fatalf("目标不存在应说清楚：%+v", s)
			}
		default:
			t.Fatalf("不该跳过 %+v", s)
		}
	}
	// 排序权重不记动态：它是次序不是表态
	if evs := goalEvents(t, a, ctx, yi, mine1.ID); len(evs) != 0 {
		t.Fatalf("排序不该记动态：%v", fieldsOf(evs))
	}
	// 顺序照请求走：mine2 排在 mine1 前面
	tree, err := a.GoalTree(ctx, yi)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, v := range tree {
		if v.Rank != nil {
			order = append(order, v.ID)
		}
	}
	if len(order) != 2 || order[0] != mine2.ID || order[1] != mine1.ID {
		t.Fatalf("目标树应按新次序返回：%v", order)
	}
	// 再发一遍同样的顺序：值没变，不算改动
	out, err = a.SetGoalRanks(ctx, yi, GoalRankInput{IDs: []string{mine2.ID, mine1.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Updated != 0 || len(out.Goals) != 2 {
		t.Fatalf("次序没变时不该算改动：%+v", out)
	}
	if _, err := a.SetGoalRanks(ctx, yi, GoalRankInput{}); err == nil {
		t.Fatal("空清单应被拒")
	}
}

// 同层次序（ADR 0022）：手工排过的在前（按权重），其余按计划开始，再按创建时间；没日期的沉到最后。
func TestGoalSiblingOrder(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)

	parent, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "上级目标"})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(title string, start string) *domain.Goal {
		t.Helper()
		in := CreateGoalInput{ParentID: parent.ID, Title: title}
		if start != "" {
			in.PlannedStart, in.PlannedEnd = day(t, start), day(t, start)
		}
		g, err := a.CreateGoal(ctx, jia, in)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	noDate := mk("还没排期的", "")
	late := mk("十月开始的", "2026-10-01")
	early := mk("九月开始的", "2026-09-01")

	titles := func() []string {
		t.Helper()
		v, err := a.GetGoal(ctx, jia, parent.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(v.Children))
		for _, c := range v.Children {
			out = append(out, c.Title)
		}
		return out
	}
	if got := titles(); strings.Join(got, ",") != "九月开始的,十月开始的,还没排期的" {
		t.Fatalf("没手工排过时应按计划开始，没日期的在最后，实际 %v", got)
	}
	// 手工排一次：手工次序压过日期
	if _, err := a.SetGoalRanks(ctx, jia, GoalRankInput{IDs: []string{noDate.ID, late.ID}}); err != nil {
		t.Fatal(err)
	}
	if got := titles(); strings.Join(got, ",") != "还没排期的,十月开始的,九月开始的" {
		t.Fatalf("手工排过的应在前，实际 %v", got)
	}
	_ = early
}
