import { Empty, Tip, cx } from "@/components/ui";
import { t } from "@/lib/i18n";

/*
 * 吞吐柱状图（web/DESIGN.md「舰内系统 v3」§3）：按周的创建 / 完成 / 终止。
 * 柱子只用 token（accent 的两档透明度 + 中性）；每根柱顶一条 accent 边光；本周（最后一列）的柱以 3s 周期呼吸。
 * 网格线 hairline；坐标轴文字等宽 11px，列多时隔一显示；悬停看各系列的数。
 */
export interface ThroughputColumn {
  key: string;
  label: string;
  now?: boolean;
  series: Array<{ name: string; value: number; tone: "accent-soft" | "accent" | "neutral" }>;
}
const FILL: Record<ThroughputColumn["series"][number]["tone"], string> = {
  "accent-soft": "color-mix(in srgb, var(--c-accent) 30%, transparent)",
  accent: "var(--c-accent)",
  neutral: "var(--c-hairline-tertiary)",
};

export function ThroughputBars({ columns, height = 120 }: { columns: ThroughputColumn[]; height?: number }) {
  if (!columns.length) return <Empty text={t("common.noData")} illustration={false} className="py-6" />;
  const max = Math.max(...columns.flatMap((c) => c.series.map((s) => s.value)), 0) || 1;
  const step = columns.length > 8 ? 2 : 1;
  const last = columns.length - 1;
  const showLabel = (i: number) => (last - i) % step === 0;
  return (
    <div>
      <div className="relative flex items-end gap-2 border-b border-hairline" style={{ height }}>
        <span className="pointer-events-none absolute inset-x-0 top-0 border-t border-dashed border-hairline" aria-hidden="true" />
        <span className="pointer-events-none absolute inset-x-0 top-1/2 border-t border-dashed border-hairline" aria-hidden="true" />
        {columns.map((c) => (
          <Tip key={c.key} tip={`${c.label}\n${c.series.map((s) => `${s.name} ${s.value}`).join(" · ")}`} className="relative h-full min-w-0 flex-1 rounded-t-xs hover:bg-surface-2">
            <div className="flex h-full w-full items-end justify-center gap-0.5 px-0.5">
              {c.series.map((s) => (
                <div key={s.name} className={cx("tbar", s.value > 0 && s.tone !== "neutral" && "tbar-lit", c.now && s.value > 0 && "tbar-now")} style={{ height: `${(s.value / max) * 100}%`, background: FILL[s.tone], minHeight: s.value ? 2 : 0 }} />
              ))}
            </div>
          </Tip>
        ))}
      </div>
      <div className="mt-1 flex gap-2">
        {columns.map((c, i) => (
          <div key={c.key} className={cx("min-w-0 flex-1 text-center font-mono text-[11px] tabular-nums whitespace-nowrap", c.now ? "text-telemetry" : "text-ink-subtle")}>
            {showLabel(i) ? c.label : ""}
          </div>
        ))}
      </div>
      <div className="mt-2 flex gap-4 text-caption text-ink-subtle">
        {columns[0].series.map((s) => (
          <span key={s.name} className="inline-flex items-center gap-1.5">
            <span className="inline-block h-2 w-2 rounded-xs" style={{ background: FILL[s.tone] }} />
            {s.name}
          </span>
        ))}
      </div>
    </div>
  );
}
