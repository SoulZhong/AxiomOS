package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// ---------- 角色 ----------

func (s *Store) ListRoles(ctx context.Context, q Querier) ([]*domain.Role, error) {
	rows, err := q.Query(ctx, `select name,titles,permissions,builtin from roles order by builtin desc, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Role
	for rows.Next() {
		r := &domain.Role{}
		var titles []byte
		if err := rows.Scan(&r.Name, &titles, &r.Permissions, &r.BuiltIn); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(titles, &r.Title)
		if r.Permissions == nil {
			r.Permissions = []string{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpsertRole(ctx context.Context, q Querier, orgID string, r *domain.Role) error {
	titles, _ := json.Marshal(r.Title)
	if r.Permissions == nil {
		r.Permissions = []string{}
	}
	_, err := q.Exec(ctx, `insert into roles(org_id,name,titles,permissions,builtin) values($1,$2,$3,$4,$5)
		on conflict (org_id,name) do update set titles=excluded.titles, permissions=excluded.permissions`, orgID, r.Name, titles, r.Permissions, r.BuiltIn)
	return err
}

func (s *Store) DeleteRole(ctx context.Context, q Querier, name string) error {
	_, err := q.Exec(ctx, `delete from roles where name=$1`, name)
	return err
}

func (s *Store) CountMembersWithRole(ctx context.Context, q Querier, role string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from members where $1 = any(roles)`, role).Scan(&n)
	return n, err
}

// ---------- 邀请 ----------

func (s *Store) CreateInvitation(ctx context.Context, q Querier, inv *domain.Invitation) (token string, err error) {
	if inv.ID == "" {
		inv.ID = NewID("inv")
	}
	token = "inv_" + NewID("t")[2:] + NewID("")[1:]
	inv.CreatedAt = time.Now()
	if inv.Roles == nil {
		inv.Roles = []string{}
	}
	_, err = q.Exec(ctx, `insert into invitations(id,org_id,email,name,roles,token_hash,invited_by,expires_at,created_at,team_id,member_id) values($1,$2,$3,$4,$5,$6,nullif($7,''),$8,$9,nullif($10,''),nullif($11,''))`,
		inv.ID, inv.OrgID, inv.Email, inv.Name, inv.Roles, HashToken(token), inv.InvitedBy, inv.ExpiresAt, inv.CreatedAt, inv.TeamID, inv.MemberID)
	return token, err
}

const invCols = `id,org_id,email,name,roles,coalesce(invited_by,''),expires_at,accepted_at,created_at,coalesce(team_id,''),coalesce(member_id,'')`

func scanInvitation(r interface{ Scan(...any) error }) (*domain.Invitation, error) {
	inv := &domain.Invitation{}
	err := r.Scan(&inv.ID, &inv.OrgID, &inv.Email, &inv.Name, &inv.Roles, &inv.InvitedBy, &inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt, &inv.TeamID, &inv.MemberID)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if inv.Roles == nil {
		inv.Roles = []string{}
	}
	return inv, err
}

func (s *Store) ListInvitations(ctx context.Context, q Querier) ([]*domain.Invitation, error) {
	rows, err := q.Query(ctx, `select `+invCols+` from invitations order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// InvitationByToken 不受行级安全限制（接受邀请时还没有组织上下文）。
func (s *Store) InvitationByToken(ctx context.Context, q Querier, token string) (*domain.Invitation, error) {
	return scanInvitation(q.QueryRow(ctx, `select `+invCols+` from invitations where token_hash=$1`, HashToken(token)))
}

// InvitationByID 在组织内按编号查邀请。
func (s *Store) InvitationByID(ctx context.Context, q Querier, id string) (*domain.Invitation, error) {
	return scanInvitation(q.QueryRow(ctx, `select `+invCols+` from invitations where id=$1`, id))
}

// CountOpenInvitationsForMember 数某个待激活成员还剩几条未接受、未过期的邀请。
func (s *Store) CountOpenInvitationsForMember(ctx context.Context, q Querier, memberID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from invitations where member_id=$1 and accepted_at is null and expires_at > now()`, memberID).Scan(&n)
	return n, err
}

func (s *Store) DeleteInvitation(ctx context.Context, q Querier, id string) error {
	_, err := q.Exec(ctx, `delete from invitations where id=$1`, id)
	return err
}

func (s *Store) MarkInvitationAccepted(ctx context.Context, q Querier, id string) error {
	_, err := q.Exec(ctx, `update invitations set accepted_at=now() where id=$1`, id)
	return err
}

// ---------- 平台统计 ----------

// OrgCounts 是一个组织的概览数字。
type OrgCounts struct {
	Members, Agents, Tasks int
	Cost30d                float64
}

func (s *Store) OrgCounts(ctx context.Context, q Querier, orgID string) (OrgCounts, error) {
	var c OrgCounts
	err := q.QueryRow(ctx, `select
		(select count(*) from members where org_id=$1 and active),
		(select count(*) from agents where org_id=$1 and revoked_at is null),
		(select count(*) from tasks where org_id=$1),
		coalesce((select sum(cost) from runs where org_id=$1 and started_at > now() - interval '30 days'),0)`, orgID).
		Scan(&c.Members, &c.Agents, &c.Tasks, &c.Cost30d)
	return c, err
}

func (s *Store) RunsLast30d(ctx context.Context, q Querier) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from runs where started_at > now() - interval '30 days'`).Scan(&n)
	return n, err
}

// ---------- 价格表管理 ----------

// PriceRow 是价格表里的一行（含来源）。
type PriceRow struct {
	domain.Price
	OrgID string
}

func (s *Store) ListPrices(ctx context.Context, q Querier, orgID string) ([]PriceRow, error) {
	rows, err := q.Query(ctx, `select model_id,input_per_million,output_per_million,cache_read_per_million,cache_write_per_million,currency,coalesce(org_id,'') from price_book where org_id is null or org_id=$1 order by org_id nulls first, model_id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PriceRow
	for rows.Next() {
		var p PriceRow
		if err := rows.Scan(&p.ModelID, &p.InputPerMillion, &p.OutputPerMillion, &p.CacheReadPerMillion, &p.CacheWritePerMillion, &p.Currency, &p.OrgID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertPrice 写入全局（orgID 空）或组织覆盖价格。
func (s *Store) UpsertPrice(ctx context.Context, q Querier, orgID string, p domain.Price) error {
	var org *string
	if orgID != "" {
		org = &orgID
	}
	if _, err := q.Exec(ctx, `delete from price_book where model_id=$1 and org_id is not distinct from $2`, p.ModelID, org); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `insert into price_book(org_id,model_id,input_per_million,output_per_million,cache_read_per_million,cache_write_per_million,currency) values($1,$2,$3,$4,$5,$6,$7)`,
		org, p.ModelID, p.InputPerMillion, p.OutputPerMillion, p.CacheReadPerMillion, p.CacheWritePerMillion, p.Currency)
	return err
}

func (s *Store) DeletePrice(ctx context.Context, q Querier, orgID, modelID string) error {
	var org *string
	if orgID != "" {
		org = &orgID
	}
	_, err := q.Exec(ctx, `delete from price_book where model_id=$1 and org_id is not distinct from $2`, modelID, org)
	return err
}

func (s *Store) ListExchangeRates(ctx context.Context, q Querier) ([][3]any, error) {
	rows, err := q.Query(ctx, `select from_currency,to_currency,rate from exchange_rates order by from_currency,to_currency`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][3]any
	for rows.Next() {
		var f, t string
		var r float64
		if err := rows.Scan(&f, &t, &r); err != nil {
			return nil, err
		}
		out = append(out, [3]any{f, t, r})
	}
	return out, rows.Err()
}

// RoleTitles 返回组织角色的多语言名称（内核渲染拒绝理由用）。
func (s *Store) RoleTitles(ctx context.Context, q Querier) (map[string]i18n.Text, error) {
	roles, err := s.ListRoles(ctx, q)
	if err != nil {
		return nil, err
	}
	out := map[string]i18n.Text{}
	for _, r := range roles {
		out[r.Name] = r.Title
	}
	return out, nil
}
