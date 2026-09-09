"use client";
import type { ReactNode } from "react";
import { type ExecutorRef, type GoalPlanPayload, type GoalPlanTask, type Priority } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import { t } from "@/lib/i18n";
import { priorityTitle } from "@/lib/terms";
import { Checkbox, Tag, cx } from "@/components/ui";

/*
 * 目标方案（ADR 0026）：action 为 goal.plan 的待确认操作，payload 是一整套拆解。
 * 目标负责人在这里逐条勾选：勾掉的键在确认时作为 skip[] 送给后端，其余一次建好。
 * 任务按方案里的顺序列，子任务缩进在上级下面；处理过的方案只读。
 */

const PRIORITY_BY_NUMBER: Record<number, Priority> = { 0: "urgent", 1: "high", 2: "normal", 3: "low" };

export function PlanReview({ plan, executors, skipped, onToggle, readOnly }: {
  plan: GoalPlanPayload;
  executors: ExecutorRef[];
  skipped: Set<string>;
  onToggle: (key: string, skip: boolean) => void;
  readOnly: boolean;
}) {
  const tasks = plan.tasks ?? [];
  const milestones = plan.milestones ?? [];
  const byKey = Object.fromEntries(tasks.map((x) => [x.key, x]));
  const titleOf = (key: string) => byKey[key]?.title ?? key;
  const nameOf = (id?: string) => executors.find((x) => x.id === id)?.name ?? id ?? "";
  // 子任务紧跟在上级后面（保持方案顺序）
  const ordered: Array<{ task: GoalPlanTask; depth: number }> = [];
  const place = (task: GoalPlanTask, depth: number) => {
    ordered.push({ task, depth });
    for (const c of tasks) if (c.parent_key === task.key) place(c, depth + 1);
  };
  for (const x of tasks) if (!x.parent_key || !byKey[x.parent_key]) place(x, 0);
  // 方案至少要有一个任务：任务全勾掉（哪怕里程碑还在）确认就会失败
  const tasksLeft = tasks.filter((x) => !skipped.has(x.key)).length;
  const row = (key: string, depth: number, main: ReactNode, meta: ReactNode) => {
    const off = skipped.has(key);
    return (
      <li key={key} className={cx("flex items-start gap-2 py-1.5", off && "opacity-50")} style={{ paddingLeft: depth * 20 }}>
        {readOnly ? (
          <span className="mt-1 inline-block h-3.5 w-3.5 shrink-0 rounded-sm border border-hairline-strong" aria-hidden="true" />
        ) : (
          <Checkbox checked={!off} onChange={(e) => onToggle(key, !e.target.checked)} label="" className="mt-0.5 shrink-0" aria-label={key} />
        )}
        <div className="min-w-0 flex-1">
          <div className={cx("text-body text-ink", off && "line-through")}>{main}</div>
          {meta && <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-caption text-ink-subtle">{meta}</div>}
        </div>
      </li>
    );
  };
  return (
    <div>
      <p className="mb-2 text-caption text-ink-subtle">{readOnly ? t("proposals.plan.readOnly") : t("proposals.plan.hint")}</p>
      {plan.rationale && (
        <div className="mb-3">
          <h4 className="eyebrow text-ink-subtle">{t("proposals.plan.rationale")}</h4>
          <p className="mt-1 whitespace-pre-wrap text-body text-ink-muted">{plan.rationale}</p>
        </div>
      )}
      <h4 className="eyebrow text-ink-subtle">{t("proposals.plan.tasks")}</h4>
      <ul className="mt-1 divide-y divide-hairline">
        {ordered.map(({ task, depth }) =>
          row(task.key, depth, task.title, (
            <>
              {task.parent_key && byKey[task.parent_key] && <span>{t("proposals.plan.subtaskOf", { title: titleOf(task.parent_key) })}</span>}
              {task.depends_on && task.depends_on.length > 0 && <span>{t("proposals.plan.dependsOn", { titles: task.depends_on.map(titleOf).join("、") })}</span>}
              {task.type_name && task.type_name !== "generic" && <Tag>{task.type_name}</Tag>}
              {typeof task.priority === "number" && PRIORITY_BY_NUMBER[task.priority] && task.priority !== 2 && <Tag>{priorityTitle(PRIORITY_BY_NUMBER[task.priority])}</Tag>}
              {task.estimate_hours != null && <span className="telemetry">{t("proposals.plan.estimate", { n: task.estimate_hours })}</span>}
              {(task.planned_start || task.planned_end) && <span className="telemetry">{fmtDate(task.planned_start ?? null)} – {fmtDate(task.planned_end ?? null)}</span>}
              <span>{task.assignee_id ? t("proposals.plan.assignee", { name: nameOf(task.assignee_id) }) : t("proposals.plan.defaultAssignee")}</span>
              {task.description && <span className="basis-full whitespace-pre-wrap text-ink-muted">{task.description}</span>}
            </>
          )),
        )}
      </ul>
      {milestones.length > 0 && (
        <>
          <h4 className="eyebrow mt-3 text-ink-subtle">{t("proposals.plan.milestones")}</h4>
          <ul className="mt-1 divide-y divide-hairline">
            {milestones.map((m) => row(m.key, 0, m.title, (
              <>
                <span className="telemetry">{fmtDate(m.due_on)}</span>
                {m.description && <span className="basis-full whitespace-pre-wrap text-ink-muted">{m.description}</span>}
              </>
            )))}
          </ul>
        </>
      )}
      {!readOnly && skipped.size > 0 && (
        <p className={cx("mt-2 text-caption", tasksLeft === 0 ? "text-danger" : "text-ink-subtle")}>
          {tasksLeft === 0 ? t("proposals.plan.nothingLeft") : t("proposals.plan.skipped", { n: skipped.size })}
        </p>
      )}
    </div>
  );
}
