# 网页级"高质感"太空 / 科幻主视觉：技术调研

> 目标场景：Next.js 16 静态导出的登录页。深空、星云、视差星空、一艘缓慢漂移的大型客轮式星舰（WALL·E 的 Axiom）、引擎辉光、发光的舰首观景窗。
> 调研日期：2026-09-04。每条技术后附"为什么有质感"一句话与来源链接。凡是没有查到一手资料的地方直接说明，不编造。

---

## 一、参考站点与案例：什么让它们看起来"贵"

### 1. Lusion（Awwwards Site of the Month / FWA）
- 手法：把 Houdini 里算好的布料模拟烘成 ArrayBuffer/PNG（16-bit 整数编码 + 关键帧插值，移动端 246KB），实时只做混合；离线渲染（Redshift）的画面与实时 WebGL 精确对齐，再叠"实时反射、解析式体积光、可交互模糊文字"。
- 为什么有质感：**离线级光照 + 实时交互层**，把 GPU 预算花在观众能感知的部分（反射、体积光），而不是花在几何上。
- 来源：https://www.awwwards.com/case-study-for-lusion-by-lusion-winner-of-site-of-the-month-may.html ；Codrops 复刻 Lusion 曲线管道+光散射：https://tympanus.net/codrops/2021/05/17/curly-tubes-from-the-lusion-website-with-three-js/

### 2. Active Theory（Webby "Crafted with Code"）
- 手法：自研 WebGL 框架 Hydra，"以游戏引擎思维做网页"，ATv5 站点做了 8 个完整 3D 环境。
- 为什么有质感：场景是**连续的空间**而不是分页的贴图，镜头运动有连贯性。
- 来源：https://www.webbyawards.com/crafted-with-code/active-theory/ ；行业综述：https://www.psychoactive.co.nz/content-hub/best-webgl-interactive-3d-agencies

### 3. Unseen Studio（Hubtown，2026 Awwwards SOTD + Developer Award）
- 手法：每个场景渲染进独立 render target，最终用 fragment shader 做遮罩/畸变过渡（Fluid、Distortion、Dissolve）；后处理 god rays、流体、鼠标拖尾累积；用 Lenis 等平滑滚动库把滚动与舞台同步；"精细调过的 easing 与速度曲线"。
- 为什么有质感：**过渡发生在着色器里而不是 DOM 里**，所有运动共享同一条速度曲线。
- 来源：https://chipsa.design/publications/legendy-veb-dizaina-texniceskii-razbor-rabot-studii-unseen ；https://www.utsubo.com/blog/best-threejs-websites-2026

### 4. Interstellar《星际穿越》Endurance WebGL（AvatarLabs / xymatic / Warner Bros.）
- 手法：华纳官方模型经 Maya/Mudbox/ZBrush 重建并"优化"后放进 WebGL，桌面 + iOS 版本，环绕飞船的电影化镜头。
- 为什么有质感：**用真实影视资产，镜头只做环绕**，不炫技。
- 来源：https://www.awwwards.com/sites/interstellar-endurance-3d-tour ；https://www.behance.net/gallery/42246175/Interstellar-Endurance-WebGL-Experience ；https://experiments.withgoogle.com/interstellar-endurance-exploration
- 附：sirxemic 的开源虫洞/黑洞光线追踪 demo（WebGL，三档分辨率）：https://sirxemic.github.io/Interstellar/

### 5. SpaceX / Starship "Flight 12"（Zero x Infinity，2026）
- 手法：Three.js + GSAP，**滚动条即飞行时钟**的 scrub 时间线，33 台 Raptor 引擎点火实时渲染，全程稳帧；另有独立 3D 视角可环绕。
- 为什么有质感：动画进度由用户控制，但**镜头轨迹是预排的**，所以永远不会出现难看的角度。
- 来源：https://www.webgpu.com/showcase/flight-12-starship-launch-webgl/ ；https://flight12.vercel.app/
- 诚实说明：SpaceX 官网本身（spacex.com）我没有找到公开的技术拆解，不能断言它用 WebGL；它的主视觉以视频/影像为主。

### 6. NASA Eyes（JPL）
- 手法：浏览器内的实时 3D 太阳系，SIGGRAPH 2015 WebGL 专场展示，任何设备浏览器可跑。
- 为什么有质感：**数据真实**（真实轨道、真实贴图），太空感来自尺度而非特效。
- 来源：https://science.nasa.gov/eyes/ ；https://en.wikipedia.org/wiki/NASA's_Eyes

### 7. Bruno Simon 作品集 / Three.js Journey
- 手法：Blender 建模 + Three.js + Cannon.js 物理；课程里的"Galaxy Generator"用 ShaderMaterial 驱动的粒子做旋臂星系，是网页星云粒子的事实教科书；近年已迁到 WebGPU/TSL。
- 为什么有质感：**有重量感的物理与克制的镜头**，粒子在着色器里动而非在 JS 里逐个更新。
- 来源：https://www.awwwards.com/brunos-portfolio-case-study.html ；https://threejs-journey.com/ ；React 版课程 demo：https://github.com/pmndrs/threejs-journey

### 8. Apple 产品页
- 手法：不是实时 3D，而是**预渲染的图片序列**（几十到几百帧）按滚动位置画到 `<canvas>`，容器 pin 住。
- 为什么有质感：每一帧都是离线渲染的电影级画质，代价是资产体积和"只能沿一条轨迹看"。
- 来源：https://css-tricks.com/lets-make-one-of-those-fancy-scrolling-animations-used-on-apple-product-pages/

### 9. Stripe 渐变
- 手法：自研 ~10KB 的 minigl，顶点位移 + 多八度 simplex 噪声（fBm），用 sin/cos 对 UV 做时间扭曲；不在视口时用 ScrollObserver 关掉。
- 为什么有质感：**fBm 而不是单层噪声**，所以有"丝状"细节；颜色只有 4 个精挑的色标。
- 来源：https://kevinhufnagl.com/how-to-stripe-website-gradient-effect/ ；https://github.com/exzenter/gradient-stripe

### 10. Vercel / Linear 类主视觉
- 手法：多层背景 = 深底 + 大而柔的光斑 + 极低不透明度网格（10–20%，1px，16/24px 间距）+ 3–5% 噪点。
- 为什么有质感：**克制**——每一层都"几乎看不见"，噪点同时压住了渐变的色带（banding）。
- 来源：https://www.setproduct.com/blog/complete-guide-to-blueprint-grid-design ；https://css-tricks.com/grainy-gradients/

### 11. 2026 年获奖站点的共同点（Hon Tran 评审总结）
- "transitions that never call attention to themselves"、"a Three.js scene treats each project like a spotlit installation, with GSAP pacing the reveals"、"the 3D never overwhelms the work"。
- 来源：https://www.hontran.dev/blog/best-award-winning-websites-2026

### 12. 游戏/电影营销站的诚实说明
- Bethesda《Starfield》官网（https://bethesda.net/en-US/game/starfield）我没有找到 WebGL 技术拆解，它的主视觉是视频与静态图。《沙丘》相关的只有个人 WebGL 实验 ARRAKIS（https://discourse.threejs.org/t/arrakis-a-webgl-experiment/72507）。皮克斯/迪士尼官方页没有找到可引用的技术资料。**结论：影视级"太空感"在营销站上大多靠视频，真正实时的太空场景主要出现在工作室作品与个人实验。**

### 案例提炼：让太空场景"贵"的八条共性
1. 光照是主角：一盏强主光 + 一盏冷色轮廓光 + 环境贴图，而不是均匀的 ambient。
2. 亮的东西真的亮：自发光超出 0–1 范围，再由 Bloom 泛光，而不是贴一张"光晕 PNG"。
3. 镜头很慢，而且有惯性；大物体更慢（见第三节"尺度心理学"）。
4. 后处理是薄薄一层：Bloom + 轻微 Vignette + 2–5% 颗粒，没有 Chromatic Aberration 拉满。
5. 有深度层次：近处大颗粒星尘、中景飞船、远景星空与星云，各自视差不同。
6. 色彩分级统一：一个 tone mapper（ACES/AgX）+ 一个曝光值，全场景一致。
7. 没有硬边：星点是软圆而不是方块，渐变有抖动（dither）。
8. 运动由一条时间线统筹（GSAP/Theatre.js），而不是几十个各自 `requestAnimationFrame`。

---

## 二、技术与网页实现

### 2.1 基础栈：Three.js / react-three-fiber + drei
- `<Stars radius depth count={5000} factor saturation fade speed />`：着色器驱动、带闪烁的星空，一行代码。为什么有质感：`fade` 让星点边缘柔和，`depth` 给出远近层次。来源：https://drei.docs.pmnd.rs/staging/stars
- `<Sparkles count size speed noise />`：飘浮的发光尘埃，用作近景星尘/引擎尾迹粒子。来源：https://drei.docs.pmnd.rs/staging/sparkles
- `<Float speed rotationIntensity floatIntensity floatingRange={[-0.1,0.1]} />`：给飞船零成本的"漂"。来源：https://drei.docs.pmnd.rs/staging/float
- `<Environment files=... environmentIntensity background={false} />` + `<Lightformer>`：用 HDR/JPEG gainmap 环境贴图给金属船体反射；注意 `preset` 依赖 CDN，"不适合生产环境"。来源：https://drei.docs.pmnd.rs/staging/environment
- Lensflare：three.js 官方 addon `Lensflare/LensflareElement(texture, size, distance)`；R3F 生态有 Ultimate Lens Flare。来源：https://threejs.org/docs/pages/Lensflare.html ；https://github.com/ektogamat/R3F-Ultimate-Lens-Flare

### 2.2 星空：Points + 逐点噪声闪烁
- 用 `THREE.Points` 而非 InstancedMesh：EF-Map 的 24,018 颗星一个 draw call，RTX 4070S 上 0.87ms；作者明确说"每颗星都是面向屏幕的点，没有几何可实例化"，且"衰减曲线比颜色更重要"。来源：https://ef-map.com/blog/threejs-rendering-3d-starfield
- 闪烁写在着色器里：`sin(time * speed + random * 6.28)` 逐星独立相位；星点用 `smoothstep(0.5, 0.2, dist)` 做软圆；两半球各 7,500 颗固定 50 单位距离，避免透视拉伸。来源：https://www.richardfu.net/solving-starfield-perspective-distortion-in-3d-space-a-three-js-case-study/
- 为什么有质感：软圆 + 独立相位 + 极小的尺寸差异（EF-Map 只有 3% 的星放大 1.6 倍）读起来像真实星空；方块点与整体同步闪烁读起来像 2005 年的 Flash。
- 自定义属性做法（InstancedBufferAttribute 存大小/颜色/相位）：https://tympanus.net/codrops/2019/01/17/interactive-particles-with-three-js/

### 2.3 星云：GLSL fBm / 域扭曲 / 体积光
- fBm 基础：https://thebookofshaders.com/13/ ；域扭曲 `f(p + h(p))`（Inigo Quilez）：https://iquilezles.org/articles/warp/
- 实战配方（2026 WebGPU 黑洞教程）：两层 fBm（大尺度结构 + 细节），lacunarity 2.0、persistence 0.5，`clamp(n + density)` 后乘颜色与亮度再相加；星点按网格 hash 放置，"锐利核心 + 柔和光晕"，带轻微色温差。来源：https://threejsroadmap.com/blog/raytracing-a-black-hole-with-webgpu
- 真体积（可选，贵）：Maxime Heckel 的 raymarching 云——50 步主循环、6 步光照子循环、6 八度 fBm、Beer 定律吸收 0.9；靠蓝噪声抖动 + 0.5x 分辨率 + 双三次上采样救性能。来源：https://blog.maximeheckel.com/posts/real-time-cloudscapes-with-volumetric-raymarching/ ；体积光后处理：https://blog.maximeheckel.com/posts/shaping-light-volumetric-lighting-with-post-processing-and-raymarching/
- 为什么有质感：多八度 + 域扭曲产生"丝缕"，单层 simplex 只会像一块模糊的紫布。登录页用**全屏 quad 上的 2D fBm 星云**即可，体积 raymarching 留给有 GPU 预算的场景。

### 2.4 后处理：pmndrs postprocessing
- 整个链在一个 EffectPass 里合并，避免传统多 pass 的开销。来源：https://github.com/pmndrs/react-postprocessing
- `<Bloom intensity luminanceThreshold={1} luminanceSmoothing mipmapBlur />`：默认就是"选择性"的——只有颜色超过 1 的材质才发光，材质必须 `toneMapped={false}`，例如 `emissive="orange" emissiveIntensity={2}`。`resolutionX/Y` 可半分辨率跑。来源：https://react-postprocessing.docs.pmnd.rs/effects/bloom
- `<Vignette offset={0.5} darkness={0.5} />`、`<Noise blendFunction={SCREEN} />`、`<ChromaticAberration offset />`、`<DepthOfField focusDistance focalLength bokehScale />`。来源：https://react-postprocessing.docs.pmnd.rs/effects/vignette ；https://react-postprocessing.docs.pmnd.rs/effects/noise ；https://docs.pmnd.rs/react-postprocessing/effects/chromatic-aberration
- 原生 three.js 路线：`UnrealBloomPass(threshold, strength, radius)` + `renderer.toneMapping = ACESFilmicToneMapping`。来源：https://github.com/mrdoob/three.js/blob/dev/examples/webgl_postprocessing_unreal_bloom.html
- 为什么有质感：Bloom 让"窗户""引擎"在物理上是光源；Vignette 把视线压向中间；颗粒消灭"过于干净的数字感"并顺手压色带。

### 2.5 色调映射与色彩管理
- `renderer.toneMapping` 可选 ACESFilmic / AgX / Neutral 等，默认 NoToneMapping，`toneMappingExposure` 默认 1，输出 sRGB。来源：https://threejs.org/docs/pages/WebGLRenderer.html
- 贴图色彩空间：颜色贴图（map/emissiveMap）标 `SRGBColorSpace`，法线/粗糙度用 `NoColorSpace`，工作空间是 Linear-sRGB。来源：https://threejs.org/manual/en/color-management.html
- 选择：AgX 基础更好、更"自然"（Blender 4.0 默认）；ACES 更"电影"但会压饱和、拉平中间调。太空场景（深黑 + 高亮光源）两者都可，AgX 在高亮处过渡更顺。来源：https://github.com/mrdoob/three.js/issues/27362 ；https://discourse.threejs.org/t/tone-mapping-overview/75204
- 为什么有质感：HDR 光源经过 filmic 曲线才会有"肩部"，窗户光不会糊成一坨白。

### 2.6 船体材质与灯光
- `MeshStandardMaterial/MeshPhysicalMaterial` + `emissive`/`emissiveMap` 做窗户，emissiveMap 可以用 `CanvasTexture` 每帧生成。来源：https://dustinpfister.github.io/2021/06/22/threejs-emissive-map/ ；https://threejs.org/docs/#api/materials/MeshStandardMaterial.emissive
- 布光：一盏暖主光（DirectionalLight）+ 对侧冷色轮廓光 + Environment 反射，这是 Digikore 讲的"影视大物体"标准布光思路（对远处施加雾/减对比表现深度）。来源：https://digikorevfx.com/the-psychology-of-scale-why-big-objects-fail-on-screen/

### 2.7 镜头与视差
- 帧率无关的阻尼：`maath/easing` 的 `damp3 / dampE(current, target, smoothTime, delta)`，在 `useFrame` 里跟随鼠标。来源：https://github.com/pmndrs/maath ；https://sudeeptobose.medium.com/basic-3d-camera-movement-with-react-three-fiber-and-maath-library-4b060bfe7c5c
- drei `CameraControls`（替代 OrbitControls，带 smoothTime）。来源：https://wawasensei.dev/courses/react-three-fiber/lessons/camera-controls
- 为什么有质感：lerp/damp 让鼠标视差有"迟滞"，直接映射鼠标坐标会像 2010 年的 jQuery 插件。

### 2.8 时间线驱动
- GSAP + R3F：`useThree` 拿 camera，GSAP timeline 直接 tween `camera.position`；ScrollTrigger `scrub` 做滚动驱动。来源：https://codepen.io/GreenSock/pen/MWBvQRW ；https://wawasensei.hashnode.dev/scroll-animations-with-react-three-fiber-and-gsap
- Theatre.js：可视化打关键帧，`sheet.sequence.position = scroll.offset * length`，导出 JSON 后从生产包移除 studio。来源：https://tympanus.net/codrops/2023/02/14/animate-a-camera-fly-through-on-scroll-using-theatre-js-and-react-three-fiber/ ；https://www.theatrejs.com/docs/latest/extensions/react-three-fiber
- 登录页不滚动，所以更需要的是一条**自动播放的慢时间线**（GSAP `repeat: -1, yoyo`）而不是 ScrollTrigger。

### 2.9 非 WebGL 路线与它们的上限
- Canvas 2D 星空：可做视差与软圆（radial gradient 贴图），但没有 Bloom/HDR，几千颗以上就吃 CPU。来源：https://fwdtools.com/ui-snippets/starfield/
- CSS-only（box-shadow 星点 + keyframes）：星点是方/圆的纯色点，没有尺寸衰减、没有独立闪烁相位、没有辉光，且几千个 box-shadow 的重绘很贵——这是它"看着廉价"的直接原因。来源：https://codepen.io/sarazond/pen/LYGbwj ；对照 WebGL 版：https://rocket-boots.github.io/webgl-starfield/
- 图片序列（Apple 法）：画质上限最高但资产大、不可交互、不响应式。来源：https://css-tricks.com/lets-make-one-of-those-fancy-scrolling-animations-used-on-apple-product-pages/
- Lottie / Rive：矢量 2D 动画；Rive runtime ~200KB gzip（含 WASM）、文件比 Lottie 小 3–5 倍、有状态机；Lottie 多了会拖 TBT。两者都做不了带光照的 3D 船，只适合 UI 微动效。来源：https://unicornicons.com/blog/lottie-vs-rive-performance ；https://www.callstack.com/blog/lottie-vs-rive-optimizing-mobile-app-animation
- 视频背景：≤4MB、1080p、H.264、`muted autoplay loop playsinline` + poster；WCAG 2.2.2 要求可暂停。来源：https://sitesplaced.com/blog/cinematic-landing-pages-with-video-backgrounds ；https://designsystem.harvardsites.harvard.edu/news/2025/02/autoplaying-hero-background-videos-digital-design

### 2.10 WebGPU 现状（2026）
- three.js 官方手册：WebGPURenderer 仍标"实验性但趋于成熟"，自动回退 WebGL 2，TSL 一次编写编译到 WGSL/GLSL；新后处理栈（含 SSGI、更好的 DoF）只在 WebGPURenderer 可用；**不支持 ShaderMaterial / onBeforeCompile**，要迁到 node material。来源：https://threejs.org/manual/en/webgpurenderer.html
- 第三方称"约 95% 用户有 WebGPU 能力浏览器"（Utsubo 迁移指南，非官方统计）。来源：https://www.utsubo.com/blog/webgpu-threejs-migration-guide ；TSL 入门：https://blog.maximeheckel.com/posts/field-guide-to-tsl-and-webgpu/
- 对登录页的结论：WebGL 2 + `postprocessing` 是稳妥选择；如果团队愿意用 TSL 写星云与闪烁，WebGPURenderer 也可用，但 R3F/drei/postprocessing 生态对它的支持仍在追。

---

## 三、性能与质量护栏

| 护栏 | 具体做法 | 来源 |
|---|---|---|
| 包体 | three.module.js gzip 约 155KB，且"不能很好地 tree-shake"；Next.js 里 `dynamic(() => import(...), { ssr: false })` 把整个 Canvas 拆出首屏包 | https://github.com/pmndrs/react-three-fiber/discussions/812 ；https://r3f.docs.pmnd.rs/api/canvas |
| DPR | 桌面上限 1（后处理时）到 1.5–2，移动端 ≤1.5；`<PerformanceMonitor onDecline>` 下调 20%，或 drei `<AdaptiveDpr>` | https://tympanus.net/codrops/2025/02/11/building-efficient-three-js-scenes-optimize-performance-while-maintaining-quality/ ；https://r3f.docs.pmnd.rs/advanced/scaling-performance |
| 帧预算 | 后处理"资源密集"，性能不足时动态关闭；开后处理时关 MSAA；draw call 上限"最多 1000" | 同上 |
| 半分辨率 Bloom | `<Bloom resolutionX resolutionY mipmapBlur>` 半分辨率 | https://react-postprocessing.docs.pmnd.rs/effects/bloom |
| 移动端回退 | 大模型在移动端会黑屏/崩溃；用低模 + 关 DoF/Noise，或直接静态海报 | https://www.krapton.com/blog/boosting-react-three-fiber-mobile-performance-in-2026-a-deep-dive-d6105c |
| 减少动效 | `matchMedia('(prefers-reduced-motion: reduce)')`：停止漂移与闪烁，保留静态画面；长时自动播放仍需提供暂停 | https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/At-rules/@media/prefers-reduced-motion ；https://web.dev/learn/accessibility/motion |
| 色带 | 8-bit 渐变必带；加 Interleaved Gradient Noise `(1/255)*noise - 0.5/255`（一行函数，无需贴图）；或 render target 用 `HalfFloatType`；three 材质有 `dithering: true` | https://blog.frost.kiwi/GLSL-noise-and-radial-gradient/ ；https://github.com/mrdoob/three.js/pull/11076 ；https://blog.maximeheckel.com/posts/the-art-of-dithering-and-retro-shading-web/ |
| 闪烁频率 | WCAG 2.3.1：任何 1 秒内不超过 3 次"闪"（相对亮度变化 ≥10%），且闪烁面积要小；星星闪烁应是幅度 10–30%、周期 2–6 秒的正弦，逐星随机相位，永远不要全场同步 | https://www.w3.org/WAI/WCAG22/Understanding/three-flashes-or-below-threshold.html |
| 星星数量 | drei 默认 5,000；Richard Fu 15,000 稳 60fps；EF-Map 24,000 <1ms。登录页 3,000–8,000 足够，关键是尺寸分布（绝大多数很小）而不是总数 | 见 2.2 |
| 为什么慢而直的运动读作"太空" | "物体越大，动得越慢——不只是主运动，还有跟随、震动、次级运动"；"速度等于小"。太空没有介质，没有抖动理由；所以飞船应做**线性 + 极慢**的平移（每分钟移动自身长度的一小部分），镜头用长阻尼 | https://digikorevfx.com/the-psychology-of-scale-why-big-objects-fail-on-screen/ |
| 尺度参照 | "没有什么比画面里的人或熟悉的东西更能传达尺度"——舰首窗内的灯光行列、舷侧的小型飞行器就是参照物 | 同上 |

---

## 四、飞船的三种做法

### A. 程序化几何（three.js 内建）
- `LatheGeometry(points)` 旋转一条 2D 剖面得到流线船体，`CapsuleGeometry` 本身就是 Lathe 的子类；舰桥/舷窗带用 `CylinderGeometry/TorusGeometry` 拼。来源：https://threejs.org/docs/#api/en/geometries/LatheGeometry ；https://dustinpfister.github.io/2022/07/22/threejs-capsule-geometry/
- 窗户：在离屏 `<canvas>` 上画一排排随机亮/暗的小矩形，作为 `emissiveMap`（`CanvasTexture`，`SRGBColorSpace`），`emissiveIntensity` 2–4 让 Bloom 拾取；或用 `InstancedMesh` 放几百个发光小片。来源：https://dustinpfister.github.io/2021/06/22/threejs-emissive-map/ ；https://tympanus.net/codrops/2026/08/11/exploring-procedural-geometry-with-three-js-and-webgpu/
- 引擎：一个 `emissiveIntensity` 高的圆盘 + Sparkles 尾迹 + Lensflare。
- 优点：零资产、零版权、包体最小、完全可调。缺点：像 Axiom 的程度取决于剖面曲线的手艺；没有细节贴图时靠 Bloom 和轮廓光"藏"。工作量：1–2 天可出样，2–4 天打磨。

### B. 加载 glTF 模型
- 可用模型（都是粉丝作品，CC-BY 4.0，需署名）：
  - Axiom Star Cruise Wall E Low Poly，63.9k 三角、31.8k 顶点：https://sketchfab.com/3d-models/axiom-star-cruise-wall-e-low-poly-c16425ea6bb744f2b819f76997ec85a5
  - Axiom Space Ship，65.1k 三角、35.2k 顶点，作者注明"从其他网站转载"：https://sketchfab.com/3d-models/axiom-space-ship-ab8a44f339a14e549170fcddeac18979
  - **风险提示**：Axiom 是迪士尼/皮克斯的设计，粉丝上传的 CC-BY 并不能授予对该造型的权利；Sketchfab 条款也写明使用者自行负责"受版权保护的设计"。商用产品页建议做"Axiom 风格"的原创客轮，而非精确复刻。来源：https://sketchfab.com/licenses
  - 无版权顾虑的 CC0 替代：Kenney Space Kit（150 件）https://kenney.nl/assets/space-kit ；Quaternius Ultimate Space Kit https://quaternius.com/packs/ultimatespaceships.html ；Poly Haven（HDRI 环境贴图也从这里拿）。
- 压缩：`gltf-transform optimize in.glb out.glb --compress draco --texture-compress webp`；Draco 只压静态几何，Meshopt 还能压动画；three 端配 `DRACOLoader` / `MeshoptDecoder`。来源：https://gltf-transform.dev/cli ；https://threejs.org/docs/pages/GLTFLoader.html ；https://www.axl-devhub.me/en/blog/optimizing-3d-models
- 优点：细节最真。缺点：版权、模型质量参差、需要在 Blender 里重做窗户 emissive、包体 +1–5MB。工作量：模型清理 1–3 天 + 集成 1 天。

### C. 2.5D 分层（SVG/PNG + 视差 + 辉光）
- 把飞船画成 3–5 层（船体、窗带、引擎、近景尘埃），CSS/GSAP 做视差，`filter: blur()` + `mix-blend-mode: screen` 叠辉光层，或把这些 PNG 作为贴图放进 R3F 平面里享受真正的 Bloom。来源：https://www.sitepoint.com/parallax-burns-converting-photographs-2d-3d-svg/ ；https://medium.com/@patrickwestwood/how-to-make-multi-layered-parallax-illustration-with-css-javascript-2b56883c3f27
- 优点：视觉完全可控，一张好插画就赢了；包体小；移动端友好。缺点：不能转视角，光照是画死的，"3D 感"来自视差错觉。工作量：取决于插画（自己画 2–5 天；外包另计）。

---

## 五、推荐方案（按推荐度排序）

场景固定：深空、fBm 星云、视差星空、大型客轮星舰慢漂、引擎辉光、发光舰首窗。页面是 Next.js 16 静态导出的登录页，不滚动。

### 方案 1（推荐）：R3F + drei + postprocessing，程序化船体，WebGL 2
- 栈：`three` + `@react-three/fiber` + `@react-three/drei`（Stars/Sparkles/Float/Environment）+ `@react-three/postprocessing`（Bloom/Vignette/Noise）+ `maath`（damp）+ GSAP（一条循环时间线）。`dynamic(..., { ssr: false })` 加载，首屏先显示 CSS 深空底色。
- 场景：全屏 quad 上两层 fBm 星云（自定义 ShaderMaterial，带 IGN 抖动）；`<Stars count≈6000 fade>` 两层不同 `radius/depth` 做视差；Lathe 船体 + CanvasTexture 窗户 emissive（intensity 3，`toneMapped={false}`）+ 引擎盘；一盏暖主光 + 一盏冷轮廓光 + Poly Haven HDRI（`background={false}`）；`<Float>` 极低幅度；鼠标视差用 `easing.damp3`，smoothTime 0.8–1.2s；Bloom `luminanceThreshold=1, mipmapBlur`，Vignette 0.4/0.5，Noise 3–5%；`toneMapping = AgX`（或 ACES）。
- 护栏：`dpr={[1, 1.5]}` + `PerformanceMonitor` 降级；`prefers-reduced-motion` 时停时间线；移动端关 Noise/DoF、星数减半；宽高比 <1 时船只露一半。
- 工作量：3–5 个工作日到"可上线"，再 2–3 天打磨光与曲线。包体约 +200–250KB gzip。
- 为什么排第一：所有"贵"的要素（HDR 自发光 + Bloom、filmic 色调、阻尼镜头、软星点、抖动渐变）都在这条栈上是现成的；没有版权与模型质量风险；改动全在代码里，与 `web/DESIGN.md` 的 token 体系可对齐。

### 方案 2：方案 1 的场景 + 原创 glTF 船
- 差别：把程序化船换成 Blender 里做的"Axiom 风格"客轮（或 Quaternius/Kenney CC0 拼装），窗户与引擎在 Blender 里刷 emissive，`gltf-transform optimize --compress draco --texture-compress webp` 后 1–3MB。
- 工作量：方案 1 + 2–4 天建模（有 Blender 手艺的前提下）。
- 何时选：当"像 Axiom"是硬需求、且团队能接受版权层面做成"致敬"而非复刻。

### 方案 3：2.5D 插画 + 视差 + R3F 只做星空与 Bloom（低风险兜底）
- 一张分层插画（船体/窗带/引擎辉光 PNG）作为 R3F 平面放进方案 1 的星空与后处理里，仍能享受真 Bloom 与 Vignette；没有 3D 转角，但登录页也不需要。
- 工作量：插画到位后 1–2 天集成。包体主要是图片（WebP，<600KB）。
- 何时选：想在一周内上线且有插画资源；或移动端为主。

### 不推荐
- CSS-only 星空 / 纯 CSS 渐变星云：没有 Bloom、没有独立闪烁、必有色带，正是"廉价感"的来源（见 2.9）。
- WebGPURenderer 作为唯一路线：生态（drei/postprocessing）支持仍在追，且现有 GLSL 星云要改写为 TSL；等 R3F v10 生态稳定后再迁。
- 视频背景：画质上限高，但不能随主题/分辨率变化，4MB 的循环视频在登录页上是最重的资产，且必须提供暂停按钮。
