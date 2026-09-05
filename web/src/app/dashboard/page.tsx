"use client";
import { Redirect } from "@/components/Redirect";

// 旧地址：/dashboard → /overview/?tab=cost（保留原查询串，DESIGN.md §10）
const PARAMS = { tab: "cost" };
export default function Page() {
  return <Redirect to="/overview/" params={PARAMS} />;
}
