"use client";
import { useEffect, useRef } from "react";

/**
 * Canvas2D 星海：两层视差的软圆星点缓慢向左流过（远层慢、近层快），密度与亮度对齐登录页的 StaticSpace（每 650px² 约一颗）。
 * 舷窗带（32px 高的状态栏）与「舰况」面板的舱外背景共用。固定种子，尺寸变化时重铺。
 * 标签页隐藏时停帧；prefers-reduced-motion 时只画一张静帧。speed 是漂移倍率（1 = 远层 2.5px/s、近层 6px/s）。
 */
export function StarBand({ className, speed = 1, density = 1 }: { className?: string; speed?: number; density?: number }) {
  const ref = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    // 软圆星点：一张 32px 的径向渐变贴图，按半径缩放绘制（arc 的边缘太硬）
    const sprite = document.createElement("canvas");
    sprite.width = sprite.height = 32;
    const sc = sprite.getContext("2d")!;
    const g = sc.createRadialGradient(16, 16, 0, 16, 16, 16);
    g.addColorStop(0, "rgba(226,231,255,1)");
    g.addColorStop(0.35, "rgba(226,231,255,0.7)");
    g.addColorStop(0.7, "rgba(226,231,255,0.12)");
    g.addColorStop(1, "rgba(226,231,255,0)");
    sc.fillStyle = g;
    sc.fillRect(0, 0, 32, 32);

    type Star = { x: number; y: number; r: number; a: number; v: number };
    let stars: Star[] = [];
    let w = 0, h = 0, dpr = 1;
    const build = () => {
      dpr = Math.min(1.5, window.devicePixelRatio || 1);
      w = canvas.clientWidth;
      h = canvas.clientHeight;
      canvas.width = Math.max(1, Math.round(w * dpr));
      canvas.height = Math.max(1, Math.round(h * dpr));
      let a = 0x2f6b1a3d;
      const rng = () => {
        a = (a + 0x6d2b79f5) >>> 0;
        let t = a;
        t = Math.imul(t ^ (t >>> 15), t | 1);
        t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
        return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
      };
      const count = Math.round(((w * h) / 650) * density);
      stars = [];
      for (let i = 0; i < count; i++) {
        const near = rng() < 0.35;
        const big = rng() < 0.04 ? 1.6 : 1;
        stars.push({
          x: rng() * w,
          y: rng() * h,
          r: (near ? 0.85 + rng() * 0.55 : 0.55 + rng() * 0.35) * big,
          a: near ? 0.5 + rng() * 0.35 : 0.28 + rng() * 0.3,
          v: (near ? 6 : 2.5) * speed,
        });
      }
    };
    const draw = (t: number) => {
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      const pad = 4;
      const span = w + pad * 2;
      for (const s of stars) {
        const x = ((((s.x - t * s.v) % span) + span) % span) - pad;
        const d = s.r * 4.4;
        ctx.globalAlpha = s.a;
        ctx.drawImage(sprite, x - d / 2, s.y - d / 2, d, d);
      }
      ctx.globalAlpha = 1;
    };
    let raf = 0;
    let running = false;
    const t0 = performance.now();
    const loop = (now: number) => {
      raf = 0;
      if (!running) return;
      draw((now - t0) / 1000);
      raf = requestAnimationFrame(loop);
    };
    const start = () => {
      if (reduced) {
        draw(0);
        return;
      }
      if (running) return;
      running = true;
      raf = requestAnimationFrame(loop);
    };
    const stop = () => {
      running = false;
      if (raf) cancelAnimationFrame(raf);
      raf = 0;
    };
    const onVisible = () => (document.hidden ? stop() : start());
    build();
    start();
    const ro = new ResizeObserver(() => {
      build();
      if (reduced) draw(0);
    });
    ro.observe(canvas);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      stop();
      ro.disconnect();
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [speed, density]);
  return <canvas ref={ref} className={className} aria-hidden="true" />;
}
