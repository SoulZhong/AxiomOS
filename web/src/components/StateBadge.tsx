"use client";
import { useState, type ReactNode } from "react";
import type { StateLabel, TaskState } from "@/lib/api";
import { stateLabel } from "@/lib/terms";
import { IconAccept, IconCancel, IconTask, IconTaskActive } from "./icons";
import { StatusLED, Tag, type LedTone, type Tone } from "./ui";

/** 状态类型到语义色的映射是固定的：未开始=中性，进行中=强调，等待中=警示，已完成=成功，已终止=中性偏暗（不是红：终止不是错误）。 */
export const LABEL_TONE: Record<StateLabel, Tone> = {
  pending: "neutral",
  active: "accent",
  waiting: "warning",
  terminal_success: "success",
  terminal_failure: "neutral",
};
export const isDarkLabel = (l: StateLabel) => l === "terminal_failure";
/** 指示灯颜色：与标签同色；已终止用更暗的一档；只有进行中的那一颗发光。 */
export const ledTone = (l: StateLabel): LedTone => (isDarkLabel(l) ? "dark" : LABEL_TONE[l]);

/**
 * 状态类型的签名图标（提案 §二，只按状态类型，不按状态名）：进行中 = 正在压缩的方块；已完成 = 压实的方块；已终止 = 气闸；
 * 待验收（等待中且有验收步骤出口，由调用方按流程定义判定后传 accept）= 扫描。未开始与其他等待中不带图标。
 */
export function stateIcon(label: StateLabel, accept?: boolean): ReactNode {
  const p = { size: 12, className: "tag-icon" } as const;
  if (label === "active") return <IconTaskActive {...p} />;
  if (label === "terminal_success") return <IconTask {...p} />;
  if (label === "terminal_failure") return <IconCancel {...p} />;
  if (label === "waiting" && accept) return <IconAccept {...p} />;
  return null;
}
/** 状态切换时该播哪条签名动效（只在同一个徽标换状态时，首次渲染不播）。 */
export type StateMotion = "compact" | "scan" | undefined;
export const stateMotion = (label: StateLabel, accept?: boolean): StateMotion => (label === "terminal_success" ? "compact" : label === "waiting" && accept ? "scan" : undefined);

/**
 * 状态徽标三级：8px 指示灯 + 状态名标签（三件套语义色，前面 12px 签名图标）+ 12px 状态类型说明文字，不重复上色。
 * 乐观更新：pending 时整组 60% 透明，确认后 200ms 回到不透明；状态名变了（同一个徽标换状态）时，
 * 内层按 state.name 重挂，新标签 160ms 淡入 + 2px 模糊过渡（.state-swap），并按新状态类型播签名动效：
 * 进入成功终态 = 压实（方块缩放回弹 + LED 转绿 420ms）；进入待验收 = 扫描线扫一次（行级扫描光由调用方在行上打 data-motion="scan"）。
 */
export function StateBadge({ state, showLabel = true, pending, led = true, accept }: { state: TaskState; showLabel?: boolean; pending?: boolean; led?: boolean; accept?: boolean }) {
  const [seen, setSeen] = useState(state.name);
  const [swapped, setSwapped] = useState(false);
  if (seen !== state.name) {
    setSeen(state.name);
    setSwapped(true);
  }
  const motion = swapped ? stateMotion(state.label, accept) : undefined;
  return (
    <span className={`state-badge inline-flex items-center gap-1.5 ${pending ? "opacity-60" : ""}`} aria-busy={pending || undefined}>
      <span key={state.name} className={swapped ? "state-swap inline-flex items-center gap-1.5" : "inline-flex items-center gap-1.5"} data-motion={motion}>
        {/* 舰内系统 v3 §5：进行中呼吸 3s（led-active，StatusLED 自带）、等待中常亮暗色（led-waiting） */}
        {led && <StatusLED tone={ledTone(state.label)} className={state.label === "waiting" ? "led-waiting" : undefined} />}
        <Tag tone={LABEL_TONE[state.label]} dark={isDarkLabel(state.label)}>
          {stateIcon(state.label, accept)}
          {state.title}
        </Tag>
        {showLabel && state.title !== stateLabel(state.label) && <span className="text-caption text-ink-subtle">{stateLabel(state.label)}</span>}
      </span>
    </span>
  );
}

export function LabelBadge({ label, led = false }: { label: StateLabel; led?: boolean }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      {led && <StatusLED tone={ledTone(label)} className={label === "waiting" ? "led-waiting" : undefined} />}
      <Tag tone={LABEL_TONE[label]} dark={isDarkLabel(label)}>{stateLabel(label)}</Tag>
    </span>
  );
}
