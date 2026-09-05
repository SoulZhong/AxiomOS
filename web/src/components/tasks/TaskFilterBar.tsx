"use client";
import type { ReactNode } from "react";
import type { ExecutorRef, Goal, Sprint, TaskType, Team } from "@/lib/api";
import { t } from "@/lib/i18n";
import { STATE_LABELS, stateLabel } from "@/lib/terms";
import { Button, Select, Tip } from "@/components/ui";
import { FILTER_KEYS, filterBlocked, hasFilters, type FilterKey, type TaskFilters, type TaskView } from "./filters";

export interface FilterOptions {
  goals: Array<{ goal: Goal; depth: number }>;
  teams: Team[];
  executors: ExecutorRef[];
  types: TaskType[];
  sprints: Sprint[];
}

/**
 * 一条筛选栏管全部任务页签：目标 / 团队 / 负责人 / 类型 / 迭代 / 状态类型（DESIGN.md §10）。
 * 当前页签吃不下的控件禁用并在气泡里说明理由；值写在地址栏，切页签不丢。右侧留一个 trailing 位给各页签放自己的读数。
 */
export function TaskFilterBar({ view, filters, options, onChange, onClear, trailing }: { view: TaskView; filters: TaskFilters; options: FilterOptions; onChange: (key: FilterKey, value: string) => void; onClear: () => void; trailing?: ReactNode }) {
  const wrap = (key: FilterKey, node: ReactNode) => {
    const reason = filterBlocked(view, key);
    return reason ? <Tip key={key} tip={t(reason)} placement="bottom">{node}</Tip> : node;
  };
  const disabled = (key: FilterKey) => !!filterBlocked(view, key);
  const sel = (key: FilterKey, width: string, label: string, children: ReactNode) =>
    wrap(
      key,
      <Select key={key} value={filters[key]} onChange={(e) => onChange(key, e.target.value)} className={width} aria-label={label} disabled={disabled(key)} data-filter={key}>
        {children}
      </Select>,
    );
  // 六个控件 + 清除按钮在 1280 宽（内容区约 1010px）一行放得下；更窄时换行
  return (
    <div className="mb-4 flex flex-wrap items-center gap-2" data-filters={FILTER_KEYS.filter((k) => filters[k]).join(",") || undefined}>
      {sel("goal", "w-44", t("taskTable.goal"), <>
        <option value="">{t("tasks.allGoals")}</option>
        {options.goals.map(({ goal: g, depth }) => <option key={g.id} value={g.id}>{"　".repeat(depth)}{g.title}</option>)}
      </>)}
      {sel("team", "w-32", t("tasks.filter.team"), <>
        <option value="">{t("board.allTeams")}</option>
        {options.teams.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
      </>)}
      {sel("assignee", "w-36", t("taskTable.assignee"), <>
        <option value="">{t("tasks.allAssignees")}</option>
        <option value="me">{t("tasks.me")}</option>
        {options.executors.map((x) => <option key={x.id} value={x.id}>{x.name}{x.kind === "agent" ? t("common.agentSuffix") : ""}</option>)}
      </>)}
      {sel("type", "w-28", t("taskTable.type"), <>
        <option value="">{t("tasks.allTypes")}</option>
        {options.types.map((x) => <option key={x.name} value={x.name}>{x.title}</option>)}
      </>)}
      {sel("sprint", "w-32", t("tasks.filter.sprint"), <>
        <option value="">{t("board.allSprints")}</option>
        {options.sprints.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
      </>)}
      {sel("state", "w-36", t("tasks.filter.stateLabel"), <>
        <option value="">{t("tasks.allLabels")}</option>
        {STATE_LABELS.map((l) => <option key={l} value={l}>{stateLabel(l)}</option>)}
      </>)}
      {hasFilters(filters) && <Button variant="ghost" size="md" onClick={onClear}>{t("common.clearFilters")}</Button>}
      {trailing && <span className="ml-auto flex items-center gap-2 text-caption text-ink-subtle">{trailing}</span>}
    </div>
  );
}
