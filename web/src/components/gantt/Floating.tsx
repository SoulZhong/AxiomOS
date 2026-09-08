"use client";
import { useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { cx } from "@/components/ui";

/**
 * fixed 定位的浮层：贴着锚点上方 / 下方居中，量完自己的尺寸后夹进视口（不被甘特图或路线图的框裁掉）。
 * 目标甘特图的里程碑气泡与路线图的条 / 菱形气泡共用它（ADR 0016、0022）。
 */
export function Floating({
  rect,
  place,
  className,
  children,
  role,
  innerRef,
  "aria-label": ariaLabel,
}: {
  rect: DOMRect;
  place: "top" | "bottom";
  className?: string;
  children: ReactNode;
  role?: string;
  innerRef?: RefObject<HTMLDivElement | null>;
  "aria-label"?: string;
}) {
  const own = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  useLayoutEffect(() => {
    const el = own.current;
    if (!el) return;
    const w = el.offsetWidth;
    const h = el.offsetHeight;
    const pad = 8;
    const left = Math.min(window.innerWidth - pad - w, Math.max(pad, rect.left + rect.width / 2 - w / 2));
    let top = place === "top" ? rect.top - h - 8 : rect.bottom + 8;
    if (place === "top" && top < pad) top = rect.bottom + 8;
    if (place === "bottom" && top + h > window.innerHeight - pad) top = Math.max(pad, rect.top - h - 8);
    setPos({ left, top });
  }, [rect, place]);
  return (
    <div
      ref={(el) => {
        own.current = el;
        if (innerRef) innerRef.current = el;
      }}
      className={cx("gantt-float", className)}
      role={role}
      aria-label={ariaLabel}
      style={pos ? { left: pos.left, top: pos.top } : { left: rect.left, top: rect.top, visibility: "hidden" }}
    >
      {children}
    </div>
  );
}
