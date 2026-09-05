import type { Agent, Goal, Member, Session, StateLabel } from "@/lib/api";
import type { Key } from "@/lib/i18n";

/*
 * 「任务」入口的五个页签与一条共用的筛选栏（DESIGN.md §10）。
 * 页签与筛选都写在地址栏查询串里（?view=&goal=&team=&assignee=&type=&sprint=&state=），切页签不丢筛选，可分享。
 *
 * 筛选 → 各页签查询参数的对应（后端接口见 docs/api.md）：
 *   列表   GET /tasks      goal / assignee / type / sprint / state 直接传；team 在前端按负责人所属团队过滤（接口没有 team）
 *   看板   GET /board      type / goal / team / assignee / sprint 直接传；state 不适用（看板本身按状态分列）
 *   甘特图 GET /gantt      只有 group/from/to；goal / team / assignee / type / state 在前端按行内任务过滤；sprint 不适用（甘特数据里没有迭代）
 *   迭代   GET /sprints    team 直接传；其余不适用（迭代列表只按团队筛）
 *   待领取 GET /backlog    无参数；goal / type / sprint / state 在前端过滤；assignee / team 不适用（待领取的任务没有负责人）
 * 不适用的控件禁用并给出理由（气泡），而不是悄悄忽略。
 */

export type TaskView = "list" | "board" | "gantt" | "sprints" | "backlog";
export const TASK_VIEWS: TaskView[] = ["list", "board", "gantt", "sprints", "backlog"];
export const isTaskView = (v: unknown): v is TaskView => typeof v === "string" && (TASK_VIEWS as string[]).includes(v);
/** 个人记住的页签（切过一次后按人记，不再按角色） */
export const TASK_VIEW_KEY = "axiomos.tasks.view";

export interface TaskFilters {
  goal: string;
  team: string;
  assignee: string;
  type: string;
  sprint: string;
  state: string;
}
export type FilterKey = keyof TaskFilters;
export const FILTER_KEYS: FilterKey[] = ["goal", "team", "assignee", "type", "sprint", "state"];
export const EMPTY_FILTERS: TaskFilters = { goal: "", team: "", assignee: "", type: "", sprint: "", state: "" };
export const hasFilters = (f: TaskFilters) => FILTER_KEYS.some((k) => !!f[k]);

/** 某个页签吃不下某个筛选时的理由（字典键）；null 表示支持。 */
export function filterBlocked(view: TaskView, key: FilterKey): Key | null {
  if (view === "board" && key === "state") return "tasks.filter.na.boardState";
  if (view === "backlog" && (key === "assignee" || key === "team")) return "tasks.filter.na.backlogAssignee";
  if (view === "sprints" && key !== "team") return "tasks.filter.na.sprintsOnlyTeam";
  if (view === "gantt" && key === "sprint") return "tasks.filter.na.ganttSprint";
  return null;
}

/** 目标子树：这个目标连同它下面所有目标的 id（筛选某个目标 = 看它整棵子树的任务，与后端口径一致） */
export function goalSubtree(goalId: string, flat: Array<{ goal: Goal }>): Set<string> {
  const byParent = new Map<string | null, Goal[]>();
  for (const { goal } of flat) {
    const list = byParent.get(goal.parent_id ?? null) ?? [];
    list.push(goal);
    byParent.set(goal.parent_id ?? null, list);
  }
  const out = new Set<string>([goalId]);
  const stack = [goalId];
  while (stack.length) {
    for (const c of byParent.get(stack.pop()!) ?? []) {
      if (!out.has(c.id)) {
        out.add(c.id);
        stack.push(c.id);
      }
    }
  }
  return out;
}

/** 执行者 → 团队：成员按 team_id，Agent 按所有者所属团队（成本归口的口径）；没有团队为 null */
export function teamOfExecutor(id: string, members: Member[], agents: Agent[]): string | null {
  const m = members.find((x) => x.id === id);
  if (m) return m.team_id;
  const a = agents.find((x) => x.id === id);
  if (a) return members.find((x) => x.id === a.owner.id)?.team_id ?? null;
  return null;
}

/** "我（含我的 Agent）"：我的成员 id + 我拥有的 Agent id */
export function principals(session: Session | null, agents: Agent[]): Set<string> {
  const out = new Set<string>();
  if (!session) return out;
  out.add(session.member.id);
  for (const a of agents) if (a.owner.id === session.member.id) out.add(a.id);
  return out;
}

/** 前端过滤用的任务摘要（列表 / 甘特图 / 待领取的任务对象形状不同，先抹平） */
export interface TaskShape {
  goalId: string | null;
  assigneeId: string | null;
  type: string;
  label: StateLabel;
  sprintId: string | null;
}
export interface MatchCtx {
  subtree: Set<string> | null;
  me: Set<string>;
  teamOf: (executorId: string) => string | null;
}
/** 按筛选栏过滤一条任务；skip 里的键由接口已经处理过、这里不再重复 */
export function matchTask(x: TaskShape, f: TaskFilters, ctx: MatchCtx, skip: FilterKey[] = []): boolean {
  const on = (k: FilterKey) => !!f[k] && !skip.includes(k);
  if (on("goal") && !(x.goalId && ctx.subtree?.has(x.goalId))) return false;
  if (on("type") && x.type !== f.type) return false;
  if (on("state") && x.label !== f.state) return false;
  if (on("sprint") && x.sprintId !== f.sprint) return false;
  if (on("assignee")) {
    if (!x.assigneeId) return false;
    if (f.assignee === "me" ? !ctx.me.has(x.assigneeId) : x.assigneeId !== f.assignee) return false;
  }
  if (on("team") && (!x.assigneeId || ctx.teamOf(x.assigneeId) !== f.team)) return false;
  return true;
}
