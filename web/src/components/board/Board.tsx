"use client";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from "react";
import { api, type BoardCard as BoardCardData, type BoardColumn, type BoardData, type BoardQuery, type TransitionAvailability } from "@/lib/api";
import { fmtDate, fmtMoney } from "@/lib/format";
import { errorMessage, useLoad } from "@/lib/hooks";
import { getLocale, t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { isOverdue, OverdueTag, PriorityTag, TypeLabel } from "@/components/TaskTable";
import { useToast } from "@/components/toast";
import { Avatar, Button, Dialog, ErrorBox, Field, Skeleton, StatusLED, Textarea, cx } from "@/components/ui";
import { PointsChip } from "./PointsChips";
import { usePointerDrag } from "./usePointerDrag";

/*
 * 看板（ADR 0012）：列 = 所选任务类型流程里的状态（不选类型时 = 五种状态类型），卡片 = 任务，拖动 = 调一次 tasks.transition。
 * - 拖动时，卡片 can_move_to 之外的列淡出；放到那里会被拒绝，理由来自 GET /tasks/{id}/workflow 里那一步的 reasons（后端拼好的中文句子）。
 * - 乐观更新：放下即把卡片挪到目标列并打 pending，后端确认后重新加载；失败回滚并 Toast 理由。
 * - 键盘：Tab 到卡片，空格拿起，左右选列，回车放下，Esc 取消；aria-live 播报。
 * - 分道（按目标 / 按执行者）：接口只给 lanes[{key,title}]，卡片归道按 card.goal.id / card.assignee.id 在前端对上（没有的进「_」道）。
 * 高度 = 视口剩余高度（与甘特图一致，把空间全给图），内部滚动，列头吸顶。
 */
export type BoardDensity = "comfortable" | "compact";

const LANE_NONE = "_";
const laneOf = (card: BoardCardData, lane: BoardQuery["lane"]) => (lane === "goal" ? (card.goal?.id ?? LANE_NONE) : lane === "assignee" ? (card.assignee?.id ?? LANE_NONE) : LANE_NONE);
const dropKey = (col: string, lane: string) => `${col}|${lane}`;
const parseDrop = (key: string | null) => (key ? { col: key.split("|")[0], lane: key.split("|")[1] ?? LANE_NONE } : null);
const sep = () => (getLocale() === "zh-CN" ? "；" : "; ");

interface PendingStep {
  card: BoardCardData;
  from: number;
  to: number;
  tr: TransitionAvailability;
}

export function Board({ query, density = "comfortable", className, onData }: { query: BoardQuery; density?: BoardDensity; className?: string; onData?: (data: BoardData) => void }) {
  const router = useRouter();
  const toast = useToast();
  const { session } = useSession();
  const currency = session?.organization.currency;
  const key = JSON.stringify(query);
  const res = useLoad(() => api.board.get(query), [key]);

  // 本地副本：乐观移动改它；接口数据一到就整体替换
  const [local, setLocal] = useState<BoardData | null>(null);
  const [seen, setSeen] = useState<BoardData | null>(null);
  if (seen !== res.data) {
    setSeen(res.data);
    setLocal(res.data);
  }
  useEffect(() => {
    if (res.data) onData?.(res.data);
  }, [res.data, onData]);

  const [pending, setPending] = useState<Set<string>>(() => new Set());
  const [landed, setLanded] = useState<string | null>(null);
  const [step, setStep] = useState<PendingStep | null>(null);
  const [comment, setComment] = useState("");
  const [result, setResult] = useState("");
  const [stepError, setStepError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [announce, setAnnounce] = useState("");
  const [kb, setKb] = useState<{ id: string; from: number; target: number } | null>(null);
  /** 移动后把焦点交还给同一张卡（它已经在另一列里）：effect 里读并清掉 */
  const focusReq = useRef<string | null>(null);

  // 高度 = 视口剩余：量一次顶部位置写进 --bd-top，窗口变化时重量
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => el.style.setProperty("--bd-top", `${Math.round(el.getBoundingClientRect().top + window.scrollY)}px`);
    measure();
    window.addEventListener("resize", measure);
    return () => window.removeEventListener("resize", measure);
  }, []);

  const columns = useMemo(() => local?.columns ?? [], [local]);
  const lanes = useMemo(() => (query.lane && query.lane !== "none" && local?.lanes?.length ? local.lanes : null), [query.lane, local]);

  const findCard = useCallback(
    (id: string): { card: BoardCardData; col: number } | null => {
      for (let i = 0; i < columns.length; i++) {
        const c = columns[i].cards.find((x) => x.id === id);
        if (c) return { card: c, col: i };
      }
      return null;
    },
    [columns],
  );
  const allowed = (card: BoardCardData, fromCol: number, col: BoardColumn, colIdx: number) => colIdx === fromCol || card.can_move_to.includes(col.state.name);

  /** 把卡片挪到另一列（本地），返回撤销函数 */
  const applyMove = (card: BoardCardData, from: number, to: number, pendingFlag: boolean) => {
    setLocal((cur) => {
      if (!cur) return cur;
      const cols = cur.columns.map((c) => ({ ...c, cards: c.cards.filter((x) => x.id !== card.id) }));
      const target = cols[to];
      const moved: BoardCardData = { ...card, state: target.state };
      cols[to] = { ...target, cards: [moved, ...target.cards] };
      for (const c of cols) {
        c.count = c.cards.length;
        c.over_limit = c.wip_limit != null && c.count > c.wip_limit;
      }
      return { ...cur, columns: cols };
    });
    setPending((s) => {
      const n = new Set(s);
      if (pendingFlag) n.add(card.id);
      else n.delete(card.id);
      return n;
    });
  };

  const commit = async (card: BoardCardData, from: number, to: number, tr: TransitionAvailability, body?: { comment?: string; result?: Record<string, unknown> }) => {
    const target = columns[to];
    applyMove(card, from, to, true);
    focusReq.current = card.id;
    try {
      await api.tasks.transition(card.id, tr.name, body);
      toast.ok(t("board.moved", { title: card.title, state: target.state.title }));
      setLanded(card.id);
      window.setTimeout(() => setLanded((x) => (x === card.id ? null : x)), 400);
      res.reload();
      return true;
    } catch (e) {
      const m = errorMessage(e);
      applyMove({ ...card, state: columns[from].state }, to, from, false);
      toast.fail(t("toast.rollback", { reason: m }));
      return false;
    } finally {
      setPending((s) => {
        const n = new Set(s);
        n.delete(card.id);
        return n;
      });
    }
  };

  /** 拖放 / 键盘放下都走这里：先看 can_move_to，再去流程可用性里找那一步 */
  const move = async (card: BoardCardData, from: number, to: number) => {
    if (from === to || !local) return;
    const target = columns[to];
    const fromTitle = columns[from].state.title, toTitle = target.state.title;
    const byType = !!local.type;
    setBusy(true);
    try {
      const wf = await api.tasks.workflow(card.id);
      const candidates = wf.transitions.filter((tr) => (byType ? tr.to === target.state.name : tr.label_to === target.state.name));
      const ok = candidates.find((tr) => tr.available);
      if (!card.can_move_to.includes(target.state.name) || !ok) {
        const reasons = candidates.flatMap((tr) => tr.reasons);
        toast.fail(reasons.length ? [...new Set(reasons)].join(sep()) : t("board.noStep", { from: fromTitle, to: toTitle }));
        setAnnounce(t("board.cancelled"));
        return;
      }
      if (ok.requires.length) {
        setStep({ card, from, to, tr: ok });
        setComment("");
        setResult("");
        setStepError(null);
        return;
      }
      await commit(card, from, to, ok);
      setAnnounce(t("board.dropped", { state: toTitle }));
    } catch (e) {
      toast.fail(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const confirmStep = async (e: FormEvent) => {
    e.preventDefault();
    if (!step) return;
    setBusy(true);
    const ok = await commit(step.card, step.from, step.to, step.tr, { comment: comment || undefined, result: step.tr.requires.includes("result") ? { summary: result } : undefined });
    setBusy(false);
    if (ok) setStep(null);
    else setStepError(t("common.error"));
  };

  // ---------- 指针拖放 ----------
  const { drag, start } = usePointerDrag({
    onDrop: (id, target) => {
      const hit = findCard(id);
      const d = parseDrop(target);
      if (!hit || !d) return;
      const to = columns.findIndex((c) => c.state.name === d.col);
      if (to < 0) return;
      void move(hit.card, hit.col, to);
    },
    onClick: (id) => router.push(`/tasks/${encodeURIComponent(id)}/`),
  });
  const dragging = drag ? findCard(drag.id) : null;
  const over = parseDrop(drag?.over ?? null);

  // ---------- 键盘 ----------
  const onCardKey = (e: ReactKeyboardEvent<HTMLDivElement>, card: BoardCardData, col: number) => {
    const picked = kb?.id === card.id;
    if (e.key === " ") {
      e.preventDefault();
      if (!picked) {
        setKb({ id: card.id, from: col, target: col });
        setAnnounce(t("board.picked", { title: card.title }));
      } else {
        dropKb(card);
      }
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (picked) dropKb(card);
      else router.push(`/tasks/${encodeURIComponent(card.id)}/`);
    } else if (picked && (e.key === "ArrowLeft" || e.key === "ArrowRight")) {
      e.preventDefault();
      const next = Math.max(0, Math.min(columns.length - 1, kb.target + (e.key === "ArrowLeft" ? -1 : 1)));
      setKb({ ...kb, target: next });
      const c = columns[next];
      setAnnounce(allowed(card, kb.from, c, next) ? t("board.kbTarget", { state: c.state.title }) : t("board.kbBlocked", { state: c.state.title }));
    } else if (picked && e.key === "Escape") {
      e.preventDefault();
      setKb(null);
      setAnnounce(t("board.cancelled"));
    }
  };
  const dropKb = (card: BoardCardData) => {
    if (!kb) return;
    const { from, target } = kb;
    setKb(null);
    if (from === target) {
      setAnnounce(t("board.cancelled"));
      return;
    }
    void move(card, from, target);
  };
  useEffect(() => {
    const id = focusReq.current;
    if (!id) return;
    focusReq.current = null;
    ref.current?.querySelector<HTMLElement>(`[data-card-id="${CSS.escape(id)}"]`)?.focus();
  }, [local]);

  const active = dragging ?? (kb ? findCard(kb.id) : null);
  const n = columns.length;

  if (res.error && !local) return <ErrorBox message={res.error} onRetry={res.reload} className={className} />;
  if (!local) {
    return (
      <div ref={ref} className={cx("bd", className)} data-density={density} aria-busy="true" style={{ ["--bd-n" as string]: 4 }}>
        <div className="bd-grid">
          {Array.from({ length: 4 }, (_, i) => (
            <div key={`h${i}`} className="bd-head"><Skeleton className="h-3 w-20" /></div>
          ))}
          {Array.from({ length: 4 }, (_, i) => (
            <div key={`c${i}`} className="bd-cell" data-tail="">
              {Array.from({ length: 3 - (i % 2) }, (_, j) => (
                <div key={j} className="bd-card" style={{ cursor: "default" }}>
                  <Skeleton className="h-3 w-4/5" />
                  <Skeleton className="h-2.5 w-2/5" />
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    );
  }

  const renderCell = (col: BoardColumn, colIdx: number, laneKey: string, tail: boolean) => {
    const cards = lanes ? col.cards.filter((c) => laneOf(c, query.lane) === laneKey) : col.cards;
    const blocked = !!active && !allowed(active.card, active.col, col, colIdx);
    const isOver = !!over && over.col === col.state.name && (!lanes || over.lane === laneKey);
    const activeLane = active ? laneOf(active.card, query.lane) : null;
    const kbTarget = !!kb && kb.target === colIdx && (!lanes || activeLane === laneKey);
    return (
      <div
        key={dropKey(col.state.name, laneKey)}
        className="bd-cell"
        data-drop={dropKey(col.state.name, laneKey)}
        data-blocked={blocked ? "" : undefined}
        data-over={isOver && !blocked ? "" : undefined}
        data-kb-target={kbTarget ? "" : undefined}
        data-last={colIdx === n - 1 ? "" : undefined}
        data-tail={tail ? "" : undefined}
        role="group"
        aria-label={col.state.title}
      >
        {cards.map((card) => (
          <Card
            key={card.id}
            card={card}
            density={density}
            currency={currency}
            lifting={drag?.id === card.id}
            picked={kb?.id === card.id}
            pending={pending.has(card.id)}
            landed={landed === card.id}
            onPointerDown={(e) => start(e, card.id)}
            onKeyDown={(e) => onCardKey(e, card, colIdx)}
          />
        ))}
        {cards.length === 0 && <div className="bd-empty">{isOver && !blocked ? t("board.dropHere") : t("board.emptyColumn")}</div>}
      </div>
    );
  };

  return (
    <div ref={ref} className={cx("bd", className)} data-density={density} data-dragging={drag ? "" : undefined} style={{ ["--bd-n" as string]: n }}>
      <div className="bd-grid" role="list" aria-label={t("board.title")}>
        {columns.map((col) => {
          const overBy = col.wip_limit != null ? col.count - col.wip_limit : 0;
          return (
            <div key={col.state.name} className="bd-head" data-over-limit={col.over_limit ? "" : undefined} role="listitem">
              <StatusLED tone={col.state.label === "active" ? "accent" : col.state.label === "waiting" ? "warning" : col.state.label === "terminal_success" ? "success" : col.state.label === "terminal_failure" ? "dark" : "neutral"} className={col.state.label === "waiting" ? "led-waiting" : undefined} />
              <span className="bd-head-title" title={col.state.title}>{col.state.title}</span>
              {col.over_limit && overBy > 0 && <span className="bd-over">{t("board.over", { n: overBy })}</span>}
              <span className="bd-head-count" title={col.wip_limit != null ? `${t("board.wip")} ${col.wip_limit}` : undefined}>
                {col.wip_limit != null ? t("board.wipReadout", { count: col.count, limit: col.wip_limit }) : col.count}
              </span>
            </div>
          );
        })}
        {lanes
          ? lanes.map((lane, li) => [
              <div key={`lane-${lane.key}`} className="bd-lane">
                <span className="bd-lane-title">{lane.title}</span>
                <span>{t("board.cards", { n: columns.reduce((s, c) => s + c.cards.filter((x) => laneOf(x, query.lane) === lane.key).length, 0) })}</span>
              </div>,
              ...columns.map((col, ci) => renderCell(col, ci, lane.key, li === lanes.length - 1)),
            ])
          : columns.map((col, ci) => renderCell(col, ci, LANE_NONE, true))}
      </div>

      {drag && dragging && (
        <div className="bd-ghost" style={{ width: drag.rect.w, transform: `translate(${drag.rect.x + drag.dx}px, ${drag.rect.y + drag.dy}px)` }} aria-hidden="true">
          <div className="bd" data-density={density} style={{ height: "auto", minHeight: 0, border: 0, background: "transparent", boxShadow: "none", overflow: "visible" }}>
            <Card card={dragging.card} density={density} currency={currency} ghost />
          </div>
        </div>
      )}
      <div className="sr-only" aria-live="polite">{announce}</div>

      {/* 需要评论 / 结果的步骤：与任务详情页同一个小表单 */}
      <Dialog open={!!step} onClose={() => setStep(null)} title={step?.tr.title ?? ""} footer={<><Button onClick={() => setStep(null)}>{t("common.cancel")}</Button><Button variant="primary" form="board-step-form" type="submit" disabled={busy}>{t("common.confirm")}</Button></>}>
        <form id="board-step-form" onSubmit={confirmStep} className="space-y-3">
          {step && <p className="text-caption text-ink-subtle">{step.card.title} · {columns[step.from]?.state.title} → {columns[step.to]?.state.title}</p>}
          {step?.tr.requires.includes("comment") && <Field label={t("task.commentRequired")}><Textarea value={comment} onChange={(e) => setComment(e.target.value)} required autoFocus /></Field>}
          {step?.tr.requires.includes("result") && <Field label={t("task.resultRequired")}><Textarea value={result} onChange={(e) => setResult(e.target.value)} required /></Field>}
          {stepError && <p className="text-caption text-danger" role="alert">{stepError}</p>}
        </form>
      </Dialog>
    </div>
  );
}

function Card({ card, density, currency, lifting, picked, pending, landed, ghost, onPointerDown, onKeyDown }: { card: BoardCardData; density: BoardDensity; currency?: string; lifting?: boolean; picked?: boolean; pending?: boolean; landed?: boolean; ghost?: boolean; onPointerDown?: (e: ReactPointerEvent<HTMLDivElement>) => void; onKeyDown?: (e: ReactKeyboardEvent<HTMLDivElement>) => void }) {
  const overdue = isOverdue(card);
  const compact = density === "compact";
  return (
    <div
      className="bd-card"
      data-card-id={ghost ? undefined : card.id}
      data-lifting={lifting ? "" : undefined}
      data-picked={picked ? "" : undefined}
      data-pending={pending ? "" : undefined}
      data-landed={landed ? "" : undefined}
      tabIndex={ghost ? -1 : 0}
      role={ghost ? undefined : "button"}
      aria-label={ghost ? undefined : `${card.title} · ${card.state.title}`}
      aria-pressed={ghost ? undefined : !!picked}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
    >
      <div className="bd-card-title">{card.title}</div>
      <div className="bd-card-row">
        {compact && card.assignee && <Avatar name={card.assignee.name} kind={card.assignee.kind} size={16} />}
        <TypeLabel type={card.type} title={card.type_title} tag />
        {card.priority !== "normal" && <PriorityTag priority={card.priority} />}
        <PointsChip value={card.points} />
        <span className="grow" />
        {compact ? (
          card.planned_end && (
            <span className="bd-card-date" data-overdue={overdue ? "" : undefined}>
              {overdue && <StatusLED tone="danger" className="!h-1.5 !w-1.5" />}
              {fmtDate(card.planned_end)}
            </span>
          )
        ) : (
          overdue && <OverdueTag>{t("board.overdue")}</OverdueTag>
        )}
      </div>
      {!compact && (
        <div className="bd-card-row" data-secondary="">
          {card.assignee ? (
            <>
              <Avatar name={card.assignee.name} kind={card.assignee.kind} size={18} />
              <span className="bd-card-name">{card.assignee.name}</span>
            </>
          ) : (
            <span className="bd-card-name text-ink-tertiary">{t("taskTable.unclaimed")}</span>
          )}
          <span className="grow" />
          {card.planned_end && (
            <span className="bd-card-date" data-overdue={overdue ? "" : undefined}>
              {overdue && <StatusLED tone="danger" className="!h-1.5 !w-1.5" />}
              {fmtDate(card.planned_end)}
            </span>
          )}
          {card.cost > 0 && <span className="bd-card-cost">{fmtMoney(card.cost, currency)}</span>}
        </div>
      )}
    </div>
  );
}
