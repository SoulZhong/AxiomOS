import { InviteAccept } from "./InviteAccept";

// 静态导出：只预渲染占位页 "_"，真实 token 在客户端从地址栏读取（见 src/lib/hooks.ts useRouteId）。
// Go 服务需把 /invite/* 回落到 out/invite/_/index.html（见 README）。
export function generateStaticParams() {
  return [{ token: "_" }];
}

export default function InvitePage() {
  return <InviteAccept />;
}
