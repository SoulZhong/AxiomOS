"use client";
import { useCallback, useEffect, useState, type RefObject } from "react";
import { markKeyboardIntent } from "./motion";

/**
 * 让一块区域独占屏幕（DESIGN.md §8「全屏」）：优先浏览器 Fullscreen API；不可用或被拒绝时退到"应用内全屏"
 * （调用方给容器加 fixed inset-0 的样式，盖过侧栏与状态栏）。Esc 或再按一次退出；快捷键 F 由调用方绑定。
 */
export function useFullscreen(ref: RefObject<HTMLElement | null>): { active: boolean; native: boolean; toggle: () => void; exit: () => void } {
  const [native, setNative] = useState(false);
  const [inApp, setInApp] = useState(false);

  useEffect(() => {
    const onChange = () => setNative(!!document.fullscreenElement && document.fullscreenElement === ref.current);
    document.addEventListener("fullscreenchange", onChange);
    return () => document.removeEventListener("fullscreenchange", onChange);
  }, [ref]);

  // 应用内全屏：Esc 退出（键盘触发不动画）；对话框开着时让对话框先关
  useEffect(() => {
    if (!inApp) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || document.querySelector("dialog[open]")) return;
      e.preventDefault();
      markKeyboardIntent();
      setInApp(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [inApp]);

  const exit = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen?.().catch(() => {});
    setInApp(false);
  }, []);

  const toggle = useCallback(() => {
    if (native || inApp) {
      exit();
      return;
    }
    const el = ref.current;
    if (!el) return;
    const req = el.requestFullscreen;
    if (typeof req !== "function" || !document.fullscreenEnabled) {
      setInApp(true);
      return;
    }
    req.call(el).catch(() => setInApp(true));
  }, [native, inApp, exit, ref]);

  return { active: native || inApp, native, toggle, exit };
}
