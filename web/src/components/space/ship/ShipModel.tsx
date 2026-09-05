"use client";
import { useEffect, useMemo, useRef } from "react";
import { useFrame, useThree } from "@react-three/fiber";
import { useGLTF } from "@react-three/drei";
import { AdditiveBlending, Color, Group, Mesh, MeshBasicMaterial, MeshPhysicalMaterial, MeshStandardMaterial, PointLight, SpriteMaterial, Vector3 } from "three";
import { fx } from "../director";
import { makeGlowTexture } from "../shaders/glow";
import { mulberry32 } from "./hullProfile";
import { ACCENT, ACCENT_HOVER, analyzeShip, BEACON_BASE, ENGINE_BASE, makeHullMaterial, MODEL_SCALE, WARM, WINDOW_BASE } from "./shared";

/**
 * Blender 建模的母舰（public/models/axiom-opt.glb：meshopt + 量化 + WebP 烘焙贴图，473 KB；解码器来自 three-stdlib，纯 JS/WASM 内联，
 * 不需要外部文件）。模型 +x 为船头、+y 向上、长 10 单位、原点在船体中心；这里整体缩到 SHIP_LEN（6.2），
 * 放在 Ship.tsx 的 drift / orbit / attention 组里，于是光标注意力、拖拽环绕、起航推进、空闲漂移全部沿用。
 * 材质全部换成我们自己的：
 * - Hull：保留烘焙法线 / ORM（AO + 粗糙度 + 金属度）/ 自发光贴图，材质换 MeshPhysicalMaterial（清漆 0.6 / 0.28、normalScale 0.6）；
 * - Windows：一张 774 块小方片的网格，加一条逐顶点 color 属性（HDR），MeshBasicMaterial vertexColors——
 *   每块窗按索引连通性分组，质心算一次；光标邻近、按键脉冲、密码聚焦变暗、8% 慢明暗都写进这条属性；
 * - BowBand：按顶点高度的增益（中心 1、上下缘 0.45）× accent HDR，聚焦密码 / 敲键时整体提亮；
 * - Engine：accent HDR 圆盘，随喷发包络提亮；辉光 sprite 与点光源按圆盘位置摆放；
 * - Beacon：最高的那簇（桅顶）随按键闪一次，其余常亮。
 */
import { MODEL_URL } from "./modelUrl";
export { MODEL_URL };
useGLTF.preload(MODEL_URL, false, true);
export { MODEL_SCALE };

const PROX_RADIUS = 1.1; // 模型单位
const RUNWAY_HALF = 0.55; // 引导灯高亮的半宽（模型单位）
const PROX_GAIN = 0.55;

export function ShipModel({ reduced }: { reduced: boolean }) {
  const gltf = useGLTF(MODEL_URL, false, true);
  const nodes = gltf.nodes as Record<string, Mesh>;
  const hull = nodes.Hull, windows = nodes.Windows, band = nodes.BowBand, engine = nodes.Engine, beacon = nodes.Beacon;

  const mats = useMemo(
    () => ({
      hull: makeHullMaterial(hull.material as MeshStandardMaterial),
      windows: new MeshBasicMaterial({ vertexColors: true, toneMapped: false }),
      band: new MeshBasicMaterial({ vertexColors: true, toneMapped: false }),
      beacon: new MeshBasicMaterial({ vertexColors: true, toneMapped: false }),
      engine: new MeshBasicMaterial({ color: ENGINE_BASE.clone(), toneMapped: false }),
    }),
    [hull],
  );
  const tex = useMemo(() => makeGlowTexture(), []);
  const sprites = useMemo(
    () => ({
      glow: new SpriteMaterial({ map: tex, blending: AdditiveBlending, depthWrite: false, transparent: true, color: new Color(ACCENT_HOVER).multiplyScalar(1.4), toneMapped: false }),
      core: new SpriteMaterial({ map: tex, blending: AdditiveBlending, depthWrite: false, transparent: true, color: new Color("#dfe3ff").multiplyScalar(2.2), toneMapped: false }),
      // 入口：从舰首窗涌向镜头的光束 + 横向的变形眩光条
      beam: new SpriteMaterial({ map: tex, blending: AdditiveBlending, depthWrite: false, depthTest: false, transparent: true, color: WARM.clone().multiplyScalar(1.6), toneMapped: false, opacity: 0 }),
      streak: new SpriteMaterial({ map: tex, blending: AdditiveBlending, depthWrite: false, depthTest: false, transparent: true, color: new Color("#c9d2ff").multiplyScalar(2.0), toneMapped: false, opacity: 0 }),
    }),
    [tex],
  );
  useEffect(
    () => () => {
      Object.values(mats).forEach((m) => m.dispose());
      sprites.glow.dispose();
      sprites.core.dispose();
      sprites.beam.dispose();
      sprites.streak.dispose();
      tex.dispose();
    },
    [mats, sprites, tex],
  );

  // 几何分析（shared.ts）：窗片分组、窗带 / 桅灯的顶点色、引擎位置。只算一次（几何来自 useGLTF 缓存）
  const data = useMemo(() => analyzeShip({ windows, band, beacon, engine }), [windows, band, beacon, engine]);

  const group = useRef<Group>(null!);
  const gl = useThree((s) => s.gl);
  const camera = useThree((s) => s.camera);
  const scene = useThree((s) => s.scene);
  const invalidate = useThree((s) => s.invalidate);
  // 模型挂上后先把它的着色器编译好（画布还透明），再宣布"模型就绪"：Lifecycle 据此放行首帧与淡入
  useEffect(() => {
    let alive = true;
    const g = group.current;
    if (!g) return;
    performance.mark("axiom:model-mounted");
    gl.compileAsync(g, camera, scene)
      .catch(() => {})
      .then(() => {
        if (!alive) return;
        fx.modelReady = true;
        performance.mark("axiom:model-ready");
        invalidate();
      });
    return () => {
      alive = false;
      fx.modelReady = false;
    };
  }, [gl, camera, scene, invalidate]);
  const glows = useRef<Group>(null!);
  const light = useRef<PointLight>(null!);
  const beam = useRef<Group>(null!);
  const st = useRef({ seq: 0, pulses: [] as { i: number; at: number }[], rng: mulberry32(0x4b455953), local: new Vector3(), c: new Color(), frame: 0, lastPulse: -1 });
  // 材质与几何分析结果放进 ref，每帧只改它们的内部值（不动 hook 返回值本身）
  const live = useRef({ mats, data, sprites });
  useEffect(() => {
    live.current = { mats, data, sprites };
  }, [mats, data, sprites]);

  useFrame(({ clock }, rawDt) => {
    const s = st.current;
    const { mats: m, data, sprites: sp } = live.current;
    const t = clock.elapsedTime;
    const now = performance.now();
    const dt = Math.min(rawDt, 1 / 30);
    // 窗带 / 引擎 / 辉光 / 点光：材质级。登舰：点火 ×2.6 / ×4，窗带两次确认闪 + 入口 ×3
    m.band.color.setScalar(1 + 0.35 * fx.listen + 0.15 * fx.keyPulse + 1.2 * fx.bandFlash + 0.4 * fx.pathU + 2.0 * fx.entrance);
    m.engine.color.copy(ENGINE_BASE).multiplyScalar(1 + 0.6 * fx.flare + 0.8 * fx.ignite);
    const breath = reduced ? 1 : 1 + 0.08 * Math.sin(t * 1.05);
    const flare = 1 + 0.8 * fx.flare + 0.8 * fx.ignite;
    glows.current.children.forEach((sp, i) => sp.scale.setScalar((i % 2 === 0 ? 1.0 : 0.36) * breath * flare * data.radius * 6.5));
    light.current.intensity = 1.5 * breath * (1 + 1.5 * fx.flare + 1.5 * fx.ignite);
    // 入口：光束与眩光随 entrance 长大（模型单位）
    // 一直可见（透明度 0）：着色器在首帧就编译好，入口开始时没有卡顿
    const e3 = fx.entrance;
    const bg = beam.current;
    sp.beam.opacity = Math.min(1, e3 * 1.4);
    sp.streak.opacity = Math.min(1, e3 * 1.6);
    const [bs, ss] = bg.children;
    bs.scale.setScalar(1.5 + 9 * e3);
    ss.scale.set(4 + 22 * e3, 0.35 + 1.1 * e3, 1);
    // 桅顶灯：只在脉冲值变化时写属性
    if (fx.keyPulse !== s.lastPulse) {
      s.lastPulse = fx.keyPulse;
      const k = 1 + 1.6 * fx.keyPulse;
      for (const i of data.mast) data.beaconColor.setXYZ(i, BEACON_BASE.r * k, BEACON_BASE.g * k, BEACON_BASE.b * k);
      data.beaconColor.needsUpdate = true;
    }
    // 舷窗
    if (fx.keySeq !== s.seq) {
      s.seq = fx.keySeq;
      const n = 3 + Math.floor(s.rng() * 3);
      const b = data.win.bridge;
      for (let k = 0; k < n && b.length; k++) s.pulses.push({ i: b[Math.floor(s.rng() * b.length)], at: now });
    }
    if (s.pulses.length) s.pulses = s.pulses.filter((p) => now - p.at < 400);
    s.frame++;
    if (!reduced && s.frame % 2) return;
    const dim = (1 - 0.2 * fx.listen) * (1 + 0.6 * fx.surge + 0.25 * fx.pathU + 0.6 * fx.entrance);
    s.local.copy(fx.cursorWorld);
    group.current.worldToLocal(s.local);
    const cx = s.local.x, cy = s.local.y;
    const doProx = !reduced && !fx.boarding && Math.abs(s.local.z) < 8;
    const runway = fx.runwayX;
    const doRunway = Number.isFinite(runway);
    const { groups, cx: wx, cy: wy, base, tw, period, phase } = data.win;
    const prox = data.prox, attr = data.winColor, c = s.c;
    const kProx = 1 - Math.exp(-12 * dt);
    for (let i = 0; i < groups.length; i++) {
      let k = base[i];
      if (tw[i] && !reduced) k *= 0.12 + 0.88 * (0.5 + 0.5 * Math.sin((t / period[i]) * Math.PI * 2 + phase[i]));
      let pt = 0;
      if (doProx) {
        const dx = wx[i] - cx, dy = wy[i] - cy;
        const dd = Math.sqrt(dx * dx + dy * dy);
        if (dd < PROX_RADIUS) {
          const u = 1 - dd / PROX_RADIUS;
          pt = PROX_GAIN * u * u;
        }
      }
      prox[i] += (pt - prox[i]) * kProx;
      k = k * dim + prox[i];
      if (doRunway) {
        const dr = Math.abs(wx[i] - runway);
        if (dr < RUNWAY_HALF) k += 1.1 * (1 - dr / RUNWAY_HALF);
      }
      c.copy(WINDOW_BASE).multiplyScalar(k);
      const verts = groups[i];
      for (let j = 0; j < verts.length; j++) attr.setXYZ(verts[j], c.r, c.g, c.b);
    }
    for (const p of s.pulses) {
      const age = now - p.at;
      const env = 1 + 0.4 * (age < 60 ? age / 60 : 1 - (age - 60) / 340);
      const verts = groups[p.i];
      for (let j = 0; j < verts.length; j++) {
        const vi = verts[j];
        attr.setXYZ(vi, attr.getX(vi) * env, attr.getY(vi) * env, attr.getZ(vi) * env);
      }
    }
    attr.needsUpdate = true;
  });

  const r = data.radius;
  // 不改 useGLTF 缓存里的节点：只借它们的几何与变换，材质是我们自己的
  const part = (node: Mesh, material: MeshBasicMaterial | MeshPhysicalMaterial) => <mesh geometry={node.geometry} material={material} position={node.position} quaternion={node.quaternion} scale={node.scale} />;
  return (
    <group ref={group} scale={MODEL_SCALE}>
      {part(hull, mats.hull)}
      {part(windows, mats.windows)}
      {part(band, mats.band)}
      {part(beacon, mats.beacon)}
      {part(engine, mats.engine)}
      <group ref={glows}>
        {data.engines.flatMap((p, i) => [
          <sprite key={`g${i}`} material={sprites.glow} position={[data.engineX - r * 0.9, p.y, p.z]} />,
          <sprite key={`c${i}`} material={sprites.core} position={[data.engineX - r * 0.8, p.y, p.z]} />,
        ])}
      </group>
      <group ref={beam} position={[data.bandCenter.x - 0.15, data.bandCenter.y, data.bandCenter.z]}>
        <sprite material={sprites.beam} scale={1.5} />
        <sprite material={sprites.streak} scale={[4, 0.35, 1]} />
      </group>
      <pointLight ref={light} color={ACCENT} intensity={1.5} distance={3.6 / MODEL_SCALE} decay={2} position={[data.engineX - r * 1.6, (data.engines[0].y + data.engines[1].y) / 2, 0]} />
    </group>
  );
}
