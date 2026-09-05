"use client";
import { useState, type FormEvent } from "react";
import { api, VISIBILITY_POLICIES, type OrgSettings, type VisibilityPolicy } from "@/lib/api";
import { errorMessage, useLoad } from "@/lib/hooks";
import { t, type Key } from "@/lib/i18n";
import { useToast } from "@/components/toast";
import { Button, ErrorBox, Field, ListSkeleton, Panel, Segmented, Select, Tag, cx } from "@/components/ui";

/*
 * 可见范围（ADR 0013）：两项策略 + 共享边界 + 「看看某个人能看到什么」预览。
 * 这两项决定组织里的人各自能看到多少数据，系统只给默认值，每个组织自己定；
 * 选到「按共享边界」时旁边直接给去团队页标记边界的入口，预览用来验证配得对不对。
 */
export function VisibilityTab({ onGoTeams }: { onGoTeams: () => void }) {
  const toast = useToast();
  const settings = useLoad(() => api.org.settings(), []);
  const [draft, setDraft] = useState<OrgSettings | null>(null);
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (settings.data && loadedFor !== JSON.stringify(settings.data)) {
    setLoadedFor(JSON.stringify(settings.data));
    setDraft({ ...settings.data });
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!draft) return;
    setBusy(true);
    setError(null);
    try {
      await api.org.updateSettings(draft);
      toast.ok(t("toast.saved"));
      settings.reload();
    } catch (err) {
      const m = errorMessage(err);
      setError(m);
      toast.fail(m);
    } finally {
      setBusy(false);
    }
  };

  if (settings.loading && !settings.data) return <Panel title={t("settings.tab.visibility")}><ListSkeleton rows={3} /></Panel>;
  if (settings.error && !settings.data) return <ErrorBox message={settings.error} onRetry={settings.reload} />;

  return (
    <div className="space-y-4">
      <Panel index={1} title={t("settings.tab.visibility")}>
        <p className="mb-4 max-w-[640px] text-body text-ink-subtle">{t("settings.visibility.description")}</p>
        <form onSubmit={submit} className="max-w-[640px] space-y-5">
          <PolicyField
            label={t("settings.visibility.collaboration")}
            hint={t("settings.visibility.collaborationHint")}
            kind="collaboration"
            value={draft?.collaboration_visibility ?? "org"}
            onChange={(v) => setDraft((d) => (d ? { ...d, collaboration_visibility: v } : d))}
            onGoTeams={onGoTeams}
          />
          <PolicyField
            label={t("settings.visibility.finance")}
            hint={t("settings.visibility.financeHint")}
            kind="finance"
            value={draft?.finance_visibility ?? "team_tree"}
            onChange={(v) => setDraft((d) => (d ? { ...d, finance_visibility: v } : d))}
            onGoTeams={onGoTeams}
          />
          <p className="text-caption text-ink-subtle">{t("settings.visibility.exception")}</p>
          {error && <p className="text-caption text-danger" role="alert">{error}</p>}
          <div>
            <Button type="submit" variant="primary" disabled={busy || !draft}>{busy ? t("common.saving") : t("common.save")}</Button>
          </div>
        </form>
      </Panel>

      <PreviewPanel settings={settings.data ?? null} />
    </div>
  );
}

function PolicyField({ label, hint, kind, value, onChange, onGoTeams }: { label: string; hint: string; kind: "collaboration" | "finance"; value: VisibilityPolicy; onChange: (v: VisibilityPolicy) => void; onGoTeams: () => void }) {
  return (
    <Field as="div" label={label} hint={hint}>
      <Segmented value={value} options={VISIBILITY_POLICIES.map((p) => [p, t(`policy.${p}` as Key)])} onChange={onChange} aria-label={label} />
      <p className="mt-2 text-caption text-ink-muted">{t(`policy.${kind}.${value}` as Key)}</p>
      {value === "boundary" && (
        <p className="mt-1 text-caption text-ink-subtle">
          {t("settings.visibility.boundaryHint")}{" "}
          <button type="button" className="text-accent hover:text-accent-hover" onClick={onGoTeams}>
            {t("settings.visibility.toTeams")}
          </button>
        </p>
      )}
    </Field>
  );
}

/** 「看看某个人能看到什么」：选一个成员，列出他能看到的团队并说明依据。 */
function PreviewPanel({ settings }: { settings: OrgSettings | null }) {
  const members = useLoad(() => api.org.members(), []);
  const [memberId, setMemberId] = useState("");
  const preview = useLoad(() => (memberId ? api.org.visibilityPreview(memberId) : Promise.resolve(null)), [memberId]);
  const data = preview.data;

  // 依据：先看策略，再看他是不是跨域的例外
  const basis = (): string => {
    if (!data) return "";
    const policy = data.collaboration_visibility ?? settings?.collaboration_visibility ?? "org";
    if (policy === "org") return t("settings.visibility.basisOrg");
    if (data.sees_all_work) return t("settings.visibility.basisAll");
    if (policy === "team_tree") return t("settings.visibility.basisTeamTree");
    const b = data.teams.find((x) => data.boundaries.includes(x.id));
    return b ? t("settings.visibility.basisBoundary", { name: b.name }) : t("settings.visibility.basisOrg");
  };
  const workTeams = data ? data.teams.filter((x) => x.work) : [];
  const financeTeams = data ? data.teams.filter((x) => x.finance) : [];

  return (
    <Panel index={2} title={t("settings.visibility.preview")}>
      <p className="mb-3 max-w-[640px] text-body text-ink-subtle">{t("settings.visibility.previewHint")}</p>
      <Field as="div" label={t("settings.visibility.previewMember")} className="max-w-[280px]">
        <Select value={memberId} onChange={(e) => setMemberId(e.target.value)}>
          <option value="">{t("settings.visibility.previewPick")}</option>
          {(members.data ?? []).filter((m) => m.active).map((m) => (
            <option key={m.id} value={m.id}>{m.name}</option>
          ))}
        </Select>
      </Field>
      {preview.error && <div className="mt-3"><ErrorBox message={preview.error} onRetry={preview.reload} /></div>}
      {data && (
        <div className="mt-4 space-y-2 border-t border-hairline pt-3">
          <div className="eyebrow text-ink-subtle">{t("settings.visibility.previewTeams")}</div>
          {workTeams.length ? (
            <div className="flex flex-wrap gap-1.5">
              {workTeams.map((x) => (
                <Tag key={x.id} tone={x.mine ? "accent" : "neutral"} title={x.is_boundary ? t("settings.teams.boundaryTag") : undefined}>
                  {"　".repeat(x.depth)}{x.name}
                </Tag>
              ))}
            </div>
          ) : (
            <p className="text-body text-ink-subtle">{t("settings.visibility.previewNone")}</p>
          )}
          <div className="eyebrow pt-1 text-ink-subtle">{t("settings.visibility.previewFinanceTeams")}</div>
          {financeTeams.length ? (
            <div className="flex flex-wrap gap-1.5">
              {financeTeams.map((x) => (
                <Tag key={x.id} tone="success">{x.name}</Tag>
              ))}
            </div>
          ) : (
            <p className={cx("text-caption text-ink-subtle")}>{t("settings.visibility.previewNoFinance")}</p>
          )}
          <p className="text-caption text-ink-muted">{basis()}</p>
        </div>
      )}
    </Panel>
  );
}
