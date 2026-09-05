"use client";
import { api } from "@/lib/api";
import { fmtHours, startOfWeek, toISODate } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { Sparkline } from "@/components/instruments/Sparkline";
import { ThroughputBars } from "@/components/instruments/ThroughputBars";
import { Empty, Readout, Skeleton, Table } from "@/components/ui";
import { shortDate, StatPanel, useDaySeries } from "./StatPanel";

/** 组织概览 · 效率：周期（创建到完成，按任务类型）与每周吞吐。 */
export function EfficiencyTab() {
  const cycle = useLoad(() => api.stats.cycle(), []);
  const throughput = useLoad(() => api.stats.throughput(), []);
  const series = useDaySeries();
  const cycleRows = cycle.data ?? [];
  const sample = cycleRows.reduce((s, c) => s + c.sample, 0);
  const avgCycle = sample ? cycleRows.reduce((s, c) => s + c.avg_hours * c.sample, 0) / sample : null;
  const weeks = throughput.data ?? [];
  const lastWeek = weeks[weeks.length - 1];
  const thisMonday = toISODate(startOfWeek(new Date()));
  return (
    <div className="grid items-start gap-4 xl:grid-cols-2">
      <StatPanel index={1} title={t("dashboard.cycle")} big={cycle.data ? <Readout value={avgCycle === null ? "—" : fmtHours(avgCycle)} /> : null} caption={t("dashboard.cycleCaption", { n: sample })} loading={cycle.loading && !cycle.data} error={cycle.error} onRetry={cycle.reload}>
        {cycleRows.length === 0 ? (
          <Empty text={t("common.noData")} illustration={false} className="py-6" />
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-full">{t("dashboard.taskType")}</th>
                <th className="num w-[64px]">{t("dashboard.sample")}</th>
                <th className="num w-[88px]">{t("dashboard.avg")}</th>
                <th className="num w-[88px]">{t("dashboard.median")}</th>
                <th className="num w-[88px]">{t("dashboard.active")}</th>
              </tr>
            </thead>
            <tbody>
              {cycleRows.map((c) => {
                const max = Math.max(...cycleRows.map((x) => x.avg_hours), 1);
                return (
                  <tr key={c.type}>
                    <td>
                      {c.type_title}
                      <div className="mt-1 h-1.5 w-full max-w-40 overflow-hidden rounded-full bg-surface-3">
                        <div className="h-full rounded-full bg-accent" style={{ width: `${(c.avg_hours / max) * 100}%` }} />
                      </div>
                    </td>
                    <td className="num">{c.sample}</td>
                    <td className="num whitespace-nowrap">{fmtHours(c.avg_hours)}</td>
                    <td className="num whitespace-nowrap">{fmtHours(c.median_hours)}</td>
                    <td className="num whitespace-nowrap">{fmtHours(c.avg_active_hours)}</td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        )}
      </StatPanel>

      <StatPanel
        index={2}
        title={t("dashboard.throughput")}
        big={throughput.data ? <Readout value={String(lastWeek?.done ?? 0)} /> : null}
        caption={t("dashboard.throughputCaption")}
        aside={series ? <Sparkline points={series.done} label={t("dashboard.throughputSpark")} /> : <Skeleton className="h-7 w-24" />}
        loading={throughput.loading && !throughput.data}
        error={throughput.error}
        onRetry={throughput.reload}
      >
        <ThroughputBars
          columns={weeks.map((w) => {
            const now = w.week === thisMonday;
            return {
              key: w.week,
              label: now ? t("dashboard.thisWeek") : shortDate(w.week),
              now,
              series: [
                { name: t("dashboard.created"), value: w.created, tone: "accent-soft" },
                { name: t("dashboard.done"), value: w.done, tone: "accent" },
                { name: t("dashboard.terminated"), value: w.terminated, tone: "neutral" },
              ],
            };
          })}
        />
      </StatPanel>
    </div>
  );
}
