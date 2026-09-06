# 流程定义规格（任务类型与流程）

状态：v0.2 · 依据 `prototypes/workflow-engine.prototype.html` 验证结果与 ADR 0004、0005、0006 整理；v0.2 起所有 `title` 字段为多语言文本（`{"zh-CN": "...", "en-US": "..."}`，也接受纯字符串，视为 zh-CN），内核产生的拒绝理由是词条键 + 参数，由输出层按用户语言渲染。

本文规定"任务类型"与"流程"的声明格式，以及内核解释它们的规则。目标是：组织可以不写代码、只写一份声明就定义一种新任务怎么流转；Agent 只需问系统"我现在能做什么"就能继续工作。

术语以 `CONTEXT.md` 为准。本文代码块里的英文是代码标识符，界面上一律显示中文。

---

## 1. 任务类型（TaskType）

```jsonc
{
  "name": "requirement",            // 代码名，组织内唯一，小写下划线
  "title": "需求",                   // 界面名称
  "participants": {                 // 参与角色位置，可为空对象
    "designer":  { "title": "设计", "role": "designer" },
    "developer": { "title": "开发", "role": "developer" },
    "tester":    { "title": "测试", "role": "tester" },
    "releaser":  { "title": "发布", "role": "releaser" }
  },
  "task_schema":   { /* JSON Schema：任务本身的自定义字段 */ },
  "result_schema": { /* JSON Schema：提交结果时的结构化字段 */ },
  "agent_instructions": "Markdown，随任务说明原样给到 Agent",
  "workflow": { /* 见第 2 节 */ }
}
```

规则：
- `participants` 的每个位置绑定一个组织角色（`role`）。位置空缺时，流程若把负责人切到该位置，任务进入待领取任务并要求该角色。
- 系统内置 `generic`、`requirement`、`bug`、`release` 四种（第 8 节）。组织可复制后修改，也可新建。
- 任务类型与流程一起版本化（第 6 节）。

## 2. 流程（Workflow）

```jsonc
{
  "version": 3,                     // 系统分配，每次保存递增
  "initial": "draft",               // 任务创建时的状态
  "states": {
    "draft":      { "title": "草稿",   "label": "pending" },
    "todo":       { "title": "待办",   "label": "pending", "claimable": true },
    "developing": { "title": "开发中", "label": "active",  "weight": 30 },
    "waiting":    { "title": "等待答复","label": "waiting" },
    "done":       { "title": "已完成", "label": "terminal_success" },
    "cancelled":  { "title": "已取消", "label": "terminal_failure" }
  },
  "transitions": [ /* 见第 3 节 */ ]
}
```

### 2.1 状态类型（`label`）

| 代码 | 界面 | 含义 | 内核依赖 |
|---|---|---|---|
| `pending` | 未开始 | 没人在做 | 可带 `claimable: true` 表示出现在待领取任务里 |
| `active` | 进行中 | 某个阶段在进行 | 执行记录只能存在于此类状态；离开时结束执行记录 |
| `waiting` | 等待中 | 等别人（答复、验收、解除阻塞） | 通知；进度沿用进入前的权重 |
| `terminal_success` | 已完成 | 成功结束 | 进度 100；前置关系满足；周期统计终点 |
| `terminal_failure` | 已终止 | 非成功结束（取消、不修复、重复…） | 进度 0；自动解除本任务作为前置的关联 |

### 2.2 状态可选字段

- `weight`（0–100）：进度权重。无权重时：`terminal_success` = 100，`terminal_failure` = 0，其他 = 进入前最近一次有权重状态的权重，初始为 0。
- `claimable`（bool）：仅对 `pending` 有意义。
- `wip_limit`（整数，≥ 0）：在制品上限。看板上该列的任务数超过它时标红并显示「超出 n」，只提醒不拦截（ADR 0012）；0 或不填表示不限。

## 3. 步骤（Transition）

```jsonc
{
  "name": "dev_done",               // 代码名，流程内唯一
  "title": "开发完成",              // 界面名称
  "from": ["developing"],           // 起点，或 ["*"] 表示任意非终止状态
  "to": "testing",                  // 终点，或 "$previous"（见 3.4）
  "by": ["assignee"],               // 谁能触发，见 3.1
  "requires": ["artifact:pr"],      // 前提条件，见 3.2
  "grant": "execute",               // Agent 触发时需要的授权，默认 execute
  "assign_to": "participant:tester",// 走完后负责人换成谁，见 3.3
  "triggered_by": {                 // 可选：由外部事件触发，见 3.5
    "source": "git",
    "event": "pr_merged"
  }
}
```

### 3.1 `by`：谁能触发

任一规则满足即可。

| 规则 | 含义 |
|---|---|
| `creator` | 任务创建者 |
| `assignee` | 当前任务负责人 |
| `reviewer` | 验收人 |
| `participant:<位置>` | 该参与角色位置上的执行者 |
| `role:<角色>` | 持有该组织角色的成员 |
| `anyone` | 组织内任何执行者 |

Agent 触发时：任务相对身份（creator / assignee / reviewer / participant）按 **Agent 本身或其所有者** 判定；组织角色继承所有者。此外还需持有 `grant` 指定的授权（第 5 节）。

### 3.2 `requires`：前提条件

全部满足才能触发。不满足时系统返回可读原因（界面与 Agent 都能看）。

| 条件 | 含义 |
|---|---|
| `deps_done` | 所有「前置」任务处于 `terminal_success` |
| `artifact:<类型>` | 任务上已附带该类型交付物 |
| `comment` | 本次触发附带一条评论 |
| `no_open_bugs` | 没有「发现于」本任务且未结束的 Bug 类型任务 |
| `result` | 本次触发附带符合 `result_schema` 的结果 |

条件集合是内核固定的；组织不能写代码扩展。需要新条件时增补本表并升版本。

### 3.3 `assign_to`：负责人切换

| 值 | 行为 |
|---|---|
| 缺省 | 负责人不变 |
| `participant:<位置>` | 位置有人 → 负责人换成他；位置空缺 → 负责人清空，任务进入待领取任务并记录"需要角色 = 该位置的 role"、"待回填位置 = 该位置" |
| `creator` | 负责人换成创建者 |
| `null` | 负责人清空，需要角色为空（任何人可领，前提是目标状态可领取） |

### 3.4 `$previous`

`to: "$previous"` 表示回到进入当前 `waiting` 状态之前的那个 `active` 状态。任务记录 `previous_state`，仅在从 `active` 离开时更新。若无记录，该步骤不可用。

### 3.5 `triggered_by`：由外部事件触发（ADR 0020）

一步可以声明"代码平台上发生某件事时自动走它"。

```jsonc
"triggered_by": { "source": "git", "event": "pr_merged" }
```

| 字段 | 取值 |
|---|---|
| `source` | 目前只有 `git`（代码平台：GitHub / GitLab / Gitee） |
| `event` | `pr_opened` PR 打开、`pr_ready` PR 转为可评审、`pr_merged` PR 合并、`pr_closed` PR 关闭（未合并）、`ci_passed` 检查通过、`ci_failed` 检查失败 |

内核的规则：

1. 外部事件到达某个任务时，在**当前状态**里找第一条 `triggered_by` 对得上的步骤。
2. 找到了就检查它的 `requires`（第 3.2 节）与 `$previous`；**不检查 `by`，也不检查 `grant`**——外部事件既不是人也不是 Agent，`by` 说的是"谁能点这个按钮"，授权说的是 Agent 的权限，两者对它都不适用。组织想禁止某一步被外部事件触发，就不给它写 `triggered_by`。
3. 全部满足就以「外部事件」为执行者走这一步：状态、负责人切换、离开进行中时结束执行记录，全部照常；**但永远不为谁开始执行记录**（ADR 0006：执行记录只在执行者自己开始时开启），即使外部操作者绑定了本系统成员、而他正好是负责人。
4. 走成了写两条动态：`ExternalEventApplied`（一句话：「GitHub：PR #12 合并，「登录改版」进入「待验收」」）与照常的 `TaskTransitioned`（统计与燃尽图靠它回放）。
5. 任何一条不满足（当前状态没有这样的步骤、前提没满足、任务已结束）就**什么都不改**，只写一条 `ExternalEventIgnored`，里面是一句说清为什么的完整中文。
6. 外部操作者（如 GitHub 登录名）记在动态里；他绑定了本系统成员时（`external_identities`，`kind = git_user`），动态挂在那个成员名下。

## 4. 执行记录（Run）规则（ADR 0006）

1. 执行记录只能在 `active` 状态下存在，且同一任务同一时刻至多一段进行中。
2. **开始**的三种方式，且都要求执行者是负责人本人或其 Agent：
   - 触发一个进入 `active` 状态的步骤，且触发者就是（或代表）切换后的负责人；
   - 从待领取任务领取一个处于 `active` 状态的任务；
   - 显式调用「开始执行」。
3. 由他人触发的交接（例如设计者点"设计完成"把负责人切给开发）不开始执行记录。
4. 离开 `active` 状态时结束当前执行记录：目标为 `terminal_failure` 记为"已取消"，否则记为"已结束"。
5. 上报用量必须挂在进行中的执行记录上；没有则拒绝。消耗按模型分条，累计值幂等取最大。
6. 执行记录字段：`id, task_id, state, executor_id, started_at, ended_at, outcome, usage[]`。

## 5. Agent 授权与步骤

| 授权（代码） | 界面 | 覆盖的动作 |
|---|---|---|
| `execute` | 执行任务 | 触发未标注 `grant` 的步骤、开始执行、上报用量、提交结果 |
| `claim_backlog` | 领取任务 | 从待领取任务领取 |
| `review` | 验收 | 触发 `grant: review` 的步骤（验收通过 / 打回 / 验证通过） |
| `comment` | 评论 | 发评论、工作日志 |
| `create_subtask` / `create_task` / `create_goal` / `assign` / `link` | 建子任务 / 建任务 / 建目标 / 指派 / 建关联 | 对应动作 |
| `manage_workflows` | 管理流程 | 修改任务类型与流程；对 Agent 只允许"需要人确认"档 |

每项授权有 `mode: direct | with_approval`。`with_approval` 的动作生成待确认操作，人确认后由系统以 Agent 名义执行。

## 6. 版本化

- 任务类型（含流程）每次保存生成新版本号；旧版本只读保留。
- 任务在创建时记录 `task_type_version`，整个生命周期钉在该版本；不迁移。
- 校验失败的版本不能保存（第 7 节）。

## 7. 校验规则（保存时）

1. 每个状态的 `label` 必须是五种之一。
2. `initial` 必须存在且 `label` 为 `pending`。
3. 至少一个 `terminal_success` 状态。
4. 每个步骤的 `from`（除 `*`）与 `to`（除 `$previous`）必须是已定义状态。
5. `assign_to: participant:<位置>` 引用的位置必须在 `participants` 中定义。
6. `requires` 中的条件必须在 3.2 表内；`artifact:<类型>` 的类型必须在组织交付物类型表内。
7. 任一 `active` 状态必须至少有一条出边（否则任务会卡死）。
8. 步骤 `name` 在流程内唯一；状态 `name` 在流程内唯一。
9. `triggered_by` 的 `source` 必须是 `git`，`event` 必须是 3.5 表里的六个之一（不认识的报「步骤「x」的触发事件「y」系统不认识。」）。

## 8. 内置任务类型

以下为规范定义；原型中的声明与此一致。

### 8.1 通用任务 `generic`

状态：`draft(未开始) → todo(未开始,可领取) → in_progress(进行中) → submitted(等待中) → done(已完成)`；另有 `waiting(等待中)`、`blocked(等待中)`、`cancelled(已终止)`。

| 步骤 | 从 → 到 | 谁 | 前提 | 负责人 |
|---|---|---|---|---|
| ready 就绪 | draft → todo | creator | — | — |
| start 开始 | todo → in_progress | assignee | deps_done | — |
| ask_for_input 提问等待 | in_progress → waiting | assignee | comment | — |
| resume 答复并恢复 | waiting → in_progress | creator, reviewer | comment | — |
| submit 提交结果 | in_progress → submitted | assignee | artifact:result | — |
| accept 验收通过 | submitted → done | reviewer (grant review) | — | — |
| reject 验收打回 | submitted → todo | reviewer (grant review) | comment | — |
| reopen 重新打开 | done → todo | reviewer, creator | — | — |
| block / unblock | 活动态 ↔ blocked | assignee, creator | — | — |
| cancel 取消 | * → cancelled | creator, role:admin | — | — |

### 8.2 需求 `requirement`

参与角色：designer 设计、developer 开发、tester 测试、releaser 发布。

状态与权重：`draft → designing(10) → developing(30) → testing(70) → releasing(90) → awaiting_acceptance(等待中,95) → done`；另有 `waiting`、`blocked`、`cancelled`。

| 步骤 | 从 → 到 | 谁 | 前提 | 负责人切到 |
|---|---|---|---|---|
| start_design 开始设计 | draft → designing | creator | — | participant:designer |
| design_done 设计完成 | designing → developing | assignee | artifact:prd | participant:developer |
| dev_done 开发完成 | developing → testing | assignee | artifact:pr | participant:tester |
| test_fail 测试不通过 | testing → developing | assignee | comment | participant:developer |
| test_pass 测试通过 | testing → releasing | assignee | artifact:test_report, no_open_bugs | participant:releaser |
| release_done 发布完成 | releasing → awaiting_acceptance | assignee | artifact:release_note | — |
| accept 验收通过 | awaiting_acceptance → done | reviewer (review) | — | — |
| reject 验收打回 | awaiting_acceptance → developing | reviewer (review) | comment | participant:developer |
| ask_for_input / resume | 任一 active ↔ waiting | assignee / creator, reviewer | comment | resume 用 $previous |
| block / unblock / cancel | 同通用 | | | unblock 用 $previous |

**内置的外部事件触发**（ADR 0020，只在状态确实存在的地方给）：`design_done` 由 `pr_opened` 触发（PR 一开就进「开发中」；缺「需求文档」时不迁移，写一条说明）、`dev_done` 由 `pr_merged` 触发（PR 合并进「测试中」）、`block` 由 `ci_failed` 触发（检查失败进「已阻塞」）。

### 8.3 Bug `bug`

参与角色：developer 修复、tester 验证。创建者即报告人。

状态：`new(未开始) → confirmed(未开始,可领取) → fixing(进行中,40) → fixed(等待中,80) → verified(已完成)`；终止态 `rejected 不是 Bug`、`duplicate 重复`、`wont_fix 不修复`。

| 步骤 | 从 → 到 | 谁 | 前提 | 负责人切到 |
|---|---|---|---|---|
| confirm 确认 | new → confirmed | creator, role:tester | — | participant:developer |
| reject / duplicate | new, confirmed → rejected / duplicate | creator, role:tester, role:developer | comment | — |
| wont_fix 不修复 | new, confirmed, fixing → wont_fix | creator, role:admin | comment | — |
| start_fix 开始修复 | confirmed → fixing | assignee | — | — |
| fixed 修复完成 | fixing → fixed | assignee | artifact:pr | participant:tester |
| verify 验证通过 | fixed → verified | assignee, reviewer (review) | — | — |
| reopen 重新打开 | fixed, verified → fixing | assignee, reviewer, creator | comment | participant:developer |

**内置的外部事件触发**：`start_fix` 由 `pr_opened` 触发（PR 一开就进「修复中」）、`fixed` 由 `pr_merged` 触发（PR 合并进「待验证」）。Bug 流程没有「已阻塞」状态，所以不给 `ci_failed` 的映射。通用任务与发布不带触发声明，组织按需自己加。

Bug 通过「发现于」关联挂到被测任务；被测任务的流程用 `no_open_bugs` 决定是否放行。

### 8.4 发布 `release`

参与角色：releaser 发布。

状态：`draft → ready(未开始,可领取) → preparing(进行中,50) → awaiting_acceptance(等待中,90) → done`；`cancelled`。

| 步骤 | 从 → 到 | 谁 | 前提 | 负责人切到 |
|---|---|---|---|---|
| ready 就绪 | draft → ready | creator | — | participant:releaser |
| start 开始发布 | ready → preparing | assignee | deps_done | — |
| release_done 发布完成 | preparing → awaiting_acceptance | assignee | artifact:release_note | — |
| accept / reject | awaiting_acceptance → done / ready | reviewer (review) | reject 需 comment | — |
| cancel | * → cancelled | creator, role:admin | — | — |

发布要带的需求通过「前置」关联表达，不引入发布实体。

## 9. 与流程相关的其他内核规则

- **领取**：任务无负责人，且（状态可领取 或 状态为 `active`）；执行者持有所需角色；Agent 另需 `claim_backlog` 授权、能力标签包含任务要求、未达并发上限。领取后回填待回填的参与角色位置；若状态为 `active`，立即开始执行记录。
- **前置关系**：不允许成环，建立时检测并拒绝。前置任务进入 `terminal_failure` 时，自动解除它对所有后续任务的前置关系，并记录动态「自动解除前置」。
- **验收人**：默认创建者；任务类型可覆盖为目标负责人或指定角色。创建者 = 负责人时允许自验收。

## 10. 给 Agent 的查询：`get_workflow`

返回当前任务上，以调用者身份可见的全部步骤及其可用性：

```jsonc
{
  "task_id": "T1",
  "state": { "name": "developing", "title": "开发中", "label": "active" },
  "active_run": { "id": "R3", "executor_id": "li-agent" } | null,
  "transitions": [
    { "name": "dev_done", "title": "开发完成", "available": false,
      "reasons": ["缺少交付物：代码 PR"] },
    { "name": "ask_for_input", "title": "提问等待", "available": true, "requires": ["comment"] }
  ],
  "can_begin": false,
  "can_claim": false
}
```

`reasons` 是面向人的中文句子，Agent 原样展示或据此行动。

## 11. 动态（事件）清单

`TaskCreated 创建任务`、`TaskAssigned 指派负责人`、`TaskClaimed 领取任务`、`TaskSentToBacklog 进入待领取`、`TaskTransitioned 状态变化`（数据里带 `from_label` / `to_label`，燃尽图回放用）、`RunStarted 开始执行`、`RunEnded 执行结束`、`UsageReported 上报用量`、`ArtifactAttached 附上交付物`、`CommentAdded 评论`、`TasksLinked 建立关联`、`RelationRemoved 自动解除前置`。

代码平台与外部事件（ADR 0020）新增：`ExternalLinkAdded 挂上外部链接`、`ExternalLinkUpdated 外部链接状态变化`、`ExternalLinkRemoved 摘掉外部链接`、`ExternalEventApplied 外部事件推进了流程`、`ExternalEventIgnored 外部事件没有推进流程`、`CodePlatformConfigured 配置代码平台`、`CodeIdentityBound 绑定代码平台登录名`。

看板与迭代（ADR 0012）新增：`SprintCreated 创建迭代`、`SprintUpdated 修改迭代`、`SprintStarted 开始迭代`、`SprintClosed 结束迭代`、`TaskAddedToSprint 加入迭代`（数据里带当时的 `points` 与 `done`）、`TaskRemovedFromSprint 移出迭代`、`PointsChanged 修改工作量`。燃尽图完全由这些动态与 `TaskTransitioned` 回放得出，不另存快照。

## 12. 未决事项

- 组织交付物类型表的维护入口（谁能加类型）。
- `result` 条件与 `result_schema` 的校验时机（提交时校验 vs 保存草稿即校验）。
- 周期任务生成的任务如何指定参与角色（复制模板 vs 每次按角色进待领取）。
