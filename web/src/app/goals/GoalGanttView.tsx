"use client";
import Link from "next/link";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { api, type Goal, type ISODate, type Milestone } from "@/lib/api";
import { addDays, fmtDate, parseDate, toISODate, today } from "@/lib/format";
import { errorMessage, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { usePersisted } from "@/lib/usePersisted";
import { useToast } from "@/components/toast";
import { alpha, BAR_H, C, GanttFrame, LABEL_MAX, LABEL_MIN, LABEL_NARROW, MONO, ROW_H, textW, type Density, type GanttHandle } from "@/components/gantt/GanttFrame";
import { GanttNavControls, GanttZoomControls } from "@/components/gantt/GanttControls";
import { MilestoneMarks, MilestoneSwatch, milestoneTipParts, type MilestoneInteraction } from "@/components/gantt/MilestoneMarks";
import { xOfTime } from "@/components/gantt/scales";
import { useGanttScale } from "@/components/gantt/useGanttScale";
import { useGanttShell } from "@/components/gantt/useGanttShell";
import { IconCheck, IconChevronDown, IconChevronRight, IconEdit, IconGoal, IconPlus, IconTrash } from "@/components/icons";
import { Button, ConfirmDialog, DateInput, Empty, ErrorBox, Input, Segmented, Skeleton, Tag, cx } from "@/components/ui";

/*
 * 「目标 · 甘特图」（DESIGN.md §11、ADR 0016）：读与树同一批目标（同一个范围），只负责"行是什么"——
 *   - 每个目标一行：横条 = 计划起止（没有就退到实际起止；都没有 → 左列一枚等宽「未排期」、不画条），进度按汇总百分比填充，
 *     截止日是横条外的一道短刻度；已放弃灰色虚线；已达成 success 色。子目标嵌套缩进、可折叠（折叠 id 记在本地），折叠后父行显示跨整个子树的汇总条。
 *   - 里程碑 = 菱形（gantt/MilestoneMarks，与任务甘特图的目标汇总行同一个渲染器）：悬停提示、点击小气泡（确认 / 撤销 / 编辑 / 删除）、拖动改日期。
 *   - 拖横条两端改计划起止；在目标行空白处双击 → 内联输入名称新建里程碑，日期取双击处。
 *   所有改动乐观更新，失败回滚并用后端的整句理由提示。刻度、表头、今天线、缩放、适应全部、全屏都来自 gantt/GanttFrame + useGanttScale + useGanttShell。
 */

const asNumber = (raw: unknown) => (typeof raw === "number" && Number.isFinite(raw) ? raw : undefined);
const asBool = (raw: unknown) => (typeof raw === "boolean" ? raw : undefined);
const asDensity = (raw: unknown) => (raw === "compact" || raw === "comfortable" ? raw : undefined);
const asKeys = (raw: unknown) => (Array.isArray(raw) && raw.every((x) => typeof x === "string") ? (raw as string[]) : undefined);
const EMPTY_KEYS: string[] = [];
const INDENT = 16;
const CLICK_PX = 3;

type Span = [number, number];
/** 目标横条的三种语气：进行中 = accent，已达成 = success，已放弃 = 灰色虚线 */
export const GOAL_FILL = {
  active: { bg: alpha("accent", 14), border: alpha("accent", 60), done: alpha("accent", 55), text: C.ink },
  achieved: { bg: alpha("success", 14), border: alpha("success", 60), done: alpha("success", 55), text: C.success },
  abandoned: { bg: alpha("neutral", 8), border: alpha("neutral", 45), done: "transparent", text: C.subtle },
  summary: { bg: alpha("neutral", 14), border: C.hairlineTertiary, done: alpha("accent", 55), text: C.telemetry },
};

/** 乐观更新的覆盖层：只盖计划起止与里程碑，其余仍以服务端为准 */
interface Over {
  planned_start?: ISODate | null;
  planned_end?: ISODate | null;
  milestones?: Milestone[];
}
interface Drag {
  kind: "start" | "end" | "ms";
  goal: Goal;
  ms?: Milestone;
  anchor: Element;
  left: number;
  x0: number;
  moved: boolean;
}
type Preview = { kind: "bar"; goalId: string; planned_start: ISODate | null; planned_end: ISODate | null } | { kind: "ms"; goalId: string; id: string; x: number; due_on: ISODate };
interface Creating {
  goalId: string;
  x: number;
  due_on: ISODate;
}
interface Pop {
  goalId: string;
  msId: string;
  rect: DOMRect;
  editing: boolean;
}

const sortMs = (ms: Milestone[]) => [...ms].sort((a, b) => (a.due_on < b.due_on ? -1 : a.due_on > b.due_on ? 1 : 0));
const statusFor = (m: Milestone, due: ISODate): Milestone["status"] => (m.reached_at ? "reached" : due < toISODate(today()) ? "overdue" : "upcoming");
/** 子树（含自己与里程碑）的最早开始与最晚结束，给折叠后的汇总条 */
function subtreeRange(g: Goal): { start: ISODate | null; end: ISODate | null } {
  let start = g.planned_start ?? g.actual_start ?? null;
  let end = g.planned_end ?? g.actual_end ?? null;
  const take = (a: ISODate | null | undefined, b: ISODate | null | undefined) => {
    if (a && (!start || a < start)) start = a;
    if (b && (!end || b > end)) end = b;
  };
  for (const k of g.children ?? []) {
    const r = subtreeRange(k);
    take(r.start, r.end);
  }
  for (const m of g.milestones ?? []) take(m.due_on, m.due_on);
  return { start, end };
}

export function GoalGanttView({ reloadKey, highlight, onNew }: { reloadKey: number; highlight: string | null; onNew: () => void }) {
  const toast = useToast();
  const goals = useLoad(() => api.goals.list(), [reloadKey]);
  const chart = useRef<GanttHandle>(null);
  const g = useGanttScale(chart, "day");
  const shellRef = useRef<HTMLDivElement>(null);
  const shell = useGanttShell(shellRef, [goals.data]);
  const scrollRef = useRef<HTMLDivElement>(null);

  // 与任务甘特图共用的界面偏好（左列宽、收窄、行高、图例）；折叠的目标另记一份
  const [labelW, setLabelW] = usePersisted<number>("axiomos.gantt.labelWidth", 260, asNumber);
  const [narrow, setNarrow] = usePersisted<boolean>("axiomos.gantt.narrow", false, asBool);
  const [density, setDensity] = usePersisted<Density>("axiomos.gantt.density", "comfortable", asDensity);
  const [legendOpen, setLegendOpen] = usePersisted<boolean>("axiomos.gantt.legend", true, asBool);
  const [collapsedKeys, setCollapsedKeys] = usePersisted<string[]>("axiomos.goals.gantt.collapsed", EMPTY_KEYS, asKeys);
  const collapsed = useMemo(() => new Set(collapsedKeys), [collapsedKeys]);
  const toggle = useCallback((id: string) => setCollapsedKeys((prev) => (prev.includes(id) ? prev.filter((k) => k !== id) : [...prev, id])), [setCollapsedKeys]);

  // ---------- 乐观更新：覆盖层跟着服务端数据的身份走，换一批数据就清空（渲染期派生状态） ----------
  const [ov, setOv] = useState<{ base: Goal[] | null; map: Record<string, Over> }>({ base: null, map: {} });
  if (ov.base !== goals.data) setOv({ base: goals.data, map: {} });
  const patch = useCallback((id: string, over: Over) => setOv((s) => ({ ...s, map: { ...s.map, [id]: { ...s.map[id], ...over } } })), []);
  const tree = useMemo(() => {
    const apply = (x: Goal): Goal => ({ ...x, ...(ov.map[x.id] ?? {}), children: (x.children ?? []).map(apply) });
    return (goals.data ?? []).map(apply);
  }, [goals.data, ov.map]);
  const flat = useMemo(() => {
    const out: Array<{ goal: Goal; depth: number }> = [];
    const walk = (x: Goal, d: number) => {
      out.push({ goal: x, depth: d });
      if (!collapsed.has(x.id)) (x.children ?? []).forEach((k) => walk(k, d + 1));
    };
    tree.forEach((x) => walk(x, 0));
    return out;
  }, [tree, collapsed]);
  const all = useMemo(() => {
    const out: Goal[] = [];
    const walk = (x: Goal) => {
      out.push(x);
      (x.children ?? []).forEach(walk);
    };
    tree.forEach(walk);
    return out;
  }, [tree]);
  const byId = useMemo(() => new Map(all.map((x) => [x.id, x])), [all]);

  const rowH = ROW_H[density];
  const barH = BAR_H[density];
  const width = Math.max(1, g.days * g.dayW);
  const xOf = useCallback(
    (d: string | null | undefined) => {
      const dt = parseDate(d);
      return dt ? xOfTime(g.from, dt.getTime(), g.dayW) : null;
    },
    [g.from, g.dayW],
  );
  const span = useCallback(
    (s: string | null | undefined, e: string | null | undefined): Span | null => {
      const x1 = xOf(s);
      const x2 = xOf(e);
      if (x1 === null && x2 === null) return null;
      const a = x1 ?? x2!;
      const b = (x2 ?? x1!) + g.dayW;
      return [Math.max(0, Math.min(a, b)), Math.min(width, Math.max(a, b))];
    },
    [xOf, g.dayW, width],
  );
  const dateAtX = useCallback((x: number) => toISODate(addDays(g.from, Math.max(0, Math.min(g.days - 1, Math.floor(x / g.dayW))))), [g.from, g.days, g.dayW]);

  // ---------- 写操作（乐观 + 回滚 + 后端整句） ----------
  const setMs = useCallback((goalId: string, ms: Milestone[]) => patch(goalId, { milestones: sortMs(ms) }), [patch]);
  const msOf = useCallback((goalId: string) => byId.get(goalId)?.milestones ?? [], [byId]);
  const saveDates = useCallback(
    async (goal: Goal, planned_start: ISODate | null, planned_end: ISODate | null) => {
      const prev = { planned_start: goal.planned_start, planned_end: goal.planned_end };
      patch(goal.id, { planned_start, planned_end });
      try {
        const res = await api.goals.update(goal.id, { planned_start, planned_end });
        patch(goal.id, { planned_start: res.planned_start, planned_end: res.planned_end });
        toast.ok(t("goals.gantt.planSaved", { title: goal.title, start: fmtDate(res.planned_start), end: fmtDate(res.planned_end) }));
      } catch (e) {
        patch(goal.id, prev);
        toast.fail(errorMessage(e));
      }
    },
    [patch, toast],
  );
  const replaceMs = useCallback((goalId: string, next: Milestone) => setMs(goalId, msOf(goalId).map((m) => (m.id === next.id ? next : m))), [setMs, msOf]);
  const mutateMs = useCallback(
    async (goalId: string, m: Milestone, optimistic: Milestone, call: () => Promise<Milestone>, okText: string) => {
      const before = msOf(goalId);
      setMs(goalId, before.map((x) => (x.id === m.id ? optimistic : x)));
      try {
        const res = await call();
        replaceMs(goalId, res);
        toast.ok(okText);
        return true;
      } catch (e) {
        setMs(goalId, before);
        toast.fail(errorMessage(e));
        return false;
      }
    },
    [msOf, setMs, replaceMs, toast],
  );
  const moveMs = (goalId: string, m: Milestone, due_on: ISODate) => mutateMs(goalId, m, { ...m, due_on, status: statusFor(m, due_on) }, () => api.milestones.update(m.id, { due_on }), t("ms.movedToast", { title: m.title, date: fmtDate(due_on) }));
  const reachMs = (goalId: string, m: Milestone) => mutateMs(goalId, m, { ...m, reached_at: new Date().toISOString(), status: "reached", ready_hint: false }, () => api.milestones.reach(m.id), t("ms.reachedToast", { title: m.title }));
  const unreachMs = (goalId: string, m: Milestone) => mutateMs(goalId, m, { ...m, reached_at: null, status: statusFor({ ...m, reached_at: null }, m.due_on) }, () => api.milestones.unreach(m.id), t("ms.unreachedToast", { title: m.title }));
  const editMs = (goalId: string, m: Milestone, input: { title: string; due_on: ISODate; description: string }) => mutateMs(goalId, m, { ...m, ...input, status: statusFor(m, input.due_on) }, () => api.milestones.update(m.id, input), t("ms.updatedToast", { title: input.title }));
  const deleteMs = async (goalId: string, m: Milestone) => {
    const before = msOf(goalId);
    setMs(goalId, before.filter((x) => x.id !== m.id));
    try {
      await api.milestones.remove(m.id);
      toast.ok(t("ms.deletedToast", { title: m.title }));
      return true;
    } catch (e) {
      setMs(goalId, before);
      toast.fail(errorMessage(e));
      return false;
    }
  };
  const createMs = async (goal: Goal, title: string, due_on: ISODate) => {
    try {
      const res = await api.milestones.create(goal.id, { title, due_on });
      setMs(goal.id, [...msOf(goal.id), res]);
      toast.ok(t("ms.createdToast", { title: res.title, date: fmtDate(res.due_on) }));
    } catch (e) {
      toast.fail(errorMessage(e));
    }
  };

  // ---------- 悬停提示 / 小气泡 / 内联新建 / 删除确认 ----------
  const [tip, setTip] = useState<{ m: Milestone; rect: DOMRect } | null>(null);
  const [pop, setPop] = useState<Pop | null>(null);
  const [creating, setCreating] = useState<Creating | null>(null);
  const [deleting, setDeleting] = useState<{ goalId: string; m: Milestone } | null>(null);
  // 气泡跟着最新的里程碑对象走（被删掉就不再显示）
  const popM = pop ? byId.get(pop.goalId)?.milestones?.find((m) => m.id === pop.msId) ?? null : null;
  // 换刻度 / 缩放时把浮层与内联输入收掉（它们是 fixed 定位，不跟着重排；渲染期派生，不走 effect）
  const layoutKey = `${g.scale}:${g.dayW}`;
  const [seenLayout, setSeenLayout] = useState(layoutKey);
  if (seenLayout !== layoutKey) {
    setSeenLayout(layoutKey);
    setTip(null);
    setPop(null);
    setCreating(null);
  }
  // 滚动时同样收掉
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const close = () => {
      setTip(null);
      setPop(null);
    };
    el.addEventListener("scroll", close, { passive: true });
    return () => el.removeEventListener("scroll", close);
  }, [goals.data]);

  // ---------- 拖动：横条两端 / 菱形（window 级监听，不依赖 pointer capture） ----------
  // 抬起时要用"当下"的写操作（它们随数据变化重建），走 ref 取最新的，避免闭包里的旧数据把刚建的里程碑覆盖掉
  const opsRef = useRef({ moveMs, saveDates });
  useEffect(() => {
    opsRef.current = { moveMs, saveDates };
  });
  const dragRef = useRef<Drag | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const startDrag = useCallback(
    (e: ReactPointerEvent<Element>, init: Omit<Drag, "left" | "x0" | "moved" | "anchor">) => {
      if (e.button !== 0) return;
      e.preventDefault();
      e.stopPropagation();
      const svg = (e.currentTarget as Element).closest("svg");
      if (!svg) return;
      const rect = svg.getBoundingClientRect();
      const d: Drag = { ...init, anchor: e.currentTarget as Element, left: rect.left, x0: e.clientX - rect.left, moved: false };
      dragRef.current = d;
      setTip(null);
      const dd = (clientX: number) => Math.round((clientX - d.left - d.x0) / g.dayW);
      const onMove = (ev: PointerEvent) => {
        const cur = dragRef.current;
        if (!cur) return;
        if (!cur.moved && Math.abs(ev.clientX - cur.left - cur.x0) < CLICK_PX) return;
        cur.moved = true;
        const n = dd(ev.clientX);
        if (cur.kind === "ms" && cur.ms) {
          const due = toISODate(addDays(parseDate(cur.ms.due_on)!, n));
          setPreview({ kind: "ms", goalId: cur.goal.id, id: cur.ms.id, x: (xOf(due) ?? 0) + g.dayW / 2, due_on: due });
        } else {
          const s = cur.goal.planned_start ?? cur.goal.actual_start ?? null;
          const en = cur.goal.planned_end ?? cur.goal.actual_end ?? null;
          let ns = s, ne = en;
          if (cur.kind === "start" && s) ns = toISODate(addDays(parseDate(s)!, n));
          if (cur.kind === "end" && en) ne = toISODate(addDays(parseDate(en)!, n));
          if (ns && ne && ns > ne) {
            if (cur.kind === "start") ns = ne;
            else ne = ns;
          }
          setPreview({ kind: "bar", goalId: cur.goal.id, planned_start: ns, planned_end: ne });
        }
      };
      const onUp = (ev: PointerEvent) => {
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
        window.removeEventListener("pointercancel", onUp);
        const cur = dragRef.current;
        dragRef.current = null;
        setPreview((p) => {
          if (cur && cur.moved && p) {
            if (p.kind === "ms" && cur.ms && p.due_on !== cur.ms.due_on) void opsRef.current.moveMs(cur.goal.id, cur.ms, p.due_on);
            if (p.kind === "bar" && (p.planned_start !== cur.goal.planned_start || p.planned_end !== cur.goal.planned_end)) void opsRef.current.saveDates(cur.goal, p.planned_start, p.planned_end);
          }
          return null;
        });
        if (cur && !cur.moved && cur.kind === "ms" && cur.ms) {
          // 没拖动 = 点击：打开小气泡
          setPop({ goalId: cur.goal.id, msId: cur.ms.id, rect: cur.anchor.getBoundingClientRect(), editing: false });
        }
        void ev;
      };
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
      window.addEventListener("pointercancel", onUp);
    },
    [g.dayW, xOf],
  );

  const msInteraction = useMemo<MilestoneInteraction<Milestone>>(
    () => ({
      onEnter: (m, el) => {
        if (dragRef.current) return;
        setTip({ m, rect: el.getBoundingClientRect() });
      },
      onLeave: () => setTip(null),
      onPointerDown: (e, m) => {
        const goal = byId.get(m.goal_id);
        if (!goal) return;
        setPop(null);
        startDrag(e, { kind: "ms", goal, ms: m });
      },
      activeId: pop?.msId ?? null,
      drag: preview?.kind === "ms" ? { id: preview.id, x: preview.x } : null,
    }),
    [byId, startDrag, pop?.msId, preview],
  );
  const onBarDown = useCallback((e: ReactPointerEvent<SVGRectElement>, goal: Goal, kind: "start" | "end") => startDrag(e, { kind, goal }), [startDrag]);
  const onRowDblClick = useCallback(
    (e: ReactMouseEvent<SVGSVGElement>, goal: Goal) => {
      if ((e.target as Element).closest(".gantt-ms, .gantt-handle")) return;
      const rect = e.currentTarget.getBoundingClientRect();
      const x = e.clientX - rect.left;
      setPop(null);
      setCreating({ goalId: goal.id, x, due_on: dateAtX(x) });
    },
    [dateAtX],
  );

  // ---------- 工具栏 ----------
  const fitAll = () => g.fitTo([...all.flatMap((x) => [x.planned_start, x.planned_end, x.actual_start, x.actual_end, x.deadline]), ...all.flatMap((x) => (x.milestones ?? []).map((m) => m.due_on))]);
  const collapseAll = () => setCollapsedKeys(all.filter((x) => (x.children ?? []).length > 0).map((x) => x.id));
  const expandAll = () => setCollapsedKeys([]);
  const total = all.length;
  const corner = (
    <>
      <span className="eyebrow truncate text-ink-subtle">{t("goals.gantt.goalCount", { n: total })}</span>
      <span className="ml-auto font-mono text-[10px] text-ink-tertiary">{t("gantt.rowCount", { n: flat.length })}</span>
    </>
  );
  const effLabelW = narrow ? LABEL_NARROW : Math.min(LABEL_MAX, Math.max(LABEL_MIN, labelW));

  // ---------- 行：递归，子级放在 .fold 里（与目标树同一条高度过渡） ----------
  const renderNode = (x: Goal, depth: number): ReactNode => {
    const kids = x.children ?? [];
    const open = !collapsed.has(x.id);
    return (
      <div key={x.id} className="gantt-group">
        <GoalRow
          goal={x}
          depth={depth}
          open={open}
          hasKids={kids.length > 0}
          rowH={rowH}
          barH={barH}
          width={width}
          labelW={effLabelW}
          narrow={narrow}
          dayW={g.dayW}
          xOf={xOf}
          span={span}
          onToggle={toggle}
          highlight={highlight}
          ms={msInteraction}
          preview={preview?.goalId === x.id && preview.kind === "bar" ? preview : null}
          dragging={dragRef.current?.goal.id === x.id && preview?.kind === "bar"}
          onBarDown={onBarDown}
          onDblClick={onRowDblClick}
          creating={creating?.goalId === x.id ? creating : null}
          onCreate={(title) => {
            setCreating(null);
            void createMs(x, title, creating!.due_on);
          }}
          onCancelCreate={() => setCreating(null)}
        />
        {kids.length > 0 && (
          <div className="fold" data-closed={open ? undefined : ""} inert={!open} aria-hidden={!open}>
            <div>{kids.map((k) => renderNode(k, depth + 1))}</div>
          </div>
        )}
      </div>
    );
  };
  const renderRows = () => tree.map((x) => renderNode(x, 0));

  return (
    <div className="gantt-page">
      <div ref={shellRef} className={cx("gantt-shell", shell.inApp && "gantt-shell-fs")} style={shell.style}>
        <div className="gantt-toolbar">
          <GanttZoomControls g={g} onFit={fitAll} canFit={total > 0} />
          {goals.data && <span className="font-mono text-[11px] text-ink-subtle">{t("goals.gantt.goalCount", { n: total })}</span>}
          <span className="gantt-btns ml-auto">
            <Button size="sm" variant="ghost" onClick={collapseAll} disabled={!total}>{t("gantt.collapseAll")}</Button>
            <Button size="sm" variant="ghost" onClick={expandAll} disabled={!total}>{t("gantt.expandAll")}</Button>
            <Segmented size="sm" value={density} options={[["compact", t("gantt.density.compact")], ["comfortable", t("gantt.density.comfortable")]]} onChange={setDensity} aria-label={t("gantt.density")} />
            <span className="gantt-sep" aria-hidden="true" />
            <GanttNavControls g={g} fs={shell.fs} />
          </span>
        </div>

        {goals.loading && !goals.data ? (
          <div className="gantt-frame rounded-lg border border-hairline bg-surface-1 p-4 shadow-panel" aria-busy="true">
            <Skeleton className="mb-4 h-2.5 w-1/3" />
            {Array.from({ length: 6 }, (_, i) => (
              <div key={i} className="mb-3 flex items-center gap-4">
                <Skeleton className="w-52" />
                <Skeleton className="h-4" style={{ width: `${20 + ((i * 17) % 40)}%`, marginLeft: `${(i * 11) % 30}%` }} />
              </div>
            ))}
          </div>
        ) : goals.error ? (
          <ErrorBox message={goals.error} onRetry={goals.reload} className="gantt-frame" />
        ) : total === 0 ? (
          <div className="gantt-frame rounded-lg border border-hairline bg-surface-1 shadow-panel">
            <Empty text={t("goals.empty")} action={<Button variant="primary" icon={<IconPlus />} onClick={onNew}>{t("goals.new")}</Button>} />
          </div>
        ) : (
          <div className="gantt-frame hud rounded-lg border border-hairline bg-surface-1 shadow-panel">
            <GanttFrame ref={chart} from={g.from} days={g.days} dayW={g.dayW} scale={g.scale} onZoom={g.onZoom} labelW={labelW} narrow={narrow} onLabelW={setLabelW} onNarrow={setNarrow} density={density} bodyH={flat.length * rowH} scrollRef={scrollRef} corner={corner} repaintKey={goals.data}>
              {renderRows}
            </GanttFrame>
          </div>
        )}

        {/* 图例 + 操作提示 */}
        <div className="gantt-legend" data-open={legendOpen ? "" : undefined}>
          <button type="button" className="gantt-legend-toggle pressable" onClick={() => setLegendOpen(!legendOpen)} aria-expanded={legendOpen}>
            <IconChevronDown className={cx("chevron", !legendOpen && "-rotate-90")} size={12} />
            {legendOpen ? t("gantt.legend.hide") : t("gantt.legend.show")}
          </button>
          {legendOpen && (
            <div className="gantt-legend-items">
              <span className="inline-flex items-center gap-1.5">
                <i className="relative inline-block h-2.5 w-6 overflow-hidden rounded-xs border" style={{ background: GOAL_FILL.active.bg, borderColor: GOAL_FILL.active.border }}><i className="absolute inset-y-0 left-0 w-1/2" style={{ background: GOAL_FILL.active.done }} /></i>
                {t("goals.gantt.legend.bar")}
              </span>
              <span className="inline-flex items-center gap-1.5"><i className="inline-block h-2.5 w-5 rounded-xs border" style={{ background: GOAL_FILL.achieved.bg, borderColor: GOAL_FILL.achieved.border }} />{t("goals.gantt.legend.achieved")}</span>
              <span className="inline-flex items-center gap-1.5"><i className="inline-block h-2.5 w-5 rounded-xs border border-dashed" style={{ background: GOAL_FILL.abandoned.bg, borderColor: GOAL_FILL.abandoned.border }} />{t("goals.gantt.legend.abandoned")}</span>
              <span className="inline-flex items-center gap-1.5"><i className="inline-block h-3 w-[1.5px] bg-ink-muted" />{t("goals.gantt.legend.deadline")}</span>
              <span className="inline-flex items-center gap-1.5">
                <i className="relative inline-block h-2.5 w-6 overflow-hidden rounded-xs border" style={{ background: GOAL_FILL.summary.bg, borderColor: GOAL_FILL.summary.border }}><i className="absolute inset-y-0 left-0 w-1/2" style={{ background: GOAL_FILL.summary.done }} /></i>
                {t("goals.gantt.legend.summary")}
              </span>
              <span className="inline-flex items-center gap-1.5">
                <MilestoneSwatch status="upcoming" />
                <MilestoneSwatch status="reached" />
                <MilestoneSwatch status="overdue" />
                {t("ms.legend")}
              </span>
              <span className="inline-flex items-center gap-1.5"><i className="inline-block h-3 w-0.5 bg-accent" />{t("gantt.today")}</span>
              <span className="text-ink-tertiary">{t("goals.gantt.hint")}</span>
            </div>
          )}
        </div>
      </div>

      {/* 里程碑悬停提示：名称 · 日期 · 状态（+ 可以确认了） */}
      {tip && !pop && !preview && (
        <Floating rect={tip.rect} place="top" className="gantt-ms-tip" role="tooltip">
          <MilestoneTipBody m={tip.m} />
        </Floating>
      )}
      {/* 点击菱形的小气泡：确认已达到 / 撤销 / 编辑 / 删除 */}
      {pop && popM && (
        <MilestonePopover
          key={pop.msId}
          m={popM}
          rect={pop.rect}
          editing={pop.editing}
          onEdit={(on) => setPop({ ...pop, editing: on })}
          onClose={() => setPop(null)}
          onReach={() => void reachMs(pop.goalId, popM).then((ok) => ok && setPop(null))}
          onUnreach={() => void unreachMs(pop.goalId, popM).then((ok) => ok && setPop(null))}
          onSave={(input) => void editMs(pop.goalId, popM, input).then((ok) => ok && setPop(null))}
          onDelete={() => {
            setDeleting({ goalId: pop.goalId, m: popM });
            setPop(null);
          }}
        />
      )}
      <ConfirmDialog open={!!deleting} title={t("ms.deleteTitle")} message={deleting ? t("ms.deleteMessage", { title: deleting.m.title, date: fmtDate(deleting.m.due_on) }) : undefined} confirmLabel={t("common.delete")} danger onConfirm={() => { const d = deleting; setDeleting(null); if (d) void deleteMs(d.goalId, d.m); }} onClose={() => setDeleting(null)} />
    </div>
  );
}

// ---------- 目标行 ----------
interface GoalRowProps {
  goal: Goal;
  depth: number;
  open: boolean;
  hasKids: boolean;
  rowH: number;
  barH: number;
  width: number;
  labelW: number;
  narrow: boolean;
  dayW: number;
  xOf: (d: string | null | undefined) => number | null;
  span: (s: string | null | undefined, e: string | null | undefined) => Span | null;
  onToggle: (id: string) => void;
  highlight: string | null;
  ms: MilestoneInteraction<Milestone>;
  preview: { planned_start: ISODate | null; planned_end: ISODate | null } | null;
  dragging: boolean;
  onBarDown: (e: ReactPointerEvent<SVGRectElement>, goal: Goal, kind: "start" | "end") => void;
  onDblClick: (e: ReactMouseEvent<SVGSVGElement>, goal: Goal) => void;
  creating: Creating | null;
  onCreate: (title: string) => void;
  onCancelCreate: () => void;
}
function GoalRow({ goal, depth, open, hasKids, rowH, barH, width, labelW, narrow, dayW, xOf, span, onToggle, highlight, ms, preview, dragging, onBarDown, onDblClick, creating, onCreate, onCancelCreate }: GoalRowProps) {
  const cy = rowH / 2;
  const abandoned = goal.status === "abandoned";
  const grown = goal.achieved || goal.progress >= 100;
  const tone = abandoned ? GOAL_FILL.abandoned : goal.achieved ? GOAL_FILL.achieved : GOAL_FILL.active;
  // 自己的横条：计划起止，没有就退到实际起止；拖动中用预览值
  const start = preview ? preview.planned_start : goal.planned_start ?? goal.actual_start;
  const end = preview ? preview.planned_end : goal.planned_end ?? goal.actual_end;
  const own = span(start, end);
  // 折叠后的汇总条：跨越整个子树
  const summary = !open && hasKids ? (() => {
    const r = subtreeRange(goal);
    return span(r.start, r.end);
  })() : null;
  const s = summary ?? own;
  const isSummary = !!summary;
  const ratio = abandoned ? 0 : Math.max(0, Math.min(1, goal.progress / 100));
  const pct = `${Math.round(goal.progress)}%`;
  const fontSize = rowH <= 28 ? 10 : 10.5;
  const dl = goal.deadline ? xOf(goal.deadline) : null;
  const dlX = dl !== null ? dl + dayW : null;
  const late = !!goal.deadline && !goal.achieved && !abandoned && goal.deadline < toISODate(today());
  const unscheduled = !own && !summary;
  return (
    <div className={cx("gantt-row gantt-row-goal", isSummary && "gantt-row-group")} style={{ height: rowH, position: "relative" }} data-dragging={dragging ? "" : undefined}>
      <div className={cx("gantt-label gantt-label-goal", narrow && "gantt-label-narrow", highlight === goal.id && "row-new")} data-depth={depth} style={{ width: labelW, paddingLeft: narrow ? undefined : 6 + depth * INDENT }} title={narrow ? goal.title : undefined}>
        <button type="button" className={cx("gantt-chevron pressable", !hasKids && "invisible")} onClick={() => onToggle(goal.id)} aria-expanded={open} aria-label={open ? t("goals.collapse") : t("goals.expand")} tabIndex={hasKids ? 0 : -1}>
          <IconChevronRight className={cx("chevron", open && "chevron-open")} />
        </button>
        {!narrow && (
          <>
            <span className={cx("inline-flex shrink-0", grown ? "text-success" : abandoned ? "text-ink-tertiary" : "text-ink-subtle")} aria-hidden="true"><IconGoal size={14} stage={grown ? "grown" : "bud"} /></span>
            <Link href={`/goals/${encodeURIComponent(goal.id)}/`} className={cx("gantt-goal-title hover:text-accent-hover", abandoned && "line-through opacity-70")} title={goal.title}>
              {goal.title}
            </Link>
            {goal.achieved && <Tag tone="success">{t("goals.achieved")}</Tag>}
            {abandoned && <Tag>{t("goals.abandonedTag")}</Tag>}
            {unscheduled && <span className="gantt-unscheduled">{t("gantt.unscheduled")}</span>}
          </>
        )}
        <span className="gantt-count">{pct}</span>
      </div>
      <svg width={width} height={rowH} className="gantt-bars" onDoubleClick={(e) => onDblClick(e, goal)} fontFamily="inherit">
        <rect x={0} y={0} width={width} height={rowH} fill={isSummary ? C.ink : "transparent"} opacity={isSummary ? 0.035 : 1} />
        {s && (
          <g>
            <rect x={s[0] + 0.5} y={cy - barH / 2 + 0.5} width={Math.max(2, s[1] - s[0] - 1)} height={barH - 1} rx={2} fill={isSummary ? GOAL_FILL.summary.bg : tone.bg} stroke={isSummary ? GOAL_FILL.summary.border : tone.border} strokeDasharray={abandoned ? "3 2" : undefined} />
            {ratio > 0 && <rect x={s[0] + 1} y={cy - barH / 2 + 1} width={Math.max(1, (s[1] - s[0] - 2) * ratio)} height={barH - 2} rx={1.5} fill={isSummary ? GOAL_FILL.summary.done : tone.done} />}
            {/* 1px 端点：比条高出 2px */}
            <line x1={s[0] + 0.5} y1={cy - barH / 2 - 2} x2={s[0] + 0.5} y2={cy + barH / 2 + 2} stroke={isSummary ? GOAL_FILL.summary.border : tone.border} />
            <line x1={s[1] - 0.5} y1={cy - barH / 2 - 2} x2={s[1] - 0.5} y2={cy + barH / 2 + 2} stroke={isSummary ? GOAL_FILL.summary.border : tone.border} />
            {/* 进度读数：条够宽就贴在条内右端，否则放条外 */}
            {s[1] - s[0] > textW(pct, fontSize) + 14 ? (
              <text x={s[1] - 5} y={cy + 3.5} fontSize={fontSize} fill={isSummary ? GOAL_FILL.summary.text : tone.text} textAnchor="end" style={{ fontFamily: MONO }} pointerEvents="none">{pct}</text>
            ) : (
              <text x={s[1] + 6} y={cy + 3.5} fontSize={fontSize} fill={C.telemetry} style={{ fontFamily: MONO }} pointerEvents="none">{pct}</text>
            )}
            {/* 两端把手：拖动改计划起止（汇总条不可拖） */}
            {!isSummary && !abandoned && (
              <>
                <rect className="gantt-handle" x={s[0] - 3} y={cy - barH / 2 - 3} width={7} height={barH + 6} rx={1.5} onPointerDown={(e) => onBarDown(e, goal, "start")} />
                <rect className="gantt-handle" x={s[1] - 4} y={cy - barH / 2 - 3} width={7} height={barH + 6} rx={1.5} onPointerDown={(e) => onBarDown(e, goal, "end")} />
              </>
            )}
          </g>
        )}
        {/* 截止日：横条外的一道短刻度（在那一天的末尾） */}
        {dlX !== null && dlX >= 0 && dlX <= width && (
          <g pointerEvents="none">
            <line x1={dlX} y1={3} x2={dlX} y2={rowH - 3} stroke={late ? C.danger : C.inkMuted} strokeWidth={1.5} />
            {rowH >= 32 && (
              <text x={dlX + 4} y={11} fontSize={10} fill={late ? C.danger : C.subtle} style={{ fontFamily: MONO }}>
                {t("gantt.deadline")}
              </text>
            )}
          </g>
        )}
        {/* 里程碑：永远画在横条之上 */}
        <MilestoneMarks marks={goal.milestones ?? []} cy={cy} xOfDate={xOf} dayW={dayW} width={width} interactive={ms} />
      </svg>
      {creating && <InlineCreate left={labelW + creating.x} top={(rowH - 26) / 2} due_on={creating.due_on} onSubmit={onCreate} onCancel={onCancelCreate} />}
    </div>
  );
}

/** 双击处的内联输入：名称，回车保存，Esc / 失焦取消 */
function InlineCreate({ left, top, due_on, onSubmit, onCancel }: { left: number; top: number; due_on: ISODate; onSubmit: (title: string) => void; onCancel: () => void }) {
  const [title, setTitle] = useState("");
  const onKey = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && title.trim()) onSubmit(title.trim());
    if (e.key === "Escape") onCancel();
  };
  return (
    <div className="gantt-inline-input" style={{ left: Math.max(0, left - 4), top }}>
      <input value={title} onChange={(e) => setTitle(e.target.value)} onKeyDown={onKey} onBlur={() => !title.trim() && onCancel()} placeholder={t("goals.gantt.newMilestone")} autoFocus aria-label={t("ms.add")} />
      <span className="gantt-inline-date">{due_on}</span>
    </div>
  );
}

function MilestoneTipBody({ m }: { m: Milestone }) {
  const p = milestoneTipParts(m);
  return (
    <>
      <span className="font-medium">{p.title}</span>
      <span className="text-ink-subtle"> · </span>
      <span className="gantt-ms-tip-date">{p.date}</span>
      <span className="text-ink-subtle"> · </span>
      <span className={m.status === "overdue" ? "text-danger" : m.status === "reached" ? "text-success" : "text-ink-muted"}>{p.status}</span>
      {p.ready && <span className="text-success"> · {t("ms.ready")}</span>}
    </>
  );
}

/** 小气泡：状态 + 日期 + 说明 + 四个动作；编辑时换成三个输入 */
function MilestonePopover({ m, rect, editing, onEdit, onClose, onReach, onUnreach, onSave, onDelete }: { m: Milestone; rect: DOMRect; editing: boolean; onEdit: (on: boolean) => void; onClose: () => void; onReach: () => void; onUnreach: () => void; onSave: (input: { title: string; due_on: ISODate; description: string }) => void; onDelete: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const [form, setForm] = useState({ title: m.title, due_on: m.due_on, description: m.description ?? "" });
  useEffect(() => {
    const onDown = (e: PointerEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    // 延一帧再挂：打开它的那一下 pointerup 不算"外面"
    const id = window.setTimeout(() => {
      window.addEventListener("pointerdown", onDown);
      window.addEventListener("keydown", onKey);
    }, 0);
    return () => {
      window.clearTimeout(id);
      window.removeEventListener("pointerdown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [onClose]);
  const p = milestoneTipParts(m);
  return (
    <Floating rect={rect} place="bottom" className="gantt-ms-pop" role="dialog" aria-label={m.title} innerRef={ref}>
      {editing ? (
        <form
          className="space-y-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (form.title.trim() && form.due_on) onSave({ title: form.title.trim(), due_on: form.due_on, description: form.description.trim() });
          }}
        >
          <Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder={t("ms.titlePlaceholder")} autoFocus aria-label={t("ms.title")} />
          <DateInput value={form.due_on} onChange={(e) => setForm({ ...form, due_on: e.target.value })} required aria-label={t("goal.plan")} />
          <Input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} placeholder={t("ms.descriptionPlaceholder")} aria-label={t("ms.descriptionPlaceholder")} />
          <div className="flex justify-end gap-1.5 pt-1">
            <Button size="sm" variant="ghost" onClick={() => onEdit(false)}>{t("common.cancel")}</Button>
            <Button size="sm" variant="primary" type="submit" disabled={!form.title.trim() || !form.due_on}>{t("common.save")}</Button>
          </div>
        </form>
      ) : (
        <>
          <div className="flex items-start gap-2">
            <span className="mt-0.5 inline-flex shrink-0"><MilestoneSwatch status={m.status} /></span>
            <div className="min-w-0 flex-1">
              <div className="truncate text-body font-medium text-ink" title={m.title}>{m.title}</div>
              <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-caption">
                <span className="font-mono text-telemetry">{m.due_on}</span>
                <Tag tone={m.status === "overdue" ? "danger" : m.status === "reached" ? "success" : "neutral"}>{p.status}</Tag>
                {p.ready && <span className="text-success">{t("ms.ready")}</span>}
              </div>
              {m.description && <p className="mt-1 text-caption text-ink-muted">{m.description}</p>}
            </div>
          </div>
          <div className="mt-2.5 flex items-center gap-1.5 border-t border-hairline pt-2.5">
            {m.status === "reached" ? (
              <Button size="sm" onClick={onUnreach}>{t("ms.unreach")}</Button>
            ) : (
              <Button size="sm" variant="primary" icon={<IconCheck />} onClick={onReach}>{t("ms.reach")}</Button>
            )}
            <span className="ml-auto inline-flex gap-1">
              <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={() => onEdit(true)}>{t("common.edit")}</Button>
              <Button size="sm" variant="ghost" icon={<IconTrash />} onClick={onDelete}>{t("common.delete")}</Button>
            </span>
          </div>
        </>
      )}
    </Floating>
  );
}

/** fixed 定位的浮层：贴着锚点上方 / 下方居中，量完自己的尺寸后夹进视口（不被甘特图的框裁掉） */
function Floating({ rect, place, className, children, role, innerRef, "aria-label": ariaLabel }: { rect: DOMRect; place: "top" | "bottom"; className?: string; children: ReactNode; role?: string; innerRef?: React.RefObject<HTMLDivElement | null>; "aria-label"?: string }) {
  const own = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  useLayoutEffect(() => {
    const el = own.current;
    if (!el) return;
    const w = el.offsetWidth;
    const h = el.offsetHeight;
    const pad = 8;
    const left = Math.min(window.innerWidth - pad - w, Math.max(pad, rect.left + rect.width / 2 - w / 2));
    let top = place === "top" ? rect.top - h - 8 : rect.bottom + 8;
    if (place === "top" && top < pad) top = rect.bottom + 8;
    if (place === "bottom" && top + h > window.innerHeight - pad) top = Math.max(pad, rect.top - h - 8);
    setPos({ left, top });
  }, [rect, place]);
  return (
    <div
      ref={(el) => {
        own.current = el;
        if (innerRef) innerRef.current = el;
      }}
      className={cx("gantt-float", className)}
      role={role}
      aria-label={ariaLabel}
      style={pos ? { left: pos.left, top: pos.top } : { left: rect.left, top: rect.top, visibility: "hidden" }}
    >
      {children}
    </div>
  );
}
