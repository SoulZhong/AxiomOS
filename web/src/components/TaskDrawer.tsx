"use client";
import { useState, type FormEvent } from "react";
import { api, type Goal, type ISODate, type Priority, type Task, type TaskType } from "@/lib/api";
import { errorMessage, useExecutors } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { PRIORITIES, priorityTitle } from "@/lib/terms";
import { useToast } from "./toast";
import { Button, DateInput, Drawer, Field, FormSection, Input, Select, Textarea } from "./ui";

export interface TaskDrawerDefaults {
  goal_id?: string | null;
  type?: string;
  planned_start?: ISODate | null;
  planned_end?: ISODate | null;
  assignee_id?: string | null;
}

/** 新建任务：右侧抽屉，不打断当前页面；创建后把新任务交给调用方原地插入并高亮。 */
export function TaskDrawer({ open, onClose, onCreated, goals, types, defaults }: { open: boolean; onClose: () => void; onCreated: (task: Task) => void; goals: Array<{ goal: Goal; depth: number }>; types: TaskType[]; defaults?: TaskDrawerDefaults }) {
  const ex = useExecutors();
  const toast = useToast();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [type, setType] = useState("generic");
  const [goal, setGoal] = useState("");
  const [assignee, setAssignee] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [estimate, setEstimate] = useState("");
  const [priority, setPriority] = useState<Priority>("normal");
  const [participants, setParticipants] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastOpen, setLastOpen] = useState(false);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (open) {
      setTitle(""); setDescription(""); setType(defaults?.type ?? "generic"); setGoal(defaults?.goal_id ?? ""); setAssignee(defaults?.assignee_id ?? "");
      setStart(defaults?.planned_start ?? ""); setEnd(defaults?.planned_end ?? ""); setEstimate(""); setPriority("normal"); setParticipants({}); setError(null);
    }
  }
  const def = types.find((x) => x.name === type);
  const executorOptions = (ex.data?.executors ?? []).map((x) => <option key={x.id} value={x.id}>{x.name}{x.kind === "agent" ? t("common.agentSuffix") : ""}</option>);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const task = await api.tasks.create({
        title, description, type, goal_id: goal || null, assignee_id: assignee || null,
        planned_start: start || null, planned_end: end || null, estimate: estimate ? Number(estimate) : null, priority,
        participants: Object.fromEntries(Object.entries(participants).filter(([, v]) => v)),
      });
      toast.ok(t("toast.created", { name: task.title }));
      onClose();
      onCreated(task);
    } catch (err) {
      const m = errorMessage(err);
      setError(m);
      toast.fail(m);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Drawer open={open} onClose={onClose} title={t("taskDialog.title")} description={t("taskDialog.hint")} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="task-form" type="submit" disabled={busy}>{t("common.create")}</Button></>}>
      <form id="task-form" onSubmit={submit} className="space-y-4">
        <FormSection title={t("form.basic")}>
          <Field label={t("taskDialog.name")}><Input value={title} onChange={(e) => setTitle(e.target.value)} required autoFocus /></Field>
          <Field label={t("taskDialog.description")}><Textarea value={description} onChange={(e) => setDescription(e.target.value)} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("taskDialog.type")}>
              <Select value={type} onChange={(e) => { setType(e.target.value); setParticipants({}); }}>
                {types.map((x) => <option key={x.name} value={x.name}>{x.title}</option>)}
              </Select>
            </Field>
            <Field label={t("taskDialog.goal")}>
              <Select value={goal} onChange={(e) => setGoal(e.target.value)}>
                <option value="">{t("taskDialog.noGoal")}</option>
                {goals.map(({ goal: g, depth }) => <option key={g.id} value={g.id}>{"　".repeat(depth)}{g.title}</option>)}
              </Select>
            </Field>
          </div>
        </FormSection>
        <FormSection title={t("form.schedule")}>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("taskDialog.start")}><DateInput value={start} onChange={(e) => setStart(e.target.value)} /></Field>
            <Field label={t("taskDialog.end")}><DateInput value={end} onChange={(e) => setEnd(e.target.value)} /></Field>
            <Field label={t("taskDialog.priority")}>
              <Select value={priority} onChange={(e) => setPriority(e.target.value as Priority)}>
                {PRIORITIES.map((p) => <option key={p} value={p}>{priorityTitle(p)}</option>)}
              </Select>
            </Field>
            <Field label={t("taskDialog.estimate")}><Input type="number" min={0} step="0.5" value={estimate} onChange={(e) => setEstimate(e.target.value)} /></Field>
          </div>
        </FormSection>
        <FormSection title={t("form.people")}>
          <Field label={t("taskDialog.assignee")} hint={t("taskDialog.assigneeHint")}>
            <Select value={assignee} onChange={(e) => setAssignee(e.target.value)}>
              <option value="">{t("taskDialog.unassigned")}</option>
              {executorOptions}
            </Select>
          </Field>
          {def && Object.keys(def.participants).length > 0 && (
            <div className="grid grid-cols-2 gap-3">
              {Object.entries(def.participants).map(([slot, p]) => (
                <Field key={slot} label={p.title}>
                  <Select value={participants[slot] ?? ""} onChange={(e) => setParticipants({ ...participants, [slot]: e.target.value })}>
                    <option value="">{t("taskDialog.vacant")}</option>
                    {executorOptions}
                  </Select>
                </Field>
              ))}
            </div>
          )}
        </FormSection>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
