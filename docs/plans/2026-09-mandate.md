# ADR 0028 实施计划：委托（2026-09-09）

依据：`docs/adr/0028-mandate-confirm-once.md`。目标：一个目标从分解到验收，人只出手两次（批准方案、方案验收），且每一步都能回答「谁的授权、哪次执行、改了哪个版本」。

## 顺序

每一步独立可测、可回滚；委托的判定藏在功能开关 `AXIOMOS_MANDATE=1` 后面，直到第 4 步的迁移清单全部完成再默认打开。

1. **数据与内核**
   - 迁移 `0024_mandate.sql`：`mandates` 表（RLS 照抄 proposals）；`tasks.version`、`goals.version`（默认 1）；`tasks.acceptance_mode`（`human` / `auto`，默认 `human`）、`tasks.plan_id`；`proposals.bundle_id`、`target_version`、`grants_snapshot`；`runs.mandate_id`。
   - store：所有写任务 / 目标的 SQL 统一 `version = version + 1`；`mandates` 的增查改；proposals 决定用 `SELECT … FOR UPDATE`。
   - domain：`ExecutorSystem`；`Executor.Mandate *MandateScope`；`NeedsApprovalFor(actor, task, action)` 白名单；`AutoAccept(c)`；`Revert(c, actor, eventID)`。场景测试：委托内推进不问人、验收步骤仍问、失效后回到逐项、系统执行者只能走验收步。
2. **应用层装载与迁移**
   - `executorFor(ctx, sess, taskID)` 唯一装载点；写路径迁移清单（tasks、review、codeplatform、observe、sprints、goals、milestones）逐一改成经它判定；未迁移完之前开关关闭。
   - 第一个写动作自动开执行记录（同事务）；`begin_task` 弃用为一次心跳。
3. **委托的生命周期**
   - 发放：方案批准（同事务）、人指派给 Agent（组织默认模板）、领取（组织可配）。收回。失效事件：换负责人、换目标、类型换版本、Agent 吊销、所有者停用、授权收回、到终态。
   - 动态：`MandateIssued / MandateRevoked / MandateStale`。
4. **待确认操作：捆、版本、快照**
   - 提出时记 `target_version`、`grants_snapshot`、并入同任务未答的捆；答时锁行比版本，过期置 `stale` 并通知 Agent；重放用快照，废止 `withDirectGrants`。
   - 网页「待我处理」按捆显示，逐项剔除；`/confirm` 同一份。
5. **方案：验收方式、参与角色、可达性**
   - `PlanTask` 加 `acceptance`、`participants`；预览返回每个任务的首步可达性；不可达不能批。批准时落 `acceptance_mode`、`plan_id`、委托。
6. **自动验收与方案验收**
   - 到「待验收」时若 `auto` 且要求都满足，系统执行者走验收步，记 `TaskAutoAccepted`；相关写动作后重试。
   - 方案最后一个任务到终态时生成 `plan.review` 捆（人工验收的任务逐项 + 里程碑逐项）；答完记 `PlanReviewed`。
7. **撤回**：任务页与 MCP 不给 Agent；所有者 24 小时内撤最近一步推进，版本校验，记 `TaskReverted`。
8. **接口与文案**：HTTP（委托的增查撤、撤回、方案预览）、MCP（`propose_goal_plan` 新字段、`get_workflow` / `get_task_brief` 返回委托状态、`begin_task` 弃用说明、指令更新）、网页（指派页 / 确认页改委托模板、方案预览、待我处理按捆、任务页撤回）、CONTEXT.md 新词、`docs/api.md`、`docs/agent-integration.md`。

每步做完：Codex `review --uncommitted`、修到没有 P1、全量测试，停下等确认再提交。

## 进度

- 2026-09-09：ADR 0028 起草并过 Codex 一轮；前序工作已提交（f40d4b8）。
- 2026-09-09：第 1–7 步与第 8 步的后端部分完成：迁移 0024、内核白名单 / 系统执行者 / 撤回、`executorFor` 装载点与写路径迁移、委托生命周期、待确认操作的捆 / 版本 / 快照 / 锁行、方案的验收方式 / 参与角色 / 可达性预检、自动验收与方案验收、HTTP 与 MCP 暴露、文档。端到端测试 `TestMandateTwoDecisions` 证明一个目标只要两次决定。剩第 8 步的网页：指派页 / 确认页改委托模板、方案预览显示可达性、待我处理按捆、任务页撤回与委托。
