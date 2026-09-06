// Package app 是应用服务层：装载领域上下文、调用内核、持久化结果、派生通知。
// HTTP 接口与 MCP 都只通过这一层操作系统（ADR 0008）。
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// App 持有存储与配置。
type App struct {
	Store            *store.Store
	HeartbeatTimeout time.Duration
	PublicURL        string // 生成邀请链接用，如 http://localhost:8080

	// SecretKey 是加密组织凭据（外部目录的保密字段等）的服务端密钥，32 字节（ADR 0014、0017）。
	SecretKey []byte
	// failCodeEvent 只在测试里置真：让一次代码平台回调在去重占位之后失败。
	failCodeEvent bool

	syncing sync.Map // org id → 正在同步
}

func New(s *store.Store) *App {
	return &App{Store: s, HeartbeatTimeout: 10 * time.Minute, PublicURL: "http://localhost:8080", SecretKey: directory.DevKey()}
}

// Session 是一次请求的身份：组织 + 执行者（成员或 Agent）+ 语言 + 权限。
type Session struct {
	OrgID     string
	Actor     *domain.Executor
	MemberID  string // 成员本人，或 Agent 的所有者
	AgentID   string // Agent 请求时非空
	AccountID string
	// ClientIP 是这次请求的来源地址（HTTP 层填，MCP 与后台任务为空），只给进程内的尝试次数限制用。
	ClientIP    string
	Locale      i18n.Locale
	IsOwner     bool
	Permissions map[string]bool

	// 范围（ADR 0013）：Scope 是本次请求的 scope 参数（HTTP 层从 ?scope= 填入，空表示默认）；
	// 两侧可见域按组织的可见性策略与共享边界算出来（domain.VisibleTeams），登录时算一次。
	Scope          string
	TeamIDs        []string // 直接所属的团队
	CollabTeamIDs  []string // 协作数据的可见域（collabAll 为真时为空 = 全组织）
	FinanceTeamIDs []string // 财务数据的可见域
	collabAll      bool
	financeAll     bool
	collabPolicy   domain.Visibility
	financePolicy  domain.Visibility

	// 正在执行一条被确认的待确认操作时，这里是确认人；动态摘要会带上「经<确认人>确认」。
	ApprovedByID   string
	ApprovedByName string
}

// WithScope 返回一个带范围参数的会话副本（HTTP / MCP 层用）。
func (s *Session) WithScope(scope string) *Session {
	if s == nil {
		return nil
	}
	c := *s
	c.Scope = scope
	return &c
}

// IsAgent 判断当前请求来自 Agent。
func (s *Session) IsAgent() bool { return s.AgentID != "" }

// Can 判断是否持有某项权限（组织负责人拥有全部权限）。
func (s *Session) Can(perm string) bool {
	if s.IsOwner {
		return true
	}
	return s.Permissions[perm]
}

// Loc 返回会话语言（未设置时为默认语言）。
func (s *Session) Loc() i18n.Locale {
	if s == nil || s.Locale == "" {
		return i18n.Default
	}
	return s.Locale
}

// UserError 是可以直接展示给用户的错误，按请求者语言渲染。
type UserError struct {
	Status  int
	Reasons []i18n.Msg
}

func (e *UserError) Error() string { return e.Render(i18n.Default) }

// Render 按语言渲染。
func (e *UserError) Render(l i18n.Locale) string {
	parts := make([]string, len(e.Reasons))
	for i, m := range e.Reasons {
		parts[i] = m.Render(l)
	}
	return strings.Join(parts, "；")
}

func userErr(status int, key string, args ...any) error {
	return &UserError{Status: status, Reasons: []i18n.Msg{i18n.M(key, args...)}}
}

// Bad 是 400 级别的用户错误（key 为词条键）。
func Bad(key string, args ...any) error { return userErr(400, key, args...) }

// Forbidden 是 403。
func Forbidden(key string, args ...any) error { return userErr(403, key, args...) }

// NotFound 是 404。
func NotFound(key string, args ...any) error { return userErr(404, key, args...) }

// Unauthorized 是 401。
func Unauthorized(key string, args ...any) error { return userErr(401, key, args...) }

// wrapErr 把存储与内核的错误翻译成用户错误。
func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	var rj *domain.Rejection
	if errors.As(err, &rj) {
		return &UserError{Status: 409, Reasons: rj.Reasons}
	}
	if errors.Is(err, store.ErrNotFound) {
		return NotFound("err.not_found")
	}
	var ue *UserError
	if errors.As(err, &ue) {
		return ue
	}
	return err
}

// tx 在会话所属组织的事务里执行。
func (a *App) tx(ctx context.Context, sess *Session, fn func(tx pgx.Tx) error) error {
	return wrapErr(a.Store.WithOrg(ctx, sess.OrgID, fn))
}

// ---------- 装载与持久化 ----------

// loadContext 为内核装载一个任务的全部材料。
func (a *App) loadContext(ctx context.Context, tx pgx.Tx, sess *Session, taskID string) (*domain.Context, error) {
	t, err := a.Store.TaskByID(ctx, tx, taskID)
	if err != nil {
		return nil, err
	}
	if err := a.requireTaskVisible(ctx, tx, sess, t); err != nil {
		return nil, err
	}
	tt, err := a.Store.TaskType(ctx, tx, t.TypeName, t.TypeVersion)
	if err != nil {
		return nil, fmt.Errorf("任务类型 %s v%d 不存在: %w", t.TypeName, t.TypeVersion, err)
	}
	pre, err := a.Store.Predecessors(ctx, tx, taskID)
	if err != nil {
		return nil, err
	}
	bugs, err := a.Store.BugsFoundIn(ctx, tx, taskID)
	if err != nil {
		return nil, err
	}
	types, err := a.Store.TaskTypesFor(ctx, tx, append(append([]*domain.Task{}, pre...), bugs...))
	if err != nil {
		return nil, err
	}
	run, err := a.Store.ActiveRun(ctx, tx, taskID)
	if err != nil {
		return nil, err
	}
	actorRuns := 0
	if sess.Actor != nil {
		actorRuns, err = a.Store.ActiveRunCount(ctx, tx, sess.Actor.ID)
		if err != nil {
			return nil, err
		}
	}
	return &domain.Context{Task: t, Type: tt, Predecessors: pre, Bugs: bugs, Types: types, ActiveRun: run, ActorRuns: actorRuns, Now: time.Now(), NewID: store.NewID}, nil
}

// requireTaskVisible 按组织的协作数据可见性策略判断这个任务在不在请求者的可见域里（ADR 0013）：
// 列表按范围裁剪，详情、任务说明与各项命令也要同一口径，否则拿着 ID 就能绕过策略。
// 默认策略（全员可见）下不查任何东西；任务的归口团队为空（未分组）时视为可见。
func (a *App) requireTaskVisible(ctx context.Context, tx pgx.Tx, sess *Session, t *domain.Task) error {
	if sess == nil || sess.CollabAll() {
		return nil
	}
	ix, err := a.OrgIndex(ctx, tx)
	if err != nil {
		return err
	}
	if team := ix.TeamOfTask(t); team != "" && !sess.CanSeeCollabTeam(team) {
		return Forbidden("err.task_hidden")
	}
	return nil
}

// persist 把内核的结果写回数据库，并派生通知。
func (a *App) persist(ctx context.Context, tx pgx.Tx, sess *Session, c *domain.Context, o *domain.Outcome) error {
	if o == nil {
		return nil
	}
	t := o.Task
	if err := a.Store.UpdateTask(ctx, tx, t); err != nil {
		return err
	}
	relationsChanged := false
	for _, e := range o.Events {
		switch e.Type {
		case "RelationRemoved", "TasksLinked":
			relationsChanged = true
		case "ArtifactAttached":
			if n := len(t.Artifacts); n > 0 {
				if err := a.Store.InsertArtifact(ctx, tx, sess.OrgID, t.ID, t.Artifacts[n-1]); err != nil {
					return err
				}
			}
		case "CommentAdded", "NoteAdded":
			if n := len(t.Comments); n > 0 {
				if err := a.Store.InsertComment(ctx, tx, sess.OrgID, t.ID, t.Comments[n-1]); err != nil {
					return err
				}
			}
		}
	}
	if relationsChanged {
		if err := a.Store.SyncRelations(ctx, tx, t); err != nil {
			return err
		}
	}
	if o.ClosedRun != nil {
		cost, err := a.costOfRun(ctx, tx, sess.OrgID, o.ClosedRun)
		if err != nil {
			return err
		}
		if err := a.Store.UpdateRun(ctx, tx, o.ClosedRun, cost); err != nil {
			return err
		}
	} else if c.ActiveRun != nil && o.OpenedRun == nil {
		cost, err := a.costOfRun(ctx, tx, sess.OrgID, c.ActiveRun)
		if err != nil {
			return err
		}
		if err := a.Store.UpdateRun(ctx, tx, c.ActiveRun, cost); err != nil {
			return err
		}
	}
	if o.OpenedRun != nil {
		if err := a.Store.InsertRun(ctx, tx, sess.OrgID, o.OpenedRun); err != nil {
			return err
		}
	}
	if err := a.insertEvents(ctx, tx, sess, o.Events); err != nil {
		return err
	}
	return a.notify(ctx, tx, sess, c.Type, t, o.Events)
}

// localeOfMember 解析某成员的语言：账号设置 → 组织默认 → 系统默认。
func (a *App) localeOfMember(ctx context.Context, tx pgx.Tx, orgID, memberID string) i18n.Locale {
	if m, err := a.Store.MemberByID(ctx, tx, memberID); err == nil {
		if acc, err := a.Store.AccountByID(ctx, tx, m.AccountID); err == nil {
			if l := i18n.Normalize(acc.Locale); l != "" {
				return l
			}
		}
	}
	if org, err := a.Store.OrganizationByID(ctx, tx, orgID); err == nil {
		if l := i18n.Normalize(org.DefaultLocale); l != "" {
			return l
		}
	}
	return i18n.Default
}

// notify 按动态派生站内通知：被指派、待验收、Agent 提问、进入待领取。文案按收件人语言渲染。
func (a *App) notify(ctx context.Context, tx pgx.Tx, sess *Session, tt *domain.TaskType, t *domain.Task, events []domain.Event) error {
	// kind 是外发的事件类型（ADR 0019）：指派给我、等我验收、提问会按收件人的规则排外发；进入待领取只在站内（kind 为空）。
	send := func(memberID string, kind string, title, body i18n.Msg) error {
		if memberID == "" || memberID == sess.Actor.ID {
			return nil
		}
		if ag, err := a.Store.AgentByID(ctx, tx, memberID); err == nil {
			memberID = ag.OwnerMemberID
		}
		l := a.localeOfMember(ctx, tx, sess.OrgID, memberID)
		return a.notifyAndDeliver(ctx, tx, sess.OrgID, kind, domain.Notification{MemberID: memberID, Title: title.Render(l), Body: body.Render(l), TaskID: t.ID}, "")
	}
	for _, e := range events {
		switch e.Type {
		case "TaskAssigned":
			if to, _ := e.Data["to"].(string); to != "" {
				if err := send(to, domain.NotifyAssigned, i18n.M("notif.assigned", t.Title), i18n.M("notif.assigned_by", sess.Actor.Name)); err != nil {
					return err
				}
			}
		case "TaskTransitioned":
			to, _ := e.Data["to"].(string)
			var title i18n.Text
			if tx, ok := e.Data["title"].(i18n.Text); ok {
				title = tx
			}
			if st := tt.Workflow.State(to); st != nil && st.Label == domain.LabelWaiting && to != "waiting" && to != "blocked" {
				if err := send(t.ReviewerID, domain.NotifyReview, i18n.M("notif.review", t.Title), i18n.M("notif.review_body", sess.Actor.Name, title)); err != nil {
					return err
				}
			}
			if to == "waiting" {
				if err := send(t.CreatorID, domain.NotifyQuestion, i18n.M("notif.question", sess.Actor.Name, t.Title), i18n.M("ev.generic", lastComment(t), "")); err != nil {
					return err
				}
			}
		case "TaskSentToBacklog":
			if err := send(t.CreatorID, "", i18n.M("notif.backlog", t.Title), i18n.M("notif.backlog_body")); err != nil {
				return err
			}
		}
	}
	return nil
}

func lastComment(t *domain.Task) string {
	if n := len(t.Comments); n > 0 {
		return t.Comments[n-1].Text
	}
	return ""
}

// costOfRun 按价格表折算一段执行记录的成本（组织结算货币）。
func (a *App) costOfRun(ctx context.Context, tx pgx.Tx, orgID string, r *domain.Run) (float64, error) {
	if len(r.Usage) == 0 {
		return 0, nil
	}
	prices, err := a.Store.PricesFor(ctx, tx, orgID)
	if err != nil {
		return 0, err
	}
	org, err := a.Store.OrganizationByID(ctx, tx, orgID)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for i, u := range r.Usage {
		p, ok := prices[u.ModelID]
		if !ok {
			continue
		}
		c := p.CostOf(u)
		rate, err := a.Store.ExchangeRate(ctx, tx, p.Currency, org.Currency)
		if err == store.ErrNotFound {
			rate = 1
		} else if err != nil {
			return 0, err
		}
		c *= rate
		r.Usage[i].Cost = c
		total += c
	}
	return total, nil
}

// ExpireStaleRuns 结束心跳超时的执行记录（由后台定时调用）。
func (a *App) ExpireStaleRuns(ctx context.Context, orgID string) (int, error) {
	n := 0
	err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		stale, err := a.Store.StaleRuns(ctx, tx, a.HeartbeatTimeout)
		if err != nil {
			return err
		}
		for _, rr := range stale {
			c := &domain.Context{Task: &domain.Task{ID: rr.TaskID}, ActiveRun: &rr.Run, Now: time.Now(), NewID: store.NewID}
			o := domain.TimeOut(c)
			if err := a.Store.UpdateRun(ctx, tx, &rr.Run, rr.Cost); err != nil {
				return err
			}
			if err := a.Store.InsertEvents(ctx, tx, orgID, o.Events); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
