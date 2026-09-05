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
| PATCH | `/goals/{id}` | 同上字段的任意子集，另有 `achieved: bool`（确认达成 / 取消达成）与 `status: "active"\|"abandoned"`（放弃 / 重新开始） |
| DELETE | `/goals/{id}` | 删除目标。只有空目标（没有子目标、没有任务）能删；否则 400 并说明还有多少子目标和任务，请改为放弃 |

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
| PATCH | `/tasks/{id}` | 同上字段的子集。Agent 只能改自己负责或自己创建的任务，需要「执行任务」授权；`reviewer_id` / `priority` / `goal_id` / `sprint_id` / `participants` / `human_only` 对 Agent 一律拒绝（ADR 0003 实现补记） |
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

区块键（系统定义）：`overview_summary` 组织概览摘要、`exceptions` 要我关注的异常、`my_tasks` 我的任务、`my_review` 等我验收、`team_load` 人员与 Agent 负荷、`cost_budget` 成本与预算、`proposals` 待确认操作、`events` 最近动态、`sprint` 当前迭代、`backlog` 待领取任务、`trend` 趋势与环比。预设键：`global` 全局视角、`unit` 部门视角、`team` 小组视角、`doer` 执行视角、`ops` 运营视角。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/workspace` | 我的工作台（已解析）：`{blocks[{key,title}], source: personal\|roles\|default, roles_used[], can_customize: true}`。解析顺序：个人微调 → 各角色布局并集（按首次出现顺序去重）→ 默认（组织负责人 `global`，其他 `doer`） |
| PUT | `/workspace/me` | `{blocks[]}` 个人微调（只能用系统区块键）→ 工作台 |
| DELETE | `/workspace/me` | 清除个人微调，恢复角色默认 → 200，返回解析后的工作台 |
| GET | `/workspace/blocks` | 全部区块 `[{key,title,description}]` 与预设 `presets[{key,title,description,blocks[]}]`，标题按语言 |
| GET | `/org/workspace` | 各角色的布局 `[{role,role_title,blocks[],preset\|null,member_count}]`（需要 `org_settings`） |
| PUT | `/org/workspace/{role}` | `{blocks[]}` 或 `{preset}` 给角色配布局（需要 `org_settings`，产生动态 `WorkspaceLayoutUpdated`） |
| DELETE | `/org/workspace/{role}` | 清除该角色布局 → 200，返回该角色视图。清除会留下一条空布局作为"明确清除"的记录：解析时等同于没有布局，但重启时的默认补齐不会再把内置预设填回来 |

首次种子时内置角色的默认映射：管理员 → 全局视角；运营、流程管理 → 运营视角；产品、设计、开发、测试、发布 → 执行视角。之后每次启动只给**没有任何记录**的内置角色补默认，组织改过或清过的一律不动。个人微调同样产生动态。

## 待确认操作

Agent 发起、但其授权模式是「需要人确认」的动作不会立即生效，而是记为一条待确认操作，等人点确认后才执行（ADR 0003）。对象：`{id, agent{id,name}, owner{id,name}, action, action_title, grant, target{kind: task|goal|sprint|task_type|agent, id, title}|null, summary, payload, status: pending|approved|rejected|expired, status_title, can_decide, decided_by, decided_at, reason, created_at, expires_at}`（`grant` 是用到的授权名，`can_decide` 表示当前登录者能否确认这一条）。`summary` 是完整中文句子，说明"确认后会发生什么"。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/proposals?status=pending|approved|rejected|expired&agent=&mine=1&limit=` | `mine=1` 只看等我确认的（我是发起 Agent 的所有者，或我持有该动作所需权限）。默认按创建时间倒序 |
| GET | `/proposals/{id}` | 详情 |
| POST | `/proposals/{id}/approve` | 确认并立即执行；执行失败时保持 pending 并返回失败理由（完整中文句子）→ `{proposal, result}` |
| POST | `/proposals/{id}/reject` | `{reason}` 必填，至少 6 个字的完整句子（Agent 直接读它）→ 待确认操作 |
| GET | `/proposals/count` | `{pending: n}`，供侧栏与状态栏显示提醒 |

Agent 侧：任何写操作若命中「需要人确认」的授权，HTTP 返回 `202` 与 `{proposal, message}`（`message` 是完整中文句子，如「已提交待确认操作，等小王确认后才会执行。」）；MCP 工具同样返回这句话与待确认操作 ID，并新增 `list_my_proposals` 查看自己提交的待确认操作。

动态种类新增：`ProposalCreated`、`ProposalApproved`、`ProposalRejected`、`ProposalExpired`（七天没人确认自动作废，由后台巡检产生）。确认后执行产生的动态照常记在 Agent 名下，并在摘要里带上「经<确认人>确认」。

会走待确认操作的动作（`action` 取值）：`task.transition`、`task.claim`、`task.begin`、`task.assign`、`task.comment`、`task.note`、`task.artifact`、`task.link`、`task.create`、`task.create_subtask`、`goal.create`、`task_type.save`、`sprint.start`、`sprint.close`。每条另有 `action_title`（当前语言的动作名）、`status_title`、`can_decide`（当前登录者能否确认）。`task_type.save`、`sprint.start`、`sprint.close` 需要「管理流程」权限才能确认，其余只有 Agent 的所有者与组织负责人能确认。

## Agent 与成员

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
| GET | `/org/members` | `[{id,name,email,roles[],team_id,active,is_owner,locale,created_at}]` |
| PATCH | `/org/members/{id}` | `{name?, roles?, active?, team_id?}` → 成员 |
| POST | `/org/members/{id}/make-owner` | 把组织负责人转给他 → 成员 |
| GET | `/org/invitations` | `[{id,email,name,roles[],url,expires_at,accepted_at,created_at}]` |
| POST | `/org/invitations` | `{email, name?, roles[]}` → 邀请（含 `url`，一期不发邮件，把链接发给对方） |
| DELETE | `/org/invitations/{id}` | 204 |
| GET | `/org/roles` | `[{name,title,titles{语言:名称},builtin,permissions[]}]`，权限取值：`org_settings`（组织设置）、`manage_workflows`（管理流程）、`cancel_any_task`（取消任何任务）。所有登录成员可读 |
| PUT | `/org/roles/{name}` | `{title: string 或 {"zh-CN","en-US"}, permissions[]}` → 角色（内置角色只能改 title） |
| DELETE | `/org/roles/{name}` | 204；内置角色和仍有人持有的角色不能删 |
| GET | `/org/teams` | `[{id,name,parent_id,lead_id,member_ids[]}]` |
| POST | `/org/teams` | `{name, parent_id?, lead_id?, member_ids?}` → 团队 |
| PATCH | `/org/teams/{id}` | 同上子集 → 团队 |
| DELETE | `/org/teams/{id}` | 204 |
| GET | `/org/capabilities` | `[{name,title,titles{语言:名称}}]` |
| PUT | `/org/capabilities/{name}` | `{title: string 或多语言对象}` → 能力标签 |
| DELETE | `/org/capabilities/{name}` | 204 |
| GET | `/org/pricing` | `{currency, models:[{model_id,input_per_million,output_per_million,cache_read_per_million,cache_write_per_million,currency,source: global|override}], exchange_rates:[{from,to,rate}]}` |
| PUT | `/org/pricing/models/{model_id}` | 组织覆盖 `{input_per_million,output_per_million,cache_read_per_million,cache_write_per_million,currency}` |
| DELETE | `/org/pricing/models/{model_id}` | 删除覆盖，回到全局 |
| PUT | `/org/pricing/rates` | `{from,to,rate}` |

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
