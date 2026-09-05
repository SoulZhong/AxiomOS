/**
 * 母舰轮廓：纯数学，不依赖 three，WebGL 母舰与 Canvas2D/SVG 静帧共用。
 *
 * 第一部分是原 SVG 手绘轮廓（viewBox 1000×420，船尾在左、船头在右）：船体上缘、钝圆船头、微鼓的船底、
 * 从船尾掠起在船头后方收成圆顶的上层建筑弧线。SVG 静帧直接用它的 path 字符串。
 *
 * 第二部分把这条侧视轮廓变成三维截面：沿船轴每个"站位"取上缘 / 下缘得到半高 h 与中心 c，
 * 截面是压扁的椭圆（半宽 w = h × aspect，船头 1.3 近圆、船尾 2.1 扁平），旋转成船体；
 * 上层建筑是骑在船体背上的半个超椭圆管，肩线正好落在船体表面。
 * 舷窗按固定的截面角度排成几排（下 4 排在船体、上 3 排在上层建筑），沿站位每 13 个 SVG 单位一盏，
 * 上层建筑越高的排越靠前才开始（越靠船尾越短）。
 *
 * 世界坐标：x 沿船轴（船头 +x），y 向上，z 朝观众；船长 SHIP_LEN。
 */

export type Pt = [number, number];
type Seg = [Pt, Pt, Pt, Pt]; // 三次贝塞尔：起点、控制点 1、控制点 2、终点
export type V3 = [number, number, number];

export const VB_W = 1000;
export const VB_H = 420;

/* ---------- 一、SVG 侧视轮廓 ---------- */

const STERN_TOP: Pt = [110, 246];
const STERN_BOTTOM: Pt = [110, 314];
/** 船体上缘（船体与上层建筑的分界）：船尾 → 船头顶，几乎水平、微微抬头 */
const HULL_TOP: Seg[] = [[STERN_TOP, [400, 242], [720, 214], [886, 200]]];
/** 船头正面：顶 → 最前端 → 底。又高又钝的圆头 */
const BOW: Seg[] = [
  [[886, 200], [944, 200], [982, 228], [982, 264]],
  [[982, 264], [982, 300], [944, 332], [886, 334]],
];
/** 船底：船头底 → 船尾底。微微鼓起的肚子 */
const BELLY: Seg[] = [[[886, 334], [620, 352], [300, 340], STERN_BOTTOM]];
/** 上层建筑上缘：从船尾甲板起一道向前升起的弧，在船头后方收成圆顶，再落到船头顶 */
const SWEEP_START: Pt = [170, 243];
const SWEEP: Seg[] = [
  [SWEEP_START, [330, 240], [540, 150], [730, 138]],
  [[730, 138], [800, 134], [860, 150], [886, 200]],
];
export const SVG_STERN_X = STERN_TOP[0];
export const SVG_NOSE_X = 982;
export const SVG_DOME: Pt = [752, 138];

const segD = (s: Seg) => `C ${s[1][0]} ${s[1][1]} ${s[2][0]} ${s[2][1]} ${s[3][0]} ${s[3][1]}`;
const revSeg = (s: Seg): Seg => [s[3], s[2], s[1], s[0]];
/** 船体：一条流畅的水滴形 */
export const HULL_D = `M ${STERN_TOP[0]} ${STERN_TOP[1]} ${HULL_TOP.map(segD).join(" ")} ${BOW.map(segD).join(" ")} ${BELLY.map(segD).join(" ")} Z`;
/** 上层建筑：上缘的弧 + 沿船体上缘折返 */
export const SUPER_D = `M ${SWEEP_START[0]} ${SWEEP_START[1]} ${SWEEP.map(segD).join(" ")} ${HULL_TOP.map(revSeg).map(segD).join(" ")} Z`;
/** 迎光边缘：船尾甲板 + 上层建筑从船尾到弧顶的一段 */
export const RIM_D = `M ${STERN_TOP[0]} ${STERN_TOP[1]} L 170 243 ${segD(SWEEP[0])}`;

function cubic(s: Seg, t: number): Pt {
  const u = 1 - t;
  const a = u * u * u, b = 3 * u * u * t, c = 3 * u * t * t, d = t * t * t;
  return [a * s[0][0] + b * s[1][0] + c * s[2][0] + d * s[3][0], a * s[0][1] + b * s[1][1] + c * s[2][1] + d * s[3][1]];
}
function sample(segs: Seg[], n = 96): Pt[] {
  const out: Pt[] = [];
  for (const s of segs) for (let i = 0; i <= n; i++) out.push(cubic(s, i / n));
  return out;
}
/** 在单调多段线上按 x 插值 y（或按 y 插值 x） */
function interp(poly: Pt[], v: number, axis: 0 | 1): number | null {
  const o = axis === 0 ? 1 : 0;
  for (let i = 1; i < poly.length; i++) {
    const a = poly[i - 1], b = poly[i];
    const lo = Math.min(a[axis], b[axis]), hi = Math.max(a[axis], b[axis]);
    if (v >= lo && v <= hi && hi > lo) return a[o] + ((v - a[axis]) / (b[axis] - a[axis])) * (b[o] - a[o]);
  }
  return null;
}

const HULL_TOP_POLY = sample(HULL_TOP);
const SWEEP_POLY = sample(SWEEP);
const BELLY_POLY = sample(BELLY).reverse(); // 让 x 递增
const BOW_POLY = sample(BOW); // y 递增
const BOW_UPPER = sample([BOW[0]]);
const BOW_LOWER = sample([BOW[1]]);
export const hullTopAt = (x: number) => interp(HULL_TOP_POLY, x, 0);
export const sweepAt = (x: number) => interp(SWEEP_POLY, x, 0);
export const bellyAt = (x: number) => interp(BELLY_POLY, x, 0);
export const bowFrontAt = (y: number) => interp(BOW_POLY, y, 1);

/** 龙骨线：船底上方 8 单位的一条平行线（SVG 静帧用） */
export const KEEL_D = BELLY_POLY.filter((_, i) => i % 6 === 0)
  .map(([x, y], i) => `${i ? "L" : "M"} ${x.toFixed(1)} ${(y - 8).toFixed(1)}`)
  .join(" ");

/** mulberry32：固定种子，每次渲染都得到同一批闪烁灯 */
export function mulberry32(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** SVG 静帧的舷窗：几排跟着船身曲线走的 2×2 小方块，合成一条 path */
export function svgLightsPath(): string {
  const SWEEP_ROWS = [14, 32, 50];
  const HULL_ROWS = [18, 40, 62, 84];
  const pts: Pt[] = [];
  const inBow = (x: number, y: number) => {
    if (x <= 880) return true;
    const xf = bowFrontAt(y);
    return xf !== null && x <= xf - 16;
  };
  for (const d of SWEEP_ROWS) {
    for (let x = 128; x <= 975; x += 10) {
      const ys = sweepAt(x), yh = hullTopAt(x);
      if (ys === null || yh === null) continue;
      const y = ys + d;
      if (y > yh - 12 || !inBow(x, y)) continue;
      pts.push([x, y]);
    }
  }
  for (const d of HULL_ROWS) {
    for (let x = 128; x <= 975; x += 10) {
      const yh = hullTopAt(x) ?? 200, yb = bellyAt(x);
      if (yb === null) continue;
      const y = yh + d;
      if (y > yb - 12 || !inBow(x, y)) continue;
      pts.push([x, y]);
    }
  }
  return pts.map(([x, y]) => `M${(x - 1).toFixed(1)} ${(y - 1).toFixed(1)}h2v2h-2z`).join("");
}

/* ---------- 二、三维截面 ---------- */

/** 船长（世界单位）：相机 fov 32、距离 13 时约占视口宽度的一半 */
export const SHIP_LEN = 6.2;
const S = SHIP_LEN / (SVG_NOSE_X - SVG_STERN_X);
const SVG_MID_X = (SVG_NOSE_X + SVG_STERN_X) / 2;
const SVG_AXIS_Y = 272;
export const wx = (x: number) => (x - SVG_MID_X) * S;
export const wy = (y: number) => (SVG_AXIS_Y - y) * S;
export const wl = (len: number) => len * S;

/** 一个站位的截面：轴向位置 x、竖直中心 c、半高 h、半宽 w（世界坐标） */
export type Ring = { x: number; c: number; h: number; w: number; p?: number };

/** 截面纵横比：船头近圆（1.3），越往船尾越扁（2.1） */
function aspectAt(svgX: number) {
  const u = Math.min(1, Math.max(0, (svgX - SVG_STERN_X) / (SVG_NOSE_X - SVG_STERN_X)));
  return 1.3 + 0.8 * Math.pow(1 - u, 1.5);
}

/** 侧视轮廓在 svgX 处的上缘 / 下缘（SVG y） */
function sectionAt(svgX: number): { top: number; bottom: number } | null {
  if (svgX < SVG_STERN_X || svgX > SVG_NOSE_X) return null;
  if (svgX <= 886) {
    const top = hullTopAt(Math.max(svgX, SVG_STERN_X + 0.01)), bottom = bellyAt(Math.max(svgX, SVG_STERN_X + 0.01));
    if (top === null || bottom === null) return null;
    return { top, bottom };
  }
  const top = interp(BOW_UPPER, svgX, 0), bottom = interp(BOW_LOWER, svgX, 0);
  if (top === null || bottom === null) return { top: 264, bottom: 264 };
  return { top, bottom };
}

export function hullRing(svgX: number): Ring | null {
  const s = sectionAt(svgX);
  if (!s) return null;
  const h = ((s.bottom - s.top) / 2) * S;
  return { x: wx(svgX), c: wy((s.top + s.bottom) / 2), h, w: h * aspectAt(svgX) };
}

/** 船体全部站位：船尾一个短而圆的尾盖，主体在船头处加密采样，最后一站收成一点 */
export function hullRings(n = 80): Ring[] {
  const rings: Ring[] = [];
  const stern = hullRing(SVG_STERN_X + 0.01)!;
  const capLen = wl(16);
  for (const s of [1, 0.92, 0.78, 0.55, 0.28]) {
    const f = Math.sqrt(1 - s * s);
    rings.push({ x: stern.x - capLen * s, c: stern.c, h: stern.h * Math.max(f, 0.02), w: stern.w * Math.max(f, 0.02) });
  }
  for (let i = 0; i <= n; i++) {
    const t = i / n;
    const svgX = SVG_STERN_X + (SVG_NOSE_X - SVG_STERN_X) * Math.sin((t * Math.PI) / 2);
    const r = hullRing(Math.min(svgX, SVG_NOSE_X));
    if (r) rings.push(r);
  }
  const last = rings[rings.length - 1];
  last.h = 0;
  last.w = 0;
  last.x = wx(SVG_NOSE_X);
  last.c = wy(264);
  return rings;
}

/** 上层建筑站位：半个超椭圆管（p=0.65，肩线圆润、顶面较平），肩线正好贴在船体表面 */
export function superRing(svgX: number): (Ring & { base: number }) | null {
  const hull = hullRing(svgX);
  const ys = sweepAt(svgX);
  if (!hull || ys === null) return null;
  const W = hull.w * 0.5;
  const base = hull.c + hull.h * Math.sqrt(1 - (W / hull.w) ** 2) - 0.012;
  const top = wy(ys);
  // 两端从 0 升起，避免出现直立的端面
  const inS = smooth((svgX - 170) / 70), inB = smooth((886 - svgX) / 46);
  const taper = inS * inB;
  const H = Math.max(0, top - base) * taper;
  return { x: hull.x, c: base, base, h: H, w: W * (0.35 + 0.65 * taper), p: 0.65 };
}
export function superRings(n = 64): Ring[] {
  const out: Ring[] = [];
  for (let i = 0; i <= n; i++) {
    const r = superRing(170 + (886 - 170) * (i / n));
    if (r) out.push(r);
  }
  return out;
}

function smooth(t: number) {
  const x = Math.min(1, Math.max(0, t));
  return x * x * (3 - 2 * x);
}

/** 截面上的一点：θ 从顶部量起，超椭圆指数 p（1 = 椭圆） */
export function ringPoint(r: Ring, theta: number): V3 {
  const p = r.p ?? 1;
  const ct = Math.cos(theta), st = Math.sin(theta);
  const y = r.c + r.h * Math.sign(ct) * Math.pow(Math.abs(ct), p);
  const z = r.w * Math.sign(st) * Math.pow(Math.abs(st), p);
  return [r.x, y, z];
}

export type Surface = { positions: Float32Array; normals: Float32Array; indices: Uint32Array; uvs?: Float32Array };

/**
 * 把一串截面连成曲面。thetaFrom → thetaTo 分 m 段；法线用有限差分的切线叉乘得到，
 * 退化处（收成一点的船头 / 船尾）退回轴向。
 */
export function buildTube(rings: Ring[], m: number, thetaFrom: number, thetaTo: number): Surface {
  const n = rings.length;
  const positions = new Float32Array(n * (m + 1) * 3);
  const normals = new Float32Array(n * (m + 1) * 3);
  const uvs = new Float32Array(n * (m + 1) * 2); // u 沿船轴（船尾 0 → 船头 1），v 绕周向
  const pt = (i: number, j: number): V3 => ringPoint(rings[Math.min(n - 1, Math.max(0, i))], thetaFrom + ((thetaTo - thetaFrom) * j) / m);
  for (let i = 0; i < n; i++) {
    for (let j = 0; j <= m; j++) {
      const P = pt(i, j);
      const k = (i * (m + 1) + j) * 3;
      uvs[(i * (m + 1) + j) * 2] = i / (n - 1);
      uvs[(i * (m + 1) + j) * 2 + 1] = j / m;
      positions[k] = P[0];
      positions[k + 1] = P[1];
      positions[k + 2] = P[2];
      // 切线：沿轴向 (i±1) 与沿周向 (j±1)
      const A = pt(i + 1, j), B = pt(i - 1, j);
      const C = pt(i, Math.min(m, j + 1)), D = pt(i, Math.max(0, j - 1));
      const tx: V3 = [A[0] - B[0], A[1] - B[1], A[2] - B[2]];
      const tt: V3 = [C[0] - D[0], C[1] - D[1], C[2] - D[2]];
      let nx = tt[1] * tx[2] - tt[2] * tx[1];
      let ny = tt[2] * tx[0] - tt[0] * tx[2];
      let nz = tt[0] * tx[1] - tt[1] * tx[0];
      let len = Math.hypot(nx, ny, nz);
      if (len < 1e-8) {
        // 退化：船头朝 +x，船尾朝 -x
        nx = i > n / 2 ? 1 : -1;
        ny = 0;
        nz = 0;
        len = 1;
      }
      normals[k] = nx / len;
      normals[k + 1] = ny / len;
      normals[k + 2] = nz / len;
    }
  }
  const indices = new Uint32Array((n - 1) * m * 6);
  let q = 0;
  for (let i = 0; i < n - 1; i++) {
    for (let j = 0; j < m; j++) {
      const a = i * (m + 1) + j, b = a + 1, c = a + (m + 1), d = c + 1;
      // 逆时针（从外面看）：法线朝外
      indices[q++] = a;
      indices[q++] = b;
      indices[q++] = c;
      indices[q++] = b;
      indices[q++] = d;
      indices[q++] = c;
    }
  }
  return { positions, normals, indices, uvs };
}

/** 船体：整圈；上层建筑：只有上半圈 */
export const buildHull = () => buildTube(hullRings(), 64, 0, Math.PI * 2);
export const buildSuperstructure = () => buildTube(superRings(), 28, -Math.PI / 2, Math.PI / 2);

/* ---------- 舷窗 ---------- */

export type WindowSpot = { p: V3; n: V3; twinkle: boolean };
const WINDOW_PITCH_SVG = 13;
const TWINKLE_RATIO = 0.08;

/** 截面外法线（椭圆 / 超椭圆的解析法线，忽略轴向倾斜——舷窗只需大致朝外） */
function ringNormal(r: Ring, theta: number): V3 {
  const p = r.p ?? 1;
  const ct = Math.cos(theta), st = Math.sin(theta);
  const ny = (Math.sign(ct) * Math.pow(Math.abs(ct), 2 - p)) / Math.max(r.h, 1e-4);
  const nz = (Math.sign(st) * Math.pow(Math.abs(st), 2 - p)) / Math.max(r.w, 1e-4);
  const len = Math.hypot(ny, nz) || 1;
  return [0, ny / len, nz / len];
}

const deg = (d: number) => (d * Math.PI) / 180;
const HULL_WINDOW_ROWS = [62, 79, 96, 113].map(deg);
/** 上层建筑：越高的排要求越高的建筑高度才开始，于是越靠船尾越短 */
const SUPER_WINDOW_ROWS: { theta: number; minH: number }[] = [
  { theta: deg(46), minH: 0.34 },
  { theta: deg(64), minH: 0.22 },
  { theta: deg(82), minH: 0.11 },
];

export function buildWindows(): WindowSpot[] {
  const rng = mulberry32(0x41784f4d); // "AxOM"
  const out: WindowSpot[] = [];
  const push = (r: Ring, theta: number) => {
    for (const side of [1, -1]) {
      const th = theta * side;
      const p = ringPoint(r, th), n = ringNormal(r, th);
      out.push({ p: [p[0] + n[0] * 0.006, p[1] + n[1] * 0.006, p[2] + n[2] * 0.006], n, twinkle: rng() < TWINKLE_RATIO });
    }
  };
  for (const theta of HULL_WINDOW_ROWS) {
    for (let x = 126; x <= 900; x += WINDOW_PITCH_SVG) {
      const r = hullRing(x);
      if (!r || r.h < 0.16) continue;
      push(r, theta);
    }
  }
  for (const row of SUPER_WINDOW_ROWS) {
    for (let x = 180; x <= 880; x += WINDOW_PITCH_SVG) {
      const r = superRing(x);
      if (!r || r.h < row.minH) continue;
      push(r, row.theta);
    }
  }
  return out;
}

/* ---------- 船头全景舷窗带 ---------- */

/** 绕船头一圈的全景舰桥窗带（±0.14 高，两侧各一条网格，在鼻尖汇成一点），贴在船体表面外 8mm */
export function buildBowBand(): Surface {
  const stations: Ring[] = [];
  const n = 26;
  for (let i = 0; i <= n; i++) {
    const t = i / n;
    const svgX = 892 + (SVG_NOSE_X - 892) * Math.sin((t * Math.PI) / 2);
    const r = hullRing(Math.min(svgX, SVG_NOSE_X));
    if (r) stations.push(r);
  }
  const last = stations[stations.length - 1];
  last.h = 0;
  last.w = 0;
  last.x = wx(SVG_NOSE_X);
  const halfBand = 0.14;
  const rows = [-1, -0.5, 0, 0.5, 1];
  const verts: number[] = [], norms: number[] = [], idx: number[] = [];
  for (const side of [1, -1]) {
    const base = verts.length / 3;
    stations.forEach((r) => {
      for (const v of rows) {
        const yAbs = v * Math.min(halfBand, r.h * 0.96);
        const q = r.h > 1e-4 ? Math.sqrt(Math.max(0, 1 - (yAbs / r.h) ** 2)) : 0;
        const z = side * r.w * q;
        let ny = r.h > 1e-4 ? yAbs / (r.h * r.h) : 0, nz = r.w > 1e-4 ? z / (r.w * r.w) : 0;
        let nx = 0;
        let len = Math.hypot(ny, nz);
        if (len < 1e-6) {
          nx = 1;
          ny = 0;
          nz = 0;
          len = 1;
        }
        nx /= len;
        ny /= len;
        nz /= len;
        // 越靠鼻尖越朝前：把法线向 +x 掺一点
        const fwd = Math.max(0, 1 - r.h / 0.3);
        const mx = nx + fwd * 1.4, my = ny, mz = nz;
        const ml = Math.hypot(mx, my, mz);
        verts.push(r.x + (mx / ml) * 0.008, r.c + yAbs + (my / ml) * 0.008, z + (mz / ml) * 0.008);
        norms.push(mx / ml, my / ml, mz / ml);
      }
    });
    const R = rows.length;
    for (let i = 0; i < stations.length - 1; i++) {
      for (let j = 0; j < R - 1; j++) {
        const a = base + i * R + j, b = a + 1, c = a + R, d = c + 1;
        if (side > 0) idx.push(a, c, b, b, c, d);
        else idx.push(a, b, c, b, d, c);
      }
    }
  }
  return { positions: new Float32Array(verts), normals: new Float32Array(norms), indices: new Uint32Array(idx) };
}

/* ---------- 推进翼、引擎、穹顶 ---------- */

/** 推进翼轮廓（x 沿船轴，s 为向外的展长）：从船尾侧面探出、宽而扁、向后掠 */
export const WING_OUTLINE: Pt[] = (() => {
  const xs = wx(SVG_STERN_X);
  return [
    [xs + 1.35, 0],
    [xs - 0.2, 1.38],
    [xs - 0.32, 1.46],
    [xs - 0.5, 1.4],
    [xs - 0.3, 1.1],
    [xs + 0.28, 0],
  ];
})();
export const WING_DIHEDRAL = 0.18; // rad，微微上翘
export const WING_THICKNESS = 0.08;

export const STERN = (() => {
  const r = hullRing(SVG_STERN_X + 0.01)!;
  return { x: r.x, c: r.c, h: r.h, w: r.w };
})();
/** 两枚引擎：船尾中心两侧 */
export const ENGINES: V3[] = [
  [STERN.x - 0.02, STERN.c - 0.02, STERN.w * 0.52],
  [STERN.x - 0.02, STERN.c - 0.02, -STERN.w * 0.52],
];
export const ENGINE_RADIUS = STERN.h * 0.46;

export const DOME: V3 = [wx(SVG_DOME[0]), wy(SVG_DOME[1]) - 0.03, 0];
export const DOME_RADIUS = wl(15);
