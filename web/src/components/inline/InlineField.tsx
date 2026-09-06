"use client";
import type { ReactNode } from "react";
import { t } from "@/lib/i18n";
import { IconCheck } from "@/components/icons";
import { cx } from "@/components/ui";
import type { InlineStatus } from "./useInlineSave";

/*
 * 基本信息表的一行（DESIGN.md §15）：标签 · 值（值本身就是控件）· 状态位。
 * 状态位固定 12px：保存中是细环，成功是对勾闪现（--motion-fast），失败时行下一句原因。
 * 只读的行也用同一个组件（status 不传），保证两种行的列对齐与行高一致。
 */
export function InlineTable({ children, className }: { children: ReactNode; className?: string }) {
  return <dl className={cx("inl-table grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-body", className)}>{children}</dl>;
}

export function InlineStatusMark({ status }: { status?: InlineStatus }) {
  return (
    <span className="inl-status" aria-live="polite">
      {status === "saving" && <span className="inl-spin" role="img" aria-label={t("inline.saving")} />}
      {status === "saved" && <IconCheck size={12} className="inl-check" aria-label={t("inline.saved")} />}
    </span>
  );
}

export function InlineField({ label, status, error, children, className }: { label: ReactNode; status?: InlineStatus; error?: string | null; children: ReactNode; className?: string }) {
  return (
    <div className={cx("inl-row contents", className)} data-saving={status === "saving" ? "" : undefined}>
      <dt className="whitespace-nowrap leading-5 text-ink-subtle">{label}</dt>
      <dd className="min-w-0">
        <div className="flex min-h-5 items-center gap-2">
          <div className="min-w-0 flex-1 leading-5">{children}</div>
          <InlineStatusMark status={status} />
        </div>
        {status === "error" && error && <p className="mt-1 text-caption text-danger" role="alert">{error}</p>}
      </dd>
    </div>
  );
}
