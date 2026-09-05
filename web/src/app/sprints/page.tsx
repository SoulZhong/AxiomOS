"use client";
import Link from "next/link";
import { useState } from "react";
import { api, type Sprint, type SprintStatus } from "@/lib/api";
import { useAction, useHighlight, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { IconEdit, IconPlay, IconPlus, IconSprint } from "@/components/icons";
import { SprintCloseDialog } from "@/components/sprints/SprintCloseDialog";
import { SprintDialog } from "@/components/sprints/SprintDialog";
import { fmtRange, SPRINT_STATUSES, sprintDays, sprintProgress, sprintStatusTitle, sprintStatusTone } from "@/components/sprints/sprintUtils";
import { VelocityPanel } from "@/components/sprints/VelocityPanel";
import { Button, Empty, ErrorBox, ListSkeleton, PageHeader, Panel, ProgressBar, Select, StatusLED, Tag, Tip, cx } from "@/components/ui";

/**
 * 迭代列表：顶部迭代速度，下面按状态分组（进行中 → 规划中 → 已结束）。
 * 每行：名称 + 迭代目标、起止与剩余天数、团队、工作量完成进度（刻度进度条）、动作（开始 / 结束 / 编辑）。
 * 开始与结束需要「管理流程」权限；没有权限时按钮置灰并在气泡里说明。
 */
export default function SprintsPage() {
  const { session } = useSession();
  const [team, setTeam] = useState("");
  const list = useLoad(() => api.sprints.list({ team: team || undefined }), [team]);
  const { busy, run } = useAction();
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<Sprint | null>(null);
  const [closing, setClosing] = useState<Sprint | null>(null);
  const [highlight, mark] = useHighlight();
  const canManage = !!session && ((session.is_owner ?? session.organization.owner_id === session.member.id) || !!session.permissions?.includes("manage_workflows"));
  const teams = session?.teams ?? [];
  const all = list.data ?? [];
  const grouped = SPRINT_STATUSES.map((s) => [s, all.filter((x) => x.status === s)] as const);
  const candidatesFor = (sp: Sprint) => all.filter((x) => x.status === "planning" && x.id !== sp.id && (x.team?.id ?? null) === (sp.team?.id ?? null));

  const start = (sp: Sprint) => run(sp.id, () => api.sprints.start(sp.id), t("sprints.started", { name: sp.name })).then((ok) => ok && list.reload());

  return (
    <div>
      <PageHeader title={t("sprints.title")} description={t("sprints.description")} actions={<Button variant="primary" icon={<IconPlus />} onClick={() => setCreating(true)}>{t("sprints.new")}</Button>} />

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Select value={team} onChange={(e) => setTeam(e.target.value)} className="w-40" aria-label={t("sprint.team")}>
          <option value="">{t("board.allTeams")}</option>
          {teams.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
        </Select>
        <span className="ml-auto text-caption text-ink-subtle">{list.data ? t("sprints.count", { n: all.length }) : ""}</span>
      </div>

      <div className="space-y-4">
        <VelocityPanel index={1} team={team} />
        {list.error && !list.data ? (
          <ErrorBox message={list.error} onRetry={list.reload} />
        ) : !list.data ? (
          <Panel index={2} title={t("sprints.status.active")}><ListSkeleton rows={3} /></Panel>
        ) : all.length === 0 ? (
          <Panel index={2} icon={<IconSprint />} title={t("sprints.title")}>
            <Empty text={t("sprints.empty")} action={<Button variant="primary" icon={<IconPlus />} onClick={() => setCreating(true)}>{t("sprints.new")}</Button>} />
          </Panel>
        ) : (
          grouped.map(([status, items], gi) => (
            <Panel key={status} index={gi + 2} icon={gi === 0 ? <IconSprint /> : undefined} title={<span className="inline-flex items-center gap-2"><StatusLED tone={status === "active" ? "accent" : status === "closed" ? "success" : "neutral"} />{sprintStatusTitle(status)}</span>} telemetry={t("panel.rows", { n: items.length })} padded={false}>
              {items.length === 0 ? (
                <p className="px-4 py-3 text-caption text-ink-subtle">{t("sprints.emptyGroup")}</p>
              ) : (
                items.map((sp) => <SprintRow key={sp.id} sprint={sp} status={status} busy={busy === sp.id} canManage={canManage} highlight={highlight === sp.id} onStart={() => void start(sp)} onClose={() => setClosing(sp)} onEdit={() => setEditing(sp)} />)
              )}
            </Panel>
          ))
        )}
      </div>

      <SprintDialog open={creating} onClose={() => setCreating(false)} teams={teams} onSaved={(s) => { mark(s.id); list.reload(); }} />
      <SprintDialog open={!!editing} onClose={() => setEditing(null)} sprint={editing} teams={teams} onSaved={() => list.reload()} />
      <SprintCloseDialog open={!!closing} onClose={() => setClosing(null)} sprint={closing} candidates={closing ? candidatesFor(closing) : []} onClosed={() => list.reload()} />
    </div>
  );
}

function SprintRow({ sprint: sp, status, busy, canManage, highlight, onStart, onClose, onEdit }: { sprint: Sprint; status: SprintStatus; busy: boolean; canManage: boolean; highlight: boolean; onStart: () => void; onClose: () => void; onEdit: () => void }) {
  const days = sprintDays(sp);
  const pct = sprintProgress(sp);
  const noPerm = canManage ? null : t("sprints.needPermission");
  return (
    <div className={cx("sp-row", highlight && "row-new")}>
      <div className="min-w-0">
        <Link href={`/sprints/${encodeURIComponent(sp.id)}/`} className="sp-name block" title={sp.name}>{sp.name}</Link>
        <div className="sp-goal" title={sp.goal || undefined}>{sp.goal || <span className="text-ink-tertiary">{t("sprint.noGoal")}</span>}</div>
      </div>
      <div className="sp-dates" data-over={days.over ? "" : undefined}>
        {fmtRange(sp.starts_on, sp.ends_on)}
        <b>{days.text}</b>
      </div>
      <div className="sp-team min-w-0 truncate text-caption text-ink-muted">
        {sp.team ? sp.team.title : <span className="text-ink-subtle">{t("sprints.noTeam")}</span>}
        <span className="ml-2 text-ink-subtle">{t("sprints.tasks", { n: sp.task_count })}</span>
      </div>
      <div className="sp-progress">
        <div className="sp-progress-read">
          <span>{t("sprints.pointsDone", { done: sp.points_done, total: sp.points_total })}</span>
          <b>{pct}%</b>
        </div>
        <ProgressBar value={pct} tone={status === "closed" ? "success" : "accent"} />
      </div>
      <div className="flex items-center justify-end gap-1">
        {status === "planning" && (
          <Tip tip={noPerm} placement="bottom"><Button size="sm" icon={<IconPlay />} disabled={busy || !canManage} onClick={onStart}>{t("sprints.start")}</Button></Tip>
        )}
        {status === "active" && (
          <Tip tip={noPerm} placement="bottom"><Button size="sm" disabled={busy || !canManage} onClick={onClose}>{t("sprints.close")}</Button></Tip>
        )}
        {status !== "closed" && <Button size="sm" variant="ghost" icon={<IconEdit />} disabled={busy} onClick={onEdit}>{t("sprints.edit")}</Button>}
        {status === "closed" && <Tag tone={sprintStatusTone.closed}>{sprintStatusTitle("closed")}</Tag>}
      </div>
    </div>
  );
}
