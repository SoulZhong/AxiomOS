---
status: accepted
---

# 后端用 Go，前端用 Next.js，数据库 PostgreSQL

AxiomOS 要同时以在线服务和私有化两种形态交付（ADR 0001），私有化交付希望是"一个二进制 + 一个数据库"就能跑；Agent 接入面（MCP、长连接心跳）对并发和资源占用敏感。我们决定：后端用 Go（HTTP API、MCP 服务、流程内核、动态存储都在同一个进程 `axiomd` 里），前端用 Next.js，数据库 PostgreSQL 并启用行级安全。前端构建产物由 Go 进程静态托管，私有化部署只需分发一个二进制和数据库连接串。

## Considered Options

- **TypeScript 全栈**：一套语言、流程内核可直接从原型搬过去、与既有项目同栈；但私有化交付要带 Node 运行时，长连接与多租户下的资源占用不如 Go 可控。
- **Python + Next.js**：与部分既有项目同栈；但流程内核要重写，并发模型和私有化打包都更重。
- **Go + Next.js（选定）**：部署最轻、并发最稳；代价是团队没有 Go 积累，流程内核要按 `docs/spec/workflow-definition.md` 用 Go 重写，并用原型的六个场景做验收测试。

## Consequences

- 单一 Go 模块，`cmd/axiomd` 是唯一入口；MCP 与 HTTP API 共享同一套领域层，不允许 MCP 绕过领域层直接写库。
- 原型的六个场景转成 Go 的验收测试，作为流程内核的回归基线。
- 前端只通过 HTTP API 与后端交互，不直接连库；API 契约用 OpenAPI 描述，前端由其生成客户端。
