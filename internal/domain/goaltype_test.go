package domain

import (
	"reflect"
	"strings"
	"testing"
)

// 目标类型不带流程（ADR 0023 第 2 条）。这条是写死的边界，不是一时的实现选择：
// 任务类型决定走哪套状态机，目标类型不决定任何东西——目标本来就没有流程，
// 进度靠汇总、达成靠人确认（ADR 0016）。一旦给目标类型配上流程或必填字段，
// 「能不能指派给一个人」这条区分目标与任务的判定规则就废了（CONTEXT.md「目标还是任务」）。
//
// 所以这里把 GoalType 的字段名单钉死：想加字段，先回 ADR 0023 改决策，再改这条测试。
func TestGoalTypeCarriesNoWorkflow(t *testing.T) {
	allowed := map[string]bool{
		"ID": true, "OrgID": true, "Name": true, "Color": true, "Icon": true,
		"Sort": true, "Active": true, "DefaultPrecision": true, "CreatedAt": true, "UpdatedAt": true,
	}
	// 一眼能认出「这是流程 / 权限 / 必填字段」的词，出现即失败——即使拼在别的词里
	forbidden := []string{"workflow", "flow", "state", "transition", "step", "field", "required",
		"permission", "grant", "role", "assign", "visibility", "budget", "cost"}

	ty := reflect.TypeOf(GoalType{})
	for i := 0; i < ty.NumField(); i++ {
		name := ty.Field(i).Name
		if !allowed[name] {
			t.Fatalf("GoalType 多出字段 %s：目标类型只做分类与显示，加字段前先回 ADR 0023 的「被否决的」一节", name)
		}
		lower := strings.ToLower(name)
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Fatalf("GoalType 的字段 %s 看着像流程 / 权限 / 必填字段：目标类型不带任何行为（ADR 0023）", name)
			}
		}
	}
	// 唯一允许的附带信息是默认时间粒度，而且它只是「预填」：取值仍是那五档，可以为空
	if !ValidDatePrecision(GoalType{}.DefaultPrecision) {
		t.Fatal("空的默认时间粒度应当合法：不预填")
	}
}

// 类型名的校验：非空、不超长，默认时间粒度只认写死的五档。
func TestValidateGoalType(t *testing.T) {
	if errs := ValidateGoalType("产品", PrecisionQuarter); len(errs) != 0 {
		t.Fatalf("正常的类型不该报错：%v", errs)
	}
	if errs := ValidateGoalType("", ""); len(errs) != 1 {
		t.Fatalf("空名字应当被拒，实际 %v", errs)
	}
	if errs := ValidateGoalType(strings.Repeat("产", GoalTypeNameMaxRunes+1), ""); len(errs) != 1 {
		t.Fatalf("超长名字应当被拒，实际 %v", errs)
	}
	if errs := ValidateGoalType("产品", "decade"); len(errs) != 1 {
		t.Fatalf("第六档时间粒度应当被拒，实际 %v", errs)
	}
	if got := NormalizeGoalTypeName("  经营\n目标  "); got != "经营 目标" {
		t.Fatalf("类型名应去掉首尾空白、换行折成空格，实际 %q", got)
	}
}
