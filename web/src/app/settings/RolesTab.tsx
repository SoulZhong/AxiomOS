"use client";
import { useState, type FormEvent } from "react";
import { api, type OrgRole } from "@/lib/api";
import { errorMessage, useAction, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { PERMISSIONS, permissionTitle } from "@/lib/terms";
import { useSession } from "@/components/AppShell";
import { IconEdit, IconPlus, IconTrash } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Checkbox, Code, ConfirmDialog, Drawer, Empty, ErrorBox, Field, Input, Panel, Table, TableSkeleton, Tag } from "@/components/ui";
import { BilingualTitleFields, buildTitle, titlePairOf, type TitlePair } from "./BilingualTitle";

export function RolesTab() {
  const { refresh } = useSession();
  const roles = useLoad(() => api.org.roles(), []);
  const [editing, setEditing] = useState<OrgRole | null | "new">(null);
  const [removing, setRemoving] = useState<OrgRole | null>(null);
  const { busy, run } = useAction();

  const remove = async (r: OrgRole) => {
    if (await run(r.name, () => api.org.deleteRole(r.name), t("toast.deleted"))) { setRemoving(null); roles.reload(); refresh(); }
  };

  return (
    <Panel index={1} title={t("settings.tab.roles")} padded={false} actions={<Button size="sm" variant="primary" icon={<IconPlus />} onClick={() => setEditing("new")}>{t("settings.roles.new")}</Button>}>
      {roles.loading && !roles.data ? <TableSkeleton rows={4} cols={4} /> : roles.error ? <div className="p-4"><ErrorBox message={roles.error} onRetry={roles.reload} /></div> : !roles.data?.length ? <Empty text={t("settings.roles.empty")} /> : (
        <Table>
          <thead><tr><th>{t("settings.tab.roles")}</th><th>{t("common.codeName")}</th><th>{t("settings.roles.permissions")}</th><th className="w-0" aria-label={t("common.actions")} /></tr></thead>
          <tbody>
            {roles.data.map((r) => (
              <tr key={r.name}>
                <td><span className="inline-flex items-center gap-2"><Tag>{r.title}</Tag>{r.builtin && <span className="text-caption text-ink-subtle">{t("common.builtin")}</span>}</span></td>
                <td><Code>{r.name}</Code></td>
                <td>{r.permissions.length ? <span className="flex flex-wrap gap-1">{r.permissions.map((p) => <Tag key={p} tone="accent">{permissionTitle(p)}</Tag>)}</span> : <span className="text-ink-subtle">—</span>}</td>
                <td className="whitespace-nowrap !py-1.5 align-middle">
                  <span className="row-actions inline-flex gap-1">
                    <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={() => setEditing(r)}>{t("common.edit")}</Button>
                    {!r.builtin && <Button size="sm" variant="ghost" className="text-danger hover:!text-danger" icon={<IconTrash />} disabled={busy === r.name} onClick={() => setRemoving(r)}>{t("common.delete")}</Button>}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <RoleDrawer role={editing} onClose={() => setEditing(null)} onSaved={() => { roles.reload(); refresh(); }} />
      <ConfirmDialog open={!!removing} title={t("common.delete")} message={removing ? t("settings.roles.deleteConfirm", { title: removing.title }) : null} confirmLabel={t("common.delete")} danger busy={!!busy} onConfirm={() => removing && void remove(removing)} onClose={() => setRemoving(null)} />
    </Panel>
  );
}

function RoleDrawer({ role, onClose, onSaved }: { role: OrgRole | null | "new"; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const open = role !== null;
  const existing = role && role !== "new" ? role : null;
  const [name, setName] = useState("");
  const [title, setTitle] = useState<TitlePair>({ zh: "", en: "" });
  const [perms, setPerms] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const key = role === null ? null : role === "new" ? "new" : role.name;
  if (key !== loadedFor) {
    setLoadedFor(key);
    if (key) {
      setName(existing?.name ?? ""); setTitle(titlePairOf(existing?.title, existing?.titles)); setPerms(existing?.permissions ?? []); setError(null);
    }
  }
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const tt = buildTitle(title);
    if (!tt) { setError(t("mock.org.needName")); return; }
    setBusy(true); setError(null);
    try {
      await api.org.saveRole(existing?.name ?? name.trim(), { title: tt, permissions: perms });
      toast.ok(t("toast.saved"));
      onClose(); onSaved();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <Drawer open={open} onClose={onClose} title={existing ? t("settings.roles.edit") : t("settings.roles.new")} footer={<><Button onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="role-form" type="submit" disabled={busy}>{t("common.save")}</Button></>}>
      <form id="role-form" onSubmit={submit} className="space-y-4">
        <Field label={t("settings.roles.name")} hint={existing ? undefined : t("settings.roles.nameHint")}>
          <Input value={name} onChange={(e) => setName(e.target.value)} required pattern="[a-z0-9_]+" disabled={!!existing} className="font-mono" autoFocus={!existing} />
        </Field>
        <BilingualTitleFields value={title} onChange={setTitle} autoFocus={!!existing} />
        <fieldset>
          <legend className="mb-1 text-caption text-ink-muted">{t("settings.roles.permissions")}</legend>
          <div className="flex flex-wrap gap-x-4 gap-y-1.5">
            {PERMISSIONS.map((p) => (
              <Checkbox key={p} checked={perms.includes(p)} disabled={!!existing?.builtin} onChange={(e) => setPerms(e.target.checked ? [...perms, p] : perms.filter((x) => x !== p))} label={permissionTitle(p)} />
            ))}
          </div>
          <p className="mt-1.5 text-caption text-ink-subtle">{t("settings.roles.crossBoundary")}</p>
          {existing?.builtin && <p className="mt-1 text-caption text-ink-subtle">{t("settings.roles.builtinHint")}</p>}
        </fieldset>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
