package domain

import (
	"sort"
	"strings"
	"testing"
)

// 一棵典型的集团树：
//
//	公司
//	├─ 产品事业部[边界]
//	│  ├─ 研发组
//	│  │  └─ 前端小组
//	│  └─ 市场组
//	├─ 服务事业部[边界]
//	│  ├─ 运维组
//	│  └─ 客服组
//	└─ 投资事业部[边界]
//	   ├─ 甲子公司[边界]
//	   └─ 乙子公司[边界]
func groupTree() []*Team {
	t := func(id, parent string, boundary bool) *Team {
		return &Team{ID: id, ParentID: parent, Name: id, IsBoundary: boundary}
	}
	return []*Team{
		t("co", "", false),
		t("prod", "co", true),
		t("dev", "prod", false),
		t("fe", "dev", false),
		t("mkt", "prod", false),
		t("svc", "co", true),
		t("ops", "svc", false),
		t("cs", "svc", false),
		t("inv", "co", true),
		t("sub_a", "inv", true),
		t("sub_b", "inv", true),
	}
}

func visible(t *testing.T, teams []*Team, mine []string, policy Visibility) string {
	t.Helper()
	ids, all := VisibleTeams(teams, mine, policy)
	if all {
		return "*"
	}
	sorted := append([]string{}, ids...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

func TestVisibleTeamsBoundarySiblingsAreHidden(t *testing.T) {
	teams := groupTree()
	// 前端小组的人，最近边界是产品事业部，能看到整个事业部（含市场组），看不到别的事业部
	if got := visible(t, teams, []string{"fe"}, VisibilityBoundary); got != "dev,fe,mkt,prod" {
		t.Fatalf("前端小组的可见域应是整个产品事业部，实际 %s", got)
	}
	if got := visible(t, teams, []string{"ops"}, VisibilityBoundary); got != "cs,ops,svc" {
		t.Fatalf("运维组的可见域应是整个服务事业部，实际 %s", got)
	}
}

func TestVisibleTeamsBoundaryParentSeesChildrenButChildrenNotEachOther(t *testing.T) {
	teams := groupTree()
	// 投资事业部直属的人：最近边界是事业部自己，能看到下面所有子公司
	if got := visible(t, teams, []string{"inv"}, VisibilityBoundary); got != "inv,sub_a,sub_b" {
		t.Fatalf("事业部直属成员应看到全部子公司，实际 %s", got)
	}
	// 子公司的人：最近边界是自己这家子公司，看不到兄弟子公司，也看不到事业部
	if got := visible(t, teams, []string{"sub_a"}, VisibilityBoundary); got != "sub_a" {
		t.Fatalf("子公司成员只应看到自己，实际 %s", got)
	}
}

func TestVisibleTeamsMultipleTeamsUnion(t *testing.T) {
	teams := groupTree()
	if got := visible(t, teams, []string{"fe", "ops"}, VisibilityBoundary); got != "cs,dev,fe,mkt,ops,prod,svc" {
		t.Fatalf("多团队成员的可见域应取并集，实际 %s", got)
	}
}

func TestVisibleTeamsNoBoundaryMeansWholeOrg(t *testing.T) {
	teams := groupTree()
	// 公司这一层没有边界标记，属于公司的人一路向上找不到边界 → 看全组织
	if got := visible(t, teams, []string{"co"}, VisibilityBoundary); got != "*" {
		t.Fatalf("一路无边界应看全组织，实际 %s", got)
	}
	// 退化一：谁都不标边界 ≡ org
	plain := groupTree()
	for _, x := range plain {
		x.IsBoundary = false
	}
	for _, mine := range [][]string{{"fe"}, {"sub_a"}, {"co"}} {
		if got := visible(t, plain, mine, VisibilityBoundary); got != "*" {
			t.Fatalf("不标边界时 %v 应等价于 org，实际 %s", mine, got)
		}
	}
}

func TestVisibleTeamsEveryTeamBoundaryEqualsTeamTree(t *testing.T) {
	// 退化二：每个团队都标边界 ≡ team_tree
	all := groupTree()
	for _, x := range all {
		x.IsBoundary = true
	}
	for _, mine := range [][]string{{"prod"}, {"dev"}, {"sub_a"}, {"dev", "ops"}} {
		b := visible(t, all, mine, VisibilityBoundary)
		tt := visible(t, all, mine, VisibilityTeamTree)
		if b != tt {
			t.Fatalf("每个团队都标边界时 %v 应等价于 team_tree：boundary=%s team_tree=%s", mine, b, tt)
		}
	}
}

func TestVisibleTeamsOrgAndTeamTree(t *testing.T) {
	teams := groupTree()
	if got := visible(t, teams, []string{"fe"}, VisibilityOrg); got != "*" {
		t.Fatalf("org 策略应看全组织，实际 %s", got)
	}
	if got := visible(t, teams, []string{"dev"}, VisibilityTeamTree); got != "dev,fe" {
		t.Fatalf("team_tree 应是自己团队及下级，实际 %s", got)
	}
	if got := visible(t, teams, nil, VisibilityTeamTree); got != "" {
		t.Fatalf("没有团队的成员在 team_tree 下可见域为空，实际 %s", got)
	}
}

func TestVisibleTeamsCycleAndDepth(t *testing.T) {
	// 数据坏掉成环时不能死循环
	cyc := []*Team{{ID: "a", ParentID: "b"}, {ID: "b", ParentID: "a"}}
	if got := visible(t, cyc, []string{"a"}, VisibilityBoundary); got != "*" {
		t.Fatalf("成环且无边界时应回落到全组织，实际 %s", got)
	}
	cyc[1].IsBoundary = true
	if got := visible(t, cyc, []string{"a"}, VisibilityBoundary); got != "a,b" {
		t.Fatalf("成环但有边界时应取该边界子树，实际 %s", got)
	}
	// 深层嵌套：100 层，只有第 3 层是边界
	deep := []*Team{{ID: "t0"}}
	for i := 1; i < 100; i++ {
		deep = append(deep, &Team{ID: "t" + itoa(i), ParentID: "t" + itoa(i-1), IsBoundary: i == 3})
	}
	if b := NearestBoundary(deep, "t5"); b != "t3" {
		t.Fatalf("深层嵌套里最近边界应是 t3，实际 %s", b)
	}
	if b := NearestBoundary(deep, "t99"); b != "" {
		t.Fatalf("超过深度上限应当返回空而不是死循环，实际 %s", b)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
