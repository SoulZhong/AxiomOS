"use client";
import { useState, type ReactNode } from "react";
import { type BlockKey, type WorkspaceCatalog } from "@/lib/api";
import { t } from "@/lib/i18n";
import { IconChevronDown } from "@/components/icons";
import { Button, Checkbox, Drawer, ListSkeleton, Select, cx } from "@/components/ui";

/*
 * 工作台编辑抽屉（ADR 0015）：个人「调整首页」与组织设置里给角色配布局共用。
 * 一张全部区块的清单：勾选 = 显示，上下键排序（纯按钮，键盘可达，不引入拖拽库）；
 * 隐藏的区块留在清单里灰掉，随时勾回来。至少保留一个区块才能保存。
 * 数据本身不在这里请求：调用方给目录（GET /workspace/blocks）与当前顺序，保存时拿到区块键数组。
 */
interface Row {
  key: BlockKey;
  shown: boolean;
}

export function WorkspaceEditor({
  open,
  onClose,
  title,
  description,
  caption,
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
  /** 一句说明当前布局从哪来（个人抽屉用） */
  caption?: ReactNode;
  catalog: WorkspaceCatalog | null;
  /** 当前显示的区块（按顺序） */
  value: BlockKey[];
  onSave: (blocks: BlockKey[]) => void | Promise<void>;
  /** 「恢复默认」：个人 = 回到角色默认；角色 = 回到系统默认 */
  onReset?: () => void | Promise<void>;
  resetLabel?: string;
  busy?: boolean;
  /** 显示"套用预设"下拉（角色抽屉用），选中后把清单换成预设的区块 */
  withPresets?: boolean;
}) {
  const [rows, setRows] = useState<Row[]>([]);
  // 抽屉每次打开都从调用方给的顺序重来（渲染期同步，和 RolesTab 的做法一致）
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const key = open ? `${value.join(",")}|${catalog?.blocks.map((b) => b.key).join(",") ?? ""}` : null;
  if (key !== loadedFor) {
    setLoadedFor(key);
    if (key && catalog) setRows(buildRows(value, catalog));
  }
  const titleOf = (k: BlockKey) => catalog?.blocks.find((b) => b.key === k)?.title ?? t(`block.${k}`);
  const descOf = (k: BlockKey) => catalog?.blocks.find((b) => b.key === k)?.description ?? "";
  const shownKeys = rows.filter((r) => r.shown).map((r) => r.key);
  const canSave = shownKeys.length > 0 && !busy;

  const move = (i: number, dir: -1 | 1) => {
    const j = i + dir;
    if (j < 0 || j >= rows.length) return;
    const next = [...rows];
    [next[i], next[j]] = [next[j], next[i]];
    setRows(next);
  };
  const toggle = (i: number, shown: boolean) => setRows(rows.map((r, idx) => (idx === i ? { ...r, shown } : r)));
  const applyPreset = (presetKey: string) => {
    const p = catalog?.presets.find((x) => x.key === presetKey);
    if (p && catalog) setRows(buildRows(p.blocks, catalog));
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={title}
      description={description}
      footer={
        <>
          {onReset && (
            <Button variant="ghost" className="mr-auto" disabled={busy} onClick={() => void onReset()}>
              {resetLabel ?? t("workspace.resetRole")}
            </Button>
          )}
          <Button onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" disabled={!canSave} onClick={() => void onSave(shownKeys)}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      {caption && <p className="mb-3 text-caption text-ink-subtle">{caption}</p>}
      {withPresets && catalog && catalog.presets.length > 0 && (
        <label className="mb-3 flex items-center gap-2 text-caption text-ink-muted">
          <span className="shrink-0">{t("settings.workspace.startFrom")}</span>
          <Select value="" onChange={(e) => applyPreset(e.target.value)} className="min-w-0 flex-1">
            <option value="">{t("settings.workspace.pickPreset")}</option>
            {catalog.presets.map((p) => (
              <option key={p.key} value={p.key}>
                {p.title}
              </option>
            ))}
          </Select>
        </label>
      )}
      {!catalog ? (
        <ListSkeleton rows={6} />
      ) : (
        <ol className="divide-y divide-hairline rounded-md border border-hairline" aria-label={t("workspace.blockList")}>
          {rows.map((r, i) => (
            <li key={r.key} className={cx("flex items-center gap-3 px-3 py-2", !r.shown && "bg-surface-2/50")}>
              <span className="w-5 shrink-0 text-right font-mono text-[11px] tabular-nums text-ink-tertiary" aria-hidden="true">
                {r.shown ? String(shownKeys.indexOf(r.key) + 1).padStart(2, "0") : "—"}
              </span>
              <Checkbox
                checked={r.shown}
                onChange={(e) => toggle(i, e.target.checked)}
                className="min-w-0 flex-1 !items-start"
                label={
                  <span className="min-w-0">
                    <span className={cx("block text-body", r.shown ? "text-ink" : "text-ink-subtle")}>{titleOf(r.key)}</span>
                    {descOf(r.key) && <span className="block truncate text-caption text-ink-subtle">{descOf(r.key)}</span>}
                  </span>
                }
              />
              <span className="inline-flex shrink-0 gap-0.5">
                <button
                  type="button"
                  className="pressable rounded-md p-1 text-ink-subtle hover:bg-surface-3 hover:text-ink disabled:opacity-30 disabled:hover:bg-transparent"
                  aria-label={t("workspace.moveUp", { title: titleOf(r.key) })}
                  disabled={i === 0}
                  onClick={() => move(i, -1)}
                >
                  <IconChevronDown className="rotate-180" />
                </button>
                <button
                  type="button"
                  className="pressable rounded-md p-1 text-ink-subtle hover:bg-surface-3 hover:text-ink disabled:opacity-30 disabled:hover:bg-transparent"
                  aria-label={t("workspace.moveDown", { title: titleOf(r.key) })}
                  disabled={i === rows.length - 1}
                  onClick={() => move(i, 1)}
                >
                  <IconChevronDown />
                </button>
              </span>
            </li>
          ))}
        </ol>
      )}
      <p className={cx("mt-2 text-caption", shownKeys.length === 0 ? "text-danger" : "text-ink-subtle")} role={shownKeys.length === 0 ? "alert" : undefined}>
        {shownKeys.length === 0 ? t("workspace.needOne") : t("workspace.shownCount", { n: shownKeys.length, total: rows.length })}
      </p>
    </Drawer>
  );
}

/** 当前显示的在前（按给的顺序），其余按目录顺序排在后面并标为隐藏。 */
function buildRows(value: BlockKey[], catalog: WorkspaceCatalog): Row[] {
  const known = new Set(catalog.blocks.map((b) => b.key));
  const shown = value.filter((k) => known.has(k));
  const rest = catalog.blocks.map((b) => b.key).filter((k) => !shown.includes(k));
  return [...shown.map((key) => ({ key, shown: true })), ...rest.map((key) => ({ key, shown: false }))];
}
