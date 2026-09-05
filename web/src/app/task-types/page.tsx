"use client";
import { Redirect } from "@/components/Redirect";

// 旧地址：/task-types → /settings/?tab=workflows（保留原查询串，DESIGN.md §10）
const PARAMS = { tab: "workflows" };
export default function Page() {
  return <Redirect to="/settings/" params={PARAMS} />;
}
