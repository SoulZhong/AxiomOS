"use client";
import Link from "next/link";
import { useMemo, useState } from "react";
import { api, type Goal, type GoalInput } from "@/lib/api";
import { fmtDate, fmtMoney, fmtNumber } from "@/lib/format";
import { useAction, useExecutors, useLoad, useRouteId, useSprout } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { isAcceptanceWait, useTaskTypeIndex } from "@/lib/states";
import { goalSummary, tallyGoals } from "../goalStatus";
import { EventList } from "@/components/EventList";
import { flattenGoals } from "@/components/GoalDrawer";
import { IconCheck, IconGoal, IconLog, IconTask } from "@/components/icons";
import { InlineDate, InlineField, InlineNumber, InlineSelect, InlineTable, InlineText, InlineTitle, useInlineSaves } from "@/components/inline";
import { ArcGauge } from "@/components/instruments/ArcGauge";
import { Odometer } from "@/components/instruments/Odometer";
import { TaskTable } from "@/components/TaskTable";
import { MilestonePanel } from "./MilestonePanel";
import { Avatar, Button, DetailSkeleton, EnergyLine, ErrorBox, IdLine, ListSkeleton, Panel, ProgressBar, TableSkeleton, Tag, cx } from "@/components/ui";

export function GoalDetail() {
  const id = useRouteId();
  return <GoalDetailBody id={id} />;
}

/** 货币符号（¥ / $ …）：预算输入框前缀 */
const currencySymbol = (currency?: string) => fmtMoney(0, currency).replace(/[\d.,\s]/g, "");

function GoalDetailBody({ id }: { id: string | null }) {
  const { session } = useSession();
  const goal = useLoad(() => (id ? api.goals.get(id) : Promise.reject(new Error(t("goal.missing")))), [id]);
  // 进度与任务数是整棵子树的汇总，任务表也要跟着看整棵子树（接口的 goal= 是精确匹配），否则两处对不上
  const tasks = useLoad(() => api.tasks.list({ limit: 500 }), [id]);
  const events = useLoad(() => api.events.list({ limit: 200 }), [id]);
  // 整棵目标树：上级目标的下拉、负责人链（谁能改）、以及"上级目标"一行显示名字而不是 ID
  const tree = useLoad(() => api.goals.list().catch(() => [] as Goal[]), [id]);
  const flat = useMemo(() => flattenGoals(tree.data ?? []), [tree.data]);
  const byId = useMemo(() => new Map(flat.map((f) => [f.goal.id, f.goal])), [flat]);
  const ex = useExecutors();
  const { busy, run } = useAction();
  const currency = session?.organization.currency;
  const types = useTaskTypeIndex();
  const saves = useInlineSaves();

  // ---------- 就地编辑（DESIGN.md §15）：乐观更新后的目标；服务端返回即以它为准，重新加载时清掉 ----------
  const [local, setLocal] = useState<Goal | null>(null);
  const [seen, setSeen] = useState<Goal | null>(goal.data);
  if (seen !== goal.data) { setSeen(goal.data); setLocal(null); }
  const shown = local ?? goal.data;
  const sprout = useSprout(shown ?? { achieved: false, progress: 0 });
  const me = session?.member.id;
  // 谁能改：本目标或任一上级目标的负责人、组织负责人 —— 与后端 canEditGoal 一致
  const canEdit = useMemo(() => {
    if (!shown || !session) return false;
    if (session.is_owner) return true;
    for (let g: Goal | undefined = shown; g; g = g.parent_id ? byId.get(g.parent_id) : undefined) if (g.owner.id === me) return true;
    return false;
  }, [shown, session, byId, me]);
  const editPlan = canEdit && !shown?.achieved; // 标题与计划类字段：已达成的只剩描述
  const editNotes = canEdit;

  const subtree = useMemo(() => {
    const ids = new Set<string>();
    const walk = (x: Goal) => { ids.add(x.id); (x.children ?? []).forEach(walk); };
    if (goal.data) walk(goal.data);
    return ids;
  }, [goal.data]);
  const mine = useMemo(() => (tasks.data ?? []).filter((x) => x.goal_id && subtree.has(x.goal_id)), [tasks.data, subtree]);
  const tally = useMemo(() => (goal.data ? tallyGoals([goal.data], tasks.data ?? [], (x) => isAcceptanceWait(x.state, types[x.type]?.workflow)) : new Map()), [goal.data, tasks.data, types]);
  // 上级目标候选：除自己和自己的子孙
  const parentOptions = useMemo(() => {
    if (!shown) return [];
    return flat.filter((f) => !subtree.has(f.goal.id)).map((f) => ({ value: f.goal.id, label: f.goal.title, depth: f.depth }));
  }, [flat, subtree, shown]);

  if (!id || (goal.loading && !goal.data)) return <DetailSkeleton />;
  if (goal.error || !goal.data || !shown) return <ErrorBox message={goal.error ?? t("goal.notFound")} onRetry={goal.reload} />;
  const g = shown;
  const over = g.budget !== null && g.cost > g.budget;
  const grown = g.achieved || g.progress >= 100;
  const sum = goalSummary(g, tally.get(g.id));

  /** 乐观更新 + 一个请求；失败回退这几个字段并抛出（标题 / 描述控件自己显示原因） */
  const save = async (body: Partial<GoalInput>, optimisticFields: Partial<Goal>) => {
    const base = g;
    const orig = Object.fromEntries(Object.keys(optimisticFields).map((k) => [k, base[k as keyof Goal]])) as Partial<Goal>;
    setLocal({ ...base, ...optimisticFields });
    try {
      const res = await api.goals.update(base.id, body);
      setLocal(res);
      events.reload();
      if (body.parent_id !== undefined) tree.reload();
      return res;
    } catch (e) {
      setLocal((prev) => ({ ...(prev ?? base), ...orig }));
      throw e;
    }
  };
  const patch = (key: string, body: Partial<GoalInput>, optimisticFields: Partial<Goal>) => void saves.run(key, () => save(body, optimisticFields));
  const st = (key: string) => saves.get(key);

  const toggleAchieved = async () => {
    if (await run("achieve", () => api.goals.update(g.id, { achieved: !g.achieved }), g.achieved ? t("toast.unachieved") : t("toast.achieved"))) { goal.reload(); events.reload(); }
  };
  const members = ex.data?.members ?? [];
  const memberOptions = members.map((m) => ({ value: m.id, label: m.name, icon: <Avatar name={m.name} size={16} /> }));
  // 团队按层级缩进
  const teams = session?.teams ?? [];
  const teamDepth = (tid: string | null): number => { let d = 0; for (let cur = teams.find((x) => x.id === tid); cur?.parent_id; cur = teams.find((x) => x.id === cur!.parent_id)) d++; return d; };
  const teamOptions = [...teams].sort((a, b) => teamDepth(a.id) - teamDepth(b.id)).map((tm) => ({ value: tm.id, label: tm.name, depth: teamDepth(tm.id) }));
  const teamName = g.team_id ? teams.find((x) => x.id === g.team_id)?.name ?? g.team_id : null;
  const parent = g.parent_id ? byId.get(g.parent_id) : undefined;
  // 面板序号眉标：01 进度、02 基本信息（右栏顶部，视觉上与进度并列），其余按 DOM 顺序从 03 起递增（子目标面板是条件渲染）
  let n = 2;
  const idx = () => ++n;

  return (
    <div>
      {/* 页头（与 PageHeader 同一版式）：标题与说明都能就地改 */}
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="mb-1 flex flex-wrap items-center gap-1 text-caption text-ink-subtle"><Link href="/goals/" className="hover:text-accent-hover">{t("goals.title")}</Link> / <span className="telemetry">{g.id}</span></div>
          <div className="-mx-2 flex flex-wrap items-center gap-x-2 gap-y-1 px-2">
            <span className={cx("inline-flex shrink-0", grown ? "text-success" : "text-ink-subtle")} data-motion={sprout ? "sprout" : undefined} aria-hidden="true"><IconGoal size={22} stage={grown ? "grown" : "bud"} /></span>
            <InlineTitle value={g.title} editable={editPlan} caption={canEdit && g.achieved ? t("inline.achievedGoal") : undefined} onSave={(v) => save({ title: v }, { title: v })} />
            {g.achieved && <Tag tone="success">{t("goals.achieved")}</Tag>}
            {over && <Tag tone="danger">{t("goals.overBudget")}</Tag>}
          </div>
          <EnergyLine className="mt-2" />
          <div className="mt-2 max-w-[768px] text-ink-subtle">
            <InlineText value={g.description} editable={editNotes} placeholder={t("goal.noDescription")} onSave={(v) => save({ description: v }, { description: v })} />
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Link href={`/tasks/?goal=${encodeURIComponent(g.id)}`} className="inline-flex"><Button icon={<IconTask />} tabIndex={-1}>{t("goal.viewTasks")}</Button></Link>
          {canEdit && <Button variant={g.achieved ? "default" : "primary"} icon={g.achieved ? undefined : <IconCheck />} onClick={() => void toggleAchieved()} disabled={busy === "achieve"}>{g.achieved ? t("goal.unachieve") : t("goal.achieve")}</Button>}
        </div>
      </div>

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

          {/* 里程碑（ADR 0016）：按日期列出，确认 / 撤销 / 编辑 / 删除，末尾一行内联新增 */}
          <MilestonePanel goal={g} index={idx()} onChanged={goal.reload} />

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
            <InlineTable>
              <InlineField label={t("common.id")}><IdLine id={g.id} /></InlineField>
              <InlineField label={t("goal.owner")} status={st("owner").status} error={st("owner").error}>
                <InlineSelect value={g.owner.id} options={memberOptions} editable={editPlan} ariaLabel={t("goal.owner")} loading={ex.loading && !ex.data}
                  display={<span className="inline-flex min-w-0 items-center gap-1.5"><Avatar name={g.owner.name} /><span className="inl-text">{g.owner.name}</span></span>}
                  onChange={(mid) => { const m = members.find((x) => x.id === mid); if (mid && m) patch("owner", { owner_id: mid }, { owner: { id: m.id, kind: "member", name: m.name } }); }} />
              </InlineField>
              <InlineField label={t("goal.team")} status={st("team").status} error={st("team").error}>
                <InlineSelect value={g.team_id} options={teamOptions} editable={editPlan} nullable nullLabel={t("goalDialog.noTeam")} ariaLabel={t("goal.team")}
                  display={teamName ? <span className="inl-text">{teamName}</span> : <span className="text-ink-subtle">{t("goalDialog.noTeam")}</span>}
                  onChange={(tid) => patch("team", { team_id: tid ?? "" }, { team_id: tid })} />
              </InlineField>
              <InlineField label={t("goal.parent")} status={st("parent").status} error={st("parent").error}>
                <InlineSelect value={g.parent_id} options={parentOptions} editable={editPlan} nullable nullLabel={t("inline.topGoal")} ariaLabel={t("goal.parent")} loading={tree.loading && !tree.data}
                  display={g.parent_id ? <Link href={`/goals/${encodeURIComponent(g.parent_id)}/`} className="inl-text hover:text-accent-hover" onClick={(e) => editPlan && e.preventDefault()}>{parent?.title ?? g.parent_id}</Link> : <span className="text-ink-subtle">{t("inline.topGoal")}</span>}
                  onChange={(pid) => patch("parent", { parent_id: pid ?? "" }, { parent_id: pid })} />
              </InlineField>
              <InlineField label={t("goal.plannedStart")} status={st("planned_start").status} error={st("planned_start").error}>
                <InlineDate value={g.planned_start} editable={editPlan} withYear ariaLabel={t("goal.plannedStart")} onChange={(d) => patch("planned_start", { planned_start: d }, { planned_start: d })} />
              </InlineField>
              <InlineField label={t("goal.plannedEnd")} status={st("planned_end").status} error={st("planned_end").error}>
                <InlineDate value={g.planned_end} editable={editPlan} withYear ariaLabel={t("goal.plannedEnd")} onChange={(d) => patch("planned_end", { planned_end: d }, { planned_end: d })} />
              </InlineField>
              <InlineField label={t("goal.deadline")} status={st("deadline").status} error={st("deadline").error}>
                <InlineDate value={g.deadline ?? null} editable={editPlan} withYear ariaLabel={t("goal.deadline")} onChange={(d) => patch("deadline", { deadline: d }, { deadline: d })} />
              </InlineField>
              <InlineField label={t("goal.actual")}>{`${fmtDate(g.actual_start, true)} – ${g.actual_end ? fmtDate(g.actual_end, true) : t("common.inProgress")}`}</InlineField>
              <InlineField label={t("goal.cost")}><Odometer value={fmtMoney(g.cost, currency)} /></InlineField>
              <InlineField label={t("goal.budget")} status={st("budget").status} error={st("budget").error}>
                <InlineNumber value={g.budget} editable={editPlan} min={0} step={0.01} prefix={currencySymbol(currency)} format={(v) => fmtNumber(v)} placeholder={t("goal.budgetUnset")} ariaLabel={t("goal.budget")} onChange={(v) => patch("budget", { budget: v }, { budget: v })} />
              </InlineField>
            </InlineTable>
          </Panel>
          <Panel index={idx()} icon={<IconLog />} title={t("goal.events")}>
            {events.loading && !events.data ? <ListSkeleton rows={4} /> : <EventList events={(events.data ?? []).filter((e) => e.goal_id === g.id)} poll={{ limit: 200, filter: (e) => e.goal_id === g.id }} />}
          </Panel>
        </div>
      </div>
    </div>
  );
}
