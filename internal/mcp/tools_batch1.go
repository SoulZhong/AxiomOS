package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// Agent 能力补齐第一批（docs/plans/2026-09-agent-capabilities.md）：目标的读与改、任务编辑、外部链接、
// 里程碑完整生命周期、验收、创建子任务别名。每个写工具都收 dry_run 与 idempotency_key，
// 指代都走 ResolveRef，拒绝理由只用现有的句子（ADR 0025）。

// kit 是注册工具时共用的那几样东西：应用、会话、失败渲染、各类指代解析器与写开关。
type kit struct {
	a    *app.App
	sess *app.Session
	loc  i18n.Locale
	tool func(name string) *sdk.Tool
	f    func(err error) (*sdk.CallToolResult, any, error)
	tid  func(ctx context.Context, ref string) (string, error)
	gid  func(ctx context.Context, ref string) (string, error)
	mid  func(ctx context.Context, ref string) (string, error)
	sid  func(ctx context.Context, ref string) (string, error)
	ws   func(dry bool, key string) *app.Session
}

func (k *kit) gtid(ctx context.Context, ref string) (string, error) {
	return k.a.ResolveRef(ctx, k.sess, app.RefGoalType, ref)
}

// ---------- 输入 ----------

type goalWriteIn struct {
	GoalID         string `json:"goal_id" jsonschema:"Goal reference: the goal ID or part of its title"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type updateGoalIn struct {
	GoalID         string  `json:"goal_id" jsonschema:"Goal reference: the goal ID or part of its title"`
	Title          *string `json:"title,omitempty" jsonschema:"New title"`
	Description    *string `json:"description,omitempty" jsonschema:"New description"`
	Outcome        *string `json:"outcome,omitempty" jsonschema:"One-line outcome metric; empty string clears it"`
	Deadline       *string `json:"deadline,omitempty" jsonschema:"Deadline YYYY-MM-DD; empty string clears it"`
	PlannedStart   *string `json:"planned_start,omitempty" jsonschema:"Planned start YYYY-MM-DD; empty string clears it"`
	PlannedEnd     *string `json:"planned_end,omitempty" jsonschema:"Planned end YYYY-MM-DD (also the deadline unless deadline is given); empty string clears it"`
	DatePrecision  *string `json:"date_precision,omitempty" jsonschema:"How precise the dates are: week | month | quarter | half | year; empty string means week"`
	Horizon        *string `json:"horizon,omitempty" jsonschema:"Horizon bucket: now | next | later; empty string means not scheduled"`
	Confidence     *string `json:"confidence,omitempty" jsonschema:"Confidence: high | medium | low; empty string clears it"`
	TypeName       *string `json:"type_name,omitempty" jsonschema:"Goal type: the type ID or its name; empty string clears it (uncategorized)"`
	OwnerID        string  `json:"owner_id,omitempty" jsonschema:"New owner: @name, member ID or email. For agents this always needs human confirmation"`
	ParentID       *string `json:"parent_id,omitempty" jsonschema:"New parent goal (ID or part of its title); empty string makes it a top-level goal. For agents this always needs human confirmation"`
	DryRun         bool    `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string  `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type goalHorizonIn struct {
	GoalIDs        []string `json:"goal_ids" jsonschema:"Goals to move: IDs or parts of their titles"`
	Horizon        string   `json:"horizon" jsonschema:"Target horizon bucket: now | next | later; empty puts them back to not scheduled"`
	DryRun         bool     `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string   `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type goalRankIn struct {
	GoalIDs        []string `json:"goal_ids" jsonschema:"Goals in the wanted order, top to bottom: IDs or parts of their titles"`
	DryRun         bool     `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string   `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type goalNoteIn struct {
	GoalID         string `json:"goal_id" jsonschema:"Goal reference: the goal ID or part of its title"`
	Text           string `json:"text" jsonschema:"One progress note: what moved, what is blocked, what comes next"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type updateTaskIn struct {
	TaskID         string         `json:"task_id" jsonschema:"Task reference: #123, 123, the task ID, or part of the title"`
	Title          *string        `json:"title,omitempty" jsonschema:"New title"`
	Description    *string        `json:"description,omitempty" jsonschema:"New description"`
	EstimateHours  *float64       `json:"estimate_hours,omitempty" jsonschema:"Estimate in hours"`
	PlannedStart   *string        `json:"planned_start,omitempty" jsonschema:"Planned start YYYY-MM-DD; empty string clears it"`
	PlannedEnd     *string        `json:"planned_end,omitempty" jsonschema:"Planned end YYYY-MM-DD; empty string clears it"`
	Points         *int           `json:"points,omitempty" jsonschema:"Story points"`
	Fields         map[string]any `json:"fields,omitempty" jsonschema:"Custom fields of the task type (merged)"`
	Clear          []string       `json:"clear,omitempty" jsonschema:"Fields to clear: estimate_hours | planned_start | planned_end | points"`
	DryRun         bool           `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string         `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type reviewIn struct {
	TaskID              string   `json:"task_id" jsonschema:"Task reference: #123, 123, the task ID, or part of the title"`
	Decision            string   `json:"decision" jsonschema:"accept (approve) or reject (send back)"`
	Comment             string   `json:"comment,omitempty" jsonschema:"What you checked and why; required when rejecting"`
	CheckedDeliverables []string `json:"checked_deliverables,omitempty" jsonschema:"Deliverables you verified: type codes or titles that are attached to the task"`
	DryRun              bool     `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey      string   `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type addLinkIn struct {
	TaskID         string `json:"task_id" jsonschema:"Task reference: #123, 123, the task ID, or part of the title"`
	URL            string `json:"url" jsonschema:"http(s) address"`
	Kind           string `json:"kind,omitempty" jsonschema:"pr | issue | doc | design | other (default other)"`
	Title          string `json:"title,omitempty" jsonschema:"Display title (defaults to the address)"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type removeLinkIn struct {
	TaskID         string `json:"task_id" jsonschema:"Task reference: #123, 123, the task ID, or part of the title"`
	LinkID         string `json:"link_id" jsonschema:"Link ID from list_task_links"`
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type updateMilestoneIn struct {
	MilestoneID    string  `json:"milestone_id" jsonschema:"Milestone ID"`
	Title          *string `json:"title,omitempty" jsonschema:"New title"`
	DueOn          string  `json:"due_on,omitempty" jsonschema:"New date YYYY-MM-DD"`
	Description    *string `json:"description,omitempty" jsonschema:"New description"`
	DryRun         bool    `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey string  `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

type createSubtaskIn struct {
	ParentID             string            `json:"parent_id" jsonschema:"Parent task: #123, 123, the task ID, or part of the title"`
	TypeName             string            `json:"type_name,omitempty" jsonschema:"Task type code name, default generic"`
	Title                string            `json:"title" jsonschema:"Title"`
	Description          string            `json:"description,omitempty" jsonschema:"Description"`
	AssigneeID           string            `json:"assignee_id,omitempty" jsonschema:"Assignee: @name, member or agent ID, or email"`
	Participants         map[string]string `json:"participants,omitempty" jsonschema:"Participant slot -> person reference"`
	RequiredCapabilities []string          `json:"required_capabilities,omitempty" jsonschema:"Capabilities required to claim"`
	EstimateHours        *float64          `json:"estimate_hours,omitempty" jsonschema:"Estimate in hours"`
	PlannedStart         string            `json:"planned_start,omitempty" jsonschema:"Planned start date YYYY-MM-DD"`
	PlannedEnd           string            `json:"planned_end,omitempty" jsonschema:"Planned end date YYYY-MM-DD"`
	Priority             *int              `json:"priority,omitempty" jsonschema:"Priority 0 (highest) to 3"`
	Ready                bool              `json:"ready,omitempty" jsonschema:"Mark ready right after creation (skip draft)"`
	DryRun               bool              `json:"dry_run,omitempty" jsonschema:"Only say what would happen; every check still runs, nothing is written"`
	IdempotencyKey       string            `json:"idempotency_key,omitempty" jsonschema:"Client-generated key (max 64 chars): the same key within 24 hours takes effect only once"`
}

// ---------- 描述 ----------

var batch1Descs = map[string]i18n.Text{
	"get_goal": i18n.T("读一个目标的完整详情：说明、负责人、上级链、子目标、直接挂着的任务、里程碑、成本与预算、进展说明。规划任务或汇报进展前先看它。",
		"Read one goal in full: description, owner, parent chain, sub-goals, its direct tasks, milestones, cost and budget, progress notes. Read it before planning tasks or reporting progress."),
	"update_goal": i18n.T("修改目标的标题、说明、日期、时间粒度、时间桶、信心度、成果指标、类型（需要「创建目标」授权，只能改自己能编辑的目标）。负责人与上级是问责链，Agent 改它们一律先经人确认；达成 / 放弃请用专门的工具。",
		"Edit a goal's title, description, dates, date precision, horizon, confidence, outcome or type (requires the Create goals grant; only goals you may edit). Owner and parent form the accountability chain, so for agents changing them always needs human confirmation; use the dedicated tools to achieve or abandon."),
	"achieve_goal": i18n.T("确认目标已达成（对 Agent 一律先经人确认）。只在目标的成果真的做到了之后调用；先用 get_goal 看任务与里程碑是否都已完成。",
		"Mark a goal as achieved (for agents this always needs human confirmation). Call it only once the outcome is really done; check tasks and milestones with get_goal first."),
	"unachieve_goal": i18n.T("撤销目标的达成，让它回到进行中（对 Agent 一律先经人确认）。", "Undo a goal's achieved status and return it to active (for agents this always needs human confirmation)."),
	"abandon_goal":   i18n.T("放弃目标（对 Agent 一律先经人确认）。目标与它的历史都留着，只是不再推进。", "Abandon a goal (for agents this always needs human confirmation). The goal and its history stay; it just stops moving."),
	"restart_goal":   i18n.T("重新开始一个已放弃的目标（对 Agent 一律先经人确认）。", "Resume an abandoned goal (for agents this always needs human confirmation)."),
	"set_goal_horizon": i18n.T("把一批目标放进同一个时间桶（现在 / 下一步 / 以后 / 还没排期）。需要「创建目标」授权；每个目标单独判权限，改不了的会列在 skipped 里并给出理由。",
		"Move a batch of goals into one horizon (now / next / later / not scheduled). Requires the Create goals grant; permission is checked per goal and the ones you cannot change are listed in skipped with a reason."),
	"rank_goals": i18n.T("按给定顺序重排一批目标（同一层里的手动次序，排在前面的更要紧）。需要「创建目标」授权；次序不记动态。",
		"Reorder a batch of goals in the given order (manual order within one level; earlier means more important). Requires the Create goals grant; the order itself leaves no activity entry."),
	"add_goal_note": i18n.T("在目标上写一句进展说明（需要「评论」授权）：推进了什么、卡在哪、下一步做什么。它记成一条动态，目标详情与动态流里都能看到；不改目标的任何字段。",
		"Write one progress note on a goal (requires the Comment grant): what moved, what is blocked, what comes next. It is recorded as an activity entry visible in the goal detail and the activity stream; it changes no goal field."),
	"update_task": i18n.T("修改任务的标题、说明、预估工时、计划起止、工作量、自定义字段（需要「执行任务」授权；只能改自己负责或自己创建的任务）。验收人、优先级、归属目标、上级任务、迭代、参与角色、所需能力、「仅限人工」由人决定，Agent 不能改；要改负责人用 assign_task，要进出迭代用 add_tasks_to_sprint。",
		"Edit a task's title, description, estimate, planned dates, points or custom fields (requires the Execute grant; only tasks you are assigned to or created). Reviewer, priority, goal, parent task, sprint, participants, required capabilities and human-only are for people to decide and cannot be changed by agents; use assign_task for the assignee and add_tasks_to_sprint for sprints."),
	"review_task": i18n.T("作为验收人通过（accept）或打回（reject）一个任务（需要「验收」授权）。先用 get_workflow 看 is_reviewer 与 review_accept_step / review_reject_step；打回必须写说明；checked_deliverables 里列你核对过的交付物，必须真的挂在任务上。内部走的是与人相同的验收步骤。",
		"As the reviewer, accept or reject (send back) a task (requires the Review grant). First check is_reviewer and review_accept_step / review_reject_step in get_workflow; rejecting requires a comment; list the deliverables you verified in checked_deliverables (they must be attached to the task). Internally this takes the same review step a person would."),
	"list_task_links":      i18n.T("列出任务上的外部链接（PR、Issue、文档、设计稿等），含 ID、种类、地址与状态。", "List the external links on a task (PR, issue, document, design...), with ID, kind, address and status."),
	"add_external_link":    i18n.T("给任务挂一条外部链接（与发评论同一套规则：需要「执行任务」授权，授权是需要人确认时会生成待确认操作）。同一任务同一地址只有一条，重复即更新。", "Attach an external link to a task (same rules as commenting: requires the Execute grant; when that grant needs confirmation a pending action is recorded). One entry per address per task; attaching the same address again updates it."),
	"remove_external_link": i18n.T("摘掉任务上的一条外部链接（需要「执行任务」授权，授权是需要人确认时会生成待确认操作）。link_id 用 list_task_links 查。", "Remove an external link from a task (requires the Execute grant; when that grant needs confirmation a pending action is recorded). Get link_id from list_task_links."),
	"update_milestone":     i18n.T("修改里程碑的名称、日期或说明（需要「创建目标」授权；授权是需要人确认时会生成待确认操作）。", "Edit a milestone's title, date or description (requires the Create goals grant; when that grant needs confirmation a pending action is recorded)."),
	"delete_milestone":     i18n.T("删除一条里程碑（对 Agent 一律先经人确认）。", "Delete a milestone (for agents this always needs human confirmation)."),
	"unreach_milestone":    i18n.T("撤销里程碑的「已达到」确认（对 Agent 一律先经人确认）。", "Undo a milestone's reached confirmation (for agents this always needs human confirmation)."),
	"create_subtask":       i18n.T("在某个任务下创建子任务（需要「创建子任务」授权）。与 create_task 填 parent_id 是同一件事，这里只是名字更直白。", "Create a subtask under a task (requires the Create subtasks grant). Same as create_task with parent_id; this name is just more direct."),
}

func init() {
	for k, v := range batch1Descs {
		toolDescs[k] = v
	}
	toolDescs["list_goals"] = i18n.T("浏览目标树（给 Agent 看的精简视图：负责人、状态、日期、时间桶、进度、成本、里程碑），要看单个目标的任务与进展说明用 get_goal。",
		"Browse the goal tree (a compact view for agents: owner, status, dates, horizon, progress, cost, milestones); use get_goal for one goal's tasks and progress notes.")
}

// optDate 把「不填 / 空串 / 日期」三种写法换成应用层的「不改 / 清空 / 改成」。
func optDate(s *string, field string, clear *[]string) (*timeT, error) {
	if s == nil {
		return nil, nil
	}
	if *s == "" {
		*clear = append(*clear, field)
		return nil, nil
	}
	return parseDate(*s)
}

// ---------- 注册 ----------

func addBatch1Tools(s *sdk.Server, k *kit) {
	a, sess, f := k.a, k.sess, k.f

	sdk.AddTool(s, k.tool("get_goal"), func(ctx context.Context, req *sdk.CallToolRequest, in goalIDIn) (*sdk.CallToolResult, any, error) {
		id, err := k.gid(ctx, in.GoalID)
		if err != nil {
			return f(err)
		}
		if id == "" {
			return f(app.Bad("err.goal_missing"))
		}
		b, err := a.GoalBriefDetail(ctx, sess, id)
		if err != nil {
			return f(err)
		}
		return jsonResult(b)
	})
	sdk.AddTool(s, k.tool("update_goal"), func(ctx context.Context, req *sdk.CallToolRequest, in updateGoalIn) (*sdk.CallToolResult, any, error) {
		id, err := k.gid(ctx, in.GoalID)
		if err != nil {
			return f(err)
		}
		ui := app.UpdateGoalInput{Title: in.Title, Description: in.Description, Outcome: in.Outcome}
		if ui.Deadline, err = optDate(in.Deadline, "deadline", &ui.Clear); err != nil {
			return f(err)
		}
		if ui.PlannedStart, err = optDate(in.PlannedStart, "planned_start", &ui.Clear); err != nil {
			return f(err)
		}
		if ui.PlannedEnd, err = optDate(in.PlannedEnd, "planned_end", &ui.Clear); err != nil {
			return f(err)
		}
		// 计划结束同时也是截止日（与网页一致），显式给了 deadline 时以 deadline 为准
		if in.PlannedEnd != nil && in.Deadline == nil {
			ui.Deadline = ui.PlannedEnd
			if *in.PlannedEnd == "" {
				ui.Clear = append(ui.Clear, "deadline")
			}
		}
		if in.DatePrecision != nil {
			p := domain.DatePrecision(*in.DatePrecision)
			ui.DatePrecision = &p
		}
		if in.Horizon != nil {
			h := domain.GoalHorizon(*in.Horizon)
			ui.Horizon = &h
		}
		if in.Confidence != nil {
			c := domain.GoalConfidence(*in.Confidence)
			ui.Confidence = &c
		}
		if in.TypeName != nil {
			typeID := ""
			if *in.TypeName != "" {
				if typeID, err = k.gtid(ctx, *in.TypeName); err != nil {
					return f(err)
				}
			}
			ui.TypeID = &typeID
		}
		if in.OwnerID != "" {
			owner, err := k.mid(ctx, in.OwnerID)
			if err != nil {
				return f(err)
			}
			ui.OwnerMemberID = &owner
		}
		if in.ParentID != nil {
			parent := ""
			if *in.ParentID != "" {
				if parent, err = k.gid(ctx, *in.ParentID); err != nil {
					return f(err)
				}
			}
			ui.ParentID = &parent
		}
		g, err := a.UpdateGoal(ctx, k.ws(in.DryRun, in.IdempotencyKey), id, ui)
		if err != nil {
			return f(err)
		}
		b, err := a.GoalBriefDetail(ctx, sess, g.ID)
		if err != nil {
			return f(err)
		}
		return jsonResult(b)
	})
	status := func(name string, to domain.GoalStatus, from ...domain.GoalStatus) {
		sdk.AddTool(s, k.tool(name), func(ctx context.Context, req *sdk.CallToolRequest, in goalWriteIn) (*sdk.CallToolResult, any, error) {
			id, err := k.gid(ctx, in.GoalID)
			if err != nil {
				return f(err)
			}
			g, err := a.ChangeGoalStatus(ctx, k.ws(in.DryRun, in.IdempotencyKey), id, to, from)
			if err != nil {
				return f(err)
			}
			b, err := a.GoalBriefDetail(ctx, sess, g.ID)
			if err != nil {
				return f(err)
			}
			return jsonResult(b)
		})
	}
	status("achieve_goal", domain.GoalAchieved, domain.GoalActive, domain.GoalDraft)
	status("unachieve_goal", domain.GoalActive, domain.GoalAchieved)
	status("abandon_goal", domain.GoalAbandoned, domain.GoalActive, domain.GoalDraft)
	status("restart_goal", domain.GoalActive, domain.GoalAbandoned)

	sdk.AddTool(s, k.tool("set_goal_horizon"), func(ctx context.Context, req *sdk.CallToolRequest, in goalHorizonIn) (*sdk.CallToolResult, any, error) {
		ids, err := a.ResolveRefs(ctx, sess, app.RefGoal, in.GoalIDs)
		if err != nil {
			return f(err)
		}
		out, err := a.BulkGoalHorizon(ctx, k.ws(in.DryRun, in.IdempotencyKey), app.BulkGoalHorizonInput{IDs: ids, Horizon: domain.GoalHorizon(in.Horizon)})
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, k.tool("rank_goals"), func(ctx context.Context, req *sdk.CallToolRequest, in goalRankIn) (*sdk.CallToolResult, any, error) {
		ids, err := a.ResolveRefs(ctx, sess, app.RefGoal, in.GoalIDs)
		if err != nil {
			return f(err)
		}
		out, err := a.SetGoalRanks(ctx, k.ws(in.DryRun, in.IdempotencyKey), app.GoalRankInput{IDs: ids})
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, k.tool("add_goal_note"), func(ctx context.Context, req *sdk.CallToolRequest, in goalNoteIn) (*sdk.CallToolResult, any, error) {
		id, err := k.gid(ctx, in.GoalID)
		if err != nil {
			return f(err)
		}
		if id == "" {
			return f(app.Bad("err.goal_missing"))
		}
		n, err := a.AddGoalNote(ctx, k.ws(in.DryRun, in.IdempotencyKey), id, in.Text)
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "goal_id": id, "note": n})
	})

	// 任务编辑与验收
	sdk.AddTool(s, k.tool("update_task"), func(ctx context.Context, req *sdk.CallToolRequest, in updateTaskIn) (*sdk.CallToolResult, any, error) {
		id, err := k.tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		ui := app.UpdateTaskInput{Title: in.Title, Description: in.Description, EstimateHours: in.EstimateHours, Fields: in.Fields}
		for _, c := range in.Clear {
			switch c {
			case "estimate_hours", "planned_start", "planned_end":
				ui.Clear = append(ui.Clear, c)
			case "points":
				ui.SetPoints = true
			}
		}
		if ui.PlannedStart, err = optDate(in.PlannedStart, "planned_start", &ui.Clear); err != nil {
			return f(err)
		}
		if ui.PlannedEnd, err = optDate(in.PlannedEnd, "planned_end", &ui.Clear); err != nil {
			return f(err)
		}
		if in.Points != nil {
			ui.Points, ui.SetPoints = in.Points, true
		}
		t, err := a.UpdateTask(ctx, k.ws(in.DryRun, in.IdempotencyKey), id, ui)
		if err != nil {
			return f(err)
		}
		return jsonResult(t)
	})
	sdk.AddTool(s, k.tool("review_task"), func(ctx context.Context, req *sdk.CallToolRequest, in reviewIn) (*sdk.CallToolResult, any, error) {
		id, err := k.tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		t, err := a.ReviewTask(ctx, k.ws(in.DryRun, in.IdempotencyKey), id, app.ReviewInput{Decision: in.Decision, Comment: in.Comment, CheckedDeliverables: in.CheckedDeliverables})
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "decision": in.Decision, "task": t})
	})
	sdk.AddTool(s, k.tool("create_subtask"), func(ctx context.Context, req *sdk.CallToolRequest, in createSubtaskIn) (*sdk.CallToolResult, any, error) {
		if in.ParentID == "" {
			return f(app.Bad("err.parent_required"))
		}
		ci := app.CreateTaskInput{TypeName: in.TypeName, Title: in.Title, Description: in.Description, Participants: map[string]string{}, RequiredCapabilities: in.RequiredCapabilities, EstimateHours: in.EstimateHours, Priority: in.Priority, Ready: in.Ready}
		var err error
		if ci.ParentID, err = k.tid(ctx, in.ParentID); err != nil {
			return f(err)
		}
		if ci.AssigneeID, err = k.mid(ctx, in.AssigneeID); err != nil {
			return f(err)
		}
		for slot, ref := range in.Participants {
			who, err := k.mid(ctx, ref)
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
		t, err := a.CreateTask(ctx, k.ws(in.DryRun, in.IdempotencyKey), ci)
		if err != nil {
			return f(err)
		}
		return jsonResult(t)
	})

	// 外部链接（ADR 0020、ADR 0003 第 35 条）
	sdk.AddTool(s, k.tool("list_task_links"), func(ctx context.Context, req *sdk.CallToolRequest, in taskIDIn) (*sdk.CallToolResult, any, error) {
		id, err := k.tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		out, err := a.TaskLinks(ctx, sess, id)
		if err != nil {
			return f(err)
		}
		return jsonResult(out)
	})
	sdk.AddTool(s, k.tool("add_external_link"), func(ctx context.Context, req *sdk.CallToolRequest, in addLinkIn) (*sdk.CallToolResult, any, error) {
		id, err := k.tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		l, err := a.AddTaskLink(ctx, k.ws(in.DryRun, in.IdempotencyKey), id, app.LinkInput{Kind: in.Kind, URL: in.URL, Title: in.Title})
		if err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "task_id": id, "link": l})
	})
	sdk.AddTool(s, k.tool("remove_external_link"), func(ctx context.Context, req *sdk.CallToolRequest, in removeLinkIn) (*sdk.CallToolResult, any, error) {
		id, err := k.tid(ctx, in.TaskID)
		if err != nil {
			return f(err)
		}
		if err := a.RemoveTaskLink(ctx, k.ws(in.DryRun, in.IdempotencyKey), id, in.LinkID); err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "task_id": id, "removed": in.LinkID})
	})

	// 里程碑（ADR 0016）
	sdk.AddTool(s, k.tool("update_milestone"), func(ctx context.Context, req *sdk.CallToolRequest, in updateMilestoneIn) (*sdk.CallToolResult, any, error) {
		ui := app.UpdateMilestoneInput{Title: in.Title, Description: in.Description}
		var err error
		if ui.DueOn, err = parseDate(in.DueOn); err != nil {
			return f(err)
		}
		m, err := a.UpdateMilestone(ctx, k.ws(in.DryRun, in.IdempotencyKey), in.MilestoneID, ui)
		if err != nil {
			return f(err)
		}
		return jsonResult(m)
	})
	sdk.AddTool(s, k.tool("delete_milestone"), func(ctx context.Context, req *sdk.CallToolRequest, in milestoneIDIn) (*sdk.CallToolResult, any, error) {
		if err := a.DeleteMilestone(ctx, k.ws(in.DryRun, in.IdempotencyKey), in.MilestoneID); err != nil {
			return f(err)
		}
		return jsonResult(map[string]any{"ok": true, "deleted": in.MilestoneID})
	})
	sdk.AddTool(s, k.tool("unreach_milestone"), func(ctx context.Context, req *sdk.CallToolRequest, in milestoneIDIn) (*sdk.CallToolResult, any, error) {
		m, err := a.UnreachMilestone(ctx, k.ws(in.DryRun, in.IdempotencyKey), in.MilestoneID)
		if err != nil {
			return f(err)
		}
		return jsonResult(m)
	})
}
