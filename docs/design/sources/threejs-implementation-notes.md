# 登录页 WebGL 深空场景：three.js 实现笔记

调研日期：2026-09-04。目标项目：`/Users/teemo/workspace-soul/AxiomOS/web`（Next.js 16.3.4 + React 19.2.8 + Tailwind 4 + pnpm 8.15.1，`output: "export"` 静态导出，见 `next.config.ts`）。现有背景是 `src/components/AxiomScene.tsx`（263 行 SVG/CSS），由 `src/components/BrandCanvas.tsx`（无 `"use client"`）挂载。

所有版本号来自 `npm view`（当日），API 名称核对自 three r185 / R3F v9 / drei v10 / postprocessing 6.39 的源码或官方文档，链接列在各节末尾。

---

## 0. 版本矩阵（2026-09-04）

| 包 | 最新 | peerDependencies（关键） | 备注 |
|---|---|---|---|
| `three` | **0.185.1**（r185，2026-07-01 发布） | — | r184 2026-04-16，r183 2026-02-20 |
| `@types/three` | **0.185.4** | — | 与 three 小版本对齐 |
| `@react-three/fiber` | **9.7.0** | `react >=19 <19.3`，`three >=0.156` | 项目 react 19.2.8 满足；10.0.0 仍是 alpha，不用 |
| `@react-three/drei` | **10.7.8** | `@react-three/fiber ^9`，`three >=0.159`，`react ^19` | 11 仍 alpha |
| `postprocessing` | **6.39.4** | `three >=0.168.0 <0.186.0` | **three 升到 0.186 前需等它放宽**；7.0 仍 beta |
| `@react-three/postprocessing` | **3.1.1** | `@react-three/fiber >=9.7.0`，`postprocessing ^6.36` | 要求 fiber ≥ 9.7.0，正好 |
| `motion` / `framer-motion` | 13.2.0 | react ^18 \|\| ^19 | 项目未装；本方案**不需要**它（见 §6） |
| `@gltf-transform/cli` | 4.5.0 | — | 只在选用 glTF 模型时用，devDependency 或 `npx` |
| `glsl-noise` | 0.0.0（MIT） | — | 只把 `snoise` 源码内联进模板字符串，不当依赖装 |

体积（bundlephobia，整包，未 tree-shake）：`three@0.185.1` min 726 KB / gzip 182 KB（`sideEffects` 仅 `./src/nodes/**`，其余可摇）；`@react-three/fiber@9.7.0` min 163 KB / gzip 52 KB；`@react-three/drei@10.7.8` 整包 min 1.6 MB / gzip 500 KB（`sideEffects: false`，只按需 import 则只带走用到的组件）；`postprocessing@6.39.4` npm 解包 2.77 MB（含 demo 资源，运行时 ESM 可摇）；`@react-three/postprocessing@3.1.1` 解包 427 KB。
实际经验值：three 核心 + WebGLRenderer + 几种几何/材质 ≈ 150 KB gzip；fiber ≈ 50 KB；postprocessing 只用 Bloom/Noise/Vignette/ToneMapping ≈ 40–60 KB；drei 只取 `Environment`+`Lightformer`+`PerformanceMonitor` ≈ 20 KB。总增量约 250–300 KB gzip，必须走 `next/dynamic` 延迟到登录面板绘制之后（§1.3）。

来源：
- npm registry（`npm view three version`、`npm view @react-three/fiber peerDependencies` 等，2026-09-04）
- three 发布时间：https://api.github.com/repos/mrdoob/three.js/releases
- bundlephobia API：https://bundlephobia.com/api/size?package=three@0.185.1

---

## 1. 库选择与 Next.js 16 静态导出

### 1.1 为什么选 R3F v9 + pmndrs postprocessing

- R3F v9 是 React 19 的兼容版本；官方迁移指南要点：`<StrictMode>` 现在从 react-dom 继承（项目 `reactStrictMode: true`，开发期 effect 会跑两次，`useMemo` 里创建的 three 对象要幂等）；类型上 `Props` 改名 `CanvasProps`，`MeshProps` 等硬编码类型删除，改用 `ThreeElements['mesh']`；自定义类通过 `extend()` + `ThreeElements` 模块增强注册。
- pmndrs `postprocessing` 把多个效果合并进一个 `EffectPass`，全屏 pass 用单三角形，比 three 自带 `EffectComposer + UnrealBloomPass` 便宜且颜色管理正确（README「Performance」「Output Color Space」节）。
- `@react-three/postprocessing` 3.x 的 `<EffectComposer>` 默认 `multisampling = 8`、`frameBufferType = HalfFloatType`、`renderPriority = 1`，并在挂载时**强制把 `gl.toneMapping` 设为 `NoToneMapping`**（源码 `EffectComposer.tsx` 第 342 行）。这决定了 §5 的链路：色调映射必须用 `<ToneMapping>` 效果放在链尾，否则画面是线性未映射的。

来源：
- https://r3f.docs.pmnd.rs/tutorials/v9-migration-guide
- https://github.com/pmndrs/react-three-fiber/releases
- https://github.com/pmndrs/postprocessing/blob/main/README.md
- https://github.com/pmndrs/react-postprocessing/blob/master/src/EffectComposer.tsx

### 1.2 Canvas 默认值（R3F 9.7）

`<Canvas>` 创建 `WebGLRenderer({ antialias: true, alpha: true, powerPreference: 'high-performance' })`，并设 `outputColorSpace = SRGBColorSpace`、`toneMapping = ACESFilmicToneMapping`（`flat` 可关）；`dpr` 默认 `[1, 2]`；`frameloop` 默认 `'always'`，可选 `'demand' | 'never'`。

本场景推荐：

```tsx
// SpaceScene.tsx（"use client"）
import { Canvas } from "@react-three/fiber";
import { NoToneMapping } from "three";

<Canvas
  dpr={[1, 1.5]}                       // Retina 上限 1.5，后期链成本降 44%（vs 2）
  frameloop={reduced ? "demand" : "always"}
  camera={{ fov: 32, near: 0.1, far: 200, position: [0, 0.6, 14] }}
  gl={{ antialias: false, stencil: false, powerPreference: "high-performance",
        toneMapping: NoToneMapping, failIfMajorPerformanceCaveat: true }}
  onCreated={({ gl }) => gl.setClearColor("#07080c", 1)}
  style={{ position: "absolute", inset: 0 }}
  aria-hidden
>
```

- `antialias: false`：后期链在自己的 HalfFloat 缓冲里做 MSAA（`multisampling`），画布自身开 MSAA 是浪费（postprocessing README 建议 `antialias:false, stencil:false, depth:false`）。
- `failIfMajorPerformanceCaveat: true`：软件渲染（SwiftShader）时上下文创建失败，走 §7 的降级。
- `dpr` 是 R3F 自动按 `window.devicePixelRatio` 夹取的区间；也可运行时 `useThree(s => s.setDpr)`。

来源：https://r3f.docs.pmnd.rs/api/canvas

### 1.3 静态导出与 `ssr: false`

Next.js 文档：`ssr: false` **只能出现在 Client Component 里**，Server Component 中使用会直接报错「`ssr: false` is not allowed with `next/dynamic` in Server Components」。`BrandCanvas.tsx` 目前没有 `"use client"`，因此需要一个薄的客户端包装：

```tsx
// src/components/space/SpaceBackground.tsx
"use client";
import dynamic from "next/dynamic";
import { StaticSpace } from "./StaticSpace";   // CSS/Canvas2D 静态版，也是 loading 占位

const SpaceScene = dynamic(() => import("./SpaceScene"), {
  ssr: false,
  loading: () => <StaticSpace />,
});

export function SpaceBackground() {
  return (
    <div className="absolute inset-0 -z-0 overflow-hidden" aria-hidden="true">
      <SpaceScene />
    </div>
  );
}
```

要点：
- `output: "export"` 下没有服务端，但 `next build` 仍会预渲染 HTML；three 在 import 时不碰 `window`，可预渲染，但 `Canvas` 需要 DOM 尺寸，`ssr:false` 避免水合不一致。
- `SpaceScene.tsx` 本身也写 `"use client"`，所有 three/R3F import 都只在它及其子文件里出现，这样 three 只进入这一个异步 chunk。
- `next/dynamic` 的 `loading` 组件先画静态星空，Canvas 首帧后再 400ms 淡入盖住它（§6.5）。
- 卸载：R3F `unmountComponentAtNode` 会 `gl.renderLists.dispose()`、`gl.forceContextLoss()`、`dispose(scene)` 并删除 root（`renderer.tsx` 第 453 行起），JSX 声明的对象在卸载时自动 `dispose()`；**在模块作用域 `new` 的共享几何/材质不会被自动释放**，要么放进组件 `useMemo` 并在 `useEffect` 清理里手动 `dispose()`，要么用 `dispose={null}` 声明它们是全局共享。

来源：
- https://nextjs.org/docs/app/guides/lazy-loading
- https://r3f.docs.pmnd.rs/api/objects（Disposal 节）
- https://github.com/pmndrs/react-three-fiber/blob/master/packages/fiber/src/core/renderer.tsx

### 1.4 低功耗：`frameloop` 与 `invalidate`

- `frameloop="demand"`：只在 `invalidate()` 被调用后渲染一帧（不立即渲染，只是排队）。适合 reduced-motion 或标签页隐藏时。
- `frameloop="never"` + `advance(timestamp)`：完全自驱动，用于自己节流到 30 fps。
- 浏览器在后台标签页会**暂停 `requestAnimationFrame`**（MDN），R3F 的 loop 基于 rAF，所以隐藏时自动停；但回到前台时 `state.clock.getDelta()` 会返回一个巨大的 delta（loop.ts 第 62 行直接用 `clock.getDelta()`），所有基于 `delta` 的阻尼要 `Math.min(delta, 1/30)` 夹取。
- 120 Hz（ProMotion）屏上 `always` 会跑 120 fps，GPU 负载翻倍；可用 `frameloop="demand"` + `setInterval(invalidate, 1000/60)` 把上限锁在 60（每次 `invalidate` 只排队一帧）。

来源：
- https://r3f.docs.pmnd.rs/advanced/scaling-performance
- https://developer.mozilla.org/en-US/docs/Web/API/Window/requestAnimationFrame
- https://github.com/pmndrs/react-three-fiber/blob/master/packages/fiber/src/core/loop.ts

---

## 2. 星图：三层 `Points` + 自定义 `ShaderMaterial`

### 2.1 设计

- **一个 `Points` 一层，共三层**（远/中/近），各自一个 `<group>` 由 `useFrame` 施加不同视差系数（远 0.02、中 0.05、近 0.09 个单位/鼠标归一化偏移）。比一个 Points 加 `aLayer` 属性简单，且三层可以用不同的 size/亮度。
- 每层顶点属性：`position`（球壳分布，避免立方体角落密度不均）、`aSize`、`aSeed`（0–1 随机，用来做闪烁相位与「是否闪烁」）。
- 点数：远层 3000、中层 1500、近层 500，共 **5000**。理由：每个点只有 1 个顶点，几何成本几乎为零；真正的成本是填充率 = 点数 × 点面积，远层点 1–1.5 px、近层 2–3 px，5000 个点覆盖 < 0.5% 屏幕像素。少于 2000 会显得稀疏（1440p 每 1000 px² 不到 1 颗），多于 6000 在 additive 叠加下背景会发灰、失去「深空」。drei 自带的 `<Stars>` 默认也是 5000（`Stars.tsx`）。
- 材质：`transparent`、`depthWrite: false`、`blending: AdditiveBlending`——星点互相叠加变亮而不是遮挡；星层 `renderOrder` 在星云之后、船之前。

### 2.2 大小衰减公式（核对自 three r185）

three 内置 `PointsMaterial` 的顶点着色器：`gl_PointSize = size; if (isPerspective) gl_PointSize *= scale / -mvPosition.z;` 其中 `uniforms.size = material.size * pixelRatio`，`uniforms.scale = drawingBufferHeight * 0.5`（`WebGLMaterials.js` 第 307–308 行，`_height` 是画布像素高）。自定义 `ShaderMaterial` 没有这两个 uniform，需自己传：

```ts
// 在 useFrame 或 resize 时更新
const { size, viewport } = useThree();
mat.uniforms.uScale.value = size.height * viewport.dpr * 0.5;
mat.uniforms.uPixelRatio.value = viewport.dpr;
```

### 2.3 着色器

```glsl
// star.vert
uniform float uTime, uScale, uPixelRatio;
attribute float aSize, aSeed;
varying float vAlpha;
float hash(float n) { return fract(sin(n * 127.1) * 43758.5453); }
void main() {
  vec4 mv = modelViewMatrix * vec4(position, 1.0);
  // 30% 的星闪烁，振幅 0.15，频率 0.4–1.2 Hz，相位由种子决定
  float twinkles = step(0.7, aSeed);
  float phase = hash(aSeed) * 6.2831;
  float freq = 0.4 + hash(aSeed + 1.0) * 0.8;
  float tw = 1.0 - 0.15 * twinkles * (0.5 + 0.5 * sin(uTime * freq * 6.2831 + phase));
  vAlpha = tw * (0.55 + 0.45 * hash(aSeed + 2.0));
  gl_PointSize = aSize * uPixelRatio * (uScale / -mv.z) * tw;
  gl_Position = projectionMatrix * mv;
}
```

```glsl
// star.frag
uniform vec3 uColor;
varying float vAlpha;
void main() {
  float d = length(gl_PointCoord - 0.5);
  float a = 1.0 - smoothstep(0.18, 0.5, d);   // 软圆点，无贴图
  a *= a;                                     // 收紧核心，避免"棉花球"
  if (a < 0.002) discard;
  gl_FragColor = vec4(uColor * a * vAlpha, a * vAlpha);
  #include <tonemapping_fragment>
  #include <colorspace_fragment>
}
```

- 闪烁振幅 0.15 是「察觉得到、盯着看才发现」的阈值；更大就成了闪光灯。频率 < 1.5 Hz 避免频闪感。
- `#include <colorspace_fragment>` 让输出与渲染目标的色彩空间一致（drei `Stars` 同样包含），在 postprocessing 的线性 HalfFloat 缓冲里等价于不变换。
- 颜色：远层偏冷 `#c7d3ff`，近层近白 `#f2f4ff`，与 DESIGN.md 的 `star / star-faint` token 对齐（从 CSS 变量读 `--c-star` 转成 `Color`）。

### 2.4 React 侧

```tsx
function StarLayer({ count, radius, spread, size, color, parallax }: Props) {
  const mat = useMemo(() => new ShaderMaterial({ vertexShader, fragmentShader,
    uniforms: { uTime:{value:0}, uScale:{value:1}, uPixelRatio:{value:1}, uColor:{value:new Color(color)} },
    transparent: true, depthWrite: false, blending: AdditiveBlending }), [color]);
  const geo = useMemo(() => {
    const pos = new Float32Array(count * 3), sz = new Float32Array(count), seed = new Float32Array(count);
    const v = new Vector3();
    for (let i = 0; i < count; i++) {
      v.setFromSphericalCoords(radius + Math.random() * spread, Math.acos(1 - 2 * Math.random()), Math.random() * Math.PI * 2);
      pos.set([v.x, v.y, v.z], i * 3); sz[i] = size * (0.5 + Math.random()); seed[i] = Math.random();
    }
    const g = new BufferGeometry();
    g.setAttribute("position", new BufferAttribute(pos, 3));
    g.setAttribute("aSize", new BufferAttribute(sz, 1));
    g.setAttribute("aSeed", new BufferAttribute(seed, 1));
    return g;
  }, [count, radius, spread, size]);
  useEffect(() => () => { geo.dispose(); mat.dispose(); }, [geo, mat]);
  const group = useRef<Group>(null!);
  useFrame(({ clock, pointer, size, viewport }, dt) => {
    mat.uniforms.uTime.value = clock.elapsedTime;
    mat.uniforms.uScale.value = size.height * viewport.dpr * 0.5;
    mat.uniforms.uPixelRatio.value = viewport.dpr;
    const d = Math.min(dt, 1 / 30);
    group.current.position.x = MathUtils.damp(group.current.position.x, pointer.x * parallax, 3, d);
    group.current.position.y = MathUtils.damp(group.current.position.y, pointer.y * parallax, 3, d);
  });
  return <group ref={group}><points geometry={geo} material={mat} frustumCulled={false} /></group>;
}
```

`state.pointer` 是 R3F 维护的归一化指针（-1..1），不用自己监听 `mousemove`。

来源：
- three r185 `points.glsl.js`：https://github.com/mrdoob/three.js/blob/r185/src/renderers/shaders/ShaderLib/points.glsl.js
- three r185 `WebGLMaterials.js`：https://github.com/mrdoob/three.js/blob/r185/src/renderers/webgl/WebGLMaterials.js
- drei `Stars.tsx`（参考实现）：https://github.com/pmndrs/drei/blob/master/src/core/Stars.tsx

---

## 3. 星云：全屏三角形 + fbm 噪声 + 抖动

### 3.1 方案 A（推荐）：烘焙一次到低分辨率 FBO，再慢速漂移

全屏每像素跑 4–5 层 3D simplex（每次 snoise ≈ 100+ ALU）在 5K 屏 DPR 1.5 上每帧约 1.8 亿次噪声求值，Safari 上会发热。星云本身变化极慢，所以：

1. 用 drei `useFBO(512, 288, { type: HalfFloatType })` 创建目标，挂载时（以及每 2 秒一次，可选）用 `gl.setRenderTarget(fbo)` 渲染一个全屏 `Mesh(PlaneGeometry(2,2), NebulaMaterial)`；
2. 屏幕上放一张 `PlaneGeometry(2,2)` 的背景，`depthTest: false`、`depthWrite: false`、`renderOrder: -10`，顶点着色器直接 `gl_Position = vec4(position.xy, 0.999, 1.0)`，片元采样 FBO 纹理并加一个随时间/鼠标缓慢移动的 UV 偏移（视差最远层）。
3. 采样时线性插值 + 抖动（§3.3），512×288 的噪声在全屏拉伸后看不出分辨率。

### 3.2 噪声源码

使用 Ashima Arts / Ian McEwan 的 `snoise(vec3)`（MIT，glsl-noise 打包的就是它），源码 100 行，直接粘进模板字符串（不要 `import x from "*.glsl"`，Turbopack 需要额外 loader 配置）。fbm + 域扭曲：

```glsl
// nebula.frag（snoise 定义省略，粘贴 glsl-noise/simplex/3d.glsl）
uniform float uTime;
uniform vec2 uRes;
uniform vec3 uColorA, uColorB;   // 例：靛蓝 #2c2f6b、暖灰紫 #4a3a5c，从 --c-accent 派生
float fbm(vec3 p) {
  float v = 0.0, a = 0.5;
  for (int i = 0; i < 5; i++) { v += a * snoise(p); p = p * 2.02 + 17.0; a *= 0.5; }
  return v;
}
void main() {
  vec2 uv = gl_FragCoord.xy / uRes;
  vec2 p = (uv - 0.5) * vec2(uRes.x / uRes.y, 1.0) * 1.6;
  float t = uTime * 0.015;                                      // 极慢
  vec2 q = vec2(fbm(vec3(p, t)), fbm(vec3(p + 5.2, t + 1.3)));  // 域扭曲第一层
  vec2 r = vec2(fbm(vec3(p + 1.7 * q + vec2(1.7, 9.2), t)),
                fbm(vec3(p + 1.7 * q + vec2(8.3, 2.8), t)));
  float f = fbm(vec3(p + 1.2 * r, t));
  float mask = smoothstep(-0.15, 0.55, f);
  mask *= smoothstep(1.3, 0.25, length(p - vec2(0.55, 0.35)));  // 只在右上角一团
  vec3 col = mix(uColorA, uColorB, clamp(r.x * 0.5 + 0.5, 0.0, 1.0));
  gl_FragColor = vec4(col * mask * 0.10, 1.0);                  // 低不透明度：贡献 ≤ 0.1
}
```

### 3.3 抖动去色带

- 三层链路里：postprocessing 的缓冲是 `HalfFloatType`（react-postprocessing 默认），中间不会丢精度；色带只在**最后写入 8-bit 画布**时产生，暗部 0–0.1 范围只有 25 级，一定会看到。
- 对策一：链尾的 `<Noise>`（胶片颗粒）本身就是抖动，`opacity` 0.03–0.05 已足够打散色带。
- 对策二：在星云着色器与背景平面着色器末尾加 interleaved gradient noise（Jimenez 2014），成本 3 条指令：

```glsl
float ign(vec2 px) { return fract(52.9829189 * fract(dot(px, vec2(0.06711056, 0.00583715)))); }
// 在 gl_FragColor 赋值前：
col += (ign(gl_FragCoord.xy) - 0.5) / 255.0;
```

- three 内置材质的 `material.dithering = true` 用的是同类随机抖动（`dithering_pars_fragment.glsl.js`），只对内置材质生效；船体 `MeshStandardMaterial` 打开它。

### 3.4 方案 B（便宜替代）

用 `CanvasTexture`：在 512×512 的 2D canvas 上画 3–4 个 `createRadialGradient` 叠加、`ctx.filter = "blur(40px)"`，作为一个大 `PlaneGeometry` 的 `map`，`transparent`、`opacity 0.6`，UV 慢速偏移。零着色器成本，但形状固定、无内部流动；作为 reduced-motion 或低端设备版本合适。

来源：
- glsl-noise `simplex/3d.glsl`（Ashima Arts, MIT）：https://github.com/hughsk/glsl-noise/blob/master/simplex/3d.glsl
- 原始仓库：https://github.com/ashima/webgl-noise（Gustavson 与 McEwan 的 "Efficient computational noise in GLSL"）
- 色带与抖动：https://blog.frost.kiwi/GLSL-noise-and-radial-gradient/ ；https://www.anisopteragames.com/how-to-fix-color-banding-with-dithering/
- three `Material.dithering`：https://threejs.org/docs/#api/en/materials/Material.dithering
- postprocessing README「Output Color Space」（为什么用 HalfFloat）：https://github.com/pmndrs/postprocessing#output-color-space

---

## 4. 母舰：程序化建模、灯光、舷窗与引擎

### 4.1 船体几何（three r185 构造签名已核对）

- `LatheGeometry(points: Vector2[], segments = 12, phiStart = 0, phiLength = 2π)`：把 `AxiomScene.tsx` 里已有的船身贝塞尔轮廓（`STERN_TOP` … 船头）采样成 24–32 个 `Vector2(半径, 沿轴位置)`，绕 Y 轴旋转后 `mesh.rotation.z = -π/2` 让轴向变成 X。这是最接近现有 SVG 手绘轮廓的方式，船头钝、船尾收扁只需在轮廓点里体现；用 `mesh.scale.set(1, 0.55, 1)` 把圆截面压扁成飞艇。
- `CapsuleGeometry(radius = 1, height = 1, capSegments = 4, radialSegments = 8, heightSegments = 1)`：一个快速原型；非均匀 `scale` 压扁即可，但船头船尾对称，缺少「船头高钝、船尾扁」的性格。
- 上层建筑：`ExtrudeGeometry(new Shape(...), { depth: 0.8, bevelEnabled: true, bevelSize: 0.06, bevelSegments: 3 })`，沿船身弧线从船尾升起，`bevel` 让边缘吃到轮廓光。
- 推进翼：两片 `ExtrudeGeometry` 薄片，`rotation` 后掠。
- 面数目标：整船 < 20k 三角形（Lathe 32×64 段 ≈ 4k 足够光滑）。

### 4.2 材质与灯光

```tsx
<meshStandardMaterial color="#d6dae1" metalness={0.15} roughness={0.62}
  envMapIntensity={0.7} dithering />
```

- 哑光白客船：`metalness` 0.1–0.2（漆面不是金属），`roughness` 0.55–0.7（漫反射为主但留一点高光带）。`color` 亮度 0.84 是安全值：与 §5 的 bloom 阈值配合，船体最亮处线性亮度 ≈ 0.84 × 灯 ≈ 1.2 以内。
- 灯：`<directionalLight intensity={1.6} color="#ffe9c8" position={[-8, 3, 6]}>`（主光，从船尾一侧的「远日」方向，与 SVG 版一致）；`<directionalLight intensity={0.9} color="#8fa8ff" position={[6, 2, -6]}>`（轮廓光，冷色，从船头后方）；`<ambientLight intensity={0.06}>`。r155 起灯光单位为物理量（`useLegacyLights` 已移除），1–3 是常规区间。
- 环境光：drei `<Environment>` 的 `preset` 从 `raw.githack.com/pmndrs/drei-assets/...` 拉 1k HDR，文档明确「preset 不适合生产环境」，静态导出更不该依赖。用 **`Lightformer` 搭一个 64px 的小房间**，无外部文件：

```tsx
import { Environment, Lightformer } from "@react-three/drei";

<Environment resolution={64} frames={1} environmentIntensity={0.5}>
  <color attach="background" args={["#05060a"]} />
  <Lightformer form="rect" intensity={2} color="#fff1dc" scale={[6, 2]} position={[-6, 3, 4]} target={[0, 0, 0]} />
  <Lightformer form="rect" intensity={1} color="#7f96ff" scale={[4, 4]} position={[6, 1, -5]} target={[0, 0, 0]} />
  <Lightformer form="ring" intensity={0.4} color="#c8d3ff" scale={3} position={[0, -6, 0]} rotation-x={Math.PI / 2} />
</Environment>
```

`frames={1}` 只烘焙一次 cubemap；`resolution={64}` 对哑光白表面足够（粗糙度高，反射本来就模糊）。

### 4.3 舷窗：`InstancedMesh` 小发光片

- 几何：`PlaneGeometry(0.03, 0.018)`；材质：`MeshBasicMaterial({ color: new Color(3.2, 3.0, 2.6), toneMapped: false })`——颜色分量 > 1 就是 HDR，bloom 只会抓它们。注意三点：
  1. three `Material.toneMapped` 文档写明「在渲染到 render target 或使用 postprocessing 时被忽略」，所以走 EffectComposer 时不设也行；设了不伤。
  2. `InstancedMesh.setColorAt(i, color)` 会创建 `instanceColor`（`Float32Array`，可以 > 1），最终颜色 = `material.color × instanceColor`。把 `material.color` 设为 (1,1,1)，每个实例自己带 HDR 值，就能做「8% 的窗慢慢明暗」：每 200 ms 改一小批实例的 `instanceColor` 再 `instanceColor.needsUpdate = true`，300 盏灯 × 3 float 忽略不计。
  3. 位置沿船身曲线采样（复用 SVG 版的贝塞尔采样逻辑），法线朝外：用 `Object3D.lookAt` 朝向船轴外侧后 `updateMatrix()`，`setMatrixAt(i, obj.matrix)`，最后 `instanceMatrix.needsUpdate = true`。

```tsx
const windows = useRef<InstancedMesh>(null!);
useLayoutEffect(() => {
  const o = new Object3D(), c = new Color();
  lampPositions.forEach(([p, n], i) => {          // p: Vector3 位置, n: 外法线
    o.position.copy(p); o.lookAt(p.clone().add(n)); o.updateMatrix();
    windows.current.setMatrixAt(i, o.matrix);
    windows.current.setColorAt(i, c.setRGB(3.2, 3.0, 2.6).multiplyScalar(0.7 + Math.random() * 0.5));
  });
  windows.current.instanceMatrix.needsUpdate = true;
  windows.current.instanceColor!.needsUpdate = true;
}, [lampPositions]);
<instancedMesh ref={windows} args={[undefined, undefined, lampPositions.length]} frustumCulled={false}>
  <planeGeometry args={[0.03, 0.018]} />
  <meshBasicMaterial color={[1, 1, 1]} toneMapped={false} />
</instancedMesh>
```

### 4.4 船头全景舷窗带与引擎辉光

- 船头带：一段 `TorusGeometry(r, 0.02, 8, 48, arc)` 或 `CylinderGeometry(r, r, 0.05, 48, 1, true)`（`openEnded`）贴在船头轮廓上，`MeshBasicMaterial` 颜色 = accent（`#8b8cf5` 类）× 4；`toneMapped: false`。
- 引擎：两枚 `<sprite>`，`SpriteMaterial({ map: glowTex, blending: AdditiveBlending, depthWrite: false, transparent: true, color: accent × 2.5 })`；`glowTex` 是 64×64 `CanvasTexture` 径向渐变（中心白→透明），`colorSpace = SRGBColorSpace`。`useFrame` 里 `sprite.scale.setScalar(base * (1 + 0.08 * Math.sin(t * 1.05)))` 做 6 s 呼吸。
- 额外一层 `pointLight`（accent 色，`intensity 0.6, distance 3`）放在船尾，让船体尾部真正被照亮而不是只靠 bloom 糊上去。

### 4.5 为什么 bloom 只挑灯

pmndrs 的 `BloomEffect` 是**亮度阈值 bloom**：`luminanceThreshold` 默认 **1.0**（postprocessing 源码）——而 react-postprocessing 文档页仍写着 0.9/`mipmapBlur: false` 的旧默认，**显式写全参数**，不要依赖默认。在 HalfFloat 线性缓冲里：
- 船体：反照率 0.84 × 主光 1.6 × cosθ ≤ 1.35（正对光的极小区域），大部分 < 1；把阈值设 **1.1**、`luminanceSmoothing 0.12`，船体几乎不 bloom，只有正对主光的边缘微亮——这是想要的「轮廓光」。
- 舷窗 3.0、船头带 ≈ 4、引擎 ≈ 2.5：远高于阈值，全部 bloom。
- 星点：`uColor × alpha ≤ 1`，不 bloom（想让近层最亮的星带一点晕，可把近层 `uColor` 乘 1.3）。
- 不需要 `<SelectiveBloom>`（要 `lights` 数组和 selection layer，多一次遮罩渲染；文档也说「不需要限制对象子集时用 Bloom 性能更好」）。

### 4.6 备选 glTF 模型（若程序化不够好）

调研结论：**没有现成的免费「太空邮轮」**；能拿到的都是战机/探险船/卡通风，要改成 Axiom 那种「圆润、白色、密密麻麻的舷窗」需要在 Blender 里重做材质并自己布窗，工作量与程序化相当。可参考的候选：

| 模型 | 许可 | 面数 / 格式 | 备注 |
|---|---|---|---|
| Quaternius「Spaceship」（Poly Pizza `/m/uCeLfsdmNP`）及 Ultimate Space Kit（92 个模型） | CC0 | 低模（单模型数百到数千三角形）；glTF/FBX/OBJ/Blend | 造型卡通，适合做远景僚船，不适合主舰 |
| Kenney「Space Kit」（150 个模型） | CC0 | 低模；官网未列格式，Kenney 近年套件普遍含 glTF | 同上 |
| JazOone「SpaceShip」（Sketchfab `6164a883…`） | CC BY 4.0 | 22,632 三角形 / 11,586 顶点，4K 贴图，三种配色带发光 | 最接近「光滑客船」的自由模型；下载为 fbx/obj/blend，Sketchfab 可下载模型通常也提供自动转换的 glTF，需在页面确认；CC BY 需在页脚署名 |
| VerzatileDev「SCI-FI SpaceShip」（itch.io） | CC0 | 未标面数；FBX（1.9 MB），Blender 导出 glTF | 探险船造型 |
| chrisonciuconcepts「low poly space ship」（Sketchfab `587941c…`） | CC BY 4.0 | 150 三角形 | 仅作远景 |

压缩与加载：

```sh
npx @gltf-transform/cli optimize ship.glb ship.opt.glb --compress meshopt --texture-compress webp --simplify 0.9
# 或分别：gltf-transform draco in.glb out.glb ；gltf-transform meshopt in.glb out.glb
```

- 推荐 **meshopt**：解码器是纯 JS（`three-stdlib` 里的 `MeshoptDecoder`），drei `useGLTF(path, useDraco = true, useMeshopt = true)` 默认已接上，静态导出不需要额外文件。
- 若用 Draco：drei 默认解码器路径是 `https://www.gstatic.com/draco/versioned/decoders/1.5.5/`（外部 CDN）。静态站要自托管：把 `node_modules/three/examples/jsm/libs/draco/gltf/` 复制到 `public/draco/`，调用 `useGLTF("/ship.glb", "/draco/")`。
- `useGLTF.preload(url)` 在 `SpaceScene` 模块顶层调用，模型与 chunk 并行下载。

来源：
- three r185 构造签名：https://github.com/mrdoob/three.js/blob/r185/src/geometries/LatheGeometry.js ；https://github.com/mrdoob/three.js/blob/r185/src/geometries/CapsuleGeometry.js
- `Material.toneMapped` / `dithering` 注释：https://github.com/mrdoob/three.js/blob/r185/src/materials/Material.js
- `InstancedMesh.setColorAt`：https://github.com/mrdoob/three.js/blob/r185/src/objects/InstancedMesh.js
- drei Environment / Lightformer：https://drei.docs.pmnd.rs/staging/environment ；https://drei.docs.pmnd.rs/staging/lightformer ；preset CDN 常量：https://github.com/pmndrs/drei/blob/master/src/core/useEnvironment.tsx
- drei useGLTF 源码（Draco 路径、meshopt）：https://github.com/pmndrs/drei/blob/master/src/core/Gltf.tsx
- Bloom / SelectiveBloom 文档：https://react-postprocessing.docs.pmnd.rs/effects/bloom ；https://react-postprocessing.docs.pmnd.rs/effects/selective-bloom
- 模型：https://poly.pizza/m/uCeLfsdmNP ；https://quaternius.com/packs/ultimatespacekit.html ；https://kenney.nl/assets/space-kit ；https://sketchfab.com/3d-models/spaceship-6164a883f57f4f13938c3c5999bc0e1f ；https://verzatiledev.itch.io/sci-fi-spaceship ；https://sketchfab.com/3d-models/low-poly-space-ship-587941c9c11742c6b82dfb99e7b210b9
- gltf-transform CLI：https://gltf-transform.dev/cli

---

## 5. 后期链：EffectComposer + Bloom + Noise + Vignette + ToneMapping

### 5.1 链路与顺序

```tsx
import { EffectComposer, Bloom, ChromaticAberration, Noise, Vignette, ToneMapping } from "@react-three/postprocessing";
import { BlendFunction, ToneMappingMode, VignetteTechnique } from "postprocessing";
import { HalfFloatType, Vector2 } from "three";

const caOffset = useMemo(() => new Vector2(0.0006, 0.0004), []);
<EffectComposer multisampling={lowPower ? 0 : 4} frameBufferType={HalfFloatType} enableNormalPass={false}>
  <Bloom mipmapBlur luminanceThreshold={1.1} luminanceSmoothing={0.12}
         intensity={0.9} radius={0.7} levels={6} />
  <ChromaticAberration offset={caOffset} radialModulation modulationOffset={0.35} />
  <ToneMapping mode={ToneMappingMode.ACES_FILMIC} />
  <Noise premultiply blendFunction={BlendFunction.ADD} opacity={0.045} />
  <Vignette offset={0.32} darkness={0.55} technique={VignetteTechnique.DEFAULT} />
</EffectComposer>
```

逐项说明（参数名核对自 postprocessing 6.39 源码）：
- **Bloom**：`mipmapBlur: true` 是现代路径（`kernelSize`/`resolutionScale`/`width`/`height` 均已标记 deprecated，只对旧的 `BlurPass` 路径生效）。mipmap bloom 自带逐级半分辨率降采样，`levels` 从默认 8 降到 5–6 即是「降分辨率跑 bloom」的正确开关（每少一级少一半像素）。`radius` 0.6–0.8 控制光晕扩散，`intensity` 0.8–1.2；窗灯本身 3.0 的 HDR 值再乘 intensity，太大就糊成一片。
- **ChromaticAberration**：`offset` 是 `Vector2`（默认 `(1e-3, 5e-4)`），`radialModulation: true` 让中心清晰、边缘分色，`modulationOffset` 默认 0.15。可选；登录面板在画面左侧，`offset` 必须很小（≤ 0.0008）否则文字后面的船边缘发彩边。
- **ToneMapping**：`ToneMappingMode.ACES_FILMIC = 6`；库默认是 `AGX`（7），AGX 更中性、高光不那么「发白」，可以两者对比。它放在 bloom 之后：bloom 要在 HDR 线性空间里算。
- **Noise**：`premultiply` 让颗粒随画面亮度缩放（暗部颗粒少），`blendFunction: ADD` + `opacity` 0.03–0.06；它同时充当 8-bit 输出的抖动。放在 ToneMapping 之后，在 LDR 空间加，强度可预测。
- **Vignette**：`offset`（半径起点）+ `darkness`；`eskil` 参数已弃用，改 `technique`。
- 顺序被 `mergeMode="auto"`（默认）合并为最少的 pass：Bloom 是卷积效果单独一 pass，其余合并。

### 5.2 色彩管理

- three r152+ 默认 `ColorManagement.enabled = true`、渲染器 `outputColorSpace = SRGBColorSpace`。postprocessing README：只要 `outputColorSpace = SRGBColorSpace`，库自动跟随（中间缓冲线性，最终 pass 转 sRGB）。
- 渲染器 `toneMapping` 必须是 `NoToneMapping`（react-postprocessing 挂载时会强制设置，卸载时恢复），否则场景在进入链路前就被夹到 [0,1]，bloom 抓不到 HDR。
- R3F `Canvas` 默认给渲染器 ACES；为避免首帧（composer 尚未挂载）与之后不一致，直接在 `gl` 里写 `toneMapping: NoToneMapping`。
- `frameBufferType: HalfFloatType` 是 react-postprocessing 默认，明确写上以防升级改默认。

### 5.3 分辨率与开销

- 整条链的分辨率由 `Canvas dpr` 决定，这是「降分辨率跑后期」的主开关：`[1, 1.5]` 在 2× Retina 上像素数是原生的 56%。
- `EffectComposer` 的 `resolutionScale` prop **只作用于深度/法线降采样 pass**（源码 `DepthDownsamplingPass({ resolutionScale })`），对 bloom 无效，不要误用。
- `multisampling`：默认 8（react-postprocessing）；本场景只有船体有几何边缘，4 足够，低功耗时 0。
- 低端设备：去掉 ChromaticAberration，`levels={4}`，`dpr` 锁 1。

来源：
- https://react-postprocessing.docs.pmnd.rs/effect-composer
- https://github.com/pmndrs/react-postprocessing/blob/master/src/EffectComposer.tsx（默认值第 187–194 行，NoToneMapping 第 342 行）
- BloomEffect 文档：https://pmndrs.github.io/postprocessing/public/docs/class/src/effects/BloomEffect.js~BloomEffect.html
- ChromaticAberration / Noise / Vignette / ToneMapping 源码：https://github.com/pmndrs/postprocessing/tree/main/src/effects ；`ToneMappingMode`：https://github.com/pmndrs/postprocessing/blob/main/src/enums/ToneMappingMode.js
- ToneMapping 效果文档：https://react-postprocessing.docs.pmnd.rs/effects/tone-mapping
- three r184 起 render target 使用工作色彩空间（发布说明）：https://github.com/mrdoob/three.js/releases

---

## 6. 相机与运动

### 6.1 阻尼

`THREE.MathUtils.damp(x, y, lambda, dt) = lerp(x, y, 1 - exp(-lambda * dt))`（r185 `MathUtils.js` 第 134 行）——帧率无关的指数逼近，`lambda` 3–5 对应约 0.3–0.2 s 的响应。不需要 drei `CameraControls`（那是用户交互控制器，还会拉进 `camera-controls` 依赖）；也不需要 `maath`。

### 6.2 鼠标视差 + 漂移 + 开场推进

```tsx
function CameraRig({ reduced }: { reduced: boolean }) {
  const start = useRef<number | null>(null);
  useFrame(({ camera, pointer, clock }, rawDt) => {
    const dt = Math.min(rawDt, 1 / 30);               // 标签页切回来时不跳
    const t = clock.elapsedTime;
    if (start.current === null) start.current = t;
    // 开场 2.5 s 推进：z 14 → 11，easeOutCubic；reduced 直接落在终点
    const k = reduced ? 1 : Math.min(1, (t - start.current) / 2.5);
    const dolly = 14 - 3 * (1 - Math.pow(1 - k, 3));
    // 鼠标视差目标（reduced 时为 0）
    const tx = reduced ? 0 : pointer.x * 0.35, ty = reduced ? 0.6 : 0.6 + pointer.y * 0.2;
    camera.position.x = MathUtils.damp(camera.position.x, tx, 4, dt);
    camera.position.y = MathUtils.damp(camera.position.y, ty, 4, dt);
    camera.position.z = dolly;
    camera.lookAt(0.8, 0, 0);
  });
  return null;
}

function ShipDrift({ reduced, children }: { reduced: boolean; children: ReactNode }) {
  const g = useRef<Group>(null!);
  useFrame(({ clock }) => {
    if (reduced) return;
    const t = clock.elapsedTime;
    g.current.position.y = Math.sin(t * 0.07) * 0.12;          // 90 s 一个来回
    g.current.position.x = Math.sin(t * 0.05 + 1.0) * 0.18;
    g.current.rotation.z = 0.07 + Math.sin(t * 0.06) * 0.008;   // 船头微抬 4° 附近轻摆
  });
  return <group ref={g}>{children}</group>;
}
```

### 6.3 prefers-reduced-motion

- 不引入 `motion`：项目没有它，而且 `motion@13.2.0` 的 `useReducedMotion` 源码是 `useState(prefersReducedMotion.current)`——**只在挂载时读一次**，并不随系统设置变化重渲染（源码里留着 TODO），文档描述与实现不符。
- 用原生 `matchMedia("(prefers-reduced-motion: reduce)")` + `useSyncExternalStore`，SSR/预渲染时返回 `false`：

```ts
// src/lib/useReducedMotion.ts
import { useSyncExternalStore } from "react";
const QUERY = "(prefers-reduced-motion: reduce)";
const subscribe = (cb: () => void) => {
  const mq = window.matchMedia(QUERY);
  mq.addEventListener("change", cb);
  return () => mq.removeEventListener("change", cb);
};
export function useReducedMotion() {
  return useSyncExternalStore(subscribe, () => window.matchMedia(QUERY).matches, () => false);
}
```

- reduced 为真时：`frameloop="demand"`，`CameraRig` 直接落终点，`ShipDrift`/星闪/引擎呼吸全部返回，舷窗常亮；挂载后调用一次 `invalidate()` 渲染静帧（需要两帧：composer 首帧初始化后再 `invalidate()` 一次）。`globals.css` 里现有的 reduced-motion 规则继续管前景淡入。

### 6.4 标签页隐藏

rAF 自动暂停（§1.4）。若想更省（有些浏览器隐藏时仍以低频跑），监听 `document.visibilitychange`，隐藏时 `setFrameloop("never")`，可见时恢复 `"always"`（`useThree(s => s.setFrameloop)`）。

### 6.5 400 ms 淡入

不要在 React 里用 state 切换（会重渲染整棵 Canvas 树）。在 `SpaceScene` 内放一个 `useFrame` 只跑一次的组件：首帧回调里 `gl.domElement.parentElement.style.opacity = "1"`，容器 CSS 写 `opacity: 0; transition: opacity 400ms var(--ease-out)`。前景面板已有「比船晚 120 ms」的淡入（`BrandCanvas.tsx` 注释），两者时序保持。

来源：
- three `MathUtils.damp`：https://github.com/mrdoob/three.js/blob/r185/src/math/MathUtils.js
- R3F `state.pointer` / `setFrameloop`：https://github.com/pmndrs/react-three-fiber/blob/master/packages/fiber/src/core/store.ts
- motion `useReducedMotion` 源码：https://github.com/motiondivision/motion/blob/main/packages/framer-motion/src/utils/reduced-motion/use-reduced-motion.ts ；文档：https://motion.dev/docs/react-use-reduced-motion
- MDN prefers-reduced-motion：https://developer.mozilla.org/en-US/docs/Web/CSS/@media/prefers-reduced-motion

---

## 7. 降级、检测与质量验证

### 7.1 WebGL 检测

three 附带 `three/addons/capabilities/WebGL.js`（package `exports` 里 `./addons/*` 映射到 `examples/jsm/*`），r185 里只剩 `WebGL.isWebGL2Available()`（WebGL1 检测已移除，postprocessing 的 `multisampling` 也要求 WebGL2）。为了不在包装层引入 three，自己写 20 行等价检测，并顺带识别软件渲染：

```ts
export function webglTier(): "full" | "lite" | "none" {
  try {
    const c = document.createElement("canvas");
    const gl = c.getContext("webgl2", { failIfMajorPerformanceCaveat: true });
    if (!gl) return c.getContext("webgl2") ? "lite" : "none";   // 有上下文但性能告警 → lite
    const dbg = gl.getExtension("WEBGL_debug_renderer_info");
    const r = dbg ? String(gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL)) : "";
    gl.getExtension("WEBGL_lose_context")?.loseContext();
    return /swiftshader|llvmpipe|software/i.test(r) ? "lite" : "full";
  } catch { return "none"; }
}
```

`"none"` → 只渲染 `StaticSpace`；`"lite"` → 挂 Canvas 但 `dpr={1}`、无 ChromaticAberration、`levels 4`、`multisampling 0`。检测放在 `SpaceBackground` 的 `useEffect` 里，结果决定是否触发 `dynamic` import（不满足时 three chunk 根本不下载）。

### 7.2 Canvas2D 静态回退（`StaticSpace.tsx`）

- 一个 `<canvas>` 铺满，`devicePixelRatio` 上限 1.5；一次性画：径向渐变星云（2–3 个 `createRadialGradient`，accent 色 6% → 0）、800 颗星（`arc` + 按层不同 alpha）、引擎处一团 accent 辉光。不跑 rAF；保留现有 SVG 母舰（`AxiomScene.tsx` 的船体部分可整体搬来）叠在上面。
- 同一组件也是 `next/dynamic` 的 `loading`，所以 WebGL 版加载的 0.3–1 s 内用户看到的是这张静帧，淡入后无缝替换。

### 7.3 色带 / 闪烁 / 帧率的检查方法

- 色带：截图后在图像编辑器里用「曲线」把 0–0.1 区间拉到 0–1，色带立刻可见；对比开/关 `Noise` 与 IGN 抖动。所有静态测试都在 sRGB、非 HDR 显示器上做（P3/HDR 屏会掩盖）。
- 闪烁：把 `uTime` 放慢 10 倍观察单颗星的亮度曲线是否平滑；确认所有动画用 `clock.elapsedTime`（时间基）而不是帧计数，60 Hz 与 120 Hz 屏行为一致；bloom 的 `luminanceSmoothing` 太小（< 0.05）会让接近阈值的像素在鼠标视差时忽亮忽灭。
- GPU 负载（Apple Silicon Safari）：Safari 15 起 WebGL2 默认开启且跑在 Metal 上（ANGLE）；Web Inspector → Timelines → 「Rendering Frames」看每帧耗时，macOS 活动监视器「GPU 历史」看整体占用；Chrome 用 `chrome://gpu` + Performance 面板的 GPU 轨道。目标：MacBook Air M 系列 60 fps 下 GPU < 30%，风扇机型不起转。最大的两个变量是 dpr 与 bloom `levels`。
- 内存：DevTools Performance monitor 观察 JS heap 与 GPU memory 在路由切换（登录 → 首页 → 登录）后回到基线，验证 R3F 的 `forceContextLoss` 与手动 `dispose` 生效。

### 7.4 Lighthouse 会罚什么

- **Total Blocking Time**：FCP 后 > 50 ms 的长任务超出部分累加；桌面绿区 < 150 ms，移动 < 200 ms。three 模块求值 + 首帧编译着色器（bloom + 自定义材质）很容易一次 100–200 ms。对策：`dynamic` 让 three 在水合之后才下载；挂载再延到 `requestIdleCallback`（Safari 无此 API，用 `setTimeout(…, 1)` 回退）或前景面板淡入完成后；着色器编译在首帧发生，无法避免，但 `Environment frames={1}`、少用材质变体能减少编译数量。
- **Reduce unused JavaScript**：每个 > 20 KiB 未使用代码的脚本文件会被列出；只 import 用到的 drei/postprocessing 组件，别 `import * as THREE`（three 可摇，但 `three/addons` 里的东西各自独立引入）。
- **Avoid enormous network payloads**：总增量 ≈ 300 KB gzip 在阈值内，但 HDR/glTF 文件要单独计入；不用 preset HDR。
- **LCP**：登录面板文字是 LCP 元素，不受 Canvas 影响；Canvas 容器必须 `position:absolute; inset:0` 且 `aria-hidden`，不引起布局位移（CLS）。

来源：
- three `WebGL.js`（r185）：https://github.com/mrdoob/three.js/blob/r185/examples/jsm/capabilities/WebGL.js
- three package `exports`（`./addons/*`）：`npm view three@0.185.1 exports`
- WebKit Safari 15（WebGL2 + Metal）：https://webkit.org/blog/11989/new-webkit-features-in-safari-15/ ；Khronos 公告：https://www.khronos.org/blog/webgl-2-achieves-pervasive-support-from-all-major-web-browsers
- Safari WebGL 性能注意（getParameter/getError 开销）：https://wonderlandengine.com/news/webgl-performance-safari-apple-vision-pro/
- Lighthouse TBT：https://developer.chrome.com/docs/lighthouse/performance/lighthouse-total-blocking-time ；unused JS：https://www.corewebvitals.io/pagespeed/reduce-unused-javascript-lighthouse

---

## 8. 推荐配置

### 8.1 安装

```sh
cd /Users/teemo/workspace-soul/AxiomOS/web
pnpm add three@0.185.1 @react-three/fiber@9.7.0 @react-three/drei@10.7.8 \
         postprocessing@6.39.4 @react-three/postprocessing@3.1.1
pnpm add -D @types/three@0.185.4
# 仅当采用 glTF 模型时：
pnpm add -D @gltf-transform/cli@4.5.0
```

不装：`maath`、`motion`、`glsl-noise`（内联）、`three-stdlib`（drei 已依赖）。
版本约束提醒：`postprocessing` 6.39.4 只允许 `three < 0.186`，升级 three 前先看 postprocessing 是否放宽。

### 8.2 文件结构

```
web/src/
  lib/
    useReducedMotion.ts          # matchMedia + useSyncExternalStore（§6.3）
    webgl.ts                     # webglTier()（§7.1）
  components/
    BrandCanvas.tsx              # 只改一行：<AxiomScene/> → <SpaceBackground/>
    space/
      SpaceBackground.tsx        # "use client"；检测 tier → next/dynamic(ssr:false) 挂 SpaceScene；loading = StaticSpace
      StaticSpace.tsx            # Canvas2D 星空 + 原 SVG 母舰；reduced-motion / no-WebGL / loading 三用
      SpaceScene.tsx             # "use client"；<Canvas> + 场景组合 + 首帧淡入 + 可见性处理
      CameraRig.tsx              # 鼠标视差、开场推进、delta 夹取（§6.2）
      Nebula.tsx                 # useFBO 烘焙 + 背景平面（§3.1）
      Starfield.tsx              # StarLayer ×3（§2.4）
      ship/
        Ship.tsx                 # <ShipDrift> 内组合 Hull + Windows + BowBand + Engines + 灯光 + Environment
        hullProfile.ts           # 从 AxiomScene.tsx 迁来的贝塞尔轮廓与采样（LatheGeometry 点集、舷窗位置）
        Hull.tsx                 # LatheGeometry + ExtrudeGeometry 上层建筑 + 推进翼
        Windows.tsx              # InstancedMesh 舷窗 + 8% 慢闪（§4.3）
        Engines.tsx              # 两枚 additive sprite + pointLight（§4.4）
      Effects.tsx                # EffectComposer 链（§5.1），接收 tier / reduced 决定参数
      shaders/
        star.ts                  # 导出 vertexShader / fragmentShader 模板字符串
        nebula.ts                # snoise（Ashima MIT 头注释保留）+ fbm + IGN 抖动
        glow.ts                  # 引擎 sprite 的 CanvasTexture 生成函数
```

### 8.3 关键参数速查

| 项 | 值 |
|---|---|
| `Canvas dpr` | `[1, 1.5]`；lite 档 `1` |
| `frameloop` | `always`；reduced-motion / 隐藏时 `demand`/`never` |
| 星点 | 3000 / 1500 / 500，size 1.2 / 1.8 / 2.6 px，视差 0.02 / 0.05 / 0.09，闪烁 30%×0.15 |
| 星云 | 512×288 HalfFloat FBO，5 层 fbm + 双层域扭曲，贡献 ≤ 0.10，`uTime × 0.015` |
| 船体材质 | `#d6dae1`，metalness 0.15，roughness 0.62，`dithering` |
| 灯 | 主光 1.6 暖，轮廓光 0.9 冷，环境 0.06，`Environment resolution 64 frames 1` |
| 舷窗 | `InstancedMesh`，HDR 色 (3.2, 3.0, 2.6)，8% 慢闪 |
| Bloom | `mipmapBlur`，threshold 1.1，smoothing 0.12，intensity 0.9，radius 0.7，levels 6 |
| 链尾 | `ToneMapping ACES_FILMIC` → `Noise ADD 0.045 premultiply` → `Vignette 0.32/0.55` |
| 相机 | fov 32，开场 z 14→11 / 2.5 s easeOutCubic，视差 ±0.35 x / ±0.2 y，damp λ=4 |
| 淡入 | 容器 opacity 0→1 400 ms，首帧后触发 |
