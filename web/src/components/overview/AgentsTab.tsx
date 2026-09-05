"use client";
import { api } from "@/lib/api";
import { fmtMoney, fmtTokens } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconAgent } from "@/components/icons";
import { ArcGauge } from "@/components/instruments/ArcGauge";
import { Odometer } from "@/components/instruments/Odometer";
import { Avatar, Empty, Readout, Table } from "@/components/ui";
import { Rate, StatPanel } from "./StatPanel";

/** 组织概览 · Agent：每个 Agent 的执行段数、验收通过率、打回率与成本；大读数是加权平均通过率（弧形仪表）。 */
export function AgentsTab() {
  const { session } = useSession();
  const currency = session?.organization.currency;
  const agents = useLoad(() => api.stats.agents(), []);
  const rows = agents.data ?? [];
  const runs = rows.reduce((s, a) => s + a.runs, 0);
  const avgSuccess = runs ? rows.reduce((s, a) => s + a.success_rate * a.runs, 0) / runs : null;
  return (
    <StatPanel
      index={1}
      icon={<IconAgent />}
      title={t("dashboard.agents")}
      big={agents.data ? (avgSuccess === null ? <Readout value="—" /> : <ArcGauge size={96} value={avgSuccess * 100} label={t("dashboard.successRate")} tone={avgSuccess >= 0.9 ? "success" : "accent"} />) : null}
      caption={t("dashboard.agentsCaption", { n: runs })}
      loading={agents.loading && !agents.data}
      error={agents.error}
      onRetry={agents.reload}
    >
      {rows.length === 0 ? (
        <Empty text={t("common.noData")} illustration={false} className="py-6" />
      ) : (
        <Table>
          <thead>
            <tr>
              <th className="w-full">Agent</th>
              <th className="w-[160px]">{t("dashboard.owner")}</th>
              <th className="num w-[96px]">{t("dashboard.runs")}</th>
              <th className="w-[140px]">{t("dashboard.successRate")}</th>
              <th className="w-[140px]">{t("dashboard.rejectRate")}</th>
              <th className="num w-[120px]">{t("taskTable.cost")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((a) => (
              <tr key={a.agent.id}>
                <td className="w-full max-w-0 min-w-[140px]">
                  <span className="flex items-center gap-2">
                    <Avatar name={a.agent.name} kind="agent" />
                    <span className="truncate" title={a.agent.name}>{a.agent.name}</span>
                  </span>
                </td>
                <td className="truncate text-ink-muted">{a.owner.name}</td>
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
  );
}
