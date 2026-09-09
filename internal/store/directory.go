package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

// ---------- 外部目录配置（ADR 0017） ----------

// DirectoryConfig 是一个组织的外部目录配置。Credentials 是非保密的凭据字段（明文），
// SecretsEnc 是全部保密字段合成一个 JSON 后加密的密文；字段的键由提供方声明（ADR 0017 补记）。
// LegacyAppID / LegacyAppSecretEnc 是 0009 期的飞书专属列，只在懒升级时读一次，写回后清空。
type DirectoryConfig struct {
	OrgID            string
	Provider         string
	Credentials      map[string]string
	SecretsEnc       []byte
	RootDepartmentID string
	// RootDepartmentIDs 是同步根部门列表（ADR 0017 补记二）：空表示整个企业；多个时各自作为顶层团队。
	// RootDepartmentID 始终等于列表第一个（列表为空时是提供方的根），兼容旧读法。
	RootDepartmentIDs []string
	DefaultRole       string
	Schedule          string
	ProxyURL          string
	UpdatedAt         time.Time

	LegacyAppID        string
	LegacyAppSecretEnc []byte
}

// NeedsUpgrade 为真表示这一行还是旧格式：新列为空、旧列有值。
func (c *DirectoryConfig) NeedsUpgrade() bool {
	return len(c.Credentials) == 0 && len(c.SecretsEnc) == 0 && (c.LegacyAppID != "" || len(c.LegacyAppSecretEnc) > 0)
}

func (s *Store) DirectoryConfig(ctx context.Context, q Querier, orgID string) (*DirectoryConfig, error) {
	c := &DirectoryConfig{Credentials: map[string]string{}, RootDepartmentIDs: []string{}}
	var creds []byte
	err := q.QueryRow(ctx, `select org_id,provider,credentials,secrets_enc,root_department_id,root_department_ids,default_role,schedule,proxy_url,updated_at,app_id,app_secret_enc from org_directory where org_id=$1`, orgID).
		Scan(&c.OrgID, &c.Provider, &creds, &c.SecretsEnc, &c.RootDepartmentID, &c.RootDepartmentIDs, &c.DefaultRole, &c.Schedule, &c.ProxyURL, &c.UpdatedAt, &c.LegacyAppID, &c.LegacyAppSecretEnc)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(creds, &c.Credentials)
	if c.Credentials == nil {
		c.Credentials = map[string]string{}
	}
	if c.RootDepartmentIDs == nil {
		c.RootDepartmentIDs = []string{}
	}
	return c, nil
}

// UpsertDirectoryConfig 写配置；旧列一并清空（升级完成）。
func (s *Store) UpsertDirectoryConfig(ctx context.Context, q Querier, c *DirectoryConfig) error {
	c.UpdatedAt = time.Now()
	if c.Credentials == nil {
		c.Credentials = map[string]string{}
	}
	if c.RootDepartmentIDs == nil {
		c.RootDepartmentIDs = []string{}
	}
	creds, _ := json.Marshal(c.Credentials)
	_, err := q.Exec(ctx, `insert into org_directory(org_id,provider,credentials,secrets_enc,root_department_id,root_department_ids,default_role,schedule,proxy_url,updated_at,app_id,app_secret_enc)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'',null)
		on conflict (org_id) do update set provider=excluded.provider, credentials=excluded.credentials, secrets_enc=excluded.secrets_enc,
		  root_department_id=excluded.root_department_id, root_department_ids=excluded.root_department_ids, default_role=excluded.default_role, schedule=excluded.schedule, proxy_url=excluded.proxy_url, updated_at=excluded.updated_at,
		  app_id='', app_secret_enc=null`,
		c.OrgID, c.Provider, creds, c.SecretsEnc, c.RootDepartmentID, c.RootDepartmentIDs, c.DefaultRole, c.Schedule, c.ProxyURL, c.UpdatedAt)
	if err == nil {
		c.LegacyAppID, c.LegacyAppSecretEnc = "", nil
	}
	return err
}

// DeleteDirectoryConfig 删除配置（回到未配置状态；外部身份与运行记录保留）。
func (s *Store) DeleteDirectoryConfig(ctx context.Context, q Querier, orgID string) error {
	_, err := q.Exec(ctx, `delete from org_directory where org_id=$1`, orgID)
	return err
}

// ScheduledDirectories 列出开了定时同步的组织（不受行级安全限制，巡检用）。
func (s *Store) ScheduledDirectories(ctx context.Context, q Querier) (map[string]string, error) {
	rows, err := q.Query(ctx, `select d.org_id, d.schedule from org_directory d join organizations o on o.id=d.org_id where o.deactivated_at is null and d.schedule in ('hourly','daily')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, sch string
		if err := rows.Scan(&id, &sch); err != nil {
			return nil, err
		}
		out[id] = sch
	}
	return out, rows.Err()
}

// ---------- 外部身份 ----------

// ExternalIdentity 是一个成员或团队在外部目录里的编号。
type ExternalIdentity struct {
	ID         string
	OrgID      string
	Provider   string
	Kind       string // member | team
	ExternalID string
	LocalID    string
}

const (
	IdentityMember = "member"
	IdentityTeam   = "team"
)

// ListExternalIdentities 列出某提供方的全部外部身份。
func (s *Store) ListExternalIdentities(ctx context.Context, q Querier, provider string) ([]ExternalIdentity, error) {
	return s.listIdentities(ctx, q, `where provider=$1`, provider)
}

// ListForeignIdentities 列出不属于当前提供方的外部身份（切换提供方后的旧来源，ADR 0017 补记）。
func (s *Store) ListForeignIdentities(ctx context.Context, q Querier, provider string) ([]ExternalIdentity, error) {
	return s.listIdentities(ctx, q, `where provider<>$1`, provider)
}

func (s *Store) listIdentities(ctx context.Context, q Querier, where string, args ...any) ([]ExternalIdentity, error) {
	rows, err := q.Query(ctx, `select id,org_id,provider,kind,external_id,local_id from external_identities `+where+` order by created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExternalIdentity
	for rows.Next() {
		var x ExternalIdentity
		if err := rows.Scan(&x.ID, &x.OrgID, &x.Provider, &x.Kind, &x.ExternalID, &x.LocalID); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) UpsertExternalIdentity(ctx context.Context, q Querier, x ExternalIdentity) error {
	if x.ID == "" {
		x.ID = NewID("xid")
	}
	_, err := q.Exec(ctx, `insert into external_identities(id,org_id,provider,kind,external_id,local_id) values($1,$2,$3,$4,$5,$6)
		on conflict (org_id,provider,kind,external_id) do update set local_id=excluded.local_id`, x.ID, x.OrgID, x.Provider, x.Kind, x.ExternalID, x.LocalID)
	return err
}

// ---------- 同步运行记录 ----------

// DirectoryRun 是一次同步。
type DirectoryRun struct {
	ID         string                 `json:"id"`
	OrgID      string                 `json:"org_id"`
	Provider   string                 `json:"provider"`
	StartedAt  time.Time              `json:"started_at"`
	FinishedAt *time.Time             `json:"finished_at"`
	Status     string                 `json:"status"` // ok | partial | failed
	Counts     domain.DirectoryCounts `json:"counts"`
	Errors     []string               `json:"errors"`
}

func (s *Store) InsertDirectoryRun(ctx context.Context, q Querier, r *DirectoryRun) error {
	if r.ID == "" {
		r.ID = NewID("dsr")
	}
	if r.Errors == nil {
		r.Errors = []string{}
	}
	counts, _ := json.Marshal(r.Counts)
	errs, _ := json.Marshal(r.Errors)
	_, err := q.Exec(ctx, `insert into directory_runs(id,org_id,provider,started_at,finished_at,status,counts,errors) values($1,$2,$3,$4,$5,$6,$7,$8)`,
		r.ID, r.OrgID, r.Provider, r.StartedAt, r.FinishedAt, r.Status, counts, errs)
	return err
}

func scanDirRun(r interface{ Scan(...any) error }) (*DirectoryRun, error) {
	x := &DirectoryRun{}
	var counts, errs []byte
	err := r.Scan(&x.ID, &x.OrgID, &x.Provider, &x.StartedAt, &x.FinishedAt, &x.Status, &counts, &errs)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(counts, &x.Counts)
	_ = json.Unmarshal(errs, &x.Errors)
	if x.Errors == nil {
		x.Errors = []string{}
	}
	return x, nil
}

const dirRunCols = `id,org_id,provider,started_at,finished_at,status,counts,errors`

func (s *Store) ListDirectoryRuns(ctx context.Context, q Querier, limit int) ([]*DirectoryRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := q.Query(ctx, `select `+dirRunCols+` from directory_runs order by started_at desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DirectoryRun
	for rows.Next() {
		r, err := scanDirRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) LastDirectoryRun(ctx context.Context, q Querier) (*DirectoryRun, error) {
	return scanDirRun(q.QueryRow(ctx, `select `+dirRunCols+` from directory_runs order by started_at desc limit 1`))
}

// ---------- 团队与成员的同步用写法 ----------

// SetMemberTeams 让成员恰好属于这些团队（同步用：一个人可能在多个部门）。
func (s *Store) SetMemberTeams(ctx context.Context, q Querier, orgID, memberID string, teamIDs []string) error {
	if _, err := q.Exec(ctx, `delete from team_members where member_id=$1`, memberID); err != nil {
		return err
	}
	for _, t := range teamIDs {
		if err := s.AddTeamMember(ctx, q, orgID, t, memberID); err != nil {
			return err
		}
	}
	return nil
}

// CreateMemberFull 按完整结构建成员（来源、激活状态由调用方指定）。
func (s *Store) CreateMemberFull(ctx context.Context, q Querier, m *domain.Member) error {
	if m.ID == "" {
		m.ID = NewID("mem")
	}
	if m.Roles == nil {
		m.Roles = []string{}
	}
	if m.Source == "" {
		m.Source = domain.SourceManual
	}
	if m.Status == "" {
		m.Status = domain.MemberActive
	}
	m.CreatedAt = time.Now()
	_, err := q.Exec(ctx, `insert into members(id,org_id,account_id,name,roles,active,created_at,source,status) values($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		m.ID, m.OrgID, m.AccountID, m.Name, m.Roles, m.Active, m.CreatedAt, m.Source, m.Status)
	return err
}

// DeletePendingMember 删掉一个从未激活的成员及其团队关系（作废邀请时用）。账号保留：没有密码，不能登录。
func (s *Store) DeletePendingMember(ctx context.Context, q Querier, memberID string) error {
	if _, err := q.Exec(ctx, `delete from team_members where member_id=$1`, memberID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `delete from members where id=$1 and status='pending_activation' and active`, memberID)
	return err
}

// ---------- 外部身份：解绑与合并时的搬迁 ----------

// IdentityRow 是带创建时间的外部身份（对应关系面板要显示「自何时」）。
type IdentityRow struct {
	ExternalIdentity
	CreatedAt time.Time
}

// ListExternalIdentityRows 列出某提供方的全部外部身份，带创建时间。
func (s *Store) ListExternalIdentityRows(ctx context.Context, q Querier, provider string) ([]IdentityRow, error) {
	rows, err := q.Query(ctx, `select id,org_id,provider,kind,external_id,local_id,created_at from external_identities where provider=$1 order by created_at`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IdentityRow
	for rows.Next() {
		var x IdentityRow
		if err := rows.Scan(&x.ID, &x.OrgID, &x.Provider, &x.Kind, &x.ExternalID, &x.LocalID, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// DeleteExternalIdentity 删一条外部身份（解绑）。返回是否真的删了一条。
func (s *Store) DeleteExternalIdentity(ctx context.Context, q Querier, provider, kind, externalID string) (bool, error) {
	tag, err := q.Exec(ctx, `delete from external_identities where provider=$1 and kind=$2 and external_id=$3`, provider, kind, externalID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RepointIdentities 把指向 from 的外部身份（所有提供方）改指向 to（合并用）。
// 同一提供方下 to 已经有别的身份时不会冲突：唯一键是 (org, provider, kind, external_id)，external_id 不变。
func (s *Store) RepointIdentities(ctx context.Context, q Querier, kind, from, to string) error {
	if _, err := q.Exec(ctx, `update external_identities set local_id=$3 where kind=$1 and local_id=$2`, kind, from, to); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `update directory_decisions set local_id=$3 where kind=$1 and local_id=$2`, kind, from, to)
	return err
}

// ---------- 冲突决定（ADR 0017 补记四） ----------

// DirectoryDecision 是人对一个外部对象的决定。
type DirectoryDecision struct {
	OrgID        string
	Provider     string
	Kind         string // member | team
	ExternalID   string
	ExternalName string
	Decision     string // merge | create | skip
	LocalID      string
	DecidedBy    string
	DecidedAt    time.Time
}

// ListDirectoryDecisions 列出某提供方的全部决定。
func (s *Store) ListDirectoryDecisions(ctx context.Context, q Querier, provider string) ([]DirectoryDecision, error) {
	rows, err := q.Query(ctx, `select org_id,provider,kind,external_id,external_name,decision,coalesce(local_id,''),decided_by,decided_at from directory_decisions where provider=$1 order by decided_at`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DirectoryDecision
	for rows.Next() {
		var d DirectoryDecision
		if err := rows.Scan(&d.OrgID, &d.Provider, &d.Kind, &d.ExternalID, &d.ExternalName, &d.Decision, &d.LocalID, &d.DecidedBy, &d.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpsertDirectoryDecision 写一条决定（同一外部对象只保留最后一次）。
func (s *Store) UpsertDirectoryDecision(ctx context.Context, q Querier, d *DirectoryDecision) error {
	d.DecidedAt = time.Now()
	var local *string
	if d.LocalID != "" {
		local = &d.LocalID
	}
	_, err := q.Exec(ctx, `insert into directory_decisions(id,org_id,provider,kind,external_id,external_name,decision,local_id,decided_by,decided_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		on conflict (org_id,provider,kind,external_id) do update set external_name=case when excluded.external_name='' then directory_decisions.external_name else excluded.external_name end,
		  decision=excluded.decision, local_id=excluded.local_id, decided_by=excluded.decided_by, decided_at=excluded.decided_at`,
		NewID("ddc"), d.OrgID, d.Provider, d.Kind, d.ExternalID, d.ExternalName, d.Decision, local, d.DecidedBy, d.DecidedAt)
	return err
}

// DeleteDirectoryDecision 删一条决定（重新考虑）。返回是否真的删了。
func (s *Store) DeleteDirectoryDecision(ctx context.Context, q Querier, provider, kind, externalID string) (bool, error) {
	tag, err := q.Exec(ctx, `delete from directory_decisions where provider=$1 and kind=$2 and external_id=$3`, provider, kind, externalID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ---------- 合并（ADR 0017 补记四） ----------

// MergeTeamRefs 把团队 from 上的一切搬到 to：直属成员、目标、迭代、下级团队。任务经目标归口，不用单独搬。
// 不改 from 自身的状态（停用与动态由应用层做）。
func (s *Store) MergeTeamRefs(ctx context.Context, q Querier, orgID, from, to string) error {
	stmts := []struct {
		sql  string
		args []any
	}{
		{`insert into team_members(org_id,team_id,member_id) select $1,$3,member_id from team_members where team_id=$2 on conflict do nothing`, []any{orgID, from, to}},
		{`delete from team_members where team_id=$1`, []any{from}},
		{`update goals set team_id=$2, updated_at=now(), version=version+1 where team_id=$1`, []any{from, to}},
		{`update sprints set team_id=$2, updated_at=now() where team_id=$1`, []any{from, to}},
		{`update teams set parent_id=$2 where parent_id=$1 and id<>$2`, []any{from, to}},
	}
	for _, st := range stmts {
		if _, err := q.Exec(ctx, st.sql, st.args...); err != nil {
			return err
		}
	}
	return nil
}

// MergeMemberRefs 把成员 from 身上"现在归谁"的引用改到 to：任务负责人 / 验收人 / 参与角色、目标负责人、Agent 所有者、
// 团队负责人、团队归属、未读通知。创建者、评论、交付物、动态、执行记录是历史，不改。
func (s *Store) MergeMemberRefs(ctx context.Context, q Querier, orgID, from, to string) error {
	stmts := []struct {
		sql  string
		args []any
	}{
		{`update tasks set assignee_id=$2, updated_at=now(), version=version+1 where assignee_id=$1`, []any{from, to}},
		{`update tasks set reviewer_id=$2, updated_at=now(), version=version+1 where reviewer_id=$1`, []any{from, to}},
		{`update tasks set participants=(select coalesce(jsonb_object_agg(key, case when value=to_jsonb($1::text) then to_jsonb($2::text) else value end), '{}'::jsonb) from jsonb_each(participants)), updated_at=now(), version=version+1 where exists (select 1 from jsonb_each_text(participants) e where e.value=$1)`, []any{from, to}},
		{`update goals set owner_member_id=$2, updated_at=now(), version=version+1 where owner_member_id=$1`, []any{from, to}},
		{`update agents set owner_member_id=$2 where owner_member_id=$1`, []any{from, to}},
		{`update teams set lead_member_id=$2 where lead_member_id=$1`, []any{from, to}},
		{`insert into team_members(org_id,team_id,member_id) select $1,team_id,$3 from team_members where member_id=$2 on conflict do nothing`, []any{orgID, from, to}},
		{`delete from team_members where member_id=$1`, []any{from}},
		{`update notifications set member_id=$2 where member_id=$1 and read_at is null`, []any{from, to}},
	}
	for _, st := range stmts {
		if _, err := q.Exec(ctx, st.sql, st.args...); err != nil {
			return err
		}
	}
	return nil
}
