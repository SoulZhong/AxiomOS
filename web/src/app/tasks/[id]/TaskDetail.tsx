"use client";
import Link from "next/link";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { api, isTerminal, LINK_KINDS, type Event, type ExecutorRef, type ExternalLink, type Goal, type LinkKind, type Milestone, type Proposal, type Relation, type RelationType, type Task, type TaskInput, type TaskState, type WorkflowDef } from "@/lib/api";
import { fmtDate, fmtDateTime, fmtDuration, fmtMoney, fmtTokens } from "@/lib/format";
import { errorMessage, useAction, useCapabilityTitles, useExecutors, useLoad, useRouteId } from "@/lib/hooks";
import { t, type Key } from "@/lib/i18n";
import { prefersReducedMotion } from "@/lib/motion";
import { isAcceptanceWait } from "@/lib/states";
import { artifactTypeTitle, PRIORITIES, priorityTitle, RELATIONS, relationTitle, roleTitle, runOutcomeTitle, stateLabel } from "@/lib/terms";
import { useSession } from "@/components/AppShell";
import { PointsChip, PointsChips } from "@/components/board/PointsChips";
import { BriefSection } from "@/components/BriefSection";
import { EventList } from "@/components/EventList";
import { flattenGoals } from "@/components/GoalDrawer";
import { IconAccept, IconApprove, IconBlocks, IconChevronDown, IconClose, IconCollab, IconExternal, IconGoal, IconLink, IconLog, IconPlus, IconRun, IconTrail, IconUsage } from "@/components/icons";
import { InlineCapabilities, InlineDate, InlineField, InlineFields, InlineNumber, InlineParticipants, InlineSelect, InlineTable, InlineText, InlineTitle, InlineToggle, executorOptions, schemaProps, useInlineSaves } from "@/components/inline";
import { StateBadge, stateMotion } from "@/components/StateBadge";
import { ActionBar, AssignDialog } from "@/components/tasks/TaskActions";
import { isOverdue, LINK_STATUS_TONE, OverdueTag, PriorityTag, TypeLabel } from "@/components/TaskTable";
import { useToast } from "@/components/toast";
import { Avatar, Button, Checkbox, ConfirmDialog, DetailSkeleton, Dialog, EnergyLine, ErrorBox, ExecutorName, Field, IdLine, Input, ListSkeleton, Panel, ProgressBar, RelativeTime, Select, Table, Tag, TaskLink, TaskNumber, Textarea, Tip, cx } from "@/components/ui";

/** PATCH 请求体：除 TaskInput 外还允许清空预估工时（null） */
type TaskPatch = Partial<TaskInput>;

/*
 * 任务详情 = 执行简报（DESIGN.md §17）。四段，按执行者需要的顺序，而不是按数据库字段：
 *   ① 现在要做什么：标题、状态与动作栏、描述、这类任务的做法、走下一步需要什么、参与角色
 *   ② 为什么做：目标链（可点）、里程碑、优先级、计划、迭代、上级任务
 *   ③ 前面发生了什么：前置任务的结果与交付物、关联、交付物、评论、执行记录、动态
 *   ④ 如何验收：验收人、验收条件（来自流程与任务类型）、预计工时与工作量、成本与用量
 * 右栏只留次要的「基本信息」（ID、类型版本、创建者、创建时间……）。所有就地编辑（§15）原样保留。
 */
export function TaskDetail() {
  const id = useRouteId();
  const { session } = useSession();
  const task = useLoad(() => (id ? api.tasks.get(id) : Promise.reject(new Error(t("task.missing")))), [id]);
  const wf = useLoad(() => (id ? api.tasks.workflow(id) : Promise.reject(new Error(t("task.missing")))), [id]);
  const events = useLoad(() => (id ? api.events.list({ task: id, limit: 100 }) : Promise.resolve([])), [id]);
  // 任务说明（GET /tasks/{id}/brief）：目标链、前置任务的结果、类型给执行者的做法；老后端没有这个接口时静默退化
  const brief = useLoad(() => (id ? api.tasks.brief(id).catch(() => null) : Promise.resolve(null)), [id]);
  const typeName = task.data?.type ?? null;
  const type = useLoad(() => (typeName ? api.taskTypes.get(typeName).catch(() => null) : Promise.resolve(null)), [typeName]);
  const caps = useCapabilityTitles();
  // 这个任务上有没有等我确认的动作（ADR 0003）：有就在动作条上方挂一条提示，点进去是筛好的「待确认操作」页
  const proposals = useLoad(() => (id ? api.proposals.list({ status: "pending", mine: 1 }).catch(() => [] as Proposal[]) : Promise.resolve([] as Proposal[])), [id]);
  const myProposals = (proposals.data ?? []).filter((p) => p.target?.kind === "task" && p.target.id === id).length;
  const [optimistic, setOptimistic] = useState<TaskState | null>(null);
  const [assigning, setAssigning] = useState(false);

  // ---------- 就地编辑（DESIGN.md §15） ----------
  // local 是乐观更新后的任务；服务端一回来就以返回值为准，任务重新加载（reload）时清掉。
  const [local, setLocal] = useState<Task | null>(null);
  const [seen, setSeen] = useState<Task | null>(task.data);
  if (seen !== task.data) { setSeen(task.data); setLocal(null); }
  const saves = useInlineSaves();
  const ex = useExecutors();
  // 归属目标的下拉、目标链与"所属目标负责人链"都要整棵目标树
  const goalTree = useLoad(() => api.goals.list().catch(() => [] as Goal[]), []);
  const flatGoals = useMemo(() => flattenGoals(goalTree.data ?? []), [goalTree.data]);
  const goalById = useMemo(() => new Map(flatGoals.map((g) => [g.goal.id, g.goal])), [flatGoals]);
  const me = session?.member.id;
  const mine = (e: ExecutorRef | null | undefined) => !!e && !!me && (e.id === me || (e.kind === "agent" && e.owner_id === me));
  const ownsGoalChain = (goalId: string | null) => {
    for (let g = goalId ? goalById.get(goalId) : undefined; g; g = g.parent_id ? goalById.get(g.parent_id) : undefined) if (g.owner.id === me) return true;
    return false;
  };
  const shown = local ?? task.data;
  // 谁能改：负责人 / 创建者 / 验收人 / 参与人（含我的 Agent）、所属目标的负责人链、组织负责人 —— 与后端 canEditTask 一致
  const canEdit = !!shown && !!session && (!!session.is_owner || mine(shown.assignee) || mine(shown.creator) || mine(shown.reviewer) || Object.values(shown.participants).some((p) => mine(p.executor)) || ownsGoalChain(shown.goal_id));
  const closed = !!shown && isTerminal(shown.state.label);
  const editPlan = canEdit && !closed; // 标题与计划类字段
  const editNotes = canEdit; // 描述与自定义字段：已结束也能改
  // 迭代 / 上级任务的选项只在能改时才取
  const sprints = useLoad(() => (editPlan ? api.sprints.list().catch(() => []) : Promise.resolve([])), [editPlan]);
  const allTasks = useLoad(() => (editPlan ? api.tasks.list({ limit: 500 }).catch(() => [] as Task[]) : Promise.resolve([] as Task[])), [editPlan]);
  // 目标链（②）：从所属目标一路向上，根在前，每级可点；里程碑取所属目标的下一个
  const goalId = shown?.goal_id ?? null;
  const chain = useMemo(() => {
    const out: Goal[] = [];
    for (let g = goalId ? goalById.get(goalId) : undefined; g; g = g.parent_id ? goalById.get(g.parent_id) : undefined) out.unshift(g);
    return out;
  }, [goalId, goalById]);
  const milestones = useLoad(() => (goalId ? api.milestones.list(goalId).catch(() => [] as Milestone[]) : Promise.resolve([] as Milestone[])), [goalId]);
  const nextMilestone = (milestones.data ?? []).find((m) => m.status !== "reached") ?? null;
  // 上级任务候选：除自己和自己的子孙
  const selfId = shown?.id ?? null;
  const parentOptions = useMemo(() => {
    const all = allTasks.data ?? [];
    if (!selfId) return [];
    const kids = new Map<string, string[]>();
    for (const tk of all) if (tk.parent_id) kids.set(tk.parent_id, [...(kids.get(tk.parent_id) ?? []), tk.id]);
    const skip = new Set<string>([selfId]);
    const stack = [selfId];
    while (stack.length) for (const c of kids.get(stack.pop()!) ?? []) { skip.add(c); stack.push(c); }
    return all.filter((tk) => !skip.has(tk.id)).map((tk) => ({ value: tk.id, label: tk.title, hint: `${tk.type_title} · ${tk.state.title}` }));
  }, [allTasks.data, selfId]);

  /** 乐观更新 + 一个请求；失败回退这几个字段并抛出（标题 / 描述控件自己显示原因） */
  const save = async (body: TaskPatch, optimisticFields: Partial<Task>) => {
    const base = shown!;
    const orig = Object.fromEntries(Object.keys(optimisticFields).map((k) => [k, base[k as keyof Task]])) as Partial<Task>;
    setLocal({ ...base, ...optimisticFields });
    try {
      const res = await api.tasks.update(base.id, body);
      setLocal(res);
      events.reload();
      return res;
    } catch (e) {
      setLocal((prev) => ({ ...(prev ?? base), ...orig }));
      throw e;
    }
  };
  /** 信息表的一行：状态记在 saves 里（对勾 / 行下原因） */
  const patch = (key: string, body: TaskPatch, optimisticFields: Partial<Task>) => void saves.run(key, () => save(body, optimisticFields));

  // 工作量：行内点数芯片，点一下即 PATCH（ADR 0012）
  const points = useAction();
  const setPoints = (n: number | null) => {
    const title = task.data?.title ?? "";
    void points.run("points", () => api.tasks.update(id!, { points: n }), n === null ? t("task.pointsCleared", { title }) : t("task.pointsSaved", { title, n })).then((ok) => ok && task.reload());
  };
  const reloadAll = () => { task.reload(); wf.reload(); events.reload(); brief.reload(); };
  // 动态轮询（组织范围，显示时只留本任务）拉到「自动解除前置」时重新加载任务：关联列表随后播断链。
  // 这条动态记在被取消的前置任务名下，data.other_id 才是本任务，所以要看组织范围的流。
  const taskReload = task.reload;
  const onNewEvents = (added: Event[]) => {
    if (added.some((e) => e.kind === "RelationRemoved" && (e.task_id === id || e.data.other_id === id))) taskReload();
  };
  // 断链（提案 §三）：上一次渲染还在、这次没了的「前置」关联，保留 600ms 作为幽灵行播链环分开；同时本任务的 LED 亮一次
  const relations = task.data?.relations;
  const [rel, setRel] = useState<{ prev: Relation[] | undefined; ghosts: Relation[] }>({ prev: relations, ghosts: [] });
  if (rel.prev !== relations) {
    const gone = relations && rel.prev ? rel.prev.filter((r) => r.type === "blocks" && !relations.some((x) => x.id === r.id)) : [];
    setRel({ prev: relations, ghosts: gone });
  }
  const ghosts = rel.ghosts;
  useEffect(() => {
    if (!ghosts.length) return;
    const id = window.setTimeout(() => setRel((r) => (r.ghosts.length ? { ...r, ghosts: [] } : r)), 600);
    return () => window.clearTimeout(id);
  }, [ghosts]);
  // 页头那一行：徽标换状态时按新状态类型播签名动效（进入待验收 = 扫描光扫过标题行；进入成功终态 = 压实在徽标里）
  const wfDef = type.data?.workflow;
  const shownName = (optimistic && optimistic.name !== task.data?.state.name ? optimistic : task.data?.state)?.name;
  const [head, setHead] = useState<{ name: string | undefined; motion: "scan" | "lit" | undefined }>({ name: shownName, motion: undefined });
  if (head.name !== shownName) {
    const st = optimistic && optimistic.name !== task.data?.state.name ? optimistic : task.data?.state;
    setHead({ name: shownName, motion: head.name !== undefined && st && stateMotion(st.label, isAcceptanceWait(st, wfDef)) === "scan" ? "scan" : undefined });
  }
  const headMotion = ghosts.length ? "lit" : head.motion;

  if (!id || (task.loading && !task.data)) return <DetailSkeleton />;
  if (task.error || !task.data || !shown) return <ErrorBox message={task.error ?? t("task.notFound")} onRetry={task.reload} />;
  const x = shown;
  const currency = session?.organization.currency;
  const artifactTypes = session?.artifact_types;
  const roles = session ? Object.fromEntries(session.roles.map((r) => [r.name, r.title])) : undefined;
  // 乐观更新：步骤一按下就把徽标切到目标状态；后端确认（任务重新加载后状态一致）或失败回滚。
  const pendingState = optimistic && optimistic.name !== x.state.name ? optimistic : null;
  const shownState = pendingState ?? x.state;
  const accept = isAcceptanceWait(shownState, wfDef);
  const executors = ex.data?.executors ?? [];
  const execById = (eid: string | null) => (eid ? executors.find((e) => e.id === eid) ?? null : null);
  const exOptions = executorOptions(executors);
  const capTitles = session?.capability_titles ?? caps;
  const sprintOptions = (sprints.data ?? []).filter((s) => s.status !== "closed" || s.id === x.sprint?.id).map((s) => ({ value: s.id, label: s.name, hint: s.team?.title }));
  const goalOptions = flatGoals.map(({ goal, depth }) => ({ value: goal.id, label: goal.title, depth }));
  const hasFields = Object.keys(schemaProps(type.data?.task_schema)).length > 0 || Object.keys(x.fields).length > 0;
  const st = (key: string) => saves.get(key);
  const instructions = brief.data?.agent_instructions ?? type.data?.agent_instructions ?? "";
  const nextNeeds = nextStepNeeds(shownState, wfDef, artifactTypes);
  const acceptance = acceptanceCriteria(wfDef, brief.data?.result_schema ?? type.data?.result_schema, artifactTypes);
  const preds = brief.data?.predecessor_results ?? [];
  const predState = (tid: string) => brief.data?.predecessors.find((p) => p.id === tid)?.state;

  return (
    <div>
      <div className="mb-1 flex flex-wrap items-center gap-1 text-caption text-ink-subtle">
        <Link href="/tasks/" className="hover:text-accent-hover">{t("task.breadcrumb")}</Link>
        <span>/</span>
        {x.goal && (
          <>
            <Link href={`/goals/${encodeURIComponent(x.goal.id)}/`} className="hover:text-accent-hover">{x.goal.title}</Link>
            <span>/</span>
          </>
        )}
        {x.number != null ? <TaskNumber n={x.number} copy className="!text-caption" /> : <span className="telemetry">{x.id}</span>}
      </div>
      <div className="mb-3">
        <div className="-mx-2 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md px-2" data-motion={headMotion}>
          <InlineTitle value={x.title} editable={editPlan} caption={canEdit && closed ? t("inline.closedTask") : undefined} onSave={(v) => save({ title: v }, { title: v })} />
          <span className="inline-flex flex-wrap items-center gap-2">
            {/* 任务类型徽标点开是这类任务的流程（组织设置 · 流程，对所有成员只读可见） */}
            <Tip tip={t("task.viewWorkflow")} placement="bottom">
              <Link href="/settings/?tab=workflows" className="inline-flex rounded-sm hover:opacity-80" aria-label={t("task.viewWorkflow")}><TypeLabel type={x.type} title={x.type_title} tag /></Link>
            </Tip>
            <StateBadge state={shownState} pending={!!pendingState} accept={accept} />
            {x.priority !== "normal" && <PriorityTag priority={x.priority} />}
            {isOverdue(x) && <OverdueTag>{t("task.overdue")}</OverdueTag>}
          </span>
        </div>
        <EnergyLine className="mt-2" />
      </div>

      {myProposals > 0 && (
        <Link href={`/proposals/?target=${encodeURIComponent(x.id)}`} className="mb-3 flex items-center gap-2 rounded-md border border-warning-border bg-warning-bg px-3 py-2 text-body text-warning transition-colors hover:border-warning">
          <IconApprove size={14} className="shrink-0" />
          <span>{t("task.proposals", { n: myProposals })}</span>
          <span className="ml-auto shrink-0 text-caption">{t("task.proposalsGo")} →</span>
        </Link>
      )}

      <ActionBar task={x} wf={wf.data} wfError={wf.error} stateTitles={type.data?.workflow.states} onOptimistic={setOptimistic} onDone={reloadAll} onAssign={() => setAssigning(true)} />
      <AssignDialog task={x} open={assigning} onClose={() => setAssigning(false)} executors={executors} onDone={reloadAll} />

      {/* 主栏自适应，侧栏固定 380px（≥1920 时 420px） */}
      <div className="mt-4 grid gap-6 lg:grid-cols-[minmax(0,1fr)_380px] 3xl:grid-cols-[minmax(0,1fr)_420px]">
        <div className="space-y-6">
          {/* ① 现在要做什么 */}
          <BriefSection n="01" title={t("brief.now")} hint={t("brief.nowHint")}>
            <Panel title={t("task.description")}>
              <InlineText value={x.description} editable={editNotes} placeholder={t("task.noDescription")} onSave={(v) => save({ description: v }, { description: v })} />
              {hasFields && (
                <div className="mt-4 border-t border-hairline pt-4">
                  <h3 className="eyebrow mb-3 text-ink-subtle">{t("task.customFields")}</h3>
                  <InlineFields
                    schema={type.data?.task_schema}
                    values={x.fields}
                    editable={editNotes}
                    stateOf={(k) => st(`field:${k}`)}
                    onChange={(k, v) => {
                      const next = { ...x.fields };
                      if (v === null || v === undefined || v === "") delete next[k]; else next[k] = v;
                      patch(`field:${k}`, { fields: { [k]: v === undefined ? null : v } }, { fields: next });
                    }}
                  />
                </div>
              )}
              {instructions && (
                <div className="mt-4 rounded-md border border-hairline bg-surface-2 px-3 py-2.5" data-brief-instructions>
                  <div className="eyebrow mb-1 normal-case text-ink-subtle">{t("brief.instructions", { type: x.type_title })}</div>
                  <p className="whitespace-pre-wrap text-body text-ink-muted">{instructions}</p>
                </div>
              )}
              {nextNeeds && !closed && (
                <p className="mt-3 text-caption text-ink-subtle" data-brief-next>{nextNeeds}</p>
              )}
            </Panel>

            {Object.keys(x.participants).length > 0 && (
              <Panel icon={<IconCollab />} title={t("task.participants")} padded={false}>
                <InlineParticipants
                  participants={x.participants}
                  executors={executors}
                  pendingSlot={x.pending_participant}
                  editable={editPlan}
                  roles={roles}
                  stateOf={(slot) => st(`p:${slot}`)}
                  onChange={(slot, eid) => patch(`p:${slot}`, { participants: { [slot]: eid ?? "" } }, { participants: { ...x.participants, [slot]: { ...x.participants[slot], executor: execById(eid) } } })}
                />
              </Panel>
            )}
          </BriefSection>

          {/* ② 为什么做 */}
          <BriefSection n="02" title={t("brief.why")} hint={t("brief.whyHint")}>
            <Panel>
              <InlineTable>
                <InlineField label={t("task.goalRow")} status={st("goal").status} error={st("goal").error}>
                  <span className="flex min-w-0 flex-wrap items-center gap-x-1 gap-y-0.5">
                    {chain.slice(0, -1).map((g) => (
                      <span key={g.id} className="inline-flex items-center gap-1 text-ink-muted">
                        <Link href={`/goals/${encodeURIComponent(g.id)}/`} className="hover:text-accent-hover">{g.title}</Link>
                        <span className="text-ink-tertiary">›</span>
                      </span>
                    ))}
                    <InlineSelect value={x.goal_id} options={goalOptions} editable={editPlan} nullable nullLabel={t("taskDialog.noGoal")} ariaLabel={t("task.goalRow")} loading={goalTree.loading && !goalTree.data}
                      display={x.goal ? <Link href={`/goals/${encodeURIComponent(x.goal.id)}/`} className="inl-text inline-flex items-center gap-1 hover:text-accent-hover" onClick={(e) => editPlan && e.preventDefault()}><IconGoal size={14} className="text-ink-subtle" />{x.goal.title}</Link> : <span className="text-ink-subtle">{t("taskDialog.noGoal")}</span>}
                      onChange={(gid) => { const g = gid ? goalById.get(gid) : undefined; patch("goal", { goal_id: gid ?? "" }, { goal_id: gid, goal: gid && g ? { id: g.id, title: g.title } : null }); }} />
                  </span>
                </InlineField>
                {x.goal && (
                  <InlineField label={t("brief.milestone")}>
                    {nextMilestone ? (
                      <span className="flex flex-wrap items-center gap-2">
                        <span>{nextMilestone.title}</span>
                        <span className="telemetry text-ink-subtle">{fmtDate(nextMilestone.due_on, true)}</span>
                        {nextMilestone.status === "overdue" && <Tag tone="danger">{t("ms.status.overdue")}</Tag>}
                      </span>
                    ) : <span className="text-ink-subtle">{milestones.loading && !milestones.data ? "…" : t("brief.noMilestone")}</span>}
                  </InlineField>
                )}
                <InlineField label={t("taskDialog.priority")} status={st("priority").status} error={st("priority").error}>
                  <InlineSelect value={x.priority} options={PRIORITIES.map((p) => ({ value: p, label: priorityTitle(p) }))} editable={editPlan} ariaLabel={t("taskDialog.priority")}
                    display={x.priority === "normal" ? <span>{priorityTitle(x.priority)}</span> : <PriorityTag priority={x.priority} />}
                    onChange={(p) => p && patch("priority", { priority: p as Task["priority"] }, { priority: p as Task["priority"] })} />
                </InlineField>
                <InlineField label={t("task.plannedStart")} status={st("planned_start").status} error={st("planned_start").error}>
                  <InlineDate value={x.planned_start} editable={editPlan} ariaLabel={t("task.plannedStart")} onChange={(d) => patch("planned_start", { planned_start: d }, { planned_start: d })} />
                </InlineField>
                <InlineField label={t("task.plannedEnd")} status={st("planned_end").status} error={st("planned_end").error}>
                  <InlineDate value={x.planned_end} editable={editPlan} ariaLabel={t("task.plannedEnd")} className={cx(isOverdue(x) && "text-danger")} onChange={(d) => patch("planned_end", { planned_end: d }, { planned_end: d })} />
                </InlineField>
                <InlineField label={t("task.sprint")} status={st("sprint").status} error={st("sprint").error}>
                  <InlineSelect value={x.sprint?.id ?? null} options={sprintOptions} editable={editPlan} nullable nullLabel={t("task.noSprint")} ariaLabel={t("task.sprint")} loading={sprints.loading && !sprints.data}
                    display={x.sprint ? <Link href={`/sprints/${encodeURIComponent(x.sprint.id)}/`} className="inl-text hover:text-accent-hover" onClick={(e) => editPlan && e.preventDefault()}>{x.sprint.name}</Link> : <span className="text-ink-subtle">{t("task.noSprint")}</span>}
                    onChange={(sid) => { const s = (sprints.data ?? []).find((y) => y.id === sid); patch("sprint", { sprint_id: sid ?? "" }, { sprint: sid && s ? { id: s.id, name: s.name } : null }); }} />
                </InlineField>
                <InlineField label={t("task.parentTask")} status={st("parent").status} error={st("parent").error}>
                  <InlineSelect value={x.parent_id} options={parentOptions} editable={editPlan} nullable nullLabel={t("inline.topTask")} ariaLabel={t("task.parentTask")} loading={allTasks.loading && !allTasks.data}
                    display={x.parent_id ? <TaskLink id={x.parent_id} title={parentOptions.find((o) => o.value === x.parent_id)?.label ?? (allTasks.data ?? []).find((tk) => tk.id === x.parent_id)?.title ?? x.parent_id} inline className="inl-text" /> : <span className="text-ink-subtle">{t("inline.topTask")}</span>}
                    onChange={(pid) => patch("parent", { parent_id: pid ?? "" }, { parent_id: pid })} />
                </InlineField>
              </InlineTable>
            </Panel>
          </BriefSection>

          {/* ③ 前面发生了什么 */}
          <BriefSection n="03" title={t("brief.before")} hint={t("brief.beforeHint")}>
            {(preds.length > 0 || (brief.loading && !brief.data)) && (
              <Panel icon={<IconBlocks />} title={t("brief.predecessors")} telemetry={preds.length ? t("panel.rows", { n: preds.length }) : undefined}>
                {brief.loading && !brief.data ? <ListSkeleton rows={2} /> : (
                  <ul className="space-y-3">
                    {preds.map((p) => {
                      const s = predState(p.task_id);
                      const summary = typeof p.result === "string" ? p.result : p.result && typeof p.result === "object" && "summary" in p.result ? String((p.result as { summary?: unknown }).summary ?? "") : "";
                      return (
                        <li key={p.task_id} className="text-body">
                          <div className="flex flex-wrap items-center gap-2">
                            <TaskLink id={p.task_id} title={p.title} inline className="font-medium" />
                            {s && <StateBadge state={s} />}
                          </div>
                          {summary ? <p className="mt-1 whitespace-pre-wrap text-ink-muted">{summary}</p> : <p className="mt-1 text-caption text-ink-subtle">{t("brief.noResultYet")}</p>}
                          {p.artifacts.length > 0 && (
                            <ul className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-caption">
                              {p.artifacts.map((a) => (
                                <li key={a.id}><a href={a.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 hover:text-accent-hover"><Tag tone="accent">{artifactTypeTitle(a.type, artifactTypes)}</Tag>{a.title}<IconExternal className="text-ink-subtle" /></a></li>
                              ))}
                            </ul>
                          )}
                        </li>
                      );
                    })}
                  </ul>
                )}
              </Panel>
            )}
            <Links task={x} editable={canEdit} onChanged={reloadAll} />
            <Relations task={x} onChanged={reloadAll} ghosts={ghosts} />
            <Artifacts task={x} onChanged={reloadAll} artifactTypes={artifactTypes} />
            <Thread task={x} onChanged={reloadAll} />
            <Panel icon={<IconRun />} title={t("task.runs")} telemetry={t("panel.rows", { n: x.runs.length })} padded={false}>
              {x.runs.length === 0 ? (
                <p className="px-4 py-4 text-body text-ink-muted">{t("task.noRuns")}</p>
              ) : (
                <Table>
                  <thead>
                    <tr>
                      <th className="w-[120px]">{t("task.stage")}</th><th className="w-[160px]">{t("task.executor")}</th><th className="w-[200px]">{t("task.started")}</th><th className="w-[96px]">{t("task.duration")}</th><th className="w-[80px]">{t("task.outcome")}</th><th className="w-full min-w-[260px]">{t("task.usage")}</th><th className="num w-[110px]">{t("task.cost")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {x.runs.map((r) => (
                      <tr key={r.id}>
                        <td className="whitespace-nowrap">{r.state_title}</td>
                        <td className="whitespace-nowrap"><ExecutorName executor={r.executor} /></td>
                        <td className="telemetry whitespace-nowrap">{fmtDateTime(r.started_at)} – {r.ended_at ? fmtDateTime(r.ended_at) : t("common.inProgress")}</td>
                        <td className="telemetry whitespace-nowrap">{fmtDuration(r.started_at, r.ended_at)}</td>
                        <td><Tag tone={r.outcome === "running" ? "accent" : "neutral"} dark={r.outcome === "cancelled"}>{runOutcomeTitle(r.outcome)}</Tag></td>
                        <td>
                          {r.usage.length === 0 ? <span className="text-ink-subtle">—</span> : (
                            <ul className="space-y-0.5 text-caption">
                              {r.usage.map((u) => <li key={u.model} className="telemetry whitespace-nowrap"><span className="text-ink-muted">{u.model}</span> · {t("task.tokens", { n: fmtTokens(u.total_tokens) })} · {t("task.toolCalls", { n: u.tool_calls })}</li>)}
                              <li className="font-mono text-ink-subtle">{t("task.totalTokens", { n: fmtTokens(r.total_tokens) })}</li>
                            </ul>
                          )}
                        </td>
                        <td className="num telemetry whitespace-nowrap">{fmtMoney(r.cost, currency)}</td>
                      </tr>
                    ))}
                  </tbody>
                  <tfoot>
                    <tr>
                      <td colSpan={5}>{t("task.total")}</td>
                      <td className="telemetry">{t("task.tokens", { n: fmtTokens(x.total_tokens) })}</td>
                      <td className="num telemetry font-medium">{fmtMoney(x.cost, currency)}</td>
                    </tr>
                  </tfoot>
                </Table>
              )}
            </Panel>
            <Panel icon={<IconLog />} title={t("task.events")} telemetry={events.data ? t("panel.rows", { n: events.data.length }) : undefined}>
              {events.loading && !events.data ? <ListSkeleton rows={4} /> : <EventList events={events.data ?? []} currentTaskId={x.id} poll={{ limit: 60, filter: (e) => e.task_id === x.id }} onNew={onNewEvents} />}
            </Panel>
          </BriefSection>

          {/* ④ 如何验收 */}
          <BriefSection n="04" title={t("brief.accept")} hint={t("brief.acceptHint")}>
            <Panel>
              <InlineTable>
                <InlineField label={t("task.reviewer")} status={st("reviewer").status} error={st("reviewer").error}>
                  <InlineSelect value={x.reviewer.id} options={exOptions} editable={editPlan} display={<ExecutorName executor={x.reviewer} />} ariaLabel={t("task.reviewer")} loading={ex.loading && !ex.data}
                    onChange={(eid) => { const e = execById(eid); if (eid && e) patch("reviewer", { reviewer_id: eid }, { reviewer: e }); }} />
                </InlineField>
                <InlineField label={t("brief.criteria")}>
                  {acceptance.length ? (
                    <ul className="space-y-0.5" data-brief-criteria>
                      {acceptance.map((line) => <li key={line} className="flex items-start gap-2"><span className="mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full bg-hairline-tertiary" aria-hidden="true" />{line}</li>)}
                    </ul>
                  ) : <span className="text-ink-subtle">{t("brief.criteriaDefault")}</span>}
                </InlineField>
                <InlineField label={t("task.estimate")} status={st("estimate").status} error={st("estimate").error}>
                  <InlineNumber value={x.estimate} editable={editPlan} min={0} step={0.5} suffix={t("inline.hours")} placeholder={t("task.estimateUnset")} ariaLabel={t("task.estimate")} onChange={(v) => patch("estimate", { estimate: v }, { estimate: v })} />
                </InlineField>
                <InlineField label={t("task.points")}>
                  <span className="flex flex-wrap items-center gap-2"><PointsChip value={x.points} />{editPlan && <PointsChips value={x.points} onChange={setPoints} busy={points.busy === "points"} size="sm" />}</span>
                </InlineField>
                <InlineField label={t("task.cost")}><span className="telemetry">{fmtMoney(x.cost, currency)}</span></InlineField>
                <InlineField label={t("task.usage")}><span className="telemetry inline-flex items-center gap-1.5"><IconUsage size={14} className="text-ink-subtle" />{t("task.tokens", { n: fmtTokens(x.total_tokens) })}</span></InlineField>
              </InlineTable>
            </Panel>
          </BriefSection>
        </div>

        {/* 右栏：先一张验收小卡（不用翻过长长的动态就知道谁验收、按什么验收），再是次要的基本信息 */}
        <div className="space-y-4">
          <Panel icon={<IconAccept />} title={t("brief.acceptCard")}>
            <InlineTable>
              <InlineField label={t("task.reviewer")}><span className="inl-static"><ExecutorName executor={x.reviewer} /></span></InlineField>
              <InlineField label={t("brief.criteria")}>
                {acceptance.length ? <span className="line-clamp-2" title={acceptance.join("\n")}>{acceptance[0]}</span> : <span className="text-ink-subtle">{t("brief.criteriaDefault")}</span>}
              </InlineField>
              <InlineField label={t("task.points")}>
                <span className="flex flex-wrap items-center gap-2">
                  <PointsChip value={x.points} />
                  <span className="text-ink-subtle">·</span>
                  <span className="telemetry">{x.estimate != null ? `${x.estimate} ${t("inline.hours")}` : t("task.estimateUnset")}</span>
                </span>
              </InlineField>
              <InlineField label={t("task.cost")}><span className="telemetry">{fmtMoney(x.cost, currency)}</span></InlineField>
            </InlineTable>
            {acceptance.length > 1 && <p className="mt-2 text-caption text-ink-subtle">{t("brief.acceptCardMore")}</p>}
          </Panel>
          <Panel title={t("task.info")}>
            <InlineTable>
              <InlineField label={t("common.id")}><IdLine id={x.id} /></InlineField>
              {/* 负责人：仍走指派（交接确认框），这一行的值就是打开它的控件 */}
              <InlineField label={t("task.assignee")}>
                {editPlan ? (
                  <button type="button" className="inl" onClick={() => setAssigning(true)} aria-haspopup="dialog" aria-label={t("task.assign")}>
                    <ExecutorName executor={x.assignee} empty={t("task.unclaimed")} />
                    <IconChevronDown size={12} className="inl-caret" aria-hidden="true" />
                  </button>
                ) : <span className="inl-static"><ExecutorName executor={x.assignee} empty={t("task.unclaimed")} /></span>}
              </InlineField>
              <InlineField label={t("task.creator")}><span className="inl-static"><ExecutorName executor={x.creator} /></span></InlineField>
              <InlineField label={t("task.createdAt")}><RelativeTime iso={x.created_at} className="telemetry" /></InlineField>
              <InlineField label={t("task.type")}>{t("task.typeVersion", { title: x.type_title, version: x.type_version })}</InlineField>
              <InlineField label={t("task.label")}>{stateLabel(shownState.label)}</InlineField>
              <InlineField label={t("task.progress")}><span className="flex items-center gap-2"><ProgressBar value={x.progress} tone={x.state.label === "terminal_success" ? "success" : "accent"} className="w-24" /><span className="tabular-nums">{x.progress}%</span></span></InlineField>
              <InlineField label={t("task.actual")}>{x.actual_start ? `${fmtDate(x.actual_start)} – ${x.actual_end ? fmtDate(x.actual_end) : t("common.inProgress")}` : "—"}</InlineField>
              <InlineField label={t("task.humanOnly")} status={st("human_only").status} error={st("human_only").error}>
                <InlineToggle checked={!!x.human_only} editable={editPlan} ariaLabel={t("task.humanOnly")} hint={t("task.humanOnlyHint")} onChange={(v) => patch("human_only", { human_only: v }, { human_only: v })} />
              </InlineField>
              <InlineField label={t("task.requiredCaps")} status={st("caps").status} error={st("caps").error}>
                <InlineCapabilities value={x.required_capabilities} titles={capTitles} editable={editPlan} onChange={(list) => patch("caps", { required_capabilities: list }, { required_capabilities: list })} />
              </InlineField>
              {x.required_role && <InlineField label={t("task.requiredRole")}>{roleTitle(x.required_role, roles)}</InlineField>}
            </InlineTable>
          </Panel>
        </div>
      </div>
    </div>
  );
}

/** 流程里"要求"的界面名：deps_done / artifact:<类型> / comment / no_open_bugs / result */
function requirementTitle(req: string, artifactTypes?: Record<string, string>): string {
  if (req === "deps_done") return t("brief.req.depsDone");
  if (req === "comment") return t("brief.req.comment");
  if (req === "no_open_bugs") return t("brief.req.noOpenBugs");
  if (req === "result") return t("brief.req.result");
  if (req.startsWith("artifact:")) return t("brief.req.artifact", { type: artifactTypeTitle(req.slice("artifact:".length), artifactTypes) });
  return req;
}
/** ①里的一句：从当前状态向前走的步骤各自需要什么（只列有要求的步骤） */
function nextStepNeeds(state: TaskState, wf: WorkflowDef | undefined, artifactTypes?: Record<string, string>): string | null {
  if (!wf) return null;
  const lines = wf.transitions
    .filter((tr) => (tr.from.includes("*") || tr.from.includes(state.name)) && tr.requires.length > 0)
    .filter((tr) => { const to = wf.states[tr.to]; return !to || to.label !== "terminal_failure"; })
    .map((tr) => t("brief.nextNeeds", { step: tr.title, needs: tr.requires.map((r) => requirementTitle(r, artifactTypes)).join(t("brief.listSep")) }));
  return lines.length ? lines.join(t("brief.lineSep")) : null;
}
/** ④里的验收条件：进入待验收 / 成功终态的步骤各要求什么，加上结果要填的字段 */
function acceptanceCriteria(wf: WorkflowDef | undefined, resultSchema: Record<string, unknown> | null | undefined, artifactTypes?: Record<string, string>): string[] {
  const out: string[] = [];
  if (wf) {
    for (const tr of wf.transitions) {
      const to = wf.states[tr.to];
      if (!to) continue;
      const intoAccept = isAcceptanceWait(to, wf);
      const intoDone = to.label === "terminal_success";
      if (!intoAccept && !intoDone) continue;
      const who = tr.by.map((b) => (b === "reviewer" ? t("task.reviewer") : b === "creator" ? t("task.creator") : b === "assignee" ? t("task.assignee") : b === "anyone" ? t("brief.anyone") : b.startsWith("role:") ? b.slice(5) : b.startsWith("participant:") ? b.slice(12) : b)).join(t("brief.listSep"));
      const needs = tr.requires.map((r) => requirementTitle(r, artifactTypes)).join(t("brief.listSep"));
      out.push(intoDone ? t("brief.criteriaDone", { step: tr.title, who, needs: needs || t("brief.noExtra") }) : t("brief.criteriaSubmit", { step: tr.title, needs: needs || t("brief.noExtra") }));
    }
  }
  const props = schemaProps(resultSchema);
  const fields = Object.entries(props).map(([k, v]) => (v && typeof v === "object" && "title" in v && typeof (v as { title?: unknown }).title === "string" ? (v as { title: string }).title : k));
  if (fields.length) out.push(t("brief.criteriaFields", { fields: fields.join(t("brief.listSep")) }));
  return Array.from(new Set(out));
}

// ---------- 关联 ----------
function Relations({ task, onChanged, ghosts }: { task: Task; onChanged: () => void; ghosts: Relation[] }) {
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const [type, setType] = useState<RelationType>("blocks");
  const [dir, setDir] = useState<"from" | "to">("from"); // from = 对方在前
  const [other, setOther] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const all = useLoad(() => (open ? api.tasks.list() : Promise.resolve([])), [open]);

  const describe = (r: Task["relations"][number]) => {
    const mine = r.from.id === task.id;
    const o = mine ? r.to : r.from;
    const label = r.type === "blocks" ? (mine ? t("task.rel.successor") : t("task.rel.predecessor")) : r.type === "found_in" ? (mine ? t("task.rel.foundIn") : t("task.rel.bugFound")) : t("task.rel.related");
    return { label, other: o };
  };
  // 关联标签：前置 / 后续 = 链，发现于 = 痕迹，相关不带图标
  const relTag = (r: Relation, label: string) => (
    <Tag tone={r.type === "blocks" ? "warning" : "neutral"}>
      {r.type === "blocks" ? <IconBlocks size={12} className="tag-icon" /> : r.type === "found_in" ? <IconTrail size={12} className="tag-icon" /> : null}
      {label}
    </Tag>
  );
  const rows = [...task.relations.map((r) => ({ r, ghost: false })), ...ghosts.map((r) => ({ r, ghost: true }))];
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      const from = dir === "from" ? other : task.id;
      const to = dir === "from" ? task.id : other;
      await api.tasks.addRelation(task.id, { type, from_task_id: from, to_task_id: to });
      toast.ok(t("toast.linked"));
      setOpen(false); onChanged();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  const dirLabel = type === "blocks" ? [t("task.dir.blocksFrom"), t("task.dir.blocksTo")] : type === "found_in" ? [t("task.dir.foundFrom"), t("task.dir.foundTo")] : [t("task.rel.related"), t("task.rel.related")];

  return (
    <Panel title={t("task.relations")} actions={<Button size="sm" icon={<IconLink />} onClick={() => setOpen(true)}>{t("task.link")}</Button>}>
      {rows.length === 0 ? <p className="text-body text-ink-subtle">{t("task.noRelationsHint")}</p> : (
        <ul className="space-y-2 text-body">
          {rows.map(({ r, ghost }) => { const d = describe(r); return (
            <li key={r.id} className="flex flex-wrap items-center gap-2" data-motion={ghost ? "unlink" : undefined} aria-hidden={ghost || undefined}>
              {relTag(r, d.label)}
              <TaskLink id={d.other.id} title={d.other.title} inline className="font-medium" />
              <StateBadge state={d.other.state} showLabel={false} />
            </li>
          ); })}
        </ul>
      )}
      <Dialog open={open} onClose={() => setOpen(false)} title={t("task.linkTitle")} footer={<><Button onClick={() => setOpen(false)}>{t("common.cancel")}</Button><Button variant="primary" form="rel-form" type="submit" disabled={busy}>{t("common.confirm")}</Button></>}>
        <form id="rel-form" onSubmit={submit} className="space-y-3">
          <Field label={t("task.relationType")}>
            <Select value={type} onChange={(e) => setType(e.target.value as RelationType)}>
              {RELATIONS.map((k) => <option key={k} value={k}>{relationTitle(k)}</option>)}
            </Select>
          </Field>
          {type !== "related" && (
            <Field label={t("task.direction")}>
              <Select value={dir} onChange={(e) => setDir(e.target.value as "from" | "to")}>
                <option value="from">{dirLabel[0]}</option>
                <option value="to">{dirLabel[1]}</option>
              </Select>
            </Field>
          )}
          <Field label={t("task.otherTask")} hint={t("task.linkHint")} error={error}>
            <Select value={other} onChange={(e) => setOther(e.target.value)} required>
              <option value="">{t("task.pickTask")}</option>
              {(all.data ?? []).filter((o) => o.id !== task.id).map((o) => <option key={o.id} value={o.id}>{o.title} ({o.type_title} · {o.state.title})</option>)}
            </Select>
          </Field>
        </form>
      </Dialog>
    </Panel>
  );
}

// ---------- 外部链接（ADR 0020） ----------
/**
 * ③ 里的「外部链接」：PR、Issue、设计稿、文档。代码平台来的那几条由回调自动建与更新（带状态与操作人），
 * 其它手工挂。一行一条：类型标签、标题（点开去那边）、状态标签、谁做的、移除。
 */
function Links({ task, editable, onChanged }: { task: Task; editable: boolean; onChanged: () => void }) {
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<LinkKind>("doc");
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [removing, setRemoving] = useState<ExternalLink | null>(null);
  const links = task.links ?? [];

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      await api.tasks.addLink(task.id, { kind, url, title: title.trim() || undefined });
      toast.ok(t("task.linkAdded"));
      setOpen(false); setUrl(""); setTitle(""); onChanged();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  const remove = async (l: ExternalLink) => {
    setBusy(true);
    try { await api.tasks.removeLink(task.id, l.id); toast.ok(t("task.linkRemoved")); setRemoving(null); onChanged(); }
    catch (err) { toast.fail(errorMessage(err)); } finally { setBusy(false); }
  };

  return (
    <Panel id="links" icon={<IconLink />} title={t("task.links")} telemetry={links.length ? t("panel.rows", { n: links.length }) : undefined}
      actions={editable && <Button size="sm" icon={<IconPlus />} onClick={() => { setError(null); setOpen(true); }}>{t("task.addLink")}</Button>}>
      {links.length === 0 ? <p className="text-body text-ink-subtle">{t("task.noLinks")}</p> : (
        <ul className="space-y-2 text-body" data-task-links>
          {links.map((l) => (
            <li key={l.id} className="flex flex-wrap items-center gap-2" data-link={l.id} data-kind={l.kind}>
              <Tag>{l.kind_title}</Tag>
              <a href={l.url} target="_blank" rel="noreferrer noopener" className="inline-flex min-w-0 items-center gap-1 font-medium hover:text-accent-hover" title={l.url}>
                <span className="truncate">{l.title || l.url}</span>
                <IconExternal className="shrink-0 text-ink-subtle" aria-hidden="true" />
              </a>
              {l.status && l.status_title && <Tag tone={LINK_STATUS_TONE[l.status] ?? "neutral"}>{l.status_title}</Tag>}
              {l.actor_name && <span className="text-caption text-ink-subtle">{t("task.linkBy", { name: l.actor_name })}</span>}
              <RelativeTime iso={l.updated_at} className="text-caption text-ink-subtle" />
              {editable && (
                <Button size="sm" variant="ghost" className="ml-auto" icon={<IconClose />} aria-label={t("task.linkRemove")} onClick={() => setRemoving(l)}>{t("task.linkRemove")}</Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <Dialog open={open} onClose={() => setOpen(false)} title={t("task.addLinkTitle")} footer={<><Button onClick={() => setOpen(false)}>{t("common.cancel")}</Button><Button variant="primary" form="link-form" type="submit" disabled={busy}>{t("common.confirm")}</Button></>}>
        <form id="link-form" onSubmit={submit} className="space-y-3">
          <Field label={t("task.linkKind")}>
            <Select value={kind} onChange={(e) => setKind(e.target.value as LinkKind)}>
              {LINK_KINDS.map((k) => <option key={k} value={k}>{t(`link.kind.${k}` as Key)}</option>)}
            </Select>
          </Field>
          <Field label={t("task.linkUrl")} error={error}><Input type="url" value={url} onChange={(e) => setUrl(e.target.value)} required autoFocus placeholder="https://" /></Field>
          <Field label={t("task.linkName")} hint={t("task.linkNameHint")}><Input value={title} onChange={(e) => setTitle(e.target.value)} /></Field>
        </form>
      </Dialog>
      <ConfirmDialog
        open={!!removing}
        title={t("task.linkRemoveTitle")}
        message={removing ? t("task.linkRemoveMsg", { title: removing.title || removing.url }) : ""}
        confirmLabel={t("task.linkRemove")}
        danger
        busy={busy}
        onConfirm={() => removing && void remove(removing)}
        onClose={() => setRemoving(null)}
      />
    </Panel>
  );
}

// ---------- 交付物 ----------
function Artifacts({ task, onChanged, artifactTypes }: { task: Task; onChanged: () => void; artifactTypes?: Record<string, string> }) {
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const [type, setType] = useState("");
  const [title, setTitle] = useState("");
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fallback = ["prd", "pr", "test_report", "release_note", "result"];
  const types = artifactTypes ?? Object.fromEntries(fallback.map((k) => [k, artifactTypeTitle(k)]));
  const terminal = isTerminal(task.state.label);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try { await api.tasks.addArtifact(task.id, { type, title, url }); toast.ok(t("toast.attached")); setOpen(false); setTitle(""); setUrl(""); onChanged(); }
    catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <Panel id="artifacts" title={t("task.artifacts")} actions={<Tip tip={terminal ? t("task.ended.noNext") : null}><Button size="sm" icon={<IconPlus />} onClick={() => { setType(Object.keys(types)[0] ?? ""); setOpen(true); }} disabled={terminal}>{t("task.attach")}</Button></Tip>}>
      {task.artifacts.length === 0 ? <p className="text-body text-ink-subtle">{t("task.noArtifacts")}</p> : (
        <ul className="space-y-2 text-body">
          {task.artifacts.map((a) => (
            <li key={a.id} className="flex flex-wrap items-center gap-2">
              <Tag tone="accent">{artifactTypeTitle(a.type, artifactTypes)}</Tag>
              <a href={a.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 font-medium hover:text-accent-hover">{a.title}<IconExternal className="text-ink-subtle" /></a>
              <span className="text-caption text-ink-subtle">{a.attached_by.name} · <RelativeTime iso={a.created_at} /></span>
            </li>
          ))}
        </ul>
      )}
      <Dialog open={open} onClose={() => setOpen(false)} title={t("task.attach")} footer={<><Button onClick={() => setOpen(false)}>{t("common.cancel")}</Button><Button variant="primary" form="artifact-form" type="submit" disabled={busy}>{t("common.confirm")}</Button></>}>
        <form id="artifact-form" onSubmit={submit} className="space-y-3">
          <Field label={t("task.artifactType")}>
            <Select value={type} onChange={(e) => setType(e.target.value)}>
              {Object.entries(types).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </Select>
          </Field>
          <Field label={t("task.artifactName")}><Input value={title} onChange={(e) => setTitle(e.target.value)} required autoFocus /></Field>
          <Field label={t("task.artifactUrl")} error={error}><Input type="url" value={url} onChange={(e) => setUrl(e.target.value)} required placeholder="https://" /></Field>
        </form>
      </Dialog>
    </Panel>
  );
}

// ---------- 评论 / 工作日志 ----------
function Thread({ task, onChanged }: { task: Task; onChanged: () => void }) {
  const toast = useToast();
  const [showNotes, setShowNotes] = useState(false);
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const notes = task.comments.filter((c) => c.kind === "note").length;
  const list = task.comments.filter((c) => showNotes || c.kind === "comment");
  // 从「待我处理 · 等我答复」进来时地址带 #comment-<id>：评论是异步取的，浏览器自己滚不到，这里在列表画出来后滚过去并点亮那一条 2.4s（纯 DOM，不进状态）
  const listKey = list.map((c) => c.id).join(",");
  useEffect(() => {
    const m = /^#comment-(.+)$/.exec(window.location.hash);
    if (!m) return;
    const el = document.getElementById(`comment-${decodeURIComponent(m[1])}`);
    if (!el) return;
    el.scrollIntoView({ block: "center", behavior: prefersReducedMotion() ? "auto" : "smooth" });
    el.classList.add("comment-anchored");
    const timer = window.setTimeout(() => el.classList.remove("comment-anchored"), 2400);
    return () => {
      window.clearTimeout(timer);
      el.classList.remove("comment-anchored");
    };
  }, [listKey]);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try { await api.tasks.addComment(task.id, { body }); toast.ok(t("toast.commented")); setBody(""); onChanged(); }
    catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <Panel id="comments" title={t("task.comments")} telemetry={t("panel.rows", { n: list.length })} actions={notes > 0 && <Checkbox className="text-caption text-ink-muted" checked={showNotes} onChange={(e) => setShowNotes(e.target.checked)} label={t("task.showNotes", { n: notes })} />}>
      {list.length === 0 ? <p className="text-body text-ink-subtle">{t("task.noCommentsHint")}</p> : (
        <ul className="space-y-4">
          {list.map((c) => (
            <li key={c.id} id={`comment-${c.id}`} className="comment-row flex gap-3 rounded-md">
              <Avatar name={c.author.name} kind={c.author.kind} size={24} className="mt-0.5" />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2 text-caption text-ink-subtle">
                  <span className="text-ink-muted">{c.author.name}</span>
                  {c.author.kind === "agent" && <Tag tone="accent">Agent</Tag>}
                  {c.kind === "note" && <Tag>{t("task.note")}</Tag>}
                  <RelativeTime iso={c.created_at} />
                </div>
                <p className={cx("mt-0.5 whitespace-pre-wrap text-body", c.kind === "note" && "text-ink-muted")}>{c.body}</p>
              </div>
            </li>
          ))}
        </ul>
      )}
      <form onSubmit={submit} className="mt-4 border-t border-hairline pt-4">
        <Textarea value={body} onChange={(e) => setBody(e.target.value)} placeholder={t("task.commentPlaceholder")} required aria-label={t("task.writeComment")} />
        <div className="mt-2 flex items-center justify-between gap-3">
          <span className="text-caption text-danger">{error}</span>
          <Button type="submit" variant="default" size="sm" disabled={busy}>{t("task.postComment")}</Button>
        </div>
      </form>
    </Panel>
  );
}
