"use client";
import { useState, type FormEvent } from "react";
import { api, isSynced, type OrgMember, type OrgTeam } from "@/lib/api";
import { errorMessage } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconWarning } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Checkbox, Drawer, Field, FormSection, Input, Select, Tag, Tip } from "@/components/ui";
import { buildTree, flattenTree } from "./tree";

/** 成员状态芯片（ADR 0017）：待激活 = 警示色；已停用 = 暗色；正常不显示（成员列表只在不正常时标状态）。 */
export function MemberStatusTag({ member }: { member: OrgMember }) {
  if (!member.active || member.status === "inactive") return <Tag dark>{member.status_title ?? t("settings.people.status.inactive")}</Tag>;
  if (member.status === "pending_activation") return <Tag tone="warning">{member.status_title ?? t("settings.people.status.pending")}</Tag>;
  return null;
}

/** 「可能与 X 重复」的小提示点（ADR 0017 补记四）：名字旁一个警示色圆点，悬停看是谁，点开即合并对话框。 */
export function DuplicateDot({ member, onOpen }: { member: OrgMember; onOpen: (otherId: string) => void }) {
  const d = member.possible_duplicate_of?.[0];
  if (!d) return null;
  const label = t("settings.people.dupHint", { name: d.name });
  return (
    <Tip tip={`${label} · ${d.reason_text}`}>
      <button type="button" className="pressable inline-flex h-5 w-5 items-center justify-center rounded-full text-warning hover:bg-warning-bg" aria-label={label} onClick={(e) => { e.stopPropagation(); onOpen(d.id); }} data-dup-dot={d.id}>
        <IconWarning size={13} />
      </button>
    </Tip>
  );
}

/** 成员详情抽屉：姓名（同步来的只读）、邮箱、角色、直属团队、账号启用；疑似重复时一行提示 +「合并」。 */
export function EditMemberDrawer({ member, roles, teams, onClose, onSaved, onMerge }: { member: OrgMember | null; roles: Array<[string, string]>; teams: OrgTeam[]; onClose: () => void; onSaved: () => void; onMerge?: (m: OrgMember, otherId: string) => void }) {
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
  const synced = isSynced(member);
  const sourceName = member?.source_title ?? member?.source;
  const syncedHint = t("settings.members.nameSynced", { name: sourceName });
  const teamOptions = flattenTree(buildTree(teams.filter((x) => x.active !== false)));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!member) return;
    setBusy(true); setError(null);
    try {
      // 同步来的成员：名字与团队都由同步决定，只提交角色与启用状态
      await api.org.updateMember(member.id, { ...(synced ? {} : { name: name.trim(), team_id: team || null }), roles: picked, active });
      toast.ok(t("toast.saved"));
      onClose(); onSaved();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };

  return (
    <Drawer open={!!member} onClose={onClose} title={t("settings.members.edit")} description={member?.email} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="member-form" type="submit" disabled={busy}>{t("common.save")}</Button></>}>
      <form id="member-form" onSubmit={submit} className="space-y-4">
        {member?.possible_duplicate_of?.length && onMerge ? (
          <div className="flex flex-wrap items-center gap-2 rounded-md border border-warning-border bg-warning-bg px-3 py-2 text-body text-warning" role="note" data-dup-line>
            <IconWarning size={14} className="shrink-0" aria-hidden="true" />
            <span className="min-w-0 flex-1">{t("settings.people.dupLine", { name: member.possible_duplicate_of[0].name, reason: member.possible_duplicate_of[0].reason_text })}</span>
            <Button size="sm" onClick={() => onMerge(member, member.possible_duplicate_of![0].id)}>{t("settings.people.dupMerge")}</Button>
          </div>
        ) : null}
        <FormSection title={t("form.basic")}>
          <Field label={t("settings.members.name")} hint={synced ? syncedHint : undefined}>
            <Input value={name} onChange={(e) => setName(e.target.value)} required autoFocus={!synced} disabled={synced} title={synced ? syncedHint : undefined} />
          </Field>
          <Field label={t("common.email")}><Input value={member?.email ?? ""} disabled /></Field>
        </FormSection>
        <FormSection title={t("form.access")}>
          <Field as="div" label={t("settings.members.roles")}>
            <div className="flex flex-wrap gap-x-4 gap-y-1.5">
              {roles.map(([r, title]) => (
                <Checkbox key={r} checked={picked.includes(r)} onChange={(e) => setPicked(e.target.checked ? [...picked, r] : picked.filter((x) => x !== r))} label={title} />
              ))}
            </div>
          </Field>
          <Field label={t("settings.members.team")} hint={synced ? t("settings.people.syncedMemberTeam", { name: sourceName }) : undefined}>
            <Select value={team} onChange={(e) => setTeam(e.target.value)} disabled={synced}>
              <option value="">{t("settings.members.noTeam")}</option>
              {teamOptions.map(({ team: x, depth }) => <option key={x.id} value={x.id}>{"　".repeat(depth)}{x.name}</option>)}
            </Select>
          </Field>
          <Checkbox checked={active} onChange={(e) => setActive(e.target.checked)} disabled={member?.is_owner} label={t("settings.members.activeField")} />
        </FormSection>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
