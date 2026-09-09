package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// 验收（Agent 能力补齐，2026-09-08）：Agent 作为验收人时不必自己猜步骤名。review_task 收一个
// 决定（通过 / 打回）、一句说明与核对过的交付物清单，由这里按步骤自己的声明挑出那一步，
// 再走与人和 transition_task 完全相同的内核路径：授权（「验收」）、待确认、只看不做、幂等键都一样。

// ReviewInput 是一次验收的输入。
type ReviewInput struct {
	Decision            string   `json:"decision"`                       // accept | reject
	Comment             string   `json:"comment"`                        // 说明；打回时必填
	CheckedDeliverables []string `json:"checked_deliverables,omitempty"` // 核对过的交付物：类型代码名或标题，必须真的挂在任务上
}

// ReviewTask 通过或打回一个任务。整个验收（挑步骤、核对交付物、推进）套在一个幂等键下：
// 同一个键重发一次拿到的是第一次的结果，不会因为任务已经不在等验收而报错。
func (a *App) ReviewTask(ctx context.Context, sess *Session, taskID string, in ReviewInput) (*domain.Task, error) {
	return idempotent(ctx, a, sess, "review_task", map[string]any{"task_id": taskID, "input": in}, func() (*domain.Task, error) {
		// 里面那一步推进不再重复占键：键已经在这一层占过了
		inner := sess.WithWrite(WriteOptions{DryRun: sess.Write.DryRun})
		return a.reviewTask(ctx, inner, taskID, in)
	})
}

func (a *App) reviewTask(ctx context.Context, sess *Session, taskID string, in ReviewInput) (*domain.Task, error) {
	decision := strings.ToLower(strings.TrimSpace(in.Decision))
	if decision != "accept" && decision != "reject" {
		return nil, Bad("err.review_decision", in.Decision)
	}
	comment := strings.TrimSpace(in.Comment)
	// 打回必须有人写的说明——「已核对交付物」那一行是系统拼的，不算说明
	if decision == "reject" && comment == "" {
		return nil, &UserError{Status: 409, Reasons: []i18n.Msg{i18n.M("reject.need_comment")}}
	}
	var step string
	var checked []string
	if err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.loadContext(ctx, tx, sess, taskID)
		if err != nil {
			return err
		}
		step = domain.ReviewStep(c, decision == "accept")
		if step == "" {
			return Bad("err.review_no_step", taskRefText(c.Task))
		}
		for _, d := range in.CheckedDeliverables {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			title := ""
			for _, art := range c.Task.Artifacts {
				if art.Type == d || art.Title == d {
					title = art.Title
					break
				}
			}
			if title == "" {
				return Bad("err.review_deliverable_missing", taskRefText(c.Task), d)
			}
			checked = append(checked, title)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(checked) > 0 {
		line := i18n.Trf(sess.Loc(), "review.checked", strings.Join(checked, i18n.Tr(sess.Loc(), "sep.list")))
		if comment == "" {
			comment = line
		} else {
			comment = line + "\n" + comment
		}
	}
	return a.Transition(ctx, sess, taskID, step, TransitionPayload{Comment: comment})
}
