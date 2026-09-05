"use client";
import { useEffect, useMemo, useRef } from "react";
import { useFrame } from "@react-three/fiber";
import { BufferAttribute, BufferGeometry, CanvasTexture, ClampToEdgeWrapping, Color, CylinderGeometry, ExtrudeGeometry, MeshBasicMaterial, MeshPhysicalMaterial, MeshStandardMaterial, NoColorSpace, Shape, SphereGeometry, SRGBColorSpace } from "three";
import { fx } from "../director";
import { buildBowBand, buildHull, buildSuperstructure, DOME, DOME_RADIUS, STERN, WING_DIHEDRAL, WING_OUTLINE, WING_THICKNESS, wx, SVG_STERN_X, type Surface } from "./hullProfile";

/**
 * 船体：轮廓旋转出的水滴形船体（船头高钝、船尾扁平）+ 骑在背上的上层建筑（半个超椭圆管，脊线在船头后方成圆顶）
 * + 两片宽而扁的后掠推进翼 + 穹顶舰桥、天线、桅顶灯 + 船头一圈 accent 全景舷窗带（HDR，Bloom 拾取）。
 * 材质：白色客轮的漆面——MeshPhysicalMaterial，底层哑光（roughness 0.6）+ 薄清漆（clearcoat 0.6 / clearcoatRoughness 0.28）抓住轮廓光；
 * 船体与上层建筑另贴三张程序化贴图（同一套板线：三道环向舱段缝 + 板线 + 检修板）：
 * 反照率对比约 3%、Sobel 法线（normalScale 0.3，只有光掠过时看得见接缝）、逐板 ±0.08 的粗糙度。
 * 舰首窗带按行做顶点色衰减（中心 HDR 1.75、边缘 0.79）：中心过 Bloom 阈值发光，边缘不过——是一条有边的发光窗，不是一团白。
 * 聚焦密码时窗带亮 35%、敲键时 +15%；桅顶灯随敲键闪一次。
 */
const HULL_COLOR = "#d6dae1";
const ACCENT_HOVER = "#828fff";
const BAND_BASE = new Color("#a4adff").multiplyScalar(1.75);
const BEACON_BASE = new Color(ACCENT_HOVER).multiplyScalar(5);

function toGeometry(s: Surface) {
  const g = new BufferGeometry();
  g.setAttribute("position", new BufferAttribute(s.positions, 3));
  g.setAttribute("normal", new BufferAttribute(s.normals, 3));
  g.setIndex(new BufferAttribute(s.indices, 1));
  if (s.uvs) g.setAttribute("uv", new BufferAttribute(s.uvs, 2));
  return g;
}

/**
 * 板线三件套。先在一张 W×H 的高度场上"刻"出：三道环向舱段缝（2px 深槽）、七条沿船轴的板线（1px 浅槽）、
 * 错落的短竖缝、以及每块检修板 ±0.04 的高度差；再由它派生：
 * - 反照率：缝处略深（≤ 3% 对比）；
 * - 法线：Sobel（NoColorSpace）；
 * - 粗糙度：逐板 0.6 ± 0.08 写进绿通道（材质 roughness 设 1，让贴图决定）。
 * u 沿船轴，v 绕周向。
 */
function makePlateMaps(): { albedo: CanvasTexture; normal: CanvasTexture; roughness: CanvasTexture } {
  const W = 1024, H = 256;
  const h = new Float32Array(W * H).fill(0.5);
  const rough = new Float32Array(W * H).fill(0.6);
  const alb = new Float32Array(W * H).fill(1);
  let seed = 7;
  const rnd = () => ((seed = (seed * 16807) % 2147483647) / 2147483647);
  const rect = (x0: number, y0: number, w: number, hh: number, dh: number, da: number) => {
    for (let y = Math.max(0, y0); y < Math.min(H, y0 + hh); y++)
      for (let x = Math.max(0, x0); x < Math.min(W, x0 + w); x++) {
        h[y * W + x] += dh;
        alb[y * W + x] -= da;
      }
  };
  const seamsU = [0.3, 0.53, 0.74];
  const rowsV = [0.08, 0.2, 0.33, 0.5, 0.67, 0.8, 0.92];
  // 检修板：在板线之间随机分格，每格一个高度 / 粗糙度偏移
  const cols: number[] = [0.1];
  while (cols[cols.length - 1] < 0.9) cols.push(cols[cols.length - 1] + 0.03 + rnd() * 0.06);
  const rowEdges = [0, ...rowsV, 1];
  for (let ci = 0; ci < cols.length - 1; ci++) {
    for (let ri = 0; ri < rowEdges.length - 1; ri++) {
      const x0 = Math.round(cols[ci] * W), x1 = Math.round(cols[ci + 1] * W);
      const y0 = Math.round(rowEdges[ri] * H), y1 = Math.round(rowEdges[ri + 1] * H);
      const dh = (rnd() - 0.5) * 0.04;
      const dr = (rnd() - 0.5) * 0.16;
      for (let y = y0; y < y1; y++)
        for (let x = x0; x < x1; x++) {
          h[y * W + x] += dh;
          rough[y * W + x] += dr;
        }
      // 每块板的短竖缝（前缘）
      if (rnd() < 0.55) rect(x0, y0 + 1, 1, y1 - y0 - 2, -0.12, 0.022);
    }
  }
  for (const u of seamsU) rect(Math.round(u * W) - 1, 0, 2, H, -0.35, 0.035);
  for (const v of rowsV) rect(0, Math.round(v * H), W, 1, -0.15, 0.028);

  const make = (fill: (i: number, px: Uint8ClampedArray) => void, srgb: boolean) => {
    const c = document.createElement("canvas");
    c.width = W;
    c.height = H;
    const ctx = c.getContext("2d")!;
    const img = ctx.createImageData(W, H);
    for (let i = 0; i < W * H; i++) fill(i, img.data);
    ctx.putImageData(img, 0, 0);
    const tex = new CanvasTexture(c);
    tex.colorSpace = srgb ? SRGBColorSpace : NoColorSpace;
    tex.wrapS = ClampToEdgeWrapping;
    tex.wrapT = ClampToEdgeWrapping;
    tex.anisotropy = 4;
    return tex;
  };
  const albedo = make((i, px) => {
    const v = Math.round(Math.min(1, Math.max(0, alb[i])) * 255);
    px[i * 4] = v;
    px[i * 4 + 1] = v;
    px[i * 4 + 2] = v;
    px[i * 4 + 3] = 255;
  }, true);
  const S = 2.4; // Sobel 强度
  const normal = make((i, px) => {
    const x = i % W, y = (i - x) / W;
    const xl = Math.max(0, x - 1), xr = Math.min(W - 1, x + 1), yu = Math.max(0, y - 1), yd = Math.min(H - 1, y + 1);
    const dx = (h[y * W + xr] - h[y * W + xl]) * S;
    const dy = (h[yd * W + x] - h[yu * W + x]) * S;
    const len = Math.hypot(dx, dy, 1);
    px[i * 4] = Math.round((-dx / len) * 127.5 + 127.5);
    px[i * 4 + 1] = Math.round((dy / len) * 127.5 + 127.5);
    px[i * 4 + 2] = Math.round((1 / len) * 127.5 + 127.5);
    px[i * 4 + 3] = 255;
  }, false);
  const roughness = make((i, px) => {
    const v = Math.round(Math.min(1, Math.max(0, rough[i])) * 255);
    px[i * 4] = v;
    px[i * 4 + 1] = v;
    px[i * 4 + 2] = v;
    px[i * 4 + 3] = 255;
  }, false);
  return { albedo, normal, roughness };
}

const PHYSICAL = { color: HULL_COLOR, metalness: 0.05, roughness: 0.6, clearcoat: 0.6, clearcoatRoughness: 0.28, envMapIntensity: 0.8, dithering: true } as const;

export function useHullMaterial() {
  const mat = useMemo(() => new MeshPhysicalMaterial(PHYSICAL), []);
  useEffect(() => () => mat.dispose(), [mat]);
  return mat;
}

/** 同一种漆，加板线三件套：只给船体与上层建筑（它们有沿船轴的 UV） */
function usePlatedMaterial() {
  const maps = useMemo(() => makePlateMaps(), []);
  const mat = useMemo(() => {
    const m = new MeshPhysicalMaterial({ ...PHYSICAL, roughness: 1, map: maps.albedo, normalMap: maps.normal, roughnessMap: maps.roughness });
    m.normalScale.set(0.3, 0.3);
    return m;
  }, [maps]);
  useEffect(
    () => () => {
      mat.dispose();
      maps.albedo.dispose();
      maps.normal.dispose();
      maps.roughness.dispose();
    },
    [mat, maps],
  );
  return mat;
}

export function Hull() {
  const white = useHullMaterial();
  const plated = usePlatedMaterial();
  const geos = useMemo(() => {
    const hull = toGeometry(buildHull());
    const sup = toGeometry(buildSuperstructure());
    const band = toGeometry(buildBowBand());
    // 窗带顶点色：每站 5 行（-1, -0.5, 0, 0.5, 1），中心亮、边缘暗
    const n = band.getAttribute("position").count;
    const col = new Float32Array(n * 3);
    const rowGain = [0.45, 0.82, 1, 0.82, 0.45];
    for (let i = 0; i < n; i++) {
      const g = rowGain[i % 5];
      col[i * 3] = g;
      col[i * 3 + 1] = g;
      col[i * 3 + 2] = g;
    }
    band.setAttribute("color", new BufferAttribute(col, 3));
    const shape = new Shape();
    const [r0, t0, t1, t2, t3, r1] = WING_OUTLINE;
    shape.moveTo(r0[0], r0[1]);
    shape.lineTo(t0[0], t0[1]);
    shape.quadraticCurveTo(t1[0], t1[1] + 0.06, t2[0], t2[1]);
    shape.lineTo(t3[0], t3[1]);
    shape.lineTo(r1[0], r1[1]);
    shape.closePath();
    const wing = new ExtrudeGeometry(shape, { depth: WING_THICKNESS, bevelEnabled: true, bevelThickness: 0.03, bevelSize: 0.028, bevelSegments: 4, curveSegments: 12 });
    wing.translate(0, 0, -WING_THICKNESS / 2);
    const dome = new SphereGeometry(DOME_RADIUS, 32, 16, 0, Math.PI * 2, 0, Math.PI / 2);
    const mast = new CylinderGeometry(0.006, 0.009, 0.34, 8);
    const beacon = new SphereGeometry(0.014, 12, 8);
    const stern = new CylinderGeometry(STERN.h * 0.9, STERN.h * 0.9, 0.05, 32);
    return { hull, sup, band, wing, dome, mast, beacon, stern };
  }, []);
  const mats = useMemo(
    () => ({
      band: new MeshBasicMaterial({ color: BAND_BASE.clone(), vertexColors: true, toneMapped: false }),
      beacon: new MeshBasicMaterial({ color: BEACON_BASE.clone(), toneMapped: false }),
      mast: new MeshBasicMaterial({ color: BEACON_BASE.clone(), toneMapped: false }),
      dark: new MeshStandardMaterial({ color: "#5c606a", metalness: 0.5, roughness: 0.5, dithering: true }),
    }),
    [],
  );
  useEffect(
    () => () => {
      Object.values(geos).forEach((g) => g.dispose());
      Object.values(mats).forEach((m) => m.dispose());
    },
    [geos, mats],
  );
  const live = useRef(mats);
  useEffect(() => {
    live.current = mats;
  }, [mats]);
  useFrame(() => {
    const m = live.current;
    m.band.color.copy(BAND_BASE).multiplyScalar(1 + 0.35 * fx.listen + 0.15 * fx.keyPulse);
    m.mast.color.copy(BEACON_BASE).multiplyScalar(1 + 1.6 * fx.keyPulse);
  });
  const wingY = STERN.c + 0.05;
  const WING_TIP = WING_OUTLINE[2];
  return (
    <group>
      <mesh geometry={geos.hull} material={plated} />
      <mesh geometry={geos.sup} material={plated} />
      <mesh geometry={geos.band} material={mats.band} />
      {/* 推进翼：左右各一片，微微上翘 */}
      <mesh geometry={geos.wing} material={white} position={[0, wingY, 0]} rotation={[Math.PI / 2 - WING_DIHEDRAL, 0, 0]} />
      <mesh geometry={geos.wing} material={white} position={[0, wingY, 0]} rotation={[-(Math.PI / 2 - WING_DIHEDRAL), 0, 0]} />
      {/* 船尾：一块略深的尾板，引擎从它探出 */}
      <mesh geometry={geos.stern} material={mats.dark} position={[wx(SVG_STERN_X) - 0.08, STERN.c, 0]} rotation={[0, 0, Math.PI / 2]} scale={[1, 1, STERN.w / STERN.h]} />
      {/* 翼尖航行灯 */}
      {[1, -1].map((side) => (
        <mesh key={side} geometry={geos.beacon} material={mats.beacon} position={[WING_TIP[0], wingY + WING_TIP[1] * Math.sin(WING_DIHEDRAL), side * WING_TIP[1] * Math.cos(WING_DIHEDRAL)]} scale={0.8} />
      ))}
      {/* 穹顶舰桥 + 天线 + 桅顶灯 */}
      <mesh geometry={geos.dome} material={white} position={DOME} scale={[1.15, 0.8, 1]} />
      <mesh geometry={geos.mast} material={mats.dark} position={[DOME[0], DOME[1] + DOME_RADIUS * 0.8 + 0.15, 0]} />
      <mesh geometry={geos.beacon} material={mats.mast} position={[DOME[0], DOME[1] + DOME_RADIUS * 0.8 + 0.33, 0]} />
    </group>
  );
}
