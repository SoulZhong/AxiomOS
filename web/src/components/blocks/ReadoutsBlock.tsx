"use client";
import { t } from "@/lib/i18n";
import { IconChart } from "@/components/icons";
import { Readout, useReadoutItems } from "@/components/ship-status/Readout";
import { cx } from "@/components/ui";
import { BlockPanel, type BlockProps } from "./BlockPanel";

/**
 * 组织概况区块（ADR 0015 补记四）：进行中 / 待验收 / 逾期 / 今日成本四个实时读数，带 24 小时趋势线，不受范围影响。
 * 尺寸适配：≥ 8 栏一行四个；4 < w < 8 时 2×2；compact（≤ 4 栏）一列往下排、趋势线放右侧。
 * dense（1 行高，默认尺寸就是 12×1）一行四个、数字收小；≥ 8 栏时趋势线放到数字右侧（120px 里放得下，默认尺寸也看得到趋势），更窄时不画趋势线。
 */
export function ReadoutsBlock({ index, title, noLink, w, compact, dense }: BlockProps) {
  const { items } = useReadoutItems();
  const cols = dense ? 4 : compact ? 1 : w !== undefined && w > 4 && w < 8 ? 2 : 4;
  const wide = w === undefined || w >= 8;
  const spark = !dense || wide;
  const row = (compact && !dense) || (dense && wide);
  return (
    <BlockPanel
      index={index}
      noLink={noLink}
      compact={compact}
      dense={dense}
      icon={<IconChart />}
      title={title ?? t("block.readouts")}
      telemetry={t("ship.trend24h")}
      padded={false}
    >
      <div
        className={cx(
          "grid h-full content-center px-4",
          dense ? "gap-x-4 py-2" : "gap-x-6 gap-y-4 py-4",
          cols === 4 ? "grid-cols-4" : cols === 2 ? "grid-cols-2" : "grid-cols-1",
          compact && !dense && "gap-y-2 py-3",
        )}
      >
        {items.map(({ key, ...r }) => (
          <Readout key={key} {...r} size={dense || compact ? "sm" : "md"} spark={spark} row={row} />
        ))}
      </div>
    </BlockPanel>
  );
}
