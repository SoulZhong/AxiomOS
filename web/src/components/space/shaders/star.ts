/**
 * 星点着色器：每颗星一个顶点，尺寸随距离衰减（与 three PointsMaterial 同一公式：size × dpr × (height/2) / -z），
 * 30% 的星以各自的相位、15% 的振幅、0.4–1.2 Hz 慢慢明暗；片元是软圆，核心收紧，没有方块也没有"棉花球"。
 * uWarp（0..1）：登舰掠航时点尺寸最多 ×3、亮度略增——配合相机 FOV 变宽、星层前移与速度线就是"曲速"。
 */
export const starVertex = /* glsl */ `
uniform float uTime;
uniform float uScale;
uniform float uPixelRatio;
uniform float uTwinkle;
uniform float uWarp;
attribute float aSize;
attribute float aSeed;
varying float vAlpha;
float hash(float n) { return fract(sin(n * 127.1) * 43758.5453); }
void main() {
  vec4 mv = modelViewMatrix * vec4(position, 1.0);
  float twinkles = step(0.7, aSeed) * uTwinkle;
  float phase = hash(aSeed) * 6.2831;
  float freq = 0.4 + hash(aSeed + 1.0) * 0.8;
  float tw = 1.0 - 0.15 * twinkles * (0.5 + 0.5 * sin(uTime * freq * 6.2831 + phase));
  vAlpha = tw * (0.55 + 0.45 * hash(aSeed + 2.0)) * (1.0 + 0.35 * uWarp);
  gl_PointSize = aSize * uPixelRatio * (uScale / -mv.z) * tw * (1.0 + 2.0 * uWarp);
  gl_Position = projectionMatrix * mv;
}
`;

export const starFragment = /* glsl */ `
uniform vec3 uColor;
varying float vAlpha;
void main() {
  float d = length(gl_PointCoord - 0.5);
  float a = 1.0 - smoothstep(0.16, 0.5, d);
  a *= a;
  if (a < 0.002) discard;
  gl_FragColor = vec4(uColor * a * vAlpha, a * vAlpha);
  #include <colorspace_fragment>
}
`;
