"use client";
import Link from "next/link";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { api, isTerminal, type Event, type Proposal, type Relation, type RelationType, type Task, type TaskState, type TransitionAvailability, type WorkflowAvailability } from "@/lib/api";
import { fmtDate, fmtDateTime, fmtDuration, fmtMoney, fmtTokens } from "@/lib/format";
import { errorMessage, useAction, useCapabilityTitles, useExecutors, useLoad, useRouteId } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { prefersReducedMotion } from "@/lib/motion";
import { isAcceptanceWait } from "@/lib/states";
import { artifactTypeTitle, capabilityTitle, RELATIONS, relationTitle, roleTitle, runOutcomeTitle, stateLabel } from "@/lib/terms";
import { useSession } from "@/components/AppShell";
import { PointsChip, PointsChips } from "@/components/board/PointsChips";
import { EventList } from "@/components/EventList";
import { IconApprove, IconBlocks, IconCollab, IconComment, IconExternal, IconHand, IconLink, IconLog, IconPlay, IconPlus, IconRun, IconTrail, IconUsage, IconUser } from "@/components/icons";
import { StateBadge, stateMotion } from "@/components/StateBadge";
import { isOverdue, OverdueTag, PriorityTag, TypeLabel } from "@/components/TaskTable";
import { useToast } from "@/components/toast";
import { Avatar, Button, Checkbox, DescList, DetailSkeleton, Dialog, EnergyLine, ErrorBox, ExecutorName, Field, HudCorners, IdLine, Input, ListSkeleton, Panel, ProgressBar, RelativeTime, Select, Table, Tag, TaskLink, Textarea, Tip, cx } from "@/components/ui";

export function TaskDetail() {
  const id = useRouteId();
  const { session } = useSession();
  const task = useLoad(() => (id ? api.tasks.get(id) : Promise.reject(new Error(t("task.missing")))), [id]);
  const wf = useLoad(() => (id ? api.tasks.workflow(id) : Promise.reject(new Error(t("task.missing")))), [id]);
  const events = useLoad(() => (id ? api.events.list({ task: id, limit: 100 }) : Promise.resolve([])), [id]);
  const typeName = task.data?.type ?? null;
  const type = useLoad(() => (typeName ? api.taskTypes.get(typeName).catch(() => null) : Promise.resolve(null)), [typeName]);
  const caps = useCapabilityTitles();
  // 这个任务上有没有等我确认的动作（ADR 0003）：有就在动作条上方挂一条提示，点进去是筛好的「待确认操作」页
  const proposals = useLoad(() => (id ? api.proposals.list({ status: "pending", mine: 1 }).catch(() => [] as Proposal[]) : Promise.resolve([] as Proposal[])), [id]);
  const myProposals = (proposals.data ?? []).filter((p) => p.target?.kind === "task" && p.target.id === id).length;
  const [optimistic, setOptimistic] = useState<TaskState | null>(null);
  // 工作量：行内点数芯片，点一下即 PATCH（ADR 0012）
  const points = useAction();
  const setPoints = (n: number | null) => {
    const title = task.data?.title ?? "";
    void points.run("points", () => api.tasks.update(id!, { points: n }), n === null ? t("task.pointsCleared", { title }) : t("task.pointsSaved", { title, n })).then((ok) => ok && task.reload());
  };
  const reloadAll = () => { task.reload(); wf.reload(); events.reload(); };
  // 动态轮询（组织范围，显示时只留本任务）拉到「自动解除前置」时重新加载任务：关联列表随后播断链。
  // 这条动态记在被取消的前置任务名下，data.other_id 才是本任务，所以要看组织范围的流。
  const taskReload = task.reload;
  const onNewEvents = (added: Event[]) => {
    if (added.some((e) => e.kind === "RelationRemoved" && (e.task_id === id || e.data.other_id === id))) taskReload();
  };
  // 断链（提案 §三）：上一次渲染还在、这次没了的「前置」关联，保留 600ms 作为幽灵行播链环分开；同时本任务的 LED 亮一次
  const relations = task.data?.relations;
  const [rel, setRel] = useState<{ prev: Relation[] | undefined; ghosts: Relation[] }>({ prev: relations, ghosts: [] });
  if (rel.prev !== relations) {
    const gone = relations && rel.prev ? rel.prev.filter((r) => r.type === "blocks" && !relations.some((x) => x.id === r.id)) : [];
    setRel({ prev: relations, ghosts: gone });
  }
  const ghosts = rel.ghosts;
  useEffect(() => {
    if (!ghosts.length) return;
    const id = window.setTimeout(() => setRel((r) => (r.ghosts.length ? { ...r, ghosts: [] } : r)), 600);
    return () => window.clearTimeout(id);
  }, [ghosts]);
  // 页头那一行：徽标换状态时按新状态类型播签名动效（进入待验收 = 扫描光扫过标题行；进入成功终态 = 压实在徽标里）
  const wfDef = type.data?.workflow;
  const shownName = (optimistic && optimistic.name !== task.data?.state.name ? optimistic : task.data?.state)?.name;
  const [head, setHead] = useState<{ name: string | undefined; motion: "scan" | "lit" | undefined }>({ name: shownName, motion: undefined });
  if (head.name !== shownName) {
    const st = optimistic && optimistic.name !== task.data?.state.name ? optimistic : task.data?.state;
    setHead({ name: shownName, motion: head.name !== undefined && st && stateMotion(st.label, isAcceptanceWait(st, wfDef)) === "scan" ? "scan" : undefined });
  }
  const headMotion = ghosts.length ? "lit" : head.motion;

  if (!id || (task.loading && !task.data)) return <DetailSkeleton />;
  if (task.error || !task.data) return <ErrorBox message={task.error ?? t("task.notFound")} onRetry={task.reload} />;
  const x = task.data;
  const currency = session?.organization.currency;
  const artifactTypes = session?.artifact_types;
  const roles = session ? Object.fromEntries(session.roles.map((r) => [r.name, r.title])) : undefined;
  // 乐观更新：步骤一按下就把徽标切到目标状态；后端确认（任务重新加载后状态一致）或失败回滚。
  const pendingState = optimistic && optimistic.name !== x.state.name ? optimistic : null;
  const shownState = pendingState ?? x.state;
  const accept = isAcceptanceWait(shownState, wfDef);
  const extraInfo: Array<[string, ReactNode]> = [];
  if (x.required_role) extraInfo.push([t("task.requiredRole"), roleTitle(x.required_role, roles)]);
  if (x.required_capabilities.length) extraInfo.push([t("task.requiredCaps"), <span key="caps" className="flex flex-wrap gap-1">{x.required_capabilities.map((c) => <Tag key={c}>{capabilityTitle(c, caps)}</Tag>)}</span>]);
  // 面板序号眉标：01 描述、02 基本信息（右栏顶部，视觉上与描述并列），其余按 DOM 顺序从 03 起递增（参与者面板是条件渲染，所以用计数器）
  let n = 2;
  const idx = () => ++n;

  return (
    <div>
      <div className="mb-1 flex flex-wrap items-center gap-1 text-caption text-ink-subtle">
        <Link href="/tasks/" className="hover:text-accent-hover">{t("task.breadcrumb")}</Link>
        <span>/</span>
        {x.goal && (
          <>
            <Link href={`/goals/${encodeURIComponent(x.goal.id)}/`} className="hover:text-accent-hover">{x.goal.title}</Link>
            <span>/</span>
          </>
        )}
        <span className="telemetry">{x.id}</span>
      </div>
      <div className="mb-3">
        <div className="-mx-2 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md px-2" data-motion={headMotion}>
          <h1 className="text-headline text-ink">{x.title}</h1>
          <span className="inline-flex flex-wrap items-center gap-2">
            {/* 任务类型徽标点开是这类任务的流程（组织设置 · 流程，对所有成员只读可见） */}
            <Tip tip={t("task.viewWorkflow")} placement="bottom">
              <Link href="/settings/?tab=workflows" className="inline-flex rounded-sm hover:opacity-80" aria-label={t("task.viewWorkflow")}><TypeLabel type={x.type} title={x.type_title} tag /></Link>
            </Tip>
            <StateBadge state={shownState} pending={!!pendingState} accept={accept} />
            {x.priority !== "normal" && <PriorityTag priority={x.priority} />}
            {isOverdue(x) && <OverdueTag>{t("task.overdue")}</OverdueTag>}
          </span>
        </div>
        <EnergyLine className="mt-2" />
      </div>

      {myProposals > 0 && (
        <Link href={`/proposals/?target=${encodeURIComponent(x.id)}`} className="mb-3 flex items-center gap-2 rounded-md border border-warning-border bg-warning-bg px-3 py-2 text-body text-warning transition-colors hover:border-warning">
          <IconApprove size={14} className="shrink-0" />
          <span>{t("task.proposals", { n: myProposals })}</span>
          <span className="ml-auto shrink-0 text-caption">{t("task.proposalsGo")} →</span>
        </Link>
      )}

      <ActionBar task={x} wf={wf.data} wfError={wf.error} stateTitles={type.data?.workflow.states} onOptimistic={setOptimistic} onDone={reloadAll} />

      {/* 主栏自适应，侧栏固定 380px（≥1920 时 420px） */}
      <div className="mt-4 grid gap-4 lg:grid-cols-[minmax(0,1fr)_380px] 3xl:grid-cols-[minmax(0,1fr)_420px]">
        <div className="space-y-4">
          <Panel index={1} title={t("task.description")}>
            <p className="whitespace-pre-wrap text-body">{x.description || <span className="text-ink-subtle">{t("task.noDescription")}</span>}</p>
            {Object.keys(x.fields).length > 0 && (
              <div className="mt-3 border-t border-hairline pt-3">
                <DescList items={Object.entries(x.fields).map(([k, v]) => [k, String(v)])} />
              </div>
            )}
          </Panel>

          {Object.keys(x.participants).length > 0 && (
            <Panel index={idx()} icon={<IconCollab />} title={t("task.participants")} padded={false}>
              <Table>
                <thead><tr><th>{t("task.slot")}</th><th>{t("task.requiredRole")}</th><th>{t("task.executor")}</th></tr></thead>
                <tbody>
                  {Object.entries(x.participants).map(([slot, p]) => (
                    <tr key={slot}>
                      <td>{p.title}{x.pending_participant === slot && <Tag tone="warning" className="ml-2">{t("task.pendingSlot")}</Tag>}</td>
                      <td>{roleTitle(p.role, roles)}</td>
                      <td><ExecutorName executor={p.executor} empty={t("task.vacant")} /></td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </Panel>
          )}

          <Relations task={x} onChanged={reloadAll} index={idx()} ghosts={ghosts} />
          <Artifacts task={x} onChanged={reloadAll} artifactTypes={artifactTypes} index={idx()} />
          <Thread task={x} onChanged={reloadAll} index={idx()} />

          <Panel index={idx()} icon={<IconLog />} title={t("task.events")} telemetry={events.data ? t("panel.rows", { n: events.data.length }) : undefined}>
            {events.loading && !events.data ? <ListSkeleton rows={4} /> : <EventList events={events.data ?? []} currentTaskId={x.id} poll={{ limit: 60, filter: (e) => e.task_id === x.id }} onNew={onNewEvents} />}
          </Panel>
        </div>

        <div className="space-y-4">
          <Panel index={2} title={t("task.info")}>
            <DescList
              items={[
                [t("common.id"), <IdLine key="id" id={x.id} />],
                [t("task.assignee"), <ExecutorName key="a" executor={x.assignee} empty={t("task.unclaimed")} />],
                [t("task.reviewer"), <ExecutorName key="r" executor={x.reviewer} />],
                [t("task.creator"), <ExecutorName key="c" executor={x.creator} />],
                [t("task.type"), t("task.typeVersion", { title: x.type_title, version: x.type_version })],
                [t("task.label"), stateLabel(shownState.label)],
                [t("task.progress"), <span key="p" className="flex items-center gap-2"><ProgressBar value={x.progress} tone={x.state.label === "terminal_success" ? "success" : "accent"} className="w-24" /><span className="tabular-nums">{x.progress}%</span></span>],
                [t("task.plan"), <span key="pl" className={cx(isOverdue(x) && "text-danger")}>{fmtDate(x.planned_start)} – {fmtDate(x.planned_end)}</span>],
                [t("task.actual"), x.actual_start ? `${fmtDate(x.actual_start)} – ${x.actual_end ? fmtDate(x.actual_end) : t("common.inProgress")}` : "—"],
                [t("task.estimate"), x.estimate === null ? t("task.estimateUnset") : t("task.hours", { n: x.estimate })],
                [t("task.points"), <span key="pt" className="flex flex-wrap items-center gap-2"><PointsChip value={x.points} /><PointsChips value={x.points} onChange={setPoints} busy={points.busy === "points"} size="sm" /></span>],
                [t("task.sprint"), x.sprint ? <Link key="sp" href={`/sprints/${encodeURIComponent(x.sprint.id)}/`} className="hover:text-accent-hover">{x.sprint.name}</Link> : <span key="sp" className="text-ink-subtle">{t("task.noSprint")}</span>],
                [t("task.cost"), <span key="cost" className="telemetry">{fmtMoney(x.cost, currency)}</span>],
                [t("task.usage"), <span key="u" className="telemetry inline-flex items-center gap-1.5"><IconUsage size={14} className="text-ink-subtle" />{t("task.tokens", { n: fmtTokens(x.total_tokens) })}</span>],
                ...extraInfo,
                [t("task.createdAt"), <RelativeTime key="t" iso={x.created_at} className="telemetry" />],
              ]}
            />
          </Panel>
        </div>
      </div>
      <div className="mt-4">
      <Panel index={idx()} icon={<IconRun />} title={t("task.runs")} telemetry={t("panel.rows", { n: x.runs.length })} padded={false}>
        {x.runs.length === 0 ? (
          <p className="px-4 py-4 text-body text-ink-muted">{t("task.noRuns")}</p>
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-[120px]">{t("task.stage")}</th><th className="w-[160px]">{t("task.executor")}</th><th className="w-[200px]">{t("task.started")}</th><th className="w-[96px]">{t("task.duration")}</th><th className="w-[80px]">{t("task.outcome")}</th><th className="w-full">{t("task.usage")}</th><th className="num w-[110px]">{t("task.cost")}</th>
              </tr>
            </thead>
            <tbody>
              {x.runs.map((r) => (
                <tr key={r.id}>
                  <td className="whitespace-nowrap">{r.state_title}</td>
                  <td className="whitespace-nowrap"><ExecutorName executor={r.executor} /></td>
                  <td className="telemetry whitespace-nowrap">{fmtDateTime(r.started_at)} – {r.ended_at ? fmtDateTime(r.ended_at) : t("common.inProgress")}</td>
                  <td className="telemetry whitespace-nowrap">{fmtDuration(r.started_at, r.ended_at)}</td>
                  <td><Tag tone={r.outcome === "running" ? "accent" : "neutral"} dark={r.outcome === "cancelled"}>{runOutcomeTitle(r.outcome)}</Tag></td>
                  <td>
                    {r.usage.length === 0 ? <span className="text-ink-subtle">—</span> : (
                      <ul className="space-y-0.5 text-caption">
                        {r.usage.map((u) => <li key={u.model} className="telemetry"><span className="text-ink-muted">{u.model}</span> · {t("task.tokens", { n: fmtTokens(u.total_tokens) })} · {t("task.toolCalls", { n: u.tool_calls })}</li>)}
                        <li className="font-mono text-ink-subtle">{t("task.totalTokens", { n: fmtTokens(r.total_tokens) })}</li>
                      </ul>
                    )}
                  </td>
                  <td className="num telemetry whitespace-nowrap">{fmtMoney(r.cost, currency)}</td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr>
                <td colSpan={5}>{t("task.total")}</td>
                <td className="telemetry">{t("task.tokens", { n: fmtTokens(x.total_tokens) })}</td>
                <td className="num telemetry font-medium">{fmtMoney(x.cost, currency)}</td>
              </tr>
            </tfoot>
          </Table>
        )}
      </Panel>
      </div>
    </div>
  );
}

// ---------- 动作栏（吸顶） ----------
type StepKind = "forward" | "secondary" | "danger";
/** 阻塞 / 解除阻塞 / 提问等待：不是向前也不是危险，用默认按钮。 */
const SECONDARY_STEPS = new Set(["block", "unblock", "ask_for_input", "answer", "resume"]);
/** 取消 / 打回 / 测试不通过 / 不修：危险描边。 */
const DANGER_STEPS = new Set(["cancel", "reject", "test_fail", "wont_fix"]);
/** 回退类：重开、标记重复——既不是主动作也不危险。 */
const BACKWARD_STEPS = new Set(["reopen", "duplicate"]);
function stepKind(tr: TransitionAvailability): StepKind | "backward" {
  if (SECONDARY_STEPS.has(tr.name)) return "secondary";
  if (DANGER_STEPS.has(tr.name) || tr.label_to === "terminal_failure") return "danger";
  if (BACKWARD_STEPS.has(tr.name)) return "backward";
  return "forward";
}
/** 每组内能走的在前。回退类步骤跟在次要组后面（默认按钮，不当主动作）。 */
function groupTransitions(transitions: TransitionAvailability[]) {
  const byAvail = (a: TransitionAvailability, b: TransitionAvailability) => Number(b.available) - Number(a.available);
  const forward = transitions.filter((tr) => stepKind(tr) === "forward").sort(byAvail);
  const secondary = [...transitions.filter((tr) => stepKind(tr) === "secondary"), ...transitions.filter((tr) => stepKind(tr) === "backward")].sort(byAvail);
  const danger = transitions.filter((tr) => stepKind(tr) === "danger").sort(byAvail);
  return { forward, secondary, danger };
}

function ActionBar({ task, wf, wfError, stateTitles, onOptimistic, onDone }: { task: Task; wf: WorkflowAvailability | null; wfError: string | null; stateTitles?: Record<string, { title: string }>; onOptimistic: (s: TaskState | null) => void; onDone: () => void }) {
  const toast = useToast();
  const [pending, setPending] = useState<TransitionAvailability | null>(null);
  const [comment, setComment] = useState("");
  const [result, setResult] = useState("");
  const [assigning, setAssigning] = useState(false);
  const [assignee, setAssignee] = useState("");
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const ex = useExecutors();
  // 交接：当前负责人与选中的执行者一人一机（或从无人到 Agent）
  const picked = (ex.data?.executors ?? []).find((e) => e.id === assignee);
  const handoff = !!picked && (task.assignee ? task.assignee.kind !== picked.kind : picked.kind === "agent");

  const run = async (fn: () => Promise<unknown>, okMessage: string, rollback?: () => void) => {
    setBusy(true);
    setFormError(null);
    try {
      await fn();
      toast.ok(okMessage);
      onDone();
      return true;
    } catch (e) {
      rollback?.();
      const m = errorMessage(e);
      setFormError(m);
      toast.fail(rollback ? t("toast.rollback", { reason: m }) : m);
      return false;
    } finally {
      setBusy(false);
    }
  };
  const targetState = (tr: TransitionAvailability): TaskState => ({ name: tr.to, title: stateTitles?.[tr.to]?.title ?? tr.title, label: tr.label_to });
  const fire = (tr: TransitionAvailability) => {
    if (tr.requires.length) { setPending(tr); setComment(""); setResult(""); setFormError(null); return; }
    const target = targetState(tr);
    onOptimistic(target);
    void run(() => api.tasks.transition(task.id, tr.name), t("toast.transitioned", { state: target.title }), () => onOptimistic(null));
  };
  const confirmPending = async (e: FormEvent) => {
    e.preventDefault();
    if (!pending) return;
    const target = targetState(pending);
    onOptimistic(target);
    const ok = await run(() => api.tasks.transition(task.id, pending.name, { comment: comment || undefined, result: pending.requires.includes("result") ? { summary: result } : undefined }), t("toast.transitioned", { state: target.title }), () => onOptimistic(null));
    if (ok) setPending(null);
  };

  const terminal = isTerminal(task.state.label);
  // 分组：向前的步骤 | 阻塞 / 提问等待 | 取消 / 打回。每组内能走的在前，不能走的置灰并用气泡解释原因。
  const groups = groupTransitions(wf?.transitions ?? []);
  const primaryName = groups.forward.find((tr) => tr.available)?.name;
  const renderStep = (tr: TransitionAvailability, kind: StepKind) => (
    <Tip key={tr.name} tip={tr.available ? null : tr.reasons.join("\n")} placement="bottom">
      <Button variant={kind === "danger" ? "danger" : tr.name === primaryName ? "primary" : "default"} disabled={!tr.available || busy} onClick={() => fire(tr)}>
        {tr.title}
      </Button>
    </Tip>
  );
  const divider = <span className="mx-1 h-5 w-px bg-hairline-strong" aria-hidden="true" />;
  const forwardCount = groups.forward.length + (wf ? 2 : 0);

  return (
    <div className="hud sticky top-0 z-30 rounded-lg border border-hairline bg-surface-1 px-3 py-2 shadow-panel">
      <HudCorners />
      <div className="flex flex-wrap items-center gap-2">
        {wfError && <span className="text-body text-danger">{wfError}</span>}
        {!wf && !wfError && <span className="text-caption text-ink-subtle">{t("task.loadingWorkflow")}</span>}
        {groups.forward.map((tr) => renderStep(tr, "forward"))}
        {wf && (
          <>
            <Tip tip={wf.can_claim ? null : wf.claim_reasons.join("\n")} placement="bottom"><Button icon={<IconHand />} disabled={!wf.can_claim || busy} onClick={() => void run(() => api.tasks.claim(task.id), t("toast.claimed"))}>{t("task.claim")}</Button></Tip>
            <Tip tip={wf.can_begin ? null : wf.begin_reasons.join("\n")} placement="bottom"><Button icon={<IconPlay />} disabled={!wf.can_begin || busy} onClick={() => void run(() => api.tasks.begin(task.id), t("toast.begun"))}>{t("task.begin")}</Button></Tip>
          </>
        )}
        {groups.secondary.length > 0 && forwardCount > 0 && divider}
        {groups.secondary.map((tr) => renderStep(tr, "secondary"))}
        {groups.danger.length > 0 && forwardCount + groups.secondary.length > 0 && divider}
        {groups.danger.map((tr) => renderStep(tr, "danger"))}
        {wf && wf.transitions.length === 0 && <span className="text-caption text-ink-subtle">{terminal ? t("task.ended.noNext") : t("task.noTransitions")}</span>}
        <span className="ml-auto inline-flex flex-wrap items-center gap-1">
          <Tip tip={terminal ? t("task.ended.noNext") : null} placement="bottom">
            <Button variant="ghost" icon={<IconUser />} disabled={busy || terminal} onClick={() => { setAssignee(task.assignee?.id ?? ""); setAssigning(true); setFormError(null); }}>{t("task.assign")}</Button>
          </Tip>
          <a href="#artifacts" className="inline-flex"><Button variant="ghost" icon={<IconPlus />} disabled={terminal} tabIndex={-1}>{t("task.attach")}</Button></a>
          <a href="#comments" className="inline-flex"><Button variant="ghost" icon={<IconComment />} tabIndex={-1}>{t("task.writeComment")}</Button></a>
        </span>
      </div>
      {wf?.active_run && (
        <div className="telemetry mt-1.5 text-ink-subtle">{t("task.activeRun", { name: task.runs.find((r) => r.id === wf.active_run?.id)?.executor.name ?? wf.active_run.executor_id, id: wf.active_run.id })}</div>
      )}

      <Dialog open={!!pending} onClose={() => setPending(null)} title={pending?.title ?? ""} footer={<><Button onClick={() => setPending(null)}>{t("common.cancel")}</Button><Button variant="primary" form="transition-form" type="submit" disabled={busy}>{t("common.confirm")}</Button></>}>
        <form id="transition-form" onSubmit={confirmPending} className="space-y-3">
          {pending?.requires.includes("comment") && <Field label={t("task.commentRequired")}><Textarea value={comment} onChange={(e) => setComment(e.target.value)} required autoFocus /></Field>}
          {pending?.requires.includes("result") && <Field label={t("task.resultRequired")}><Textarea value={result} onChange={(e) => setResult(e.target.value)} required /></Field>}
          {formError && <p className="text-caption text-danger" role="alert">{formError}</p>}
        </form>
      </Dialog>

      {/* 指派 = 交接确认框：人与 Agent 之间交接时，标题前的双星 12s 慢速环绕（只在这里出现） */}
      <Dialog open={assigning} onClose={() => setAssigning(false)} title={<span className="inline-flex items-center gap-2"><IconCollab size={20} className={cx("text-ink-subtle", handoff && "orbit-slow")} />{t("task.assignTitle")}</span>} footer={<><Button onClick={() => setAssigning(false)}>{t("common.cancel")}</Button><Button variant="primary" disabled={busy} onClick={async () => { if (await run(() => api.tasks.assign(task.id, assignee || null), t("toast.assigned"))) setAssigning(false); }}>{t("common.confirm")}</Button></>}>
        <Field label={t("task.assignee")} hint={t("task.assignHint")} error={formError}>
          <Select value={assignee} onChange={(e) => setAssignee(e.target.value)} autoFocus>
            <option value="">{t("task.assignClear")}</option>
            {(ex.data?.executors ?? []).map((e) => <option key={e.id} value={e.id}>{e.name}{e.kind === "agent" ? t("common.agentSuffix") : ""}</option>)}
          </Select>
        </Field>
      </Dialog>
    </div>
  );
}

// ---------- 关联 ----------
function Relations({ task, onChanged, index, ghosts }: { task: Task; onChanged: () => void; index?: number; ghosts: Relation[] }) {
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const [type, setType] = useState<RelationType>("blocks");
  const [dir, setDir] = useState<"from" | "to">("from"); // from = 对方在前
  const [other, setOther] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const all = useLoad(() => (open ? api.tasks.list() : Promise.resolve([])), [open]);

  const describe = (r: Task["relations"][number]) => {
    const mine = r.from.id === task.id;
    const o = mine ? r.to : r.from;
    const label = r.type === "blocks" ? (mine ? t("task.rel.successor") : t("task.rel.predecessor")) : r.type === "found_in" ? (mine ? t("task.rel.foundIn") : t("task.rel.bugFound")) : t("task.rel.related");
    return { label, other: o };
  };
  // 关联标签：前置 / 后续 = 链，发现于 = 痕迹，相关不带图标
  const relTag = (r: Relation, label: string) => (
    <Tag tone={r.type === "blocks" ? "warning" : "neutral"}>
      {r.type === "blocks" ? <IconBlocks size={12} className="tag-icon" /> : r.type === "found_in" ? <IconTrail size={12} className="tag-icon" /> : null}
      {label}
    </Tag>
  );
  const rows = [...task.relations.map((r) => ({ r, ghost: false })), ...ghosts.map((r) => ({ r, ghost: true }))];
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      const from = dir === "from" ? other : task.id;
      const to = dir === "from" ? task.id : other;
      await api.tasks.addRelation(task.id, { type, from_task_id: from, to_task_id: to });
      toast.ok(t("toast.linked"));
      setOpen(false); onChanged();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  const dirLabel = type === "blocks" ? [t("task.dir.blocksFrom"), t("task.dir.blocksTo")] : type === "found_in" ? [t("task.dir.foundFrom"), t("task.dir.foundTo")] : [t("task.rel.related"), t("task.rel.related")];

  return (
    <Panel index={index} title={t("task.relations")} actions={<Button size="sm" icon={<IconLink />} onClick={() => setOpen(true)}>{t("task.link")}</Button>}>
      {rows.length === 0 ? <p className="text-body text-ink-subtle">{t("task.noRelations")}</p> : (
        <ul className="space-y-2 text-body">
          {rows.map(({ r, ghost }) => { const d = describe(r); return (
            <li key={r.id} className="flex flex-wrap items-center gap-2" data-motion={ghost ? "unlink" : undefined} aria-hidden={ghost || undefined}>
              {relTag(r, d.label)}
              <TaskLink id={d.other.id} title={d.other.title} inline className="font-medium" />
              <StateBadge state={d.other.state} showLabel={false} />
            </li>
          ); })}
        </ul>
      )}
      <Dialog open={open} onClose={() => setOpen(false)} title={t("task.linkTitle")} footer={<><Button onClick={() => setOpen(false)}>{t("common.cancel")}</Button><Button variant="primary" form="rel-form" type="submit" disabled={busy}>{t("common.confirm")}</Button></>}>
        <form id="rel-form" onSubmit={submit} className="space-y-3">
          <Field label={t("task.relationType")}>
            <Select value={type} onChange={(e) => setType(e.target.value as RelationType)}>
              {RELATIONS.map((k) => <option key={k} value={k}>{relationTitle(k)}</option>)}
            </Select>
          </Field>
          {type !== "related" && (
            <Field label={t("task.direction")}>
              <Select value={dir} onChange={(e) => setDir(e.target.value as "from" | "to")}>
                <option value="from">{dirLabel[0]}</option>
                <option value="to">{dirLabel[1]}</option>
              </Select>
            </Field>
          )}
          <Field label={t("task.otherTask")} hint={t("task.linkHint")} error={error}>
            <Select value={other} onChange={(e) => setOther(e.target.value)} required>
              <option value="">{t("task.pickTask")}</option>
              {(all.data ?? []).filter((o) => o.id !== task.id).map((o) => <option key={o.id} value={o.id}>{o.title} ({o.type_title} · {o.state.title})</option>)}
            </Select>
          </Field>
        </form>
      </Dialog>
    </Panel>
  );
}

// ---------- 交付物 ----------
function Artifacts({ task, onChanged, artifactTypes, index }: { task: Task; onChanged: () => void; artifactTypes?: Record<string, string>; index?: number }) {
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const [type, setType] = useState("");
  const [title, setTitle] = useState("");
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fallback = ["prd", "pr", "test_report", "release_note", "result"];
  const types = artifactTypes ?? Object.fromEntries(fallback.map((k) => [k, artifactTypeTitle(k)]));
  const terminal = isTerminal(task.state.label);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try { await api.tasks.addArtifact(task.id, { type, title, url }); toast.ok(t("toast.attached")); setOpen(false); setTitle(""); setUrl(""); onChanged(); }
    catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <Panel id="artifacts" index={index} title={t("task.artifacts")} actions={<Tip tip={terminal ? t("task.ended.noNext") : null}><Button size="sm" icon={<IconPlus />} onClick={() => { setType(Object.keys(types)[0] ?? ""); setOpen(true); }} disabled={terminal}>{t("task.attach")}</Button></Tip>}>
      {task.artifacts.length === 0 ? <p className="text-body text-ink-subtle">{t("task.noArtifacts")}</p> : (
        <ul className="space-y-2 text-body">
          {task.artifacts.map((a) => (
            <li key={a.id} className="flex flex-wrap items-center gap-2">
              <Tag tone="accent">{artifactTypeTitle(a.type, artifactTypes)}</Tag>
              <a href={a.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 font-medium hover:text-accent-hover">{a.title}<IconExternal className="text-ink-subtle" /></a>
              <span className="text-caption text-ink-subtle">{a.attached_by.name} · <RelativeTime iso={a.created_at} /></span>
            </li>
          ))}
        </ul>
      )}
      <Dialog open={open} onClose={() => setOpen(false)} title={t("task.attach")} footer={<><Button onClick={() => setOpen(false)}>{t("common.cancel")}</Button><Button variant="primary" form="artifact-form" type="submit" disabled={busy}>{t("common.confirm")}</Button></>}>
        <form id="artifact-form" onSubmit={submit} className="space-y-3">
          <Field label={t("task.artifactType")}>
            <Select value={type} onChange={(e) => setType(e.target.value)}>
              {Object.entries(types).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </Select>
          </Field>
          <Field label={t("task.artifactName")}><Input value={title} onChange={(e) => setTitle(e.target.value)} required autoFocus /></Field>
          <Field label={t("task.artifactUrl")} error={error}><Input type="url" value={url} onChange={(e) => setUrl(e.target.value)} required placeholder="https://" /></Field>
        </form>
      </Dialog>
    </Panel>
  );
}

// ---------- 评论 / 工作日志 ----------
function Thread({ task, onChanged, index }: { task: Task; onChanged: () => void; index?: number }) {
  const toast = useToast();
  const [showNotes, setShowNotes] = useState(false);
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const notes = task.comments.filter((c) => c.kind === "note").length;
  const list = task.comments.filter((c) => showNotes || c.kind === "comment");
  // 从「待我处理 · 等我答复」进来时地址带 #comment-<id>：评论是异步取的，浏览器自己滚不到，这里在列表画出来后滚过去并点亮那一条 2.4s（纯 DOM，不进状态）
  const listKey = list.map((c) => c.id).join(",");
  useEffect(() => {
    const m = /^#comment-(.+)$/.exec(window.location.hash);
    if (!m) return;
    const el = document.getElementById(`comment-${decodeURIComponent(m[1])}`);
    if (!el) return;
    el.scrollIntoView({ block: "center", behavior: prefersReducedMotion() ? "auto" : "smooth" });
    el.classList.add("comment-anchored");
    const timer = window.setTimeout(() => el.classList.remove("comment-anchored"), 2400);
    return () => {
      window.clearTimeout(timer);
      el.classList.remove("comment-anchored");
    };
  }, [listKey]);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try { await api.tasks.addComment(task.id, { body }); toast.ok(t("toast.commented")); setBody(""); onChanged(); }
    catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <Panel id="comments" index={index} title={t("task.comments")} telemetry={t("panel.rows", { n: list.length })} actions={notes > 0 && <Checkbox className="text-caption text-ink-muted" checked={showNotes} onChange={(e) => setShowNotes(e.target.checked)} label={t("task.showNotes", { n: notes })} />}>
      {list.length === 0 ? <p className="text-body text-ink-subtle">{t("task.noComments")}</p> : (
        <ul className="space-y-4">
          {list.map((c) => (
            <li key={c.id} id={`comment-${c.id}`} className="comment-row flex gap-3 rounded-md">
              <Avatar name={c.author.name} kind={c.author.kind} size={24} className="mt-0.5" />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2 text-caption text-ink-subtle">
                  <span className="text-ink-muted">{c.author.name}</span>
                  {c.author.kind === "agent" && <Tag tone="accent">Agent</Tag>}
                  {c.kind === "note" && <Tag>{t("task.note")}</Tag>}
                  <RelativeTime iso={c.created_at} />
                </div>
                <p className={cx("mt-0.5 whitespace-pre-wrap text-body", c.kind === "note" && "text-ink-muted")}>{c.body}</p>
              </div>
            </li>
          ))}
        </ul>
      )}
      <form onSubmit={submit} className="mt-4 border-t border-hairline pt-4">
        <Textarea value={body} onChange={(e) => setBody(e.target.value)} placeholder={t("task.commentPlaceholder")} required aria-label={t("task.writeComment")} />
        <div className="mt-2 flex items-center justify-between gap-3">
          <span className="text-caption text-danger">{error}</span>
          <Button type="submit" variant="default" size="sm" disabled={busy}>{t("task.postComment")}</Button>
        </div>
      </form>
    </Panel>
  );
}
