import { BufferAttribute, BufferGeometry, Color, DynamicDrawUsage, Matrix4, Mesh, MeshPhysicalMaterial, MeshStandardMaterial, Vector3 } from "three";
import { mulberry32, SHIP_LEN } from "./hullProfile";

/**
 * Blender 母舰模型（public/models/axiom-opt.glb）在两个场景里共用的部分：登录页的全尺寸场景（ShipModel）与应用内「舰况」面板的
 * 小场景（ship-status/ShipScene）。这里只有纯函数与常量：材质常量、船体材质、窗片分组、几何分析；每帧怎么点灯由各自的场景决定。
 * 模型 +x 为船头、+y 向上、长 10 单位、原点在船体中心；整体缩到 SHIP_LEN（6.2）。
 */
export const MODEL_LEN = 10;
export const MODEL_SCALE = SHIP_LEN / MODEL_LEN;

export const HULL_COLOR = "#d6dae1";
export const ACCENT = "#5e6ad2";
export const ACCENT_HOVER = "#828fff";
export const WINDOW_BASE = new Color(1.85, 1.76, 1.55);
export const BAND_BASE = new Color("#a4adff").multiplyScalar(1.75);
export const BEACON_BASE = new Color(ACCENT_HOVER).multiplyScalar(5);
export const ENGINE_BASE = new Color(ACCENT_HOVER).multiplyScalar(4);
export const WARM = new Color("#fff1dc");
const TWINKLE_RATIO = 0.08;

export type ShipNodes = { hull: Mesh; windows: Mesh; band: Mesh; engine: Mesh; beacon: Mesh };

export type Windows = { groups: Int32Array[]; cx: Float32Array; cy: Float32Array; base: Float32Array; tw: Uint8Array; period: Float32Array; phase: Float32Array; bridge: number[]; /** 固定种子的 0–1 排序键：按"点亮比例"点灯时用 */ rank: Float32Array };

/** 按索引连通性把窗片分组（对 meshopt 重排稳健），质心转到模型空间 */
export function analyzeWindows(geo: BufferGeometry, nodeMatrix: Matrix4): Windows {
  const pos = geo.getAttribute("position");
  const idx = geo.getIndex()!;
  const n = pos.count;
  const parent = new Int32Array(n);
  for (let i = 0; i < n; i++) parent[i] = i;
  const find = (a: number): number => {
    while (parent[a] !== a) {
      parent[a] = parent[parent[a]];
      a = parent[a];
    }
    return a;
  };
  const union = (a: number, b: number) => {
    const ra = find(a), rb = find(b);
    if (ra !== rb) parent[ra] = rb;
  };
  for (let t = 0; t < idx.count; t += 3) {
    union(idx.getX(t), idx.getX(t + 1));
    union(idx.getX(t), idx.getX(t + 2));
  }
  const byRoot = new Map<number, number[]>();
  for (let i = 0; i < n; i++) {
    const r = find(i);
    let g = byRoot.get(r);
    if (!g) byRoot.set(r, (g = []));
    g.push(i);
  }
  const v = new Vector3();
  const raw = [...byRoot.values()].map((verts) => {
    const c = new Vector3();
    for (const i of verts) c.add(v.fromBufferAttribute(pos, i).applyMatrix4(nodeMatrix));
    c.divideScalar(verts.length);
    return { verts, c };
  });
  // 没焊在一起的窗片：质心相距 < 0.02 的并成一块
  raw.sort((a, b) => a.c.x - b.c.x);
  const merged: { verts: number[]; c: Vector3 }[] = [];
  for (const g of raw) {
    const last = merged[merged.length - 1];
    if (last && last.c.distanceTo(g.c) < 0.02) {
      last.verts.push(...g.verts);
    } else merged.push({ verts: [...g.verts], c: g.c.clone() });
  }
  const m = merged.length;
  const rng = mulberry32(0x57494e44), trng = mulberry32(0x5354415a), rrng = mulberry32(0x52414e4b);
  const out: Windows = { groups: merged.map((g) => Int32Array.from(g.verts)), cx: new Float32Array(m), cy: new Float32Array(m), base: new Float32Array(m), tw: new Uint8Array(m), period: new Float32Array(m), phase: new Float32Array(m), bridge: [], rank: new Float32Array(m) };
  merged.forEach((g, i) => {
    out.cx[i] = g.c.x;
    out.cy[i] = g.c.y;
    out.base[i] = 0.55 + rng() * 0.6;
    out.rank[i] = rrng();
    if (trng() < TWINKLE_RATIO) {
      out.tw[i] = 1;
      out.period[i] = 3 + trng() * 4;
      out.phase[i] = trng() * Math.PI * 2;
    }
    // 舰桥附近：穹顶（x≈2.5）前后 1.8、上层建筑
    if (Math.abs(g.c.x - 2.5) < 1.8 && g.c.y > 0.35) out.bridge.push(i);
  });
  return out;
}

export function addColorAttribute(geo: BufferGeometry, dynamic: boolean) {
  const attr = new BufferAttribute(new Float32Array(geo.getAttribute("position").count * 3), 3);
  if (dynamic) attr.setUsage(DynamicDrawUsage);
  geo.setAttribute("color", attr);
  return attr;
}

export function modelY(geo: BufferGeometry, nodeMatrix: Matrix4) {
  const pos = geo.getAttribute("position");
  const v = new Vector3();
  const ys = new Float32Array(pos.count);
  for (let i = 0; i < pos.count; i++) ys[i] = v.fromBufferAttribute(pos, i).applyMatrix4(nodeMatrix).y;
  return ys;
}

/** 船体材质：保留烘焙法线 / ORM / 自发光贴图，换成带清漆的 MeshPhysicalMaterial。调用方负责 dispose。 */
export function makeHullMaterial(src: MeshStandardMaterial) {
  const hullMat = new MeshPhysicalMaterial({
    color: new Color(HULL_COLOR),
    map: src.map,
    normalMap: src.normalMap,
    roughnessMap: src.roughnessMap,
    metalnessMap: src.metalnessMap,
    aoMap: src.aoMap,
    aoMapIntensity: 1,
    emissiveMap: src.emissiveMap,
    emissive: new Color(0xffffff),
    emissiveIntensity: 0.6,
    metalness: 0.5, // × ORM 蓝通道（≈0.1）
    roughness: 1, // × ORM 绿通道（0.49–0.75）
    clearcoat: 0.6,
    clearcoatRoughness: 0.28,
    envMapIntensity: 1.0,
    dithering: true,
  });
  hullMat.normalScale.set(0.6, 0.6);
  return hullMat;
}

export interface ShipAnalysis {
  win: Windows;
  winColor: BufferAttribute;
  beaconColor: BufferAttribute;
  /** 桅顶灯的顶点索引 */
  mast: number[];
  /** 两枚引擎的质心（模型空间） */
  engines: Vector3[];
  engineX: number;
  radius: number;
  bandCenter: Vector3;
  prox: Float32Array;
}

/**
 * 几何分析：窗片分组、窗带 / 桅灯的顶点色、引擎位置。只算一次（几何来自 useGLTF 缓存）。
 * 会在 windows / band / beacon 的几何上加 color 属性（窗与桅灯是动态的，窗带是静态的）。
 */
export function analyzeShip({ windows, band, beacon, engine }: Omit<ShipNodes, "hull">): ShipAnalysis {
  windows.updateMatrix();
  band.updateMatrix();
  beacon.updateMatrix();
  engine.updateMatrix();
  const win = analyzeWindows(windows.geometry, windows.matrix);
  const winColor = addColorAttribute(windows.geometry, true);
  // 窗带：中心 1，上下缘 0.45
  const bandColor = addColorAttribute(band.geometry, false);
  const by = modelY(band.geometry, band.matrix);
  let lo = Infinity, hi = -Infinity;
  for (const y of by) {
    lo = Math.min(lo, y);
    hi = Math.max(hi, y);
  }
  const mid = (lo + hi) / 2, half = Math.max(1e-4, (hi - lo) / 2);
  for (let i = 0; i < by.length; i++) {
    const u = (by[i] - mid) / half;
    const g = 1 - 0.55 * u * u;
    bandColor.setXYZ(i, BAND_BASE.r * g, BAND_BASE.g * g, BAND_BASE.b * g);
  }
  // 桅顶灯：最高的那簇
  const beaconColor = addColorAttribute(beacon.geometry, true);
  const beY = modelY(beacon.geometry, beacon.matrix);
  let top = -Infinity;
  for (const y of beY) top = Math.max(top, y);
  const mast: number[] = [];
  for (let i = 0; i < beY.length; i++) {
    const isMast = beY[i] > top - 0.6;
    if (isMast) mast.push(i);
    const k = isMast ? 1 : 0.8;
    beaconColor.setXYZ(i, BEACON_BASE.r * k, BEACON_BASE.g * k, BEACON_BASE.b * k);
  }
  // 引擎：按 z 的正负分成两枚，各自的质心与 x 最小值（喷口朝 -x）
  const ep = engine.geometry.getAttribute("position");
  const v = new Vector3();
  const acc = [new Vector3(), new Vector3()], cnt = [0, 0];
  let xmin = Infinity, ymin = Infinity, ymax = -Infinity;
  for (let i = 0; i < ep.count; i++) {
    v.fromBufferAttribute(ep, i).applyMatrix4(engine.matrix);
    const s = v.z >= 0 ? 0 : 1;
    acc[s].add(v);
    cnt[s]++;
    xmin = Math.min(xmin, v.x);
    ymin = Math.min(ymin, v.y);
    ymax = Math.max(ymax, v.y);
  }
  const engines = acc.map((a, i) => a.divideScalar(Math.max(1, cnt[i])));
  const radius = (ymax - ymin) / 2;
  // 舰首窗带中心（模型空间）
  const bp = band.geometry.getAttribute("position");
  const bc = new Vector3();
  for (let i = 0; i < bp.count; i++) bc.add(v.fromBufferAttribute(bp, i).applyMatrix4(band.matrix));
  bc.divideScalar(Math.max(1, bp.count));
  return { win, winColor, beaconColor, mast, engines, engineX: xmin, radius, bandCenter: bc, prox: new Float32Array(win.groups.length) };
}

/** 把桅顶灯写成 base × k（只在 k 变化时调用） */
export function setMast(a: ShipAnalysis, k: number) {
  for (const i of a.mast) a.beaconColor.setXYZ(i, BEACON_BASE.r * k, BEACON_BASE.g * k, BEACON_BASE.b * k);
  a.beaconColor.needsUpdate = true;
}
