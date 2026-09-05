"use client";
import { useCallback, useMemo, useRef, useState } from "react";
import { api, OVERVIEW_PERIODS, type OverviewData, type OverviewPeriod, type OverviewUnit } from "@/lib/api";
import { fmtDate, fmtMoney } from "@/lib/format";
import { setQueryParams, useLoad, useQueryParam } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useFadeOnChange } from "@/lib/useFadeOnChange";
import { setScope, useScopeState } from "@/lib/useScope";
import { useSession } from "@/components/AppShell";
import { ScopeMoney } from "@/components/ScopePicker";
import { ExceptionsBlock } from "@/components/blocks/ExceptionsBlock";
import { TeamLoadBlock } from "@/components/blocks/TeamLoadBlock";
import { TrendBlock } from "@/components/blocks/TrendBlock";
import { IconChevronRight } from "@/components/icons";
import { AgentsTab } from "@/components/overview/AgentsTab";
import { CostTab } from "@/components/overview/CostTab";
import { EfficiencyTab } from "@/components/overview/EfficiencyTab";
import { Empty, ErrorBox, PageHeader, Panel, ProgressBar, Readout, Segmented, Table, TableSkeleton, Tabs, cx } from "@/components/ui";

const TABS = ["overview", "cost", "efficiency", "agents"] as const;
type Tab = (typeof TABS)[number];
const isTab = (v: unknown): v is Tab => typeof v === "string" && (TABS as readonly string[]).includes(v);

/*
 * 组织概览（ADR 0013、DESIGN.md §10）：四个页签，?tab=overview|cost|efficiency|agents，默认「概览」。
 *   概览：各组织单元对比、要我关注的异常、人员与 Agent 负荷、趋势与环比（全部按当前范围取数，与首页共用区块组件）；
 *   成本 / 效率 / Agent：原「运营总览」的四块拆到三个页签（/dashboard 跳到 ?tab=cost）。
 * 表格里点组织单元 = 把范围切到那一档往下看，页头的面包屑负责回到上一层。看不到财务数据的范围里，所有金额走 ScopeMoney 显示「—」。
 * 版面遵守 DESIGN.md 的空间铁律：不放装饰画面，信息铺满宽度，1280 起两列、1920 起表格更宽。
 */
export default function OverviewPage() {
  const { session } = useSession();
  const scope = useScopeState();
  const qTab = useQueryParam("tab");
  const tab: Tab = isTab(qTab) ? qTab : "overview";
  const switchTab = useCallback((k: Tab) => setQueryParams({ tab: k === "overview" ? null : k }), []);
  const [period, setPeriod] = useState<OverviewPeriod>("month");
  const bodyRef = useRef<HTMLDivElement>(null);
  useFadeOnChange(bodyRef, tab);

  // 面包屑：当前范围沿团队树往上的一串，点任意一层切回去
  const trail = useMemo(() => {
    const teams = session?.teams ?? [];
    const chain: Array<{ id: string; title: string }> = [];
    let cur = teams.find((x) => x.id === scope.scope);
    const seen = new Set<string>();
    while (cur && !seen.has(cur.id)) {
      chain.unshift({ id: cur.id, title: cur.name });
      seen.add(cur.id);
      cur = cur.parent_id ? teams.find((x) => x.id === cur!.parent_id) : undefined;
    }
    return chain;
  }, [session?.teams, scope.scope]);
  const canPickAll = scope.options.some((o) => o.id === "all");

  return (
    <div>
      <PageHeader
        title={t("overview.title")}
        description={t("overview.description")}
        breadcrumb={
          trail.length || canPickAll ? (
            <>
              {canPickAll &&
                (scope.scope === "all" ? (
                  <span className="text-ink">{t("scope.all")}</span>
                ) : (
                  <button type="button" className="hover:text-accent-hover" onClick={() => setScope("all")}>
                    {t("scope.all")}
                  </button>
                ))}
              {trail.map((x, i) => (
                <span key={x.id} className="flex items-center gap-1">
                  {(canPickAll || i > 0) && <IconChevronRight size={12} className="text-ink-tertiary" />}
                  {i === trail.length - 1 ? (
                    <span className="text-ink">{x.title}</span>
                  ) : (
                    <button type="button" className="hover:text-accent-hover" onClick={() => setScope(x.id)}>
                      {x.title}
                    </button>
                  )}
                </span>
              ))}
            </>
          ) : undefined
        }
      />
      <Tabs
        value={tab}
        label={t("overview.tabs")}
        items={TABS.map((k) => ({ key: k, label: t(`overview.tab.${k}`) }))}
        onChange={switchTab}
        actions={tab === "overview" ? <Segmented size="sm" value={period} options={OVERVIEW_PERIODS.map((k) => [k, t(`overview.period.${k}`)])} onChange={setPeriod} aria-label={t("overview.trend")} /> : undefined}
      />
      <div ref={bodyRef} data-tab={tab}>
        {tab === "cost" ? <CostTab /> : tab === "efficiency" ? <EfficiencyTab /> : tab === "agents" ? <AgentsTab /> : <SummaryTab period={period} currency={session?.organization.currency} scope={scope.scope} />}
      </div>
    </div>
  );
}

/** 概览页签：各组织单元对比 + 与首页共用的三个区块 */
function SummaryTab({ period, currency, scope }: { period: OverviewPeriod; currency?: string; scope: string }) {
  const overview = useLoad(() => api.stats.overview(period), [period]);
  const data = overview.data;
  return (
    <div>
      <div className="grid items-start gap-4">
        {/* 01 各组织单元对比 */}
        <Panel
          index={1}
          icon={<IconOverviewInline />}
          title={t("overview.units")}
          telemetry={data ? t("overview.range", { from: fmtDate(data.range.from), to: fmtDate(data.range.to) }) : undefined}
          padded={false}
        >
          {overview.loading && !data ? (
            <TableSkeleton rows={4} cols={7} />
          ) : overview.error ? (
            <div className="p-4">
              <ErrorBox message={overview.error} onRetry={overview.reload} />
            </div>
          ) : !data || data.units.length === 0 ? (
            <Empty text={t("overview.emptyUnits")} illustration={false} className="py-8" />
          ) : (
            <UnitsTable units={data.units} totals={data.totals} currency={currency} scope={scope} />
          )}
        </Panel>

        {/* 02 要我关注的异常 · 03 人员与 Agent 负荷 · 04 趋势与环比：与首页共用同一批区块组件 */}
        <div className="grid min-w-0 items-start gap-4 xl:grid-cols-2">
          <ExceptionsBlock index={2} noLink />
          <TeamLoadBlock index={3} noLink />
        </div>
        <TrendBlock index={4} period={period} noLink />
      </div>
    </div>
  );
}

/** 面板头的图标：和侧栏「组织概览」用同一枚。 */
function IconOverviewInline() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="2" y="2.5" width="5" height="5" rx="1" />
      <rect x="9" y="2.5" width="5" height="5" rx="1" />
      <rect x="2" y="9" width="5" height="4.5" rx="1" />
      <rect x="9" y="9" width="5" height="4.5" rx="1" />
    </svg>
  );
}

// ---------- 01 各组织单元对比 ----------

function UnitsTable({ units, totals, currency, scope }: { units: OverviewUnit[]; totals: OverviewData["totals"]; currency?: string; scope: string }) {
  return (
    <Table>
      <thead>
        <tr>
          <th className="w-full min-w-[160px]">{t("overview.unit")}</th>
          <th className="w-[132px]">{t("overview.goalProgress")}</th>
          <th className="num w-[92px]">{t("overview.tasks")}</th>
          <th className="num w-[72px]">{t("overview.overdue")}</th>
          <th className="num w-[112px]">{t("overview.throughput")}</th>
          <th className="num w-[168px]">{t("overview.cost")}</th>
          <th className="num w-[112px]">{t("overview.budget")}</th>
          <th className="num w-[128px]">{t("overview.budgetUsed")}</th>
        </tr>
      </thead>
      <tbody>
        {units.map((u) => (
          <UnitRow key={u.id} unit={u} currency={currency} drillable={u.kind === "team" && u.id !== scope} />
        ))}
        <tr className="bg-surface-2/60">
          <td className="font-medium text-ink">{t("overview.totals")}</td>
          <td>
            <ProgressAndPct value={totals.goal_progress} />
          </td>
          <td className="num whitespace-nowrap">
            {totals.tasks_done} / {totals.tasks_total}
          </td>
          <td className={cx("num", totals.overdue > 0 && "text-danger")}>{totals.overdue}</td>
          <td className="num whitespace-nowrap">
            <span className="inline-flex items-baseline gap-1.5">
              <Readout value={totals.throughput} />
              <Delta value={totals.throughput} prev={totals.prev.throughput} more="good" />
            </span>
          </td>
          <td className="num whitespace-nowrap">
            <span className="inline-flex items-baseline gap-1.5">
              <ScopeMoney value={totals.cost} currency={currency} />
              <Delta value={totals.cost} prev={totals.prev.cost} more="bad" currency={currency} money />
            </span>
          </td>
          <td className="num">
            <ScopeMoney value={totals.budget} currency={currency} />
          </td>
          <td className="num">
            <BudgetUsed pct={totals.budget_used_pct} />
          </td>
        </tr>
      </tbody>
    </Table>
  );
}

function UnitRow({ unit, currency, drillable }: { unit: OverviewUnit; currency?: string; drillable: boolean }) {
  const name = drillable ? (
    <button type="button" className="text-left text-ink hover:text-accent-hover" onClick={() => setScope(unit.id)} title={t("overview.drill", { name: unit.title })}>
      {unit.title}
    </button>
  ) : (
    <span className={unit.kind === "unassigned" ? "text-ink-subtle" : "text-ink"}>{unit.title}</span>
  );
  return (
    <tr>
      <td className="w-full max-w-0 min-w-[160px] truncate">{name}</td>
      <td>
        <ProgressAndPct value={unit.goal_progress} />
      </td>
      <td className="num whitespace-nowrap">
        {unit.tasks_done} / {unit.tasks_total}
      </td>
      <td className={cx("num", unit.overdue > 0 && "text-danger")}>{unit.overdue}</td>
      <td className="num whitespace-nowrap">
        <span className="inline-flex items-baseline gap-1.5">
          {unit.throughput}
          <Delta value={unit.throughput} prev={unit.prev.throughput} more="good" />
        </span>
      </td>
      <td className="num whitespace-nowrap">
        <span className="inline-flex items-baseline gap-1.5">
          <ScopeMoney value={unit.cost} currency={currency} />
          <Delta value={unit.cost} prev={unit.prev.cost} more="bad" currency={currency} money />
        </span>
      </td>
      <td className="num">
        <ScopeMoney value={unit.budget} currency={currency} />
      </td>
      <td className="num">
        <BudgetUsed pct={unit.budget_used_pct} />
      </td>
    </tr>
  );
}

function ProgressAndPct({ value }: { value: number }) {
  return (
    <span className="flex items-center gap-2">
      <ProgressBar value={value} className="w-20" tone={value >= 100 ? "success" : "accent"} />
      <span className="tabular-nums text-ink-muted">{Math.round(value)}%</span>
    </span>
  );
}

function BudgetUsed({ pct }: { pct: number | null }) {
  if (pct === null || pct === undefined) return <span className="text-ink-subtle">—</span>;
  const over = pct > 100;
  // 花掉一点点也别显示成 0%：小于 1% 时给「<1%」，看的人才知道有在花钱
  const text = pct > 0 && pct < 1 ? t("overview.underOnePct") : `${Math.round(pct)}%`;
  return (
    <span className="inline-flex items-center gap-1.5">
      <ProgressBar value={Math.min(100, pct)} className="w-10" tone={over ? "danger" : "accent"} />
      <span className={cx("tabular-nums", over ? "text-danger" : "text-ink-muted")}>{text}</span>
    </span>
  );
}

/** 环比：带正负号的数字，颜色按"变大是好事还是坏事"。看不到成本时不显示。 */
function Delta({ value, prev, more, money, currency }: { value: number | null; prev: number | null; more: "good" | "bad"; money?: boolean; currency?: string }) {
  const { financial } = useScopeState();
  // 看不到成本的范围里，成本的环比也不能露出来（后端按单元给的 cost 可能还在）
  if (money && !financial) return null;
  if (value === null || prev === null || value === undefined || prev === undefined) return null;
  const d = Math.round((value - prev) * 100) / 100;
  if (!d) return <span className="text-caption text-ink-tertiary">±0</span>;
  const up = d > 0;
  const good = more === "good" ? up : !up;
  const text = money ? `${up ? "+" : "−"}${fmtMoney(Math.abs(d), currency)}` : `${up ? "+" : "−"}${Math.abs(d)}`;
  return <span className={cx("text-caption tabular-nums", good ? "text-success" : "text-danger")}>{text}</span>;
}
