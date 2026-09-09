package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// Agent 能力补齐第二批（观测）：看动态与统计、迭代的创建与修改、解除关联、我的通知、只读的组织上下文。

type listEventsIn struct {
	TaskID string `json:"task_id,omitempty" jsonschema:"Only this task: #123, 123, the task ID, or part of the title"`
	GoalID string `json:"goal_id,omitempty" jsonschema:"Only this goal and its sub-goals (goal ID or part of its title)"`
	Since  string `json:"since,omitempty" jsonschema:"Only entries at or after this time: YYYY-MM-DD or RFC3339"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Max entries (default 50, max 200), newest first"`
}

type createSprintIn struct {
	Name           string `json:"name" jsonschema:"Sprint name"`
	Goal           string `json:"goal,omitempty" jsonschema:"Sprint goal, one sentence"`
	TeamID         string `json:"team_id,omitempty" jsonschema:"Team ID (from list_teams); omit for an organization-wide sprint"`
	StartsOn       string `json:"starts_on" jsonschema:"Start date YYYY-MM-DD"`
	EndsOn         string `json:"ends_on" jsonschema:"End date YYYY-MM-DD"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type updateSprintIn struct {
	SprintID       string  `json:"sprint_id" jsonschema:"Sprint reference: the sprint ID or part of its name"`
	Name           *string `json:"name,omitempty" jsonschema:"New name"`
	Goal           *string `json:"goal,omitempty" jsonschema:"New sprint goal"`
	StartsOn       string  `json:"starts_on,omitempty" jsonschema:"New start date YYYY-MM-DD"`
	EndsOn         string  `json:"ends_on,omitempty" jsonschema:"New end date YYYY-MM-DD"`
	DryRun         bool    `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string  `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type notificationsIn struct {
	UnreadOnly bool `json:"unread_only,omitempty" jsonschema:"Only unread ones"`
	Limit      int  `json:"limit,omitempty" jsonschema:"Max entries (default 50, max 200), newest first"`
}

type markReadIn struct {
	IDs []int64 `json:"ids" jsonschema:"Notification IDs from list_my_notifications"`
}

var batch2Descs = map[string]i18n.Text{
	"list_events": i18n.T("看动态：按任务、按目标（含子目标）、按时间起点筛，最新的在前。每条是一句话，与网页上的动态完全一样。复盘、找阻塞、看谁做了什么时用它。",
		"Read the activity stream: filter by task, by goal (with its sub-goals) or by start time, newest first. Each entry is one sentence, identical to the web activity. Use it for retrospectives, finding blockers, and seeing who did what."),
	"get_goal_metrics": i18n.T("一个目标（含子树）的数字：进度、任务按状态类型的分布、逾期与无人认领的数、成本对预算、已完成任务的平均周期、里程碑摘要、成本按执行者归口。看不到这个范围的钱时成本为 0 且 financial 为假。",
		"Numbers for one goal (with its sub-tree): progress, tasks by state type, overdue and unassigned counts, cost against budget, average lead time of finished tasks, milestone summary, cost by executor. When you cannot see this scope's finances, costs are 0 and financial is false."),
	"get_task_metrics": i18n.T("一个任务的数字：从创建到完成（或到现在）的小时数、在当前状态待了多久、被打回几次、执行记录的分布、成本与按模型的用量、评论 / 交付物 / 子任务数。",
		"Numbers for one task: hours from creation to completion (or now), hours in the current state, review rounds, execution record breakdown, cost and usage by model, counts of comments / deliverables / subtasks."),
	"get_my_metrics": i18n.T("我自己的数字：手上的活与逾期、等我验收的、执行记录的成败、被打回的次数、成本与用量（全部与最近 30 天）、并发上限与正在跑的数、我参与的设了预算的目标各花了多少。汇报前或决定要不要再领活时看它。",
		"My own numbers: open and overdue tasks, tasks awaiting my review, execution record outcomes, times I was sent back, cost and usage (total and last 30 days), concurrency limit and active runs, spend on budgeted goals I take part in. Check it before reporting or before claiming more work."),
	"create_sprint": i18n.T("创建一个规划中的迭代（需要「创建任务」授权；授权是需要人确认时会生成待确认操作）。建好后用 add_tasks_to_sprint 放任务；开始它要走 start_sprint（对 Agent 必经人确认）。",
		"Create a sprint in planning (requires the Create tasks grant; when that grant needs confirmation a pending action is recorded). Then add tasks with add_tasks_to_sprint; starting it goes through start_sprint (always confirmed by a person for agents)."),
	"update_sprint": i18n.T("修改迭代的名称、迭代目标或起止日期（需要「创建任务」授权；已结束的迭代不能改）。", "Edit a sprint's name, goal or dates (requires the Create tasks grant; a closed sprint cannot be edited)."),
	"unlink_tasks": i18n.T("解除两个任务之间的一条关联（需要「建立关联」授权）。解除前置关系会改变依赖图，对 Agent 一律先经人确认；其余关系按授权模式。",
		"Remove one relation between two tasks (requires the Link tasks grant). Removing a blocks relation changes the dependency graph, so for agents it always needs human confirmation; other relation kinds follow the grant mode."),
	"list_my_notifications": i18n.T("列出与我有关的通知（挂在我负责、创建、参与或验收的任务上的），可只看未读。被打回、有人评论、被指派都会出现在这里。",
		"List notifications that concern me (on tasks I am assigned to, created, take part in or review), optionally unread only. Send-backs, comments and assignments show up here."),
	"mark_notifications_read": i18n.T("把几条通知标为已读，只能标与我有关的；返回剩下的未读数。已读只是阅读状态，不记动态。", "Mark notifications as read (only ones that concern me); returns the remaining unread count. Read state leaves no activity entry."),
	"list_teams":              i18n.T("列出团队：名称、上级、负责人、成员名单。指派与规划时用。", "List teams: name, parent, lead, members. Use it when assigning and planning."),
	"list_capabilities":       i18n.T("列出组织的能力标签（代码名 → 名称）。创建任务填 required_capabilities 时用。", "List the organization's capability tags (code name → title). Use it when filling required_capabilities on a task."),
	"get_org_context": i18n.T("组织背景一次看全：组织名与货币、可见性策略、我的范围、我是谁（团队、角色、能力标签、授权、并发上限）、团队、能力标签、目标类型、任务类型、角色。只读；规划或指派前先看它。",
		"The organization at a glance: name and currency, visibility policy, my scope, who I am (teams, roles, capabilities, grants, concurrency limit), teams, capability tags, goal types, task types, roles. Read-only; check it before planning or assigning."),
}

func init() {
	for k, v := range batch2Descs {
		toolDescs[k] = v
	}
}

func addBatch2Tools(s *sdk.Server, k *kit) {
	a, sess, f := k.a, k.sess, k.f

	sdk.AddTool(s, k.tool("list_events"), func(ctx context.Context, req *sdk.CallToolRequest, in listEventsIn) (*sdk.CallToolResult, any, error) {
		ef := app.EventFilter{Limit: in.Limit}
		var err error
		if ef.TaskID, err = k.tid(ctx, in.TaskID); err != nil {
			return f(err)
		}
		if ef.GoalID, err = k.gid(ctx, in.GoalID); err != nil {
			return f(err)
		}
		if ef.Since, err = parseDate(in.Since); err != nil {
			return f(err)
		}
		out, err := a.ListEventLines(ctx, sess, ef)
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, k.tool("get_goal_metrics"), func(ctx context.Context, req *sdk.CallToolRequest, in goalIDIn) (*sdk.CallToolResult, any, error) {
		id, err := k.gid(ctx, in.GoalID)
		if err != nil {
			return f(err)
		}
		if id == "" {
			return f(app.Bad("err.goal_missing"))
		}
		m, err := a.GoalMetricsOf(ctx, sess, id)
		if err != nil {
			return f(err)
		}
		return jsonResult(m)
	})
	sdk.AddTool(s, k.tool("get_task_metrics"), func(ctx context.Context, req *sdk.CallToolRequest, in taskIDIn) (*sdk.CallToolResult, any, error) {
		id, err := k.tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		m, err := a.TaskMetricsOf(ctx, sess, id)
		if err != nil {
			return f(err)
		}
		return jsonResult(m)
	})
	sdk.AddTool(s, k.tool("get_my_metrics"), func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		m, err := a.MyMetricsOf(ctx, sess)
		if err != nil {
			return f(err)
		}
		return jsonResult(m)
	})

	sdk.AddTool(s, k.tool("create_sprint"), func(ctx context.Context, req *sdk.CallToolRequest, in createSprintIn) (*sdk.CallToolResult, any, error) {
		ci := app.CreateSprintInput{Name: in.Name, Goal: in.Goal, TeamID: in.TeamID}
		var err error
		if ci.StartsOn, err = parseDate(in.StartsOn); err != nil {
			return f(err)
		}
		if ci.EndsOn, err = parseDate(in.EndsOn); err != nil {
			return f(err)
		}
		sp, err := a.CreateSprint(ctx, k.ws(in.DryRun, in.IdempotencyKey), ci)
		if err != nil {
			return f(err)
		}
		return jsonResult(sp)
	})
	sdk.AddTool(s, k.tool("update_sprint"), func(ctx context.Context, req *sdk.CallToolRequest, in updateSprintIn) (*sdk.CallToolResult, any, error) {
		spID, err := k.sid(ctx, in.SprintID)
		if err != nil {
			return f(err)
		}
		ui := app.UpdateSprintInput{Name: in.Name, Goal: in.Goal}
		if ui.StartsOn, err = parseDate(in.StartsOn); err != nil {
			return f(err)
		}
		if ui.EndsOn, err = parseDate(in.EndsOn); err != nil {
			return f(err)
		}
		sp, err := a.UpdateSprint(ctx, k.ws(in.DryRun, in.IdempotencyKey), spID, ui)
		if err != nil {
			return f(err)
		}
		return jsonResult(sp)
	})
	sdk.AddTool(s, k.tool("unlink_tasks"), func(ctx context.Context, req *sdk.CallToolRequest, in linkIn) (*sdk.CallToolResult, any, error) {
		id, err := k.tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		other, err := k.tid(ctx, in.OtherID)
		if err != nil {
			return f(err)
		}
		t, err := a.Unlink(ctx, k.ws(in.DryRun, in.IdempotencyKey), id, domain.RelationType(in.Type), other)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "relations": t.Relations})
	})

	sdk.AddTool(s, k.tool("list_my_notifications"), func(ctx context.Context, req *sdk.CallToolRequest, in notificationsIn) (*sdk.CallToolResult, any, error) {
		out, err := a.MyNotificationLines(ctx, sess, in.UnreadOnly, in.Limit)
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, k.tool("mark_notifications_read"), func(ctx context.Context, req *sdk.CallToolRequest, in markReadIn) (*sdk.CallToolResult, any, error) {
		n, err := a.MarkMyNotificationsRead(ctx, sess, in.IDs)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "unread": n})
	})

	sdk.AddTool(s, k.tool("list_teams"), func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		out, err := a.ListTeamBriefs(ctx, sess)
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, k.tool("list_capabilities"), func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		caps, err := a.Capabilities(ctx, sess)
		if err != nil {
			return f(err)
		}
		out := map[string]string{}
		for name, t := range caps {
			out[name] = t.In(k.loc)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, k.tool("get_org_context"), func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		out, err := a.OrgContextOf(ctx, sess)
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
}
