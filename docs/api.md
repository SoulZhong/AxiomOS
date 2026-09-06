# HTTP 接口参考（/api/v1）

给网页用的接口。返回形状与 `web/src/lib/api.ts` 里的类型一一对应；Agent 请用 MCP（`docs/agent-integration.md`）。

人类用户通过登录后的 Cookie（`axiomos_session`）或 `Authorization: Bearer <会话令牌>` 访问；Agent 令牌（`axm_...`）也能调这些接口。所有日期字段是 `YYYY-MM-DD`，时间字段是 RFC3339。金额已折算成组织结算货币。

**出网限制（默认只许公网）**：所有由组织自己填、服务器会去访问的地址——通知 webhook 的接收地址、代码平台的接口地址（`api_base`）、出网代理（`proxy_url`）——都要过同一道守卫：只认 http(s)（没打开内网出网时只许 https）、不许带用户名密码、不许指向回环 / 私网 / 链路本地 / 唯一本地 / 组播地址，云元数据地址（169.254.169.254、metadata.google.internal）任何时候都不许。保存时按域名查一遍，真正连接前按解析出的地址再查一遍（DNS 改绑绕不过去），重定向到不允许的地址也会被挡下。私有化部署要指向内网 GitLab 或内网接收端时，给服务端设置环境变量 `AXIOMOS_ALLOW_PRIVATE_EGRESS=1`；不设时保存内网地址会被拒并给出这句话：「这个地址指向内网，默认不允许。私有化部署可以设置环境变量 AXIOMOS_ALLOW_PRIVATE_EGRESS=1 后再填。」

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

任务对象：`{id, number, goal_id, goal{id,title}, parent_id, type, type_title, type_version, title, description, state{name,title,label,weight?,claimable?}, previous_state, creator, assignee, reviewer (ExecutorRef), participants{位置: {title, role, executor}}, required_role, pending_participant, required_capabilities[], human_only, relations[], artifacts[], comments[], runs[], subtasks[], links[], pr?, planned_start, planned_end, actual_start, actual_end, estimate, priority: urgent|high|normal|low, progress, overdue, total_tokens, cost, fields, created_at, updated_at}`。

- 关联：`{id, type: blocks|found_in|related, from{id,title,type,state}, to{...}}`。`blocks` 时 from 是前置；`found_in` 时 from 是 Bug。
- 交付物：`{id, type, title, url, attached_by, created_at}`。
- 评论：`{id, kind: comment|note, author, body, created_at}`。
- 执行记录：`{id, task_id, state, state_title, executor, started_at, ended_at, outcome: running|ended|cancelled, usage[{model,input_tokens,output_tokens,total_tokens,duration_ms,tool_calls}], total_tokens, cost}`。
- 外部链接（ADR 0020）：`{id, kind: pr|issue|doc|design|other, kind_title, provider, url, title, status?: open|draft|merged|closed|passed|failed, status_title?, actor_name?, external_id?, created_at, updated_at}`。详情与任务说明给完整的 `links[]`；列表行只给 `pr`（最近更新的那条 PR 链接）：`{status, status_title, url, title, count}`。

列表接口返回精简的任务对象（关联、交付物、评论、执行记录、外部链接为空数组，但带 `pr` 小标）。

**任务序号**：`number` 是组织内从 1 递增的整数，界面上写成 `#123`；任务对象、列表、看板卡片、甘特图、子任务与关联里的任务引用（`TaskRefV`）、任务说明、MCP 返回都带它；动态另带 `task_number`。`GET /tasks/{id}` 的 `{id}` 也接受 `123`（纯数字按序号找）；MCP 的 `task_id` 参数接受 `#123` / `123`。找不到 → 404「没有序号为 N 的任务。」（`err.task_number`）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/tasks?goal=&assignee=&creator=&reviewer=&state=&type=&parent=&limit=` | `assignee/creator/reviewer` 可填 `me`（含我的 Agent）；`state` 可填状态名或状态类型 |
| GET | `/tasks/mine` | 我（含我的 Agent）名下未结束的任务 |
| POST | `/tasks` | `{title, type?, goal_id?, parent_id?, description?, assignee_id?, reviewer_id?, participants?{位置: 执行者 id}, required_capabilities?, human_only?, planned_start?, planned_end?, estimate?, priority?, fields?, ready?}`（`ready` 默认 true：创建后直接就绪）→ 任务 |
| GET | `/tasks/{id}` | 任务详情；`{id}` 为纯数字时按序号查（`/tasks/123`） |
| GET | `/task-by-number/{n}` | 按序号查任务详情（`123` 或 `#123`）。原计划的 `/tasks/by-number/{n}` 与 `/tasks/{id}/workflow` 在路由上冲突，改用这条 |
| GET | `/tasks/{id}/links` | 任务上的外部链接（ADR 0020）→ `[外部链接]` |
| POST | `/tasks/{id}/links` | `{kind: pr\|issue\|doc\|design\|other（默认 other）, url, title?}` → 外部链接。`url` 必须是 http / https；同一任务同一地址只有一条（重复即更新）。Agent 与发评论同一套规则：要有「执行任务」授权，是「需要人确认」时返回 202 的待确认操作。动态 `ExternalLinkAdded` / `ExternalLinkUpdated` |
| DELETE | `/tasks/{id}/links/{link_id}` | 204。动态 `ExternalLinkRemoved`；没有 → 404「没有这条外部链接。」 |
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

## MCP 与界面同一套解释（功能规划第 9 项）

MCP 工具与 HTTP 接口都只调用 app 层，拒绝理由由同一批词条渲染；MCP 层把任何错误按请求者语言（Agent 用所有者的语言）渲染成与 HTTP `error.message` **完全相同**的句子（`mcp.RenderError`），「没有找到」也按词条，不再漏出默认语言或内部错误原文。六类理由与词条：

| 理由 | 词条 | HTTP |
|---|---|---|
| 越权（缺授权） | `reject.agent_no_grant` / `err.agent_no_grant`「Agent 没有「评论」授权」，`err.agent_task_field`、`err.agent_task_not_mine` | 409 / 403 |
| 范围不可见 | `err.task_hidden`「这个任务不在你能看到的范围里。」（任务详情、任务说明、流程与全部任务命令都按组织的协作数据可见性策略判断，与列表同一口径），`err.parent_task_hidden`，`err.scope_*` | 403 |
| 需要人确认 | `proposal.pending_msg`「已提交待确认操作，等某人确认后才会执行。」HTTP 202 `{proposal, message}`；MCP 返回同一句 + `proposal.hint`，不算错误 | 202 |
| 前置未完成 | `reject.deps_open`「前置任务未完成：X」 | 409 |
| 并发已满 | `reject.concurrency`「Agent 已达并发上限」（领取、开始执行、推进进入进行中三条路都查） | 409 |
| 任务已结束 | `reject.task_closed`「任务已经结束，不能再领取、开始或推进。」（新增：此前是「当前状态没有这一步」），编辑用 `err.task_closed_edit` | 409 |

`internal/mcp/parity_test.go` 用一张表把每个场景在两个面上各走一遍（中英文），断言两边的句子等于词条渲染结果。工具描述里写清前置（「先用 get_task_brief 拿到任务说明、get_workflow 确认 can_begin 为真，再 begin」）；`task_id` 参数接受序号 `#123`。

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

## 显示偏好（功能规划第 4 项）

一个人怎么看界面：任务列表显示哪些列、看板卡片显示哪些字段、默认打开哪个任务视图、是否紧凑、侧栏是否默认收起。解析与工作台同一套（ADR 0015）：**逐字段** 个人 → 角色（按成员的角色顺序取第一个设了这项的）→ 系统默认。每条记录都是部分对象，只存明确设过的字段。Agent 调用一律 403（`err.preferences_agent`）。

偏好对象：`{task_list_columns[], task_card_fields[], default_task_view, compact, sidebar_collapsed, source: personal|role|default, sources{字段: personal|role|default}, roles_used[], overrides[]}`（`source` 是整体来源：有任一字段来自个人即 personal；`overrides` 是个人明确设过的字段）。

列键：`number, title, type, state, assignee, reviewer, goal, sprint, priority, points, planned, due, cost`；卡片字段键：`number, assignee, due, priority, points, goal`；视图键：`list, board, gantt, sprints, backlog`。系统默认：列 `number,title,type,state,assignee,goal,priority,due`，卡片 `number,assignee,due,priority`，视图 `list`，紧凑与侧栏收起均为假。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/me/preferences` | 我的偏好（已解析） |
| PUT | `/me/preferences` | 部分修改：请求体是偏好字段的任意子集，如 `{"compact": true, "task_card_fields": ["number","due"]}`；某字段传 `null` 表示清掉个人的这一项、回到角色 / 默认。校验（整句）：列表必须含 `title`（`err.pref_columns_title`）、至少一列、列 / 字段 / 视图必须在目录里且不重复、不认识的键报错（`err.pref_key_unknown`）、空对象报错（`err.pref_body_empty`）→ 偏好。动态 `PreferencesUpdated` |
| DELETE | `/me/preferences` | 一键重置：清掉个人记录 → 200，返回解析后的偏好 |
| GET | `/me/preferences/catalog` | `{columns[{key,title}], card_fields[{key,title}], views[{key,title}], defaults{偏好字段}}`，标题按语言 |
| GET | `/org/preferences` | 各角色的偏好 `[{role, role_title, data{只含设过的字段}, fields[], member_count}]`（需要 `org_settings`） |
| PUT | `/org/preferences/{role}` | 给角色配默认，语义同 `PUT /me/preferences`（部分修改、`null` 清项）→ 角色偏好（需要 `org_settings`） |
| DELETE | `/org/preferences/{role}` | 清除角色偏好 → 角色偏好（`data` 为空） |

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
| GET | `/agents` | `[{id,name,owner: ExecutorRef,shared,capabilities[],grants[{name,mode}],online,last_seen_at,max_concurrency,runtime,created_at}]`。`runtime` 取值 `claude-code | cursor | codex | custom`（设备码接入时按申请填） |
| GET | `/agents/{id}/check` | 接入向导的连接检查：`{online, connected, last_seen_at, last_tool?, last_tool_at?, hint}`。`connected` 表示曾收到过它的请求；`last_tool` 是最近一次调用的 MCP 工具；`hint` 是一句人话（「还没有收到这个 Agent 的任何请求…」/「已在线：最近一次调用了 whoami（16:32）。」）。能看到这个 Agent 的人都能查 |
| POST | `/agents` | `{name, runtime?, capabilities[], grants[{name, mode: direct|with_approval}], shared?, max_concurrency?}` → `{agent, token}`（令牌只返回一次） |
| PATCH | `/agents/{id}` | 同上字段子集 → Agent |
| DELETE | `/agents/{id}` | 204。吊销：令牌失效、进行中的执行记录取消、名下任务回待领取 |
| GET | `/members` | `[{id,name,email,roles[],team_id,created_at}]` |
| GET | `/capabilities` | `{代码名: 中文}` |

## 任务类型

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/task-types` | `[{name,title,builtin,participants{位置:{title,role}},participant_order[],task_schema,result_schema,agent_instructions,workflow{version,initial,states{名:{name,title,label,weight?,claimable?}},state_order[],transitions[{name,title,from[],to,by[],requires[],grant?,assign_to,triggered_by?{source,event}}]}}]` |
| GET | `/task-types/{name}` | 单个 |
| PUT | `/task-types/{name}` | 保存新版本；请求体为领域格式（见 `docs/spec/workflow-definition.md`）；组织负责人或 admin / workflow_admin 角色 |

**外部事件触发**（ADR 0020）：一步可以带 `triggered_by: {source: "git", event: pr_opened\|pr_ready\|pr_merged\|pr_closed\|ci_passed\|ci_failed}`，声明"代码平台上发生这件事时自动走它"。校验：`source` 只能是 `git`，`event` 必须在这六个里（否则「步骤「x」的触发事件「y」系统不认识。」）。可选事件的清单在 `GET /org/code-platform` 的 `events[{key,title}]` 里。内置的 `requirement` 带 `design_done ← pr_opened`、`dev_done ← pr_merged`、`block ← ci_failed`，`bug` 带 `start_fix ← pr_opened`、`fixed ← pr_merged`；`generic` 与 `release` 不带。

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
| GET | `/org/directory/providers` | 可接入的提供方 `[{key, title, root_department_id, fields[{key, title, secret, optional, placeholder, hint, set: false}], prerequisites[], tip{text, url?}}]`（顺序固定：feishu、wecom）。代码平台（GitHub / GitLab / Gitee）不在这里，它们是注册表里的另一种能力，见「代码平台与外部事件」 |
| GET | `/org/directory` | `{provider: "feishu"\|"wecom"\|null, provider_title, configured: bool, credentials{key: value}（只有非保密字段）, secrets_set{key: bool}（保密字段是否已设置）, root_department_id, root_department_ids[]（同步根部门列表，空表示整个企业；`root_department_id` 是列表第一个，兼容旧读法）, default_role, schedule: manual\|hourly\|daily, schedule_title, schedules[{value,title}], proxy_url, providers[]（同上，当前提供方的 `fields[].set` 与非保密字段的 `fields[].value` 会填上）, last_run{id, provider, started_at, finished_at, status: ok\|partial\|failed, status_title, added_teams, updated_teams, deactivated_teams, added_members, updated_members, deactivated_members, errors[]}\|null}`。永远不返回保密字段的值 |
| PUT | `/org/directory` | `{provider?, credentials?{key: value}, root_department_id?, root_department_ids?: []string, default_role?, schedule?, proxy_url?}`。`provider` 首次保存必填，之后省略表示沿用；换提供方时凭据与根部门重置为新提供方的。`credentials` 按提供方声明逐字段校验：非保密字段省略表示沿用现有值（首次必填，否则「请填写<字段名>。」）；保密字段省略或为空表示不改（首次保存必填，否则「第一次保存需要填写<字段名>。」）。**同步根**：`root_department_ids` 是列表（ADR 0017 补记二）：空列表 = 整个企业；一个 = 只同步那个部门（它自己也成为一个团队）；多个 = 每个各自作为顶层团队。只给 `root_department_id` 时视为长度为一的列表；提供方的根（飞书 `"0"`、企业微信 `"1"`）等于整个企业，不进列表。去空白、去重。`proxy_url` 与地址类字段要过出网守卫（同「代码平台」那一节，指向内网时给同一句话）。产生动态 `DirectoryConfigured`（数据里只有改了哪些字段的名字，**没有任何配置值**） |
| DELETE | `/org/directory` | 断开 IM 集成：删掉配置，回到未接入；已同步的团队与成员、外部身份、历次同步都保留 → 配置（未接入）|
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

## 通知外发（ADR 0019，`/org/notifications` 与 `/me/notifications`）

三层：站内「待我处理」是事实源；组织决定允许哪些**通知通道**（飞书、企业微信、邮件、webhook）与哪些事件类型可外发；个人在允许范围内为每种事件选通道与**安静时段**。外发只对新产生的事项生效，不回放历史；由后台每 15 秒把到点的**投递**发出去（最多 3 次，间隔 30 秒、2 分钟；安静时段内的顺延到时段结束），失败不阻塞业务写入。同一事项、同一收件人、同一通道只发一次。

事件类型（`kind`，顺序固定）：`proposal` 待确认操作、`review` 等我验收、`assigned` 指派给我、`question` 提问、`blocked` 任务被阻塞、`overdue` 逾期、`milestone_due` 里程碑到期。`blocked` 由外部事件产生（ADR 0020）：代码平台报「检查失败」并把任务推进一个「等待中」的状态时，给负责人一条「任务被阻塞：<任务>」，正文写清是哪个平台的什么事、任务进了哪个状态；组织策略与个人规则照常适用。前四种在站内通知落库的那一刻排队（链接 `PUBLIC_URL/tasks/{id}/`，待确认操作指向首页 `PUBLIC_URL/`）；`overdue` / `milestone_due` 由后台每分钟扫一次，只对最近 3 天内刚逾期 / 刚到期的事项各提醒一次（不另落站内通知：「待我处理」本来就按日期派生出逾期组），链接分别是任务页与目标页。进入待领取、待确认操作被拒绝只在站内。消息正文 = 站内那句话 + 一个「打开」按钮 / 链接，不含成本与他人信息。

通道是提供方的一种能力（`internal/directory` 的 `Messenger`）：飞书、企业微信复用 IM 集成的凭据与接入检查（清单多一项 `messaging`「能以应用身份发消息」，待处理、不阻塞；企业微信发消息要另填自建应用的 `agent_id` 与 `app_secret`，通讯录同步的 Secret 发不了消息；飞书同一个应用，要开通「以应用的身份发消息」权限）；邮件（`email`：`host, port, username, password（保密）, from`，都留空表示用服务端环境变量 `SMTP_URL` / `SMTP_FROM`）与 webhook（`webhook`：`url`、可选 `secret`（保密））是只发消息的提供方。IM 通道只在它是组织当前 IM 集成提供方且接入齐全时可配置。保密字段加密入库，永不回显。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/org/notifications` | 组织策略（需要 `org_settings`）→ `{channels{key: {key, title, enabled, configured, available（= enabled 且 configured）, im, config{非保密字段}, secrets_set{保密字段: bool}, fields[{key,title,secret,placeholder,hint,set,value?}], prerequisites[], hint?（为什么不可用）, health{status: ok\|degraded, streak, last_error, last_at?}}}, channel_order[]（feishu、wecom、email、webhook；测试提供方接入时排最前）, allowed_kinds[], kinds[{key,title}], health{channel: {...同上}}}`。默认：所有通道 `enabled: true`（IM 通道接入即可用，邮件 / webhook 配了就可用），全部事件类型允许。`health.status` 在最近连续失败 ≥ 3 次时为 `degraded`，`last_error` 是提供方原话 |
| PUT | `/org/notifications` | `{channels?{key: {enabled?, config?{字段: 值}}}, allowed_kinds?[]}`。`config` 按提供方声明逐字段校验（「Webhook通道没有「token」这个字段。」）；地址类字段（webhook 的 `url`）要过出网守卫，指向内网时 400「这个地址指向内网，默认不允许。私有化部署可以设置环境变量 AXIOMOS_ALLOW_PRIVATE_EGRESS=1 后再填。」；保密字段省略或为空表示不改；`allowed_kinds` 整体替换、按目录顺序存（不认识的报「没有「x」这种事件类型。」）；两者都没给报「没有要修改的内容。」→ 同 GET。每个改了的通道一条动态 `NotificationChannelConfigured`（`{channel, enabled, fields[]}`，关闭时句子是「关闭了通知通道」），允许范围改了一条 `NotificationPolicyChanged`（`{allowed_kinds[]}`） |
| POST | `/org/notifications/test` | `{channel}` 给调用者本人同步发一条测试消息 → `{ok, message, delivery}`。`message` 是一句投递结果：「已通过飞书发给你，去看看。」/「飞书没发出去：<原话>」/「这个成员没有绑定飞书身份，收不到飞书消息。」；通道没配置时 400「飞书通道还不能用：<原因>」。记一条 `kind: test` 的投递（不重试） |
| GET | `/org/notifications/deliveries?limit=&channel=&status=` | 组织最近的投递（需要 `org_settings`）→ `[{id, kind, kind_title, channel, channel_title, status: queued\|sent\|failed\|skipped, status_title, title, text, url, error, attempts, created_at, sent_at?, recipient{id,name}}]`，倒序，`limit` 默认 50、最多 500 |
| GET | `/me/notifications` | 我的偏好（解析后；Agent 403）→ `{rules{kind: channels[]}, sources{kind: personal\|default}, quiet_hours{from,to}\|null, available_channels[{key, title, im, bound, hint?}], kinds[{key,title}], allowed_kinds[], defaults{kind: channels[]}, source: personal\|default}`。`available_channels` 只列组织开了且配置齐全的通道；`bound` 是我绑没绑这个 IM 的身份（没绑时 `hint` 说去哪儿绑，规则里选了它也发不到）。默认：`proposal` 与 `review` 走 IM（绑定了才算），否则走邮件（组织开了邮件时），其余只站内；组织不允许的事件或通道会从 `rules` 里裁掉（`sources` 仍说来源） |
| PUT | `/me/notifications` | 部分修改：`{rules?{kind: channels[]}, quiet_hours?{from: "22:00", to: "08:00"}\|null}`。`rules` 里出现的事件类型被设置（空列表 = 只站内），`quiet_hours` 出现即设置（`null` 清掉）。校验（整句）：通道必须是组织开放的（「组织没有开放邮件通道。」）、事件必须是组织允许的（「组织不允许外发「逾期」，这一项只能留在站内。」）、时间 `HH:MM` 且首尾不同、不认识的键报错、空对象报错 → 同 GET。动态 `NotificationPreferencesUpdated` |
| DELETE | `/me/notifications` | 一键重置：清掉个人记录 → 200，同 GET |
| GET | `/me/notifications/deliveries?limit=` | 我最近的投递（形状同组织侧，不带 `recipient`） |

**投递状态**：`queued` 排队（含退避重试中，`attempts` / `error` 是上一次的）→ `sent`；三次都失败 → `failed`（`error` 是提供方原话）；没法发 → `skipped`（原因整句：没绑定 IM 身份、没有邮箱、组织关了通道、通道配置有问题）。

**提供方接口**：飞书 `POST /open-apis/im/v1/messages?receive_id_type=open_id`，卡片（`interactive`：标题 + 一句话 + 「打开」按钮）或纯文本（没链接时）；`messaging` 检查往一个不存在的 open_id 试发一条——权限类错误码 → 待处理并指向 `…/app/{app_id}/auth`，230002 → 机器人未启用，其余拒绝说明权限已有。企业微信 `POST /cgi-bin/message/send`（`textcard`：标题、一句话、`url`、`btntxt`；`invaliduser` 非空算失败）；`messaging` 检查用应用 Secret 读 `GET /cgi-bin/agent/get?agentid=`。webhook：`POST` JSON `{kind, title, text, url, recipient{id,name}, at}`，头 `X-AxiomOS-Kind`，有密钥时 `X-AxiomOS-Signature: t=<unix 秒>,v1=<hex(HMAC-SHA256(secret, "<秒>.<body>"))>`，非 2xx 算拒绝。`t` 是**发出这一刻**的时间（不是消息生成时间）：接收方验签时先看 `t` 与当前时间相差是否在 **5 分钟**以内，超出就丢弃（防重放），再用常数时间比较 `v1`。邮件：纯文本，主题 = 那句话，正文 = 那句话 + 「打开：<链接>」，`X-AxiomOS-Kind` 头；465 走 TLS，其余端口 STARTTLS。

## 代码平台与外部事件（ADR 0020）

代码平台（GitHub、GitLab、Gitee）与 IM 集成、通知通道走**同一套提供方注册表**（`internal/directory`），只是能力不同：`Providers()` 读组织结构、`MessagingProviders()` 发消息、`CodeHostProviders()` 收 PR 与检查结果。三个列表互不重叠，接口层与前端都按声明渲染，不出现平台名的分支。

接入步骤与 IM 集成一样：选平台 → 填令牌 → 检查（凭据可用 / 能读到仓库 / 回调已建好）→ 选仓库（系统自动去建回调）→ 完成。一个组织一个代码平台。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/org/code-platform` | 需要 `org_settings` → `{provider: "github"\|"gitlab"\|"gitee"\|null, provider_title, configured, credentials{非保密字段}, secrets_set{保密字段: bool}, providers[{key,title,fields[{key,title,secret,optional,placeholder,hint,set,value?}],prerequisites[],tip{text,url?}}], repos[{id, full_name, enabled, hook_ok, hook_error?}], webhook_url, webhook_secret_set, proxy_url, console_url?, events[{key,title}]}`。`events` 是六种外部事件，流程编辑器按它列候选。**回调密钥默认不返回**，只有 `webhook_secret_set` 说它设没设 |
| GET | `/org/code-platform?reveal=secret` | 同上，另外带 `webhook_secret`（明文，人要把它复制到代码平台的回调设置里）。这一次响应带 `Cache-Control: no-store`，并产生一条动态 `CodeWebhookSecretRevealed`「某人 查看了 GitHub 的回调密钥」。凭据类的保密字段仍然永不回显 |
| PUT | `/org/code-platform` | `{provider?, credentials?{key: value}, repos?[全名], proxy_url?, rotate_webhook_secret?}`。`provider` 首次必填；`credentials` 按提供方声明逐字段校验（可选字段可以留空，如 GitHub 的接口地址）；保密字段省略或为空表示不改。**地址类字段（`api_base`、`proxy_url`）要过出网守卫**：指向内网时 400「这个地址指向内网，默认不允许。私有化部署可以设置环境变量 AXIOMOS_ALLOW_PRIVATE_EGRESS=1 后再填。」，带用户名密码时 400「地址里不能带用户名和密码，请去掉再填。」，非 https 时 400「这个地址要用 https 开头，收到的是「…」。」。第一次保存、换平台，或 `rotate_webhook_secret: true` 时生成新的回调密钥（换了要把新密钥填回代码平台，动态 `CodeWebhookSecretRotated`）。给了 `repos` 就**整体替换**仓库选择，并逐个调用平台的建回调接口：建不上不算失败，那一行带 `hook_ok: false` 与一句 `hook_error`（如「这个令牌看不到仓库「acme/nope」。」）→ 同 GET。动态 `CodePlatformConfigured`（`{provider, provider_switched, fields[]}`：**只有改了哪些字段的名字，没有任何配置值**） |
| DELETE | `/org/code-platform` | 断开代码平台：删掉配置与仓库选择，回到未接入；已挂的外部链接与动态保留 → 配置（未接入）|
| POST | `/org/code-platform/test` | 跑同一份接入检查并摘要 → `{ok, repos, error?, warnings[]?}`；有阻塞项时 `ok: false`，`error` 是那一项的说明与修法（如「GitHub 拒绝了这个令牌：HTTP 401: {"message":"Bad credentials"}。重新填写访问令牌，并确认它有读取仓库与管理 webhook 的权限。」） |
| GET | `/org/code-platform/checklist` | 现场跑接入检查（Check 形状与 IM 集成完全一致）→ `{provider, provider_title, console_url?, webhook_url, checks[{key,title,status: ok\|todo\|blocked\|skipped,detail,fix,fix_url,blocking}], ready, next{step: credentials\|checks\|repos\|done, text}, repos[全名]}`。检查项按序：`credentials` 凭据可用 → `repos` 能读到仓库 → `webhook` 回调已建好（选中的仓库上有指向 `webhook_url` 的回调且带密钥）。未配置时 `checks` 为空、`next.step = credentials` |
| GET | `/org/code-platform/repos` | 现场读平台上的仓库并标出已选中的 → `[{id, full_name, enabled, hook_ok, hook_error?}]` |
| GET | `/me/code-identity` | 我绑定的登录名 → `{provider, provider_title, login, bound}`；Agent 403 |
| PUT | `/me/code-identity` | `{login}`（空串表示解绑）→ 同 GET。别人已经绑了这个登录名时 400「登录名「alice」已经绑定给别人了。」。动态 `CodeIdentityBound` |
| POST | `/hooks/code/{provider}/{org_id}` | **公开**（完整路径 `/api/v1/hooks/code/github/{org_id}`）。用组织自己存的密钥验签，验过就回 200 `{provider, event, duplicate, tasks[], applied[], ignored[], note?}`；签名不对 401「回调签名不对，这次请求被丢弃了。」，组织没配这个平台 404。同一投递编号只处理一次（`webhook_events` 去重；平台没给投递编号时用载荷指纹）。**验签之后还要看仓库**：载荷里的仓库必须是这个组织在 `code_repos` 里接进来且还启用着的，否则什么都不改，`note` 写「仓库「x/y」不在这个组织接入的仓库里，这次回调没有改动任何东西。」（密钥万一泄露也不能拿没接入的仓库伪造事件）。载荷带的链接地址不属于这个平台的域名时也不挂链接，`note` 说明原因 |

**各平台**（凭据字段以 `GET /org/code-platform` 的 `providers` 为准）：

- GitHub `github`：`token`（访问令牌，保密）、`api_base`（接口地址，可选，企业版填 `https://<域名>/api/v3`）。接口 `GET /user/repos`、`GET/POST /repos/{owner}/{repo}/hooks`、`PATCH .../hooks/{id}`；回调事件 `pull_request` 与 `check_suite`。请求头 `X-GitHub-Event`、`X-GitHub-Delivery`、`X-Hub-Signature-256: sha256=hex(HMAC-SHA256(secret, body))`。
- GitLab `gitlab`：`token`（访问令牌，保密，范围 api）、`api_base`（可选，默认 `https://gitlab.com/api/v4`）。接口 `GET /projects?membership=true`、`GET/POST/PUT /projects/{编码后的全名}/hooks`；回调开 `merge_requests_events` 与 `pipeline_events`。请求头 `X-Gitlab-Event`、`X-Gitlab-Event-UUID`、`X-Gitlab-Token`（就是密钥本身，逐字节比对）。
- Gitee `gitee`：`token`（私人令牌，保密，勾 projects 与 hook）。接口 `GET /user/repos`、`GET/POST/PATCH /repos/{owner}/{repo}/hooks`。请求头 `X-Gitee-Event`、`X-Gitee-Token`（密码模式就是密钥本身；签名模式是 `base64(HMAC-SHA256(secret, "<X-Gitee-Timestamp>\n<secret>")`），两种都认。Gitee 没有统一的流水线回调，所以它只产生 PR 类事件。

**事件映射**（平台动作 → 六种外部事件 + 链接状态）：

| 平台动作 | 外部事件 | 链接状态 |
|---|---|---|
| GitHub `pull_request` opened / reopened（非草稿）；GitLab MR `open` / `reopen`；Gitee `open` / `reopen` | `pr_opened` | `open` |
| 同上但是草稿 | `pr_opened` | `draft` |
| GitHub `ready_for_review`；GitLab MR `update` 且草稿标记从真变假 | `pr_ready` | `open` |
| GitHub `converted_to_draft` | —（只更新链接） | `draft` |
| GitHub `closed` 且 merged；GitLab `merge`；Gitee `merge` | `pr_merged` | `merged` |
| GitHub `closed` 未 merged；GitLab `close`；Gitee `close` 未 merged | `pr_closed` | `closed` |
| GitHub `check_suite` completed 且 conclusion=success；GitLab 流水线 `success` | `ci_passed` | `passed` |
| GitHub `check_suite` conclusion=failure / timed_out / startup_failure；GitLab 流水线 `failed` | `ci_failed` | `failed` |
| 其余（同步提交、改标题、ping、推送…） | —（只更新链接或直接跳过） | 不变 |

**任务关联**：PR 标题或分支名里的 `#123`（组织内任务序号）、描述里出现的本系统任务链接（`PUBLIC_URL/tasks/{id}`）；命中多个任务时每个都处理一遍。CI 事件通常不带这些信息，靠这个 PR 已经挂上的外部链接（`external_id`）找回任务。

**一次回调做的事**（每个命中的任务各来一遍）：

1. 建或更新外部链接（`{task_id, provider, kind: pr, url, title, external_id, status, actor_name, updated_at}`；同一任务同一地址只有一条）。动态 `ExternalLinkAdded` / `ExternalLinkUpdated`。
2. PR 链接第一次挂上时顺带附一条「代码 PR」交付物（内置流程里「开发完成」「修复完成」以它为前提；不补的话 PR 合并事件永远只能写一条「缺少交付物」的说明）。动态 `ArtifactAttached`。
3. 把事件交给内核：当前状态有 `triggered_by` 对得上的步骤且其余前提满足就走它（动态 `ExternalEventApplied` + `TaskTransitioned`），否则什么都不改（动态 `ExternalEventIgnored`，里面是一句说清为什么的中文）。
4. 这一步进的是「等待中」类状态且事件是「检查失败」时，给负责人一条 `blocked` 通知（走 ADR 0019 的通道与个人规则）。

**外部操作者**：动态里带 `external_source`（平台）与 `external_user_name`（登录名）；这个登录名绑定了本系统成员时（`external_identities`，`kind = git_user`，`external_id` = 登录名），动态挂在那个成员名下，界面显示成他。没绑定时显示成「GitHub 用户 alice」。

**凭据存储**：`org_code_platform.credentials`（jsonb，非保密字段明文）+ `secrets_enc`（保密字段合成一个 JSON 后用 `AXIOMOS_SECRET_KEY` 做 AES-GCM 加密）+ `webhook_secret_enc`（回调签名密钥，同一把服务端密钥）。仓库选择在 `code_repos`，外部链接在 `external_links`，投递去重在 `webhook_events`（保留 30 天）。

## Agent 设备码接入（ADR 0018）

Agent 端一条命令，人在网页里批准，系统创建 Agent、把令牌交给 Agent 端并写好客户端配置。设备码绑定的是 Agent 身份（批准人成为所有者），不是人的登录。验证码 15 分钟内有效；轮询间隔 5 秒，快于它返回 429 `{"error":{"code":"slow_down","message":"轮询太快了，每 5 秒问一次。"}}`。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/agent-auth/device` | **公开**。`{client: "claude-code"\|"cursor"\|"codex"\|"custom", name?}` → `{device_code, user_code (ABCD-1234), verification_url (<PUBLIC_URL>/agents/connect/?code=ABCD-1234), expires_in: 900, interval: 5}`。`name` 最多 80 字，未知 `client` → 400 |
| GET | `/agent-auth/device/{user_code}` | 需登录，**按账号与来源地址限次**：连续查不到 10 次（5 分钟内）就冷却 5 分钟，期间一律 429「验证码试得太多了，过几分钟再试。」（批准 / 拒绝走同一道限制）。按验证码看申请（大小写、横线、空格都不计较）：`{user_code, client, client_title, name, status: pending\|approved\|denied\|expired, status_title, created_at, expires_at, decided_at, agent{id,name}\|null, approved_by{id,name}\|null}`。不存在或属于别的组织 → 404 |
| POST | `/agent-auth/device/{user_code}/approve` | 需登录（Agent 不能批准）。`{name?, capabilities[], grants: {execute: "allow"\|"with_approval"\|"deny", comment: ..., ...} 或 [{name, mode}], max_concurrency?, shared?}` → `{agent, request}`。以批准人为所有者创建 Agent（`runtime` = 申请的 `client`，`name` 缺省用申请里的名字，再缺省用运行环境名），`deny` 的授权不进表，`manage_workflows` 只能是需要人确认。已处理 / 已过期 → 400 整句。动态 `AgentConnected`「某人 批准了 Agent「X」接入（Claude Code）」 |
| POST | `/agent-auth/device/{user_code}/deny` | 需登录。拒绝 → 申请对象（`status: denied`），不创建任何东西 |
| POST | `/agent-auth/token` | **公开**。`{device_code}` → `{status: pending\|approved\|denied\|expired, token?, agent?{id,name}, organization?, mcp_url, interval}`。`token` 只在批准后的**第一次**成功轮询里出现，之后再问只回 `status: approved` 与 `agent`；无效设备码 → 404。限流与领取都是单条带条件的 SQL：并发轮询里只有一次能过限流、只有一次能领到令牌，其余 429 或只回状态 |
| GET | `/agent-auth/connect.sh?client=&lang=` | **公开**。返回 `text/x-shellscript`（POSIX sh，只依赖 curl）：申请设备码 → 打印「打开 <网址> 输入 ABCD-1234 批准这个 Agent」并尽量拉起浏览器（`AXIOMOS_NO_BROWSER=1` 关掉）→ 轮询 → 写配置：`claude-code` 有 `claude` CLI 时 `claude mcp add --transport http axiomos <mcp_url> --header "Authorization: Bearer <token>"`，否则打印 JSON；`cursor` 合并进 `~/.cursor/mcp.json`（有 python3 时就地合并，否则打印）；`codex` 追加 `[mcp_servers.axiomos]` 到 `~/.codex/config.toml`；`custom` 只打印 JSON → 最后调一次 `whoami` 做连接检查。句子按 `lang` 或 `Accept-Language`。环境变量 `AXIOMOS_AGENT_NAME` 指定名字。令牌只写进客户端配置，不落日志。用法：`curl -fsSL '<PUBLIC_URL>/api/v1/agent-auth/connect.sh?client=claude-code' \| sh`。PowerShell 版暂不提供（Windows 用 WSL，或走「其他客户端」的手工令牌路径） |

后台巡检每分钟把过期的待批准申请标为 `expired`、清掉一天前的记录；已批准但一直没来取的令牌在过期后也会清掉。

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
