"use client";
import { Redirect } from "@/components/Redirect";

// 旧地址：/sprints → /tasks/?view=sprints（保留原查询串，DESIGN.md §10）
const PARAMS = { view: "sprints" };
export default function Page() {
  return <Redirect to="/tasks/" params={PARAMS} />;
}
