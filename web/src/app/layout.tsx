import type { Metadata } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import "./globals.css";
import { AppShell } from "@/components/AppShell";
import { ToastProvider } from "@/components/toast";
import { LocaleProvider } from "@/lib/i18n";
import { BOOT_SCRIPT } from "@/lib/boot";

// Inter 承担拉丁与数字（400 / 500 / 600，600 只给页面标题与品牌页标题）；中文回退链在 globals.css 的 --font-sans 里。
const inter = Inter({ subsets: ["latin"], weight: ["400", "500", "600"], variable: "--font-inter", display: "swap" });
// JetBrains Mono 只用于遥测读数：ID、时间戳、token、成本明细、快捷键、表头眉标。
const jetbrains = JetBrains_Mono({ subsets: ["latin"], weight: ["400", "500"], variable: "--font-jetbrains", display: "swap" });

export const metadata: Metadata = {
  title: "AxiomOS",
  description: "AI 原生的组织操作系统 · The AI-native operating system for organizations",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    // suppressHydrationWarning：lang 与 data-theme 都在水合前由脚本写到 <html> 上（语言见 LocaleProvider）
    <html lang="zh-CN" className={`${inter.variable} ${jetbrains.variable}`} suppressHydrationWarning>
      <head>
        {/* 首屏前应用主题偏好（localStorage axiomos.theme）与侧栏收起偏好（axiomos.sidebar），避免先闪一下深色 / 展开态；"系统"不写属性，交给 prefers-color-scheme */}
        <script dangerouslySetInnerHTML={{ __html: BOOT_SCRIPT }} />
      </head>
      <body>
        <LocaleProvider>
          <ToastProvider>
            <AppShell>{children}</AppShell>
          </ToastProvider>
        </LocaleProvider>
      </body>
    </html>
  );
}
