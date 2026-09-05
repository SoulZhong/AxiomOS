"use client";
import { createContext, useCallback, useContext, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { IconNotify } from "./icons";

/**
 * Toast：右上角、surface-3 底 + 强细线、3 秒消失（失败 5 秒）。成功不加绿，失败加一条 2px danger 左边线（DESIGN.md）。
 * 每个写操作都要有一条：成功 toast.ok(...)，失败 toast.fail(...)。
 *
 * 动效（globals.css .toast）：从顶边滑入 200ms，沿同一条轴退回 120ms；transition 而不是 keyframes，连续弹出互不打断。
 * 左侧一枚信标图标：出现时信标闪两下后常亮（900ms，只动 opacity），不摇铃铛——系统里没有独立的通知指示器，新通知的信标动效先落在这里。
 * 交互：点一下关闭；向上滑动关闭（≥32px，或快速一甩 > 0.11px/ms 不看距离）；向下拖有阻尼；鼠标停在上面时计时暂停。
 * 拖动时直接写元素的 transform（不走 CSS 变量），并打 data-dragging 关掉过渡，手指抬起后过渡从当前位置接着走。
 */
export interface ToastApi {
  ok: (message: string) => void;
  fail: (message: string) => void;
}

interface Item {
  id: number;
  message: string;
  tone: "ok" | "fail";
  leaving?: boolean;
}

const MAX = 4;
/** 与 globals.css 里 .toast[data-leaving] 的 120ms 对齐，留 20ms 余量 */
const EXIT_MS = 140;
const SWIPE_PX = 32;
/** px/ms：快速一甩即关，不必拖满距离 */
const FLICK = 0.11;

const Ctx = createContext<ToastApi>({ ok: () => {}, fail: () => {} });

export const useToast = () => useContext(Ctx);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Item[]>([]);
  const seq = useRef(0);
  /** 还没开始退场的 id（按弹出顺序），超过 MAX 时最早的一条先退场 */
  const live = useRef<number[]>([]);
  /** 每条的计时器：悬停时暂停并记下剩余时间，离开后接着跑 */
  const timers = useRef(new Map<number, { t: number; remaining: number; startedAt: number }>());

  const remove = useCallback((id: number) => {
    timers.current.delete(id);
    setItems((xs) => xs.filter((x) => x.id !== id));
  }, []);
  const dismiss = useCallback(
    (id: number) => {
      const tm = timers.current.get(id);
      if (tm) window.clearTimeout(tm.t);
      timers.current.delete(id);
      live.current = live.current.filter((x) => x !== id);
      setItems((xs) => xs.map((x) => (x.id === id ? { ...x, leaving: true } : x)));
      window.setTimeout(() => remove(id), EXIT_MS);
    },
    [remove],
  );
  const pause = useCallback((id: number) => {
    const tm = timers.current.get(id);
    if (!tm || !tm.t) return;
    window.clearTimeout(tm.t);
    tm.remaining = Math.max(600, tm.remaining - (Date.now() - tm.startedAt));
    tm.t = 0;
  }, []);
  const resume = useCallback(
    (id: number) => {
      const tm = timers.current.get(id);
      if (!tm || tm.t) return;
      tm.startedAt = Date.now();
      tm.t = window.setTimeout(() => dismiss(id), tm.remaining);
    },
    [dismiss],
  );

  const push = useCallback(
    (message: string, tone: Item["tone"]) => {
      const id = ++seq.current;
      const ms = tone === "fail" ? 5000 : 3000;
      setItems((xs) => [...xs, { id, message, tone }]);
      timers.current.set(id, { t: window.setTimeout(() => dismiss(id), ms), remaining: ms, startedAt: Date.now() });
      live.current.push(id);
      if (live.current.length > MAX) dismiss(live.current[0]);
    },
    [dismiss],
  );
  const api = useMemo<ToastApi>(() => ({ ok: (m) => push(m, "ok"), fail: (m) => push(m, "fail") }), [push]);

  return (
    <Ctx.Provider value={api}>
      {children}
      <div className="pointer-events-none fixed top-12 right-4 z-[90] flex w-[min(360px,calc(100vw-32px))] flex-col gap-2" aria-live="polite">
        {items.map((x) => (
          <ToastItem key={x.id} item={x} onDismiss={() => dismiss(x.id)} onPause={() => pause(x.id)} onResume={() => resume(x.id)} />
        ))}
      </div>
    </Ctx.Provider>
  );
}

function ToastItem({ item, onDismiss, onPause, onResume }: { item: Item; onDismiss: () => void; onPause: () => void; onResume: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const drag = useRef<{ y0: number; t0: number; dy: number; moved: boolean } | null>(null);

  const onDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    // 已经在拖了就忽略第二根手指
    if (drag.current || item.leaving || e.button !== 0) return;
    drag.current = { y0: e.clientY, t0: Date.now(), dy: 0, moved: false };
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const onMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    const el = ref.current;
    if (!d || !el) return;
    const raw = e.clientY - d.y0;
    // 向上自由，向下只跟 1/4（阻尼，不是硬墙）
    d.dy = raw < 0 ? raw : raw * 0.25;
    if (Math.abs(raw) > 4) d.moved = true;
    el.dataset.dragging = "";
    el.style.transform = `translateY(${d.dy}px)`;
  };
  const settle = (dismissIt: boolean) => {
    const el = ref.current;
    drag.current = null;
    if (el) {
      delete el.dataset.dragging;
      el.style.transform = "";
    }
    if (dismissIt) onDismiss();
  };
  const onUp = () => {
    const d = drag.current;
    if (!d) return;
    const velocity = Math.abs(d.dy) / Math.max(1, Date.now() - d.t0);
    const flung = d.dy < 0 && (-d.dy >= SWIPE_PX || velocity > FLICK);
    // 没拖动就是一次点击：点一下关闭
    settle(flung || !d.moved);
  };

  return (
    <div
      ref={ref}
      role="status"
      data-leaving={item.leaving ? "" : undefined}
      className={`toast pointer-events-auto flex cursor-default items-start gap-2.5 rounded-md border border-hairline-strong bg-surface-3 px-3.5 py-2.5 text-body text-ink shadow-panel select-none ${item.tone === "fail" ? "border-l-2 border-l-danger" : ""}`}
      onPointerDown={onDown}
      onPointerMove={onMove}
      onPointerUp={onUp}
      onPointerCancel={() => settle(false)}
      onPointerEnter={onPause}
      onPointerLeave={onResume}
    >
      <IconNotify className={`mt-0.5 shrink-0 ${item.tone === "fail" ? "text-danger" : "text-ink-subtle"}`} />
      <span className="min-w-0">{item.message}</span>
    </div>
  );
}
