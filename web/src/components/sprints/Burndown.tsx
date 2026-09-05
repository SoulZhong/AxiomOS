"use client";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { Burndown as BurndownData } from "@/lib/api";
import { fmtDate, parseDate, toISODate, today } from "@/lib/format";
import { t } from "@/lib/i18n";
import { useReducedMotion } from "@/lib/useReducedMotion";
import { Empty } from "@/components/ui";

/*
 * 燃尽图（SVG）：理想线 = 细虚线，实际线 = accent 1.5px + 6% 面积；x 轴按天、y 轴按接口给的单位（工作量 / 任务数）。
 * 宽度随容器（ResizeObserver），高度固定；悬停给一条竖线与当天读数。挂载时实际线描一次 240ms，reduced-motion 时即时。
 * 网格线 hairline、坐标轴文字等宽 11px（DESIGN.md「图表」）。
 */
const PAD = { l: 36, r: 12, t: 12, b: 26 };

export function Burndown({ data, height = 240, className }: { data: BurndownData; height?: number; className?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(720);
  const [hover, setHover] = useState<number | null>(null);
  const reduced = useReducedMotion();
  // 首帧带 data-draw 描一次线，300ms 后摘掉；reduced-motion 时不挂这个属性
  const [draw, setDraw] = useState(true);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setWidth(Math.max(240, Math.round(e.contentRect.width))));
    ro.observe(el);
    setWidth(Math.max(240, Math.round(el.getBoundingClientRect().width)));
    return () => ro.disconnect();
  }, []);
  useEffect(() => {
    const id = window.setTimeout(() => setDraw(false), 300);
    return () => window.clearTimeout(id);
  }, []);

  const ideal = data.ideal;
  if (ideal.length < 2) return <Empty text={t("sprint.burndown.empty")} illustration={false} className="py-6" />;
  const days = ideal.map((p) => p.date);
  const nDays = days.length;
  const max = Math.max(1, ...ideal.map((p) => p.value), ...data.actual.map((p) => p.value));
  const w = width - PAD.l - PAD.r;
  const h = height - PAD.t - PAD.b;
  const x = (i: number) => PAD.l + (i / (nDays - 1)) * w;
  const y = (v: number) => PAD.t + h - (v / max) * h;
  const idx = (date: string) => days.indexOf(date);
  const line = (pts: Array<{ date: string; value: number }>) => pts.map((p, i) => `${i === 0 ? "M" : "L"} ${x(idx(p.date)).toFixed(1)} ${y(p.value).toFixed(1)}`).join(" ");
  const actual = data.actual.filter((p) => idx(p.date) >= 0);
  const area = actual.length >= 2 ? `${line(actual)} L ${x(idx(actual[actual.length - 1].date)).toFixed(1)} ${(PAD.t + h).toFixed(1)} L ${x(idx(actual[0].date)).toFixed(1)} ${(PAD.t + h).toFixed(1)} Z` : "";
  const todayIdx = idx(toISODate(today()));
  // y 轴 4 档；x 轴标签按宽度隔几天显示一次
  const yTicks = [0, 0.25, 0.5, 0.75, 1].map((f) => Math.round(max * f));
  const every = Math.max(1, Math.ceil(nDays / Math.max(2, Math.floor(w / 64))));
  const unit = t(`sprint.unit.${data.unit}`);
  const last = actual[actual.length - 1];

  const onMove = (e: React.PointerEvent<SVGSVGElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    const px = e.clientX - r.left;
    const i = Math.round(((px - PAD.l) / w) * (nDays - 1));
    setHover(i >= 0 && i < nDays ? i : null);
  };
  const hi = hover;
  const hIdeal = hi !== null ? ideal[hi]?.value : undefined;
  const hActual = hi !== null ? actual.find((p) => idx(p.date) === hi)?.value : undefined;
  const tipLeft = hi !== null ? Math.min(width - 150, Math.max(0, x(hi) + 8)) : 0;

  return (
    <div ref={ref} className={`bn ${className ?? ""}`}>
      <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`${t("sprint.tab.burndown")} · ${unit}`} onPointerMove={onMove} onPointerLeave={() => setHover(null)}>
        {yTicks.map((v) => (
          <g key={v}>
            <line className="bn-grid" x1={PAD.l} x2={PAD.l + w} y1={y(v)} y2={y(v)} />
            <text className="bn-axis" x={PAD.l - 8} y={y(v)} textAnchor="end" dominantBaseline="middle">{v}</text>
          </g>
        ))}
        {days.map((d, i) => {
          const dd = parseDate(d);
          const show = i % every === 0 || i === nDays - 1;
          return (
            <g key={d}>
              <line className="bn-grid" x1={x(i)} x2={x(i)} y1={PAD.t + h} y2={PAD.t + h + (show ? 5 : 3)} />
              {show && <text className="bn-axis" x={x(i)} y={height - 6} textAnchor={i === 0 ? "start" : i === nDays - 1 ? "end" : "middle"}>{dd ? fmtDate(d) : d}</text>}
            </g>
          );
        })}
        {todayIdx >= 0 && <line className="bn-today" x1={x(todayIdx)} x2={x(todayIdx)} y1={PAD.t} y2={PAD.t + h} strokeDasharray="2 3" />}
        <path className="bn-ideal" d={line(ideal)} />
        {area && <path className="bn-area" d={area} />}
        {actual.length >= 2 && <path className="bn-actual" d={line(actual)} pathLength={1} data-draw={draw && !reduced ? "" : undefined} />}
        {last && (
          <>
            <circle className="bn-halo" cx={x(idx(last.date))} cy={y(last.value)} r={5} />
            <circle className="bn-dot" cx={x(idx(last.date))} cy={y(last.value)} r={2.2} />
          </>
        )}
        {hi !== null && <line className="bn-hover" x1={x(hi)} x2={x(hi)} y1={PAD.t} y2={PAD.t + h} />}
      </svg>
      {hi !== null && (
        <div className="bn-tip" style={{ left: tipLeft }}>
          <div>{fmtDate(days[hi])}{hi === todayIdx ? ` · ${t("sprint.burndown.today")}` : ""}</div>
          {hActual !== undefined && <div>{t("sprint.burndown.actual")} <b>{hActual}</b> {unit}</div>}
          {hIdeal !== undefined && <div>{t("sprint.burndown.ideal")} <b>{hIdeal}</b> {unit}</div>}
        </div>
      )}
      <div className="bn-legend">
        <span><i />{t("sprint.burndown.actual")}{last ? ` · ${t("sprint.burndown.remaining")} ${last.value} ${unit}` : ""}</span>
        <span><i data-ideal="" />{t("sprint.burndown.ideal")}</span>
        <span className="ml-auto">{t("sprint.burndown.caption", { unit })}</span>
      </div>
    </div>
  );
}
