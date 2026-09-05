"use client";
import { t } from "@/lib/i18n";
import { THEMES, useTheme, type Theme } from "@/lib/theme";
import { Segmented } from "./ui";

/** 主题切换：系统 / 深色 / 浅色 的小分段控件。侧边栏页脚占满一行（block），后台顶栏用紧凑版。 */
export function ThemeSwitch({ block = false, className }: { block?: boolean; className?: string }) {
  const [theme, setTheme] = useTheme();
  const options: Array<[Theme, string]> = THEMES.map((k) => [k, t(`theme.${k}`)]);
  return <Segmented value={theme} options={options} onChange={setTheme} size="sm" block={block} className={className} aria-label={t("theme.label")} />;
}
