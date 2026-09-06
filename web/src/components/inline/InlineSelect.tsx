"use client";
import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { t } from "@/lib/i18n";
import { IconCheck, IconChevronDown } from "@/components/icons";
import { cx } from "@/components/ui";

/*
 * 下拉值控件（DESIGN.md §15）：静止时是普通文本，悬停出现淡底 + 细线 + 小箭头；点开是 surface-3 的列表。
 * 选项 > 8 个时顶部带搜索框；depth 用于目标树的层级缩进；nullable 时列表首项是「不指定」（或调用方给的字样）。
 * 键盘：↑↓ 移动、Enter 选中、Esc 关闭、Home/End 首尾；搜索框内同样可用。
 * 不能改的人看到的是同一行高的纯文本（display 或选项名）。
 */
export interface InlineOption {
  value: string;
  label: string;
  /** 树形缩进层级（目标 / 团队） */
  depth?: number;
  /** 右侧的 12px 灰字（类型、状态） */
  hint?: string;
  icon?: ReactNode;
  disabled?: boolean;
  /** 额外的搜索词 */
  keywords?: string;
}

export function InlineSelect({ value, options, onChange, editable, nullable = false, nullLabel, display, placeholder, searchable, align = "left", ariaLabel, className, loading }: {
  value: string | null;
  options: InlineOption[];
  onChange: (next: string | null) => void;
  editable: boolean;
  nullable?: boolean;
  nullLabel?: string;
  /** 静止态怎么画当前值（头像 + 名字之类）；不给则用选项名 */
  display?: ReactNode;
  placeholder?: string;
  searchable?: boolean;
  align?: "left" | "right";
  ariaLabel?: string;
  className?: string;
  /** 选项还在加载：菜单里显示加载中 */
  loading?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const [place, setPlace] = useState<"bottom" | "top">("bottom");
  const wrap = useRef<HTMLSpanElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const id = useId();
  const canSearch = searchable ?? options.length > 8;
  const current = options.find((o) => o.value === value);

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    const filtered = q ? options.filter((o) => `${o.label} ${o.keywords ?? ""} ${o.hint ?? ""}`.toLowerCase().includes(q)) : options;
    const nullRow: InlineOption[] = nullable && !q ? [{ value: "", label: nullLabel ?? t("inline.none") }] : [];
    return [...nullRow, ...filtered];
  }, [options, query, nullable, nullLabel]);

  const close = (refocus: boolean) => {
    setOpen(false);
    setQuery("");
    if (refocus) btn.current?.focus();
  };
  // 打开：定位到当前值；下方空间不够时向上展开
  const openMenu = () => {
    const i = rows.findIndex((o) => (o.value || null) === value);
    setActive(i >= 0 ? i : 0);
    const r = wrap.current?.getBoundingClientRect();
    setPlace(r && window.innerHeight - r.bottom < 320 && r.top > 320 ? "top" : "bottom");
    setOpen(true);
  };
  const pick = (o: InlineOption | undefined) => {
    if (!o || o.disabled) return;
    const next = o.value || null;
    close(true);
    if (next !== value) onChange(next);
  };
  useEffect(() => {
    if (!open) return;
    (canSearch ? search.current : list.current)?.focus();
    const onDown = (e: MouseEvent) => {
      if (!wrap.current?.contains(e.target as Node)) { setOpen(false); setQuery(""); }
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open, canSearch]);
  useEffect(() => {
    if (!open) return;
    const el = list.current?.querySelector<HTMLElement>(`[data-i="${active}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [active, open]);

  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape") { e.preventDefault(); close(true); return; }
    if (e.key === "ArrowDown") { e.preventDefault(); setActive((i) => Math.min(rows.length - 1, i + 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setActive((i) => Math.max(0, i - 1)); }
    else if (e.key === "Home") { e.preventDefault(); setActive(0); }
    else if (e.key === "End") { e.preventDefault(); setActive(rows.length - 1); }
    else if (e.key === "Enter") { e.preventDefault(); pick(rows[active]); }
    else if (e.key === "Tab") close(false);
  };

  const shown = display ?? (current ? <span className="inline-flex min-w-0 items-center gap-1.5">{current.icon}<span className="inl-text">{current.label}</span></span> : <span className="inl-text text-ink-subtle">{placeholder ?? (nullable ? (nullLabel ?? t("inline.none")) : "—")}</span>);
  if (!editable) return <span className={cx("inl-static", className)}>{shown}</span>;

  return (
    <span ref={wrap} className={cx("inl-wrap relative inline-flex max-w-full", className)}>
      <button ref={btn} type="button" className="inl" aria-haspopup="listbox" aria-expanded={open} aria-controls={open ? `${id}-list` : undefined} aria-label={ariaLabel} onClick={() => (open ? close(true) : openMenu())} onKeyDown={(e) => { if (!open && (e.key === "ArrowDown" || e.key === "ArrowUp")) { e.preventDefault(); openMenu(); } }}>
        {shown}
        <IconChevronDown size={12} className="inl-caret" aria-hidden="true" />
      </button>
      {open && (
        <div ref={menu} className="inl-menu" data-align={align} style={place === "top" ? { top: "auto", bottom: "calc(100% + 4px)" } : undefined} onKeyDown={onKey}>
          {canSearch && <input ref={search} value={query} onChange={(e) => { setQuery(e.target.value); setActive(0); }} placeholder={t("inline.search")} className="inl-menu-search text-body" aria-label={t("inline.search")} aria-controls={`${id}-list`} />}
          <ul ref={list} id={`${id}-list`} role="listbox" tabIndex={canSearch ? -1 : 0} className="inl-menu-list" aria-activedescendant={rows[active] ? `${id}-${active}` : undefined}>
            {loading && <li role="presentation" className="inl-opt text-ink-subtle">{t("common.loading")}</li>}
            {!loading && rows.length === 0 && <li role="presentation" className="inl-opt text-ink-subtle">{options.length ? t("inline.noMatch") : t("inline.noOptions")}</li>}
            {rows.map((o, i) => {
              const selected = (o.value || null) === value;
              return (
                <li key={o.value || "__null"} id={`${id}-${i}`} data-i={i} role="option" aria-selected={selected} aria-disabled={o.disabled || undefined} data-active={i === active ? "" : undefined} className="inl-opt text-body" style={o.depth ? { paddingLeft: 8 + o.depth * 14 } : undefined} onMouseMove={() => setActive(i)} onMouseDown={(e) => e.preventDefault()} onClick={() => pick(o)}>
                  <span className="inl-opt-check" aria-hidden="true">{selected && <IconCheck size={12} />}</span>
                  {o.icon}
                  <span className={cx("inl-opt-label", !o.value && "text-ink-subtle")}>{o.label}</span>
                  {o.hint && <span className="inl-opt-hint">{o.hint}</span>}
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </span>
  );
}
