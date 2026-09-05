"use client";
import { api, type OverviewPeriod, type TrendPoint } from "@/lib/api";
import { fmtDate, parseDate } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { financeHiddenText, useScopeState } from "@/lib/useScope";
import { useSession } from "@/components/AppShell";
import { IconChart } from "@/components/icons";
import { ThroughputBars } from "@/components/instruments/ThroughputBars";
import { ScopeMoney } from "@/components/ScopePicker";
import { ListSkeleton } from "@/components/ui";
import { BlockPanel, type BlockProps } from "./BlockPanel";

/** 趋势与环比：这一段时间的成本折线（配上一段的对比线）与吞吐柱状图。首页默认按月；组织概览页把自己的时间段传进来。 */
export function TrendBlock({ index, title, noLink, period = "month" }: BlockProps & { period?: OverviewPeriod }) {
  const { session } = useSession();
  const currency = session?.organization.currency;
  const overview = useLoad(() => api.stats.overview(period), [period]);
  const data = overview.data;
  return (
    <BlockPanel
      index={index}
      noLink={noLink}
      icon={<IconChart />}
      title={title ?? t("block.trend")}
      telemetry={data ? t("overview.prevRange", { from: fmtDate(data.prev_range.from), to: fmtDate(data.prev_range.to) }) : undefined}
      href="/overview/"
      loading={overview.loading && !data}
      error={overview.error}
      onRetry={overview.reload}
      empty={!!data && data.trend.length === 0}
      emptyText={t("overview.noTrend")}
      skeleton={<ListSkeleton rows={4} />}
    >
      {data && data.trend.length > 0 && <Trend trend={data.trend} prev={data.prev_trend} prevCost={data.totals.prev.cost} currency={currency} />}
    </BlockPanel>
  );
}

function shortDate(iso: string): string {
  const d = parseDate(iso);
  return d ? `${d.getMonth() + 1}/${d.getDate()}` : iso;
}

export function Trend({ trend, prev, prevCost, currency }: { trend: TrendPoint[]; prev?: TrendPoint[]; prevCost: number | null; currency?: string }) {
  const scope = useScopeState();
  const cost = trend.map((p) => p.cost ?? 0);
  // 上一段时间的对比线：后端给了 prev_trend 就照画，没给就用上一段的平均值画一条横线
  const prevSeries = prev?.length === trend.length ? prev.map((p) => p.cost ?? 0) : prevCost !== null ? new Array<number>(trend.length).fill(prevCost / Math.max(1, trend.length)) : null;
  const prevLabel = prev?.length === trend.length ? t("overview.prevLine") : t("overview.prevAverage");
  const max = Math.max(...cost, ...(prevSeries ?? [0]), 1);
  return (
    <div className="grid gap-6 xl:grid-cols-2">
      <div className="min-w-0">
        <div className="mb-2 flex items-baseline justify-between gap-2">
          <span className="eyebrow text-ink-subtle">{t("overview.trendCost")}</span>
          <span className="text-caption text-ink-subtle">
            <ScopeMoney value={scope.financial ? cost.reduce((s, x) => s + x, 0) : null} currency={currency} />
          </span>
        </div>
        {scope.financial ? (
          <>
            <LineChart series={cost} compare={prevSeries} max={max} />
            <div className="mt-2 flex gap-4 text-caption text-ink-subtle">
              <span className="inline-flex items-center gap-1.5">
                <span className="inline-block h-0.5 w-4 rounded-full bg-accent" />
                {t("overview.trendCost")}
              </span>
              {prevSeries && (
                <span className="inline-flex items-center gap-1.5">
                  <span className="inline-block h-0.5 w-4 rounded-full bg-hairline-tertiary" />
                  {prevLabel}
                </span>
              )}
            </div>
          </>
        ) : (
          <p className="py-8 text-center text-body text-ink-subtle">{financeHiddenText(scope)}</p>
        )}
        <div className="mt-1 flex gap-2">
          {trend.map((p, i) => (
            <span key={p.bucket} className="min-w-0 flex-1 text-center font-mono text-[11px] tabular-nums text-ink-subtle">
              {(trend.length - 1 - i) % (trend.length > 8 ? 2 : 1) === 0 ? shortDate(p.bucket) : ""}
            </span>
          ))}
        </div>
      </div>
      <div className="min-w-0">
        <div className="mb-2 eyebrow text-ink-subtle">{t("overview.throughput")}</div>
        <ThroughputBars
          columns={trend.map((p, i) => ({
            key: p.bucket,
            label: shortDate(p.bucket),
            now: i === trend.length - 1,
            series: [
              { name: t("overview.trendCreated"), value: p.created, tone: "accent-soft" },
              { name: t("overview.trendDone"), value: p.done, tone: "accent" },
            ],
          }))}
        />
      </div>
    </div>
  );
}

/** 一条实线（本段）+ 一条淡线（上一段）。只画数据，不做动画。 */
function LineChart({ series, compare, max }: { series: number[]; compare: number[] | null; max: number }) {
  const path = (xs: number[]) =>
    xs
      .map((v, i) => `${i === 0 ? "M" : "L"} ${((i / Math.max(1, xs.length - 1)) * 100).toFixed(2)} ${(38 - (v / max) * 36).toFixed(2)}`)
      .join(" ");
  return (
    <svg width="100%" height={120} viewBox="0 0 100 40" preserveAspectRatio="none" role="img" aria-label={t("overview.trendCost")}>
      <line x1="0" y1="2" x2="100" y2="2" stroke="var(--c-hairline)" strokeWidth={1} vectorEffect="non-scaling-stroke" strokeDasharray="3 3" />
      <line x1="0" y1="20" x2="100" y2="20" stroke="var(--c-hairline)" strokeWidth={1} vectorEffect="non-scaling-stroke" strokeDasharray="3 3" />
      <line x1="0" y1="38" x2="100" y2="38" stroke="var(--c-hairline)" strokeWidth={1} vectorEffect="non-scaling-stroke" />
      {compare && <path d={path(compare)} fill="none" stroke="var(--c-hairline-tertiary)" strokeWidth={1.5} vectorEffect="non-scaling-stroke" strokeDasharray="4 4" />}
      <path d={path(series)} fill="none" stroke="var(--c-accent)" strokeWidth={1.5} vectorEffect="non-scaling-stroke" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}
