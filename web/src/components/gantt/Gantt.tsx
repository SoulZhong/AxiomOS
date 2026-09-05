"use client";
import Link from "next/link";
import { memo, useCallback, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent, type Ref, type RefObject, type TransitionEvent as ReactTransitionEvent } from "react";
import type { GanttData, GanttTask, ISODate, Run, StateLabel } from "@/lib/api";
import { isTerminal } from "@/lib/api";
import { addDays, parseDate, toISODate, today } from "@/lib/format";
import { stateLabel } from "@/lib/terms";
import { t as tt } from "@/lib/i18n";
import { Avatar } from "@/components/ui";
import { IconChevronRight } from "@/components/icons";
import { alpha, BAR_H, C, clipToWidth, GanttFrame, LABEL_MAX, LABEL_MIN, LABEL_NARROW, MONO, ROW_H, textW, type Density, type GanttFrameCtx, type GanttHandle } from "./GanttFrame";
import { MilestoneMarks } from "./MilestoneMarks";
import { isMacro, xOfTime, type Cell, type Scale } from "./scales";

/*
 * 任务甘特图（DESIGN.md §8「宏观到微观」+ §Axiom 9「航线图」）：只负责"行是什么"——
 *   - 每个分组一行"组行"（航段 / 汇总条 / 密度热度），下面一个 .fold 折叠区装任务行（200ms / 150ms 高度过渡，沿用目标树），
 *   - 每一行 = 固定在左侧的标签格 + 一张与图区同宽的小 SVG（条、执行记录段），
 *   - 盖在上面的前置箭头层、横向拖拽新建。
 * 表头、刻度尺、今天线、网格、缩放锚点、左列拖宽都在 GanttFrame 里；目标甘特图（ADR 0016）复用同一个框。
 * 行数超过 150 时只渲染视口附近的行（上下各 600px），其余用占位高度顶着。
 */
export type { Density, GanttHandle } from "./GanttFrame";
export { LABEL_MAX, LABEL_MIN, LABEL_NARROW } from "./GanttFrame";
export { defaultRange } from "./useGanttScale";
export const UNSCHEDULED_KEY = "__unscheduled";
const ACTUAL_H = 4;
const VIRTUAL_AT = 150;
const OVERSCAN = 600;

/** 任务条按状态类型上色（三件套：底 / 边 / 字）：未开始=中性，进行中=强调，等待中=警示，已完成=成功，已终止=中性偏暗 */
export const STATE_FILL: Record<StateLabel, { bg: string; border: string; text: string }> = {
  pending: { bg: alpha("neutral", 18), border: alpha("neutral", 40), text: C.inkMuted },
  active: { bg: alpha("accent", 38), border: alpha("accent", 70), text: C.ink },
  waiting: { bg: alpha("warning", 18), border: alpha("warning", 45), text: C.warning },
  terminal_success: { bg: alpha("success", 18), border: alpha("success", 45), text: C.success },
  terminal_failure: { bg: alpha("neutral", 10), border: alpha("neutral", 25), text: C.subtle },
};
/** 状态点（窄列 / 行首）：与状态徽标同一套语义色 */
const STATE_DOT: Record<StateLabel, string> = { pending: C.neutral, active: C.accent, waiting: C.warning, terminal_success: C.success, terminal_failure: C.neutral };
/** 成本高亮：按四分位由浅到深（accent 系） */
const COST_FILL = [alpha("accent", 16), alpha("accent", 34), alpha("accent", 62), C.accent];
const COST_STROKE = { low: alpha("accent", 50), high: alpha("accent", 90) };
/** 甘特页图例里"成本"的样例色块 */
export const COST_LEGEND = { bg: COST_STROKE.low, border: COST_STROKE.high };
/** 执行记录段：Agent = accent，人 = ink-muted（与实际条同一套语言） */
export const RUN_FILL = { agent: C.accent, member: C.inkMuted };
/** 汇总条：底 / 已完成段 */
export const SUMMARY_FILL = { bg: alpha("neutral", 14), border: C.hairlineTertiary, done: alpha("accent", 55) };
/** 密度热度：0–5 个任务 → 由无到深 */
const densityFill = (n: number) => (n <= 0 ? "transparent" : alpha("accent", Math.min(5, n) * 7 + 3));

export interface CreateDraft {
  rowKey: string;
  start: ISODate;
  end: ISODate;
}

/** 一行分组：后端的分组 + 从各组抽出来的「未排期」（两头日期都没有的任务） */
export interface GanttGroupModel {
  key: string;
  title: string;
  goal: GanttData["rows"][number]["goal"];
  tasks: GanttTask[];
  unscheduled: boolean;
  /** 未排期组里：任务原来所在分组的标题 */
  origin?: Map<string, string>;
}

export function buildGroups(data: GanttData): GanttGroupModel[] {
  const out: GanttGroupModel[] = [];
  const loose: GanttTask[] = [];
  const origin = new Map<string, string>();
  for (const r of data.rows) {
    const scheduled: GanttTask[] = [];
    for (const t of r.tasks) {
      if (t.planned_start || t.planned_end) scheduled.push(t);
      else {
        loose.push(t);
        origin.set(t.id, r.title);
      }
    }
    out.push({ key: r.key, title: r.title, goal: r.goal, tasks: scheduled, unscheduled: false });
  }
  if (loose.length) out.push({ key: UNSCHEDULED_KEY, title: tt("gantt.unscheduled"), goal: null, tasks: loose, unscheduled: true, origin });
  return out;
}

interface Props {
  ref?: Ref<GanttHandle>;
  data: GanttData;
  groups: GanttGroupModel[];
  from: Date;
  days: number;
  dayW: number;
  scale: Scale;
  /** 滚轮 / 捏合缩放：anchor 是光标下的时间与它在视口里的横向位置 */
  onZoom: (nextDayW: number, anchor: { time: number; px: number }) => void;
  showCost: boolean;
  showOverdue: boolean;
  onCreate?: (draft: CreateDraft) => void;
  /** 任务 id → 执行记录（时档显示） */
  runs: Record<string, Run[]>;
  collapsed: Set<string>;
  onToggle: (key: string) => void;
  density: Density;
  labelW: number;
  narrow: boolean;
  onLabelW: (w: number) => void;
  onNarrow: (narrow: boolean) => void;
  scrollRef: RefObject<HTMLDivElement | null>;
}

type Row = { kind: "group"; key: string; g: GanttGroupModel; open: boolean } | { kind: "task"; key: string; g: GanttGroupModel; t: GanttTask };
type Span = [number, number];
interface Summary {
  span: Span | null;
  ratio: number;
  total: number;
  done: number;
  overdue: number;
}
interface Drag {
  rowKey: string;
  x0: number;
  x1: number;
}

export function Gantt({ ref, data, groups, from, days, dayW, scale, onZoom, showCost, showOverdue, onCreate, runs, collapsed, onToggle, density, labelW: labelWProp, narrow, onLabelW, onNarrow, scrollRef }: Props) {
  const rowH = ROW_H[density];
  const barH = BAR_H[density];
  const width = Math.max(1, days * dayW);
  const macro = isMacro(scale);
  // 与 GanttFrame 里的算法一致：收窄 = 56px，否则取持久值并夹在 [200, 420]
  const labelW = narrow ? LABEL_NARROW : Math.min(LABEL_MAX, Math.max(LABEL_MIN, labelWProp));

  // ---------- 行 ----------
  const rows = useMemo<Row[]>(() => {
    const out: Row[] = [];
    for (const g of groups) {
      const open = !macro && !collapsed.has(g.key);
      out.push({ kind: "group", key: `g:${g.key}`, g, open });
      if (open) for (const t of g.tasks) out.push({ kind: "task", key: `t:${g.key}:${t.id}`, g, t });
    }
    return out;
  }, [groups, collapsed, macro]);
  const totalRows = rows.length;
  // 每个组在可见行里的序号（组行）与它第一条任务行的序号，给虚拟化用
  const offsets = useMemo(() => {
    const out: Array<{ groupIndex: number; first: number }> = [];
    let i = 0;
    for (const g of groups) {
      out.push({ groupIndex: i, first: i + 1 });
      i += 1 + (!macro && !collapsed.has(g.key) ? g.tasks.length : 0);
    }
    return out;
  }, [groups, collapsed, macro]);
  const bodyH = totalRows * rowH;
  const virtual = totalRows > VIRTUAL_AT;

  // 成本分位：0–3 档
  const costLevel = useMemo(() => {
    const costs = data.rows.flatMap((r) => r.tasks.map((t) => t.cost)).filter((c) => c > 0).sort((a, b) => a - b);
    const q = (p: number) => costs[Math.min(costs.length - 1, Math.floor(costs.length * p))] ?? 0;
    const [q1, q2, q3] = [q(0.25), q(0.5), q(0.75)];
    return (c: number) => (c <= 0 ? -1 : c <= q1 ? 0 : c <= q2 ? 1 : c <= q3 ? 2 : 3);
  }, [data.rows]);

  const xOf = useCallback(
    (d: string | null) => {
      const dt = parseDate(d);
      return dt ? xOfTime(from, dt.getTime(), dayW) : null;
    },
    [from, dayW],
  );
  /** 计划条：[start, end] 含端点（日期粒度） */
  const span = useCallback(
    (s: string | null, e: string | null): Span | null => {
      const x1 = xOf(s);
      const x2 = xOf(e);
      if (x1 === null && x2 === null) return null;
      const a = x1 ?? x2!;
      const b = (x2 ?? x1!) + dayW;
      return [Math.max(0, Math.min(a, b)), Math.min(width, Math.max(a, b))];
    },
    [xOf, dayW, width],
  );

  // 分组汇总：跨度、完成比例、数量、逾期数
  const summaries = useMemo(() => {
    const m = new Map<string, Summary>();
    const td = today();
    for (const g of groups) {
      const ts = g.tasks;
      const minS = ts.map((t) => t.planned_start).filter(Boolean).sort()[0] ?? null;
      const maxE = ts.map((t) => t.planned_end).filter(Boolean).sort().reverse()[0] ?? null;
      const sp = span(g.goal?.planned_start ?? minS, g.goal?.planned_end ?? maxE);
      const done = ts.filter((t) => t.state.label === "terminal_success").length;
      const overdue = ts.filter((t) => !!parseDate(t.planned_end) && parseDate(t.planned_end)! < td && !isTerminal(t.state.label)).length;
      const ratio = g.goal ? Math.max(0, Math.min(1, g.goal.progress / 100)) : ts.length ? done / ts.length : 0;
      m.set(g.key, { span: sp, ratio, total: ts.length, done, overdue });
    }
    return m;
  }, [groups, span]);

  // 任务条位置索引（前置箭头）：收起的组里的任务指到该组的汇总条
  const barIndex = useMemo(() => {
    const m = new Map<string, { x1: number; x2: number; y: number }>();
    rows.forEach((r, i) => {
      const cy = i * rowH + rowH / 2;
      if (r.kind === "task") {
        const s = span(r.t.planned_start, r.t.planned_end);
        if (s && !m.has(r.t.id)) m.set(r.t.id, { x1: s[0], x2: s[1], y: cy });
      } else if (!r.open) {
        const s = summaries.get(r.g.key)?.span;
        if (s) for (const t of r.g.tasks) if (!m.has(t.id)) m.set(t.id, { x1: s[0], x2: s[1], y: cy });
      }
    });
    return m;
  }, [rows, rowH, span, summaries]);

  // ---------- 拖拽新建 ----------
  const [drag, setDrag] = useState<Drag | null>(null);
  const dragRef = useRef<Drag | null>(null);
  const localX = (e: ReactPointerEvent) => {
    const rect = (e.currentTarget as Element).getBoundingClientRect();
    return e.clientX - rect.left;
  };
  const onDown = useCallback(
    (e: ReactPointerEvent<SVGSVGElement>, rowKey: string) => {
      if (!onCreate || e.button !== 0 || (e.target as Element).closest("a")) return;
      const x = localX(e);
      dragRef.current = { rowKey, x0: x, x1: x };
      setDrag(dragRef.current);
      e.currentTarget.setPointerCapture?.(e.pointerId);
    },
    [onCreate],
  );
  const onMove = useCallback((e: ReactPointerEvent<SVGSVGElement>) => {
    if (!dragRef.current) return;
    dragRef.current = { ...dragRef.current, x1: localX(e) };
    setDrag(dragRef.current);
  }, []);
  const onUp = useCallback(() => {
    const d = dragRef.current;
    if (!d) return;
    dragRef.current = null;
    setDrag(null);
    const a = Math.min(d.x0, d.x1);
    const b = Math.max(d.x0, d.x1);
    if (b - a < Math.max(4, dayW / 2)) return;
    const si = Math.floor(a / dayW);
    const ei = Math.max(si, Math.ceil(b / dayW) - 1);
    onCreate?.({ rowKey: d.rowKey, start: toISODate(addDays(from, si)), end: toISODate(addDays(from, ei)) });
  }, [dayW, from, onCreate]);

  // ---------- 折叠中：箭头层暂时淡出，等高度过渡结束再回来（箭头位置按终态算） ----------
  const [folding, setFolding] = useState(0);
  const onFoldStart = useCallback((e: ReactTransitionEvent) => {
    if (e.propertyName === "grid-template-rows") setFolding((n) => n + 1);
  }, []);
  const onFoldEnd = useCallback((e: ReactTransitionEvent) => {
    if (e.propertyName === "grid-template-rows") setFolding((n) => Math.max(0, n - 1));
  }, []);

  const corner = (
    <>
      <span className="eyebrow truncate text-ink-subtle">{tt("gantt.taskCount", { n: groups.reduce((s, g) => s + g.tasks.length, 0) })}</span>
      <span className="ml-auto font-mono text-[10px] text-ink-tertiary">{tt("gantt.rowCount", { n: totalRows })}</span>
    </>
  );

  // 虚拟化：每个组的任务行只渲染窗口内的那些，其余用占位高度
  const renderRows = (ctx: GanttFrameCtx) => {
    const win = virtual ? [Math.max(0, Math.floor((ctx.view.top - OVERSCAN) / rowH)), Math.ceil((ctx.view.top + ctx.view.h + OVERSCAN) / rowH)] : [0, totalRows];
    return groups.map((g, gi) => {
      const open = !macro && !collapsed.has(g.key);
      const { groupIndex, first } = offsets[gi];
      const s = summaries.get(g.key)!;
      const groupVisible = !virtual || (groupIndex >= win[0] && groupIndex <= win[1]);
      // 任务行窗口
      const a = Math.max(0, win[0] - first);
      const b = Math.min(g.tasks.length, win[1] - first + 1);
      const before = Math.max(0, Math.min(g.tasks.length, a));
      const slice = open && b > a ? g.tasks.slice(a, b) : [];
      const after = open ? Math.max(0, g.tasks.length - Math.max(a, b)) : 0;
      return (
        <div key={g.key} className="gantt-group">
          {groupVisible ? (
            <GroupRow g={g} open={open} macro={macro} summary={s} rowH={rowH} barH={barH} width={width} labelW={labelW} narrow={narrow} dayW={dayW} cells={ctx.header.bottom} xOf={xOf} onToggle={onToggle} drag={drag?.rowKey === g.key ? drag : null} onDown={onDown} onMove={onMove} onUp={onUp} creatable={!!onCreate} />
          ) : (
            <div style={{ height: rowH }} />
          )}
          {!macro && (
            <div className="fold" data-closed={open ? undefined : ""} onTransitionStart={onFoldStart} onTransitionEnd={onFoldEnd} onTransitionCancel={onFoldEnd} aria-hidden={!open}>
              <div>
                {before > 0 && <div style={{ height: before * rowH }} />}
                {slice.map((t) => (
                  <TaskRow key={t.id} t={t} groupKey={g.key} origin={g.origin?.get(t.id)} rowH={rowH} barH={barH} width={width} labelW={labelW} narrow={narrow} dayW={dayW} span={span} showOverdue={showOverdue} costLevel={showCost ? costLevel(t.cost) : -2} runs={scale === "hour" ? runs[t.id] : undefined} now={ctx.now} from={from} drag={drag?.rowKey === g.key ? drag : null} onDown={onDown} onMove={onMove} onUp={onUp} />
                ))}
                {after > 0 && <div style={{ height: after * rowH }} />}
              </div>
            </div>
          )}
        </div>
      );
    });
  };

  // 前置箭头
  const renderDeps = (ctx: GanttFrameCtx) => (
    <svg className="gantt-deps" style={{ left: ctx.labelW, width: ctx.width, opacity: folding ? 0 : 1 }} aria-hidden="true">
      <defs>
        <marker id="gantt-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
          <path d="M0,0 L8,4 L0,8 z" fill={C.tertiary} />
        </marker>
      </defs>
      {data.dependencies.map((d, i) => {
        const a = barIndex.get(d.from_task_id);
        const b = barIndex.get(d.to_task_id);
        if (!a || !b || (a.x2 === b.x2 && a.y === b.y)) return null;
        const sx = a.x2, sy = a.y, tx = b.x1, ty = b.y;
        const path = tx - 4 >= sx + 6 ? `M${sx},${sy} H${sx + 6} V${ty} H${tx - 1}` : `M${sx},${sy} H${sx + 6} V${sy + (ty > sy ? rowH / 2 : -rowH / 2)} H${tx - 10} V${ty} H${tx - 1}`;
        return <path key={i} d={path} fill="none" stroke={C.tertiary} strokeWidth={1.2} markerEnd="url(#gantt-arrow)" />;
      })}
    </svg>
  );

  return (
    <GanttFrame ref={ref} from={from} days={days} dayW={dayW} scale={scale} onZoom={onZoom} labelW={labelWProp} narrow={narrow} onLabelW={onLabelW} onNarrow={onNarrow} density={density} bodyH={bodyH} creatable={!!onCreate} scrollRef={scrollRef} corner={corner} repaintKey={data} overlay={renderDeps}>
      {renderRows}
    </GanttFrame>
  );
}

// ---------- 组行 ----------
interface GroupRowProps {
  g: GanttGroupModel;
  open: boolean;
  macro: boolean;
  summary: Summary;
  rowH: number;
  barH: number;
  width: number;
  labelW: number;
  narrow: boolean;
  dayW: number;
  cells: Cell[];
  xOf: (d: string | null) => number | null;
  onToggle: (key: string) => void;
  drag: Drag | null;
  onDown: (e: ReactPointerEvent<SVGSVGElement>, rowKey: string) => void;
  onMove: (e: ReactPointerEvent<SVGSVGElement>) => void;
  onUp: () => void;
  creatable: boolean;
}
const GroupRow = memo(function GroupRow({ g, open, macro, summary, rowH, barH, width, labelW, narrow, dayW, cells, xOf, onToggle, drag, onDown, onMove, onUp, creatable }: GroupRowProps) {
  const cy = rowH / 2;
  // 键盘触发（Enter / Space）：只让高度过渡，箭头不转动画
  const onChevronKey = (e: ReactKeyboardEvent<HTMLButtonElement>) => {
    if (e.key !== "Enter" && e.key !== " ") return;
    const el = e.currentTarget;
    el.classList.add("gantt-kbd");
    window.setTimeout(() => el.classList.remove("gantt-kbd"), 300);
  };
  const s = summary.span;
  const deadline = g.goal ? xOf(g.goal.planned_end) : null;
  const caption = `${tt("gantt.summary.done", { done: summary.done, n: summary.total })}${summary.overdue ? ` · ${tt("gantt.summary.overdue", { n: summary.overdue })}` : ""}`;
  // 密度热度（季 / 年）：每格按落在其中的任务数着色
  const density = useMemo(() => {
    if (!macro) return [];
    return cells.map((c) => {
      const a = c.x, b = c.x + c.w;
      let n = 0;
      for (const t of g.tasks) {
        const x1 = xOf(t.planned_start) ?? xOf(t.planned_end);
        const x2 = (xOf(t.planned_end) ?? xOf(t.planned_start))! + dayW;
        if (x1 !== null && Math.min(x1, x2) < b && Math.max(x1, x2) > a) n++;
      }
      return n;
    });
  }, [macro, cells, g.tasks, xOf, dayW]);
  const showFold = !macro;
  return (
    <div className="gantt-row gantt-row-group" style={{ height: rowH }}>
      <div className={`gantt-label gantt-label-group ${narrow ? "gantt-label-narrow" : ""}`} style={{ width: labelW }}>
        {showFold ? (
          <button type="button" className="gantt-chevron pressable" onClick={() => onToggle(g.key)} onKeyDown={onChevronKey} aria-expanded={open} aria-label={open ? tt("gantt.collapse") : tt("gantt.expand")} title={open ? tt("gantt.collapse") : tt("gantt.expand")}>
            <IconChevronRight className={`chevron ${open ? "chevron-open" : ""}`} />
          </button>
        ) : (
          <span className="gantt-chevron gantt-chevron-off" title={tt("gantt.macroHint")} aria-hidden="true">
            <IconChevronRight />
          </span>
        )}
        {!narrow && (
          <>
            {g.goal ? (
              <Link href={`/goals/${encodeURIComponent(g.goal.id)}/`} className="min-w-0 truncate text-ink hover:text-accent-hover">
                {g.title}
              </Link>
            ) : (
              <span className="min-w-0 truncate">{g.title}</span>
            )}
          </>
        )}
        <span className="gantt-count">{g.tasks.length}</span>
      </div>
      <svg width={width} height={rowH} className={`gantt-bars ${creatable ? "cursor-crosshair" : ""}`} onPointerDown={(e) => onDown(e, g.key)} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={onUp} fontFamily="inherit">
        <rect x={0} y={0} width={width} height={rowH} fill={C.ink} opacity={0.035} />
        {macro && density.map((n, i) => (n > 0 ? <rect key={i} x={cells[i].x} y={0} width={cells[i].w} height={rowH} fill={densityFill(n)} /> : null))}
        <g pointerEvents="none">
          {open && s && (
            <>
              {/* 航段：细线 + 两端航点 + 进度段（accent）+ 进度航点 */}
              <line x1={s[0]} y1={cy} x2={s[1]} y2={cy} stroke={C.hairlineTertiary} strokeWidth={2} />
              {summary.ratio > 0 && <line x1={s[0]} y1={cy} x2={s[0] + (s[1] - s[0]) * summary.ratio} y2={cy} stroke={C.accent} strokeWidth={2} />}
              <circle cx={s[0]} cy={cy} r={3} fill={C.surface1} stroke={summary.ratio > 0 ? C.accent : C.hairlineTertiary} strokeWidth={1.5} />
              <circle cx={s[1]} cy={cy} r={3} fill={C.surface1} stroke={summary.ratio >= 1 ? C.accent : C.hairlineTertiary} strokeWidth={1.5} />
              {summary.ratio > 0 && summary.ratio < 1 && <circle cx={s[0] + (s[1] - s[0]) * summary.ratio} cy={cy} r={2.5} fill={C.accent} />}
            </>
          )}
          {!open && s && (
            <>
              {/* 汇总条：跨度 + 已完成比例（实心段）+ 等宽读数（数量 · 逾期） */}
              <rect x={s[0] + 0.5} y={cy - barH / 2 + 0.5} width={Math.max(2, s[1] - s[0] - 1)} height={barH - 1} rx={2} fill={SUMMARY_FILL.bg} stroke={SUMMARY_FILL.border} />
              {summary.ratio > 0 && <rect x={s[0] + 1} y={cy - barH / 2 + 1} width={Math.max(1, (s[1] - s[0] - 2) * summary.ratio)} height={barH - 2} rx={1.5} fill={SUMMARY_FILL.done} />}
              <line x1={s[0] + 0.5} y1={cy - barH / 2 - 2} x2={s[0] + 0.5} y2={cy + barH / 2 + 2} stroke={SUMMARY_FILL.border} />
              <line x1={s[1] - 0.5} y1={cy - barH / 2 - 2} x2={s[1] - 0.5} y2={cy + barH / 2 + 2} stroke={SUMMARY_FILL.border} />
              {s[1] - s[0] > textW(caption, 10) + 12 ? (
                <text x={s[1] - 6} y={cy + 3.5} fontSize={10} fill={summary.overdue ? C.danger : C.telemetry} textAnchor="end" style={{ fontFamily: MONO }}>
                  {caption}
                </text>
              ) : (
                <text x={s[1] + 6} y={cy + 3.5} fontSize={10} fill={summary.overdue ? C.danger : C.telemetry} style={{ fontFamily: MONO }}>
                  {caption}
                </text>
              )}
            </>
          )}
          {!open && !s && summary.total > 0 && (
            <text x={6} y={cy + 3.5} fontSize={10} fill={C.telemetry} style={{ fontFamily: MONO }}>
              {caption}
            </text>
          )}
          {deadline !== null && deadline >= 0 && deadline <= width && (
            <>
              <line x1={deadline + dayW} y1={3} x2={deadline + dayW} y2={rowH - 3} stroke={C.inkMuted} strokeWidth={1.5} />
              {rowH >= 32 && (
                <text x={deadline + dayW + 4} y={11} fontSize={10} fill={C.subtle} style={{ fontFamily: MONO }}>
                  {tt("gantt.deadline")}
                </text>
              )}
            </>
          )}
        </g>
        {/* 里程碑（ADR 0016）：目标汇总行上同样画菱形，与目标甘特图同一个渲染器；只挂原生提示，不可拖 */}
        {g.goal?.milestones?.length ? <MilestoneMarks marks={g.goal.milestones} cy={cy} xOfDate={xOf} dayW={dayW} width={width} /> : null}
        {drag && <DragGhost drag={drag} cy={cy} barH={barH} />}
      </svg>
    </div>
  );
});

// ---------- 任务行 ----------
interface TaskRowProps {
  t: GanttTask;
  groupKey: string;
  origin?: string;
  rowH: number;
  barH: number;
  width: number;
  labelW: number;
  narrow: boolean;
  dayW: number;
  span: (s: string | null, e: string | null) => Span | null;
  showOverdue: boolean;
  /** -2 = 不高亮成本；-1 = 无成本；0–3 = 分位 */
  costLevel: number;
  runs?: Run[];
  now: number;
  from: Date;
  drag: Drag | null;
  onDown: (e: ReactPointerEvent<SVGSVGElement>, rowKey: string) => void;
  onMove: (e: ReactPointerEvent<SVGSVGElement>) => void;
  onUp: () => void;
}
const TaskRow = memo(function TaskRow({ t, groupKey, origin, rowH, barH, width, labelW, narrow, dayW, span, showOverdue, costLevel: lvl, runs, now, from, drag, onDown, onMove, onUp }: TaskRowProps) {
  const cy = rowH / 2;
  const s = span(t.planned_start, t.planned_end);
  const overdue = showOverdue && !!parseDate(t.planned_end) && parseDate(t.planned_end)! < today() && !isTerminal(t.state.label);
  const st = STATE_FILL[t.state.label];
  const fill = lvl === -2 ? st.bg : lvl === -1 ? C.surface4 : COST_FILL[lvl];
  const stroke = lvl === -2 ? st.border : lvl === -1 ? C.hairlineStrong : lvl >= 2 ? COST_STROKE.high : COST_STROKE.low;
  // 只有实心 accent 条用反白字；半透明的条在浅色底上反白读不清，用 ink
  const textFill = lvl === -2 ? st.text : lvl >= 3 ? C.onAccent : C.ink;
  const actualEnd = t.actual_end ?? (t.actual_start && !isTerminal(t.state.label) ? toISODate(today()) : null);
  const as = t.actual_start ? span(t.actual_start, actualEnd) : null;
  const title = `${t.title}\n${t.state.title} (${stateLabel(t.state.label)})\n${tt("gantt.tip.plan")} ${t.planned_start ?? "?"} – ${t.planned_end ?? "?"}${t.actual_start ? `\n${tt("gantt.tip.actual")} ${t.actual_start} – ${t.actual_end ?? tt("gantt.tip.ongoing")}` : ""}\n${tt("gantt.tip.cost")} ${t.cost.toFixed(2)}`;
  const labelInside = s ? s[1] - s[0] - 10 >= textW(t.title) : false;
  const labelFits = s ? s[1] - s[0] > 24 : false;
  const fontSize = rowH <= 28 ? 10.5 : 11;
  // 执行记录段（时档）：按实际起止画在条内，Agent 与人不同色
  const segs = useMemo(() => {
    if (!runs?.length) return [];
    return runs
      .map((r) => {
        const a = parseDate(r.started_at)?.getTime();
        if (!a) return null;
        const b = r.ended_at ? parseDate(r.ended_at)?.getTime() ?? now : now;
        const x1 = Math.max(0, xOfTime(from, a, dayW));
        const x2 = Math.min(width, xOfTime(from, b, dayW));
        return x2 > x1 ? { x1, x2, agent: r.executor.kind === "agent", title: `${r.executor.name} · ${r.state_title}` } : null;
      })
      .filter((x): x is NonNullable<typeof x> => !!x);
  }, [runs, now, from, dayW, width]);
  return (
    <div className="gantt-row gantt-row-task" style={{ height: rowH }}>
      <div className={`gantt-label ${narrow ? "gantt-label-narrow" : ""}`} style={{ width: labelW }} title={narrow ? `${t.title}${origin ? ` · ${origin}` : ""}` : origin}>
        <i className="gantt-dot" style={{ background: STATE_DOT[t.state.label] }} aria-hidden="true" />
        {!narrow && (
          <Link href={`/tasks/${encodeURIComponent(t.id)}/`} className="min-w-0 flex-1 truncate text-ink-muted hover:text-accent-hover">
            {t.title}
            {origin && <span className="ml-1.5 text-caption text-ink-tertiary">{origin}</span>}
          </Link>
        )}
        {t.assignee ? <Avatar name={t.assignee.name} kind={t.assignee.kind} size={16} /> : !narrow ? <span className="shrink-0 text-caption text-ink-subtle">{tt("gantt.unclaimed")}</span> : <i className="gantt-avatar-empty" aria-hidden="true" />}
      </div>
      <svg width={width} height={rowH} className="gantt-bars cursor-crosshair" onPointerDown={(e) => onDown(e, groupKey)} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={onUp} fontFamily="inherit">
        <rect x={0} y={0} width={width} height={rowH} fill="transparent" />
        {s ? (
          <Link href={`/tasks/${encodeURIComponent(t.id)}/`}>
            <title>{title}</title>
            <rect x={s[0] + 0.5} y={cy - barH / 2 + 0.5} width={Math.max(2, s[1] - s[0] - 1)} height={barH - 1} rx={2} fill={fill} stroke={stroke} strokeWidth={1} />
            {/* 1px 端点：比条高出 2px */}
            <line x1={s[0] + 0.5} y1={cy - barH / 2 - 2} x2={s[0] + 0.5} y2={cy + barH / 2 + 2} stroke={stroke} strokeWidth={1} />
            <line x1={s[1] - 0.5} y1={cy - barH / 2 - 2} x2={s[1] - 0.5} y2={cy + barH / 2 + 2} stroke={stroke} strokeWidth={1} />
            {as && <rect x={as[0]} y={cy + barH / 2 - ACTUAL_H - 1} width={Math.max(2, as[1] - as[0])} height={ACTUAL_H} rx={2} fill={t.state.label === "active" ? C.accentHover : C.inkMuted} opacity={0.85} />}
            {segs.map((r, i) => (
              <rect key={i} x={r.x1} y={cy - barH / 2 + 3} width={Math.max(1.5, r.x2 - r.x1)} height={barH - 6 - (as ? ACTUAL_H - 1 : 0)} rx={1} fill={r.agent ? RUN_FILL.agent : RUN_FILL.member} opacity={0.9}>
                <title>{r.title}</title>
              </rect>
            ))}
            {labelFits && labelInside && (
              <text x={s[0] + 5} y={cy + 4} fontSize={fontSize} fill={textFill} pointerEvents="none">
                {clipToWidth(t.title, s[1] - s[0] - 8, fontSize)}
              </text>
            )}
            {!labelInside && (
              <text x={s[1] + (overdue ? 12 : 6)} y={cy + 4} fontSize={fontSize} fill={C.inkMuted} pointerEvents="none">
                {clipToWidth(t.title, Math.max(0, width - s[1] - 12), fontSize)}
              </text>
            )}
            {overdue && <polygon points={`${s[1] + 1},${cy - 5} ${s[1] + 7},${cy} ${s[1] + 1},${cy + 5}`} fill={C.danger} />}
          </Link>
        ) : (
          <>
            {segs.map((r, i) => (
              <rect key={i} x={r.x1} y={cy - barH / 2 + 3} width={Math.max(1.5, r.x2 - r.x1)} height={barH - 6} rx={1} fill={r.agent ? RUN_FILL.agent : RUN_FILL.member} opacity={0.9}>
                <title>{r.title}</title>
              </rect>
            ))}
            {as && <rect x={as[0]} y={cy + barH / 2 - ACTUAL_H - 1} width={Math.max(2, as[1] - as[0])} height={ACTUAL_H} rx={2} fill={t.state.label === "active" ? C.accentHover : C.inkMuted} opacity={0.85} />}
            {!as && !segs.length && (
              <text x={6} y={cy + 4} fontSize={fontSize} fill={C.tertiary} pointerEvents="none">
                {tt("gantt.unscheduled")}
              </text>
            )}
          </>
        )}
        {drag && <DragGhost drag={drag} cy={cy} barH={barH} />}
      </svg>
    </div>
  );
});

function DragGhost({ drag, cy, barH }: { drag: Drag; cy: number; barH: number }) {
  return <rect x={Math.min(drag.x0, drag.x1)} y={cy - barH / 2} width={Math.abs(drag.x1 - drag.x0)} height={barH} rx={3} fill={C.accent} opacity={0.25} stroke={C.accent} strokeDasharray="3 2" pointerEvents="none" />;
}
