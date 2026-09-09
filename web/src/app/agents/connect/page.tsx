"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { api, type AgentCheck, type DeviceApproveInput, type DeviceGrantChoice, type DeviceRequest, type GrantName } from "@/lib/api";
import { fmtRelative } from "@/lib/format";
import { errorMessage, useCapabilityTitles, useLoad, useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { GRANT_ORDER, GRANT_PRESETS, GRANT_PRESET_MODES, type GrantPreset, agentStateTitle, capabilityTitle, grantPresetTitle, grantTitle, matchGrantPreset } from "@/lib/terms";
import { useSession } from "@/components/AppShell";
import { clientTitle } from "@/components/agents/ConnectWizard";
import { WizardSteps } from "@/components/agents/WizardSteps";
import { IconAgent, IconBacklog, IconCheck } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Checkbox, ErrorBox, Field, FormSection, Input, ListSkeleton, PageHeader, Panel, RelativeTime, Segmented, StatusLED, Tag } from "@/components/ui";

const CHECK_MS = 3_000;
const CHECK_MAX_MS = 120_000;
const CHOICES: DeviceGrantChoice[] = ["allow", "with_approval", "deny"];

/**
 * `/agents/connect/?code=ABCD-1234`（DESIGN.md §21，ADR 0018）：人在网页里批准 Agent 接入。
 * 没有 code 就让人输入；有 code 先 GET /agent-auth/device/{code}：待批准 → 表单（名称、能力标签、每项授权三选一、最多同时任务数、是否公共）；
 * 批准后进「连接检查」：每 3 秒问一次 GET /agents/{id}/check，最多 2 分钟，收到它的第一次请求后给「试领一个任务」。已拒绝 / 已过期 / 找不到都是一句完整的话。
 */
export default function ConnectPage() {
  const router = useRouter();
  const qCode = useQueryParam("code");
  const [typed, setTyped] = useState("");
  const code = qCode?.trim() || null;
  const req = useLoad(() => (code ? api.agentAuth.request(code) : Promise.resolve(null)), [code]);
  const [decided, setDecided] = useState<{ request: DeviceRequest; agentId: string | null } | null>(null);
  const current = decided?.request ?? req.data ?? null;
  const agentId = decided?.agentId ?? current?.agent?.id ?? null;

  const submitCode = (e: FormEvent) => {
    e.preventDefault();
    const c = typed.trim();
    if (!c) return;
    router.push(`/agents/connect/?code=${encodeURIComponent(c)}`);
  };

  return (
    <div className="mx-auto max-w-[760px]">
      <PageHeader title={t("connect.title")} description={t("connect.description")} breadcrumb={<Link href="/agents/" className="hover:text-accent-hover">{t("nav.agents")}</Link>} />
      {!code ? (
        <Panel index={1} title={t("connect.codeField")}>
          <WizardSteps current="approve" className="mb-4" />
          <form onSubmit={submitCode} className="flex flex-wrap items-end gap-2" data-connect-state="input">
            <Field label={t("connect.codeField")} hint={t("connect.codeHint")}>
              <Input value={typed} onChange={(e) => setTyped(e.target.value.toUpperCase())} placeholder="ABCD-1234" autoFocus className="w-48 font-mono" />
            </Field>
            <Button variant="primary" type="submit" disabled={!typed.trim()} className="mb-5">{t("connect.lookup")}</Button>
          </form>
        </Panel>
      ) : req.loading && !current ? (
        <Panel><ListSkeleton rows={4} /></Panel>
      ) : req.error && !current ? (
        <Panel index={1} title={t("connect.title")}>
          <WizardSteps current="approve" className="mb-4" />
          <div data-connect-state="error">
            <ErrorBox message={req.error} onRetry={req.reload} />
            <form onSubmit={submitCode} className="mt-4 flex flex-wrap items-end gap-2">
              <Field label={t("connect.codeField")}>
                <Input value={typed} onChange={(e) => setTyped(e.target.value.toUpperCase())} placeholder="ABCD-1234" className="w-48 font-mono" />
              </Field>
              <Button type="submit" disabled={!typed.trim()} className="mb-5">{t("connect.lookup")}</Button>
            </form>
          </div>
        </Panel>
      ) : current ? (
        current.status === "pending" ? (
          <ApproveForm request={current} onDecided={(r, id) => setDecided({ request: r, agentId: id })} />
        ) : current.status === "approved" ? (
          <CheckStep request={current} agentId={agentId} justNow={!!decided} />
        ) : (
          <Panel index={1} title={current.status_title}>
            <WizardSteps current="approve" className="mb-4" />
            <RequestSummary request={current} />
            <p className="mt-3 text-body text-ink" data-connect-state={current.status}>{current.status === "denied" ? t("connect.deniedText", { who: current.approved_by?.name ?? "" }) : t("connect.expiredText")}</p>
            <p className="mt-1 text-caption text-ink-subtle">{current.status === "denied" ? t("connect.deniedHint") : t("connect.expiredHint")}</p>
            <div className="mt-4 flex gap-2">
              <Link href="/agents/" className="inline-flex"><Button icon={<IconAgent />} tabIndex={-1}>{t("connect.backToAgents")}</Button></Link>
            </div>
          </Panel>
        )
      ) : null}
    </div>
  );
}

/** 申请是什么：运行环境、申请时填的名字、什么时候、什么时候过期 */
function RequestSummary({ request }: { request: DeviceRequest }) {
  return (
    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-caption" data-connect-request={request.user_code}>
      <dt className="text-ink-subtle">{t("connect.codeField")}</dt><dd className="telemetry">{request.user_code}</dd>
      <dt className="text-ink-subtle">{t("agents.runtime")}</dt><dd className="text-ink">{request.client_title || clientTitle(request.client)}</dd>
      <dt className="text-ink-subtle">{t("connect.requestedName")}</dt><dd className="text-ink">{request.name || <span className="text-ink-subtle">{t("connect.noName")}</span>}</dd>
      <dt className="text-ink-subtle">{t("connect.requestedAt")}</dt><dd className="text-ink"><RelativeTime iso={request.created_at} /></dd>
      {request.status === "pending" && <><dt className="text-ink-subtle">{t("connect.expiresAt")}</dt><dd className="text-ink">{fmtRelative(request.expires_at)}</dd></>}
    </dl>
  );
}

function ApproveForm({ request, onDecided }: { request: DeviceRequest; onDecided: (r: DeviceRequest, agentId: string | null) => void }) {
  const { session } = useSession();
  const toast = useToast();
  const capTitles = useCapabilityTitles();
  const [name, setName] = useState(request.name || "");
  const [caps, setCaps] = useState<string[]>([]);
  // 默认：执行 = 需要人确认，评论 = 允许，其余不给；「管理流程」对 Agent 只能是需要人确认
  const [grants, setGrants] = useState<Record<GrantName, DeviceGrantChoice>>(() => Object.fromEntries(GRANT_ORDER.map((g) => [g, g === "execute" ? "with_approval" : g === "comment" ? "allow" : "deny"])) as Record<GrantName, DeviceGrantChoice>);
  const [max, setMax] = useState("1");
  const [shared, setShared] = useState(false);
  const [busy, setBusy] = useState<"approve" | "deny" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const isOwner = !!session?.is_owner;
  const choiceTitle = (c: DeviceGrantChoice) => t(`connect.choice.${c}`);

  const approve = async (e: FormEvent) => {
    e.preventDefault();
    setBusy("approve"); setError(null);
    try {
      const input: DeviceApproveInput = { name: name.trim() || undefined, capabilities: caps, grants, max_concurrency: Math.max(1, Number(max) || 1), shared: isOwner ? shared : undefined };
      const r = await api.agentAuth.approve(request.user_code, input);
      toast.ok(t("connect.approved", { name: r.agent.name }));
      onDecided(r.request, r.agent.id);
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(null); }
  };
  const deny = async () => {
    setBusy("deny"); setError(null);
    try {
      const r = await api.agentAuth.deny(request.user_code);
      toast.ok(t("connect.deniedToast"));
      onDecided(r, null);
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(null); }
  };

  return (
    <Panel index={1} title={t("connect.step.approve")} telemetry={request.status_title}>
      <WizardSteps current="approve" className="mb-4" />
      <RequestSummary request={request} />
      <form onSubmit={approve} className="mt-4 space-y-4" data-connect-state="pending">
        <FormSection title={t("form.basic")}>
          <Field label={t("connect.agentName")} hint={t("connect.agentNameHint", { fallback: request.name || (request.client_title || clientTitle(request.client)) })}>
            <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={80} autoFocus className="w-full max-w-[400px]" />
          </Field>
          <div className="grid max-w-[400px] grid-cols-2 gap-3">
            <Field label={t("agents.maxConcurrencyField")}><Input type="number" min={1} value={max} onChange={(e) => setMax(e.target.value)} /></Field>
          </div>
          {isOwner && <Checkbox checked={shared} onChange={(e) => setShared(e.target.checked)} label={t("agents.sharedField")} />}
        </FormSection>
        <FormSection title={t("agents.capabilities")}>
          <div className="flex flex-wrap gap-x-4 gap-y-1.5">
            {(session?.capabilities ?? []).length === 0 && <span className="text-body text-ink-subtle">{t("agents.noCapVocab")}</span>}
            {(session?.capabilities ?? []).map((c) => (
              <Checkbox key={c} checked={caps.includes(c)} onChange={(e) => setCaps(e.target.checked ? [...caps, c] : caps.filter((x) => x !== c))} label={capabilityTitle(c, capTitles)} />
            ))}
          </div>
        </FormSection>
        <FormSection title={t("form.access")}>
          <p className="text-caption text-ink-muted">{t("connect.grantsHint")}</p>
          {/* 授权预设（DESIGN.md §21）：按角色一键填好，再逐项微调；正好等于某个预设时高亮它，否则显示「自定义」 */}
          <div className="flex flex-wrap items-center gap-2" data-grant-presets="">
            <span className="text-caption text-ink-subtle">{t("connect.presetLabel")}</span>
            <Segmented
              size="sm"
              value={matchGrantPreset(grants) ?? "custom"}
              onChange={(p) => { if (p !== "custom") setGrants(Object.fromEntries(GRANT_ORDER.map((g) => [g, GRANT_PRESET_MODES[p][g] ?? "deny"])) as Record<GrantName, DeviceGrantChoice>); }}
              options={[...GRANT_PRESETS.map((p) => [p, grantPresetTitle(p)] as [GrantPreset | "custom", string]), ["custom", t("connect.presetCustom")]]}
              aria-label={t("connect.presetLabel")}
            />
          </div>
          <p className="text-caption text-ink-subtle">{t("connect.presetHint")}</p>
          <div className="space-y-1.5" data-grants="">
            {GRANT_ORDER.map((g) => {
              const options = (g === "manage_workflows" ? CHOICES.filter((c) => c !== "allow") : CHOICES).map((c) => [c, choiceTitle(c)] as [DeviceGrantChoice, string]);
              return (
                <div key={g} className="flex flex-wrap items-center justify-between gap-2 text-body" data-grant={g}>
                  <span>{grantTitle(g)}</span>
                  <Segmented size="sm" value={grants[g]} onChange={(v) => setGrants({ ...grants, [g]: v })} options={options} aria-label={grantTitle(g)} />
                </div>
              );
            })}
          </div>
          <p className="text-caption text-ink-subtle">{t("agents.workflowGrantHint")}</p>
        </FormSection>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
        <div className="flex flex-wrap items-center gap-2 border-t border-hairline pt-4">
          <Button variant="primary" type="submit" icon={<IconCheck />} disabled={!!busy}>{busy === "approve" ? t("common.saving") : t("connect.approve")}</Button>
          <Button variant="ghost" className="text-danger hover:!text-danger" disabled={!!busy} onClick={() => void deny()}>{t("connect.deny")}</Button>
          <span className="ml-auto text-caption text-ink-subtle">{t("connect.ownerNote", { name: session?.member.name ?? "" })}</span>
        </div>
      </form>
    </Panel>
  );
}

/** 第四步：连接检查。每 3 秒问一次，最多 2 分钟；收到过它的请求（且没有被停用）就停下来给第五步。
    MCP 的 Agent 平时不说话，所以判断的是「有没有说过话」，不是「此刻在不在线」（CONTEXT.md「Agent 状态」）。 */
function CheckStep({ request, agentId, justNow }: { request: DeviceRequest; agentId: string | null; justNow: boolean }) {
  const [check, setCheck] = useState<AgentCheck | null>(null);
  const [error, setError] = useState<string | null>(null);
  // 超时记的是"第几轮"超时了：再检查一次（tick+1）自然清掉
  const [timedOut, setTimedOut] = useState<number | null>(null);
  const [tick, setTick] = useState(0);
  const done = !!check?.connected && check.state !== "inactive";
  const isTimedOut = timedOut === tick;
  useEffect(() => {
    if (!agentId || done) return;
    let alive = true;
    const started = Date.now();
    const ask = async () => {
      try {
        const c = await api.agents.check(agentId);
        if (!alive) return;
        setCheck(c);
        setError(null);
        if (c.connected && c.state !== "inactive") return;
      } catch (e) {
        if (alive) setError(errorMessage(e));
      }
      if (!alive) return;
      if (Date.now() - started >= CHECK_MAX_MS) { setTimedOut(tick); return; }
      timer = window.setTimeout(ask, CHECK_MS);
    };
    let timer = window.setTimeout(ask, 0);
    return () => { alive = false; window.clearTimeout(timer); };
  }, [agentId, done, tick]);
  const tone = !check?.connected ? "neutral" : check.state === "running" ? "accent" : check.state === "ready" ? "online" : "dark";
  return (
    <Panel index={1} title={done ? t("connect.step.try") : t("connect.step.check")} telemetry={request.agent?.name ?? undefined}>
      <WizardSteps current={done ? "try" : "check"} className="mb-4" />
      <div data-connect-state={done ? "connected" : "checking"} className="space-y-3">
        {justNow && <p className="text-body text-ink">{t("connect.approvedText", { name: request.agent?.name ?? "" })}</p>}
        {!justNow && <p className="text-caption text-ink-subtle">{t("connect.alreadyApproved", { who: request.approved_by?.name ?? "", name: request.agent?.name ?? "" })}</p>}
        <div className="flex items-start gap-2 rounded-md border border-hairline bg-surface-1 px-3 py-2.5">
          <StatusLED tone={tone} className="mt-1.5" />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2 text-body text-ink">
              <span>{check?.connected ? agentStateTitle(check.state) : t("connect.waitingFirst")}</span>
              {check?.last_tool && <Tag>{t("connect.lastTool", { tool: check.last_tool })}</Tag>}
              {!done && !isTimedOut && <span className="eyebrow text-ink-tertiary">{t("connect.polling")}</span>}
            </div>
            <p className="mt-0.5 text-caption text-ink-subtle" data-check-hint="">{error ?? check?.hint ?? t("connect.checkStarting")}</p>
            {isTimedOut && !done && (
              <p className="mt-1 text-caption text-warning">
                {t("connect.checkTimeout")} <button type="button" className="underline hover:text-ink" onClick={() => setTick((n) => n + 1)}>{t("connect.checkAgain")}</button>
              </p>
            )}
          </div>
        </div>
        {done ? (
          <div className="space-y-2">
            <p className="text-caption text-ink-muted">{t("connect.tryHint")}</p>
            <div className="flex flex-wrap gap-2">
              <Link href="/tasks/?view=backlog" className="inline-flex"><Button variant="primary" icon={<IconBacklog />} tabIndex={-1}>{t("connect.tryTask")}</Button></Link>
              <Link href="/agents/" className="inline-flex"><Button icon={<IconAgent />} tabIndex={-1}>{t("connect.backToAgents")}</Button></Link>
            </div>
          </div>
        ) : (
          <div className="flex flex-wrap gap-2">
            <Link href="/agents/" className="inline-flex"><Button variant="ghost" icon={<IconAgent />} tabIndex={-1}>{t("connect.backToAgents")}</Button></Link>
          </div>
        )}
      </div>
    </Panel>
  );
}
