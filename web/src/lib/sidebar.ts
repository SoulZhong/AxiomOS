"use client";
// 侧栏收起 / 展开：存在 localStorage "axiomos.sidebar"（"collapsed" | "expanded"），作用到 <html data-sidebar>。
// - 没有偏好时按断点：≥1200 展开，768–1199 收成 56px 图标栏，<768 是抽屉导航（不受影响）。
// - 显式偏好在 ≥768 时覆盖断点。首屏由 layout.tsx 的内联脚本（lib/boot.ts）先写一次属性（与主题同一套做法），避免先闪出展开态。
// - 宽度、标签显隐全部由 CSS 按 html[data-sidebar] + 媒体查询决定（globals.css「侧栏收起」），React 只负责切换与订阅。
import { useSyncExternalStore } from "react";
import { SIDEBAR_KEY } from "./boot";

export type SidebarPref = "collapsed" | "expanded" | null;
export { SIDEBAR_KEY };

const isPref = (v: unknown): v is Exclude<SidebarPref, null> => v === "collapsed" || v === "expanded";

let current: SidebarPref = null;
let resolved = false;
const listeners = new Set<() => void>();

export function getSidebarPref(): SidebarPref {
  if (!resolved && typeof window !== "undefined") {
    try {
      const v = window.localStorage.getItem(SIDEBAR_KEY);
      current = isPref(v) ? v : null;
    } catch {
      current = null;
    }
    resolved = true;
  }
  return current;
}

/** 当前实际是否收起：显式偏好优先，否则按断点。 */
export function isSidebarCollapsed(): boolean {
  const p = getSidebarPref();
  if (p) return p === "collapsed";
  return typeof window !== "undefined" ? !window.matchMedia("(min-width: 1200px)").matches : false;
}

export function setSidebarPref(p: Exclude<SidebarPref, null>) {
  resolved = true;
  current = p;
  document.documentElement.setAttribute("data-sidebar", p);
  try {
    window.localStorage.setItem(SIDEBAR_KEY, p);
  } catch {
    /* 隐私模式下 localStorage 不可用：本次会话仍然生效 */
  }
  listeners.forEach((fn) => fn());
}

export function toggleSidebar() {
  setSidebarPref(isSidebarCollapsed() ? "expanded" : "collapsed");
}

const subscribe = (cb: () => void) => {
  listeners.add(cb);
  const mq = window.matchMedia("(min-width: 1200px)");
  mq.addEventListener("change", cb);
  return () => {
    listeners.delete(cb);
    mq.removeEventListener("change", cb);
  };
};

/** 当前是否收起（服务端与水合阶段按展开算；真正的首屏样式由 CSS + 内联脚本决定，不等 React）。 */
export function useSidebarCollapsed(): boolean {
  return useSyncExternalStore(subscribe, isSidebarCollapsed, () => false);
}
