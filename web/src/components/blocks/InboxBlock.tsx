"use client";
import Link from "next/link";
import { t } from "@/lib/i18n";
import { IconInbox } from "@/components/icons";
import { InboxGroups, compactRows, liveCount, useInbox } from "@/components/inbox/InboxList";
import { RejectDialog } from "@/components/proposals/RejectDialog";
import { Odometer } from "@/components/ship-status/Odometer";
import { Button, cx } from "@/components/ui";
import { BlockPanel, CompactList, type BlockProps } from "./BlockPanel";

/**
 * 待我处理区块（DESIGN.md §12，ADR 0015 补记四）：GET /inbox 的六组事项，只看本人、不受范围影响，每条右侧就是它的动作。
 * 头部：标题 + 条数（滚动数字）；右侧「查看待确认操作的历史」。各组在区块内滚动。
 * 尺寸适配：compact（≤ 4 栏）只给条数与前三条；dense（1 行高）只剩头部与每组的条数小芯片。编辑模式下本体不响应（外壳统一处理）。
 */
export function InboxBlock({ index, title, noLink, compact, dense }: BlockProps) {
  const ib = useInbox();
  const { data } = ib;
  const heading = (
    <span className="inline-flex items-center gap-2">
      {title ?? t("block.inbox")}
      {data && <Odometer text={String(ib.total)} className={cx("inbox-count", ib.total > 0 && "inbox-count-warm")} />}
    </span>
  );
  return (
    <>
      <BlockPanel
        index={index}
        noLink={noLink}
        compact={compact}
        dense={dense}
        icon={<IconInbox />}
        title={heading}
        href="/proposals/"
        hrefLabel={t("inbox.history")}
        padded={false}
        loading={ib.inbox.loading && !data}
        error={data ? null : ib.inbox.error}
        onRetry={ib.inbox.reload}
        empty={ib.allEmpty}
        emptyText={data?.empty || t("inbox.emptyHint")}
        emptyAction={<Link href="/tasks/?view=backlog" className="inline-flex"><Button size="sm" tabIndex={-1}>{t("inbox.emptyGo")}</Button></Link>}
      >
        {dense ? (
          <div className="flex h-full flex-wrap content-center items-center gap-2 px-4 py-2">
            {ib.visible.map((g) => {
              const n = liveCount(ib, g);
              if (!n) return null;
              return (
                <span key={g.kind} className="inline-flex h-6 items-center gap-1.5 rounded-sm border border-hairline bg-surface-2 px-2 text-caption text-ink-muted" title={g.title || t(`inbox.group.${g.kind}`)}>
                  {t(`inbox.short.${g.kind}`)}
                  <span className={cx("font-mono tabular-nums", g.kind === "overdue" ? "text-danger" : "text-ink")}>{n}</span>
                </span>
              );
            })}
          </div>
        ) : compact ? (
          <CompactList count={ib.total} label={t("block.inbox.compact")} rows={compactRows(ib)} />
        ) : (
          <InboxGroups ib={ib} />
        )}
      </BlockPanel>
      <RejectDialog proposal={ib.rejecting} busy={!!ib.busy} onClose={() => ib.setRejecting(null)} onSubmit={ib.reject} />
    </>
  );
}
