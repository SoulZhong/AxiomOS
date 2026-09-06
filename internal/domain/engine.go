package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// Context 是内核评估一个任务时需要的全部材料，由应用层装载。
type Context struct {
	Task         *Task
	Type         *TaskType
	Predecessors []*Task              // 以本任务为后置的前置任务（它们 blocks 本任务）
	Bugs         []*Task              // 「发现于」本任务的 Bug 类型任务
	Types        map[string]*TaskType // 前置任务与 Bug 的任务类型，按名字
	ActiveRun    *Run                 // 本任务进行中的执行记录，可为 nil
	ActorRuns    int                  // 触发者当前进行中的执行记录数（并发上限用）
	Now          time.Time
	NewID        func(prefix string) string
}

func (c *Context) now() time.Time {
	if c.Now.IsZero() {
		return time.Now()
	}
	return c.Now
}

func (c *Context) newID(prefix string) string {
	if c.NewID != nil {
		return c.NewID(prefix)
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func (c *Context) typeOf(t *Task) *TaskType {
	if t == c.Task {
		return c.Type
	}
	if tt, ok := c.Types[t.TypeName]; ok {
		return tt
	}
	return nil
}

func (c *Context) stateOf(t *Task) *State {
	tt := c.typeOf(t)
	if tt == nil {
		return nil
	}
	return tt.Workflow.State(t.State)
}

func (c *Context) labelOf(t *Task) Label {
	if s := c.stateOf(t); s != nil {
		return s.Label
	}
	return ""
}

// Payload 是触发一步时随身带的东西。
type Payload struct {
	Comment string
	Result  map[string]any
}

// Reason 是一条可翻译的原因。
type Reason = i18n.Msg

// Availability 描述一步现在能不能走、不能走的原因。
// NeedsApproval 不是拒绝理由：这一步照样提供给触发者，只是走它会先记成一条待确认操作（ADR 0003）。
type Availability struct {
	Transition    Transition
	Available     bool
	NeedsApproval bool
	Reasons       []Reason
}

// Outcome 是一次命令的全部副作用，由应用层持久化。
type Outcome struct {
	Task      *Task
	ClosedRun *Run
	OpenedRun *Run
	Events    []Event
}

func (o *Outcome) emit(c *Context, typ, actorID string, data map[string]any) {
	o.Events = append(o.Events, Event{Type: typ, TaskID: c.Task.ID, ActorID: actorID, At: c.now(), Data: data})
}

// Rejection 是面向人的拒绝理由（可翻译）。
type Rejection struct{ Reasons []Reason }

// Error 用默认语言渲染。
func (r *Rejection) Error() string { return r.Render(i18n.Default) }

// Render 按语言渲染，多条用分号连接。
func (r *Rejection) Render(l i18n.Locale) string {
	parts := make([]string, len(r.Reasons))
	for i, m := range r.Reasons {
		parts[i] = m.Render(l)
	}
	return strings.Join(parts, "；")
}

func reject(key string, args ...any) error {
	return &Rejection{Reasons: []Reason{i18n.M(key, args...)}}
}

// RenderReasons 按语言渲染一组原因。
func RenderReasons(l i18n.Locale, rs []Reason) []string {
	out := make([]string, 0, len(rs))
	for _, m := range rs {
		out = append(out, m.Render(l))
	}
	return out
}

// ---------- 身份 ----------

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func isPrincipal(actor *Executor, id string) bool {
	return id != "" && contains(actor.Principals(), id)
}

// matchesBy 判断触发者是否满足任一规则。Agent 的组织角色由应用层复制自所有者。
func matchesBy(t *Task, actor *Executor, by []string) bool {
	for _, rule := range by {
		switch {
		case rule == "anyone":
			return true
		case rule == "creator":
			if isPrincipal(actor, t.CreatorID) {
				return true
			}
		case rule == "assignee":
			if isPrincipal(actor, t.AssigneeID) {
				return true
			}
		case rule == "reviewer":
			if isPrincipal(actor, t.ReviewerID) {
				return true
			}
		case strings.HasPrefix(rule, "participant:"):
			if isPrincipal(actor, t.Participants[strings.TrimPrefix(rule, "participant:")]) {
				return true
			}
		case strings.HasPrefix(rule, "role:"):
			if contains(actor.Roles, strings.TrimPrefix(rule, "role:")) {
				return true
			}
		}
	}
	return false
}

// RoleTitle 由应用层注入：角色代码名 → 多语言名称。
var RoleTitle = func(role string) i18n.Text {
	for _, r := range BuiltinRoles {
		if r.Name == role {
			return r.Title
		}
	}
	return i18n.Text{i18n.Default: role, i18n.EnUS: role}
}

// DescribeRule 把触发者规则翻译成可渲染的消息。
func DescribeRule(rule string, tt *TaskType) Reason {
	switch {
	case rule == "creator", rule == "assignee", rule == "reviewer", rule == "anyone":
		return i18n.M("rule." + rule)
	case strings.HasPrefix(rule, "participant:"):
		slot := strings.TrimPrefix(rule, "participant:")
		if tt != nil {
			if p := tt.Participant(slot); p != nil {
				return i18n.M("rule.participant", p.Title)
			}
		}
		return i18n.M("rule.participant", slot)
	case strings.HasPrefix(rule, "role:"):
		return i18n.M("rule.role", RoleTitle(strings.TrimPrefix(rule, "role:")))
	}
	return i18n.M("ev.generic", rule, "")
}

// ArtifactTitle 返回交付物类型的多语言名；应用层可替换以支持组织自定义类型。
var ArtifactTitle = func(typ string) i18n.Text {
	if t, ok := BuiltinArtifactTitles[typ]; ok {
		return t
	}
	return i18n.Text{i18n.Default: typ, i18n.EnUS: typ}
}

// ---------- 前提条件 ----------

func (c *Context) checkCondition(cond string, p Payload) *Reason {
	t := c.Task
	switch {
	case strings.HasPrefix(cond, "artifact:"):
		typ := strings.TrimPrefix(cond, "artifact:")
		for _, a := range t.Artifacts {
			if a.Type == typ {
				return nil
			}
		}
		m := i18n.M("reject.missing_artifact", ArtifactTitle(typ))
		return &m
	case cond == "deps_done":
		var open []*Task
		for _, pre := range c.Predecessors {
			if c.labelOf(pre) != LabelTerminalSuccess {
				open = append(open, pre)
			}
		}
		if len(open) > 0 {
			m := i18n.M("reject.deps_open", titles(open))
			return &m
		}
	case cond == "no_open_bugs":
		var open []*Task
		for _, b := range c.Bugs {
			if !c.labelOf(b).IsTerminal() {
				open = append(open, b)
			}
		}
		if len(open) > 0 {
			m := i18n.M("reject.open_bugs", titles(open))
			return &m
		}
	case cond == "comment":
		if strings.TrimSpace(p.Comment) == "" {
			m := i18n.M("reject.need_comment")
			return &m
		}
	case cond == "result":
		if p.Result == nil {
			m := i18n.M("reject.need_result")
			return &m
		}
	}
	return nil
}

// titles 按任务 ID（创建顺序）稳定排序后返回标题列表（渲染时按语言分隔符连接）。
func titles(ts []*Task) []string {
	sort.Slice(ts, func(i, j int) bool { return ts[i].ID < ts[j].ID })
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Title
	}
	return out
}

// ---------- 查询 ----------

// Progress 按规格 §2.2 计算任务进度。
func Progress(t *Task, tt *TaskType) int {
	s := tt.Workflow.State(t.State)
	if s == nil {
		return 0
	}
	switch s.Label {
	case LabelTerminalSuccess:
		return 100
	case LabelTerminalFailure:
		return 0
	}
	if s.Weight != nil {
		return *s.Weight
	}
	return t.LastWeight
}

// joinMsgs 把多条消息合成一个用 " / " 连接的参数。
type joinMsgs []i18n.Msg

// Render 实现 i18n 参数渲染。
func (j joinMsgs) Render(l i18n.Locale) string {
	parts := make([]string, len(j))
	for i, m := range j {
		parts[i] = m.Render(l)
	}
	return strings.Join(parts, " / ")
}

// grantOf 返回触发一步所需的授权，默认「执行任务」。
func grantOf(tr Transition) Grant {
	if tr.Grant == "" {
		return GrantExecute
	}
	return tr.Grant
}

// Available 列出当前状态下的全部步骤及触发者能否走。
func Available(c *Context, actor *Executor, p Payload) []Availability {
	t, wf := c.Task, &c.Type.Workflow
	cur := wf.State(t.State)
	var out []Availability
	for _, tr := range wf.Transitions {
		applies := false
		for _, f := range tr.From {
			if f == "*" {
				applies = cur != nil && !cur.Label.IsTerminal()
			} else if f == t.State {
				applies = true
			}
		}
		if !applies {
			continue
		}
		var reasons []Reason
		if !matchesBy(t, actor, tr.By) {
			names := make(joinMsgs, len(tr.By))
			for i, b := range tr.By {
				names[i] = DescribeRule(b, c.Type)
			}
			reasons = append(reasons, i18n.M("reject.not_by", names))
		}
		g := grantOf(tr)
		if !actor.HasGrant(g) {
			reasons = append(reasons, i18n.M("reject.agent_no_grant", i18n.Key("grant."+string(g))))
		}
		for _, cond := range tr.Requires {
			if r := c.checkCondition(cond, p); r != nil {
				reasons = append(reasons, *r)
			}
		}
		if tr.To == "$previous" && t.PreviousState == "" {
			reasons = append(reasons, i18n.M("reject.no_previous"))
		}
		out = append(out, Availability{Transition: tr, Available: len(reasons) == 0, NeedsApproval: NeedsApproval(actor, g), Reasons: reasons})
	}
	return out
}

// CanBegin 判断触发者现在能否开始执行。
func CanBegin(c *Context, actor *Executor) []Reason {
	t := c.Task
	s := c.Type.Workflow.State(t.State)
	var reasons []Reason
	switch {
	case s != nil && s.Label.IsTerminal():
		reasons = append(reasons, i18n.M("reject.task_closed"))
	case s == nil || s.Label != LabelActive:
		reasons = append(reasons, i18n.M("reject.not_active"))
	}
	if !isPrincipal(actor, t.AssigneeID) {
		reasons = append(reasons, i18n.M("reject.begin_not_assignee"))
	}
	if c.ActiveRun != nil {
		reasons = append(reasons, i18n.M("reject.run_exists"))
	}
	if actor.Kind == ExecutorAgent {
		if !actor.HasGrant(GrantExecute) {
			reasons = append(reasons, i18n.M("reject.agent_no_grant", i18n.Key("grant.execute")))
		}
		if t.HumanOnly {
			reasons = append(reasons, i18n.M("reject.human_only"))
		}
		if c.ActorRuns >= maxConcurrent(actor) {
			reasons = append(reasons, i18n.M("reject.concurrency"))
		}
	}
	return reasons
}

// CanClaim 判断触发者现在能否领取。
func CanClaim(c *Context, actor *Executor) []Reason {
	t := c.Task
	s := c.Type.Workflow.State(t.State)
	var reasons []Reason
	if t.AssigneeID != "" {
		reasons = append(reasons, i18n.M("reject.has_assignee"))
	}
	if s != nil && s.Label.IsTerminal() {
		reasons = append(reasons, i18n.M("reject.task_closed"))
	} else if s == nil || (!s.Claimable && s.Label != LabelActive) {
		var title any = t.State
		if s != nil {
			title = s.Title
		}
		reasons = append(reasons, i18n.M("reject.not_claimable", title))
	}
	if t.RequiredRole != "" && !contains(actor.Roles, t.RequiredRole) {
		reasons = append(reasons, i18n.M("reject.need_role", RoleTitle(t.RequiredRole)))
	}
	if actor.Kind == ExecutorAgent {
		if !actor.HasGrant(GrantClaimBacklog) {
			reasons = append(reasons, i18n.M("reject.agent_no_grant", i18n.Key("grant.claim_backlog")))
		}
		if t.HumanOnly {
			reasons = append(reasons, i18n.M("reject.human_only"))
		}
		var missing []string
		for _, cap := range t.RequiredCapabilities {
			if !contains(actor.Capabilities, cap) {
				missing = append(missing, cap)
			}
		}
		if len(missing) > 0 {
			reasons = append(reasons, i18n.M("reject.missing_caps", missing))
		}
		if s != nil && s.Label == LabelActive && c.ActorRuns >= maxConcurrent(actor) {
			reasons = append(reasons, i18n.M("reject.concurrency"))
		}
	}
	return reasons
}

func maxConcurrent(e *Executor) int {
	if e.MaxConcurrent <= 0 {
		return 1
	}
	return e.MaxConcurrent
}

// ---------- 命令 ----------

func (c *Context) openRun(o *Outcome, executorID string) {
	now := c.now()
	r := &Run{ID: c.newID("run"), TaskID: c.Task.ID, State: c.Task.State, ExecutorID: executorID, StartedAt: now, LastBeat: now}
	o.OpenedRun = r
	c.ActiveRun = r
	if c.Task.ActualStart == nil {
		c.Task.ActualStart = &now
	}
	o.emit(c, "RunStarted", executorID, map[string]any{"run_id": r.ID, "state": r.State})
}

func (c *Context) closeRun(o *Outcome, actorID string, outcome RunOutcome) {
	r := c.ActiveRun
	if r == nil {
		return
	}
	now := c.now()
	r.EndedAt = &now
	r.Outcome = outcome
	o.ClosedRun = r
	c.ActiveRun = nil
	o.emit(c, "RunEnded", actorID, map[string]any{"run_id": r.ID, "outcome": string(outcome)})
}

// Apply 触发流程里的一步。失败返回 *Rejection。
func Apply(c *Context, actor *Executor, name string, p Payload) (*Outcome, error) {
	t, wf := c.Task, &c.Type.Workflow
	if cur := wf.State(t.State); cur != nil && cur.Label.IsTerminal() {
		return nil, reject("reject.task_closed")
	}
	var found *Availability
	for _, a := range Available(c, actor, p) {
		if a.Transition.Name == name {
			a := a
			found = &a
		}
	}
	if found == nil {
		return nil, reject("reject.no_transition", name)
	}
	if !found.Available {
		return nil, &Rejection{Reasons: found.Reasons}
	}
	if found.NeedsApproval {
		return nil, needsApproval(grantOf(found.Transition))
	}
	// 进入进行中且会为自己开执行记录的 Agent，同样受最多同时任务数约束（与领取、开始执行一致）
	if toDef := wf.State(found.Transition.To); toDef != nil && toDef.Label == LabelActive && actor.Kind == ExecutorAgent &&
		isPrincipal(actor, t.AssigneeID) && !t.HumanOnly && c.ActiveRun == nil && c.ActorRuns >= maxConcurrent(actor) {
		return nil, reject("reject.concurrency")
	}
	return applyStep(c, actor, found.Transition, p)
}

// applyStep 走完一步的全部副作用：评论与结果、结束 / 开始执行记录、改状态、切负责人、发动态。
// 调用方已经做完可用性判断（Apply 与外部事件触发共用这一段，ADR 0020）。
func applyStep(c *Context, actor *Executor, tr Transition, p Payload) (*Outcome, error) {
	t, wf := c.Task, &c.Type.Workflow
	from := t.State
	to := tr.To
	if to == "$previous" {
		to = t.PreviousState
	}
	fromDef, toDef := wf.State(from), wf.State(to)
	if fromDef == nil {
		return nil, reject("reject.bad_state", from)
	}
	if toDef == nil {
		return nil, reject("reject.bad_state", to)
	}
	o := &Outcome{Task: t}
	now := c.now()

	if strings.TrimSpace(p.Comment) != "" {
		t.Comments = append(t.Comments, Comment{ID: c.newID("cmt"), ByID: actor.ID, Text: p.Comment, CreatedAt: now})
		o.emit(c, "CommentAdded", actor.ID, map[string]any{"text": p.Comment})
	}
	if p.Result != nil {
		if t.Fields == nil {
			t.Fields = map[string]any{}
		}
		t.Fields["result"] = p.Result
	}

	// 离开进行中：结束执行记录
	if fromDef.Label == LabelActive {
		outcome := RunCompleted
		if toDef.Label == LabelTerminalFailure {
			outcome = RunCancelled
		}
		c.closeRun(o, actor.ID, outcome)
		t.PreviousState = from
	}
	if fromDef.Weight != nil {
		t.LastWeight = *fromDef.Weight
	}
	t.State = to
	t.UpdatedAt = now
	o.emit(c, "TaskTransitioned", actor.ID, map[string]any{"transition": tr.Name, "title": tr.Title, "from": from, "to": to, "from_title": fromDef.Title, "to_title": toDef.Title, "from_label": string(fromDef.Label), "to_label": string(toDef.Label)})

	// 终态：实际结束时间；已终止时自动解除本任务作为前置的关联
	if toDef.Label.IsTerminal() {
		t.ActualEnd = &now
	} else {
		t.ActualEnd = nil
	}
	if toDef.Label == LabelTerminalFailure {
		kept := t.Relations[:0]
		for _, r := range t.Relations {
			if r.Type == RelationBlocks {
				o.emit(c, "RelationRemoved", "", map[string]any{"type": string(r.Type), "other_id": r.OtherID})
				continue
			}
			kept = append(kept, r)
		}
		t.Relations = kept
	}

	// 负责人切换
	switch {
	case strings.HasPrefix(tr.AssignTo, "participant:"):
		slot := strings.TrimPrefix(tr.AssignTo, "participant:")
		var slotTitle i18n.Text
		role := ""
		if pt := c.Type.Participant(slot); pt != nil {
			slotTitle, role = pt.Title, pt.Role
		} else {
			slotTitle = i18n.Text{i18n.Default: slot}
		}
		if who := t.Participants[slot]; who != "" {
			t.AssigneeID, t.RequiredRole, t.PendingSlot = who, "", ""
			o.emit(c, "TaskAssigned", actor.ID, map[string]any{"to": who, "via_slot": slotTitle})
		} else {
			t.AssigneeID, t.RequiredRole, t.PendingSlot = "", role, slot
			o.emit(c, "TaskSentToBacklog", actor.ID, map[string]any{"required_role": role, "slot": slot})
		}
	case tr.AssignTo == "creator":
		t.AssigneeID, t.RequiredRole, t.PendingSlot = t.CreatorID, "", ""
		o.emit(c, "TaskAssigned", actor.ID, map[string]any{"to": t.CreatorID})
	case tr.ClearAssignee:
		t.AssigneeID, t.RequiredRole, t.PendingSlot = "", "", ""
		o.emit(c, "TaskSentToBacklog", actor.ID, nil)
	}

	// 进入进行中：只有触发者本人就是（或代表）负责人时才开始执行记录（ADR 0006）。
	// 外部事件不是执行者，永远不为谁开执行记录。
	if toDef.Label == LabelActive && actor.Kind != ExecutorExternal && isPrincipal(actor, t.AssigneeID) && !(actor.Kind == ExecutorAgent && t.HumanOnly) {
		c.openRun(o, actor.ID)
	}
	return o, nil
}

// Claim 从待领取任务领取。
func Claim(c *Context, actor *Executor) (*Outcome, error) {
	if reasons := CanClaim(c, actor); len(reasons) > 0 {
		return nil, &Rejection{Reasons: reasons}
	}
	if NeedsApproval(actor, GrantClaimBacklog) {
		return nil, needsApproval(GrantClaimBacklog)
	}
	t := c.Task
	o := &Outcome{Task: t}
	t.AssigneeID = actor.ID
	if t.PendingSlot != "" {
		if t.Participants == nil {
			t.Participants = map[string]string{}
		}
		t.Participants[t.PendingSlot] = actor.ID
		t.PendingSlot = ""
	}
	t.RequiredRole = ""
	t.UpdatedAt = c.now()
	o.emit(c, "TaskClaimed", actor.ID, nil)
	if s := c.Type.Workflow.State(t.State); s != nil && s.Label == LabelActive {
		c.openRun(o, actor.ID)
	}
	return o, nil
}

// Begin 显式开始执行。
func Begin(c *Context, actor *Executor) (*Outcome, error) {
	if reasons := CanBegin(c, actor); len(reasons) > 0 {
		return nil, &Rejection{Reasons: reasons}
	}
	if NeedsApproval(actor, GrantExecute) {
		return nil, needsApproval(GrantExecute)
	}
	o := &Outcome{Task: c.Task}
	c.openRun(o, actor.ID)
	return o, nil
}

// Assign 由有权限的人直接指派负责人；executorID 为空表示清空负责人。
func Assign(c *Context, actor *Executor, executorID string) (*Outcome, error) {
	if actor.Kind == ExecutorAgent && !actor.HasGrant(GrantAssign) {
		return nil, reject("reject.agent_no_grant", i18n.Key("grant.assign"))
	}
	t := c.Task
	if c.ActiveRun != nil && c.ActiveRun.ExecutorID != executorID {
		return nil, reject("reject.run_busy")
	}
	if NeedsApproval(actor, GrantAssign) {
		return nil, needsApproval(GrantAssign)
	}
	o := &Outcome{Task: t}
	t.AssigneeID, t.RequiredRole = executorID, ""
	if t.PendingSlot != "" && executorID != "" {
		if t.Participants == nil {
			t.Participants = map[string]string{}
		}
		t.Participants[t.PendingSlot] = executorID
		t.PendingSlot = ""
	}
	t.UpdatedAt = c.now()
	if executorID == "" {
		o.emit(c, "TaskSentToBacklog", actor.ID, nil)
	} else {
		o.emit(c, "TaskAssigned", actor.ID, map[string]any{"to": executorID})
	}
	return o, nil
}

// ReportUsage 上报累计用量（按模型幂等取最大）。
func ReportUsage(c *Context, actor *Executor, usage []Usage) (*Outcome, error) {
	r := c.ActiveRun
	if r == nil {
		return nil, reject("reject.no_run")
	}
	if r.ExecutorID != actor.ID && !isPrincipal(actor, r.ExecutorID) {
		return nil, reject("reject.not_run_executor")
	}
	for _, u := range usage {
		merged := false
		for i := range r.Usage {
			if r.Usage[i].ModelID == u.ModelID {
				r.Usage[i] = maxUsage(r.Usage[i], u)
				merged = true
			}
		}
		if !merged {
			r.Usage = append(r.Usage, u)
		}
	}
	r.LastBeat = c.now()
	o := &Outcome{Task: c.Task}
	o.emit(c, "UsageReported", actor.ID, map[string]any{"run_id": r.ID})
	return o, nil
}

func maxUsage(a, b Usage) Usage {
	m := func(x, y int64) int64 {
		if x > y {
			return x
		}
		return y
	}
	return Usage{ModelID: a.ModelID, InputTokens: m(a.InputTokens, b.InputTokens), OutputTokens: m(a.OutputTokens, b.OutputTokens),
		CacheReadTokens: m(a.CacheReadTokens, b.CacheReadTokens), CacheWriteTokens: m(a.CacheWriteTokens, b.CacheWriteTokens), ToolCalls: m(a.ToolCalls, b.ToolCalls)}
}

// TimeOut 由应用层在心跳超时后调用：结束执行记录，任务回到负责人手上（状态不变）。
func TimeOut(c *Context) *Outcome {
	o := &Outcome{Task: c.Task}
	c.closeRun(o, "", RunTimedOut)
	return o
}

// AddArtifact 附上交付物。
func AddArtifact(c *Context, actor *Executor, a Artifact) *Outcome {
	if a.ID == "" {
		a.ID = c.newID("art")
	}
	a.ByID = actor.ID
	a.CreatedAt = c.now()
	c.Task.Artifacts = append(c.Task.Artifacts, a)
	o := &Outcome{Task: c.Task}
	o.emit(c, "ArtifactAttached", actor.ID, map[string]any{"type": a.Type, "title": a.Title})
	return o
}

// AddComment 写评论或工作日志。
func AddComment(c *Context, actor *Executor, text string, isNote bool) (*Outcome, error) {
	if actor.Kind == ExecutorAgent && !actor.HasGrant(GrantComment) {
		return nil, reject("reject.agent_no_grant", i18n.Key("grant.comment"))
	}
	if NeedsApproval(actor, GrantComment) {
		return nil, needsApproval(GrantComment)
	}
	cm := Comment{ID: c.newID("cmt"), ByID: actor.ID, Text: text, IsNote: isNote, CreatedAt: c.now()}
	c.Task.Comments = append(c.Task.Comments, cm)
	o := &Outcome{Task: c.Task}
	typ := "CommentAdded"
	if isNote {
		typ = "NoteAdded"
	}
	o.emit(c, typ, actor.ID, map[string]any{"text": text})
	return o, nil
}

// Link 从本任务出发建立关联。blocksOf 返回某任务作为前置指向的后置任务 ID，用于绕圈检测。
func Link(c *Context, actor *Executor, typ RelationType, otherID string, blocksOf func(id string) []string) (*Outcome, error) {
	if actor.Kind == ExecutorAgent && !actor.HasGrant(GrantLink) {
		return nil, reject("reject.agent_no_grant", i18n.Key("grant.link"))
	}
	if otherID == c.Task.ID {
		return nil, reject("reject.self_link")
	}
	if typ == RelationBlocks && WouldCycle(c.Task.ID, otherID, blocksOf) {
		return nil, reject("reject.cycle")
	}
	for _, r := range c.Task.Relations {
		if r.Type == typ && r.OtherID == otherID {
			return nil, reject("reject.dup_relation")
		}
	}
	if NeedsApproval(actor, GrantLink) {
		return nil, needsApproval(GrantLink)
	}
	c.Task.Relations = append(c.Task.Relations, Relation{Type: typ, OtherID: otherID})
	o := &Outcome{Task: c.Task}
	o.emit(c, "TasksLinked", actor.ID, map[string]any{"type": string(typ), "other_id": otherID})
	return o, nil
}

// WouldCycle 判断 from blocks to 是否成环：若 to 已（传递地）blocks from 则成环。
func WouldCycle(fromID, toID string, blocksOf func(id string) []string) bool {
	seen := map[string]bool{}
	stack := []string{toID}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == fromID {
			return true
		}
		if seen[cur] {
			continue
		}
		seen[cur] = true
		stack = append(stack, blocksOf(cur)...)
	}
	return false
}
