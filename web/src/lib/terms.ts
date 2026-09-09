// 界面用语：代码标识符 -> 当前语言的文案（CONTEXT.md / ADR 0007，字典在 src/lib/i18n.ts）。
// 数据自带的名字（状态、步骤、角色、交付物类型……）由后端按语言返回，这里的兜底只在缺失时使用，不覆盖服务端给的。
import type { AgentState, EventKind, GrantName, GrantMode, Priority, Proposal, ProposalStatus, ProposalTarget, RelationType, RunOutcome, StateLabel, TaskType } from "./api";
import { parseDate } from "./format";
import { t, type Key } from "./i18n";

export const STATE_LABELS: StateLabel[] = ["pending", "active", "waiting", "terminal_success", "terminal_failure"];
export const stateLabel = (l: StateLabel) => t(`label.${l}` as Key);

export const RELATIONS: RelationType[] = ["blocks", "found_in", "related"];
export const relationTitle = (r: RelationType) => t(`relation.${r}` as Key);

export const GRANT_ORDER: GrantName[] = [
  "execute",
  "claim_backlog",
  "review",
  "comment",
  "create_subtask",
  "create_task",
  "create_goal",
  "assign",
  "link",
  "manage_workflows",
];
export const grantTitle = (g: GrantName) => t(`grant.${g}` as Key);
export const grantModeTitle = (m: GrantMode) => t(`grantMode.${m}` as Key);

/**
 * 授权预设（DESIGN.md §21）：确认页与注册抽屉上按角色一键填好十项授权，再逐项微调。
 * 预设只是预填，不改授权模型（ADR 0003：所有者 ∩ 授权，高风险动作需要人确认），
 * 所以这张表是界面常量，不是组织可配置的词表（ADR 0014：先写死，等有人要改再开）。
 * 值：allow = 直接生效，with_approval = 需要人确认，没列的 = 不给。
 */
export type GrantPreset = "developer" | "pm" | "tester" | "observer";
export const GRANT_PRESETS: GrantPreset[] = ["developer", "pm", "tester", "observer"];
export const GRANT_PRESET_MODES: Record<GrantPreset, Partial<Record<GrantName, "allow" | "with_approval">>> = {
  // 开发：自己领、自己做、随手记；建子任务与关联直接生效，验收与指派不给
  developer: { execute: "allow", claim_backlog: "allow", comment: "allow", create_subtask: "allow", link: "allow", create_task: "with_approval" },
  // 产品：拆任务、建目标、指派；执行与验收都要人确认
  pm: { create_task: "allow", create_subtask: "allow", comment: "allow", link: "allow", create_goal: "with_approval", assign: "with_approval", execute: "with_approval", review: "with_approval" },
  // 测试：领测试任务、做验收、报 Bug（创建任务直接生效，Bug 要及时进系统）
  tester: { execute: "allow", claim_backlog: "allow", review: "allow", comment: "allow", create_task: "allow", link: "allow" },
  // 只读观察：一项授权都不给，只能看（读工具不要授权）
  observer: {},
};
export const grantPresetTitle = (p: GrantPreset) => t(`grantPreset.${p}` as Key);
/** 当前这组选择正好等于哪个预设（没有就 null，界面显示「自定义」）。 */
export function matchGrantPreset(modes: Partial<Record<GrantName, "allow" | "with_approval" | "deny" | "direct" | "">>): GrantPreset | null {
  const norm = (v: string | undefined) => (v === "direct" ? "allow" : v === "deny" ? "" : v ?? "");
  for (const p of GRANT_PRESETS) {
    const want = GRANT_PRESET_MODES[p];
    if (GRANT_ORDER.every((g) => norm(modes[g]) === (want[g] ?? ""))) return p;
  }
  return null;
}

// Agent 状态（CONTEXT.md「Agent 状态」）：执行中 / 可用 / 已停用。后端也给 state_title，
// 但界面自己译一遍，切换语言时不用等下一次请求。
export const AGENT_STATES: AgentState[] = ["running", "ready", "inactive"];
export const agentStateTitle = (s: AgentState) => t(`agentState.${s}` as Key);

export const runOutcomeTitle = (o: RunOutcome) => t(`outcome.${o}` as Key);

export const PRIORITIES: Priority[] = ["low", "normal", "high", "urgent"];
export const priorityTitle = (p: Priority) => t(`priority.${p}` as Key);

export const eventTitle = (k: EventKind) => {
  const key = `event.${k}` as Key;
  const s = t(key);
  return s === key ? k : s;
};

// 四种组织级权限（CONTEXT.md「权限」；view_all_cost 见 ADR 0013）
export const PERMISSIONS = ["org_settings", "manage_workflows", "cancel_any_task", "view_all_work", "view_all_cost"] as const;
/** 跨共享边界的两项例外权限（ADR 0013），角色编辑界面要单独解释 */
export const CROSS_BOUNDARY_PERMISSIONS = ["view_all_work", "view_all_cost"] as const;
export type Permission = (typeof PERMISSIONS)[number];
export const permissionTitle = (p: string) => (PERMISSIONS.includes(p as Permission) ? t(`perm.${p}` as Key) : p);

const ROLE_FALLBACK = ["pm", "designer", "developer", "tester", "releaser", "admin"];
const ARTIFACT_TYPE_FALLBACK = ["prd", "pr", "test_report", "release_note", "result", "doc", "report", "file"];

/** 角色界面名：优先服务端给的（会话 roles），其次内置角色的兜底，最后原样。 */
export const roleTitle = (role: string, roles?: Record<string, string>) => roles?.[role] ?? (ROLE_FALLBACK.includes(role) ? t(`role.${role}` as Key) : role);
/** 交付物类型界面名：优先服务端给的（会话 artifact_types）。 */
export const artifactTypeTitle = (type: string, types?: Record<string, string>) => types?.[type] ?? (ARTIFACT_TYPE_FALLBACK.includes(type) ? t(`artifactType.${type}` as Key) : type);
/** 能力标签界面名：优先服务端给的（GET /capabilities）。 */
export const capabilityTitle = (c: string, caps?: Record<string, string>) => caps?.[c] ?? c;

/** 步骤 by 规则 -> 文案 */
export function describeBy(rule: string, type?: TaskType, roles?: Record<string, string>): string {
  if (rule === "creator") return t("rule.creator");
  if (rule === "assignee") return t("rule.assignee");
  if (rule === "reviewer") return t("rule.reviewer");
  if (rule === "anyone") return t("rule.anyone");
  if (rule.startsWith("participant:")) {
    const slot = rule.slice("participant:".length);
    return t("rule.participant", { title: type?.participants[slot]?.title ?? slot });
  }
  if (rule.startsWith("role:")) return t("rule.role", { title: roleTitle(rule.slice(5), roles) });
  return rule;
}

/** 步骤 requires 条件 -> 文案 */
export function describeRequire(c: string, artifactTypes?: Record<string, string>): string {
  if (c === "deps_done") return t("require.deps_done");
  if (c === "comment") return t("require.comment");
  if (c === "no_open_bugs") return t("require.no_open_bugs");
  if (c === "result") return t("require.result");
  if (c.startsWith("artifact:")) return t("require.artifact", { type: artifactTypeTitle(c.slice(9), artifactTypes) });
  return c;
}

/** 步骤 assign_to -> 文案 */
export function describeAssignTo(v: string | null | undefined, type?: TaskType): string {
  if (v === undefined) return t("assignTo.unchanged");
  if (v === null) return t("assignTo.clear");
  if (v === "creator") return t("rule.creator");
  if (v.startsWith("participant:")) {
    const slot = v.slice("participant:".length);
    return t("rule.participant", { title: type?.participants[slot]?.title ?? slot });
  }
  return v;
}

/** 状态名 -> 文案（$previous 和 * 也要翻） */
export function describeStateName(name: string, type?: TaskType): string {
  if (name === "*") return t("state.any");
  if (name === "$previous") return t("state.previous");
  return type?.workflow.states[name]?.title ?? name;
}

// ---------- 待确认操作（ADR 0003） ----------

export const PROPOSAL_STATUSES: ProposalStatus[] = ["pending", "approved", "rejected", "expired"];
export const proposalStatusTitle = (s: ProposalStatus) => t(`proposal.status.${s}` as Key);

/**
 * 界面上的状态：还没人确认、但有效期已过的，按「已过期」显示。
 * 后端会在它自己的节奏里把 pending 改成 expired，两边不一致时以时间为准，人不会看到一条"过期了还催你确认"的记录。
 */
export function proposalStatusOf(p: Pick<Proposal, "status" | "expires_at">, now = Date.now()): ProposalStatus {
  if (p.status !== "pending") return p.status;
  const at = parseDate(p.expires_at);
  return at && at.getTime() <= now ? "expired" : "pending";
}

/** 待确认操作的目标（任务 / 目标 / 迭代）在界面上的链接；任务类型与 Agent 没有独立详情页，返回 null。 */
export function proposalTargetHref(target: ProposalTarget | null): string | null {
  if (!target) return null;
  if (target.kind === "task") return `/tasks/${encodeURIComponent(target.id)}/`;
  if (target.kind === "goal") return `/goals/${encodeURIComponent(target.id)}/`;
  if (target.kind === "sprint") return `/sprints/${encodeURIComponent(target.id)}/`;
  return null;
}
export const proposalTargetKindTitle = (k: ProposalTarget["kind"]) => t(`proposal.target.${k}` as Key);

/**
 * 动作的输入（payload）里一个字段的界面名：认识的字段给中文，不认识的原样显示代码名。
 * 后端可以随时加新动作，这里不做穷举，只保证常见字段读起来是人话。
 */
const PAYLOAD_FIELDS = [
  "title", "description", "goal_id", "goal", "parent_id", "type", "task_type", "priority", "estimate", "points",
  "planned_start", "planned_end", "assignee_id", "assignee", "executor_id", "reviewer_id", "task_id", "task_ids",
  "sprint_id", "state", "from", "to", "transition", "transition_title", "body", "comment", "kind", "reason",
  "capabilities", "required_role", "artifact", "url", "result", "summary", "outcome", "name", "unfinished",
];
export const proposalFieldTitle = (key: string) => (PAYLOAD_FIELDS.includes(key) ? t(`proposal.field.${key}` as Key) : key);
