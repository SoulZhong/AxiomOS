import type { FocusEvent, KeyboardEvent } from "react";

/**
 * 场景事件总线：登录 / 邀请 / 后台登录页把表单的焦点、按键、提交结果发给 WebGL 场景。
 * 这个文件不依赖 three（页面直接 import 它，不能把 three 拖进主包）；场景那边每帧读 sceneInput 里的目标值，
 * 所有运动仍然经过阻尼（director.ts）。任何事件都不碰 DOM 焦点、不 preventDefault。
 */
export type SceneEvent = "submit" | "success" | "failure" | "focus:email" | "focus:password" | "focus:other" | "blur" | "key" | "input";
export type FocusKind = "email" | "password" | "other" | null;
export type Phase = "idle" | "submitting" | "success" | "failure";

const now = () => (typeof performance !== "undefined" ? performance.now() : 0);
/** 按键脉冲 ≤ 3 次/秒（WCAG 2.3.1） */
const KEY_MIN_GAP_MS = 334;

export const sceneInput = {
  /** 光标 NDC（-1..1），窗口任何位置的 pointermove 都更新；离开窗口时 seen=false，场景慢慢回中 */
  pointer: { x: 0, y: 0, seen: false },
  lastInputAt: 0,
  focus: null as FocusKind,
  phase: "idle" as Phase,
  phaseAt: 0,
  failAt: -1e9,
  keyAt: -1e9,
  keySeq: 0,
  /** 拖拽环绕：按下后的像素位移 */
  drag: { active: false, x0: 0, y0: 0, dx: 0, dy: 0 },
  /** 滚轮推拉：相机 z 的目标偏移与最后一次滚动时间 */
  dolly: { target: 0, at: -1e9 },
  /** reduced-motion（frameloop=demand）时场景登记的重绘函数 */
  invalidate: null as null | (() => void),
  /** WebGL 场景在跑（SceneDirector 挂载时置 true）；没有场景就没有起航转场，也不必等 */
  live: false,
  reduced: false,
};
/** 登录成功 = 登舰：四拍镜头（确认 0.35s → 掠航 1.35s → 入口 0.65s → 白场 0.25s）的总时长；跳转在它播完之后（只在场景在跑且未开启减少动效时） */
export const BOARD_MS = 2600;
/** 进入应用时 AppShell 据此把暖白退场层再淡出 500ms */
export const BOARDING_FLAG = "axiomos.boarding";

export function sceneEmit(e: SceneEvent): Promise<void> {
  const t = now();
  const s = sceneInput;
  s.lastInputAt = t;
  switch (e) {
    case "focus:email":
      s.focus = "email";
      break;
    case "focus:password":
      s.focus = "password";
      break;
    case "focus:other":
      s.focus = "other";
      break;
    case "blur":
      s.focus = null;
      break;
    case "key":
      if (t - s.keyAt >= KEY_MIN_GAP_MS) {
        s.keyAt = t;
        s.keySeq++;
      }
      break;
    case "submit":
      s.phase = "submitting";
      s.phaseAt = t;
      break;
    case "success":
      s.phase = "success";
      s.phaseAt = t;
      if (s.live && !s.reduced) {
        document.querySelector("[data-brand-root]")?.setAttribute("data-boarding", "");
        try {
          sessionStorage.setItem(BOARDING_FLAG, "1");
        } catch {}
      }
      break;
    case "failure":
      s.phase = "failure";
      s.phaseAt = t;
      s.failAt = t;
      document.querySelector("[data-brand-root]")?.removeAttribute("data-boarding");
      break;
    case "input":
      break;
  }
  s.invalidate?.();
  if (e === "success" && s.live && !s.reduced) return new Promise((r) => setTimeout(r, BOARD_MS));
  return Promise.resolve();
}

/**
 * 挂在 <form> 上的三个捕获阶段监听：焦点进入 / 离开哪个字段、每次按键。
 * 只记录，不阻止默认行为，不改焦点，不影响 Tab 顺序。
 */
export function sceneFormProps() {
  return {
    onFocusCapture: (e: FocusEvent<HTMLElement>) => {
      const el = e.target as HTMLInputElement;
      if (!(el instanceof HTMLInputElement)) return;
      sceneEmit(el.type === "password" ? "focus:password" : el.type === "email" ? "focus:email" : "focus:other");
    },
    onBlurCapture: (e: FocusEvent<HTMLElement>) => {
      if (e.target instanceof HTMLInputElement) sceneEmit("blur");
    },
    onKeyDownCapture: (e: KeyboardEvent<HTMLElement>) => {
      // 只有会改变输入内容的键才算"敲了一下"（字符、退格、删除）；方向键 / Tab / Enter 只重置空闲计时
      sceneEmit(e.key.length === 1 || e.key === "Backspace" || e.key === "Delete" ? "key" : "input");
    },
  };
}
