# ADR 0011：品牌页使用 WebGL 实时场景

日期：2026-09-04 · 状态：已采纳 · 关联：ADR 0010

## 背景

登录、邀请、后台登录三页承担"AxiomOS 取名自《机器人总动员》的母舰 Axiom"这一品牌叙事。此前用 SVG + CSS 实现星空与母舰，用户评价"很没质感，缺少科幻色彩"。调研（`docs/design/sources/scifi-web-animation-survey.md`）表明廉价感有明确的技术根源：没有真实光源与辉光，星点没有独立相位和尺寸衰减，渐变必有色带，运动各自为政。

## 决策

品牌页背景改为 three.js + react-three-fiber + postprocessing 的 WebGL 2 实时场景：程序化建模的母舰（哑光白 PBR 船体、HDR 自发光舷窗、船头舷窗带、引擎辉光）、三层着色器星场、fbm 星云、Bloom → ACES 色调映射 → 胶片颗粒 → 暗角的后期链、带阻尼的镜头视差与开场推进。实现细节以 `docs/design/sources/threejs-implementation-notes.md` 为准。

约束：
- 场景只出现在品牌页，通过 `next/dynamic`（`ssr: false`）延迟到水合之后加载；主应用不引入 three。
- 无 WebGL 2、软件渲染、`prefers-reduced-motion` 时退回 Canvas2D 静态星空 + 原 SVG 母舰；加载期间同样显示该静帧。
- DPR 上限 1.5；所有运动以时间为基、`delta` 夹取到 1/30；标签页隐藏时停帧。
- 母舰为原创"Axiom 风格"客轮，不复刻迪士尼/皮克斯的具体造型，不引入第三方模型资产。

## 代价

- 前端增加约 250–300 KB gzip 的异步包，仅品牌页加载。
- `postprocessing` 锁定 `three < 0.186`，升级 three 需同步等它放宽。
- 场景调参需要在真机（Apple Silicon Safari、Windows Chrome）上看，截图不能代替。

## 被否决的方案

- 纯 CSS / SVG：正是被否决的现状。
- 视频背景：不能跟随主题与分辨率，4MB 起，需要暂停按钮。
- 第三方 Axiom 粉丝模型：CC-BY 不能覆盖迪士尼的造型权利。
- WebGPU 唯一路线：drei / postprocessing 生态尚未跟上。
