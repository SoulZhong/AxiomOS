"use client";
import Link from "next/link";
import { useState } from "react";
import { api, isTerminal, type Task } from "@/lib/api";
import { fmtDate, fmtMoney, today, parseDate } from "@/lib/format";
import { useAction } from "@/lib/hooks";
import { recentlyClaimed, rememberClaim } from "@/lib/motion";
import { isAcceptanceWait, isBugType, useTaskTypeIndex } from "@/lib/states";
import { priorityTitle } from "@/lib/terms";
import { t as tt } from "@/lib/i18n";
import { useSession } from "./AppShell";
import { IconBug, IconHand, IconOverdue, IconPlay } from "./icons";
import { StateBadge, stateMotion } from "./StateBadge";
import { Button, Empty, ExecutorName, Table, Tag, TaskLink, cx } from "./ui";

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
 */
export function TaskTable({ tasks, currency = "CNY", showGoal = true, compact = false, emptyText, emptyAction, highlightId, onChanged }: { tasks: Task[]; currency?: string; showGoal?: boolean; compact?: boolean; emptyText?: string; emptyAction?: React.ReactNode; highlightId?: string | null; onChanged?: () => void }) {
  const { session } = useSession();
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

  return (
    <Table>
      <thead>
        <tr>
          <th className="w-full">{tt("taskTable.task")}</th>
          <th className="w-[80px]">{tt("taskTable.type")}</th>
          <th className="w-[118px]">{tt("taskTable.state")}</th>
          <th className="w-[110px]">{tt("taskTable.assignee")}</th>
          {showGoal && <th className="w-[130px]">{tt("taskTable.goal")}</th>}
          <th className="w-[130px]">{tt("taskTable.plan")}</th>
          {!compact && <th className="w-[60px]">{tt("taskTable.priority")}</th>}
          {!compact && <th className="num w-[88px]">{tt("taskTable.cost")}</th>}
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
            <tr key={x.id} className={cx(highlightId === x.id && "row-new")} data-motion={snap.motions.get(x.id)}>
              <td className="w-full max-w-0 min-w-[176px]">
                <span className="flex items-center gap-2">
                  <TaskLink id={x.id} title={x.title} className="min-w-0 flex-1" />
                  {isOverdue(x) && <OverdueTag>{tt("taskTable.overdue")}</OverdueTag>}
                </span>
              </td>
              <td className="whitespace-nowrap"><TypeLabel type={x.type} title={x.type_title} /></td>
              <td className="whitespace-nowrap">
                <StateBadge state={x.state} accept={accept(x)} />
              </td>
              <td className="max-w-[130px] whitespace-nowrap">
                <ExecutorName executor={x.assignee} empty={tt("taskTable.unclaimed")} className="max-w-full" />
              </td>
              {showGoal && (
                <td className="max-w-[150px]">
                  {x.goal ? (
                    <Link href={`/goals/${encodeURIComponent(x.goal.id)}/`} className="block truncate text-ink-muted hover:text-accent-hover" title={x.goal.title}>
                      {x.goal.title}
                    </Link>
                  ) : (
                    <span className="text-ink-subtle">—</span>
                  )}
                </td>
              )}
              <td className="whitespace-nowrap">
                <span className={cx("font-mono text-mono", isOverdue(x) ? "text-danger" : "text-ink-muted")} title={isOverdue(x) ? tt("taskTable.overdue") : undefined}>
                  {fmtDate(x.planned_start)} – {fmtDate(x.planned_end)}
                </span>
              </td>
              {!compact && (
                <td className="whitespace-nowrap">
                  <PriorityTag priority={x.priority} />
                </td>
              )}
              {!compact && <td className="num whitespace-nowrap">{x.cost ? <span className="telemetry">{fmtMoney(x.cost, currency)}</span> : <span className="text-ink-subtle">—</span>}</td>}
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
