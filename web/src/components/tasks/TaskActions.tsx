"use client";
import { useState, type FormEvent } from "react";
import { api, isTerminal, type ExecutorRef, type Task, type TaskState, type TransitionAvailability, type WorkflowAvailability } from "@/lib/api";
import { errorMessage } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconCollab, IconComment, IconHand, IconPlay, IconPlus, IconUser } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Dialog, Field, HudCorners, Select, Textarea, Tip, cx } from "@/components/ui";

/*
 * 任务的动作栏与指派对话框：任务详情页（吸顶）和列表 / 看板的抽屉（compact）共用同一份，
 * 这样"现在能做什么"在两处完全一致（DESIGN.md §17「执行简报」①）。
 */
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

export function ActionBar({ task, wf, wfError, stateTitles, onOptimistic, onDone, onAssign, compact = false }: { task: Task; wf: WorkflowAvailability | null; wfError: string | null; stateTitles?: Record<string, { title: string }>; onOptimistic: (s: TaskState | null) => void; onDone: () => void; onAssign: () => void; /** 抽屉里：不吸顶、不带面板外框，右侧只留「指派」 */ compact?: boolean }) {
  const toast = useToast();
  const [pending, setPending] = useState<TransitionAvailability | null>(null);
  const [comment, setComment] = useState("");
  const [result, setResult] = useState("");
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

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
    <div className={compact ? "task-actions-compact" : "hud sticky top-0 z-30 rounded-lg border border-hairline bg-surface-1 px-3 py-2 shadow-panel"} data-task-actions>
      {!compact && <HudCorners />}
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
            <Button variant="ghost" icon={<IconUser />} disabled={busy || terminal} onClick={onAssign}>{t("task.assign")}</Button>
          </Tip>
          {!compact && <a href="#artifacts" className="inline-flex"><Button variant="ghost" icon={<IconPlus />} disabled={terminal} tabIndex={-1}>{t("task.attach")}</Button></a>}
          {!compact && <a href="#comments" className="inline-flex"><Button variant="ghost" icon={<IconComment />} tabIndex={-1}>{t("task.writeComment")}</Button></a>}
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
    </div>
  );
}

/** 指派 = 交接确认框：人与 Agent 之间交接时，标题前的双星 12s 慢速环绕（只在这里出现）。动作栏的「指派」与信息表的「负责人」一行都打开它。 */
export function AssignDialog({ task, open, onClose, executors, onDone }: { task: Task; open: boolean; onClose: () => void; executors: ExecutorRef[]; onDone: () => void }) {
  const toast = useToast();
  const [assignee, setAssignee] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastOpen, setLastOpen] = useState(false);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (open) { setAssignee(task.assignee?.id ?? ""); setError(null); }
  }
  // 交接：当前负责人与选中的执行者一人一机（或从无人到 Agent）
  const picked = executors.find((e) => e.id === assignee);
  const handoff = !!picked && (task.assignee ? task.assignee.kind !== picked.kind : picked.kind === "agent");
  const confirm = async () => {
    setBusy(true); setError(null);
    try { await api.tasks.assign(task.id, assignee || null); toast.ok(t("toast.assigned")); onDone(); onClose(); }
    catch (e) { const m = errorMessage(e); setError(m); toast.fail(m); }
    finally { setBusy(false); }
  };
  return (
    <Dialog open={open} onClose={onClose} title={<span className="inline-flex items-center gap-2"><IconCollab size={20} className={cx("text-ink-subtle", handoff && "orbit-slow")} />{t("task.assignTitle")}</span>} footer={<><Button onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" disabled={busy} onClick={() => void confirm()}>{t("common.confirm")}</Button></>}>
      <Field label={t("task.assignee")} hint={t("task.assignHint")} error={error}>
        <Select value={assignee} onChange={(e) => setAssignee(e.target.value)} autoFocus>
          <option value="">{t("task.assignClear")}</option>
          {executors.map((e) => <option key={e.id} value={e.id}>{e.name}{e.kind === "agent" ? t("common.agentSuffix") : ""}</option>)}
        </Select>
      </Field>
    </Dialog>
  );
}

