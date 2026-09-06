package api

import (
	"encoding/json"
	"testing"
)

// estimate 与 estimate_hours 等价；null 表示清空；parent_id / required_capabilities 原样传给应用层。
func TestTaskInputEstimateAlias(t *testing.T) {
	var in TaskInputV
	if err := json.Unmarshal([]byte(`{"estimate": 6.5, "parent_id": "", "required_capabilities": []}`), &in); err != nil {
		t.Fatal(err)
	}
	out := in.toUpdate()
	if out.EstimateHours == nil || *out.EstimateHours != 6.5 {
		t.Fatalf("estimate 应映射到 EstimateHours，实际 %v", out.EstimateHours)
	}
	if out.ParentID == nil || *out.ParentID != "" {
		t.Fatalf("parent_id 空字符串应原样传递（提为顶级），实际 %v", out.ParentID)
	}
	if out.RequiredCapabilities == nil || len(*out.RequiredCapabilities) != 0 {
		t.Fatalf("required_capabilities 空列表应传递为「清空」，实际 %v", out.RequiredCapabilities)
	}

	in = TaskInputV{}
	if err := json.Unmarshal([]byte(`{"estimate_hours": 3}`), &in); err != nil {
		t.Fatal(err)
	}
	if out = in.toUpdate(); out.EstimateHours == nil || *out.EstimateHours != 3 {
		t.Fatalf("estimate_hours 应同样生效，实际 %v", out.EstimateHours)
	}

	in = TaskInputV{}
	if err := json.Unmarshal([]byte(`{"estimate": null, "planned_end": null}`), &in); err != nil {
		t.Fatal(err)
	}
	out = in.toUpdate()
	if out.EstimateHours != nil || len(out.Clear) != 2 || out.Clear[0] != "estimate_hours" || out.Clear[1] != "planned_end" {
		t.Fatalf("null 应表示清空，实际 est=%v clear=%v", out.EstimateHours, out.Clear)
	}

	in = TaskInputV{}
	if err := json.Unmarshal([]byte(`{"title": "x"}`), &in); err != nil {
		t.Fatal(err)
	}
	if out = in.toUpdate(); out.ParentID != nil || out.RequiredCapabilities != nil || out.EstimateHours != nil || len(out.Clear) != 0 {
		t.Fatalf("没带的字段不应被当成修改：%+v", out)
	}
}

// 目标：parent_id 传递；budget null 清空；planned_end 同步截止日，显式 deadline 优先。
func TestGoalInputToUpdate(t *testing.T) {
	var in GoalInputV
	if err := json.Unmarshal([]byte(`{"parent_id": "goal_x", "budget": null, "planned_end": "2026-09-20"}`), &in); err != nil {
		t.Fatal(err)
	}
	ui, err := in.toUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if ui.ParentID == nil || *ui.ParentID != "goal_x" {
		t.Fatalf("parent_id 应传递，实际 %v", ui.ParentID)
	}
	if len(ui.Clear) != 1 || ui.Clear[0] != "budget" || ui.Budget != nil {
		t.Fatalf("budget null 应表示清空，实际 clear=%v", ui.Clear)
	}
	if ui.PlannedEnd == nil || ui.Deadline == nil || !ui.PlannedEnd.Equal(*ui.Deadline) {
		t.Fatalf("计划结束应同步截止日：%v %v", ui.PlannedEnd, ui.Deadline)
	}
	in = GoalInputV{}
	if err := json.Unmarshal([]byte(`{"planned_end": null, "deadline": "2026-10-01"}`), &in); err != nil {
		t.Fatal(err)
	}
	if ui, _ = in.toUpdate(); len(ui.Clear) != 1 || ui.Clear[0] != "planned_end" || ui.Deadline == nil {
		t.Fatalf("显式 deadline 应优先于计划结束的清空：clear=%v deadline=%v", ui.Clear, ui.Deadline)
	}
}
