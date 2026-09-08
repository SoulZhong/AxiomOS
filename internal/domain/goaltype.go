package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teemo/axiomos/internal/i18n"
)

// GoalTypeNameMaxRunes 是目标类型名称的长度上限：它是一枚小标签，不是一句话。
const GoalTypeNameMaxRunes = 20

// GoalType 是目标的类型（ADR 0023）：组织自己维护的一张词表，用来把目标分成
// 产品 / 项目 / 经营目标这样几类。不同公司叫法不同（产品线、战役、主题），
// 所以它是可配的，不是写死的枚举（ADR 0014）。
//
// 这个结构体只有分类与显示需要的字段，这一点是**故意的、写死的**：
//
//   - 没有流程。任务类型决定走哪套状态机（ADR 0004、0005），目标类型不决定任何东西：
//     目标本来就没有流程，进度靠汇总、达成靠人确认（ADR 0016）。给目标类型配流程，
//     「能不能指派给一个人」这条区分目标与任务的判定规则就废了（CONTEXT.md「目标还是任务」）。
//   - 不决定权限、不决定成本归口、不决定可见范围，也没有必填字段。
//   - 唯一允许的附带信息是 DefaultPrecision：新建这个类型的目标时预填的时间粒度，
//     仍可逐个目标改。它是「预填」，不是「限制」。
//
// 想加字段前先回到 ADR 0023 的「被否决的」一节：那里明确否决了给目标类型配流程或必填字段。
// goaltype_test.go 里有一条测试会数这个结构体的字段，拦住这类字段偷偷长出来。
type GoalType struct {
	ID     string `json:"id"`
	OrgID  string `json:"org_id"`
	Name   string `json:"name"`
	Color  string `json:"color"`
	Icon   string `json:"icon"`
	Sort   int    `json:"sort"`
	Active bool   `json:"active"`
	// DefaultPrecision 是新建这个类型的目标时预填的时间粒度，空表示不预填。
	DefaultPrecision DatePrecision `json:"default_precision"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

// NormalizeGoalTypeName 规整类型名：去掉首尾空白，换行折成空格（它是一枚标签，不是段落）。
func NormalizeGoalTypeName(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// ValidateGoalType 校验一个目标类型：名称非空且不超长，默认时间粒度只认写死的五档（空表示不预填）。
// 颜色与图标不校验取值：它们是显示用的，界面自己给可选项。
func ValidateGoalType(name string, p DatePrecision) []error {
	var errs []error
	add := func(key string, a ...any) { errs = append(errs, &ValidationError{Msg: i18n.M(key, a...)}) }
	if name == "" {
		add("val.goal_type_name_required")
	}
	if utf8.RuneCountInString(name) > GoalTypeNameMaxRunes {
		add("val.goal_type_name_long", GoalTypeNameMaxRunes)
	}
	if p != "" && !ValidDatePrecision(p) {
		add("val.goal_date_precision")
	}
	return errs
}
