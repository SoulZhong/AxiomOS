// Package mcp 是给 Agent 的 MCP 工具面。每个请求按 Bearer 令牌绑定到一个 Agent 会话，
// 工具只调用 app 层，不直接碰数据库（ADR 0008）。工具描述按 Agent 所有者的语言输出。
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// Authenticator 由 api 包提供：从请求解析身份。
type Authenticator func(r *http.Request) (*app.Session, error)

// Handler 返回可挂到 /mcp 的 HTTP 处理器。
func Handler(a *app.App, auth Authenticator) http.Handler {
	return sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		sess, err := auth(r)
		if err != nil {
			loc := i18n.ParseAcceptLanguage(r.Header.Get("Accept-Language"))
			if loc == "" {
				loc = i18n.Default
			}
			return unauthenticatedServer(loc, err)
		}
		return newServer(a, sess)
	}, &sdk.StreamableHTTPOptions{Stateless: true})
}

func unauthenticatedServer(loc i18n.Locale, err error) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "axiomos", Version: "0.2.0"}, nil)
	sdk.AddTool(s, &sdk.Tool{Name: "whoami", Description: i18n.T("查看当前身份。", "Show the current identity.").In(loc)}, func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		return textResult(i18n.Trf(loc, "mcp.unauth", RenderError(loc, err))), nil, nil
	})
	return s
}

// RenderError 把任何错误渲染成请求者语言的一句话，与 HTTP 接口的 error.message 完全一致：
// 用户错误按词条渲染，「没有找到」也按词条，其余（内部错误）用通用句子而不是泄漏内部细节。
func RenderError(loc i18n.Locale, err error) string {
	var ue *app.UserError
	if errors.As(err, &ue) {
		return ue.Render(loc)
	}
	if errors.Is(err, store.ErrNotFound) {
		return i18n.Tr(loc, "err.not_found")
	}
	return i18n.Tr(loc, "err.internal")
}

func textResult(s string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: s}}}
}

func jsonResult(v any) (*sdk.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, v, nil
}

func fail(loc i18n.Locale, err error) (*sdk.CallToolResult, any, error) {
	// 只看不做（ADR 0025）：这不是错误，而是"我将要做什么"的一句话，什么都没写
	if d, ok := app.AsDryRun(err); ok {
		out := map[string]any{"dry_run": true, "will": d.Will}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: d.Will}, &sdk.TextContent{Text: string(b)}}}, out, nil
	}
	// 命中幂等键（ADR 0025）：这次没有再写一次，带回来的是第一次的结果
	if rp, ok := app.AsRepeated(err); ok {
		out := map[string]any{"repeated": true, "message": rp.Message, "result": json.RawMessage(rp.Result),
			"first_result_ref": rp.Ref, "first_called_at": rp.CreatedAt}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: rp.Message}, &sdk.TextContent{Text: string(b)}}}, out, nil
	}
	// 命中「需要人确认」的授权：这不是错误，而是已经记下一条待确认操作（ADR 0003）
	if pp, ok := app.AsProposalPending(err); ok {
		text := pp.Message + " " + i18n.Trf(loc, "proposal.hint", pp.Proposal.ID)
		out := map[string]any{
			"pending_approval": true,
			"message":          text,
			"proposal": map[string]any{
				"id": pp.Proposal.ID, "action": pp.Proposal.Action, "action_title": pp.Proposal.ActionTitle,
				"summary": pp.Proposal.SummaryText, "status": string(pp.Proposal.Status),
				"owner": pp.Proposal.OwnerName, "expires_at": pp.Proposal.ExpiresAt,
			},
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}, &sdk.TextContent{Text: string(b)}}}, out, nil
	}
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: RenderError(loc, err)}}}, nil, nil
}

// ---------- 工具输入 ----------

type taskIDIn struct {
	TaskID string `json:"task_id" jsonschema:"Task reference: #123, 123, the task ID, or part of the title"`
}

// 写类工具都带两个可选参数（ADR 0025）：dry_run（只看不做）与 idempotency_key（幂等键）。
// 它们不改工具的语义，只改"这次算不算数"，所以每个写类工具的输入结构里各写一遍，
// 让客户端在工具清单里直接看得到。

type taskWriteIn struct {
	TaskID         string `json:"task_id" jsonschema:"Task reference: #123, 123, the task ID, or part of the title"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type transitionIn struct {
	TaskID         string         `json:"task_id" jsonschema:"Task reference: #123, 123, the task ID, or part of the title"`
	Name           string         `json:"name" jsonschema:"Step code name, e.g. submit or dev_done; use get_workflow to list available steps"`
	Comment        string         `json:"comment,omitempty" jsonschema:"Comment attached to this step; required for ask_for_input, reject and similar steps"`
	Result         map[string]any `json:"result,omitempty" jsonschema:"Structured result fields when submitting, per result_schema in the task brief"`
	DryRun         bool           `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string         `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type heartbeatIn struct {
	TaskID string         `json:"task_id,omitempty" jsonschema:"ID of the task being executed; omit when idle"`
	Usage  []domain.Usage `json:"usage,omitempty" jsonschema:"Cumulative usage per model (totals so far, not deltas)"`
}

type commentIn struct {
	TaskID         string `json:"task_id" jsonschema:"Task reference: #123, 123, the task ID, or part of the title"`
	Text           string `json:"text" jsonschema:"Content"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type artifactIn struct {
	TaskID         string `json:"task_id" jsonschema:"Task reference: #123, 123, the task ID, or part of the title"`
	Type           string `json:"type" jsonschema:"Deliverable type code: result, prd, pr, test_report, release_note, document, file, link"`
	Title          string `json:"title" jsonschema:"Deliverable title"`
	Ref            string `json:"ref" jsonschema:"Link or reference"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type createTaskIn struct {
	GoalID               string            `json:"goal_id,omitempty" jsonschema:"Goal ID; optional when creating a subtask"`
	ParentID             string            `json:"parent_id,omitempty" jsonschema:"Parent task ID (for subtasks)"`
	TypeName             string            `json:"type_name,omitempty" jsonschema:"Task type code name, default generic"`
	Title                string            `json:"title" jsonschema:"Title"`
	Description          string            `json:"description,omitempty" jsonschema:"Description"`
	AssigneeID           string            `json:"assignee_id,omitempty" jsonschema:"Assignee (member or agent ID)"`
	Participants         map[string]string `json:"participants,omitempty" jsonschema:"Participant slot -> executor ID"`
	RequiredCapabilities []string          `json:"required_capabilities,omitempty" jsonschema:"Capabilities required to claim"`
	EstimateHours        *float64          `json:"estimate_hours,omitempty" jsonschema:"Estimate in hours"`
	PlannedStart         string            `json:"planned_start,omitempty" jsonschema:"Planned start date YYYY-MM-DD"`
	PlannedEnd           string            `json:"planned_end,omitempty" jsonschema:"Planned end date YYYY-MM-DD"`
	Priority             *int              `json:"priority,omitempty" jsonschema:"Priority 0 (highest) to 3"`
	Ready                bool              `json:"ready,omitempty" jsonschema:"Mark ready right after creation (skip draft)"`
	DryRun               bool              `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey       string            `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type createGoalIn struct {
	ParentID       string `json:"parent_id,omitempty" jsonschema:"Parent goal reference: the goal ID or part of its title"`
	Title          string `json:"title" jsonschema:"Title"`
	Description    string `json:"description,omitempty" jsonschema:"Description"`
	Deadline       string `json:"deadline,omitempty" jsonschema:"Deadline YYYY-MM-DD"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type goalIDIn struct {
	GoalID string `json:"goal_id" jsonschema:"Goal reference: the goal ID or part of its title"`
}

type createMilestoneIn struct {
	GoalID         string `json:"goal_id" jsonschema:"Goal reference: the goal ID or part of its title"`
	Title          string `json:"title" jsonschema:"What should be achieved by that date"`
	DueOn          string `json:"due_on" jsonschema:"Date YYYY-MM-DD"`
	Description    string `json:"description,omitempty" jsonschema:"Optional description"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type milestoneIDIn struct {
	MilestoneID    string `json:"milestone_id" jsonschema:"Milestone ID"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type linkIn struct {
	TaskID         string `json:"task_id" jsonschema:"This task: #123, 123, the task ID, or part of the title"`
	Type           string `json:"type" jsonschema:"Relation type: blocks (this task is a predecessor of the other), found_in (this task is a bug found in the other), relates_to"`
	OtherID        string `json:"other_id" jsonschema:"The other task: #123, 123, the task ID, or part of the title"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type assignIn struct {
	TaskID         string `json:"task_id" jsonschema:"Task reference: #123, 123, the task ID, or part of the title"`
	ExecutorID     string `json:"executor_id" jsonschema:"Person reference: @name, the member or agent ID, or an email"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type listSprintsIn struct {
	TeamID string `json:"team_id,omitempty" jsonschema:"Team ID; omit for all"`
	Status string `json:"status,omitempty" jsonschema:"planning | active | closed; omit for all"`
}

type sprintIDIn struct {
	SprintID string `json:"sprint_id" jsonschema:"Sprint reference: the sprint ID or part of its name"`
}

type sprintWriteIn struct {
	SprintID       string `json:"sprint_id" jsonschema:"Sprint reference: the sprint ID or part of its name"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type addTasksToSprintIn struct {
	SprintID       string   `json:"sprint_id" jsonschema:"Sprint reference: the sprint ID or part of its name"`
	TaskIDs        []string `json:"task_ids" jsonschema:"Tasks to add: #123, 123, task IDs, or parts of their titles"`
	DryRun         bool     `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string   `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type removeTaskFromSprintIn struct {
	SprintID       string `json:"sprint_id" jsonschema:"Sprint reference: the sprint ID or part of its name"`
	TaskID         string `json:"task_id" jsonschema:"Task to remove: #123, 123, the task ID, or part of the title"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type boardIn struct {
	TypeName   string `json:"type_name,omitempty" jsonschema:"Task type code name; columns follow its workflow states. Omit for the five state-type columns"`
	GoalID     string `json:"goal_id,omitempty" jsonschema:"Only tasks under this goal (goal ID or part of its title)"`
	SprintID   string `json:"sprint_id,omitempty" jsonschema:"Only tasks in this sprint (sprint ID or part of its name)"`
	AssigneeID string `json:"assignee_id,omitempty" jsonschema:"Only tasks assigned to this person (@name, ID or email); 'me' for myself and my owner"`
	TeamID     string `json:"team_id,omitempty" jsonschema:"Only tasks whose assignee belongs to this team"`
}

type listProposalsIn struct {
	Status string `json:"status,omitempty" jsonschema:"pending | approved | rejected | expired; omit for all"`
}

type closeSprintIn struct {
	SprintID       string `json:"sprint_id" jsonschema:"Sprint reference: the sprint ID or part of its name"`
	Unfinished     string `json:"unfinished,omitempty" jsonschema:"Where unfinished tasks go: backlog (default) or next"`
	NextSprintID   string `json:"next_sprint_id,omitempty" jsonschema:"Sprint to carry unfinished tasks into when unfinished=next"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

// ---------- 工具描述（中英） ----------

var toolDescs = map[string]i18n.Text{
	"whoami":          i18n.T("查看当前身份：我是谁、所有者是谁、有哪些授权与能力标签。", "Show who I am: identity, owner, grants and capabilities."),
	"list_my_tasks":   i18n.T("列出分配给我（或我的所有者）的、还没结束的任务。", "List open tasks assigned to me (or my owner)."),
	"list_backlog":    i18n.T("列出待领取任务（没有负责人、符合我的角色与能力标签的任务）。", "List unclaimed tasks (no assignee, matching my role and capabilities)."),
	"get_task_brief":  i18n.T("获取任务说明：描述、所属目标链、评论、前置任务的结果与交付物、任务类型给 Agent 的执行指令、当前可用步骤。开始任何任务前先调用它；task_id 也可以写序号（#123）。", "Get the task brief: description, goal chain, comments, predecessor results and deliverables, agent instructions for the task type, and available steps. Call this before starting any task; task_id also accepts the task number (#123)."),
	"get_workflow":    i18n.T("查看某任务现在处于什么状态、我能走哪些步骤、不能走的原因，以及能否领取 / 开始执行。被拒绝前先看它：reasons 里的句子和网页上显示的一样。", "See the task's current state, which steps I can take, why others are unavailable, and whether I can claim or begin. Check it before acting: the sentences in reasons are the same ones the web app shows."),
	"claim_task":      i18n.T("从待领取任务里领取一个任务。先用 list_backlog 看 can_claim 与 reasons（需要的角色、能力标签、并发上限），能领再领。领取后我就是负责人；若任务处于进行中阶段会立即开始一段执行记录。", "Claim an unclaimed task. First check can_claim and reasons in list_backlog (required role, capabilities, concurrency limit), then claim. I become the assignee; if the task is in an in-progress stage an execution record starts immediately."),
	"begin_task":      i18n.T("开始执行：在任务的进行中阶段开启一段执行记录。先用 get_task_brief 拿到任务说明、get_workflow 确认 can_begin 为真，再 begin；上报用量前必须先调用它。前置任务未完成、任务已结束、并发已满时会被拒绝并给出原因。", "Begin executing: open an execution record in the task's in-progress stage. First read the brief with get_task_brief and confirm can_begin in get_workflow, then begin; required before reporting usage. Rejected with a reason when predecessors are unfinished, the task is closed, or the concurrency limit is reached."),
	"transition_task": i18n.T("推进流程：触发一个步骤（如 start、submit、dev_done、ask_for_input）。先用 get_workflow 看步骤名、前提（交付物、评论、前置任务）与 needs_approval，再推进；已结束的任务不能再推进。", "Advance the workflow: trigger a step (start, submit, dev_done, ask_for_input, ...). First use get_workflow for step names, preconditions (deliverables, comment, predecessors) and needs_approval, then advance; a finished task cannot be advanced."),
	"heartbeat":       i18n.T("心跳并上报累计用量。先 begin_task 开启执行记录，执行中每 60 秒调用一次；usage 里按模型填累计 token 数（幂等，取最大值）。", "Heartbeat and report cumulative usage. Call begin_task first to open an execution record, then call this every 60 seconds while executing; usage holds cumulative tokens per model (idempotent, max wins)."),
	"add_comment":     i18n.T("在任务上发一条评论（会通知人，需要「评论」授权）。要提问并等待答复请用 transition_task 的 ask_for_input 步骤。", "Post a comment on the task (notifies people; requires the Comment grant). To ask and wait for a reply use the ask_for_input step via transition_task."),
	"add_note":        i18n.T("在任务上写一条工作日志（不打扰人，默认折叠，需要「评论」授权）。用于记录过程与中间结论。", "Write a work note on the task (collapsed by default, no notifications; requires the Comment grant). Use it for process and intermediate findings."),
	"attach_artifact": i18n.T("给任务附上交付物（PR、文档、报表、结果摘要等）。先用 get_task_brief 或 get_workflow 看这一步要求哪种交付物类型，再附上。", "Attach a deliverable (PR, document, report, result summary...). First check in get_task_brief or get_workflow which deliverable type the next step requires, then attach it."),
	"create_task":     i18n.T("创建任务（需要「创建任务」授权）；填 parent_id 则创建子任务（需要「创建子任务」授权）。", "Create a task (requires the Create tasks grant); set parent_id to create a subtask (requires Create subtasks)."),
	"create_goal":     i18n.T("创建目标（需要「创建目标」授权）。", "Create a goal (requires the Create goals grant)."),
	"link_tasks":      i18n.T("建立两个任务的关联：前置、发现于、相关。发现 Bug 时先 create_task（type_name=bug）再用 found_in 关联到被测任务。", "Link two tasks: blocks, found_in, relates_to. For a bug, create_task with type_name=bug then link it with found_in to the task under test."),
	"assign_task":     i18n.T("把任务指派给某个成员或 Agent（需要「指派」授权）。", "Assign the task to a member or agent (requires the Assign grant)."),
	"list_goals":      i18n.T("浏览目标树（含进度与成本）。", "Browse the goal tree (with progress and cost)."),
	"list_task_types": i18n.T("列出任务类型及其流程定义（状态、步骤、参与角色、交付物要求）。", "List task types and their workflow definitions (states, steps, participant roles, deliverable requirements)."),
	"list_members":    i18n.T("列出组织成员与 Agent 的 ID 和名字（指派、填参与角色时用）。", "List member and agent IDs and names (for assigning and filling participant roles)."),
	"get_task":        i18n.T("读取任务详情（含执行记录、关联、子任务）。task_id 也可以写序号（#123）。", "Read task details (execution records, relations, subtasks). task_id also accepts the task number (#123)."),
	"list_tasks":      i18n.T("按条件列出任务。", "List tasks by filter."),

	"list_sprints":            i18n.T("列出迭代（规划中 / 进行中 / 已结束），含任务数与工作量汇总。", "List sprints (planning / active / closed) with task counts and points totals."),
	"get_sprint":              i18n.T("查看一个迭代：迭代目标、起止日期、迭代待办、燃尽图、迭代速度。", "Get one sprint: goal, dates, sprint backlog, burndown and velocity."),
	"add_tasks_to_sprint":     i18n.T("把任务加入迭代待办（已结束的迭代会被拒绝；任务原本在别的迭代里会先移出）。", "Add tasks to a sprint backlog (rejected for closed sprints; a task already in another sprint is moved)."),
	"remove_task_from_sprint": i18n.T("把任务移出迭代待办。", "Remove a task from a sprint backlog."),
	"get_board":               i18n.T("查看看板：按状态分列的任务卡片；每张卡片的 can_move_to 与 moves 说明你现在能把它拖到哪个状态、要走哪一步（用 transition_task 执行）。列上的 over_limit 表示超出在制品上限（只提醒不拦截）。", "View the board: task cards grouped by state. Each card's can_move_to and moves tell you which states you can move it to and which step to call via transition_task. over_limit on a column means the WIP limit is exceeded (a reminder, not a block)."),
	"start_sprint":            i18n.T("开始一个规划中的迭代。需要「管理流程」权限；对 Agent 这一步必须经人确认，调用后会生成一条待确认操作，等人确认才真正开始。", "Start a sprint in planning. Requires the Manage workflows permission; for agents this always needs human confirmation, so the call records a pending action that a person must confirm."),
	"close_sprint":            i18n.T("结束迭代：未完成的任务退回待领取任务（backlog）或转入下一个迭代（next）。需要「管理流程」权限；对 Agent 这一步必须经人确认，会生成一条待确认操作。", "Close a sprint: unfinished tasks return to the unclaimed tasks (backlog) or carry over to the next sprint (next). Requires Manage workflows; for agents this always needs human confirmation and records a pending action."),

	"list_milestones":  i18n.T("列出一个目标自己的里程碑（不含子目标）：名称、日期、状态（未到 / 已达到 / 已逾期）与「可以确认了」提示。里程碑是目标在时间轴上的刻度，不是任务，没有人去做它。", "List a goal's own milestones (not sub-goals): title, date, status (upcoming / reached / overdue) and the ready-to-confirm hint. A milestone marks what the goal should have achieved by a date; it is not a task and nobody executes it."),
	"create_milestone": i18n.T("在目标上新增里程碑（需要「创建目标」授权；授权是需要人确认时会生成一条待确认操作）。", "Add a milestone to a goal (requires the Create goals grant; when that grant needs confirmation, a pending action is recorded instead)."),
	"reach_milestone":  i18n.T("确认里程碑已达到（需要「创建目标」授权；授权是需要人确认时会生成一条待确认操作）。只在里程碑对应的事真的做到了之后调用；list_milestones 里 ready_hint 为真表示日期前的任务都已完成。", "Confirm a milestone as reached (requires the Create goals grant; when that grant needs confirmation, a pending action is recorded instead). Call it only once what the milestone stands for is actually done; ready_hint in list_milestones tells you the tasks due before that date are all finished."),

	"list_my_proposals": i18n.T("列出我提交的待确认操作（等人确认 / 已确认 / 已拒绝 / 已过期），可按状态过滤。被告知「已提交待确认操作」后用它查看进展，不要重复提交同一个操作。", "List the pending actions I submitted (awaiting confirmation / confirmed / rejected / expired), optionally filtered by status. Use it after being told an action was submitted for confirmation; do not resubmit the same action."),
}

// ---------- 服务器 ----------

func newServer(a *app.App, sess *app.Session) *sdk.Server {
	loc := sess.Loc()
	s := sdk.NewServer(&sdk.Implementation{Name: "axiomos", Version: "0.2.0"}, &sdk.ServerOptions{
		Instructions: instructions(sess),
	})
	tool := func(name string) *sdk.Tool { return &sdk.Tool{Name: name, Description: toolDescs[name].In(loc)} }
	f := func(err error) (*sdk.CallToolResult, any, error) { return fail(loc, err) }
	// 指代解析（ADR 0025）：任务接受 #123 / 123 / ID / 标题片段，目标与迭代接受 ID 或名称片段，
	// 人接受 @名字 / ID / 邮箱。指代不明时解析器直接拒绝并列出候选，绝不替人挑一个。
	tid := func(ctx context.Context, ref string) (string, error) {
		return a.ResolveRef(ctx, sess, app.RefTask, ref)
	}
	gid := func(ctx context.Context, ref string) (string, error) {
		return a.ResolveRef(ctx, sess, app.RefGoal, ref)
	}
	mid := func(ctx context.Context, ref string) (string, error) {
		return a.ResolveRef(ctx, sess, app.RefMember, ref)
	}
	sid := func(ctx context.Context, ref string) (string, error) {
		return a.ResolveRef(ctx, sess, app.RefSprint, ref)
	}
	// 写开关：把这次调用的 dry_run 与 idempotency_key 挂到会话副本上，交给应用层
	ws := func(dry bool, key string) *app.Session {
		return sess.WithWrite(app.WriteOptions{DryRun: dry, IdempotencyKey: key})
	}
	// 记下每次工具调用（连接检查用；遥测，不产生动态）
	s.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			res, err := next(ctx, method, req)
			if method == "tools/call" {
				if p, ok := req.GetParams().(*sdk.CallToolParamsRaw); ok && p != nil {
					a.RecordAgentTool(ctx, sess, p.Name)
				}
			}
			return res, err
		}
	})

	addPrompts(s, sess)        // 斜杠命令（ADR 0025 第 1 条）
	addNextActions(s, a, sess) // next_actions：带编号的可执行动作清单（ADR 0025 第 4 条）
	k := &kit{a: a, sess: sess, loc: loc, tool: tool, f: f, tid: tid, gid: gid, mid: mid, sid: sid, ws: ws}
	addBatch1Tools(s, k) // 目标的读与改、任务编辑、外部链接、里程碑、验收、创建子任务
	addBatch2Tools(s, k) // 动态与统计、迭代创建与修改、解除关联、通知、只读组织上下文
	addBatch3Tools(s, k) // 目标方案（ADR 0026）
	addConfirm(s, k)     // 在 Agent 里确认（ADR 0027）：/confirm 提示 + decide_proposal

	sdk.AddTool(s, tool("whoami"), func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		me, err := a.Me(ctx, sess)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"executor_id": sess.Actor.ID, "name": sess.Actor.Name, "kind": sess.Actor.Kind, "owner_member_id": sess.MemberID, "organization": me.Organization.Name, "locale": loc, "grants": sess.Actor.Grants, "capabilities": sess.Actor.Capabilities, "roles": sess.Actor.Roles})
	})
	sdk.AddTool(s, tool("list_my_tasks"), func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		out, err := a.MyTasks(ctx, sess)
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, tool("list_backlog"), func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		out, err := a.Backlog(ctx, sess)
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, tool("get_task_brief"), func(ctx context.Context, req *sdk.CallToolRequest, in taskIDIn) (*sdk.CallToolResult, any, error) {
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		b, err := a.Brief(ctx, sess, id)
		if err != nil {
			return f(err)
		}
		return jsonResult(b)
	})
	sdk.AddTool(s, tool("get_workflow"), func(ctx context.Context, req *sdk.CallToolRequest, in taskIDIn) (*sdk.CallToolResult, any, error) {
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		v, err := a.Workflow(ctx, sess, id)
		if err != nil {
			return f(err)
		}
		return jsonResult(v)
	})
	sdk.AddTool(s, tool("claim_task"), func(ctx context.Context, req *sdk.CallToolRequest, in taskWriteIn) (*sdk.CallToolResult, any, error) {
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		t, err := a.Claim(ctx, ws(in.DryRun, in.IdempotencyKey), id)
		if err != nil {
			return f(err)
		}
		return jsonResult(t)
	})
	sdk.AddTool(s, tool("begin_task"), func(ctx context.Context, req *sdk.CallToolRequest, in taskWriteIn) (*sdk.CallToolResult, any, error) {
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		t, err := a.Begin(ctx, ws(in.DryRun, in.IdempotencyKey), id)
		if err != nil {
			return f(err)
		}
		return jsonResult(t)
	})
	sdk.AddTool(s, tool("transition_task"), func(ctx context.Context, req *sdk.CallToolRequest, in transitionIn) (*sdk.CallToolResult, any, error) {
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		t, err := a.Transition(ctx, ws(in.DryRun, in.IdempotencyKey), id, in.Name, app.TransitionPayload{Comment: in.Comment, Result: in.Result})
		if err != nil {
			return f(err)
		}
		return jsonResult(t)
	})
	sdk.AddTool(s, tool("heartbeat"), func(ctx context.Context, req *sdk.CallToolRequest, in heartbeatIn) (*sdk.CallToolResult, any, error) {
		t, err := a.Heartbeat(ctx, sess, in.TaskID, in.Usage)
		if err != nil {
			return f(err)
		}
		if t == nil {
			return textResult(i18n.Tr(loc, "mcp.heartbeat_ok")), nil, nil
		}
		return jsonResult(map[string]any{"ok": true, "task_id": t.ID})
	})
	sdk.AddTool(s, tool("add_comment"), func(ctx context.Context, req *sdk.CallToolRequest, in commentIn) (*sdk.CallToolResult, any, error) {
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		t, err := a.AddComment(ctx, ws(in.DryRun, in.IdempotencyKey), id, in.Text, false)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "task_id": t.ID})
	})
	sdk.AddTool(s, tool("add_note"), func(ctx context.Context, req *sdk.CallToolRequest, in commentIn) (*sdk.CallToolResult, any, error) {
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		t, err := a.AddComment(ctx, ws(in.DryRun, in.IdempotencyKey), id, in.Text, true)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "task_id": t.ID})
	})
	sdk.AddTool(s, tool("attach_artifact"), func(ctx context.Context, req *sdk.CallToolRequest, in artifactIn) (*sdk.CallToolResult, any, error) {
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		t, err := a.AddArtifact(ctx, ws(in.DryRun, in.IdempotencyKey), id, domain.Artifact{Type: in.Type, Title: in.Title, Ref: in.Ref})
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "task_id": t.ID, "artifacts": t.Artifacts})
	})
	sdk.AddTool(s, tool("create_task"), func(ctx context.Context, req *sdk.CallToolRequest, in createTaskIn) (*sdk.CallToolResult, any, error) {
		ci := app.CreateTaskInput{TypeName: in.TypeName, Title: in.Title, Description: in.Description, Participants: map[string]string{}, RequiredCapabilities: in.RequiredCapabilities, EstimateHours: in.EstimateHours, Priority: in.Priority, Ready: in.Ready}
		var err error
		if ci.GoalID, err = gid(ctx, in.GoalID); err != nil {
			return f(err)
		}
		if ci.ParentID, err = tid(ctx, in.ParentID); err != nil {
			return f(err)
		}
		if ci.AssigneeID, err = mid(ctx, in.AssigneeID); err != nil {
			return f(err)
		}
		for slot, ref := range in.Participants {
			who, err := mid(ctx, ref)
			if err != nil {
				return f(err)
			}
			ci.Participants[slot] = who
		}
		if ci.PlannedStart, err = parseDate(in.PlannedStart); err != nil {
			return f(err)
		}
		if ci.PlannedEnd, err = parseDate(in.PlannedEnd); err != nil {
			return f(err)
		}
		t, err := a.CreateTask(ctx, ws(in.DryRun, in.IdempotencyKey), ci)
		if err != nil {
			return f(err)
		}
		return jsonResult(t)
	})
	sdk.AddTool(s, tool("create_goal"), func(ctx context.Context, req *sdk.CallToolRequest, in createGoalIn) (*sdk.CallToolResult, any, error) {
		gi := app.CreateGoalInput{Title: in.Title, Description: in.Description}
		var err error
		if gi.ParentID, err = gid(ctx, in.ParentID); err != nil {
			return f(err)
		}
		if gi.Deadline, err = parseDate(in.Deadline); err != nil {
			return f(err)
		}
		g, err := a.CreateGoal(ctx, ws(in.DryRun, in.IdempotencyKey), gi)
		if err != nil {
			return f(err)
		}
		return jsonResult(g)
	})
	// 里程碑（ADR 0016）
	sdk.AddTool(s, tool("list_milestones"), func(ctx context.Context, req *sdk.CallToolRequest, in goalIDIn) (*sdk.CallToolResult, any, error) {
		goalID, err := gid(ctx, in.GoalID)
		if err != nil {
			return f(err)
		}
		out, err := a.ListMilestones(ctx, sess, goalID)
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, tool("create_milestone"), func(ctx context.Context, req *sdk.CallToolRequest, in createMilestoneIn) (*sdk.CallToolResult, any, error) {
		mi := app.CreateMilestoneInput{Title: in.Title, Description: in.Description}
		var err error
		if mi.GoalID, err = gid(ctx, in.GoalID); err != nil {
			return f(err)
		}
		if mi.DueOn, err = parseDate(in.DueOn); err != nil {
			return f(err)
		}
		m, err := a.CreateMilestone(ctx, ws(in.DryRun, in.IdempotencyKey), mi)
		if err != nil {
			return f(err)
		}
		return jsonResult(m)
	})
	sdk.AddTool(s, tool("reach_milestone"), func(ctx context.Context, req *sdk.CallToolRequest, in milestoneIDIn) (*sdk.CallToolResult, any, error) {
		m, err := a.ReachMilestone(ctx, ws(in.DryRun, in.IdempotencyKey), in.MilestoneID)
		if err != nil {
			return f(err)
		}
		return jsonResult(m)
	})
	sdk.AddTool(s, tool("link_tasks"), func(ctx context.Context, req *sdk.CallToolRequest, in linkIn) (*sdk.CallToolResult, any, error) {
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		other, err := tid(ctx, in.OtherID)
		if err != nil {
			return f(err)
		}
		t, err := a.Link(ctx, ws(in.DryRun, in.IdempotencyKey), id, domain.RelationType(in.Type), other)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "relations": t.Relations})
	})
	sdk.AddTool(s, tool("assign_task"), func(ctx context.Context, req *sdk.CallToolRequest, in assignIn) (*sdk.CallToolResult, any, error) {
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		who, err := mid(ctx, in.ExecutorID)
		if err != nil {
			return f(err)
		}
		t, err := a.Assign(ctx, ws(in.DryRun, in.IdempotencyKey), id, who)
		if err != nil {
			return f(err)
		}
		return jsonResult(t)
	})
	sdk.AddTool(s, tool("list_goals"), func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		tree, err := a.GoalBriefTree(ctx, sess)
		if err != nil {
			return f(err)
		}
		return jsonResult(tree)
	})
	sdk.AddTool(s, tool("list_task_types"), func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		out, err := a.ListTaskTypes(ctx, sess)
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, tool("list_members"), func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		names, err := a.ExecutorNames(ctx, sess)
		if err != nil {
			return f(err)
		}
		return jsonResult(names)
	})
	sdk.AddTool(s, tool("get_task"), func(ctx context.Context, req *sdk.CallToolRequest, in taskIDIn) (*sdk.CallToolResult, any, error) {
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		d, err := a.GetTaskDetail(ctx, sess, id)
		if err != nil {
			return f(err)
		}
		return jsonResult(d)
	})
	sdk.AddTool(s, tool("list_tasks"), func(ctx context.Context, req *sdk.CallToolRequest, in struct {
		GoalID   string `json:"goal_id,omitempty" jsonschema:"Goal ID"`
		State    string `json:"state,omitempty" jsonschema:"State code name"`
		TypeName string `json:"type_name,omitempty" jsonschema:"Task type code name"`
	}) (*sdk.CallToolResult, any, error) {
		goalID, err := gid(ctx, in.GoalID)
		if err != nil {
			return f(err)
		}
		out, err := a.ListTaskSummaries(ctx, sess, store.TaskFilter{GoalID: goalID, State: in.State, TypeName: in.TypeName})
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})

	// 看板与迭代（ADR 0012）
	sdk.AddTool(s, tool("list_sprints"), func(ctx context.Context, req *sdk.CallToolRequest, in listSprintsIn) (*sdk.CallToolResult, any, error) {
		out, err := a.ListSprints(ctx, sess, store.SprintFilter{TeamID: in.TeamID, Status: domain.SprintStatus(in.Status)})
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, tool("get_sprint"), func(ctx context.Context, req *sdk.CallToolRequest, in sprintIDIn) (*sdk.CallToolResult, any, error) {
		spID, err := sid(ctx, in.SprintID)
		if err != nil {
			return f(err)
		}
		d, err := a.GetSprint(ctx, sess, spID)
		if err != nil {
			return f(err)
		}
		return jsonResult(d)
	})
	sdk.AddTool(s, tool("add_tasks_to_sprint"), func(ctx context.Context, req *sdk.CallToolRequest, in addTasksToSprintIn) (*sdk.CallToolResult, any, error) {
		spID, err := sid(ctx, in.SprintID)
		if err != nil {
			return f(err)
		}
		ids, err := a.ResolveRefs(ctx, sess, app.RefTask, in.TaskIDs)
		if err != nil {
			return f(err)
		}
		n, err := a.AddTasksToSprint(ctx, ws(in.DryRun, in.IdempotencyKey), spID, ids)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "added": n})
	})
	sdk.AddTool(s, tool("remove_task_from_sprint"), func(ctx context.Context, req *sdk.CallToolRequest, in removeTaskFromSprintIn) (*sdk.CallToolResult, any, error) {
		spID, err := sid(ctx, in.SprintID)
		if err != nil {
			return f(err)
		}
		id, err := tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		if err := a.RemoveTaskFromSprint(ctx, ws(in.DryRun, in.IdempotencyKey), spID, id); err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true})
	})
	sdk.AddTool(s, tool("get_board"), func(ctx context.Context, req *sdk.CallToolRequest, in boardIn) (*sdk.CallToolResult, any, error) {
		bf := app.BoardFilter{TypeName: in.TypeName, TeamID: in.TeamID, AssigneeID: in.AssigneeID}
		var err error
		if bf.GoalID, err = gid(ctx, in.GoalID); err != nil {
			return f(err)
		}
		if bf.SprintID, err = sid(ctx, in.SprintID); err != nil {
			return f(err)
		}
		if in.AssigneeID != "" && in.AssigneeID != "me" {
			if bf.AssigneeID, err = mid(ctx, in.AssigneeID); err != nil {
				return f(err)
			}
		}
		b, err := a.Board(ctx, sess, bf)
		if err != nil {
			return f(err)
		}
		return jsonResult(b)
	})
	sdk.AddTool(s, tool("start_sprint"), func(ctx context.Context, req *sdk.CallToolRequest, in sprintWriteIn) (*sdk.CallToolResult, any, error) {
		spID, err := sid(ctx, in.SprintID)
		if err != nil {
			return f(err)
		}
		sp, err := a.StartSprint(ctx, ws(in.DryRun, in.IdempotencyKey), spID)
		if err != nil {
			return f(err)
		}
		return jsonResult(sp)
	})
	sdk.AddTool(s, tool("close_sprint"), func(ctx context.Context, req *sdk.CallToolRequest, in closeSprintIn) (*sdk.CallToolResult, any, error) {
		spID, err := sid(ctx, in.SprintID)
		if err != nil {
			return f(err)
		}
		nextID, err := sid(ctx, in.NextSprintID)
		if err != nil {
			return f(err)
		}
		res, err := a.CloseSprint(ctx, ws(in.DryRun, in.IdempotencyKey), spID, in.Unfinished, nextID)
		if err != nil {
			return f(err)
		}
		return jsonResult(res)
	})

	// 待确认操作（ADR 0003）
	sdk.AddTool(s, tool("list_my_proposals"), func(ctx context.Context, req *sdk.CallToolRequest, in listProposalsIn) (*sdk.CallToolResult, any, error) {
		out, err := a.ListProposals(ctx, sess, app.ProposalFilter{Status: domain.ProposalStatus(in.Status), AgentID: sess.Actor.ID})
		if err != nil {
			return f(err)
		}
		if len(out) == 0 {
			return textResult(i18n.Tr(loc, "mcp.no_proposals")), nil, nil
		}
		return jsonResult(out)
	})
	return s
}

var instructionsText = i18n.T(`你是 AxiomOS 里的执行者「%s」。%s

## 基本规矩
1. 被拒绝时读拒绝理由并据此行动，不要重试同一操作。理由和网页上显示的是同一句话，只有六类：越权（缺授权）、范围不可见、需要人确认、前置未完成、并发已满、任务已结束。
2. 有些授权是「需要人确认」：这类操作调用后不会立刻生效，而是记成一条待确认操作，返回里会告诉你等谁确认、待确认操作 ID。别重试，用 list_my_proposals 看进展；被拒绝就读理由，改了再提。
3. 写操作都接受两个可选参数：dry_run=true 只回一句「会……」而不做任何改动（念给人听、人点头再真做）；idempotency_key 是你自己生成的键，同一个键 24 小时内只生效一次，返回里带 repeated=true 就说明这次没有重复创建。
4. 指代对象用确切写法：任务写 #编号，人写 @名字或邮箱，目标与迭代写编号或名称里的一段；名字对上不止一个时系统会列出候选让你问人，不要自己挑。
5. 人的话说得不清楚、你拿不准他指的是哪个任务或要做哪件事时，先调 next_actions，把带编号的动作清单念给人听让人报编号，不要自己猜着做。
6. 常用操作在客户端里是斜杠命令，人可以直接用：领一个任务（claim_task）、开始做任务（start_task）、提交交付（submit_task）、提问等待（ask_question）、看我的任务（my_tasks）、看某个任务（task_detail）、写进展（add_note）、汇报用量（report_usage）、确认待确认操作（confirm）。斜杠命令名不是工具名，工具以清单为准。人不想切到网页去确认时，请他敲 /confirm：那条提示会给你一张凭证，你再用 decide_proposal 转达他的决定；没有凭证永远不要调 decide_proposal。注意客户端会给 MCP 的斜杠命令加前缀：在 Claude Code 里是 /mcp__axiomos__confirm，参数按顺序空格分开（例如「/mcp__axiomos__confirm all」）；把带前缀的写法念给人。

## 做任务
7. 用 list_my_tasks 看分配给你的任务；用 list_backlog 看可领取的任务（看 can_claim 与 reasons 再 claim_task）。
8. 动手前先 get_task_brief 读任务说明与执行指令，再 get_workflow 看你现在能走哪一步。
9. 进入进行中阶段后先 begin_task 开启执行记录；执行中每 60 秒 heartbeat 并上报累计用量。
10. 交付前用 attach_artifact 附上要求的交付物，再用 transition_task 推进（如 submit、dev_done）。
11. 需求不清就用 transition_task 的 ask_for_input 步骤提问并等待；过程记录用 add_note，要人看的用 add_comment。
12. 改自己负责或自己创建的任务用 update_task（标题、说明、预估、计划起止、工作量、自定义字段；验收人、优先级、归属、迭代、参与角色由人定）。子任务用 create_subtask。外部链接（PR、文档、设计稿）用 list_task_links / add_external_link / remove_external_link。任务间的前置、发现于、相关关系用 link_tasks / unlink_tasks。
13. 做验收先看 get_workflow 的 is_reviewer 与 review_accept_step / review_reject_step，再用 review_task(decision: accept|reject, comment, checked_deliverables)；打回必须写说明，核对过的交付物必须真的挂在任务上。

## 维护目标
14. list_goals 看目标树，get_goal 读单个目标的详情（上级链、直接任务、里程碑、成本对预算、进展说明）。动手改之前先读它。
15. update_goal 改标题、说明、日期、时间粒度、时间桶、信心度、成果指标、类型；改负责人、上级对你一律先经人确认。达成 / 撤销达成 / 放弃 / 重新开始用 achieve_goal / unachieve_goal / abandon_goal / restart_goal，对你一律先经人确认。批量改时间桶用 set_goal_horizon，重排次序用 rank_goals。
16. add_goal_note 在目标上写一句进展说明（推进了什么、卡在哪、下一步做什么），不改字段、不通知人。
17. 里程碑：list_milestones 看，create_milestone / update_milestone 增改，reach_milestone 只在对应的事真的做到了之后确认；delete_milestone 与 unreach_milestone 对你一律先经人确认。

## 领取目标（目标方案）
18. 目标不是任务，没人能"领"它。你领取目标的方式是提交一份目标方案：先 get_goal 读清目标、get_org_context 看清团队与任务类型，再 propose_goal_plan(goal, tasks[], milestones[], rationale)——任务清单里每条带方案内的键，依赖用 depends_on、子任务用 parent_key 指向别的键；理由写清读了什么、假设了什么、没做什么。
19. 整份方案记成一条待确认操作，目标负责人在网页上整体批准、整体拒绝或勾掉几条再批准；批准后一次落库，任务默认指派给你并直接就绪。用 get_goal_plan_status 看进展；被拒绝就读理由改方案再提，不要重提同一份。

## 看动态、统计与组织
20. 要知道团队这段时间在做什么：list_sprints / get_sprint 看迭代待办与燃尽，get_board 看看板，list_events 看动态（可按任务、目标、时间起点筛，句子与网页一样）。
21. 复盘或汇报用数字：get_goal_metrics（目标含子树的进度、任务分布、逾期、成本对预算、周期）、get_task_metrics（任务的周期、打回次数、执行记录、用量）、get_my_metrics（你自己的手上的活、逾期、成本与用量、并发余量）。汇报前或决定要不要再领活时先看 get_my_metrics。
22. 规划与指派前先 get_org_context 看组织背景（货币、可见性、你的范围与授权、团队、能力标签、目标类型、任务类型、角色）；list_teams、list_capabilities、list_members、list_task_types 各看一类。
23. 迭代：create_sprint / update_sprint 建与改（需要「创建任务」授权），add_tasks_to_sprint / remove_task_from_sprint 进出迭代待办；start_sprint / close_sprint 对你必经人确认。
24. list_my_notifications 看与你有关的通知（被打回、有人评论、被指派），处理完用 mark_notifications_read 标已读。

## 不归你做的
25. 成员、团队、角色、授权、能力标签、目标类型、价格表的配置；流程定义与任务类型的修改；Agent 的注册与吊销；通知策略、IM 集成、代码平台的配置；待确认操作的裁决；目标与任务的删除。这些没有工具，也不要试图绕过去。`,
	`You are the executor "%s" in AxiomOS.%s

## Ground rules
1. When rejected, read the reason and act on it; do not retry the same call. Reasons are the very sentences the web app shows and fall into six kinds: missing grant, outside your visible scope, needs human confirmation, predecessors unfinished, concurrency limit reached, task already finished.
2. Some grants require human confirmation: such a call does not take effect immediately but is recorded as a pending action, and the reply tells you who must confirm it and its ID. Do not retry; check progress with list_my_proposals; if rejected, read the reason, revise, and propose again.
3. Every write tool accepts two optional arguments: dry_run=true answers with one sentence describing what would happen and changes nothing (read it out and act only after the person agrees); idempotency_key is a key you generate, and the same key takes effect only once within 24 hours — repeated=true in the reply means nothing was created again.
4. Refer to objects precisely: #number for a task, @name or an email for a person, the ID or part of the name for goals and sprints; when a name matches more than one, the system lists the candidates for you to ask about instead of guessing.
5. When the human's request is ambiguous and you are not sure which task or which action they mean, call next_actions first, read the numbered list back to them and let them pick a number; never guess.
6. The common operations are slash commands in the client, and the human can use them directly: claim_task, start_task, submit_task, ask_question, my_tasks, task_detail, add_note, report_usage, confirm. Slash-command names are not tool names; the tool list is authoritative. When the person would rather not switch to the web to confirm, ask them to run /confirm: that prompt hands you a ticket, and you relay their decision with decide_proposal; never call decide_proposal without a ticket. Clients prefix MCP slash commands: in Claude Code it is /mcp__axiomos__confirm with positional, space-separated arguments (e.g. "/mcp__axiomos__confirm all"); read the prefixed form to the person.

## Doing tasks
7. Use list_my_tasks for tasks assigned to you and list_backlog for claimable tasks (check can_claim and reasons, then claim_task).
8. Before acting, read the brief with get_task_brief, then get_workflow to see which step you can take now.
9. After entering an in-progress stage call begin_task to open an execution record; heartbeat every 60 seconds with cumulative usage.
10. Before delivering, attach the required deliverables with attach_artifact, then advance with transition_task (submit, dev_done, ...).
11. If requirements are unclear, use the ask_for_input step via transition_task and wait; use add_note for process notes and add_comment for things people must see.
12. Edit tasks you are assigned to or created with update_task (title, description, estimate, planned dates, points, custom fields; reviewer, priority, goal, sprint and participants are for people to decide). Subtasks: create_subtask. External links (PR, document, design): list_task_links / add_external_link / remove_external_link. Relations between tasks (blocks, found_in, relates_to): link_tasks / unlink_tasks.
13. To review, first check is_reviewer and review_accept_step / review_reject_step in get_workflow, then review_task(decision: accept|reject, comment, checked_deliverables); rejecting requires a comment, and checked deliverables must really be attached to the task.

## Maintaining goals
14. list_goals shows the goal tree; get_goal reads one goal in full (parent chain, direct tasks, milestones, cost against budget, progress notes). Read it before changing anything.
15. update_goal edits title, description, dates, date precision, horizon, confidence, outcome and type; changing the owner or parent always needs human confirmation for you. Achieve / unachieve / abandon / resume with achieve_goal / unachieve_goal / abandon_goal / restart_goal — always confirmed by a person for you. set_goal_horizon moves a batch between horizons; rank_goals reorders.
16. add_goal_note writes one progress note on a goal (what moved, what is blocked, what comes next); it changes no field and notifies nobody.
17. Milestones: list_milestones to read, create_milestone / update_milestone to add and edit, reach_milestone only once what the milestone stands for is really done; delete_milestone and unreach_milestone always need human confirmation for you.

## Claiming a goal (goal plan)
18. A goal is not a task; nobody can "claim" it. You claim a goal by proposing a plan: read the goal with get_goal and the org with get_org_context, then propose_goal_plan(goal, tasks[], milestones[], rationale) — every task carries a key inside the plan, depends_on and parent_key point at other keys; the rationale says what you read, what you assumed, what you left out.
19. The whole plan becomes one pending action; the goal owner approves it as a whole, rejects it, or unticks some items and approves the rest. On approval everything is created in one go, with tasks assigned to you and ready. Check progress with get_goal_plan_status; if rejected, read the reason and propose a revised plan, never the same one again.

## Activity, numbers and the organization
20. To see what the team is doing: list_sprints / get_sprint for sprint backlog and burndown, get_board for the board, list_events for the activity stream (filter by task, goal or start time; sentences are the same as on the web).
21. Use numbers for retrospectives and reports: get_goal_metrics (a goal's progress, task distribution, overdue, cost against budget, lead time), get_task_metrics (a task's lead time, review rounds, execution records, usage), get_my_metrics (your own open and overdue work, cost and usage, concurrency headroom). Check get_my_metrics before reporting or before claiming more work.
22. Before planning or assigning, read get_org_context (currency, visibility, your scope and grants, teams, capabilities, goal types, task types, roles); list_teams, list_capabilities, list_members and list_task_types each cover one part.
23. Sprints: create_sprint / update_sprint to create and edit (requires the Create tasks grant), add_tasks_to_sprint / remove_task_from_sprint for the sprint backlog; start_sprint / close_sprint always need human confirmation for you.
24. list_my_notifications shows notifications that concern you (sent back, commented, assigned); mark_notifications_read once handled.

## Not yours to do
25. Configuring members, teams, roles, grants, capability tags, goal types or pricing; changing workflow definitions and task types; registering or revoking agents; notification policy, IM integration and code platform settings; deciding pending actions; deleting goals and tasks. There are no tools for these; do not try to work around that.`)

var agentNote = i18n.T("你替你的所有者工作，能做的事不超过所有者本人，并受授权限制。", " You work on behalf of your owner; you can never do more than the owner, and grants limit you further.")

func instructions(sess *app.Session) string {
	note := ""
	if sess.IsAgent() {
		note = agentNote.In(sess.Loc())
	}
	return fmt.Sprintf(instructionsText.In(sess.Loc()), sess.Actor.Name, note)
}

func parseDate(s string) (*timeT, error) {
	if s == "" {
		return nil, nil
	}
	t, err := parseTime(strings.TrimSpace(s))
	if err != nil {
		return nil, app.Bad("err.date_format", s)
	}
	return &t, nil
}
