package app

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 看板是任务按流程状态分列的视图（ADR 0012）：列 = 所选任务类型流程里的状态；
// 不指定类型时退化为五种状态类型。拖动卡片 = 调用内核的一步，可拖到哪里由内核按当前登录者算。

// BoardFilter 是看板的过滤条件。
type BoardFilter struct {
	TypeName   string
	GoalID     string
	TeamID     string
	AssigneeID string // "me" 表示当前登录者（含其 Agent）
	SprintID   string
	Lane       string // none | goal | assignee
}

// BoardColumn 是看板的一列。
type BoardColumn struct {
	State     StateView   `json:"state"`
	Claimable bool        `json:"claimable,omitempty"`
	WIPLimit  int         `json:"wip_limit,omitempty"`
	Count     int         `json:"count"`
	OverLimit bool        `json:"over_limit"`
	Cards     []BoardCard `json:"cards"`
}

// BoardCard 是一张卡片：任务摘要 + 可拖到的状态。
type BoardCard struct {
	TaskSummary
	CanMoveTo []string      `json:"can_move_to"`
	Moves     []domain.Move `json:"moves"`
	Lane      string        `json:"lane,omitempty"`
}

// BoardLane 是一条泳道。
type BoardLane struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// BoardView 是整个看板。
type BoardView struct {
	Type    *domain.TaskType `json:"type"`
	Columns []BoardColumn    `json:"columns"`
	Lanes   []BoardLane      `json:"lanes,omitempty"`
}

// Board 组装看板。
func (a *App) Board(ctx context.Context, sess *Session, f BoardFilter) (*BoardView, error) {
	if f.Lane != "" && f.Lane != "none" && f.Lane != "goal" && f.Lane != "assignee" {
		return nil, Bad("err.board_lane")
	}
	loc := sess.Loc()
	v := &BoardView{Columns: []BoardColumn{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		tf := store.TaskFilter{TypeName: f.TypeName, GoalID: f.GoalID, SprintID: f.SprintID}
		if f.AssigneeID == "me" {
			tf.Executors = a.myExecutorIDs(ctx, tx, sess)
		} else if f.AssigneeID != "" {
			tf.AssigneeID = f.AssigneeID
		}
		tasks, err := a.Store.ListTasks(ctx, tx, tf)
		if err != nil {
			return err
		}
		// 范围（ADR 0013）：对看板这类协作数据它只是筛选。
		scope, ix, err := a.scopedIndex(ctx, tx, sess)
		if err != nil {
			return err
		}
		tasks = ix.filterTasks(scope, tasks)
		if f.TeamID != "" {
			teamOf, err := a.Store.TeamOfMembers(ctx, tx)
			if err != nil {
				return err
			}
			agents, _ := a.Store.ListAgents(ctx, tx)
			for _, ag := range agents {
				if t, ok := teamOf[ag.OwnerMemberID]; ok {
					teamOf[ag.ID] = t
				}
			}
			kept := tasks[:0]
			for _, t := range tasks {
				if teamOf[t.AssigneeID] == f.TeamID {
					kept = append(kept, t)
				}
			}
			tasks = kept
		}
		names, err := a.Store.ExecutorNames(ctx, tx)
		if err != nil {
			return err
		}
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		goalTitle := map[string]string{}
		if f.Lane == "goal" {
			goals, err := a.Store.ListGoals(ctx, tx)
			if err != nil {
				return err
			}
			for _, g := range goals {
				goalTitle[g.ID] = g.Title
			}
		}

		// 列
		var colType *domain.TaskType
		if f.TypeName != "" {
			colType, err = a.Store.CurrentTaskType(ctx, tx, f.TypeName)
			if err != nil {
				return Bad("err.type_missing", f.TypeName)
			}
			v.Type = colType
			for _, st := range colType.Workflow.States {
				v.Columns = append(v.Columns, BoardColumn{State: StateView{Name: st.Name, Title: st.Title.In(loc), Label: st.Label, LabelTitle: domain.LabelTitle(st.Label, loc)}, Claimable: st.Claimable, WIPLimit: st.WIPLimit, Cards: []BoardCard{}})
			}
		} else {
			for _, l := range []domain.Label{domain.LabelPending, domain.LabelActive, domain.LabelWaiting, domain.LabelTerminalSuccess, domain.LabelTerminalFailure} {
				v.Columns = append(v.Columns, BoardColumn{State: StateView{Name: string(l), Title: domain.LabelTitle(l, loc), Label: l, LabelTitle: domain.LabelTitle(l, loc)}, Cards: []BoardCard{}})
			}
		}
		colIndex := map[string]int{}
		for i, c := range v.Columns {
			colIndex[c.State.Name] = i
		}

		// 卡片
		now := time.Now()
		lanes := map[string]string{}
		var laneKeys []string
		for _, t := range tasks {
			tt := types[t.TypeName]
			if tt == nil {
				continue
			}
			st := tt.Workflow.State(t.State)
			if st == nil {
				continue
			}
			key := t.State
			if colType == nil {
				key = string(st.Label)
			}
			ci, ok := colIndex[key]
			if !ok {
				continue
			}
			c, err := a.loadContext(ctx, tx, sess, t.ID)
			if err != nil {
				return err
			}
			card := BoardCard{TaskSummary: summarize(c.Task, tt, names, 0, now, loc), CanMoveTo: []string{}, Moves: []domain.Move{}}
			seen := map[string]bool{}
			for _, m := range domain.Moves(c, sess.Actor) {
				target := m.To
				if colType == nil {
					if ts := tt.Workflow.State(m.To); ts != nil {
						target = string(ts.Label)
					}
				}
				if _, exists := colIndex[target]; !exists || seen[target] {
					continue
				}
				seen[target] = true
				card.CanMoveTo = append(card.CanMoveTo, target)
				card.Moves = append(card.Moves, domain.Move{To: target, Transition: m.Transition})
			}
			switch f.Lane {
			case "goal":
				card.Lane = t.GoalID
				title := goalTitle[t.GoalID]
				if t.GoalID == "" {
					title = i18n.Tr(loc, "group.no_goal")
				}
				if _, ok := lanes[card.Lane]; !ok {
					lanes[card.Lane] = title
					laneKeys = append(laneKeys, card.Lane)
				}
			case "assignee":
				card.Lane = t.AssigneeID
				title := names[t.AssigneeID]
				if t.AssigneeID == "" {
					title = i18n.Tr(loc, "group.unclaimed")
				}
				if _, ok := lanes[card.Lane]; !ok {
					lanes[card.Lane] = title
					laneKeys = append(laneKeys, card.Lane)
				}
			}
			v.Columns[ci].Cards = append(v.Columns[ci].Cards, card)
		}
		for i := range v.Columns {
			col := &v.Columns[i]
			col.Count = len(col.Cards)
			col.OverLimit = col.WIPLimit > 0 && col.Count > col.WIPLimit
		}
		if f.Lane == "goal" || f.Lane == "assignee" {
			sort.SliceStable(laneKeys, func(i, j int) bool {
				// 空键（未挂目标 / 待领取）排最后
				if laneKeys[i] == "" || laneKeys[j] == "" {
					return laneKeys[j] == ""
				}
				return lanes[laneKeys[i]] < lanes[laneKeys[j]]
			})
			v.Lanes = []BoardLane{}
			for _, k := range laneKeys {
				v.Lanes = append(v.Lanes, BoardLane{Key: k, Title: lanes[k]})
			}
		}
		return nil
	})
	return v, err
}

// myExecutorIDs 返回当前登录者本人及其 Agent 的 ID（Agent 请求时是 Agent 与所有者）。
func (a *App) myExecutorIDs(ctx context.Context, tx pgx.Tx, sess *Session) []string {
	ids := []string{sess.Actor.ID}
	if sess.IsAgent() {
		return append(ids, sess.MemberID)
	}
	if agents, err := a.Store.ListAgents(ctx, tx); err == nil {
		for _, ag := range agents {
			if ag.OwnerMemberID == sess.MemberID {
				ids = append(ids, ag.ID)
			}
		}
	}
	return ids
}
