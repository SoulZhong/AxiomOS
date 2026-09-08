package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// 批量改时间桶（ADR 0021）：在路线图上把卡片从一栏拖到另一栏是最常见的动作，一次拖动就是一次请求。
// 每个目标单独判权限、单独记动态；改不了的跳过并给一句完整的中文理由，其余照做（与成员批量操作一致）。

// BulkGoalHorizonInput 是一次批量改时间桶。Horizon 为空表示放回「还没排期」。
type BulkGoalHorizonInput struct {
	IDs     []string           `json:"ids"`
	Horizon domain.GoalHorizon `json:"horizon"`
}

// BulkGoalHorizon 把一批目标放进同一个时间桶。
func (a *App) BulkGoalHorizon(ctx context.Context, sess *Session, in BulkGoalHorizonInput) (*BulkResult, error) {
	if !domain.ValidHorizon(in.Horizon) {
		return nil, goalPlanErr(domain.ValidateGoalPlan(in.Horizon, "", "", ""))
	}
	if len(in.IDs) == 0 {
		return nil, Bad("err.bulk_empty")
	}
	loc := sess.Loc()
	out := &BulkResult{Skipped: []BulkSkip{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		seen := map[string]bool{}
		for _, id := range in.IDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			g, err := a.Store.GoalByID(ctx, tx, id)
			if err != nil {
				out.Skipped = append(out.Skipped, BulkSkip{ID: id, Reason: i18n.Tr(loc, "err.goal_gone"), Code: "err.goal_gone"})
				continue
			}
			if !a.canEditGoal(ctx, tx, sess, g) {
				out.Skipped = append(out.Skipped, BulkSkip{ID: id, Name: g.Title,
					Reason: i18n.Tr(loc, "err.goal_edit_forbidden"), Code: "err.goal_edit_forbidden"})
				continue
			}
			if g.Horizon == in.Horizon {
				out.Skipped = append(out.Skipped, BulkSkip{ID: id, Name: g.Title,
					Reason: i18n.Trf(loc, "err.goal_horizon_same", g.Title, i18n.Tr(loc, horizonKey(in.Horizon))), Code: "err.goal_horizon_same"})
				continue
			}
			out.Updated++
			// 只看不做（ADR 0025）：权限、存在性、"本来就在这个桶里"都已经判完了，
			// 到写回之前停住，最后把两笔账（改几个、跳过几个）说成一句话。
			if sess.Write.DryRun {
				continue
			}
			from := g.Horizon
			g.Horizon = in.Horizon
			if err := a.Store.UpdateGoal(ctx, tx, g); err != nil {
				return err
			}
			c := fieldChange{Field: "horizon", From: nilIfEmpty(string(from)), To: nilIfEmpty(string(in.Horizon))}
			ev := c.event("GoalFieldChanged", sess, "", map[string]any{"goal_id": g.ID, "title": g.Title})
			ev.At = time.Now()
			if err := a.insertEvents(ctx, tx, sess, []domain.Event{ev}); err != nil {
				return err
			}
		}
		if sess.Write.DryRun {
			return dryRunList(sess, bulkGoalWill(i18n.M("will.goal.horizon_bulk", out.Updated,
				i18n.M(horizonKey(in.Horizon))), out.Updated, out.Skipped)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// horizonKey 是时间桶的词条键；空是「还没排期」。
func horizonKey(h domain.GoalHorizon) string {
	if h == "" {
		return "horizon.none"
	}
	return "horizon." + string(h)
}

// bulkGoalWill 把一次批量改动说成并列的两笔账：改得动的几个，改不动的按理由分几组。
// 一个都改不动时只说跳过的部分，不说「会把 0 个目标……」。
func bulkGoalWill(head i18n.Msg, updated int, skipped []BulkSkip) []i18n.Msg {
	var out []i18n.Msg
	if updated > 0 {
		out = append(out, head)
	}
	byCode := map[string]int{}
	for _, s := range skipped {
		byCode[s.Code]++
	}
	for _, c := range []struct{ code, key string }{
		{"err.goal_edit_forbidden", "will.bulk_skip_forbidden"},
		{"err.goal_gone", "will.bulk_skip_gone"},
		{"err.goal_horizon_same", "will.bulk_skip_same"},
	} {
		if n := byCode[c.code]; n > 0 {
			out = append(out, i18n.M(c.key, n))
		}
	}
	return out
}
