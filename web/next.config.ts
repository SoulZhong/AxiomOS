import type { NextConfig } from "next";
import { PHASE_DEVELOPMENT_SERVER } from "next/constants";

// 构建时静态导出：产物在 out/，由 Go 进程（cmd/axiomd）托管。
// trailingSlash 让每个页面导出成 <路径>/index.html，静态文件服务器不需要改写规则；
// 动态路由 /tasks/[id]、/goals/[id] 会导出为 tasks/_/index.html，服务端需把 /tasks/* 回落到它（见 README）。
// 开发服务器不开 export：否则 next dev 会拒绝 generateStaticParams 之外的动态参数（/tasks/T1 直接报错）。
const config = (phase: string): NextConfig => ({
  output: phase === PHASE_DEVELOPMENT_SERVER ? undefined : "export",
  trailingSlash: true,
  images: { unoptimized: true },
  reactStrictMode: true,
  // 开发服务器：把 /api 代理到后端（NEXT_PUBLIC_API_BASE 指向自己即可同源带 Cookie）；导出构建里没有 rewrites。
  // trailingSlash 的 308 会先于 rewrites 触发，所以开发时跳过它（页面链接本来就带斜杠）
  skipTrailingSlashRedirect: phase === PHASE_DEVELOPMENT_SERVER,
  rewrites: phase === PHASE_DEVELOPMENT_SERVER ? async () => [{ source: "/api/:path*", destination: `${process.env.DEV_API_PROXY ?? "http://localhost:8080"}/api/:path*` }] : undefined,
});

export default config;
