package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teemo/axiomos/internal/i18n"
)

// 路线图（ADR 0021）：目标多出三个表达「意图」的字段。三档是写死的——多一档就会有人拿它当季度，
// 路线图立刻退化成排期表（ADR 0014 的边界：管理策略可配，表达方式写死）。
// 三个字段都可空：空的时间桶表示还没排期，空的信心度表示没填。

// GoalHorizon 是目标的时间桶：粗时间表达，只有现在 / 下一步 / 以后三档。
type GoalHorizon string

const (
	HorizonNow   GoalHorizon = "now"
	HorizonNext  GoalHorizon = "next"
	HorizonLater GoalHorizon = "later"
)

// AllHorizons 是三档时间桶，顺序就是路线图上从左到右的顺序。
var AllHorizons = []GoalHorizon{HorizonNow, HorizonNext, HorizonLater}

// GoalConfidence 是目标能不能按这个时间桶达成的把握，只有高 / 中 / 低三档。
type GoalConfidence string

const (
	ConfidenceHigh   GoalConfidence = "high"
	ConfidenceMedium GoalConfidence = "medium"
	ConfidenceLow    GoalConfidence = "low"
)

// AllConfidences 是三档信心度，由高到低。
var AllConfidences = []GoalConfidence{ConfidenceHigh, ConfidenceMedium, ConfidenceLow}

// OutcomeMaxRunes 是成果指标的长度上限：一句话就好。
const OutcomeMaxRunes = 200

// ValidHorizon 判断时间桶取值是否合法；空表示还没排期，也合法。
func ValidHorizon(h GoalHorizon) bool {
	if h == "" {
		return true
	}
	for _, x := range AllHorizons {
		if x == h {
			return true
		}
	}
	return false
}

// ValidConfidence 判断信心度取值是否合法；空表示没填，也合法。
func ValidConfidence(c GoalConfidence) bool {
	if c == "" {
		return true
	}
	for _, x := range AllConfidences {
		if x == c {
			return true
		}
	}
	return false
}

// NormalizeOutcome 规整成果指标：去掉首尾空白，换行折成空格（它是一句话，不是段落）。
func NormalizeOutcome(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// ValidateGoalPlan 校验路线图这几个字段：时间桶与信心度只认写死的三档，成果指标最多 200 字，
// 时间粒度只认五档。传进来的成果指标应当已经过 NormalizeOutcome。
func ValidateGoalPlan(h GoalHorizon, c GoalConfidence, outcome string, p DatePrecision) []error {
	var errs []error
	add := func(key string, a ...any) { errs = append(errs, &ValidationError{Msg: i18n.M(key, a...)}) }
	if !ValidHorizon(h) {
		add("val.goal_horizon")
	}
	if !ValidConfidence(c) {
		add("val.goal_confidence")
	}
	if utf8.RuneCountInString(outcome) > OutcomeMaxRunes {
		add("val.goal_outcome_long")
	}
	if !ValidDatePrecision(p) {
		add("val.goal_date_precision")
	}
	return errs
}

// 时间线（ADR 0022）：目标再多两个字段。时间粒度说的是「日期填到多细为止」，
// 决定路线图上的条怎么画——到周是吸到整周的硬边实条，更粗的两端吸附到刻度并做雾边；
// 排序权重是泳道内的手动次序，纯排序，不记动态。

// DatePrecision 是目标日期的粒度：到周 / 到月 / 到季度 / 到半年 / 到年。
// 路线图最细就到周，再细是甘特图的事（ADR 0022）。
// 空字符串等价于 PrecisionWeek（旧目标一律如此），落库时统一存空。
type DatePrecision string

const (
	PrecisionWeek    DatePrecision = "week"
	PrecisionMonth   DatePrecision = "month"
	PrecisionQuarter DatePrecision = "quarter"
	PrecisionHalf    DatePrecision = "half"
	PrecisionYear    DatePrecision = "year"
)

// AllDatePrecisions 是五档时间粒度，由细到粗。
var AllDatePrecisions = []DatePrecision{PrecisionWeek, PrecisionMonth, PrecisionQuarter, PrecisionHalf, PrecisionYear}

// ValidDatePrecision 判断时间粒度取值是否合法；空表示到周，也合法。
func ValidDatePrecision(p DatePrecision) bool {
	if p == "" {
		return true
	}
	for _, x := range AllDatePrecisions {
		if x == p {
			return true
		}
	}
	return false
}

// NormalizeDatePrecision 是落库前的规整：week 与空是同一件事，统一存空，
// 免得同一个含义有两种写法（比较、记动态时都要少一次分支）。
func NormalizeDatePrecision(p DatePrecision) DatePrecision {
	if p == PrecisionWeek {
		return ""
	}
	return p
}

// EffectiveDatePrecision 是对外呈现用的粒度：空一律读作「到周」。
func EffectiveDatePrecision(p DatePrecision) DatePrecision {
	if p == "" {
		return PrecisionWeek
	}
	return p
}

// SnapRange 把一段日期撑到该粒度的边界上：开始退到所在刻度的第一天，结束进到所在刻度的最后一天。
// 每一档都吸附——最细的「到周」也要吸到那一周的周一与周日，因为路线图上最细就是一周（ADR 0022）。
// 零值表示这一端没有日期，原样返回，另一端照算。
// 界面上的条就是照这个范围画的——服务端与前端各算一遍会对不齐，所以由这里给出唯一的答案。
// 目标自己填的计划起止一个字都不改：吸附只影响怎么画、对外分享降到多粗。
func SnapRange(start, end time.Time, p DatePrecision) (time.Time, time.Time) {
	if !start.IsZero() {
		start = snapStart(start, p)
	}
	if !end.IsZero() {
		end = snapEnd(end, p)
	}
	return start, end
}

// snapStart 退到所在刻度的第一天零点。周以周一起算（与界面的 startOfWeek 一致）。
func snapStart(t time.Time, p DatePrecision) time.Time {
	y, m := t.Year(), t.Month()
	switch p {
	case PrecisionMonth:
		// 本月一号
	case PrecisionWeek, "":
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		back := (int(day.Weekday()) + 6) % 7 // 周一 0、周日 6
		return day.AddDate(0, 0, -back)
	case PrecisionQuarter:
		m = time.Month((int(m)-1)/3*3 + 1)
	case PrecisionHalf:
		if m <= time.June {
			m = time.January
		} else {
			m = time.July
		}
	case PrecisionYear:
		m = time.January
	}
	return time.Date(y, m, 1, 0, 0, 0, 0, t.Location())
}

// snapEnd 进到所在刻度的最后一天零点（日期只取到天，不带时分秒）。
func snapEnd(t time.Time, p DatePrecision) time.Time {
	first := snapStart(t, p)
	if p == PrecisionWeek || p == "" {
		return first.AddDate(0, 0, 6) // 那一周的周日
	}
	months := 1
	switch p {
	case PrecisionQuarter:
		months = 3
	case PrecisionHalf:
		months = 6
	case PrecisionYear:
		months = 12
	}
	return first.AddDate(0, months, 0).AddDate(0, 0, -1)
}
