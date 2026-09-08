"use client";
import { useCallback, useMemo, useState, type RefObject } from "react";
import type { ISODate } from "@/lib/api";
import { addDays, diffDays, parseDate, toISODate, today } from "@/lib/format";
import type { GanttHandle } from "./GanttFrame";
import { clampDayW, DAY_MS, isMacro, NAV_STEP_DAYS, SCALE_DAY_W, scaleFor, SCALES, SPAN_DAYS, type Scale } from "./scales";

/*
 * 甘特图的刻度与范围（DESIGN.md §8）：横轴唯一的自由度是 dayW（每天多少像素），档位由它就近推出；
 * 范围（from / to）完全由前端决定，切档时按档位重设并围绕视口中心（今天在视口里时以今天为锚）。
 * 任务甘特图与目标甘特图（ADR 0016）共用这一套：调用方自己 useRef<GanttHandle> 一个 chart，交给 GanttFrame 的 ref，再传进来（ref 不放进返回值，渲染期读返回值才不会被当成读 ref）。
 */

export function defaultRange(): { from: ISODate; to: ISODate } {
  return { from: toISODate(addDays(today(), -30)), to: toISODate(addDays(today(), 30)) };
}

export interface GanttScale {
  dayW: number;
  scale: Scale;
  /** 季 / 年：任务收敛为汇总条 + 密度热度 */
  macro: boolean;
  range: { from: ISODate; to: ISODate };
  from: Date;
  to: Date;
  days: number;
  /** 按钮切档 */
  setScale: (s: Scale) => void;
  /** 滚轮 / 捏合的连续缩放（GanttFrame 的 onZoom） */
  onZoom: (nextDayW: number, anchor: { time: number }) => void;
  zoomStep: (dir: 1 | -1) => void;
  /** 让这些日期全部进入视口（适应全部） */
  fitTo: (dates: Array<ISODate | null | undefined>) => void;
  /** 上一段 / 下一段 */
  shift: (dir: 1 | -1) => void;
  goToday: () => void;
  extend: () => void;
  shorten: () => void;
  canShorten: boolean;
}

/**
 * allowed 是这张图开放的档位（由细到粗），默认六档全开。
 * 路线图只开周 / 月 / 季度 / 年（ADR 0022）：连续缩放也被夹在这四档的范围内，滚轮再怎么放大也到不了天。
 */
export function useGanttScale(chart: RefObject<GanttHandle | null>, initial: Scale = "day", allowed: readonly Scale[] = SCALES): GanttScale {
  const [dayW, setDayW] = useState<number>(SCALE_DAY_W[initial]);
  const finest = allowed[0] ?? SCALES[0];
  const coarsest = allowed[allowed.length - 1] ?? SCALES[SCALES.length - 1];
  const clamp = useCallback((w: number) => Math.min(SCALE_DAY_W[finest], Math.max(SCALE_DAY_W[coarsest], clampDayW(w))), [finest, coarsest]);
  const scale = scaleFor(dayW, allowed);
  const [range, setRange] = useState(defaultRange);

  const from = useMemo(() => parseDate(range.from) ?? today(), [range.from]);
  const to = useMemo(() => parseDate(range.to) ?? addDays(today(), 30), [range.to]);
  const days = Math.max(1, diffDays(from, to) + 1);

  const centerRange = useCallback((s: Scale, centerMs: number) => {
    const spanDays = SPAN_DAYS[s];
    const c = new Date(centerMs);
    c.setHours(0, 0, 0, 0);
    setRange({ from: toISODate(addDays(c, -Math.floor(spanDays / 2))), to: toISODate(addDays(c, Math.ceil(spanDays / 2))) });
  }, []);
  // 按钮切档：今天在视口里就以今天为锚（它留在原位），否则围绕视口中心
  const setScale = useCallback(
    (s: Scale) => {
      if (s === scale && dayW === SCALE_DAY_W[s]) return;
      const now = Date.now();
      let center = chart.current?.centerTime() ?? now;
      if (chart.current?.isVisible(now)) {
        chart.current.anchorAt(now);
        center = now;
      }
      setDayW(SCALE_DAY_W[s]);
      centerRange(s, center);
    },
    [scale, dayW, centerRange, chart],
  );
  const onZoom = useCallback(
    (next: number, anchor: { time: number }) => {
      const w = clamp(next);
      const s = scaleFor(w, allowed);
      setDayW(w);
      if (s !== scaleFor(dayW, allowed)) centerRange(s, anchor.time);
    },
    [dayW, centerRange, clamp, allowed],
  );
  const zoomStep = useCallback(
    (dir: 1 | -1) => {
      const i = allowed.indexOf(scale);
      setScale(allowed[Math.min(allowed.length - 1, Math.max(0, i - dir))]);
    },
    [scale, setScale, allowed],
  );
  const fitTo = useCallback((dates: Array<ISODate | null | undefined>) => {
    const sorted = dates.filter((d): d is string => !!d).sort();
    if (!sorted.length) return;
    const a = parseDate(sorted[0])!;
    const b = parseDate(sorted[sorted.length - 1])!;
    const span = Math.max(7, diffDays(a, b) + 1);
    const pad = Math.max(1, Math.round(span * 0.04));
    const nf = addDays(a, -pad);
    const nt = addDays(b, pad);
    const total = diffDays(nf, nt) + 1;
    const vw = (chart.current?.viewportWidth() ?? 800) - 8;
    setRange({ from: toISODate(nf), to: toISODate(nt) });
    setDayW(clamp(vw / total));
    requestAnimationFrame(() => chart.current?.scrollToTime(nf.getTime(), 0));
  }, [chart, clamp]);
  const shift = useCallback(
    (dir: 1 | -1) => {
      const n = NAV_STEP_DAYS[scale] * dir;
      setRange({ from: toISODate(addDays(from, n)), to: toISODate(addDays(to, n)) });
      requestAnimationFrame(() => chart.current?.scrollToTime(chart.current.centerTime() + n * DAY_MS, 0.5));
    },
    [scale, from, to, chart],
  );
  const goToday = useCallback(() => {
    centerRange(scale, Date.now());
    requestAnimationFrame(() => chart.current?.scrollToTime(Date.now(), 0.3));
  }, [scale, centerRange, chart]);
  const extend = useCallback(() => setRange((r) => ({ from: r.from, to: toISODate(addDays(to, Math.round(SPAN_DAYS[scale] / 2))) })), [to, scale]);
  const canShorten = days > SPAN_DAYS[scale] * 1.5;
  const shorten = useCallback(() => setRange((r) => ({ from: r.from, to: toISODate(addDays(from, SPAN_DAYS[scale])) })), [from, scale]);

  return { dayW, scale, macro: isMacro(scale), range, from, to, days, setScale, onZoom, zoomStep, fitTo, shift, goToday, extend, shorten, canShorten };
}
