"use client";
import { useEffect, useState, type FormEvent } from "react";
import { api, type Agent, type AgentStat, type Event, type Grant, type GrantMode, type GrantName, type Task } from "@/lib/api";
import { fmtMoney, fmtRelative, parseDate, today } from "@/lib/format";
import { errorMessage, useAction, useCapabilityTitles, useHighlight, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { isAcceptanceWait, useTaskTypeIndex } from "@/lib/states";
import { capabilityTitle, GRANT_ORDER, grantModeTitle, grantTitle } from "@/lib/terms";
import { useSession } from "@/components/AppShell";
import { AgentVisor, type VisorState } from "@/components/AgentVisor";
import { IconAgent, IconApprove, IconEdit, IconKey, IconPlus, IconTrash } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Avatar, Button, Checkbox, ConfirmDialog, CopyLine, Dialog, Drawer, Empty, ErrorBox, Field, FormSection, Input, PageHeader, Panel, RelativeTime, Select, StatChips, StatusLED, Table, TableSkeleton, Tag, TagList, Tip, cx } from "@/components/ui";

const POLL_MS = 30_000;

/** 最近心跳：一分钟内按秒说，之后沿用相对时间。 */
function heartbeat(iso: string | null): string {
  const d = parseDate(iso);
  if (!d) return t("agents.never");
  const sec = Math.max(0, Math.round((Date.now() - d.getTime()) / 1000));
  return sec < 60 ? t("time.secondsAgo", { n: sec }) : fmtRelative(iso);
}

/**
 * 每行下方的等宽遥测（舰内系统 v3 §6）：`最近心跳 12 秒前 · 今日 3 段 · ¥4.17`。
 * 今日段数 = 今天由这个 Agent 发起的「开始执行」动态条数（GET /events）；成本 = GET /stats/agents 的累计成本。
 */
function AgentTelemetry({ agent, stat, todayRuns, currency }: { agent: Agent; stat: AgentStat | undefined; todayRuns: number; currency?: string }) {
  return (
    <span className="eyebrow mt-1 flex items-center gap-1.5 normal-case text-ink-subtle">
      <span>{t("agents.lastHeartbeat")}</span>
      <span className="text-telemetry">{heartbeat(agent.last_seen_at)}</span>
      <span className="text-hairline-tertiary" aria-hidden="true">·</span>
      <span>{t("agents.todaySegments", { n: todayRuns })}</span>
      <span className="text-hairline-tertiary" aria-hidden="true">·</span>
      <span>{t("agents.costTotal")}</span>
      <span className="text-telemetry">{stat ? fmtMoney(stat.cost, currency) : "—"}</span>
    </span>
  );
}

export default function AgentsPage() {
  const { session } = useSession();
  const agents = useLoad(() => api.agents.list(), []);
  const stats = useLoad(() => api.stats.agents().catch(() => [] as AgentStat[]), []);
  const events = useLoad(() => api.events.list({ limit: 300 }).catch(() => [] as Event[]), []);
  // 状态语言要看"有没有打开的执行记录 / 有没有等人回答的提问"：
  // 打开的执行记录从动态推（RunStarted 的 run_id 没有对应的 RunEnded；任务列表接口不带执行记录）；等待输入 = 任务等待中且不是待验收
  const tasks = useLoad(() => api.tasks.list().catch(() => [] as Task[]), []);
  const types = useTaskTypeIndex();
  const caps = useCapabilityTitles();
  // 心跳 30s 一拍：标签页可见时重拉 Agent 与任务，上线 / 离线是真实变化才会播充电 / 变灰
  const reloadAgents = agents.reload;
  const reloadTasks = tasks.reload;
  const reloadEvents = events.reload;
  useEffect(() => {
    const id = window.setInterval(() => {
      if (document.hidden) return;
      reloadAgents();
      reloadTasks();
      reloadEvents();
    }, POLL_MS);
    return () => window.clearInterval(id);
  }, [reloadAgents, reloadTasks, reloadEvents]);
  const openRuns = (id: string) => {
    const ended = new Set((events.data ?? []).filter((e) => e.kind === "RunEnded").map((e) => String(e.data.run_id ?? "")));
    return (events.data ?? []).some((e) => e.kind === "RunStarted" && e.actor?.id === id && !ended.has(String(e.data.run_id ?? "")));
  };
  // 上线 / 离线的变化（渲染期比较上一次的在线快照），动效 800ms 后清掉
  const onlineSig = (agents.data ?? []).map((a) => `${a.id}:${a.online ? 1 : 0}`).join("\n");
  const [snap, setSnap] = useState<{ sig: string; online: Map<string, boolean>; motions: Record<string, "charge" | "dim"> }>(() => ({ sig: onlineSig, online: new Map((agents.data ?? []).map((a) => [a.id, a.online])), motions: {} }));
  if (snap.sig !== onlineSig) {
    const online = new Map<string, boolean>();
    const motions: Record<string, "charge" | "dim"> = {};
    for (const a of agents.data ?? []) {
      const prev = snap.online.get(a.id);
      if (prev !== undefined && prev !== a.online) motions[a.id] = a.online ? "charge" : "dim";
      online.set(a.id, a.online);
    }
    setSnap({ sig: onlineSig, online, motions });
  }
  const hasMotion = Object.keys(snap.motions).length > 0;
  useEffect(() => {
    if (!hasMotion) return;
    const id = window.setTimeout(() => setSnap((s) => (Object.keys(s.motions).length ? { ...s, motions: {} } : s)), 800);
    return () => window.clearTimeout(id);
  }, [hasMotion, snap.sig]);
  const visorState = (a: Agent): VisorState => {
    if (!a.online) return "offline";
    const rows = tasks.data ?? [];
    if (openRuns(a.id)) return "busy";
    if (rows.some((x) => x.assignee?.id === a.id && x.state.label === "waiting" && !isAcceptanceWait(x.state, types[x.type]?.workflow))) return "waiting";
    return "online";
  };
  const currency = session?.organization.currency;
  const statOf = (id: string) => stats.data?.find((s) => s.agent.id === id);
  const dayStart = today().getTime();
  const todayRuns = (id: string) => events.data?.filter((e) => e.kind === "RunStarted" && e.actor?.id === id && new Date(e.created_at).getTime() >= dayStart).length ?? 0;
  const [registering, setRegistering] = useState(false);
  const [token, setToken] = useState<{ agent: Agent; token: string } | null>(null);
  const [removing, setRemoving] = useState<Agent | null>(null);
  const [highlight, mark] = useHighlight();
  const { busy, run } = useAction();
  const online = agents.data?.filter((a) => a.online).length ?? 0;
  // 可见范围（DESIGN.md §14）：组织负责人 / 持「组织设置」权限的人看到全部并每行显示所有者；
  // 其他人只看到自己的加公共 Agent。列表里出现 can_manage = false 的行就一定不是"全部"。
  const isAdmin = !!session?.is_owner || !!session?.permissions?.includes("org_settings");
  const seesAll = (agents.data ?? []).some((a) => !a.can_manage) ? false : isAdmin;

  const remove = async (a: Agent) => {
    if (await run(a.id, () => api.agents.remove(a.id), t("toast.removed", { name: a.name }))) {
      setRemoving(null);
      agents.reload();
    }
  };

  return (
    <div>
      <PageHeader title={t("agents.title")} description={seesAll ? t("agents.descriptionAll") : t("agents.descriptionMine")} actions={<Button variant="primary" icon={<IconPlus />} onClick={() => setRegistering(true)}>{t("agents.register")}</Button>} />
      <StatChips
        className="mb-4"
        value={null}
        total={agents.data?.length ?? 0}
        items={[{ key: "online", label: t("agents.online"), value: online, tone: "accent" }]}
      />
      <Panel index={1} icon={<IconAgent />} title={t("nav.agents")} telemetry={agents.data ? t("panel.rows", { n: agents.data.length }) : undefined} padded={false}>
        {agents.loading ? <TableSkeleton rows={4} cols={6} /> : agents.error ? <div className="p-4"><ErrorBox message={agents.error} onRetry={agents.reload} /></div> : !agents.data?.length ? (
          <Empty text={t("agents.empty")} action={<Button variant="primary" icon={<IconPlus />} onClick={() => setRegistering(true)}>{t("agents.register")}</Button>} />
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-full">Agent</th><th className="w-[140px]">{t("agents.status")}</th>{seesAll && <th className="w-[110px]">{t("agents.owner")}</th>}<th className="w-[200px]">{t("agents.capabilities")}</th><th className="w-[100px]">{t("agents.grants")}</th><th className="num w-[88px]">{t("agents.maxAtOnce")}</th><th className="actions w-[236px]" aria-label={t("common.actions")} />
              </tr>
            </thead>
            <tbody>
              {agents.data.map((a) => {
                const grantLines = GRANT_ORDER.filter((g) => a.grants.some((x) => x.name === g)).map((g) => `${grantTitle(g)} · ${grantModeTitle(a.grants.find((x) => x.name === g)!.mode)}`);
                // 含「需要人确认」的授权：气泡里多一句说明这些动作会先变成待确认操作（ADR 0003）
                const hasApproval = a.grants.some((x) => x.mode === "with_approval");
                return (
                  <tr key={a.id} className={cx(highlight === a.id && "row-new")}>
                    <td className="w-full max-w-0 min-w-[220px]">
                      <span className="flex items-center gap-2.5">
                        <AgentVisor state={visorState(a)} size={24} motion={snap.motions[a.id]} />
                        <span className="flex min-w-0 flex-col">
                          <span className="flex min-w-0 items-center gap-2">
                            <span className="min-w-0 truncate font-medium" title={a.name}>{a.name}</span>
                            {a.shared && <Tip tip={t("agents.sharedManaged")}><Tag tone="accent">{t("agents.sharedTag")}</Tag></Tip>}
                          </span>
                          <AgentTelemetry agent={a} stat={statOf(a.id)} todayRuns={todayRuns(a.id)} currency={currency} />
                        </span>
                      </span>
                    </td>
                    <td className="whitespace-nowrap">
                      <span className="inline-flex items-center gap-1.5">
                        {/* 在线 = success 灯常亮微光；离线 = 暗灯 */}
                        <StatusLED tone={a.online ? "online" : "dark"} />
                        <span>{a.online ? t("agents.online") : t("agents.offlineShort")}</span>
                        {!a.online && <span className="text-caption text-ink-subtle">· {a.last_seen_at ? <RelativeTime iso={a.last_seen_at} /> : t("agents.never")}</span>}
                      </span>
                    </td>
                    {seesAll && <td className="max-w-[140px] whitespace-nowrap"><span className="flex items-center gap-1.5"><Avatar name={a.owner.name} size={20} /><span className="truncate">{a.owner.name}</span></span></td>}
                    <td className="whitespace-nowrap"><TagList items={a.capabilities.map((c) => capabilityTitle(c, caps))} empty={t("agents.noCaps")} /></td>
                    <td className="whitespace-nowrap">
                      {a.grants.length === 0 ? <span className="text-ink-subtle">—</span> : (
                        <Tip placement="bottom" tip={hasApproval ? `${grantLines.join("\n")}\n\n${t("agents.withApprovalHint")}` : grantLines.join("\n")}>
                          {/* 含「需确认」的授权：舵轮图标；悬停 / 聚焦预览"转舵"（审批界面建成后由批准动作触发） */}
                          {hasApproval ? (
                            <Tag tone="warning" className="helm-turn">
                              <IconApprove size={12} className="tag-icon" />
                              {t("agents.grantCount", { n: a.grants.length })}
                            </Tag>
                          ) : (
                            <Tag>{t("agents.grantCount", { n: a.grants.length })}</Tag>
                          )}
                        </Tip>
                      )}
                    </td>
                    <td className="num whitespace-nowrap">{t("agents.countUnit", { n: a.max_concurrency })}</td>
                    <td className="actions">
                      {/* 别人的公共 Agent 只读：没有编辑 / 移除 / 令牌（ADR 0017 同期的可见范围规则） */}
                      {a.can_manage && <span className="row-actions">
                        <Tip tip={t("agents.noTokenStored")}><Button size="sm" variant="ghost" icon={<IconKey />} disabled>{t("agents.viewToken")}</Button></Tip>
                        <Tip tip={t("agents.editUnavailable")}><Button size="sm" variant="ghost" icon={<IconEdit />} disabled>{t("common.edit")}</Button></Tip>
                        <Button size="sm" variant="ghost" className="text-danger hover:!text-danger" icon={<IconTrash />} disabled={busy === a.id} onClick={() => setRemoving(a)}>{t("agents.remove")}</Button>
                      </span>}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        )}
      </Panel>
      <RegisterDrawer open={registering} capabilities={session?.capabilities ?? []} capTitles={caps} onClose={() => setRegistering(false)} onCreated={(r) => { setToken(r); mark(r.agent.id); agents.reload(); }} />
      <Dialog open={!!token} onClose={() => setToken(null)} title={t("agents.registered")} footer={<Button variant="primary" onClick={() => setToken(null)}>{t("agents.saved")}</Button>}>
        <p className="text-ink-muted">{t("agents.tokenHint", { name: token?.agent.name })}</p>
        <CopyLine text={token?.token ?? ""} />
      </Dialog>
      <ConfirmDialog open={!!removing} title={t("agents.remove")} message={removing ? t("agents.removeConfirm", { name: removing.name }) : null} confirmLabel={t("agents.remove")} danger busy={!!busy} onConfirm={() => removing && void remove(removing)} onClose={() => setRemoving(null)} />
    </div>
  );
}

function RegisterDrawer({ open, capabilities, capTitles, onClose, onCreated }: { open: boolean; capabilities: string[]; capTitles: Record<string, string>; onClose: () => void; onCreated: (r: { agent: Agent; token: string }) => void }) {
  const toast = useToast();
  const [name, setName] = useState("");
  const [shared, setShared] = useState(false);
  const [caps, setCaps] = useState<string[]>([]);
  const [modes, setModes] = useState<Record<string, GrantMode | "">>({ execute: "direct", comment: "direct" });
  const [max, setMax] = useState("1");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      const grants: Grant[] = GRANT_ORDER.filter((g) => modes[g]).map((g) => ({ name: g, mode: modes[g] as GrantMode }));
      const r = await api.agents.create({ name, shared, capabilities: caps, grants, max_concurrency: Number(max) || 1 });
      toast.ok(t("toast.registered", { name: r.agent.name }));
      onClose();
      setName(""); setCaps([]); setModes({ execute: "direct", comment: "direct" }); setShared(false); setMax("1");
      onCreated(r);
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };

  return (
    <Drawer open={open} onClose={onClose} title={t("agents.register")} description={t("agents.registerHint")} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="agent-form" type="submit" disabled={busy}>{t("agents.registerAndToken")}</Button></>}>
      <form id="agent-form" onSubmit={submit} className="space-y-4">
        <FormSection title={t("form.basic")}>
          <Field label={t("common.name")}><Input value={name} onChange={(e) => setName(e.target.value)} required autoFocus /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("agents.maxConcurrencyField")}><Input type="number" min={1} value={max} onChange={(e) => setMax(e.target.value)} /></Field>
          </div>
          <Checkbox checked={shared} onChange={(e) => setShared(e.target.checked)} label={t("agents.sharedField")} />
        </FormSection>
        <FormSection title={t("agents.capabilities")}>
          <div className="flex flex-wrap gap-x-4 gap-y-1.5">
            {capabilities.length === 0 && <span className="text-body text-ink-subtle">{t("agents.noCapVocab")}</span>}
            {capabilities.map((c) => (
              <Checkbox key={c} checked={caps.includes(c)} onChange={(e) => setCaps(e.target.checked ? [...caps, c] : caps.filter((x) => x !== c))} label={capabilityTitle(c, capTitles)} />
            ))}
          </div>
        </FormSection>
        <FormSection title={t("form.access")}>
          <p className="text-caption text-ink-muted">{t("agents.grantsField")}</p>
          <div className="space-y-1.5">
            {GRANT_ORDER.map((g: GrantName) => (
              <label key={g} className="flex items-center justify-between gap-3 text-body">
                <span>{grantTitle(g)}</span>
                <Select value={modes[g] ?? ""} onChange={(e) => setModes({ ...modes, [g]: e.target.value as GrantMode | "" })} className="w-40">
                  <option value="">{t("agents.noGrant")}</option>
                  <option value="direct">{grantModeTitle("direct")}</option>
                  <option value="with_approval">{grantModeTitle("with_approval")}</option>
                </Select>
              </label>
            ))}
          </div>
          <p className="text-caption text-ink-subtle">{t("agents.workflowGrantHint")}</p>
        </FormSection>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
