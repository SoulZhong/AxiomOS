// 日期、金额、token 的显示格式，跟随当前语言（src/lib/i18n.ts）。
import { getLocale, t } from "./i18n";

export function parseDate(s: string | null | undefined): Date | null {
  if (!s) return null;
  const d = new Date(s.length === 10 ? `${s}T00:00:00` : s);
  return Number.isNaN(d.getTime()) ? null : d;
}

export function toISODate(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

export function today(): Date {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  return d;
}

export function addDays(d: Date, n: number): Date {
  const r = new Date(d);
  r.setDate(r.getDate() + n);
  return r;
}

export function diffDays(a: Date, b: Date): number {
  return Math.round((b.getTime() - a.getTime()) / 86400000);
}

/** 周一 */
export function startOfWeek(d: Date): Date {
  const r = new Date(d);
  r.setHours(0, 0, 0, 0);
  const wd = (r.getDay() + 6) % 7;
  r.setDate(r.getDate() - wd);
  return r;
}

const MONTHS_EN = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** 月份短名：zh 返回 "9月"，en 返回 "Sep"。month 为 1–12。 */
export function monthName(month: number): string {
  return getLocale() === "zh-CN" ? `${month}月` : MONTHS_EN[month - 1];
}

export function fmtDate(s: string | null | undefined, withYear = false): string {
  const d = parseDate(s);
  if (!d) return "—";
  const y = d.getFullYear(), m = d.getMonth() + 1, day = d.getDate();
  if (getLocale() === "zh-CN") return withYear ? `${y}年${m}月${day}日` : `${m}月${day}日`;
  return withYear ? `${MONTHS_EN[m - 1]} ${day}, ${y}` : `${MONTHS_EN[m - 1]} ${day}`;
}

export function fmtDateTime(s: string | null | undefined): string {
  const d = parseDate(s);
  if (!d) return "—";
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  return `${fmtDate(s)} ${hh}:${mm}`;
}

export function fmtRelative(s: string | null | undefined): string {
  const d = parseDate(s);
  if (!d) return "—";
  const diff = Date.now() - d.getTime();
  const m = Math.round(diff / 60000);
  if (m < 1) return t("time.justNow");
  if (m < 60) return t("time.minutesAgo", { n: m });
  const h = Math.round(m / 60);
  if (h < 24) return t("time.hoursAgo", { n: h });
  const days = Math.round(h / 24);
  if (days < 30) return t("time.daysAgo", { n: days });
  return fmtDate(s);
}

const CURRENCY_SYMBOL: Record<string, string> = { CNY: "¥", USD: "$", EUR: "€", GBP: "£", JPY: "JP¥", HKD: "HK$" };

export function fmtNumber(n: number, digits = 2): string {
  return n.toLocaleString(getLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
}

/** 金额：人民币用 ¥，其他常见货币用符号，不认识的货币前置代码。 */
export function fmtMoney(n: number | null | undefined, currency = "CNY"): string {
  if (n === null || n === undefined) return "—";
  const sym = CURRENCY_SYMBOL[currency] ?? `${currency} `;
  return `${sym}${fmtNumber(n)}`;
}

export function fmtTokens(n: number | null | undefined): string {
  if (!n) return "0";
  if (getLocale() === "zh-CN") {
    if (n >= 100_000_000) return t("num.yi", { n: (n / 100_000_000).toFixed(2) });
    if (n >= 10_000) return t("num.wan", { n: (n / 10_000).toFixed(n >= 1_000_000 ? 0 : 1) });
    return n.toLocaleString("zh-CN");
  }
  if (n >= 1_000_000) return t("num.m", { n: (n / 1_000_000).toFixed(n >= 100_000_000 ? 0 : 1) });
  if (n >= 1_000) return t("num.k", { n: (n / 1_000).toFixed(n >= 100_000 ? 0 : 1) });
  return n.toLocaleString("en-US");
}

export function fmtHours(h: number | null | undefined): string {
  if (h === null || h === undefined) return "—";
  if (h < 1) return t("time.minutes", { n: Math.round(h * 60) });
  if (h < 48) return t("time.hours", { n: h.toFixed(1) });
  return t("time.days", { n: (h / 24).toFixed(1) });
}

export function fmtDuration(startISO: string, endISO: string | null): string {
  const a = parseDate(startISO);
  const b = endISO ? parseDate(endISO) : new Date();
  if (!a || !b) return "—";
  return fmtHours((b.getTime() - a.getTime()) / 3600000);
}

export function fmtPercent(p: number): string {
  return `${Math.round(p * 100)}%`;
}
