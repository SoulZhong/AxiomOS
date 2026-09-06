"use client";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useState, useSyncExternalStore, type ReactNode } from "react";
import { prefersReducedMotion } from "@/lib/motion";
import { api, ApiError, MOCK, type Session } from "@/lib/api";
import { normalizeLocale, setLocale, t } from "@/lib/i18n";
import { clearPreferences, loadPreferences } from "@/lib/preferences";
import { toggleSidebar, useSidebarCollapsed } from "@/lib/sidebar";
import { applyScopeSession } from "@/lib/useScope";
import { BridgeBar } from "./BridgeBar";
import { IconAgent, IconApprove, IconBacklog, IconBoard, IconChart, IconChevronLeft, IconChevronRight, IconClose, IconFlow, IconGanttFlight, IconGoal, IconHome, IconKeyboard, IconLogout, IconMenu, IconOrg, IconOverview, IconSearch, IconSettings, IconSprint, IconTask, ShipMark } from "./icons";
import { CommandPalette } from "./palette/CommandPalette";
import { useShipTelemetry } from "./ship-status/telemetry";
import { ChordIndicator, ShortcutHelp, useGlobalShortcuts, useIsMac } from "./shortcuts";
import { arrival } from "./space/arrival";
import { BOARDING_FLAG } from "./space/sceneBus";
import { Avatar, Kbd, Tag, Tip, cx } from "./ui";

interface SessionCtx {
  session: Session | null;
  /** 能否打开组织设置：组织负责人，或持有 org_settings 权限的角色 */
  canManageOrg: boolean;
  /** 重新读取会话（登录后调用） */
  refresh: () => void;
  logout: () => Promise<void>;
}
const Ctx = createContext<SessionCtx>({ session: null, canManageOrg: false, refresh: () => {}, logout: async () => {} });
export const useSession = () => useContext(Ctx);

const norm = (p: string) => (p.length > 1 ? p.replace(/\/+$/, "") : p);

/** 舰桥标识牌：组织标识 · 货币，如 `DEMO · CNY`。没有 slug 时用组织名。 */
export const orgEyebrow = (org: Session["organization"] | null | undefined) => (org ? `${(org.slug ?? org.name).toUpperCase()} · ${org.currency}` : "— · —");

export function AppShell({ children }: { children: ReactNode }) {
  const pathname = norm(usePathname() || "/");
  const router = useRouter();
  const [session, setSession] = useState<Session | null>(null);
  const [canManageOrg, setCanManageOrg] = useState(false);
  const [checked, setChecked] = useState(false);
  const isLogin = pathname === "/login";
  // 平台后台与邀请页不走组织会话，也不套这层外壳
  const bare = pathname === "/admin" || pathname.startsWith("/admin/") || pathname.startsWith("/invite/");

  const [tick, setTick] = useState(0);
  const refresh = useCallback(() => setTick((tk) => tk + 1), []);

  // 登舰抵达：登录页在跳转前写下 sessionStorage 标记，这里在绘制前盖上同一层暖白并让它 500ms 退去，应用不会"啪"地出现
  const arriving = useSyncExternalStore(arrival.subscribe, arrival.get, arrival.getServer);
  useLayoutEffect(() => {
    if (isLogin || bare) return;
    let flagged = false;
    try {
      flagged = sessionStorage.getItem(BOARDING_FLAG) === "1";
      if (flagged) sessionStorage.removeItem(BOARDING_FLAG);
    } catch {}
    if (!flagged) return;
    arrival.set(true);
    const id = window.setTimeout(() => arrival.set(false), 700);
    return () => window.clearTimeout(id);
  }, [pathname, isLogin, bare]);

  useEffect(() => {
    if (bare) return;
    let alive = true;
    api.auth.me().then(
      async (s) => {
        if (!alive) return;
        setSession(s);
        setChecked(true);
        // 范围：可选档位、默认档、财务策略都以会话为准（ADR 0013）
        applyScopeSession(s);
        // 登录后以成员自己的语言为准
        const l = normalizeLocale(s.member.locale);
        if (l) setLocale(l);
        // 显示偏好（DESIGN.md §20）：列、卡片字段、默认页签、紧凑、侧栏默认
        void loadPreferences();
        // 组织设置入口：负责人，或持有 org_settings 权限。会话里有 is_owner / permissions 就直接用；
        // 老后端没有这两个字段时再查 GET /org/roles（失败即视为不可见）。
        if ((s.is_owner ?? s.organization.owner_id === s.member.id) || s.permissions?.includes("org_settings")) {
          setCanManageOrg(true);
        } else if (s.permissions) {
          setCanManageOrg(false);
        } else {
          try {
            const roles = await api.org.roles();
            if (alive) setCanManageOrg(roles.some((r) => s.member.roles.includes(r.name) && r.permissions.includes("org_settings")));
          } catch {
            if (alive) setCanManageOrg(false);
          }
        }
      },
      (e: unknown) => {
        if (!alive) return;
        setSession(null);
        setCanManageOrg(false);
        setChecked(true);
        applyScopeSession(null);
        clearPreferences();
        if (e instanceof ApiError && e.status === 401 && !isLogin) router.replace("/login/");
      },
    );
    return () => {
      alive = false;
    };
  }, [tick, isLogin, bare, router]);

  const logout = async () => {
    await api.auth.logout();
    setSession(null);
    setCanManageOrg(false);
    clearPreferences();
    router.replace("/login/");
  };

  const value = { session, canManageOrg, refresh, logout };
  if (isLogin || bare) {
    return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
  }
  return (
    <Ctx.Provider value={value}>
      {arriving && <div className="ax-arrive" aria-hidden="true" />}
      <Shell pathname={pathname} session={session} checked={checked} canManageOrg={canManageOrg} logout={logout} landing={arriving}>
        {children}
      </Shell>
    </Ctx.Provider>
  );
}

function Shell({ pathname, session, checked, canManageOrg, logout, landing, children }: { pathname: string; session: Session | null; checked: boolean; canManageOrg: boolean; logout: () => Promise<void>; landing?: boolean; children: ReactNode }) {
  const [navOpen, setNavOpen] = useState(false);
  const [help, setHelp] = useState(false);
  const [palette, setPalette] = useState(false);
  const openHelp = useCallback(() => setHelp(true), []);
  const togglePalette = useCallback(() => setPalette((p) => !p), []);
  // 侧栏收起 / 展开：底部按钮或 `[`；偏好存 localStorage，首屏由内联脚本写到 html[data-sidebar]，宽度与标签显隐全在 CSS（globals.css「侧栏收起」）
  const collapsed = useSidebarCollapsed();
  const onSidebar = useCallback(() => toggleSidebar(), []);
  useGlobalShortcuts({ onHelp: openHelp, onPalette: togglePalette, onSidebar });
  const mac = useIsMac();
  // 路由切换时面板"落定"：给 main 里的一级面板（.hud）按顺序编号，CSS 按 12ms 交错、160ms 从下方 4px 淡入（键盘导航 / reduced-motion 不动）
  useLayoutEffect(() => {
    if (prefersReducedMotion()) return;
    const main = document.querySelector("main.page-enter");
    main?.querySelectorAll<HTMLElement>(".hud").forEach((el, i) => el.style.setProperty("--i", String(i)));
  }, [pathname]);
  // 路由变化时收起移动端抽屉导航（渲染期同步，不用 effect）
  const [seenPath, setSeenPath] = useState(pathname);
  if (seenPath !== pathname) {
    setSeenPath(pathname);
    setNavOpen(false);
  }

  // 侧栏「我的工作」的角标 = 待我处理的条数（DESIGN.md §12）：与舷窗带共用同一份 30s 遥测（ship-status/telemetry），不另起轮询
  const inbox = useShipTelemetry(!!session)?.inbox ?? 0;
  // 六个入口（DESIGN.md §10、§12，CONTEXT.md「入口」）：看板 / 甘特图 / 迭代 / 待领取是「任务」的页签，运营数据是「组织概览」的页签，流程是「组织设置」的页签，
  // 待确认操作并入「我的工作」的「待我处理」（历史记录 /proposals/ 只从那里和指令台进）。
  // 签名图标（提案 §二）：目标 = 靴中的芽，任务 = 方块，Agent = 视窗；我的工作 / 概览 / 设置沿用通用图标
  const nav: Array<{ href: string; label: string; icon: ReactNode; badge?: number; match?: (p: string) => boolean }> = [
    { href: "/", label: t("nav.home"), icon: <IconHome />, badge: inbox },
    { href: "/overview", label: t("nav.overview"), icon: <IconOverview /> },
    { href: "/goals", label: t("nav.goals"), icon: <IconGoal /> },
    // 迭代详情（/sprints/[id]）仍属于「任务」入口
    { href: "/tasks", label: t("nav.tasks"), icon: <IconTask />, match: (p) => p.startsWith("/tasks") || p.startsWith("/sprints") },
    { href: "/agents", label: t("nav.agents"), icon: <IconAgent /> },
    // 组织设置只给有入口的人；其他人也有「设置」：我的偏好（DESIGN.md §20）与只读的流程
    { href: "/settings", label: canManageOrg ? t("nav.settings") : t("nav.settingsMine"), icon: <IconSettings /> },
  ];
  // 指令台里除六个入口外，再列出各页签的直达项（「任务 · 看板」……）与待确认操作的历史记录，地址带查询串
  const sub = (base: string, label: string, tabs: Array<[string, string, ReactNode]>) => tabs.map(([q, tl, icon]) => ({ href: `${base}${q}`, label: `${label} · ${tl}`, icon }));
  const palettePages = [
    ...nav.map((n) => ({ href: n.href === "/" ? "/" : `${n.href}/`, label: n.label, icon: n.icon })),
    ...sub("/tasks/?view=", t("nav.tasks"), [["board", t("tasks.view.board"), <IconBoard key="b" />], ["gantt", t("tasks.view.gantt"), <IconGanttFlight key="g" />], ["sprints", t("tasks.view.sprints"), <IconSprint key="s" />], ["backlog", t("tasks.view.backlog"), <IconBacklog key="k" />]]),
    ...sub("/overview/?tab=", t("nav.overview"), [["cost", t("overview.tab.cost"), <IconChart key="c" />], ["efficiency", t("overview.tab.efficiency"), <IconChart key="e" />], ["agents", t("overview.tab.agents"), <IconAgent key="a" />]]),
    { href: "/proposals/", label: `${t("proposals.title")} · ${t("proposals.history")}`, icon: <IconApprove key="p" /> },
    // 「流程」每个成员都能查看（只读），不受组织设置入口的门槛限制；没有组织设置入口的人看到的就叫「流程」
    ...(canManageOrg ? sub("/settings/?tab=", t("nav.settings"), [["me", t("settings.tab.me"), <IconSettings key="m" />], ["workflows", t("settings.tab.workflows"), <IconFlow key="w" />]]) : [{ href: "/settings/?tab=me", label: `${t("nav.settingsMine")} · ${t("settings.tab.me")}`, icon: <IconSettings key="m" /> }, { href: "/settings/?tab=workflows", label: t("taskTypes.title"), icon: <IconFlow key="w" /> }]),
  ];
  // 收起态（显式收起，或 768–1199 没有偏好）只显示图标 + 气泡，文字藏起来（.sb-x）；展开态完整；<768 变抽屉（始终完整）。
  const labelCls = "sb-x min-w-0 truncate";
  /** 收起时图标带右侧气泡说明；展开时气泡不显示（CSS 只在收起态放出 .sidebar 里的 tip） */
  const navTip = (label: string, children: ReactNode) => <Tip tip={label} placement="right" className="sb-tip">{children}</Tip>;
  const roleLine = session ? (session.member.roles.length ? session.roles.filter((r) => session.member.roles.includes(r.name)).map((r) => r.title).join(" · ") : session.member.email) : "";

  return (
    <div className={cx("flex min-h-screen bg-canvas", landing && "ax-landing")}>
      {navOpen && <div className="fade-in fixed inset-0 z-40 bg-overlay md:hidden" onClick={() => setNavOpen(false)} aria-hidden="true" />}
      {/* 移动端抽屉导航：从左侧滑入 200ms（iOS 抽屉曲线），退回 150ms 强 ease-out；reduced-motion 时不过渡 */}
      <aside
        className={cx(
          "sidebar fixed inset-y-0 left-0 z-50 flex w-[220px] shrink-0 flex-col overflow-hidden border-r border-hairline bg-canvas text-ink-subtle transition-transform motion-reduce:transition-none md:sticky md:top-0 md:h-screen md:translate-x-0",
          navOpen ? "translate-x-0 duration-200 ease-drawer" : "-translate-x-full duration-150 ease-out",
        )}
        aria-label={t("nav.menu")}
      >
        {/* 1. 侧边栏底部区域允许的那一层最淡星点（主内容区不允许） */}
        <div className="starfield-sidebar pointer-events-none absolute inset-x-0 bottom-0 h-[38%]" aria-hidden="true" />

        {/* 2+3. 舰徽 + 字标同一行，下面是舰桥标识牌 */}
        <div className="sb-head relative flex items-start justify-between gap-2 px-4 pt-4 pb-3">
          <Link href="/" className="sb-brand flex min-w-0 flex-col gap-1" title="AxiomOS">
            <span className="sb-row flex items-center gap-2.5">
              <ShipMark size={24} className="shrink-0 text-ink" />
              <span className="sb-x truncate text-[15px] leading-6 font-medium tracking-[-0.2px] text-ink">AxiomOS</span>
            </span>
            {/* 组织标识牌：默认组织头像 = 客轮剪影（14px）+ `DEMO · CNY`；收起时只剩头像，名字进气泡 */}
            <span className="sb-row flex items-center gap-1.5 text-ink-subtle">
              {navTip(session ? orgEyebrow(session.organization) : "— · —", <IconOrg size={14} className="shrink-0" />)}
              <span className="sb-x eyebrow truncate">{session ? orgEyebrow(session.organization) : "— · —"}</span>
            </span>
          </Link>
          <button type="button" onClick={() => setNavOpen(false)} className="pressable rounded-md p-1 text-ink-subtle hover:text-ink md:hidden" aria-label={t("common.close")}>
            <IconClose />
          </button>
        </div>
        <nav className="sb-nav relative flex flex-1 flex-col gap-0.5 px-3">
          {nav.map((n) => {
            const active = n.href === "/" ? pathname === "/" : n.match ? n.match(pathname) : pathname.startsWith(n.href);
            return (
              <Link key={n.href} href={n.href === "/" ? "/" : `${n.href}/`} className="nav-item sb-row" aria-current={active ? "page" : undefined}>
                {navTip(n.badge ? `${n.label} · ${t("inbox.countTip", { n: n.badge })}` : n.label, <span className="relative shrink-0">{n.icon}{!!n.badge && <span className="nav-dot sb-n" aria-hidden="true" />}</span>)}
                <span className={labelCls}>{n.label}</span>
                {!!n.badge && <span className="sb-x nav-badge">{n.badge}</span>}
              </Link>
            );
          })}
          {/* 指令台（⌘K）、快捷键说明与收起按钮在侧栏底部；收起时三者都还是图标 */}
          <button type="button" onClick={togglePalette} className="nav-item sb-row mt-auto">
            {navTip(t("palette.title"), <span className="shrink-0"><IconSearch /></span>)}
            <span className={labelCls}>{t("palette.title")}</span>
            <span className="sb-x ml-auto"><Kbd>{mac ? "⌘K" : "Ctrl K"}</Kbd></span>
          </button>
          <button type="button" onClick={openHelp} className="nav-item sb-row">
            {navTip(t("shortcuts.title"), <span className="shrink-0"><IconKeyboard /></span>)}
            <span className={labelCls}>{t("shortcuts.title")}</span>
          </button>
          <button type="button" onClick={onSidebar} className="nav-item sb-row sb-toggle" aria-expanded={!collapsed} aria-label={collapsed ? t("nav.expand") : t("nav.collapse")}>
            {navTip(collapsed ? t("nav.expand") : t("nav.collapse"), <span className="shrink-0">{collapsed ? <IconChevronRight /> : <IconChevronLeft />}</span>)}
            <span className={labelCls}>{collapsed ? t("nav.expand") : t("nav.collapse")}</span>
            <span className="sb-x ml-auto"><Kbd>[</Kbd></span>
          </button>
        </nav>
        <div className="sb-foot relative mt-2 border-t border-hairline px-4 py-3">
          {MOCK && (
            <div className="sb-x mb-2">
              <Tag tone="warning">{t("common.mockMode")}</Tag>
            </div>
          )}
          {session ? (
            <div className="sb-row flex items-center gap-2.5">
              {navTip(session.member.name, <Avatar name={session.member.name} size={32} />)}
              <span className="sb-x min-w-0 flex-1">
                <span className="block truncate text-body leading-[1.3] font-medium text-ink">{session.member.name}</span>
                <span className="block truncate text-caption text-ink-subtle">{roleLine}</span>
              </span>
              <button type="button" onClick={() => void logout()} className="sb-x pressable rounded-md p-1 text-ink-subtle hover:bg-surface-2 hover:text-ink" title={t("common.logout")} aria-label={t("common.logout")}>
                <IconLogout />
              </button>
            </div>
          ) : (
            <span className="sb-x text-caption">{checked ? t("common.notLoggedIn") : "…"}</span>
          )}
          {/* 主题 / 语言切换在舰桥状态栏右侧；页脚只留成员与退出。收起态：退出成为一枚居中图标 */}
          {session && (
            <button type="button" onClick={() => void logout()} className="sb-n pressable mt-2 w-full justify-center rounded-md p-1 text-ink-subtle hover:bg-surface-2 hover:text-ink" title={t("common.logout")} aria-label={t("common.logout")}>
              <IconLogout />
            </button>
          )}
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        {/* 移动端顶栏：只在 <768 出现 */}
        <header className="flex h-12 items-center gap-3 border-b border-hairline bg-canvas px-4 md:hidden">
          <button type="button" onClick={() => setNavOpen(true)} className="pressable rounded-md p-1 text-ink-subtle hover:bg-surface-2" aria-label={t("nav.menu")}>
            <IconMenu />
          </button>
          <ShipMark size={20} className="text-ink" animate={false} />
          <span className="text-body font-medium text-ink">AxiomOS</span>
          <span className="eyebrow truncate text-ink-subtle">{session ? orgEyebrow(session.organization) : ""}</span>
        </header>
        {/* 1. 舰桥状态栏：每一页都在 */}
        <BridgeBar org={session?.organization} live={!!session} />
        {/* 2. 坐标网格画布；12. 入场：key 随路由变化，重新播放 120ms 淡入 + 2px 上移（键盘导航 g t / g g 时 html[data-kbd] 让它跳过）。
            内容区不设最大宽度：表格 / 面板网格随视口铺满，只有长文（页头说明、设置表单）自己限宽 */}
        <div className="bridge-canvas flex min-w-0 flex-1 flex-col">
          <main key={pathname} className="page-enter w-full min-w-0 flex-1 p-4 md:p-6 3xl:px-8">{children}</main>
        </div>
      </div>
      <ShortcutHelp open={help} onClose={() => setHelp(false)} />
      <ChordIndicator />
      <CommandPalette open={palette} onClose={() => setPalette(false)} onHelp={openHelp} pages={palettePages} loggedIn={!!session} canManageOrg={canManageOrg} />
    </div>
  );
}
