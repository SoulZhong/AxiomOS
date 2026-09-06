"use client";
import { useRef, useState } from "react";
import { fmtDate } from "@/lib/format";
import { t } from "@/lib/i18n";
import { IconChevronDown, IconClose } from "@/components/icons";
import { DateInput, cx } from "@/components/ui";

/*
 * 日期值控件（DESIGN.md §15）：静止显示与页面其他地方一致的日期写法（9月8日），点开是浏览器的原生日期选择器
 * （showPicker；不支持的浏览器退回成同一行里的原生日期输入框）。可清空的字段在悬停时出现 ×。
 */
export function InlineDate({ value, onChange, editable, nullable = true, withYear = false, placeholder, ariaLabel, className }: {
  value: string | null;
  onChange: (next: string | null) => void;
  editable: boolean;
  nullable?: boolean;
  withYear?: boolean;
  placeholder?: string;
  ariaLabel?: string;
  className?: string;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [native, setNative] = useState(false);
  const text = value ? fmtDate(value, withYear) : null;
  if (!editable) return <span className={cx("inl-static", className)}>{text ?? <span className="text-ink-subtle">{placeholder ?? "—"}</span>}</span>;

  const open = () => {
    const el = input.current;
    if (!el) return;
    try {
      if (typeof el.showPicker === "function") { el.showPicker(); return; }
    } catch {
      // 某些浏览器要求输入框可见才允许 showPicker：退回原生输入框
    }
    setNative(true);
  };
  if (native) {
    return (
      <DateInput value={value ?? ""} autoFocus onChange={(e) => onChange(e.target.value || null)} onBlur={() => setNative(false)} onKeyDown={(e) => { if (e.key === "Escape" || e.key === "Enter") setNative(false); }} className={cx("h-7 w-40 -my-1", className)} aria-label={ariaLabel} />
    );
  }
  return (
    <span className={cx("inl-wrap group relative inline-flex max-w-full items-center gap-1", className)}>
      <button type="button" className="inl" onClick={open} aria-label={ariaLabel}>
        <span className={cx("inl-text tabular-nums", !text && "text-ink-subtle")}>{text ?? placeholder ?? t("inline.none")}</span>
        <IconChevronDown size={12} className="inl-caret" aria-hidden="true" />
      </button>
      <input ref={input} type="date" tabIndex={-1} aria-hidden="true" value={value ?? ""} onChange={(e) => onChange(e.target.value || null)} className="pointer-events-none absolute bottom-0 left-0 h-px w-px opacity-0" />
      {nullable && value && (
        <button type="button" className="inl-chip-x opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100" onClick={() => onChange(null)} aria-label={t("inline.clear")} title={t("inline.clear")}>
          <IconClose size={12} />
        </button>
      )}
    </span>
  );
}
