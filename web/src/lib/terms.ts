// 界面用语：代码标识符 -> 当前语言的文案（CONTEXT.md / ADR 0007，字典在 src/lib/i18n.ts）。
// 数据自带的名字（状态、步骤、角色、交付物类型……）由后端按语言返回，这里的兜底只在缺失时使用，不覆盖服务端给的。
import type { EventKind, GrantName, GrantMode, Priority, Proposal, ProposalStatus, ProposalTarget, RelationType, RunOutcome, StateLabel, TaskType } from "./api";
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
