// 示例数据（NEXT_PUBLIC_MOCK=1 时使用）。内存中维护一份小世界：
// 四种内置任务类型、四个成员、四个 Agent、四个目标、十几个任务，
// 并用一段精简的流程内核算出"现在能做什么"，让动作栏的行为和真实后端一致。
// 二期：组织设置、邀请、平台后台也有一份内存实现。
// 多语言：真实后端会按请求者语言返回数据里的名字；这里用一张 zh -> en 小表在返回前整体替换（见 localize），
// 拼出来的句子（拒绝理由、动态摘要）直接走字典里的 mock.* 键。
import type {
  Agent,
  AgentInput,
  AgentRegistration,
  AgentStat,
  AdminOrganization,
  AdminOrganizationCreated,
  AdminOrganizationInput,
  AdminStats,
  AdminUser,
  Artifact,
  BacklogItem,
  BlockDef,
  BlockKey,
  BoardCard,
  BoardColumn,
  BoardData,
  BoardLane,
  Burndown,
  Capability,
  Comment,
  CostGroup,
  CostStat,
  CycleStat,
  Event,
  EventKind,
  ExchangeRate,
  ExecutorRef,
  GanttData,
  GanttGroup,
  GanttRow,
  GanttTask,
  Goal,
  GoalInput,
  Grant,
  GrantName,
  ID,
  Invitation,
  InvitationInfo,
  LocalizedTitle,
  Member,
  Milestone,
  MilestoneInput,
  MilestoneMark,
  MilestoneSummary,
  OrgInfo,
  OrgMember,
  OrgPricing,
  OrgRole,
  OrgSettings,
  OrgTeam,
  VisibilityPreview,
  Organization,
  Preset,
  RoleWorkspace,
  Workspace,
  WorkspaceCatalog,
  OverviewData,
  OverviewPeriod,
  OverviewUnit,
  ExceptionGoal,
  ExceptionTask,
  ExceptionsData,
  LoadRow,
  ScopeOption,
  TrendPoint,
  PriceInput,
  PriceModel,
  Proposal,
  ProposalApproval,
  ProposalCount,
  ProposalStatus,
  ProposalTarget,
  Relation,
  RelationType,
  Run,
  Session,
  Sprint,
  SprintCloseInput,
  SprintDetail,
  SprintInput,
  SprintStatus,
  StateLabel,
  Task,
  TaskInput,
  TaskQuery,
  TaskRef,
  TaskState,
  TaskType,
  Team,
  ThroughputStat,
  TransitionAvailability,
  TransitionBody,
  TransitionDef,
  Usage,
  VelocityData,
  WorkflowAvailability,
  Inbox,
  InboxCount,
  InboxGroup,
  InboxKind,
  InboxQuestion,
  InboxTask,
  Notification,
} from "./api";
import { ApiError, BLOCK_KEYS, INBOX_KINDS, isBlockKey } from "./api";
import { addDays, diffDays, parseDate, startOfWeek, toISODate, today } from "./format";
import { getLocale, normalizeLocale, t, type Locale } from "./i18n";

// ---------- 时间助手 ----------
const T0 = today();
const day = (offset: number) => toISODate(addDays(T0, offset));
const at = (offset: number, hour = 9, minute = 0) => {
  const d = addDays(T0, offset);
  d.setHours(hour, minute, 0, 0);
  return d.toISOString();
};
const nowISO = () => new Date().toISOString();
let seq = 100;
const nextId = (prefix: string) => `${prefix}${++seq}`;

// ---------- 服务端名字的英文（模拟后端按语言输出） ----------
const EN: Record<string, string> = {
  // 组织、团队、角色、能力、交付物类型
  "示例科技": "Example Tech", "产品研发部": "Product R&D", "前端组": "Frontend", "测试组": "QA",
  "产品": "Product", "设计": "Design", "开发": "Development", "测试": "Testing", "发布": "Release", "管理员": "Admin",
  "编码": "Coding", "写作": "Writing", "数据分析": "Data analysis",
  "需求文档": "Requirements doc", "代码 PR": "Pull request", "测试报告": "Test report", "发布记录": "Release note", "结果摘要": "Result summary", "文档": "Document", "报表": "Report",
  // 任务类型、状态、步骤、参与角色
  "通用任务": "Generic task", "需求": "Requirement", "修复": "Fix", "验证": "Verify",
  "草稿": "Draft", "待办": "To do", "进行中": "In progress", "等待答复": "Waiting for reply", "已阻塞": "Blocked", "待验收": "Awaiting acceptance", "已完成": "Done", "已取消": "Cancelled",
  "设计中": "Designing", "开发中": "Developing", "测试中": "Testing", "发布中": "Releasing", "已上线": "Live",
  "新建": "New", "已确认": "Confirmed", "修复中": "Fixing", "待验证": "To verify", "已验证": "Verified", "不是 Bug": "Not a bug", "重复": "Duplicate", "不修复": "Won't fix",
  "就绪": "Ready", "已发布": "Released",
  "开始": "Start", "提问等待": "Ask and wait", "答复并恢复": "Reply and resume", "提交结果": "Submit result", "验收通过": "Accept", "验收打回": "Reject", "重新打开": "Reopen", "解除阻塞": "Unblock", "标记阻塞": "Mark blocked", "取消": "Cancel",
  "开始设计": "Start design", "设计完成": "Design done", "开发完成": "Development done", "测试不通过": "Test failed", "测试通过": "Test passed", "发布完成": "Release done",
  "确认": "Confirm", "判定非 Bug": "Mark not a bug", "判定重复": "Mark duplicate", "开始修复": "Start fixing", "修复完成": "Fix done", "验证通过": "Verified", "开始发布": "Start release",
  "背景": "Background", "复现步骤": "Steps to reproduce", "严重程度": "Severity", "版本号": "Version",
  "按任务描述完成工作，把成果整理成一份「结果摘要」附在任务上后再提交。": "Do the work as described, attach a result summary to the task, then submit.",
  "开发阶段：阅读需求文档，在仓库里实现并提交 PR；不确定的地方用评论提问，不要猜。": "Development stage: read the requirements doc, implement in the repository and open a PR; ask in a comment when unsure instead of guessing.",
  "先复现，再定位根因；修复要附带回归测试并提交 PR。": "Reproduce first, then find the root cause; the fix must include a regression test and a PR.",
  "确认所有前置需求已上线，整理发布记录后再执行发布。": "Confirm every predecessor requirement is live, write the release note, then release.",
  // 工作台：区块与预设（ADR 0015）
  "组织概览摘要": "Organization overview summary", "要我关注的异常": "Exceptions for me", "我的任务": "My tasks", "等我验收": "Awaiting my review", "人员与 Agent 负荷": "People and agent load", "成本与预算": "Cost and budget", "待确认操作": "Pending confirmations", "最近动态": "Recent activity", "当前迭代": "Current sprint", "待领取任务": "Unclaimed tasks", "趋势与环比": "Trend and comparison",
  "当前范围这一个月的目标进度、任务、逾期、吞吐与成本合计。": "This month's goal progress, tasks, overdue, throughput and cost for the current scope.",
  "逾期与停滞的任务、逾期或超预算的目标、等我确认的操作。": "Overdue and stalled tasks, overdue or over-budget goals, actions waiting for my confirmation.",
  "我和我的 Agent 名下未结束的任务。": "Open tasks owned by me and my agents.",
  "我是验收人、正在等我处理的任务。": "Tasks where I am the reviewer and they are waiting on me.",
  "每个人和 Agent 手上有多少活、有没有逾期。": "How much each person and agent has on hand, and whether anything is overdue.",
  "这一个月的成本、预算执行率，以及成本最高的目标。": "This month's cost, budget usage, and the most expensive goals.",
  "Agent 发起、等我确认后才执行的动作。": "Actions started by agents that run only after I confirm.",
  "当前范围里最近发生了什么。": "What happened recently in the current scope.",
  "进行中的迭代：进度、剩余天数与燃尽图。": "The active sprint: progress, days left and burndown.",
  "现在我能领的任务。": "Tasks I can claim right now.",
  "成本与吞吐随时间的变化，与上一段时间对比。": "Cost and throughput over time, compared with the previous period.",
  "全局视角": "Global view", "部门视角": "Department view", "小组视角": "Team view", "执行视角": "Doer view", "运营视角": "Operations view",
  "看全公司：概览、异常、成本、负荷与趋势。": "The whole company: overview, exceptions, cost, load and trend.",
  "看本部：概览、异常、负荷、成本，加上等我验收的。": "Your department: overview, exceptions, load, cost, plus what awaits your review.",
  "带小组：异常、负荷、当前迭代、验收与待领取。": "Leading a team: exceptions, load, current sprint, review and unclaimed tasks.",
  "干活：我的任务、等我验收、待领取、当前迭代与动态。": "Getting work done: my tasks, my review, unclaimed tasks, current sprint and activity.",
  "看经营：成本、趋势、负荷与异常。": "Running the business: cost, trend, load and exceptions.",
  // 目标、任务标题（让英文示例看起来自然）
  "Q3 提升用户留存": "Q3: improve retention", "登录体验改版": "Login experience redesign", "支付转化优化": "Checkout conversion", "内部效率工具": "Internal productivity tools",
  "登录页改版": "Login page redesign", "金额显示错位": "Amount misaligned", "写周报": "Write weekly report", "整理客户名单": "Clean up customer list", "v2.0 发布": "v2.0 release",
  "导出报表": "Export reports", "写月度总结": "Write monthly summary", "客户访谈纪要": "Customer interview notes", "导出 CSV 乱码": "CSV export garbled", "数据看板原型": "Overview prototype",
  "支付页优化": "Checkout page optimization", "周报模板整理": "Weekly report template", "留存漏斗分析": "Retention funnel analysis",
  "接入短信验证码": "Wire up SMS verification", "登录页埋点": "Login page analytics events", "验证码倒计时不归零": "Countdown never resets", "报表导出加权限": "Permissions for report export",
  "第 5 次迭代": "Sprint 5", "第 6 次迭代": "Sprint 6", "第 7 次迭代": "Sprint 7", "第 8 次迭代": "Sprint 8",
  "打通登录改版的开发与测试，上线一键登录。": "Finish development and testing of the login redesign; ship one-tap login.",
  "支付页两步走通，报表导出可用。": "Two-step checkout working; report export usable.",
  "补齐周报、月报的自动化。": "Automate the weekly and monthly reports.",
  "留存漏斗与访谈纪要。": "Retention funnel and interview notes.",
  "把次月留存从 31% 提到 38%。": "Raise next-month retention from 31% to 38%.", "减少登录流失，支持一键登录。": "Reduce login drop-off; support one-tap login.", "支付页改版与报表导出。": "Checkout redesign and report export.", "周报、月报、看板自动化。": "Automate weekly/monthly reports and dashboards.",
  "小李的编码 Agent": "Li's coding agent", "小张的测试 Agent": "Zhang's testing agent", "小王的写作助手": "Wang's writing assistant", "公共文档助手": "Shared docs assistant",
  "小王": "Wang", "小李": "Li", "小张": "Zhang", "小赵": "Zhao", "小钱": "Qian",
  "登录页改版需求文档": "Login redesign requirements", "报表导出需求": "Report export requirements", "8 月总结": "August summary", "留存漏斗报告": "Retention funnel report",
  // 待确认操作
  "创建任务": "Create task", "领取任务": "Claim task", "验收": "Review",
  "确认后会在目标「登录体验改版」下创建任务「补充登录失败的埋点」，并放进待领取任务等人来领。":
    "Confirming creates the task \u201cAdd tracking for failed logins\u201d under the goal \u201cLogin experience redesign\u201d and leaves it unclaimed for someone to pick up.",
  "确认后公共文档助手会领取任务「导出报表」，并立刻开始执行。":
    "Confirming lets the shared writing agent claim the task \u201cExport reports\u201d and start on it right away.",
  "确认后小李的编码 Agent 会验收任务「留存漏斗分析」的结果，任务将进入已完成。":
    "Confirming lets Li\u2019s coding agent accept the result of \u201cRetention funnel analysis\u201d, which moves the task to done.",
  "确认后会在目标「内部效率工具」下创建任务「整理上月周报模板」，并指派给小王的写作助手。":
    "Confirming creates the task \u201cTidy up last month\u2019s weekly report template\u201d under the goal \u201cInternal tooling\u201d and assigns it to Wang\u2019s writing agent.",
  "确认后小李的编码 Agent 会验收任务「登录页改版」的结果，任务将进入待发布。":
    "Confirming lets Li\u2019s coding agent accept the result of \u201cLogin page redesign\u201d, which moves the task to ready for release.",
  "确认后公共文档助手会领取任务「整理客户名单」，并立刻开始执行。":
    "Confirming lets the shared writing agent claim the task \u201cTidy up the customer list\u201d and start on it right away.",
  "补充登录失败的埋点": "Add tracking for failed logins",
  "把登录失败的原因、次数、耗时都记下来，为下一步优化留数据。": "Record why logins fail, how often, and how long they take, so the next round of work has data.",
  "我可以先把导出格式的说明写完。": "I can start by writing up the export format.",
  "报告里的口径和上次一致，可以收。": "The report uses the same definitions as last time, so it can be accepted.",
  "整理上月周报模板": "Tidy up last month\u2019s weekly report template",
  "测试报告还没上传，先补齐再验收。": "The test report hasn\u2019t been uploaded yet; add it before this is reviewed.",
  // 种子动态
  "创建目标「Q3 提升用户留存」": "Created goal \"Q3: improve retention\"",
  "在「Q3 提升用户留存」下创建目标「登录体验改版」": "Created goal \"Login experience redesign\" under \"Q3: improve retention\"",
  "创建需求「登录页改版」": "Created requirement \"Login page redesign\"",
  "开始设计：草稿 → 设计中": "Start design: Draft → Designing",
  "负责人切到参与角色·设计：小王": "Assignee switched to participant · Design: Wang",
  "附上需求文档「登录页改版需求文档」": "Attached requirements doc \"Login redesign requirements\"",
  "设计完成：设计中 → 开发中": "Design done: Designing → Developing",
  "负责人切到参与角色·开发：小李": "Assignee switched to participant · Development: Li",
  "小李的编码 Agent 开始执行（开发中）": "Li's coding agent started working (Developing)",
  "上报用量：claude-sonnet-5 1.2 万 token": "Usage reported: claude-sonnet-5 12K tokens",
  "评论：主按钮用品牌蓝还是深灰？": "Comment: brand blue or dark grey for the primary button?",
  "提问等待：开发中 → 等待答复": "Ask and wait: Developing → Waiting for reply",
  "执行记录结束（已结束）": "Execution record ended (Ended)",
  "评论：用品牌蓝，深灰那处是笔误。": "Comment: brand blue; the dark grey was a typo.",
  "答复并恢复：等待答复 → 开发中": "Reply and resume: Waiting for reply → Developing",
  "创建 Bug「金额显示错位」": "Created bug \"Amount misaligned\"",
  "建立关联：「金额显示错位」发现于「登录页改版」": "Linked: \"Amount misaligned\" found in \"Login page redesign\"",
  "确认：新建 → 已确认": "Confirm: New → Confirmed",
  "负责人切到参与角色·修复：小李": "Assignee switched to participant · Fix: Li",
  "工作日志：已生成 LoginForm 组件与手机号校验": "Work note: generated LoginForm component and phone validation",
  "上报用量：claude-sonnet-5 1.8 万 token，claude-opus-5 4000 token": "Usage reported: claude-sonnet-5 18K tokens, claude-opus-5 4K tokens",
  "开发完成后测试角色空缺，进入待领取任务（要求角色：测试）": "Testing slot vacant after development; sent to unclaimed tasks (requires role: Testing)",
  "取消：待办 → 已取消": "Cancel: To do → Cancelled",
  "提交结果：进行中 → 待验收": "Submit result: In progress → Awaiting acceptance",
  // 范围与组织概览（ADR 0013）用到的新名字
  "产品事业部": "Product division", "服务事业部": "Service division", "运营组": "Operations", "客户支持组": "Customer support",
  "小周": "Zhou", "小孙": "Sun", "小陈": "Chen", "运营": "Operations",
  "小周的客服助手": "Zhou's support assistant",
  "客户满意度提升": "Raise customer satisfaction", "把满意度从 82 分提到 90 分。": "Raise the satisfaction score from 82 to 90.",
  "回访流失客户": "Call back churned customers", "整理常见问题": "Tidy up the FAQ", "客服话术更新": "Update the support scripts", "月度满意度报表": "Monthly satisfaction report",
};
/** 单个名字按当前语言输出（拼句子时用）。 */
const L = (s: string) => (getLocale() === "en-US" ? (EN[s] ?? s) : s);
/** 整个返回值按当前语言输出：把表里有的中文字符串换成英文，其余原样。 */
function localize<T>(v: T): T {
  if (getLocale() !== "en-US") return v;
  const walk = (x: unknown): unknown => {
    if (typeof x === "string") return EN[x] ?? x;
    if (Array.isArray(x)) return x.map(walk);
    if (x && typeof x === "object") return Object.fromEntries(Object.entries(x as Record<string, unknown>).map(([k, val]) => [k, walk(val)]));
    return x;
  };
  return walk(v) as T;
}
/** 多语言标题输入 -> 存储的中文名（示例里以中文为主键，英文写进 EN 表）。 */
function storeTitle(input: LocalizedTitle, current?: string): string {
  if (typeof input === "string") {
    if (getLocale() === "en-US") {
      const zh = current ?? input;
      EN[zh] = input;
      return zh;
    }
    if (current && EN[current] && !EN[input]) EN[input] = EN[current];
    return input;
  }
  const zh = input["zh-CN"]?.trim() || current || input["en-US"]?.trim() || "";
  if (input["en-US"]?.trim()) EN[zh] = input["en-US"].trim();
  else if (current && EN[current] && zh !== current) EN[zh] = EN[current];
  return zh;
}

// ---------- 任务类型（spec §8） ----------
const S = (name: string, label: StateLabel, title: string, extra: Partial<TaskState> = {}): TaskState => ({ name, label, title, ...extra });
const states = (...list: TaskState[]): Record<string, TaskState> => Object.fromEntries(list.map((s) => [s.name, s]));
const T = (name: string, title: string, from: string | string[], to: string, by: string | string[], extra: Partial<TransitionDef> = {}): TransitionDef => ({
  name,
  title,
  from: ([] as string[]).concat(from),
  to,
  by: ([] as string[]).concat(by),
  requires: [],
  ...extra,
});
const commonTail = (active: string[]): TransitionDef[] => [
  T("block", "标记阻塞", active, "blocked", ["assignee", "creator"]),
  T("cancel", "取消", "*", "cancelled", ["creator", "role:admin"]),
];

const TASK_TYPES: Record<string, TaskType> = {
  generic: {
    name: "generic",
    title: "通用任务",
    builtin: true,
    participants: {},
    task_schema: null,
    result_schema: null,
    agent_instructions: "按任务描述完成工作，把成果整理成一份「结果摘要」附在任务上后再提交。",
    workflow: {
      version: 1,
      initial: "draft",
      states: states(
        S("draft", "pending", "草稿"),
        S("todo", "pending", "待办", { claimable: true }),
        S("in_progress", "active", "进行中", { wip_limit: 1 }),
        S("waiting", "waiting", "等待答复"),
        S("blocked", "waiting", "已阻塞"),
        S("submitted", "waiting", "待验收", { wip_limit: 2 }),
        S("done", "terminal_success", "已完成"),
        S("cancelled", "terminal_failure", "已取消"),
      ),
      transitions: [
        T("ready", "就绪", "draft", "todo", "creator"),
        T("start", "开始", "todo", "in_progress", "assignee", { requires: ["deps_done"] }),
        T("ask_for_input", "提问等待", "in_progress", "waiting", "assignee", { requires: ["comment"] }),
        T("resume", "答复并恢复", "waiting", "in_progress", ["creator", "reviewer"], { requires: ["comment"] }),
        T("submit", "提交结果", "in_progress", "submitted", "assignee", { requires: ["artifact:result"] }),
        T("accept", "验收通过", "submitted", "done", "reviewer", { grant: "review" }),
        T("reject", "验收打回", "submitted", "todo", "reviewer", { requires: ["comment"], grant: "review" }),
        T("reopen", "重新打开", "done", "todo", ["reviewer", "creator"]),
        T("unblock", "解除阻塞", "blocked", "todo", ["assignee", "creator"]),
        ...commonTail(["todo", "in_progress", "waiting"]),
      ],
    },
  },
  requirement: {
    name: "requirement",
    title: "需求",
    builtin: true,
    participants: {
      designer: { title: "设计", role: "designer" },
      developer: { title: "开发", role: "developer" },
      tester: { title: "测试", role: "tester" },
      releaser: { title: "发布", role: "releaser" },
    },
    task_schema: { type: "object", properties: { background: { type: "string", title: "背景" } } },
    result_schema: null,
    agent_instructions: "开发阶段：阅读需求文档，在仓库里实现并提交 PR；不确定的地方用评论提问，不要猜。",
    workflow: {
      version: 2,
      initial: "draft",
      states: states(
        S("draft", "pending", "草稿"),
        S("designing", "active", "设计中", { weight: 10 }),
        S("developing", "active", "开发中", { weight: 30, wip_limit: 2 }),
        S("testing", "active", "测试中", { weight: 70 }),
        S("releasing", "active", "发布中", { weight: 90 }),
        S("awaiting_acceptance", "waiting", "待验收", { weight: 95 }),
        S("waiting", "waiting", "等待答复"),
        S("blocked", "waiting", "已阻塞"),
        S("done", "terminal_success", "已上线"),
        S("cancelled", "terminal_failure", "已取消"),
      ),
      transitions: [
        T("start_design", "开始设计", "draft", "designing", "creator", { assign_to: "participant:designer" }),
        T("design_done", "设计完成", "designing", "developing", "assignee", { requires: ["artifact:prd"], assign_to: "participant:developer" }),
        T("dev_done", "开发完成", "developing", "testing", "assignee", { requires: ["artifact:pr"], assign_to: "participant:tester" }),
        T("test_fail", "测试不通过", "testing", "developing", "assignee", { requires: ["comment"], assign_to: "participant:developer" }),
        T("test_pass", "测试通过", "testing", "releasing", "assignee", { requires: ["artifact:test_report", "no_open_bugs"], assign_to: "participant:releaser" }),
        T("release_done", "发布完成", "releasing", "awaiting_acceptance", "assignee", { requires: ["artifact:release_note"] }),
        T("accept", "验收通过", "awaiting_acceptance", "done", "reviewer", { grant: "review" }),
        T("reject", "验收打回", "awaiting_acceptance", "developing", "reviewer", { requires: ["comment"], grant: "review", assign_to: "participant:developer" }),
        T("ask_for_input", "提问等待", ["designing", "developing", "testing", "releasing"], "waiting", "assignee", { requires: ["comment"] }),
        T("resume", "答复并恢复", "waiting", "$previous", ["creator", "reviewer"], { requires: ["comment"] }),
        ...commonTail(["designing", "developing", "testing", "releasing"]),
        T("unblock", "解除阻塞", "blocked", "$previous", ["assignee", "creator"]),
      ],
    },
  },
  bug: {
    name: "bug",
    title: "Bug",
    builtin: true,
    participants: {
      developer: { title: "修复", role: "developer" },
      tester: { title: "验证", role: "tester" },
    },
    task_schema: { type: "object", properties: { steps: { type: "string", title: "复现步骤" }, severity: { type: "string", title: "严重程度" } } },
    result_schema: null,
    agent_instructions: "先复现，再定位根因；修复要附带回归测试并提交 PR。",
    workflow: {
      version: 1,
      initial: "new",
      states: states(
        S("new", "pending", "新建"),
        S("confirmed", "pending", "已确认", { claimable: true }),
        S("fixing", "active", "修复中", { weight: 40 }),
        S("fixed", "waiting", "待验证", { weight: 80 }),
        S("verified", "terminal_success", "已验证"),
        S("rejected", "terminal_failure", "不是 Bug"),
        S("duplicate", "terminal_failure", "重复"),
        S("wont_fix", "terminal_failure", "不修复"),
      ),
      transitions: [
        T("confirm", "确认", "new", "confirmed", ["creator", "role:tester"], { assign_to: "participant:developer" }),
        T("reject", "判定非 Bug", ["new", "confirmed"], "rejected", ["creator", "role:tester", "role:developer"], { requires: ["comment"] }),
        T("duplicate", "判定重复", ["new", "confirmed"], "duplicate", ["creator", "role:tester", "role:developer"], { requires: ["comment"] }),
        T("wont_fix", "不修复", ["new", "confirmed", "fixing"], "wont_fix", ["creator", "role:admin"], { requires: ["comment"] }),
        T("start_fix", "开始修复", "confirmed", "fixing", "assignee"),
        T("fixed", "修复完成", "fixing", "fixed", "assignee", { requires: ["artifact:pr"], assign_to: "participant:tester" }),
        T("verify", "验证通过", "fixed", "verified", ["assignee", "reviewer"], { grant: "review" }),
        T("reopen", "重新打开", ["fixed", "verified"], "fixing", ["assignee", "reviewer", "creator"], { requires: ["comment"], assign_to: "participant:developer" }),
      ],
    },
  },
  release: {
    name: "release",
    title: "发布",
    builtin: true,
    participants: { releaser: { title: "发布", role: "releaser" } },
    task_schema: { type: "object", properties: { version: { type: "string", title: "版本号" } } },
    result_schema: null,
    agent_instructions: "确认所有前置需求已上线，整理发布记录后再执行发布。",
    workflow: {
      version: 1,
      initial: "draft",
      states: states(
        S("draft", "pending", "草稿"),
        S("ready", "pending", "就绪", { claimable: true }),
        S("preparing", "active", "发布中", { weight: 50 }),
        S("awaiting_acceptance", "waiting", "待验收", { weight: 90 }),
        S("done", "terminal_success", "已发布"),
        S("cancelled", "terminal_failure", "已取消"),
      ),
      transitions: [
        T("ready", "就绪", "draft", "ready", "creator", { assign_to: "participant:releaser" }),
        T("start", "开始发布", "ready", "preparing", "assignee", { requires: ["deps_done"] }),
        T("release_done", "发布完成", "preparing", "awaiting_acceptance", "assignee", { requires: ["artifact:release_note"] }),
        T("accept", "验收通过", "awaiting_acceptance", "done", "reviewer", { grant: "review" }),
        T("reject", "验收打回", "awaiting_acceptance", "ready", "reviewer", { requires: ["comment"], grant: "review" }),
        T("cancel", "取消", "*", "cancelled", ["creator", "role:admin"]),
      ],
    },
  },
};

// ---------- 组织、成员、团队、Agent ----------
const ORG: Organization & { slug: string } = { id: "org1", slug: "example", name: "示例科技", owner_id: "wang", currency: "CNY", default_locale: "zh-CN", created_at: at(-120) };
const ROLES: OrgRole[] = [
  { name: "pm", title: "产品", builtin: true, permissions: [] },
  { name: "designer", title: "设计", builtin: true, permissions: [] },
  { name: "developer", title: "开发", builtin: true, permissions: [] },
  { name: "tester", title: "测试", builtin: true, permissions: [] },
  { name: "releaser", title: "发布", builtin: true, permissions: [] },
  { name: "admin", title: "管理员", builtin: true, permissions: ["org_settings", "manage_workflows", "cancel_any_task"] },
  { name: "ops", title: "运营", builtin: true, permissions: ["view_all_work", "view_all_cost"] },
];
/** 各语言的名称（真实后端在 roles / capabilities 上带 titles） */
const titlesOf = (zh: string): Partial<Record<Locale, string>> => ({ "zh-CN": zh, "en-US": EN[zh] ?? zh });
const viewRole = (r: OrgRole): OrgRole => ({ ...r, titles: titlesOf(r.title) });
const viewCapability = (c: Capability): Capability => ({ ...c, titles: titlesOf(c.title) });
const roleOf = (name: string) => ROLES.find((r) => r.name === name);
// 团队树（示例数据）：两个事业部各带两个组；产品事业部被标成共享边界，
// 于是「按共享边界」这条策略在界面上能直接看出差别（ADR 0013）。
const TEAMS: OrgTeam[] = [
  { id: "team-rd", name: "产品事业部", lead_id: "wang", parent_id: null, member_ids: ["wang"], is_boundary: true },
  { id: "team-fe", name: "前端组", lead_id: "li", parent_id: "team-rd", member_ids: ["li", "zhao"] },
  { id: "team-qa", name: "测试组", lead_id: "zhang", parent_id: "team-rd", member_ids: ["zhang"] },
  { id: "team-svc", name: "服务事业部", lead_id: "zhou", parent_id: null, member_ids: ["zhou"] },
  { id: "team-ops", name: "运营组", lead_id: "sun", parent_id: "team-svc", member_ids: ["sun"] },
  { id: "team-cs", name: "客户支持组", lead_id: "chen", parent_id: "team-svc", member_ids: ["chen"] },
];
interface MemberRow extends Member { active: boolean; password?: string }
const MEMBERS: Record<ID, MemberRow> = {
  wang: { id: "wang", name: "小王", email: "wang@example.com", roles: ["pm", "designer", "admin"], team_id: "team-rd", locale: "zh-CN", active: true, created_at: at(-120) },
  li: { id: "li", name: "小李", email: "li@example.com", roles: ["developer"], team_id: "team-fe", locale: "zh-CN", active: true, created_at: at(-110) },
  zhang: { id: "zhang", name: "小张", email: "zhang@example.com", roles: ["tester"], team_id: "team-qa", locale: "en-US", active: true, created_at: at(-100) },
  zhao: { id: "zhao", name: "小赵", email: "zhao@example.com", roles: ["releaser", "developer"], team_id: "team-fe", locale: "zh-CN", active: true, created_at: at(-90) },
  zhou: { id: "zhou", name: "小周", email: "zhou@example.com", roles: ["pm"], team_id: "team-svc", locale: "zh-CN", active: true, created_at: at(-80) },
  sun: { id: "sun", name: "小孙", email: "sun@example.com", roles: ["pm", "ops"], team_id: "team-ops", locale: "zh-CN", active: true, created_at: at(-70) },
  chen: { id: "chen", name: "小陈", email: "chen@example.com", roles: ["tester"], team_id: "team-cs", locale: "zh-CN", active: true, created_at: at(-65) },
};
const ARTIFACT_TYPES: Record<string, string> = { prd: "需求文档", pr: "代码 PR", test_report: "测试报告", release_note: "发布记录", result: "结果摘要", doc: "文档", report: "报表" };
const CAPABILITIES: Capability[] = [
  { name: "coding", title: "编码" },
  { name: "testing", title: "测试" },
  { name: "writing", title: "写作" },
  { name: "data_analysis", title: "数据分析" },
  { name: "design", title: "设计" },
];

const g = (name: Grant["name"], mode: Grant["mode"] = "direct"): Grant => ({ name, mode });
const AGENTS: Record<ID, Agent> = {
  "li-agent": {
    id: "li-agent", name: "小李的编码 Agent", owner: ref("li"), shared: false, capabilities: ["coding"],
    grants: [g("execute"), g("claim_backlog"), g("comment"), g("link"), g("review", "with_approval")],
    online: true, last_seen_at: nowISO(), max_concurrency: 2, created_at: at(-60),
  },
  "zhang-agent": {
    id: "zhang-agent", name: "小张的测试 Agent", owner: ref("zhang"), shared: false, capabilities: ["testing", "coding"],
    grants: [g("execute"), g("claim_backlog"), g("review"), g("comment")],
    online: true, last_seen_at: at(0, 8, 40), max_concurrency: 1, created_at: at(-45),
  },
  "wang-agent": {
    id: "wang-agent", name: "小王的写作助手", owner: ref("wang"), shared: false, capabilities: ["writing", "data_analysis"],
    grants: [g("execute"), g("comment"), g("create_task", "with_approval")],
    online: false, last_seen_at: at(-1, 18), max_concurrency: 1, created_at: at(-30),
  },
  "zhou-agent": {
    id: "zhou-agent", name: "小周的客服助手", owner: ref("zhou"), shared: false, capabilities: ["writing", "data_analysis"],
    grants: [g("execute"), g("comment")],
    online: true, last_seen_at: nowISO(), max_concurrency: 2, created_at: at(-25),
  },
  "shared-doc": {
    id: "shared-doc", name: "公共文档助手", owner: ref("wang"), shared: true, capabilities: ["writing"],
    grants: [g("execute"), g("comment"), g("claim_backlog", "with_approval")],
    online: true, last_seen_at: nowISO(), max_concurrency: 3, created_at: at(-20),
  },
};

function ref(id: ID): ExecutorRef {
  const m = MEMBERS[id];
  if (m) return { id, kind: "member", name: m.name };
  const a = AGENTS[id];
  if (a) return { id, kind: "agent", name: a.name, owner_id: a.owner.id };
  return { id, kind: "member", name: id };
}

let ME: ID = "wang";
let loggedIn = true;

// ---------- 内部存储 ----------
interface GoalRow {
  id: ID; title: string; description: string; owner_id: ID; parent_id: ID | null; achieved: boolean; budget: number | null; status?: Goal["status"]; team_id?: ID | null;
  planned_start: string | null; planned_end: string | null; actual_start: string | null; actual_end: string | null; deadline?: string | null; created_at: string;
}
/** 里程碑（ADR 0016）：目标上的一个有日期的节点；状态与"可以确认了"都是算出来的 */
interface MilestoneRow {
  id: ID; goal_id: ID; title: string; description: string; due_on: string; reached_at: string | null; created_by: ID; created_at: string; updated_at: string;
}
interface TaskRow {
  id: ID; goal_id: ID | null; parent_id: ID | null; type: string; type_version: number; title: string; description: string;
  state: string; previous_state: string | null; last_weight: number; creator_id: ID; assignee_id: ID | null; reviewer_id: ID;
  participants: Record<string, ID | null>; required_role: string | null; pending_participant: string | null; required_capabilities: string[];
  artifacts: Artifact[]; comments: Comment[]; planned_start: string | null; planned_end: string | null; actual_start: string | null; actual_end: string | null;
  estimate: number | null; points: number | null; sprint_id: ID | null; priority: Task["priority"]; fields: Record<string, unknown>; created_at: string; updated_at: string;
}
interface SprintRow {
  id: ID; team_id: ID | null; name: string; goal: string; starts_on: string; ends_on: string; status: SprintStatus;
  created_by: ID; started_at: string | null; closed_at: string | null; created_at: string;
  /** 已结束且任务已归档的示例迭代：直接给汇总数 */
  snapshot?: { task_count: number; points_total: number; points_done: number; tasks_done: number };
}
interface RelationRow { id: ID; type: RelationType; from: ID; to: ID; created_at: string }
/** 待确认操作（ADR 0003）：Agent 发起、要等人点确认才执行的动作。 */
interface ProposalRow {
  id: ID; agent_id: ID; action: string; action_title: string; grant: GrantName; target: ProposalTarget | null;
  summary: string; payload: Record<string, unknown>; status: ProposalStatus;
  decided_by: ID | null; decided_at: string | null; reason: string | null; created_at: string; expires_at: string;
}
interface RunRow { id: ID; task_id: ID; state: string; executor_id: ID; started_at: string; ended_at: string | null; outcome: Run["outcome"]; usage: Usage[] }

const goals: Record<ID, GoalRow> = {};
const milestones: Record<ID, MilestoneRow> = {};
const tasks: Record<ID, TaskRow> = {};
const sprints: Record<ID, SprintRow> = {};
const relations: RelationRow[] = [];
const proposals: ProposalRow[] = [];
const runs: RunRow[] = [];
const events: Event[] = [];

function addGoal(row: GoalRow) { goals[row.id] = row; }
function addMilestone(row: Omit<MilestoneRow, "updated_at" | "created_by" | "description"> & Partial<MilestoneRow>) {
  milestones[row.id] = { description: "", created_by: goals[row.goal_id]?.owner_id ?? "wang", updated_at: row.created_at, ...row };
}
function addTask(row: Omit<TaskRow, "artifacts" | "comments" | "last_weight" | "previous_state" | "fields" | "updated_at" | "type_version" | "parent_id" | "required_role" | "pending_participant" | "required_capabilities" | "description" | "points" | "sprint_id"> & Partial<TaskRow>) {
  const def = TASK_TYPES[row.type];
  const st = def.workflow.states[row.state];
  const full: TaskRow = {
    parent_id: null, type_version: def.workflow.version, description: "", previous_state: null,
    last_weight: st.weight ?? 0, required_role: null, pending_participant: null, required_capabilities: [], points: null, sprint_id: null,
    artifacts: [], comments: [], fields: {}, updated_at: row.created_at, ...row,
  };
  tasks[full.id] = full;
  return full;
}
function link(type: RelationType, from: ID, to: ID, created_at: string) { relations.push({ id: nextId("L"), type, from, to, created_at }); }
function usage(model: string, input: number, output: number, duration_ms: number, tool_calls: number): Usage {
  return { model, input_tokens: input, output_tokens: output, total_tokens: input + output, duration_ms, tool_calls };
}
function addRun(row: Omit<RunRow, "outcome"> & { outcome?: Run["outcome"] }) {
  runs.push({ outcome: row.ended_at ? "ended" : "running", ...row });
}
function emit(kind: EventKind, opts: { task?: ID | null; goal?: ID | null; actor?: ID | null; summary: string; data?: Record<string, unknown>; at?: string }) {
  const tk = opts.task ? tasks[opts.task] : null;
  events.push({
    id: nextId("E"), kind, task_id: opts.task ?? null, task_title: tk?.title ?? null,
    goal_id: opts.goal ?? tk?.goal_id ?? null, actor: opts.actor ? ref(opts.actor) : null,
    summary: opts.summary, data: opts.data ?? {}, created_at: opts.at ?? nowISO(),
  });
}

// 价格表（每百万 token，人民币）：全局表 + 组织覆盖
const GLOBAL_PRICES: Record<string, PriceModel> = {
  "claude-sonnet-5": { model_id: "claude-sonnet-5", input_per_million: 21, output_per_million: 105, cache_read_per_million: 2.1, cache_write_per_million: 26, currency: "CNY" },
  "claude-opus-5": { model_id: "claude-opus-5", input_per_million: 105, output_per_million: 525, cache_read_per_million: 10.5, cache_write_per_million: 131, currency: "CNY" },
  "gpt-5": { model_id: "gpt-5", input_per_million: 9, output_per_million: 70, cache_read_per_million: 0.9, cache_write_per_million: 9, currency: "CNY" },
};
const ORG_PRICES: Record<string, PriceModel> = {};
const RATES: ExchangeRate[] = [{ from: "USD", to: "CNY", rate: 7.2 }];
const priceOf = (model: string) => ORG_PRICES[model] ?? GLOBAL_PRICES[model];
const usageCost = (u: Usage) => {
  const p = priceOf(u.model) ?? { input_per_million: 20, output_per_million: 100 };
  return (u.input_tokens * p.input_per_million + u.output_tokens * p.output_per_million) / 1_000_000;
};
const runCost = (r: RunRow) => r.usage.reduce((s, u) => s + usageCost(u), 0);
const runTokens = (r: RunRow) => r.usage.reduce((s, u) => s + u.total_tokens, 0);

// ---------- 种子数据 ----------
function seed() {
  addGoal({ id: "G1", title: "Q3 提升用户留存", description: "把次月留存从 31% 提到 38%。", owner_id: "wang", parent_id: null, achieved: false, budget: 5000, planned_start: day(-40), planned_end: day(50), actual_start: day(-38), actual_end: null, created_at: at(-42) });
  addGoal({ id: "G2", title: "登录体验改版", description: "减少登录流失，支持一键登录。", owner_id: "li", parent_id: "G1", achieved: false, budget: 2000, planned_start: day(-30), planned_end: day(20), actual_start: day(-28), actual_end: null, created_at: at(-32) });
  addGoal({ id: "G3", title: "支付转化优化", description: "支付页改版与报表导出。", owner_id: "zhang", parent_id: "G1", achieved: false, budget: null, planned_start: day(-10), planned_end: day(45), actual_start: day(-9), actual_end: null, created_at: at(-12) });
  addGoal({ id: "G4", title: "内部效率工具", description: "周报、月报、看板自动化。", owner_id: "zhao", parent_id: null, achieved: false, budget: 1500, planned_start: day(-20), planned_end: day(60), actual_start: day(-20), actual_end: null, deadline: day(60), created_at: at(-22) });
  // 里程碑（ADR 0016）：与演示库一致——一条已达到、一条未到、一条逾期（且该日期前的任务都已完成 → 可以确认了）
  addMilestone({ id: "M1", goal_id: "G1", title: "登录页改版进入测试", description: "登录页需求开发完成并提测。", due_on: day(-3), reached_at: at(-2, 18), created_at: at(-40) });
  addMilestone({ id: "M2", goal_id: "G1", title: "官网 v2.0 上线", description: "登录页与支付页一起随 v2.0 发布。", due_on: day(30), reached_at: null, created_at: at(-40) });
  addMilestone({ id: "M3", goal_id: "G4", title: "供应商合同归档完成", due_on: day(-4), reached_at: null, created_at: at(-20) });

  // T1：需求，走到开发中；设计阶段小王没留执行记录；开发阶段两段执行记录（提问前后各一段）
  addTask({
    id: "T1", goal_id: "G2", type: "requirement", title: "登录页改版", description: "重做登录页：支持手机号一键登录，替换旧表单。参考需求文档。",
    state: "developing", previous_state: "developing", creator_id: "wang", assignee_id: "li", reviewer_id: "wang",
    participants: { designer: "wang", developer: "li", tester: "zhang", releaser: "zhao" },
    planned_start: day(-14), planned_end: day(7), actual_start: day(-12), actual_end: null, estimate: 40, points: 8, sprint_id: "S7", priority: "high", created_at: at(-14, 10),
    fields: { background: "旧登录页流失率 22%，高于行业均值。" },
  });
  tasks.T1.artifacts.push({ id: "A1", type: "prd", title: "登录页改版需求文档", url: "https://docs.example.com/prd/login", attached_by: ref("wang"), created_at: at(-10, 15) });
  tasks.T1.comments.push(
    { id: "C1", kind: "comment", author: ref("li-agent"), body: "主按钮用品牌蓝还是深灰？需求文档里两处不一致。", created_at: at(-6, 11, 20) },
    { id: "C2", kind: "comment", author: ref("wang"), body: "用品牌蓝，深灰那处是笔误。", created_at: at(-6, 14, 5) },
    { id: "C3", kind: "note", author: ref("li-agent"), body: "已生成 LoginForm 组件与手机号校验，单测 12/12 通过；下一步接短信验证码接口。", created_at: at(-2, 16, 30) },
  );
  addRun({ id: "R1", task_id: "T1", state: "developing", executor_id: "li-agent", started_at: at(-8, 9), ended_at: at(-6, 11, 20), usage: [usage("claude-sonnet-5", 9000, 3000, 5_400_000, 38)] });
  addRun({ id: "R2", task_id: "T1", state: "developing", executor_id: "li-agent", started_at: at(-5, 9, 30), ended_at: null, usage: [usage("claude-sonnet-5", 13000, 5000, 9_800_000, 71), usage("claude-opus-5", 3000, 1000, 1_200_000, 6)] });

  // T2：Bug，发现于 T1
  addTask({
    id: "T2", goal_id: "G2", type: "bug", title: "金额显示错位", description: "登录后首页金额在窄屏下换行错位。", state: "confirmed", creator_id: "zhang", assignee_id: "li", reviewer_id: "zhang",
    participants: { developer: "li", tester: "zhang" }, planned_start: day(-3), planned_end: day(2), actual_start: null, actual_end: null, estimate: 4, points: 2, sprint_id: "S7", priority: "urgent", created_at: at(-3, 14),
    fields: { steps: "1. 登录 2. 缩窄窗口到 375px 3. 看首页金额", severity: "中" },
  });
  link("found_in", "T2", "T1", at(-3, 14, 5));

  addTask({ id: "T3", goal_id: "G4", type: "generic", title: "写周报", description: "本周项目进展周报。", state: "todo", creator_id: "wang", assignee_id: "li", reviewer_id: "wang", participants: {}, planned_start: day(-1), planned_end: day(1), actual_start: null, actual_end: null, estimate: 2, points: 1, sprint_id: "S7", priority: "normal", created_at: at(-1, 9) });
  addTask({ id: "T4", goal_id: "G3", type: "generic", title: "整理客户名单", description: "从 CRM 导出并去重。", state: "todo", creator_id: "wang", assignee_id: null, reviewer_id: "wang", participants: {}, required_capabilities: ["data_analysis"], planned_start: day(1), planned_end: day(4), actual_start: null, actual_end: null, estimate: 6, points: 3, priority: "normal", created_at: at(-1, 10) });
  addTask({ id: "T5", goal_id: "G2", type: "release", title: "v2.0 发布", description: "登录改版随 v2.0 上线。", state: "ready", creator_id: "zhao", assignee_id: "zhao", reviewer_id: "zhao", participants: { releaser: "zhao" }, planned_start: day(8), planned_end: day(10), actual_start: null, actual_end: null, estimate: 8, points: 3, sprint_id: "S8", priority: "high", created_at: at(-5, 16), fields: { version: "2.0.0" } });
  link("blocks", "T1", "T5", at(-5, 16, 10));

  // T6：需求，测试角色空缺，进入待领取
  addTask({
    id: "T6", goal_id: "G3", type: "requirement", title: "导出报表", description: "支付报表支持按周导出 CSV。", state: "testing", previous_state: "testing", creator_id: "wang", assignee_id: null, reviewer_id: "wang",
    participants: { designer: "wang", developer: "li", tester: null, releaser: "zhao" }, required_role: "tester", pending_participant: "tester", required_capabilities: ["testing"],
    planned_start: day(-10), planned_end: day(12), actual_start: day(-9), actual_end: null, estimate: 24, points: 5, sprint_id: "S7", priority: "normal", created_at: at(-10, 9),
  });
  tasks.T6.artifacts.push(
    { id: "A2", type: "prd", title: "报表导出需求", url: "https://docs.example.com/prd/export", attached_by: ref("wang"), created_at: at(-9, 12) },
    { id: "A3", type: "pr", title: "feat: weekly CSV export #4", url: "https://git.example.com/pr/4", attached_by: ref("li-agent"), created_at: at(-4, 18) },
  );
  addRun({ id: "R3", task_id: "T6", state: "developing", executor_id: "li-agent", started_at: at(-8, 9), ended_at: at(-4, 18), usage: [usage("claude-sonnet-5", 7000, 2000, 4_100_000, 29)] });

  addTask({ id: "T7", goal_id: "G4", type: "generic", title: "写月度总结", description: "", state: "done", creator_id: "wang", assignee_id: "wang", reviewer_id: "wang", participants: {}, planned_start: day(-20), planned_end: day(-17), actual_start: day(-20), actual_end: day(-18), estimate: 3, points: 2, sprint_id: "S6", priority: "normal", created_at: at(-20, 9) });
  tasks.T7.artifacts.push({ id: "A4", type: "result", title: "8 月总结", url: "https://docs.example.com/monthly/8", attached_by: ref("wang-agent"), created_at: at(-18, 15) });
  addRun({ id: "R4", task_id: "T7", state: "in_progress", executor_id: "wang-agent", started_at: at(-20, 9, 10), ended_at: at(-18, 15), usage: [usage("claude-sonnet-5", 4000, 2000, 1_500_000, 9)] });

  addTask({ id: "T8", goal_id: "G3", type: "generic", title: "客户访谈纪要", description: "整理 6 场访谈录音为纪要。", state: "in_progress", creator_id: "wang", assignee_id: "wang", reviewer_id: "wang", participants: {}, planned_start: day(-2), planned_end: day(3), actual_start: day(-2), actual_end: null, estimate: 5, points: 3, sprint_id: "S7", priority: "normal", created_at: at(-2, 9) });
  addRun({ id: "R5", task_id: "T8", state: "in_progress", executor_id: "wang-agent", started_at: at(-2, 9, 5), ended_at: null, usage: [usage("gpt-5", 2500, 500, 600_000, 4)] });

  addTask({ id: "T9", goal_id: "G3", type: "bug", title: "导出 CSV 乱码", description: "旧版导出 Excel 打开乱码。", state: "wont_fix", creator_id: "zhang", assignee_id: null, reviewer_id: "zhang", participants: { developer: null, tester: "zhang" }, planned_start: day(-15), planned_end: day(-13), actual_start: null, actual_end: day(-14), estimate: null, priority: "low", created_at: at(-15, 9) });
  tasks.T9.comments.push({ id: "C4", kind: "comment", author: ref("wang"), body: "旧版导出即将下线，不修。", created_at: at(-14, 10) });

  addTask({ id: "T10", goal_id: "G4", type: "generic", title: "数据看板原型", description: "运营总览的可点原型。", state: "in_progress", creator_id: "zhao", assignee_id: "zhang", reviewer_id: "zhao", participants: {}, planned_start: day(-12), planned_end: day(-3), actual_start: day(-11), actual_end: null, estimate: 16, points: 5, sprint_id: "S7", priority: "high", created_at: at(-12, 9) });
  addRun({ id: "R6", task_id: "T10", state: "in_progress", executor_id: "zhang-agent", started_at: at(-11, 9), ended_at: at(-4, 17), usage: [usage("claude-opus-5", 20000, 6000, 12_000_000, 88)] });

  addTask({ id: "T11", goal_id: "G3", type: "requirement", title: "支付页优化", description: "缩短支付路径到两步。", state: "draft", creator_id: "wang", assignee_id: null, reviewer_id: "wang", participants: { designer: "wang", developer: "zhao", tester: "zhang", releaser: "zhao" }, planned_start: day(5), planned_end: day(30), actual_start: null, actual_end: null, estimate: 60, points: 13, sprint_id: "S8", priority: "high", created_at: at(-1, 15) });
  addTask({ id: "T12", goal_id: "G4", type: "generic", title: "周报模板整理", description: "", state: "cancelled", creator_id: "wang", assignee_id: null, reviewer_id: "wang", participants: {}, planned_start: day(-8), planned_end: day(-6), actual_start: null, actual_end: day(-7), estimate: 1, points: 1, sprint_id: "S6", priority: "low", created_at: at(-8, 9) });
  addTask({ id: "T13", goal_id: "G1", type: "generic", title: "留存漏斗分析", description: "按渠道拆留存漏斗。", state: "submitted", creator_id: "wang", assignee_id: "zhang", reviewer_id: "wang", participants: {}, planned_start: day(-6), planned_end: day(-1), actual_start: day(-6), actual_end: null, estimate: 8, points: 3, sprint_id: "S7", priority: "normal", created_at: at(-6, 9) });
  tasks.T13.artifacts.push({ id: "A5", type: "result", title: "留存漏斗报告", url: "https://docs.example.com/report/retention", attached_by: ref("zhang-agent"), created_at: at(-1, 17) });
  addRun({ id: "R7", task_id: "T13", state: "in_progress", executor_id: "zhang-agent", started_at: at(-6, 9), ended_at: at(-1, 17), usage: [usage("claude-sonnet-5", 6000, 3000, 2_000_000, 22)] });

  // 本迭代里已经完成的几件事（燃尽图靠它们往下走）与一件待办
  addTask({ id: "T14", goal_id: "G2", type: "generic", title: "接入短信验证码", description: "对接短信服务商，验证码 60 秒有效。", state: "done", creator_id: "wang", assignee_id: "li", reviewer_id: "wang", participants: {}, planned_start: day(-6), planned_end: day(-3), actual_start: day(-6), actual_end: day(-4), estimate: 6, points: 3, sprint_id: "S7", priority: "normal", created_at: at(-7, 9) });
  addTask({ id: "T15", goal_id: "G2", type: "generic", title: "登录页埋点", description: "按钮点击、失败原因、耗时。", state: "done", creator_id: "wang", assignee_id: "zhao", reviewer_id: "wang", participants: {}, planned_start: day(-5), planned_end: day(-2), actual_start: day(-5), actual_end: day(-2), estimate: 3, points: 2, sprint_id: "S7", priority: "normal", created_at: at(-6, 9) });
  addTask({ id: "T16", goal_id: "G2", type: "bug", title: "验证码倒计时不归零", description: "重新发送后倒计时从上一次剩余秒数继续。", state: "verified", creator_id: "zhang", assignee_id: "li", reviewer_id: "zhang", participants: { developer: "li", tester: "zhang" }, planned_start: day(-3), planned_end: day(-1), actual_start: day(-3), actual_end: day(-1), estimate: 2, points: 1, sprint_id: "S7", priority: "high", created_at: at(-3, 10) });
  addTask({ id: "T17", goal_id: "G3", type: "generic", title: "报表导出加权限", description: "只有团队负责人能导出全量。", state: "todo", creator_id: "wang", assignee_id: null, reviewer_id: "wang", participants: {}, planned_start: day(3), planned_end: day(9), actual_start: null, actual_end: null, estimate: 4, points: 2, priority: "normal", created_at: at(-1, 16) });

  // 服务事业部这条线：另一个共享域的目标与任务，用来看范围切换与各组织单元对比
  addGoal({ id: "G5", title: "客户满意度提升", description: "把满意度从 82 分提到 90 分。", owner_id: "zhou", parent_id: null, achieved: false, budget: 1200, planned_start: day(-25), planned_end: day(35), actual_start: day(-24), actual_end: null, created_at: at(-26) });
  addTask({ id: "T18", goal_id: "G5", type: "generic", title: "回访流失客户", description: "按上月流失名单逐个回访。", state: "in_progress", creator_id: "zhou", assignee_id: "zhou", reviewer_id: "zhou", participants: {}, planned_start: day(-6), planned_end: day(2), actual_start: day(-6), actual_end: null, estimate: 10, points: 5, priority: "high", created_at: at(-7, 9) });
  addTask({ id: "T19", goal_id: "G5", type: "generic", title: "整理常见问题", description: "把工单里重复最多的问题整理成条目。", state: "done", creator_id: "zhou", assignee_id: "sun", reviewer_id: "zhou", participants: {}, planned_start: day(-12), planned_end: day(-8), actual_start: day(-12), actual_end: day(-9), estimate: 6, points: 3, priority: "normal", created_at: at(-13, 9) });
  addTask({ id: "T20", goal_id: "G5", type: "generic", title: "客服话术更新", description: "按新版流程更新话术。", state: "todo", creator_id: "zhou", assignee_id: "chen", reviewer_id: "zhou", participants: {}, planned_start: day(-4), planned_end: day(-1), actual_start: null, actual_end: null, estimate: 4, points: 2, priority: "normal", created_at: at(-9, 9) });
  addTask({ id: "T21", goal_id: "G5", type: "generic", title: "月度满意度报表", description: "按渠道拆满意度并给出结论。", state: "in_progress", creator_id: "zhou", assignee_id: "zhou-agent", reviewer_id: "zhou", participants: {}, planned_start: day(-3), planned_end: day(4), actual_start: day(-3), actual_end: null, estimate: 8, points: 3, priority: "normal", created_at: at(-3, 9) });
  addRun({ id: "R20", task_id: "T21", state: "in_progress", executor_id: "zhou-agent", started_at: at(-3, 10), ended_at: at(-3, 15), usage: [usage("gpt-5", 9000, 3000, 3_400_000, 18)] });
  addRun({ id: "R21", task_id: "T21", state: "in_progress", executor_id: "zhou-agent", started_at: at(-1, 9), ended_at: at(-1, 12), usage: [usage("claude-sonnet-5", 6000, 2200, 2_100_000, 11)] });

  // 给「待我处理」的示例（DESIGN.md §12）：小王名下一条逾期两天还没开始的、一条明天才开始的，以及一条小李的编码 Agent 停下来提问、等创建者小王答复的
  // （自己的 Agent 提的问不算"等我答复"——它和我是同一方，真实后端同样按 isPrincipal 判断）
  addTask({ id: "T22", goal_id: "G2", type: "generic", title: "补齐登录改版的验收清单", description: "把一键登录的验收项按设备与网络环境列全。", state: "todo", creator_id: "wang", assignee_id: "wang", reviewer_id: "zhao", participants: {}, planned_start: day(-4), planned_end: day(-2), actual_start: null, actual_end: null, estimate: 3, points: 2, sprint_id: "S7", priority: "high", created_at: at(-5, 11) });
  addTask({ id: "T23", goal_id: "G3", type: "generic", title: "审阅支付页两步方案", description: "对照支付页优化需求，审阅交互稿。", state: "todo", creator_id: "zhao", assignee_id: "wang", reviewer_id: "zhao", participants: {}, planned_start: day(1), planned_end: day(3), actual_start: null, actual_end: null, estimate: 2, points: 1, sprint_id: "S8", priority: "normal", created_at: at(-1, 14) });
  addTask({ id: "T24", goal_id: "G4", type: "generic", title: "整理竞品登录流程", description: "记录五家竞品的登录步骤与耗时。", state: "waiting", previous_state: "in_progress", creator_id: "wang", assignee_id: "li-agent", reviewer_id: "wang", participants: {}, planned_start: day(-2), planned_end: day(2), actual_start: day(-2), actual_end: null, estimate: 4, points: 2, sprint_id: "S7", priority: "normal", created_at: at(-2, 9) });
  tasks.T24.comments.push({ id: "C5", kind: "comment", author: ref("li-agent"), body: "五家里有两家已经下线了独立登录页，改成了第三方账号直连。这两家是跳过，还是按第三方流程记？", created_at: at(0, 8, 12) });
  addRun({ id: "R22", task_id: "T24", state: "in_progress", executor_id: "li-agent", started_at: at(-2, 9, 20), ended_at: at(0, 8, 12), usage: [usage("claude-sonnet-5", 5200, 1800, 1_900_000, 14)] });

  // 迭代：两个已结束（其中最早的一个任务已归档，只留汇总）、一个进行中、一个规划中
  sprints.S5 = { id: "S5", team_id: "team-rd", name: "第 5 次迭代", goal: "留存漏斗与访谈纪要。", starts_on: day(-48), ends_on: day(-35), status: "closed", created_by: "wang", started_at: at(-48, 9), closed_at: at(-35, 18), created_at: at(-50), snapshot: { task_count: 9, points_total: 26, points_done: 21, tasks_done: 7 } };
  sprints.S6 = { id: "S6", team_id: "team-rd", name: "第 6 次迭代", goal: "补齐周报、月报的自动化。", starts_on: day(-34), ends_on: day(-21), status: "closed", created_by: "wang", started_at: at(-34, 9), closed_at: at(-21, 18), created_at: at(-36), snapshot: { task_count: 8, points_total: 24, points_done: 18, tasks_done: 6 } };
  sprints.S7 = { id: "S7", team_id: "team-rd", name: "第 7 次迭代", goal: "打通登录改版的开发与测试，上线一键登录。", starts_on: day(-6), ends_on: day(7), status: "active", created_by: "wang", started_at: at(-6, 9), closed_at: null, created_at: at(-8) };
  sprints.S8 = { id: "S8", team_id: "team-rd", name: "第 8 次迭代", goal: "支付页两步走通，报表导出可用。", starts_on: day(8), ends_on: day(21), status: "planning", created_by: "wang", started_at: null, closed_at: null, created_at: at(-2) };

  // 动态（按时间顺序）
  emit("GoalCreated", { goal: "G1", actor: "wang", summary: "创建目标「Q3 提升用户留存」", at: at(-42) });
  emit("GoalCreated", { goal: "G2", actor: "wang", summary: "在「Q3 提升用户留存」下创建目标「登录体验改版」", at: at(-32) });
  emit("TaskCreated", { task: "T1", actor: "wang", summary: "创建需求「登录页改版」", at: at(-14, 10) });
  emit("TaskTransitioned", { task: "T1", actor: "wang", summary: "开始设计：草稿 → 设计中", data: { from: "draft", to: "designing" }, at: at(-12, 9) });
  emit("TaskAssigned", { task: "T1", actor: null, summary: "负责人切到参与角色·设计：小王", at: at(-12, 9) });
  emit("ArtifactAttached", { task: "T1", actor: "wang", summary: "附上需求文档「登录页改版需求文档」", at: at(-10, 15) });
  emit("TaskTransitioned", { task: "T1", actor: "wang", summary: "设计完成：设计中 → 开发中", data: { from: "designing", to: "developing" }, at: at(-9, 10) });
  emit("TaskAssigned", { task: "T1", actor: null, summary: "负责人切到参与角色·开发：小李", at: at(-9, 10) });
  emit("RunStarted", { task: "T1", actor: "li-agent", summary: "小李的编码 Agent 开始执行（开发中）", at: at(-8, 9) });
  emit("UsageReported", { task: "T1", actor: "li-agent", summary: "上报用量：claude-sonnet-5 1.2 万 token", at: at(-7, 18) });
  emit("CommentAdded", { task: "T1", actor: "li-agent", summary: "评论：主按钮用品牌蓝还是深灰？", at: at(-6, 11, 20) });
  emit("TaskTransitioned", { task: "T1", actor: "li-agent", summary: "提问等待：开发中 → 等待答复", data: { from: "developing", to: "waiting" }, at: at(-6, 11, 20) });
  emit("RunEnded", { task: "T1", actor: null, summary: "执行记录结束（已结束）", at: at(-6, 11, 20) });
  emit("CommentAdded", { task: "T1", actor: "wang", summary: "评论：用品牌蓝，深灰那处是笔误。", at: at(-6, 14, 5) });
  emit("TaskTransitioned", { task: "T1", actor: "wang", summary: "答复并恢复：等待答复 → 开发中", data: { from: "waiting", to: "developing" }, at: at(-6, 14, 5) });
  emit("RunStarted", { task: "T1", actor: "li-agent", summary: "小李的编码 Agent 开始执行（开发中）", at: at(-5, 9, 30) });
  emit("TaskCreated", { task: "T2", actor: "zhang", summary: "创建 Bug「金额显示错位」", at: at(-3, 14) });
  emit("TasksLinked", { task: "T2", actor: "zhang", summary: "建立关联：「金额显示错位」发现于「登录页改版」", at: at(-3, 14, 5) });
  emit("TaskTransitioned", { task: "T2", actor: "zhang", summary: "确认：新建 → 已确认", data: { from: "new", to: "confirmed" }, at: at(-3, 14, 30) });
  emit("TaskAssigned", { task: "T2", actor: null, summary: "负责人切到参与角色·修复：小李", at: at(-3, 14, 30) });
  emit("CommentAdded", { task: "T1", actor: "li-agent", summary: "工作日志：已生成 LoginForm 组件与手机号校验", at: at(-2, 16, 30) });
  emit("UsageReported", { task: "T1", actor: "li-agent", summary: "上报用量：claude-sonnet-5 1.8 万 token，claude-opus-5 4000 token", at: at(-1, 19) });
  emit("TaskSentToBacklog", { task: "T6", actor: null, summary: "开发完成后测试角色空缺，进入待领取任务（要求角色：测试）", at: at(-4, 18) });
  emit("TaskTransitioned", { task: "T12", actor: "wang", summary: "取消：待办 → 已取消", data: { from: "todo", to: "cancelled" }, at: at(-7, 9) });
  emit("TaskTransitioned", { task: "T13", actor: "zhang-agent", summary: "提交结果：进行中 → 待验收", data: { from: "in_progress", to: "submitted" }, at: at(-1, 17) });
  emit("SprintStarted", { actor: "wang", summary: "开始迭代「第 7 次迭代」", data: { sprint_id: "S7" }, at: at(-6, 9) });
  emit("TaskAddedToSprint", { task: "T16", actor: "zhang", summary: "加入迭代「第 7 次迭代」", data: { sprint_id: "S7" }, at: at(-3, 10) });
  emit("SprintCreated", { actor: "wang", summary: "创建迭代「第 8 次迭代」", data: { sprint_id: "S8" }, at: at(-2) });
  emit("TaskCreated", { task: "T22", actor: "wang", summary: "创建任务「补齐登录改版的验收清单」", at: at(-5, 11) });
  emit("TaskCreated", { task: "T23", actor: "zhao", summary: "创建任务「审阅支付页两步方案」", at: at(-1, 14) });
  emit("TaskCreated", { task: "T24", actor: "wang", summary: "创建任务「整理竞品登录流程」", at: at(-2, 9) });
  emit("RunStarted", { task: "T24", actor: "li-agent", summary: "小李的编码 Agent 开始执行（进行中）", at: at(-2, 9, 20) });
  emit("CommentAdded", { task: "T24", actor: "li-agent", summary: "评论：五家里有两家已经下线了独立登录页，是跳过还是按第三方流程记？", at: at(0, 8, 12) });
  emit("TaskTransitioned", { task: "T24", actor: "li-agent", summary: "提问等待：进行中 → 等待答复", data: { from: "in_progress", to: "waiting" }, at: at(0, 8, 12) });
}
seed();

/**
 * 待确认操作的示例：三条等人确认（其中两条由小王自己的 Agent 发起，第三条是小李的，用来演示「只看等我确认」），
 * 各一条已确认 / 已拒绝 / 已过期。有效期从几小时到两天不等，页面上就有真实的倒计时可看。
 */
function seedProposals() {
  const hours = (n: number) => new Date(Date.now() + n * 3600_000).toISOString();
  const taskTarget = (id: ID): ProposalTarget => ({ kind: "task", id, title: tasks[id].title });
  const goalTarget = (id: ID): ProposalTarget => ({ kind: "goal", id, title: goals[id].title });
  proposals.push(
    {
      id: "P1", agent_id: "wang-agent", action: "task.create", action_title: "创建任务", grant: "create_task", target: goalTarget("G2"),
      summary: "确认后会在目标「登录体验改版」下创建任务「补充登录失败的埋点」，并放进待领取任务等人来领。",
      payload: { title: "补充登录失败的埋点", goal_id: "G2", type: "generic", priority: "high", estimate: 4, planned_start: day(1), planned_end: day(3), description: "把登录失败的原因、次数、耗时都记下来，为下一步优化留数据。" },
      status: "pending", decided_by: null, decided_at: null, reason: null, created_at: at(0, 8, 20), expires_at: hours(20),
    },
    {
      id: "P2", agent_id: "shared-doc", action: "task.claim", action_title: "领取任务", grant: "claim_backlog", target: taskTarget("T6"),
      summary: "确认后公共文档助手会领取任务「导出报表」，并立刻开始执行。",
      payload: { task_id: "T6", capabilities: ["writing"], comment: "我可以先把导出格式的说明写完。" },
      status: "pending", decided_by: null, decided_at: null, reason: null, created_at: at(0, 9, 5), expires_at: hours(3.5),
    },
    {
      id: "P3", agent_id: "li-agent", action: "task.transition", action_title: "验收", grant: "review", target: taskTarget("T13"),
      summary: "确认后小李的编码 Agent 会验收任务「留存漏斗分析」的结果，任务将进入已完成。",
      payload: { task_id: "T13", transition: "accept", from: "submitted", to: "done", comment: "报告里的口径和上次一致，可以收。" },
      status: "pending", decided_by: null, decided_at: null, reason: null, created_at: at(-1, 17, 40), expires_at: hours(46),
    },
    {
      id: "P4", agent_id: "wang-agent", action: "task.create", action_title: "创建任务", grant: "create_task", target: goalTarget("G4"),
      summary: "确认后会在目标「内部效率工具」下创建任务「整理上月周报模板」，并指派给小王的写作助手。",
      payload: { title: "整理上月周报模板", goal_id: "G4", type: "generic", assignee_id: "wang-agent", estimate: 2 },
      status: "approved", decided_by: "wang", decided_at: at(-1, 10, 12), reason: null, created_at: at(-1, 10, 5), expires_at: at(6, 10, 5),
    },
    {
      id: "P5", agent_id: "li-agent", action: "task.transition", action_title: "验收", grant: "review", target: taskTarget("T1"),
      summary: "确认后小李的编码 Agent 会验收任务「登录页改版」的结果，任务将进入待发布。",
      payload: { task_id: "T1", transition: "accept", from: "submitted", to: "ready" },
      status: "rejected", decided_by: "wang", decided_at: at(-2, 15, 30), reason: "测试报告还没上传，先补齐再验收。", created_at: at(-2, 15, 10), expires_at: at(5, 15, 10),
    },
    {
      id: "P6", agent_id: "shared-doc", action: "task.claim", action_title: "领取任务", grant: "claim_backlog", target: taskTarget("T4"),
      summary: "确认后公共文档助手会领取任务「整理客户名单」，并立刻开始执行。",
      payload: { task_id: "T4", capabilities: ["writing"] },
      status: "expired", decided_by: null, decided_at: null, reason: null, created_at: at(-9, 11), expires_at: at(-2, 11),
    },
  );
  for (const p of proposals) {
    emit("ProposalCreated", { actor: p.agent_id, task: p.target?.kind === "task" ? p.target.id : null, summary: t("mock.ev.proposalCreated", { action: L(p.action_title) }), data: { proposal_id: p.id }, at: p.created_at });
    if (p.status === "approved") emit("ProposalApproved", { actor: p.agent_id, summary: t("mock.ev.proposalApproved", { action: L(p.action_title), name: L(MEMBERS[p.decided_by!].name) }), data: { proposal_id: p.id }, at: p.decided_at! });
    if (p.status === "rejected") emit("ProposalRejected", { actor: p.decided_by, summary: t("mock.ev.proposalRejected", { action: L(p.action_title), reason: p.reason ?? "" }), data: { proposal_id: p.id }, at: p.decided_at! });
  }
}
seedProposals();

// ---------- 视图转换 ----------
const defOf = (tk: TaskRow) => TASK_TYPES[tk.type];
const stateOf = (tk: TaskRow): TaskState => defOf(tk).workflow.states[tk.state];
const isTerminalRow = (tk: TaskRow) => ["terminal_success", "terminal_failure"].includes(stateOf(tk).label);
const progressOf = (tk: TaskRow) => {
  const st = stateOf(tk);
  if (st.label === "terminal_success") return 100;
  if (st.label === "terminal_failure") return 0;
  return st.weight ?? tk.last_weight;
};
const taskRef = (tk: TaskRow): TaskRef => ({ id: tk.id, title: tk.title, type: tk.type, state: stateOf(tk) });
const viewRun = (r: RunRow): Run => ({
  id: r.id, task_id: r.task_id, state: r.state, state_title: TASK_TYPES[tasks[r.task_id].type].workflow.states[r.state]?.title ?? r.state,
  executor: ref(r.executor_id), started_at: r.started_at, ended_at: r.ended_at, outcome: r.outcome, usage: r.usage, total_tokens: runTokens(r), cost: round2(runCost(r)),
});
const viewRelation = (r: RelationRow): Relation => ({ id: r.id, type: r.type, from: taskRef(tasks[r.from]), to: taskRef(tasks[r.to]), created_at: r.created_at });
const round2 = (n: number) => Math.round(n * 100) / 100;
const taskRuns = (id: ID) => runs.filter((r) => r.task_id === id);
const taskCost = (id: ID) => round2(taskRuns(id).reduce((s, r) => s + runCost(r), 0));

function viewTask(tk: TaskRow): Task {
  const def = defOf(tk);
  const rs = taskRuns(tk.id);
  return {
    id: tk.id, goal_id: tk.goal_id, goal: tk.goal_id && goals[tk.goal_id] ? { id: tk.goal_id, title: goals[tk.goal_id].title } : null, parent_id: tk.parent_id,
    type: tk.type, type_title: def.title, type_version: tk.type_version, title: tk.title, description: tk.description,
    state: stateOf(tk), previous_state: tk.previous_state, creator: ref(tk.creator_id), assignee: tk.assignee_id ? ref(tk.assignee_id) : null, reviewer: ref(tk.reviewer_id),
    participants: Object.fromEntries(Object.entries(def.participants).map(([slot, p]) => [slot, { title: p.title, role: p.role, executor: tk.participants[slot] ? ref(tk.participants[slot] as ID) : null }])),
    required_role: tk.required_role, pending_participant: tk.pending_participant, required_capabilities: tk.required_capabilities,
    relations: relations.filter((r) => r.from === tk.id || r.to === tk.id).map(viewRelation),
    artifacts: tk.artifacts, comments: tk.comments, runs: rs.map(viewRun),
    planned_start: tk.planned_start, planned_end: tk.planned_end, actual_start: tk.actual_start, actual_end: tk.actual_end,
    estimate: tk.estimate, points: tk.points, sprint: tk.sprint_id && sprints[tk.sprint_id] ? { id: tk.sprint_id, name: sprints[tk.sprint_id].name } : null,
    priority: tk.priority, progress: progressOf(tk), total_tokens: rs.reduce((s, r) => s + runTokens(r), 0), cost: taskCost(tk.id),
    fields: tk.fields, created_at: tk.created_at, updated_at: tk.updated_at,
  };
}

function goalSubtreeIds(id: ID): ID[] {
  const out = [id];
  for (const gr of Object.values(goals)) if (gr.parent_id === id) out.push(...goalSubtreeIds(gr.id));
  return out;
}
/** 里程碑状态：已确认 → 已达到；日期已过未确认 → 逾期；否则未到 */
const milestoneStatus = (m: MilestoneRow): Milestone["status"] => (m.reached_at ? "reached" : m.due_on < day(0) ? "overdue" : "upcoming");
/** 可以确认了：目标子树里所有计划结束早于或等于该日期的任务（跳过已终止的）都已进入成功终态，且至少有一个；已达到的恒为假 */
function milestoneReady(m: MilestoneRow): boolean {
  if (m.reached_at) return false;
  const ids = goalSubtreeIds(m.goal_id);
  const due = Object.values(tasks).filter((tk) => tk.goal_id && ids.includes(tk.goal_id) && tk.planned_end && tk.planned_end <= m.due_on && stateOf(tk).label !== "terminal_failure");
  return due.length > 0 && due.every((tk) => stateOf(tk).label === "terminal_success");
}
const viewMilestone = (m: MilestoneRow): Milestone => ({
  id: m.id, goal_id: m.goal_id, title: m.title, description: m.description, due_on: m.due_on, reached_at: m.reached_at,
  status: milestoneStatus(m), ready_hint: milestoneReady(m), created_by: ref(m.created_by), created_at: m.created_at, updated_at: m.updated_at,
});
const goalMilestones = (goalId: ID) => Object.values(milestones).filter((m) => m.goal_id === goalId).sort((a, b) => (a.due_on < b.due_on ? -1 : a.due_on > b.due_on ? 1 : 0));
const milestoneMark = (m: MilestoneRow): MilestoneMark => ({ id: m.id, title: m.title, due_on: m.due_on, status: milestoneStatus(m), ready_hint: milestoneReady(m) });
function milestoneSummary(goalId: ID): MilestoneSummary {
  const ms = goalMilestones(goalId).map(viewMilestone);
  const next = ms.find((m) => m.status === "upcoming");
  return { total: ms.length, reached: ms.filter((m) => m.status === "reached").length, overdue: ms.filter((m) => m.status === "overdue").length, next: next ? { title: next.title, due_on: next.due_on } : null };
}
function viewGoal(gr: GoalRow, withChildren: boolean): Goal {
  const ids = goalSubtreeIds(gr.id);
  const ts = Object.values(tasks).filter((tk) => tk.goal_id && ids.includes(tk.goal_id));
  const weight = (tk: TaskRow) => tk.estimate ?? 1;
  const totalW = ts.reduce((s, tk) => s + weight(tk), 0);
  const progress = gr.achieved ? 100 : totalW ? Math.round(ts.reduce((s, tk) => s + progressOf(tk) * weight(tk), 0) / totalW) : 0;
  return {
    id: gr.id, title: gr.title, description: gr.description, owner: ref(gr.owner_id), parent_id: gr.parent_id, team_id: gr.team_id ?? null, progress, achieved: gr.achieved,
    status: gr.status ?? (gr.achieved ? "achieved" : "active"),
    budget: gr.budget, cost: round2(ts.reduce((s, tk) => s + taskCost(tk.id), 0)),
    planned_start: gr.planned_start, planned_end: gr.planned_end, actual_start: gr.actual_start, actual_end: gr.actual_end, deadline: gr.deadline ?? null,
    task_count: ts.length, done_task_count: ts.filter((tk) => stateOf(tk).label === "terminal_success").length,
    children: withChildren ? Object.values(goals).filter((c) => c.parent_id === gr.id).map((c) => viewGoal(c, true)) : [],
    created_at: gr.created_at,
    milestones: goalMilestones(gr.id).map(viewMilestone), milestone_summary: milestoneSummary(gr.id),
  };
}

const viewMember = (m: MemberRow): Member => ({ id: m.id, name: m.name, email: m.email, roles: m.roles, team_id: m.team_id, locale: m.locale, created_at: m.created_at });
const viewOrgMember = (m: MemberRow): OrgMember => ({ ...viewMember(m), active: m.active, is_owner: ORG.owner_id === m.id });
const viewTeam = (tm: OrgTeam): Team => ({ id: tm.id, name: tm.name, lead_id: tm.lead_id, parent_id: tm.parent_id, is_boundary: !!tm.is_boundary });

function session(): Session {
  const me = MEMBERS[ME];
  return {
    member: viewMember(me),
    organization: ORG,
    roles: ROLES.map((r) => ({ name: r.name, title: r.title })),
    teams: TEAMS.map(viewTeam),
    artifact_types: ARTIFACT_TYPES,
    capabilities: CAPABILITIES.map((c) => c.name),
    capability_titles: Object.fromEntries(CAPABILITIES.map((c) => [c.name, c.title])),
    permissions: [...new Set(me.roles.flatMap((r) => roleOf(r)?.permissions ?? []))],
    is_owner: ORG.owner_id === me.id,
    scopes: scopeOptions(),
    default_scope: me.team_id ?? "all",
    settings: { ...ORG_SETTINGS },
  };
}

// ---------- 精简流程内核（以登录成员的身份） ----------
const principals = (): ID[] => [ME, ...Object.values(AGENTS).filter((a) => a.owner.id === ME).map((a) => a.id)];
const myRoles = () => MEMBERS[ME].roles;
const describeRule = (rule: string, def: TaskType) => {
  if (rule === "creator") return t("rule.creator");
  if (rule === "assignee") return t("rule.assignee");
  if (rule === "reviewer") return t("rule.reviewer");
  if (rule === "anyone") return t("rule.anyone");
  if (rule.startsWith("participant:")) return t("rule.participant", { title: L(def.participants[rule.slice(12)]?.title ?? rule.slice(12)) });
  if (rule.startsWith("role:")) return t("rule.role", { title: L(roleOf(rule.slice(5))?.title ?? rule.slice(5)) });
  return rule;
};
function matchesBy(tk: TaskRow, by: string[]): boolean {
  const ps = principals();
  return by.some((rule) => {
    if (rule === "anyone") return true;
    if (rule === "creator") return ps.includes(tk.creator_id);
    if (rule === "assignee") return !!tk.assignee_id && ps.includes(tk.assignee_id);
    if (rule === "reviewer") return ps.includes(tk.reviewer_id);
    if (rule.startsWith("participant:")) { const e = tk.participants[rule.slice(12)]; return !!e && ps.includes(e); }
    if (rule.startsWith("role:")) return myRoles().includes(rule.slice(5));
    return false;
  });
}
const joinTitles = (xs: string[]) => xs.map(L).join(getLocale() === "zh-CN" ? "、" : ", ");
function checkRequire(tk: TaskRow, c: string, body?: TransitionBody): string | null {
  if (c === "deps_done") {
    const open = relations.filter((r) => r.type === "blocks" && r.to === tk.id).map((r) => tasks[r.from]).filter((o) => stateOf(o).label !== "terminal_success");
    return open.length ? t("mock.depsOpen", { titles: joinTitles(open.map((o) => o.title)) }) : null;
  }
  if (c === "no_open_bugs") {
    const open = relations.filter((r) => r.type === "found_in" && r.to === tk.id).map((r) => tasks[r.from]).filter((b) => !isTerminalRow(b));
    return open.length ? t("mock.bugsOpen", { titles: joinTitles(open.map((b) => b.title)) }) : null;
  }
  if (c === "comment") return body ? (body.comment?.trim() ? null : t("mock.needComment")) : null;
  if (c === "result") return body ? (body.result ? null : t("mock.needResult")) : null;
  if (c.startsWith("artifact:")) { const type = c.slice(9); return tk.artifacts.some((a) => a.type === type) ? null : t("mock.missingArtifact", { type: L(ARTIFACT_TYPES[type] ?? type) }); }
  return null;
}
function availability(tk: TaskRow, body?: TransitionBody): TransitionAvailability[] {
  const def = defOf(tk);
  const wf = def.workflow;
  return wf.transitions
    .filter((tr) => (tr.from.includes("*") ? !isTerminalRow(tk) : tr.from.includes(tk.state)))
    .map((tr) => {
      const reasons: string[] = [];
      if (!matchesBy(tk, tr.by)) reasons.push(t("mock.notYou", { who: tr.by.map((b) => describeRule(b, def)).join(" / ") }));
      for (const c of tr.requires) { const r = checkRequire(tk, c, body); if (r) reasons.push(r); }
      if (tr.to === "$previous" && !tk.previous_state) reasons.push(t("mock.noPrevious"));
      const to = tr.to === "$previous" ? (tk.previous_state ?? tr.to) : tr.to;
      return { name: tr.name, title: tr.title, to, available: reasons.length === 0, reasons, requires: tr.requires.filter((c) => c === "comment" || c === "result"), label_to: wf.states[to]?.label ?? "pending" };
    });
}
const activeRun = (id: ID) => runs.find((r) => r.task_id === id && !r.ended_at) ?? null;
function beginReasons(tk: TaskRow): string[] {
  const rs: string[] = [];
  if (stateOf(tk).label !== "active") rs.push(t("mock.notActive"));
  if (!tk.assignee_id || !principals().includes(tk.assignee_id)) rs.push(t("mock.notAssignee"));
  if (activeRun(tk.id)) rs.push(t("mock.runActive"));
  return rs;
}
function claimReasons(tk: TaskRow): string[] {
  const rs: string[] = [];
  const st = stateOf(tk);
  if (tk.assignee_id) rs.push(t("mock.hasAssignee"));
  if (!(st.claimable || st.label === "active")) rs.push(t("mock.notClaimable"));
  if (tk.required_role && !myRoles().includes(tk.required_role)) rs.push(t("mock.needRole", { role: L(roleOf(tk.required_role)?.title ?? tk.required_role) }));
  return rs;
}
function workflowView(tk: TaskRow): WorkflowAvailability {
  const ar = activeRun(tk.id);
  const br = beginReasons(tk);
  const cr = claimReasons(tk);
  return { task_id: tk.id, state: stateOf(tk), active_run: ar ? { id: ar.id, executor_id: ar.executor_id } : null, transitions: availability(tk), can_begin: br.length === 0, can_claim: cr.length === 0, begin_reasons: br, claim_reasons: cr };
}

function openRun(tk: TaskRow, executor: ID) {
  runs.push({ id: nextId("R"), task_id: tk.id, state: tk.state, executor_id: executor, started_at: nowISO(), ended_at: null, outcome: "running", usage: [] });
  if (!tk.actual_start) tk.actual_start = day(0);
  emit("RunStarted", { task: tk.id, actor: executor, summary: t("mock.ev.runStarted", { name: L(ref(executor).name), stage: L(stateOf(tk).title) }) });
}
function closeRun(tk: TaskRow, outcome: "ended" | "cancelled") {
  const r = activeRun(tk.id);
  if (!r) return;
  r.ended_at = nowISO();
  r.outcome = outcome;
  emit("RunEnded", { task: tk.id, actor: null, summary: t("mock.ev.runEnded", { outcome: t(outcome === "ended" ? "outcome.ended" : "outcome.cancelled") }) });
}

function doTransition(tk: TaskRow, name: string, body: TransitionBody): Task {
  const def = defOf(tk);
  const tr = def.workflow.transitions.find((x) => x.name === name);
  if (!tr) throw new ApiError(404, t("mock.noStep"));
  const av = availability(tk, body).find((x) => x.name === name);
  if (!av) throw new ApiError(409, t("mock.noStepHere"));
  if (!av.available) throw new ApiError(409, av.reasons.join(getLocale() === "zh-CN" ? "；" : "; "));
  const fromState = stateOf(tk);
  const toName = av.to;
  const toState = def.workflow.states[toName];
  if (body.comment?.trim()) {
    tk.comments.push({ id: nextId("C"), kind: "comment", author: ref(ME), body: body.comment.trim(), created_at: nowISO() });
    emit("CommentAdded", { task: tk.id, actor: ME, summary: t("mock.ev.comment", { body: body.comment.trim().slice(0, 40) }) });
  }
  if (fromState.label === "active") {
    tk.previous_state = tk.state;
    closeRun(tk, toState.label === "terminal_failure" ? "cancelled" : "ended");
  }
  if (fromState.weight !== undefined) tk.last_weight = fromState.weight;
  tk.state = toName;
  tk.updated_at = nowISO();
  emit("TaskTransitioned", { task: tk.id, actor: ME, summary: t("mock.ev.transition", { step: L(tr.title), from: L(fromState.title), to: L(toState.title) }), data: { from: fromState.name, to: toName } });
  // 负责人切换
  if (tr.assign_to !== undefined) {
    if (tr.assign_to === null) { tk.assignee_id = null; tk.required_role = null; tk.pending_participant = null; emit("TaskSentToBacklog", { task: tk.id, actor: null, summary: t("mock.ev.clearAssignee") }); }
    else if (tr.assign_to === "creator") { tk.assignee_id = tk.creator_id; emit("TaskAssigned", { task: tk.id, actor: null, summary: t("mock.ev.assignCreator", { name: L(ref(tk.creator_id).name) }) }); }
    else if (tr.assign_to.startsWith("participant:")) {
      const slot = tr.assign_to.slice(12);
      const e = tk.participants[slot];
      if (e) { tk.assignee_id = e; tk.required_role = null; tk.pending_participant = null; emit("TaskAssigned", { task: tk.id, actor: null, summary: t("mock.ev.assignSlot", { slot: L(def.participants[slot].title), name: L(ref(e).name) }) }); }
      else { tk.assignee_id = null; tk.required_role = def.participants[slot].role; tk.pending_participant = slot; emit("TaskSentToBacklog", { task: tk.id, actor: null, summary: t("mock.ev.slotVacant", { slot: L(def.participants[slot].title) }) }); }
    }
  }
  if (toState.label === "terminal_success" || toState.label === "terminal_failure") {
    tk.actual_end = day(0);
    if (toState.label === "terminal_failure") {
      for (const r of [...relations]) {
        if (r.type === "blocks" && r.from === tk.id) {
          relations.splice(relations.indexOf(r), 1);
          emit("RelationRemoved", { task: r.to, actor: null, summary: t("mock.ev.depRemoved", { title: L(tk.title) }) });
        }
      }
    }
  }
  // 进入进行中状态且触发者就是新负责人 → 开始执行记录
  if (toState.label === "active" && tk.assignee_id && principals().includes(tk.assignee_id)) openRun(tk, ME);
  return viewTask(tk);
}

// ---------- 甘特图与统计 ----------
const ganttTask = (tk: TaskRow): GanttTask => ({
  id: tk.id, title: tk.title, type: tk.type, type_title: defOf(tk).title, state: stateOf(tk), assignee: tk.assignee_id ? ref(tk.assignee_id) : null, goal_id: tk.goal_id,
  planned_start: tk.planned_start, planned_end: tk.planned_end, actual_start: tk.actual_start, actual_end: tk.actual_end, progress: progressOf(tk), cost: taskCost(tk.id),
});
function gantt(group: GanttGroup, from: string, to: string, pick: ((team: ID | null) => boolean) | null = null): GanttData {
  const f = parseDate(from) ?? addDays(T0, -14);
  const e = parseDate(to) ?? addDays(T0, 45);
  const inRange = (tk: TaskRow) => {
    const ps = parseDate(tk.planned_start), pe = parseDate(tk.planned_end);
    if (!ps || !pe) return true;
    return pe >= f && ps <= e;
  };
  const all = Object.values(tasks).filter(inRange).filter((tk) => !pick || pick(taskTeam(tk)));
  const rows: GanttRow[] = [];
  const push = (key: string, title: string, list: TaskRow[], goal: GanttRow["goal"] = null) => {
    if (list.length || goal) rows.push({ key, title, goal, tasks: list.map(ganttTask) });
  };
  if (group === "goal") {
    const walk = (parent: ID | null, depth: number) => {
      for (const gr of Object.values(goals).filter((x) => x.parent_id === parent)) {
        const gv = viewGoal(gr, false);
        push(gr.id, `${"　".repeat(depth)}${L(gr.title)}`, all.filter((tk) => tk.goal_id === gr.id), { id: gr.id, planned_start: gr.planned_start, planned_end: gr.planned_end, deadline: gr.deadline ?? null, progress: gv.progress, milestones: goalMilestones(gr.id).map(milestoneMark) });
        walk(gr.id, depth + 1);
      }
    };
    walk(null, 0);
    push("_", t("mock.gantt.noGoal"), all.filter((tk) => !tk.goal_id));
  } else if (group === "team") {
    for (const team of TEAMS) push(team.id, team.name, all.filter((tk) => tk.assignee_id && teamOf(tk.assignee_id) === team.id));
    push("_", t("mock.gantt.noAssignee"), all.filter((tk) => !tk.assignee_id));
  } else if (group === "executor") {
    for (const id of [...Object.keys(MEMBERS), ...Object.keys(AGENTS)]) push(id, ref(id).name, all.filter((tk) => tk.assignee_id === id));
    push("_", t("mock.gantt.noAssignee"), all.filter((tk) => !tk.assignee_id));
  } else {
    for (const def of Object.values(TASK_TYPES)) push(def.name, def.title, all.filter((tk) => tk.type === def.name));
  }
  const ids = new Set(all.map((tk) => tk.id));
  return { group, from: toISODate(f), to: toISODate(e), rows, dependencies: relations.filter((r) => r.type === "blocks" && ids.has(r.from) && ids.has(r.to)).map((r) => ({ from_task_id: r.from, to_task_id: r.to })) };
}
function teamOf(executor: ID): ID | null {
  const m = MEMBERS[executor] ?? (AGENTS[executor] ? MEMBERS[AGENTS[executor].owner.id] : null);
  return m?.team_id ?? null;
}

function costStats(group: CostGroup, pick: ((team: ID | null) => boolean) | null, financial: boolean): CostStat[] {
  if (!financial) return [];
  const buckets = new Map<string, CostStat>();
  const add = (key: string, title: string, cost: number, tokens: number) => {
    const b = buckets.get(key) ?? { key, title, cost: 0, total_tokens: 0, run_count: 0 };
    b.cost += cost; b.total_tokens += tokens; b.run_count += 1;
    buckets.set(key, b);
  };
  for (const r of runs) {
    if (pick && !pick(teamOf(r.executor_id))) continue;
    const tk = tasks[r.task_id];
    if (group === "model") { for (const u of r.usage) add(u.model, u.model, usageCost(u), u.total_tokens); continue; }
    const cost = runCost(r), tokens = runTokens(r);
    if (group === "goal") { const root = tk.goal_id ? rootGoal(tk.goal_id) : null; add(root?.id ?? "_", root?.title ?? t("mock.gantt.noGoal"), cost, tokens); }
    else if (group === "team") { const tid = teamOf(r.executor_id); add(tid ?? "_", TEAMS.find((x) => x.id === tid)?.name ?? t("mock.cost.noTeam"), cost, tokens); }
    else add(r.executor_id, ref(r.executor_id).name, cost, tokens);
  }
  return [...buckets.values()].map((b) => ({ ...b, cost: round2(b.cost) })).sort((a, b) => b.cost - a.cost);
}
function rootGoal(id: ID): GoalRow {
  let gr = goals[id];
  while (gr.parent_id) gr = goals[gr.parent_id];
  return gr;
}
function cycleStats(): CycleStat[] {
  return [
    { type: "generic", type_title: "通用任务", sample: 14, avg_hours: 38.5, median_hours: 26, avg_active_hours: 6.2 },
    { type: "requirement", type_title: "需求", sample: 5, avg_hours: 412, median_hours: 380, avg_active_hours: 71 },
    { type: "bug", type_title: "Bug", sample: 9, avg_hours: 29, median_hours: 18, avg_active_hours: 4.1 },
    { type: "release", type_title: "发布", sample: 3, avg_hours: 52, median_hours: 48, avg_active_hours: 5 },
  ];
}
function throughputStats(): ThroughputStat[] {
  const base = [[6, 4, 1], [8, 5, 0], [7, 7, 2], [9, 6, 1], [5, 8, 0], [10, 7, 1], [8, 9, 1], [6, 3, 1]];
  const monday = startOfWeek(T0);
  return base.map(([created, done, terminated], i) => ({ week: toISODate(addDays(monday, -7 * (7 - i))), created, done, terminated }));
}
function agentStats(): AgentStat[] {
  const fixture: Record<ID, [number, number, number]> = { "li-agent": [23, 19, 3], "zhang-agent": [15, 14, 1], "wang-agent": [11, 9, 2], "shared-doc": [6, 6, 0] };
  return Object.values(AGENTS).map((a) => {
    const [n, acc, rej] = fixture[a.id] ?? [0, 0, 0];
    const rs = runs.filter((r) => r.executor_id === a.id);
    return { agent: ref(a.id), owner: a.owner, runs: n, accepted: acc, rejected: rej, success_rate: n ? acc / n : 0, reject_rate: n ? rej / n : 0, total_tokens: rs.reduce((s, r) => s + runTokens(r), 0), cost: round2(rs.reduce((s, r) => s + runCost(r), 0)) };
  });
}

// ---------- 组织设置、邀请、平台后台的内存数据 ----------
interface InvitationRow extends Invitation { token: string }
const INVITATIONS: InvitationRow[] = [
  { id: "I1", token: "demo", email: "qian@example.com", name: "小钱", roles: ["developer"], url: "", expires_at: at(7), accepted_at: null, created_at: at(-1, 11) },
];
const inviteUrl = (token: string) => `${typeof window !== "undefined" ? window.location.origin : ""}/invite/${token}/`;
const viewInvitation = (i: InvitationRow): Invitation => ({ id: i.id, email: i.email, name: i.name, roles: i.roles, url: inviteUrl(i.token), expires_at: i.expires_at, accepted_at: i.accepted_at, created_at: i.created_at });
const canManageOrg = () => ORG.owner_id === ME || myRoles().some((r) => roleOf(r)?.permissions.includes("org_settings"));
const requireOrgAdmin = () => { requireLogin(); if (!canManageOrg()) throw new ApiError(403, t("mock.forbidden")); };

interface AdminOrgRow { id: ID; slug: string; name: string; currency: string; default_locale: Locale; owner: AdminOrganization["owner"]; member_count: number; agent_count: number; task_count: number; cost_30d: number; deactivated_at: string | null; created_at: string }
const ADMIN_ORGS: AdminOrgRow[] = [
  { id: "org1", slug: "example", name: "示例科技", currency: "CNY", default_locale: "zh-CN", owner: { id: "wang", name: "小王", email: "wang@example.com" }, member_count: 4, agent_count: 4, task_count: 13, cost_30d: 0, deactivated_at: null, created_at: at(-120) },
  { id: "org2", slug: "north", name: "North Labs", currency: "USD", default_locale: "en-US", owner: { id: "amy", name: "Amy", email: "amy@north.example" }, member_count: 9, agent_count: 6, task_count: 41, cost_30d: 312.4, deactivated_at: null, created_at: at(-60) },
  { id: "org3", slug: "old", name: "Old Co", currency: "CNY", default_locale: "zh-CN", owner: null, member_count: 2, agent_count: 0, task_count: 3, cost_30d: 0, deactivated_at: at(-10), created_at: at(-200) },
];
const viewAdminOrg = (o: AdminOrgRow): AdminOrganization => ({ ...o, cost_30d: o.id === "org1" ? round2(runs.reduce((s, r) => s + runCost(r), 0)) : o.cost_30d });
const ADMINS: AdminUser[] = [{ id: "adm1", email: "admin@axiomos.example", name: "Platform admin" }];
let adminLoggedIn = false;
const requireAdmin = () => { if (!adminLoggedIn) throw new ApiError(401, t("mock.admin.required")); };

// ---------- 路由 ----------
type Query = Record<string, string | number | undefined | null> | undefined;
type Handler = (m: RegExpMatchArray, body: unknown, q: Query) => unknown;
const routes: Array<[string, RegExp, Handler]> = [];
const on = (method: string, pattern: string, h: Handler) => routes.push([method, new RegExp(`^${pattern.replace(/:(\w+)/g, "(?<$1>[^/]+)")}$`), h]);
const str = (q: Query, k: string) => (q?.[k] === undefined || q?.[k] === null ? undefined : String(q[k]));
const getTask = (id: string) => { const tk = tasks[id]; if (!tk) throw new ApiError(404, t("mock.task.notFound")); return tk; };
const getGoal = (id: string) => { const gr = goals[id]; if (!gr) throw new ApiError(404, t("mock.goal.notFound")); return gr; };
const requireLogin = () => { if (!loggedIn) throw new ApiError(401, t("mock.login.required")); };

on("POST", "/auth/login", (_m, body) => {
  const b = (body ?? {}) as { email?: string; password?: string };
  if (!b.email) throw new ApiError(400, t("mock.login.needEmail"));
  if (b.password === "wrong") throw new ApiError(401, t("mock.login.bad"));
  const m = Object.values(MEMBERS).find((x) => x.email === b.email && x.active);
  ME = m?.id ?? "wang";
  loggedIn = true;
  return session();
});
on("POST", "/auth/logout", () => { loggedIn = false; return undefined; });
on("GET", "/auth/me", () => { requireLogin(); return session(); });
on("PATCH", "/auth/me", (_m, body) => {
  requireLogin();
  const b = (body ?? {}) as { locale?: string };
  const l = normalizeLocale(b.locale);
  if (l) MEMBERS[ME].locale = l;
  return session();
});

on("GET", "/members", () => { requireLogin(); return Object.values(MEMBERS).filter((m) => m.active).map(viewMember); });
on("GET", "/capabilities", () => { requireLogin(); return Object.fromEntries(CAPABILITIES.map((c) => [c.name, c.title])); });
on("GET", "/goals", (_m, _b, q) => {
  requireLogin();
  // 目标按负责人所属团队归口；子树里有本范围的任务时也留下（不然下钻会看不见挂在别处的目标）
  const pick = scopePick(q);
  const keep = (gr: GoalRow) => !pick || pick(teamOf(gr.owner_id)) || goalSubtreeIds(gr.id).some((gid) => Object.values(tasks).some((tk) => tk.goal_id === gid && pick(taskTeam(tk))));
  return Object.values(goals).filter((gr) => !gr.parent_id && keep(gr)).map((gr) => viewGoal(gr, true));
});
on("POST", "/goals", (_m, body) => {
  requireLogin();
  const b = body as GoalInput;
  if (!b.title?.trim()) throw new ApiError(400, t("mock.goal.emptyTitle"));
  const row: GoalRow = { id: nextId("G"), title: b.title.trim(), description: b.description ?? "", owner_id: b.owner_id ?? ME, parent_id: b.parent_id ?? null, achieved: false, budget: b.budget ?? null, planned_start: b.planned_start ?? null, planned_end: b.planned_end ?? null, actual_start: null, actual_end: null, created_at: nowISO() };
  addGoal(row);
  emit("GoalCreated", { goal: row.id, actor: ME, summary: t("mock.ev.goalCreated", { title: row.title }) });
  return viewGoal(row, true);
});
on("GET", "/goals/:id", (m) => { requireLogin(); return viewGoal(getGoal(m.groups!.id), true); });
on("PATCH", "/goals/:id", (m, body) => {
  requireLogin();
  const gr = getGoal(m.groups!.id);
  const b = body as Partial<GoalInput>;
  if (b.title !== undefined) gr.title = b.title;
  if (b.description !== undefined) gr.description = b.description;
  if (b.owner_id !== undefined) gr.owner_id = b.owner_id;
  if (b.budget !== undefined) gr.budget = b.budget;
  if (b.planned_start !== undefined) gr.planned_start = b.planned_start;
  if (b.planned_end !== undefined) gr.planned_end = b.planned_end;
  if (b.deadline !== undefined) gr.deadline = b.deadline;
  if (b.achieved !== undefined) { gr.achieved = b.achieved; gr.actual_end = b.achieved ? day(0) : null; gr.status = b.achieved ? "achieved" : "active"; }
  if (b.status !== undefined) gr.status = b.status;
  emit("GoalUpdated", { goal: gr.id, actor: ME, summary: t("mock.ev.goalUpdated", { title: L(gr.title) }) });
  return viewGoal(gr, true);
});

// 只有空目标能删；有内容的目标应当放弃（与后端同一条规则）
on("DELETE", "/goals/:id", (m) => {
  requireLogin();
  const gr = getGoal(m.groups!.id);
  const children = Object.values(goals).filter((c) => c.parent_id === gr.id).length;
  const ts = Object.values(tasks).filter((tk) => tk.goal_id === gr.id).length;
  if (children || ts) throw new ApiError(400, t("mock.goal.notEmpty", { children: String(children), tasks: String(ts) }));
  delete goals[gr.id];
  for (const m of goalMilestones(gr.id)) delete milestones[m.id];
  emit("GoalDeleted", { actor: ME, summary: t("mock.ev.goalDeleted", { title: L(gr.title) }) });
  return undefined;
});

// ---------- 里程碑（ADR 0016） ----------
const getMilestone = (id: string) => { const m = milestones[id]; if (!m) throw new ApiError(404, t("mock.ms.notFound")); return m; };
const isoDate = (v: unknown) => typeof v === "string" && /^\d{4}-\d{2}-\d{2}$/.test(v);
const msEvent = (kind: EventKind, m: MilestoneRow, key: "mock.ev.msCreated" | "mock.ev.msUpdated" | "mock.ev.msReached" | "mock.ev.msUnreached" | "mock.ev.msDeleted") => {
  const gr = goals[m.goal_id];
  emit(kind, { goal: m.goal_id, actor: ME, summary: t(key, { goal: L(gr?.title ?? m.goal_id), title: L(m.title), date: m.due_on }), data: { milestone_id: m.id, goal_id: m.goal_id, goal_title: gr?.title, title: m.title, due_on: m.due_on } });
};
on("GET", "/goals/:id/milestones", (m) => { requireLogin(); return goalMilestones(getGoal(m.groups!.id).id).map(viewMilestone); });
on("POST", "/goals/:id/milestones", (m, body) => {
  requireLogin();
  const gr = getGoal(m.groups!.id);
  const b = (body ?? {}) as Partial<MilestoneInput>;
  if (!b.title?.trim()) throw new ApiError(400, t("mock.ms.emptyTitle"));
  if (!isoDate(b.due_on)) throw new ApiError(400, t("mock.ms.badDate"));
  const row: MilestoneRow = { id: nextId("M"), goal_id: gr.id, title: b.title.trim(), description: b.description?.trim() ?? "", due_on: b.due_on!, reached_at: null, created_by: ME, created_at: nowISO(), updated_at: nowISO() };
  milestones[row.id] = row;
  msEvent("MilestoneCreated", row, "mock.ev.msCreated");
  return viewMilestone(row);
});
on("PATCH", "/milestones/:id", (m, body) => {
  requireLogin();
  const row = getMilestone(m.groups!.id);
  const b = (body ?? {}) as Partial<MilestoneInput>;
  if (b.title !== undefined) { if (!b.title.trim()) throw new ApiError(400, t("mock.ms.emptyTitle")); row.title = b.title.trim(); }
  if (b.due_on !== undefined) { if (!isoDate(b.due_on)) throw new ApiError(400, t("mock.ms.badDate")); row.due_on = b.due_on; }
  if (b.description !== undefined) row.description = b.description.trim();
  row.updated_at = nowISO();
  msEvent("MilestoneUpdated", row, "mock.ev.msUpdated");
  return viewMilestone(row);
});
on("DELETE", "/milestones/:id", (m) => {
  requireLogin();
  const row = getMilestone(m.groups!.id);
  delete milestones[row.id];
  msEvent("MilestoneDeleted", row, "mock.ev.msDeleted");
  return undefined;
});
on("POST", "/milestones/:id/reach", (m) => {
  requireLogin();
  const row = getMilestone(m.groups!.id);
  if (row.reached_at) throw new ApiError(400, t("mock.ms.alreadyReached", { title: L(row.title) }));
  row.reached_at = nowISO();
  row.updated_at = nowISO();
  msEvent("MilestoneReached", row, "mock.ev.msReached");
  return viewMilestone(row);
});
on("POST", "/milestones/:id/unreach", (m) => {
  requireLogin();
  const row = getMilestone(m.groups!.id);
  if (!row.reached_at) throw new ApiError(400, t("mock.ms.notReached", { title: L(row.title) }));
  row.reached_at = null;
  row.updated_at = nowISO();
  msEvent("MilestoneUnreached", row, "mock.ev.msUnreached");
  return viewMilestone(row);
});

on("GET", "/tasks", (_m, _b, q) => {
  requireLogin();
  const f: TaskQuery = { goal: str(q, "goal"), assignee: str(q, "assignee"), creator: str(q, "creator"), reviewer: str(q, "reviewer"), state: str(q, "state"), type: str(q, "type"), sprint: str(q, "sprint") };
  const me = principals();
  const pick = scopePick(q);
  const matchExec = (want: string | undefined, actual: ID | null) => !want || (want === "me" ? !!actual && me.includes(actual) : actual === want);
  return Object.values(tasks)
    .filter(keepTask(pick))
    .filter((tk) => (!f.goal || (tk.goal_id ? goalSubtreeIds(f.goal).includes(tk.goal_id) : false)))
    .filter((tk) => matchExec(f.assignee, tk.assignee_id) && matchExec(f.creator, tk.creator_id) && matchExec(f.reviewer, tk.reviewer_id))
    .filter((tk) => !f.state || tk.state === f.state || stateOf(tk).label === f.state)
    .filter((tk) => !f.type || tk.type === f.type)
    .filter((tk) => !f.sprint || tk.sprint_id === f.sprint)
    .sort((a, b) => (b.updated_at > a.updated_at ? 1 : -1))
    .map(viewTask);
});
on("POST", "/tasks", (_m, body) => {
  requireLogin();
  const b = body as TaskInput;
  const def = TASK_TYPES[b.type];
  if (!def) throw new ApiError(400, t("mock.task.badType"));
  if (!b.title?.trim()) throw new ApiError(400, t("mock.task.emptyTitle"));
  const participants: Record<string, ID | null> = Object.fromEntries(Object.keys(def.participants).map((slot) => [slot, b.participants?.[slot] ?? null]));
  const row = addTask({
    id: nextId("T"), goal_id: b.goal_id ?? null, parent_id: b.parent_id ?? null, type: b.type, title: b.title.trim(), description: b.description ?? "", state: def.workflow.initial,
    creator_id: ME, assignee_id: b.assignee_id ?? null, reviewer_id: b.reviewer_id ?? ME, participants, required_capabilities: b.required_capabilities ?? [],
    planned_start: b.planned_start ?? null, planned_end: b.planned_end ?? null, actual_start: null, actual_end: null, estimate: b.estimate ?? null, points: b.points ?? null, sprint_id: b.sprint_id || null, priority: b.priority ?? "normal", fields: b.fields ?? {}, created_at: nowISO(),
  });
  emit("TaskCreated", { task: row.id, actor: ME, summary: t("mock.ev.taskCreated", { type: L(def.title), title: row.title }) });
  return viewTask(row);
});
on("GET", "/tasks/:id", (m) => { requireLogin(); return viewTask(getTask(m.groups!.id)); });
on("PATCH", "/tasks/:id", (m, body) => {
  requireLogin();
  const tk = getTask(m.groups!.id);
  const b = body as Partial<TaskInput>;
  for (const k of ["title", "description", "planned_start", "planned_end", "estimate", "priority", "goal_id"] as const) {
    if (b[k] !== undefined) (tk as unknown as Record<string, unknown>)[k] = b[k];
  }
  if (b.fields) tk.fields = { ...tk.fields, ...b.fields };
  if (b.points !== undefined && b.points !== tk.points) {
    tk.points = b.points === null ? null : Math.max(0, Math.round(Number(b.points)));
    emit("PointsChanged", { task: tk.id, actor: ME, summary: tk.points === null ? t("mock.ev.pointsCleared") : t("mock.ev.pointsChanged", { n: tk.points }), data: { points: tk.points } });
  }
  if (b.sprint_id !== undefined) setTaskSprint(tk, b.sprint_id || null, ME);
  tk.updated_at = nowISO();
  emit("TaskUpdated", { task: tk.id, actor: ME, summary: t("mock.ev.taskUpdated", { title: L(tk.title) }) });
  return viewTask(tk);
});
on("GET", "/tasks/:id/workflow", (m) => { requireLogin(); return workflowView(getTask(m.groups!.id)); });
on("POST", "/tasks/:id/transitions/:name", (m, body) => { requireLogin(); return doTransition(getTask(m.groups!.id), m.groups!.name, (body ?? {}) as TransitionBody); });
on("POST", "/tasks/:id/claim", (m) => {
  requireLogin();
  const tk = getTask(m.groups!.id);
  const rs = claimReasons(tk);
  if (rs.length) throw new ApiError(409, rs.join(getLocale() === "zh-CN" ? "；" : "; "));
  tk.assignee_id = ME;
  if (tk.pending_participant) tk.participants[tk.pending_participant] = ME;
  tk.required_role = null; tk.pending_participant = null; tk.updated_at = nowISO();
  emit("TaskClaimed", { task: tk.id, actor: ME, summary: t("mock.ev.claimed", { name: L(MEMBERS[ME].name) }) });
  if (stateOf(tk).label === "active") openRun(tk, ME);
  return viewTask(tk);
});
on("POST", "/tasks/:id/begin", (m) => {
  requireLogin();
  const tk = getTask(m.groups!.id);
  const rs = beginReasons(tk);
  if (rs.length) throw new ApiError(409, rs.join(getLocale() === "zh-CN" ? "；" : "; "));
  openRun(tk, ME);
  return viewTask(tk);
});
on("POST", "/tasks/:id/assign", (m, body) => {
  requireLogin();
  const tk = getTask(m.groups!.id);
  const b = body as { executor_id: ID | null };
  if (b.executor_id && !MEMBERS[b.executor_id] && !AGENTS[b.executor_id]) throw new ApiError(400, t("mock.executorMissing"));
  tk.assignee_id = b.executor_id;
  tk.updated_at = nowISO();
  emit("TaskAssigned", { task: tk.id, actor: ME, summary: b.executor_id ? t("mock.ev.assigned", { name: L(ref(b.executor_id).name) }) : t("mock.ev.unassigned") });
  return viewTask(tk);
});
on("POST", "/tasks/:id/artifacts", (m, body) => {
  requireLogin();
  const tk = getTask(m.groups!.id);
  const b = body as { type: string; title: string; url: string };
  if (!b.type) throw new ApiError(400, t("mock.pickArtifactType"));
  const a: Artifact = { id: nextId("A"), type: b.type, title: b.title || b.url, url: b.url, attached_by: ref(ME), created_at: nowISO() };
  tk.artifacts.push(a);
  tk.updated_at = nowISO();
  emit("ArtifactAttached", { task: tk.id, actor: ME, summary: t("mock.ev.artifact", { type: L(ARTIFACT_TYPES[b.type] ?? b.type), title: a.title }) });
  return a;
});
on("POST", "/tasks/:id/comments", (m, body) => {
  requireLogin();
  const tk = getTask(m.groups!.id);
  const b = body as { body: string; kind?: "comment" | "note" };
  if (!b.body?.trim()) throw new ApiError(400, t("mock.emptyComment"));
  const c: Comment = { id: nextId("C"), kind: b.kind ?? "comment", author: ref(ME), body: b.body.trim(), created_at: nowISO() };
  tk.comments.push(c);
  tk.updated_at = nowISO();
  emit("CommentAdded", { task: tk.id, actor: ME, summary: t("mock.ev.comment", { body: c.body.slice(0, 40) }) });
  return c;
});
on("POST", "/tasks/:id/relations", (m, body) => {
  requireLogin();
  const tk = getTask(m.groups!.id);
  const b = body as { type: RelationType; from_task_id: ID; to_task_id: ID };
  if (b.from_task_id !== tk.id && b.to_task_id !== tk.id) throw new ApiError(400, t("mock.relationSelf"));
  if (b.from_task_id === b.to_task_id) throw new ApiError(400, t("mock.relationSame"));
  getTask(b.from_task_id); getTask(b.to_task_id);
  if (b.type === "blocks" && wouldCycle(b.from_task_id, b.to_task_id)) throw new ApiError(409, t("mock.relationCycle"));
  const r: RelationRow = { id: nextId("L"), type: b.type, from: b.from_task_id, to: b.to_task_id, created_at: nowISO() };
  relations.push(r);
  emit("TasksLinked", { task: tk.id, actor: ME, summary: t("mock.ev.linked", { from: L(tasks[r.from].title), rel: t(`relation.${b.type}`), to: L(tasks[r.to].title) }) });
  return viewRelation(r);
});
function wouldCycle(from: ID, to: ID): boolean {
  // 若 to 已经（直接或间接）是 from 的前置，则成环
  const seen = new Set<ID>();
  const stack = [from];
  while (stack.length) {
    const cur = stack.pop()!;
    if (cur === to) return true;
    if (seen.has(cur)) continue;
    seen.add(cur);
    for (const r of relations) if (r.type === "blocks" && r.to === cur) stack.push(r.from);
  }
  return false;
}

on("GET", "/backlog", (_m, _b, q) => {
  requireLogin();
  const pick = scopePick(q);
  return Object.values(tasks)
    .filter(keepTask(pick))
    .filter((tk) => !tk.assignee_id && !isTerminalRow(tk) && (stateOf(tk).claimable || stateOf(tk).label === "active"))
    .map((tk): BacklogItem => { const rs = claimReasons(tk); return { task: viewTask(tk), required_role: tk.required_role, required_capabilities: tk.required_capabilities, can_claim: rs.length === 0, reasons: rs }; });
});
on("GET", "/gantt", (_m, _b, q) => { requireLogin(); return gantt((str(q, "group") as GanttGroup) || "goal", str(q, "from") ?? "", str(q, "to") ?? "", scopePick(q)); });

on("GET", "/agents", () => { requireLogin(); return Object.values(AGENTS); });
on("POST", "/agents", (_m, body): AgentRegistration => {
  requireLogin();
  const b = body as AgentInput;
  if (!b.name?.trim()) throw new ApiError(400, t("mock.agentEmptyName"));
  const a: Agent = { id: nextId("agent-"), name: b.name.trim(), owner: ref(ME), shared: !!b.shared, capabilities: b.capabilities ?? [], grants: b.grants ?? [], online: false, last_seen_at: null, max_concurrency: b.max_concurrency ?? 1, created_at: nowISO() };
  AGENTS[a.id] = a;
  emit("AgentRegistered", { actor: ME, summary: t("mock.ev.agentRegistered", { name: a.name }) });
  const token = `axm_${Math.random().toString(36).slice(2)}${Math.random().toString(36).slice(2)}`;
  return { agent: a, token };
});
on("DELETE", "/agents/:id", (m) => {
  requireLogin();
  const a = AGENTS[m.groups!.id];
  if (!a) throw new ApiError(404, t("mock.agentNotFound"));
  delete AGENTS[m.groups!.id];
  emit("AgentRemoved", { actor: ME, summary: t("mock.ev.agentRemoved", { name: L(a.name) }) });
  return undefined;
});

// ---------- 待确认操作（ADR 0003） ----------
/** 有效期过了但还没人处理的，按「已过期」返回（真实后端也会自己把它改过去）。 */
const proposalStatus = (p: ProposalRow): ProposalStatus => (p.status === "pending" && parseDate(p.expires_at)! <= new Date() ? "expired" : p.status);
const proposalOwner = (p: ProposalRow): ID => AGENTS[p.agent_id]?.owner.id ?? ME;
/**
 * 「只看等我确认」：示例里只按"发起 Agent 的所有者是我"判断。
 * 真实后端还包含"我持有这个动作所需的权限"那一类，示例数据不模拟组织权限的交集。
 */
const proposalIsMine = (p: ProposalRow) => proposalOwner(p) === ME;
function viewProposal(p: ProposalRow): Proposal {
  const owner = proposalOwner(p);
  return {
    id: p.id,
    agent: { id: p.agent_id, name: AGENTS[p.agent_id]?.name ?? p.agent_id, kind: "agent" },
    owner: { id: owner, name: MEMBERS[owner]?.name ?? owner, kind: "member" },
    action: p.action,
    action_title: p.action_title,
    grant: p.grant,
    target: p.target,
    summary: p.summary,
    payload: p.payload,
    status: proposalStatus(p),
    can_decide: proposalIsMine(p),
    decided_by: p.decided_by ? { id: p.decided_by, name: MEMBERS[p.decided_by]?.name ?? p.decided_by, kind: "member" } : null,
    decided_at: p.decided_at,
    reason: p.reason,
    created_at: p.created_at,
    expires_at: p.expires_at,
  };
}
const getProposal = (id: string) => { const p = proposals.find((x) => x.id === id); if (!p) throw new ApiError(404, t("mock.proposal.notFound")); return p; };
/** 确认 / 拒绝前的检查：只有所有者能处理，且只能处理还在等的那些。 */
function proposalDecidable(p: ProposalRow) {
  if (!proposalIsMine(p)) throw new ApiError(403, t("mock.proposal.forbidden"));
  const st = proposalStatus(p);
  if (st === "expired") throw new ApiError(409, t("mock.proposal.expired"));
  if (st !== "pending") throw new ApiError(409, t("mock.proposal.decided"));
}
on("GET", "/proposals", (_m, _b, q) => {
  requireLogin();
  const status = str(q, "status");
  const agent = str(q, "agent");
  const mine = str(q, "mine") === "1";
  return proposals
    .filter((p) => (!status || proposalStatus(p) === status) && (!agent || p.agent_id === agent) && (!mine || proposalIsMine(p)))
    .sort((a, b) => b.created_at.localeCompare(a.created_at))
    .map(viewProposal);
});
on("GET", "/proposals/count", (): ProposalCount => {
  requireLogin();
  return { pending: proposals.filter((p) => proposalStatus(p) === "pending" && proposalIsMine(p)).length };
});
on("GET", "/proposals/:id", (m) => { requireLogin(); return viewProposal(getProposal(m.groups!.id)); });
on("POST", "/proposals/:id/approve", (m): ProposalApproval => {
  requireLogin();
  const p = getProposal(m.groups!.id);
  proposalDecidable(p);
  p.status = "approved";
  p.decided_by = ME;
  p.decided_at = nowISO();
  // 确认后立刻执行：示例里只做得到的那一件——领取任务真的把任务给发起的 Agent
  let result: unknown = null;
  if (p.action === "task.claim" && p.target?.kind === "task" && tasks[p.target.id]) {
    const tk = tasks[p.target.id];
    tk.assignee_id = p.agent_id;
    tk.pending_participant = null;
    tk.updated_at = nowISO();
    emit("TaskClaimed", { task: tk.id, actor: p.agent_id, summary: t("mock.ev.claimed", { name: L(AGENTS[p.agent_id].name) }) });
    result = viewTask(tk);
  }
  emit("ProposalApproved", { actor: p.agent_id, task: p.target?.kind === "task" ? p.target.id : null, summary: t("mock.ev.proposalApproved", { action: L(p.action_title), name: L(MEMBERS[ME].name) }), data: { proposal_id: p.id } });
  return { proposal: viewProposal(p), result };
});
on("POST", "/proposals/:id/reject", (m, body) => {
  requireLogin();
  const p = getProposal(m.groups!.id);
  proposalDecidable(p);
  const reason = ((body ?? {}) as { reason?: string }).reason?.trim() ?? "";
  if (reason.length < 6) throw new ApiError(400, t("mock.proposal.needReason"));
  p.status = "rejected";
  p.decided_by = ME;
  p.decided_at = nowISO();
  p.reason = reason;
  emit("ProposalRejected", { actor: ME, task: p.target?.kind === "task" ? p.target.id : null, summary: t("mock.ev.proposalRejected", { action: L(p.action_title), reason }), data: { proposal_id: p.id } });
  return viewProposal(p);
});

// ---------- 待我处理与站内通知（DESIGN.md §12） ----------
interface NotificationRow { id: number; member_id: ID; title: string; body: string; task_id: ID | null; read_at: string | null; created_at: string }
const notifications: NotificationRow[] = [
  { id: 1, member_id: "wang", title: "「留存漏斗分析」已提交，等你验收", body: "小张的测试 Agent 提交了结果：报告里的口径和上次一致。", task_id: "T13", read_at: null, created_at: at(-1, 17, 2) },
  { id: 2, member_id: "wang", title: "「第 7 次迭代」还剩 2 天", body: "12 个任务里还有 5 个没到验收，其中 1 个逾期。", task_id: null, read_at: null, created_at: at(0, 8) },
  { id: 3, member_id: "wang", title: "小李的编码 Agent 在「导出报表」上附了代码 PR", body: "feat: weekly CSV export #4，等测试角色领取后进入测试。", task_id: "T6", read_at: null, created_at: at(-4, 18, 3) },
  { id: 4, member_id: "wang", title: "小王的写作助手已离线一天", body: "上次心跳是昨天 18:00，名下没有进行中的执行记录。", task_id: null, read_at: at(-1, 19), created_at: at(-1, 18, 30) },
  { id: 5, member_id: "li", title: "「金额显示错位」已确认并指派给你", body: "小张确认了这个 Bug，严重程度：中，在第 7 次迭代里。", task_id: "T2", read_at: null, created_at: at(-3, 14, 31) },
  { id: 6, member_id: "li", title: "小王答复了你的 Agent 在「登录页改版」里的提问", body: "用品牌蓝，深灰那处是笔误。任务已回到开发中。", task_id: "T1", read_at: null, created_at: at(-6, 14, 6) },
];
const viewNotification = (n: NotificationRow): Notification => ({ id: n.id, member_id: n.member_id, title: n.title, body: n.body, task_id: n.task_id, read_at: n.read_at, created_at: n.created_at });
const INBOX_TITLES: Record<InboxKind, string> = {
  overdue: "我负责但逾期的任务", proposals: "等我确认的待确认操作", review: "等我验收的任务", questions: "等我答复的提问", unstarted: "指派给我但没开始的任务", notifications: "未读通知",
};
/** 从当前状态出发、由我（按参与规则）能触发的步骤 */
const stepsForMe = (tk: TaskRow) => {
  const me = principals();
  const who = (rule: string) => (rule === "assignee" ? !!tk.assignee_id && me.includes(tk.assignee_id) : rule === "reviewer" ? tk.reviewer_id === ME : rule === "creator" ? tk.creator_id === ME : rule === "anyone");
  return defOf(tk).workflow.transitions.filter((tr) => (tr.from.includes("*") || tr.from.includes(tk.state)) && tr.by.some(who));
};
/**
 * 待我处理的六组（与真实后端同一套规则，只认状态类型与步骤声明）：
 * 逾期 = 我负责、计划结束日早于今天、没结束；待确认 = 等人确认且我能确认；待验收 = 停在等待类型、从这里有一步带「验收」授权且由我触发；
 * 待答复 = 停在等待类型、从这里有一步要求评论、不带验收授权且由我触发，且最后一条评论不是我写的；未开始 = 我负责、处于未开始类型；通知 = 我的未读。
 * 同一任务可能出现在两组（例如逾期且待验收），count 是各组之和，不去重。
 */
function inboxGroups(): InboxGroup[] {
  const me = principals();
  const open = Object.values(tasks).filter((tk) => !isTerminalRow(tk));
  const mine = open.filter((tk) => tk.assignee_id && me.includes(tk.assignee_id));
  const overdue: InboxTask[] = mine
    .filter((tk) => tk.planned_end && parseDate(tk.planned_end)! < T0)
    .map((tk) => ({ ...viewTask(tk), days_overdue: Math.round((T0.getTime() - parseDate(tk.planned_end)!.getTime()) / 86_400_000) }))
    .sort((a, b) => b.days_overdue - a.days_overdue);
  const pending = proposals.filter((p) => proposalStatus(p) === "pending" && proposalIsMine(p)).sort((a, b) => b.created_at.localeCompare(a.created_at)).map(viewProposal);
  const waiting = open.filter((tk) => stateOf(tk).label === "waiting");
  const review: InboxTask[] = waiting.filter((tk) => stepsForMe(tk).some((tr) => tr.grant === "review")).map((tk) => ({ ...viewTask(tk), days_overdue: 0 }));
  const questions: InboxQuestion[] = waiting.flatMap((tk) => {
    if (!stepsForMe(tk).some((tr) => tr.requires.includes("comment") && tr.grant !== "review")) return [];
    const last = tk.comments.filter((c) => c.kind === "comment").at(-1);
    if (!last || me.includes(last.author.id)) return [];
    return [{ task: viewTask(tk), comment: last }];
  });
  const unstarted: InboxTask[] = mine.filter((tk) => stateOf(tk).label === "pending").map((tk) => ({ ...viewTask(tk), days_overdue: 0 }));
  const unread = notifications.filter((n) => n.member_id === ME && !n.read_at).sort((a, b) => b.created_at.localeCompare(a.created_at)).map(viewNotification);
  const all: InboxGroup[] = [
    { kind: "overdue", title: INBOX_TITLES.overdue, count: overdue.length, items: overdue },
    { kind: "proposals", title: INBOX_TITLES.proposals, count: pending.length, items: pending },
    { kind: "review", title: INBOX_TITLES.review, count: review.length, items: review },
    { kind: "questions", title: INBOX_TITLES.questions, count: questions.length, items: questions },
    { kind: "unstarted", title: INBOX_TITLES.unstarted, count: unstarted.length, items: unstarted },
    { kind: "notifications", title: INBOX_TITLES.notifications, count: unread.length, items: unread },
  ];
  return INBOX_KINDS.map((k) => all.find((g) => g.kind === k)!).filter((g) => g.count > 0);
}
on("GET", "/inbox", (): Inbox => {
  requireLogin();
  const groups = inboxGroups();
  const count = groups.reduce((n, g) => n + g.count, 0);
  return count ? { count, groups } : { count: 0, groups: [], empty: t("mock.inbox.empty") };
});
on("GET", "/inbox/count", (): InboxCount => {
  requireLogin();
  const groups = inboxGroups();
  return { count: groups.reduce((n, g) => n + g.count, 0), by_kind: Object.fromEntries(groups.map((g) => [g.kind, g.count])) };
});
on("POST", "/notifications/read", (_m, body) => {
  requireLogin();
  const ids = ((body ?? {}) as { ids?: unknown }).ids;
  if (!Array.isArray(ids)) throw new ApiError(400, t("mock.inbox.needIds"));
  for (const n of notifications) if (n.member_id === ME && !n.read_at && ids.includes(n.id)) n.read_at = nowISO();
  return { count: notifications.filter((n) => n.member_id === ME && !n.read_at).length };
});
on("GET", "/notifications", (_m, _b, q) => {
  requireLogin();
  const mine = notifications.filter((n) => n.member_id === ME).sort((a, b) => b.created_at.localeCompare(a.created_at));
  const out = mine.map(viewNotification);
  if (str(q, "read") === "1") for (const n of mine) if (!n.read_at) n.read_at = nowISO();
  return out;
});

on("GET", "/task-types", () => { requireLogin(); return Object.values(TASK_TYPES); });
on("GET", "/task-types/:name", (m) => { requireLogin(); const d = TASK_TYPES[m.groups!.name]; if (!d) throw new ApiError(404, t("mock.typeNotFound")); return d; });


// ---------- 可见性策略、共享边界与范围（ADR 0013） ----------
// 组织的两项策略，系统只给默认值；组织设置页改完立刻生效（这一份就是 mock 的"数据库"）。
const ORG_SETTINGS: OrgSettings = { collaboration_visibility: "org", finance_visibility: "team_tree" };

const childTeams = (parent: ID | null) => TEAMS.filter((x) => x.parent_id === parent);
const teamRow = (id: ID) => TEAMS.find((x) => x.id === id) ?? null;
function teamSubtree(id: ID): ID[] {
  const out = [id];
  for (const c of childTeams(id)) out.push(...teamSubtree(c.id));
  return out;
}
/** 从一个团队沿上级往上找最近的共享边界（自己也算）；没有就是 null。 */
function boundaryOf(teamId: ID | null): OrgTeam | null {
  let cur = teamId ? teamRow(teamId) : null;
  const seen = new Set<ID>();
  while (cur && !seen.has(cur.id)) {
    if (cur.is_boundary) return cur;
    seen.add(cur.id);
    cur = cur.parent_id ? teamRow(cur.parent_id) : null;
  }
  return null;
}
const permsOf = (id: ID) => [...new Set((MEMBERS[id]?.roles ?? []).flatMap((r) => roleOf(r)?.permissions ?? []))];

interface Visibility { teams: ID[] | null; boundary: OrgTeam | null; free: boolean }
/**
 * 某个成员在某一类数据上能看到哪些团队。teams = null 表示全组织。
 * 组织负责人与持有「查看全部工作」/「查看全部成本」的人跨域，不受策略限制。
 */
function visibleTeams(memberId: ID, kind: "work" | "finance"): Visibility {
  const policy = kind === "work" ? ORG_SETTINGS.collaboration_visibility : ORG_SETTINGS.finance_visibility;
  const free = ORG.owner_id === memberId || permsOf(memberId).includes(kind === "work" ? "view_all_work" : "view_all_cost");
  if (free || policy === "org") return { teams: null, boundary: null, free };
  const mine = MEMBERS[memberId]?.team_id ?? null;
  if (policy === "team_tree") return { teams: mine ? teamSubtree(mine) : [], boundary: null, free };
  const b = boundaryOf(mine);
  return { teams: b ? teamSubtree(b.id) : null, boundary: b, free };
}

/** 会话里的可切范围：协作策略决定列出哪些档，财务策略决定每一档能不能看钱。 */
function scopeOptions(): ScopeOption[] {
  const work = visibleTeams(ME, "work");
  const money = visibleTeams(ME, "finance");
  const financial = (scope: string) => (money.teams === null ? true : scope !== "all" && money.teams.includes(scope));
  const out: ScopeOption[] = [];
  if (work.teams === null) {
    out.push({ id: "all", title: t("scope.all"), depth: 0, financial: financial("all") });
    const walk = (tm: OrgTeam, depth: number) => {
      out.push({ id: tm.id, title: tm.name, depth, financial: financial(tm.id) });
      for (const c of childTeams(tm.id)) walk(c, depth + 1);
    };
    for (const r of childTeams(null)) walk(r, 1);
    return out;
  }
  const visible = new Set(work.teams);
  const walk = (id: ID, depth: number) => {
    const tm = teamRow(id);
    if (!tm) return;
    out.push({ id, title: tm.name, depth, financial: financial(id) });
    for (const c of childTeams(id)) if (visible.has(c.id)) walk(c.id, depth + 1);
  };
  for (const id of work.teams) {
    const tm = teamRow(id);
    if (tm && (!tm.parent_id || !visible.has(tm.parent_id))) walk(id, 0);
  }
  return out;
}

/** 协作数据的归口：谁在做就算谁的团队；还没人领时先算创建人的团队。 */
function taskTeam(tk: TaskRow): ID | null {
  return teamOf(tk.assignee_id ?? tk.creator_id);
}
type Pick = (team: ID | null) => boolean;
/** 当前请求要筛掉哪些团队的协作数据；null = 不筛。越权的范围直接 403。 */
function scopePick(q: Query): Pick | null {
  const scope = str(q, "scope") || "all";
  const work = visibleTeams(ME, "work");
  if (scope === "all") {
    if (work.teams === null) return null;
    const set = new Set(work.teams);
    return (team) => !!team && set.has(team);
  }
  if (work.teams !== null && !work.teams.includes(scope)) {
    throw new ApiError(403, work.boundary ? t("mock.scope.deniedBoundary", { name: L(work.boundary.name) }) : t("mock.scope.deniedTeamTree"));
  }
  const set = new Set(teamSubtree(scope));
  return (team) => !!team && set.has(team);
}
/** 这次请求能不能看钱。 */
function scopeFinancial(q: Query): boolean {
  const scope = str(q, "scope") || "all";
  const money = visibleTeams(ME, "finance");
  return money.teams === null ? true : scope !== "all" && money.teams.includes(scope);
}
const keepTask = (pick: Pick | null) => (tk: TaskRow) => !pick || pick(taskTeam(tk));

// ---------- 组织概览（/stats/overview、/stats/exceptions、/stats/load） ----------
interface Span { from: string; to: string }
const PERIOD_DAYS: Record<OverviewPeriod, number> = { week: 7, month: 30, quarter: 90 };
const PERIOD_BUCKET: Record<OverviewPeriod, number> = { week: 1, month: 3, quarter: 7 };
const spanOf = (endOffset: number, days: number): Span => ({ from: day(endOffset - days + 1), to: day(endOffset) });
const inSpan = (iso: string | null | undefined, sp: Span) => !!iso && iso.slice(0, 10) >= sp.from && iso.slice(0, 10) <= sp.to;
const goalTeam = (gr: GoalRow) => teamOf(gr.owner_id);

/** 一个组织单元这一段时间的数字。pick 决定哪些团队算进来。 */
function unitNumbers(pick: Pick, cur: Span, prev: Span, financial: boolean): Omit<OverviewUnit, "id" | "title" | "kind"> {
  const ts = Object.values(tasks).filter((tk) => pick(taskTeam(tk)));
  const done = ts.filter((tk) => stateOf(tk).label === "terminal_success");
  const todayISO = day(0);
  const overdue = ts.filter((tk) => !isTerminalRow(tk) && tk.planned_end && tk.planned_end < todayISO).length;
  // 成本归口：执行者所属团队（ADR 0013，唯一口径）
  const costIn = (sp: Span) => round2(runs.filter((r) => pick(teamOf(r.executor_id)) && inSpan(r.started_at, sp)).reduce((sum, r) => sum + runCost(r), 0));
  const gs = Object.values(goals).filter((gr) => pick(goalTeam(gr)));
  const budgetTotal = gs.reduce((sum, gr) => sum + (gr.budget ?? 0), 0);
  const budget = budgetTotal > 0 ? budgetTotal : null;
  const cost = costIn(cur);
  const progress = gs.length ? Math.round(gs.reduce((sum, gr) => sum + viewGoal(gr, false).progress, 0) / gs.length) : 0;
  return {
    goal_progress: progress,
    tasks_total: ts.length,
    tasks_done: done.length,
    overdue,
    cost: financial ? cost : null,
    budget: financial ? budget : null,
    budget_used_pct: financial && budget ? Math.round((cost / budget) * 100) : null,
    throughput: done.filter((tk) => inSpan(tk.actual_end, cur)).length,
    prev: { cost: financial ? costIn(prev) : null, throughput: done.filter((tk) => inSpan(tk.actual_end, prev)).length },
  };
}

function trendPoints(pick: Pick, sp: Span, bucketDays: number, days: number, financial: boolean): TrendPoint[] {
  const start = parseDate(sp.from)!;
  const buckets = Math.ceil(days / bucketDays);
  return Array.from({ length: buckets }, (_, i) => {
    const b: Span = { from: toISODate(addDays(start, i * bucketDays)), to: toISODate(addDays(start, Math.min(days - 1, (i + 1) * bucketDays - 1))) };
    const cost = round2(runs.filter((r) => pick(teamOf(r.executor_id)) && inSpan(r.started_at, b)).reduce((sum, r) => sum + runCost(r), 0));
    const rows = Object.values(tasks).filter((tk) => pick(taskTeam(tk)));
    return {
      bucket: b.from,
      cost: financial ? cost : null,
      done: rows.filter((tk) => stateOf(tk).label === "terminal_success" && inSpan(tk.actual_end, b)).length,
      created: rows.filter((tk) => inSpan(tk.created_at, b)).length,
    };
  });
}

function overview(q: Query): OverviewData {
  const scope = str(q, "scope") || "all";
  const period = (str(q, "period") as OverviewPeriod) || "month";
  const days = PERIOD_DAYS[period] ?? 30;
  const cur = spanOf(0, days);
  const prev = spanOf(-days, days);
  const outer = scopePick(q);
  const financial = scopeFinancial(q);
  const within: Pick = (team) => !outer || outer(team);
  const units: OverviewUnit[] = [];
  const push = (id: ID, title: string, kind: OverviewUnit["kind"], pick: Pick) => {
    const n = unitNumbers(pick, cur, prev, financial);
    if (kind === "unassigned" && !n.tasks_total && !n.cost) return;
    units.push({ id, title, kind, ...n });
  };
  const teamPick = (root: ID): Pick => {
    const set = new Set(teamSubtree(root));
    return (team) => !!team && set.has(team);
  };
  if (scope === "all") {
    for (const tm of childTeams(null)) if (within(tm.id)) push(tm.id, tm.name, "team", teamPick(tm.id));
    push("_", t("overview.unassigned"), "unassigned", (team) => !team);
  } else {
    for (const c of childTeams(scope)) push(c.id, c.name, "team", teamPick(c.id));
    // 本级直属：这个团队自己名下的人，不属于任何下级组
    const self = teamRow(scope);
    if (self) push(self.id, t("overview.selfUnit", { name: self.name }), "team", (team) => team === self.id);
  }
  return {
    scope,
    period,
    range: cur,
    prev_range: prev,
    units,
    totals: unitNumbers(within, cur, prev, financial),
    trend: trendPoints(within, cur, PERIOD_BUCKET[period] ?? 3, days, financial),
    prev_trend: trendPoints(within, prev, PERIOD_BUCKET[period] ?? 3, days, financial),
  };
}

const exceptionTask = (tk: TaskRow): ExceptionTask => ({
  id: tk.id,
  title: tk.title,
  state: stateOf(tk),
  assignee: tk.assignee_id ? ref(tk.assignee_id) : null,
  goal: tk.goal_id && goals[tk.goal_id] ? { id: tk.goal_id, title: goals[tk.goal_id].title } : null,
  planned_end: tk.planned_end,
  overdue_days: tk.planned_end ? Math.max(0, diffDays(parseDate(tk.planned_end)!, T0)) : 0,
});

function exceptions(q: Query): ExceptionsData {
  const pick = scopePick(q);
  const financial = scopeFinancial(q);
  const keep = keepTask(pick);
  const todayISO = day(0);
  const open = Object.values(tasks).filter((tk) => keep(tk) && !isTerminalRow(tk));
  const myGoals = Object.values(goals).filter((gr) => !pick || pick(goalTeam(gr)));
  const goalView = (gr: GoalRow): ExceptionGoal => {
    const v = viewGoal(gr, false);
    return {
      id: gr.id,
      title: gr.title,
      owner: v.owner,
      progress: v.progress,
      achieved: gr.achieved,
      planned_end: gr.planned_end,
      overdue_days: gr.planned_end ? Math.max(0, diffDays(parseDate(gr.planned_end)!, T0)) : 0,
      budget: financial ? gr.budget : null,
      cost: financial ? v.cost : null,
      all_tasks_done: v.task_count > 0 && v.task_count === v.done_task_count,
    };
  };
  return {
    overdue_tasks: open.filter((tk) => tk.planned_end && tk.planned_end < todayISO).map(exceptionTask),
    overdue_goals: myGoals.filter((gr) => !gr.achieved && gr.planned_end && gr.planned_end < todayISO).map(goalView),
    over_budget_goals: financial ? myGoals.filter((gr) => gr.budget != null && viewGoal(gr, false).cost > gr.budget).map(goalView) : [],
    stuck_tasks: open
      .filter((tk) => diffDays(parseDate(tk.updated_at)!, new Date()) >= 5)
      .map((tk) => ({ task: exceptionTask(tk), days_in_state: diffDays(parseDate(tk.updated_at)!, new Date()) }))
      .sort((a, b) => b.days_in_state - a.days_in_state),
    pending_proposals: proposals.filter((pr) => proposalStatus(pr) === "pending" && proposalIsMine(pr)).map(viewProposal),
  };
}

function loadRows(q: Query): LoadRow[] {
  const pick = scopePick(q);
  const keep = (team: ID | null) => !pick || pick(team);
  const monday = startOfWeek(new Date());
  const week: Span = { from: toISODate(monday), to: toISODate(addDays(monday, 6)) };
  const todayISO = day(0);
  const ids = [
    ...Object.values(MEMBERS).filter((m) => m.active && keep(m.team_id)).map((m) => m.id),
    ...Object.values(AGENTS).filter((a) => keep(teamOf(a.id))).map((a) => a.id),
  ];
  const rows = ids.map((id): LoadRow => {
    const mine = Object.values(tasks).filter((tk) => tk.assignee_id === id && !isTerminalRow(tk));
    const team = teamOf(id);
    const tm = team ? teamRow(team) : null;
    const agent = AGENTS[id];
    const overlapsWeek = (tk: TaskRow) => (tk.planned_start ?? "9999") <= week.to && (tk.planned_end ?? "0000") >= week.from;
    return {
      executor: ref(id),
      team: tm ? { id: tm.id, title: tm.name } : null,
      open_tasks: mine.length,
      active_tasks: mine.filter((tk) => stateOf(tk).label === "active").length,
      points_open: mine.reduce((sum, tk) => sum + (tk.points ?? 0), 0),
      planned_hours_this_week: mine.filter(overlapsWeek).reduce((sum, tk) => sum + (tk.estimate ?? 0), 0),
      overdue: mine.filter((tk) => tk.planned_end && tk.planned_end < todayISO).length,
      capacity_hint: null,
      ...(agent ? { max_concurrent: agent.max_concurrency, online: agent.online } : {}),
    };
  });
  return rows.sort((a, b) => b.points_open - a.points_open || b.open_tasks - a.open_tasks);
}

on("GET", "/stats/overview", (_m, _b, q) => { requireLogin(); return overview(q); });
on("GET", "/stats/exceptions", (_m, _b, q) => { requireLogin(); return exceptions(q); });
on("GET", "/stats/load", (_m, _b, q) => { requireLogin(); return loadRows(q); });

// 可见范围设置：读所有登录成员都能读（会话里也带一份），写要「组织设置」权限
on("GET", "/org/settings", () => { requireLogin(); return { ...ORG_SETTINGS }; });
on("PATCH", "/org/settings", (_m, body) => {
  requireOrgAdmin();
  const b = (body ?? {}) as Partial<OrgSettings>;
  const ok = ["org", "boundary", "team_tree"];
  if (b.collaboration_visibility && ok.includes(b.collaboration_visibility)) ORG_SETTINGS.collaboration_visibility = b.collaboration_visibility;
  if (b.finance_visibility && ok.includes(b.finance_visibility)) ORG_SETTINGS.finance_visibility = b.finance_visibility;
  emit("AgentUpdated", { actor: ME, summary: t("mock.ev.visibilityChanged") });
  return { ...ORG_SETTINGS };
});
on("GET", "/org/visibility-preview", (_m, _b, q): VisibilityPreview => {
  requireOrgAdmin();
  const id = str(q, "member") ?? "";
  const m = MEMBERS[id];
  if (!m) throw new ApiError(404, t("mock.org.memberNotFound"));
  const work = visibleTeams(id, "work");
  const money = visibleTeams(id, "finance");
  const workIds = work.teams === null ? TEAMS.map((x) => x.id) : work.teams;
  const moneyIds = money.teams === null ? TEAMS.map((x) => x.id) : money.teams;
  const depthOf = (tid: ID): number => { const tm = teamRow(tid); return tm?.parent_id ? 1 + depthOf(tm.parent_id) : 0; };
  const b = boundaryOf(m.team_id);
  return {
    member_id: m.id,
    member_name: m.name,
    team_ids: m.team_id ? [m.team_id] : [],
    collaboration_visibility: ORG_SETTINGS.collaboration_visibility,
    finance_visibility: ORG_SETTINGS.finance_visibility,
    sees_all_work: work.teams === null,
    sees_all_cost: money.teams === null,
    cross_boundary_by_role: ORG.owner_id !== m.id && (work.free || money.free),
    boundaries: b ? [b.id] : [],
    teams: TEAMS.map((tm) => ({ id: tm.id, name: tm.name, depth: depthOf(tm.id), is_boundary: !!tm.is_boundary, mine: m.team_id === tm.id, work: workIds.includes(tm.id), finance: moneyIds.includes(tm.id) })),
    visible_team_ids: workIds,
    finance_visible_team_ids: moneyIds,
  };
});

on("GET", "/stats/cost", (_m, _b, q) => { requireLogin(); return costStats((str(q, "group") as CostGroup) || "goal", scopePick(q), scopeFinancial(q)); });
on("GET", "/stats/cycle", () => { requireLogin(); return cycleStats(); });
on("GET", "/stats/throughput", () => { requireLogin(); return throughputStats(); });
on("GET", "/stats/agents", (_m, _b, q) => { requireLogin(); const pick = scopePick(q); return agentStats().filter((a) => !pick || pick(teamOf(a.agent.id))); });

on("GET", "/events", (_m, _b, q) => {
  requireLogin();
  const task = str(q, "task");
  const limit = Number(str(q, "limit") ?? 50);
  const pick = scopePick(q);
  const inScope = (e: Event) => !pick || !e.task_id || !tasks[e.task_id] || pick(taskTeam(tasks[e.task_id]));
  return events.filter((e) => (!task || e.task_id === task) && inScope(e)).slice().sort((a, b) => (a.created_at < b.created_at ? 1 : -1)).slice(0, limit);
});

// ---------- 看板与迭代（ADR 0012） ----------
const LABEL_ORDER: StateLabel[] = ["pending", "active", "waiting", "terminal_success", "terminal_failure"];
const PRIORITY_RANK: Record<Task["priority"], number> = { urgent: 0, high: 1, normal: 2, low: 3 };
const summaryOf = (tk: TaskRow): Omit<BoardCard, "can_move_to"> => {
  const v = viewTask(tk);
  return { id: v.id, goal_id: v.goal_id, goal: v.goal, type: v.type, type_title: v.type_title, title: v.title, state: v.state, assignee: v.assignee, planned_start: v.planned_start, planned_end: v.planned_end, priority: v.priority, progress: v.progress, cost: v.cost, points: v.points, sprint: v.sprint, estimate: v.estimate };
};
const byBoardOrder = (a: TaskRow, b: TaskRow) => PRIORITY_RANK[a.priority] - PRIORITY_RANK[b.priority] || (a.planned_end ?? "9999").localeCompare(b.planned_end ?? "9999") || a.created_at.localeCompare(b.created_at);
function board(q: Query): BoardData {
  const typeName = str(q, "type");
  const def = typeName ? TASK_TYPES[typeName] : undefined;
  if (typeName && !def) throw new ApiError(400, t("mock.task.badType"));
  const goal = str(q, "goal"), team = str(q, "team"), assignee = str(q, "assignee"), sprint = str(q, "sprint");
  const lane = (str(q, "lane") ?? "none") as BoardLane;
  const me = principals();
  const rows = Object.values(tasks)
    .filter(keepTask(scopePick(q)))
    .filter((tk) => !def || tk.type === def.name)
    .filter((tk) => !goal || (tk.goal_id ? goalSubtreeIds(goal).includes(tk.goal_id) : false))
    .filter((tk) => !team || (!!tk.assignee_id && teamOf(tk.assignee_id) === team))
    .filter((tk) => !assignee || (assignee === "me" ? !!tk.assignee_id && me.includes(tk.assignee_id) : tk.assignee_id === assignee))
    .filter((tk) => !sprint || tk.sprint_id === sprint)
    .sort(byBoardOrder);
  const card = (tk: TaskRow): BoardCard => {
    const av = availability(tk).filter((x) => x.available);
    const targets = def ? av.map((x) => x.to) : av.map((x) => x.label_to);
    return { ...summaryOf(tk), can_move_to: [...new Set(targets)] };
  };
  const columns: BoardColumn[] = def
    ? Object.values(def.workflow.states).map((st) => {
        const cards = rows.filter((tk) => tk.state === st.name).map(card);
        return { state: st, wip_limit: st.wip_limit ?? null, count: cards.length, over_limit: st.wip_limit != null && cards.length > st.wip_limit, cards };
      })
    : LABEL_ORDER.map((label) => {
        const cards = rows.filter((tk) => stateOf(tk).label === label).map(card);
        return { state: { name: label, title: t(`label.${label}`), label }, wip_limit: null, count: cards.length, over_limit: false, cards };
      });
  let lanes: BoardData["lanes"];
  if (lane === "goal") {
    const ids = [...new Set(rows.map((tk) => tk.goal_id ?? "_"))];
    lanes = ids.map((id) => ({ key: id, title: id === "_" ? t("mock.gantt.noGoal") : goals[id].title }));
  } else if (lane === "assignee") {
    const ids = [...new Set(rows.map((tk) => tk.assignee_id ?? "_"))];
    lanes = ids.map((id) => ({ key: id, title: id === "_" ? t("mock.gantt.noAssignee") : ref(id).name }));
  }
  return { type: def ? { name: def.name, title: def.title } : null, columns, lanes };
}
on("GET", "/board", (_m, _b, q) => { requireLogin(); return board(q); });

const sprintTasks = (sp: SprintRow) => Object.values(tasks).filter((tk) => tk.sprint_id === sp.id);
const doneRow = (tk: TaskRow) => stateOf(tk).label === "terminal_success";
const pointsOf = (tk: TaskRow) => tk.points ?? 0;
function viewSprint(sp: SprintRow): Sprint {
  const ts = sprintTasks(sp);
  const snap = ts.length === 0 && sp.snapshot ? sp.snapshot : null;
  const team = sp.team_id ? TEAMS.find((x) => x.id === sp.team_id) : null;
  return {
    id: sp.id, team: team ? { id: team.id, title: team.name } : null, name: sp.name, goal: sp.goal, starts_on: sp.starts_on, ends_on: sp.ends_on, status: sp.status,
    task_count: snap ? snap.task_count : ts.length,
    points_total: snap ? snap.points_total : ts.reduce((s, tk) => s + pointsOf(tk), 0),
    points_done: snap ? snap.points_done : ts.filter(doneRow).reduce((s, tk) => s + pointsOf(tk), 0),
    created_by: sp.created_by, started_at: sp.started_at, closed_at: sp.closed_at, created_at: sp.created_at,
  };
}
/** 燃尽：按天回放。有工作量就按工作量，否则按任务数；实际线只画到今天（或结束那天）。 */
function burndown(sp: SprintRow): Burndown {
  const ts = sprintTasks(sp);
  const usePoints = ts.some((tk) => tk.points != null);
  const unit: Burndown["unit"] = usePoints ? "points" : "tasks";
  const weight = (tk: TaskRow) => (usePoints ? pointsOf(tk) : 1);
  const start = parseDate(sp.starts_on)!, end = parseDate(sp.ends_on)!;
  const span = Math.max(1, diffDays(start, end));
  const total = ts.reduce((s, tk) => s + weight(tk), 0);
  const ideal = Array.from({ length: span + 1 }, (_, i) => ({ date: toISODate(addDays(start, i)), value: Math.round((total * (1 - i / span)) * 100) / 100 }));
  if (sp.status === "planning") return { unit, ideal, actual: [] };
  const last = sp.status === "closed" ? parseDate(sp.closed_at!)! : T0;
  const untilIdx = Math.min(span, Math.max(0, diffDays(start, last)));
  const actual = Array.from({ length: untilIdx + 1 }, (_, i) => {
    const d = toISODate(addDays(start, i));
    const remaining = ts.filter((tk) => !(doneRow(tk) && tk.actual_end && tk.actual_end <= d)).reduce((s, tk) => s + weight(tk), 0);
    return { date: d, value: remaining };
  });
  return { unit, ideal, actual };
}
const closedSprints = () => Object.values(sprints).filter((sp) => sp.status === "closed").sort((a, b) => (b.closed_at ?? "").localeCompare(a.closed_at ?? "")).slice(0, 5);
function velocity(team?: string): VelocityData {
  const list = closedSprints().filter((sp) => !team || sp.team_id === team);
  const items = list.map((sp) => {
    const v = viewSprint(sp);
    const ts = sprintTasks(sp);
    return { id: sp.id, name: sp.name, points_done: v.points_done, tasks_done: ts.length ? ts.filter(doneRow).length : (sp.snapshot?.tasks_done ?? 0) };
  });
  return { sprints: items.reverse(), average_points: items.length ? Math.round((items.reduce((s, x) => s + x.points_done, 0) / items.length) * 10) / 10 : 0 };
}
const getSprint = (id: string) => { const sp = sprints[id]; if (!sp) throw new ApiError(404, t("mock.sprint.notFound")); return sp; };
const canManageWorkflows = () => ORG.owner_id === ME || myRoles().some((r) => roleOf(r)?.permissions.includes("manage_workflows"));
function setTaskSprint(tk: TaskRow, sprintId: ID | null, actor: ID | null) {
  if (tk.sprint_id === sprintId) return;
  if (sprintId) {
    const sp = getSprint(sprintId);
    if (sp.status === "closed") throw new ApiError(409, t("mock.sprint.closed"));
  }
  const prev = tk.sprint_id;
  tk.sprint_id = sprintId;
  tk.updated_at = nowISO();
  if (prev && sprints[prev]) emit("TaskRemovedFromSprint", { task: tk.id, actor, summary: t("mock.ev.removedFromSprint", { name: L(sprints[prev].name) }), data: { sprint_id: prev } });
  if (sprintId) emit("TaskAddedToSprint", { task: tk.id, actor, summary: t("mock.ev.addedToSprint", { name: L(sprints[sprintId].name) }), data: { sprint_id: sprintId } });
}
const STATUS_RANK: Record<SprintStatus, number> = { active: 0, planning: 1, closed: 2 };
on("GET", "/sprints/velocity", (_m, _b, q) => { requireLogin(); return velocity(str(q, "team")); });
on("GET", "/sprints", (_m, _b, q) => {
  requireLogin();
  const team = str(q, "team"), status = str(q, "status");
  const pick = scopePick(q);
  return Object.values(sprints)
    .filter((sp) => !pick || pick(sp.team_id))
    .filter((sp) => !team || sp.team_id === team)
    .filter((sp) => !status || sp.status === status)
    .sort((a, b) => STATUS_RANK[a.status] - STATUS_RANK[b.status] || (a.status === "closed" ? b.starts_on.localeCompare(a.starts_on) : a.starts_on.localeCompare(b.starts_on)))
    .map(viewSprint);
});
function validateSprintDates(starts_on: string, ends_on: string) {
  if (!parseDate(starts_on) || !parseDate(ends_on)) throw new ApiError(400, t("mock.sprint.badDates"));
  if (ends_on <= starts_on) throw new ApiError(400, t("mock.sprint.badDates"));
}
on("POST", "/sprints", (_m, body) => {
  requireLogin();
  const b = (body ?? {}) as SprintInput;
  if (!b.name?.trim()) throw new ApiError(400, t("mock.sprint.needName"));
  validateSprintDates(b.starts_on, b.ends_on);
  if (b.team_id && !TEAMS.some((x) => x.id === b.team_id)) throw new ApiError(400, t("mock.org.teamNotFound"));
  const sp: SprintRow = { id: nextId("S"), team_id: b.team_id || null, name: b.name.trim(), goal: b.goal?.trim() ?? "", starts_on: b.starts_on, ends_on: b.ends_on, status: "planning", created_by: ME, started_at: null, closed_at: null, created_at: nowISO() };
  sprints[sp.id] = sp;
  emit("SprintCreated", { actor: ME, summary: t("mock.ev.sprintCreated", { name: sp.name }), data: { sprint_id: sp.id } });
  return viewSprint(sp);
});
on("GET", "/sprints/:id", (m): SprintDetail => {
  requireLogin();
  const sp = getSprint(m.groups!.id);
  const v = velocity(sp.team_id ?? undefined);
  // 与真实后端一致：迭代详情里的任务是完整任务对象
  return { ...viewSprint(sp), tasks: sprintTasks(sp).sort(byBoardOrder).map(viewTask), burndown: burndown(sp), velocity: { sprints: v.sprints.length, average: v.average_points } };
});
on("PATCH", "/sprints/:id", (m, body) => {
  requireLogin();
  const sp = getSprint(m.groups!.id);
  if (sp.status === "closed") throw new ApiError(409, t("mock.sprint.closed"));
  const b = (body ?? {}) as Partial<SprintInput>;
  if (b.name !== undefined) { if (!b.name.trim()) throw new ApiError(400, t("mock.sprint.needName")); sp.name = b.name.trim(); }
  if (b.goal !== undefined) sp.goal = b.goal.trim();
  if (b.starts_on !== undefined || b.ends_on !== undefined) { validateSprintDates(b.starts_on ?? sp.starts_on, b.ends_on ?? sp.ends_on); sp.starts_on = b.starts_on ?? sp.starts_on; sp.ends_on = b.ends_on ?? sp.ends_on; }
  return viewSprint(sp);
});
on("POST", "/sprints/:id/start", (m) => {
  requireLogin();
  if (!canManageWorkflows()) throw new ApiError(403, t("mock.sprint.needPermission"));
  const sp = getSprint(m.groups!.id);
  if (sp.status !== "planning") throw new ApiError(409, t("mock.sprint.notPlanning"));
  const other = Object.values(sprints).find((x) => x.status === "active" && x.team_id === sp.team_id);
  if (other) throw new ApiError(409, t("mock.sprint.activeExists", { team: L(sp.team_id ? TEAMS.find((x) => x.id === sp.team_id)?.name ?? "" : ORG.name), name: L(other.name) }));
  sp.status = "active";
  sp.started_at = nowISO();
  emit("SprintStarted", { actor: ME, summary: t("mock.ev.sprintStarted", { name: L(sp.name) }), data: { sprint_id: sp.id } });
  return viewSprint(sp);
});
on("POST", "/sprints/:id/close", (m, body) => {
  requireLogin();
  if (!canManageWorkflows()) throw new ApiError(403, t("mock.sprint.needPermission"));
  const sp = getSprint(m.groups!.id);
  if (sp.status !== "active") throw new ApiError(409, t("mock.sprint.notActive"));
  const b = (body ?? {}) as SprintCloseInput;
  let next: SprintRow | null = null;
  if (b.unfinished === "next") {
    if (!b.next_sprint_id) throw new ApiError(400, t("mock.sprint.needNext"));
    next = getSprint(b.next_sprint_id);
    if (next.status === "closed") throw new ApiError(409, t("mock.sprint.closed"));
  }
  let moved = 0, returned = 0;
  for (const tk of sprintTasks(sp)) {
    if (isTerminalRow(tk)) continue;
    setTaskSprint(tk, next ? next.id : null, ME);
    if (next) moved += 1; else returned += 1;
  }
  sp.status = "closed";
  sp.closed_at = nowISO();
  emit("SprintClosed", { actor: ME, summary: t("mock.ev.sprintClosed", { name: L(sp.name), moved, returned }), data: { sprint_id: sp.id, moved, returned } });
  return { sprint: viewSprint(sp), moved, returned };
});
on("POST", "/sprints/:id/tasks", (m, body) => {
  requireLogin();
  const sp = getSprint(m.groups!.id);
  if (sp.status === "closed") throw new ApiError(409, t("mock.sprint.closed"));
  const b = (body ?? {}) as { task_ids?: ID[] };
  let added = 0;
  for (const id of b.task_ids ?? []) { const tk = getTask(id); if (tk.sprint_id !== sp.id) { setTaskSprint(tk, sp.id, ME); added += 1; } }
  return { added };
});
on("DELETE", "/sprints/:id/tasks/:task", (m) => {
  requireLogin();
  const sp = getSprint(m.groups!.id);
  if (sp.status === "closed") throw new ApiError(409, t("mock.sprint.closed"));
  const tk = getTask(m.groups!.task);
  if (tk.sprint_id === sp.id) setTaskSprint(tk, null, ME);
  return undefined;
});

// ---------- 工作台（ADR 0015：/workspace、/org/workspace） ----------
const BLOCK_DEFS: BlockDef[] = [
  { key: "overview_summary", title: "组织概览摘要", description: "当前范围这一个月的目标进度、任务、逾期、吞吐与成本合计。" },
  { key: "exceptions", title: "要我关注的异常", description: "逾期与停滞的任务、逾期或超预算的目标、等我确认的操作。" },
  { key: "my_tasks", title: "我的任务", description: "我和我的 Agent 名下未结束的任务。" },
  { key: "my_review", title: "等我验收", description: "我是验收人、正在等我处理的任务。" },
  { key: "team_load", title: "人员与 Agent 负荷", description: "每个人和 Agent 手上有多少活、有没有逾期。" },
  { key: "cost_budget", title: "成本与预算", description: "这一个月的成本、预算执行率，以及成本最高的目标。" },
  { key: "events", title: "最近动态", description: "当前范围里最近发生了什么。" },
  { key: "sprint", title: "当前迭代", description: "进行中的迭代：进度、剩余天数与燃尽图。" },
  { key: "backlog", title: "待领取任务", description: "现在我能领的任务。" },
  { key: "trend", title: "趋势与环比", description: "成本与吞吐随时间的变化，与上一段时间对比。" },
];
const PRESETS: Preset[] = [
  { key: "global", title: "全局视角", description: "看全公司：概览、异常、成本、负荷与趋势。", blocks: ["overview_summary", "exceptions", "cost_budget", "team_load", "trend", "events"] },
  { key: "unit", title: "部门视角", description: "看本部：概览、异常、负荷、成本，加上等我验收的。", blocks: ["overview_summary", "exceptions", "team_load", "cost_budget", "my_review", "events"] },
  { key: "team", title: "小组视角", description: "带小组：异常、负荷、当前迭代、验收与待领取。", blocks: ["exceptions", "team_load", "sprint", "my_review", "backlog", "events"] },
  { key: "doer", title: "执行视角", description: "干活：我的任务、等我验收、待领取、当前迭代与动态。", blocks: ["my_tasks", "my_review", "backlog", "sprint", "events"] },
  { key: "ops", title: "运营视角", description: "看经营：成本、趋势、负荷与异常。", blocks: ["cost_budget", "trend", "team_load", "exceptions", "overview_summary", "events"] },
];
const presetOf = (key: string) => PRESETS.find((p) => p.key === key);
/** 首次种子：管理员 → 全局视角；运营、流程管理 → 运营视角；产品、设计、开发、测试、发布 → 执行视角。 */
const ROLE_LAYOUTS: Record<string, { blocks: BlockKey[]; preset: string | null }> = Object.fromEntries(
  ROLES.map((r) => {
    const key = r.name === "admin" ? "global" : r.name === "ops" || r.name === "workflow_admin" ? "ops" : "doer";
    return [r.name, { blocks: [...presetOf(key)!.blocks], preset: key }];
  }),
);
/** 个人微调：成员 id → 区块顺序；没有就是没调过 */
const PERSONAL_LAYOUTS: Record<ID, BlockKey[]> = {};
const blockView = (k: BlockKey) => ({ key: k, title: BLOCK_DEFS.find((b) => b.key === k)?.title ?? k });
const parseBlocks = (v: unknown): BlockKey[] => {
  if (!Array.isArray(v)) throw new ApiError(400, t("mock.ws.needBlocks"));
  const out: BlockKey[] = [];
  for (const k of v) {
    if (typeof k !== "string" || !isBlockKey(k)) throw new ApiError(400, t("mock.ws.badBlock", { key: String(k) }));
    if (!out.includes(k)) out.push(k);
  }
  if (!out.length) throw new ApiError(400, t("mock.ws.needBlocks"));
  return out;
};
/** 解析顺序：个人微调 → 各角色布局并集（按首次出现顺序去重）→ 默认（组织负责人 global，其他 doer）。 */
function resolveWorkspace(): Workspace {
  const me = MEMBERS[ME];
  const personal = PERSONAL_LAYOUTS[ME];
  if (personal) return { blocks: personal.map(blockView), source: "personal", roles_used: [], can_customize: true };
  const used = me.roles.filter((r) => ROLE_LAYOUTS[r]);
  if (used.length) {
    const merged: BlockKey[] = [];
    for (const r of used) for (const k of ROLE_LAYOUTS[r].blocks) if (!merged.includes(k)) merged.push(k);
    return { blocks: merged.map(blockView), source: "roles", roles_used: used, can_customize: true };
  }
  const fallback = presetOf(ORG.owner_id === ME ? "global" : "doer")!;
  return { blocks: fallback.blocks.map(blockView), source: "default", roles_used: [], can_customize: true };
}
const viewRoleWorkspace = (r: OrgRole): RoleWorkspace => {
  const layout = ROLE_LAYOUTS[r.name];
  const fallback = presetOf("doer")!;
  return {
    role: r.name,
    role_title: r.title,
    blocks: layout ? [...layout.blocks] : [...fallback.blocks],
    preset: layout ? layout.preset : fallback.key,
    member_count: Object.values(MEMBERS).filter((m) => m.active && m.roles.includes(r.name)).length,
  };
};
on("GET", "/workspace", () => { requireLogin(); return resolveWorkspace(); });
on("PUT", "/workspace/me", (_m, body) => {
  requireLogin();
  PERSONAL_LAYOUTS[ME] = parseBlocks((body as { blocks?: unknown } | null)?.blocks);
  return resolveWorkspace();
});
on("DELETE", "/workspace/me", () => { requireLogin(); delete PERSONAL_LAYOUTS[ME]; return undefined; });
on("GET", "/workspace/blocks", (): WorkspaceCatalog => { requireLogin(); return { blocks: BLOCK_DEFS.filter((b) => BLOCK_KEYS.includes(b.key)), presets: PRESETS }; });
on("GET", "/org/workspace", () => { requireOrgAdmin(); return ROLES.map(viewRoleWorkspace); });
on("PUT", "/org/workspace/:role", (m, body) => {
  requireOrgAdmin();
  const role = roleOf(m.groups!.role);
  if (!role) throw new ApiError(404, t("mock.org.roleNotFound"));
  const b = (body ?? {}) as { blocks?: unknown; preset?: unknown };
  if (typeof b.preset === "string") {
    const p = presetOf(b.preset);
    if (!p) throw new ApiError(400, t("mock.ws.badPreset"));
    ROLE_LAYOUTS[role.name] = { blocks: [...p.blocks], preset: p.key };
  } else {
    const blocks = parseBlocks(b.blocks);
    const same = PRESETS.find((p) => p.blocks.length === blocks.length && p.blocks.every((k, i) => k === blocks[i]));
    ROLE_LAYOUTS[role.name] = { blocks, preset: same?.key ?? null };
  }
  emit("WorkspaceLayoutUpdated", { actor: ME, summary: t("mock.ev.workspaceLayout", { role: L(role.title) }), data: { role: role.name, preset: ROLE_LAYOUTS[role.name].preset, blocks: ROLE_LAYOUTS[role.name].blocks } });
  return viewRoleWorkspace(role);
});
on("DELETE", "/org/workspace/:role", (m) => {
  requireOrgAdmin();
  const role = roleOf(m.groups!.role);
  if (!role) throw new ApiError(404, t("mock.org.roleNotFound"));
  delete ROLE_LAYOUTS[role.name];
  emit("WorkspaceLayoutUpdated", { actor: ME, summary: t("mock.ev.workspaceLayoutReset", { role: L(role.title) }), data: { role: role.name } });
  return undefined;
});

// ---------- 组织设置 ----------
const orgInfo = (): OrgInfo => ({ id: ORG.id, slug: ORG.slug, name: ORG.name, currency: ORG.currency, default_locale: ORG.default_locale, owner: ref(ORG.owner_id) });
on("GET", "/org", () => { requireOrgAdmin(); return orgInfo(); });
on("PATCH", "/org", (_m, body) => {
  requireOrgAdmin();
  const b = (body ?? {}) as { name?: string; default_locale?: string; currency?: string };
  if (b.name !== undefined) { if (!b.name.trim()) throw new ApiError(400, t("mock.org.needName")); ORG.name = b.name.trim(); }
  if (b.default_locale !== undefined) ORG.default_locale = normalizeLocale(b.default_locale) ?? ORG.default_locale;
  if (b.currency !== undefined) ORG.currency = b.currency.toUpperCase();
  ADMIN_ORGS[0].name = ORG.name; ADMIN_ORGS[0].currency = ORG.currency; ADMIN_ORGS[0].default_locale = ORG.default_locale;
  return orgInfo();
});
const getMember = (id: string) => { const m = MEMBERS[id]; if (!m) throw new ApiError(404, t("mock.org.memberNotFound")); return m; };
on("GET", "/org/members", () => { requireOrgAdmin(); return Object.values(MEMBERS).map(viewOrgMember); });
on("PATCH", "/org/members/:id", (m, body) => {
  requireOrgAdmin();
  const mem = getMember(m.groups!.id);
  const b = (body ?? {}) as { name?: string; roles?: string[]; active?: boolean; team_id?: ID | null };
  if (b.name !== undefined) mem.name = b.name;
  if (b.roles !== undefined) mem.roles = b.roles.filter((r) => roleOf(r));
  if (b.active !== undefined) mem.active = b.active;
  if (b.team_id !== undefined) {
    mem.team_id = b.team_id;
    for (const tm of TEAMS) tm.member_ids = tm.member_ids.filter((x) => x !== mem.id);
    const tm = TEAMS.find((x) => x.id === b.team_id);
    if (tm) tm.member_ids.push(mem.id);
  }
  return viewOrgMember(mem);
});
on("POST", "/org/members/:id/make-owner", (m) => {
  requireOrgAdmin();
  const mem = getMember(m.groups!.id);
  ORG.owner_id = mem.id;
  ADMIN_ORGS[0].owner = { id: mem.id, name: mem.name, email: mem.email };
  return viewOrgMember(mem);
});
on("GET", "/org/invitations", () => { requireOrgAdmin(); return INVITATIONS.map(viewInvitation); });
on("POST", "/org/invitations", (_m, body) => {
  requireOrgAdmin();
  const b = (body ?? {}) as { email?: string; name?: string; roles?: string[] };
  if (!b.email?.trim()) throw new ApiError(400, t("mock.org.needEmail"));
  const row: InvitationRow = { id: nextId("I"), token: `inv_${Math.random().toString(36).slice(2, 10)}`, email: b.email.trim(), name: b.name?.trim() ?? "", roles: b.roles ?? [], url: "", expires_at: at(7), accepted_at: null, created_at: nowISO() };
  INVITATIONS.push(row);
  return viewInvitation(row);
});
on("DELETE", "/org/invitations/:id", (m) => {
  requireOrgAdmin();
  const i = INVITATIONS.findIndex((x) => x.id === m.groups!.id);
  if (i < 0) throw new ApiError(404, t("mock.invite.notFound"));
  INVITATIONS.splice(i, 1);
  return undefined;
});
on("GET", "/org/roles", () => { requireLogin(); return ROLES.map(viewRole); });
on("PUT", "/org/roles/:name", (m, body) => {
  requireOrgAdmin();
  const name = m.groups!.name;
  const b = (body ?? {}) as { title?: LocalizedTitle; permissions?: string[] };
  let role = roleOf(name);
  if (role) {
    if (b.title !== undefined) role.title = storeTitle(b.title, role.title);
    if (!role.builtin && b.permissions !== undefined) role.permissions = b.permissions;
  } else {
    const title = b.title !== undefined ? storeTitle(b.title) : name;
    if (!title) throw new ApiError(400, t("mock.org.needName"));
    role = { name, title, builtin: false, permissions: b.permissions ?? [] };
    ROLES.push(role);
  }
  return viewRole(role);
});
on("DELETE", "/org/roles/:name", (m) => {
  requireOrgAdmin();
  const role = roleOf(m.groups!.name);
  if (!role) throw new ApiError(404, t("mock.org.roleNotFound"));
  if (role.builtin) throw new ApiError(409, t("mock.org.roleBuiltin"));
  if (Object.values(MEMBERS).some((x) => x.roles.includes(role.name))) throw new ApiError(409, t("mock.org.roleInUse"));
  ROLES.splice(ROLES.indexOf(role), 1);
  return undefined;
});
const getTeam = (id: string) => { const tm = TEAMS.find((x) => x.id === id); if (!tm) throw new ApiError(404, t("mock.org.teamNotFound")); return tm; };
const applyTeamMembers = (tm: OrgTeam) => { for (const id of tm.member_ids) if (MEMBERS[id]) MEMBERS[id].team_id = tm.id; };
on("GET", "/org/teams", () => { requireOrgAdmin(); return TEAMS; });
on("POST", "/org/teams", (_m, body) => {
  requireOrgAdmin();
  const b = (body ?? {}) as { name?: string; parent_id?: ID | null; lead_id?: ID | null; member_ids?: ID[]; is_boundary?: boolean };
  if (!b.name?.trim()) throw new ApiError(400, t("mock.org.needName"));
  const tm: OrgTeam = { id: nextId("team-"), name: b.name.trim(), parent_id: b.parent_id ?? null, lead_id: b.lead_id ?? null, member_ids: b.member_ids ?? [], is_boundary: !!b.is_boundary };
  TEAMS.push(tm);
  applyTeamMembers(tm);
  return tm;
});
on("PATCH", "/org/teams/:id", (m, body) => {
  requireOrgAdmin();
  const tm = getTeam(m.groups!.id);
  const b = (body ?? {}) as { name?: string; parent_id?: ID | null; lead_id?: ID | null; member_ids?: ID[]; is_boundary?: boolean };
  if (b.name !== undefined) tm.name = b.name;
  if (b.parent_id !== undefined) tm.parent_id = b.parent_id === tm.id ? tm.parent_id : b.parent_id;
  if (b.lead_id !== undefined) tm.lead_id = b.lead_id;
  if (b.is_boundary !== undefined) tm.is_boundary = !!b.is_boundary;
  if (b.member_ids !== undefined) {
    for (const id of tm.member_ids) if (MEMBERS[id] && !b.member_ids.includes(id) && MEMBERS[id].team_id === tm.id) MEMBERS[id].team_id = null;
    tm.member_ids = b.member_ids;
    applyTeamMembers(tm);
  }
  return tm;
});
on("DELETE", "/org/teams/:id", (m) => {
  requireOrgAdmin();
  const tm = getTeam(m.groups!.id);
  for (const mem of Object.values(MEMBERS)) if (mem.team_id === tm.id) mem.team_id = null;
  for (const child of TEAMS) if (child.parent_id === tm.id) child.parent_id = tm.parent_id;
  TEAMS.splice(TEAMS.indexOf(tm), 1);
  return undefined;
});
on("GET", "/org/capabilities", () => { requireOrgAdmin(); return CAPABILITIES.map(viewCapability); });
on("PUT", "/org/capabilities/:name", (m, body) => {
  requireOrgAdmin();
  const name = m.groups!.name;
  const b = (body ?? {}) as { title?: LocalizedTitle };
  let cap = CAPABILITIES.find((c) => c.name === name);
  if (cap) { if (b.title !== undefined) cap.title = storeTitle(b.title, cap.title); }
  else {
    const title = b.title !== undefined ? storeTitle(b.title) : name;
    if (!title) throw new ApiError(400, t("mock.org.needName"));
    cap = { name, title };
    CAPABILITIES.push(cap);
  }
  return viewCapability(cap);
});
on("DELETE", "/org/capabilities/:name", (m) => {
  requireOrgAdmin();
  const i = CAPABILITIES.findIndex((c) => c.name === m.groups!.name);
  if (i >= 0) CAPABILITIES.splice(i, 1);
  return undefined;
});
on("GET", "/org/pricing", (): OrgPricing => {
  requireOrgAdmin();
  const ids = new Set([...Object.keys(GLOBAL_PRICES), ...Object.keys(ORG_PRICES)]);
  return {
    currency: ORG.currency,
    models: [...ids].sort().map((id) => (ORG_PRICES[id] ? { ...ORG_PRICES[id], source: "override" as const } : { ...GLOBAL_PRICES[id], source: "global" as const })),
    exchange_rates: RATES,
  };
});
on("PUT", "/org/pricing/models/:model", (m, body) => {
  requireOrgAdmin();
  const b = body as PriceInput;
  const row: PriceModel = { model_id: m.groups!.model, ...b, currency: (b.currency || ORG.currency).toUpperCase() };
  ORG_PRICES[row.model_id] = row;
  return { ...row, source: "override" as const };
});
on("DELETE", "/org/pricing/models/:model", (m) => { requireOrgAdmin(); delete ORG_PRICES[m.groups!.model]; return undefined; });
on("PUT", "/org/pricing/rates", (_m, body) => {
  requireOrgAdmin();
  const b = body as ExchangeRate;
  if (!(b.rate > 0)) throw new ApiError(400, t("mock.org.badRate"));
  const row: ExchangeRate = { from: b.from.toUpperCase(), to: b.to.toUpperCase(), rate: b.rate };
  const i = RATES.findIndex((r) => r.from === row.from && r.to === row.to);
  if (i >= 0) RATES[i] = row; else RATES.push(row);
  return row;
});

// ---------- 邀请（公开） ----------
const getInvitation = (token: string) => { const i = INVITATIONS.find((x) => x.token === token); if (!i) throw new ApiError(404, t("mock.invite.notFound")); return i; };
on("GET", "/invitations/:token", (m): InvitationInfo => {
  const i = getInvitation(m.groups!.token);
  return { organization_name: ORG.name, email: i.email, name: i.name, expired: parseDate(i.expires_at)! < new Date(), accepted: !!i.accepted_at };
});
on("POST", "/invitations/:token/accept", (m, body) => {
  const i = getInvitation(m.groups!.token);
  const b = (body ?? {}) as { name?: string; password?: string };
  if (!b.password) throw new ApiError(400, t("mock.invite.needPassword"));
  if (parseDate(i.expires_at)! < new Date() || i.accepted_at) throw new ApiError(409, t("mock.invite.notFound"));
  let mem = Object.values(MEMBERS).find((x) => x.email === i.email);
  if (!mem) {
    mem = { id: nextId("m"), name: b.name?.trim() || i.name || i.email, email: i.email, roles: i.roles, team_id: null, locale: getLocale(), active: true, created_at: nowISO() };
    MEMBERS[mem.id] = mem;
    ADMIN_ORGS[0].member_count += 1;
  }
  i.accepted_at = nowISO();
  ME = mem.id;
  loggedIn = true;
  return session();
});

// ---------- 平台后台 ----------
on("POST", "/admin/login", (_m, body) => {
  const b = (body ?? {}) as { email?: string; password?: string };
  if (!b.email) throw new ApiError(400, t("mock.login.needEmail"));
  if (b.password === "wrong") throw new ApiError(401, t("mock.admin.bad"));
  adminLoggedIn = true;
  return { admin: ADMINS[0] };
});
on("POST", "/admin/logout", () => { adminLoggedIn = false; return undefined; });
on("GET", "/admin/me", () => { requireAdmin(); return { admin: ADMINS[0] }; });
on("GET", "/admin/organizations", () => { requireAdmin(); return ADMIN_ORGS.map(viewAdminOrg); });
on("POST", "/admin/organizations", (_m, body): AdminOrganizationCreated => {
  requireAdmin();
  const b = body as AdminOrganizationInput;
  if (!b.slug?.trim() || !b.name?.trim()) throw new ApiError(400, t("mock.org.needName"));
  if (!b.owner_email?.trim()) throw new ApiError(400, t("mock.org.needEmail"));
  if (ADMIN_ORGS.some((o) => o.slug === b.slug.trim())) throw new ApiError(409, t("mock.admin.slugTaken"));
  const row: AdminOrgRow = {
    id: nextId("org"), slug: b.slug.trim(), name: b.name.trim(), currency: (b.currency || "CNY").toUpperCase(), default_locale: normalizeLocale(b.default_locale) ?? "zh-CN",
    owner: { id: nextId("m"), name: b.owner_name?.trim() || b.owner_email, email: b.owner_email.trim() }, member_count: 1, agent_count: 0, task_count: 0, cost_30d: 0, deactivated_at: null, created_at: nowISO(),
  };
  ADMIN_ORGS.push(row);
  return { ...viewAdminOrg(row), owner_invite_url: inviteUrl(`owner_${row.slug}`) };
});
const getAdminOrg = (id: string) => { const o = ADMIN_ORGS.find((x) => x.id === id); if (!o) throw new ApiError(404, t("mock.admin.orgNotFound")); return o; };
on("GET", "/admin/organizations/:id", (m) => { requireAdmin(); return viewAdminOrg(getAdminOrg(m.groups!.id)); });
on("PATCH", "/admin/organizations/:id", (m, body) => {
  requireAdmin();
  const o = getAdminOrg(m.groups!.id);
  const b = (body ?? {}) as { name?: string; deactivated?: boolean };
  if (b.name !== undefined) { o.name = b.name; if (o.id === ORG.id) ORG.name = b.name; }
  if (b.deactivated !== undefined) o.deactivated_at = b.deactivated ? nowISO() : null;
  return viewAdminOrg(o);
});
on("GET", "/admin/pricing", () => { requireAdmin(); return Object.values(GLOBAL_PRICES); });
on("PUT", "/admin/pricing/:model", (m, body) => {
  requireAdmin();
  const b = body as PriceInput;
  const row: PriceModel = { model_id: m.groups!.model, ...b, currency: (b.currency || "CNY").toUpperCase() };
  GLOBAL_PRICES[row.model_id] = row;
  return row;
});
on("DELETE", "/admin/pricing/:model", (m) => { requireAdmin(); delete GLOBAL_PRICES[m.groups!.model]; return undefined; });
on("GET", "/admin/stats", (): AdminStats => {
  requireAdmin();
  const live = ADMIN_ORGS.filter((o) => !o.deactivated_at);
  return {
    organizations: ADMIN_ORGS.length,
    members: live.reduce((s, o) => s + o.member_count, 0),
    agents: live.reduce((s, o) => s + o.agent_count, 0),
    runs_30d: runs.length + 37,
    cost_30d: ADMIN_ORGS.map(viewAdminOrg).filter((o) => o.cost_30d > 0).map((o) => ({ org_id: o.id, name: o.name, cost: o.cost_30d, currency: o.currency })),
  };
});
on("GET", "/admin/admins", () => { requireAdmin(); return ADMINS; });
on("POST", "/admin/admins", (_m, body) => {
  requireAdmin();
  const b = (body ?? {}) as { email?: string; name?: string; password?: string };
  if (!b.email?.trim()) throw new ApiError(400, t("mock.org.needEmail"));
  const a: AdminUser = { id: nextId("adm"), email: b.email.trim(), name: b.name?.trim() || b.email.trim() };
  ADMINS.push(a);
  return a;
});

const clone = <T,>(v: T): T => (v === undefined ? v : (JSON.parse(JSON.stringify(v)) as T));

export async function mockRequest(method: string, path: string, body?: unknown, query?: Query): Promise<unknown> {
  await new Promise((r) => setTimeout(r, 120));
  for (const [m, re, h] of routes) {
    if (m !== method) continue;
    const match = path.match(re);
    if (match) return localize(clone(h(match, body, query)));
  }
  throw new ApiError(404, t("api.notFound", { method, path }));
}
