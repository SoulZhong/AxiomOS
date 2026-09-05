"use client";
import Link from "next/link";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { api, type Session } from "@/lib/api";
import { fmtMoney } from "@/lib/format";
import { t } from "@/lib/i18n";
import { LanguageSwitch } from "./LanguageSwitch";
import { ScopePicker } from "./ScopePicker";
import { useShipTelemetry } from "./ship-status/telemetry";
import { Sparkline } from "./ship-status/Sparkline";
import { StarBand } from "./ship-status/StarBand";
import { ThemeSwitch } from "./ThemeSwitch";
import { Readout, StatusLED, SysClock, cx } from "./ui";

/*
 * 舷窗带（DESIGN.md「舰内系统 v3」§1）：主内容区顶部 32px 的舰桥舷窗——背后一条 Canvas2D 星海缓慢流过（StarBand，两层视差、软圆星点），
 * 前景是等宽 11px 读数。整条在浅色主题下也是深色（data-theme="dark"）：白色舱室里的一条舷窗。
 * 读数：AXIOM · 操作系统 · 组织 DEMO · AGENT 在线 n/m（有在线时雷达式脉冲）· 执行中 n · 待确认 n（只在有的时候出现，点它去待确认操作页）· 今日成本 ¥x（24 小时迷你折线 + 数字滚动）；
 * 右侧 系统 16:07:22 + 主题 / 语言切换。数据来自 ship-status/telemetry（30s 刷新、隐藏时停、与「舰况」面板共用一次请求）。
 * 后台版：AXIOM · 平台控制台 · 组织 n · 系统。
 */

/** 一条读数：标签（ink-subtle）+ 数值（遥测色）。className 负责 display（默认 inline-flex；按断点用 `hidden md:inline-flex` 收起）。 */
function Item({ led, children, className = "inline-flex" }: { led?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <span className={cx("shrink-0 items-center gap-1.5 whitespace-nowrap", className)}>
      {led}
      {children}
    </span>
  );
}
const Val = ({ value }: { value: ReactNode }) => <Readout value={value} className="text-telemetry" />;
/** 标签 + 数值 + 可选后缀：英文是 `AGENTS 2/2 ONLINE`，中文是 `AGENT 在线 2/2`（后缀为空就不渲染，不留多余空格）。 */
function Reading({ label, value, suffix }: { label: string; value: ReactNode; suffix?: string }) {
  return (
    <>
      {label} <Val value={value} />
      {suffix ? <> {suffix}</> : null}
    </>
  );
}
const Sep = ({ className }: { className?: string }) => <i className={cx("bridge-sep", className)} aria-hidden="true" />;

/**
 * 舷窗带里的滚动数字（odometer）：每一位是一列 0–9，值变化时该列 160ms 强 ease-out 滚到新数字；非数字字符（¥ , .）原样显示。
 * 首次渲染不滚（data-still）。位数从右往左对齐（key 按距末尾的位置），金额增加一位时新位从左边出现，已有的位不跳。
 */
function Odometer({ text, className }: { text: string; className?: string }) {
  const [still, setStill] = useState(true);
  const first = useRef(text);
  useEffect(() => {
    if (text !== first.current) setStill(false);
  }, [text]);
  const chars = [...text];
  return (
    <span className={cx("odo", className)} data-still={still || undefined} aria-label={text}>
      {chars.map((ch, i) => {
        const fromEnd = chars.length - 1 - i;
        if (!/\d/.test(ch)) return <span key={`c${fromEnd}`} className="odo-c">{ch}</span>;
        const d = Number(ch);
        return (
          <span key={`d${fromEnd}`} className="odo-d" aria-hidden="true">
            <span className="odo-col" style={{ transform: `translateY(${-d * 100}%)` }}>
              {Array.from({ length: 10 }, (_, k) => <span key={k}>{k}</span>)}
            </span>
          </span>
        );
      })}
    </span>
  );
}

/** 雷达式脉冲：在线的 success 灯外一圈 2.4s 扩散的细环（有 Agent 在线时才有）。 */
function RadarLED({ online }: { online: boolean }) {
  return (
    <span className="radar" aria-hidden="true">
      <StatusLED tone={online ? "online" : "dark"} />
      {online && <i className="radar-ring" />}
    </span>
  );
}

/** 组织外壳的舷窗带。 */
export function BridgeBar({ org, live }: { org: Session["organization"] | null | undefined; live: boolean }) {
  const tele = useShipTelemetry(live);
  const slug = org ? (org.slug ?? org.name).toUpperCase() : "—";
  const anyOnline = !!tele && tele.agentsOnline > 0;
  const anyRuns = !!tele && tele.active > 0;
  const cost = tele ? fmtMoney(tele.costToday, org?.currency) : "—";
  const pending = tele?.proposalsPending ?? 0;
  return (
    <div className="bridge-bar" data-theme="dark" role="status" aria-label={t("bridge.label")}>
      <StarBand className="star-band" />
      <div className="bridge-content eyebrow flex h-full items-center gap-2.5 px-4 text-ink-subtle md:px-6">
        <Item className="hidden lg:inline-flex">{t("bridge.os")}</Item>
        <Sep className="hidden lg:block" />
        <Item>
          <Reading label={t("bridge.org")} value={slug} />
        </Item>
        {/* 范围选择器：紧挨着组织读数，全公司 → 某个团队是同一条线上的收窄（ADR 0013） */}
        <Sep />
        <ScopePicker className="inline-flex" />
        <Sep className="hidden sm:block" />
        <Item className="hidden sm:inline-flex" led={<RadarLED online={anyOnline} />}>
          <Reading label={t("bridge.agents")} value={tele ? `${tele.agentsOnline}/${tele.agentsTotal}` : "–/–"} suffix={t("bridge.online")} />
        </Item>
        <Sep className="hidden md:block" />
        <Item className="hidden md:inline-flex" led={<StatusLED tone={anyRuns ? "accent" : "dark"} />}>
          <Reading label={t("bridge.runs")} value={tele ? tele.active : "–"} suffix={t("bridge.active")} />
        </Item>
        {/* 待确认操作：只在有等人确认的动作时出现（ADR 0003），点它进「待确认操作」页 */}
        {!!pending && (
          <>
            <Sep />
            <Item led={<StatusLED tone="warning" />}>
              <Link href="/proposals/" className="inline-flex items-center gap-1.5 hover:text-ink" title={t("bridge.proposalsTip", { n: pending })}>
                <Reading label={t("bridge.proposals")} value={pending} />
              </Link>
            </Item>
          </>
        )}
        {/* 1280 宽的内容区放不下全部五组读数 + 时钟 + 两个切换：成本读数从 1400px 起显示 */}
        <Sep className="hidden min-[1400px]:block" />
        <Item className="hidden min-[1400px]:inline-flex">
          {t("bridge.costToday")} <Odometer text={cost} className="text-telemetry" />
          {tele && <Sparkline data={tele.series.cost} width={48} height={14} tone="accent" className="ml-1" title={tele.costReal ? t("bridge.costSpark") : t("bridge.costFlat")} />}
        </Item>
        <span className="ml-auto flex shrink-0 items-center gap-2.5">
          <SysClock className="hidden sm:inline-flex" />
          <Sep className="hidden sm:block" />
          <ThemeSwitch />
          <LanguageSwitch loggedIn={live} />
        </span>
      </div>
    </div>
  );
}

const ADMIN_REFRESH_MS = 30_000;
/** 平台后台：组织数，30s 一次，隐藏时跳过。 */
function useAdminOrgs(live: boolean): number | null {
  const [n, setN] = useState<number | null>(null);
  useEffect(() => {
    if (!live) return;
    let alive = true;
    const pull = () => {
      if (document.hidden) return;
      api.admin.stats().then(
        (s) => alive && setN(s.organizations),
        () => {
          /* 失败时保留上一组读数 */
        },
      );
    };
    pull();
    const id = window.setInterval(pull, ADMIN_REFRESH_MS);
    const onVisible = () => {
      if (!document.hidden) pull();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      alive = false;
      window.clearInterval(id);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [live]);
  return n;
}

/** 平台后台的舷窗带：AXIOM · 平台控制台 · 组织 n · 系统。 */
export function AdminBridgeBar({ live }: { live: boolean }) {
  const orgs = useAdminOrgs(live);
  return (
    <div className="bridge-bar" data-theme="dark" role="status" aria-label={t("bridge.label")}>
      <StarBand className="star-band" />
      <div className="bridge-content eyebrow flex h-full items-center gap-3 px-4 text-ink-subtle md:px-6">
        <Item className="hidden sm:inline-flex">{t("bridge.console")}</Item>
        <Sep className="hidden sm:block" />
        <Item>
          <Reading label={t("bridge.orgs")} value={orgs ?? "–"} />
        </Item>
        <span className="ml-auto flex shrink-0 items-center gap-2.5">
          <SysClock className="hidden sm:inline-flex" />
          <Sep className="hidden sm:block" />
          <ThemeSwitch />
          <LanguageSwitch />
        </span>
      </div>
    </div>
  );
}
