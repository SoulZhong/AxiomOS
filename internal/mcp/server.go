// Package mcp 是给 Agent 的 MCP 工具面。每个请求按 Bearer 令牌绑定到一个 Agent 会话，
// 工具只调用 app 层，不直接碰数据库（ADR 0008）。工具描述按 Agent 所有者的语言输出。
package mcp

import (
	"context"
	"encoding/json"
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
	msg := err.Error()
	if ue, ok := err.(*app.UserError); ok {
		msg = ue.Render(loc)
	}
	sdk.AddTool(s, &sdk.Tool{Name: "whoami", Description: i18n.T("查看当前身份。", "Show the current identity.").In(loc)}, func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		return textResult(i18n.Trf(loc, "mcp.unauth", msg)), nil, nil
	})
	return s
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
	msg := err.Error()
	if ue, ok := err.(*app.UserError); ok {
		msg = ue.Render(loc)
	}
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: msg}}}, nil, nil
}

// ---------- 工具输入 ----------

type taskIDIn struct {
	TaskID string `json:"task_id" jsonschema:"Task ID"`
}

type transitionIn struct {
	TaskID  string         `json:"task_id" jsonschema:"Task ID"`
	Name    string         `json:"name" jsonschema:"Step code name, e.g. submit or dev_done; use get_workflow to list available steps"`
	Comment string         `json:"comment,omitempty" jsonschema:"Comment attached to this step; required for ask_for_input, reject and similar steps"`
	Result  map[string]any `json:"result,omitempty" jsonschema:"Structured result fields when submitting, per result_schema in the task brief"`
}

type heartbeatIn struct {
	TaskID string         `json:"task_id,omitempty" jsonschema:"ID of the task being executed; omit when idle"`
	Usage  []domain.Usage `json:"usage,omitempty" jsonschema:"Cumulative usage per model (totals so far, not deltas)"`
}

type commentIn struct {
	TaskID string `json:"task_id" jsonschema:"Task ID"`
	Text   string `json:"text" jsonschema:"Content"`
}

type artifactIn struct {
	TaskID string `json:"task_id" jsonschema:"Task ID"`
	Type   string `json:"type" jsonschema:"Deliverable type code: result, prd, pr, test_report, release_note, document, file, link"`
	Title  string `json:"title" jsonschema:"Deliverable title"`
	Ref    string `json:"ref" jsonschema:"Link or reference"`
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
}

type createGoalIn struct {
	ParentID    string `json:"parent_id,omitempty" jsonschema:"Parent goal ID"`
	Title       string `json:"title" jsonschema:"Title"`
	Description string `json:"description,omitempty" jsonschema:"Description"`
	Deadline    string `json:"deadline,omitempty" jsonschema:"Deadline YYYY-MM-DD"`
}

type goalIDIn struct {
	GoalID string `json:"goal_id" jsonschema:"Goal ID"`
}

type createMilestoneIn struct {
	GoalID      string `json:"goal_id" jsonschema:"Goal the milestone belongs to"`
	Title       string `json:"title" jsonschema:"What should be achieved by that date"`
	DueOn       string `json:"due_on" jsonschema:"Date YYYY-MM-DD"`
	Description string `json:"description,omitempty" jsonschema:"Optional description"`
}

type milestoneIDIn struct {
	MilestoneID string `json:"milestone_id" jsonschema:"Milestone ID"`
}

type linkIn struct {
	TaskID  string `json:"task_id" jsonschema:"This task ID"`
	Type    string `json:"type" jsonschema:"Relation type: blocks (this task is a predecessor of the other), found_in (this task is a bug found in the other), relates_to"`
	OtherID string `json:"other_id" jsonschema:"The other task ID"`
}

type assignIn struct {
	TaskID     string `json:"task_id" jsonschema:"Task ID"`
	ExecutorID string `json:"executor_id" jsonschema:"Member or agent ID"`
}

type listSprintsIn struct {
	TeamID string `json:"team_id,omitempty" jsonschema:"Team ID; omit for all"`
	Status string `json:"status,omitempty" jsonschema:"planning | active | closed; omit for all"`
}

type sprintIDIn struct {
	SprintID string `json:"sprint_id" jsonschema:"Sprint ID"`
}

type addTasksToSprintIn struct {
	SprintID string   `json:"sprint_id" jsonschema:"Sprint ID"`
	TaskIDs  []string `json:"task_ids" jsonschema:"Task IDs to add"`
}

type removeTaskFromSprintIn struct {
	SprintID string `json:"sprint_id" jsonschema:"Sprint ID"`
	TaskID   string `json:"task_id" jsonschema:"Task ID to remove"`
}

type boardIn struct {
	TypeName   string `json:"type_name,omitempty" jsonschema:"Task type code name; columns follow its workflow states. Omit for the five state-type columns"`
	GoalID     string `json:"goal_id,omitempty" jsonschema:"Only tasks under this goal"`
	SprintID   string `json:"sprint_id,omitempty" jsonschema:"Only tasks in this sprint"`
	AssigneeID string `json:"assignee_id,omitempty" jsonschema:"Only tasks assigned to this executor; 'me' for myself and my owner"`
	TeamID     string `json:"team_id,omitempty" jsonschema:"Only tasks whose assignee belongs to this team"`
}

type listProposalsIn struct {
	Status string `json:"status,omitempty" jsonschema:"pending | approved | rejected | expired; omit for all"`
}

type closeSprintIn struct {
	SprintID     string `json:"sprint_id" jsonschema:"Sprint ID"`
	Unfinished   string `json:"unfinished,omitempty" jsonschema:"Where unfinished tasks go: backlog (default) or next"`
	NextSprintID string `json:"next_sprint_id,omitempty" jsonschema:"Sprint to carry unfinished tasks into when unfinished=next"`
}

// ---------- 工具描述（中英） ----------

var toolDescs = map[string]i18n.Text{
	"whoami":          i18n.T("查看当前身份：我是谁、所有者是谁、有哪些授权与能力标签。", "Show who I am: identity, owner, grants and capabilities."),
	"list_my_tasks":   i18n.T("列出分配给我（或我的所有者）的、还没结束的任务。", "List open tasks assigned to me (or my owner)."),
	"list_backlog":    i18n.T("列出待领取任务（没有负责人、符合我的角色与能力标签的任务）。", "List unclaimed tasks (no assignee, matching my role and capabilities)."),
	"get_task_brief":  i18n.T("获取任务说明：描述、所属目标链、评论、前置任务的结果与交付物、任务类型给 Agent 的执行指令、当前可用步骤。开始任何任务前先调用它。", "Get the task brief: description, goal chain, comments, predecessor results and deliverables, agent instructions for the task type, and available steps. Call this before starting any task."),
	"get_workflow":    i18n.T("查看某任务现在处于什么状态、我能走哪些步骤、不能走的原因，以及能否领取/开始执行。", "See the task's current state, which steps I can take, why others are unavailable, and whether I can claim or begin."),
	"claim_task":      i18n.T("从待领取任务里领取一个任务。领取后我就是负责人；若任务处于进行中阶段会立即开始一段执行记录。", "Claim an unclaimed task. I become the assignee; if the task is in an in-progress stage an execution record starts immediately."),
	"begin_task":      i18n.T("开始执行：在任务的进行中阶段开启一段执行记录。上报用量前必须先调用它。", "Begin executing: open an execution record in the task's in-progress stage. Required before reporting usage."),
	"transition_task": i18n.T("推进流程：触发一个步骤（如 start、submit、dev_done、ask_for_input）。步骤名与前提用 get_workflow 查看。", "Advance the workflow: trigger a step (start, submit, dev_done, ask_for_input, ...). Use get_workflow for step names and preconditions."),
	"heartbeat":       i18n.T("心跳并上报累计用量。执行中每 60 秒调用一次；usage 里按模型填累计 token 数（幂等，取最大值）。", "Heartbeat and report cumulative usage. Call every 60 seconds while executing; usage holds cumulative tokens per model (idempotent, max wins)."),
	"add_comment":     i18n.T("在任务上发一条评论（会通知人）。要提问并等待答复请用 transition_task 的 ask_for_input 步骤。", "Post a comment on the task (notifies people). To ask and wait for a reply use the ask_for_input step via transition_task."),
	"add_note":        i18n.T("在任务上写一条工作日志（不打扰人，默认折叠）。用于记录过程与中间结论。", "Write a work note on the task (collapsed by default, no notifications). Use it for process and intermediate findings."),
	"attach_artifact": i18n.T("给任务附上交付物（PR、文档、报表、结果摘要等）。很多步骤要求先附上特定类型的交付物。", "Attach a deliverable (PR, document, report, result summary...). Many steps require a specific deliverable type first."),
	"create_task":     i18n.T("创建任务（需要「创建任务」授权）；填 parent_id 则创建子任务（需要「创建子任务」授权）。", "Create a task (requires the Create tasks grant); set parent_id to create a subtask (requires Create subtasks)."),
	"create_goal":     i18n.T("创建目标（需要「创建目标」授权）。", "Create a goal (requires the Create goals grant)."),
	"link_tasks":      i18n.T("建立两个任务的关联：前置、发现于、相关。发现 Bug 时先 create_task（type_name=bug）再用 found_in 关联到被测任务。", "Link two tasks: blocks, found_in, relates_to. For a bug, create_task with type_name=bug then link it with found_in to the task under test."),
	"assign_task":     i18n.T("把任务指派给某个成员或 Agent（需要「指派」授权）。", "Assign the task to a member or agent (requires the Assign grant)."),
	"list_goals":      i18n.T("浏览目标树（含进度与成本）。", "Browse the goal tree (with progress and cost)."),
	"list_task_types": i18n.T("列出任务类型及其流程定义（状态、步骤、参与角色、交付物要求）。", "List task types and their workflow definitions (states, steps, participant roles, deliverable requirements)."),
	"list_members":    i18n.T("列出组织成员与 Agent 的 ID 和名字（指派、填参与角色时用）。", "List member and agent IDs and names (for assigning and filling participant roles)."),
	"get_task":        i18n.T("读取任务详情（含执行记录、关联、子任务）。", "Read task details (execution records, relations, subtasks)."),
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
		b, err := a.Brief(ctx, sess, in.TaskID)
		if err != nil {
			return f(err)
		}
		return jsonResult(b)
	})
	sdk.AddTool(s, tool("get_workflow"), func(ctx context.Context, req *sdk.CallToolRequest, in taskIDIn) (*sdk.CallToolResult, any, error) {
		v, err := a.Workflow(ctx, sess, in.TaskID)
		if err != nil {
			return f(err)
		}
		return jsonResult(v)
	})
	sdk.AddTool(s, tool("claim_task"), func(ctx context.Context, req *sdk.CallToolRequest, in taskIDIn) (*sdk.CallToolResult, any, error) {
		t, err := a.Claim(ctx, sess, in.TaskID)
		if err != nil {
			return f(err)
		}
		return jsonResult(t)
	})
	sdk.AddTool(s, tool("begin_task"), func(ctx context.Context, req *sdk.CallToolRequest, in taskIDIn) (*sdk.CallToolResult, any, error) {
		t, err := a.Begin(ctx, sess, in.TaskID)
		if err != nil {
			return f(err)
		}
		return jsonResult(t)
	})
	sdk.AddTool(s, tool("transition_task"), func(ctx context.Context, req *sdk.CallToolRequest, in transitionIn) (*sdk.CallToolResult, any, error) {
		t, err := a.Transition(ctx, sess, in.TaskID, in.Name, app.TransitionPayload{Comment: in.Comment, Result: in.Result})
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
		t, err := a.AddComment(ctx, sess, in.TaskID, in.Text, false)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "task_id": t.ID})
	})
	sdk.AddTool(s, tool("add_note"), func(ctx context.Context, req *sdk.CallToolRequest, in commentIn) (*sdk.CallToolResult, any, error) {
		t, err := a.AddComment(ctx, sess, in.TaskID, in.Text, true)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "task_id": t.ID})
	})
	sdk.AddTool(s, tool("attach_artifact"), func(ctx context.Context, req *sdk.CallToolRequest, in artifactIn) (*sdk.CallToolResult, any, error) {
		t, err := a.AddArtifact(ctx, sess, in.TaskID, domain.Artifact{Type: in.Type, Title: in.Title, Ref: in.Ref})
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "task_id": t.ID, "artifacts": t.Artifacts})
	})
	sdk.AddTool(s, tool("create_task"), func(ctx context.Context, req *sdk.CallToolRequest, in createTaskIn) (*sdk.CallToolResult, any, error) {
		ci := app.CreateTaskInput{GoalID: in.GoalID, ParentID: in.ParentID, TypeName: in.TypeName, Title: in.Title, Description: in.Description, AssigneeID: in.AssigneeID, Participants: in.Participants, RequiredCapabilities: in.RequiredCapabilities, EstimateHours: in.EstimateHours, Priority: in.Priority, Ready: in.Ready}
		var err error
		if ci.PlannedStart, err = parseDate(in.PlannedStart); err != nil {
			return f(err)
		}
		if ci.PlannedEnd, err = parseDate(in.PlannedEnd); err != nil {
			return f(err)
		}
		t, err := a.CreateTask(ctx, sess, ci)
		if err != nil {
			return f(err)
		}
		return jsonResult(t)
	})
	sdk.AddTool(s, tool("create_goal"), func(ctx context.Context, req *sdk.CallToolRequest, in createGoalIn) (*sdk.CallToolResult, any, error) {
		gi := app.CreateGoalInput{ParentID: in.ParentID, Title: in.Title, Description: in.Description}
		var err error
		if gi.Deadline, err = parseDate(in.Deadline); err != nil {
			return f(err)
		}
		g, err := a.CreateGoal(ctx, sess, gi)
		if err != nil {
			return f(err)
		}
		return jsonResult(g)
	})
	// 里程碑（ADR 0016）
	sdk.AddTool(s, tool("list_milestones"), func(ctx context.Context, req *sdk.CallToolRequest, in goalIDIn) (*sdk.CallToolResult, any, error) {
		out, err := a.ListMilestones(ctx, sess, in.GoalID)
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, tool("create_milestone"), func(ctx context.Context, req *sdk.CallToolRequest, in createMilestoneIn) (*sdk.CallToolResult, any, error) {
		mi := app.CreateMilestoneInput{GoalID: in.GoalID, Title: in.Title, Description: in.Description}
		var err error
		if mi.DueOn, err = parseDate(in.DueOn); err != nil {
			return f(err)
		}
		m, err := a.CreateMilestone(ctx, sess, mi)
		if err != nil {
			return f(err)
		}
		return jsonResult(m)
	})
	sdk.AddTool(s, tool("reach_milestone"), func(ctx context.Context, req *sdk.CallToolRequest, in milestoneIDIn) (*sdk.CallToolResult, any, error) {
		m, err := a.ReachMilestone(ctx, sess, in.MilestoneID)
		if err != nil {
			return f(err)
		}
		return jsonResult(m)
	})
	sdk.AddTool(s, tool("link_tasks"), func(ctx context.Context, req *sdk.CallToolRequest, in linkIn) (*sdk.CallToolResult, any, error) {
		t, err := a.Link(ctx, sess, in.TaskID, domain.RelationType(in.Type), in.OtherID)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "relations": t.Relations})
	})
	sdk.AddTool(s, tool("assign_task"), func(ctx context.Context, req *sdk.CallToolRequest, in assignIn) (*sdk.CallToolResult, any, error) {
		t, err := a.Assign(ctx, sess, in.TaskID, in.ExecutorID)
		if err != nil {
			return f(err)
		}
		return jsonResult(t)
	})
	sdk.AddTool(s, tool("list_goals"), func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		tree, err := a.GoalTree(ctx, sess)
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
		d, err := a.GetTaskDetail(ctx, sess, in.TaskID)
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
		out, err := a.ListTaskSummaries(ctx, sess, store.TaskFilter{GoalID: in.GoalID, State: in.State, TypeName: in.TypeName})
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
		d, err := a.GetSprint(ctx, sess, in.SprintID)
		if err != nil {
			return f(err)
		}
		return jsonResult(d)
	})
	sdk.AddTool(s, tool("add_tasks_to_sprint"), func(ctx context.Context, req *sdk.CallToolRequest, in addTasksToSprintIn) (*sdk.CallToolResult, any, error) {
		n, err := a.AddTasksToSprint(ctx, sess, in.SprintID, in.TaskIDs)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "added": n})
	})
	sdk.AddTool(s, tool("remove_task_from_sprint"), func(ctx context.Context, req *sdk.CallToolRequest, in removeTaskFromSprintIn) (*sdk.CallToolResult, any, error) {
		if err := a.RemoveTaskFromSprint(ctx, sess, in.SprintID, in.TaskID); err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true})
	})
	sdk.AddTool(s, tool("get_board"), func(ctx context.Context, req *sdk.CallToolRequest, in boardIn) (*sdk.CallToolResult, any, error) {
		b, err := a.Board(ctx, sess, app.BoardFilter{TypeName: in.TypeName, GoalID: in.GoalID, SprintID: in.SprintID, AssigneeID: in.AssigneeID, TeamID: in.TeamID})
		if err != nil {
			return f(err)
		}
		return jsonResult(b)
	})
	sdk.AddTool(s, tool("start_sprint"), func(ctx context.Context, req *sdk.CallToolRequest, in sprintIDIn) (*sdk.CallToolResult, any, error) {
		sp, err := a.StartSprint(ctx, sess, in.SprintID)
		if err != nil {
			return f(err)
		}
		return jsonResult(sp)
	})
	sdk.AddTool(s, tool("close_sprint"), func(ctx context.Context, req *sdk.CallToolRequest, in closeSprintIn) (*sdk.CallToolResult, any, error) {
		res, err := a.CloseSprint(ctx, sess, in.SprintID, in.Unfinished, in.NextSprintID)
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
工作方式：
1. 用 list_my_tasks 看分配给你的任务；用 list_backlog 看可领取的任务。
2. 动手前先 get_task_brief 读任务说明与执行指令，再 get_workflow 看你现在能走哪一步。
3. 进入进行中阶段后先 begin_task 开启执行记录；执行中每 60 秒 heartbeat 并上报累计用量。
4. 交付前用 attach_artifact 附上要求的交付物，再用 transition_task 推进（如 submit、dev_done）。
5. 需求不清就用 transition_task 的 ask_for_input 步骤提问并等待；过程记录用 add_note。
6. 被拒绝时读拒绝理由并据此行动，不要重试同一操作。
7. 要了解团队这段时间在做什么，用 list_sprints / get_sprint 看迭代待办与燃尽，用 get_board 看看板。
8. 有些授权是「需要人确认」：这类操作调用后不会立刻生效，而是记成一条待确认操作，返回里会告诉你等谁确认、待确认操作 ID。别重试，用 list_my_proposals 看进展。`,
	`You are the executor "%s" in AxiomOS.%s
How to work:
1. Use list_my_tasks for tasks assigned to you and list_backlog for claimable tasks.
2. Before acting, read the brief with get_task_brief, then get_workflow to see which step you can take now.
3. After entering an in-progress stage call begin_task to open an execution record; heartbeat every 60 seconds with cumulative usage.
4. Before delivering, attach the required deliverables with attach_artifact, then advance with transition_task (submit, dev_done, ...).
5. If requirements are unclear, use the ask_for_input step via transition_task and wait; use add_note for process notes.
6. When rejected, read the reason and act on it; do not retry the same call.
7. To see what the team is working on right now, use list_sprints / get_sprint for the sprint backlog and burndown, and get_board for the board.
8. Some grants require human confirmation: such a call does not take effect immediately but is recorded as a pending action, and the reply tells you who must confirm it and its ID. Do not retry; check progress with list_my_proposals.`)

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
