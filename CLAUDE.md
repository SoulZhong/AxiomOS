# AxiomOS 开发约定

## 先读什么

- `CONTEXT.md`：唯一的词汇表。界面、通知、报错、MCP 工具描述里只能出现它定义的中文名；英文只做代码标识符（ADR 0007）。
- `docs/adr/`：二十三条架构决策。改动触碰到其中任何一条时先读它；尤其 0014 定义了"什么可配、什么写死"的边界，0017 定义了外部目录（飞书、企业微信；提供方是 `internal/directory` 里的插件）作为组织架构来源时的规则。 0018–0020 定义了 Agent 设备码授权、通知外发与外部事件触发迁移；0021 定义了路线图是目标的一种看法——目标多出时间桶、信心度、成果指标三个字段，三档写死，不做成可配置词表；0022 取代了 0021 的第 3、4 条：路线图只有时间线一种排布（卡片分列已删），目标再多出时间粒度（`date_precision`，到周 / 到月 / 到季度 / 到半年 / 到年，最细到周，空等于到周）与排序权重（`rank`，泳道内的手动次序，不记动态）两个字段，条按粒度吸附到刻度画，父目标没填日期时从子目标与任务推算、推算值只在读时算不落库；0023 给目标加了一个类型（`goal_types` 是组织自己维护的词表，可改名、可加、可停用，有目标在用就只能停用不能删），类型**只做分类与显示**——不带流程、不决定权限与成本归口，唯一的附带信息是新建时预填的默认时间粒度，这条边界写死。`internal/directory` 的注册表现在带三种能力：读组织结构（`Providers()`）、发消息（`MessagingProviders()`）、代码平台（`CodeHostProviders()`，GitHub / GitLab / Gitee）；三个列表互不重叠，新增平台仍然只是新增一个文件。
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
- 出网默认只许公网：组织自己填的地址（通知 webhook、代码平台 `api_base`、出网代理）都要过 `internal/directory` 的出网守卫（`egress.go`），内网 / 回环 / 云元数据一律拒；私有化部署设 `AXIOMOS_ALLOW_PRIVATE_EGRESS=1` 才放行内网，云元数据地址任何时候都不放。保存时查一遍、连接前按解析出的地址再查一遍。
- 密钥不随便回显：代码平台的回调密钥只在显式要看的那一次返回（`?reveal=secret`，`Cache-Control: no-store` + 一条动态）；配置变更的动态只记改了哪些字段的名字，不记配置值。
- 外部事件（代码平台的 PR / CI）只能通过流程定义里的 `triggered_by` 推进状态，走的是与人和 Agent 同一段内核实现；它永远不开执行记录，也不参与 `by` 与授权判定（ADR 0020）。

## 本地开发

```
export PATH="/opt/homebrew/bin:$PATH"   # Go 在这里
make db && make run                      # Postgres :5439，后端 :8080
DATABASE_URL='postgres://axiomos:axiomos@localhost:5439/axiomos?sslmode=disable' go test ./...
```

演示账号 `zhong@demo.local` / `demo1234`。

## 提交

用户没有明确要求时不要 commit。
