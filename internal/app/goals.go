package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// GoalView 是带汇总信息的目标。
type GoalView struct {
	*domain.Goal
	Progress   int         `json:"progress"`
	TaskCount  int         `json:"task_count"`
	DoneCount  int         `json:"done_count"`
	Cost       float64     `json:"cost"`
	OverBudget bool        `json:"over_budget"`
	Children   []*GoalView `json:"children"`
	Start      *time.Time  `json:"start,omitempty"` // 子项聚合的计划开始
	End        *time.Time  `json:"end,omitempty"`   // 子项聚合的计划结束
	// 汇总（ADR 0022 第 6 条）：目标自己没填计划起止时，从子目标与子树里的任务推出来的起止。
	// 只在读时算，永远不写回数据库；目标自己填了就以自己的为准，这两个字段仍然给出推算值供对照。
	DerivedStart *time.Time `json:"derived_start,omitempty"`
	DerivedEnd   *time.Time `json:"derived_end,omitempty"`
	// 里程碑（ADR 0016）：目标自己的，按日期升序；摘要只算自己的里程碑，提示按子树任务算。
	Milestones       []*MilestoneView        `json:"milestones"`
	MilestoneSummary domain.MilestoneSummary `json:"milestone_summary"`
	// Type 是目标类型（ADR 0023），未分类时为 nil。只做分类与显示，不影响这里任何一个汇总数字。
	Type *domain.GoalType `json:"type"`
}

// CreateGoalInput 是创建/修改目标的输入。
type CreateGoalInput struct {
	ParentID string `json:"parent_id"`
	TeamID   string `json:"team_id"`
	// TypeID 是目标类型（ADR 0023），可以不填（未分类）。
	TypeID        string     `json:"type_id"`
	OwnerMemberID string     `json:"owner_member_id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Budget        *float64   `json:"budget"`
	Deadline      *time.Time `json:"deadline"`
	PlannedStart  *time.Time `json:"planned_start"`
	PlannedEnd    *time.Time `json:"planned_end"`
	// 路线图三字段（ADR 0021），都可以不填
	Horizon    domain.GoalHorizon    `json:"horizon"`
	Confidence domain.GoalConfidence `json:"confidence"`
	Outcome    string                `json:"outcome"`
	// 时间线两字段（ADR 0022），也都可以不填
	DatePrecision domain.DatePrecision `json:"date_precision"`
	Rank          *float64             `json:"rank"`
}

// goalPlanErr 把路线图三字段的校验问题变成一句句完整的话。
func goalPlanErr(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return &UserError{Status: 400, Reasons: reasonsOf(errs)}
}

// CreateGoal 创建目标。
func (a *App) CreateGoal(ctx context.Context, sess *Session, in CreateGoalInput) (*domain.Goal, error) {
	return idempotent(ctx, a, sess, "create_goal", in, func() (*domain.Goal, error) {
		return a.createGoal(ctx, sess, in)
	})
}

func (a *App) createGoal(ctx context.Context, sess *Session, in CreateGoalInput) (*domain.Goal, error) {
	if in.Title == "" {
		return nil, Bad("err.title_required")
	}
	in.Outcome = domain.NormalizeOutcome(in.Outcome)
	if err := goalPlanErr(domain.ValidateGoalPlan(in.Horizon, in.Confidence, in.Outcome, in.DatePrecision)); err != nil {
		return nil, err
	}
	if sess.IsAgent() {
		if !sess.Actor.HasGrant(domain.GrantCreateGoal) {
			return nil, Forbidden("err.agent_no_grant", i18n.Key("grant.create_goal"))
		}
		if domain.NeedsApproval(sess.Actor, domain.GrantCreateGoal) {
			return nil, a.proposeOrFail(ctx, sess, func(tx pgx.Tx) (proposalDraft, error) {
				return proposalDraft{Action: ActionGoalCreate, Grant: domain.GrantCreateGoal, TargetKind: "goal", TargetTitle: in.Title,
					Payload: in, Summary: i18n.M("proposal.summary.goal.create", in.Title)}, nil
			})
		}
	}
	owner := in.OwnerMemberID
	if owner == "" {
		owner = sess.MemberID
	}
	g := &domain.Goal{OrgID: sess.OrgID, ParentID: in.ParentID, TeamID: in.TeamID, TypeID: in.TypeID, OwnerMemberID: owner, Title: in.Title, Description: in.Description, Budget: in.Budget, Deadline: in.Deadline, PlannedStart: in.PlannedStart, PlannedEnd: in.PlannedEnd,
		Horizon: in.Horizon, Confidence: in.Confidence, Outcome: in.Outcome, DatePrecision: in.DatePrecision, Rank: in.Rank}
	// 没指定团队时按就近原则归队：上级目标的团队 → 当前正看着的那个团队 → 创建者自己的团队。
	// 否则新目标会落成"未分组"，在按团队筛选的范围里直接消失。
	defaultTeam := in.TeamID == ""
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		var parent *domain.Goal
		if in.ParentID != "" {
			p, err := a.Store.GoalByID(ctx, tx, in.ParentID)
			if err != nil {
				return Bad("err.parent_goal_missing")
			}
			parent = p
		}
		if defaultTeam {
			switch {
			case parent != nil && parent.TeamID != "":
				g.TeamID = parent.TeamID
			case sess.Scope != "" && sess.Scope != "all" && sess.Scope != "mine":
				g.TeamID = sess.Scope
			default:
				if ix, err := a.OrgIndex(ctx, tx); err == nil {
					g.TeamID = ix.TeamOfExecutor(sess.MemberID)
				}
			}
		}
		// 目标类型（ADR 0023）：停用了的类型新建时选不到；类型带了默认时间粒度而这次没显式填粒度时，
		// 在这里预填（应用层的事，不是数据库默认值：它只是「预填」，之后可以逐个目标改）。
		if g.TypeID != "" {
			types, err := a.goalTypesByID(ctx, tx)
			if err != nil {
				return err
			}
			t := types[g.TypeID]
			if t == nil {
				return Bad("err.goal_type_missing")
			}
			if !t.Active {
				return Bad("err.goal_type_inactive", t.Name)
			}
			if in.DatePrecision == "" && t.DefaultPrecision != "" {
				g.DatePrecision = t.DefaultPrecision
			}
		}
		// 只看不做（ADR 0025）：上级、团队、类型都校验过了，就在写库之前停住。
		if sess.Write.DryRun {
			clauses := []i18n.Msg{i18n.M("will.goal.create", g.Title)}
			if g.TeamID != "" {
				if ix, err := a.OrgIndex(ctx, tx); err == nil && ix.TeamName[g.TeamID] != "" {
					clauses = append(clauses, i18n.M("will.goal.team", ix.TeamName[g.TeamID]))
				}
			}
			return dryRun(sess, clauses...)
		}
		if err := a.Store.CreateGoal(ctx, tx, g); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "GoalCreated", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"goal_id": g.ID, "title": g.Title}}})
	})
	return g, err
}

// UpdateGoalInput 是可改字段。指针为 nil 表示不改；要清空可空字段（预算、截止日、计划起止、进度）时把字段名写进 Clear。
// ParentID 指向空字符串表示提为顶级目标。
type UpdateGoalInput struct {
	Title         *string            `json:"title"`
	Description   *string            `json:"description"`
	Status        *domain.GoalStatus `json:"status"`
	OwnerMemberID *string            `json:"owner_member_id"`
	TeamID        *string            `json:"team_id"`
	// TypeID 是目标类型（ADR 0023）：指向空字符串表示清空（未分类）。
	TypeID           *string    `json:"type_id"`
	ParentID         *string    `json:"parent_id"`
	Budget           *float64   `json:"budget"`
	Deadline         *time.Time `json:"deadline"`
	PlannedStart     *time.Time `json:"planned_start"`
	PlannedEnd       *time.Time `json:"planned_end"`
	ProgressOverride *int       `json:"progress_override"`
	// 路线图三字段（ADR 0021）：指向空字符串表示清空（未排期 / 没填信心度 / 去掉成果指标）
	Horizon    *domain.GoalHorizon    `json:"horizon"`
	Confidence *domain.GoalConfidence `json:"confidence"`
	Outcome    *string                `json:"outcome"`
	// 时间线两字段（ADR 0022）：时间粒度传空字符串等同于「到周」；排序权重写进 Clear 表示清空
	DatePrecision *domain.DatePrecision `json:"date_precision"`
	Rank          *float64              `json:"rank"`
	Clear         []string              `json:"clear,omitempty"` // budget | deadline | planned_start | planned_end | progress_override | rank
	// ExpectStatus 是改状态时对「现在是什么状态」的要求（达成只能从进行中来，重新开始只能从已放弃来）。
	// 它随输入一起进待确认操作的载荷，所以人确认时会按目标那时的状态再校验一遍，不会拿旧结论盖掉新状态。
	ExpectStatus []domain.GoalStatus `json:"expect_status,omitempty"`
}

// UpdateGoal 修改目标；只有目标负责人、上级目标负责人或组织负责人可以。
// 每个真正变了的字段各记一条 GoalFieldChanged 动态（带旧值与新值），没有变化就不记。
// 换上级（parent_id）时整棵子树跟着走：目标不能挂到自己或自己的子目标下面，上级必须在调用者看得到的范围里。
func (a *App) UpdateGoal(ctx context.Context, sess *Session, id string, in UpdateGoalInput) (*domain.Goal, error) {
	return idempotent(ctx, a, sess, "update_goal", goalPatchPayload{GoalID: id, Input: in}, func() (*domain.Goal, error) {
		return a.updateGoal(ctx, sess, id, in)
	})
}

// goalPatchPayload 是「对哪个目标改什么」：既是幂等键的指纹，也是待确认操作里记下的载荷。
type goalPatchPayload struct {
	GoalID string          `json:"goal_id"`
	Input  UpdateGoalInput `json:"input"`
}

// goalUpdateMustConfirm 判断 Agent 的这次目标修改要不要先经人确认（ADR 0003 补记）：
// 负责人、上级、团队、状态（达成 / 放弃 / 重新开始）是目标的问责链与结论，Agent 改动一律待确认；
// 其余字段按「创建目标」授权的模式。正在重放一条已确认的待确认操作时不再拦。
func goalUpdateMustConfirm(sess *Session, in UpdateGoalInput) bool {
	if !sess.IsAgent() || sess.ApprovedByID != "" {
		return false
	}
	if in.OwnerMemberID != nil || in.ParentID != nil || in.TeamID != nil || in.Status != nil {
		return true
	}
	return domain.NeedsApproval(sess.Actor, domain.GrantCreateGoal)
}

// goalUpdateDraft 按改的是什么挑动作名与说明句子：状态变化各有自己的名字，好让确认的人一眼看懂。
func goalUpdateDraft(g *domain.Goal, title string, from domain.GoalStatus, in UpdateGoalInput, changed int) (string, i18n.Msg) {
	if in.Status != nil {
		switch *in.Status {
		case domain.GoalAchieved:
			return ActionGoalAchieve, i18n.M("proposal.summary.goal.achieve", title)
		case domain.GoalAbandoned:
			return ActionGoalAbandon, i18n.M("proposal.summary.goal.abandon", title)
		case domain.GoalActive:
			if from == domain.GoalAchieved {
				return ActionGoalUnachieve, i18n.M("proposal.summary.goal.unachieve", title)
			}
			return ActionGoalRestart, i18n.M("proposal.summary.goal.restart", title)
		}
	}
	return ActionGoalUpdate, i18n.M("proposal.summary.goal.update", title, changed)
}

func (a *App) updateGoal(ctx context.Context, sess *Session, id string, in UpdateGoalInput) (*domain.Goal, error) {
	var g *domain.Goal
	var prop *ProposalView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cur, err := a.Store.GoalByID(ctx, tx, id)
		if err != nil {
			return NotFound("err.goal_missing")
		}
		origTitle, fromStatus := cur.Title, cur.Status
		if sess.IsAgent() && !sess.Actor.HasGrant(domain.GrantCreateGoal) {
			return Forbidden("err.agent_no_grant", i18n.Key("grant.create_goal"))
		}
		if !a.canEditGoal(ctx, tx, sess, cur) {
			return Forbidden("err.goal_edit_forbidden")
		}
		if in.Status != nil && len(in.ExpectStatus) > 0 {
			if err := checkGoalStatusChange(sess, cur, *in.Status, in.ExpectStatus); err != nil {
				return err
			}
		}
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		var changes []fieldChange
		if in.Title != nil && *in.Title != cur.Title {
			if *in.Title == "" {
				return Bad("err.title_required")
			}
			changes = append(changes, fieldChange{Field: "title", From: cur.Title, To: *in.Title})
			cur.Title = *in.Title
		}
		if in.Description != nil && *in.Description != cur.Description {
			changes = append(changes, fieldChange{Field: "description", From: cur.Description, To: *in.Description})
			cur.Description = *in.Description
		}
		if in.Status != nil && *in.Status != cur.Status {
			changes = append(changes, fieldChange{Field: "status", From: string(cur.Status), To: string(*in.Status)})
			cur.Status = *in.Status
		}
		if in.OwnerMemberID != nil && *in.OwnerMemberID != cur.OwnerMemberID {
			if *in.OwnerMemberID == "" {
				return Bad("err.member_missing")
			}
			if _, err := a.Store.MemberByID(ctx, tx, *in.OwnerMemberID); err != nil {
				return Bad("err.member_missing")
			}
			changes = append(changes, fieldChange{Field: "owner_member_id", From: cur.OwnerMemberID, To: *in.OwnerMemberID,
				FromTitle: a.executorName(ctx, tx, cur.OwnerMemberID), ToTitle: a.executorName(ctx, tx, *in.OwnerMemberID)})
			cur.OwnerMemberID = *in.OwnerMemberID
		}
		if in.TeamID != nil && *in.TeamID != cur.TeamID {
			names, err := a.teamNames(ctx, tx)
			if err != nil {
				return err
			}
			if *in.TeamID != "" && names[*in.TeamID] == "" {
				return Bad("err.team_missing")
			}
			changes = append(changes, fieldChange{Field: "team_id", From: nilIfEmpty(cur.TeamID), To: nilIfEmpty(*in.TeamID),
				FromTitle: names[cur.TeamID], ToTitle: names[*in.TeamID]})
			cur.TeamID = *in.TeamID
		}
		if in.TypeID != nil && *in.TypeID != cur.TypeID {
			types, err := a.goalTypesByID(ctx, tx)
			if err != nil {
				return err
			}
			if *in.TypeID != "" {
				t := types[*in.TypeID]
				if t == nil {
					return Bad("err.goal_type_missing")
				}
				if !t.Active {
					return Bad("err.goal_type_inactive", t.Name)
				}
			}
			// 名字记进动态：类型之后改名不影响这条历史（ADR 0023 第 6 条）
			changes = append(changes, fieldChange{Field: "type_id", From: nilIfEmpty(cur.TypeID), To: nilIfEmpty(*in.TypeID),
				FromTitle: goalTypeName(types, cur.TypeID), ToTitle: goalTypeName(types, *in.TypeID)})
			cur.TypeID = *in.TypeID
		}
		if in.ParentID != nil && *in.ParentID != cur.ParentID {
			goals, err := a.Store.ListGoals(ctx, tx)
			if err != nil {
				return err
			}
			titles := map[string]string{}
			for _, x := range goals {
				titles[x.ID] = x.Title
			}
			if *in.ParentID != "" {
				var parent *domain.Goal
				for _, x := range goals {
					if x.ID == *in.ParentID {
						parent = x
					}
				}
				if parent == nil {
					return Bad("err.parent_goal_missing")
				}
				if parent.TeamID != "" && !sess.CanSeeCollabTeam(parent.TeamID) {
					return Forbidden("err.parent_goal_hidden")
				}
				if parent.ID == cur.ID || goalDescendants(goals, cur.ID)[parent.ID] {
					return Bad("err.goal_parent_cycle")
				}
			}
			changes = append(changes, fieldChange{Field: "parent_id", From: nilIfEmpty(cur.ParentID), To: nilIfEmpty(*in.ParentID),
				FromTitle: titles[cur.ParentID], ToTitle: titles[*in.ParentID]})
			cur.ParentID = *in.ParentID
		}
		money := map[string]any{"currency": org.Currency}
		switch {
		case contains(in.Clear, "budget"):
			if cur.Budget != nil {
				changes = append(changes, fieldChange{Field: "budget", From: floatVal(cur.Budget), To: nil, Extra: money})
				cur.Budget = nil
			}
		case in.Budget != nil && !sameFloat(in.Budget, cur.Budget):
			changes = append(changes, fieldChange{Field: "budget", From: floatVal(cur.Budget), To: *in.Budget, Extra: money})
			cur.Budget = in.Budget
		}
		dates := []struct {
			field string
			cur   **time.Time
			in    *time.Time
		}{{"deadline", &cur.Deadline, in.Deadline}, {"planned_start", &cur.PlannedStart, in.PlannedStart}, {"planned_end", &cur.PlannedEnd, in.PlannedEnd}}
		for _, d := range dates {
			switch {
			case contains(in.Clear, d.field):
				if *d.cur != nil {
					changes = append(changes, fieldChange{Field: d.field, From: dateVal(*d.cur), To: nil})
					*d.cur = nil
				}
			case d.in != nil && !sameTime(d.in, *d.cur):
				// 截止日在接口层会跟着计划结束一起送来；两者同值同变时只记计划结束这一条
				if d.field == "deadline" && in.PlannedEnd != nil && in.PlannedEnd.Equal(*d.in) {
					*d.cur = d.in
					continue
				}
				changes = append(changes, fieldChange{Field: d.field, From: dateVal(*d.cur), To: dateVal(d.in)})
				*d.cur = d.in
			}
		}
		switch {
		case contains(in.Clear, "progress_override"):
			if cur.ProgressOverride != nil {
				changes = append(changes, fieldChange{Field: "progress_override", From: *cur.ProgressOverride, To: nil})
				cur.ProgressOverride = nil
			}
		case in.ProgressOverride != nil && (cur.ProgressOverride == nil || *cur.ProgressOverride != *in.ProgressOverride):
			var from any
			if cur.ProgressOverride != nil {
				from = *cur.ProgressOverride
			}
			changes = append(changes, fieldChange{Field: "progress_override", From: from, To: *in.ProgressOverride})
			cur.ProgressOverride = in.ProgressOverride
		}
		// 路线图三字段（ADR 0021）：先按输入算出目标值，一起校验，再各记一条动态
		horizon, confidence, outcome := cur.Horizon, cur.Confidence, cur.Outcome
		if in.Horizon != nil {
			horizon = *in.Horizon
		}
		if in.Confidence != nil {
			confidence = *in.Confidence
		}
		if in.Outcome != nil {
			outcome = domain.NormalizeOutcome(*in.Outcome)
		}
		precision := cur.DatePrecision
		if in.DatePrecision != nil {
			precision = domain.NormalizeDatePrecision(*in.DatePrecision)
		}
		if err := goalPlanErr(domain.ValidateGoalPlan(horizon, confidence, outcome, precision)); err != nil {
			return err
		}
		if horizon != cur.Horizon {
			changes = append(changes, fieldChange{Field: "horizon", From: nilIfEmpty(string(cur.Horizon)), To: nilIfEmpty(string(horizon))})
			cur.Horizon = horizon
		}
		if confidence != cur.Confidence {
			changes = append(changes, fieldChange{Field: "confidence", From: nilIfEmpty(string(cur.Confidence)), To: nilIfEmpty(string(confidence))})
			cur.Confidence = confidence
		}
		if outcome != cur.Outcome {
			changes = append(changes, fieldChange{Field: "outcome", From: nilIfEmpty(cur.Outcome), To: nilIfEmpty(outcome)})
			cur.Outcome = outcome
		}
		if precision != cur.DatePrecision {
			changes = append(changes, fieldChange{Field: "date_precision", From: nilIfEmpty(string(cur.DatePrecision)), To: nilIfEmpty(string(precision))})
			cur.DatePrecision = precision
		}
		// 排序权重不记动态：它是泳道里的次序，不是对目标本身的表态，一次拖动会动好几个目标，
		// 记下来只会把动态刷满（ADR 0022 第 4 条）。改了就直接写。
		switch {
		case contains(in.Clear, "rank"):
			if cur.Rank != nil {
				cur.Rank = nil
				changes = append(changes, fieldChange{Field: "rank", Silent: true})
			}
		case in.Rank != nil && !sameFloat(in.Rank, cur.Rank):
			cur.Rank = in.Rank
			changes = append(changes, fieldChange{Field: "rank", Silent: true})
		}
		if len(changes) == 0 {
			// 一个字段都没变：只看不做说「什么都不会变。」，真做也不记动态、不挂待确认操作。
			if sess.Write.DryRun {
				return dryRun(sess)
			}
			g = cur
			return nil
		}
		// Agent 的闸（ADR 0003）：校验都过了才挂待确认操作，免得人确认了一条注定失败的。
		if goalUpdateMustConfirm(sess, in) {
			action, summary := goalUpdateDraft(cur, origTitle, fromStatus, in, len(changes))
			if sess.Write.DryRun {
				return dryRunPending(sess, a.executorName(ctx, tx, sess.MemberID), summary)
			}
			prop, err = a.createProposal(ctx, tx, sess, proposalDraft{Action: action, Grant: domain.GrantCreateGoal, TargetKind: "goal",
				TargetID: cur.ID, TargetTitle: origTitle, Payload: goalPatchPayload{GoalID: cur.ID, Input: in}, Summary: summary})
			return err
		}
		// 只看不做（ADR 0025）：字段算完、权限判完，就在写库之前停住。
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.goal.update", cur.Title, len(changes)))
		}
		if err := a.Store.UpdateGoal(ctx, tx, cur); err != nil {
			return err
		}
		g = cur
		base := map[string]any{"goal_id": cur.ID, "title": cur.Title}
		events := make([]domain.Event, 0, len(changes))
		for _, c := range changes {
			if c.Silent {
				continue
			}
			events = append(events, c.event("GoalFieldChanged", sess, "", base))
		}
		if len(events) == 0 {
			return nil
		}
		return a.insertEvents(ctx, tx, sess, events)
	})
	if err != nil {
		return nil, err
	}
	if prop != nil {
		return nil, pending(sess, prop)
	}
	return g, nil
}

// ChangeGoalStatus 是达成 / 撤销达成 / 放弃 / 重新开始四个闭环动作的共同入口：
// 目标现在的状态配不配这样改（理由是完整句子）由 UpdateGoal 按 ExpectStatus 判，
// 待确认操作重放时同样再判一遍；Agent 一律待确认。from 为空表示不限来源状态。
func (a *App) ChangeGoalStatus(ctx context.Context, sess *Session, id string, to domain.GoalStatus, from []domain.GoalStatus) (*domain.Goal, error) {
	st := to
	expect := from
	if len(expect) == 0 {
		// 不限来源时也要带上「不能已经是它」的检查：把除目标状态外的全部状态列为可来源
		for _, x := range []domain.GoalStatus{domain.GoalDraft, domain.GoalActive, domain.GoalAchieved, domain.GoalAbandoned} {
			if x != to {
				expect = append(expect, x)
			}
		}
	}
	return a.UpdateGoal(ctx, sess, id, UpdateGoalInput{Status: &st, ExpectStatus: expect})
}

// checkGoalStatusChange 判断目标现在的状态允不允许改成 to：已经是 to 了说「什么都不用做」，
// 不在 expect 里说「现在是 X，只有 Y 的目标才能这样改」。
func checkGoalStatusChange(sess *Session, cur *domain.Goal, to domain.GoalStatus, expect []domain.GoalStatus) error {
	loc := sess.Loc()
	if cur.Status == to {
		return Bad("err.goal_status_same", cur.Title, i18n.Tr(loc, "goal.status."+string(to)))
	}
	names := make([]string, 0, len(expect))
	for _, f := range expect {
		if cur.Status == f {
			return nil
		}
		names = append(names, i18n.Tr(loc, "goal.status."+string(f)))
	}
	return Bad("err.goal_status_expect", cur.Title, i18n.Tr(loc, "goal.status."+string(cur.Status)), strings.Join(names, i18n.Tr(loc, "sep.list")))
}

// goalDescendants 返回某个目标的全部子孙（不含自己）。
func goalDescendants(goals []*domain.Goal, rootID string) map[string]bool {
	children := map[string][]string{}
	for _, g := range goals {
		if g.ParentID != "" {
			children[g.ParentID] = append(children[g.ParentID], g.ID)
		}
	}
	out := map[string]bool{}
	stack := []string{rootID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range children[id] {
			if !out[c] {
				out[c] = true
				stack = append(stack, c)
			}
		}
	}
	return out
}

// DeleteGoal 删除一个目标。只有空目标（没有子目标、没有任务）才能删；有内容的目标应当「放弃」，
// 这样历史与动态都留着（与成员注销、任务取消一致：可以停用，不做物理删除）。
func (a *App) DeleteGoal(ctx context.Context, sess *Session, id string) error {
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		g, err := a.Store.GoalByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if !a.canEditGoal(ctx, tx, sess, g) {
			return Forbidden("err.goal_edit_forbidden")
		}
		children, tasks, err := a.Store.GoalUsage(ctx, tx, id)
		if err != nil {
			return err
		}
		if children > 0 || tasks > 0 {
			return Bad("err.goal_not_empty", children, tasks)
		}
		// 只看不做（ADR 0025）：能不能删已经判完了，就在删之前停住。
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.goal.delete", g.Title))
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "GoalDeleted", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"goal_id": g.ID, "title": g.Title}}}); err != nil {
			return err
		}
		return a.Store.DeleteGoal(ctx, tx, id)
	})
}

func (a *App) canEditGoal(ctx context.Context, tx pgx.Tx, sess *Session, g *domain.Goal) bool {
	if sess.IsAgent() && !sess.Actor.HasGrant(domain.GrantCreateGoal) {
		return false
	}
	org, _ := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
	if org != nil && org.OwnerMemberID == sess.MemberID {
		return true
	}
	for cur := g; cur != nil; {
		if cur.OwnerMemberID == sess.MemberID {
			return true
		}
		if cur.ParentID == "" {
			break
		}
		p, err := a.Store.GoalByID(ctx, tx, cur.ParentID)
		if err != nil {
			break
		}
		cur = p
	}
	return false
}

// GoalFilter 是目标树的筛选条件（ADR 0021）。
type GoalFilter struct {
	// Horizon 是时间桶：now | next | later，"none" 表示还没排期；空表示不筛。
	Horizon string
	// Type 是目标类型编号（ADR 0023），"none" 表示未分类；空表示不筛。
	Type string
}

// GoalTree 返回带进度、成本、时间聚合的目标树，按本次请求的范围筛选（范围只是筛选）。
func (a *App) GoalTree(ctx context.Context, sess *Session) ([]*GoalView, error) {
	return a.GoalTreeFiltered(ctx, sess, GoalFilter{})
}

// GoalTreeFiltered 是带筛选的目标树。按时间桶筛时返回的是「命中的那些目标」本身：
// 它们各自做顶级，子树与汇总（进度、成本、里程碑）照旧按整棵子树算；命中的目标之下
// 又命中的目标留在原位，不重复出现。
func (a *App) GoalTreeFiltered(ctx context.Context, sess *Session, f GoalFilter) ([]*GoalView, error) {
	if f.Horizon != "" && !validHorizonFilter(f.Horizon) {
		return nil, Bad("err.goal_horizon_filter", f.Horizon)
	}
	var roots []*GoalView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, err := a.scope(ctx, tx, sess)
		if err != nil {
			return err
		}
		goals, err := a.Store.ListGoals(ctx, tx)
		if err != nil {
			return err
		}
		goals = filterGoals(scope, goals)
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		runs, err := a.Store.AllRuns(ctx, tx)
		if err != nil {
			return err
		}
		roots = buildGoalTree(goals, tasks, types, runs)
		if err := a.attachMilestones(ctx, tx, roots, tasks, types); err != nil {
			return err
		}
		// 目标类型（ADR 0023）：挂上类型对象供显示，未分类的留 nil
		goalTypes, err := a.goalTypesByID(ctx, tx)
		if err != nil {
			return err
		}
		attachGoalTypes(roots, goalTypes)
		if f.Type != "" {
			if f.Type != "none" {
				types, err := a.goalTypesByID(ctx, tx)
				if err != nil {
					return err
				}
				if types[f.Type] == nil {
					return Bad("err.goal_type_filter", f.Type)
				}
			}
			roots = pickBy(roots, func(v *GoalView) bool { return matchGoalType(v.TypeID, f.Type) })
		}
		if f.Horizon != "" {
			roots = pickByHorizon(roots, f.Horizon)
		}
		return nil
	})
	return roots, err
}

// validHorizonFilter 判断筛选值：三档加上 none（还没排期）。
func validHorizonFilter(h string) bool {
	return h == "none" || domain.ValidHorizon(domain.GoalHorizon(h)) && h != ""
}

// pickByHorizon 从整棵树里挑出时间桶命中的目标，作为顶级返回（顺序按创建时间，深度优先）。
func pickByHorizon(tree []*GoalView, h string) []*GoalView {
	return pickBy(tree, func(v *GoalView) bool { return matchHorizon(v.Horizon, h) })
}

// pickBy 从整棵树里挑出命中的目标，作为顶级返回（顺序按创建时间，深度优先）：
// 命中的目标之下又命中的目标留在原位，不重复出现，子树与汇总照旧按整棵子树算。
func pickBy(tree []*GoalView, hit func(*GoalView) bool) []*GoalView {
	out := []*GoalView{}
	var walk func(vs []*GoalView)
	walk = func(vs []*GoalView) {
		for _, v := range vs {
			if hit(v) {
				out = append(out, v)
				continue
			}
			walk(v.Children)
		}
	}
	walk(tree)
	return out
}

// attachGoalTypes 把类型对象挂到整棵树上。
func attachGoalTypes(vs []*GoalView, types map[string]*domain.GoalType) {
	for _, v := range vs {
		if v.TypeID != "" {
			v.Type = types[v.TypeID]
		}
		attachGoalTypes(v.Children, types)
	}
}

// matchGoalType 判断一个目标是不是这一类；want 为 "none" 时匹配未分类的目标。
func matchGoalType(cur, want string) bool {
	if want == "none" {
		return cur == ""
	}
	return cur == want
}

// goalTypeName 取类型当时的名字，用来写进动态；没有类型时是空串。
func goalTypeName(types map[string]*domain.GoalType, id string) string {
	if t := types[id]; t != nil {
		return t.Name
	}
	return ""
}

func matchHorizon(cur domain.GoalHorizon, want string) bool {
	if want == "none" {
		return cur == ""
	}
	return string(cur) == want
}

// GetGoal 返回单个目标及其子树。
func (a *App) GetGoal(ctx context.Context, sess *Session, id string) (*GoalView, error) {
	tree, err := a.GoalTree(ctx, sess)
	if err != nil {
		return nil, err
	}
	var find func(vs []*GoalView) *GoalView
	find = func(vs []*GoalView) *GoalView {
		for _, v := range vs {
			if v.ID == id {
				return v
			}
			if f := find(v.Children); f != nil {
				return f
			}
		}
		return nil
	}
	v := find(tree)
	if v == nil {
		return nil, NotFound("err.goal_missing")
	}
	return v, nil
}

// buildGoalTree 是纯函数：自下而上聚合。
func buildGoalTree(goals []*domain.Goal, tasks []*domain.Task, types map[string]*domain.TaskType, runs []*store.RunRow) []*GoalView {
	views := map[string]*GoalView{}
	for _, g := range goals {
		views[g.ID] = &GoalView{Goal: g, Children: []*GoalView{}, Milestones: []*MilestoneView{}}
	}
	costByTask := map[string]float64{}
	for _, r := range runs {
		costByTask[r.TaskID] += r.Cost
	}
	// 每个目标直接挂的任务：只算顶层任务（子任务的进度由父任务表达）
	type agg struct {
		weight, weighted float64
		count, done      int
		cost             float64
		start, end       *time.Time
	}
	direct := map[string]*agg{}
	for _, t := range tasks {
		if t.GoalID == "" {
			continue
		}
		ag := direct[t.GoalID]
		if ag == nil {
			ag = &agg{}
			direct[t.GoalID] = ag
		}
		ag.cost += costByTask[t.ID]
		if t.ParentID != "" {
			continue
		}
		w := 1.0
		if t.EstimateHours != nil && *t.EstimateHours > 0 {
			w = *t.EstimateHours
		}
		p := 0
		if tt := types[t.TypeName]; tt != nil {
			p = domain.Progress(t, tt)
			if s := tt.Workflow.State(t.State); s != nil && s.Label == domain.LabelTerminalSuccess {
				ag.done++
			}
		}
		ag.weight += w
		ag.weighted += w * float64(p)
		ag.count++
		ag.start = minTime(ag.start, t.PlannedStart)
		ag.end = maxTime(ag.end, t.PlannedEnd)
	}
	// 建树
	var roots []*GoalView
	for _, g := range goals {
		v := views[g.ID]
		if p := views[g.ParentID]; g.ParentID != "" && p != nil {
			p.Children = append(p.Children, v)
		} else {
			roots = append(roots, v)
		}
	}
	var fill func(v *GoalView) (weight, weighted float64)
	fill = func(v *GoalView) (float64, float64) {
		wSum, wdSum := 0.0, 0.0
		if ag := direct[v.ID]; ag != nil {
			wSum, wdSum = ag.weight, ag.weighted
			v.TaskCount, v.DoneCount, v.Cost = ag.count, ag.done, ag.cost
			v.Start, v.End = ag.start, ag.end
		}
		sort.SliceStable(v.Children, func(i, j int) bool { return goalBefore(v.Children[i], v.Children[j]) })
		for _, c := range v.Children {
			cw, cwd := fill(c)
			wSum += cw
			wdSum += cwd
			v.TaskCount += c.TaskCount
			v.DoneCount += c.DoneCount
			v.Cost += c.Cost
			v.Start = minTime(v.Start, c.Start)
			v.End = maxTime(v.End, c.End)
		}
		// 汇总只在读时算（ADR 0022 第 6 条）：到这里 v.Start / v.End 里还只有子目标与任务的日期，
		// 正是「目标自己没填时该画在哪」的答案，先留一份；随后再并进目标自己填的计划起止。
		v.DerivedStart, v.DerivedEnd = v.Start, v.End
		if v.PlannedStart != nil {
			v.Start = minTime(v.Start, v.PlannedStart)
		}
		if v.PlannedEnd != nil {
			v.End = maxTime(v.End, v.PlannedEnd)
		}
		switch {
		case v.Status == domain.GoalAchieved:
			v.Progress = 100
		case v.ProgressOverride != nil:
			v.Progress = *v.ProgressOverride
		case wSum > 0:
			v.Progress = int(wdSum/wSum + 0.5)
		}
		v.OverBudget = v.Budget != nil && v.Cost > *v.Budget
		return wSum, wdSum
	}
	for _, r := range roots {
		fill(r)
	}
	sort.SliceStable(roots, func(i, j int) bool { return goalBefore(roots[i], roots[j]) })
	if roots == nil {
		roots = []*GoalView{}
	}
	return roots
}

// goalBefore 是同一层目标之间的先后（ADR 0022）：先看手工排的次序，再看计划开始，最后按创建时间。
//
//  1. 手工排过的（rank 不为空）排在没排过的前面，彼此按 rank 从小到大——手工次序就是「这个更要紧」的表态；
//  2. 都没手工排过时，填了计划开始的排在前面（早的在前），没填日期的沉到最后；
//  3. 还分不出来就按创建时间。
//
// 这里看的是目标自己填的计划开始，不是从子目标推算的：推算值只用来画条，不参与排序，
// 否则父目标的次序会跟着子目标的日期跳来跳去。
func goalBefore(a, b *GoalView) bool {
	if (a.Rank != nil) != (b.Rank != nil) {
		return a.Rank != nil
	}
	if a.Rank != nil && *a.Rank != *b.Rank {
		return *a.Rank < *b.Rank
	}
	if (a.PlannedStart != nil) != (b.PlannedStart != nil) {
		return a.PlannedStart != nil
	}
	if a.PlannedStart != nil && !a.PlannedStart.Equal(*b.PlannedStart) {
		return a.PlannedStart.Before(*b.PlannedStart)
	}
	return a.CreatedAt.Before(b.CreatedAt)
}

// filterGoals 按范围裁剪目标：目标自己的团队在范围里就留下，上级目标为了保住树形也留下。
func filterGoals(scope *Scope, goals []*domain.Goal) []*domain.Goal {
	if scope == nil || scope.All {
		return goals
	}
	byID := map[string]*domain.Goal{}
	for _, g := range goals {
		byID[g.ID] = g
	}
	keep := map[string]bool{}
	for _, g := range goals {
		if !scope.Includes(g.TeamID) {
			continue
		}
		for cur := g; cur != nil && !keep[cur.ID]; cur = byID[cur.ParentID] {
			keep[cur.ID] = true
			if cur.ParentID == "" {
				break
			}
		}
	}
	out := make([]*domain.Goal, 0, len(goals))
	for _, g := range goals {
		if keep[g.ID] {
			out = append(out, g)
		}
	}
	return out
}

func minTime(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b == nil || a.Before(*b) {
		return a
	}
	return b
}

func maxTime(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b == nil || a.After(*b) {
		return a
	}
	return b
}
