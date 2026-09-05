"use client";
import { useEffect, useMemo, useRef } from "react";
import { useFrame } from "@react-three/fiber";
import { AdditiveBlending, BufferAttribute, BufferGeometry, Color, Group, MathUtils, ShaderMaterial, Vector3 } from "three";
import { CAMERA_Y, CAMERA_Z } from "./CameraRig";
import { fx } from "./director";
import { mulberry32 } from "./ship/hullProfile";
import { starFragment, starVertex } from "./shaders/star";

/**
 * 星场：三层 Points（远 3000 / 中 1500 / 近 500），以相机为中心的球壳、只分布在视锥方向，各自的视差系数与极慢的滚转；
 * 尺寸与闪烁在着色器里（shaders/star.ts）。固定种子，每次加载同一片星空。
 * 视差目标来自 fx.px/py（导演层的光标目标），空闲吸引时随相机一起慢慢漂；起航时星层向相机前移（近层更多）。
 */
type LayerProps = {
  count: number;
  radius: number;
  spread: number;
  sizePx: number;
  color: string;
  brightness: number;
  parallax: number;
  drift: number;
  seed: number;
  reduced: boolean;
};

const REF_HEIGHT = 450; // 设计基准：900px 高视口的一半
const CAMERA = new Vector3(0, CAMERA_Y, CAMERA_Z);

function StarLayer({ count, radius, spread, sizePx, color, brightness, parallax, drift, seed, reduced }: LayerProps) {
  const mat = useMemo(
    () =>
      new ShaderMaterial({
        vertexShader: starVertex,
        fragmentShader: starFragment,
        uniforms: {
          uTime: { value: 0 },
          uScale: { value: REF_HEIGHT },
          uPixelRatio: { value: 1 },
          uTwinkle: { value: reduced ? 0 : 1 },
          uWarp: { value: 0 },
          uColor: { value: new Color(color).multiplyScalar(brightness) },
        },
        transparent: true,
        depthWrite: false,
        blending: AdditiveBlending,
      }),
    [color, brightness, reduced],
  );
  const geo = useMemo(() => {
    const rng = mulberry32(seed);
    const pos = new Float32Array(count * 3), sz = new Float32Array(count), sd = new Float32Array(count);
    const v = new Vector3();
    const meanR = radius + spread / 2;
    // 以相机为中心、朝 -z 的一个 42° 半角锥：星只分布在看得见的方向上（含视差余量），5000 颗都在画面里
    const cosMax = Math.cos((42 * Math.PI) / 180);
    for (let i = 0; i < count; i++) {
      const cz = cosMax + (1 - cosMax) * rng();
      const sr = Math.sqrt(1 - cz * cz), phi = rng() * Math.PI * 2;
      v.set(sr * Math.cos(phi), sr * Math.sin(phi), -cz).multiplyScalar(radius + rng() * spread).add(CAMERA);
      pos.set([v.x, v.y, v.z], i * 3);
      // 绝大多数很小，极少数放大：尺寸分布比总数更重要
      const big = rng() < 0.04 ? 1.7 : 1;
      sz[i] = ((sizePx * (0.55 + rng() * 0.6) * big) * meanR) / REF_HEIGHT;
      sd[i] = rng();
    }
    const g = new BufferGeometry();
    g.setAttribute("position", new BufferAttribute(pos, 3));
    g.setAttribute("aSize", new BufferAttribute(sz, 1));
    g.setAttribute("aSeed", new BufferAttribute(sd, 1));
    return g;
  }, [count, radius, spread, sizePx, seed]);
  useEffect(
    () => () => {
      geo.dispose();
      mat.dispose();
    },
    [geo, mat],
  );
  const group = useRef<Group>(null!);
  // uniform 对象放进 ref，每帧只改它的 value（React Compiler 的不可变规则不追踪 ref）
  const uniforms = useRef(mat.uniforms);
  useEffect(() => {
    uniforms.current = mat.uniforms;
  }, [mat]);
  useFrame(({ clock, size, viewport }, dt) => {
    const t = clock.elapsedTime;
    const u = uniforms.current;
    u.uTime.value = t;
    u.uScale.value = size.height * 0.5;
    u.uPixelRatio.value = viewport.dpr;
    u.uWarp.value = fx.warp;
    const d = Math.min(dt, 1 / 30);
    const g = group.current;
    const tx = fx.px * parallax, ty = fx.py * parallax;
    g.position.x = reduced ? tx : MathUtils.damp(g.position.x, tx, 3, d);
    g.position.y = reduced ? ty : MathUtils.damp(g.position.y, ty, 3, d);
    g.position.z = fx.warp * 4 * (0.5 + parallax * 4);
    if (!reduced) g.rotation.z = t * drift;
  });
  return (
    <group ref={group}>
      <points geometry={geo} material={mat} frustumCulled={false} />
    </group>
  );
}

export function Starfield({ reduced }: { reduced: boolean }) {
  return (
    <>
      <StarLayer count={3000} radius={72} spread={26} sizePx={1.25} color="#c7d3ff" brightness={0.95} parallax={0.1} drift={0.00022} seed={11} reduced={reduced} />
      <StarLayer count={1500} radius={42} spread={16} sizePx={1.8} color="#e2e7ff" brightness={1} parallax={0.25} drift={0.0003} seed={23} reduced={reduced} />
      <StarLayer count={500} radius={24} spread={10} sizePx={2.6} color="#f2f4ff" brightness={1.15} parallax={0.5} drift={0.0004} seed={37} reduced={reduced} />
    </>
  );
}
