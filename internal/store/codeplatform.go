package store

import (
	"context"
	"encoding/json"
	"time"
)

// 代码平台与外部链接（ADR 0020）的存储。形状与外部目录（directory.go）一致：
// 非保密凭据明文放 credentials，保密字段合成一个 JSON 加密放 secrets_enc。

// CodePlatformConfig 是一个组织的代码平台配置。
type CodePlatformConfig struct {
	OrgID            string
	Provider         string
	Credentials      map[string]string
	SecretsEnc       []byte
	WebhookSecretEnc []byte
	ProxyURL         string
	UpdatedAt        time.Time
}

func (s *Store) CodePlatformConfig(ctx context.Context, q Querier, orgID string) (*CodePlatformConfig, error) {
	c := &CodePlatformConfig{Credentials: map[string]string{}}
	var creds []byte
	err := q.QueryRow(ctx, `select org_id,provider,credentials,secrets_enc,webhook_secret_enc,proxy_url,updated_at from org_code_platform where org_id=$1`, orgID).
		Scan(&c.OrgID, &c.Provider, &creds, &c.SecretsEnc, &c.WebhookSecretEnc, &c.ProxyURL, &c.UpdatedAt)
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
	return c, nil
}

// UpsertCodePlatformConfig 写配置。
func (s *Store) UpsertCodePlatformConfig(ctx context.Context, q Querier, c *CodePlatformConfig) error {
	c.UpdatedAt = time.Now()
	if c.Credentials == nil {
		c.Credentials = map[string]string{}
	}
	creds, _ := json.Marshal(c.Credentials)
	_, err := q.Exec(ctx, `insert into org_code_platform(org_id,provider,credentials,secrets_enc,webhook_secret_enc,proxy_url,updated_at)
		values($1,$2,$3,$4,$5,$6,$7)
		on conflict (org_id) do update set provider=excluded.provider, credentials=excluded.credentials, secrets_enc=excluded.secrets_enc,
		  webhook_secret_enc=excluded.webhook_secret_enc, proxy_url=excluded.proxy_url, updated_at=excluded.updated_at`,
		c.OrgID, c.Provider, creds, c.SecretsEnc, c.WebhookSecretEnc, c.ProxyURL, c.UpdatedAt)
	return err
}

// DeleteCodePlatformConfig 删除配置与仓库选择（回到未配置状态；外部链接保留）。
func (s *Store) DeleteCodePlatformConfig(ctx context.Context, q Querier, orgID string) error {
	if _, err := q.Exec(ctx, `delete from code_repos where org_id=$1`, orgID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `delete from org_code_platform where org_id=$1`, orgID)
	return err
}

// CodeRepo 是一个被接进来的仓库。
type CodeRepo struct {
	OrgID     string
	Provider  string
	RepoID    string
	FullName  string
	Enabled   bool
	HookOK    bool
	HookError string
}

// ListCodeRepos 列出组织已选的仓库。
func (s *Store) ListCodeRepos(ctx context.Context, q Querier, orgID, provider string) ([]CodeRepo, error) {
	rows, err := q.Query(ctx, `select org_id,provider,repo_id,full_name,enabled,hook_ok,hook_error from code_repos where org_id=$1 and provider=$2 order by full_name`, orgID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CodeRepo{}
	for rows.Next() {
		var r CodeRepo
		if err := rows.Scan(&r.OrgID, &r.Provider, &r.RepoID, &r.FullName, &r.Enabled, &r.HookOK, &r.HookError); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertCodeRepo 写一条仓库选择。
func (s *Store) UpsertCodeRepo(ctx context.Context, q Querier, r CodeRepo) error {
	_, err := q.Exec(ctx, `insert into code_repos(org_id,provider,repo_id,full_name,enabled,hook_ok,hook_error,updated_at)
		values($1,$2,$3,$4,$5,$6,$7,now())
		on conflict (org_id,provider,repo_id) do update set full_name=excluded.full_name, enabled=excluded.enabled,
		  hook_ok=excluded.hook_ok, hook_error=excluded.hook_error, updated_at=now()`,
		r.OrgID, r.Provider, r.RepoID, r.FullName, r.Enabled, r.HookOK, r.HookError)
	return err
}

// DeleteCodeReposExcept 删掉不在这批编号里的仓库选择（整体替换）。
func (s *Store) DeleteCodeReposExcept(ctx context.Context, q Querier, orgID, provider string, keep []string) error {
	if keep == nil {
		keep = []string{}
	}
	_, err := q.Exec(ctx, `delete from code_repos where org_id=$1 and provider=$2 and not (repo_id = any($3))`, orgID, provider, keep)
	return err
}

// ---------- 外部链接 ----------

// ExternalLink 是任务上挂的一条外部链接。
type ExternalLink struct {
	ID         string    `json:"id"`
	OrgID      string    `json:"-"`
	TaskID     string    `json:"task_id"`
	Provider   string    `json:"provider"`
	Kind       string    `json:"kind"`
	URL        string    `json:"url"`
	Title      string    `json:"title"`
	ExternalID string    `json:"external_id"`
	Status     string    `json:"status"`
	ActorName  string    `json:"actor_name"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const linkCols = `id,org_id,task_id,provider,kind,url,title,external_id,status,actor_name,created_by,created_at,updated_at`

func scanLink(r interface{ Scan(...any) error }) (*ExternalLink, error) {
	x := &ExternalLink{}
	err := r.Scan(&x.ID, &x.OrgID, &x.TaskID, &x.Provider, &x.Kind, &x.URL, &x.Title, &x.ExternalID, &x.Status, &x.ActorName, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return x, err
}

// LinksOfTask 列出一个任务的外部链接（建立顺序）。
func (s *Store) LinksOfTask(ctx context.Context, q Querier, taskID string) ([]*ExternalLink, error) {
	return s.queryLinks(ctx, q, `where task_id=$1 order by created_at, id`, taskID)
}

// LinksOfTasks 一次读一批任务的外部链接（列表页的 PR 小标用）。
func (s *Store) LinksOfTasks(ctx context.Context, q Querier, taskIDs []string) (map[string][]*ExternalLink, error) {
	out := map[string][]*ExternalLink{}
	if len(taskIDs) == 0 {
		return out, nil
	}
	ls, err := s.queryLinks(ctx, q, `where task_id = any($1) order by created_at, id`, taskIDs)
	if err != nil {
		return nil, err
	}
	for _, l := range ls {
		out[l.TaskID] = append(out[l.TaskID], l)
	}
	return out, nil
}

// LinksByExternalID 按平台上的唯一编号找链接（一个 PR 可能挂在多个任务上）。
func (s *Store) LinksByExternalID(ctx context.Context, q Querier, externalID string) ([]*ExternalLink, error) {
	return s.queryLinks(ctx, q, `where external_id=$1 and external_id<>'' order by created_at`, externalID)
}

// LinkByID 读一条链接。
func (s *Store) LinkByID(ctx context.Context, q Querier, id string) (*ExternalLink, error) {
	return scanLink(q.QueryRow(ctx, `select `+linkCols+` from external_links where id=$1`, id))
}

func (s *Store) queryLinks(ctx context.Context, q Querier, where string, args ...any) ([]*ExternalLink, error) {
	rows, err := q.Query(ctx, `select `+linkCols+` from external_links `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ExternalLink{}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// UpsertLink 写一条外部链接：同一任务同一地址只有一条，重复时更新标题、状态与操作者。
// 返回这次是新建（true）还是更新（false）。
func (s *Store) UpsertLink(ctx context.Context, q Querier, l *ExternalLink) (bool, error) {
	if l.ID == "" {
		l.ID = NewID("lnk")
	}
	if l.Kind == "" {
		l.Kind = "other"
	}
	var id string
	err := q.QueryRow(ctx, `insert into external_links(`+linkCols+`) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now(),now())
		on conflict (org_id,task_id,url) do update set kind=excluded.kind,
		  title=case when excluded.title='' then external_links.title else excluded.title end,
		  external_id=case when excluded.external_id='' then external_links.external_id else excluded.external_id end,
		  status=case when excluded.status='' then external_links.status else excluded.status end,
		  actor_name=case when excluded.actor_name='' then external_links.actor_name else excluded.actor_name end,
		  updated_at=now()
		returning id`,
		l.ID, l.OrgID, l.TaskID, l.Provider, l.Kind, l.URL, l.Title, l.ExternalID, l.Status, l.ActorName, l.CreatedBy).Scan(&id)
	if err != nil {
		return false, err
	}
	created := id == l.ID
	l.ID = id
	return created, nil
}

// DeleteLink 删一条外部链接。返回是否真的删了。
func (s *Store) DeleteLink(ctx context.Context, q Querier, taskID, id string) (bool, error) {
	tag, err := q.Exec(ctx, `delete from external_links where id=$1 and task_id=$2`, id, taskID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ---------- webhook 去重 ----------

// SeenWebhook 记下一次投递；已经记过时返回 true（重发，不再处理）。
func (s *Store) SeenWebhook(ctx context.Context, q Querier, orgID, provider, deliveryID string) (bool, error) {
	if deliveryID == "" {
		return false, nil
	}
	tag, err := q.Exec(ctx, `insert into webhook_events(org_id,provider,delivery_id) values($1,$2,$3) on conflict do nothing`, orgID, provider, deliveryID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return true, nil
	}
	// 顺手清理一个月前的记录，表不会无限长
	_, _ = q.Exec(ctx, `delete from webhook_events where org_id=$1 and received_at < now() - interval '30 days'`, orgID)
	return false, nil
}

// ReleaseWebhook 撤销一次占位：处理过程中出错时调用，好让代码平台重投时能重新处理。
// 处理本身是幂等的（链接按 URL 去重，迁移在状态已经走过之后不会再匹配），重投不会重复生效。
func (s *Store) ReleaseWebhook(ctx context.Context, q Querier, orgID, provider, deliveryID string) error {
	if deliveryID == "" {
		return nil
	}
	_, err := q.Exec(ctx, `delete from webhook_events where org_id=$1 and provider=$2 and delivery_id=$3`, orgID, provider, deliveryID)
	return err
}
