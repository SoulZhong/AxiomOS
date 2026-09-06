import type { ComponentType } from "react";
import { BLOCK_SIZE_DEFAULTS, type BlockHeight, type BlockKey, type BlockWidth } from "@/lib/api";
import type { BlockProps } from "./BlockPanel";
import { BacklogBlock } from "./BacklogBlock";
import { CostBudgetBlock } from "./CostBudgetBlock";
import { EventsBlock } from "./EventsBlock";
import { ExceptionsBlock } from "./ExceptionsBlock";
import { InboxBlock } from "./InboxBlock";
import { MyReviewBlock } from "./MyReviewBlock";
import { MyTasksBlock } from "./MyTasksBlock";
import { OverviewSummaryBlock } from "./OverviewSummaryBlock";
import { ReadoutsBlock } from "./ReadoutsBlock";
import { SprintBlock } from "./SprintBlock";
import { TeamLoadBlock } from "./TeamLoadBlock";
import { TrendBlock } from "./TrendBlock";

export interface BlockEntry {
  component: ComponentType<BlockProps>;
  /** 目录接口没给 min_w 时的兜底：编辑器不允许选到比这更窄（表格类 6 栏，其余 4 栏） */
  minW: BlockWidth;
  /** 目录接口没给 default_w / default_h 时的兜底 */
  defaultW: BlockWidth;
  defaultH: BlockHeight;
}

const entry = (key: BlockKey, component: ComponentType<BlockProps>): BlockEntry => ({ component, minW: BLOCK_SIZE_DEFAULTS[key].minW, defaultW: BLOCK_SIZE_DEFAULTS[key].w, defaultH: BLOCK_SIZE_DEFAULTS[key].h });

/**
 * 区块键 → 组件与尺寸兜底（ADR 0015 补记二）。区块集合是系统定义的，这里是前端唯一的清单：
 * 「我的工作」按 GET /workspace 给的顺序与宽高从这里取组件排进 12 栏网格（WorkspaceGrid）。
 * `inbox`（待我处理）与 `readouts`（组织概况）自补记四起也是区块，默认在最上面、最窄 6 栏，和别的区块一样可以拖动、改大小、移除。
 * `proposals` 已不在目录里（DESIGN.md §12，被「待我处理」覆盖）：老布局带着它时这里查不到，页面直接跳过。
 */
export const blockRegistry: Record<BlockKey, BlockEntry> = {
  inbox: entry("inbox", InboxBlock),
  readouts: entry("readouts", ReadoutsBlock),
  overview_summary: entry("overview_summary", OverviewSummaryBlock),
  exceptions: entry("exceptions", ExceptionsBlock),
  my_tasks: entry("my_tasks", MyTasksBlock),
  my_review: entry("my_review", MyReviewBlock),
  team_load: entry("team_load", TeamLoadBlock),
  cost_budget: entry("cost_budget", CostBudgetBlock),
  events: entry("events", EventsBlock),
  sprint: entry("sprint", SprintBlock),
  backlog: entry("backlog", BacklogBlock),
  trend: entry("trend", TrendBlock),
};
