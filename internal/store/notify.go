package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

// ---------- 通知外发（ADR 0019） ----------

// NotifyChannel 是组织的一个通道配置：非保密字段明文放 Config，保密字段合成 JSON 加密放 SecretsEnc。
type NotifyChannel struct {
	OrgID      string
	Channel    string
	Enabled    bool
	Config     map[string]string
	SecretsEnc []byte
	UpdatedAt  time.Time
}

// ListNotifyChannels 读组织全部通道配置（没配过的通道没有行）。
func (s *Store) ListNotifyChannels(ctx context.Context, q Querier) (map[string]*NotifyChannel, error) {
	rows, err := q.Query(ctx, `select org_id,channel,enabled,config,secrets_enc,updated_at from notification_channels`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*NotifyChannel{}
	for rows.Next() {
		c := &NotifyChannel{Config: map[string]string{}}
		var cfg []byte
		if err := rows.Scan(&c.OrgID, &c.Channel, &c.Enabled, &cfg, &c.SecretsEnc, &c.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(cfg, &c.Config)
		if c.Config == nil {
			c.Config = map[string]string{}
		}
		out[c.Channel] = c
	}
	return out, rows.Err()
}

// UpsertNotifyChannel 写一条通道配置。
func (s *Store) UpsertNotifyChannel(ctx context.Context, q Querier, c *NotifyChannel) error {
	if c.Config == nil {
		c.Config = map[string]string{}
	}
	cfg, _ := json.Marshal(c.Config)
	_, err := q.Exec(ctx, `insert into notification_channels(org_id,channel,enabled,config,secrets_enc,updated_at) values($1,$2,$3,$4,$5,now())
		on conflict (org_id,channel) do update set enabled=excluded.enabled, config=excluded.config, secrets_enc=excluded.secrets_enc, updated_at=now()`,
		c.OrgID, c.Channel, c.Enabled, cfg, c.SecretsEnc)
	return err
}

// NotifyPolicy 读组织允许外发的事件类型；没有记录时 ok 为假（视为全部允许）。
func (s *Store) NotifyPolicy(ctx context.Context, q Querier) (kinds []string, ok bool, err error) {
	err = q.QueryRow(ctx, `select allowed_kinds from notification_policies`).Scan(&kinds)
	if isNoRows(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if kinds == nil {
		kinds = []string{}
	}
	return kinds, true, nil
}

// PutNotifyPolicy 写组织允许的事件类型。
func (s *Store) PutNotifyPolicy(ctx context.Context, q Querier, orgID string, kinds []string) error {
	if kinds == nil {
		kinds = []string{}
	}
	_, err := q.Exec(ctx, `insert into notification_policies(org_id,allowed_kinds,updated_at) values($1,$2,now())
		on conflict (org_id) do update set allowed_kinds=excluded.allowed_kinds, updated_at=now()`, orgID, kinds)
	return err
}

// 个人通知偏好复用 preferences 表：subject_kind = 'notify'。
const notifyPrefKind = "notify"

// NotifyPrefs 读一个成员的通知偏好；没设过返回 ErrNotFound。
func (s *Store) NotifyPrefs(ctx context.Context, q Querier, memberID string) (*domain.NotifyPrefs, error) {
	var data []byte
	err := q.QueryRow(ctx, `select data from preferences where subject_kind=$1 and subject_id=$2`, notifyPrefKind, memberID).Scan(&data)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p := &domain.NotifyPrefs{}
	_ = json.Unmarshal(data, p)
	return p, nil
}

// PutNotifyPrefs 写入或覆盖一个成员的通知偏好。
func (s *Store) PutNotifyPrefs(ctx context.Context, q Querier, orgID, memberID string, p domain.NotifyPrefs) error {
	b, _ := json.Marshal(p)
	_, err := q.Exec(ctx, `insert into preferences(org_id,subject_kind,subject_id,data,updated_at) values($1,$2,$3,$4,now())
		on conflict (org_id,subject_kind,subject_id) do update set data=excluded.data, updated_at=now()`, orgID, notifyPrefKind, memberID, b)
	return err
}

// DeleteNotifyPrefs 清掉一个成员的通知偏好。
func (s *Store) DeleteNotifyPrefs(ctx context.Context, q Querier, memberID string) error {
	_, err := q.Exec(ctx, `delete from preferences where subject_kind=$1 and subject_id=$2`, notifyPrefKind, memberID)
	return err
}

// ---------- 投递记录 ----------

// 投递状态。
const (
	DeliveryQueued  = "queued"
	DeliverySent    = "sent"
	DeliveryFailed  = "failed"
	DeliverySkipped = "skipped"
)

// Delivery 是一次外发。
type Delivery struct {
	ID             int64
	OrgID          string
	ItemID         string
	NotificationID *int64
	Kind           string
	MemberID       string
	Channel        string
	Status         string
	Title          string
	Body           string
	URL            string
	Error          string
	Attempts       int
	NextAt         time.Time
	CreatedAt      time.Time
	SentAt         *time.Time
}

const deliveryCols = `id,org_id,item_id,notification_id,kind,member_id,channel,status,title,body,url,error,attempts,next_at,created_at,sent_at`

func scanDelivery(r interface{ Scan(...any) error }) (*Delivery, error) {
	d := &Delivery{}
	err := r.Scan(&d.ID, &d.OrgID, &d.ItemID, &d.NotificationID, &d.Kind, &d.MemberID, &d.Channel, &d.Status, &d.Title, &d.Body, &d.URL, &d.Error, &d.Attempts, &d.NextAt, &d.CreatedAt, &d.SentAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// EnqueueDelivery 排一条投递；同一事项、同一收件人、同一通道已有记录时不重复（返回 false）。
func (s *Store) EnqueueDelivery(ctx context.Context, q Querier, d *Delivery) (bool, error) {
	if d.Status == "" {
		d.Status = DeliveryQueued
	}
	if d.NextAt.IsZero() {
		d.NextAt = time.Now()
	}
	err := q.QueryRow(ctx, `insert into notification_deliveries(org_id,item_id,notification_id,kind,member_id,channel,status,title,body,url,error,next_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) on conflict (org_id,item_id,member_id,channel) do nothing returning id,created_at`,
		d.OrgID, d.ItemID, d.NotificationID, d.Kind, d.MemberID, d.Channel, d.Status, d.Title, d.Body, d.URL, d.Error, d.NextAt).Scan(&d.ID, &d.CreatedAt)
	if isNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

// DueDeliveries 取到点的排队投递（跨组织，巡检用；不走行级安全）。返回按组织分组前的平铺列表。
func (s *Store) DueDeliveries(ctx context.Context, q Querier, now time.Time, limit int) ([]*Delivery, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := q.Query(ctx, `select `+deliveryCols+` from notification_deliveries where status=$1 and next_at<=$2 order by next_at, id limit $3`, DeliveryQueued, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Delivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpdateDelivery 写回状态、错误、次数、下次时间、发送时间。
func (s *Store) UpdateDelivery(ctx context.Context, q Querier, d *Delivery) error {
	_, err := q.Exec(ctx, `update notification_deliveries set status=$2, error=$3, attempts=$4, next_at=$5, sent_at=$6 where id=$1`,
		d.ID, d.Status, d.Error, d.Attempts, d.NextAt, d.SentAt)
	return err
}

// DeliveryFilter 是列投递记录的条件。
type DeliveryFilter struct {
	MemberID string
	Channel  string
	Status   string
	Limit    int
}

// ListDeliveries 列最近的投递（组织内，按时间倒序）。
func (s *Store) ListDeliveries(ctx context.Context, q Querier, f DeliveryFilter) ([]*Delivery, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	rows, err := q.Query(ctx, `select `+deliveryCols+` from notification_deliveries
		where ($1='' or member_id=$1) and ($2='' or channel=$2) and ($3='' or status=$3) order by id desc limit $4`, f.MemberID, f.Channel, f.Status, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Delivery{}
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeliveryByID 读一条投递。
func (s *Store) DeliveryByID(ctx context.Context, q Querier, id int64) (*Delivery, error) {
	return scanDelivery(q.QueryRow(ctx, `select `+deliveryCols+` from notification_deliveries where id=$1`, id))
}

// ChannelHealth 是一个通道最近的投递情况：连续失败次数（从最近一次往前数，遇到成功为止）与最近一次错误原话。
type ChannelHealth struct {
	Streak    int
	LastError string
	LastAt    *time.Time
}

// DeliveryHealth 按通道统计最近 50 条已结束的投递（sent / failed），算连续失败次数。
func (s *Store) DeliveryHealth(ctx context.Context, q Querier) (map[string]*ChannelHealth, error) {
	rows, err := q.Query(ctx, `select channel,status,error,coalesce(sent_at,created_at) from (
		select channel,status,error,sent_at,created_at,id, row_number() over (partition by channel order by id desc) as rn
		from notification_deliveries where status in ('sent','failed')) x where rn<=50 order by channel, id desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*ChannelHealth{}
	closed := map[string]bool{}
	for rows.Next() {
		var ch, status, errText string
		var at time.Time
		if err := rows.Scan(&ch, &status, &errText, &at); err != nil {
			return nil, err
		}
		h := out[ch]
		if h == nil {
			h = &ChannelHealth{}
			out[ch] = h
		}
		if h.LastAt == nil {
			t := at
			h.LastAt = &t
		}
		if status == DeliveryFailed {
			if h.LastError == "" {
				h.LastError = errText
			}
			if !closed[ch] {
				h.Streak++
			}
		} else {
			closed[ch] = true
		}
	}
	return out, rows.Err()
}

// InsertNotificationID 插入站内通知并返回它的编号（外发投递要挂在它上面）。
func (s *Store) InsertNotificationID(ctx context.Context, q Querier, orgID string, n domain.Notification) (int64, error) {
	var id int64
	err := q.QueryRow(ctx, `insert into notifications(org_id,member_id,title,body,task_id) values($1,$2,$3,$4,nullif($5,'')) returning id`, orgID, n.MemberID, n.Title, n.Body, n.TaskID).Scan(&id)
	return id, err
}

// MemberExternalID 读某成员在某提供方的外部编号；没绑定返回空串。
func (s *Store) MemberExternalID(ctx context.Context, q Querier, provider, memberID string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `select external_id from external_identities where provider=$1 and kind='member' and local_id=$2 order by created_at limit 1`, provider, memberID).Scan(&id)
	if isNoRows(err) {
		return "", nil
	}
	return id, err
}
