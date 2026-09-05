"use client";
import { useState, type FormEvent } from "react";
import { SCOPE_ALL } from "@/lib/scope";
import { useScopeState } from "@/lib/useScope";
import { api, type Goal } from "@/lib/api";
import { errorMessage, useExecutors } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "./AppShell";
import { useToast } from "./toast";
import { Button, DateInput, Drawer, Field, FormSection, Input, Select, Textarea } from "./ui";

export function flattenGoals(goals: Goal[], depth = 0): Array<{ goal: Goal; depth: number }> {
  return goals.flatMap((g) => [{ goal: g, depth }, ...flattenGoals(g.children ?? [], depth + 1)]);
}

/** 新建目标：右侧抽屉。 */
export function GoalDrawer({ open, parentId, goals, onClose, onCreated }: { open: boolean; parentId: string | null; goals: Array<{ goal: Goal; depth: number }>; onClose: () => void; onCreated: (goal: Goal) => void }) {
  const { session } = useSession();
  const ex = useExecutors();
  const toast = useToast();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [parent, setParent] = useState<string>("");
  const [owner, setOwner] = useState<string>("");
  // 归属团队：默认跟着当前正看着的范围（不是"全公司"时），有上级目标时跟上级；否则新目标会落成"未分组"
  const { scope } = useScopeState();
  const [team, setTeam] = useState<string>("");
  const [budget, setBudget] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastOpen, setLastOpen] = useState(false);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (open) {
      setTitle(""); setDescription(""); setParent(parentId ?? ""); setOwner(session?.member.id ?? ""); setBudget(""); setStart(""); setEnd(""); setError(null);
      const parentTeam = parentId ? goals.find((x) => x.goal.id === parentId)?.goal.team_id ?? "" : "";
      setTeam(parentTeam || (scope !== SCOPE_ALL ? scope : ""));
    }
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const g = await api.goals.create({ title, description, parent_id: parent || null, owner_id: owner || undefined, budget: budget ? Number(budget) : null, planned_start: start || null, planned_end: end || null });
      toast.ok(t("toast.created", { name: g.title }));
      onClose();
      onCreated(g);
    } catch (err) {
      const m = errorMessage(err);
      setError(m);
      toast.fail(m);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Drawer open={open} onClose={onClose} title={t("goalDialog.title")} description={t("goalDialog.hint")} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="goal-form" type="submit" disabled={busy}>{t("common.create")}</Button></>}>
      <form id="goal-form" onSubmit={submit} className="space-y-4">
        <FormSection title={t("form.basic")}>
          <Field label={t("goalDialog.name")}><Input value={title} onChange={(e) => setTitle(e.target.value)} required autoFocus /></Field>
          <Field label={t("goalDialog.description")}><Textarea value={description} onChange={(e) => setDescription(e.target.value)} className="min-h-16" /></Field>
          <Field label={t("goalDialog.parent")}>
            <Select value={parent} onChange={(e) => setParent(e.target.value)}>
              <option value="">{t("goalDialog.noParent")}</option>
              {goals.map(({ goal, depth }) => <option key={goal.id} value={goal.id}>{"　".repeat(depth)}{goal.title}</option>)}
            </Select>
          </Field>
        </FormSection>
        <FormSection title={t("form.schedule")}>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("goalDialog.start")}><DateInput value={start} onChange={(e) => setStart(e.target.value)} /></Field>
            <Field label={t("goalDialog.end")}><DateInput value={end} onChange={(e) => setEnd(e.target.value)} /></Field>
            <Field label={t("goalDialog.budget")} hint={t("goalDialog.budgetHint")}><Input type="number" min={0} step="0.01" value={budget} onChange={(e) => setBudget(e.target.value)} /></Field>
          </div>
        </FormSection>
        <FormSection title={t("form.people")}>
          <Field label={t("goalDialog.team")} hint={t("goalDialog.teamHint")}>
            <Select value={team} onChange={(e) => setTeam(e.target.value)}>
              <option value="">{t("goalDialog.noTeam")}</option>
              {(session?.teams ?? []).map((tm) => <option key={tm.id} value={tm.id}>{tm.name}</option>)}
            </Select>
          </Field>
          <Field label={t("goalDialog.owner")}>
            <Select value={owner} onChange={(e) => setOwner(e.target.value)}>
              {(ex.data?.members ?? []).map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
            </Select>
          </Field>
        </FormSection>
        {error && <p className="text-caption text-danger" role="alert">{error}</p>}
      </form>
    </Drawer>
  );
}
