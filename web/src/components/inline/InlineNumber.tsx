"use client";
import { useRef, useState, type KeyboardEvent } from "react";
import { cx } from "@/components/ui";

/*
 * 数字 / 单行文字值控件（DESIGN.md §15）：静止是文本，悬停淡底，聚焦是焦点环；
 * 回车或失焦提交（值变了才），Esc 回到原值。可空字段清空即传 null。
 */
export function InlineNumber({ value, onChange, editable, prefix, suffix, min, step, nullable = true, placeholder = "—", format, ariaLabel, className }: {
  value: number | null;
  onChange: (next: number | null) => void;
  editable: boolean;
  /** 货币符号之类，紧贴数字前 */
  prefix?: string;
  /** 单位，数字后（小时、点） */
  suffix?: string;
  min?: number;
  step?: number | "any";
  nullable?: boolean;
  placeholder?: string;
  /** 静止态的数字写法（千分位、小数位） */
  format?: (n: number) => string;
  ariaLabel?: string;
  className?: string;
}) {
  const [draft, setDraft] = useState(value === null ? "" : String(value));
  const [focused, setFocused] = useState(false);
  const ref = useRef<HTMLInputElement>(null);
  // 外部值变了（保存成功 / 回退）且没在输入：草稿跟着变（渲染期调整状态，不用 effect）
  const [prev, setPrev] = useState(value);
  if (prev !== value) { setPrev(value); if (!focused) setDraft(value === null ? "" : String(value)); }

  const shownText = value === null ? null : `${prefix ?? ""}${format ? format(value) : String(value)}${suffix ? ` ${suffix}` : ""}`;
  if (!editable) return <span className={cx("inl-static tabular-nums", className)}>{shownText ?? <span className="text-ink-subtle">{placeholder}</span>}</span>;

  const commit = () => {
    const s = draft.trim();
    if (s === "") {
      if (nullable) { if (value !== null) onChange(null); }
      else setDraft(value === null ? "" : String(value));
      return;
    }
    const raw = Number(s.replace(/[,\s]/g, ""));
    const n = step === 1 ? Math.round(raw) : raw;
    if (!Number.isFinite(n) || (min !== undefined && n < min)) { setDraft(value === null ? "" : String(value)); return; }
    if (n !== value) onChange(n);
  };
  // 静止时显示带千分位的写法，聚焦后换成可编辑的原始数字（type=text 才能显示逗号）
  const idleText = value === null ? "" : format ? format(value) : String(value);
  const shownValue = focused ? draft : idleText;
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") { e.preventDefault(); ref.current?.blur(); }
    else if (e.key === "Escape") { e.preventDefault(); setDraft(value === null ? "" : String(value)); ref.current?.blur(); }
  };
  const width = `${Math.max(3, shownValue.length + 1)}ch`;
  return (
    <label className={cx("inl-input", className)}>
      {prefix && (focused || value !== null) && <span className="inl-affix">{prefix}</span>}
      <input
        ref={ref}
        type="text"
        inputMode="decimal"
        value={shownValue}
        placeholder={placeholder}
        aria-label={ariaLabel}
        onChange={(e) => setDraft(e.target.value)}
        onFocus={() => setFocused(true)}
        onBlur={() => { setFocused(false); commit(); }}
        onKeyDown={onKey}
        className="tabular-nums placeholder:text-ink-subtle"
        style={{ width }}
      />
      {suffix && (focused || value !== null) && <span className="inl-affix">{suffix}</span>}
    </label>
  );
}

/** 单行文字（自定义字段里的文本）：同一套外形。 */
export function InlineTextInput({ value, onChange, editable, placeholder = "—", ariaLabel, className }: { value: string; onChange: (next: string) => void; editable: boolean; placeholder?: string; ariaLabel?: string; className?: string }) {
  const [draft, setDraft] = useState(value);
  const [focused, setFocused] = useState(false);
  const ref = useRef<HTMLInputElement>(null);
  const [prev, setPrev] = useState(value);
  if (prev !== value) { setPrev(value); if (!focused) setDraft(value); }
  if (!editable) return <span className={cx("inl-static", className)}>{value || <span className="text-ink-subtle">{placeholder}</span>}</span>;
  const commit = () => {
    const s = draft.trim();
    if (s !== value) onChange(s);
  };
  return (
    <label className={cx("inl-input w-full", className)}>
      <input
        ref={ref}
        type="text"
        value={draft}
        placeholder={placeholder}
        aria-label={ariaLabel}
        onChange={(e) => setDraft(e.target.value)}
        onFocus={() => setFocused(true)}
        onBlur={() => { setFocused(false); commit(); }}
        onKeyDown={(e) => {
          if (e.key === "Enter") { e.preventDefault(); ref.current?.blur(); }
          else if (e.key === "Escape") { e.preventDefault(); setDraft(value); ref.current?.blur(); }
        }}
        className="placeholder:text-ink-subtle"
      />
    </label>
  );
}
