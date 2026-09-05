"use client";
import { useEffect, useMemo } from "react";
import { AdditiveBlending, BackSide, Color, ShaderMaterial, SphereGeometry, Vector3 } from "three";

/**
 * 背景行星：画面右下角、远处（z −70）一颗大而暗的行星，只在角落露出一段弧，给母舰做尺度参照。
 * 单个球 + 紧凑着色器，不用贴图：远日在它身后偏左，朝向我们的一面几乎全在阴影里，只有左缘一弯暖色晨昏线，
 * 一圈薄薄的 Fresnel 大气边缘（accent 蓝），外面再套一层 BackSide additive 的柔和大气晕。
 * 亮度上限约为船体的 1/4，不抢戏。表面只有两层 hash 噪声的纬向云带。
 */
const SUN_DIR = new Vector3(-0.8, 0.18, -0.55).normalize();
const ATMO = new Color("#5f7cff");
const WARM = new Color("#ffb98a");

const vertex = /* glsl */ `
varying vec3 vN;
varying vec3 vW;
void main() {
  vN = normalize(mat3(modelMatrix) * normal);
  vec4 w = modelMatrix * vec4(position, 1.0);
  vW = w.xyz;
  gl_Position = projectionMatrix * viewMatrix * w;
}
`;
const surface = /* glsl */ `
uniform vec3 uSun;
uniform vec3 uAtmo;
uniform vec3 uWarm;
varying vec3 vN;
varying vec3 vW;
float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
float vnoise(vec2 p) {
  vec2 i = floor(p), f = fract(p);
  f = f * f * (3.0 - 2.0 * f);
  return mix(mix(hash(i), hash(i + vec2(1, 0)), f.x), mix(hash(i + vec2(0, 1)), hash(i + vec2(1, 1)), f.x), f.y);
}
void main() {
  vec3 N = normalize(vN);
  vec3 V = normalize(cameraPosition - vW);
  float ndl = dot(N, uSun);
  float ndv = max(dot(N, V), 0.0);
  // 纬向云带：两层噪声，只在日照侧看得见
  float lat = asin(clamp(N.y, -1.0, 1.0));
  float band = 0.8 + 0.2 * vnoise(vec2(lat * 9.0 + vnoise(vec2(N.x * 4.0, N.z * 4.0)) * 1.5, N.x * 2.0));
  band *= 0.9 + 0.1 * vnoise(vec2(N.x * 14.0, N.z * 14.0 + lat * 6.0));
  float day = smoothstep(-0.06, 0.32, ndl);
  vec3 dark = vec3(0.006, 0.008, 0.015);
  vec3 lit = vec3(0.20, 0.18, 0.17) * band;
  vec3 col = mix(dark, lit, day) * 0.36;
  // 晨昏线：暖色一道
  float term = smoothstep(-0.10, 0.0, ndl) * (1.0 - smoothstep(0.0, 0.16, ndl));
  col += uWarm * term * 0.14;
  // 大气边缘：Fresnel，暗面也有一点（散射）
  float f = pow(1.0 - ndv, 4.0);
  col += uAtmo * f * (0.07 + 0.28 * smoothstep(-0.5, 0.4, ndl));
  gl_FragColor = vec4(col, 1.0);
}
`;
const halo = /* glsl */ `
uniform vec3 uSun;
uniform vec3 uAtmo;
varying vec3 vN;
varying vec3 vW;
void main() {
  vec3 N = normalize(vN);
  vec3 V = normalize(cameraPosition - vW);
  // BackSide：法线背向相机，dot 为负；边缘处趋近 0
  float rim = pow(clamp(1.0 + dot(N, V) * 1.15, 0.0, 1.0), 3.0);
  float lit = 0.35 + 0.65 * smoothstep(-0.6, 0.5, dot(N, uSun));
  gl_FragColor = vec4(uAtmo * rim * lit * 0.28, rim * lit);
}
`;

export function Planet() {
  const geo = useMemo(() => new SphereGeometry(1, 96, 64), []);
  const mats = useMemo(
    () => ({
      surface: new ShaderMaterial({ vertexShader: vertex, fragmentShader: surface, uniforms: { uSun: { value: SUN_DIR }, uAtmo: { value: ATMO }, uWarm: { value: WARM } } }),
      halo: new ShaderMaterial({ vertexShader: vertex, fragmentShader: halo, uniforms: { uSun: { value: SUN_DIR }, uAtmo: { value: ATMO } }, side: BackSide, transparent: true, depthWrite: false, blending: AdditiveBlending }),
    }),
    [],
  );
  useEffect(
    () => () => {
      geo.dispose();
      mats.surface.dispose();
      mats.halo.dispose();
    },
    [geo, mats],
  );
  return (
    <group position={[46, -46, -70]}>
      <mesh geometry={geo} material={mats.surface} scale={28} />
      <mesh geometry={geo} material={mats.halo} scale={28 * 1.04} />
    </group>
  );
}
