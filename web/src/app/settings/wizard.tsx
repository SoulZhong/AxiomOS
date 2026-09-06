"use client";
import { useState, type ReactNode } from "react";
import type { DirectoryCheck, DirectoryField, DirectoryProviderInfo } from "@/lib/api";
import { t, type Key } from "@/lib/i18n";
import { IconCheck, IconExternal } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Empty, ErrorBox, Field, Input, ListSkeleton, Tag, cx, type Tone } from "@/components/ui";

/*
 * 接入向导的公共零件（DESIGN.md §14「接入向导」、§22）：IM 集成与代码平台走同一套提供方注册表，
 * 所以也走同一套界面——一步一行的步骤条、提供方卡片、凭据字段、检查清单。
 * 这个文件里没有任何平台名，也没有任何"这一步该干什么"的判断：那些留在各自的页签里。
 */
export type StepState = "done" | "current" | "upcoming";
/** 检查清单的最小形状：IM 集成与代码平台各自的清单都满足它 */
export interface ChecklistLike { checks: DirectoryCheck[]; console_url?: string }
export interface ChecklistState<T extends ChecklistLike = ChecklistLike> { data: T | null; error: string | null; loading: boolean }

/**
 * 一步一行：左边序号圆点（做完 ✓ 绿、当前强调色、还没到灰），右边标题 + 一行摘要 + 右侧动作；展开时内容在下面。
 * 步与步之间一根细竖线，读起来是"从上到下的一条路"。
 */
export function StepRow({ n, title, state, warn, summary, action, open, children, last }: { n: number; title: string; state: StepState; warn?: boolean; summary?: ReactNode; action?: ReactNode; open: boolean; children: ReactNode; last?: boolean }) {
  const dot = state === "done"
    ? warn ? "border-warning-border bg-warning-bg text-warning" : "border-success-border bg-success-bg text-success"
    : state === "current" ? "border-accent bg-accent text-on-accent" : "border-hairline bg-surface-2 text-ink-subtle";
  return (
    <li className={cx("relative pl-10", !last && "pb-5 before:absolute before:bottom-0 before:left-[11px] before:top-7 before:w-px before:bg-hairline before:content-['']")} data-step={n} data-state={state} aria-current={state === "current" ? "step" : undefined}>
      <span className={cx("absolute left-0 top-0 inline-flex h-6 w-6 items-center justify-center rounded-full border font-mono text-[11px] font-medium", dot)} aria-hidden="true">
        {state === "done" ? (warn ? "!" : <IconCheck size={12} />) : n}
      </span>
      <div className="flex min-h-6 items-center gap-3">
        <h3 className={cx("shrink-0 text-body font-medium", state === "upcoming" ? "text-ink-subtle" : "text-ink")}>{title}</h3>
        {summary && <span className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 truncate text-body text-ink-muted">{summary}</span>}
        {action && <span className="ml-auto shrink-0">{action}</span>}
      </div>
      {open && <div className="mt-3">{children}</div>}
    </li>
  );
}

/** 检查那一步的一行摘要：「6 项检查全部通过」或「4 项通过 · 1 项阻塞 · 1 项待处理」 */
export function CheckSummary({ checks }: { checks: DirectoryCheck[] }) {
  const ok = checks.filter((c) => c.status === "ok").length;
  const blocked = checks.filter((c) => c.status === "blocked").length;
  const todo = checks.filter((c) => c.status === "todo").length;
  if (checks.length > 0 && ok === checks.length) return <span className="text-success">{t("settings.directory.wizard.allOk", { n: checks.length })}</span>;
  return (
    <>
      <span>{t("settings.directory.wizard.passed", { n: ok })}</span>
      {blocked > 0 && <><span aria-hidden="true">·</span><span className="text-danger">{t("settings.directory.wizard.blockedN", { n: blocked })}</span></>}
      {todo > 0 && <><span aria-hidden="true">·</span><span className="text-warning">{t("settings.directory.wizard.todoN", { n: todo })}</span></>}
    </>
  );
}

/** 一排提供方卡片：标题、前置条件、「选择」。选中的那张换成强调色边框与「已选择」。 */
export function ProviderCards({ providers, picked, onPick, tip = false }: { providers: DirectoryProviderInfo[]; picked: string | null; onPick: (key: string) => void; /** 卡片上带一句「到哪儿去找凭据」与「打开控制台」 */ tip?: boolean }) {
  if (!providers.length) return <Empty text={t("settings.directory.noProviders")} illustration={false} className="py-6" />;
  return (
    <ul className="grid gap-3 sm:grid-cols-2" role="list">
      {providers.map((p) => {
        const on = p.key === picked;
        return (
          <li key={p.key} className={cx("flex flex-col rounded-md border bg-surface-2 p-4", on ? "border-accent shadow-[inset_0_0_0_1px_var(--c-accent)]" : "border-hairline")} data-provider={p.key} aria-current={on ? "true" : undefined}>
            <div className="flex items-center gap-2">
              <span className="text-title font-medium">{p.title}</span>
              {on && <Tag tone="accent">{t("settings.directory.picked")}</Tag>}
            </div>
            {p.prerequisites.length > 0 && (
              <>
                <div className="eyebrow mt-3 text-ink-subtle">{t("settings.directory.prerequisites")}</div>
                <ol className="mt-1 list-decimal space-y-1 pl-5 text-body text-ink-muted">
                  {p.prerequisites.map((x, i) => <li key={i}>{x}</li>)}
                </ol>
              </>
            )}
            {tip && p.tip && (
              <p className="mt-3 text-caption text-ink-subtle" data-provider-tip>
                {p.tip.text}
                {p.tip.url && <> <WizardLink href={p.tip.url}>{t("settings.directory.openConsole", { name: p.title })}</WizardLink></>}
              </p>
            )}
            <div className="mt-auto pt-4">
              <Button variant={on ? "default" : "primary"} disabled={on} onClick={() => onPick(p.key)} aria-label={t("settings.directory.pickAria", { name: p.title })}>{on ? t("settings.directory.picked") : t("settings.directory.pick")}</Button>
            </div>
          </li>
        );
      })}
    </ul>
  );
}

export function WizardLink({ href, children, className }: { href: string; children: ReactNode; className?: string }) {
  return (
    <a href={href} target="_blank" rel="noreferrer noopener" className={cx("inline-flex items-center gap-1 text-accent underline-offset-2 hover:underline", className)}>
      {children}<IconExternal size={12} aria-hidden="true" />
    </a>
  );
}

/** 一个凭据字段。保密字段不回传：已设置时只显示「已设置 ••••」，想换才点「重设」，改主意点「不改了」。选填的在标题后标「选填」。 */
export function CredentialField({ field: f, value, onChange, isSet, resetting, onReset }: { field: DirectoryField; value: string; onChange: (v: string) => void; isSet: boolean; resetting: boolean; onReset: (on: boolean) => void }) {
  const label = f.optional ? <span className="inline-flex items-center gap-1.5">{f.title}<span className="text-caption font-normal text-ink-subtle">{t("settings.code.optional")}</span></span> : f.title;
  if (!f.secret) {
    return <Field label={label} hint={f.hint || undefined}><Input value={value} onChange={(e) => onChange(e.target.value)} placeholder={f.placeholder || undefined} autoComplete="off" spellCheck={false} /></Field>;
  }
  const hint = resetting && isSet ? t("settings.directory.secretNewHint", { title: f.title }) : f.hint || t("settings.directory.secretHint");
  return (
    <Field as="div" label={label} hint={hint}>
      {resetting ? (
        <span className="flex items-center gap-2">
          <Input type="password" value={value} onChange={(e) => onChange(e.target.value)} placeholder={f.placeholder || undefined} autoComplete="new-password" aria-label={f.title} />
          {isSet && <Button size="sm" onClick={() => onReset(false)}>{t("settings.directory.keepSecret")}</Button>}
        </span>
      ) : (
        <span className="flex h-8 items-center gap-2">
          <Tag tone="success">{t("settings.directory.secretSet")}</Tag>
          <span className="telemetry text-ink-subtle">••••••••••••</span>
          <Button size="sm" onClick={() => onReset(true)}>{t("settings.directory.resetSecret")}</Button>
        </span>
      )}
    </Field>
  );
}

const CHECK_DOT: Record<DirectoryCheck["status"], string> = { ok: "bg-success", todo: "bg-warning", blocked: "bg-danger", skipped: "bg-neutral" };
const CHECK_TONE: Record<DirectoryCheck["status"], Tone> = { ok: "success", todo: "warning", blocked: "danger", skipped: "neutral" };

/**
 * 接入检查清单：每项一行——状态点、标题、一句"怎么做"、「打开{平台}控制台」（直达那一页）、「详情」（代号只在这里）。
 * 黄色（不拦着往下走）的项可以「先跳过」，行还留着；底部「我已处理，重新检查」重新拉一次清单。
 */
export function ChecklistStep<T extends ChecklistLike>({ cl, providerTitle, skipped, canSkip, lead, onSkip, onSkipAll, onRecheck }: { cl: ChecklistState<T>; providerTitle: string; skipped: string[]; canSkip: boolean; /** 清单上方那一句（阻塞 / 待处理 / 全通过三种情况） */ lead?: (kind: "blocked" | "todo" | "ok") => string; onSkip?: (key: string) => void; onSkipAll?: () => void; onRecheck: () => Promise<T | null> }) {
  const toast = useToast();
  const data = cl.data;
  if (!data && cl.loading) return <div className="max-w-[640px]"><ListSkeleton rows={4} /></div>;
  if (!data) return <ErrorBox message={cl.error ?? t("settings.directory.wizard.checkError")} onRetry={() => void onRecheck()} className="max-w-[640px]" />;
  const blocked = data.checks.filter((c) => c.status === "blocked");
  const pending = data.checks.filter((c) => c.status === "todo" && !skipped.includes(c.key));
  const allOk = data.checks.length > 0 && data.checks.every((c) => c.status === "ok");
  const say = lead ?? ((k: "blocked" | "todo" | "ok") => (k === "ok" ? t("settings.directory.wizard.checkLeadOk") : k === "blocked" ? t("settings.directory.wizard.checkLead", { name: providerTitle }) : t("settings.directory.wizard.checkLeadTodo", { name: providerTitle })));
  const recheck = async () => {
    const d = await onRecheck();
    if (!d) return;
    toast.ok(d.checks.every((c) => c.status === "ok") ? t("settings.directory.wizard.recheckOk") : t("settings.directory.wizard.rechecked"));
  };
  return (
    <div className="max-w-[640px]">
      <p className="mb-3 text-body text-ink-muted">{say(allOk ? "ok" : blocked.length > 0 ? "blocked" : "todo")}</p>
      {cl.error && <p className="mb-2 text-caption text-danger" role="alert">{cl.error}</p>}
      <ul className={cx("divide-y divide-hairline rounded-md border border-hairline bg-surface-1", cl.loading && "opacity-60 transition-opacity")} aria-busy={cl.loading || undefined} data-checklist>
        {data.checks.map((c) => (
          <CheckRow key={c.key} check={c} skipped={skipped.includes(c.key)} consoleUrl={data.console_url} providerTitle={providerTitle} onSkip={canSkip && onSkip && c.status === "todo" && !c.blocking && !skipped.includes(c.key) ? () => onSkip(c.key) : undefined} />
        ))}
      </ul>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button variant={blocked.length > 0 ? "primary" : "default"} disabled={cl.loading} onClick={() => void recheck()}>{cl.loading ? t("settings.directory.wizard.checking") : t("settings.directory.wizard.recheck")}</Button>
        {canSkip && onSkipAll && blocked.length === 0 && pending.length > 0 && <Button variant="primary" onClick={onSkipAll}>{t("settings.directory.wizard.skipAll")}</Button>}
      </div>
    </div>
  );
}

export function CheckRow({ check: c, skipped, consoleUrl, providerTitle, onSkip }: { check: DirectoryCheck; skipped: boolean; consoleUrl?: string; providerTitle: string; onSkip?: () => void }) {
  const [showDetail, setShowDetail] = useState(false);
  const url = c.fix_url || consoleUrl;
  const statusTitle = t(`settings.directory.wizard.status.${c.status}` as Key);
  return (
    <li className="flex items-start gap-3 px-3 py-2.5" data-check={c.key} data-status={c.status}>
      <span className={cx("mt-[7px] h-2 w-2 shrink-0 rounded-full", CHECK_DOT[c.status])} role="img" aria-label={statusTitle} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className={cx("text-body font-medium", c.status === "skipped" ? "text-ink-subtle" : "text-ink")}>{c.title}</span>
          {c.status !== "ok" && <Tag tone={CHECK_TONE[c.status]}>{skipped && c.status === "todo" ? t("settings.directory.wizard.skipped") : statusTitle}</Tag>}
        </div>
        {c.fix && c.status !== "ok" && <p className="mt-0.5 text-body text-ink-muted">{c.fix}</p>}
        {(url || c.detail) && c.status !== "skipped" && (
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-caption">
            {url && c.status !== "ok" && <WizardLink href={url}>{t("settings.directory.openConsole", { name: providerTitle })}</WizardLink>}
            {c.detail && (
              <button type="button" className="text-ink-subtle underline-offset-2 hover:text-ink hover:underline" aria-expanded={showDetail} onClick={() => setShowDetail((v) => !v)}>
                {t("settings.directory.wizard.detail")}
              </button>
            )}
          </div>
        )}
        {showDetail && c.detail && <p className="mt-1.5 rounded-sm bg-surface-2 px-2 py-1 text-caption text-ink-muted break-words">{c.detail}</p>}
        {!showDetail && c.status === "skipped" && c.detail && <p className="mt-0.5 text-caption text-ink-subtle">{c.detail}</p>}
      </div>
      {onSkip && <Button size="sm" variant="ghost" className="shrink-0" onClick={onSkip}>{t("settings.directory.wizard.skip")}</Button>}
    </li>
  );
}
