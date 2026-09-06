"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { api, type InboxGroup, type InboxKind, type InboxQuestion, type InboxTask, type Notification, type Proposal } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import { errorMessage, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { prefersReducedMotion } from "@/lib/motion";
import { IconApprove, IconChevronRight, IconPlay } from "@/components/icons";
import { HELM_MS } from "@/components/proposals/RejectDialog";
import { refreshShipTelemetry } from "@/components/ship-status/telemetry";
import { useToast } from "@/components/toast";
import { Avatar, Button, RelativeTime, Tag, Tip, cx, TaskNumber } from "@/components/ui";
import type { CompactRow } from "@/components/blocks/BlockPanel";

/*
 * 待我处理（DESIGN.md §12、CONTEXT.md「待我处理」）：「我的工作」里的一个区块（InboxBlock，ADR 0015 补记四：可拖动、改大小、移除），这里是它的取数、动作与行。
 * GET /inbox 给六组事项（按紧急度：逾期 → 待确认 → 待验收 → 待答复 → 未开始 → 通知，空组省略），只看本人、不受范围影响。
 * 每条右侧就是它的动作：
 *   逾期任务 → 打开 · 待确认操作 → 确认 / 拒绝（与待确认操作页同一套：转舵 240ms、拒绝要写完整理由）· 等我验收 → 去验收
 *   等我答复 → 答复（任务详情定位到那条评论）· 未开始 → 开始（乐观）· 通知 → 标为已读（这一组在屏幕上停留 3 秒后自动标已读）
 * 处理完一条：行 200ms 淡出并收拢高度（沿用目标树的 .fold 曲线），读数用数字滚动。
 * 自动标已读的通知不消失：读数与角标减掉它们，行留在原位变淡，让人这次还能看完；手动点「标为已读」的才收起。
 */

/** 行收拢 200ms 之后再从列表里拿掉（多留 20ms 给过渡收尾） */
const FOLD_MS = 220;
/** 通知组在屏幕上停留这么久之后自动标已读 */
const AUTO_READ_MS = 3000;

export const rowKey = (kind: InboxKind, id: string | number) => `${kind}:${id}`;

/**
 * 待我处理的状态与动作（取数、乐观收拢、确认 / 拒绝 / 开始 / 标已读）。「待我处理」区块（InboxBlock）用它；渲染在 InboxGroups 里。
 */
export function useInbox() {
  const toast = useToast();
  const router = useRouter();
  const inbox = useLoad(() => api.inbox.get(), []);
  // 正在收拢的行 / 已经拿掉的行：都算"已处理"，读数立刻减掉
  const [closing, setClosing] = useState<Set<string>>(() => new Set());
  const [gone, setGone] = useState<Set<string>>(() => new Set());
  // 自动标已读的通知：留在原位变淡，只从读数里减掉
  const [autoRead, setAutoRead] = useState<Set<number>>(() => new Set());
  const [helm, setHelm] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [rejecting, setRejecting] = useState<Proposal | null>(null);
  const timers = useRef<number[]>([]);
  useEffect(() => () => timers.current.forEach((id) => window.clearTimeout(id)), []);

  // 数据重新取回来（切语言、重试）时，本地的处理痕迹作废
  const [seenData, setSeenData] = useState(inbox.data);
  if (seenData !== inbox.data) {
    setSeenData(inbox.data);
    setClosing(new Set());
    setGone(new Set());
    setAutoRead(new Set());
  }

  const handled = (key: string) => closing.has(key) || gone.has(key);
  /** 处理完一条：先收拢，200ms 后从列表拿掉 */
  const close = (key: string) => {
    setClosing((s) => new Set(s).add(key));
    const wait = prefersReducedMotion() ? 0 : FOLD_MS;
    timers.current.push(
      window.setTimeout(() => {
        setGone((s) => new Set(s).add(key));
        setClosing((s) => {
          const n = new Set(s);
          n.delete(key);
          return n;
        });
      }, wait),
    );
  };
  /** 乐观处理失败：把行放回来 */
  const reopen = (key: string) => {
    setClosing((s) => {
      const n = new Set(s);
      n.delete(key);
      return n;
    });
    setGone((s) => {
      const n = new Set(s);
      n.delete(key);
      return n;
    });
  };

  // ---------- 动作 ----------
  /** 确认：转舵 240ms → 后端回来 → 这一行收拢。后端比动效快时也把 240ms 放完；失败时舵轮转回去、行留在原位、Toast 说后端那句理由。 */
  const approve = async (p: Proposal) => {
    if (busy) return;
    setBusy(p.id);
    setHelm(p.id);
    const wait = prefersReducedMotion() ? 0 : HELM_MS;
    const played = new Promise((r) => window.setTimeout(r, wait));
    try {
      await api.proposals.approve(p.id);
      await played;
      close(rowKey("proposals", p.id));
      toast.ok(t("proposals.approvedToast", { agent: p.agent.name, action: p.action_title }));
      refreshShipTelemetry();
    } catch (e) {
      await played;
      toast.fail(errorMessage(e));
    } finally {
      setHelm(null);
      setBusy(null);
    }
  };
  /** 拒绝：理由写完才提交（Agent 会读它），成功后这一行收拢 */
  const reject = async (p: Proposal, reason: string) => {
    setBusy(p.id);
    try {
      await api.proposals.reject(p.id, reason);
      setRejecting(null);
      close(rowKey("proposals", p.id));
      toast.ok(t("proposals.rejectedToast", { agent: p.agent.name }));
      refreshShipTelemetry();
      return true;
    } catch (e) {
      toast.fail(errorMessage(e));
      return false;
    } finally {
      setBusy(null);
    }
  };
  /**
   * 开始一条未开始的任务：乐观收拢，然后按流程可用性做正确的那一步——
   * 已在进行中类型状态就直接开始执行记录；否则走那条通往进行中状态、由我触发的步骤（内核在负责人自己触发时开执行记录，ADR 0006）。
   * 那一步要附带评论 / 结果时，这里做不了，把行放回并带人去任务详情；一步都走不了就放回并说后端的理由。
   */
  const begin = async (task: InboxTask) => {
    const key = rowKey("unstarted", task.id);
    close(key);
    try {
      const wf = await api.tasks.workflow(task.id);
      if (wf.can_begin) {
        await api.tasks.begin(task.id);
      } else {
        const toActive = wf.transitions.filter((tr) => tr.label_to === "active");
        const ready = toActive.find((tr) => tr.available);
        if (!ready) {
          reopen(key);
          const reasons = [...wf.begin_reasons, ...toActive.flatMap((tr) => tr.reasons)];
          toast.fail(reasons[0] ?? t("inbox.cannotBegin"));
          return;
        }
        if (ready.requires.length > 0) {
          reopen(key);
          router.push(taskHref(task.id));
          return;
        }
        await api.tasks.transition(task.id, ready.name);
      }
      toast.ok(t("inbox.begun", { title: task.title }));
      refreshShipTelemetry();
    } catch (e) {
      reopen(key);
      toast.fail(errorMessage(e));
    }
  };
  /** 手动标已读：乐观收拢 */
  const markRead = async (n: Notification) => {
    const key = rowKey("notifications", n.id);
    close(key);
    try {
      await api.notifications.read([n.id]);
      refreshShipTelemetry();
    } catch (e) {
      reopen(key);
      toast.fail(errorMessage(e));
    }
  };
  /** 通知组在屏幕上停留 3 秒：把还没处理的都标已读，行留在原位变淡 */
  const autoMark = async (ids: number[]) => {
    if (!ids.length) return;
    try {
      await api.notifications.read(ids);
      setAutoRead((s) => {
        const n = new Set(s);
        ids.forEach((id) => n.add(id));
        return n;
      });
      refreshShipTelemetry();
    } catch (e) {
      toast.fail(errorMessage(e));
    }
  };

  // ---------- 可见的组与读数 ----------
  const data = inbox.data;
  const groups = (data?.groups ?? []).map((g) => ({ ...g, items: (g.items as Array<InboxTask | Proposal | InboxQuestion | Notification>).filter((it) => !gone.has(rowKey(g.kind, idOf(g.kind, it)))) })) as InboxGroup[];
  const visible = groups.filter((g) => g.items.length > 0);
  const total = visible.reduce((s, g) => s + g.items.filter((it) => !closing.has(rowKey(g.kind, idOf(g.kind, it))) && !(g.kind === "notifications" && autoRead.has((it as Notification).id))).length, 0);
  const allEmpty = !!data && visible.length === 0;

  return { inbox, data, visible, total, allEmpty, handled, autoRead, helm, busy, rejecting, setRejecting, approve, reject, begin, markRead, autoMark };
}

/** 全部可见的组，一组接一组（区块的正常尺寸）。 */
export function InboxGroups({ ib }: { ib: ReturnType<typeof useInbox> }) {
  return (
    <>
      {ib.visible.map((g) => (
        <Group
          key={g.kind}
          group={g}
          handled={ib.handled}
          autoRead={ib.autoRead}
          helm={ib.helm}
          busy={ib.busy}
          onApprove={(p) => void ib.approve(p)}
          onReject={ib.setRejecting}
          onBegin={(task) => void ib.begin(task)}
          onRead={(n) => void ib.markRead(n)}
          onAutoRead={(ids) => void ib.autoMark(ids)}
        />
      ))}
    </>
  );
}

/** 每组还剩几条没处理（通知只算未读） */
export function liveCount(ib: ReturnType<typeof useInbox>, g: InboxGroup): number {
  return (g.items as Array<InboxTask | Proposal | InboxQuestion | Notification>).filter((it) => !ib.handled(rowKey(g.kind, idOf(g.kind, it))) && !(g.kind === "notifications" && ib.autoRead.has((it as Notification).id))).length;
}

/** 窄区块（≤ 4 栏）的行：每条事项一行标题 + 右侧这一组的短名，按组的紧急度顺序排。 */
export function compactRows(ib: ReturnType<typeof useInbox>): CompactRow[] {
  const rows: CompactRow[] = [];
  for (const g of ib.visible) {
    const meta = <span className={cx(g.kind === "overdue" && "text-danger")}>{t(`inbox.short.${g.kind}`)}</span>;
    for (const it of g.items as Array<InboxTask | Proposal | InboxQuestion | Notification>) {
      const key = rowKey(g.kind, idOf(g.kind, it));
      if (ib.handled(key)) continue;
      if (g.kind === "notifications" && ib.autoRead.has((it as Notification).id)) continue;
      let title: ReactNode;
      if (g.kind === "proposals") {
        const p = it as Proposal;
        title = <Link href="/proposals/" className="block truncate hover:text-accent-hover">{p.agent.name} · {p.action_title}</Link>;
      } else if (g.kind === "questions") {
        const q = it as InboxQuestion;
        title = <Link href={`${taskHref(q.task.id)}#comment-${encodeURIComponent(q.comment.id)}`} className="block truncate hover:text-accent-hover">{q.comment.author.name} · {q.comment.body}</Link>;
      } else if (g.kind === "notifications") {
        const n = it as Notification;
        title = n.task_id ? <Link href={taskHref(n.task_id)} className="block truncate hover:text-accent-hover">{n.title}</Link> : <span className="block truncate">{n.title}</span>;
      } else {
        const task = it as InboxTask;
        title = <Link href={taskHref(task.id)} className="block truncate hover:text-accent-hover"><TaskNumber n={task.number} className="mr-1" />{task.title}</Link>;
      }
      rows.push({ key, title, meta });
    }
  }
  return rows;
}

export const idOf = (kind: InboxKind, it: InboxTask | Proposal | InboxQuestion | Notification): string | number =>
  kind === "questions" ? (it as InboxQuestion).comment.id : (it as InboxTask | Proposal | Notification).id;

const groupTitle = (g: InboxGroup) => g.title || t(`inbox.group.${g.kind}`);

/** 一组：小标题 + 条数，下面一列紧凑的行。通知组挂 IntersectionObserver：在屏幕上连续停留 3 秒就把还没读的标为已读。 */
function Group({ group, handled, autoRead, helm, busy, onApprove, onReject, onBegin, onRead, onAutoRead }: {
  group: InboxGroup;
  handled: (key: string) => boolean;
  autoRead: Set<number>;
  helm: string | null;
  busy: string | null;
  onApprove: (p: Proposal) => void;
  onReject: (p: Proposal) => void;
  onBegin: (task: InboxTask) => void;
  onRead: (n: Notification) => void;
  onAutoRead: (ids: number[]) => void;
}) {
  const ref = useRef<HTMLElement>(null);
  const fired = useRef(false);
  const isNotifications = group.kind === "notifications";
  const pendingIds = isNotifications ? (group.items as Notification[]).filter((n) => !handled(rowKey("notifications", n.id)) && !autoRead.has(n.id)).map((n) => n.id) : [];
  const pendingKey = pendingIds.join(",");
  useEffect(() => {
    if (!isNotifications || fired.current || !pendingKey || !ref.current || typeof IntersectionObserver === "undefined") return;
    let timer = 0;
    const io = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          if (!timer) {
            timer = window.setTimeout(() => {
              fired.current = true;
              io.disconnect();
              onAutoRead(pendingKey.split(",").map(Number));
            }, AUTO_READ_MS);
          }
        } else if (timer) {
          window.clearTimeout(timer);
          timer = 0;
        }
      },
      { threshold: 0.5 },
    );
    io.observe(ref.current);
    return () => {
      io.disconnect();
      if (timer) window.clearTimeout(timer);
    };
    // 只按待读的 id 集合重挂：id 一样时不重新计时
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isNotifications, pendingKey]);

  const live = group.items.filter((it) => !handled(rowKey(group.kind, idOf(group.kind, it as InboxTask | Proposal | InboxQuestion | Notification)))).length;
  const unread = isNotifications ? pendingIds.length : live;
  return (
    <section ref={ref} className="inbox-group border-b border-hairline last:border-b-0" aria-label={groupTitle(group)}>
      <header className="flex items-center gap-2 border-b border-hairline bg-surface-2/60 px-4 py-1.5">
        <span className="eyebrow text-ink-subtle">{groupTitle(group)}</span>
        <span className="eyebrow text-telemetry tabular-nums">{unread}</span>
      </header>
      <ul>
        {group.kind === "proposals" &&
          group.items.map((p) => (
            <Row key={p.id} closed={handled(rowKey("proposals", p.id))}>
              <ProposalRow p={p} helm={helm === p.id} busy={busy === p.id} onApprove={() => onApprove(p)} onReject={() => onReject(p)} />
            </Row>
          ))}
        {group.kind === "questions" &&
          group.items.map((q) => (
            <Row key={q.comment.id} closed={handled(rowKey("questions", q.comment.id))}>
              <QuestionRow q={q} />
            </Row>
          ))}
        {group.kind === "notifications" &&
          group.items.map((n) => (
            <Row key={n.id} closed={handled(rowKey("notifications", n.id))}>
              <NotificationRow n={n} read={autoRead.has(n.id)} onRead={() => onRead(n)} />
            </Row>
          ))}
        {(group.kind === "overdue" || group.kind === "review" || group.kind === "unstarted") &&
          group.items.map((task) => (
            <Row key={task.id} closed={handled(rowKey(group.kind, task.id))}>
              <TaskRow kind={group.kind} task={task} onBegin={() => onBegin(task)} />
            </Row>
          ))}
      </ul>
    </section>
  );
}

/** 一行的外壳：处理完（closed）就用 .fold 收拢——grid 行 1fr → 0fr + 淡出，200ms。 */
function Row({ closed, children }: { closed: boolean; children: ReactNode }) {
  return (
    <li className="fold inbox-row border-t border-hairline first:border-t-0" data-closed={closed ? "" : undefined} inert={closed} aria-hidden={closed || undefined}>
      <div className="flex items-center gap-3 px-4 py-2.5">{children}</div>
    </li>
  );
}

/** 行右侧的链接动作：看起来是幽灵按钮，本体是链接（新标签页、拖拽都还是链接的行为） */
function LinkAction({ href, children, icon }: { href: string; children: ReactNode; icon?: ReactNode }) {
  return (
    <Link href={href} className="inline-flex shrink-0">
      <Button size="sm" variant="ghost" tabIndex={-1} icon={icon}>
        {children}
      </Button>
    </Link>
  );
}

export const taskHref = (id: string) => `/tasks/${encodeURIComponent(id)}/`;

/** 任务类的行：标题 + 一行说明（目标 · 类型 · 状态，逾期几天 / 谁提交的 / 计划何时开始），右侧按组给动作。 */
function TaskRow({ kind, task, onBegin }: { kind: "overdue" | "review" | "unstarted"; task: InboxTask; onBegin: () => void }) {
  const meta: ReactNode[] = [];
  if (task.goal) meta.push(<span key="g" className="truncate">{task.goal.title}</span>);
  meta.push(<span key="t">{task.type_title}</span>);
  if (kind === "overdue") meta.push(<span key="o" className="text-danger">{t("inbox.overdueDays", { n: task.days_overdue })}</span>);
  else if (kind === "review") meta.push(<span key="r">{task.assignee ? t("inbox.submittedBy", { name: task.assignee.name }) : task.state.title}</span>);
  else if (task.planned_start) meta.push(<span key="p">{t("inbox.plannedStart", { date: fmtDate(task.planned_start) })}</span>);
  return (
    <>
      <Link href={taskHref(task.id)} className="min-w-0 flex-1 hover:text-accent-hover">
        <span className="block truncate text-body text-ink"><TaskNumber n={task.number} className="mr-1" />{task.title}</span>
        <span className="mt-0.5 flex min-w-0 items-center gap-1.5 text-caption text-ink-subtle">
          {meta.map((m, i) => (
            <span key={i} className="inline-flex min-w-0 items-center gap-1.5">
              {i > 0 && <span className="text-hairline-tertiary" aria-hidden="true">·</span>}
              {m}
            </span>
          ))}
        </span>
      </Link>
      {kind === "overdue" && <LinkAction href={taskHref(task.id)} icon={<IconChevronRight />}>{t("inbox.open")}</LinkAction>}
      {kind === "review" && <LinkAction href={taskHref(task.id)} icon={<IconChevronRight />}>{t("inbox.review")}</LinkAction>}
      {kind === "unstarted" && (
        <Button size="sm" variant="ghost" icon={<IconPlay />} onClick={onBegin} className="shrink-0">
          {t("inbox.begin")}
        </Button>
      )}
    </>
  );
}

/** 待确认操作的行：发起的 Agent、动作、后端那句"确认后会发生什么"；右侧确认（转舵）/ 拒绝。 */
function ProposalRow({ p, helm, busy, onApprove, onReject }: { p: Proposal; helm: boolean; busy: boolean; onApprove: () => void; onReject: () => void }) {
  const mayDecide = p.can_decide !== false;
  return (
    <>
      <Avatar name={p.agent.name} kind="agent" className="shrink-0" />
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate text-body text-ink">{p.agent.name}</span>
          <Tag tone="accent">{p.action_title}</Tag>
          {p.target && <span className="hidden truncate text-caption text-ink-subtle sm:inline">{p.target.title}</span>}
          <RelativeTime iso={p.created_at} className="ml-auto shrink-0 text-caption text-ink-subtle" />
        </span>
        <span className="mt-0.5 line-clamp-1 block text-caption text-ink-muted" title={p.summary}>{p.summary}</span>
      </span>
      <Tip tip={mayDecide ? null : t("proposals.cannotDecide")} className="inline-flex shrink-0 items-center gap-1">
        <span className="helm-turn inline-flex" data-motion={helm ? "helm" : undefined}>
          <Button variant="primary" size="sm" icon={<IconApprove />} disabled={busy || !mayDecide} onClick={onApprove}>
            {t("proposals.approve")}
          </Button>
        </span>
        <Button variant="ghost" size="sm" className="text-danger hover:!text-danger" disabled={busy || !mayDecide} onClick={onReject}>
          {t("proposals.reject")}
        </Button>
      </Tip>
    </>
  );
}

/** 等我答复的提问：谁在哪个任务里问了什么；右侧「答复」去任务详情并定位到那条评论。 */
function QuestionRow({ q }: { q: InboxQuestion }) {
  const href = `${taskHref(q.task.id)}#comment-${encodeURIComponent(q.comment.id)}`;
  return (
    <>
      <Avatar name={q.comment.author.name} kind={q.comment.author.kind} className="shrink-0" />
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-center gap-1.5 text-caption text-ink-subtle">
          <span className="text-ink-muted">{q.comment.author.name}</span>
          <span className="truncate">{t("inbox.askedIn", { title: q.task.title })}</span>
          <RelativeTime iso={q.comment.created_at} className="ml-auto shrink-0" />
        </span>
        <span className="mt-0.5 line-clamp-2 block text-body text-ink">{q.comment.body}</span>
      </span>
      <LinkAction href={href} icon={<IconChevronRight />}>{t("inbox.reply")}</LinkAction>
    </>
  );
}

/** 通知：标题 + 正文一行 + 时间；有任务的能点去任务。自动标已读后变淡，只剩「已读」两个字。 */
function NotificationRow({ n, read, onRead }: { n: Notification; read: boolean; onRead: () => void }) {
  const title = <span className={cx("block truncate text-body", read ? "text-ink-muted" : "text-ink")}>{n.title}</span>;
  return (
    <>
      <span className={cx("inbox-led shrink-0", read && "inbox-led-read")} aria-hidden="true" />
      <span className="min-w-0 flex-1">
        {n.task_id ? <Link href={taskHref(n.task_id)} className="block hover:text-accent-hover">{title}</Link> : title}
        {n.body && <span className={cx("mt-0.5 line-clamp-1 block text-caption", read ? "text-ink-subtle" : "text-ink-muted")} title={n.body}>{n.body}</span>}
      </span>
      <RelativeTime iso={n.created_at} className="shrink-0 text-caption text-ink-subtle" />
      {read ? (
        <span className="w-[72px] shrink-0 text-right text-caption text-ink-subtle">{t("inbox.read")}</span>
      ) : (
        <Button size="sm" variant="ghost" onClick={onRead} className="shrink-0">
          {t("inbox.markRead")}
        </Button>
      )}
    </>
  );
}
