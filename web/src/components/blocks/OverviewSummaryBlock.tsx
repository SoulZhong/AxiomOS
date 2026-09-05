"use client";
import type { ReactNode } from "react";
import { api } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconOverview } from "@/components/icons";
import { ScopeMoney } from "@/components/ScopePicker";
import { ProgressBar, Readout, Skeleton, cx } from "@/components/ui";
import { BlockPanel, type BlockProps } from "./BlockPanel";

/** 组织概览摘要：当前范围这一个月的合计，一行五个读数。对 CEO 是全公司、对部门负责人是本部（同一个区块、同一套口径）。 */
export function OverviewSummaryBlock({ index, title, noLink }: BlockProps) {
  const { session } = useSession();
  const currency = session?.organization.currency;
  const overview = useLoad(() => api.stats.overview("month"), []);
  const totals = overview.data?.totals;
  return (
    <BlockPanel
      index={index}
      noLink={noLink}
      icon={<IconOverview />}
      title={title ?? t("block.overview_summary")}
      telemetry={overview.data ? t("overview.range", { from: fmtDate(overview.data.range.from), to: fmtDate(overview.data.range.to) }) : undefined}
      href="/overview/"
      hrefLabel={t("home.overviewLink")}
      loading={overview.loading && !totals}
      error={overview.error}
      onRetry={overview.reload}
      empty={!overview.loading && !totals}
      emptyText={t("common.noData")}
      skeleton={
        <div className="grid grid-cols-2 gap-4 md:grid-cols-5">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i}>
              <Skeleton className="mb-2 h-3 w-16" />
              <Skeleton className="h-6 w-20" />
            </div>
          ))}
        </div>
      }
    >
      {totals && (
        <div className="grid grid-cols-2 gap-4 md:grid-cols-5">
          <Cell label={t("overview.goalProgress")}>
            <span className="flex items-center gap-2">
              <Readout value={`${Math.round(totals.goal_progress)}%`} />
              <ProgressBar value={totals.goal_progress} className="w-12" />
            </span>
          </Cell>
          <Cell label={t("overview.tasks")}>
            <Readout value={`${totals.tasks_done} / ${totals.tasks_total}`} />
          </Cell>
          <Cell label={t("overview.overdue")} tone={totals.overdue > 0 ? "danger" : undefined}>
            <Readout value={totals.overdue} />
          </Cell>
          <Cell label={t("overview.throughput")}>
            <Readout value={totals.throughput} />
          </Cell>
          <Cell label={t("overview.cost")}>
            <ScopeMoney value={totals.cost} currency={currency} />
          </Cell>
        </div>
      )}
    </BlockPanel>
  );
}

function Cell({ label, tone, children }: { label: string; tone?: "danger"; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="eyebrow mb-1 text-ink-subtle">{label}</div>
      <div className={cx("text-title tabular-nums", tone === "danger" ? "text-danger" : "text-ink")}>{children}</div>
    </div>
  );
}
