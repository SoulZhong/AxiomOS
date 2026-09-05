/**
 * 星云：全屏三角形上跑 5 层 fbm + 两层域扭曲，烘焙一次到 512×288 的 HalfFloat 目标；
 * 屏幕上再由一张背景平面采样它（慢速漂移 + 鼠标视差），并叠上船尾一侧地平线外的暖色远日与 IGN 抖动。
 *
 * snoise 来自 Ashima Arts / Ian McEwan 的 webgl-noise（MIT）：
 *   Description : Array and textureless GLSL 2D/3D/4D simplex noise functions.
 *   Author : Ian McEwan, Ashima Arts.  License : Copyright (C) 2011 Ashima Arts. All rights reserved.
 *   Distributed under the MIT License. https://github.com/ashima/webgl-noise
 */
const snoise = /* glsl */ `
vec3 mod289(vec3 x) { return x - floor(x * (1.0 / 289.0)) * 289.0; }
vec4 mod289(vec4 x) { return x - floor(x * (1.0 / 289.0)) * 289.0; }
vec4 permute(vec4 x) { return mod289(((x * 34.0) + 1.0) * x); }
vec4 taylorInvSqrt(vec4 r) { return 1.79284291400159 - 0.85373472095314 * r; }
float snoise(vec3 v) {
  const vec2 C = vec2(1.0 / 6.0, 1.0 / 3.0);
  const vec4 D = vec4(0.0, 0.5, 1.0, 2.0);
  vec3 i = floor(v + dot(v, C.yyy));
  vec3 x0 = v - i + dot(i, C.xxx);
  vec3 g = step(x0.yzx, x0.xyz);
  vec3 l = 1.0 - g;
  vec3 i1 = min(g.xyz, l.zxy);
  vec3 i2 = max(g.xyz, l.zxy);
  vec3 x1 = x0 - i1 + C.xxx;
  vec3 x2 = x0 - i2 + C.yyy;
  vec3 x3 = x0 - D.yyy;
  i = mod289(i);
  vec4 p = permute(permute(permute(i.z + vec4(0.0, i1.z, i2.z, 1.0)) + i.y + vec4(0.0, i1.y, i2.y, 1.0)) + i.x + vec4(0.0, i1.x, i2.x, 1.0));
  float n_ = 0.142857142857;
  vec3 ns = n_ * D.wyz - D.xzx;
  vec4 j = p - 49.0 * floor(p * ns.z * ns.z);
  vec4 x_ = floor(j * ns.z);
  vec4 y_ = floor(j - 7.0 * x_);
  vec4 x = x_ * ns.x + ns.yyyy;
  vec4 y = y_ * ns.x + ns.yyyy;
  vec4 h = 1.0 - abs(x) - abs(y);
  vec4 b0 = vec4(x.xy, y.xy);
  vec4 b1 = vec4(x.zw, y.zw);
  vec4 s0 = floor(b0) * 2.0 + 1.0;
  vec4 s1 = floor(b1) * 2.0 + 1.0;
  vec4 sh = -step(h, vec4(0.0));
  vec4 a0 = b0.xzyw + s0.xzyw * sh.xxyy;
  vec4 a1 = b1.xzyw + s1.xzyw * sh.zzww;
  vec3 p0 = vec3(a0.xy, h.x);
  vec3 p1 = vec3(a0.zw, h.y);
  vec3 p2 = vec3(a1.xy, h.z);
  vec3 p3 = vec3(a1.zw, h.w);
  vec4 norm = taylorInvSqrt(vec4(dot(p0, p0), dot(p1, p1), dot(p2, p2), dot(p3, p3)));
  p0 *= norm.x; p1 *= norm.y; p2 *= norm.z; p3 *= norm.w;
  vec4 m = max(0.6 - vec4(dot(x0, x0), dot(x1, x1), dot(x2, x2), dot(x3, x3)), 0.0);
  m = m * m;
  return 42.0 * dot(m * m, vec4(dot(p0, x0), dot(p1, x1), dot(p2, x2), dot(p3, x3)));
}
`;

export const fullscreenVertex = /* glsl */ `
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = vec4(position.xy, 0.9999, 1.0);
}
`;

/** 烘焙：输出线性颜色，贡献已按 ≤ 0.1 压低 */
export const nebulaBakeFragment = /* glsl */ `
precision highp float;
uniform float uTime;
uniform vec2 uRes;
uniform vec3 uColorA;
uniform vec3 uColorB;
varying vec2 vUv;
${snoise}
float fbm(vec3 p) {
  float v = 0.0, a = 0.5;
  for (int i = 0; i < 4; i++) { v += a * snoise(p); p = p * 2.02 + 17.0; a *= 0.5; }
  return v;
}
void main() {
  vec2 uv = vUv;
  vec2 p = (uv - 0.5) * vec2(uRes.x / uRes.y, 1.0) * 1.15;
  float t = uTime * 0.015;
  vec2 q = vec2(fbm(vec3(p, t)), fbm(vec3(p + 5.2, t + 1.3)));
  vec2 r = vec2(fbm(vec3(p + 1.1 * q + vec2(1.7, 9.2), t)), fbm(vec3(p + 1.1 * q + vec2(8.3, 2.8), t)));
  float f = fbm(vec3(p + 0.9 * r, t));
  // 软的丝缕结构 + 一团更大更软的底光，一起只在右上角
  float falloff = smoothstep(1.35, 0.1, length((p - vec2(0.5, 0.36)) * vec2(0.8, 1.2)));
  float mask = smoothstep(-0.35, 0.75, f) * falloff + falloff * falloff * 0.45;
  vec3 col = mix(uColorA, uColorB, clamp(r.x * 0.5 + 0.5, 0.0, 1.0));
  gl_FragColor = vec4(col * mask, 1.0);
}
`;

/** 显示：采样烘焙结果（慢速漂移 + 视差）+ 暖色远日 + IGN 抖动 */
export const nebulaDisplayFragment = /* glsl */ `
precision highp float;
uniform sampler2D uMap;
uniform vec2 uOffset;
uniform float uAspect;
uniform float uStrength;
uniform vec3 uSun;
uniform float uSunStrength;
varying vec2 vUv;
float ign(vec2 px) { return fract(52.9829189 * fract(dot(px, vec2(0.06711056, 0.00583715)))); }
void main() {
  vec2 uv = vUv + uOffset;
  vec3 col = texture2D(uMap, uv).rgb * uStrength;
  // 船尾一侧（左下）地平线外的远日：一大团极淡的暖光 + 更淡的整片渐变
  vec2 sp = (vUv - vec2(0.14, -0.06)) * vec2(uAspect, 1.0);
  float d = length(sp);
  float sun = smoothstep(0.95, 0.0, d);
  sun = sun * sun;
  float haze = smoothstep(2.2, 0.0, d) * 0.1;
  col += uSun * (sun + haze) * uSunStrength;
  col += (ign(gl_FragCoord.xy) - 0.5) / 2048.0;
  gl_FragColor = vec4(max(col, 0.0), 1.0);
}
`;
