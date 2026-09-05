"use client";
// 范围（CONTEXT.md「范围」、ADR 0013）：你正在看的是全公司还是某个团队（含其全部下级团队）。
//
// 这里只放"当前选的是哪一档"这一小段全局状态：
//   - 可选的档位与每一档能不能看财务数据，全部以 GET /auth/me 的 scopes[] 为准，前端不自己推算；
//   - 选择记在 localStorage（axiomos.scope），刷新后还在；
//   - src/lib/api.ts 在所有列表与统计请求上带 scope=，src/lib/hooks.ts 的 useLoad 把它算进依赖，
//     所以切换范围时当前页面会自动重新取数。
//
// 可见性是每个组织自己配的策略（全员可见 / 按共享边界 / 只看自己团队及下级），所以界面不假定任何一种：
// 能选哪些档、哪一档看得到钱都读后端给的；成本被藏起来时说哪一句话，按策略选词条
// （按共享边界时还要说出是哪个共享域，域名从会话的团队树上算：自己团队向上最近的那个边界）。
import type { ScopeOption, Session, Team, VisibilityPolicy } from "./api";

/** 全公司这一档的 id（docs/api.md「范围与概览」）。 */
export const SCOPE_ALL = "all";
export const SCOPE_STORAGE_KEY = "axiomos.scope";

export interface ScopeState {
  /** 当前范围：SCOPE_ALL 或团队 id */
  scope: string;
  /** 可切的档位，按会话给的顺序（含缩进层级） */
  options: ScopeOption[];
  /** 当前这一档能不能看财务数据；没有档位信息时先当作能看（等会话到达再纠正） */
  financial: boolean;
  /** 组织的财务可见性策略，决定"看不到成本"时给哪一句理由 */
  financeVisibility: VisibilityPolicy | null;
  /** 自己所在的共享域（自己团队向上最近的那个边界）的名字，没有就是 null */
  boundaryName: string | null;
}

const SERVER_STATE: ScopeState = { scope: SCOPE_ALL, options: [], financial: true, financeVisibility: null, boundaryName: null };

function readStored(): string | null {
  try {
    const raw = localStorage.getItem(SCOPE_STORAGE_KEY);
    return raw && raw.trim() ? raw : null;
  } catch {
    return null;
  }
}

// 模块加载时就把存过的选择读回来：/auth/me 之后的第一批请求（任务、目标、统计）就已经带上正确的范围。
let state: ScopeState = typeof window === "undefined" ? SERVER_STATE : { ...SERVER_STATE, scope: readStored() ?? SCOPE_ALL };

const listeners = new Set<() => void>();
const emit = () => listeners.forEach((fn) => fn());
export const subscribeScope = (cb: () => void) => {
  listeners.add(cb);
  return () => listeners.delete(cb);
};
export const getScopeState = () => state;
export const getServerScopeState = () => SERVER_STATE;
/** 给 api.ts 用：当前请求要带的 scope。 */
export const getScope = () => state.scope;

const optionOf = (options: ScopeOption[], id: string) => options.find((o) => o.id === id) ?? null;

function commit(next: ScopeState) {
  const same =
    next.scope === state.scope &&
    next.options === state.options &&
    next.financial === state.financial &&
    next.financeVisibility === state.financeVisibility &&
    next.boundaryName === state.boundaryName;
  if (same) return;
  state = next;
  emit();
}

/** 切换范围（选择器调用）：立刻生效并记住；不在可选列表里的 id 忽略。 */
export function setScope(id: string) {
  if (state.options.length && !optionOf(state.options, id)) return;
  try {
    localStorage.setItem(SCOPE_STORAGE_KEY, id);
  } catch {
    /* 隐私模式下只留在内存里 */
  }
  commit({ ...state, scope: id, financial: optionOf(state.options, id)?.financial ?? state.financial });
}

/** 自己所在的共享域：从自己的团队沿上级往上找最近的那个边界（自己的团队也算），找不到返回 null。 */
export function nearestBoundary(teams: Team[], teamId: string | null | undefined): Team | null {
  const byId = new Map(teams.map((x) => [x.id, x]));
  let cur = teamId ? byId.get(teamId) : undefined;
  const seen = new Set<string>();
  while (cur && !seen.has(cur.id)) {
    if (cur.is_boundary) return cur;
    seen.add(cur.id);
    cur = cur.parent_id ? byId.get(cur.parent_id) : undefined;
  }
  return null;
}

/**
 * 会话到达时对齐：档位列表、默认档、财务策略都以后端为准。
 * 存过的选择还在列表里就继续用它，否则回到 default_scope（没有就是全公司）。
 */
export function applySession(session: Session | null) {
  if (!session) {
    commit({ ...SERVER_STATE, scope: state.scope });
    return;
  }
  const options = session.scopes ?? [];
  const fallback = session.default_scope ?? SCOPE_ALL;
  const stored = readStored();
  let scope = state.scope;
  if (!options.length) scope = stored ?? fallback;
  else if (stored && optionOf(options, stored)) scope = stored;
  // 没有存过选择时以会话的 default_scope 为准（有团队的人默认自己的团队），
  // 不能沿用会话到达前的内存默认值「全公司」——那会让按边界看财务的人一进来就撞 403
  else if (optionOf(options, fallback)) scope = fallback;
  else if (optionOf(options, scope)) scope = state.scope;
  else scope = options[0].id;
  commit({
    scope,
    options,
    financial: optionOf(options, scope)?.financial ?? true,
    financeVisibility: session.settings?.finance_visibility ?? null,
    boundaryName: nearestBoundary(session.teams ?? [], session.member.team_id)?.name ?? null,
  });
}
