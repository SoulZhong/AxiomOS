package app

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// 在 Agent 里确认（ADR 0027）。人在 Agent 客户端里敲斜杠命令 /confirm，客户端向服务器取提示
// （prompts/get）——这一步只有人能触发，Agent 自己调不到。服务器趁这一步发一个一次性的确认凭证，
// 写进提示文本，凭证只对人在命令里点名的那几条待确认操作有效、十分钟内有效、每条只能用一次。
// Agent 随后调 decide_proposal 把凭证带回来，服务器以**所有者本人**的身份裁决。
// 凭证证明的是"人刚刚敲了这条命令"，Agent 拿它做不了人没点名的事。

// ConfirmTTL 是凭证有效期。
const ConfirmTTL = 10 * time.Minute

// confirmGrant 是一张凭证：发给哪个 Agent、代表哪个人、允许裁决哪些待确认操作。
type confirmGrant struct {
	OrgID     string
	AgentID   string
	MemberID  string
	Allowed   map[string]bool // 待确认操作 ID → 还没用过
	Decision  string          // approve | reject | ""（都可以）
	ExpiresAt time.Time
	mu        sync.Mutex
}

// ConfirmScope 是人在命令里点名的范围：一个待确认操作的 ID，或 all（全部等他确认的）。
type ConfirmScope struct {
	Which    string // 待确认操作 ID | "all"
	Decision string // approve | reject | ""
}

// ConfirmTicket 是发出去的凭证与它覆盖的待确认操作。
type ConfirmTicket struct {
	Nonce     string          `json:"nonce"`
	ExpiresAt time.Time       `json:"expires_at"`
	Proposals []*ProposalView `json:"proposals"`
}

// MintConfirmTicket 给 Agent 的所有者发一张确认凭证。只有 Agent 会话能取（人在 Agent 客户端里）；
// 范围里的待确认操作必须是所有者本人能裁决的、还在等确认的；一条都没有就不发凭证。
func (a *App) MintConfirmTicket(ctx context.Context, sess *Session, scope ConfirmScope) (*ConfirmTicket, error) {
	if !sess.IsAgent() {
		return nil, Bad("err.confirm_agent_only")
	}
	decision := strings.ToLower(strings.TrimSpace(scope.Decision))
	if decision != "" && decision != "approve" && decision != "reject" {
		return nil, Bad("err.confirm_decision", scope.Decision)
	}
	owner, err := a.memberSession(ctx, sess.OrgID, sess.MemberID)
	if err != nil {
		return nil, err
	}
	owner.Locale = sess.Loc()
	mine, err := a.ListProposals(ctx, owner, ProposalFilter{Status: domain.ProposalPending, Mine: true})
	if err != nil {
		return nil, err
	}
	which := strings.TrimSpace(scope.Which)
	var picked []*ProposalView
	for _, p := range mine {
		if which == "" || which == "all" || p.ID == which {
			picked = append(picked, p)
		}
	}
	if len(picked) == 0 {
		if which != "" && which != "all" {
			return nil, NotFound("err.confirm_scope", which)
		}
		return nil, Bad("err.confirm_nothing")
	}
	g := &confirmGrant{OrgID: sess.OrgID, AgentID: sess.AgentID, MemberID: sess.MemberID, Allowed: map[string]bool{}, Decision: decision, ExpiresAt: time.Now().Add(ConfirmTTL)}
	for _, p := range picked {
		g.Allowed[p.ID] = true
	}
	nonce := store.NewID("cfm")
	a.confirmGrants.Store(nonce, g)
	return &ConfirmTicket{Nonce: nonce, ExpiresAt: g.ExpiresAt, Proposals: picked}, nil
}

// DecideViaAgent 用凭证裁决一条待确认操作：以所有者本人的身份执行，动态里记上「由 Agent 转达」。
func (a *App) DecideViaAgent(ctx context.Context, sess *Session, nonce, proposalID, decision, reason string, opts ApproveOptions) (any, error) {
	if !sess.IsAgent() {
		return nil, Bad("err.confirm_agent_only")
	}
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != "approve" && decision != "reject" {
		return nil, Bad("err.confirm_decision", decision)
	}
	v, ok := a.confirmGrants.Load(strings.TrimSpace(nonce))
	if !ok {
		return nil, Forbidden("err.confirm_nonce")
	}
	g := v.(*confirmGrant)
	g.mu.Lock()
	defer g.mu.Unlock()
	if time.Now().After(g.ExpiresAt) || g.AgentID != sess.AgentID || g.OrgID != sess.OrgID {
		a.confirmGrants.Delete(nonce)
		return nil, Forbidden("err.confirm_nonce")
	}
	if !g.Allowed[proposalID] {
		return nil, Forbidden("err.confirm_scope", proposalID)
	}
	if g.Decision != "" && g.Decision != decision {
		return nil, Forbidden("err.confirm_decision_fixed", g.Decision)
	}
	owner, err := a.memberSession(ctx, g.OrgID, g.MemberID)
	if err != nil {
		return nil, err
	}
	owner.Locale = sess.Loc()
	owner.ViaAgentID, owner.ViaAgentName = sess.AgentID, sess.Actor.Name
	var out any
	if decision == "approve" {
		out, err = a.ApproveProposalWith(ctx, owner, proposalID, opts)
	} else {
		out, err = a.RejectProposal(ctx, owner, proposalID, reason)
	}
	if err != nil {
		return nil, err
	}
	// 每条只能用一次；全用完了凭证就作废
	delete(g.Allowed, proposalID)
	if len(g.Allowed) == 0 {
		a.confirmGrants.Delete(nonce)
	}
	return out, nil
}

// ListProposalsForOwner 列出等 Agent 的所有者确认的待确认操作（给 /confirm 的清单用），按所有者本人的身份算。
func (a *App) ListProposalsForOwner(ctx context.Context, sess *Session) ([]*ProposalView, error) {
	if !sess.IsAgent() {
		return nil, Bad("err.confirm_agent_only")
	}
	owner, err := a.memberSession(ctx, sess.OrgID, sess.MemberID)
	if err != nil {
		return nil, err
	}
	owner.Locale = sess.Loc()
	return a.ListProposals(ctx, owner, ProposalFilter{Status: domain.ProposalPending, Mine: true})
}

// memberSession 构造某个成员的会话（权限、范围都按本人算）。
func (a *App) memberSession(ctx context.Context, orgID, memberID string) (*Session, error) {
	var sess *Session
	err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		m, err := a.Store.MemberByID(ctx, tx, memberID)
		if err != nil {
			return Forbidden("err.member_missing")
		}
		if !m.Active {
			return Forbidden("err.member_inactive")
		}
		sess = &Session{OrgID: orgID, MemberID: m.ID, AccountID: m.AccountID, Actor: &domain.Executor{ID: m.ID, Kind: domain.ExecutorMember, Name: m.Name, Roles: m.Roles}}
		return a.fillSession(ctx, tx, sess, m)
	})
	return sess, wrapErr(err)
}

// SweepConfirmTickets 清掉过期的凭证（后台巡检调用）。
func (a *App) SweepConfirmTickets(now time.Time) int {
	n := 0
	a.confirmGrants.Range(func(k, v any) bool {
		if g, ok := v.(*confirmGrant); ok && now.After(g.ExpiresAt) {
			a.confirmGrants.Delete(k)
			n++
		}
		return true
	})
	return n
}
