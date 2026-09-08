package app

import (
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/domain"
)

// 路线图三字段（ADR 0021）：创建与就地编辑都认三档，成果指标规整并限长，每个字段各记一条动态。
func TestGoalRoadmapFields(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)

	// 创建时就能填
	g, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "把新版官网上线", Horizon: domain.HorizonNext,
		Confidence: domain.ConfidenceMedium, Outcome: "  登录转化率从 62% 提到 70%  "})
	if err != nil {
		t.Fatal(err)
	}
	if g.Horizon != domain.HorizonNext || g.Confidence != domain.ConfidenceMedium {
		t.Fatalf("时间桶与信心度没存上：%+v", g)
	}
	if g.Outcome != "登录转化率从 62% 提到 70%" {
		t.Fatalf("成果指标应去掉首尾空白，实际 %q", g.Outcome)
	}

	// 第四档一律拒绝，理由是完整句子
	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "x", Horizon: "q4"}); err == nil {
		t.Fatal("第四档时间桶应被拒")
	} else if s := userErrText(t, err); !strings.Contains(s, "时间桶只有现在、下一步、以后三档") {
		t.Fatalf("理由不对：%s", s)
	}
	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "x", Confidence: "very_high"}); err == nil {
		t.Fatal("第四档信心度应被拒")
	} else if s := userErrText(t, err); !strings.Contains(s, "信心度只有高、中、低三档") {
		t.Fatalf("理由不对：%s", s)
	}
	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "x", Outcome: strings.Repeat("成", 201)}); err == nil {
		t.Fatal("超过 200 字的成果指标应被拒")
	} else if s := userErrText(t, err); !strings.Contains(s, "不要超过 200 字") {
		t.Fatalf("理由不对：%s", s)
	}

	// 就地编辑：改时间桶 + 改信心度 + 改成果指标 = 三条动态
	now, low := domain.HorizonNow, domain.ConfidenceLow
	outcome := "登录转化率从 62% 提到 75%"
	if _, err := a.UpdateGoal(ctx, jia, g.ID, UpdateGoalInput{Horizon: &now, Confidence: &low, Outcome: &outcome}); err != nil {
		t.Fatal(err)
	}
	evs := goalEvents(t, a, ctx, jia, g.ID)
	if len(evs) != 3 {
		t.Fatalf("应有 3 条动态，实际 %d：%v", len(evs), fieldsOf(evs))
	}
	for _, e := range evs {
		switch e.Data["field"] {
		case "horizon":
			if e.Data["from"] != "next" || e.Data["to"] != "now" {
				t.Fatalf("时间桶动态应记取值：%v", e.Data)
			}
			if _, ok := e.Data["to_title"]; ok {
				t.Fatalf("三档的名字不该落库，界面上现渲染：%v", e.Data)
			}
		case "confidence":
			if e.Data["from"] != "medium" || e.Data["to"] != "low" {
				t.Fatalf("信心度动态应记取值：%v", e.Data)
			}
		case "outcome":
			if e.Data["to"] != outcome {
				t.Fatalf("成果指标动态应记新句子：%v", e.Data)
			}
		default:
			t.Fatalf("多出一条动态：%v", e.Data)
		}
	}

	// 同样的值再写一遍不记动态；清空各记一条
	if _, err := a.UpdateGoal(ctx, jia, g.ID, UpdateGoalInput{Horizon: &now, Confidence: &low, Outcome: &outcome}); err != nil {
		t.Fatal(err)
	}
	if evs := goalEvents(t, a, ctx, jia, g.ID); len(evs) != 3 {
		t.Fatalf("没变化不该记动态，实际 %d 条", len(evs))
	}
	empty, emptyH, emptyC := "", domain.GoalHorizon(""), domain.GoalConfidence("")
	after, err := a.UpdateGoal(ctx, jia, g.ID, UpdateGoalInput{Horizon: &emptyH, Confidence: &emptyC, Outcome: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if after.Horizon != "" || after.Confidence != "" || after.Outcome != "" {
		t.Fatalf("三个字段应被清空：%+v", after)
	}
	cleared := map[string]bool{}
	for _, e := range goalEvents(t, a, ctx, jia, g.ID) {
		if e.Data["to"] == nil {
			cleared[e.Data["field"].(string)] = true
		}
	}
	for _, f := range []string{"horizon", "confidence", "outcome"} {
		if !cleared[f] {
			t.Fatalf("清空 %s 应各记一条动态（to 为空）", f)
		}
	}

	// 就地编辑同样只认三档
	bad := domain.GoalHorizon("someday")
	if _, err := a.UpdateGoal(ctx, jia, g.ID, UpdateGoalInput{Horizon: &bad}); err == nil {
		t.Fatal("第四档时间桶应被拒")
	} else if s := userErrText(t, err); !strings.Contains(s, "时间桶只有现在") {
		t.Fatalf("理由不对：%s", s)
	}
	long := strings.Repeat("成", 201)
	if _, err := a.UpdateGoal(ctx, jia, g.ID, UpdateGoalInput{Outcome: &long}); err == nil {
		t.Fatal("超长成果指标应被拒")
	}
}

// 按时间桶筛目标：命中的做顶级，没排期的用 none 取。
func TestGoalTreeHorizonFilter(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)

	nowGoal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "现在做的", Horizon: domain.HorizonNow})
	if err != nil {
		t.Fatal(err)
	}
	// 子目标在另一个桶里：筛 later 时它自己做顶级
	later, err := a.CreateGoal(ctx, jia, CreateGoalInput{ParentID: nowGoal.ID, Title: "以后再说的", Horizon: domain.HorizonLater})
	if err != nil {
		t.Fatal(err)
	}
	idle, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "还没排期的"})
	if err != nil {
		t.Fatal(err)
	}

	ids := func(f GoalFilter) []string {
		t.Helper()
		vs, err := a.GoalTreeFiltered(ctx, jia, f)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(vs))
		for _, v := range vs {
			out = append(out, v.ID)
		}
		return out
	}
	if got := ids(GoalFilter{Horizon: "now"}); len(got) != 1 || got[0] != nowGoal.ID {
		t.Fatalf("now 应只返回现在这一桶，实际 %v", got)
	}
	if got := ids(GoalFilter{Horizon: "later"}); len(got) != 1 || got[0] != later.ID {
		t.Fatalf("命中的子目标应自己做顶级，实际 %v", got)
	}
	if got := ids(GoalFilter{Horizon: "none"}); len(got) != 1 || got[0] != idle.ID {
		t.Fatalf("none 应返回还没排期的目标，实际 %v", got)
	}
	if got := ids(GoalFilter{Horizon: "next"}); len(got) != 0 {
		t.Fatalf("这一桶是空的，实际 %v", got)
	}
	if got := ids(GoalFilter{}); len(got) != 2 {
		t.Fatalf("不筛时还是整棵树（两个顶级），实际 %v", got)
	}
	if _, err := a.GoalTreeFiltered(ctx, jia, GoalFilter{Horizon: "q1"}); err == nil {
		t.Fatal("筛选值不合法应被拒")
	} else if s := userErrText(t, err); !strings.Contains(s, "时间桶筛选") {
		t.Fatalf("理由不对：%s", s)
	}
}

// 批量改时间桶：能改的改，改不了的跳过并给一句完整的理由，一个目标一条动态。
func TestBulkGoalHorizon(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)

	mine, err := a.CreateGoal(ctx, yi, CreateGoalInput{Title: "乙自己的目标"})
	if err != nil {
		t.Fatal(err)
	}
	others, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "甲的目标"})
	if err != nil {
		t.Fatal(err)
	}

	out, err := a.BulkGoalHorizon(ctx, yi, BulkGoalHorizonInput{IDs: []string{mine.ID, others.ID, "goal_nope"}, Horizon: domain.HorizonNow})
	if err != nil {
		t.Fatal(err)
	}
	if out.Updated != 1 || len(out.Skipped) != 2 {
		t.Fatalf("应改 1 个跳过 2 个，实际 %+v", out)
	}
	for _, s := range out.Skipped {
		if s.Reason == "" || !strings.Contains(s.Reason, "目标") {
			t.Fatalf("跳过的理由要是一句完整的话：%+v", s)
		}
		switch s.ID {
		case others.ID:
			if s.Code != "err.goal_edit_forbidden" || s.Name != "甲的目标" {
				t.Fatalf("改不了别人的目标应说清楚：%+v", s)
			}
		case "goal_nope":
			if s.Code != "err.goal_gone" || !strings.HasSuffix(s.Reason, "。") {
				t.Fatalf("目标不存在应说清楚：%+v", s)
			}
		default:
			t.Fatalf("不该跳过 %+v", s)
		}
	}
	// 改的那个：值变了，动态只有一条
	after, err := a.GetGoal(ctx, yi, mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Horizon != domain.HorizonNow {
		t.Fatalf("时间桶没改上：%s", after.Horizon)
	}
	evs := goalEvents(t, a, ctx, yi, mine.ID)
	if len(evs) != 1 || evs[0].Data["field"] != "horizon" || evs[0].Data["to"] != "now" {
		t.Fatalf("应只有一条时间桶动态：%v", fieldsOf(evs))
	}

	// 已经在这一桶里的再拖一次：跳过，不记新动态
	out, err = a.BulkGoalHorizon(ctx, yi, BulkGoalHorizonInput{IDs: []string{mine.ID}, Horizon: domain.HorizonNow})
	if err != nil {
		t.Fatal(err)
	}
	if out.Updated != 0 || len(out.Skipped) != 1 || out.Skipped[0].Code != "err.goal_horizon_same" {
		t.Fatalf("没变化应跳过：%+v", out)
	}
	if !strings.Contains(out.Skipped[0].Reason, "现在") {
		t.Fatalf("理由应念出桶的名字：%s", out.Skipped[0].Reason)
	}
	if len(goalEvents(t, a, ctx, yi, mine.ID)) != 1 {
		t.Fatal("没变化不该记动态")
	}

	// 拖回「还没排期」：空时间桶合法
	if out, err = a.BulkGoalHorizon(ctx, yi, BulkGoalHorizonInput{IDs: []string{mine.ID}, Horizon: ""}); err != nil {
		t.Fatal(err)
	} else if out.Updated != 1 {
		t.Fatalf("空时间桶表示还没排期，应能改回去：%+v", out)
	}

	// 第四档与空清单一律 400
	if _, err := a.BulkGoalHorizon(ctx, yi, BulkGoalHorizonInput{IDs: []string{mine.ID}, Horizon: "q4"}); err == nil {
		t.Fatal("第四档应被拒")
	}
	if _, err := a.BulkGoalHorizon(ctx, yi, BulkGoalHorizonInput{Horizon: domain.HorizonNow}); err == nil {
		t.Fatal("空清单应被拒")
	}
}
