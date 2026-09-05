"use client";
import { useState } from "react";
import { useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { ErrorBox, ListSkeleton, PageHeader, Panel } from "@/components/ui";
import { BasicTab } from "./BasicTab";
import { CapabilitiesTab } from "./CapabilitiesTab";
import { InvitationsTab } from "./InvitationsTab";
import { MembersTab } from "./MembersTab";
import { PricingTab } from "./PricingTab";
import { RolesTab } from "./RolesTab";
import { TeamsTab } from "./TeamsTab";
import { VisibilityTab } from "./VisibilityTab";
import { WorkflowsTab } from "./WorkflowsTab";
import { WorkspaceTab } from "./WorkspaceTab";

// 「流程」原来是独立入口，按 DESIGN.md §10 并入组织设置（/task-types 跳到 ?tab=workflows）
const TABS = ["basic", "visibility", "workspace", "members", "invitations", "roles", "teams", "capabilities", "pricing", "workflows"] as const;
type Tab = (typeof TABS)[number];

export default function SettingsPage() {
  const { session, canManageOrg } = useSession();
  const qTab = useQueryParam("tab");
  const [tab, setTab] = useState<Tab | null>(null);
  // 「流程」对每个成员只读可见（改流程才要「管理流程」权限）；其余页签仍只给组织负责人 / 持有「组织设置」权限的人
  const visible: readonly Tab[] = canManageOrg ? TABS : ["workflows"];
  const current: Tab = tab ?? (visible.includes(qTab as Tab) ? (qTab as Tab) : visible[0]);

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
          {current === "visibility" && <VisibilityTab onGoTeams={() => { setTab("teams"); window.history.replaceState(null, "", "?tab=teams"); }} />}
          {current === "workspace" && <WorkspaceTab />}
          {current === "members" && <MembersTab />}
          {current === "invitations" && <InvitationsTab />}
          {current === "roles" && <RolesTab />}
          {current === "teams" && <TeamsTab />}
          {current === "capabilities" && <CapabilitiesTab />}
          {current === "pricing" && <PricingTab />}
          {current === "workflows" && <WorkflowsTab standalone={!canManageOrg} />}
        </div>
      </div>
    </div>
  );
}
