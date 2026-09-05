"use client";
import { useState, type FormEvent } from "react";
import type { PriceInput, PriceModel } from "@/lib/api";
import { errorMessage } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useToast } from "./toast";
import { Button, Drawer, Field, Input } from "./ui";

/** 编辑一条模型价格（每百万 token）。组织覆盖与全局价格表共用；用右侧抽屉。 */
export function PriceDialog({ open, title, initial, defaultCurrency, lockModel, onClose, onSave }: {
  open: boolean;
  title: string;
  initial?: PriceModel | null;
  defaultCurrency: string;
  /** 编辑已有行时不能改模型标识 */
  lockModel?: boolean;
  onClose: () => void;
  onSave: (modelId: string, input: PriceInput) => Promise<void>;
}) {
  const toast = useToast();
  const [model, setModel] = useState("");
  const [input, setInput] = useState("");
  const [output, setOutput] = useState("");
  const [cacheRead, setCacheRead] = useState("");
  const [cacheWrite, setCacheWrite] = useState("");
  const [currency, setCurrency] = useState(defaultCurrency);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastOpen, setLastOpen] = useState(false);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (open) {
      setModel(initial?.model_id ?? "");
      setInput(initial ? String(initial.input_per_million) : "");
      setOutput(initial ? String(initial.output_per_million) : "");
      setCacheRead(initial ? String(initial.cache_read_per_million) : "");
      setCacheWrite(initial ? String(initial.cache_write_per_million) : "");
      setCurrency(initial?.currency ?? defaultCurrency);
      setError(null);
    }
  }
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      await onSave(model.trim(), {
        input_per_million: Number(input) || 0,
        output_per_million: Number(output) || 0,
        cache_read_per_million: Number(cacheRead) || 0,
        cache_write_per_million: Number(cacheWrite) || 0,
        currency: currency.trim().toUpperCase() || defaultCurrency,
      });
      toast.ok(t("toast.saved"));
      onClose();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <Drawer open={open} onClose={onClose} title={title} description={t("settings.pricing.hint")} footer={<><Button onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="price-form" type="submit" disabled={busy}>{t("common.save")}</Button></>}>
      <form id="price-form" onSubmit={submit} className="space-y-4">
        <div className="grid grid-cols-[1fr_100px] gap-3">
          <Field label={t("settings.pricing.modelId")}><Input value={model} onChange={(e) => setModel(e.target.value)} required disabled={lockModel} placeholder="claude-sonnet-5" className="font-mono" autoFocus={!lockModel} /></Field>
          <Field label={t("settings.pricing.currency")}><Input value={currency} onChange={(e) => setCurrency(e.target.value)} maxLength={3} className="uppercase" /></Field>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label={t("settings.pricing.input")}><Input type="number" min={0} step="0.001" value={input} onChange={(e) => setInput(e.target.value)} required autoFocus={lockModel} /></Field>
          <Field label={t("settings.pricing.output")}><Input type="number" min={0} step="0.001" value={output} onChange={(e) => setOutput(e.target.value)} required /></Field>
          <Field label={t("settings.pricing.cacheRead")}><Input type="number" min={0} step="0.001" value={cacheRead} onChange={(e) => setCacheRead(e.target.value)} /></Field>
          <Field label={t("settings.pricing.cacheWrite")}><Input type="number" min={0} step="0.001" value={cacheWrite} onChange={(e) => setCacheWrite(e.target.value)} /></Field>
        </div>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
