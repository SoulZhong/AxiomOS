"use client";
import { cx } from "@/components/ui";

/**
 * 迷你折线：24 点，hairline-strong 描边，尾端一枚发光点（tone 决定颜色：accent / danger / warning / success）。
 * 纯内联 SVG，没有坐标轴；空序列或全零画一条贴底的平线。
 */
export type SparkTone = "accent" | "danger" | "warning" | "success" | "neutral";
const DOT: Record<SparkTone, string> = { accent: "var(--c-accent-hover)", danger: "var(--c-danger)", warning: "var(--c-warning)", success: "var(--c-success)", neutral: "var(--c-ink-subtle)" };

export function Sparkline({ data, width = 48, height = 14, tone = "accent", className, title }: { data: number[]; width?: number; height?: number; tone?: SparkTone; className?: string; title?: string }) {
  const n = data.length;
  const max = Math.max(0, ...data);
  const min = Math.min(0, ...data);
  const range = max - min || 1;
  const pad = 2;
  const pts = data.map((v, i) => {
    const x = n > 1 ? pad + (i / (n - 1)) * (width - pad * 2) : width / 2;
    const y = height - pad - ((v - min) / range) * (height - pad * 2);
    return [x, y] as const;
  });
  const d = pts.length ? pts.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(1)} ${y.toFixed(1)}`).join(" ") : `M${pad} ${height - pad} L${width - pad} ${height - pad}`;
  const last = pts[pts.length - 1] ?? [width - pad, height - pad];
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className={cx("spark shrink-0", className)} aria-hidden={title ? undefined : true} role={title ? "img" : undefined} focusable="false">
      {title && <title>{title}</title>}
      <path d={d} fill="none" stroke="var(--c-hairline-strong)" strokeWidth="1" strokeLinejoin="round" strokeLinecap="round" vectorEffect="non-scaling-stroke" />
      <circle cx={last[0]} cy={last[1]} r="2.6" fill={DOT[tone]} opacity="0.28" />
      <circle cx={last[0]} cy={last[1]} r="1.4" fill={DOT[tone]} />
    </svg>
  );
}
