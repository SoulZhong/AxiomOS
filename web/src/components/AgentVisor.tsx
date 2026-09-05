"use client";
import { t } from "@/lib/i18n";
import { cx } from "@/components/ui";

/**
 * Agent 的状态语言（提案「Axiom 图标与动效」§四）：视窗 + 双点 + 外环，只用亮度与节律，不做表情。
 * - online：双点常亮，环 success 3s 呼吸（有心跳，可以领任务）
 * - busy：双点交替明暗（永不同步），环 accent 常亮（有打开的执行记录）
 * - waiting：双点常亮偏暖，环静止（发起了「请求输入」，等人回答）
 * - offline：双点 25%，环灰静止（心跳超时）
 * motion="charge"：刚上线，外环顺时针充满 520ms、双点随后点亮；"dim"：刚离线，400ms 变灰（CSS 过渡）。
 * 样式在 globals.css「签名图标与状态动效」C 节。20 / 24 / 32 三档。
 */
export type VisorState = "online" | "busy" | "waiting" | "offline";
export function AgentVisor({ state, size = 24, motion, className }: { state: VisorState; size?: number; motion?: "charge" | "dim"; className?: string }) {
  return (
    <span className={cx("visor", className)} style={{ width: size, height: size }} data-state={state} data-motion={motion} role="img" aria-label={state === "offline" ? t("agents.offlineShort") : t("agents.online")}>
      <svg width={size} height={size} viewBox="0 0 24 24" focusable="false" aria-hidden="true">
        <circle className="visor-ring" cx="12" cy="12" r="10.5" />
        <rect className="ax-visor" x="5" y="8" width="14" height="8" rx="4" />
        <circle className="ax-eye ax-eye-l" cx="9.5" cy="12" r="1.3" />
        <circle className="ax-eye ax-eye-r" cx="14.5" cy="12" r="1.3" />
      </svg>
    </span>
  );
}
