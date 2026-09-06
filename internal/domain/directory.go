package domain

import "strings"

// 外部目录同步的纯函数内核（ADR 0017）：拿到外部目录的部门树与人员名单，对照本系统现状，
// 算出一份"该做什么"的计划。预览与真正的同步用的是同一份计划，只是后者把它写下去。
//
// 认人认团队分四级（ADR 0017 补记四）：
//   - 成员：外部身份 → 邮箱 → 手机号 → 姓名相同且所在部门与本地主团队同名；
//   - 团队：外部身份 → 同名且同层级（父团队也对上，或都是顶层）。
// 第一级直接算同一个；其余各级只产生候选（Action = confirm），要人决定：合并到已有 / 作为新建 / 跳过。
// 决定存成记录（DirectoryState.Decisions），下次对照直接按决定走。

// ExternalDept 是外部目录里的一个部门。
type ExternalDept struct {
	ID       string
	Name     string
	ParentID string // 父部门编号；根部门下的父编号是根编号
}

// ExternalUser 是外部目录里的一个人。
type ExternalUser struct {
	ID      string
	Name    string
	Email   string
	Mobile  string
	DeptIDs []string // 所属部门编号（可多个）
}

// 决定的三种取值（ADR 0017 补记四）。
const (
	DecisionMerge  = "merge"  // 合并到已有的本地对象
	DecisionCreate = "create" // 作为新建
	DecisionSkip   = "skip"   // 跳过，不同步
)

// Decision 是人对一个外部对象的决定。
type Decision struct {
	Decision string
	LocalID  string // merge 时的本地对象
}

// 候选的认法。
const (
	ReasonEmail         = "email"           // 邮箱相同
	ReasonMobile        = "mobile"          // 手机号相同
	ReasonNameTeam      = "name_team"       // 姓名相同且所在部门与本地主团队同名
	ReasonTeamNameLevel = "team_name_level" // 团队同名同层级
	ReasonNameUnique    = "name_unique"     // 姓名相同，且两边都只有这一个同名的人
)

// MatchCandidate 是一个可能是同一个对象的本地候选。Via 是认出它时用到的团队名（name_team 时）。
type MatchCandidate struct {
	LocalID string `json:"local_id"`
	Reason  string `json:"reason"`
	Via     string `json:"via,omitempty"`
}

// DirectoryState 是本系统当前与同步有关的现状。
type DirectoryState struct {
	Teams          []*Team
	Members        []*Member
	Emails         map[string]string // 成员 ID → 邮箱（小写）
	Mobiles        map[string]string // 成员 ID → 手机号（本系统暂不存成员手机号，留空即跳过第三级）
	MemberTeams    map[string][]string
	TeamIdentity   map[string]string // 部门编号 → 团队 ID
	MemberIdentity map[string]string // 人员编号 → 成员 ID
	// TeamDecisions / MemberDecisions 是已经做过的决定，键是外部编号。
	TeamDecisions   map[string]Decision
	MemberDecisions map[string]Decision
	OwnerMemberID   string
	RootDeptID      string // 同步根部门；它自身不映射为团队，除非 IncludeRoot
	IncludeRoot     bool
}

// 计划动作。
const (
	PlanCreate  = "create"
	PlanUpdate  = "update"
	PlanKeep    = "keep"
	PlanConfirm = "confirm" // 有候选、还没决定：预览列进「需要你确认」，同步不执行
)

// TeamPlan 是对一个部门的处理。
type TeamPlan struct {
	ExternalID       string `json:"external_id"`
	Name             string `json:"name"`
	ParentExternalID string `json:"parent_external_id,omitempty"`
	LocalID          string `json:"local_id,omitempty"`
	Action           string `json:"action"`
	// Bind 为真表示这次要补一条外部身份（按决定合并到已有团队）。
	Bind bool `json:"bind,omitempty"`
	// MergeFrom 非空表示以前的同步已经建过一个重复团队，这次要把它整体并入 LocalID。
	MergeFrom string `json:"merge_from,omitempty"`
	// Candidates 只在 Action = confirm 时有。
	Candidates []MatchCandidate `json:"candidates,omitempty"`
}

// MemberPlan 是对一个人的处理。
type MemberPlan struct {
	ExternalID string   `json:"external_id"`
	Name       string   `json:"name"`
	Email      string   `json:"email,omitempty"`
	Mobile     string   `json:"mobile,omitempty"`
	DeptIDs    []string `json:"dept_ids"`
	LocalID    string   `json:"local_id,omitempty"`
	Action     string   `json:"action"`
	// Bind 为真表示这次要补一条外部身份（按决定合并到已有成员）。
	Bind bool `json:"bind,omitempty"`
	// MergeFrom 非空表示以前的同步已经建过一个重复成员，这次要把它并入 LocalID。
	MergeFrom  string `json:"merge_from,omitempty"`
	Reactivate bool   `json:"reactivate,omitempty"` // 之前被停用、这次又出现
	// Candidates 只在 Action = confirm 时有。
	Candidates []MatchCandidate `json:"candidates,omitempty"`
}

// DirectoryPlan 是一次同步要做的全部事情。
type DirectoryPlan struct {
	Teams             []TeamPlan // 按父在前、子在后排好，创建时可以直接顺序执行
	DeactivateTeams   []string   // 外部目录里已消失的团队 ID
	Members           []MemberPlan
	DeactivateMembers []string // 外部目录里已消失的成员 ID
	SkippedOwner      string   // 组织负责人不在外部目录里时不停用他，记在这里
	SkippedTeams      []string // 按决定跳过的部门编号
	SkippedMembers    []string // 按决定跳过的人员编号
}

// Confirmations 是还没决定的候选数：大于零时同步不执行。
func (p DirectoryPlan) Confirmations() int {
	n := 0
	for _, t := range p.Teams {
		if t.Action == PlanConfirm {
			n++
		}
	}
	for _, m := range p.Members {
		if m.Action == PlanConfirm {
			n++
		}
	}
	return n
}

// Counts 汇总计划里的数量。
func (p DirectoryPlan) Counts() DirectoryCounts {
	var c DirectoryCounts
	for _, t := range p.Teams {
		switch t.Action {
		case PlanCreate:
			c.AddedTeams++
		case PlanUpdate:
			c.UpdatedTeams++
		}
	}
	c.DeactivatedTeams = len(p.DeactivateTeams)
	for _, m := range p.Members {
		switch m.Action {
		case PlanCreate:
			c.AddedMembers++
		case PlanUpdate:
			c.UpdatedMembers++
		}
	}
	c.DeactivatedMembers = len(p.DeactivateMembers)
	return c
}

// DirectoryCounts 是一次同步的数量汇总。
type DirectoryCounts struct {
	AddedTeams         int `json:"added_teams"`
	UpdatedTeams       int `json:"updated_teams"`
	DeactivatedTeams   int `json:"deactivated_teams"`
	AddedMembers       int `json:"added_members"`
	UpdatedMembers     int `json:"updated_members"`
	DeactivatedMembers int `json:"deactivated_members"`
}

// PlanDirectory 对照现状算出计划。不写任何东西。
func PlanDirectory(st DirectoryState, depts []ExternalDept, users []ExternalUser) DirectoryPlan {
	var plan DirectoryPlan
	teamByID := map[string]*Team{}
	for _, t := range st.Teams {
		teamByID[t.ID] = t
	}
	memberByID := map[string]*Member{}
	for _, m := range st.Members {
		memberByID[m.ID] = m
	}
	teamParents := map[string]string{}
	for _, t := range st.Teams {
		teamParents[t.ID] = t.ParentID
	}

	// ---- 团队：先按决定去掉跳过的部门（它的子部门提到它的父下面），再按父在前排序 ----
	depts = dropSkippedDepts(depts, st.TeamDecisions, &plan)
	present := map[string]ExternalDept{}
	for _, d := range depts {
		if d.ID == "" {
			continue
		}
		present[d.ID] = d
	}
	isRoot := func(id string) bool { return id == "" || id == st.RootDeptID }
	ordered := orderDepts(depts, present, isRoot)
	// claimedTeams：已被某个外部身份或合并决定占用的本地团队，不再当别人的候选
	claimedTeams := map[string]bool{}
	for _, local := range st.TeamIdentity {
		claimedTeams[local] = true
	}
	for _, d := range st.TeamDecisions {
		if d.Decision == DecisionMerge && d.LocalID != "" {
			claimedTeams[d.LocalID] = true
		}
	}
	// resolved：这次对照里每个部门对应的本地团队（身份或合并决定），子部门判层级用
	resolved := map[string]string{}
	for ext, local := range st.TeamIdentity {
		resolved[ext] = local
	}
	for ext, d := range st.TeamDecisions {
		if d.Decision == DecisionMerge && d.LocalID != "" {
			if _, ok := teamByID[d.LocalID]; ok {
				resolved[ext] = d.LocalID
			}
		}
	}
	for _, d := range ordered {
		parent := d.ParentID
		if isRoot(parent) && !(st.IncludeRoot && parent == st.RootDeptID && d.ID != st.RootDeptID) {
			parent = ""
		}
		if _, ok := present[parent]; !ok {
			parent = ""
		}
		tp := TeamPlan{ExternalID: d.ID, Name: strings.TrimSpace(d.Name), ParentExternalID: parent, Action: PlanCreate}
		wantParent := ""
		parentResolved := parent == ""
		if parent != "" {
			wantParent = resolved[parent] // 父还没建时为空，执行时再补
			parentResolved = wantParent != ""
		}
		local, bound := st.TeamIdentity[d.ID]
		decision, decided := st.TeamDecisions[d.ID]
		switch {
		case decided && decision.Decision == DecisionMerge && decision.LocalID != "" && teamByID[decision.LocalID] != nil:
			// 按决定合并到已有团队；以前同步建过的重复团队整体并入
			tp.LocalID, tp.Action = decision.LocalID, PlanUpdate
			if !bound || local != decision.LocalID {
				tp.Bind = true
			}
			if bound && local != decision.LocalID && teamByID[local] != nil {
				tp.MergeFrom = local
			}
			resolved[d.ID] = decision.LocalID
		case bound && teamByID[local] != nil:
			t := teamByID[local]
			tp.LocalID = local
			tp.Action = PlanKeep
			if t.Name != tp.Name || t.Inactive || t.ExternalName != tp.Name || (wantParent != "" && t.ParentID != wantParent) || (parent == "" && t.ParentID != "" && t.Source != SourceManual) {
				tp.Action = PlanUpdate
			}
			// 父部门是这次才新建的，本团队的父也要改
			if parent != "" && wantParent == "" {
				tp.Action = PlanUpdate
			}
		case decided && decision.Decision == DecisionCreate:
			// 按决定新建，不再问
		default:
			// 第二级：同名且同层级的本地团队是候选
			if parentResolved {
				for _, t := range st.Teams {
					if t.Inactive || claimedTeams[t.ID] || strings.TrimSpace(t.Name) != tp.Name || t.ParentID != wantParent {
						continue
					}
					tp.Candidates = append(tp.Candidates, MatchCandidate{LocalID: t.ID, Reason: ReasonTeamNameLevel})
				}
			}
			if len(tp.Candidates) > 0 {
				tp.Action = PlanConfirm
			}
		}
		plan.Teams = append(plan.Teams, tp)
	}
	for ext, local := range st.TeamIdentity {
		if _, ok := present[ext]; ok {
			continue
		}
		if _, skipped := st.TeamDecisions[ext]; skipped && st.TeamDecisions[ext].Decision == DecisionSkip {
			continue // 跳过的部门：它以前同步进来的团队不因此停用，留给人处理
		}
		if t, ok := teamByID[local]; ok && !t.Inactive {
			plan.DeactivateTeams = append(plan.DeactivateTeams, local)
		}
	}
	sortStrings(plan.DeactivateTeams)

	// ---- 成员 ----
	emailToMember := map[string]string{}
	for id, e := range st.Emails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			emailToMember[e] = id
		}
	}
	mobileToMember := map[string]string{}
	for id, m := range st.Mobiles {
		if m = normalizeMobile(m); m != "" {
			mobileToMember[m] = id
		}
	}
	deptName := map[string]string{}
	for _, d := range depts {
		deptName[d.ID] = strings.TrimSpace(d.Name)
	}
	seenUsers := map[string]bool{}
	claimed := map[string]bool{} // 已被某个外部身份或合并决定占用的本地成员
	for _, local := range st.MemberIdentity {
		claimed[local] = true
	}
	for _, d := range st.MemberDecisions {
		if d.Decision == DecisionMerge && d.LocalID != "" {
			claimed[d.LocalID] = true
		}
	}
	imNameCount := map[string]int{}
	for _, u := range users {
		if n := strings.TrimSpace(u.Name); n != "" {
			imNameCount[n]++
		}
	}
	localNameCount := map[string]int{}
	for _, m := range st.Members {
		if m.Active {
			localNameCount[strings.TrimSpace(m.Name)]++
		}
	}
	for _, u := range users {
		if u.ID == "" || seenUsers[u.ID] {
			continue
		}
		seenUsers[u.ID] = true
		if d, ok := st.MemberDecisions[u.ID]; ok && d.Decision == DecisionSkip {
			plan.SkippedMembers = append(plan.SkippedMembers, u.ID)
			continue
		}
		mp := MemberPlan{ExternalID: u.ID, Name: strings.TrimSpace(u.Name), Email: strings.ToLower(strings.TrimSpace(u.Email)), Mobile: normalizeMobile(u.Mobile), Action: PlanCreate, DeptIDs: []string{}}
		for _, d := range u.DeptIDs {
			if _, ok := present[d]; ok {
				mp.DeptIDs = append(mp.DeptIDs, d)
			}
		}
		sortStrings(mp.DeptIDs)
		local, bound := st.MemberIdentity[u.ID]
		decision, decided := st.MemberDecisions[u.ID]
		switch {
		case decided && decision.Decision == DecisionMerge && decision.LocalID != "" && memberByID[decision.LocalID] != nil:
			mp.LocalID = decision.LocalID
			if !bound || local != decision.LocalID {
				mp.Bind = true
			}
			if bound && local != decision.LocalID && memberByID[local] != nil {
				mp.MergeFrom = local
			}
			local, bound = decision.LocalID, true
		case bound:
		case decided && decision.Decision == DecisionCreate:
		default:
			// 第二至四级：只产生候选
			if mp.Email != "" {
				if cand, found := emailToMember[mp.Email]; found && !claimed[cand] {
					mp.Candidates = append(mp.Candidates, MatchCandidate{LocalID: cand, Reason: ReasonEmail})
				}
			}
			if mp.Mobile != "" {
				if cand, found := mobileToMember[mp.Mobile]; found && !claimed[cand] && !hasCandidate(mp.Candidates, cand) {
					mp.Candidates = append(mp.Candidates, MatchCandidate{LocalID: cand, Reason: ReasonMobile})
				}
			}
			if mp.Name != "" {
				for _, m := range st.Members {
					if claimed[m.ID] || hasCandidate(mp.Candidates, m.ID) || strings.TrimSpace(m.Name) != mp.Name {
						continue
					}
					primary := teamByID[primaryTeamOf(teamParents, st.MemberTeams[m.ID])]
					if primary == nil {
						continue
					}
					for _, d := range mp.DeptIDs {
						if deptName[d] != "" && deptName[d] == strings.TrimSpace(primary.Name) {
							mp.Candidates = append(mp.Candidates, MatchCandidate{LocalID: m.ID, Reason: ReasonNameTeam, Via: primary.Name})
							break
						}
					}
				}
			}
			// 第五级：同名，且飞书里和系统里都只有这一个同名的人
			if len(mp.Candidates) == 0 && mp.Name != "" && imNameCount[mp.Name] == 1 && localNameCount[mp.Name] == 1 {
				for _, m := range st.Members {
					if m.Active && !claimed[m.ID] && strings.TrimSpace(m.Name) == mp.Name {
						mp.Candidates = append(mp.Candidates, MatchCandidate{LocalID: m.ID, Reason: ReasonNameUnique})
						break
					}
				}
			}
			if len(mp.Candidates) > 0 {
				mp.Action = PlanConfirm
			}
		}
		if bound {
			m := memberByID[local]
			mp.LocalID = local
			if m == nil {
				// 身份指向的成员不存在了（不应发生），当作新建
				mp.LocalID, mp.Action, mp.Bind, mp.MergeFrom = "", PlanCreate, false, ""
			} else {
				mp.Action = PlanKeep
				if mp.Bind || mp.MergeFrom != "" || m.Name != mp.Name {
					mp.Action = PlanUpdate
				}
				if !m.Active {
					mp.Action, mp.Reactivate = PlanUpdate, true
				}
				if !sameTeams(st.MemberTeams[local], mp.DeptIDs, resolved) {
					mp.Action = PlanUpdate
				}
			}
		}
		plan.Members = append(plan.Members, mp)
	}
	for ext, local := range st.MemberIdentity {
		if seenUsers[ext] {
			continue
		}
		m, ok := memberByID[local]
		if !ok || !m.Active {
			continue
		}
		if local == st.OwnerMemberID {
			plan.SkippedOwner = local
			continue
		}
		plan.DeactivateMembers = append(plan.DeactivateMembers, local)
	}
	sortStrings(plan.DeactivateMembers)
	sortStrings(plan.SkippedMembers)
	sortStrings(plan.SkippedTeams)
	return plan
}

// dropSkippedDepts 去掉按决定跳过的部门；它的子部门提到它的父下面，人员里对它的归属自然消失。
func dropSkippedDepts(depts []ExternalDept, decisions map[string]Decision, plan *DirectoryPlan) []ExternalDept {
	skipped := map[string]string{} // 跳过的部门 → 它的父
	for _, d := range depts {
		if dec, ok := decisions[d.ID]; ok && dec.Decision == DecisionSkip {
			skipped[d.ID] = d.ParentID
			plan.SkippedTeams = append(plan.SkippedTeams, d.ID)
		}
	}
	if len(skipped) == 0 {
		return depts
	}
	out := make([]ExternalDept, 0, len(depts))
	for _, d := range depts {
		if _, ok := skipped[d.ID]; ok {
			continue
		}
		for i := 0; i < 64; i++ {
			p, ok := skipped[d.ParentID]
			if !ok {
				break
			}
			d.ParentID = p
		}
		out = append(out, d)
	}
	return out
}

func hasCandidate(cs []MatchCandidate, id string) bool {
	for _, c := range cs {
		if c.LocalID == id {
			return true
		}
	}
	return false
}

// normalizeMobile 只留数字，去掉加号、空格、连字符；超过 11 位时视为带国家码，只取最后 11 位。
func normalizeMobile(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 11 {
		out = out[len(out)-11:]
	}
	return out
}

// primaryTeamOf 是成员的主团队：所属团队里树上最深的那个（多个同深时取 ID 最小），与成本归口一致。
func primaryTeamOf(parents map[string]string, ids []string) string {
	best, bestDepth := "", -1
	for _, id := range ids {
		depth := 0
		for cur, i := parents[id], 0; cur != "" && i < 64; cur, i = parents[cur], i+1 {
			depth++
		}
		if depth > bestDepth || (depth == bestDepth && id < best) {
			best, bestDepth = id, depth
		}
	}
	return best
}

// sameTeams 判断成员现有团队与外部部门映射到的团队是否一致（未建的部门视为不一致）。
func sameTeams(have []string, deptIDs []string, identity map[string]string) bool {
	want := map[string]bool{}
	for _, d := range deptIDs {
		local, ok := identity[d]
		if !ok {
			return false
		}
		want[local] = true
	}
	if len(want) != len(have) {
		return false
	}
	for _, h := range have {
		if !want[h] {
			return false
		}
	}
	return true
}

// orderDepts 把部门按"父在前"排好；孤儿（父不在名单里且不是根）当作顶层。
func orderDepts(depts []ExternalDept, present map[string]ExternalDept, isRoot func(string) bool) []ExternalDept {
	children := map[string][]ExternalDept{}
	var tops []ExternalDept
	for _, d := range depts {
		if d.ID == "" {
			continue
		}
		if _, ok := present[d.ParentID]; ok && d.ParentID != d.ID {
			children[d.ParentID] = append(children[d.ParentID], d)
		} else {
			tops = append(tops, d)
		}
	}
	_ = isRoot
	var out []ExternalDept
	seen := map[string]bool{}
	var walk func(ds []ExternalDept, depth int)
	walk = func(ds []ExternalDept, depth int) {
		for _, d := range ds {
			if seen[d.ID] || depth > 64 {
				continue
			}
			seen[d.ID] = true
			out = append(out, d)
			walk(children[d.ID], depth+1)
		}
	}
	walk(tops, 0)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ---------- 同步之后的疑似重复（ADR 0017 补记四） ----------

// DuplicatePair 是一对疑似重复：A 是手工对象，B 是同步来的对象。
type DuplicatePair struct {
	Kind   string // member | team
	A, B   string // 本地 ID
	Reason string
	Via    string // name_team 时用到的团队名
}

// FindDuplicates 用同一套认法（第二至四级）在手工对象与同步对象之间找疑似重复。
// 邮箱在同一组织里不会重复（成员按账号唯一），所以成员实际靠手机号与"姓名相同且主团队同名"；
// 团队靠同名同层级（父团队相同，或父团队本身也是一对同名同层级的重复）。
func FindDuplicates(st DirectoryState) []DuplicatePair {
	var out []DuplicatePair
	teamByID := map[string]*Team{}
	parents := map[string]string{}
	for _, t := range st.Teams {
		teamByID[t.ID] = t
		parents[t.ID] = t.ParentID
	}
	// 团队
	memo := map[string]bool{}
	var sameLevel func(a, b *Team, depth int) bool
	sameLevel = func(a, b *Team, depth int) bool {
		if depth > 64 {
			return false
		}
		if a.ParentID == "" || b.ParentID == "" {
			return a.ParentID == "" && b.ParentID == ""
		}
		if a.ParentID == b.ParentID {
			return true
		}
		key := a.ParentID + "|" + b.ParentID
		if v, ok := memo[key]; ok {
			return v
		}
		pa, pb := teamByID[a.ParentID], teamByID[b.ParentID]
		v := pa != nil && pb != nil && strings.TrimSpace(pa.Name) == strings.TrimSpace(pb.Name) && sameLevel(pa, pb, depth+1)
		memo[key] = v
		return v
	}
	for _, a := range st.Teams {
		if a.Inactive || a.Source != SourceManual {
			continue
		}
		for _, b := range st.Teams {
			if b.Inactive || b.Source == SourceManual || strings.TrimSpace(a.Name) != strings.TrimSpace(b.Name) || !sameLevel(a, b, 0) {
				continue
			}
			out = append(out, DuplicatePair{Kind: "team", A: a.ID, B: b.ID, Reason: ReasonTeamNameLevel})
		}
	}
	// 成员
	mobileOf := func(id string) string { return normalizeMobile(st.Mobiles[id]) }
	for _, a := range st.Members {
		if !a.Active || a.Source != SourceManual {
			continue
		}
		pa := teamByID[primaryTeamOf(parents, st.MemberTeams[a.ID])]
		for _, b := range st.Members {
			if !b.Active || b.Source == SourceManual || a.ID == b.ID {
				continue
			}
			if e := strings.ToLower(strings.TrimSpace(st.Emails[a.ID])); e != "" && e == strings.ToLower(strings.TrimSpace(st.Emails[b.ID])) {
				out = append(out, DuplicatePair{Kind: "member", A: a.ID, B: b.ID, Reason: ReasonEmail})
				continue
			}
			if m := mobileOf(a.ID); m != "" && m == mobileOf(b.ID) {
				out = append(out, DuplicatePair{Kind: "member", A: a.ID, B: b.ID, Reason: ReasonMobile})
				continue
			}
			if strings.TrimSpace(a.Name) != strings.TrimSpace(b.Name) || pa == nil {
				continue
			}
			pb := teamByID[primaryTeamOf(parents, st.MemberTeams[b.ID])]
			if pb != nil && strings.TrimSpace(pa.Name) == strings.TrimSpace(pb.Name) {
				out = append(out, DuplicatePair{Kind: "member", A: a.ID, B: b.ID, Reason: ReasonNameTeam, Via: pa.Name})
			}
		}
	}
	// 第五级：手工成员与同步成员同名，且两边都只有这一个同名的人
	manualNames, syncedNames := map[string]int{}, map[string]int{}
	for _, m := range st.Members {
		if !m.Active {
			continue
		}
		if m.Source == SourceManual {
			manualNames[strings.TrimSpace(m.Name)]++
		} else {
			syncedNames[strings.TrimSpace(m.Name)]++
		}
	}
	paired := map[string]bool{}
	for _, d := range out {
		if d.Kind == "member" {
			paired[d.A] = true
		}
	}
	for _, a := range st.Members {
		n := strings.TrimSpace(a.Name)
		if !a.Active || a.Source != SourceManual || paired[a.ID] || n == "" || manualNames[n] != 1 || syncedNames[n] != 1 {
			continue
		}
		for _, b := range st.Members {
			if b.Active && b.Source != SourceManual && strings.TrimSpace(b.Name) == n {
				out = append(out, DuplicatePair{Kind: "member", A: a.ID, B: b.ID, Reason: ReasonNameUnique})
				break
			}
		}
	}
	return out
}
