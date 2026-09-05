package domain

// 可见域（ADR 0013）：一个成员能看到哪些团队，由组织的可见性策略与团队树上的
// 共享边界标记算出来。这是纯函数，不依赖数据库，改规则先改这里并补测试。

// maxTeamDepth 是向上走的步数上限，兼作环路保护（数据坏掉时不会转死循环）。
const maxTeamDepth = 64

// NearestBoundary 从 teamID 沿上级一路向上找最近的共享边界团队（teamID 自己也算）。
// 一路到顶都没有边界时返回空串。
func NearestBoundary(teams []*Team, teamID string) string {
	byID := map[string]*Team{}
	for _, t := range teams {
		byID[t.ID] = t
	}
	id, _ := nearestBoundary(byID, teamID)
	return id
}

// nearestBoundary 返回最近的边界团队。reached 为假表示走到了深度上限还没走完
// （数据成环或树深得离谱），这时按看得更少处理，绝不放大可见范围。
func nearestBoundary(byID map[string]*Team, teamID string) (id string, reached bool) {
	seen := map[string]bool{}
	cur := teamID
	for i := 0; i < maxTeamDepth; i++ {
		t := byID[cur]
		if t == nil {
			return "", true
		}
		if seen[cur] {
			return "", true // 成环：当作一路无边界
		}
		seen[cur] = true
		if t.IsBoundary {
			return t.ID, true
		}
		if t.ParentID == "" {
			return "", true
		}
		cur = t.ParentID
	}
	return "", false
}

// Subtree 返回某团队及其全部下级团队的 ID（含自己），带环路保护。
func Subtree(teams []*Team, rootID string) []string {
	children := map[string][]string{}
	for _, t := range teams {
		children[t.ParentID] = append(children[t.ParentID], t.ID)
	}
	out := []string{}
	seen := map[string]bool{}
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		if id == "" || seen[id] || depth > maxTeamDepth {
			return
		}
		seen[id] = true
		out = append(out, id)
		for _, c := range children[id] {
			walk(c, depth+1)
		}
	}
	walk(rootID, 0)
	return out
}

// VisibleTeams 算一个成员的可见域：
//
//   - org：全组织（all 为真，teams 为空）。
//   - team_tree：他所属的每个团队的整棵子树，取并集。
//   - boundary：他所属的每个团队向上找到最近的共享边界，可见域是那个边界团队的整棵
//     子树；某个团队一路向上都没有边界，则这个成员看全组织。属于多个团队时取并集。
//
// 不标记任何边界时 boundary 等价于 org；每个团队都标记时等价于 team_tree。
// 返回的团队 ID 按 teams 的顺序去重，结果稳定。
func VisibleTeams(teams []*Team, myTeamIDs []string, policy Visibility) (ids []string, all bool) {
	if policy == VisibilityOrg {
		return []string{}, true
	}
	byID := map[string]*Team{}
	for _, t := range teams {
		byID[t.ID] = t
	}
	roots := []string{}
	for _, id := range myTeamIDs {
		if byID[id] == nil {
			continue
		}
		switch policy {
		case VisibilityBoundary:
			b, reached := nearestBoundary(byID, id)
			if !reached {
				roots = append(roots, id) // 没走完：退到自己这棵子树，宁可看得少
				continue
			}
			if b == "" {
				return []string{}, true // 一路无边界 = 看全组织
			}
			roots = append(roots, b)
		default: // team_tree
			roots = append(roots, id)
		}
	}
	seen := map[string]bool{}
	for _, r := range roots {
		for _, x := range Subtree(teams, r) {
			seen[x] = true
		}
	}
	ids = []string{}
	for _, t := range teams { // 按团队表顺序输出，结果稳定
		if seen[t.ID] {
			ids = append(ids, t.ID)
		}
	}
	return ids, false
}
