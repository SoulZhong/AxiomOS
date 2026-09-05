// AxiomOS 接口客户端与数据类型。
// 字段名是代码标识符（英文），界面文案一律走 src/lib/terms.ts 里的中文（见 CONTEXT.md、ADR 0007）。
// 所有请求：<NEXT_PUBLIC_API_BASE>/api/v1/...，Cookie 会话（credentials: include）。
// NEXT_PUBLIC_MOCK=1 时改走 src/lib/mock.ts 的内存数据，不访问后端。

import { getLocale, t, type Locale } from "./i18n";
import { getScope } from "./scope";

export const API_BASE = process.env.NEXT_PUBLIC_API_BASE ?? "http://localhost:8080";
export const MOCK = process.env.NEXT_PUBLIC_MOCK === "1";
export type { Locale };

// ---------- 基础 ----------

export type ID = string;
export type ISODate = string; // "2026-09-03"
export type ISODateTime = string; // "2026-09-03T08:00:00Z"

/** 状态类型：流程里每个状态必须归入的五类之一。 */
export type StateLabel = "pending" | "active" | "waiting" | "terminal_success" | "terminal_failure";

/** 任务当前所处的状态（名字 + 界面名 + 状态类型）。 */
export interface TaskState {
  name: string;
  title: string;
  label: StateLabel;
  weight?: number;
  claimable?: boolean;
  /** 在制品上限：这个状态同时容纳的任务数上限，只提醒不拦截（ADR 0012） */
  wip_limit?: number | null;
}

/** 执行者：人或 Agent，任务不关心是哪一种。 */
export type ExecutorKind = "member" | "agent";
export interface ExecutorRef {
  id: ID;
  kind: ExecutorKind;
  name: string;
  /** kind = agent 时：所有者 */
  owner_id?: ID;
}

export interface ApiErrorBody {
  error?: { code?: string; message?: string };
  message?: string;
}

export class ApiError extends Error {
  status: number;
  code?: string;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

// ---------- 组织与人 ----------

export interface Organization {
  id: ID;
  name: string;
  owner_id: ID;
  /** 成本显示用的货币代码，默认 CNY */
  currency: string;
  /** 组织默认语言；成员没有自己设置时用它 */
  default_locale: Locale;
  slug?: string;
  created_at: ISODateTime;
}

export interface Role {
  name: string;
  title: string;
}

export interface Team {
  id: ID;
  name: string;
  lead_id: ID | null;
  parent_id: ID | null;
  /** 共享边界（ADR 0013）：这个团队连同下面所有团队构成一个共享域 */
  is_boundary?: boolean;
}

export interface Member {
  id: ID;
  name: string;
  email: string;
  roles: string[]; // 角色代码名
  team_id: ID | null;
  /** 成员的界面语言 */
  locale: Locale;
  created_at: ISODateTime;
}

/** 当前登录会话 */
export interface Session {
  member: Member;
  organization: Organization;
  roles: Role[]; // 组织内全部角色（代码名 -> 界面名）
  teams: Team[];
  /** 组织交付物类型表：代码名 -> 界面名 */
  artifact_types: Record<string, string>;
  /** 能力标签词表（代码名） */
  capabilities: string[];
  /** 能力标签界面名：代码名 -> 当前语言名 */
  capability_titles?: Record<string, string>;
  /** 当前成员的组织级权限（所有角色权限的并集）：org_settings / manage_workflows / cancel_any_task / view_all_cost */
  permissions?: string[];
  /** 是否组织负责人 */
  is_owner?: boolean;
  /** 可切的范围（ADR 0013）：全公司 + 能选的团队，按层级顺序 */
  scopes?: ScopeOption[];
  /** 进来默认选中的那一档：团队 id 或 "all" */
  default_scope?: string;
  /** 组织的可见范围策略；界面用它决定"看不到成本"时说哪一句理由 */
  settings?: OrgSettings;
}

// ---------- 范围与可见范围策略（ADR 0013） ----------

/** 一档范围：全公司（id = "all"）或某个团队（含其全部下级团队）。 */
export interface ScopeOption {
  id: ID | "all";
  title: string;
  /** 层级深度，0 是顶层；选择器按它缩进 */
  depth: number;
  /** 这一档能不能看财务数据（成本、预算、用量）。false 时界面把金额显示成「—」 */
  financial: boolean;
}

/**
 * 可见性策略（ADR 0013）：三选一。
 * org = 全员可见；boundary = 按共享边界（团队上的 is_boundary 标记）；team_tree = 只看自己团队及下级。
 */
export type VisibilityPolicy = "org" | "boundary" | "team_tree";
export const VISIBILITY_POLICIES: VisibilityPolicy[] = ["org", "boundary", "team_tree"];

/** 组织的两项可见性策略：每个组织自己配，系统只给默认值（协作 org / 财务 team_tree）。 */
export interface OrgSettings {
  collaboration_visibility: VisibilityPolicy;
  finance_visibility: VisibilityPolicy;
}

/**
 * 「看看某某能看到什么」的预览（GET /org/visibility-preview?member=，形状以后端实际返回为准）。
 * teams 是全组织的团队树（带层级），每个团队标出这个成员能否看它的协作数据（work）与财务数据（finance）。
 */
export interface VisibilityPreview {
  member_id: ID;
  member_name: string;
  /** 他所在的团队 */
  team_ids: ID[];
  collaboration_visibility: VisibilityPolicy;
  finance_visibility: VisibilityPolicy;
  /** 不受协作 / 财务可见性限制（组织负责人，或持有对应权限） */
  sees_all_work: boolean;
  sees_all_cost: boolean;
  /** 跨域是因为角色上的权限 */
  cross_boundary_by_role: boolean;
  /** 与他相关的共享边界（团队 id） */
  boundaries: ID[];
  teams: Array<{ id: ID; name: string; depth: number; is_boundary: boolean; mine: boolean; work: boolean; finance: boolean }>;
  visible_team_ids: ID[];
  finance_visible_team_ids: ID[];
}

// ---------- 组织设置（/org/...，二期） ----------

export interface OrgInfo {
  id: ID;
  slug: string;
  name: string;
  currency: string;
  default_locale: Locale;
  owner: ExecutorRef;
}

export interface OrgMember extends Member {
  active: boolean;
  is_owner: boolean;
}

export interface Invitation {
  id: ID;
  email: string;
  name: string;
  roles: string[];
  url: string;
  expires_at: ISODateTime;
  accepted_at: ISODateTime | null;
  created_at: ISODateTime;
}

export interface OrgRole {
  name: string;
  title: string;
  /** 各语言的名称，编辑时用 */
  titles?: Partial<Record<Locale, string>>;
  builtin: boolean;
  permissions: string[]; // org_settings / manage_workflows / cancel_any_task / view_all_cost
}

/** 多语言标题：字符串视为当前语言；对象按语言分别给。 */
export type LocalizedTitle = string | Partial<Record<Locale, string>>;

export interface OrgTeam {
  id: ID;
  name: string;
  parent_id: ID | null;
  lead_id: ID | null;
  member_ids: ID[];
  /** 共享边界（ADR 0013）：打开后这个团队和它下面的所有团队构成一个共享域 */
  is_boundary?: boolean;
}

export interface Capability {
  name: string;
  title: string;
  titles?: Partial<Record<Locale, string>>;
}

export interface PriceModel {
  model_id: string;
  input_per_million: number;
  output_per_million: number;
  cache_read_per_million: number;
  cache_write_per_million: number;
  currency: string;
}
export interface OrgPriceModel extends PriceModel {
  source: "global" | "override";
}
export interface ExchangeRate {
  from: string;
  to: string;
  rate: number;
}
export interface OrgPricing {
  currency: string;
  models: OrgPriceModel[];
  exchange_rates: ExchangeRate[];
}
export type PriceInput = Omit<PriceModel, "model_id">;

// ---------- 邀请（公开） ----------

export interface InvitationInfo {
  organization_name: string;
  email: string;
  name: string;
  expired: boolean;
  accepted: boolean;
}

// ---------- 平台后台（/admin/...） ----------

export interface AdminUser {
  id: ID;
  email: string;
  name: string;
}

export interface AdminOrganization {
  id: ID;
  slug: string;
  name: string;
  currency: string;
  default_locale: Locale;
  owner: { id: ID; name: string; email: string } | null;
  member_count: number;
  agent_count: number;
  task_count: number;
  cost_30d: number;
  deactivated_at: ISODateTime | null;
  created_at: ISODateTime;
}

export interface AdminOrganizationInput {
  slug: string;
  name: string;
  currency?: string;
  default_locale?: Locale;
  owner_email: string;
  owner_name: string;
}

/** 新建组织的返回：多一个负责人邀请链接（新账号要通过它设密码）。 */
export interface AdminOrganizationCreated extends AdminOrganization {
  owner_invite_url: string;
}

export interface AdminStats {
  organizations: number;
  members: number;
  agents: number;
  runs_30d: number;
  cost_30d: Array<{ org_id: ID; name: string; cost: number; currency: string }>;
}

// ---------- Agent ----------

export type GrantName =
  | "execute"
  | "claim_backlog"
  | "review"
  | "comment"
  | "create_subtask"
  | "create_task"
  | "create_goal"
  | "assign"
  | "link"
  | "manage_workflows";
export type GrantMode = "direct" | "with_approval";

export interface Grant {
  name: GrantName;
  mode: GrantMode;
}

export interface Agent {
  id: ID;
  name: string;
  owner: ExecutorRef; // kind = member
  shared: boolean; // 公共 Agent
  capabilities: string[];
  grants: Grant[];
  online: boolean;
  last_seen_at: ISODateTime | null;
  max_concurrency: number;
  created_at: ISODateTime;
}

export interface AgentInput {
  name: string;
  capabilities: string[];
  grants: Grant[];
  shared?: boolean;
  max_concurrency?: number;
}

/** 注册 Agent 的返回：token 只返回这一次。 */
export interface AgentRegistration {
  agent: Agent;
  token: string;
}

// ---------- 待确认操作（ADR 0003：授权是「需要人确认」时，Agent 的动作先记成这一条，等人点确认才执行） ----------

export type ProposalStatus = "pending" | "approved" | "rejected" | "expired";
export type ProposalTargetKind = "task" | "goal" | "sprint" | "task_type" | "agent";

/** 动作作用在哪个东西上；有些动作（如创建目标）没有目标，为 null。 */
export interface ProposalTarget {
  kind: ProposalTargetKind;
  id: ID;
  title: string;
}

/** 发起的 Agent、所有者、确认人：只保证有 id 与名字。 */
export interface ProposalActor {
  id: ID;
  name: string;
  kind?: ExecutorKind;
}

export interface Proposal {
  id: ID;
  agent: ProposalActor;
  owner: ProposalActor;
  /** 动作代码名，如 task.transition；界面只显示 action_title */
  action: string;
  action_title: string;
  /** 触发这次确认的授权（是它被设成「需要人确认」）；老数据可能没有 */
  grant?: GrantName | null;
  target: ProposalTarget | null;
  /** 完整中文句子：确认后会发生什么 */
  summary: string;
  /** 再执行这个动作所需的全部输入 */
  payload: Record<string, unknown>;
  status: ProposalStatus;
  /** 后端按语言渲染的状态名；界面用自己的字典，这里只作兜底 */
  status_title?: string;
  /** 当前登录成员能不能确认 / 拒绝这一条（后端算好的：所有者，或持有这个动作要的权限） */
  can_decide?: boolean;
  decided_by: ProposalActor | null;
  decided_at: ISODateTime | null;
  /** 拒绝理由（完整句子），Agent 会读它 */
  reason: string | null;
  created_at: ISODateTime;
  expires_at: ISODateTime | null;
}

export interface ProposalQuery {
  status?: ProposalStatus;
  agent?: ID;
  /** 1 = 只看等我确认的 */
  mine?: 1;
}

/** 确认并立即执行：result 是这次执行的返回，形状随动作而变，界面不解析它。 */
export interface ProposalApproval {
  proposal: Proposal;
  result?: unknown;
}

export interface ProposalCount {
  pending: number;
}

// ---------- 目标 ----------

export interface Goal {
  id: ID;
  title: string;
  description: string;
  owner: ExecutorRef; // 目标负责人（kind = member）
  parent_id: ID | null;
  team_id: ID | null;
  progress: number; // 0–100，由下面的目标和任务汇总
  achieved: boolean; // 是否达成，由负责人确认
  status: "draft" | "active" | "achieved" | "abandoned";
  budget: number | null; // 预算（成本上限，超了只提醒）
  cost: number; // 已产生成本
  planned_start: ISODate | null;
  planned_end: ISODate | null;
  actual_start: ISODate | null;
  actual_end: ISODate | null;
  /** 截止日：横条外的一道短刻度（可与 planned_end 不同） */
  deadline?: ISODate | null;
  task_count: number;
  done_task_count: number;
  children: Goal[]; // GET /goals 返回树时填充
  created_at: ISODateTime;
  /** 里程碑（ADR 0016），按日期升序；老后端没有这个字段 */
  milestones?: Milestone[];
  milestone_summary?: MilestoneSummary;
}

// ---------- 里程碑（ADR 0016）：目标的时间刻度，不是任务 ----------

export type MilestoneStatus = "upcoming" | "reached" | "overdue";

export interface Milestone {
  id: ID;
  goal_id: ID;
  title: string;
  description: string;
  due_on: ISODate;
  reached_at: ISODateTime | null;
  /** 由日期与 reached_at 算出：未到 / 已达到 / 逾期 */
  status: MilestoneStatus;
  /** 该日期前计划结束的任务都已完成，可以确认了（已达到的恒为假） */
  ready_hint: boolean;
  created_by: ExecutorRef;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

export interface MilestoneSummary {
  total: number;
  reached: number;
  overdue: number;
  next: { title: string; due_on: ISODate } | null;
}

export interface MilestoneInput {
  title: string;
  due_on: ISODate;
  description?: string;
}

/** 甘特图目标行上的精简里程碑（/gantt 按目标分组时带） */
export interface MilestoneMark {
  id: ID;
  title: string;
  due_on: ISODate;
  status: MilestoneStatus;
  ready_hint?: boolean;
}

export interface GoalInput {
  title: string;
  description?: string;
  owner_id?: ID;
  parent_id?: ID | null;
  budget?: number | null;
  planned_start?: ISODate | null;
  planned_end?: ISODate | null;
  deadline?: ISODate | null;
  achieved?: boolean;
  /** "abandoned" 放弃，"active" 重新开始 */
  status?: "active" | "abandoned";
}

// ---------- 任务 ----------

export type RelationType = "blocks" | "found_in" | "related";

/** 关联：blocks = 前置（from 没完成 to 不能领）；found_in = 发现于（from 是 Bug，to 是被测任务）；related = 相关。 */
export interface Relation {
  id: ID;
  type: RelationType;
  from: TaskRef;
  to: TaskRef;
  created_at: ISODateTime;
}

export interface TaskRef {
  id: ID;
  title: string;
  type: string;
  state: TaskState;
}

export interface Artifact {
  id: ID;
  type: string; // 组织交付物类型代码名
  title: string;
  url: string;
  attached_by: ExecutorRef;
  created_at: ISODateTime;
}

/** 评论（人看的对话）与工作日志（Agent 的过程记录）共用一个线程。 */
export interface Comment {
  id: ID;
  kind: "comment" | "note";
  author: ExecutorRef;
  body: string;
  created_at: ISODateTime;
}

/** 用量：按模型分条，不含金额。 */
export interface Usage {
  model: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  duration_ms: number;
  tool_calls: number;
}

export type RunOutcome = "running" | "ended" | "cancelled";

/** 执行记录 */
export interface Run {
  id: ID;
  task_id: ID;
  state: string; // 所在的进行中状态名
  state_title: string;
  executor: ExecutorRef;
  started_at: ISODateTime;
  ended_at: ISODateTime | null;
  outcome: RunOutcome;
  usage: Usage[];
  total_tokens: number;
  cost: number;
}

export type Priority = "low" | "normal" | "high" | "urgent";

export interface Participant {
  title: string;
  role: string;
  executor: ExecutorRef | null;
}

export interface Task {
  id: ID;
  goal_id: ID | null;
  goal: { id: ID; title: string } | null;
  parent_id: ID | null;
  type: string;
  type_title: string;
  type_version: number;
  title: string;
  description: string;
  state: TaskState;
  previous_state: string | null;
  creator: ExecutorRef;
  assignee: ExecutorRef | null; // 任务负责人
  reviewer: ExecutorRef; // 验收人
  participants: Record<string, Participant>; // 参与角色位置
  required_role: string | null; // 进入待领取任务时要求的角色
  pending_participant: string | null; // 待回填的参与角色位置
  required_capabilities: string[];
  relations: Relation[];
  artifacts: Artifact[];
  comments: Comment[];
  runs: Run[];
  planned_start: ISODate | null;
  planned_end: ISODate | null;
  actual_start: ISODate | null;
  actual_end: ISODate | null;
  estimate: number | null; // 预计工时（小时）
  /** 工作量：相对大小的整数点数（1、2、3、5、8、13），可空；燃尽图与迭代速度按它算 */
  points: number | null;
  /** 所属迭代（任务通过 sprint_id 归属） */
  sprint: SprintRef | null;
  priority: Priority;
  progress: number;
  total_tokens: number;
  cost: number;
  fields: Record<string, unknown>; // task_schema 定义的自定义字段
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

export interface TaskInput {
  title: string;
  type: string;
  goal_id?: ID | null;
  parent_id?: ID | null;
  description?: string;
  assignee_id?: ID | null;
  participants?: Record<string, ID>; // 位置 -> 执行者
  reviewer_id?: ID;
  required_capabilities?: string[];
  planned_start?: ISODate | null;
  planned_end?: ISODate | null;
  estimate?: number | null;
  /** 工作量（整数点数），null 清除 */
  points?: number | null;
  /** 所属迭代；空字符串表示移出迭代 */
  sprint_id?: ID | "" | null;
  priority?: Priority;
  fields?: Record<string, unknown>;
}

export interface TaskQuery {
  goal?: ID;
  /** 执行者 id，或 "me" 表示当前登录成员（含他的 Agent） */
  assignee?: ID | "me";
  creator?: ID | "me";
  reviewer?: ID | "me";
  /** 状态名，或状态类型（pending/active/...） */
  state?: string;
  type?: string;
  /** 迭代 id */
  sprint?: ID;
  limit?: number;
}

// ---------- 看板与迭代（ADR 0012：看板是流程状态的视图，迭代是时间盒容器） ----------

export type SprintStatus = "planning" | "active" | "closed";

export interface SprintRef {
  id: ID;
  name: string;
}

export interface Sprint {
  id: ID;
  team: { id: ID; title: string } | null;
  name: string;
  goal: string;
  starts_on: ISODate;
  ends_on: ISODate;
  status: SprintStatus;
  task_count: number;
  points_total: number;
  points_done: number;
  created_by: ID;
  started_at: ISODateTime | null;
  closed_at: ISODateTime | null;
  created_at?: ISODateTime;
}

export interface SprintInput {
  name: string;
  goal?: string;
  team_id?: ID | null;
  starts_on: ISODate;
  ends_on: ISODate;
}

export interface BurndownPoint {
  date: ISODate;
  value: number;
}

/** 燃尽：每天剩余工作量（有工作量时）或任务数，配一条从起点到终点的理想线。由动态回放得出。 */
export interface Burndown {
  unit: "points" | "tasks";
  ideal: BurndownPoint[];
  actual: BurndownPoint[];
}

/** 任务精简对象：看板卡片与迭代待办用，没有关联 / 评论 / 执行记录明细。 */
export type TaskSummary = Pick<Task, "id" | "goal_id" | "goal" | "type" | "type_title" | "title" | "state" | "assignee" | "planned_start" | "planned_end" | "priority" | "progress" | "cost" | "points" | "sprint" | "estimate">;

export interface SprintDetail extends Sprint {
  /** 后端返回的是完整任务对象（关联 / 交付物 / 评论 / 执行记录为空数组），见 docs/api.md */
  tasks: Task[];
  burndown: Burndown;
  velocity: { sprints: number; average: number };
}

export interface SprintCloseInput {
  /** 未完成的任务：退回待办，或转入指定的下一个迭代 */
  unfinished: "backlog" | "next";
  next_sprint_id?: ID;
}

export interface SprintCloseResult {
  sprint: Sprint;
  moved: number;
  returned: number;
}

/** 迭代速度：最近 5 个已结束迭代各自完成的工作量与平均值。 */
export interface VelocityData {
  sprints: Array<{ id: ID; name: string; points_done: number; tasks_done: number }>;
  average_points: number;
}

export type BoardLane = "none" | "goal" | "assignee";

export interface BoardQuery {
  /** 任务类型：给了则列 = 该类型流程的状态；不给则列 = 五种状态类型 */
  type?: string;
  goal?: ID;
  team?: ID;
  assignee?: ID | "me";
  sprint?: ID;
  lane?: BoardLane;
}

export interface BoardCard extends TaskSummary {
  /** 当前登录者能把这张卡拖到哪些列（状态名；全类型看板时是状态类型名），由内核计算 */
  can_move_to: string[];
}

export interface BoardColumn {
  /** 列对应的状态；全类型看板时 name 是状态类型名 */
  state: TaskState;
  wip_limit?: number | null;
  count: number;
  over_limit: boolean;
  cards: BoardCard[];
}

export interface BoardData {
  type: { name: string; title: string } | null;
  columns: BoardColumn[];
  lanes?: Array<{ key: string; title: string }>;
}

// ---------- 流程可用性（spec §10 get_workflow） ----------

export interface TransitionAvailability {
  name: string;
  title: string;
  to: string;
  available: boolean;
  reasons: string[]; // 不可用的原因，面向人的中文
  requires: string[]; // 触发时要附带的东西（comment / result）
  label_to: StateLabel; // 目标状态类型，用于按钮的语气
}

export interface WorkflowAvailability {
  task_id: ID;
  state: TaskState;
  active_run: { id: ID; executor_id: ID } | null;
  transitions: TransitionAvailability[];
  can_begin: boolean;
  can_claim: boolean;
  begin_reasons: string[];
  claim_reasons: string[];
}

export interface TransitionBody {
  comment?: string;
  result?: Record<string, unknown>;
}

// ---------- 任务类型与流程（spec §1–3） ----------

export interface ParticipantDef {
  title: string;
  role: string;
}

export interface TransitionDef {
  name: string;
  title: string;
  from: string[]; // ["*"] 表示任意非终止状态
  to: string; // 或 "$previous"
  by: string[]; // creator / assignee / reviewer / participant:<位置> / role:<角色> / anyone
  requires: string[]; // deps_done / artifact:<类型> / comment / no_open_bugs / result
  grant?: GrantName; // 默认 execute
  assign_to?: string | null; // participant:<位置> / creator / null；缺省不变
}

export interface WorkflowDef {
  version: number;
  initial: string;
  states: Record<string, TaskState>;
  transitions: TransitionDef[];
}

export interface TaskType {
  name: string;
  title: string;
  builtin: boolean;
  participants: Record<string, ParticipantDef>;
  task_schema: Record<string, unknown> | null;
  result_schema: Record<string, unknown> | null;
  agent_instructions: string;
  workflow: WorkflowDef;
}

// ---------- 待领取任务 ----------

export interface BacklogItem {
  task: Task;
  required_role: string | null;
  required_capabilities: string[];
  can_claim: boolean; // 当前登录者能否领取
  reasons: string[]; // 不能领取的原因
}

// ---------- 甘特图 ----------

export type GanttGroup = "goal" | "team" | "executor" | "type";

export interface GanttTask {
  id: ID;
  title: string;
  type: string;
  type_title: string;
  state: TaskState;
  assignee: ExecutorRef | null;
  goal_id: ID | null;
  planned_start: ISODate | null;
  planned_end: ISODate | null;
  actual_start: ISODate | null;
  actual_end: ISODate | null;
  progress: number;
  cost: number;
}

export interface GanttRow {
  key: string; // 分组键：目标 id / 团队 id / 执行者 id / 任务类型名；未分类为 "_"
  title: string;
  /** 按目标分组时带目标信息（截止线画在 planned_end） */
  goal: {
    id: ID;
    planned_start: ISODate | null;
    planned_end: ISODate | null;
    deadline?: ISODate | null;
    progress: number;
    /** 里程碑（ADR 0016）：目标汇总行上的菱形 */
    milestones?: MilestoneMark[];
  } | null;
  tasks: GanttTask[];
}

export interface GanttDependency {
  from_task_id: ID; // 前置
  to_task_id: ID; // 后续
}

export interface GanttData {
  group: GanttGroup;
  from: ISODate;
  to: ISODate;
  rows: GanttRow[];
  dependencies: GanttDependency[];
}

// ---------- 动态 ----------

export type EventKind =
  | "TaskCreated"
  | "TaskAssigned"
  | "TaskClaimed"
  | "TaskSentToBacklog"
  | "TaskTransitioned"
  | "RunStarted"
  | "RunEnded"
  | "UsageReported"
  | "ArtifactAttached"
  | "CommentAdded"
  | "NoteAdded"
  | "TaskUpdated"
  | "AgentUpdated"
  | "TaskTypeSaved"
  | "TasksLinked"
  | "RelationRemoved"
  | "GoalCreated"
  | "GoalUpdated"
  | "GoalDeleted"
  | "MilestoneCreated"
  | "MilestoneUpdated"
  | "MilestoneReached"
  | "MilestoneUnreached"
  | "MilestoneDeleted"
  | "AgentRegistered"
  | "AgentRemoved"
  | "SprintCreated"
  | "SprintStarted"
  | "SprintClosed"
  | "TaskAddedToSprint"
  | "TaskRemovedFromSprint"
  | "PointsChanged"
  | "ProposalCreated"
  | "ProposalApproved"
  | "ProposalRejected"
  | "OrgSettingsUpdated"
  | "TeamCreated"
  | "TeamUpdated"
  | "TeamDeleted"
  | "WorkspaceLayoutUpdated";

export interface Event {
  id: ID;
  kind: EventKind;
  task_id: ID | null;
  task_title: string | null;
  goal_id: ID | null;
  actor: ExecutorRef | null; // 系统自动产生时为 null
  summary: string; // 一句中文
  data: Record<string, unknown>;
  created_at: ISODateTime;
}

// ---------- 统计 ----------

export type CostGroup = "goal" | "team" | "executor" | "model";

export interface CostStat {
  key: string;
  title: string;
  cost: number;
  total_tokens: number;
  run_count: number;
}

export interface CycleStat {
  type: string;
  type_title: string;
  sample: number; // 样本数
  avg_hours: number; // 创建到完成的平均小时
  median_hours: number;
  avg_active_hours: number; // 执行记录累计（真正在做的时间）
}

export interface ThroughputStat {
  week: ISODate; // 周一
  created: number;
  done: number;
  terminated: number;
}

export interface AgentStat {
  agent: ExecutorRef;
  owner: ExecutorRef;
  runs: number;
  accepted: number; // 验收通过
  rejected: number; // 被打回
  success_rate: number; // 0–1
  reject_rate: number; // 0–1
  total_tokens: number;
  cost: number;
}

// ---------- 组织概览（docs/api.md「范围与概览」） ----------

export type OverviewPeriod = "week" | "month" | "quarter";
export const OVERVIEW_PERIODS: OverviewPeriod[] = ["week", "month", "quarter"];

export interface DateRange {
  from: ISODate;
  to: ISODate;
}

/** 一个组织单元这一段时间的运行情况。金额在看不到财务数据的范围里为 null。 */
export interface OverviewUnit {
  id: ID;
  title: string;
  /** team = 团队（可以继续下钻）；unassigned = 没有团队的那一档 */
  kind: "team" | "unassigned";
  /** 目标进度 0–100 */
  goal_progress: number;
  tasks_total: number;
  tasks_done: number;
  overdue: number;
  cost: number | null;
  budget: number | null;
  /** 预算执行率 0–100（预算为空时 null） */
  budget_used_pct: number | null;
  /** 吞吐：这一段时间完成的任务数 */
  throughput: number;
  /** 上一段同样长度的时间，用来算环比 */
  prev: { cost: number | null; throughput: number };
}

export interface TrendPoint {
  /** 分桶的起点（日或周） */
  bucket: ISODate;
  cost: number | null;
  done: number;
  created: number;
}

export interface OverviewData {
  scope: string;
  period: OverviewPeriod;
  range: DateRange;
  prev_range: DateRange;
  units: OverviewUnit[];
  totals: Omit<OverviewUnit, "id" | "title" | "kind"> & { id?: ID; title?: string };
  trend: TrendPoint[];
  /**
   * 上一段时间的同样分桶，用来画对比线。docs/api.md 的契约里没有这一项，
   * 后端没给时界面退回画一条「上期平均」的横线。
   */
  prev_trend?: TrendPoint[];
}

// ---------- 要我关注的异常 ----------

/** 异常列表里的任务：只保证有这些字段（够直接打开并处理它）。 */
export interface ExceptionTask {
  id: ID;
  title: string;
  state: TaskState;
  /** 负责人：契约里是 ExecutorRef，后端目前给的是名字字符串，两种都认 */
  assignee: ExecutorRef | string | null;
  assignee_id?: ID | null;
  goal?: { id: ID; title: string } | null;
  goal_id?: ID | null;
  goal_title?: string | null;
  team?: string | null;
  planned_end?: ISODate | null;
  /** 逾期天数（后端算好）；后端字段名是 days_overdue，这里两个都认 */
  overdue_days?: number;
  days_overdue?: number;
  url?: string;
}

export interface ExceptionGoal {
  id: ID;
  title: string;
  owner?: ExecutorRef | null;
  progress?: number;
  achieved?: boolean;
  planned_end?: ISODate | null;
  deadline?: ISODate | null;
  overdue_days?: number;
  budget?: number | null;
  cost?: number | null;
  /** 全部任务都完成了，只差负责人确认达成 */
  all_tasks_done?: boolean;
}

export interface StuckTask {
  task: ExceptionTask;
  /** 在同一个状态里停了多少天 */
  days_in_state: number;
}

export interface ExceptionsData {
  overdue_tasks: ExceptionTask[];
  overdue_goals: ExceptionGoal[];
  over_budget_goals: ExceptionGoal[];
  stuck_tasks: StuckTask[];
  pending_proposals: Proposal[];
}

// ---------- 人员与 Agent 负荷 ----------

export interface LoadRow {
  executor: ExecutorRef;
  /** 所属团队；后端可能给对象也可能给名字 */
  team?: { id: ID; title: string } | string | null;
  /** 名下未结束的任务数 */
  open_tasks: number;
  /** 其中进行中的 */
  active_tasks: number;
  /** 未结束任务的工作量点数合计 */
  points_open: number;
  /** 本周计划工时 */
  planned_hours_this_week: number;
  overdue: number;
  /** 后端给的一句负荷提示（可选） */
  capacity_hint?: string | null;
  /** Agent：最多同时任务数 */
  max_concurrent?: number | null;
  /** Agent：是否在线 */
  online?: boolean;
}

// ---------- 工作台（ADR 0015，docs/api.md「工作台」） ----------

/** 区块键：系统定义的有限集合，客户不能自造。 */
export type BlockKey = "overview_summary" | "exceptions" | "my_tasks" | "my_review" | "team_load" | "cost_budget" | "events" | "sprint" | "backlog" | "trend";
/** `proposals` 区块已从目录移除（DESIGN.md §12：被「待我处理」覆盖）；老布局里出现它时前端直接忽略。 */
export const BLOCK_KEYS: BlockKey[] = ["overview_summary", "exceptions", "my_tasks", "my_review", "team_load", "cost_budget", "events", "sprint", "backlog", "trend"];
export const isBlockKey = (k: string): k is BlockKey => (BLOCK_KEYS as string[]).includes(k);

/** 已解析的工作台里的一块：title 由后端按语言给。 */
export interface WorkspaceBlock {
  key: BlockKey;
  title: string;
}

/** 布局从哪来：个人微调 / 各角色布局并集 / 系统默认。 */
export type WorkspaceSource = "personal" | "roles" | "default";

/** GET /workspace：我的工作台（已解析）。 */
export interface Workspace {
  blocks: WorkspaceBlock[];
  source: WorkspaceSource;
  /** source = roles 时用到的角色代码名 */
  roles_used: string[];
  can_customize: boolean;
}

export interface BlockDef {
  key: BlockKey;
  title: string;
  description: string;
}

/** 预设：系统发的一套区块组合，按"这类人关心什么"命名，不写死角色名。 */
export interface Preset {
  key: string;
  title: string;
  description: string;
  blocks: BlockKey[];
}

/** GET /workspace/blocks：全部区块与预设。 */
export interface WorkspaceCatalog {
  blocks: BlockDef[];
  presets: Preset[];
}

/** GET /org/workspace 的一行：一个角色的布局。preset 为 null 表示逐块改过（自定义）。 */
export interface RoleWorkspace {
  role: string;
  role_title: string;
  blocks: BlockKey[];
  preset: string | null;
  member_count: number;
}

export type RoleWorkspaceInput = { blocks: BlockKey[] } | { preset: string };

// ---------- 待我处理（DESIGN.md §12，docs/api.md「待我处理」） ----------

/** 六组事项，顺序就是紧急度：逾期 → 待确认 → 待验收 → 待答复 → 未开始 → 通知。 */
export type InboxKind = "overdue" | "proposals" | "review" | "questions" | "unstarted" | "notifications";
export const INBOX_KINDS: InboxKind[] = ["overdue", "proposals", "review", "questions", "unstarted", "notifications"];

/** 任务类组里的一条：任务精简对象加逾期天数（只有 overdue 组非零）。 */
export type InboxTask = Task & { days_overdue: number };

/** 等我答复的一条提问：任务加那条评论。 */
export interface InboxQuestion {
  task: Task;
  comment: Comment;
}

/** 站内通知：后端按语言给标题与正文；task_id 有值时能点去任务。id 是数字，标已读时按 id 传。 */
export interface Notification {
  id: number;
  member_id?: ID;
  title: string;
  body: string;
  task_id?: ID | null;
  read_at?: ISODateTime | null;
  created_at: ISODateTime;
}

/** 一组事项：title 由后端按语言给；items 的形状按 kind 不同。 */
export type InboxGroup =
  | { kind: "overdue" | "review" | "unstarted"; title?: string; count: number; items: InboxTask[] }
  | { kind: "proposals"; title?: string; count: number; items: Proposal[] }
  | { kind: "questions"; title?: string; count: number; items: InboxQuestion[] }
  | { kind: "notifications"; title?: string; count: number; items: Notification[] };

/** GET /inbox：空组省略；全空时 empty 是那句「没有等你处理的事。」 */
export interface Inbox {
  count: number;
  groups: InboxGroup[];
  empty?: string;
}

/** GET /inbox/count：侧栏角标与状态栏读数用的那一个 n。 */
export interface InboxCount {
  count: number;
  by_kind: Partial<Record<InboxKind, number>>;
}

// ---------- 请求 ----------

type Query = Record<string, string | number | undefined | null>;

/**
 * 接受 scope= 的列表与统计接口（docs/api.md「范围与概览」）。
 * 详情接口（/tasks/{id} 之类）与待确认操作不带：待确认操作是个人队列，不按范围筛。
 */
const SCOPED_PATHS = new Set([
  "/goals",
  "/tasks",
  "/tasks/mine",
  "/backlog",
  "/board",
  "/gantt",
  "/sprints",
  "/sprints/velocity",
  "/events",
  "/stats/cost",
  "/stats/cycle",
  "/stats/throughput",
  "/stats/agents",
  "/stats/overview",
  "/stats/exceptions",
  "/stats/load",
]);

/** 给请求补上当前范围；调用方显式传了 scope 就用它的。 */
function withScope(method: string, path: string, query?: Query): Query | undefined {
  if (method !== "GET" || !SCOPED_PATHS.has(path)) return query;
  const scope = getScope();
  if (query?.scope) return query;
  return { ...(query ?? {}), scope };
}

async function request<T>(method: string, path: string, body?: unknown, rawQuery?: Query): Promise<T> {
  const query = withScope(method, path, rawQuery);
  if (MOCK) {
    const { mockRequest } = await import("./mock");
    return mockRequest(method, path, body, query) as Promise<T>;
  }
  const url = new URL(`${API_BASE}/api/v1${path}`);
  if (query) {
    for (const [k, v] of Object.entries(query)) {
      if (v !== undefined && v !== null && v !== "") url.searchParams.set(k, String(v));
    }
  }
  const headers: Record<string, string> = { "Accept-Language": getLocale() };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  const res = await fetch(url.toString(), {
    method,
    credentials: "include",
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const err = (data ?? {}) as ApiErrorBody;
    throw new ApiError(res.status, err.error?.message ?? err.message ?? t("api.failed", { status: res.status }), err.error?.code);
  }
  return data as T;
}

export const api = {
  auth: {
    login: (email: string, password: string) => request<Session>("POST", "/auth/login", { email, password }),
    logout: () => request<void>("POST", "/auth/logout"),
    me: () => request<Session>("GET", "/auth/me"),
    /** 改自己的设置（目前只有语言）。 */
    updateMe: (patch: { locale: Locale }) => request<Session>("PATCH", "/auth/me", patch),
  },
  /** 成员列表（选择目标负责人、指派任务时用）。 */
  members: {
    list: () => request<Member[]>("GET", "/members"),
  },
  /** 能力标签：代码名 -> 当前语言的界面名。 */
  capabilities: {
    list: () => request<Record<string, string>>("GET", "/capabilities"),
  },
  goals: {
    list: () => request<Goal[]>("GET", "/goals"),
    create: (input: GoalInput) => request<Goal>("POST", "/goals", input),
    get: (id: ID) => request<Goal>("GET", `/goals/${encodeURIComponent(id)}`),
    update: (id: ID, patch: Partial<GoalInput>) => request<Goal>("PATCH", `/goals/${encodeURIComponent(id)}`, patch),
    /** 只有空目标（没有子目标、没有任务）能删；有内容的目标应当放弃 */
    remove: (id: ID) => request<void>("DELETE", `/goals/${encodeURIComponent(id)}`),
  },
  /** 里程碑（ADR 0016）：属于目标；能编辑该目标的人可增改删、确认已达到 / 撤销。每次写操作产生动态。 */
  milestones: {
    list: (goalId: ID) => request<Milestone[]>("GET", `/goals/${encodeURIComponent(goalId)}/milestones`),
    create: (goalId: ID, input: MilestoneInput) => request<Milestone>("POST", `/goals/${encodeURIComponent(goalId)}/milestones`, input),
    update: (id: ID, patch: Partial<MilestoneInput>) => request<Milestone>("PATCH", `/milestones/${encodeURIComponent(id)}`, patch),
    remove: (id: ID) => request<void>("DELETE", `/milestones/${encodeURIComponent(id)}`),
    reach: (id: ID) => request<Milestone>("POST", `/milestones/${encodeURIComponent(id)}/reach`, {}),
    unreach: (id: ID) => request<Milestone>("POST", `/milestones/${encodeURIComponent(id)}/unreach`, {}),
  },
  tasks: {
    list: (q: TaskQuery = {}) => request<Task[]>("GET", "/tasks", undefined, q as Query),
    create: (input: TaskInput) => request<Task>("POST", "/tasks", input),
    get: (id: ID) => request<Task>("GET", `/tasks/${encodeURIComponent(id)}`),
    update: (id: ID, patch: Partial<TaskInput>) => request<Task>("PATCH", `/tasks/${encodeURIComponent(id)}`, patch),
    workflow: (id: ID) => request<WorkflowAvailability>("GET", `/tasks/${encodeURIComponent(id)}/workflow`),
    transition: (id: ID, name: string, body: TransitionBody = {}) =>
      request<Task>("POST", `/tasks/${encodeURIComponent(id)}/transitions/${encodeURIComponent(name)}`, body),
    claim: (id: ID) => request<Task>("POST", `/tasks/${encodeURIComponent(id)}/claim`, {}),
    begin: (id: ID) => request<Task>("POST", `/tasks/${encodeURIComponent(id)}/begin`, {}),
    assign: (id: ID, executor_id: ID | null) => request<Task>("POST", `/tasks/${encodeURIComponent(id)}/assign`, { executor_id }),
    addArtifact: (id: ID, input: { type: string; title: string; url: string }) =>
      request<Artifact>("POST", `/tasks/${encodeURIComponent(id)}/artifacts`, input),
    addComment: (id: ID, input: { body: string; kind?: "comment" | "note" }) =>
      request<Comment>("POST", `/tasks/${encodeURIComponent(id)}/comments`, input),
    /** from/to 之一必须是本任务；blocks: from 是前置；found_in: from 是 Bug。 */
    addRelation: (id: ID, input: { type: RelationType; from_task_id: ID; to_task_id: ID }) =>
      request<Relation>("POST", `/tasks/${encodeURIComponent(id)}/relations`, input),
  },
  backlog: {
    list: () => request<BacklogItem[]>("GET", "/backlog"),
  },
  gantt: {
    get: (q: { group: GanttGroup; from: ISODate; to: ISODate }) => request<GanttData>("GET", "/gantt", undefined, q),
  },
  /** 看板：任务按流程状态分列；拖动 = 调用 tasks.transition。 */
  board: {
    get: (q: BoardQuery = {}) => request<BoardData>("GET", "/board", undefined, q as Query),
  },
  /** 迭代：时间盒容器。开始 / 结束需要 manage_workflows 权限。 */
  sprints: {
    list: (q: { team?: ID; status?: SprintStatus } = {}) => request<Sprint[]>("GET", "/sprints", undefined, q),
    create: (input: SprintInput) => request<Sprint>("POST", "/sprints", input),
    get: (id: ID) => request<SprintDetail>("GET", `/sprints/${encodeURIComponent(id)}`),
    update: (id: ID, patch: Partial<SprintInput>) => request<Sprint>("PATCH", `/sprints/${encodeURIComponent(id)}`, patch),
    start: (id: ID) => request<Sprint>("POST", `/sprints/${encodeURIComponent(id)}/start`, {}),
    close: (id: ID, input: SprintCloseInput) => request<SprintCloseResult>("POST", `/sprints/${encodeURIComponent(id)}/close`, input),
    addTasks: (id: ID, task_ids: ID[]) => request<{ added: number }>("POST", `/sprints/${encodeURIComponent(id)}/tasks`, { task_ids }),
    removeTask: (id: ID, taskId: ID) => request<void>("DELETE", `/sprints/${encodeURIComponent(id)}/tasks/${encodeURIComponent(taskId)}`),
    velocity: (q: { team?: ID } = {}) => request<VelocityData>("GET", "/sprints/velocity", undefined, q),
  },
  agents: {
    list: () => request<Agent[]>("GET", "/agents"),
    create: (input: AgentInput) => request<AgentRegistration>("POST", "/agents", input),
    remove: (id: ID) => request<void>("DELETE", `/agents/${encodeURIComponent(id)}`),
  },
  /** 待确认操作：Agent 发起、等人点确认才执行的动作（ADR 0003）。 */
  proposals: {
    list: (q: ProposalQuery = {}) => request<Proposal[]>("GET", "/proposals", undefined, q as Query),
    get: (id: ID) => request<Proposal>("GET", `/proposals/${encodeURIComponent(id)}`),
    /** 确认并立即执行；执行失败时后端保持 pending 并返回一句完整的失败理由。 */
    approve: (id: ID) => request<ProposalApproval>("POST", `/proposals/${encodeURIComponent(id)}/approve`, {}),
    reject: (id: ID, reason: string) => request<Proposal>("POST", `/proposals/${encodeURIComponent(id)}/reject`, { reason }),
    count: () => request<ProposalCount>("GET", "/proposals/count"),
  },
  taskTypes: {
    list: () => request<TaskType[]>("GET", "/task-types"),
    get: (name: string) => request<TaskType>("GET", `/task-types/${encodeURIComponent(name)}`),
  },
  /** 统计：全部按当前范围裁剪（ADR 0013，scope= 由 request 自动带上）。 */
  stats: {
    cost: (group: CostGroup) => request<CostStat[]>("GET", "/stats/cost", undefined, { group }),
    cycle: () => request<CycleStat[]>("GET", "/stats/cycle"),
    throughput: () => request<ThroughputStat[]>("GET", "/stats/throughput"),
    agents: () => request<AgentStat[]>("GET", "/stats/agents"),
    /** 组织概览：各组织单元对比 + 趋势与环比 */
    overview: (period: OverviewPeriod = "month") => request<OverviewData>("GET", "/stats/overview", undefined, { period }),
    /** 要我关注的异常 */
    exceptions: () => request<ExceptionsData>("GET", "/stats/exceptions"),
    /** 人员与 Agent 负荷 */
    load: () => request<LoadRow[]>("GET", "/stats/load"),
  },
  events: {
    list: (q: { task?: ID; limit?: number } = {}) => request<Event[]>("GET", "/events", undefined, q),
  },
  /** 工作台（ADR 0015）：首页由区块组成，布局按角色配，个人可微调。 */
  workspace: {
    /** 我的工作台（已解析：个人微调 → 角色并集 → 默认） */
    get: () => request<Workspace>("GET", "/workspace"),
    /** 个人微调：只能用系统区块键 */
    saveMine: (blocks: BlockKey[]) => request<Workspace>("PUT", "/workspace/me", { blocks }),
    /** 清除个人微调，回到角色默认 */
    resetMine: () => request<void>("DELETE", "/workspace/me"),
    /** 全部区块与预设（标题按语言）。后端若只返回区块数组，这里补成 { blocks, presets: [] }。 */
    catalog: async (): Promise<WorkspaceCatalog> => {
      const r = await request<WorkspaceCatalog | BlockDef[]>("GET", "/workspace/blocks");
      if (Array.isArray(r)) return { blocks: r, presets: [] };
      return { blocks: r.blocks ?? [], presets: r.presets ?? [] };
    },
    /** 各角色的布局（需要 org_settings） */
    org: () => request<RoleWorkspace[]>("GET", "/org/workspace"),
    /** 给角色配布局：整套预设，或逐块给 */
    saveRole: (role: string, input: RoleWorkspaceInput) => request<RoleWorkspace>("PUT", `/org/workspace/${encodeURIComponent(role)}`, input),
    /** 清除该角色布局，回到默认 */
    resetRole: (role: string) => request<void>("DELETE", `/org/workspace/${encodeURIComponent(role)}`),
  },
  /** 待我处理（DESIGN.md §12）：只看本人，不带范围。 */
  inbox: {
    get: () => request<Inbox>("GET", "/inbox"),
    count: () => request<InboxCount>("GET", "/inbox/count"),
  },
  notifications: {
    /** 把这几条标为已读 → 剩下的未读数 */
    read: (ids: number[]) => request<{ count: number }>("POST", "/notifications/read", { ids }),
  },
  /** 组织设置：组织负责人或持有 org_settings 权限的角色。 */
  org: {
    get: () => request<OrgInfo>("GET", "/org"),
    update: (patch: { name?: string; default_locale?: Locale; currency?: string }) => request<OrgInfo>("PATCH", "/org", patch),
    /** 可见范围策略（ADR 0013）：每个组织自己配 */
    settings: () => request<OrgSettings>("GET", "/org/settings"),
    updateSettings: (patch: Partial<OrgSettings>) => request<OrgSettings>("PATCH", "/org/settings", patch),
    /** 预览某个成员实际能看到哪些团队（配错可见性最容易在这里发现） */
    visibilityPreview: (memberId: ID) => request<VisibilityPreview>("GET", "/org/visibility-preview", undefined, { member: memberId }),
    members: () => request<OrgMember[]>("GET", "/org/members"),
    updateMember: (id: ID, patch: { name?: string; roles?: string[]; active?: boolean; team_id?: ID | null }) =>
      request<OrgMember>("PATCH", `/org/members/${encodeURIComponent(id)}`, patch),
    makeOwner: (id: ID) => request<OrgMember>("POST", `/org/members/${encodeURIComponent(id)}/make-owner`, {}),
    invitations: () => request<Invitation[]>("GET", "/org/invitations"),
    createInvitation: (input: { email: string; name?: string; roles: string[] }) => request<Invitation>("POST", "/org/invitations", input),
    deleteInvitation: (id: ID) => request<void>("DELETE", `/org/invitations/${encodeURIComponent(id)}`),
    roles: () => request<OrgRole[]>("GET", "/org/roles"),
    saveRole: (name: string, input: { title: LocalizedTitle; permissions: string[] }) => request<OrgRole>("PUT", `/org/roles/${encodeURIComponent(name)}`, input),
    deleteRole: (name: string) => request<void>("DELETE", `/org/roles/${encodeURIComponent(name)}`),
    teams: () => request<OrgTeam[]>("GET", "/org/teams"),
    createTeam: (input: { name: string; parent_id?: ID | null; lead_id?: ID | null; member_ids?: ID[]; is_boundary?: boolean }) => request<OrgTeam>("POST", "/org/teams", input),
    updateTeam: (id: ID, patch: { name?: string; parent_id?: ID | null; lead_id?: ID | null; member_ids?: ID[]; is_boundary?: boolean }) =>
      request<OrgTeam>("PATCH", `/org/teams/${encodeURIComponent(id)}`, patch),
    deleteTeam: (id: ID) => request<void>("DELETE", `/org/teams/${encodeURIComponent(id)}`),
    capabilities: () => request<Capability[]>("GET", "/org/capabilities"),
    saveCapability: (name: string, input: { title: LocalizedTitle }) => request<Capability>("PUT", `/org/capabilities/${encodeURIComponent(name)}`, input),
    deleteCapability: (name: string) => request<void>("DELETE", `/org/capabilities/${encodeURIComponent(name)}`),
    pricing: () => request<OrgPricing>("GET", "/org/pricing"),
    savePrice: (modelId: string, input: PriceInput) => request<OrgPriceModel>("PUT", `/org/pricing/models/${encodeURIComponent(modelId)}`, input),
    deletePrice: (modelId: string) => request<void>("DELETE", `/org/pricing/models/${encodeURIComponent(modelId)}`),
    saveRate: (input: ExchangeRate) => request<ExchangeRate>("PUT", "/org/pricing/rates", input),
  },
  /** 邀请接受（公开，不需要登录）。 */
  invitations: {
    get: (token: string) => request<InvitationInfo>("GET", `/invitations/${encodeURIComponent(token)}`),
    accept: (token: string, input: { name: string; password: string }) => request<Session>("POST", `/invitations/${encodeURIComponent(token)}/accept`, input),
  },
  /** 平台后台：独立账号、独立 Cookie（axiomos_admin）。 */
  admin: {
    login: (email: string, password: string) => request<{ admin: AdminUser }>("POST", "/admin/login", { email, password }),
    logout: () => request<void>("POST", "/admin/logout"),
    me: () => request<{ admin: AdminUser }>("GET", "/admin/me"),
    organizations: () => request<AdminOrganization[]>("GET", "/admin/organizations"),
    createOrganization: (input: AdminOrganizationInput) => request<AdminOrganizationCreated>("POST", "/admin/organizations", input),
    getOrganization: (id: ID) => request<AdminOrganization>("GET", `/admin/organizations/${encodeURIComponent(id)}`),
    updateOrganization: (id: ID, patch: { name?: string; deactivated?: boolean }) => request<AdminOrganization>("PATCH", `/admin/organizations/${encodeURIComponent(id)}`, patch),
    pricing: () => request<PriceModel[]>("GET", "/admin/pricing"),
    savePrice: (modelId: string, input: PriceInput) => request<PriceModel>("PUT", `/admin/pricing/${encodeURIComponent(modelId)}`, input),
    deletePrice: (modelId: string) => request<void>("DELETE", `/admin/pricing/${encodeURIComponent(modelId)}`),
    stats: () => request<AdminStats>("GET", "/admin/stats"),
    admins: () => request<AdminUser[]>("GET", "/admin/admins"),
    createAdmin: (input: { email: string; name: string; password: string }) => request<AdminUser>("POST", "/admin/admins", input),
  },
};

export const isTerminal = (label: StateLabel) => label === "terminal_success" || label === "terminal_failure";
