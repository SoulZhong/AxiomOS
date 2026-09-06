"use client";
import { useCallback, useMemo, useState } from "react";
import { api, inviteUrlOf, isSynced, type OrgMember, type OrgTeam } from "@/lib/api";
import { errorMessage, setQueryParams, useLoad, useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { usePersisted } from "@/lib/usePersisted";
import { useSession } from "@/components/AppShell";
import { IconChevronDown, IconChevronRight, IconDownload, IconPlus, IconUpload } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Checkbox, ConsequenceDialog, Empty, ErrorBox, ListSkeleton, Menu, Panel, Segmented, Select, Tag, TableSkeleton, Tip, cx } from "@/components/ui";
import { MergeDialog, type MergeSide } from "../MergeDialog";
import { BulkTeamDialog, ChangeRolesDialog, ChangeTeamDialog, ImportDialog, InviteDialog, MoveTeamDialog } from "./dialogs";
import { EditMemberDrawer } from "./MemberDrawer";
import { MemberTable, type PersonRow } from "./MemberTable";
import { TeamTree, type TreeActions } from "./TeamTree";
import { buildTree, countable, flattenTree, membersIn, pathOf, subtreeCounts } from "./tree";

/*
 * 「成员与团队」页签（DESIGN.md §16）：左边团队树，右边选中团队的成员表。
 * 原「成员」「团队」「邀请」三个页签合并到这里；?tab=members|teams|invitations 都跳到 ?tab=people。
 * 人数全部按成员表在前端算，改动后立刻反映在树上。
 * 待激活的人就是成员表里 status 为 pending_activation 的那些（后端在邀请时就建好了成员，带 invitation.id）；前端不再自己拼邀请行。
 * 疑似重复（ADR 0017 补记四）：表格里名字旁一个提示点、抽屉里一行提示，点开都是同一个合并对话框。
 */

type Status = "all" | "active" | "pending_activation" | "inactive";
const STATUS_RANK: Record<string, number> = { active: 0, pending_activation: 1, inactive: 2 };
const statusOf = (m: OrgMember): Exclude<Status, "all"> => (!m.active || m.status === "inactive" ? "inactive" : m.status === "pending_activation" ? "pending_activation" : "active");

export function PeopleTab() {
  const { session, refresh } = useSession();
  const toast = useToast();
  const members = useLoad(() => api.org.members(), []);
  const teams = useLoad(() => api.org.teams(), []);
  const roles = useLoad(() => api.org.roles(), []);
  const [selectedTeam, setSelectedTeam] = useState<string | null>(null);
  const [status, setStatus] = useState<Status>("all");
  const [directOnly, setDirectOnly] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [highlight, setHighlight] = useState<string | null>(null);
  const [expanded, setExpanded] = usePersisted<Record<string, boolean>>("axiomos.people.tree", {});
  // 对话框：每次打开换一个 key，让表单从头开始
  const [dialog, setDialog] = useState<null | { kind: "bulk" | "invite" | "import"; key: number }>(null);
  const [editing, setEditing] = useState<OrgMember | null>(null);
  const [teamFor, setTeamFor] = useState<OrgMember | null>(null);
  const [rolesFor, setRolesFor] = useState<OrgMember | null>(null);
  const [deactivating, setDeactivating] = useState<OrgMember | null>(null);
  const [owning, setOwning] = useState<OrgMember | null>(null);
  const [movingTeam, setMovingTeam] = useState<OrgTeam | null>(null);
  const [removingTeam, setRemovingTeam] = useState<OrgTeam | null>(null);
  const [revoking, setRevoking] = useState<PersonRow | null>(null);
  const [merging, setMerging] = useState<{ kind: "member"; a: MergeSide; b: MergeSide; reason?: string } | null>(null);
  const [busy, setBusy] = useState(false);
  // 停用前统计影响范围（DESIGN.md §6）：名下未结束任务、负责的目标、他的 Agent；只在对话框打开时取
  const deactivatingId = deactivating?.id ?? null;
  const impact = useLoad(async () => {
    if (!deactivatingId) return null;
    const [tasks, goals, agents] = await Promise.all([api.tasks.list({ assignee: deactivatingId, limit: 500 }).catch(() => []), api.goals.list().catch(() => []), api.agents.list().catch(() => [])]);
    const openTasks = tasks.filter((x) => x.state.label !== "terminal_success" && x.state.label !== "terminal_failure").length;
    const walk = (gs: typeof goals): number => gs.reduce((n, g) => n + (g.owner.id === deactivatingId && g.status !== "abandoned" && !g.achieved ? 1 : 0) + walk(g.children ?? []), 0);
    return { openTasks, goals: walk(goals), agents: agents.filter((a) => a.owner.id === deactivatingId).length };
  }, [deactivatingId]);

  const allMembers = useMemo<PersonRow[]>(() => members.data ?? [], [members.data]);
  const allTeams = useMemo(() => teams.data ?? [], [teams.data]);
  const roleList = roles.data ?? [];
  const orgName = session?.organization.name ?? "";
  const reloadAll = useCallback(() => { members.reload(); teams.reload(); refresh(); }, [members, teams, refresh]);

  const counts = useMemo(() => subtreeCounts(allMembers, allTeams), [allMembers, allTeams]);
  const total = useMemo(() => allMembers.filter(countable).length, [allMembers]);
  const team = allTeams.find((x) => x.id === selectedTeam) ?? null;
  const path = useMemo(() => pathOf(allTeams, selectedTeam), [allTeams, selectedTeam]);
  const inScope = useMemo(() => membersIn(allMembers, allTeams, team ? team.id : null, false), [allMembers, allTeams, team]);
  const scopeTotal = inScope.filter(countable).length;
  const scopeInactive = inScope.filter((m) => statusOf(m) === "inactive").length;
  const hasChildren = team ? allTeams.some((x) => x.parent_id === team.id && x.active !== false) : allTeams.length > 0;
  const visible = useMemo(() => {
    const base = membersIn(allMembers, allTeams, team ? team.id : null, directOnly && !!team);
    return base
      .filter((m) => status === "all" || statusOf(m) === status)
      .sort((a, b) => STATUS_RANK[statusOf(a)] - STATUS_RANK[statusOf(b)] || Number(b.is_owner) - Number(a.is_owner) || a.name.localeCompare(b.name, "zh-Hans-CN"));
  }, [allMembers, allTeams, team, directOnly, status]);
  const selectedVisible = visible.filter((m) => selected.has(m.id));
  const inactiveView = status === "inactive";

  const selectTeam = (id: string | null, focusMember?: string) => {
    setSelectedTeam(id);
    setSelected(new Set());
    if (focusMember) {
      // 搜索命中：展开到那个团队，状态筛选放开，让人一定看得见，再高亮
      setStatus("all"); setDirectOnly(false);
      const open: Record<string, boolean> = {};
      for (const x of pathOf(allTeams, id)) open[x.id] = true;
      setExpanded((prev) => ({ ...prev, ...open }));
      setHighlight(focusMember);
      window.setTimeout(() => setHighlight((cur) => (cur === focusMember ? null : cur)), 2200);
      window.setTimeout(() => document.querySelector(`[data-member="${focusMember}"]`)?.scrollIntoView({ block: "center" }), 50);
    }
  };
  const openDialog = (kind: "bulk" | "invite" | "import") => setDialog({ kind, key: Date.now() });
  // ?member=<id>（快速命令「跳到成员」）：成员表一到就定位到那个人，参数随即清掉
  const qMember = useQueryParam("member");
  const [seenMember, setSeenMember] = useState<string | null>(null);
  if (qMember !== seenMember && members.data) {
    setSeenMember(qMember);
    const m = qMember ? allMembers.find((x) => x.id === qMember) : undefined;
    if (m) {
      selectTeam(m.team_id ?? null, m.id);
      setQueryParams({ member: null });
    }
  }

  // ---- 团队操作（树上的菜单） ----
  const act = async (fn: () => Promise<unknown>, ok: string): Promise<boolean> => {
    setBusy(true);
    try { await fn(); toast.ok(ok); reloadAll(); return true; } catch (err) { toast.fail(errorMessage(err)); return false; } finally { setBusy(false); }
  };
  const treeActions: TreeActions = {
    createTeam: async (parentId, name) => {
      let created: OrgTeam | null = null;
      const ok = await act(async () => { created = await api.org.createTeam({ name, parent_id: parentId }); }, t("settings.people.teamCreated", { name }));
      if (ok && created) { if (parentId) setExpanded((prev) => ({ ...prev, [parentId]: true })); selectTeam((created as OrgTeam).id); }
      return ok;
    },
    renameTeam: (tm, name) => act(() => api.org.updateTeam(tm.id, { name }), t("settings.people.teamRenamed", { name })),
    moveTeam: (tm) => setMovingTeam(tm),
    toggleBoundary: (tm) => void act(() => api.org.updateTeam(tm.id, { is_boundary: !tm.is_boundary }), t("toast.saved")),
    deactivateTeam: (tm) => setRemovingTeam(tm),
    reactivateTeam: (tm) => void act(() => api.org.updateTeam(tm.id, { active: true }), t("settings.people.teamRestored", { name: tm.name })),
  };
  const removeTeam = async (tm: OrgTeam) => {
    // 手工建的空团队直接删掉；同步来的只能停用（下次同步还会对上）
    const ok = await act(() => (isSynced(tm) ? api.org.updateTeam(tm.id, { active: false }) : api.org.deleteTeam(tm.id)), t("settings.people.teamRemoved", { name: tm.name }));
    if (ok) { setRemovingTeam(null); if (selectedTeam === tm.id) selectTeam(tm.parent_id ?? null); }
  };

  // ---- 成员操作（表格行的菜单） ----
  const rowActions = {
    open: (m: OrgMember) => setEditing(m),
    changeTeam: (m: OrgMember) => setTeamFor(m),
    changeRoles: (m: OrgMember) => setRolesFor(m),
    copyInvite: (m: OrgMember) => { const url = inviteUrlOf(m); if (!url) return; void navigator.clipboard?.writeText(url); toast.ok(t("common.copied")); },
    deactivate: (m: OrgMember) => setDeactivating(m),
    reactivate: (m: OrgMember) => void act(() => api.org.updateMember(m.id, { active: true }), t("settings.people.reactivated", { name: m.name })),
    makeOwner: (m: OrgMember) => setOwning(m),
    revokeInvite: (m: PersonRow) => setRevoking(m),
    merge: (m: OrgMember, otherId: string) => {
      const other = allMembers.find((x) => x.id === otherId);
      const hint = m.possible_duplicate_of?.find((d) => d.id === otherId);
      const side = (x: OrgMember): MergeSide => ({ id: x.id, name: x.name, team_path: pathOf(allTeams, x.team_id).map((p) => p.name).join(" / "), source: x.source, source_title: x.source_title, active: x.active });
      setMerging({ kind: "member", a: side(m), b: other ? side(other) : { id: otherId, name: hint?.name ?? otherId }, reason: hint?.reason_text });
    },
  };
  const revoke = async (m: PersonRow) => { const id = m.invitation?.id; if (id && (await act(() => api.org.deleteInvitation(id), t("toast.deleted")))) setRevoking(null); };
  const deactivate = async (m: OrgMember) => { if (await act(() => api.org.updateMember(m.id, { active: false }), t("settings.people.deactivated", { name: m.name }))) setDeactivating(null); };
  const makeOwner = async (m: OrgMember) => { if (await act(() => api.org.makeOwner(m.id), t("toast.saved"))) setOwning(null); };
  const bulkReactivate = async () => {
    const ids = selectedVisible.map((m) => m.id);
    if (await act(async () => { const r = await api.org.bulkMembers({ member_ids: ids, action: "reactivate" }); if (r.skipped.length) toast.fail(r.skipped.map((s) => s.reason).join("；")); }, t("settings.people.bulkDone", { n: ids.length }))) setSelected(new Set());
  };
  const exportCsv = async () => {
    setBusy(true);
    try {
      const text = await api.org.exportMembersCsv();
      const blob = new Blob(["\uFEFF" + text], { type: "text/csv;charset=utf-8" });
      const a = document.createElement("a");
      a.href = URL.createObjectURL(blob);
      a.download = "members.csv";
      a.click();
      window.setTimeout(() => URL.revokeObjectURL(a.href), 1000);
      toast.ok(t("settings.people.exportDone", { n: Math.max(0, text.trim().split(/\r?\n/).length - 1) }));
    } catch (err) { toast.fail(errorMessage(err)); } finally { setBusy(false); }
  };

  const loading = (members.loading && !members.data) || (teams.loading && !teams.data);
  const error = members.error ?? teams.error;
  const flatTeams = useMemo(() => flattenTree(buildTree(allTeams)), [allTeams]);
  const inviteAction = <Button size="sm" variant="primary" icon={<IconPlus />} onClick={() => openDialog("invite")}>{t("settings.people.invite")}</Button>;

  return (
    <Panel index={1} title={t("settings.tab.people")} telemetry={members.data ? t("settings.people.total", { n: total }) : undefined} padded={false} bodyClassName="[&>.tbl-wrap]:rounded-b-none">
      {loading ? (
        <div className="grid min-[1024px]:grid-cols-[240px_minmax(0,1fr)]">
          <div className="hidden border-r border-hairline min-[1024px]:block"><ListSkeleton rows={6} /></div>
          <TableSkeleton rows={6} cols={6} />
        </div>
      ) : error ? (
        <div className="p-4"><ErrorBox message={error} onRetry={reloadAll} /></div>
      ) : (
        <div className="grid min-[1024px]:grid-cols-[240px_minmax(0,1fr)]">
          {/* 左栏：团队树（<1024 折成下拉选择器） */}
          <aside className="hidden min-h-[520px] border-r border-hairline min-[1024px]:block" aria-label={t("settings.people.tree")}>
            {/* 树跟着视口走（表格很长时也不用滚回去找团队），自己在内部滚动 */}
            <TeamTree orgName={orgName} teams={allTeams} members={allMembers} counts={counts} total={total} selected={selectedTeam} onSelect={selectTeam} expanded={expanded} onToggle={(id, open) => setExpanded((prev) => ({ ...prev, [id]: open }))} actions={treeActions} className="sticky top-10 max-h-[calc(100vh-56px)] min-h-[520px]" />
          </aside>
          <div className="min-w-0">
            <div className="border-b border-hairline px-4 pt-3 pb-2 min-[1024px]:hidden">
              <label className="block">
                <span className="mb-1 block text-caption text-ink-subtle">{t("settings.people.pickTeam")}</span>
                <Select data-team-picker value={selectedTeam ?? ""} onChange={(e) => selectTeam(e.target.value || null)}>
                  <option value="">{orgName} · {total}</option>
                  {flatTeams.map(({ team: x, depth }) => <option key={x.id} value={x.id}>{"　".repeat(depth)}{x.name} · {counts.get(x.id) ?? 0}</option>)}
                </Select>
              </label>
            </div>
            {/* 右栏头部：面包屑、团队名与人数、已停用入口 */}
            <header className="border-b border-hairline px-4 pt-3 pb-2.5">
              <nav className="flex flex-wrap items-center gap-1 text-caption text-ink-subtle" aria-label={t("settings.title")}>
                <span>{t("settings.title")}</span>
                <IconChevronRight size={10} className="text-ink-tertiary" />
                <button type="button" className="hover:text-ink" onClick={() => selectTeam(null)}>{t("settings.tab.people")}</button>
                {path.map((x, i) => (
                  <span key={x.id} className="inline-flex items-center gap-1">
                    <IconChevronRight size={10} className="text-ink-tertiary" />
                    {i === path.length - 1 ? <span className="text-ink-muted">{x.name}</span> : <button type="button" className="hover:text-ink" onClick={() => selectTeam(x.id)}>{x.name}</button>}
                  </span>
                ))}
              </nav>
              <div className="mt-1 flex flex-wrap items-center justify-between gap-2">
                <div className="flex min-w-0 flex-wrap items-center gap-2">
                  <h3 className="text-title text-ink">{inactiveView ? t("settings.people.inactiveView") : team?.name ?? orgName}</h3>
                  <span className="text-caption text-ink-subtle">{inactiveView ? t("settings.teams.memberCount", { n: scopeInactive }) : t("settings.people.total", { n: scopeTotal })}{hasChildren && !inactiveView && <> · {t("settings.people.totalHint")}</>}</span>
                  {team?.is_boundary && <Tag tone="accent" title={t("settings.teams.boundaryOn")}>{t("settings.teams.boundaryTag")}</Tag>}
                  {team && isSynced(team) && <Tag title={t("settings.teams.nameSynced", { name: team.source_title ?? team.source })}>{t("settings.directory.from", { name: team.source_title ?? team.source })}</Tag>}
                  {team?.active === false && <Tag dark>{t("settings.people.teamInactive")}</Tag>}
                </div>
                <Button size="sm" variant={inactiveView ? "default" : "ghost"} aria-pressed={inactiveView} onClick={() => { setStatus(inactiveView ? "all" : "inactive"); setSelected(new Set()); }}>
                  {inactiveView ? t("settings.people.backToAll") : <>{t("settings.people.inactiveView")} <span className="telemetry text-ink-subtle">{scopeInactive}</span></>}
                </Button>
              </div>
            </header>
            {/* 筛选与操作条 */}
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-hairline px-4 py-2">
              {!inactiveView && <Segmented size="sm" value={status} onChange={(v) => { setStatus(v); setSelected(new Set()); }} options={[["all", t("settings.people.status.all")], ["active", t("settings.people.status.active")], ["pending_activation", t("settings.people.status.pending")], ["inactive", t("settings.people.status.inactive")]]} aria-label={t("settings.people.status")} />}
              {team && <Checkbox checked={directOnly} onChange={(e) => setDirectOnly(e.target.checked)} label={t("settings.people.directOnly")} title={t("settings.people.directOnlyHint")} className="text-[13px]" />}
              <div className="ml-auto flex flex-wrap items-center gap-2">
                {selectedVisible.length > 0 && (
                  <span className="inline-flex items-center gap-1.5 text-caption text-ink-subtle">
                    <span className="tabular-nums">{t("settings.people.selected", { n: selectedVisible.length })}</span>
                    <button type="button" className="text-accent hover:text-accent-hover" onClick={() => setSelected(new Set())}>{t("settings.people.clearSelection")}</button>
                  </span>
                )}
                {inactiveView ? (
                  <Tip tip={selectedVisible.length ? null : t("settings.people.bulkNeedSelection")}><Button size="sm" disabled={!selectedVisible.length || busy} onClick={() => void bulkReactivate()}>{t("settings.people.reactivate")}</Button></Tip>
                ) : (
                  <Tip tip={selectedVisible.length ? null : t("settings.people.bulkNeedSelection")}><Button size="sm" disabled={!selectedVisible.length} onClick={() => openDialog("bulk")}>{t("settings.people.bulkTeam")}</Button></Tip>
                )}
                <Menu size="sm" label={t("settings.people.importExport")} trigger={<>{t("settings.people.importExport")}<IconChevronDown size={12} className="opacity-70" /></>} items={[
                  { key: "import", label: t("settings.people.import"), icon: <IconUpload />, onSelect: () => openDialog("import") },
                  { key: "export", label: t("settings.people.export"), icon: <IconDownload />, onSelect: () => void exportCsv() },
                ]} />
                {inviteAction}
              </div>
            </div>
            {/* 成员表 */}
            {visible.length === 0 ? (
              <Empty
                text={inactiveView ? t("settings.people.emptyInactive") : status !== "all" || (directOnly && scopeTotal > 0) ? t("settings.people.emptyFilter") : team ? t("settings.people.emptyTeam") : t("settings.members.empty")}
                action={!inactiveView && status === "all" && (
                  <span className="inline-flex items-center gap-2">
                    <Button variant="primary" icon={<IconPlus />} onClick={() => openDialog("invite")}>{t("settings.people.invite")}</Button>
                    <Button icon={<IconUpload />} onClick={() => openDialog("import")}>{t("settings.people.import")}</Button>
                  </span>
                )}
              />
            ) : (
              <div className={cx(busy && "opacity-80")}>
                <MemberTable
                  members={visible}
                  teams={allTeams}
                  roles={roleList}
                  selected={selected}
                  onToggle={(id, checked) => setSelected((prev) => { const next = new Set(prev); if (checked) next.add(id); else next.delete(id); return next; })}
                  onToggleAll={(ids, checked) => setSelected((prev) => { const next = new Set(prev); for (const id of ids) { if (checked) next.add(id); else next.delete(id); } return next; })}
                  highlight={highlight}
                  canTransferOwner={!!session?.is_owner}
                  actions={rowActions}
                />
              </div>
            )}
          </div>
        </div>
      )}

      <EditMemberDrawer member={editing} roles={roleList.map((r) => [r.name, r.title])} teams={allTeams} onClose={() => setEditing(null)} onSaved={reloadAll} onMerge={(m, other) => { setEditing(null); rowActions.merge(m, other); }} />
      {merging && <MergeDialog key={`${merging.a.id}:${merging.b.id}`} kind="member" a={merging.a} b={merging.b} reason={merging.reason} onClose={() => setMerging(null)} onMerged={reloadAll} />}
      {teamFor && <ChangeTeamDialog key={teamFor.id} member={teamFor} teams={allTeams} onClose={() => setTeamFor(null)} onSaved={reloadAll} />}
      {rolesFor && <ChangeRolesDialog key={rolesFor.id} member={rolesFor} roles={roleList} onClose={() => setRolesFor(null)} onSaved={reloadAll} />}
      {movingTeam && <MoveTeamDialog key={movingTeam.id} team={movingTeam} teams={allTeams} onClose={() => setMovingTeam(null)} onMoved={reloadAll} />}
      {dialog?.kind === "bulk" && <BulkTeamDialog key={dialog.key} open members={selectedVisible} teams={allTeams} defaultTeam={selectedTeam} onClose={() => setDialog(null)} onDone={() => { setSelected(new Set()); reloadAll(); }} />}
      {dialog?.kind === "invite" && <InviteDialog key={dialog.key} open roles={roleList} teams={allTeams} defaultTeam={selectedTeam} onClose={() => setDialog(null)} onInvited={() => { reloadAll(); }} />}
      {dialog?.kind === "import" && <ImportDialog key={dialog.key} open roles={roleList} teams={allTeams} onClose={() => setDialog(null)} onImported={reloadAll} />}
      <ConsequenceDialog
        open={!!deactivating}
        title={deactivating ? t("settings.people.deactivateTitle", { name: deactivating.name }) : ""}
        counting={impact.loading && !impact.data}
        effects={[
          t("settings.people.deactivateEffect.login"),
          impact.data && impact.data.openTasks > 0 ? t("settings.people.deactivateEffect.tasks", { n: impact.data.openTasks }) : t("settings.people.deactivateEffect.noTasks"),
          impact.data && impact.data.goals > 0 ? t("settings.people.deactivateEffect.goals", { n: impact.data.goals }) : null,
          impact.data && impact.data.agents > 0 ? t("settings.people.deactivateEffect.agents", { n: impact.data.agents }) : null,
        ]}
        note={t("settings.people.deactivateEffect.restore")}
        confirmLabel={t("settings.people.deactivate")}
        danger
        busy={busy}
        onConfirm={() => deactivating && void deactivate(deactivating)}
        onClose={() => setDeactivating(null)}
      />
      <ConsequenceDialog open={!!revoking} title={revoking ? t("settings.people.revokeTitle", { email: revoking.email }) : ""} effects={[t("settings.people.revokeEffect.link"), t("settings.people.revokeEffect.member")]} confirmLabel={t("settings.people.revokeInvite")} danger busy={busy} onConfirm={() => revoking && void revoke(revoking)} onClose={() => setRevoking(null)} />
      <ConsequenceDialog open={!!owning} title={owning ? t("settings.members.makeOwnerTitle", { name: owning.name }) : ""} effects={[t("settings.members.makeOwnerEffect.you"), t("settings.members.makeOwnerEffect.agents")]} danger={false} busy={busy} onConfirm={() => owning && void makeOwner(owning)} onClose={() => setOwning(null)} />
      <ConsequenceDialog
        open={!!removingTeam}
        title={removingTeam ? t("settings.people.deactivateTeamTitle", { name: removingTeam.name }) : ""}
        effects={[t("settings.people.deactivateTeamEffect.tree"), removingTeam && isSynced(removingTeam) ? t("settings.people.deactivateTeamEffect.synced", { source: removingTeam.source_title ?? removingTeam.source ?? "" }) : t("settings.people.deactivateTeamEffect.manual")]}
        confirmLabel={t("settings.people.deactivateTeam")}
        danger
        busy={busy}
        onConfirm={() => removingTeam && void removeTeam(removingTeam)}
        onClose={() => setRemovingTeam(null)}
      />
    </Panel>
  );
}
