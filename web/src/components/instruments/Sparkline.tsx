import { cx } from "@/components/ui";

/*
 * 迷你折线（web/DESIGN.md「舰内系统 v3」§3）：统计卡右上角，24 点。
 * hairline-strong 描边（安静的仪表走线）、可选 6% accent 面积、尾端一枚发光的 accent 圆点。
 * 只画数据不动画；点少于 2 个时画一条平线。
 */
export const SPARK_POINTS = 24;

export function Sparkline({ points, width = 96, height = 28, area = true, label, className }: { points: number[]; width?: number; height?: number; area?: boolean; label?: string; className?: string }) {
  const pts = points.length >= 2 ? points : [0, 0];
  const max = Math.max(...pts, 0);
  const pad = 3; // 给尾端圆点留位
  const w = width - pad * 2;
  const h = height - pad * 2;
  const xy = pts.map((p, i) => [pad + (i / (pts.length - 1)) * w, pad + h - (max > 0 ? (p / max) * h : 0)] as const);
  const line = xy.map(([x, y], i) => `${i === 0 ? "M" : "L"} ${x.toFixed(1)} ${y.toFixed(1)}`).join(" ");
  const [ex, ey] = xy[xy.length - 1];
  const areaD = `${line} L ${ex.toFixed(1)} ${(pad + h).toFixed(1)} L ${xy[0][0].toFixed(1)} ${(pad + h).toFixed(1)} Z`;
  return (
    <svg className={cx("spark", className)} width={width} height={height} viewBox={`0 0 ${width} ${height}`} role={label ? "img" : undefined} aria-hidden={label ? undefined : "true"} aria-label={label}>
      {area && <path className="spark-area" d={areaD} />}
      <path className="spark-line" d={line} />
      <circle className="spark-halo" cx={ex} cy={ey} r={4} />
      <circle className="spark-dot" cx={ex} cy={ey} r={1.75} />
    </svg>
  );
}

/**
 * 把带时间戳的记录按"最近 n 天，每天一格"归并成 n 个点（最后一格是今天）。
 * value 取每条记录的权重（默认 1 = 计数）；没有时间的记录忽略。
 */
export function bucketByDay<T>(rows: T[], at: (r: T) => string | null | undefined, value: (r: T) => number = () => 1, n = SPARK_POINTS): number[] {
  const out = new Array<number>(n).fill(0);
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const t0 = today.getTime();
  for (const r of rows) {
    const s = at(r);
    if (!s) continue;
    const d = new Date(s.length === 10 ? `${s}T00:00:00` : s);
    if (Number.isNaN(d.getTime())) continue;
    d.setHours(0, 0, 0, 0);
    const idx = n - 1 - Math.round((t0 - d.getTime()) / 86400000);
    if (idx >= 0 && idx < n) out[idx] += value(r);
  }
  return out;
}
