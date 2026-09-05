"use client";
// 主题：系统 / 深色 / 浅色。存在 localStorage "axiomos.theme"；作用到 <html data-theme>。
// - "dark" / "light" 写成 data-theme，CSS 里 :root[data-theme=...] 换整套 token 值；
// - "system" 不写属性，由 @media (prefers-color-scheme) 决定，系统切换时浏览器自己重算，不需要 JS 监听。
// - 首屏由 layout.tsx 里的内联脚本（lib/boot.ts）先读一次（避免闪一下深色），这里只负责之后的切换与 React 侧订阅。
import { useSyncExternalStore } from "react";
import { THEME_KEY } from "./boot";

export type Theme = "system" | "dark" | "light";
export const THEMES: Theme[] = ["system", "dark", "light"];
export { THEME_KEY };

const isTheme = (v: unknown): v is Theme => v === "system" || v === "dark" || v === "light";

let current: Theme = "system";
let resolved = false;
const listeners = new Set<() => void>();

// 首屏内联脚本在 lib/boot.ts（不能放在这个 "use client" 模块里，见那里的说明）。

export function getTheme(): Theme {
  if (!resolved && typeof window !== "undefined") {
    try {
      const v = window.localStorage.getItem(THEME_KEY);
      current = isTheme(v) ? v : "system";
    } catch {
      current = "system";
    }
    resolved = true;
  }
  return current;
}

function apply(t: Theme) {
  const el = document.documentElement;
  if (t === "system") el.removeAttribute("data-theme");
  else el.setAttribute("data-theme", t);
}

export function setTheme(t: Theme) {
  resolved = true;
  if (t === current) return;
  current = t;
  apply(t);
  try {
    if (t === "system") window.localStorage.removeItem(THEME_KEY);
    else window.localStorage.setItem(THEME_KEY, t);
  } catch {
    /* 隐私模式等情况下 localStorage 不可用：本次会话仍然生效 */
  }
  listeners.forEach((fn) => fn());
}

const subscribe = (cb: () => void) => {
  listeners.add(cb);
  return () => listeners.delete(cb);
};

/** 当前主题偏好（服务端渲染与水合阶段为 "system"）。 */
export function useTheme(): [Theme, (t: Theme) => void] {
  const theme = useSyncExternalStore(subscribe, getTheme, () => "system" as Theme);
  return [theme, setTheme];
}
