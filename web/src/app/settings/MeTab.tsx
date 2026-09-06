"use client";
import { useState, type FormEvent } from "react";
import { api, type PreferencesPatch } from "@/lib/api";
import { errorMessage, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { applyPreferences, usePreferences } from "@/lib/preferences";
import { useSession } from "@/components/AppShell";
import { useToast } from "@/components/toast";
import { Button, ConsequenceDialog, ErrorBox, Input, ListSkeleton, Panel, Tag } from "@/components/ui";
import { MeNotifications } from "./MeNotifications";
import { PreferenceForm } from "./PreferenceForm";

/*
 * 组织设置 → 我的偏好（DESIGN.md §20）：每个成员都能进。改一项立即 PUT /me/preferences（只发这一项），
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
        title={t("settings.tab.me")}
        telemetry={busy ? t("common.saving") : savedAt ? t("pref.saved") : prefs.overrides.length ? t("pref.overrideCount", { n: prefs.overrides.length }) : undefined}
        actions={<Button size="sm" variant="ghost" disabled={busy || prefs.overrides.length === 0} onClick={() => setResetting(true)}>{t("pref.reset")}</Button>}
      >
        <p className="mb-3 text-caption text-ink-muted">{t("pref.description")}</p>
        {catalog.loading && !catalog.data ? <ListSkeleton rows={5} /> : catalog.error ? <ErrorBox message={catalog.error} onRetry={catalog.reload} /> : catalog.data ? (
          <PreferenceForm value={prefs} catalog={catalog.data} sources={prefs.sources} roleTitle={roleTitle} busy={busy} onChange={(patch) => void change(patch)} />
        ) : null}
      </Panel>
      <MeNotifications index={2} />
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
