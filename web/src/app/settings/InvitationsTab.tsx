"use client";
import { useState, type FormEvent } from "react";
import { api, type Invitation } from "@/lib/api";
import { fmtDateTime, parseDate } from "@/lib/format";
import { errorMessage, useAction, useHighlight, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconPlus, IconTrash } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Checkbox, ConfirmDialog, CopyButton, CopyLine, Dialog, Drawer, Empty, ErrorBox, Field, FormSection, Input, Panel, Table, TableSkeleton, Tag, TagList, cx } from "@/components/ui";

export function InvitationsTab() {
  const list = useLoad(() => api.org.invitations(), []);
  const roles = useLoad(() => api.org.roles(), []);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<Invitation | null>(null);
  const [removing, setRemoving] = useState<Invitation | null>(null);
  const [highlight, mark] = useHighlight();
  const { busy, run } = useAction();
  const roleTitle = (name: string) => roles.data?.find((r) => r.name === name)?.title ?? name;
  const status = (i: Invitation) => (i.accepted_at ? <Tag tone="success">{t("settings.invitations.accepted")}</Tag> : parseDate(i.expires_at)! < new Date() ? <Tag dark>{t("settings.invitations.expired")}</Tag> : <Tag tone="warning">{t("settings.invitations.pending")}</Tag>);

  const remove = async (i: Invitation) => {
    if (await run(i.id, () => api.org.deleteInvitation(i.id), t("toast.deleted"))) { setRemoving(null); list.reload(); }
  };

  return (
    <Panel index={1} title={t("settings.tab.invitations")} telemetry={list.data ? t("panel.rows", { n: list.data.length }) : undefined} padded={false} actions={<Button size="sm" variant="primary" icon={<IconPlus />} onClick={() => setCreating(true)}>{t("settings.invitations.new")}</Button>}>
      {list.loading && !list.data ? <TableSkeleton rows={3} cols={6} /> : list.error ? <div className="p-4"><ErrorBox message={list.error} onRetry={list.reload} /></div> : !list.data?.length ? <Empty text={t("settings.invitations.empty")} action={<Button variant="primary" icon={<IconPlus />} onClick={() => setCreating(true)}>{t("settings.invitations.new")}</Button>} /> : (
        <Table>
          <thead>
            <tr><th className="w-full">{t("common.email")}</th><th className="w-[120px]">{t("common.name")}</th><th className="w-[180px]">{t("settings.members.roles")}</th><th className="w-[80px]">{t("settings.members.status")}</th><th className="w-[150px]">{t("settings.invitations.expires")}</th><th className="actions w-[150px]" aria-label={t("common.actions")} /></tr>
          </thead>
          <tbody>
            {list.data.map((i) => (
              <tr key={i.id} className={cx(highlight === i.id && "row-new")}>
                <td className="w-full max-w-0 min-w-[160px]"><span className="block truncate" title={i.email}>{i.email}</span></td>
                <td>{i.name || <span className="text-ink-subtle">—</span>}</td>
                <td className="whitespace-nowrap"><TagList items={i.roles.map(roleTitle)} /></td>
                <td>{status(i)}</td>
                <td className="telemetry whitespace-nowrap">{fmtDateTime(i.expires_at)}</td>
                <td className="actions">
                  <span className="row-actions">
                    {!i.accepted_at && <CopyButton text={i.url} variant="ghost" />}
                    <Button size="sm" variant="ghost" className="text-danger hover:!text-danger" icon={<IconTrash />} disabled={busy === i.id} onClick={() => setRemoving(i)}>{t("common.delete")}</Button>
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <CreateDrawer open={creating} roles={(roles.data ?? []).map((r) => [r.name, r.title])} onClose={() => setCreating(false)} onCreated={(i) => { setCreated(i); mark(i.id); list.reload(); }} />
      <Dialog open={!!created} onClose={() => setCreated(null)} title={t("settings.invitations.link")} footer={<Button variant="primary" onClick={() => setCreated(null)}>{t("admin.orgs.done")}</Button>}>
        <p className="text-ink-muted">{t("settings.invitations.linkHint")}</p>
        <CopyLine text={created?.url ?? ""} />
      </Dialog>
      <ConfirmDialog open={!!removing} title={t("common.delete")} message={removing ? t("settings.invitations.deleteConfirm", { email: removing.email }) : null} confirmLabel={t("common.delete")} danger busy={!!busy} onConfirm={() => removing && void remove(removing)} onClose={() => setRemoving(null)} />
    </Panel>
  );
}

function CreateDrawer({ open, roles, onClose, onCreated }: { open: boolean; roles: Array<[string, string]>; onClose: () => void; onCreated: (i: Invitation) => void }) {
  const toast = useToast();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      const i = await api.org.createInvitation({ email: email.trim(), name: name.trim() || undefined, roles: picked });
      toast.ok(t("toast.invited", { email: i.email }));
      onClose(); setEmail(""); setName(""); setPicked([]);
      onCreated(i);
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <Drawer open={open} onClose={onClose} title={t("settings.invitations.new")} description={t("settings.invitations.newHint")} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="invite-form" type="submit" disabled={busy}>{t("settings.invitations.create")}</Button></>}>
      <form id="invite-form" onSubmit={submit} className="space-y-4">
        <FormSection title={t("form.basic")}>
          <Field label={t("common.email")}><Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus /></Field>
          <Field label={t("common.name")}><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
        </FormSection>
        <FormSection title={t("form.access")}>
          <Field as="div" label={t("settings.members.roles")}>
            <div className="flex flex-wrap gap-x-4 gap-y-1.5">
              {roles.map(([r, title]) => (
                <Checkbox key={r} checked={picked.includes(r)} onChange={(e) => setPicked(e.target.checked ? [...picked, r] : picked.filter((x) => x !== r))} label={title} />
              ))}
            </div>
          </Field>
        </FormSection>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
