"use client";
import dynamic from "next/dynamic";
import { useCallback, useEffect, useRef, useState } from "react";
import { webglTier, type WebGLTier } from "@/lib/webgl";
import { MODEL_URL } from "./ship/modelUrl";
import { StaticSpace } from "./StaticSpace";

/**
 * 品牌页背景的入口（客户端组件，因为 next/dynamic 的 ssr:false 只能在这里用）：
 * 1. 挂载即画深空静帧（StaticSpace，只有星空，没有船）——卡片与文字不等任何东西，240ms 后就在；
 * 2. 挂载后的下一帧检测 WebGL 分级——"none" 就到此为止，换成带 SVG 母舰的静帧，three 的 chunk 根本不下载；
 * 3. 否则立刻挂 SpaceScene（chunk 开始下载），同时给 glb 打一条 <link rel=preload as=fetch>，模型与 chunk 并行下载；
 * 4. 场景在模型就绪、着色器编译完成后的第一帧回调 onReady：容器 600ms 淡入，镜头的 2.5s 推进从这一刻开始，
 *    船"驶入"一个已经在场的页面；再过 700ms 撤掉静帧。根节点的 data-scene-ready 仍会打上（兼容），但内容不再等它。
 * 第一帧的时刻用 performance.mark("axiom:scene-frame") 记下，便于测量。
 */
const SpaceScene = dynamic(() => import("./SpaceScene"), { ssr: false, loading: () => null });

export function SpaceBackground() {
  const ref = useRef<HTMLDivElement>(null);
  const glRef = useRef<HTMLDivElement>(null);
  const [tier, setTier] = useState<WebGLTier | null>(null);
  const [staticHidden, setStaticHidden] = useState(false);

  const markReady = useCallback(() => {
    ref.current?.parentElement?.setAttribute("data-scene-ready", "");
  }, []);

  useEffect(() => {
    let cancelled = false;
    const raf = requestAnimationFrame(() => {
      if (cancelled) return;
      const t = webglTier();
      setTier(t);
      if (t === "none") {
        markReady();
        return;
      }
      // 模型与 three 的 chunk 并行下载（与 GLTFLoader 的 fetch 同模式：cors + same-origin 凭据）
      if (!document.querySelector(`link[rel="preload"][href="${MODEL_URL}"]`)) {
        const link = document.createElement("link");
        link.rel = "preload";
        link.as = "fetch";
        link.href = MODEL_URL;
        link.crossOrigin = "anonymous";
        document.head.appendChild(link);
      }
    });
    return () => {
      cancelled = true;
      cancelAnimationFrame(raf);
    };
  }, [markReady]);

  const onFirstFrame = useCallback(() => {
    performance.mark("axiom:scene-frame");
    if (glRef.current) glRef.current.style.opacity = "1";
    markReady();
    window.setTimeout(() => setStaticHidden(true), 700);
  }, [markReady]);

  return (
    <div ref={ref} className="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
      {!staticHidden && <StaticSpace ship={tier === "none"} />}
      {tier && tier !== "none" && (
        <div ref={glRef} className="absolute inset-0" style={{ opacity: 0, transition: "opacity 600ms var(--ease-out)" }}>
          <SpaceScene tier={tier} onReady={onFirstFrame} />
        </div>
      )}
    </div>
  );
}
