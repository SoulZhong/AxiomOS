"use client";
import { useState, type FormEvent } from "react";
import { api, type Capability } from "@/lib/api";
import { errorMessage, useAction, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconEdit, IconPlus, IconTrash } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Code, ConfirmDialog, Drawer, Empty, ErrorBox, Field, Input, Panel, Table, TableSkeleton, Tag } from "@/components/ui";
import { BilingualTitleFields, buildTitle, titlePairOf, type TitlePair } from "./BilingualTitle";

export function CapabilitiesTab() {
  const { refresh } = useSession();
  const caps = useLoad(() => api.org.capabilities(), []);
  const [editing, setEditing] = useState<Capability | null | "new">(null);
  const [removing, setRemoving] = useState<Capability | null>(null);
  const { busy, run } = useAction();

  const remove = async (c: Capability) => {
    if (await run(c.name, () => api.org.deleteCapability(c.name), t("toast.deleted"))) { setRemoving(null); caps.reload(); refresh(); }
  };

  return (
    <Panel index={1} title={t("settings.tab.capabilities")} padded={false} actions={<Button size="sm" variant="primary" icon={<IconPlus />} onClick={() => setEditing("new")}>{t("settings.caps.new")}</Button>}>
      {caps.loading && !caps.data ? <TableSkeleton rows={4} cols={3} /> : caps.error ? <div className="p-4"><ErrorBox message={caps.error} onRetry={caps.reload} /></div> : !caps.data?.length ? <Empty text={t("settings.caps.empty")} action={<Button variant="primary" icon={<IconPlus />} onClick={() => setEditing("new")}>{t("settings.caps.new")}</Button>} /> : (
        <Table>
          <thead><tr><th>{t("settings.tab.capabilities")}</th><th>{t("common.codeName")}</th><th className="w-0" aria-label={t("common.actions")} /></tr></thead>
          <tbody>
            {caps.data.map((c) => (
              <tr key={c.name}>
                <td><Tag>{c.title}</Tag></td>
                <td><Code>{c.name}</Code></td>
                <td className="whitespace-nowrap !py-1.5 align-middle">
                  <span className="row-actions inline-flex gap-1">
                    <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={() => setEditing(c)}>{t("common.edit")}</Button>
                    <Button size="sm" variant="ghost" className="text-danger hover:!text-danger" icon={<IconTrash />} disabled={busy === c.name} onClick={() => setRemoving(c)}>{t("common.delete")}</Button>
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <CapabilityDrawer cap={editing} onClose={() => setEditing(null)} onSaved={() => { caps.reload(); refresh(); }} />
      <ConfirmDialog open={!!removing} title={t("common.delete")} message={removing ? t("settings.caps.deleteConfirm", { title: removing.title }) : null} confirmLabel={t("common.delete")} danger busy={!!busy} onConfirm={() => removing && void remove(removing)} onClose={() => setRemoving(null)} />
    </Panel>
  );
}

function CapabilityDrawer({ cap, onClose, onSaved }: { cap: Capability | null | "new"; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const existing = cap && cap !== "new" ? cap : null;
  const [name, setName] = useState("");
  const [title, setTitle] = useState<TitlePair>({ zh: "", en: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const key = cap === null ? null : cap === "new" ? "new" : cap.name;
  if (key !== loadedFor) {
    setLoadedFor(key);
    if (key) { setName(existing?.name ?? ""); setTitle(titlePairOf(existing?.title, existing?.titles)); setError(null); }
  }
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const tt = buildTitle(title);
    if (!tt) { setError(t("mock.org.needName")); return; }
    setBusy(true); setError(null);
    try {
      await api.org.saveCapability(existing?.name ?? name.trim(), { title: tt });
      toast.ok(t("toast.saved"));
      onClose(); onSaved();
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <Drawer open={cap !== null} onClose={onClose} title={existing ? t("settings.caps.edit") : t("settings.caps.new")} description={t("settings.caps.empty")} footer={<><Button onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="cap-form" type="submit" disabled={busy}>{t("common.save")}</Button></>}>
      <form id="cap-form" onSubmit={submit} className="space-y-4">
        <Field label={t("settings.caps.name")} hint={existing ? undefined : t("settings.caps.nameHint")}>
          <Input value={name} onChange={(e) => setName(e.target.value)} required pattern="[a-z0-9_]+" disabled={!!existing} className="font-mono" autoFocus={!existing} />
        </Field>
        <BilingualTitleFields value={title} onChange={setTitle} autoFocus={!!existing} />
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
