"use client";
import { useState, type FormEvent } from "react";
import { api, type AdminOrganization, type AdminOrganizationCreated, type Locale } from "@/lib/api";
import { fmtDate, fmtMoney } from "@/lib/format";
import { errorMessage, useAction, useHighlight, useLoad } from "@/lib/hooks";
import { LOCALE_NAMES, LOCALES, t } from "@/lib/i18n";
import { IconOrg, IconPlus } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Code, ConfirmDialog, CopyLine, Dialog, Drawer, Empty, ErrorBox, Field, Input, PageHeader, Panel, Select, StatChips, Table, TableSkeleton, Tag, cx } from "@/components/ui";

export default function AdminOrganizationsPage() {
  const orgs = useLoad(() => api.admin.organizations(), []);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<AdminOrganizationCreated | null>(null);
  const [toggling, setToggling] = useState<AdminOrganization | null>(null);
  const [highlight, mark] = useHighlight();
  const { busy, run } = useAction();
  const active = orgs.data?.filter((o) => !o.deactivated_at).length ?? 0;

  const toggle = async (o: AdminOrganization) => {
    const deactivate = !o.deactivated_at;
    if (await run(o.id, () => api.admin.updateOrganization(o.id, { deactivated: deactivate }), t("toast.saved"))) { setToggling(null); orgs.reload(); }
  };

  return (
    <div>
      <PageHeader title={t("admin.orgs.title")} description={t("admin.orgs.description")} actions={<Button variant="primary" icon={<IconPlus />} onClick={() => setCreating(true)}>{t("admin.orgs.new")}</Button>} />
      <StatChips className="mb-4" value={null} items={[{ key: "total", label: t("home.total"), value: orgs.data?.length ?? 0 }, { key: "active", label: t("admin.orgs.active"), value: active, tone: "accent" }]} />
      <Panel index={1} title={t("admin.nav.organizations")} telemetry={orgs.data ? t("panel.rows", { n: orgs.data.length }) : undefined} padded={false}>
        {orgs.loading && !orgs.data ? <TableSkeleton rows={4} cols={7} /> : orgs.error ? <div className="p-4"><ErrorBox message={orgs.error} onRetry={orgs.reload} /></div> : !orgs.data?.length ? <Empty text={t("admin.orgs.empty")} /> : (
          <Table>
            <thead>
              <tr><th>{t("admin.orgs.name")}</th><th>{t("admin.orgs.owner")}</th><th className="num">{t("admin.orgs.members")}</th><th className="num">{t("admin.orgs.agents")}</th><th className="num">{t("admin.orgs.tasks")}</th><th className="num">{t("admin.orgs.cost30d")}</th><th>{t("admin.orgs.status")}</th><th>{t("admin.orgs.createdAt")}</th><th className="w-0" aria-label={t("common.actions")} /></tr>
            </thead>
            <tbody>
              {orgs.data.map((o) => (
                <tr key={o.id} className={cx(o.deactivated_at && "text-ink-subtle", highlight === o.id && "row-new")}>
                  <td>
                    <span className="flex items-center gap-2.5">
                      {/* 默认组织头像：客轮剪影，24px 方形 surface-3 底 */}
                      <span className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-xs border border-hairline bg-surface-3 text-ink-muted" aria-hidden="true"><IconOrg size={16} /></span>
                      <span className="min-w-0">
                        {o.name}
                        <div className="text-caption text-ink-subtle"><Code className="text-caption">{o.slug}</Code> · {o.currency} · {LOCALE_NAMES[o.default_locale] ?? o.default_locale}</div>
                      </span>
                    </span>
                  </td>
                  <td>{o.owner ? <>{o.owner.name}<div><Code className="text-caption">{o.owner.email}</Code></div></> : <span className="text-ink-subtle">{t("admin.orgs.noOwner")}</span>}</td>
                  <td className="num">{o.member_count}</td>
                  <td className="num">{o.agent_count}</td>
                  <td className="num">{o.task_count}</td>
                  <td className="num whitespace-nowrap">{fmtMoney(o.cost_30d, o.currency)}</td>
                  <td>{o.deactivated_at ? <Tag dark>{t("admin.orgs.deactivated")}</Tag> : <Tag tone="success">{t("admin.orgs.active")}</Tag>}</td>
                  <td className="telemetry whitespace-nowrap">{fmtDate(o.created_at, true)}</td>
                  <td className="whitespace-nowrap !py-1.5 align-middle">
                    <span className="row-actions">
                      <Button size="sm" variant={o.deactivated_at ? "default" : "danger"} disabled={busy === o.id} onClick={() => setToggling(o)}>{o.deactivated_at ? t("admin.orgs.reactivate") : t("admin.orgs.deactivate")}</Button>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>
      <CreateDrawer open={creating} onClose={() => setCreating(false)} onCreated={(o) => { setCreated(o); mark(o.id); orgs.reload(); }} />
      <Dialog open={!!created} onClose={() => setCreated(null)} title={t("admin.orgs.created")} footer={<Button variant="primary" onClick={() => setCreated(null)}>{t("admin.orgs.done")}</Button>}>
        <p className="text-ink-muted">{t("admin.orgs.inviteHint")}</p>
        <div>
          <div className="mb-1 text-caption text-ink-muted">{t("admin.orgs.inviteUrl")}</div>
          <CopyLine text={created?.owner_invite_url ?? ""} />
        </div>
      </Dialog>
      <ConfirmDialog
        open={!!toggling}
        title={toggling?.deactivated_at ? t("admin.orgs.reactivate") : t("admin.orgs.deactivate")}
        message={toggling ? t(toggling.deactivated_at ? "admin.orgs.reactivateConfirm" : "admin.orgs.deactivateConfirm", { name: toggling.name }) : null}
        confirmLabel={toggling?.deactivated_at ? t("admin.orgs.reactivate") : t("admin.orgs.deactivate")}
        danger={!!toggling && !toggling.deactivated_at}
        busy={!!busy}
        onConfirm={() => toggling && void toggle(toggling)}
        onClose={() => setToggling(null)}
      />
    </div>
  );
}

function CreateDrawer({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (o: AdminOrganizationCreated) => void }) {
  const toast = useToast();
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [currency, setCurrency] = useState("CNY");
  const [locale, setLocaleValue] = useState<Locale>("zh-CN");
  const [ownerEmail, setOwnerEmail] = useState("");
  const [ownerName, setOwnerName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      const o = await api.admin.createOrganization({ slug: slug.trim(), name: name.trim(), currency: currency.trim().toUpperCase() || undefined, default_locale: locale, owner_email: ownerEmail.trim(), owner_name: ownerName.trim() });
      toast.ok(t("toast.created", { name: o.name }));
      onClose();
      setSlug(""); setName(""); setCurrency("CNY"); setLocaleValue("zh-CN"); setOwnerEmail(""); setOwnerName("");
      onCreated(o);
    } catch (err) { const m = errorMessage(err); setError(m); toast.fail(m); } finally { setBusy(false); }
  };
  return (
    <Drawer open={open} onClose={onClose} title={t("admin.orgs.new")} description={t("admin.orgs.inviteHint")} footer={<><Button onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="org-form" type="submit" disabled={busy}>{t("common.create")}</Button></>}>
      <form id="org-form" onSubmit={submit} className="space-y-4">
        <Field label={t("admin.orgs.name")}><Input value={name} onChange={(e) => setName(e.target.value)} required autoFocus /></Field>
        <Field label={t("admin.orgs.slug")} hint={t("admin.orgs.slugHint")}><Input value={slug} onChange={(e) => setSlug(e.target.value)} required pattern="[a-z0-9-]+" className="font-mono" /></Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label={t("admin.orgs.currency")}><Input value={currency} onChange={(e) => setCurrency(e.target.value)} maxLength={3} className="uppercase" /></Field>
          <Field label={t("admin.orgs.locale")}>
            <Select value={locale} onChange={(e) => setLocaleValue(e.target.value as Locale)}>
              {LOCALES.map((l) => <option key={l} value={l}>{LOCALE_NAMES[l]}</option>)}
            </Select>
          </Field>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label={t("admin.orgs.ownerEmail")}><Input type="email" value={ownerEmail} onChange={(e) => setOwnerEmail(e.target.value)} required /></Field>
          <Field label={t("admin.orgs.ownerName")}><Input value={ownerName} onChange={(e) => setOwnerName(e.target.value)} required /></Field>
        </div>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
