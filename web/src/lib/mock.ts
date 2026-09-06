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
  DirectoryConfig,
  DirectoryInput,
  DirectoryProviderInfo,
  DirectoryPreview,
  DirectoryBinding,
  DirectoryCandidate,
  DirectoryConfirmation,
  DirectoryDecided,
  DirectoryDecision,
  DirectoryDecisionInput,
  DirectoryDecisionRecord,
  DirectoryDuplicate,
  DirectoryDuplicateSide,
  DirectoryKind,
  DirectoryMappings,
  DirectoryMatchReason,
  DirectorySkipped,
  MemberImportDecision,
  DirectoryRun,
  DirectoryTeamPlan,
  DirectoryTest, DirectoryChecklist, DirectoryCheck,
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
  MemberImportPreview,
  MemberImportResult,
  MemberImportRow,
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
  LayoutBlock,
  RawBlock,
  Preset,
  RoleWorkspace,
  Workspace,
  WorkspaceBlock,
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
import { fieldChangeSentence } from "./fieldChange";
import { ApiError, BLOCK_KEYS, GRID_COLS, GRID_MAX_H, INBOX_KINDS, blocksOverlap, compactLayout, isBlockHeight, isBlockKey, isBlockWidth, normalizeLayout, sameLayout } from "./api";
import { addDays, diffDays, parseDate, startOfWeek, toISODate, today } from "./format";
import { getLocale, normalizeLocale, t, type Key, type Locale } from "./i18n";

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
  "待我处理": "For me", "组织概况": "Organization readouts",
  "等我确认、验收、答复的事和我负责但逾期的任务，只看本人、不受范围影响。": "Things waiting for me to confirm, accept or answer, plus my own overdue tasks; personal only, unaffected by scope.",
  "进行中、待验收、逾期与今日成本四个实时读数，带 24 小时趋势线。": "Four live readouts — in progress, awaiting acceptance, overdue and cost today — each with a 24-hour trend line.",
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
// 产品事业部这一支来自IM 集成（source 存提供方代码名，ADR 0017），服务事业部是手工建的；「旧项目组」在IM 集成里已经没有了，下次同步会被停用
const TEAMS: OrgTeam[] = [
  { id: "team-rd", name: "产品事业部", lead_id: "wang", parent_id: null, member_ids: ["wang"], is_boundary: true, source: "feishu", external_name: "产品事业部", active: true },
  { id: "team-fe", name: "前端组", lead_id: "li", parent_id: "team-rd", member_ids: ["li", "zhao", "wu", "wu-manual"], source: "feishu", external_name: "前端组", active: true },
  { id: "team-qa", name: "测试组", lead_id: "zhang", parent_id: "team-rd", member_ids: ["zhang", "zheng"], source: "feishu", external_name: "测试组", active: true },
  { id: "team-old", name: "旧项目组", lead_id: null, parent_id: "team-rd", member_ids: [], source: "feishu", external_name: "旧项目组", active: true },
  { id: "team-svc", name: "服务事业部", lead_id: "zhou", parent_id: null, member_ids: ["zhou"], source: "manual", external_name: null, active: true },
  { id: "team-ops", name: "运营组", lead_id: "sun", parent_id: "team-svc", member_ids: ["sun"], source: "manual", external_name: null, active: true },
  { id: "team-cs", name: "客户支持组", lead_id: "chen", parent_id: "team-svc", member_ids: ["chen"], source: "manual", external_name: null, active: true },
  // 手工在产品事业部下建的组；IM 集成里后来也建了同名部门 → 预览时要人确认是不是同一个（ADR 0017 补记四）
  { id: "team-data", name: "数据平台组", lead_id: "feng", parent_id: "team-rd", member_ids: ["feng", "han", "han-old"], source: "manual", external_name: null, active: true },
];
interface MemberRow extends Member { active: boolean; password?: string; invite_token?: string }
const MEMBERS: Record<ID, MemberRow> = {
  wang: { id: "wang", name: "小王", email: "wang@example.com", roles: ["pm", "designer", "admin"], team_id: "team-rd", locale: "zh-CN", active: true, created_at: at(-120), source: "manual", status: "active" },
  li: { id: "li", name: "小李", email: "li@example.com", roles: ["developer"], team_id: "team-fe", locale: "zh-CN", active: true, created_at: at(-110), source: "feishu", status: "active" },
  zhang: { id: "zhang", name: "小张", email: "zhang@example.com", roles: ["tester"], team_id: "team-qa", locale: "en-US", active: true, created_at: at(-100), source: "feishu", status: "active" },
  zhao: { id: "zhao", name: "小赵", email: "zhao@example.com", roles: ["releaser", "developer"], team_id: "team-fe", locale: "zh-CN", active: true, created_at: at(-90), source: "feishu", status: "active" },
  zhou: { id: "zhou", name: "小周", email: "zhou@example.com", roles: ["pm"], team_id: "team-svc", locale: "zh-CN", active: true, created_at: at(-80), source: "manual", status: "active" },
  sun: { id: "sun", name: "小孙", email: "sun@example.com", roles: ["pm", "ops"], team_id: "team-ops", locale: "zh-CN", active: true, created_at: at(-70), source: "manual", status: "active" },
  chen: { id: "chen", name: "小陈", email: "chen@example.com", roles: ["tester"], team_id: "team-cs", locale: "zh-CN", active: true, created_at: at(-65), source: "manual", status: "active" },
  // 上次同步进来、还没登录过的两位（待激活）
  wu: { id: "wu", name: "吴小雨", email: "wu@example.com", roles: ["developer"], team_id: "team-fe", locale: "zh-CN", active: true, created_at: at(-3, 3), source: "feishu", status: "pending_activation", invite_token: "dir_wu" },
  zheng: { id: "zheng", name: "郑一", email: "zheng@example.com", roles: ["developer"], team_id: "team-qa", locale: "zh-CN", active: true, created_at: at(-3, 3), source: "feishu", status: "pending_activation", invite_token: "dir_zheng" },
  // 手工建的数据平台组的人：冯小北的邮箱与 IM 里的同一个人相同（第二级认法）；韩小南在 IM 里没邮箱，靠"姓名相同且部门同名"认出两个候选（其中一个是停用的旧账号）
  feng: { id: "feng", name: "冯小北", email: "feng@example.com", roles: ["developer"], team_id: "team-data", locale: "zh-CN", active: true, created_at: at(-40), source: "manual", status: "active" },
  han: { id: "han", name: "韩小南", email: "han@example.com", roles: ["developer"], team_id: "team-data", locale: "zh-CN", active: true, created_at: at(-30), source: "manual", status: "active" },
  "han-old": { id: "han-old", name: "韩小南", email: "han.old@example.com", roles: ["developer"], team_id: "team-data", locale: "zh-CN", active: false, created_at: at(-200), source: "manual", status: "inactive" },
  // 手工建的吴小雨与同步进来的吴小雨同名同组：同步之后仍算"可能重复"，成员页会提示、对应关系面板里能合并
  "wu-manual": { id: "wu-manual", name: "吴小雨", email: "wu.xiaoyu@example.com", roles: ["developer"], team_id: "team-fe", locale: "zh-CN", active: true, created_at: at(-50), source: "manual", status: "active" },
  // 被邀请、还没接受的人（POST /org/invitations 会立刻建出这条待激活成员）
  qian: { id: "qian", name: "小钱", email: "qian@example.com", roles: ["developer"], team_id: null, locale: "zh-CN", active: true, created_at: at(-1, 11), source: "manual", status: "pending_activation", invite_token: "demo" },
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
    online: true, last_seen_at: nowISO(), max_concurrency: 2, created_at: at(-60), can_manage: true,
  },
  "zhang-agent": {
    id: "zhang-agent", name: "小张的测试 Agent", owner: ref("zhang"), shared: false, capabilities: ["testing", "coding"],
    grants: [g("execute"), g("claim_backlog"), g("review"), g("comment")],
    online: true, last_seen_at: at(0, 8, 40), max_concurrency: 1, created_at: at(-45), can_manage: true,
  },
  "wang-agent": {
    id: "wang-agent", name: "小王的写作助手", owner: ref("wang"), shared: false, capabilities: ["writing", "data_analysis"],
    grants: [g("execute"), g("comment"), g("create_task", "with_approval")],
    online: false, last_seen_at: at(-1, 18), max_concurrency: 1, created_at: at(-30), can_manage: true,
  },
  "zhou-agent": {
    id: "zhou-agent", name: "小周的客服助手", owner: ref("zhou"), shared: false, capabilities: ["writing", "data_analysis"],
    grants: [g("execute"), g("comment")],
    online: true, last_seen_at: nowISO(), max_concurrency: 2, created_at: at(-25), can_manage: true,
  },
  "shared-doc": {
    id: "shared-doc", name: "公共文档助手", owner: ref("wang"), shared: true, capabilities: ["writing"],
    grants: [g("execute"), g("comment"), g("claim_backlog", "with_approval")],
    online: true, last_seen_at: nowISO(), max_concurrency: 3, created_at: at(-20), can_manage: true,
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
  human_only?: boolean;
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
    required_role: tk.required_role, pending_participant: tk.pending_participant, required_capabilities: tk.required_capabilities, human_only: tk.human_only ?? false,
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

const memberStatus = (m: MemberRow): NonNullable<Member["status"]> => (!m.active ? "inactive" : m.status === "pending_activation" ? "pending_activation" : "active");
const memberStatusTitle = (st: NonNullable<Member["status"]>) => (st === "inactive" ? t("mock.member.status.inactive") : st === "pending_activation" ? t("mock.member.status.pending") : t("mock.member.status.active"));
const viewMember = (m: MemberRow): Member => ({ id: m.id, name: m.name, email: m.email, roles: m.roles, team_id: m.team_id, locale: m.locale, created_at: m.created_at, source: m.source ?? "manual", source_title: sourceTitle(m.source), status: memberStatus(m) });
const viewOrgMember = (m: MemberRow, dups?: DirectoryDuplicate[]): OrgMember => {
  const st = memberStatus(m);
  const invRow = st === "pending_activation" && m.invite_token ? INVITATIONS.find((i) => i.token === m.invite_token) : undefined;
  const inv = st === "pending_activation" && m.invite_token ? { id: invRow?.id, url: inviteUrl(m.invite_token), expires_at: invRow?.expires_at ?? at(7) } : null;
  // 所在的全部团队：直属团队排第一，其余是被「加入」的团队
  const team_ids = [...(m.team_id ? [m.team_id] : []), ...TEAMS.filter((tm) => tm.member_ids.includes(m.id) && tm.id !== m.team_id).map((tm) => tm.id)];
  // 「可能与 X 重复」：一次列表调用算一遍（ADR 0017 补记四）
  const hints = (dups ?? []).filter((d) => d.kind === "member" && (d.a.id === m.id || d.b.id === m.id)).map((d) => { const o = d.a.id === m.id ? d.b : d.a; return { id: o.id, name: o.name, reason: d.reason, reason_text: d.reason_text }; });
  return { ...viewMember(m), active: m.active, is_owner: ORG.owner_id === m.id, status_title: memberStatusTitle(st), invitation: inv, invitation_url: inv?.url ?? null, team_ids, ...(hints.length ? { possible_duplicate_of: hints } : {}) };
};
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
/** 邀请行带上它建出的待激活成员（member_id）与团队（team_id），成员页据此把「撤回邀请」挂在成员上 */
const viewInvitation = (i: InvitationRow): Invitation => {
  const mem = Object.values(MEMBERS).find((m) => m.invite_token === i.token);
  return { id: i.id, email: i.email, name: i.name, roles: i.roles, url: inviteUrl(i.token), expires_at: i.expires_at, accepted_at: i.accepted_at, created_at: i.created_at, team_id: mem?.team_id ?? null, member_id: mem?.id ?? null };
};
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
// ---------- 就地编辑（DESIGN.md §15）：与后端同一套规则 ----------
const isOrgOwner = () => ORG.owner_id === ME;
/** 目标：负责人链（本目标或任一上级目标的负责人）或组织负责人 */
function canEditGoal(gr: GoalRow): boolean {
  if (isOrgOwner()) return true;
  for (let cur: GoalRow | undefined = gr; cur; cur = cur.parent_id ? goals[cur.parent_id] : undefined) if (cur.owner_id === ME) return true;
  return false;
}
/** 任务：负责人 / 创建者 / 验收人 / 参与人（含我的 Agent）、所属目标的负责人链、组织负责人 */
function canEditTask(tk: TaskRow): boolean {
  if (isOrgOwner()) return true;
  const mine = (id: ID | null | undefined) => !!id && (id === ME || AGENTS[id]?.owner.id === ME);
  if (mine(tk.assignee_id) || mine(tk.creator_id) || mine(tk.reviewer_id) || Object.values(tk.participants).some(mine)) return true;
  if (tk.goal_id && goals[tk.goal_id]) return canEditGoal(goals[tk.goal_id]);
  return false;
}
interface FieldChange { field: string; from: unknown; to: unknown; from_title?: unknown; to_title?: unknown; extra?: Record<string, unknown> }
/** 每个字段一条动态，句子按后端同一套模板拼（src/lib/fieldChange.ts） */
function emitFieldChange(kind: "GoalFieldChanged" | "TaskFieldChanged", target: { task?: ID; goal?: ID | null; title: string }, c: FieldChange) {
  const data: Record<string, unknown> = { field: c.field, from: c.from ?? null, to: c.to ?? null, title: L(target.title), ...(target.goal ? { goal_id: target.goal } : {}), ...c.extra };
  if (c.from_title !== undefined) data.from_title = c.from_title;
  if (c.to_title !== undefined) data.to_title = c.to_title;
  emit(kind, { task: target.task, goal: target.goal, actor: ME, summary: fieldChangeSentence({ kind, data, actor: ref(ME), task_title: L(target.title) }), data });
}
const nameOf = (id: ID | null | undefined) => (id ? L(ref(id).name) : null);
const teamName = (id: ID | null | undefined) => (id ? L(TEAMS.find((x) => x.id === id)?.name ?? id) : null);
const goalTitle = (id: ID | null | undefined) => (id && goals[id] ? L(goals[id].title) : null);
const taskTitle = (id: ID | null | undefined) => (id && tasks[id] ? L(tasks[id].title) : null);
const sameList = (a: string[], b: string[]) => a.length === b.length && a.every((x, i) => x === b[i]);

on("PATCH", "/goals/:id", (m, body) => {
  requireLogin();
  const gr = getGoal(m.groups!.id);
  if (!canEditGoal(gr)) throw new ApiError(403, t("mock.goal.editForbidden"));
  const b = body as Partial<GoalInput>;
  const target = { goal: gr.id, title: gr.title };
  const fc = (c: FieldChange) => emitFieldChange("GoalFieldChanged", { ...target, title: gr.title }, c);
  if (b.title !== undefined) {
    const next = b.title.trim();
    if (!next) throw new ApiError(400, t("mock.goal.emptyTitle"));
    if (next !== gr.title) { const from = gr.title; gr.title = next; fc({ field: "title", from: L(from), to: L(next) }); }
  }
  if (b.description !== undefined && b.description !== gr.description) { const from = gr.description; gr.description = b.description; fc({ field: "description", from, to: b.description }); }
  if (b.owner_id !== undefined && b.owner_id !== gr.owner_id) { const from = gr.owner_id; gr.owner_id = b.owner_id; fc({ field: "owner_member_id", from, to: b.owner_id, from_title: nameOf(from), to_title: nameOf(b.owner_id) }); }
  if (b.team_id !== undefined && (b.team_id || null) !== (gr.team_id ?? null)) { const from = gr.team_id ?? null; gr.team_id = b.team_id || null; fc({ field: "team_id", from, to: gr.team_id, from_title: teamName(from), to_title: teamName(gr.team_id) }); }
  if (b.parent_id !== undefined) {
    const to = b.parent_id || null;
    if (to && (to === gr.id || goalSubtreeIds(gr.id).includes(to))) throw new ApiError(400, t("mock.goal.parentCycle"));
    if (to && !goals[to]) throw new ApiError(404, t("mock.goal.notFound"));
    if (to !== gr.parent_id) { const from = gr.parent_id; gr.parent_id = to; fc({ field: "parent_id", from, to, from_title: goalTitle(from), to_title: goalTitle(to) }); }
  }
  if (b.budget !== undefined && b.budget !== gr.budget) { const from = gr.budget; gr.budget = b.budget; fc({ field: "budget", from, to: b.budget, extra: { currency: ORG.currency } }); }
  if (b.planned_start !== undefined && (b.planned_start || null) !== gr.planned_start) { const from = gr.planned_start; gr.planned_start = b.planned_start || null; fc({ field: "planned_start", from, to: gr.planned_start }); }
  if (b.planned_end !== undefined && (b.planned_end || null) !== gr.planned_end) {
    const from = gr.planned_end; gr.planned_end = b.planned_end || null; fc({ field: "planned_end", from, to: gr.planned_end });
    // 与后端一致：计划结束同时也是截止日（不另记一条动态），除非这次显式带了 deadline
    if (b.deadline === undefined) gr.deadline = gr.planned_end;
  }
  if (b.deadline !== undefined && (b.deadline || null) !== (gr.deadline ?? null)) { const from = gr.deadline ?? null; gr.deadline = b.deadline || null; fc({ field: "deadline", from, to: gr.deadline }); }
  if (b.achieved !== undefined && b.achieved !== gr.achieved) {
    const from = gr.status ?? (gr.achieved ? "achieved" : "active");
    gr.achieved = b.achieved; gr.actual_end = b.achieved ? day(0) : null; gr.status = b.achieved ? "achieved" : "active";
    fc({ field: "status", from, to: gr.status });
  }
  if (b.status !== undefined && b.status !== (gr.status ?? "active")) { const from = gr.status ?? "active"; gr.status = b.status; if (b.status === "active") gr.achieved = false; fc({ field: "status", from, to: b.status }); }
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
  if (!canEditTask(tk)) throw new ApiError(403, t("mock.task.editForbidden"));
  const b = body as Partial<TaskInput> & { estimate_hours?: number | null };
  const def = defOf(tk);
  const closed = stateOf(tk).label === "terminal_success" || stateOf(tk).label === "terminal_failure";
  // 先算出每个真正变了的字段，再统一应用：已结束的任务只允许描述与自定义字段
  const pending: Array<{ field: string; apply: () => void; change: FieldChange | null }> = [];
  const push = (field: string, apply: () => void, change: FieldChange | null) => pending.push({ field, apply, change });
  if (b.title !== undefined) {
    const next = b.title.trim();
    if (!next) throw new ApiError(400, t("mock.task.emptyTitle"));
    if (next !== tk.title) push("title", () => { tk.title = next; }, { field: "title", from: L(tk.title), to: L(next) });
  }
  if (b.description !== undefined && b.description !== tk.description) push("description", () => { tk.description = b.description!; }, { field: "description", from: tk.description, to: b.description });
  if (b.reviewer_id !== undefined && b.reviewer_id && b.reviewer_id !== tk.reviewer_id) push("reviewer_id", () => { tk.reviewer_id = b.reviewer_id!; }, { field: "reviewer_id", from: tk.reviewer_id, to: b.reviewer_id, from_title: nameOf(tk.reviewer_id), to_title: nameOf(b.reviewer_id) });
  if (b.priority !== undefined && b.priority !== tk.priority) push("priority", () => { tk.priority = b.priority!; }, { field: "priority", from: tk.priority, to: b.priority });
  const est = b.estimate !== undefined ? b.estimate : b.estimate_hours;
  if (est !== undefined && est !== tk.estimate) push("estimate_hours", () => { tk.estimate = est; }, { field: "estimate_hours", from: tk.estimate, to: est });
  if (b.planned_start !== undefined && (b.planned_start || null) !== tk.planned_start) push("planned_start", () => { tk.planned_start = b.planned_start || null; }, { field: "planned_start", from: tk.planned_start, to: b.planned_start || null });
  if (b.planned_end !== undefined && (b.planned_end || null) !== tk.planned_end) push("planned_end", () => { tk.planned_end = b.planned_end || null; }, { field: "planned_end", from: tk.planned_end, to: b.planned_end || null });
  if (b.human_only !== undefined && b.human_only !== (tk.human_only ?? false)) push("human_only", () => { tk.human_only = b.human_only; }, { field: "human_only", from: tk.human_only ?? false, to: b.human_only });
  if (b.goal_id !== undefined && (b.goal_id || null) !== tk.goal_id) {
    const to = b.goal_id || null;
    if (to && !goals[to]) throw new ApiError(404, t("mock.goal.notFound"));
    push("goal_id", () => { tk.goal_id = to; }, { field: "goal_id", from: tk.goal_id, to, from_title: goalTitle(tk.goal_id), to_title: goalTitle(to) });
  }
  if (b.parent_id !== undefined && (b.parent_id || null) !== tk.parent_id) {
    const to = b.parent_id || null;
    const descendants = (id: ID): ID[] => Object.values(tasks).filter((x) => x.parent_id === id).flatMap((x) => [x.id, ...descendants(x.id)]);
    if (to && (to === tk.id || descendants(tk.id).includes(to))) throw new ApiError(400, t("mock.task.parentCycle"));
    if (to && !tasks[to]) throw new ApiError(404, t("mock.task.notFound"));
    push("parent_id", () => { tk.parent_id = to; }, { field: "parent_id", from: tk.parent_id, to, from_title: taskTitle(tk.parent_id), to_title: taskTitle(to) });
  }
  if (b.required_capabilities !== undefined && !sameList(b.required_capabilities, tk.required_capabilities)) {
    const next = b.required_capabilities;
    const bad = next.find((c) => !CAPABILITIES.some((x) => x.name === c));
    if (bad) throw new ApiError(400, t("mock.task.badCapability", { name: bad }));
    const titles = (xs: string[]) => xs.map((c) => L(CAPABILITIES.find((x) => x.name === c)?.title ?? c));
    push("required_capabilities", () => { tk.required_capabilities = next; }, { field: "required_capabilities", from: tk.required_capabilities, to: next, from_title: titles(tk.required_capabilities), to_title: titles(next) });
  }
  if (b.participants) {
    for (const [slot, id] of Object.entries(b.participants)) {
      if (!def.participants[slot]) continue;
      const to = id || null;
      if (to === (tk.participants[slot] ?? null)) continue;
      const from = tk.participants[slot] ?? null;
      push("participants", () => { tk.participants[slot] = to; }, { field: "participants", from, to, from_title: nameOf(from), to_title: nameOf(to), extra: { slot, label: L(def.participants[slot].title) } });
    }
  }
  if (b.fields) {
    for (const [key, v] of Object.entries(b.fields)) {
      if (JSON.stringify(tk.fields[key] ?? null) === JSON.stringify(v ?? null)) continue;
      push("fields", () => { if (v === null || v === undefined || v === "") delete tk.fields[key]; else tk.fields[key] = v; }, { field: "fields", from: tk.fields[key] ?? null, to: v ?? null, extra: { key } });
    }
  }
  if (closed && pending.some((p) => p.field !== "description" && p.field !== "fields")) throw new ApiError(400, t("mock.task.closedEdit"));
  if (closed && (b.points !== undefined || b.sprint_id !== undefined)) throw new ApiError(400, t("mock.task.closedEdit"));
  for (const p of pending) {
    const change = p.change;
    p.apply();
    if (change) emitFieldChange("TaskFieldChanged", { task: tk.id, goal: tk.goal_id, title: tk.title }, change);
  }
  if (b.points !== undefined && b.points !== tk.points) {
    tk.points = b.points === null ? null : Math.max(0, Math.round(Number(b.points)));
    emit("PointsChanged", { task: tk.id, actor: ME, summary: tk.points === null ? t("mock.ev.pointsCleared") : t("mock.ev.pointsChanged", { n: tk.points }), data: { points: tk.points } });
  }
  if (b.sprint_id !== undefined) setTaskSprint(tk, b.sprint_id || null, ME);
  if (pending.length || b.points !== undefined || b.sprint_id !== undefined) tk.updated_at = nowISO();
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

// 可见范围：组织负责人 / 持「组织设置」权限的人看到全部并都能管；其他人只看到自己的加公共 Agent，公共 Agent 对非所有者只读
const viewAgent = (a: Agent): Agent => ({ ...a, can_manage: canManageOrg() || a.owner.id === ME });
on("GET", "/agents", () => { requireLogin(); return Object.values(AGENTS).filter((a) => canManageOrg() || a.owner.id === ME || a.shared).map(viewAgent); });
on("POST", "/agents", (_m, body): AgentRegistration => {
  requireLogin();
  const b = body as AgentInput;
  if (!b.name?.trim()) throw new ApiError(400, t("mock.agentEmptyName"));
  const a: Agent = { id: nextId("agent-"), name: b.name.trim(), owner: ref(ME), shared: !!b.shared, capabilities: b.capabilities ?? [], grants: b.grants ?? [], online: false, last_seen_at: null, max_concurrency: b.max_concurrency ?? 1, created_at: nowISO(), can_manage: true };
  AGENTS[a.id] = a;
  emit("AgentRegistered", { actor: ME, summary: t("mock.ev.agentRegistered", { name: a.name }) });
  const token = `axm_${Math.random().toString(36).slice(2)}${Math.random().toString(36).slice(2)}`;
  return { agent: a, token };
});
on("DELETE", "/agents/:id", (m) => {
  requireLogin();
  const a = AGENTS[m.groups!.id];
  if (!a) throw new ApiError(404, t("mock.agentNotFound"));
  if (!viewAgent(a).can_manage) throw new ApiError(403, t("mock.forbidden"));
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

// ---------- 工作台（ADR 0015 与补记二：/workspace、/org/workspace；区块带宽高） ----------
const BLOCK_DEFS: BlockDef[] = [
  { key: "inbox", title: "待我处理", description: "等我确认、验收、答复的事和我负责但逾期的任务，只看本人、不受范围影响。", default_w: 12, default_h: 2, min_w: 6 },
  { key: "readouts", title: "组织概况", description: "进行中、待验收、逾期与今日成本四个实时读数，带 24 小时趋势线。", default_w: 12, default_h: 1, min_w: 6 },
  { key: "overview_summary", title: "组织概览摘要", description: "当前范围这一个月的目标进度、任务、逾期、吞吐与成本合计。", default_w: 12, default_h: 1, min_w: 3 },
  { key: "exceptions", title: "要我关注的异常", description: "逾期与停滞的任务、逾期或超预算的目标、等我确认的操作。", default_w: 6, default_h: 2, min_w: 3 },
  { key: "my_tasks", title: "我的任务", description: "我和我的 Agent 名下未结束的任务。", default_w: 12, default_h: 2, min_w: 6 },
  { key: "my_review", title: "等我验收", description: "我是验收人、正在等我处理的任务。", default_w: 12, default_h: 2, min_w: 6 },
  { key: "team_load", title: "人员与 Agent 负荷", description: "每个人和 Agent 手上有多少活、有没有逾期。", default_w: 6, default_h: 2, min_w: 3 },
  { key: "cost_budget", title: "成本与预算", description: "这一个月的成本、预算执行率，以及成本最高的目标。", default_w: 6, default_h: 2, min_w: 3 },
  { key: "events", title: "最近动态", description: "当前范围里最近发生了什么。", default_w: 6, default_h: 3, min_w: 3 },
  { key: "sprint", title: "当前迭代", description: "进行中的迭代：进度、剩余天数与燃尽图。", default_w: 6, default_h: 2, min_w: 3 },
  { key: "backlog", title: "待领取任务", description: "现在我能领的任务。", default_w: 6, default_h: 2, min_w: 3 },
  { key: "trend", title: "趋势与环比", description: "成本与吞吐随时间的变化，与上一段时间对比。", default_w: 12, default_h: 2, min_w: 6 },
];
const blockDef = (k: BlockKey) => BLOCK_DEFS.find((b) => b.key === k);
/** 预设里的区块带坐标（与后端 internal/app 的内置预设一致）：[key, x, y, w, h]。补记四：每个预设最上面是「待我处理」，随后「组织概况」（执行视角不带它）。 */
type Placed = [BlockKey, number, number, number, number];
const placed = (xs: Placed[]): LayoutBlock[] => xs.map(([key, x, y, w, h]) => ({ key, x, y, w, h }));
const PRESETS: Preset[] = [
  {
    key: "global",
    title: "全局视角",
    description: "看全公司的概览、异常、成本与趋势，适合组织负责人。",
    blocks: placed([["inbox", 0, 0, 12, 2], ["readouts", 0, 2, 12, 1], ["overview_summary", 0, 3, 12, 1], ["exceptions", 0, 4, 6, 2], ["cost_budget", 6, 4, 6, 2], ["trend", 0, 6, 12, 2]]),
  },
  {
    key: "unit",
    title: "部门视角",
    description: "看本部的概览、异常与负荷，兼顾等我验收，适合部门负责人。",
    blocks: placed([["inbox", 0, 0, 12, 2], ["readouts", 0, 2, 12, 1], ["overview_summary", 0, 3, 12, 1], ["exceptions", 0, 4, 6, 2], ["team_load", 6, 4, 6, 2], ["my_review", 0, 6, 6, 3], ["events", 6, 6, 6, 3]]),
  },
  {
    key: "team",
    title: "小组视角",
    description: "先看等我验收和小组负荷，再看异常、我的任务与当前迭代，适合小组负责人。",
    blocks: placed([["inbox", 0, 0, 12, 2], ["readouts", 0, 2, 12, 1], ["my_review", 0, 3, 6, 2], ["team_load", 6, 3, 6, 2], ["exceptions", 0, 5, 6, 2], ["sprint", 6, 5, 6, 2], ["my_tasks", 0, 7, 12, 2], ["events", 0, 9, 12, 3]]),
  },
  {
    key: "doer",
    title: "执行视角",
    description: "先看我的任务，再看等我验收、待领取任务与当前迭代，适合一线成员。",
    blocks: placed([["inbox", 0, 0, 12, 2], ["my_tasks", 0, 2, 12, 2], ["my_review", 0, 4, 12, 2], ["backlog", 0, 6, 6, 2], ["sprint", 6, 6, 6, 2], ["events", 0, 8, 12, 3]]),
  },
  {
    key: "ops",
    title: "运营视角",
    description: "先看成本与趋势，再看异常与负荷，适合运营与流程管理。",
    blocks: placed([["inbox", 0, 0, 12, 2], ["readouts", 0, 2, 12, 1], ["cost_budget", 0, 3, 6, 2], ["trend", 6, 3, 6, 2], ["exceptions", 0, 5, 6, 2], ["team_load", 6, 5, 6, 2], ["events", 0, 7, 12, 3]]),
  },
];
const presetOf = (key: string) => PRESETS.find((p) => p.key === key);
const copyLayout = (xs: LayoutBlock[]) => xs.map((b) => ({ ...b }));
/**
 * 补记四的迁移（与后端一致）：补记四之前存下来的布局没有 `inbox` / `readouts`，加载时把缺的补到最上方、其余区块下移；
 * 之后用户自己移除的不再补回（这只在加载时对存好的布局跑一次，不在解析时跑；按新预设种下的角色布局不需要——执行视角本来就不带 readouts）。
 */
const migrateTopBlocks = (blocks: LayoutBlock[]): LayoutBlock[] => {
  const top: LayoutBlock[] = [];
  if (!blocks.some((b) => b.key === "inbox")) top.push({ key: "inbox", x: 0, y: 0, w: 12, h: 2 });
  if (!blocks.some((b) => b.key === "readouts")) top.push({ key: "readouts", x: 0, y: top.length ? 2 : 0, w: 12, h: 1 });
  if (!top.length) return blocks;
  const shift = top.reduce((s, b) => s + b.h, 0);
  return compactLayout([...top, ...blocks.map((b) => ({ ...b, y: b.y + shift }))]);
};
/** 首次种子：管理员 → 全局视角；运营、流程管理 → 运营视角；产品、设计、开发、测试、发布 → 执行视角。 */
const ROLE_LAYOUTS: Record<string, { blocks: LayoutBlock[]; preset: string | null }> = Object.fromEntries(
  ROLES.map((r) => {
    const key = r.name === "admin" ? "global" : r.name === "ops" || r.name === "workflow_admin" ? "ops" : "doer";
    return [r.name, { blocks: copyLayout(presetOf(key)!.blocks), preset: key }];
  }),
);
/** 补记四之前存下来的个人布局（示例数据里没有）：加载时过一遍 migrateTopBlocks */
const STORED_PERSONAL_LAYOUTS: Record<ID, LayoutBlock[]> = {};
/** 个人微调：成员 id → 区块（带宽高）；没有就是没调过 */
const PERSONAL_LAYOUTS: Record<ID, LayoutBlock[]> = Object.fromEntries(Object.entries(STORED_PERSONAL_LAYOUTS).map(([id, xs]) => [id, migrateTopBlocks(xs)]));
const blockView = (b: LayoutBlock): WorkspaceBlock => ({ ...b, title: blockDef(b.key)?.title ?? b.key });
/**
 * 解析 PUT 的 blocks：接受字符串数组或 {key, x?, y?, w?, h?} 对象数组（docs/api.md）；缺省宽高取目录默认，缺坐标的按顺序致密排布。
 * 校验：键存在且不重复、0 ≤ x、x + w ≤ 12、w ≥ min_w、1 ≤ h ≤ 6；坐标重叠的后一块另找空位；保存前向上压实。拒绝理由是完整的中文句子（与后端同句）。
 */
const parseBlocks = (v: unknown): LayoutBlock[] => {
  if (!Array.isArray(v)) throw new ApiError(400, t("mock.ws.needBlocks"));
  const raws: RawBlock[] = [];
  for (const item of v) {
    const key = typeof item === "string" ? item : item && typeof item === "object" ? (item as { key?: unknown }).key : undefined;
    if (typeof key !== "string" || !isBlockKey(key)) throw new ApiError(400, t("mock.ws.badBlock", { key: String(key) }));
    if (raws.some((b) => (typeof b === "string" ? b : b.key) === key)) throw new ApiError(400, t("mock.ws.dupBlock", { title: blockDef(key)?.title ?? key }));
    const def = blockDef(key)!;
    const raw = typeof item === "string" ? {} : (item as { x?: unknown; y?: unknown; w?: unknown; h?: unknown });
    const given = (k: "x" | "y" | "w" | "h") => raw[k] !== undefined && raw[k] !== null;
    if (given("w") && !isBlockWidth(raw.w)) throw new ApiError(400, t("mock.ws.badWidth", { title: def.title, n: def.min_w, cols: GRID_COLS }));
    if (given("h") && !isBlockHeight(raw.h)) throw new ApiError(400, t("mock.ws.badHeight", { title: def.title, max: GRID_MAX_H }));
    const w = isBlockWidth(raw.w) ? raw.w : def.default_w;
    if (w < def.min_w) throw new ApiError(400, t("mock.ws.badWidth", { title: def.title, n: def.min_w, cols: GRID_COLS }));
    if (given("x") && (typeof raw.x !== "number" || !Number.isInteger(raw.x) || raw.x < 0 || raw.x + w > GRID_COLS)) throw new ApiError(400, t("mock.ws.badX", { title: def.title }));
    if (given("y") && (typeof raw.y !== "number" || !Number.isInteger(raw.y) || raw.y < 0)) throw new ApiError(400, t("mock.ws.badY", { title: def.title }));
    raws.push({ key, x: raw.x as number | undefined, y: raw.y as number | undefined, w, h: isBlockHeight(raw.h) ? raw.h : def.default_h });
  }
  if (!raws.length) throw new ApiError(400, t("mock.ws.needBlocks"));
  return compactLayout(replaceOverlapping(normalizeLayout(raws, BLOCK_DEFS)));
};
/** 与后端一致：坐标重叠不拒绝，后面的那块丢掉坐标按顺序另找空位。 */
const replaceOverlapping = (xs: LayoutBlock[]): LayoutBlock[] => normalizeLayout(xs.map((b, i) => (xs.slice(0, i).some((p) => blocksOverlap(p, b)) ? { key: b.key, w: b.w, h: b.h } : b)), BLOCK_DEFS);
/** 解析顺序：个人微调 → 各角色布局并集（按首次出现顺序去重，宽高取首次出现的那份）→ 默认（组织负责人 global，其他 doer）。 */
function resolveWorkspace(): Workspace {
  const me = MEMBERS[ME];
  const personal = PERSONAL_LAYOUTS[ME];
  if (personal) return { blocks: personal.map(blockView), source: "personal", roles_used: [], can_customize: true };
  const used = me.roles.filter((r) => ROLE_LAYOUTS[r]);
  if (used.length) {
    const merged: LayoutBlock[] = [];
    for (const r of used) for (const b of ROLE_LAYOUTS[r].blocks) if (!merged.some((x) => x.key === b.key)) merged.push({ ...b });
    // 并集里来自后面角色的块可能与前面的重叠：重叠的丢掉坐标按顺序找空位，再向上压实
    const union = compactLayout(replaceOverlapping(merged));
    return { blocks: union.map(blockView), source: "roles", roles_used: used, can_customize: true };
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
    blocks: copyLayout(layout ? layout.blocks : fallback.blocks),
    preset: layout ? layout.preset : fallback.key,
    member_count: Object.values(MEMBERS).filter((m) => m.active && m.roles.includes(r.name)).length,
  };
};
on("GET", "/workspace", () => { requireLogin(); return resolveWorkspace(); });
on("PUT", "/workspace/me", (_m, body) => {
  requireLogin();
  PERSONAL_LAYOUTS[ME] = parseBlocks((body as { blocks?: unknown } | null)?.blocks);
  emit("WorkspaceLayoutUpdated", { actor: ME, summary: t("mock.ev.workspaceLayoutMine"), data: { blocks: PERSONAL_LAYOUTS[ME] } });
  return resolveWorkspace();
});
on("DELETE", "/workspace/me", () => { requireLogin(); delete PERSONAL_LAYOUTS[ME]; return undefined; });
on("GET", "/workspace/blocks", (): WorkspaceCatalog => { requireLogin(); return { blocks: BLOCK_DEFS.filter((b) => BLOCK_KEYS.includes(b.key)), presets: PRESETS.map((p) => ({ ...p, blocks: copyLayout(p.blocks) })) }; });
on("GET", "/org/workspace", () => { requireOrgAdmin(); return ROLES.map(viewRoleWorkspace); });
on("PUT", "/org/workspace/:role", (m, body) => {
  requireOrgAdmin();
  const role = roleOf(m.groups!.role);
  if (!role) throw new ApiError(404, t("mock.org.roleNotFound"));
  const b = (body ?? {}) as { blocks?: unknown; preset?: unknown };
  if (typeof b.preset === "string") {
    const p = presetOf(b.preset);
    if (!p) throw new ApiError(400, t("mock.ws.badPreset"));
    ROLE_LAYOUTS[role.name] = { blocks: copyLayout(p.blocks), preset: p.key };
  } else {
    const blocks = parseBlocks(b.blocks);
    const same = PRESETS.find((p) => sameLayout(p.blocks, blocks));
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
const isSyncedRow = (x: { source?: string }) => !!x.source && x.source !== "manual";
/** 换直属团队：从所有团队里摘掉，再放进目标团队（null = 不属于任何团队） */
const moveMember = (mem: MemberRow, teamId: ID | null) => {
  for (const tm of TEAMS) tm.member_ids = tm.member_ids.filter((x) => x !== mem.id);
  const tm = TEAMS.find((x) => x.id === teamId);
  mem.team_id = tm?.id ?? null;
  if (tm) tm.member_ids.push(mem.id);
};
on("GET", "/org/members", () => { requireOrgAdmin(); const dups = findDuplicates(); return Object.values(MEMBERS).map((m) => viewOrgMember(m, dups)); });
on("PATCH", "/org/members/:id", (m, body) => {
  requireOrgAdmin();
  const mem = getMember(m.groups!.id);
  const b = (body ?? {}) as { name?: string; roles?: string[]; active?: boolean; team_id?: ID | null };
  if (b.team_id !== undefined && b.team_id !== mem.team_id && isSyncedRow(mem)) throw new ApiError(409, t("mock.people.syncedMemberTeam", { name: sourceTitle(mem.source) }));
  if (b.active === false && ORG.owner_id === mem.id) throw new ApiError(409, t("mock.people.ownerKeep"));
  if (b.name !== undefined && !isSyncedRow(mem)) mem.name = b.name;
  if (b.roles !== undefined) mem.roles = b.roles.filter((r) => roleOf(r));
  if (b.active !== undefined) mem.active = b.active;
  if (b.team_id !== undefined) moveMember(mem, b.team_id);
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
  const b = (body ?? {}) as { email?: string; name?: string; roles?: string[]; team_id?: ID | null };
  if (!b.email?.trim()) throw new ApiError(400, t("mock.org.needEmail"));
  const row: InvitationRow = { id: nextId("I"), token: `inv_${Math.random().toString(36).slice(2, 10)}`, email: b.email.trim(), name: b.name?.trim() ?? "", roles: b.roles ?? [], url: "", expires_at: at(7), accepted_at: null, created_at: nowISO() };
  INVITATIONS.push(row);
  // 被邀请的人立刻以「待激活」出现在成员表里（成员与团队页），接受邀请后转正常
  let mem = Object.values(MEMBERS).find((x) => x.email.toLowerCase() === row.email.toLowerCase());
  if (!mem) {
    mem = { id: nextId("m"), name: row.name || row.email, email: row.email, roles: row.roles, team_id: null, locale: getLocale(), active: true, created_at: nowISO(), source: "manual", status: "pending_activation", invite_token: row.token };
    MEMBERS[mem.id] = mem;
    ADMIN_ORGS[0].member_count += 1;
  } else if (mem.status === "pending_activation") {
    mem.invite_token = row.token;
    if (row.roles.length) mem.roles = row.roles;
  }
  if (b.team_id !== undefined && mem.status === "pending_activation") {
    const tm = TEAMS.find((x) => x.id === b.team_id);
    for (const x of TEAMS) x.member_ids = x.member_ids.filter((id) => id !== mem!.id);
    mem.team_id = tm?.id ?? null;
    if (tm) tm.member_ids.push(mem.id);
  }
  return viewInvitation(row);
});
on("DELETE", "/org/invitations/:id", (m) => {
  requireOrgAdmin();
  const i = INVITATIONS.findIndex((x) => x.id === m.groups!.id);
  if (i < 0) throw new ApiError(404, t("mock.invite.notFound"));
  const [row] = INVITATIONS.splice(i, 1);
  // 撤回邀请 = 连带删掉它建出的、还没接受的待激活成员
  const mem = Object.values(MEMBERS).find((x) => x.invite_token === row.token && x.status === "pending_activation");
  if (mem) { for (const tm of TEAMS) tm.member_ids = tm.member_ids.filter((id) => id !== mem.id); delete MEMBERS[mem.id]; ADMIN_ORGS[0].member_count = Math.max(0, ADMIN_ORGS[0].member_count - 1); }
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
// ---------- IM 集成同步（ADR 0017 及补记）：提供方是数据——这里的两份声明就是"服务器会返回的数据"，界面代码里没有平台名 ----------
/** 可接入的提供方声明（与 internal/directory 里注册的一致；标题、提示、指引按当前语言给） */
function mockProviders(): DirectoryProviderInfo[] {
  const zh = getLocale() === "zh-CN";
  const x = (a: string, b: string) => (zh ? a : b);
  return [
    {
      key: "feishu", title: x("飞书", "Feishu"), root_department_id: "0",
      fields: [
        { key: "app_id", title: "App ID", secret: false, placeholder: "cli_xxxxxxxxxxxxxxxx", hint: x("飞书开放平台 → 开发者后台 → 应用 → 凭证与基础信息", "Feishu Open Platform → Developer Console → the app → Credentials & Basic Info") },
        { key: "app_secret", title: "App Secret", secret: true, placeholder: "", hint: x("与 App ID 同一页；保存后只显示是否已设置", "Same page as the App ID; only whether it is set is shown after saving") },
      ],
      prerequisites: [
        x("在飞书开放平台创建一个企业自建应用", "Create a custom enterprise app on Feishu Open Platform"),
        x("为它开通通讯录只读权限（contact:contact.base:readonly 等），并发布版本", "Grant it read-only contact permissions (contact:contact.base:readonly, etc.) and publish a version"),
        x("把要同步的部门加进应用的通讯录权限范围", "Add the departments to sync to the app's contact permission scope"),
      ],
      tip: { text: x("App ID 和 App Secret 在飞书开放平台里你创建的企业自建应用的「凭证与基础信息」页；应用要开通通讯录只读权限并发布版本。", "The App ID and App Secret are on the “Credentials & Basic Info” page of your custom app on Feishu Open Platform; the app needs read-only contact permissions and a released version."), url: "https://open.feishu.cn/app" },
    },
    {
      key: "wecom", title: x("企业微信", "WeCom"), root_department_id: "1",
      fields: [
        { key: "corp_id", title: x("企业 ID", "Corp ID"), secret: false, placeholder: "wwxxxxxxxxxxxxxxxx", hint: x("企业微信管理后台 → 我的企业 → 企业信息 → 企业 ID", "WeCom admin console → My Company → Company Info → Corp ID") },
        { key: "corp_secret", title: x("通讯录同步 Secret", "Contacts sync Secret"), secret: true, placeholder: "", hint: x("管理后台 → 管理工具 → 通讯录同步 → Secret；保存后只显示是否已设置", "Admin console → Management Tools → Contacts Sync → Secret; only whether it is set is shown after saving") },
      ],
      prerequisites: [
        x("在企业微信管理后台的「管理工具 → 通讯录同步」里开启 API 接口同步，取得 Secret", "In the WeCom admin console open Management Tools → Contacts Sync, enable API sync and copy the Secret"),
        x("把本系统的出网 IP 加进通讯录同步的可信 IP 列表", "Add this system's outbound IP to the contacts-sync trusted IP list"),
        x("新建的应用可能拿不到姓名、手机、邮箱等敏感字段；拿不到时只同步能拿到的字段，并在同步结果里说明", "Newly created apps may not receive sensitive fields (name, mobile, email); then only the available fields are synced and the sync result says so"),
      ],
      tip: { text: x("企业 ID 在企业微信管理后台「我的企业 → 企业信息」最底部；通讯录同步 Secret 在「管理工具 → 通讯录同步」开启 API 接口同步后显示，并要把服务器出网 IP 加入企业可信 IP。", "The Corp ID is at the bottom of “My Company → Company Info” in the WeCom admin console; the contacts-sync Secret appears under “Management Tools → Contacts Sync” after enabling API sync, and the server’s outbound IP must be added to the trusted IPs."), url: "https://work.weixin.qq.com/wework_admin/frame" },
    },
  ];
}
const providerOf = (key: string | null | undefined) => (key ? mockProviders().find((p) => p.key === key) ?? null : null);
/** 成员 / 团队来源的界面名：手工，或提供方名称（找不到声明时退回代码名） */
function sourceTitle(source: string | undefined): string {
  if (!source || source === "manual") return t("mock.source.manual");
  return providerOf(source)?.title ?? source;
}

interface DirectoryState { provider: string | null; credentials: Record<string, string>; secrets: Record<string, string>; root_department_id: string; root_department_ids: string[]; default_role: string; schedule: DirectoryConfig["schedule"]; proxy_url: string }
const DIR: DirectoryState = { provider: "feishu", credentials: { app_id: "cli_a1b2c3d4e5f6" }, secrets: { app_secret: "***" }, root_department_id: "od-product", root_department_ids: [], default_role: "developer", schedule: "daily", proxy_url: "" };
/** 已接入 = 选了提供方，且它声明的每个字段都有值 */
const dirConfigured = () => { const p = providerOf(DIR.provider); return !!p && p.fields.every((f) => (f.secret ? !!DIR.secrets[f.key] : !!DIR.credentials[f.key])); };
/** 外部身份：IM 集成里的编号 -> 本地 id（同一个人 / 部门在多次同步之间靠它认出来）。换提供方时清空：旧来源的身份不再匹配 */
let EXT_TEAMS: Record<string, ID> = { "od-product": "team-rd", "od-fe": "team-fe", "od-qa": "team-qa", "od-old": "team-old" };
let EXT_MEMBERS: Record<string, ID> = { "ou-li": "li", "ou-zhang": "zhang", "ou-zhao": "zhao", "ou-wu": "wu", "ou-zheng": "zheng" };
/** 每条外部身份是什么时候绑上的（对应关系面板的 since）；没记录的是第一次同步时绑的 */
const EXT_SINCE: Record<string, string> = {};
const sinceOf = (kind: DirectoryKind, ext: string) => EXT_SINCE[`${kind}:${ext}`] ?? at(-3, 3);
/** 对候选的决定（ADR 0017 补记四）：同一外部对象只保留最后一次；换提供方时清空 */
interface DecisionRow { provider: string; kind: DirectoryKind; external_id: string; external_name: string; decision: DirectoryDecision; local_id?: ID; decided_at: string }
let DIR_DECISIONS: DecisionRow[] = [];
const decisionOf = (kind: DirectoryKind, ext: string) => DIR_DECISIONS.find((d) => d.provider === DIR.provider && d.kind === kind && d.external_id === ext);
const decisionTitle = (d: DirectoryDecision) => t(`mock.directory.decision.${d}` as Key);
const viewDecision = (d: DecisionRow): DirectoryDecisionRecord => {
  const local = d.local_id ? (d.kind === "team" ? TEAMS.find((x) => x.id === d.local_id)?.name : MEMBERS[d.local_id]?.name) : undefined;
  return { kind: d.kind, external_id: d.external_id, external_name: d.external_name, decision: d.decision, decision_title: decisionTitle(d.decision), local_id: d.local_id ?? null, local_name: local ?? null, decided_at: d.decided_at };
};
/** 团队 / 成员的原始名字（不走 L()，认法要按存的名字比） */
const rawTeamName = (id: ID | null | undefined) => (id ? TEAMS.find((x) => x.id === id)?.name ?? null : null);
const candidateOf = (kind: DirectoryKind, id: ID, reason: DirectoryMatchReason, reasonText: string): DirectoryCandidate => {
  if (kind === "team") { const tm = TEAMS.find((x) => x.id === id)!; return { local_id: id, name: tm.name, team_path: teamPath(tm.parent_id), source: tm.source ?? "manual", source_title: sourceTitle(tm.source), reason, reason_text: reasonText }; }
  const m = MEMBERS[id]; return { local_id: id, name: m.name, team_path: teamPath(m.team_id), source: m.source ?? "manual", source_title: sourceTitle(m.source), reason, reason_text: reasonText };
};
/** 同步之后，用同一套认法（第二至四级）在手工对象（a）与同步对象（b）之间找疑似重复；停用的不算 */
function findDuplicates(): DirectoryDuplicate[] {
  const out: DirectoryDuplicate[] = [];
  const side = (kind: DirectoryKind, id: ID): DirectoryDuplicateSide => { const c = candidateOf(kind, id, "email", ""); return { id, name: c.name, source: c.source, source_title: c.source_title, team_path: c.team_path }; };
  const members = Object.values(MEMBERS).filter((m) => m.active);
  const countNames = (rows: typeof members) => rows.reduce<Record<string, number>>((acc, m) => { acc[m.name] = (acc[m.name] ?? 0) + 1; return acc; }, {});
  const manualNames = countNames(members.filter((m) => !isSyncedRow(m)));
  const syncedNames = countNames(members.filter((m) => isSyncedRow(m)));
  for (const a of members.filter((m) => !isSyncedRow(m))) {
    for (const b of members.filter((m) => isSyncedRow(m))) {
      if (a.email.toLowerCase() === b.email.toLowerCase()) out.push({ kind: "member", a: side("member", a.id), b: side("member", b.id), reason: "email", reason_text: t("mock.directory.reason.email") });
      else if (a.name === b.name && a.team_id && rawTeamName(a.team_id) === rawTeamName(b.team_id)) out.push({ kind: "member", a: side("member", a.id), b: side("member", b.id), reason: "name_team", reason_text: t("mock.directory.reason.name_team", { team: L(rawTeamName(a.team_id)!) }) });
      else if (a.name === b.name && manualNames[a.name] === 1 && syncedNames[b.name] === 1) out.push({ kind: "member", a: side("member", a.id), b: side("member", b.id), reason: "name_unique", reason_text: t("mock.directory.reason.name_unique") });
    }
  }
  const teams = TEAMS.filter((x) => x.active !== false);
  for (const a of teams.filter((x) => !isSyncedRow(x))) for (const b of teams.filter((x) => isSyncedRow(x))) {
    if (a.name === b.name && (a.parent_id ?? null) === (b.parent_id ?? null)) out.push({ kind: "team", a: side("team", a.id), b: side("team", b.id), reason: "team_name_level", reason_text: t("mock.directory.reason.team_name_level") });
  }
  return out;
}
/** IM 集成里现在的部门树（根部门之下）：测试组改了名，多了一个数据平台组，旧项目组没了 */
const REMOTE_TEAMS: Array<{ ext: string; name: string; parent: string | null }> = [
  { ext: "od-product", name: "产品事业部", parent: null },
  { ext: "od-fe", name: "前端组", parent: "od-product" },
  { ext: "od-qa", name: "质量组", parent: "od-product" },
  { ext: "od-data", name: "数据平台组", parent: "od-product" },
];
/** IM 集成里现在的人：小赵已经离开；多了冯小北与韩小南（韩小南没有邮箱） */
const REMOTE_MEMBERS: Array<{ ext: string; name: string; email: string | null; team: string }> = [
  { ext: "ou-li", name: "小李", email: "li@example.com", team: "od-fe" },
  { ext: "ou-zhang", name: "小张", email: "zhang@example.com", team: "od-qa" },
  { ext: "ou-wu", name: "吴小雨", email: "wu@example.com", team: "od-fe" },
  { ext: "ou-zheng", name: "郑一", email: "zheng@example.com", team: "od-qa" },
  { ext: "ou-feng", name: "冯小北", email: "feng@example.com", team: "od-data" },
  { ext: "ou-han", name: "韩小南", email: null, team: "od-data" },
];
const DIR_RUNS: DirectoryRun[] = [
  { id: "dr1", provider: "feishu", started_at: at(-3, 3), finished_at: at(-3, 3, 1), status: "ok", status_title: "", added_teams: 4, updated_teams: 0, deactivated_teams: 0, added_members: 5, updated_members: 0, deactivated_members: 0, errors: [] },
];
const runStatusTitle = (st: DirectoryRun["status"]) => (st === "ok" ? t("mock.directory.status.ok") : st === "partial" ? t("mock.directory.status.partial") : t("mock.directory.status.failed"));
const viewDirRun = (r: DirectoryRun): DirectoryRun => ({ ...r, status_title: runStatusTitle(r.status) });
const SCHEDULES: DirectoryConfig["schedules"] = [
  { value: "manual", title: "手动" }, { value: "hourly", title: "每小时" }, { value: "daily", title: "每天" },
];
/** GET /org/directory：保密字段永远只给"有没有设"；providers[] 里当前提供方的字段带 set / value */
const viewDirectory = (): DirectoryConfig => {
  const cur = providerOf(DIR.provider);
  const providers = mockProviders().map((p) => (p.key !== DIR.provider ? p : { ...p, fields: p.fields.map((f) => (f.secret ? { ...f, set: !!DIR.secrets[f.key] } : { ...f, set: !!DIR.credentials[f.key], value: DIR.credentials[f.key] || undefined })) }));
  const secrets_set: Record<string, boolean> = {};
  for (const f of cur?.fields ?? []) if (f.secret) secrets_set[f.key] = !!DIR.secrets[f.key];
  return {
    provider: DIR.provider, provider_title: cur?.title ?? "", configured: dirConfigured(), credentials: { ...DIR.credentials }, secrets_set, providers,
    root_department_id: DIR.root_department_id, root_department_ids: [...DIR.root_department_ids], default_role: DIR.default_role,
    schedule: DIR.schedule, schedule_title: SCHEDULES.find((x) => x.value === DIR.schedule)?.title ?? DIR.schedule, schedules: SCHEDULES, proxy_url: DIR.proxy_url,
    last_run: lastRunOfCurrent(),
  };
};
/** 上次同步只算当前提供方的：换了提供方就等于还没同步过 */
const lastRunOfCurrent = () => { const r = [...DIR_RUNS].reverse().find((x) => x.provider === DIR.provider); return r ? viewDirRun(r) : null; };
/** 同步根：勾了多个就按多个（每个成为顶层团队）；否则按单个根部门；根部门等于提供方的根 = 整个企业 */
const syncRoots = (): string[] | null => {
  if (DIR.root_department_ids.length) return DIR.root_department_ids;
  const p = providerOf(DIR.provider);
  return !p || !DIR.root_department_id || DIR.root_department_id === p.root_department_id ? null : [DIR.root_department_id];
};
/** 示例数据的剧本（ADR 0017 补记二）：保密字段以 bad 开头 → 凭据被拒；含 noname → 部门 / 人员读得到但没名字（字段级权限缺失）；
 *  其他 → 应用只被授权了部分部门（scope 待处理，给出可选的根）且邮箱权限未开；选好同步根后范围通过。 */
const CHECK_ORDER = ["credentials", "scope", "dept_names", "user_names", "emails", "published"] as const;
const SUGGESTED_ROOTS = () => REMOTE_TEAMS.filter((r) => r.parent === "od-product").map((r) => ({ id: r.ext, name: r.name }));
function directoryChecklist(): DirectoryChecklist {
  const p = providerOf(DIR.provider);
  if (!p || !dirConfigured()) throw new ApiError(400, t("mock.directory.notConfigured"));
  const secret = p.fields.filter((f) => f.secret).map((f) => DIR.secrets[f.key] ?? "").join("");
  const bad = secret.startsWith("bad");
  const noname = secret.includes("noname");
  const appId = Object.values(DIR.credentials)[0] ?? "";
  // 控制台链接：尽量直达"这个应用"的那一页（这是服务端的知识，界面只管打开）
  const consoleUrl = p.tip?.url ?? "";
  const appPage = (page: string) => (p.key === "feishu" && appId ? `https://open.feishu.cn/app/${appId}/${page}` : consoleUrl);
  const rootsChosen = syncRoots() !== null;
  const scopePartial = !rootsChosen;
  const fieldTitles = p.fields.map((f) => f.title).join(getLocale() === "zh-CN" ? "、" : ", ");
  const mk = (key: (typeof CHECK_ORDER)[number], status: DirectoryCheck["status"], blocking: boolean, extra: Partial<DirectoryCheck> = {}): DirectoryCheck => ({ key, title: t(`mock.directory.check.${key}` as Key), status, blocking, ...extra });
  const skipped = (key: (typeof CHECK_ORDER)[number]) => mk(key, "skipped", false, { detail: t("mock.directory.check.skippedDetail") });
  const checks: DirectoryCheck[] = [];
  if (bad) {
    checks.push(mk("credentials", "blocked", true, { fix: t("mock.directory.check.credentials.fix", { name: p.title, fields: fieldTitles }), fix_url: appPage("baseinfo"), detail: t("mock.directory.check.credentials.detail") }));
    for (const k of CHECK_ORDER.slice(1)) checks.push(skipped(k));
  } else {
    checks.push(mk("credentials", "ok", true, { detail: t("mock.directory.check.credentials.okDetail", { tenant: ORG.name }) }));
    checks.push(scopePartial
      ? mk("scope", "todo", false, { fix: t("mock.directory.check.scope.fix", { n: SUGGESTED_ROOTS().length }), fix_url: appPage("auth"), detail: t("mock.directory.check.scope.detail", { names: SUGGESTED_ROOTS().map((r) => r.name).join(" / ") }) })
      : mk("scope", "ok", false, { detail: t("mock.directory.check.scope.okDetail", { n: syncRoots()!.length }) }));
    if (noname) {
      checks.push(mk("dept_names", "blocked", true, { fix: t("mock.directory.check.dept_names.fix", { name: p.title }), fix_url: appPage("auth"), detail: t("mock.directory.check.dept_names.detail") }));
      checks.push(mk("user_names", "blocked", true, { fix: t("mock.directory.check.user_names.fix", { name: p.title }), fix_url: appPage("auth"), detail: t("mock.directory.check.user_names.detail") }));
      checks.push(skipped("emails"));
    } else {
      checks.push(mk("dept_names", "ok", true));
      checks.push(mk("user_names", "ok", true));
      checks.push(mk("emails", "todo", false, { fix: t("mock.directory.check.emails.fix", { name: p.title }), fix_url: appPage("auth"), detail: t("mock.directory.check.emails.detail") }));
    }
    checks.push(mk("published", "ok", true, { detail: t("mock.directory.check.published.okDetail") }));
  }
  const ready = !checks.some((c) => c.status === "blocked");
  const synced = lastRunOfCurrent()?.status !== undefined && lastRunOfCurrent()!.status !== "failed";
  const step: DirectoryChecklist["next"]["step"] = !ready ? "checks" : scopePartial ? "scope" : checks.some((c) => c.status === "todo") ? "checks" : !synced ? "preview" : "schedule";
  return {
    provider: p.key, provider_title: p.title, console_url: consoleUrl, checks, ready,
    next: { step, text: t(`mock.directory.next.${step}` as Key, { name: p.title }) },
    // 应用被授权的部门（示例数据里应用永远只被授权了部分部门）：范围待选时用来勾，选好后用来显示名字
    suggested_roots: ready ? SUGGESTED_ROOTS() : undefined,
    root_department_id: DIR.root_department_id,
  };
}
/** 预览 / 同步前的门：有阻塞项就 400，句子里说清楚卡在哪 */
const requireReady = () => {
  const cl = directoryChecklist();
  if (!cl.ready) throw new ApiError(400, t("mock.directory.notReady", { reasons: cl.checks.filter((c) => c.status === "blocked").map((c) => c.title).join(getLocale() === "zh-CN" ? "、" : ", ") }));
};
/**
 * 对照IM 集成与本地：每个部门是新建 / 更新 / 保持 / 待确认；本地有外部身份但IM 集成里没了的部门和人要停用；来自旧来源的只提醒、不停用。
 * 冲突（ADR 0017 补记四）：认人认团队分四级——外部身份直接算同一个；邮箱相同 / 姓名相同且部门与本地主团队同名（成员）、同名同层级（团队）只产生候选，
 * 进 confirmations 等人决定；已决定的按决定走（merge → 视为已绑定、create → 新建、skip → 不同步）。
 */
function directoryPlan() {
  // 同步范围：勾了根就只要根和它们下面的部门，根成为顶层；否则整棵树
  const roots = syncRoots();
  const inScope = (ext: string): boolean => { if (!roots) return true; let cur: string | null = ext; while (cur) { if (roots.includes(cur)) return true; cur = REMOTE_TEAMS.find((r) => r.ext === cur)?.parent ?? null; } return false; };
  let remote = REMOTE_TEAMS.filter((r) => inScope(r.ext)).map((r) => (roots?.includes(r.ext) ? { ...r, parent: null } : r));
  const confirmations: DirectoryConfirmation[] = [];
  const decided: DirectoryDecided[] = [];
  const skipped: DirectorySkipped[] = [];
  // 跳过的部门不同步，它的子部门提到它的父下面
  for (const r of [...remote]) {
    const d = decisionOf("team", r.ext);
    if (d?.decision !== "skip") continue;
    skipped.push({ kind: "team", external_id: r.ext, external_name: r.name, decided_at: d.decided_at });
    remote = remote.filter((x) => x.ext !== r.ext).map((x) => (x.parent === r.ext ? { ...x, parent: r.parent } : x));
  }
  // 已被外部身份或合并决定占用的本地对象不再当别人的候选
  const takenTeams = new Set<ID>([...Object.values(EXT_TEAMS), ...DIR_DECISIONS.filter((d) => d.provider === DIR.provider && d.kind === "team" && d.decision === "merge" && d.local_id).map((d) => d.local_id!)]);
  const takenMembers = new Set<ID>([...Object.values(EXT_MEMBERS), ...DIR_DECISIONS.filter((d) => d.provider === DIR.provider && d.kind === "member" && d.decision === "merge" && d.local_id).map((d) => d.local_id!)]);
  /** 外部部门对应的本地团队：外部身份，或合并决定 */
  const teamLocal = (ext: string): ID | undefined => EXT_TEAMS[ext] ?? (decisionOf("team", ext)?.decision === "merge" ? decisionOf("team", ext)!.local_id : undefined);
  const remoteName = (ext: string | null | undefined) => (ext ? REMOTE_TEAMS.find((x) => x.ext === ext)?.name : undefined);
  const teams: DirectoryTeamPlan[] = remote.map((r) => {
    const parentLocal = r.parent ? teamLocal(r.parent) ?? null : null;
    const bound = EXT_TEAMS[r.ext];
    const dec = decisionOf("team", r.ext);
    const localId = bound ?? (dec?.decision === "merge" ? dec.local_id : undefined);
    const local = localId ? TEAMS.find((x) => x.id === localId) : undefined;
    const base = { external_id: r.ext, name: r.name, parent_external_id: r.parent ?? undefined };
    // 同名同层级的本地团队（父团队也对上，或都是顶层）
    const cands = TEAMS.filter((x) => x.active !== false && x.name === r.name && (x.parent_id ?? null) === parentLocal && !isSyncedRow(x));
    const external = { id: r.ext, name: r.name, parent: remoteName(r.parent) };
    const asCandidates = (ids: ID[]) => ids.map((id) => candidateOf("team", id, "team_name_level", t("mock.directory.reason.team_name_level")));
    if (dec && dec.decision !== "skip") decided.push({ kind: "team", external, candidates: asCandidates(cands.map((x) => x.id)), decision: dec.decision, decision_title: decisionTitle(dec.decision), decided_local_id: dec.local_id ?? null, decided_local_name: dec.local_id ? rawTeamName(dec.local_id) : null });
    if (local) {
      const action: DirectoryTeamPlan["action"] = local.name !== r.name || (local.parent_id ?? null) !== parentLocal || !local.active ? "update" : "keep";
      return { ...base, local_id: local.id, action, ...(bound ? {} : { bind: true }) };
    }
    if (dec?.decision === "create") return { ...base, local_id: null, action: "create" };
    const open = cands.filter((x) => !takenTeams.has(x.id));
    if (open.length) {
      confirmations.push({ kind: "team", external, candidates: asCandidates(open.map((x) => x.id)) });
      return { ...base, local_id: null, action: "confirm", candidates: open.map((x) => ({ local_id: x.id, reason: "team_name_level" as const })) };
    }
    return { ...base, local_id: null, action: "create" };
  });
  const remoteTeamExts = new Set(remote.map((r) => r.ext));
  const teamsToDeactivate = Object.entries(EXT_TEAMS).filter(([ext, id]) => !remoteTeamExts.has(ext) && TEAMS.find((x) => x.id === id)?.active).map(([, id]) => ({ id, name: TEAMS.find((x) => x.id === id)!.name }));
  // 成员：跳过的不进名单；合并决定的视为已有；其余按邮箱 / 姓名+部门找候选
  const mergeTargets: Record<string, ID> = {};
  const members = REMOTE_MEMBERS.filter((r) => remoteTeamExts.has(r.team)).filter((r) => {
    const d = decisionOf("member", r.ext);
    if (d?.decision === "skip") { skipped.push({ kind: "member", external_id: r.ext, external_name: r.name, decided_at: d.decided_at }); return false; }
    return true;
  });
  const membersNew: typeof members = [];
  const membersExisting: typeof members = [];
  const membersToConfirm: typeof members = [];
  for (const r of members) {
    const dec = decisionOf("member", r.ext);
    const deptName = remoteName(r.team) ?? "";
    const byEmail = r.email ? Object.values(MEMBERS).filter((m) => !isSyncedRow(m) && m.email.toLowerCase() === r.email!.toLowerCase()) : [];
    const byName = Object.values(MEMBERS).filter((m) => !isSyncedRow(m) && m.name === r.name && rawTeamName(m.team_id) === deptName);
    const cands = byEmail.length ? byEmail.map((m) => candidateOf("member", m.id, "email", t("mock.directory.reason.email"))) : byName.map((m) => candidateOf("member", m.id, "name_team", t("mock.directory.reason.name_team", { team: L(deptName) })));
    const external = { id: r.ext, name: r.name, dept: deptName, email_masked: r.email ? r.email.replace(/^(.).*(@.*)$/, "$1***$2") : undefined, mobile_tail: undefined };
    if (dec && dec.decision !== "skip") decided.push({ kind: "member", external, candidates: cands, decision: dec.decision, decision_title: decisionTitle(dec.decision), decided_local_id: dec.local_id ?? null, decided_local_name: dec.local_id ? MEMBERS[dec.local_id]?.name ?? null : null });
    if (EXT_MEMBERS[r.ext]) { membersExisting.push(r); continue; }
    if (dec?.decision === "merge" && dec.local_id) { mergeTargets[r.ext] = dec.local_id; membersExisting.push(r); continue; }
    if (dec?.decision === "create") { membersNew.push(r); continue; }
    const open = cands.filter((c) => !takenMembers.has(c.local_id));
    if (open.length) { confirmations.push({ kind: "member", external, candidates: open }); membersToConfirm.push(r); continue; }
    membersNew.push(r);
  }
  const remoteMemberExts = new Set(members.map((r) => r.ext));
  const membersToDeactivate = Object.entries(EXT_MEMBERS).filter(([ext, id]) => !remoteMemberExts.has(ext) && MEMBERS[id]?.active && ORG.owner_id !== id).map(([, id]) => ({ id, name: MEMBERS[id].name }));
  const providerTitle = providerOf(DIR.provider)?.title ?? "";
  // 没邮箱的人要手工邀请——除非他要并入一个已有成员（那就有邮箱了）或还在等确认
  const notes = membersNew.filter((r) => !r.email).map((r) => t("mock.directory.noteNoEmail", { name: r.name, provider: providerTitle }));
  // 换过提供方：来自旧来源、还在用的团队与成员改为手工维护（ADR 0017 补记：不自动停用）
  const oldSource = (x: string | undefined) => !!x && x !== "manual" && x !== DIR.provider;
  for (const tm of TEAMS) if (oldSource(tm.source) && tm.active !== false) notes.push(t("mock.directory.noteOldSource", { kind: t("mock.directory.kindTeam"), name: tm.name, provider: sourceTitle(tm.source) }));
  for (const m of Object.values(MEMBERS)) if (oldSource(m.source) && m.active) notes.push(t("mock.directory.noteOldSource", { kind: t("mock.directory.kindMember"), name: m.name, provider: sourceTitle(m.source) }));
  return { teams, members, teamsToDeactivate, membersNew, membersExisting, membersToConfirm, membersToDeactivate, mergeTargets, notes, confirmations, decided, skipped };
}
on("GET", "/org/directory/providers", () => { requireOrgAdmin(); return mockProviders(); });
on("GET", "/org/directory", () => { requireOrgAdmin(); return viewDirectory(); });
on("PUT", "/org/directory", (_m, body) => {
  requireOrgAdmin();
  const b = (body ?? {}) as Partial<DirectoryInput>;
  const key = b.provider?.trim() || DIR.provider;
  if (!key) throw new ApiError(400, t("mock.directory.needProvider"));
  const p = providerOf(key);
  if (!p) throw new ApiError(400, t("mock.directory.unknownProvider", { key }));
  const changed = key !== DIR.provider;
  const creds = b.credentials ?? {};
  // 校验按声明逐字段来：非保密必填；保密字段第一次（或换了提供方）必填，之后省略 / 空串 = 沿用
  for (const f of p.fields) {
    const v = (creds[f.key] ?? "").trim();
    if (!f.secret && !v) throw new ApiError(400, t("mock.directory.needField", { title: f.title }));
    if (f.secret && !v && (changed || !DIR.secrets[f.key])) throw new ApiError(400, t("mock.directory.needSecretField", { title: f.title }));
  }
  if (changed) { DIR.credentials = {}; DIR.secrets = {}; EXT_TEAMS = {}; EXT_MEMBERS = {}; DIR_DECISIONS = DIR_DECISIONS.filter((d) => d.provider !== DIR.provider); }
  DIR.provider = key;
  for (const f of p.fields) {
    const v = (creds[f.key] ?? "").trim();
    if (!f.secret) DIR.credentials[f.key] = v;
    else if (v) DIR.secrets[f.key] = v;
  }
  if (b.root_department_id !== undefined || changed) DIR.root_department_id = (b.root_department_id ?? "").trim() || p.root_department_id;
  if (b.root_department_ids !== undefined || changed) DIR.root_department_ids = (b.root_department_ids ?? []).map((x) => x.trim()).filter(Boolean);
  if (b.default_role !== undefined) DIR.default_role = b.default_role;
  if (b.schedule !== undefined) DIR.schedule = b.schedule;
  if (b.proxy_url !== undefined) DIR.proxy_url = b.proxy_url.trim();
  emit("DirectoryConfigured", { actor: ME, summary: t("mock.ev.directoryConfigured", { name: p.title }) });
  return viewDirectory();
});
on("POST", "/org/directory/test", (): DirectoryTest => {
  requireOrgAdmin();
  const p = providerOf(DIR.provider);
  if (!p || !dirConfigured()) throw new ApiError(400, t("mock.directory.notConfigured"));
  // 示例数据里：保密字段以 bad 开头就当作被提供方拒绝
  if (p.fields.some((f) => f.secret && DIR.secrets[f.key]?.startsWith("bad"))) return { ok: false, error: t("mock.directory.testBad", { name: p.title, fields: p.fields.map((f) => f.title).join(getLocale() === "zh-CN" ? " 或 " : " or ") }) };
  // 测试连接 = 检查清单的一句话摘要（阻塞项的标题 + 怎么做；范围待选时附可选的根）
  const cl = directoryChecklist();
  const blocked = cl.checks.find((c) => c.status === "blocked");
  if (blocked) return { ok: false, error: `${blocked.title}：${blocked.fix ?? ""}`, suggested_roots: cl.suggested_roots };
  return { ok: true, tenant_name: ORG.name, department_name: TEAMS.find((x) => x.id === EXT_TEAMS[DIR.root_department_id])?.name ?? ORG.name, suggested_roots: cl.suggested_roots };
});
on("GET", "/org/directory/checklist", () => { requireOrgAdmin(); return directoryChecklist(); });
on("GET", "/org/directory/preview", (): DirectoryPreview => {
  requireOrgAdmin();
  if (!dirConfigured()) throw new ApiError(400, t("mock.directory.notConfigured"));
  requireReady();
  const p = directoryPlan();
  return {
    teams: p.teams, teams_to_deactivate: p.teamsToDeactivate,
    members_total: p.members.length, members_new: p.membersNew.length, members_existing: p.membersExisting.length, members_to_deactivate: p.membersToDeactivate,
    notes: p.notes, confirmations: p.confirmations, decided: p.decided, skipped: p.skipped, blocked_by_confirmations: p.confirmations.length,
  };
});
on("POST", "/org/directory/sync", (): DirectoryRun => {
  requireOrgAdmin();
  if (!dirConfigured()) throw new ApiError(400, t("mock.directory.notConfigured"));
  requireReady();
  const provider = DIR.provider!;
  const p = directoryPlan();
  // 预览里还有未决定的候选：不同步（ADR 0017 补记四）
  if (p.confirmations.length) throw new ApiError(400, t("mock.directory.confirmFirst", { n: p.confirmations.length }), "err.directory_confirm_first");
  const started = nowISO();
  let addedTeams = 0, updatedTeams = 0;
  for (const tp of p.teams) {
    const parentLocal = tp.parent_external_id ? EXT_TEAMS[tp.parent_external_id] ?? null : null;
    if (tp.action === "create") {
      const tm: OrgTeam = { id: nextId("team-"), name: tp.name, parent_id: parentLocal, lead_id: null, member_ids: [], source: provider, external_name: tp.name, active: true };
      TEAMS.push(tm); EXT_TEAMS[tp.external_id] = tm.id; EXT_SINCE[`team:${tp.external_id}`] = started; addedTeams++;
    } else if (tp.action === "update" || tp.bind) {
      const tm = getTeam(tp.local_id!);
      // 合并决定：补外部身份，名称 / 上级按 IM，来源改为提供方
      if (tp.bind) { EXT_TEAMS[tp.external_id] = tm.id; EXT_SINCE[`team:${tp.external_id}`] = started; tm.source = provider; }
      if (tp.action === "update") { tm.name = tp.name; tm.parent_id = parentLocal; tm.active = true; updatedTeams++; }
      tm.external_name = tp.name;
    }
  }
  for (const x of p.teamsToDeactivate) getTeam(x.id).active = false;
  let addedMembers = 0, updatedMembers = 0;
  const invitations: NonNullable<DirectoryRun["invitations"]> = [];
  for (const r of p.members) {
    const teamId = EXT_TEAMS[r.team] ?? null;
    let localId = EXT_MEMBERS[r.ext];
    // 合并决定：绑上外部身份，来源改为提供方，停用的恢复；姓名 / 团队按 IM，角色不动
    if (!localId && p.mergeTargets[r.ext] && MEMBERS[p.mergeTargets[r.ext]]) {
      localId = p.mergeTargets[r.ext];
      EXT_MEMBERS[r.ext] = localId; EXT_SINCE[`member:${r.ext}`] = started;
      const m = MEMBERS[localId]; m.source = provider; if (!m.active) { m.active = true; m.status = "active"; }
      updatedMembers++;
    }
    if (!localId) {
      const id = nextId("mem-");
      const token = `dir_${id}`;
      // 没邮箱的人先给一个占位邮箱：<外部编号>@<提供方>.invalid
      MEMBERS[id] = { id, name: r.name, email: r.email ?? `${r.ext}@${provider}.invalid`, roles: DIR.default_role ? [DIR.default_role] : [], team_id: teamId, locale: ORG.default_locale, active: true, created_at: started, source: provider, status: "pending_activation", invite_token: token };
      EXT_MEMBERS[r.ext] = id; EXT_SINCE[`member:${r.ext}`] = started;
      if (teamId) getTeam(teamId).member_ids.push(id);
      invitations.push({ member_id: id, name: r.name, url: inviteUrl(token) });
      addedMembers++;
    } else {
      const m = MEMBERS[localId];
      if (m.name !== r.name || m.team_id !== teamId) {
        m.name = r.name;
        if (m.team_id !== teamId) { for (const tm of TEAMS) tm.member_ids = tm.member_ids.filter((x) => x !== m.id); if (teamId) getTeam(teamId).member_ids.push(m.id); m.team_id = teamId; }
        updatedMembers++;
      }
    }
  }
  for (const x of p.membersToDeactivate) { MEMBERS[x.id].active = false; MEMBERS[x.id].status = "inactive"; }
  const run: DirectoryRun = {
    id: nextId("dr"), provider, started_at: started, finished_at: nowISO(), status: p.notes.length ? "partial" : "ok", status_title: "",
    added_teams: addedTeams, updated_teams: updatedTeams, deactivated_teams: p.teamsToDeactivate.length,
    added_members: addedMembers, updated_members: updatedMembers, deactivated_members: p.membersToDeactivate.length, errors: p.notes,
  };
  DIR_RUNS.push(run);
  emit("DirectorySyncRan", { actor: ME, summary: t("mock.ev.directorySyncRan", { a: addedMembers, u: updatedMembers, d: p.membersToDeactivate.length }) });
  return { ...viewDirRun(run), invitations };
});
on("GET", "/org/directory/runs", (_m, _b, q) => { requireOrgAdmin(); const limit = Number(str(q, "limit") ?? 20) || 20; return [...DIR_RUNS].reverse().slice(0, limit).map(viewDirRun); });
// ---- 冲突与对应关系（ADR 0017 补记四） ----
on("PUT", "/org/directory/decisions", (_m, body) => {
  requireOrgAdmin();
  if (!DIR.provider) throw new ApiError(400, t("mock.directory.notConfigured"));
  const items = ((body ?? {}) as { items?: DirectoryDecisionInput[] }).items ?? [];
  const out: DecisionRow[] = [];
  for (const it of items) {
    if (it.kind !== "member" && it.kind !== "team") throw new ApiError(400, t("mock.directory.badDecision"));
    if (it.decision !== "merge" && it.decision !== "create" && it.decision !== "skip") throw new ApiError(400, t("mock.directory.badDecision"));
    if (it.decision === "merge" && !it.local_id) throw new ApiError(400, t("mock.directory.needLocal"));
    if (it.decision === "merge" && it.kind === "team") { const tm = getTeam(it.local_id!); if (tm.active === false) throw new ApiError(400, t("mock.merge.teamInactive", { name: tm.name })); }
    if (it.decision === "merge" && it.kind === "member" && !MEMBERS[it.local_id!]) throw new ApiError(404, t("mock.org.memberNotFound"));
    const name = it.external_name ?? (it.kind === "team" ? REMOTE_TEAMS.find((r) => r.ext === it.external_id)?.name : REMOTE_MEMBERS.find((r) => r.ext === it.external_id)?.name) ?? it.external_id;
    const row: DecisionRow = { provider: DIR.provider, kind: it.kind, external_id: it.external_id, external_name: name, decision: it.decision, local_id: it.decision === "merge" ? it.local_id : undefined, decided_at: nowISO() };
    DIR_DECISIONS = DIR_DECISIONS.filter((d) => !(d.provider === row.provider && d.kind === row.kind && d.external_id === row.external_id));
    DIR_DECISIONS.push(row); out.push(row);
    emit("DirectoryDecided", { actor: ME, summary: t("mock.ev.directoryDecided", { provider: providerOf(DIR.provider)?.title ?? "", name, decision: decisionTitle(row.decision) }), data: { kind: row.kind, external_id: row.external_id, decision: row.decision } });
  }
  return out.map(viewDecision);
});
on("DELETE", "/org/directory/decisions/:kind/:ext", (m) => {
  requireOrgAdmin();
  const { kind, ext } = m.groups!;
  const d = DIR_DECISIONS.find((x) => x.provider === DIR.provider && x.kind === kind && x.external_id === decodeURIComponent(ext));
  if (!d) throw new ApiError(404, t("mock.directory.noDecision"));
  DIR_DECISIONS = DIR_DECISIONS.filter((x) => x !== d);
  emit("DirectoryDecided", { actor: ME, summary: t("mock.ev.directoryReconsider", { name: d.external_name }), data: { kind: d.kind, external_id: d.external_id, decision: "reconsider" } });
  return undefined;
});
on("GET", "/org/directory/mappings", (): DirectoryMappings => {
  requireOrgAdmin();
  const p = providerOf(DIR.provider);
  if (!p) return { provider: null, bound: [], duplicates: findDuplicates(), skipped: [] };
  const bound: DirectoryBinding[] = [
    ...Object.entries(EXT_TEAMS).map(([ext, id]): DirectoryBinding | null => { const tm = TEAMS.find((x) => x.id === id); return tm ? { kind: "team", external_id: ext, external_name: REMOTE_TEAMS.find((r) => r.ext === ext)?.name ?? tm.external_name ?? tm.name, local_id: id, local_name: tm.name, local_active: tm.active !== false, since: sinceOf("team", ext) } : null; }).filter((x): x is DirectoryBinding => !!x),
    ...Object.entries(EXT_MEMBERS).map(([ext, id]): DirectoryBinding | null => { const mem = MEMBERS[id]; return mem ? { kind: "member", external_id: ext, external_name: REMOTE_MEMBERS.find((r) => r.ext === ext)?.name ?? mem.name, local_id: id, local_name: mem.name, local_active: mem.active, since: sinceOf("member", ext) } : null; }).filter((x): x is DirectoryBinding => !!x),
  ];
  const skipped: DirectorySkipped[] = DIR_DECISIONS.filter((d) => d.provider === DIR.provider && d.decision === "skip").map((d) => ({ kind: d.kind, external_id: d.external_id, external_name: d.external_name, decided_at: d.decided_at }));
  return { provider: p.key, provider_title: p.title, bound, duplicates: findDuplicates(), skipped };
});
on("DELETE", "/org/directory/bindings/:kind/:ext", (m) => {
  requireOrgAdmin();
  const kind = m.groups!.kind as DirectoryKind;
  const ext = decodeURIComponent(m.groups!.ext);
  const map = kind === "team" ? EXT_TEAMS : EXT_MEMBERS;
  const id = map[ext];
  if (!id) throw new ApiError(404, t("mock.directory.noBinding"));
  delete map[ext];
  DIR_DECISIONS = DIR_DECISIONS.filter((d) => !(d.kind === kind && d.external_id === ext));
  // 本地对象改为手工维护
  let name = ext;
  if (kind === "team") { const tm = TEAMS.find((x) => x.id === id); if (tm) { tm.source = "manual"; tm.external_name = null; name = tm.name; } }
  else if (MEMBERS[id]) { MEMBERS[id].source = "manual"; name = MEMBERS[id].name; }
  emit("DirectoryUnbound", { actor: ME, summary: t("mock.ev.directoryUnbound", { name }), data: { kind, external_id: ext, local_id: id } });
  return undefined;
});
/** 把团队 from 并入 into：成员、下级团队、迭代、目标、共享边界、负责人、外部身份整体过去；旧团队停用不删除、改为手工来源 */
on("POST", "/org/teams/:id/merge", (m, body) => {
  requireOrgAdmin();
  const from = getTeam(m.groups!.id);
  const into = getTeam(String((body as { into?: string } | undefined)?.into ?? ""));
  if (from.id === into.id) throw new ApiError(400, t("mock.merge.self"));
  if (into.active === false) throw new ApiError(400, t("mock.merge.teamInactive", { name: into.name }));
  if (from.active === false) throw new ApiError(400, t("mock.merge.teamEmpty", { name: from.name }));
  // 目标在旧团队子树里时先提到旧团队的位置，不成环
  if (teamSubtree(from.id).includes(into.id)) into.parent_id = from.parent_id;
  for (const id of from.member_ids) { if (!into.member_ids.includes(id)) into.member_ids.push(id); if (MEMBERS[id]?.team_id === from.id) MEMBERS[id].team_id = into.id; }
  from.member_ids = [];
  for (const tm of TEAMS) if (tm.parent_id === from.id && tm.id !== into.id) tm.parent_id = into.id;
  for (const sp of Object.values(sprints)) if (sp.team_id === from.id) sp.team_id = into.id;
  for (const gr of Object.values(goals)) if (gr.team_id === from.id) gr.team_id = into.id;
  if (from.is_boundary) into.is_boundary = true;
  if (!into.lead_id && from.lead_id) into.lead_id = from.lead_id;
  for (const [ext, id] of Object.entries(EXT_TEAMS)) if (id === from.id) EXT_TEAMS[ext] = into.id;
  for (const d of DIR_DECISIONS) if (d.kind === "team" && d.local_id === from.id) d.local_id = into.id;
  if (isSyncedRow(from) && !isSyncedRow(into)) { into.source = from.source; into.external_name = from.external_name; }
  from.active = false; from.source = "manual"; from.external_name = null; from.lead_id = null;
  emit("TeamMerged", { actor: ME, summary: t("mock.ev.teamMerged", { a: from.name, b: into.name }), data: { from: from.id, into: into.id } });
  return viewOrgTeam(into);
});
/** 把成员 from 并入 into：任务 / 目标 / Agent / 团队负责人 / 团队归属改到目标，历史不改；外部身份过去；旧成员停用 */
on("POST", "/org/members/:id/merge", (m, body) => {
  requireOrgAdmin();
  const from = MEMBERS[m.groups!.id];
  const into = MEMBERS[String((body as { into?: string } | undefined)?.into ?? "")];
  if (!from || !into) throw new ApiError(404, t("mock.org.memberNotFound"));
  if (from.id === into.id) throw new ApiError(400, t("mock.merge.self"));
  if (ORG.owner_id === from.id) throw new ApiError(400, t("mock.merge.owner"));
  if (!into.active) throw new ApiError(400, t("mock.merge.memberInactive", { name: into.name }));
  for (const tk of Object.values(tasks)) { if (tk.assignee_id === from.id) tk.assignee_id = into.id; if (tk.reviewer_id === from.id) tk.reviewer_id = into.id; }
  for (const gr of Object.values(goals)) if (gr.owner_id === from.id) gr.owner_id = into.id;
  for (const a of Object.values(AGENTS)) if (a.owner.id === from.id) a.owner = ref(into.id);
  for (const tm of TEAMS) {
    if (tm.lead_id === from.id) tm.lead_id = into.id;
    if (tm.member_ids.includes(from.id)) { tm.member_ids = tm.member_ids.filter((x) => x !== from.id); if (!tm.member_ids.includes(into.id)) tm.member_ids.push(into.id); }
  }
  if (!into.team_id && from.team_id) into.team_id = from.team_id;
  for (const n of notifications) if (n.member_id === from.id && !n.read_at) n.member_id = into.id;
  for (const [ext, id] of Object.entries(EXT_MEMBERS)) if (id === from.id) EXT_MEMBERS[ext] = into.id;
  for (const d of DIR_DECISIONS) if (d.kind === "member" && d.local_id === from.id) d.local_id = into.id;
  // 旧成员来自 IM 而目标是手工的 → 目标接上 IM 身份并按 IM 改名
  if (isSyncedRow(from) && !isSyncedRow(into)) { into.source = from.source; into.name = from.name; }
  from.active = false; from.status = "inactive"; from.team_id = null;
  emit("MemberMerged", { actor: ME, summary: t("mock.ev.memberMerged", { a: from.name, b: into.name }), data: { from: from.id, into: into.id } });
  return viewOrgMember(into, findDuplicates());
});

const applyTeamMembers = (tm: OrgTeam) => { for (const id of tm.member_ids) if (MEMBERS[id]) MEMBERS[id].team_id = tm.id; };
const countable = (id: ID) => { const m = MEMBERS[id]; return !!m && m.active; };
const viewOrgTeam = (tm: OrgTeam): OrgTeam => {
  const sub = teamSubtree(tm.id);
  const all = new Set<ID>();
  for (const x of TEAMS) if (sub.includes(x.id)) for (const id of x.member_ids) if (countable(id)) all.add(id);
  return { ...tm, source: tm.source ?? "manual", source_title: sourceTitle(tm.source), member_count: tm.member_ids.filter(countable).length, subtree_member_count: all.size };
};
on("GET", "/org/teams", () => { requireOrgAdmin(); return TEAMS.map(viewOrgTeam); });
on("POST", "/org/teams", (_m, body) => {
  requireOrgAdmin();
  const b = (body ?? {}) as { name?: string; parent_id?: ID | null; lead_id?: ID | null; member_ids?: ID[]; is_boundary?: boolean };
  if (!b.name?.trim()) throw new ApiError(400, t("mock.org.needName"));
  const tm: OrgTeam = { id: nextId("team-"), name: b.name.trim(), parent_id: b.parent_id ?? null, lead_id: b.lead_id ?? null, member_ids: b.member_ids ?? [], is_boundary: !!b.is_boundary };
  if (tm.parent_id) getTeam(tm.parent_id);
  TEAMS.push(tm);
  applyTeamMembers(tm);
  return viewOrgTeam(tm);
});
on("PATCH", "/org/teams/:id", (m, body) => {
  requireOrgAdmin();
  const tm = getTeam(m.groups!.id);
  const b = (body ?? {}) as { name?: string; parent_id?: ID | null; lead_id?: ID | null; member_ids?: ID[]; is_boundary?: boolean; active?: boolean };
  // 拒绝都用整句说明（和后端一致）：同步团队不能改名；不能挪到自己或下级下面；有人或有下级的团队不能停用
  if (b.name !== undefined && b.name.trim() !== tm.name && isSyncedRow(tm)) throw new ApiError(409, t("mock.people.syncedTeamName", { name: sourceTitle(tm.source) }));
  if (b.name !== undefined && !b.name.trim()) throw new ApiError(400, t("mock.org.needName"));
  if (b.parent_id !== undefined && b.parent_id !== null) {
    if (teamSubtree(tm.id).includes(b.parent_id)) throw new ApiError(409, t("mock.people.moveIntoSelf"));
    getTeam(b.parent_id);
  }
  if (b.active === false && tm.active !== false && (tm.member_ids.length > 0 || TEAMS.some((x) => x.parent_id === tm.id && x.active !== false))) throw new ApiError(409, t("mock.people.teamNotEmpty"));
  if (b.name !== undefined) tm.name = b.name.trim();
  if (b.parent_id !== undefined) tm.parent_id = b.parent_id;
  if (b.lead_id !== undefined) tm.lead_id = b.lead_id;
  if (b.is_boundary !== undefined) tm.is_boundary = !!b.is_boundary;
  if (b.active !== undefined) tm.active = b.active;
  if (b.member_ids !== undefined) {
    for (const id of tm.member_ids) if (MEMBERS[id] && !b.member_ids.includes(id) && MEMBERS[id].team_id === tm.id) MEMBERS[id].team_id = null;
    tm.member_ids = b.member_ids;
    applyTeamMembers(tm);
  }
  return viewOrgTeam(tm);
});
on("DELETE", "/org/teams/:id", (m) => {
  requireOrgAdmin();
  const tm = getTeam(m.groups!.id);
  // 只能删空的手工团队（有人或有下级要先挪走；同步团队只能停用）
  if (isSyncedRow(tm)) throw new ApiError(409, t("mock.people.syncedTeamDelete", { name: sourceTitle(tm.source) }));
  if (tm.member_ids.length > 0 || TEAMS.some((x) => x.parent_id === tm.id)) throw new ApiError(409, t("mock.people.teamNotEmpty"));
  TEAMS.splice(TEAMS.indexOf(tm), 1);
  return undefined;
});

// ---------- 成员与团队页：批量操作、CSV 导入导出（DESIGN.md §16） ----------
on("POST", "/org/members/bulk", (_m, body) => {
  requireOrgAdmin();
  const b = (body ?? {}) as { member_ids?: ID[]; action?: string; team_id?: ID | null; roles?: string[] };
  const ids = [...new Set(b.member_ids ?? [])];
  if (!ids.length) throw new ApiError(400, t("mock.people.needMembers"));
  const skipped: Array<{ id: ID; reason: string }> = [];
  let updated = 0;
  const target = b.team_id ? getTeam(b.team_id) : null;
  for (const id of ids) {
    const mem = MEMBERS[id];
    if (!mem) { skipped.push({ id, reason: t("mock.org.memberNotFound") }); continue; }
    switch (b.action) {
      case "move_team":
        if (isSyncedRow(mem)) { skipped.push({ id, reason: t("mock.people.syncedMemberTeam", { name: sourceTitle(mem.source) }) }); continue; }
        moveMember(mem, target?.id ?? null); break;
      case "add_team":
        if (!target) throw new ApiError(400, t("mock.org.teamNotFound"));
        if (target.member_ids.includes(id)) { skipped.push({ id, reason: t("mock.people.alreadyInTeam", { team: target.name }) }); continue; }
        target.member_ids.push(id);
        if (!mem.team_id) mem.team_id = target.id;
        break;
      case "set_roles": mem.roles = (b.roles ?? []).filter((r) => roleOf(r)); break;
      case "deactivate":
        if (ORG.owner_id === id) { skipped.push({ id, reason: t("mock.people.ownerKeep") }); continue; }
        if (!mem.active) { skipped.push({ id, reason: t("mock.people.alreadyInactive") }); continue; }
        mem.active = false; break;
      case "reactivate":
        if (mem.active) { skipped.push({ id, reason: t("mock.people.alreadyActive") }); continue; }
        mem.active = true; break;
      default: throw new ApiError(400, t("mock.people.badAction"));
    }
    updated++;
  }
  return { updated, skipped };
});

const teamPath = (id: ID | null): string => { const parts: string[] = []; let cur = TEAMS.find((x) => x.id === id); while (cur) { parts.unshift(cur.name); cur = TEAMS.find((x) => x.id === cur!.parent_id); } return parts.join(" / "); };
const teamByPath = (path: string): OrgTeam | null => {
  const parts = path.split("/").map((x) => x.trim()).filter(Boolean);
  if (!parts.length) return null;
  // 先按完整路径找，找不到再按最后一段的名字找（导入表里常常只写团队名）
  let parent: ID | null = null; let cur: OrgTeam | undefined;
  for (const name of parts) { cur = TEAMS.find((x) => x.name === name && (x.parent_id ?? null) === parent); if (!cur) break; parent = cur.id; }
  if (cur && parts.length) return cur;
  const last = parts[parts.length - 1];
  return TEAMS.find((x) => x.name === last) ?? null;
};
const csvCell = (v: string) => (/[",\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v);
const parseCsv = (text: string): string[][] => {
  const rows: string[][] = []; let row: string[] = []; let cell = ""; let q = false;
  const src = text.replace(/^\uFEFF/, "");
  for (let i = 0; i < src.length; i++) {
    const c = src[i];
    if (q) { if (c === '"') { if (src[i + 1] === '"') { cell += '"'; i++; } else q = false; } else cell += c; continue; }
    if (c === '"') q = true;
    else if (c === ",") { row.push(cell); cell = ""; }
    else if (c === "\n" || c === "\r") { if (c === "\r" && src[i + 1] === "\n") i++; row.push(cell); rows.push(row); row = []; cell = ""; }
    else cell += c;
  }
  if (cell !== "" || row.length) { row.push(cell); rows.push(row); }
  return rows.filter((r) => r.some((x) => x.trim() !== ""));
};
// 导出六列（和真实后端一致）：姓名、邮箱、团队（路径用「 / 」）、角色（用「、」分开）、状态、来源；导入只看前四列
const csvHeader = () => [t("mock.people.csv.name"), t("mock.people.csv.email"), t("mock.people.csv.team"), t("mock.people.csv.roles"), t("mock.people.csv.status"), t("mock.people.csv.source")];
on("GET", "/org/members/export.csv", () => {
  requireOrgAdmin();
  const lines = [csvHeader().join(",")];
  for (const m of Object.values(MEMBERS)) lines.push([m.name, m.email, teamPath(m.team_id), m.roles.map((r) => roleOf(r)?.title ?? r).join("、"), memberStatusTitle(memberStatus(m)), sourceTitle(m.source)].map(csvCell).join(","));
  return lines.join("\n") + "\n";
});
const emailOk = (s: string) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s);
/** 导入请求体：整个请求体是 CSV 文本，或 JSON `{csv, decisions}`（对 confirm 行的决定） */
const importInput = (body: unknown): { csv: string; decisions: MemberImportDecision[] } => {
  if (typeof body === "string") return { csv: body, decisions: [] };
  const b = (body ?? {}) as { csv?: string; decisions?: MemberImportDecision[] };
  return { csv: b.csv ?? "", decisions: b.decisions ?? [] };
};
function importPreview(text: string, decisions: MemberImportDecision[] = []): MemberImportPreview {
  const rows = parseCsv(text);
  const out: MemberImportRow[] = [];
  const seen = new Map<string, number>();
  rows.forEach((cells, idx) => {
    const line = idx + 1;
    const [name = "", email = "", team_path = "", rolesRaw = ""] = cells.map((x) => x.trim());
    // 第一行如果不含邮箱就是表头
    if (idx === 0 && !email.includes("@")) return;
    const roles = rolesRaw.split(/[;|、；,，/]/).map((x) => x.trim()).filter(Boolean);
    const base = { line, name, email, team_path, roles: [] as string[], team_id: null as ID | null };
    const invalid = (reason: string): MemberImportRow => ({ ...base, action: "invalid", reason });
    if (!email) { out.push(invalid(t("mock.people.csv.needEmail"))); return; }
    if (!emailOk(email)) { out.push(invalid(t("mock.people.csv.badEmail"))); return; }
    const dup = seen.get(email.toLowerCase());
    if (dup) { out.push(invalid(t("mock.people.csv.duplicate", { line: dup }))); return; }
    seen.set(email.toLowerCase(), line);
    const existing = Object.values(MEMBERS).find((m) => m.email.toLowerCase() === email.toLowerCase());
    if (!existing && !name) { out.push(invalid(t("mock.people.csv.needName"))); return; }
    const badRole = roles.find((r) => !ROLES.some((x) => x.name === r || x.title === r || EN[x.title] === r));
    if (badRole) { out.push(invalid(t("mock.people.csv.roleMissing", { name: badRole }))); return; }
    const roleNames = roles.map((r) => ROLES.find((x) => x.name === r || x.title === r || EN[x.title] === r)!.name);
    let team: OrgTeam | null = null;
    if (team_path) { team = teamByPath(team_path); if (!team) { out.push(invalid(t("mock.people.csv.teamMissing", { name: team_path }))); return; } }
    let reason: string | undefined;
    if (existing && isSyncedRow(existing) && team && team.id !== existing.team_id) reason = t("mock.people.csv.syncedTeamKept", { name: sourceTitle(existing.source) });
    // 新邮箱、但姓名与某个手工成员相同且行里的团队与他的主团队同名（同步同一套认法的第四级，ADR 0017 补记四）→ 要人决定
    if (!existing && team) {
      const cands = Object.values(MEMBERS).filter((m) => !isSyncedRow(m) && m.name === name && m.team_id === team!.id).map((m) => candidateOf("member", m.id, "name_team", t("mock.directory.reason.name_team", { team: L(team!.name) })));
      if (cands.length) {
        const d = decisions.find((x) => x.line === line);
        if (d?.decision === "merge") {
          if (!d.local_id || !cands.some((c) => c.local_id === d.local_id)) { out.push(invalid(t("mock.people.csv.mergeNotCandidate", { line }))); return; }
          out.push({ ...base, roles: roleNames, team_id: team.id, action: "update", merge_into: d.local_id, candidates: cands } as MemberImportRow); return;
        }
        if (d?.decision === "skip") { out.push({ ...base, roles: roleNames, team_id: team.id, action: "skip", reason: t("mock.people.csv.skippedByDecision"), candidates: cands }); return; }
        if (d?.decision !== "create") { out.push({ ...base, roles: roleNames, team_id: team.id, action: "confirm", candidates: cands }); return; }
      }
    }
    out.push({ ...base, roles: roleNames, team_id: team?.id ?? null, action: existing ? "update" : "create", reason });
  });
  const n = (a: MemberImportRow["action"]) => out.filter((r) => r.action === a).length;
  return { rows: out, summary: { create: n("create"), update: n("update"), invalid: n("invalid"), confirm: n("confirm"), skip: n("skip") } };
}
on("POST", "/org/members/import/preview", (_m, body) => { requireOrgAdmin(); const { csv, decisions } = importInput(body); return importPreview(csv, decisions); });
on("POST", "/org/members/import", (_m, body) => {
  requireOrgAdmin();
  const { csv, decisions } = importInput(body);
  const preview = importPreview(csv, decisions);
  const result: MemberImportResult = { created: 0, updated: 0, skipped: [], invitations: [] };
  for (const r of preview.rows) {
    if (r.action === "invalid" || r.action === "skip") { result.skipped.push({ line: r.line, email: r.email, reason: r.reason ?? "" }); continue; }
    // 没决定的 confirm 行这次不导入
    if (r.action === "confirm") { result.skipped.push({ line: r.line, email: r.email, reason: t("mock.people.csv.undecided") }); continue; }
    const mergeInto = (r as MemberImportRow & { merge_into?: ID }).merge_into;
    const existing = mergeInto ? MEMBERS[mergeInto] : Object.values(MEMBERS).find((m) => m.email.toLowerCase() === r.email.toLowerCase());
    if (existing) {
      if (r.name && !isSyncedRow(existing)) existing.name = r.name;
      if (r.roles.length) existing.roles = r.roles;
      if (r.team_path && !isSyncedRow(existing)) moveMember(existing, r.team_id);
      result.updated++;
      continue;
    }
    const token = `imp_${Math.random().toString(36).slice(2, 10)}`;
    const mem: MemberRow = { id: nextId("m"), name: r.name, email: r.email, roles: r.roles, team_id: null, locale: getLocale(), active: true, created_at: nowISO(), source: "manual", status: "pending_activation", invite_token: token };
    MEMBERS[mem.id] = mem;
    ADMIN_ORGS[0].member_count += 1;
    if (r.team_id) moveMember(mem, r.team_id);
    INVITATIONS.push({ id: nextId("I"), token, email: r.email, name: r.name, roles: r.roles, url: "", expires_at: at(7), accepted_at: null, created_at: nowISO() });
    result.created++;
    result.invitations.push({ email: r.email, name: r.name, url: inviteUrl(token) });
  }
  return result;
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
  } else if (mem.status === "pending_activation") {
    mem.status = "active";
    delete mem.invite_token;
    if (b.name?.trim() && !isSyncedRow(mem)) mem.name = b.name.trim();
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
