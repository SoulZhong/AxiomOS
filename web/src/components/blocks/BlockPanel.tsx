"use client";
import Link from "next/link";
import type { ReactNode } from "react";
import { t } from "@/lib/i18n";
import { IconChevronRight } from "@/components/icons";
import { ErrorBox, ListSkeleton, Panel } from "@/components/ui";

/*
 * 区块的统一外壳（ADR 0015）：每个区块都是一块 Panel——
 * 左：序号 + 签名图标 + 标题；右：遥测读数（数量 / 时间段）+ 去完整页面的链接。
 * 加载中给骨架、出错给重试、没有内容给一句安静的空状态；数据本身由各区块自己取，并服从当前范围（useLoad 已随范围重取）。
 */
export interface BlockProps {
  /** 在首页里的序号（01、02……），由工作台按顺序给 */
  index: number;
  /** 后端按语言给的区块名；没有时用字典里的 */
  title?: string;
  /** 已经在完整页面上（组织概览页）时不再给"去完整页面"的链接 */
  noLink?: boolean;
}

export function BlockPanel({
  index,
  title,
  icon,
  telemetry,
  href,
  hrefLabel,
  noLink,
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
  if (noLink) href = undefined;
  let body: ReactNode;
  if (loading) body = skeleton ?? <ListSkeleton rows={3} />;
  else if (error) body = <ErrorBox message={error} onRetry={onRetry} />;
  else if (empty)
    body = (
      <div className="flex flex-col items-center justify-center gap-2 px-4 py-6 text-center">
        <p className="text-body text-ink-subtle">{emptyText ?? t("common.empty")}</p>
        {emptyAction && <div>{emptyAction}</div>}
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
      telemetry={telemetry}
      padded={padded && !bare}
      className={className}
      actions={
        actions || href ? (
          <>
            {actions}
            {href && (
              <Link href={href} className="inline-flex items-center gap-0.5 text-caption text-ink-muted hover:text-accent-hover">
                {hrefLabel ?? t("block.more")}
                <IconChevronRight />
              </Link>
            )}
          </>
        ) : undefined
      }
    >
      {needPad ? <div className="p-4">{body}</div> : body}
    </Panel>
  );
}
