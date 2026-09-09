# Agent 能力补齐清单（2026-09-08）

两份独立审计合并的结果：我方对照（对话中给出）与 Codex 的只读审计（`docs/design/sources/agent-capability-audit-codex.md`）。两边一致的结论：**任务执行主链可用；目标治理、完整编辑、验收、外部链接、运营观测缺失；"Agent 领取目标并提交拆解由负责人审核"没有任何实现。**

## 两边都指出的缺口（按价值排序）

1. **目标的读与改**：`get_goal`（单个目标的完整详情、父子链、任务、成本）、`update_goal`（标题、说明、日期、时间粒度、时间桶、信心度、成果指标、类型；负责人与上级属高风险字段，Agent 改动一律走待确认）、`achieve_goal / unachieve_goal / abandon_goal / restart_goal`（确认达成等闭环动作，走待确认）、`set_goal_horizon`、`rank_goals`。删除不给 Agent。
2. **任务编辑**：`update_task`，规则沿用 HTTP：只能改自己负责或自己创建的任务；验收人、优先级、归属目标、迭代、参与角色、仅限人工对 Agent 一律拒绝（ADR 0003 补记）。
3. **外部链接**：`list_task_links`、`add_external_link`、`remove_external_link`，与评论同一套授权（ADR 0003 第 35 条承诺过，至今只有 HTTP）。
4. **里程碑完整生命周期**：`update_milestone`、`delete_milestone`、`unreach_milestone`；删除与撤销默认待确认。
5. **看动态与统计**：`list_events`（按任务 / 目标 / 时间范围）、`get_goal_metrics`、`get_task_metrics`、`get_my_metrics`（Agent 自己的成本与用量，对照预算）。只读，不需确认。
6. **迭代**：`create_sprint`、`update_sprint`；开始 / 结束继续要求「改流程」授权且 Agent 必走待确认。
7. **关联删除**：`unlink_tasks`；删阻塞关系待确认。
8. **通知**：`list_my_notifications`、`mark_notifications_read`，只处理自己的。

## Codex 单独指出、我方遗漏的

- **验收没有清晰契约**。Agent 作为验收人时，只能靠 `transition_task` 传 `review/reject` 步骤名，既不知道自己是不是验收人，也没有专门的输入约定。补 `review_task(task, decision: accept|reject, comment, checked_deliverables[])`，内部仍走状态机，但对 Agent 是一等能力；需要「验收」授权。
- **斜杠命令名 ≠ 工具名**。`docs/agent-integration.md` 列的八条是提示别名，映射到 `begin_task`、`transition_task` 等真实工具；文档要明确标注，否则有人会拿提示名去 `tools/call`。
- **`create_subtask` 作为显式别名**：功能上 `create_task(parent_id)` 已覆盖，但授权名叫「创建子任务」、工具里却找不到同名的东西，客户端发现性差。加一个薄别名。
- **只读的组织上下文**：`list_teams`、`list_capabilities`、`get_org_context`，帮助 Agent 规划与指派，不开写入。

## 我方单独指出、Codex 未提的

- 目标详情返给 Agent 的 `list_goals` 输出含 `org_id`、`type_id` 等内部字段，应做成给 Agent 看的精简视图（与 `get_task_brief` 同一思路）。
- Agent 无法给目标写一句进展说明（目标只有汇总百分比）；可作为 `update_goal` 的一个字段或 `add_goal_note`。

## 两边一致：故意不给 Agent 的

成员 / 团队增删、合并、邀请；角色、授权、能力标签、目标类型、价格表、汇率的配置；流程定义与任务类型的修改（只能待确认，且第一版不开）；Agent 注册、吊销、所有者变更；组织通知策略、IM 集成、代码平台配置；待确认操作的裁决；目标与任务的不可逆删除。理由：这些改变的是组织边界、问责链与系统规则，不是"干活"；Agent 的有效权限是所有者 ∩ 授权，即便暴露也多半被拒，不暴露更清晰。

## 需要单独设计的：Agent 领取目标

用户目标原文：「支持 Agent 能够领取目标，自主规划任务，提交系统，由目标负责人审核」。当前领域模型明确目标没有执行者、没有流程、负责人必须是人（CONTEXT.md「目标还是任务」）。两边一致的建议是**不让目标变成可执行对象**，而是加一条"提案"流：

- `propose_goal_plan(goal, tasks[], milestones[], rationale, dry_run, idempotency_key)`：Agent 以目标为上下文提交一整套拆解（任务清单、依赖、预估、里程碑、理由），生成**一条**待确认操作整体呈现给目标负责人。
- 目标负责人在网页上看到完整方案，可整体批准、整体拒绝、或逐条勾掉再批准；批准后一次性落成任务与里程碑，任务默认指派给提案的 Agent（或负责人改派）。
- `get_goal_plan_status(proposal)` 供 Agent 查询进展。
- 「领取目标」在语义上落为：Agent 对某个目标提交了被批准的方案，并成为其中任务的负责人；目标负责人仍是人。

这条要写成 ADR 0026，涉及待确认操作的载荷形状（多对象）、审核界面（逐条勾选）、批准后的批量落库与动态。

## 实施顺序建议

第一批（补齐，一天量级）：1、2、3、4、`review_task`、`create_subtask` 别名、精简目标视图、文档标注。
第二批（观测）：5、6、7、8、只读组织上下文。
第三批（新设计）：ADR 0026 与 `propose_goal_plan` 全流程。

## 给接手会话的交接

- **做什么**：上面三批全部完成，每批做完跑 Codex 审查（`codex review --uncommitted`，不要设 `no_proxy`，模型满载时隔三分钟重试）并修到没有 P1，再全量测试。提交与推送只在用户说了之后做；提交信息不带任何 Claude 署名。
- **验证方式**：后端 `DATABASE_URL='postgres://axiomos:axiomos@localhost:5439/axiomos?sslmode=disable' AXIOMOS_ALLOW_PRIVATE_EGRESS=1 go test -count=1 ./...`；前端在 `web/` 里 `pnpm exec tsc --noEmit`、`pnpm lint`、`NEXT_PUBLIC_MOCK=0 pnpm build`。服务用 `pgrep -f bin/axiomd` 找到后按 pid 停（绝不 `pkill -f`），再 `go build -o bin/axiomd ./cmd/axiomd` 并以 `DATABASE_URL=… PLATFORM_ADMIN_EMAIL=admin@axiomos.local PLATFORM_ADMIN_PASSWORD=admin1234 AXIOMOS_ALLOW_PRIVATE_EGRESS=1 nohup ./bin/axiomd &` 启动；本地全在 localhost，所以必须带那个出网变量。
- **演示组织是用户的真实工作区**：`org_directory` 里是真实的飞书凭据，**绝不读写、绝不断开**；组织里只有用户自己的三个目标、零任务，验证时创建的东西必须删干净；用户的 Claude Code 已注册为 Agent「仲维建的 Claude Code」，十项授权全是需要人确认。
- **新工具的三条硬约束**（ADR 0025）：写工具都要接受 `dry_run` 与 `idempotency_key` 并走统一收口；拒绝理由只用现有六类句子；对象引用走 `ResolveRef`，指代不明列候选不猜。新增工具后 `internal/mcp/dryrun_test.go` 的遍历测试会要求它被覆盖或写明理由。
- **目标提案流（第三批）先写 ADR 0026 再动手**，载荷是多对象、审核界面要能逐条勾选，批准后一次性落库并产生动态。

## 进度（2026-09-08）

- 第一批：已完成并提交（`ad1a00e`）。
- 第二批：已完成并提交（`eed06c7`）。
- 第三批：ADR 0026 已写，`propose_goal_plan` / `get_goal_plan_status`、逐条勾选的确认接口与网页审核界面已实现。
