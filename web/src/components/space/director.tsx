"use client";
import { useEffect } from "react";
import { useFrame, useThree } from "@react-three/fiber";
import { MathUtils, Matrix4, Plane, Raycaster, Vector2, Vector3 } from "three";
import { BOARD_MS, sceneInput } from "./sceneBus";

/**
 * 导演：把 DOM 输入（sceneBus.sceneInput 里的目标值）翻译成每帧的包络 fx，其余组件只读 fx。
 * - 输入只改目标，所有运动在这里经过 MathUtils.damp；reduced-motion 时 damp 退化为"直接落定"；
 * - 优先级 -1：在所有 useFrame 之前跑，于是同一帧里每个人读到的是同一组值；
 * - 监听挂在品牌页根节点 / window 上：光标位置、拖拽环绕（只在内容列之外、≥1024px）、滚轮推拉（只在场景区域，页面可滚动时不拦截）、
 *   空闲计时；任何监听都不 preventDefault（滚轮推拉除外，且只在场景区域内）。
 */
export const IDLE_AFTER_MS = 12000;
const DOLLY_IDLE_MS = 3000;
const KEY_PULSE_MS = 400;
const FAIL_FLASH_MS = 200;
const FAIL_RESET_MS = 1400;

export const fx = {
  /** 光标目标（-1..1；离开窗口 / 空闲吸引时回 0） */
  px: 0,
  py: 0,
  /** 船的注意力：朝光标的偏航 / 俯仰（弧度） */
  attentionYaw: 0,
  attentionPitch: 0,
  /** 拖拽环绕（弧度），松手回正 */
  orbitAz: 0,
  orbitPolar: 0,
  /** 滚轮推拉：相机 z 偏移 */
  dolly: 0,
  /** 聚焦邮箱 / 姓名：相机推向舰首 */
  focusPush: 0,
  /** 聚焦密码：船"在听"——舷窗暗 20%、舰首窗带亮 */
  listen: 0,
  /** 按键脉冲包络 0..1 与已处理的序号 */
  keyPulse: 0,
  keySeq: 0,
  /** 提交：引擎喷发（λ 10） */
  flare: 0,
  /** 星场 / 星尘的流动强度 0..1（登舰掠航时 = 速度） */
  warp: 0,
  thrust: 0,
  /**
   * 登舰四拍（秒，从 success 起算；-1 = 没在登舰）：
   *  0–0.35 确认：ignite 引擎点火（120ms 攻击）、surge 全船舷窗 +60% 后回落、bandFlash 窗带两次 60ms 闪、runwayX 沿船身跑的引导灯（模型单位 x）、anticip 镜头后拉；
   *  0.35–1.7 掠航：pathU 样条进度 0..1、speed 速度包络 0..1（FOV / 星流 / 速度线）、pathS 拍内时间 0..1；
   *  1.7–2.35 入口：entrance 0..1（ease-in）——镜头直入舰首窗，窗带 ×3、泛光扩张、光束与横向眩光；
   *  2.35–2.6 白场（CSS）。
   */
  boardT: -1,
  boarding: false,
  ignite: 0,
  surge: 0,
  bandFlash: 0,
  runwayX: NaN,
  anticip: 0,
  pathU: 0,
  pathS: 0,
  speed: 0,
  entrance: 0,
  /** 兼容：舷窗 / 窗带的整体提亮 0..1 */
  board: 0,
  /** 母舰 attention 组的世界矩阵（Ship 每帧写）：镜头样条在船的局部空间里 */
  shipMatrix: new Matrix4(),
  /** 场景就绪（模型到位、着色器编译完）的时钟时刻；镜头推进从这里起算。null = 还没就绪 */
  introAt: null as number | null,
  modelReady: false,
  /** 登录失败：冷轮廓光切成冷红 */
  fail: 0,
  /** 空闲吸引模式 0..1 与它自己的时钟（秒） */
  idle: 0,
  idleT: 0,
  /** 光标射线与船所在平面（z=0）的交点（世界坐标，阻尼过） */
  cursorWorld: new Vector3(0, 0, 0),
  cursorRayOrigin: new Vector3(0, 0, 12.5),
  cursorRayDir: new Vector3(0, 0, -1),
  /** 舰首窗带的世界坐标（Ship 每帧写，景深读） */
  bowWorld: new Vector3(3, -0.5, 0),
  reduced: false,
};

const instant = (_x: number, y: number) => y;
export const damp = (x: number, y: number, lambda: number, dt: number) => (fx.reduced ? y : MathUtils.damp(x, y, lambda, dt));

const AZ_MAX = (20 * Math.PI) / 180;
const POLAR_MAX = (8 * Math.PI) / 180;
const PLANE = new Plane(new Vector3(0, 0, 1), 0);
const ndc = new Vector2();
const hit = new Vector3();

/**
 * 场景卸载 / 重新挂载时把所有瞬态包络归零：fx 与 sceneInput 是模块级对象，退出登录再回到登录页时
 * 若不重置，镜头会仍停在登舰终点、登舰进度仍是 1。
 */
export function resetScene() {
  fx.px = fx.py = 0;
  fx.attentionYaw = fx.attentionPitch = 0;
  fx.orbitAz = fx.orbitPolar = 0;
  fx.dolly = fx.focusPush = fx.listen = 0;
  fx.keyPulse = 0;
  fx.flare = fx.board = fx.warp = fx.thrust = 0;
  fx.introAt = null;
  fx.fail = 0;
  fx.idle = fx.idleT = 0;
  const s = sceneInput;
  s.pointer.seen = false;
  s.lastInputAt = performance.now();
  s.focus = null;
  s.phase = "idle";
  s.phaseAt = 0;
  s.failAt = -1e9;
  s.keyAt = -1e9;
  s.drag.active = false;
  s.drag.dx = s.drag.dy = 0;
  s.dolly.target = 0;
  s.dolly.at = -1e9;
  document.querySelector("[data-brand-root]")?.removeAttribute("data-boarding");
}

export function SceneDirector({ reduced }: { reduced: boolean }) {
  const gl = useThree((s) => s.gl);
  const invalidate = useThree((s) => s.invalidate);

  useEffect(() => {
    fx.reduced = reduced;
    const s = sceneInput;
    const canvas = gl.domElement;
    const root = canvas.closest<HTMLElement>("[data-brand-root]") ?? document.body;
    s.invalidate = reduced ? () => invalidate() : null;
    s.live = true;
    s.reduced = reduced;
    s.lastInputAt = performance.now();
    const inContent = (t: EventTarget | null) => t instanceof Element && !!t.closest("[data-brand-content]");
    const wide = () => window.innerWidth >= 1024;
    const touch = () => {
      s.lastInputAt = performance.now();
      if (reduced) invalidate();
    };
    const onMove = (e: PointerEvent) => {
      const r = canvas.getBoundingClientRect();
      if (r.width > 0 && r.height > 0) {
        s.pointer.x = MathUtils.clamp(((e.clientX - r.left) / r.width) * 2 - 1, -1, 1);
        s.pointer.y = MathUtils.clamp(-((e.clientY - r.top) / r.height) * 2 + 1, -1, 1);
        s.pointer.seen = true;
      }
      if (s.drag.active) {
        s.drag.dx = e.clientX - s.drag.x0;
        s.drag.dy = e.clientY - s.drag.y0;
      }
      touch();
    };
    const onDown = (e: PointerEvent) => {
      if (e.button !== 0 || e.pointerType === "touch" || !wide() || inContent(e.target) || s.phase === "success") return;
      s.drag.active = true;
      s.drag.x0 = e.clientX;
      s.drag.y0 = e.clientY;
      s.drag.dx = 0;
      s.drag.dy = 0;
      root.setAttribute("data-orbiting", "");
      touch();
    };
    const endDrag = () => {
      if (!s.drag.active) return;
      s.drag.active = false;
      s.drag.dx = 0;
      s.drag.dy = 0;
      root.removeAttribute("data-orbiting");
      touch();
    };
    const onWheel = (e: WheelEvent) => {
      // 页面本身可滚动、窄屏、或指针在表单列上：一律不拦截
      if (!wide() || inContent(e.target) || s.phase === "success" || document.documentElement.scrollHeight > window.innerHeight + 1) return;
      e.preventDefault();
      s.dolly.target = MathUtils.clamp(s.dolly.target + e.deltaY * 0.004, -1.5, 1.5);
      s.dolly.at = performance.now();
      touch();
    };
    const onLeave = () => {
      s.pointer.seen = false;
      endDrag();
    };
    window.addEventListener("pointermove", onMove, { passive: true });
    root.addEventListener("pointerdown", onDown);
    window.addEventListener("pointerup", endDrag);
    window.addEventListener("pointercancel", endDrag);
    window.addEventListener("blur", onLeave);
    document.documentElement.addEventListener("mouseleave", onLeave);
    root.addEventListener("wheel", onWheel, { passive: false });
    window.addEventListener("keydown", touch);
    return () => {
      window.removeEventListener("pointermove", onMove);
      root.removeEventListener("pointerdown", onDown);
      window.removeEventListener("pointerup", endDrag);
      window.removeEventListener("pointercancel", endDrag);
      window.removeEventListener("blur", onLeave);
      document.documentElement.removeEventListener("mouseleave", onLeave);
      root.removeEventListener("wheel", onWheel);
      window.removeEventListener("keydown", touch);
      root.removeAttribute("data-orbiting");
      s.invalidate = null;
      resetScene();
      s.live = false;
      s.drag.active = false;
    };
  }, [gl, invalidate, reduced]);

  useFrame(({ camera }, rawDt) => {
    const dt = Math.min(rawDt, 1 / 30);
    const s = sceneInput;
    const now = performance.now();
    const d = fx.reduced ? instant : MathUtils.damp;

    // 失败状态 1.4s 后自动回到 idle（页面只发一次 failure）
    if (s.phase === "failure" && now - s.failAt > FAIL_RESET_MS) s.phase = "idle";

    // 空闲吸引：12s 无输入且没有拖拽 / 提交；进入很慢（λ 0.35），任何输入 λ 2 退出
    const idleWanted = !fx.reduced && !s.drag.active && s.phase === "idle" && now - s.lastInputAt > IDLE_AFTER_MS;
    fx.idle = d(fx.idle, idleWanted ? 1 : 0, idleWanted ? 0.35 : 2, dt);
    if (fx.idle > 0.001) fx.idleT += dt * fx.idle;
    else fx.idleT = 0;

    // 光标目标：看见了就用，离开窗口 / 空闲时回中
    const seen = s.pointer.seen && !fx.reduced;
    fx.px = seen ? s.pointer.x * (1 - fx.idle) : 0;
    fx.py = seen ? s.pointer.y * (1 - fx.idle) : 0;

    // 注意力：±3° / ±1.5°，λ 1.5（大质量物体的反应速度）
    fx.attentionYaw = d(fx.attentionYaw, fx.px * ((3 * Math.PI) / 180), 1.5, dt);
    fx.attentionPitch = d(fx.attentionPitch, -fx.py * ((1.5 * Math.PI) / 180), 1.5, dt);

    // 拖拽环绕：像素 → 角度，限制 ±20° / ±8°；拖动时 λ 8 跟手，松手 λ 2.5 回正
    const azT = s.drag.active ? MathUtils.clamp(s.drag.dx * 0.004, -AZ_MAX, AZ_MAX) : 0;
    const polT = s.drag.active ? MathUtils.clamp(s.drag.dy * 0.003, -POLAR_MAX, POLAR_MAX) : 0;
    fx.orbitAz = d(fx.orbitAz, azT, s.drag.active ? 8 : 2.5, dt);
    fx.orbitPolar = d(fx.orbitPolar, polT, s.drag.active ? 8 : 2.5, dt);

    // 滚轮推拉：3s 没再滚就回到导演机位
    if (now - s.dolly.at > DOLLY_IDLE_MS) s.dolly.target = 0;
    fx.dolly = d(fx.dolly, s.dolly.target, 3, dt);

    // 表单焦点
    const push = s.focus === "email" || s.focus === "other";
    fx.focusPush = d(fx.focusPush, push ? 1 : 0, 2.5, dt);
    fx.listen = d(fx.listen, s.focus === "password" ? 1 : 0, 4, dt);

    // 按键脉冲：60ms 起、340ms 落
    const age = now - s.keyAt;
    fx.keyPulse = fx.reduced || age >= KEY_PULSE_MS ? 0 : age < 60 ? age / 60 : 1 - (age - 60) / (KEY_PULSE_MS - 60);
    fx.keySeq = s.keySeq;

    // 提交：引擎 λ 10（300ms）喷发；失败 / 回到 idle 时退回
    const go = s.phase === "submitting" || s.phase === "success";
    fx.flare = d(fx.flare, go ? 1 : 0, 10, dt);
    fx.thrust = 0;
    // 登舰四拍（时间驱动，不受输入影响；reduced 时不播）
    const bt = s.phase === "success" && !fx.reduced ? Math.min((now - s.phaseAt) / 1000, BOARD_MS / 1000) : -1;
    fx.boardT = bt;
    fx.boarding = bt >= 0;
    if (bt >= 0) {
      const ss = MathUtils.smoothstep;
      fx.ignite = ss(bt, 0, 0.12);
      fx.surge = bt < 0.08 ? bt / 0.08 : Math.max(0, 1 - (bt - 0.08) / 0.35);
      fx.bandFlash = (bt >= 0.06 && bt < 0.12) || (bt >= 0.2 && bt < 0.26) ? 1 : 0;
      fx.runwayX = bt < 1.7 ? -5.2 + ((bt % 0.45) / 0.45) * 10 : NaN;
      fx.anticip = ss(bt, 0, 0.35);
      const sP = MathUtils.clamp((bt - 0.35) / 1.35, 0, 1);
      fx.pathS = sP;
      fx.pathU = rampU(sP);
      fx.speed = bt >= 0.35 && bt < 1.7 ? rampV(sP) : 0;
      const e3 = MathUtils.clamp((bt - 1.7) / 0.65, 0, 1);
      fx.entrance = e3 * e3;
      fx.board = Math.max(0.6 * fx.surge, 0.35 * fx.pathU, fx.entrance);
      fx.warp = fx.speed;
      // 输入在登舰期间全部忽略：注意力 / 环绕 / 推拉 / 焦点 / 空闲都归零
      fx.idle = 0;
      fx.px = 0;
      fx.py = 0;
      fx.attentionYaw = d(fx.attentionYaw, 0, 3, dt);
      fx.attentionPitch = d(fx.attentionPitch, 0, 3, dt);
      fx.orbitAz = d(fx.orbitAz, 0, 3, dt);
      fx.orbitPolar = d(fx.orbitPolar, 0, 3, dt);
      fx.dolly = d(fx.dolly, 0, 3, dt);
      fx.focusPush = d(fx.focusPush, 0, 3, dt);
      fx.listen = d(fx.listen, 0, 3, dt);
      fx.keyPulse = 0;
      s.drag.active = false;
    } else {
      fx.ignite = 0;
      fx.surge = 0;
      fx.bandFlash = 0;
      fx.runwayX = NaN;
      fx.anticip = 0;
      fx.pathS = 0;
      fx.pathU = 0;
      fx.speed = 0;
      fx.entrance = 0;
      fx.board = 0;
      fx.warp = 0;
    }

    // 失败：200ms 内切成冷红（λ 30），之后 λ 6 回来
    const failAge = now - s.failAt;
    const failWanted = failAge >= 0 && failAge < FAIL_FLASH_MS;
    fx.fail = d(fx.fail, failWanted ? 1 : 0, failWanted ? 30 : 6, dt);

    // 光标射线与 z=0 平面的交点（世界坐标）
    ndc.set(fx.px, fx.py);
    ray.setFromCamera(ndc, camera);
    fx.cursorRayOrigin.copy(ray.ray.origin);
    fx.cursorRayDir.copy(ray.ray.direction);
    if (ray.ray.intersectPlane(PLANE, hit)) {
      const k = fx.reduced ? 1 : 1 - Math.exp(-4 * dt);
      fx.cursorWorld.lerp(hit, k);
    }
  }, -1);
  return null;
}

const ray = new Raycaster();

/**
 * 掠航的速度剖面：前 25% smoothstep 加速、中段全速、后 25% smoothstep 减速；
 * rampV 是归一化速度（峰值 1），rampU 是它的积分（0..1 的路径进度），用 400 段数值积分做成查表。
 */
const RAMP_N = 400;
const rampTable = (() => {
  const v = new Float32Array(RAMP_N + 1), u = new Float32Array(RAMP_N + 1);
  let acc = 0;
  for (let i = 0; i <= RAMP_N; i++) {
    const s = i / RAMP_N;
    v[i] = MathUtils.smoothstep(s, 0, 0.25) * (1 - MathUtils.smoothstep(s, 0.75, 1));
    if (i > 0) acc += ((v[i] + v[i - 1]) / 2) * (1 / RAMP_N);
    u[i] = acc;
  }
  for (let i = 0; i <= RAMP_N; i++) u[i] /= acc;
  return { v, u };
})();
function rampV(s: number) {
  const i = Math.min(RAMP_N, Math.max(0, Math.round(s * RAMP_N)));
  return rampTable.v[i];
}
function rampU(s: number) {
  const f = MathUtils.clamp(s, 0, 1) * RAMP_N, i = Math.floor(f), k = f - i;
  return i >= RAMP_N ? 1 : rampTable.u[i] * (1 - k) + rampTable.u[i + 1] * k;
}
