"use client";
import { t } from "@/lib/i18n";
import { HudCorners, cx } from "@/components/ui";
import { Readout, useReadoutItems } from "./Readout";

/**
 * 组织概况：信息优先的读数条（进行中 / 待验收 / 逾期 / 今日成本，各带 24 小时趋势线）。
 * 科技感只来自页面风格、状态栏、动效与交互，不在这里放任何占空间的装饰（用户明确要求）。
 * 读数与画法在 Readout.tsx，与「我的工作」里的「组织概况」区块（ReadoutsBlock，ADR 0015 补记四）共用；这一条给不在网格里的页面用。
 */
export function ShipStatus({ className }: { className?: string }) {
  const { items } = useReadoutItems();
  return (
    <section className={cx("hud rounded-lg border border-hairline bg-surface-1 shadow-panel", className)} aria-label={t("ship.title")}>
      <HudCorners />
      {/* 信息优先：读数占主体 */}
      <div className="flex min-w-0 flex-col gap-3 px-5 py-4">
        <div className="eyebrow text-ink-subtle">{t("ship.title")}</div>
        <div className="grid grid-cols-2 gap-x-6 gap-y-4 md:grid-cols-4">
          {items.map(({ key, ...r }) => (
            <Readout key={key} {...r} />
          ))}
        </div>
      </div>
    </section>
  );
}
