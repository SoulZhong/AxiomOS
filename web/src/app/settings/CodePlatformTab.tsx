"use client";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, type CodeChecklist, type CodePlatform, type CodePlatformInput, type CodeRepo, type DirectoryProviderInfo } from "@/lib/api";
import { errorMessage, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconCheck } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Checkbox, ConsequenceDialog, CopyLine, Empty, ErrorBox, Field, ListSkeleton, Panel, Tag, cx } from "@/components/ui";
import { ChecklistStep, CheckSummary, CredentialField, ProviderCards, StepRow, WizardLink, type ChecklistState, type StepState } from "./wizard";

/*
 * 组织设置 → 代码平台（ADR 0020，DESIGN.md §23）：与「IM 集成」同一条被引导的路，只是能力不同——
 *   ① 选平台 → ② 填凭据（保存即检查）→ ③ 接入检查（逐条修，每条有「打开控制台」与「我已处理，重新检查」）→ ④ 选仓库 → ⑤ 完成。
 * 当前在哪一步是从数据推出来的（见 dataStep），不记在状态里；做完的步骤折成一行摘要可点「修改」。
 * 提供方是数据不是分支：卡片、凭据字段、提示、控制台链接、检查项全部来自接口，这个文件里没有平台名。
 * ⑤ 给出回调地址与回调密钥：密钥平时只显示「已设置」，点「显示密钥」才现场取一次明文（服务端记一条查看记录），
 *   取到的明文只留在这个组件里，不并回配置状态，收起、离开这一步或重新读设置都会消失；「换一把密钥」换完把新的再给一次。
 *   凭据类的保密字段仍然永不回显。
 */
type StepNo = 1 | 2 | 3 | 4 | 5;

export function CodePlatformTab() {
  const toast = useToast();
  const config = useLoad(() => api.org.codePlatform.get(), []);
  const [picked, setPicked] = useState<string | null>(null);
  const [switching, setSwitching] = useState(false);
  const [confirmSwitch, setConfirmSwitch] = useState(false);
  const [confirmDisconnect, setConfirmDisconnect] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);
  const [override, setOverride] = useState<StepNo | null>(null);
  const [checksOpen, setChecksOpen] = useState<boolean | null>(null);
  const [cl, setCl] = useState<ChecklistState<CodeChecklist>>({ data: null, error: null, loading: false });

  const cfg = config.data;
  const configured = !!cfg?.configured;
  const providerKey = cfg?.provider ?? null;
  const runChecklist = useCallback(async (): Promise<CodeChecklist | null> => {
    setCl((s) => ({ ...s, loading: true, error: null }));
    try {
      const d = await api.org.codePlatform.checklist();
      setCl({ data: d, error: null, loading: false });
      return d;
    } catch (e) {
      setCl((s) => ({ data: s.data, error: errorMessage(e), loading: false }));
      return null;
    }
  }, []);
  useEffect(() => {
    if (configured) void runChecklist();
    else setCl({ data: null, error: null, loading: false });
  }, [configured, providerKey, runChecklist]);

  if (config.loading && !config.data) return <Panel index={1} title={t("settings.tab.code")}><ListSkeleton rows={3} /></Panel>;
  if (config.error && !config.data) return <ErrorBox message={config.error} onRetry={config.reload} />;
  if (!cfg) return null;

  const provider = cfg.provider ? cfg.providers.find((p) => p.key === cfg.provider) ?? null : null;
  const formProvider = picked ? cfg.providers.find((p) => p.key === picked) ?? null : switching ? null : provider;
  const choosing = !cfg.provider || switching;
  const checks = cl.data?.checks ?? [];
  const blocked = checks.filter((c) => c.status === "blocked");
  const todos = checks.filter((c) => c.status === "todo");
  const hasBad = blocked.length > 0 || todos.length > 0;
  const chosenRepos = cfg.repos.filter((r) => r.enabled);
  // 数据说我们在哪一步（⑤ 是终点，接好之后一直停在那里）
  const dataStep: StepNo =
    choosing && !picked ? 1
    : !cfg.configured || picked ? 2
    : !cl.data || blocked.length > 0 ? 3
    : chosenRepos.length === 0 ? 4
    : 5;
  const current: StepNo = dataStep <= 2 ? dataStep : override !== null && !(blocked.length > 0 && override > 3) ? override : dataStep;
  const stateOf = (n: StepNo): StepState => (n === current ? "current" : n < dataStep ? "done" : "upcoming");
  const checkSummary = cl.data ? <CheckSummary checks={checks} /> : cl.loading ? <span className="text-ink-subtle">{t("settings.directory.wizard.checking")}</span> : <span className="text-ink-subtle">{t("settings.directory.wizard.notChecked")}</span>;
  // ③ 做完后折成一行；接好之后有坏的自动展开
  const checksShown = stateOf(3) === "current" || (stateOf(3) === "done" && (checksOpen ?? (dataStep === 5 && hasBad)));
  const settled = dataStep === 5 && !choosing && !picked;

  const onCredentialsSaved = (p: DirectoryProviderInfo) => {
    setPicked(null); setSwitching(false); setOverride(null); config.reload();
    if (cfg.configured && p.key === cfg.provider) void runChecklist();
  };
  // 断开：凭据与仓库选择没了，页面回到 ① 选平台（清掉向导里所有临时状态，再重新读一次设置）
  const disconnect = async () => {
    setDisconnecting(true);
    try {
      await api.org.codePlatform.disconnect();
      setConfirmDisconnect(false);
      setPicked(null); setSwitching(false); setOverride(null); setChecksOpen(null);
      setCl({ data: null, error: null, loading: false });
      config.reload();
      toast.ok(t("settings.code.disconnected"));
    } catch (err) { toast.fail(errorMessage(err)); } finally { setDisconnecting(false); }
  };

  return (
    <div className="space-y-4">
      <Panel index={1} title={t("settings.tab.code")}>
        <p className="mb-4 max-w-[640px] text-body text-ink-subtle">{t("settings.code.description")}</p>
        <div className="max-w-[760px]">
          {/* 接好之后：顶部一条状态摘要（平台 · 仓库数 · 检查摘要 · 重新检查） */}
          {settled && (
            <div className="mb-5 flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-md border border-hairline bg-surface-2 px-3 py-2 text-body" data-code-status>
              <span className={cx("h-2 w-2 shrink-0 rounded-full", blocked.length ? "bg-danger" : todos.length ? "bg-warning" : "bg-success")} aria-hidden="true" />
              <span className="font-medium">{cfg.provider_title}</span>
              <Tag tone="success">{t("settings.code.connected")}</Tag>
              <span className="flex flex-wrap items-center gap-x-1.5 text-ink-muted">
                <span>{t("settings.code.repoCount", { n: chosenRepos.length })}</span>
                <span aria-hidden="true">·</span>
                {checkSummary}
              </span>
              <span className="ml-auto flex items-center gap-2">
                <Button size="sm" disabled={cl.loading} onClick={() => void runChecklist()}>{cl.loading ? t("settings.directory.wizard.checking") : t("settings.code.recheckNow")}</Button>
                <Button size="sm" variant="ghost" onClick={() => setConfirmSwitch(true)}>{t("settings.code.switch")}</Button>
                <Button size="sm" variant="ghost" onClick={() => setConfirmDisconnect(true)} data-code-disconnect>{t("settings.code.disconnect")}</Button>
              </span>
            </div>
          )}
          <ol aria-label={t("settings.tab.code")}>
            {/* ① 选平台 */}
            <StepRow
              n={1}
              title={t("settings.code.step1")}
              state={stateOf(1)}
              summary={stateOf(1) !== "current" && (
                <>
                  <span className="text-ink">{formProvider?.title ?? provider?.title ?? t("settings.directory.wizard.notChosen")}</span>
                  {cfg.configured && !picked && !switching && <Tag tone="success">{t("settings.code.connected")}</Tag>}
                </>
              )}
              action={stateOf(1) === "done" && (
                switching
                  ? <Button size="sm" variant="ghost" onClick={() => { setSwitching(false); setPicked(null); }}>{t("settings.code.cancelSwitch")}</Button>
                  : <Button size="sm" variant="ghost" onClick={() => (cfg.configured && !picked ? setConfirmSwitch(true) : setPicked(null))}>{t("settings.directory.wizard.edit")}</Button>
              )}
              open={stateOf(1) === "current"}
            >
              <p className="mb-3 max-w-[640px] text-body text-ink-muted">{switching ? t("settings.code.switching", { name: cfg.provider_title }) : t("settings.code.unconfigured")}</p>
              <ProviderCards providers={cfg.providers} picked={picked} onPick={(k) => { setPicked(k); setOverride(null); }} tip />
              {switching && <div className="mt-3"><Button size="sm" onClick={() => { setSwitching(false); setPicked(null); }}>{t("settings.code.cancelSwitch")}</Button></div>}
            </StepRow>

            {/* ② 填凭据：保存即检查 */}
            <StepRow
              n={2}
              title={t("settings.code.step2")}
              state={stateOf(2)}
              summary={stateOf(2) === "done" && provider && <CredentialsSummary cfg={cfg} provider={provider} />}
              action={stateOf(2) === "done" && <Button size="sm" variant="ghost" onClick={() => setOverride(2)}>{t("settings.directory.wizard.edit")}</Button>}
              open={stateOf(2) === "current" && !!formProvider}
            >
              {formProvider && (
                <CredentialsStep
                  key={formProvider.key}
                  cfg={cfg}
                  provider={formProvider}
                  onSaved={() => onCredentialsSaved(formProvider)}
                  onCancel={cfg.configured && !picked ? () => setOverride(null) : undefined}
                />
              )}
            </StepRow>

            {/* ③ 接入检查 */}
            <StepRow
              n={3}
              title={t("settings.code.step3")}
              state={stateOf(3)}
              warn={stateOf(3) === "done" && hasBad}
              summary={stateOf(3) === "done" && checkSummary}
              action={stateOf(3) === "done" && <Button size="sm" variant="ghost" onClick={() => setChecksOpen(!checksShown)}>{checksShown ? t("settings.directory.wizard.collapse") : t("settings.directory.wizard.view")}</Button>}
              open={checksShown}
            >
              <ChecklistStep
                cl={cl}
                providerTitle={cfg.provider_title}
                skipped={[]}
                canSkip={false}
                lead={(k) => (k === "ok" ? t("settings.code.checkLeadOk") : k === "blocked" ? t("settings.code.checkLead", { name: cfg.provider_title }) : t("settings.code.checkLeadTodo"))}
                onRecheck={runChecklist}
              />
            </StepRow>

            {/* ④ 选仓库 */}
            <StepRow
              n={4}
              title={t("settings.code.step4")}
              state={stateOf(4)}
              summary={stateOf(4) === "done" && <span className="text-ink">{chosenRepos.length ? t("settings.code.repoCount", { n: chosenRepos.length }) : t("settings.code.noRepoPicked")}</span>}
              action={stateOf(4) === "done" && <Button size="sm" variant="ghost" onClick={() => setOverride(4)}>{t("settings.directory.wizard.edit")}</Button>}
              open={stateOf(4) === "current"}
            >
              {provider && <ReposStep providerTitle={cfg.provider_title} chosen={cfg.repos} onSaved={() => { setOverride(null); config.reload(); void runChecklist(); }} onCancel={dataStep === 5 ? () => setOverride(null) : undefined} />}
            </StepRow>

            {/* ⑤ 完成：回调地址与回调密钥 */}
            <StepRow
              n={5}
              title={t("settings.code.step5")}
              state={stateOf(5)}
              open={stateOf(5) === "current"}
              last
            >
              <DoneStep cfg={cfg} />
            </StepRow>
          </ol>
        </div>
        <ConsequenceDialog
          open={confirmSwitch}
          title={t("settings.code.switch")}
          subject={t("settings.code.switching", { name: cfg.provider_title })}
          effects={[t("settings.code.switchEffect.links"), t("settings.code.switchEffect.hooks"), t("settings.code.switchEffect.secret")]}
          confirmLabel={t("settings.code.switchGo")}
          danger
          onConfirm={() => { setConfirmSwitch(false); setSwitching(true); setPicked(null); setOverride(null); }}
          onClose={() => setConfirmSwitch(false)}
        />
        <ConsequenceDialog
          open={confirmDisconnect}
          title={t("settings.code.disconnectTitle")}
          subject={t("settings.code.disconnectSubject", { name: cfg.provider_title })}
          effects={[t("settings.code.disconnectEffect.creds"), t("settings.code.disconnectEffect.links"), t("settings.code.disconnectEffect.hooks")]}
          note={t("settings.code.disconnectNote")}
          confirmLabel={t("settings.code.disconnectGo")}
          danger
          busy={disconnecting}
          onConfirm={() => void disconnect()}
          onClose={() => setConfirmDisconnect(false)}
        />
      </Panel>
      {cfg.configured && <EventsPanel events={cfg.events} />}
    </div>
  );
}

/** ② 的一行摘要：非保密字段「标题 值」，保密字段「标题 已设置」——字段按提供方声明来 */
function CredentialsSummary({ cfg, provider }: { cfg: CodePlatform; provider: DirectoryProviderInfo }) {
  return (
    <>
      {provider.fields.map((f, i) => (
        <span key={f.key} className="inline-flex items-center gap-1.5">
          {i > 0 && <span aria-hidden="true">·</span>}
          {f.secret
            ? <span>{t("settings.directory.wizard.credSet", { title: f.title })}</span>
            : <><span>{f.title}</span><span className="telemetry text-ink">{cfg.credentials[f.key] || "—"}</span></>}
        </span>
      ))}
    </>
  );
}

/** ② 凭据表单：字段按提供方声明渲染，选填的可以留空。保存成功后由父组件自动跑检查清单。 */
function CredentialsStep({ cfg, provider, onSaved, onCancel }: { cfg: CodePlatform; provider: DirectoryProviderInfo; onSaved: () => void; onCancel?: () => void }) {
  const toast = useToast();
  const active = cfg.provider === provider.key;
  const secretsSet: Record<string, boolean> = Object.fromEntries(provider.fields.filter((f) => f.secret).map((f) => [f.key, active && !!cfg.secrets_set[f.key]]));
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(provider.fields.map((f) => [f.key, active && !f.secret ? cfg.credentials[f.key] ?? "" : ""])));
  const [resetting, setResetting] = useState<Record<string, boolean>>(() => Object.fromEntries(Object.entries(secretsSet).map(([k, v]) => [k, !v])));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    for (const f of provider.fields) {
      if (f.optional) continue;
      const v = (values[f.key] ?? "").trim();
      if (!f.secret && !v) { setError(t("settings.directory.needField", { title: f.title })); return; }
      if (f.secret && !secretsSet[f.key] && !v) { setError(t("settings.directory.needSecretField", { title: f.title })); return; }
    }
    setBusy(true);
    try {
      const credentials: Record<string, string> = {};
      for (const f of provider.fields) {
        const v = (values[f.key] ?? "").trim();
        if (!f.secret) credentials[f.key] = v;
        else if (resetting[f.key] && v) credentials[f.key] = v; // 省略保密字段 = 沿用已保存的
      }
      const input: CodePlatformInput = active ? { provider: provider.key, credentials, proxy_url: cfg.proxy_url } : { provider: provider.key, credentials, repos: [], proxy_url: "" };
      await api.org.codePlatform.save(input);
      toast.ok(t("settings.code.saved"));
      onSaved();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  const tip = provider.tip ?? null;

  return (
    <form onSubmit={submit} className="max-w-[640px] space-y-4">
      <p className="text-body text-ink-muted">{t("settings.code.credLead", { name: provider.title })}</p>
      {tip && (
        <p className="text-caption text-ink-subtle" data-provider-tip>
          {tip.text}
          {tip.url && <> <WizardLink href={tip.url}>{t("settings.directory.openConsole", { name: provider.title })}</WizardLink></>}
        </p>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        {provider.fields.map((f) => (
          <CredentialField
            key={f.key}
            field={f}
            value={values[f.key] ?? ""}
            onChange={(v) => setValues((d) => ({ ...d, [f.key]: v }))}
            isSet={!!secretsSet[f.key]}
            resetting={!!resetting[f.key]}
            onReset={(on) => { setResetting((r) => ({ ...r, [f.key]: on })); if (!on) setValues((d) => ({ ...d, [f.key]: "" })); }}
          />
        ))}
      </div>
      {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" variant="primary" disabled={busy}>{busy ? t("settings.directory.wizard.checking") : t("settings.directory.wizard.saveAndCheck")}</Button>
        {onCancel && <Button variant="ghost" onClick={onCancel}>{t("settings.directory.wizard.keep")}</Button>}
      </div>
    </form>
  );
}

/** ④ 选仓库：现场读平台上的仓库，勾上要接的；保存时系统去建回调，建不上的那一行写清原因。 */
function ReposStep({ providerTitle, chosen, onSaved, onCancel }: { providerTitle: string; chosen: CodeRepo[]; onSaved: () => void; onCancel?: () => void }) {
  const toast = useToast();
  const [reload, setReload] = useState(0);
  const repos = useLoad(() => api.org.codePlatform.repos(), [reload]);
  const [picked, setPicked] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const list = repos.data ?? [];
  const ids = picked ?? list.filter((r) => r.enabled).map((r) => r.full_name);
  const hookOf = (name: string) => chosen.find((r) => r.full_name === name);
  const toggle = (name: string, on: boolean) => setPicked(on ? (ids.includes(name) ? ids : [...ids, name]) : ids.filter((x) => x !== name));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!ids.length) { setError(t("settings.code.needRepos")); return; }
    setBusy(true);
    try { await api.org.codePlatform.save({ repos: ids }); toast.ok(t("settings.code.reposSaved")); setPicked(null); onSaved(); }
    catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };

  return (
    <form onSubmit={submit} className="max-w-[640px] space-y-3">
      <p className="text-body text-ink-muted">{t("settings.code.reposLead", { name: providerTitle })}</p>
      {repos.loading && !repos.data ? <ListSkeleton rows={3} />
        : repos.error ? <ErrorBox message={repos.error} onRetry={() => setReload((v) => v + 1)} />
        : !list.length ? <Empty text={t("settings.code.noRepos", { name: providerTitle })} illustration={false} className="py-6" />
        : (
          <ul className="divide-y divide-hairline rounded-md border border-hairline" data-repos>
            {list.map((r) => {
              const saved = hookOf(r.full_name);
              return (
                <li key={r.full_name} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2" data-repo={r.full_name}>
                  <Checkbox label={<span className="font-medium text-ink">{r.full_name}</span>} checked={ids.includes(r.full_name)} onChange={(e) => toggle(r.full_name, e.target.checked)} />
                  {saved?.enabled && (
                    saved.hook_ok
                      ? <Tag tone="success" className="ml-auto"><IconCheck size={12} className="tag-icon" />{t("settings.code.hookOk")}</Tag>
                      : <Tag tone="warning" className="ml-auto">{t("settings.code.hookBad")}</Tag>
                  )}
                  {saved && !saved.hook_ok && saved.hook_error && <p className="w-full text-caption text-ink-muted">{saved.hook_error}</p>}
                </li>
              );
            })}
          </ul>
        )}
      {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" variant="primary" disabled={busy || !list.length}>{busy ? t("common.saving") : t("settings.code.saveRepos")}</Button>
        <Button variant="ghost" disabled={repos.loading} onClick={() => setReload((v) => v + 1)}>{t("settings.code.reposReload")}</Button>
        {onCancel && <Button variant="ghost" onClick={onCancel}>{t("settings.directory.wizard.keep")}</Button>}
      </div>
    </form>
  );
}

/**
 * ⑤ 完成：回调地址一直摆着能复制；回调密钥平时只说「已设置」，要看得自己点一下。
 * 明文只存在这个组件的 state 里（reveal 或换密钥那一次返回的），不写回 cfg：收起、离开这一步、重新读设置都会消失。
 */
function DoneStep({ cfg }: { cfg: CodePlatform }) {
  const toast = useToast();
  const [secret, setSecret] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmRotate, setConfirmRotate] = useState(false);
  // 重新读了一次设置就把明文收回去（cfg 是新对象），渲染期直接改，不留一帧旧密钥
  const [seen, setSeen] = useState(cfg);
  if (seen !== cfg) { setSeen(cfg); setSecret(null); }

  const reveal = async () => {
    setBusy(true);
    try { const d = await api.org.codePlatform.revealSecret(); setSecret(d.webhook_secret ?? ""); }
    catch (err) { toast.fail(errorMessage(err)); }
    finally { setBusy(false); }
  };
  const rotate = async () => {
    setBusy(true);
    try { const d = await api.org.codePlatform.save({ rotate_webhook_secret: true }); setConfirmRotate(false); setSecret(d.webhook_secret ?? ""); }
    catch (err) { toast.fail(errorMessage(err)); }
    finally { setBusy(false); }
  };

  if (!cfg.webhook_url) return <p className="max-w-[640px] text-body text-ink-muted">{t("settings.code.unconfigured")}</p>;
  return (
    <div className="max-w-[640px] space-y-3" data-code-done>
      <p className="text-body text-ink-muted">{t("settings.code.doneLead", { name: cfg.provider_title })}</p>
      <Field as="div" label={t("settings.code.webhookUrl")}><CopyLine text={cfg.webhook_url} /></Field>
      {cfg.webhook_secret_set && (
        <div className="space-y-1.5" data-code-secret={secret === null ? "hidden" : "shown"}>
          <Field as="div" label={t("settings.code.webhookSecret")}>
            {secret === null ? (
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-body text-ink">{t("settings.code.secretSet")}</span>
                <Button size="sm" disabled={busy} onClick={() => void reveal()}>{t("settings.code.showSecret")}</Button>
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmRotate(true)}>{t("settings.code.rotateSecret")}</Button>
              </div>
            ) : (
              <div className="space-y-2">
                <CopyLine text={secret} />
                <div className="flex flex-wrap items-center gap-2">
                  <Button size="sm" variant="ghost" onClick={() => setSecret(null)}>{t("settings.code.hideSecret")}</Button>
                  <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmRotate(true)}>{t("settings.code.rotateSecret")}</Button>
                </div>
              </div>
            )}
          </Field>
          <p className="text-caption text-ink-subtle">{t("settings.code.secretAudited")}</p>
        </div>
      )}
      <p className="text-caption text-ink-subtle">{t("settings.code.pasteHint", { name: cfg.provider_title })}</p>
      {cfg.console_url && <p className="text-caption"><WizardLink href={cfg.console_url}>{t("settings.directory.openConsole", { name: cfg.provider_title })}</WizardLink></p>}
      <ConsequenceDialog
        open={confirmRotate}
        title={t("settings.code.rotateSecret")}
        effects={[t("settings.code.rotateEffect", { name: cfg.provider_title })]}
        confirmLabel={t("settings.code.rotateSecret")}
        busy={busy}
        onConfirm={() => void rotate()}
        onClose={() => setConfirmRotate(false)}
      />
    </div>
  );
}

/** 能触发流程的事件：流程页签里那些「PR 合并时自动走这一步」的标签就出自这六个。 */
function EventsPanel({ events }: { events: CodePlatform["events"] }) {
  if (!events.length) return null;
  return (
    <Panel index={2} title={t("settings.code.events")}>
      <ul className="flex flex-wrap gap-2" data-code-events>
        {events.map((e) => <li key={e.key}><Tag>{e.title}</Tag></li>)}
      </ul>
    </Panel>
  );
}
