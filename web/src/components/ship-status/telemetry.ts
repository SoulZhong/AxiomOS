"use client";
import { useEffect, useSyncExternalStore } from "react";
import { api, isTerminal, type Event, type ProposalCount, type StateLabel, type Task } from "@/lib/api";
import { parseDate, today } from "@/lib/format";

/**
 * 舰内遥测：舷窗带（BridgeBar）与「舰况」面板共用的一份组织读数，30s 刷新一次，标签页隐藏时停，
 * 有几个订阅者都只拉一次。全部来自现有接口，没有新端点：
 * - 计数：GET /tasks（全部）→ 进行中 / 待验收（waiting）/ 逾期（计划结束日已过且未结束）/ 全部；GET /agents → 在线 / 全部。
 * - 24 点趋势：GET /events（最近 300 条）里的「状态变化」按小时倒放——从当前状态出发，每跨过一个整点记一次三类计数
 *   （状态名 → 状态类型 由 GET /task-types 的流程定义给出；查不到的状态跳过）。
 * - 今日成本：今天有执行记录事件（开始 / 结束 / 上报用量）的任务，各取一次 GET /tasks/:id 拿到执行记录的金额，
 *   按结束（或开始）小时累计成 24 点；没有今天的执行记录时是一条平线（costReal=false，界面会说明）。
 * - 待确认操作：GET /proposals/count 的 pending，侧栏徽标与状态栏读数共用它（同一个 30s 轮询，不另起定时器）。
 */
export const HOURS = 24;

export interface Series {
  active: number[];
  waiting: number[];
  overdue: number[];
  cost: number[];
}

export interface ShipTelemetry {
  active: number;
  waiting: number;
  overdue: number;
  total: number;
  agentsOnline: number;
  agentsTotal: number;
  costToday: number;
  costReal: boolean;
  /** 等人确认的操作条数（ADR 0003）；老后端没有这个接口时是 0 */
  proposalsPending: number;
  series: Series;
  at: number;
}

const REFRESH_MS = 30_000;
const EVENT_LIMIT = 300;
const COST_TASK_CAP = 8;

const isOverdueAt = (t: Task, label: StateLabel, dayStart: Date) => {
  const pe = parseDate(t.planned_end);
  return !!pe && pe < dayStart && !isTerminal(label);
};

const startOfDay = (d: Date) => {
  const r = new Date(d);
  r.setHours(0, 0, 0, 0);
  return r;
};

/** 状态变化事件按小时倒放，得到过去 24 小时每个整点的三类计数（最后一点是现在）。 */
function replaySeries(tasks: Task[], events: Event[], labelOf: (task: Task, state: string) => StateLabel | null, now: Date): Omit<Series, "cost"> {
  const byId = new Map(tasks.map((t) => [t.id, t]));
  const label = new Map(tasks.map((t) => [t.id, t.state.label]));
  const count = (at: Date) => {
    const ds = startOfDay(at);
    let active = 0, waiting = 0, overdue = 0;
    for (const t of tasks) {
      const l = label.get(t.id) ?? t.state.label;
      if (l === "active") active++;
      else if (l === "waiting") waiting++;
      if (isOverdueAt(t, l, ds)) overdue++;
    }
    return { active, waiting, overdue };
  };
  const transitions = events
    .filter((e) => e.kind === "TaskTransitioned" && e.task_id && byId.has(e.task_id))
    .map((e) => ({ at: new Date(e.created_at).getTime(), task: byId.get(e.task_id!)!, from: String(e.data?.from ?? "") }))
    .filter((e) => Number.isFinite(e.at))
    .sort((a, b) => b.at - a.at);
  const out = { active: new Array<number>(HOURS), waiting: new Array<number>(HOURS), overdue: new Array<number>(HOURS) };
  let k = 0;
  for (let i = HOURS - 1; i >= 0; i--) {
    const at = new Date(now.getTime() - (HOURS - 1 - i) * 3600_000);
    // 把这一刻之后发生的状态变化撤回：任务回到 from 状态
    while (k < transitions.length && transitions[k].at > at.getTime()) {
      const tr = transitions[k++];
      const l = labelOf(tr.task, tr.from);
      if (l) label.set(tr.task.id, l);
    }
    const c = count(at);
    out.active[i] = c.active;
    out.waiting[i] = c.waiting;
    out.overdue[i] = c.overdue;
  }
  return out;
}

/** 今日成本：今天有执行记录事件的任务，各取一次详情，按小时累计。 */
async function costSeries(events: Event[], now: Date): Promise<{ series: number[]; total: number; real: boolean }> {
  const dayStart = startOfDay(now).getTime();
  const ids = new Set<string>();
  for (const e of events) {
    if ((e.kind === "RunStarted" || e.kind === "RunEnded" || e.kind === "UsageReported") && e.task_id && new Date(e.created_at).getTime() >= dayStart) ids.add(e.task_id);
    if (ids.size >= COST_TASK_CAP) break;
  }
  const buckets = new Array<number>(HOURS).fill(0);
  if (ids.size === 0) return { series: buckets, total: 0, real: false };
  const details = await Promise.all([...ids].map((id) => api.tasks.get(id).catch(() => null)));
  let any = false;
  for (const t of details) {
    for (const r of t?.runs ?? []) {
      const end = r.ended_at ? new Date(r.ended_at).getTime() : now.getTime();
      const start = new Date(r.started_at).getTime();
      const stamp = r.ended_at ? end : start;
      if (stamp < dayStart || !(r.cost > 0)) continue;
      any = true;
      buckets[Math.min(HOURS - 1, Math.max(0, new Date(stamp).getHours()))] += r.cost;
    }
  }
  // 累计到当前小时，之后保持（折线尾端 = 今日总额）
  const series = new Array<number>(HOURS);
  let acc = 0;
  const h = now.getHours();
  for (let i = 0; i < HOURS; i++) {
    if (i <= h) acc += buckets[i];
    series[i] = acc;
  }
  return { series, total: acc, real: any };
}

export async function fetchShipTelemetry(): Promise<ShipTelemetry> {
  const now = new Date();
  const [tasks, agents, events, types, proposals] = await Promise.all([
    api.tasks.list(),
    api.agents.list(),
    api.events.list({ limit: EVENT_LIMIT }).catch(() => [] as Event[]),
    api.taskTypes.list().catch(() => []),
    api.proposals.count().catch(() => ({ pending: 0 }) as ProposalCount),
  ]);
  const states = new Map(types.map((tt) => [tt.name, tt.workflow.states]));
  const labelOf = (task: Task, state: string): StateLabel | null => states.get(task.type)?.[state]?.label ?? null;
  const dayStart = today();
  let active = 0, waiting = 0, overdue = 0;
  for (const t of tasks) {
    if (t.state.label === "active") active++;
    else if (t.state.label === "waiting") waiting++;
    if (isOverdueAt(t, t.state.label, dayStart)) overdue++;
  }
  const [replayed, cost] = await Promise.all([Promise.resolve(replaySeries(tasks, events, labelOf, now)), costSeries(events, now)]);
  return {
    active,
    waiting,
    overdue,
    total: tasks.length,
    agentsOnline: agents.filter((a) => a.online).length,
    agentsTotal: agents.length,
    costToday: cost.total,
    costReal: cost.real,
    proposalsPending: proposals.pending ?? 0,
    series: { ...replayed, cost: cost.series },
    at: now.getTime(),
  };
}

// ---------- 模块级订阅：多处订阅只跑一个定时器 ----------
let snapshot: ShipTelemetry | null = null;
let inflight: Promise<void> | null = null;
let timer = 0;
const listeners = new Set<() => void>();

function pull() {
  if (typeof document !== "undefined" && document.hidden) return;
  if (inflight) return;
  inflight = fetchShipTelemetry()
    .then((d) => {
      snapshot = d;
      listeners.forEach((fn) => fn());
    })
    .catch(() => {
      /* 失败时保留上一组读数 */
    })
    .finally(() => {
      inflight = null;
    });
}

const onVisible = () => {
  if (!document.hidden) pull();
};

function subscribe(cb: () => void) {
  listeners.add(cb);
  if (listeners.size === 1) {
    pull();
    timer = window.setInterval(pull, REFRESH_MS);
    document.addEventListener("visibilitychange", onVisible);
  }
  return () => {
    listeners.delete(cb);
    if (listeners.size === 0) {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    }
  };
}
const noop = () => () => {};
const getSnapshot = () => snapshot;
const getServer = () => null;

/** 订阅舰内遥测；live=false（未登录）时不拉。写操作之后可调 refreshShipTelemetry() 立即刷新。 */
export function useShipTelemetry(live: boolean): ShipTelemetry | null {
  const data = useSyncExternalStore(live ? subscribe : noop, getSnapshot, getServer);
  useEffect(() => {
    if (!live) snapshot = null;
  }, [live]);
  return live ? data : null;
}

export function refreshShipTelemetry() {
  pull();
}
