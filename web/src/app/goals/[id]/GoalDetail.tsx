"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { api, effectivePrecision, GOAL_CONFIDENCES, GOAL_DATE_PRECISIONS, GOAL_HORIZONS, type Goal, type GoalConfidence, type GoalDatePrecision, type GoalHorizon, type GoalInput, type GoalType } from "@/lib/api";
import { fmtDate, fmtMoney, fmtNumber } from "@/lib/format";
import { useAction, useExecutors, useLoad, useRouteId, useSprout } from "@/lib/hooks";
import { t, type Key } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { isAcceptanceWait, useTaskTypeIndex } from "@/lib/states";
import { goalSummary, tallyGoals } from "../goalStatus";
import { BriefSection } from "@/components/BriefSection";
import { EventList } from "@/components/EventList";
import { flattenGoals } from "@/components/GoalDrawer";
import { IconCancel, IconCheck, IconGoal, IconLog, IconPlus, IconTask, IconTrash } from "@/components/icons";
import { InlineDate, InlineField, InlineNumber, InlineSelect, InlineTable, InlineText, InlineTextInput, InlineTitle, useInlineSaves } from "@/components/inline";
import { ArcGauge } from "@/components/instruments/ArcGauge";
import { Odometer } from "@/components/instruments/Odometer";
import { TaskTable } from "@/components/TaskTable";
import { MilestonePanel } from "./MilestonePanel";
import { Avatar, Button, ConsequenceDialog, DetailSkeleton, EnergyLine, ErrorBox, IdLine, ListSkeleton, Panel, ProgressBar, TableSkeleton, Tag, Tip, cx } from "@/components/ui";

export function GoalDetail() {
  const id = useRouteId();
  return <GoalDetailBody id={id} />;
}

/** 货币符号（¥ / $ …）：预算输入框前缀 */
const currencySymbol = (currency?: string) => fmtMoney(0, currency).replace(/[\d.,\s]/g, "");

/** 时间桶与信心度（ADR 0021）：三档写死，不做成可配置词表 */
const horizonOptions = () => GOAL_HORIZONS.map((h) => ({ value: h, label: t(`horizon.${h}` as Key) }));
const confidenceOptions = () => GOAL_CONFIDENCES.map((c) => ({ value: c, label: t(`confidence.${c}` as Key) }));
/** 时间粒度（ADR 0022）：日期填到多细为止，最细到周；空等价于「到周」，所以这一档不可空 */
const precisionOptions = () => GOAL_DATE_PRECISIONS.map((p) => ({ value: p, label: t(`date_precision.${p}` as Key) }));

/*
 * 目标详情 = 执行简报的目标版（DESIGN.md §17）：
 *   ① 目标是什么：标题、描述、进度与状态摘要句、动作（确认达成 / 放弃 / 重新开始 / 删除）
 *   ② 上级目标与里程碑：目标链（可点）、上级目标、里程碑面板
 *   ③ 任务与动态：子目标、任务表、动态
 *   ④ 负责人、预算与成本：负责人、团队、计划、截止日、预算 / 已花
 * 右栏只留次要的「基本信息」。放弃与删除都走后果对话框（§6），写清会影响几个子目标和任务。
 */
function GoalDetailBody({ id }: { id: string | null }) {
  const { session } = useSession();
  const router = useRouter();
  const goal = useLoad(() => (id ? api.goals.get(id) : Promise.reject(new Error(t("goal.missing")))), [id]);
  // 进度与任务数是整棵子树的汇总，任务表也要跟着看整棵子树（接口的 goal= 是精确匹配），否则两处对不上
  const tasks = useLoad(() => api.tasks.list({ limit: 500 }), [id]);
  const events = useLoad(() => api.events.list({ limit: 200 }), [id]);
  // 整棵目标树：上级目标的下拉、负责人链（谁能改）、以及"上级目标"一行显示名字而不是 ID
  const tree = useLoad(() => api.goals.list().catch(() => [] as Goal[]), [id]);
  // 目标类型词表（ADR 0023）：选择器只列启用的，本目标已有的停用类型照常列出
  const goalTypes = useLoad(() => api.goals.types().catch(() => [] as GoalType[]), []);
  const flat = useMemo(() => flattenGoals(tree.data ?? []), [tree.data]);
  const byId = useMemo(() => new Map(flat.map((f) => [f.goal.id, f.goal])), [flat]);
  const ex = useExecutors();
  const { busy, run } = useAction();
  const currency = session?.organization.currency;
  const types = useTaskTypeIndex();
  const saves = useInlineSaves();
  const [confirming, setConfirming] = useState<"abandon" | "delete" | null>(null);

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
  const typeList = useMemo(() => (goalTypes.data ?? []).filter((x) => x.active || x.id === shown?.type?.id), [goalTypes.data, shown?.type?.id]);
  const typeOptions = useMemo(() => typeList.map((x) => ({ value: x.id, label: x.name })), [typeList]);
  const typeRef = (idv: string) => typeList.find((x) => x.id === idv) ?? null;
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
  // 目标链（②）：上级一路向上，根在前
  const parents = useMemo(() => {
    const out: Goal[] = [];
    for (let g = shown?.parent_id ? byId.get(shown.parent_id) : undefined; g; g = g.parent_id ? byId.get(g.parent_id) : undefined) out.unshift(g);
    return out;
  }, [shown?.parent_id, byId]);

  if (!id || (goal.loading && !goal.data)) return <DetailSkeleton />;
  if (goal.error || !goal.data || !shown) return <ErrorBox message={goal.error ?? t("goal.notFound")} onRetry={goal.reload} />;
  const g = shown;
  const over = g.budget !== null && g.cost > g.budget;
  const grown = g.achieved || g.progress >= 100;
  const abandoned = g.status === "abandoned";
  const sum = goalSummary(g, tally.get(g.id));
  const childCount = subtree.size - 1;
  const openTasks = mine.filter((x) => x.state.label !== "terminal_success" && x.state.label !== "terminal_failure").length;
  const empty = g.children.length === 0 && g.task_count === 0;

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
  // 放弃 / 重新开始：保留全部历史，只是从"在做的事"里拿掉；删除只对空目标开放
  const abandon = async () => {
    const next = abandoned ? "active" : "abandoned";
    if (await run("abandon", () => api.goals.update(g.id, { status: next }), next === "abandoned" ? t("goals.abandonedToast", { title: g.title }) : t("goals.resumedToast", { title: g.title }))) { setConfirming(null); goal.reload(); events.reload(); tree.reload(); }
  };
  const remove = async () => {
    if (await run("delete", () => api.goals.remove(g.id), t("goals.deletedToast", { title: g.title }))) { setConfirming(null); router.push("/goals/"); }
  };
  const members = ex.data?.members ?? [];
  const memberOptions = members.map((m) => ({ value: m.id, label: m.name, icon: <Avatar name={m.name} size={16} /> }));
  // 团队按层级缩进
  const teams = session?.teams ?? [];
  const teamDepth = (tid: string | null): number => { let d = 0; for (let cur = teams.find((x) => x.id === tid); cur?.parent_id; cur = teams.find((x) => x.id === cur!.parent_id)) d++; return d; };
  const teamOptions = [...teams].sort((a, b) => teamDepth(a.id) - teamDepth(b.id)).map((tm) => ({ value: tm.id, label: tm.name, depth: teamDepth(tm.id) }));
  const teamName = g.team_id ? teams.find((x) => x.id === g.team_id)?.name ?? g.team_id : null;
  const parent = g.parent_id ? byId.get(g.parent_id) : undefined;
  const newTaskHref = `/tasks/?goal=${encodeURIComponent(g.id)}&new=1`;

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
            {abandoned && <Tag>{t("goals.abandonedTag")}</Tag>}
            {over && <Tag tone="danger">{t("goals.overBudget")}</Tag>}
          </div>
          <EnergyLine className="mt-2" />
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <Link href={`/tasks/?goal=${encodeURIComponent(g.id)}`} className="inline-flex"><Button icon={<IconTask />} tabIndex={-1}>{t("goal.viewTasks")}</Button></Link>
          {canEdit && (
            <>
              <Button variant="ghost" icon={<IconCancel />} disabled={busy === "abandon"} onClick={() => (abandoned ? void abandon() : setConfirming("abandon"))}>{abandoned ? t("goals.resume") : t("goals.abandon")}</Button>
              <Tip tip={empty ? null : t("goals.deleteBlocked")} placement="bottom">
                <Button variant="ghost" icon={<IconTrash />} disabled={!empty || busy === "delete"} onClick={() => setConfirming("delete")}>{t("common.delete")}</Button>
              </Tip>
              <Button variant={g.achieved ? "default" : "primary"} icon={g.achieved ? undefined : <IconCheck />} onClick={() => void toggleAchieved()} disabled={busy === "achieve"}>{g.achieved ? t("goal.unachieve") : t("goal.achieve")}</Button>
            </>
          )}
        </div>
      </div>

      {/* 主栏自适应，侧栏固定 380px（≥1920 时 420px） */}
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_380px] 3xl:grid-cols-[minmax(0,1fr)_420px]">
        <div className="space-y-6">
          {/* ① 目标是什么 */}
          <BriefSection n="01" title={t("goal.brief.what")} hint={t("goal.brief.whatHint")}>
            <Panel title={t("goals.progress")}>
              {/* 进度 = 96px 弧形仪表（舰内系统 v3 §3），旁边是完成数；预算仍用刻度进度条（可能超过 100%） */}
              <div className="flex items-center gap-4">
                <ArcGauge size={96} value={g.progress} tone={g.achieved ? "success" : "accent"} label={t("goals.progress")} delay={sprout ? 480 : 0} />
                <span className="min-w-0">
                  <span className="block text-body text-ink-muted">{t("goal.tasksDone", { done: g.done_task_count, total: g.task_count })}</span>
                  {/* 下一步该干什么（DESIGN.md §9），与目标列表同一套判定 */}
                  <span className={cx("mt-1 block text-body", sum.tone === "danger" ? "text-danger" : sum.tone === "warning" ? "text-warning" : sum.tone === "accent" ? "text-accent-hover" : "text-ink-subtle")}>{sum.text}</span>
                </span>
              </div>
              <div className="mt-4 border-t border-hairline pt-4 text-ink-muted">
                <InlineText value={g.description} editable={editNotes} placeholder={t("goal.noDescription")} onSave={(v) => save({ description: v }, { description: v })} />
              </div>
            </Panel>
          </BriefSection>

          {/* ② 上级目标与里程碑 */}
          <BriefSection n="02" title={t("goal.brief.parents")} hint={t("goal.brief.parentsHint")}>
            <Panel>
              <InlineTable>
                <InlineField label={t("goal.parent")} status={st("parent").status} error={st("parent").error}>
                  <span className="flex min-w-0 flex-wrap items-center gap-x-1 gap-y-0.5">
                    {parents.slice(0, -1).map((p) => (
                      <span key={p.id} className="inline-flex items-center gap-1 text-ink-muted">
                        <Link href={`/goals/${encodeURIComponent(p.id)}/`} className="hover:text-accent-hover">{p.title}</Link>
                        <span className="text-ink-tertiary">›</span>
                      </span>
                    ))}
                    <InlineSelect value={g.parent_id} options={parentOptions} editable={editPlan} nullable nullLabel={t("inline.topGoal")} ariaLabel={t("goal.parent")} loading={tree.loading && !tree.data}
                      display={g.parent_id ? <Link href={`/goals/${encodeURIComponent(g.parent_id)}/`} className="inl-text hover:text-accent-hover" onClick={(e) => editPlan && e.preventDefault()}>{parent?.title ?? g.parent_id}</Link> : <span className="text-ink-subtle">{t("inline.topGoal")}</span>}
                      onChange={(pid) => patch("parent", { parent_id: pid ?? "" }, { parent_id: pid })} />
                  </span>
                </InlineField>
                {/* 路线图三字段（ADR 0021）：成果指标说清达成时什么变了，时间桶是粗时间、与下面的精确日期互不覆盖，信心度只在路线图上呈现 */}
                <InlineField label={t("goal.outcome")} status={st("outcome").status} error={st("outcome").error}>
                  <InlineTextInput value={g.outcome ?? ""} editable={editNotes} placeholder={t("goal.outcomeUnset")} ariaLabel={t("goal.outcome")} onChange={(v) => patch("outcome", { outcome: v }, { outcome: v })} />
                </InlineField>
                <InlineField label={t("goal.horizon")} status={st("horizon").status} error={st("horizon").error}>
                  <InlineSelect value={g.horizon || null} options={horizonOptions()} editable={editPlan} nullable nullLabel={t("horizon.none")} ariaLabel={t("goal.horizon")}
                    display={g.horizon ? <span className="inl-text">{g.horizon_title || t(`horizon.${g.horizon}` as Key)}</span> : <span className="text-ink-subtle">{t("horizon.none")}</span>}
                    onChange={(v) => patch("horizon", { horizon: (v ?? "") as GoalHorizon }, { horizon: (v ?? "") as GoalHorizon, horizon_title: v ? t(`horizon.${v}` as Key) : "" })} />
                </InlineField>
                <InlineField label={t("goal.confidence")} status={st("confidence").status} error={st("confidence").error}>
                  <InlineSelect value={g.confidence || null} options={confidenceOptions()} editable={editPlan} nullable nullLabel={t("confidence.none")} ariaLabel={t("goal.confidence")}
                    display={g.confidence ? <span className="inl-text">{g.confidence_title || t(`confidence.${g.confidence}` as Key)}</span> : <span className="text-ink-subtle">{t("confidence.none")}</span>}
                    onChange={(v) => patch("confidence", { confidence: (v ?? "") as GoalConfidence }, { confidence: (v ?? "") as GoalConfidence, confidence_title: v ? t(`confidence.${v}` as Key) : "" })} />
                </InlineField>
                {/* 目标类型（ADR 0023）：组织自己维护的词表，只做分类不带流程。选择器只列启用的，本目标已有的停用类型照常列出，改不动的人看到的是只读文字 */}
                <InlineField label={t("goal.type")} status={st("type_id").status} error={st("type_id").error}>
                  <InlineSelect value={g.type?.id ?? null} options={typeOptions} editable={editPlan} nullable nullLabel={t("goalType.none")} ariaLabel={t("goal.type")} loading={goalTypes.loading && !goalTypes.data}
                    display={g.type ? <span className="inl-text inline-flex items-center gap-1.5"><i className="inline-block h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: g.type.color }} aria-hidden="true" />{g.type.name}</span> : <span className="text-ink-subtle">{t("goalType.none")}</span>}
                    onChange={(v) => patch("type_id", { type_id: v ?? "" }, { type: v ? typeRef(v) : null })} />
                </InlineField>
                {/* 时间粒度（ADR 0022）：决定路线图上这根条怎么画，也决定对外分享时显示到什么精度；在时间线上拖过条会落实成「到周」 */}
                <InlineField label={t("goal.datePrecision")} status={st("date_precision").status} error={st("date_precision").error}>
                  <InlineSelect value={effectivePrecision(g.date_precision)} options={precisionOptions()} editable={editPlan} ariaLabel={t("goal.datePrecision")}
                    display={<span className="inl-text">{g.date_precision_title || t(`date_precision.${effectivePrecision(g.date_precision)}` as Key)}</span>}
                    onChange={(v) => patch("date_precision", { date_precision: (v ?? "week") as GoalDatePrecision }, { date_precision: (v ?? "week") as GoalDatePrecision, date_precision_title: t(`date_precision.${v ?? "week"}` as Key) })} />
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
              </InlineTable>
            </Panel>
            {/* 里程碑（ADR 0016）：按日期列出，确认 / 撤销 / 编辑 / 删除，末尾一行内联新增 */}
            <MilestonePanel goal={g} onChanged={goal.reload} />
          </BriefSection>

          {/* ③ 任务与动态 */}
          <BriefSection n="03" title={t("goal.brief.work")} hint={t("goal.brief.workHint")}>
            {g.children.length > 0 && (
              <Panel icon={<IconGoal />} title={t("goal.children")} telemetry={t("panel.rows", { n: g.children.length })} padded={false}>
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
            <Panel icon={<IconTask />} title={t("goal.tasks")} telemetry={tasks.data ? t("panel.rows", { n: mine.length }) : undefined} padded={false} actions={<span className="inline-flex items-center gap-2"><Link href={`/tasks/?goal=${encodeURIComponent(g.id)}`} className="text-caption text-ink-muted hover:text-accent-hover">{t("goal.filterTasks")}</Link><Link href={newTaskHref} className="inline-flex"><Button size="sm" icon={<IconPlus />} tabIndex={-1}>{t("tasks.new")}</Button></Link></span>}>
              {tasks.loading && !tasks.data ? <TableSkeleton rows={3} cols={6} /> : tasks.error ? <div className="p-4"><ErrorBox message={tasks.error} onRetry={tasks.reload} /></div> : (
                <TaskTable tasks={mine} currency={currency} showGoal={g.children.length > 0} emptyText={t("goal.noTasksHint")} emptyAction={<Link href={newTaskHref} className="inline-flex"><Button variant="primary" icon={<IconPlus />} tabIndex={-1}>{t("goal.newTask")}</Button></Link>} onChanged={tasks.reload} />
              )}
            </Panel>
            <Panel icon={<IconLog />} title={t("goal.events")}>
              {events.loading && !events.data ? <ListSkeleton rows={4} /> : <EventList events={(events.data ?? []).filter((e) => e.goal_id === g.id)} poll={{ limit: 200, filter: (e) => e.goal_id === g.id }} />}
            </Panel>
          </BriefSection>

          {/* ④ 负责人、预算与成本 */}
          <BriefSection n="04" title={t("goal.brief.owner")} hint={t("goal.brief.ownerHint")}>
            <Panel>
              <InlineTable>
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
                <InlineField label={t("goal.budget")} status={st("budget").status} error={st("budget").error}>
                  <InlineNumber value={g.budget} editable={editPlan} min={0} step={0.01} prefix={currencySymbol(currency)} format={(v) => fmtNumber(v)} placeholder={t("goal.budgetUnset")} ariaLabel={t("goal.budget")} onChange={(v) => patch("budget", { budget: v }, { budget: v })} />
                </InlineField>
                <InlineField label={t("goal.cost")}>
                  <span className="flex min-w-0 flex-col gap-1">
                    <span className={cx("tabular-nums", over && "text-danger")}><Odometer value={fmtMoney(g.cost, currency)} />{g.budget !== null && <> / <Odometer value={fmtMoney(g.budget, currency)} /></>}</span>
                    {g.budget !== null && <ProgressBar value={g.budget ? (g.cost / g.budget) * 100 : 0} tone={over ? "danger" : "accent"} className="w-40" />}
                    {over && <span className="text-caption text-danger">{t("goal.overBudgetHint")}</span>}
                  </span>
                </InlineField>
              </InlineTable>
            </Panel>
          </BriefSection>
        </div>

        <div className="space-y-4">
          <Panel title={t("goal.info")}>
            <InlineTable>
              <InlineField label={t("common.id")}><IdLine id={g.id} /></InlineField>
              <InlineField label={t("goal.actual")}>{`${fmtDate(g.actual_start, true)} – ${g.actual_end ? fmtDate(g.actual_end, true) : t("common.inProgress")}`}</InlineField>
              <InlineField label={t("goals.tasksLabel")}>{`${g.done_task_count}/${g.task_count}`}</InlineField>
              <InlineField label={t("goal.children")}>{childCount > 0 ? t("goals.childCount", { n: childCount }) : <span className="text-ink-subtle">{t("common.none")}</span>}</InlineField>
            </InlineTable>
          </Panel>
        </div>
      </div>

      {/* 放弃：写清会影响几个子目标和任务；删除只对空目标开放 */}
      <ConsequenceDialog
        open={confirming === "abandon"}
        title={t("goals.abandonTitle", { title: g.title })}
        effects={[
          childCount > 0 ? t("goals.abandonEffect.children", { n: childCount }) : null,
          openTasks > 0 ? t("goals.abandonEffect.tasks", { n: openTasks }) : t("goals.abandonEffect.noOpenTasks"),
          t("goals.abandonEffect.kept"),
        ]}
        confirmLabel={t("goals.abandon")}
        danger
        busy={busy === "abandon"}
        onConfirm={() => void abandon()}
        onClose={() => setConfirming(null)}
      />
      <ConsequenceDialog
        open={confirming === "delete"}
        title={t("goals.deleteTitle")}
        effects={[t("goals.deleteMessage", { title: g.title })]}
        confirmLabel={t("common.delete")}
        danger
        busy={busy === "delete"}
        onConfirm={() => void remove()}
        onClose={() => setConfirming(null)}
      />
    </div>
  );
}
