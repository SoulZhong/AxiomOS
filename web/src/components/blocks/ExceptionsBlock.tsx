"use client";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { api, type ExceptionGoal, type ExceptionTask, type ExceptionsData } from "@/lib/api";
import { useAction, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconOverdue } from "@/components/icons";
import { ScopeMoney } from "@/components/ScopePicker";
import { StateBadge } from "@/components/StateBadge";
import { Button, ListSkeleton } from "@/components/ui";
import { BlockPanel, CompactList, type BlockProps } from "./BlockPanel";

/*
 * 要我关注的异常：逾期任务、停滞任务、逾期目标、超预算目标、等我确认的操作，按组分列，每组默认 5 条。
 * 目标"任务都完成了，只差确认达成"时行内直接给确认按钮。数据按当前范围（GET /stats/exceptions）。
 */
export function ExceptionsBlock({ index, title, noLink, compact, dense }: BlockProps) {
  const { session } = useSession();
  const currency = session?.organization.currency;
  const exceptions = useLoad(() => api.stats.exceptions(), []);
  const d = exceptions.data;
  const count = d ? d.overdue_tasks.length + d.stuck_tasks.length + d.overdue_goals.length + d.over_budget_goals.length + d.pending_proposals.length : 0;
  return (
    <BlockPanel
      index={index}
      noLink={noLink}
      compact={compact}
      icon={<IconOverdue />}
      title={title ?? t("block.exceptions")}
      telemetry={d ? t("panel.rows", { n: count }) : undefined}
      href="/overview/"
      padded={false}
      loading={exceptions.loading && !d}
      error={exceptions.error}
      onRetry={exceptions.reload}
      empty={!!d && count === 0}
      emptyText={t("overview.exceptionsClear")}
      skeleton={<ListSkeleton rows={5} />}
    >
      {d && (compact || dense) && <CompactList count={count} label={t("block.exceptions.compact")} rows={compactRows(d)} dense={dense} />}
      {d && !compact && !dense && <Exceptions data={d} currency={currency} onChanged={exceptions.reload} />}
    </BlockPanel>
  );
}

/** 窄区块：五组按紧急度拍平，只取前三条（逾期任务 → 停滞 → 逾期目标 → 超预算 → 待确认） */
function compactRows(d: ExceptionsData) {
  return [
    ...d.overdue_tasks.map((x) => ({ key: `t-${x.id}`, title: taskRow(x, t("overview.overdueDays", { n: x.overdue_days ?? x.days_overdue ?? 0 })).title, meta: <span className="text-danger">{t("overview.overdueDays", { n: x.overdue_days ?? x.days_overdue ?? 0 })}</span> })),
    ...d.stuck_tasks.map((x) => ({ key: `s-${x.task.id}`, title: taskRow(x.task, "").title, meta: t("overview.stuckDays", { n: x.days_in_state }) })),
    ...d.overdue_goals.map((g) => ({ key: `g-${g.id}`, title: goalRow(g, "").title, meta: t("overview.overdueDays", { n: g.overdue_days ?? 0 }) })),
    ...d.over_budget_goals.map((g) => ({ key: `b-${g.id}`, title: goalRow(g, "").title, meta: t("overview.overBudgetGoals") })),
    ...d.pending_proposals.map((p) => ({ key: `p-${p.id}`, title: <span className="truncate text-ink">{p.summary}</span>, meta: p.agent.name })),
  ];
}

function Exceptions({ data, currency, onChanged }: { data: ExceptionsData; currency?: string; onChanged: () => void }) {
  const { busy, run } = useAction();
  const groups = [
    { key: "overdue_tasks", title: t("overview.overdueTasks"), rows: data.overdue_tasks.map((x) => taskRow(x, t("overview.overdueDays", { n: x.overdue_days ?? x.days_overdue ?? 0 }))) },
    { key: "stuck_tasks", title: t("overview.stuckTasks"), rows: data.stuck_tasks.map((x) => taskRow(x.task, t("overview.stuckDays", { n: x.days_in_state }))) },
    { key: "overdue_goals", title: t("overview.overdueGoals"), rows: data.overdue_goals.map((g) => goalRow(g, t("overview.overdueDays", { n: g.overdue_days ?? 0 }))) },
    {
      key: "over_budget_goals",
      title: t("overview.overBudgetGoals"),
      rows: data.over_budget_goals.map((g) =>
        goalRow(
          g,
          <span className="inline-flex items-center gap-1">
            <ScopeMoney value={g.cost ?? null} currency={currency} />
            <span className="text-ink-tertiary">/</span>
            <ScopeMoney value={g.budget ?? null} currency={currency} />
          </span>,
        ),
      ),
    },
    {
      key: "pending_proposals",
      title: t("overview.pendingProposals"),
      rows: data.pending_proposals.map((p) => ({
        key: p.id,
        title: <span className="truncate text-ink">{p.summary}</span>,
        meta: <span className="text-ink-subtle">{p.agent.name}</span>,
        action: (
          <Link href="/proposals/" className="text-caption text-ink-muted hover:text-accent-hover">
            {t("overview.goConfirm")}
          </Link>
        ),
      })),
    },
  ].filter((g) => g.rows.length > 0);

  const achievable = data.overdue_goals.concat(data.over_budget_goals).filter((g) => g.all_tasks_done && !g.achieved);

  return (
    <div className="divide-y divide-hairline">
      {groups.map((g) => (
        <ExceptionGroup key={g.key} title={g.title} rows={g.rows} />
      ))}
      {achievable.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 px-4 py-3">
          {achievable.map((g) => (
            <Button
              key={g.id}
              size="sm"
              disabled={busy === g.id}
              onClick={() => void run(g.id, () => api.goals.update(g.id, { achieved: true }), t("overview.markAchieved")).then((ok) => ok && onChanged())}
            >
              {t("overview.markAchieved")}：{g.title}
            </Button>
          ))}
        </div>
      )}
    </div>
  );
}

interface ExRow {
  key: string;
  title: ReactNode;
  meta: ReactNode;
  action: ReactNode;
}

function taskRow(x: ExceptionTask, meta: ReactNode): ExRow {
  return {
    key: x.id,
    title: (
      <Link href={`/tasks/${encodeURIComponent(x.id)}/`} className="min-w-0 truncate text-ink hover:text-accent-hover">
        {x.title}
      </Link>
    ),
    meta: (
      <span className="flex items-center gap-2">
        {x.state && <StateBadge state={x.state} showLabel={false} />}
        <span className="text-ink-subtle">{meta}</span>
        {x.assignee && <span className="text-ink-subtle">{typeof x.assignee === "string" ? x.assignee : x.assignee.name}</span>}
      </span>
    ),
    action: (
      <Link href={`/tasks/${encodeURIComponent(x.id)}/`} className="text-caption text-ink-muted hover:text-accent-hover">
        {t("overview.openTask")}
      </Link>
    ),
  };
}

function goalRow(g: ExceptionGoal, meta: ReactNode): ExRow {
  return {
    key: g.id,
    title: (
      <Link href={`/goals/${encodeURIComponent(g.id)}/`} className="min-w-0 truncate text-ink hover:text-accent-hover">
        {g.title}
      </Link>
    ),
    meta: (
      <span className="flex items-center gap-2 text-ink-subtle">
        {meta}
        {g.owner && <span>{g.owner.name}</span>}
      </span>
    ),
    action: (
      <Link href={`/goals/${encodeURIComponent(g.id)}/`} className="text-caption text-ink-muted hover:text-accent-hover">
        {t("overview.openGoal")}
      </Link>
    ),
  };
}

const EX_LIMIT = 5;
function ExceptionGroup({ title, rows }: { title: string; rows: ExRow[] }) {
  const [all, setAll] = useState(false);
  const shown = all ? rows : rows.slice(0, EX_LIMIT);
  return (
    <div className="px-4 py-3">
      <div className="mb-2 flex items-center gap-2">
        <span className="eyebrow text-ink-subtle">{title}</span>
        <span className="text-caption text-ink-tertiary tabular-nums">{rows.length}</span>
      </div>
      <ul className="space-y-1.5">
        {shown.map((r) => (
          <li key={r.key} className="flex min-w-0 items-center gap-3">
            <span className="min-w-0 flex-1 truncate text-body">{r.title}</span>
            <span className="hidden shrink-0 text-caption md:flex">{r.meta}</span>
            <span className="shrink-0">{r.action}</span>
          </li>
        ))}
      </ul>
      {rows.length > EX_LIMIT && !all && (
        <button type="button" className="mt-2 text-caption text-ink-muted hover:text-accent-hover" onClick={() => setAll(true)}>
          {t("overview.exceptionsMore", { n: rows.length - EX_LIMIT })}
        </button>
      )}
    </div>
  );
}
