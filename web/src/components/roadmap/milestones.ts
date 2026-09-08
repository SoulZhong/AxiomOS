import type { Milestone, MilestoneMark, MilestoneStatus } from "@/lib/api";
import { parseDate } from "@/lib/format";
import { t } from "@/lib/i18n";

/*
 * 路线图上的里程碑（ADR 0022）：菱形沿用甘特图的渲染器，只是密的时候合并——
 * 挨得太近（同一周之内）的合成一枚，气泡里把这一簇全列出来。
 */

/** 一簇里程碑：画一枚菱形，气泡里列全 */
export interface MsCluster extends MilestoneMark {
  items: Milestone[];
}

/** 越靠前越"要紧"：逾期盖过未到，未到盖过已达到 */
const rank: Record<MilestoneStatus, number> = { overdue: 0, upcoming: 1, reached: 2 };
/** 两枚菱形挨得比这还近就合并（菱形 8px + 一点呼吸） */
const MERGE_PX = 13;

export function clusterMilestones(ms: readonly Milestone[], dayW: number): MsCluster[] {
  if (!ms.length) return [];
  const sorted = [...ms].sort((a, b) => (a.due_on < b.due_on ? -1 : a.due_on > b.due_on ? 1 : 0));
  const dayOf = (d: string) => (parseDate(d)?.getTime() ?? 0) / 86400_000;
  const out: MsCluster[] = [];
  let cur: Milestone[] = [];
  const flush = () => {
    if (!cur.length) return;
    const head = [...cur].sort((a, b) => rank[a.status] - rank[b.status])[0];
    out.push({
      id: cur[0].id,
      title: cur.length === 1 ? cur[0].title : t("roadmap.msCluster", { n: cur.length }),
      due_on: cur[0].due_on,
      status: head.status,
      ready_hint: cur.some((m) => m.ready_hint),
      items: cur,
    });
    cur = [];
  };
  for (const m of sorted) {
    if (cur.length && (dayOf(m.due_on) - dayOf(cur[0].due_on)) * dayW > MERGE_PX) flush();
    cur.push(m);
  }
  flush();
  return out;
}
