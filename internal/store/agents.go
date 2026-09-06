package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

const agentCols = `id,org_id,owner_member_id,name,runtime,capabilities,grants,max_concurrent,shared,last_seen_at,revoked_at,created_at,coalesce(last_tool,''),last_tool_at`

func scanAgent(r interface{ Scan(...any) error }) (*domain.Agent, error) {
	a := &domain.Agent{}
	var grants []byte
	err := r.Scan(&a.ID, &a.OrgID, &a.OwnerMemberID, &a.Name, &a.Runtime, &a.Capabilities, &grants, &a.MaxConcurrent, &a.Shared, &a.LastSeenAt, &a.RevokedAt, &a.CreatedAt, &a.LastTool, &a.LastToolAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.Grants = map[domain.Grant]domain.GrantMode{}
	_ = json.Unmarshal(grants, &a.Grants)
	if a.Capabilities == nil {
		a.Capabilities = []string{}
	}
	return a, nil
}

// CreateAgent 注册 Agent，返回明文令牌（只此一次）。
func (s *Store) CreateAgent(ctx context.Context, q Querier, a *domain.Agent) (string, error) {
	if a.ID == "" {
		a.ID = NewID("agt")
	}
	token := "axm_" + NewID("tok")[4:] + NewID("")[1:]
	grants, _ := json.Marshal(a.Grants)
	if a.Capabilities == nil {
		a.Capabilities = []string{}
	}
	a.CreatedAt = time.Now()
	_, err := q.Exec(ctx, `insert into agents(id,org_id,owner_member_id,name,runtime,capabilities,grants,max_concurrent,shared,token_hash,created_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		a.ID, a.OrgID, a.OwnerMemberID, a.Name, a.Runtime, a.Capabilities, grants, a.MaxConcurrent, a.Shared, HashToken(token), a.CreatedAt)
	return token, err
}

func (s *Store) AgentByID(ctx context.Context, q Querier, id string) (*domain.Agent, error) {
	return scanAgent(q.QueryRow(ctx, `select `+agentCols+` from agents where id=$1`, id))
}

// AgentByToken 不经行级安全（令牌先于组织上下文），返回 Agent 及其组织。
func (s *Store) AgentByToken(ctx context.Context, q Querier, token string) (*domain.Agent, error) {
	a, err := scanAgent(q.QueryRow(ctx, `select `+agentCols+` from agents where token_hash=$1 and revoked_at is null`, HashToken(token)))
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Store) ListAgents(ctx context.Context, q Querier) ([]*domain.Agent, error) {
	rows, err := q.Query(ctx, `select `+agentCols+` from agents where revoked_at is null order by created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UpdateAgent(ctx context.Context, q Querier, a *domain.Agent) error {
	grants, _ := json.Marshal(a.Grants)
	_, err := q.Exec(ctx, `update agents set name=$2,runtime=$3,capabilities=$4,grants=$5,max_concurrent=$6,shared=$7 where id=$1`,
		a.ID, a.Name, a.Runtime, a.Capabilities, grants, a.MaxConcurrent, a.Shared)
	return err
}

func (s *Store) TouchAgent(ctx context.Context, q Querier, id string) error {
	_, err := q.Exec(ctx, `update agents set last_seen_at=now() where id=$1`, id)
	return err
}

// RecordAgentTool 记下 Agent 最近一次调用的 MCP 工具（连接检查用）。
func (s *Store) RecordAgentTool(ctx context.Context, q Querier, id, tool string) error {
	_, err := q.Exec(ctx, `update agents set last_seen_at=now(), last_tool=$2, last_tool_at=now() where id=$1`, id, tool)
	return err
}

func (s *Store) RevokeAgent(ctx context.Context, q Querier, id string) error {
	_, err := q.Exec(ctx, `update agents set revoked_at=now(), token_hash=null where id=$1`, id)
	return err
}

// ---------- 能力标签 ----------

func (s *Store) ListCapabilities(ctx context.Context, q Querier) (map[string]i18n.Text, error) {
	rows, err := q.Query(ctx, `select name,title,titles from capabilities order by name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]i18n.Text{}
	for rows.Next() {
		var n, t string
		var titles []byte
		if err := rows.Scan(&n, &t, &titles); err != nil {
			return nil, err
		}
		var tx i18n.Text
		_ = json.Unmarshal(titles, &tx)
		if tx.IsZero() {
			tx = i18n.Text{i18n.Default: t}
		}
		out[n] = tx
	}
	return out, rows.Err()
}

func (s *Store) UpsertCapability(ctx context.Context, q Querier, orgID, name string, title i18n.Text) error {
	titles, _ := json.Marshal(title)
	_, err := q.Exec(ctx, `insert into capabilities(org_id,name,title,titles) values($1,$2,$3,$4) on conflict (org_id,name) do update set title=excluded.title, titles=excluded.titles`, orgID, name, title.In(i18n.Default), titles)
	return err
}

func (s *Store) DeleteCapability(ctx context.Context, q Querier, name string) error {
	_, err := q.Exec(ctx, `delete from capabilities where name=$1`, name)
	return err
}

func (s *Store) CountAgents(ctx context.Context, q Querier) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from agents where revoked_at is null`).Scan(&n)
	return n, err
}

// ---------- 执行者解析 ----------

// ExecutorByID 把成员或 Agent 解析成领域层的执行者（Agent 的角色复制自所有者）。
func (s *Store) ExecutorByID(ctx context.Context, q Querier, id string) (*domain.Executor, error) {
	if m, err := s.MemberByID(ctx, q, id); err == nil {
		return &domain.Executor{ID: m.ID, Kind: domain.ExecutorMember, Name: m.Name, Roles: m.Roles}, nil
	} else if err != ErrNotFound {
		return nil, err
	}
	a, err := s.AgentByID(ctx, q, id)
	if err != nil {
		return nil, err
	}
	return s.executorOfAgent(ctx, q, a)
}

func (s *Store) executorOfAgent(ctx context.Context, q Querier, a *domain.Agent) (*domain.Executor, error) {
	owner, err := s.MemberByID(ctx, q, a.OwnerMemberID)
	if err != nil {
		return nil, err
	}
	return &domain.Executor{ID: a.ID, Kind: domain.ExecutorAgent, Name: a.Name, OwnerID: owner.ID, Roles: owner.Roles,
		Grants: a.Grants, Capabilities: a.Capabilities, MaxConcurrent: a.MaxConcurrent, Shared: a.Shared}, nil
}

// ExecutorOfAgent 导出版本。
func (s *Store) ExecutorOfAgent(ctx context.Context, q Querier, a *domain.Agent) (*domain.Executor, error) {
	return s.executorOfAgent(ctx, q, a)
}

// ExecutorNames 返回所有成员与 Agent 的名字映射，用于展示。
func (s *Store) ExecutorNames(ctx context.Context, q Querier) (map[string]string, error) {
	out := map[string]string{}
	rows, err := q.Query(ctx, `select id,name from members union all select id,name from agents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}
