/**
 * WebGL 2 能力分级（品牌页背景场景用）：
 * - "full"：硬件加速的 WebGL 2；
 * - "lite"：有 WebGL 2 但带性能告警或软件渲染（SwiftShader / llvmpipe）→ dpr 1、无色散、Bloom 4 级、无多重采样；
 * - "none"：没有 WebGL 2 → 只用 Canvas2D 静帧，three 的 chunk 根本不下载。
 * 只在客户端调用。
 */
export type WebGLTier = "full" | "lite" | "none";

export function webglTier(): WebGLTier {
  try {
    const c = document.createElement("canvas");
    const gl = c.getContext("webgl2", { failIfMajorPerformanceCaveat: true });
    if (!gl) {
      const soft = c.getContext("webgl2");
      if (!soft) return "none";
      soft.getExtension("WEBGL_lose_context")?.loseContext();
      return "lite";
    }
    const dbg = gl.getExtension("WEBGL_debug_renderer_info");
    const renderer = dbg ? String(gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL)) : "";
    gl.getExtension("WEBGL_lose_context")?.loseContext();
    return /swiftshader|llvmpipe|software|mesa offscreen/i.test(renderer) ? "lite" : "full";
  } catch {
    return "none";
  }
}
