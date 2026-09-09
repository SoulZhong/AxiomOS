"use client";
import { useMemo, useRef, useState, type KeyboardEvent } from "react";
import { isSynced, type OrgMember, type OrgTeam } from "@/lib/api";
import { t } from "@/lib/i18n";
import { IconBoundary, IconChevronRight, IconLink, IconOrg, IconPlus, IconSearch, IconTeam } from "@/components/icons";
import { Avatar, Button, Menu, Tag, Tip, cx, type MenuItem } from "@/components/ui";
import { buildTree, type TeamNode } from "./tree";

/*
 * 左栏团队树（DESIGN.md §16）：顶部搜索（找人 / 找团队），根节点是组织名，每个团队一行（图标、名称、人数、边界标记、同步标记、⋮ 菜单），
 * 底部固定「＋ 新建团队」。重命名与新建都在树上就地输入。键盘：↑↓ 在行间移动，→ 展开 / 进入下级，← 收起 / 回上级，Enter 选中。
 */

export interface TreeActions {
  createTeam: (parentId: string | null, name: string) => Promise<boolean>;
  renameTeam: (team: OrgTeam, name: string) => Promise<boolean>;
  editTeam: (team: OrgTeam) => void;
  moveTeam: (team: OrgTeam) => void;
  toggleBoundary: (team: OrgTeam) => void;
  /** 同步来的团队只能停用（下次同步还会对上）；手工建的直接删除 */
  deactivateTeam: (team: OrgTeam) => void;
  reactivateTeam: (team: OrgTeam) => void;
  deleteTeam: (team: OrgTeam) => void;
}

export function TeamTree({ orgName, teams, members, counts, total, selected, onSelect, expanded, onToggle, actions, className }: {
  orgName: string;
  teams: OrgTeam[];
  members: OrgMember[];
  /** 每个团队含下级的人数 */
  counts: Map<string, number>;
  /** 全组织的人数 */
  total: number;
  selected: string | null;
  /** 选中团队；focusMember 是搜索命中的成员 id（右栏定位并高亮） */
  onSelect: (id: string | null, focusMember?: string) => void;
  expanded: Record<string, boolean>;
  onToggle: (id: string, open: boolean) => void;
  actions: TreeActions;
  className?: string;
}) {
  const [query, setQuery] = useState("");
  const [renaming, setRenaming] = useState<string | null>(null);
  const [creating, setCreating] = useState<{ parent: string | null } | null>(null);
  // 人数与 ⋮ 共用行尾同一格：平时是人数，悬停 / 聚焦 / 菜单打开时换成 ⋮
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const slot = (id: string, count: number, items: MenuItem[], label: string, selectedRow: boolean) => (
    <span className="ml-auto inline-flex h-7 w-7 shrink-0 items-center justify-end">
      <span className={cx("pr-1 text-caption tabular-nums group-hover:hidden group-focus-within:hidden group-data-[menu-open]:hidden", selectedRow ? "text-accent-hover" : "text-ink-subtle")}>{count}</span>
      <span className="hidden group-hover:inline-flex group-focus-within:inline-flex group-data-[menu-open]:inline-flex">
        <Menu items={items} label={label} onOpenChange={(o) => setMenuFor(o ? id : null)} />
      </span>
    </span>
  );
  const treeRef = useRef<HTMLUListElement>(null);
  const nodes = useMemo(() => buildTree(teams), [teams]);
  const isOpen = (n: TeamNode) => expanded[n.team.id] ?? n.depth < 1;

  // 搜索：按姓名 / 邮箱找人，按名字找团队
  const q = query.trim().toLowerCase();
  const hitMembers = q ? members.filter((m) => m.name.toLowerCase().includes(q) || m.email.toLowerCase().includes(q)).slice(0, 8) : [];
  const hitTeams = q ? teams.filter((x) => x.name.toLowerCase().includes(q)).slice(0, 6) : [];
  const teamName = (id: string | null) => teams.find((x) => x.id === id)?.name ?? null;

  const startCreate = (parent: string | null) => {
    if (parent) onToggle(parent, true);
    setRenaming(null);
    setCreating({ parent });
  };

  // 键盘导航：只在可见的行之间走
  const rows = () => Array.from(treeRef.current?.querySelectorAll<HTMLElement>("[data-tree-row]") ?? []);
  const onKey = (e: KeyboardEvent<HTMLUListElement>) => {
    const target = e.target as HTMLElement;
    if (target.tagName === "INPUT") return;
    const row = target.closest<HTMLElement>("[data-tree-row]");
    if (!row) return;
    const all = rows();
    const i = all.indexOf(row);
    const id = row.dataset.team ?? null;
    const node = id ? flatNodes(nodes).find((n) => n.team.id === id) : null;
    const focusAt = (j: number) => all[Math.max(0, Math.min(all.length - 1, j))]?.focus();
    switch (e.key) {
      case "ArrowDown": e.preventDefault(); focusAt(i + 1); break;
      case "ArrowUp": e.preventDefault(); focusAt(i - 1); break;
      case "Home": e.preventDefault(); focusAt(0); break;
      case "End": e.preventDefault(); focusAt(all.length - 1); break;
      case "ArrowRight":
        e.preventDefault();
        if (node && node.children.length) { if (!isOpen(node)) onToggle(node.team.id, true); else focusAt(i + 1); }
        break;
      case "ArrowLeft": {
        e.preventDefault();
        if (node && node.children.length && isOpen(node)) onToggle(node.team.id, false);
        else if (node?.team.parent_id) all.find((el) => el.dataset.team === node.team.parent_id)?.focus();
        else if (node) all[0]?.focus();
        break;
      }
      case "Enter": case " ": e.preventDefault(); onSelect(id); break;
      case "F2": if (node && !isSynced(node.team)) { e.preventDefault(); setRenaming(node.team.id); } break;
    }
  };

  const renderNode = (n: TeamNode): React.ReactNode => {
    const tm = n.team;
    const inactive = tm.active === false;
    const synced = isSynced(tm);
    const sourceName = tm.source_title ?? tm.source;
    const hasChildren = n.children.length > 0;
    const open = isOpen(n);
    const isSel = selected === tm.id;
    const occupied = tm.member_ids.length > 0 || n.children.some((c) => c.team.active !== false);
    // 菜单：新建下级 / 编辑 / 重命名 / 移动 / 边界，最后是危险项——同步来的团队只能停用，手工建的才能删除
    const items: MenuItem[] = [
      { key: "child", label: t("settings.people.newChild"), onSelect: () => startCreate(tm.id), disabled: inactive && t("settings.people.teamInactive") },
      { key: "edit", label: t("settings.people.editTeam"), onSelect: () => actions.editTeam(tm) },
      { key: "rename", label: t("settings.people.rename"), onSelect: () => { setCreating(null); setRenaming(tm.id); }, disabled: synced && t("settings.people.syncedTeam", { name: sourceName }) },
      { key: "move", label: t("settings.people.moveTo"), onSelect: () => actions.moveTeam(tm) },
      { key: "boundary", label: tm.is_boundary ? t("settings.people.unsetBoundary") : t("settings.people.setBoundary"), onSelect: () => actions.toggleBoundary(tm) },
    ];
    if (inactive) items.push({ key: "restore", label: t("settings.people.reactivateTeam"), onSelect: () => actions.reactivateTeam(tm) });
    else if (synced) items.push({ key: "deactivate", label: t("settings.people.deactivateTeam"), onSelect: () => actions.deactivateTeam(tm), danger: true, disabled: occupied && t("settings.people.teamNotEmpty") });
    // 手工团队有人有下级也能删：影响范围与二次确认在对话框里
    if (!synced) items.push({ key: "delete", label: t("settings.people.deleteTeam"), onSelect: () => actions.deleteTeam(tm), danger: true });
    return (
      <li key={tm.id} role="none">
        <div
          role="treeitem"
          aria-level={n.depth + 2}
          aria-expanded={hasChildren ? open : undefined}
          aria-selected={isSel}
          tabIndex={isSel ? 0 : -1}
          data-tree-row
          data-team={tm.id}
          data-menu-open={menuFor === tm.id || undefined}
          onClick={() => onSelect(tm.id)}
          className={cx("group relative flex h-8 cursor-pointer items-center gap-1.5 rounded-md pr-1 select-none outline-none focus-visible:ring-1 focus-visible:ring-accent-focus", isSel ? "bg-accent-bg text-accent-hover" : "text-ink hover:bg-surface-2", inactive && !isSel && "text-ink-subtle")}
          style={{ paddingLeft: 8 + (n.depth + 1) * 16 }}
        >
          {hasChildren ? (
            <button type="button" tabIndex={-1} aria-label={open ? t("settings.people.collapse") : t("settings.people.expand")} onClick={(e) => { e.stopPropagation(); onToggle(tm.id, !open); }} className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-xs text-ink-subtle hover:text-ink">
              <IconChevronRight size={12} className={cx("transition-transform duration-150", open && "rotate-90")} />
            </button>
          ) : <span className="w-4 shrink-0" aria-hidden="true" />}
          <IconTeam size={14} className={cx("shrink-0", isSel ? "text-accent-hover" : "text-ink-subtle")} />
          {renaming === tm.id ? (
            <InlineName initial={tm.name} onDone={async (name) => { if (name && name !== tm.name && !(await actions.renameTeam(tm, name))) return; setRenaming(null); }} onCancel={() => setRenaming(null)} />
          ) : (
            <span className="min-w-0 flex-1 truncate" title={tm.name}>{tm.name}</span>
          )}
          {tm.is_boundary && <Tip tip={t("settings.teams.boundaryOn")} className="shrink-0"><IconBoundary size={12} className="text-accent" aria-label={t("settings.people.boundaryMark")} /></Tip>}
          {synced && <Tip tip={t("settings.people.syncedTeam", { name: sourceName })} className="shrink-0"><IconLink size={12} className="text-ink-subtle" aria-label={t("settings.directory.from", { name: sourceName })} /></Tip>}
          {inactive && <Tag dark>{t("settings.people.teamInactive")}</Tag>}
          {slot(tm.id, counts.get(tm.id) ?? 0, items, t("settings.people.teamActions", { name: tm.name }), isSel)}
        </div>
        {(hasChildren || creating?.parent === tm.id) && open && (
          <ul role="group">
            {n.children.map(renderNode)}
            {creating?.parent === tm.id && <NewTeamRow depth={n.depth + 1} onDone={async (name) => { if (!name || (await actions.createTeam(tm.id, name))) setCreating(null); }} onCancel={() => setCreating(null)} />}
          </ul>
        )}
      </li>
    );
  };

  return (
    <div className={cx("flex min-h-0 flex-col", className)}>
      <div className="px-3 pt-3 pb-2">
        <label className="relative block">
          <IconSearch size={14} className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-tertiary" />
          <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("settings.people.search")} aria-label={t("settings.people.search")} className="h-8 w-full rounded-md border border-hairline bg-surface-1 pr-3 pl-8 text-body text-ink placeholder:text-ink-tertiary hover:border-hairline-strong" onKeyDown={(e) => { if (e.key === "Escape") setQuery(""); }} />
        </label>
      </div>
      {q ? (
        <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2" role="listbox" aria-label={t("settings.people.search")}>
          {hitMembers.length === 0 && hitTeams.length === 0 && <p className="px-2 py-6 text-center text-caption text-ink-subtle">{t("settings.people.searchEmpty")}</p>}
          {hitMembers.length > 0 && <div className="eyebrow px-2 pt-1 pb-1 text-ink-subtle">{t("settings.people.searchMembers")}</div>}
          {hitMembers.map((m) => (
            <button key={m.id} type="button" role="option" aria-selected={false} onClick={() => { setQuery(""); onSelect(m.team_id ?? null, m.id); }} className="flex h-9 w-full items-center gap-2 rounded-md px-2 text-left hover:bg-surface-2">
              <Avatar name={m.name} size={20} />
              <span className="min-w-0 flex-1 truncate text-body">{m.name} <span className="text-caption text-ink-subtle">{m.email}</span></span>
              <span className="shrink-0 text-caption text-ink-subtle">{teamName(m.team_id) ?? t("settings.members.noTeam")}</span>
            </button>
          ))}
          {hitTeams.length > 0 && <div className="eyebrow px-2 pt-2 pb-1 text-ink-subtle">{t("settings.people.searchTeams")}</div>}
          {hitTeams.map((x) => (
            <button key={x.id} type="button" role="option" aria-selected={false} onClick={() => { setQuery(""); onSelect(x.id); }} className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-left hover:bg-surface-2">
              <IconTeam size={14} className="shrink-0 text-ink-subtle" />
              <span className="min-w-0 flex-1 truncate text-body">{x.name}</span>
              <span className="shrink-0 text-caption tabular-nums text-ink-subtle">{counts.get(x.id) ?? 0}</span>
            </button>
          ))}
        </div>
      ) : (
        <ul ref={treeRef} role="tree" aria-label={t("settings.people.tree")} className="min-h-0 flex-1 overflow-y-auto px-2 pb-2" onKeyDown={onKey}>
          <li role="none">
            <div role="treeitem" aria-level={1} aria-selected={selected === null} tabIndex={selected === null ? 0 : -1} data-tree-row data-menu-open={menuFor === "__root" || undefined} onClick={() => onSelect(null)} className={cx("group flex h-8 cursor-pointer items-center gap-1.5 rounded-md pr-1 pl-2 select-none outline-none focus-visible:ring-1 focus-visible:ring-accent-focus", selected === null ? "bg-accent-bg text-accent-hover" : "text-ink hover:bg-surface-2")}>
              <span className="w-4 shrink-0" aria-hidden="true" />
              <IconOrg size={14} className={cx("shrink-0", selected === null ? "text-accent-hover" : "text-ink-subtle")} />
              <span className="min-w-0 flex-1 truncate font-medium" title={orgName}>{orgName}</span>
              {slot("__root", total, [{ key: "new", label: t("settings.people.newTeam"), icon: <IconPlus />, onSelect: () => startCreate(null) }], t("settings.people.teamActions", { name: orgName }), selected === null)}
            </div>
            <ul role="group">
              {nodes.map(renderNode)}
              {creating?.parent === null && <NewTeamRow depth={0} onDone={async (name) => { if (!name || (await actions.createTeam(null, name))) setCreating(null); }} onCancel={() => setCreating(null)} />}
            </ul>
          </li>
        </ul>
      )}
      <div className="border-t border-hairline p-2">
        <Button variant="ghost" size="sm" icon={<IconPlus />} className="w-full justify-start" onClick={() => startCreate(null)}>{t("settings.people.newTeam")}</Button>
      </div>
    </div>
  );
}

function flatNodes(nodes: TeamNode[]): TeamNode[] {
  return nodes.flatMap((n) => [n, ...flatNodes(n.children)]);
}

/** 就地改名：回车保存、Esc 取消、失焦保存。 */
function InlineName({ initial, onDone, onCancel }: { initial: string; onDone: (name: string) => void | Promise<void>; onCancel: () => void }) {
  const [value, setValue] = useState(initial);
  const done = useRef(false);
  const finish = (save: boolean) => {
    if (done.current) return;
    done.current = true;
    if (save) void onDone(value.trim()); else onCancel();
  };
  return (
    <input
      autoFocus
      value={value}
      onChange={(e) => setValue(e.target.value)}
      onFocus={(e) => e.currentTarget.select()}
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => { e.stopPropagation(); if (e.key === "Enter") { e.preventDefault(); finish(true); } else if (e.key === "Escape") { e.preventDefault(); finish(false); } }}
      onBlur={() => finish(true)}
      aria-label={t("settings.people.rename")}
      className="h-6 min-w-0 flex-1 rounded-xs border border-accent-focus bg-surface-1 px-1.5 text-body text-ink outline-none"
    />
  );
}

/** 新建团队的输入行：回车创建、Esc 取消、空着失焦就取消。 */
function NewTeamRow({ depth, onDone, onCancel }: { depth: number; onDone: (name: string) => void | Promise<void>; onCancel: () => void }) {
  const [value, setValue] = useState("");
  const done = useRef(false);
  const finish = (save: boolean) => {
    if (done.current) return;
    const name = value.trim();
    if (!save || !name) { done.current = true; onCancel(); return; }
    done.current = true;
    void Promise.resolve(onDone(name)).finally(() => { done.current = false; });
  };
  return (
    <li role="none">
      <div className="flex h-8 items-center gap-1.5 rounded-md pr-1" style={{ paddingLeft: 8 + (depth + 1) * 16 }}>
        <span className="w-4 shrink-0" aria-hidden="true" />
        <IconTeam size={14} className="shrink-0 text-ink-subtle" />
        <input
          autoFocus
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder={t("settings.people.teamNamePlaceholder")}
          aria-label={t("settings.people.newTeam")}
          onKeyDown={(e) => { e.stopPropagation(); if (e.key === "Enter") { e.preventDefault(); finish(true); } else if (e.key === "Escape") { e.preventDefault(); finish(false); } }}
          onBlur={() => { if (!value.trim()) finish(false); }}
          className="h-6 min-w-0 flex-1 rounded-xs border border-accent-focus bg-surface-1 px-1.5 text-body text-ink outline-none placeholder:text-ink-tertiary"
        />
      </div>
    </li>
  );
}
