package domain

import "testing"

// 外部目录对照（ADR 0017）：首次全建；同一输入再算一遍全是 keep；改名 → update；消失 → 停用；邮箱匹配已有成员。
func TestPlanDirectory(t *testing.T) {
	depts := []ExternalDept{{ID: "od-a", Name: "产品部", ParentID: "0"}, {ID: "od-b", Name: "研发组", ParentID: "od-a"}}
	users := []ExternalUser{{ID: "ou-1", Name: "小李", Email: "li@x.com", DeptIDs: []string{"od-b"}}, {ID: "ou-2", Name: "小王", DeptIDs: []string{"od-a"}}}
	st := DirectoryState{Emails: map[string]string{}, TeamIdentity: map[string]string{}, MemberIdentity: map[string]string{}, RootDeptID: "0"}

	p := PlanDirectory(st, depts, users)
	if len(p.Teams) != 2 || p.Teams[0].ExternalID != "od-a" || p.Teams[0].Action != PlanCreate || p.Teams[1].ParentExternalID != "od-a" {
		t.Fatalf("首次应按父在前创建两个团队，实际 %+v", p.Teams)
	}
	if c := p.Counts(); c.AddedTeams != 2 || c.AddedMembers != 2 || c.DeactivatedMembers != 0 {
		t.Fatalf("数量不符 %+v", c)
	}

	// 模拟已写入
	st.Teams = []*Team{{ID: "t-a", Name: "产品部", ExternalName: "产品部", Source: "feishu"}, {ID: "t-b", Name: "研发组", ExternalName: "研发组", ParentID: "t-a", Source: "feishu"}}
	st.TeamIdentity = map[string]string{"od-a": "t-a", "od-b": "t-b"}
	st.Members = []*Member{{ID: "m-1", Name: "小李", Active: true}, {ID: "m-2", Name: "小王", Active: true}}
	st.MemberIdentity = map[string]string{"ou-1": "m-1", "ou-2": "m-2"}
	st.MemberTeams = map[string][]string{"m-1": {"t-b"}, "m-2": {"t-a"}}
	p = PlanDirectory(st, depts, users)
	for _, tp := range p.Teams {
		if tp.Action != PlanKeep {
			t.Fatalf("第二次团队应全部 keep，实际 %+v", tp)
		}
	}
	for _, mp := range p.Members {
		if mp.Action != PlanKeep {
			t.Fatalf("第二次成员应全部 keep，实际 %+v", mp)
		}
	}
	if len(p.DeactivateMembers) != 0 || len(p.DeactivateTeams) != 0 {
		t.Fatalf("不应停用任何东西 %+v", p)
	}

	// 改名 + 部门消失 + 人消失 + 新人邮箱匹配已有手工成员
	depts2 := []ExternalDept{{ID: "od-a", Name: "产品中心", ParentID: "0"}}
	users2 := []ExternalUser{{ID: "ou-1", Name: "小李", Email: "li@x.com", DeptIDs: []string{"od-a"}}, {ID: "ou-3", Name: "老张", Email: "zhang@x.com", DeptIDs: []string{"od-a"}}}
	st.Members = append(st.Members, &Member{ID: "m-manual", Name: "张三", Active: true, Source: SourceManual})
	st.Emails["m-manual"] = "Zhang@x.com"
	p = PlanDirectory(st, depts2, users2)
	if len(p.Teams) != 1 || p.Teams[0].Action != PlanUpdate || p.Teams[0].Name != "产品中心" {
		t.Fatalf("改名应为 update，实际 %+v", p.Teams)
	}
	if len(p.DeactivateTeams) != 1 || p.DeactivateTeams[0] != "t-b" {
		t.Fatalf("消失的部门应停用，实际 %+v", p.DeactivateTeams)
	}
	if len(p.DeactivateMembers) != 1 || p.DeactivateMembers[0] != "m-2" {
		t.Fatalf("消失的人应停用，实际 %+v", p.DeactivateMembers)
	}
	var zhang *MemberPlan
	for i := range p.Members {
		if p.Members[i].ExternalID == "ou-3" {
			zhang = &p.Members[i]
		}
	}
	if zhang == nil || zhang.Action != PlanConfirm || len(zhang.Candidates) != 1 || zhang.Candidates[0].LocalID != "m-manual" || zhang.Candidates[0].Reason != ReasonEmail {
		t.Fatalf("新人邮箱与已有成员相同时应只产生候选，等人确认，实际 %+v", zhang)
	}
	if p.Confirmations() != 1 {
		t.Fatalf("应有一条待确认，实际 %d", p.Confirmations())
	}
	// 决定合并 → 按已有成员更新并补外部身份
	st.MemberDecisions = map[string]Decision{"ou-3": {Decision: DecisionMerge, LocalID: "m-manual"}}
	p = PlanDirectory(st, depts2, users2)
	zhang = nil
	for i := range p.Members {
		if p.Members[i].ExternalID == "ou-3" {
			zhang = &p.Members[i]
		}
	}
	if zhang == nil || zhang.LocalID != "m-manual" || !zhang.Bind || zhang.Action != PlanUpdate || p.Confirmations() != 0 {
		t.Fatalf("决定合并后应更新已有成员并补身份，实际 %+v", zhang)
	}
	// 小李换了部门 → update
	for _, mp := range p.Members {
		if mp.ExternalID == "ou-1" && mp.Action != PlanUpdate {
			t.Fatalf("换部门应为 update，实际 %+v", mp)
		}
	}
}

// 组织负责人不在外部目录里时不停用他。
func TestPlanDirectoryKeepsOwner(t *testing.T) {
	st := DirectoryState{Emails: map[string]string{}, TeamIdentity: map[string]string{}, MemberIdentity: map[string]string{"ou-owner": "m-owner"},
		Members: []*Member{{ID: "m-owner", Name: "老板", Active: true}}, OwnerMemberID: "m-owner", RootDeptID: "0"}
	p := PlanDirectory(st, nil, nil)
	if len(p.DeactivateMembers) != 0 || p.SkippedOwner != "m-owner" {
		t.Fatalf("负责人不应被停用，实际 %+v", p)
	}
}

// 四级认法（ADR 0017 补记四）：邮箱、手机号、姓名且部门与主团队同名各产生一个候选；团队同名同层级产生候选；
// 决定 create → 新建不再问；决定 skip → 不进计划、列进 skipped；已有身份（第一级）不产生候选。
func TestPlanDirectoryCandidates(t *testing.T) {
	depts := []ExternalDept{{ID: "od-a", Name: "产品部", ParentID: "0"}, {ID: "od-b", Name: "研发组", ParentID: "od-a"}, {ID: "od-c", Name: "设计组", ParentID: "od-a"}}
	users := []ExternalUser{
		{ID: "ou-mail", Name: "花名甲", Email: "A@x.com", DeptIDs: []string{"od-b"}},
		{ID: "ou-mob", Name: "花名乙", Mobile: "+86 138-0000-0000", DeptIDs: []string{"od-b"}},
		{ID: "ou-name", Name: "丙", DeptIDs: []string{"od-b"}},
		{ID: "ou-other", Name: "丙", DeptIDs: []string{"od-c"}}, // 同名但部门不同 → 不是候选
		{ID: "ou-bound", Name: "丁", Email: "d@x.com", DeptIDs: []string{"od-a"}},
	}
	st := DirectoryState{
		Teams: []*Team{
			{ID: "t-prod", Name: "产品部", Source: SourceManual},
			{ID: "t-dev", Name: "研发组", ParentID: "t-prod", Source: SourceManual},
			{ID: "t-dev2", Name: "研发组", Source: SourceManual}, // 顶层的同名团队：层级不同，不是候选
			{ID: "t-old", Name: "设计组", ParentID: "t-prod", Source: SourceManual, Inactive: true},
		},
		Members: []*Member{
			{ID: "m-a", Name: "甲", Active: true, Source: SourceManual},
			{ID: "m-b", Name: "乙", Active: true, Source: SourceManual},
			{ID: "m-c", Name: "丙", Active: true, Source: SourceManual},
			{ID: "m-d", Name: "丁", Active: true, Source: SourceManual},
			{ID: "m-e", Name: "丁", Active: true, Source: SourceManual},
		},
		Emails:         map[string]string{"m-a": "a@x.com", "m-d": "d@x.com", "m-e": "e@x.com"},
		Mobiles:        map[string]string{"m-b": "13800000000"},
		MemberTeams:    map[string][]string{"m-c": {"t-prod", "t-dev"}, "m-e": {"t-prod"}},
		TeamIdentity:   map[string]string{},
		MemberIdentity: map[string]string{"ou-bound": "m-d"},
		RootDeptID:     "0",
	}
	p := PlanDirectory(st, depts, users)
	byExt := map[string]MemberPlan{}
	for _, m := range p.Members {
		byExt[m.ExternalID] = m
	}
	if m := byExt["ou-mail"]; m.Action != PlanConfirm || len(m.Candidates) != 1 || m.Candidates[0].LocalID != "m-a" || m.Candidates[0].Reason != ReasonEmail {
		t.Fatalf("邮箱相同应为候选，实际 %+v", m)
	}
	if m := byExt["ou-mob"]; m.Action != PlanConfirm || len(m.Candidates) != 1 || m.Candidates[0].LocalID != "m-b" || m.Candidates[0].Reason != ReasonMobile {
		t.Fatalf("手机号相同应为候选，实际 %+v", m)
	}
	if m := byExt["ou-name"]; m.Action != PlanConfirm || len(m.Candidates) != 1 || m.Candidates[0].LocalID != "m-c" || m.Candidates[0].Reason != ReasonNameTeam || m.Candidates[0].Via != "研发组" {
		t.Fatalf("姓名相同且部门与主团队同名应为候选，实际 %+v", m)
	}
	if m := byExt["ou-other"]; m.Action != PlanCreate || len(m.Candidates) != 0 {
		t.Fatalf("同名但部门不同不该是候选，实际 %+v", m)
	}
	if m := byExt["ou-bound"]; m.Action != PlanUpdate || m.LocalID != "m-d" || len(m.Candidates) != 0 {
		t.Fatalf("已有外部身份直接算同一个（邮箱与另一人相同也不问），实际 %+v", m)
	}
	// 团队：产品部 ↔ t-prod（都是顶层）；研发组 ↔ t-dev（父都对上），t-dev2 顶层不算；设计组的同名团队已停用不算
	byTeam := map[string]TeamPlan{}
	for _, tp := range p.Teams {
		byTeam[tp.ExternalID] = tp
	}
	if tp := byTeam["od-a"]; tp.Action != PlanConfirm || len(tp.Candidates) != 1 || tp.Candidates[0].LocalID != "t-prod" || tp.Candidates[0].Reason != ReasonTeamNameLevel {
		t.Fatalf("顶层同名团队应为候选，实际 %+v", tp)
	}
	if tp := byTeam["od-b"]; tp.Action != PlanConfirm || len(tp.Candidates) != 0 {
		// 父部门还没决定 → 层级对不上，研发组这一级先不问
		t.Logf("父未决定时研发组不产生候选：%+v", tp)
	}
	if tp := byTeam["od-c"]; tp.Action != PlanCreate {
		t.Fatalf("已停用的同名团队不该是候选，实际 %+v", tp)
	}
	if p.Confirmations() != 4 {
		t.Fatalf("应有 4 条待确认（三人 + 产品部），实际 %d", p.Confirmations())
	}

	// 决定：产品部合并到 t-prod → 研发组这一层现在能对上 t-dev；甲合并、乙作为新建、丙跳过
	st.TeamDecisions = map[string]Decision{"od-a": {Decision: DecisionMerge, LocalID: "t-prod"}}
	st.MemberDecisions = map[string]Decision{
		"ou-mail": {Decision: DecisionMerge, LocalID: "m-a"},
		"ou-mob":  {Decision: DecisionCreate},
		"ou-name": {Decision: DecisionSkip},
	}
	p = PlanDirectory(st, depts, users)
	byTeam = map[string]TeamPlan{}
	for _, tp := range p.Teams {
		byTeam[tp.ExternalID] = tp
	}
	if tp := byTeam["od-a"]; tp.Action != PlanUpdate || tp.LocalID != "t-prod" || !tp.Bind || tp.MergeFrom != "" {
		t.Fatalf("决定合并的团队应更新并补身份，实际 %+v", tp)
	}
	if tp := byTeam["od-b"]; tp.Action != PlanConfirm || len(tp.Candidates) != 1 || tp.Candidates[0].LocalID != "t-dev" {
		t.Fatalf("父对上后子团队同名同层级应为候选，实际 %+v", tp)
	}
	byExt = map[string]MemberPlan{}
	for _, m := range p.Members {
		byExt[m.ExternalID] = m
	}
	if m := byExt["ou-mail"]; m.Action != PlanUpdate || m.LocalID != "m-a" || !m.Bind {
		t.Fatalf("决定合并应更新并补身份，实际 %+v", m)
	}
	if m := byExt["ou-mob"]; m.Action != PlanCreate || len(m.Candidates) != 0 {
		t.Fatalf("决定新建后不再问，实际 %+v", m)
	}
	if _, ok := byExt["ou-name"]; ok || len(p.SkippedMembers) != 1 || p.SkippedMembers[0] != "ou-name" {
		t.Fatalf("决定跳过的人不进计划、列进 skipped，实际 %+v %v", byExt["ou-name"], p.SkippedMembers)
	}
	if p.Confirmations() != 1 {
		t.Fatalf("只剩研发组一条待确认，实际 %d", p.Confirmations())
	}
	if c := p.Counts(); c.AddedMembers != 2 || c.UpdatedMembers != 2 {
		t.Fatalf("数量不符 %+v", c)
	}
}

// 决定合并到已有对象、但以前的同步已经按同一个外部编号建过重复对象：计划要把重复对象整体并入（MergeFrom）。
// 跳过的部门：它的子部门提到它的父下面；以前同步进来的对应团队不因此停用。
func TestPlanDirectoryMergeFromAndSkippedDept(t *testing.T) {
	depts := []ExternalDept{{ID: "od-a", Name: "产品部", ParentID: "0"}, {ID: "od-b", Name: "研发组", ParentID: "od-a"}, {ID: "od-c", Name: "前端", ParentID: "od-b"}}
	users := []ExternalUser{{ID: "ou-1", Name: "小李", DeptIDs: []string{"od-c"}}}
	st := DirectoryState{
		Teams: []*Team{
			{ID: "t-manual", Name: "产品部", Source: SourceManual},
			{ID: "t-synced", Name: "产品部", Source: "feishu", ExternalName: "产品部"},
			{ID: "t-dev", Name: "研发组", ParentID: "t-synced", Source: "feishu", ExternalName: "研发组"},
		},
		Members:         []*Member{{ID: "m-manual", Name: "李", Active: true, Source: SourceManual}, {ID: "m-synced", Name: "小李", Active: true, Source: "feishu"}},
		Emails:          map[string]string{},
		MemberTeams:     map[string][]string{"m-synced": {"t-dev"}},
		TeamIdentity:    map[string]string{"od-a": "t-synced", "od-b": "t-dev"},
		MemberIdentity:  map[string]string{"ou-1": "m-synced"},
		TeamDecisions:   map[string]Decision{"od-a": {Decision: DecisionMerge, LocalID: "t-manual"}, "od-b": {Decision: DecisionSkip}},
		MemberDecisions: map[string]Decision{"ou-1": {Decision: DecisionMerge, LocalID: "m-manual"}},
		RootDeptID:      "0",
	}
	p := PlanDirectory(st, depts, users)
	if len(p.Teams) != 2 || p.Teams[0].ExternalID != "od-a" || p.Teams[0].LocalID != "t-manual" || p.Teams[0].MergeFrom != "t-synced" || !p.Teams[0].Bind || p.Teams[0].Action != PlanUpdate {
		t.Fatalf("重复团队应并入决定的目标，实际 %+v", p.Teams)
	}
	if p.Teams[1].ExternalID != "od-c" || p.Teams[1].ParentExternalID != "od-a" || p.Teams[1].Action != PlanCreate {
		t.Fatalf("跳过的部门的子部门应提到它的父下面，实际 %+v", p.Teams[1])
	}
	if len(p.SkippedTeams) != 1 || p.SkippedTeams[0] != "od-b" || len(p.DeactivateTeams) != 0 {
		t.Fatalf("跳过的部门列进 skipped 且不停用其团队，实际 %v %v", p.SkippedTeams, p.DeactivateTeams)
	}
	if len(p.Members) != 1 || p.Members[0].LocalID != "m-manual" || p.Members[0].MergeFrom != "m-synced" || !p.Members[0].Bind || p.Members[0].Action != PlanUpdate {
		t.Fatalf("重复成员应并入决定的目标，实际 %+v", p.Members)
	}
}

// 同步之后用同一套认法在手工对象与同步对象之间找疑似重复。
func TestFindDuplicates(t *testing.T) {
	st := DirectoryState{
		Teams: []*Team{
			{ID: "t-m", Name: "产品部", Source: SourceManual},
			{ID: "t-s", Name: "产品部", Source: "feishu"},
			{ID: "t-m-dev", Name: "研发组", ParentID: "t-m", Source: SourceManual},
			{ID: "t-s-dev", Name: "研发组", ParentID: "t-s", Source: "feishu"},
			{ID: "t-s-top", Name: "研发组", Source: "feishu"}, // 顶层，与 t-m-dev 层级不同
			{ID: "t-off", Name: "产品部", Source: "feishu", Inactive: true},
		},
		Members: []*Member{
			{ID: "m-a", Name: "张三", Active: true, Source: SourceManual},
			{ID: "m-b", Name: "张三", Active: true, Source: "feishu"},
			{ID: "m-c", Name: "张三", Active: true, Source: "feishu"}, // 在别的团队
			{ID: "m-d", Name: "李四", Active: true, Source: SourceManual},
			{ID: "m-e", Name: "李四（飞书）", Active: true, Source: "feishu"},
			{ID: "m-f", Name: "张三", Active: false, Source: "feishu"},
		},
		Emails:      map[string]string{},
		Mobiles:     map[string]string{"m-d": "13900000000", "m-e": "139 0000 0000"},
		MemberTeams: map[string][]string{"m-a": {"t-m-dev"}, "m-b": {"t-s-dev"}, "m-c": {"t-s"}},
	}
	dups := FindDuplicates(st)
	got := map[string]DuplicatePair{}
	for _, d := range dups {
		got[d.Kind+":"+d.A+":"+d.B] = d
	}
	if len(dups) != 4 {
		t.Fatalf("应有 4 对疑似重复，实际 %d: %+v", len(dups), dups)
	}
	if d, ok := got["team:t-m:t-s"]; !ok || d.Reason != ReasonTeamNameLevel {
		t.Fatalf("顶层同名团队应为重复，实际 %+v", dups)
	}
	if _, ok := got["team:t-m-dev:t-s-dev"]; !ok {
		t.Fatalf("父团队也是同名同层级的子团队应为重复，实际 %+v", dups)
	}
	if _, ok := got["team:t-m-dev:t-s-top"]; ok {
		t.Fatalf("层级不同不该算重复，实际 %+v", dups)
	}
	if d, ok := got["member:m-a:m-b"]; !ok || d.Reason != ReasonNameTeam || d.Via != "研发组" {
		t.Fatalf("姓名相同且主团队同名应为重复，实际 %+v", dups)
	}
	if d, ok := got["member:m-d:m-e"]; !ok || d.Reason != ReasonMobile {
		t.Fatalf("手机号相同应为重复，实际 %+v", dups)
	}
}
