"use client";
import { useMemo, useState, type PointerEvent as ReactPointerEvent } from "react";
import { api, isTerminal, type SprintDetail, type TaskSummary } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import { errorMessage, useExecutors, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { flattenGoals } from "@/components/GoalDrawer";
import { IconChevronLeft, IconChevronRight, IconSearch, IconTask } from "@/components/icons";
import { StateBadge } from "@/components/StateBadge";
import { PointsChips } from "@/components/board/PointsChips";
import { usePointerDrag } from "@/components/board/usePointerDrag";
import { isOverdue, TypeLabel } from "@/components/TaskTable";
import { useSession } from "@/components/AppShell";
import { useToast } from "@/components/toast";
import { Avatar, Button, ErrorBox, ListSkeleton, Panel, Select, TaskLink, cx, inputCls } from "@/components/ui";

/*
 * 迭代待办（规划视图）：左边「待办」= 不在任何迭代里、还没结束的任务（可按目标 / 团队筛），右边「本迭代」。
 * 拖动即加入 / 移出（POST /sprints/{id}/tasks、DELETE …/tasks/{task_id}），每行也有按钮；点数字给任务估工作量（PATCH /tasks/{id}）。
 * 乐观更新：拖过去立刻出现在另一边并打 pending，失败退回。已结束的迭代只读。
 */
const DROP_BACKLOG = "backlog";
const DROP_SPRINT = "sprint";

export function SprintPlanning({ sprint, onChanged }: { sprint: SprintDetail; onChanged: () => void }) {
  const toast = useToast();
  const { session } = useSession();
  const readOnly = sprint.status === "closed";
  const [goal, setGoal] = useState("");
  const [team, setTeam] = useState(sprint.team?.id ?? "");
  const [q, setQ] = useState("");
  const goals = useLoad(() => api.goals.list(), []);
  const ex = useExecutors();
  const all = useLoad(() => api.tasks.list({ goal: goal || undefined }), [goal]);
  const flat = useMemo(() => flattenGoals(goals.data ?? []), [goals.data]);
  const teams = useMemo(() => (ex.data ? new Map(ex.data.members.map((m) => [m.id, m.team_id])) : null), [ex.data]);
  const teamOf = (x: TaskSummary) => {
    if (!x.assignee || !teams) return null;
    if (x.assignee.kind === "member") return teams.get(x.assignee.id) ?? null;
    return x.assignee.owner_id ? (teams.get(x.assignee.owner_id) ?? null) : null;
  };

  // 本地两列：左 = 接口的候选去掉已在右边的；右 = 迭代任务 + 刚拖进来的
  const [inSprint, setInSprint] = useState<TaskSummary[]>(sprint.tasks);
  const [seenTasks, setSeenTasks] = useState(sprint.tasks);
  if (seenTasks !== sprint.tasks) {
    setSeenTasks(sprint.tasks);
    setInSprint(sprint.tasks);
  }
  const [pending, setPending] = useState<Set<string>>(() => new Set());
  const inIds = new Set(inSprint.map((x) => x.id));
  const needle = q.trim().toLowerCase();
  const backlog = (all.data ?? [])
    .filter((x) => !x.sprint && !isTerminal(x.state.label) && !inIds.has(x.id))
    .filter((x) => !team || teamOf(x) === team)
    .filter((x) => !needle || x.title.toLowerCase().includes(needle));

  const mark = (id: string, on: boolean) =>
    setPending((s) => {
      const n = new Set(s);
      if (on) n.add(id);
      else n.delete(id);
      return n;
    });
  const add = async (task: TaskSummary) => {
    if (readOnly || inIds.has(task.id)) return;
    setInSprint((xs) => [{ ...task, sprint: { id: sprint.id, name: sprint.name } }, ...xs]);
    mark(task.id, true);
    try {
      await api.sprints.addTasks(sprint.id, [task.id]);
      toast.ok(t("sprint.backlog.added", { title: task.title }));
      onChanged();
      all.reload();
    } catch (e) {
      setInSprint((xs) => xs.filter((x) => x.id !== task.id));
      toast.fail(t("toast.rollback", { reason: errorMessage(e) }));
    } finally {
      mark(task.id, false);
    }
  };
  const remove = async (task: TaskSummary) => {
    if (readOnly) return;
    setInSprint((xs) => xs.filter((x) => x.id !== task.id));
    mark(task.id, true);
    try {
      await api.sprints.removeTask(sprint.id, task.id);
      toast.ok(t("sprint.backlog.removed", { title: task.title }));
      onChanged();
      all.reload();
    } catch (e) {
      setInSprint((xs) => [task, ...xs]);
      toast.fail(t("toast.rollback", { reason: errorMessage(e) }));
    } finally {
      mark(task.id, false);
    }
  };
  const setPoints = async (task: TaskSummary, points: number | null) => {
    mark(task.id, true);
    const patch = (xs: TaskSummary[]) => xs.map((x) => (x.id === task.id ? { ...x, points } : x));
    setInSprint(patch);
    try {
      await api.tasks.update(task.id, { points });
      toast.ok(points === null ? t("task.pointsCleared", { title: task.title }) : t("task.pointsSaved", { title: task.title, n: points }));
      onChanged();
      all.reload();
    } catch (e) {
      setInSprint((xs) => xs.map((x) => (x.id === task.id ? { ...x, points: task.points } : x)));
      toast.fail(errorMessage(e));
    } finally {
      mark(task.id, false);
    }
  };

  const { drag, start } = usePointerDrag({
    onDrop: (id, target) => {
      if (!target) return;
      const fromSprint = inSprint.find((x) => x.id === id);
      const fromBacklog = backlog.find((x) => x.id === id);
      if (target === DROP_SPRINT && fromBacklog) void add(fromBacklog);
      if (target === DROP_BACKLOG && fromSprint) void remove(fromSprint);
    },
  });
  const dragged = drag ? (inSprint.find((x) => x.id === drag.id) ?? backlog.find((x) => x.id === drag.id)) : null;
  const draggedFrom = drag ? (inIds.has(drag.id) ? DROP_SPRINT : DROP_BACKLOG) : null;
  const totalPoints = inSprint.reduce((s, x) => s + (x.points ?? 0), 0);

  const row = (task: TaskSummary, side: "backlog" | "sprint", ghost = false) => (
    <div
      key={task.id}
      className="sp-item"
      data-lifting={!ghost && drag?.id === task.id ? "" : undefined}
      data-pending={pending.has(task.id) ? "" : undefined}
      tabIndex={ghost ? -1 : 0}
      onPointerDown={ghost || readOnly ? undefined : (e: ReactPointerEvent<HTMLDivElement>) => start(e, task.id)}
      onKeyDown={
        ghost || readOnly
          ? undefined
          : (e) => {
              if (e.key === "Enter" || e.key === " ") {
                if ((e.target as HTMLElement).closest("a, button")) return;
                e.preventDefault();
                void (side === "backlog" ? add(task) : remove(task));
              }
            }
      }
      aria-label={task.title}
    >
      <span className="sp-handle" aria-hidden="true"><IconTask size={14} /></span>
      <span className="min-w-0">
        <span className="sp-item-title"><TaskLink id={task.id} title={task.title} inline /></span>
        <span className="sp-item-sub">
          <TypeLabel type={task.type} title={task.type_title} />
          <StateBadge state={task.state} showLabel={false} led={false} />
          {task.assignee && <span className="inline-flex items-center gap-1"><Avatar name={task.assignee.name} kind={task.assignee.kind} size={14} />{task.assignee.name}</span>}
          {task.planned_end && <span className={cx("font-mono text-[11px]", isOverdue(task) && "text-danger")}>{fmtDate(task.planned_end)}</span>}
        </span>
      </span>
      <PointsChips value={task.points} onChange={(n) => void setPoints(task, n)} busy={pending.has(task.id)} size="sm" label={`${t("task.points")} · ${task.title}`} />
      {!readOnly && !ghost && (
        <span className="sp-item-actions">
          {side === "backlog" ? (
            <Button size="sm" variant="ghost" icon={<IconChevronRight />} onClick={() => void add(task)} disabled={pending.has(task.id)}>{t("sprint.backlog.add")}</Button>
          ) : (
            <Button size="sm" variant="ghost" icon={<IconChevronLeft />} onClick={() => void remove(task)} disabled={pending.has(task.id)}>{t("sprint.backlog.remove")}</Button>
          )}
        </span>
      )}
    </div>
  );

  return (
    <div>
      <p className="mb-3 text-caption text-ink-subtle">{readOnly ? t("sprint.backlog.closed") : t("sprint.backlog.hint")}</p>
      <div className="sp-plan">
        <Panel
          index={1}
          title={t("sprint.backlog.available")}
          telemetry={t("panel.rows", { n: backlog.length })}
          padded={false}
          actions={
            <>
              <label className="relative block w-36">
                <span className="pointer-events-none absolute top-1/2 left-2 -translate-y-1/2 text-ink-subtle"><IconSearch size={14} /></span>
                <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("sprint.backlog.search")} aria-label={t("sprint.backlog.search")} className={cx(inputCls, "h-7 w-full pl-7 text-caption")} />
              </label>
              <Select value={goal} onChange={(e) => setGoal(e.target.value)} className="h-7 w-36 text-caption" aria-label={t("taskTable.goal")}>
                <option value="">{t("tasks.allGoals")}</option>
                {flat.map(({ goal: g, depth }) => <option key={g.id} value={g.id}>{"　".repeat(depth)}{g.title}</option>)}
              </Select>
              <Select value={team} onChange={(e) => setTeam(e.target.value)} className="h-7 w-28 text-caption" aria-label={t("sprint.team")}>
                <option value="">{t("board.allTeams")}</option>
                {(session?.teams ?? []).map((tm) => <option key={tm.id} value={tm.id}>{tm.name}</option>)}
              </Select>
            </>
          }
        >
          {all.loading && !all.data ? (
            <ListSkeleton rows={4} />
          ) : all.error ? (
            <div className="p-4"><ErrorBox message={all.error} onRetry={all.reload} /></div>
          ) : (
            <div className="sp-list" data-drop={DROP_BACKLOG} data-over={drag && drag.over === DROP_BACKLOG && draggedFrom === DROP_SPRINT ? "" : undefined} data-over-label={t("sprint.backlog.dropRemove")} data-disabled={readOnly ? "" : undefined}>
              {backlog.map((x) => row(x, "backlog"))}
              {backlog.length === 0 && <p className="py-8 text-center text-caption text-ink-tertiary">{t("sprint.backlog.emptyLeft")}</p>}
            </div>
          )}
        </Panel>
        <Panel index={2} icon={<IconTask />} title={t("sprint.backlog.inSprint")} telemetry={t("sprint.backlog.total", { points: totalPoints, n: inSprint.length })} padded={false}>
          <div className="sp-list" data-drop={DROP_SPRINT} data-over={drag && drag.over === DROP_SPRINT && draggedFrom === DROP_BACKLOG ? "" : undefined} data-over-label={t("sprint.backlog.dropAdd")} data-disabled={readOnly ? "" : undefined}>
            {inSprint.map((x) => row(x, "sprint"))}
            {inSprint.length === 0 && <p className="py-8 text-center text-caption text-ink-tertiary">{t("sprint.backlog.emptyRight")}</p>}
          </div>
        </Panel>
      </div>
      {drag && dragged && (
        <div className="bd-ghost sp-ghost" style={{ width: drag.rect.w, transform: `translate(${drag.rect.x + drag.dx}px, ${drag.rect.y + drag.dy}px)` }} aria-hidden="true">
          {row(dragged, draggedFrom === DROP_SPRINT ? "sprint" : "backlog", true)}
        </div>
      )}
    </div>
  );
}
