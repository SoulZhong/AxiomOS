import type { ReactElement, SVGProps } from "react";

/**
 * 唯一的一套线性图标：16px 画布、1.5px 描边、圆头圆角。
 * 图标只做辅助，永远配文字；不用 emoji。
 * 文件末尾是两枚品牌线稿：轨道舰徽（ShipMark，24 / 48 / 64）与空状态的雷达扫描（RadarIllustration）。
 */
type IconProps = SVGProps<SVGSVGElement> & { size?: number };

function Svg({ size = 16, children, ...rest }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false" {...rest}>
      {children}
    </svg>
  );
}

export const IconHome = (p: IconProps) => (
  <Svg {...p}>
    <path d="M2.5 7.5 8 3l5.5 4.5V13a.5.5 0 0 1-.5.5H3a.5.5 0 0 1-.5-.5z" />
    <path d="M6.5 13.5V9.5h3v4" />
  </Svg>
);
export const IconTarget = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="8" cy="8" r="5.5" />
    <circle cx="8" cy="8" r="2.5" />
    <path d="M8 2.5V1M8 15v-1.5M2.5 8H1M15 8h-1.5" />
  </Svg>
);
export const IconList = (p: IconProps) => (
  <Svg {...p}>
    <path d="M5.5 4h8M5.5 8h8M5.5 12h8" />
    <circle cx="2.75" cy="4" r=".75" fill="currentColor" stroke="none" />
    <circle cx="2.75" cy="8" r=".75" fill="currentColor" stroke="none" />
    <circle cx="2.75" cy="12" r=".75" fill="currentColor" stroke="none" />
  </Svg>
);
export const IconInbox = (p: IconProps) => (
  <Svg {...p}>
    <path d="M2.5 9.5V12a1.5 1.5 0 0 0 1.5 1.5h8a1.5 1.5 0 0 0 1.5-1.5V9.5" />
    <path d="M2.5 9.5h3l1 1.5h3l1-1.5h3" />
    <path d="M4 9.5 5.5 3.5h5L12 9.5" />
  </Svg>
);
export const IconGantt = (p: IconProps) => (
  <Svg {...p}>
    <path d="M2.5 2.5v11h11" />
    <path d="M5 5.5h5M7 8.5h6M5 11.5h3" />
  </Svg>
);
export const IconBot = (p: IconProps) => (
  <Svg {...p}>
    <rect x="2.5" y="5" width="11" height="8.5" rx="1.5" />
    <path d="M8 5V2.5M5.5 2.5h5" />
    <circle cx="5.75" cy="9" r=".75" fill="currentColor" stroke="none" />
    <circle cx="10.25" cy="9" r=".75" fill="currentColor" stroke="none" />
    <path d="M6 11.5h4" />
  </Svg>
);
export const IconChart = (p: IconProps) => (
  <Svg {...p}>
    <path d="M2.5 13.5h11" />
    <path d="M4 13.5v-4M7 13.5V6M10 13.5V8.5M13 13.5V3.5" />
  </Svg>
);
/** 组织概览：四块并排的面板（通用图标，不承载类比）。 */
export const IconOverview = (p: IconProps) => (
  <Svg {...p}>
    <rect x="2" y="2.5" width="5" height="5" rx="1" />
    <rect x="9" y="2.5" width="5" height="5" rx="1" />
    <rect x="2" y="9" width="5" height="4.5" rx="1" />
    <rect x="9" y="9" width="5" height="4.5" rx="1" />
  </Svg>
);
export const IconFlow = (p: IconProps) => (
  <Svg {...p}>
    <rect x="1.5" y="6" width="4" height="4" rx="1" />
    <rect x="10.5" y="2.5" width="4" height="4" rx="1" />
    <rect x="10.5" y="9.5" width="4" height="4" rx="1" />
    <path d="M5.5 8h2.5V4.5h2.5M8 8v3.5h2.5" />
  </Svg>
);
export const IconSettings = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="8" cy="8" r="2" />
    <path d="M8 1.75v1.5M8 12.75v1.5M1.75 8h1.5M12.75 8h1.5M3.58 3.58l1.06 1.06M11.36 11.36l1.06 1.06M3.58 12.42l1.06-1.06M11.36 4.64l1.06-1.06" />
  </Svg>
);
export const IconPlus = (p: IconProps) => (
  <Svg {...p}>
    <path d="M8 3v10M3 8h10" />
  </Svg>
);
export const IconSearch = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="7" cy="7" r="4.5" />
    <path d="m10.5 10.5 3 3" />
  </Svg>
);
export const IconClose = (p: IconProps) => (
  <Svg {...p}>
    <path d="m4 4 8 8M12 4l-8 8" />
  </Svg>
);
export const IconChevronRight = (p: IconProps) => (
  <Svg {...p}>
    <path d="m6 3.5 4.5 4.5L6 12.5" />
  </Svg>
);
export const IconChevronDown = (p: IconProps) => (
  <Svg {...p}>
    <path d="m3.5 6 4.5 4.5L12.5 6" />
  </Svg>
);
export const IconChevronLeft = (p: IconProps) => (
  <Svg {...p}>
    <path d="M10 3.5 5.5 8l4.5 4.5" />
  </Svg>
);
export const IconCheck = (p: IconProps) => (
  <Svg {...p}>
    <path d="m3 8.5 3 3 7-7" />
  </Svg>
);
export const IconCopy = (p: IconProps) => (
  <Svg {...p}>
    <rect x="5.5" y="5.5" width="8" height="8" rx="1.5" />
    <path d="M10.5 5.5V4a1.5 1.5 0 0 0-1.5-1.5H4A1.5 1.5 0 0 0 2.5 4v5A1.5 1.5 0 0 0 4 10.5h1.5" />
  </Svg>
);
export const IconExternal = (p: IconProps) => (
  <Svg {...p}>
    <path d="M9 2.5h4.5V7" />
    <path d="M13.5 2.5 7 9" />
    <path d="M12 9.5V12a1.5 1.5 0 0 1-1.5 1.5H4A1.5 1.5 0 0 1 2.5 12V5.5A1.5 1.5 0 0 1 4 4h2.5" />
  </Svg>
);
export const IconLink = (p: IconProps) => (
  <Svg {...p}>
    <path d="M6.5 9.5 9.5 6.5" />
    <path d="m7 4.5 1.5-1.5a2.83 2.83 0 0 1 4 4L11 8.5" />
    <path d="m9 11.5-1.5 1.5a2.83 2.83 0 0 1-4-4L5 7.5" />
  </Svg>
);
/** 竖排三点：行内 / 树节点的「更多操作」菜单触发 */
export const IconMore = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="8" cy="3.5" r="1" fill="currentColor" stroke="none" />
    <circle cx="8" cy="8" r="1" fill="currentColor" stroke="none" />
    <circle cx="8" cy="12.5" r="1" fill="currentColor" stroke="none" />
  </Svg>
);
export const IconDownload = (p: IconProps) => (
  <Svg {...p}>
    <path d="M8 2.5v7.5" />
    <path d="m5 7.5 3 3 3-3" />
    <path d="M2.5 11.5v1a1 1 0 0 0 1 1h9a1 1 0 0 0 1-1v-1" />
  </Svg>
);
export const IconUpload = (p: IconProps) => (
  <Svg {...p}>
    <path d="M8 10.5V3" />
    <path d="m5 6 3-3 3 3" />
    <path d="M2.5 11.5v1a1 1 0 0 0 1 1h9a1 1 0 0 0 1-1v-1" />
  </Svg>
);
/** 团队：两个人 */
export const IconTeam = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="6" cy="5.5" r="2.25" />
    <path d="M1.75 13c0-2.3 1.9-3.75 4.25-3.75S10.25 10.7 10.25 13" />
    <path d="M10.5 3.5a2.25 2.25 0 0 1 0 4.2" />
    <path d="M11.5 9.5c1.7.35 2.75 1.6 2.75 3.5" />
  </Svg>
);
/** 共享边界：一圈围栏 */
export const IconBoundary = (p: IconProps) => (
  <Svg {...p}>
    <rect x="2.5" y="2.5" width="11" height="11" rx="2" strokeDasharray="2.2 1.8" />
    <circle cx="8" cy="8" r="1.75" />
  </Svg>
);
export const IconCalendar = (p: IconProps) => (
  <Svg {...p}>
    <rect x="2.5" y="3.5" width="11" height="10" rx="1.5" />
    <path d="M2.5 6.5h11M5.5 2v3M10.5 2v3" />
  </Svg>
);
export const IconClock = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="8" cy="8" r="5.5" />
    <path d="M8 5v3l2 1.5" />
  </Svg>
);
export const IconWarning = (p: IconProps) => (
  <Svg {...p}>
    <path d="M8 2.5 14 13H2z" />
    <path d="M8 6.5v3" />
    <circle cx="8" cy="11.5" r=".6" fill="currentColor" stroke="none" />
  </Svg>
);
export const IconPlay = (p: IconProps) => (
  <Svg {...p}>
    <path d="M5 3.5v9l7-4.5z" />
  </Svg>
);
export const IconHand = (p: IconProps) => (
  <Svg {...p}>
    <path d="M5.5 8.5V4a1 1 0 0 1 2 0v3.5M7.5 7V3a1 1 0 0 1 2 0v4M9.5 7.5V4a1 1 0 0 1 2 0v5" />
    <path d="M11.5 9v1.5A3.5 3.5 0 0 1 8 14H7.4a3.5 3.5 0 0 1-2.8-1.4L3 10.5a1 1 0 0 1 1.5-1.3L5.5 10" />
  </Svg>
);
export const IconUser = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="8" cy="5.5" r="2.75" />
    <path d="M2.75 14a5.25 5.25 0 0 1 10.5 0" />
  </Svg>
);
export const IconLogout = (p: IconProps) => (
  <Svg {...p}>
    <path d="M6.5 2.5H4A1.5 1.5 0 0 0 2.5 4v8A1.5 1.5 0 0 0 4 13.5h2.5" />
    <path d="M10 5.5 12.5 8 10 10.5M6.5 8h6" />
  </Svg>
);
export const IconMenu = (p: IconProps) => (
  <Svg {...p}>
    <path d="M2.5 4.5h11M2.5 8h11M2.5 11.5h11" />
  </Svg>
);
export const IconKeyboard = (p: IconProps) => (
  <Svg {...p}>
    <rect x="1.5" y="4" width="13" height="8" rx="1.5" />
    <path d="M4 7h.01M6.5 7h.01M9 7h.01M11.5 7h.01M5 9.5h6" />
  </Svg>
);
export const IconTrash = (p: IconProps) => (
  <Svg {...p}>
    <path d="M3 4.5h10M6.5 4.5V3h3v1.5M4.5 4.5l.6 8a1 1 0 0 0 1 .9h3.8a1 1 0 0 0 1-.9l.6-8" />
  </Svg>
);
export const IconEdit = (p: IconProps) => (
  <Svg {...p}>
    <path d="m9.5 3.5 3 3-7 7H2.5v-3z" />
    <path d="m8 5 3 3" />
  </Svg>
);
export const IconBuilding = (p: IconProps) => (
  <Svg {...p}>
    <path d="M2.5 13.5h11M3.5 13.5V3a.5.5 0 0 1 .5-.5h5a.5.5 0 0 1 .5.5v10.5M9.5 6.5h2.5a.5.5 0 0 1 .5.5v6.5" />
    <path d="M5.5 5h1.5M5.5 7.5h1.5M5.5 10h1.5" />
  </Svg>
);
export const IconTag = (p: IconProps) => (
  <Svg {...p}>
    <path d="M2.5 2.5h5l6 6-5 5-6-6z" />
    <circle cx="5.5" cy="5.5" r=".75" fill="currentColor" stroke="none" />
  </Svg>
);
export const IconComment = (p: IconProps) => (
  <Svg {...p}>
    <path d="M2.5 3.5A1 1 0 0 1 3.5 2.5h9a1 1 0 0 1 1 1v6.5a1 1 0 0 1-1 1H7l-3 2.5v-2.5h-.5a1 1 0 0 1-1-1z" />
    <path d="M5.5 5.5h5M5.5 8h3" />
  </Svg>
);
export const IconKey = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="5.5" cy="10.5" r="3" />
    <path d="m7.75 8.25 5.75-5.75M11 5l1.5 1.5M9 7l1.5 1.5" />
  </Svg>
);


// ---------- 签名图标（20 枚，提案「Axiom 图标与动效」§二） ----------
/*
 * 24 网格、1.5px 描边、圆头圆角、单色随文字色；与上面的 16 网格图标并排时屏幕描边保持一致（≤20px 时 1.5px，更大时按比例）。
 * 类比只留在注释里：目标 = 靴中的芽，任务 = 压实的方块，待领取 = 方块堆，Agent = 视窗双点，验收 = 扫描，待确认操作 = 舵轮，
 * 执行记录 = 进度环中的播放，用量 = 太阳能电池，Bug = 刷子与异物，发现于 = 痕迹，阻塞 = 链，逾期 = 时钟加告警灯，已取消 = 气闸，
 * 通知 = 信标，协作 = 双星环绕，甘特图 = 航线，动态 = 磁带，组织 = 客轮剪影。
 * 动效要点亮的部件带 ax-* 类名（globals.css「签名动效」按 [data-motion] 找它们）。
 */
function Svg24({ size = 16, className, strokeWidth, children, ...rest }: IconProps) {
  const sw = strokeWidth ?? (size <= 20 ? +(36 / size).toFixed(2) : 1.5);
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={sw} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false" className={cxi("sig", className)} {...rest}>
      {children}
    </svg>
  );
}
const cxi = (...xs: Array<string | undefined>) => xs.filter(Boolean).join(" ");

/** 目标：靴中的芽。stage="bud" 只有芽（未完成），"grown" 长出两片叶子（完成）；发芽动效画 .ax-stem / .ax-leaf。 */
export const IconGoal = ({ stage = "grown", ...p }: IconProps & { stage?: "bud" | "grown" }) => (
  <Svg24 {...p} className={cxi("sig-goal", p.className)} data-stage={stage}>
    <path d="M7 14h10l-1 6H8z" />
    <path className="ax-stem" pathLength={40} d="M12 14v-4" />
    <path className="ax-leaf ax-leaf-r" pathLength={40} d="M12 10c0-3 2-4 4-4 0 2.5-1.5 4-4 4z" />
    {stage === "grown" && <path className="ax-leaf ax-leaf-l" pathLength={40} d="M12 12c0-2.5-1.7-3.5-3.5-3.5 0 2.2 1.5 3.5 3.5 3.5z" />}
  </Svg24>
);
/** 里程碑：目标时间轴上的一枚菱形刻度，下面一道短竿把它钉在日期上（ADR 0016）。 */
export const IconMilestone = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-milestone", p.className)}>
    <path d="M12 4l6.5 6.5L12 17l-6.5-6.5z" />
    <path d="M12 17v3" />
  </Svg24>
);
/** 任务：方块（等轴立方体）。完成时整枚"压实"（.sig-task 缩 15% 回弹）。 */
export const IconTask = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-task", p.className)}>
    <path d="M12 3l8 4.5v9L12 21l-8-4.5v-9z" />
    <path d="M4 7.5l8 4.5 8-4.5" />
    <path d="M12 12v9" />
  </Svg24>
);
/** 进行中：正在压缩——小一号的方块，四周四道短线。只用于状态标记。 */
export const IconTaskActive = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-task-active", p.className)}>
    <path d="M12 5l6 3.5v7L12 19l-6-3.5v-7z" />
    <path d="M6 8.5l6 3.5 6-3.5" />
    <path d="M12 12v7" />
    <path d="M2.5 12h2M19.5 12h2M12 1.5v2M12 20.5v2" />
  </Svg24>
);
/** 待领取任务：方块堆（两块垫底、一块在上）。领取时上面那块（.ax-pile-top）被取走。 */
export const IconBacklog = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-backlog", p.className)}>
    <path d="M4 14h6v6H4zM14 14h6v6h-6z" />
    <path className="ax-pile-top" d="M9 6h6v6H9z" />
  </Svg24>
);
/** Agent：视窗与双点。双点是状态语言（.ax-eye-l / .ax-eye-r）：可用常亮、执行中交替、已停用变暗；不画脸。 */
export const IconAgent = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-agent", p.className)}>
    <rect className="ax-visor" x="3" y="6" width="18" height="12" rx="6" />
    <circle className="ax-eye ax-eye-l" cx="9" cy="12" r="1.6" fill="currentColor" stroke="none" />
    <circle className="ax-eye ax-eye-r" cx="15" cy="12" r="1.6" fill="currentColor" stroke="none" />
  </Svg24>
);
/** 成员：通用人形（与 Agent 的视窗形成人 / 机对照）。 */
export const IconMember = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-member", p.className)}>
    <circle cx="12" cy="8" r="4" />
    <path d="M4 20c0-3.5 3.6-6 8-6s8 2.5 8 6" />
  </Svg24>
);
/** 验收：扫描。中线（.ax-scanline）是扫描线，进入待验收时扫一次。 */
export const IconAccept = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-accept", p.className)}>
    <path d="M2.5 12s3.5-6 9.5-6 9.5 6 9.5 6-3.5 6-9.5 6-9.5-6-9.5-6z" />
    <circle cx="12" cy="12" r="3" />
    <path className="ax-scanline" d="M4.5 12h4M15.5 12h4" />
  </Svg24>
);
/** 待确认操作：舵轮（.ax-helm）。批准时转四分之一圈（.helm-turn），拒绝时回正。 */
export const IconApprove = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-approve", p.className)}>
    <g className="ax-helm">
      <circle cx="12" cy="12" r="6" />
      <circle cx="12" cy="12" r="1.4" fill="currentColor" stroke="none" />
      <path d="M12 2.5V6M12 18v3.5M3.8 7.3l3 1.7M17.2 15l3 1.7M3.8 16.7l3-1.7M17.2 9l3-1.7" />
    </g>
  </Svg24>
);
/** 执行记录：进度环中的播放。环的缺口是尚未完成的部分。 */
export const IconRun = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-run", p.className)}>
    <path className="ax-ring-path" d="M12 3a9 9 0 1 1-6.4 2.6" />
    <path d="M10 9l5 3-5 3z" />
  </Svg24>
);
/** 用量 / 成本：太阳能电池。两格电量 + 上方三道阳光。 */
export const IconUsage = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-usage", p.className)}>
    <rect x="3" y="10" width="16" height="9" rx="2" />
    <path d="M19 13h2v3h-2" />
    <path className="ax-cell" d="M8 13.5v2M13 13.5v2" />
    <path d="M11 2.5v2.5M6.5 4l1.8 2M15.5 4l-1.8 2" />
  </Svg24>
);
/** Bug：刷子与异物。异物是那个点，刷子是处理它的动作。 */
export const IconBug = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-bug", p.className)}>
    <rect x="4" y="13" width="10" height="4" rx="1" />
    <path d="M6 17v3M9.5 17v3M13 17v3" />
    <path d="M13.5 13l4.5-6.5" />
    <circle cx="19" cy="5.5" r="1.8" fill="currentColor" stroke="none" />
  </Svg24>
);
/** 发现于：追踪的痕迹——三点足迹指向来源。 */
export const IconTrail = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-trail", p.className)}>
    <circle cx="5" cy="18" r="1.6" fill="currentColor" stroke="none" />
    <circle cx="9.5" cy="13.5" r="1.6" fill="currentColor" stroke="none" />
    <circle cx="14" cy="9.5" r="1.6" fill="currentColor" stroke="none" />
    <path d="M16.5 6h4v4" />
    <path d="M20.5 6l-4 4" />
  </Svg24>
);
/** 阻塞：链。两个链环（.ax-link-a / .ax-link-b），前置被取消时链自动断开（断链动效）。 */
export const IconBlocks = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-blocks", p.className)}>
    <path className="ax-link-a" d="M10 14a3.5 3.5 0 0 1 0-5l2-2a3.5 3.5 0 0 1 5 5l-1 1" />
    <path className="ax-link-b" d="M14 10a3.5 3.5 0 0 1 0 5l-2 2a3.5 3.5 0 0 1-5-5l1-1" />
  </Svg24>
);
/** 逾期：时钟加告警灯。右上的点是 LED（.ax-ov-led），沿用"双闪后休息 4 秒"。 */
export const IconOverdue = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-overdue", p.className)}>
    <circle cx="11" cy="13" r="7" />
    <path d="M11 9v4l2.5 1.5" />
    <circle className="ax-ov-led" cx="19.5" cy="4.5" r="2" fill="currentColor" stroke="none" />
  </Svg24>
);
/** 已取消：气闸。送出去，不是删除。 */
export const IconCancel = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-cancel", p.className)}>
    <path d="M4 5v14h9" />
    <path d="M4 5h9" />
    <path d="M12 12h9M17 8l4 4-4 4" />
  </Svg24>
);
/** 通知：桅顶信标。有新通知时信标（.ax-beacon）闪两下，不摇铃铛。 */
export const IconNotify = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-notify", p.className)}>
    <circle className="ax-beacon" cx="12" cy="14" r="2" fill="currentColor" stroke="none" />
    <path d="M8 10a5.5 5.5 0 0 1 8 0" />
    <path d="M5 7a10 10 0 0 1 14 0" />
    <path d="M12 16.5V21" />
  </Svg24>
);
/** 参与角色 / 交接：双星环绕（.ax-orbit），与舰徽同源。 */
export const IconCollab = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-collab", p.className)}>
    <circle cx="12" cy="12" r="7.5" strokeDasharray="1.5 3.5" opacity=".8" />
    <g className="ax-orbit">
      <circle cx="12" cy="3.5" r="2.2" fill="currentColor" stroke="none" />
      <circle cx="12" cy="20.5" r="2.2" fill="currentColor" stroke="none" opacity=".55" />
    </g>
  </Svg24>
);
/** 甘特图：航线。实线是计划，虚线是前后依赖。 */
export const IconGanttFlight = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-gantt", p.className)}>
    <path d="M3.5 6.5h8" />
    <path d="M9.5 12h9" />
    <path d="M5.5 17.5h7" />
    <path d="M11.5 6.5l-2 5.5M18.5 12l-6 5.5" strokeDasharray="2.5 3" />
  </Svg24>
);
/** 动态：磁带——被记录下来的事。 */
export const IconLog = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-log", p.className)}>
    <rect x="3" y="6" width="18" height="12" rx="2" />
    <circle cx="8.5" cy="12" r="2" />
    <circle cx="15.5" cy="12" r="2" />
    <path d="M8.5 15.5h7" />
  </Svg24>
);
/** 组织：客轮剪影。只作默认组织头像与后台组织列表。 */
export const IconOrg = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-org", p.className)}>
    <path d="M3 15h15a3 3 0 0 0 3-3 2 2 0 0 0-2-2H6l-3 5z" />
    <path d="M8 10V8h7v2" />
    <path d="M11 8V6" />
    <path d="M6 15l-2 3h14" />
  </Svg24>
);

/** 看板：三列，每列一张卡片（列 = 流程状态，卡片 = 任务）。 */
export const IconBoard = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-board", p.className)}>
    <path d="M4 5v14M12 5v14M20 5v14" />
    <rect x="6" y="7.5" width="4" height="4" rx="1" />
    <rect x="14" y="7.5" width="4" height="3" rx="1" />
    <rect x="14" y="13" width="4" height="3" rx="1" />
  </Svg24>
);
/** 迭代：一圈循环的箭头，顶上一道刻度——固定的时间盒。 */
export const IconSprint = (p: IconProps) => (
  <Svg24 {...p} className={cxi("sig-sprint", p.className)}>
    <path d="M19.5 12a7.5 7.5 0 1 1-2.2-5.3" />
    <path d="M17 3.5v3.5h-3.5" />
    <path d="M12 8.5V12l2.5 1.5" />
  </Svg24>
);

/** 拖动排序的把手：六点，只在行悬停时露出来（样式由调用方给）。 */
export const IconGrip = (p: IconProps) => (
  <Svg {...p}>
    {[4, 8, 12].map((y) => [6, 10].map((x) => <circle key={`${x}-${y}`} cx={x} cy={y} r="1" fill="currentColor" stroke="none" />))}
  </Svg>
);

// ---------- 目标类型的图标（ADR 0023） ----------
/**
 * 目标类型能选的图标：**只从这套现成的线性图标里挑**，不开自定义上传、不接 emoji（DESIGN.md「图标只做辅助，永远配文字；不用 emoji」）。
 * 键就是存进 `goal_types.icon` 的字符串，内置的三种类型用的是 blocks / gantt / chart。
 * 名字在字典里（`goalType.icon.*`），拿不准的字符串一律回落到标签图标，不会画成空白。
 */
export const GOAL_TYPE_ICONS: Record<string, (p: IconProps) => ReactElement> = {
  blocks: IconBlocks,
  gantt: IconGantt,
  chart: IconChart,
  target: IconTarget,
  goal: IconGoal,
  milestone: IconMilestone,
  flow: IconFlow,
  board: IconBoard,
  sprint: IconSprint,
  team: IconTeam,
  building: IconBuilding,
  tag: IconTag,
  bug: IconBug,
  overview: IconOverview,
};
export const GOAL_TYPE_ICON_NAMES = Object.keys(GOAL_TYPE_ICONS);
/** 画一枚目标类型的图标；不认识的名字回落到标签。 */
export const GoalTypeIcon = ({ name, ...p }: IconProps & { name: string }) => {
  const C = GOAL_TYPE_ICONS[name] ?? IconTag;
  return <C {...p} />;
};

// ---------- 品牌线稿 ----------
/**
 * 轨道舰徽：细线环 + 一颗沿环运行的圆点（12s 一周匀速，reduced-motion 时静止）。
 * 侧边栏 24px，空状态 48px，登录页 64px。描边随尺寸放大但保持"细线"（1.5px → 2px）。animate=false 时圆点静止在右上。
 */
export function ShipMark({ size = 24, className, animate = true, title }: { size?: number; className?: string; animate?: boolean; title?: string }) {
  const big = size >= 48;
  const sw = big ? 2 : 1.5;
  const dot = big ? 3.5 : 2.25;
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" fill="none" aria-hidden={title ? undefined : true} role={title ? "img" : undefined} focusable="false" className={className}>
      {title && <title>{title}</title>}
      {/* 核心：一颗实心小圆 */}
      <circle cx="32" cy="32" r={big ? 5 : 6} fill="currentColor" />
      {/* 内环：细线 */}
      <circle cx="32" cy="32" r="14" stroke="currentColor" strokeWidth={sw} opacity="0.55" />
      {/* 轨道环：细线，微微倾斜的椭圆，像轨道投影 */}
      <ellipse cx="32" cy="32" rx="27" ry="27" stroke="currentColor" strokeWidth={sw} />
      {/* 沿环运行的圆点：外层 g 旋转 */}
      <g className={animate ? "ship-mark-orbit" : undefined} style={animate ? undefined : { transform: "rotate(-45deg)", transformOrigin: "50% 50%" }}>
        <circle cx="59" cy="32" r={dot} fill="currentColor" />
      </g>
    </svg>
  );
}

/**
 * 空状态插画：雷达扫描。三圈细线同心圆（ink-tertiary）+ 四个方位刻度 + 一道 60° 的扇形微光（accent 16%，前缘 accent 70%）
 * 8s 一圈匀速旋转（.radar-sweep，reduced-motion 时静止）；正中一枚 48px 舰徽（静止，避免两种转速叠在一起）。120px。
 */
export function RadarIllustration({ className }: { className?: string }) {
  return (
    <span className={`relative inline-block h-[120px] w-[120px] ${className ?? ""}`} aria-hidden="true">
      <svg width="120" height="120" viewBox="0 0 120 120" fill="none" focusable="false" className="absolute inset-0 text-ink-tertiary">
        <g stroke="currentColor" strokeWidth="1">
          <circle cx="60" cy="60" r="58" opacity=".5" />
          <circle cx="60" cy="60" r="44" opacity=".6" strokeDasharray="2 3" />
          <circle cx="60" cy="60" r="30" opacity=".7" />
          <path d="M60 2v5M60 113v5M2 60h5M113 60h5" opacity=".8" />
        </g>
        <g className="radar-sweep">
          <path d="M60 60 60 2A58 58 0 0 1 110.2 31Z" fill="color-mix(in srgb, var(--c-accent) 16%, transparent)" />
          <line x1="60" y1="60" x2="60" y2="2" stroke="color-mix(in srgb, var(--c-accent) 70%, transparent)" strokeWidth="1" />
        </g>
      </svg>
      <ShipMark size={48} animate={false} className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 text-ink-subtle" />
    </span>
  );
}
