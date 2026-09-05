import { TaskDetail } from "./TaskDetail";

// 静态导出：只预渲染占位页 "_"，真实 id 在客户端从地址栏读取（见 src/lib/hooks.ts useRouteId）。
// 开发模式下 next.config.ts 不开 export，任意 id 都能直接访问。
export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function TaskPage() {
  return <TaskDetail />;
}
