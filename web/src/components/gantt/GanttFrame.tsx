"use client";
import { useEffect, useImperativeHandle, useLayoutEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode, type Ref, type RefObject } from "react";
import { diffDays, today } from "@/lib/format";
import { t as tt } from "@/lib/i18n";
import { fadeRepaint } from "@/lib/motion";
import { IconChevronLeft, IconChevronRight } from "@/components/icons";
import { buildHeader, DAY_MS, xOfTime, type Cell, type Header, type Scale } from "./scales";

/*
 * 甘特图的"框"（DESIGN.md §8 / §11）：与"行是什么"无关的那一半——
 *   - 一个两个方向都在内部滚的容器；固定在顶部的两层表头 + 刻度尺 + 今天三角；左上角同时固定在左侧（可拖宽 / 收窄）；
 *   - 铺在图区底下的网格层（周末底纹、竖线、今天线）；
 *   - 缩放锚点（切档 / 滚轮 / 改范围后光标或视口中心下的时间不动）、Ctrl/⌘ + 滚轮与捏合缩放；
 *   - 换刻度 / 换一批数据时一次 200ms 的重绘过渡（lib/motion fadeRepaint）。
 * 行由调用方用 children(ctx) 注入：任务甘特图画任务条（Gantt.tsx），目标甘特图画目标横条 + 里程碑菱形（ADR 0016）。
 * overlay(ctx) 盖在行之上（前置箭头之类）。
 */

export type Density = "compact" | "comfortable";
export const LABEL_MIN = 200;
export const LABEL_MAX = 420;
export const LABEL_NARROW = 56;
/** 表头：上层（0–18）→ 刻度尺（18–28）→ 下层（28–56） */
export const HEADER_H = 56;
const RULER_Y = 28;
export const ROW_H: Record<Density, number> = { compact: 28, comfortable: 36 };
export const BAR_H: Record<Density, number> = { compact: 12, comfortable: 16 };

// 颜色全部来自 DESIGN.md v2 的 token：SVG 里直接引用 globals.css 的原始变量 --c-*，深浅主题下都成立
const v = (token: string) => `var(--c-${token})`;
/** token 的 alpha 变体（语义色三件套 / 成本分位）：用 color-mix 从 token 派生，不写死 rgba */
export const alpha = (token: string, pct: number) => `color-mix(in srgb, var(--c-${token}) ${pct}%, transparent)`;
export const C = {
  accent: v("accent"),
  accentHover: v("accent-hover"),
  danger: v("danger"),
  success: v("success"),
  warning: v("warning"),
  neutral: v("neutral"),
  ink: v("ink"),
  inkMuted: v("ink-muted"),
  subtle: v("ink-subtle"),
  tertiary: v("ink-tertiary"),
  onAccent: v("on-accent"),
  surface1: v("surface-1"),
  surface2: v("surface-2"),
  surface3: v("surface-3"),
  surface4: v("surface-4"),
  hairline: v("hairline"),
  hairlineStrong: v("hairline-strong"),
  hairlineTertiary: v("hairline-tertiary"),
  telemetry: v("telemetry"),
};
export const MONO = "var(--font-jetbrains), 'SF Mono', Menlo, monospace";

/** 估算一段文字在 SVG 里的宽度（中日韩全角按 1em，其余按 0.6em） */
export const textW = (s: string, size = 11) => {
  let w = 0;
  for (const ch of s) w += /[⺀-鿿豈-﫿＀-￯]/.test(ch) ? size : size * 0.6;
  return w;
};
export function clipToWidth(s: string, maxPx: number, size = 11): string {
  if (textW(s, size) <= maxPx) return s;
  let out = "";
  let w = 0;
  for (const ch of s) {
    const cw = /[⺀-鿿豈-﫿＀-￯]/.test(ch) ? size : size * 0.6;
    if (w + cw + size > maxPx) break;
    out += ch;
    w += cw;
  }
  return out ? `${out}…` : "";
}

export interface GanttHandle {
  /** 把某个时间点滚到视口的 at（0–1）处 */
  scrollToTime: (ms: number, at?: number) => void;
  /** 视口中心对应的时间 */
  centerTime: () => number;
  /** 某个时间点是否在视口里 */
  isVisible: (ms: number) => boolean;
  /** 下一次换刻度 / 范围时，让这个时间点留在它现在的位置（切档按钮以今天为锚） */
  anchorAt: (ms: number) => void;
  /** 图区视口宽度（不含左列） */
  viewportWidth: () => number;
}

/** 给行渲染用的上下文：图区宽度、视口、表头格子、今天线位置、当前时间。 */
export interface GanttFrameCtx {
  /** 图区宽度（不含左列）= days × dayW */
  width: number;
  /** 实际左列宽（收窄时是 LABEL_NARROW） */
  labelW: number;
  /** 滚动位置与视口尺寸（rAF 节流） */
  view: { left: number; top: number; w: number; h: number };
  header: Header;
  todayX: number;
  todayVisible: boolean;
  /** 当前时间戳（时档每分钟刷新） */
  now: number;
  /** 日期 / 时间戳 → x */
  xOfTime: (ms: number) => number;
}

export interface GanttFrameProps {
  ref?: Ref<GanttHandle>;
  from: Date;
  days: number;
  dayW: number;
  scale: Scale;
  /** 滚轮 / 捏合缩放：anchor 是光标下的时间与它在视口里的横向位置 */
  onZoom: (nextDayW: number, anchor: { time: number; px: number }) => void;
  /** 左列宽度（未收窄时的持久值）与收窄开关 */
  labelW: number;
  narrow: boolean;
  onLabelW: (w: number) => void;
  onNarrow: (narrow: boolean) => void;
  density: Density;
  /** 图区正文的最小高度（行数 × 行高），给虚拟化占位 */
  bodyH: number;
  /** 图区允许拖拽新建时给十字光标 */
  creatable?: boolean;
  /** 表头的最小格：默认到天；路线图传 "week"（它永远不画某一天，ADR 0022） */
  minUnit?: "day" | "week";
  scrollRef: RefObject<HTMLDivElement | null>;
  /** 左上角（左列表头）的内容：通常是一行等宽读数 */
  corner?: ReactNode;
  /** 这个值变化时播一次 200ms 的重绘过渡（换刻度 / 换一批数据） */
  repaintKey?: unknown;
  /** 行 */
  children: (ctx: GanttFrameCtx) => ReactNode;
  /** 盖在行之上的一层（前置箭头等） */
  overlay?: (ctx: GanttFrameCtx) => ReactNode;
}

export function GanttFrame({ ref, from, days, dayW, scale, onZoom, labelW: labelWProp, narrow, onLabelW, onNarrow, density, bodyH, creatable, minUnit = "day", scrollRef, corner, repaintKey, children, overlay }: GanttFrameProps) {
  const width = Math.max(1, days * dayW);
  const labelW = narrow ? LABEL_NARROW : Math.min(LABEL_MAX, Math.max(LABEL_MIN, labelWProp));
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (scale !== "hour") return;
    const id = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(id);
  }, [scale]);
  const todayX = scale === "hour" ? xOfTime(from, now, dayW) : diffDays(from, today()) * dayW + dayW / 2;
  const todayVisible = todayX >= 0 && todayX <= width;

  // ---------- 视口状态：滚动位置与尺寸（rAF 节流），给表头的"跟随滚动的大单位标签"与行虚拟化用 ----------
  const [view, setView] = useState({ left: 0, top: 0, w: 0, h: 0 });
  const scrollPos = useRef({ left: 0, top: 0 });
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    let raf = 0;
    const sync = () => {
      raf = 0;
      scrollPos.current = { left: el.scrollLeft, top: el.scrollTop };
      setView((p) => (p.left === el.scrollLeft && p.top === el.scrollTop && p.w === el.clientWidth && p.h === el.clientHeight ? p : { left: el.scrollLeft, top: el.scrollTop, w: el.clientWidth, h: el.clientHeight }));
    };
    const onScroll = () => {
      scrollPos.current = { left: el.scrollLeft, top: el.scrollTop };
      if (!raf) raf = requestAnimationFrame(sync);
    };
    sync();
    el.addEventListener("scroll", onScroll, { passive: true });
    const ro = new ResizeObserver(() => onScroll());
    ro.observe(el);
    return () => {
      el.removeEventListener("scroll", onScroll);
      ro.disconnect();
      if (raf) cancelAnimationFrame(raf);
    };
  }, [scrollRef]);

  // ---------- 缩放锚点：切档 / 缩放 / 改范围后保持光标（或视口中心）下的时间不动 ----------
  const anchorRef = useRef<{ time: number; px: number } | null>(null);
  const prevLayout = useRef({ from: from.getTime(), dayW });
  const firstLayout = useRef(true);
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const chartViewW = () => el.clientWidth - labelW;
    if (firstLayout.current) {
      firstLayout.current = false;
      // 首屏：今天放在视口 30% 处
      const x = scale === "hour" ? xOfTime(from, Date.now(), dayW) : diffDays(from, today()) * dayW;
      el.scrollLeft = Math.max(0, x - chartViewW() * 0.3);
      prevLayout.current = { from: from.getTime(), dayW };
      return;
    }
    const prev = prevLayout.current;
    if (prev.from === from.getTime() && prev.dayW === dayW) return;
    let anchor = anchorRef.current;
    anchorRef.current = null;
    if (!anchor) {
      // 没有显式锚点（按钮切档、改范围）：保持视口中心的时间
      const px = chartViewW() / 2;
      anchor = { time: prev.from + ((scrollPos.current.left + px) / prev.dayW) * DAY_MS, px };
    }
    el.scrollLeft = Math.max(0, xOfTime(from, anchor.time, dayW) - anchor.px);
    scrollPos.current.left = el.scrollLeft;
    prevLayout.current = { from: from.getTime(), dayW };
  }, [from, dayW, labelW, scale, scrollRef]);

  useImperativeHandle(
    ref,
    () => ({
      scrollToTime: (ms, at = 0.3) => {
        const el = scrollRef.current;
        if (!el) return;
        anchorRef.current = null;
        el.scrollLeft = Math.max(0, xOfTime(from, ms, dayW) - (el.clientWidth - labelW) * at);
        scrollPos.current.left = el.scrollLeft;
      },
      centerTime: () => {
        const el = scrollRef.current;
        const px = el ? (el.clientWidth - labelW) / 2 : 400;
        return from.getTime() + ((scrollPos.current.left + px) / dayW) * DAY_MS;
      },
      isVisible: (ms) => {
        const el = scrollRef.current;
        if (!el) return false;
        const px = xOfTime(from, ms, dayW) - scrollPos.current.left;
        return px >= 0 && px <= el.clientWidth - labelW;
      },
      anchorAt: (ms) => {
        anchorRef.current = { time: ms, px: xOfTime(from, ms, dayW) - scrollPos.current.left };
      },
      viewportWidth: () => {
        const el = scrollRef.current;
        return el ? el.clientWidth - labelW : 800;
      },
    }),
    [from, dayW, labelW, scrollRef],
  );

  // Ctrl/⌘ + 滚轮与触控板捏合（浏览器把捏合报成 ctrlKey 的 wheel）：围绕光标缩放；要 preventDefault 就得非 passive
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return;
      e.preventDefault();
      const rect = el.getBoundingClientRect();
      const px = e.clientX - rect.left - labelW;
      const time = from.getTime() + ((el.scrollLeft + px) / dayW) * DAY_MS;
      const factor = Math.exp(-e.deltaY * (Math.abs(e.deltaY) >= 50 ? 0.004 : 0.01));
      anchorRef.current = { time, px };
      onZoom(dayW * factor, { time, px });
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [from, dayW, labelW, onZoom, scrollRef]);

  // ---------- 表头 ----------
  const header = useMemo(() => buildHeader(scale, from, days, dayW, new Date(now), minUnit), [scale, from, days, dayW, now, minUnit]);

  // ---------- 左列拖宽 ----------
  const resize = useRef<{ x: number; w: number } | null>(null);
  const onResizeDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (narrow) return;
    resize.current = { x: e.clientX, w: labelWProp };
    (e.currentTarget as Element).setPointerCapture?.(e.pointerId);
  };
  const onResizeMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!resize.current) return;
    onLabelW(Math.round(Math.min(LABEL_MAX, Math.max(LABEL_MIN, resize.current.w + e.clientX - resize.current.x))));
  };
  const onResizeUp = () => {
    resize.current = null;
  };

  // 切档 / 换一批数据时整张图会重排：一次 200ms 的重绘过渡（不重挂、不丢滚动位置）。首次渲染不播；连续缩放（dayW 变但档位没变）不播。
  const panelRef = useRef<HTMLDivElement>(null);
  const firstPaint = useRef(true);
  useEffect(() => {
    if (firstPaint.current) {
      firstPaint.current = false;
      return;
    }
    const anim = fadeRepaint(panelRef.current);
    return () => anim?.cancel();
  }, [scale, repaintKey]);

  const totalW = labelW + width;
  const topTierLabelX = (c: Cell) => {
    // 大单位标签跟着横向滚动走：贴在格子左缘与视口左缘中较右的一处，但不超出格子右缘
    const tw = textW(c.label) + 12;
    return Math.min(c.x + c.w - tw, Math.max(c.x, view.left)) + 6;
  };
  const bottomFont = header.bottom[0]?.w && header.bottom[0].w < 18 ? 9.5 : header.bottom[0]?.w && header.bottom[0].w < 30 ? 10 : 11;
  const ctx: GanttFrameCtx = { width, labelW, view, header, todayX, todayVisible, now, xOfTime: (ms) => xOfTime(from, ms, dayW) };

  return (
    <div ref={panelRef} className="gantt-panel">
      <div ref={scrollRef} className={`gantt-scroll gantt-drag ${creatable ? "gantt-creatable" : ""}`} data-density={density}>
        {/* 表头：固定在顶部；左上角同时固定在左侧 */}
        <div className="gantt-head" style={{ width: totalW, height: HEADER_H }}>
          <div className="gantt-corner" style={{ width: labelW }}>
            {narrow ? (
              <button type="button" className="gantt-narrow-btn pressable" onClick={() => onNarrow(false)} title={tt("gantt.labelWide")} aria-label={tt("gantt.labelWide")}>
                <IconChevronRight />
              </button>
            ) : (
              <>
                {corner}
                <button type="button" className="gantt-narrow-btn pressable" onClick={() => onNarrow(true)} title={tt("gantt.labelNarrow")} aria-label={tt("gantt.labelNarrow")}>
                  <IconChevronLeft />
                </button>
              </>
            )}
            {!narrow && (
              <div className="gantt-resize" style={{ height: Math.max(view.h, HEADER_H) }} onPointerDown={onResizeDown} onPointerMove={onResizeMove} onPointerUp={onResizeUp} onPointerCancel={onResizeUp} role="separator" aria-orientation="vertical" aria-label={tt("gantt.labelResize")} aria-valuemin={LABEL_MIN} aria-valuemax={LABEL_MAX} aria-valuenow={labelWProp} title={tt("gantt.labelResize")} />
            )}
          </div>
          <svg width={width} height={HEADER_H} className="gantt-head-svg" fontFamily="inherit" aria-hidden="true">
            <rect x={0} y={0} width={width} height={HEADER_H} fill={C.surface2} />
            {header.top.map((c, i) => (
              <g key={`top${i}`}>
                <line x1={c.x + 0.5} y1={0} x2={c.x + 0.5} y2={HEADER_H} stroke={C.hairlineStrong} />
                {c.w > 24 && (
                  <text x={topTierLabelX(c)} y={13} fontSize={11} fontWeight={500} fill={c.today ? C.ink : C.subtle} letterSpacing="0.6" style={{ fontFamily: MONO, textTransform: "uppercase" }}>
                    {c.w > textW(c.label) + 8 ? c.label : clipToWidth(c.label, c.w - 8)}
                  </text>
                )}
              </g>
            ))}
            {/* 刻度尺：每个小单位一道细刻度，大单位一道长刻度 */}
            <g pointerEvents="none">
              <line x1={0} y1={RULER_Y - 0.5} x2={width} y2={RULER_Y - 0.5} stroke={C.hairlineStrong} />
              {header.ruler.map((r, i) => (
                <line key={`rl${i}`} x1={r.x + 0.5} y1={RULER_Y - (r.major ? 8 : 3)} x2={r.x + 0.5} y2={RULER_Y} stroke={r.major ? C.hairlineTertiary : C.hairlineStrong} />
              ))}
            </g>
            {header.bottom.map((c, i) => (
              <g key={`bot${i}`}>
                {c.weekend && <rect x={c.x} y={RULER_Y} width={c.w} height={HEADER_H - RULER_Y} fill={C.surface3} />}
                {(scale !== "day" || c.major) && c.w >= 6 && <line x1={c.x + 0.5} y1={RULER_Y} x2={c.x + 0.5} y2={HEADER_H} stroke={c.major ? C.hairlineStrong : C.hairline} />}
                {c.today ? (
                  <>
                    <rect x={c.x + Math.max(1, Math.min(3, (c.w - 18) / 2))} y={33} width={Math.max(14, c.w - 2 * Math.max(1, Math.min(3, (c.w - 18) / 2)))} height={18} rx={4} fill={C.accent} />
                    <text x={c.x + c.w / 2} y={46} fontSize={bottomFont} fontWeight={500} fill={C.onAccent} textAnchor="middle" style={{ fontFamily: MONO }}>
                      {c.w >= 14 ? c.label : ""}
                    </text>
                  </>
                ) : (
                  <text x={c.x + c.w / 2} y={46} fontSize={bottomFont} fill={c.weekend ? C.tertiary : C.subtle} textAnchor="middle" style={{ fontFamily: MONO }}>
                    {c.w >= textW(c.label, bottomFont) + 2 ? c.label : ""}
                  </text>
                )}
              </g>
            ))}
            <line x1={0} y1={HEADER_H - 0.5} x2={width} y2={HEADER_H - 0.5} stroke={C.hairline} />
            {/* 今天 / 现在：表头上的小三角 */}
            {todayVisible && <polygon points={`${todayX - 4},${HEADER_H - 6} ${todayX + 4},${HEADER_H - 6} ${todayX},${HEADER_H - 1}`} fill={C.accent} />}
          </svg>
        </div>

        {/* 图区：网格层 → 行 → 覆盖层 */}
        <div className="gantt-body" style={{ width: totalW, minHeight: bodyH }}>
          <svg className="gantt-grid" style={{ left: labelW, width }} aria-hidden="true">
            {header.weekends.map((w, i) => (
              <rect key={`we${i}`} x={w.x} y={0} width={w.w} height="100%" fill={C.ink} opacity={0.025} />
            ))}
            {header.columns.map((c, i) => (
              <line key={`vl${i}`} x1={c.x + 0.5} y1={0} x2={c.x + 0.5} y2="100%" stroke={c.major ? C.hairlineStrong : C.hairline} />
            ))}
            {todayVisible && <line x1={todayX} y1={0} x2={todayX} y2="100%" stroke={C.accent} strokeWidth={1.5} />}
          </svg>
          {children(ctx)}
          {overlay?.(ctx)}
        </div>
      </div>
    </div>
  );
}
