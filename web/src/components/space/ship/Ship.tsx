"use client";
import { Suspense, useRef } from "react";
import { useFrame, useThree } from "@react-three/fiber";
import { Group, Vector3 } from "three";
import { CAMERA_FOV, CAMERA_Z } from "../CameraRig";
import { fx } from "../director";
import { Engines } from "./Engines";
import { Hull } from "./Hull";
import { SHIP_LEN, SVG_NOSE_X, wx, wy } from "./hullProfile";
import { ShipModel } from "./ShipModel";
import { Shuttles } from "./Shuttles";
import { Windows } from "./Windows";

/**
 * 母舰的摆位、漂移与对输入的回应。层级：
 *   outer（摆位 + 缩放 + 提交时向前 0.6）→ orbit（拖拽环绕 ±20°/±8°，松手回正）→ drift（姿态 + 90s 漂移）
 *   → attention（朝光标 ±3°/±1.5°，λ 1.5）→ 船体 / 舷窗 / 引擎
 * 摆位：≥1024px 时船不能压到左侧 96 + 440px 的内容列——按视口宽度算出船的左缘应落在的视口比例，
 * 再把船放到那里；放不下就等比缩小。窄屏居中、退到内容后面。竖直方向落在下三分之二。
 * 姿态：船头朝右、略向观众偏转 16°（看得见船头正面与近侧舷窗），抬头 4°。
 * 漂移：90s 一个来回、±0.18 单位，外加 ±0.5° 的摆动；空闲吸引时幅度 ×1.5；reduced-motion 时静止。
 * 每帧把舰首窗带的世界坐标写进 fx.bowWorld（景深对焦点）。
 * 船体本身是 Blender 模型（ShipModel）；glb 到达之前用程序化母舰（Hull / Windows / Engines）占位。
 */
const YAW = -0.28;
const PITCH = 0.07;
const CONTENT_COLUMN_PX = 96 + 440 + 48; // 内容列 + 一点呼吸（推进翼尖不贴到面板角标）
const X_EXTENT: [number, number] = [-4.0, SHIP_LEN / 2 + 0.1]; // 含推进翼（Blender 模型 ×0.62 的包围盒，再算上近侧翼尖因偏航向左探出的部分）
const BOW_LOCAL = new Vector3(wx(SVG_NOSE_X) - 0.25, wy(264), 0);
const THRUST_UNITS = 0.6;

function ProceduralShip({ reduced }: { reduced: boolean }) {
  return (
    <>
      <Hull />
      <Windows reduced={reduced} />
      <Engines reduced={reduced} />
    </>
  );
}

export function Ship({ reduced }: { reduced: boolean }) {
  const outer = useRef<Group>(null!);
  const orbit = useRef<Group>(null!);
  const drift = useRef<Group>(null!);
  const attention = useRef<Group>(null!);
  const size = useThree((s) => s.size);

  // 相机在终点时的可见半宽 / 半高（世界单位，z=0 平面附近）
  const halfH = Math.tan((CAMERA_FOV * Math.PI) / 360) * CAMERA_Z;
  const halfW = halfH * (size.width / size.height);
  const cosYaw = Math.cos(YAW), sinYaw = Math.sin(YAW);
  const projLen = (X_EXTENT[1] - X_EXTENT[0]) * cosYaw;
  const wide = size.width >= 1024;
  let scale = 1, x = 0, y = -0.55;
  if (wide) {
    const fL = Math.max(0.43, CONTENT_COLUMN_PX / size.width);
    const fa = Math.max(0.2, 1 - fL - 0.03);
    scale = Math.min(1, (fa * 2 * halfW) / projLen);
    const left = (fL - 0.5) * 2 * halfW;
    x = left - X_EXTENT[0] * cosYaw * scale;
    y = -halfH * 0.22 - 0.15 * (1 - scale);
  } else {
    scale = Math.min(1, (0.9 * 2 * halfW) / projLen);
    x = -((X_EXTENT[0] + X_EXTENT[1]) / 2) * cosYaw * scale;
    y = -halfH * 0.5;
  }

  useFrame(({ clock }) => {
    const t = clock.elapsedTime;
    const amp = 1 + 0.5 * fx.idle;
    const g = drift.current;
    if (!reduced) {
      g.position.y = Math.sin(t * 0.07) * 0.12 * amp;
      g.position.x = Math.sin(t * 0.05 + 1.0) * 0.18 * amp;
      g.rotation.z = PITCH + Math.sin(t * 0.06) * 0.0087 * amp;
    }
    // 船头方向（世界）：+x 绕 y 转 YAW
    const th = fx.thrust * THRUST_UNITS * scale;
    outer.current.position.set(x + th * cosYaw, y, -th * sinYaw);
    orbit.current.rotation.set(fx.orbitPolar, fx.orbitAz, 0);
    attention.current.rotation.set(fx.attentionPitch, fx.attentionYaw, 0);
    attention.current.updateWorldMatrix(true, false);
    fx.shipMatrix.copy(attention.current.matrixWorld);
    fx.bowWorld.copy(BOW_LOCAL).applyMatrix4(attention.current.matrixWorld);
  });

  return (
    <group ref={outer} position={[x, y, 0]} scale={scale}>
      <group ref={orbit}>
        <group ref={drift} rotation={[0, YAW, PITCH]}>
          <group ref={attention}>
            {/* Blender 模型加载前先显示程序化母舰（同一位置、同一姿态），加载完成后替换 */}
            <Suspense fallback={<ProceduralShip reduced={reduced} />}>
              <ShipModel reduced={reduced} />
            </Suspense>
          </group>
        </group>
      </group>
      <Shuttles reduced={reduced} />
    </group>
  );
}
