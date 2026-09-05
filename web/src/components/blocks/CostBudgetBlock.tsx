"use client";
import { api } from "@/lib/api";
import { fmtMoney } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { financeHiddenText, useScopeState } from "@/lib/useScope";
import { useSession } from "@/components/AppShell";
import { IconUsage } from "@/components/icons";
import { ScopeMoney } from "@/components/ScopePicker";
import { ListSkeleton, ProgressBar, Readout, cx } from "@/components/ui";
import { BlockPanel, type BlockProps } from "./BlockPanel";

const TOP = 5;

/*
 * 成本与预算：这一个月当前范围的成本（带环比）、预算与执行率，下面按目标列成本最高的几条。
 * 看不到财务数据的范围里整块只说一句原因（句子按组织的策略选，见 financeHiddenText）。
 */
export function CostBudgetBlock({ index, title, noLink }: BlockProps) {
  const { session } = useSession();
  const scope = useScopeState();
  const currency = session?.organization.currency;
  const overview = useLoad(() => api.stats.overview("month"), []);
  const byGoal = useLoad(() => (scope.financial ? api.stats.cost("goal") : Promise.resolve([])), [scope.financial]);
  const totals = overview.data?.totals;
  const goals = [...(byGoal.data ?? [])].filter((g) => g.cost > 0).sort((a, b) => b.cost - a.cost).slice(0, TOP);
  const maxCost = Math.max(...goals.map((g) => g.cost), 1);
  const delta = totals && totals.cost !== null && totals.prev.cost !== null ? Math.round((totals.cost - totals.prev.cost) * 100) / 100 : null;
  return (
    <BlockPanel
      index={index}
      noLink={noLink}
      icon={<IconUsage />}
      title={title ?? t("block.cost_budget")}
      telemetry={t("block.cost_budget.thisMonth")}
      href="/overview/?tab=cost"
      loading={overview.loading && !totals}
      error={overview.error}
      onRetry={overview.reload}
      skeleton={<ListSkeleton rows={4} />}
    >
      {!scope.financial ? (
        <p className="py-6 text-center text-body text-ink-subtle">{financeHiddenText(scope)}</p>
      ) : totals ? (
        <div>
          <div className="grid grid-cols-3 gap-4">
            <div className="min-w-0">
              <div className="eyebrow mb-1 text-ink-subtle">{t("overview.cost")}</div>
              <div className="flex items-baseline gap-1.5 text-title tabular-nums text-ink">
                <ScopeMoney value={totals.cost} currency={currency} />
                {delta !== null && delta !== 0 && <span className={cx("text-caption tabular-nums", delta > 0 ? "text-danger" : "text-success")}>{`${delta > 0 ? "+" : "−"}${fmtMoney(Math.abs(delta), currency)}`}</span>}
              </div>
            </div>
            <div className="min-w-0">
              <div className="eyebrow mb-1 text-ink-subtle">{t("overview.budget")}</div>
              <div className="text-title tabular-nums text-ink">
                <ScopeMoney value={totals.budget} currency={currency} />
              </div>
            </div>
            <div className="min-w-0">
              <div className="eyebrow mb-1 text-ink-subtle">{t("overview.budgetUsed")}</div>
              <div className="flex items-center gap-2 text-title tabular-nums text-ink">
                {totals.budget_used_pct === null ? (
                  <span className="text-ink-subtle">—</span>
                ) : (
                  <>
                    <Readout value={`${Math.round(totals.budget_used_pct)}%`} className={cx(totals.budget_used_pct > 100 && "text-danger")} />
                    <ProgressBar value={Math.min(100, totals.budget_used_pct)} className="w-14" tone={totals.budget_used_pct > 100 ? "danger" : "accent"} />
                  </>
                )}
              </div>
            </div>
          </div>
          <div className="mt-4 border-t border-hairline pt-3">
            <div className="mb-2 flex items-center justify-between">
              <span className="eyebrow text-ink-subtle">{t("block.cost_budget.byGoal")}</span>
              {byGoal.data && goals.length > 0 && <span className="text-caption text-ink-tertiary tabular-nums">{t("block.cost_budget.topN", { n: goals.length })}</span>}
            </div>
            {byGoal.loading && !byGoal.data ? (
              <ListSkeleton rows={3} />
            ) : goals.length === 0 ? (
              <p className="py-2 text-caption text-ink-subtle">{t("block.cost_budget.empty")}</p>
            ) : (
              <ul className="space-y-1.5">
                {goals.map((g) => (
                  <li key={g.key} className="grid grid-cols-[minmax(0,1fr)_96px_88px] items-center gap-3 text-body">
                    <span className="truncate text-ink" title={g.title}>
                      {g.title}
                    </span>
                    <ProgressBar value={(g.cost / maxCost) * 100} className="w-full" />
                    <span className="telemetry text-right tabular-nums text-ink-muted">{fmtMoney(g.cost, currency)}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      ) : null}
    </BlockPanel>
  );
}
