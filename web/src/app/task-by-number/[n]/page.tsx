import { ByNumber } from "./ByNumber";

// 静态导出：只预渲染占位页 "_"，真实序号在客户端从地址栏读取（与 /tasks/[id] 同一做法）。
export function generateStaticParams() {
  return [{ n: "_" }];
}

export default function TaskByNumberPage() {
  return <ByNumber />;
}
