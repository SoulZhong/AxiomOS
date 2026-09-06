"use client";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { setQueryParams, useExecutors, useHighlight, useLoad, useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useFadeOnChange } from "@/lib/useFadeOnChange";
import { useSession } from "@/components/AppShell";
import { flattenGoals } from "@/components/GoalDrawer";
import { IconBacklog, IconBoard, IconGanttFlight, IconList, IconPlus, IconSprint } from "@/components/icons";
import { useShortcutHandler } from "@/components/shortcuts";
import { TaskDrawer, type TaskDrawerDefaults } from "@/components/TaskDrawer";
import { BacklogView } from "@/components/tasks/BacklogView";
import { BoardView } from "@/components/tasks/BoardView";
import { FILTER_KEYS, goalSubtree, isTaskView, principals, TASK_VIEW_KEY, TASK_VIEWS, teamOfExecutor, type FilterKey, type MatchCtx, type TaskFilters, type TaskView } from "@/components/tasks/filters";
import { GanttView } from "@/components/tasks/GanttView";
import { ListView } from "@/components/tasks/ListView";
import { SprintsView } from "@/components/tasks/SprintsView";
import { TaskFilterBar, type FilterOptions } from "@/components/tasks/TaskFilterBar";
import { Button, ListSkeleton, PageHeader, Panel, Tabs } from "@/components/ui";

const VIEW_ICON: Record<TaskView, React.ReactNode> = { list: <IconList />, board: <IconBoard />, gantt: <IconGanttFlight />, sprints: <IconSprint />, backlog: <IconBacklog /> };

/**
 * 默认页签的规则（DESIGN.md §10、ADR 0015）：
 *   1. 地址栏有 ?view= 就听它（分享链接、旧地址跳转）。
 *   2. 否则用这个人自己切过的那一次（localStorage axiomos.tasks.view）——切过一次后就按人记，不再按角色。
 *   3. 都没有时按角色：读 GET /workspace 解析出的区块顺序，与 GET /workspace/blocks 的预设逐个比对——
 *      正好是「执行视角」（doer）或「小组视角」（team）的人干的是具体的活，进「任务」默认到看板；正好是其他预设的默认到列表。
 *      多个角色并集出来的布局对不上任何预设时，看区块的成分：含经营类区块（组织概览摘要 / 成本与预算 / 趋势与环比）的人在管事 → 列表；
 *      否则有「我的任务」（执行者的典型首屏）→ 看板；再否则 → 列表。
 */
const MANAGE_BLOCKS = new Set(["overview_summary", "cost_budget", "trend"]);
async function defaultViewByRole(): Promise<TaskView> {
  try {
    const [ws, catalog] = await Promise.all([api.workspace.get(), api.workspace.catalog().catch(() => ({ blocks: [], presets: [] }))]);
    const keys = ws.blocks.map((b) => b.key);
    const preset = catalog.presets.find((p) => p.blocks.length === keys.length && p.blocks.every((b, i) => b.key === keys[i]));
    if (preset) return preset.key === "doer" || preset.key === "team" ? "board" : "list";
    if (keys.some((k) => MANAGE_BLOCKS.has(k))) return "list";
    return keys.includes("my_tasks") ? "board" : "list";
  } catch {
    return "list";
  }
}
const readRemembered = (): TaskView | null => {
  try {
    const v = localStorage.getItem(TASK_VIEW_KEY);
    return isTaskView(v) ? v : null;
  } catch {
    return null;
  }
};

/*
 * 「任务」入口（DESIGN.md §10）：五个页签 = 同一批任务的不同看法，一条筛选栏管全部，页签与筛选都在地址栏里。
 * 新建任务按钮对所有页签都在页头；甘特图拖出来的草稿也走同一个抽屉。
 */
export default function TasksPage() {
  const { session } = useSession();
  const qView = useQueryParam("view");
  const qNew = useQueryParam("new");
  const qGoal = useQueryParam("goal");
  const qTeam = useQueryParam("team");
  const qAssignee = useQueryParam("assignee");
  const qType = useQueryParam("type");
  const qSprint = useQueryParam("sprint");
  const qState = useQueryParam("state");
  const filters = useMemo<TaskFilters>(() => ({ goal: qGoal ?? "", team: qTeam ?? "", assignee: qAssignee ?? "", type: qType ?? "", sprint: qSprint ?? "", state: qState ?? "" }), [qGoal, qTeam, qAssignee, qType, qSprint, qState]);
  const view: TaskView | null = isTaskView(qView) ? qView : null;

  // 地址栏没有 ?view= 时解析默认页签并写回地址栏（之后一切以地址栏为准）；effect 只在客户端跑，这里可以直接读 localStorage
  const resolving = useRef(false);
  useEffect(() => {
    if (view || resolving.current) return;
    const remembered = readRemembered();
    if (remembered) {
      setQueryParams({ view: remembered });
      return;
    }
    resolving.current = true;
    void defaultViewByRole().then((v) => {
      resolving.current = false;
      if (!isTaskView(new URLSearchParams(window.location.search).get("view"))) setQueryParams({ view: v });
    });
  }, [view]);
  const switchView = useCallback((v: TaskView) => {
    setQueryParams({ view: v });
    try {
      localStorage.setItem(TASK_VIEW_KEY, v);
    } catch {}
  }, []);
  const setFilter = useCallback((key: FilterKey, value: string) => setQueryParams({ [key]: value || null }), []);
  const clearFilters = useCallback(() => setQueryParams(Object.fromEntries(FILTER_KEYS.map((k) => [k, null]))), []);

  // 筛选栏的选项：各页签共用一份，不重复拉
  const goals = useLoad(() => api.goals.list(), []);
  const types = useLoad(() => api.taskTypes.list(), []);
  const sprints = useLoad(() => api.sprints.list().catch(() => []), []);
  const ex = useExecutors();
  const flat = useMemo(() => flattenGoals(goals.data ?? []), [goals.data]);
  const options: FilterOptions = useMemo(() => ({ goals: flat, teams: session?.teams ?? [], executors: ex.data?.executors ?? [], types: types.data ?? [], sprints: sprints.data ?? [] }), [flat, session?.teams, ex.data, types.data, sprints.data]);
  const members = ex.data?.members;
  const agents = ex.data?.agents;
  const ctx = useMemo<MatchCtx>(() => ({ subtree: filters.goal ? goalSubtree(filters.goal, flat) : null, me: principals(session, agents ?? []), teamOf: (id) => teamOfExecutor(id, members ?? [], agents ?? []) }), [filters.goal, flat, session, members, agents]);

  // 新建任务：页头按钮、n 快捷键、?new=1、甘特图拖拽草稿都开同一个抽屉；创建后各页签重新取数、列表里高亮新行
  const [draft, setDraft] = useState<TaskDrawerDefaults | null>(null);
  const [reloadKey, setReloadKey] = useState(0);
  const [highlight, mark] = useHighlight();
  const openNew = useCallback(() => setDraft({ goal_id: filters.goal || null, type: filters.type || undefined }), [filters.goal, filters.type]);
  useShortcutHandler("new-task", openNew);
  const [seenNew, setSeenNew] = useState<string | null>(null);
  if (qNew !== seenNew) {
    setSeenNew(qNew);
    if (qNew) setDraft({ goal_id: qGoal || null });
  }

  // 页签切换：内容区一次 200ms 的交叉淡入（与甘特图切刻度同一条过渡）
  const bodyRef = useRef<HTMLDivElement>(null);
  useFadeOnChange(bodyRef, view);

  return (
    <div>
      <PageHeader title={t("tasks.title")} description={t("tasks.description")} actions={<Button variant="primary" icon={<IconPlus />} onClick={openNew}>{t("tasks.new")}</Button>} />
      <Tabs value={view} label={t("tasks.views")} items={TASK_VIEWS.map((v) => ({ key: v, label: t(`tasks.view.${v}`), icon: VIEW_ICON[v] }))} onChange={switchView} />
      {view && <TaskFilterBar view={view} filters={filters} options={options} onChange={setFilter} onClear={clearFilters} />}
      <div ref={bodyRef} data-view={view ?? undefined}>
        {!view ? (
          <Panel><ListSkeleton rows={5} /></Panel>
        ) : view === "list" ? (
          <ListView filters={filters} ctx={ctx} reloadKey={reloadKey} highlightId={highlight} onNew={openNew} />
        ) : view === "board" ? (
          <BoardView filters={filters} reloadKey={reloadKey} />
        ) : view === "gantt" ? (
          <GanttView filters={filters} ctx={ctx} reloadKey={reloadKey} onCreate={setDraft} />
        ) : view === "sprints" ? (
          <SprintsView filters={filters} />
        ) : (
          <BacklogView filters={filters} ctx={ctx} reloadKey={reloadKey} />
        )}
      </div>
      <TaskDrawer
        open={!!draft}
        onClose={() => {
          setDraft(null);
          if (qNew) setQueryParams({ new: null });
        }}
        onCreated={(task) => {
          mark(task.id);
          setReloadKey((k) => k + 1);
        }}
        goals={flat}
        types={types.data ?? []}
        defaults={draft ?? undefined}
      />
    </div>
  );
}
