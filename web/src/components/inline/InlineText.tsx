"use client";
import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { t } from "@/lib/i18n";
import { IconEdit } from "@/components/icons";
import { Button, cx } from "@/components/ui";
import { useInlineSave } from "./useInlineSave";

/*
 * 多行文本就地编辑（描述，DESIGN.md §15）：点击进入自动增高的文本域，底部「保存 / 取消」两个小按钮；
 * ⌘/Ctrl+Enter 保存，Esc 取消。失败留在编辑态并在按钮旁说明原因。
 */
export function InlineText({ value, onSave, editable, placeholder, className }: { value: string; onSave: (next: string) => Promise<unknown>; editable: boolean; placeholder: string; className?: string }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const ref = useRef<HTMLTextAreaElement>(null);
  const save = useInlineSave();

  const grow = () => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "0px";
    el.style.height = `${Math.max(72, el.scrollHeight + 2)}px`;
  };
  useEffect(() => {
    if (!editing) return;
    grow();
    const el = ref.current;
    el?.focus();
    el?.setSelectionRange(el.value.length, el.value.length);
  }, [editing]);

  const start = () => {
    if (!editable) return;
    setDraft(value);
    save.clear();
    setEditing(true);
  };
  const cancel = () => setEditing(false);
  const commit = async () => {
    if (draft === value) { setEditing(false); return; }
    const ok = await save.run(() => onSave(draft));
    if (ok) setEditing(false);
  };
  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Escape") { e.preventDefault(); cancel(); }
    else if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); void commit(); }
  };

  if (editing) {
    const busy = save.status === "saving";
    return (
      <div className={className}>
        <textarea ref={ref} value={draft} onChange={(e) => { setDraft(e.target.value); grow(); }} onKeyDown={onKey} onInput={grow} className="inl-textarea text-body" aria-label={t("inline.editDescription")} disabled={busy} />
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button size="sm" variant="primary" onClick={() => void commit()} disabled={busy}>{t("common.save")}</Button>
          <Button size="sm" variant="ghost" onClick={cancel} disabled={busy}>{t("common.cancel")}</Button>
          {save.status === "error" && save.error ? <span className="text-caption text-danger" role="alert">{save.error}</span> : <span className="hidden text-caption text-ink-tertiary sm:inline">{t("inline.descriptionHint")}</span>}
        </div>
      </div>
    );
  }
  if (!editable) {
    return <p className={cx("whitespace-pre-wrap text-body", className)}>{value || <span className="text-ink-subtle">{placeholder}</span>}</p>;
  }
  return (
    <div className={className}>
      <button type="button" className={cx("inl-para whitespace-pre-wrap text-body", save.status === "saving" && "opacity-60")} onClick={start} title={t("inline.editDescription")}>
        <IconEdit size={14} className="inl-pencil" aria-hidden="true" />
        {value || <span className="text-ink-subtle">{placeholder}</span>}
      </button>
      {save.status === "error" && save.error && <p className="mt-2 text-caption text-danger" role="alert">{save.error}</p>}
    </div>
  );
}
