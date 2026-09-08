package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// 路线图三字段（ADR 0021）：时间桶与信心度只认写死的三档，成果指标规整成一句话且不超过 200 字。
func TestValidateGoalPlan(t *testing.T) {
	if errs := ValidateGoalPlan(HorizonNow, ConfidenceLow, "登录转化率从 62% 提到 70%", PrecisionQuarter); len(errs) != 0 {
		t.Fatalf("合法取值不该报错：%v", RenderErrors(i18n.ZhCN, errs))
	}
	if errs := ValidateGoalPlan("", "", "", ""); len(errs) != 0 {
		t.Fatalf("三个字段都可以空：%v", RenderErrors(i18n.ZhCN, errs))
	}
	// 第四档时间桶：拒绝，理由是完整句子
	errs := ValidateGoalPlan("q4", "", "", "")
	if len(errs) != 1 {
		t.Fatalf("应有一条问题，实际 %d", len(errs))
	}
	if got := RenderErrors(i18n.ZhCN, errs)[0]; !strings.Contains(got, "时间桶只有现在、下一步、以后三档") {
		t.Fatalf("时间桶的理由不对：%s", got)
	}
	// 第四档信心度
	errs = ValidateGoalPlan("", "very_high", "", "")
	if len(errs) != 1 || !strings.Contains(RenderErrors(i18n.ZhCN, errs)[0], "信心度只有高、中、低三档") {
		t.Fatalf("信心度的理由不对：%v", RenderErrors(i18n.ZhCN, errs))
	}
	// 成果指标 200 字是上限：正好 200 通过，201 被拒
	if errs := ValidateGoalPlan("", "", strings.Repeat("成", OutcomeMaxRunes), ""); len(errs) != 0 {
		t.Fatalf("200 字应通过：%v", RenderErrors(i18n.ZhCN, errs))
	}
	errs = ValidateGoalPlan("", "", strings.Repeat("成", OutcomeMaxRunes+1), "")
	if len(errs) != 1 || !strings.Contains(RenderErrors(i18n.ZhCN, errs)[0], "不要超过 200 字") {
		t.Fatalf("超长的理由不对：%v", RenderErrors(i18n.ZhCN, errs))
	}
	// 四个问题一起报
	if errs := ValidateGoalPlan("q4", "very_high", strings.Repeat("成", 300), "someday"); len(errs) != 4 {
		t.Fatalf("四个问题应一起报，实际 %d", len(errs))
	}
}

// 成果指标是一句话：首尾空白去掉，换行折成空格。
func TestNormalizeOutcome(t *testing.T) {
	if got := NormalizeOutcome("  登录转化率从 62% 提到 70%  "); got != "登录转化率从 62% 提到 70%" {
		t.Fatalf("首尾空白应去掉，实际 %q", got)
	}
	if got := NormalizeOutcome("第一行\r\n第二行"); got != "第一行 第二行" {
		t.Fatalf("换行应折成空格，实际 %q", got)
	}
}

// 时间粒度（ADR 0022）：只认五档，第六档给一句完整的话；到日已经不是一档了。
func TestValidateDatePrecision(t *testing.T) {
	for _, p := range append(AllDatePrecisions, "") {
		if errs := ValidateGoalPlan("", "", "", p); len(errs) != 0 {
			t.Fatalf("%q 应是合法粒度：%v", p, RenderErrors(i18n.ZhCN, errs))
		}
	}
	for _, bad := range []DatePrecision{"day", "someday", "week2", "Q4"} {
		errs := ValidateGoalPlan("", "", "", bad)
		if len(errs) != 1 {
			t.Fatalf("%q 应被拒，实际 %d 条问题", bad, len(errs))
		}
		if got := RenderErrors(i18n.ZhCN, errs)[0]; !strings.Contains(got, "时间粒度只有到周、到月、到季度、到半年、到年这几档") {
			t.Fatalf("理由不对：%s", got)
		}
	}
	// 空与 week 是同一件事：落库统一存空，对外统一读作到周
	if NormalizeDatePrecision(PrecisionWeek) != "" || NormalizeDatePrecision(PrecisionQuarter) != PrecisionQuarter {
		t.Fatal("week 应规整成空，其余原样")
	}
	if EffectiveDatePrecision("") != PrecisionWeek || EffectiveDatePrecision(PrecisionYear) != PrecisionYear {
		t.Fatal("空应读作到周，其余原样")
	}
}

// 吸附：每一档都把范围撑到刻度边界上，最细的到周也吸到周一与周日。
func TestSnapRange(t *testing.T) {
	d := func(s string) time.Time {
		tt, err := time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return tt
	}
	cases := []struct {
		p                  DatePrecision
		start, end         string
		wantStart, wantEnd string
	}{
		// 第四季度的目标：吸到 10-01..12-31
		{PrecisionQuarter, "2026-10-15", "2026-11-20", "2026-10-01", "2026-12-31"},
		{PrecisionQuarter, "2026-01-01", "2026-03-31", "2026-01-01", "2026-03-31"},
		{PrecisionMonth, "2026-09-07", "2026-10-02", "2026-09-01", "2026-10-31"},
		{PrecisionHalf, "2026-02-03", "2026-08-09", "2026-01-01", "2026-12-31"},
		{PrecisionHalf, "2026-07-01", "2026-09-30", "2026-07-01", "2026-12-31"},
		{PrecisionYear, "2026-05-05", "2026-05-06", "2026-01-01", "2026-12-31"},
		// 到周：2026-09-07 是周一，2026-09-09 是周三 → 09-07..09-13
		{PrecisionWeek, "2026-09-09", "2026-09-09", "2026-09-07", "2026-09-13"},
		{PrecisionWeek, "2026-09-07", "2026-09-13", "2026-09-07", "2026-09-13"},
		// 空等于到周
		{"", "2026-09-06", "2026-09-14", "2026-08-31", "2026-09-20"},
	}
	for _, c := range cases {
		gotStart, gotEnd := SnapRange(d(c.start), d(c.end), c.p)
		if !gotStart.Equal(d(c.wantStart)) || !gotEnd.Equal(d(c.wantEnd)) {
			t.Fatalf("%s 粒度 %s..%s 应吸到 %s..%s，实际 %s..%s", c.p, c.start, c.end, c.wantStart, c.wantEnd,
				gotStart.Format("2006-01-02"), gotEnd.Format("2006-01-02"))
		}
	}
	// 只有一端有日期时，另一端原样是零值
	s, e := SnapRange(d("2026-10-15"), time.Time{}, PrecisionQuarter)
	if !s.Equal(d("2026-10-01")) || !e.IsZero() {
		t.Fatalf("只有开始时应只吸开始：%v %v", s, e)
	}
	s, e = SnapRange(time.Time{}, d("2026-10-15"), PrecisionQuarter)
	if !s.IsZero() || !e.Equal(d("2026-12-31")) {
		t.Fatalf("只有结束时应只吸结束：%v %v", s, e)
	}
}
