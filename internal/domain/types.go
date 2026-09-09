// Package domain 是 AxiomOS 的领域层：任务类型与流程的声明、任务、执行记录、动态，
// 以及解释它们的纯函数。这里不依赖数据库；应用层负责装载与持久化。
//
// 术语以仓库根目录 CONTEXT.md 为准；本包中的英文标识符只是代码名。
package domain

import (
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// Label 是状态类型（见 docs/spec/workflow-definition.md §2.1）。
type Label string

const (
	LabelPending         Label = "pending"          // 未开始
	LabelActive          Label = "active"           // 进行中
	LabelWaiting         Label = "waiting"          // 等待中
	LabelTerminalSuccess Label = "terminal_success" // 已完成
	LabelTerminalFailure Label = "terminal_failure" // 已终止
)

// LabelTitle 返回状态类型在某语言下的名称。
func LabelTitle(l Label, loc i18n.Locale) string { return i18n.Tr(loc, "label."+string(l)) }

func (l Label) IsTerminal() bool {
	return l == LabelTerminalSuccess || l == LabelTerminalFailure
}

// State 是流程里的一个状态。
type State struct {
	Name      string    `json:"name"`
	Title     i18n.Text `json:"title"`
	Label     Label     `json:"label"`
	Claimable bool      `json:"claimable,omitempty"` // 仅 pending 有意义
	Weight    *int      `json:"weight,omitempty"`    // 进度权重 0–100
	WIPLimit  int       `json:"wip_limit,omitempty"` // 在制品上限，0 表示不限；只提醒不拦截（ADR 0012）
}

// Transition 是流程里的一步。
type Transition struct {
	Name          string    `json:"name"`
	Title         i18n.Text `json:"title"`
	From          []string  `json:"from"`                     // 状态名，或 ["*"] 表示任意非终止状态
	To            string    `json:"to"`                       // 状态名，或 "$previous"
	By            []string  `json:"by"`                       // creator | assignee | reviewer | participant:<slot> | role:<role> | anyone
	Requires      []string  `json:"requires,omitempty"`       // deps_done | artifact:<type> | comment | no_open_bugs | result
	Grant         Grant     `json:"grant,omitempty"`          // Agent 触发所需授权，默认 execute
	AssignTo      string    `json:"assign_to,omitempty"`      // participant:<slot> | creator | "" (不变)
	ClearAssignee bool      `json:"clear_assignee,omitempty"` // 显式清空负责人
	// TriggeredBy 声明这一步可以由外部事件触发（ADR 0020）：外部事件到达时，若任务当前状态有这样一条步骤
	// 且它的其余前提满足，系统以「外部事件」为执行者走它。不填表示只能由人或 Agent 触发。
	TriggeredBy *Trigger `json:"triggered_by,omitempty"`
}

// Trigger 是一条步骤的外部触发条件（ADR 0020）。Source 目前只有 "git"（代码平台）；
// Event 是六种外部事件之一（见 ExternalEvents）。
type Trigger struct {
	Source string `json:"source"`
	Event  string `json:"event"`
}

// Workflow 是绑定在任务类型上的流程声明。
type Workflow struct {
	Version     int          `json:"version"`
	Initial     string       `json:"initial"`
	States      []State      `json:"states"`
	Transitions []Transition `json:"transitions"`
}

// State 按名字查状态。
func (w *Workflow) State(name string) *State {
	for i := range w.States {
		if w.States[i].Name == name {
			return &w.States[i]
		}
	}
	return nil
}

// Participant 是任务类型规定的参与角色位置。
type Participant struct {
	Slot  string    `json:"slot"`
	Title i18n.Text `json:"title"`
	Role  string    `json:"role"`
}

// TaskType 是任务类型。
type TaskType struct {
	Name              string         `json:"name"`
	Title             i18n.Text      `json:"title"`
	Participants      []Participant  `json:"participants"`
	TaskSchema        map[string]any `json:"task_schema,omitempty"`
	ResultSchema      map[string]any `json:"result_schema,omitempty"`
	AgentInstructions string         `json:"agent_instructions,omitempty"`
	Workflow          Workflow       `json:"workflow"`
	BuiltIn           bool           `json:"built_in"`
}

// Participant 按位置名查参与角色。
func (t *TaskType) Participant(slot string) *Participant {
	for i := range t.Participants {
		if t.Participants[i].Slot == slot {
			return &t.Participants[i]
		}
	}
	return nil
}

// Grant 是所有者给 Agent 的授权（代码名）。
type Grant string

const (
	GrantExecute         Grant = "execute"
	GrantClaimBacklog    Grant = "claim_backlog"
	GrantReview          Grant = "review"
	GrantComment         Grant = "comment"
	GrantCreateSubtask   Grant = "create_subtask"
	GrantCreateTask      Grant = "create_task"
	GrantCreateGoal      Grant = "create_goal"
	GrantAssign          Grant = "assign"
	GrantLink            Grant = "link"
	GrantManageWorkflows Grant = "manage_workflows"
)

// AllGrants 是全部授权，按界面上的顺序。
var AllGrants = []Grant{GrantExecute, GrantClaimBacklog, GrantReview, GrantComment, GrantCreateSubtask, GrantCreateTask, GrantCreateGoal, GrantAssign, GrantLink, GrantManageWorkflows}

// GrantTitle 返回授权在某语言下的名称。
func GrantTitle(g Grant, loc i18n.Locale) string { return i18n.Tr(loc, "grant."+string(g)) }

// GrantMode 是授权的生效方式。
type GrantMode string

const (
	GrantDirect       GrantMode = "direct"        // 直接生效
	GrantWithApproval GrantMode = "with_approval" // 需要人确认
)

// ExecutorKind 区分人与 Agent。
type ExecutorKind string

const (
	ExecutorMember   ExecutorKind = "member"
	ExecutorAgent    ExecutorKind = "agent"
	ExecutorExternal ExecutorKind = "external" // 外部事件（代码平台的 PR / CI），ADR 0020
	ExecutorSystem   ExecutorKind = "system"   // 系统本身：自动验收（ADR 0028）；不参与 by、不看授权、不开执行记录
)

// SystemActorID 是系统执行者在动态里的署名。
const SystemActorID = "system"

// Executor 是能被指派任务的对象：成员或 Agent。
type Executor struct {
	ID            string
	Kind          ExecutorKind
	Name          string
	OwnerID       string   // Agent 的所有者；成员为空
	Roles         []string // 成员的组织角色；Agent 继承所有者，这里为空
	Grants        map[Grant]GrantMode
	Capabilities  []string
	MaxConcurrent int  // Agent 并发上限，0 视为 1
	Shared        bool // 公共 Agent
	// Mandate 是这次动作所在任务上活动中的委托（ADR 0028）；应用层装载，内核只据此放行白名单动作。
	Mandate *MandateScope
}

// Principals 返回身份判定时代表的 ID：成员是自己；Agent 是自己和所有者。
func (e *Executor) Principals() []string {
	if e.Kind == ExecutorAgent && e.OwnerID != "" {
		return []string{e.ID, e.OwnerID}
	}
	return []string{e.ID}
}

// HasGrant 判断 Agent 是否持有授权；成员总是持有。
func (e *Executor) HasGrant(g Grant) bool {
	if e.Kind != ExecutorAgent {
		return true
	}
	_, ok := e.Grants[g]
	return ok
}

// RelationType 是关联类型。
type RelationType string

const (
	RelationBlocks    RelationType = "blocks"     // 前置：本任务是 Other 的前置
	RelationFoundIn   RelationType = "found_in"   // 发现于：本任务（Bug）在 Other 中被发现
	RelationRelatesTo RelationType = "relates_to" // 相关
)

// Relation 是从某任务出发的一条关联。
type Relation struct {
	Type    RelationType `json:"type"`
	OtherID string       `json:"other_id"`
}

// Artifact 是交付物。
type Artifact struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Ref       string    `json:"ref"`
	ByID      string    `json:"by_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Comment 是评论或工作日志。
type Comment struct {
	ID        string    `json:"id"`
	ByID      string    `json:"by_id"`
	Text      string    `json:"text"`
	IsNote    bool      `json:"is_note"` // 工作日志
	CreatedAt time.Time `json:"created_at"`
}

// Task 是任务。
type Task struct {
	ID                   string            `json:"id"`
	Number               int               `json:"number"` // 组织内递增的可读序号，界面上写成 #123
	OrgID                string            `json:"org_id"`
	GoalID               string            `json:"goal_id"`
	ParentID             string            `json:"parent_id,omitempty"`
	TypeName             string            `json:"type_name"`
	TypeVersion          int               `json:"type_version"`
	Title                string            `json:"title"`
	Description          string            `json:"description"`
	CreatorID            string            `json:"creator_id"`
	ReviewerID           string            `json:"reviewer_id"`
	AssigneeID           string            `json:"assignee_id,omitempty"`
	RequiredRole         string            `json:"required_role,omitempty"`
	PendingSlot          string            `json:"pending_slot,omitempty"`
	RequiredCapabilities []string          `json:"required_capabilities,omitempty"`
	HumanOnly            bool              `json:"human_only"`
	State                string            `json:"state"`
	PreviousState        string            `json:"previous_state,omitempty"`
	LastWeight           int               `json:"last_weight"`
	Participants         map[string]string `json:"participants"` // slot -> executor id
	Fields               map[string]any    `json:"fields,omitempty"`
	Priority             int               `json:"priority"` // 0 最高
	EstimateHours        *float64          `json:"estimate_hours,omitempty"`
	Points               *int              `json:"points,omitempty"`    // 工作量（相对大小），与预计工时并存
	SprintID             string            `json:"sprint_id,omitempty"` // 所属迭代
	PlannedStart         *time.Time        `json:"planned_start,omitempty"`
	PlannedEnd           *time.Time        `json:"planned_end,omitempty"`
	ActualStart          *time.Time        `json:"actual_start,omitempty"`
	ActualEnd            *time.Time        `json:"actual_end,omitempty"`
	Artifacts            []Artifact        `json:"artifacts"`
	Comments             []Comment         `json:"comments"`
	Relations            []Relation        `json:"relations"`
	AcceptanceMode       string            `json:"acceptance_mode,omitempty"` // human（默认）| auto，ADR 0028
	PlanID               string            `json:"plan_id,omitempty"`         // 来自哪份目标方案
	Version              int               `json:"version"`                   // 每次写入加一，待确认操作与委托据此判断是否过期
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

// RunOutcome 是执行记录的结束方式。
type RunOutcome string

const (
	RunCompleted RunOutcome = "completed"
	RunCancelled RunOutcome = "cancelled"
	RunTimedOut  RunOutcome = "timed_out"
)

// Usage 是一条按模型分开的用量。
type Usage struct {
	ModelID          string  `json:"model_id"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	ToolCalls        int64   `json:"tool_calls"`
	Cost             float64 `json:"cost"` // 由价格表折算，应用层填
}

func (u Usage) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

// Run 是执行记录。
type Run struct {
	ID         string     `json:"id"`
	TaskID     string     `json:"task_id"`
	State      string     `json:"state"`
	ExecutorID string     `json:"executor_id"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	Outcome    RunOutcome `json:"outcome,omitempty"`
	LastBeat   time.Time  `json:"last_heartbeat"`
	Usage      []Usage    `json:"usage"`
	MandateID  string     `json:"mandate_id,omitempty"` // 在哪份委托之下开的（ADR 0028）
}

func (r *Run) Active() bool { return r.EndedAt == nil }

// Event 是一条动态。
type Event struct {
	Type    string         `json:"type"`
	TaskID  string         `json:"task_id,omitempty"`
	ActorID string         `json:"actor_id,omitempty"`
	At      time.Time      `json:"at"`
	Data    map[string]any `json:"data,omitempty"`
}
