// 就地编辑的动态句子（TaskFieldChanged / GoalFieldChanged，DESIGN.md §15）。
// 真实后端在 event.summary 里给出这句话（internal/api/views.go fieldChangeSummary），前端优先用它；
// 这里的实现按同一套模板拼，给示例数据模式用，也给 summary 缺失的老数据兜底。
import type { Event, Priority } from "./api";
import { fmtDate, fmtMoney, fmtNumber } from "./format";
import { getLocale, t, type Key } from "./i18n";
import { priorityTitle } from "./terms";

/** 后端把优先级存成 0–3（紧急 → 低），动态数据里可能是数字也可能是代码名。 */
const PRIORITY_BY_INDEX: Priority[] = ["urgent", "high", "normal", "low"];
function priorityOf(v: unknown): string {
  if (typeof v === "number") return PRIORITY_BY_INDEX[v] ? priorityTitle(PRIORITY_BY_INDEX[v]) : String(v);
  if (typeof v === "string" && (PRIORITY_BY_INDEX as string[]).includes(v)) return priorityTitle(v as Priority);
  return String(v ?? "");
}

const str = (v: unknown): string => {
  if (v === null || v === undefined) return "";
  if (typeof v === "number") return fmtNumber(v, Number.isInteger(v) ? 0 : 2);
  if (typeof v === "boolean") return v ? "true" : "false";
  if (Array.isArray(v)) return v.map(str).join(getLocale() === "zh-CN" ? "、" : ", ");
  return String(v);
};
const empty = (v: unknown) => v === null || v === undefined || v === "" || (Array.isArray(v) && v.length === 0);

/** 写死词表（时间桶、信心度、时间粒度）的名字：空值没有名字。 */
const enumTitle = (kind: "horizon" | "confidence" | "date_precision", v: unknown): string => (empty(v) ? "" : t(`${kind}.${str(v)}` as Key));

/** 字段名 → 界面名；自定义字段用「字段名」。 */
export function fieldLabel(field: string, data: Record<string, unknown>): string {
  if (field === "fields") return `「${str(data.key)}」`;
  if (field === "participants" && data.label) return str(data.label);
  const key = `fc.field.${field}` as Key;
  const s = t(key);
  return s === key ? field : s;
}

/**
 * 一条字段改动 → 一句话。kind 由事件类型决定；对象名取 data.title，任务事件再退回 task_title。
 * who 是执行者名（系统动作为「系统」）。
 */
export function fieldChangeSentence(e: Pick<Event, "kind" | "data" | "actor" | "task_title">): string {
  const d = e.data ?? {};
  const kind = e.kind === "GoalFieldChanged" ? "goal" : "task";
  const who = e.actor?.name ?? t("common.system");
  const title = str(d.title) || e.task_title || "";
  const obj = t(`fc.obj.${kind}` as Key, { title });
  const field = str(d.field);
  let from = d.from, to = d.to;
  if (empty(from)) from = null;
  if (empty(to)) to = null;
  const name = (idKey: "from" | "to", titleKey: "from_title" | "to_title") => (empty(d[titleKey]) ? str(d[idKey]) : str(d[titleKey]));
  const label = fieldLabel(field, d);

  switch (field) {
    case "title":
      return t("fc.title", { who, kind: t(`fc.kind.${kind}` as Key), from: str(from), to: str(to) });
    case "description":
      return t("fc.description", { who, obj });
    case "parent_id": {
      const fromN = name("from", "from_title"), toN = name("to", "to_title");
      if (to === null || !toN) return t("fc.parent_top", { who, obj });
      if (from === null || !fromN) return t("fc.parent_set", { who, obj, to: toN });
      return t("fc.parent_moved", { who, obj, from: fromN, to: toN });
    }
    case "human_only":
      return t(to === true ? "fc.human_only_on" : "fc.human_only_off", { who, obj });
    // 成果指标是一句话，写全新的那句就够了，不用把旧句子也念一遍（ADR 0021）
    case "outcome":
      if (to === null) return t("fc.cleared", { who, obj, field: label });
      return t("fc.rewrote_q", { who, obj, field: label, to: str(to) });
    case "status":
      if (to === "achieved") return t("fc.status.achieved", { who, obj });
      if (to === "abandoned") return t("fc.status.abandoned", { who, obj });
      if (to === "active" && from === "abandoned") return t("fc.status.resumed", { who, obj });
      if (to === "active" && from === "achieved") return t("fc.status.unachieved", { who, obj });
      break;
  }

  // 值的呈现：名字、文字套引号；日期、数字、金额裸写
  let fromS = "", toS = "", quoted = true;
  switch (field) {
    case "owner_member_id": case "team_id": case "reviewer_id": case "goal_id": case "required_capabilities": case "participants":
      fromS = name("from", "from_title"); toS = name("to", "to_title"); break;
    case "priority":
      fromS = priorityOf(from); toS = priorityOf(to); break;
    // 时间桶、信心度与时间粒度只存取值，名字按当前语言现渲染（ADR 0021、0022）
    case "horizon": case "confidence": case "date_precision":
      fromS = enumTitle(field, from); toS = enumTitle(field, to); break;
    case "deadline": case "planned_start": case "planned_end":
      quoted = false; fromS = from === null ? "" : fmtDate(str(from)); toS = to === null ? "" : fmtDate(str(to)); break;
    case "budget": {
      quoted = false;
      const cur = typeof d.currency === "string" && d.currency ? d.currency : undefined;
      fromS = typeof from === "number" ? fmtMoney(from, cur) : ""; toS = typeof to === "number" ? fmtMoney(to, cur) : ""; break;
    }
    case "estimate_hours":
      quoted = false;
      fromS = typeof from === "number" ? t("fc.hours", { n: str(from) }) : ""; toS = typeof to === "number" ? t("fc.hours", { n: str(to) }) : ""; break;
    default:
      fromS = str(from); toS = str(to);
  }
  const q = quoted ? "_q" : "";
  if (to === null && !toS) return t("fc.cleared", { who, obj, field: label });
  if (from === null && !fromS) return t(`fc.set${q}` as Key, { who, obj, field: label, to: toS });
  return t(`fc.changed${q}` as Key, { who, obj, field: label, from: fromS, to: toS });
}

/** 动态面板的正文：有服务端句子就用它，没有（老数据 / 示例）再拼。 */
export function eventSummary(e: Event): string {
  if (e.summary) return e.summary;
  if (e.kind === "TaskFieldChanged" || e.kind === "GoalFieldChanged") return fieldChangeSentence(e);
  return "";
}
