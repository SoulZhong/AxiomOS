"use client";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { setQueryParams, useHighlight, useLoad, useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useFadeOnChange } from "@/lib/useFadeOnChange";
import { flattenGoals, GoalDrawer } from "@/components/GoalDrawer";
import { IconClose, IconGoal, IconPlus, IconTrail } from "@/components/icons";
import { useShortcutHandler } from "@/components/shortcuts";
import { Button, ListSkeleton, PageHeader, Panel, Tabs } from "@/components/ui";
import { RoadmapView } from "@/components/roadmap/RoadmapView";
import { TreeView } from "./TreeView";

/*
 * 「目标」入口（DESIGN.md §10、§25）：页签 路线图 · 树，?view=roadmap|tree，
 * 同一批目标、同一个范围。与「任务」用同一个页签组件、同一套地址栏约定与 200ms 交叉淡入。
 *
 * 默认页签是路线图（ADR 0021、0022）：进「目标」先看到方向，需要细节再切树。
 * 地址栏没有 ?view= 时：先听这个人上次切过的那一个（localStorage），没切过就是路线图；
 * 解析结果写回地址栏，之后一切以地址栏为准（与「任务」同一套，DESIGN.md §10）。
 * 「甘特图」页签已并入路线图（ADR 0022 补记）：?view=gantt 与记住的 "gantt" 都落到路线图，地址栏随即改成 ?view=roadmap。
 *
 * ?type=<id>（组织设置里「看用了这个类型的目标」过来的）两个页签都认：路线图与树都只显示这个类型的目标，
 * 页签下面挂一枚可去掉的芯片，去掉就是清空这个筛选。
 */
const VIEWS = ["roadmap", "tree"] as const;
const VIEW_ICON: Record<GoalView, React.ReactNode> = { roadmap: <IconTrail />, tree: <IconGoal /> };
type GoalView = (typeof VIEWS)[number];
const isView = (v: unknown): v is GoalView => typeof v === "string" && (VIEWS as readonly string[]).includes(v);
/** 并掉的旧页签 → 现在该去哪（ADR 0022 补记：目标甘特图并进了路线图） */
const RETIRED: Record<string, GoalView> = { gantt: "roadmap" };
const retiredTo = (v: unknown): GoalView | null => (typeof v === "string" ? RETIRED[v] ?? null : null);
/** 个人记住的页签（切过一次后按人记） */
const GOAL_VIEW_KEY = "axiomos.goals.view";
const readRemembered = (): GoalView | null => {
  try {
    const v = localStorage.getItem(GOAL_VIEW_KEY);
    return isView(v) ? v : retiredTo(v);
  } catch {
    return null;
  }
};

export default function GoalsPage() {
  const qView = useQueryParam("view");
  const qNew = useQueryParam("new");
  const qType = useQueryParam("type");
  const view: GoalView | null = isView(qView) ? qView : null;
  useEffect(() => {
    if (view) return;
    // ?view=gantt 直接落到路线图（不听上次切过的那一个）；地址栏一并改干净，之后分享出去的就是新地址
    setQueryParams({ view: retiredTo(qView) ?? readRemembered() ?? "roadmap" });
  }, [view, qView]);
  const switchView = useCallback((v: GoalView) => {
    setQueryParams({ view: v });
    try {
      localStorage.setItem(GOAL_VIEW_KEY, v);
    } catch {}
  }, []);
  const bodyRef = useRef<HTMLDivElement>(null);
  useFadeOnChange(bodyRef, view);

  // 类型词表（ADR 0023）：芯片上要显示类型名，路线图的类型下拉也用它
  const types = useLoad(() => api.goals.types(), []);
  const setType = useCallback((v: string | null) => setQueryParams({ type: v }), []);
  const typeName = qType ? (qType === "none" ? t("goalType.none") : (types.data ?? []).find((x) => x.id === qType)?.name ?? qType) : null;

  // 新建目标（页头按钮 / 行内「新建子目标」/ 路线图空列）走同一个抽屉；创建后各页签重新取数、树里高亮新行
  const [creating, setCreating] = useState<string | null | false>(false); // parent_id
  const [reloadKey, setReloadKey] = useState(0);
  const [highlight, mark] = useHighlight();
  const goals = useLoad(() => api.goals.list(), [reloadKey]);
  const flat = useMemo(() => flattenGoals(goals.data ?? []), [goals.data]);
  const openNew = useCallback(() => setCreating(null), []);
  // 新建目标：页头按钮、g c 快捷键、?new=1（快速命令 / 快捷键跨页跳来）都开同一个抽屉
  useShortcutHandler("new-goal", openNew);
  const [seenNew, setSeenNew] = useState<string | null>(null);
  if (qNew !== seenNew) {
    setSeenNew(qNew);
    if (qNew) setCreating(null);
  }

  return (
    <div>
      <PageHeader title={t("goals.title")} description={t("goals.description")} actions={<Button variant="primary" icon={<IconPlus />} onClick={openNew}>{t("goals.new")}</Button>} />
      <Tabs value={view} label={t("goals.views")} items={VIEWS.map((v) => ({ key: v, label: t(`goals.view.${v}`), icon: VIEW_ICON[v] }))} onChange={switchView} />
      {qType && (
        <div className="mt-2 flex flex-wrap items-center gap-1.5" role="list" aria-label={t("tasks.filter.active")}>
          <span role="listitem" className="filter-chip" data-chip="type">
            <span className="text-ink-subtle">{t("goal.type")}</span>
            <span className="max-w-[200px] truncate text-ink">{typeName}</span>
            <button type="button" className="filter-chip-x pressable" onClick={() => setType(null)} aria-label={t("tasks.filter.remove", { name: t("goal.type") })}>
              <IconClose size={12} />
            </button>
          </span>
        </div>
      )}
      <div ref={bodyRef} data-view={view ?? undefined}>
        {!view ? (
          <Panel><ListSkeleton rows={5} /></Panel>
        ) : view === "roadmap" ? (
          <RoadmapView reloadKey={reloadKey} onNew={openNew} type={qType} types={types.data ?? []} onType={setType} />
        ) : (
          <TreeView reloadKey={reloadKey} highlight={highlight} type={qType} onAddChild={(id) => setCreating(id)} onNew={openNew} onClearType={() => setType(null)} />
        )}
      </div>
      <GoalDrawer open={creating !== false} parentId={creating === false ? null : creating} goals={flat} onClose={() => { setCreating(false); if (qNew) setQueryParams({ new: null }); }} onCreated={(g) => { mark(g.id); setReloadKey((k) => k + 1); }} />
    </div>
  );
}
