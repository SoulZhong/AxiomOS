"use client";
import { useState } from "react";
import { api, type CostGroup } from "@/lib/api";
import { fmtMoney, fmtTokens } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { financeHiddenText, useScopeState } from "@/lib/useScope";
import { BarList } from "@/components/BarList";
import { CostBudgetBlock } from "@/components/blocks/CostBudgetBlock";
import { IconUsage } from "@/components/icons";
import { Odometer } from "@/components/instruments/Odometer";
import { Sparkline } from "@/components/instruments/Sparkline";
import { Segmented, Skeleton } from "@/components/ui";
import { StatPanel, useDaySeries } from "./StatPanel";

const COST_GROUPS: CostGroup[] = ["goal", "team", "executor", "model"];

/** 组织概览 · 成本：按目标 / 团队 / 执行者 / 模型分组的成本（左），本月成本与预算执行（右，与首页「成本与预算」同一个区块）。 */
export function CostTab() {
  const { session } = useSession();
  const currency = session?.organization.currency;
  const [group, setGroup] = useState<CostGroup>("goal");
  // 这一档看不到财务时不去请求（会被后端按策略拒绝），直接给出和旁边区块一致的说明句
  const sc = useScopeState();
  const cost = useLoad(() => (sc.financial ? api.stats.cost(group) : Promise.resolve([])), [group, sc.financial]);
  const series = useDaySeries();
  const total = (cost.data ?? []).reduce((s, c) => s + c.cost, 0);
  return (
    <div className="grid items-start gap-4 xl:grid-cols-2">
      <StatPanel
        index={1}
        icon={<IconUsage />}
        title={t("dashboard.cost")}
        big={sc.financial && cost.data ? <Odometer value={fmtMoney(total, currency)} /> : null}
        caption={t("dashboard.costCaption")}
        aside={series ? <Sparkline points={series.cost} label={t("dashboard.costSpark")} /> : <Skeleton className="h-7 w-24" />}
        actions={<Segmented size="sm" value={group} options={COST_GROUPS.map((k) => [k, t(`dashboard.group.${k}`)])} onChange={setGroup} />}
        loading={cost.loading && !cost.data}
        error={sc.financial ? cost.error : null}
        onRetry={cost.reload}
      >
        {!sc.financial ? (
          <p className="py-6 text-center text-body text-ink-subtle">{financeHiddenText(sc)}</p>
        ) : (
        <BarList
          limit={8}
          items={(cost.data ?? []).map((c) => ({
            key: c.key,
            title: c.title,
            value: c.cost,
            display: <Odometer value={fmtMoney(c.cost, currency)} />,
            sub: <Odometer value={t("dashboard.costSub", { tokens: fmtTokens(c.total_tokens), runs: c.run_count })} />,
          }))}
        />
        )}
      </StatPanel>
      <CostBudgetBlock index={2} noLink />
    </div>
  );
}
