"use client";
import { useEffect, useRef, type RefObject } from "react";
import { fadeRepaint } from "./motion";

/**
 * key 变化时给 ref 指向的元素播一次 200ms 的重绘过渡（页签切换的"交叉淡入"，DESIGN.md §10；与甘特图切刻度同一条过渡）。
 * 首次挂载与 key 由 null 变成有值（首屏解析出默认页签）不播；键盘触发 / reduced-motion 由 fadeRepaint 自己处理。
 */
export function useFadeOnChange(ref: RefObject<Element | null>, key: unknown) {
  const prev = useRef(key);
  useEffect(() => {
    const was = prev.current;
    prev.current = key;
    if (was === key || was === null || was === undefined || key === null || key === undefined) return;
    const anim = fadeRepaint(ref.current);
    return () => anim?.cancel();
  }, [key, ref]);
}
