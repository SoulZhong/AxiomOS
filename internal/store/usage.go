package store

import (
	"context"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

// 用量自动采集（ADR 0030）：客户端按会话报累计数，这里存上一次看到的累计数与每次的增量。

// UsageSession 是某个 Agent 某个客户端会话里某个模型的累计用量（上一次报上来的）。
type UsageSession struct {
	AgentID    string
	SessionKey string
	Client     string
	ModelID    string
	Usage      domain.Usage
	UpdatedAt  time.Time
}

// UsageSessionsOf 读一个会话里各模型的累计数。
func (s *Store) UsageSessionsOf(ctx context.Context, q Querier, agentID, sessionKey string) (map[string]*UsageSession, error) {
	rows, err := q.Query(ctx, `select client,model_id,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,updated_at
		from agent_usage_sessions where agent_id=$1 and session_key=$2`, agentID, sessionKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*UsageSession{}
	for rows.Next() {
		x := &UsageSession{AgentID: agentID, SessionKey: sessionKey}
		if err := rows.Scan(&x.Client, &x.ModelID, &x.Usage.InputTokens, &x.Usage.OutputTokens, &x.Usage.CacheReadTokens, &x.Usage.CacheWriteTokens, &x.UpdatedAt); err != nil {
			return nil, err
		}
		x.Usage.ModelID = x.ModelID
		out[x.ModelID] = x
	}
	return out, rows.Err()
}

// UpsertUsageSession 记下某个模型最新的累计数。
func (s *Store) UpsertUsageSession(ctx context.Context, q Querier, orgID, agentID, sessionKey, client string, u domain.Usage) error {
	_, err := q.Exec(ctx, `insert into agent_usage_sessions(org_id,agent_id,session_key,client,model_id,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,updated_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
		on conflict (org_id, agent_id, session_key, model_id) do update set client=excluded.client, input_tokens=excluded.input_tokens, output_tokens=excluded.output_tokens,
		  cache_read_tokens=excluded.cache_read_tokens, cache_write_tokens=excluded.cache_write_tokens, updated_at=now()`,
		orgID, agentID, sessionKey, client, u.ModelID, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)
	return err
}

// UsageEntry 是一次增量：归口到哪段执行记录（可空）。
type UsageEntry struct {
	ID         string
	AgentID    string
	SessionKey string
	Client     string
	RunID      string
	TaskID     string
	Usage      domain.Usage
	At         time.Time
}

// InsertUsageEntry 记一条增量。
func (s *Store) InsertUsageEntry(ctx context.Context, q Querier, orgID string, e *UsageEntry) error {
	if e.ID == "" {
		e.ID = NewID("usg")
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	_, err := q.Exec(ctx, `insert into agent_usage_entries(id,org_id,agent_id,session_key,client,model_id,run_id,task_id,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,at)
		values($1,$2,$3,$4,$5,$6,nullif($7,''),nullif($8,''),$9,$10,$11,$12,$13)`,
		e.ID, orgID, e.AgentID, e.SessionKey, e.Client, e.Usage.ModelID, e.RunID, e.TaskID, e.Usage.InputTokens, e.Usage.OutputTokens, e.Usage.CacheReadTokens, e.Usage.CacheWriteTokens, e.At)
	return err
}

// UnattributedUsage 某个 Agent（agentID 为空则全组织）自 since 起没归口到执行记录的用量，按模型合计。
func (s *Store) UnattributedUsage(ctx context.Context, q Querier, agentID string, since time.Time) ([]domain.Usage, error) {
	rows, err := q.Query(ctx, `select model_id, sum(input_tokens), sum(output_tokens), sum(cache_read_tokens), sum(cache_write_tokens)
		from agent_usage_entries where run_id is null and ($1='' or agent_id=$1) and at >= $2 group by model_id order by model_id`, agentID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Usage
	for rows.Next() {
		var u domain.Usage
		if err := rows.Scan(&u.ModelID, &u.InputTokens, &u.OutputTokens, &u.CacheReadTokens, &u.CacheWriteTokens); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ActiveRunOfExecutor 某个执行者此刻开着的执行记录（几段同时开着时取最近有心跳的那段）。
func (s *Store) ActiveRunOfExecutor(ctx context.Context, q Querier, executorID string) (*RunRow, error) {
	return scanRun(q.QueryRow(ctx, `select `+runCols+` from runs where executor_id=$1 and ended_at is null order by last_heartbeat desc limit 1`, executorID))
}
