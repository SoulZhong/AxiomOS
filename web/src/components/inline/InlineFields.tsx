"use client";
import type { TaskType } from "@/lib/api";
import { InlineDate } from "./InlineDate";
import { InlineField, InlineTable } from "./InlineField";
import { InlineNumber, InlineTextInput } from "./InlineNumber";
import { InlineSelect } from "./InlineSelect";
import type { InlineSaveState } from "./useInlineSave";

/*
 * 自定义字段（DESIGN.md §15）：按任务类型的 task_schema（JSON Schema 的 properties）逐行渲染：
 * 文本 / 数字 / 日期（format: date）/ 枚举（enum）各用对应的值控件；数据里有、定义里没有的字段照常显示为文本。
 * 已结束的任务这些行照样能改（备注类字段）。
 */
interface PropDef { type?: string; title?: string; format?: string; enum?: unknown[]; description?: string }

export function schemaProps(schema: TaskType["task_schema"] | null | undefined): Record<string, PropDef> {
  const props = (schema as { properties?: Record<string, PropDef> } | null | undefined)?.properties;
  return props && typeof props === "object" ? props : {};
}

export function InlineFields({ schema, values, onChange, editable, stateOf, className }: {
  schema: TaskType["task_schema"] | null | undefined;
  values: Record<string, unknown>;
  onChange: (key: string, next: unknown) => void;
  editable: boolean;
  stateOf: (key: string) => InlineSaveState;
  className?: string;
}) {
  const props = schemaProps(schema);
  const keys = [...Object.keys(props), ...Object.keys(values).filter((k) => !(k in props))];
  if (!keys.length) return null;
  return (
    <InlineTable className={className}>
      {keys.map((key) => {
        const def = props[key] ?? {};
        const v = values[key];
        const st = stateOf(key);
        const label = def.title ?? key;
        let control;
        if (Array.isArray(def.enum) && def.enum.length) {
          control = <InlineSelect value={v === undefined || v === null || v === "" ? null : String(v)} options={def.enum.map((e) => ({ value: String(e), label: String(e) }))} onChange={(next) => onChange(key, next)} editable={editable} nullable ariaLabel={label} />;
        } else if (def.type === "number" || def.type === "integer") {
          control = <InlineNumber value={typeof v === "number" ? v : v === undefined || v === null || v === "" ? null : Number(v)} onChange={(next) => onChange(key, next)} editable={editable} step={def.type === "integer" ? 1 : "any"} ariaLabel={label} />;
        } else if (def.format === "date") {
          control = <InlineDate value={typeof v === "string" && v ? v : null} onChange={(next) => onChange(key, next)} editable={editable} ariaLabel={label} />;
        } else {
          control = <InlineTextInput value={v === undefined || v === null ? "" : String(v)} onChange={(next) => onChange(key, next)} editable={editable} ariaLabel={label} />;
        }
        return (
          <InlineField key={key} label={label} status={st.status} error={st.error}>
            {control}
          </InlineField>
        );
      })}
    </InlineTable>
  );
}
