"use client";
import { useState } from "react";
import { api, DEFAULT_PREFERENCES, PREF_FIELDS, type PrefField, type PreferencesPatch, type PrefSource, type PrefValues, type RolePreferences } from "@/lib/api";
import { errorMessage, useAction, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconEdit } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, ConsequenceDialog, Dialog, Empty, ErrorBox, Panel, Table, TableSkeleton, Tag } from "@/components/ui";
import { PreferenceForm } from "./PreferenceForm";

/*
 * 组织设置 → 工作台 → 显示偏好（DESIGN.md §20）：给角色配默认。一行一个角色：角色、成员数、设过的项；
 * 「编辑」打开同一份偏好表单（角色设的 ∪ 系统默认；来源标签写「角色」/「默认」），改一项立即 PUT /org/preferences/{role}；「恢复默认」DELETE。
 */
export function RolePreferencesPanel({ index = 3 }: { index?: number }) {
  const toast = useToast();
  const rows = useLoad(() => api.preferences.org(), []);
  const catalog = useLoad(() => api.preferences.catalog(), []);
  const { busy, run } = useAction();
  const [editing, setEditing] = useState<RolePreferences | null>(null);
  const [resetting, setResetting] = useState<RolePreferences | null>(null);
  const [saving, setSaving] = useState(false);
  const defaults = catalog.data?.defaults ?? DEFAULT_PREFERENCES;
  const fieldTitle = (f: PrefField) => t(`pref.field.${f}`);

  const change = async (patch: PreferencesPatch) => {
    if (!editing) return;
    setSaving(true);
    try {
      const r = await api.preferences.saveRole(editing.role, patch);
      setEditing(r);
      rows.reload();
    } catch (e) {
      toast.fail(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };
  const reset = async (r: RolePreferences) => {
    if (await run(r.role, () => api.preferences.resetRole(r.role), t("pref.roleResetDone", { role: r.role_title }))) {
      setResetting(null);
      setEditing(null);
      rows.reload();
    }
  };
  const resolved = (r: RolePreferences): PrefValues => ({ ...defaults, ...r.data });
  const sourcesOf = (r: RolePreferences) => Object.fromEntries(PREF_FIELDS.map((f) => [f, r.data[f] !== undefined ? "role" : "default"])) as Record<PrefField, PrefSource>;

  return (
    <div>
      <Panel index={index} title={t("pref.roleTitle")} padded={false} telemetry={rows.data ? t("settings.workspace.roleCount", { n: rows.data.length }) : undefined}>
        <p className="border-b border-hairline px-4 py-3 text-caption text-ink-muted">{t("pref.roleDescription")}</p>
        {rows.loading && !rows.data ? (
          <TableSkeleton rows={4} cols={4} />
        ) : rows.error ? (
          <div className="p-4"><ErrorBox message={rows.error} onRetry={rows.reload} /></div>
        ) : !rows.data?.length ? (
          <Empty text={t("settings.roles.empty")} illustration={false} />
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-[140px]">{t("settings.workspace.role")}</th>
                <th className="num w-[72px]">{t("settings.workspace.members")}</th>
                <th>{t("pref.roleFields")}</th>
                <th className="w-0" aria-label={t("common.actions")} />
              </tr>
            </thead>
            <tbody>
              {rows.data.map((r) => (
                <tr key={r.role} data-role-pref={r.role}>
                  <td><Tag>{r.role_title}</Tag></td>
                  <td className="num tabular-nums">{r.member_count}</td>
                  <td>
                    {r.fields.length ? (
                      <span className="flex flex-wrap gap-1">{r.fields.map((f) => <Tag key={f} tone="accent">{fieldTitle(f)}</Tag>)}</span>
                    ) : (
                      <span className="text-ink-subtle">{t("pref.roleUsesDefault")}</span>
                    )}
                  </td>
                  <td className="whitespace-nowrap !py-1.5 align-middle">
                    <span className="row-actions inline-flex gap-1">
                      <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={() => setEditing(r)}>{t("common.edit")}</Button>
                      <Button size="sm" variant="ghost" disabled={busy === r.role || r.fields.length === 0} onClick={() => setResetting(r)}>{t("settings.workspace.resetDefault")}</Button>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>

      <Dialog
        open={!!editing}
        onClose={() => setEditing(null)}
        title={editing ? t("pref.roleEditTitle", { role: editing.role_title }) : ""}
        width={640}
        footer={<><Button variant="ghost" disabled={!editing?.fields.length || saving} onClick={() => editing && setResetting(editing)}>{t("settings.workspace.resetDefault")}</Button><Button variant="primary" onClick={() => setEditing(null)}>{t("common.done")}</Button></>}
      >
        <p className="mb-3 text-caption text-ink-muted">{t("pref.roleEditHint")}</p>
        {editing && catalog.data && (
          <PreferenceForm value={resolved(editing)} catalog={catalog.data} sources={sourcesOf(editing)} busy={saving} onChange={(patch) => void change(patch)} sourceOf={(src) => (src === "default" ? t("pref.source.default") : t("pref.source.thisRole"))} />
        )}
        {catalog.error && <ErrorBox message={catalog.error} onRetry={catalog.reload} />}
      </Dialog>
      <ConsequenceDialog
        open={!!resetting}
        title={resetting ? t("pref.roleResetTitle", { role: resetting.role_title }) : ""}
        effects={[t("pref.roleResetEffect.role"), t("pref.roleResetEffect.personal")]}
        danger={false}
        confirmLabel={t("settings.workspace.resetDefault")}
        busy={!!busy}
        onConfirm={() => resetting && void reset(resetting)}
        onClose={() => setResetting(null)}
      />
    </div>
  );
}
