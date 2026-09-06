"use client";
import Link from "next/link";
import { useMemo, useState } from "react";
import { api, type Goal, type Milestone, type TaskState } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import { useExecutors, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { isAcceptanceWait } from "@/lib/states";
import { EventList } from "@/components/EventList";
import { flattenGoals } from "@/components/GoalDrawer";
import { IconExternal, IconGoal } from "@/components/icons";
import { StateBadge } from "@/components/StateBadge";
import { isOverdue, OverdueTag, PriorityTag, TypeLabel } from "@/components/TaskTable";
import { Button, Drawer, ErrorBox, ExecutorName, ListSkeleton, Tag, TaskNumber, cx } from "@/components/ui";
import { ActionBar, AssignDialog } from "./TaskActions";

/**
 * 任务抽屉（DESIGN.md §17「执行简报」的中间一档密度）：列表 / 看板里点任务先开它，只有 ①现在要做什么 与 ②为什么做，
 * 加最近 5 条动态和「打开完整页」。地址栏带 ?task=<id>，可分享；Esc 或关闭按钮清掉参数。
 * 动作栏与详情页同一份（TaskActions），这里能做的事和详情页一致。
 */
export function TaskPeek({ id, onClose, onChanged }: { id: string | null; onClose: () => void; onChanged?: () => void }) {
  // 关闭过渡期间 id 已经是 null，仍按最后一个 id 画，不闪空
  const [lastId, setLastId] = useState(id);
  if (id && id !== lastId) setLastId(id);
  const tid = lastId;
  const task = useLoad(() => (tid ? api.tasks.get(tid) : Promise.resolve(null)), [tid]);
  const wf = useLoad(() => (tid ? api.tasks.workflow(tid) : Promise.resolve(null)), [tid]);
  const brief = useLoad(() => (tid ? api.tasks.brief(tid).catch(() => null) : Promise.resolve(null)), [tid]);
  const events = useLoad(() => (tid ? api.events.list({ task: tid, limit: 5 }) : Promise.resolve([])), [tid]);
  const typeName = task.data?.type ?? null;
  const type = useLoad(() => (typeName ? api.taskTypes.get(typeName).catch(() => null) : Promise.resolve(null)), [typeName]);
  const goalTree = useLoad(() => api.goals.list().catch(() => [] as Goal[]), []);
  const goalId = task.data?.goal_id ?? null;
  const milestones = useLoad(() => (goalId ? api.milestones.list(goalId).catch(() => [] as Milestone[]) : Promise.resolve([] as Milestone[])), [goalId]);
  const ex = useExecutors();
  const [optimistic, setOptimistic] = useState<TaskState | null>(null);
  const [assigning, setAssigning] = useState(false);
  const goalById = useMemo(() => new Map(flattenGoals(goalTree.data ?? []).map((g) => [g.goal.id, g.goal])), [goalTree.data]);
  // 目标链：从所属目标一路向上（根在前），每级可点
  const chain = useMemo(() => {
    const out: Goal[] = [];
    for (let g = goalId ? goalById.get(goalId) : undefined; g; g = g.parent_id ? goalById.get(g.parent_id) : undefined) out.unshift(g);
    return out;
  }, [goalId, goalById]);
  const reloadAll = () => { task.reload(); wf.reload(); events.reload(); onChanged?.(); };
  const x = task.data;
  const pendingState = x && optimistic && optimistic.name !== x.state.name ? optimistic : null;
  const shownState = x ? pendingState ?? x.state : null;
  const nextMilestone = (milestones.data ?? []).find((m) => m.status !== "reached") ?? null;
  const slots = x ? Object.entries(x.participants) : [];
  const full = tid ? `/tasks/${encodeURIComponent(tid)}/` : "/tasks/";

  return (
    <Drawer
      open={!!id}
      onClose={onClose}
      title={x ? <span className="inline-flex min-w-0 items-baseline gap-1.5"><TaskNumber n={x.number} copy /><Link href={full} className="min-w-0 hover:text-accent-hover">{x.title}</Link></span> : t("common.loading")}
      description={x ? `${x.type_title} · ${shownState?.title ?? x.state.title}` : undefined}
      footer={<><Button variant="ghost" onClick={onClose}>{t("common.close")}</Button><Link href={full} className="inline-flex"><Button variant="primary" icon={<IconExternal />} tabIndex={-1}>{t("task.openFull")}</Button></Link></>}
    >
      {task.loading && !x ? <ListSkeleton rows={6} /> : task.error || !x || !shownState ? <ErrorBox message={task.error ?? t("task.notFound")} onRetry={task.reload} /> : (
        <div className="space-y-5" data-task-peek={x.id}>
          <div className="flex flex-wrap items-center gap-2">
            <TypeLabel type={x.type} title={x.type_title} tag />
            <StateBadge state={shownState} pending={!!pendingState} accept={isAcceptanceWait(shownState, type.data?.workflow)} />
            {x.priority !== "normal" && <PriorityTag priority={x.priority} />}
            {isOverdue(x) && <OverdueTag>{t("task.overdue")}</OverdueTag>}
          </div>
          <ActionBar compact task={x} wf={wf.data} wfError={wf.error} stateTitles={type.data?.workflow.states} onOptimistic={setOptimistic} onDone={reloadAll} onAssign={() => setAssigning(true)} />
          <AssignDialog task={x} open={assigning} onClose={() => setAssigning(false)} executors={ex.data?.executors ?? []} onDone={reloadAll} />

          {/* ① 现在要做什么 */}
          <section>
            <h3 className="brief-eyebrow"><span className="text-telemetry">01</span>{t("brief.now")}</h3>
            <p className={cx("whitespace-pre-wrap text-body", x.description ? "text-ink" : "text-ink-subtle")}>{x.description || t("task.noDescription")}</p>
            {brief.data?.agent_instructions && (
              <div className="mt-3 rounded-md border border-hairline bg-surface-2 px-3 py-2">
                <div className="eyebrow mb-1 normal-case text-ink-subtle">{t("brief.instructions", { type: x.type_title })}</div>
                <p className="whitespace-pre-wrap text-body text-ink-muted">{brief.data.agent_instructions}</p>
              </div>
            )}
            <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-body">
              <dt className="text-ink-subtle">{t("task.assignee")}</dt>
              <dd><ExecutorName executor={x.assignee} empty={t("task.unclaimed")} /></dd>
              {slots.map(([slot, p]) => (
                <div key={slot} className="contents">
                  <dt className="text-ink-subtle">{p.title}</dt>
                  <dd>{p.executor ? <ExecutorName executor={p.executor} /> : <span className="text-ink-subtle">{x.pending_participant === slot ? t("task.pendingSlot") : t("task.vacant")}</span>}</dd>
                </div>
              ))}
              <dt className="text-ink-subtle">{t("task.reviewer")}</dt>
              <dd><ExecutorName executor={x.reviewer} /></dd>
            </dl>
          </section>

          {/* ② 为什么做 */}
          <section>
            <h3 className="brief-eyebrow"><span className="text-telemetry">02</span>{t("brief.why")}</h3>
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-body">
              <dt className="text-ink-subtle">{t("task.goalRow")}</dt>
              <dd>
                {chain.length ? (
                  <span className="flex flex-wrap items-center gap-x-1 gap-y-0.5">
                    {chain.map((g, i) => (
                      <span key={g.id} className="inline-flex items-center gap-1">
                        {i > 0 && <span className="text-ink-tertiary">›</span>}
                        <Link href={`/goals/${encodeURIComponent(g.id)}/`} className={cx("inline-flex items-center gap-1 hover:text-accent-hover", i === chain.length - 1 ? "text-ink" : "text-ink-muted")}>
                          {i === chain.length - 1 && <IconGoal size={14} className="text-ink-subtle" />}{g.title}
                        </Link>
                      </span>
                    ))}
                  </span>
                ) : x.goal ? <Link href={`/goals/${encodeURIComponent(x.goal.id)}/`} className="hover:text-accent-hover">{x.goal.title}</Link> : <span className="text-ink-subtle">{t("taskDialog.noGoal")}</span>}
              </dd>
              {nextMilestone && (
                <>
                  <dt className="text-ink-subtle">{t("brief.milestone")}</dt>
                  <dd className="flex flex-wrap items-center gap-2"><span>{nextMilestone.title}</span><span className="telemetry text-ink-subtle">{fmtDate(nextMilestone.due_on, true)}</span>{nextMilestone.status === "overdue" && <Tag tone="danger">{t("ms.status.overdue")}</Tag>}</dd>
                </>
              )}
              <dt className="text-ink-subtle">{t("taskDialog.priority")}</dt>
              <dd><PriorityTag priority={x.priority} /></dd>
              <dt className="text-ink-subtle">{t("task.plan")}</dt>
              <dd className={cx("telemetry", isOverdue(x) && "text-danger")}>{fmtDate(x.planned_start)} – {fmtDate(x.planned_end)}</dd>
              <dt className="text-ink-subtle">{t("task.sprint")}</dt>
              <dd>{x.sprint ? <Link href={`/sprints/${encodeURIComponent(x.sprint.id)}/`} className="hover:text-accent-hover">{x.sprint.name}</Link> : <span className="text-ink-subtle">{t("task.noSprint")}</span>}</dd>
            </dl>
          </section>

          {/* 最近动态：最多 5 条，全部在完整页 */}
          <section>
            <h3 className="brief-eyebrow"><span className="text-telemetry">03</span>{t("brief.recent")}</h3>
            {events.loading && !events.data ? <ListSkeleton rows={3} /> : <EventList events={(events.data ?? []).slice(0, 5)} currentTaskId={x.id} showTask={false} poll={false} />}
            <Link href={full} className="mt-2 inline-block text-caption text-ink-subtle hover:text-accent-hover">{t("brief.moreInFull")} →</Link>
          </section>
        </div>
      )}
    </Drawer>
  );
}
