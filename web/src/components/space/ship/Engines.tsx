"use client";
import { useEffect, useMemo, useRef } from "react";
import { useFrame } from "@react-three/fiber";
import { AdditiveBlending, CircleGeometry, Color, CylinderGeometry, Group, MeshBasicMaterial, MeshStandardMaterial, PointLight, SpriteMaterial } from "three";
import { fx } from "../director";
import { ENGINE_RADIUS, ENGINES, STERN } from "./hullProfile";
import { makeGlowTexture } from "../shaders/glow";

/**
 * 引擎：两枚喷口（深色筒 + HDR accent 发光盘）+ 两团 additive 辉光 sprite（6s 呼吸 ±8%）
 * + 一盏 accent 点光源真正照亮船尾（不只靠 Bloom 糊上去）。
 * 提交时喷发：sprite ×1.8、点光 ×2.5（fx.flare，300ms）；失败时同样的曲线退回。
 */
const ACCENT = "#5e6ad2";
const ACCENT_HOVER = "#828fff";

export function Engines({ reduced }: { reduced: boolean }) {
  const tex = useMemo(() => makeGlowTexture(), []);
  const glowMat = useMemo(() => new SpriteMaterial({ map: tex, blending: AdditiveBlending, depthWrite: false, transparent: true, color: new Color(ACCENT_HOVER).multiplyScalar(1.4), toneMapped: false }), [tex]);
  const coreMat = useMemo(() => new SpriteMaterial({ map: tex, blending: AdditiveBlending, depthWrite: false, transparent: true, color: new Color("#dfe3ff").multiplyScalar(2.2), toneMapped: false }), [tex]);
  const discMat = useMemo(() => new MeshBasicMaterial({ color: new Color(ACCENT_HOVER).multiplyScalar(4), toneMapped: false }), []);
  const barrel = useMemo(() => new MeshStandardMaterial({ color: "#4a4e58", metalness: 0.55, roughness: 0.45, dithering: true }), []);
  const geos = useMemo(
    () => ({
      barrel: new CylinderGeometry(ENGINE_RADIUS * 0.92, ENGINE_RADIUS, 0.34, 32, 1, true),
      disc: new CircleGeometry(ENGINE_RADIUS * 0.86, 32),
    }),
    [],
  );
  useEffect(
    () => () => {
      tex.dispose();
      glowMat.dispose();
      coreMat.dispose();
      discMat.dispose();
      barrel.dispose();
      geos.barrel.dispose();
      geos.disc.dispose();
    },
    [tex, glowMat, coreMat, discMat, barrel, geos],
  );
  const glows = useRef<Group>(null!);
  const light = useRef<PointLight>(null!);
  useFrame(({ clock }) => {
    const t = clock.elapsedTime;
    const breath = reduced ? 1 : 1 + 0.08 * Math.sin(t * 1.05);
    const flare = 1 + 0.8 * fx.flare;
    glows.current.children.forEach((s, i) => s.scale.setScalar((i % 2 === 0 ? 0.62 : 0.22) * breath * flare));
    light.current.intensity = 1.1 * breath * (1 + 1.5 * fx.flare);
  });
  return (
    <group>
      {ENGINES.map((p, i) => (
        <group key={i} position={p}>
          <mesh geometry={geos.barrel} material={barrel} position={[-0.12, 0, 0]} rotation={[0, 0, Math.PI / 2]} />
          <mesh geometry={geos.disc} material={discMat} position={[-0.29, 0, 0]} rotation={[0, -Math.PI / 2, 0]} />
        </group>
      ))}
      <group ref={glows}>
        {ENGINES.flatMap((p, i) => [
          <sprite key={`g${i}`} material={glowMat} position={[p[0] - 0.36, p[1], p[2]]} scale={0.62} />,
          <sprite key={`c${i}`} material={coreMat} position={[p[0] - 0.33, p[1], p[2]]} scale={0.22} />,
        ])}
      </group>
      <pointLight ref={light} color={ACCENT} intensity={1.1} distance={3.6} decay={2} position={[STERN.x - 0.6, STERN.c, 0]} />
    </group>
  );
}
