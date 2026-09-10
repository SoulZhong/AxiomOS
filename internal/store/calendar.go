package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

// 外部日历与日程（ADR 0032）。

// CalendarConfig 是一家提供方在组织里的连接。
type CalendarConfig struct {
	OrgID       string
	Provider    string
	Credentials map[string]string
	SecretsEnc  []byte
	ProxyURL    string
	Enabled     bool
	LastSyncAt  *time.Time
	LastStatus  string // "" | ok | failed
	LastError   string
	UpdatedAt   time.Time
}

const calendarCols = `org_id,provider,credentials,secrets_enc,proxy_url,enabled,last_sync_at,last_status,last_error,updated_at`

func scanCalendar(r interface{ Scan(...any) error }) (*CalendarConfig, error) {
	c := &CalendarConfig{Credentials: map[string]string{}}
	var creds []byte
	err := r.Scan(&c.OrgID, &c.Provider, &creds, &c.SecretsEnc, &c.ProxyURL, &c.Enabled, &c.LastSyncAt, &c.LastStatus, &c.LastError, &c.UpdatedAt)
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

// CalendarConfig 读一家提供方的连接。
func (s *Store) CalendarConfig(ctx context.Context, q Querier, provider string) (*CalendarConfig, error) {
	return scanCalendar(q.QueryRow(ctx, `select `+calendarCols+` from org_calendars where provider=$1`, provider))
}

// ListCalendarConfigs 列出组织里全部连接（按提供方名排序）。
func (s *Store) ListCalendarConfigs(ctx context.Context, q Querier) ([]*CalendarConfig, error) {
	rows, err := q.Query(ctx, `select `+calendarCols+` from org_calendars order by provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CalendarConfig
	for rows.Next() {
		c, err := scanCalendar(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpsertCalendarConfig 写连接（不动同步状态）。
func (s *Store) UpsertCalendarConfig(ctx context.Context, q Querier, c *CalendarConfig) error {
	c.UpdatedAt = time.Now()
	if c.Credentials == nil {
		c.Credentials = map[string]string{}
	}
	creds, _ := json.Marshal(c.Credentials)
	_, err := q.Exec(ctx, `insert into org_calendars(org_id,provider,credentials,secrets_enc,proxy_url,enabled,updated_at)
		values($1,$2,$3,$4,$5,$6,$7)
		on conflict (org_id, provider) do update set credentials=excluded.credentials, secrets_enc=excluded.secrets_enc,
		  proxy_url=excluded.proxy_url, enabled=excluded.enabled, updated_at=excluded.updated_at`,
		c.OrgID, c.Provider, creds, c.SecretsEnc, c.ProxyURL, c.Enabled, c.UpdatedAt)
	return err
}

// MarkCalendarSync 记最近一次同步的结果。
func (s *Store) MarkCalendarSync(ctx context.Context, q Querier, provider, status, errText string, at time.Time) error {
	_, err := q.Exec(ctx, `update org_calendars set last_sync_at=$2, last_status=$3, last_error=$4 where provider=$1`, provider, at, status, errText)
	return err
}

// DeleteCalendarConfig 断开一家提供方：连接、成员绑定与同步来的事件一起删。
func (s *Store) DeleteCalendarConfig(ctx context.Context, q Querier, provider string) error {
	for _, stmt := range []string{
		`delete from calendar_events where provider=$1`,
		`delete from calendar_identities where provider=$1`,
		`delete from org_calendars where provider=$1`,
	} {
		if _, err := q.Exec(ctx, stmt, provider); err != nil {
			return err
		}
	}
	return nil
}

// DeleteCalendarConfigIfUnused 收掉自助提供方自动建的连接行：一条语句判「没有任何成员还绑着」，并发时不会误删别人刚存的绑定。
func (s *Store) DeleteCalendarConfigIfUnused(ctx context.Context, q Querier, provider string) error {
	_, err := q.Exec(ctx, `delete from org_calendars where provider=$1 and not exists (select 1 from calendar_identities where provider=$1)`, provider)
	return err
}

// ScheduledCalendars 列出全部启用了外部日历的组织（跨组织，绕过行级安全，后台巡检用）。
func (s *Store) ScheduledCalendars(ctx context.Context, q Querier) (map[string][]string, error) {
	rows, err := q.Query(ctx, `select c.org_id, c.provider from org_calendars c join organizations o on o.id=c.org_id where o.deactivated_at is null and c.enabled`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var org, prov string
		if err := rows.Scan(&org, &prov); err != nil {
			return nil, err
		}
		out[org] = append(out[org], prov)
	}
	return out, rows.Err()
}

// ---------- 成员绑定 ----------

// CalendarIdentity 是成员与外部日历账号的绑定。
type CalendarIdentity struct {
	ID             string
	OrgID          string
	Provider       string
	MemberID       string
	ExternalUserID string
	Email          string
	SecretsEnc     []byte
	ConnectedAt    time.Time
	// Label 是外部日历的名字（订阅链接文件里写的）；Last* 是这个成员最近一次同步的结果
	Label      string
	LastSyncAt *time.Time
	LastStatus string
	LastError  string
}

const calendarIdentityCols = `id,org_id,provider,member_id,external_user_id,email,secrets_enc,connected_at,label,last_sync_at,last_status,last_error`

func scanCalendarIdentity(r interface{ Scan(...any) error }) (*CalendarIdentity, error) {
	x := &CalendarIdentity{}
	err := r.Scan(&x.ID, &x.OrgID, &x.Provider, &x.MemberID, &x.ExternalUserID, &x.Email, &x.SecretsEnc, &x.ConnectedAt, &x.Label, &x.LastSyncAt, &x.LastStatus, &x.LastError)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return x, nil
}

// UpsertCalendarIdentity 写一条绑定（同一成员同一提供方只有一条）。
func (s *Store) UpsertCalendarIdentity(ctx context.Context, q Querier, x *CalendarIdentity) error {
	if x.ID == "" {
		x.ID = NewID("cid")
	}
	x.ConnectedAt = time.Now()
	_, err := q.Exec(ctx, `insert into calendar_identities(id,org_id,provider,member_id,external_user_id,email,secrets_enc,connected_at,label)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9)
		on conflict (org_id, provider, member_id) do update set external_user_id=excluded.external_user_id, email=excluded.email,
		  secrets_enc=excluded.secrets_enc, connected_at=excluded.connected_at, label=excluded.label, last_sync_at=null, last_status='', last_error=''`,
		x.ID, x.OrgID, x.Provider, x.MemberID, x.ExternalUserID, x.Email, x.SecretsEnc, x.ConnectedAt, x.Label)
	return err
}

// MarkCalendarIdentitySync 记下某个成员这次同步的结果（没有绑定行的成员——飞书 / 企业微信按目录身份对上的——不记）。
// label 非空时顺手更新日历名（这次拉取看到的）。
func (s *Store) MarkCalendarIdentitySync(ctx context.Context, q Querier, provider, memberID, status, errText, label string, at time.Time) error {
	_, err := q.Exec(ctx, `update calendar_identities set last_sync_at=$3, last_status=$4, last_error=$5, label=case when $6<>'' then $6 else label end where provider=$1 and member_id=$2`, provider, memberID, at, status, errText, label)
	return err
}

// ---------- 对外订阅源 ----------

// CalendarFeed 是成员对外发布的订阅源：token 是链接里的密钥。
type CalendarFeed struct {
	Token     string
	OrgID     string
	MemberID  string
	CreatedAt time.Time
}

func scanCalendarFeed(r interface{ Scan(...any) error }) (*CalendarFeed, error) {
	x := &CalendarFeed{}
	err := r.Scan(&x.Token, &x.OrgID, &x.MemberID, &x.CreatedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return x, nil
}

// CalendarFeedOf 读某个成员的订阅源。
func (s *Store) CalendarFeedOf(ctx context.Context, q Querier, memberID string) (*CalendarFeed, error) {
	return scanCalendarFeed(q.QueryRow(ctx, `select token,org_id,member_id,created_at from calendar_feeds where member_id=$1`, memberID))
}

// CalendarFeedByToken 按密钥找订阅源（公开接口用，跨组织查，调用方拿 Pool）。
func (s *Store) CalendarFeedByToken(ctx context.Context, q Querier, token string) (*CalendarFeed, error) {
	return scanCalendarFeed(q.QueryRow(ctx, `select f.token,f.org_id,f.member_id,f.created_at from calendar_feeds f join organizations o on o.id=f.org_id
		where f.token=$1 and o.deactivated_at is null`, token))
}

// SetCalendarFeed 给成员建或换订阅源（换就是换密钥）。
func (s *Store) SetCalendarFeed(ctx context.Context, q Querier, orgID, memberID, token string) error {
	_, err := q.Exec(ctx, `insert into calendar_feeds(token,org_id,member_id,created_at) values($1,$2,$3,now())
		on conflict (org_id, member_id) do update set token=excluded.token, created_at=excluded.created_at`, token, orgID, memberID)
	return err
}

// DeleteCalendarFeed 停用成员的订阅源。
func (s *Store) DeleteCalendarFeed(ctx context.Context, q Querier, memberID string) error {
	_, err := q.Exec(ctx, `delete from calendar_feeds where member_id=$1`, memberID)
	return err
}

// CalendarIdentityOf 读某个成员在某家提供方的绑定。
func (s *Store) CalendarIdentityOf(ctx context.Context, q Querier, provider, memberID string) (*CalendarIdentity, error) {
	return scanCalendarIdentity(q.QueryRow(ctx, `select `+calendarIdentityCols+` from calendar_identities where provider=$1 and member_id=$2`, provider, memberID))
}

// ListCalendarIdentities 列出某家提供方的全部绑定。
func (s *Store) ListCalendarIdentities(ctx context.Context, q Querier, provider string) ([]*CalendarIdentity, error) {
	rows, err := q.Query(ctx, `select `+calendarIdentityCols+` from calendar_identities where provider=$1 order by connected_at`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CalendarIdentity
	for rows.Next() {
		x, err := scanCalendarIdentity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// DeleteCalendarIdentity 解除绑定，并删掉这个成员从这家提供方同步来的事件。
func (s *Store) DeleteCalendarIdentity(ctx context.Context, q Querier, provider, memberID string) error {
	if _, err := q.Exec(ctx, `delete from calendar_events where provider=$1 and member_id=$2`, provider, memberID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `delete from calendar_identities where provider=$1 and member_id=$2`, provider, memberID)
	return err
}

// ---------- 事件副本 ----------

// CalendarEventRow 是同步来的一条会议。
type CalendarEventRow struct {
	ID         string
	OrgID      string
	Provider   string
	MemberID   string
	ExternalID string
	Title      string
	StartsAt   time.Time
	EndsAt     time.Time
	AllDay     bool
	Busy       bool
	URL        string
	UpdatedAt  time.Time
}

const calendarEventCols = `id,org_id,provider,member_id,external_id,title,starts_at,ends_at,all_day,busy,url,updated_at`

// ReplaceCalendarEvents 用这次拉到的事件替换某个成员在区间内的副本：区间内不再返回的删掉，其余幂等写入。
func (s *Store) ReplaceCalendarEvents(ctx context.Context, q Querier, orgID, provider, memberID string, from, to time.Time, events []CalendarEventRow) error {
	keep := make([]string, 0, len(events))
	for _, e := range events {
		keep = append(keep, e.ExternalID)
	}
	if _, err := q.Exec(ctx, `delete from calendar_events where provider=$1 and member_id=$2 and ends_at > $3 and starts_at < $4 and not (external_id = any($5))`,
		provider, memberID, from, to, keep); err != nil {
		return err
	}
	now := time.Now()
	for _, e := range events {
		if e.ID == "" {
			e.ID = NewID("cev")
		}
		if _, err := q.Exec(ctx, `insert into calendar_events(id,org_id,provider,member_id,external_id,title,starts_at,ends_at,all_day,busy,url,updated_at)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			on conflict (org_id, provider, member_id, external_id) do update set title=excluded.title, starts_at=excluded.starts_at, ends_at=excluded.ends_at,
			  all_day=excluded.all_day, busy=excluded.busy, url=excluded.url, updated_at=excluded.updated_at`,
			e.ID, orgID, provider, memberID, e.ExternalID, e.Title, e.StartsAt, e.EndsAt, e.AllDay, e.Busy, e.URL, now); err != nil {
			return err
		}
	}
	return nil
}

// CalendarEventsOf 读若干成员在区间内的会议副本（按开始时间）。
func (s *Store) CalendarEventsOf(ctx context.Context, q Querier, memberIDs []string, from, to time.Time) ([]*CalendarEventRow, error) {
	if len(memberIDs) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `select `+calendarEventCols+` from calendar_events where member_id = any($1) and ends_at > $2 and starts_at < $3 order by starts_at, ends_at`, memberIDs, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CalendarEventRow
	for rows.Next() {
		e := &CalendarEventRow{}
		if err := rows.Scan(&e.ID, &e.OrgID, &e.Provider, &e.MemberID, &e.ExternalID, &e.Title, &e.StartsAt, &e.EndsAt, &e.AllDay, &e.Busy, &e.URL, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------- 日程要的区间查询 ----------

// TasksInRange 读负责人属于这些执行者、且计划起止或截止落在区间内的任务（含已结束的，界面按状态画）。
func (s *Store) TasksInRange(ctx context.Context, q Querier, executors []string, from, to time.Time) ([]*domain.Task, error) {
	if len(executors) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `select `+taskCols+` from tasks where assignee_id = any($1)
		and ((planned_start is not null and planned_end is not null and planned_end >= $2::date and planned_start < $3::date)
		  or (planned_start is not null and planned_end is null and planned_start >= $2::date and planned_start < $3::date)
		  or (planned_start is null and planned_end is not null and planned_end >= $2::date and planned_end < $3::date))
		order by coalesce(planned_start, planned_end), priority`, executors, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GoalsInRange 读负责人属于这些成员、或归口到这些团队的、计划起止落在区间内的目标。
func (s *Store) GoalsInRange(ctx context.Context, q Querier, owners, teams []string, from, to time.Time) ([]*domain.Goal, error) {
	if len(owners) == 0 && len(teams) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `select `+goalCols+` from goals where (owner_member_id = any($1) or team_id = any($4))
		and planned_start is not null and planned_end is not null and planned_end >= $2::date and planned_start < $3::date
		order by planned_start`, owners, from, to, teams)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Goal
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// MilestonesInRange 读这些成员负责的、或归口到这些团队的目标下，到期日落在区间内的里程碑。
func (s *Store) MilestonesInRange(ctx context.Context, q Querier, owners, teams []string, from, to time.Time) ([]*domain.Milestone, error) {
	if len(owners) == 0 && len(teams) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `select `+milestoneCols+` from milestones m where m.goal_id in (select id from goals where owner_member_id = any($1) or team_id = any($4))
		and m.due_on >= $2::date and m.due_on < $3::date order by m.due_on`, owners, from, to, teams)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Milestone
	for rows.Next() {
		m, err := scanMilestone(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
