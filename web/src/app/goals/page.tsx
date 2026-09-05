"use client";
import { useCallback, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { setQueryParams, useHighlight, useLoad, useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useFadeOnChange } from "@/lib/useFadeOnChange";
import { flattenGoals, GoalDrawer } from "@/components/GoalDrawer";
import { IconGanttFlight, IconGoal, IconPlus } from "@/components/icons";
import { Button, PageHeader, Tabs } from "@/components/ui";
import { GoalGanttView } from "./GoalGanttView";
import { TreeView } from "./TreeView";

/*
 * 「目标」入口（DESIGN.md §10、§11）：页签 树 · 甘特图，?view=tree|gantt，同一批目标、同一个范围。
 * 与「任务」用同一个页签组件、同一套地址栏约定与 200ms 交叉淡入；甘特图页签（ADR 0016）= GoalGanttView，复用任务甘特图的框。
 */
const VIEWS = ["tree", "gantt"] as const;
const VIEW_ICON: Record<GoalView, React.ReactNode> = { tree: <IconGoal />, gantt: <IconGanttFlight /> };
type GoalView = (typeof VIEWS)[number];
const isView = (v: unknown): v is GoalView => typeof v === "string" && (VIEWS as readonly string[]).includes(v);

export default function GoalsPage() {
  const qView = useQueryParam("view");
  const view: GoalView = isView(qView) ? qView : "tree";
  const switchView = useCallback((v: GoalView) => setQueryParams({ view: v === "tree" ? null : v }), []);
  const bodyRef = useRef<HTMLDivElement>(null);
  useFadeOnChange(bodyRef, view);

  // 新建目标（页头按钮 / 行内「新建子目标」）走同一个抽屉；创建后各页签重新取数、树里高亮新行
  const [creating, setCreating] = useState<string | null | false>(false); // parent_id
  const [reloadKey, setReloadKey] = useState(0);
  const [highlight, mark] = useHighlight();
  const goals = useLoad(() => api.goals.list(), [reloadKey]);
  const flat = useMemo(() => flattenGoals(goals.data ?? []), [goals.data]);
  const openNew = useCallback(() => setCreating(null), []);

  return (
    <div>
      <PageHeader title={t("goals.title")} description={t("goals.description")} actions={<Button variant="primary" icon={<IconPlus />} onClick={openNew}>{t("goals.new")}</Button>} />
      <Tabs value={view} label={t("goals.views")} items={VIEWS.map((v) => ({ key: v, label: t(`goals.view.${v}`), icon: VIEW_ICON[v] }))} onChange={switchView} />
      <div ref={bodyRef} data-view={view}>
        {view === "tree" ? <TreeView reloadKey={reloadKey} highlight={highlight} onAddChild={(id) => setCreating(id)} onNew={openNew} /> : <GoalGanttView reloadKey={reloadKey} highlight={highlight} onNew={openNew} />}
      </div>
      <GoalDrawer open={creating !== false} parentId={creating === false ? null : creating} goals={flat} onClose={() => setCreating(false)} onCreated={(g) => { mark(g.id); setReloadKey((k) => k + 1); }} />
    </div>
  );
}
