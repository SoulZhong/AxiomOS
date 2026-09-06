# AxiomOS

**中文** | [English](README.en.md)

AI 原生的组织操作系统：目标、任务、人与 Agent 在同一个系统里协同运转。可高度定制的 SaaS，同时支持私有化部署。设计与术语见 `CONTEXT.md`，架构决策见 `docs/adr/`，流程定义规格见 `docs/spec/`，接口契约见 `docs/api.md`。

## 本地运行

```
make db      # 启动 PostgreSQL（Docker，端口 5439）
make run     # 后端 :8080，启动时自动迁移、写入内置任务类型与演示数据
make web     # 前端开发服务器 :3000（生产时由后端托管 web/out）
make test    # 后端测试；设置 DATABASE_URL 时会额外跑数据库集成测试
```

演示账号：`zhong@demo.local` / `demo1234`（其他：`li`、`zhang`、`zhao@demo.local`）。首次启动会在日志里打印一个演示 Agent 的令牌。

平台后台在 `/admin/`。`make run` 默认会创建平台管理员 `admin@axiomos.local` / `admin1234`（本地开发用）；正式部署请设置环境变量：

```
PLATFORM_ADMIN_EMAIL=admin@example.com PLATFORM_ADMIN_PASSWORD=改成你的密码 make run
```

其他环境变量：`DATABASE_URL`、`ADDR`（默认 :8080）、`WEB_DIR`（默认 web/out）、`PUBLIC_URL`（生成邀请链接用，默认 http://localhost:8080）、`ALLOW_ORIGINS`、`SEED_DEMO`（默认 1）。

`AXIOMOS_ALLOW_PRIVATE_EGRESS=1`：允许服务器往内网出网。组织自己填的地址（通知 webhook 的接收地址、代码平台的接口地址、出网代理）默认只许公网 https，内网、回环、云元数据地址一律拒绝；私有化部署要接内网 GitLab 或内网的接收端时设置这个变量。云元数据地址（169.254.169.254、metadata.google.internal）任何时候都不放行。本地开发连的是 localhost，`make run` 会带上它。

界面语言：简体中文与英文，每个账号可在页脚或登录页切换；组织有默认语言；Agent 收到的工具描述和拒绝理由用所有者的语言。

把 Agent 接进来：见 [docs/agent-integration.md](docs/agent-integration.md)。MCP 端点是 `/mcp`，用 `Authorization: Bearer <Agent 令牌>`。

## 文档

- `CONTEXT.md` 词汇表：所有面向用户的名称都以它为准
- `docs/adr/` 二十条架构决策（多租户隔离、动态骨架、Agent 权限、跨角色流转、状态类型、执行记录、用语原则、技术栈、设计规范、看板与迭代、可见范围、可定制边界、角色工作台、里程碑、IM 集成、Agent 设备码授权、通知外发、外部事件与链接）
- `docs/spec/workflow-definition.md` 任务类型与流程定义规格
- `docs/api.md` HTTP 接口与 MCP 工具契约
- `web/DESIGN.md` 界面设计规范 v2（以 Linear 为蓝本，叠加 Axiom 母舰科幻层；来源见 `docs/design/sources/`）
- `docs/agent-integration.md` Agent 接入指南
- `prototypes/` 一次性验证原型

## 目录

- `cmd/axiomd` 后端唯一入口：HTTP API + MCP + 静态托管前端
- `internal/domain` 领域层：流程内核、任务、执行记录、动态（纯 Go，无数据库）
- `internal/app` 应用服务：命令与查询
- `internal/store` PostgreSQL 存储与迁移
- `internal/api` HTTP 接口
- `internal/mcp` 给 Agent 的 MCP 工具面
- `web` Next.js 前端（静态导出到 `web/out`）
- `prototypes` 一次性验证原型
