package app

// 委托（ADR 0028）：所有者把一个任务交给一个 Agent 做到底。
//
// 这个文件是委托在应用层的全部：唯一的装载点（executorFor）、发放 / 收回 / 失效、
// 写回后的连带效果（失效核对、自动验收、方案验收）、撤回。
// 「在不在委托内、放不放行」的判定不在这里——在内核 domain.NeedsApprovalFor。

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// mandateEnabled 是功能开关：AXIOMOS_MANDATE=0 关掉委托判定（写路径全部退回 ADR 0003 的逐项判定）。
var mandateEnabled = os.Getenv("AXIOMOS_MANDATE") != "0"

// MandateOptions 是发委托时人能改的部分；空值取组织默认（本期默认写在代码里，见 defaultMandateOptions）。
type MandateOptions struct {
	BudgetCost   *float64        `json:"budget_cost,omitempty"`
	BudgetTokens *int64          `json:"budget_tokens,omitempty"`
	Deadline     *time.Time      `json:"deadline,omitempty"`
	SideEffects  map[string]bool `json:"side_effects,omitempty"`
	AskMe        []string        `json:"ask_me,omitempty"`
}

// defaultMandateOptions 是组织默认的委托模板：对外副作用全关；改负责人、取消、方案外新建要问人。
func defaultMandateOptions() MandateOptions {
	return MandateOptions{SideEffects: map[string]bool{}, AskMe: []string{domain.AskReassign, domain.AskCancel, domain.AskCreateOutsidePlan}}
}

// mergeMandateOptions 把所有者的微调叠在组织默认上：ask_me 只能加不能减（ADR 0028 第 1.3 条）。
func mergeMandateOptions(o MandateOptions) (MandateOptions, error) {
	d := defaultMandateOptions()
	for k, v := range o.SideEffects {
		switch k {
		case domain.SideEffectExternalLink:
			d.SideEffects[k] = v
		default:
			return d, Bad("err.mandate_side_effect", k)
		}
	}
	seen := map[string]bool{}
	for _, a := range d.AskMe {
		seen[a] = true
	}
	for _, a := range o.AskMe {
		a = strings.TrimSpace(a)
		ok := false
		for _, known := range domain.AskMeAll {
			if a == known {
				ok = true
			}
		}
		if !ok {
			return d, Bad("err.mandate_ask", a)
		}
		if !seen[a] {
			d.AskMe = append(d.AskMe, a)
			seen[a] = true
		}
	}
	d.BudgetCost, d.BudgetTokens, d.Deadline = o.BudgetCost, o.BudgetTokens, o.Deadline
	return d, nil
}

// base 返回会话最初的执行者（不带委托）。loadContext 会按任务换上带委托的副本，这里留着原件。
func (s *Session) base() *domain.Executor {
	if s.baseActor != nil {
		return s.baseActor
	}
	return s.Actor
}

// executorFor 是委托的唯一装载点（ADR 0028 第 1.2 条）：Agent 在这个任务上有活动中的委托时，
// 返回一份带委托范围、授权取快照的执行者副本；否则返回原件。读的时候顺手核对失效条件。
func (a *App) executorFor(ctx context.Context, tx pgx.Tx, sess *Session, t *domain.Task) (*domain.Executor, *domain.Mandate, error) {
	base := sess.base()
	if !mandateEnabled || base == nil || !sess.IsAgent() || t == nil {
		return base, nil, nil
	}
	m, err := a.Store.ActiveMandate(ctx, tx, t.ID, sess.AgentID)
	if err != nil || m == nil {
		return base, nil, err
	}
	if reason := mandateStaleReason(m, t); reason != "" {
		if err := a.endMandate(ctx, tx, sess, m, domain.MandateStale, reason); err != nil {
			return nil, nil, err
		}
		return base, nil, nil
	}
	ex := *base
	if len(m.Grants) > 0 {
		ex.Grants = map[domain.Grant]domain.GrantMode{}
		for g, mode := range m.Grants {
			ex.Grants[g] = mode
		}
	}
	ex.Mandate = m.Scope()
	return &ex, m, nil
}

// mandateStaleReason 判断委托是否已失效（ADR 0028 第 1.4 条），返回原因的文案键；空表示仍有效。
func mandateStaleReason(m *domain.Mandate, t *domain.Task) string {
	switch {
	case t.AssigneeID != m.AssigneeID:
		return "mandate.stale.assignee"
	case t.GoalID != m.GoalID: // 没归目标的任务后来归了目标也算换目标（Codex P1）
		return "mandate.stale.goal"
	case m.TypeVersion != 0 && t.TypeVersion != m.TypeVersion:
		return "mandate.stale.type"
	}
	return ""
}

// issueMandate 在当前事务里给某个 Agent 发一份这个任务的委托。issuerID 是发委托的人（必须是人）。
// 已有活动中的委托就原样返回。
func (a *App) issueMandate(ctx context.Context, tx pgx.Tx, sess *Session, t *domain.Task, agentID, issuerID, planID string, opts MandateOptions) (*domain.Mandate, error) {
	if !mandateEnabled || agentID == "" || issuerID == "" {
		return nil, nil
	}
	if existing, err := a.Store.ActiveMandate(ctx, tx, t.ID, agentID); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}
	ag, err := a.Store.AgentByID(ctx, tx, agentID)
	if err != nil || ag.RevokedAt != nil {
		return nil, nil
	}
	o, err := mergeMandateOptions(opts)
	if err != nil {
		return nil, err
	}
	grants := map[domain.Grant]domain.GrantMode{}
	for g, mode := range ag.Grants {
		grants[g] = mode
	}
	m := &domain.Mandate{OrgID: sess.OrgID, TaskID: t.ID, AgentID: agentID, OwnerID: issuerID, PlanID: planID,
		GoalID: t.GoalID, TypeVersion: t.TypeVersion, BudgetCost: o.BudgetCost, BudgetTokens: o.BudgetTokens, Deadline: o.Deadline,
		SideEffects: o.SideEffects, AskMe: o.AskMe, Grants: grants, TaskVersion: t.Version, AssigneeID: t.AssigneeID, Status: domain.MandateActive}
	if err := a.Store.InsertMandate(ctx, tx, m); err != nil {
		return nil, err
	}
	ev := domain.Event{Type: "MandateIssued", TaskID: t.ID, ActorID: issuerID, At: time.Now(),
		Data: map[string]any{"mandate_id": m.ID, "agent_id": agentID, "plan_id": planID, "ask_me": o.AskMe}}
	if err := a.Store.InsertEvents(ctx, tx, sess.OrgID, []domain.Event{ev}); err != nil {
		return nil, err
	}
	return m, nil
}

// endMandate 收回或标记失效，只追加动态。
func (a *App) endMandate(ctx context.Context, tx pgx.Tx, sess *Session, m *domain.Mandate, status, reason string) error {
	if !m.Active() {
		return nil
	}
	if err := a.Store.EndMandate(ctx, tx, m.ID, status, reason); err != nil {
		return err
	}
	m.Status, m.Reason = status, reason
	typ := "MandateStale"
	actor := ""
	if status == domain.MandateRevoked {
		typ = "MandateRevoked"
		if sess != nil && sess.Actor != nil {
			actor = sess.Actor.ID
		}
	}
	ev := domain.Event{Type: typ, TaskID: m.TaskID, ActorID: actor, At: time.Now(),
		Data: map[string]any{"mandate_id": m.ID, "agent_id": m.AgentID, "reason": reason}}
	if err := a.Store.InsertEvents(ctx, tx, sess.OrgID, []domain.Event{ev}); err != nil {
		return err
	}
	if status == domain.MandateStale {
		// 失效要让发委托的人知道：他得决定要不要重新发
		loc := a.localeOfMember(ctx, tx, sess.OrgID, m.OwnerID)
		name := a.executorName(ctx, tx, m.AgentID)
		n := domain.Notification{MemberID: m.OwnerID, TaskID: m.TaskID, Title: i18n.Trf(loc, "notif.mandate_stale", name), Body: i18n.Tr(loc, reason)}
		return a.notifyAndDeliver(ctx, tx, sess.OrgID, "", n, a.deliveryLink(m.TaskID, ""))
	}
	return nil
}

// reconcileMandates 写回任务之后核对它上面的委托：负责人、目标、类型版本变了或任务到终态，委托失效。
func (a *App) reconcileMandates(ctx context.Context, tx pgx.Tx, sess *Session, c *domain.Context, t *domain.Task) error {
	if !mandateEnabled {
		return nil
	}
	ms, err := a.Store.ListMandates(ctx, tx, store.MandateFilter{TaskID: t.ID, OnlyActive: true})
	if err != nil {
		return err
	}
	terminal := false
	if st := c.Type.Workflow.State(t.State); st != nil {
		terminal = st.Label.IsTerminal()
	}
	for _, m := range ms {
		if reason := mandateStaleReason(m, t); reason != "" {
			if err := a.endMandate(ctx, tx, sess, m, domain.MandateStale, reason); err != nil {
				return err
			}
			continue
		}
		if terminal {
			// 任务到终态是委托的自然结束：不记动态、不通知
			if err := a.Store.EndMandate(ctx, tx, m.ID, domain.MandateDone, "mandate.stale.closed"); err != nil {
				return err
			}
		}
	}
	return nil
}

// endMandatesOfAgent 把某个 Agent 名下的活动委托全部置为失效（吊销、所有者停用、授权收回时用）。
func (a *App) endMandatesOfAgent(ctx context.Context, tx pgx.Tx, sess *Session, agentID, reason string, newGrants map[domain.Grant]domain.GrantMode) error {
	ms, err := a.Store.ListMandates(ctx, tx, store.MandateFilter{AgentID: agentID, OnlyActive: true})
	if err != nil {
		return err
	}
	for _, m := range ms {
		if newGrants != nil {
			// 授权变更：只有快照里用到的授权被收回才失效
			lost := false
			for g := range m.Grants {
				if _, ok := newGrants[g]; !ok {
					lost = true
				}
			}
			if !lost {
				continue
			}
		}
		if err := a.endMandate(ctx, tx, sess, m, domain.MandateStale, reason); err != nil {
			return err
		}
	}
	return nil
}

// issuerOf 返回这次写动作背后的人：成员本人，或正在重放一条已确认操作时的确认人；Agent 自己直接做的没有。
func issuerOf(sess *Session) string {
	if sess == nil {
		return ""
	}
	if !sess.IsAgent() {
		return sess.MemberID
	}
	return sess.ApprovedByID
}

// afterWrite 是 persist 写回之后的连带效果（同一个事务）：
//  1. 指派给 Agent 且背后有人 → 发委托；Agent 直接领取 → 记一条「发委托」的待确认操作（领取后问一次）；
//  2. 自动验收；
//  3. 核对委托失效；
//  4. 方案里最后一个任务收尾 → 生成方案验收。
func (a *App) afterWrite(ctx context.Context, tx pgx.Tx, sess *Session, c *domain.Context, o *domain.Outcome) error {
	if !mandateEnabled || sess == nil || sess.Write.DryRun {
		return nil
	}
	t := o.Task
	for _, e := range o.Events {
		switch e.Type {
		case "TaskAssigned":
			to, _ := e.Data["to"].(string)
			if to == "" || !isAgentID(to) {
				continue
			}
			if issuer := issuerOf(sess); issuer != "" {
				if _, err := a.issueMandate(ctx, tx, sess, t, to, issuer, "", MandateOptions{}); err != nil {
					return err
				}
			}
		case "TaskClaimed":
			if !sess.IsAgent() {
				continue
			}
			if issuer := issuerOf(sess); issuer != "" {
				if _, err := a.issueMandate(ctx, tx, sess, t, sess.AgentID, issuer, "", MandateOptions{}); err != nil {
					return err
				}
				continue
			}
			if err := a.proposeMandate(ctx, tx, sess, t); err != nil {
				return err
			}
		}
	}
	// 先试自动验收（可能把任务推到终态），再核对委托失效（到终态即结束），最后看方案是否收尾
	if err := a.tryAutoAccept(ctx, tx, sess, c); err != nil {
		return err
	}
	if err := a.reconcileMandates(ctx, tx, sess, c, t); err != nil {
		return err
	}
	return a.maybePlanReview(ctx, tx, sess, c.Task)
}

func isAgentID(id string) bool { return strings.HasPrefix(id, "agt_") }

// proposeMandate 记一条「给这个 Agent 发委托」的待确认操作（Agent 自己领了任务，领取后问一次）。
func (a *App) proposeMandate(ctx context.Context, tx pgx.Tx, sess *Session, t *domain.Task) error {
	if existing, err := a.Store.ActiveMandate(ctx, tx, t.ID, sess.AgentID); err != nil || existing != nil {
		return err
	}
	if p, err := a.Store.PendingProposalOnTask(ctx, tx, sess.AgentID, "task", t.ID); err != nil {
		return err
	} else if p != nil && p.Action == ActionMandateIssue {
		return nil
	}
	_, err := a.createProposal(ctx, tx, sess, proposalDraft{Action: ActionMandateIssue, Grant: domain.GrantExecute, TargetKind: "task", TargetID: t.ID, TargetTitle: t.Title,
		Payload: map[string]any{"task_id": t.ID, "agent_id": sess.AgentID},
		Summary: i18n.M("proposal.summary.mandate.issue", sess.base().Name, t.Title)})
	return err
}

// applyMandateIssue 重放「发委托」：确认人就是发委托的人。
func (a *App) applyMandateIssue(ctx context.Context, sess *Session, taskID string) (*MandateView, error) {
	var v *MandateView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		t, err := a.Store.TaskByID(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if t.AssigneeID != sess.AgentID {
			return Bad("err.mandate_not_assignee")
		}
		m, err := a.issueMandate(ctx, tx, sess, t, sess.AgentID, sess.ApprovedByID, "", MandateOptions{})
		if err != nil {
			return err
		}
		if m == nil {
			return Bad("err.mandate_not_issued")
		}
		v = a.mandateView(ctx, tx, m)
		return nil
	})
	return v, err
}

// tryAutoAccept 任务标了自动验收、停在等验收的状态、要求都满足时，由系统执行者走验收步（ADR 0028 第 5 条）。
func (a *App) tryAutoAccept(ctx context.Context, tx pgx.Tx, sess *Session, c *domain.Context) error {
	t := c.Task
	if t.AcceptanceMode != domain.AcceptanceAuto {
		return nil
	}
	st := c.Type.Workflow.State(t.State)
	if st == nil || st.Label != domain.LabelWaiting {
		return nil
	}
	name, reasons := domain.AutoAcceptCheck(c)
	if name == "" || len(reasons) > 0 {
		return nil
	}
	o, err := domain.AutoAccept(c)
	if err != nil {
		var rj *domain.Rejection
		if errors.As(err, &rj) {
			return nil
		}
		return err
	}
	return a.persistOutcome(ctx, tx, sess, c, o)
}

// ---------- 方案验收（ADR 0028 第 6 条） ----------

// planFinished 判断一份方案的任务是否都收尾了：到终态，或停在等验收。
func (a *App) planFinished(ctx context.Context, tx pgx.Tx, planID string) (bool, []*domain.Task, error) {
	tasks, err := a.Store.ListTasks(ctx, tx, store.TaskFilter{PlanID: planID})
	if err != nil || len(tasks) == 0 {
		return false, nil, err
	}
	types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
	if err != nil {
		return false, nil, err
	}
	for _, t := range tasks {
		tt := types[t.TypeName]
		if tt == nil {
			return false, nil, nil
		}
		st := tt.Workflow.State(t.State)
		if st == nil {
			return false, nil, nil
		}
		if st.Label.IsTerminal() {
			continue
		}
		if st.Label == domain.LabelWaiting && domain.ReviewStepOf(t, tt, true) != "" {
			continue
		}
		return false, nil, nil
	}
	return true, tasks, nil
}

// maybePlanReview 方案里最后一个任务收尾时生成一份「方案验收」的待确认操作，确认人是目标负责人。
func (a *App) maybePlanReview(ctx context.Context, tx pgx.Tx, sess *Session, t *domain.Task) error {
	if t.PlanID == "" {
		return nil
	}
	done, tasks, err := a.planFinished(ctx, tx, t.PlanID)
	if err != nil || !done {
		return err
	}
	plan, err := a.Store.ProposalByID(ctx, tx, t.PlanID)
	if err != nil {
		return nil // 方案记录不在了就不催验收
	}
	// 已经有一份（等待中或已通过）就不再生成
	existing, err := a.Store.ListProposals(ctx, tx, store.ProposalFilter{Actions: []string{ActionPlanReview}, TargetID: plan.TargetID})
	if err != nil {
		return err
	}
	for _, p := range existing {
		if pid, _ := p.Payload["plan_id"].(string); pid == t.PlanID && (p.Status == domain.ProposalPending || p.Status == domain.ProposalApproved) {
			return nil
		}
	}
	g, err := a.Store.GoalByID(ctx, tx, plan.TargetID)
	if err != nil {
		return err
	}
	types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
	if err != nil {
		return err
	}
	var items []map[string]any
	humanCount := 0
	for _, x := range tasks {
		full, err := a.Store.TaskByID(ctx, tx, x.ID)
		if err != nil {
			return err
		}
		tt := types[x.TypeName]
		awaiting := tt != nil && domain.ReviewStepOf(full, tt, true) != "" && tt.Workflow.State(full.State) != nil && tt.Workflow.State(full.State).Label == domain.LabelWaiting
		if awaiting {
			humanCount++
		}
		items = append(items, map[string]any{"id": full.ID, "number": full.Number, "title": full.Title, "state": full.State,
			"acceptance": full.AcceptanceMode, "awaiting_review": awaiting, "artifacts": toPayload(full.Artifacts)})
	}
	var milestones []map[string]any
	if applied, err := a.appliedPlan(ctx, tx, g.ID, t.PlanID); err == nil && applied != nil {
		for _, m := range applied.Milestones {
			ms, err := a.Store.MilestoneByID(ctx, tx, m.ID)
			if err != nil {
				continue
			}
			milestones = append(milestones, map[string]any{"id": ms.ID, "title": ms.Title, "reached": ms.ReachedAt != nil})
		}
	}
	// 以提方案的 Agent 名义记这条待确认操作（它是这份方案的执行者），确认人是目标负责人
	agentSess, err := a.agentSessionTx(ctx, tx, sess.OrgID, plan.AgentID)
	if err != nil {
		return nil
	}
	d := proposalDraft{Action: ActionPlanReview, Grant: domain.GrantReview, TargetKind: "goal", TargetID: g.ID, TargetTitle: g.Title,
		Payload:   map[string]any{"plan_id": t.PlanID, "goal_id": g.ID, "tasks": items, "milestones": milestones, "human_count": humanCount},
		Summary:   i18n.M("proposal.summary.plan.review", g.Title, len(tasks), humanCount),
		DeciderID: g.OwnerMemberID}
	if _, err := a.createProposal(ctx, tx, agentSess, d); err != nil {
		return err
	}
	ev := domain.Event{Type: "PlanReviewRequested", ActorID: "", At: time.Now(), Data: map[string]any{"goal_id": g.ID, "title": g.Title, "plan_id": t.PlanID, "task_count": len(tasks), "human_count": humanCount}}
	return a.Store.InsertEvents(ctx, tx, sess.OrgID, []domain.Event{ev})
}

// applyPlanReview 重放「方案验收」：等验收的任务逐个验收通过（跳过的不动），里程碑标为已达到（跳过的不动）。
// sess 是 Agent 的重放会话（带确认人）；验收与里程碑都以确认人本人的身份做。
func (a *App) applyPlanReview(ctx context.Context, sess *Session, p *domain.Proposal, opts ApproveOptions) (map[string]any, error) {
	decider := sess.ApprovedByID
	if decider == "" {
		return nil, Forbidden("err.proposal_forbidden")
	}
	human, err := a.memberSession(ctx, sess.OrgID, decider)
	if err != nil {
		return nil, err
	}
	human.Locale = sess.Loc()
	skip := map[string]bool{}
	for _, k := range opts.Skip {
		skip[strings.TrimSpace(k)] = true
	}
	accepted, reached, skipped := []string{}, []string{}, []string{}
	if items, ok := p.Payload["tasks"].([]any); ok {
		for _, it := range items {
			m, _ := it.(map[string]any)
			id, _ := m["id"].(string)
			awaiting, _ := m["awaiting_review"].(bool)
			if !awaiting {
				continue
			}
			if skip[id] {
				skipped = append(skipped, id)
				continue
			}
			if _, err := a.ReviewTask(ctx, human, id, ReviewInput{Decision: "accept", Comment: i18n.Tr(human.Loc(), "plan.review.accept_comment")}); err != nil {
				// 已经不在等验收（比如刚被别人验收）就跳过，其余错误如实返回
				var ue *UserError
				if errors.As(err, &ue) && ue.Status == 409 {
					skipped = append(skipped, id)
					continue
				}
				return nil, err
			}
			accepted = append(accepted, id)
		}
	}
	if items, ok := p.Payload["milestones"].([]any); ok {
		for _, it := range items {
			m, _ := it.(map[string]any)
			id, _ := m["id"].(string)
			if already, _ := m["reached"].(bool); already || skip[id] {
				if skip[id] {
					skipped = append(skipped, id)
				}
				continue
			}
			if _, err := a.ReachMilestone(ctx, human, id); err != nil {
				var ue *UserError
				if errors.As(err, &ue) && (ue.Status == 409 || ue.Status == 404) {
					skipped = append(skipped, id)
					continue
				}
				return nil, err
			}
			reached = append(reached, id)
		}
	}
	sort.Strings(skipped)
	res := map[string]any{"plan_id": p.Payload["plan_id"], "accepted": accepted, "milestones_reached": reached, "skipped": skipped}
	err = a.tx(ctx, human, func(tx pgx.Tx) error {
		ev := domain.Event{Type: "PlanReviewed", ActorID: decider, At: time.Now(), Data: map[string]any{
			"goal_id": p.TargetID, "title": p.TargetTitle, "plan_id": p.Payload["plan_id"], "accepted": len(accepted), "milestones": len(reached), "skipped": len(skipped)}}
		return a.Store.InsertEvents(ctx, tx, human.OrgID, []domain.Event{ev})
	})
	return res, err
}

// ---------- 撤回（ADR 0028 第 3 条） ----------

// RevertTask 所有者在 24 小时内撤回委托内最近一步推进：任务回到那一步之前的状态，只追加动态。
func (a *App) RevertTask(ctx context.Context, sess *Session, taskID, reason string) (*domain.Task, error) {
	if sess.IsAgent() {
		return nil, Forbidden("err.revert_agent")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, Bad("err.revert_reason")
	}
	return idempotent(ctx, a, sess, "revert_task", map[string]any{"task_id": taskID, "reason": reason}, func() (*domain.Task, error) {
		var task *domain.Task
		err := a.tx(ctx, sess, func(tx pgx.Tx) error {
			c, err := a.loadContext(ctx, tx, sess, taskID)
			if err != nil {
				return err
			}
			ev, err := a.revertibleStep(ctx, tx, sess, c.Task)
			if err != nil {
				return err
			}
			from, _ := ev.Data["from"].(string)
			if sess.Write.DryRun {
				title := from
				if st := c.Type.Workflow.State(from); st != nil {
					title = st.Title.In(sess.Loc())
				}
				return dryRun(sess, i18n.M("will.task.revert", taskRefText(c.Task), title))
			}
			o, err := domain.Revert(c, sess.Actor, from, reason)
			if err != nil {
				return err
			}
			o.Events[len(o.Events)-1].Data["event_id"] = ev.ID
			if err := a.persist(ctx, tx, sess, c, o); err != nil {
				return err
			}
			task = o.Task
			return nil
		})
		return task, err
	})
}

// revertibleStep 找最近一步委托内的推进，并核对：24 小时内、之后没有别的推进、任务版本没再变、撤的人有权。
func (a *App) revertibleStep(ctx context.Context, tx pgx.Tx, sess *Session, t *domain.Task) (*store.EventRow, error) {
	rows, err := a.Store.ListEvents(ctx, tx, t.ID, 50)
	if err != nil {
		return nil, err
	}
	for _, e := range rows {
		switch e.Type {
		case "TaskReverted", "TaskAutoAccepted":
			return nil, Bad("err.revert_none")
		case "TaskTransitioned":
			mid, _ := e.Data["mandate_id"].(string)
			if mid == "" {
				return nil, Bad("err.revert_none")
			}
			if time.Since(e.At) > domain.RevertWindow {
				return nil, Bad("err.revert_late")
			}
			if v, ok := e.Data["version"].(float64); ok && int(v) != t.Version {
				return nil, Bad("err.revert_changed")
			}
			m, err := a.Store.MandateByID(ctx, tx, mid)
			if err != nil {
				return nil, Bad("err.revert_none")
			}
			if !(sess.IsOwner || sess.MemberID == m.OwnerID) {
				return nil, Forbidden("err.revert_forbidden")
			}
			return e, nil
		}
	}
	return nil, Bad("err.revert_none")
}

// ---------- 对外 ----------

// MandateView 是给接口与网页看的委托。
type MandateView struct {
	*domain.Mandate
	AgentName string `json:"agent_name"`
	OwnerName string `json:"owner_name"`
	Active    bool   `json:"active"`
}

func (a *App) mandateView(ctx context.Context, tx pgx.Tx, m *domain.Mandate) *MandateView {
	return &MandateView{Mandate: m, AgentName: a.executorName(ctx, tx, m.AgentID), OwnerName: a.executorName(ctx, tx, m.OwnerID), Active: m.Active()}
}

// TaskMandates 列出一个任务上的委托（新的在前）。
func (a *App) TaskMandates(ctx context.Context, sess *Session, taskID string) ([]*MandateView, error) {
	var out []*MandateView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		t, err := a.Store.TaskByID(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if err := a.requireTaskVisible(ctx, tx, sess, t); err != nil {
			return err
		}
		ms, err := a.Store.ListMandates(ctx, tx, store.MandateFilter{TaskID: taskID})
		if err != nil {
			return err
		}
		out = []*MandateView{}
		for _, m := range ms {
			out = append(out, a.mandateView(ctx, tx, m))
		}
		return nil
	})
	return out, err
}

// MandateStatusOf 给 Agent 看它在这个任务上的委托状态（get_workflow / get_task_brief 用）。
type MandateStatus struct {
	InMandate   bool            `json:"in_mandate"`
	MandateID   string          `json:"mandate_id,omitempty"`
	AskMe       []string        `json:"ask_me,omitempty"`
	SideEffects map[string]bool `json:"side_effects,omitempty"`
}

// IssueMandate 人把一个任务委托给某个 Agent（任务负责人必须已是这个 Agent）。
func (a *App) IssueMandate(ctx context.Context, sess *Session, taskID, agentID string, opts MandateOptions) (*MandateView, error) {
	if sess.IsAgent() {
		return nil, Forbidden("err.mandate_agent")
	}
	var v *MandateView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		t, err := a.Store.TaskByID(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if err := a.requireTaskVisible(ctx, tx, sess, t); err != nil {
			return err
		}
		if t.AssigneeID != agentID {
			return Bad("err.mandate_not_assignee")
		}
		ag, err := a.Store.AgentByID(ctx, tx, agentID)
		if err != nil {
			return NotFound("err.agent_missing")
		}
		if !(sess.IsOwner || sess.MemberID == ag.OwnerMemberID || sess.CanManageAgent(ag)) {
			return Forbidden("err.mandate_forbidden")
		}
		if _, err := mergeMandateOptions(opts); err != nil {
			return err
		}
		// 只看不做（ADR 0025）：校验都过了，写库之前停住
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.mandate.issue", taskRefText(t), ag.Name))
		}
		m, err := a.issueMandate(ctx, tx, sess, t, agentID, sess.MemberID, "", opts)
		if err != nil {
			return err
		}
		if m == nil {
			return Bad("err.mandate_not_issued")
		}
		v = a.mandateView(ctx, tx, m)
		return nil
	})
	return v, err
}

// RevokeMandate 所有者收回委托。
func (a *App) RevokeMandate(ctx context.Context, sess *Session, id, reason string) (*MandateView, error) {
	if sess.IsAgent() {
		return nil, Forbidden("err.mandate_agent")
	}
	var v *MandateView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		m, err := a.Store.MandateByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if !(sess.IsOwner || sess.MemberID == m.OwnerID) {
			return Forbidden("err.mandate_forbidden")
		}
		if !m.Active() {
			return Bad("err.mandate_ended")
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.mandate.revoke", a.executorName(ctx, tx, m.AgentID)))
		}
		if err := a.endMandate(ctx, tx, sess, m, domain.MandateRevoked, reason); err != nil {
			return err
		}
		v = a.mandateView(ctx, tx, m)
		return nil
	})
	return v, err
}
