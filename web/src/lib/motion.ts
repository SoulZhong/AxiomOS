"use client";
/**
 * 动效的两个小开关（web/DESIGN.md「入场与切换」）。
 *
 * 1. 键盘触发的动作不动画：快捷键（n / ? / g t / g g）和 Esc 会在 <html> 上打一枚 600ms 的 data-kbd 标记，
 *    globals.css 里 `html[data-kbd] …` 把抽屉 / 对话框 / 页面入场的过渡缩为 0ms；useNativeDialog 也据此立即关闭而不等过渡。
 * 2. 强 ease-out 曲线给 WAAPI 用（甘特图切换刻度时的一次 200ms 过渡），从 :root 的 --ease-out 读，不另写一份。
 */
const KBD_ATTR = "data-kbd";
let timer = 0;

/** 标记"这一下是键盘触发的"，600ms 内的抽屉 / 对话框 / 页面入场都不动画。 */
export function markKeyboardIntent(ms = 600) {
  const el = document.documentElement;
  el.setAttribute(KBD_ATTR, "");
  window.clearTimeout(timer);
  timer = window.setTimeout(() => el.removeAttribute(KBD_ATTR), ms);
}

export const keyboardIntent = () => document.documentElement.hasAttribute(KBD_ATTR);

export const prefersReducedMotion = () => window.matchMedia("(prefers-reduced-motion: reduce)").matches;

/** :root 上的 --ease-out（cubic-bezier(0.23, 1, 0.32, 1)）；读不到时退回同一条曲线的字面值。 */
export function easeOut(): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue("--ease-out").trim();
  return v || "cubic-bezier(0.23, 1, 0.32, 1)";
}

/*
 * 3. 领取任务的两段动效需要跨组件记住"刚领走了哪一块"（提案「领取任务 · 取走一块」）：
 *    - 领取成功时 rememberClaim(id) 并广播 `axiomos:claimed`（侧栏「待领取任务」图标取走一块）；
 *    - 之后 6s 内任何任务表里第一次出现这条任务，行从左侧滑入（TaskTable 读 recentlyClaimed）。
 */
export const CLAIM_EVENT = "axiomos:claimed";
const claimed = new Map<string, number>();
const CLAIM_TTL = 6_000;
export function rememberClaim(id: string) {
  claimed.set(id, Date.now());
  window.dispatchEvent(new CustomEvent(CLAIM_EVENT, { detail: { id } }));
}
export function recentlyClaimed(id: string): boolean {
  const at = claimed.get(id);
  if (at === undefined) return false;
  if (Date.now() - at > CLAIM_TTL) {
    claimed.delete(id);
    return false;
  }
  return true;
}
