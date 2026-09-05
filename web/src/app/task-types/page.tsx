"use client";
import { useState } from "react";
import { api, type TaskType } from "@/lib/api";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { describeAssignTo, describeBy, describeRequire, describeStateName, grantTitle, roleTitle, stateLabel } from "@/lib/terms";
import { useSession } from "@/components/AppShell";
import { IconBug, IconChevronRight } from "@/components/icons";
import { isBugType } from "@/lib/states";
import { LABEL_TONE, isDarkLabel } from "@/components/StateBadge";
import { Code, ErrorBox, ListSkeleton, PageHeader, Panel, Table, Tag } from "@/components/ui";

export default function TaskTypesPage() {
  const { session } = useSession();
  const types = useLoad(() => api.taskTypes.list(), []);
  const [current, setCurrent] = useState<string | null>(null);
  const roles = session ? Object.fromEntries(session.roles.map((r) => [r.name, r.title])) : undefined;
  const list = types.data ?? [];
  const def: TaskType | undefined = list.find((x) => x.name === (current ?? list[0]?.name));

  return (
    <div>
      <PageHeader title={t("taskTypes.title")} description={t("taskTypes.description")} />
      {types.loading ? <Panel><ListSkeleton rows={5} /></Panel> : types.error ? <ErrorBox message={types.error} onRetry={types.reload} /> : !def ? null : (
        <div className="grid gap-4 lg:grid-cols-[200px_minmax(0,1fr)]">
          <nav className="flex gap-1 overflow-x-auto lg:flex-col lg:gap-0.5" aria-label={t("taskTypes.title")}>
            {list.map((x) => (
              <button key={x.name} type="button" onClick={() => setCurrent(x.name)} className="vtab shrink-0 whitespace-nowrap" aria-current={def.name === x.name ? "true" : undefined}>
                {isBugType(x.name) && <IconBug size={14} className="mr-1.5 inline-block align-[-2px] text-ink-subtle" />}
                {x.title}
                {x.builtin && <span className="ml-2 text-caption font-normal text-ink-subtle">{t("common.builtin")}</span>}
              </button>
            ))}
          </nav>
          <div className="space-y-4">
            <Panel index={1} title={<span className="inline-flex items-center gap-2">{def.title}<Code>{t("taskTypes.version", { name: def.name, version: def.workflow.version })}</Code></span>}>
              <div className="grid gap-4 md:grid-cols-2">
                <div>
                  <div className="mb-1 text-caption text-ink-muted">{t("taskTypes.participants")}</div>
                  {Object.keys(def.participants).length === 0 ? <p className="text-body text-ink-subtle">{t("taskTypes.noParticipants")}</p> : (
                    <ul className="space-y-1 text-body">
                      {Object.entries(def.participants).map(([slot, p]) => <li key={slot}>{p.title} <span className="text-caption text-ink-subtle">{t("taskTypes.participantMeta", { slot, role: roleTitle(p.role, roles) })}</span></li>)}
                    </ul>
                  )}
                </div>
                <div>
                  <div className="mb-1 text-caption text-ink-muted">{t("taskTypes.instructions")}</div>
                  <p className="whitespace-pre-wrap text-body">{def.agent_instructions || <span className="text-ink-subtle">{t("taskTypes.noInstructions")}</span>}</p>
                </div>
              </div>
            </Panel>

            <Panel index={2} title={t("taskTypes.states")} telemetry={t("panel.rows", { n: Object.keys(def.workflow.states).length })} padded={false}>
              <Table>
                <thead><tr><th>{t("taskTypes.state")}</th><th>{t("taskTypes.label")}</th><th className="num">{t("taskTypes.weight")}</th><th>{t("taskTypes.claimable")}</th><th>{t("common.codeName")}</th></tr></thead>
                <tbody>
                  {Object.values(def.workflow.states).map((s) => (
                    <tr key={s.name}>
                      <td>
                        <span className="inline-flex items-center gap-2">
                          <Tag tone={LABEL_TONE[s.label]} dark={isDarkLabel(s.label)}>{s.title}</Tag>
                          {s.name === def.workflow.initial && <span className="text-caption text-ink-subtle">{t("taskTypes.initial")}</span>}
                        </span>
                      </td>
                      <td className="text-ink-muted">{stateLabel(s.label)}</td>
                      <td className="num">{s.weight ?? <span className="text-ink-subtle">{t("taskTypes.inherit")}</span>}</td>
                      <td>{s.claimable ? t("common.yes") : <span className="text-ink-subtle">—</span>}</td>
                      <td><Code>{s.name}</Code></td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </Panel>

            <Panel index={3} title={t("taskTypes.transitions")} telemetry={t("panel.rows", { n: def.workflow.transitions.length })} padded={false}>
              <Table>
                <thead><tr><th>{t("taskTypes.transition")}</th><th>{t("taskTypes.fromTo")}</th><th>{t("taskTypes.by")}</th><th>{t("taskTypes.requires")}</th><th>{t("taskTypes.assignTo")}</th><th>{t("taskTypes.grant")}</th></tr></thead>
                <tbody>
                  {def.workflow.transitions.map((tr) => {
                    const toState = def.workflow.states[tr.to];
                    return (
                      <tr key={tr.name}>
                        <td>{tr.title}<div><Code className="text-caption">{tr.name}</Code></div></td>
                        <td>
                          <span className="inline-flex flex-wrap items-center gap-1">
                            <span className="text-ink-muted">{tr.from.map((f) => describeStateName(f, def)).join(", ")}</span>
                            <IconChevronRight className="text-ink-subtle" />
                            {toState ? <Tag tone={LABEL_TONE[toState.label]} dark={isDarkLabel(toState.label)}>{toState.title}</Tag> : describeStateName(tr.to, def)}
                          </span>
                        </td>
                        <td>{tr.by.map((b) => describeBy(b, def, roles)).join(" / ")}</td>
                        <td>{tr.requires.length ? tr.requires.map((r) => <div key={r}>{describeRequire(r, session?.artifact_types)}</div>) : <span className="text-ink-subtle">—</span>}</td>
                        <td>{describeAssignTo(tr.assign_to, def)}</td>
                        <td><Tag>{grantTitle(tr.grant ?? "execute")}</Tag></td>
                      </tr>
                    );
                  })}
                </tbody>
              </Table>
            </Panel>
          </div>
        </div>
      )}
    </div>
  );
}
