package api

import (
	"testing"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 目标类型的动态句子（ADR 0023）。名字念的是动态里记下的那个，改名不影响历史。
func TestGoalTypeEventSentences(t *testing.T) {
	actor := refs{"m1": {ID: "m1", Kind: "member", Name: "仲维建"}}
	say := func(kind string, data map[string]any) string {
		ev := &store.EventRow{Event: domain.Event{Type: kind, ActorID: "m1", Data: data}}
		return eventSummary(ev, actor, map[string]string{}, map[string]i18n.Text{}, i18n.ZhCN)
	}
	cases := []struct{ got, want string }{
		{say("GoalTypeCreated", map[string]any{"type_id": "gt1", "name": "产品"}), "仲维建 新建了目标类型「产品」"},
		{say("GoalTypeRenamed", map[string]any{"type_id": "gt1", "from": "产品", "name": "产品线"}), "仲维建 把目标类型「产品」改名为「产品线」"},
		{say("GoalTypeDeactivated", map[string]any{"type_id": "gt1", "name": "产品"}), "仲维建 停用了目标类型「产品」"},
		{say("GoalTypeReactivated", map[string]any{"type_id": "gt1", "name": "产品"}), "仲维建 恢复了目标类型「产品」"},
		{say("GoalTypeDeleted", map[string]any{"type_id": "gt1", "name": "产品"}), "仲维建 删除了目标类型「产品」"},
		// 目标身上的类型改动走就地编辑那条路：句子里念的是当时的名字
		{say("GoalFieldChanged", map[string]any{"goal_id": "g1", "title": "把新版官网上线", "field": "type_id",
			"from": "gt2", "to": "gt1", "from_title": "项目", "to_title": "产品"}),
			"仲维建 把目标「把新版官网上线」的类型从「项目」改成了「产品」"},
		{say("GoalFieldChanged", map[string]any{"goal_id": "g1", "title": "把新版官网上线", "field": "type_id",
			"from": "gt1", "to": nil, "from_title": "产品"}),
			"仲维建 清空了目标「把新版官网上线」的类型"},
		{say("GoalFieldChanged", map[string]any{"goal_id": "g1", "title": "把新版官网上线", "field": "type_id",
			"from": nil, "to": "gt1", "to_title": "产品"}),
			"仲维建 把目标「把新版官网上线」的类型设为「产品」"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Fatalf("句子不对：\n得到 %s\n期望 %s", c.got, c.want)
		}
	}
}
