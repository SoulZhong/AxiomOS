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
	Email   string      `json:"email"`
	TeamID  string      `json:"team_id"`
	IsOwner bool        `json:"is_owner"`
	Locale  i18n.Locale `json:"locale"`
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
		teamOf, _ := a.Store.TeamOfMembers(ctx, tx)
		for _, m := range ms {
			d := MemberDetail{Member: m, TeamID: teamOf[m.ID], IsOwner: org.OwnerMemberID == m.ID, Locale: a.localeOfMember(ctx, tx, sess.OrgID, m.ID)}
			if acc, err := a.Store.AccountByID(ctx, tx, m.AccountID); err == nil {
				d.Email = acc.Email
			}
			out = append(out, d)
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

// UpdateMember 修改成员。停用成员时吊销其 Agent、任务回待领取。
func (a *App) UpdateMember(ctx context.Context, sess *Session, id string, in MemberPatch) (*MemberDetail, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		m, err := a.Store.MemberByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
			m.Name = strings.TrimSpace(*in.Name)
		}
		if in.Roles != nil {
			roles, err := a.Store.ListRoles(ctx, tx)
			if err != nil {
				return err
			}
			known := map[string]bool{}
			for _, r := range roles {
				known[r.Name] = true
			}
			for _, r := range in.Roles {
				if !known[r] {
					return Bad("err.role_unknown", r)
				}
			}
			m.Roles = in.Roles
		}
		if in.Active != nil {
			m.Active = *in.Active
		}
		if err := a.Store.UpdateMember(ctx, tx, m); err != nil {
			return err
		}
		if in.TeamID != nil {
			if *in.TeamID != "" {
				if _, err := teamByID(ctx, tx, a, *in.TeamID); err != nil {
					return Bad("err.team_missing")
				}
			}
			if err := a.Store.SetMemberTeam(ctx, tx, sess.OrgID, m.ID, *in.TeamID); err != nil {
				return err
			}
		}
		if in.Active != nil && !*in.Active {
			return a.deactivateMemberEffects(ctx, tx, sess, m)
		}
		return nil
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
}

// Invite 创建邀请并返回链接（一期不发邮件）。
func (a *App) Invite(ctx context.Context, sess *Session, email, name string, roles []string) (*InvitationView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailRe.MatchString(email) {
		return nil, Bad("err.email_required")
	}
	if roles == nil {
		roles = []string{}
	}
	var view *InvitationView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		if acc, _, err := a.Store.AccountByEmail(ctx, tx, email); err == nil {
			if _, err := a.Store.MemberByAccount(ctx, tx, sess.OrgID, acc.ID); err == nil {
				return Bad("err.already_member")
			}
		}
		inv := &domain.Invitation{OrgID: sess.OrgID, Email: email, Name: name, Roles: roles, InvitedBy: sess.MemberID, ExpiresAt: time.Now().Add(14 * 24 * time.Hour)}
		token, err := a.Store.CreateInvitation(ctx, tx, inv)
		if err != nil {
			return err
		}
		view = &InvitationView{Invitation: inv, URL: a.PublicURL + "/invite/" + token + "/"}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "MemberInvited", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"email": email}}})
	})
	return view, err
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

// DeleteInvitation 作废邀请。
func (a *App) DeleteInvitation(ctx context.Context, sess *Session, id string) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error { return a.Store.DeleteInvitation(ctx, tx, id) })
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
		for _, t := range teams {
			ids := members[t.ID]
			if ids == nil {
				ids = []string{}
			}
			out = append(out, TeamView{Team: t, MemberIDs: ids})
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
}

// SaveTeam 新建（id 空）或修改团队。
func (a *App) SaveTeam(ctx context.Context, sess *Session, id string, in TeamPatch) (*TeamView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		var t *domain.Team
		var events []domain.Event
		if id == "" {
			if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
				return Bad("err.title_required")
			}
			t = &domain.Team{OrgID: sess.OrgID, Name: strings.TrimSpace(*in.Name)}
			if in.ParentID != nil {
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
			events = append(events, domain.Event{Type: "TeamCreated", ActorID: sess.Actor.ID, At: time.Now(),
				Data: map[string]any{"team_id": t.ID, "name": t.Name, "is_boundary": t.IsBoundary}})
			if t.IsBoundary {
				events = append(events, domain.Event{Type: "TeamBoundaryChanged", ActorID: sess.Actor.ID, At: time.Now(),
					Data: map[string]any{"team_id": t.ID, "name": t.Name, "is_boundary": true}})
			}
		} else {
			cur, err := teamByID(ctx, tx, a, id)
			if err != nil {
				return Bad("err.team_missing")
			}
			t = cur
			if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
				t.Name = strings.TrimSpace(*in.Name)
			}
			if in.ParentID != nil && *in.ParentID != id {
				t.ParentID = *in.ParentID
			}
			if in.LeadID != nil {
				t.LeadMemberID = *in.LeadID
			}
			// 共享边界改动会立刻改变一批人的可见范围，单独记一条动态说清楚（ADR 0013）
			if in.IsBoundary != nil && *in.IsBoundary != t.IsBoundary {
				t.IsBoundary = *in.IsBoundary
				events = append(events, domain.Event{Type: "TeamBoundaryChanged", ActorID: sess.Actor.ID, At: time.Now(),
					Data: map[string]any{"team_id": t.ID, "name": t.Name, "is_boundary": t.IsBoundary}})
			}
			if err := a.Store.UpdateTeam(ctx, tx, t); err != nil {
				return err
			}
			events = append(events, domain.Event{Type: "TeamUpdated", ActorID: sess.Actor.ID, At: time.Now(),
				Data: map[string]any{"team_id": t.ID, "name": t.Name, "is_boundary": t.IsBoundary}})
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

// DeleteTeam 删除团队。
func (a *App) DeleteTeam(ctx context.Context, sess *Session, id string) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		if err := a.Store.DeleteTeam(ctx, tx, id); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "TeamDeleted", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"team_id": id}}})
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
	return a.tx(ctx, sess, func(tx pgx.Tx) error { return a.Store.DeletePrice(ctx, tx, sess.OrgID, modelID) })
}

// SetExchangeRate 写汇率。
func (a *App) SetExchangeRate(ctx context.Context, sess *Session, from, to string, rate float64) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	if rate <= 0 || from == "" || to == "" {
		return Bad("err.price_fields")
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		return a.Store.UpsertExchangeRate(ctx, tx, sess.OrgID, strings.ToUpper(from), strings.ToUpper(to), rate)
	})
}
