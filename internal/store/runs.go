package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

const runCols = `id,task_id,state,executor_id,started_at,ended_at,coalesce(outcome,''),last_heartbeat,usage,cost`

type RunRow struct {
	domain.Run
	Cost float64 `json:"cost"`
}

func scanRun(r interface{ Scan(...any) error }) (*RunRow, error) {
	rr := &RunRow{}
	var usage []byte
	var outcome string
	err := r.Scan(&rr.ID, &rr.TaskID, &rr.State, &rr.ExecutorID, &rr.StartedAt, &rr.EndedAt, &outcome, &rr.LastBeat, &usage, &rr.Cost)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rr.Outcome = domain.RunOutcome(outcome)
	rr.Usage = []domain.Usage{}
	_ = json.Unmarshal(usage, &rr.Usage)
	return rr, nil
}

func (s *Store) InsertRun(ctx context.Context, q Querier, orgID string, r *domain.Run) error {
	usage, _ := json.Marshal(r.Usage)
	if r.Usage == nil {
		usage = []byte("[]")
	}
	_, err := q.Exec(ctx, `insert into runs(id,org_id,task_id,state,executor_id,started_at,ended_at,outcome,last_heartbeat,usage) values($1,$2,$3,$4,$5,$6,$7,nullif($8,''),$9,$10)`,
		r.ID, orgID, r.TaskID, r.State, r.ExecutorID, r.StartedAt, r.EndedAt, string(r.Outcome), r.LastBeat, usage)
	return err
}

func (s *Store) UpdateRun(ctx context.Context, q Querier, r *domain.Run, cost float64) error {
	usage, _ := json.Marshal(r.Usage)
	_, err := q.Exec(ctx, `update runs set ended_at=$2,outcome=nullif($3,''),last_heartbeat=$4,usage=$5,cost=$6 where id=$1`, r.ID, r.EndedAt, string(r.Outcome), r.LastBeat, usage, cost)
	return err
}

func (s *Store) ActiveRun(ctx context.Context, q Querier, taskID string) (*domain.Run, error) {
	rr, err := scanRun(q.QueryRow(ctx, `select `+runCols+` from runs where task_id=$1 and ended_at is null`, taskID))
	if err == ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rr.Run, nil
}

func (s *Store) ActiveRunCount(ctx context.Context, q Querier, executorID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from runs where executor_id=$1 and ended_at is null`, executorID).Scan(&n)
	return n, err
}

// OpenRunCounts 返回每个执行者名下打开的执行记录数（Agent 状态用，一次查完不逐个数）。
func (s *Store) OpenRunCounts(ctx context.Context, q Querier) (map[string]int, error) {
	rows, err := q.Query(ctx, `select executor_id, count(*) from runs where ended_at is null group by executor_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (s *Store) RunsOfTask(ctx context.Context, q Querier, taskID string) ([]*RunRow, error) {
	rows, err := q.Query(ctx, `select `+runCols+` from runs where task_id=$1 order by started_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RunRow
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) AllRuns(ctx context.Context, q Querier) ([]*RunRow, error) {
	rows, err := q.Query(ctx, `select `+runCols+` from runs order by started_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RunRow
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StaleRuns 返回心跳超过 timeout 的、由 Agent 执行的进行中执行记录（人不打心跳，不受此限）。
func (s *Store) StaleRuns(ctx context.Context, q Querier, timeout time.Duration) ([]*RunRow, error) {
	rows, err := q.Query(ctx, `select `+runCols+` from runs where ended_at is null and last_heartbeat < $1 and executor_id in (select id from agents)`, time.Now().Add(-timeout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RunRow
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- 动态 ----------

func (s *Store) InsertEvents(ctx context.Context, q Querier, orgID string, events []domain.Event) error {
	for _, e := range events {
		data, _ := json.Marshal(e.Data)
		if e.Data == nil {
			data = []byte("{}")
		}
		if _, err := q.Exec(ctx, `insert into events(org_id,type,task_id,actor_id,at,data) values($1,$2,nullif($3,''),nullif($4,''),$5,$6)`, orgID, e.Type, e.TaskID, e.ActorID, e.At, data); err != nil {
			return err
		}
	}
	return nil
}

type EventRow struct {
	ID int64 `json:"id"`
	domain.Event
}

func (s *Store) ListEvents(ctx context.Context, q Querier, taskID string, limit int) ([]*EventRow, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows interface {
		Next() bool
		Scan(...any) error
		Close()
		Err() error
	}
	var err error
	if taskID != "" {
		rows, err = q.Query(ctx, `select id,type,coalesce(task_id,''),coalesce(actor_id,''),at,data from events where task_id=$1 order by id desc limit $2`, taskID, limit)
	} else {
		rows, err = q.Query(ctx, `select id,type,coalesce(task_id,''),coalesce(actor_id,''),at,data from events order by id desc limit $1`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EventRow
	for rows.Next() {
		e := &EventRow{}
		var data []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.TaskID, &e.ActorID, &e.At, &data); err != nil {
			return nil, err
		}
		e.Data = map[string]any{}
		_ = json.Unmarshal(data, &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListEventsByGoal 取挂在某个目标上的动态（目标创建、字段修改、里程碑、进展说明），按时间倒序。
// 目标类动态没有 task_id，目标在 data.goal_id 里。
func (s *Store) ListEventsByGoal(ctx context.Context, q Querier, goalID string, limit int) ([]*EventRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := q.Query(ctx, `select id,type,coalesce(task_id,''),coalesce(actor_id,''),at,data from events where data->>'goal_id'=$1 order by id desc limit $2`, goalID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EventRow
	for rows.Next() {
		e := &EventRow{}
		var data []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.TaskID, &e.ActorID, &e.At, &data); err != nil {
			return nil, err
		}
		e.Data = map[string]any{}
		_ = json.Unmarshal(data, &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------- 通知 ----------

func (s *Store) InsertNotification(ctx context.Context, q Querier, orgID string, n domain.Notification) error {
	_, err := q.Exec(ctx, `insert into notifications(org_id,member_id,title,body,task_id) values($1,$2,$3,$4,nullif($5,''))`, orgID, n.MemberID, n.Title, n.Body, n.TaskID)
	return err
}

func (s *Store) ListNotifications(ctx context.Context, q Querier, memberID string, limit int) ([]*domain.Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := q.Query(ctx, `select id,member_id,title,body,coalesce(task_id,''),read_at,created_at from notifications where member_id=$1 order by id desc limit $2`, memberID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Notification
	for rows.Next() {
		n := &domain.Notification{}
		if err := rows.Scan(&n.ID, &n.MemberID, &n.Title, &n.Body, &n.TaskID, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) MarkNotificationsRead(ctx context.Context, q Querier, memberID string) error {
	_, err := q.Exec(ctx, `update notifications set read_at=now() where member_id=$1 and read_at is null`, memberID)
	return err
}

// UnreadNotifications 只取某成员未读的通知（待我处理用）。
func (s *Store) UnreadNotifications(ctx context.Context, q Querier, memberID string, limit int) ([]*domain.Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := q.Query(ctx, `select id,member_id,title,body,coalesce(task_id,''),read_at,created_at from notifications where member_id=$1 and read_at is null order by id desc limit $2`, memberID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Notification
	for rows.Next() {
		n := &domain.Notification{}
		if err := rows.Scan(&n.ID, &n.MemberID, &n.Title, &n.Body, &n.TaskID, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// CountUnreadNotifications 统计某成员的未读通知数。
func (s *Store) CountUnreadNotifications(ctx context.Context, q Querier, memberID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from notifications where member_id=$1 and read_at is null`, memberID).Scan(&n)
	return n, err
}

// MarkNotificationsReadByIDs 把指定的几条标为已读；member_id 条件保证只能标自己的。
func (s *Store) MarkNotificationsReadByIDs(ctx context.Context, q Querier, memberID string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := q.Exec(ctx, `update notifications set read_at=now() where member_id=$1 and read_at is null and id = any($2)`, memberID, ids)
	return err
}

// ---------- 价格表 ----------

// PricesFor 返回组织覆盖优先、其次全局默认的价格表（按模型）。
func (s *Store) PricesFor(ctx context.Context, q Querier, orgID string) (map[string]domain.Price, error) {
	rows, err := q.Query(ctx, `select model_id,input_per_million,output_per_million,cache_read_per_million,cache_write_per_million,currency,org_id from price_book where org_id is null or org_id=$1 order by org_id nulls first, effective_from`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.Price{}
	for rows.Next() {
		var p domain.Price
		var org *string
		if err := rows.Scan(&p.ModelID, &p.InputPerMillion, &p.OutputPerMillion, &p.CacheReadPerMillion, &p.CacheWritePerMillion, &p.Currency, &org); err != nil {
			return nil, err
		}
		out[p.ModelID] = p // 后写覆盖：组织条目在全局之后
	}
	return out, rows.Err()
}

func (s *Store) UpsertGlobalPrice(ctx context.Context, q Querier, p domain.Price) error {
	var exists bool
	if err := q.QueryRow(ctx, `select exists(select 1 from price_book where org_id is null and model_id=$1)`, p.ModelID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err := q.Exec(ctx, `insert into price_book(org_id,model_id,input_per_million,output_per_million,cache_read_per_million,cache_write_per_million,currency) values(null,$1,$2,$3,$4,$5,$6)`,
		p.ModelID, p.InputPerMillion, p.OutputPerMillion, p.CacheReadPerMillion, p.CacheWritePerMillion, p.Currency)
	return err
}

func (s *Store) ExchangeRate(ctx context.Context, q Querier, from, to string) (float64, error) {
	if from == to {
		return 1, nil
	}
	var rate float64
	err := q.QueryRow(ctx, `select rate from exchange_rates where from_currency=$1 and to_currency=$2`, from, to).Scan(&rate)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return rate, err
}

func (s *Store) UpsertExchangeRate(ctx context.Context, q Querier, orgID, from, to string, rate float64) error {
	_, err := q.Exec(ctx, `insert into exchange_rates(org_id,from_currency,to_currency,rate) values($1,$2,$3,$4) on conflict (org_id,from_currency,to_currency) do update set rate=excluded.rate`, orgID, from, to, rate)
	return err
}
