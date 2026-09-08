package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/i18n"
)

// 手工排序（ADR 0022）：在时间线的一条泳道里把目标上下拖一次，就是一次 PUT /goals/rank。
// 前端把这条泳道里的目标按拖完之后的顺序整串发过来，服务端给它们重新发一遍等间距的排序权重，
// 免得前端自己算插入值、算着算着挤到一起。
//
// 排序权重不记动态：它是次序不是表态，一次拖动会动好几个目标，记下来只会把动态刷满。
// 每个目标单独判权限，改不了的跳过并给一句完整的中文理由（与批量改时间桶一致）。

// RankStep 是相邻两个目标之间的间距。第 i 个（从 0 数）拿到 (i+1)*RankStep。
const RankStep = 1000.0

// GoalRankInput 是一次泳道重排：IDs 就是拖完之后从上到下的顺序。
type GoalRankInput struct {
	IDs []string `json:"ids"`
}

// RankedGoal 是重排后的一个目标与它的新排序权重。
type RankedGoal struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Rank  float64 `json:"rank"`
}

// GoalRankResult 是重排的结果：改了几个、跳过了谁、以及这一串目标各自最新的排序权重。
type GoalRankResult struct {
	Updated int          `json:"updated"`
	Skipped []BulkSkip   `json:"skipped"`
	Goals   []RankedGoal `json:"goals"`
}

// SetGoalRanks 按给定顺序给一串目标发等间距的排序权重。
// Updated 只数真的变了的；Goals 列出全部改得动的目标（含权重本来就对的），前端照它更新本地次序。
func (a *App) SetGoalRanks(ctx context.Context, sess *Session, in GoalRankInput) (*GoalRankResult, error) {
	return idempotent(ctx, a, sess, "rank_goals", in, func() (*GoalRankResult, error) {
		return a.setGoalRanks(ctx, sess, in)
	})
}

func (a *App) setGoalRanks(ctx context.Context, sess *Session, in GoalRankInput) (*GoalRankResult, error) {
	if len(in.IDs) == 0 {
		return nil, Bad("err.bulk_empty")
	}
	if err := a.goalBatchGate(ctx, sess, ActionGoalRank, in.IDs, in, func(n int) i18n.Msg { return i18n.M("proposal.summary.goal.rank", n) }); err != nil {
		return nil, err
	}
	loc := sess.Loc()
	out := &GoalRankResult{Skipped: []BulkSkip{}, Goals: []RankedGoal{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		seen := map[string]bool{}
		pos := 0
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
			// 排不动的目标不占位置：剩下的照旧首尾相接，中间不留空档
			pos++
			rank := float64(pos) * RankStep
			if g.Rank == nil || *g.Rank != rank {
				out.Updated++
				// 只看不做（ADR 0025）：次序算完了，写回之前停住。
				if !sess.Write.DryRun {
					g.Rank = &rank
					if err := a.Store.UpdateGoal(ctx, tx, g); err != nil {
						return err
					}
				}
			}
			out.Goals = append(out.Goals, RankedGoal{ID: g.ID, Title: g.Title, Rank: rank})
		}
		if sess.Write.DryRun {
			return dryRunList(sess, bulkGoalWill(i18n.M("will.goal.rank_bulk", out.Updated), out.Updated, out.Skipped)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
