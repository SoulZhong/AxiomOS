"use client";
import { useMemo, useState } from "react";
import type { BoardLane } from "@/lib/api";
import { t } from "@/lib/i18n";
import { usePersisted } from "@/lib/usePersisted";
import { Board, type BoardDensity } from "@/components/board/Board";
import { EnergyLine, Segmented, Tip } from "@/components/ui";
import type { TaskFilters } from "./filters";

const LANES: BoardLane[] = ["none", "goal", "assignee"];
const DENSITIES: BoardDensity[] = ["comfortable", "compact"];

/**
 * 「任务 · 看板」：筛选栏的 type / goal / team / assignee / sprint 直接成为 GET /board 的参数；
 * 状态类型在看板上不适用（列本身就是状态）。分道与密度是看板自己的控件，密度记在 localStorage。
 */
export function BoardView({ filters, reloadKey }: { filters: TaskFilters; reloadKey: number }) {
  const [density, setDensity] = usePersisted<BoardDensity>("axiomos.board.density", "comfortable", (v) => (DENSITIES.includes(v as BoardDensity) ? (v as BoardDensity) : undefined));
  const [lane, setLane] = useState<BoardLane>("none");
  const query = useMemo(() => ({ type: filters.type || undefined, goal: filters.goal || undefined, team: filters.team || undefined, assignee: filters.assignee || undefined, sprint: filters.sprint || undefined, lane }), [filters.type, filters.goal, filters.team, filters.assignee, filters.sprint, lane]);
  return (
    <div className="flex min-h-0 flex-col">
      <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-2">
        <Tip tip={t("board.description") + "\n" + t("board.dragHint")} placement="bottom">
          <span className="eyebrow inline-flex h-5 w-5 cursor-help items-center justify-center rounded-xs border border-hairline-strong text-ink-subtle" aria-label={t("board.hint")}>?</span>
        </Tip>
        <EnergyLine className="hidden sm:block" />
        <span className="ml-auto flex flex-wrap items-center gap-3">
          <Segmented size="sm" value={lane} onChange={setLane} label={t("board.lane")} aria-label={t("board.lane")} options={LANES.map((l) => [l, t(`board.lane.${l}`)])} />
          <Segmented size="sm" value={density} onChange={setDensity} aria-label={t("board.density")} options={DENSITIES.map((d) => [d, t(`board.density.${d}`)])} />
        </span>
      </div>
      {/* 新建任务后重挂看板取一次新数据（key 变化） */}
      <Board key={reloadKey} query={query} density={density} />
    </div>
  );
}
