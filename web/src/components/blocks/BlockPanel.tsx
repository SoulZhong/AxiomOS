"use client";
import Link from "next/link";
import { createContext, useContext, type ReactNode } from "react";
import type { BlockHeight, BlockWidth } from "@/lib/api";
import { t } from "@/lib/i18n";
import { IconChevronRight } from "@/components/icons";
import { ErrorBox, ListSkeleton, Panel, cx } from "@/components/ui";

/*
 * 区块的统一外壳（ADR 0015 / 补记二）：每个区块都是一块 Panel——
 * 左：序号 + 签名图标 + 标题；右：遥测读数（数量 / 时间段）+ 去完整页面的链接。
 * 加载中给骨架、出错给重试、没有内容给一句安静的空状态；数据本身由各区块自己取，并服从当前范围（useLoad 已随范围重取）。
 *
 * 放进 12 栏网格（WorkspaceGrid）时外面有 BlockFrame：区块撑满格子的固定高度，头部固定、内容在区块内部滚动；
 * 编辑模式下头部右侧只有一枚 ×（移除），区块本体不响应点击（按住任意位置就是拖动），也不再给"查看全部"链接。
 * 尺寸适配是每个区块自己的责任（DESIGN.md §13）：compact（≤ 4 栏）时表格类只显示计数与前三条，数字类只显示大数字；
 * dense（只有 1 行高，120px）时只剩头部读数 + 一枚关键数字 / 计数，内边距也收紧。
 */
export interface BlockProps {
  /** 在首页里的序号（01、02……），由工作台按顺序给 */
  index: number;
  /** 后端按语言给的区块名；没有时用字典里的 */
  title?: string;
  /** 已经在完整页面上（组织概览页）时不再给"去完整页面"的链接 */
  noLink?: boolean;
  /** 在网格里占几栏 / 哪一档高度；不在网格里时不给 */
  w?: BlockWidth;
  h?: BlockHeight;
  /** ≤ 4 栏：只显示关键数字与标题（表格类显示计数与前三条） */
  compact?: boolean;
  /** 只有 1 行高：头部 + 关键数字 / 计数，不放清单 */
  dense?: boolean;
}

/** 网格格子给区块外壳的信息：是否在编辑、编辑时头部右侧的 ×。不在网格里时为 null。 */
export interface BlockFrame {
  editing: boolean;
  remove?: ReactNode;
}
export const BlockFrameContext = createContext<BlockFrame | null>(null);
export const useBlockFrame = () => useContext(BlockFrameContext);

export function BlockPanel({
  index,
  title,
  icon,
  telemetry,
  href,
  hrefLabel,
  noLink,
  compact,
  dense,
  actions,
  loading,
  error,
  onRetry,
  empty,
  emptyText,
  emptyAction,
  skeleton,
  padded = true,
  className,
  id,
  children,
}: {
  index: number;
  title: ReactNode;
  icon?: ReactNode;
  telemetry?: ReactNode;
  href?: string;
  hrefLabel?: string;
  noLink?: boolean;
  /** 窄区块：头部不放遥测读数，标题留足位置 */
  compact?: boolean;
  /** 1 行高：内容区内边距收紧（p-4 → px-4 py-2） */
  dense?: boolean;
  actions?: ReactNode;
  loading?: boolean;
  error?: string | null;
  onRetry?: () => void;
  empty?: boolean;
  emptyText?: ReactNode;
  emptyAction?: ReactNode;
  skeleton?: ReactNode;
  padded?: boolean;
  className?: string;
  id?: string;
  children: ReactNode;
}) {
  const frame = useBlockFrame();
  const framed = frame !== null;
  if (noLink || frame?.editing) href = undefined;
  let body: ReactNode;
  if (loading) body = skeleton ?? <ListSkeleton rows={3} />;
  else if (error) body = <ErrorBox message={error} onRetry={onRetry} />;
  else if (empty)
    // 1 行高时空状态只留那一句（去掉动作链接与大内边距），保证 120px 里不用滚
    body = (
      <div className={cx("flex flex-col items-center justify-center gap-2 px-4 text-center", dense ? "py-2" : "py-6")}>
        <p className="text-body text-ink-subtle">{emptyText ?? t("common.empty")}</p>
        {emptyAction && !dense && <div>{emptyAction}</div>}
      </div>
    );
  else body = children;
  // 骨架 / 报错需要内边距；空状态自带；表格类内容自己贴边
  const needPad = loading || !!error;
  const bare = needPad || !!empty;
  return (
    <Panel
      id={id}
      index={index}
      icon={icon}
      title={title}
      telemetry={compact ? undefined : telemetry}
      padded={padded && !bare}
      className={cx(framed && "flex h-full flex-col", frame?.editing && "border-hairline-strong", className)}
      bodyClassName={cx(framed && "ws-body min-h-0 flex-1 overflow-auto", framed && !!empty && "flex flex-col justify-center", dense && padded && !bare && "!p-0")}
      actions={
        (actions && !frame?.editing) || href || frame?.remove ? (
          <>
            {!frame?.editing && actions}
            {frame?.editing && frame.remove}
            {href && (
              <Link href={href} className="inline-flex items-center gap-0.5 text-caption whitespace-nowrap text-ink-muted hover:text-accent-hover">
                {compact ? t("block.more") : (hrefLabel ?? t("block.more"))}
                <IconChevronRight />
              </Link>
            )}
          </>
        ) : undefined
      }
    >
      {needPad ? <div className="p-4">{body}</div> : dense && padded && !bare ? <div className="px-4 py-2">{body}</div> : body}
    </Panel>
  );
}

/** 窄区块（≤ 4 栏）的数字类内容：一枚大数字 + 眉标 + 一行说明（DESIGN.md「小指标面板」，数字 24px/500）。 */
export function BigFigure({ label, value, sub, tone, dense, className }: { label: ReactNode; value: ReactNode; sub?: ReactNode; tone?: "danger" | "warning"; /** 1 行高：去掉说明行、收紧内边距 */ dense?: boolean; className?: string }) {
  return (
    <div className={cx("flex h-full min-h-0 flex-col justify-center px-4", dense ? "py-2" : "py-3", className)}>
      <div className="eyebrow mb-1 truncate text-ink-subtle">{label}</div>
      <div className={cx("text-[24px] leading-tight font-medium tabular-nums", tone === "danger" ? "text-danger" : tone === "warning" ? "text-warning" : "text-ink")}>{value}</div>
      {sub && !dense && <div className="mt-1 truncate text-caption text-ink-subtle">{sub}</div>}
    </div>
  );
}

export interface CompactRow {
  key: string;
  title: ReactNode;
  /** 右侧一小段：状态点、时间、动作 */
  meta?: ReactNode;
}

/** 窄区块（≤ 4 栏）的表格 / 清单类内容：计数 + 前三条；dense（1 行高）时只有计数。 */
export function CompactList({ count, label, rows, limit = 3, dense }: { count: number; label: ReactNode; rows: CompactRow[]; limit?: number; dense?: boolean }) {
  if (dense) limit = 0;
  return (
    <div>
      <div className={cx("flex items-baseline gap-2 px-4", dense ? "py-2" : "pt-3 pb-2")}>
        <span className="text-[24px] leading-none font-medium tabular-nums text-ink">{count}</span>
        <span className="truncate text-caption text-ink-subtle">{label}</span>
      </div>
      {rows.length > 0 && (
        <ul className="divide-y divide-hairline border-t border-hairline">
          {rows.slice(0, limit).map((r) => (
            <li key={r.key} className="flex min-w-0 items-center gap-2 px-4 py-1.5 text-body">
              <span className="min-w-0 flex-1 truncate">{r.title}</span>
              {r.meta && <span className="inline-flex shrink-0 items-center gap-1.5 text-caption text-ink-subtle">{r.meta}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
