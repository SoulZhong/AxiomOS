import { useSyncExternalStore } from "react";

/**
 * prefers-reduced-motion，随系统设置变化重渲染（matchMedia + useSyncExternalStore）。
 * 预渲染 / 水合阶段返回 false。
 */
const QUERY = "(prefers-reduced-motion: reduce)";

const subscribe = (cb: () => void) => {
  const mq = window.matchMedia(QUERY);
  mq.addEventListener("change", cb);
  return () => mq.removeEventListener("change", cb);
};

export function useReducedMotion(): boolean {
  return useSyncExternalStore(subscribe, () => window.matchMedia(QUERY).matches, () => false);
}
