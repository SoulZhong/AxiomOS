"use client";
import { useState } from "react";
import { useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { ErrorBox, ListSkeleton, PageHeader, Panel } from "@/components/ui";
import { BasicTab } from "./BasicTab";
import { CapabilitiesTab } from "./CapabilitiesTab";
import { DirectoryTab } from "./DirectoryTab";
import { PeopleTab } from "./people/PeopleTab";
import { PricingTab } from "./PricingTab";
import { RolesTab } from "./RolesTab";
import { VisibilityTab } from "./VisibilityTab";
import { WorkflowsTab } from "./WorkflowsTab";
import { WorkspaceTab } from "./WorkspaceTab";

// 「流程」原来是独立入口，按 DESIGN.md §10 并入组织设置（/task-types 跳到 ?tab=workflows）
// 「成员」「团队」「邀请」三个页签按 DESIGN.md §16 合并为「成员与团队」（people）；旧地址仍能打开
const TABS = ["basic", "visibility", "workspace", "people", "roles", "directory", "capabilities", "pricing", "workflows"] as const;
type Tab = (typeof TABS)[number];
const LEGACY: Record<string, Tab> = { members: "people", teams: "people", invitations: "people" };

export default function SettingsPage() {
  const { session, canManageOrg } = useSession();
  const qTab = useQueryParam("tab");
  const [tab, setTab] = useState<Tab | null>(null);
  // 「流程」对每个成员只读可见（改流程才要「管理流程」权限）；其余页签仍只给组织负责人 / 持有「组织设置」权限的人
  const visible: readonly Tab[] = canManageOrg ? TABS : ["workflows"];
  const wanted = qTab ? (LEGACY[qTab] ?? qTab) : null;
  const current: Tab = tab ?? (visible.includes(wanted as Tab) ? (wanted as Tab) : visible[0]);

  if (!session) return <Panel><ListSkeleton rows={4} /></Panel>;
  if (!visible.includes(current)) return <ErrorBox message={t("settings.forbidden")} />;

  return (
    <div>
      {canManageOrg ? <PageHeader title={t("settings.title")} description={t("settings.description")} /> : <PageHeader title={t("taskTypes.title")} description={t("taskTypes.description")} />}
      <div className={canManageOrg ? "grid gap-4 lg:grid-cols-[180px_minmax(0,1fr)]" : undefined}>
        <nav className="flex gap-1 overflow-x-auto lg:flex-col lg:gap-0.5" aria-label={t("settings.title")} hidden={visible.length < 2}>
          {visible.map((k) => (
            <button key={k} type="button" onClick={() => { setTab(k); window.history.replaceState(null, "", `?tab=${k}`); }} className="vtab shrink-0 whitespace-nowrap" aria-current={current === k ? "true" : undefined}>
              {t(`settings.tab.${k}`)}
            </button>
          ))}
        </nav>
        <div className="min-w-0">
          {current === "basic" && <BasicTab />}
          {current === "visibility" && <VisibilityTab onGoTeams={() => { setTab("people"); window.history.replaceState(null, "", "?tab=people"); }} />}
          {current === "workspace" && <WorkspaceTab />}
          {current === "people" && <PeopleTab />}
          {current === "roles" && <RolesTab />}
          {current === "directory" && <DirectoryTab />}
          {current === "capabilities" && <CapabilitiesTab />}
          {current === "pricing" && <PricingTab />}
          {current === "workflows" && <WorkflowsTab standalone={!canManageOrg} />}
        </div>
      </div>
    </div>
  );
}
