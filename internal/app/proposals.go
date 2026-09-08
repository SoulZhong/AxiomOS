package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 待确认操作（ADR 0003）：Agent 的某项授权是「需要人确认」时，动作不立即执行，
// 而是记成一条待确认操作；人点确认后，系统以这个 Agent 的身份、把该授权当作直接生效重新执行一次。

// 动作代码名。界面上显示的是 proposal.action.* 词条。
const (
	ActionTaskTransition    = "task.transition"
	ActionTaskClaim         = "task.claim"
	ActionTaskBegin         = "task.begin"
	ActionTaskAssign        = "task.assign"
	ActionTaskComment       = "task.comment"
	ActionTaskNote          = "task.note"
	ActionTaskArtifact      = "task.artifact"
	ActionTaskLink          = "task.link"
	ActionTaskExternalLink  = "task.external_link"
	ActionTaskCreate        = "task.create"
	ActionTaskCreateSubtask = "task.create_subtask"
	ActionTaskUpdate        = "task.update"
	ActionGoalCreate        = "goal.create"
	// 目标的维护（Agent 能力补齐，2026-09-08）：改字段、四个闭环动作、批量改时间桶与重排、写进展说明
	ActionGoalUpdate    = "goal.update"
	ActionGoalAchieve   = "goal.achieve"
	ActionGoalUnachieve = "goal.unachieve"
	ActionGoalAbandon   = "goal.abandon"
	ActionGoalRestart   = "goal.restart"
	ActionGoalHorizon   = "goal.horizon"
	ActionGoalRank      = "goal.rank"
	ActionGoalNote      = "goal.note"
	// 摘外部链接与挂外部链接同一套规则（ADR 0003 第 35 条）
	ActionTaskExternalLinkRemove = "task.external_link_remove"
	// 解除关联（前置关系对 Agent 一律待确认）；迭代的创建与修改按「创建任务」授权
	ActionTaskUnlink   = "task.unlink"
	ActionSprintCreate = "sprint.create"
	ActionSprintUpdate = "sprint.update"
	ActionTaskTypeSave = "task_type.save"
	ActionSprintStart  = "sprint.start"
	ActionSprintClose  = "sprint.close"
	// 里程碑（ADR 0016）：受「创建目标」授权约束
	ActionMilestoneCreate  = "milestone.create"
	ActionMilestoneUpdate  = "milestone.update"
	ActionMilestoneDelete  = "milestone.delete"
	ActionMilestoneReach   = "milestone.reach"
	ActionMilestoneUnreach = "milestone.unreach"
)

// proposalPermission 是确认某个动作所需的组织权限；空表示只有 Agent 的所有者（和组织负责人）能确认。
var proposalPermission = map[string]string{
	ActionTaskTypeSave: "manage_workflows",
	ActionSprintStart:  "manage_workflows",
	ActionSprintClose:  "manage_workflows",
}

// proposalDraft 是一条还没落库的待确认操作。
type proposalDraft struct {
	Action      string
	Grant       domain.Grant
	TargetKind  string
	TargetID    string
	TargetTitle string
	Payload     any      // 会序列化成 JSON，确认时按同样的结构反序列化回来
	Summary     i18n.Msg // 完整句子：确认后会发生什么
}

// ProposalView 是待确认操作 + 已解析的名字与已渲染的句子。
type ProposalView struct {
	*domain.Proposal
	AgentName     string `json:"agent_name"`
	OwnerName     string `json:"owner_name"`
	DecidedByName string `json:"decided_by_name,omitempty"`
	ActionTitle   string `json:"action_title"`
	SummaryText   string `json:"summary_text"`
	StatusTitle   string `json:"status_title"`
	CanDecide     bool   `json:"can_decide"`
}

// ProposalPending 表示一次写操作没有执行，而是转成了待确认操作。
// 它实现 error，方便沿着现有的返回路径一路传到 HTTP 与 MCP 层。
type ProposalPending struct {
	Proposal *ProposalView
	Message  string // 完整中文句子（按请求者语言）
}

func (p *ProposalPending) Error() string { return p.Message }

// AsProposalPending 判断一个错误是不是"已转成待确认操作"。
func AsProposalPending(err error) (*ProposalPending, bool) {
	var pp *ProposalPending
	if errors.As(err, &pp) {
		return pp, true
	}
	return nil, false
}

// ---------- 工具 ----------

// renderBoth 把一条消息渲染成中英两种文本，存进库里。
func renderBoth(m i18n.Msg) i18n.Text {
	return i18n.Text{i18n.ZhCN: m.Render(i18n.ZhCN), i18n.EnUS: m.Render(i18n.EnUS)}
}

func toPayload(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}
	return m
}

func fromPayload(m map[string]any, v any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func payloadStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func payloadMap(m map[string]any, key string) map[string]any {
	x, _ := m[key].(map[string]any)
	return x
}

// ---------- 记录一条待确认操作 ----------

// createProposal 在当前事务里记下一条待确认操作，写动态并通知确认人。
func (a *App) createProposal(ctx context.Context, tx pgx.Tx, sess *Session, d proposalDraft) (*ProposalView, error) {
	now := time.Now()
	p := &domain.Proposal{
		OrgID: sess.OrgID, AgentID: sess.AgentID, OwnerID: sess.MemberID,
		Action: d.Action, Grant: d.Grant, TargetKind: d.TargetKind, TargetID: d.TargetID, TargetTitle: d.TargetTitle,
		Payload: toPayload(d.Payload), Summary: renderBoth(d.Summary),
		Status: domain.ProposalPending, CreatedAt: now, ExpiresAt: now.Add(domain.ProposalTTL),
	}
	if p.Payload == nil {
		p.Payload = map[string]any{}
	}
	if errs := domain.ValidateProposal(p); len(errs) > 0 {
		return nil, Bad("err.proposal_invalid", strings.Join(domain.RenderErrors(sess.Loc(), errs), "；"))
	}
	if err := a.Store.InsertProposal(ctx, tx, p); err != nil {
		return nil, err
	}
	ev := domain.Event{Type: "ProposalCreated", ActorID: sess.Actor.ID, At: now,
		Data: map[string]any{"proposal_id": p.ID, "action": p.Action, "summary": p.Summary, "agent_id": p.AgentID}}
	if p.TargetKind == "task" {
		ev.TaskID = p.TargetID
	}
	if err := a.Store.InsertEvents(ctx, tx, sess.OrgID, []domain.Event{ev}); err != nil {
		return nil, err
	}
	// 通知确认人（Agent 的所有者）
	loc := a.localeOfMember(ctx, tx, sess.OrgID, p.OwnerID)
	n := domain.Notification{MemberID: p.OwnerID, Title: i18n.Trf(loc, "notif.proposal", sess.Actor.Name), Body: p.Summary.In(loc)}
	if p.TargetKind == "task" {
		n.TaskID = p.TargetID
	}
	// 外发（ADR 0019）：待确认操作按所有者的规则排进投递表，链接指向首页（在「待我处理」里确认）
	if err := a.notifyAndDeliver(ctx, tx, sess.OrgID, domain.NotifyProposal, n, a.deliveryLink("", "")); err != nil {
		return nil, err
	}
	return a.proposalView(ctx, tx, sess, p)
}

// pending 把一条刚记下的待确认操作变成给调用方的返回值。
func pending(sess *Session, v *ProposalView) error {
	return &ProposalPending{Proposal: v, Message: i18n.Trf(sess.Loc(), "proposal.pending_msg", v.OwnerName)}
}

// proposalView 补齐名字与已渲染的句子。
func (a *App) proposalView(ctx context.Context, tx pgx.Tx, sess *Session, p *domain.Proposal) (*ProposalView, error) {
	names, err := a.Store.ExecutorNames(ctx, tx)
	if err != nil {
		return nil, err
	}
	return a.proposalViewWith(sess, p, names), nil
}

func (a *App) proposalViewWith(sess *Session, p *domain.Proposal, names map[string]string) *ProposalView {
	loc := sess.Loc()
	v := &ProposalView{Proposal: p, AgentName: names[p.AgentID], OwnerName: names[p.OwnerID],
		ActionTitle: i18n.Tr(loc, "proposal.action."+p.Action), SummaryText: p.Summary.In(loc),
		StatusTitle: i18n.Tr(loc, "proposal.status."+string(p.Status)), CanDecide: canDecideProposal(sess, p)}
	if v.AgentName == "" {
		v.AgentName = p.AgentID
	}
	if v.OwnerName == "" {
		v.OwnerName = p.OwnerID
	}
	if p.DecidedBy != "" {
		v.DecidedByName = names[p.DecidedBy]
	}
	return v
}

// canDecideProposal 判断当前身份能否确认 / 拒绝这条待确认操作：
// 组织负责人永远可以；Agent 的所有者可以；持有这个动作所需权限的成员也可以。Agent 自己不能确认自己的。
func canDecideProposal(sess *Session, p *domain.Proposal) bool {
	if sess.IsAgent() {
		return false
	}
	if sess.IsOwner || sess.MemberID == p.OwnerID {
		return true
	}
	if perm := proposalPermission[p.Action]; perm != "" && sess.Can(perm) {
		return true
	}
	return false
}

// ---------- 查询 ----------

// ProposalFilter 是待确认操作列表的过滤条件。
type ProposalFilter struct {
	Status  domain.ProposalStatus
	AgentID string
	Mine    bool // 只看等我确认的
	Limit   int
}

// ListProposals 列出待确认操作（默认按创建时间倒序）。
func (a *App) ListProposals(ctx context.Context, sess *Session, f ProposalFilter) ([]*ProposalView, error) {
	out := []*ProposalView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		rows, err := a.Store.ListProposals(ctx, tx, store.ProposalFilter{Status: f.Status, AgentID: f.AgentID, Limit: f.Limit})
		if err != nil {
			return err
		}
		names, err := a.Store.ExecutorNames(ctx, tx)
		if err != nil {
			return err
		}
		for _, p := range rows {
			v := a.proposalViewWith(sess, p, names)
			if f.Mine && !v.CanDecide {
				continue
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

// GetProposal 读取一条待确认操作。
func (a *App) GetProposal(ctx context.Context, sess *Session, id string) (*ProposalView, error) {
	var v *ProposalView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		p, err := a.Store.ProposalByID(ctx, tx, id)
		if err != nil {
			return NotFound("err.proposal_missing")
		}
		v, err = a.proposalView(ctx, tx, sess, p)
		return err
	})
	return v, err
}

// PendingProposalCount 返回等人确认的条数（侧栏提醒用）。
func (a *App) PendingProposalCount(ctx context.Context, sess *Session) (int, error) {
	n := 0
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		n, err = a.Store.CountPendingProposals(ctx, tx)
		return
	})
	return n, err
}

// ---------- 确认与拒绝 ----------

// loadPendingProposal 取出一条还能处理的待确认操作，并校验确认人身份。
func (a *App) loadPendingProposal(ctx context.Context, tx pgx.Tx, sess *Session, id string) (*domain.Proposal, error) {
	p, err := a.Store.ProposalByID(ctx, tx, id)
	if err != nil {
		return nil, NotFound("err.proposal_missing")
	}
	if !canDecideProposal(sess, p) {
		return nil, Forbidden("err.proposal_forbidden")
	}
	if p.Status == domain.ProposalExpired {
		return nil, Bad("err.proposal_expired")
	}
	if !p.Pending() {
		return nil, Bad("err.proposal_decided")
	}
	if time.Now().After(p.ExpiresAt) {
		return nil, Bad("err.proposal_expired")
	}
	return p, nil
}

// ApproveResult 是确认后的结果。
type ApproveResult struct {
	Proposal *ProposalView `json:"proposal"`
	Result   any           `json:"result"`
}

// ApproveProposal 确认并立即执行：以发起的 Agent 身份、把那项授权当作「直接生效」重新执行一次。
// 执行失败时保持 pending，并把失败理由（完整句子）返回给确认人。
func (a *App) ApproveProposal(ctx context.Context, sess *Session, id string) (*ApproveResult, error) {
	if err := refuseDryRun(sess); err != nil {
		return nil, err
	}
	var p *domain.Proposal
	if err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		p, err = a.loadPendingProposal(ctx, tx, sess, id)
		return
	}); err != nil {
		return nil, err
	}
	// 以 Agent 的身份执行，授权按直接生效处理，并在动态里记上「经<确认人>确认」
	agentSess, err := a.agentSession(ctx, p.OrgID, p.AgentID)
	if err != nil {
		return nil, err
	}
	agentSess.Actor = withDirectGrants(agentSess.Actor)
	agentSess.ApprovedByID, agentSess.ApprovedByName = sess.MemberID, sess.Actor.Name
	agentSess.Locale = sess.Loc()

	result, err := a.runProposal(ctx, agentSess, p)
	if err != nil {
		// 保持 pending：把失败理由原样交给确认人
		return nil, err
	}

	var v *ProposalView
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		now := time.Now()
		p.Status, p.DecidedBy, p.DecidedAt = domain.ProposalApproved, sess.MemberID, &now
		if err := a.Store.UpdateProposalDecision(ctx, tx, p); err != nil {
			return err
		}
		ev := domain.Event{Type: "ProposalApproved", ActorID: sess.Actor.ID, At: now,
			Data: map[string]any{"proposal_id": p.ID, "action": p.Action, "summary": p.Summary, "agent_id": p.AgentID}}
		if p.TargetKind == "task" {
			ev.TaskID = p.TargetID
		}
		if err := a.Store.InsertEvents(ctx, tx, sess.OrgID, []domain.Event{ev}); err != nil {
			return err
		}
		v, err = a.proposalView(ctx, tx, sess, p)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &ApproveResult{Proposal: v, Result: result}, nil
}

// RejectProposal 拒绝一条待确认操作。理由必填，且必须是给人和 Agent 直接读的完整句子。
func (a *App) RejectProposal(ctx context.Context, sess *Session, id, reason string) (*ProposalView, error) {
	if err := refuseDryRun(sess); err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	// 与界面同一条规则：太短的理由（"不行"）对 Agent 没有信息量
	if len([]rune(reason)) < 6 {
		return nil, Bad("err.proposal_reason")
	}
	var v *ProposalView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		p, err := a.loadPendingProposal(ctx, tx, sess, id)
		if err != nil {
			return err
		}
		now := time.Now()
		p.Status, p.DecidedBy, p.DecidedAt, p.Reason = domain.ProposalRejected, sess.MemberID, &now, reason
		if err := a.Store.UpdateProposalDecision(ctx, tx, p); err != nil {
			return err
		}
		ev := domain.Event{Type: "ProposalRejected", ActorID: sess.Actor.ID, At: now,
			Data: map[string]any{"proposal_id": p.ID, "action": p.Action, "summary": p.Summary, "agent_id": p.AgentID, "reason": reason}}
		if p.TargetKind == "task" {
			ev.TaskID = p.TargetID
		}
		if err := a.Store.InsertEvents(ctx, tx, sess.OrgID, []domain.Event{ev}); err != nil {
			return err
		}
		v, err = a.proposalView(ctx, tx, sess, p)
		if err != nil {
			return err
		}
		// 让所有者知道结果（拒绝的人不是所有者时）
		if p.OwnerID != sess.MemberID {
			loc := a.localeOfMember(ctx, tx, sess.OrgID, p.OwnerID)
			return a.Store.InsertNotification(ctx, tx, sess.OrgID, domain.Notification{
				MemberID: p.OwnerID,
				Title:    i18n.Trf(loc, "notif.proposal_rejected", v.AgentName),
				Body:     p.Summary.In(loc) + " " + reason,
			})
		}
		return nil
	})
	return v, err
}

// ExpireProposals 把超过有效期还没人确认的待确认操作作废（由后台巡检调用）。
func (a *App) ExpireProposals(ctx context.Context, orgID string) (int, error) {
	n := 0
	err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		stale, err := a.Store.StaleProposals(ctx, tx, time.Now())
		if err != nil {
			return err
		}
		for _, p := range stale {
			now := time.Now()
			p.Status, p.DecidedAt = domain.ProposalExpired, &now
			if err := a.Store.UpdateProposalDecision(ctx, tx, p); err != nil {
				return err
			}
			ev := domain.Event{Type: "ProposalExpired", At: now,
				Data: map[string]any{"proposal_id": p.ID, "action": p.Action, "summary": p.Summary, "agent_id": p.AgentID}}
			if p.TargetKind == "task" {
				ev.TaskID = p.TargetID
			}
			if err := a.Store.InsertEvents(ctx, tx, orgID, []domain.Event{ev}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// ---------- 再执行 ----------

// agentSession 构造某个 Agent 的会话（权限取自它的所有者）。
func (a *App) agentSession(ctx context.Context, orgID, agentID string) (*Session, error) {
	var sess *Session
	err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		ag, err := a.Store.AgentByID(ctx, tx, agentID)
		if err != nil {
			return Bad("err.proposal_agent_gone")
		}
		if ag.RevokedAt != nil {
			return Bad("err.proposal_agent_gone")
		}
		ex, err := a.Store.ExecutorOfAgent(ctx, tx, ag)
		if err != nil {
			return err
		}
		owner, err := a.Store.MemberByID(ctx, tx, ag.OwnerMemberID)
		if err != nil {
			return err
		}
		sess = &Session{OrgID: orgID, MemberID: ag.OwnerMemberID, AccountID: owner.AccountID, AgentID: ag.ID, Actor: ex}
		return a.fillSession(ctx, tx, sess, owner)
	})
	return sess, wrapErr(err)
}

// withDirectGrants 复制一个执行者，把它的全部授权当作「直接生效」。
func withDirectGrants(e *domain.Executor) *domain.Executor {
	c := *e
	c.Grants = map[domain.Grant]domain.GrantMode{}
	for g := range e.Grants {
		c.Grants[g] = domain.GrantDirect
	}
	return &c
}

// runProposal 按记录下来的动作与输入重新执行一次。
func (a *App) runProposal(ctx context.Context, sess *Session, p *domain.Proposal) (any, error) {
	switch p.Action {
	case ActionTaskTransition:
		return a.Transition(ctx, sess, p.TargetID, payloadStr(p.Payload, "transition"),
			TransitionPayload{Comment: payloadStr(p.Payload, "comment"), Result: payloadMap(p.Payload, "result")})
	case ActionTaskClaim:
		return a.Claim(ctx, sess, p.TargetID)
	case ActionTaskBegin:
		return a.Begin(ctx, sess, p.TargetID)
	case ActionTaskAssign:
		return a.Assign(ctx, sess, p.TargetID, payloadStr(p.Payload, "executor_id"))
	case ActionTaskComment:
		return a.AddComment(ctx, sess, p.TargetID, payloadStr(p.Payload, "text"), false)
	case ActionTaskNote:
		return a.AddComment(ctx, sess, p.TargetID, payloadStr(p.Payload, "text"), true)
	case ActionTaskArtifact:
		var art domain.Artifact
		if err := fromPayload(p.Payload, &art); err != nil {
			return nil, Bad("err.proposal_action", p.Action)
		}
		art.ID, art.ByID = "", ""
		return a.AddArtifact(ctx, sess, p.TargetID, art)
	case ActionTaskLink:
		return a.Link(ctx, sess, p.TargetID, domain.RelationType(payloadStr(p.Payload, "type")), payloadStr(p.Payload, "other_id"))
	case ActionTaskExternalLink:
		return a.AddTaskLink(ctx, sess, p.TargetID, LinkInput{Kind: payloadStr(p.Payload, "kind"), URL: payloadStr(p.Payload, "url"), Title: payloadStr(p.Payload, "title")})
	case ActionTaskUnlink:
		return a.Unlink(ctx, sess, p.TargetID, domain.RelationType(payloadStr(p.Payload, "type")), payloadStr(p.Payload, "other_id"))
	case ActionSprintCreate:
		var in CreateSprintInput
		if err := fromPayload(p.Payload, &in); err != nil {
			return nil, Bad("err.proposal_action", p.Action)
		}
		return a.CreateSprint(ctx, sess, in)
	case ActionSprintUpdate:
		var sp sprintPatchPayload
		if err := fromPayload(p.Payload, &sp); err != nil || sp.SprintID == "" {
			return nil, Bad("err.proposal_action", p.Action)
		}
		return a.UpdateSprint(ctx, sess, sp.SprintID, sp.Input)
	case ActionTaskExternalLinkRemove:
		if err := a.RemoveTaskLink(ctx, sess, p.TargetID, payloadStr(p.Payload, "link_id")); err != nil {
			return nil, err
		}
		return map[string]any{"removed": payloadStr(p.Payload, "link_id")}, nil
	case ActionGoalUpdate, ActionGoalAchieve, ActionGoalUnachieve, ActionGoalAbandon, ActionGoalRestart:
		var gp goalPatchPayload
		if err := fromPayload(p.Payload, &gp); err != nil || gp.GoalID == "" {
			return nil, Bad("err.proposal_action", p.Action)
		}
		return a.UpdateGoal(ctx, sess, gp.GoalID, gp.Input)
	case ActionGoalHorizon:
		var in BulkGoalHorizonInput
		if err := fromPayload(p.Payload, &in); err != nil {
			return nil, Bad("err.proposal_action", p.Action)
		}
		return a.BulkGoalHorizon(ctx, sess, in)
	case ActionGoalRank:
		var in GoalRankInput
		if err := fromPayload(p.Payload, &in); err != nil {
			return nil, Bad("err.proposal_action", p.Action)
		}
		return a.SetGoalRanks(ctx, sess, in)
	case ActionGoalNote:
		return a.AddGoalNote(ctx, sess, payloadStr(p.Payload, "goal_id"), payloadStr(p.Payload, "text"))
	case ActionTaskUpdate:
		var in UpdateTaskInput
		if err := fromPayload(p.Payload, &in); err != nil {
			return nil, Bad("err.proposal_action", p.Action)
		}
		return a.UpdateTask(ctx, sess, p.TargetID, in)
	case ActionTaskCreate, ActionTaskCreateSubtask:
		var in CreateTaskInput
		if err := fromPayload(p.Payload, &in); err != nil {
			return nil, Bad("err.proposal_action", p.Action)
		}
		return a.CreateTask(ctx, sess, in)
	case ActionGoalCreate:
		var in CreateGoalInput
		if err := fromPayload(p.Payload, &in); err != nil {
			return nil, Bad("err.proposal_action", p.Action)
		}
		return a.CreateGoal(ctx, sess, in)
	case ActionTaskTypeSave:
		var tt domain.TaskType
		if err := fromPayload(p.Payload, &tt); err != nil {
			return nil, Bad("err.proposal_action", p.Action)
		}
		return a.SaveTaskType(ctx, sess, &tt)
	case ActionSprintStart:
		return a.StartSprint(ctx, sess, p.TargetID)
	case ActionSprintClose:
		return a.CloseSprint(ctx, sess, p.TargetID, payloadStr(p.Payload, "unfinished"), payloadStr(p.Payload, "next_sprint_id"))
	case ActionMilestoneCreate:
		var in CreateMilestoneInput
		if err := fromPayload(p.Payload, &in); err != nil {
			return nil, Bad("err.proposal_action", p.Action)
		}
		return a.CreateMilestone(ctx, sess, in)
	case ActionMilestoneUpdate, ActionMilestoneDelete, ActionMilestoneReach, ActionMilestoneUnreach:
		var mp milestonePayload
		if err := fromPayload(p.Payload, &mp); err != nil || mp.MilestoneID == "" {
			return nil, Bad("err.proposal_action", p.Action)
		}
		switch p.Action {
		case ActionMilestoneUpdate:
			var in UpdateMilestoneInput
			if mp.Input != nil {
				in = *mp.Input
			}
			return a.UpdateMilestone(ctx, sess, mp.MilestoneID, in)
		case ActionMilestoneDelete:
			if err := a.DeleteMilestone(ctx, sess, mp.MilestoneID); err != nil {
				return nil, err
			}
			return map[string]any{"deleted": mp.MilestoneID}, nil
		case ActionMilestoneReach:
			return a.ReachMilestone(ctx, sess, mp.MilestoneID)
		default:
			return a.UnreachMilestone(ctx, sess, mp.MilestoneID)
		}
	}
	return nil, Bad("err.proposal_action", p.Action)
}

// ---------- 供各服务调用的入口 ----------

// proposeOrFail 在自己的事务里记一条待确认操作，并返回给调用方的 ProposalPending。
func (a *App) proposeOrFail(ctx context.Context, sess *Session, build func(tx pgx.Tx) (proposalDraft, error)) error {
	var v *ProposalView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		d, err := build(tx)
		if err != nil {
			return err
		}
		// 只看不做：待确认操作也是一次写入，不能落库；照实说「真做的时候会挂起等谁确认」。
		if sess.Write.DryRun {
			return dryRunPending(sess, a.executorName(ctx, tx, sess.MemberID), d.Summary)
		}
		v, err = a.createProposal(ctx, tx, sess, d)
		return err
	})
	if err != nil {
		return err
	}
	return pending(sess, v)
}

// taskDraft 按内核给出的授权，构造这次任务命令对应的待确认操作。
type taskDraft func(tx pgx.Tx, c *domain.Context, g domain.Grant) (proposalDraft, error)

// taskCommand 执行一次任务命令；命中「需要人确认」时改记一条待确认操作，什么都不改。
// op 是这次操作的名字与归一化参数，只给幂等键用（ADR 0025）。
func (a *App) taskCommand(ctx context.Context, sess *Session, op writeOp, taskID string, draft taskDraft,
	fn func(c *domain.Context) (*domain.Outcome, error)) (*domain.Task, error) {
	return idempotent(ctx, a, sess, op.Name, op.Args, func() (*domain.Task, error) {
		var task *domain.Task
		var v *ProposalView
		err := a.tx(ctx, sess, func(tx pgx.Tx) error {
			c, err := a.loadContext(ctx, tx, sess, taskID)
			if err != nil {
				return err
			}
			o, err := fn(c)
			if err != nil {
				var na *domain.ErrNeedsApproval
				if draft != nil && errors.As(err, &na) {
					d, err := draft(tx, c, na.Grant)
					if err != nil {
						return err
					}
					if sess.Write.DryRun {
						return dryRunPending(sess, a.executorName(ctx, tx, sess.MemberID), d.Summary)
					}
					v, err = a.createProposal(ctx, tx, sess, d)
					return err
				}
				return err
			}
			if err := a.persist(ctx, tx, sess, c, o); err != nil {
				return err
			}
			task = o.Task
			return nil
		})
		if err != nil {
			return nil, err
		}
		if v != nil {
			return nil, pending(sess, v)
		}
		return task, nil
	})
}

// writeOp 是一次写操作的身份：名字（工具名 / 接口）与归一化后的参数，用来算幂等键的指纹。
type writeOp struct {
	Name string
	Args any
}

// insertEvents 写动态；正在执行一条被确认的待确认操作时，给每条动态带上「经<确认人>确认」。
func (a *App) insertEvents(ctx context.Context, tx pgx.Tx, sess *Session, events []domain.Event) error {
	// 只看不做的兜底（ADR 0025 第 2 条）：硬规则是"任何写操作都要产生动态"，所以这里就是所有写路径
	// 的必经之处。走到这里还带着"只看不做"，说明这条路径忘了在写回之前停住——宁可整笔回滚并照实说
	// 一句"还不支持"，也不能一边说"只是看看"一边把东西写进去。
	if sess != nil && sess.Write.DryRun {
		return Bad("err.dry_run_unsupported")
	}
	if sess != nil && sess.ApprovedByName != "" {
		for i := range events {
			if events[i].Data == nil {
				events[i].Data = map[string]any{}
			}
			events[i].Data["approved_by"] = sess.ApprovedByID
			events[i].Data["approved_by_name"] = sess.ApprovedByName
		}
	}
	return a.Store.InsertEvents(ctx, tx, sess.OrgID, events)
}

// transitionTitles 返回一步的起点与终点状态名称（用于待确认操作的说明句子）。
func transitionTitles(c *domain.Context, name string) (from, to i18n.Text) {
	if st := c.Type.Workflow.State(c.Task.State); st != nil {
		from = st.Title
	}
	for _, tr := range c.Type.Workflow.Transitions {
		if tr.Name != name {
			continue
		}
		target := tr.To
		if target == "$previous" {
			target = c.Task.PreviousState
		}
		if st := c.Type.Workflow.State(target); st != nil {
			to = st.Title
		}
	}
	return from, to
}

// executorName 解析一个执行者的名字，解析不出来就用 ID。
func (a *App) executorName(ctx context.Context, tx pgx.Tx, id string) string {
	if id == "" {
		return ""
	}
	if names, err := a.Store.ExecutorNames(ctx, tx); err == nil {
		if n := names[id]; n != "" {
			return n
		}
	}
	return id
}
