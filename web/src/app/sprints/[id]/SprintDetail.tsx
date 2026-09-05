"use client";
import Link from "next/link";
import { useState } from "react";
import { api, type Sprint } from "@/lib/api";
import { useAction, useLoad, useRouteId } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { Board } from "@/components/board/Board";
import { IconEdit, IconPlay, IconSprint, IconTask } from "@/components/icons";
import { TaskTable } from "@/components/TaskTable";
import { Burndown } from "@/components/sprints/Burndown";
import { SprintCloseDialog } from "@/components/sprints/SprintCloseDialog";
import { SprintDialog } from "@/components/sprints/SprintDialog";
import { SprintPlanning } from "@/components/sprints/SprintPlanning";
import { fmtRange, sprintDays, sprintProgress, sprintStatusTitle, sprintStatusTone } from "@/components/sprints/sprintUtils";
import { Button, DetailSkeleton, EnergyLine, ErrorBox, Panel, ProgressBar, Segmented, Select, StatusLED, Tag, Tip, cx } from "@/components/ui";

type Tab = "burndown" | "backlog" | "board";
const TABS: Tab[] = ["burndown", "backlog", "board"];

/**
 * 迭代详情：页头（名称、状态、团队、迭代目标、起止与剩余天数、工作量进度）+ 三个视图：燃尽图 / 迭代待办 / 看板（带迭代筛选的看板组件）。
 */
export function SprintDetail() {
  const id = useRouteId();
  const { session } = useSession();
  const sp = useLoad(() => (id ? api.sprints.get(id) : Promise.reject(new Error(t("sprint.missing")))), [id]);
  const siblings = useLoad(() => api.sprints.list({ status: "planning" }).catch(() => [] as Sprint[]), [id]);
  const types = useLoad(() => api.taskTypes.list().catch(() => []), []);
  const { busy, run } = useAction();
  const [tab, setTab] = useState<Tab>("burndown");
  const [boardType, setBoardType] = useState("");
  const [editing, setEditing] = useState(false);
  const [closing, setClosing] = useState(false);
  const canManage = !!session && ((session.is_owner ?? session.organization.owner_id === session.member.id) || !!session.permissions?.includes("manage_workflows"));

  if (!id || (sp.loading && !sp.data)) return <DetailSkeleton />;
  if (sp.error || !sp.data) return <ErrorBox message={sp.error ?? t("sprint.notFound")} onRetry={sp.reload} />;
  const x = sp.data;
  const days = sprintDays(x);
  const pct = sprintProgress(x);
  const unfinished = x.tasks.filter((tk) => tk.state.label !== "terminal_success" && tk.state.label !== "terminal_failure").length;
  const candidates = (siblings.data ?? []).filter((s) => s.id !== x.id && (s.team?.id ?? null) === (x.team?.id ?? null));
  const noPerm = canManage ? null : t("sprints.needPermission");
  const start = () => run(x.id, () => api.sprints.start(x.id), t("sprints.started", { name: x.name })).then((ok) => ok && sp.reload());

  return (
    <div>
      <div className="mb-1 flex flex-wrap items-center gap-1 text-caption text-ink-subtle">
        <Link href="/sprints/" className="hover:text-accent-hover">{t("sprint.breadcrumb")}</Link>
        <span>/</span>
        <span className="telemetry">{x.id}</span>
      </div>
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <h1 className="text-headline text-ink">{x.name}</h1>
            <span className="inline-flex items-center gap-1.5">
              <StatusLED tone={x.status === "active" ? "accent" : x.status === "closed" ? "success" : "neutral"} />
              <Tag tone={sprintStatusTone[x.status]}>{sprintStatusTitle(x.status)}</Tag>
            </span>
            {x.team && <Tag>{x.team.title}</Tag>}
          </div>
          <EnergyLine className="mt-2" />
          <p className={cx("mt-2 max-w-[768px] text-body", x.goal ? "text-ink-muted" : "text-ink-subtle")}>{x.goal || t("sprint.noGoal")}</p>
          <div className="sp-head-meta">
            <span className="telemetry">{fmtRange(x.starts_on, x.ends_on)}</span>
            <span className={cx(days.over && "text-danger")}>{days.text}</span>
            <span>{t("sprints.tasks", { n: x.task_count })}</span>
            <span className="inline-flex items-center gap-2">
              {t("sprints.pointsDone", { done: x.points_done, total: x.points_total })}
              <ProgressBar value={pct} tone={x.status === "closed" ? "success" : "accent"} className="w-28" />
              <span className="telemetry">{pct}%</span>
            </span>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {x.status === "planning" && <Tip tip={noPerm} placement="bottom"><Button variant="primary" icon={<IconPlay />} disabled={busy === x.id || !canManage} onClick={() => void start()}>{t("sprints.start")}</Button></Tip>}
          {x.status === "active" && <Tip tip={noPerm} placement="bottom"><Button variant="primary" disabled={busy === x.id || !canManage} onClick={() => setClosing(true)}>{t("sprints.close")}</Button></Tip>}
          {x.status !== "closed" && <Button icon={<IconEdit />} onClick={() => setEditing(true)}>{t("sprints.edit")}</Button>}
        </div>
      </div>

      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Segmented value={tab} onChange={setTab} options={TABS.map((k) => [k, t(`sprint.tab.${k}`)])} aria-label={t("sprint.tab.burndown")} />
        {tab === "board" && (
          <Select value={boardType} onChange={(e) => setBoardType(e.target.value)} className="w-36" aria-label={t("board.typeLabel")}>
            <option value="">{t("tasks.allTypes")}</option>
            {(types.data ?? []).map((tt) => <option key={tt.name} value={tt.name}>{tt.title}</option>)}
          </Select>
        )}
      </div>

      {/* 燃尽图与本迭代任务并排：宽屏时左图右表，窄屏堆叠，避免整页留白 */}
      {tab === "burndown" && (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <Panel index={1} icon={<IconSprint />} title={t("sprint.tab.burndown")} telemetry={t(`sprint.unit.${x.burndown.unit}`)}>
            {x.status === "planning" || x.burndown.actual.length === 0 ? <p className="py-6 text-center text-body text-ink-subtle">{t("sprint.burndown.empty")}</p> : <Burndown data={x.burndown} />}
          </Panel>
          <Panel index={2} icon={<IconTask />} title={t("sprint.tasksPanel")} telemetry={t("sprints.tasks", { n: x.task_count })}>
            <div className="overflow-x-auto"><TaskTable tasks={x.tasks} currency={session?.organization.currency} compact showGoal={false} emptyText={t("sprint.noTasks")} onChanged={sp.reload} /></div>
          </Panel>
        </div>
      )}
      {tab === "backlog" && <SprintPlanning sprint={x} onChanged={sp.reload} />}
      {tab === "board" && <Board query={{ sprint: x.id, type: boardType || undefined, lane: "none" }} density="comfortable" />}

      <SprintDialog open={editing} onClose={() => setEditing(false)} sprint={x} teams={session?.teams ?? []} onSaved={() => sp.reload()} />
      <SprintCloseDialog open={closing} onClose={() => setClosing(false)} sprint={x} candidates={candidates} unfinished={unfinished} onClosed={() => sp.reload()} />
    </div>
  );
}
