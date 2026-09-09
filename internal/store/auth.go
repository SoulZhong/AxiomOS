package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

// HashToken 对会话令牌或 Agent 令牌做单向散列后入库。
func HashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// ---------- 账号 ----------

func (s *Store) CreateAccount(ctx context.Context, q Querier, email, passwordHash, name string) (*domain.Account, error) {
	a := &domain.Account{ID: NewID("acc"), Email: email, Name: name, CreatedAt: time.Now()}
	_, err := q.Exec(ctx, `insert into accounts(id,email,password_hash,name,created_at) values($1,$2,$3,$4,$5)`, a.ID, a.Email, passwordHash, a.Name, a.CreatedAt)
	return a, err
}

func (s *Store) AccountByEmail(ctx context.Context, q Querier, email string) (*domain.Account, string, error) {
	a := &domain.Account{}
	var hash string
	err := q.QueryRow(ctx, `select id,email,name,created_at,password_hash,locale,platform_admin from accounts where lower(email)=lower($1)`, email).Scan(&a.ID, &a.Email, &a.Name, &a.CreatedAt, &hash, &a.Locale, &a.PlatformAdmin)
	if isNoRows(err) {
		return nil, "", ErrNotFound
	}
	return a, hash, err
}

func (s *Store) AccountByID(ctx context.Context, q Querier, id string) (*domain.Account, error) {
	a := &domain.Account{}
	err := q.QueryRow(ctx, `select id,email,name,created_at,locale,platform_admin from accounts where id=$1`, id).Scan(&a.ID, &a.Email, &a.Name, &a.CreatedAt, &a.Locale, &a.PlatformAdmin)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *Store) UpdateAccount(ctx context.Context, q Querier, id string, name, locale *string, passwordHash *string) error {
	_, err := q.Exec(ctx, `update accounts set name=coalesce($2,name), locale=coalesce($3,locale), password_hash=coalesce($4,password_hash) where id=$1`, id, name, locale, passwordHash)
	return err
}

func (s *Store) SetPlatformAdmin(ctx context.Context, q Querier, id string, on bool) error {
	_, err := q.Exec(ctx, `update accounts set platform_admin=$2 where id=$1`, id, on)
	return err
}

func (s *Store) ListPlatformAdmins(ctx context.Context, q Querier) ([]*domain.Account, error) {
	rows, err := q.Query(ctx, `select id,email,name,created_at,locale,platform_admin from accounts where platform_admin order by created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Account
	for rows.Next() {
		a := &domain.Account{}
		if err := rows.Scan(&a.ID, &a.Email, &a.Name, &a.CreatedAt, &a.Locale, &a.PlatformAdmin); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------- 会话 ----------

func (s *Store) CreateSession(ctx context.Context, q Querier, token, accountID, orgID string, ttl time.Duration) error {
	_, err := q.Exec(ctx, `insert into sessions(token_hash,account_id,org_id,expires_at) values($1,$2,$3,$4)`, HashToken(token), accountID, orgID, time.Now().Add(ttl))
	return err
}

func (s *Store) SessionLookup(ctx context.Context, q Querier, token string) (accountID, orgID string, err error) {
	err = q.QueryRow(ctx, `select account_id,org_id from sessions where token_hash=$1 and expires_at>now()`, HashToken(token)).Scan(&accountID, &orgID)
	if isNoRows(err) {
		return "", "", ErrNotFound
	}
	return
}

func (s *Store) DeleteSession(ctx context.Context, q Querier, token string) error {
	_, err := q.Exec(ctx, `delete from sessions where token_hash=$1`, HashToken(token))
	return err
}

// ---------- 组织 ----------

func (s *Store) CreateOrganization(ctx context.Context, q Querier, slug, name, currency string) (*domain.Organization, error) {
	o := &domain.Organization{ID: NewID("org"), Slug: slug, Name: name, Currency: currency, DefaultLocale: "zh-CN", CreatedAt: time.Now(),
		CollaborationVisibility: domain.DefaultCollaborationVisibility, FinanceVisibility: domain.DefaultFinanceVisibility}
	_, err := q.Exec(ctx, `insert into organizations(id,slug,name,currency,default_locale,created_at,collaboration_visibility,finance_visibility) values($1,$2,$3,$4,$5,$6,$7,$8)`,
		o.ID, o.Slug, o.Name, o.Currency, o.DefaultLocale, o.CreatedAt, string(o.CollaborationVisibility), string(o.FinanceVisibility))
	return o, err
}

func (s *Store) UpdateOrganization(ctx context.Context, q Querier, o *domain.Organization) error {
	_, err := q.Exec(ctx, `update organizations set name=$2, currency=$3, default_locale=$4, deactivated_at=$5, collaboration_visibility=$6, finance_visibility=$7 where id=$1`,
		o.ID, o.Name, o.Currency, o.DefaultLocale, o.DeactivatedAt, string(o.Collaboration()), string(o.Finance()))
	return err
}

// ListOrganizations 平台后台用，不受行级安全限制。
func (s *Store) ListOrganizations(ctx context.Context, q Querier) ([]*domain.Organization, error) {
	rows, err := q.Query(ctx, `select id from organizations order by created_at`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []*domain.Organization
	for _, id := range ids {
		o, err := s.OrganizationByID(ctx, q, id)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

func (s *Store) SetOrganizationOwner(ctx context.Context, q Querier, orgID, memberID string) error {
	_, err := q.Exec(ctx, `update organizations set owner_member_id=$2 where id=$1`, orgID, memberID)
	return err
}

func (s *Store) OrganizationByID(ctx context.Context, q Querier, id string) (*domain.Organization, error) {
	o := &domain.Organization{}
	var owner *string
	var collab, fin string
	err := q.QueryRow(ctx, `select id,slug,name,owner_member_id,currency,default_locale,created_at,deactivated_at,coalesce(collaboration_visibility,''),coalesce(finance_visibility,'') from organizations where id=$1`, id).
		Scan(&o.ID, &o.Slug, &o.Name, &owner, &o.Currency, &o.DefaultLocale, &o.CreatedAt, &o.DeactivatedAt, &collab, &fin)
	o.CollaborationVisibility, o.FinanceVisibility = domain.Visibility(collab), domain.Visibility(fin)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if owner != nil {
		o.OwnerMemberID = *owner
	}
	return o, err
}

func (s *Store) OrganizationBySlug(ctx context.Context, q Querier, slug string) (*domain.Organization, error) {
	var id string
	if err := q.QueryRow(ctx, `select id from organizations where slug=$1`, slug).Scan(&id); err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.OrganizationByID(ctx, q, id)
}

// OrganizationsOfAccount 列出账号所属的组织（不受行级安全限制，组织表不隔离）。
func (s *Store) OrganizationsOfAccount(ctx context.Context, q Querier, accountID string) ([]*domain.Organization, error) {
	rows, err := q.Query(ctx, `select o.id from organizations o where o.deactivated_at is null and exists (select 1 from members m where m.org_id=o.id and m.account_id=$1 and m.active)`, accountID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []*domain.Organization
	for _, id := range ids {
		o, err := s.OrganizationByID(ctx, q, id)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// ---------- 成员 ----------

const memberCols = `id,org_id,account_id,name,roles,active,created_at,coalesce(source,'manual'),coalesce(status,'active')`

func scanMember(r interface{ Scan(...any) error }) (*domain.Member, error) {
	m := &domain.Member{}
	err := r.Scan(&m.ID, &m.OrgID, &m.AccountID, &m.Name, &m.Roles, &m.Active, &m.CreatedAt, &m.Source, &m.Status)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// 已停用以 active 为准；status 列只存 active | pending_activation（ADR 0017）
	m.Status = m.DerivedStatus()
	return m, nil
}

func (s *Store) CreateMember(ctx context.Context, q Querier, orgID, accountID, name string, roles []string) (*domain.Member, error) {
	m := &domain.Member{OrgID: orgID, AccountID: accountID, Name: name, Roles: roles, Active: true, Source: domain.SourceManual, Status: domain.MemberActive}
	return m, s.CreateMemberFull(ctx, q, m)
}

func (s *Store) MemberByID(ctx context.Context, q Querier, id string) (*domain.Member, error) {
	return scanMember(q.QueryRow(ctx, `select `+memberCols+` from members where id=$1`, id))
}

func (s *Store) MemberByAccount(ctx context.Context, q Querier, orgID, accountID string) (*domain.Member, error) {
	return scanMember(q.QueryRow(ctx, `select `+memberCols+` from members where org_id=$1 and account_id=$2`, orgID, accountID))
}

func (s *Store) ListMembers(ctx context.Context, q Querier) ([]*domain.Member, error) {
	rows, err := q.Query(ctx, `select `+memberCols+` from members order by created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Member
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) UpdateMemberRoles(ctx context.Context, q Querier, id string, roles []string) error {
	_, err := q.Exec(ctx, `update members set roles=$2 where id=$1`, id, roles)
	return err
}

func (s *Store) UpdateMember(ctx context.Context, q Querier, m *domain.Member) error {
	// status 传 inactive 时不改激活状态（停用只体现在 active 上），这样停用再启用的待激活成员仍是待激活
	_, err := q.Exec(ctx, `update members set name=$2, roles=$3, active=$4, source=coalesce(nullif($5,''),source),
		status=case when $6 in ('active','pending_activation') then $6 else status end where id=$1`,
		m.ID, m.Name, m.Roles, m.Active, m.Source, m.Status)
	return err
}

// SetMemberTeam 让成员只属于一个团队（空表示移出所有团队）。
func (s *Store) SetMemberTeam(ctx context.Context, q Querier, orgID, memberID, teamID string) error {
	if _, err := q.Exec(ctx, `delete from team_members where member_id=$1`, memberID); err != nil {
		return err
	}
	if teamID == "" {
		return nil
	}
	return s.AddTeamMember(ctx, q, orgID, teamID, memberID)
}

func (s *Store) UpdateTeam(ctx context.Context, q Querier, t *domain.Team) error {
	_, err := q.Exec(ctx, `update teams set name=$2, parent_id=nullif($3,''), lead_member_id=nullif($4,''), is_boundary=$5, source=coalesce(nullif($6,''),source), external_name=$7, active=$8 where id=$1`,
		t.ID, t.Name, t.ParentID, t.LeadMemberID, t.IsBoundary, t.Source, t.ExternalName, !t.Inactive)
	return err
}

// DeleteTeam 删除团队：成员离开它，直接下级挪到 newParent 下（空 = 顶层），归口到它的目标与迭代改成不归口。
func (s *Store) DeleteTeam(ctx context.Context, q Querier, id, newParent string) error {
	if _, err := q.Exec(ctx, `delete from team_members where team_id=$1`, id); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `update teams set parent_id=nullif($2,'') where parent_id=$1`, id, newParent); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `update goals set team_id=null, version=version+1 where team_id=$1`, id); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `update sprints set team_id=null where team_id=$1`, id); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `delete from teams where id=$1`, id)
	return err
}

// TeamRefCounts 数归口到这个团队的目标数与迭代数（删除前的影响统计）。
func (s *Store) TeamRefCounts(ctx context.Context, q Querier, id string) (goals, sprints int, err error) {
	err = q.QueryRow(ctx, `select (select count(*) from goals where team_id=$1), (select count(*) from sprints where team_id=$1)`, id).Scan(&goals, &sprints)
	return
}

// TeamMembers 返回团队 → 成员 ID 列表。
func (s *Store) TeamMembers(ctx context.Context, q Querier) (map[string][]string, error) {
	rows, err := q.Query(ctx, `select team_id, member_id from team_members`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var t, m string
		if err := rows.Scan(&t, &m); err != nil {
			return nil, err
		}
		out[t] = append(out[t], m)
	}
	return out, rows.Err()
}

func (s *Store) CountMembers(ctx context.Context, q Querier) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from members where active`).Scan(&n)
	return n, err
}

// ---------- 团队 ----------

func (s *Store) CreateTeam(ctx context.Context, q Querier, t *domain.Team) error {
	if t.ID == "" {
		t.ID = NewID("team")
	}
	if t.Source == "" {
		t.Source = domain.SourceManual
	}
	_, err := q.Exec(ctx, `insert into teams(id,org_id,parent_id,name,lead_member_id,is_boundary,source,external_name,active) values($1,$2,nullif($3,''),$4,nullif($5,''),$6,$7,$8,$9)`,
		t.ID, t.OrgID, t.ParentID, t.Name, t.LeadMemberID, t.IsBoundary, t.Source, t.ExternalName, !t.Inactive)
	return err
}

func (s *Store) ListTeams(ctx context.Context, q Querier) ([]*domain.Team, error) {
	rows, err := q.Query(ctx, `select id,org_id,coalesce(parent_id,''),name,coalesce(lead_member_id,''),is_boundary,coalesce(source,'manual'),coalesce(external_name,''),coalesce(active,true) from teams order by name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Team
	for rows.Next() {
		t := &domain.Team{}
		var active bool
		if err := rows.Scan(&t.ID, &t.OrgID, &t.ParentID, &t.Name, &t.LeadMemberID, &t.IsBoundary, &t.Source, &t.ExternalName, &active); err != nil {
			return nil, err
		}
		t.Inactive = !active
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) AddTeamMember(ctx context.Context, q Querier, orgID, teamID, memberID string) error {
	_, err := q.Exec(ctx, `insert into team_members(org_id,team_id,member_id) values($1,$2,$3) on conflict do nothing`, orgID, teamID, memberID)
	return err
}

// TeamsOfMember 返回某成员所属的全部团队 ID。
func (s *Store) TeamsOfMember(ctx context.Context, q Querier, memberID string) ([]string, error) {
	rows, err := q.Query(ctx, `select team_id from team_members where member_id=$1`, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TeamMemberships 返回成员 → 全部所属团队 ID（按 team_id 排序，结果确定）。
func (s *Store) TeamMemberships(ctx context.Context, q Querier) (map[string][]string, error) {
	rows, err := q.Query(ctx, `select member_id, team_id from team_members order by member_id, team_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var m, t string
		if err := rows.Scan(&m, &t); err != nil {
			return nil, err
		}
		out[m] = append(out[m], t)
	}
	return out, rows.Err()
}

// TeamOfMembers 返回成员 → 团队 ID 的映射（多团队成员取 team_id 最小的那个，结果确定；归口请用 OrgIndex 的"最深团队"规则）。
func (s *Store) TeamOfMembers(ctx context.Context, q Querier) (map[string]string, error) {
	rows, err := q.Query(ctx, `select member_id, team_id from team_members order by member_id, team_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var m, t string
		if err := rows.Scan(&m, &t); err != nil {
			return nil, err
		}
		if _, ok := out[m]; !ok {
			out[m] = t
		}
	}
	return out, rows.Err()
}
