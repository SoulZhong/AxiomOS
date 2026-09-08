"use client";
import Link from "next/link";
import { useState } from "react";
import { api } from "@/lib/api";
import { t } from "@/lib/i18n";
import { IconAgent, IconChevronDown, IconChevronRight, ShipMark } from "@/components/icons";
import { LanguageSwitch } from "@/components/LanguageSwitch";
import { useToast } from "@/components/toast";
import { Button, CopyButton, CopyLine } from "@/components/ui";

/**
 * `/connect/`（ADR 0024）：**同一条地址**的浏览器一面。
 * Agent / curl 取 `GET /connect` 拿到写给 Agent 看的操作说明（Markdown）；浏览器带 `Accept: text/html` 会被 303 到这里。
 * 所以这张页面只做一件事：让人把那条链接原样拿走。
 *
 * 公开页，不需要登录（AppShell 把 /connect 归进 bare：不查会话，也就不会被 401 弹去登录页）；
 * 没有侧栏、没有舷窗带、没有范围条；居中一栏、最宽 560px。语言与登录页一样由 LocaleProvider 解析
 * （localStorage → navigator.language），底部留一个切换。
 *
 * 链接由 publicBase() 得出——浏览器里就是 window.location.origin，所以任何部署下复制到的都是那个部署自己的地址。
 */
export default function OnboardPage() {
  const toast = useToast();
  const [selfOpen, setSelfOpen] = useState(false);
  const prompt = api.agentAuth.onboardPrompt();
  const command = `curl -fsSL '${api.agentAuth.scriptUrl("claude-code")}' | sh`;
  const copied = () => toast.ok(t("onboard.copied"));

  return (
    <div className="flex min-h-screen flex-col items-center bg-canvas px-4 py-12">
      {/* my-auto 而不是 justify-center：内容比视口高时（窄屏展开「我想自己动手」）自动外边距归零，顶部不会被裁掉 */}
      <main className="my-auto w-full max-w-[560px]">
        {/* 品牌：舰徽 + 产品名，一行 */}
        <div className="flex items-center gap-2.5">
          <ShipMark size={24} className="text-ink" />
          <span className="text-title text-ink">AxiomOS</span>
        </div>

        {/* 全页只有这一句话 */}
        <h1 className="mt-6 text-headline text-balance text-ink">{t("onboard.lead")}</h1>

        {/* 链接：大号等宽，整条可见（长了折行，不截断）；复制按钮在右 */}
        <div className="mt-6">
          <p className="eyebrow mb-2 text-ink-subtle">{t("onboard.linkLabel")}</p>
          <div className="flex flex-col gap-2.5 sm:flex-row sm:items-start">
            <code data-onboard-link="" className="min-w-0 flex-1 rounded-md border border-hairline bg-code-bg px-3.5 py-3 font-mono text-[15px] leading-[1.5] break-all text-telemetry">
              {prompt}
            </code>
            <CopyButton text={prompt} size="md" variant="primary" onCopied={copied} />
          </div>
        </div>

        {/* 接下来会发生什么：四句大白话，等宽序号 */}
        <section className="mt-8">
          <h2 className="text-body font-medium text-ink">{t("onboard.next")}</h2>
          <ol className="mt-3 flex flex-col gap-2">
            {([t("onboard.next1"), t("onboard.next2"), t("onboard.next3"), t("onboard.next4")] as const).map((s, i) => (
              <li key={i} className="flex items-baseline gap-2.5 text-body text-ink-muted">
                <span className="eyebrow shrink-0 text-ink-tertiary">{String(i + 1).padStart(2, "0")}</span>
                <span className="min-w-0">{s}</span>
              </li>
            ))}
          </ol>
        </section>

        {/* 我想自己动手：默认收起，里面是原来的 curl 一行与手工注册的入口 */}
        <section className="mt-8 border-t border-hairline pt-4">
          <button type="button" className="pressable inline-flex items-center gap-1 text-caption text-ink-muted hover:text-ink" onClick={() => setSelfOpen((v) => !v)} aria-expanded={selfOpen}>
            {selfOpen ? <IconChevronDown size={14} /> : <IconChevronRight size={14} />}
            {t("onboard.self")}
          </button>
          {selfOpen && (
            <div className="mt-3 space-y-2" data-onboard-self="">
              <p className="text-caption text-ink-subtle">{t("onboard.selfHint")}</p>
              <CopyLine text={command} />
              <p className="text-caption text-ink-subtle">{t("onboard.selfNote")}</p>
              <p className="pt-1 text-caption text-ink-subtle">{t("onboard.manual")}</p>
              <Link href="/agents/" className="inline-flex"><Button size="sm" icon={<IconAgent />} tabIndex={-1}>{t("onboard.manualLink")}</Button></Link>
            </div>
          )}
        </section>

        <div className="mt-10">
          <LanguageSwitch />
        </div>
      </main>
    </div>
  );
}
