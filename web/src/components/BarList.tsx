import type { ReactNode } from "react";
import { Empty, Tip, cx } from "./ui";
import { t } from "@/lib/i18n";

export interface BarItem {
  key: string;
  title: ReactNode;
  value: number;
  display: ReactNode;
  sub?: ReactNode;
}

/** 横向条形列表：一行一个条目，条长按最大值等比。看板里的"小图"。 */
export function BarList({ items, tone = "accent", limit }: { items: BarItem[]; tone?: "accent" | "success" | "danger"; limit?: number }) {
  if (!items.length) return <Empty text={t("common.noData")} illustration={false} className="py-6" />;
  const shown = limit ? items.slice(0, limit) : items;
  const max = Math.max(...items.map((i) => i.value), 0) || 1;
  const fill = tone === "success" ? "bg-success" : tone === "danger" ? "bg-danger" : "bg-accent";
  return (
    <ul className="space-y-2">
      {shown.map((i) => (
        <li key={i.key} className="grid grid-cols-[minmax(0,140px)_1fr_auto] items-center gap-3 text-body">
          <span className="truncate" title={typeof i.title === "string" ? i.title : undefined}>
            {i.title}
          </span>
          <div className="h-2 w-full overflow-hidden rounded-xs bg-surface-3">
            <div className={cx("h-full rounded-xs", fill)} style={{ width: `${Math.max(1, (i.value / max) * 100)}%` }} />
          </div>
          <span className="text-right tabular-nums">
            {i.display}
            {i.sub && <span className="ml-1.5 font-mono text-[11px] text-ink-subtle">{i.sub}</span>}
          </span>
        </li>
      ))}
    </ul>
  );
}

/** 竖向柱状：用于按周的吞吐。柱子只用 accent 单色（不同透明度）+ 中性；网格线 hairline；坐标轴文字等宽 11px。标签用 8/31 这样的短日期，列多时隔一个显示；悬停看各系列的数。 */
export function ColumnChart({ columns, height = 120 }: { columns: Array<{ key: string; label: string; series: Array<{ name: string; value: number; color: string }> }>; height?: number }) {
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
                <div key={s.name} className="w-full max-w-4 rounded-t-xs" style={{ height: `${(s.value / max) * 100}%`, background: s.color, minHeight: s.value ? 2 : 0 }} />
              ))}
            </div>
          </Tip>
        ))}
      </div>
      <div className="mt-1 flex gap-2">
        {columns.map((c, i) => (
          <div key={c.key} className="min-w-0 flex-1 text-center font-mono text-[11px] tabular-nums whitespace-nowrap text-ink-subtle">
            {showLabel(i) ? c.label : ""}
          </div>
        ))}
      </div>
      <div className="mt-2 flex gap-4 text-caption text-ink-subtle">
        {columns[0].series.map((s) => (
          <span key={s.name} className="inline-flex items-center gap-1.5">
            <span className="inline-block h-2 w-2 rounded-xs" style={{ background: s.color }} />
            {s.name}
          </span>
        ))}
      </div>
    </div>
  );
}
