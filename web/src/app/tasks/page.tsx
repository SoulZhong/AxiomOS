"use client";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { useExecutors, useHighlight, useLoad, useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { STATE_LABELS, stateLabel } from "@/lib/terms";
import { useSession } from "@/components/AppShell";
import { flattenGoals } from "@/components/GoalDrawer";
import { IconPlus, IconSearch, IconTask } from "@/components/icons";
import { useShortcutHandler } from "@/components/shortcuts";
import { isOverdue, TaskTable } from "@/components/TaskTable";
import { TaskDrawer } from "@/components/TaskDrawer";
import { Button, ErrorBox, Kbd, PageHeader, Panel, Select, StatChips, TableSkeleton, cx, inputCls } from "@/components/ui";

export default function TasksPage() {
  const { session } = useSession();
  const qGoal = useQueryParam("goal");
  const qAssignee = useQueryParam("assignee");
  const qFocus = useQueryParam("focus");
  const qNew = useQueryParam("new");
  const [goal, setGoal] = useState("");
  const [assignee, setAssignee] = useState("");
  const [state, setState] = useState("");
  const [type, setType] = useState("");
  const [q, setQ] = useState("");
  const [chip, setChip] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [highlight, mark] = useHighlight();
  const searchRef = useRef<HTMLInputElement>(null);
  // 地址栏参数首次出现时写入筛选（在渲染期同步，避免在 effect 里 setState）
  const [seenQuery, setSeenQuery] = useState<string | null>(null);
  const queryKey = `${qGoal ?? ""}|${qAssignee ?? ""}|${qNew ?? ""}`;
  if (queryKey !== seenQuery) {
    setSeenQuery(queryKey);
    if (qGoal) setGoal(qGoal);
    if (qAssignee) setAssignee(qAssignee);
    if (qNew) setCreating(true);
  }
  useEffect(() => {
    if (qFocus === "search") searchRef.current?.focus();
  }, [qFocus]);

  const goals = useLoad(() => api.goals.list(), []);
  const types = useLoad(() => api.taskTypes.list(), []);
  const ex = useExecutors();
  const tasks = useLoad(() => api.tasks.list({ goal: goal || undefined, assignee: assignee || undefined, state: state || undefined, type: type || undefined }), [goal, assignee, state, type]);
  const flat = useMemo(() => flattenGoals(goals.data ?? []), [goals.data]);

  // 快捷键：/ 聚焦搜索，n 新建
  useShortcutHandler("search", useCallback(() => searchRef.current?.focus(), []));
  useShortcutHandler("new-task", useCallback(() => setCreating(true), []));

  const all = tasks.data ?? [];
  const open = all.filter((x) => x.state.label !== "terminal_success" && x.state.label !== "terminal_failure");
  const active = all.filter((x) => x.state.label === "active");
  const overdue = all.filter(isOverdue);
  const waiting = all.filter((x) => x.state.label === "waiting");
  const needle = q.trim().toLowerCase();
  const shown = (chip === "active" ? active : chip === "overdue" ? overdue : chip === "waiting" ? waiting : chip === "open" ? open : all).filter((x) => !needle || x.title.toLowerCase().includes(needle) || x.id.toLowerCase().includes(needle));
  const filtered = !!(goal || assignee || state || type || q || chip);
  const clear = () => { setGoal(""); setAssignee(""); setState(""); setType(""); setQ(""); setChip(null); };

  return (
    <div>
      <PageHeader title={t("tasks.title")} description={t("tasks.description")} actions={<Button variant="primary" icon={<IconPlus />} onClick={() => setCreating(true)}>{t("tasks.new")}</Button>} />

      <StatChips
        className="mb-4"
        value={chip}
        onChange={setChip}
        total={all.length}
        items={[
          { key: "open", label: t("chips.open"), value: open.length },
          { key: "active", label: t("home.active"), value: active.length, tone: "accent" },
          { key: "overdue", label: t("home.overdue"), value: overdue.length, tone: "danger" },
          { key: "waiting", label: t("home.waitingReview"), value: waiting.length, tone: "warning" },
        ]}
      />

      {/* 过滤条：一行内联控件 */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <label className="relative block w-full sm:w-60">
          <span className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-subtle"><IconSearch /></span>
          <input ref={searchRef} value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("tasks.search")} aria-label={t("tasks.search")} className={cx(inputCls, "w-full pl-8 pr-9")} data-shortcut-search />
          <span className="pointer-events-none absolute top-1/2 right-2 -translate-y-1/2"><Kbd>/</Kbd></span>
        </label>
        <Select value={goal} onChange={(e) => setGoal(e.target.value)} className="w-52" aria-label={t("taskTable.goal")}>
          <option value="">{t("tasks.allGoals")}</option>
          {flat.map(({ goal: g, depth }) => <option key={g.id} value={g.id}>{"　".repeat(depth)}{g.title}</option>)}
        </Select>
        <Select value={assignee} onChange={(e) => setAssignee(e.target.value)} className="w-44" aria-label={t("taskTable.assignee")}>
          <option value="">{t("tasks.allAssignees")}</option>
          <option value="me">{t("tasks.me")}</option>
          {(ex.data?.executors ?? []).map((x) => <option key={x.id} value={x.id}>{x.name}{x.kind === "agent" ? t("common.agentSuffix") : ""}</option>)}
        </Select>
        <Select value={state} onChange={(e) => setState(e.target.value)} className="w-36" aria-label={t("taskTable.state")}>
          <option value="">{t("tasks.allLabels")}</option>
          {STATE_LABELS.map((l) => <option key={l} value={l}>{stateLabel(l)}</option>)}
        </Select>
        <Select value={type} onChange={(e) => setType(e.target.value)} className="w-36" aria-label={t("taskTable.type")}>
          <option value="">{t("tasks.allTypes")}</option>
          {(types.data ?? []).map((x) => <option key={x.name} value={x.name}>{x.title}</option>)}
        </Select>
        {filtered && <Button variant="ghost" size="md" onClick={clear}>{t("common.clearFilters")}</Button>}
        <span className="ml-auto text-caption text-ink-subtle">{t("tasks.count", { n: shown.length })}</span>
      </div>

      <Panel index={1} icon={<IconTask />} title={t("nav.tasks")} telemetry={tasks.data ? t("panel.rows", { n: shown.length }) : undefined} padded={false}>
        {tasks.loading && !tasks.data ? <TableSkeleton rows={6} cols={7} /> : tasks.error ? <div className="p-4"><ErrorBox message={tasks.error} onRetry={tasks.reload} /></div> : (
          <TaskTable tasks={shown} currency={session?.organization.currency} highlightId={highlight} onChanged={tasks.reload} emptyText={filtered ? t("tasks.noMatch") : undefined} emptyAction={filtered ? <Button onClick={clear}>{t("common.clearFilters")}</Button> : <Button variant="primary" icon={<IconPlus />} onClick={() => setCreating(true)}>{t("tasks.new")}</Button>} />
        )}
      </Panel>
      <TaskDrawer open={creating} onClose={() => setCreating(false)} onCreated={(task) => { mark(task.id); tasks.reload(); }} goals={flat} types={types.data ?? []} defaults={{ goal_id: goal || null }} />
    </div>
  );
}
