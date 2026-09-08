// next_actions：当前上下文下带编号的可执行动作清单（ADR 0025 第 4 条）。
//
// 只读——只调 app 层的查询，不写任何东西、不产生动态。每条动作都是「编号 + 一句话 + 要调的工具 + 参数」，
// Agent 把清单念给人，人说「第 2 个」，Agent 直接照着调。把自由聊天变成选菜单，是消除歧义最省事的办法。
//
// 清单来自真实状态，不猜：我名下没结束的任务（现在该开始 / 推进 / 提交 / 提问）、我能领的待领取任务、
// 等我验收的任务、我提交的还没人确认的操作；给了 task 就换成「这个任务现在能走的每一步」。
// 顺序按紧急度：逾期 → 进行中 → 等我验收 → 其他在手 → 可领取 → 待确认操作；同组内按优先级、计划结束日、序号排，
// 同样的状态永远排出同样的清单。最多 12 条。
package mcp

import (
	"context"
	"sort"
	"strconv"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

const maxNextActions = 12

var nextActionsDesc = i18n.T(
	"列出你现在可以做的事：带编号的动作清单，每条写明调哪个工具、传什么参数。只读，不改任何东西。人的话说得不清楚时先调它，把清单念给人听，让人报编号，不要自己猜。给了 task 就只列这个任务现在能走的每一步。",
	"List what you can do right now: a numbered list of actions, each naming the tool to call and the arguments to pass. Read-only, changes nothing. When the human's request is ambiguous, call this first, read the list back and let them pick a number instead of guessing. With task given it lists exactly the steps available on that task now.")

// nextActionsIn 是 next_actions 的入参：只有一个可选的任务。
type nextActionsIn struct {
	Task string `json:"task,omitempty" jsonschema:"Task number (#123 / 123) or task ID; omit to list actions across all your work"`
}

// nextAction 是清单里的一条：编号、一句话、要调的工具与参数。
type nextAction struct {
	N         int            `json:"n"`
	Label     string         `json:"label"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

// nextActionsOut 是返回：清单加一句「念给人听，让人报编号」。
type nextActionsOut struct {
	Actions []nextAction `json:"actions"`
	Note    string       `json:"note"`
}

// 紧急度分组，也是排序的第一个键。
const (
	grpOverdue = iota
	grpActive
	grpReview
	grpAssigned
	grpBacklog
)

// candidate 是一条待判断的任务。
type candidate struct {
	group int
	task  app.TaskSummary
}

// buildNextActions 组装清单。
func buildNextActions(ctx context.Context, a *app.App, sess *app.Session, ref string) (*nextActionsOut, error) {
	loc := sess.Loc()
	types, err := a.ListTaskTypes(ctx, sess)
	if err != nil {
		return nil, err
	}
	byType := map[string]*domain.TaskType{}
	for _, tt := range types {
		byType[tt.Name] = tt
	}
	if strings.TrimSpace(ref) != "" {
		return oneTaskActions(ctx, a, sess, byType, ref)
	}

	cands, err := collectCandidates(ctx, a, sess)
	if err != nil {
		return nil, err
	}
	sortCandidates(cands)

	// 待确认操作只占一条，留在最后；先给它留个位置。
	proposals, err := a.ListProposals(ctx, sess, app.ProposalFilter{Status: domain.ProposalPending, AgentID: sess.Actor.ID})
	if err != nil {
		return nil, err
	}
	room := maxNextActions
	if len(proposals) > 0 {
		room--
	}

	out := &nextActionsOut{Actions: []nextAction{}}
	for _, c := range cands {
		if len(out.Actions) >= room {
			break
		}
		w, err := a.Workflow(ctx, sess, c.task.ID)
		if err != nil || w == nil {
			continue // 列得出来却读不到流程：跳过，清单是建议不是断言
		}
		if act := actionFor(loc, byType[c.task.TypeName], c, w); act != nil {
			out.Actions = append(out.Actions, *act)
		}
	}
	if len(proposals) > 0 {
		out.Actions = append(out.Actions, nextAction{
			Label: i18n.T("你有 "+strconv.Itoa(len(proposals))+" 条待确认操作还在等人确认：看看进展，不要重复提交同一个操作。",
				"You have "+strconv.Itoa(len(proposals))+" pending action(s) still awaiting confirmation: check their progress and do not resubmit the same action.").In(loc),
			Tool:      "list_my_proposals",
			Arguments: map[string]any{"status": "pending"},
		})
	}
	number(out)
	out.Note = noteText(loc, len(out.Actions))
	return out, nil
}

// oneTaskActions 给定任务时：正好是 get_workflow 现在说能走的每一步，参数已经填好。
func oneTaskActions(ctx context.Context, a *app.App, sess *app.Session, byType map[string]*domain.TaskType, ref string) (*nextActionsOut, error) {
	loc := sess.Loc()
	id, err := a.ResolveTaskRef(ctx, sess, ref)
	if err != nil {
		return nil, err
	}
	w, err := a.Workflow(ctx, sess, id)
	if err != nil {
		return nil, err
	}
	t, err := a.GetTask(ctx, sess, id)
	if err != nil {
		return nil, err
	}
	r, title := taskRef(t.Number), clip(t.Title)
	out := &nextActionsOut{Actions: []nextAction{}}
	if w.CanClaim {
		out.Actions = append(out.Actions, nextAction{
			Label:     i18n.T("领取 "+r+"「"+title+"」，我来当负责人。", "Claim "+r+" \""+title+"\" and become its assignee.").In(loc),
			Tool:      "claim_task",
			Arguments: map[string]any{"task_id": r},
		})
	}
	if w.CanBegin {
		out.Actions = append(out.Actions, nextAction{
			Label:     i18n.T("开始执行 "+r+"「"+title+"」：开启一段执行记录。", "Begin executing "+r+" \""+title+"\": open an execution record.").In(loc),
			Tool:      "begin_task",
			Arguments: map[string]any{"task_id": r},
		})
	}
	for _, tv := range w.Transitions {
		if !tv.Available || len(out.Actions) >= maxNextActions {
			continue
		}
		act := transitionAction(r, tv)
		act.Label = i18n.T("推进 "+r+"「"+title+"」：走「"+tv.Title+"」。", "Advance "+r+" \""+title+"\": take the \""+tv.Title+"\" step.").In(loc) + suffix(loc, tv)
		out.Actions = append(out.Actions, act)
	}
	if len(out.Actions) > maxNextActions {
		out.Actions = out.Actions[:maxNextActions]
	}
	number(out)
	if len(out.Actions) == 0 {
		out.Note = i18n.T("这个任务现在没有你能走的步骤。用 get_workflow 看每一步不能走的原因。",
			"There is no step you can take on this task right now. Use get_workflow to see why each one is unavailable.").In(loc)
		return out, nil
	}
	out.Note = noteText(loc, len(out.Actions))
	return out, nil
}

// collectCandidates 收齐四类来源的任务。
func collectCandidates(ctx context.Context, a *app.App, sess *app.Session) ([]candidate, error) {
	mine, err := a.MyTasks(ctx, sess)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var cands []candidate
	for _, s := range mine {
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		g := grpAssigned
		switch {
		case s.Overdue:
			g = grpOverdue
		case s.State.Label == domain.LabelActive:
			g = grpActive
		case s.State.Label == domain.LabelWaiting && isMine(sess, s.ReviewerID):
			g = grpReview // 我既是负责人又是验收人：现在等的是我验收
		}
		cands = append(cands, candidate{group: g, task: s})
	}

	// 等我验收：停在等待类型状态、验收人是我（或我的所有者）的任务。
	all, err := a.ListTaskSummaries(ctx, sess, store.TaskFilter{})
	if err != nil {
		return nil, err
	}
	for _, s := range all {
		if seen[s.ID] || s.State.Label != domain.LabelWaiting || !isMine(sess, s.ReviewerID) {
			continue
		}
		seen[s.ID] = true
		cands = append(cands, candidate{group: grpReview, task: s})
	}

	// 可领取：Agent 得有「领取任务」授权才列。
	if !sess.IsAgent() || sess.Actor.HasGrant(domain.GrantClaimBacklog) {
		backlog, err := a.Backlog(ctx, sess)
		if err != nil {
			return nil, err
		}
		for _, s := range backlog {
			if seen[s.ID] {
				continue
			}
			seen[s.ID] = true
			cands = append(cands, candidate{group: grpBacklog, task: s})
		}
	}
	return cands, nil
}

// sortCandidates 按紧急度排：分组 → 优先级 → 计划结束日（没填的排后面）→ 序号。同样的状态排出同样的清单。
func sortCandidates(cs []candidate) {
	sort.SliceStable(cs, func(i, j int) bool {
		x, y := cs[i], cs[j]
		if x.group != y.group {
			return x.group < y.group
		}
		if x.task.Priority != y.task.Priority {
			return x.task.Priority < y.task.Priority
		}
		xe, ye := x.task.PlannedEnd, y.task.PlannedEnd
		if (xe == nil) != (ye == nil) {
			return xe != nil
		}
		if xe != nil && !xe.Equal(*ye) {
			return xe.Before(*ye)
		}
		if x.task.Number != y.task.Number {
			return x.task.Number < y.task.Number
		}
		return x.task.ID < y.task.ID
	})
}

// actionFor 给一条候选任务挑出一条动作；挑不出就不出现在清单里。
func actionFor(loc i18n.Locale, tt *domain.TaskType, c candidate, w *app.WorkflowView) *nextAction {
	r, title := taskRef(c.task.Number), clip(c.task.Title)
	if c.group == grpBacklog {
		if !w.CanClaim {
			return nil
		}
		return &nextAction{
			Label:     i18n.T("领取待领取任务 "+r+"「"+title+"」。", "Claim the unclaimed task "+r+" \""+title+"\".").In(loc),
			Tool:      "claim_task",
			Arguments: map[string]any{"task_id": r},
		}
	}
	if c.group == grpReview {
		tv := pickTransition(tt, w, true)
		if tv == nil {
			return nil
		}
		act := transitionAction(r, *tv)
		act.Label = i18n.T("验收 "+r+"「"+title+"」：走「"+tv.Title+"」。", "Review "+r+" \""+title+"\": take the \""+tv.Title+"\" step.").In(loc) + suffix(loc, *tv)
		return &act
	}
	// 在手的任务：能开执行记录就先开，否则走排在最前的那一步，都没有就先读简报。
	overdue := ""
	if c.group == grpOverdue {
		overdue = i18n.T("已逾期，", "Overdue — ").In(loc)
	}
	if w.CanBegin {
		return &nextAction{
			Label:     overdue + i18n.T("开始执行 "+r+"「"+title+"」：开启一段执行记录。", "Begin executing "+r+" \""+title+"\": open an execution record.").In(loc),
			Tool:      "begin_task",
			Arguments: map[string]any{"task_id": r},
		}
	}
	if tv := pickTransition(tt, w, false); tv != nil {
		act := transitionAction(r, *tv)
		act.Label = overdue + i18n.T("推进 "+r+"「"+title+"」：走「"+tv.Title+"」。", "Advance "+r+" \""+title+"\": take the \""+tv.Title+"\" step.").In(loc) + suffix(loc, *tv)
		return &act
	}
	return &nextAction{
		Label: overdue + i18n.T("读 "+r+"「"+title+"」的任务说明：现在没有你能直接走的步骤，先看清楚还差什么。",
			"Read the brief of "+r+" \""+title+"\": no step is available to you right now, so find out what is missing.").In(loc),
		Tool:      "get_task_brief",
		Arguments: map[string]any{"task_id": r},
	}
}

// transitionAction 把一步渲染成一条动作，参数已经填好；调用方自己写这条的说明。
func transitionAction(r string, tv app.TransitionView) nextAction {
	args := map[string]any{"task_id": r, "name": tv.Name}
	if hasStr(tv.Requires, "comment") {
		args["comment"] = ""
	}
	if hasStr(tv.Requires, "result") {
		args["result"] = map[string]any{}
	}
	return nextAction{Tool: "transition_task", Arguments: args}
}

// suffix 补上这一步的两个提醒：要写一句说明、需要人确认。
func suffix(loc i18n.Locale, tv app.TransitionView) string {
	s := ""
	if hasStr(tv.Requires, "comment") {
		s += i18n.T("这一步要写一句说明。", " This step needs a sentence of explanation.").In(loc)
	}
	if tv.NeedsApproval {
		s += i18n.T("这一步会先记成一条待确认操作，等人确认。", " This step is recorded as a pending action and waits for a human to confirm it.").In(loc)
	}
	return s
}

// pickTransition 从现在能走的步骤里挑一条：review 为真时只认验收那一步。
// 排序只看步骤自己的声明（要交付物、去哪个状态类型、要不要写说明），不认步骤名（ADR 0005）。
func pickTransition(tt *domain.TaskType, w *app.WorkflowView, review bool) *app.TransitionView {
	best, bestRank := -1, 0
	for i, tv := range w.Transitions {
		if !tv.Available {
			continue
		}
		def := transitionDef(tt, tv.Name)
		if def == nil {
			continue
		}
		rank := transitionRank(tt, def)
		if rank == 0 {
			continue
		}
		if review != (def.Grant == domain.GrantReview) {
			continue
		}
		if best < 0 || rank < bestRank {
			best, bestRank = i, rank
		}
	}
	if best < 0 {
		return nil
	}
	return &w.Transitions[best]
}

func transitionDef(tt *domain.TaskType, name string) *domain.Transition {
	if tt == nil {
		return nil
	}
	for i := range tt.Workflow.Transitions {
		if tt.Workflow.Transitions[i].Name == name {
			return &tt.Workflow.Transitions[i]
		}
	}
	return nil
}

// transitionRank 是「该不该主动建议这一步、排多前」：数字越小越靠前，0 表示不主动建议。
// 交付物那一步最靠前；去终止失败状态的（取消、判定非 Bug）与「标记阻塞」永远不主动建议。
func transitionRank(tt *domain.TaskType, def *domain.Transition) int {
	needsComment, hasArtifact := false, false
	for _, r := range def.Requires {
		switch {
		case r == "comment":
			needsComment = true
		case strings.HasPrefix(r, "artifact:"):
			hasArtifact = true
		}
	}
	label := domain.LabelActive // "$previous" 是回到原来那个进行中的状态
	if def.To != "$previous" {
		st := tt.Workflow.State(def.To)
		if st == nil {
			return 0
		}
		label = st.Label
	}
	switch {
	case label == domain.LabelTerminalFailure:
		return 0
	case hasArtifact:
		return 1
	case label == domain.LabelActive && !needsComment:
		return 2
	case def.Grant == domain.GrantReview && label == domain.LabelTerminalSuccess:
		return 3
	case label == domain.LabelActive:
		return 4
	case label == domain.LabelWaiting && needsComment:
		return 5
	case label == domain.LabelPending && !needsComment:
		return 6
	}
	return 0
}

// ---------- 小工具 ----------

func number(out *nextActionsOut) {
	for i := range out.Actions {
		out.Actions[i].N = i + 1
	}
}

func noteText(loc i18n.Locale, n int) string {
	if n == 0 {
		return i18n.T("现在没有等你做的事。要确认可以再看一眼 list_my_tasks 与 list_backlog。",
			"There is nothing waiting for you right now. To double-check, look at list_my_tasks and list_backlog.").In(loc)
	}
	return i18n.T("把这份清单念给人听，让人报编号，你再执行那一条；不要自己替人挑。",
		"Read this list to the human, let them pick a number, then run that one; do not choose for them.").In(loc)
}

func taskRef(n int) string { return "#" + strconv.Itoa(n) }

// clip 把标题截到 30 个字，免得一条动作说明太长。
func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 30 {
		return string(r[:30]) + "…"
	}
	return s
}

func hasStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// isMine 判断某个执行者 ID 是不是我本人或我的所有者。
func isMine(sess *app.Session, id string) bool {
	return id != "" && (id == sess.Actor.ID || id == sess.MemberID)
}

// addNextActions 把 next_actions 挂到服务器上（ADR 0025 第 4 条）。
func addNextActions(s *sdk.Server, a *app.App, sess *app.Session) {
	loc := sess.Loc()
	sdk.AddTool(s, &sdk.Tool{Name: "next_actions", Description: nextActionsDesc.In(loc)},
		func(ctx context.Context, req *sdk.CallToolRequest, in nextActionsIn) (*sdk.CallToolResult, any, error) {
			out, err := buildNextActions(ctx, a, sess, in.Task)
			if err != nil {
				return fail(loc, err)
			}
			return jsonResult(out)
		})
}
