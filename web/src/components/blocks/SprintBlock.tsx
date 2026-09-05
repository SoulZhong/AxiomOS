"use client";
import Link from "next/link";
import { api } from "@/lib/api";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconSprint } from "@/components/icons";
import { Burndown } from "@/components/sprints/Burndown";
import { fmtRange, sprintDays, sprintProgress } from "@/components/sprints/sprintUtils";
import { ListSkeleton, ProgressBar, Readout, Skeleton, cx } from "@/components/ui";
import { BlockPanel, type BlockProps } from "./BlockPanel";

/*
 * 当前迭代：当前范围里进行中的迭代（取第一个），给名称、迭代目标、起止与剩余天数、工作量完成进度和燃尽图。
 * 没有进行中的迭代时是一句安静的空状态，不放装饰。
 */
export function SprintBlock({ index, title, noLink }: BlockProps) {
  const list = useLoad(() => api.sprints.list({ status: "active" }), []);
  const current = list.data?.[0] ?? null;
  const detail = useLoad(() => (current ? api.sprints.get(current.id) : Promise.resolve(null)), [current?.id ?? null]);
  const sp = detail.data ?? current;
  const days = sp ? sprintDays(sp) : null;
  const pct = sp ? sprintProgress(sp) : 0;
  return (
    <BlockPanel
      index={index}
      noLink={noLink}
      icon={<IconSprint />}
      title={title ?? t("block.sprint")}
      telemetry={days ? <span className={cx(days.over && "text-danger")}>{days.text}</span> : undefined}
      href={sp ? `/sprints/${encodeURIComponent(sp.id)}/` : "/sprints/"}
      hrefLabel={sp ? t("block.sprint.open") : t("block.sprint.all")}
      loading={list.loading && !list.data}
      error={list.error}
      onRetry={list.reload}
      empty={!!list.data && !current}
      emptyText={t("block.sprint.empty")}
      emptyAction={
        <Link href="/sprints/" className="text-caption text-ink-muted hover:text-accent-hover">
          {t("block.sprint.all")}
        </Link>
      }
      skeleton={
        <div>
          <Skeleton className="mb-2 h-4 w-40" />
          <Skeleton className="mb-4 h-3 w-64" />
          <ListSkeleton rows={3} />
        </div>
      }
    >
      {sp && (
        <div>
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <div className="min-w-0">
              <Link href={`/sprints/${encodeURIComponent(sp.id)}/`} className="text-title text-ink hover:text-accent-hover">
                {sp.name}
              </Link>
              {sp.goal && <p className="mt-0.5 truncate text-caption text-ink-muted">{sp.goal}</p>}
            </div>
            <span className="telemetry text-caption text-ink-subtle">
              {fmtRange(sp.starts_on, sp.ends_on)}
              {sp.team ? ` · ${sp.team.title}` : ""}
            </span>
          </div>
          <div className="mt-3 grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3">
            <ProgressBar value={pct} tone={pct >= 100 ? "success" : "accent"} className="w-full" />
            <span className="text-caption tabular-nums text-ink-muted">
              <Readout value={`${sp.points_done} / ${sp.points_total}`} /> · {pct}% · {t("block.sprint.tasks", { n: sp.task_count })}
            </span>
          </div>
          <div className="mt-3">
            {detail.loading && !detail.data ? (
              <Skeleton className="h-[160px] w-full" />
            ) : detail.data && detail.data.burndown.actual.length > 0 ? (
              <Burndown data={detail.data.burndown} height={160} />
            ) : (
              <p className="py-4 text-center text-caption text-ink-subtle">{t("sprint.burndown.empty")}</p>
            )}
          </div>
        </div>
      )}
    </BlockPanel>
  );
}
