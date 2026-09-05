# 登录页星舰场景：交互、质感与专业管线调研

调研日期：2026-09-04。承接前两份报告（`scifi-web-animation-survey.md`、`threejs-implementation-notes.md`），不重复其中已写的星空/星云/Bloom/相机阻尼内容。现状：three r185 + R3F 9.7 + drei 10.7 + postprocessing 6.39 + @react-three/postprocessing 3.1，程序化白色客轮、~730 盏 HDR 实例化舷窗、舰首窗带、引擎辉光、5000 颗三层视差星、烘焙 fbm 星云、Bloom→ACES→Noise→Vignette、阻尼鼠标视差、2.5 s 开场推进。

负责人的判断是两件事：**画面不"活"（缺少对鼠标/键盘的响应）**，**质感还差一档**。下面 A 节解决前者，B 节解决后者，C 节回答"有没有更专业的展示方案"。每一项都给来源链接和诚实的工时估计（以"一个会话"≈ 4–6 小时 Claude Code 协作计）。凡是没有查到一手资料的，直接写明。

版本核对（2026-09-04，npm registry）：`camera-controls` 3.1.2（drei `CameraControls` 的底层）、`n8ao` 2.0.1（peer `three >=0.137`、`postprocessing >=6.30`）、`@splinetool/runtime` 2.0.34、`@react-three/postprocessing` master 的 `src/index.ts` 导出 `N8AO`（来自 `./passes/N8AO`）、`LensFlare`、`Autofocus`、`LUT`、`GodRays`、`SMAA`、`ShockWave` 等。

---

## A. 让场景"活"起来的交互模式（不变成玩具）

先说总原则，来自 2026 年获奖站点的共同点（Hon Tran 评审总结，前一份报告已引）："3D 从不压过内容"。登录页的主角是表单，场景只能**回应**用户，不能**索取**注意力。所以下面每一条都遵守：输入只改变"目标值"，真正的运动永远经过阻尼（`MathUtils.damp`，已有）；幅度以角度计不超过个位数；任何响应都不改变表单的焦点、布局或可读性。

### A1. 注意力跟随：船缓慢转向光标（damped lookAt）

- 谁在做：Unseen Studio 的 Hubtown（2026 Awwwards SOTD）——"3D hero monolith + mouse-reveal"，光标经过处揭示几何与光照细节；Awwwards 的 "Hero mouse cursor" 灵感集里大量首屏都是"单个精心打光的物体 + 光标响应"。来源：https://www.utsubo.com/blog/best-threejs-websites-2026 ；https://www.awwwards.com/inspiration/hero-mouse-cursor-arcade-1
- R3F 做法：不要直接 `lookAt(pointer)`（会把船甩来甩去）。在 `useFrame` 里把 `state.pointer`（-1..1）映射成目标欧拉角 `yaw = pointer.x * 0.05`、`pitch = pointer.y * 0.03`（约 ±3°/±1.7°），再 `group.rotation.y = damp(rotation.y, yaw, 1.5, dt)`。λ 取 1.5（比相机视差的 4 更慢），船"注意到"光标要花半秒——这正是大质量物体的反应速度。three 论坛里"让模型看向光标并限制最大角度"的经典讨论：https://discourse.threejs.org/t/getting-object-to-look-at-mouse-cursor/45712
- 为什么高级：**相机视差 + 物体反向微转**造成两层运动差，观众感知到"体积"而不是"贴图在滑动"。
- 工时：0.5 h（已有 CameraRig 的模式，加一个 group 即可）。

### A2. 拖拽环绕（有限制、自动回正）与滚轮推拉

- 两个 drei 组件，选一个：
  - `PresentationControls`：专为"展示一个物体"设计，`polar={[-0.1, 0.15]}`、`azimuth={[-0.35, 0.35]}` 限制角度，`snap={{ mass: 2, tension: 120, friction: 30 }}` 松手回正（react-spring 弹簧配置），`global` 让整个画布可拖，`cursor` 自动切换抓手光标，`speed`、`zoom` 缩放灵敏度。来源：https://drei.docs.pmnd.rs/controls/presentation-controls ；源码 https://github.com/pmndrs/drei/blob/master/src/web/PresentationControls.tsx
  - `CameraControls`（yomotsu camera-controls 3.1.2）：`minPolarAngle/maxPolarAngle/minAzimuthAngle/maxAzimuthAngle/minDistance/maxDistance`、`smoothTime`（默认 0.25，建议 0.6–0.8 让它"重"）、`draggingSmoothTime`、`boundaryFriction`；`rotateTo(az, pol, true)`、`dollyTo(d, true)`、`setLookAt(..., true)`、`reset(true)` 都带过渡；事件 `controlstart/controlend/rest/sleep`；把 `mouseButtons.right = ACTION.NONE` 关掉平移，`mouseButtons.wheel = ACTION.DOLLY` 保留滚轮推拉并夹在 `minDistance 9 / maxDistance 15`。来源：https://github.com/yomotsu/camera-controls/blob/dev/readme.md ；drei 页 https://drei.docs.pmnd.rs/controls/introduction
- 建议：登录页用 `PresentationControls` 包住船（只转船不转相机，星空不跟着转，视差层次得以保留），`snap` 回正；滚轮推拉不要给——登录页有可能出现页面滚动，滚轮被 canvas 吞掉是可用性事故。若一定要，只在指针悬停在船的包围盒内才启用 `zoom`。
- 为什么高级：**有限制的自由**。可以拖，但永远拖不出难看的角度，松手会回到导演机位（前一份报告 SpaceX Flight 12 的"预排镜头轨迹"原则）。
- 工时：1 h（PresentationControls）；2 h（CameraControls，需处理与现有 CameraRig 的相机所有权冲突）。

### A3. 光标邻近效果：靠近的舷窗更亮、星尘让路、尾流涟漪

- 实现：把 `state.pointer` 射线与船轴平面求交（R3F 事件里有 `unprojectedPoint`，或 `raycaster.ray.intersectPlane`），得到世界坐标 `P`。舷窗 `InstancedMesh` 已经用 `instanceColor` 存 HDR 值，每帧对 `|window_i - P| < r` 的实例把亮度目标乘 1.6，再 damp 回去——只更新一小批实例的 `instanceColor`，成本忽略。来源（R3F 里光/物体跟随鼠标的射线做法）：https://discourse.threejs.org/t/make-spotlight-follow-mouse-in-three-fiber/40773 ；https://r3f.docs.pmnd.rs/api/events
- 再加一盏 `pointLight`（accent 色，`intensity 0.4, distance 4`）跟着 `P` 走（damp λ=3），船体在光标附近会真的被照亮，不只是舷窗变亮。
- 星尘让路 / 涟漪属于"锦上添花"：Codrops 的水波扭曲（在离屏 canvas 上画衰减圆，作为位移贴图）https://tympanus.net/codrops/2019/10/08/creating-a-water-like-distortion-effect-with-three-js/ ；实例化粒子的鼠标斥力 https://tympanus.net/codrops/2023/12/13/creating-an-interactive-mouse-effect-with-instancing-in-three-js/ 。太空没有介质，涟漪在语义上是错的，**建议不做涟漪**，只做"舷窗被注意到"和"尘埃被轻推"。
- 为什么高级：光标成为**场景里的一个光源**，而不是 DOM 层的一个箭头；Hubtown 的 mouse-reveal 本质就是这个。
- 工时：舷窗邻近 1.5 h；跟随点光 0.5 h；尘埃斥力 2 h（需要自定义 points 着色器加 uniform）。

### A4. 键盘响应：敲键发出信号、Enter 触发转场

- 诚实说明：我没有找到任何获奖站点把"表单打字"映射到 3D 场景；键盘驱动 3D 的著名案例是 Bruno Simon 的作品集（方向键开车，https://www.awwwards.com/brunos-portfolio-case-study.html ），与登录场景不是一回事。最接近的先例是 Darin Senneff 的 Yeti 登录表单：邮箱输入时头像视线跟着光标位置走，密码框聚焦时捂眼——2D SVG + GSAP，是"表单驱动角色"的事实标准。来源：https://codepen.io/dsenneff/details/NyVrzB ；https://codemyui.com/signup-password-field-reaction-yeti-mascot-login-form/
- 实现：**不要用 drei `KeyboardControls`**——它是给游戏输入设计的全局键位映射（https://drei.docs.pmnd.rs/controls/keyboard-controls ），我没有核实它是否会 `preventDefault`，而登录表单绝不能冒这个险。直接在 `<form onKeyDown>` 里做：每次按键往一个 zustand/ref 队列里推一个事件；`useFrame` 消费队列：随机选 3–5 盏舷窗把 `instanceColor` 目标乘 1.4 后 400 ms 衰减回来（像船上有人回应一次通讯），舰首窗带 `emissiveIntensity` 短促 +15%；节流到 ≤ 8 次/秒，且**必须**满足 WCAG 2.3.1（1 秒内 ≤3 次可感知闪烁、面积小）——舷窗只占屏幕极小面积，且每次只是 +40% 亮度，安全。来源：https://www.w3.org/WAI/WCAG22/Understanding/three-flashes-or-below-threshold.html
- Enter/提交：见 A7。
- 为什么高级：**输入有回声**。用户第一次意识到"船在听"的那一刻就是记忆点，但它小到不打扰输入。
- 工时：1.5 h。

### A5. 空闲"吸引模式"：N 秒无操作后缓慢环绕

- three 原生做法是 `OrbitControls.autoRotate`，社区早就发现它在用户交互时也在转，PR #5671 加了"交互中暂停"：https://github.com/mrdoob/three.js/pull/5671 ；论坛"空闲时相机旋转"讨论：https://discourse.threejs.org/t/idle-user-perspective-camera-rotation/15056
- 我们的实现：在 `CameraRig` 里维护 `lastInputAt`（pointermove/keydown/focus 都重置）。空闲 > 12 s 后，把相机目标切到一条**预排的**慢轨道：`θ = sin(t·0.05)·0.12`（约 ±7°，一圈两分钟），船自身的 `Float` 幅度也放大 1.5×；任何输入后 damp 回导演机位。用 CameraControls 时可以监听 `sleep` 事件启动计时，`controlstart` 取消。
- 为什么高级：让"没人动鼠标"时画面仍有一条**电影运镜**，而不是死掉；同时因为轨道是预排的，永远不会出现难看的角度。
- 工时：1 h。

### A6. 表单焦点响应：聚焦邮箱时相机推向舰桥，密码时窗带收敛

- 先例：Yeti（同 A4）。R3F 里没有现成组件，做法是把 DOM 焦点事件写进一个共享 store：`focus: 'email' | 'password' | null`。`CameraRig` 按它选择目标：`email` → 相机目标 z 从 11 推到 10.2、lookAt 从船中段移到舰首窗带（约 1 秒的 damp，λ=2）；`password` → 舷窗整体亮度目标乘 0.85（船"安静下来"），窗带亮度 -20%；`blur` → 回到默认。
- 关键约束：只改 3D 目标值，**永远不动 DOM 焦点**，不在焦点变化时插入任何会影响输入框位置的东西；`prefers-reduced-motion` 下把 λ 提到 20（几乎瞬时，无运动感）或直接不做。
- 为什么高级：场景对**用户所处的步骤**有反应，而不只是对鼠标坐标有反应——这是"活"与"跟随"的区别。
- 工时：1.5 h。

### A7. 成功 / 失败转场

- 成功：引擎辉光 sprite 目标 scale ×2.2、`pointLight` 强度 ×3；同时把星层的 z 速度从 0 拉到一个大值并把相机 fov 从 32 推到 40（fwdtools 的 warp 实现证明**透视除法 + fov 变宽 + 点尺寸翻倍**就是"曲速"的全部，2600 颗点足够，不需要自定义着色器）。0.9 s 后路由跳转，期间表单淡出。来源：https://fwdtools.com/ui-snippets/three-starfield-warp/ ；三层星空的着色器已在上一份报告，只需给 `uWarp` uniform 拉伸 `gl_PointSize` 的 y 方向。
- 失败：**不要**全屏闪红。做法：冷色轮廓光 200 ms 内切成 `#ff6b5c`、强度 ×1.4，再 600 ms damp 回去；舷窗不动。react-postprocessing 有 `ShockWave` 效果（源码 `src/effects/ShockWave.tsx`）可以做一次从船心扩散的微弱波纹，但登录失败用它太"游戏"，先不用。
- 为什么高级：成功转场把"登录"这个动作变成"起航"——叙事闭环；失败只是一次轻微的仪表警告。
- 工时：成功 2 h；失败 0.5 h。

### A8. 登录卡片 3D 倾斜（跟随光标）

- 库：`react-parallax-tilt`（约 3 kB，`tiltMaxAngleX/Y`、`perspective`、`glareEnable`、`glarePosition`、`onMove`），https://github.com/mkosir/react-parallax-tilt 。
- 参数：`tiltMaxAngleX={3} tiltMaxAngleY={4} perspective={1400} transitionSpeed={900} glareEnable glareMaxOpacity={0.06}`——比它默认值小一个量级。
- 门控：只在 `matchMedia('(hover: hover) and (pointer: fine)')` 为真且未开启减少动效时挂载；触屏与陀螺仪一律不做。来源：https://www.smashingmagazine.com/2022/03/guide-hover-pointer-media-queries/ ；https://css-irl.info/detecting-hover-capable-devices/
- 注意 `web/DESIGN.md` 的克制原则：倾斜会让卡片边缘的文字抗锯齿变差，`will-change: transform` + 整数像素尺寸能减轻；如果视觉审查觉得"像 2015 年的 tvOS 海报"，就把 glare 去掉只留 2° 倾斜。
- 工时：0.5 h。

### A9. 无障碍与"不打扰表单"的硬约束

- WCAG 2.3.3（AAA）：由交互触发的动画必须可关闭——`prefers-reduced-motion` 时 A1–A8 全部退化为"目标值直接落定"，且提供页面内开关（前一份报告已建议）。来源：https://www.w3.org/WAI/WCAG21/Understanding/animation-from-interactions.html
- 键盘焦点永不被劫持：canvas 容器 `aria-hidden`、`tabIndex={-1}`、`pointer-events: none`（A2 需要拖拽时只在船的区域内开启 `pointer-events`），所有键盘监听挂在 `<form>` 而非 `window`。
- 打字时的运动只允许发生在**远离输入框的区域**（船在右侧，表单在左侧）且亮度变化 ≤ 40%，绝不改变卡片自身。
- 标签页隐藏 / `frameloop='demand'` 时，所有队列清空，避免回到前台时"补放"一串脉冲。
- 工时：包含在各项内，额外 1 h 做一次全量走查。

**A 节合计：一个会话（约 6 h）可以完成 A1、A4、A5、A6、A7、A8、A9；A2、A3 放第二个会话。**

---

## B. 质感杠杆（按"视觉收益 / 工时"排序，API 已按当前版本核对）

前提：现有链路是 `EffectComposer(multisampling 4, HalfFloat)` → Bloom → ToneMapping → Noise → Vignette。下面的顺序是我的建议排序，不是文档排序。

### B1. 船体材质：`MeshPhysicalMaterial` 清漆层（收益高 / 工时 0.5 h）

- 白色客轮的"贵"来自漆面：底层哑光、表面一层薄清漆抓住轮廓光。参数起点：`color #d6dae1, metalness 0.05, roughness 0.55, clearcoat 0.6, clearcoatRoughness 0.28, envMapIntensity 0.8`；`sheen` 是给织物的，这里不用。来源：https://threejs.org/docs/pages/MeshPhysicalMaterial.html
- 必须配合有方向性的环境（现有 `Lightformer` 房间）才看得出清漆，否则只是变亮。

### B2. 程序化法线 / 粗糙度贴图：分段板线（收益高 / 工时 2 h）

- 船体是 `LatheGeometry`，自带 UV，所以不需要三平面映射；在离屏 canvas 上画：横向分段线（1 px 深槽）、纵向舱段接缝、随机 2–4 px 的检修板矩形，然后用 Sobel 得到法线贴图（`NoColorSpace`），`normalScale 0.35`；同一张图的板块随机 ±0.08 写进 `roughnessMap`。板线在轮廓光下会出现细微高光断裂，这是"有人造过这艘船"的证据。
- 若以后换成无 UV 的布尔几何再考虑三平面：Ben Golus 的三平面法线正确做法 https://bgolus.medium.com/normal-mapping-for-a-triplanar-shader-10bf39dca05a ；Ronja 教程 https://www.ronja-tutorials.com/post/010-triplanar-mapping/

### B3. N8AO 环境光遮蔽（收益中高 / 工时 1 h）

- `@react-three/postprocessing` 直接导出 `N8AO`（`src/passes/N8AO.tsx`，包装 `n8ao` 的 `N8AOPostPass`）。props：`aoRadius`(默认 5)、`distanceFalloff`(1)、`intensity`(1)、`quality: 'performance'|'low'|'medium'|'high'|'ultra'`（映射到 `setQualityMode`）、`aoSamples`(16)、`denoiseSamples`(4)、`denoiseRadius`(12)、`color`、`halfRes`、`screenSpaceRadius`、`depthAwareUpsampling`(true)、`renderMode`(0)。来源：https://raw.githubusercontent.com/pmndrs/react-postprocessing/master/src/passes/N8AO.tsx ；`n8ao` README：https://github.com/N8python/n8ao
- 起点：`<N8AO halfRes quality="medium" aoRadius={0.6} distanceFalloff={0.4} intensity={1.6} color="#06070c" />` 放在 Bloom 之前。它需要 WebGL2 + 深度纹理（现有链路满足），**不支持 WebGPU**（README 明示）。船只有一个大曲面，AO 主要出现在上层建筑与船体交接、推进翼根部——正是现在"塑料感"的来源。移动/lite 档关掉。

### B4. 抗锯齿：MSAA 4 保留，不上 SMAA（收益低 / 工时 0.5 h）

- react-postprocessing 文档：SMAA 是 WebGL1 或 MSAA 出现伪影时的替代，使用时必须 `multisampling={0}`，且 SMAA 是异步的要包 Suspense。来源：https://react-postprocessing.docs.pmnd.rs/effects/smaa
- 船体边缘在 4× MSAA 下已经够；真正让边缘"闪"的是 bloom 阈值附近的像素（上一份报告 §7.3），先调 `luminanceSmoothing`。

### B5. 前景星尘（收益中 / 工时 1 h）

- drei `Sparkles`：`count`(100)、`speed`(1)、`opacity`(1)、`color`、`size`、`scale`、`noise`(1)，全部可传 `Float32Array` 逐粒子控制，并允许自定义着色器（暴露 `time/pixelRatio` uniform）。来源：https://drei.docs.pmnd.rs/staging/sparkles
- 起点：两组——近景 `count 60, size 4–9, speed 0.15, opacity 0.35, scale [10, 6, 4]` 放在相机与船之间，远景 `count 200, size 1–2, opacity 0.2`。想让尘埃"感光"（靠近舷窗/引擎的更亮），需要自定义片元着色器按到光源的距离乘亮度——这就是 A3 里"尘埃让路"的同一份代码。

### B6. 薄景深：`Autofocus` 而非手调 `DepthOfField`（收益中 / 工时 0.5 h）

- `DepthOfField` props：`focusDistance`(0, 归一化 0–1)、`focalLength`(0.1)、`bokehScale`(1)、`width/height`。`Autofocus` 继承它并加 `target`、`mouse`、`smoothTime`(0.25)、`manual`、`debug`。来源：https://react-postprocessing.docs.pmnd.rs/effects/depth-of-field ；https://react-postprocessing.docs.pmnd.rs/effects/autofocus
- 起点：`<Autofocus target={[0.8, 0, 0]} smoothTime={0.6} focalLength={0.02} bokehScale={1.4} />`——只让近景尘埃和远景星微糊，船清晰。**登录面板在 DOM 层，不受影响。** 移动端关。

### B7. 背景行星 / 月球 + 大气边缘（收益高 / 工时 3 h）

- 最紧凑的 GLSL：外层球体（半径 ×1.12）用 `side: BackSide, blending: AdditiveBlending, transparent`，片元 `float f = pow(0.7 - dot(vNormal, vec3(0,0,1)), 2.0); gl_FragColor = vec4(color * f, f);`——这是 three 论坛与 Franky Hung 教程共同采用的"大气光晕"配方；内层球体再叠一层 Fresnel 让暗面边缘泛蓝。来源：https://discourse.threejs.org/t/how-to-create-an-atmospheric-glow-effect-on-surface-of-globe-sphere/32852 ；https://franky-arkon-digital.medium.com/make-your-own-earth-in-three-js-8b875e281b1e ；更完整的日夜/大气：https://threejs-journey.com/lessons/earth-shaders
- 起点：行星放在船的后下方、只露出 1/4 弧，颜色取 DESIGN.md 的 accent 冷色，大气 `color (0.45,0.6,1.0)`，`f` 再乘 1.8 交给 Bloom；行星本体用无贴图的 `MeshStandardMaterial` + 3 层 fbm 的 `CanvasTexture` 做云带。行星的作用是**尺度参照**（Digikore 的原则）——船在一颗星球前面才显得大。

### B8. 护航小艇沿路径飞行（收益中 / 工时 1 h）

- `new CatmullRomCurve3(points, true)`，`useFrame` 里 `t = (clock.elapsedTime * 0.01) % 1`，`shuttle.position.copy(curve.getPointAt(t))`，`shuttle.lookAt(curve.getPointAt((t + 0.01) % 1))`。来源：https://threejs.org/docs/#api/en/extras/core/Curve.getPointAt ；DEPT 的电影相机路径文章 https://www.deptagency.com/en-us/insight/coding-a-cinematic-camera-path/
- 小艇就是 0.02 单位的 `CapsuleGeometry` + 一个 HDR 尾灯 sprite，3 艘，各自不同速度。它们不是装饰，是"这艘船有几公里长"的尺子。

### B9. 太阳圆盘 + GodRays / 镜头光斑（收益中 / 工时 2 h，风险：抢戏）

- `GodRays` 需要一个 `forwardRef` 的太阳 mesh，props：`samples 60, density 0.96, decay 0.9, weight 0.4, exposure 0.6, clampMax 1, kernelSize SMALL`；要正确遮挡需 `<EffectComposer autoClear={false}>`。来源：https://react-postprocessing.docs.pmnd.rs/effects/god-rays
- `LensFlare`（基于 ektogamat 的 Ultimate Lens Flare，已并入 @react-three/postprocessing）：`position, glareSize, starPoints, ghosts, haloScale, flareShape, flareSize, opacity, colorGain, secondaryGhosts, additionalStreaks, occlusion`；遮挡靠射线检测，大场景建议包 `<Bvh>`；`userData={{ lensflare: 'no-occlusion' }}` 排除物体。来源：https://react-postprocessing.docs.pmnd.rs/effects/lensflare ；https://github.com/ektogamat/R3F-Ultimate-Lens-Flare
- 建议：太阳放在画面**外**的左上，只让 GodRays 的光束从边缘扫进来（`weight 0.25, exposure 0.35`），不放 LensFlare——登录面板在左侧，光斑鬼影会压在文字上。替代方案 `three-good-godrays`（真正的体积光，需要阴影贴图，peer 只到 three 0.182）暂不兼容 r185：https://github.com/Ameobea/three-good-godrays

### B10. 环境贴图：自托管 HDRI 还是继续用 Lightformer（收益低 / 工时 1 h）

- 诚实结论：**Poly Haven 没有深空 HDRI**。它的夜空（Rogland Clear Night、Dikhololo Night、Qwantani Night Pure Sky）都带地平线或地面，用于反射会在船腹映出地形。来源：https://polyhaven.com/hdris/skies/night
- 体积参考（Poly Haven API，`rogland_clear_night`）：1k `.hdr` 1.69 MB、1k `.exr` 6.09 MB、2k `.hdr` 6.84 MB。来源：https://api.polyhaven.com/files/rogland_clear_night 。RGBELoader 直接读 `.hdr`；想压到几百 KB 用 `@monogrid/gainmap-js` 的 `HDRJPGLoader` 读 gain-map JPEG（官方在线编码器 gainmap-creator.monogrid.com；README 没给具体压缩比数字，不引用传闻）。来源：https://github.com/MONOGRID/gainmap-js
- 真正的深空全天图：NASA SVS "Deep Star Maps 2020"（Gaia 17 亿颗星，4k–64k 等距柱状 EXR，署名 NASA/GSFC SVS 与 ESA/Gaia/DPAC），可作背景球或烘进星云。来源：https://svs.gsfc.nasa.gov/4851
- 建议：**光照继续用 Lightformer**（暖主光 + 冷轮廓光是导演布光，HDRI 反而不可控）；如果要"真实反射"，把 NASA 星图 2k 版转成 gain-map JPEG 作 `Environment` 的 `files`，`environmentIntensity 0.3`。

### B11. 色彩分级 LUT（收益低中 / 工时 1 h）

- `<LUT lut={texture} tetrahedralInterpolation blendFunction={BlendFunction.SRC} />`，纹理由 `postprocessing` 的 `LUTCubeLoader` 读 `.cube`（32³ 或 64³）。来源：https://react-postprocessing.docs.pmnd.rs/effects/lut ；https://pmndrs.github.io/postprocessing/public/docs/class/src/loaders/LUTCubeLoader.js~LUTCubeLoader.html
- 放在 ToneMapping 之后、Noise 之前。价值在于把"冷蓝深空 + 暖舷窗"这两种色温一次锁定，而不是各处手调。`.cube` 可以用 Blender 或 DaVinci 导出；没有调色师时，先用一条 5 参数的自制 S 曲线（`BrightnessContrast` + `HueSaturation`）代替。

### B12. 体积星云替代方案（收益中 / 工时 3–6 h，暂缓）

- 现有 fbm 烘焙星云是正确的成本档。若要"有厚度"：CK42BB 的 procedural-stars-threejs 在 WebGL2 上用 **billboard sprite 层叠**模拟 5 类星云（WebGPU 才用 raymarching），思路可借用——20–40 张软噪声 sprite、不同深度、additive、随视差错开。来源：https://github.com/CK42BB/procedural-stars-threejs 。真体积 raymarching（Maxime Heckel）与 three 官方 `webgpu_volume_cloud` 只在 WebGPU 或高端 GPU 上合算：https://blog.maximeheckel.com/posts/real-time-cloudscapes-with-volumetric-raymarching/ ；https://threejs.org/examples/webgpu_volume_cloud.html

### B13. Blender 烘焙 AO（收益中高 / 见 C1）

只有换成 glTF 船体时才有意义；程序化船体上 N8AO（B3）是等价的实时替代。

**B 节一个会话能做的组合：B1 + B2 + B3 + B5 + B6（约 5 h），视觉变化最明显；B7 + B8 是第二个会话；B9/B10/B11/B12 视审美评审再定。**

---

## C. "更专业"的展示管线：面向独立开发者 + Claude Code + Apple Silicon 的诚实对比

### C1. Blender 脚本建模（bpy）→ 烘焙 → glTF → R3F

- 安装与无头运行：`brew install --cask blender`（当前 5.2.0，`blender@lts` 同为 5.2，要求 macOS ≥ 11），https://formulae.brew.sh/cask/blender 。`blender -b -P build_ship.py -- --seed 3`（`-b` 后台，`-P` 脚本，`--` 之后是脚本参数），或 `--python-expr "import bpy; bpy.ops.export_scene.gltf(filepath='ship.glb')"`。来源：https://github.khronos.org/glTF-Tutorials/BlenderGltfConverter/ ；https://til.jakelazaroff.com/blender/export-a-blender-file-to-glb-from-the-command-line/ ；命令行渲染参数（`-E CYCLES -s -e -o -F -a`，`-f/-a` 必须放最后）：https://docs.blender.org/manual/en/latest/advanced/command_line/render.html
- 能完全无 GUI 吗：**能**。建模用 `bpy.ops.mesh.primitive_*` + `bmesh` 挤出/放样剖面，`bpy.ops.object.modifier_add(type='SUBSURF'|'BEVEL'|'BOOLEAN')`，舷窗阵列用 `ARRAY` 修改器或 Geometry Nodes；烘焙 AO/法线用 Cycles 的 `bpy.ops.object.bake(type='AO'|'NORMAL'|'EMIT')`（Blender 文档站对抓取工具返回 403，我只能引用手册 URL 与社区脚本：https://docs.blender.org/manual/en/latest/render/cycles/baking.html ；AO 烘焙脚本 https://gist.github.com/AndrewRayCode/760c4634a77551827de41ed67585064b ；bevel→法线 + AO 自动烘焙插件 https://github.com/GarikDog/autobake_tools ）。Claude Code 的迭代闭环是 `blender -b -P script.py -f 1 -o /tmp/preview_#` 渲出一张预览 PNG，读图、改脚本、再渲——每轮 20–60 秒。
- 参考代码库：a1studmuffin 的 SpaceshipGenerator（MIT，2016）证明"盒子 → 多次挤出 → 按面朝向分类加细节（引擎、天线、灯）"的纯 bpy 流程可行；它的美学是战舰，不是客轮，但结构可抄。https://github.com/a1studmuffin/SpaceshipGenerator
- 质量收益（相对程序化 three 几何）：真实倒角（轮廓光在边缘有 1–2 px 的亮线）、Subsurf 的连续曲率（现有 Lathe 在船头/船尾接缝有可见折痕）、布尔切出的舱门/凹槽、烘焙 AO 进接缝、舷窗作为一张 emissive 贴图（730 个实例变 0 个 draw call），法线贴图板线。这是"从像模型到像照片"的那一档。
- 代价：包体 +1–3 MB（`gltf-transform optimize --compress meshopt --texture-compress webp`，上一份报告已写）；Blender 与 three 的色调映射要对齐——three r160 起有 `AgXToneMapping`，正是 Blender 4.0 默认的 AgX，预览渲染与浏览器会一致（https://github.com/mrdoob/three.js/releases/tag/r160 ；https://github.com/mrdoob/three.js/issues/27362 ）。现有链路用 ACES，要么两边都改 AgX，要么接受偏差。
- 工时：无头闭环搭建 0.5 天；建模迭代 2–3 天（无人能在视口里"看"，全靠渲图评审）；烘焙 + 导出 + R3F 接入 0.5–1 天。**共 3–5 天。**

### C2. Cycles 预渲染转台序列 + 光标映射 + 实时星空合成（Apple 法）

- 帧数与体积：60–180 帧足够；1080p WebP 60 帧约 3–5 MB（PNG 15–20 MB），AVIF 更小；Apple 自己在小屏/慢设备上退回静态图。来源：https://gsapvault.com/blog/scroll-image-sequence-tutorial ；https://www.jamesbattye.dev/articles/building-a-scroll-based-image-sequencer-with-gsap ；https://css-tricks.com/lets-make-one-of-those-fancy-scrolling-animations-used-on-apple-product-pages/
- 我们的变体：不是滚动驱动而是**光标驱动**——横向 ±12° 共 49 帧、纵向 ±4° 共 9 帧的 2D 网格（441 帧太多；实际做 49×1 加 3 档纵向 = 147 帧），带 alpha 的 WebP/AVIF，画到 `<canvas>` 后作为 R3F 里一张 `PlaneGeometry` 的贴图（`transparent`，`toneMapped false`），星空/星云/尘埃仍是实时的、仍有视差，Bloom 仍能拾取船的舷窗（把舷窗渲成单独的 emissive 层、超出 1.0 的 HDR 值存 16-bit 或用 gain-map）。
- 光照同步：Blender 场景里放与 `Lightformer` 同位置、同色温的面光；两边都用 AgX；渲染 3 张关键角度与浏览器截图并排对色。
- 质量 vs 带宽：画质是 Cycles 级（软阴影、GI、真反射），但船只能在预渲染的角度内转，Enter 起航时引擎"喷发"要另渲一段序列或退回实时 sprite；每次改船都要重渲 147 帧（M 系列 Apple Silicon Cycles GPU 每帧 1080p 中等采样约 10–40 秒，即 30–90 分钟一轮，这一数字是经验估计，未找到公开基准）。
- 工时：在 C1 模型完成之后再 1–2 天。

### C3. Spline / Unicorn Studio

- Spline：`@splinetool/runtime` 2.0.34 压缩后 961 KB / gzip 268 KB，另含 WASM（`hana-ui` 3.36 MB 未压缩），比整套 three + R3F + postprocessing 还重；免费版"web 导出带水印、不提供代码导出"，Hobby $12/座/月起去水印；导出为 React/Next/R3F 组件，`.splinecode` 可自托管。**没有任何文档说明可以在 GUI 之外编写场景**——它是设计师工具，Claude Code 无法迭代。来源：https://bundlephobia.com/api/size?package=@splinetool/runtime ；https://spline.design/pricing ；https://docs.spline.design/exporting-your-scene/web/exporting-as-code
- Unicorn Studio：运行时约 29–50 KB gzip；免费版带 logo 且无商业许可，Legend $14–20/月解锁去水印、商业许可、自定义 GLB（≤30 MB）与 JSON 导出给 `unicornstudio.js` 自托管；本质是 2D 着色器效果器（鼠标/滚动/悬停），3D 只是附属。同样 GUI 作者工具。来源：https://www.unicorn.studio/docs/faqs/ ；https://www.unicorn.studio/docs/embed/
- 结论：两者都不适合本项目——不能由 agent 编写、运行时或许可都是额外成本，而且做不出"HDR 舷窗 + Bloom + 自定义星空"这种需要自己写着色器的东西。

### C4. Needle Engine / PlayCanvas / Babylon.js

- Needle：商业许可、以 Unity/Blender 为编辑器，卖点是"把 Unity 场景导出到网页"。没有 Unity 工程时没有优势。来源（Needle 自己的对比页，注意是营销材料）：https://cloud.needle.tools/compare/needle-vs-playcanvas-vs-r3f
- PlayCanvas：引擎 MIT，可视化编辑器订阅制（私有项目收费）；优势在团队协作与配置器类项目。来源同上及 https://www.utsubo.com/blog/threejs-vs-babylonjs-vs-playcanvas-comparison
- Babylon.js：MIT，最小 ~300 KB，全功能 2 MB+，Inspector、物理、WebGPU 成熟度更好；但我们的瓶颈是**资产与布光**，不是引擎功能。来源：https://www.pkgpulse.com/guides/threejs-vs-react-three-fiber-vs-babylonjs-3d-webgl-2026
- WebGPU 补充：多篇 2026 文章称 three r182 起把 `WebGPURenderer` 列为推荐渲染器、覆盖率约 95%（https://app.cinevva.com/blog/2026-06-09-web-game-engines-2026-comparison ），但 `postprocessing` 6.39 与 `n8ao` 仍是 WebGL2 路线（n8ao README 明写不支持 WebGPU）。登录页继续 WebGL2，不迁。
- 结论：**换引擎没有质感收益**，只有迁移成本。

### C5. Rive 做 UI 层

- 运行时：WASM 约 78 KB gzip（`@rive-app/canvas-lite` 更小，无文本引擎），运行时开源；React 封装 `rive-react`。来源：https://help.rive.app/runtimes/overview/web-js/faq ；https://github.com/rive-app/rive-react
- 适合：登录卡片上的状态机微动效（输入校验图标、按钮加载态、Yeti 式角色）。不适合：船与星空。
- 障碍：Rive 文件只能在 Rive 编辑器（GUI）里做，Claude Code 无法产出 `.riv`。对没有设计师的团队，`motion`/CSS + SVG 就够。工时若走 Rive：学习 + 制作 1–2 天，且需要人手。

### C6. 带透明通道的预渲染视频

- 现实：Safari 只认 HEVC alpha，Chrome/Firefox 只认 VP9 WebM alpha，必须两份并把 HEVC 放在 `<source>` 第一位；同一素材 VP9 1.1 MB 对 HEVC 3.4 MB；Safari 对 WebM alpha 的支持"在路上但要很久"。来源：https://jakearchibald.com/2024/video-with-transparency/ ；https://rotato.app/blog/transparent-videos-for-the-web ；http://terhech.de/posts/2025-02-02-transparent-video-safari.html
- 结论：画质上限高，但对光标/键盘零响应——与负责人"画面要活"的诉求正相反。只作为 `lite` 档或 reduced-motion 的静态海报替代考虑，且那时静态图更省。

---

## 排序推荐

### 本会话就做（约 6 h，全部在现有 R3F 代码里）

1. **A1 注意力跟随 + A6 表单焦点响应 + A4 键盘脉冲**（3.5 h）——直接回答"缺少交互效果"，三者共用一个 `useSceneInput` store（pointer 目标、focus 状态、按键队列）。
2. **A7 成功起航 / 失败警示**（2.5 h）——登录页唯一的"大动作"，把品牌叙事闭合。
3. **A5 空闲吸引模式 + A8 卡片倾斜 + A9 无障碍门控**（2.5 h）——如果时间不够，A8 可以砍。
4. 顺手做 **B1 清漆 + B3 N8AO 半分辨率**（1.5 h）——两处参数级改动，质感立刻上一档。

### 第二个会话（约 6 h）

5. **B2 程序化法线/粗糙度板线**（2 h）、**B5 前景星尘 + A3 光标邻近舷窗/点光**（3 h）、**B6 Autofocus 薄景深**（0.5 h）。
6. **A2 PresentationControls 有限拖拽**（1 h）。

### 第三个会话

7. **B7 背景行星 + 大气**（3 h）与 **B8 护航小艇**（1 h）——尺度感是"太空感"的最后一块；再评估 **B9 边缘 GodRays**、**B11 LUT**。

### 需要更长资产管线的（3–5 天，独立立项）

8. **C1 Blender 无头 bpy 建模 + 烘焙 AO/法线/emissive + meshopt glTF**。这是从"程序化几何"到"像一艘造出来的船"的唯一路径，且 Claude Code 能通过渲图闭环独立迭代；前提是先把 A/B 两节做完，否则新模型进来后交互还得重做一遍。
9. **C2 Cycles 转台序列**只在 C1 完成后、且实时管线仍不满足画质时再考虑；它以牺牲"活"为代价换画质，与当前诉求有张力。

### 不做

- Spline / Unicorn Studio（无法由 agent 编写、许可与运行时成本）、换引擎（无质感收益）、透明视频（不能交互）、Rive（需要人开 GUI）。

一句话：**先让现有场景"听得见"用户（A），再给它一层漆和阴影（B1–B3），最后才是换船（C1）。** 前两步在两个会话内可完成，第三步是一个独立的 3–5 天项目。
