"use client";
import { t } from "@/lib/i18n";
import { cx } from "@/components/ui";

/** 开关值控件（仅限人工，DESIGN.md §15）：小轨道 + 是/否；不能改的人只看到「是 / 否」。 */
export function InlineToggle({ checked, onChange, editable, ariaLabel, className, hint }: { checked: boolean; onChange: (next: boolean) => void; editable: boolean; ariaLabel?: string; className?: string; hint?: string }) {
  const text = checked ? t("common.yes") : t("common.no");
  if (!editable) return <span className={cx("inl-static", className)}>{text}</span>;
  return (
    <button type="button" role="switch" aria-checked={checked} aria-label={ariaLabel} title={hint} className={cx("inl", className)} onClick={() => onChange(!checked)}>
      <span className="inl-switch" aria-hidden="true" />
      <span className="inl-text">{text}</span>
    </button>
  );
}
