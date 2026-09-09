"use client";
import Link from "next/link";
import { isGoalPlan, type GoalPlanPayload, type Proposal } from "@/lib/api";
import { t } from "@/lib/i18n";
import { proposalFieldTitle, proposalTargetHref } from "@/lib/terms";
import { Code } from "@/components/ui";

/*
 * 待确认操作「要确认的到底是什么」：后端那句「确认后会……」只说动作，不说内容。这里按动作把载荷里
 * 人最该看的那几样摆出来——推进任务的说明、交付物的标题与地址、评论的正文、方案的规模、改了哪些字段——
 * 待我处理的行与待确认操作页都用它。认不出的动作什么也不显示，不塞 JSON 给人读。
 */
const str = (v: unknown) => (typeof v === "string" ? v.trim() : "");

export function ProposalDetails({ p, className }: { p: Proposal; className?: string }) {
  const pl = (p.payload ?? {}) as Record<string, unknown>;
  const rows: Array<[string, React.ReactNode]> = [];
  const push = (label: string, node: React.ReactNode) => { if (node) rows.push([label, node]); };
  const text = (v: unknown) => { const s = str(v); return s ? <span className="whitespace-pre-wrap">{s}</span> : null; };
  const link = (v: unknown) => {
    const s = str(v);
    if (!s) return null;
    return /^https?:\/\//.test(s) ? <a href={s} target="_blank" rel="noreferrer" className="text-accent hover:text-accent-hover break-all">{s}</a> : <Code className="text-caption break-all">{s}</Code>;
  };
  switch (p.action) {
    case "task.transition":
      push(t("proposals.detail.comment"), text(pl.comment));
      break;
    case "task.artifact": {
      const title = str(pl.title);
      push(t("proposals.detail.artifact"), title ? <>{title}{link(pl.ref) ? <> · {link(pl.ref)}</> : null}</> : link(pl.ref));
      break;
    }
    case "task.comment":
    case "task.note":
    case "goal.note":
      push(t("proposals.detail.text"), text(pl.text));
      break;
    case "task.external_link":
      push(t("proposals.detail.link"), <>{str(pl.title) && <>{str(pl.title)} · </>}{link(pl.url)}</>);
      break;
    case "task.create":
    case "task.create_subtask":
    case "goal.create":
      push(t("proposals.detail.title"), text(pl.title));
      push(t("proposals.detail.description"), text(pl.description));
      break;
    case "task.update":
    case "goal.update": {
      const input = (p.action === "goal.update" ? (pl.input as Record<string, unknown> | undefined) : pl) ?? {};
      const fields = Object.entries(input).filter(([k, v]) => v !== null && v !== undefined && !["task_id", "goal_id", "expect_status", "clear"].includes(k)).map(([k]) => proposalFieldTitle(k));
      push(t("proposals.detail.fields"), fields.length ? fields.join("、") : null);
      break;
    }
    case "goal.plan": {
      const plan = pl as unknown as GoalPlanPayload;
      const href = "/proposals/";
      push(t("proposals.detail.plan"), <>{t("proposals.detail.planSize", { tasks: plan.tasks?.length ?? 0, milestones: plan.milestones?.length ?? 0 })} · <Link href={href} className="text-accent hover:text-accent-hover">{t("proposals.detail.planReview")}</Link></>);
      push(t("proposals.plan.rationale"), text(plan.rationale));
      break;
    }
    default:
      break;
  }
  const target = p.target && proposalTargetHref(p.target);
  if (!rows.length && !target) return null;
  return (
    <dl className={className}>
      {rows.map(([k, v]) => (
        <div key={k} className="flex gap-2 text-caption">
          <dt className="shrink-0 text-ink-subtle">{k}</dt>
          <dd className="min-w-0 text-ink-muted">{v}</dd>
        </div>
      ))}
      {p.target && target && !isGoalPlan(p) && (
        <div className="flex gap-2 text-caption">
          <dt className="shrink-0 text-ink-subtle">{t("proposals.targetLabel")}</dt>
          <dd className="min-w-0"><Link href={target} className="text-ink-muted hover:text-accent-hover">{p.target.title}</Link></dd>
        </div>
      )}
    </dl>
  );
}
