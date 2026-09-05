# AxiomOS 开发约定

## 先读什么

- `CONTEXT.md`：唯一的词汇表。界面、通知、报错、MCP 工具描述里只能出现它定义的中文名；英文只做代码标识符（ADR 0007）。
- `docs/adr/`：十六条架构决策。改动触碰到其中任何一条时先读它；尤其 0014 定义了"什么可配、什么写死"的边界。改动触碰到其中任何一条时先读它。
- `docs/spec/workflow-definition.md`：流程定义规格，内核 `internal/domain` 的实现依据。
- `web/DESIGN.md`：界面设计规范 v2（以 Linear 为蓝本 + 克制的 Axiom 科幻层，ADR 0010）。改任何界面前先读它，新 token 与新装饰先加进规范。

## 结构与分层

- `internal/domain`：纯函数内核，不依赖数据库；所有规则变更先在这里落地并补场景测试（`engine_test.go` 六个场景是回归基线）。
- `internal/store`：PostgreSQL。组织内读写必须走 `Store.WithOrg`，它设置 `app.org_id` 并 `SET ROLE axiomos_app` 让行级安全生效。绕过它就是跨组织泄露。
- `internal/app`：应用服务。HTTP 与 MCP 只能调用这一层，不能直接碰 store。
- `internal/api`、`internal/mcp`：两个薄接口层。新增能力先加到 app，再分别暴露。
- `web/`：Next.js 静态导出，只通过 `/api/v1` 与后端交互。

## 硬规则

- 执行记录只在负责人或其 Agent 自己开始时开启（ADR 0006）；不要在状态进入时自动开。
- 内核只认状态类型（pending/active/waiting/terminal_success/terminal_failure），永远不要在代码里匹配状态名。
- 任何写操作都要产生动态（`events` 表）；没有动态的写入视为 bug。
- Agent 的有效权限 = 所有者权限 ∩ 授权；`manage_workflows` 对 Agent 只能是 `with_approval`。
- 拒绝理由必须是完整的中文句子，人和 Agent 都直接读它。

## 本地开发

```
export PATH="/opt/homebrew/bin:$PATH"   # Go 在这里
make db && make run                      # Postgres :5439，后端 :8080
DATABASE_URL='postgres://axiomos:axiomos@localhost:5439/axiomos?sslmode=disable' go test ./...
```

演示账号 `zhong@demo.local` / `demo1234`。

## 提交

用户没有明确要求时不要 commit。
