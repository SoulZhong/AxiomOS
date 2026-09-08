package app

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 给 Agent 看的目标视图（与 get_task_brief 同一思路）：只有 Agent 规划与汇报要用的字段，
// 名字代替内部 ID（负责人、团队、类型都直接写名字），日期写成 YYYY-MM-DD，不带 org_id 这类内部字段。

// GoalRef 是一行目标：树里的子目标、详情里的上级链都用它。
type GoalRef struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	StatusTitle string `json:"status_title"`
	Progress    int    `json:"progress"`
}

// MilestoneBrief 是一行里程碑。
type MilestoneBrief struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	DueOn       string `json:"due_on"`
	Status      string `json:"status"`
	StatusTitle string `json:"status_title"`
	ReadyHint   bool   `json:"ready_hint"`
}

// GoalNote 是目标上的一条进展说明（记在动态里，不是目标的字段）。
type GoalNote struct {
	ID   int64     `json:"id"`
	At   time.Time `json:"at"`
	ByID string    `json:"by_id"`
	By   string    `json:"by"`
	Text string    `json:"text"`
}

// GoalBrief 是给 Agent 的目标视图。树（list_goals）里每个节点都是它；详情（get_goal）再补上级链、任务与进展说明。
type GoalBrief struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	Status        string   `json:"status"`
	StatusTitle   string   `json:"status_title"`
	OwnerID       string   `json:"owner_id"`
	Owner         string   `json:"owner"`
	Team          string   `json:"team,omitempty"`
	Type          string   `json:"type,omitempty"`
	ParentID      string   `json:"parent_id,omitempty"`
	Deadline      string   `json:"deadline,omitempty"`
	PlannedStart  string   `json:"planned_start,omitempty"`
	PlannedEnd    string   `json:"planned_end,omitempty"`
	DerivedStart  string   `json:"derived_start,omitempty"`
	DerivedEnd    string   `json:"derived_end,omitempty"`
	DatePrecision string   `json:"date_precision,omitempty"`
	Horizon       string   `json:"horizon,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
	Outcome       string   `json:"outcome,omitempty"`
	Progress      int      `json:"progress"`
	TaskCount     int      `json:"task_count"`
	DoneCount     int      `json:"done_count"`
	Cost          float64  `json:"cost"`
	Currency      string   `json:"currency"`
	Budget        *float64 `json:"budget,omitempty"`
	OverBudget    bool     `json:"over_budget"`
	// CanEdit 表示当前身份能不能改这个目标（Agent 还要看授权；授权是需要人确认时改动会先挂起）。
	CanEdit    bool             `json:"can_edit"`
	Milestones []MilestoneBrief `json:"milestones"`
	Children   []*GoalBrief     `json:"children"`
	// 只在详情里出现
	Parents []GoalRef     `json:"parents,omitempty"` // 从根到直接上级
	Tasks   []TaskSummary `json:"tasks,omitempty"`   // 直接挂在这个目标下的任务（不含子目标的）
	Notes   []GoalNote    `json:"notes,omitempty"`   // 最近的进展说明，新的在前
}

// GoalBriefTree 是给 Agent 的目标树。
func (a *App) GoalBriefTree(ctx context.Context, sess *Session) ([]*GoalBrief, error) {
	tree, err := a.GoalTree(ctx, sess)
	if err != nil {
		return nil, err
	}
	var out []*GoalBrief
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		r, err := a.goalBriefRefs(ctx, tx, sess)
		if err != nil {
			return err
		}
		out = r.tree(tree)
		return nil
	})
	if out == nil {
		out = []*GoalBrief{}
	}
	return out, err
}

// GoalBriefDetail 是给 Agent 的单个目标详情：视图 + 上级链 + 直接任务 + 进展说明。
func (a *App) GoalBriefDetail(ctx context.Context, sess *Session, id string) (*GoalBrief, error) {
	tree, err := a.GoalTree(ctx, sess)
	if err != nil {
		return nil, err
	}
	// 从根到这个目标的一路：最后一个是它自己，前面的是上级链（带各自算好的进度）
	path := goalPath(tree, id)
	if path == nil {
		return nil, NotFound("err.goal_missing")
	}
	v := path[len(path)-1]
	tasks, err := a.ListTaskSummaries(ctx, sess, store.TaskFilter{GoalID: id})
	if err != nil {
		return nil, err
	}
	var out *GoalBrief
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		r, err := a.goalBriefRefs(ctx, tx, sess)
		if err != nil {
			return err
		}
		out = r.brief(v)
		out.Parents = r.parents(path[:len(path)-1])
		out.Tasks = []TaskSummary{}
		for _, t := range tasks {
			if t.ParentID == "" {
				out.Tasks = append(out.Tasks, t)
			}
		}
		rows, err := a.Store.ListEventsByGoal(ctx, tx, id, 200)
		if err != nil {
			return err
		}
		out.Notes = []GoalNote{}
		for _, e := range rows {
			if e.Type != "GoalNoteAdded" {
				continue
			}
			text, _ := e.Data["text"].(string)
			out.Notes = append(out.Notes, GoalNote{ID: e.ID, At: e.At, ByID: e.ActorID, By: r.names[e.ActorID], Text: text})
			if len(out.Notes) >= 20 {
				break
			}
		}
		return nil
	})
	return out, err
}

// goalBriefRefs 是渲染视图要用的名字表与判断器。
type goalBriefRefs struct {
	a        *App
	ctx      context.Context
	tx       pgx.Tx
	sess     *Session
	names    map[string]string
	teams    map[string]string
	currency string
}

func (a *App) goalBriefRefs(ctx context.Context, tx pgx.Tx, sess *Session) (*goalBriefRefs, error) {
	names, err := a.Store.ExecutorNames(ctx, tx)
	if err != nil {
		return nil, err
	}
	teams, err := a.teamNames(ctx, tx)
	if err != nil {
		return nil, err
	}
	org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
	if err != nil {
		return nil, err
	}
	return &goalBriefRefs{a: a, ctx: ctx, tx: tx, sess: sess, names: names, teams: teams, currency: org.Currency}, nil
}

func (r *goalBriefRefs) tree(vs []*GoalView) []*GoalBrief {
	out := make([]*GoalBrief, 0, len(vs))
	for _, v := range vs {
		out = append(out, r.brief(v))
	}
	return out
}

func (r *goalBriefRefs) brief(v *GoalView) *GoalBrief {
	loc := r.sess.Loc()
	b := &GoalBrief{ID: v.ID, Title: v.Title, Description: v.Description, Status: string(v.Status), StatusTitle: i18n.Tr(loc, "goal.status."+string(v.Status)),
		OwnerID: v.OwnerMemberID, Owner: r.names[v.OwnerMemberID], Team: r.teams[v.TeamID], ParentID: v.ParentID,
		Deadline: briefDate(v.Deadline), PlannedStart: briefDate(v.PlannedStart), PlannedEnd: briefDate(v.PlannedEnd),
		DerivedStart: briefDate(v.DerivedStart), DerivedEnd: briefDate(v.DerivedEnd),
		DatePrecision: string(domain.NormalizeDatePrecision(v.DatePrecision)), Horizon: string(v.Horizon), Confidence: string(v.Confidence), Outcome: v.Outcome,
		Progress: v.Progress, TaskCount: v.TaskCount, DoneCount: v.DoneCount, Cost: v.Cost, Currency: r.currency, Budget: v.Budget, OverBudget: v.OverBudget,
		CanEdit: r.a.canEditGoal(r.ctx, r.tx, r.sess, v.Goal), Milestones: []MilestoneBrief{}, Children: []*GoalBrief{}}
	if b.Owner == "" {
		b.Owner = v.OwnerMemberID
	}
	if v.Type != nil {
		b.Type = v.Type.Name
	}
	for _, m := range v.Milestones {
		b.Milestones = append(b.Milestones, MilestoneBrief{ID: m.ID, Title: m.Title, Description: m.Description, DueOn: m.DueOn.Format("2006-01-02"),
			Status: string(m.Status), StatusTitle: i18n.Tr(loc, "milestone.status."+string(m.Status)), ReadyHint: m.ReadyHint})
	}
	b.Children = r.tree(v.Children)
	return b
}

// parents 把从根到直接上级的一路变成一行行的目标引用（进度取自树上算好的汇总）。
func (r *goalBriefRefs) parents(chain []*GoalView) []GoalRef {
	out := make([]GoalRef, 0, len(chain))
	for _, cur := range chain {
		out = append(out, GoalRef{ID: cur.ID, Title: cur.Title, Status: string(cur.Status), StatusTitle: i18n.Tr(r.sess.Loc(), "goal.status."+string(cur.Status)), Progress: cur.Progress})
	}
	return out
}

// goalPath 在树里找到某个目标，返回从根到它的一路；找不到返回 nil。
func goalPath(tree []*GoalView, id string) []*GoalView {
	for _, v := range tree {
		if v.ID == id {
			return []*GoalView{v}
		}
		if rest := goalPath(v.Children, id); rest != nil {
			return append([]*GoalView{v}, rest...)
		}
	}
	return nil
}

func briefDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// ---------- 进展说明 ----------

// AddGoalNote 在目标上写一句进展说明。目标没有评论区，这句话记成一条动态（GoalNoteAdded），
// 目标详情与动态流里都能看到。与任务上的工作日志同一套授权：Agent 要有「评论」授权，
// 授权是「需要人确认」时先记一条待确认操作。
func (a *App) AddGoalNote(ctx context.Context, sess *Session, goalID, text string) (*GoalNote, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, Bad("err.empty_text")
	}
	return idempotent(ctx, a, sess, "add_goal_note", map[string]any{"goal_id": goalID, "text": text}, func() (*GoalNote, error) {
		var note *GoalNote
		var prop *ProposalView
		err := a.tx(ctx, sess, func(tx pgx.Tx) error {
			g, err := a.Store.GoalByID(ctx, tx, goalID)
			if err != nil {
				return NotFound("err.goal_missing")
			}
			if g.TeamID != "" && !sess.CanSeeCollabTeam(g.TeamID) {
				return Forbidden("err.goal_hidden")
			}
			if sess.IsAgent() {
				if !sess.Actor.HasGrant(domain.GrantComment) {
					return Forbidden("err.agent_no_grant", i18n.Key("grant.comment"))
				}
				if domain.NeedsApproval(sess.Actor, domain.GrantComment) {
					summary := i18n.M("proposal.summary.goal.note", sess.Actor.Name, g.Title)
					if sess.Write.DryRun {
						return dryRunPending(sess, a.executorName(ctx, tx, sess.MemberID), summary)
					}
					prop, err = a.createProposal(ctx, tx, sess, proposalDraft{Action: ActionGoalNote, Grant: domain.GrantComment, TargetKind: "goal",
						TargetID: g.ID, TargetTitle: g.Title, Payload: map[string]any{"goal_id": g.ID, "text": text}, Summary: summary})
					return err
				}
			}
			if sess.Write.DryRun {
				return dryRun(sess, i18n.M("will.goal.note", g.Title))
			}
			now := time.Now()
			if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "GoalNoteAdded", ActorID: sess.Actor.ID, At: now,
				Data: map[string]any{"goal_id": g.ID, "title": g.Title, "text": text}}}); err != nil {
				return err
			}
			note = &GoalNote{At: now, ByID: sess.Actor.ID, By: sess.Actor.Name, Text: text}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if prop != nil {
			return nil, pending(sess, prop)
		}
		return note, nil
	})
}
