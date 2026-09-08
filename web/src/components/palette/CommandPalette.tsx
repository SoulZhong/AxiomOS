"use client";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode, type SyntheticEvent } from "react";
import { api, type Agent, type Goal, type Member, type Task } from "@/lib/api";
import { LOCALES, setLocale, t, useLocale } from "@/lib/i18n";
import { keyboardIntent, markKeyboardIntent } from "@/lib/motion";
import { agentStateTitle } from "@/lib/terms";
import { THEMES, useTheme } from "@/lib/theme";
import { setScope, useScopeState } from "@/lib/useScope";
import { IconAgent, IconBacklog, IconEdit, IconGoal, IconHome, IconKeyboard, IconMember, IconOrg, IconPlus, IconTask } from "@/components/icons";
import { Kbd, cx } from "@/components/ui";

/**
 * 指令台（DESIGN.md「舰内系统 v3」§4）：⌘K / Ctrl+K 或侧栏按钮打开的全局命令面板。等宽提示符 `›`，模糊搜索页面、任务、目标、Agent
 * （现有列表接口，打开时拉一次、30s 内复用；输入 120ms 防抖）；动作：新建任务 / 目标、领取任务、打开待我处理、编辑布局、切换主题、切换语言、快捷键；
 * 范围：会话给的档位逐个列出（当前那档带「当前」）；成员：有组织设置入口的人可以跳到成员与团队页并定位到那个人（DESIGN.md §19）。
 * 上下键选择、回车执行、Esc 关闭、Tab 不离开输入框（焦点留在面板内）。结果分组带等宽眉标。
 * 进场：原生 dialog + @starting-style，120ms 从 scale(0.98) 淡入，退场 80ms；键盘打开（⌘K）时按「键盘触发的动作不动画」直接出现。
 * 面板在浅色主题下也是深色（data-theme="dark"），和舷窗带一样是舱内的全息屏。
 */
export type PaletteGroup = "actions" | "scopes" | "pages" | "tasks" | "goals" | "agents" | "members";
export interface PaletteItem {
  id: string;
  group: PaletteGroup;
  title: string;
  hint?: string;
  keywords?: string;
  /** 16px 签名图标（页面项沿用侧栏的；任务 / 目标 / Agent 各自的概念图标；动作只给新建 / 领取 / 快捷键） */
  icon?: ReactNode;
  run: () => void;
}
export interface PalettePage {
  href: string;
  label: string;
  icon?: ReactNode;
}

const GROUP_ORDER: PaletteGroup[] = ["actions", "scopes", "pages", "tasks", "goals", "agents", "members"];
const CACHE_MS = 30_000;
const DEBOUNCE_MS = 120;
const MAX_PER_GROUP = 8;

/** 子序列模糊匹配：命中返回分数（前缀、连续命中、词首加分），没命中返回 null。 */
export function fuzzyScore(query: string, text: string): number | null {
  const q = query.trim().toLowerCase();
  if (!q) return 0;
  const s = text.toLowerCase();
  const at = s.indexOf(q);
  if (at >= 0) return 100 - at + (at === 0 ? 20 : 0);
  let qi = 0, score = 0, last = -2;
  for (let i = 0; i < s.length && qi < q.length; i++) {
    if (s[i] === q[qi]) {
      score += i === last + 1 ? 6 : 2;
      if (i === 0 || s[i - 1] === " " || s[i - 1] === "-") score += 3;
      last = i;
      qi++;
    }
  }
  return qi === q.length ? score : null;
}

let cache: { at: number; tasks: Task[]; goals: Goal[]; agents: Agent[]; members: Member[] } | null = null;
async function loadIndex() {
  if (cache && Date.now() - cache.at < CACHE_MS) return cache;
  const [tasks, goals, agents, members] = await Promise.all([api.tasks.list().catch(() => [] as Task[]), api.goals.list().catch(() => [] as Goal[]), api.agents.list().catch(() => [] as Agent[]), api.members.list().catch(() => [] as Member[])]);
  cache = { at: Date.now(), tasks, goals, agents, members };
  return cache;
}
const flattenGoals = (gs: Goal[], out: Goal[] = []): Goal[] => {
  for (const g of gs) {
    out.push(g);
    if (g.children?.length) flattenGoals(g.children, out);
  }
  return out;
};

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = window.setTimeout(() => setV(value), ms);
    return () => window.clearTimeout(id);
  }, [value, ms]);
  return v;
}

export function CommandPalette({ open, onClose, onHelp, pages, loggedIn, canManageOrg = false }: { open: boolean; onClose: () => void; onHelp: () => void; pages: PalettePage[]; loggedIn: boolean; canManageOrg?: boolean }) {
  const router = useRouter();
  const { locale } = useLocale();
  const [theme, setTheme] = useTheme();
  const scopeState = useScopeState();
  const ref = useRef<HTMLDialogElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const [shown, setShown] = useState(open);
  const [query, setQuery] = useState("");
  const q = useDebounced(query, DEBOUNCE_MS);
  const [index, setIndex] = useState<Awaited<ReturnType<typeof loadIndex>> | null>(null);
  const [sel, setSel] = useState(0);
  if (open && !shown) setShown(true);
  // 打开时清空输入、选中第一项；输入变化时回到第一项（渲染期同步，不用 effect）
  const [seenOpen, setSeenOpen] = useState(open);
  if (seenOpen !== open) {
    setSeenOpen(open);
    if (open) {
      setQuery("");
      setSel(0);
    }
  }
  const [seenQ, setSeenQ] = useState(q);
  if (seenQ !== q) {
    setSeenQ(q);
    setSel(0);
  }

  // 打开 / 关闭：与 ui.tsx 的对话框同一套 data-closing + transitionend 收尾
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (open) {
      delete el.dataset.closing;
      if (!el.open) el.showModal();
      requestAnimationFrame(() => inputRef.current?.focus());
      return;
    }
    if (!el.open) return;
    el.dataset.closing = "";
    let done = false;
    const finish = () => {
      if (done) return;
      done = true;
      el.close();
      delete el.dataset.closing;
      setShown(false);
    };
    if (keyboardIntent()) {
      const now = window.setTimeout(finish, 0);
      return () => window.clearTimeout(now);
    }
    const onEnd = (e: TransitionEvent) => {
      if (e.target === el && e.propertyName === "opacity") finish();
    };
    el.addEventListener("transitionend", onEnd);
    const fallback = window.setTimeout(finish, 160);
    return () => {
      el.removeEventListener("transitionend", onEnd);
      window.clearTimeout(fallback);
    };
  }, [open]);
  useEffect(() => {
    if (!open || !loggedIn) return;
    let alive = true;
    loadIndex().then((ix) => alive && setIndex(ix));
    return () => {
      alive = false;
    };
  }, [open, loggedIn]);

  const close = useCallback(() => onClose(), [onClose]);
  const go = useCallback(
    (href: string) => {
      close();
      router.push(href);
    },
    [close, router],
  );

  const items = useMemo<PaletteItem[]>(() => {
    const nextTheme = THEMES[(THEMES.indexOf(theme) + 1) % THEMES.length];
    const nextLocale = LOCALES[(LOCALES.indexOf(locale) + 1) % LOCALES.length];
    const actions: PaletteItem[] = [
      { id: "a:new", group: "actions", title: t("palette.newTask"), hint: "t c", keywords: "new task create", icon: <IconPlus />, run: () => go("/tasks/?new=1") },
      { id: "a:newGoal", group: "actions", title: t("palette.newGoal"), hint: "g c", keywords: "new goal create", icon: <IconGoal />, run: () => go("/goals/?new=1") },
      { id: "a:inbox", group: "actions", title: t("palette.goInbox"), hint: "n o", keywords: "inbox waiting home", icon: <IconHome />, run: () => go("/") },
      { id: "a:claim", group: "actions", title: t("palette.claim"), hint: "v k", keywords: "claim backlog", icon: <IconBacklog />, run: () => go("/tasks/?view=backlog") },
      { id: "a:layout", group: "actions", title: t("palette.editLayout"), keywords: "layout workspace edit blocks", icon: <IconEdit />, run: () => go("/?edit=1") },
      { id: "a:theme", group: "actions", title: t("palette.theme"), hint: t(`theme.${nextTheme}`), keywords: "theme dark light", run: () => { close(); setTheme(nextTheme); } },
      { id: "a:lang", group: "actions", title: t("palette.lang"), hint: nextLocale === "zh-CN" ? "中文" : "English", keywords: "language english 中文", run: () => { close(); if (loggedIn) void api.auth.updateMe({ locale: nextLocale }).catch(() => {}); setLocale(nextLocale); } },
      { id: "a:keys", group: "actions", title: t("palette.shortcuts"), hint: "?", keywords: "shortcuts keys help", icon: <IconKeyboard />, run: () => { close(); onHelp(); } },
    ];
    const scopeItems: PaletteItem[] = scopeState.options.map((o) => ({ id: `s:${o.id}`, group: "scopes", title: t("palette.scope", { name: o.title }), hint: o.id === scopeState.scope ? t("palette.scopeCurrent") : undefined, keywords: "scope 范围 team", icon: <IconOrg />, run: () => { close(); setScope(o.id); } }));
    const pageItems: PaletteItem[] = pages.map((p) => ({ id: `p:${p.href}`, group: "pages", title: p.label, keywords: p.href, icon: p.icon, run: () => go(p.href) }));
    const taskItems: PaletteItem[] = (index?.tasks ?? []).map((tk) => ({ id: `t:${tk.id}`, group: "tasks", title: tk.number != null ? `#${tk.number} ${tk.title}` : tk.title, hint: tk.state.title, keywords: `${tk.number != null ? `#${tk.number} ${tk.number}` : ""} ${tk.type_title} ${tk.assignee?.name ?? ""}`, icon: <IconTask />, run: () => go(`/tasks/${encodeURIComponent(tk.id)}/`) }));
    const goalItems: PaletteItem[] = flattenGoals(index?.goals ?? []).map((g) => ({ id: `g:${g.id}`, group: "goals", title: g.title, hint: `${Math.round(g.progress)}%`, keywords: g.owner?.name ?? "", icon: <IconGoal stage={g.achieved ? "grown" : "bud"} />, run: () => go(`/goals/${encodeURIComponent(g.id)}/`) }));
    const agentItems: PaletteItem[] = (index?.agents ?? []).map((a) => ({ id: `ag:${a.id}`, group: "agents", title: a.name, hint: agentStateTitle(a.state), keywords: a.owner.name, icon: <IconAgent />, run: () => go("/agents/") }));
    const memberItems: PaletteItem[] = canManageOrg ? (index?.members ?? []).map((m) => ({ id: `m:${m.id}`, group: "members", title: m.name, hint: m.email, keywords: `${m.email} ${m.roles.join(" ")}`, icon: <IconMember />, run: () => go(`/settings/?tab=people&member=${encodeURIComponent(m.id)}`) })) : [];
    return [...actions, ...scopeItems, ...pageItems, ...taskItems, ...goalItems, ...agentItems, ...memberItems];
  }, [theme, locale, pages, index, go, close, setTheme, loggedIn, onHelp, scopeState, canManageOrg]);

  const results = useMemo(() => {
    const qq = q.trim();
    // 输入 `#123` / `123`：第一项就是「打开任务 #123」，回车走 /task-by-number/123/（找不到时那一页会说没有这个序号）
    const num = /^#?(\d+)$/.exec(qq)?.[1];
    const jump: PaletteItem[] = num ? [{ id: `n:${num}`, group: "actions", title: t("palette.openNumber", { n: num }), hint: "#", icon: <IconTask />, run: () => go(`/task-by-number/${num}/`) }] : [];
    const scored = items
      .map((it) => ({ it, score: qq ? Math.max(fuzzyScore(qq, it.title) ?? -1, it.keywords ? (fuzzyScore(qq, it.keywords) ?? -1) - 1 : -1) : 0 }))
      .filter((x) => x.score >= 0);
    const groups = new Map<PaletteGroup, PaletteItem[]>();
    for (const g of GROUP_ORDER) {
      const list = scored.filter((x) => x.it.group === g);
      if (qq) list.sort((a, b) => b.score - a.score);
      const take = qq || g === "actions" || g === "pages" ? list.slice(0, MAX_PER_GROUP) : g === "scopes" ? list.slice(0, 3) : list.slice(0, 5);
      const picked = g === "actions" ? [...jump, ...take.map((x) => x.it)] : take.map((x) => x.it);
      if (picked.length) groups.set(g, picked);
    }
    return groups;
  }, [items, q, go]);
  const flat = useMemo(() => [...results.values()].flat(), [results]);
  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>(`[data-i="${sel}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [sel, flat]);

  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" || (e.key === "Tab" && !e.shiftKey)) {
      e.preventDefault();
      if (flat.length) setSel((s) => (s + 1) % flat.length);
    } else if (e.key === "ArrowUp" || (e.key === "Tab" && e.shiftKey)) {
      e.preventDefault();
      if (flat.length) setSel((s) => (s - 1 + flat.length) % flat.length);
    } else if (e.key === "Home") {
      e.preventDefault();
      setSel(0);
    } else if (e.key === "End") {
      e.preventDefault();
      setSel(Math.max(0, flat.length - 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const it = flat[sel];
      if (it) {
        markKeyboardIntent();
        it.run();
      }
    }
  };
  const onCancel = (e: SyntheticEvent<HTMLDialogElement>) => {
    e.preventDefault();
    markKeyboardIntent();
    close();
  };
  const groupLabel: Record<PaletteGroup, string> = { actions: t("palette.groupActions"), scopes: t("palette.groupScopes"), pages: t("palette.groupPages"), tasks: t("palette.groupTasks"), goals: t("palette.groupGoals"), agents: t("palette.groupAgents"), members: t("palette.groupMembers") };
  let i = -1;
  return (
    <dialog ref={ref} onClose={close} onCancel={onCancel} className="modal palette" data-theme="dark" aria-label={t("palette.title")} onClick={(e) => e.target === ref.current && close()}>
      {shown && (
        <div className="flex max-h-[min(60vh,520px)] flex-col">
          <div className="flex items-center gap-2.5 border-b border-hairline px-4">
            <span className="font-mono text-[15px] leading-none text-telemetry select-none" aria-hidden="true">›</span>
            <input
              ref={inputRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={onKey}
              placeholder={t("palette.placeholder")}
              className="h-11 min-w-0 flex-1 bg-transparent text-body text-ink outline-none placeholder:text-ink-tertiary"
              autoComplete="off"
              autoCorrect="off"
              spellCheck={false}
              role="combobox"
              aria-expanded="true"
              aria-controls="palette-list"
              aria-activedescendant={flat[sel] ? `palette-${flat[sel].id}` : undefined}
              aria-autocomplete="list"
            />
            <Kbd>Esc</Kbd>
          </div>
          <ul ref={listRef} id="palette-list" role="listbox" className="m-0 min-h-0 flex-1 list-none overflow-y-auto p-1.5">
            {flat.length === 0 && <li className="px-3 py-6 text-center text-caption text-ink-subtle">{loggedIn && !index && q ? t("common.loading") : t("palette.empty")}</li>}
            {[...results.entries()].map(([g, list]) => (
              <li key={g} role="presentation" className="pt-1.5 first:pt-0">
                <div className="eyebrow px-2.5 pb-1 text-ink-subtle" aria-hidden="true">{groupLabel[g]}</div>
                <ul role="group" aria-label={groupLabel[g]} className="m-0 list-none p-0">
                  {list.map((it) => {
                    i++;
                    const k = i;
                    const active = k === sel;
                    return (
                      <li
                        key={it.id}
                        id={`palette-${it.id}`}
                        data-i={k}
                        role="option"
                        aria-selected={active}
                        className={cx("palette-item flex h-9 cursor-default items-center gap-3 rounded-md px-2.5 text-body text-ink", active && "is-active")}
                        onMouseMove={() => !active && setSel(k)}
                        onClick={() => it.run()}
                      >
                        <span className="inline-flex w-4 shrink-0 justify-center text-ink-subtle" aria-hidden="true">{it.icon}</span>
                        <span className="min-w-0 flex-1 truncate">{it.title}</span>
                        {it.hint && <span className="eyebrow shrink-0 text-ink-subtle"><span className="normal-case">{it.hint}</span></span>}
                      </li>
                    );
                  })}
                </ul>
              </li>
            ))}
          </ul>
          <div className="flex items-center justify-between gap-3 border-t border-hairline px-4 py-2 eyebrow text-ink-tertiary">
            <span className="normal-case">{t("palette.hint")}</span>
            <span className="text-telemetry">{t("palette.title")}</span>
          </div>
        </div>
      )}
    </dialog>
  );
}
