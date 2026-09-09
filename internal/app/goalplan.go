package app

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 目标方案（ADR 0026）：Agent「领取目标」落为一条待确认操作，载荷是整套拆解；
// 目标负责人在网页上逐条勾选后一次落库。目标本身不变成可执行对象。

// PlanTask 是方案里的一个任务。Key 是方案内的键，依赖与上级都用它指；不填就按位置生成 t1、t2…。
type PlanTask struct {
	Key                  string     `json:"key"`
	Title                string     `json:"title"`
	Description          string     `json:"description,omitempty"`
	TypeName             string     `json:"type_name,omitempty"`
	EstimateHours        *float64   `json:"estimate_hours,omitempty"`
	PlannedStart         *time.Time `json:"planned_start,omitempty"`
	PlannedEnd           *time.Time `json:"planned_end,omitempty"`
	Priority             *int       `json:"priority,omitempty"`
	RequiredCapabilities []string   `json:"required_capabilities,omitempty"`
	AssigneeID           string     `json:"assignee_id,omitempty"` // 不填就是提案的 Agent
	ParentKey            string     `json:"parent_key,omitempty"`  // 上级任务（方案内的键）
	DependsOn            []string   `json:"depends_on,omitempty"`  // 前置任务（方案内的键）
	// ADR 0028：验收方式（auto 默认 / human）与参与角色（槽位 → 执行者；不填由负责人填满所有槽）。
	Acceptance   string            `json:"acceptance,omitempty"`
	Participants map[string]string `json:"participants,omitempty"`
}

// PlanTaskCheck 是方案预览里一个任务的可达性预检（ADR 0028 第 4 条）：首步能不能走、走不了的原因。
type PlanTaskCheck struct {
	Key          string            `json:"key"`
	AssigneeID   string            `json:"assignee_id"`
	AssigneeName string            `json:"assignee_name"`
	Acceptance   string            `json:"acceptance"`
	Participants map[string]string `json:"participants"`
	Reachable    bool              `json:"reachable"`
	Reasons      []string          `json:"reasons,omitempty"`
}

// PlanMilestone 是方案里的一条里程碑。
type PlanMilestone struct {
	Key         string     `json:"key"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	DueOn       *time.Time `json:"due_on"`
}

// GoalPlanInput 是一份目标方案。
type GoalPlanInput struct {
	GoalID     string          `json:"goal_id"`
	Rationale  string          `json:"rationale,omitempty"`
	Tasks      []PlanTask      `json:"tasks"`
	Milestones []PlanMilestone `json:"milestones,omitempty"`
	// DeciderID 是确认人（目标负责人），提交时由系统填进载荷，网页与 canDecide 据此判断谁能拍板。
	DeciderID string `json:"decider_id,omitempty"`
}

// ApproveOptions 是确认时的选择：跳过哪些键、把全部任务改派给谁。只有目标方案用它。
type ApproveOptions struct {
	Skip       []string `json:"skip,omitempty"`
	AssigneeID string   `json:"assignee_id,omitempty"`
}

// PlanCreated 是落库后的一条记录。
type PlanCreated struct {
	Key    string `json:"key"`
	ID     string `json:"id"`
	Number int    `json:"number,omitempty"`
	Title  string `json:"title"`
}

// GoalPlanResult 是批准后一次落库的结果。
type GoalPlanResult struct {
	ProposalID string        `json:"proposal_id"`
	GoalID     string        `json:"goal_id"`
	GoalTitle  string        `json:"goal_title"`
	Tasks      []PlanCreated `json:"tasks"`
	Milestones []PlanCreated `json:"milestones"`
	Skipped    []string      `json:"skipped"`
}

// GoalPlanStatus 是 Agent 查方案进展时看到的。
type GoalPlanStatus struct {
	Proposal *ProposalView   `json:"proposal"`
	Result   *GoalPlanResult `json:"result,omitempty"`
}

// planKeys 给没写键的条目按位置发键，并检查重复。
func planKeys(in *GoalPlanInput) error {
	seen := map[string]bool{}
	for i := range in.Tasks {
		t := &in.Tasks[i]
		t.Key = strings.TrimSpace(t.Key)
		if t.Key == "" {
			t.Key = "t" + strconv.Itoa(i+1)
		}
		if seen[t.Key] {
			return Bad("err.plan_key_dup", t.Key)
		}
		seen[t.Key] = true
	}
	for i := range in.Milestones {
		m := &in.Milestones[i]
		m.Key = strings.TrimSpace(m.Key)
		if m.Key == "" {
			m.Key = "m" + strconv.Itoa(i+1)
		}
		if seen[m.Key] {
			return Bad("err.plan_key_dup", m.Key)
		}
		seen[m.Key] = true
	}
	return nil
}

// planOrder 按上级与依赖排出创建顺序（上级先于子任务、前置先于后置），成环时报出环上的键。
func planOrder(tasks []PlanTask) ([]int, error) {
	idx := map[string]int{}
	for i, t := range tasks {
		idx[t.Key] = i
	}
	deps := make([][]int, len(tasks))
	for i, t := range tasks {
		if t.ParentKey != "" {
			j, ok := idx[t.ParentKey]
			if !ok {
				return nil, Bad("err.plan_key_missing", t.ParentKey)
			}
			deps[i] = append(deps[i], j)
		}
		for _, d := range t.DependsOn {
			j, ok := idx[d]
			if !ok {
				return nil, Bad("err.plan_key_missing", d)
			}
			if j == i {
				return nil, Bad("err.plan_cycle", t.Key)
			}
			deps[i] = append(deps[i], j)
		}
	}
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := make([]int, len(tasks))
	var order []int
	var visit func(i int, path []string) error
	visit = func(i int, path []string) error {
		switch color[i] {
		case black:
			return nil
		case grey:
			return Bad("err.plan_cycle", strings.Join(append(path, tasks[i].Key), " → "))
		}
		color[i] = grey
		for _, j := range deps[i] {
			if err := visit(j, append(path, tasks[i].Key)); err != nil {
				return err
			}
		}
		color[i] = black
		order = append(order, i)
		return nil
	}
	for i := range tasks {
		if err := visit(i, nil); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// ProposeGoalPlan 提交一份目标方案：校验完整份方案，记成一条待确认操作，等目标负责人审。
// 只有 Agent 能提（人直接在网页上建任务）；不看授权模式，方案一律经人审（ADR 0026 第 2 条）。
func (a *App) ProposeGoalPlan(ctx context.Context, sess *Session, in GoalPlanInput) (*ProposalView, error) {
	if !sess.IsAgent() {
		return nil, Bad("err.plan_agent_only")
	}
	if len(in.Tasks) == 0 {
		return nil, Bad("err.plan_empty")
	}
	if !sess.Actor.HasGrant(domain.GrantCreateTask) {
		return nil, Forbidden("err.agent_no_grant", i18n.Key("grant.create_task"))
	}
	if len(in.Milestones) > 0 && !sess.Actor.HasGrant(domain.GrantCreateGoal) {
		return nil, Forbidden("err.agent_no_grant", i18n.Key("grant.create_goal"))
	}
	if err := planKeys(&in); err != nil {
		return nil, err
	}
	for i, t := range in.Tasks {
		if strings.TrimSpace(t.Title) == "" {
			return nil, Bad("err.plan_title", i+1)
		}
		if t.ParentKey != "" && !sess.Actor.HasGrant(domain.GrantCreateSubtask) {
			return nil, Forbidden("err.agent_no_grant", i18n.Key("grant.create_subtask"))
		}
	}
	if _, err := planOrder(in.Tasks); err != nil {
		return nil, err
	}
	for i := range in.Tasks {
		in.Tasks[i].Title = strings.TrimSpace(in.Tasks[i].Title)
		if in.Tasks[i].TypeName == "" {
			in.Tasks[i].TypeName = "generic"
		}
		switch in.Tasks[i].Acceptance {
		case "":
			in.Tasks[i].Acceptance = domain.AcceptanceAuto
		case domain.AcceptanceAuto, domain.AcceptanceHuman:
		default:
			return nil, Bad("err.plan_acceptance", i+1)
		}
	}
	for i := range in.Milestones {
		m := &in.Milestones[i]
		m.Title = strings.TrimSpace(m.Title)
		ms := &domain.Milestone{Title: m.Title}
		if m.DueOn != nil {
			d := dateOnly(*m.DueOn)
			m.DueOn = &d
			ms.DueOn = d
		}
		if err := a.validMilestone(sess, ms); err != nil {
			return nil, err
		}
	}
	in.Rationale = strings.TrimSpace(in.Rationale)
	out, err := idempotent(ctx, a, sess, "propose_goal_plan", in, func() (*ProposalView, error) {
		var v *ProposalView
		err := a.tx(ctx, sess, func(tx pgx.Tx) error {
			g, err := a.Store.GoalByID(ctx, tx, in.GoalID)
			if err != nil {
				return NotFound("err.goal_missing")
			}
			if g.TeamID != "" && !sess.CanSeeCollabTeam(g.TeamID) {
				return Forbidden("err.goal_hidden")
			}
			if g.Status == domain.GoalAchieved || g.Status == domain.GoalAbandoned {
				return Bad("err.plan_goal_closed", g.Title, i18n.Tr(sess.Loc(), "goal.status."+string(g.Status)))
			}
			names, err := a.Store.ExecutorNames(ctx, tx)
			if err != nil {
				return err
			}
			for i := range in.Tasks {
				t := &in.Tasks[i]
				tt, err := a.Store.CurrentTaskType(ctx, tx, t.TypeName)
				if err != nil {
					return Bad("err.type_missing", t.TypeName)
				}
				if t.AssigneeID != "" && names[t.AssigneeID] == "" {
					return Bad("err.member_missing")
				}
				// 参与角色：不填由负责人填满所有槽（显式写进方案，人看得见能改）
				assignee := t.AssigneeID
				if assignee == "" {
					assignee = sess.Actor.ID
				}
				if len(tt.Participants) > 0 {
					if t.Participants == nil {
						t.Participants = map[string]string{}
					}
					for _, pt := range tt.Participants {
						if t.Participants[pt.Slot] == "" {
							t.Participants[pt.Slot] = assignee
						}
					}
				}
				for slot, who := range t.Participants {
					pt := tt.Participant(slot)
					if pt == nil {
						return Bad("err.no_participant", tt.Title, slot)
					}
					if names[who] == "" {
						return Bad("err.member_missing")
					}
					if who != assignee {
						ex, err := a.Store.ExecutorByID(ctx, tx, who)
						if err != nil || !hasRole(ex.Roles, pt.Role) {
							return Bad("err.plan_participant_role", i+1, pt.Title.In(sess.Loc()), names[who])
						}
					}
				}
			}
			in.DeciderID = g.OwnerMemberID
			summary := i18n.M("proposal.summary.goal.plan", g.Title, len(in.Tasks), len(in.Milestones), sess.Actor.Name)
			if sess.Write.DryRun {
				return dryRunPending(sess, names[g.OwnerMemberID], summary)
			}
			v, err = a.createProposal(ctx, tx, sess, proposalDraft{Action: ActionGoalPlan, Grant: domain.GrantCreateTask, TargetKind: "goal",
				TargetID: g.ID, TargetTitle: g.Title, Payload: in, Summary: summary, DeciderID: g.OwnerMemberID})
			return err
		})
		return v, err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// applyGoalPlan 是批准后的一次落库：在一个事务里建里程碑、按依赖顺序建任务、建前置关系，
// 跳过的不建，指向它们的依赖与上级引用一并去掉。sess 是提案的 Agent 的会话（授权已按直接生效处理）。
func (a *App) applyGoalPlan(ctx context.Context, sess *Session, p *domain.Proposal, opts ApproveOptions) (*GoalPlanResult, error) {
	var in GoalPlanInput
	if err := fromPayload(p.Payload, &in); err != nil || in.GoalID == "" {
		return nil, Bad("err.proposal_action", p.Action)
	}
	if err := planKeys(&in); err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, t := range in.Tasks {
		known[t.Key] = true
	}
	for _, m := range in.Milestones {
		known[m.Key] = true
	}
	skip := map[string]bool{}
	for _, k := range opts.Skip {
		k = strings.TrimSpace(k)
		if !known[k] {
			return nil, Bad("err.plan_skip_unknown", k)
		}
		skip[k] = true
	}
	var kept []PlanTask
	for _, t := range in.Tasks {
		if skip[t.Key] {
			continue
		}
		if skip[t.ParentKey] {
			t.ParentKey = ""
		}
		var deps []string
		for _, d := range t.DependsOn {
			if !skip[d] {
				deps = append(deps, d)
			}
		}
		t.DependsOn = deps
		kept = append(kept, t)
	}
	if len(kept) == 0 {
		return nil, Bad("err.plan_nothing_left")
	}
	order, err := planOrder(kept)
	if err != nil {
		return nil, err
	}
	res := &GoalPlanResult{ProposalID: p.ID, GoalID: in.GoalID, Tasks: []PlanCreated{}, Milestones: []PlanCreated{}, Skipped: []string{}}
	for _, k := range opts.Skip {
		res.Skipped = append(res.Skipped, strings.TrimSpace(k))
	}
	sort.Strings(res.Skipped)
	var already *GoalPlanResult
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		// 幂等：这份方案已经落过库（GoalPlanApplied 与创建在同一个事务里）就原样返回那次的结果。
		// 落库与「标记已确认」是两个事务，后者失败时人会再点一次确认，不能把整份方案再建一遍。
		if prev, err := a.appliedPlan(ctx, tx, in.GoalID, p.ID); err != nil {
			return err
		} else if prev != nil {
			already = prev
			return nil
		}
		g, err := a.Store.GoalByID(ctx, tx, in.GoalID)
		if err != nil {
			return NotFound("err.goal_missing")
		}
		res.GoalTitle = g.Title
		// 首步走不了的任务不能批准（ADR 0028 第 4 条）：人得先改负责人或参与角色，或勾掉它
		checks, err := a.planChecks(ctx, tx, sess, in, kept, opts.AssigneeID)
		if err != nil {
			return err
		}
		var unreachable []string
		for _, ck := range checks {
			if !ck.Reachable {
				unreachable = append(unreachable, ck.Key+"「"+planTitle(kept, ck.Key)+"」："+strings.Join(ck.Reasons, "，"))
			}
		}
		if len(unreachable) > 0 {
			return Bad("err.plan_unreachable", strings.Join(unreachable, "；"))
		}
		// 确认时改派的负责人也要是本组织里的人或 Agent（提交时只校验了方案里逐条写的）
		if opts.AssigneeID != "" {
			names, err := a.Store.ExecutorNames(ctx, tx)
			if err != nil {
				return err
			}
			if names[opts.AssigneeID] == "" {
				return Bad("err.member_missing")
			}
		}
		for _, m := range in.Milestones {
			if skip[m.Key] {
				continue
			}
			ms := &domain.Milestone{OrgID: sess.OrgID, GoalID: g.ID, Title: m.Title, Description: m.Description, CreatedBy: sess.Actor.ID}
			if m.DueOn != nil {
				ms.DueOn = dateOnly(*m.DueOn)
			}
			if err := a.validMilestone(sess, ms); err != nil {
				return err
			}
			if err := a.insertMilestoneTx(ctx, tx, sess, g, ms); err != nil {
				return err
			}
			res.Milestones = append(res.Milestones, PlanCreated{Key: m.Key, ID: ms.ID, Title: ms.Title})
		}
		// 批准即就绪：任务建出来直接进「待办」，不停在草稿。就绪那一步默认要「执行任务」授权，而提方案的
		// Agent 未必有它——这里是目标负责人刚刚批准的方案，就绪是这次批准的一部分，所以只在这个事务里
		// 给一份带「执行任务」的执行者副本；动态仍记在 Agent 名下并带「经某人确认」。
		ready := *sess
		actor := *sess.Actor
		actor.Grants = map[domain.Grant]domain.GrantMode{}
		for g, m := range sess.Actor.Grants {
			actor.Grants[g] = m
		}
		actor.Grants[domain.GrantExecute] = domain.GrantDirect
		ready.Actor = &actor
		created := map[string]*domain.Task{}
		blocksOf := func(id string) []string {
			ids, _ := a.Store.BlocksOf(ctx, tx, id)
			return ids
		}
		for _, i := range order {
			t := kept[i]
			assignee := t.AssigneeID
			if opts.AssigneeID != "" {
				assignee = opts.AssigneeID
			}
			if assignee == "" {
				assignee = sess.Actor.ID
			}
			participants := map[string]string{}
			for slot, who := range t.Participants {
				if opts.AssigneeID != "" && who == t.AssigneeID || who == "" {
					who = assignee
				}
				participants[slot] = who
			}
			acceptance := t.Acceptance
			if acceptance == "" {
				acceptance = domain.AcceptanceAuto
			}
			ci := CreateTaskInput{GoalID: g.ID, TypeName: t.TypeName, Title: t.Title, Description: t.Description, AssigneeID: assignee,
				RequiredCapabilities: t.RequiredCapabilities, EstimateHours: t.EstimateHours, PlannedStart: t.PlannedStart, PlannedEnd: t.PlannedEnd,
				Priority: t.Priority, Ready: true, Participants: participants, AcceptanceMode: acceptance, PlanID: p.ID}
			if t.ParentKey != "" {
				ci.ParentID = created[t.ParentKey].ID
			}
			task, err := a.createTaskTx(ctx, tx, &ready, ci)
			if err != nil {
				return err
			}
			created[t.Key] = task
			// 批准即委托（ADR 0028 第 4 条）：负责人是 Agent 时，确认人给它发一份这个任务的委托
			if isAgentID(assignee) && sess.ApprovedByID != "" {
				if _, err := a.issueMandate(ctx, tx, sess, task, assignee, sess.ApprovedByID, p.ID, MandateOptions{}); err != nil {
					return err
				}
			}
			res.Tasks = append(res.Tasks, PlanCreated{Key: t.Key, ID: task.ID, Number: task.Number, Title: task.Title})
			// 前置关系：前置任务 blocks 这个任务
			for _, d := range t.DependsOn {
				pre := created[d]
				c, err := a.loadContext(ctx, tx, sess, pre.ID)
				if err != nil {
					return err
				}
				o, err := domain.Link(c, sess.Actor, domain.RelationBlocks, task.ID, blocksOf)
				if err != nil {
					return err
				}
				if err := a.persist(ctx, tx, sess, c, o); err != nil {
					return err
				}
			}
		}
		// 方案里的顺序（不是创建顺序）更好读
		sort.SliceStable(res.Tasks, func(x, y int) bool {
			return planIndex(in.Tasks, res.Tasks[x].Key) < planIndex(in.Tasks, res.Tasks[y].Key)
		})
		ev := domain.Event{Type: "GoalPlanApplied", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{
			"proposal_id": p.ID, "goal_id": g.ID, "title": g.Title, "tasks": toPayload(res)["tasks"], "milestones": toPayload(res)["milestones"],
			"skipped": res.Skipped, "task_count": len(res.Tasks), "milestone_count": len(res.Milestones)}}
		return a.insertEvents(ctx, tx, sess, []domain.Event{ev})
	})
	if err != nil {
		return nil, err
	}
	if already != nil {
		return already, nil
	}
	return res, nil
}

// appliedPlan 找这份方案落库时记的那条 GoalPlanApplied 动态，还原成结果；没有就是还没落过。
func (a *App) appliedPlan(ctx context.Context, tx pgx.Tx, goalID, proposalID string) (*GoalPlanResult, error) {
	rows, err := a.Store.ListEventsByGoal(ctx, tx, goalID, 500)
	if err != nil {
		return nil, err
	}
	for _, e := range rows {
		if e.Type != "GoalPlanApplied" || e.Data["proposal_id"] != proposalID {
			continue
		}
		var r GoalPlanResult
		if err := fromPayload(map[string]any{"proposal_id": proposalID, "goal_id": goalID, "goal_title": e.Data["title"],
			"tasks": e.Data["tasks"], "milestones": e.Data["milestones"], "skipped": e.Data["skipped"]}, &r); err != nil {
			return nil, err
		}
		return &r, nil
	}
	return nil, nil
}

func planIndex(tasks []PlanTask, key string) int {
	for i, t := range tasks {
		if t.Key == key {
			return i
		}
	}
	return len(tasks)
}

// GoalPlanStatusOf 给 Agent 查一份方案的进展：待确认操作本身，批准了就带上落库结果。
func (a *App) GoalPlanStatusOf(ctx context.Context, sess *Session, proposalID string) (*GoalPlanStatus, error) {
	v, err := a.GetProposal(ctx, sess, proposalID)
	if err != nil {
		return nil, err
	}
	if v.Action != ActionGoalPlan {
		return nil, Bad("err.plan_not_plan", proposalID)
	}
	out := &GoalPlanStatus{Proposal: v}
	if v.Status != domain.ProposalApproved {
		return out, nil
	}
	err = a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		out.Result, err = a.appliedPlan(ctx, tx, v.TargetID, proposalID)
		return err
	})
	return out, err
}

func planTitle(tasks []PlanTask, key string) string {
	for _, t := range tasks {
		if t.Key == key {
			return t.Title
		}
	}
	return ""
}

// planChecks 做可达性预检（ADR 0028 第 4 条）：按方案把每个任务在内存里建出来（就绪后的状态），
// 看负责人能不能走它的第一步——只看 by 规则、角色与授权持有，不看前置与交付物（那是做的时候的事）。
func (a *App) planChecks(ctx context.Context, tx pgx.Tx, sess *Session, in GoalPlanInput, tasks []PlanTask, override string) ([]PlanTaskCheck, error) {
	names, err := a.Store.ExecutorNames(ctx, tx)
	if err != nil {
		return nil, err
	}
	var out []PlanTaskCheck
	for _, t := range tasks {
		assignee := t.AssigneeID
		if override != "" {
			assignee = override
		}
		if assignee == "" {
			assignee = sess.Actor.ID
		}
		acceptance := t.Acceptance
		if acceptance == "" {
			acceptance = domain.AcceptanceAuto
		}
		ck := PlanTaskCheck{Key: t.Key, AssigneeID: assignee, AssigneeName: names[assignee], Acceptance: acceptance, Participants: map[string]string{}}
		for slot, who := range t.Participants {
			if who == "" || (override != "" && who == t.AssigneeID) {
				who = assignee
			}
			ck.Participants[slot] = who
		}
		tt, err := a.Store.CurrentTaskType(ctx, tx, t.TypeName)
		if err != nil {
			ck.Reasons = []string{i18n.Trf(sess.Loc(), "err.type_missing", t.TypeName)}
			out = append(out, ck)
			continue
		}
		ex, err := a.Store.ExecutorByID(ctx, tx, assignee)
		if err != nil {
			ck.Reasons = []string{i18n.Tr(sess.Loc(), "err.member_missing")}
			out = append(out, ck)
			continue
		}
		// 在内存里过一遍「创建 + 就绪」：创建者是提方案的 Agent，就绪那一步由它走（同 applyGoalPlan）
		task := &domain.Task{ID: "plan:" + t.Key, TypeName: tt.Name, TypeVersion: tt.Workflow.Version, Title: t.Title, CreatorID: sess.Actor.ID,
			ReviewerID: sess.MemberID, AssigneeID: assignee, State: tt.Workflow.Initial, Participants: ck.Participants, RequiredCapabilities: t.RequiredCapabilities}
		c := &domain.Context{Task: task, Type: tt, Now: time.Now(), NewID: store.NewID}
		creator := *sess.base()
		creator.Grants = map[domain.Grant]domain.GrantMode{}
		for g := range sess.base().Grants {
			creator.Grants[g] = domain.GrantDirect
		}
		creator.Grants[domain.GrantExecute] = domain.GrantDirect
		for _, av := range domain.Available(c, &creator, domain.Payload{}) {
			if to := tt.Workflow.State(av.Transition.To); av.Available && to != nil && to.Label == domain.LabelPending {
				task.State = av.Transition.To
				break
			}
		}
		// 负责人能否走第一步：忽略前置与交付物等要求，只看 by、角色、授权
		var reasons []string
		reachable := false
		for _, av := range domain.Available(c, ex, domain.Payload{}) {
			blocking := []string{}
			for _, r := range av.Reasons {
				switch r.Key {
				case "reject.missing_artifact", "reject.deps_open", "reject.need_comment", "reject.open_bugs", "reject.need_result", "reject.no_previous":
					continue
				}
				blocking = append(blocking, i18n.Trf(sess.Loc(), r.Key, r.Args...))
			}
			if len(blocking) == 0 {
				reachable = true
				break
			}
			reasons = append(reasons, blocking...)
		}
		if !reachable && len(reasons) == 0 {
			reasons = []string{i18n.Tr(sess.Loc(), "err.plan_no_first_step")}
		}
		ck.Reachable = reachable
		if !reachable {
			ck.Reasons = dedupe(reasons)
		}
		out = append(out, ck)
	}
	return out, nil
}

func dedupe(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
