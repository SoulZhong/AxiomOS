"use client";
import { Redirect } from "@/components/Redirect";

// 旧地址：/board → /tasks/?view=board（保留原查询串，DESIGN.md §10）
const PARAMS = { view: "board" };
export default function Page() {
  return <Redirect to="/tasks/" params={PARAMS} />;
}
