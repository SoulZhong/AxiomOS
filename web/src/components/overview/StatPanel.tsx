"use client";
import { useMemo, type ReactNode } from "react";
import { api } from "@/lib/api";
import { fmtPercent, parseDate } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { bucketByDay } from "@/components/instruments/Sparkline";
import { ErrorBox, ListSkeleton, Panel, Skeleton, cx } from "@/components/ui";

/*
 * 组织概览的数据页签（成本 / 效率 / Agent）共用的小件——原「运营总览」的四块拆到三个页签里（DESIGN.md §10、§3 仪表级数据）。
 */

/**
 * 统计面板：序号眉标 + 一个大读数（24px/600；金额是滚动数字，通过率是弧形仪表）+ 一句说明（12px，基线对齐）
 * + 右上角的仪表位（aside：迷你折线）+ 一个小图。
 */
export function StatPanel({ index, icon, title, big, caption, aside, actions, loading, error, onRetry, className, children }: { index: number; icon?: ReactNode; title: string; big: ReactNode | null; caption: string; aside?: ReactNode; actions?: ReactNode; loading: boolean; error: string | null; onRetry: () => void; className?: string; children: ReactNode }) {
  return (
    <Panel index={index} icon={icon} title={title} actions={actions} padded={false} className={className}>
      <div className="flex items-end justify-between gap-4 px-4 pt-4 pb-3">
        <div className="flex min-w-0 items-baseline gap-2">
          {big === null ? <Skeleton className="h-6 w-24 self-center" /> : <span className="text-headline leading-none tabular-nums text-ink">{big}</span>}
          <span className="text-caption leading-none text-ink-subtle">{caption}</span>
        </div>
        {aside && <div className="shrink-0 self-start">{aside}</div>}
      </div>
      {loading ? <ListSkeleton rows={4} /> : error ? <div className="p-4"><ErrorBox message={error} onRetry={onRetry} /></div> : <div className="[&>.tbl-wrap]:border-t [&>.tbl-wrap]:border-hairline [&>:not(.tbl-wrap)]:px-4 [&>:not(.tbl-wrap)]:pb-4">{children}</div>}
    </Panel>
  );
}

export function Rate({ value, tone }: { value: number; tone: "success" | "danger" }) {
  return (
    <span className="inline-flex items-center gap-2">
      <span className="h-1.5 w-10 overflow-hidden rounded-full bg-surface-3"><span className={cx("block h-full rounded-full", tone === "success" ? "bg-success" : "bg-danger")} style={{ width: `${value * 100}%` }} /></span>
      <span className="tabular-nums">{fmtPercent(value)}</span>
    </span>
  );
}

/** 周起始日的短标签：8/31，不占宽度。 */
export function shortDate(iso: string): string {
  const d = parseDate(iso);
  return d ? `${d.getMonth() + 1}/${d.getDate()}` : iso;
}

/**
 * 迷你折线的序列后端没有现成接口，从 GET /tasks 派生：
 *   - 成本：所有任务的执行记录（task.runs）按 started_at 归到"近 24 天，每天一格"，每格是当天开始的执行记录的成本合计；
 *   - 吞吐：已完成（state.label = terminal_success）的任务按 actual_end 归到近 24 天，每格是当天完成的任务数。
 */
export function useDaySeries() {
  const tasks = useLoad(() => api.tasks.list(), []);
  return useMemo(() => {
    const rows = tasks.data;
    if (!rows) return null;
    return {
      cost: bucketByDay(rows.flatMap((x) => x.runs), (r) => r.started_at, (r) => r.cost),
      done: bucketByDay(rows.filter((x) => x.state.label === "terminal_success"), (x) => x.actual_end),
    };
  }, [tasks.data]);
}
