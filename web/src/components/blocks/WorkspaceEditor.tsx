"use client";
import { useState, type ReactNode } from "react";
import type { LayoutBlock, WorkspaceCatalog } from "@/lib/api";
import { t } from "@/lib/i18n";
import { Button, Dialog, Select } from "@/components/ui";
import { WorkspaceGrid } from "./WorkspaceGrid";

/*
 * 角色布局编辑器（ADR 0015 补记二 / 三）：组织设置「工作台」页签用，一个几乎通栏的对话框里放同一张 12 栏网格（编辑态，拖动位置、拖边框改大小），
 * 上面可以「从预设开始」把整套带坐标的区块换进来，下面保存 / 取消 / 恢复默认。
 * 区块里显示的是当前管理员自己能看到的数据，只作预览。保存前所有改动只在本地。
 * 「我的工作」的个人编辑不用这个对话框——它直接把页面切成编辑态（见 app/page.tsx）。
 */
export function WorkspaceEditorDialog({
  open,
  onClose,
  title,
  description,
  catalog,
  value,
  onSave,
  onReset,
  resetLabel,
  busy,
  withPresets = false,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  catalog: WorkspaceCatalog | null;
  /** 当前布局（带坐标与宽高） */
  value: LayoutBlock[];
  onSave: (blocks: LayoutBlock[]) => void | Promise<void>;
  /** 「恢复默认」：角色 = 回到系统默认 */
  onReset?: () => void | Promise<void>;
  resetLabel?: string;
  busy?: boolean;
  /** 显示"从预设开始"下拉，选中后把网格换成预设的区块（带预设的宽高） */
  withPresets?: boolean;
}) {
  const [draft, setDraft] = useState<LayoutBlock[]>([]);
  // 每次打开都从调用方给的布局重来（渲染期同步，和 RolesTab 的做法一致）
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const key = open ? value.map((b) => `${b.key}:${b.x}:${b.y}:${b.w}:${b.h}`).join(",") : null;
  if (key !== loadedFor) {
    setLoadedFor(key);
    if (key !== null) setDraft(value.map((b) => ({ ...b })));
  }
  const canSave = draft.length > 0 && !busy;
  const applyPreset = (presetKey: string) => {
    const p = catalog?.presets.find((x) => x.key === presetKey);
    if (p) setDraft(p.blocks.map((b) => ({ ...b })));
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={title}
      width={1400}
      footer={
        <>
          {onReset && (
            <Button variant="ghost" className="mr-auto" disabled={busy} onClick={() => void onReset()}>
              {resetLabel ?? t("settings.workspace.resetDefault")}
            </Button>
          )}
          <Button onClick={onClose}>{t("workspace.cancel")}</Button>
          <Button variant="primary" disabled={!canSave} onClick={() => void onSave(draft)}>
            {busy ? t("workspace.saving") : t("workspace.save")}
          </Button>
        </>
      }
    >
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        {description && <p className="min-w-0 flex-1 text-caption text-ink-subtle">{description}</p>}
        {withPresets && catalog && catalog.presets.length > 0 && (
          <label className="flex shrink-0 items-center gap-2 text-caption text-ink-muted">
            <span className="shrink-0">{t("settings.workspace.startFrom")}</span>
            <Select value="" onChange={(e) => applyPreset(e.target.value)} className="min-w-[160px]">
              <option value="">{t("settings.workspace.pickPreset")}</option>
              {catalog.presets.map((p) => (
                <option key={p.key} value={p.key}>
                  {p.title}
                </option>
              ))}
            </Select>
          </label>
        )}
      </div>
      <p className="text-caption text-ink-subtle" title={t("workspace.keyboardHint")}>
        {t("workspace.resizeHint")}
      </p>
      <div className="-mx-1 max-h-[calc(100vh-260px)] overflow-y-auto px-1 py-1">
        <WorkspaceGrid blocks={draft} catalog={catalog} editing onChange={setDraft} />
      </div>
      <p className="text-caption text-ink-subtle" role={draft.length === 0 ? "alert" : undefined}>
        {draft.length === 0 ? t("workspace.needOne") : t("workspace.shownCount", { n: draft.length, total: catalog?.blocks.length ?? draft.length })}
      </p>
    </Dialog>
  );
}
