import type { ReactNode } from "react";
import { AdminShell } from "@/components/AdminShell";

// 平台后台：/admin/... 不套组织外壳（AppShell 对 /admin 路径直接透传），自己有一层外壳与登录页。
export default function AdminLayout({ children }: { children: ReactNode }) {
  return <AdminShell>{children}</AdminShell>;
}
