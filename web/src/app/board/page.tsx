"use client";
import { useMemo, useState } from "react";
import { api, type BoardLane } from "@/lib/api";
import { useExecutors, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { usePersisted } from "@/lib/usePersisted";
import { useSession } from "@/components/AppShell";
import { Board, type BoardDensity } from "@/components/board/Board";
import { flattenGoals } from "@/components/GoalDrawer";
import { EnergyLine, Segmented, Select, Tip } from "@/components/ui";

const LANES: BoardLane[] = ["none", "goal", "assignee"];
const DENSITIES: BoardDensity[] = ["comfortable", "compact"];

/**
 * 看板页：工具栏一行（任务类型 / 目标 / 团队 / 执行者 / 迭代 / 分道 / 密度），下面把空间全给看板。
 * 任务类型与密度记在 localStorage；其余筛选是会话内的。
 */
export default function BoardPage() {
  const { session } = useSession();
  const [type, setType] = usePersisted<string>("axiomos.board.type", "");
  const [density, setDensity] = usePersisted<BoardDensity>("axiomos.board.density", "comfortable", (v) => (DENSITIES.includes(v as BoardDensity) ? (v as BoardDensity) : undefined));
  const [goal, setGoal] = useState("");
  const [team, setTeam] = useState("");
  const [assignee, setAssignee] = useState("");
  const [sprint, setSprint] = useState("");
  const [lane, setLane] = useState<BoardLane>("none");

  const types = useLoad(() => api.taskTypes.list(), []);
  const goals = useLoad(() => api.goals.list(), []);
  const sprints = useLoad(() => api.sprints.list().catch(() => []), []);
  const ex = useExecutors();
  const flat = useMemo(() => flattenGoals(goals.data ?? []), [goals.data]);
  const openSprints = (sprints.data ?? []).filter((s) => s.status !== "closed");
  const query = useMemo(() => ({ type: type || undefined, goal: goal || undefined, team: team || undefined, assignee: assignee || undefined, sprint: sprint || undefined, lane }), [type, goal, team, assignee, sprint, lane]);

  return (
    <div className="flex min-h-0 flex-col">
      <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-2">
        <div className="mr-1 flex items-center gap-2">
          <h1 className="text-headline text-ink">{t("board.title")}</h1>
          <Tip tip={t("board.description") + "\n" + t("board.dragHint")} placement="bottom">
            <span className="eyebrow inline-flex h-5 w-5 cursor-help items-center justify-center rounded-xs border border-hairline-strong text-ink-subtle" aria-label={t("board.hint")}>?</span>
          </Tip>
        </div>
        <Select value={type} onChange={(e) => setType(e.target.value)} className="w-36" aria-label={t("board.typeLabel")}>
          <option value="">{t("tasks.allTypes")}</option>
          {(types.data ?? []).map((x) => <option key={x.name} value={x.name}>{x.title}</option>)}
        </Select>
        <Select value={goal} onChange={(e) => setGoal(e.target.value)} className="w-44" aria-label={t("taskTable.goal")}>
          <option value="">{t("tasks.allGoals")}</option>
          {flat.map(({ goal: g, depth }) => <option key={g.id} value={g.id}>{"　".repeat(depth)}{g.title}</option>)}
        </Select>
        <Select value={team} onChange={(e) => setTeam(e.target.value)} className="w-36" aria-label={t("sprint.team")}>
          <option value="">{t("board.allTeams")}</option>
          {(session?.teams ?? []).map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
        </Select>
        <Select value={assignee} onChange={(e) => setAssignee(e.target.value)} className="w-40" aria-label={t("taskTable.assignee")}>
          <option value="">{t("tasks.allAssignees")}</option>
          <option value="me">{t("tasks.me")}</option>
          {(ex.data?.executors ?? []).map((x) => <option key={x.id} value={x.id}>{x.name}{x.kind === "agent" ? t("common.agentSuffix") : ""}</option>)}
        </Select>
        <Select value={sprint} onChange={(e) => setSprint(e.target.value)} className="w-36" aria-label={t("task.sprint")}>
          <option value="">{t("board.allSprints")}</option>
          {openSprints.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
        </Select>
        <span className="ml-auto flex flex-wrap items-center gap-3">
          <Segmented size="sm" value={lane} onChange={setLane} label={t("board.lane")} aria-label={t("board.lane")} options={LANES.map((l) => [l, t(`board.lane.${l}`)])} />
          <Segmented size="sm" value={density} onChange={setDensity} aria-label={t("board.density")} options={DENSITIES.map((d) => [d, t(`board.density.${d}`)])} />
        </span>
      </div>
      <EnergyLine className="mb-3" />
      <Board query={query} density={density} />
    </div>
  );
}
