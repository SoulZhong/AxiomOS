"use client";
import { type PrefField, type PreferencesCatalog, type PreferencesPatch, type PrefSource, type PrefValues } from "@/lib/api";
import { t } from "@/lib/i18n";
import { Checkbox, Segmented, Switch, Tag, cx } from "@/components/ui";

/**
 * 显示偏好的五行（DESIGN.md §20）：任务列表的列、看板卡片字段、默认任务视图、紧凑、侧栏默认收起。
 * 「我的偏好」与组织设置里的角色默认共用：每行右侧一枚来源标签（个人 / 来自角色「X」/ 默认），改一项就 onChange 一次（调用方立即保存）。
 * 列表与字段按目录顺序勾选，「标题」固定勾上；每次给出的是整份数组（后端要的就是整份）。
 */
export function PreferenceForm({ value, catalog, sources, roleTitle, busy, onChange, sourceOf }: {
  /** 解析后的当前值（个人页是 GET /me/preferences；角色页是角色设的 ∪ 系统默认） */
  value: PrefValues;
  catalog: PreferencesCatalog;
  sources: Record<PrefField, PrefSource>;
  /** 来源是角色时显示的角色名 */
  roleTitle?: string;
  busy?: boolean;
  onChange: (patch: PreferencesPatch) => void;
  /** 自定义来源标签的文案（角色页：「角色」/「默认」） */
  sourceOf?: (src: PrefSource) => string;
}) {
  const label = (src: PrefSource) => (sourceOf ? sourceOf(src) : src === "personal" ? t("pref.source.personal") : src === "role" ? t("pref.source.role", { role: roleTitle ?? "" }) : t("pref.source.default"));
  const source = (f: PrefField) => <Tag tone={sources[f] === "default" ? "neutral" : "accent"} className="self-start">{label(sources[f])}</Tag>;
  const toggle = (f: "task_list_columns" | "task_card_fields", key: string, on: boolean) => {
    const order = (f === "task_list_columns" ? catalog.columns : catalog.card_fields).map((c) => c.key);
    const set = new Set(value[f]);
    if (on) set.add(key);
    else set.delete(key);
    if (f === "task_list_columns") set.add("title");
    onChange({ [f]: order.filter((k) => set.has(k)) });
  };
  return (
    <div className={cx(busy && "opacity-70")} aria-busy={busy || undefined} data-pref-form="">
      <div className="pref-row" data-pref="task_list_columns">
        <div className="min-w-0">
          <div className="text-body text-ink">{t("pref.columns")}</div>
          <p className="mb-2 text-caption text-ink-subtle">{t("pref.columnsHint")}</p>
          <div className="flex flex-wrap gap-x-4 gap-y-1.5">
            {catalog.columns.map((c) => (
              <Checkbox key={c.key} label={c.title} checked={c.key === "title" || value.task_list_columns.includes(c.key)} disabled={busy || c.key === "title"} onChange={(e) => toggle("task_list_columns", c.key, e.target.checked)} />
            ))}
          </div>
        </div>
        {source("task_list_columns")}
      </div>
      <div className="pref-row" data-pref="task_card_fields">
        <div className="min-w-0">
          <div className="text-body text-ink">{t("pref.cardFields")}</div>
          <p className="mb-2 text-caption text-ink-subtle">{t("pref.cardFieldsHint")}</p>
          <div className="flex flex-wrap gap-x-4 gap-y-1.5">
            {catalog.card_fields.map((c) => (
              <Checkbox key={c.key} label={c.title} checked={value.task_card_fields.includes(c.key)} disabled={busy} onChange={(e) => toggle("task_card_fields", c.key, e.target.checked)} />
            ))}
          </div>
        </div>
        {source("task_card_fields")}
      </div>
      <div className="pref-row" data-pref="default_task_view">
        <div className="min-w-0">
          <div className="text-body text-ink">{t("pref.defaultView")}</div>
          <p className="mb-2 text-caption text-ink-subtle">{t("pref.defaultViewHint")}</p>
          <Segmented size="sm" value={value.default_task_view} onChange={(v) => onChange({ default_task_view: v })} aria-label={t("pref.defaultView")} options={catalog.views.map((v) => [v.key, v.title])} />
        </div>
        {source("default_task_view")}
      </div>
      <div className="pref-row" data-pref="compact">
        <div className="flex min-w-0 items-start gap-3">
          <Switch checked={value.compact} disabled={busy} onChange={(v) => onChange({ compact: v })} label={t("pref.compact")} className="mt-0.5" />
          <div className="min-w-0">
            <div className="text-body text-ink">{t("pref.compact")}</div>
            <p className="text-caption text-ink-subtle">{t("pref.compactHint")}</p>
          </div>
        </div>
        {source("compact")}
      </div>
      <div className="pref-row" data-pref="sidebar_collapsed">
        <div className="flex min-w-0 items-start gap-3">
          <Switch checked={value.sidebar_collapsed} disabled={busy} onChange={(v) => onChange({ sidebar_collapsed: v })} label={t("pref.sidebar")} className="mt-0.5" />
          <div className="min-w-0">
            <div className="text-body text-ink">{t("pref.sidebar")}</div>
            <p className="text-caption text-ink-subtle">{t("pref.sidebarHint")}</p>
          </div>
        </div>
        {source("sidebar_collapsed")}
      </div>
    </div>
  );
}
