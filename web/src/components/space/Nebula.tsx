"use client";
import { useEffect, useMemo, useRef } from "react";
import { useFrame, useThree } from "@react-three/fiber";
import { useFBO } from "@react-three/drei";
import { Color, HalfFloatType, Mesh, OrthographicCamera, PlaneGeometry, Scene, ShaderMaterial, Vector2 } from "three";
import { fullscreenVertex, nebulaBakeFragment, nebulaDisplayFragment } from "./shaders/nebula";

/**
 * 星云 + 远日背景：fbm 星云烘焙一次到 512×288 HalfFloat 目标（首帧），屏幕上的背景平面采样它，
 * 慢速漂移 + 鼠标视差（最远的一层）；暖色远日与抖动在显示着色器里。
 */
const FBO_W = 512;
const FBO_H = 288;
const NEBULA_A = "#2c2f6b"; // 靛蓝
const NEBULA_B = "#4a3a5c"; // 暖灰紫
const SUN = "#ffd9a0"; // --c-sun

export function Nebula({ reduced }: { reduced: boolean }) {
  const fbo = useFBO(FBO_W, FBO_H, { type: HalfFloatType, depthBuffer: false, stencilBuffer: false });
  const bake = useMemo(() => {
    const scene = new Scene();
    const camera = new OrthographicCamera(-1, 1, 1, -1, 0, 1);
    const material = new ShaderMaterial({
      vertexShader: fullscreenVertex,
      fragmentShader: nebulaBakeFragment,
      uniforms: {
        uTime: { value: 0 },
        uRes: { value: new Vector2(FBO_W, FBO_H) },
        uColorA: { value: new Color(NEBULA_A) },
        uColorB: { value: new Color(NEBULA_B) },
      },
      depthTest: false,
      depthWrite: false,
    });
    const geometry = new PlaneGeometry(2, 2);
    scene.add(new Mesh(geometry, material));
    return { scene, camera, material, geometry };
  }, []);
  const display = useMemo(
    () =>
      new ShaderMaterial({
        vertexShader: fullscreenVertex,
        fragmentShader: nebulaDisplayFragment,
        uniforms: {
          uMap: { value: fbo.texture },
          uOffset: { value: new Vector2(0, 0) },
          uAspect: { value: 1.6 },
          uStrength: { value: 0.18 },
          uSun: { value: new Color(SUN) },
          uSunStrength: { value: 0.03 },
        },
        depthTest: false,
        depthWrite: false,
      }),
    [fbo],
  );
  useEffect(
    () => () => {
      bake.material.dispose();
      bake.geometry.dispose();
      display.dispose();
    },
    [bake, display],
  );
  const baked = useRef(false);
  const size = useThree((s) => s.size);
  const uniforms = useRef(display.uniforms);
  useEffect(() => {
    uniforms.current = display.uniforms;
  }, [display]);
  useFrame(({ gl, pointer, clock }, dt) => {
    const u = uniforms.current;
    if (!baked.current) {
      gl.setRenderTarget(fbo);
      gl.render(bake.scene, bake.camera);
      gl.setRenderTarget(null);
      baked.current = true;
    }
    u.uAspect.value = size.width / size.height;
    const d = Math.min(dt, 1 / 30);
    const t = clock.elapsedTime;
    const off = u.uOffset.value as Vector2;
    const tx = reduced ? 0 : -pointer.x * 0.006 + t * 0.00035;
    const ty = reduced ? 0 : -pointer.y * 0.004 + t * 0.00012;
    off.x = reduced ? tx : MathUtils_damp(off.x, tx, 2.5, d);
    off.y = reduced ? ty : MathUtils_damp(off.y, ty, 2.5, d);
  });
  return (
    <mesh material={display} renderOrder={-10} frustumCulled={false}>
      <planeGeometry args={[2, 2]} />
    </mesh>
  );
}

function MathUtils_damp(x: number, y: number, lambda: number, dt: number) {
  return x + (y - x) * (1 - Math.exp(-lambda * dt));
}
