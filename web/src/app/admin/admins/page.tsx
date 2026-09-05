"use client";
import { useState, type FormEvent } from "react";
import { api } from "@/lib/api";
import { errorMessage, useHighlight, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconPlus } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Avatar, Button, Code, Drawer, Empty, ErrorBox, Field, Input, PageHeader, Panel, Table, TableSkeleton, cx } from "@/components/ui";

export default function AdminAdminsPage() {
  const admins = useLoad(() => api.admin.admins(), []);
  const [creating, setCreating] = useState(false);
  const [highlight, mark] = useHighlight();
  return (
    <div>
      <PageHeader title={t("admin.admins.title")} description={t("admin.admins.description")} actions={<Button variant="primary" icon={<IconPlus />} onClick={() => setCreating(true)}>{t("admin.admins.new")}</Button>} />
      <Panel index={1} title={t("admin.nav.admins")} telemetry={admins.data ? t("panel.rows", { n: admins.data.length }) : undefined} padded={false}>
        {admins.loading && !admins.data ? <TableSkeleton rows={3} cols={2} /> : admins.error ? <div className="p-4"><ErrorBox message={admins.error} onRetry={admins.reload} /></div> : !admins.data?.length ? <Empty text={t("admin.admins.empty")} /> : (
          <Table>
            <thead><tr><th>{t("common.name")}</th><th>{t("common.email")}</th></tr></thead>
            <tbody>
              {admins.data.map((a) => (
                <tr key={a.id} className={cx(highlight === a.id && "row-new")}>
                  <td><span className="inline-flex items-center gap-2"><Avatar name={a.name} />{a.name}</span></td>
                  <td><Code>{a.email}</Code></td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>
      <CreateDrawer open={creating} onClose={() => setCreating(false)} onCreated={(id) => { mark(id); admins.reload(); }} />
    </div>
  );
}

function CreateDrawer({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (id: string) => void }) {
  const toast = useToast();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      const a = await api.admin.createAdmin({ email: email.trim(), name: name.trim(), password });
      toast.ok(t("toast.created", { name: a.name }));
      onClose(); setEmail(""); setName(""); setPassword("");
      onCreated(a.id);
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <Drawer open={open} onClose={onClose} title={t("admin.admins.new")} description={t("admin.admins.description")} footer={<><Button onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="admin-form" type="submit" disabled={busy}>{t("common.add")}</Button></>}>
      <form id="admin-form" onSubmit={submit} className="space-y-4">
        <Field label={t("common.email")}><Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus /></Field>
        <Field label={t("common.name")}><Input value={name} onChange={(e) => setName(e.target.value)} required /></Field>
        <Field label={t("common.password")}><Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoComplete="new-password" /></Field>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
