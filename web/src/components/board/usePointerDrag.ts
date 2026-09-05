"use client";
import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";

/*
 * 指针拖放（看板卡片与迭代待办共用），不依赖任何库。
 * - pointerdown 记下起点；移动超过 4px 才算拖，之前抬起算点击（onClick）。
 * - 拖动时把元素的 pointer capture 抓住（离开元素边界仍然收到事件），落点用 elementsFromPoint 找最近的 [data-drop]。
 * - 影子卡由调用方渲染：drag.rect 给尺寸与起点，drag.dx / dy 给位移，只动 transform。
 * - 第二根手指 / 已在拖时忽略；Esc 取消；拖动期间 html[data-bd-dragging] 锁住光标与选区。
 */
export interface DragState {
  id: string;
  rect: { x: number; y: number; w: number; h: number };
  dx: number;
  dy: number;
  /** 当前悬停的落点（[data-drop] 的值），没有则 null */
  over: string | null;
}

export function usePointerDrag({ onDrop, onClick, dropSelector = "[data-drop]" }: { onDrop: (id: string, target: string | null) => void; onClick?: (id: string) => void; dropSelector?: string }) {
  const [drag, setDrag] = useState<DragState | null>(null);
  const live = useRef<{ id: string; x0: number; y0: number; el: HTMLElement; pointerId: number; active: boolean; over: string | null; raf: number } | null>(null);
  const cbs = useRef({ onDrop, onClick });
  useEffect(() => {
    cbs.current = { onDrop, onClick };
  });

  const findTarget = useCallback(
    (x: number, y: number): string | null => {
      for (const el of document.elementsFromPoint(x, y)) {
        const hit = (el as HTMLElement).closest?.(dropSelector) as HTMLElement | null;
        if (hit) return hit.dataset.drop ?? null;
      }
      return null;
    },
    [dropSelector],
  );

  const finish = useCallback((commit: boolean) => {
    const d = live.current;
    if (!d) return;
    live.current = null;
    window.cancelAnimationFrame(d.raf);
    try {
      d.el.releasePointerCapture(d.pointerId);
    } catch {
      /* 元素已卸载 */
    }
    delete document.documentElement.dataset.bdDragging;
    setDrag(null);
    if (d.active) {
      if (commit) cbs.current.onDrop(d.id, d.over);
    } else if (commit) {
      cbs.current.onClick?.(d.id);
    }
  }, []);

  useEffect(() => {
    const onMove = (e: PointerEvent) => {
      const d = live.current;
      if (!d || e.pointerId !== d.pointerId) return;
      const dx = e.clientX - d.x0, dy = e.clientY - d.y0;
      if (!d.active) {
        if (Math.hypot(dx, dy) < 4) return;
        d.active = true;
        document.documentElement.dataset.bdDragging = "";
        const r = d.el.getBoundingClientRect();
        setDrag({ id: d.id, rect: { x: r.left, y: r.top, w: r.width, h: r.height }, dx, dy, over: null });
      }
      window.cancelAnimationFrame(d.raf);
      d.raf = window.requestAnimationFrame(() => {
        const over = findTarget(e.clientX, e.clientY);
        d.over = over;
        setDrag((s) => (s ? { ...s, dx, dy, over } : s));
      });
    };
    const onUp = (e: PointerEvent) => {
      const d = live.current;
      if (!d || e.pointerId !== d.pointerId) return;
      finish(true);
    };
    const onCancel = (e: PointerEvent) => {
      const d = live.current;
      if (!d || e.pointerId !== d.pointerId) return;
      finish(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && live.current?.active) {
        e.preventDefault();
        finish(false);
      }
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onCancel);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      window.removeEventListener("keydown", onKey);
      finish(false);
    };
  }, [findTarget, finish]);

  /** 挂在可拖元素的 onPointerDown 上。 */
  const start = useCallback((e: ReactPointerEvent<HTMLElement>, id: string) => {
    if (live.current || e.button !== 0) return;
    // 卡片里的链接 / 按钮自己处理点击，不从它们上面起拖
    if ((e.target as HTMLElement).closest("a, button, input, select, textarea")) return;
    const el = e.currentTarget;
    live.current = { id, x0: e.clientX, y0: e.clientY, el, pointerId: e.pointerId, active: false, over: null, raf: 0 };
    try {
      el.setPointerCapture(e.pointerId);
    } catch {
      /* 某些环境不支持 */
    }
  }, []);

  return { drag, start };
}
