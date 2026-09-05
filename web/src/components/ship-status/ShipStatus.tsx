"use client";
import type { ReactNode } from "react";
import { useSession } from "@/components/AppShell";
import { fmtMoney } from "@/lib/format";
import { t } from "@/lib/i18n";
import { HudCorners, cx } from "@/components/ui";
import { Sparkline, type SparkTone } from "./Sparkline";
import { useShipTelemetry } from "./telemetry";

/**
 * 组织概况：信息优先的读数面板（进行中 / 待验收 / 逾期 / 今日成本，各带 24 小时趋势线）。
 * 科技感只来自页面风格、状态栏、动效与交互，不在这里放任何占空间的装饰（用户明确要求）。
 * 数据来自 telemetry.ts（与状态栏共用一次请求，30s 刷新）。首页与运营看板顶部各放一块。
 */

function Readout({ label, value, series, tone, hint }: { label: string; value: ReactNode; series: number[] | undefined; tone: SparkTone; hint?: string }) {
  const color = tone === "danger" ? "text-danger" : tone === "warning" ? "text-warning" : "text-ink";
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <span className="text-caption text-ink-subtle">{label}</span>
      <span className={cx("font-mono text-[26px] leading-none font-medium tracking-[-0.4px] tabular-nums", color)}>{value}</span>
      <Sparkline data={series ?? []} width={96} height={20} tone={series && series.some((v) => v !== series[0]) ? tone : "neutral"} title={hint} />
    </div>
  );
}

export function ShipStatus({ className }: { className?: string }) {
  const { session } = useSession();
  const tele = useShipTelemetry(!!session);
  const currency = session?.organization.currency;

  return (
    <section className={cx("hud rounded-lg border border-hairline bg-surface-1 shadow-panel", className)} aria-label={t("ship.title")}>
      <HudCorners />
      {/* 信息优先：读数占主体 */}
      <div className="flex min-w-0 flex-col gap-3 px-5 py-4">
        <div className="eyebrow text-ink-subtle">{t("ship.title")}</div>
        <div className="grid grid-cols-2 gap-x-6 gap-y-4 md:grid-cols-4">
          <Readout label={t("ship.active")} value={tele ? tele.active : "—"} series={tele?.series.active} tone="accent" hint={t("ship.trend24h")} />
          <Readout label={t("ship.waiting")} value={tele ? tele.waiting : "—"} series={tele?.series.waiting} tone={tele && tele.waiting > 0 ? "warning" : "neutral"} hint={t("ship.trend24h")} />
          <Readout label={t("ship.overdue")} value={tele ? tele.overdue : "—"} series={tele?.series.overdue} tone={tele && tele.overdue > 0 ? "danger" : "neutral"} hint={t("ship.trend24h")} />
          <Readout label={t("ship.costToday")} value={tele ? fmtMoney(tele.costToday, currency) : "—"} series={tele?.series.cost} tone="accent" hint={tele?.costReal ? t("bridge.costSpark") : t("bridge.costFlat")} />
        </div>
      </div>
    </section>
  );
}
