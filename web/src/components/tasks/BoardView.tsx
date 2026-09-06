"use client";
import { useCallback, useMemo, useState } from "react";
import type { BoardCard, BoardLane } from "@/lib/api";
import { t } from "@/lib/i18n";
import { usePersisted } from "@/lib/usePersisted";
import { Board, type BoardDensity } from "@/components/board/Board";
import { EnergyLine, Segmented, Tip } from "@/components/ui";
import { matchDue, matchQuery, type TaskFilters } from "./filters";

const LANES: BoardLane[] = ["none", "goal", "assignee"];
const DENSITIES: BoardDensity[] = ["comfortable", "compact"];

/**
 * 「任务 · 看板」：筛选栏的 type / goal / team / assignee / sprint 直接成为 GET /board 的参数；
 * 状态类型在看板上不适用（列本身就是状态）；优先级 / 截止日 / 关键词在前端按卡片过滤。分道与密度是看板自己的控件，密度记在 localStorage。
 * 点卡片开抽屉（onOpen），抽屉里「打开完整页」进详情。
 */
export function BoardView({ filters, reloadKey, onOpen }: { filters: TaskFilters; reloadKey: number; onOpen: (id: string) => void }) {
  const [density, setDensity] = usePersisted<BoardDensity>("axiomos.board.density", "comfortable", (v) => (DENSITIES.includes(v as BoardDensity) ? (v as BoardDensity) : undefined));
  const [lane, setLane] = useState<BoardLane>("none");
  const query = useMemo(() => ({ type: filters.type || undefined, goal: filters.goal || undefined, team: filters.team || undefined, assignee: filters.assignee || undefined, sprint: filters.sprint || undefined, lane }), [filters.type, filters.goal, filters.team, filters.assignee, filters.sprint, lane]);
  const { priority, due, q } = filters;
  const filterCard = useCallback((c: BoardCard) => (!priority || c.priority === priority) && matchDue(due, c.planned_end, c.state.label) && matchQuery(q, c.title, c.id, c.number), [priority, due, q]);
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
      <Board key={reloadKey} query={query} density={density} onOpen={onOpen} filterCard={priority || due || q ? filterCard : undefined} />
    </div>
  );
}
