"use client";
import type { ReactNode } from "react";
import { useSession } from "@/components/AppShell";
import { fmtMoney } from "@/lib/format";
import { t } from "@/lib/i18n";
import { cx } from "@/components/ui";
import { Sparkline, type SparkTone } from "./Sparkline";
import { useShipTelemetry } from "./telemetry";

/**
 * 组织概况的四个读数（进行中 / 待验收 / 逾期 / 今日成本，各带 24 小时趋势线）：
 * 「组织概况」区块（ReadoutsBlock）与独立的读数条（ShipStatus）共用这一份取数与画法。数据来自 telemetry.ts（与状态栏共用一次请求，30s 刷新）。
 */
export interface ReadoutItem {
  key: "active" | "waiting" | "overdue" | "cost";
  label: string;
  value: ReactNode;
  series: number[] | undefined;
  tone: SparkTone;
  hint: string;
}

/** 四个读数的当前值；遥测还没回来时 value 是 "—"、series 为空。 */
export function useReadoutItems(): { items: ReadoutItem[]; ready: boolean } {
  const { session } = useSession();
  const tele = useShipTelemetry(!!session);
  const currency = session?.organization.currency;
  const items: ReadoutItem[] = [
    { key: "active", label: t("ship.active"), value: tele ? tele.active : "—", series: tele?.series.active, tone: "accent", hint: t("ship.trend24h") },
    { key: "waiting", label: t("ship.waiting"), value: tele ? tele.waiting : "—", series: tele?.series.waiting, tone: tele && tele.waiting > 0 ? "warning" : "neutral", hint: t("ship.trend24h") },
    { key: "overdue", label: t("ship.overdue"), value: tele ? tele.overdue : "—", series: tele?.series.overdue, tone: tele && tele.overdue > 0 ? "danger" : "neutral", hint: t("ship.trend24h") },
    { key: "cost", label: t("ship.costToday"), value: tele ? fmtMoney(tele.costToday, currency) : "—", series: tele?.series.cost, tone: "accent", hint: tele?.costReal ? t("bridge.costSpark") : t("bridge.costFlat") },
  ];
  return { items, ready: !!tele };
}

/**
 * 一个读数：眉标 → 等宽大数字 → 趋势线。
 * size="sm" 数字 22px（窄 / 1 行高的区块）；spark=false 不画趋势线（1 行高）；row 时趋势线放到右侧、与数字同一行（窄区块一列往下排）。
 */
export function Readout({ label, value, series, tone, hint, size = "md", spark = true, row = false, className }: Omit<ReadoutItem, "key"> & { size?: "md" | "sm"; spark?: boolean; row?: boolean; className?: string }) {
  const color = tone === "danger" ? "text-danger" : tone === "warning" ? "text-warning" : "text-ink";
  const live = series && series.some((v) => v !== series[0]) ? tone : "neutral";
  const figure = <span className={cx("font-mono leading-none font-medium tracking-[-0.4px] tabular-nums", size === "sm" ? "text-[22px]" : "text-[26px]", color)}>{value}</span>;
  const line = spark ? <Sparkline data={series ?? []} width={96} height={20} tone={live} title={hint} /> : null;
  if (row)
    return (
      <div className={cx("flex min-w-0 items-center gap-4", className)}>
        <span className="flex min-w-0 flex-col gap-1">
          <span className="truncate text-caption text-ink-subtle">{label}</span>
          {figure}
        </span>
        {line}
      </div>
    );
  return (
    <div className={cx("flex min-w-0 flex-col gap-1.5", className)}>
      <span className="truncate text-caption text-ink-subtle">{label}</span>
      {figure}
      {line}
    </div>
  );
}
