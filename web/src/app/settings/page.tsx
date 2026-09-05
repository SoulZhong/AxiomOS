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
import { WorkspaceTab } from "./WorkspaceTab";

const TABS = ["basic", "visibility", "workspace", "members", "invitations", "roles", "teams", "capabilities", "pricing"] as const;
type Tab = (typeof TABS)[number];

export default function SettingsPage() {
  const { session, canManageOrg } = useSession();
  const qTab = useQueryParam("tab");
  const [tab, setTab] = useState<Tab | null>(null);
  const current: Tab = tab ?? (TABS.includes(qTab as Tab) ? (qTab as Tab) : "basic");

  if (!session) return <Panel><ListSkeleton rows={4} /></Panel>;
  if (!canManageOrg) return <ErrorBox message={t("settings.forbidden")} />;

  return (
    <div>
      <PageHeader title={t("settings.title")} description={t("settings.description")} />
      <div className="grid gap-4 lg:grid-cols-[180px_minmax(0,1fr)]">
        <nav className="flex gap-1 overflow-x-auto lg:flex-col lg:gap-0.5" aria-label={t("settings.title")}>
          {TABS.map((k) => (
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
        </div>
      </div>
    </div>
  );
}
