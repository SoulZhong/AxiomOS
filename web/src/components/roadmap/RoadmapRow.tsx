"use client";
import Link from "next/link";
import { useId, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from "react";
import type { Goal, ISODate } from "@/lib/api";
import { fmtDate, toISODate, today } from "@/lib/format";
import { t } from "@/lib/i18n";
import { C, MONO, textW } from "@/components/gantt/GanttFrame";
import { MilestoneMarks, type MilestoneInteraction } from "@/components/gantt/MilestoneMarks";
import { IconChevronRight, IconGantt } from "@/components/icons";
import { cx } from "@/components/ui";
import { barTone, confidenceTone, type BarSpan, type ColorBy } from "./model";
import type { MsCluster } from "./milestones";

/*
 * 路线图时间线的一行（ADR 0022「页面规格」）：左边固定的信息列，右边一根条。
 * 行高 44px 固定，信息列一行四段——折叠箭头 + 信心度圆点 + 标题、成果指标、负责人、进度。
 */

export const ROW_H = 44;
export const LANE_H = 32;
export const BAR_H = 18;
export const INDENT = 16;
/** 信息列：常态 320px，窄屏 200px（只留标题与信心度圆点） */
export const INFO_W = 320;
export const INFO_W_NARROW = 200;

export interface RowSpan {
  span: BarSpan;
  /** 折叠后跨整棵子树的汇总条 */
  summary: boolean;
  /** 图区里的像素区间 */
  x0: number;
  x1: number;
}

export interface RoadmapRowProps {
  goal: Goal;
  depth: number;
  open: boolean;
  hasKids: boolean;
  width: number;
  infoW: number;
  compact: boolean;
  /** 行选中（单击选中，回车进详情） */
  selected: boolean;
  /** 这一行的条；null = 没有日期（幽灵条由 ghostFrom 画） */
  row: RowSpan | null;
  /** 幽灵条从哪里起（今天线的 x），只有「还没排期」的行有 */
  ghostX: number | null;
  /** 一格宽（渐隐雾边正好一格） */
  cellW: number;
  dayW: number;
  xOf: (d: string | null | undefined) => number | null;
  clusters: MsCluster[];
  /** 「还没排期」那一组里显示的上级目标名（树被拆开之后仍看得出它是谁的下级） */
  parentTitle: string | null;
  ms: MilestoneInteraction<MsCluster>;
  editable: boolean;
  /** 条按什么着色（按状态 / 按类型） */
  colorBy: ColorBy;
  /** 这一行能不能上下拖着排序（只有泳道里的顶级行可以） */
  rankable: boolean;
  onToggle: (id: string) => void;
  onSelect: (id: string) => void;
  onBarEnter: (g: Goal, el: Element) => void;
  onBarLeave: () => void;
  onBarDown: (e: ReactPointerEvent<Element>, g: Goal, kind: "move" | "start" | "end" | "ghost") => void;
  onRankDown: (e: ReactPointerEvent<Element>, g: Goal) => void;
  onDoubleClick: (e: React.MouseEvent<SVGSVGElement>, g: Goal) => void;
  dragging: boolean;
  /** 正在被拖着换位置的那一行 */
  ranking: boolean;
  /** 双击条区空白处新建里程碑 */
  creating: { x: number; due_on: ISODate } | null;
  onCreate: (title: string) => void;
  onCancelCreate: () => void;
}

export function RoadmapRow(p: RoadmapRowProps) {
  const { goal, row, width, infoW, compact, cellW } = p;
  const uid = useId().replace(/[^a-zA-Z0-9]/g, "");
  const cy = ROW_H / 2;
  const barY = cy - BAR_H / 2;
  const tone = barTone(goal, !!row?.summary, p.colorBy);
  const abandoned = goal.status === "abandoned";
  const conf = confidenceTone(goal);
  const pct = `${Math.round(goal.progress)}%`;
  const ratio = abandoned ? 0 : Math.max(0, Math.min(1, goal.progress / 100));
  const kind = row?.span.kind;
  const derived = kind === "derived";
  // 截止日：条外的一道短刻度（沿用目标甘特图的画法），画在那一天的末尾；已经过了还没达成的画成告警色
  const dlX = goal.deadline ? (p.xOf(goal.deadline) ?? null) : null;
  const deadlineX = dlX === null ? null : dlX + p.dayW;
  const late = !!goal.deadline && !goal.achieved && !abandoned && goal.deadline < toISODate(today());

  // 渐隐雾边：一格宽，条太短时最多吃掉半根
  const barW = row ? Math.max(3, row.x1 - row.x0) : 0;
  const fadeW = Math.min(cellW, barW / 2);
  const fadeStart = !!row && row.span.fadeStart && fadeW > 1;
  const fadeEnd = !!row && row.span.fadeEnd && fadeW > 1;
  const maskId = `rm-mask-${uid}`;
  const masked = fadeStart || fadeEnd;

  return (
    <div className={cx("gantt-row rt-row", row?.summary && "rt-row-summary", p.selected && "rt-row-on", p.ranking && "rt-row-ranking")} style={{ height: ROW_H }} data-dragging={p.dragging ? "" : undefined}>
      <div className={cx("gantt-label rt-info", compact && "rt-info-compact")} style={{ width: infoW, paddingLeft: 8 + p.depth * INDENT }} onClick={() => p.onSelect(goal.id)}>
        {p.editable && p.rankable && !compact && (
          <span className="rt-grip" onPointerDown={(e) => p.onRankDown(e, goal)} title={t("roadmap.rankTip")} aria-hidden="true">
            <svg width="8" height="14" viewBox="0 0 8 14" fill="currentColor" focusable="false">
              {[2, 6, 10].map((y) => [1.5, 6.5].map((x) => <circle key={`${x}-${y}`} cx={x} cy={y} r="1" />))}
            </svg>
          </span>
        )}
        <button type="button" className={cx("gantt-chevron pressable", !p.hasKids && "invisible")} onClick={(e) => { e.stopPropagation(); p.onToggle(goal.id); }} aria-expanded={p.open} aria-label={p.open ? t("goals.collapse") : t("goals.expand")} tabIndex={p.hasKids ? 0 : -1}>
          <IconChevronRight className={cx("chevron", p.open && "chevron-open")} />
        </button>
        <span className="rt-info-main">
          <span className="rt-title-line">
            <i className={cx("rt-dot", conf && `rt-dot-${conf}`)} title={conf ? t("roadmap.confidenceTip", { v: goal.confidence_title || "" }) : t("confidence.none")} aria-hidden="true" />
            <Link href={`/goals/${encodeURIComponent(goal.id)}/`} className={cx("rt-title", abandoned && "line-through opacity-70")} title={goal.title} onClick={(e) => e.stopPropagation()}>
              {goal.title}
            </Link>
            {derived && (
              <span className="rt-derived" title={t("roadmap.derivedTip")} aria-label={t("roadmap.derivedTip")}>
                ↗
              </span>
            )}
          </span>
          {/* 第二行：上级目标、目标类型（ADR 0023，一枚类型色点 + 名字）与成果指标；都没有就不占行，其余三段自然垂直居中 */}
          {!compact && (goal.type || goal.outcome || p.parentTitle) && (
            <span className="rt-sub">
              {p.parentTitle && (
                <span className="rt-parent" title={p.parentTitle}>
                  {p.parentTitle} ›
                </span>
              )}
              {goal.type && (
                <span className="rt-type" title={goal.type.name}>
                  <i className="rt-type-dot" style={{ background: goal.type.color }} aria-hidden="true" />
                  {goal.type.name}
                </span>
              )}
              {goal.outcome && (
                <span className="rt-outcome" title={goal.outcome}>
                  {goal.outcome}
                </span>
              )}
            </span>
          )}
        </span>
        {!compact && (
          <>
            <ProgressPie ratio={ratio} done={goal.achieved} pct={Math.round(goal.progress)} title={t("roadmap.progressTip", { pct, owner: goal.owner.name })} />
            <Link href={`/tasks/?view=gantt&goal=${encodeURIComponent(goal.id)}`} className="rt-jump" title={t("roadmap.openTasks")} aria-label={t("roadmap.openTasks")} onClick={(e) => e.stopPropagation()}>
              <IconGantt size={13} />
            </Link>
          </>
        )}
      </div>

      <svg width={width} height={ROW_H} className="gantt-bars rt-bars" onDoubleClick={(e) => p.onDoubleClick(e, goal)} fontFamily="inherit">
        {row?.summary && <rect x={0} y={0} width={width} height={ROW_H} fill={C.ink} opacity={0.03} />}
        {masked && (
          <defs>
            <mask id={maskId} maskUnits="userSpaceOnUse" x={row!.x0 - 4} y={0} width={barW + 8} height={ROW_H}>
              <rect x={row!.x0} y={0} width={barW} height={ROW_H} fill="#fff" />
              {fadeStart && <rect x={row!.x0} y={0} width={fadeW} height={ROW_H} fill="url(#rm-fade-l)" />}
              {fadeEnd && <rect x={row!.x1 - fadeW} y={0} width={fadeW} height={ROW_H} fill="url(#rm-fade-r)" />}
            </mask>
          </defs>
        )}

        {/* 幽灵条：还没排期的目标，从今天线起一根极淡的虚线，按住往右拖就算排期了 */}
        {p.ghostX !== null && (
          <g className="rt-ghost" onPointerDown={p.editable ? (e) => p.onBarDown(e, goal, "ghost") : undefined} data-locked={p.editable ? undefined : ""}>
            <rect className="rt-ghost-hit" x={p.ghostX} y={barY - 4} width={Math.max(120, cellW * 5)} height={BAR_H + 8} />
            <rect className="rt-ghost-bar" x={p.ghostX + 0.5} y={barY + 0.5} width={Math.max(60, cellW * 3)} height={BAR_H - 1} rx={4} />
            <text className="rt-ghost-hint" x={p.ghostX + Math.max(60, cellW * 3) + 8} y={cy + 3.5} fontSize={10.5}>
              {t("roadmap.ghostHint")}
            </text>
          </g>
        )}

        {row && (
          <g mask={masked ? `url(#${maskId})` : undefined}>
            <rect x={row.x0 + 0.5} y={barY + 0.5} width={Math.max(2, barW - 1)} height={BAR_H - 1} rx={4} fill={tone.bg} stroke={tone.border} strokeDasharray={abandoned ? "3 2" : undefined} />
            {ratio > 0 && <rect x={row.x0 + 1.5} y={barY + 1.5} width={Math.max(1, (barW - 3) * ratio)} height={BAR_H - 3} rx={3} fill={tone.done} />}
            {/* 斜纹 = 这条是从子目标推出来的，不是谁定的 */}
            {derived && <rect x={row.x0 + 0.5} y={barY + 0.5} width={Math.max(2, barW - 1)} height={BAR_H - 1} rx={4} fill="url(#rm-hatch)" />}
          </g>
        )}
        {row && barW > textW(pct, 10) + 16 && (
          <text x={row.x1 - 6} y={cy + 3.5} fontSize={10} fill={tone.text} textAnchor="end" style={{ fontFamily: MONO }} pointerEvents="none">
            {pct}
          </text>
        )}

        {/* 截止日：横条外的一道短刻度，与条无关——条说的是打算什么时候做，这一道说的是最晚什么时候 */}
        {deadlineX !== null && deadlineX >= 0 && deadlineX <= width && (
          <g className="rt-deadline">
            <line x1={deadlineX} y1={3} x2={deadlineX} y2={ROW_H - 3} stroke={late ? C.danger : C.inkMuted} strokeWidth={1.5} pointerEvents="none" />
            <text x={deadlineX + 4} y={11} fontSize={10} fill={late ? C.danger : C.subtle} style={{ fontFamily: MONO }} pointerEvents="none">
              {t("gantt.deadline")}
            </text>
            <rect x={deadlineX - 4} y={0} width={8} height={ROW_H} fill="transparent">
              <title>{t("roadmap.deadlineTip", { date: fmtDate(goal.deadline) })}</title>
            </rect>
          </g>
        )}

        {/*
          命中区。一层 <g> 管悬停气泡，里面从下到上是：整根条的透明命中面、条身（拖着移动）、两端把手（改长短）。
          次序要紧：SVG 里后画的在上面，命中面必须在最下面，否则它会把把手的按下事件吃掉。
        */}
        {row && (
          <g onPointerEnter={(e) => p.onBarEnter(goal, e.currentTarget)} onPointerLeave={p.onBarLeave}>
            <rect className="rt-bar-hit" x={row.x0} y={barY - 2} width={barW} height={BAR_H + 4} />
            {p.editable && !row.summary && !abandoned && (
              <>
                <rect className="rt-grab" x={row.x0 + 4} y={barY - 2} width={Math.max(1, barW - 8)} height={BAR_H + 4} onPointerDown={(e) => p.onBarDown(e, goal, "move")} />
                <rect className="gantt-handle" x={row.x0 - 3} y={barY - 3} width={8} height={BAR_H + 6} rx={2} onPointerDown={(e) => p.onBarDown(e, goal, "start")} />
                <rect className="gantt-handle" x={row.x1 - 5} y={barY - 3} width={8} height={BAR_H + 6} rx={2} onPointerDown={(e) => p.onBarDown(e, goal, "end")} />
              </>
            )}
          </g>
        )}

        {/* 里程碑：路线图上唯一出现具体某一天的地方 */}
        <MilestoneMarks marks={p.clusters} cy={cy} xOfDate={p.xOf} dayW={p.dayW} width={width} interactive={p.ms} />
      </svg>
      {p.creating && <InlineCreate left={infoW + p.creating.x} top={(ROW_H - 26) / 2} due_on={p.creating.due_on} onSubmit={p.onCreate} onCancel={p.onCancelCreate} />}
    </div>
  );
}

/** 双击处的内联输入：名称，回车保存，Esc / 失焦取消（沿用目标甘特图那一套） */
function InlineCreate({ left, top, due_on, onSubmit, onCancel }: { left: number; top: number; due_on: ISODate; onSubmit: (title: string) => void; onCancel: () => void }) {
  const [title, setTitle] = useState("");
  const onKey = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && title.trim()) onSubmit(title.trim());
    if (e.key === "Escape") onCancel();
  };
  return (
    <div className="gantt-inline-input" style={{ left: Math.max(0, left - 4), top }}>
      <input value={title} onChange={(e) => setTitle(e.target.value)} onKeyDown={onKey} onBlur={() => !title.trim() && onCancel()} placeholder={t("roadmap.newMilestone")} autoFocus aria-label={t("ms.add")} />
      <span className="gantt-inline-date">{due_on}</span>
    </div>
  );
}

/** 泳道头：一行道名 + 目标数 */
/** 进度环形图：26px，数字写在环里，比"环 + 外置数字"再省一截横向空间。
 *  环按汇总进度画弧，已达成用成功色；数字省掉百分号（外面有「进度」的提示），100% 时缩一号字免得挤。 */
function ProgressPie({ ratio, done, pct, title }: { ratio: number; done: boolean; pct: number; title: string }) {
  const R = 10.5, C = 13, full = 2 * Math.PI * R;
  return (
    <span className="rt-pie" title={title}>
      <svg width={26} height={26} viewBox="0 0 26 26" aria-hidden="true">
        <circle cx={C} cy={C} r={R} fill="none" stroke="var(--c-hairline-strong)" strokeWidth={2} />
        {ratio > 0 && (
          <circle
            cx={C} cy={C} r={R} fill="none" strokeWidth={2} strokeLinecap="round"
            stroke={done ? "var(--c-success)" : "var(--c-accent)"}
            strokeDasharray={`${(full * ratio).toFixed(2)} ${full.toFixed(2)}`}
            transform={`rotate(-90 ${C} ${C})`}
          />
        )}
        <text x={C} y={C} textAnchor="middle" dominantBaseline="central" className="rt-pie-num" fontSize={pct >= 100 ? 8.5 : 9.5}>
          {pct}
        </text>
      </svg>
    </span>
  );
}

export function LaneHead({ title, n, infoW, width }: { title: string; n: number; infoW: number; width: number }) {
  return (
    <div className="gantt-row rt-lane" style={{ height: LANE_H }}>
      <div className="gantt-label rt-lane-label" style={{ width: infoW }}>
        <span className="rt-lane-title">{title}</span>
        <span className="gantt-count">{n}</span>
      </div>
      <svg width={width} height={LANE_H} className="gantt-bars" aria-hidden="true" />
    </div>
  );
}

/** 图区里画一次的模板：斜纹与两头的渐隐（所有行共用，条上只引用 id） */
export function RoadmapDefs() {
  return (
    <svg width={0} height={0} className="rt-defs" aria-hidden="true" focusable="false">
      <defs>
        <pattern id="rm-hatch" width="7" height="7" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
          <line x1="0" y1="0" x2="0" y2="7" stroke={C.ink} strokeWidth="2" opacity="0.22" />
        </pattern>
        <linearGradient id="rm-fade-l" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stopColor="#000" />
          <stop offset="1" stopColor="#fff" />
        </linearGradient>
        <linearGradient id="rm-fade-r" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stopColor="#fff" />
          <stop offset="1" stopColor="#000" />
        </linearGradient>
      </defs>
    </svg>
  );
}
