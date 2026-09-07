"use client";
import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { api, isSynced, type DirectoryBinding, type DirectoryChecklist, type DirectoryConfig, type DirectoryConfirmation, type DirectoryDecided, type DirectoryDuplicate, type DirectoryInput, type DirectoryKind, type DirectoryPreview, type DirectoryProviderInfo, type DirectoryRun, type DirectorySchedule, type DirectorySkipped } from "@/lib/api";
import { fmtDate, fmtDateTime, parseDate } from "@/lib/format";
import { errorMessage, setQueryParams, useLoad, useQueryParam } from "@/lib/hooks";
import { t, type Key } from "@/lib/i18n";
import { IconCopy, IconMember, IconPlay, IconSearch, IconTeam } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Checkbox, ConfirmDialog, ConsequenceDialog, DescList, Empty, ErrorBox, Field, Input, ListSkeleton, Panel, Select, Table, Tabs, Tag, Tip, cx, type Tone } from "@/components/ui";
import { MergeDialog } from "./MergeDialog";
import { ChecklistStep, CheckSummary, CredentialField, ProviderCards, StepRow, WizardLink, type ChecklistState, type StepState } from "./wizard";

/*
 * IM 集成 = 一条被引导的路（ADR 0017 补记二，DESIGN.md §14「接入向导」）：
 *   ① 选平台 → ② 填凭据（保存即检查）→ ③ 权限检查（逐条修，每条有「打开控制台」与「我已处理，重新检查」）→ ④ 同步范围 → ⑤ 预览并同步 → ⑥ 定时。
 * 当前在哪一步不是记在状态里，而是从数据推出来的（见 stepOf）：没选提供方 → ①；没配好 → ②；检查清单有阻塞 / 待处理 → ③；
 * 应用只被授权了部分部门且还没选根 → ④；还没同步过 → ⑤；否则接入完成，页面顶部是一条状态摘要（「N 项检查全部通过 · 上次同步 …」），
 * 每一步折成一行摘要可点「修改」。做完的步骤折叠，只有当前步展开。
 * 提供方是数据，不是分支（ADR 0017 补记）：卡片、凭据字段、提示、控制台链接、检查项全部来自接口；这个文件里没有任何平台名。
 * 文案只说"去哪儿点什么"；权限的英文代号只出现在接口给的 detail 里，界面把它放在「详情」后面。
 * 冲突与对应关系（ADR 0017 补记四，DESIGN.md §14「冲突与对应关系」）：预览表格上方是「需要你确认」——每条一行，左边 IM 里的对象、中间"可能是"、
 * 右边系统里的疑似对象，三个动作；决定立刻写到后端，行移到「已决定」；全部处理完之前「立即同步」显示为「还有 N 条要先确认」并禁用。
 * 接入完成的状态视图里常驻「预览同步」按钮与「对应关系」面板（已绑定 / 可能重复 / 已跳过），同步完之后仍能回来处理。
 */
type StepNo = 1 | 2 | 3 | 4 | 5 | 6;
type DirChecklistState = ChecklistState<DirectoryChecklist>;

export function DirectoryTab() {
  const toast = useToast();
  const config = useLoad(() => api.org.directory.get(), []);
  const roles = useLoad(() => api.org.roles(), []);
  const runs = useLoad(() => api.org.directory.runs(20), []);
  // picked：正在配置（还没保存）的提供方；switching：已接入后点了「更换提供方」并确认，正在重新挑
  const [picked, setPicked] = useState<string | null>(null);
  const [switching, setSwitching] = useState(false);
  const [confirmSwitch, setConfirmSwitch] = useState(false);
  const [confirmDisconnect, setConfirmDisconnect] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);
  // override：用户点了某一步的「修改」，临时把那一步展开；保存 / 完成 / 不改了之后清掉，回到数据推出的那一步
  const [override, setOverride] = useState<StepNo | null>(null);
  // 本次会话里点了「先跳过」的待处理项（不拦同步的黄色项）；只在还没同步过时挡在 ③
  const [skipped, setSkipped] = useState<string[]>([]);
  // 接入完成后 ③ 折成一行，有坏的默认展开；用户可以手动收起 / 展开
  const [checksOpen, setChecksOpen] = useState<boolean | null>(null);
  const [lastPreview, setLastPreview] = useState<DirectoryPreview | null>(null);
  const [lastRun, setLastRun] = useState<DirectoryRun | null>(null);
  // 状态视图里点了「预览同步」：展开 ⑤ 并立刻跑一次预览（这里发请求，⑤ 只显示忙碌）
  const [autoPreviewing, setAutoPreviewing] = useState(false);
  // 对应关系面板要在决定 / 合并 / 解绑之后刷新
  const [mappingsVersion, setMappingsVersion] = useState(0);
  const bumpMappings = useCallback(() => setMappingsVersion((v) => v + 1), []);
  const [cl, setCl] = useState<DirChecklistState>({ data: null, error: null, loading: false });

  const cfg = config.data;
  const configured = !!cfg?.configured;
  const providerKey = cfg?.provider ?? null;
  const runChecklist = useCallback(async (): Promise<DirectoryChecklist | null> => {
    setCl((s) => ({ ...s, loading: true, error: null }));
    try {
      const d = await api.org.directory.checklist();
      setCl({ data: d, error: null, loading: false });
      return d;
    } catch (e) {
      setCl((s) => ({ data: s.data, error: errorMessage(e), loading: false }));
      return null;
    }
  }, []);
  // 配好了就跑检查清单；换了提供方重跑；没配好就清空
  useEffect(() => {
    if (configured) void runChecklist();
    else setCl({ data: null, error: null, loading: false });
  }, [configured, providerKey, runChecklist]);

  if (config.loading && !config.data) return <Panel index={1} title={t("settings.tab.directory")}><ListSkeleton rows={3} /></Panel>;
  if (config.error && !config.data) return <ErrorBox message={config.error} onRetry={config.reload} />;
  if (!cfg) return null;

  const provider = cfg.provider ? cfg.providers.find((p) => p.key === cfg.provider) ?? null : null;
  const formProvider = picked ? cfg.providers.find((p) => p.key === picked) ?? null : switching ? null : provider;
  const choosing = !cfg.provider || switching;
  const checks = cl.data?.checks ?? [];
  const blocked = checks.filter((c) => c.status === "blocked");
  const todos = checks.filter((c) => c.status === "todo");
  const pending = todos.filter((c) => !skipped.includes(c.key));
  // 同步过 = 当前提供方有一次不是失败的运行
  const synced = !!cfg.last_run && cfg.last_run.status !== "failed" && (!cfg.last_run.provider || cfg.last_run.provider === cfg.provider);
  const rootIds = cfg.root_department_ids ?? [];
  const rootsChosen = rootIds.length > 0 || (!!cfg.root_department_id && cfg.root_department_id !== (provider?.root_department_id ?? ""));
  // 可选的同步根：清单顶层的 suggested_roots，没有就取带 roots 的那一项检查（后端把它挂在"权限范围"上）
  const suggested = cl.data?.suggested_roots?.length ? cl.data.suggested_roots : checks.find((c) => c.roots?.length)?.roots ?? [];
  const scopeOpen = suggested.length > 0 && !rootsChosen;
  // 数据说我们在哪一步（null = 接入完成）
  const dataStep: StepNo | null =
    choosing && !picked ? 1
    : !cfg.configured || picked ? 2
    : !cl.data ? 3
    : blocked.length > 0 || (!synced && pending.length > 0) ? 3
    : scopeOpen ? 4
    : !synced ? 5
    : null;
  // 展开哪一步：① ② 由数据说了算；之后可以被「修改」临时覆盖（但有阻塞项时不能跳过 ③）
  const current: StepNo | null = dataStep !== null && dataStep <= 2 ? dataStep : override !== null && !(blocked.length > 0 && override > 3) ? override : dataStep;
  const stateOf = (n: StepNo): StepState => (n === current ? "current" : dataStep === null || n < dataStep ? "done" : "upcoming");
  const hasBad = blocked.length > 0 || todos.length > 0;
  const roleOptions: Array<[string, string]> = (roles.data ?? []).map((r) => [r.name, r.title]);

  const afterSave = () => { setOverride(null); config.reload(); };
  const onCredentialsSaved = (p: DirectoryProviderInfo) => {
    setPicked(null); setSwitching(false); setSkipped([]); setLastPreview(null); setLastRun(null);
    afterSave();
    // 同一个提供方重存：deps 不变，得手动重跑；换了提供方 / 第一次配好由 effect 触发
    if (cfg.configured && p.key === cfg.provider) void runChecklist();
  };
  const onScopeSaved = () => { setLastPreview(null); afterSave(); void runChecklist(); };
  const onSynced = (r: DirectoryRun) => { setLastRun(r); setLastPreview(null); setOverride(5); config.reload(); runs.reload(); void runChecklist(); bumpMappings(); };
  const openPreview = async () => {
    setLastRun(null); setOverride(5); setAutoPreviewing(true);
    try { setLastPreview(await api.org.directory.preview()); } catch (err) { toast.fail(errorMessage(err)); } finally { setAutoPreviewing(false); }
  };
  // 向导回到起点：清掉所有临时状态（挑到一半的提供方、跳过的检查项、上一次预览 / 同步结果）
  const resetWizard = () => {
    setPicked(null); setOverride(null); setSkipped([]); setChecksOpen(null);
    setLastPreview(null); setLastRun(null); setAutoPreviewing(false);
  };
  const startSwitch = () => { setConfirmSwitch(false); setSwitching(true); resetWizard(); };
  // 断开：凭据与同步设置没了，页面回到 ① 选平台（走向导自己的复位路径，再重新读一次设置）
  const disconnect = async () => {
    setDisconnecting(true);
    try {
      await api.org.directory.disconnect();
      setConfirmDisconnect(false);
      setSwitching(false); resetWizard();
      setCl({ data: null, error: null, loading: false });
      config.reload(); runs.reload(); bumpMappings();
      toast.ok(t("settings.directory.disconnected"));
    } catch (err) { toast.fail(errorMessage(err)); } finally { setDisconnecting(false); }
  };
  const checkSummary = cl.data ? <CheckSummary checks={checks} /> : cl.loading ? <span className="text-ink-subtle">{t("settings.directory.wizard.checking")}</span> : <span className="text-ink-subtle">{t("settings.directory.wizard.notChecked")}</span>;
  const scopeSummary = rootsChosen
    ? rootIds.length > 0
      ? t("settings.directory.wizard.scopeSummarySome", { names: rootIds.map((id) => suggested.find((r) => r.id === id)?.name ?? id).join("、") })
      : t("settings.directory.wizard.scopeSummaryRoot", { id: cfg.root_department_id })
    : t("settings.directory.wizard.scopeAll");
  // ③ 做完后折成一行；接入完成的状态视图里有坏的自动展开，向导进行中不抢 ④⑤⑥ 的注意力
  const checksShown = stateOf(3) === "current" || (stateOf(3) === "done" && (checksOpen ?? (dataStep === null && hasBad)));

  return (
    <div className="space-y-4">
      <Panel index={1} title={t("settings.tab.directory")}>
        <p className="mb-4 max-w-[640px] text-body text-ink-subtle">{t("settings.directory.description")}</p>
        {/* 已接入：顶部一条即时状态（③ 的摘要 + 上次同步），有坏的下面 ③ 会自动展开 */}
        <div className="max-w-[760px]">
        {synced && !choosing && !picked && (
          <div className="mb-5 flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-md border border-hairline bg-surface-2 px-3 py-2 text-body" data-directory-status>
            <span className={cx("h-2 w-2 shrink-0 rounded-full", blocked.length ? "bg-danger" : todos.length ? "bg-warning" : "bg-success")} aria-hidden="true" />
            <span className="font-medium">{cfg.provider_title}</span>
            <Tag tone="success">{t("settings.directory.connected")}</Tag>
            <span className="flex flex-wrap items-center gap-x-1.5 text-ink-muted">
              {checkSummary}
              <span aria-hidden="true">·</span>
              <span>{t("settings.directory.lastRun")}</span>
              <span className="telemetry">{fmtDateTime(cfg.last_run!.started_at)}</span>
              <RunStatus run={cfg.last_run!} />
            </span>
            <span className="ml-auto flex items-center gap-2">
              <Button size="sm" icon={<IconSearch />} disabled={autoPreviewing} onClick={() => void openPreview()} data-preview-now>{t("settings.directory.preview")}</Button>
              <Button size="sm" variant="ghost" onClick={() => setConfirmSwitch(true)}>{t("settings.directory.switch")}</Button>
              <Button size="sm" variant="ghost" onClick={() => setConfirmDisconnect(true)} data-directory-disconnect>{t("settings.directory.disconnect")}</Button>
            </span>
          </div>
        )}
        {/* 接入完成：对应关系面板常驻在状态摘要下面（ADR 0017 补记四） */}
        {synced && !choosing && !picked && <MappingsPanel key={mappingsVersion} providerTitle={cfg.provider_title} onChanged={bumpMappings} className="mb-5" />}
        <ol aria-label={t("settings.tab.directory")}>
          {/* ① 选平台 */}
          <StepRow
            n={1}
            title={t("settings.directory.wizard.step1")}
            state={stateOf(1)}
            summary={stateOf(1) !== "current" && (
              <>
                <span className="text-ink">{formProvider?.title ?? provider?.title ?? t("settings.directory.wizard.notChosen")}</span>
                {cfg.configured && !picked && !switching && <Tag tone="success">{t("settings.directory.connected")}</Tag>}
              </>
            )}
            action={stateOf(1) === "done" && (
              switching
                ? <Button size="sm" variant="ghost" onClick={() => { setSwitching(false); setPicked(null); }}>{t("settings.directory.cancelSwitch")}</Button>
                : <Button size="sm" variant="ghost" onClick={() => (cfg.configured && !picked ? setConfirmSwitch(true) : setPicked(null))}>{t("settings.directory.wizard.edit")}</Button>
            )}
            open={stateOf(1) === "current"}
          >
            <p className="mb-3 max-w-[640px] text-body text-ink-muted">{switching ? t("settings.directory.switching", { name: cfg.provider_title }) : t("settings.directory.unconfigured")}</p>
            <ProviderCards providers={cfg.providers} picked={picked} onPick={(k) => { setPicked(k); setOverride(null); }} />
            {switching && <div className="mt-3"><Button size="sm" onClick={() => { setSwitching(false); setPicked(null); }}>{t("settings.directory.cancelSwitch")}</Button></div>}
          </StepRow>

          {/* ② 填凭据：保存即检查 */}
          <StepRow
            n={2}
            title={t("settings.directory.wizard.step2")}
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

          {/* ③ 权限检查：一张清单 */}
          <StepRow
            n={3}
            title={t("settings.directory.wizard.step3")}
            state={stateOf(3)}
            warn={stateOf(3) === "done" && hasBad}
            summary={stateOf(3) === "done" && checkSummary}
            action={stateOf(3) === "done" && <Button size="sm" variant="ghost" onClick={() => setChecksOpen(!checksShown)}>{checksShown ? t("settings.directory.wizard.collapse") : t("settings.directory.wizard.view")}</Button>}
            open={checksShown}
          >
            <ChecklistStep
              cl={cl}
              providerTitle={cfg.provider_title}
              skipped={skipped}
              canSkip={stateOf(3) === "current" && !synced}
              onSkip={(k) => setSkipped((s) => (s.includes(k) ? s : [...s, k]))}
              onSkipAll={() => setSkipped(todos.map((c) => c.key))}
              onRecheck={runChecklist}
            />
          </StepRow>

          {/* ④ 同步范围 */}
          <StepRow
            n={4}
            title={t("settings.directory.wizard.step4")}
            state={stateOf(4)}
            summary={stateOf(4) === "done" && <span className="text-ink">{scopeSummary}</span>}
            action={stateOf(4) === "done" && <Button size="sm" variant="ghost" onClick={() => setOverride(4)}>{t("settings.directory.wizard.edit")}</Button>}
            open={stateOf(4) === "current"}
          >
            {provider && <ScopeStep key={`${cfg.root_department_id}|${rootIds.join(",")}|${suggested.length}`} cfg={cfg} provider={provider} suggested={suggested} roles={roleOptions} onSaved={onScopeSaved} onCancel={dataStep !== 4 ? () => setOverride(null) : undefined} />}
          </StepRow>

          {/* ⑤ 预览并同步 */}
          <StepRow
            n={5}
            title={t("settings.directory.wizard.step5")}
            state={stateOf(5)}
            summary={stateOf(5) === "done" && (
              synced
                ? <><span>{t("settings.directory.lastRun")}</span><span className="telemetry">{fmtDateTime(cfg.last_run!.started_at)}</span><RunStatus run={cfg.last_run!} /></>
                : <span>{t("settings.directory.wizard.notSynced")}</span>
            )}
            action={stateOf(5) === "done" && <Button size="sm" variant="ghost" onClick={() => setOverride(5)}>{t("settings.directory.wizard.again")}</Button>}
            open={stateOf(5) === "current"}
          >
            <SyncStep
              providerTitle={cfg.provider_title}
              ready={!!cl.data?.ready && blocked.length === 0}
              preview={lastPreview}
              onPreview={setLastPreview}
              busy={autoPreviewing}
              onDecided={bumpMappings}
              result={lastRun}
              onSynced={onSynced}
              onNext={() => setOverride(6)}
              onCancel={dataStep === null && !lastRun ? () => setOverride(null) : undefined}
            />
          </StepRow>

          {/* ⑥ 定时与代理 */}
          <StepRow
            n={6}
            title={t("settings.directory.wizard.step6")}
            state={stateOf(6)}
            summary={stateOf(6) === "done" && <span className="text-ink">{cfg.schedule_title} · {cfg.proxy_url ? t("settings.directory.wizard.viaProxy", { url: cfg.proxy_url }) : t("settings.directory.wizard.direct")}</span>}
            action={stateOf(6) === "done" && <Button size="sm" variant="ghost" onClick={() => setOverride(6)}>{t("settings.directory.wizard.edit")}</Button>}
            open={stateOf(6) === "current"}
            last
          >
            {provider && <ScheduleStep key={`${cfg.schedule}|${cfg.proxy_url}`} cfg={cfg} provider={provider} onDone={() => { setOverride(null); setLastRun(null); }} onSaved={() => config.reload()} />}
          </StepRow>
        </ol>
        </div>
        <ConsequenceDialog
          open={confirmSwitch}
          title={t("settings.directory.switch")}
          subject={t("settings.directory.switching", { name: cfg.provider_title })}
          effects={[t("settings.directory.switchEffect.keep", { name: cfg.provider_title }), t("settings.directory.switchEffect.manual"), t("settings.directory.switchEffect.realign")]}
          confirmLabel={t("settings.directory.switchGo")}
          danger
          onConfirm={startSwitch}
          onClose={() => setConfirmSwitch(false)}
        />
        <ConsequenceDialog
          open={confirmDisconnect}
          title={t("settings.directory.disconnectTitle")}
          subject={t("settings.directory.disconnectSubject", { name: cfg.provider_title })}
          effects={[t("settings.directory.disconnectEffect.creds"), t("settings.directory.disconnectEffect.people"), t("settings.directory.disconnectEffect.history")]}
          note={t("settings.directory.disconnectNote")}
          confirmLabel={t("settings.directory.disconnectGo")}
          danger
          busy={disconnecting}
          onConfirm={() => void disconnect()}
          onClose={() => setConfirmDisconnect(false)}
        />
      </Panel>
      {cfg.configured && <RunsPanel runs={runs.data} loading={runs.loading && !runs.data} error={runs.error} onRetry={runs.reload} />}
    </div>
  );
}

/** ② 的一行摘要：非保密字段「标题 值」，保密字段「标题 已设置」——字段按提供方声明来 */
function CredentialsSummary({ cfg, provider }: { cfg: DirectoryConfig; provider: DirectoryProviderInfo }) {
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

/** PUT 的底：带上已保存的一切（非保密凭据原样回传、保密省略 = 沿用），只改要改的字段。 */
function baseInput(cfg: DirectoryConfig): DirectoryInput {
  const input: DirectoryInput = { provider: cfg.provider ?? "", credentials: { ...cfg.credentials }, root_department_id: cfg.root_department_id, default_role: cfg.default_role, schedule: cfg.schedule, proxy_url: cfg.proxy_url };
  if (cfg.root_department_ids) input.root_department_ids = cfg.root_department_ids;
  return input;
}

/**
 * ② 凭据表单：字段按提供方声明渲染——保密字段是密码框（已设置时显示「已设置 ••••」和「重设」），非保密是普通输入框；
 * 上面一句提示（到哪儿去找）+ 「打开{平台}控制台」。校验与后端一句一样。保存成功后由父组件自动跑检查清单。
 */
function CredentialsStep({ cfg, provider, onSaved, onCancel }: { cfg: DirectoryConfig; provider: DirectoryProviderInfo; onSaved: () => void; onCancel?: () => void }) {
  const toast = useToast();
  const active = cfg.provider === provider.key;
  const secretsSet: Record<string, boolean> = Object.fromEntries(provider.fields.filter((f) => f.secret).map((f) => [f.key, active && !!cfg.secrets_set[f.key]]));
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(provider.fields.map((f) => [f.key, active && !f.secret ? cfg.credentials[f.key] ?? "" : ""])));
  // 每个保密字段单独一个「正在重设」：没设过的一开始就是输入框
  const [resetting, setResetting] = useState<Record<string, boolean>>(() => Object.fromEntries(Object.entries(secretsSet).map(([k, v]) => [k, !v])));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const setCred = (k: string, v: string) => setValues((d) => ({ ...d, [k]: v }));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    for (const f of provider.fields) {
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
      // 换到别的提供方：范围 / 角色 / 代理从头来；同一个提供方：其余设置原样带上
      const input: DirectoryInput = active ? { ...baseInput(cfg), credentials } : { provider: provider.key, credentials, root_department_id: "", root_department_ids: [], default_role: "", schedule: cfg.schedule, proxy_url: "" };
      await api.org.directory.save(input);
      toast.ok(t("settings.directory.saved"));
      onSaved();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  const tip = provider.tip ?? null;

  return (
    <form onSubmit={submit} className="max-w-[640px] space-y-4">
      <p className="text-body text-ink-muted">{t("settings.directory.wizard.credLead", { name: provider.title })}</p>
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
            onChange={(v) => setCred(f.key, v)}
            isSet={!!secretsSet[f.key]}
            resetting={!!resetting[f.key]}
            onReset={(on) => { setResetting((r) => ({ ...r, [f.key]: on })); if (!on) setCred(f.key, ""); }}
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

/**
 * ④ 同步范围：应用被授权的是部分部门时，列出这些部门让人勾选作为同步根（每个成为一个顶层团队）；
 * 是全员时，单选「整个企业」（默认）或「只同步某些部门」+ 根部门编号。顺带定同步进来的人的默认角色。
 */
function ScopeStep({ cfg, provider, suggested, roles, onSaved, onCancel }: { cfg: DirectoryConfig; provider: DirectoryProviderInfo; suggested: Array<{ id: string; name: string }>; roles: Array<[string, string]>; onSaved: () => void; onCancel?: () => void }) {
  const toast = useToast();
  const partial = suggested.length > 0;
  const savedIds = cfg.root_department_ids ?? [];
  const singleRoot = cfg.root_department_id && cfg.root_department_id !== provider.root_department_id ? cfg.root_department_id : "";
  const [ids, setIds] = useState<string[]>(() => (savedIds.length ? savedIds : singleRoot ? [singleRoot] : []));
  const [mode, setMode] = useState<"all" | "some">(savedIds.length || singleRoot ? "some" : "all");
  const [root, setRoot] = useState(singleRoot || savedIds[0] || "");
  const [role, setRole] = useState(cfg.default_role);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const toggle = (id: string, on: boolean) => setIds((s) => (on ? (s.includes(id) ? s : [...s, id]) : s.filter((x) => x !== id)));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    const input = baseInput(cfg);
    if (partial) {
      if (!ids.length) { setError(t("settings.directory.wizard.needRoots")); return; }
      input.root_department_ids = ids;
      input.root_department_id = ids[0];
    } else if (mode === "some") {
      if (!root.trim()) { setError(t("settings.directory.wizard.needRoot")); return; }
      input.root_department_ids = [];
      input.root_department_id = root.trim();
    } else {
      input.root_department_ids = [];
      input.root_department_id = "";
    }
    input.default_role = role;
    setBusy(true);
    try { await api.org.directory.save(input); toast.ok(t("settings.directory.wizard.scopeSaved")); onSaved(); } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };

  return (
    <form onSubmit={submit} className="max-w-[640px] space-y-4">
      <p className="text-body text-ink-muted">{partial ? t("settings.directory.wizard.scopePartialLead", { name: provider.title }) : t("settings.directory.wizard.scopeAllLead")}</p>
      {partial ? (
        <ul className="divide-y divide-hairline rounded-md border border-hairline" data-suggested-roots>
          {suggested.map((r) => (
            <li key={r.id} className="flex items-center gap-3 px-3 py-2">
              <Checkbox label={<span className="font-medium text-ink">{r.name}</span>} checked={ids.includes(r.id)} onChange={(e) => toggle(r.id, e.target.checked)} />
              <span className="telemetry ml-auto truncate" title={r.id}>{r.id}</span>
            </li>
          ))}
        </ul>
      ) : (
        <div className="space-y-2">
          <label className="flex items-center gap-1.5 text-body"><input type="radio" name="scope" className="h-3.5 w-3.5" checked={mode === "all"} onChange={() => setMode("all")} />{t("settings.directory.wizard.scopeAll")}</label>
          <label className="flex items-center gap-1.5 text-body"><input type="radio" name="scope" className="h-3.5 w-3.5" checked={mode === "some"} onChange={() => setMode("some")} />{t("settings.directory.wizard.scopeSome")}</label>
          {mode === "some" && (
            <Field label={t("settings.directory.rootDept")} hint={t("settings.directory.rootDeptHint", { name: provider.title, root: provider.root_department_id })} className="pl-5">
              <Input value={root} onChange={(e) => setRoot(e.target.value)} placeholder={provider.root_department_id} spellCheck={false} />
            </Field>
          )}
        </div>
      )}
      <Field label={t("settings.directory.defaultRole")} className="sm:max-w-[300px]">
        <Select value={role} onChange={(e) => setRole(e.target.value)}>
          <option value="">{t("settings.directory.noDefaultRole")}</option>
          {roles.map(([name, title]) => <option key={name} value={name}>{title}</option>)}
        </Select>
      </Field>
      {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" variant="primary" disabled={busy}>{busy ? t("common.saving") : t("settings.directory.wizard.saveScope")}</Button>
        {onCancel && <Button variant="ghost" onClick={onCancel}>{t("settings.directory.wizard.keep")}</Button>}
      </div>
    </form>
  );
}

const ACTION_TONE: Record<DirectoryPreview["teams"][number]["action"], Tone> = { create: "success", update: "accent", keep: "neutral", confirm: "warning" };

/** ⑤ 预览 + 立即同步：预览不落库；「立即同步」只在预览过、且检查没有阻塞时出现；同步后显示结果与邀请链接，再「下一步：定时」。 */
function SyncStep({ providerTitle, ready, preview, onPreview, busy = false, onDecided, result, onSynced, onNext, onCancel }: { providerTitle: string; ready: boolean; preview: DirectoryPreview | null; onPreview: (p: DirectoryPreview | null) => void; /** 父组件正在替这一步拉预览（状态视图的「预览同步」） */ busy?: boolean; onDecided?: () => void; result: DirectoryRun | null; onSynced: (r: DirectoryRun) => void; onNext: () => void; onCancel?: () => void }) {
  const toast = useToast();
  const [previewingLocal, setPreviewing] = useState(false);
  const previewing = previewingLocal || busy;
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [syncing, setSyncing] = useState(false);

  const runPreview = async () => {
    setPreviewing(true); setPreviewError(null);
    try { onPreview(await api.org.directory.preview()); } catch (err) { setPreviewError(errorMessage(err)); } finally { setPreviewing(false); }
  };
  // 决定之后：本地先把那一行挪到「已决定」，再静默刷新一次预览让数字对上
  const refreshQuiet = async () => { try { onPreview(await api.org.directory.preview()); } catch { /* 保留本地状态 */ } onDecided?.(); };
  const blockedN = preview?.blocked_by_confirmations ?? preview?.confirmations?.length ?? 0;
  const runSync = async () => {
    setSyncing(true);
    try {
      const r = await api.org.directory.sync();
      setConfirming(false);
      // 后端在有阻塞项时不报 400，而是记一次失败的运行：把那句原话当错误给出来
      if (r.status === "failed") toast.fail(r.errors[0] ?? t("common.error"));
      else toast.ok(t("settings.directory.synced", { a: r.added_members, u: r.updated_members, d: r.deactivated_members }));
      onSynced(r);
    } catch (err) { toast.fail(errorMessage(err)); } finally { setSyncing(false); }
  };
  const willDeactivate = preview ? preview.members_to_deactivate.length + preview.teams_to_deactivate.length : 0;

  return (
    <div>
      {!result && <p className="mb-3 max-w-[640px] text-body text-ink-muted">{t("settings.directory.wizard.previewLead")}</p>}
      <div className="flex flex-wrap items-center gap-2">
        {!result && <Button icon={<IconSearch />} disabled={previewing} onClick={() => void runPreview()} variant={preview ? "default" : "primary"}>{previewing ? t("settings.directory.previewing") : t("settings.directory.preview")}</Button>}
        {!result && preview && (
          <Tip tip={blockedN > 0 ? t("settings.directory.confirm.blockedTip", { n: blockedN }) : ready ? null : t("settings.directory.wizard.notReady")}>
            <Button variant="primary" icon={<IconPlay />} disabled={syncing || !ready || blockedN > 0} onClick={() => setConfirming(true)} data-sync-button data-blocked={blockedN || undefined}>
              {blockedN > 0 ? t("settings.directory.confirm.blockedButton", { n: blockedN }) : t("settings.directory.sync")}
            </Button>
          </Tip>
        )}
        {result && <Button variant="primary" onClick={onNext}>{t("settings.directory.wizard.nextSchedule")}</Button>}
        {onCancel && !preview && !result && <Button variant="ghost" onClick={onCancel}>{t("settings.directory.wizard.keep")}</Button>}
      </div>
      {previewError && <ErrorBox message={previewError} onRetry={() => void runPreview()} className="mt-3 max-w-[640px]" />}
      {preview && !result && <PreviewTable preview={preview} providerTitle={providerTitle} onPreview={onPreview} onDecided={() => void refreshQuiet()} className="mt-4" />}
      {result && <SyncResult run={result} className="mt-4" />}
      <ConfirmDialog
        open={confirming}
        title={t("settings.directory.sync")}
        confirmLabel={syncing ? t("settings.directory.syncing") : t("settings.directory.sync")}
        danger={willDeactivate > 0}
        busy={syncing}
        onConfirm={() => void runSync()}
        onClose={() => setConfirming(false)}
        message={
          <>
            {t("settings.directory.syncConfirm", { name: providerTitle })}
            <br />
            <span className={cx("mt-2 block", willDeactivate > 0 && "text-danger")}>
              {preview ? (willDeactivate > 0 ? t("settings.directory.syncConfirmDeactivate", { members: preview.members_to_deactivate.length, teams: preview.teams_to_deactivate.length }) : t("settings.directory.noDeactivate")) : t("settings.directory.syncConfirmNoPreview")}
            </span>
          </>
        }
      />
    </div>
  );
}

/** 预览表格：团队逐个（新建 / 更新 / 保持 + 上级）、成员数量、将停用的人逐个列出（最危险的一步，必须先看到）、需要手工处理的备注。 */
function PreviewTable({ preview, providerTitle, onPreview, onDecided, className }: { preview: DirectoryPreview; providerTitle: string; onPreview?: (p: DirectoryPreview) => void; onDecided?: () => void; className?: string }) {
  const nameOf = (ext: string | undefined) => (ext ? preview.teams.find((x) => x.external_id === ext)?.name ?? ext : t("settings.directory.topLevel"));
  const willDeactivate = preview.members_to_deactivate.length + preview.teams_to_deactivate.length;
  return (
    <div className={cx("space-y-4", className)}>
      {onPreview && <Confirmations preview={preview} providerTitle={providerTitle} onPreview={onPreview} onDecided={onDecided} />}
      <section>
        <div className="eyebrow mb-1.5 text-ink-subtle">{t("settings.directory.previewTeams")}</div>
        <div className="overflow-hidden rounded-md border border-hairline">
          <Table>
            <thead><tr><th className="w-full">{t("settings.teams.name")}</th><th className="w-[110px]">{t("settings.directory.plan")}</th><th className="w-[220px]">{t("settings.directory.parent")}</th></tr></thead>
            <tbody>
              {preview.teams.map((x) => (
                <tr key={x.external_id}>
                  <td className="w-full max-w-0 truncate font-medium" title={x.name}>{x.name}</td>
                  <td className="whitespace-nowrap"><Tag tone={ACTION_TONE[x.action]}>{t(`settings.directory.action.${x.action}` as Key)}</Tag></td>
                  <td className="max-w-[220px] truncate whitespace-nowrap text-ink-muted">{nameOf(x.parent_external_id)}</td>
                </tr>
              ))}
              {preview.teams_to_deactivate.map((x) => (
                <tr key={x.id} className="text-ink-subtle">
                  <td className="w-full max-w-0 truncate font-medium line-through" title={x.name}>{x.name}</td>
                  <td className="whitespace-nowrap"><Tag tone="danger">{t("settings.teams.inactive")}</Tag></td>
                  <td className="whitespace-nowrap">—</td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      </section>
      <section>
        <div className="eyebrow mb-1.5 text-ink-subtle">{t("settings.directory.previewMembers")}</div>
        <div className="flex flex-wrap items-center gap-1.5">
          <Tag>{t("settings.directory.membersTotal", { name: providerTitle, n: preview.members_total })}</Tag>
          <Tag tone="success">{t("settings.directory.membersNew", { n: preview.members_new })}</Tag>
          <Tag>{t("settings.directory.membersExisting", { n: preview.members_existing })}</Tag>
        </div>
        {willDeactivate > 0 ? (
          <div className="mt-2 rounded-md border border-danger-border bg-danger-bg p-3 text-body">
            {preview.members_to_deactivate.length > 0 && (
              <div className="flex flex-wrap items-center gap-1.5 text-danger">
                <span className="font-medium">{t("settings.directory.willDeactivateMembers", { n: preview.members_to_deactivate.length })}</span>
                {preview.members_to_deactivate.map((m) => <Tag key={m.id} tone="danger">{m.name}</Tag>)}
              </div>
            )}
            {preview.teams_to_deactivate.length > 0 && (
              <div className={cx("flex flex-wrap items-center gap-1.5 text-danger", preview.members_to_deactivate.length > 0 && "mt-1.5")}>
                <span className="font-medium">{t("settings.directory.willDeactivateTeams", { n: preview.teams_to_deactivate.length })}</span>
                {preview.teams_to_deactivate.map((x) => <Tag key={x.id} tone="danger">{x.name}</Tag>)}
              </div>
            )}
          </div>
        ) : (
          <p className="mt-2 text-body text-ink-muted">{t("settings.directory.noDeactivate")}</p>
        )}
      </section>
      {preview.notes.length > 0 && (
        <section>
          <div className="eyebrow mb-1.5 text-ink-subtle">{t("settings.directory.notes")}</div>
          <ul className="list-disc space-y-0.5 pl-5 text-body text-warning">
            {preview.notes.map((n, i) => <li key={i}>{n}</li>)}
          </ul>
        </section>
      )}
    </div>
  );
}

// ---------- 冲突：需要你确认 / 已决定 / 已跳过（ADR 0017 补记四） ----------
type Decision = "merge" | "create" | "skip";
const KIND_ICON: Record<DirectoryKind, typeof IconTeam> = { team: IconTeam, member: IconMember };

/**
 * 预览表格上方的「需要你确认（N）」：每条一行——左边 IM 里的对象（名字、部门 / 上级、遮住的邮箱或手机尾号），中间"可能是"，
 * 右边系统里的疑似对象（名字 · 团队 · 来源；多个候选时是下拉）+ 一句认法，最右三个动作。决定立刻写到后端，行挪进折叠的「已决定」；
 * 「已跳过」单列一小组；两处都能「重新考虑」。团队选「合并到已有」先在行内写明"旧团队的成员、目标、任务、迭代会并过去，旧团队停用"再确认。
 */
function Confirmations({ preview, providerTitle, onPreview, onDecided }: { preview: DirectoryPreview; providerTitle: string; onPreview: (p: DirectoryPreview) => void; onDecided?: () => void }) {
  const toast = useToast();
  const confirmations = preview.confirmations ?? [];
  const decided = preview.decided ?? [];
  const skipped = preview.skipped ?? [];
  const [busy, setBusy] = useState<string | null>(null);
  const [decidedOpen, setDecidedOpen] = useState(false);
  const keyOf = (kind: DirectoryKind, ext: string) => `${kind}:${ext}`;
  if (!confirmations.length && !decided.length && !skipped.length) return null;

  const decide = async (c: DirectoryConfirmation, decision: Decision, localId?: string) => {
    const k = keyOf(c.kind, c.external.id);
    setBusy(k);
    try {
      const [rec] = await api.org.directory.decide([{ kind: c.kind, external_id: c.external.id, external_name: c.external.name, decision, local_id: localId }]);
      // 本地先挪：不等下一次预览
      const rest = confirmations.filter((x) => x !== c);
      const next: DirectoryPreview = { ...preview, confirmations: rest, blocked_by_confirmations: rest.length };
      if (decision === "skip") next.skipped = [...skipped, { kind: c.kind, external_id: c.external.id, external_name: c.external.name, decided_at: rec?.decided_at ?? new Date().toISOString() }];
      else {
        const target = localId ? c.candidates.find((x) => x.local_id === localId) : undefined;
        const row: DirectoryDecided = { ...c, decision, decision_title: rec?.decision_title ?? t(`settings.directory.confirm.${decision}` as Key), decided_local_id: target?.local_id ?? null, decided_local_name: target?.name ?? null };
        next.decided = [...decided, row];
      }
      onPreview(next);
      toast.ok(t("settings.directory.confirm.decidedToast"));
      onDecided?.();
    } catch (err) { toast.fail(errorMessage(err)); } finally { setBusy(null); }
  };
  const reconsider = async (kind: DirectoryKind, ext: string) => {
    const k = keyOf(kind, ext);
    setBusy(k);
    try {
      await api.org.directory.reconsider(kind, ext);
      toast.ok(t("settings.directory.confirm.reconsidered"));
      // 撤回后那条要重新变回候选：让后端重算
      onPreview(await api.org.directory.preview());
      onDecided?.();
    } catch (err) { toast.fail(errorMessage(err)); } finally { setBusy(null); }
  };

  return (
    <div className="space-y-3" data-confirmations>
      {confirmations.length > 0 && (
        <section className="rounded-md border border-warning-border bg-warning-bg/40" data-confirm-open={confirmations.length}>
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1 px-3 pt-2.5 pb-1.5">
            <span className="text-body font-medium text-warning">{t("settings.directory.confirm.title")}<span className="telemetry ml-1.5">{confirmations.length}</span></span>
            <span className="text-caption text-ink-muted">{t("settings.directory.confirm.lead")}</span>
          </div>
          <ul className="divide-y divide-warning-border/60 border-t border-warning-border/60">
            {confirmations.map((c) => <ConfirmRow key={keyOf(c.kind, c.external.id)} item={c} providerTitle={providerTitle} busy={busy === keyOf(c.kind, c.external.id)} onDecide={(d, id) => void decide(c, d, id)} />)}
          </ul>
        </section>
      )}
      {decided.length > 0 && (
        <section className="rounded-md border border-hairline bg-surface-1" data-decided={decided.length}>
          <button type="button" className="flex w-full items-center gap-2 px-3 py-2 text-left text-body" aria-expanded={decidedOpen} onClick={() => setDecidedOpen((v) => !v)}>
            <span className={cx("inline-block text-[10px] text-ink-subtle transition-transform", decidedOpen && "rotate-90")} aria-hidden="true">▶</span>
            <span className="font-medium text-ink">{t("settings.directory.confirm.decided")}<span className="telemetry ml-1.5">{decided.length}</span></span>
            {!decidedOpen && <span className="min-w-0 truncate text-caption text-ink-subtle">{decided.map((d) => d.external.name).join("、")}</span>}
          </button>
          {decidedOpen && (
            <ul className="divide-y divide-hairline border-t border-hairline">
              {decided.map((d) => {
                const Icon = KIND_ICON[d.kind];
                const k = keyOf(d.kind, d.external.id);
                return (
                  <li key={k} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-body" data-decided-row={d.external.id}>
                    <Icon size={14} className="shrink-0 text-ink-subtle" aria-hidden="true" />
                    <span className="font-medium text-ink">{d.external.name}</span>
                    <span className="text-ink-subtle">→</span>
                    {d.decision === "merge" ? <span className="text-ink-muted">{t("settings.directory.confirm.mergeInto", { name: d.decided_local_name ?? d.decided_local_id ?? "" })}</span> : <Tag tone="success">{d.decision_title || t("settings.directory.confirm.create")}</Tag>}
                    <span className="ml-auto"><Button size="sm" variant="ghost" disabled={busy === k} onClick={() => void reconsider(d.kind, d.external.id)}>{t("settings.directory.confirm.reconsider")}</Button></span>
                  </li>
                );
              })}
            </ul>
          )}
        </section>
      )}
      {skipped.length > 0 && <SkippedList items={skipped} busy={busy} onReconsider={(kind, ext) => void reconsider(kind, ext)} className="rounded-md border border-hairline bg-surface-1" title />}
    </div>
  );
}

/** 一条需要确认的对象。右边有多个候选时是下拉；团队的「合并到已有」先展开一句说明再「确认合并」。 */
function ConfirmRow({ item: c, providerTitle, busy, onDecide }: { item: DirectoryConfirmation; providerTitle: string; busy: boolean; onDecide: (d: Decision, localId?: string) => void }) {
  const [picked, setPicked] = useState(c.candidates[0]?.local_id ?? "");
  const [confirmingMerge, setConfirmingMerge] = useState(false);
  const Icon = KIND_ICON[c.kind];
  const cand = c.candidates.find((x) => x.local_id === picked) ?? c.candidates[0];
  const ext = c.external;
  const caption = [ext.dept ?? ext.parent, ext.email_masked, ext.mobile_tail ? t("settings.directory.confirm.mobileTail", { tail: ext.mobile_tail }) : null].filter(Boolean).join(" · ");
  const merge = () => { if (c.kind === "team" && !confirmingMerge) { setConfirmingMerge(true); return; } onDecide("merge", cand?.local_id); };
  return (
    <li className={cx("px-3 py-2.5", busy && "opacity-60")} aria-busy={busy || undefined} data-confirm-row={ext.id} data-kind={c.kind}>
      <div className="grid items-center gap-x-3 gap-y-1.5 md:grid-cols-[minmax(0,1fr)_auto_minmax(0,1.6fr)_auto]">
        {/* 左：IM 里的对象 */}
        <div className="flex min-w-0 items-start gap-2">
          <Icon size={14} className="mt-[3px] shrink-0 text-ink-subtle" aria-hidden="true" />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-1.5"><span className="truncate font-medium text-ink" title={ext.name}>{ext.name}</span><Tag tone="accent">{t("settings.directory.from", { name: providerTitle })}</Tag></div>
            {caption && <div className="truncate text-caption text-ink-subtle" title={caption}>{caption}</div>}
          </div>
        </div>
        {/* 中 */}
        <span className="text-caption text-ink-subtle md:px-1">{t("settings.directory.confirm.maybe")}</span>
        {/* 右：系统里的疑似对象 */}
        <div className="min-w-0">
          {c.candidates.length > 1 ? (
            <Select value={picked} onChange={(e) => setPicked(e.target.value)} aria-label={t("settings.directory.confirm.pickCandidate")} className="w-full">
              {c.candidates.map((x) => <option key={x.local_id} value={x.local_id}>{x.name} · {x.team_path || t("settings.directory.topLevel")} · {isSynced(x) ? t("settings.directory.from", { name: x.source_title }) : x.source_title}</option>)}
            </Select>
          ) : cand ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="truncate font-medium text-ink" title={cand.name}>{cand.name}</span>
              {cand.team_path && <span className="truncate text-caption text-ink-muted" title={cand.team_path}>· {cand.team_path}</span>}
              <Tag>{isSynced(cand) ? t("settings.directory.from", { name: cand.source_title }) : cand.source_title}</Tag>
            </div>
          ) : null}
          {cand?.reason_text && <div className="mt-0.5 text-caption text-ink-subtle" data-reason={cand.reason}>{cand.reason_text}</div>}
        </div>
        {/* 动作 */}
        <div className="flex flex-wrap items-center justify-end gap-1.5">
          <Button size="sm" variant={confirmingMerge ? "default" : "primary"} disabled={busy || !cand} onClick={merge}>{t("settings.directory.confirm.merge")}</Button>
          <Button size="sm" disabled={busy} onClick={() => onDecide("create")}>{t("settings.directory.confirm.create")}</Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => onDecide("skip")}>{t("settings.directory.confirm.skip")}</Button>
        </div>
      </div>
      {confirmingMerge && (
        <div className="mt-2 flex flex-wrap items-center gap-2 rounded-md border border-warning-border bg-warning-bg px-3 py-2 text-body text-warning" role="note" data-team-merge-note>
          <span className="min-w-0 flex-1">{t("settings.directory.confirm.teamMergeNote")}</span>
          <Button size="sm" variant="danger" disabled={busy} onClick={() => onDecide("merge", cand?.local_id)}>{t("settings.directory.confirm.confirmMerge")}</Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmingMerge(false)}>{t("common.cancel")}</Button>
        </div>
      )}
    </li>
  );
}

/** 已跳过的对象：名字、什么时候跳过的、「重新考虑」。预览里与对应关系面板共用。 */
function SkippedList({ items, busy, onReconsider, className, title }: { items: DirectorySkipped[]; busy: string | null; onReconsider: (kind: DirectoryKind, ext: string) => void; className?: string; title?: boolean }) {
  return (
    <section className={className} data-skipped={items.length}>
      {title && <div className="px-3 py-2 text-body font-medium text-ink">{t("settings.directory.confirm.skipped")}<span className="telemetry ml-1.5">{items.length}</span></div>}
      <ul className={cx("divide-y divide-hairline", title && "border-t border-hairline")}>
        {items.map((x) => {
          const Icon = KIND_ICON[x.kind];
          const k = `${x.kind}:${x.external_id}`;
          return (
            <li key={k} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-body" data-skipped-row={x.external_id}>
              <Icon size={14} className="shrink-0 text-ink-subtle" aria-hidden="true" />
              <span className="font-medium text-ink-muted">{x.external_name}</span>
              <span className="telemetry text-ink-subtle">{t("settings.directory.confirm.skippedOn", { date: fmtDate(x.decided_at) })}</span>
              <span className="ml-auto"><Button size="sm" variant="ghost" disabled={busy === k} onClick={() => onReconsider(x.kind, x.external_id)}>{t("settings.directory.confirm.reconsider")}</Button></span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

// ---------- 对应关系面板：已绑定 / 可能重复 / 已跳过 ----------
type MappingTab = "bound" | "duplicates" | "skipped";

/**
 * 接入完成后常驻在状态摘要下面。已绑定：种类图标、IM 名 → 本地名、绑定时间，「解绑」要确认（说清对象改为手工维护）；
 * 可能重复：a ↔ b（各带来源标签）+ 认法，「合并」开合并对话框（默认保留同步的那个）；已跳过：「重新考虑」。页签标签带数量，各有空态。
 */
function MappingsPanel({ providerTitle, onChanged, className }: { providerTitle: string; onChanged: () => void; className?: string }) {
  const toast = useToast();
  const data = useLoad(() => api.org.directory.mappings(), []);
  // ?mappings=bound|duplicates|skipped：从成员页的「去解绑」过来时直接停在那一页（参数看过就清掉，不粘在地址栏上）
  const wanted = useQueryParam("mappings");
  const [tab, setTab] = useState<MappingTab>(() => (wanted === "duplicates" || wanted === "skipped" ? wanted : "bound"));
  useEffect(() => { if (wanted) setQueryParams({ mappings: null }); }, [wanted]);
  const [filter, setFilter] = useState("");
  const [unbinding, setUnbinding] = useState<DirectoryBinding | null>(null);
  const [merging, setMerging] = useState<DirectoryDuplicate | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const m = data.data;
  const bound = useMemo(() => m?.bound ?? [], [m]);
  const dups = m?.duplicates ?? [];
  const skipped = m?.skipped ?? [];
  const shownBound = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const list = q ? bound.filter((b) => (b.external_name ?? "").toLowerCase().includes(q) || b.local_name.toLowerCase().includes(q)) : bound;
    // 团队在前，再按名字
    return [...list].sort((a, b) => (a.kind === b.kind ? a.local_name.localeCompare(b.local_name, "zh-Hans-CN") : a.kind === "team" ? -1 : 1));
  }, [bound, filter]);
  const changed = () => { data.reload(); onChanged(); };
  const unbind = async (b: DirectoryBinding) => {
    setBusy(`${b.kind}:${b.external_id}`);
    try { await api.org.directory.unbind(b.kind, b.external_id); toast.ok(t("settings.directory.mappings.unbound", { name: b.local_name })); setUnbinding(null); changed(); } catch (err) { toast.fail(errorMessage(err)); } finally { setBusy(null); }
  };
  const reconsider = async (kind: DirectoryKind, ext: string) => {
    setBusy(`${kind}:${ext}`);
    try { await api.org.directory.reconsider(kind, ext); toast.ok(t("settings.directory.confirm.reconsidered")); changed(); } catch (err) { toast.fail(errorMessage(err)); } finally { setBusy(null); }
  };
  const items: Array<{ key: MappingTab; label: string; count: number }> = [
    { key: "bound", label: t("settings.directory.mappings.bound"), count: bound.length },
    { key: "duplicates", label: t("settings.directory.mappings.duplicates"), count: dups.length },
    { key: "skipped", label: t("settings.directory.mappings.skipped"), count: skipped.length },
  ];
  return (
    <section className={cx("rounded-md border border-hairline bg-surface-1", className)} data-mappings aria-label={t("settings.directory.mappings.title")}>
      <div className="px-3 pt-2.5">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
          <h3 className="text-body font-medium text-ink">{t("settings.directory.mappings.title")}</h3>
          <span className="text-caption text-ink-subtle">{t("settings.directory.mappings.lead", { name: providerTitle })}</span>
        </div>
        <Tabs value={tab} items={items} onChange={setTab} label={t("settings.directory.mappings.title")} className="mt-1" actions={tab === "bound" && bound.length > 8 ? <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t("settings.directory.mappings.filter")} aria-label={t("settings.directory.mappings.filter")} className="h-7 w-[180px] text-caption" /> : undefined} />
      </div>
      {data.loading && !m ? <div className="p-3"><ListSkeleton rows={3} /></div> : data.error && !m ? <div className="p-3"><ErrorBox message={data.error} onRetry={data.reload} /></div> : (
        <div className={cx("max-h-[360px] overflow-y-auto", data.loading && "opacity-60 transition-opacity")} aria-busy={data.loading || undefined} data-mappings-tab={tab}>
          {tab === "bound" && (
            !bound.length ? <Empty text={t("settings.directory.mappings.boundEmpty")} illustration={false} className="py-6" />
            : !shownBound.length ? <Empty text={t("settings.directory.mappings.noMatch")} illustration={false} className="py-6" />
            : (
              <ul className="divide-y divide-hairline">
                {shownBound.map((b) => {
                  const Icon = KIND_ICON[b.kind];
                  const k = `${b.kind}:${b.external_id}`;
                  return (
                    <li key={k} className="flex items-center gap-3 px-3 py-1.5 text-body" data-bound-row={b.external_id}>
                      <Icon size={14} className="shrink-0 text-ink-subtle" aria-label={t(`settings.directory.mappings.kind.${b.kind}` as Key)} />
                      <span className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-0.5">
                        <span className="truncate text-ink-muted" title={b.external_name}>{b.external_name || b.external_id}</span>
                        <span className="text-ink-subtle" aria-hidden="true">→</span>
                        <span className={cx("truncate font-medium", b.local_active ? "text-ink" : "text-ink-subtle line-through")} title={b.local_name}>{b.local_name}</span>
                        {!b.local_active && <Tag dark>{t("settings.directory.mappings.inactive")}</Tag>}
                      </span>
                      <span className="telemetry hidden shrink-0 text-ink-subtle sm:inline">{t("settings.directory.mappings.since", { date: fmtDate(b.since) })}</span>
                      <Button size="sm" variant="ghost" className="shrink-0" disabled={busy === k} onClick={() => setUnbinding(b)}>{t("settings.directory.mappings.unbind")}</Button>
                    </li>
                  );
                })}
              </ul>
            )
          )}
          {tab === "duplicates" && (
            !dups.length ? <Empty text={t("settings.directory.mappings.duplicatesEmpty")} illustration={false} className="py-6" /> : (
              <>
                <p className="px-3 pt-2 text-caption text-ink-subtle">{t("settings.directory.mappings.duplicateLead")}</p>
                <ul className="divide-y divide-hairline">
                  {dups.map((d) => {
                    const Icon = KIND_ICON[d.kind];
                    return (
                      <li key={`${d.kind}:${d.a.id}:${d.b.id}`} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-body" data-dup-row={`${d.a.id}:${d.b.id}`}>
                        <Icon size={14} className="shrink-0 text-ink-subtle" aria-hidden="true" />
                        <DupSide side={d.a} />
                        <span className="text-ink-subtle" aria-hidden="true">↔</span>
                        <DupSide side={d.b} />
                        <span className="w-full text-caption text-ink-subtle sm:ml-auto sm:w-auto" data-reason={d.reason}>{d.reason_text}</span>
                        <Button size="sm" onClick={() => setMerging(d)}>{t("settings.directory.mappings.merge")}</Button>
                      </li>
                    );
                  })}
                </ul>
              </>
            )
          )}
          {tab === "skipped" && (
            !skipped.length ? <Empty text={t("settings.directory.mappings.skippedEmpty")} illustration={false} className="py-6" /> : <SkippedList items={skipped} busy={busy} onReconsider={(kind, ext) => void reconsider(kind, ext)} />
          )}
        </div>
      )}
      <ConfirmDialog
        open={!!unbinding}
        title={unbinding ? t("settings.directory.mappings.unbindTitle", { name: unbinding.local_name }) : ""}
        message={unbinding ? t("settings.directory.mappings.unbindConfirm", { name: unbinding.local_name, provider: providerTitle }) : null}
        confirmLabel={t("settings.directory.mappings.unbind")}
        danger
        busy={!!busy}
        onConfirm={() => unbinding && void unbind(unbinding)}
        onClose={() => setUnbinding(null)}
      />
      {merging && <MergeDialog key={`${merging.a.id}:${merging.b.id}`} kind={merging.kind} a={merging.a} b={merging.b} reason={merging.reason_text} providerTitle={providerTitle} onClose={() => setMerging(null)} onMerged={changed} />}
    </section>
  );
}

function DupSide({ side }: { side: DirectoryDuplicate["a"] }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <span className="truncate font-medium text-ink" title={side.name}>{side.name}</span>
      {side.team_path && <span className="hidden truncate text-caption text-ink-muted md:inline" title={side.team_path}>{side.team_path}</span>}
      <Tag tone={isSynced(side) ? "accent" : "neutral"}>{isSynced(side) ? t("settings.directory.from", { name: side.source_title }) : side.source_title}</Tag>
    </span>
  );
}

/** ⑥ 频率与代理：有改动是「保存并完成」，没改动是「完成」；完成后回到状态视图。 */
function ScheduleStep({ cfg, provider, onDone, onSaved }: { cfg: DirectoryConfig; provider: DirectoryProviderInfo; onDone: () => void; onSaved: () => void }) {
  const toast = useToast();
  const [schedule, setSchedule] = useState<DirectorySchedule>(cfg.schedule);
  const [proxy, setProxy] = useState(cfg.proxy_url);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dirty = schedule !== cfg.schedule || proxy.trim() !== cfg.proxy_url;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!dirty) { onDone(); return; }
    setBusy(true); setError(null);
    try {
      await api.org.directory.save({ ...baseInput(cfg), schedule, proxy_url: proxy.trim() });
      toast.ok(t("settings.directory.saved"));
      onSaved(); onDone();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <form onSubmit={submit} className="max-w-[640px] space-y-4">
      <p className="text-body text-ink-muted">{t("settings.directory.wizard.scheduleLead")}</p>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("settings.directory.schedule")}>
          <Select value={schedule} onChange={(e) => setSchedule(e.target.value as DirectorySchedule)}>
            {cfg.schedules.map((s) => <option key={s.value} value={s.value}>{s.title}</option>)}
          </Select>
        </Field>
        <Field label={t("settings.directory.proxy")} hint={t("settings.directory.proxyHint", { name: provider.title })}><Input value={proxy} onChange={(e) => setProxy(e.target.value)} placeholder="http://proxy.internal:3128" spellCheck={false} /></Field>
      </div>
      {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" variant="primary" disabled={busy}>{busy ? t("common.saving") : dirty ? t("settings.directory.wizard.saveAndDone") : t("settings.directory.wizard.done")}</Button>
      </div>
    </form>
  );
}

/** 本次同步结果：数量、错误、一次性的邀请链接（带复制）。 */
function SyncResult({ run, className }: { run: DirectoryRun; className?: string }) {
  const toast = useToast();
  const copy = (url: string) => { void navigator.clipboard?.writeText(url); toast.ok(t("common.copied")); };
  return (
    <div className={cx("max-w-[640px]", className)}>
      <div className="eyebrow mb-1.5 flex items-center gap-2 text-ink-subtle">{t("settings.directory.result")}<RunStatus run={run} /></div>
      <DescList
        items={[
          [t("settings.directory.previewTeams"), t("settings.directory.teamsLine", { a: run.added_teams, u: run.updated_teams, d: run.deactivated_teams })],
          [t("settings.directory.previewMembers"), t("settings.directory.membersLine", { a: run.added_members, u: run.updated_members, d: run.deactivated_members })],
        ]}
      />
      {run.errors.length > 0 && (
        <div className="mt-3">
          <div className="eyebrow mb-1 text-ink-subtle">{t("settings.directory.errors")}</div>
          <ul className="list-disc space-y-0.5 pl-5 text-body text-danger">{run.errors.map((e, i) => <li key={i}>{e}</li>)}</ul>
        </div>
      )}
      {run.invitations && run.invitations.length > 0 && (
        <div className="mt-3">
          <div className="eyebrow mb-1 text-ink-subtle">{t("settings.directory.invitations")}</div>
          <p className="mb-2 text-caption text-ink-subtle">{t("settings.directory.invitationsHint")}</p>
          <ul className="divide-y divide-hairline rounded-md border border-hairline">
            {run.invitations.map((i) => (
              <li key={i.member_id} className="flex items-center gap-3 px-3 py-1.5">
                <span className="w-[120px] shrink-0 truncate font-medium" title={i.name}>{i.name}</span>
                <code className="telemetry min-w-0 flex-1 truncate text-ink-muted" title={i.url}>{i.url}</code>
                <Button size="sm" variant="ghost" icon={<IconCopy />} onClick={() => copy(i.url)}>{t("common.copy")}</Button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

const RUN_TONE: Record<DirectoryRun["status"], Tone> = { ok: "success", partial: "warning", failed: "danger", running: "accent" };
function RunStatus({ run }: { run: DirectoryRun }) {
  return <Tag tone={RUN_TONE[run.status] ?? "neutral"}>{run.status_title || run.status}</Tag>;
}
const durationSec = (r: DirectoryRun) => {
  const a = parseDate(r.started_at), b = parseDate(r.finished_at);
  return a && b ? Math.max(0, Math.round((b.getTime() - a.getTime()) / 1000)) : null;
};

/** 历次同步：接口已按时间倒序给，最新在上。 */
function RunsPanel({ runs, loading, error, onRetry }: { runs: DirectoryRun[] | null; loading: boolean; error: string | null; onRetry: () => void }) {
  return (
    <Panel index={2} title={t("settings.directory.runs")} padded={false} telemetry={runs ? t("panel.rows", { n: runs.length }) : undefined}>
      {loading ? <div className="p-4"><ListSkeleton rows={2} /></div> : error ? <div className="p-4"><ErrorBox message={error} onRetry={onRetry} /></div> : !runs?.length ? <Empty text={t("settings.directory.runsEmpty")} illustration={false} className="py-6" /> : (
        <Table>
          <thead>
            <tr>
              <th className="w-[170px]">{t("settings.directory.started")}</th><th className="w-[100px]">{t("settings.directory.status")}</th><th className="w-[240px]">{t("settings.directory.previewTeams")}</th><th className="w-[240px]">{t("settings.directory.previewMembers")}</th><th className="w-full">{t("settings.directory.errors")}</th>
            </tr>
          </thead>
          <tbody>
            {runs.map((r, i) => {
              const sec = durationSec(r);
              return (
                <tr key={r.id ?? i}>
                  <td className="telemetry whitespace-nowrap">{fmtDateTime(r.started_at)}{sec !== null && <span className="text-ink-subtle"> · {t("settings.directory.duration", { s: sec })}</span>}</td>
                  <td><RunStatus run={r} /></td>
                  <td className="whitespace-nowrap text-ink-muted">{t("settings.directory.teamsLine", { a: r.added_teams, u: r.updated_teams, d: r.deactivated_teams })}</td>
                  <td className="whitespace-nowrap text-ink-muted">{t("settings.directory.membersLine", { a: r.added_members, u: r.updated_members, d: r.deactivated_members })}</td>
                  <td className="max-w-0">{r.errors.length ? <span className="block truncate text-danger" title={r.errors.join("\n")}>{r.errors.join("；")}</span> : <span className="text-ink-subtle">—</span>}</td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}
    </Panel>
  );
}
