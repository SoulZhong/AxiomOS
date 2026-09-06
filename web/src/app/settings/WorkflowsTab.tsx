"use client";
import { useState } from "react";
import { api, type TaskType } from "@/lib/api";
import { useLoad } from "@/lib/hooks";
import { t, type Key } from "@/lib/i18n";
import { isBugType } from "@/lib/states";
import { describeAssignTo, describeBy, describeRequire, describeStateName, grantTitle, roleTitle, stateLabel } from "@/lib/terms";
import { useSession } from "@/components/AppShell";
import { IconBug, IconChevronRight } from "@/components/icons";
import { LABEL_TONE, isDarkLabel } from "@/components/StateBadge";
import { Code, ErrorBox, ListSkeleton, Panel, Table, Tag } from "@/components/ui";

/** 外部事件（ADR 0020）的界面名：六种写死在这里，因为流程页对每个成员只读可见，不该为了一个标签去要「组织设置」权限。 */
const CODE_EVENTS = ["pr_opened", "pr_ready", "pr_merged", "pr_closed", "ci_passed", "ci_failed"];
const eventTitle = (key: string) => (CODE_EVENTS.includes(key) ? t(`code.event.${key}` as Key) : key);

/** 组织设置 · 流程（原独立入口「流程」，DESIGN.md §10 并入）：每种任务类型走哪套流程——状态、每一步谁能走、走之前要满足什么、走完后负责人换成谁。 */
/** standalone：没有组织设置入口的成员单独看这一页时，说明句已在页头，这里不再重复 */
export function WorkflowsTab({ standalone = false }: { standalone?: boolean } = {}) {
  const { session } = useSession();
  const types = useLoad(() => api.taskTypes.list(), []);
  const [current, setCurrent] = useState<string | null>(null);
  const roles = session ? Object.fromEntries(session.roles.map((r) => [r.name, r.title])) : undefined;
  const list = types.data ?? [];
  const def: TaskType | undefined = list.find((x) => x.name === (current ?? list[0]?.name));
  // 只读提示：没有「管理流程」权限的人也能看这里（回归修复），但改不了
  const canEdit = !!session && ((session.is_owner ?? session.organization.owner_id === session.member.id) || !!session.permissions?.includes("manage_workflows"));

  return (
    <div>
      <p className="mb-4 max-w-[768px] text-body text-ink-subtle">{!standalone && t("taskTypes.description")}{!canEdit && <span className={standalone ? undefined : "ml-1 text-ink-tertiary"}>{t("settings.workflowsReadOnly")}</span>}</p>
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
                        <td>
                          {tr.title}
                          <div><Code className="text-caption">{tr.name}</Code></div>
                          {/* 由外部事件触发的迁移（ADR 0020）：只读地说清"什么时候会自动走" */}
                          {tr.triggered_by && <div className="mt-1"><Tag tone="accent">{t("taskTypes.triggeredBy", { event: eventTitle(tr.triggered_by.event) })}</Tag></div>}
                        </td>
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
