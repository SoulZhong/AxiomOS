"use client";
// 范围的 React 一侧（存储在 src/lib/scope.ts）。
import { useSyncExternalStore } from "react";
import { getScopeState, getServerScopeState, subscribeScope, type ScopeState } from "./scope";
import { t } from "./i18n";

export { SCOPE_ALL, setScope, nearestBoundary, applySession as applyScopeSession } from "./scope";
export type { ScopeState } from "./scope";

/** 当前范围（切换时组件重新渲染，useLoad 也会重新取数）。 */
export function useScopeState(): ScopeState {
  return useSyncExternalStore(subscribeScope, getScopeState, getServerScopeState);
}

/**
 * 成本被藏起来时给的那一句话：按组织的财务可见性策略选词条，不拼字符串。
 * 按共享边界时点出是哪个共享域；策略未知（后端没在会话里给）时用中性的那一句。
 */
export function financeHiddenText(state: ScopeState): string {
  if (state.financeVisibility === "boundary" && state.boundaryName) return t("scope.finance.hiddenBoundary", { name: state.boundaryName });
  if (state.financeVisibility === "team_tree") return t("scope.finance.hiddenTeamTree");
  return t("scope.finance.hidden");
}
