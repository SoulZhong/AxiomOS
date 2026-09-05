"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { useLoad, useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconPlus, IconSearch, IconTask } from "@/components/icons";
import { useShortcutHandler } from "@/components/shortcuts";
import { isOverdue, TaskTable } from "@/components/TaskTable";
import { Button, ErrorBox, Kbd, Panel, StatChips, TableSkeleton, cx, inputCls } from "@/components/ui";
import { hasFilters, matchTask, type MatchCtx, type TaskFilters } from "./filters";

/**
 * 「任务 · 列表」：共用筛选栏之外，自己有搜索框（/ 聚焦）与四个状态计数芯片。
 * goal / assignee / type / sprint / state 交给 GET /tasks；team 在前端按负责人所属团队过滤（接口没有 team 参数）。
 * 地址栏里的 reviewer / creator（首页「等我验收」链接）原样传给接口。
 */
export function ListView({ filters, ctx, reloadKey, highlightId, onNew }: { filters: TaskFilters; ctx: MatchCtx; reloadKey: number; highlightId: string | null; onNew: () => void }) {
  const { session } = useSession();
  const qReviewer = useQueryParam("reviewer");
  const qCreator = useQueryParam("creator");
  const qFocus = useQueryParam("focus");
  const [q, setQ] = useState("");
  const [chip, setChip] = useState<string | null>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (qFocus === "search") searchRef.current?.focus();
  }, [qFocus]);
  useShortcutHandler("search", useCallback(() => searchRef.current?.focus(), []));

  const tasks = useLoad(
    () => api.tasks.list({ goal: filters.goal || undefined, assignee: filters.assignee || undefined, state: filters.state || undefined, type: filters.type || undefined, sprint: filters.sprint || undefined, reviewer: qReviewer || undefined, creator: qCreator || undefined }),
    [filters.goal, filters.assignee, filters.state, filters.type, filters.sprint, qReviewer, qCreator, reloadKey],
  );

  const all = (tasks.data ?? []).filter((x) => matchTask({ goalId: x.goal_id, assigneeId: x.assignee?.id ?? null, type: x.type, label: x.state.label, sprintId: x.sprint?.id ?? null }, filters, ctx, ["goal", "assignee", "type", "sprint", "state"]));
  const open = all.filter((x) => x.state.label !== "terminal_success" && x.state.label !== "terminal_failure");
  const active = all.filter((x) => x.state.label === "active");
  const overdue = all.filter(isOverdue);
  const waiting = all.filter((x) => x.state.label === "waiting");
  const needle = q.trim().toLowerCase();
  const shown = (chip === "active" ? active : chip === "overdue" ? overdue : chip === "waiting" ? waiting : chip === "open" ? open : all).filter((x) => !needle || x.title.toLowerCase().includes(needle) || x.id.toLowerCase().includes(needle));
  const filtered = hasFilters(filters) || !!q || !!chip || !!qReviewer || !!qCreator;
  const clearLocal = () => { setQ(""); setChip(null); };

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <label className="relative block w-full sm:w-64">
          <span className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-subtle"><IconSearch /></span>
          <input ref={searchRef} value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("tasks.search")} aria-label={t("tasks.search")} className={cx(inputCls, "w-full pl-8 pr-9")} data-shortcut-search />
          <span className="pointer-events-none absolute top-1/2 right-2 -translate-y-1/2"><Kbd>/</Kbd></span>
        </label>
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
          <TaskTable tasks={shown} currency={session?.organization.currency} highlightId={highlightId} onChanged={tasks.reload} emptyText={filtered ? t("tasks.noMatch") : undefined} emptyAction={filtered ? <Button onClick={clearLocal}>{t("common.clearFilters")}</Button> : <Button variant="primary" icon={<IconPlus />} onClick={onNew}>{t("tasks.new")}</Button>} />
        )}
      </Panel>
    </div>
  );
}
