"use client";
import { useMemo, useState, type FormEvent } from "react";
import { api, type OrgMember, type OrgTeam } from "@/lib/api";
import { errorMessage, useAction, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconEdit, IconPlus, IconTrash } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Avatar, Button, Checkbox, ConfirmDialog, Drawer, Empty, ErrorBox, Field, Input, ListSkeleton, Panel, Select, Tag } from "@/components/ui";

function flatten(teams: OrgTeam[], parent: string | null = null, depth = 0): Array<{ team: OrgTeam; depth: number }> {
  return teams.filter((x) => (x.parent_id ?? null) === parent).flatMap((x) => [{ team: x, depth }, ...flatten(teams, x.id, depth + 1)]);
}
function descendants(teams: OrgTeam[], id: string): Set<string> {
  const out = new Set<string>([id]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const x of teams) if (x.parent_id && out.has(x.parent_id) && !out.has(x.id)) { out.add(x.id); grew = true; }
  }
  return out;
}

export function TeamsTab() {
  const { refresh } = useSession();
  const teams = useLoad(() => api.org.teams(), []);
  const members = useLoad(() => api.org.members(), []);
  // 两项策略都没选「按共享边界」时，边界标记不起作用，得说一句，别让人以为已经生效了
  const settings = useLoad(() => api.org.settings().catch(() => null), []);
  const [editing, setEditing] = useState<OrgTeam | { parent_id: string | null } | null>(null);
  const [removing, setRemoving] = useState<OrgTeam | null>(null);
  const { busy, run } = useAction();
  const flat = useMemo(() => flatten(teams.data ?? []), [teams.data]);
  const memberName = (id: string | null) => members.data?.find((m) => m.id === id)?.name ?? null;
  const boundaryUsed = !settings.data || settings.data.collaboration_visibility === "boundary" || settings.data.finance_visibility === "boundary";
  const anyBoundary = (teams.data ?? []).some((x) => x.is_boundary);

  const remove = async (tm: OrgTeam) => {
    if (await run(tm.id, () => api.org.deleteTeam(tm.id), t("toast.deleted"))) { setRemoving(null); teams.reload(); refresh(); }
  };

  return (
    <Panel index={1} title={t("settings.tab.teams")} padded={false} actions={<Button size="sm" variant="primary" icon={<IconPlus />} onClick={() => setEditing({ parent_id: null })}>{t("settings.teams.new")}</Button>}>
      {teams.loading && !teams.data ? <ListSkeleton rows={4} /> : teams.error ? <div className="p-4"><ErrorBox message={teams.error} onRetry={teams.reload} /></div> : !flat.length ? <Empty text={t("settings.teams.empty")} action={<Button variant="primary" icon={<IconPlus />} onClick={() => setEditing({ parent_id: null })}>{t("settings.teams.new")}</Button>} /> : (
        <ul>
          {flat.map(({ team: tm, depth }) => {
            const lead = memberName(tm.lead_id);
            return (
              <li key={tm.id} className="flex items-center gap-4 border-b border-hairline py-2.5 pr-3 last:border-b-0 hover:bg-surface-2" style={{ paddingLeft: 16 + depth * 24, boxShadow: tm.is_boundary ? "inset 2px 0 0 var(--c-accent)" : undefined }}>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 font-medium">
                    {tm.name}
                    {tm.is_boundary && <Tag tone="accent" title={t("settings.teams.boundaryOn")}>{t("settings.teams.boundaryTag")}</Tag>}
                  </div>
                  <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-caption text-ink-subtle">
                    <span className="inline-flex items-center gap-1">{lead && <Avatar name={lead} size={14} />}{t("settings.teams.lead")} {lead ?? t("settings.teams.noLead")}</span>
                    <span>·</span>
                    <span>{t("settings.teams.memberCount", { n: tm.member_ids.length })}{tm.member_ids.length > 0 && <>: {tm.member_ids.map((id) => memberName(id) ?? id).join(", ")}</>}</span>
                  </div>
                </div>
                <span className="row-actions inline-flex shrink-0 gap-1">
                  <Button size="sm" variant="ghost" icon={<IconPlus />} onClick={() => setEditing({ parent_id: tm.id })}>{t("settings.teams.addChild")}</Button>
                  <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={() => setEditing(tm)}>{t("common.edit")}</Button>
                  <Button size="sm" variant="ghost" className="text-danger hover:!text-danger" icon={<IconTrash />} disabled={busy === tm.id} onClick={() => setRemoving(tm)}>{t("common.delete")}</Button>
                </span>
              </li>
            );
          })}
        </ul>
      )}
      {anyBoundary && !boundaryUsed && <p className="border-t border-hairline px-4 py-2 text-caption text-ink-subtle">{t("settings.teams.boundaryUnused")}</p>}
      <TeamDrawer target={editing} teams={teams.data ?? []} members={members.data ?? []} onClose={() => setEditing(null)} onSaved={() => { teams.reload(); members.reload(); refresh(); }} />
      <ConfirmDialog open={!!removing} title={t("common.delete")} message={removing ? t("settings.teams.deleteConfirm", { name: removing.name }) : null} confirmLabel={t("common.delete")} danger busy={!!busy} onConfirm={() => removing && void remove(removing)} onClose={() => setRemoving(null)} />
    </Panel>
  );
}

function TeamDrawer({ target, teams, members, onClose, onSaved }: { target: OrgTeam | { parent_id: string | null } | null; teams: OrgTeam[]; members: OrgMember[]; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const existing = target && "id" in target ? target : null;
  const [name, setName] = useState("");
  const [parent, setParent] = useState("");
  const [lead, setLead] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [boundary, setBoundary] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const key = target === null ? null : existing ? `edit:${existing.id}` : `new:${target.parent_id ?? ""}`;
  if (key !== loadedFor) {
    setLoadedFor(key);
    if (key) {
      setName(existing?.name ?? ""); setParent(existing?.parent_id ?? (target && !existing ? target.parent_id ?? "" : "")); setLead(existing?.lead_id ?? ""); setPicked(existing?.member_ids ?? []); setBoundary(!!existing?.is_boundary); setError(null);
    }
  }
  const excluded = existing ? descendants(teams, existing.id) : new Set<string>();
  const parentOptions = flatten(teams).filter(({ team: x }) => !excluded.has(x.id));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      const body = { name: name.trim(), parent_id: parent || null, lead_id: lead || null, member_ids: picked, is_boundary: boundary };
      if (existing) await api.org.updateTeam(existing.id, body); else await api.org.createTeam(body);
      toast.ok(existing ? t("toast.saved") : t("toast.created", { name: body.name }));
      onClose(); onSaved();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };

  return (
    <Drawer open={target !== null} onClose={onClose} title={existing ? t("settings.teams.edit") : t("settings.teams.new")} footer={<><Button onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="team-form" type="submit" disabled={busy}>{t("common.save")}</Button></>}>
      <form id="team-form" onSubmit={submit} className="space-y-4">
        <Field label={t("settings.teams.name")}><Input value={name} onChange={(e) => setName(e.target.value)} required autoFocus /></Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label={t("settings.teams.parent")}>
            <Select value={parent} onChange={(e) => setParent(e.target.value)}>
              <option value="">{t("settings.teams.noParent")}</option>
              {parentOptions.map(({ team: x, depth }) => <option key={x.id} value={x.id}>{"　".repeat(depth)}{x.name}</option>)}
            </Select>
          </Field>
          <Field label={t("settings.teams.lead")}>
            <Select value={lead} onChange={(e) => setLead(e.target.value)}>
              <option value="">{t("settings.teams.noLead")}</option>
              {members.filter((m) => m.active).map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
            </Select>
          </Field>
        </div>
        <Field as="div" label={t("settings.teams.boundary")}>
          <Checkbox checked={boundary} onChange={(e) => setBoundary(e.target.checked)} label={t("settings.teams.boundary")} />
          <p className="mt-1 text-caption text-ink-subtle">{boundary ? t("settings.teams.boundaryOn") : t("settings.teams.boundaryOff")}</p>
        </Field>
        <fieldset>
          <legend className="mb-1 text-caption text-ink-muted">{t("settings.teams.members")}</legend>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1.5">
            {members.filter((m) => m.active).map((m) => (
              <Checkbox key={m.id} checked={picked.includes(m.id)} onChange={(e) => setPicked(e.target.checked ? [...picked, m.id] : picked.filter((x) => x !== m.id))} label={m.name} />
            ))}
          </div>
        </fieldset>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
