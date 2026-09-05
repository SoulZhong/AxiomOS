"use client";
import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";
import { api, type ExecutorRef, type Goal, type Priority, type Proposal, type ProposalStatus, type ProposalTarget, type Sprint, type Task, type TaskType } from "@/lib/api";
import { fmtDate, fmtDateTime, parseDate } from "@/lib/format";
import { errorMessage, useCapabilityTitles, useExecutors, useLoad, useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { prefersReducedMotion } from "@/lib/motion";
import { useTaskTypeIndex } from "@/lib/states";
import { capabilityTitle, grantTitle, PROPOSAL_STATUSES, priorityTitle, proposalFieldTitle, proposalStatusOf, proposalStatusTitle, proposalTargetHref, proposalTargetKindTitle } from "@/lib/terms";
import { usePersisted } from "@/lib/usePersisted";
import { useSession } from "@/components/AppShell";
import { IconApprove, IconChevronDown, IconChevronRight, IconClose } from "@/components/icons";
import { refreshShipTelemetry } from "@/components/ship-status/telemetry";
import { StateBadge } from "@/components/StateBadge";
import { useToast } from "@/components/toast";
import { Avatar, Button, Checkbox, Code, DescList, Dialog, Empty, ErrorBox, ExecutorName, Field, ListSkeleton, PageHeader, Panel, ProgressBar, RelativeTime, StatChips, Tag, Textarea, Tip, cx } from "@/components/ui";

/*
 * 待确认操作（CONTEXT.md「待确认操作」/ ADR 0003）：Agent 的授权是「需要人确认」时，它发起的动作先记成一条，
 * 等人点确认才执行。这一页就是人处理它们的地方：
 * - 芯片条按状态过滤（默认「待确认」），右侧「只看等我确认」对应接口的 mine=1，默认开。
 * - 每一行给足决策要用的东西：谁发起的（Agent 视窗头像 + 名字 + 所有者）、要做什么、针对谁、确认后会发生什么（后端给的完整句子）、
 *   什么时候发起的、还有多久过期。展开一行能看到"确认后按这些内容执行"（payload 按人话排版，不是一坨 JSON）与对象现在的状态。
 * - 「确认」是主按钮：先播 240ms 的转舵（DESIGN.md §7 的 .helm-turn / IconApprove），再乐观地把这一行标成已确认并弹 Toast；
 *   失败时回滚并把后端那句完整的失败理由原样显示。「拒绝」要求写一句完整的理由，Agent 会读它。
 */

/** 转舵 240ms（globals.css F.），与乐观更新错开：动效放完再翻牌，reduced-motion 时立即翻。 */
const HELM_MS = 240;
/** 倒计时每分钟自己往前走一格，不用等 30 秒的遥测。 */
const TICK_MS = 60_000;
/** 6 小时内到期的用警示色催一下。 */
const SOON_MS = 6 * 3600_000;
const MIN_REASON = 6;

export default function ProposalsPage() {
  const { session } = useSession();
  const toast = useToast();
  const targetFilter = useQueryParam("target");
  const [mine, setMine] = usePersisted("axiomos.proposals.mine", true);
  const [status, setStatus] = useState<ProposalStatus | null>("pending");
  const list = useLoad(() => api.proposals.list(mine ? { mine: 1 } : {}), [mine]);
  const types = useTaskTypeIndex();
  const caps = useCapabilityTitles();
  // payload 里的执行者 id 换成名字（人和 Agent 都在这一份里）
  const executors = useExecutors().data?.executors ?? [];
  // 乐观更新：确认 / 拒绝之后先用本地这一份盖住，请求回来再换成后端返回的那一条；失败就撤掉
  const [edited, setEdited] = useState<Record<string, Proposal>>({});
  const [helm, setHelm] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [rejecting, setRejecting] = useState<Proposal | null>(null);
  // 刚处理过的那几条先留在原位（让人看见结果），换一次筛选就按新条件重新排
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [recent, setRecent] = useState<string[]>([]);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(id);
  }, []);

  const all = (list.data ?? []).map((p) => edited[p.id] ?? p);
  const scoped = targetFilter ? all.filter((p) => p.target?.id === targetFilter) : all;
  const counts = Object.fromEntries(PROPOSAL_STATUSES.map((s) => [s, scoped.filter((p) => proposalStatusOf(p, now) === s).length])) as Record<ProposalStatus, number>;
  const rows = status ? scoped.filter((p) => proposalStatusOf(p, now) === status || recent.includes(p.id)) : scoped;
  const targetTitle = targetFilter ? scoped[0]?.target?.title ?? targetFilter : null;

  const put = (p: Proposal) => {
    setEdited((m) => ({ ...m, [p.id]: p }));
    setRecent((xs) => (xs.includes(p.id) ? xs : [...xs, p.id]));
  };
  const drop = (id: string) => {
    setEdited((m) => { const next = { ...m }; delete next[id]; return next; });
    setRecent((xs) => xs.filter((x) => x !== id));
  };

  /**
   * 确认：转舵 240ms → 乐观翻牌 → 后端回来后换成真结果并刷新侧栏 / 状态栏的条数。
   * 后端比动效快时也要把这 240ms 放完（否则舵轮刚转一半这一行就变了）；失败时回滚，舵轮自己转回原位。
   */
  const approve = async (p: Proposal) => {
    if (busy) return;
    setBusy(p.id);
    setHelm(p.id);
    const wait = prefersReducedMotion() ? 0 : HELM_MS;
    // 这枚定时器就是"动效放完了"的信号：后端比它快时 await 它，慢时它早就 resolve 了
    const played = new Promise((r) => window.setTimeout(r, wait));
    const me = session ? { id: session.member.id, name: session.member.name, kind: "member" as const } : null;
    const flip = window.setTimeout(() => put({ ...p, status: "approved", decided_at: new Date().toISOString(), decided_by: me }), wait);
    try {
      const r = await api.proposals.approve(p.id);
      await played;
      window.clearTimeout(flip);
      put(r.proposal);
      toast.ok(t("proposals.approvedToast", { agent: p.agent.name, action: p.action_title }));
      refreshShipTelemetry();
    } catch (e) {
      await played;
      window.clearTimeout(flip);
      drop(p.id);
      toast.fail(errorMessage(e));
    } finally {
      setHelm(null);
      setBusy(null);
    }
  };

  /** 拒绝：理由是完整句子，Agent 会读它，所以不做乐观省略——写完才提交。 */
  const reject = async (p: Proposal, reason: string) => {
    setBusy(p.id);
    put({ ...p, status: "rejected", reason, decided_at: new Date().toISOString(), decided_by: session ? { id: session.member.id, name: session.member.name, kind: "member" } : null });
    try {
      const next = await api.proposals.reject(p.id, reason);
      put(next);
      setRejecting(null);
      toast.ok(t("proposals.rejectedToast", { agent: p.agent.name }));
      refreshShipTelemetry();
      return true;
    } catch (e) {
      drop(p.id);
      toast.fail(errorMessage(e));
      return false;
    } finally {
      setBusy(null);
    }
  };

  const emptyText = targetFilter ? t("proposals.emptyTarget") : status === "pending" ? (mine ? t("proposals.empty") : t("proposals.emptyAll")) : status ? t("proposals.emptyStatus", { status: proposalStatusTitle(status) }) : t("proposals.emptyAll");

  return (
    <div>
      <PageHeader title={t("proposals.title")} description={t("proposals.description")} />
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <StatChips
          value={status}
          onChange={(k) => { setStatus(k as ProposalStatus | null); setRecent([]); }}
          total={scoped.length}
          items={PROPOSAL_STATUSES.map((s) => ({ key: s, label: proposalStatusTitle(s), value: counts[s], tone: s === "pending" ? ("warning" as const) : undefined }))}
        />
        <Tip tip={t("proposals.mineHint")}>
          <Checkbox checked={mine} onChange={(e) => setMine(e.target.checked)} label={t("proposals.mineOnly")} className="text-ink-muted" />
        </Tip>
      </div>
      {targetFilter && (
        <div className="mb-3 flex flex-wrap items-center gap-2 text-body text-ink-muted">
          <Tag tone="accent">{t("proposals.filteredBy", { title: targetTitle ?? "" })}</Tag>
          <Link href="/proposals/" className="inline-flex items-center gap-1 text-caption text-ink-subtle hover:text-accent-hover">
            <IconClose size={12} />
            {t("proposals.clearFilter")}
          </Link>
        </div>
      )}
      <Panel index={1} icon={<IconApprove />} title={t("proposals.title")} telemetry={list.data ? t("panel.rows", { n: rows.length }) : undefined} padded={false}>
        {list.loading && !list.data ? (
          <div className="p-4"><ListSkeleton rows={4} /></div>
        ) : list.error ? (
          <div className="p-4"><ErrorBox message={list.error} onRetry={list.reload} /></div>
        ) : rows.length === 0 ? (
          <Empty text={emptyText} action={mine && !targetFilter && status === "pending" ? <span className="text-caption text-ink-subtle">{t("proposals.emptyMineHint")}</span> : undefined} />
        ) : (
          <ul className="divide-y divide-hairline">
            {rows.map((p) => (
              <Row
                key={p.id}
                p={p}
                now={now}
                types={types}
                caps={caps}
                executors={executors}
                open={!!open[p.id]}
                onToggle={() => setOpen((m) => ({ ...m, [p.id]: !m[p.id] }))}
                helm={helm === p.id}
                busy={busy === p.id}
                onApprove={() => void approve(p)}
                onReject={() => setRejecting(p)}
              />
            ))}
          </ul>
        )}
      </Panel>
      <RejectDialog proposal={rejecting} busy={!!busy} onClose={() => setRejecting(null)} onSubmit={reject} />
    </div>
  );
}

/** 一条待确认操作。展开后才去读对象现在的状态（读不到就直说读不到，不假装）。 */
function Row({ p, now, types, caps, executors, open, onToggle, helm, busy, onApprove, onReject }: {
  p: Proposal;
  now: number;
  types: Record<string, TaskType>;
  caps: Record<string, string>;
  executors: ExecutorRef[];
  open: boolean;
  onToggle: () => void;
  helm: boolean;
  busy: boolean;
  onApprove: () => void;
  onReject: () => void;
}) {
  const status = proposalStatusOf(p, now);
  const pending = status === "pending";
  // can_decide 是后端算好的"这条轮不轮得到我拍板"；老后端没有这个字段时按能处理算
  const mayDecide = p.can_decide !== false;
  const href = proposalTargetHref(p.target);
  return (
    <li className="px-4 py-3.5">
      <div className="flex items-start gap-3">
        <Avatar name={p.agent.name} kind="agent" size={24} className="mt-0.5" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="font-medium text-ink">{p.action_title}</span>
            {!p.target && <span className="text-caption text-ink-subtle">{t("proposals.noTarget")}</span>}
            {p.target && (
              <span className="inline-flex min-w-0 items-center gap-1.5 text-caption text-ink-subtle">
                <span>{t("proposals.targetLabel")}</span>
                <Tag>{proposalTargetKindTitle(p.target.kind)}</Tag>
                {href ? (
                  <Link href={href} className="min-w-0 truncate text-ink-muted hover:text-accent-hover" title={p.target.title}>{p.target.title}</Link>
                ) : (
                  <span className="min-w-0 truncate text-ink-muted" title={p.target.title}>{p.target.title}</span>
                )}
              </span>
            )}
            {!pending && <StatusTag status={status} />}
          </div>
          <p className="mt-1 text-body text-ink-muted">{p.summary}</p>
          {/* 遥测行：名字用正常字体（.eyebrow 会把拉丁名字全大写，Agent 的名字不该被喊出来），时间用等宽遥测色 */}
          <div className="mt-1.5 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-caption text-ink-subtle">
            <span className="text-ink-muted">{p.agent.name}</span>
            <span className="text-hairline-tertiary" aria-hidden="true">·</span>
            <span>{t("proposals.owner")}</span>
            <span className="text-ink-muted">{p.owner.name}</span>
            <span className="text-hairline-tertiary" aria-hidden="true">·</span>
            <RelativeTime iso={p.created_at} className="telemetry" />
            {pending && (
              <>
                <span className="text-hairline-tertiary" aria-hidden="true">·</span>
                <Expiry iso={p.expires_at} now={now} />
              </>
            )}
          </div>
          {!pending && <Decision p={p} status={status} />}
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          {pending && (
            <Tip tip={mayDecide ? null : t("proposals.cannotDecide")} className="inline-flex items-center gap-1.5">
              {/* 转舵：确认按钮上的舵轮转四分之一圈（DESIGN.md §7 F.），240ms 后这一行才翻成已确认 */}
              <span className="helm-turn inline-flex" data-motion={helm ? "helm" : undefined}>
                <Button variant="primary" size="sm" icon={<IconApprove />} disabled={busy || !mayDecide} onClick={onApprove}>
                  {t("proposals.approve")}
                </Button>
              </span>
              <Button variant="ghost" size="sm" className="text-danger hover:!text-danger" disabled={busy || !mayDecide} onClick={onReject}>
                {t("proposals.reject")}
              </Button>
            </Tip>
          )}
          <button
            type="button"
            onClick={onToggle}
            aria-expanded={open}
            className="pressable rounded-md p-1 text-ink-subtle hover:bg-surface-2 hover:text-ink"
            title={open ? t("proposals.collapse") : t("proposals.expand")}
            aria-label={open ? t("proposals.collapse") : t("proposals.expand")}
          >
            {open ? <IconChevronDown /> : <IconChevronRight />}
          </button>
        </div>
      </div>
      {open && (
        <div className="mt-3 ml-9 rounded-md border border-hairline bg-surface-2 p-3">
          <h3 className="eyebrow mb-2 text-ink-subtle">{t("proposals.payload")}</h3>
          <Payload payload={p.payload} types={types} caps={caps} executors={executors} target={p.target} />
          {p.grant && (
            <p className="mt-2 text-caption text-ink-subtle">
              {t("proposals.viaGrant", { grant: grantTitle(p.grant) })}
            </p>
          )}
          {p.target && (
            <div className="mt-3 border-t border-hairline pt-3">
              <h3 className="eyebrow mb-2 text-ink-subtle">{t("proposals.targetNow", { kind: proposalTargetKindTitle(p.target.kind) })}</h3>
              <TargetState target={p.target} />
            </div>
          )}
        </div>
      )}
    </li>
  );
}

const STATUS_TONE = { pending: "warning", approved: "success", rejected: "danger", expired: "neutral" } as const;
function StatusTag({ status }: { status: ProposalStatus }) {
  return <Tag tone={STATUS_TONE[status]} dark={status === "expired"}>{proposalStatusTitle(status)}</Tag>;
}

/** 已经处理过的那一行下面的一句交代：谁、什么时候、为什么。 */
function Decision({ p, status }: { p: Proposal; status: ProposalStatus }) {
  const who = p.decided_by?.name;
  const line = status === "approved" && who ? t("proposals.approvedBy", { name: who }) : status === "rejected" && who ? t("proposals.rejectedBy", { name: who }) : status === "expired" ? t("proposals.expiredNote") : null;
  if (!line && !p.reason) return null;
  return (
    <p className="mt-1.5 text-caption text-ink-subtle">
      {line}
      {p.decided_at && <span className="ml-1 text-telemetry" title={fmtDateTime(p.decided_at)}>{fmtDateTime(p.decided_at)}</span>}
      {p.reason && <span className="mt-0.5 block text-ink-muted">{t("proposals.reasonShown", { reason: p.reason })}</span>}
    </p>
  );
}

/** 还有多久过期；6 小时内变警示色，过了就直说已过期。 */
function Expiry({ iso, now }: { iso: string | null; now: number }) {
  const d = parseDate(iso);
  if (!d) return null;
  const ms = d.getTime() - now;
  if (ms <= 0) return <span>{t("proposals.expired")}</span>;
  const hours = Math.floor(ms / 3600_000);
  const text = ms < 3600_000 ? t("proposals.expiresSoon") : t("proposals.expiresIn", { time: hours < 24 ? t("time.hours", { n: hours }) : t("time.days", { n: Math.floor(hours / 24) }) });
  return <span className={cx(ms < SOON_MS && "text-warning")} title={fmtDateTime(iso)}>{text}</span>;
}

// ---------- 确认后按这些内容执行 ----------

const DATE_FIELDS = ["planned_start", "planned_end", "starts_on", "ends_on", "due"];
const TEXT_FIELDS = ["description", "body", "comment", "summary", "reason", "result"];
const ID_FIELDS = ["goal_id", "parent_id", "task_id", "task_ids", "sprint_id", "assignee_id", "executor_id", "reviewer_id"];
const EXECUTOR_FIELDS = ["assignee_id", "assignee", "executor_id", "reviewer_id", "owner_id"];

/** payload 排成人话的键值表：认识的字段翻成中文并把值也翻好，不认识的照原样列出来，不塞 JSON 给人读。 */
interface ValueCtx {
  types: Record<string, TaskType>;
  caps: Record<string, string>;
  executors: ExecutorRef[];
  target: ProposalTarget | null;
}

function Payload({ payload, ...ctx }: { payload: Record<string, unknown> } & ValueCtx) {
  const entries = Object.entries(payload ?? {}).filter(([, v]) => v !== null && v !== undefined && v !== "");
  if (!entries.length) return <p className="text-caption text-ink-subtle">{t("proposals.payloadEmpty")}</p>;
  return <DescList items={entries.map(([k, v]) => [proposalFieldTitle(k), <Value key={k} field={k} value={v} {...ctx} />])} />;
}

function Value({ field, value, types, caps, executors, target }: { field: string; value: unknown } & ValueCtx): ReactNode {
  const ctx = { types, caps, executors, target };
  if (Array.isArray(value)) {
    return <span className="flex flex-wrap gap-1">{value.map((v, i) => <Tag key={i}>{field === "capabilities" ? capabilityTitle(String(v), caps) : typeof v === "object" ? JSON.stringify(v) : String(v)}</Tag>)}</span>;
  }
  if (typeof value === "boolean") return <span>{value ? t("common.yes") : t("common.no")}</span>;
  if (value && typeof value === "object") {
    return <DescList className="text-caption" items={Object.entries(value as Record<string, unknown>).map(([k, v]) => [proposalFieldTitle(k), <Value key={k} field={k} value={v} {...ctx} />])} />;
  }
  const s = String(value);
  // 执行者 id 换成名字：确认之前最该看清楚的就是"这件事会落到谁头上"
  if (EXECUTOR_FIELDS.includes(field)) {
    const who = executors.find((x) => x.id === s);
    if (who) return <ExecutorName executor={who} />;
  }
  // 指向的正是这条待确认操作的对象时，直接给名字与链接，不让人对着一个 id 猜
  if (ID_FIELDS.includes(field) && target && s === target.id) {
    const href = proposalTargetHref(target);
    return href ? <Link href={href} className="text-ink hover:text-accent-hover">{target.title}</Link> : <span>{target.title}</span>;
  }
  if (DATE_FIELDS.includes(field)) return <span className="telemetry">{fmtDate(s)}</span>;
  if (field === "priority") return <span>{priorityTitle(s as Priority)}</span>;
  if (field === "type" || field === "task_type") return <span>{types[s]?.title ?? s}</span>;
  if (field === "capabilities") return <Tag>{capabilityTitle(s, caps)}</Tag>;
  if (ID_FIELDS.includes(field)) return <Code className="text-caption">{s}</Code>;
  if (TEXT_FIELDS.includes(field)) return <span className="whitespace-pre-wrap">{s}</span>;
  if (typeof value === "number") return <span className="tabular-nums">{s}</span>;
  return <span className="break-words">{s}</span>;
}

// ---------- 对象现在的状态 ----------

type TargetView =
  | { kind: "task"; task: Task }
  | { kind: "goal"; goal: Goal }
  | { kind: "sprint"; sprint: Sprint }
  | null;

/** 展开时才读：让人在确认之前看一眼这个对象现在是什么样。 */
function TargetState({ target }: { target: ProposalTarget }) {
  const r = useLoad(async (): Promise<TargetView> => {
    if (target.kind === "task") return { kind: "task", task: await api.tasks.get(target.id) };
    if (target.kind === "goal") return { kind: "goal", goal: await api.goals.get(target.id) };
    if (target.kind === "sprint") return { kind: "sprint", sprint: await api.sprints.get(target.id) };
    return null;
  }, [target.kind, target.id]);
  if (r.loading && !r.data) return <ListSkeleton rows={1} />;
  if (r.error || !r.data) return <p className="text-caption text-ink-subtle">{t("proposals.targetGone")}</p>;
  const v = r.data;
  if (v.kind === "task") {
    return (
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-body">
        <StateBadge state={v.task.state} />
        <span className="inline-flex items-center gap-1.5 text-caption text-ink-subtle">
          {t("proposals.targetAssignee")}
          <ExecutorName executor={v.task.assignee} className="text-ink-muted" />
        </span>
      </div>
    );
  }
  if (v.kind === "goal") {
    return (
      <div className="flex items-center gap-3">
        <ProgressBar value={v.goal.progress} className="max-w-[240px]" tone={v.goal.achieved ? "success" : "accent"} />
        <span className="text-caption text-ink-muted">{t("proposals.targetProgress", { n: Math.round(v.goal.progress) })}</span>
      </div>
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 text-body">
      <Tag tone={v.sprint.status === "active" ? "accent" : v.sprint.status === "closed" ? "success" : "neutral"}>{v.sprint.name}</Tag>
      <span className="telemetry text-caption">{fmtDate(v.sprint.starts_on)} – {fmtDate(v.sprint.ends_on)}</span>
    </div>
  );
}

// ---------- 拒绝：理由是写给 Agent 看的 ----------

function RejectDialog({ proposal, busy, onClose, onSubmit }: { proposal: Proposal | null; busy: boolean; onClose: () => void; onSubmit: (p: Proposal, reason: string) => Promise<boolean> }) {
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  // 换了一条（或关掉重开）就把输入清空：渲染期比较，不用 effect
  const id = proposal?.id ?? null;
  const [seen, setSeen] = useState(id);
  if (seen !== id) {
    setSeen(id);
    setReason("");
    setError(null);
  }
  const submit = async () => {
    if (!proposal) return;
    if (reason.trim().length < MIN_REASON) {
      setError(t("proposals.reasonTooShort"));
      return;
    }
    // 失败时后端那句完整的理由已经由 Toast 说了，这里只把对话框留着让人改
    await onSubmit(proposal, reason.trim());
  };
  return (
    <Dialog
      open={!!proposal}
      onClose={onClose}
      title={t("proposals.rejectTitle")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="danger" disabled={busy} onClick={() => void submit()}>{t("proposals.reject")}</Button>
        </>
      }
    >
      {proposal && <p className="text-ink-muted">{proposal.summary}</p>}
      <Field label={t("proposals.reasonLabel")} error={error}>
        <Textarea value={reason} onChange={(e) => { setReason(e.target.value); setError(null); }} placeholder={t("proposals.reasonPlaceholder")} autoFocus />
      </Field>
      {/* 说明始终在场：写错了要提示，但"这句话是给 Agent 看的"这件事不能因为报错就消失 */}
      <p className="text-caption text-ink-subtle">{t("proposals.rejectHint", { agent: proposal?.agent.name })}</p>
    </Dialog>
  );
}
