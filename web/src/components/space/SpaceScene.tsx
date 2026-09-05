"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { Canvas, useFrame, useThree } from "@react-three/fiber";
import { Environment, Lightformer } from "@react-three/drei";
import { Color, DirectionalLight, NoToneMapping } from "three";
import type { WebGLTier } from "@/lib/webgl";
import { useReducedMotion } from "@/lib/useReducedMotion";
import { CameraRig, CAMERA_FOV, CAMERA_Y } from "./CameraRig";
import { fx, SceneDirector, resetScene } from "./director";
import { Dust } from "./Dust";
import { Effects } from "./Effects";
import { Nebula } from "./Nebula";
import { Planet } from "./Planet";
import { Ship } from "./ship/Ship";
import { SpeedLines } from "./SpeedLines";
import { Starfield } from "./Starfield";

/**
 * WebGL 场景：<Canvas> + 星云 / 星场 / 行星 / 灯光 / 母舰 / 前景星尘 / 后期 / 镜头 / 导演。所有 three 的 import 只出现在这棵子树里，
 * 于是 three 只进这一个异步 chunk。
 * - dpr [1, 1.5]（lite 档 1）；渲染器 NoToneMapping（色调映射在后期链尾）；
 * - reduced-motion：frameloop="demand"，挂载后两次 invalidate 渲染一张静帧，之后每个输入事件再画一帧（目标值直接落定）；
 * - 标签页隐藏：frameloop="never"；
 * - 首帧真正画出来之后回调 onReady（容器 400ms 淡入由外层做）。
 * 画布容器 pointer-events: none，所有输入监听由 SceneDirector 挂在品牌页根节点上，表单的焦点与滚动不受影响。
 */
const CANVAS = "#010102"; // --c-canvas
const SUN = "#fff1dc";
const RIM = "#8fa8ff";
const RIM_FAIL = "#ff6b5c";

export default function SpaceScene({ tier, onReady }: { tier: WebGLTier; onReady: () => void }) {
  // 在子组件挂载之前把模块级场景状态归零（退出登录后重新进入登录页）
  useState(() => {
    resetScene();
    return true;
  });
  const reduced = useReducedMotion();
  const lite = tier === "lite";
  return (
    <Canvas
      dpr={lite ? 1 : [1, 1.5]}
      frameloop={reduced ? "demand" : "always"}
      flat
      camera={{ fov: CAMERA_FOV, near: 0.1, far: 220, position: [0, CAMERA_Y, 16] }}
      gl={{ antialias: false, stencil: false, powerPreference: "high-performance", toneMapping: NoToneMapping }}
      onCreated={({ gl }) => {
        gl.toneMapping = NoToneMapping;
        gl.setClearColor(CANVAS, 1);
      }}
      style={{ position: "absolute", inset: 0 }}
    >
      <SceneDirector reduced={reduced} />
      <Nebula reduced={reduced} />
      <Starfield reduced={reduced} />
      <Planet />
      <Lights />
      <Environment resolution={64} frames={1} environmentIntensity={0.35}>
        <color attach="background" args={["#05060a"]} />
        <Lightformer form="rect" intensity={2} color="#fff1dc" scale={[6, 2, 1]} position={[-6, 3, 4]} target={[0, 0, 0]} />
        <Lightformer form="rect" intensity={1} color="#7f96ff" scale={[4, 4, 1]} position={[6, 1, -5]} target={[0, 0, 0]} />
        <Lightformer form="ring" intensity={0.4} color="#c8d3ff" scale={3} position={[0, -6, 0]} rotation-x={Math.PI / 2} />
      </Environment>
      <Ship reduced={reduced} />
      <Dust reduced={reduced} count={lite ? 240 : 480} />
      {!reduced && <SpeedLines count={lite ? 200 : 400} />}
      <Effects lite={lite} />
      <CameraRig reduced={reduced} />
      <Lifecycle reduced={reduced} onReady={onReady} />
    </Canvas>
  );
}

/** 主光：船尾一侧的远日（暖）；轮廓光：船头后方（冷），登录失败时 200ms 切成冷红再回来；几乎没有环境光 */
function Lights() {
  const rim = useRef<DirectionalLight>(null!);
  const colors = useMemo(() => ({ rim: new Color(RIM), fail: new Color(RIM_FAIL) }), []);
  useFrame(() => {
    const l = rim.current;
    l.color.copy(colors.rim).lerp(colors.fail, fx.fail);
    l.intensity = 1.6 * (1 + 0.4 * fx.fail);
  });
  return (
    <>
      <directionalLight intensity={3.1} color={SUN} position={[-6, 7, 5]} />
      <directionalLight ref={rim} intensity={1.6} color={RIM} position={[5, -1.5, -7]} />
      <ambientLight intensity={0.06} />
    </>
  );
}

/** 模型没在这么久内到位就先用程序化母舰亮相（模型到了再换） */
const MODEL_WAIT_MS = 4000;

function Lifecycle({ reduced, onReady }: { reduced: boolean; onReady: () => void }) {
  const invalidate = useThree((s) => s.invalidate);
  const setFrameloop = useThree((s) => s.setFrameloop);
  const done = useRef(false);
  const mountedAt = useRef<number | null>(null);
  // priority 2：在 EffectComposer（priority 1）画完这一帧之后才算"首帧"；且要等模型就绪（或超时）——
  // 画布此时还是透明的，着色器已在看不见的帧里编译过，淡入时没有卡顿
  useFrame(({ clock }) => {
    if (done.current) return;
    const now = performance.now();
    if (mountedAt.current === null) mountedAt.current = now;
    if (!fx.modelReady && now - mountedAt.current < MODEL_WAIT_MS) return;
    done.current = true;
    fx.introAt = clock.elapsedTime;
    requestAnimationFrame(onReady);
    if (reduced) invalidate();
  }, 2);
  useEffect(() => {
    if (fx.modelReady) return;
    const id = window.setTimeout(invalidate, MODEL_WAIT_MS + 20);
    return () => window.clearTimeout(id);
  }, [invalidate]);
  useEffect(() => {
    if (!reduced) return;
    invalidate();
    const id = window.setTimeout(invalidate, 120);
    return () => window.clearTimeout(id);
  }, [reduced, invalidate]);
  useEffect(() => {
    const onVisibility = () => setFrameloop(document.hidden ? "never" : reduced ? "demand" : "always");
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, [reduced, setFrameloop]);
  return null;
}
