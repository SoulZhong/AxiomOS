---
version: 2.0
name: AxiomOS-design-system
description: AxiomOS 的设计规范 v2，以 Linear 的设计语言为蓝本：近黑画布、四级表面阶梯、细线分层、唯一的薰衣草蓝强调色、无阴影、负字距标题。在此之上叠一层克制的"Axiom 母舰"科幻气质（名字取自《机器人总动员》里的星舰 Axiom）：星图背景、舰桥仪表式的等宽遥测文字、带微光的状态指示灯、登录页的星舰视界。整体气质：一艘安静运转的星舰的舰桥控制台——密集、精确、深邃，没有廉价的霓虹。

colors:
  # 画布与表面阶梯（Linear）
  canvas: "#010102"
  surface-1: "#0f1011"
  surface-2: "#141516"
  surface-3: "#18191a"
  surface-4: "#1d1e20"
  hairline: "#23252a"
  hairline-strong: "#34343a"
  hairline-tertiary: "#3e3e44"
  edge-highlight: "rgba(255,255,255,0.04)"
  overlay: "rgba(0,0,0,0.6)"
  # 文字（Linear）
  ink: "#f7f8f8"
  ink-muted: "#d0d6e0"
  ink-subtle: "#8a8f98"
  ink-tertiary: "#62666d"
  # 唯一强调色（Linear 薰衣草蓝）
  accent: "#5e6ad2"
  accent-hover: "#828fff"
  accent-focus: "#5e69d1"
  accent-bg: "rgba(94,106,210,0.14)"
  accent-border: "rgba(94,106,210,0.40)"
  accent-glow: "rgba(94,106,210,0.35)"
  # 语义色（深色画布专用，文字色 / 12% 底 / 35% 边）
  success: "#27a644"
  success-bg: "rgba(39,166,68,0.12)"
  success-border: "rgba(39,166,68,0.35)"
  warning: "#d9a53b"
  warning-bg: "rgba(217,165,59,0.12)"
  warning-border: "rgba(217,165,59,0.35)"
  danger: "#e5484d"
  danger-bg: "rgba(229,72,77,0.12)"
  danger-border: "rgba(229,72,77,0.35)"
  neutral: "#8a8f98"
  neutral-bg: "rgba(138,143,152,0.12)"
  neutral-border: "rgba(138,143,152,0.30)"
  # Axiom 科幻层
  star: "rgba(247,248,248,0.55)"
  star-faint: "rgba(247,248,248,0.18)"
  grid-line: "rgba(94,106,210,0.08)"
  telemetry: "#9aa3ff"

typography:
  display:
    fontFamily: "Inter, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Noto Sans CJK SC', system-ui, sans-serif"
    fontSize: 40px
    fontWeight: 600
    lineHeight: 1.15
    letterSpacing: -1.0px
  headline:
    fontFamily: "Inter, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Noto Sans CJK SC', system-ui, sans-serif"
    fontSize: 24px
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: -0.5px
  title:
    fontFamily: "Inter, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Noto Sans CJK SC', system-ui, sans-serif"
    fontSize: 16px
    fontWeight: 500
    lineHeight: 1.25
    letterSpacing: -0.2px
  body:
    fontFamily: "Inter, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Noto Sans CJK SC', system-ui, sans-serif"
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: 0
  body-medium:
    fontFamily: "Inter, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Noto Sans CJK SC', system-ui, sans-serif"
    fontSize: 14px
    fontWeight: 500
    lineHeight: 1.5
    letterSpacing: 0
  caption:
    fontFamily: "Inter, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Noto Sans CJK SC', system-ui, sans-serif"
    fontSize: 12px
    fontWeight: 400
    lineHeight: 1.4
    letterSpacing: 0
  eyebrow:
    fontFamily: "'JetBrains Mono', 'SF Mono', Menlo, monospace"
    fontSize: 11px
    fontWeight: 500
    lineHeight: 1.3
    letterSpacing: 0.6px
  button:
    fontFamily: "Inter, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Noto Sans CJK SC', system-ui, sans-serif"
    fontSize: 14px
    fontWeight: 500
    lineHeight: 1.2
    letterSpacing: 0
  mono:
    fontFamily: "'JetBrains Mono', 'SF Mono', Menlo, monospace"
    fontSize: 12px
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: 0

rounded:
  xs: 4px
  sm: 6px
  md: 8px
  lg: 12px
  xl: 16px
  full: 9999px

spacing:
  2xs: 2px
  xs: 4px
  sm: 8px
  md: 12px
  lg: 16px
  xl: 24px
  2xl: 32px
  3xl: 48px

components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "#ffffff"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 0 14px
    height: 32px
    boxShadow: "0 0 0 1px {colors.accent-border}, 0 0 16px {colors.accent-glow}"
  button-primary-hover:
    backgroundColor: "{colors.accent-hover}"
    textColor: "#ffffff"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 0 14px
  button-secondary:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 0 14px
    border: "1px solid {colors.hairline-strong}"
  button-tertiary:
    backgroundColor: "transparent"
    textColor: "{colors.ink-muted}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 0 10px
  button-danger:
    backgroundColor: "transparent"
    textColor: "{colors.danger}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 0 14px
    border: "1px solid {colors.danger-border}"
  text-input:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: 0 12px
    height: 32px
    border: "1px solid {colors.hairline}"
  text-input-focused:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: 0 12px
    border: "1px solid {colors.accent-focus}"
    boxShadow: "0 0 0 2px {colors.accent-bg}"
  panel:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.lg}"
    padding: 0
    border: "1px solid {colors.hairline}"
    boxShadow: "inset 0 1px 0 {colors.edge-highlight}"
  panel-header:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    typography: "{typography.title}"
    rounded: "{rounded.lg} {rounded.lg} 0 0"
    padding: 12px 16px
    border: "0 0 1px 0 solid {colors.hairline}"
  table-header:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink-subtle}"
    typography: "{typography.eyebrow}"
    rounded: "0"
    padding: 8px 12px
  table-row:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "0"
    padding: 10px 12px
  table-row-hover:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "0"
    padding: 10px 12px
  tag-neutral:
    backgroundColor: "{colors.neutral-bg}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.caption}"
    rounded: "{rounded.xs}"
    padding: 1px 6px
    border: "1px solid {colors.neutral-border}"
  tag-accent:
    backgroundColor: "{colors.accent-bg}"
    textColor: "{colors.accent-hover}"
    typography: "{typography.caption}"
    rounded: "{rounded.xs}"
    padding: 1px 6px
    border: "1px solid {colors.accent-border}"
  tag-success:
    backgroundColor: "{colors.success-bg}"
    textColor: "{colors.success}"
    typography: "{typography.caption}"
    rounded: "{rounded.xs}"
    padding: 1px 6px
    border: "1px solid {colors.success-border}"
  tag-warning:
    backgroundColor: "{colors.warning-bg}"
    textColor: "{colors.warning}"
    typography: "{typography.caption}"
    rounded: "{rounded.xs}"
    padding: 1px 6px
    border: "1px solid {colors.warning-border}"
  tag-danger:
    backgroundColor: "{colors.danger-bg}"
    textColor: "{colors.danger}"
    typography: "{typography.caption}"
    rounded: "{rounded.xs}"
    padding: 1px 6px
    border: "1px solid {colors.danger-border}"
  status-led:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent}"
    typography: "{typography.caption}"
    rounded: "{rounded.full}"
    padding: 0
    boxShadow: "0 0 6px {colors.accent-glow}"
  sidebar:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-subtle}"
    typography: "{typography.body}"
    rounded: "0"
    padding: 16px 12px
    border: "0 1px 0 0 solid {colors.hairline}"
  sidebar-item-active:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink}"
    typography: "{typography.body-medium}"
    rounded: "{rounded.sm}"
    padding: 6px 10px
    border: "0 0 0 2px solid {colors.accent}"
  drawer:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.lg} 0 0 {rounded.lg}"
    padding: 0
    border: "1px solid {colors.hairline-strong}"
  toast:
    backgroundColor: "{colors.surface-3}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: 10px 14px
    border: "1px solid {colors.hairline-strong}"
  code-block:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.telemetry}"
    typography: "{typography.mono}"
    rounded: "{rounded.md}"
    padding: 12px
    border: "1px solid {colors.hairline}"
  kbd:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.eyebrow}"
    rounded: "{rounded.xs}"
    padding: 1px 5px
    border: "1px solid {colors.hairline-strong}"
  brand-canvas:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "0"
    padding: 0
  brand-card:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.lg}"
    padding: 32px
    border: "1px solid {colors.hairline-strong}"
    boxShadow: "inset 0 1px 0 {colors.edge-highlight}, 0 0 48px {colors.accent-bg}"
---

## 定位

AxiomOS 是一艘星舰的舰桥：整个组织的目标、任务、人和 Agent 都在这块深色的控制台上运转。风格基底是 Linear（密集、精确、克制、无阴影、单一强调色），科幻感来自**材质与仪表**，不来自霓虹与渐变：近黑画布像舷窗外的太空，四级表面像层层抬升的仪表板，细线像面板的接缝，薰衣草蓝像舰桥仪表的指示光，等宽小字像遥测读数。

默认是**深色**（舰桥）。另有一套**浅色模式**（"日间舱室"），同一套语义 token 名只换值，见「浅色模式」一节。品牌页（登录、邀请、后台登录）永远是深色——那是舱外的太空。

## 颜色

- **画布与阶梯**：页面底 `{colors.canvas}`；面板 `{colors.surface-1}`；悬停行、次级按钮、当前导航项 `{colors.surface-2}`；下拉菜单与提示气泡 `{colors.surface-3}`；再抬升一级 `{colors.surface-4}`。层次只靠阶梯和 1px 细线（`{colors.hairline}` → `{colors.hairline-strong}` → `{colors.hairline-tertiary}`），**没有投影**。每个面板顶边加一条 `{colors.edge-highlight}` 内嵌高光，Linear 称之为"像素渲染感"。
- **唯一强调色**：薰衣草蓝 `{colors.accent}`。用于主按钮、当前导航的左侧竖线、焦点环、链接、进度条、甘特图的"今天"线、选中态。悬停变亮 `{colors.accent-hover}`。主按钮和进行中的状态指示灯允许一圈 `{colors.accent-glow}` 微光，这是整套系统里**唯一的发光**。
- **语义色**只表状态，且在深色画布上用"文字色 + 12% 底 + 35% 边"的三件套：未开始=neutral，进行中=accent，等待中=warning，已完成=success，已终止=neutral（终止不是错误），逾期与危险操作=danger。
- **遥测色** `{colors.telemetry}` 只给等宽的读数文字（ID、时间戳、token 数、快捷键），是强调色的浅变体，不是第二个强调色。
- 禁止：第二个饱和色、任何渐变、玻璃拟态、彩色背景大面积铺满、青柠或粉色。

## 字体与字阶

- 界面字体 **Inter**（Linear 自有字体的公开替代），400 / 500 / 600；中文回退 PingFang SC / Hiragino Sans GB / Microsoft YaHei / Noto Sans CJK SC。数字全局 `tabular-nums`。
- 等宽字体 **JetBrains Mono**，只用于遥测读数：ID、时间戳、token、成本明细、快捷键、代码、表头微标签。
- 字阶：页面标题 24px/600 字距 -0.5px；面板标题 16px/500 字距 -0.2px；正文 14px/400 行高 1.5；说明 12px；表头与眉标 11px 等宽、正字距 +0.6px（Linear 的"眉标用正字距做分类标记"）。品牌页大标题 40px/600 字距 -1px。
- 600 只允许出现在页面标题与品牌页标题；正文与按钮不超过 500。

## 间距与圆角

- 4px 基数：2 / 4 / 8 / 12 / 16 / 24 / 32 / 48。面板内边距 16px，面板间距 16px，页面外边距 24px，表格单元格 10px 12px，标签 1px 6px。
- 圆角：标签 4px，导航项 6px，按钮与输入框 8px，面板与抽屉 12px，品牌页大面板 16px，指示灯全圆。不用胶囊按钮。

## 布局

- 外壳：左侧 220px 侧边栏，`{colors.canvas}` 底，右侧 1px `{colors.hairline}`；顶部无全局顶栏；内容区最大 1280px，外边距 24px。
- 侧边栏顶部：舰徽（一个细线绘制的环形轨道 + 圆点）与字标 "AxiomOS" 同一行，下面 11px 等宽眉标显示组织名（如 `DEMO · CNY`），这是"舰桥标识牌"。
- 页面三段式不变：页头（标题 + 一句说明 + 右侧唯一主按钮）→ 统计芯片条 → 面板。
- 断点：≥1200 完整；992–1199 侧栏收成 56px 图标栏；768–991 详情侧栏落到下方；<768 单列，导航变抽屉。

## 浅色模式

- 触发：侧边栏底部的主题切换（系统 / 深色 / 浅色），保存在浏览器；默认跟随系统 `prefers-color-scheme`。切换即时生效，不刷新。
- 实现：所有颜色 token 都是 CSS 变量，浅色只在 `:root[data-theme="light"]`（以及未显式选择时的 `@media (prefers-color-scheme: light)`）里覆盖值；组件代码不写任何硬编码颜色。
- 浅色 token 值（Linear 的 inverse 阶梯为底）：

| token | 深色 | 浅色 |
|---|---|---|
| canvas | #010102 | #fbfbfc |
| surface-1 | #0f1011 | #ffffff |
| surface-2 | #141516 | #f5f6f6 |
| surface-3 | #18191a | #eeeff1 |
| surface-4 | #1d1e20 | #e7e8eb |
| hairline | #23252a | #e4e5e8 |
| hairline-strong | #34343a | #d3d4d8 |
| hairline-tertiary | #3e3e44 | #c4c5ca |
| edge-highlight | rgba(255,255,255,0.04) | transparent |
| overlay | rgba(0,0,0,0.6) | rgba(16,17,20,0.35) |
| ink | #f7f8f8 | #1c1d21 |
| ink-muted | #d0d6e0 | #3f414a |
| ink-subtle | #8a8f98 | #6b6f78 |
| ink-tertiary | #62666d | #9a9ea6 |
| on-accent（强调色底上的文字） | #ffffff | #ffffff |
| code-bg（代码块底） | = canvas | = surface-2 |
| accent / accent-hover / accent-focus | #5e6ad2 / #828fff / #5e69d1 | #5e6ad2 / #4f5bc7 / #5e69d1 |
| accent-bg / accent-border / accent-glow | 14% / 40% / 35% | 10% / 35% / 18% |
| success / warning / danger / neutral | #27a644 / #d9a53b / #e5484d / #8a8f98 | #187a31 / #8f5e08 / #c9303a / #6b6f78 |
| 语义 -bg / -border | 12% / 35% | 10% / 30% |
| telemetry | #9aa3ff | #4f5bc7 |
| star / star-faint | 见上 | 浅色下不使用（品牌页始终深色） |
| grid-line（坐标网格画布） | rgba(94,106,210,0.08) | rgba(94,106,210,0.06) |

- 浅色下的差异：面板不用顶边高光，改为 1px `{colors.hairline}`；主按钮微光减半；状态指示灯呼吸不变；侧边栏底部的星点层不显示；表头底色 `{colors.surface-2}`；代码块底色 `{colors.surface-2}` 而不是画布。
- 对比度底线：`ink-subtle` 在 `surface-1` 上不低于 4.5:1（实测 5.0:1）；语义色文字在各自 10% 底上不低于 4.5:1（success 4.7 / warning 4.9 / danger 4.6——浅色的三个语义色因此比 Linear 的原值再深一档）；`ink-tertiary` 仍不用于必须阅读的文字。
- 实现细节：原始变量叫 `--c-<token>`（`globals.css`），Tailwind 的 `--color-<token>` 通过 `@theme inline` 映射到它们；品牌页根节点带 `data-theme="dark"`，整套深色值在这棵子树上局部重设。SVG / 内联样式里派生的透明度用 `color-mix(in srgb, var(--c-accent) 38%, transparent)`，不写 rgba 字面值。

## Axiom 科幻层（舰桥系统）

科幻感必须**一眼可见**，但来自舰桥仪表的材质与信息密度，不来自霓虹与特效。以下是全站必做的元素，按可见度排序：

1. **舰桥状态栏（全局）**：主内容区顶部一条 32px 高的遥测条，`{colors.surface-1}` 底、底部 1px `{colors.hairline}`，左起等宽 11px 读数：`AXIOM · OPERATING SYSTEM`、`ORG DEMO`、`AGENTS 2/2 ONLINE`（在线用 success 指示灯）、`RUNS 1 ACTIVE`（进行中用呼吸的 accent 指示灯）、`COST TODAY ¥25.07`；右侧 `SYS 16:07:22`（秒级跳动）与主题/语言切换。数据来自现有接口（agents、tasks/mine、stats/cost），30 秒刷新一次。这是"舰桥"最直接的证据，每一页都在。
2. **坐标网格画布**：主内容区画布铺一层极淡的 24px 点阵网格（`{colors.grid-line}`，深色 8% / 浅色 6%），面板盖在网格之上。侧边栏底部保留星点层。
3. **HUD 面板**：面板四角各一枚 6px 细线角标（`{colors.hairline-strong}`，悬停或面板处于活动状态时变 `{colors.accent-border}`）；面板标题前加等宽序号眉标（`01`、`02`…按页面内顺序），标题右侧留遥测位（如条数、更新时间）。
4. **能量线**：页面标题下方一条 1px 线，左段 `{colors.accent}` 到右段透明的渐隐（这是唯一允许的渐变，且只在这里），宽 160px。
5. **状态指示灯**：8px（原 6px），进行中的呼吸 + 微光，在线 Agent 的 success 灯常亮微光。
6. **刻度进度条**：进度条底部每 10% 一道 1px 刻度（`{colors.hairline-strong}`），像仪表刻度；目标树与详情侧栏都用它。
7. **遥测读数**：所有 ID、时间戳、token、成本、快捷键等宽 + `{colors.telemetry}`；数字变化时 200ms 淡入。
8. **雷达空状态**：空状态插画改为雷达扫描（细线同心圆 + 一道 8s 一圈的扇形微光，`prefers-reduced-motion` 时静止），下方一句说明 + 主动作。
9. **甘特图 = 航线图**：时间轴顶部加刻度尺（每天一道细刻度、每周一道长刻度、等宽日期），"今天"是带小三角的 accent 竖线，任务条两端有 1px 端点，目标汇总条像航段。
10. **登录页 = 接近母舰**：见下方「品牌页」一节。不再有自检提示语、不再有打字动画。
11. **舰徽**：轨道环 + 公转点，侧边栏 24px、登录页 64px、空状态 48px。
12. **入场与切换**（动效词汇表，只有这些）：页面 120ms 淡入 + 2px 上移；抽屉从右侧 32px 处滑入 160ms（iOS 抽屉曲线）、退回 100ms；对话框 scale(0.98) 淡入 160ms、退场 100ms；Toast 从顶边滑入 200ms、沿同一条轴退回 120ms，可向上滑动关闭；提示气泡从触发点一侧 scale(0.97) 淡入 125ms、离开 80ms；目标树折叠 200ms / 150ms；状态徽标换状态 160ms 淡入 + 2px 模糊；甘特图切换刻度时 200ms 的模糊过渡；状态栏读数变化淡入；按钮按下 scale(0.97)。曲线只有四种（强 ease-out / 强 ease-in-out / iOS 抽屉 / linear），退场永远比入场快，只动 transform / opacity，键盘触发（快捷键、Esc）的动作不动画，prefers-reduced-motion 时只保留淡入淡出。无其他动画。

浅色模式保留全部结构：网格与角标用浅色 token，星图只在品牌页。

**语言**：所有舰桥文字（状态栏读数、眉标、坐标轴、空状态说明）都是 i18n 词条，中文界面下必须是中文（如 `AXIOM · 操作系统`、`AGENT 在线 2/2`、`执行中 3`、`今日成本`），只有产品名 AXIOM / AxiomOS 与 Agent、token、MCP、ID 这类术语保留拉丁字母。等宽"丝印"样式不变，只换词。

## 组件

- **按钮**：主按钮 `{components.button-primary}` 带微光，一页只有一个；次级 `{components.button-secondary}`；第三级 `{components.button-tertiary}`（表格行内、抽屉取消）；危险 `{components.button-danger}`。高 32px，小号 28px。
- **输入框**：`{colors.surface-1}` 底 + 细线，焦点 1px `{colors.accent-focus}` + 2px `{colors.accent-bg}` 外圈。日期控件 `color-scheme: dark`，与输入框同高同边。
- **面板**：`{components.panel}`；面板头透明底 + 底部细线，标题 16px/500；面板内不再嵌套面板。
- **表格**：表头 `{colors.surface-2}` 底、11px 等宽眉标、`{colors.ink-subtle}`；行悬停 `{colors.surface-2}`；边线 `{colors.hairline}`；数字右对齐等宽。
- **标签**：三件套语义色，4px 圆角，12px。
- **状态徽标**：状态指示灯 + 状态名标签 + 12px 状态类型说明。
- **进度条**：高 6px，底 `{colors.surface-3}`，条 `{colors.accent}`，完成后 `{colors.success}`；不发光。
- **头像**：成员圆形 `{colors.surface-3}` 底 + `{colors.ink-muted}` 字；Agent 方形 `{colors.accent-bg}` 底 + 机器人图标。
- **抽屉**：右侧 480px，`{components.drawer}`，头部标题 + 一句提示，底部第三级"取消" + 主按钮。
- **对话框**：`{colors.surface-2}`，宽 480px，`{colors.overlay}` 遮罩。
- **提示气泡 / Toast**：`{components.toast}`。失败 Toast 左侧 2px `{colors.danger}` 边线。
- **空状态**：细线绘制的轨道舰徽变体（探测器 / 卫星），`{colors.ink-tertiary}` 线条，一句说明 + 一个主动作。
- **图表**：柱与线用 `{colors.accent}` 单色，网格线 `{colors.hairline}`，坐标轴文字等宽 11px。

## 品牌页（登录 / 邀请 / 后台登录）= 接近母舰

意象来自《机器人总动员》：镜头从深空缓缓推向 Axiom 母舰——一艘巨大、洁白、层层甲板都亮着灯的星际邮轮，安静地悬在星海里。登录就是"请求登舰"。画面必须**一眼看出是一艘星舰**，动画必须**慢、稳、有重量感**，像真实的太空镜头。

实现方式是 WebGL 实时场景（ADR 0011，技术依据见 `docs/design/sources/threejs-implementation-notes.md`）。让画面"贵"的八条原则，来自 `docs/design/sources/scifi-web-animation-survey.md`：

1. 光照是主角：一盏暖主光 + 一盏冷轮廓光 + 环境贴图，不用均匀环境光。
2. 亮的东西真的亮：舷窗、船头带、引擎是 HDR 自发光，由 Bloom 泛光，不贴光晕图。
3. 镜头很慢且有惯性；大物体更慢。
4. 后期是薄薄一层：Bloom + 轻微暗角 + 3%–5% 颗粒，不拉满色散。
5. 有深度层次：近景星尘、中景母舰、远景星空与星云，视差各不相同。
6. 一个色调映射（ACES）+ 一个曝光值，全场一致。
7. 没有硬边：星点是软圆，渐变有抖动。
8. 运动由一条时间线统筹。

### 画面构成（自后向前）

1. **深空**：`{colors.canvas}` 底；右上角一团 fbm 噪声星云（靛蓝 → 暖灰紫，贡献不超过 10%，域扭曲、极慢流动）；船尾方向一线**暖色远日**（`--c-sun` #ffd9a0，4%–6%），全站唯一允许的暖色。
2. **星场**：三层 5000 颗软圆星点（远 3000 / 中 1500 / 近 500），尺寸随距离衰减，30% 的星以各自相位、15% 振幅、低于 1.5 Hz 缓慢明暗；鼠标视差按层 0.02 / 0.05 / 0.09，带阻尼。
3. **Axiom 母舰**（原创"Axiom 风格"客轮，≥1024px 时占据视口右侧约 54% 宽、下三分之二高，船头朝右上抬起约 4°）：
   - 流畅的水滴形船体（旋转轮廓 + 压扁截面），船头高大钝圆、船尾收窄，两片宽而扁的后掠推进翼；一条从船尾掠起、在船头后方形成圆顶脊线的上层建筑；小穹顶舰桥与天线；
   - 哑光白 PBR 材质（`#d6dae1`，metalness 0.15，roughness 0.62），暖主光来自船尾方向的远日，冷轮廓光来自船头后方；
   - 300 盏以上 HDR 自发光舷窗沿船体曲线排成 6–7 排，越靠船尾越短；约 8% 以各自 3–7s 周期缓慢明暗；
   - 船头一圈 accent 色的**全景舷窗带**（HDR，Bloom 拾取），引擎两团 accent 辉光 6s 呼吸，并真实照亮船尾；
   - 90s 一周期极慢漂移与约 0.5° 的微幅摆动。
4. **后期**：Bloom（阈值 1.1，只有光源发光）→ ACES 色调映射 → 4.5% 颗粒 → 暗角 0.32/0.55。
5. **镜头**：开场 2.5s 从远处推进（easeOutCubic），之后跟随鼠标做 ±0.35 单位的阻尼视差。
6. **前景内容列**：≥1024px 时内容（舰徽、眉标、标题、说明、登录卡、语言切换）左对齐在左侧 440px 一列内，左侧留白 96px，垂直居中，**与船不重叠**；窄屏时居中堆叠、船退到内容后方。登录卡是 `{components.brand-card}` 的全息面板版——`{colors.surface-1}` 90% 不透明，1px `{colors.accent-border}` 边，四角 8px 角标，极淡 accent 外晕；场景首帧后 400ms 淡入，卡片再晚 120ms 出现。
7. **文字**：64px 轨道舰徽 → 等宽眉标 `AXIOM · 操作系统` → 40px/600 标题「**AI 原生**的组织操作系统」（关键词 `{colors.accent-hover}`；英文可到三行）→ 一句说明「目标、任务、人与 Agent，在同一个系统里协同运转。」。**没有自检行，没有打字机效果，没有任何"花哨的提示语"。**
8. 语言切换在卡片下方，等宽 11px。

### 交互（画面必须是活的）
原则：输入只改变目标值，所有运动仍经过阻尼插值；不劫持键盘，不在输入框附近制造干扰；`prefers-reduced-motion` 时每个效果直接落到终态。

- **光标注视**：船体随光标偏航 ±3°、俯仰 ±1.5°（λ 1.5）；光标附近 0.7 单位内的舷窗最多亮 55%；前景星尘被光标轻轻推开（半径 1.4）。
- **拖拽环绕**：在内容列之外按住拖动，方位角 ±20°、俯仰 ±8°，松手后弹回（λ 2.5）；光标为抓手。滚轮在场景区域内推拉镜头（z 11–14），3 秒无操作后回位；页面本身不滚动。
- **表单注视**：聚焦邮箱框时镜头向船头推进 0.8 单位（约 1.2 秒）；聚焦密码框时舷窗调暗 20%、舰桥窗带提亮 35%，像船在"听"；失焦复原。
- **按键信号**：表单内每次按键（限流 ≤ 3 次/秒）让舰桥附近 3–5 盏舷窗脉冲 +40%、桅顶信标闪一次、窗带 +15%，400ms 内衰减。
- **登舰**（登录成功，共 2.6 秒，只在场景在跑且未开启减少动效时）：
  1. 确认 0–0.35s：引擎点火（辉光 ×2.6、点光 ×4），全船舷窗涌亮 +60% 后回落，舰桥窗带两次 60ms 应答闪光，导航灯沿舷窗排从船尾跑向船头指引入口，登录卡 300ms 淡出下沉，镜头微微后拉做预备。
  2. 掠航 0.35–1.7s：镜头沿船体坐标系中的 Catmull-Rom 样条飞行：从当前机位下潜、掠过近翼与引擎辉光、贴着船侧（约 0.5 单位）让舷窗成排流过、抬升绕到船头正前方 2.2 单位；速度曲线两端平滑、中段全速；视场 32→52 再落到 40，穿越时 ±2° 横滚；星点放大、星尘与 400 条速度线随速度拉伸，Bloom 随速度增强。
  3. 入口 1.7–2.35s：正对全景窗直入到 0.28 单位，窗带 ×3、舷窗 +60%，Bloom 升到 3.3，暗角退去，暖色光束与横向光晕从窗心展开。
  4. 登舰 2.35–2.6s：暖白遮罩 250ms 充满；进入应用后遮罩 600ms 退去，应用内容从 1.03 缩放、0 不透明度落定。
  过场期间忽略一切输入；失败时冷红告警加镜头后坐 0.3；减少动效或无 WebGL 时立即进入。
- **空闲巡航**：12 秒无输入后镜头进入约 2 分钟一周期的纪录片式慢巡航，任何输入即缓慢退出。
- **登录卡倾斜**：光标在卡片外围时卡片随之倾斜 ≤ 4°（λ 6）；光标进入卡片或卡片内有焦点时倾斜归零并保持静止（密码管理器的弹窗锚定在输入框上，几何一直变会闪烁），只有 accent 8% 的高光继续跟随；仅在 `(hover: hover) and (pointer: fine)` 下启用。
- **尺度参照**：2–3 架小型穿梭机每 20–40 秒沿曲线驶向船头；远处右下角一颗背光行星只露出带大气边缘的一角，亮度不超过船体四分之一。

### 加载期
- 页面打开即显示内容列与登录卡（240ms 淡入），背景先是 Canvas2D 深空（星点密度与亮度与 WebGL 星场匹配，右上星云、船尾远日），**没有船**；脚本与模型并行下载，模型着色器预编译完成后场景 600ms 淡入，同时开始 2.5 秒推镜，让船驶入已经存在的页面。
- 本机冷缓存约 0.7 秒、热缓存约 0.3 秒出现第一帧；模型 4 秒未就绪时用程序化船体兜底，模型到达后替换。
- 只有无 WebGL 2 的环境才显示 SVG 母舰静帧。

### 母舰资产
母舰是 Blender 建模的原创资产 `web/public/models/axiom.glb`（脚本 `tools/blender/axiom_ship.py`）：细分曲面船体、台阶式甲板檐、六道环形接缝、凹陷引擎舱、774 盏投射到曲面上的舷窗、连续环绕船头的全景窗带；烘焙法线 / 环境光遮蔽 / 粗糙度贴图 2048²；材质名 `Hull` / `Windows` / `BowBand` / `Engine` / `Beacon`，发光强度由网页驱动。程序化船体只作为加载期间与降级时的替身。

### 后台登录与邀请页
同一画面。后台登录眉标 `AXIOM · 平台控制台`；邀请页标题换成"加入组织「X」"。

### 降级与护栏
- 无 WebGL 2、软件渲染、`prefers-reduced-motion`：Canvas2D 静态星空 + SVG 母舰静帧，灯常亮；加载期间显示同一静帧。
- DPR 上限 1.5；低端设备去色散、Bloom 层级降到 4、DPR 锁 1。
- 所有运动以 `clock.elapsedTime` 为基，`delta` 夹取到 1/30；标签页隐藏时停帧。
- 不允许：扫描线、粒子雨、闪烁文字、霓虹描边、快速运动、同步闪烁、任何超过 240ms 的界面动画（背景层的慢动作除外）。

## 舰内系统 v3（应用内的科技感）

登录是"登舰"，那么应用内就是**舰内**：《机器人总动员》里 Axiom 的内部——洁白光滑的表面、深色的舷窗与全息屏、处处是安静的仪表读数。原则不变：Linear 的克制与效率是底，科幻是一层薄薄的、有信息量的仪表皮肤；每个装饰都必须承载真实数据或真实状态，纯装饰一律不加。

**空间铁律**：应用内的每一块空间都先给有效信息。科技感通过页面风格（表面、分隔线、等宽读数）、交互元素（指令面板、状态 LED、悬停角标）、背景（点阵、状态栏星海）与动效（材质化进场、数字滚动、仪表扫动）来体现，不通过占位的装饰画面。登录页之外不再出现母舰模型。

**用词铁律**：科幻只是视觉主题，不是组织系统的组织方式。界面上的一切文字（标题、标签、提示、命令名、图片替代文字，中英文皆然）只用 `CONTEXT.md` 的词：组织概况、状态栏、快速命令、登录。**禁止**在任何用户可见的地方出现舰船隐喻（舰况、舷窗、引擎、信标、指令台、登舰、舰桥、母舰，英文的 bridge / ship / command deck 同理）。视觉元素可以用数据驱动（亮着的舷窗数量对应进行中任务），但不解释、不命名这种映射。隐喻词只允许出现在本规范和代码注释里。深色与浅色两个主题都要成立：深色是"夜航舰桥"，浅色是"Axiom 白色舱室"（白色表面、极淡冷蓝的分隔线、深色的舷窗带与屏幕形成对比）。

### 1. 舷窗带（状态栏）
状态栏是舰桥的舷窗：32px 高，背后一条 Canvas2D 星海缓慢流过（两层视差、软圆星点、密度与登录页星场一致，浅色主题下也是深色带，像白色舱室里的一条舷窗），前景是等宽读数。读数是活的：`AGENT 在线 n/m` 旁有雷达式脉冲（有 Agent 在线时）；`今日成本` 带 24 小时迷你折线；数值变化时数字滚动。刷新周期 30s，标签页隐藏时停。

### 2. 组织概况（首页与运营看板顶部）
一块约 130px 高的纯读数面板：进行中 / 待验收 / 逾期 / 今日成本，26px 等宽数字，各带 24 小时趋势线，一行四列。**不放任何装饰性画面**——母舰模型只属于登录页；应用内的科技感来自页面风格、状态栏、背景、动效与交互元素，绝不占用本应显示有效信息的空间。

### 3. 仪表级数据
- 金额与 token 计数用滚动数字（odometer，每位 160ms `--ease-out`，仅在数值变化时滚）。
- 进度用带刻度的弧形仪表（arc gauge）：目标进度、验收通过率；圆环外圈 24 格刻度，已完成格点亮。
- 统计卡右上角迷你折线（sparkline，24 点，hairline-strong 描边，尾端一个发光点）。
- 吞吐柱状图的柱子带 accent 边光；今日柱呼吸。
- 动态流是**舰桥日志**：等宽时间戳列（`15:55:29`）、执行者芯片、事件类型用 LED 色点；顶部一行 `实时 · 最近同步 12s 前`，有新事件时 LED 闪一次并把新行 200ms 淡入。

### 4. 指令台（⌘K / Ctrl+K）
全局命令面板：等宽提示符 `›`，模糊搜索页面、任务、目标、Agent；动作：新建任务、领取任务、切换主题、切换语言、打开快捷键；上下键选择、回车执行、Esc 关闭；进场 120ms 从 0.98 缩放淡入，结果分组带眉标。触发按钮放在侧栏底部「快捷键」旁，显示 `⌘K`。

### 5. 面板与表面
- 主面板顶部一条 1px 的边缘高光（深色）/ 极淡冷蓝分隔（浅色）；面板 hover 时四角 6px 角标 120ms 浮现（只在一级面板）。
- 状态 LED：`进行中` 呼吸（3s），`逾期` 快两下再停 4s，`等待中` 常亮暗色。
- 路由切换：整页 100ms 只动透明度的极轻进场，无位移、无交错；键盘切页同样适用（轻到不会被感知为等待）。这是 2026-09-04 与用户确认的折中：高频操作原则上零动画，导航是唯一例外。
- 浅色主题：表面 #ffffff / #f7f8fa / #eef0f4，分隔线 `color-mix(accent 10%, #e3e6ee)`，文字 ink 不变；舷窗带与指令台保持深色。

### 6. Agent 页
Agent 头像是视窗双点 + 外环（§7 的状态语言，`AgentVisor`）：在线 = 双点常亮、环 success 3s 呼吸；执行中 = 双点交替、环 accent 常亮；等待输入 = 双点偏暖、环静止；离线 = 双点 25%、环灰静止；上线时外环充满 520ms，离线 400ms 变灰。每行下方一行等宽遥测：`最近心跳 12s 前 · 今日 3 段 · ¥4.17`。

### 7. 签名图标与状态动效
完整提案（含每枚图标的 SVG 与每条动效的样稿）见 `docs/design/icons-and-motion.html`，2026-09-04 经用户确认全面落地。要点：

- **类比只用于推导**：《机器人总动员》里每台机器人有一条指令、船长握着舵。目标 = 靴中的芽（证据），任务 = 压实的方块，待领取 = 方块堆，Agent = 视窗双点，验收 = 扫描，待确认操作 = 舵轮，执行记录 = 进度环，用量 = 太阳能电池，Bug = 刷子与异物，发现于 = 痕迹，阻塞 = 链，逾期 = 时钟加告警灯，已取消 = 气闸，通知 = 信标，参与角色 / 交接 = 双星环绕，甘特图 = 航线，动态 = 磁带，组织 = 客轮剪影（只做默认头像）。界面文字仍只用 CONTEXT.md 的词。
- **规格**：24 网格、1.5px 描边、圆角端点、单色随文字色；只有这 20 枚承载类比，其余图标保持通用。
- **状态动效只在状态真正变化时发生**（≤240ms，环境动效除外）：任务完成压实 420ms 并亮绿灯；目标完成发芽 480ms；进入待验收扫描 320ms；领取任务取走一块 260ms；Agent 上线充电 520ms，执行中双点交替，离线 400ms 变灰；批准待确认操作转舵 240ms；新通知信标闪两下；逾期双闪后休息 4 秒；交接双星环绕 12 秒；阻塞解除断链 200ms；成本数字滚动。
- **Agent 状态语言**：在线 = 双点常亮、环 3 秒呼吸；执行中 = 双点交替、环常亮；等待输入 = 双点常亮偏暖、环静止；离线 = 双点 25%、环灰静止；注销不显示。不画脸、不做表情。

### 8. 甘特图 v2：宏观到微观
甘特图信息密度最高，规则是**把空间全给图**、**任何粒度都能看**、**能收能放**、**能全屏**。

- **空间**：页面标题与说明在图上方只占一行（说明折进「?」提示）；图区高度 = 视口剩余高度（不随行数无限增长，内部滚动，表头与左列固定）；左侧标签列可拖拽调宽（200–420px）并可收成 56px 的窄列（只留状态点与头像）；行高有「紧凑 28px / 舒适 36px」两档；图例收进底部一行，可折叠。
- **粒度**：刻度六档——时 / 日 / 周 / 月 / 季 / 年。表头始终两层（上层大单位、下层小单位，例如 月 + 日、季 + 周、年 + 月）。「时」档显示执行记录段（执行记录在任务条内按实际起止画成细段，Agent 与人不同色）；「季 / 年」档任务收敛为目标汇总条 + 密度热度（每格按任务数着色）。缩放：按钮、Ctrl/⌘ + 滚轮、触控板捏合，围绕光标位置缩放；「适应全部」一键把整个范围放进视口；切档 200ms 模糊过渡（已有）。今天线在所有档位可见。
- **收起 / 展开**：分组行（目标 / 团队 / 执行者 / 任务类型）左侧有折叠箭头；收起后该组显示一条汇总条（时间跨度、完成比例、任务数与逾期数）；工具栏有「全部收起 / 全部展开」；「未排期」默认收起并显示数量；折叠状态按分组方式记在 localStorage。
- **全屏**：工具栏「全屏」按钮（快捷键 F）：优先调用浏览器全屏 API 让图区独占屏幕；不可用时退到应用内全屏（隐藏侧栏与状态栏，图区铺满）。Esc 或再按一次退出；全屏内工具栏保留。
- **动效**：折叠展开 200ms / 150ms（高度过渡，沿用目标树），缩放围绕光标平滑，其余不变。

### 9. 目标与任务的区分
判定规则见 `CONTEXT.md`。界面上两者用**不同的视觉语言**，让人一眼知道自己面对的是"要做的事"还是"要达成的结果"，同时各自把必要信息给足。

| | 目标（意图） | 任务（要做的事） |
|---|---|---|
| 图标 | 靴中的芽 | 压实的方块 |
| 进展 | 64px 弧形仪表（下面汇总出来的百分比） | 状态徽标（它在自己流程里走到哪一步），**不显示百分比** |
| 版式 | 树形行，缩进 20px/层带引导线，根目标标题 16px/500，行高松 | 表格行，等宽日期与成本，行高紧 |
| 人 | 负责人（只能是人） | 任务负责人（人或 Agent，头像区分） |
| 必给的信息 | 任务完成数 / 总数、**状态摘要句**、计划区间、预算与已花 | 类型、状态、负责人、计划、优先级、成本 |
| 动作动词 | 规划类：新建子目标、新建任务、放弃 | 执行类：领取、开始、提交、验收 |

**状态摘要句**是目标行的关键：它直接说出下一步该干什么，按优先级取第一条命中的——
1. 还没有任务 → 「还没有任务，先拆几个任务出来」
2. 有逾期 → 「n 个任务逾期」（danger 色）
3. 有待验收 → 「n 个任务等验收」（warning 色）
4. 全部完成但未确认达成 → 「任务都完成了，确认达成吗？」并在行内给出「确认达成」按钮
5. 有进行中 → 「n 个进行中，n 个未开始」
6. 其余 → 「n 个任务未开始」

### 禁止
仍然禁止：扫描线、粒子雨、打字机文案、霓虹描边、随机闪烁、超过 240ms 的界面动画、任何不承载数据的装饰。

## 交互与体验原则

沿用 v1 的十条：先扫后读（统计芯片条）、动作就近（行内悬停动作）、状态可解释（禁用必有原因）、不打断（抽屉）、反馈即时（Toast + 乐观更新）、加载有骨架、一致的时间语言、Agent 与人并列可辨、甘特图是重点页面、看板四面板。

## 组件细则（沿用 v1 评审结论，颜色按本版）

- 统计芯片条：32px 高，`{colors.surface-1}` 底 + 细线，数字 14px/500，语义芯片前 6px 指示灯；选中 `{colors.accent-border}` 边 + `{colors.accent-bg}` 底；第一枚永远是「全部」。
- 头像：中文名取最后一个字，拉丁名取首字母；Agent 方形机器人图标；20 / 24 / 32px 三档。
- ID 只出现在详情页面包屑和侧栏「ID」行（等宽遥测色 + 复制）；列表不显示 ID；行高 40–44px；单元格不超过两行。
- 表格：非文本列固定宽度；行内动作固定在最后一列，悬停出现，不抖动。
- 动作条：只有向前推进的步骤用主按钮；阻塞 / 解除 / 提问用次级；取消 / 打回 / 测试不通过 / 不修复用危险；三组之间细线；禁用步骤置灰并在气泡里解释；指派 / 附上交付物 / 写评论为右侧第三级按钮。
- 动态：默认 10 条 + 「查看全部 (N)」；本任务页不重复任务名后缀；同一人 5 分钟内的连续动作合并。
- 目标树：每层缩进 20px + 引导线；有子项的行有折叠箭头；行内含负责人、三个带标签的小指标、进度条与百分比、超预算标签；悬停动作查看 / 新建子目标 / 新建任务。
- 抽屉表单：按「基本 / 安排 / 人员」分组；短字段两列；日期控件与输入框一致。
- Agent 列表：「N 项授权」折叠 + 气泡；能力标签最多 3 个；状态灯 + 在线/离线 + 相对时间；「最多同时 N 个」。
- 成员列表：只在停用时显示状态标签。
- 看板：短日期坐标轴、隔一显示、柱子悬停数值；大数字 24px/600 + 12px 说明基线对齐。
- 小指标面板：数字 20px/500 + 说明，不带面板头。
- 品牌页：徽标与字标同一行；标题最多两行；卡片 400px。

## 禁止

- 第二个强调色、渐变、玻璃拟态、投影、霓虹描边、扫描线动画、粒子爆炸、赛博朋克式的青色 + 品红。
- 硬编码任何颜色（组件里只能出现 token：Tailwind 的 `bg-surface-1` 之类，或 `var(--c-*)`）；在品牌页提供浅色。
- 粗于 600 的字重；正文与按钮粗于 500。
- 用图标代替文字按钮；emoji。
