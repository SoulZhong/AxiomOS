"use client";
import { useEffect, useState } from "react";
import { api, type TaskState, type TaskType, type WorkflowDef } from "./api";

/*
 * 状态语义（不按状态名匹配，只看状态类型与流程定义；CLAUDE.md 硬规则）。
 * - 待验收 = 等待中类型的状态，且流程里有一条从它出发、需要 `review` 授权的步骤（验收通过 / 打回 / 验证通过）。
 *   首页「等我验收」用的就是这个语义（reviewer=me & waiting）；这里把它落到单个状态上，给徽标图标与扫描动效用。
 * - 其他等待中（提问等待、阻塞）不是待验收。
 */
export function isAcceptanceWait(state: TaskState, wf?: WorkflowDef | null): boolean {
  if (state.label !== "waiting") return false;
  if (!wf) return false;
  return wf.transitions.some((tr) => tr.grant === "review" && (tr.from.includes(state.name) || tr.from.includes("*")));
}

/** 任务类型索引（按代码名），全站共用一次 GET /task-types，30s 内复用。 */
let cache: { at: number; byName: Record<string, TaskType> } | null = null;
let inflight: Promise<Record<string, TaskType>> | null = null;
const TTL = 30_000;
function loadTypes(): Promise<Record<string, TaskType>> {
  if (cache && Date.now() - cache.at < TTL) return Promise.resolve(cache.byName);
  if (!inflight) {
    inflight = api.taskTypes
      .list()
      .then((list) => {
        const byName = Object.fromEntries(list.map((x) => [x.name, x]));
        cache = { at: Date.now(), byName };
        return byName;
      })
      .catch(() => cache?.byName ?? {})
      .finally(() => {
        inflight = null;
      });
  }
  return inflight;
}
export function useTaskTypeIndex(enabled = true): Record<string, TaskType> {
  const [idx, setIdx] = useState<Record<string, TaskType>>(() => cache?.byName ?? {});
  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    void loadTypes().then((b) => alive && setIdx(b));
    return () => {
      alive = false;
    };
  }, [enabled]);
  return idx;
}
/** 内置 Bug 类型（代码名 bug）：只有它换成「刷子与异物」图标。 */
export const isBugType = (typeName: string) => typeName === "bug";
