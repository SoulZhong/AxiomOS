"use client";
import { t } from "@/lib/i18n";
import { cx } from "@/components/ui";

/** 接入向导的五步（DESIGN.md §21）：选运行环境 → 执行命令 → 批准授权 → 连接检查 → 试领任务。 */
export const CONNECT_STEPS = ["link", "approve", "check", "try"] as const;
export type ConnectStep = (typeof CONNECT_STEPS)[number];

/** 步骤条：已完成的实心、当前的 accent、之后的灰；等宽两位序号。 */
export function WizardSteps({ current, className }: { current: ConnectStep; className?: string }) {
  const ci = CONNECT_STEPS.indexOf(current);
  return (
    <ol className={cx("flex flex-wrap items-center gap-x-1 gap-y-1", className)} aria-label={t("connect.stepsLabel")} data-wizard-steps={current}>
      {CONNECT_STEPS.map((s, i) => {
        const state = i < ci ? "done" : i === ci ? "current" : "todo";
        return (
          <li key={s} className="flex items-center gap-1" aria-current={state === "current" ? "step" : undefined} data-step-state={state}>
            <span className={cx("eyebrow inline-flex h-5 min-w-5 items-center justify-center rounded-xs border px-1", state === "current" ? "border-accent bg-accent-bg text-accent-hover" : state === "done" ? "border-hairline-strong bg-surface-2 text-ink-muted" : "border-hairline text-ink-tertiary")}>{String(i + 1).padStart(2, "0")}</span>
            <span className={cx("text-caption", state === "current" ? "font-medium text-ink" : state === "done" ? "text-ink-muted" : "text-ink-tertiary")}>{t(`connect.step.${s}`)}</span>
            {i < CONNECT_STEPS.length - 1 && <span className="mx-1 text-hairline-tertiary" aria-hidden="true">›</span>}
          </li>
        );
      })}
    </ol>
  );
}
