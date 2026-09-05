"use client";
import Link from "next/link";
import { api } from "@/lib/api";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconApprove } from "@/components/icons";
import { Avatar, ListSkeleton, RelativeTime, Tag } from "@/components/ui";
import { BlockPanel, type BlockProps } from "./BlockPanel";

const LIMIT = 6;

/** 待确认操作：等我确认的那些（mine=1，pending）。每行 = 发起的 Agent、要做什么、后端那句"确认后会发生什么"、发起时间；点开去确认。不按范围筛（它是个人队列）。 */
export function ProposalsBlock({ index, title, noLink }: BlockProps) {
  const list = useLoad(() => api.proposals.list({ mine: 1, status: "pending" }), []);
  const rows = list.data ?? [];
  return (
    <BlockPanel
      index={index}
      noLink={noLink}
      icon={<IconApprove />}
      title={title ?? t("block.proposals")}
      telemetry={list.data ? t("block.proposals.pending", { n: rows.length }) : undefined}
      href="/proposals/"
      padded={false}
      loading={list.loading && !list.data}
      error={list.error}
      onRetry={list.reload}
      empty={!!list.data && rows.length === 0}
      emptyText={t("block.proposals.empty")}
      skeleton={<ListSkeleton rows={3} />}
    >
      <ul className="divide-y divide-hairline">
        {rows.slice(0, LIMIT).map((p) => (
          <li key={p.id}>
            <Link href={`/proposals/?id=${encodeURIComponent(p.id)}`} className="flex items-start gap-3 px-4 py-2.5 hover:bg-surface-2">
              <Avatar name={p.agent.name} kind="agent" className="mt-0.5 shrink-0" />
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-2">
                  <span className="truncate text-body text-ink">{p.agent.name}</span>
                  <Tag tone="accent">{p.action_title}</Tag>
                  <RelativeTime iso={p.created_at} className="ml-auto shrink-0 text-caption text-ink-subtle" />
                </span>
                <span className="mt-0.5 line-clamp-2 block text-caption text-ink-muted">{p.summary}</span>
              </span>
            </Link>
          </li>
        ))}
        {rows.length > LIMIT && (
          <li className="px-4 py-2 text-caption text-ink-subtle">
            <Link href="/proposals/" className="hover:text-accent-hover">
              {t("block.moreN", { n: rows.length })}
            </Link>
          </li>
        )}
      </ul>
    </BlockPanel>
  );
}
