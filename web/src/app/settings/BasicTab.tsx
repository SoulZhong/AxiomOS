"use client";
import { useState, type FormEvent } from "react";
import { api, type Locale } from "@/lib/api";
import { errorMessage, useLoad } from "@/lib/hooks";
import { LOCALE_NAMES, LOCALES, t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { useToast } from "@/components/toast";
import { Avatar, Button, Code, ErrorBox, Field, FormSection, Input, ListSkeleton, Panel, Select } from "@/components/ui";

export function BasicTab() {
  const { refresh } = useSession();
  const toast = useToast();
  const org = useLoad(() => api.org.get(), []);
  const [name, setName] = useState("");
  const [locale, setLocaleValue] = useState<Locale>("zh-CN");
  const [currency, setCurrency] = useState("CNY");
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 数据到达时填表（渲染期同步，避免 effect 里 setState）
  if (org.data && loadedFor !== org.data.id) {
    setLoadedFor(org.data.id);
    setName(org.data.name);
    setLocaleValue(org.data.default_locale);
    setCurrency(org.data.currency);
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      await api.org.update({ name: name.trim(), default_locale: locale, currency: currency.trim().toUpperCase() });
      toast.ok(t("toast.saved"));
      org.reload();
      refresh();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };

  if (org.loading && !org.data) return <Panel title={t("settings.tab.basic")}><ListSkeleton rows={3} /></Panel>;
  if (org.error && !org.data) return <ErrorBox message={org.error} onRetry={org.reload} />;

  return (
    <Panel index={1} title={t("settings.tab.basic")}>
      <form onSubmit={submit} className="max-w-[512px] space-y-4">
        <FormSection title={t("form.basic")}>
          <Field label={t("settings.basic.name")}><Input value={name} onChange={(e) => setName(e.target.value)} required /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("settings.basic.locale")} hint={t("settings.basic.localeHint")}>
              <Select value={locale} onChange={(e) => setLocaleValue(e.target.value as Locale)}>
                {LOCALES.map((l) => <option key={l} value={l}>{LOCALE_NAMES[l]}</option>)}
              </Select>
            </Field>
            <Field label={t("settings.basic.currency")} hint={t("settings.basic.currencyHint")}>
              <Input value={currency} onChange={(e) => setCurrency(e.target.value)} maxLength={3} className="uppercase" required />
            </Field>
          </div>
        </FormSection>
        <FormSection title={t("form.people")}>
          <div className="grid grid-cols-2 gap-3 text-body">
            <div>
              <div className="mb-1 text-caption text-ink-muted">{t("settings.basic.owner")}</div>
              <span className="inline-flex items-center gap-1.5"><Avatar name={org.data?.owner.name ?? ""} />{org.data?.owner.name}</span>
            </div>
            <div>
              <div className="mb-1 text-caption text-ink-muted">{t("settings.basic.slug")}</div>
              <Code>{org.data?.slug}</Code>
            </div>
          </div>
        </FormSection>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
        <div>
          <Button type="submit" variant="primary" disabled={busy}>{busy ? t("common.saving") : t("common.save")}</Button>
        </div>
      </form>
    </Panel>
  );
}
