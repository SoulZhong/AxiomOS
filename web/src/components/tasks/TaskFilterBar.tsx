"use client";
import { useCallback, useEffect, useRef, type ReactNode } from "react";
import type { ExecutorRef, Goal, Sprint, TaskType, Team } from "@/lib/api";
import { useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { PRIORITIES, priorityTitle, STATE_LABELS, stateLabel } from "@/lib/terms";
import { IconClose, IconSearch } from "@/components/icons";
import { useShortcutHandler } from "@/components/shortcuts";
import { Button, Kbd, Select, Tip, cx, inputCls } from "@/components/ui";
import { DUE_FILTERS, FILTER_KEYS, filterBlocked, hasFilters, isDueFilter, type FilterKey, type TaskFilters, type TaskView } from "./filters";

export interface FilterOptions {
  goals: Array<{ goal: Goal; depth: number }>;
  teams: Team[];
  executors: ExecutorRef[];
  types: TaskType[];
  sprints: Sprint[];
}

/**
 * 一条筛选栏管全部任务页签（DESIGN.md §10、§18）：关键词 + 目标 / 团队 / 负责人 / 类型 / 迭代 / 状态类型 / 优先级 / 截止日。
 * 值全在地址栏，切页签不丢；当前页签吃不下的控件禁用并在气泡里说明理由。
 * 生效中的条件在下一行显示为芯片，每枚可以单独 × 掉，末尾「清空」一键清掉全部；这样用户看得见"我现在看的是什么"。
 * 关键词输入框：`f` 或 `/` 聚焦（全局快捷键交给这里接管）。右侧 trailing 位给各页签放自己的读数。
 */
export function TaskFilterBar({ view, filters, options, onChange, onClear, trailing }: { view: TaskView; filters: TaskFilters; options: FilterOptions; onChange: (key: FilterKey, value: string) => void; onClear: () => void; trailing?: ReactNode }) {
  const inputRef = useRef<HTMLInputElement>(null);
  const focus = useCallback(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);
  useShortcutHandler("filter", focus);
  useShortcutHandler("search", focus);
  const qFocus = useQueryParam("focus");
  useEffect(() => {
    if (qFocus === "search") focus();
  }, [qFocus, focus]);

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

  // 芯片上显示的值：id → 名字
  const valueLabel = (key: FilterKey, v: string): string => {
    switch (key) {
      case "goal": return options.goals.find((g) => g.goal.id === v)?.goal.title ?? v;
      case "team": return options.teams.find((x) => x.id === v)?.name ?? v;
      case "assignee": return v === "me" ? t("tasks.me") : options.executors.find((x) => x.id === v)?.name ?? v;
      case "type": return options.types.find((x) => x.name === v)?.title ?? v;
      case "sprint": return options.sprints.find((x) => x.id === v)?.name ?? v;
      case "state": return stateLabel(v as (typeof STATE_LABELS)[number]);
      case "priority": return priorityTitle(v as (typeof PRIORITIES)[number]);
      case "due": return isDueFilter(v) ? t(`tasks.due.${v}`) : v;
      case "q": return `“${v}”`;
    }
  };
  const keyLabel: Record<FilterKey, string> = {
    goal: t("taskTable.goal"), team: t("tasks.filter.team"), assignee: t("taskTable.assignee"), type: t("taskTable.type"), sprint: t("tasks.filter.sprint"),
    state: t("tasks.filter.stateLabel"), priority: t("taskTable.priority"), due: t("tasks.filter.due"), q: t("tasks.filter.keyword"),
  };
  const active = FILTER_KEYS.filter((k) => !!filters[k]);

  return (
    <div className="mb-4" data-filters={active.join(",") || undefined}>
      <div className="flex flex-wrap items-center gap-2">
        <label className="relative block w-full sm:w-56">
          <span className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-subtle"><IconSearch /></span>
          <input
            ref={inputRef}
            value={filters.q}
            onChange={(e) => onChange("q", e.target.value)}
            onKeyDown={(e) => { if (e.key === "Escape") { e.preventDefault(); (e.target as HTMLInputElement).blur(); } }}
            placeholder={t("tasks.search")}
            aria-label={t("tasks.search")}
            className={cx(inputCls, "w-full pl-8 pr-9")}
            disabled={disabled("q")}
            data-filter="q"
          />
          <span className="pointer-events-none absolute top-1/2 right-2 -translate-y-1/2"><Kbd>f</Kbd></span>
        </label>
        {sel("goal", "w-40", t("taskTable.goal"), <>
          <option value="">{t("tasks.allGoals")}</option>
          {options.goals.map(({ goal: g, depth }) => <option key={g.id} value={g.id}>{"　".repeat(depth)}{g.title}</option>)}
        </>)}
        {sel("team", "w-28", t("tasks.filter.team"), <>
          <option value="">{t("board.allTeams")}</option>
          {options.teams.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
        </>)}
        {sel("assignee", "w-32", t("taskTable.assignee"), <>
          <option value="">{t("tasks.allAssignees")}</option>
          <option value="me">{t("tasks.me")}</option>
          {options.executors.map((x) => <option key={x.id} value={x.id}>{x.name}{x.kind === "agent" ? t("common.agentSuffix") : ""}</option>)}
        </>)}
        {sel("type", "w-28", t("taskTable.type"), <>
          <option value="">{t("tasks.allTypes")}</option>
          {options.types.map((x) => <option key={x.name} value={x.name}>{x.title}</option>)}
        </>)}
        {sel("sprint", "w-28", t("tasks.filter.sprint"), <>
          <option value="">{t("board.allSprints")}</option>
          {options.sprints.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
        </>)}
        {sel("state", "w-32", t("tasks.filter.stateLabel"), <>
          <option value="">{t("tasks.allLabels")}</option>
          {STATE_LABELS.map((l) => <option key={l} value={l}>{stateLabel(l)}</option>)}
        </>)}
        {sel("priority", "w-28", t("taskTable.priority"), <>
          <option value="">{t("tasks.allPriorities")}</option>
          {PRIORITIES.map((p) => <option key={p} value={p}>{priorityTitle(p)}</option>)}
        </>)}
        {sel("due", "w-28", t("tasks.filter.due"), <>
          <option value="">{t("tasks.allDue")}</option>
          {DUE_FILTERS.map((d) => <option key={d} value={d}>{t(`tasks.due.${d}`)}</option>)}
        </>)}
        {trailing && <span className="ml-auto flex items-center gap-2 text-caption text-ink-subtle">{trailing}</span>}
      </div>
      {hasFilters(filters) && (
        <div className="mt-2 flex flex-wrap items-center gap-1.5" role="list" aria-label={t("tasks.filter.active")} data-filter-chips>
          {active.map((k) => {
            const blocked = filterBlocked(view, k);
            return (
              <span key={k} role="listitem" className={cx("filter-chip", blocked && "is-off")} title={blocked ? t(blocked) : undefined} data-chip={k}>
                <span className="text-ink-subtle">{keyLabel[k]}</span>
                <span className="max-w-[200px] truncate text-ink">{valueLabel(k, filters[k])}</span>
                <button type="button" className="filter-chip-x pressable" onClick={() => onChange(k, "")} aria-label={t("tasks.filter.remove", { name: keyLabel[k] })}>
                  <IconClose size={12} />
                </button>
              </span>
            );
          })}
          <Button variant="ghost" size="sm" onClick={onClear}>{t("common.clearFilters")}</Button>
        </div>
      )}
    </div>
  );
}
