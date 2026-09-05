"use client";
import { useState, type FormEvent } from "react";
import { api, type OrgMember } from "@/lib/api";
import { errorMessage, useAction, useLoad } from "@/lib/hooks";
import { LOCALE_NAMES, t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconEdit } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Avatar, Button, Checkbox, ConfirmDialog, Drawer, Empty, ErrorBox, Field, FormSection, Input, Panel, Select, Table, TableSkeleton, Tag, TagList, cx } from "@/components/ui";

export function MembersTab() {
  const { session, refresh } = useSession();
  const members = useLoad(() => api.org.members(), []);
  const roles = useLoad(() => api.org.roles(), []);
  const teams = useLoad(() => api.org.teams(), []);
  const [editing, setEditing] = useState<OrgMember | null>(null);
  const [owning, setOwning] = useState<OrgMember | null>(null);
  const { busy, run } = useAction();
  const roleTitle = (name: string) => roles.data?.find((r) => r.name === name)?.title ?? session?.roles.find((r) => r.name === name)?.title ?? name;
  const teamName = (id: string | null) => teams.data?.find((x) => x.id === id)?.name ?? session?.teams.find((x) => x.id === id)?.name ?? null;

  const makeOwner = async (m: OrgMember) => {
    if (await run(m.id, () => api.org.makeOwner(m.id), t("toast.saved"))) { setOwning(null); members.reload(); refresh(); }
  };

  return (
    <Panel index={1} title={t("settings.tab.members")} padded={false}>
      {members.loading && !members.data ? <TableSkeleton rows={4} cols={6} /> : members.error ? <div className="p-4"><ErrorBox message={members.error} onRetry={members.reload} /></div> : !members.data?.length ? <Empty text={t("settings.members.empty")} /> : (
        <Table>
          <thead>
            <tr>
              <th className="w-full">{t("settings.members.name")}</th><th className="w-[170px]">{t("common.email")}</th><th className="w-[150px]">{t("settings.members.roles")}</th><th className="w-[80px]">{t("settings.members.team")}</th><th className="w-[64px]">{t("settings.members.locale")}</th><th className="actions w-[188px]" aria-label={t("common.actions")} />
            </tr>
          </thead>
          <tbody>
            {members.data.map((m) => (
              <tr key={m.id} className={cx(!m.active && "text-ink-subtle")}>
                <td className="w-full max-w-0 min-w-[140px]">
                  <span className="flex items-center gap-2">
                    <Avatar name={m.name} size={20} />
                    <span className="min-w-0 truncate font-medium" title={m.name}>{m.name}</span>
                    {m.is_owner && <Tag tone="accent">{t("settings.members.owner")}</Tag>}
                    {!m.active && <Tag dark>{t("settings.members.inactive")}</Tag>}
                  </span>
                </td>
                <td className="max-w-[170px]"><span className="block truncate text-ink-muted" title={m.email}>{m.email}</span></td>
                <td className="whitespace-nowrap"><TagList items={m.roles.map(roleTitle)} /></td>
                <td className="whitespace-nowrap">{teamName(m.team_id) ?? <span className="text-ink-subtle">—</span>}</td>
                <td className="whitespace-nowrap">{LOCALE_NAMES[m.locale] ?? m.locale}</td>
                <td className="actions">
                  <span className="row-actions">
                    <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={() => setEditing(m)}>{t("common.edit")}</Button>
                    {!m.is_owner && m.active && <Button size="sm" variant="ghost" disabled={busy === m.id} onClick={() => setOwning(m)}>{t("settings.members.makeOwner")}</Button>}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <EditMemberDrawer member={editing} roles={(roles.data ?? []).map((r) => [r.name, r.title])} teams={(teams.data ?? []).map((x) => [x.id, x.name])} onClose={() => setEditing(null)} onSaved={() => { members.reload(); refresh(); }} />
      <ConfirmDialog open={!!owning} title={t("settings.members.makeOwner")} message={owning ? t("settings.members.makeOwnerConfirm", { name: owning.name }) : null} busy={!!busy} onConfirm={() => owning && void makeOwner(owning)} onClose={() => setOwning(null)} />
    </Panel>
  );
}

function EditMemberDrawer({ member, roles, teams, onClose, onSaved }: { member: OrgMember | null; roles: Array<[string, string]>; teams: Array<[string, string]>; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const [name, setName] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [team, setTeam] = useState("");
  const [active, setActive] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  if (member && loadedFor !== member.id) {
    setLoadedFor(member.id);
    setName(member.name); setPicked(member.roles); setTeam(member.team_id ?? ""); setActive(member.active); setError(null);
  }
  if (!member && loadedFor !== null) setLoadedFor(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!member) return;
    setBusy(true); setError(null);
    try {
      await api.org.updateMember(member.id, { name: name.trim(), roles: picked, team_id: team || null, active });
      toast.ok(t("toast.saved"));
      onClose(); onSaved();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };

  return (
    <Drawer open={!!member} onClose={onClose} title={t("settings.members.edit")} description={member?.email} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="member-form" type="submit" disabled={busy}>{t("common.save")}</Button></>}>
      <form id="member-form" onSubmit={submit} className="space-y-4">
        <FormSection title={t("form.basic")}>
          <Field label={t("settings.members.name")}><Input value={name} onChange={(e) => setName(e.target.value)} required autoFocus /></Field>
          <Field label={t("common.email")}><Input value={member?.email ?? ""} disabled /></Field>
        </FormSection>
        <FormSection title={t("form.access")}>
          <Field as="div" label={t("settings.members.roles")}>
            <div className="flex flex-wrap gap-x-4 gap-y-1.5">
              {roles.map(([name, title]) => (
                <Checkbox key={name} checked={picked.includes(name)} onChange={(e) => setPicked(e.target.checked ? [...picked, name] : picked.filter((x) => x !== name))} label={title} />
              ))}
            </div>
          </Field>
          <Field label={t("settings.members.team")}>
            <Select value={team} onChange={(e) => setTeam(e.target.value)}>
              <option value="">{t("settings.members.noTeam")}</option>
              {teams.map(([id, title]) => <option key={id} value={id}>{title}</option>)}
            </Select>
          </Field>
          <Checkbox checked={active} onChange={(e) => setActive(e.target.checked)} disabled={member?.is_owner} label={t("settings.members.activeField")} />
        </FormSection>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
