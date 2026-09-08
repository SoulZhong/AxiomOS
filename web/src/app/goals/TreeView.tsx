"use client";
import Link from "next/link";
import { useMemo, useState } from "react";
import { api, type Goal } from "@/lib/api";
import { isAcceptanceWait, useTaskTypeIndex } from "@/lib/states";
import { fmtDate, fmtMoney } from "@/lib/format";
import { useAction, useLoad, useSprout } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { flattenGoals } from "@/components/GoalDrawer";
import { useSession } from "@/components/AppShell";
import { IconCancel, IconCheck, IconChevronRight, IconGoal, IconPlus, IconTrash } from "@/components/icons";
import { ArcGauge } from "@/components/instruments/ArcGauge";
import { Odometer } from "@/components/instruments/Odometer";
import { goalSummary, tallyGoals, type GoalTally } from "./goalStatus";
import { Avatar, Button, ConsequenceDialog, Empty, ErrorBox, ListSkeleton, Panel, StatChips, Tag, Tip, cx } from "@/components/ui";

/**
 * 「目标 · 树」（DESIGN.md §9）：树形行，缩进 20px/层带引导线；每行给任务完成数、状态摘要句、计划区间、预算与已花，右侧弧形仪表。
 * 路线图页签（§25、ADR 0022）另由 roadmap/RoadmapView 提供；两者读同一批目标、同一个范围与同一个类型筛选。
 * type = 地址栏的 ?type=<id>（或 none）：直接交给 GET /goals?type=，命中的目标各自做顶级，子树照旧。
 */
export function TreeView({ reloadKey, highlight, type, onAddChild, onNew, onClearType }: { reloadKey: number; highlight: string | null; type: string | null; onAddChild: (parentId: string) => void; onNew: () => void; onClearType: () => void }) {
  const { session } = useSession();
  const goals = useLoad(() => api.goals.list(type ? { type } : {}), [reloadKey, type]);
  // 目标行要说清"下一步做什么"，需要它名下任务的分布：一次拉全量任务在前端按目标树汇总
  const tasks = useLoad(() => api.tasks.list({ limit: 500 }), [reloadKey]);
  const types = useTaskTypeIndex();
  const [deleting, setDeleting] = useState<Goal | null>(null);
  const [abandoning, setAbandoning] = useState<Goal | null>(null);
  const { busy, run } = useAction();
  // 放弃 / 重新开始：保留全部历史，只是从"在做的事"里拿掉；放弃先过后果对话框（几个子目标、几个未结束任务），重新开始直接做；删除只对空目标开放
  const doAbandon = (g: Goal) => {
    const next = g.status === "abandoned" ? "active" : "abandoned";
    void run(g.id, () => api.goals.update(g.id, { status: next }), next === "abandoned" ? t("goals.abandonedToast", { title: g.title }) : t("goals.resumedToast", { title: g.title })).then((ok) => { if (ok) { setAbandoning(null); goals.reload(); } });
  };
  const abandon = (g: Goal) => (g.status === "abandoned" ? doAbandon(g) : setAbandoning(g));

  const achieve = (g: Goal) => {
    void run(g.id, () => api.goals.update(g.id, { achieved: true }), t("goals.achievedToast", { title: g.title })).then((ok) => ok && goals.reload());
  };
  const doDelete = () => {
    const g = deleting;
    if (!g) return;
    void run(g.id, () => api.goals.remove(g.id), t("goals.deletedToast", { title: g.title })).then((ok) => {
      if (ok) {
        setDeleting(null);
        goals.reload();
      }
    });
  };
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const flat = useMemo(() => flattenGoals(goals.data ?? []), [goals.data]);
  const tally = useMemo(() => tallyGoals(goals.data ?? [], tasks.data ?? [], (x) => isAcceptanceWait(x.state, types[x.type]?.workflow)), [goals.data, tasks.data, types]);
  const abandonTally = abandoning ? tally.get(abandoning.id) : undefined;
  const abandonOpen = abandonTally ? abandonTally.overdue + abandonTally.waiting + abandonTally.active + abandonTally.pending : 0;
  const abandonKids = abandoning ? flattenGoals(abandoning.children ?? []).length : 0;
  const currency = session?.organization.currency;
  const achieved = flat.filter((x) => x.goal.achieved).length;
  const over = flat.filter((x) => x.goal.budget !== null && x.goal.cost > x.goal.budget).length;

  const toggle = (id: string) =>
    setCollapsed((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });

  return (
    <div>
      <StatChips
        className="mb-4"
        value={null}
        total={flat.length}
        items={[
          { key: "achieved", label: t("goals.achieved"), value: achieved },
          { key: "over", label: t("goals.overBudget"), value: over, tone: "danger" },
        ]}
      />
      <Panel index={1} icon={<IconGoal />} title={t("goals.view.tree")} telemetry={goals.data ? t("panel.rows", { n: flat.length }) : undefined} padded={false}>
        {goals.loading && !goals.data ? (
          <ListSkeleton rows={5} />
        ) : goals.error ? (
          <div className="p-4"><ErrorBox message={goals.error} onRetry={goals.reload} /></div>
        ) : flat.length === 0 ? (
          // 「一个目标都没有」与「这个类型下没有」是两回事：后者请他去掉类型筛选
          type ? (
            <Empty text={t("roadmap.emptyFiltered")} action={<Button onClick={onClearType}>{t("roadmap.clearFilters")}</Button>} />
          ) : (
            <Empty text={t("goals.empty")} action={<Button variant="primary" icon={<IconPlus />} onClick={onNew}>{t("goals.new")}</Button>} />
          )
        ) : (
          <ul>
            {(goals.data ?? []).map((g) => (
              <GoalRow key={g.id} goal={g} depth={0} collapsed={collapsed} onToggle={toggle} onAddChild={onAddChild} onAbandon={abandon} onDelete={setDeleting} onAchieve={achieve} tally={tally} busy={busy} currency={currency} highlight={highlight} />
            ))}
          </ul>
        )}
      </Panel>
      <ConsequenceDialog open={!!deleting} title={t("goals.deleteTitle")} effects={[deleting ? t("goals.deleteMessage", { title: deleting.title }) : null]} confirmLabel={t("common.delete")} danger busy={busy === deleting?.id} onConfirm={doDelete} onClose={() => setDeleting(null)} />
      <ConsequenceDialog
        open={!!abandoning}
        title={abandoning ? t("goals.abandonTitle", { title: abandoning.title }) : ""}
        effects={[abandonKids > 0 ? t("goals.abandonEffect.children", { n: abandonKids }) : null, abandonOpen > 0 ? t("goals.abandonEffect.tasks", { n: abandonOpen }) : t("goals.abandonEffect.noOpenTasks"), t("goals.abandonEffect.kept")]}
        confirmLabel={t("goals.abandon")}
        danger
        busy={busy === abandoning?.id}
        onConfirm={() => abandoning && doAbandon(abandoning)}
        onClose={() => setAbandoning(null)}
      />
    </div>
  );
}

const INDENT = 20;
const GUTTER = 12;

/** 目标树的一行：缩进 20px/层 + 1px 竖向引导线；根目标标题 16px/500；指标组 = 任务 / 计划 / 预算 三个小标签值；右侧 64px 弧形仪表 = 进度（舰内系统 v3 §3）。 */
function GoalRow({ goal, depth, collapsed, onToggle, onAddChild, onAbandon, onDelete, onAchieve, tally, busy, currency, highlight }: { goal: Goal; depth: number; collapsed: Set<string>; onToggle: (id: string) => void; onAddChild: (id: string) => void; onAbandon: (g: Goal) => void; onDelete: (g: Goal) => void; onAchieve: (g: Goal) => void; tally: Map<string, GoalTally>; busy: string | null; currency?: string; highlight: string | null }) {
  const kids = goal.children ?? [];
  const sprout = useSprout(goal);
  const grown = goal.achieved || goal.progress >= 100;
  const open = !collapsed.has(goal.id);
  const overBudget = goal.budget !== null && goal.cost > goal.budget;
  const empty = kids.length === 0 && goal.task_count === 0;
  const sum = goalSummary(goal, tally.get(goal.id));
  const metric = (label: string, value: React.ReactNode, danger = false) => (
    <span className="inline-flex items-baseline gap-1 whitespace-nowrap">
      <span className="eyebrow text-ink-subtle">{label}</span>
      <span className={cx("text-caption tabular-nums", danger ? "text-danger" : "text-ink-muted")}>{value}</span>
    </span>
  );
  return (
    <li>
      <div className={cx("relative flex items-center gap-3 border-b border-hairline py-2 pr-3 last:border-b-0 hover:bg-surface-2", highlight === goal.id && "row-new")} style={{ paddingLeft: GUTTER + depth * INDENT }}>
        {/* 祖先层级的竖向引导线：每层一条，穿过整行 */}
        {Array.from({ length: depth }, (_, i) => (
          <span key={i} className="absolute top-0 bottom-0 w-px bg-hairline" style={{ left: GUTTER + i * INDENT + 11 }} aria-hidden="true" />
        ))}
        <button type="button" onClick={() => onToggle(goal.id)} className={cx("pressable relative flex h-6 w-6 shrink-0 items-center justify-center rounded-xs text-ink-subtle hover:bg-surface-3 hover:text-ink", kids.length === 0 && "invisible")} aria-expanded={open} aria-label={open ? t("goals.collapse") : t("goals.expand")}>
          {/* 一枚向右的箭头，展开时转 90°（.chevron） */}
          <IconChevronRight className={cx("chevron", open && "chevron-open")} />
        </button>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            {/* 靴中的芽：未完成只有芽，完成后长出叶子 */}
            <span className={cx("inline-flex shrink-0", grown ? "text-success" : "text-ink-subtle")} data-motion={sprout ? "sprout" : undefined} aria-hidden="true"><IconGoal stage={grown ? "grown" : "bud"} /></span>
            <Link href={`/goals/${encodeURIComponent(goal.id)}/`} className={cx("min-w-0 truncate font-medium text-ink hover:text-accent-hover", depth === 0 ? "text-title leading-[1.4]" : "text-body")} title={goal.title}>
              {goal.title}
            </Link>
            {goal.achieved && <Tag tone="success">{t("goals.achieved")}</Tag>}
            {goal.status === "abandoned" && <Tag>{t("goals.abandonedTag")}</Tag>}
            {overBudget && <Tag tone="danger">{t("goals.overBudget")}</Tag>}
            {kids.length > 0 && <span className="whitespace-nowrap text-caption text-ink-subtle">{t("goals.childCount", { n: kids.length })}</span>}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-0.5">
            <span className="inline-flex items-center gap-1.5 text-caption text-ink-muted">
              <Avatar name={goal.owner.name} size={16} />
              <span className="truncate">{goal.owner.name}</span>
            </span>
            {metric(t("goals.tasksLabel"), `${goal.done_task_count}/${goal.task_count}`)}
            {/* 状态摘要句：一句话说清下一步该干什么（DESIGN.md §9） */}
            <span className={cx("text-caption", sum.tone === "danger" ? "text-danger" : sum.tone === "warning" ? "text-warning" : sum.tone === "accent" ? "text-accent-hover" : "text-ink-subtle")}>{sum.text}</span>
            {metric(t("goal.plan"), `${fmtDate(goal.planned_start)} – ${fmtDate(goal.planned_end)}`)}
            {goal.budget !== null && metric(t("goal.budget"), <><Odometer value={fmtMoney(goal.cost, currency)} /> / <Odometer value={fmtMoney(goal.budget, currency)} /></>, overBudget)}
          </div>
        </div>
        <div className="hidden w-20 shrink-0 justify-end sm:flex">
          <ArcGauge size={64} value={goal.progress} tone={goal.achieved ? "success" : "accent"} label={t("goals.progress")} delay={sprout ? 480 : 0} />
        </div>
        {/* 标题本身就是详情链接，这里不再重复「查看」，留出宽度给放弃与删除 */}
        <span className="row-actions shrink-0 whitespace-nowrap">
          {sum.needsAchieve && <Button size="sm" variant="primary" icon={<IconCheck />} disabled={busy === goal.id} onClick={() => onAchieve(goal)}>{t("goal.achieve")}</Button>}
          <Button size="sm" variant="ghost" icon={<IconPlus />} onClick={() => onAddChild(goal.id)}>{t("goals.addChild")}</Button>
          <Link href={`/tasks/?goal=${encodeURIComponent(goal.id)}&new=1`} className="inline-flex"><Button size="sm" variant="ghost" icon={<IconPlus />} tabIndex={-1}>{t("tasks.new")}</Button></Link>
          <Button size="sm" variant="ghost" icon={<IconCancel />} disabled={busy === goal.id} onClick={() => onAbandon(goal)}>{goal.status === "abandoned" ? t("goals.resume") : t("goals.abandon")}</Button>
          {/* 只有空目标能删；非空时按钮禁用并说明原因 */}
          <Tip tip={empty ? null : t("goals.deleteBlocked")} placement="bottom">
            <Button size="sm" variant="ghost" icon={<IconTrash />} disabled={!empty || busy === goal.id} onClick={() => onDelete(goal)}>{t("common.delete")}</Button>
          </Tip>
        </span>
      </div>
      {/* 子级常驻：折叠用 grid-template-rows 1fr → 0fr 过渡（.fold），收起时 inert，不可聚焦也不进无障碍树 */}
      {kids.length > 0 && (
        <div className="fold" data-closed={open ? undefined : ""} inert={!open} aria-hidden={!open}>
          <ul>
            {kids.map((c) => (
              <GoalRow key={c.id} goal={c} depth={depth + 1} collapsed={collapsed} onToggle={onToggle} onAddChild={onAddChild} onAbandon={onAbandon} onDelete={onDelete} onAchieve={onAchieve} tally={tally} busy={busy} currency={currency} highlight={highlight} />
            ))}
          </ul>
        </div>
      )}
    </li>
  );
}
