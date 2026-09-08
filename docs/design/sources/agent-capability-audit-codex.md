# Agent 能力覆盖审计（Codex 独立视角，2026-09-08）

> 由 OpenAI Codex CLI 在只读模式下独立审计 HTTP 接口全集、MCP 工具全集、CONTEXT.md、ADR 0003/0012/0016/0021–0025 与 docs/agent-integration.md 后生成，原文未改。与 `docs/plans/2026-09-agent-capabilities.md` 对照。


审计范围：`internal/api/*.go` 中 `auth(` / `pub(` 路由、`internal/mcp/server.go` 注册工具及 `prompts.go` 提示、`CONTEXT.md`、ADR 0003/0012/0016/0021–0025、`docs/agent-integration.md`。结论是：任务执行主链已经可用，但 Agent 仍缺少目标治理、完整编辑、验收、外部链接、运营观测和若干承诺中的流程能力。

## 1. HTTP 与 MCP 对照

当前 MCP 工具包括：

`whoami`、`list_my_tasks`、`list_backlog`、`get_task_brief`、`get_workflow`、`claim_task`、`begin_task`、`transition_task`、`heartbeat`、`add_comment`、`add_note`、`attach_artifact`、`create_task`、`create_goal`、`list_milestones`、`create_milestone`、`reach_milestone`、`link_tasks`、`assign_task`、`list_goals`、`list_task_types`、`list_members`、`get_task`、`list_tasks`、`list_sprints`、`get_sprint`、`add_tasks_to_sprint`、`remove_task_from_sprint`、`get_board`、`start_sprint`、`close_sprint`、`list_my_proposals`、`next_actions`。

### 目标

HTTP 有：

- `GET/POST/PATCH/DELETE /goals`
- `PUT /goals/horizon`
- `PUT /goals/rank`
- `GET /goals/{id}`
- `PATCH/DELETE /goals/{id}`

MCP 只有 `list_goals`、`create_goal`。缺少：

- `update_goal`：无法让 Agent 修正目标描述、时间、负责人以外的可编辑字段，也无法根据规划结果维护目标。
- `delete_goal`：通常应禁止 Agent 直接删除，改为 `archive_goal` 或提交待确认操作。
- `set_goal_horizon`、`rank_goals`：ADR 0025 明确把“批量改时间桶与批量重排”列入 Agent 的 dry-run/idempotency 范围，但没有对应工具。
- `get_goal`：只能列目标树，无法按单目标读取完整详情、父子链、成本和负责人。

影响：Agent 能创建目标，却无法持续维护目标，也无法完成“目标→规划→执行→复盘”的闭环。

建议：

- `get_goal(goal_id)`
- `update_goal(goal_id, patch, idempotency_key, dry_run)`，默认直接生效；涉及负责人、归属等高风险字段走待确认。
- `set_goal_horizon(goal_id, horizon)`、`rank_goals(goal_ids)`，支持 dry-run 和幂等。
- 删除应保留为人工专属，或提供 `archive_goal` 且强制待确认。

### 任务

已有创建、领取、开始、推进、评论、日志、交付物、指派、关联、读取工具，但 HTTP 的：

- `PATCH /tasks/{id}` 没有等价的 `update_task`。
- `GET /tasks/{id}/links`、`DELETE /tasks/{id}/links/{link_id}` 没有读取/删除关联工具。
- `POST /tasks/{id}/links` 对应的是代码平台链接；现有 `link_tasks` 只是任务间 blocks/found_in/relates_to 关系，语义不同。
- `GET /task-by-number/{n}` 目前靠各工具内部解析 `#123`，没有独立工具，属于可用但不完整的接口映射。
- `GET /tasks/{id}/workflow`、`brief` 已通过 `get_workflow`、`get_task_brief` 覆盖。

建议：

- `update_task(task_id, patch, ...)`：只允许 Agent 修改自己负责或自己创建的任务；执行任务授权可直接或待确认。验收人、优先级、目标归属、迭代、参与角色、“仅限人工”等字段继续禁止 Agent 修改，符合 ADR 0003。
- `list_task_links(task_id)`、`remove_task_link(task_id, link_id, ...)`。
- `add_external_link(task_id, url, kind, title, ...)`：ADR 0003 明确承诺 Agent 可挂外部链接，但当前只有 HTTP 与文档描述，没有 MCP 工具；执行授权需确认时走 Proposal。

### 里程碑

HTTP 有创建、更新、删除、reach、unreach；MCP 只有列出、创建、reach。缺少：

- `update_milestone`
- `delete_milestone`
- `unreach_milestone`

影响：Agent 可以宣布达成，却不能纠正日期/说明，也不能撤销误确认。建议提供三者，沿用“创建目标”授权；删除、撤销应默认待确认，所有写操作支持 dry-run/idempotency。

### 迭代

HTTP 有列表、创建、读取、更新、开始、结束、加入/移除任务、速度统计。MCP 缺少：

- `create_sprint`
- `update_sprint`
- `get_sprint` 虽存在，但没有独立的 `velocity` 查询工具（详情中部分覆盖）。
- 迭代任务加入/移除已有覆盖。

影响：Agent 能操作现有迭代，却不能自主建立或调整迭代计划。建议 `create_sprint`、`update_sprint`；开始/结束继续要求 `manage_workflows` 且 Agent 必须待确认，符合 ADR 0012 和 MCP 文案。

### 关联

`link_tasks` 只支持新增任务关系，HTTP 没有对应的关系删除 MCP 工具。建议 `unlink_tasks(from_task_id, relation_id/to_task_id)`，执行授权；删除 blocks 关系建议待确认，避免破坏依赖图。

### 外部链接

HTTP 代码平台接口提供任务链接增删；MCP 没有对应工具。这个缺口直接违背 ADR 0003 第 35 条“Agent 挂外部链接与发评论同一套规则”。应补 `list_task_links`、`add_external_link`、`remove_external_link`。

### 动态、统计

HTTP 有 `GET /events`，以及成本、周期、吞吐、Agent、总览、异常、负荷等统计接口。MCP 完全没有：

- `list_events` / `get_activity`
- `get_cost_stats`、`get_cycle_stats`、`get_throughput_stats`
- `get_agent_stats`、`get_overview_stats`、`get_exception_stats`、`get_load_stats`

影响：Agent 无法做目标复盘、识别阻塞、监控成本和自身 SLA，只能执行，不能运营。建议先补 `get_goal_metrics`、`get_task_metrics`、`get_agent_metrics`、`list_events`；读取类工具无需待确认。

### 成员/团队/组织配置

HTTP 暴露成员、团队、角色、能力、目标类型、目录、代码平台、通知设置、工作台等大量管理接口，MCP 都没有。这部分大多应当**故意不给 Agent**：

- 成员/团队增删、合并、邀请、角色和能力授权会改变组织边界与问责链。
- Agent 注册、吊销、所有者变更属于身份安全操作。
- 流程定义、任务类型、组织权限、计价、目录同步会改变系统规则。
- 通知渠道与组织策略属于管理员配置。

ADR 0003 只允许“管理流程”以待确认方式给 Agent；其余组织设置没有 Agent 工具是合理的。可考虑只读 `get_org_context`、`list_teams`、`list_capabilities`，帮助 Agent 规划和指派，但不开放写入。

### 待确认操作、通知

MCP 有 `list_my_proposals`，但没有 HTTP 的批准/拒绝能力，这是合理的：Agent 不能批准自己的 Proposal。`POST /notifications/read`、通知列表及偏好设置也没有 MCP 工具。

建议补 `list_my_notifications`、`mark_notifications_read`，仅处理自己的通知；组织通知策略仍归人。Proposal 的 approve/reject 必须保留网页/人工入口。

## 2. 文档与授权承诺核对

`CONTEXT.md` 与 ADR 0003 明确出现的授权包括执行任务、领取任务、验收、评论、创建子任务、创建任务、创建目标、指派、建立关联、管理流程。

覆盖情况：

- 执行任务：`claim_task`、`begin_task`、`transition_task`、`heartbeat`、`attach_artifact` 基本覆盖。
- 领取任务：`claim_task` 覆盖。
- 评论：`add_comment`、`add_note` 覆盖。
- 创建子任务：没有独立的 `create_subtask`，只能依赖 `create_task(parent_id=...)`。功能上存在，但工具层没有清晰的一等能力，授权错误和客户端发现性较差。
- 验收：没有明确的 `review` / `accept_task` / `reject_task` 工具。虽然 `transition_task` 可传 `review`、`reject` 等步骤，但 Agent 无法稳定知道自己是否是验收人，也没有专门的验收输入契约。这是最明显的承诺缺口。
- `link`：`link_tasks` 覆盖新增任务关系，但缺删除和外部链接。
- `manage_workflows`：`start_sprint`、`close_sprint` 有覆盖；任务类型/流程定义的修改没有 MCP 工具，且应继续限制为人工或待确认。

`docs/agent-integration.md` 中的八条斜杠命令并不全是 MCP 工具：`/start_task`、`/submit_task`、`/ask_question`、`/my_tasks`、`/task_detail`、`/add_note`、`/report_usage` 是 prompt 别名，分别映射到 `begin_task`、`transition_task`、`list_my_tasks` 等真实工具。这种设计可用，但文档容易让使用者误以为这些名字可直接 `tools/call`。应在文档明确标注“提示命令名≠MCP 工具名”。

## 3. 演示组织目标流程

用户描述的流程是：“支持 Agent 能够领取目标，自主规划任务，提交系统，由目标负责人审核”。

当前模型明确规定目标没有执行者、没有流程，目标负责人是人；Agent 只能领取任务。因此该流程没有完整实现：

1. Agent 不能领取目标：没有 `claim_goal`，且领域模型故意不把目标当可执行对象。
2. Agent 不能以目标为上下文自主生成一组计划并一次提交：只能逐个 `create_task`，没有 `plan_goal` / `submit_plan`。
3. 没有“目标负责人审核计划”的 Proposal 或 review 状态。
4. 现有“验收”针对任务，不是目标；里程碑由目标负责人确认，但不是 Agent 提交计划的审核流。

若产品承诺必须保留，建议新增 `propose_goal_plan(goal_id, tasks[], milestones[], rationale, idempotency_key, dry_run)`，生成待确认操作；目标负责人通过网页确认后批量创建任务/里程碑。Agent 只提交计划，不直接改变目标结构。再提供 `get_goal_plan_status` 查询状态。这样符合 ADR 0003 的 Human-in-the-loop，也不破坏“目标不可执行”的领域定义。

## 4. 按价值排序的补齐清单

1. **任务验收工具**：`review_task`（accept/reject、comment、deliverable 校验），明确验收人和待确认语义。
2. **目标计划提案流**：`propose_goal_plan` + `get_goal_plan_status`，兑现演示组织承诺。
3. **任务编辑与外部链接**：`update_task`、`add/list/remove_external_link`。
4. **目标完整读取与维护**：`get_goal`、`update_goal`、horizon/rank 工具。
5. **里程碑完整生命周期**：update/delete/unreach。
6. **统计与动态读取**：目标、任务、Agent 成本/吞吐/异常及 `list_events`。
7. **迭代创建与调整**：`create_sprint`、`update_sprint`。
8. **关联删除与通知读取**：`unlink_tasks`、`list_my_notifications`、`mark_notifications_read`。
9. **显式 `create_subtask` 别名**，减少授权与客户端发现歧义。

故意不给 Agent 的范围应继续包括成员/团队合并与邀请、角色/授权/能力配置、流程定义写入、Agent 吊销与所有者变更、组织通知策略、Proposal 裁决，以及目标/任务的不可逆删除。