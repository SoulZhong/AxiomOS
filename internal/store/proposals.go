package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

const proposalCols = `id,org_id,agent_id,owner_id,action,grant_name,target_kind,target_id,target_title,payload,summary,status,coalesce(decided_by,''),decided_at,reason,created_at,expires_at`

func scanProposal(r interface{ Scan(...any) error }) (*domain.Proposal, error) {
	p := &domain.Proposal{}
	var payload, summary []byte
	var status, grant string
	err := r.Scan(&p.ID, &p.OrgID, &p.AgentID, &p.OwnerID, &p.Action, &grant, &p.TargetKind, &p.TargetID, &p.TargetTitle,
		&payload, &summary, &status, &p.DecidedBy, &p.DecidedAt, &p.Reason, &p.CreatedAt, &p.ExpiresAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Grant = domain.Grant(grant)
	p.Status = domain.ProposalStatus(status)
	p.Payload = map[string]any{}
	_ = json.Unmarshal(payload, &p.Payload)
	_ = json.Unmarshal(summary, &p.Summary)
	return p, nil
}

// InsertProposal 写入一条待确认操作。
func (s *Store) InsertProposal(ctx context.Context, q Querier, p *domain.Proposal) error {
	if p.ID == "" {
		p.ID = NewID("prp")
	}
	if p.Status == "" {
		p.Status = domain.ProposalPending
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	if p.ExpiresAt.IsZero() {
		p.ExpiresAt = p.CreatedAt.Add(domain.ProposalTTL)
	}
	if p.Payload == nil {
		p.Payload = map[string]any{}
	}
	payload, err := json.Marshal(p.Payload)
	if err != nil {
		return err
	}
	summary, err := json.Marshal(p.Summary)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `insert into proposals(id,org_id,agent_id,owner_id,action,grant_name,target_kind,target_id,target_title,payload,summary,status,created_at,expires_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		p.ID, p.OrgID, p.AgentID, p.OwnerID, p.Action, string(p.Grant), p.TargetKind, p.TargetID, p.TargetTitle,
		payload, summary, string(p.Status), p.CreatedAt, p.ExpiresAt)
	return err
}

// ProposalByID 读取一条待确认操作。
func (s *Store) ProposalByID(ctx context.Context, q Querier, id string) (*domain.Proposal, error) {
	return scanProposal(q.QueryRow(ctx, `select `+proposalCols+` from proposals where id=$1`, id))
}

// UpdateProposalDecision 记录确认 / 拒绝 / 过期的结果。
func (s *Store) UpdateProposalDecision(ctx context.Context, q Querier, p *domain.Proposal) error {
	_, err := q.Exec(ctx, `update proposals set status=$2, decided_by=nullif($3,''), decided_at=$4, reason=$5 where id=$1`,
		p.ID, string(p.Status), p.DecidedBy, p.DecidedAt, p.Reason)
	return err
}

// ProposalFilter 是待确认操作列表的过滤条件。
type ProposalFilter struct {
	Status   domain.ProposalStatus
	AgentID  string
	OwnerID  string   // 只看这个成员名下 Agent 提交的
	Actions  []string // 只看这些动作
	Limit    int
	OnlyOpen bool // 只看还在等人确认的
}

// ListProposals 按创建时间倒序列出待确认操作。
func (s *Store) ListProposals(ctx context.Context, q Querier, f ProposalFilter) ([]*domain.Proposal, error) {
	var where []string
	var args []any
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if f.Status != "" {
		where = append(where, "status="+arg(string(f.Status)))
	}
	if f.OnlyOpen {
		where = append(where, "status='pending'")
	}
	if f.AgentID != "" {
		where = append(where, "agent_id="+arg(f.AgentID))
	}
	if f.OwnerID != "" {
		where = append(where, "owner_id="+arg(f.OwnerID))
	}
	if len(f.Actions) > 0 {
		where = append(where, "action = any("+arg(f.Actions)+")")
	}
	sql := `select ` + proposalCols + ` from proposals`
	if len(where) > 0 {
		sql += " where " + strings.Join(where, " and ")
	}
	sql += " order by created_at desc, id desc"
	if f.Limit > 0 {
		sql += fmt.Sprintf(" limit %d", f.Limit)
	}
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CountPendingProposals 统计还在等人确认的条数。
func (s *Store) CountPendingProposals(ctx context.Context, q Querier) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from proposals where status='pending'`).Scan(&n)
	return n, err
}

// StaleProposals 返回已经过了有效期、但还在等人确认的待确认操作。
func (s *Store) StaleProposals(ctx context.Context, q Querier, now time.Time) ([]*domain.Proposal, error) {
	rows, err := q.Query(ctx, `select `+proposalCols+` from proposals where status='pending' and expires_at < $1 order by created_at`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
