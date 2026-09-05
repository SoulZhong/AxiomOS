"use client";
import { useState, type FormEvent } from "react";
import { api, type Goal, type Milestone, type MilestoneStatus } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import { useAction } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconCheck, IconEdit, IconMilestone, IconPlus, IconTrash } from "@/components/icons";
import { Button, ConfirmDialog, DateInput, Input, Panel, Tag, Tip, cx, type Tone } from "@/components/ui";

/*
 * 目标详情 · 「里程碑」面板（DESIGN.md §11、ADR 0016）：按日期列出，每条带状态芯片、等宽日期、说明、
 * 「确认已达到 / 撤销」，行内悬停出现编辑 / 删除；末尾一行内联新增（名称 + 日期）。
 * 每次写操作后由父级重新加载目标（里程碑随目标对象一起回来）。
 */

const TONE: Record<MilestoneStatus, Tone> = { upcoming: "neutral", reached: "success", overdue: "danger" };

export function MilestonePanel({ goal, index, onChanged }: { goal: Goal; index: number; onChanged: () => void }) {
  const list = [...(goal.milestones ?? [])].sort((a, b) => (a.due_on < b.due_on ? -1 : a.due_on > b.due_on ? 1 : 0));
  const { busy, run } = useAction();
  const [editing, setEditing] = useState<{ id: string; title: string; due_on: string; description: string } | null>(null);
  const [adding, setAdding] = useState({ title: "", due_on: "" });
  const [deleting, setDeleting] = useState<Milestone | null>(null);
  const summary = goal.milestone_summary;

  const reach = (m: Milestone) => void run(m.id, () => api.milestones.reach(m.id), t("ms.reachedToast", { title: m.title })).then((ok) => ok && onChanged());
  const unreach = (m: Milestone) => void run(m.id, () => api.milestones.unreach(m.id), t("ms.unreachedToast", { title: m.title })).then((ok) => ok && onChanged());
  const saveEdit = (e: FormEvent) => {
    e.preventDefault();
    if (!editing || !editing.title.trim() || !editing.due_on) return;
    const { id, title, due_on, description } = editing;
    void run(id, () => api.milestones.update(id, { title: title.trim(), due_on, description: description.trim() }), t("ms.updatedToast", { title: title.trim() })).then((ok) => {
      if (ok) {
        setEditing(null);
        onChanged();
      }
    });
  };
  const add = (e: FormEvent) => {
    e.preventDefault();
    const title = adding.title.trim();
    if (!title || !adding.due_on) return;
    void run("add", () => api.milestones.create(goal.id, { title, due_on: adding.due_on }), t("ms.createdToast", { title, date: fmtDate(adding.due_on) })).then((ok) => {
      if (ok) {
        setAdding({ title: "", due_on: "" });
        onChanged();
      }
    });
  };
  const doDelete = () => {
    const m = deleting;
    if (!m) return;
    void run(m.id, () => api.milestones.remove(m.id), t("ms.deletedToast", { title: m.title })).then((ok) => {
      if (ok) {
        setDeleting(null);
        onChanged();
      }
    });
  };

  return (
    <Panel index={index} icon={<IconMilestone />} title={t("ms.title")} telemetry={summary ? `${t("ms.summary", { reached: summary.reached, total: summary.total })}${summary.overdue ? ` · ${t("ms.summaryOverdue", { n: summary.overdue })}` : ""}` : undefined} padded={false}>
      {list.length === 0 && <p className="border-b border-hairline px-4 py-3 text-body text-ink-subtle">{t("ms.empty")}</p>}
      <ul>
        {list.map((m) =>
          editing?.id === m.id ? (
            <li key={m.id} className="border-b border-hairline bg-surface-2 px-4 py-2">
              <form onSubmit={saveEdit} className="flex flex-wrap items-center gap-2">
                <Input value={editing.title} onChange={(e) => setEditing({ ...editing, title: e.target.value })} placeholder={t("ms.titlePlaceholder")} className="min-w-[200px] flex-1" autoFocus aria-label={t("ms.title")} />
                <DateInput value={editing.due_on} onChange={(e) => setEditing({ ...editing, due_on: e.target.value })} className="w-40" required aria-label={t("goal.plan")} />
                <Input value={editing.description} onChange={(e) => setEditing({ ...editing, description: e.target.value })} placeholder={t("ms.descriptionPlaceholder")} className="min-w-[200px] flex-1" aria-label={t("ms.descriptionPlaceholder")} />
                <Button size="sm" variant="primary" type="submit" disabled={busy === m.id || !editing.title.trim() || !editing.due_on}>{t("common.save")}</Button>
                <Button size="sm" variant="ghost" onClick={() => setEditing(null)}>{t("common.cancel")}</Button>
              </form>
            </li>
          ) : (
            <li key={m.id} className="flex min-h-[44px] items-center gap-3 border-b border-hairline px-4 py-2 hover:bg-surface-2">
              <Tag tone={TONE[m.status]}>{t(`ms.status.${m.status}`)}</Tag>
              <span className="w-20 shrink-0 font-mono text-[12px] text-telemetry tabular-nums">{m.due_on}</span>
              <div className="min-w-0 flex-1">
                <div className={cx("truncate font-medium", m.status === "reached" ? "text-ink-muted" : "text-ink")} title={m.title}>{m.title}</div>
                {(m.description || m.status === "reached" || m.ready_hint) && (
                  <div className="mt-0.5 flex flex-wrap items-center gap-x-3 text-caption text-ink-subtle">
                    {m.description && <span className="min-w-0 truncate">{m.description}</span>}
                    {m.status === "reached" && m.reached_at && <span className="whitespace-nowrap">{t("ms.reachedAt", { date: fmtDate(m.reached_at) })}</span>}
                    {m.ready_hint && m.status !== "reached" && <span className="whitespace-nowrap text-success">{t("ms.ready")}</span>}
                  </div>
                )}
              </div>
              {m.status === "reached" ? (
                <Button size="sm" variant="ghost" disabled={busy === m.id} onClick={() => unreach(m)}>{t("ms.unreach")}</Button>
              ) : (
                <Button size="sm" variant={m.ready_hint ? "primary" : "default"} icon={<IconCheck />} disabled={busy === m.id} onClick={() => reach(m)}>{t("ms.reach")}</Button>
              )}
              <span className="row-actions shrink-0 whitespace-nowrap">
                <Tip tip={t("common.edit")} placement="bottom"><Button size="sm" variant="ghost" icon={<IconEdit />} aria-label={t("common.edit")} onClick={() => setEditing({ id: m.id, title: m.title, due_on: m.due_on, description: m.description ?? "" })} /></Tip>
                <Tip tip={t("common.delete")} placement="bottom"><Button size="sm" variant="ghost" icon={<IconTrash />} aria-label={t("common.delete")} disabled={busy === m.id} onClick={() => setDeleting(m)} /></Tip>
              </span>
            </li>
          ),
        )}
        {/* 末尾一行内联新增：名称 + 日期，回车即建 */}
        <li className="px-4 py-2">
          <form onSubmit={add} className="flex flex-wrap items-center gap-2">
            <span className="inline-flex shrink-0 text-ink-subtle" aria-hidden="true"><IconMilestone size={16} /></span>
            <Input value={adding.title} onChange={(e) => setAdding({ ...adding, title: e.target.value })} placeholder={t("ms.titlePlaceholder")} className="min-w-[200px] flex-1" aria-label={t("ms.add")} />
            <DateInput value={adding.due_on} onChange={(e) => setAdding({ ...adding, due_on: e.target.value })} className="w-40" aria-label={t("goal.plan")} />
            <Button size="sm" variant="primary" type="submit" icon={<IconPlus />} disabled={busy === "add" || !adding.title.trim() || !adding.due_on}>{t("ms.add")}</Button>
          </form>
        </li>
      </ul>
      <ConfirmDialog open={!!deleting} title={t("ms.deleteTitle")} message={deleting ? t("ms.deleteMessage", { title: deleting.title, date: fmtDate(deleting.due_on) }) : undefined} confirmLabel={t("common.delete")} danger busy={busy === deleting?.id} onConfirm={doDelete} onClose={() => setDeleting(null)} />
    </Panel>
  );
}
