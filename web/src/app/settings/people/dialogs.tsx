"use client";
import { useRef, useState, type FormEvent } from "react";
import { api, isSynced, type BulkMembersResult, type DirectoryDecision, type Invitation, type MemberImportDecision, type MemberImportPreview, type MemberImportResult, type MemberImportRow, type OrgMember, type OrgRole, type OrgTeam } from "@/lib/api";
import { errorMessage } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconBoundary, IconCheck, IconLink, IconTeam, IconUpload } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Checkbox, CopyButton, CopyLine, Dialog, ErrorBox, Field, Input, Segmented, Select, Table, Tag, cx, type Tone } from "@/components/ui";
import { buildTree, flattenTree, subtreeIds } from "./tree";

/*
 * 成员与团队页的对话框（DESIGN.md §16）：移动团队、批量变更团队、单人变更团队 / 改角色、邀请成员、CSV 导入。
 * 每个对话框自己管表单状态；父组件用 key 让它在每次打开时重置。
 */

/** 团队选择列表（单选）：带层级缩进、边界标记与同步标记；exclude 里的团队置灰并说明原因。 */
export function TeamPickerList({ teams, value, onChange, exclude, excludeReason, noneLabel, className }: { teams: OrgTeam[]; value: string | null; onChange: (id: string | null) => void; exclude?: Set<string>; excludeReason?: string; noneLabel?: string; className?: string }) {
  const rows = flattenTree(buildTree(teams.filter((x) => x.active !== false)));
  const row = (id: string | null, label: string, depth: number, disabled: boolean, marks?: React.ReactNode) => {
    const selected = value === id;
    return (
      <button key={id ?? "__none"} type="button" role="radio" aria-checked={selected} aria-disabled={disabled || undefined} disabled={disabled} title={disabled ? excludeReason : undefined} onClick={() => !disabled && onChange(id)} className={cx("flex h-8 w-full items-center gap-2 rounded-md pr-2 text-left text-body", selected ? "bg-accent-bg text-accent-hover" : disabled ? "text-ink-tertiary" : "text-ink hover:bg-surface-3")} style={{ paddingLeft: 8 + depth * 16 }}>
        <span className="inline-flex w-3 shrink-0 justify-center text-accent" aria-hidden="true">{selected && <IconCheck size={12} />}</span>
        {id !== null && <IconTeam size={14} className="shrink-0 opacity-70" />}
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {marks}
      </button>
    );
  };
  return (
    <div className={cx("max-h-[280px] overflow-y-auto rounded-md border border-hairline bg-surface-1 p-1", className)} role="radiogroup">
      {noneLabel !== undefined && row(null, noneLabel, 0, false)}
      {rows.map(({ team, depth }) => row(team.id, team.name, depth, !!exclude?.has(team.id), (
        <span className="inline-flex shrink-0 items-center gap-1 text-ink-subtle">
          {team.is_boundary && <IconBoundary size={12} aria-label={t("settings.people.boundaryMark")} />}
          {isSynced(team) && <IconLink size={12} aria-label={t("settings.directory.from", { name: team.source_title ?? team.source })} />}
        </span>
      )))}
    </div>
  );
}

/** 编辑团队：名称（同步来的不能改）、上级、负责人、共享边界，一次改好。只提交改过的字段。 */
export function EditTeamDialog({ team, teams, members, onClose, onSaved }: { team: OrgTeam; teams: OrgTeam[]; members: OrgMember[]; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const synced = isSynced(team);
  const [name, setName] = useState(team.name);
  const [parent, setParent] = useState<string | null>(team.parent_id ?? null);
  const [lead, setLead] = useState<string>(team.lead_id ?? "");
  const [boundary, setBoundary] = useState(!!team.is_boundary);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const exclude = subtreeIds(teams, team.id);
  const parentRows = flattenTree(buildTree(teams.filter((x) => x.active !== false)));
  const people = [...members].filter((m) => m.active !== false).sort((a, b) => a.name.localeCompare(b.name, "zh-Hans-CN"));
  const patch: Parameters<typeof api.org.updateTeam>[1] = {};
  if (!synced && name.trim() && name.trim() !== team.name) patch.name = name.trim();
  if (parent !== (team.parent_id ?? null)) patch.parent_id = parent;
  if ((lead || null) !== (team.lead_id ?? null)) patch.lead_id = lead || null;
  if (boundary !== !!team.is_boundary) patch.is_boundary = boundary;
  const dirty = Object.keys(patch).length > 0;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!dirty) return;
    setBusy(true); setError(null);
    try {
      await api.org.updateTeam(team.id, patch);
      toast.ok(t("toast.saved"));
      onSaved(); onClose();
    } catch (err) { setError(errorMessage(err)); } finally { setBusy(false); }
  };
  return (
    <Dialog open onClose={onClose} title={t("settings.people.editTeamTitle", { name: team.name })} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" type="submit" form="edit-team" disabled={busy || !dirty || (!synced && !name.trim())}>{t("common.save")}</Button></>}>
      <form id="edit-team" onSubmit={submit} className="flex flex-col gap-3">
        <Field label={t("settings.teams.name")} hint={synced ? t("settings.teams.nameSynced", { name: team.source_title ?? team.source }) : undefined}>
          <Input value={name} onChange={(e) => setName(e.target.value)} disabled={synced} maxLength={80} autoFocus={!synced} className="w-full" />
        </Field>
        <Field label={t("settings.teams.parent")} as="div">
          <Select value={parent ?? ""} onChange={(e) => setParent(e.target.value || null)} className="w-full">
            <option value="">{t("settings.teams.noParent")}</option>
            {parentRows.map(({ team: x, depth }) => <option key={x.id} value={x.id} disabled={exclude.has(x.id)}>{"\u00a0\u00a0".repeat(depth)}{x.name}</option>)}
          </Select>
        </Field>
        <Field label={t("settings.teams.lead")} as="div">
          <Select value={lead} onChange={(e) => setLead(e.target.value)} className="w-full">
            <option value="">{t("settings.teams.noLead")}</option>
            {people.map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
          </Select>
        </Field>
        <Field label={t("settings.teams.boundary")} hint={boundary ? t("settings.teams.boundaryOn") : t("settings.teams.boundaryOff")} as="div">
          <Checkbox label={t("settings.teams.boundaryTag")} checked={boundary} onChange={(e) => setBoundary(e.target.checked)} />
        </Field>
        {error && <ErrorBox message={error} />}
      </form>
    </Dialog>
  );
}

/** 移动团队：选一个新的上级（自己和下级不能选）。 */
export function MoveTeamDialog({ team, teams, onClose, onMoved }: { team: OrgTeam | null; teams: OrgTeam[]; onClose: () => void; onMoved: () => void }) {
  const toast = useToast();
  const [target, setTarget] = useState<string | null>(team?.parent_id ?? null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const exclude = team ? subtreeIds(teams, team.id) : new Set<string>();
  const submit = async () => {
    if (!team) return;
    setBusy(true); setError(null);
    try {
      await api.org.updateTeam(team.id, { parent_id: target });
      toast.ok(t("settings.people.teamMoved", { name: team.name }));
      onMoved(); onClose();
    } catch (err) { setError(errorMessage(err)); } finally { setBusy(false); }
  };
  return (
    <Dialog open={!!team} onClose={onClose} title={t("settings.people.moveTeamTitle", { name: team?.name })} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" disabled={busy || target === (team?.parent_id ?? null)} onClick={() => void submit()}>{t("common.confirm")}</Button></>}>
      <TeamPickerList teams={teams} value={target} onChange={setTarget} exclude={exclude} excludeReason={t("settings.people.moveTeamSelf")} noneLabel={t("settings.people.topLevel")} />
      <p className="text-caption text-ink-subtle">{t("settings.people.moveTeamSelf")}</p>
      {error && <ErrorBox message={error} />}
    </Dialog>
  );
}

/** 批量变更团队：移到（离开原团队）或加入（保留原团队），确认前写明影响人数，结束后列出没变更的人和原因。 */
export function BulkTeamDialog({ open, members, teams, defaultTeam, onClose, onDone, onGoUnbind }: { open: boolean; members: OrgMember[]; teams: OrgTeam[]; defaultTeam: string | null; onClose: () => void; onDone: () => void; onGoUnbind?: () => void }) {
  const toast = useToast();
  const [mode, setMode] = useState<"move_team" | "add_team">("move_team");
  const [target, setTarget] = useState<string | null>(defaultTeam);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<BulkMembersResult | null>(null);
  // 打开时的选中名单：提交后父组件会清空选择，结果里的名字仍要能对上
  const [snapshot] = useState(members);
  const affected = snapshot.filter((m) => m.active !== false);
  const nameOf = (id: string) => snapshot.find((m) => m.id === id)?.name ?? id;
  // 名单里同步来的那些人是从哪个 IM 来的（说「解绑」时要点名）
  const syncedName = snapshot.find((m) => isSynced(m))?.source_title ?? snapshot.find((m) => isSynced(m))?.source;
  const submit = async () => {
    setBusy(true); setError(null);
    try {
      const r = await api.org.bulkMembers({ member_ids: affected.map((m) => m.id), action: mode, team_id: target });
      setResult(r);
      // 一个都没变更时别报喜：对话框留着，下面列出每个人没变更的原因
      if (r.updated === 0 && r.skipped.length) toast.fail(t("settings.people.bulkNone", { n: r.skipped.length }));
      else toast.ok(t("settings.people.bulkDone", { n: r.updated }));
      onDone();
    } catch (err) { setError(errorMessage(err)); } finally { setBusy(false); }
  };
  const canSubmit = affected.length > 0 && (mode === "add_team" ? target !== null : true);
  return (
    <Dialog open={open} onClose={onClose} title={t("settings.people.bulkTeam")} footer={result ? <Button variant="primary" onClick={onClose}>{t("admin.orgs.done")}</Button> : <><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" disabled={busy || !canSubmit} onClick={() => void submit()}>{t("common.confirm")}</Button></>}>
      {result ? (
        <div className="space-y-2">
          <p className="text-ink-muted">{t("settings.people.bulkDone", { n: result.updated })}</p>
          {result.skipped.length > 0 && (
            <div className="rounded-md border border-warning-border bg-warning-bg p-3">
              <p className="mb-1.5 text-warning">{t("settings.people.bulkSkipped", { n: result.skipped.length })}</p>
              <ul className="space-y-1 text-caption text-ink-muted">
                {result.skipped.map((s) => <li key={s.id} data-skipped={s.id}><span className="font-medium text-ink">{nameOf(s.id)}</span> · {s.reason}</li>)}
              </ul>
              {/* 有人是因为「团队由同步决定」没变更：接着给出路，别只留一句理由 */}
              {result.skipped.some((s) => s.code === "err.member_synced_team") && <UnbindHint name={syncedName} onGoUnbind={onGoUnbind} plain />}
            </div>
          )}
        </div>
      ) : (
        <>
          <Segmented size="sm" value={mode} onChange={setMode} options={[["move_team", t("settings.people.bulkMove")], ["add_team", t("settings.people.bulkAdd")]]} aria-label={t("settings.people.bulkTeam")} />
          <Field as="div" label={t("settings.people.targetTeam")}>
            <TeamPickerList teams={teams} value={target} onChange={setTarget} noneLabel={mode === "move_team" ? t("settings.members.noTeam") : undefined} />
          </Field>
          <p className="text-body text-ink-muted">{t("settings.people.bulkAffect", { n: affected.length })}</p>
          {affected.length < snapshot.length && <p className="text-caption text-ink-subtle">{t("settings.people.selectionInactive")}</p>}
          {error && <ErrorBox message={error} />}
        </>
      )}
    </Dialog>
  );
}

/** 单人变更直属团队。 */
export function ChangeTeamDialog({ member, teams, onClose, onSaved, onGoUnbind }: { member: OrgMember | null; teams: OrgTeam[]; onClose: () => void; onSaved: () => void; onGoUnbind?: () => void }) {
  const toast = useToast();
  const [target, setTarget] = useState<string | null>(member?.team_id ?? null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submit = async () => {
    if (!member) return;
    setBusy(true); setError(null);
    try {
      await api.org.updateMember(member.id, { team_id: target });
      const tm = teams.find((x) => x.id === target);
      toast.ok(tm ? t("settings.people.teamSaved", { name: member.name, team: tm.name }) : t("settings.people.teamCleared", { name: member.name }));
      onSaved(); onClose();
    } catch (err) { setError(errorMessage(err)); } finally { setBusy(false); }
  };
  return (
    <Dialog open={!!member} onClose={onClose} title={t("settings.people.teamTitle", { name: member?.name })} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" disabled={busy || target === (member?.team_id ?? null)} onClick={() => void submit()}>{t("common.save")}</Button></>}>
      <TeamPickerList teams={teams} value={target} onChange={setTarget} noneLabel={t("settings.members.noTeam")} />
      {/* 同步来的成员：先说清团队由同步决定，别让人选完了才挨一句红字；服务端拒了也留在这里把话说完 */}
      {member && isSynced(member) && <UnbindHint name={member.source_title ?? member.source} onGoUnbind={onGoUnbind} />}
      {error && <ErrorBox message={error} />}
    </Dialog>
  );
}

/** 「团队由{name}同步决定。想手工管，先到 IM 集成 → 对应关系里解绑。」+「去解绑」。 */
function UnbindHint({ name, onGoUnbind, plain }: { name?: string; onGoUnbind?: () => void; plain?: boolean }) {
  return (
    <p className={plain ? "mt-2 text-caption text-warning" : "rounded-md border border-warning-border bg-warning-bg p-3 text-body text-warning"} role="note" data-unbind-hint>
      {t("settings.people.syncedTeamUnbind", { name })}
      {onGoUnbind && <> <button type="button" className="font-medium underline-offset-2 hover:underline" onClick={onGoUnbind} data-go-unbind>{t("settings.people.goUnbind")}</button></>}
    </p>
  );
}

/** 单人改角色。 */
export function ChangeRolesDialog({ member, roles, onClose, onSaved }: { member: OrgMember | null; roles: OrgRole[]; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const [picked, setPicked] = useState<string[]>(member?.roles ?? []);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submit = async () => {
    if (!member) return;
    setBusy(true); setError(null);
    try {
      await api.org.updateMember(member.id, { roles: picked });
      toast.ok(t("settings.people.rolesSaved", { name: member.name }));
      onSaved(); onClose();
    } catch (err) { setError(errorMessage(err)); } finally { setBusy(false); }
  };
  return (
    <Dialog open={!!member} onClose={onClose} title={t("settings.people.rolesTitle", { name: member?.name })} footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" disabled={busy} onClick={() => void submit()}>{t("common.save")}</Button></>}>
      <div className="grid grid-cols-2 gap-x-4 gap-y-2">
        {roles.map((r) => <Checkbox key={r.name} checked={picked.includes(r.name)} onChange={(e) => setPicked(e.target.checked ? [...picked, r.name] : picked.filter((x) => x !== r.name))} label={r.title} />)}
      </div>
      {error && <ErrorBox message={error} />}
    </Dialog>
  );
}

/** 邀请成员：邮箱 + 姓名 + 角色 + 团队（默认当前团队）→ 同一个对话框里显示链接。 */
export function InviteDialog({ open, roles, teams, defaultTeam, onClose, onInvited }: { open: boolean; roles: OrgRole[]; teams: OrgTeam[]; defaultTeam: string | null; onClose: () => void; onInvited: (i: Invitation) => void }) {
  const toast = useToast();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [team, setTeam] = useState(defaultTeam ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState<Invitation | null>(null);
  const teamOptions = flattenTree(buildTree(teams.filter((x) => x.active !== false)));
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      const i = await api.org.createInvitation({ email: email.trim(), name: name.trim() || undefined, roles: picked, team_id: team || null });
      setCreated(i);
      toast.ok(t("toast.invited", { email: i.email }));
      onInvited(i);
    } catch (err) { setError(errorMessage(err)); } finally { setBusy(false); }
  };
  return (
    <Dialog open={open} onClose={onClose} title={t("settings.people.invite")} footer={created ? <Button variant="primary" onClick={onClose}>{t("admin.orgs.done")}</Button> : <><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" form="invite-form" type="submit" disabled={busy}>{t("settings.invitations.create")}</Button></>}>
      {created ? (
        <>
          <p className="text-ink-muted">{t("settings.people.inviteDone", { email: created.email })}</p>
          <CopyLine text={created.url} />
          <p className="text-caption text-ink-subtle">{t("settings.invitations.linkHint")}</p>
        </>
      ) : (
        <form id="invite-form" onSubmit={submit} className="space-y-3">
          <p className="text-caption text-ink-subtle">{t("settings.people.inviteHint")}</p>
          <Field label={t("common.email")}><Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus /></Field>
          <Field label={t("common.name")}><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <Field as="div" label={t("settings.members.roles")}>
            <div className="flex flex-wrap gap-x-4 gap-y-1.5">
              {roles.map((r) => <Checkbox key={r.name} checked={picked.includes(r.name)} onChange={(e) => setPicked(e.target.checked ? [...picked, r.name] : picked.filter((x) => x !== r.name))} label={r.title} />)}
            </div>
          </Field>
          <Field label={t("settings.people.inviteTeam")}>
            <Select value={team} onChange={(e) => setTeam(e.target.value)}>
              <option value="">{t("settings.members.noTeam")}</option>
              {teamOptions.map(({ team: x, depth }) => <option key={x.id} value={x.id}>{"　".repeat(depth)}{x.name}</option>)}
            </Select>
          </Field>
          {error && <p className="text-caption text-danger" role="alert">{error}</p>}
        </form>
      )}
    </Dialog>
  );
}

const ACTION_TONE: Record<MemberImportPreview["rows"][number]["action"], Tone> = { create: "success", update: "accent", confirm: "warning", skip: "neutral", invalid: "danger" };
const DECISIONS: DirectoryDecision[] = ["merge", "create", "skip"];

/**
 * CSV 导入：选文件 → 预览（每行一个动作与原因）→ 确认 → 结果（新成员的邀请链接可复制）。
 * 「待确认」行（新邮箱、但姓名与某个手工成员相同且团队同名，ADR 0017 补记四）在行下面给出疑似对象和三选一（合并到已有 / 作为新建 / 跳过），
 * 决定随 decisions[] 一起提交；没决定的行后端会跳过，确认按钮上写明有几行。
 */
export function ImportDialog({ open, roles, teams, onClose, onImported }: { open: boolean; roles: OrgRole[]; teams: OrgTeam[]; onClose: () => void; onImported: () => void }) {
  const toast = useToast();
  const file = useRef<HTMLInputElement>(null);
  const [csv, setCsv] = useState<string | null>(null);
  const [fileName, setFileName] = useState("");
  const [preview, setPreview] = useState<MemberImportPreview | null>(null);
  const [result, setResult] = useState<MemberImportResult | null>(null);
  const [busy, setBusy] = useState<"preview" | "import" | null>(null);
  const [error, setError] = useState<string | null>(null);
  // 对「待确认」行的决定：行号 → 决定（合并时还有并入谁）
  const [decisions, setDecisions] = useState<Record<number, { decision: DirectoryDecision; local_id?: string }>>({});
  const roleTitle = (r: string) => roles.find((x) => x.name === r)?.title ?? r;
  const teamName = (id: string | null, path: string) => teams.find((x) => x.id === id)?.name ?? path;

  const onFile = async (f: File | undefined) => {
    if (!f) return;
    setError(null); setPreview(null); setFileName(f.name); setDecisions({});
    const text = await f.text();
    setCsv(text);
    setBusy("preview");
    try { setPreview(await api.org.importMembersPreview(text)); } catch (err) { setError(errorMessage(err)); } finally { setBusy(null); }
  };
  const decisionList = (): MemberImportDecision[] => Object.entries(decisions).map(([line, d]) => ({ line: Number(line), decision: d.decision, ...(d.decision === "merge" && d.local_id ? { local_id: d.local_id } : {}) }));
  const confirm = async () => {
    if (!csv) return;
    setBusy("import"); setError(null);
    try {
      const r = await api.org.importMembers(csv, decisionList());
      setResult(r);
      toast.ok(t("settings.people.importDone", { created: r.created, updated: r.updated }));
      onImported();
    } catch (err) { setError(errorMessage(err)); } finally { setBusy(null); }
  };
  const confirmRows = preview?.rows.filter((r) => r.action === "confirm") ?? [];
  const undecided = confirmRows.filter((r) => !decisions[r.line]).length;
  const decidedImportable = confirmRows.filter((r) => decisions[r.line] && decisions[r.line].decision !== "skip").length;
  const importable = preview ? preview.summary.create + preview.summary.update + decidedImportable : 0;
  const decide = (line: number, decision: DirectoryDecision, localId?: string) => setDecisions((d) => ({ ...d, [line]: { decision, local_id: localId ?? d[line]?.local_id } }));

  return (
    <Dialog open={open} onClose={onClose} title={t("settings.people.importTitle")} width={preview || result ? 720 : 480} footer={result ? <Button variant="primary" onClick={onClose}>{t("admin.orgs.done")}</Button> : <><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>{preview && <Button variant="primary" disabled={busy !== null || importable === 0} onClick={() => void confirm()} data-import-confirm data-undecided={undecided || undefined}>{undecided > 0 ? t("settings.people.importConfirmSkip", { n: undecided }) : t("settings.people.importConfirm")}</Button>}</>}>
      <input ref={file} type="file" accept=".csv,text/csv" className="hidden" onChange={(e) => { void onFile(e.target.files?.[0]); e.target.value = ""; }} />
      {result ? (
        <div className="space-y-3">
          <p className="text-ink-muted">{t("settings.people.importDone", { created: result.created, updated: result.updated })}</p>
          {result.skipped.length > 0 && (
            <div className="rounded-md border border-warning-border bg-warning-bg p-3">
              <p className="mb-1.5 text-warning">{t("settings.people.importSkipped", { n: result.skipped.length })}</p>
              <ul className="space-y-1 text-caption text-ink-muted">
                {result.skipped.map((s, i) => <li key={i}>{s.line !== undefined && <span className="telemetry mr-1.5">{t("settings.people.line")} {s.line}</span>}{s.email && <span className="mr-1.5 text-ink">{s.email}</span>}{s.reason}</li>)}
              </ul>
            </div>
          )}
          {result.invitations.length > 0 && (
            <div>
              <p className="mb-1.5 text-body text-ink-muted">{t("settings.people.importLinks")}</p>
              <ul className="max-h-[260px] space-y-1.5 overflow-y-auto">
                {result.invitations.map((inv) => (
                  <li key={inv.email} className="flex items-center gap-2 rounded-md border border-hairline bg-surface-1 px-3 py-1.5">
                    <span className="min-w-0 flex-1 truncate">{inv.name ? <span className="font-medium">{inv.name}</span> : null} <span className="text-ink-subtle">{inv.email}</span></span>
                    <CopyButton text={inv.url} size="sm" variant="ghost" />
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      ) : (
        <div className="space-y-3">
          <p className="text-caption text-ink-subtle">{t("settings.people.importHint")}</p>
          <div className="flex items-center gap-3">
            <Button icon={<IconUpload />} onClick={() => file.current?.click()} disabled={busy !== null}>{preview ? t("settings.people.reChooseFile") : t("settings.people.chooseFile")}</Button>
            {fileName && <span className="min-w-0 truncate text-caption text-ink-subtle">{fileName}</span>}
            {busy === "preview" && <span className="text-caption text-ink-subtle">{t("settings.people.previewing")}</span>}
          </div>
          {error && <ErrorBox message={error} />}
          {preview && (
            <>
              <div className="flex flex-wrap items-center gap-1.5">
                <Tag tone="success">{t("settings.people.action.create")} {preview.summary.create}</Tag>
                <Tag tone="accent">{t("settings.people.action.update")} {preview.summary.update}</Tag>
                {confirmRows.length > 0 && <Tag tone="warning">{t("settings.people.action.confirm")} {confirmRows.length}</Tag>}
                {(preview.summary.skip ?? 0) > 0 && <Tag>{t("settings.people.action.skip")} {preview.summary.skip}</Tag>}
                <Tag tone={preview.summary.invalid ? "danger" : "neutral"}>{t("settings.people.action.invalid")} {preview.summary.invalid}</Tag>
              </div>
              {confirmRows.length > 0 && <p className={cx("text-caption", undecided > 0 ? "text-warning" : "text-ink-subtle")} data-undecided-note>{undecided > 0 ? t("settings.people.csv.undecided", { n: undecided }) : t("settings.people.csv.allDecided")}</p>}
              {preview.rows.length === 0 ? <p className="text-body text-ink-subtle">{t("settings.people.importNothing")}</p> : (
                <div className="max-h-[360px] overflow-auto rounded-md border border-hairline">
                  <Table>
                    <thead><tr><th className="w-[48px]">{t("settings.people.line")}</th><th className="w-[72px]">{t("settings.people.importAction")}</th><th className="w-[110px]">{t("settings.members.name")}</th><th className="w-full">{t("common.email")}</th><th className="w-[120px]">{t("settings.members.team")}</th><th className="w-[140px]">{t("settings.members.roles")}</th></tr></thead>
                    <tbody>
                      {preview.rows.map((r) => (
                        <ImportRow key={r.line} row={r} decision={decisions[r.line]} onDecide={(d, id) => decide(r.line, d, id)} teamName={teamName} roleTitle={roleTitle} />
                      ))}
                    </tbody>
                  </Table>
                </div>
              )}
            </>
          )}
        </div>
      )}
    </Dialog>
  );
}

/** 预览里的一行；待确认的行下面多一行：可能是谁 + 三选一。 */
function ImportRow({ row: r, decision, onDecide, teamName, roleTitle }: { row: MemberImportRow; decision?: { decision: DirectoryDecision; local_id?: string }; onDecide: (d: DirectoryDecision, localId?: string) => void; teamName: (id: string | null, path: string) => string; roleTitle: (r: string) => string }) {
  const cands = r.candidates ?? [];
  const [picked, setPicked] = useState(cands[0]?.local_id ?? "");
  const cand = cands.find((c) => c.local_id === picked) ?? cands[0];
  const confirm = r.action === "confirm";
  const undecided = confirm && !decision;
  const rowCls = cx(r.action === "invalid" && "text-ink-subtle", undecided && "bg-warning-bg/40");
  return (
    <>
                        <tr className={rowCls} data-import-row={r.line} data-action={r.action} data-decision={decision?.decision}>
                          <td className="telemetry">{r.line}</td>
                          <td><Tag tone={ACTION_TONE[r.action]}>{t(`settings.people.action.${r.action}`)}</Tag></td>
                          <td className="max-w-[110px] truncate" title={r.name}>{r.name || <span className="text-ink-tertiary">—</span>}</td>
                          <td className="w-full max-w-0">
                            <span className="block truncate" title={r.email}>{r.email || <span className="text-ink-tertiary">—</span>}</span>
                            {r.reason && <span className={cx("block truncate text-caption", r.action === "invalid" ? "text-danger" : "text-ink-subtle")} title={r.reason}>{r.reason}</span>}
                          </td>
                          <td className="max-w-[120px] truncate" title={r.team_path}>{r.team_path ? teamName(r.team_id, r.team_path) : <span className="text-ink-tertiary">—</span>}</td>
                          <td className="max-w-[140px] truncate" title={r.roles.map(roleTitle).join("、")}>{r.roles.length ? r.roles.map(roleTitle).join("、") : <span className="text-ink-tertiary">—</span>}</td>
                        </tr>
      {confirm && cand && (
                        <tr className={cx("!border-t-0", rowCls)} data-import-confirm-row={r.line}>
                          <td />
                          <td colSpan={5} className="!pt-0">
                            <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 pb-1">
                              <span className="text-caption text-ink-subtle">{t("settings.people.csv.maybe")}</span>
                              {cands.length > 1 ? (
                                <Select value={picked} onChange={(e) => { setPicked(e.target.value); if (decision?.decision === "merge") onDecide("merge", e.target.value); }} aria-label={t("settings.directory.confirm.pickCandidate")} className="h-7 text-caption">
                                  {cands.map((c) => <option key={c.local_id} value={c.local_id}>{c.name} · {c.team_path} · {c.source_title}</option>)}
                                </Select>
                              ) : (
                                <span className="inline-flex flex-wrap items-center gap-1.5"><span className="font-medium text-ink">{cand.name}</span>{cand.team_path && <span className="text-caption text-ink-muted">· {cand.team_path}</span>}<Tag>{isSynced(cand) ? t("settings.directory.from", { name: cand.source_title }) : cand.source_title}</Tag></span>
                              )}
                              <span className="text-caption text-ink-subtle">{cand.reason_text}</span>
                              <Segmented size="sm" className="ml-auto" value={(decision?.decision ?? "") as DirectoryDecision | ""} onChange={(v) => v && onDecide(v, v === "merge" ? cand.local_id : undefined)} options={DECISIONS.map((d) => [d, t(`settings.directory.confirm.${d}` as const)] as [DirectoryDecision | "", string])} aria-label={t("settings.people.importAction")} />
                            </div>
                          </td>
                        </tr>
      )}
    </>
  );
}
