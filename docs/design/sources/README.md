# 设计规范来源

- `sentry-DESIGN.md`：getdesign.md 发布的 Sentry DESIGN.md（`npx getdesign@latest add sentry` 取得，2026-09-03），是对 sentry.io 营销站设计语言的解读：午夜紫罗兰画布、电光青柠强调色、Rubik 字体、单一主按钮层级。
- `sentry-design-system-SKILL.md`：getsentry/sentry 仓库 `.agents/skills/design-system/SKILL.md`，Sentry 产品界面的布局与排版原语规范（Container/Flex/Grid/Stack/Text/Heading、语义化 token）。
- `scraps-tokens-typography.tsx`、`scraps-tokens-size.tsx`：getsentry/sentry 仓库 `static/app/utils/theme/scraps/tokens/` 的产品级 token（字号、间距、圆角、断点）。
- 产品调色板取自同目录 `color.tsx` 的 light 系列（见 `../DESIGN.md` 里的 colors）。

`web/DESIGN.md` 是 AxiomOS 采用的规范，由以上来源合成：产品界面用 Sentry 产品 token，外壳与登录等品牌面用营销站的午夜紫罗兰与青柠。

## v2（2026-09-03，ADR 0010）

- `linear-DESIGN.md`：getdesign.md / awesome-design-md 发布的 Linear DESIGN.md（linear.app 营销站分析）：#010102 画布、四级表面阶梯、细线、薰衣草蓝 #5e6ad2 唯一强调色、无阴影。`web/DESIGN.md` v2 以它为蓝本，并叠加"Axiom 母舰"科幻层；Sentry 来源保留作历史参考。

## 协作复盘（2026-09-08）

- `collab-retro-brief-2026-09-08.md`：一个 Agent 在 AxiomOS 里把目标「企业员工的 Agent 能够快速的接入到系统」从分解做到交付的一天：背景、关键事实、Agent 自己的初步判断、完整动态流水。
- `collab-retro-codex-2026-09-08.md`：Codex 对上面材料的独立评审：找出流水里隐含的九个协作问题，逐条批判 Agent 的判断，按 P0 / P1 / P2 给出调整方案，并把问题归因到 Agent 行为与系统两侧。
