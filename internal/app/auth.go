package app

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

const sessionTTL = 30 * 24 * time.Hour

// Login 用邮箱密码登录，返回会话令牌。orgSlug 为空时取账号的第一个组织。
func (a *App) Login(ctx context.Context, email, password, orgSlug string) (token string, sess *Session, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	acc, hash, err := a.Store.AccountByEmail(ctx, a.Store.Pool, email)
	if err != nil {
		return "", nil, Bad("err.bad_credentials")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", nil, Bad("err.bad_credentials")
	}
	orgs, err := a.Store.OrganizationsOfAccount(ctx, a.Store.Pool, acc.ID)
	if err != nil {
		return "", nil, err
	}
	if len(orgs) == 0 {
		return "", nil, Forbidden("err.no_org")
	}
	org := orgs[0]
	if orgSlug != "" {
		found := false
		for _, o := range orgs {
			if o.Slug == orgSlug {
				org, found = o, true
			}
		}
		if !found {
			return "", nil, Forbidden("err.not_in_org", orgSlug)
		}
	}
	token = store.NewID("ses") + store.NewID("")[1:]
	if err := a.Store.CreateSession(ctx, a.Store.Pool, token, acc.ID, org.ID, sessionTTL); err != nil {
		return "", nil, err
	}
	sess, err = a.SessionFromToken(ctx, token)
	return token, sess, err
}

// Logout 作废会话。
func (a *App) Logout(ctx context.Context, token string) error {
	return a.Store.DeleteSession(ctx, a.Store.Pool, token)
}

// SessionFromToken 解析人类会话。
func (a *App) SessionFromToken(ctx context.Context, token string) (*Session, error) {
	accountID, orgID, err := a.Store.SessionLookup(ctx, a.Store.Pool, token)
	if err != nil {
		return nil, Unauthorized("err.login_required")
	}
	if orgID == "" {
		return nil, Unauthorized("err.login_required")
	}
	var sess *Session
	err = a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		m, err := a.Store.MemberByAccount(ctx, tx, orgID, accountID)
		if err != nil {
			return Forbidden("err.member_missing")
		}
		if !m.Active {
			return Forbidden("err.member_inactive")
		}
		sess = &Session{OrgID: orgID, MemberID: m.ID, AccountID: accountID, Actor: &domain.Executor{ID: m.ID, Kind: domain.ExecutorMember, Name: m.Name, Roles: m.Roles}}
		return a.fillSession(ctx, tx, sess, m)
	})
	return sess, wrapErr(err)
}

// fillSession 补齐语言、负责人身份与权限。
func (a *App) fillSession(ctx context.Context, tx pgx.Tx, sess *Session, m *domain.Member) error {
	org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
	if err != nil {
		return err
	}
	sess.IsOwner = org.OwnerMemberID == m.ID
	sess.Locale = a.localeOfMember(ctx, tx, sess.OrgID, m.ID)
	sess.Permissions = map[string]bool{}
	roles, err := a.Store.ListRoles(ctx, tx)
	if err != nil {
		return err
	}
	for _, r := range roles {
		if contains(m.Roles, r.Name) {
			for _, p := range r.Permissions {
				sess.Permissions[p] = true
			}
		}
	}
	// 没有角色表记录的内置角色按内置权限
	for _, br := range domain.BuiltinRoles {
		if contains(m.Roles, br.Name) {
			for _, p := range br.Permissions {
				sess.Permissions[p] = true
			}
		}
	}
	return a.loadScopeContext(ctx, tx, sess, org)
}

// SessionFromAgentToken 解析 Agent 令牌。
func (a *App) SessionFromAgentToken(ctx context.Context, token string) (*Session, error) {
	ag, err := a.Store.AgentByToken(ctx, a.Store.Pool, token)
	if err != nil {
		return nil, Unauthorized("err.agent_token")
	}
	var sess *Session
	err = a.Store.WithOrg(ctx, ag.OrgID, func(tx pgx.Tx) error {
		ex, err := a.Store.ExecutorOfAgent(ctx, tx, ag)
		if err != nil {
			return err
		}
		if err := a.Store.TouchAgent(ctx, tx, ag.ID); err != nil {
			return err
		}
		owner, err := a.Store.MemberByID(ctx, tx, ag.OwnerMemberID)
		if err != nil {
			return err
		}
		sess = &Session{OrgID: ag.OrgID, MemberID: ag.OwnerMemberID, AccountID: owner.AccountID, AgentID: ag.ID, Actor: ex}
		return a.fillSession(ctx, tx, sess, owner)
	})
	return sess, wrapErr(err)
}

// SetMyLocale 修改当前账号的语言。
func (a *App) SetMyLocale(ctx context.Context, sess *Session, locale string) error {
	l := i18n.Normalize(locale)
	if l == "" {
		return Bad("err.locale", locale)
	}
	ls := string(l)
	return wrapErr(a.Store.UpdateAccount(ctx, a.Store.Pool, sess.AccountID, nil, &ls, nil))
}

// Me 是当前身份的展示信息。
type Me struct {
	OrgID        string               `json:"org_id"`
	Organization *domain.Organization `json:"organization"`
	Member       *domain.Member       `json:"member"`
	Email        string               `json:"email"`
	Locale       i18n.Locale          `json:"locale"`
	Agent        *domain.Agent        `json:"agent,omitempty"`
	Agents       []*domain.Agent      `json:"agents"`
	IsOrgOwner   bool                 `json:"is_org_owner"`
	Permissions  []string             `json:"permissions"`
	Scopes       []ScopeOption        `json:"scopes"`
	DefaultScope string               `json:"default_scope"`
}

// Me 返回当前身份。
func (a *App) Me(ctx context.Context, sess *Session) (*Me, error) {
	me := &Me{OrgID: sess.OrgID}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		me.Organization = org
		me.Member, err = a.Store.MemberByID(ctx, tx, sess.MemberID)
		if err != nil {
			return err
		}
		me.IsOrgOwner = org.OwnerMemberID == sess.MemberID
		me.Locale = sess.Loc()
		me.Permissions = []string{}
		for p := range sess.Permissions {
			me.Permissions = append(me.Permissions, p)
		}
		if acc, err := a.Store.AccountByID(ctx, tx, me.Member.AccountID); err == nil {
			me.Email = acc.Email
		}
		if sess.IsAgent() {
			me.Agent, err = a.Store.AgentByID(ctx, tx, sess.AgentID)
			if err != nil {
				return err
			}
		}
		me.Scopes, me.DefaultScope, err = a.ScopeOptions(ctx, tx, sess)
		if err != nil {
			return err
		}
		all, err := a.Store.ListAgents(ctx, tx)
		if err != nil {
			return err
		}
		me.Agents = []*domain.Agent{}
		for _, ag := range all {
			if ag.OwnerMemberID == sess.MemberID {
				me.Agents = append(me.Agents, ag)
			}
		}
		return nil
	})
	return me, err
}

// HashPassword 生成密码散列。
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// ---------- Agent 管理 ----------

// RegisterAgentInput 是注册 Agent 的输入。
type RegisterAgentInput struct {
	Name          string                            `json:"name"`
	Runtime       string                            `json:"runtime"`
	Capabilities  []string                          `json:"capabilities"`
	Grants        map[domain.Grant]domain.GrantMode `json:"grants"`
	MaxConcurrent int                               `json:"max_concurrent"`
	Shared        bool                              `json:"shared"`
	OwnerMemberID string                            `json:"owner_member_id"` // 公共 Agent 由组织负责人注册
}

// RegisterAgent 注册 Agent，返回一次性令牌。授权不得超出所有者权限（ADR 0003）。
func (a *App) RegisterAgent(ctx context.Context, sess *Session, in RegisterAgentInput) (*domain.Agent, string, error) {
	if sess.IsAgent() {
		return nil, "", Forbidden("err.agent_cant_register")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, "", Bad("err.agent_name")
	}
	if in.Grants == nil {
		in.Grants = map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect, domain.GrantComment: domain.GrantDirect}
	}
	if in.MaxConcurrent <= 0 {
		in.MaxConcurrent = 1
	}
	if in.Runtime == "" {
		in.Runtime = "custom"
	}
	var ag *domain.Agent
	var token string
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		owner := sess.MemberID
		if in.Shared {
			if org.OwnerMemberID != sess.MemberID {
				return Forbidden("err.shared_owner_only")
			}
			owner = org.OwnerMemberID
		}
		// 管理流程只能是需要人确认
		if _, ok := in.Grants[domain.GrantManageWorkflows]; ok {
			in.Grants[domain.GrantManageWorkflows] = domain.GrantWithApproval
		}
		caps, err := a.Store.ListCapabilities(ctx, tx)
		if err != nil {
			return err
		}
		for _, c := range in.Capabilities {
			if _, ok := caps[c]; !ok {
				return Bad("err.cap_unknown", c)
			}
		}
		ag = &domain.Agent{OrgID: sess.OrgID, OwnerMemberID: owner, Name: in.Name, Runtime: in.Runtime, Capabilities: in.Capabilities, Grants: in.Grants, MaxConcurrent: in.MaxConcurrent, Shared: in.Shared}
		token, err = a.Store.CreateAgent(ctx, tx, ag)
		if err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "AgentRegistered", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"agent_id": ag.ID, "name": ag.Name}}})
	})
	return ag, token, err
}

// ListAgents 列出组织内的 Agent。
func (a *App) ListAgents(ctx context.Context, sess *Session) ([]*domain.Agent, error) {
	var out []*domain.Agent
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		out, err = a.Store.ListAgents(ctx, tx)
		if out == nil {
			out = []*domain.Agent{}
		}
		return
	})
	return out, err
}

// UpdateAgent 修改授权、能力等；只有所有者或组织负责人可以。
func (a *App) UpdateAgent(ctx context.Context, sess *Session, id string, in RegisterAgentInput) (*domain.Agent, error) {
	var ag *domain.Agent
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cur, err := a.Store.AgentByID(ctx, tx, id)
		if err != nil {
			return err
		}
		org, _ := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if cur.OwnerMemberID != sess.MemberID && org.OwnerMemberID != sess.MemberID {
			return Forbidden("err.agent_edit_forbidden")
		}
		if in.Name != "" {
			cur.Name = in.Name
		}
		if in.Runtime != "" {
			cur.Runtime = in.Runtime
		}
		if in.Capabilities != nil {
			cur.Capabilities = in.Capabilities
		}
		if in.Grants != nil {
			if _, ok := in.Grants[domain.GrantManageWorkflows]; ok {
				in.Grants[domain.GrantManageWorkflows] = domain.GrantWithApproval
			}
			cur.Grants = in.Grants
		}
		if in.MaxConcurrent > 0 {
			cur.MaxConcurrent = in.MaxConcurrent
		}
		if err := a.Store.UpdateAgent(ctx, tx, cur); err != nil {
			return err
		}
		ag = cur
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "AgentUpdated", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"agent_id": cur.ID}}})
	})
	return ag, err
}

// RevokeAgent 吊销 Agent：令牌失效、进行中的执行记录取消、名下任务回到待领取。
func (a *App) RevokeAgent(ctx context.Context, sess *Session, id string) error {
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		cur, err := a.Store.AgentByID(ctx, tx, id)
		if err != nil {
			return err
		}
		org, _ := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if cur.OwnerMemberID != sess.MemberID && org.OwnerMemberID != sess.MemberID {
			return Forbidden("err.agent_revoke_forbid")
		}
		if err := a.Store.RevokeAgent(ctx, tx, id); err != nil {
			return err
		}
		tasks, err := a.Store.ListTasks(ctx, tx, store.TaskFilter{AssigneeID: id})
		if err != nil {
			return err
		}
		var events []domain.Event
		for _, t := range tasks {
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
			events = append(events, domain.Event{Type: "TaskSentToBacklog", TaskID: t.ID, ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"reason": "Agent 已吊销"}})
		}
		events = append(events, domain.Event{Type: "AgentRevoked", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"agent_id": id}})
		return a.insertEvents(ctx, tx, sess, events)
	})
}

// ListMembers 列出组织成员。
func (a *App) ListMembers(ctx context.Context, sess *Session) ([]*domain.Member, error) {
	var out []*domain.Member
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		out, err = a.Store.ListMembers(ctx, tx)
		return
	})
	return out, err
}

// Capabilities 列出能力标签词表（多语言）。
func (a *App) Capabilities(ctx context.Context, sess *Session) (map[string]i18n.Text, error) {
	var out map[string]i18n.Text
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		out, err = a.Store.ListCapabilities(ctx, tx)
		return
	})
	return out, err
}

// ExecutorNames 名称映射。
func (a *App) ExecutorNames(ctx context.Context, sess *Session) (map[string]string, error) {
	var out map[string]string
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		out, err = a.Store.ExecutorNames(ctx, tx)
		return
	})
	return out, err
}
