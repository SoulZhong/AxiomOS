"use client";
import { useState, type FormEvent } from "react";
import { api, type OrgPriceModel } from "@/lib/api";
import { fmtNumber } from "@/lib/format";
import { errorMessage, useAction, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconEdit, IconPlus } from "@/components/icons";
import { PriceDialog } from "@/components/PriceDialog";
import { useToast } from "@/components/toast";
import { Button, Code, ConfirmDialog, Empty, ErrorBox, Field, Input, Panel, Table, TableSkeleton, Tag } from "@/components/ui";

export function PricingTab() {
  const pricing = useLoad(() => api.org.pricing(), []);
  const [editing, setEditing] = useState<OrgPriceModel | null | "new">(null);
  const [removing, setRemoving] = useState<OrgPriceModel | null>(null);
  const { busy, run } = useAction();
  const currency = pricing.data?.currency ?? "CNY";

  const removeOverride = async (m: OrgPriceModel) => {
    if (await run(m.model_id, () => api.org.deletePrice(m.model_id), t("toast.saved"))) { setRemoving(null); pricing.reload(); }
  };

  return (
    <div className="space-y-4">
      <Panel index={1} title={t("settings.pricing.models")} padded={false} actions={<Button size="sm" variant="primary" icon={<IconPlus />} onClick={() => setEditing("new")}>{t("settings.pricing.addModel")}</Button>}>
        {pricing.loading && !pricing.data ? <TableSkeleton rows={4} cols={7} /> : pricing.error ? <div className="p-4"><ErrorBox message={pricing.error} onRetry={pricing.reload} /></div> : !pricing.data?.models.length ? <Empty text={t("settings.pricing.empty")} /> : (
          <Table>
            <thead>
              <tr><th>{t("settings.pricing.model")}</th><th className="num">{t("settings.pricing.input")}</th><th className="num">{t("settings.pricing.output")}</th><th className="num">{t("settings.pricing.cacheRead")}</th><th className="num">{t("settings.pricing.cacheWrite")}</th><th>{t("settings.pricing.currency")}</th><th>{t("settings.pricing.source")}</th><th className="w-0" aria-label={t("common.actions")} /></tr>
            </thead>
            <tbody>
              {pricing.data.models.map((m) => (
                <tr key={m.model_id}>
                  <td><Code className="text-ink">{m.model_id}</Code></td>
                  <td className="num">{fmtNumber(m.input_per_million)}</td>
                  <td className="num">{fmtNumber(m.output_per_million)}</td>
                  <td className="num">{fmtNumber(m.cache_read_per_million)}</td>
                  <td className="num">{fmtNumber(m.cache_write_per_million)}</td>
                  <td>{m.currency}</td>
                  <td>{m.source === "override" ? <Tag tone="accent">{t("settings.pricing.override")}</Tag> : <Tag>{t("settings.pricing.global")}</Tag>}</td>
                  <td className="whitespace-nowrap !py-1.5 align-middle">
                    <span className="row-actions inline-flex gap-1">
                      <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={() => setEditing(m)}>{m.source === "override" ? t("settings.pricing.editOverride") : t("settings.pricing.setOverride")}</Button>
                      {m.source === "override" && <Button size="sm" variant="ghost" disabled={busy === m.model_id} onClick={() => setRemoving(m)}>{t("settings.pricing.removeOverride")}</Button>}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
        <PriceDialog
          open={editing !== null}
          title={editing && editing !== "new" ? t("settings.pricing.editOverride") : t("settings.pricing.addModel")}
          initial={editing && editing !== "new" ? editing : null}
          lockModel={!!editing && editing !== "new"}
          defaultCurrency={currency}
          onClose={() => setEditing(null)}
          onSave={async (id, input) => { await api.org.savePrice(id, input); pricing.reload(); }}
        />
        <ConfirmDialog open={!!removing} title={t("settings.pricing.removeOverride")} message={removing ? t("settings.pricing.removeOverrideConfirm", { model: removing.model_id }) : null} busy={!!busy} onConfirm={() => removing && void removeOverride(removing)} onClose={() => setRemoving(null)} />
      </Panel>

      <Panel index={2} title={t("settings.pricing.rates")}>
        {pricing.data && (
          <div className="mb-4">
            {pricing.data.exchange_rates.length === 0 ? <p className="text-body text-ink-subtle">{t("settings.pricing.noRates")}</p> : (
              <ul className="flex flex-wrap gap-2 text-body">
                {pricing.data.exchange_rates.map((r) => <li key={`${r.from}-${r.to}`} className="rounded-md border border-hairline bg-surface-2 px-2.5 py-1 tabular-nums">1 {r.from} = {r.rate} {r.to}</li>)}
              </ul>
            )}
          </div>
        )}
        <RateForm defaultTo={currency} onSaved={pricing.reload} />
      </Panel>
    </div>
  );
}

function RateForm({ defaultTo, onSaved }: { defaultTo: string; onSaved: () => void }) {
  const toast = useToast();
  const [from, setFrom] = useState("USD");
  const [to, setTo] = useState(defaultTo);
  const [rate, setRate] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [seenDefault, setSeenDefault] = useState(defaultTo);
  if (seenDefault !== defaultTo) { setSeenDefault(defaultTo); setTo(defaultTo); }
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try { await api.org.saveRate({ from: from.trim().toUpperCase(), to: to.trim().toUpperCase(), rate: Number(rate) }); toast.ok(t("toast.saved")); setRate(""); onSaved(); }
    catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <form onSubmit={submit} className="flex flex-wrap items-end gap-3">
      <Field label={t("settings.pricing.from")} className="w-24"><Input value={from} onChange={(e) => setFrom(e.target.value)} maxLength={3} className="uppercase" required /></Field>
      <Field label={t("settings.pricing.to")} className="w-24"><Input value={to} onChange={(e) => setTo(e.target.value)} maxLength={3} className="uppercase" required /></Field>
      <Field label={t("settings.pricing.rate")} className="w-32"><Input type="number" min={0} step="0.0001" value={rate} onChange={(e) => setRate(e.target.value)} required /></Field>
      <Button type="submit" disabled={busy}>{t("settings.pricing.saveRate")}</Button>
      {error && <span className="text-caption text-danger" role="alert">{error}</span>}
    </form>
  );
}
