"use client";
import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { usePathname } from "next/navigation";
import { api, ApiError, type Agent, type ExecutorRef, type Member } from "./api";
import { t } from "./i18n";
import { useScopeState } from "./useScope";
import { useSession } from "@/components/AppShell";
import { useToast } from "@/components/toast";

export interface Loadable<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  reload: () => void;
}

interface LoadResult<T> {
  key: string;
  data: T | null;
  error: string | null;
}

/**
 * 在客户端加载一段数据；deps 变化时重新加载。
 * loading 由"结果对应的 key 是否等于当前 key"推出，重新加载期间保留上一次的数据（甘特图切换分组时不闪）。
 */
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[]): Loadable<T> {
  const [tick, setTick] = useState(0);
  // 范围也是依赖：切换范围时每个在加载数据的页面都重新取一次（请求上的 scope= 由 api.ts 带，ADR 0013）
  const { scope } = useScopeState();
  const key = `${JSON.stringify(deps)}#${tick}#${scope}`;
  const [result, setResult] = useState<LoadResult<T>>({ key: "", data: null, error: null });

  useEffect(() => {
    let alive = true;
    fn().then(
      (data) => {
        if (alive) setResult({ key, data, error: null });
      },
      (e: unknown) => {
        if (alive) setResult((prev) => ({ key, data: prev.data, error: errorMessage(e) }));
      },
    );
    return () => {
      alive = false;
    };
    // fn 故意不进依赖：调用方每次渲染都会传新的闭包，只按 key 触发。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  const reload = useCallback(() => setTick((t) => t + 1), []);
  const loading = result.key !== key;
  return { data: result.data, error: loading ? null : result.error, loading, reload };
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message || t("common.error");
  return t("common.error");
}

/** 本页自己用 replaceState 改地址栏时广播这一事件，让 useQueryParam 的订阅者跟着重渲染（popstate 只在前进 / 后退时触发） */
const LOCATION_EVENT = "axiomos:location";
const subscribeLocation = (cb: () => void) => {
  window.addEventListener("popstate", cb);
  window.addEventListener(LOCATION_EVENT, cb);
  return () => {
    window.removeEventListener("popstate", cb);
    window.removeEventListener(LOCATION_EVENT, cb);
  };
};

/**
 * 改地址栏查询串（不刷新、不入历史）：patch 里的空值删掉这个参数，其余原样保留。
 * 页签与筛选都写在 URL 里（DESIGN.md §10：切页签不丢筛选，可分享）。
 */
export function setQueryParams(patch: Record<string, string | null | undefined>) {
  const url = new URL(window.location.href);
  for (const [k, v] of Object.entries(patch)) {
    if (v === null || v === undefined || v === "") url.searchParams.delete(k);
    else url.searchParams.set(k, v);
  }
  const next = url.pathname + (url.searchParams.toString() ? `?${url.searchParams.toString()}` : "") + url.hash;
  if (next === window.location.pathname + window.location.search + window.location.hash) return;
  window.history.replaceState(window.history.state, "", next);
  window.dispatchEvent(new Event(LOCATION_EVENT));
}
const lastSegment = (p: string) => {
  const segs = p.split("/").filter(Boolean);
  return segs[segs.length - 1] ?? "";
};

/**
 * 动态路由的 id。静态导出时 /tasks/[id] 只预渲染了占位页 "_"，
 * 首屏 usePathname() 可能给的是占位值，所以占位时改从浏览器地址栏取；
 * 服务端渲染时返回 null，页面显示加载中，避免水合不一致。
 */
export function useRouteId(): string | null {
  const pathname = usePathname() ?? "";
  const loc = useSyncExternalStore(subscribeLocation, () => window.location.pathname, () => null);
  if (loc === null) return null;
  const fromRouter = lastSegment(pathname);
  const seg = fromRouter && fromRouter !== "_" ? fromRouter : lastSegment(loc);
  return seg && seg !== "_" ? decodeURIComponent(seg) : null;
}

/** 读取地址栏 query（静态导出下不用 useSearchParams，省掉 Suspense 边界）。服务端为 null。 */
export function useQueryParam(name: string): string | null {
  const search = useSyncExternalStore(subscribeLocation, () => window.location.search, () => null);
  if (search === null) return null;
  return new URLSearchParams(search).get(name);
}

/** 能力标签界面名（代码名 -> 当前语言）：优先会话里的 capability_titles，没有再取 GET /capabilities；都失败时界面退回显示代码名。 */
export function useCapabilityTitles(): Record<string, string> {
  const { session } = useSession();
  const fromSession = session?.capability_titles;
  const r = useLoad(() => (fromSession ? Promise.resolve(fromSession) : api.capabilities.list().catch(() => ({}) as Record<string, string>)), [fromSession]);
  return fromSession ?? r.data ?? {};
}

/** 全部执行者（成员 + Agent），用于指派、筛选。 */
export function useExecutors(): Loadable<{ members: Member[]; agents: Agent[]; executors: ExecutorRef[] }> {
  return useLoad(async () => {
    const [members, agents] = await Promise.all([api.members.list(), api.agents.list()]);
    const executors: ExecutorRef[] = [
      ...members.map((m): ExecutorRef => ({ id: m.id, kind: "member", name: m.name })),
      ...agents.map((a): ExecutorRef => ({ id: a.id, kind: "agent", name: a.name, owner_id: a.owner.id })),
    ];
    return { members, agents, executors };
  }, []);
}

/**
 * 写操作的统一外壳：busy 状态 + 成功 / 失败 Toast（每个写操作都要有反馈，DESIGN.md）。
 * run(key, fn, okMessage) 返回是否成功；失败时 toast 显示后端给的原因。
 */
export function useAction() {
  const toast = useToast();
  const [busy, setBusy] = useState<string | null>(null);
  const run = useCallback(
    async (key: string, fn: () => Promise<unknown>, okMessage?: string): Promise<boolean> => {
      setBusy(key);
      try {
        await fn();
        if (okMessage) toast.ok(okMessage);
        return true;
      } catch (e) {
        toast.fail(errorMessage(e));
        return false;
      } finally {
        setBusy(null);
      }
    },
    [toast],
  );
  return { busy, run };
}

/** 新建后原地高亮 2 秒：返回当前高亮的 id 与 setter，2 秒后自动清空。 */
export function useHighlight(): [string | null, (id: string) => void] {
  const [id, setId] = useState<string | null>(null);
  const mark = useCallback((next: string) => {
    setId(next);
    window.setTimeout(() => setId((cur) => (cur === next ? null : cur)), 2200);
  }, []);
  return [id, mark];
}

/**
 * 目标完成 · 发芽（提案 §三）：目标进度到 100% 或负责人确认达成时，靴中的芽长出叶子（茎先长、叶后展，一次绿色微光），仪表随后扫动。
 * 只在同一个组件里看到"真的变了"才播（渲染期比较上一次的快照），首次渲染不播；900ms 后清掉标记。
 */
export function useSprout(goal: { achieved: boolean; progress: number }): boolean {
  const [snap, setSnap] = useState({ achieved: goal.achieved, progress: goal.progress, sprout: false });
  if (snap.achieved !== goal.achieved || snap.progress !== goal.progress) {
    const sprout = (!snap.achieved && goal.achieved) || (snap.progress < 100 && goal.progress >= 100);
    setSnap({ achieved: goal.achieved, progress: goal.progress, sprout });
  }
  useEffect(() => {
    if (!snap.sprout) return;
    const id = window.setTimeout(() => setSnap((s) => (s.sprout ? { ...s, sprout: false } : s)), 900);
    return () => window.clearTimeout(id);
  }, [snap.sprout]);
  return snap.sprout;
}
