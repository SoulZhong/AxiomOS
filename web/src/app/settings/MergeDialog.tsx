"use client";
import { useState } from "react";
import { api, isSynced, type DirectoryKind, type ID, type MemberSource } from "@/lib/api";
import { errorMessage } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconCheck, IconMember, IconTeam } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Dialog, ErrorBox, Tag, cx } from "@/components/ui";

/*
 * 合并对话框（ADR 0017 补记四，DESIGN.md §14「冲突与对应关系」）：两个疑似重复的对象，选保留哪一个，另一个并进去。
 * 对应关系面板的「可能重复」、成员页的「可能与 X 重复」提示、成员抽屉里的「合并」都开这一个。
 * 有一边不能被并走时（组织负责人、已停用的团队 —— 后端给 can_be_merged_away / keep_reason），方向就定了：
 * 直接选中那一边，另一张卡片不再是可选项，写明原因，省得人选了再挨一句红字。两边都能并走时默认保留同步来的那个（它以后还会随 IM 更新）。
 * 写清楚什么会过去、什么不变；团队要写明"旧团队的成员、目标、任务、迭代会并过去，旧团队停用"。
 */
export interface MergeSide {
  id: ID;
  name: string;
  /** 团队路径（成员：所在团队；团队：上级路径） */
  team_path?: string;
  source?: MemberSource;
  source_title?: string;
  active?: boolean;
  /** 能不能当被并走的那个（后端按合并时只看被并走那一方的规矩给）；false 时 keep_reason 是一句原因 */
  can_be_merged_away?: boolean;
  keep_reason?: string;
}

/** 只有一边不能被并走时返回那一边（方向就定了）；两边都能、或都不能时返回 null，交给默认与后端。 */
function mustKeepSide(a: MergeSide, b: MergeSide): MergeSide | null {
  const stuck = [a, b].filter((s) => s.can_be_merged_away === false);
  return stuck.length === 1 ? stuck[0] : null;
}

export function MergeDialog({ kind, a, b, reason, providerTitle, onClose, onMerged }: { kind: DirectoryKind; a: MergeSide; b: MergeSide; reason?: string; providerTitle?: string; onClose: () => void; onMerged: () => void }) {
  const toast = useToast();
  const mustKeep = mustKeepSide(a, b);
  // 有一边只能保留时就选它；否则默认保留同步来的那个，两个都是手工（或都同步）时保留 a
  const [keep, setKeep] = useState<ID>(() => (mustKeep ? mustKeep.id : isSynced(b) && !isSynced(a) ? b.id : a.id));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const into = keep === a.id ? a : b;
  const from = keep === a.id ? b : a;
  const keepingManual = !isSynced(into) && isSynced(from);
  const provider = providerTitle ?? from.source_title ?? into.source_title ?? "";
  // 两边同名时句子里带上来源，不然「把「吴小雨」并入「吴小雨」」看不出方向
  const label = (x: MergeSide) => (a.name === b.name ? `${x.name}（${isSynced(x) ? t("settings.directory.from", { name: x.source_title ?? x.source }) : x.source_title ?? t("mock.source.manual")}）` : x.name);
  const submit = async () => {
    setBusy(true); setError(null);
    try {
      if (kind === "team") await api.org.mergeTeam(from.id, into.id);
      else await api.org.mergeMember(from.id, into.id);
      toast.ok(t("settings.merge.done", { from: from.name, into: into.name }));
      onMerged(); onClose();
    } catch (err) { setError(errorMessage(err)); } finally { setBusy(false); }
  };
  return (
    <Dialog open onClose={onClose} title={t(kind === "team" ? "settings.merge.title.team" : "settings.merge.title.member")} width={520} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="danger" disabled={busy} onClick={() => void submit()}>{busy ? t("settings.merge.merging") : t("settings.merge.confirm")}</Button></>}>
      {reason && <p className="text-caption text-ink-subtle">{reason}</p>}
      <div className="eyebrow text-ink-subtle">{t("settings.merge.keep")}</div>
      <div className="grid gap-2 sm:grid-cols-2" role="radiogroup" aria-label={t("settings.merge.keep")} data-merge-sides>
        {[a, b].map((side) => <SideCard key={side.id} kind={kind} side={side} selected={keep === side.id} locked={!!mustKeep && side.id !== mustKeep.id} onSelect={() => setKeep(side.id)} />)}
      </div>
      {mustKeep?.keep_reason && <p className="text-caption text-ink-muted" data-merge-must-keep>{t("settings.merge.mustKeep", { name: mustKeep.name, reason: mustKeep.keep_reason })}</p>}
      <div className="rounded-md border border-warning-border bg-warning-bg p-3 text-body text-warning" data-merge-note>
        <p className="font-medium">{t("settings.merge.willMerge", { from: label(from), into: label(into) })}</p>
        <p className="mt-1">{t(kind === "team" ? "settings.merge.teamNote" : "settings.merge.memberNote")}</p>
        {keepingManual && provider && <p className="mt-1">{t("settings.merge.syncedNote", { provider })}</p>}
      </div>
      {error && <ErrorBox message={error} />}
    </Dialog>
  );
}

/** 一边一张卡片。locked = 保留这一边这个选项不成立（另一边只能保留），卡片还在，但淡下去、没有选择圈、点不动。 */
function SideCard({ kind, side, selected, locked, onSelect }: { kind: DirectoryKind; side: MergeSide; selected: boolean; locked: boolean; onSelect: () => void }) {
  const Icon = kind === "team" ? IconTeam : IconMember;
  const why = side.can_be_merged_away === false ? side.keep_reason : undefined;
  const body = (
    <>
      {locked
        ? <span className="mt-0.5 inline-block h-4 w-4 shrink-0" aria-hidden="true" />
        : <span className={cx("mt-0.5 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full border", selected ? "border-accent bg-accent text-on-accent" : "border-hairline-strong")} aria-hidden="true">{selected && <IconCheck size={10} />}</span>}
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-1.5">
          <Icon size={14} className="shrink-0 text-ink-subtle" aria-hidden="true" />
          <span className="truncate font-medium text-ink" title={side.name}>{side.name}</span>
          {side.active === false && <Tag dark>{t("settings.directory.mappings.inactive")}</Tag>}
        </span>
        <span className="mt-0.5 block truncate text-caption text-ink-muted" title={side.team_path || undefined}>{side.team_path || "—"}</span>
        {why && <span className="mt-0.5 block text-caption text-ink-muted">{t("settings.merge.mustKeepThis", { reason: why })}</span>}
        <span className="mt-1.5 block"><Tag tone={isSynced(side) ? "accent" : "neutral"}>{isSynced(side) ? t("settings.directory.from", { name: side.source_title ?? side.source }) : side.source_title ?? t("mock.source.manual")}</Tag></span>
        {locked ? <span className="mt-1.5 block text-caption text-ink-subtle">{t("settings.merge.mergedAway")}</span> : selected && <span className="mt-1.5 block text-caption text-accent">{t("settings.merge.keepThis")}</span>}
      </span>
    </>
  );
  const shell = "flex items-start gap-2.5 rounded-md border p-3 text-left";
  if (locked) return <div className={cx(shell, "border-hairline bg-surface-2 opacity-60")} data-merge-locked={side.id}>{body}</div>;
  return (
    <button type="button" role="radio" aria-checked={selected} onClick={onSelect} className={cx(shell, selected ? "border-accent bg-accent-bg/40 shadow-[inset_0_0_0_1px_var(--c-accent)]" : "border-hairline bg-surface-1 hover:bg-surface-3")}>
      {body}
    </button>
  );
}
