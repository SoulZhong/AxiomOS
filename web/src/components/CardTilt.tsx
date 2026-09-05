"use client";
import { useEffect, useRef, type ReactNode } from "react";

/**
 * 登舰面板的轻微倾斜：随光标最多 4°（面板"面向"光标——靠近光标的一侧略向后），阻尼 λ 6，
 * 面板上的微光高光跟着光标走。只在 (hover: hover) and (pointer: fine) 且未开启减少动效时启用；
 * 只写四个 CSS 变量（globals.css 的 .card-tilt），没有依赖。光标离开窗口时回正。
 * 光标进入卡片内部、或卡片内有元素获得焦点时，倾斜归零并保持静止：密码管理器（如 Enpass）的弹窗锚定在
 * 输入框的位置上，几何一直在动会让它不停重定位而闪烁。此时只有高光继续跟随光标。
 */
const MAX_DEG = 4;

export function CardTilt({ children, className }: { children: ReactNode; className?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (!window.matchMedia("(hover: hover) and (pointer: fine)").matches || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    let tx = 0, ty = 0, cx = 0, cy = 0, gx = 50, gy = 50, go = 0, cgx = 50, cgy = 50, cgo = 0;
    let raf = 0, last = 0;
    let inside = false, focused = el.contains(document.activeElement);
    const tick = (now: number) => {
      raf = 0;
      const dt = Math.min((now - (last || now)) / 1000, 1 / 30);
      last = now;
      const k = 1 - Math.exp(-6 * dt);
      cx += (tx - cx) * k;
      cy += (ty - cy) * k;
      cgx += (gx - cgx) * k;
      cgy += (gy - cgy) * k;
      cgo += (go - cgo) * k;
      el.style.setProperty("--tilt-x", `${cx.toFixed(3)}deg`);
      el.style.setProperty("--tilt-y", `${cy.toFixed(3)}deg`);
      el.style.setProperty("--glow-x", `${cgx.toFixed(2)}%`);
      el.style.setProperty("--glow-y", `${cgy.toFixed(2)}%`);
      el.style.setProperty("--glow-o", cgo.toFixed(3));
      if (Math.abs(tx - cx) + Math.abs(ty - cy) + Math.abs(go - cgo) + Math.abs(gx - cgx) * 0.01 > 0.005) raf = requestAnimationFrame(tick);
      else last = 0;
    };
    const schedule = () => {
      if (!raf) raf = requestAnimationFrame(tick);
    };
    const onMove = (e: PointerEvent) => {
      const r = el.getBoundingClientRect();
      if (!r.width) return;
      const px = (e.clientX - (r.left + r.width / 2)) / (r.width / 2);
      const py = (e.clientY - (r.top + r.height / 2)) / (r.height / 2);
      // 面板面向光标：光标在右，右侧略向后（rotateY 正）；光标在上，上侧略向后（rotateX 正）
      ty = Math.max(-1, Math.min(1, px)) * MAX_DEG;
      tx = -Math.max(-1, Math.min(1, py)) * MAX_DEG;
      gx = Math.max(-40, Math.min(140, ((e.clientX - r.left) / r.width) * 100));
      gy = Math.max(-40, Math.min(140, ((e.clientY - r.top) / r.height) * 100));
      const near = Math.max(Math.abs(px), Math.abs(py));
      go = near < 1 ? 1 : Math.max(0, 1 - (near - 1) / 1.5);
      inside = near <= 1.02;
      if (inside || focused) tx = ty = 0;
      schedule();
    };
    const onFocusIn = () => {
      focused = true;
      tx = ty = 0;
      schedule();
    };
    const onFocusOut = (e: FocusEvent) => {
      focused = e.relatedTarget instanceof Node && el.contains(e.relatedTarget);
    };
    const onLeave = () => {
      tx = ty = 0;
      go = 0;
      schedule();
    };
    window.addEventListener("pointermove", onMove, { passive: true });
    el.addEventListener("focusin", onFocusIn);
    el.addEventListener("focusout", onFocusOut);
    document.documentElement.addEventListener("mouseleave", onLeave);
    window.addEventListener("blur", onLeave);
    return () => {
      window.removeEventListener("pointermove", onMove);
      el.removeEventListener("focusin", onFocusIn);
      el.removeEventListener("focusout", onFocusOut);
      document.documentElement.removeEventListener("mouseleave", onLeave);
      window.removeEventListener("blur", onLeave);
      if (raf) cancelAnimationFrame(raf);
    };
  }, []);
  return (
    <div ref={ref} className={className}>
      {children}
    </div>
  );
}
