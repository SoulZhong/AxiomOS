"use client";
// 显示偏好（DESIGN.md §20）：登录后拉一次 GET /me/preferences（个人 → 角色 → 默认，后端逐字段解析好），
// 存在模块级，组件用 usePreferences() 订阅；改过（我的偏好页保存 / 重置）就 applyPreferences 整体替换。
// 生效点：任务表的列（TaskTable）、看板卡片字段（Board）、进「任务」的默认页签（tasks/page）、紧凑密度（body[data-density]）、侧栏默认收起（sidebar.ts 的兜底）。
import { useSyncExternalStore } from "react";
import { api, DEFAULT_PREFERENCES, PREF_FIELDS, type Preferences, type PrefField, type PrefSource } from "./api";
import { setSidebarDefault } from "./sidebar";

export const FALLBACK_PREFERENCES: Preferences = {
  ...DEFAULT_PREFERENCES,
  source: "default",
  sources: Object.fromEntries(PREF_FIELDS.map((f) => [f, "default" as PrefSource])) as Record<PrefField, PrefSource>,
  roles_used: [],
  overrides: [],
};

interface State {
  prefs: Preferences;
  /** 已经从接口拿到过（失败也算，避免页面一直等） */
  loaded: boolean;
}
let state: State = { prefs: FALLBACK_PREFERENCES, loaded: false };
const listeners = new Set<() => void>();
const notify = () => listeners.forEach((fn) => fn());

function sideEffects(p: Preferences) {
  if (typeof document === "undefined") return;
  if (p.compact) document.body.dataset.density = "compact";
  else delete document.body.dataset.density;
  setSidebarDefault(p.sidebar_collapsed);
}

/** 整体替换（接口返回的已解析对象）并作用到页面。 */
export function applyPreferences(p: Preferences) {
  state = { prefs: p, loaded: true };
  sideEffects(p);
  notify();
}

/** 登录后拉一次；失败（老后端没有这个接口）就按默认值并标记已加载。 */
export async function loadPreferences(): Promise<void> {
  try {
    applyPreferences(await api.preferences.get());
  } catch {
    state = { prefs: FALLBACK_PREFERENCES, loaded: true };
    sideEffects(FALLBACK_PREFERENCES);
    notify();
  }
}

/** 退出登录：回到默认。 */
export function clearPreferences() {
  state = { prefs: FALLBACK_PREFERENCES, loaded: false };
  sideEffects(FALLBACK_PREFERENCES);
  notify();
}

export const getPreferences = () => state.prefs;

const subscribe = (cb: () => void) => {
  listeners.add(cb);
  return () => listeners.delete(cb);
};
const getSnapshot = () => state;
const getServer = () => state;

/** 当前已解析的偏好；loaded 为 false 时还是默认值（首屏水合阶段）。 */
export function usePreferences(): State {
  return useSyncExternalStore(subscribe, getSnapshot, getServer);
}
