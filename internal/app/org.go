package app

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// requireOrgSettings 组织设置类操作的权限门。
func (a *App) requireOrgSettings(sess *Session) error {
	if sess.IsAgent() || !sess.Can("org_settings") {
		return Forbidden("err.org_settings")
	}
	return nil
}

// ---------- 组织本身 ----------

// GetOrganization 返回当前组织。
func (a *App) GetOrganization(ctx context.Context, sess *Session) (*domain.Organization, error) {
	var org *domain.Organization
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		org, err = a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		return
	})
	return org, err
}

// OrgPatch 是组织可改字段。
type OrgPatch struct {
	Name          *string `json:"name"`
	DefaultLocale *string `json:"default_locale"`
	Currency      *string `json:"currency"`
}

// UpdateOrganization 修改组织。
func (a *App) UpdateOrganization(ctx context.Context, sess *Session, in OrgPatch) (*domain.Organization, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	if err := refuseDryRun(sess); err != nil {
		return nil, err
	}
	var org *domain.Organization
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cur, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
			cur.Name = strings.TrimSpace(*in.Name)
		}
		if in.DefaultLocale != nil {
			l := i18n.Normalize(*in.DefaultLocale)
			if l == "" {
				return Bad("err.locale", *in.DefaultLocale)
			}
			cur.DefaultLocale = string(l)
		}
		if in.Currency != nil && *in.Currency != "" {
			cur.Currency = strings.ToUpper(*in.Currency)
		}
		if err := a.Store.UpdateOrganization(ctx, tx, cur); err != nil {
			return err
		}
		org = cur
		return nil
	})
	return org, err
}

// ---------- 可见性策略（ADR 0013） ----------

// VisibilityOption 是一项策略的候选取值。
type VisibilityOption struct {
	Value string `json:"value"`
	Title string `json:"title"`
}

// OrgSettings 是组织的管理策略。「协作数据全员可见、财务数据按团队树」只是默认值，
// 每个组织可以按自己的管理方式改（ADR 0013）。
type OrgSettings struct {
	CollaborationVisibility domain.Visibility  `json:"collaboration_visibility"`
	CollaborationTitle      string             `json:"collaboration_title"`
	CollaborationOptions    []VisibilityOption `json:"collaboration_options"`
	FinanceVisibility       domain.Visibility  `json:"finance_visibility"`
	FinanceTitle            string             `json:"finance_title"`
	FinanceOptions          []VisibilityOption `json:"finance_options"`
}

func visibilityOptions(among []domain.Visibility, loc i18n.Locale) []VisibilityOption {
	out := make([]VisibilityOption, 0, len(among))
	for _, v := range among {
		out = append(out, VisibilityOption{Value: string(v), Title: i18n.Tr(loc, "visibility."+string(v))})
	}
	return out
}

func settingsView(org *domain.Organization, loc i18n.Locale) *OrgSettings {
	return &OrgSettings{
		CollaborationVisibility: org.Collaboration(),
		CollaborationTitle:      i18n.Tr(loc, "visibility."+string(org.Collaboration())),
		CollaborationOptions:    visibilityOptions(domain.CollaborationVisibilities, loc),
		FinanceVisibility:       org.Finance(),
		FinanceTitle:            i18n.Tr(loc, "visibility."+string(org.Finance())),
		FinanceOptions:          visibilityOptions(domain.FinanceVisibilities, loc),
	}
}

// OrgSettingsPatch 是可改的策略字段。
type OrgSettingsPatch struct {
	CollaborationVisibility *string `json:"collaboration_visibility"`
	FinanceVisibility       *string `json:"finance_visibility"`
}

// GetOrgSettings 读取组织的可见性策略。
func (a *App) GetOrgSettings(ctx context.Context, sess *Session) (*OrgSettings, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var out *OrgSettings
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		out = settingsView(org, sess.Loc())
		return nil
	})
	return out, err
}

// UpdateOrgSettings 修改可见性策略；每次改动都记一条动态。
func (a *App) UpdateOrgSettings(ctx context.Context, sess *Session, in OrgSettingsPatch) (*OrgSettings, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var out *OrgSettings
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		if in.CollaborationVisibility != nil {
			v := domain.Visibility(strings.TrimSpace(*in.CollaborationVisibility))
			if !domain.ValidVisibility(v, domain.CollaborationVisibilities) {
				return Bad("err.visibility", string(v))
			}
			org.CollaborationVisibility = v
		}
		if in.FinanceVisibility != nil {
			v := domain.Visibility(strings.TrimSpace(*in.FinanceVisibility))
			if !domain.ValidVisibility(v, domain.FinanceVisibilities) {
				return Bad("err.visibility", string(v))
			}
			org.FinanceVisibility = v
		}
		if err := a.Store.UpdateOrganization(ctx, tx, org); err != nil {
			return err
		}
		out = settingsView(org, sess.Loc())
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "OrgSettingsUpdated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"collaboration_visibility": string(org.Collaboration()), "finance_visibility": string(org.Finance())}}})
	})
	return out, err
}

// ---------- 成员 ----------

// MemberDetail 是组织设置里的成员。
type MemberDetail struct {
	*domain.Member
	Email string `json:"email"`
	// TeamID 是主团队（多团队时取树上最深的那个，与成本归口一致）；TeamIDs 是全部所属团队。
	TeamID  string      `json:"team_id"`
	TeamIDs []string    `json:"team_ids"`
	IsOwner bool        `json:"is_owner"`
	Locale  i18n.Locale `json:"locale"`
	// Invitation 是待激活成员尚未接受的最近一条邀请（ADR 0017）；链接只在创建时返回一次。
	Invitation *domain.Invitation `json:"invitation,omitempty"`
	// PossibleDuplicateOf 是用同一套认法找到的疑似重复（ADR 0017 补记四）：手工成员与同步成员之间；列表调用时算一次。
	PossibleDuplicateOf []DuplicateRef `json:"possible_duplicate_of,omitempty"`
	// AgentCount 是这个人名下没被吊销的 Agent 数：管理员推广接入时靠它看谁还没接入（目标「Agent 快速接入」#19）。
	AgentCount int `json:"agent_count"`
}

// DuplicateRef 是「可能与 X 重复」提示里的 X。
type DuplicateRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Reason     string `json:"reason"`
	ReasonText string `json:"reason_text"`
	// CanBeMergedAway 说 X 能不能当被并走的那个；不能时 KeepReason 是一句原因（见 memberKeepReason）。合并对话框据此挡掉不成立的方向。
	CanBeMergedAway bool   `json:"can_be_merged_away"`
	KeepReason      string `json:"keep_reason,omitempty"`
}

// OrgMembers 列出成员（含停用）。
func (a *App) OrgMembers(ctx context.Context, sess *Session) ([]MemberDetail, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	out := []MemberDetail{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		ms, err := a.Store.ListMembers(ctx, tx)
		if err != nil {
			return err
		}
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return err
		}
		memberships, err := a.Store.TeamMemberships(ctx, tx)
		if err != nil {
			return err
		}
		parents := teamParents(teams)
		// 待激活成员：找出绑定在其邮箱上、尚未接受且未过期的最近一条邀请
		pendingInv := map[string]*domain.Invitation{}
		if invs, err := a.Store.ListInvitations(ctx, tx); err == nil {
			for _, inv := range invs {
				if inv.AcceptedAt != nil || time.Now().After(inv.ExpiresAt) {
					continue
				}
				if _, ok := pendingInv[strings.ToLower(inv.Email)]; !ok {
					pendingInv[strings.ToLower(inv.Email)] = inv
				}
			}
		}
		agentsOf := map[string]int{}
		if ags, err := a.Store.ListAgents(ctx, tx); err == nil {
			for _, ag := range ags {
				if ag.RevokedAt == nil {
					agentsOf[ag.OwnerMemberID]++
				}
			}
		}
		emails := map[string]string{}
		for _, m := range ms {
			ids := memberships[m.ID]
			if ids == nil {
				ids = []string{}
			}
			d := MemberDetail{Member: m, TeamID: primaryTeam(parents, ids), TeamIDs: ids, IsOwner: org.OwnerMemberID == m.ID, Locale: a.localeOfMember(ctx, tx, sess.OrgID, m.ID), AgentCount: agentsOf[m.ID]}
			if acc, err := a.Store.AccountByID(ctx, tx, m.AccountID); err == nil {
				d.Email = acc.Email
				emails[m.ID] = strings.ToLower(acc.Email)
			}
			if m.DerivedStatus() == domain.MemberPendingActivation {
				d.Invitation = pendingInv[strings.ToLower(d.Email)]
			}
			out = append(out, d)
		}
		// 疑似重复（ADR 0017 补记四）：一次列表算一遍，两边都提示
		dups := domain.FindDuplicates(domain.DirectoryState{Teams: teams, Members: ms, Emails: emails, Mobiles: map[string]string{}, MemberTeams: memberships})
		if len(dups) > 0 {
			idx := map[string]int{}
			for i := range out {
				idx[out[i].ID] = i
			}
			loc := sess.Loc()
			// ref 是对面那个人在提示里的样子，带上「他能不能被并走」（规矩见 memberKeepReason），合并对话框据此挡掉不成立的方向
			ref := func(other MemberDetail, reason, text string) DuplicateRef {
				why := memberKeepReason(other.Member, org.OwnerMemberID, loc)
				return DuplicateRef{ID: other.ID, Name: other.Name, Reason: reason, ReasonText: text, CanBeMergedAway: why == "", KeepReason: why}
			}
			for _, dp := range dups {
				if dp.Kind != store.IdentityMember {
					continue
				}
				text := reasonText(domain.MatchCandidate{Reason: dp.Reason, Via: dp.Via}, loc)
				if i, ok := idx[dp.A]; ok {
					out[i].PossibleDuplicateOf = append(out[i].PossibleDuplicateOf, ref(out[idx[dp.B]], dp.Reason, text))
				}
				if i, ok := idx[dp.B]; ok {
					out[i].PossibleDuplicateOf = append(out[i].PossibleDuplicateOf, ref(out[idx[dp.A]], dp.Reason, text))
				}
			}
		}
		return nil
	})
	return out, err
}

// MemberPatch 是成员可改字段。
type MemberPatch struct {
	Name   *string  `json:"name"`
	Roles  []string `json:"roles"`
	Active *bool    `json:"active"`
	TeamID *string  `json:"team_id"`
}

// UpdateMember 修改成员。停用成员时吊销其 Agent、任务回待领取。每处改动各记一条动态。
// 来自IM 集成的成员姓名与所在的同步团队由同步决定，手工改会被拒（ADR 0017）。
func (a *App) UpdateMember(ctx context.Context, sess *Session, id string, in MemberPatch) (*MemberDetail, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		m, err := a.Store.MemberByID(ctx, tx, id)
		if err != nil {
			return err
		}
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		var events []domain.Event
		ev := func(kind string, data map[string]any) {
			data["member_id"], data["name"] = m.ID, m.Name
			events = append(events, domain.Event{Type: kind, ActorID: sess.Actor.ID, At: time.Now(), Data: data})
		}
		if in.Name != nil && strings.TrimSpace(*in.Name) != "" && strings.TrimSpace(*in.Name) != m.Name {
			if m.Source != domain.SourceManual {
				return Bad("err.member_synced_name", SourceText(m.Source))
			}
			old := m.Name
			m.Name = strings.TrimSpace(*in.Name)
			ev("MemberRenamed", map[string]any{"from": old})
		}
		if in.Roles != nil {
			if err := a.checkRoles(ctx, tx, in.Roles); err != nil {
				return err
			}
			if !sameStrings(m.Roles, in.Roles) {
				m.Roles = in.Roles
				ev("MemberRolesChanged", map[string]any{"roles": in.Roles})
			}
		}
		if in.Active != nil && *in.Active != m.Active {
			if !*in.Active {
				if err := checkDeactivate(sess, org, m); err != nil {
					return err
				}
				ev("MemberDeactivated", map[string]any{})
			} else {
				ev("MemberReactivated", map[string]any{})
			}
			m.Active = *in.Active
		}
		if err := a.Store.UpdateMember(ctx, tx, m); err != nil {
			return err
		}
		if in.TeamID != nil {
			teams, err := a.Store.ListTeams(ctx, tx)
			if err != nil {
				return err
			}
			byID := teamsByID(teams)
			current, err := a.Store.TeamsOfMember(ctx, tx, m.ID)
			if err != nil {
				return err
			}
			if *in.TeamID != "" {
				if _, ok := byID[*in.TeamID]; !ok {
					return Bad("err.team_missing")
				}
			}
			if err := checkMemberTeamChange(m, byID, current, *in.TeamID, false); err != nil {
				return err
			}
			if !(len(current) == 1 && current[0] == *in.TeamID) && !(len(current) == 0 && *in.TeamID == "") {
				if err := a.Store.SetMemberTeam(ctx, tx, sess.OrgID, m.ID, *in.TeamID); err != nil {
					return err
				}
				if *in.TeamID == "" {
					ev("MemberTeamCleared", map[string]any{})
				} else {
					ev("MemberTeamChanged", map[string]any{"team_id": *in.TeamID, "team_name": byID[*in.TeamID].Name, "mode": "move"})
				}
			}
		}
		if in.Active != nil && !*in.Active {
			if err := a.deactivateMemberEffects(ctx, tx, sess, m); err != nil {
				return err
			}
		}
		return a.insertEvents(ctx, tx, sess, events)
	})
	if err != nil {
		return nil, err
	}
	ms, err := a.OrgMembers(ctx, sess)
	if err != nil {
		return nil, err
	}
	for i := range ms {
		if ms[i].ID == id {
			return &ms[i], nil
		}
	}
	return nil, NotFound("err.member_missing")
}

// checkRoles 确认每个角色都存在。
func (a *App) checkRoles(ctx context.Context, tx pgx.Tx, roles []string) error {
	known, err := a.Store.ListRoles(ctx, tx)
	if err != nil {
		return err
	}
	for _, r := range roles {
		found := false
		for _, k := range known {
			if k.Name == r {
				found = true
				break
			}
		}
		if !found {
			return Bad("err.role_unknown", r)
		}
	}
	return nil
}

// checkDeactivate 停用成员的两条硬规则：组织负责人不能被停用，自己不能停用自己。
func checkDeactivate(sess *Session, org *domain.Organization, m *domain.Member) error {
	if org.OwnerMemberID == m.ID {
		return Bad("err.owner_deactivate")
	}
	if sess.MemberID == m.ID {
		return Bad("err.self_deactivate")
	}
	return nil
}

// checkMemberTeamChange 判断能否手工改这个成员的团队归属（ADR 0017）：来自IM 集成的成员，
// 他在同步团队里的归属由同步决定——不能手工把他挪出同步团队（move 时他正属于某个同步团队），
// 也不能手工把他加进同步团队（目标团队是同步来的）。手工成员不受限。target 为空表示移出全部团队。
func checkMemberTeamChange(m *domain.Member, byID map[string]*domain.Team, current []string, target string, add bool) error {
	if m.Source == domain.SourceManual {
		return nil
	}
	if target != "" {
		if t := byID[target]; t != nil && t.Source != domain.SourceManual {
			return Bad("err.member_synced_team", SourceText(m.Source))
		}
	}
	if !add {
		for _, id := range current {
			if t := byID[id]; t != nil && t.Source != domain.SourceManual && id != target {
				return Bad("err.member_synced_team", SourceText(m.Source))
			}
		}
	}
	return nil
}

// ---------- 团队树的小工具 ----------

func teamsByID(teams []*domain.Team) map[string]*domain.Team {
	out := make(map[string]*domain.Team, len(teams))
	for _, t := range teams {
		out[t.ID] = t
	}
	return out
}

func teamParents(teams []*domain.Team) map[string]string {
	out := make(map[string]string, len(teams))
	for _, t := range teams {
		out[t.ID] = t.ParentID
	}
	return out
}

// teamDepth 团队在树上的深度（根为 0）；成环或断链时停在 64 层。
func teamDepth(parents map[string]string, id string) int {
	d := 0
	for cur := parents[id]; cur != "" && d < 64; cur = parents[cur] {
		d++
	}
	return d
}

// primaryTeam 多团队成员的主团队：树上最深的那个，深度相同取 team_id 最小（与 OrgIndex 的成本归口一致）。
func primaryTeam(parents map[string]string, ids []string) string {
	best, bestDepth := "", -1
	for _, t := range ids {
		if d := teamDepth(parents, t); d > bestDepth || (d == bestDepth && t < best) {
			best, bestDepth = t, d
		}
	}
	return best
}

// inSubtree 判断 id 是否在 root 的子树里（含 root 自己）。
func inSubtree(parents map[string]string, id, root string) bool {
	for cur, i := id, 0; cur != "" && i < 64; cur, i = parents[cur], i+1 {
		if cur == root {
			return true
		}
	}
	return false
}

// teamPath 是团队的完整路径，如「产品事业部 / 研发组」。
func teamPath(byID map[string]*domain.Team, id string) string {
	var parts []string
	for cur, i := id, 0; cur != "" && i < 64; i++ {
		t := byID[cur]
		if t == nil {
			break
		}
		parts = append([]string{t.Name}, parts...)
		cur = t.ParentID
	}
	return strings.Join(parts, " / ")
}

// deactivateMemberEffects 停用成员的连带效果：吊销 Agent、任务回待领取。
func (a *App) deactivateMemberEffects(ctx context.Context, tx pgx.Tx, sess *Session, m *domain.Member) error {
	agents, err := a.Store.ListAgents(ctx, tx)
	if err != nil {
		return err
	}
	ids := []string{m.ID}
	for _, ag := range agents {
		if ag.OwnerMemberID == m.ID {
			if err := a.Store.RevokeAgent(ctx, tx, ag.ID); err != nil {
				return err
			}
			if err := a.endMandatesOfAgent(ctx, tx, sess, ag.ID, "mandate.stale.agent", nil); err != nil {
				return err
			}
			ids = append(ids, ag.ID)
		}
	}
	tasks, err := a.Store.ListTasks(ctx, tx, store.TaskFilter{Executors: ids})
	if err != nil {
		return err
	}
	var events []domain.Event
	for _, t := range tasks {
		tt, err := a.Store.TaskType(ctx, tx, t.TypeName, t.TypeVersion)
		if err != nil {
			continue
		}
		if st := tt.Workflow.State(t.State); st == nil || st.Label.IsTerminal() {
			continue
		}
		if run, _ := a.Store.ActiveRun(ctx, tx, t.ID); run != nil {
			now := time.Now()
			run.EndedAt, run.Outcome = &now, domain.RunCancelled
			if err := a.Store.UpdateRun(ctx, tx, run, 0); err != nil {
				return err
			}
			events = append(events, domain.Event{Type: "RunEnded", TaskID: t.ID, ActorID: sess.Actor.ID, At: now, Data: map[string]any{"run_id": run.ID, "outcome": "cancelled"}})
		}
		t.AssigneeID = ""
		if err := a.Store.UpdateTask(ctx, tx, t); err != nil {
			return err
		}
		events = append(events, domain.Event{Type: "TaskSentToBacklog", TaskID: t.ID, ActorID: sess.Actor.ID, At: time.Now()})
	}
	return a.insertEvents(ctx, tx, sess, events)
}

// MakeOwner 把组织负责人转给某成员；只有当前负责人可以。
func (a *App) MakeOwner(ctx context.Context, sess *Session, memberID string) error {
	if !sess.IsOwner {
		return Forbidden("err.owner_required")
	}
	if err := refuseDryRun(sess); err != nil {
		return err
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		m, err := a.Store.MemberByID(ctx, tx, memberID)
		if err != nil {
			return err
		}
		if !m.Active {
			return Forbidden("err.member_inactive")
		}
		return a.Store.SetOrganizationOwner(ctx, tx, sess.OrgID, memberID)
	})
}

// ---------- 邀请 ----------

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// InvitationView 带链接的邀请。
type InvitationView struct {
	*domain.Invitation
	URL string `json:"url"`
	// Member 是这次邀请预建的待激活成员（邮箱已有可登录账号、或成员早已存在时为 nil）；只给调用方记动态用。
	Member *domain.Member `json:"-"`
}

// Invite 创建邀请并返回链接（一期不发邮件）。新邮箱同时预建一个待激活成员（带角色与团队），与导入、同步一致。
func (a *App) Invite(ctx context.Context, sess *Session, email, name string, roles []string, teamID *string) (*InvitationView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var view *InvitationView
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		view, err = a.inviteTx(ctx, tx, sess, email, name, roles, teamID)
		return
	})
	return view, err
}

// inviteTx 在事务里创建邀请并记动态（ADR 0017）。手工邀请与 CSV 导入共用：
//   - 邮箱已是正常成员：拒绝（err.already_member）；
//   - 邮箱是待激活成员：重发邀请链接，姓名 / 角色缺省沿用成员的，团队不改；
//   - 邮箱已有可登录账号（别的组织的人）：只发邀请、不预建成员，接受时用自己的密码验证；
//   - 其余（新邮箱，或只有无密码占位账号）：建一个待激活的手工成员（带角色与团队）并发邀请。
func (a *App) inviteTx(ctx context.Context, tx pgx.Tx, sess *Session, email, name string, roles []string, teamID *string) (*InvitationView, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailRe.MatchString(email) {
		return nil, Bad("err.email_required")
	}
	if roles == nil {
		roles = []string{}
	}
	name = strings.TrimSpace(name)
	var team *domain.Team
	if teamID != nil && *teamID != "" {
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return nil, err
		}
		team = teamsByID(teams)[*teamID]
		if team == nil {
			return nil, Bad("err.team_missing")
		}
		if team.Inactive {
			return nil, Bad("err.team_inactive", team.Name)
		}
	}
	inv := &domain.Invitation{OrgID: sess.OrgID, Email: email, Name: name, Roles: roles, InvitedBy: sess.MemberID, ExpiresAt: time.Now().Add(14 * 24 * time.Hour)}
	if team != nil {
		inv.TeamID = team.ID
	}
	var created *domain.Member
	acc, hash, err := a.Store.AccountByEmail(ctx, tx, email)
	switch {
	case err == nil:
		if m, err := a.Store.MemberByAccount(ctx, tx, sess.OrgID, acc.ID); err == nil {
			if m.DerivedStatus() != domain.MemberPendingActivation {
				return nil, Bad("err.already_member")
			}
			if inv.Name == "" {
				inv.Name = m.Name
			}
			if len(inv.Roles) == 0 {
				inv.Roles = m.Roles
			}
			inv.MemberID = m.ID
		} else if strings.HasPrefix(hash, "!") {
			// 无密码的占位账号（以前的邀请被作废过）：照新邮箱处理
			created, err = a.createPendingMember(ctx, tx, sess, acc, inv, team)
			if err != nil {
				return nil, err
			}
		}
		// 别的组织的可登录账号：只发邀请
	case err == store.ErrNotFound:
		// 没有可用密码的账号：要通过邀请链接设密码才能登录（同 IM 集成同步，ADR 0017）
		acc, err = a.Store.CreateAccount(ctx, tx, email, "!"+store.NewID("nopw"), name)
		if err != nil {
			return nil, err
		}
		created, err = a.createPendingMember(ctx, tx, sess, acc, inv, team)
		if err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	token, err := a.Store.CreateInvitation(ctx, tx, inv)
	if err != nil {
		return nil, err
	}
	view := &InvitationView{Invitation: inv, URL: a.PublicURL + "/invite/" + token + "/", Member: created}
	data := map[string]any{"email": email}
	if inv.MemberID != "" {
		data["member_id"] = inv.MemberID
	}
	return view, a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "MemberInvited", ActorID: sess.Actor.ID, At: time.Now(), Data: data}})
}

// createPendingMember 给邀请预建一个待激活的手工成员（带角色与团队），并把邀请指向它。
func (a *App) createPendingMember(ctx context.Context, tx pgx.Tx, sess *Session, acc *domain.Account, inv *domain.Invitation, team *domain.Team) (*domain.Member, error) {
	name := inv.Name
	if name == "" {
		name = strings.Split(inv.Email, "@")[0]
	}
	m := &domain.Member{OrgID: sess.OrgID, AccountID: acc.ID, Name: name, Roles: inv.Roles, Active: true, Source: domain.SourceManual, Status: domain.MemberPendingActivation}
	if err := a.Store.CreateMemberFull(ctx, tx, m); err != nil {
		return nil, err
	}
	if team != nil {
		if err := a.Store.AddTeamMember(ctx, tx, sess.OrgID, team.ID, m.ID); err != nil {
			return nil, err
		}
	}
	inv.MemberID = m.ID
	return m, nil
}

// ListInvitations 列出邀请（链接只在创建时返回）。
func (a *App) ListInvitations(ctx context.Context, sess *Session) ([]InvitationView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	out := []InvitationView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		invs, err := a.Store.ListInvitations(ctx, tx)
		if err != nil {
			return err
		}
		for _, inv := range invs {
			out = append(out, InvitationView{Invitation: inv})
		}
		return nil
	})
	return out, err
}

// DeleteInvitation 作废邀请。邀请预建的手工待激活成员若从未激活、也没有别的有效邀请了，一并删掉（同步来的成员不动，由同步管）。
func (a *App) DeleteInvitation(ctx context.Context, sess *Session, id string) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		inv, err := a.Store.InvitationByID(ctx, tx, id)
		if err == store.ErrNotFound {
			return NotFound("err.invite_missing")
		}
		if err != nil {
			return err
		}
		if err := a.Store.DeleteInvitation(ctx, tx, id); err != nil {
			return err
		}
		data := map[string]any{"email": inv.Email}
		if inv.MemberID != "" && inv.AcceptedAt == nil {
			m, err := a.Store.MemberByID(ctx, tx, inv.MemberID)
			if err == nil && m.DerivedStatus() == domain.MemberPendingActivation && m.Source == domain.SourceManual {
				n, err := a.Store.CountOpenInvitationsForMember(ctx, tx, m.ID)
				if err != nil {
					return err
				}
				if n == 0 {
					if err := a.Store.DeletePendingMember(ctx, tx, m.ID); err != nil {
						return err
					}
					data["member_id"], data["name"] = m.ID, m.Name
				}
			}
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "InvitationRevoked", ActorID: sess.Actor.ID, At: time.Now(), Data: data}})
	})
}

// InvitationInfo 是公开的邀请信息。
type InvitationInfo struct {
	OrganizationName string `json:"organization_name"`
	Email            string `json:"email"`
	Name             string `json:"name"`
	Expired          bool   `json:"expired"`
	Accepted         bool   `json:"accepted"`
	HasAccount       bool   `json:"has_account"`
}

// LookupInvitation 按令牌查邀请（公开）。
func (a *App) LookupInvitation(ctx context.Context, token string) (*InvitationInfo, error) {
	inv, err := a.Store.InvitationByToken(ctx, a.Store.Pool, token)
	if err != nil {
		return nil, NotFound("err.invite_missing")
	}
	org, err := a.Store.OrganizationByID(ctx, a.Store.Pool, inv.OrgID)
	if err != nil {
		return nil, err
	}
	info := &InvitationInfo{OrganizationName: org.Name, Email: inv.Email, Name: inv.Name, Expired: time.Now().After(inv.ExpiresAt), Accepted: inv.AcceptedAt != nil}
	if _, _, err := a.Store.AccountByEmail(ctx, a.Store.Pool, inv.Email); err == nil {
		info.HasAccount = true
	}
	return info, nil
}

// AcceptInvitation 接受邀请：创建或验证账号，加入组织，返回登录令牌。
func (a *App) AcceptInvitation(ctx context.Context, token, name, password string, locale i18n.Locale) (string, *Session, error) {
	inv, err := a.Store.InvitationByToken(ctx, a.Store.Pool, token)
	if err != nil {
		return "", nil, NotFound("err.invite_missing")
	}
	if inv.AcceptedAt != nil {
		return "", nil, Bad("err.invite_accepted")
	}
	if time.Now().After(inv.ExpiresAt) {
		return "", nil, Bad("err.invite_missing")
	}
	if strings.TrimSpace(name) == "" {
		name = inv.Name
	}
	if strings.TrimSpace(name) == "" {
		name = strings.Split(inv.Email, "@")[0]
	}
	acc, hash, err := a.Store.AccountByEmail(ctx, a.Store.Pool, inv.Email)
	// 从外部目录同步进来的待激活成员（ADR 0017）：账号已存在但没有可用密码，接受邀请就是设密码并激活
	if err == nil {
		var pending *domain.Member
		if e := a.Store.WithOrg(ctx, inv.OrgID, func(tx pgx.Tx) error {
			m, err := a.Store.MemberByAccount(ctx, tx, inv.OrgID, acc.ID)
			if err == nil && m.DerivedStatus() == domain.MemberPendingActivation {
				pending = m
			}
			return nil
		}); e != nil {
			return "", nil, e
		}
		if pending != nil {
			return a.activatePendingMember(ctx, inv, acc, pending, name, password, locale)
		}
	}
	switch {
	case err == store.ErrNotFound:
		if len(password) < 8 {
			return "", nil, Bad("err.password_short")
		}
		h, err := HashPassword(password)
		if err != nil {
			return "", nil, err
		}
		acc, err = a.Store.CreateAccount(ctx, a.Store.Pool, inv.Email, h, name)
		if err != nil {
			return "", nil, err
		}
		if locale != "" {
			ls := string(locale)
			_ = a.Store.UpdateAccount(ctx, a.Store.Pool, acc.ID, nil, &ls, nil)
		}
	case err != nil:
		return "", nil, err
	default:
		if err := checkPassword(hash, password); err != nil {
			return "", nil, Bad("err.bad_credentials")
		}
	}
	err = a.Store.WithOrg(ctx, inv.OrgID, func(tx pgx.Tx) error {
		if _, err := a.Store.MemberByAccount(ctx, tx, inv.OrgID, acc.ID); err == nil {
			return Bad("err.already_member")
		}
		m, err := a.Store.CreateMember(ctx, tx, inv.OrgID, acc.ID, name, inv.Roles)
		if err != nil {
			return err
		}
		if err := a.Store.MarkInvitationAccepted(ctx, tx, inv.ID); err != nil {
			return err
		}
		// 平台后台建组织时指定的负责人：组织还没有负责人，第一个接受邀请的成员就是
		if org, err := a.Store.OrganizationByID(ctx, tx, inv.OrgID); err == nil && org.OwnerMemberID == "" {
			if err := a.Store.SetOrganizationOwner(ctx, tx, inv.OrgID, m.ID); err != nil {
				return err
			}
		}
		return a.Store.InsertEvents(ctx, tx, inv.OrgID, []domain.Event{{Type: "MemberJoined", ActorID: m.ID, At: time.Now(), Data: map[string]any{"member_id": m.ID}}})
	})
	if err != nil {
		return "", nil, wrapErr(err)
	}
	tok := store.NewID("ses") + store.NewID("")[1:]
	if err := a.Store.CreateSession(ctx, a.Store.Pool, tok, acc.ID, inv.OrgID, sessionTTL); err != nil {
		return "", nil, err
	}
	sess, err := a.SessionFromToken(ctx, tok)
	return tok, sess, err
}

// activatePendingMember 给待激活成员设密码、激活并登录。
func (a *App) activatePendingMember(ctx context.Context, inv *domain.Invitation, acc *domain.Account, m *domain.Member, name, password string, locale i18n.Locale) (string, *Session, error) {
	if len(password) < 8 {
		return "", nil, Bad("err.password_short")
	}
	h, err := HashPassword(password)
	if err != nil {
		return "", nil, err
	}
	var nm *string
	if n := strings.TrimSpace(name); n != "" && n != inv.Email {
		nm = &n
	}
	var ls *string
	if locale != "" {
		s := string(locale)
		ls = &s
	}
	if err := a.Store.UpdateAccount(ctx, a.Store.Pool, acc.ID, nm, ls, &h); err != nil {
		return "", nil, err
	}
	err = a.Store.WithOrg(ctx, inv.OrgID, func(tx pgx.Tx) error {
		m.Status, m.Active = domain.MemberActive, true
		if nm != nil {
			m.Name = *nm
		}
		if err := a.Store.UpdateMember(ctx, tx, m); err != nil {
			return err
		}
		if err := a.Store.MarkInvitationAccepted(ctx, tx, inv.ID); err != nil {
			return err
		}
		return a.Store.InsertEvents(ctx, tx, inv.OrgID, []domain.Event{{Type: "MemberActivated", ActorID: m.ID, At: time.Now(), Data: map[string]any{"member_id": m.ID, "name": m.Name}}})
	})
	if err != nil {
		return "", nil, wrapErr(err)
	}
	tok := store.NewID("ses") + store.NewID("")[1:]
	if err := a.Store.CreateSession(ctx, a.Store.Pool, tok, acc.ID, inv.OrgID, sessionTTL); err != nil {
		return "", nil, err
	}
	sess, err := a.SessionFromToken(ctx, tok)
	return tok, sess, err
}

// ---------- 角色 ----------

// ListRoles 列出角色（登录用户皆可读，界面需要角色名）。
func (a *App) ListRoles(ctx context.Context, sess *Session) ([]*domain.Role, error) {
	out := []*domain.Role{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		rs, err := a.Store.ListRoles(ctx, tx)
		if err != nil {
			return err
		}
		if rs != nil {
			out = rs
		}
		return nil
	})
	return out, err
}

var roleNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)

// SaveRole 新建或修改角色；内置角色只能改名称。
func (a *App) SaveRole(ctx context.Context, sess *Session, name string, title i18n.Text, permissions []string) (*domain.Role, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	if err := refuseDryRun(sess); err != nil {
		return nil, err
	}
	if !roleNameRe.MatchString(name) {
		return nil, Bad("err.slug")
	}
	for _, p := range permissions {
		if !contains(domain.Permissions, p) {
			return nil, Bad("err.role_unknown", p)
		}
	}
	var role *domain.Role
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		rs, err := a.Store.ListRoles(ctx, tx)
		if err != nil {
			return err
		}
		role = &domain.Role{Name: name, Title: title, Permissions: permissions}
		for _, r := range rs {
			if r.Name == name {
				role.BuiltIn = r.BuiltIn
				if r.BuiltIn {
					role.Permissions = r.Permissions
				}
			}
		}
		if role.Title.IsZero() {
			role.Title = i18n.Text{i18n.Default: name}
		}
		if role.Permissions == nil {
			role.Permissions = []string{}
		}
		return a.Store.UpsertRole(ctx, tx, sess.OrgID, role)
	})
	return role, err
}

// DeleteRole 删除角色。
func (a *App) DeleteRole(ctx context.Context, sess *Session, name string) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	if err := refuseDryRun(sess); err != nil {
		return err
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		rs, err := a.Store.ListRoles(ctx, tx)
		if err != nil {
			return err
		}
		for _, r := range rs {
			if r.Name == name && r.BuiltIn {
				return Bad("err.role_builtin")
			}
		}
		n, err := a.Store.CountMembersWithRole(ctx, tx, name)
		if err != nil {
			return err
		}
		if n > 0 {
			return Bad("err.role_in_use")
		}
		// 角色没了，它的工作台布局也没有意义
		if err := a.Store.DeleteRoleLayout(ctx, tx, name); err != nil {
			return err
		}
		return a.Store.DeleteRole(ctx, tx, name)
	})
}

// ---------- 团队 ----------

// TeamView 是带成员的团队。
type TeamView struct {
	*domain.Team
	MemberIDs []string `json:"member_ids"`
	// MemberCount 是直属的正常成员数；SubtreeMemberCount 是它和全部下级团队里不重复的正常成员数（停用的不算）。
	MemberCount        int `json:"member_count"`
	SubtreeMemberCount int `json:"subtree_member_count"`
}

func teamByID(ctx context.Context, tx pgx.Tx, a *App, id string) (*domain.Team, error) {
	teams, err := a.Store.ListTeams(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, t := range teams {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, store.ErrNotFound
}

// ListTeams 列出团队。
func (a *App) ListTeams(ctx context.Context, sess *Session) ([]TeamView, error) {
	out := []TeamView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return err
		}
		members, err := a.Store.TeamMembers(ctx, tx)
		if err != nil {
			return err
		}
		ms, err := a.Store.ListMembers(ctx, tx)
		if err != nil {
			return err
		}
		active := map[string]bool{}
		for _, m := range ms {
			active[m.ID] = m.Active
		}
		for _, t := range teams {
			ids := members[t.ID]
			if ids == nil {
				ids = []string{}
			}
			v := TeamView{Team: t, MemberIDs: ids}
			for _, id := range ids {
				if active[id] {
					v.MemberCount++
				}
			}
			seen := map[string]bool{}
			for _, sub := range domain.Subtree(teams, t.ID) {
				for _, id := range members[sub] {
					if active[id] && !seen[id] {
						seen[id] = true
						v.SubtreeMemberCount++
					}
				}
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

// TeamPatch 是团队可改字段。
type TeamPatch struct {
	Name      *string  `json:"name"`
	ParentID  *string  `json:"parent_id"`
	LeadID    *string  `json:"lead_id"`
	MemberIDs []string `json:"member_ids"`
	// IsBoundary 把团队标记为共享边界（ADR 0013）。改它会立刻改变一批人的可见范围。
	IsBoundary *bool `json:"is_boundary"`
	// Active 传 false 停用团队（只停用不删除，ADR 0017），传 true 恢复。
	Active *bool `json:"active"`
}

// SaveTeam 新建（id 空）或修改团队。修改时：挪动上级要过成环检查；来自IM 集成的团队不能改名；
// 停用要先清空成员与下级团队。每处改动各记一条动态。
func (a *App) SaveTeam(ctx context.Context, sess *Session, id string, in TeamPatch) (*TeamView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return err
		}
		byID, parents := teamsByID(teams), teamParents(teams)
		var t *domain.Team
		var events []domain.Event
		ev := func(kind string, data map[string]any) {
			data["team_id"], data["name"] = t.ID, t.Name
			events = append(events, domain.Event{Type: kind, ActorID: sess.Actor.ID, At: time.Now(), Data: data})
		}
		if id == "" {
			if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
				return Bad("err.title_required")
			}
			t = &domain.Team{OrgID: sess.OrgID, Name: strings.TrimSpace(*in.Name)}
			if in.ParentID != nil && *in.ParentID != "" {
				if _, ok := byID[*in.ParentID]; !ok {
					return Bad("err.team_missing")
				}
				t.ParentID = *in.ParentID
			}
			if in.LeadID != nil {
				t.LeadMemberID = *in.LeadID
			}
			if in.IsBoundary != nil {
				t.IsBoundary = *in.IsBoundary
			}
			if err := a.Store.CreateTeam(ctx, tx, t); err != nil {
				return err
			}
			id = t.ID
			ev("TeamCreated", map[string]any{"is_boundary": t.IsBoundary})
			if t.IsBoundary {
				ev("TeamBoundaryChanged", map[string]any{"is_boundary": true})
			}
		} else {
			cur, ok := byID[id]
			if !ok {
				return Bad("err.team_missing")
			}
			t = cur
			if in.Name != nil && strings.TrimSpace(*in.Name) != "" && strings.TrimSpace(*in.Name) != t.Name {
				if t.Source != domain.SourceManual {
					return Bad("err.team_synced_name", SourceText(t.Source))
				}
				t.Name = strings.TrimSpace(*in.Name)
				ev("TeamUpdated", map[string]any{})
			}
			if in.ParentID != nil && *in.ParentID != t.ParentID {
				np := *in.ParentID
				if np != "" {
					if _, ok := byID[np]; !ok {
						return Bad("err.team_missing")
					}
					if inSubtree(parents, np, id) {
						return Bad("err.team_cycle")
					}
				}
				t.ParentID = np
				parents[id] = np
				data := map[string]any{"parent_id": np}
				if np != "" {
					data["parent_name"] = byID[np].Name
				}
				ev("TeamMoved", data)
			}
			if in.LeadID != nil && *in.LeadID != t.LeadMemberID {
				t.LeadMemberID = *in.LeadID
				ev("TeamUpdated", map[string]any{})
			}
			// 共享边界改动会立刻改变一批人的可见范围，单独记一条动态说清楚（ADR 0013）
			if in.IsBoundary != nil && *in.IsBoundary != t.IsBoundary {
				t.IsBoundary = *in.IsBoundary
				ev("TeamBoundaryChanged", map[string]any{"is_boundary": t.IsBoundary})
			}
			if in.Active != nil && *in.Active == t.Inactive {
				if !*in.Active {
					blocked, err := a.teamHasActiveContent(ctx, tx, teams, id)
					if err != nil {
						return err
					}
					if blocked {
						return Bad("err.team_deactivate_blocked")
					}
					t.Inactive = true
					ev("TeamDeactivated", map[string]any{"manual": true})
				} else {
					t.Inactive = false
					ev("TeamReactivated", map[string]any{})
				}
			}
			if err := a.Store.UpdateTeam(ctx, tx, t); err != nil {
				return err
			}
			if len(events) == 0 && in.MemberIDs == nil {
				ev("TeamUpdated", map[string]any{})
			}
		}
		if in.MemberIDs != nil {
			for _, mid := range in.MemberIDs {
				if err := a.Store.SetMemberTeam(ctx, tx, sess.OrgID, mid, id); err != nil {
					return err
				}
			}
			members, _ := a.Store.TeamMembers(ctx, tx)
			for _, mid := range members[id] {
				if !contains(in.MemberIDs, mid) {
					if err := a.Store.SetMemberTeam(ctx, tx, sess.OrgID, mid, ""); err != nil {
						return err
					}
				}
			}
			ev("TeamUpdated", map[string]any{"member_ids": in.MemberIDs})
		}
		return a.insertEvents(ctx, tx, sess, events)
	})
	if err != nil {
		return nil, err
	}
	teams, err := a.ListTeams(ctx, sess)
	if err != nil {
		return nil, err
	}
	for i := range teams {
		if teams[i].ID == id {
			return &teams[i], nil
		}
	}
	return nil, NotFound("err.team_missing")
}

// teamHasActiveContent 判断团队里是否还有正常成员或未停用的下级团队。
func (a *App) teamHasActiveContent(ctx context.Context, tx pgx.Tx, teams []*domain.Team, id string) (bool, error) {
	for _, t := range teams {
		if t.ParentID == id && !t.Inactive {
			return true, nil
		}
	}
	members, err := a.Store.TeamMembers(ctx, tx)
	if err != nil {
		return false, err
	}
	if len(members[id]) == 0 {
		return false, nil
	}
	ms, err := a.Store.ListMembers(ctx, tx)
	if err != nil {
		return false, err
	}
	for _, m := range ms {
		if m.Active && contains(members[id], m.ID) {
			return true, nil
		}
	}
	return false, nil
}

// DeleteTeam 删除团队：只有没有成员、没有下级团队的手工团队才能删；来自IM 集成的团队只能停用（ADR 0017）。
// TeamImpact 是删除一个团队会波及什么：确认对话框先给人看，DeleteTeam 用同一段算法记进动态。
type TeamImpact struct {
	Members  int `json:"members"`   // 直接成员，会离开这个团队
	OnlyTeam int `json:"only_team"` // 其中从此不属于任何团队的人
	SubTeams int `json:"sub_teams"` // 直接下级团队，会上移到新上级
	Goals    int `json:"goals"`     // 归口到它的目标，改成不归口
	Sprints  int `json:"sprints"`   // 属于它的迭代，改成按组织
	// NewParentID / NewParent 是下级上移后的新上级；空 = 顶层
	NewParentID string `json:"new_parent_id"`
	NewParent   string `json:"new_parent"`
	IsBoundary  bool   `json:"is_boundary"`
}

func (a *App) teamImpact(ctx context.Context, tx pgx.Tx, teams []*domain.Team, t *domain.Team) (*TeamImpact, error) {
	members, err := a.Store.TeamMembers(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := &TeamImpact{Members: len(members[t.ID]), NewParentID: t.ParentID, IsBoundary: t.IsBoundary}
	if p := teamsByID(teams)[t.ParentID]; p != nil {
		out.NewParent = p.Name
	}
	elsewhere := map[string]bool{}
	for tid, ms := range members {
		if tid == t.ID {
			continue
		}
		for _, m := range ms {
			elsewhere[m] = true
		}
	}
	for _, m := range members[t.ID] {
		if !elsewhere[m] {
			out.OnlyTeam++
		}
	}
	for _, x := range teams {
		if x.ParentID == t.ID {
			out.SubTeams++
		}
	}
	out.Goals, out.Sprints, err = a.Store.TeamRefCounts(ctx, tx, t.ID)
	return out, err
}

// TeamDeleteImpact 删除前看影响范围（GET /org/teams/{id}/impact）。
func (a *App) TeamDeleteImpact(ctx context.Context, sess *Session, id string) (*TeamImpact, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var out *TeamImpact
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return err
		}
		t := teamsByID(teams)[id]
		if t == nil {
			return Bad("err.team_missing")
		}
		out, err = a.teamImpact(ctx, tx, teams, t)
		return err
	})
	return out, err
}

// DeleteTeam 删除手工建的团队，有成员、有下级也能删：成员离开它，下级上移到它的上级，归口到它的目标与迭代改成不归口。
// 界面上删除前要经过带影响范围的二次确认。来自IM 集成的团队只能停用（下次同步还会对上）。
func (a *App) DeleteTeam(ctx context.Context, sess *Session, id string) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return err
		}
		t := teamsByID(teams)[id]
		if t == nil {
			return Bad("err.team_missing")
		}
		if t.Source != domain.SourceManual {
			return Bad("err.team_delete_synced", SourceText(t.Source))
		}
		impact, err := a.teamImpact(ctx, tx, teams, t)
		if err != nil {
			return err
		}
		if err := a.Store.DeleteTeam(ctx, tx, id, t.ParentID); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "TeamDeleted", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{
			"team_id": id, "name": t.Name, "members": impact.Members, "sub_teams": impact.SubTeams, "goals": impact.Goals, "sprints": impact.Sprints, "new_parent_id": t.ParentID,
		}}})
	})
}

// VisibilityTeam 是预览里的一个团队。
type VisibilityTeam struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Depth      int    `json:"depth"`
	IsBoundary bool   `json:"is_boundary"`
	Mine       bool   `json:"mine"`    // 他自己所属的团队
	Work       bool   `json:"work"`    // 能看到这个团队的协作数据
	Finance    bool   `json:"finance"` // 能看到这个团队的财务数据
}

// VisibilityPreview 是「这样配之后某某能看到什么」。
type VisibilityPreview struct {
	MemberID              string            `json:"member_id"`
	MemberName            string            `json:"member_name"`
	TeamIDs               []string          `json:"team_ids"`
	CollaborationPolicy   domain.Visibility `json:"collaboration_visibility"`
	CollaborationTitle    string            `json:"collaboration_title"`
	FinancePolicy         domain.Visibility `json:"finance_visibility"`
	FinanceTitle          string            `json:"finance_title"`
	SeesAllWork           bool              `json:"sees_all_work"`
	SeesAllCost           bool              `json:"sees_all_cost"`
	CrossBoundaryByRole   bool              `json:"cross_boundary_by_role"`
	Boundaries            []string          `json:"boundaries"` // 他所在团队向上最近的共享边界
	Teams                 []VisibilityTeam  `json:"teams"`
	VisibleTeamIDs        []string          `json:"visible_team_ids"`
	FinanceVisibleTeamIDs []string          `json:"finance_visible_team_ids"`
}

// PreviewVisibility 算某个成员在当前策略下实际能看到哪些团队、能不能看财务。
// 组织设置页用它做「这样配之后某某能看到什么」的预览。
func (a *App) PreviewVisibility(ctx context.Context, sess *Session, memberID string) (*VisibilityPreview, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	loc := sess.Loc()
	out := &VisibilityPreview{Teams: []VisibilityTeam{}, TeamIDs: []string{}, Boundaries: []string{}, VisibleTeamIDs: []string{}, FinanceVisibleTeamIDs: []string{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		m, err := a.Store.MemberByID(ctx, tx, memberID)
		if err != nil {
			return err
		}
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return err
		}
		mine, err := a.Store.TeamsOfMember(ctx, tx, m.ID)
		if err != nil {
			return err
		}
		roles, err := a.Store.ListRoles(ctx, tx)
		if err != nil {
			return err
		}
		perms := map[string]bool{}
		for _, r := range roles {
			if contains(m.Roles, r.Name) {
				for _, p := range r.Permissions {
					perms[p] = true
				}
			}
		}
		isOwner := org.OwnerMemberID == m.ID
		out.MemberID, out.MemberName, out.TeamIDs = m.ID, m.Name, mine
		out.CollaborationPolicy, out.FinancePolicy = org.Collaboration(), org.Finance()
		out.CollaborationTitle = i18n.Tr(loc, "visibility."+string(out.CollaborationPolicy))
		out.FinanceTitle = i18n.Tr(loc, "visibility."+string(out.FinancePolicy))
		work, workAll := domain.VisibleTeams(teams, mine, out.CollaborationPolicy)
		fin, finAll := domain.VisibleTeams(teams, mine, out.FinancePolicy)
		out.SeesAllWork = workAll || isOwner || perms["view_all_work"]
		out.SeesAllCost = finAll || isOwner || perms["view_all_cost"]
		out.CrossBoundaryByRole = isOwner || perms["view_all_work"] || perms["view_all_cost"]
		for _, id := range mine {
			if b := domain.NearestBoundary(teams, id); b != "" {
				out.Boundaries = append(out.Boundaries, b)
			}
		}
		depth := map[string]int{}
		for _, t := range teams {
			d, cur := 0, t.ParentID
			for i := 0; i < 64 && cur != ""; i++ {
				d++
				cur = parentOf(teams, cur)
			}
			depth[t.ID] = d
		}
		for _, t := range teams {
			v := VisibilityTeam{ID: t.ID, Name: t.Name, Depth: depth[t.ID], IsBoundary: t.IsBoundary, Mine: contains(mine, t.ID)}
			v.Work = out.SeesAllWork || contains(work, t.ID)
			v.Finance = out.SeesAllCost || contains(fin, t.ID)
			if v.Work {
				out.VisibleTeamIDs = append(out.VisibleTeamIDs, t.ID)
			}
			if v.Finance {
				out.FinanceVisibleTeamIDs = append(out.FinanceVisibleTeamIDs, t.ID)
			}
			out.Teams = append(out.Teams, v)
		}
		return nil
	})
	return out, err
}

func parentOf(teams []*domain.Team, id string) string {
	for _, t := range teams {
		if t.ID == id {
			return t.ParentID
		}
	}
	return ""
}

// ---------- 能力标签 ----------

var capNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)

// SaveCapability 新建或修改能力标签。
func (a *App) SaveCapability(ctx context.Context, sess *Session, name string, title i18n.Text) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	if err := refuseDryRun(sess); err != nil {
		return err
	}
	if !capNameRe.MatchString(name) {
		return Bad("err.slug")
	}
	if title.IsZero() {
		title = i18n.Text{i18n.Default: name}
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error { return a.Store.UpsertCapability(ctx, tx, sess.OrgID, name, title) })
}

// DeleteCapability 删除能力标签。
func (a *App) DeleteCapability(ctx context.Context, sess *Session, name string) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	if err := refuseDryRun(sess); err != nil {
		return err
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error { return a.Store.DeleteCapability(ctx, tx, name) })
}

// ---------- 价格表 ----------

// PricingView 是组织价格表视图。
type PricingView struct {
	Currency      string         `json:"currency"`
	Models        []PriceModel   `json:"models"`
	ExchangeRates []ExchangeRate `json:"exchange_rates"`
}

// PriceModel 是一行价格（含来源）。
type PriceModel struct {
	domain.Price
	Source string `json:"source"` // global | override
}

// ExchangeRate 是一条汇率。
type ExchangeRate struct {
	From string  `json:"from"`
	To   string  `json:"to"`
	Rate float64 `json:"rate"`
}

// Pricing 返回组织可见的价格表。
func (a *App) Pricing(ctx context.Context, sess *Session) (*PricingView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	v := &PricingView{Models: []PriceModel{}, ExchangeRates: []ExchangeRate{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		v.Currency = org.Currency
		rows, err := a.Store.ListPrices(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		byModel := map[string]PriceModel{}
		var order []string
		for _, r := range rows {
			src := "global"
			if r.OrgID != "" {
				src = "override"
			}
			if _, ok := byModel[r.ModelID]; !ok {
				order = append(order, r.ModelID)
			}
			byModel[r.ModelID] = PriceModel{Price: r.Price, Source: src}
		}
		for _, m := range order {
			v.Models = append(v.Models, byModel[m])
		}
		rates, err := a.Store.ListExchangeRates(ctx, tx)
		if err != nil {
			return err
		}
		for _, r := range rates {
			v.ExchangeRates = append(v.ExchangeRates, ExchangeRate{From: r[0].(string), To: r[1].(string), Rate: r[2].(float64)})
		}
		return nil
	})
	return v, err
}

func validPrice(p domain.Price) bool {
	return p.InputPerMillion >= 0 && p.OutputPerMillion >= 0 && p.CacheReadPerMillion >= 0 && p.CacheWritePerMillion >= 0
}

// SetPriceOverride 写组织覆盖价格。
func (a *App) SetPriceOverride(ctx context.Context, sess *Session, p domain.Price) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	if err := refuseDryRun(sess); err != nil {
		return err
	}
	if !validPrice(p) {
		return Bad("err.price_fields")
	}
	if p.Currency == "" {
		p.Currency = "USD"
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error { return a.Store.UpsertPrice(ctx, tx, sess.OrgID, p) })
}

// DeletePriceOverride 删除覆盖。
func (a *App) DeletePriceOverride(ctx context.Context, sess *Session, modelID string) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	if err := refuseDryRun(sess); err != nil {
		return err
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error { return a.Store.DeletePrice(ctx, tx, sess.OrgID, modelID) })
}

// SetExchangeRate 写汇率。
func (a *App) SetExchangeRate(ctx context.Context, sess *Session, from, to string, rate float64) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	if err := refuseDryRun(sess); err != nil {
		return err
	}
	if rate <= 0 || from == "" || to == "" {
		return Bad("err.price_fields")
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		return a.Store.UpsertExchangeRate(ctx, tx, sess.OrgID, strings.ToUpper(from), strings.ToUpper(to), rate)
	})
}
