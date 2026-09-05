"use client";
import { useState, type FormEvent } from "react";
import { api, type Sprint, type Team } from "@/lib/api";
import { addDays, toISODate, today } from "@/lib/format";
import { errorMessage } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useToast } from "@/components/toast";
import { Button, DateInput, Dialog, Field, Input, Select, Textarea } from "@/components/ui";

/** 新建 / 编辑迭代：名称、迭代目标、团队、起止日期。默认两周。 */
export function SprintDialog({ open, onClose, sprint, teams, onSaved }: { open: boolean; onClose: () => void; sprint?: Sprint | null; teams: Team[]; onSaved: (s: Sprint) => void }) {
  const toast = useToast();
  const [name, setName] = useState("");
  const [goal, setGoal] = useState("");
  const [team, setTeam] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastOpen, setLastOpen] = useState(false);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (open) {
      const d0 = today();
      setName(sprint?.name ?? "");
      setGoal(sprint?.goal ?? "");
      setTeam(sprint?.team?.id ?? teams[0]?.id ?? "");
      setStart(sprint?.starts_on ?? toISODate(d0));
      setEnd(sprint?.ends_on ?? toISODate(addDays(d0, 14)));
      setError(null);
    }
  }
  const editing = !!sprint;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const saved = editing
        ? await api.sprints.update(sprint.id, { name, goal, starts_on: start, ends_on: end })
        : await api.sprints.create({ name, goal, team_id: team || null, starts_on: start, ends_on: end });
      toast.ok(editing ? t("sprints.saved", { name: saved.name }) : t("sprints.created", { name: saved.name }));
      onClose();
      onSaved(saved);
    } catch (err) {
      const m = errorMessage(err);
      setError(m);
      toast.fail(m);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open={open} onClose={onClose} title={editing ? t("sprints.form.editTitle") : t("sprints.new")} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="sprint-form" type="submit" disabled={busy}>{editing ? t("common.save") : t("common.create")}</Button></>}>
      <form id="sprint-form" onSubmit={submit} className="space-y-3">
        <Field label={t("sprints.form.name")}><Input value={name} onChange={(e) => setName(e.target.value)} required autoFocus placeholder={t("sprints.form.namePlaceholder")} /></Field>
        <Field label={t("sprints.form.goal")} hint={t("sprints.form.goalHint")}><Textarea value={goal} onChange={(e) => setGoal(e.target.value)} /></Field>
        {!editing && (
          <Field label={t("sprints.form.team")}>
            <Select value={team} onChange={(e) => setTeam(e.target.value)}>
              <option value="">{t("sprints.noTeam")}</option>
              {teams.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
            </Select>
          </Field>
        )}
        <div className="grid grid-cols-2 gap-3">
          <Field label={t("sprints.form.start")}><DateInput value={start} onChange={(e) => setStart(e.target.value)} required /></Field>
          <Field label={t("sprints.form.end")} error={error}><DateInput value={end} onChange={(e) => setEnd(e.target.value)} required min={start || undefined} /></Field>
        </div>
      </form>
    </Dialog>
  );
}
