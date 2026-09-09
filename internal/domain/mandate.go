// 委托（ADR 0028）：所有者把一个任务交给一个 Agent 做到底的凭据。
//
// 内核只认一件事：这个 Agent 在这个任务上有没有活动中的委托（Executor.Mandate）。
// 有，则白名单里的动作直接生效，不看那项授权是「直接生效」还是「需要人确认」；
// 白名单之外的动作（改负责人、取消、验收、解除前置、改流程……）仍按 ADR 0003 逐项判。
// 判定只在这里一处，应用层不许自己判「在不在委托内」。
package domain

import (
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// MandateScope 是装进执行者的委托范围。
type MandateScope struct {
	ID          string
	TaskID      string
	SideEffects map[string]bool // 对外动作逐项许可；本期只有 SideEffectExternalLink
}

// 副作用的名字。只有有对应写路径的才启用；没启用的名字一律视为未许可。
const (
	SideEffectExternalLink = "external_link"
	SideEffectPushCode     = "push_code"    // 保留，尚无写路径
	SideEffectSendMessage  = "send_message" // 保留，尚无写路径
)

// 验收方式。
const (
	AcceptanceHuman = "human"
	AcceptanceAuto  = "auto"
)

// MandateAction 是白名单里的动作种类。
type MandateAction string

const (
	ActTransition   MandateAction = "transition"    // 按流程推进（含提问等待）
	ActBegin        MandateAction = "begin"         // 开始执行
	ActArtifact     MandateAction = "artifact"      // 附交付物
	ActNote         MandateAction = "note"          // 写进展
	ActResult       MandateAction = "result"        // 提交结果
	ActComment      MandateAction = "comment"       // 评论
	ActSubtask      MandateAction = "subtask"       // 在本任务下建子任务
	ActLink         MandateAction = "link"          // 给本任务建关联（不含解除前置）
	ActExternalLink MandateAction = "external_link" // 挂 / 摘外部链接
)

// Action 是一次待判定的动作。
type Action struct {
	Kind       MandateAction
	Grant      Grant       // 这个动作要的授权
	Transition *Transition // Kind == ActTransition 时
	ToLabel    Label       // Kind == ActTransition 时去向状态的类型
}

// InMandate 判断执行者在这个任务上是否持有活动中的委托。
func (e *Executor) InMandate(taskID string) bool {
	return e != nil && e.Kind == ExecutorAgent && e.Mandate != nil && e.Mandate.TaskID == taskID && taskID != ""
}

// MandateCovers 判断委托是否覆盖这个动作（白名单）。
func MandateCovers(actor *Executor, taskID string, a Action) bool {
	if !actor.InMandate(taskID) {
		return false
	}
	switch a.Kind {
	case ActArtifact, ActNote, ActResult, ActBegin, ActComment, ActSubtask, ActLink:
		return true
	case ActExternalLink:
		return actor.Mandate.SideEffects[SideEffectExternalLink]
	case ActTransition:
		if a.Transition == nil {
			return false
		}
		// 步骤要由负责人走、只要「执行任务」授权、去向不是失败终态；验收、流程管理、取消一律不在名单内。
		if grantOf(*a.Transition) != GrantExecute {
			return false
		}
		if a.ToLabel == LabelTerminalFailure {
			return false
		}
		for _, b := range a.Transition.By {
			if b == "assignee" {
				return true
			}
		}
		return false
	}
	return false
}

// NeedsApprovalFor 是唯一的「要不要问人」判定：那项授权是「需要人确认」、且委托不覆盖这个动作。
func NeedsApprovalFor(actor *Executor, taskID string, a Action) bool {
	return NeedsApproval(actor, a.Grant) && !MandateCovers(actor, taskID, a)
}

// toLabel 解析一步的去向状态类型（$previous 按上一状态算）。
func (c *Context) toLabel(tr Transition) Label {
	to := tr.To
	if to == "$previous" {
		to = c.Task.PreviousState
	}
	if st := c.Type.Workflow.State(to); st != nil {
		return st.Label
	}
	return ""
}

// EnsureRun 委托内的第一个写动作自动开执行记录（ADR 0028 第 2 条）：任务在进行中、触发者就是负责人、
// 还没有执行记录、没超并发上限。不满足就什么都不做（写动作照常生效）。
func EnsureRun(c *Context, actor *Executor, o *Outcome) {
	if o == nil || !actor.InMandate(c.Task.ID) || c.ActiveRun != nil || o.OpenedRun != nil {
		return
	}
	st := c.Type.Workflow.State(c.Task.State)
	if st == nil || st.Label != LabelActive || !isPrincipal(actor, c.Task.AssigneeID) || c.Task.HumanOnly {
		return
	}
	if c.ActorRuns >= maxConcurrent(actor) {
		return
	}
	c.openRun(o, actor.ID)
}

// systemActor 是系统执行者：自动验收时的触发者。
func systemActor() *Executor {
	return &Executor{ID: SystemActorID, Kind: ExecutorSystem, Name: "系统"}
}

// AutoAcceptCheck 判断此刻能否自动验收：任务标了自动验收、处于有验收步骤的状态、步骤自己声明的要求都满足。
// 返回验收步骤名与不满足的原因；步骤名为空表示当前状态没有验收通过这一步。
func AutoAcceptCheck(c *Context) (string, []Reason) {
	t := c.Task
	if t.AcceptanceMode != AcceptanceAuto {
		return "", []Reason{i18n.M("reject.not_auto_accept")}
	}
	name := ReviewStep(c, true)
	if name == "" {
		return "", nil
	}
	var tr *Transition
	for i := range c.Type.Workflow.Transitions {
		if c.Type.Workflow.Transitions[i].Name == name {
			tr = &c.Type.Workflow.Transitions[i]
		}
	}
	if tr == nil {
		return "", nil
	}
	var reasons []Reason
	for _, cond := range tr.Requires {
		if r := c.checkCondition(cond, Payload{}); r != nil {
			reasons = append(reasons, *r)
		}
	}
	return name, reasons
}

// AutoAccept 由系统执行者走验收通过那一步（ADR 0028 第 5 条）。
// 它不参与 by 判定、不看授权、不开执行记录，只能走 grant: review 且去向是成功终态的那一步；
// 条件不满足时返回拒绝，任务停在原状态等 Agent 补齐。
func AutoAccept(c *Context) (*Outcome, error) {
	name, reasons := AutoAcceptCheck(c)
	if len(reasons) > 0 {
		return nil, &Rejection{Reasons: reasons}
	}
	if name == "" {
		return nil, reject("reject.no_transition", "accept")
	}
	var tr Transition
	for _, x := range c.Type.Workflow.Transitions {
		if x.Name == name {
			tr = x
		}
	}
	o, err := applyStep(c, systemActor(), tr, Payload{})
	if err != nil {
		return nil, err
	}
	head := Event{Type: "TaskAutoAccepted", TaskID: c.Task.ID, ActorID: SystemActorID, At: c.now(),
		Data: map[string]any{"transition": tr.Name, "title": tr.Title, "to": c.Task.State}}
	o.Events = append([]Event{head}, o.Events...)
	return o, nil
}

// RevertWindow 是所有者能撤回委托内一步推进的时限。
const RevertWindow = 24 * time.Hour

// Revert 把任务撤回到某个状态（ADR 0028 第 3 条）：只追加动态，不删任何事实。
// 谁能撤、撤哪一步、版本是否仍是那一步之后的版本，由应用层按动态与版本校验；内核只负责状态与执行记录。
func Revert(c *Context, actor *Executor, toState, reason string) (*Outcome, error) {
	t, wf := c.Task, &c.Type.Workflow
	toDef := wf.State(toState)
	if toDef == nil {
		return nil, reject("reject.no_state", toState)
	}
	fromDef := wf.State(t.State)
	o := &Outcome{Task: t}
	if fromDef != nil && fromDef.Label == LabelActive {
		c.closeRun(o, actor.ID, RunCancelled)
	}
	from := t.State
	t.State = toState
	t.PreviousState = ""
	if toDef.Weight != nil {
		t.LastWeight = *toDef.Weight
	}
	if !toDef.Label.IsTerminal() {
		t.ActualEnd = nil
	}
	t.UpdatedAt = c.now()
	data := map[string]any{"from": from, "to": toState, "reason": reason}
	if fromDef != nil {
		data["from_title"] = fromDef.Title
	}
	data["to_title"] = toDef.Title
	o.emit(c, "TaskReverted", actor.ID, data)
	return o, nil
}

// 委托的状态。
const (
	MandateActive  = "active"
	MandateRevoked = "revoked" // 所有者收回
	MandateStale   = "stale"   // 失效：负责人、目标、类型版本、授权、Agent 变了
	MandateDone    = "done"    // 任务到终态，委托自然结束（不记动态、不通知）
)

// AskMe 是委托里仍要问人的事的固定清单（ADR 0028 第 1.3 条）。组织给默认值，所有者只能加勾不能去勾。
const (
	AskReassign          = "reassign"            // 改负责人
	AskCancel            = "cancel"              // 取消
	AskCreateOutsidePlan = "create_outside_plan" // 方案外新建
	AskAccept            = "accept"              // 任务级人工验收
)

// AskMeAll 是 ask_me 的全部合法值。
var AskMeAll = []string{AskReassign, AskCancel, AskCreateOutsidePlan, AskAccept}

// Mandate 是一份委托（持久化对象；内核判定只用 MandateScope）。
type Mandate struct {
	ID           string              `json:"id"`
	OrgID        string              `json:"org_id"`
	TaskID       string              `json:"task_id"`
	AgentID      string              `json:"agent_id"`
	OwnerID      string              `json:"owner_id"` // 发出委托的人
	PlanID       string              `json:"plan_id,omitempty"`
	GoalID       string              `json:"goal_id,omitempty"`      // 发出时任务的目标；换目标即失效
	TypeVersion  int                 `json:"type_version,omitempty"` // 发出时任务类型的版本；换版本即失效
	BudgetCost   *float64            `json:"budget_cost,omitempty"`
	BudgetTokens *int64              `json:"budget_tokens,omitempty"`
	Deadline     *time.Time          `json:"deadline,omitempty"`
	SideEffects  map[string]bool     `json:"side_effects"`
	AskMe        []string            `json:"ask_me"`
	Grants       map[Grant]GrantMode `json:"grants"` // 发出时的授权快照
	TaskVersion  int                 `json:"task_version"`
	AssigneeID   string              `json:"assignee_id"`
	Status       string              `json:"status"`
	Reason       string              `json:"reason,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	EndedAt      *time.Time          `json:"ended_at,omitempty"`
}

// Active 判断委托是否仍在生效。
func (m *Mandate) Active() bool { return m != nil && m.Status == MandateActive }

// Scope 把委托装成内核用的范围。
func (m *Mandate) Scope() *MandateScope {
	if m == nil {
		return nil
	}
	se := map[string]bool{}
	for k, v := range m.SideEffects {
		se[k] = v
	}
	return &MandateScope{ID: m.ID, TaskID: m.TaskID, SideEffects: se}
}

// Asks 判断这份委托是否要求某件事问人。
func (m *Mandate) Asks(what string) bool {
	if m == nil {
		return false
	}
	for _, a := range m.AskMe {
		if a == what {
			return true
		}
	}
	return false
}
