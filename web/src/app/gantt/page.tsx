"use client";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, type GanttGroup, type Run } from "@/lib/api";
import { addDays, diffDays, parseDate, toISODate, today } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { markKeyboardIntent } from "@/lib/motion";
import { STATE_LABELS, stateLabel } from "@/lib/terms";
import { useFullscreen } from "@/lib/useFullscreen";
import { usePersisted } from "@/lib/usePersisted";
import { buildGroups, COST_LEGEND, defaultRange, Gantt, LABEL_MAX, LABEL_MIN, RUN_FILL, STATE_FILL, SUMMARY_FILL, UNSCHEDULED_KEY, type CreateDraft, type Density, type GanttHandle } from "@/components/gantt/Gantt";
import { clampDayW, isMacro, NAV_STEP_DAYS, SCALE_DAY_W, scaleFor, SCALES, scaleTitle, SPAN_DAYS, type Scale } from "@/components/gantt/scales";
import { flattenGoals } from "@/components/GoalDrawer";
import { IconChevronDown, IconChevronLeft, IconChevronRight } from "@/components/icons";
import { TaskDrawer, type TaskDrawerDefaults } from "@/components/TaskDrawer";
import { Button, Checkbox, cx, EnergyLine, ErrorBox, Segmented, Skeleton, Tip } from "@/components/ui";

const GROUPS: GanttGroup[] = ["goal", "team", "executor", "type"];
const DAY_MS = 86400_000;
const nowMs = () => Date.now();
const isEditable = (el: EventTarget | null) => el instanceof HTMLElement && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT" || el.isContentEditable);
const asNumber = (raw: unknown) => (typeof raw === "number" && Number.isFinite(raw) ? raw : undefined);
const asBool = (raw: unknown) => (typeof raw === "boolean" ? raw : undefined);
const asDensity = (raw: unknown) => (raw === "compact" || raw === "comfortable" ? raw : undefined);
const asKeys = (raw: unknown) => (Array.isArray(raw) && raw.every((x) => typeof x === "string") ? (raw as string[]) : undefined);

/*
 * 甘特图 v2（DESIGN.md §8）：把空间全给图、任何粒度都能看、能收能放、能全屏。
 * 范围（from / to）完全由前端决定——后端 /gantt 返回全部任务，from / to 只是回显；切档时范围按档位重设并围绕视口中心。
 */
export default function GanttPage() {
  const [group, setGroup] = useState<GanttGroup>("goal");
  const [dayW, setDayW] = useState<number>(SCALE_DAY_W.day);
  const scale = scaleFor(dayW);
  const [showCost, setShowCost] = useState(false);
  const [showOverdue, setShowOverdue] = useState(true);
  const [range, setRange] = useState(defaultRange);
  const [draft, setDraft] = useState<TaskDrawerDefaults | null>(null);

  // 记在本地的界面状态（axiomos.gantt.*）
  const [labelW, setLabelW] = usePersisted<number>("axiomos.gantt.labelWidth", 260, asNumber);
  const [narrow, setNarrow] = usePersisted<boolean>("axiomos.gantt.narrow", false, asBool);
  const [density, setDensity] = usePersisted<Density>("axiomos.gantt.density", "comfortable", asDensity);
  const [legendOpen, setLegendOpen] = usePersisted<boolean>("axiomos.gantt.legend", true, asBool);
  const collapsedDefault = useMemo(() => [UNSCHEDULED_KEY], []);
  const [collapsedKeys, setCollapsedKeys] = usePersisted<string[]>(`axiomos.gantt.collapsed.${group}`, collapsedDefault, asKeys);
  const collapsed = useMemo(() => new Set(collapsedKeys), [collapsedKeys]);

  const data = useLoad(() => api.gantt.get({ group, from: range.from, to: range.to }), [group]);
  const goals = useLoad(() => api.goals.list(), []);
  const types = useLoad(() => api.taskTypes.list(), []);
  const flat = useMemo(() => flattenGoals(goals.data ?? []), [goals.data]);
  const groups = useMemo(() => (data.data ? buildGroups(data.data) : []), [data.data]);

  const from = useMemo(() => parseDate(range.from) ?? today(), [range.from]);
  const to = useMemo(() => parseDate(range.to) ?? addDays(today(), 30), [range.to]);
  const days = Math.max(1, diffDays(from, to) + 1);

  // 时档：执行记录按任务懒加载（/tasks/{id} 带 runs），只取有实际开始的任务；任务 id 全局唯一，缓存跨分组复用
  const [runs, setRuns] = useState<Record<string, Run[]>>({});
  const pendingRunIds = useMemo(() => (scale === "hour" && data.data ? data.data.rows.flatMap((r) => r.tasks).filter((t) => t.actual_start && !(t.id in runs)).map((t) => t.id) : []), [scale, data.data, runs]);
  const runsLoading = pendingRunIds.length > 0;
  useEffect(() => {
    const ids = pendingRunIds;
    if (!ids.length) return;
    let alive = true;
    (async () => {
      const got: Record<string, Run[]> = {};
      const queue = [...ids];
      const worker = async () => {
        for (let id = queue.shift(); id; id = queue.shift()) {
          try {
            got[id] = (await api.tasks.get(id)).runs;
          } catch {
            got[id] = [];
          }
        }
      };
      await Promise.all(Array.from({ length: 4 }, worker));
      if (alive) setRuns((prev) => ({ ...prev, ...got }));
    })();
    return () => {
      alive = false;
    };
  }, [pendingRunIds]);

  // ---------- 范围与缩放 ----------
  const chart = useRef<GanttHandle>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const centerRange = useCallback((s: Scale, centerMs: number) => {
    const spanDays = SPAN_DAYS[s];
    const c = new Date(centerMs);
    c.setHours(0, 0, 0, 0);
    setRange({ from: toISODate(addDays(c, -Math.floor(spanDays / 2))), to: toISODate(addDays(c, Math.ceil(spanDays / 2))) });
  }, []);
  // 按钮切档：今天在视口里就以今天为锚（它留在原位），否则围绕视口中心
  const setScale = (s: Scale) => {
    if (s === scale && dayW === SCALE_DAY_W[s]) return;
    const now = nowMs();
    let center = chart.current?.centerTime() ?? now;
    if (chart.current?.isVisible(now)) {
      chart.current.anchorAt(now);
      center = now;
    }
    setDayW(SCALE_DAY_W[s]);
    centerRange(s, center);
  };
  const onZoom = useCallback(
    (next: number, anchor: { time: number }) => {
      const w = clampDayW(next);
      const s = scaleFor(w);
      setDayW(w);
      if (s !== scaleFor(dayW)) centerRange(s, anchor.time);
    },
    [dayW, centerRange],
  );
  const zoomStep = (dir: 1 | -1) => {
    const i = SCALES.indexOf(scale);
    const next = SCALES[Math.min(SCALES.length - 1, Math.max(0, i - dir))];
    setScale(next);
  };
  const fitAll = () => {
    const ts = groups.flatMap((g) => g.tasks);
    const dates = [...ts.flatMap((x) => [x.planned_start, x.planned_end]), ...groups.flatMap((g) => [g.goal?.planned_start ?? null, g.goal?.planned_end ?? null])].filter((d): d is string => !!d).sort();
    if (!dates.length) return;
    const a = parseDate(dates[0])!;
    const b = parseDate(dates[dates.length - 1])!;
    const span = Math.max(7, diffDays(a, b) + 1);
    const pad = Math.max(1, Math.round(span * 0.04));
    const nf = addDays(a, -pad);
    const nt = addDays(b, pad);
    const total = diffDays(nf, nt) + 1;
    const vw = (chart.current?.viewportWidth() ?? 800) - 8;
    setRange({ from: toISODate(nf), to: toISODate(nt) });
    setDayW(clampDayW(vw / total));
    requestAnimationFrame(() => chart.current?.scrollToTime(nf.getTime(), 0));
  };
  const shift = (dir: 1 | -1) => {
    const n = NAV_STEP_DAYS[scale] * dir;
    setRange({ from: toISODate(addDays(from, n)), to: toISODate(addDays(to, n)) });
    requestAnimationFrame(() => chart.current?.scrollToTime(chart.current.centerTime() + n * DAY_MS, 0.5));
  };
  const goToday = () => {
    centerRange(scale, nowMs());
    requestAnimationFrame(() => chart.current?.scrollToTime(nowMs(), 0.3));
  };
  const extend = () => setRange({ from: range.from, to: toISODate(addDays(to, Math.round(SPAN_DAYS[scale] / 2))) });
  const canShorten = days > SPAN_DAYS[scale] * 1.5;
  const shorten = () => setRange({ from: range.from, to: toISODate(addDays(from, SPAN_DAYS[scale])) });

  // ---------- 收起 / 展开 ----------
  const toggle = (key: string) => setCollapsedKeys((prev) => (prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]));
  const collapseAll = () => setCollapsedKeys(groups.map((g) => g.key));
  const expandAll = () => setCollapsedKeys([]);
  const macro = isMacro(scale);

  // ---------- 全屏 ----------
  const shellRef = useRef<HTMLDivElement>(null);
  const fs = useFullscreen(shellRef);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "f" || e.metaKey || e.ctrlKey || e.altKey || isEditable(e.target) || document.querySelector("dialog[open]")) return;
      e.preventDefault();
      markKeyboardIntent();
      fs.toggle();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [fs]);

  // 图区高度 = 视口剩余高度：量出外壳距离页面顶部的距离，再减去页面底边距
  const [shellTop, setShellTop] = useState(0);
  useLayoutEffect(() => {
    const measure = () => {
      const el = shellRef.current;
      if (!el || fs.active) return;
      setShellTop(Math.round(el.getBoundingClientRect().top + window.scrollY));
    };
    measure();
    window.addEventListener("resize", measure);
    const ro = new ResizeObserver(measure);
    if (shellRef.current?.parentElement) ro.observe(shellRef.current.parentElement);
    return () => {
      window.removeEventListener("resize", measure);
      ro.disconnect();
    };
  }, [fs.active, data.data]);

  const onCreate = (d: CreateDraft) => {
    const key = d.rowKey === UNSCHEDULED_KEY ? "_" : d.rowKey;
    setDraft({
      planned_start: d.start,
      planned_end: d.end,
      goal_id: group === "goal" && key !== "_" ? key : null,
      type: group === "type" && key !== "_" ? key : undefined,
      assignee_id: group === "executor" && key !== "_" ? key : null,
    });
  };

  const total = groups.reduce((s, g) => s + g.tasks.length, 0);
  const inApp = fs.active && !fs.native;

  return (
    <div className="gantt-page">
      {/* 标题行只占一行：说明折进「?」提示 */}
      <div className="gantt-title">
        <h1 className="text-headline text-ink">{t("gantt.title")}</h1>
        <Tip tip={t("gantt.description")} placement="bottom">
          <button type="button" className="gantt-help pressable" aria-label={t("gantt.help")}>
            ?
          </button>
        </Tip>
        <EnergyLine className="ml-1 hidden sm:block" />
        {data.data && <span className="ml-auto font-mono text-[11px] text-ink-subtle">{t("gantt.taskCount", { n: total })}</span>}
      </div>

      <div ref={shellRef} className={cx("gantt-shell", inApp && "gantt-shell-fs")} style={fs.active ? undefined : { height: `calc(100vh - ${shellTop}px - 24px)` }}>
        {/* 工具栏：全屏内也保留 */}
        <div className="gantt-toolbar">
          <Segmented size="sm" label={t("gantt.group")} value={group} options={GROUPS.map((g) => [g, t(`gantt.group.${g}`)])} onChange={setGroup} />
          <Segmented size="sm" label={t("gantt.zoom")} value={scale} options={SCALES.map((z) => [z, scaleTitle(z)])} onChange={setScale} aria-label={t("gantt.zoom")} />
          <span className="gantt-btns" title={t("gantt.zoomTip")}>
            <Button size="sm" variant="ghost" onClick={() => zoomStep(-1)} disabled={scale === "year"} aria-label={t("gantt.zoomOut")} title={t("gantt.zoomOut")}>−</Button>
            <Button size="sm" variant="ghost" onClick={() => zoomStep(1)} disabled={scale === "hour"} aria-label={t("gantt.zoomIn")} title={t("gantt.zoomIn")}>+</Button>
            <Button size="sm" variant="ghost" onClick={fitAll} disabled={!total}>{t("gantt.fit")}</Button>
          </span>
          <Checkbox checked={showCost} onChange={(e) => setShowCost(e.target.checked)} label={t("gantt.showCost")} />
          <Checkbox checked={showOverdue} onChange={(e) => setShowOverdue(e.target.checked)} label={t("gantt.showOverdue")} />
          <span className="gantt-btns ml-auto">
            <Tip tip={macro ? t("gantt.macroHint") : null} placement="bottom">
              <Button size="sm" variant="ghost" onClick={collapseAll} disabled={macro || !groups.length}>{t("gantt.collapseAll")}</Button>
            </Tip>
            <Tip tip={macro ? t("gantt.macroHint") : null} placement="bottom">
              <Button size="sm" variant="ghost" onClick={expandAll} disabled={macro || !groups.length}>{t("gantt.expandAll")}</Button>
            </Tip>
            <Segmented size="sm" value={density} options={[["compact", t("gantt.density.compact")], ["comfortable", t("gantt.density.comfortable")]]} onChange={setDensity} aria-label={t("gantt.density")} />
            <span className="gantt-sep" aria-hidden="true" />
            <Button size="sm" icon={<IconChevronLeft />} onClick={() => shift(-1)}>{t(`gantt.nav.prev.${scale}`)}</Button>
            <Button size="sm" onClick={goToday}>{t("gantt.today")}</Button>
            <Button size="sm" onClick={() => shift(1)}>{t(`gantt.nav.next.${scale}`)}<IconChevronRight /></Button>
            <Button size="sm" variant="ghost" onClick={canShorten ? shorten : extend}>{canShorten ? t("gantt.shorten") : t("gantt.extend")}</Button>
            <span className="gantt-sep" aria-hidden="true" />
            <Button size="sm" variant={fs.active ? "default" : "ghost"} onClick={fs.toggle} title={t("gantt.fullscreenTip")} aria-pressed={fs.active}>
              {fs.active ? t("gantt.exitFullscreen") : t("gantt.fullscreen")}
            </Button>
          </span>
        </div>

        {data.loading && !data.data ? (
          <div className="gantt-frame rounded-lg border border-hairline bg-surface-1 p-4 shadow-panel" aria-busy="true">
            <Skeleton className="mb-4 h-2.5 w-1/3" />
            {Array.from({ length: 8 }, (_, i) => (
              <div key={i} className="mb-3 flex items-center gap-4">
                <Skeleton className="w-52" />
                <Skeleton className="h-4" style={{ width: `${20 + ((i * 17) % 40)}%`, marginLeft: `${(i * 11) % 30}%` }} />
              </div>
            ))}
          </div>
        ) : data.error ? (
          <ErrorBox message={data.error} onRetry={data.reload} className="gantt-frame" />
        ) : data.data ? (
          <div className="gantt-frame hud rounded-lg border border-hairline bg-surface-1 shadow-panel">
            <Gantt
              ref={chart}
              data={data.data}
              groups={groups}
              from={from}
              days={days}
              dayW={dayW}
              scale={scale}
              onZoom={onZoom}
              showCost={showCost}
              showOverdue={showOverdue}
              onCreate={onCreate}
              runs={runs}
              collapsed={collapsed}
              onToggle={toggle}
              density={density}
              labelW={Math.min(LABEL_MAX, Math.max(LABEL_MIN, labelW))}
              narrow={narrow}
              onLabelW={setLabelW}
              onNarrow={setNarrow}
              scrollRef={scrollRef}
            />
          </div>
        ) : null}

        {/* 图例：底部一行，可折叠 */}
        <div className="gantt-legend" data-open={legendOpen ? "" : undefined}>
          <button type="button" className="gantt-legend-toggle pressable" onClick={() => setLegendOpen(!legendOpen)} aria-expanded={legendOpen}>
            <IconChevronDown className={cx("chevron", !legendOpen && "-rotate-90")} size={12} />
            {legendOpen ? t("gantt.legend.hide") : t("gantt.legend.show")}
          </button>
          {legendOpen && (
            <div className="gantt-legend-items">
              {STATE_LABELS.map((l) => (
                <span key={l} className="inline-flex items-center gap-1.5"><i className="inline-block h-2.5 w-5 rounded-xs border" style={{ background: STATE_FILL[l].bg, borderColor: STATE_FILL[l].border }} />{stateLabel(l)}</span>
              ))}
              <span className="inline-flex items-center gap-1.5">
                {/* 航段样例：细线 + 两端航点 */}
                <svg width="22" height="8" viewBox="0 0 22 8" aria-hidden="true" focusable="false"><line x1="3" y1="4" x2="19" y2="4" stroke="var(--c-hairline-tertiary)" strokeWidth="2" /><circle cx="3" cy="4" r="2.5" fill="var(--c-surface-1)" stroke="var(--c-hairline-tertiary)" strokeWidth="1.5" /><circle cx="19" cy="4" r="2.5" fill="var(--c-surface-1)" stroke="var(--c-hairline-tertiary)" strokeWidth="1.5" /></svg>
                {t("gantt.legend.goal")}
              </span>
              <span className="inline-flex items-center gap-1.5">
                <i className="relative inline-block h-2.5 w-6 overflow-hidden rounded-xs border" style={{ background: SUMMARY_FILL.bg, borderColor: SUMMARY_FILL.border }}><i className="absolute inset-y-0 left-0 w-1/2" style={{ background: SUMMARY_FILL.done }} /></i>
                {t("gantt.legend.summary")}
              </span>
              <span className="inline-flex items-center gap-1.5"><i className="inline-block h-1 w-5 rounded-xs bg-ink-muted" />{t("gantt.legend.actual")}</span>
              {scale === "hour" && (
                <>
                  <span className="inline-flex items-center gap-1.5"><i className="inline-block h-2 w-5 rounded-xs" style={{ background: RUN_FILL.agent }} />{t("gantt.legend.runAgent")}</span>
                  <span className="inline-flex items-center gap-1.5"><i className="inline-block h-2 w-5 rounded-xs" style={{ background: RUN_FILL.member }} />{t("gantt.legend.runPerson")}</span>
                </>
              )}
              {macro && <span className="inline-flex items-center gap-1.5"><i className="inline-block h-2.5 w-5 rounded-xs bg-accent/30" />{t("gantt.legend.density")}</span>}
              <span className="inline-flex items-center gap-1.5"><i className="inline-block h-3 w-0.5 bg-accent" />{t("gantt.today")}</span>
              <span className="inline-flex items-center gap-1.5"><i className="inline-block h-0 w-0 border-y-[5px] border-l-[6px] border-y-transparent border-l-danger" />{t("gantt.legend.overdue")}</span>
              {showCost && <span className="inline-flex items-center gap-1.5"><i className="inline-block h-2.5 w-5 rounded-xs" style={{ background: COST_LEGEND.bg, border: `1px solid ${COST_LEGEND.border}` }} />{t("gantt.legend.cost")}</span>}
              <span>{t("gantt.legend.lines")}</span>
              {runsLoading && <span className="font-mono text-[11px] text-telemetry">{t("gantt.runsLoading")}</span>}
            </div>
          )}
        </div>
      </div>
      <TaskDrawer open={!!draft} onClose={() => setDraft(null)} onCreated={() => data.reload()} goals={flat} types={types.data ?? []} defaults={draft ?? undefined} />
    </div>
  );
}
