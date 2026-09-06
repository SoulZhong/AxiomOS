# HTTP 接口参考（/api/v1）

给网页用的接口。返回形状与 `web/src/lib/api.ts` 里的类型一一对应；Agent 请用 MCP（`docs/agent-integration.md`）。

人类用户通过登录后的 Cookie（`axiomos_session`）或 `Authorization: Bearer <会话令牌>` 访问；Agent 令牌（`axm_...`）也能调这些接口。所有日期字段是 `YYYY-MM-DD`，时间字段是 RFC3339。金额已折算成组织结算货币。

错误统一为：

```json
{"error": {"code": "rejected", "message": "缺少交付物：测试报告"}}
```

状态码：400 输入有误、401 未登录、403 无权、404 不存在、409 流程规则拒绝。`message` 是给人看的句子，按请求者语言（成员设置，未登录时按 `Accept-Language`）输出。

## 登录

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/auth/login` | `{email, password, org?}` → 会话（见下），同时设置 Cookie |
| POST | `/auth/logout` | 204 |
| GET | `/auth/me` | 会话：`{member{id,name,email,roles[],team_id}, organization{id,name,owner_id,currency}, roles[{name,title}], teams[{id,name,lead_id,parent_id}], artifact_types{代码名: 中文}, capabilities[]}` |

执行者引用（下文的 `ExecutorRef`）统一为 `{id, kind: member|agent, name, owner_id?}`。

## 目标

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/goals` | 目标树：`[{id,title,description,owner: ExecutorRef,parent_id,team_id,progress,achieved,status,budget,cost,over_budget,planned_start,planned_end,actual_start,actual_end,deadline,task_count,done_task_count,children[]}]` |
| POST | `/goals` | `{title, description?, owner_id?, parent_id?, team_id?, budget?, planned_start?, planned_end?, deadline?}` → 目标 |
| GET | `/goals/{id}` | 单个目标（含子树） |
| PATCH | `/goals/{id}` | 同上字段的任意子集（含 `parent_id`：换上级，空字符串表示提为顶级；不能挂到自己或自己的子目标下，目标树深度上限不变），另有 `achieved: bool`（确认达成 / 取消达成）与 `status: "active"\|"abandoned"`（放弃 / 重新开始）。每个字段的改动各产生一条动态 `GoalFieldChanged`（`data{goal_id, title, field, from, to, from_title?, to_title?, currency?}`），句子含旧值与新值；`budget` / `planned_start` / `planned_end` / `deadline` 传 `null` 表示清空 |
| DELETE | `/goals/{id}` | 删除目标。只有空目标（没有子目标、没有任务）能删；否则 400 并说明还有多少子目标和任务，请改为放弃 |

## 里程碑（ADR 0016）

里程碑对象：`{id, goal_id, title, description, due_on(YYYY-MM-DD), reached_at|null, status: upcoming|reached|overdue, ready_hint: bool, created_by: ExecutorRef, created_at, updated_at}`。`ready_hint` 的口径：目标子树里所有计划结束早于或等于该日期的任务（跳过已终止的）都已进入成功终态，且至少有一个这样的任务；已达到的里程碑恒为假。`status` 由日期与 `reached_at` 算出；`ready_hint` 为真表示该日期前计划结束的任务都已完成、可以确认了。目标对象（树与详情）新增 `milestones[]`（按日期升序）与 `milestone_summary{total, reached, overdue, next{title,due_on}|null}`；`/gantt` 按目标分组时目标行带 `milestones[]`。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/goals/{id}/milestones` | 该目标的里程碑（不含子目标） |
| POST | `/goals/{id}/milestones` | `{title, due_on, description?}` → 里程碑（能编辑该目标的人；Agent 受「创建目标」授权约束） |
| PATCH | `/milestones/{id}` | `{title?, due_on?, description?}` |
| DELETE | `/milestones/{id}` | 删除（产生动态） |
| POST | `/milestones/{id}/reach` | 确认已达到 → 里程碑 |
| POST | `/milestones/{id}/unreach` | 撤销确认 |

动态种类：`MilestoneCreated`、`MilestoneUpdated`、`MilestoneReached`、`MilestoneUnreached`、`MilestoneDeleted`，摘要按语言渲染并带目标标题与日期。Agent 走待确认操作时的动作名：`milestone.create / update / delete / reach / unreach`。MCP 新增 `list_milestones`、`create_milestone`、`reach_milestone`。目标删除会连带删除其里程碑（里程碑是目标的一部分）；"有子目标或任务不能删"的规则不变。

## 任务

任务对象：`{id, goal_id, goal{id,title}, parent_id, type, type_title, type_version, title, description, state{name,title,label,weight?,claimable?}, previous_state, creator, assignee, reviewer (ExecutorRef), participants{位置: {title, role, executor}}, required_role, pending_participant, required_capabilities[], human_only, relations[], artifacts[], comments[], runs[], subtasks[], planned_start, planned_end, actual_start, actual_end, estimate, priority: urgent|high|normal|low, progress, overdue, total_tokens, cost, fields, created_at, updated_at}`。

- 关联：`{id, type: blocks|found_in|related, from{id,title,type,state}, to{...}}`。`blocks` 时 from 是前置；`found_in` 时 from 是 Bug。
- 交付物：`{id, type, title, url, attached_by, created_at}`。
- 评论：`{id, kind: comment|note, author, body, created_at}`。
- 执行记录：`{id, task_id, state, state_title, executor, started_at, ended_at, outcome: running|ended|cancelled, usage[{model,input_tokens,output_tokens,total_tokens,duration_ms,tool_calls}], total_tokens, cost}`。

列表接口返回精简的任务对象（关联、交付物、评论、执行记录为空数组）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/tasks?goal=&assignee=&creator=&reviewer=&state=&type=&parent=&limit=` | `assignee/creator/reviewer` 可填 `me`（含我的 Agent）；`state` 可填状态名或状态类型 |
| GET | `/tasks/mine` | 我（含我的 Agent）名下未结束的任务 |
| POST | `/tasks` | `{title, type?, goal_id?, parent_id?, description?, assignee_id?, reviewer_id?, participants?{位置: 执行者 id}, required_capabilities?, human_only?, planned_start?, planned_end?, estimate?, priority?, fields?, ready?}`（`ready` 默认 true：创建后直接就绪）→ 任务 |
| GET | `/tasks/{id}` | 任务详情 |
| PATCH | `/tasks/{id}` | 同上字段的子集（含 `parent_id`：换上级任务，空字符串表示提为顶级；`required_capabilities`；`estimate` 与 `estimate_hours` 等价）。已结束的任务只能改 `description` 与 `fields`（`err.task_closed_edit`）。每个字段的改动各产生一条动态 `TaskFieldChanged`（`data{title, goal_id?, field, from, to, from_title?, to_title?, label?, slot?, key?}`；工作量与迭代沿用 `PointsChanged` / `TaskAddedToSprint` / `TaskRemovedFromSprint`），句子含旧值与新值；`estimate` / `planned_start` / `planned_end` 传 `null` 表示清空。人只能改与自己有关的任务（负责人、创建者、验收人、参与人、所属目标的负责人链）或是组织负责人（`err.task_edit_forbidden`）。Agent 只能改自己负责或自己创建的任务，需要「执行任务」授权；`reviewer_id` / `priority` / `goal_id` / `parent_id` / `sprint_id` / `participants` / `required_capabilities` / `human_only` 对 Agent 一律拒绝（ADR 0003 实现补记） |
| GET | `/tasks/{id}/workflow` | `{task_id, state, active_run{id,executor_id}, transitions[{name,title,to,available,needs_approval?,reasons[],requires[comment|result],label_to}], can_begin, begin_reasons[], can_claim, claim_reasons[], progress}`。`needs_approval` 为真表示这一步可以走，但触发者（Agent）的授权是「需要人确认」，走它会先生成一条待确认操作 |
| GET | `/tasks/{id}/brief` | 任务说明（给执行者的完整背景） |
| POST | `/tasks/{id}/transitions/{name}` | `{comment?, result?}` → 任务 |
| POST | `/tasks/{id}/claim` | → 任务 |
| POST | `/tasks/{id}/begin` | → 任务 |
| POST | `/tasks/{id}/assign` | `{executor_id}` → 任务 |
| POST | `/tasks/{id}/artifacts` | `{type, title, url}` → 交付物 |
| POST | `/tasks/{id}/comments` | `{body, kind?: comment|note}` → 评论 |
| POST | `/tasks/{id}/relations` | `{type, from_task_id, to_task_id}`（一端必须是本任务）→ 关联 |
| POST | `/tasks/{id}/heartbeat` | `{usage[]}` 累计用量（Agent 用） |

## 待领取与甘特图

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/backlog` | `[{task, required_role, required_capabilities[], can_claim, reasons[]}]`，按当前登录者判断能否领取 |
| GET | `/gantt?group=goal|team|executor|type&from=&to=` | `{group, from, to, rows[{key, title, depth, goal{id,planned_start,planned_end,deadline,progress}|null, tasks[{id,title,type,type_title,state,assignee,goal_id,planned_start,planned_end,actual_start,actual_end,progress,cost,overdue}]}], dependencies[{from_task_id,to_task_id}]}`；按目标分组时行为深度优先，`key` 为 `_` 表示未分类 |

## 看板与迭代

看板是任务按流程状态分列的视图，拖动卡片即调用 `/tasks/{id}/transitions/{name}`。迭代是时间盒容器，任务通过 `sprint_id` 归属。任务对象新增字段：`points`（工作量，整数，可空）、`sprint{id,name}|null`。`POST/PATCH /tasks` 接受 `points?`、`sprint_id?`（空字符串表示移出迭代）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/board?type=&goal=&team=&assignee=&sprint=&lane=none|goal|assignee` | `{type: {name,title}|null, columns[{state{name,title,label,claimable?}, wip_limit?, count, over_limit, cards[任务精简对象 + {points, sprint, can_move_to[]状态名}]}], lanes[{key,title}]?}`。指定 `type` 时列 = 该类型流程的状态；不指定时列 = 五种状态类型（`state.name` 为类型名）。`can_move_to` 由内核按当前登录者计算 |
| GET | `/sprints?team=&status=planning|active|closed` | 迭代列表 `[{id, team{id,title}|null, name, goal, starts_on, ends_on, status, task_count, points_total, points_done, created_by, started_at, closed_at}]` |
| POST | `/sprints` | `{name, goal?, team_id?, starts_on, ends_on}` → 迭代（状态 planning） |
| GET | `/sprints/{id}` | 迭代详情：列表字段 + `tasks[任务精简对象]`, `burndown{unit: points|tasks, ideal[{date,value}], actual[{date,value}]}`, `velocity{sprints: n, average}` |
| PATCH | `/sprints/{id}` | `{name?, goal?, starts_on?, ends_on?}`（已结束的不可改） |
| POST | `/sprints/{id}/start` | → 迭代（同一团队已有进行中迭代时拒绝，理由为完整中文句子；需要 `manage_workflows`） |
| POST | `/sprints/{id}/close` | `{unfinished: backlog|next, next_sprint_id?}` → `{sprint, moved: n, returned: n}`（需要 `manage_workflows`） |
| POST | `/sprints/{id}/tasks` | `{task_ids[]}` 把任务加入迭代（已结束的迭代拒绝）→ `{added: n}` |
| DELETE | `/sprints/{id}/tasks/{task_id}` | 移出迭代 |
| GET | `/sprints/velocity?team=` | `{sprints[{id,name,points_done,tasks_done}], average_points}` 最近 5 个已结束迭代 |

流程定义的状态新增可选字段 `wip_limit`（整数）。动态种类新增：`SprintCreated`、`SprintStarted`、`SprintClosed`、`TaskAddedToSprint`、`TaskRemovedFromSprint`、`PointsChanged`，摘要按语言渲染。

MCP 工具新增：`list_sprints`、`get_sprint`（含燃尽）、`add_tasks_to_sprint`、`remove_task_from_sprint`、`get_board`；`start_sprint` / `close_sprint` 对 Agent 走需确认（返回待确认操作）。

## 范围与概览

**范围参数** `scope`：所有列表与统计接口接受 `scope=<team_id>`（该团队及其全部下级团队）或 `scope=all`（全公司，默认）。能切到哪些范围由组织的两项**可见性策略**决定（ADR 0013，见组织设置）：

- `collaboration_visibility`：`org`（默认）/ `boundary`（按共享边界）/ `team_tree`（只看自己团队及下级）
- `finance_visibility`：`team_tree`（默认）/ `org` / `boundary`

**共享边界**：团队对象新增 `is_boundary: bool`，可通过 `PATCH /org/teams/{id}` 设置。可见域 = 自己所在团队向上最近的边界所辖的整棵子树，无边界则全组织，属于多个团队时取并集。跨边界需要权限「查看全部工作」（`view_all_work`）或「查看全部成本」（`view_all_cost`），组织负责人不受限。

请求超出允许范围时返回 403，理由是完整中文句子并说明是哪条策略导致的。

`GET /auth/me` 增加 `scopes[{id,title,depth,financial}]`（当前登录者可切的范围，`financial` 表示这一档能否看财务数据；范围 ID 除团队 ID 外还有 `all` 全公司与 `mine` 自己的可见域）、`default_scope`（默认选中的那一档），以及 `settings{collaboration_visibility, finance_visibility}`（组织的两项策略，前端据此措辞）。概览的 `units[].kind` 取值 `team | self（本级直属成员）| unassigned`。统计周期是滚动窗口（周 = 最近 7 天，月 = 30 天，季 = 90 天）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/stats/overview?scope=&period=week\|month\|quarter` | 概览：`{scope, period, range{from,to}, prev_range{from,to}, units[{id,title,kind: team\|unassigned, goal_progress, tasks_total, tasks_done, overdue, cost, budget, budget_used_pct, throughput, prev{cost,throughput}}], totals{...同上}, trend[{bucket, cost, done, created}]}`。`units` 是当前范围下一级的组织单元，可继续以某个 unit 的 id 作为 `scope` 下钻 |
| GET | `/stats/exceptions?scope=` | 要关注的异常：`{overdue_tasks[{id,title,type,type_title,state,assignee(名字),assignee_id,team,team_id,goal_id,goal_title,priority,planned_end(RFC3339),days_overdue,url}], overdue_goals[], over_budget_goals[], stuck_tasks[{task, days_in_state}], pending_proposals[待确认操作对象]}` |
| GET | `/stats/load?scope=` | 人员与 Agent 负荷：`[{executor{id,kind,name}, team(名字), team_id, open_tasks, active_tasks, points_open, planned_hours_this_week, overdue, capacity_hint, active_runs, max_concurrent, online}]`，后两项成员为 null。`capacity_hint` 是整句中文提示 |

现有 `/stats/cost`、`/stats/cycle`、`/stats/throughput`、`/stats/agents` 全部接受 `scope=`，并按 ADR 0013 的成本归口（执行者所属团队）裁剪。`/stats/cost?group=team` 的分组即按此口径。

## 工作台（ADR 0015）

区块键（系统定义，按目录顺序）：`inbox` 待我处理（只看本人、不受范围影响，默认 12×2、最窄 6 栏）、`readouts` 组织概况（进行中 / 待验收 / 逾期 / 今日成本四个实时读数带 24 小时趋势线，默认 12×1、最窄 6 栏）、`overview_summary` 组织概览摘要、`exceptions` 要我关注的异常、`my_tasks` 我的任务、`my_review` 等我验收、`team_load` 人员与 Agent 负荷、`cost_budget` 成本与预算、`events` 最近动态、`sprint` 当前迭代、`backlog` 待领取任务、`trend` 趋势与环比。预设键：`global` 全局视角、`unit` 部门视角、`team` 小组视角、`doer` 执行视角、`ops` 运营视角。`proposals` 区块已移除（并入「待我处理」）；已存布局里出现它时服务端静默丢弃。`inbox` 与 `readouts` 也是普通区块（ADR 0015 补记四）：可拖动、改大小、移除，移除后从目录找回；每个预设以 `inbox` 开头、其后是 `readouts`（`doer` 不带 `readouts`）。迁移 0010 给已存的个人与角色布局在最上方补上这两块并把其余区块下移（套用 `doer` 预设的角色行只补 `inbox`），已含 `inbox` 的与已清除的空布局不动。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/workspace` | 我的工作台（已解析）：`{blocks[{key,title,x,y,w,h}], source: personal\|roles\|default, roles_used[], can_customize: true}`。12 栏网格：`0 ≤ x`，`x + w ≤ 12`，`w` 为该区块 `min_w`…12 的整数，`h` 为 1…6 的整数（行高单位 120px）。缺 `x/y` 的旧布局按顺序致密排布。解析顺序：个人 → 各角色布局并集（按首次出现顺序去重，宽高取首次出现的那份）→ 默认（组织负责人 `global`，其他 `doer`） |
| PUT | `/workspace/me` | `{blocks[{key,x?,y?,w?,h?}]}`（也接受 `blocks[string]`，缺省尺寸取目录默认、缺省坐标按顺序排布）→ 工作台。校验：键存在且不重复、`0 ≤ x`、`x + w ≤ 12`、`w ≥ min_w`、`1 ≤ h ≤ 6` |
| DELETE | `/workspace/me` | 清除个人微调，恢复角色默认 → 200，返回解析后的工作台 |
| GET | `/workspace/blocks` | 全部区块 `[{key,title,description,default_w,default_h,min_w}]` 与预设 `presets[{key,title,description,blocks[{key,x,y,w,h}]}]`，标题按语言 |
| GET | `/org/workspace` | 各角色的布局 `[{role,role_title,blocks[{key,x,y,w,h}],preset\|null,member_count}]`（需要 `org_settings`） |
| PUT | `/org/workspace/{role}` | `{blocks[{key,x?,y?,w?,h?}]}` 或 `{preset}` 给角色配布局（需要 `org_settings`，产生动态 `WorkspaceLayoutUpdated`） |
| DELETE | `/org/workspace/{role}` | 清除该角色布局 → 200，返回该角色视图。清除会留下一条空布局作为"明确清除"的记录：解析时等同于没有布局，但重启时的默认补齐不会再把内置预设填回来 |

首次种子时内置角色的默认映射：管理员 → 全局视角；运营、流程管理 → 运营视角；产品、设计、开发、测试、发布 → 执行视角。之后每次启动只给**没有任何记录**的内置角色补默认，组织改过或清过的一律不动。个人微调同样产生动态。

## 待我处理（DESIGN.md §12）

只看本人，不受 `scope` 影响。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/inbox` | `{count, groups[{kind: overdue|proposals|review|questions|unstarted|notifications, title, count, items[]}], empty?}`，组按紧急度排序，空组省略；`title` 是当前语言的组名，全空时 `empty` 是那句「没有等你处理的事。」。`items` 形状按组：任务类组是任务精简对象（含 `days_overdue` / `state`）；`proposals` 是待确认操作对象（`can_decide` 为真的）；`questions` 是等我答复的评论 `{task, comment}`；`notifications` 是站内通知对象。Agent 调用返回 403（Agent 没有待我处理） |
| GET | `/inbox/count` | `{count, by_kind{...}}`，供侧栏角标与状态栏读数（与 `/proposals/count` 合并；后者保留兼容） |
| POST | `/notifications/read` | `{ids[]}` 标为已读（只能标自己的，别人的 ID 被忽略）→ `{count}` 是剩余未读数。标已读不产生动态 |

## 待确认操作

Agent 发起、但其授权模式是「需要人确认」的动作不会立即生效，而是记为一条待确认操作，等人点确认后才执行（ADR 0003）。对象：`{id, agent{id,name}, owner{id,name}, action, action_title, grant, target{kind: task|goal|sprint|task_type|agent, id, title}|null, summary, payload, status: pending|approved|rejected|expired, status_title, can_decide, decided_by, decided_at, reason, created_at, expires_at}`（`grant` 是用到的授权名，`can_decide` 表示当前登录者能否确认这一条）。`summary` 是完整中文句子，说明"确认后会发生什么"。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/proposals?status=pending|approved|rejected|expired&agent=&mine=1&limit=` | `mine=1` 只看等我确认的（我是发起 Agent 的所有者，或我持有该动作所需权限）。默认按创建时间倒序 |
| GET | `/proposals/{id}` | 详情 |
| POST | `/proposals/{id}/approve` | 确认并立即执行；执行失败时保持 pending 并返回失败理由（完整中文句子）→ `{proposal, result}` |
| POST | `/proposals/{id}/reject` | `{reason}` 必填，至少 6 个字的完整句子（Agent 直接读它）→ 待确认操作 |
| GET | `/proposals/count` | `{pending: n}`，保留兼容：成员取 `/inbox/count` 里 `by_kind.proposals`（等我确认的条数），Agent 仍是全组织待确认条数 |

Agent 侧：任何写操作若命中「需要人确认」的授权，HTTP 返回 `202` 与 `{proposal, message}`（`message` 是完整中文句子，如「已提交待确认操作，等小王确认后才会执行。」）；MCP 工具同样返回这句话与待确认操作 ID，并新增 `list_my_proposals` 查看自己提交的待确认操作。

动态种类新增：`ProposalCreated`、`ProposalApproved`、`ProposalRejected`、`ProposalExpired`（七天没人确认自动作废，由后台巡检产生）。确认后执行产生的动态照常记在 Agent 名下，并在摘要里带上「经<确认人>确认」。

会走待确认操作的动作（`action` 取值）：`task.transition`、`task.claim`、`task.begin`、`task.assign`、`task.comment`、`task.note`、`task.artifact`、`task.link`、`task.create`、`task.create_subtask`、`goal.create`、`milestone.create`、`milestone.update`、`milestone.delete`、`milestone.reach`、`milestone.unreach`（里程碑动作的 `target` 是所属目标，`payload.milestone_id` 指向那条里程碑）、`task_type.save`、`sprint.start`、`sprint.close`。每条另有 `action_title`（当前语言的动作名）、`status_title`、`can_decide`（当前登录者能否确认）。`task_type.save`、`sprint.start`、`sprint.close` 需要「管理流程」权限才能确认，其余只有 Agent 的所有者与组织负责人能确认。

## Agent 与成员

**可见范围**：`GET /agents` 对组织负责人与持 `org_settings` 权限的成员返回全部 Agent；其他成员只返回自己所有的 Agent 加公共 Agent（公共 Agent 对非所有者只读，`can_manage: false`）。对不可管理的 Agent 执行修改 / 吊销 / 换令牌返回 403，理由是完整中文句子。Agent 对象新增 `can_manage: bool`、`shared: bool`。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/agents` | `[{id,name,owner: ExecutorRef,shared,capabilities[],grants[{name,mode}],online,last_seen_at,max_concurrency,runtime,created_at}]` |
| POST | `/agents` | `{name, runtime?, capabilities[], grants[{name, mode: direct|with_approval}], shared?, max_concurrency?}` → `{agent, token}`（令牌只返回一次） |
| PATCH | `/agents/{id}` | 同上字段子集 → Agent |
| DELETE | `/agents/{id}` | 204。吊销：令牌失效、进行中的执行记录取消、名下任务回待领取 |
| GET | `/members` | `[{id,name,email,roles[],team_id,created_at}]` |
| GET | `/capabilities` | `{代码名: 中文}` |

## 任务类型

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/task-types` | `[{name,title,builtin,participants{位置:{title,role}},participant_order[],task_schema,result_schema,agent_instructions,workflow{version,initial,states{名:{name,title,label,weight?,claimable?}},state_order[],transitions[{name,title,from[],to,by[],requires[],grant?,assign_to}]}}]` |
| GET | `/task-types/{name}` | 单个 |
| PUT | `/task-types/{name}` | 保存新版本；请求体为领域格式（见 `docs/spec/workflow-definition.md`）；组织负责人或 admin / workflow_admin 角色 |

## 统计与记录

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/stats/cost?group=goal|team|executor|model` | `[{key,title,cost,total_tokens,run_count}]` |
| GET | `/stats/cycle` | `[{type,type_title,sample,avg_hours,median_hours,avg_active_hours}]` |
| GET | `/stats/throughput` | 最近 12 周 `[{week(周一),created,done,terminated}]` |
| GET | `/stats/agents` | `[{agent,owner,runs,accepted,rejected,success_rate,reject_rate,total_tokens,cost}]` |
| GET | `/events?task=&limit=` | `[{id,kind,task_id,task_title,goal_id,actor,summary,data,created_at}]`，`summary` 是服务端拼好的中文句子 |
| GET | `/notifications?read=1` | 站内通知；`read=1` 同时标为已读 |

---

# 二期新增：多语言、组织设置、平台后台

## 多语言

- 语言代码：`zh-CN`、`en-US`。成员有 `locale`，组织有 `default_locale`；解析顺序：成员 → 组织默认 → `zh-CN`。Agent 用所有者的语言。
- 未登录接口按 `Accept-Language` 选语言。已登录接口忽略请求头，按成员设置。
- 所有接口里的 `title`、`label_title`、状态名、步骤名、参与角色名、授权名、角色名、交付物类型名、能力标签名、拒绝理由、动态 `summary`、通知文案都已按请求者语言输出。前端不需要再翻译这些数据。
- `GET /auth/me` 的 `member` 增加 `locale`，`organization` 增加 `slug`、`default_locale`；会话另有 `capability_titles{代码名: 当前语言名}`、`permissions[]`、`is_owner`；`PATCH /auth/me` `{locale}` → 会话。
- 内置任务类型的定义里，标题字段是 `{"zh-CN": "...", "en-US": "..."}` 形式；接口输出时已解析成字符串。`PUT /task-types/{name}` 接受字符串（视为当前语言）或多语言对象。

## 组织设置（`/api/v1/org/...`）

组织设置对象包含两项**可见性策略**（ADR 0013）：`collaboration_visibility` 与 `finance_visibility`，取值均为 `"org" | "boundary" | "team_tree"`，默认分别是 `org` 与 `team_tree`。团队对象上的 `is_boundary` 配合 `boundary` 取值使用。另有 `GET /org/visibility-preview?member=` 返回某个成员实际能看到的团队列表，供设置页预览。通过 `GET/PATCH /org/settings` 读写，需要 `org_settings` 权限，改动产生动态。

需要组织负责人，或持有 `org_settings` 权限的角色。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/org` | `{id,slug,name,currency,default_locale,owner: ExecutorRef}` |
| PATCH | `/org` | `{name?, default_locale?, currency?}` |
| GET | `/org/members` | `[{id,name,email,roles[],team_id,team_ids[],active,is_owner,locale,created_at,source,source_title,status,status_title,invitation?,possible_duplicate_of?[{id,name,reason,reason_text}]}]`。`possible_duplicate_of` 是「可能与 X 重复」提示（ADR 0017 补记四）：用同步同一套认法在手工成员与同步成员之间找到的疑似重复，两边都带；一次列表调用算一遍，不另开接口。`team_ids` 是全部所属团队；`team_id` 是主团队（多团队时取树上最深的那个，与成本归口一致），兼容旧读法。`status` 取值 `active`（正常）/ `pending_activation`（待激活）/ `inactive`（已停用）；待激活成员带 `invitation{id,email,expires_at,...}`（链接只在创建邀请时返回一次，要链接就再 `POST /org/invitations` 一次） |
| PATCH | `/org/members/{id}` | `{name?, roles?, active?, team_id?}` → 成员。规则：来自IM 集成的成员不能改名（「来自飞书的成员姓名由同步决定。」），也不能手工挪出 / 挪进同步来的团队（「来自飞书的成员的团队由同步决定。」）；组织负责人不能被停用（「组织负责人不能被停用。」）；不能停用自己（「不能停用自己。」）。每处改动各一条动态：`MemberRenamed`、`MemberRolesChanged`、`MemberTeamChanged`（`data.mode: move`）/ `MemberTeamCleared`、`MemberDeactivated`、`MemberReactivated` |
| POST | `/org/members/bulk` | `{member_ids[], action: "move_team"\|"add_team"\|"set_roles"\|"deactivate"\|"reactivate", team_id?, roles?[]}` → `{updated, skipped[{id,name,reason}]}`。每个成员单独判定：不满足规则的跳过并给一句完整的中文理由，其余照做。`move_team` 把成员的全部团队归属替换成这一个，`add_team` 追加一个（已在里面 →「这个成员已经在团队「X」里。」）；目标团队不存在 / 已停用整个请求 400。规则同 PATCH：来自IM 集成的成员不能手工挪出 / 挪进同步团队（手工成员不受限，来自 IM 的成员可以被加进手工团队）；负责人与自己不能停用；已停用的再停用 →「这个成员已经停用。」，未停用的恢复 →「这个成员没有停用。」。一人一条动态：`MemberTeamChanged`（`data.mode: move\|add`）、`MemberRolesChanged`、`MemberDeactivated`（连带吊销其 Agent、任务回待领取）、`MemberReactivated` |
| GET | `/org/members/export.csv` | 导出全部成员（含停用）为 CSV：`text/csv; charset=utf-8`，UTF-8 带 BOM，表头 `姓名,邮箱,团队,角色,状态,来源`；团队是主团队的完整路径「产品事业部 / 研发组」，多个角色用「、」连接（角色、状态、来源按请求者语言输出显示名） |
| POST | `/org/members/import/preview` | 请求体三选一：`multipart/form-data` 里的文件（任意字段名）、`application/json` 的 `{csv}`、或整个请求体就是 CSV 文本（上限 8 MB）。第一行必须是表头，认「姓名 / name」「邮箱 / email」「团队 / team」「角色 / roles」（其余列忽略，只有邮箱必填）→ `{rows[{line,name,email,team_path,team_id\|null,roles[],action: create\|update\|confirm\|skip\|invalid,reason?,candidates?[{local_id,name,team_path,source,source_title,reason,reason_text}]}], summary{create,update,confirm,skip,invalid}}`。以邮箱认人：新邮箱 → `create`；已有的手工成员 → `update`；新邮箱、但姓名与某个手工成员相同且行里的团队与他的主团队同名（同步同一套认法的第四级，ADR 0017 补记四）→ `confirm` 带 `candidates`，要人决定；请求体可带 `decisions[{line, decision: merge\|create\|skip, local_id?}]`（JSON 形式与 `csv` 并列，multipart 形式放在 `decisions` 字段）：`merge` 且 `local_id` 在候选里 → `update`（不在候选里 →「第 N 行决定合并的成员不在候选里。」）、`create` → `create`、`skip` → `skip`（「按你的决定跳过。」）；邮箱格式不对、文件里重复、找不到团队路径（「找不到团队「X」。」，单段名字唯一时也认）、团队已停用、角色不存在（角色可写代码名或任一语言的显示名）、来自IM 集成的成员（「来自飞书的成员由同步维护，导入不会改动。」）→ `invalid` 带理由。团队列没填表示不改团队；没有角色列表示不改角色。不落库 |
| POST | `/org/members/import` | 同上请求体（含 `decisions`），执行 → `{created, updated, unchanged, skipped[{line,email,reason}], invitations[{email,url}]}`。没决定的 `confirm` 行与 `skip` 行进 `skipped`（「还没确认是不是同一个人，这次没有导入。」/「按你的决定跳过。」）。`create` 行：建一个待激活的手工成员（带角色与团队）并生成邀请链接（动态 `MemberImported` + `MemberInvited`）；邮箱已有可登录账号（别的组织的人）时只发邀请、不预建成员。`update` 行：改姓名 / 角色 / 团队，每处一条动态（`MemberRenamed` / `MemberRolesChanged` / `MemberTeamChanged`），什么都没变的计入 `unchanged`。`invalid` 行原样进 `skipped`。所有改动在一个事务里 |
| POST | `/org/members/{id}/make-owner` | 把组织负责人转给他 → 成员 |
| POST | `/org/members/{id}/merge` | `{into}` 把成员 `{id}` 并入 `into` → 合并后的目标成员（ADR 0017 补记四）。目标保留自己的角色、Agent、一切；旧成员身上"现在归谁"的引用改到目标：任务负责人 / 验收人 / 参与角色、目标负责人、Agent 所有者、团队负责人、团队归属、未读通知；历史（创建者、评论、交付物、动态、执行记录）不改；外部身份与决定搬到目标；旧成员来自 IM 而目标是手工的 → 目标接上 IM 身份并按 IM 改名。旧成员停用（不吊销 Agent，它们已归目标）。拒绝：并入自己（「不能把一个对象并入它自己。」）、旧成员是组织负责人（「组织负责人不能被并入别人。」）、目标已停用（「成员「X」已停用，先恢复再合并。」）。动态 `MemberMerged`「把成员「A」并入了「B」」 |
| GET | `/org/invitations` | `[{id,email,name,roles[],team_id,member_id,expires_at,accepted_at,created_at}]`（链接只在创建时返回）。`team_id` 是邀请时指定的团队；`member_id` 是这条邀请对应的待激活成员（手工邀请 / 导入 / 同步预建的），邮箱属于别的组织的可登录账号时为 `null` |
| POST | `/org/invitations` | `{email, name?, roles[], team_id?}` → 邀请（含 `url`，一期不发邮件，把链接发给对方）。与 CSV 导入同一条路（ADR 0017）：新邮箱同时预建一个待激活的手工成员（带角色与团队，立刻出现在 `GET /org/members` 里，`status=pending_activation`），邀请的 `member_id` 指向它，动态 `MemberInvited`。邮箱已是正常成员 → 400 `err.already_member`；邮箱是待激活成员 → 重发链接，姓名 / 角色缺省沿用成员的，团队不改；邮箱已有可登录账号（别的组织的人）→ 只发邀请、不预建成员（`member_id` 为 `null`），接受时用自己的密码验证。`team_id` 必须存在且未停用（`err.team_missing` / `err.team_inactive`） |
| DELETE | `/org/invitations/{id}` | 204，作废邀请（动态 `InvitationRevoked`）。邀请预建的手工待激活成员若从未激活、且没有别的有效邀请了，一并删掉（从成员列表消失；无密码的占位账号保留，不能登录）。同步来的待激活成员不动，由同步管；已接受的邀请只删记录 |
| GET | `/org/roles` | `[{name,title,titles{语言:名称},builtin,permissions[]}]`，权限取值：`org_settings`（组织设置）、`manage_workflows`（管理流程）、`cancel_any_task`（取消任何任务）。所有登录成员可读 |
| PUT | `/org/roles/{name}` | `{title: string 或 {"zh-CN","en-US"}, permissions[]}` → 角色（内置角色只能改 title） |
| DELETE | `/org/roles/{name}` | 204；内置角色和仍有人持有的角色不能删 |
| GET | `/org/teams` | `[{id,name,parent_id,lead_id,member_ids[],is_boundary,source,source_title,external_name,active,member_count,subtree_member_count}]`。`member_count` 是直属的正常成员数，`subtree_member_count` 是它和全部下级团队里不重复的正常成员数（已停用的成员都不算）；`active: false` 表示已停用（只停用不删除，ADR 0017） |
| POST | `/org/teams` | `{name, parent_id?, lead_id?, member_ids?, is_boundary?}` → 团队（`parent_id` 必须存在） |
| PATCH | `/org/teams/{id}` | `{name?, parent_id?, lead_id?, member_ids?, is_boundary?, active?}` → 团队。`parent_id` 换上级（空字符串提为顶层）：不能挪到自己或自己的下级下面（「不能把团队挪到它自己或它的下级下面。」，动态 `TeamMoved`）；`name`：来自IM 集成的团队不能改名（「来自飞书的团队名称由同步决定。」）；`is_boundary`：共享边界（ADR 0013，动态 `TeamBoundaryChanged`）；`active: false` 停用：还有正常成员或未停用的下级团队时拒绝（「先把成员和下级团队挪走，再停用这个团队。」，动态 `TeamDeactivated`，`data.manual: true`）；`active: true` 恢复（动态 `TeamReactivated`）。其余改动记 `TeamUpdated` |
| DELETE | `/org/teams/{id}` | 204。只有没有成员、没有下级团队的手工团队能删：来自IM 集成的 →「来自飞书的团队不能删除，只能停用。」；不空 →「先把成员和下级团队挪走，再删除这个团队。」 |
| POST | `/org/teams/{id}/merge` | `{into}` 把团队 `{id}` 并入 `into` → 合并后的目标团队（ADR 0017 补记四）。旧团队的直属成员、目标（任务经目标归口）、迭代、下级团队整体挂到目标下；旧团队是共享边界则目标也成为边界；没负责人的目标接过旧团队负责人；外部身份与决定搬到目标；旧团队来自 IM 而目标是手工的 → 目标接上 IM 身份，名称按 IM。目标在旧团队子树里时先提到旧团队的位置，不成环。旧团队停用不删除、改为手工来源。拒绝：并入自己、目标已停用（「团队「X」已停用，先恢复再合并。」）、旧团队已停用（「团队「X」已停用，没有可以并入的内容。」）。动态 `TeamMerged`「把团队「A」并入了「B」」 |
| GET | `/org/capabilities` | `[{name,title,titles{语言:名称}}]` |
| PUT | `/org/capabilities/{name}` | `{title: string 或多语言对象}` → 能力标签 |
| DELETE | `/org/capabilities/{name}` | 204 |
| GET | `/org/pricing` | `{currency, models:[{model_id,input_per_million,output_per_million,cache_read_per_million,cache_write_per_million,currency,source: global|override}], exchange_rates:[{from,to,rate}]}` |
| PUT | `/org/pricing/models/{model_id}` | 组织覆盖 `{input_per_million,output_per_million,cache_read_per_million,cache_write_per_million,currency}` |
| DELETE | `/org/pricing/models/{model_id}` | 删除覆盖，回到全局 |
| PUT | `/org/pricing/rates` | `{from,to,rate}` |

## IM 集成同步（ADR 0017，`/api/v1/org/directory/...`，需要 `org_settings`）

提供方（飞书、企业微信…）是插件：每个平台在 `internal/directory` 里一个文件，声明自己的凭据字段与前置条件（ADR 0017 补记）。接口层、前端都按声明渲染，不出现平台名的分支；新增平台时这一节的形状不变。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/org/directory/providers` | 可接入的提供方 `[{key, title, root_department_id, fields[{key, title, secret, placeholder, hint, set: false}], prerequisites[], tip{text, url?}}]`（顺序固定：feishu、wecom） |
| GET | `/org/directory` | `{provider: "feishu"\|"wecom"\|null, provider_title, configured: bool, credentials{key: value}（只有非保密字段）, secrets_set{key: bool}（保密字段是否已设置）, root_department_id, root_department_ids[]（同步根部门列表，空表示整个企业；`root_department_id` 是列表第一个，兼容旧读法）, default_role, schedule: manual\|hourly\|daily, schedule_title, schedules[{value,title}], proxy_url, providers[]（同上，当前提供方的 `fields[].set` 与非保密字段的 `fields[].value` 会填上）, last_run{id, provider, started_at, finished_at, status: ok\|partial\|failed, status_title, added_teams, updated_teams, deactivated_teams, added_members, updated_members, deactivated_members, errors[]}\|null}`。永远不返回保密字段的值 |
| PUT | `/org/directory` | `{provider?, credentials?{key: value}, root_department_id?, root_department_ids?: []string, default_role?, schedule?, proxy_url?}`。`provider` 首次保存必填，之后省略表示沿用；换提供方时凭据与根部门重置为新提供方的。`credentials` 按提供方声明逐字段校验：非保密字段省略表示沿用现有值（首次必填，否则「请填写<字段名>。」）；保密字段省略或为空表示不改（首次保存必填，否则「第一次保存需要填写<字段名>。」）。**同步根**：`root_department_ids` 是列表（ADR 0017 补记二）：空列表 = 整个企业；一个 = 只同步那个部门（它自己也成为一个团队）；多个 = 每个各自作为顶层团队。只给 `root_department_id` 时视为长度为一的列表；提供方的根（飞书 `"0"`、企业微信 `"1"`）等于整个企业，不进列表。去空白、去重。产生动态 `DirectoryConfigured`（数据里只有非保密字段与改了哪些保密字段的键名） |
| GET | `/org/directory/checklist` | **接入检查**（ADR 0017 补记二）：现场让提供方跑一遍它声明的检查项并汇总成向导状态，不缓存 → `{provider, provider_title, console_url?（这个应用在提供方控制台的首页）, checks[{key, title, status: ok\|todo\|blocked\|skipped, detail, fix, fix_url, blocking}], ready: bool（没有 blocked）, next{step: credentials\|checks\|scope\|preview\|sync\|schedule, text}, suggested_roots[{id,name}]（权限范围只含部分部门时，可选作同步根的部门）, root_department_id, root_department_ids[]}`。`title` / `fix` 是一句"去哪儿点什么"的中文，权限的英文代号只出现在 `detail`；`fix_url` 尽量直达这个应用在控制台里的那一页。未配置时 `checks` 为空、`next.step = credentials`。`next` 的规则：有 blocked → `checks`（`text` 是第一项阻塞检查的修法）；`scope` 为 todo 且还没选同步根 → `scope`；没有成功过的同步 → `preview`；频率仍是手动 → `schedule`；否则 `sync`（完成态）。连不上提供方时合成一项 `connection` 的阻塞检查 |
| POST | `/org/directory/test` | 用当前凭据跑同一份接入检查并摘要 → `{ok, tenant_name?, department_name?, error?, warnings[]?, suggested_roots[]?}`：有阻塞项时 `ok: false`，`error` 是那一项的说明与修法（如「测试目录拒绝了这组凭据：invalid app_secret (code 10003)。重新填写凭据。」）；待处理项进 `warnings`；权限范围只含部分部门时另带 `suggested_roots`（不算失败，可以在 PUT 里选成 `root_department_ids`）。与 checklist 用的是同一份检查，两边永远一致 |
| POST | `/org/directory/sync` | 立即同步。接入检查有阻塞项时不同步：记一次 `failed` 运行，`errors[0]` 是「接入还没完成：<那项检查的说明与修法>」（定时同步同样被挡，原因在运行记录里）。预览里还有未决定的候选时不同步：400 `err.directory_confirm_first`「还有 N 条要先确认，再同步。」（定时同步记一次 `failed` 运行，同一句话）。成功时 → `last_run` 形状加 `invitations[{member_id,name,url}]`（本次新建成员的邀请链接，只在这一次响应里出现）；同时产生动态 `DirectorySyncRan`（摘要含新增 / 更新 / 停用数量） |
| GET | `/org/directory/preview` | 不落库地拉取部门树与人员数并与现有团队 / 成员对照。接入检查有阻塞项时返回 400「接入还没完成：<那项检查的说明与修法>」（`err.directory_blocked`）：`{teams[{external_id,name,parent_external_id,local_id\|null,action: create\|update\|keep\|confirm, bind?, merge_from?, candidates?[{local_id,reason,via?}]}], teams_to_deactivate[], members_total, members_new, members_existing, members_to_deactivate[], notes[], confirmations[], decided[], skipped[{kind,external_id,external_name,decided_at}], blocked_by_confirmations}`（`notes` 是无法自动处理的项，如没有邮箱的人；换过提供方时，来自旧来源、尚未被新来源认出的团队与成员列成「来自旧来源（飞书），将改为手工维护」，不会停用）。**冲突**（ADR 0017 补记四）：认人认团队分五级——成员：外部身份 → 邮箱相同 → 手机号相同 → 姓名相同且所在部门与本地主团队同名 → 同名且两边唯一（IM 里和系统里都只有这一个叫这个名字的人）；团队：外部身份 → 同名且同层级（父团队也对上，或都是顶层）。第一级直接算同一个；其余各级只产生候选，进 `confirmations[{kind: member\|team, external{id,name,dept?,parent?,email_masked?,mobile_tail?}, candidates[{local_id,name,team_path,source,source_title,reason: email\|mobile\|name_team\|name_unique\|team_name_level, reason_text}]}]`，`blocked_by_confirmations` 是它的条数；已决定的（合并 / 新建）进 `decided[]`（同形状，多 `decision, decision_title, decided_local_id, decided_local_name`）；决定跳过的进 `skipped[]`，不进 `teams` / 成员数。已被外部身份或合并决定占用的本地对象不再当别人的候选；已停用的团队不当候选，已停用的成员可以（合并即恢复）。本系统暂不存成员手机号，第三级对 IM 同步实际不触发（`mobile_tail` 仍给出）；跳过的部门的子部门提到它的父下面 |
| GET | `/org/directory/runs?limit=` | 历次同步记录 |
| PUT | `/org/directory/decisions` | 批量写决定（ADR 0017 补记四）`{items[{kind: member\|team, external_id, external_name?, decision: merge\|create\|skip, local_id?}]}` → `[{kind,external_id,external_name,decision,decision_title,local_id?,local_name?,decided_at}]`。同一外部对象只保留最后一次；`merge` 必须给 `local_id`（「合并到已有时要指明并入哪个本地对象。」），团队目标不能已停用。之后的预览按决定走：`merge` → 视为已绑定，同步时补外部身份，姓名 / 名称、团队归属、在职状态按 IM，角色、Agent、任务、目标由本系统管；若以前的同步已按同一外部编号建过重复对象，同步时先把它整体并入（团队：成员、目标、迭代、共享边界、下级团队；成员：任务 / 目标 / Agent 的归属），再绑定；`create` → 新建不再问；`skip` → 不同步，列在 `skipped`。每条一条动态 `DirectoryDecided` |
| DELETE | `/org/directory/decisions/{kind}/{external_id}` | 204。撤回一条决定（重新考虑），下次预览会再问；没有 → 404「没有这条决定。」。动态 `DirectoryDecided`（`decision: reconsider`） |
| GET | `/org/directory/mappings` | **对应关系**面板 → `{provider?, provider_title?, bound[{kind, external_id, external_name?（团队给 IM 里的名字）, local_id, local_name, local_active, since}], duplicates[{kind, a{id,name,source,source_title,team_path}, b{...}, reason, reason_text}], skipped[{kind, external_id, external_name, decided_at}]}`。`duplicates` 是同步之后用同一套认法（第二至四级）在手工对象（`a`）与同步对象（`b`）之间找到的疑似重复：成员按邮箱 / 手机号 / 姓名相同且主团队同名，团队按同名同层级（父团队相同，或父团队本身也是一对同名同层级的重复）；停用的不算。没配置提供方时只有 `duplicates` |
| DELETE | `/org/directory/bindings/{kind}/{external_id}` | 204。解绑：删这条外部身份及它的决定，本地对象改为手工维护（`source: manual`，团队清空 `external_name`）；下次预览它会重新成为候选。没有 → 404「没有这条对应关系。」。动态 `DirectoryUnbound` |

成员对象新增 `source: manual\|<提供方 key>`、`source_title`（「手工」或提供方名字）、`status: active\|pending_activation\|inactive`、`status_title`，待激活成员另带 `invitation{url,expires_at}`；团队对象新增 `source`、`source_title`、`external_name`、`active`。没有邮箱的外部人员会得到一个占位邮箱（`<外部编号>@<提供方>.invalid`）并被列进 `notes`，需要手工邀请。组织负责人永不会被同步停用。

**凭据存储**：`org_directory.credentials`（jsonb，非保密字段明文）+ `org_directory.secrets_enc`（全部保密字段合成一个 JSON 后用 `AXIOMOS_SECRET_KEY`（32 字节 base64）做 AES-GCM 加密；未设置时用开发密钥并在启动日志告警）。0009 期的 `app_id` / `app_secret_enc` 两列暂留，第一次读到旧格式的行时由服务端就地转成新格式（服务端密钥换过时只搬 App ID，保密字段要重填）。

**换提供方**：一个组织一个提供方。切换后旧外部身份保留但不再参与匹配；同步时把仍标着旧来源、且没被新来源认出的团队与成员改为手工维护（`source: manual`，产生动态 `DirectoryDetached`），不停用。

**各提供方**（凭据字段以 `GET /org/directory/providers` 为准）：

- 飞书 `feishu`：`app_id`（App ID）、`app_secret`（App Secret，保密）；根部门 `"0"`。接口 `POST /open-apis/auth/v3/tenant_access_token/internal`，`GET /open-apis/contact/v3/departments/{id}/children?fetch_child=true`（分页），`GET /open-apis/contact/v3/users/find_by_department?department_id=`（分页；需要通讯录只读权限），可选 `GET /open-apis/tenant/v2/tenant/query` 读企业名，`GET /open-apis/contact/v3/scopes` 读权限范围。**检查项**（按序）：`credentials` 凭据可用（令牌被拒 → 阻塞，链接 `…/app/{app_id}/baseinfo`）；`scope` 通讯录权限范围（读根部门的子部门：通 → 全部成员；40004 → 从 scopes 列出被授权部门，有 → todo 不阻塞并给 `suggested_roots`，已把其中一个选成同步根则通过；一个都没有 → 阻塞）；`dept_names` 部门名称权限（读一页部门全部没有 `name` → 阻塞，`detail` 里给 `contact:department.base:readonly`，链接 `…/app/{app_id}/auth`）；`user_names` 人员姓名权限（同法，`contact:user.base:readonly`）；`user_emails` 邮箱权限（全没邮箱 → todo 不阻塞，说明要靠邀请链接）；`published` 版本已发布（令牌能取但所有读接口都以 99991663 / 99991672 一类"没权限"失败 → 阻塞，链接 `…/app/{app_id}/version`）。飞书按权限裁字段而不报错（code 0），所以名称类检查只能真的读一页探测。
- 企业微信 `wecom`：`corp_id`（企业 ID）、`corp_secret`（通讯录同步 Secret，保密）；根部门 `"1"`。接口 `GET /cgi-bin/gettoken?corpid=&corpsecret=`（令牌缓存到 `expires_in` 前 60 秒），`GET /cgi-bin/department/list?id=`（一次返回该部门及全部后代，不分页；根部门的父是 0），`GET /cgi-bin/user/list?department_id=&fetch_child=0`（不分页）。人员编号用 `userid`；`status` 1 已激活、4 未激活算在职，2 已禁用、5 退出企业算离开；邮箱取 `biz_mail`，没有再取 `email`；新建应用拿不到姓名时用 `userid` 顶着。没有读企业名的接口，连通性测试只返回根部门名（即企业名）。令牌失效（40014 / 42001）自动重取一次；其余 `errcode` 一律作为「提供方拒绝」把 `errmsg` 原话带回。需要把服务端出网 IP 加进通讯录同步的可信 IP。**检查项**（按序）：`credentials` 凭据可用（40013 / 40001 一类 → 阻塞，链接管理后台「我的企业」）；`trusted_ip` 服务器 IP 在可信 IP 里（`department/list` 或 `gettoken` 报 60020 → 阻塞，从 errmsg 里抠出 `from ip: x.x.x.x` 放进 `detail` 与 `fix`，链接「管理工具 → 通讯录同步」）；`structure` 能读到部门树；`names` 姓名等敏感字段权限（`user/list` 一份人员全部没有 `name` → 阻塞）。

## 邀请（公开）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/invitations/{token}` | `{organization_name, email, name, expired, accepted}` |
| POST | `/invitations/{token}/accept` | `{name, password}`；账号已存在时 `password` 用于验证。成功后设置登录 Cookie → 会话 |

前端页面 `/invite/<token>/`（静态占位 `out/invite/_/index.html`，从地址栏读 token）。

## 平台后台（`/api/v1/admin/...`）

平台管理员是独立账号类型（`accounts.platform_admin = true`），用独立 Cookie `axiomos_admin`。首个平台管理员由环境变量 `PLATFORM_ADMIN_EMAIL` / `PLATFORM_ADMIN_PASSWORD` 在启动时创建。前端页面在 `/admin/...`，有自己的登录页。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/admin/login` | `{email,password}` → `{admin:{id,email,name}}` |
| POST | `/admin/logout` | 204 |
| GET | `/admin/me` | `{admin:{id,email,name}}` |
| GET | `/admin/organizations` | `[{id,slug,name,currency,default_locale,owner:{id,name,email}|null,member_count,agent_count,task_count,cost_30d,deactivated_at,created_at}]` |
| POST | `/admin/organizations` | `{slug,name,currency?,default_locale?,owner_email,owner_name}` → 组织，另含 `owner_invite_url`（新账号需要通过邀请链接设密码） |
| GET | `/admin/organizations/{id}` | 单个（同上字段） |
| PATCH | `/admin/organizations/{id}` | `{name?, deactivated?: bool}` |
| GET | `/admin/pricing` | 全局价格表 `[{model_id,...,currency}]` |
| PUT | `/admin/pricing/{model_id}` | 新增或修改 |
| DELETE | `/admin/pricing/{model_id}` | 204 |
| GET | `/admin/stats` | `{organizations, members, agents, runs_30d, cost_30d:[{org_id,name,cost,currency}]}` |
| GET | `/admin/admins` | `[{id,email,name}]` |
| POST | `/admin/admins` | `{email,name,password}` |
