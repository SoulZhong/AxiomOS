"use client";
import { Redirect } from "@/components/Redirect";

// 旧地址：/backlog → /tasks/?view=backlog（保留原查询串，DESIGN.md §10）
const PARAMS = { view: "backlog" };
export default function Page() {
  return <Redirect to="/tasks/" params={PARAMS} />;
}
