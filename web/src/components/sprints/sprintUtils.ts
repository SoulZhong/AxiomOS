import type { Sprint, SprintStatus } from "@/lib/api";
import { diffDays, fmtDate, parseDate, today } from "@/lib/format";
import { t } from "@/lib/i18n";
import type { Tone } from "@/components/ui";

export const SPRINT_STATUSES: SprintStatus[] = ["active", "planning", "closed"];
export const sprintStatusTitle = (s: SprintStatus) => t(`sprints.status.${s}`);
export const sprintStatusTone: Record<SprintStatus, Tone> = { active: "accent", planning: "neutral", closed: "success" };

export const fmtRange = (a: string, b: string) => `${fmtDate(a)} – ${fmtDate(b)}`;

/** 时间读数：进行中 = 还剩 / 已超 n 天；规划中 = n 天后开始；已结束 = 时长。over 为真时读数标红。 */
export function sprintDays(sp: Sprint): { text: string; over: boolean } {
  const s = parseDate(sp.starts_on), e = parseDate(sp.ends_on);
  if (!s || !e) return { text: "—", over: false };
  const now = today();
  const len = Math.max(1, diffDays(s, e));
  if (sp.status === "active") {
    const d = diffDays(now, e);
    if (d > 0) return { text: t("sprints.daysLeft", { n: d }), over: false };
    if (d === 0) return { text: t("sprints.endsToday"), over: false };
    return { text: t("sprints.daysOver", { n: -d }), over: true };
  }
  if (sp.status === "planning") {
    const d = diffDays(now, s);
    return { text: d > 0 ? t("sprints.startsIn", { n: d }) : t("sprints.duration", { n: len }), over: false };
  }
  return { text: t("sprints.duration", { n: len }), over: false };
}

export const sprintProgress = (sp: Sprint) => (sp.points_total > 0 ? Math.round((sp.points_done / sp.points_total) * 100) : 0);
