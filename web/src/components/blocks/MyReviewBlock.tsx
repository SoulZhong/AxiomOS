"use client";
import { api } from "@/lib/api";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconAccept } from "@/components/icons";
import { TaskTable } from "@/components/TaskTable";
import { StateBadge } from "@/components/StateBadge";
import { TableSkeleton, TaskLink } from "@/components/ui";
import { BlockPanel, CompactList, type BlockProps } from "./BlockPanel";

/** 等我验收：我是验收人、正在等待中（待验收 / 等待答复）的任务。 */
export function MyReviewBlock({ index, title, noLink, compact, dense }: BlockProps) {
  const { session } = useSession();
  const currency = session?.organization.currency;
  const review = useLoad(() => api.tasks.list({ reviewer: "me", state: "waiting" }), []);
  const rows = review.data ?? [];
  return (
    <BlockPanel
      id="review"
      index={index}
      noLink={noLink}
      compact={compact}
      icon={<IconAccept />}
      title={title ?? t("block.my_review")}
      telemetry={review.data ? t("panel.rows", { n: rows.length }) : undefined}
      href="/tasks/?reviewer=me&state=waiting"
      padded={false}
      loading={review.loading && !review.data}
      error={review.error}
      onRetry={review.reload}
      empty={!!review.data && rows.length === 0}
      emptyText={t("home.noReview")}
      skeleton={<TableSkeleton rows={2} cols={5} />}
    >
      {compact || dense ? (
        <CompactList dense={dense} count={rows.length} label={t("block.my_review.compact")} rows={rows.map((x) => ({ key: x.id, title: <TaskLink id={x.id} title={x.title} className="min-w-0 truncate" />, meta: <StateBadge state={x.state} showLabel={false} /> }))} />
      ) : (
        <div className="overflow-x-auto"><TaskTable tasks={rows} currency={currency} showGoal={false} compact onChanged={review.reload} /></div>
      )}
    </BlockPanel>
  );
}
