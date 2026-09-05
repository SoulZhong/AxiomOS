"use client";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { t } from "@/lib/i18n";

/**
 * 旧地址的客户端跳转（静态导出没有服务端重定向，DESIGN.md §10「旧地址保留」）：
 * 保留原来的查询串，再叠上 params（如 view=board），replace 到新地址；页面本身只有一行「正在跳转…」。
 */
export function Redirect({ to, params }: { to: string; params?: Record<string, string> }) {
  const router = useRouter();
  useEffect(() => {
    const sp = new URLSearchParams(window.location.search);
    for (const [k, v] of Object.entries(params ?? {})) sp.set(k, v);
    const qs = sp.toString();
    router.replace(`${to}${qs ? `?${qs}` : ""}`);
  }, [router, to, params]);
  return <p className="text-caption text-ink-subtle" role="status">{t("common.redirecting")}</p>;
}
