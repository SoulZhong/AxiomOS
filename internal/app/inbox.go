package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 待我处理（DESIGN.md §12）：「我的工作」顶部的固定区，汇总所有等我出手的事。
// 只看本人，永远不套范围（ADR 0013 的 scope 只是筛选，而这里的问题是"谁该出手"，答案只有我）。
// 组的顺序就是紧急度：逾期 → 待确认 → 待验收 → 待答复 → 未开始 → 通知；空组省略。
//
// 每组的规则：
//   - overdue        我是负责人、计划结束日早于今天、且没到终止类型状态的任务，附 days_overdue
//   - proposals      还在等人确认、且我能确认（can_decide）的待确认操作
//   - review         停在等待类型状态、从这里出发有一步需要「验收」授权且由我触发的任务（domain.AwaitsReviewBy）
//   - questions      停在等待类型状态、从这里出发有一步要求评论、不带验收授权且由我触发的任务，
//                    最后一条评论不是我写的（domain.AwaitsReplyFrom + domain.OpenQuestion）
//   - unstarted      我是负责人、处于未开始类型状态的任务
//   - notifications  我的未读通知
//
// 只认状态类型与步骤声明，不认状态名（ADR 0005）。

// 组的键，也是接口上的 kind。
const (
	InboxOverdue       = "overdue"
	InboxProposals     = "proposals"
	InboxReview        = "review"
	InboxQuestions     = "questions"
	InboxUnstarted     = "unstarted"
	InboxNotifications = "notifications"
)

// InboxKinds 是全部组，按紧急度排序。
var InboxKinds = []string{InboxOverdue, InboxProposals, InboxReview, InboxQuestions, InboxUnstarted, InboxNotifications}

// InboxTask 是任务类组里的一条：任务摘要加逾期天数（只有 overdue 组非零）。
type InboxTask struct {
	Task        TaskSummary `json:"task"`
	DaysOverdue int         `json:"days_overdue"`
}

// InboxQuestion 是等我答复的一条提问：任务加那条评论。
type InboxQuestion struct {
	Task    TaskSummary    `json:"task"`
	Comment domain.Comment `json:"comment"`
}

// InboxGroup 是一组事项。四种载荷按 Kind 只有一种非空。
type InboxGroup struct {
	Kind          string                 `json:"kind"`
	Title         string                 `json:"title"`
	Count         int                    `json:"count"`
	Tasks         []InboxTask            `json:"tasks,omitempty"`
	Proposals     []*ProposalView        `json:"proposals,omitempty"`
	Questions     []InboxQuestion        `json:"questions,omitempty"`
	Notifications []*domain.Notification `json:"notifications,omitempty"`
}

// Inbox 是整个「待我处理」。Count 是各组条数之和；全空时 Empty 是那句「没有等你处理的事。」
type Inbox struct {
	Count  int          `json:"count"`
	Groups []InboxGroup `json:"groups"`
	Empty  string       `json:"empty,omitempty"`
}

// InboxCount 是只有读数的版本，供侧栏角标与状态栏。
type InboxCount struct {
	Count  int            `json:"count"`
	ByKind map[string]int `json:"by_kind"`
}

// requireInboxOwner 待我处理只属于人：Agent 没有收件箱，它的工具列表就是它的收件箱。
func requireInboxOwner(sess *Session) error {
	if sess.IsAgent() {
		return Forbidden("err.inbox_agent")
	}
	return nil
}

// inboxRaw 是一次事务里收齐的原始材料，Inbox 与 InboxCount 共用。
type inboxRaw struct {
	overdue, review, unstarted []*domain.Task
	questions                  []inboxRawQuestion
	proposals                  []*domain.Proposal
	notifications              []*domain.Notification
	types                      map[string]*domain.TaskType
	today                      time.Time
}

type inboxRawQuestion struct {
	task    *domain.Task
	comment domain.Comment
}

// daysOverdue 算计划结束日到今天隔了几天（计划结束日当天不算逾期）。
// 计划结束日是个日历日（date 列），用 calendarDay 取，不换算时区；今天用 startOfDay（服务器时区）。
func daysOverdue(plannedEnd *time.Time, today time.Time) int {
	if plannedEnd == nil {
		return 0
	}
	end := calendarDay(*plannedEnd)
	if !end.Before(today) {
		return 0
	}
	return int(today.Sub(end).Hours()/24 + 0.5)
}

// collectInbox 在事务里按上面的规则挑出等我出手的事。withNotifications 为假时只数通知不取内容。
func (a *App) collectInbox(ctx context.Context, tx pgx.Tx, sess *Session, withNotifications bool) (*inboxRaw, error) {
	raw := &inboxRaw{today: startOfDay(time.Now())}
	tasks, err := a.Store.AllTasks(ctx, tx)
	if err != nil {
		return nil, err
	}
	raw.types, err = a.Store.TaskTypesFor(ctx, tx, tasks)
	if err != nil {
		return nil, err
	}
	me := sess.Actor
	for _, t := range tasks {
		tt := raw.types[t.TypeName]
		if tt == nil {
			continue
		}
		st := tt.Workflow.State(t.State)
		if st == nil || st.Label.IsTerminal() {
			continue
		}
		if t.AssigneeID == sess.MemberID {
			if daysOverdue(t.PlannedEnd, raw.today) > 0 {
				raw.overdue = append(raw.overdue, t)
			}
			if st.Label == domain.LabelPending {
				raw.unstarted = append(raw.unstarted, t)
			}
		}
		if domain.AwaitsReviewBy(t, tt, me) {
			raw.review = append(raw.review, t)
		}
		if domain.AwaitsReplyFrom(t, tt, me) {
			if q := domain.OpenQuestion(t, me); q != nil {
				raw.questions = append(raw.questions, inboxRawQuestion{task: t, comment: *q})
			}
		}
	}
	pending, err := a.Store.ListProposals(ctx, tx, store.ProposalFilter{OnlyOpen: true})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, p := range pending {
		// 过期但巡检还没作废的不算：人确认它也会被拒
		if now.After(p.ExpiresAt) || !canDecideProposal(sess, p) {
			continue
		}
		raw.proposals = append(raw.proposals, p)
	}
	if withNotifications {
		raw.notifications, err = a.Store.UnreadNotifications(ctx, tx, sess.MemberID, 100)
		if err != nil {
			return nil, err
		}
	} else {
		n, err := a.Store.CountUnreadNotifications(ctx, tx, sess.MemberID)
		if err != nil {
			return nil, err
		}
		raw.notifications = make([]*domain.Notification, n)
	}
	return raw, nil
}

func (r *inboxRaw) counts() map[string]int {
	return map[string]int{
		InboxOverdue:       len(r.overdue),
		InboxProposals:     len(r.proposals),
		InboxReview:        len(r.review),
		InboxQuestions:     len(r.questions),
		InboxUnstarted:     len(r.unstarted),
		InboxNotifications: len(r.notifications),
	}
}

// Inbox 返回当前成员的「待我处理」，组按紧急度排序，空组省略。
func (a *App) Inbox(ctx context.Context, sess *Session) (*Inbox, error) {
	if err := requireInboxOwner(sess); err != nil {
		return nil, err
	}
	loc := sess.Loc()
	out := &Inbox{Groups: []InboxGroup{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		raw, err := a.collectInbox(ctx, tx, sess, true)
		if err != nil {
			return err
		}
		names, err := a.Store.ExecutorNames(ctx, tx)
		if err != nil {
			return err
		}
		now := time.Now()
		sum := func(t *domain.Task) TaskSummary { return summarize(t, raw.types[t.TypeName], names, 0, now, loc) }
		taskGroup := func(kind string, tasks []*domain.Task) {
			if len(tasks) == 0 {
				return
			}
			g := InboxGroup{Kind: kind, Title: i18n.Tr(loc, "inbox.kind."+kind), Count: len(tasks), Tasks: make([]InboxTask, 0, len(tasks))}
			for _, t := range tasks {
				it := InboxTask{Task: sum(t)}
				if kind == InboxOverdue {
					it.DaysOverdue = daysOverdue(t.PlannedEnd, raw.today)
				}
				g.Tasks = append(g.Tasks, it)
			}
			out.Groups = append(out.Groups, g)
		}
		for _, kind := range InboxKinds {
			switch kind {
			case InboxOverdue:
				taskGroup(kind, raw.overdue)
			case InboxReview:
				taskGroup(kind, raw.review)
			case InboxUnstarted:
				taskGroup(kind, raw.unstarted)
			case InboxProposals:
				if len(raw.proposals) == 0 {
					continue
				}
				g := InboxGroup{Kind: kind, Title: i18n.Tr(loc, "inbox.kind."+kind), Count: len(raw.proposals)}
				for _, p := range raw.proposals {
					g.Proposals = append(g.Proposals, a.proposalViewWith(sess, p, names))
				}
				out.Groups = append(out.Groups, g)
			case InboxQuestions:
				if len(raw.questions) == 0 {
					continue
				}
				g := InboxGroup{Kind: kind, Title: i18n.Tr(loc, "inbox.kind."+kind), Count: len(raw.questions)}
				for _, q := range raw.questions {
					g.Questions = append(g.Questions, InboxQuestion{Task: sum(q.task), Comment: q.comment})
				}
				out.Groups = append(out.Groups, g)
			case InboxNotifications:
				if len(raw.notifications) == 0 {
					continue
				}
				out.Groups = append(out.Groups, InboxGroup{Kind: kind, Title: i18n.Tr(loc, "inbox.kind."+kind), Count: len(raw.notifications), Notifications: raw.notifications})
			}
		}
		for _, g := range out.Groups {
			out.Count += g.Count
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out.Count == 0 {
		out.Empty = i18n.Tr(loc, "inbox.empty")
	}
	return out, nil
}

// InboxCount 只返回读数（侧栏角标、状态栏），不组装任何条目。
func (a *App) InboxCount(ctx context.Context, sess *Session) (*InboxCount, error) {
	if err := requireInboxOwner(sess); err != nil {
		return nil, err
	}
	out := &InboxCount{ByKind: map[string]int{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		raw, err := a.collectInbox(ctx, tx, sess, false)
		if err != nil {
			return err
		}
		out.ByKind = raw.counts()
		for _, n := range out.ByKind {
			out.Count += n
		}
		return nil
	})
	return out, err
}

// MarkNotificationsRead 把我的这几条通知标为已读，返回我剩下的未读数。
// 传进来的 ID 若不是我的通知，直接忽略（SQL 带 member_id 条件），不会越权标别人的。
//
// 这是「任何写操作都要产生动态」这条硬规则的唯一例外：把一条通知标为已读只是阅读状态的变化，
// 不改任何领域对象，也不值得让全组织的动态流里多出一行"某人读了通知"。
func (a *App) MarkNotificationsRead(ctx context.Context, sess *Session, ids []int64) (int, error) {
	if err := requireInboxOwner(sess); err != nil {
		return 0, err
	}
	if err := refuseDryRun(sess); err != nil {
		return 0, err
	}
	remaining := 0
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		if err := a.Store.MarkNotificationsReadByIDs(ctx, tx, sess.MemberID, ids); err != nil {
			return err
		}
		var err error
		remaining, err = a.Store.CountUnreadNotifications(ctx, tx, sess.MemberID)
		return err
	})
	return remaining, err
}
