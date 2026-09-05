"use client";
import { api } from "@/lib/api";
import { LOCALE_NAMES, LOCALES, setLocale, useLocale, type Locale } from "@/lib/i18n";
import { cx } from "./ui";

/**
 * 语言切换：11px 等宽眉标风格（舰桥标识牌的一部分）。总是写 localStorage；loggedIn 时再同步到后端（PATCH /auth/me）。
 * 后端调用失败不阻塞切换：本地已经切了，下次登录会再对齐。
 * onDark 是旧参数（现在只有一个世界），保留以兼容调用。
 */
export function LanguageSwitch({ loggedIn = false, className }: { loggedIn?: boolean; className?: string; onDark?: boolean }) {
  const { locale } = useLocale();
  const pick = (l: Locale) => {
    if (l === locale) return;
    if (loggedIn) void api.auth.updateMe({ locale: l }).catch(() => {});
    setLocale(l);
  };
  return (
    <span className={cx("eyebrow inline-flex items-center gap-2 normal-case", className)}>
      {LOCALES.map((l, i) => (
        <span key={l} className="inline-flex items-center gap-2">
          {i > 0 && <span className="text-hairline-tertiary" aria-hidden="true">/</span>}
          <button type="button" onClick={() => pick(l)} className={cx("pressable rounded-xs", l === locale ? "text-ink" : "text-ink-subtle hover:text-ink")} aria-pressed={l === locale}>
            {LOCALE_NAMES[l]}
          </button>
        </span>
      ))}
    </span>
  );
}
