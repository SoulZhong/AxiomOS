"use client";
import { cx } from "@/components/ui";

/*
 * 滚动数字（web/DESIGN.md「舰内系统 v3」§3）：金额与 token 计数用它。
 * 传入已经格式化好的字符串（fmtMoney / fmtTokens 的结果），每个数字位是一个 1em 高的窗口，
 * 里面一列 0–9；数值变化时整列 translateY（160ms 强 ease-out，样式在 instruments.css）。
 * 货币前缀、千分位、小数点、单位（万 / K）是静态字符。位以"从右数第几位"作 key，
 * 于是位数增加时新位在最左边直接出现，其余位各自滚到新值；数值不变时什么都不动。
 * prefers-reduced-motion 下过渡为 0（CSS 里关掉），即时切换。
 */
const DIGITS = ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9"];

export function Odometer({ value, className }: { value: string; className?: string }) {
  const chars = Array.from(value);
  const n = chars.length;
  return (
    <span className={cx("odm", className)}>
      <span className="sr-only">{value}</span>
      <span aria-hidden="true" className="contents">
        {chars.map((c, i) => {
          const key = n - i; // 从右数
          const d = DIGITS.indexOf(c);
          if (d < 0) {
            return (
              <span key={`s${key}`} className="odm-s">
                {c}
              </span>
            );
          }
          return (
            <span key={`d${key}`} className="odm-d">
              <span className="odm-col" style={{ transform: `translateY(-${d}em)` }}>
                {DIGITS.map((x) => (
                  <span key={x}>{x}</span>
                ))}
              </span>
            </span>
          );
        })}
      </span>
    </span>
  );
}
