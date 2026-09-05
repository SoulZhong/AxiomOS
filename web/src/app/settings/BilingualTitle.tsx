"use client";
import type { LocalizedTitle } from "@/lib/api";
import { getLocale, t, type Locale } from "@/lib/i18n";
import { Field, Input } from "@/components/ui";

export interface TitlePair { zh: string; en: string }

/** 从服务端给的 titles（或只有当前语言的 title）填出两个输入框的初值。 */
export function titlePairOf(title?: string, titles?: Partial<Record<Locale, string>>): TitlePair {
  if (titles) return { zh: titles["zh-CN"] ?? "", en: titles["en-US"] ?? "" };
  return getLocale() === "zh-CN" ? { zh: title ?? "", en: "" } : { zh: "", en: title ?? "" };
}

/** 两个输入框 -> 请求体：只填了当前语言时发字符串；否则发对象，只带填了的语言。空则 null。 */
export function buildTitle(p: TitlePair): LocalizedTitle | null {
  const zh = p.zh.trim(), en = p.en.trim();
  if (!zh && !en) return null;
  const cur = getLocale();
  if (zh && !en && cur === "zh-CN") return zh;
  if (en && !zh && cur === "en-US") return en;
  const obj: Partial<Record<Locale, string>> = {};
  if (zh) obj["zh-CN"] = zh;
  if (en) obj["en-US"] = en;
  return obj;
}

export function BilingualTitleFields({ value, onChange, autoFocus }: { value: TitlePair; onChange: (v: TitlePair) => void; autoFocus?: boolean }) {
  return (
    <div className="grid grid-cols-2 gap-3">
      <Field label={t("settings.roles.titleZh")}><Input value={value.zh} onChange={(e) => onChange({ ...value, zh: e.target.value })} autoFocus={autoFocus && getLocale() === "zh-CN"} /></Field>
      <Field label={t("settings.roles.titleEn")} hint={t("settings.roles.titleHint")}><Input value={value.en} onChange={(e) => onChange({ ...value, en: e.target.value })} autoFocus={autoFocus && getLocale() === "en-US"} /></Field>
    </div>
  );
}
