"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { api, type GanttData, type GanttGroup, type Run } from "@/lib/api";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { STATE_LABELS, stateLabel } from "@/lib/terms";
import { usePersisted } from "@/lib/usePersisted";
import { buildGroups, COST_LEGEND, Gantt, RUN_FILL, STATE_FILL, SUMMARY_FILL, UNSCHEDULED_KEY, type CreateDraft, type Density, type GanttHandle } from "@/components/gantt/Gantt";
import { GanttNavControls, GanttZoomControls } from "@/components/gantt/GanttControls";
import { useGanttScale } from "@/components/gantt/useGanttScale";
import { useGanttShell } from "@/components/gantt/useGanttShell";
import { IconChevronDown } from "@/components/icons";
import type { TaskDrawerDefaults } from "@/components/TaskDrawer";
import { Button, Checkbox, cx, ErrorBox, Segmented, Skeleton, Tip } from "@/components/ui";
import { FILTER_KEYS, matchTask, type MatchCtx, type TaskFilters } from "./filters";

const GROUPS: GanttGroup[] = ["goal", "team", "executor", "type"];
const asNumber = (raw: unknown) => (typeof raw === "number" && Number.isFinite(raw) ? raw : undefined);
const asBool = (raw: unknown) => (typeof raw === "boolean" ? raw : undefined);
const asDensity = (raw: unknown) => (raw === "compact" || raw === "comfortable" ? raw : undefined);
const asKeys = (raw: unknown) => (Array.isArray(raw) && raw.every((x) => typeof x === "string") ? (raw as string[]) : undefined);

/** GET /gantt 只认分组与范围：筛选栏的 goal / team / assignee / type / state / 截止日 / 关键词在这里按行内任务过滤（甘特数据里没有优先级与迭代）；筛掉后空了的行不再显示（目标筛选时保留子树里的目标行）。 */
function applyFilters(data: GanttData, f: TaskFilters, ctx: MatchCtx): GanttData {
  if (!FILTER_KEYS.some((k) => k !== "sprint" && k !== "priority" && f[k])) return data;
  const keep = new Set<string>();
  const rows = data.rows
    .map((r) => ({ ...r, tasks: r.tasks.filter((x) => matchTask({ id: x.id, number: x.number, title: x.title, goalId: x.goal_id, assigneeId: x.assignee?.id ?? null, type: x.type, label: x.state.label, sprintId: null, priority: "normal", plannedEnd: x.planned_end }, f, ctx, ["sprint", "priority"])) }))
    .filter((r) => r.tasks.length > 0 || (data.group === "goal" && !!f.goal && !!ctx.subtree?.has(r.key)));
  for (const r of rows) for (const x of r.tasks) keep.add(x.id);
  return { ...data, rows, dependencies: data.dependencies.filter((d) => keep.has(d.from_task_id) && keep.has(d.to_task_id)) };
}

/*
 * 「任务 · 甘特图」（DESIGN.md §8）：把空间全给图、任何粒度都能看、能收能放、能全屏。
 * 刻度 / 范围 / 缩放 / 全屏 / 高度全部来自 gantt/useGanttScale 与 useGanttShell；这里只剩任务甘特图特有的东西：分组、成本 / 逾期开关、执行记录懒加载、图例、拖拽新建。
 */
export function GanttView({ filters, ctx, reloadKey, onCreate }: { filters: TaskFilters; ctx: MatchCtx; reloadKey: number; onCreate: (defaults: TaskDrawerDefaults) => void }) {
  const [group, setGroup] = useState<GanttGroup>("goal");
  const chart = useRef<GanttHandle>(null);
  const g = useGanttScale(chart, "day");
  const [showCost, setShowCost] = useState(false);
  const [showOverdue, setShowOverdue] = useState(true);

  // 记在本地的界面状态（axiomos.gantt.*）
  const [labelW, setLabelW] = usePersisted<number>("axiomos.gantt.labelWidth", 260, asNumber);
  const [narrow, setNarrow] = usePersisted<boolean>("axiomos.gantt.narrow", false, asBool);
  const [density, setDensity] = usePersisted<Density>("axiomos.gantt.density", "comfortable", asDensity);
  const [legendOpen, setLegendOpen] = usePersisted<boolean>("axiomos.gantt.legend", true, asBool);
  const collapsedDefault = useMemo(() => [UNSCHEDULED_KEY], []);
  const [collapsedKeys, setCollapsedKeys] = usePersisted<string[]>(`axiomos.gantt.collapsed.${group}`, collapsedDefault, asKeys);
  const collapsed = useMemo(() => new Set(collapsedKeys), [collapsedKeys]);

  const raw = useLoad(() => api.gantt.get({ group, from: g.range.from, to: g.range.to }), [group, reloadKey]);
  const data = useMemo(() => (raw.data ? applyFilters(raw.data, filters, ctx) : null), [raw.data, filters, ctx]);
  const groups = useMemo(() => (data ? buildGroups(data) : []), [data]);

  // 时档：执行记录按任务懒加载（/tasks/{id} 带 runs），只取有实际开始的任务；任务 id 全局唯一，缓存跨分组复用
  const [runs, setRuns] = useState<Record<string, Run[]>>({});
  const pendingRunIds = useMemo(() => (g.scale === "hour" && data ? data.rows.flatMap((r) => r.tasks).filter((x) => x.actual_start && !(x.id in runs)).map((x) => x.id) : []), [g.scale, data, runs]);
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

  const fitAll = () => g.fitTo([...groups.flatMap((x) => x.tasks).flatMap((x) => [x.planned_start, x.planned_end]), ...groups.flatMap((x) => [x.goal?.planned_start ?? null, x.goal?.planned_end ?? null])]);
  const toggle = (key: string) => setCollapsedKeys((prev) => (prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]));
  const collapseAll = () => setCollapsedKeys(groups.map((x) => x.key));
  const expandAll = () => setCollapsedKeys([]);
  const shellRef = useRef<HTMLDivElement>(null);
  const shell = useGanttShell(shellRef, [data]);
  const scrollRef = useRef<HTMLDivElement>(null);

  const create = (d: CreateDraft) => {
    const key = d.rowKey === UNSCHEDULED_KEY ? "_" : d.rowKey;
    onCreate({
      planned_start: d.start,
      planned_end: d.end,
      goal_id: group === "goal" && key !== "_" ? key : filters.goal || null,
      type: group === "type" && key !== "_" ? key : filters.type || undefined,
      assignee_id: group === "executor" && key !== "_" ? key : null,
    });
  };
  const total = groups.reduce((s, x) => s + x.tasks.length, 0);

  return (
    <div className="gantt-page">
      <div ref={shellRef} className={cx("gantt-shell", shell.inApp && "gantt-shell-fs")} style={shell.style}>
        {/* 工具栏：全屏内也保留 */}
        <div className="gantt-toolbar">
          <Segmented size="sm" label={t("gantt.group")} value={group} options={GROUPS.map((k) => [k, t(`gantt.group.${k}`)])} onChange={setGroup} />
          <GanttZoomControls g={g} onFit={fitAll} canFit={total > 0} />
          <Checkbox checked={showCost} onChange={(e) => setShowCost(e.target.checked)} label={t("gantt.showCost")} />
          <Checkbox checked={showOverdue} onChange={(e) => setShowOverdue(e.target.checked)} label={t("gantt.showOverdue")} />
          {data && <span className="font-mono text-[11px] text-ink-subtle">{t("gantt.taskCount", { n: total })}</span>}
          <span className="gantt-btns ml-auto">
            <Tip tip={g.macro ? t("gantt.macroHint") : null} placement="bottom">
              <Button size="sm" variant="ghost" onClick={collapseAll} disabled={g.macro || !groups.length}>{t("gantt.collapseAll")}</Button>
            </Tip>
            <Tip tip={g.macro ? t("gantt.macroHint") : null} placement="bottom">
              <Button size="sm" variant="ghost" onClick={expandAll} disabled={g.macro || !groups.length}>{t("gantt.expandAll")}</Button>
            </Tip>
            <Segmented size="sm" value={density} options={[["compact", t("gantt.density.compact")], ["comfortable", t("gantt.density.comfortable")]]} onChange={setDensity} aria-label={t("gantt.density")} />
            <span className="gantt-sep" aria-hidden="true" />
            <GanttNavControls g={g} fs={shell.fs} />
          </span>
        </div>

        {raw.loading && !raw.data ? (
          <div className="gantt-frame rounded-lg border border-hairline bg-surface-1 p-4 shadow-panel" aria-busy="true">
            <Skeleton className="mb-4 h-2.5 w-1/3" />
            {Array.from({ length: 8 }, (_, i) => (
              <div key={i} className="mb-3 flex items-center gap-4">
                <Skeleton className="w-52" />
                <Skeleton className="h-4" style={{ width: `${20 + ((i * 17) % 40)}%`, marginLeft: `${(i * 11) % 30}%` }} />
              </div>
            ))}
          </div>
        ) : raw.error ? (
          <ErrorBox message={raw.error} onRetry={raw.reload} className="gantt-frame" />
        ) : data ? (
          <div className="gantt-frame hud rounded-lg border border-hairline bg-surface-1 shadow-panel">
            <Gantt
              ref={chart}
              data={data}
              groups={groups}
              from={g.from}
              days={g.days}
              dayW={g.dayW}
              scale={g.scale}
              onZoom={g.onZoom}
              showCost={showCost}
              showOverdue={showOverdue}
              onCreate={create}
              runs={runs}
              collapsed={collapsed}
              onToggle={toggle}
              density={density}
              labelW={labelW}
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
              {g.scale === "hour" && (
                <>
                  <span className="inline-flex items-center gap-1.5"><i className="inline-block h-2 w-5 rounded-xs" style={{ background: RUN_FILL.agent }} />{t("gantt.legend.runAgent")}</span>
                  <span className="inline-flex items-center gap-1.5"><i className="inline-block h-2 w-5 rounded-xs" style={{ background: RUN_FILL.member }} />{t("gantt.legend.runPerson")}</span>
                </>
              )}
              {g.macro && <span className="inline-flex items-center gap-1.5"><i className="inline-block h-2.5 w-5 rounded-xs bg-accent/30" />{t("gantt.legend.density")}</span>}
              <span className="inline-flex items-center gap-1.5"><i className="inline-block h-3 w-0.5 bg-accent" />{t("gantt.today")}</span>
              <span className="inline-flex items-center gap-1.5"><i className="inline-block h-0 w-0 border-y-[5px] border-l-[6px] border-y-transparent border-l-danger" />{t("gantt.legend.overdue")}</span>
              {showCost && <span className="inline-flex items-center gap-1.5"><i className="inline-block h-2.5 w-5 rounded-xs" style={{ background: COST_LEGEND.bg, border: `1px solid ${COST_LEGEND.border}` }} />{t("gantt.legend.cost")}</span>}
              <span>{t("gantt.legend.lines")}</span>
              {runsLoading && <span className="font-mono text-[11px] text-telemetry">{t("gantt.runsLoading")}</span>}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
