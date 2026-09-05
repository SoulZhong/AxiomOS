"use client";
import { useCallback, useEffect, useRef, useState } from "react";

/**
 * 记在 localStorage 里的一小段界面状态（甘特图的左列宽度、行高、图例、折叠……）。
 * 预渲染 / 水合阶段先用默认值，挂载后读一次本地值（避免服务端与客户端首屏不一致）；写入失败（隐私模式）静默。
 */
export function usePersisted<T>(key: string, initial: T, parse?: (raw: unknown) => T | undefined): [T, (next: T | ((prev: T) => T)) => void] {
  const [value, setValue] = useState<T>(initial);
  const initialRef = useRef(initial);
  useEffect(() => {
    initialRef.current = initial;
  });
  // key 变化（如按分组方式记折叠状态）时重新读取；没存过就回到默认值
  useEffect(() => {
    let next: T | undefined;
    try {
      const raw = localStorage.getItem(key);
      if (raw !== null) {
        const parsed = JSON.parse(raw) as unknown;
        next = parse ? parse(parsed) : (parsed as T);
      }
    } catch {
      /* 读不到就用默认值 */
    }
    setValue(next === undefined ? initialRef.current : next);
  }, [key, parse]);
  const set = useCallback(
    (next: T | ((prev: T) => T)) => {
      setValue((prev) => {
        const v = typeof next === "function" ? (next as (p: T) => T)(prev) : next;
        try {
          localStorage.setItem(key, JSON.stringify(v));
        } catch {
          /* 写不进就只留在内存里 */
        }
        return v;
      });
    },
    [key],
  );
  return [value, set];
}
