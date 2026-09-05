"use client";
import { Redirect } from "@/components/Redirect";

// 旧地址：/gantt → /tasks/?view=gantt（保留原查询串，DESIGN.md §10）
const PARAMS = { view: "gantt" };
export default function Page() {
  return <Redirect to="/tasks/" params={PARAMS} />;
}
