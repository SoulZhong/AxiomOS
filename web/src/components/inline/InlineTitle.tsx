"use client";
import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { t } from "@/lib/i18n";
import { IconEdit } from "@/components/icons";
import { cx } from "@/components/ui";
import { useInlineSave } from "./useInlineSave";

/*
 * 标题就地编辑（DESIGN.md §15）：悬停淡底色 + 铅笔（铅笔在文字右侧，不盖字），点击变成同字号的输入框；
 * 回车保存、Esc 取消、失焦保存；空标题不保存并回到原文。不能改的人看到的就是 h1 文本。
 * onSave 抛错即失败：本组件回到原文并在标题下一句说明原因（乐观更新由调用方在 onSave 里做）。
 */
export function InlineTitle({ value, onSave, editable, caption, className, level = "h1" }: { value: string; onSave: (next: string) => Promise<unknown>; editable: boolean; /** 不能改时的说明（如「已结束，改不了计划」） */ caption?: ReactNode; className?: string; level?: "h1" | "h2" }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const ref = useRef<HTMLInputElement>(null);
  const skipBlur = useRef(false);
  const save = useInlineSave();
  const Heading = level;

  useEffect(() => {
    if (editing) {
      ref.current?.focus();
      ref.current?.select();
    }
  }, [editing]);

  const start = () => {
    if (!editable) return;
    setDraft(value);
    save.clear();
    setEditing(true);
  };
  const cancel = () => {
    skipBlur.current = true;
    setEditing(false);
  };
  const commit = () => {
    const next = draft.trim();
    setEditing(false);
    if (!next || next === value) return;
    void save.run(() => onSave(next));
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") { e.preventDefault(); skipBlur.current = true; commit(); }
    else if (e.key === "Escape") { e.preventDefault(); cancel(); }
  };
  const onBlur = () => {
    if (skipBlur.current) { skipBlur.current = false; return; }
    commit();
  };

  if (editing) {
    return (
      <Heading className={cx("text-headline max-w-full min-w-0 flex-1 text-ink", className)}>
        {/* 输入框随字数变宽（中文一字一 em），最少 12 字宽、最多整行：右侧徽标不会被推到最右 */}
        <input ref={ref} value={draft} onChange={(e) => setDraft(e.target.value)} onKeyDown={onKey} onBlur={onBlur} className="inl-title-input" style={{ width: `min(100%, calc(${Math.max(12, Array.from(draft).length + 1)}em + 16px))` }} aria-label={t("inline.editTitle")} maxLength={200} />
      </Heading>
    );
  }
  return (
    <Heading className={cx("text-headline max-w-full min-w-0 text-ink", className)}>
      {editable ? (
        <button type="button" className={cx("inl-title", save.status === "saving" && "opacity-60")} onClick={start} title={t("inline.editTitle")}>
          <span className="min-w-0">{value}</span>
          <IconEdit size={16} className="inl-pencil" aria-hidden="true" />
        </button>
      ) : (
        <span className="inline-flex flex-wrap items-baseline gap-x-2">
          <span>{value}</span>
          {caption && <span className="text-caption font-normal tracking-normal text-ink-subtle">{caption}</span>}
        </span>
      )}
      {save.status === "error" && save.error && <span className="mt-1 block text-caption font-normal tracking-normal text-danger" role="alert">{save.error}</span>}
    </Heading>
  );
}
