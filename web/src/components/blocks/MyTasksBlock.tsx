"use client";
import { useMemo } from "react";
import { api, isTerminal } from "@/lib/api";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconTask } from "@/components/icons";
import { isOverdue, TaskTable } from "@/components/TaskTable";
import { StateBadge } from "@/components/StateBadge";
import { TableSkeleton, TaskLink, cx } from "@/components/ui";
import { BlockPanel, CompactList, type BlockProps } from "./BlockPanel";

const LIMIT = 10;

/** 我的任务：我（含我的 Agent）名下未结束的任务，逾期的排前面；右上角读数是进行中 / 逾期 / 待验收的数量。 */
export function MyTasksBlock({ index, title, noLink, compact, dense }: BlockProps) {
  const { session } = useSession();
  const currency = session?.organization.currency;
  const mine = useLoad(() => api.tasks.list({ assignee: "me" }), []);
  const open = useMemo(() => {
    const xs = mine.data?.filter((x) => !isTerminal(x.state.label)) ?? [];
    // 逾期的先看到，其余保持后端顺序
    return [...xs.filter(isOverdue), ...xs.filter((x) => !isOverdue(x))];
  }, [mine.data]);
  const active = open.filter((x) => x.state.label === "active").length;
  const overdue = open.filter(isOverdue).length;
  const waiting = open.filter((x) => x.state.label === "waiting").length;
  return (
    <BlockPanel
      index={index}
      noLink={noLink}
      compact={compact}
      icon={<IconTask />}
      title={title ?? t("block.my_tasks")}
      telemetry={
        mine.data ? (
          <span className="inline-flex items-center gap-2">
            <span>{t("block.my_tasks.active", { n: active })}</span>
            <span className={cx(overdue > 0 && "text-danger")}>{t("block.my_tasks.overdue", { n: overdue })}</span>
            <span className={cx(waiting > 0 && "text-warning")}>{t("block.my_tasks.waiting", { n: waiting })}</span>
          </span>
        ) : undefined
      }
      href="/tasks/?assignee=me"
      hrefLabel={open.length > LIMIT ? t("block.moreN", { n: open.length }) : t("block.more")}
      padded={false}
      loading={mine.loading && !mine.data}
      error={mine.error}
      onRetry={mine.reload}
      empty={!!mine.data && open.length === 0}
      emptyText={t("home.noMine")}
      skeleton={<TableSkeleton rows={4} cols={5} />}
    >
      {compact || dense ? (
        <CompactList
          dense={dense}
          count={open.length}
          label={<span className={cx(overdue > 0 && "text-danger")}>{t("block.my_tasks.compact", { n: overdue })}</span>}
          rows={open.map((x) => ({ key: x.id, title: <TaskLink id={x.id} title={x.title} className="min-w-0 truncate" />, meta: <StateBadge state={x.state} showLabel={false} /> }))}
        />
      ) : (
        <div className="overflow-x-auto"><TaskTable tasks={open.slice(0, LIMIT)} currency={currency} showGoal={false} compact onChanged={mine.reload} /></div>
      )}
    </BlockPanel>
  );
}
