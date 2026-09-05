"use client";
import Link from "next/link";
import { useMemo } from "react";
import { api, type Goal } from "@/lib/api";
import { fmtDate, fmtMoney } from "@/lib/format";
import { useAction, useLoad, useRouteId, useSprout } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { isAcceptanceWait, useTaskTypeIndex } from "@/lib/states";
import { goalSummary, tallyGoals } from "../goalStatus";
import { EventList } from "@/components/EventList";
import { IconCheck, IconGoal, IconLog, IconTask } from "@/components/icons";
import { ArcGauge } from "@/components/instruments/ArcGauge";
import { Odometer } from "@/components/instruments/Odometer";
import { TaskTable } from "@/components/TaskTable";
import { Avatar, Button, DescList, DetailSkeleton, ErrorBox, IdLine, ListSkeleton, PageHeader, Panel, ProgressBar, TableSkeleton, Tag, cx } from "@/components/ui";

export function GoalDetail() {
  const id = useRouteId();
  return <GoalDetailBody id={id} />;
}

function GoalDetailBody({ id }: { id: string | null }) {
  const { session } = useSession();
  const goal = useLoad(() => (id ? api.goals.get(id) : Promise.reject(new Error(t("goal.missing")))), [id]);
  // 进度与任务数是整棵子树的汇总，任务表也要跟着看整棵子树（接口的 goal= 是精确匹配），否则两处对不上
  const tasks = useLoad(() => api.tasks.list({ limit: 500 }), [id]);
  const events = useLoad(() => api.events.list({ limit: 200 }), [id]);
  const { busy, run } = useAction();
  const currency = session?.organization.currency;
  const sprout = useSprout(goal.data ?? { achieved: false, progress: 0 });
  const types = useTaskTypeIndex();
  const subtree = useMemo(() => {
    const ids = new Set<string>();
    const walk = (x: Goal) => { ids.add(x.id); (x.children ?? []).forEach(walk); };
    if (goal.data) walk(goal.data);
    return ids;
  }, [goal.data]);
  const mine = useMemo(() => (tasks.data ?? []).filter((x) => x.goal_id && subtree.has(x.goal_id)), [tasks.data, subtree]);
  const tally = useMemo(() => (goal.data ? tallyGoals([goal.data], tasks.data ?? [], (x) => isAcceptanceWait(x.state, types[x.type]?.workflow)) : new Map()), [goal.data, tasks.data, types]);

  if (!id || (goal.loading && !goal.data)) return <DetailSkeleton />;
  if (goal.error || !goal.data) return <ErrorBox message={goal.error ?? t("goal.notFound")} onRetry={goal.reload} />;
  const g = goal.data;
  const over = g.budget !== null && g.cost > g.budget;
  const grown = g.achieved || g.progress >= 100;
  const sum = goalSummary(g, tally.get(g.id));

  const toggleAchieved = async () => {
    if (await run("achieve", () => api.goals.update(g.id, { achieved: !g.achieved }), g.achieved ? t("toast.unachieved") : t("toast.achieved"))) goal.reload();
  };
  // 面板序号眉标：01 进度、02 基本信息（右栏顶部，视觉上与进度并列），其余按 DOM 顺序从 03 起递增（子目标面板是条件渲染）
  let n = 2;
  const idx = () => ++n;

  return (
    <div>
      <PageHeader
        breadcrumb={<><Link href="/goals/" className="hover:text-accent-hover">{t("goals.title")}</Link> / <span className="telemetry">{g.id}</span></>}
        title={<span className="inline-flex flex-wrap items-center gap-2"><span className={cx("inline-flex", grown ? "text-success" : "text-ink-subtle")} data-motion={sprout ? "sprout" : undefined} aria-hidden="true"><IconGoal size={22} stage={grown ? "grown" : "bud"} /></span>{g.title}{g.achieved && <Tag tone="success">{t("goals.achieved")}</Tag>}{over && <Tag tone="danger">{t("goals.overBudget")}</Tag>}</span>}
        description={g.description || t("goal.noDescription")}
        actions={
          <>
            <Link href={`/tasks/?goal=${encodeURIComponent(g.id)}`} className="inline-flex"><Button icon={<IconTask />} tabIndex={-1}>{t("goal.viewTasks")}</Button></Link>
            <Button variant={g.achieved ? "default" : "primary"} icon={g.achieved ? undefined : <IconCheck />} onClick={() => void toggleAchieved()} disabled={busy === "achieve"}>{g.achieved ? t("goal.unachieve") : t("goal.achieve")}</Button>
          </>
        }
      />

      {/* 主栏自适应，侧栏固定 380px（≥1920 时 420px） */}
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_380px] 3xl:grid-cols-[minmax(0,1fr)_420px]">
        <div className="space-y-4">
          <Panel index={1} title={t("goals.progress")}>
            {/* 进度 = 96px 弧形仪表（舰内系统 v3 §3），旁边是完成数；预算仍用刻度进度条（可能超过 100%） */}
            <div className="flex items-center gap-4">
              <ArcGauge size={96} value={g.progress} tone={g.achieved ? "success" : "accent"} label={t("goals.progress")} delay={sprout ? 480 : 0} />
              <span className="min-w-0">
                <span className="block text-body text-ink-muted">{t("goal.tasksDone", { done: g.done_task_count, total: g.task_count })}</span>
                {/* 下一步该干什么（DESIGN.md §9），与目标列表同一套判定 */}
                <span className={cx("mt-1 block text-body", sum.tone === "danger" ? "text-danger" : sum.tone === "warning" ? "text-warning" : sum.tone === "accent" ? "text-accent-hover" : "text-ink-subtle")}>{sum.text}</span>
              </span>
            </div>
            {g.budget !== null && (
              <div className="mt-4">
                <div className="mb-1 flex justify-between text-body">
                  <span className="text-ink-muted">{t("goal.budget")}</span>
                  <span className={cx("tabular-nums", over && "text-danger")}><Odometer value={fmtMoney(g.cost, currency)} /> / <Odometer value={fmtMoney(g.budget, currency)} /></span>
                </div>
                <ProgressBar value={g.budget ? (g.cost / g.budget) * 100 : 0} tone={over ? "danger" : "accent"} />
                {over && <p className="mt-1 text-caption text-danger">{t("goal.overBudgetHint")}</p>}
              </div>
            )}
          </Panel>

          {g.children.length > 0 && (
            <Panel index={idx()} icon={<IconGoal />} title={t("goal.children")} telemetry={t("panel.rows", { n: g.children.length })} padded={false}>
              <ul>
                {g.children.map((c) => (
                  <li key={c.id} className="flex h-10 items-center gap-4 border-b border-hairline px-4 last:border-b-0 hover:bg-surface-2">
                    <Link href={`/goals/${encodeURIComponent(c.id)}/`} className="min-w-0 flex-1 truncate font-medium hover:text-accent-hover" title={c.title}>{c.title}</Link>
                    <span className="inline-flex w-28 items-center gap-1.5 text-caption text-ink-muted"><Avatar name={c.owner.name} size={16} /><span className="truncate">{c.owner.name}</span></span>
                    <div className="hidden w-32 sm:block"><ProgressBar value={c.progress} tone={c.achieved ? "success" : "accent"} /></div>
                    <span className="w-9 text-right text-caption tabular-nums text-ink-muted">{c.progress}%</span>
                  </li>
                ))}
              </ul>
            </Panel>
          )}

          <Panel index={idx()} icon={<IconTask />} title={t("goal.tasks")} telemetry={tasks.data ? t("panel.rows", { n: mine.length }) : undefined} padded={false} actions={<Link href={`/tasks/?goal=${encodeURIComponent(g.id)}`} className="text-caption text-ink-muted hover:text-accent-hover">{t("goal.filterTasks")}</Link>}>
            {tasks.loading && !tasks.data ? <TableSkeleton rows={3} cols={6} /> : tasks.error ? <div className="p-4"><ErrorBox message={tasks.error} onRetry={tasks.reload} /></div> : <TaskTable tasks={mine} currency={currency} showGoal={g.children.length > 0} emptyText={t("goal.noTasks")} onChanged={tasks.reload} />}
          </Panel>
        </div>

        <div className="space-y-4">
          <Panel index={2} title={t("goal.info")}>
            <DescList
              items={[
                [t("common.id"), <IdLine key="id" id={g.id} />],
                [t("goal.owner"), <span key="o" className="inline-flex items-center gap-1.5"><Avatar name={g.owner.name} />{g.owner.name}</span>],
                [t("goal.parent"), g.parent_id ? <Link key="p" href={`/goals/${encodeURIComponent(g.parent_id)}/`} className="telemetry hover:text-accent-hover">{g.parent_id}</Link> : t("common.none")],
                [t("goal.plan"), `${fmtDate(g.planned_start, true)} – ${fmtDate(g.planned_end, true)}`],
                [t("goal.actual"), `${fmtDate(g.actual_start, true)} – ${g.actual_end ? fmtDate(g.actual_end, true) : t("common.inProgress")}`],
                [t("goal.cost"), <Odometer key="c" value={fmtMoney(g.cost, currency)} />],
                [t("goal.budget"), g.budget === null ? t("goal.budgetUnset") : <Odometer key="b" value={fmtMoney(g.budget, currency)} />],
              ]}
            />
          </Panel>
          <Panel index={idx()} icon={<IconLog />} title={t("goal.events")}>
            {events.loading && !events.data ? <ListSkeleton rows={4} /> : <EventList events={(events.data ?? []).filter((e) => e.goal_id === g.id)} poll={{ limit: 200, filter: (e) => e.goal_id === g.id }} />}
          </Panel>
        </div>
      </div>
    </div>
  );
}
