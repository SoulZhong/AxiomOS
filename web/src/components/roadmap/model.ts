import { effectivePrecision, type Goal, type GoalDatePrecision, type ISODate, type Session } from "@/lib/api";
import { addDays, monthName, parseDate, startOfWeek, toISODate } from "@/lib/format";
import { t, type Key } from "@/lib/i18n";
import { alpha, C } from "@/components/gantt/GanttFrame";
import type { Scale } from "@/components/gantt/scales";

/*
 * 「目标 · 路线图」时间线的数据侧（ADR 0022）：条画成什么样、泳道怎么分、拖动吸到哪里。
 * 画法与交互在 RoadmapView / RoadmapRow 里，这里只有纯函数，方便单独读与改。
 */

// ---------- 刻度：只开周 / 月 / 季度 / 年，最细到周 ----------

/** 由细到粗的四档（ADR 0022：不开天与小时，那是甘特图的事） */
export const ROADMAP_SCALES: Scale[] = ["week", "month", "quarter", "year"];
export const DEFAULT_SCALE: Scale = "quarter";
export const isRoadmapScale = (v: unknown): v is Scale => typeof v === "string" && (ROADMAP_SCALES as string[]).includes(v);

/** 拖动时吸到哪一档的边界：季度刻度下按月吸，其余按周吸（ADR 0022 交互一条） */
export const SNAP_UNIT: Record<Scale, "week" | "month"> = { hour: "week", day: "week", week: "week", month: "week", quarter: "month", year: "month" };

/** 表头下层一格有多宽：渐隐雾边正好一格（周档 / 月档 / 季度档是一周，年档是一个月） */
export const cellDays = (scale: Scale) => (scale === "year" ? 30 : 7);

/**
 * 在空白处双击新建里程碑时，日期吸到当前刻度**下层**那一格的头上（年档是那个月的 1 号，其余是那一周的周一）。
 * 与拖条的 SNAP_UNIT 不是一回事：那个说的是条要落在哪个格子（季度刻度按月吸），这个说的是"双击的是哪一格"。
 */
export const TIER_UNIT: Record<Scale, "week" | "month"> = { hour: "week", day: "week", week: "week", month: "week", quarter: "week", year: "month" };

// ---------- 日期粒度：吸附与措辞 ----------

const startOfMonth = (d: Date) => new Date(d.getFullYear(), d.getMonth(), 1);
const endOfMonths = (d: Date, months: number) => addDays(new Date(d.getFullYear(), d.getMonth() + months, 1), -1);
/** 粒度对应的格子跨几个月（周不用月算） */
const monthsOf: Record<Exclude<GoalDatePrecision, "week">, number> = { month: 1, quarter: 3, half: 6, year: 12 };

/** 退到所在格子的第一天（与后端 domain.snapStart 同一口径：周从周一起算） */
export function snapStartTo(d: Date, p: GoalDatePrecision): Date {
  if (p === "week") return startOfWeek(d);
  const m = p === "month" ? d.getMonth() : p === "quarter" ? Math.floor(d.getMonth() / 3) * 3 : p === "half" ? (d.getMonth() < 6 ? 0 : 6) : 0;
  return new Date(d.getFullYear(), m, 1);
}
/** 进到所在格子的最后一天（周＝周日） */
export function snapEndTo(d: Date, p: GoalDatePrecision): Date {
  const first = snapStartTo(d, p);
  return p === "week" ? addDays(first, 6) : endOfMonths(first, monthsOf[p]);
}

/** 拖动落点吸到当前刻度的边界：起点退到格子头、终点进到格子尾 */
export function snapDragStart(d: Date, unit: "week" | "month"): Date {
  return unit === "week" ? startOfWeek(d) : startOfMonth(d);
}
export function snapDragEnd(d: Date, unit: "week" | "month"): Date {
  return unit === "week" ? addDays(startOfWeek(d), 6) : endOfMonths(startOfMonth(d), 1);
}

/**
 * 按粒度说一段时间（路线图上不出现某一天）：
 * 到周说「9月8日那一周」，到月说「2026年9月」，到季度说「2026年第4季度」，到半年 / 到年同理。
 * 起止落在同一格时只说一格，跨格时说「A 到 B」。
 */
export function spanWords(start: ISODate | null, end: ISODate | null, p: GoalDatePrecision): string {
  const a = parseDate(start);
  const b = parseDate(end);
  if (!a && !b) return t("roadmap.span.none");
  const one = (d: Date) => cellWords(d, p);
  if (!a) return t("roadmap.span.until", { end: one(b!) });
  if (!b) return t("roadmap.span.from", { start: one(a) });
  const s = one(a);
  const e = one(b);
  return s === e ? s : t("roadmap.span.range", { start: s, end: e });
}

/** 一格的说法 */
export function cellWords(d: Date, p: GoalDatePrecision): string {
  const y = d.getFullYear();
  if (p === "year") return t("gantt.year", { year: y });
  if (p === "half") return t(d.getMonth() < 6 ? "roadmap.cell.half.1" : "roadmap.cell.half", { year: y });
  if (p === "quarter") return t("gantt.hdr.quarter", { year: y, q: Math.floor(d.getMonth() / 3) + 1 });
  if (p === "month") return t("gantt.yearMonth", { year: y, month: monthName(d.getMonth() + 1) });
  const w = startOfWeek(d);
  return t("roadmap.cell.week", { month: monthName(w.getMonth() + 1), day: w.getDate() });
}

// ---------- 条：四种画法 ----------

/**
 * 条的四种画法，把"这个日期有多硬"画出来（ADR 0022）：
 *   hard    到周：硬边实条，两端已吸到周一与周日
 *   fuzzy   到月 / 季度 / 半年 / 年：吸到刻度边界，两端各一格宽渐隐
 *   derived 自己没填、由子目标推出来：整条斜纹 + 标题旁一枚箭头，只有推出来的那一头渐隐
 *   ghost   完全没有日期：沉到「还没排期」，从今天线起一根极淡的虚线
 */
export type BarKind = "hard" | "fuzzy" | "derived" | "ghost";

export interface BarSpan {
  kind: BarKind;
  /** 画条用的起止（已按粒度吸附；服务端给了就用服务端的） */
  start: ISODate | null;
  end: ISODate | null;
  /** 这一端要不要渐隐 */
  fadeStart: boolean;
  fadeEnd: boolean;
  /** 有效粒度 */
  precision: GoalDatePrecision;
  /** 目标自己填了日期吗（拖动时用来判断动的是哪一头） */
  ownStart: ISODate | null;
  ownEnd: ISODate | null;
}

const snapISO = (d: ISODate | null | undefined, p: GoalDatePrecision, which: "start" | "end"): ISODate | null => {
  const dt = parseDate(d);
  if (!dt) return null;
  return toISODate(which === "start" ? snapStartTo(dt, p) : snapEndTo(dt, p));
};

/**
 * 一个目标的条怎么画。服务端会给吸附好的 planned_start_snapped / planned_end_snapped，
 * 没给（老后端、示例数据）时前端按同样的规则自己吸一遍，两边结果一致。
 */
export function barSpan(g: Goal): BarSpan {
  const precision = effectivePrecision(g.date_precision);
  const derivedStart = !!g.derived?.start;
  const derivedEnd = !!g.derived?.end;
  // planned_start / planned_end 在推算的那一头给的是推算值，用 derived 标记把"自己填的"择出来
  const ownStart = derivedStart ? null : g.planned_start ?? null;
  const ownEnd = derivedEnd ? null : g.planned_end ?? null;
  const rawStart = derivedStart ? g.derived_start ?? null : ownStart;
  const rawEnd = derivedEnd ? g.derived_end ?? null : ownEnd;
  const start = g.planned_start_snapped ?? snapISO(rawStart, precision, "start");
  const end = g.planned_end_snapped ?? snapISO(rawEnd, precision, "end");
  if (!start && !end) return { kind: "ghost", start: null, end: null, fadeStart: false, fadeEnd: false, precision, ownStart: null, ownEnd: null };
  if (derivedStart || derivedEnd) return { kind: "derived", start, end, fadeStart: derivedStart, fadeEnd: derivedEnd, precision, ownStart, ownEnd };
  const fuzzy = precision !== "week";
  return { kind: fuzzy ? "fuzzy" : "hard", start, end, fadeStart: fuzzy, fadeEnd: fuzzy, precision, ownStart, ownEnd };
}

/**
 * 同一条泳道里的先后（与后端 goalBefore 同一口径，ADR 0022）：
 *   1. 手工排过的（有排序权重）排在前面，彼此按权重从小到大；
 *   2. 都没排过时，自己填了计划开始的在前（早的在前），没填的沉到最后；
 *   3. 还分不出来就按创建时间。
 * 平时用服务端给的次序就够了；拖过一次之后要立刻看到新次序，才在本地按同一条规则再排一遍。
 */
export function orderGoals(items: readonly Goal[]): Goal[] {
  return [...items].sort((a, b) => {
    const ra = a.rank ?? null;
    const rb = b.rank ?? null;
    if ((ra !== null) !== (rb !== null)) return ra !== null ? -1 : 1;
    if (ra !== null && rb !== null && ra !== rb) return ra - rb;
    const sa = barSpan(a).ownStart;
    const sb = barSpan(b).ownStart;
    if (!!sa !== !!sb) return sa ? -1 : 1;
    if (sa && sb && sa !== sb) return sa < sb ? -1 : 1;
    return a.created_at < b.created_at ? -1 : a.created_at > b.created_at ? 1 : 0;
  });
}

/** 折叠后的父行：跨整棵子树（含里程碑）的汇总区间 */
export function subtreeSpan(g: Goal): { start: ISODate | null; end: ISODate | null } {
  let start: ISODate | null = null;
  let end: ISODate | null = null;
  const take = (a: ISODate | null | undefined, b: ISODate | null | undefined) => {
    if (a && (!start || a < start)) start = a;
    if (b && (!end || b > end)) end = b;
  };
  const walk = (x: Goal) => {
    const s = barSpan(x);
    take(s.start, s.end);
    for (const m of x.milestones ?? []) take(m.due_on, m.due_on);
    (x.children ?? []).forEach(walk);
  };
  walk(g);
  return { start, end };
}

// ---------- 条的颜色：唯一的决策点 ----------

export interface BarTone {
  bg: string;
  border: string;
  done: string;
  text: string;
}

/** 条按什么着色：按状态（默认）或按目标类型（ADR 0023） */
export type ColorBy = "status" | "type";
export const COLOR_BYS: ColorBy[] = ["status", "type"];
export const isColorBy = (v: unknown): v is ColorBy => v === "status" || v === "type";

/** 十六进制色 + 百分比 → 半透明（与 token 的 alpha() 同一写法，深浅主题下都成立） */
const mix = (hex: string, pct: number) => `color-mix(in srgb, ${hex} ${pct}%, transparent)`;

/**
 * 条用什么颜色，只在这一处决定。
 *   按状态（默认）：进行中 / 已达成 / 已放弃三种语气，沿用目标甘特图；
 *   按类型（ADR 0023）：底色取类型自己的颜色，没分类的退回中性色。
 * 无论哪一种，"按进度填充"与三种语气的深浅关系都不变；画条的地方不碰颜色。
 */
export function barTone(g: Goal, summary = false, colorBy: ColorBy = "status"): BarTone {
  if (summary) return { bg: alpha("neutral", 14), border: C.hairlineTertiary, done: alpha("accent", 55), text: C.telemetry };
  if (g.status === "abandoned") return { bg: alpha("neutral", 8), border: alpha("neutral", 45), done: "transparent", text: C.subtle };
  if (colorBy === "type") {
    const c = g.type?.color;
    if (!c) return { bg: alpha("neutral", 12), border: alpha("neutral", 50), done: alpha("neutral", 55), text: C.ink };
    return { bg: mix(c, 14), border: mix(c, 60), done: mix(c, 55), text: C.ink };
  }
  if (g.achieved) return { bg: alpha("success", 14), border: alpha("success", 60), done: alpha("success", 55), text: C.success };
  return { bg: alpha("accent", 14), border: alpha("accent", 60), done: alpha("accent", 55), text: C.ink };
}

/** 信心度圆点的颜色（高 = 成功色、中 = 中性、低 = 警示色，没填不画） */
export const confidenceTone = (g: Goal) => (g.confidence === "high" ? "success" : g.confidence === "medium" ? "neutral" : g.confidence === "low" ? "warning" : null);

// ---------- 泳道：一张描述表，加一档只是往数组里加一项 ----------

/** 一条泳道：id 是分组值，title 是道名 */
export interface Lane {
  id: string;
  title: string;
}

/**
 * 分组方式的描述：`of` 把一个目标映射到它所在的泳道，返回 null 表示这个目标没有这个属性
 * （落到「{分组名}未填」那一道）。工具条上的分段控件直接由这个数组渲染，
 * 以后加「按类型」（ADR 0023）只是再往数组里加一项。
 */
export interface LaneSpec {
  key: string;
  title: string;
  /** null = 不分组，整页一条道 */
  of: ((g: Goal) => Lane | null) | null;
  /** 没有分组值的那一道叫什么 */
  emptyTitle?: string;
  /** 泳道之间的次序（越小越靠上）；没给就按道名排 */
  weight?: (id: string) => number;
}

export function laneSpecs(session: Session | null): LaneSpec[] {
  const teams = session?.teams ?? [];
  const teamIndex = new Map(teams.map((x, i) => [x.id, i]));
  return [
    {
      key: "team",
      title: t("roadmap.group.team"),
      of: (g) => (g.team_id ? { id: g.team_id, title: teams.find((x) => x.id === g.team_id)?.name ?? g.team_id } : null),
      emptyTitle: t("roadmap.group.noTeam"),
      weight: (id) => teamIndex.get(id) ?? 900,
    },
    {
      key: "owner",
      title: t("roadmap.group.owner"),
      of: (g) => ({ id: g.owner.id, title: g.owner.name }),
    },
    {
      key: "horizon",
      title: t("roadmap.group.horizon"),
      of: (g) => (g.horizon ? { id: g.horizon, title: g.horizon_title || t(`horizon.${g.horizon}` as Key) } : null),
      // 与底部那一组区别开：这一道说的是"时间桶这个字段没填"，不是"没有日期"
      emptyTitle: t("roadmap.group.noHorizon"),
      weight: (id) => ["now", "next", "later"].indexOf(id),
    },
    {
      key: "type",
      title: t("roadmap.group.type"),
      of: (g) => (g.type ? { id: g.type.id, title: g.type.name } : null),
      emptyTitle: t("goalType.none"),
    },
    { key: "none", title: t("roadmap.group.none"), of: null },
  ];
}
