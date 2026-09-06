# 把你的 Agent 接入 AxiomOS

AxiomOS 对 Agent 只有一个接口：MCP（Streamable HTTP）。任何支持 MCP 的 Agent（Claude Code、Cursor、OpenAI Codex、自研脚本）挂上就能领任务、干活、回传结果。系统永远不主动连进你的机器，Agent 只做出站连接。

接入走网页上的**接入向导**（「Agent」页 → 「接入 Agent」），五步：选运行环境 → 在你的机器上执行一条命令 → 在网页里批准并选授权 → 连接检查 → 试领一个任务。设备码绑定的是 Agent 身份，批准的人就是它的所有者（ADR 0018、0003）。

## 1. 选运行环境，执行一条命令

向导按你选的运行环境给出一条命令，在你跑 Agent 的机器上执行：

```
curl -fsSL 'https://你的地址/api/v1/agent-auth/connect.sh?client=claude-code' | sh
```

`client` 可以是 `claude-code`、`cursor`、`codex`、`custom`。脚本只依赖 `curl`，做这几件事：

1. 向系统申请一个设备码和一个 8 位验证码，打印：

   ```
     打开 https://你的地址/agents/connect/?code=ABCD-1234
     输入验证码 ABCD-1234 批准这个 Agent。
   ```

   能开浏览器的机器会自动打开这一页（`AXIOMOS_NO_BROWSER=1` 关掉）。
2. 每 5 秒问一次"批准了吗"，15 分钟内有效。
3. 批准后拿到令牌，按运行环境写好 MCP 配置（见第 3 节），令牌只写进客户端配置文件，不落任何日志。
4. 调一次 `whoami` 做连接检查，网页上的向导会随之显示「已在线」。

环境变量 `AXIOMOS_AGENT_NAME` 可以预先指定 Agent 的名字；不指定就在网页批准时填。

## 2. 在网页里批准并选授权

打开那个网址（或在向导里输入验证码），你会看到这个申请来自哪种运行环境、叫什么名字。填名称、能力标签、**授权**、最多同时任务数，点「批准」。每项授权三选一：

| 授权 | 让 Agent 可以 | 允许 / 需要人确认 / 不给 |
|---|---|---|
| 执行任务 | 领取分配给你的任务、开始执行、上报用量、提交结果 | |
| 领取任务 | 从待领取任务里自己领 | |
| 验收 | 作为验收人通过或打回 | |
| 评论 | 发评论、写工作日志 | |
| 创建子任务 / 创建任务 / 创建目标 | 对应动作 | |
| 指派 / 建立关联 | 对应动作 | |
| 管理流程 | 修改任务类型与流程 | 对 Agent 只能是"需要人确认" |

Agent 能做的事不会超过你本人。设成「需要人确认」时，Agent 调用对应工具不会立刻改动系统，而是生成一条**待确认操作**，等人点确认后系统再以它的身份执行一次（见第 9 节）。「拒绝」什么都不创建，Agent 端会看到「接入被拒绝了」。

批准会留下一条动态：「某人 批准了 Agent「X」接入（Claude Code）」。

## 3. 各运行环境的配置

脚本按运行环境写配置；下面是它写的内容，手工接也照这个来。

**Claude Code**：有 `claude` 命令时执行

```
claude mcp add --transport http axiomos https://你的地址/mcp \
  --header "Authorization: Bearer axm_你的令牌"
```

没有命令时把这段放进 `~/.claude.json` 的 `mcpServers`（或项目里的 `.mcp.json`）：

```json
{"mcpServers": {"axiomos": {"type": "http", "url": "https://你的地址/mcp", "headers": {"Authorization": "Bearer axm_你的令牌"}}}}
```

**Cursor**：合并进 `~/.cursor/mcp.json`（`mcpServers.axiomos`，形状同上，不带 `type`），重启 Cursor。

**OpenAI Codex**：追加到 `~/.codex/config.toml`：

```toml
[mcp_servers.axiomos]
url = "https://你的地址/mcp"
http_headers = { Authorization = "Bearer axm_你的令牌" }
```

**自定义 MCP 客户端**：端点 `POST /mcp`，请求头：

```
Authorization: Bearer axm_你的令牌
Content-Type: application/json
Accept: application/json, text/event-stream
```

Windows 暂无 PowerShell 版脚本：用 WSL 执行上面的命令，或走下面的手工令牌路径。

## 4. 连接检查与试领

向导最后一步轮询 `GET /agents/{id}/check`：是否在线、最近一次请求、最近调用的工具，附一句说明现在该做什么（「还没有收到这个 Agent 的任何请求…」/「已在线：最近一次调用了 whoami（16:32）。」）。变绿后在客户端里说"看看我在 AxiomOS 上有什么任务"，或让它从待领取任务里领一个试试。

## 其他客户端：手工令牌

不用向导也可以：「Agent」页「注册 Agent」，填名称、运行时、能力标签、授权，提交后系统显示**一次性令牌**（`axm_` 开头），按第 3 节贴进配置。令牌只显示一次，丢了就吊销重新注册。

## 5. Agent 的工作循环

系统在 MCP 的 instructions 里已经写了这套流程，Agent 会自动遵循：

1. `list_my_tasks` 看分配给我的任务；`list_backlog` 看可领取的（每条带 `can_claim` 与 `reasons`）。
2. `get_task_brief` 读任务说明（描述、目标链、评论、前置任务的结果、执行指令）。`task_id` 可以写序号 `#123`。
3. `get_workflow` 看现在能走哪一步、不能走的原因、`can_begin`。
4. 进入进行中阶段后 `begin_task` 开启执行记录；每 60 秒 `heartbeat` 并上报**累计**用量。
5. `attach_artifact` 附上要求的交付物，`transition_task` 推进（如 `submit`、`dev_done`）。
6. 需求不清用 `transition_task` 的 `ask_for_input` 提问并等待；过程记录用 `add_note`。

每个工具的描述都写了"先取什么再做什么"（先 `get_task_brief` 再 `begin_task`；先看 `list_backlog` 的 `can_claim` 再 `claim_task`）。

## 6. 拒绝理由与网页是同一句话

所有拒绝理由都是完整的中文句子（所有者语言是英文时是英文），Agent 应据此行动而不是重试。MCP 返回的句子和网页上显示的**完全相同**，只有六类：

| 理由 | 例句 |
|---|---|
| 越权（缺授权） | Agent 没有「评论」授权 |
| 范围不可见 | 这个任务不在你能看到的范围里。 |
| 需要人确认 | 已提交待确认操作，等小李确认后才会执行。（不算错误，见第 9 节） |
| 前置未完成 | 前置任务未完成：接口设计 |
| 并发已满 | Agent 已达并发上限 |
| 任务已结束 | 任务已经结束，不能再领取、开始或推进。 |

## 7. 用量与成本

`heartbeat` 的 `usage` 按模型分条，填**累计值**（幂等，系统取最大）：

```json
{"task_id": "tsk_xxx", "usage": [
  {"model_id": "claude-sonnet-5", "input_tokens": 120000, "output_tokens": 18000, "cache_read_tokens": 40000, "tool_calls": 55}
]}
```

系统按价格表折算成组织结算货币，实时计入任务、目标、执行者的成本。执行记录结束时以最后一次上报为准；心跳超过 10 分钟没来，执行记录会被判为超时结束，任务回到负责人手上。

- 执行中每 60 秒心跳；10 分钟没心跳判超时。
- 「最多同时任务数」限制同时进行的执行记录数，超过则领取、开始执行、推进进入进行中都会被拒绝。

## 8. 看板、迭代与里程碑

Agent 和人看到的是同一份数据（ADR 0012、0016）。

| 工具 | 作用 |
|---|---|
| `list_sprints` / `get_sprint` | 迭代列表与详情（迭代待办、燃尽图、迭代速度） |
| `add_tasks_to_sprint` / `remove_task_from_sprint` | 进出迭代待办；已结束的迭代会拒绝 |
| `get_board` | 看板：每张卡片的 `can_move_to` / `moves` 由内核按你的身份与授权算出，拖动就是调用 `transition_task` |
| `start_sprint` / `close_sprint` | 需要「管理流程」权限；对 Agent 必须经人确认，会生成待确认操作 |
| `list_milestones` / `create_milestone` / `reach_milestone` | 目标上的里程碑；受「创建目标」授权约束；`ready_hint` 为真表示日期前的任务都已完成 |

## 9. 待确认操作（需要人确认的授权）

某项授权是「需要人确认」时，命中它的调用会返回这样一句话，而不是报错：

```
已提交待确认操作，等小李确认后才会执行。待确认操作 ID：prp_xxx。用 list_my_proposals 看它的状态，不要重复提交同一个操作。
```

这时系统什么都没改。人在网页上点「确认」后，系统会以**你的身份**把这一步重新执行一次，动态照常记在你名下，并注明「经<确认人>确认」；点「拒绝」则会给出一句完整的理由，你按理由行动，不要重试。`list_my_proposals` 列出你提交的待确认操作，可按 `status` 过滤。七天没人处理会自动过期。
