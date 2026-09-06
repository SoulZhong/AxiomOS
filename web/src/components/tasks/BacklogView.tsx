"use client";
import Link from "next/link";
import { useState } from "react";
import { api } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import { useAction, useCapabilityTitles, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { prefersReducedMotion, rememberClaim } from "@/lib/motion";
import { capabilityTitle, roleTitle } from "@/lib/terms";
import { useSession } from "@/components/AppShell";
import { IconBacklog, IconHand, IconPlus } from "@/components/icons";
import { StateBadge } from "@/components/StateBadge";
import { TypeLabel } from "@/components/TaskTable";
import { Button, Empty, ErrorBox, Panel, StatChips, Table, TableSkeleton, Tag, TagList, TaskLink, Tip } from "@/components/ui";
import { hasFilters, matchTask, type MatchCtx, type TaskFilters } from "./filters";

/**
 * 「任务 · 待领取」：所有还没有负责人的任务，符合条件的执行者可以自己领（GET /backlog 没有筛选参数，
 * goal / type / sprint / state 在前端过滤；负责人与团队对没有负责人的任务没有意义）。
 */
export function BacklogView({ filters, ctx, reloadKey, onOpen, onNew }: { filters: TaskFilters; ctx: MatchCtx; reloadKey: number; onOpen: (id: string) => void; onNew: () => void }) {
  const { session } = useSession();
  const items = useLoad(() => api.backlog.list(), [reloadKey]);
  const caps = useCapabilityTitles();
  const { busy, run } = useAction();
  const roles = session ? Object.fromEntries(session.roles.map((r) => [r.name, r.title])) : undefined;
  const shown = (items.data ?? []).filter((b) => matchTask({ id: b.task.id, number: b.task.number, title: b.task.title, goalId: b.task.goal_id, assigneeId: null, type: b.task.type, label: b.task.state.label, sprintId: b.task.sprint?.id ?? null, priority: b.task.priority, plannedEnd: b.task.planned_end }, filters, ctx, ["assignee", "team"]));
  const claimable = shown.filter((b) => b.can_claim).length;

  // 领取成功：这一行 240ms 向右淡出（取走一块）后再重新加载；同时记住 id，让它在任务表里从左滑入
  const [leaving, setLeaving] = useState<string | null>(null);
  const claim = async (id: string) => {
    if (await run(id, () => api.tasks.claim(id), t("toast.claimed"))) {
      rememberClaim(id);
      if (prefersReducedMotion()) return items.reload();
      setLeaving(id);
      window.setTimeout(() => {
        items.reload();
        setLeaving(null);
      }, 260);
    }
  };

  return (
    <div>
      <StatChips className="mb-4" value={null} total={shown.length} items={[{ key: "claimable", label: t("home.claimableShort"), value: claimable, tone: "accent" }]} />
      <Panel index={1} icon={<IconBacklog />} title={t("backlog.title")} telemetry={items.data ? t("panel.rows", { n: shown.length }) : undefined} padded={false}>
        {items.loading && !items.data ? <TableSkeleton rows={4} cols={7} /> : items.error ? <div className="p-4"><ErrorBox message={items.error} onRetry={items.reload} /></div> : shown.length === 0 ? <Empty text={hasFilters(filters) ? t("tasks.noMatch") : t("backlog.emptyHint")} action={hasFilters(filters) ? undefined : <Button variant="primary" icon={<IconPlus />} onClick={onNew}>{t("tasks.new")}</Button>} /> : (
          <Table>
            <thead>
              <tr>
                <th className="w-full">{t("taskTable.task")}</th><th className="w-[80px]">{t("taskTable.type")}</th><th className="w-[124px]">{t("taskTable.state")}</th><th className="w-[88px]">{t("backlog.requiredRole")}</th><th className="w-[150px]">{t("backlog.caps")}</th><th className="w-[140px]">{t("taskTable.goal")}</th><th className="w-[150px]">{t("taskTable.plan")}</th><th className="actions w-[96px]" aria-label={t("common.actions")} />
              </tr>
            </thead>
            <tbody>
              {shown.map(({ task: x, required_role, required_capabilities, can_claim, reasons }) => (
                <tr key={x.id} data-motion={leaving === x.id ? "pile" : undefined} data-task-row={x.id} className="cursor-pointer" onClick={(e) => { if ((e.target as HTMLElement).closest("a,button")) return; onOpen(x.id); }}>
                  <td className="w-full max-w-0 min-w-[180px]"><TaskLink id={x.id} number={x.number} title={x.title} /></td>
                  <td className="whitespace-nowrap"><TypeLabel type={x.type} title={x.type_title} /></td>
                  <td className="whitespace-nowrap"><StateBadge state={x.state} /></td>
                  <td className="whitespace-nowrap">{required_role ? <Tag tone="warning">{roleTitle(required_role, roles)}</Tag> : <span className="text-ink-subtle">{t("backlog.anyone")}</span>}</td>
                  <td className="whitespace-nowrap"><TagList items={required_capabilities.map((c) => capabilityTitle(c, caps))} /></td>
                  <td className="max-w-[160px]">{x.goal ? <Link href={`/goals/${encodeURIComponent(x.goal.id)}/`} className="block truncate text-ink-muted hover:text-accent-hover" title={x.goal.title}>{x.goal.title}</Link> : <span className="text-ink-subtle">—</span>}</td>
                  <td className="whitespace-nowrap font-mono text-mono text-ink-muted">{fmtDate(x.planned_start)} – {fmtDate(x.planned_end)}</td>
                  <td className="actions">
                    <Tip tip={can_claim ? null : reasons.join("\n")}>
                      <Button size="sm" variant={can_claim ? "default" : "ghost"} icon={<IconHand />} disabled={!can_claim || busy === x.id} onClick={() => void claim(x.id)}>{t("backlog.claim")}</Button>
                    </Tip>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>
    </div>
  );
}
