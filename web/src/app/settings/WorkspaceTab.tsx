"use client";
import { useState } from "react";
import { api, sameLayout, type BlockKey, type LayoutBlock, type RoleWorkspace } from "@/lib/api";
import { errorMessage, useAction, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { WorkspaceEditorDialog } from "@/components/blocks/WorkspaceEditor";
import { SizeChip } from "@/components/blocks/WorkspaceGrid";
import { IconEdit } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, ConsequenceDialog, Empty, ErrorBox, Panel, Select, Table, TableSkeleton, Tag } from "@/components/ui";
import { RolePreferencesPanel } from "./RolePreferences";

/*
 * 组织设置 → 工作台（ADR 0015 与补记二）：每个角色进来先看到什么。
 * 一行一个角色：角色名、成员数、当前预设（或「自定义」）、区块清单（每块带「宽×高」）；
 * 行内换预设（PUT {preset}）、在网格编辑器里拖动位置与大小（PUT {blocks[{key,x,y,w,h}]}）、恢复默认（DELETE）。
 * 下面把系统发的预设按接口给的标题和说明列出来，不写死预设名。
 */

export function WorkspaceTab() {
  const toast = useToast();
  const rows = useLoad(() => api.workspace.org(), []);
  const catalog = useLoad(() => api.workspace.catalog(), []);
  const { busy, run } = useAction();
  const [editing, setEditing] = useState<RoleWorkspace | null>(null);
  const [resetting, setResetting] = useState<RoleWorkspace | null>(null);
  const [saving, setSaving] = useState(false);
  const presets = catalog.data?.presets ?? [];
  const blockTitle = (k: BlockKey) => catalog.data?.blocks.find((b) => b.key === k)?.title ?? t(`block.${k}`);
  const presetTitle = (k: string | null) => (k ? (presets.find((p) => p.key === k)?.title ?? k) : t("settings.workspace.custom"));
  // 后端清除布局后给的是 preset: null + 默认区块；区块（含坐标与宽高）恰好等于某个预设时按那个预设显示，不叫「自定义」
  const presetOf = (r: RoleWorkspace) => r.preset ?? presets.find((p) => sameLayout(p.blocks, r.blocks))?.key ?? null;

  const applyPreset = async (r: RoleWorkspace, preset: string) => {
    if (!preset) return;
    if (await run(r.role, () => api.workspace.saveRole(r.role, { preset }), t("settings.workspace.presetApplied", { title: presetTitle(preset), role: r.role_title }))) rows.reload();
  };
  const saveBlocks = async (blocks: LayoutBlock[]) => {
    if (!editing) return;
    setSaving(true);
    try {
      await api.workspace.saveRole(editing.role, { blocks });
      toast.ok(t("toast.saved"));
      setEditing(null);
      rows.reload();
    } catch (e) {
      toast.fail(t("workspace.saveFailed", { reason: errorMessage(e) }));
    } finally {
      setSaving(false);
    }
  };
  const reset = async (r: RoleWorkspace) => {
    if (await run(r.role, () => api.workspace.resetRole(r.role), t("settings.workspace.resetDone", { role: r.role_title }))) {
      setResetting(null);
      setEditing(null);
      rows.reload();
    }
  };

  const chips = (blocks: LayoutBlock[]) => (
    <span className="flex flex-wrap gap-1">
      {blocks.map((b, i) => (
        <Tag key={b.key} title={`${i + 1}. ${blockTitle(b.key)} · ${t("workspace.sizeChip", { w: b.w, h: b.h })}`}>
          <span className="inline-flex items-center gap-1.5">
            {blockTitle(b.key)}
            <SizeChip block={b} />
          </span>
        </Tag>
      ))}
      {blocks.length === 0 && <span className="text-ink-subtle">—</span>}
    </span>
  );

  return (
    <div className="space-y-4">
      <Panel index={1} title={t("settings.tab.workspace")} padded={false} telemetry={rows.data ? t("settings.workspace.roleCount", { n: rows.data.length }) : undefined}>
        <p className="border-b border-hairline px-4 py-3 text-caption text-ink-muted">{t("settings.workspace.description")}</p>
        {rows.loading && !rows.data ? (
          <TableSkeleton rows={5} cols={5} />
        ) : rows.error ? (
          <div className="p-4">
            <ErrorBox message={rows.error} onRetry={rows.reload} />
          </div>
        ) : !rows.data?.length ? (
          <Empty text={t("settings.roles.empty")} illustration={false} />
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-[140px]">{t("settings.workspace.role")}</th>
                <th className="num w-[72px]">{t("settings.workspace.members")}</th>
                <th className="w-[168px]">{t("settings.workspace.preset")}</th>
                <th>{t("settings.workspace.blocks")}</th>
                <th className="w-0" aria-label={t("common.actions")} />
              </tr>
            </thead>
            <tbody>
              {rows.data.map((r) => (
                <tr key={r.role}>
                  <td>
                    <Tag>{r.role_title}</Tag>
                  </td>
                  <td className="num tabular-nums">{r.member_count}</td>
                  <td className="!py-1.5">
                    <Select value={presetOf(r) ?? ""} onChange={(e) => void applyPreset(r, e.target.value)} disabled={busy === r.role || presets.length === 0} aria-label={t("settings.workspace.pickPreset")} className="w-full">
                      <option value="" disabled>
                        {t("settings.workspace.custom")}
                      </option>
                      {presets.map((p) => (
                        <option key={p.key} value={p.key}>
                          {p.title}
                        </option>
                      ))}
                    </Select>
                  </td>
                  <td>{chips(r.blocks)}</td>
                  <td className="whitespace-nowrap !py-1.5 align-middle">
                    <span className="row-actions inline-flex gap-1">
                      <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={() => setEditing(r)}>
                        {t("common.edit")}
                      </Button>
                      <Button size="sm" variant="ghost" disabled={busy === r.role} onClick={() => setResetting(r)}>
                        {t("settings.workspace.resetDefault")}
                      </Button>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>

      <Panel index={2} title={t("settings.workspace.presetsTitle")} padded={false} telemetry={catalog.data ? t("panel.rows", { n: presets.length }) : undefined}>
        {catalog.loading && !catalog.data ? (
          <TableSkeleton rows={5} cols={3} />
        ) : catalog.error ? (
          <div className="p-4">
            <ErrorBox message={catalog.error} onRetry={catalog.reload} />
          </div>
        ) : presets.length === 0 ? (
          <Empty text={t("settings.workspace.noPresets")} illustration={false} />
        ) : (
          <ul className="divide-y divide-hairline">
            {presets.map((p) => (
              <li key={p.key} className="grid gap-x-4 gap-y-1 px-4 py-2.5 md:grid-cols-[200px_minmax(0,1fr)]">
                <span className="min-w-0">
                  <span className="block text-body text-ink">{p.title}</span>
                  <span className="block text-caption text-ink-subtle">{p.description}</span>
                </span>
                <span className="self-center">{chips(p.blocks)}</span>
              </li>
            ))}
          </ul>
        )}
      </Panel>

      {/* 显示偏好的角色默认（DESIGN.md §20）：与工作台同一思路，个人 → 角色 → 默认 */}
      <RolePreferencesPanel index={3} />

      <WorkspaceEditorDialog
        open={!!editing}
        onClose={() => setEditing(null)}
        title={editing ? t("settings.workspace.editTitle", { role: editing.role_title }) : ""}
        description={t("settings.workspace.editSizeHint")}
        catalog={catalog.data}
        value={editing?.blocks ?? []}
        onSave={saveBlocks}
        onReset={editing ? () => setResetting(editing) : undefined}
        resetLabel={t("settings.workspace.resetDefault")}
        busy={saving}
        withPresets
      />
      <ConsequenceDialog
        open={!!resetting}
        title={resetting ? t("settings.workspace.resetTitle", { role: resetting.role_title }) : ""}
        effects={[t("settings.workspace.resetEffect.role"), t("settings.workspace.resetEffect.personal")]}
        danger={false}
        confirmLabel={t("settings.workspace.resetDefault")}
        busy={!!busy}
        onConfirm={() => resetting && void reset(resetting)}
        onClose={() => setResetting(null)}
      />
    </div>
  );
}
