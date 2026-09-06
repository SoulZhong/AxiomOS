package store

import (
	"context"
	"crypto/rand"
	"time"
)

// ---------- Agent 设备码（ADR 0018） ----------
//
// 这张表在组织之外：申请时还不知道属于哪个组织，轮询时也没有会话。所以这里的方法都直接用 Pool，
// 不走 WithOrg；批准那一步在组织事务里调用也可以（表没有行级安全）。

// DeviceCode 是一条设备码申请。
type DeviceCode struct {
	ID           string
	UserCode     string
	Client       string
	Name         string
	Status       string // pending | approved | denied | expired
	OrgID        string
	AgentID      string
	ApprovedBy   string
	TokenEnc     []byte
	TokenTakenAt *time.Time
	LastPolledAt *time.Time
	DecidedAt    *time.Time
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

const deviceCols = `id,user_code,client,name,status,coalesce(org_id,''),coalesce(agent_id,''),coalesce(approved_by,''),token_enc,token_taken_at,last_polled_at,decided_at,expires_at,created_at`

func scanDevice(r interface{ Scan(...any) error }) (*DeviceCode, error) {
	d := &DeviceCode{}
	err := r.Scan(&d.ID, &d.UserCode, &d.Client, &d.Name, &d.Status, &d.OrgID, &d.AgentID, &d.ApprovedBy, &d.TokenEnc, &d.TokenTakenAt, &d.LastPolledAt, &d.DecidedAt, &d.ExpiresAt, &d.CreatedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// 验证码字母表：去掉容易看混的 0/O、1/I。
const userCodeLetters = "BCDFGHJKLMNPQRSTVWXZ"
const userCodeDigits = "23456789"

// NewUserCode 生成 ABCD-1234 形式的验证码。
func NewUserCode() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	out := make([]byte, 0, 9)
	for i := 0; i < 4; i++ {
		out = append(out, userCodeLetters[int(b[i])%len(userCodeLetters)])
	}
	out = append(out, '-')
	for i := 4; i < 8; i++ {
		out = append(out, userCodeDigits[int(b[i])%len(userCodeDigits)])
	}
	return string(out)
}

// CreateDeviceCode 记一条申请，返回明文设备码（只此一次）。验证码撞上已有的就换一个再试。
func (s *Store) CreateDeviceCode(ctx context.Context, q Querier, client, name string, ttl time.Duration) (*DeviceCode, string, error) {
	deviceCode := "dvc_" + NewID("")[1:] + NewID("")[1:]
	for attempt := 0; attempt < 5; attempt++ {
		d := &DeviceCode{ID: NewID("adc"), UserCode: NewUserCode(), Client: client, Name: name, Status: "pending", CreatedAt: time.Now()}
		d.ExpiresAt = d.CreatedAt.Add(ttl)
		_, err := q.Exec(ctx, `insert into agent_device_codes(id,device_code_hash,user_code,client,name,status,expires_at,created_at) values($1,$2,$3,$4,$5,$6,$7,$8)`,
			d.ID, HashToken(deviceCode), d.UserCode, d.Client, d.Name, d.Status, d.ExpiresAt, d.CreatedAt)
		if err == nil {
			return d, deviceCode, nil
		}
		if attempt == 4 {
			return nil, "", err
		}
	}
	return nil, "", ErrNotFound
}

// DeviceByUserCode 按验证码读。
func (s *Store) DeviceByUserCode(ctx context.Context, q Querier, userCode string) (*DeviceCode, error) {
	return scanDevice(q.QueryRow(ctx, `select `+deviceCols+` from agent_device_codes where user_code=$1`, userCode))
}

// DeviceByCode 按明文设备码读。
func (s *Store) DeviceByCode(ctx context.Context, q Querier, deviceCode string) (*DeviceCode, error) {
	return scanDevice(q.QueryRow(ctx, `select `+deviceCols+` from agent_device_codes where device_code_hash=$1`, HashToken(deviceCode)))
}

// DecideDevice 写批准 / 拒绝结果；批准时带上组织、Agent 与加密后的令牌。
func (s *Store) DecideDevice(ctx context.Context, q Querier, id, status, orgID, agentID, approvedBy string, tokenEnc []byte) error {
	_, err := q.Exec(ctx, `update agent_device_codes set status=$2, org_id=nullif($3,''), agent_id=nullif($4,''), approved_by=nullif($5,''), token_enc=$6, decided_at=now() where id=$1`,
		id, status, orgID, agentID, approvedBy, tokenEnc)
	return err
}

// TouchDevicePoll 记下这次轮询时间，同时就是限流：检查与更新合成一条语句，
// 只有距上次轮询超过 minInterval 的那一次才会改到行（返回真）；并发的多次轮询里只有一次能过。
func (s *Store) TouchDevicePoll(ctx context.Context, q Querier, id string, minInterval time.Duration) (bool, error) {
	tag, err := q.Exec(ctx, `update agent_device_codes set last_polled_at=now()
		where id=$1 and (last_polled_at is null or last_polled_at < now() - make_interval(secs => $2))`,
		id, minInterval.Seconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// TakeDeviceToken 领取令牌：一条带条件的原子更新，清掉密文并记下领取时间。
// 返回真表示这次调用抢到了令牌；并发的第二次轮询改不到行，返回假，不能再拿到令牌。
func (s *Store) TakeDeviceToken(ctx context.Context, q Querier, id string) (bool, error) {
	tag, err := q.Exec(ctx, `update agent_device_codes set token_enc=null, token_taken_at=now()
		where id=$1 and token_enc is not null`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ExpireDeviceCodes 把过期的待批准申请标为过期，并清理一天前的旧记录；返回本次标为过期的条数。
func (s *Store) ExpireDeviceCodes(ctx context.Context, q Querier, now time.Time) (int, error) {
	tag, err := q.Exec(ctx, `update agent_device_codes set status='expired', token_enc=null where status='pending' and expires_at < $1`, now)
	if err != nil {
		return 0, err
	}
	// 已批准但一直没来取令牌的，也不能让密文一直躺着：过期后清掉
	if _, err := q.Exec(ctx, `update agent_device_codes set token_enc=null where token_enc is not null and expires_at < $1`, now); err != nil {
		return 0, err
	}
	if _, err := q.Exec(ctx, `delete from agent_device_codes where created_at < $1`, now.Add(-24*time.Hour)); err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
