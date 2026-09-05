"use client";
import { useEffect, useLayoutEffect, useMemo, useRef } from "react";
import { useFrame } from "@react-three/fiber";
import { Color, InstancedMesh, MeshBasicMaterial, Object3D, PlaneGeometry, Vector3 } from "three";
import { fx } from "../director";
import { buildWindows, DOME, mulberry32 } from "./hullProfile";

/**
 * 舷窗：InstancedMesh 小发光片，颜色 > 1（HDR，暖白 1.85/1.76/1.55 × 0.55–1.15，每盏不同亮度，看起来有人住），Bloom 只抓它们。
 * 每帧对全部实例重算一次 instanceColor（约 730 盏，开销可忽略），叠加四种调制：
 * - 约 8% 由固定种子选出，各自以 3–7s 的周期慢慢明暗；
 * - 光标邻近：与光标在船平面上的交点（船体局部坐标）距离 < 0.7 的舷窗最多亮 55%，各自阻尼；
 * - 按键脉冲：每次敲键从舰桥附近随机挑 3–5 盏，+40% 持续 400ms；
 * - 聚焦密码：整体暗 20%（fx.listen）。
 * reduced-motion：常亮，只有聚焦密码的整体变暗直接落定。
 */
const BASE = new Color(1.85, 1.76, 1.55);
const PROX_RADIUS = 0.7;
const PROX_GAIN = 0.55;

export function Windows({ reduced }: { reduced: boolean }) {
  const spots = useMemo(() => buildWindows(), []);
  const ref = useRef<InstancedMesh>(null!);
  const geo = useMemo(() => new PlaneGeometry(0.026, 0.015), []);
  const mat = useMemo(() => new MeshBasicMaterial({ color: 0xffffff, toneMapped: false }), []);
  useEffect(
    () => () => {
      geo.dispose();
      mat.dispose();
    },
    [geo, mat],
  );
  const data = useMemo(() => {
    const rng = mulberry32(0x57494e44);
    const trng = mulberry32(0x5354415a);
    const n = spots.length;
    const base = new Float32Array(n), period = new Float32Array(n), phase = new Float32Array(n), tw = new Uint8Array(n);
    const px = new Float32Array(n), py = new Float32Array(n);
    const bridge: number[] = [];
    spots.forEach((s, i) => {
      base[i] = 0.55 + rng() * 0.6;
      px[i] = s.p[0];
      py[i] = s.p[1];
      if (s.twinkle) {
        tw[i] = 1;
        period[i] = 3 + trng() * 4;
        phase[i] = trng() * Math.PI * 2;
      }
      // 舰桥附近：穹顶前后 1.1 单位内、在上层建筑（y 高于船轴）
      if (Math.abs(s.p[0] - DOME[0]) < 1.1 && s.p[1] > 0.05) bridge.push(i);
    });
    return { base, period, phase, tw, px, py, bridge };
  }, [spots]);
  useLayoutEffect(() => {
    const mesh = ref.current;
    const o = new Object3D(), c = new Color(), target = new Vector3();
    spots.forEach((s, i) => {
      o.position.set(s.p[0], s.p[1], s.p[2]);
      target.set(s.p[0] + s.n[0], s.p[1] + s.n[1], s.p[2] + s.n[2]);
      o.lookAt(target);
      o.updateMatrix();
      mesh.setMatrixAt(i, o.matrix);
      mesh.setColorAt(i, c.copy(BASE).multiplyScalar(data.base[i]));
    });
    mesh.instanceMatrix.needsUpdate = true;
    if (mesh.instanceColor) mesh.instanceColor.needsUpdate = true;
  }, [spots, data]);

  const state = useRef({ seq: 0, pulses: [] as { i: number; at: number }[], rng: mulberry32(0x4b455953), local: new Vector3(), c: new Color(), frame: 0, prox: new Float32Array(spots.length) });
  useFrame(({ clock }, rawDt) => {
    const st = state.current;
    const mesh = ref.current;
    const dt = Math.min(rawDt, 1 / 30);
    const t = clock.elapsedTime;
    const now = performance.now();
    // 新的按键：从舰桥附近挑 3–5 盏
    if (fx.keySeq !== st.seq) {
      st.seq = fx.keySeq;
      const n = 3 + Math.floor(st.rng() * 3);
      for (let k = 0; k < n && data.bridge.length; k++) st.pulses.push({ i: data.bridge[Math.floor(st.rng() * data.bridge.length)], at: now });
    }
    if (st.pulses.length) st.pulses = st.pulses.filter((p) => now - p.at < 400);
    // reduced：只在需要时（listen 变化）重算；这里每帧都算，但 frameloop=demand 只在事件后跑一帧
    st.frame++;
    if (!reduced && st.frame % 2) return; // 舷窗 30Hz 更新就够
    const dim = 1 - 0.2 * fx.listen;
    st.local.copy(fx.cursorWorld);
    mesh.worldToLocal(st.local);
    const cx = st.local.x, cy = st.local.y;
    const doProx = !reduced && Math.abs(st.local.z) < 6;
    const { base, period, phase, tw, px, py } = data;
    const prox = st.prox;
    const c = st.c;
    const kProx = 1 - Math.exp(-6 * dt * 2);
    for (let i = 0; i < base.length; i++) {
      let k = base[i];
      if (tw[i] && !reduced) k *= 0.12 + 0.88 * (0.5 + 0.5 * Math.sin((t / period[i]) * Math.PI * 2 + phase[i]));
      let pt = 0;
      if (doProx) {
        const dx = px[i] - cx, dy = py[i] - cy;
        const dd = Math.sqrt(dx * dx + dy * dy);
        if (dd < PROX_RADIUS) {
          const u = 1 - dd / PROX_RADIUS;
          pt = PROX_GAIN * u * u;
        }
      }
      prox[i] += (pt - prox[i]) * kProx;
      k = k * dim + prox[i];
      mesh.setColorAt(i, c.copy(BASE).multiplyScalar(k));
    }
    for (const p of st.pulses) {
      const age = now - p.at;
      const env = age < 60 ? age / 60 : 1 - (age - 60) / 340;
      mesh.getColorAt(p.i, c);
      mesh.setColorAt(p.i, c.multiplyScalar(1 + 0.4 * env));
    }
    if (mesh.instanceColor) mesh.instanceColor.needsUpdate = true;
  });
  return <instancedMesh ref={ref} args={[geo, mat, spots.length]} frustumCulled={false} />;
}
