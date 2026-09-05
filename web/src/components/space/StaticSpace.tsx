"use client";
import { useEffect, useRef } from "react";
import { HULL_D, KEEL_D, RIM_D, SUPER_D, svgLightsPath, VB_H, VB_W } from "./ship/hullProfile";

/**
 * 静帧：Canvas2D 星空（径向渐变星云 + 远日 + 按面积约 2000 颗星，密度与亮度对齐 WebGL 星场，交叉淡入几乎看不出）
 * + 可选的 SVG 母舰。
 * - WebGL 场景加载期间：只画深空（ship=false），卡片与文字已经在了，船稍后"驶入"；
 * - 没有 WebGL 2（tier none）：加上 SVG 母舰作为最终画面——这是唯一能看到船的方式。
 * 不跑 rAF。母舰 SVG 与 WebGL 版共用同一条轮廓（hullProfile.ts）。
 */
const LIGHTS_D = svgLightsPath();

export function StaticSpace({ ship = false }: { ship?: boolean }) {
  const ref = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const draw = () => {
      const dpr = Math.min(1.5, window.devicePixelRatio || 1);
      const w = canvas.clientWidth, h = canvas.clientHeight;
      if (!w || !h) return;
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      const ctx = canvas.getContext("2d");
      if (!ctx) return;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.fillStyle = "#010102";
      ctx.fillRect(0, 0, w, h);
      // 右上角一团靛蓝星云
      const neb = ctx.createRadialGradient(w * 0.86, h * 0.1, 0, w * 0.86, h * 0.1, Math.max(w, h) * 0.55);
      neb.addColorStop(0, "rgba(94,106,210,0.11)");
      neb.addColorStop(0.5, "rgba(74,58,92,0.05)");
      neb.addColorStop(1, "rgba(0,0,0,0)");
      ctx.fillStyle = neb;
      ctx.fillRect(0, 0, w, h);
      // 船尾一侧地平线外的暖色远日
      const sun = ctx.createRadialGradient(w * 0.2, h * 1.02, 0, w * 0.2, h * 1.02, Math.max(w, h) * 0.5);
      sun.addColorStop(0, "rgba(255,217,160,0.07)");
      sun.addColorStop(1, "rgba(255,217,160,0)");
      ctx.fillStyle = sun;
      ctx.fillRect(0, 0, w, h);
      // 星：固定种子；数量按面积（1440×900 约 2000 颗），三层尺寸 / 亮度对应 WebGL 的远 / 中 / 近层
      let a = 0x2f6b1a3d;
      const rng = () => {
        a = (a + 0x6d2b79f5) >>> 0;
        let t = a;
        t = Math.imul(t ^ (t >>> 15), t | 1);
        t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
        return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
      };
      const count = Math.round((w * h) / 650);
      for (let i = 0; i < count; i++) {
        const x = rng() * w, y = rng() * h;
        const layer = rng();
        const big = rng() < 0.04 ? 1.6 : 1;
        const r = (layer < 0.6 ? 0.55 + rng() * 0.35 : layer < 0.9 ? 0.75 + rng() * 0.45 : 1.0 + rng() * 0.6) * big;
        const alpha = layer < 0.6 ? 0.3 + rng() * 0.3 : layer < 0.9 ? 0.45 + rng() * 0.35 : 0.65 + rng() * 0.35;
        ctx.beginPath();
        ctx.arc(x, y, r, 0, Math.PI * 2);
        ctx.fillStyle = `rgba(226,231,255,${alpha.toFixed(3)})`;
        ctx.fill();
      }
    };
    draw();
    const ro = new ResizeObserver(draw);
    ro.observe(canvas);
    return () => ro.disconnect();
  }, []);

  return (
    <div className="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
      <canvas ref={ref} className="absolute inset-0 h-full w-full" />
      {ship && (
      <div className="ax-ship-layer">
        <svg className="ax-ship" viewBox={`0 0 ${VB_W} ${VB_H}`} overflow="visible">
          <defs>
            <radialGradient id="ax-engine-glow" cx="50%" cy="50%" r="50%">
              <stop offset="0" className="ax-eng-0" />
              <stop offset="0.45" className="ax-eng-1" />
              <stop offset="1" className="ax-eng-2" />
            </radialGradient>
            <linearGradient id="ax-hull-fill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0" className="ax-hull-0" />
              <stop offset="1" className="ax-hull-1" />
            </linearGradient>
            <linearGradient id="ax-super-fill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0" className="ax-super-0" />
              <stop offset="1" className="ax-hull-1" />
            </linearGradient>
            <filter id="ax-blur" x="-30%" y="-30%" width="160%" height="160%">
              <feGaussianBlur stdDeviation="7" />
            </filter>
          </defs>
          <ellipse className="ax-engine" cx="70" cy="278" rx="124" ry="70" fill="url(#ax-engine-glow)" />
          <path className="ax-fin" d="M 176 248 L 46 206 Q 30 212 40 232 L 176 278 Z" />
          <path className="ax-fin" d="M 176 312 L 46 354 Q 30 348 40 328 L 176 282 Z" />
          <path className="ax-nozzle" d="M 36 214 L 46 206 L 46 220 L 36 226 Z M 36 346 L 46 354 L 46 340 L 36 334 Z" />
          <path className="ax-hull" d={HULL_D} fill="url(#ax-hull-fill)" />
          <path className="ax-hull" d={SUPER_D} fill="url(#ax-super-fill)" />
          <path className="ax-seam" d={KEEL_D} />
          <path className="ax-rim" d={RIM_D} />
          <path className="ax-bow-glow" d="M 898 214 C 946 218 968 240 968 264 C 968 288 946 310 898 316" filter="url(#ax-blur)" />
          <path className="ax-bow-window" d="M 898 214 C 946 218 968 240 968 264 C 968 288 946 310 898 316" />
          <path className="ax-dome" d="M 738 138 A 14 14 0 0 1 766 138 Z" />
          <path className="ax-seam" d="M 752 124 V 100" />
          <circle className="ax-beacon" cx="752" cy="98" r="1.5" />
          <path className="ax-lights" d={LIGHTS_D} />
        </svg>
      </div>
      )}
    </div>
  );
}
