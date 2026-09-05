"use client";
import Link from "next/link";
import { api, type BacklogItem } from "@/lib/api";
import { useAction, useCapabilityTitles, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { capabilityTitle } from "@/lib/terms";
import { IconBacklog, IconHand } from "@/components/icons";
import { StateBadge } from "@/components/StateBadge";
import { Button, ListSkeleton, Tag, TaskLink } from "@/components/ui";
import { BlockPanel, type BlockProps } from "./BlockPanel";

const LIMIT = 8;

/** 待领取任务：当前范围里我能领的（can_claim）。行内直接「领取」，领完从这里消失、出现在「我的任务」。 */
export function BacklogBlock({ index, title, noLink }: BlockProps) {
  const backlog = useLoad(() => api.backlog.list(), []);
  const caps = useCapabilityTitles();
  const { busy, run } = useAction();
  const claimable: BacklogItem[] = backlog.data?.filter((b) => b.can_claim) ?? [];
  const total = backlog.data?.length ?? 0;
  const claim = (b: BacklogItem) => run(b.task.id, () => api.tasks.claim(b.task.id), t("toast.claimed")).then((ok) => ok && backlog.reload());
  return (
    <BlockPanel
      index={index}
      noLink={noLink}
      icon={<IconBacklog />}
      title={title ?? t("block.backlog")}
      telemetry={backlog.data ? t("block.backlog.count", { n: claimable.length, total }) : undefined}
      href="/backlog/"
      padded={false}
      loading={backlog.loading && !backlog.data}
      error={backlog.error}
      onRetry={backlog.reload}
      empty={!!backlog.data && claimable.length === 0}
      emptyText={total > 0 ? t("block.backlog.noneForMe", { n: total }) : t("block.backlog.empty")}
      emptyAction={
        total > 0 ? (
          <Link href="/backlog/" className="text-caption text-ink-muted hover:text-accent-hover">
            {t("block.more")}
          </Link>
        ) : undefined
      }
      skeleton={<ListSkeleton rows={4} />}
    >
      <ul className="divide-y divide-hairline">
        {claimable.slice(0, LIMIT).map((b) => (
          <li key={b.task.id} className="flex items-center gap-3 px-4 py-2">
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-2">
                <TaskLink id={b.task.id} title={b.task.title} className="min-w-0 truncate" />
                <StateBadge state={b.task.state} showLabel={false} />
              </span>
              <span className="mt-0.5 flex flex-wrap items-center gap-1.5 text-caption text-ink-subtle">
                <span>{b.task.type_title}</span>
                {b.task.goal && (
                  <>
                    <span className="text-ink-tertiary">·</span>
                    <span className="truncate">{b.task.goal.title}</span>
                  </>
                )}
                {b.required_capabilities.map((c) => (
                  <Tag key={c}>{capabilityTitle(c, caps)}</Tag>
                ))}
              </span>
            </span>
            <Button size="sm" icon={<IconHand />} disabled={busy === b.task.id} onClick={() => void claim(b)}>
              {t("backlog.claim")}
            </Button>
          </li>
        ))}
        {claimable.length > LIMIT && (
          <li className="px-4 py-2 text-caption text-ink-subtle">
            <Link href="/backlog/" className="hover:text-accent-hover">
              {t("block.moreN", { n: claimable.length })}
            </Link>
          </li>
        )}
      </ul>
    </BlockPanel>
  );
}
