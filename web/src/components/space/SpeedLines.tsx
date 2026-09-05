"use client";
import { useEffect, useMemo, useRef } from "react";
import { useFrame } from "@react-three/fiber";
import { AdditiveBlending, Color, Group, InstancedMesh, MathUtils, MeshBasicMaterial, Object3D, PlaneGeometry } from "three";
import { fx } from "./director";
import { mulberry32 } from "./ship/hullProfile";

/**
 * 速度线：掠航的快段里，从画面消失点（行进方向）向外辐射的细长 additive 条（实例化，默认 400 条）。
 * 整组挂在相机坐标系里（每帧复制相机的位置与朝向），只在速度 0.3–0.9 之间淡入到全开；条很细（0.0016×深度），长度随速度拉长；速度为 0 时整组隐藏，零开销。
 */
const TINT = new Color("#aab4ff").multiplyScalar(1.3);

export function SpeedLines({ count = 400 }: { count?: number }) {
  const group = useRef<Group>(null!);
  const mesh = useRef<InstancedMesh>(null!);
  const geo = useMemo(() => new PlaneGeometry(1, 1), []);
  const mat = useMemo(() => new MeshBasicMaterial({ color: TINT, transparent: true, opacity: 0, blending: AdditiveBlending, depthWrite: false, depthTest: false, toneMapped: false }), []);
  useEffect(
    () => () => {
      geo.dispose();
      mat.dispose();
    },
    [geo, mat],
  );
  const data = useMemo(() => {
    const rng = mulberry32(0x53504431);
    const angle = new Float32Array(count), radius = new Float32Array(count), depth = new Float32Array(count), len = new Float32Array(count), seed = new Float32Array(count);
    for (let i = 0; i < count; i++) {
      angle[i] = rng() * Math.PI * 2;
      radius[i] = 0.5 + Math.pow(rng(), 0.7) * 2.4;
      depth[i] = 2.5 + rng() * 5;
      len[i] = 0.35 + rng() * 0.9;
      seed[i] = rng();
    }
    return { angle, radius, depth, len, seed };
  }, [count]);
  const live = useRef({ mat, data, o: new Object3D() });
  useEffect(() => {
    live.current = { mat, data, o: new Object3D() };
  }, [mat, data]);
  useFrame(({ camera, clock }) => {
    // 只在快段出现：速度 0.3 以下没有，0.9 全开
    const s = MathUtils.smoothstep(fx.speed, 0.3, 0.9);
    const m = mesh.current, g = group.current;
    if (!m || !g) return;
    // 静止时把实例数设为 0 而不是隐藏：着色器在首帧就编译好，掠航开始时没有卡顿
    if (s < 0.01) {
      m.count = 0;
      return;
    }
    const { mat: material, data: d, o } = live.current;
    m.count = d.angle.length;
    material.opacity = 0.06 + 0.3 * s;
    g.position.copy(camera.position);
    g.quaternion.copy(camera.quaternion);
    const t = clock.elapsedTime;
    for (let i = 0; i < d.angle.length; i++) {
      const a = d.angle[i];
      const dp = d.depth[i];
      // 条沿径向向外滑动，制造流过的感觉
      const slide = ((t * (1.5 + d.seed[i]) * s + d.seed[i] * 3) % 3) * 0.5;
      const r = d.radius[i] + slide;
      const L = d.len[i] * (0.1 + 1.7 * s) * (dp / 4);
      const w = 0.0016 * dp;
      o.position.set(Math.cos(a) * (r + L / 2), Math.sin(a) * (r + L / 2), -dp);
      o.rotation.set(0, 0, a);
      o.scale.set(L, w, 1);
      o.updateMatrix();
      m.setMatrixAt(i, o.matrix);
    }
    m.instanceMatrix.needsUpdate = true;
  });
  return (
    <group ref={group}>
      <instancedMesh ref={mesh} args={[geo, mat, count]} frustumCulled={false} renderOrder={5} />
    </group>
  );
}
