"use client";
import { useEffect, useRef, useState } from "react";
import { cx } from "@/components/ui";

/**
 * 滚动数字（odometer）：每一位是一列 0–9，值变化时该列 160ms 强 ease-out 滚到新数字；非数字字符（¥ , .）原样显示。
 * 首次渲染不滚（data-still）。位数从右往左对齐（key 按距末尾的位置），金额增加一位时新位从左边出现，已有的位不跳。
 * 舷窗带的成本读数与「待我处理」的条数共用它（DESIGN.md §12：读数用数字滚动）。
 */
export function Odometer({ text, className }: { text: string; className?: string }) {
  const [still, setStill] = useState(true);
  const first = useRef(text);
  useEffect(() => {
    if (text !== first.current) setStill(false);
  }, [text]);
  const chars = [...text];
  return (
    <span className={cx("odo", className)} data-still={still || undefined} aria-label={text}>
      {chars.map((ch, i) => {
        const fromEnd = chars.length - 1 - i;
        if (!/\d/.test(ch)) return <span key={`c${fromEnd}`} className="odo-c">{ch}</span>;
        const d = Number(ch);
        return (
          <span key={`d${fromEnd}`} className="odo-d" aria-hidden="true">
            {/* 每行 1.3em（globals.css .odo-col > span）：按行高位移，不能用百分比——百分比是整列（十行）的高度 */}
            <span className="odo-col" style={{ transform: `translateY(${-d * 1.3}em)` }}>
              {Array.from({ length: 10 }, (_, k) => <span key={k}>{k}</span>)}
            </span>
          </span>
        );
      })}
    </span>
  );
}
