"use client";
import { useEffect, useId, useState } from "react";
import { cx } from "@/components/ui";

/*
 * 弧形仪表（web/DESIGN.md「舰内系统 v3」§3）：目标进度、验收通过率。
 * 270° 弧（从左下经顶到右下），外圈 24 格刻度，已完成的格点亮 accent；中央是读数与标签。
 * 两档尺寸：64（行内，标签不进圆心，只作无障碍名）与 96（面板里的主读数，标签在读数下方）。
 * 挂载时弧从 0 扫到当前值，更新时从旧值扫到新值：都是 stroke-dashoffset 的 240ms 过渡（CSS）。
 */
const START = 135; // 度，左下
const SWEEP = 270;
const TICKS = 24;

const rad = (deg: number) => (deg * Math.PI) / 180;
const pt = (cx: number, cy: number, r: number, deg: number) => [cx + r * Math.cos(rad(deg)), cy + r * Math.sin(rad(deg))] as const;
function arcPath(c: number, r: number) {
  const [x0, y0] = pt(c, c, r, START);
  const [x1, y1] = pt(c, c, r, START + SWEEP);
  return `M ${x0.toFixed(2)} ${y0.toFixed(2)} A ${r} ${r} 0 1 1 ${x1.toFixed(2)} ${y1.toFixed(2)}`;
}

export type GaugeTone = "accent" | "success" | "danger";
const TONE: Record<GaugeTone, string> = { accent: "var(--c-accent)", success: "var(--c-success)", danger: "var(--c-danger)" };

export function ArcGauge({ value, label, display, size = 64, tone = "accent", className, delay = 0 }: { value: number; label?: string; display?: string; size?: 64 | 96; tone?: GaugeTone; className?: string; delay?: number }) {
  const v = Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0));
  // 首帧画 0，下一帧再画真值：弧从 0 扫过来。reduced-motion 时 CSS 把过渡关掉，等于即时。
  // delay：目标刚"发芽"时仪表晚 480ms 再扫（提案「目标完成 · 发芽」：上级目标的仪表随后扫动）。
  const [shown, setShown] = useState(0);
  useEffect(() => {
    let raf = 0;
    const timer = window.setTimeout(() => {
      raf = window.requestAnimationFrame(() => setShown(v));
    }, delay);
    return () => {
      window.clearTimeout(timer);
      window.cancelAnimationFrame(raf);
    };
  }, [v, delay]);

  const c = size / 2;
  const stroke = size === 96 ? 4 : 3;
  const rArc = c - 9;
  const rTickOut = c - 1;
  const rTickIn = c - 5;
  const len = rad(SWEEP) * rArc;
  const lit = Math.floor((shown / 100) * TICKS + 1e-6);
  const text = display ?? `${Math.round(v)}%`;
  const id = useId();

  return (
    <svg
      className={cx("gauge", className)}
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      role="img"
      aria-labelledby={`${id}-t`}
      style={{ ["--gauge" as string]: TONE[tone] }}
    >
      <title id={`${id}-t`}>{label ? `${label} ${text}` : text}</title>
      {Array.from({ length: TICKS }, (_, i) => {
        const a = START + ((i + 0.5) * SWEEP) / TICKS;
        const [x0, y0] = pt(c, c, rTickIn, a);
        const [x1, y1] = pt(c, c, rTickOut, a);
        return <line key={i} className="gauge-tick" data-lit={i < lit ? "" : undefined} x1={x0.toFixed(2)} y1={y0.toFixed(2)} x2={x1.toFixed(2)} y2={y1.toFixed(2)} />;
      })}
      <path className="gauge-track" d={arcPath(c, rArc)} strokeWidth={stroke} />
      <path className="gauge-arc" d={arcPath(c, rArc)} strokeWidth={stroke} strokeDasharray={len} strokeDashoffset={len * (1 - shown / 100)} />
      <text className="gauge-value" x={c} y={size === 96 ? c + 2 : c + 1} textAnchor="middle" dominantBaseline="middle" fontSize={size === 96 ? 20 : 13}>
        {text}
      </text>
      {size === 96 && label && (
        <text className="gauge-label" x={c} y={c + 20} textAnchor="middle" dominantBaseline="middle">
          {label}
        </text>
      )}
    </svg>
  );
}
