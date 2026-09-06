"use client";
import type { ReactNode } from "react";
import { cx } from "./ui";

/**
 * 执行简报的一段（DESIGN.md §17）：任务与目标详情都按「现在要做什么 → 为什么做 → 前面发生了什么 → 如何验收」四段排，
 * 每段一行眉标（两位序号 + 标题 + 一句说明），下面是这一段的面板。序号用遥测色，和面板眉标同一套字。
 */
export function BriefSection({ n, title, hint, children, className, id }: { n: string; title: string; hint?: string; children: ReactNode; className?: string; id?: string }) {
  return (
    <section id={id} className={cx("brief-section space-y-3", className)} data-brief={n} aria-label={title}>
      <h2 className="brief-eyebrow">
        <span className="text-telemetry">{n}</span>
        <span>{title}</span>
        {hint && <span className="brief-hint">{hint}</span>}
      </h2>
      {children}
    </section>
  );
}
