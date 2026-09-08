package app

import (
	"context"
	"strings"
	"testing"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

func sp(s string) *string { return &s }

// typeByName 从组织的类型词表里按名字找一个。
func typeByName(t *testing.T, a *App, ctx context.Context, sess *Session, name string) GoalTypeView {
	t.Helper()
	types, err := a.ListGoalTypes(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range types {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("没有叫「%s」的目标类型：%v", name, types)
	return GoalTypeView{}
}

// orgEvents 取某个类型上的组织级动态。
func goalTypeEvents(t *testing.T, a *App, ctx context.Context, sess *Session, typeID string) []*store.EventRow {
	t.Helper()
	rows, err := a.Events(ctx, sess, "", 300)
	if err != nil {
		t.Fatal(err)
	}
	var out []*store.EventRow
	for _, e := range rows {
		if strings.HasPrefix(e.Type, "GoalType") && e.Data["type_id"] == typeID {
			out = append(out, e)
		}
	}
	return out
}

// 内置三种目标类型（ADR 0023 第 3 条）：新组织开箱就有产品、项目、经营目标，各带颜色、图标与默认时间粒度。
func TestGoalTypeDefaults(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)

	types, err := a.ListGoalTypes(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 3 {
		t.Fatalf("应当预置三种目标类型，实际 %d：%v", len(types), types)
	}
	want := []string{"产品", "项目", "经营目标"}
	for i, n := range want {
		if types[i].Name != n {
			t.Fatalf("第 %d 个类型应是「%s」，实际「%s」", i+1, n, types[i].Name)
		}
		if types[i].Color == "" || types[i].Icon == "" {
			t.Fatalf("内置类型应带颜色与图标：%+v", types[i])
		}
		if !types[i].Active {
			t.Fatalf("内置类型应当是启用的：%+v", types[i])
		}
		if types[i].GoalCount != 0 {
			t.Fatalf("新组织的类型下面不该有目标：%+v", types[i])
		}
	}
	if typeByName(t, a, ctx, jia, "产品").DefaultPrecision != domain.PrecisionQuarter {
		t.Fatal("「产品」的默认时间粒度应是到季度")
	}
	if typeByName(t, a, ctx, jia, "项目").DefaultPrecision != domain.PrecisionWeek {
		t.Fatal("「项目」的默认时间粒度应是到周")
	}
	if typeByName(t, a, ctx, jia, "经营目标").DefaultPrecision != domain.PrecisionQuarter {
		t.Fatal("「经营目标」的默认时间粒度应是到季度")
	}

	// 再跑一遍补齐不会重复写入（幂等）
	if err := a.EnsureOrgDefaults(ctx, jia.OrgID); err != nil {
		t.Fatal(err)
	}
	if types, _ := a.ListGoalTypes(ctx, jia); len(types) != 3 {
		t.Fatalf("补齐应当幂等，实际 %d 种", len(types))
	}

	// 读：所有成员都能读（新建目标要选它）
	if _, err := a.ListGoalTypes(ctx, yi); err != nil {
		t.Fatalf("普通成员应当能读目标类型：%v", err)
	}
}

// 增删改与权限（ADR 0023 第 6 条：与能力标签、价格表同一道门）。
func TestGoalTypeCRUDAndPermission(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)

	// 写要 org_settings：普通成员一律 403
	if _, err := a.CreateGoalType(ctx, yi, GoalTypePatch{Name: sp("战役")}); err == nil {
		t.Fatal("没有组织设置权限的人不该能新建目标类型")
	}
	created, err := a.CreateGoalType(ctx, jia, GoalTypePatch{Name: sp("  战役  "), Color: sp("#5e6ad2"), Icon: sp("target")})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "战役" {
		t.Fatalf("类型名应去掉首尾空白，实际 %q", created.Name)
	}
	if created.Sort != 4 {
		t.Fatalf("不给排序时应排到最后（内置三种之后），实际 %d", created.Sort)
	}
	if evs := goalTypeEvents(t, a, ctx, jia, created.ID); len(evs) != 1 || evs[0].Type != "GoalTypeCreated" {
		t.Fatalf("新建应记一条 GoalTypeCreated：%v", evs)
	}

	// 名字重复、空名字、超长名字都拒，理由是完整句子
	if _, err := a.CreateGoalType(ctx, jia, GoalTypePatch{Name: sp("战役")}); err == nil {
		t.Fatal("重名应被拒")
	} else if s := userErrText(t, err); !strings.Contains(s, "已经有一个叫「战役」的目标类型") {
		t.Fatalf("理由不对：%s", s)
	}
	if _, err := a.CreateGoalType(ctx, jia, GoalTypePatch{Name: sp("   ")}); err == nil {
		t.Fatal("空名字应被拒")
	} else if s := userErrText(t, err); !strings.Contains(s, "目标类型要有名字") {
		t.Fatalf("理由不对：%s", s)
	}

	// 改颜色 / 图标 / 排序 / 默认粒度：合记一条 GoalTypeUpdated
	q := domain.PrecisionQuarter
	if _, err := a.UpdateGoalType(ctx, jia, created.ID, GoalTypePatch{Color: sp("#d9a53b"), Sort: intp(9), DefaultPrecision: &q}); err != nil {
		t.Fatal(err)
	}
	evs := goalTypeEvents(t, a, ctx, jia, created.ID)
	if len(evs) != 2 || evs[0].Type != "GoalTypeUpdated" {
		t.Fatalf("改显示应记一条 GoalTypeUpdated：%v", kindsOf(evs))
	}
	// 什么都没变时不记动态
	if _, err := a.UpdateGoalType(ctx, jia, created.ID, GoalTypePatch{Color: sp("#d9a53b")}); err != nil {
		t.Fatal(err)
	}
	if evs := goalTypeEvents(t, a, ctx, jia, created.ID); len(evs) != 2 {
		t.Fatalf("没有变化不该记动态：%v", kindsOf(evs))
	}

	// 普通成员不能改、不能删
	if _, err := a.UpdateGoalType(ctx, yi, created.ID, GoalTypePatch{Name: sp("x")}); err == nil {
		t.Fatal("没有组织设置权限的人不该能改目标类型")
	}
	if err := a.DeleteGoalType(ctx, yi, created.ID); err == nil {
		t.Fatal("没有组织设置权限的人不该能删目标类型")
	}

	// 没有目标在用，可以删
	if err := a.DeleteGoalType(ctx, jia, created.ID); err != nil {
		t.Fatal(err)
	}
	if evs := goalTypeEvents(t, a, ctx, jia, created.ID); len(evs) != 3 || evs[0].Type != "GoalTypeDeleted" {
		t.Fatalf("删除应记一条 GoalTypeDeleted：%v", kindsOf(evs))
	}
	if _, err := a.UpdateGoalType(ctx, jia, created.ID, GoalTypePatch{Name: sp("x")}); err == nil {
		t.Fatal("删掉的类型再改应当 404")
	}
}

func kindsOf(rows []*store.EventRow) []string {
	out := make([]string, 0, len(rows))
	for _, e := range rows {
		out = append(out, e.Type)
	}
	return out
}

// 有目标在用的类型不许删，只能停用（ADR 0023 第 3 条，与团队、能力标签同一套规则）。
func TestGoalTypeDeleteRefusedWhileInUse(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)
	product := typeByName(t, a, ctx, jia, "产品")

	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "把新版官网上线", TypeID: product.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "官网信息架构重排", TypeID: product.ID}); err != nil {
		t.Fatal(err)
	}
	if got := typeByName(t, a, ctx, jia, "产品").GoalCount; got != 2 {
		t.Fatalf("类型下面应有 2 个目标，实际 %d", got)
	}
	err := a.DeleteGoalType(ctx, jia, product.ID)
	if err == nil {
		t.Fatal("还有目标在用的类型不该能删")
	}
	if s := userErrText(t, err); s != "「产品」下面还有 2 个目标，先把它们改成别的类型或者停用这个类型。" {
		t.Fatalf("拒绝理由不对：%s", s)
	}
}

// 停用之后新建时选不到，已有目标照常显示（ADR 0023 第 3 条）。
func TestGoalTypeDeactivateHidesFromCreation(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)
	product := typeByName(t, a, ctx, jia, "产品")

	g, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "把新版官网上线", TypeID: product.ID})
	if err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := a.UpdateGoalType(ctx, jia, product.ID, GoalTypePatch{Active: &off}); err != nil {
		t.Fatal(err)
	}
	evs := goalTypeEvents(t, a, ctx, jia, product.ID)
	if len(evs) == 0 || evs[0].Type != "GoalTypeDeactivated" {
		t.Fatalf("停用应记一条 GoalTypeDeactivated：%v", kindsOf(evs))
	}

	// 新建时选不到，理由是完整句子
	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "另一个", TypeID: product.ID}); err == nil {
		t.Fatal("停用的类型不该能用来新建目标")
	} else if s := userErrText(t, err); !strings.Contains(s, "目标类型「产品」已停用") {
		t.Fatalf("理由不对：%s", s)
	}
	// 把别的目标改成这个类型也不行
	other, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "别的目标"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateGoal(ctx, jia, other.ID, UpdateGoalInput{TypeID: &product.ID}); err == nil {
		t.Fatal("停用的类型不该能被指到别的目标上")
	}

	// 已有目标照常显示这个类型；改这个目标的别的字段也不该被停用挡住
	view, err := a.GetGoal(ctx, jia, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Type == nil || view.Type.Name != "产品" {
		t.Fatalf("已有目标应照常显示已停用的类型：%+v", view.Type)
	}
	if _, err := a.UpdateGoal(ctx, jia, g.ID, UpdateGoalInput{Title: sp("把新版官网上线（改）")}); err != nil {
		t.Fatalf("类型停用不该挡住这个目标的其它编辑：%v", err)
	}

	// 恢复
	on := true
	if _, err := a.UpdateGoalType(ctx, jia, product.ID, GoalTypePatch{Active: &on}); err != nil {
		t.Fatal(err)
	}
	if evs := goalTypeEvents(t, a, ctx, jia, product.ID); evs[0].Type != "GoalTypeReactivated" {
		t.Fatalf("恢复应记一条 GoalTypeReactivated：%v", kindsOf(evs))
	}
	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "恢复之后又能选了", TypeID: product.ID}); err != nil {
		t.Fatal(err)
	}
}

// 改名只改显示，不动历史（ADR 0023 第 6 条）：目标还是那个目标，旧动态还念旧名字。
func TestGoalTypeRenameKeepsHistory(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)
	product := typeByName(t, a, ctx, jia, "产品")
	project := typeByName(t, a, ctx, jia, "项目")

	g, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "把新版官网上线", TypeID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	// 换类型：一条 GoalFieldChanged，带当时的两个名字
	if _, err := a.UpdateGoal(ctx, jia, g.ID, UpdateGoalInput{TypeID: &product.ID}); err != nil {
		t.Fatal(err)
	}
	evs := goalEvents(t, a, ctx, jia, g.ID)
	if len(evs) != 1 || evs[0].Data["field"] != "type_id" {
		t.Fatalf("换类型应记一条 type_id 的动态：%v", fieldsOf(evs))
	}
	if evs[0].Data["from_title"] != "项目" || evs[0].Data["to_title"] != "产品" {
		t.Fatalf("动态里应记下当时的名字：%v", evs[0].Data)
	}

	// 改名
	renamed, err := a.UpdateGoalType(ctx, jia, product.ID, GoalTypePatch{Name: sp("产品线")})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != product.ID {
		t.Fatal("改名不该换编号")
	}
	tevs := goalTypeEvents(t, a, ctx, jia, product.ID)
	if tevs[0].Type != "GoalTypeRenamed" || tevs[0].Data["from"] != "产品" || tevs[0].Data["name"] != "产品线" {
		t.Fatalf("改名应记一条 GoalTypeRenamed，带新旧名字：%v", tevs[0].Data)
	}

	// 历史一个字不动：目标还挂在同一个类型上，旧动态里还是「产品」
	view, err := a.GetGoal(ctx, jia, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.TypeID != product.ID || view.Type.Name != "产品线" {
		t.Fatalf("目标应仍挂在同一个类型上，显示新名字：%+v", view.Type)
	}
	evs = goalEvents(t, a, ctx, jia, g.ID)
	if evs[0].Data["to_title"] != "产品" {
		t.Fatalf("改名不该动历史，旧动态应仍念「产品」：%v", evs[0].Data)
	}
}

// 按类型筛选目标树，以及 none（未分类）。
func TestGoalTreeFilterByType(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)
	product := typeByName(t, a, ctx, jia, "产品")
	project := typeByName(t, a, ctx, jia, "项目")

	root, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "把新版官网上线", TypeID: product.ID})
	if err != nil {
		t.Fatal(err)
	}
	child, err := a.CreateGoal(ctx, jia, CreateGoalInput{ParentID: root.ID, Title: "官网信息架构重排", TypeID: product.ID})
	if err != nil {
		t.Fatal(err)
	}
	mvp, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "AxiomOS MVP 版本", TypeID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	none, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "行政：季度例行事项"})
	if err != nil {
		t.Fatal(err)
	}

	ids := func(f GoalFilter) []string {
		roots, err := a.GoalTreeFiltered(ctx, jia, f)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, v := range roots {
			out = append(out, v.ID)
		}
		return out
	}
	// 命中的目标各自做顶级；命中的目标之下又命中的留在原位，不重复出现
	if got := ids(GoalFilter{Type: product.ID}); len(got) != 1 || got[0] != root.ID {
		t.Fatalf("按「产品」筛应只返回顶层的那个：%v（子目标 %s 应留在它下面）", got, child.ID)
	}
	if got := ids(GoalFilter{Type: project.ID}); len(got) != 1 || got[0] != mvp.ID {
		t.Fatalf("按「项目」筛应返回 MVP：%v", got)
	}
	if got := ids(GoalFilter{Type: "none"}); len(got) != 1 || got[0] != none.ID {
		t.Fatalf("none 应返回未分类的那个：%v", got)
	}
	if _, err := a.GoalTreeFiltered(ctx, jia, GoalFilter{Type: "gtype_不存在"}); err == nil {
		t.Fatal("不存在的类型编号应当被拒")
	}
}

// 默认时间粒度是「预填」，不是「限制」（ADR 0023 第 2 条）：只在新建、且没显式填粒度时生效。
func TestGoalTypeDefaultPrecisionPrefill(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, _ := newTestOrg(t, a, ctx)
	product := typeByName(t, a, ctx, jia, "产品") // 默认到季度
	project := typeByName(t, a, ctx, jia, "项目") // 默认到周

	// 没填粒度 → 用类型的默认值
	g, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "把新版官网上线", TypeID: product.ID})
	if err != nil {
		t.Fatal(err)
	}
	if g.DatePrecision != domain.PrecisionQuarter {
		t.Fatalf("应预填类型的默认粒度「到季度」，实际 %q", g.DatePrecision)
	}
	// 显式填了 → 以填的为准
	g2, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "官网信息架构重排", TypeID: product.ID, DatePrecision: domain.PrecisionMonth})
	if err != nil {
		t.Fatal(err)
	}
	if g2.DatePrecision != domain.PrecisionMonth {
		t.Fatalf("显式填的粒度应当优先，实际 %q", g2.DatePrecision)
	}
	// 类型的默认粒度是「到周」时落库仍是空（week 与空是同一件事）
	g3, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "AxiomOS MVP 版本", TypeID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	if domain.EffectiveDatePrecision(g3.DatePrecision) != domain.PrecisionWeek {
		t.Fatalf("「项目」应预填到周，实际 %q", g3.DatePrecision)
	}
	// 没有类型 → 不预填
	g4, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "行政：季度例行事项"})
	if err != nil {
		t.Fatal(err)
	}
	if g4.DatePrecision != "" {
		t.Fatalf("没有类型时不该预填，实际 %q", g4.DatePrecision)
	}
	// 后来才挂上类型 → 不改已有目标的粒度（预填只发生在新建那一刻）
	if _, err := a.UpdateGoal(ctx, jia, g4.ID, UpdateGoalInput{TypeID: &product.ID}); err != nil {
		t.Fatal(err)
	}
	after, err := a.GetGoal(ctx, jia, g4.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.DatePrecision != "" {
		t.Fatalf("改类型不该动已有目标的时间粒度，实际 %q", after.DatePrecision)
	}
	// 清空类型：一条「清空了」的动态
	if _, err := a.UpdateGoal(ctx, jia, g4.ID, UpdateGoalInput{TypeID: sp("")}); err != nil {
		t.Fatal(err)
	}
	evs := goalEvents(t, a, ctx, jia, g4.ID)
	if evs[0].Data["field"] != "type_id" || evs[0].Data["to"] != nil {
		t.Fatalf("清空类型应记一条 to 为空的动态：%v", evs[0].Data)
	}
}

// 成本按目标类型分组（ADR 0023 第 5 条）：没有类型的目标归「未分类」。
func TestCostStatsByGoalType(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	product := typeByName(t, a, ctx, jia, "产品")

	run := func(sess *Session, goalID, title string, tokens int64) {
		t.Helper()
		ag, token, err := a.RegisterAgent(ctx, sess, RegisterAgentInput{Name: title + " 的 Agent",
			Grants: map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect}})
		if err != nil {
			t.Fatal(err)
		}
		agSess, err := a.SessionFromAgentToken(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		task, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goalID, Title: title, AssigneeID: ag.ID, Ready: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Transition(ctx, agSess, task.ID, "start", TransitionPayload{}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Heartbeat(ctx, agSess, task.ID, []domain.Usage{{ModelID: "claude-sonnet-5", InputTokens: tokens}}); err != nil {
			t.Fatal(err)
		}
	}

	typed, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "把新版官网上线", TypeID: product.ID})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "行政：季度例行事项"})
	if err != nil {
		t.Fatal(err)
	}
	run(yi, typed.ID, "官网首屏", 2_000_000)
	run(jia, plain.ID, "写周报模板", 1_000_000)

	jia.Scope = ScopeAll
	buckets, err := a.CostStats(ctx, jia, "goal_type")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]StatBucket{}
	for _, b := range buckets {
		byKey[b.Key] = b
	}
	if b, ok := byKey[product.ID]; !ok || b.Title != "产品" || b.Value <= 0 {
		t.Fatalf("应有一档「产品」的成本：%+v", buckets)
	}
	if b, ok := byKey[""]; !ok || b.Title != "未分类" || b.Value <= 0 {
		t.Fatalf("没有类型的目标应归「未分类」：%+v", buckets)
	}
	if byKey[product.ID].Value <= byKey[""].Value {
		t.Fatalf("「产品」用了两倍 token，成本应更高：%+v", buckets)
	}
	// 分组档位没写错时旧的几档照旧
	if _, err := a.CostStats(ctx, jia, "team"); err != nil {
		t.Fatal(err)
	}
}
