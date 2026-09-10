# 目标「企业员工的 Agent 能够快速的接入到系统」的实施记录（2026-09-08）

目标方案 `prp_itnmayoc2mn2oyjz` 批准后建出 #11–#19。这里是每个任务的需求要点、改动、测试与发布说明，作为各阶段的交付物引用。核对与验证的原始记录在 `docs/design/sources/onboarding-audit-2026-09-08.md`（#11）与 `onboarding-e2e-2026-09-08.md`（#15–#18）。

## #12 补 Windows 的接入脚本（PowerShell 版 connect.sh）

**需求**：Windows 用户不必装 WSL；顺手关掉 #11 的三条脚本卡点。

**改动**
- 新增 `GET /api/v1/agent-auth/connect.ps1?client=&lang=`（`internal/api/connect.ps1.tmpl`），`irm '…/connect.ps1?client=claude-code' | iex`。与 sh 版取同一批配置常量与同一批句子（`connectScriptWith` 把两份模板收到一个渲染函数里）。Cursor 用 `ConvertFrom-Json` 就地合并，Codex 已有段落整段替换，只依赖 PowerShell 5.1+ 自带的 `Invoke-RestMethod`。
- `claude mcp add` 加 `--scope user`：写用户级配置，任何目录都能用（原来默认 local 只对当前项目目录生效）。常量只此一处（`cfgClaudeCLI`），脚本、`/connect` 说明书、`docs/agent-integration.md` 一起变。
- sh 版 Codex 分支：已有 `[mcp_servers.axiomos]` 时用 python3 整段替换，不再只把片段打印出来让人手改。
- sh 版 Claude Code 分支结尾提示「重启（或 /mcp 里重连）后生效；配置是全局的」。
- 网页向导与 `/connect/` 页的「我想自己动手」给出 macOS / Linux / WSL 与 Windows PowerShell 两条命令。

**测试**：`internal/api/onboard_test.go` 新增 PowerShell 版的渲染断言（配置常量一致、`--scope user`、未知 client 400）；sh 版三种客户端真跑一遍见 e2e 记录。PowerShell 脚本本机没有 `pwsh`，未真正执行。

**发布**：随下一次部署生效；无数据迁移。文档 `docs/api.md`、`docs/agent-integration.md` 已改。

## #13 确认页提供授权预设

**需求**：确认页上十项授权逐项三选一太慢，员工不知道选什么。

**改动**：`web/src/lib/terms.ts` 加 `GRANT_PRESETS` / `GRANT_PRESET_MODES`（开发 / 产品 / 测试 / 只读观察）与 `matchGrantPreset`；确认页（`/agents/connect/`）与「注册 Agent」抽屉的授权表上方各加一排 `Segmented`：点一个填好十项，再逐项微调；当前选择正好等于某个预设时高亮，否则显示「自定义」。预设是界面常量，只做预填，不改授权模型（ADR 0003），「管理流程」在任何预设里都不是直接生效。设计规范 `web/DESIGN.md` §21。

**测试**：`pnpm exec tsc --noEmit`、`pnpm lint`、`NEXT_PUBLIC_MOCK=0 pnpm build` 通过；确认页的预设在本机浏览器里可点、可回到自定义。

**发布**：纯前端，随 `web/out` 重建生效。

## #14 写给企业员工的接入指南（一页纸）

`/connect/` 页新增「确认时选哪个授权」与五条常见问题（验证码过期、点了拒绝、打不开确认页、公司内网、刚接完显示「执行中」）；`docs/agent-integration.md` 新增「1a. 给员工的一页纸」。全篇不出现 MCP 之类的词。

## #20 管理员推广路径（替代 #19；原任务的创建者是旧 Agent，无法开始设计）

**需求**：管理员在「Agent」页一键拿到可直接贴进飞书群的文字（接入链接 + 三步 + 授权预设建议），并在成员页看到谁还没接入 Agent。

**设计**
- 「Agent」页头部「复制给团队的接入说明」：文案来自 i18n `agents.rolloutText`，链接用 `publicBase()/connect`（任何部署下复制到的都是那个部署自己的地址），点一下进剪贴板并提示「已复制」。文案全篇不出现 MCP 之类的词，与 `/connect/` 页、`docs/agent-integration.md`「1a. 给员工的一页纸」是同一份三步说明。
- 「组织设置 · 成员与团队」筛选条「只看未接入 Agent 的 n」：`GET /org/members` 每条带 `agent_count`（名下没被吊销的 Agent 数），成员行上已接入的显示「n 个 Agent」标签。
- 不做群发：AxiomOS 不替管理员往群里发消息，只把文字准备好——发到哪个群、怎么说是人的事。

**验证（2026-09-10）**：Agent 页有「复制给团队的接入说明」按钮，点后提示「已复制」；`/connect/` 页有「接下来会发生什么」「确认时选哪个授权」「常见问题」三段；成员页筛选「只看未接入 Agent 的」正常。tsc / lint / build 通过。

**发布**：随下一次部署生效；无数据迁移。

## #19 管理员推广路径（已由 #20 替代）

**需求**：管理员一键拿到可贴进团队群的文字；看得到谁还没接入。

**改动**
- 「Agent」页头部新增「复制给团队的接入说明」：链接 + 三步 + 授权预设建议（`api.agentAuth.rolloutText()`，文案在 i18n `agents.rolloutText`）。
- `GET /org/members` 每条带 `agent_count`（名下没被吊销的 Agent 数）；成员与团队页的筛选条新增「只看未接入 Agent 的 n」，成员行上已接入的显示「n 个 Agent」标签。

**测试**：后端 `internal/app`、`internal/api` 测试通过；前端 tsc / lint / build 通过。

**发布**：随下一次部署生效；无数据迁移。
