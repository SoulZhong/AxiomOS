"use client";
import { api } from "@/lib/api";
import { fmtMoney } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { Empty, ErrorBox, PageHeader, Panel, StatChips, Table, TableSkeleton } from "@/components/ui";

export default function AdminOverviewPage() {
  const stats = useLoad(() => api.admin.stats(), []);
  const s = stats.data;
  return (
    <div>
      <PageHeader title={t("admin.overview.title")} description={t("admin.overview.description")} />
      {stats.error && !s ? <ErrorBox message={stats.error} onRetry={stats.reload} /> : (
        <>
          <StatChips
            className="mb-4"
            value={null}
            items={[
              { key: "orgs", label: t("admin.overview.organizations"), value: s?.organizations ?? "—" },
              { key: "members", label: t("admin.overview.members"), value: s?.members ?? "—" },
              { key: "agents", label: t("admin.overview.agents"), value: s?.agents ?? "—", tone: "accent" },
              { key: "runs", label: t("admin.overview.runs30d"), value: s?.runs_30d ?? "—" },
            ]}
          />
          <Panel index={1} title={t("admin.overview.cost30d")} telemetry={s ? t("panel.rows", { n: s.cost_30d.length }) : undefined} padded={false}>
            {!s ? <TableSkeleton rows={3} cols={2} /> : s.cost_30d.length === 0 ? <Empty text={t("admin.overview.noCost")} /> : (
              <Table>
                <thead><tr><th className="w-full">{t("admin.overview.org")}</th><th className="num w-[160px]">{t("admin.overview.cost")}</th></tr></thead>
                <tbody>
                  {s.cost_30d.map((c) => (
                    <tr key={c.org_id}>
                      <td>{c.name}</td>
                      <td className="num">{fmtMoney(c.cost, c.currency)}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </Panel>
        </>
      )}
    </div>
  );
}

