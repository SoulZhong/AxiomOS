import type { ReactNode } from "react";
import { t } from "@/lib/i18n";
import { CardTilt } from "./CardTilt";
import { SpaceBackground } from "./space/SpaceBackground";
import { ShipMark } from "./icons";
import { LanguageSwitch } from "./LanguageSwitch";
import { cx } from "./ui";

/**
 * 品牌画布：登录 / 邀请 / 平台后台登录共用——镜头从深空缓缓推向 Axiom 母舰，登录就是"请求登舰"（DESIGN.md「品牌页 = 接近母舰」）。
 * 背景全部在 space/SpaceBackground（WebGL 实时场景：fbm 星云 + 远日 → 三层视差星场 → 背景行星 → 程序化 PBR 母舰 → 前景星尘
 * → N8AO / 景深 / Bloom / ACES / 颗粒 / 暗角；加载期与无 WebGL 时是 Canvas2D + SVG 静帧）；
 * 前景：64px 轨道舰徽 → 等宽眉标 → 40px/600 标题（关键词 accent-hover 着色）→ 一句说明 → 登舰面板 → 等宽语言切换。
 * ≥1024px 时前景靠左成一栏（96px 内边距、440px 宽、垂直居中），母舰完整地占据画面右侧 42% → 96%，两者不重叠；
 * 窄屏时前景居中堆叠，母舰退到后面 45%。
 * 前景挂载即从下方 8px 处 240ms 淡入，不等场景；WebGL 场景就绪后在它身后 600ms 淡入并开始 2.5s 的镜头推进——船驶入已经在场的页面。
 * 登录成功 = 登舰：镜头 1.6s 加速飞向舰首窗，前景淡出，暖白涌满画面，再进入应用（AppShell 让它退去）。
 * 面板：400px，surface-1 90%（隐约透出船体）+ 1px accent-border + 极淡外晕，四角各一枚 8px 细线角标；随光标最多倾斜 4°（CardTilt）。
 * 没有自检行，没有打字机效果。
 * 场景的输入监听挂在根节点（data-brand-root）：光标位置、内容列（data-brand-content）之外的拖拽环绕与滚轮推拉；
 * 表单事件由各页面通过 space/sceneBus 发给场景。
 * 品牌页永远是深色（舱外的太空）：根节点 data-theme="dark" 让 globals.css 把整套深色 token 局部重设到这棵子树，
 * 用户选了浅色也不受影响。
 */
export function BrandCanvas({ children, headline = true, subtitle, eyebrow }: { children: ReactNode; headline?: boolean | ReactNode; subtitle?: ReactNode; eyebrow?: string }) {
  const head = headline === true ? (
    <>
      {t("login.headlinePre")}
      <span className="text-accent-hover">{t("login.headlineKey")}</span>
      {t("login.headlinePost")}
    </>
  ) : headline === false ? null : headline;
  return (
    <div data-theme="dark" data-brand-root className="relative min-h-screen overflow-hidden bg-canvas text-ink">
      <SpaceBackground />
      {/* 登舰：成功后最后 350ms 涌满画面的暖白（见 globals.css .ax-board-overlay） */}
      <div className="ax-board-overlay" aria-hidden="true" />

      {/* ≥1024px：内容靠左一栏（96px 内边距、440px 宽、垂直居中），母舰完整地占右侧；窄屏：居中堆叠，船退到后面 */}
      <div className="ax-fg relative z-10 flex min-h-screen flex-col items-center justify-center px-4 py-10 lg:items-start lg:pl-24 lg:pr-0">
        <div data-brand-content className="flex w-full max-w-[440px] flex-col items-center text-center lg:items-start lg:text-left">
          {/* 轨道舰徽 64px */}
          <ShipMark size={64} className="text-accent-hover" title="AxiomOS" />
          {/* 舰桥标识牌 */}
          <p className="eyebrow mt-5 text-ink-subtle">{eyebrow ?? t("bridge.os")}</p>
          {head && <h1 className="mt-3 text-display text-balance text-ink">{head}</h1>}
          {subtitle && <p className="mt-3 text-body text-ink-subtle">{subtitle}</p>}

          <CardTilt className="card-tilt relative mt-8 w-full max-w-[400px] text-left">
            <div className="brand-card relative rounded-xl p-8 text-ink">{children}</div>
            {/* 四角定位标记：8px 细线角标 */}
            <Corner className="-top-1.5 -left-1.5 border-t border-l" />
            <Corner className="-top-1.5 -right-1.5 border-t border-r" />
            <Corner className="-bottom-1.5 -left-1.5 border-b border-l" />
            <Corner className="-bottom-1.5 -right-1.5 border-b border-r" />
          </CardTilt>
          <div className="mt-6">
            <LanguageSwitch />
          </div>
        </div>
      </div>
    </div>
  );
}

function Corner({ className }: { className: string }) {
  return <span className={cx("pointer-events-none absolute h-2 w-2 border-accent-hover opacity-70", className)} aria-hidden="true" />;
}
