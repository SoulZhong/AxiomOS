"use client";
import { useMemo, useState, type ReactNode } from "react";
import { api, type CostGroup } from "@/lib/api";
import { fmtHours, fmtMoney, fmtPercent, fmtTokens, parseDate, startOfWeek, toISODate } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconAgent, IconUsage } from "@/components/icons";
import { BarList } from "@/components/BarList";
import { ArcGauge } from "@/components/instruments/ArcGauge";
import { Odometer } from "@/components/instruments/Odometer";
import { Sparkline, bucketByDay } from "@/components/instruments/Sparkline";
import { ThroughputBars } from "@/components/instruments/ThroughputBars";
import { ShipStatus } from "@/components/ship-status/ShipStatus";
import { Avatar, Empty, ErrorBox, ListSkeleton, PageHeader, Panel, Readout, Segmented, Skeleton, Table, cx } from "@/components/ui";

const COST_GROUPS: CostGroup[] = ["goal", "team", "executor", "model"];

/*
 * 运营看板（web/DESIGN.md「舰内系统 v3」§3 仪表级数据）。
 * 金额 / token 用滚动数字；验收通过率用弧形仪表；成本与吞吐卡右上角各一条 24 点迷你折线；吞吐柱带边光、本周柱呼吸。
 * 迷你折线的序列后端没有现成接口，从 GET /tasks 派生（见 series 注释）：
 *   - 成本：所有任务的执行记录（task.runs）按 started_at 归到"近 24 天，每天一格"，每格是当天开始的执行记录的成本合计；
 *   - 吞吐：已完成（state.label = terminal_success）的任务按 actual_end 归到近 24 天，每格是当天完成的任务数。
 */
export default function DashboardPage() {
  const { session } = useSession();
  const currency = session?.organization.currency;
  const [group, setGroup] = useState<CostGroup>("goal");
  const cost = useLoad(() => api.stats.cost(group), [group]);
  const cycle = useLoad(() => api.stats.cycle(), []);
  const throughput = useLoad(() => api.stats.throughput(), []);
  const agents = useLoad(() => api.stats.agents(), []);
  const tasks = useLoad(() => api.tasks.list(), []);

  const series = useMemo(() => {
    const rows = tasks.data;
    if (!rows) return null;
    return {
      cost: bucketByDay(rows.flatMap((x) => x.runs), (r) => r.started_at, (r) => r.cost),
      done: bucketByDay(rows.filter((x) => x.state.label === "terminal_success"), (x) => x.actual_end),
    };
  }, [tasks.data]);

  const total = (cost.data ?? []).reduce((s, c) => s + c.cost, 0);
  const cycleRows = cycle.data ?? [];
  const sample = cycleRows.reduce((s, c) => s + c.sample, 0);
  const avgCycle = sample ? cycleRows.reduce((s, c) => s + c.avg_hours * c.sample, 0) / sample : null;
  const weeks = throughput.data ?? [];
  const lastWeek = weeks[weeks.length - 1];
  const thisMonday = toISODate(startOfWeek(new Date()));
  const agentRows = agents.data ?? [];
  const runs = agentRows.reduce((s, a) => s + a.runs, 0);
  const avgSuccess = runs ? agentRows.reduce((s, a) => s + a.success_rate * a.runs, 0) / runs : null;

  return (
    <div>
      <PageHeader title={t("dashboard.title")} description={t("dashboard.description")} />
      {/* 舰况（舰内系统 v3 §2）：母舰模型 + 四组读数，看板顶部 */}
      <ShipStatus className="mb-4" />
      {/* 2×2；≥1920 一行四个 */}
      <div className="grid gap-4 md:grid-cols-2 3xl:grid-cols-4">
        <StatPanel
          index={1}
          icon={<IconUsage />}
          title={t("dashboard.cost")}
          big={cost.data ? <Odometer value={fmtMoney(total, currency)} /> : null}
          caption={t("dashboard.costCaption")}
          aside={series ? <Sparkline points={series.cost} label={t("dashboard.costSpark")} /> : <Skeleton className="h-7 w-24" />}
          actions={<Segmented size="sm" value={group} options={COST_GROUPS.map((k) => [k, t(`dashboard.group.${k}`)])} onChange={setGroup} />}
          loading={cost.loading && !cost.data}
          error={cost.error}
          onRetry={cost.reload}
        >
          <BarList
            limit={8}
            items={(cost.data ?? []).map((c) => ({
              key: c.key,
              title: c.title,
              value: c.cost,
              display: <Odometer value={fmtMoney(c.cost, currency)} />,
              sub: <Odometer value={t("dashboard.costSub", { tokens: fmtTokens(c.total_tokens), runs: c.run_count })} />,
            }))}
          />
        </StatPanel>

        <StatPanel index={2} title={t("dashboard.cycle")} big={cycle.data ? <Readout value={avgCycle === null ? "—" : fmtHours(avgCycle)} /> : null} caption={t("dashboard.cycleCaption", { n: sample })} loading={cycle.loading && !cycle.data} error={cycle.error} onRetry={cycle.reload}>
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
          index={3}
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

        <StatPanel
          index={4}
          icon={<IconAgent />}
          title={t("dashboard.agents")}
          big={agents.data ? (avgSuccess === null ? <Readout value="—" /> : <ArcGauge size={96} value={avgSuccess * 100} label={t("dashboard.successRate")} tone={avgSuccess >= 0.9 ? "success" : "accent"} />) : null}
          caption={t("dashboard.agentsCaption", { n: runs })}
          loading={agents.loading && !agents.data}
          error={agents.error}
          onRetry={agents.reload}
        >
          {agentRows.length === 0 ? (
            <Empty text={t("common.noData")} illustration={false} className="py-6" />
          ) : (
            <Table>
              <thead>
                <tr>
                  <th className="w-full">Agent</th>
                  <th className="num w-[56px]">{t("dashboard.runs")}</th>
                  <th className="w-[88px]">{t("dashboard.successRate")}</th>
                  <th className="w-[88px]">{t("dashboard.rejectRate")}</th>
                  <th className="num w-[80px]">{t("taskTable.cost")}</th>
                </tr>
              </thead>
              <tbody>
                {agentRows.map((a) => (
                  <tr key={a.agent.id}>
                    <td className="w-full max-w-0 min-w-[140px]">
                      <span className="flex items-center gap-2">
                        <Avatar name={a.agent.name} kind="agent" />
                        <span className="min-w-0">
                          <span className="block truncate" title={a.agent.name}>{a.agent.name}</span>
                          <span className="block text-caption text-ink-subtle">{a.owner.name}</span>
                        </span>
                      </span>
                    </td>
                    <td className="num">{a.runs}</td>
                    <td><Rate value={a.success_rate} tone="success" /></td>
                    <td><Rate value={a.reject_rate} tone="danger" /></td>
                    <td className="num whitespace-nowrap" title={fmtTokens(a.total_tokens)}><Odometer value={fmtMoney(a.cost, currency)} /></td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </StatPanel>
      </div>
    </div>
  );
}

/** 周起始日的短标签：8/31，不占宽度。 */
function shortDate(iso: string): string {
  const d = parseDate(iso);
  return d ? `${d.getMonth() + 1}/${d.getDate()}` : iso;
}

/**
 * 统计面板：序号眉标 + 一个大读数（24px/600；金额是滚动数字，通过率是弧形仪表）+ 一句说明（12px，基线对齐）
 * + 右上角的仪表位（aside：迷你折线）+ 一个小图。
 */
function StatPanel({ index, icon, title, big, caption, aside, actions, loading, error, onRetry, children }: { index: number; icon?: ReactNode; title: string; big: ReactNode | null; caption: string; aside?: ReactNode; actions?: ReactNode; loading: boolean; error: string | null; onRetry: () => void; children: ReactNode }) {
  return (
    <Panel index={index} icon={icon} title={title} actions={actions} padded={false}>
      <div className="flex items-end justify-between gap-4 px-4 pt-4 pb-3">
        <div className="flex min-w-0 items-baseline gap-2">
          {big === null ? <Skeleton className="h-6 w-24 self-center" /> : <span className="text-headline leading-none tabular-nums text-ink">{big}</span>}
          <span className="text-caption leading-none text-ink-subtle">{caption}</span>
        </div>
        {aside && <div className="shrink-0 self-start">{aside}</div>}
      </div>
      {loading ? <ListSkeleton rows={4} /> : error ? <div className="p-4"><ErrorBox message={error} onRetry={onRetry} /></div> : <div className="[&>.tbl-wrap]:border-t [&>.tbl-wrap]:border-hairline [&>:not(.tbl-wrap)]:px-4 [&>:not(.tbl-wrap)]:pb-4">{children}</div>}
    </Panel>
  );
}

function Rate({ value, tone }: { value: number; tone: "success" | "danger" }) {
  return (
    <span className="inline-flex items-center gap-2">
      <span className="h-1.5 w-10 overflow-hidden rounded-full bg-surface-3"><span className={cx("block h-full rounded-full", tone === "success" ? "bg-success" : "bg-danger")} style={{ width: `${value * 100}%` }} /></span>
      <span className="tabular-nums">{fmtPercent(value)}</span>
    </span>
  );
}
