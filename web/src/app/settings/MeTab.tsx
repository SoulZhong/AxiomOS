"use client";
import { useState, type FormEvent } from "react";
import { api, type MyCalendarView, type MyFeedView, type PreferencesPatch } from "@/lib/api";
import { fmtRelative } from "@/lib/format";
import { errorMessage, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { applyPreferences, usePreferences } from "@/lib/preferences";
import { useSession } from "@/components/AppShell";
import { useQueryParam } from "@/lib/hooks";
import { useToast } from "@/components/toast";
import { Button, ConsequenceDialog, CopyLine, ErrorBox, Input, ListSkeleton, Panel, Tag } from "@/components/ui";
import { MeNotifications } from "./MeNotifications";
import { PreferenceForm } from "./PreferenceForm";

/*
 * 组织设置 → 个人设置（DESIGN.md §20）：每个成员都能进；第一块「我的偏好」，后面是通知、外部日历、代码平台身份。改一项立即 PUT /me/preferences（只发这一项），
 * 返回的整份解析结果直接作用到页面（列、卡片、紧凑、侧栏）；「恢复默认」DELETE 后回到角色 / 系统默认。
 * 下面接着两块也是"只影响我自己"的设置：通知（ADR 0019 的第三层）与我在代码平台上的登录名（ADR 0020）。
 */
export function MeTab() {
  const { session } = useSession();
  const toast = useToast();
  const { prefs } = usePreferences();
  const catalog = useLoad(() => api.preferences.catalog(), []);
  const [busy, setBusy] = useState(false);
  const [resetting, setResetting] = useState(false);
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const roleTitle = prefs.roles_used.map((r) => session?.roles.find((x) => x.name === r)?.title ?? r).join(" · ");

  const change = async (patch: PreferencesPatch) => {
    setBusy(true);
    try {
      applyPreferences(await api.preferences.update(patch));
      setSavedAt(Date.now());
    } catch (e) {
      toast.fail(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const reset = async () => {
    setBusy(true);
    try {
      applyPreferences(await api.preferences.reset());
      toast.ok(t("pref.resetDone"));
      setResetting(false);
    } catch (e) {
      toast.fail(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-4">
      <Panel
        index={1}
        title={t("pref.title")}
        telemetry={busy ? t("common.saving") : savedAt ? t("pref.saved") : prefs.overrides.length ? t("pref.overrideCount", { n: prefs.overrides.length }) : undefined}
        actions={<Button size="sm" variant="ghost" disabled={busy || prefs.overrides.length === 0} onClick={() => setResetting(true)}>{t("pref.reset")}</Button>}
      >
        <p className="mb-3 text-caption text-ink-muted">{t("pref.description")}</p>
        {catalog.loading && !catalog.data ? <ListSkeleton rows={5} /> : catalog.error ? <ErrorBox message={catalog.error} onRetry={catalog.reload} /> : catalog.data ? (
          <PreferenceForm value={prefs} catalog={catalog.data} sources={prefs.sources} roleTitle={roleTitle} busy={busy} onChange={(patch) => void change(patch)} />
        ) : null}
      </Panel>
      <MeNotifications index={2} />
      <CalendarPanel index={3} />
      <CodeIdentityPanel index={4} />
      <ConsequenceDialog
        open={resetting}
        title={t("pref.resetTitle")}
        effects={[t("pref.resetEffect.personal"), t("pref.resetEffect.role")]}
        danger={false}
        confirmLabel={t("pref.reset")}
        busy={busy}
        onConfirm={() => void reset()}
        onClose={() => setResetting(false)}
      />
    </div>
  );
}

/** 我的代码平台身份（ADR 0020）：一行输入 + 绑定 / 解绑。绑上之后，那边的 PR 动态记在我名下。 */
function CodeIdentityPanel({ index }: { index: number }) {
  const toast = useToast();
  const id = useLoad(() => api.me.codeIdentity.get(), []);
  const [login, setLogin] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const data = id.data;
  const value = login ?? data?.login ?? "";
  const connected = !!data?.provider;

  const put = async (next: string) => {
    setBusy(true); setError(null);
    try {
      const r = await api.me.codeIdentity.save(next);
      setLogin(r.login);
      toast.ok(next ? t("me.codeId.saved", { login: r.login }) : t("me.codeId.unbound"));
    } catch (e) { const m = errorMessage(e); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!value.trim()) { setError(t("me.codeId.needLogin")); return; }
    void put(value.trim());
  };

  return (
    <Panel index={index} title={t("me.codeId.title")}>
      {id.loading && !data ? <ListSkeleton rows={1} /> : id.error && !data ? <ErrorBox message={id.error} onRetry={id.reload} /> : (
        <>
          <p className="mb-3 max-w-[640px] text-body text-ink-muted">{connected ? t("me.codeId.hint", { name: data!.provider_title }) : t("me.codeId.hintNone")}</p>
          {connected && (
            <form onSubmit={submit} className="flex max-w-[640px] flex-wrap items-center gap-2" data-code-identity>
              <Input value={value} onChange={(e) => { setLogin(e.target.value); setError(null); }} placeholder="octocat" spellCheck={false} autoComplete="off" aria-label={t("me.codeId.login")} className="w-[220px]" />
              <Button type="submit" variant="primary" size="sm" disabled={busy}>{t("me.codeId.bind")}</Button>
              {data!.bound && <Button size="sm" variant="ghost" disabled={busy} onClick={() => { setLogin(""); void put(""); }}>{t("me.codeId.unbind")}</Button>}
              {data!.bound && !login && <Tag tone="success">{t("me.codeId.bound")}</Tag>}
              {error && <p className="w-full text-caption text-danger" role="alert">{error}</p>}
            </form>
          )}
        </>
      )}
    </Panel>
  );
}

/**
 * 我的外部日历（ADR 0032）：飞书 / 企业微信沿用外部目录的身份，不用连；Google 自己授权一次；
 * 日历订阅链接自己贴一条就连上（不用管理员）。最后一段是反方向：把 AxiomOS 发布成一条订阅链接给日历软件。
 */
function CalendarPanel({ index }: { index: number }) {
  const toast = useToast();
  const list = useLoad(() => api.me.calendars.list(), []);
  const flag = useQueryParam("calendar");
  const reason = useQueryParam("reason");
  const [busy, setBusy] = useState<string | null>(null);
  const disconnect = async (c: MyCalendarView) => {
    setBusy(c.provider);
    try { await api.me.calendars.disconnect(c.provider); toast.ok(t("me.calendar.disconnected", { name: c.provider_title })); list.reload(); }
    catch (e) { toast.fail(errorMessage(e)); } finally { setBusy(null); }
  };
  const all = list.data ?? [];
  const rows = all.filter((c) => !c.self_service && (c.org_configured || c.connected));
  const link = all.find((c) => c.self_service);
  return (
    <Panel index={index} title={t("me.calendar.title")}>
      {list.loading && !list.data ? <ListSkeleton rows={2} /> : list.error && !list.data ? <ErrorBox message={list.error} onRetry={list.reload} /> : (
        <>
          <p className="mb-3 max-w-[640px] text-body text-ink-muted">{t("me.calendar.hint")}</p>
          {flag === "connected" && <p className="mb-3 text-caption text-success" role="status">{t("me.calendar.justConnected")}</p>}
          {flag === "denied" && <p className="mb-3 text-caption text-warning" role="alert">{t("me.calendar.denied")}</p>}
          {flag === "failed" && <p className="mb-3 text-caption text-danger" role="alert">{t("me.calendar.failed", { reason: reason ?? "" })}</p>}
          {rows.length === 0 && !link ? (
            <p className="text-caption text-ink-subtle">{t("me.calendar.none")}</p>
          ) : rows.length > 0 && (
            <ul className="max-w-[640px] divide-y divide-hairline" data-my-calendars>
              {rows.map((c) => (
                <li key={c.provider} className="flex flex-wrap items-center gap-2 py-2">
                  <span className="font-medium">{c.provider_title}</span>
                  {c.connected ? <Tag tone="success">{t("me.calendar.connected")}</Tag> : <Tag>{t("me.calendar.notConnected")}</Tag>}
                  <span className="text-caption text-ink-subtle">{c.connected ? (c.email || c.external_user_id || "") : c.per_member ? t("me.calendar.connectHint") : t("me.calendar.viaDirectory")}</span>
                  <span className="ml-auto flex items-center gap-2">
                    {!c.connected && c.per_member && c.auth_url && <a className="btn btn-primary btn-sm" href={c.auth_url}>{t("me.calendar.connect", { name: c.provider_title })}</a>}
                    {c.connected && c.per_member && <Button size="sm" variant="ghost" disabled={busy === c.provider} onClick={() => void disconnect(c)}>{t("me.calendar.disconnect")}</Button>}
                  </span>
                </li>
              ))}
            </ul>
          )}
          {link && <SubscriptionLink cal={link} busy={busy === link.provider} onChanged={list.reload} onDisconnect={() => void disconnect(link)} />}
          <FeedSection />
        </>
      )}
    </Panel>
  );
}

/** 日历订阅链接：一行输入 + 连接；连上后显示日历名、最近同步、失败原因，可换链接或断开。链接本身永远不回显。 */
function SubscriptionLink({ cal, busy, onChanged, onDisconnect }: { cal: MyCalendarView; busy: boolean; onChanged: () => void; onDisconnect: () => void }) {
  const toast = useToast();
  const [url, setUrl] = useState("");
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const showForm = !cal.connected || editing;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!url.trim()) { setError(t("me.calendar.badLinkEmpty")); return; }
    setSaving(true); setError(null);
    try {
      const r = await api.me.calendars.connect(cal.provider, { url: url.trim() });
      toast.ok(t("me.calendar.linkConnected", { label: r.label || cal.provider_title }));
      setUrl(""); setEditing(false); onChanged();
    } catch (err) { setError(errorMessage(err)); } finally { setSaving(false); }
  };
  return (
    <section className="mt-4 max-w-[640px] border-t border-hairline pt-4" data-calendar-link>
      <div className="flex flex-wrap items-center gap-2">
        <h4 className="text-body font-medium text-ink">{t("me.calendar.linkTitle")}</h4>
        {cal.connected ? <Tag tone="success">{t("me.calendar.connected")}</Tag> : <Tag>{t("me.calendar.notConnected")}</Tag>}
        {cal.connected && cal.label && <span className="text-caption text-ink-subtle">{cal.label}</span>}
        {cal.connected && (
          <span className="ml-auto flex items-center gap-2">
            <Button size="sm" variant="ghost" disabled={busy || saving} onClick={() => { setEditing((v) => !v); setError(null); }}>{t("me.calendar.linkReplace")}</Button>
            <Button size="sm" variant="ghost" disabled={busy || saving} onClick={onDisconnect}>{t("me.calendar.disconnect")}</Button>
          </span>
        )}
      </div>
      <p className="mt-1 text-caption text-ink-muted">{t("me.calendar.linkHint")}</p>
      {cal.connected && (
        <p className={"mt-1 text-caption " + (cal.last_status === "failed" ? "text-danger" : "text-ink-subtle")} role={cal.last_status === "failed" ? "alert" : undefined}>
          {cal.last_sync_at ? t("me.calendar.lastSync", { when: fmtRelative(cal.last_sync_at), status: cal.last_status_title ?? "" }) : t("me.calendar.neverSynced")}
          {cal.last_status === "failed" && cal.last_error ? ` · ${cal.last_error}` : ""}
        </p>
      )}
      {showForm && (
        <form onSubmit={submit} className="mt-2 flex flex-wrap items-center gap-2">
          <Input value={url} onChange={(e) => { setUrl(e.target.value); setError(null); }} placeholder={t("me.calendar.linkPlaceholder")} spellCheck={false} autoComplete="off" aria-label={t("me.calendar.linkTitle")} className="min-w-0 flex-1" />
          <Button type="submit" variant="primary" size="sm" disabled={saving || busy}>{t("me.calendar.linkConnect")}</Button>
          {editing && <Button size="sm" variant="ghost" disabled={saving} onClick={() => { setEditing(false); setUrl(""); setError(null); }}>{t("common.cancel")}</Button>}
          {error && <p className="w-full text-caption text-danger" role="alert">{error}</p>}
        </form>
      )}
      {(cal.prerequisites?.length ?? 0) > 0 && (
        <details className="mt-2 text-caption text-ink-subtle">
          <summary className="cursor-pointer select-none">{t("me.calendar.linkWhere")}</summary>
          <ul className="mt-1 list-disc space-y-0.5 pl-5">
            {cal.prerequisites!.map((x) => <li key={x}>{x}</li>)}
          </ul>
          {cal.tip && <p className="mt-1">{cal.tip.text}</p>}
        </details>
      )}
    </section>
  );
}

/** 反方向：把 AxiomOS 发布成一条订阅链接。生成 / 换链接 / 停用；换与停用都要确认，因为旧链接立刻失效。 */
function FeedSection() {
  const toast = useToast();
  const feed = useLoad(() => api.me.feed.get(), []);
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState<"rotate" | "disable" | null>(null);
  const [fresh, setFresh] = useState<MyFeedView | null>(null);
  const data = fresh ?? feed.data;
  const run = async (fn: () => Promise<MyFeedView | void>, ok: string) => {
    setBusy(true);
    try { const r = await fn(); setFresh(r ?? { enabled: false }); toast.ok(ok); setConfirm(null); }
    catch (e) { toast.fail(errorMessage(e)); } finally { setBusy(false); }
  };
  return (
    <section className="mt-4 max-w-[640px] border-t border-hairline pt-4" data-calendar-feed>
      <h4 className="text-body font-medium text-ink">{t("me.feed.title")}</h4>
      <p className="mt-1 text-caption text-ink-muted">{t("me.feed.hint")}</p>
      {feed.loading && !data ? <ListSkeleton rows={1} /> : feed.error && !data ? <ErrorBox message={feed.error} onRetry={feed.reload} /> : data?.enabled && data.url ? (
        <div className="mt-2 flex flex-col gap-2">
          <CopyLine text={data.url} />
          <div className="flex flex-wrap items-center gap-2 text-caption text-ink-subtle">
            {data.webcal_url && <a className="text-accent hover:underline" href={data.webcal_url}>webcal://</a>}
            <span className="ml-auto flex items-center gap-2">
              <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirm("rotate")}>{t("me.feed.rotate")}</Button>
              <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirm("disable")}>{t("me.feed.disable")}</Button>
            </span>
          </div>
          <p className="text-caption text-ink-subtle">{t("me.feed.secret")}</p>
          <details className="text-caption text-ink-subtle">
            <summary className="cursor-pointer select-none">{t("me.feed.howTo")}</summary>
            <ul className="mt-1 list-disc space-y-0.5 pl-5">
              <li>{t("me.feed.howGoogle")}</li>
              <li>{t("me.feed.howOutlook")}</li>
              <li>{t("me.feed.howApple")}</li>
              <li>{t("me.feed.howFeishu")}</li>
            </ul>
          </details>
        </div>
      ) : (
        <div className="mt-2">
          <Button size="sm" variant="primary" disabled={busy} onClick={() => void run(() => api.me.feed.reset(), t("me.feed.created"))}>{t("me.feed.create")}</Button>
        </div>
      )}
      <ConsequenceDialog
        open={confirm === "rotate"}
        title={t("me.feed.rotateTitle")}
        effects={[t("me.feed.rotateEffect.old"), t("me.feed.rotateEffect.new")]}
        confirmLabel={t("me.feed.rotate")}
        busy={busy}
        onConfirm={() => void run(() => api.me.feed.reset(), t("me.feed.rotated"))}
        onClose={() => setConfirm(null)}
      />
      <ConsequenceDialog
        open={confirm === "disable"}
        title={t("me.feed.disableTitle")}
        effects={[t("me.feed.disableEffect.gone"), t("me.feed.disableEffect.again")]}
        confirmLabel={t("me.feed.disable")}
        busy={busy}
        onConfirm={() => void run(() => api.me.feed.remove(), t("me.feed.disabled"))}
        onClose={() => setConfirm(null)}
      />
    </section>
  );
}
