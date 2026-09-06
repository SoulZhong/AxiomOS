"use client";
import { useRouter } from "next/navigation";
import { useEffect, useSyncExternalStore } from "react";
import { t } from "@/lib/i18n";
import { markKeyboardIntent } from "@/lib/motion";
import { Dialog, Kbd, cx } from "./ui";

/**
 * 全局快捷键（DESIGN.md §19）：前缀式两键组合 + 单键。
 *   前缀（按下后 1.5 秒内等第二个键，右下角出现小提示）：
 *     t c 新建任务 · g c 新建目标 · g t 去任务 · g g 去目标 · n o 打开「我的工作」（待我处理）
 *     v l / v b / v g / v s / v k 切到 列表 / 看板 / 甘特图 / 迭代 / 待领取
 *   单键：j / k 在任务列表或看板里上下选、Enter 打开抽屉、o 打开完整页、f 或 / 聚焦筛选、? 快捷键表、[ 收起侧栏、Esc 关闭
 *   ⌘K / Ctrl K 快速命令（输入框里也生效）。
 * 输入框里不触发；有对话框打开时不触发（对话框自己处理 Esc）。
 * 页面可以用 useShortcutHandler 接管某个动作（任务页接管 "new-task" 直接开抽屉、"view" 直接切页签）；没人接管时退回到跳转。
 */
export type ShortcutAction = "search" | "filter" | "new-task" | "new-goal" | "view" | "nav-down" | "nav-up" | "nav-open" | "nav-open-full" | "escape";
type Handler = (arg?: string) => void;

const handlers = new Map<ShortcutAction, Handler>();

export function useShortcutHandler(action: ShortcutAction, fn: Handler | null) {
  useEffect(() => {
    if (!fn) return;
    handlers.set(action, fn);
    return () => {
      if (handlers.get(action) === fn) handlers.delete(action);
    };
  }, [action, fn]);
}

const isEditable = (el: EventTarget | null) => {
  if (!(el instanceof HTMLElement)) return false;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
};

/** 平台判断：Mac 显示 ⌘K，其他显示 Ctrl K（服务端与水合阶段一律 ⌘K）。 */
const noSub = () => () => {};
const isMac = () => /Mac|iPhone|iPad/.test(navigator.platform) || /Mac/.test(navigator.userAgent);
export function useIsMac(): boolean {
  return useSyncExternalStore(noSub, isMac, () => true);
}

// ---------- 前缀状态（右下角的小提示读它） ----------
export type Prefix = "t" | "g" | "v" | "n";
const PREFIXES: Prefix[] = ["t", "g", "v", "n"];
const CHORD_MS = 1500;
let pending: Prefix | null = null;
let pendingTimer = 0;
const chordListeners = new Set<() => void>();
const setPending = (p: Prefix | null) => {
  pending = p;
  window.clearTimeout(pendingTimer);
  if (p) pendingTimer = window.setTimeout(() => setPending(null), CHORD_MS);
  chordListeners.forEach((fn) => fn());
};
const subscribeChord = (cb: () => void) => {
  chordListeners.add(cb);
  return () => chordListeners.delete(cb);
};
const getPending = () => pending;
const getServerPending = () => null;

/** 每个前缀能接的第二个键（提示与快捷键表共用） */
export const CHORDS: Record<Prefix, Array<[string, () => string]>> = {
  t: [["c", () => t("shortcuts.newTask")]],
  g: [["t", () => t("shortcuts.goTasks")], ["g", () => t("shortcuts.goGoals")], ["c", () => t("shortcuts.newGoal")]],
  v: [["l", () => t("tasks.view.list")], ["b", () => t("tasks.view.board")], ["g", () => t("tasks.view.gantt")], ["s", () => t("tasks.view.sprints")], ["k", () => t("tasks.view.backlog")]],
  n: [["o", () => t("shortcuts.goInbox")]],
};
const VIEW_OF: Record<string, string> = { l: "list", b: "board", g: "gantt", s: "sprints", k: "backlog" };

export function useGlobalShortcuts({ onHelp, onPalette, onSidebar }: { onHelp: () => void; onPalette?: () => void; onSidebar?: () => void }) {
  const router = useRouter();
  useEffect(() => {
    const go = (href: string) => {
      markKeyboardIntent(1000);
      router.push(href);
    };
    const call = (action: ShortcutAction, fallback: () => void, arg?: string) => {
      markKeyboardIntent();
      const h = handlers.get(action);
      if (h) h(arg);
      else fallback();
    };
    const runChord = (prefix: Prefix, key: string): boolean => {
      if (prefix === "t" && key === "c") { call("new-task", () => go("/tasks/?new=1")); return true; }
      if (prefix === "g" && key === "t") { go("/tasks/"); return true; }
      if (prefix === "g" && key === "g") { go("/goals/"); return true; }
      if (prefix === "g" && key === "c") { call("new-goal", () => go("/goals/?new=1")); return true; }
      if (prefix === "n" && key === "o") { go("/"); return true; }
      if (prefix === "v" && VIEW_OF[key]) { const v = VIEW_OF[key]; call("view", () => go(`/tasks/?view=${v}`), v); return true; }
      return false;
    };
    const onKey = (e: KeyboardEvent) => {
      // ⌘K / Ctrl+K：快速命令。输入框里也生效；已有其他对话框打开时不叠加（面板自己打开时由它的 Esc 关）
      if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k") {
        if (!onPalette || document.querySelector("dialog[open]:not(.palette)")) return;
        e.preventDefault();
        markKeyboardIntent();
        onPalette();
        return;
      }
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isEditable(e.target)) return;
      if (document.querySelector("dialog[open]")) return;
      if (e.key === "Escape") {
        setPending(null);
        call("escape", () => {});
        return;
      }
      const prefix = pending;
      if (prefix) {
        setPending(null);
        if (runChord(prefix, e.key)) {
          e.preventDefault();
          return;
        }
      }
      if ((PREFIXES as string[]).includes(e.key)) {
        e.preventDefault();
        setPending(e.key as Prefix);
        return;
      }
      switch (e.key) {
        case "/":
        case "f":
          e.preventDefault();
          call("filter", () => call("search", () => go("/tasks/?focus=search")));
          return;
        case "j":
          e.preventDefault();
          call("nav-down", () => {});
          return;
        case "k":
          e.preventDefault();
          call("nav-up", () => {});
          return;
        case "Enter":
          if (e.target instanceof HTMLElement && e.target.closest("a,button,[role=button]")) return;
          call("nav-open", () => {});
          return;
        case "o":
          e.preventDefault();
          call("nav-open-full", () => {});
          return;
        case "?":
          e.preventDefault();
          markKeyboardIntent();
          onHelp();
          return;
        case "[":
          if (!onSidebar) return;
          e.preventDefault();
          markKeyboardIntent();
          onSidebar();
          return;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      setPending(null);
    };
  }, [router, onHelp, onPalette, onSidebar]);
}

/** 右下角的前缀提示：按下 t / g / v / n 后出现，列出能接的第二个键；1.5 秒没按或按了别的键就消失。 */
export function ChordIndicator() {
  const p = useSyncExternalStore(subscribeChord, getPending, getServerPending);
  if (!p) return null;
  return (
    <div className="chord-hint" role="status" aria-live="polite" data-chord={p}>
      <Kbd>{p}</Kbd>
      <span className="text-ink-subtle">{t("shortcuts.then")}</span>
      {CHORDS[p].map(([k, label]) => (
        <span key={k} className="inline-flex items-center gap-1">
          <Kbd>{k}</Kbd>
          <span className="text-ink-muted">{label()}</span>
        </span>
      ))}
    </div>
  );
}

export function ShortcutHelp({ open, onClose }: { open: boolean; onClose: () => void }) {
  const mac = useIsMac();
  const groups: Array<[string, Array<[string[], string]>]> = [
    [t("shortcuts.group.go"), [
      [["g", "t"], t("shortcuts.goTasks")],
      [["g", "g"], t("shortcuts.goGoals")],
      [["n", "o"], t("shortcuts.goInbox")],
    ]],
    [t("shortcuts.group.create"), [
      [["t", "c"], t("shortcuts.newTask")],
      [["g", "c"], t("shortcuts.newGoal")],
    ]],
    [t("shortcuts.group.view"), [
      [["v", "l"], t("tasks.view.list")],
      [["v", "b"], t("tasks.view.board")],
      [["v", "g"], t("tasks.view.gantt")],
      [["v", "s"], t("tasks.view.sprints")],
      [["v", "k"], t("tasks.view.backlog")],
    ]],
    [t("shortcuts.group.list"), [
      [["j"], t("shortcuts.navDown")],
      [["k"], t("shortcuts.navUp")],
      [["Enter"], t("shortcuts.open")],
      [["o"], t("shortcuts.openFull")],
      [["f"], t("shortcuts.filter")],
      [["Esc"], t("shortcuts.close")],
    ]],
    [t("shortcuts.group.general"), [
      [[mac ? "⌘K" : "Ctrl K"], t("shortcuts.palette")],
      [["?"], t("shortcuts.help")],
      [["["], t("shortcuts.sidebar")],
    ]],
  ];
  return (
    <Dialog open={open} onClose={onClose} title={t("shortcuts.title")} width={560}>
      <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
        {groups.map(([title, rows]) => (
          <section key={title}>
            <h3 className="eyebrow mb-1 text-ink-subtle">{title}</h3>
            <ul className="divide-y divide-hairline">
              {rows.map(([keys, label]) => (
                <li key={label} className="flex items-center justify-between gap-3 py-1.5">
                  <span className={cx("min-w-0 truncate")}>{label}</span>
                  <span className="inline-flex shrink-0 items-center gap-1">
                    {keys.map((k, i) => (
                      <span key={i} className="inline-flex items-center gap-1">
                        {i > 0 && <span className="text-caption text-ink-subtle">{t("shortcuts.then")}</span>}
                        <Kbd>{k}</Kbd>
                      </span>
                    ))}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
      <p className="text-caption text-ink-subtle">{t("shortcuts.note")}</p>
    </Dialog>
  );
}
