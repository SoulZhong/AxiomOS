"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage } from "@/lib/hooks";

/*
 * 就地编辑的保存状态（DESIGN.md §15）：一行一个请求。
 * idle → saving（值 60% 透明）→ saved（12px 对勾闪现 1.4s 后回到 idle）；失败 → error（调用方回退值，行下一句原因）。
 * 同一行连续保存时以最后一次为准（序号比对），早到的结果不覆盖晚发的。
 */
export type InlineStatus = "idle" | "saving" | "saved" | "error";
export interface InlineSaveState {
  status: InlineStatus;
  error: string | null;
}
const IDLE: InlineSaveState = { status: "idle", error: null };
const SAVED_MS = 1400;

/** 多行共用：按 key 记每一行的状态。run(key, fn, onOk, onFail) 返回是否成功。 */
export function useInlineSaves() {
  const [states, setStates] = useState<Record<string, InlineSaveState>>({});
  const seq = useRef<Record<string, number>>({});
  const timers = useRef<Record<string, number>>({});
  useEffect(() => () => Object.values(timers.current).forEach((id) => window.clearTimeout(id)), []);
  const set = useCallback((key: string, s: InlineSaveState) => setStates((prev) => ({ ...prev, [key]: s })), []);
  const run = useCallback(
    async <T,>(key: string, fn: () => Promise<T>, onOk?: (result: T) => void, onFail?: () => void): Promise<boolean> => {
      const n = (seq.current[key] = (seq.current[key] ?? 0) + 1);
      window.clearTimeout(timers.current[key]);
      set(key, { status: "saving", error: null });
      try {
        const r = await fn();
        if (seq.current[key] !== n) return true;
        onOk?.(r);
        set(key, { status: "saved", error: null });
        timers.current[key] = window.setTimeout(() => setStates((prev) => (prev[key]?.status === "saved" ? { ...prev, [key]: IDLE } : prev)), SAVED_MS);
        return true;
      } catch (e) {
        if (seq.current[key] !== n) return false;
        onFail?.();
        set(key, { status: "error", error: errorMessage(e) });
        return false;
      }
    },
    [set],
  );
  const get = useCallback((key: string): InlineSaveState => states[key] ?? IDLE, [states]);
  const clear = useCallback((key: string) => set(key, IDLE), [set]);
  return { get, run, clear };
}

/** 单个控件自己用（标题、描述）。 */
export function useInlineSave() {
  const saves = useInlineSaves();
  const run = useCallback(<T,>(fn: () => Promise<T>, onOk?: (r: T) => void, onFail?: () => void) => saves.run("_", fn, onOk, onFail), [saves]);
  const state = saves.get("_");
  return { status: state.status, error: state.error, run, clear: () => saves.clear("_") };
}
