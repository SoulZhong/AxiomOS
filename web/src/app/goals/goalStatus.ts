import type { Goal, Task } from "@/lib/api";
import { t } from "@/lib/i18n";
import { isOverdue } from "@/components/TaskTable";

/**
 * 目标行的「状态摘要句」（DESIGN.md §9）：一句话说清这个目标下一步该干什么。
 * 统计范围是目标自己 + 全部后代目标下的任务。按优先级取第一条命中的：
 * 没有任务 → 逾期 → 等验收 → 都完成了等确认 → 有进行中 → 未开始。
 * 里程碑（ADR 0016）插在最前面：目标还在做、却有逾期的里程碑时，先说「里程碑「X」已逾期」。
 */
export interface GoalTally {
  total: number;
  done: number;
  overdue: number;
  waiting: number;
  active: number;
  pending: number;
}

export type Tone = "danger" | "warning" | "accent" | "muted";

const EMPTY: GoalTally = { total: 0, done: 0, overdue: 0, waiting: 0, active: 0, pending: 0 };

/** 把任务按目标归拢，再沿目标树把子目标的数字并进父目标 */
export function tallyGoals(goals: Goal[], tasks: Task[], isAcceptanceWait: (x: Task) => boolean): Map<string, GoalTally> {
  const own = new Map<string, GoalTally>();
  for (const x of tasks) {
    if (!x.goal_id) continue;
    const c = own.get(x.goal_id) ?? { ...EMPTY };
    c.total += 1;
    const label = x.state.label;
    if (label === "terminal_success") c.done += 1;
    else if (label === "terminal_failure") {
      // 已终止的任务不计入"还要做的事"，但仍算在总数里
    } else if (isOverdue(x)) c.overdue += 1;
    else if (isAcceptanceWait(x)) c.waiting += 1;
    else if (label === "active" || label === "waiting") c.active += 1;
    else c.pending += 1;
    own.set(x.goal_id, c);
  }
  const out = new Map<string, GoalTally>();
  const walk = (g: Goal): GoalTally => {
    const acc = { ...(own.get(g.id) ?? EMPTY) };
    for (const kid of g.children ?? []) {
      const k = walk(kid);
      acc.total += k.total;
      acc.done += k.done;
      acc.overdue += k.overdue;
      acc.waiting += k.waiting;
      acc.active += k.active;
      acc.pending += k.pending;
    }
    out.set(g.id, acc);
    return acc;
  };
  goals.forEach(walk);
  return out;
}

/** 摘要句 + 语气；needsAchieve 为真时行内出现「确认达成」 */
export function goalSummary(goal: Goal, c: GoalTally | undefined): { text: string; tone: Tone; needsAchieve: boolean } {
  const x = c ?? EMPTY;
  if (goal.status === "abandoned") return { text: t("goals.sum.abandoned"), tone: "muted", needsAchieve: false };
  if (goal.achieved) return { text: t("goals.sum.achieved"), tone: "accent", needsAchieve: false };
  const lateMs = goal.milestones?.find((m) => m.status === "overdue");
  if (lateMs) return { text: t("goals.sum.msOverdue", { title: lateMs.title }), tone: "danger", needsAchieve: false };
  if (x.total === 0) return { text: t("goals.sum.noTasks"), tone: "muted", needsAchieve: false };
  if (x.overdue > 0) return { text: t("goals.sum.overdue", { n: x.overdue }), tone: "danger", needsAchieve: false };
  if (x.waiting > 0) return { text: t("goals.sum.waiting", { n: x.waiting }), tone: "warning", needsAchieve: false };
  if (x.done === x.total) return { text: t("goals.sum.allDone"), tone: "accent", needsAchieve: true };
  if (x.active > 0) return { text: x.pending > 0 ? t("goals.sum.active", { n: x.active, rest: x.pending }) : t("goals.sum.activeOnly", { n: x.active }), tone: "accent", needsAchieve: false };
  return { text: t("goals.sum.pending", { n: x.pending }), tone: "muted", needsAchieve: false };
}
