"use client";
import { useRef } from "react";
import { useFrame } from "@react-three/fiber";
import { CatmullRomCurve3, MathUtils, Matrix4, PerspectiveCamera, Vector3 } from "three";
import { fx } from "./director";
import { SHIP_LEN, SVG_NOSE_X, wx } from "./ship/hullProfile";

/** 相机的最终距离（开场推进的终点）；Ship 的布局计算也用它 */
export const CAMERA_Z = 12.5;
export const CAMERA_Y = 1.15;
export const CAMERA_FOV = 32;
export const LOOK_AT: [number, number, number] = [0.35, -0.25, 0];
const DOLLY_FROM = 15.5;
const DOLLY_SECONDS = 2.5;
const DOLLY_MIN = 11;
const DOLLY_MAX = 14;

/** 船头（船局部单位，SHIP_LEN=6.2）：舰首窗带中心与鼻尖 */
const BOW = new Vector3(wx(SVG_NOSE_X) - 0.3, 0.05, 0);
const NOSE_X = SHIP_LEN / 2 + 0.07;
/**
 * 掠航样条的中途点（船局部空间，+x 船头、+z 观众一侧）：
 * 贴着近侧船尾下方掠过引擎辉光 → 沿船舷（离船体表面约 0.55）→ 升起、向前绕到船头前方 → 正对舰首窗 2.2 单位处。
 * 船体半宽约 0.68、推进翼在 y≈0.16、z 到 1.7；P1 压在翼面之下。
 */
const PATH_MID = [new Vector3(-2.4, -0.6, 1.65), new Vector3(0.1, -0.22, 1.28), new Vector3(2.4, 0.38, 1.7), new Vector3(4.4, 0.18, 1.15)];
const PATH_END = new Vector3(NOSE_X + 2.2, 0.06, 0);
const LOOK_MID = [new Vector3(-1.2, -0.15, 0.35), new Vector3(1.4, 0.05, 0.25), new Vector3(3.0, 0.08, 0.05)];
/** 入口：从窗前 2.2 直入到鼻尖前 0.28（near 0.1） */
const ENTER_END = new Vector3(NOSE_X + 0.28, 0.05, 0);

/**
 * 镜头：场景就绪（fx.introAt）后 2.5s 从远处推进（easeOutCubic），之前停在远端（画布还没淡入）；之后所有目标叠加、再经阻尼：
 * - 鼠标视差 ±0.35 / ±0.2（λ 4）；滚轮推拉 z 11–14；聚焦邮箱向舰首推 0.8；空闲吸引的慢环绕；登录失败向后一顿 0.3。
 * 登舰（fx.boardT ≥ 0，全部时间驱动、忽略输入）：
 *  确认 0–0.35s：原机位后拉 0.25（anticip）；
 *  掠航 0.35–1.7s：以那一刻的机位为起点，在船的局部空间里沿 Catmull-Rom 样条飞（PATH_MID → PATH_END），进度 fx.pathU
 *   （前 25% 加速、中段全速、后 25% 减速），视线沿另一条样条从原视点滑到舰首窗；FOV 32 + 20·speed，收尾到 40；±2° 侧滚；
 *  入口 1.7–2.35s：从窗前 2.2 直入到鼻尖前 0.28（ease-in），视线锁定窗带，FOV 40；
 *  之后由 CSS 的暖白覆盖。
 * delta 夹取到 1/30；reduced-motion 直接落在终点、不做视差、不登舰。
 */
export function CameraRig({ reduced }: { reduced: boolean }) {
  const ref = useRef({
    pos: new Vector3(),
    look: new Vector3(),
    inv: new Matrix4(),
    lastLook: new Vector3(LOOK_AT[0], LOOK_AT[1], LOOK_AT[2]),
    path: null as CatmullRomCurve3 | null,
    lookPath: null as CatmullRomCurve3 | null,
  });
  useFrame(({ camera, clock }, rawDt) => {
    const st = ref.current;
    const dt = Math.min(rawDt, 1 / 30);
    const t = clock.elapsedTime;
    const cam = camera as PerspectiveCamera;
    const p = st.pos, look = st.look;
    let fov = CAMERA_FOV;
    let roll = 0;

    if (fx.boarding && fx.boardT >= 0.35) {
      // 掠航 / 入口：船局部空间
      if (!st.path) {
        st.inv.copy(fx.shipMatrix).invert();
        const p0 = camera.position.clone().applyMatrix4(st.inv);
        const l0 = st.lastLook.clone().applyMatrix4(st.inv);
        st.path = new CatmullRomCurve3([p0, ...PATH_MID, PATH_END], false, "centripetal");
        st.lookPath = new CatmullRomCurve3([l0, ...LOOK_MID, BOW.clone(), BOW.clone()], false, "centripetal");
      }
      if (fx.boardT < 1.7) {
        st.path.getPointAt(fx.pathU, p);
        st.lookPath!.getPointAt(fx.pathU, look);
        fov = CAMERA_FOV + 20 * fx.speed + 8 * MathUtils.smoothstep(fx.pathS, 0.75, 1);
        roll = MathUtils.degToRad(2) * Math.sin(Math.PI * 2 * fx.pathS);
      } else {
        p.lerpVectors(PATH_END, ENTER_END, fx.entrance);
        look.copy(BOW);
        fov = 40;
      }
      p.applyMatrix4(fx.shipMatrix);
      look.applyMatrix4(fx.shipMatrix);
      camera.position.copy(p);
      camera.lookAt(look);
      if (roll) camera.rotateZ(roll);
    } else {
      if (st.path) {
        st.path = null;
        st.lookPath = null;
      }
      const k = reduced ? 1 : fx.introAt === null ? 0 : Math.min(1, (t - fx.introAt) / DOLLY_SECONDS);
      const intro = DOLLY_FROM - (DOLLY_FROM - CAMERA_Z) * (1 - Math.pow(1 - k, 3));
      const idle = fx.idle, it = fx.idleT;
      const tx = fx.px * 0.35 + idle * Math.sin(it * 0.05) * 1.1;
      const ty = CAMERA_Y + fx.py * 0.2 + idle * Math.cos(it * 0.037) * 0.4;
      const tz = MathUtils.clamp(intro + fx.dolly - 0.8 * fx.focusPush + 0.3 * fx.fail + 0.25 * fx.anticip + idle * Math.sin(it * 0.028) * 0.6, DOLLY_MIN - 0.8, DOLLY_MAX + 0.5);
      const lx = LOOK_AT[0] + fx.focusPush * 0.5 + idle * Math.sin(it * 0.02) * 0.35;
      const ly = LOOK_AT[1] + idle * Math.cos(it * 0.023) * 0.12;
      if (reduced) {
        p.set(tx, ty, tz);
      } else {
        p.set(
          MathUtils.damp(camera.position.x, tx, 4, dt),
          MathUtils.damp(camera.position.y, ty, 4, dt),
          // 开场推进本身已是缓动，只对其余目标做阻尼
          k < 1 ? tz : MathUtils.damp(camera.position.z, tz, 3, dt),
        );
      }
      look.set(lx, ly, LOOK_AT[2]);
      st.lastLook.copy(look);
      camera.position.copy(p);
      camera.lookAt(look);
    }

    if (Math.abs(cam.fov - fov) > 0.01) {
      cam.fov = fov;
      cam.updateProjectionMatrix();
    }
  });
  return null;
}
