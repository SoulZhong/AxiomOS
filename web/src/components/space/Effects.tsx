"use client";
import { useRef } from "react";
import { useFrame } from "@react-three/fiber";
import { Bloom, DepthOfField, EffectComposer, N8AO, Noise, ToneMapping, Vignette } from "@react-three/postprocessing";
import { BlendFunction, BloomEffect, DepthOfFieldEffect, ToneMappingMode, VignetteEffect, VignetteTechnique } from "postprocessing";
import { HalfFloatType } from "three";
import { fx } from "./director";

/**
 * 后期链：N8AO（半分辨率，接触阴影：上层建筑与船体交接、推进翼根部）→ 薄景深（对焦舰首窗带，焦段 5、bokeh 0.8：只有前景星尘与远处行星微糊，星点保持锐利）
 * → Bloom（mipmap，阈值 1.1，只有 HDR 光源发光）→ ACES → 4.5% 颗粒（兼作 8-bit 抖动）→ 暗角。
 * 不做色散：它会给每颗星镶上红蓝边，看起来像廉价滤镜。
 * EffectComposer 挂载时会把渲染器强制为 NoToneMapping，所以色调映射只能在链尾做。
 * lite 档：无 N8AO、无景深、Bloom 4 级、无多重采样。
 */
export function Effects({ lite }: { lite: boolean }) {
  const dof = useRef<DepthOfFieldEffect>(null);
  const bloom = useRef<BloomEffect>(null);
  const vignette = useRef<VignetteEffect>(null);
  useFrame(() => {
    dof.current?.target?.copy(fx.bowWorld);
    // 登舰：掠航快段泛光略升，入口时扩张到 3.3；暗角随入口退掉，白场干净
    if (bloom.current) bloom.current.intensity = 0.9 + 0.6 * fx.speed + 2.4 * fx.entrance;
    if (vignette.current) vignette.current.darkness = 0.55 * (1 - fx.entrance);
  });
  return (
    <EffectComposer multisampling={lite ? 0 : 4} frameBufferType={HalfFloatType} enableNormalPass={false}>
      {!lite && <N8AO halfRes quality="medium" aoRadius={1.5} distanceFalloff={1} intensity={1.2} color="#06070c" />}
      {!lite && <DepthOfField ref={dof} target={[3, -0.5, 0]} focusRange={5} bokehScale={0.8} />}
      <Bloom ref={bloom} mipmapBlur luminanceThreshold={1.1} luminanceSmoothing={0.12} intensity={0.9} radius={0.7} levels={lite ? 4 : 6} />
      <ToneMapping mode={ToneMappingMode.ACES_FILMIC} />
      <Noise premultiply blendFunction={BlendFunction.ADD} opacity={0.045} />
      <Vignette ref={vignette} offset={0.32} darkness={0.55} technique={VignetteTechnique.DEFAULT} />
    </EffectComposer>
  );
}
