"use client";
import { useEffect, useMemo, useRef } from "react";
import { useFrame } from "@react-three/fiber";
import { AdditiveBlending, CatmullRomCurve3, Color, Group, SpriteMaterial, Vector3 } from "three";
import { makeGlowTexture } from "../shaders/glow";
import { mulberry32 } from "./hullProfile";

/**
 * 护航小艇：三条 CatmullRom 航线，从远处驶向舰首，各自跑 12–16s，然后歇 20–40s 再来一趟。
 * 每艘是一枚 HDR 尾灯 sprite + 一团更淡的光晕，只有 0.05 单位——它们是"这艘船有几公里长"的尺子。
 * 坐标在母舰 outer 组（只有摆位与缩放）里，跟着船的摆位、不跟着船的转向。reduced-motion：不出现。
 */
const PATHS: Vector3[][] = [
  [new Vector3(-7.5, -1.3, -3.6), new Vector3(-3, -0.95, -2.7), new Vector3(1.4, -0.55, -1.9), new Vector3(3.4, -0.25, -1.0), new Vector3(4.8, 0.05, 0.3)],
  [new Vector3(-6.5, 2.6, -2.2), new Vector3(-2.2, 1.7, -3.1), new Vector3(1.8, 0.95, -2.6), new Vector3(3.9, 0.35, -1.5), new Vector3(5.2, 0.1, -0.6)],
  [new Vector3(-2.5, -3.2, 2.6), new Vector3(0.6, -2.3, 1.7), new Vector3(2.8, -1.3, 0.7), new Vector3(4.4, -0.5, -0.3), new Vector3(5.4, -0.15, -1.1)],
];

type Run = { start: number; duration: number; next: number };

export function Shuttles({ reduced }: { reduced: boolean }) {
  const tex = useMemo(() => makeGlowTexture(64), []);
  const lampMat = useMemo(() => new SpriteMaterial({ map: tex, blending: AdditiveBlending, depthWrite: false, transparent: true, color: new Color("#dfe6ff").multiplyScalar(2.6), toneMapped: false }), [tex]);
  const haloMat = useMemo(() => new SpriteMaterial({ map: tex, blending: AdditiveBlending, depthWrite: false, transparent: true, color: new Color("#828fff").multiplyScalar(0.7), toneMapped: false }), [tex]);
  useEffect(
    () => () => {
      tex.dispose();
      lampMat.dispose();
      haloMat.dispose();
    },
    [tex, lampMat, haloMat],
  );
  const curves = useMemo(() => PATHS.map((p) => new CatmullRomCurve3(p, false, "centripetal")), []);
  const runs = useRef<Run[] | null>(null);
  const rng = useRef(mulberry32(0x53485554));
  const groups = useRef<(Group | null)[]>([]);
  const tmp = useMemo(() => new Vector3(), []);
  useFrame(({ clock }) => {
    if (reduced) return;
    const t = clock.elapsedTime;
    if (!runs.current) {
      const r = rng.current;
      // 第一趟 4–10s 后出发，错开
      runs.current = curves.map((_, i) => ({ start: 4 + i * 3 + r() * 3, duration: 12 + r() * 4, next: 0 }));
    }
    runs.current.forEach((run, i) => {
      const g = groups.current[i];
      if (!g) return;
      const u = (t - run.start) / run.duration;
      if (u < 0) {
        g.visible = false;
        return;
      }
      if (u >= 1) {
        g.visible = false;
        const r = rng.current;
        run.start = t + 20 + r() * 20;
        run.duration = 12 + r() * 4;
        return;
      }
      g.visible = true;
      curves[i].getPointAt(u, tmp);
      g.position.copy(tmp);
      // 靠近船时更亮更大（透视之外再给一点"驶近"的感觉），两端淡入淡出
      const fade = Math.min(1, u * 8, (1 - u) * 6);
      g.scale.setScalar(fade * (0.75 + u * 0.5));
    });
  });
  if (reduced) return null;
  return (
    <group>
      {curves.map((_, i) => (
        <group
          key={i}
          ref={(el) => {
            groups.current[i] = el;
          }}
          visible={false}
        >
          <sprite material={lampMat} scale={0.05} />
          <sprite material={haloMat} scale={0.16} />
        </group>
      ))}
    </group>
  );
}
