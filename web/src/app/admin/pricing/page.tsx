"use client";
import { useState } from "react";
import { api, type PriceModel } from "@/lib/api";
import { fmtNumber } from "@/lib/format";
import { useAction, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconEdit, IconPlus, IconTrash } from "@/components/icons";
import { PriceDialog } from "@/components/PriceDialog";
import { Button, Code, ConfirmDialog, Empty, ErrorBox, PageHeader, Panel, Table, TableSkeleton } from "@/components/ui";

export default function AdminPricingPage() {
  const prices = useLoad(() => api.admin.pricing(), []);
  const [editing, setEditing] = useState<PriceModel | null | "new">(null);
  const [removing, setRemoving] = useState<PriceModel | null>(null);
  const { busy, run } = useAction();

  const remove = async (m: PriceModel) => {
    if (await run(m.model_id, () => api.admin.deletePrice(m.model_id), t("toast.deleted"))) { setRemoving(null); prices.reload(); }
  };

  return (
    <div>
      <PageHeader title={t("admin.pricing.title")} description={t("admin.pricing.description")} actions={<Button variant="primary" icon={<IconPlus />} onClick={() => setEditing("new")}>{t("admin.pricing.new")}</Button>} />
      <Panel index={1} title={t("admin.nav.pricing")} telemetry={prices.data ? t("panel.rows", { n: prices.data.length }) : undefined} padded={false}>
        {prices.loading && !prices.data ? <TableSkeleton rows={4} cols={6} /> : prices.error ? <div className="p-4"><ErrorBox message={prices.error} onRetry={prices.reload} /></div> : !prices.data?.length ? <Empty text={t("admin.pricing.empty")} action={<Button variant="primary" icon={<IconPlus />} onClick={() => setEditing("new")}>{t("admin.pricing.new")}</Button>} /> : (
          <Table>
            <thead>
              <tr><th>{t("settings.pricing.model")}</th><th className="num">{t("settings.pricing.input")}</th><th className="num">{t("settings.pricing.output")}</th><th className="num">{t("settings.pricing.cacheRead")}</th><th className="num">{t("settings.pricing.cacheWrite")}</th><th>{t("settings.pricing.currency")}</th><th className="w-0" aria-label={t("common.actions")} /></tr>
            </thead>
            <tbody>
              {prices.data.map((m) => (
                <tr key={m.model_id}>
                  <td><Code className="text-ink">{m.model_id}</Code></td>
                  <td className="num">{fmtNumber(m.input_per_million)}</td>
                  <td className="num">{fmtNumber(m.output_per_million)}</td>
                  <td className="num">{fmtNumber(m.cache_read_per_million)}</td>
                  <td className="num">{fmtNumber(m.cache_write_per_million)}</td>
                  <td>{m.currency}</td>
                  <td className="whitespace-nowrap !py-1.5 align-middle">
                    <span className="row-actions inline-flex gap-1">
                      <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={() => setEditing(m)}>{t("common.edit")}</Button>
                      <Button size="sm" variant="ghost" className="text-danger hover:!text-danger" icon={<IconTrash />} disabled={busy === m.model_id} onClick={() => setRemoving(m)}>{t("common.delete")}</Button>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>
      <PriceDialog
        open={editing !== null}
        title={editing && editing !== "new" ? t("admin.pricing.edit") : t("admin.pricing.new")}
        initial={editing && editing !== "new" ? editing : null}
        lockModel={!!editing && editing !== "new"}
        defaultCurrency="CNY"
        onClose={() => setEditing(null)}
        onSave={async (id, input) => { await api.admin.savePrice(id, input); prices.reload(); }}
      />
      <ConfirmDialog open={!!removing} title={t("common.delete")} message={removing ? t("admin.pricing.deleteConfirm", { model: removing.model_id }) : null} confirmLabel={t("common.delete")} danger busy={!!busy} onConfirm={() => removing && void remove(removing)} onClose={() => setRemoving(null)} />
    </div>
  );
}
