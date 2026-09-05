"use client";
import { useRouter } from "next/navigation";
import { useEffect, useSyncExternalStore } from "react";
import { t } from "@/lib/i18n";
import { markKeyboardIntent } from "@/lib/motion";
import { Dialog, Kbd } from "./ui";

/**
 * 全局快捷键（DESIGN.md）：/ 聚焦搜索、g t 去任务、g g 去目标、n 新建任务、? 打开说明、[ 收起 / 展开侧栏。
 * 页面可以用 useShortcutHandler 接管某个动作（任务页接管 "new-task" 直接开抽屉）；
 * 没人接管时退回到跳转（/tasks/?new=1 由任务页读取）。
 */
export type ShortcutAction = "search" | "new-task";

const handlers = new Map<ShortcutAction, () => void>();

export function useShortcutHandler(action: ShortcutAction, fn: (() => void) | null) {
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

export function useGlobalShortcuts({ onHelp, onPalette, onSidebar }: { onHelp: () => void; onPalette?: () => void; onSidebar?: () => void }) {
  const router = useRouter();
  useEffect(() => {
    let pendingG = 0;
    const onKey = (e: KeyboardEvent) => {
      // ⌘K / Ctrl+K：指令台。输入框里也生效；已有其他对话框打开时不叠加（指令台自己打开时由它的 Esc 关）
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
      const now = Date.now();
      const chord = pendingG && now - pendingG < 800;
      pendingG = 0;
      // 键盘触发的动作不动画（DESIGN.md「入场与切换」）：先打标记，抽屉 / 对话框 / 页面入场据此跳过过渡
      if (chord) {
        if (e.key === "t") {
          e.preventDefault();
          markKeyboardIntent(1000);
          router.push("/tasks/");
          return;
        }
        if (e.key === "g") {
          e.preventDefault();
          markKeyboardIntent(1000);
          router.push("/goals/");
          return;
        }
      }
      switch (e.key) {
        case "g":
          pendingG = now;
          return;
        case "/": {
          e.preventDefault();
          markKeyboardIntent();
          const h = handlers.get("search");
          if (h) h();
          else router.push("/tasks/?focus=search");
          return;
        }
        case "n": {
          e.preventDefault();
          markKeyboardIntent();
          const h = handlers.get("new-task");
          if (h) h();
          else router.push("/tasks/?new=1");
          return;
        }
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
    return () => window.removeEventListener("keydown", onKey);
  }, [router, onHelp, onPalette, onSidebar]);
}

export function ShortcutHelp({ open, onClose }: { open: boolean; onClose: () => void }) {
  const mac = useIsMac();
  const rows: Array<[string[], string]> = [
    [[mac ? "⌘K" : "Ctrl K"], t("shortcuts.palette")],
    [["/"], t("shortcuts.search")],
    [["g", "t"], t("shortcuts.goTasks")],
    [["g", "g"], t("shortcuts.goGoals")],
    [["n"], t("shortcuts.newTask")],
    [["?"], t("shortcuts.help")],
    [["["], t("shortcuts.sidebar")],
  ];
  return (
    <Dialog open={open} onClose={onClose} title={t("shortcuts.title")} width={400}>
      <ul className="divide-y divide-hairline">
        {rows.map(([keys, label]) => (
          <li key={label} className="flex items-center justify-between py-2">
            <span>{label}</span>
            <span className="inline-flex items-center gap-1">
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
      <p className="text-caption text-ink-subtle">{t("shortcuts.note")}</p>
    </Dialog>
  );
}
