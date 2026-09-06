"use client";
import { useState, type FormEvent } from "react";
import { api, type Sprint, type SprintCloseResult } from "@/lib/api";
import { errorMessage } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useToast } from "@/components/toast";
import { Button, Dialog, Field, Select } from "@/components/ui";

/** 结束迭代：未完成的任务退回待办，或转入同一团队的某个规划中迭代。 */
export function SprintCloseDialog({ open, onClose, sprint, candidates, unfinished, onClosed }: { open: boolean; onClose: () => void; sprint: Sprint | null; candidates: Sprint[]; unfinished?: number; onClosed: (r: SprintCloseResult) => void }) {
  const toast = useToast();
  const [mode, setMode] = useState<"backlog" | "next">("backlog");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastOpen, setLastOpen] = useState(false);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (open) {
      setMode(candidates.length ? "next" : "backlog");
      setNext(candidates[0]?.id ?? "");
      setError(null);
    }
  }
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!sprint) return;
    setBusy(true);
    setError(null);
    try {
      const r = await api.sprints.close(sprint.id, mode === "next" ? { unfinished: "next", next_sprint_id: next } : { unfinished: "backlog" });
      toast.ok(t("sprints.closed", { name: sprint.name, moved: r.moved, returned: r.returned }));
      onClose();
      onClosed(r);
    } catch (err) {
      const m = errorMessage(err);
      setError(m);
      toast.fail(m);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open={open} onClose={onClose} title={t("sprints.closeTitle")} width={440} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="sprint-close-form" type="submit" disabled={busy}>{t("sprints.close")}</Button></>}>
      <form id="sprint-close-form" onSubmit={submit} className="space-y-3">
        <p className="text-ink-muted">
          {sprint?.name}
          {unfinished !== undefined && <span className="ml-2 text-caption text-ink-subtle">{unfinished > 0 ? t("sprints.unfinishedCount", { n: unfinished }) : t("sprints.allDone")}</span>}
        </p>
        <Field label={t("sprints.closeHint")}>
          <Select value={mode} onChange={(e) => setMode(e.target.value as "backlog" | "next")} autoFocus>
            <option value="backlog">{t("sprints.unfinished.backlog")}</option>
            <option value="next" disabled={!candidates.length}>{t("sprints.unfinished.next")}</option>
          </Select>
        </Field>
        {mode === "next" && (
          <Field label={t("sprints.nextSprint")} error={error}>
            <Select value={next} onChange={(e) => setNext(e.target.value)} required>
              {candidates.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
            </Select>
          </Field>
        )}
        {mode === "backlog" && !candidates.length && <p className="text-caption text-ink-subtle">{t("sprints.noNext")}</p>}
        {/* 后果（DESIGN.md §6）：写清几个任务去哪儿、结束后不能再改 */}
        <div data-consequences>
          <p className="eyebrow mb-1.5 text-ink-subtle">{t("consequence.lead")}</p>
          <ul className="space-y-1.5 text-body text-ink">
            {[
              unfinished === undefined ? null : unfinished === 0 ? t("sprints.closeEffect.none") : mode === "next" ? t("sprints.closeEffect.next", { n: unfinished, name: candidates.find((s) => s.id === next)?.name ?? "" }) : t("sprints.closeEffect.backlog", { n: unfinished }),
              t("sprints.closeEffect.frozen"),
            ].filter(Boolean).map((line, i) => (
              <li key={i} className="flex items-start gap-2"><span className="mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full bg-danger" aria-hidden="true" /><span>{line}</span></li>
            ))}
          </ul>
        </div>
        {mode === "backlog" && error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Dialog>
  );
}
