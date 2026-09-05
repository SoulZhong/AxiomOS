"use client";
import { t } from "@/lib/i18n";
import { cx } from "@/components/ui";

/** 工作量的取值：团队自己校准的相对大小（CONTEXT.md）。 */
export const POINT_VALUES = [1, 2, 3, 5, 8, 13] as const;

/** 只读的点数芯片：`3 点`；没估时虚线框「未估」。 */
export function PointsChip({ value, className }: { value: number | null | undefined; className?: string }) {
  if (value === null || value === undefined) {
    return <span className={cx("pt-chip", className)} data-empty="" title={t("task.pointsUnset")}>{t("board.pointsUnset")}</span>;
  }
  return <span className={cx("pt-chip", className)} title={t("points.label", { n: value })}>{t("board.points", { n: value })}</span>;
}

/**
 * 行内点数编辑器：1 2 3 5 8 13 一排小按钮，当前值高亮；再点一次当前值 = 清除。
 * 每次点击立刻调用 onChange（由调用方 PATCH 并给 Toast），busy 时禁用。
 */
export function PointsChips({ value, onChange, busy, size = "md", className, label }: { value: number | null | undefined; onChange: (next: number | null) => void; busy?: boolean; size?: "sm" | "md"; className?: string; label?: string }) {
  return (
    <span className={cx("pt-edit", className)} role="radiogroup" aria-label={label ?? t("task.points")}>
      {POINT_VALUES.map((n) => {
        const on = value === n;
        return (
          <button
            key={n}
            type="button"
            role="radio"
            aria-checked={on}
            className="pt-btn"
            style={size === "sm" ? { height: 22, minWidth: 24, fontSize: 11 } : undefined}
            disabled={busy}
            onClick={() => onChange(on ? null : n)}
            title={on ? t("points.clear") : t("points.label", { n })}
          >
            {n}
          </button>
        );
      })}
    </span>
  );
}
