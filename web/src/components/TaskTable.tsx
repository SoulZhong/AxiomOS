"use client";
import Link from "next/link";
import { useState } from "react";
import { api, isTerminal, type LinkStatus, type Task, type TaskPR } from "@/lib/api";
import { fmtDate, fmtMoney, today, parseDate } from "@/lib/format";
import { useAction } from "@/lib/hooks";
import { recentlyClaimed, rememberClaim } from "@/lib/motion";
import { usePreferences } from "@/lib/preferences";
import { isAcceptanceWait, isBugType, useTaskTypeIndex } from "@/lib/states";
import { priorityTitle } from "@/lib/terms";
import { t as tt } from "@/lib/i18n";
import { useSession } from "./AppShell";
import { IconBug, IconExternal, IconHand, IconOverdue, IconPlay } from "./icons";
import { StateBadge, stateMotion } from "./StateBadge";
import { PointsChip } from "./board/PointsChips";
import { Button, Empty, ExecutorName, Table, Tag, TaskLink, TaskNumber, cx, type Tone } from "./ui";

export function isOverdue(t: { planned_end: string | null; state: { label: Task["state"]["label"] } }): boolean {
  const pe = parseDate(t.planned_end);
  return !!pe && pe < today() && !isTerminal(t.state.label);
}

export function PriorityTag({ priority }: { priority: Task["priority"] }) {
  if (priority === "normal") return <span className="text-ink-subtle">{priorityTitle("normal")}</span>;
  if (priority === "low") return <Tag className="!text-ink-subtle">{priorityTitle("low")}</Tag>;
  return <Tag tone={priority === "urgent" ? "danger" : "warning"}>{priorityTitle(priority)}</Tag>;
}

/** 逾期标签：时钟加告警灯，灯沿用"双闪后休息 4 秒"。 */
export function OverdueTag({ children }: { children: React.ReactNode }) {
  return (
    <Tag tone="danger">
      <IconOverdue size={12} className="tag-icon" />
      {children}
    </Tag>
  );
}
/** 外部链接状态的颜色（ADR 0020）：打开 / 通过是好的，失败是红的，草稿与已关闭是灰的。 */
export const LINK_STATUS_TONE: Record<LinkStatus, Tone> = { open: "accent", draft: "neutral", merged: "success", closed: "neutral", passed: "success", failed: "danger" };

/**
 * 列表行与看板卡片上的 PR 小标（ADR 0020）：最近更新的那条 PR 链接的状态，点一下去代码平台。
 * 同一个任务上挂了多条 PR 时右边跟一个「共 N 条 PR」。
 */
export function PrChip({ pr, className }: { pr: TaskPR; className?: string }) {
  return (
    <a
      href={pr.url}
      target="_blank"
      rel="noreferrer noopener"
      className={cx("inline-flex shrink-0 items-center gap-1 hover:opacity-80", className)}
      title={pr.count > 1 ? `${pr.title} · ${tt("task.prMore", { n: pr.count })}` : pr.title}
      aria-label={`PR ${pr.status_title}`}
    >
      <Tag tone={LINK_STATUS_TONE[pr.status] ?? "neutral"}>
        PR
        <span className="ml-1">{pr.status_title}</span>
        {pr.count > 1 && <span className="ml-1 opacity-70">+{pr.count - 1}</span>}
      </Tag>
      <IconExternal size={11} className="text-ink-subtle" aria-hidden="true" />
    </a>
  );
}

/** 任务类型：内置 Bug 类型前面带「刷子与异物」，其余只显示名字。 */
export function TypeLabel({ type, title, tag = false }: { type: string; title: string; tag?: boolean }) {
  const icon = isBugType(type) ? <IconBug size={tag ? 12 : 14} className={tag ? "tag-icon" : "mr-1.5 text-ink-subtle"} /> : null;
  if (tag) return <Tag>{icon}{title}</Tag>;
  return <span className="inline-flex items-center">{icon}{title}</span>;
}

type RowMotion = "compact" | "scan" | "picked" | undefined;

/**
 * 任务表：第一列是一行省略的标题（点标题进详情，ID 不印在列表里）；悬停整行显示行内动作（领取 / 开始执行），动作列定宽、行不跳。
 * highlightId：刚创建的任务高亮 2 秒。onChanged：行内动作成功后让父级重新加载。
 * 签名动效（只在同一张表里状态真的变了时）：行进入成功终态 = 压实；进入待验收 = 一道扫描光扫过该行；
 * 刚从待领取任务领走的任务第一次出现在表里 = 从左滑入（motion.ts 的 recentlyClaimed）。
 * onOpen：点行的空白处开抽屉（j / k 选中的行也用 data-task-row 找到）；标题链接仍进完整页。
 */
/** 列键（与 GET /me/preferences/catalog 的 columns 一致）；title 固定有 */
export type TaskColumn = "number" | "title" | "type" | "state" | "assignee" | "reviewer" | "goal" | "sprint" | "priority" | "points" | "planned" | "due" | "cost";
const COLUMN_ORDER: TaskColumn[] = ["number", "title", "type", "state", "assignee", "reviewer", "goal", "sprint", "priority", "points", "planned", "due", "cost"];
/** 紧凑用法（工作台区块、迭代详情）不显示的列 */
const COMPACT_HIDES: TaskColumn[] = ["reviewer", "sprint", "priority", "points", "cost"];
const isColumn = (k: string): k is TaskColumn => (COLUMN_ORDER as string[]).includes(k);

export function TaskTable({ tasks, currency = "CNY", showGoal = true, compact = false, columns: columnsProp, emptyText, emptyAction, highlightId, onChanged, onOpen }: { tasks: Task[]; currency?: string; showGoal?: boolean; compact?: boolean; /** 显示哪些列（缺省按显示偏好 DESIGN.md §20；顺序按目录固定） */ columns?: string[]; emptyText?: string; emptyAction?: React.ReactNode; highlightId?: string | null; onChanged?: () => void; /** 点行（标题链接与按钮之外）：任务页传入后打开抽屉（DESIGN.md §17）；标题链接始终进完整页 */ onOpen?: (id: string) => void }) {
  const { session } = useSession();
  const { prefs } = usePreferences();
  const wanted = new Set((columnsProp ?? prefs.task_list_columns).filter(isColumn));
  wanted.add("title");
  const cols = COLUMN_ORDER.filter((c) => wanted.has(c) && (showGoal || c !== "goal") && (!compact || !COMPACT_HIDES.includes(c)));
  const { busy, run } = useAction();
  const types = useTaskTypeIndex();
  const accept = (x: Task) => isAcceptanceWait(x.state, types[x.type]?.workflow);
  // 同一张表内比较每行的状态名（渲染期调整状态，不用 effect）：状态变了就记下该播的动效，播过留着直到再变；
  // 第一次出现在表里的行如果是刚领走的任务，则从左滑入。
  const sig = tasks.map((x) => `${x.id}:${x.state.name}`).join("\n");
  const [snap, setSnap] = useState<{ sig: string; states: Map<string, string>; motions: Map<string, RowMotion> }>(() => ({
    sig,
    states: new Map(tasks.map((x) => [x.id, x.state.name])),
    motions: new Map(tasks.map((x) => [x.id, recentlyClaimed(x.id) ? "picked" : undefined])),
  }));
  if (snap.sig !== sig) {
    const motions = new Map(snap.motions);
    const states = new Map<string, string>();
    for (const x of tasks) {
      const prev = snap.states.get(x.id);
      if (prev === undefined) motions.set(x.id, recentlyClaimed(x.id) ? "picked" : undefined);
      else if (prev !== x.state.name) motions.set(x.id, stateMotion(x.state.label, accept(x)));
      states.set(x.id, x.state.name);
    }
    setSnap({ sig, states, motions });
  }

  if (!tasks.length) return <Empty text={emptyText ?? tt("taskTable.empty")} action={emptyAction} />;
  const me = session?.member.id;

  const claim = (x: Task) =>
    run(x.id, () => api.tasks.claim(x.id), tt("toast.claimed")).then((ok) => {
      if (ok) {
        rememberClaim(x.id);
        onChanged?.();
      }
    });
  const begin = (x: Task) => run(x.id, () => api.tasks.begin(x.id), tt("toast.begun")).then((ok) => ok && onChanged?.());

  const HEAD: Record<TaskColumn, React.ReactNode> = {
    number: <th key="number" className="w-[56px]">{tt("taskTable.number")}</th>,
    title: <th key="title" className="w-full">{tt("taskTable.task")}</th>,
    type: <th key="type" className="w-[80px]">{tt("taskTable.type")}</th>,
    state: <th key="state" className="w-[118px]">{tt("taskTable.state")}</th>,
    assignee: <th key="assignee" className="w-[110px]">{tt("taskTable.assignee")}</th>,
    reviewer: <th key="reviewer" className="w-[110px]">{tt("taskTable.reviewer")}</th>,
    goal: <th key="goal" className="w-[130px]">{tt("taskTable.goal")}</th>,
    sprint: <th key="sprint" className="w-[110px]">{tt("taskTable.sprint")}</th>,
    priority: <th key="priority" className="w-[60px]">{tt("taskTable.priority")}</th>,
    points: <th key="points" className="num w-[64px]">{tt("taskTable.points")}</th>,
    planned: <th key="planned" className="w-[130px]">{tt("taskTable.plan")}</th>,
    due: <th key="due" className="w-[96px]">{tt("taskTable.due")}</th>,
    cost: <th key="cost" className="num w-[88px]">{tt("taskTable.cost")}</th>,
  };
  const cell = (c: TaskColumn, x: Task) => {
    const overdue = isOverdue(x);
    switch (c) {
      case "number":
        return <td key={c} className="whitespace-nowrap"><TaskNumber n={x.number} /></td>;
      case "title":
        return (
          <td key={c} className="w-full max-w-0 min-w-[176px]">
            <span className="flex items-center gap-2">
              <TaskLink id={x.id} title={x.title} className="min-w-0 flex-1" />
              {x.pr && <PrChip pr={x.pr} />}
              {overdue && <OverdueTag>{tt("taskTable.overdue")}</OverdueTag>}
            </span>
          </td>
        );
      case "type":
        return <td key={c} className="whitespace-nowrap"><TypeLabel type={x.type} title={x.type_title} /></td>;
      case "state":
        return <td key={c} className="whitespace-nowrap"><StateBadge state={x.state} accept={accept(x)} /></td>;
      case "assignee":
        return <td key={c} className="max-w-[130px] whitespace-nowrap"><ExecutorName executor={x.assignee} empty={tt("taskTable.unclaimed")} className="max-w-full" /></td>;
      case "reviewer":
        return <td key={c} className="max-w-[130px] whitespace-nowrap"><ExecutorName executor={x.reviewer?.id ? x.reviewer : null} className="max-w-full" /></td>;
      case "goal":
        return (
          <td key={c} className="max-w-[150px]">
            {x.goal ? (
              <Link href={`/goals/${encodeURIComponent(x.goal.id)}/`} className="block truncate text-ink-muted hover:text-accent-hover" title={x.goal.title}>
                {x.goal.title}
              </Link>
            ) : (
              <span className="text-ink-subtle">—</span>
            )}
          </td>
        );
      case "sprint":
        return <td key={c} className="max-w-[130px]">{x.sprint ? <Link href={`/sprints/${encodeURIComponent(x.sprint.id)}/`} className="block truncate text-ink-muted hover:text-accent-hover" title={x.sprint.name}>{x.sprint.name}</Link> : <span className="text-ink-subtle">—</span>}</td>;
      case "priority":
        return <td key={c} className="whitespace-nowrap"><PriorityTag priority={x.priority} /></td>;
      case "points":
        return <td key={c} className="num whitespace-nowrap">{x.points != null ? <PointsChip value={x.points} /> : <span className="text-ink-subtle">—</span>}</td>;
      case "planned":
        return (
          <td key={c} className="whitespace-nowrap">
            <span className={cx("font-mono text-mono", overdue ? "text-danger" : "text-ink-muted")} title={overdue ? tt("taskTable.overdue") : undefined}>
              {fmtDate(x.planned_start)} – {fmtDate(x.planned_end)}
            </span>
          </td>
        );
      case "due":
        return (
          <td key={c} className="whitespace-nowrap">
            {x.planned_end ? <span className={cx("font-mono text-mono", overdue ? "text-danger" : "text-ink-muted")}>{fmtDate(x.planned_end)}</span> : <span className="text-ink-subtle">—</span>}
          </td>
        );
      case "cost":
        return <td key={c} className="num whitespace-nowrap">{x.cost ? <span className="telemetry">{fmtMoney(x.cost, currency)}</span> : <span className="text-ink-subtle">—</span>}</td>;
    }
  };

  return (
    <Table>
      <thead>
        <tr>
          {cols.map((c) => HEAD[c])}
          <th className="actions w-[104px]" aria-label={tt("common.actions")} />
        </tr>
      </thead>
      <tbody>
        {tasks.map((x) => {
          const terminal = isTerminal(x.state.label);
          const canClaim = !x.assignee && !terminal;
          const mine = !!me && !!x.assignee && (x.assignee.id === me || x.assignee.owner_id === me);
          const canBegin = mine && x.state.label === "pending";
          return (
            <tr
              key={x.id}
              className={cx(highlightId === x.id && "row-new", onOpen && "cursor-pointer")}
              data-motion={snap.motions.get(x.id)}
              data-task-row={x.id}
              onClick={onOpen ? (e) => { if ((e.target as HTMLElement).closest("a,button,input,select,textarea")) return; onOpen(x.id); } : undefined}
            >
              {cols.map((c) => cell(c, x))}
              <td className="actions">
                <span className="row-actions">
                  {canClaim && (
                    <Button size="sm" variant="ghost" icon={<IconHand />} disabled={busy === x.id} onClick={() => void claim(x)}>
                      {tt("task.claim")}
                    </Button>
                  )}
                  {canBegin && (
                    <Button size="sm" variant="ghost" icon={<IconPlay />} disabled={busy === x.id} onClick={() => void begin(x)}>
                      {tt("task.begin")}
                    </Button>
                  )}
                </span>
              </td>
            </tr>
          );
        })}
      </tbody>
    </Table>
  );
}
