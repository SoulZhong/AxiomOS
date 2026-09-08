"use client";
import { useEffect, useRef, useState } from "react";
import type { ISODate, Milestone } from "@/lib/api";
import { t } from "@/lib/i18n";
import { Floating } from "@/components/gantt/Floating";
import { MilestoneSwatch, milestoneTipParts } from "@/components/gantt/MilestoneMarks";
import { IconCheck, IconEdit, IconTrash } from "@/components/icons";
import { Button, DateInput, Input, Tag } from "@/components/ui";

/*
 * 点开一枚菱形的小气泡（ADR 0016、0022 补记）：状态 + 日期 + 说明 + 四个动作，编辑时换成三个输入。
 * 原来长在「目标 · 甘特图」里，那个页签并进路线图之后搬到这里，路线图与目标详情共用同一套说法。
 * 改不动这个目标的人（editable=false）只看得到内容，看不到动作。
 */
export function MilestonePopover({
  m,
  rect,
  editable,
  editing,
  onEdit,
  onClose,
  onReach,
  onUnreach,
  onSave,
  onDelete,
}: {
  m: Milestone;
  rect: DOMRect;
  editable: boolean;
  editing: boolean;
  onEdit: (on: boolean) => void;
  onClose: () => void;
  onReach: () => void;
  onUnreach: () => void;
  onSave: (input: { title: string; due_on: ISODate; description: string }) => void;
  onDelete: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [form, setForm] = useState({ title: m.title, due_on: m.due_on, description: m.description ?? "" });
  useEffect(() => {
    const onDown = (e: PointerEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    // 延一帧再挂：打开它的那一下 pointerup 不算"外面"
    const id = window.setTimeout(() => {
      window.addEventListener("pointerdown", onDown);
      window.addEventListener("keydown", onKey);
    }, 0);
    return () => {
      window.clearTimeout(id);
      window.removeEventListener("pointerdown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [onClose]);
  const p = milestoneTipParts(m);
  return (
    <Floating rect={rect} place="bottom" className="gantt-ms-pop" role="dialog" aria-label={m.title} innerRef={ref}>
      {editing ? (
        <form
          className="space-y-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (form.title.trim() && form.due_on) onSave({ title: form.title.trim(), due_on: form.due_on, description: form.description.trim() });
          }}
        >
          <Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder={t("ms.titlePlaceholder")} autoFocus aria-label={t("ms.title")} />
          <DateInput value={form.due_on} onChange={(e) => setForm({ ...form, due_on: e.target.value })} required aria-label={t("goal.plan")} />
          <Input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} placeholder={t("ms.descriptionPlaceholder")} aria-label={t("ms.descriptionPlaceholder")} />
          <div className="flex justify-end gap-1.5 pt-1">
            <Button size="sm" variant="ghost" onClick={() => onEdit(false)}>{t("common.cancel")}</Button>
            <Button size="sm" variant="primary" type="submit" disabled={!form.title.trim() || !form.due_on}>{t("common.save")}</Button>
          </div>
        </form>
      ) : (
        <>
          <div className="flex items-start gap-2">
            <span className="mt-0.5 inline-flex shrink-0"><MilestoneSwatch status={m.status} /></span>
            <div className="min-w-0 flex-1">
              <div className="truncate text-body font-medium text-ink" title={m.title}>{m.title}</div>
              <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-caption">
                <span className="font-mono text-telemetry">{m.due_on}</span>
                <Tag tone={m.status === "overdue" ? "danger" : m.status === "reached" ? "success" : "neutral"}>{p.status}</Tag>
                {p.ready && <span className="text-success">{t("ms.ready")}</span>}
              </div>
              {m.description && <p className="mt-1 text-caption text-ink-muted">{m.description}</p>}
            </div>
          </div>
          {editable && (
            <div className="mt-2.5 flex items-center gap-1.5 border-t border-hairline pt-2.5">
              {m.status === "reached" ? (
                <Button size="sm" onClick={onUnreach}>{t("ms.unreach")}</Button>
              ) : (
                <Button size="sm" variant="primary" icon={<IconCheck />} onClick={onReach}>{t("ms.reach")}</Button>
              )}
              <span className="ml-auto inline-flex gap-1">
                <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={() => onEdit(true)}>{t("common.edit")}</Button>
                <Button size="sm" variant="ghost" icon={<IconTrash />} onClick={onDelete}>{t("common.delete")}</Button>
              </span>
            </div>
          )}
        </>
      )}
    </Floating>
  );
}
