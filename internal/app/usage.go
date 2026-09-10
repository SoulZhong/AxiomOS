package app

// 用量自动采集（ADR 0030）：Agent 的客户端（Claude Code 的 Stop 钩子等）每轮结束把整个会话的累计用量报上来，
// 这里与上一次比较得出增量：那一刻这个 Agent 有开着的执行记录就走心跳归口到任务上，没有就记成「未归口用量」。
// 累计数幂等——同一份数报几次都只算一次；数字来自客户端自己记的账，不靠模型自报。

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// UsageReportInput 是一次上报：会话键（客户端自己的会话 id）、客户端名、按模型的累计用量。
type UsageReportInput struct {
	Session    string         `json:"session"`
	Client     string         `json:"client"`
	Cumulative []domain.Usage `json:"cumulative"`
}

// UsageReportResult 告诉客户端这次算出了多少增量、归到了哪个任务（没有就是未归口）。
type UsageReportResult struct {
	DeltaTokens  int64  `json:"delta_tokens"`
	TaskID       string `json:"task_id,omitempty"`
	TaskNumber   int    `json:"task_number,omitempty"`
	Unattributed bool   `json:"unattributed"`
}

const usageSessionKeyMax = 128

// ReportAgentUsage 处理一次上报（POST /me/usage，只有 Agent 能调）。
func (a *App) ReportAgentUsage(ctx context.Context, sess *Session, in UsageReportInput) (*UsageReportResult, error) {
	if !sess.IsAgent() {
		return nil, Forbidden("err.usage_agent_only")
	}
	key := strings.TrimSpace(in.Session)
	if key == "" || len(key) > usageSessionKeyMax {
		return nil, Bad("err.usage_session")
	}
	client := strings.ToLower(strings.TrimSpace(in.Client))
	if len(client) > 40 {
		client = client[:40]
	}
	if err := refuseDryRun(sess); err != nil {
		return nil, err
	}
	res := &UsageReportResult{}
	var deltas []domain.Usage
	var run *store.RunRow
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		prev, err := a.Store.UsageSessionsOf(ctx, tx, sess.AgentID, key)
		if err != nil {
			return err
		}
		for _, cum := range in.Cumulative {
			model := strings.TrimSpace(cum.ModelID)
			if model == "" {
				continue
			}
			cum.ModelID = model
			var old domain.Usage
			if p := prev[model]; p != nil {
				old = p.Usage
			}
			d := domain.Usage{ModelID: model,
				InputTokens: nonNeg(cum.InputTokens - old.InputTokens), OutputTokens: nonNeg(cum.OutputTokens - old.OutputTokens),
				CacheReadTokens: nonNeg(cum.CacheReadTokens - old.CacheReadTokens), CacheWriteTokens: nonNeg(cum.CacheWriteTokens - old.CacheWriteTokens)}
			if d.TotalTokens() == 0 {
				continue
			}
			// 累计数只增不减：客户端重发旧的（更小的）累计数时不倒扣
			merged := domain.Usage{ModelID: model, InputTokens: max64(cum.InputTokens, old.InputTokens), OutputTokens: max64(cum.OutputTokens, old.OutputTokens),
				CacheReadTokens: max64(cum.CacheReadTokens, old.CacheReadTokens), CacheWriteTokens: max64(cum.CacheWriteTokens, old.CacheWriteTokens)}
			if err := a.Store.UpsertUsageSession(ctx, tx, sess.OrgID, sess.AgentID, key, client, merged); err != nil {
				return err
			}
			deltas = append(deltas, d)
			res.DeltaTokens += d.TotalTokens()
		}
		if len(deltas) == 0 {
			return nil
		}
		if r, err := a.Store.ActiveRunOfExecutor(ctx, tx, sess.AgentID); err == nil {
			run = r
		} else if err != store.ErrNotFound {
			return err
		}
		for _, d := range deltas {
			e := &store.UsageEntry{AgentID: sess.AgentID, SessionKey: key, Client: client, Usage: d}
			if run != nil {
				e.RunID, e.TaskID = run.ID, run.TaskID
			}
			if err := a.Store.InsertUsageEntry(ctx, tx, sess.OrgID, e); err != nil {
				return err
			}
		}
		if run == nil {
			// 没有开着的执行记录：记在 Agent 名下，动态里留一笔（归口的那种由心跳记 UsageReported）
			res.Unattributed = true
			return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "AgentUsageRecorded", ActorID: sess.Actor.ID, At: time.Now(),
				Data: map[string]any{"session": key, "client": client, "tokens": res.DeltaTokens}}})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if run == nil {
		return res, nil
	}
	// 归口：把增量加到这段执行记录的累计数上，走心跳（内核取最大、算成本、记动态）
	cum := map[string]domain.Usage{}
	for _, u := range run.Usage {
		cum[u.ModelID] = u
	}
	for _, d := range deltas {
		u := cum[d.ModelID]
		u.ModelID = d.ModelID
		u.InputTokens += d.InputTokens
		u.OutputTokens += d.OutputTokens
		u.CacheReadTokens += d.CacheReadTokens
		u.CacheWriteTokens += d.CacheWriteTokens
		cum[d.ModelID] = u
	}
	usage := make([]domain.Usage, 0, len(cum))
	for _, u := range cum {
		usage = append(usage, u)
	}
	t, err := a.Heartbeat(ctx, sess, run.TaskID, usage)
	if err != nil {
		return nil, err
	}
	res.TaskID, res.TaskNumber = t.ID, t.Number
	return res, nil
}

func nonNeg(x int64) int64 {
	if x < 0 {
		return 0
	}
	return x
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// unattributedUsage 某个 Agent（空 = 全组织）自 since 起的未归口用量：按模型的 token 与折算成本。
func (a *App) unattributedUsage(ctx context.Context, tx pgx.Tx, orgID, agentID string, since time.Time) ([]domain.Usage, float64, error) {
	us, err := a.Store.UnattributedUsage(ctx, tx, agentID, since)
	if err != nil || len(us) == 0 {
		return us, 0, err
	}
	cost, err := a.costOfRun(ctx, tx, orgID, &domain.Run{Usage: us})
	return us, cost, err
}
