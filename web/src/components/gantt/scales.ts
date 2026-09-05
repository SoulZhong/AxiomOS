import { t, type Key } from "@/lib/i18n";
import { addDays, diffDays, monthName, startOfWeek } from "@/lib/format";

/*
 * 甘特图 v2 的六档刻度（DESIGN.md §8）。横轴的唯一自由度是"每天多少像素"（dayW）：
 * 按钮切档把 dayW 设成档位的标准值；Ctrl/⌘ + 滚轮与捏合连续缩放 dayW，档位（表头两层怎么画）由 dayW 就近推出。
 *
 *   档位   px/天   最小格        上层 / 下层
 *   时      624    时 26px       日 / 时
 *   日       34    日 34px       月 / 日
 *   周       16    日 16px       周 / 日
 *   月        6    周 42px       月 / 周
 *   季      3.5    周 24.5px     季 / 周
 *   年      1.4    月 ≈42px      年 / 月
 */
export type Scale = "hour" | "day" | "week" | "month" | "quarter" | "year";
export const SCALES: Scale[] = ["hour", "day", "week", "month", "quarter", "year"];
export const SCALE_DAY_W: Record<Scale, number> = { hour: 624, day: 34, week: 16, month: 6, quarter: 3.5, year: 1.4 };
export const MIN_DAY_W = 0.6;
export const MAX_DAY_W = 1600;
/** 切档时把可见范围重设为这么多天（居中于当前视口中心），横向总宽都在 2–2.5k px 上下 */
export const SPAN_DAYS: Record<Scale, number> = { hour: 4, day: 60, week: 120, month: 365, quarter: 730, year: 1825 };
/** 「上一段 / 下一段」每次移动的天数 */
export const NAV_STEP_DAYS: Record<Scale, number> = { hour: 1, day: 14, week: 30, month: 91, quarter: 365, year: 1095 };

export const scaleTitle = (s: Scale) => t(`gantt.zoom.${s}`);
/** 季 / 年：任务收敛为汇总条 + 密度热度 */
export const isMacro = (s: Scale) => s === "quarter" || s === "year";

/** 由连续的 dayW 推出档位：对数空间里取最近的标准值 */
export function scaleFor(dayW: number): Scale {
  let best: Scale = "day";
  let bestD = Infinity;
  for (const s of SCALES) {
    const d = Math.abs(Math.log(dayW) - Math.log(SCALE_DAY_W[s]));
    if (d < bestD) {
      bestD = d;
      best = s;
    }
  }
  return best;
}

export const clampDayW = (w: number) => Math.min(MAX_DAY_W, Math.max(MIN_DAY_W, w));

export interface Cell {
  x: number;
  w: number;
  label: string;
  /** 下层：周末列（日 / 周档） */
  weekend?: boolean;
  /** 下层：今天 / 当前小时所在格 */
  today?: boolean;
  /** 起点是大单位（月 / 日）的边界：竖线画得更实 */
  major?: boolean;
}
export interface Tick {
  x: number;
  major: boolean;
}
export interface Header {
  top: Cell[];
  bottom: Cell[];
  /** 刻度尺：细 / 长刻度 */
  ruler: Tick[];
  /** 图区的竖线（下层格的边界） */
  columns: Tick[];
  /** 周末底纹（日 / 周档） */
  weekends: Array<{ x: number; w: number }>;
}

const HOUR = 3600_000;
const DAY = 86400_000;

/** 生成两层表头 + 刻度尺 + 图区竖线。from 是范围起点（当天 0 点），days 是范围天数。 */
export function buildHeader(scale: Scale, from: Date, days: number, dayW: number, now: Date): Header {
  const top: Cell[] = [];
  const bottom: Cell[] = [];
  const ruler: Tick[] = [];
  const columns: Tick[] = [];
  const weekends: Array<{ x: number; w: number }> = [];
  const width = days * dayW;
  const to = addDays(from, days); // 开区间终点
  const todayIdx = diffDays(from, new Date(now.getFullYear(), now.getMonth(), now.getDate()));
  const xOfDay = (i: number) => i * dayW;
  const clampCell = (a: number, b: number) => [Math.max(0, a), Math.min(width, b)] as const;

  const months = (): Cell[] => {
    const out: Cell[] = [];
    let cur = new Date(from.getFullYear(), from.getMonth(), 1);
    while (cur < to) {
      const next = new Date(cur.getFullYear(), cur.getMonth() + 1, 1);
      const [a, b] = clampCell(xOfDay(diffDays(from, cur)), xOfDay(diffDays(from, next)));
      out.push({ x: a, w: b - a, label: t("gantt.yearMonth", { year: cur.getFullYear(), month: monthName(cur.getMonth() + 1) }), major: true });
      cur = next;
    }
    return out;
  };
  const weeks = (labelOf: (monday: Date) => string): Cell[] => {
    const out: Cell[] = [];
    let w = startOfWeek(from);
    while (w < to) {
      const n = addDays(w, 7);
      const [a, b] = clampCell(xOfDay(diffDays(from, w)), xOfDay(diffDays(from, n)));
      out.push({ x: a, w: b - a, label: labelOf(w), major: w.getDate() === 1, today: todayIdx >= diffDays(from, w) && todayIdx < diffDays(from, n) });
      w = n;
    }
    return out;
  };
  const dayCells = (): Cell[] => {
    const out: Cell[] = [];
    for (let i = 0; i < days; i++) {
      const d = addDays(from, i);
      const wd = d.getDay();
      const weekend = wd === 0 || wd === 6;
      out.push({ x: xOfDay(i), w: dayW, label: String(d.getDate()), weekend, today: i === todayIdx, major: d.getDate() === 1 });
      if (weekend) weekends.push({ x: xOfDay(i), w: dayW });
    }
    return out;
  };

  if (scale === "hour") {
    const hw = dayW / 24;
    for (let i = 0; i < days; i++) {
      const d = addDays(from, i);
      top.push({ x: xOfDay(i), w: dayW, label: t("gantt.hdr.day", { month: monthName(d.getMonth() + 1), day: d.getDate(), weekday: t(`gantt.weekday.${d.getDay()}` as Key) }), major: true, today: i === todayIdx });
      const wd = d.getDay();
      if (wd === 0 || wd === 6) weekends.push({ x: xOfDay(i), w: dayW });
      for (let h = 0; h < 24; h++) {
        const x = xOfDay(i) + h * hw;
        const isNowHour = i === todayIdx && h === now.getHours();
        bottom.push({ x, w: hw, label: String(h).padStart(2, "0"), today: isNowHour, major: h === 0 });
        ruler.push({ x, major: h % 6 === 0 });
        if (hw >= 18 || h % 6 === 0) columns.push({ x, major: h === 0 });
      }
    }
  } else if (scale === "day") {
    top.push(...months());
    bottom.push(...dayCells());
    for (let i = 0; i < days; i++) {
      const d = addDays(from, i);
      ruler.push({ x: xOfDay(i), major: d.getDay() === 1 });
      columns.push({ x: xOfDay(i), major: d.getDate() === 1 });
    }
  } else if (scale === "week") {
    top.push(...weeks((m) => t("gantt.hdr.week", { start: `${m.getMonth() + 1}/${m.getDate()}`, end: `${addDays(m, 6).getMonth() + 1}/${addDays(m, 6).getDate()}` })));
    bottom.push(...dayCells());
    for (let i = 0; i < days; i++) {
      const d = addDays(from, i);
      ruler.push({ x: xOfDay(i), major: d.getDay() === 1 });
      columns.push({ x: xOfDay(i), major: d.getDate() === 1 });
    }
  } else if (scale === "month") {
    top.push(...months());
    bottom.push(...weeks((m) => `${m.getMonth() + 1}/${m.getDate()}`));
    for (let i = 0; i < days; i++) {
      const d = addDays(from, i);
      if (d.getDay() === 1 || d.getDate() === 1) {
        ruler.push({ x: xOfDay(i), major: d.getDate() === 1 });
        columns.push({ x: xOfDay(i), major: d.getDate() === 1 });
      } else if (dayW >= 5) ruler.push({ x: xOfDay(i), major: false });
    }
  } else if (scale === "quarter") {
    let cur = new Date(from.getFullYear(), Math.floor(from.getMonth() / 3) * 3, 1);
    while (cur < to) {
      const next = new Date(cur.getFullYear(), cur.getMonth() + 3, 1);
      const [a, b] = clampCell(xOfDay(diffDays(from, cur)), xOfDay(diffDays(from, next)));
      top.push({ x: a, w: b - a, label: t("gantt.hdr.quarter", { year: cur.getFullYear(), q: Math.floor(cur.getMonth() / 3) + 1 }), major: true });
      cur = next;
    }
    // 下层是周：含月初的那一周标月份名，其余标周一的日号
    bottom.push(
      ...weeks((m) => {
        const end = addDays(m, 6);
        if (m.getDate() === 1) return monthName(m.getMonth() + 1);
        if (end.getMonth() !== m.getMonth()) return monthName(end.getMonth() + 1);
        return String(m.getDate());
      }),
    );
    for (let i = 0; i < days; i++) {
      const d = addDays(from, i);
      if (d.getDate() === 1) {
        ruler.push({ x: xOfDay(i), major: true });
        columns.push({ x: xOfDay(i), major: true });
      } else if (d.getDay() === 1) {
        ruler.push({ x: xOfDay(i), major: false });
        columns.push({ x: xOfDay(i), major: false });
      }
    }
  } else {
    let cur = new Date(from.getFullYear(), 0, 1);
    while (cur < to) {
      const next = new Date(cur.getFullYear() + 1, 0, 1);
      const [a, b] = clampCell(xOfDay(diffDays(from, cur)), xOfDay(diffDays(from, next)));
      top.push({ x: a, w: b - a, label: t("gantt.year", { year: cur.getFullYear() }), major: true });
      cur = next;
    }
    let mm = new Date(from.getFullYear(), from.getMonth(), 1);
    while (mm < to) {
      const next = new Date(mm.getFullYear(), mm.getMonth() + 1, 1);
      const [a, b] = clampCell(xOfDay(diffDays(from, mm)), xOfDay(diffDays(from, next)));
      bottom.push({ x: a, w: b - a, label: monthName(mm.getMonth() + 1), major: mm.getMonth() === 0, today: todayIdx >= diffDays(from, mm) && todayIdx < diffDays(from, next) });
      mm = next;
    }
    for (let i = 0; i < days; i++) {
      const d = addDays(from, i);
      if (d.getDate() === 1) {
        ruler.push({ x: xOfDay(i), major: d.getMonth() % 3 === 0 });
        columns.push({ x: xOfDay(i), major: d.getMonth() === 0 });
      } else if (d.getDay() === 1 && dayW >= 2) ruler.push({ x: xOfDay(i), major: false });
    }
  }
  return { top, bottom, ruler, columns, weekends };
}

/** 时间点 → x：日期（00:00）与时间戳都支持；时档按小时精确，其余档位按天 */
export function xOfTime(from: Date, ms: number, dayW: number): number {
  return ((ms - from.getTime()) / DAY) * dayW;
}
export const HOUR_MS = HOUR;
export const DAY_MS = DAY;
