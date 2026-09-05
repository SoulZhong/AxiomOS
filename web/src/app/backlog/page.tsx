"use client";
import Link from "next/link";
import { api } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import { useState } from "react";
import { useAction, useCapabilityTitles, useLoad } from "@/lib/hooks";
import { prefersReducedMotion, rememberClaim } from "@/lib/motion";
import { t } from "@/lib/i18n";
import { capabilityTitle, roleTitle } from "@/lib/terms";
import { useSession } from "@/components/AppShell";
import { IconBacklog, IconHand } from "@/components/icons";
import { StateBadge } from "@/components/StateBadge";
import { TypeLabel } from "@/components/TaskTable";
import { Button, Empty, ErrorBox, PageHeader, Panel, StatChips, Table, TableSkeleton, Tag, TagList, TaskLink, Tip } from "@/components/ui";

export default function BacklogPage() {
  const { session } = useSession();
  const items = useLoad(() => api.backlog.list(), []);
  const caps = useCapabilityTitles();
  const { busy, run } = useAction();
  const roles = session ? Object.fromEntries(session.roles.map((r) => [r.name, r.title])) : undefined;
  const claimable = items.data?.filter((b) => b.can_claim).length ?? 0;

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
      <PageHeader title={t("backlog.title")} description={t("backlog.description")} />
      <StatChips
        className="mb-4"
        value={null}
        total={items.data?.length ?? 0}
        items={[{ key: "claimable", label: t("home.claimableShort"), value: claimable, tone: "accent" }]}
      />
      <Panel index={1} icon={<IconBacklog />} title={t("nav.backlog")} telemetry={items.data ? t("panel.rows", { n: items.data.length }) : undefined} padded={false}>
        {items.loading ? <TableSkeleton rows={4} cols={7} /> : items.error ? <div className="p-4"><ErrorBox message={items.error} onRetry={items.reload} /></div> : !items.data?.length ? <Empty text={t("backlog.empty")} /> : (
          <Table>
            <thead>
              <tr>
                <th className="w-full">{t("taskTable.task")}</th><th className="w-[80px]">{t("taskTable.type")}</th><th className="w-[124px]">{t("taskTable.state")}</th><th className="w-[88px]">{t("backlog.requiredRole")}</th><th className="w-[150px]">{t("backlog.caps")}</th><th className="w-[140px]">{t("taskTable.goal")}</th><th className="w-[150px]">{t("taskTable.plan")}</th><th className="actions w-[96px]" aria-label={t("common.actions")} />
              </tr>
            </thead>
            <tbody>
              {items.data.map(({ task: x, required_role, required_capabilities, can_claim, reasons }) => (
                <tr key={x.id} data-motion={leaving === x.id ? "pile" : undefined}>
                  <td className="w-full max-w-0 min-w-[180px]"><TaskLink id={x.id} title={x.title} /></td>
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
