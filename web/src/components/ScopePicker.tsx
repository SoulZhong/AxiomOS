"use client";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { fmtMoney } from "@/lib/format";
import { t } from "@/lib/i18n";
import { financeHiddenText, setScope, useScopeState } from "@/lib/useScope";
import { IconCheck, IconChevronDown } from "./icons";
import { Readout, cx } from "./ui";

/*
 * 范围选择器（CONTEXT.md「范围」、ADR 0013）。
 * 放在状态栏里紧挨着组织读数：范围是全局筛选，每一页都在用它，而状态栏是唯一每页都在的横条；
 * 放进页头就得在十来个页面各摆一个，还要跟每页唯一的主按钮抢位置。
 * 选项与「这一档能不能看成本」都以 GET /auth/me 的 scopes[] 为准 —— 组织可以配自己的可见范围策略，
 * 界面不推算也不写死。选项少于两档时不显示（没得选就不占位）。
 * 状态栏本身 overflow: hidden，所以菜单挂到 body 上按按钮位置定位。
 */

const INDENT = 12;

export function ScopePicker({ className }: { className?: string }) {
  const state = useScopeState();
  const [open, setOpen] = useState(false);
  const [box, setBox] = useState<{ left: number; top: number } | null>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);

  const place = useCallback(() => {
    const r = btn.current?.getBoundingClientRect();
    if (r) setBox({ left: Math.max(8, Math.min(r.left, window.innerWidth - 268)), top: r.bottom + 6 });
  }, []);

  useLayoutEffect(() => {
    if (!open) return;
    place();
    const onScroll = () => place();
    window.addEventListener("resize", onScroll);
    window.addEventListener("scroll", onScroll, true);
    return () => {
      window.removeEventListener("resize", onScroll);
      window.removeEventListener("scroll", onScroll, true);
    };
  }, [open, place]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        btn.current?.focus();
      }
    };
    const onDown = (e: PointerEvent) => {
      const target = e.target as Node;
      if (!menu.current?.contains(target) && !btn.current?.contains(target)) setOpen(false);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("pointerdown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("pointerdown", onDown);
    };
  }, [open]);

  const options = state.options;
  if (options.length < 2) return null;
  const current = options.find((o) => o.id === state.scope);
  const hidden = financeHiddenText(state);

  return (
    <span className={cx("shrink-0 items-center gap-1.5 whitespace-nowrap", className)}>
      {t("scope.label")}{" "}
      <button
        ref={btn}
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="pressable inline-flex items-center gap-1 rounded-xs text-telemetry hover:text-ink"
        aria-haspopup="listbox"
        aria-expanded={open}
        title={t("scope.tip")}
      >
        <Readout value={current?.title ?? t("scope.all")} className="text-telemetry" />
        <IconChevronDown size={12} />
      </button>
      {open &&
        box &&
        createPortal(
          <div
            ref={menu}
            role="listbox"
            aria-label={t("scope.label")}
            className="fade-in fixed z-50 max-h-[70vh] w-[260px] overflow-y-auto rounded-lg border border-hairline-strong bg-surface-3 py-1 text-body"
            style={{ left: box.left, top: box.top }}
          >
            {options.map((o) => {
              const active = o.id === state.scope;
              return (
                <button
                  key={o.id}
                  type="button"
                  role="option"
                  aria-selected={active}
                  title={o.financial ? undefined : hidden}
                  onClick={() => {
                    setScope(o.id);
                    setOpen(false);
                  }}
                  className={cx(
                    "flex w-full items-center gap-2 py-1.5 pr-3 text-left hover:bg-surface-4",
                    active ? "text-ink" : "text-ink-muted",
                  )}
                  style={{ paddingLeft: 10 + o.depth * INDENT }}
                >
                  <span className="w-3.5 shrink-0 text-accent">{active && <IconCheck size={14} />}</span>
                  <span className="min-w-0 flex-1 truncate">{o.title}</span>
                  {!o.financial && <span className="shrink-0 text-caption text-ink-subtle">{t("scope.noFinanceShort")}</span>}
                </button>
              );
            })}
          </div>,
          document.body,
        )}
    </span>
  );
}

/**
 * 按范围显示的金额：当前这一档看不到财务数据时显示「—」，并用气泡说明为什么
 * （句子按组织的财务可见性策略选，见 financeHiddenText）。
 */
export function ScopeMoney({ value, currency, className }: { value: number | null | undefined; currency?: string; className?: string }) {
  const state = useScopeState();
  if (!state.financial) {
    return (
      <span className={cx("cursor-help text-ink-subtle", className)} title={financeHiddenText(state)}>
        —
      </span>
    );
  }
  return <span className={className}>{value === null || value === undefined ? "—" : fmtMoney(value, currency)}</span>;
}

/** 当前范围能不能看财务数据（用来决定整列、整块要不要出现）。 */
export function useFinancialVisible(): boolean {
  return useScopeState().financial;
}
