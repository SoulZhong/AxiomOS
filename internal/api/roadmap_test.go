package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

func goalFieldEvent(field string, from, to any) *store.EventRow {
	data := map[string]any{"goal_id": "goal_1", "title": "把新版官网上线", "field": field, "from": from, "to": to}
	return &store.EventRow{Event: domain.Event{Type: "GoalFieldChanged", Data: data}}
}

// 时间桶与信心度只存取值，句子里的名字是现渲染的（ADR 0021）。
func TestRoadmapFieldChangeSentences(t *testing.T) {
	say := func(e *store.EventRow) string {
		return eventSummary(e, refs{}, map[string]string{}, map[string]i18n.Text{}, i18n.ZhCN)
	}
	cases := []struct {
		ev   *store.EventRow
		want []string
	}{
		{goalFieldEvent("horizon", "next", "now"), []string{"目标「把新版官网上线」的时间桶", "从「下一步」改成了「现在」"}},
		{goalFieldEvent("horizon", nil, "later"), []string{"时间桶设为「以后」"}},
		{goalFieldEvent("horizon", "now", nil), []string{"清空了", "时间桶"}},
		{goalFieldEvent("confidence", nil, "low"), []string{"信心度设为「低」"}},
		{goalFieldEvent("confidence", "high", "medium"), []string{"信心度从「高」改成了「中」"}},
		{goalFieldEvent("outcome", nil, "登录转化率从 62% 提到 70%"), []string{"成果指标改成了「登录转化率从 62% 提到 70%」"}},
		{goalFieldEvent("outcome", "旧的一句话", "新的一句话"), []string{"成果指标改成了「新的一句话」"}},
		{goalFieldEvent("outcome", "旧的一句话", nil), []string{"清空了", "成果指标"}},
	}
	for _, c := range cases {
		got := say(c.ev)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Fatalf("句子缺「%s」：%s", w, got)
			}
		}
		if strings.Contains(got, "horizon.") || strings.Contains(got, "confidence.") {
			t.Fatalf("句子里不该出现词条键：%s", got)
		}
	}
}

// 接口层的输入映射：三个字段传空串表示清空，不传就是不改。
func TestGoalInputRoadmapFields(t *testing.T) {
	var in GoalInputV
	if err := json.Unmarshal([]byte(`{"horizon":"now","confidence":"low","outcome":"  一句话  "}`), &in); err != nil {
		t.Fatal(err)
	}
	ui, err := in.toUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if ui.Horizon == nil || *ui.Horizon != domain.HorizonNow || ui.Confidence == nil || *ui.Confidence != domain.ConfidenceLow {
		t.Fatalf("时间桶与信心度没映射上：%+v", ui)
	}
	if ui.Outcome == nil || *ui.Outcome != "  一句话  " {
		t.Fatalf("成果指标原样传给应用层（那里再规整）：%v", ui.Outcome)
	}

	in = GoalInputV{}
	if err := json.Unmarshal([]byte(`{"horizon":"","outcome":""}`), &in); err != nil {
		t.Fatal(err)
	}
	if ui, err = in.toUpdate(); err != nil {
		t.Fatal(err)
	}
	if ui.Horizon == nil || *ui.Horizon != "" || ui.Outcome == nil || *ui.Outcome != "" {
		t.Fatalf("空串表示清空，应原样传递：%+v", ui)
	}
	if ui.Confidence != nil {
		t.Fatalf("没传的字段不该改：%v", ui.Confidence)
	}
}

// 时间粒度的句子（ADR 0022）：空值是「到周」而不是「没有」，两头都要念出粒度。
func TestDatePrecisionSentences(t *testing.T) {
	say := func(e *store.EventRow) string {
		return eventSummary(e, refs{}, map[string]string{}, map[string]i18n.Text{}, i18n.ZhCN)
	}
	cases := []struct {
		ev   *store.EventRow
		want []string
	}{
		{goalFieldEvent("date_precision", "quarter", nil), []string{"目标「把新版官网上线」的时间粒度", "从「到季度」改成了「到周」"}},
		{goalFieldEvent("date_precision", nil, "quarter"), []string{"时间粒度从「到周」改成了「到季度」"}},
		{goalFieldEvent("date_precision", "month", "half"), []string{"时间粒度从「到月」改成了「到半年」"}},
	}
	for _, c := range cases {
		got := say(c.ev)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Fatalf("句子缺「%s」：%s", w, got)
			}
		}
		if strings.Contains(got, "date_precision.") || strings.Contains(got, "清空") {
			t.Fatalf("时间粒度没有「没填」这一说：%s", got)
		}
	}
}

// 接口层的时间线字段：粒度原样传，排序权重传 null 表示清空。
func TestGoalInputTimelineFields(t *testing.T) {
	var in GoalInputV
	if err := json.Unmarshal([]byte(`{"date_precision":"quarter","rank":1500}`), &in); err != nil {
		t.Fatal(err)
	}
	ui, err := in.toUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if ui.DatePrecision == nil || *ui.DatePrecision != domain.PrecisionQuarter {
		t.Fatalf("粒度没映射上：%+v", ui.DatePrecision)
	}
	if ui.Rank == nil || *ui.Rank != 1500 {
		t.Fatalf("排序权重没映射上：%v", ui.Rank)
	}

	in = GoalInputV{}
	if err := json.Unmarshal([]byte(`{"rank":null}`), &in); err != nil {
		t.Fatal(err)
	}
	if ui, err = in.toUpdate(); err != nil {
		t.Fatal(err)
	}
	cleared := false
	for _, f := range ui.Clear {
		if f == "rank" {
			cleared = true
		}
	}
	if ui.Rank != nil || !cleared {
		t.Fatalf("null 应表示清空排序权重：%+v", ui)
	}
	if ui.DatePrecision != nil {
		t.Fatalf("没传的字段不该改：%v", ui.DatePrecision)
	}
}

// 目标视图上的条画在哪（ADR 0022）：每一档都吸附，推算出来的那一端有标记。
func TestGoalViewSnappedAndDerived(t *testing.T) {
	d := func(s string) *time.Time {
		tt, err := time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return &tt
	}
	rf := refs{"m1": ExecutorRef{ID: "m1", Kind: "member", Name: "甲"}}
	// 自己填了第四季度的日期，粒度到季度：吸到 10-01..12-31，原值不动
	own := &app.GoalView{Goal: &domain.Goal{ID: "g1", OwnerMemberID: "m1", Title: "官网 v2.0",
		PlannedStart: d("2026-10-15"), PlannedEnd: d("2026-11-20"), DatePrecision: domain.PrecisionQuarter}}
	v := goalView(own, rf, nil, i18n.ZhCN)
	if v.DatePrecision != "quarter" || v.DatePrecisionTitle != "到季度" {
		t.Fatalf("粒度与名字不对：%+v", v)
	}
	if str(v.PlannedStart) != "2026-10-15" || str(v.PlannedEnd) != "2026-11-20" {
		t.Fatalf("原值不该被吸附改写：%v..%v", str(v.PlannedStart), str(v.PlannedEnd))
	}
	if str(v.PlannedStartSnapped) != "2026-10-01" || str(v.PlannedEndSnapped) != "2026-12-31" {
		t.Fatalf("到季度应吸到 10-01..12-31，实际 %v..%v", str(v.PlannedStartSnapped), str(v.PlannedEndSnapped))
	}
	if v.Derived.Start || v.Derived.End {
		t.Fatalf("自己填的日期不是推算的：%+v", v.Derived)
	}

	// 自己什么都没填，日期从子树推出来：两端都是推算的，粒度按到周吸附
	derived := &app.GoalView{Goal: &domain.Goal{ID: "g2", OwnerMemberID: "m1", Title: "官网改版"},
		DerivedStart: d("2026-09-09"), DerivedEnd: d("2026-09-09")}
	v = goalView(derived, rf, nil, i18n.ZhCN)
	if v.DatePrecision != "week" || v.DatePrecisionTitle != "到周" {
		t.Fatalf("空粒度应读作到周：%+v", v)
	}
	if !v.Derived.Start || !v.Derived.End {
		t.Fatalf("两端都该标成推算的：%+v", v.Derived)
	}
	if str(v.DerivedStart) != "2026-09-09" || str(v.DerivedEnd) != "2026-09-09" {
		t.Fatalf("推算值应原样给：%v..%v", str(v.DerivedStart), str(v.DerivedEnd))
	}
	if str(v.PlannedStartSnapped) != "2026-09-07" || str(v.PlannedEndSnapped) != "2026-09-13" {
		t.Fatalf("到周应吸到周一与周日，实际 %v..%v", str(v.PlannedStartSnapped), str(v.PlannedEndSnapped))
	}

	// 完全没有日期：两个吸附字段都不给（界面画幽灵条）
	ghost := &app.GoalView{Goal: &domain.Goal{ID: "g3", OwnerMemberID: "m1", Title: "还没排期的"}}
	v = goalView(ghost, rf, nil, i18n.ZhCN)
	if v.PlannedStartSnapped != nil || v.PlannedEndSnapped != nil || v.Derived.Start || v.Derived.End {
		t.Fatalf("没有日期就什么都不给：%+v", v)
	}
}
