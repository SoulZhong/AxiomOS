package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 范围（ADR 0013）：你正在看的是全公司还是某个团队（含其全部下级团队）。
//
// 「谁能看到多少」不是产品事实，而是管理策略：组织有两项可见性设置
// （collaboration_visibility / finance_visibility，取值 org | boundary | team_tree），
// 团队树上还能标共享边界（Team.IsBoundary）。可见域的算法是内核里的纯函数
// domain.VisibleTeams，这里只负责把它接到会话与请求参数上。代码里不写死任何一家
// 公司的管理方式，只有默认值。
//
// 跨边界的例外：组织负责人永远看全部；「查看全部工作」跨边界看协作数据，「查看全部
// 成本」跨边界看财务数据。
//
// 唯一不可配置的是成本归口（见 TeamOfExecutor）：那是「算得对」的问题，不是管理策略。

// ScopeAll 是全公司范围的 ID。
const ScopeAll = "all"

// ScopeMine 是「我能看到的全部」：策略把范围收窄到自己团队树时的默认档。
const ScopeMine = "mine"

// Scope 是解析后的范围。
type Scope struct {
	ID      string   // all | mine | 团队 ID
	Title   string   // 当前语言的名称
	All     bool     // 全公司（含「未分组」）
	TeamIDs []string // 团队及其全部下级；All 为真时为空
	Root    string   // 范围根团队 ID（All 或 mine 时为空）
	Finance bool     // 当前登录者能不能在这个范围里看财务数据
}

// HasTeam 判断某个归口团队（空串表示「未分组」）在不在范围内。
// Includes 是协作数据用的范围判断：属于范围内某个团队，或者根本不属于任何团队（未分组）。
// 范围是按团队筛选，不属于任何团队的目标 / 任务不能被"团队筛选"筛掉，否则只有切到全公司才看得见，
// 刚建的东西会从眼前消失。财务口径仍用 HasTeam（未分组的钱只在全公司汇总里出现）。
func (s *Scope) Includes(teamID string) bool {
	return teamID == "" || s.HasTeam(teamID)
}

func (s *Scope) HasTeam(teamID string) bool {
	if s == nil || s.All {
		return true
	}
	for _, id := range s.TeamIDs {
		if id == teamID {
			return true
		}
	}
	return false
}

// teamDescendants 是内核 domain.Subtree 的简写（团队及其全部下级，含自己）。
func teamDescendants(teams []*domain.Team, rootID string) []string {
	return domain.Subtree(teams, rootID)
}

// TeamDescendants 返回某团队及其全部下级团队的 ID（含自己）。
func (a *App) TeamDescendants(ctx context.Context, tx pgx.Tx, rootID string) ([]string, error) {
	teams, err := a.Store.ListTeams(ctx, tx)
	if err != nil {
		return nil, err
	}
	return teamDescendants(teams, rootID), nil
}

// MyTeamRoots 返回当前登录者直接所属的团队（Agent 用所有者的）。
func MyTeamRoots(sess *Session) []string {
	if sess == nil {
		return nil
	}
	return sess.TeamIDs
}

// CanSeeFinance 判断当前登录者能不能看这个范围里的财务数据。
func CanSeeFinance(sess *Session, scope *Scope) bool {
	if sess == nil {
		return false
	}
	if scope == nil || scope.All {
		return sess.FinanceAll()
	}
	if scope.Root == "" { // mine：自己的可见域，按策略逐个判断
		for _, id := range scope.TeamIDs {
			if !sess.CanSeeFinanceTeam(id) {
				return false
			}
		}
		return len(scope.TeamIDs) > 0 || sess.FinanceAll()
	}
	return sess.CanSeeFinanceTeam(scope.Root)
}

// ResolveScope 把 scope 参数解析成范围。scope 为空时用会话上本次请求的参数。
// finance 为真表示这是财务接口：越权的范围直接 403，理由说明是哪条策略。
func (a *App) ResolveScope(ctx context.Context, tx pgx.Tx, sess *Session, raw string, finance bool) (*Scope, error) {
	if raw == "" {
		raw = sess.Scope
	}
	raw = strings.TrimSpace(raw)
	loc := sess.Loc()
	mine := func(finance bool) *Scope {
		ids := sess.CollabTeamIDs
		if finance {
			ids = sess.FinanceTeamIDs
		}
		s := &Scope{ID: ScopeMine, Title: i18n.Tr(loc, "scope.mine"), TeamIDs: append([]string{}, ids...)}
		s.Finance = CanSeeFinance(sess, s)
		return s
	}
	all := func() *Scope {
		s := &Scope{ID: ScopeAll, Title: i18n.Tr(loc, "scope.all"), All: true}
		s.Finance = sess.FinanceAll()
		return s
	}
	switch raw {
	case "", ScopeAll, ScopeMine:
		explicit := raw == ScopeAll
		if raw == ScopeMine {
			return mine(finance), nil
		}
		if finance {
			if sess.FinanceAll() {
				return all(), nil
			}
			if explicit {
				return nil, Forbidden(financeDeniedKey(sess))
			}
			return mine(true), nil
		}
		if sess.CollabAll() {
			return all(), nil
		}
		if explicit {
			return nil, Forbidden(collabDeniedKey(sess, true))
		}
		return mine(false), nil
	}
	teams, err := a.Store.ListTeams(ctx, tx)
	if err != nil {
		return nil, err
	}
	var team *domain.Team
	for _, t := range teams {
		if t.ID == raw {
			team = t
		}
	}
	if team == nil {
		return nil, NotFound("err.team_missing")
	}
	if finance && !sess.CanSeeFinanceTeam(team.ID) {
		return nil, Forbidden(financeDeniedKey(sess))
	}
	if !finance && !sess.CanSeeCollabTeam(team.ID) {
		return nil, Forbidden(collabDeniedKey(sess, false))
	}
	s := &Scope{ID: team.ID, Title: team.Name, TeamIDs: teamDescendants(teams, team.ID), Root: team.ID}
	s.Finance = CanSeeFinance(sess, s)
	return s, nil
}

// financeDeniedKey 按当前生效的策略挑拒绝理由，让人知道是哪条设置挡住的。
func financeDeniedKey(sess *Session) string {
	if sess.FinancePolicy() == domain.VisibilityBoundary {
		return "err.scope_finance_boundary"
	}
	return "err.scope_finance"
}

// collabDeniedKey 同上，协作数据那一侧。
func collabDeniedKey(sess *Session, all bool) string {
	if sess.CollabPolicy() == domain.VisibilityBoundary {
		if all {
			return "err.scope_collab_boundary_all"
		}
		return "err.scope_collab_boundary"
	}
	if all {
		return "err.scope_collab_all"
	}
	return "err.scope_collab"
}

// scope 解析本次请求的范围（协作口径，不会 403 到财务规则上）。
func (a *App) scope(ctx context.Context, tx pgx.Tx, sess *Session) (*Scope, error) {
	return a.ResolveScope(ctx, tx, sess, "", false)
}

// financeScope 解析本次请求的范围（财务口径，越权 403）。
func (a *App) financeScope(ctx context.Context, tx pgx.Tx, sess *Session) (*Scope, error) {
	return a.ResolveScope(ctx, tx, sess, "", true)
}

// ---------- 归口索引 ----------

// OrgIndex 是算范围与归口要用的组织结构快照。
type OrgIndex struct {
	Teams        []*domain.Team
	TeamName     map[string]string
	Parent       map[string]string
	Members      []*domain.Member
	Agents       []*domain.Agent
	Goals        []*domain.Goal
	teamOfMember map[string]string
	ownerOfAgent map[string]string
	goalTeam     map[string]string
}

// OrgIndex 装载团队、成员、Agent、目标。
func (a *App) OrgIndex(ctx context.Context, tx pgx.Tx) (*OrgIndex, error) {
	ix := &OrgIndex{TeamName: map[string]string{}, Parent: map[string]string{}, ownerOfAgent: map[string]string{}, goalTeam: map[string]string{}}
	var err error
	if ix.Teams, err = a.Store.ListTeams(ctx, tx); err != nil {
		return nil, err
	}
	for _, t := range ix.Teams {
		ix.TeamName[t.ID] = t.Name
		ix.Parent[t.ID] = t.ParentID
	}
	// 成本归口必须唯一且确定（ADR 0013）：多团队成员取树上最深的那个团队（更具体的归属），
	// 深度相同时取 team_id 最小者。不能依赖 SQL 返回顺序。
	memberships, err := a.Store.TeamMemberships(ctx, tx)
	if err != nil {
		return nil, err
	}
	ix.teamOfMember = map[string]string{}
	for m, teams := range memberships {
		best, bestDepth := "", -1
		for _, t := range teams {
			d := ix.depth(t)
			if d > bestDepth || (d == bestDepth && t < best) {
				best, bestDepth = t, d
			}
		}
		ix.teamOfMember[m] = best
	}
	if ix.Members, err = a.Store.ListMembers(ctx, tx); err != nil {
		return nil, err
	}
	if ix.Agents, err = a.Store.ListAgents(ctx, tx); err != nil {
		return nil, err
	}
	for _, ag := range ix.Agents {
		ix.ownerOfAgent[ag.ID] = ag.OwnerMemberID
	}
	if ix.Goals, err = a.Store.ListGoals(ctx, tx); err != nil {
		return nil, err
	}
	for _, g := range ix.Goals {
		ix.goalTeam[g.ID] = g.TeamID
	}
	return ix, nil
}

// depth 返回团队在树上的深度（根为 0）；成环或断链时停在 64 层。
func (ix *OrgIndex) depth(teamID string) int {
	d := 0
	for cur := ix.Parent[teamID]; cur != "" && d < 64; cur = ix.Parent[cur] {
		d++
	}
	return d
}

// TeamOfExecutor 是全系统唯一的成本归口（ADR 0013）：一条执行记录的成本算在执行者
// 所属团队头上；执行者是 Agent 时算在其所有者所属团队；没有团队的算「未分组」（空串）。
//
// 这一条不做成组织可配置：它是「算得对」的问题，不是管理策略——钱是谁花的就归谁，
// 这个口径总能算出来且唯一。目标所属团队作为另一个分组维度并存，但不是归口。
func (ix *OrgIndex) TeamOfExecutor(executorID string) string {
	if executorID == "" {
		return ""
	}
	id := executorID
	if owner, ok := ix.ownerOfAgent[id]; ok {
		id = owner
	}
	return ix.teamOfMember[id]
}

// TeamOfTask 是任务的归口团队：负责人所属团队（同成本归口）；没有负责人时退回任务
// 所属目标的团队，都没有算「未分组」。
func (ix *OrgIndex) TeamOfTask(t *domain.Task) string {
	if tid := ix.TeamOfExecutor(t.AssigneeID); tid != "" {
		return tid
	}
	if t.GoalID != "" {
		return ix.goalTeam[t.GoalID]
	}
	return ""
}

// TeamOfGoal 是目标的归口团队：目标自己的团队。
func (ix *OrgIndex) TeamOfGoal(g *domain.Goal) string { return g.TeamID }

// IsAgent 判断某个执行者是不是 Agent。
func (ix *OrgIndex) IsAgent(id string) bool { _, ok := ix.ownerOfAgent[id]; return ok }

// UnitOf 把一个归口团队折到当前范围下一级的组织单元上。
// 范围是全公司时，单元是根团队（没有团队的归「未分组」）；范围是某个团队时，单元是
// 它的直接下级团队，团队自己的直属成员单独成一个单元；不在范围里的返回 false。
func (ix *OrgIndex) UnitOf(scope *Scope, teamID string) (string, bool) {
	if scope.All {
		if teamID == "" {
			return "", true // 未分组
		}
		cur := teamID
		for i := 0; i < 64; i++ {
			p := ix.Parent[cur]
			if p == "" {
				return cur, true
			}
			cur = p
		}
		return cur, true
	}
	if !scope.HasTeam(teamID) {
		return "", false
	}
	if teamID == scope.Root {
		return scope.Root, true
	}
	cur := teamID
	for i := 0; i < 64; i++ {
		p := ix.Parent[cur]
		if p == scope.Root {
			return cur, true
		}
		if p == "" {
			return scope.Root, true
		}
		cur = p
	}
	return scope.Root, true
}

// ---------- 会话上的策略 ----------

// FinancePolicy 返回组织的财务可见性策略。
func (s *Session) FinancePolicy() domain.Visibility {
	if domain.ValidVisibility(s.financePolicy, domain.FinanceVisibilities) {
		return s.financePolicy
	}
	return domain.DefaultFinanceVisibility
}

// CollabPolicy 返回组织的协作数据可见性策略。
func (s *Session) CollabPolicy() domain.Visibility {
	if domain.ValidVisibility(s.collabPolicy, domain.CollaborationVisibilities) {
		return s.collabPolicy
	}
	return domain.DefaultCollaborationVisibility
}

// FinanceAll 判断能不能看全组织的财务数据（跨边界要「查看全部成本」）。
func (s *Session) FinanceAll() bool {
	return s.financeAll || s.IsOwner || s.Can("view_all_cost")
}

// CanSeeFinanceTeam 判断能不能看某个团队（及其下级）的财务数据。
func (s *Session) CanSeeFinanceTeam(teamID string) bool {
	return s.FinanceAll() || contains(s.FinanceTeamIDs, teamID)
}

// CollabAll 判断能不能看全组织的协作数据（跨边界要「查看全部工作」）。
func (s *Session) CollabAll() bool {
	return s.collabAll || s.IsOwner || s.Can("view_all_work")
}

// CanSeeCollabTeam 判断能不能看某个团队（及其下级）的协作数据。
func (s *Session) CanSeeCollabTeam(teamID string) bool {
	return s.CollabAll() || contains(s.CollabTeamIDs, teamID)
}

// loadScopeContext 补齐会话上的团队、策略与两侧可见域（登录时算一次）。
func (a *App) loadScopeContext(ctx context.Context, tx pgx.Tx, sess *Session, org *domain.Organization) error {
	sess.collabPolicy, sess.financePolicy = org.Collaboration(), org.Finance()
	roots, err := a.Store.TeamsOfMember(ctx, tx, sess.MemberID)
	if err != nil {
		return err
	}
	teams, err := a.Store.ListTeams(ctx, tx)
	if err != nil {
		return err
	}
	sess.TeamIDs = roots
	sess.CollabTeamIDs, sess.collabAll = domain.VisibleTeams(teams, roots, sess.CollabPolicy())
	sess.FinanceTeamIDs, sess.financeAll = domain.VisibleTeams(teams, roots, sess.FinancePolicy())
	return nil
}

// ScopeOption 是范围选择器里的一档。
type ScopeOption struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Depth     int    `json:"depth"`
	Financial bool   `json:"financial"`
}

// ScopeOptions 返回当前登录者可切的范围，以及默认选中的那一档。
func (a *App) ScopeOptions(ctx context.Context, tx pgx.Tx, sess *Session) ([]ScopeOption, string, error) {
	teams, err := a.Store.ListTeams(ctx, tx)
	if err != nil {
		return nil, "", err
	}
	out := []ScopeOption{}
	if sess.CollabAll() {
		out = append(out, ScopeOption{ID: ScopeAll, Title: i18n.Tr(sess.Loc(), "scope.all"), Depth: 0, Financial: sess.FinanceAll()})
	}
	children := map[string][]*domain.Team{}
	for _, t := range teams {
		children[t.ParentID] = append(children[t.ParentID], t)
	}
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, t := range children[parent] {
			if sess.CanSeeCollabTeam(t.ID) {
				out = append(out, ScopeOption{ID: t.ID, Title: t.Name, Depth: depth + 1, Financial: sess.CanSeeFinanceTeam(t.ID)})
			}
			walk(t.ID, depth+1)
		}
	}
	walk("", 0)
	def := ScopeAll
	if len(sess.TeamIDs) > 0 {
		def = sess.TeamIDs[0]
	} else if len(out) > 0 {
		def = out[0].ID
	}
	found := false
	for _, o := range out {
		if o.ID == def {
			found = true
		}
	}
	if !found && len(out) > 0 {
		def = out[0].ID
	}
	return out, def, nil
}

// ---------- 范围筛选 ----------

// filterTasks 按范围裁剪任务。
func (ix *OrgIndex) filterTasks(scope *Scope, tasks []*domain.Task) []*domain.Task {
	if scope == nil || scope.All {
		return tasks
	}
	out := make([]*domain.Task, 0, len(tasks))
	for _, t := range tasks {
		if scope.Includes(ix.TeamOfTask(t)) {
			out = append(out, t)
		}
	}
	return out
}

// filterRuns 按范围裁剪执行记录（成本归口）。
func (ix *OrgIndex) filterRuns(scope *Scope, runs []*store.RunRow) []*store.RunRow {
	if scope == nil || scope.All {
		return runs
	}
	out := make([]*store.RunRow, 0, len(runs))
	for _, r := range runs {
		if scope.HasTeam(ix.TeamOfExecutor(r.ExecutorID)) {
			out = append(out, r)
		}
	}
	return out
}
