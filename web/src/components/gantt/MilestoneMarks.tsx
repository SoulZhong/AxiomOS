"use client";
import type { PointerEvent as ReactPointerEvent } from "react";
import type { ISODate, MilestoneMark, MilestoneStatus } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import { t } from "@/lib/i18n";
import { alpha, C } from "./GanttFrame";

/*
 * 里程碑 = 菱形（DESIGN.md §11、ADR 0016）。画在所属目标的行上、对应日期那一天的中点，8px：
 *   未到 = hairline-strong 描边空心；已达到 = success 实心；逾期 = danger 实心并带小三角。
 * 目标甘特图与任务甘特图（按目标分组的汇总行）共用这一个渲染器；任务永远是横条，里程碑永远是菱形。
 * 不传 interactive 时只挂原生 <title>（任务甘特图）；传了则由调用方接管悬停提示、点击与拖动（目标甘特图）。
 */

/** 菱形半对角线：整枚 8px */
export const MS_R = 4;
const HIT = 18;

export const MS_FILL: Record<MilestoneStatus, { fill: string; stroke: string }> = {
  // 空心菱形落在半透明的 accent 横条上，hairline 会被吃掉，描边用 ink-subtle 才读得出来
  upcoming: { fill: C.surface1, stroke: C.subtle },
  reached: { fill: C.success, stroke: C.success },
  overdue: { fill: C.danger, stroke: C.danger },
};

export const milestoneStatusTitle = (s: MilestoneStatus) => t(`ms.status.${s}`);

/** 悬停提示的三段：名称 · 日期 · 状态（+ 可以确认了） */
export function milestoneTipParts(m: Pick<MilestoneMark, "title" | "due_on" | "status" | "ready_hint">): { title: string; date: string; status: string; ready: boolean } {
  return { title: m.title, date: fmtDate(m.due_on), status: milestoneStatusTitle(m.status), ready: !!m.ready_hint && m.status !== "reached" };
}
export function milestoneTipText(m: Pick<MilestoneMark, "title" | "due_on" | "status" | "ready_hint">): string {
  const p = milestoneTipParts(m);
  return `${p.title} · ${p.date} · ${p.status}${p.ready ? ` · ${t("ms.ready")}` : ""}`;
}

export interface MilestoneInteraction<M extends MilestoneMark = MilestoneMark> {
  onEnter: (m: M, el: SVGGElement) => void;
  onLeave: (m: M) => void;
  /** 调用方自己区分点击与拖动（按下后位移 < 3px 视为点击） */
  onPointerDown: (e: ReactPointerEvent<SVGGElement>, m: M) => void;
  /** 打开了气泡的那一枚：带光晕 */
  activeId?: string | null;
  /** 正在拖动的那一枚：临时的 x */
  drag?: { id: string; x: number } | null;
}

export interface MilestoneMarksProps<M extends MilestoneMark = MilestoneMark> {
  marks: readonly M[];
  cy: number;
  /** 某一天左缘的 x；超出范围的返回 null 则不画 */
  xOfDate: (d: ISODate) => number | null;
  dayW: number;
  /** 图区宽度：落在外面的不画 */
  width: number;
  interactive?: MilestoneInteraction<M>;
}

const diamond = (cx: number, cy: number, r: number) => `${cx},${cy - r} ${cx + r},${cy} ${cx},${cy + r} ${cx - r},${cy}`;

export function MilestoneMarks<M extends MilestoneMark = MilestoneMark>({ marks, cy, xOfDate, dayW, width, interactive }: MilestoneMarksProps<M>) {
  if (!marks.length) return null;
  return (
    <g className="gantt-ms-layer">
      {marks.map((m) => {
        const dragging = interactive?.drag?.id === m.id;
        const x0 = xOfDate(m.due_on);
        if (x0 === null && !dragging) return null;
        const cx = dragging ? interactive!.drag!.x : x0! + dayW / 2;
        if (cx < -HIT || cx > width + HIT) return null;
        const f = MS_FILL[m.status];
        return (
          <g
            key={m.id}
            className="gantt-ms"
            data-status={m.status}
            data-active={interactive?.activeId === m.id ? "" : undefined}
            data-dragging={dragging ? "" : undefined}
            onPointerEnter={interactive ? (e) => interactive.onEnter(m, e.currentTarget) : undefined}
            onPointerLeave={interactive ? () => interactive.onLeave(m) : undefined}
            onPointerDown={interactive ? (e) => interactive.onPointerDown(e, m) : undefined}
            role={interactive ? "button" : undefined}
            aria-label={milestoneTipText(m)}
          >
            {!interactive && <title>{milestoneTipText(m)}</title>}
            {/* 命中区：比菱形大一圈，拖动与点击都好按 */}
            <rect className="gantt-ms-hit" x={cx - HIT / 2} y={cy - HIT / 2} width={HIT} height={HIT} />
            <g className="gantt-ms-shape">
              {/* 光环：把菱形从横条上"抬"出来，无论横条什么颜色都看得见 */}
              <polygon className="gantt-ms-halo" points={diamond(cx, cy, MS_R + 2)} fill={C.surface1} stroke={m.status === "upcoming" ? alpha("ink", 0) : C.surface1} strokeWidth={1} />
              <polygon points={diamond(cx, cy, MS_R)} fill={f.fill} stroke={f.stroke} strokeWidth={1.5} strokeLinejoin="round" />
              {/* 逾期：菱形右上方一枚小三角 */}
              {m.status === "overdue" && <polygon points={`${cx + 3},${cy - MS_R - 6} ${cx + 8},${cy - MS_R - 6} ${cx + 5.5},${cy - MS_R - 1.5}`} fill={C.danger} />}
            </g>
          </g>
        );
      })}
    </g>
  );
}

/** 图例里的一枚样例菱形 */
export function MilestoneSwatch({ status }: { status: MilestoneStatus }) {
  const f = MS_FILL[status];
  return (
    <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true" focusable="false">
      <polygon points={diamond(7, 7, 4)} fill={f.fill} stroke={f.stroke} strokeWidth="1.5" strokeLinejoin="round" />
      {status === "overdue" && <polygon points="9,0.5 13,0.5 11,4" fill={C.danger} />}
    </svg>
  );
}
