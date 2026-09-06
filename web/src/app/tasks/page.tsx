"use client";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { setQueryParams, useExecutors, useHighlight, useLoad, useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { markKeyboardIntent } from "@/lib/motion";
import { getPreferences, usePreferences } from "@/lib/preferences";
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
import { TaskPeek } from "@/components/tasks/TaskPeek";
import { Button, ListSkeleton, PageHeader, Panel, Tabs } from "@/components/ui";

const VIEW_ICON: Record<TaskView, React.ReactNode> = { list: <IconList />, board: <IconBoard />, gantt: <IconGanttFlight />, sprints: <IconSprint />, backlog: <IconBacklog /> };

/**
 * 默认页签的规则（DESIGN.md §10、§20，ADR 0015）：
 *   1. 地址栏有 ?view= 就听它（分享链接、旧地址跳转）。
 *   2. 否则用这个人自己切过的那一次（localStorage axiomos.tasks.view）——切过一次后就按人记，不再按角色。
 *   2½. 显示偏好里明确设过「默认任务视图」（个人或角色，GET /me/preferences 的 sources 不是 default）就用它。
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

/**
 * j / k 在当前页签里上下选任务（表格行与看板卡片都带 data-task-row），Enter 开抽屉，o 进完整页，Esc 清掉选中。
 * 选中的 id 交给各视图画高亮（data-kbd-selected），页签切换时清空。
 */
function useTaskListNav(view: TaskView | null, onOpen: (id: string) => void, onOpenFull: (id: string) => void) {
  const [selected, setSelected] = useState<string | null>(null);
  const [seenView, setSeenView] = useState(view);
  if (seenView !== view) {
    setSeenView(view);
    setSelected(null);
  }
  const rows = () => Array.from(document.querySelectorAll<HTMLElement>("main [data-task-row]")).map((el) => el.dataset.taskRow!).filter(Boolean);
  const move = useCallback((delta: number) => {
    const ids = rows();
    if (!ids.length) return;
    setSelected((cur) => {
      const i = cur ? ids.indexOf(cur) : -1;
      const next = ids[Math.max(0, Math.min(ids.length - 1, i < 0 ? (delta > 0 ? 0 : ids.length - 1) : i + delta))];
      window.requestAnimationFrame(() => document.querySelector<HTMLElement>(`main [data-task-row="${CSS.escape(next)}"]`)?.scrollIntoView({ block: "nearest" }));
      return next;
    });
  }, []);
  useShortcutHandler("nav-down", useCallback(() => move(1), [move]));
  useShortcutHandler("nav-up", useCallback(() => move(-1), [move]));
  useShortcutHandler("nav-open", useCallback(() => { if (selected) onOpen(selected); }, [selected, onOpen]));
  useShortcutHandler("nav-open-full", useCallback(() => { if (selected) onOpenFull(selected); }, [selected, onOpenFull]));
  useShortcutHandler("escape", useCallback(() => setSelected(null), []));
  // 高亮由 DOM 属性承担（各视图的行 / 卡片重挂后也补上）
  useEffect(() => {
    const all = document.querySelectorAll<HTMLElement>("main [data-task-row][data-kbd-selected]");
    all.forEach((el) => el.removeAttribute("data-kbd-selected"));
    if (selected) document.querySelector<HTMLElement>(`main [data-task-row="${CSS.escape(selected)}"]`)?.setAttribute("data-kbd-selected", "");
  });
  return selected;
}

/*
 * 「任务」入口（DESIGN.md §10、§17、§18）：五个页签 = 同一批任务的不同看法，一条筛选栏管全部，页签、筛选与打开的任务都在地址栏里
 * （?view= &goal=…&q= &task=<id>）。点任务先开抽屉（只有①②和最近动态），「打开完整页」进详情。
 * 新建任务按钮对所有页签都在页头；甘特图拖出来的草稿也走同一个抽屉。
 */
export default function TasksPage() {
  const { session } = useSession();
  const router = useRouter();
  const qView = useQueryParam("view");
  const qNew = useQueryParam("new");
  const qTask = useQueryParam("task");
  const qGoal = useQueryParam("goal");
  const qTeam = useQueryParam("team");
  const qAssignee = useQueryParam("assignee");
  const qType = useQueryParam("type");
  const qSprint = useQueryParam("sprint");
  const qState = useQueryParam("state");
  const qPriority = useQueryParam("priority");
  const qDue = useQueryParam("due");
  const qQ = useQueryParam("q");
  const filters = useMemo<TaskFilters>(() => ({ goal: qGoal ?? "", team: qTeam ?? "", assignee: qAssignee ?? "", type: qType ?? "", sprint: qSprint ?? "", state: qState ?? "", priority: qPriority ?? "", due: qDue ?? "", q: qQ ?? "" }), [qGoal, qTeam, qAssignee, qType, qSprint, qState, qPriority, qDue, qQ]);
  const view: TaskView | null = isTaskView(qView) ? qView : null;

  // 地址栏没有 ?view= 时解析默认页签并写回地址栏（之后一切以地址栏为准）；effect 只在客户端跑，这里可以直接读 localStorage。
  // 显示偏好还没拉到时先等（loaded 变化会再跑一次）
  const resolving = useRef(false);
  const prefsLoaded = usePreferences().loaded;
  useEffect(() => {
    if (view || resolving.current || !prefsLoaded) return;
    const remembered = readRemembered();
    if (remembered) {
      setQueryParams({ view: remembered });
      return;
    }
    const prefs = getPreferences();
    if (prefs.sources.default_task_view !== "default" && isTaskView(prefs.default_task_view)) {
      setQueryParams({ view: prefs.default_task_view });
      return;
    }
    resolving.current = true;
    void defaultViewByRole().then((v) => {
      resolving.current = false;
      if (!isTaskView(new URLSearchParams(window.location.search).get("view"))) setQueryParams({ view: v });
    });
  }, [view, prefsLoaded]);
  const switchView = useCallback((v: TaskView) => {
    setQueryParams({ view: v });
    try {
      localStorage.setItem(TASK_VIEW_KEY, v);
    } catch {}
  }, []);
  useShortcutHandler("view", useCallback((v?: string) => { if (isTaskView(v)) switchView(v); }, [switchView]));
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

  // 新建任务：页头按钮、t c 快捷键、?new=1、甘特图拖拽草稿都开同一个抽屉；创建后各页签重新取数、列表里高亮新行
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

  // 任务抽屉：?task=<id>，可分享；关闭清掉参数。抽屉里的动作改了任务后各页签重新取数。
  const openTask = useCallback((id: string) => setQueryParams({ task: id }), []);
  const closeTask = useCallback(() => setQueryParams({ task: null }), []);
  const openFull = useCallback((id: string) => { markKeyboardIntent(); router.push(`/tasks/${encodeURIComponent(id)}/`); }, [router]);
  useTaskListNav(view, openTask, openFull);
  const bump = useCallback(() => setReloadKey((k) => k + 1), []);

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
          <ListView filters={filters} ctx={ctx} reloadKey={reloadKey} highlightId={highlight} onNew={openNew} onOpen={openTask} onClearFilters={clearFilters} />
        ) : view === "board" ? (
          <BoardView filters={filters} reloadKey={reloadKey} onOpen={openTask} />
        ) : view === "gantt" ? (
          <GanttView filters={filters} ctx={ctx} reloadKey={reloadKey} onCreate={setDraft} />
        ) : view === "sprints" ? (
          <SprintsView filters={filters} />
        ) : (
          <BacklogView filters={filters} ctx={ctx} reloadKey={reloadKey} onOpen={openTask} onNew={openNew} />
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
          bump();
        }}
        goals={flat}
        types={types.data ?? []}
        defaults={draft ?? undefined}
      />
      <TaskPeek id={qTask} onClose={closeTask} onChanged={bump} />
    </div>
  );
}
