"use client";
import { t } from "@/lib/i18n";
import { IconClose, IconPlus } from "@/components/icons";
import { Tag, cx } from "@/components/ui";
import { InlineSelect } from "./InlineSelect";

/*
 * 所需能力（DESIGN.md §15）：组织能力标签的芯片；能改的人在芯片上悬停出现 ×，末尾一个「添加」下拉列出还没选的标签。
 * 每次增删都是一个请求（整张列表发过去）。
 */
export function InlineCapabilities({ value, titles, onChange, editable, className }: { value: string[]; titles: Record<string, string>; onChange: (next: string[]) => void; editable: boolean; className?: string }) {
  const rest = Object.keys(titles).filter((c) => !value.includes(c));
  const title = (c: string) => titles[c] ?? c;
  if (!editable && !value.length) return <span className={cx("inl-static text-ink-subtle", className)}>—</span>;
  return (
    <span className={cx("inline-flex max-w-full flex-wrap items-center gap-1", className)}>
      {value.map((c) => (
        <Tag key={c}>
          {title(c)}
          {editable && (
            <button type="button" className="inl-chip-x" aria-label={t("inline.remove", { name: title(c) })} onClick={() => onChange(value.filter((x) => x !== c))}>
              <IconClose size={10} />
            </button>
          )}
        </Tag>
      ))}
      {editable && rest.length > 0 && (
        <InlineSelect
          value={null}
          editable
          options={rest.map((c) => ({ value: c, label: title(c) }))}
          onChange={(c) => c && onChange([...value, c])}
          ariaLabel={t("inline.add")}
          display={<span className="inline-flex items-center gap-1 text-caption text-ink-subtle"><IconPlus size={12} />{t("inline.add")}</span>}
          className="-my-1"
        />
      )}
    </span>
  );
}
