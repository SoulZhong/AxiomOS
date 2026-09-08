package store

import (
	"context"
	"encoding/json"
	"time"
)

// IdempotencyTTL 是一个幂等键的有效期：24 小时内同一个键只生效一次（ADR 0025）。
const IdempotencyTTL = 24 * time.Hour

// IdempotencyLease 是"占住这个键但还没干完"的租约。租约之内说明真的有人在做，同键的后来者
// 只能等；超过租约说明上一次的进程已经死了，后来者接手继续做。
const IdempotencyLease = 60 * time.Second

// 幂等记录的两种状态。
const (
	IdemPending = "pending" // 已经占住这个键，活还在干
	IdemDone    = "done"    // 干完了，Result 是那一次的结果
)

// IdempotencyRecord 是一次写操作的占位或结果。
type IdempotencyRecord struct {
	ID         string          `json:"id"`
	OrgID      string          `json:"org_id"`
	ExecutorID string          `json:"executor_id"`
	Key        string          `json:"key"`
	Endpoint   string          `json:"endpoint"`
	ArgsHash   string          `json:"args_hash"`
	State      string          `json:"state"`
	ResultRef  string          `json:"result_ref"`
	Result     json.RawMessage `json:"result"`
	CreatedAt  time.Time       `json:"created_at"`
	StartedAt  time.Time       `json:"started_at"`
	ExpiresAt  time.Time       `json:"expires_at"`
}

// Fresh 判断这条占位还在租约里（真的有人在做），now 一般传 time.Now()。
func (r *IdempotencyRecord) Fresh(now time.Time) bool {
	return r.State == IdemPending && now.Sub(r.StartedAt) < IdempotencyLease
}

const idemCols = `id,org_id,executor_id,key,endpoint,args_hash,state,result_ref,result,created_at,started_at,expires_at`

func scanIdem(row interface{ Scan(...any) error }) (*IdempotencyRecord, error) {
	r := &IdempotencyRecord{}
	var result []byte
	err := row.Scan(&r.ID, &r.OrgID, &r.ExecutorID, &r.Key, &r.Endpoint, &r.ArgsHash, &r.State,
		&r.ResultRef, &result, &r.CreatedAt, &r.StartedAt, &r.ExpiresAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.Result = json.RawMessage(result)
	return r, nil
}

// IdempotencyRecordByKey 取一个执行者的某个键；没有或已过期都返回 ErrNotFound。
func (s *Store) IdempotencyRecordByKey(ctx context.Context, q Querier, executorID, key string) (*IdempotencyRecord, error) {
	return scanIdem(q.QueryRow(ctx, `select `+idemCols+` from idempotency_keys
        where executor_id=$1 and key=$2 and expires_at > now()`, executorID, key))
}

// ReserveIdempotencyKey 先占住一个键再干活（ADR 0025）。
//
// 返回 (占住了, 拦路的那条记录, 错误)：
//   - 占住了 = true：这个键归调用方了（新占的，或者接手了一条过期 / 租约已断的占位），可以真做。
//   - 占住了 = false：拦路的那条记录还有效——要么已经干完（state=done），要么别人正在干（租约内的 pending）。
//
// 并发安全靠两件事：插入用 on conflict do nothing 让先到的赢；没插进去时用 select ... for update
// 锁住那一行再判断，同一个键的两个请求于是排成队，不会同时认为"没人占"。
func (s *Store) ReserveIdempotencyKey(ctx context.Context, q Querier, r *IdempotencyRecord) (bool, *IdempotencyRecord, error) {
	now := time.Now()
	if r.ID == "" {
		r.ID = NewID("idm")
	}
	r.State = IdemPending
	r.CreatedAt, r.StartedAt = now, now
	r.ExpiresAt = now.Add(IdempotencyTTL)
	tag, err := q.Exec(ctx, `insert into idempotency_keys(id,org_id,executor_id,key,endpoint,args_hash,state,result_ref,result,created_at,started_at,expires_at)
        values($1,$2,$3,$4,$5,$6,'pending','','null',$7,$7,$8) on conflict (org_id,executor_id,key) do nothing`,
		r.ID, r.OrgID, r.ExecutorID, r.Key, r.Endpoint, r.ArgsHash, now, r.ExpiresAt)
	if err != nil {
		return false, nil, err
	}
	if tag.RowsAffected() > 0 {
		return true, nil, nil
	}
	// 没插进去：锁住已有的那一行，看它是干完了、正在干，还是已经作废可以接手。
	prev, err := scanIdem(q.QueryRow(ctx, `select `+idemCols+` from idempotency_keys
        where executor_id=$1 and key=$2 for update`, r.ExecutorID, r.Key))
	if err != nil {
		return false, nil, err
	}
	takeOver := !prev.ExpiresAt.After(now) || (prev.State == IdemPending && !prev.Fresh(now))
	if !takeOver {
		return false, prev, nil
	}
	// 上一次过期了或者半路死了：接手。参数不一样也照接——过期的键本来就可以重新用，
	// 而半路死了的那一次没有留下结果，拿旧参数比对没有意义。
	if _, err := q.Exec(ctx, `update idempotency_keys
        set id=$1, endpoint=$2, args_hash=$3, state='pending', result_ref='', result='null',
            created_at=$4, started_at=$4, expires_at=$5
        where org_id=$6 and executor_id=$7 and key=$8`,
		r.ID, r.Endpoint, r.ArgsHash, now, r.ExpiresAt, r.OrgID, r.ExecutorID, r.Key); err != nil {
		return false, nil, err
	}
	return true, nil, nil
}

// FinishIdempotencyKey 把占位改成"干完了"，连同这一次的结果。
// 只认自己占的那一行（按 id），免得接手之后把别人的记录改掉。
func (s *Store) FinishIdempotencyKey(ctx context.Context, q Querier, r *IdempotencyRecord) error {
	if len(r.Result) == 0 {
		r.Result = json.RawMessage("null")
	}
	_, err := q.Exec(ctx, `update idempotency_keys set state='done', result_ref=$2, result=$3 where id=$1`,
		r.ID, r.ResultRef, []byte(r.Result))
	return err
}

// ReleaseIdempotencyKey 把没干成的占位删掉：失败本来就该允许重试。
func (s *Store) ReleaseIdempotencyKey(ctx context.Context, q Querier, id string) error {
	_, err := q.Exec(ctx, `delete from idempotency_keys where id=$1 and state='pending'`, id)
	return err
}

// SweepIdempotencyKeys 删掉已经过期的键（由后台巡检调用），返回删掉的条数。
func (s *Store) SweepIdempotencyKeys(ctx context.Context, q Querier, now time.Time) (int, error) {
	tag, err := q.Exec(ctx, `delete from idempotency_keys where expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// CountIdempotencyKeys 返回当前组织里的键数（测试与自检用）。
func (s *Store) CountIdempotencyKeys(ctx context.Context, q Querier) (int, error) {
	n := 0
	err := q.QueryRow(ctx, `select count(*) from idempotency_keys`).Scan(&n)
	return n, err
}
