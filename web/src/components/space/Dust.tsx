"use client";
import { useEffect, useMemo, useRef } from "react";
import { useFrame } from "@react-three/fiber";
import { AdditiveBlending, BufferAttribute, BufferGeometry, Color, ShaderMaterial, Vector3 } from "three";
import { CAMERA_FOV, CAMERA_Y, CAMERA_Z } from "./CameraRig";
import { fx } from "./director";
import { mulberry32 } from "./ship/hullProfile";

/**
 * 前景星尘：相机与母舰之间（z 2.5–10）的一片细小颗粒，按视锥形状分布；additive、随距离衰减、极慢的各自漂移、极淡的 accent 色。
 * 对光标有反应：离光标射线 1.4 单位内的颗粒被轻轻推开（着色器里按到射线的距离算，CPU 只传射线）；
 * 起航时向相机流过来（uWarp）。full 档 480 颗，lite 档 240 颗；reduced-motion 时静止、不推。
 */
const TINT = "#aab4ff";

const vertex = /* glsl */ `
uniform float uTime;
uniform float uScale;
uniform float uPixelRatio;
uniform float uWarp;
uniform float uRepel;
uniform vec3 uRayO;
uniform vec3 uRayD;
attribute float aSize;
attribute float aSeed;
varying float vAlpha;
void main() {
  vec3 p = position;
  float s = aSeed * 6.2831;
  // 各自的慢漂移：三个不同频率的正弦，幅度 0.12
  p += vec3(sin(uTime * 0.11 + s), cos(uTime * 0.09 + s * 1.7), sin(uTime * 0.07 + s * 2.3)) * 0.12;
  // 起航：向相机流动
  p.z += uWarp * 2.5 * (0.5 + aSeed);
  // 光标斥力：到射线的距离
  vec3 rel = p - uRayO;
  float t = max(dot(rel, uRayD), 0.0);
  vec3 away = rel - uRayD * t;
  float d = length(away);
  float push = uRepel * smoothstep(1.4, 0.0, d) * 0.55;
  p += (d > 1e-4 ? away / d : vec3(0.0)) * push;
  vec4 mv = modelViewMatrix * vec4(p, 1.0);
  float dist = -mv.z;
  vAlpha = (0.18 + 0.5 * aSeed) * smoothstep(0.8, 3.0, dist) * (1.0 + 0.6 * uWarp);
  gl_PointSize = aSize * uPixelRatio * (uScale / dist);
  gl_Position = projectionMatrix * mv;
}
`;
const fragment = /* glsl */ `
uniform vec3 uColor;
varying float vAlpha;
void main() {
  float d = length(gl_PointCoord - 0.5);
  float a = 1.0 - smoothstep(0.1, 0.5, d);
  a *= a * vAlpha;
  if (a < 0.003) discard;
  gl_FragColor = vec4(uColor * a, a);
  #include <colorspace_fragment>
}
`;

export function Dust({ reduced, count }: { reduced: boolean; count: number }) {
  const mat = useMemo(
    () =>
      new ShaderMaterial({
        vertexShader: vertex,
        fragmentShader: fragment,
        uniforms: {
          uTime: { value: 0 },
          uScale: { value: 450 },
          uPixelRatio: { value: 1 },
          uWarp: { value: 0 },
          uRepel: { value: reduced ? 0 : 1 },
          uRayO: { value: new Vector3(0, CAMERA_Y, CAMERA_Z) },
          uRayD: { value: new Vector3(0, 0, -1) },
          uColor: { value: new Color(TINT).multiplyScalar(0.8) },
        },
        transparent: true,
        depthWrite: false,
        blending: AdditiveBlending,
      }),
    [reduced],
  );
  const geo = useMemo(() => {
    const rng = mulberry32(0x44555354);
    const pos = new Float32Array(count * 3), sz = new Float32Array(count), sd = new Float32Array(count);
    const tanH = Math.tan((CAMERA_FOV * Math.PI) / 360);
    for (let i = 0; i < count; i++) {
      const z = 2.5 + rng() * 7.5;
      const dist = CAMERA_Z - z;
      const hh = tanH * dist * 1.35, hw = hh * 1.9;
      pos[i * 3] = (rng() * 2 - 1) * hw;
      pos[i * 3 + 1] = CAMERA_Y + (rng() * 2 - 1) * hh;
      pos[i * 3 + 2] = z;
      sz[i] = 0.012 + rng() * 0.022;
      sd[i] = rng();
    }
    const g = new BufferGeometry();
    g.setAttribute("position", new BufferAttribute(pos, 3));
    g.setAttribute("aSize", new BufferAttribute(sz, 1));
    g.setAttribute("aSeed", new BufferAttribute(sd, 1));
    return g;
  }, [count]);
  useEffect(
    () => () => {
      geo.dispose();
      mat.dispose();
    },
    [geo, mat],
  );
  const uniforms = useRef(mat.uniforms);
  useEffect(() => {
    uniforms.current = mat.uniforms;
  }, [mat]);
  useFrame(({ clock, size, viewport }) => {
    const u = uniforms.current;
    if (!reduced) u.uTime.value = clock.elapsedTime;
    u.uScale.value = size.height * 0.5;
    u.uPixelRatio.value = viewport.dpr;
    u.uWarp.value = Math.max(fx.warp, fx.entrance);
    (u.uRayO.value as Vector3).copy(fx.cursorRayOrigin);
    (u.uRayD.value as Vector3).copy(fx.cursorRayDir);
  });
  return <points geometry={geo} material={mat} frustumCulled={false} />;
}
