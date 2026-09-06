import { teamIdsOf, type OrgMember, type OrgTeam } from "@/lib/api";

/*
 * 团队树的纯函数（成员与团队页，DESIGN.md §16）。
 * 人数一律在前端按成员表算：树上的数字和右边表格永远一致，改动后立刻更新，不等后端的 member_count。
 * 计入人数的是「正常」与「待激活」的人；已停用的不算。
 */

export interface TeamNode {
  team: OrgTeam;
  depth: number;
  children: TeamNode[];
}

/** 按 parent_id 搭成树；同级按名字排序（中文按拼音）。找不到上级的团队当顶层。 */
export function buildTree(teams: OrgTeam[]): TeamNode[] {
  const ids = new Set(teams.map((x) => x.id));
  const byParent = new Map<string | null, OrgTeam[]>();
  for (const tm of teams) {
    const key = tm.parent_id && ids.has(tm.parent_id) ? tm.parent_id : null;
    byParent.set(key, [...(byParent.get(key) ?? []), tm]);
  }
  const sortTeams = (xs: OrgTeam[]) => [...xs].sort((a, b) => Number(a.active === false) - Number(b.active === false) || a.name.localeCompare(b.name, "zh-Hans-CN"));
  const build = (parent: string | null, depth: number): TeamNode[] => sortTeams(byParent.get(parent) ?? []).map((team) => ({ team, depth, children: build(team.id, depth + 1) }));
  return build(null, 0);
}

export function flattenTree(nodes: TeamNode[]): TeamNode[] {
  return nodes.flatMap((n) => [n, ...flattenTree(n.children)]);
}

/** 这个团队和它下面的所有团队 id */
export function subtreeIds(teams: OrgTeam[], id: string): Set<string> {
  const out = new Set<string>([id]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const x of teams) if (x.parent_id && out.has(x.parent_id) && !out.has(x.id)) { out.add(x.id); grew = true; }
  }
  return out;
}

/** 从顶层到这个团队的路径（面包屑） */
export function pathOf(teams: OrgTeam[], id: string | null): OrgTeam[] {
  const out: OrgTeam[] = [];
  const seen = new Set<string>();
  let cur = teams.find((x) => x.id === id);
  while (cur && !seen.has(cur.id)) {
    seen.add(cur.id);
    out.unshift(cur);
    cur = teams.find((x) => x.id === cur!.parent_id);
  }
  return out;
}

/** 计入人数的人：正常 + 待激活 */
export const countable = (m: OrgMember) => m.active !== false && m.status !== "inactive";

/** 团队（含或不含下级）里的成员；teamId 为 null 表示整个组织 */
export function membersIn(members: OrgMember[], teams: OrgTeam[], teamId: string | null, directOnly: boolean): OrgMember[] {
  if (teamId === null) return members;
  const scope = directOnly ? new Set([teamId]) : subtreeIds(teams, teamId);
  return members.filter((m) => teamIdsOf(m).some((id) => scope.has(id)));
}

/** 树上每个团队的人数（含下级、只算正常 + 待激活） */
export function subtreeCounts(members: OrgMember[], teams: OrgTeam[]): Map<string, number> {
  const out = new Map<string, number>();
  for (const tm of teams) out.set(tm.id, membersIn(members, teams, tm.id, false).filter(countable).length);
  return out;
}
