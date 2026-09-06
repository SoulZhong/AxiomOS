"use client";
import { useState } from "react";
import { api } from "@/lib/api";
import { useLoad, useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconPlus, IconTask } from "@/components/icons";
import { isOverdue, TaskTable } from "@/components/TaskTable";
import { Button, ErrorBox, Panel, StatChips, TableSkeleton } from "@/components/ui";
import { hasFilters, matchTask, type MatchCtx, type TaskFilters } from "./filters";

/**
 * 「任务 · 列表」：共用筛选栏（含关键词）之外，自己有四个状态计数芯片。
 * goal / assignee / type / sprint / state 交给 GET /tasks；team / 优先级 / 截止日 / 关键词在前端过滤（接口没有这些参数）。
 * 地址栏里的 reviewer / creator（首页「等我验收」链接）原样传给接口。点行开抽屉（onOpen），标题链接进完整页。
 */
export function ListView({ filters, ctx, reloadKey, highlightId, onNew, onOpen, onClearFilters }: { filters: TaskFilters; ctx: MatchCtx; reloadKey: number; highlightId: string | null; onNew: () => void; onOpen: (id: string) => void; onClearFilters: () => void }) {
  const { session } = useSession();
  const qReviewer = useQueryParam("reviewer");
  const qCreator = useQueryParam("creator");
  const [chip, setChip] = useState<string | null>(null);

  const tasks = useLoad(
    () => api.tasks.list({ goal: filters.goal || undefined, assignee: filters.assignee || undefined, state: filters.state || undefined, type: filters.type || undefined, sprint: filters.sprint || undefined, reviewer: qReviewer || undefined, creator: qCreator || undefined }),
    [filters.goal, filters.assignee, filters.state, filters.type, filters.sprint, qReviewer, qCreator, reloadKey],
  );

  const all = (tasks.data ?? []).filter((x) => matchTask({ id: x.id, number: x.number, title: x.title, goalId: x.goal_id, assigneeId: x.assignee?.id ?? null, type: x.type, label: x.state.label, sprintId: x.sprint?.id ?? null, priority: x.priority, plannedEnd: x.planned_end }, filters, ctx, ["goal", "assignee", "type", "sprint", "state"]));
  const open = all.filter((x) => x.state.label !== "terminal_success" && x.state.label !== "terminal_failure");
  const active = all.filter((x) => x.state.label === "active");
  const overdue = all.filter(isOverdue);
  const waiting = all.filter((x) => x.state.label === "waiting");
  const shown = chip === "active" ? active : chip === "overdue" ? overdue : chip === "waiting" ? waiting : chip === "open" ? open : all;
  const filtered = hasFilters(filters) || !!chip || !!qReviewer || !!qCreator;
  const clearLocal = () => { setChip(null); onClearFilters(); };

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <StatChips
          value={chip}
          onChange={setChip}
          total={all.length}
          items={[
            { key: "open", label: t("chips.open"), value: open.length },
            { key: "active", label: t("home.active"), value: active.length, tone: "accent" },
            { key: "overdue", label: t("home.overdue"), value: overdue.length, tone: "danger" },
            { key: "waiting", label: t("home.waitingReview"), value: waiting.length, tone: "warning" },
          ]}
        />
        <span className="ml-auto text-caption text-ink-subtle">{t("tasks.count", { n: shown.length })}</span>
      </div>
      <Panel index={1} icon={<IconTask />} title={t("tasks.view.list")} telemetry={tasks.data ? t("panel.rows", { n: shown.length }) : undefined} padded={false}>
        {tasks.loading && !tasks.data ? <TableSkeleton rows={6} cols={7} /> : tasks.error ? <div className="p-4"><ErrorBox message={tasks.error} onRetry={tasks.reload} /></div> : (
          <TaskTable tasks={shown} currency={session?.organization.currency} highlightId={highlightId} onChanged={tasks.reload} onOpen={onOpen} emptyText={filtered ? t("tasks.noMatch") : t("tasks.emptyAll")} emptyAction={filtered ? <Button onClick={clearLocal}>{t("common.clearFilters")}</Button> : <Button variant="primary" icon={<IconPlus />} onClick={onNew}>{t("tasks.new")}</Button>} />
        )}
      </Panel>
    </div>
  );
}
