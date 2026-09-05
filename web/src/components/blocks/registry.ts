import type { ComponentType } from "react";
import type { BlockKey } from "@/lib/api";
import type { BlockProps } from "./BlockPanel";
import { BacklogBlock } from "./BacklogBlock";
import { CostBudgetBlock } from "./CostBudgetBlock";
import { EventsBlock } from "./EventsBlock";
import { ExceptionsBlock } from "./ExceptionsBlock";
import { MyReviewBlock } from "./MyReviewBlock";
import { MyTasksBlock } from "./MyTasksBlock";
import { OverviewSummaryBlock } from "./OverviewSummaryBlock";
import { SprintBlock } from "./SprintBlock";
import { TeamLoadBlock } from "./TeamLoadBlock";
import { TrendBlock } from "./TrendBlock";

/** 区块在两列网格里占多宽：full = 通栏（表格、趋势），half = 半栏（≥1280 时并排）。 */
export type BlockSpan = "full" | "half";

export interface BlockEntry {
  component: ComponentType<BlockProps>;
  span: BlockSpan;
}

/**
 * 区块键 → 组件与默认栏宽（ADR 0015）。区块集合是系统定义的，这里是前端唯一的清单：
 * 「我的工作」按 GET /workspace 给的顺序从这里取组件；组织概览页也从这里取同一批组件。
 * `proposals` 已不在目录里（DESIGN.md §12，被「待我处理」覆盖）：老布局带着它时这里查不到，页面直接跳过。
 */
export const blockRegistry: Record<BlockKey, BlockEntry> = {
  overview_summary: { component: OverviewSummaryBlock, span: "full" },
  exceptions: { component: ExceptionsBlock, span: "half" },
  my_tasks: { component: MyTasksBlock, span: "full" },
  my_review: { component: MyReviewBlock, span: "full" },
  team_load: { component: TeamLoadBlock, span: "half" },
  cost_budget: { component: CostBudgetBlock, span: "half" },
  events: { component: EventsBlock, span: "half" },
  sprint: { component: SprintBlock, span: "half" },
  backlog: { component: BacklogBlock, span: "half" },
  trend: { component: TrendBlock, span: "full" },
};
