"use client";
import { useEffect, useLayoutEffect, useState, type CSSProperties, type RefObject } from "react";
import { markKeyboardIntent } from "@/lib/motion";
import { useFullscreen } from "@/lib/useFullscreen";

const isEditable = (el: EventTarget | null) => el instanceof HTMLElement && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT" || el.isContentEditable);

/**
 * 甘特图的外壳：全屏（F 切换，Esc 退出；浏览器不给全屏时退到应用内全屏）+ 图区高度 = 视口剩余高度。
 * measureDeps 变化（数据到了、页签切换）时重新量一次外壳距页面顶部的距离。
 * 用法：const shellRef = useRef<HTMLDivElement>(null); const shell = useGanttShell(shellRef, [data]);
 *       <div ref={shellRef} className={cx("gantt-shell", shell.inApp && "gantt-shell-fs")} style={shell.style}>…</div>
 * （ref 由调用方持有、不放进返回值，渲染期读返回值才不会被 lint 当成读 ref。）
 */
export function useGanttShell(ref: RefObject<HTMLDivElement | null>, measureDeps: unknown[]) {
  const fs = useFullscreen(ref);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "f" || e.metaKey || e.ctrlKey || e.altKey || isEditable(e.target) || document.querySelector("dialog[open]")) return;
      e.preventDefault();
      markKeyboardIntent();
      fs.toggle();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [fs]);

  const [top, setTop] = useState(0);
  useLayoutEffect(() => {
    const measure = () => {
      const el = ref.current;
      if (!el || fs.active) return;
      setTop(Math.round(el.getBoundingClientRect().top + window.scrollY));
    };
    measure();
    window.addEventListener("resize", measure);
    const ro = new ResizeObserver(measure);
    if (ref.current?.parentElement) ro.observe(ref.current.parentElement);
    return () => {
      window.removeEventListener("resize", measure);
      ro.disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fs.active, ref, ...measureDeps]);

  const style: CSSProperties | undefined = fs.active ? undefined : { height: `calc(100vh - ${top}px - 24px)` };
  return { fs, inApp: fs.active && !fs.native, style };
}
