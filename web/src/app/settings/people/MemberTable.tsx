"use client";
import { useState, type MouseEvent } from "react";
import { inviteUrlOf, isSynced, teamIdsOf, type OrgMember, type OrgRole, type OrgTeam } from "@/lib/api";
import { t } from "@/lib/i18n";
import { IconLink } from "@/components/icons";
import { Avatar, Button, Menu, StatusLED, Table, Tag, TagList, Tip, cx, type MenuItem } from "@/components/ui";
import { DuplicateDot, MemberStatusTag } from "./MemberDrawer";

/*
 * 右栏成员表（DESIGN.md §16）：多选框、姓名（头像 + 名字 + 状态标签）、状态、邮箱、角色（最多两个）、团队、来源、操作（详情 · ⋯）。
 * 点行打开详情抽屉；勾选框与操作列的点击不冒泡。
 * 疑似重复的人名旁有一个小提示点「可能与 X 重复」，点开即合并对话框（ADR 0017 补记四）。
 * 同步来的成员：团队格里常驻一个小链接点，不用打开菜单就看得见团队为什么改不了，点它直接去「IM 集成 → 对应关系」解绑。
 */

/** 表格里的一行就是一个成员；待激活的成员带 invitation（id 用于「撤回邀请」） */
export type PersonRow = OrgMember;

export interface RowActions {
  open: (m: OrgMember) => void;
  revokeInvite: (m: PersonRow) => void;
  changeTeam: (m: OrgMember) => void;
  changeRoles: (m: OrgMember) => void;
  copyInvite: (m: OrgMember) => void;
  deactivate: (m: OrgMember) => void;
  reactivate: (m: OrgMember) => void;
  makeOwner: (m: OrgMember) => void;
  /** 「可能与 X 重复」→ 合并对话框 */
  merge: (m: OrgMember, otherId: string) => void;
  /** 同步来的成员：团队由同步决定，去「IM 集成 → 对应关系」解绑 */
  goUnbind?: () => void;
}

/** 操作列钉在右侧；左边一道细线把它和滚动内容分开 */
const STICKY = "sticky right-0 z-[1] shadow-[inset_1px_0_0_var(--c-hairline)]";

export function MemberTable({ members, teams, roles, selected, onToggle, onToggleAll, highlight, canTransferOwner, actions }: {
  members: PersonRow[];
  teams: OrgTeam[];
  roles: OrgRole[];
  selected: Set<string>;
  onToggle: (id: string, checked: boolean) => void;
  onToggleAll: (ids: string[], checked: boolean) => void;
  highlight: string | null;
  /** 只有组织负责人自己能把负责人转给别人 */
  canTransferOwner: boolean;
  actions: RowActions;
}) {
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const roleTitle = (name: string) => roles.find((r) => r.name === name)?.title ?? name;
  const teamName = (id: string | null) => teams.find((x) => x.id === id)?.name ?? null;
  const ids = members.map((m) => m.id);
  const allChecked = ids.length > 0 && ids.every((id) => selected.has(id));
  const someChecked = ids.some((id) => selected.has(id));

  const rowClick = (e: MouseEvent<HTMLTableRowElement>, m: PersonRow) => {
    const el = e.target as HTMLElement;
    if (el.closest("button, input, a, [role=menu]")) return;
    actions.open(m);
  };

  return (
    // 1440 宽、左边开着团队树时右栏只有约 730px：状态列在 2xl 以下收起（名字旁的状态标签已经说明待激活 / 已停用），其余列都放得下、团队名不被操作列盖住；再窄才横向滚动
    <Table className="[&>table]:min-w-[720px]">
      <thead>
        <tr>
          <th className="w-[36px] !pr-0"><input type="checkbox" className="h-3.5 w-3.5 align-middle" checked={allChecked} ref={(el) => { if (el) el.indeterminate = !allChecked && someChecked; }} onChange={(e) => onToggleAll(ids, e.target.checked)} aria-label={t("settings.people.selectAll")} /></th>
          <th className="w-full">{t("settings.members.name")}</th>
          <th className="hidden w-[76px] 2xl:table-cell">{t("settings.members.status")}</th>
          <th className="w-[130px]">{t("common.email")}</th>
          <th className="w-[120px]">{t("settings.members.roles")}</th>
          <th className="w-[110px]">{t("settings.members.team")}</th>
          <th className="w-[84px]">{t("settings.people.source")}</th>
          {/* 操作列钉在右边：窄屏横向滚动时也够得着 */}
          <th className={cx("actions w-[100px]", STICKY, "bg-surface-2")} aria-label={t("common.actions")} />
        </tr>
      </thead>
      <tbody>
        {members.map((m) => {
          const inactive = !m.active || m.status === "inactive";
          const pending = !inactive && m.status === "pending_activation";
          const synced = isSynced(m);
          const sourceName = m.source_title ?? m.source;
          const extraTeams = teamIdsOf(m).filter((id) => id !== m.team_id).map((id) => teamName(id) ?? id);
          const direct = teamName(m.team_id);
          const checked = selected.has(m.id);
          const items: MenuItem[] = [
            { key: "team", label: t("settings.people.changeTeam"), onSelect: () => actions.changeTeam(m), disabled: synced && t("settings.people.syncedTeamUnbind", { name: sourceName }) },
            { key: "roles", label: t("settings.people.changeRoles"), onSelect: () => actions.changeRoles(m) },
            ...(pending && inviteUrlOf(m) ? [{ key: "invite", label: t("settings.members.copyInvite"), onSelect: () => actions.copyInvite(m) }] : []),
            // 待激活且有邀请记录：可以撤回邀请（连带这条待激活成员）；否则按停用 / 恢复
            ...(pending && m.invitation?.id ? [{ key: "revoke", label: t("settings.people.revokeInvite"), onSelect: () => actions.revokeInvite(m), danger: true }] : []),
            inactive
              ? { key: "restore", label: t("settings.people.reactivate"), onSelect: () => actions.reactivate(m) }
              : { key: "deactivate", label: t("settings.people.deactivate"), onSelect: () => actions.deactivate(m), danger: true, disabled: m.is_owner && t("settings.people.ownerKeep") },
            ...(canTransferOwner && !m.is_owner && !inactive && !pending ? [{ key: "owner", label: t("settings.members.makeOwner"), onSelect: () => actions.makeOwner(m) }] : []),
          ];
          return (
            <tr key={m.id} data-member={m.id} onClick={(e) => rowClick(e, m)} aria-selected={checked} className={cx("group cursor-pointer aria-selected:bg-accent-bg/60", inactive && "text-ink-subtle", highlight === m.id && "row-new")}>
              <td className="!pr-0"><input type="checkbox" className="h-3.5 w-3.5 align-middle" checked={checked} onChange={(e) => onToggle(m.id, e.target.checked)} aria-label={t("settings.people.selectOne", { name: m.name })} /></td>
              <td className="w-full max-w-0 min-w-[170px]">
                <span className="flex items-center gap-2">
                  <Avatar name={m.name} size={20} className={cx(inactive && "opacity-60")} />
                  <span className={cx("min-w-0 truncate font-medium", inactive && "text-ink-subtle")} title={m.name}>{m.name}</span>
                  {m.is_owner && <Tag tone="accent" className="shrink-0">{t("settings.members.owner")}</Tag>}
                  <span className="shrink-0 empty:hidden"><MemberStatusTag member={m} /></span>
                  <DuplicateDot member={m} onOpen={(otherId) => actions.merge(m, otherId)} />
                </span>
              </td>
              <td className="hidden whitespace-nowrap 2xl:table-cell">
                <span className="inline-flex items-center gap-1.5 text-caption">
                  <StatusLED tone={inactive ? "dark" : pending ? "warning" : "success"} />
                  <span className={inactive ? "text-ink-subtle" : "text-ink-muted"}>{m.status_title ?? (inactive ? t("settings.people.status.inactive") : pending ? t("settings.people.status.pending") : t("settings.people.status.active"))}</span>
                </span>
              </td>
              <td className="max-w-[130px]"><span className="block truncate text-ink-muted" title={m.email}>{m.email}</span></td>
              <td className="whitespace-nowrap"><TagList items={m.roles.map(roleTitle)} max={2} /></td>
              <td className="max-w-[110px] whitespace-nowrap">
                <span className="inline-flex max-w-full items-center gap-1">
                  <span className="truncate" title={direct ?? undefined}>{direct ?? <span className="text-ink-subtle">—</span>}</span>
                  {extraTeams.length > 0 && <Tip tip={t("settings.people.alsoIn", { teams: extraTeams.join("、") })}><Tag>+{extraTeams.length}</Tag></Tip>}
                  {synced && actions.goUnbind && (
                    <Tip tip={t("settings.people.syncedTeamUnbind", { name: sourceName })}>
                      <button type="button" className="pressable inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-ink-tertiary hover:bg-surface-3 hover:text-ink-muted" aria-label={t("settings.people.syncedTeamUnbind", { name: sourceName })} onClick={(e) => { e.stopPropagation(); actions.goUnbind?.(); }} data-synced-team={m.id}>
                        <IconLink size={12} />
                      </button>
                    </Tip>
                  )}
                </span>
              </td>
              <td className="whitespace-nowrap">{synced ? <Tag title={t("settings.members.nameSynced", { name: sourceName })}>{t("settings.directory.from", { name: sourceName })}</Tag> : <span className="text-ink-subtle">{m.source_title ?? t("mock.source.manual")}</span>}</td>
              <td className={cx("actions", STICKY, checked ? "bg-surface-2" : "bg-surface-1 group-hover:bg-surface-2")}>
                {/* 「详情」悬停出现；⋮ 常驻，钉住的操作列才不会看起来是空的一条 */}
                <span className="inline-flex items-center gap-1">
                  <span className={cx("row-actions", menuFor === m.id && "!opacity-100")}><Button size="sm" variant="ghost" onClick={() => actions.open(m)}>{t("settings.people.detail")}</Button></span>
                  <Menu items={items} label={t("settings.people.moreActions")} onOpenChange={(o) => setMenuFor(o ? m.id : null)} />
                </span>
              </td>
            </tr>
          );
        })}
      </tbody>
    </Table>
  );
}
