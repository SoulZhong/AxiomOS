package app

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// 显示偏好：个人 → 角色 → 默认 逐字段解析；部分修改、null 清项、一键重置；校验句子完整；Agent 没有偏好。
func TestPreferencesResolution(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	zh := i18n.ZhCN

	v, err := a.MyPreferences(ctx, yi)
	if err != nil {
		t.Fatal(err)
	}
	def := domain.DefaultPreferences()
	if v.Source != domain.PreferenceSourceDefault || strings.Join(v.TaskListColumns, ",") != strings.Join(def.TaskListColumns, ",") || v.DefaultTaskView != "list" {
		t.Fatalf("没有任何配置时应是系统默认，实际 %+v", v)
	}

	// 组织给「开发」角色配默认
	if _, err := a.SetRolePreferences(ctx, yi, "developer", []byte(`{"compact":true}`)); err == nil {
		t.Fatal("没有「组织设置」权限的成员不该能配角色偏好")
	}
	rv, err := a.SetRolePreferences(ctx, jia, "developer", []byte(`{"task_list_columns":["number","title","state"],"compact":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rv.Fields, ",") != "task_list_columns,compact" {
		t.Fatalf("角色记录只含设过的字段，实际 %v", rv.Fields)
	}
	v, err = a.MyPreferences(ctx, yi)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != domain.PreferenceSourceRole || !v.Compact || strings.Join(v.TaskListColumns, ",") != "number,title,state" || v.Sources["default_task_view"] != domain.PreferenceSourceDefault {
		t.Fatalf("角色默认应生效、没设的字段回落系统默认，实际 %+v", v)
	}
	if len(v.RolesUsed) != 1 || v.RolesUsed[0] != "developer" {
		t.Fatalf("roles_used 应是 developer，实际 %v", v.RolesUsed)
	}

	// 个人微调：只改带的字段
	v, err = a.SetMyPreferences(ctx, yi, []byte(`{"compact":false,"default_task_view":"board"}`))
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != domain.PreferenceSourcePersonal || v.Compact || v.DefaultTaskView != "board" || strings.Join(v.TaskListColumns, ",") != "number,title,state" {
		t.Fatalf("个人微调应覆盖角色、其余保留角色的，实际 %+v", v)
	}
	if strings.Join(v.Overrides, ",") != "default_task_view,compact" || v.Sources["compact"] != domain.PreferenceSourcePersonal || v.Sources["task_list_columns"] != domain.PreferenceSourceRole {
		t.Fatalf("来源标注不对：overrides=%v sources=%v", v.Overrides, v.Sources)
	}
	// null 清掉一项，回到角色
	v, err = a.SetMyPreferences(ctx, yi, []byte(`{"compact":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Compact || v.Sources["compact"] != domain.PreferenceSourceRole || strings.Join(v.Overrides, ",") != "default_task_view" {
		t.Fatalf("传 null 应回到角色默认，实际 %+v", v)
	}

	// 校验：整句理由
	bad := []struct{ body, key string }{
		{`{"task_list_columns":["state"]}`, "err.pref_columns_title"},
		{`{"task_list_columns":[]}`, "err.pref_columns_empty"},
		{`{"default_task_view":"foo"}`, "err.pref_view_unknown"},
		{`{"foo":1}`, "err.pref_key_unknown"},
		{`{}`, "err.pref_body_empty"},
		{`{"compact":"yes"}`, "err.pref_value"},
	}
	for _, b := range bad {
		_, err := a.SetMyPreferences(ctx, yi, []byte(b.body))
		if err == nil {
			t.Fatalf("%s 应被拒", b.body)
		}
		want := i18n.Tr(zh, b.key)
		if i := strings.Index(want, "%"); i >= 0 {
			want = want[:i]
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s 的理由应含「%s」，实际 %v", b.body, want, err)
		}
	}
	if _, err := a.SetMyPreferences(ctx, yi, []byte(`{"task_list_columns":["title","nope"]}`)); err == nil || !strings.Contains(err.Error(), "「nope」") {
		t.Fatalf("未知列应点名，实际 %v", err)
	}

	// 一键重置
	v, err = a.ClearMyPreferences(ctx, yi)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != domain.PreferenceSourceRole || v.DefaultTaskView != "list" || len(v.Overrides) != 0 {
		t.Fatalf("重置后应回到角色默认，实际 %+v", v)
	}
	// 清角色 → 系统默认
	if _, err := a.ClearRolePreferences(ctx, jia, "developer"); err != nil {
		t.Fatal(err)
	}
	v, _ = a.MyPreferences(ctx, yi)
	if v.Source != domain.PreferenceSourceDefault || v.Compact {
		t.Fatalf("清掉角色后应是系统默认，实际 %+v", v)
	}
	list, err := a.OrgPreferences(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range list {
		if r.Role == "developer" {
			found = true
			if len(r.Fields) != 0 || r.MemberCount != 1 {
				t.Fatalf("清掉后的角色记录应为空、成员数 1，实际 %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("组织偏好列表应包含每个角色")
	}
	cat := a.PreferenceCatalog(yi)
	if len(cat.Columns) != len(domain.TaskListColumns) || cat.Columns[0].Title != "序号" || len(cat.CardFields) != len(domain.TaskCardFields) {
		t.Fatalf("目录不完整：%+v", cat)
	}

	// Agent 没有偏好
	_, tok, err := a.RegisterAgent(ctx, yi, RegisterAgentInput{Name: "偏好 Agent"})
	if err != nil {
		t.Fatal(err)
	}
	as, _ := a.SessionFromAgentToken(ctx, tok)
	if _, err := a.MyPreferences(ctx, as); err == nil || !strings.Contains(err.Error(), i18n.Tr(zh, "err.preferences_agent")) {
		t.Fatalf("Agent 应被拒并给整句理由，实际 %v", err)
	}

	// 每次写都有动态
	n := 0
	_ = a.Store.WithOrg(ctx, yi.OrgID, func(tx pgx.Tx) error {
		evs, _ := a.Store.ListEvents(ctx, tx, "", 200)
		for _, e := range evs {
			if e.Type == "PreferencesUpdated" {
				n++
			}
		}
		return nil
	})
	if n < 5 {
		t.Fatalf("应有至少 5 条 PreferencesUpdated 动态（角色设、个人设两次、个人清、角色清），实际 %d", n)
	}
}
