"use client";
import { api, type LoadRow } from "@/lib/api";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconAgent } from "@/components/icons";
import { Avatar, ProgressBar, StatusLED, Table, TableSkeleton, Tip, cx } from "@/components/ui";
import { BlockPanel, type BlockProps } from "./BlockPanel";

/** 人员与 Agent 负荷：当前范围里每个执行者名下的活，按负荷从高到低（GET /stats/load）。 */
export function TeamLoadBlock({ index, title, noLink }: BlockProps) {
  const load = useLoad(() => api.stats.load(), []);
  const rows = load.data ?? [];
  return (
    <BlockPanel
      index={index}
      noLink={noLink}
      icon={<IconAgent />}
      title={title ?? t("block.team_load")}
      telemetry={load.data ? t("block.team_load.count", { n: rows.length }) : undefined}
      href="/overview/"
      padded={false}
      loading={load.loading && !load.data}
      error={load.error}
      onRetry={load.reload}
      empty={!!load.data && rows.length === 0}
      emptyText={t("overview.loadEmpty")}
      skeleton={<TableSkeleton rows={5} cols={5} />}
    >
      <LoadTable rows={rows} />
    </BlockPanel>
  );
}

export function LoadTable({ rows }: { rows: LoadRow[] }) {
  const max = Math.max(...rows.map((r) => r.points_open || r.open_tasks), 1);
  return (
    <Table>
      <thead>
        <tr>
          <th className="w-full min-w-[150px]">{t("overview.executor")}</th>
          <th className="w-[104px]">{t("overview.loadBar")}</th>
          <th className="num w-[112px]">{t("overview.doingSplit")}</th>
          <th className="num w-[72px]">{t("overview.points")}</th>
          <th className="num w-[84px]">{t("overview.plannedHours")}</th>
          <th className="num w-[64px]">{t("overview.overdue")}</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => {
          const agent = r.executor.kind === "agent";
          const teamName = typeof r.team === "string" ? r.team : (r.team?.title ?? null);
          return (
            <tr key={r.executor.id}>
              <td className="w-full max-w-0 min-w-[150px]">
                <span className="flex items-center gap-2">
                  <Avatar name={r.executor.name} kind={agent ? "agent" : "member"} />
                  <span className="min-w-0">
                    <span className="flex items-center gap-1.5">
                      <span className="truncate text-ink" title={r.executor.name}>
                        {r.executor.name}
                      </span>
                      {agent && (
                        <Tip tip={r.online ? t("overview.online") : t("overview.offline")}>
                          <StatusLED tone={r.online ? "online" : "dark"} />
                        </Tip>
                      )}
                    </span>
                    <span className="block truncate text-caption text-ink-subtle">
                      {[teamName, agent && r.max_concurrent ? t("overview.maxConcurrent", { n: r.max_concurrent }) : null, r.capacity_hint || null].filter(Boolean).join(" · ") || "—"}
                    </span>
                  </span>
                </span>
              </td>
              <td>
                <ProgressBar value={((r.points_open || r.open_tasks) / max) * 100} tone={r.overdue > 0 ? "danger" : "accent"} className="w-20" />
              </td>
              <td className="num whitespace-nowrap">
                <span className="text-ink">{r.active_tasks}</span>
                <span className="text-ink-tertiary"> / </span>
                <span className="text-ink-subtle">{Math.max(0, r.open_tasks - r.active_tasks)}</span>
              </td>
              <td className="num">{r.points_open}</td>
              <td className="num whitespace-nowrap">{r.planned_hours_this_week ? t("overview.hoursShort", { n: r.planned_hours_this_week }) : "—"}</td>
              <td className={cx("num", r.overdue > 0 && "text-danger")}>{r.overdue}</td>
            </tr>
          );
        })}
      </tbody>
    </Table>
  );
}
