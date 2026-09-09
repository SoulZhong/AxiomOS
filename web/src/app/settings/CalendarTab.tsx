"use client";
import { useState, type FormEvent } from "react";
import { api, type CalendarProviderView, type CalendarTestResult } from "@/lib/api";
import { errorMessage, useLoad } from "@/lib/hooks";
import { fmtRelative } from "@/lib/format";
import { t } from "@/lib/i18n";
import { useToast } from "@/components/toast";
import { Button, ConsequenceDialog, CopyLine, ErrorBox, ListSkeleton, Panel, Tag, cx } from "@/components/ui";
import { CheckRow, CredentialField, WizardLink } from "./wizard";

/*
 * 组织设置 → 外部日历（ADR 0032，DESIGN.md §27）：一家提供方一张卡，同时可以接好几家。
 * 每张卡：状态一行（已接入 / 未接入 · 最近同步 · 连接了几个人）→ 凭据（飞书 / 企业微信借用 IM 集成的，这里只补日历专用字段）
 *   → 保存即检查 → 检查项逐条给结论 → 立即同步 / 停用 / 断开。凭据类保密字段永不回显。
 * Google 是成员各自授权：这里只登记 OAuth 客户端，并给出要填进客户端的回调地址。
 */
export function CalendarTab() {
  const list = useLoad(() => api.org.calendars.list(), []);
  if (list.loading && !list.data) return <Panel index={1} title={t("settings.tab.calendar")}><ListSkeleton rows={3} /></Panel>;
  if (list.error && !list.data) return <ErrorBox message={list.error} onRetry={list.reload} />;
  const providers = list.data?.providers ?? [];
  return (
    <div className="space-y-4">
      <Panel index={1} title={t("settings.tab.calendar")}>
        <p className="mb-2 max-w-[640px] text-body text-ink-subtle">{t("settings.calendar.description")}</p>
        <p className="max-w-[640px] text-caption text-ink-tertiary">{t("settings.calendar.privacy")}</p>
      </Panel>
      {providers.map((p, i) => <ProviderCard key={p.provider} p={p} index={i + 2} onChanged={list.reload} />)}
    </div>
  );
}

function ProviderCard({ p, index, onChanged }: { p: CalendarProviderView; index: number; onChanged: () => void }) {
  const toast = useToast();
  const [editing, setEditing] = useState(!p.configured);
  const [test, setTest] = useState<CalendarTestResult | null>(null);
  const [testing, setTesting] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);

  const runTest = async () => {
    setTesting(true);
    try { setTest(await api.org.calendars.test(p.provider)); } catch (e) { toast.fail(errorMessage(e)); } finally { setTesting(false); }
  };
  const sync = async () => {
    setSyncing(true);
    try {
      const r = await api.org.calendars.sync(p.provider);
      if (r.status === "ok") toast.ok(t("settings.calendar.synced", { members: r.members, events: r.events }));
      else toast.fail(r.errors.join("；"));
      onChanged();
    } catch (e) { toast.fail(errorMessage(e)); } finally { setSyncing(false); }
  };
  const toggle = async () => {
    setBusy(true);
    try { await api.org.calendars.save(p.provider, { enabled: !p.enabled }); onChanged(); } catch (e) { toast.fail(errorMessage(e)); } finally { setBusy(false); }
  };
  const disconnect = async () => {
    setBusy(true);
    try { await api.org.calendars.disconnect(p.provider); setConfirm(false); setEditing(true); setTest(null); onChanged(); toast.ok(t("settings.calendar.disconnected", { name: p.provider_title })); }
    catch (e) { toast.fail(errorMessage(e)); } finally { setBusy(false); }
  };

  const status = !p.configured ? "off" : !p.enabled ? "paused" : p.last_status === "failed" ? "failed" : p.last_status === "ok" ? "ok" : "new";
  return (
    <Panel index={index} title={p.provider_title}>
      <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-body" data-calendar-provider={p.provider} data-calendar-status={status}>
        <span className={cx("h-2 w-2 shrink-0 rounded-full", status === "ok" ? "bg-success" : status === "failed" ? "bg-danger" : status === "paused" ? "bg-warning" : "bg-neutral")} aria-hidden="true" />
        {p.configured ? <Tag tone={p.enabled ? "success" : "neutral"}>{p.enabled ? t("settings.calendar.connected") : t("settings.calendar.paused")}</Tag> : <Tag>{t("settings.calendar.notConnected")}</Tag>}
        {p.configured && (
          <span className="flex flex-wrap items-center gap-x-1.5 text-ink-muted">
            <span>{t("settings.calendar.members", { n: p.connected_members })}</span>
            <span aria-hidden="true">·</span>
            <span>{p.last_sync_at ? t("settings.calendar.lastSync", { when: fmtRelative(p.last_sync_at), status: p.last_status_title }) : t("settings.calendar.neverSynced")}</span>
          </span>
        )}
        {p.configured && (
          <span className="ml-auto flex flex-wrap items-center gap-2">
            <Button size="sm" disabled={syncing || !p.enabled} onClick={() => void sync()}>{syncing ? t("settings.calendar.syncing") : t("settings.calendar.syncNow")}</Button>
            <Button size="sm" variant="ghost" disabled={testing} onClick={() => void runTest()}>{testing ? t("settings.directory.wizard.checking") : t("settings.calendar.check")}</Button>
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => void toggle()}>{p.enabled ? t("settings.calendar.pause") : t("settings.calendar.resume")}</Button>
            <Button size="sm" variant="ghost" onClick={() => setEditing((v) => !v)}>{editing ? t("settings.directory.wizard.keep") : t("settings.calendar.editCreds")}</Button>
            <Button size="sm" variant="ghost" onClick={() => setConfirm(true)}>{t("settings.calendar.disconnect")}</Button>
          </span>
        )}
      </div>
      {p.last_error && <p className="mb-3 rounded-md border border-danger-border bg-danger-bg px-3 py-2 text-caption text-danger" role="alert">{p.last_error}</p>}
      {p.prerequisites.length > 0 && !p.configured && (
        <ol className="mb-3 list-decimal space-y-1 pl-5 text-caption text-ink-muted">{p.prerequisites.map((x, i) => <li key={i}>{x}</li>)}</ol>
      )}
      {p.per_member && p.redirect_url && (
        <div className="mb-3 max-w-[640px]">
          <p className="mb-1 text-caption text-ink-subtle">{t("settings.calendar.redirectHint")}</p>
          <CopyLine text={p.redirect_url} />
        </div>
      )}
      {p.inherits_directory && <p className="mb-3 text-caption text-ink-subtle">{t("settings.calendar.inherits", { name: p.provider_title })}</p>}
      {editing && <CredentialsForm p={p} onSaved={() => { setEditing(false); onChanged(); void runTest(); }} onCancel={p.configured ? () => setEditing(false) : undefined} />}
      {test && (
        <div className="mt-3 space-y-1" data-calendar-checks>
          {test.error && <p className="text-caption text-danger" role="alert">{test.error}</p>}
          {test.checks.map((c) => <CheckRow key={c.key} check={c} skipped={false} providerTitle={p.provider_title} />)}
          {!test.error && test.checks.length === 0 && test.ok && <p className="text-caption text-success">{t("settings.calendar.checkOk")}</p>}
        </div>
      )}
      <ConsequenceDialog open={confirm} title={t("settings.calendar.disconnectTitle", { name: p.provider_title })} subject={p.provider_title}
        effects={[t("settings.calendar.disconnectEffect1"), t("settings.calendar.disconnectEffect2")]} confirmLabel={t("settings.calendar.disconnect")} danger busy={busy} onConfirm={() => void disconnect()} onClose={() => setConfirm(false)} />
    </Panel>
  );
}

function CredentialsForm({ p, onSaved, onCancel }: { p: CalendarProviderView; onSaved: () => void; onCancel?: () => void }) {
  const toast = useToast();
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(p.fields.map((f) => [f.key, !f.secret ? p.credentials[f.key] ?? "" : ""])));
  const [resetting, setResetting] = useState<Record<string, boolean>>(() => Object.fromEntries(p.fields.filter((f) => f.secret).map((f) => [f.key, !p.secrets_set[f.key]])));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    const credentials: Record<string, string> = {};
    for (const f of p.fields) {
      const v = (values[f.key] ?? "").trim();
      if (!f.secret) { credentials[f.key] = v; continue; }
      if (resetting[f.key] && v) credentials[f.key] = v; // 省略保密字段 = 沿用已保存的
    }
    setBusy(true);
    try {
      await api.org.calendars.save(p.provider, { credentials, enabled: true });
      toast.ok(t("settings.calendar.saved", { name: p.provider_title }));
      onSaved();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <form onSubmit={submit} className="max-w-[640px] space-y-4" data-calendar-form>
      {p.tip && (
        <p className="text-caption text-ink-subtle">
          {p.tip.text}
          {p.tip.url && <> <WizardLink href={p.tip.url}>{t("settings.directory.openConsole", { name: p.provider_title })}</WizardLink></>}
        </p>
      )}
      {p.fields.length === 0 ? (
        <p className="text-body text-ink-muted">{t("settings.calendar.noFields")}</p>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          {p.fields.map((f) => (
            <CredentialField key={f.key} field={f} value={values[f.key] ?? ""} onChange={(v) => setValues((d) => ({ ...d, [f.key]: v }))}
              isSet={!!p.secrets_set[f.key]} resetting={!!resetting[f.key]} onReset={(on) => { setResetting((r) => ({ ...r, [f.key]: on })); if (!on) setValues((d) => ({ ...d, [f.key]: "" })); }} />
          ))}
        </div>
      )}
      {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" variant="primary" disabled={busy}>{busy ? t("settings.directory.wizard.checking") : t("settings.directory.wizard.saveAndCheck")}</Button>
        {onCancel && <Button variant="ghost" onClick={onCancel}>{t("settings.directory.wizard.keep")}</Button>}
      </div>
    </form>
  );
}
