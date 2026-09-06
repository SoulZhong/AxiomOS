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

/** 成员从哪来："manual" 是手工建的，其余值是外部目录提供方的代码名（ADR 0017 补记：提供方是数据，不是分支） */
export type MemberSource = string;
/** 是否由外部目录同步而来（名字由同步决定，界面上只读） */
export const isSynced = (x: { source?: MemberSource | null } | null | undefined): boolean => !!x?.source && x.source !== "manual";
/** 成员状态：正常 / 待激活（同步进来还没登录过）/ 已停用 */
export type MemberStatus = "active" | "pending_activation" | "inactive";

export interface Member {
  id: ID;
  name: string;
  email: string;
  roles: string[]; // 角色代码名
  team_id: ID | null;
  /** 成员的界面语言 */
  locale: Locale;
  created_at: ISODateTime;
  source?: MemberSource;
  /** 来源的界面名（「手工」或提供方名称），后端按语言给 */
  source_title?: string;
  status?: MemberStatus;
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
  /** 状态的界面名（后端按语言给） */
  status_title?: string;
  /** 待激活成员的邀请（id 用于「撤回邀请」；链接只在创建时给一次） */
  invitation?: { id?: ID; url?: string | null; expires_at: ISODateTime } | null;
  /** 同上，扁平字段（成员与团队页的接口约定）；读取用 inviteUrlOf() */
  invitation_url?: string | null;
  /** 所在的全部团队（直属团队 team_id 之外还能被加进别的团队） */
  team_ids?: ID[];
  /** 「可能与 X 重复」（ADR 0017 补记四）：用同步同一套认法在手工成员与同步成员之间找到的疑似重复，两边都带 */
  possible_duplicate_of?: DirectoryDuplicateHint[];
}
export interface DirectoryDuplicateHint { id: ID; name: string; reason: DirectoryMatchReason; reason_text: string }
/** 待激活成员的邀请链接：两种字段形态都认 */
export const inviteUrlOf = (m: OrgMember | null | undefined): string | null => m?.invitation?.url || m?.invitation_url || null;
/** 成员所在的全部团队 id（没有 team_ids 时退回直属团队） */
export const teamIdsOf = (m: OrgMember): ID[] => (m.team_ids && m.team_ids.length ? m.team_ids : m.team_id ? [m.team_id] : []);

/** 成员与团队页的批量操作（POST /org/members/bulk） */
export type BulkMemberAction = "move_team" | "add_team" | "set_roles" | "deactivate" | "reactivate";
export interface BulkMembersResult {
  updated: number;
  skipped: Array<{ id: ID; reason: string }>;
}
/** CSV 导入预览：每行一个动作 */
export interface MemberImportRow {
  line: number;
  name: string;
  email: string;
  team_path: string;
  team_id: ID | null;
  roles: string[];
  /** confirm = 新邮箱、但姓名与某个手工成员相同且团队同名，要人决定（ADR 0017 补记四）；skip = 按决定跳过 */
  action: "create" | "update" | "confirm" | "skip" | "invalid";
  reason?: string;
  /** action 为 confirm 时的疑似对象 */
  candidates?: DirectoryCandidate[];
}
export interface MemberImportPreview {
  rows: MemberImportRow[];
  summary: { create: number; update: number; invalid: number; confirm?: number; skip?: number };
}
/** 导入时对 confirm 行的决定：merge 要给 local_id（必须在候选里） */
export interface MemberImportDecision { line: number; decision: DirectoryDecision; local_id?: ID }
export interface MemberImportResult {
  created: number;
  updated: number;
  skipped: Array<{ line?: number; email?: string; reason: string }>;
  invitations: Array<{ email: string; name?: string; url: string }>;
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
  /** 邀请时选的团队，以及后端据此立刻建出的待激活成员（成员表里直接看得到，不用前端拼） */
  team_id?: ID | null;
  member_id?: ID | null;
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
  /** 来自外部目录的团队名称不可改（ADR 0017） */
  source?: MemberSource;
  /** 来源的界面名（「手工」或提供方名称） */
  source_title?: string;
  external_name?: string | null;
  /** 外部目录里消失的部门只停用不删除 */
  active?: boolean;
  /** 直属人数 / 含下级的总人数（成员与团队页；没有时前端按 member_ids 算） */
  member_count?: number;
  subtree_member_count?: number;
}

// ---------- 外部目录同步（ADR 0017） ----------

/** 提供方代码名：来自 GET /org/directory/providers，前端不枚举 */
export type DirectoryProvider = string;
export type DirectorySchedule = "manual" | "hourly" | "daily";
export type DirectoryRunStatus = "ok" | "partial" | "failed" | "running";

export interface DirectoryRun {
  id?: ID;
  provider?: string;
  started_at: ISODateTime;
  finished_at: ISODateTime | null;
  status: DirectoryRunStatus;
  status_title: string;
  added_teams: number;
  updated_teams: number;
  deactivated_teams: number;
  added_members: number;
  updated_members: number;
  deactivated_members: number;
  errors: string[];
  /** 只在「立即同步」的响应里出现：本次新建成员的邀请链接 */
  invitations?: Array<{ member_id: ID; name: string; url: string }>;
}

/** 提供方声明的一个凭据字段：表单按它渲染，校验按它做 */
export interface DirectoryField {
  key: string;
  title: string;
  /** 保密字段：只上传不回传，界面只知道有没有设过 */
  secret: boolean;
  /** 选填字段：界面上标「选填」，留空不报错 */
  optional?: boolean;
  placeholder: string;
  hint: string;
  /** 只在 GET /org/directory 的 providers[] 里、且是当前提供方时有意义 */
  set?: boolean;
  value?: string;
}

/** 一个可接入的提供方：怎么叫、要填什么、接入前要准备什么 */
export interface DirectoryProviderInfo {
  key: DirectoryProvider;
  title: string;
  /** 提供方的根部门编号（留空同步根部门时后端用它） */
  root_department_id: string;
  fields: DirectoryField[];
  prerequisites: string[];
  /** 到哪儿去找这些凭据：一句提示，带 url 时界面上附「打开{name}控制台」外链 */
  tip?: DirectoryTip;
  /** 过渡期字段：旧版后端给的是分步指引，界面只取第一步当提示 */
}
export interface DirectoryTip { text: string; url?: string }

export interface DirectoryConfig {
  provider: DirectoryProvider | null;
  provider_title: string;
  configured: boolean;
  /** 非保密凭据的值（按字段键） */
  credentials: Record<string, string>;
  /** 保密凭据是否已设置（按字段键）；值从不回传 */
  secrets_set: Record<string, boolean>;
  providers: DirectoryProviderInfo[];
  root_department_id: string;
  /** 多个同步根（见 DirectoryInput.root_department_ids）；旧后端不给 */
  root_department_ids?: string[];
  default_role: string;
  schedule: DirectorySchedule;
  schedule_title: string;
  schedules: Array<{ value: DirectorySchedule; title: string }>;
  proxy_url: string;
  last_run: DirectoryRun | null;
}

export interface DirectoryInput {
  provider: DirectoryProvider;
  /** 按提供方声明的字段键；省略（或空串）某个保密字段表示不改 */
  credentials?: Record<string, string>;
  root_department_id?: string;
  /** 多个同步根（应用只被授权了部分部门时从里面勾选）；每个根成为一个顶层团队。空数组 = 只看 root_department_id */
  root_department_ids?: string[];
  default_role?: string;
  schedule?: DirectorySchedule;
  proxy_url?: string;
}

// ---------- 接入检查清单（ADR 0017 补记二）：提供方自己声明检查项，应用层汇总成向导状态 ----------
export type DirectoryCheckStatus = "ok" | "todo" | "blocked" | "skipped";
export interface DirectoryCheck {
  key: string;
  title: string;
  /** ok 通过；todo 待处理（不拦同步）；blocked 阻塞（同步前必须处理）；skipped 前面的项没过、这项没查 */
  status: DirectoryCheckStatus;
  /** 技术细节（可能出现权限代号、错误码）：只放在「详情」里 */
  detail?: string;
  /** 一句"去哪儿点什么" */
  fix?: string;
  /** 尽量是这个应用在控制台里的那一页 */
  fix_url?: string;
  blocking: boolean;
  /** 只在"权限范围"那一项上、且应用只被授权了部分部门时：可以作为同步根的部门（与 DirectoryChecklist.suggested_roots 相同） */
  roots?: Array<{ id: string; name: string }>;
}
export type DirectoryStep = "credentials" | "checks" | "scope" | "preview" | "sync" | "schedule";
export interface DirectoryChecklist {
  provider: DirectoryProvider;
  provider_title: string;
  /** 提供方控制台首页：检查项没给 fix_url 时的退路 */
  console_url?: string;
  checks: DirectoryCheck[];
  /** 没有阻塞项 */
  ready: boolean;
  next: { step: DirectoryStep; text: string };
  /** 应用只被授权了部分部门时：可以勾选作为同步根的部门 */
  suggested_roots?: Array<{ id: string; name: string }>;
  root_department_id: string;
  root_department_ids?: string[];
}

export interface DirectoryTest {
  ok: boolean;
  tenant_name?: string;
  department_name?: string;
  /** 失败时是一句完整的话 */
  error?: string;
  /** 根部门被拒绝、应用只被授权部分部门时：可直接填成同步根部门的部门 */
  suggested_roots?: Array<{ id: string; name: string }>;
}

export interface DirectoryTeamPlan {
  external_id: string;
  name: string;
  parent_external_id?: string;
  local_id?: string | null;
  /** confirm = 有疑似相同的本地团队，等人决定（ADR 0017 补记四） */
  action: "create" | "update" | "keep" | "confirm";
  /** 这次同步会把外部身份绑到 local_id 上（合并决定） */
  bind?: boolean;
  merge_from?: ID | null;
  candidates?: Array<{ local_id: ID; reason: DirectoryMatchReason; via?: string }>;
}

// ---------- 冲突与对应关系（ADR 0017 补记四） ----------
export type DirectoryKind = "member" | "team";
export type DirectoryDecision = "merge" | "create" | "skip";
/** 认法：邮箱相同 / 手机号相同 / 姓名相同且所在部门与本地主团队同名 / 团队同名同层级 */
export type DirectoryMatchReason = "email" | "mobile" | "name_team" | "name_unique" | "team_name_level";
/** 系统里的疑似对象 */
export interface DirectoryCandidate {
  local_id: ID;
  name: string;
  team_path: string;
  source: MemberSource;
  source_title: string;
  reason: DirectoryMatchReason;
  /** 一句小字：「邮箱相同」「姓名相同且都在研发组下」「团队同名同层级」 */
  reason_text: string;
}
/** IM 里的对象 */
export interface DirectoryExternal { id: string; name: string; dept?: string; parent?: string; email_masked?: string; mobile_tail?: string }
/** 预览里「需要你确认」的一条 */
export interface DirectoryConfirmation { kind: DirectoryKind; external: DirectoryExternal; candidates: DirectoryCandidate[] }
/** 已决定（合并 / 新建）的一条：同形状，多决定 */
export interface DirectoryDecided extends DirectoryConfirmation {
  decision: "merge" | "create";
  decision_title: string;
  decided_local_id?: ID | null;
  decided_local_name?: string | null;
}
export interface DirectorySkipped { kind: DirectoryKind; external_id: string; external_name: string; decided_at: ISODateTime }
export interface DirectoryDecisionInput { kind: DirectoryKind; external_id: string; external_name?: string; decision: DirectoryDecision; local_id?: ID }
export interface DirectoryDecisionRecord { kind: DirectoryKind; external_id: string; external_name: string; decision: DirectoryDecision; decision_title: string; local_id?: ID | null; local_name?: string | null; decided_at: ISODateTime }
/** 「对应关系」面板：已绑定 / 可能重复 / 已跳过 */
export interface DirectoryBinding { kind: DirectoryKind; external_id: string; external_name?: string; local_id: ID; local_name: string; local_active: boolean; since: ISODateTime }
export interface DirectoryDuplicateSide { id: ID; name: string; source: MemberSource; source_title: string; team_path: string }
/** 同步之后在手工对象（a）与同步对象（b）之间找到的疑似重复 */
export interface DirectoryDuplicate { kind: DirectoryKind; a: DirectoryDuplicateSide; b: DirectoryDuplicateSide; reason: DirectoryMatchReason; reason_text: string }
export interface DirectoryMappings { provider?: string | null; provider_title?: string; bound: DirectoryBinding[]; duplicates: DirectoryDuplicate[]; skipped: DirectorySkipped[] }

export interface DirectoryPreview {
  teams: DirectoryTeamPlan[];
  teams_to_deactivate: Array<{ id: ID; name: string }>;
  members_total: number;
  members_new: number;
  members_existing: number;
  members_to_deactivate: Array<{ id: ID; name: string }>;
  /** 无法自动处理的项（如没有邮箱的人），每条是一句话 */
  notes: string[];
  /** 需要人决定的候选（ADR 0017 补记四）；旧后端不给 */
  confirmations?: DirectoryConfirmation[];
  decided?: DirectoryDecided[];
  skipped?: DirectorySkipped[];
  /** = confirmations.length；大于 0 时同步被挡住 */
  blocked_by_confirmations?: number;
}

// ---------- 通知外发（ADR 0019）：组织策略、个人规则、投递记录 ----------

/** 一个通道最近的健康状况：连续失败 ≥ 3 次时 degraded，last_error 是提供方原话 */
export interface NotifyHealth {
  status: "ok" | "degraded";
  streak: number;
  last_error: string;
  last_at?: ISODateTime;
}
/** 事件类型（顺序由后端固定） */
export interface NotifyKind { key: string; title: string }
/** 组织侧的一个通知通道：能不能用、要填什么、最近好不好 */
export interface NotifyChannel {
  key: string;
  title: string;
  enabled: boolean;
  configured: boolean;
  /** = enabled 且 configured */
  available: boolean;
  /** 是不是 IM 通道（复用 IM 集成的凭据与接入检查） */
  im: boolean;
  config: Record<string, string>;
  secrets_set: Record<string, boolean>;
  fields: DirectoryField[];
  prerequisites: string[];
  /** 为什么不可用（一句完整的话） */
  hint?: string;
  health: NotifyHealth;
}
export interface OrgNotifications {
  channels: Record<string, NotifyChannel>;
  channel_order: string[];
  allowed_kinds: string[];
  kinds: NotifyKind[];
  health: Record<string, NotifyHealth>;
}
export interface OrgNotificationsInput {
  channels?: Record<string, { enabled?: boolean; config?: Record<string, string> }>;
  allowed_kinds?: string[];
}
export type DeliveryStatus = "queued" | "sent" | "failed" | "skipped";
/** 一条外发投递：组织侧带收件人，个人侧不带 */
export interface Delivery {
  id: number;
  kind: string;
  kind_title: string;
  channel: string;
  channel_title: string;
  status: DeliveryStatus;
  status_title: string;
  title: string;
  text: string;
  url: string;
  error: string;
  attempts: number;
  created_at: ISODateTime;
  sent_at?: ISODateTime;
  recipient?: { id: ID; name: string };
}
/** 「发送测试」的结果：message 是一句可以直接显示的话 */
export interface NotifyTestResult { ok: boolean; message: string; delivery?: Delivery }
/** 个人能选的通道；bound 是我绑没绑这个 IM 的身份，没绑时 hint 说去哪儿绑 */
export interface MyNotifyChannel { key: string; title: string; im: boolean; bound: boolean; hint?: string }
export interface QuietHours { from: string; to: string }
export interface MyNotifications {
  rules: Record<string, string[]>;
  sources: Record<string, "personal" | "default">;
  quiet_hours: QuietHours | null;
  available_channels: MyNotifyChannel[];
  kinds: NotifyKind[];
  allowed_kinds: string[];
  defaults: Record<string, string[]>;
  source: "personal" | "default";
}
export interface MyNotificationsInput { rules?: Record<string, string[]>; quiet_hours?: QuietHours | null }

// ---------- 代码平台与外部事件（ADR 0020） ----------

export interface CodeRepo { id: string; full_name: string; enabled: boolean; hook_ok: boolean; hook_error?: string }
/** 六种外部事件，流程编辑器按它列候选 */
export interface CodeEvent { key: string; title: string }
export interface CodePlatform {
  provider: string | null;
  provider_title: string;
  configured: boolean;
  credentials: Record<string, string>;
  secrets_set: Record<string, boolean>;
  providers: DirectoryProviderInfo[];
  repos: CodeRepo[];
  webhook_url: string;
  /** 回调签名密钥：本系统生成。平时不返回，只有显式索取（revealSecret / 换密钥）的那一次才带上，且那一次会记进审计 */
  webhook_secret?: string;
  webhook_secret_set: boolean;
  proxy_url: string;
  console_url?: string;
  events: CodeEvent[];
}
export interface CodePlatformInput { provider?: string; credentials?: Record<string, string>; repos?: string[]; proxy_url?: string; /** 换一把回调密钥：返回的那一份带上新密钥，旧的立刻失效 */ rotate_webhook_secret?: boolean }
export interface CodePlatformTest { ok: boolean; repos: number; error?: string; warnings?: string[] }
export type CodeStep = "credentials" | "checks" | "repos" | "done";
export interface CodeChecklist {
  provider: string;
  provider_title: string;
  console_url?: string;
  webhook_url: string;
  checks: DirectoryCheck[];
  ready: boolean;
  next: { step: CodeStep; text: string };
  repos: string[];
}
/** 我在代码平台上的登录名 */
export interface CodeIdentity { provider: string; provider_title: string; login: string; bound: boolean }

export type LinkKind = "pr" | "issue" | "doc" | "design" | "other";
export const LINK_KINDS: LinkKind[] = ["pr", "issue", "doc", "design", "other"];
export type LinkStatus = "open" | "draft" | "merged" | "closed" | "passed" | "failed";
/** 任务上的一条外部链接（PR / Issue / 文档 / 设计稿 / 其它） */
export interface ExternalLink {
  id: ID;
  kind: LinkKind;
  kind_title: string;
  provider: string;
  url: string;
  title: string;
  status?: LinkStatus;
  status_title?: string;
  actor_name?: string;
  external_id?: string;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}
export interface LinkInput { kind?: LinkKind; url: string; title?: string }
/** 列表行与看板卡片上的 PR 小标：最近更新的那条 PR 链接 */
export interface TaskPR { status: LinkStatus; status_title: string; url: string; title: string; count: number }

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
  runtime?: string;
  created_at: ISODateTime;
  /** 当前登录者能不能改它：自己的 Agent 可以；别人的公共 Agent 只读（非管理员看不到其他人的私有 Agent） */
  can_manage: boolean;
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

/** GET /agents/{id}/check：接入向导的连接检查，一句人话（ADR 0018）。 */
export interface AgentCheck {
  online: boolean;
  /** 曾收到过它的请求 */
  connected: boolean;
  last_seen_at: ISODateTime | null;
  last_tool?: string | null;
  last_tool_at?: ISODateTime | null;
  hint: string;
}

// ---------- 设备码接入（ADR 0018）：Agent 端申请验证码，人在网页里批准 ----------

export type DeviceClient = "claude-code" | "cursor" | "codex" | "custom";
export const DEVICE_CLIENTS: DeviceClient[] = ["claude-code", "cursor", "codex", "custom"];
export type DeviceStatus = "pending" | "approved" | "denied" | "expired";

/** POST /agent-auth/device 的返回（公开；device_code 只有 Agent 端知道） */
export interface DeviceGrant {
  device_code: string;
  user_code: string;
  verification_url: string;
  expires_in: number;
  interval: number;
}

/** GET /agent-auth/device/{user_code}：人在网页里看到的申请 */
export interface DeviceRequest {
  user_code: string;
  client: DeviceClient | string;
  client_title: string;
  name: string;
  status: DeviceStatus;
  status_title: string;
  created_at: ISODateTime;
  expires_at: ISODateTime;
  decided_at: ISODateTime | null;
  agent: { id: ID; name: string } | null;
  approved_by: { id: ID; name: string } | null;
}

export type DeviceGrantChoice = "allow" | "with_approval" | "deny";

export interface DeviceApproveInput {
  name?: string;
  capabilities: string[];
  grants: Partial<Record<GrantName, DeviceGrantChoice>>;
  max_concurrency?: number;
  shared?: boolean;
}

/** POST /agent-auth/token：Agent 端轮询；token 只在批准后的第一次成功轮询里出现 */
export interface DeviceToken {
  status: DeviceStatus;
  token?: string;
  agent?: { id: ID; name: string };
  /** 组织名（后端返回的是一个名字，不是对象） */
  organization?: string;
  mcp_url?: string;
  interval?: number;
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
  /** 换上级：空字符串 / null 表示提为顶级 */
  parent_id?: ID | null;
  team_id?: ID | null;
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
  number?: number;
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
  /** 组织内递增的可读序号，界面上写成 #123（老后端没有时缺省） */
  number?: number;
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
  /** 仅限人工：Agent 不能领取 / 执行（老后端没有这个字段） */
  human_only?: boolean;
  relations: Relation[];
  artifacts: Artifact[];
  comments: Comment[];
  runs: Run[];
  /** 外部链接（ADR 0020）：详情给完整的一份，列表行是空数组 */
  links?: ExternalLink[];
  /** PR 小标（ADR 0020）：最近更新的那条 PR 链接，没有 PR 时不给 */
  pr?: TaskPR;
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
  human_only?: boolean;
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
export type TaskSummary = Pick<Task, "id" | "number" | "goal_id" | "goal" | "type" | "type_title" | "title" | "state" | "assignee" | "planned_start" | "planned_end" | "priority" | "progress" | "cost" | "points" | "sprint" | "estimate" | "pr">;

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
  /** 由外部事件触发（ADR 0020）：代码平台上发生这件事时自动走这一步 */
  triggered_by?: { source: "git"; event: string } | null;
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

/** 任务说明（CONTEXT.md「任务说明」，GET /tasks/{id}/brief）：执行者接到任务时看到的完整背景。任务详情的「执行简报」①②④段直接读它。 */
export interface TaskBrief {
  task: Task;
  type_title: string;
  state: TaskState;
  /** 任务类型给执行者的做法说明 */
  agent_instructions: string;
  task_schema?: Record<string, unknown> | null;
  result_schema?: Record<string, unknown> | null;
  /** 自上而下的目标标题（根目标在前） */
  goal_chain: string[];
  predecessors: TaskSummary[];
  predecessor_results: PredecessorResult[];
  workflow: WorkflowAvailability | null;
  names: Record<string, string>;
}
export interface PredecessorResult {
  task_id: ID;
  title: string;
  result?: unknown;
  artifacts: Artifact[];
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
  number?: number;
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
  | "TaskFieldChanged"
  | "GoalFieldChanged"
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
  | "DirectoryConfigured"
  | "DirectoryDisconnected"
  | "DirectorySyncRan"
  | "DirectoryDecided"
  | "DirectoryUnbound"
  | "MemberMerged"
  | "TeamMerged"
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
  | "WorkspaceLayoutUpdated"
  | "PreferencesUpdated"
  | "AgentConnected"
  // 通知外发（ADR 0019）与外部事件 / 外部链接（ADR 0020）
  | "NotificationChannelConfigured"
  | "NotificationPolicyChanged"
  | "NotificationPreferencesUpdated"
  | "CodePlatformConfigured"
  | "CodePlatformDisconnected"
  | "CodeIdentityBound"
  | "ExternalLinkAdded"
  | "ExternalLinkUpdated"
  | "ExternalLinkRemoved"
  | "ExternalEventApplied"
  | "ExternalEventIgnored";

export interface Event {
  id: ID;
  kind: EventKind;
  task_id: ID | null;
  task_title: string | null;
  task_number?: number | null;
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
export type BlockKey = "inbox" | "readouts" | "overview_summary" | "exceptions" | "my_tasks" | "my_review" | "team_load" | "cost_budget" | "events" | "sprint" | "backlog" | "trend";
/**
 * `inbox`（待我处理）与 `readouts`（组织概况）自 ADR 0015 补记四起也是区块，默认在最上面，可拖动 / 改大小 / 移除。
 * `proposals` 区块已从目录移除（DESIGN.md §12：被「待我处理」覆盖）；老布局里出现它时前端直接忽略。
 */
export const BLOCK_KEYS: BlockKey[] = ["inbox", "readouts", "overview_summary", "exceptions", "my_tasks", "my_review", "team_load", "cost_budget", "events", "sprint", "backlog", "trend"];
export const isBlockKey = (k: string): k is BlockKey => (BLOCK_KEYS as string[]).includes(k);

/** 网格常量（DESIGN.md §13）：12 栏、行高单位 120px、间距 16px、最高 6 行。 */
export const GRID_COLS = 12;
export const GRID_ROW_PX = 120;
export const GRID_GAP_PX = 16;
export const GRID_MAX_H = 6;
/** 区块宽度：占几栏，`min_w`…12 的任意整数（ADR 0015 补记三，不再限定档位）。 */
export type BlockWidth = number;
/** 区块高度：占几行，1…6 的任意整数。 */
export type BlockHeight = number;
export const isBlockWidth = (w: unknown): w is BlockWidth => typeof w === "number" && Number.isInteger(w) && w >= 1 && w <= GRID_COLS;
export const isBlockHeight = (h: unknown): h is BlockHeight => typeof h === "number" && Number.isInteger(h) && h >= 1 && h <= GRID_MAX_H;

/**
 * 每个区块的默认宽高与最小宽度（ADR 0015 补记二）。目录接口（GET /workspace/blocks）给的才是权威值；
 * 这里是老后端只给字符串数组、或还没带 default_w 时的兜底，与后端 GET /workspace/blocks 现在给的值一致（待我处理、组织概况、任务表、概览摘要、趋势最窄 6 栏，其余 3 栏）。
 */
export const BLOCK_SIZE_DEFAULTS: Record<BlockKey, { w: BlockWidth; h: BlockHeight; minW: BlockWidth }> = {
  inbox: { w: 12, h: 2, minW: 6 },
  readouts: { w: 12, h: 1, minW: 6 },
  overview_summary: { w: 12, h: 1, minW: 3 },
  exceptions: { w: 6, h: 2, minW: 3 },
  my_tasks: { w: 12, h: 2, minW: 6 },
  my_review: { w: 12, h: 2, minW: 6 },
  team_load: { w: 6, h: 2, minW: 3 },
  cost_budget: { w: 6, h: 2, minW: 3 },
  events: { w: 6, h: 3, minW: 3 },
  sprint: { w: 6, h: 2, minW: 3 },
  backlog: { w: 6, h: 2, minW: 3 },
  trend: { w: 12, h: 2, minW: 6 },
};

/** 布局里的一块：键 + 12 栏网格里的坐标与宽高（ADR 0015 补记三）。PUT 的输入与角色布局、预设都用这个形状。 */
export interface LayoutBlock {
  key: BlockKey;
  /** 左上角所在的栏（0 起），x + w ≤ 12 */
  x: number;
  /** 左上角所在的行（0 起） */
  y: number;
  w: BlockWidth;
  h: BlockHeight;
}

/** 已解析的工作台里的一块：title 由后端按语言给；坐标 / 宽高缺省时前端按目录默认与顺序补齐。 */
export interface WorkspaceBlock extends LayoutBlock {
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
  default_w: BlockWidth;
  default_h: BlockHeight;
  /** 编辑器不允许拖到比这更窄 */
  min_w: BlockWidth;
}

/** 预设：系统发的一套区块组合（带坐标与宽高），按"这类人关心什么"命名，不写死角色名。 */
export interface Preset {
  key: string;
  title: string;
  description: string;
  blocks: LayoutBlock[];
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
  blocks: LayoutBlock[];
  preset: string | null;
  member_count: number;
}

export type RoleWorkspaceInput = { blocks: LayoutBlock[] } | { preset: string };

/** 接口里一块区块的原始形状：老后端给字符串或只有宽高，新后端给带坐标的对象。 */
export type RawBlock = BlockKey | { key: BlockKey; title?: string; x?: number; y?: number; w?: number; h?: number };
type RawBlockDef = Omit<BlockDef, "default_w" | "default_h" | "min_w"> & Partial<Pick<BlockDef, "default_w" | "default_h" | "min_w">>;
type RawPreset = Omit<Preset, "blocks"> & { blocks: RawBlock[] };
type RawRoleWorkspace = Omit<RoleWorkspace, "blocks"> & { blocks: RawBlock[] };
type RawWorkspace = Omit<Workspace, "blocks"> & { blocks: Array<{ key: BlockKey; title?: string; x?: number; y?: number; w?: number; h?: number }> };

type SizeDefs = Pick<BlockDef, "key" | "default_w" | "default_h">[];
const isCoord = (v: unknown): v is number => typeof v === "number" && Number.isInteger(v) && v >= 0;

/** 两块是否重叠（都是 12 栏网格里的矩形）。 */
export const blocksOverlap = (a: LayoutBlock, b: LayoutBlock) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/**
 * 缺坐标时的致密排布（docs/api.md「工作台」：缺 x/y 的旧布局按顺序致密排布）：
 * 按数组顺序，从上到下逐行、从左到右逐栏找第一个能放下 w×h 的空位；已经有坐标的块先占位。结果确定，不依赖渲染。
 */
export function placeMissing(blocks: Array<Omit<LayoutBlock, "x" | "y"> & { x?: number; y?: number }>): LayoutBlock[] {
  const placed: LayoutBlock[] = [];
  const out: LayoutBlock[] = [];
  for (const b of blocks) {
    const w = Math.min(Math.max(1, b.w), GRID_COLS);
    if (isCoord(b.x) && isCoord(b.y)) {
      const fixed = { key: b.key, x: Math.min(b.x, GRID_COLS - w), y: b.y, w, h: b.h };
      placed.push(fixed);
      out.push(fixed);
    } else out.push({ key: b.key, x: -1, y: -1, w, h: b.h });
  }
  for (const b of out) {
    if (b.x >= 0) continue;
    search: for (let y = 0; ; y++) {
      for (let x = 0; x + b.w <= GRID_COLS; x++) {
        const cand = { ...b, x, y };
        if (!placed.some((p) => blocksOverlap(p, cand))) {
          b.x = x;
          b.y = y;
          placed.push(b);
          break search;
        }
      }
    }
  }
  return out;
}

/**
 * 向上压实（与 react-grid-layout 的 vertical compact 同一规则）：按 y、x 排序，每块尽量往上移到不碰别人为止。
 * 保存前与 mock 校验后都用它，保证不留空洞。
 */
export function compactLayout(blocks: LayoutBlock[]): LayoutBlock[] {
  const sorted = [...blocks].sort((a, b) => a.y - b.y || a.x - b.x);
  const done: LayoutBlock[] = [];
  for (const b of sorted) {
    const cur = { ...b };
    while (cur.y > 0 && !done.some((d) => blocksOverlap(d, { ...cur, y: cur.y - 1 }))) cur.y--;
    done.push(cur);
  }
  return blocks.map((b) => done.find((d) => d.key === b.key)!);
}

/**
 * 把接口给的一块补成完整的 {key, x, y, w, h}：宽高缺省或不合法时取目录默认（优先用调用方传进来的目录，其次 BLOCK_SIZE_DEFAULTS）；
 * 坐标缺省时留给 normalizeLayout 按顺序排布（这里只补尺寸，坐标原样带出）。目录里没有的键（如已移除的 proposals）原样保留，由页面按 registry 跳过。
 */
export function normalizeLayoutBlock(raw: RawBlock, defs?: SizeDefs): Omit<LayoutBlock, "x" | "y"> & { x?: number; y?: number } {
  const key = typeof raw === "string" ? raw : raw.key;
  const def = defs?.find((d) => d.key === key);
  const fallback = BLOCK_SIZE_DEFAULTS[key] ?? { w: 12, h: 2 };
  const r: { x?: number; y?: number; w?: number; h?: number } = typeof raw === "string" ? {} : raw;
  const w = isBlockWidth(r.w) ? r.w : (def?.default_w ?? fallback.w);
  const h = isBlockHeight(r.h) ? r.h : (def?.default_h ?? fallback.h);
  return { key, w, h, x: isCoord(r.x) ? r.x : undefined, y: isCoord(r.y) ? r.y : undefined };
}
/** 一整份布局：逐块补尺寸，再给缺坐标的块按顺序找空位。 */
export function normalizeLayout(raws: RawBlock[] | undefined, defs?: SizeDefs): LayoutBlock[] {
  return placeMissing((raws ?? []).map((b) => normalizeLayoutBlock(b, defs)));
}
function normalizeBlockDef(d: RawBlockDef): BlockDef {
  const fb = BLOCK_SIZE_DEFAULTS[d.key] ?? { w: 12, h: 2, minW: 3 };
  return { ...d, default_w: isBlockWidth(d.default_w) ? d.default_w : fb.w, default_h: isBlockHeight(d.default_h) ? d.default_h : fb.h, min_w: isBlockWidth(d.min_w) ? d.min_w : fb.minW };
}
function normalizeWorkspace(ws: RawWorkspace): Workspace {
  const blocks = normalizeLayout(ws.blocks ?? []);
  return { ...ws, blocks: blocks.map((b, i) => ({ ...b, title: ws.blocks?.[i]?.title ?? "" })) };
}
/** PUT 的请求体：只发 {key, x, y, w, h}。 */
const layoutBody = (blocks: LayoutBlock[]) => ({ blocks: blocks.map(({ key, x, y, w, h }) => ({ key, x, y, w, h })) });
/** 两份布局是否完全一样（键、坐标、宽高都相同，顺序无关）。 */
export const sameLayout = (a: LayoutBlock[], b: LayoutBlock[]) => a.length === b.length && a.every((x) => b.some((y) => y.key === x.key && y.x === x.x && y.y === x.y && y.w === x.w && y.h === x.h));

// ---------- 显示偏好（个人 → 角色 → 默认，逐字段解析；与工作台同一套思路） ----------

export type PrefField = "task_list_columns" | "task_card_fields" | "default_task_view" | "compact" | "sidebar_collapsed";
export const PREF_FIELDS: PrefField[] = ["task_list_columns", "task_card_fields", "default_task_view", "compact", "sidebar_collapsed"];
export type PrefSource = "personal" | "role" | "default";

export interface PrefValues {
  /** 任务列表显示哪些列（键在目录里；必须含 title） */
  task_list_columns: string[];
  /** 看板卡片显示哪些字段 */
  task_card_fields: string[];
  /** 进「任务」默认打开哪个页签 */
  default_task_view: string;
  compact: boolean;
  sidebar_collapsed: boolean;
}

/** GET /me/preferences：已解析，每个字段附带它来自哪一层 */
export interface Preferences extends PrefValues {
  source: PrefSource;
  sources: Record<PrefField, PrefSource>;
  /** source = role 时用到的角色代码名 */
  roles_used: string[];
  /** 个人明确设过的字段 */
  overrides: PrefField[];
}

/** PUT 的请求体：字段的任意子集；null 表示清掉这一层的这一项 */
export type PreferencesPatch = { [K in PrefField]?: PrefValues[K] | null };

export interface PrefOption {
  key: string;
  title: string;
}

/** GET /me/preferences/catalog */
export interface PreferencesCatalog {
  columns: PrefOption[];
  card_fields: PrefOption[];
  views: PrefOption[];
  defaults: PrefValues;
}

/** GET /org/preferences 的一行：一个角色的默认（只含设过的字段） */
export interface RolePreferences {
  role: string;
  role_title: string;
  data: Partial<PrefValues>;
  fields: PrefField[];
  member_count: number;
}

export const DEFAULT_PREFERENCES: PrefValues = {
  task_list_columns: ["number", "title", "type", "state", "assignee", "goal", "priority", "due"],
  task_card_fields: ["number", "assignee", "due", "priority"],
  default_task_view: "list",
  compact: false,
  sidebar_collapsed: false,
};

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

/** 非 JSON 的请求体 / 响应（CSV 导入导出）：text 给请求体的原文与类型；accept "text" 时把响应原文当结果 */
interface RawOptions { text?: { body: string; type: string }; accept?: "text" }
async function request<T>(method: string, path: string, body?: unknown, rawQuery?: Query, raw?: RawOptions): Promise<T> {
  const query = withScope(method, path, rawQuery);
  if (MOCK) {
    const { mockRequest } = await import("./mock");
    return mockRequest(method, path, raw?.text ? raw.text.body : body, query) as Promise<T>;
  }
  const url = new URL(`${API_BASE}/api/v1${path}`);
  if (query) {
    for (const [k, v] of Object.entries(query)) {
      if (v !== undefined && v !== null && v !== "") url.searchParams.set(k, String(v));
    }
  }
  const headers: Record<string, string> = { "Accept-Language": getLocale() };
  if (raw?.text) headers["Content-Type"] = raw.text.type;
  else if (body !== undefined) headers["Content-Type"] = "application/json";
  const res = await fetch(url.toString(), {
    method,
    credentials: "include",
    headers,
    body: raw?.text ? raw.text.body : body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  if (raw?.accept === "text" && res.ok) return text as T;
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

/** CSV 导入的请求体：没有决定时整个请求体就是 CSV 文本；带决定时用 JSON `{csv, decisions}`（两种形式后端都认） */
const importBody = (csv: string, decisions?: MemberImportDecision[]): [unknown, Query | undefined, RawOptions | undefined] =>
  decisions && decisions.length ? [{ csv, decisions }, undefined, undefined] : [undefined, undefined, { text: { body: csv, type: "text/csv" } }];

/** 对外可见的地址：浏览器里以当前页面来源为准（生产由 Go 进程同源托管），否则退回 API_BASE。 */
export const publicBase = (): string => (typeof window !== "undefined" && window.location.origin && !/^https?:\/\/localhost:3\d{3}$/.test(window.location.origin) ? window.location.origin : API_BASE);

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
    /** 按可读序号找任务（`123` 或 `#123`）；找不到 → 404 整句 */
    byNumber: (n: number | string) => request<Task>("GET", `/task-by-number/${encodeURIComponent(String(n).replace(/^#/, ""))}`),
    update: (id: ID, patch: Partial<TaskInput>) => request<Task>("PATCH", `/tasks/${encodeURIComponent(id)}`, patch),
    workflow: (id: ID) => request<WorkflowAvailability>("GET", `/tasks/${encodeURIComponent(id)}/workflow`),
    /** 任务说明：目标链、前置任务的结果与交付物、任务类型的做法说明 */
    brief: (id: ID) => request<TaskBrief>("GET", `/tasks/${encodeURIComponent(id)}/brief`),
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
    /** 外部链接（ADR 0020）：同一任务同一地址只有一条，重复即更新 */
    links: (id: ID) => request<ExternalLink[]>("GET", `/tasks/${encodeURIComponent(id)}/links`),
    addLink: (id: ID, input: LinkInput) => request<ExternalLink>("POST", `/tasks/${encodeURIComponent(id)}/links`, input),
    removeLink: (id: ID, linkId: ID) => request<void>("DELETE", `/tasks/${encodeURIComponent(id)}/links/${encodeURIComponent(linkId)}`),
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
    /** 连接检查：在线否、最近一次请求 / 工具，附一句现在该做什么 */
    check: (id: ID) => request<AgentCheck>("GET", `/agents/${encodeURIComponent(id)}/check`),
  },
  /** 设备码接入（ADR 0018）。device / token 是公开接口（Agent 端调用），其余需登录。 */
  agentAuth: {
    device: (input: { client: DeviceClient; name?: string }) => request<DeviceGrant>("POST", "/agent-auth/device", input),
    request: (userCode: string) => request<DeviceRequest>("GET", `/agent-auth/device/${encodeURIComponent(userCode)}`),
    approve: (userCode: string, input: DeviceApproveInput) => request<{ agent: Agent; request: DeviceRequest }>("POST", `/agent-auth/device/${encodeURIComponent(userCode)}/approve`, input),
    deny: (userCode: string) => request<DeviceRequest>("POST", `/agent-auth/device/${encodeURIComponent(userCode)}/deny`, {}),
    token: (deviceCode: string) => request<DeviceToken>("POST", "/agent-auth/token", { device_code: deviceCode }),
    /** 接入脚本的完整地址（在 Agent 所在机器上 `curl … | sh`）；浏览器里用页面自己的来源，避免把 localhost 写进命令 */
    scriptUrl: (client: DeviceClient) => `${publicBase()}/api/v1/agent-auth/connect.sh?client=${encodeURIComponent(client)}`,
  },
  /** 显示偏好：个人 → 角色 → 默认（逐字段）。Agent 调用一律 403。 */
  preferences: {
    get: () => request<Preferences>("GET", "/me/preferences"),
    update: (patch: PreferencesPatch) => request<Preferences>("PUT", "/me/preferences", patch),
    /** 一键重置：清掉个人记录，返回解析后的偏好 */
    reset: () => request<Preferences>("DELETE", "/me/preferences"),
    catalog: () => request<PreferencesCatalog>("GET", "/me/preferences/catalog"),
    /** 各角色的默认（需要 org_settings） */
    org: () => request<RolePreferences[]>("GET", "/org/preferences"),
    saveRole: (role: string, patch: PreferencesPatch) => request<RolePreferences>("PUT", `/org/preferences/${encodeURIComponent(role)}`, patch),
    resetRole: (role: string) => request<RolePreferences>("DELETE", `/org/preferences/${encodeURIComponent(role)}`),
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
    /** 我的工作台（已解析：个人微调 → 角色并集 → 默认）；宽高缺省时按目录默认补齐 */
    get: async (): Promise<Workspace> => normalizeWorkspace(await request<RawWorkspace>("GET", "/workspace")),
    /** 个人微调：区块键 + 宽高（ADR 0015 补记二） */
    saveMine: async (blocks: LayoutBlock[]): Promise<Workspace> => normalizeWorkspace(await request<RawWorkspace>("PUT", "/workspace/me", layoutBody(blocks))),
    /** 清除个人微调，回到角色默认 */
    resetMine: () => request<void>("DELETE", "/workspace/me"),
    /** 全部区块与预设（标题按语言）。后端若只返回区块数组，这里补成 { blocks, presets: [] }；没带尺寸字段时按默认补齐。 */
    catalog: async (): Promise<WorkspaceCatalog> => {
      const r = await request<{ blocks?: RawBlockDef[]; presets?: RawPreset[] } | RawBlockDef[]>("GET", "/workspace/blocks");
      const blocks = (Array.isArray(r) ? r : (r.blocks ?? [])).map(normalizeBlockDef);
      const presets = (Array.isArray(r) ? [] : (r.presets ?? [])).map((p) => ({ ...p, blocks: normalizeLayout(p.blocks, blocks) }));
      return { blocks, presets };
    },
    /** 各角色的布局（需要 org_settings） */
    org: async (): Promise<RoleWorkspace[]> => (await request<RawRoleWorkspace[]>("GET", "/org/workspace")).map((r) => ({ ...r, blocks: normalizeLayout(r.blocks) })),
    /** 给角色配布局：整套预设，或逐块给（带宽高） */
    saveRole: async (role: string, input: RoleWorkspaceInput): Promise<RoleWorkspace> => {
      const r = await request<RawRoleWorkspace>("PUT", `/org/workspace/${encodeURIComponent(role)}`, "blocks" in input ? layoutBody(input.blocks) : input);
      return { ...r, blocks: normalizeLayout(r.blocks) };
    },
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
  /** 我的通知规则与我在代码平台上的身份（ADR 0019 / 0020）；Agent 403。 */
  me: {
    notifications: {
      get: () => request<MyNotifications>("GET", "/me/notifications"),
      update: (input: MyNotificationsInput) => request<MyNotifications>("PUT", "/me/notifications", input),
      reset: () => request<MyNotifications>("DELETE", "/me/notifications"),
      deliveries: (limit = 20) => request<Delivery[]>("GET", "/me/notifications/deliveries", undefined, { limit }),
    },
    codeIdentity: {
      get: () => request<CodeIdentity>("GET", "/me/code-identity"),
      save: (login: string) => request<CodeIdentity>("PUT", "/me/code-identity", { login }),
    },
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
    createInvitation: (input: { email: string; name?: string; roles: string[]; team_id?: ID | null }) => request<Invitation>("POST", "/org/invitations", input),
    /** 成员与团队页：批量变更团队 / 改角色 / 停用 / 恢复 */
    bulkMembers: (input: { member_ids: ID[]; action: BulkMemberAction; team_id?: ID | null; roles?: string[] }) => request<BulkMembersResult>("POST", "/org/members/bulk", input),
    /** CSV 导出（姓名、邮箱、团队、角色），返回原文 */
    exportMembersCsv: () => request<string>("GET", "/org/members/export.csv", undefined, undefined, { accept: "text" }),
    /** CSV 导入：先预览（不落库），再确认 */
    importMembersPreview: (csv: string, decisions?: MemberImportDecision[]) => request<MemberImportPreview>("POST", "/org/members/import/preview", ...importBody(csv, decisions)),
    importMembers: (csv: string, decisions?: MemberImportDecision[]) => request<MemberImportResult>("POST", "/org/members/import", ...importBody(csv, decisions)),
    /** 把成员 id 并入 into（ADR 0017 补记四）：目标保留一切，旧成员的归属改到目标并停用 */
    mergeMember: (id: ID, into: ID) => request<OrgMember>("POST", `/org/members/${encodeURIComponent(id)}/merge`, { into }),
    deleteInvitation: (id: ID) => request<void>("DELETE", `/org/invitations/${encodeURIComponent(id)}`),
    roles: () => request<OrgRole[]>("GET", "/org/roles"),
    saveRole: (name: string, input: { title: LocalizedTitle; permissions: string[] }) => request<OrgRole>("PUT", `/org/roles/${encodeURIComponent(name)}`, input),
    deleteRole: (name: string) => request<void>("DELETE", `/org/roles/${encodeURIComponent(name)}`),
    teams: () => request<OrgTeam[]>("GET", "/org/teams"),
    createTeam: (input: { name: string; parent_id?: ID | null; lead_id?: ID | null; member_ids?: ID[]; is_boundary?: boolean }) => request<OrgTeam>("POST", "/org/teams", input),
    updateTeam: (id: ID, patch: { name?: string; parent_id?: ID | null; lead_id?: ID | null; member_ids?: ID[]; is_boundary?: boolean; active?: boolean }) =>
      request<OrgTeam>("PATCH", `/org/teams/${encodeURIComponent(id)}`, patch),
    deleteTeam: (id: ID) => request<void>("DELETE", `/org/teams/${encodeURIComponent(id)}`),
    /** 把团队 id 并入 into：旧团队的成员、目标、任务、迭代整体并入，旧团队停用不删除 */
    mergeTeam: (id: ID, into: ID) => request<OrgTeam>("POST", `/org/teams/${encodeURIComponent(id)}/merge`, { into }),
    capabilities: () => request<Capability[]>("GET", "/org/capabilities"),
    saveCapability: (name: string, input: { title: LocalizedTitle }) => request<Capability>("PUT", `/org/capabilities/${encodeURIComponent(name)}`, input),
    deleteCapability: (name: string) => request<void>("DELETE", `/org/capabilities/${encodeURIComponent(name)}`),
    pricing: () => request<OrgPricing>("GET", "/org/pricing"),
    savePrice: (modelId: string, input: PriceInput) => request<OrgPriceModel>("PUT", `/org/pricing/models/${encodeURIComponent(modelId)}`, input),
    deletePrice: (modelId: string) => request<void>("DELETE", `/org/pricing/models/${encodeURIComponent(modelId)}`),
    saveRate: (input: ExchangeRate) => request<ExchangeRate>("PUT", "/org/pricing/rates", input),
    /** 外部目录同步（ADR 0017）：配置、测试连接、预览、立即同步、历次记录 */
    directory: {
      get: () => request<DirectoryConfig>("GET", "/org/directory"),
      providers: () => request<DirectoryProviderInfo[]>("GET", "/org/directory/providers"),
      save: (input: DirectoryInput) => request<DirectoryConfig>("PUT", "/org/directory", input),
      test: () => request<DirectoryTest>("POST", "/org/directory/test", {}),
      checklist: () => request<DirectoryChecklist>("GET", "/org/directory/checklist"),
      preview: () => request<DirectoryPreview>("GET", "/org/directory/preview"),
      sync: () => request<DirectoryRun>("POST", "/org/directory/sync", {}),
      runs: (limit = 20) => request<DirectoryRun[]>("GET", "/org/directory/runs", undefined, { limit }),
      /** 冲突与对应关系（ADR 0017 补记四）：写决定、撤回决定、对应关系面板、解绑 */
      decide: (items: DirectoryDecisionInput[]) => request<DirectoryDecisionRecord[]>("PUT", "/org/directory/decisions", { items }),
      reconsider: (kind: DirectoryKind, externalId: string) => request<void>("DELETE", `/org/directory/decisions/${kind}/${encodeURIComponent(externalId)}`),
      mappings: () => request<DirectoryMappings>("GET", "/org/directory/mappings"),
      unbind: (kind: DirectoryKind, externalId: string) => request<void>("DELETE", `/org/directory/bindings/${kind}/${encodeURIComponent(externalId)}`),
      /** 断开：删掉凭据与同步设置，已同步进来的团队 / 成员、对应关系与历次同步记录都留着。幂等。 */
      disconnect: () => request<DirectoryConfig>("DELETE", "/org/directory"),
    },
    /** 通知外发（ADR 0019）：组织允许哪些通道与事件、发一条测试、最近投递 */
    notifications: {
      get: () => request<OrgNotifications>("GET", "/org/notifications"),
      save: (input: OrgNotificationsInput) => request<OrgNotifications>("PUT", "/org/notifications", input),
      test: (channel: string) => request<NotifyTestResult>("POST", "/org/notifications/test", { channel }),
      deliveries: (q: { limit?: number; channel?: string; status?: string } = {}) => request<Delivery[]>("GET", "/org/notifications/deliveries", undefined, q),
    },
    /** 代码平台（ADR 0020）：与 IM 集成同一套提供方注册表，只是能力不同 */
    codePlatform: {
      get: () => request<CodePlatform>("GET", "/org/code-platform"),
      /** 索取一次回调密钥：只有这一次的返回里带 webhook_secret，服务端会记一条审计 */
      revealSecret: () => request<CodePlatform>("GET", "/org/code-platform", undefined, { reveal: "secret" }),
      save: (input: CodePlatformInput) => request<CodePlatform>("PUT", "/org/code-platform", input),
      test: () => request<CodePlatformTest>("POST", "/org/code-platform/test", {}),
      checklist: () => request<CodeChecklist>("GET", "/org/code-platform/checklist"),
      repos: () => request<CodeRepo[]>("GET", "/org/code-platform/repos"),
      /** 断开：删掉凭据与仓库选择，已经挂上的外部链接与历史动态都留着。幂等。 */
      disconnect: () => request<CodePlatform>("DELETE", "/org/code-platform"),
    },
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
