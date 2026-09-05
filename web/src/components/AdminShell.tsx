"use client";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { api, ApiError, MOCK, type AdminUser } from "@/lib/api";
import { t } from "@/lib/i18n";
import { AdminBridgeBar } from "./BridgeBar";
import { IconHome, IconLogout, IconOrg, IconTag, IconUser, ShipMark } from "./icons";
import { Avatar, Tag, cx } from "./ui";

interface AdminCtx {
  admin: AdminUser | null;
  refresh: () => void;
  logout: () => Promise<void>;
}
const Ctx = createContext<AdminCtx>({ admin: null, refresh: () => {}, logout: async () => {} });
export const useAdmin = () => useContext(Ctx);

const norm = (p: string) => (p.length > 1 ? p.replace(/\/+$/, "") : p);

/** 平台后台的外壳：独立会话（Cookie axiomos_admin）、canvas 底顶栏 + 底部细线，与组织外壳互不相干。 */
export function AdminShell({ children }: { children: ReactNode }) {
  const pathname = norm(usePathname() || "/admin");
  const router = useRouter();
  const isLogin = pathname === "/admin/login";
  const [admin, setAdmin] = useState<AdminUser | null>(null);
  const [checked, setChecked] = useState(false);
  const [tick, setTick] = useState(0);
  const refresh = useCallback(() => setTick((x) => x + 1), []);

  useEffect(() => {
    let alive = true;
    api.admin.me().then(
      (r) => { if (alive) { setAdmin(r.admin); setChecked(true); } },
      (e: unknown) => {
        if (!alive) return;
        setAdmin(null); setChecked(true);
        if (e instanceof ApiError && e.status === 401 && !isLogin) router.replace("/admin/login/");
      },
    );
    return () => { alive = false; };
  }, [tick, isLogin, router]);

  const logout = useCallback(async () => {
    await api.admin.logout();
    setAdmin(null);
    router.replace("/admin/login/");
  }, [router]);

  const value = { admin, refresh, logout };
  if (isLogin) return <Ctx.Provider value={value}>{children}</Ctx.Provider>;

  const nav = [
    { href: "/admin", label: t("admin.nav.overview"), icon: <IconHome /> },
    { href: "/admin/organizations", label: t("admin.nav.organizations"), icon: <IconOrg /> },
    { href: "/admin/pricing", label: t("admin.nav.pricing"), icon: <IconTag /> },
    { href: "/admin/admins", label: t("admin.nav.admins"), icon: <IconUser /> },
  ];

  return (
    <Ctx.Provider value={value}>
      <div className="flex min-h-screen flex-col bg-canvas">
        <header className="border-b border-hairline bg-canvas text-ink-subtle">
          <div className="flex h-14 w-full items-center gap-4 px-4 md:px-6">
            <Link href="/admin/" className="flex items-center gap-2.5" title="AxiomOS">
              <ShipMark size={24} className="text-ink" />
              <span className="flex flex-col leading-none">
                <span className="text-[15px] leading-5 font-medium tracking-[-0.2px] text-ink">AxiomOS</span>
                <span className="eyebrow hidden text-ink-subtle sm:block">{t("bridge.console")}</span>
              </span>
            </Link>
            <nav className="ml-2 flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto">
              {nav.map((n) => {
                const active = n.href === "/admin" ? pathname === "/admin" : pathname.startsWith(n.href);
                return (
                  <Link key={n.href} href={`${n.href}/`} className={cx("nav-item", "!py-1.5")} aria-current={active ? "page" : undefined}>
                    {n.icon}
                    <span>{n.label}</span>
                  </Link>
                );
              })}
            </nav>
            <div className="flex shrink-0 items-center gap-3">
              {MOCK && <Tag tone="warning">{t("common.mockMode")}</Tag>}
              {admin ? (
                <span className="flex items-center gap-2">
                  <Avatar name={admin.name} size={24} />
                  <span className="hidden text-body text-ink sm:inline">{admin.name}</span>
                  <button type="button" onClick={() => void logout()} className="pressable rounded-md p-1 text-ink-subtle hover:bg-surface-2 hover:text-ink" title={t("admin.logout")} aria-label={t("admin.logout")}>
                    <IconLogout />
                  </button>
                </span>
              ) : (
                <span className="text-caption">{checked ? t("common.notLoggedIn") : "…"}</span>
              )}
            </div>
          </div>
        </header>
        {/* 舰桥状态栏（后台版）+ 坐标网格画布 */}
        <AdminBridgeBar live={!!admin} />
        <div className="bridge-canvas flex min-w-0 flex-1 flex-col">
          <main key={pathname} className="page-enter w-full min-w-0 flex-1 p-4 md:p-6 3xl:px-8">{children}</main>
        </div>
      </div>
    </Ctx.Provider>
  );
}
