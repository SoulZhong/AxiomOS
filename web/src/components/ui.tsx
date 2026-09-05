"use client";
import Link from "next/link";
import { useEffect, useRef, useState, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type SyntheticEvent, type TextareaHTMLAttributes } from "react";
import type { ExecutorRef } from "@/lib/api";
import { fmtDateTime, fmtRelative } from "@/lib/format";
import { t } from "@/lib/i18n";
import { keyboardIntent, markKeyboardIntent } from "@/lib/motion";
import { IconAgent, IconCheck, IconClose, IconCopy, RadarIllustration } from "./icons";

/*
 * 组件小套件，对应 web/DESIGN.md v2 的 components 节与「Axiom 科幻层（舰桥系统）」。
 * 深色画布：surface-1 面板 + 1px 细线 + 顶边内嵌高光，没有投影；唯一强调色是 accent；
 * 语义色只表状态，用"文字 / 12% 底 / 35% 边"三件套；唯一的发光是主按钮与状态指示灯。
 * 舰桥层：面板四角 HUD 角标 + 序号眉标 + 遥测位；页头能量线；刻度进度条；雷达空状态；读数淡入。
 */

export function cx(...xs: Array<string | false | null | undefined>) {
  return xs.filter(Boolean).join(" ");
}

// ---------- 标签（tag-*） ----------
export type Tone = "neutral" | "accent" | "success" | "warning" | "danger";
const TONES: Record<Tone, string> = {
  neutral: "border-neutral-border bg-neutral-bg text-ink-muted",
  accent: "border-accent-border bg-accent-bg text-accent-hover",
  success: "border-success-border bg-success-bg text-success",
  warning: "border-warning-border bg-warning-bg text-warning",
  danger: "border-danger-border bg-danger-bg text-danger",
};
/** 三件套语义色：文字 + 12% 底 + 35% 边，4px 圆角、12px。dark = 已终止（不是错误，只是结束了）：中性、文字再淡一级。 */
export function Tag({ tone = "neutral", dark, children, className, title }: { tone?: Tone; dark?: boolean; children: ReactNode; className?: string; title?: string }) {
  return (
    <span title={title} className={cx("inline-flex items-center rounded-xs border px-1.5 py-px text-caption whitespace-nowrap", dark ? "border-hairline-strong bg-surface-3 text-ink-subtle" : TONES[tone], className)}>
      {children}
    </span>
  );
}
export const Badge = Tag;
/** 一行标签，最多 3 个，其余折成 "+N"（悬停看全部）。表格单元格不再因为标签换行而变高。 */
export function TagList({ items, max = 3, tone, empty = "—" }: { items: string[]; max?: number; tone?: Tone; empty?: ReactNode }) {
  if (!items.length) return <span className="text-ink-subtle">{empty}</span>;
  const shown = items.slice(0, max);
  const rest = items.slice(max);
  return (
    <span className="inline-flex items-center gap-1 whitespace-nowrap">
      {shown.map((x) => <Tag key={x} tone={tone}>{x}</Tag>)}
      {rest.length > 0 && (
        <Tip tip={rest.join("\n")}>
          <Tag tone={tone}>+{rest.length}</Tag>
        </Tip>
      )}
    </span>
  );
}

// ---------- 按钮（button-*） ----------
export type ButtonVariant = "primary" | "default" | "danger" | "ghost" | "on-dark";
/** primary = 主按钮（带微光，一页只有一个）；default = 次级（surface-2 + 强细线）；ghost = 第三级（透明）；danger = 危险描边。on-dark 是旧名，等同 default。 */
const VARIANTS: Record<ButtonVariant, string> = {
  primary: "btn-primary bg-accent text-on-accent border-transparent shadow-primary hover:bg-accent-hover active:bg-accent-focus",
  default: "bg-surface-2 text-ink border-hairline-strong hover:bg-surface-3 hover:border-hairline-tertiary active:bg-surface-4",
  danger: "bg-transparent text-danger border-danger-border hover:bg-danger-bg",
  ghost: "bg-transparent text-ink-muted border-transparent hover:bg-surface-2 hover:text-ink",
  "on-dark": "bg-surface-2 text-ink border-hairline-strong hover:bg-surface-3",
};
export function Button({ variant = "default", size = "md", icon, className, children, type = "button", ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; size?: "sm" | "md"; icon?: ReactNode }) {
  return (
    <button
      type={type}
      {...props}
      className={cx(
        "pressable inline-flex shrink-0 items-center justify-center gap-1.5 rounded-md border text-[14px] leading-none font-medium whitespace-nowrap disabled:cursor-not-allowed disabled:opacity-40 disabled:shadow-none",
        size === "sm" ? "h-7" : "h-8",
        variant === "ghost" ? (size === "sm" ? "px-2" : "px-2.5") : size === "sm" ? "px-2.5" : "px-3.5",
        VARIANTS[variant],
        className,
      )}
    >
      {icon && <span className="-ml-0.5 inline-flex text-current opacity-80">{icon}</span>}
      {children}
    </button>
  );
}

/** 纯 CSS 提示气泡（surface-3 底 + 强细线，12px，最大宽 280px）；tip 为空时不显示。禁用按钮的原因就放这里。 */
export function Tip({ tip, children, className, placement = "top" }: { tip?: string | null; children: ReactNode; className?: string; placement?: "top" | "bottom" | "right" }) {
  return (
    <span className={cx("tip", placement === "bottom" && "tip-bottom", placement === "right" && "tip-right", className)} data-tip={tip || undefined}>
      {children}
    </span>
  );
}

// ---------- 表单（text-input） ----------
export const inputCls = "h-8 rounded-md border border-hairline bg-surface-1 px-3 text-body text-ink placeholder:text-ink-tertiary hover:border-hairline-strong disabled:bg-surface-2 disabled:text-ink-subtle disabled:hover:border-hairline";
/** 默认占满一行；className 里自带 w-* 时不再加 w-full（过滤条里的窄下拉）。 */
const widthOf = (className?: string) => (className && /(^|\s)(w-|min-w-|max-w-)/.test(className) ? "" : "w-full");
export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={cx(inputCls, widthOf(props.className), props.className)} />;
}
export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...props} className={cx(inputCls, widthOf(props.className), "pr-6", props.className)} />;
}
export function Textarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...props} className={cx(inputCls, widthOf(props.className), "h-auto min-h-20 py-1.5 leading-[1.4]", props.className)} />;
}
/** 标签在上方（12px 说明色），说明在下方，错误信息 12px 危险色。不用占位符当标签。eyebrow = 品牌页的等宽眉标标签（EMAIL / PASSWORD）。 */
export function Field({ label, hint, error, children, className, as = "label", eyebrow = false }: { label: ReactNode; hint?: ReactNode; error?: string | null; children: ReactNode; className?: string; as?: "label" | "div"; eyebrow?: boolean }) {
  const Tagname = as;
  return (
    <Tagname className={cx("block", className)}>
      <span className={cx("block text-ink-subtle", eyebrow ? "eyebrow mb-1.5" : "mb-1 text-caption")}>{label}</span>
      {children}
      {error ? <span className="mt-1 block text-caption text-danger">{error}</span> : hint ? <span className="mt-1 block text-caption text-ink-subtle">{hint}</span> : null}
    </Tagname>
  );
}
/** 日期输入：与 Input 同高同边；空值时把浏览器的 mm/dd/yyyy 淡成占位符色。 */
export function DateInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input type="date" {...props} data-empty={props.value === "" || props.value === undefined ? "true" : undefined} className={cx(inputCls, widthOf(props.className), props.className)} />;
}
/** 表单分组：小标题 + 细线分隔。短字段（日期、优先级）才两栏，标题 / 描述占满一行。 */
export function FormSection({ title, children, className }: { title: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cx("border-t border-hairline pt-4 first:border-t-0 first:pt-0", className)}>
      <h3 className="eyebrow mb-3 text-ink-subtle">{title}</h3>
      <div className="space-y-3">{children}</div>
    </section>
  );
}
export function Checkbox({ label, className, ...props }: InputHTMLAttributes<HTMLInputElement> & { label: ReactNode }) {
  return (
    <label className={cx("inline-flex items-center gap-1.5 text-body", props.disabled && "text-ink-subtle", className)}>
      <input type="checkbox" {...props} className="h-3.5 w-3.5" />
      {label}
    </label>
  );
}

// ---------- 面板（panel / panel-header）：HUD 角标 + 序号眉标 + 遥测位 ----------
/** 四角 6px 细线角标。父元素需带 .hud（相对定位 + --hud 颜色变量）；悬停 / focus-within 变 accent-border。 */
export function HudCorners() {
  return (
    <>
      <i className="hud-c hud-tl" aria-hidden="true" />
      <i className="hud-c hud-tr" aria-hidden="true" />
      <i className="hud-c hud-bl" aria-hidden="true" />
      <i className="hud-c hud-br" aria-hidden="true" />
    </>
  );
}
/** 面板序号眉标：`01`、`02`…按页面内顺序；数字也可以直接传字符串。 */
export const panelIndex = (n: number | string) => (typeof n === "number" ? String(n).padStart(2, "0") : n);
/**
 * 面板：surface-1 + 细线 + 顶边高光 + 四角 HUD 角标。
 * index = 标题前的等宽序号眉标（页面内顺序）；telemetry = 标题右侧的遥测位（条数、更新时间之类的等宽读数）。
 */
export function Panel({ title, actions, children, className, padded = true, id, index, telemetry, icon }: { title?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string; padded?: boolean; id?: string; index?: number | string; telemetry?: ReactNode; icon?: ReactNode }) {
  return (
    <section id={id} className={cx("hud rounded-lg border border-hairline bg-surface-1 shadow-panel", className)}>
      <HudCorners />
      {(title || actions || index !== undefined) && <PanelHeader title={title} actions={actions} index={index} telemetry={telemetry} icon={icon} />}
      <div className={cx(padded && "p-4", "[&>.tbl-wrap]:rounded-b-lg")}>{children}</div>
    </section>
  );
}
/** icon = 标题前的 16px 签名图标（ink-subtle），只给承载概念的面板：目标 / 任务 / 待领取 / Agent / 动态 / 执行记录 / 用量 / 参与角色。 */
export function PanelHeader({ title, actions, index, telemetry, icon }: { title?: ReactNode; actions?: ReactNode; index?: number | string; telemetry?: ReactNode; icon?: ReactNode }) {
  return (
    <header className="flex min-h-[44px] items-center justify-between gap-3 rounded-t-lg border-b border-hairline bg-transparent px-4 py-2.5">
      <h2 className="flex min-w-0 items-center gap-2.5 text-title text-ink">
        {index !== undefined && <span className="eyebrow shrink-0 text-telemetry" aria-hidden="true">{panelIndex(index)}</span>}
        {icon && <span className="inline-flex shrink-0 text-ink-subtle" aria-hidden="true">{icon}</span>}
        <span className="min-w-0">{title}</span>
      </h2>
      {(actions || telemetry) && (
        <div className="flex shrink-0 items-center gap-3">
          {telemetry && <span className="eyebrow hidden text-ink-subtle sm:inline">{telemetry}</span>}
          {actions && <div className="flex items-center gap-2">{actions}</div>}
        </div>
      )}
    </header>
  );
}
export const Card = Panel;

/** 舰桥读数：value 变化时 200ms 淡入（key 随值重挂）。只包数字 / 读数，不包整行。 */
export function Readout({ value, className }: { value: ReactNode; className?: string }) {
  return (
    <span key={typeof value === "string" || typeof value === "number" ? String(value) : undefined} className={cx("readout", className)}>
      {value}
    </span>
  );
}

/** 舰桥状态栏右侧的等宽时钟 `SYS 16:07:22`，秒级跳动。挂载后才读时间，服务端不渲染，避免水合不一致；固定 8ch 宽、tabular 数字，跳动不抖。 */
export function SysClock({ className }: { className?: string }) {
  const [now, setNow] = useState<string | null>(null);
  useEffect(() => {
    const tick = () => {
      const d = new Date();
      setNow(`${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}:${String(d.getSeconds()).padStart(2, "0")}`);
    };
    tick();
    const id = window.setInterval(tick, 1000);
    return () => window.clearInterval(id);
  }, []);
  return (
    // className 负责 display（默认 inline-flex；状态栏在窄屏用 `hidden sm:inline-flex` 收起）
    <span className={cx("eyebrow items-center gap-1.5 text-ink-subtle", className ?? "inline-flex")} aria-hidden="true">
      <span>{t("bridge.sys")}</span>
      <span className="inline-block w-[8ch] text-telemetry tabular-nums">{now ?? "--:--:--"}</span>
    </span>
  );
}

/** 4. 能量线：标题下一条 160px × 1px 的 accent → 透明渐隐。 */
export function EnergyLine({ className }: { className?: string }) {
  return <span className={cx("energy-line", className)} aria-hidden="true" />;
}

/** 页头：标题 24px/600 -0.5px → 能量线 → 一句说明（ink-subtle）；右侧唯一主动作。时钟已移到舰桥状态栏。 */
export function PageHeader({ title, description, actions, breadcrumb, className }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; breadcrumb?: ReactNode; className?: string; clock?: boolean }) {
  return (
    <div className={cx("mb-4 flex flex-wrap items-start justify-between gap-3", className)}>
      <div className="min-w-0">
        {breadcrumb && <div className="mb-1 flex flex-wrap items-center gap-1 text-caption text-ink-subtle">{breadcrumb}</div>}
        <h1 className="text-headline text-ink">{title}</h1>
        <EnergyLine className="mt-2" />
        {description && <p className="mt-2 max-w-[768px] text-body text-ink-subtle">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}

// ---------- 空状态 ----------
/** 8. 雷达扫描：细线同心圆 + 8s 一圈的扇形微光，正中 48px 舰徽。reduced-motion 时静止。 */
export function EmptyIllustration({ className }: { className?: string }) {
  return <RadarIllustration className={className} />;
}
export function Empty({ text, action, illustration = true, className }: { text?: ReactNode; action?: ReactNode; illustration?: boolean; className?: string }) {
  return (
    <div className={cx("flex flex-col items-center justify-center gap-3 px-4 py-10 text-center", className)}>
      {illustration && <EmptyIllustration />}
      <p className="text-body text-ink-subtle">{text ?? t("common.empty")}</p>
      {action && <div>{action}</div>}
    </div>
  );
}

// ---------- 骨架屏 ----------
export function Skeleton({ className, style }: { className?: string; style?: React.CSSProperties }) {
  return <div className={cx("skeleton h-3.5", className)} style={style} aria-hidden="true" />;
}
/** 与表格同形的骨架：表头 + n 行。 */
export function TableSkeleton({ rows = 5, cols = 5 }: { rows?: number; cols?: number }) {
  const widths = [38, 14, 16, 16, 12, 10, 10];
  return (
    <div aria-busy="true">
      <div className="flex gap-3 border-b border-hairline bg-surface-2 px-3 py-2.5">
        {Array.from({ length: cols }, (_, i) => (
          <Skeleton key={i} className="h-2.5" style={{ width: `${widths[i % widths.length]}%` }} />
        ))}
      </div>
      {Array.from({ length: rows }, (_, r) => (
        <div key={r} className="flex items-center gap-3 border-t border-hairline px-3 py-3 first:border-t-0">
          {Array.from({ length: cols }, (_, i) => (
            <Skeleton key={i} style={{ width: `${widths[i % widths.length] - (r % 3) * 2}%` }} />
          ))}
        </div>
      ))}
    </div>
  );
}
export function ListSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div className="space-y-3 p-4" aria-busy="true">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="space-y-1.5">
          <Skeleton style={{ width: `${55 - (i % 3) * 10}%` }} />
          <Skeleton className="h-2.5" style={{ width: `${30 + (i % 2) * 15}%` }} />
        </div>
      ))}
    </div>
  );
}
export function DetailSkeleton() {
  return (
    <div aria-busy="true">
      <Skeleton className="mb-2 h-2.5 w-40" />
      <Skeleton className="mb-4 h-5 w-96" />
      <div className="mb-4 h-12 rounded-lg border border-hairline bg-surface-1 shadow-panel" />
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_380px]">
        <div className="space-y-4">
          <div className="rounded-lg border border-hairline bg-surface-1 shadow-panel"><ListSkeleton rows={3} /></div>
          <div className="rounded-lg border border-hairline bg-surface-1 shadow-panel"><TableSkeleton rows={3} cols={4} /></div>
        </div>
        <div className="rounded-lg border border-hairline bg-surface-1 shadow-panel"><ListSkeleton rows={6} /></div>
      </div>
    </div>
  );
}
/** 兼容旧调用：加载中一律用骨架，不转圈。 */
export function Loading({ rows = 4 }: { rows?: number; text?: string }) {
  return <ListSkeleton rows={rows} />;
}

export function ErrorBox({ message, onRetry, className }: { message: string; onRetry?: () => void; className?: string }) {
  return (
    <div className={cx("flex items-center justify-between gap-3 rounded-md border border-danger-border bg-danger-bg px-3 py-2 text-body text-danger", className)} role="alert">
      <span>{message}</span>
      {onRetry && (
        <Button size="sm" variant="default" onClick={onRetry}>
          {t("common.retry")}
        </Button>
      )}
    </div>
  );
}

// ---------- 进度条 ----------
/** 6. 刻度进度条：6px 高，surface-3 底，accent 条（完成后 success），每 10% 一道 1px 刻度压在条上，像仪表刻度。不发光、不动画。 */
export function ProgressBar({ value, className, tone = "accent" }: { value: number; className?: string; tone?: "accent" | "success" | "neutral" | "danger" }) {
  const v = Math.max(0, Math.min(100, value));
  const fill = tone === "success" ? "bg-success" : tone === "neutral" ? "bg-hairline-tertiary" : tone === "danger" ? "bg-danger" : "bg-accent";
  return (
    <div className={cx("relative h-1.5 w-full overflow-hidden rounded-full bg-surface-3", className)} role="progressbar" aria-valuenow={v} aria-valuemin={0} aria-valuemax={100}>
      <div className={cx("h-full rounded-full", fill)} style={{ width: `${v}%` }} />
      <span className="progress-ticks" aria-hidden="true" />
    </div>
  );
}

// ---------- 头像 / 执行者 ----------
/** 中文名取最后一个字（名，不是姓），拉丁名取首字母。 */
export const initialOf = (name: string) => {
  const s = name.trim();
  if (!s) return "?";
  const chars = Array.from(s);
  const cjk = chars.filter((c) => /\p{Script=Han}/u.test(c));
  if (cjk.length) return cjk[cjk.length - 1];
  const first = chars.find((c) => /\p{L}|\p{N}/u.test(c)) ?? chars[0];
  return first.toUpperCase();
};
/** 人是圆形（surface-3 底 + ink-muted 首字，中文取名的最后一个字），Agent 是方形（accent-bg 底 + 视窗双点图标）。表格 20，页头 24，侧栏页脚 32。 */
export function Avatar({ name, kind = "member", size = 20, className }: { name: string; kind?: "member" | "agent"; size?: number; className?: string }) {
  const agent = kind === "agent";
  return (
    <span
      aria-hidden="true"
      title={name}
      className={cx("inline-flex shrink-0 items-center justify-center font-medium select-none", agent ? "rounded-xs border border-accent-border bg-accent-bg text-accent-hover" : "rounded-full bg-surface-3 text-ink-muted", className)}
      style={{ width: size, height: size, fontSize: Math.round(size * 0.5), lineHeight: 1 }}
    >
      {agent ? <IconAgent size={size <= 20 ? 12 : size <= 24 ? 14 : 18} strokeWidth={size <= 20 ? 2.4 : 2.1} /> : initialOf(name)}
    </span>
  );
}

/** 执行者名字：头像 + 名字；Agent 带 accent 标签，人不加标签。 */
export function ExecutorName({ executor, empty = "—", avatar = true, className }: { executor: ExecutorRef | null | undefined; empty?: string; avatar?: boolean; className?: string }) {
  if (!executor) return <span className={cx("text-ink-subtle", className)}>{empty}</span>;
  return (
    <span className={cx("inline-flex items-center gap-1.5", className)}>
      {avatar && <Avatar name={executor.name} kind={executor.kind} />}
      <span className="truncate">{executor.name}</span>
      {executor.kind === "agent" && <Tag tone="accent">Agent</Tag>}
    </span>
  );
}

/** 相对时间（3 分钟前），悬停显示完整时间。 */
export function RelativeTime({ iso, className }: { iso: string | null | undefined; className?: string }) {
  if (!iso) return <span className={className}>—</span>;
  return (
    <time dateTime={iso} title={fmtDateTime(iso)} className={className}>
      {fmtRelative(iso)}
    </time>
  );
}

// ---------- 复制 ----------
export function CopyButton({ text, size = "sm", variant = "default", plain = false }: { text: string; size?: "sm" | "md"; variant?: ButtonVariant; plain?: boolean }) {
  const [done, setDone] = useState(false);
  return (
    <Button
      size={size}
      variant={variant}
      icon={plain ? undefined : done ? <IconCheck /> : <IconCopy />}
      onClick={() => {
        void navigator.clipboard?.writeText(text);
        setDone(true);
        window.setTimeout(() => setDone(false), 1500);
      }}
    >
      {done ? t("common.copied") : t("common.copy")}
    </Button>
  );
}
/** 只读的一行代码（code-bg 底：深色是画布、浅色是 surface-2，遥测色）+ 复制按钮：token、邀请链接。 */
export function CopyLine({ text }: { text: string }) {
  return (
    <div className="flex items-start gap-2">
      <code className="telemetry min-w-0 flex-1 rounded-md border border-hairline bg-code-bg px-3 py-2 break-all">{text}</code>
      <CopyButton text={text} size="md" />
    </div>
  );
}
export function Code({ children, className, title }: { children: ReactNode; className?: string; title?: string }) {
  return <code title={title} className={cx("telemetry", className)}>{children}</code>;
}

// ---------- 对话框（确认类）与抽屉（新建 / 编辑） ----------
/**
 * 原生 <dialog> 的开关，带退场：open=false 时先打 data-closing（globals.css 里 100ms 退回原位），等 opacity 过渡结束再 close()。
 * 返回的 shown 在退场期间仍为 true，内容要按 shown 渲染，抽屉才不会空着滑走。
 * 退场中又 open=true：去掉 data-closing，过渡从当前位置折返（不重播）。键盘触发（html[data-kbd]）或 reduced-motion 下 0ms 的过渡不会触发 transitionend，直接关。
 */
function useNativeDialog(open: boolean) {
  const ref = useRef<HTMLDialogElement>(null);
  const [shown, setShown] = useState(open);
  // 打开是同步的：渲染期就把 shown 拉起来，内容和 showModal 同一帧出现
  if (open && !shown) setShown(true);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (open) {
      delete el.dataset.closing;
      if (!el.open) el.showModal();
      return;
    }
    if (!el.open) return;
    el.dataset.closing = "";
    let done = false;
    const finish = () => {
      if (done) return;
      done = true;
      el.close();
      delete el.dataset.closing;
      setShown(false);
    };
    if (keyboardIntent()) {
      // 0ms 过渡不会有 transitionend；下一拍直接关
      const now = window.setTimeout(finish, 0);
      return () => window.clearTimeout(now);
    }
    const onEnd = (e: TransitionEvent) => {
      if (e.target === el && e.propertyName === "opacity") finish();
    };
    el.addEventListener("transitionend", onEnd);
    // 兜底：过渡被禁用 / 0ms 时 transitionend 不会来
    const fallback = window.setTimeout(finish, 200);
    return () => {
      el.removeEventListener("transitionend", onEnd);
      window.clearTimeout(fallback);
    };
  }, [open]);
  return [ref, shown] as const;
}
/** Esc 关闭：拦下浏览器的立即关闭，标记为键盘触发（不动画），再交给 onClose 走正常关闭路径。 */
function cancelDialog(e: SyntheticEvent<HTMLDialogElement>, onClose: () => void) {
  e.preventDefault();
  markKeyboardIntent();
  onClose();
}

/** 居中对话框：12px 圆角，480px，标题 16px/500，动作右对齐、主按钮最右。只用于确认与短表单。 */
export function Dialog({ open, onClose, title, children, footer, width = 480 }: { open: boolean; onClose: () => void; title: ReactNode; children: ReactNode; footer?: ReactNode; width?: number }) {
  const [ref, shown] = useNativeDialog(open);
  return (
    <dialog ref={ref} onClose={onClose} onCancel={(e) => cancelDialog(e, onClose)} className="modal m-auto rounded-lg border border-hairline-strong bg-surface-2 p-0 text-ink shadow-panel" style={{ width, maxWidth: "calc(100vw - 32px)" }}>
      {shown && (
        <div>
          <header className="flex items-center justify-between gap-3 px-5 pt-4 pb-2">
            <h2 className="text-title">{title}</h2>
            <button type="button" onClick={onClose} className="pressable -mr-1.5 rounded-md p-1 text-ink-subtle hover:bg-surface-3 hover:text-ink" aria-label={t("common.close")}>
              <IconClose />
            </button>
          </header>
          <div className="space-y-3 px-5 py-2 text-body">{children}</div>
          {footer && <footer className="flex justify-end gap-2 px-5 pt-3 pb-4">{footer}</footer>}
        </div>
      )}
    </dialog>
  );
}

/** 确认对话框：危险操作用 danger 主按钮。 */
export function ConfirmDialog({ open, title, message, confirmLabel, danger, busy, onConfirm, onClose }: { open: boolean; title: ReactNode; message?: ReactNode; confirmLabel?: string; danger?: boolean; busy?: boolean; onConfirm: () => void; onClose: () => void }) {
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={title}
      width={420}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant={danger ? "danger" : "primary"} disabled={busy} onClick={onConfirm} autoFocus>
            {confirmLabel ?? t("common.confirm")}
          </Button>
        </>
      }
    >
      {message && <p className="text-ink-muted">{message}</p>}
    </Dialog>
  );
}

/** 右侧抽屉：480px，创建 / 编辑表单用这个而不是居中弹窗（不打断当前页面）。头部只有标题 + 一行短提示。 */
export function Drawer({ open, onClose, title, description, children, footer }: { open: boolean; onClose: () => void; title: ReactNode; description?: ReactNode; children: ReactNode; footer?: ReactNode }) {
  const [ref, shown] = useNativeDialog(open);
  return (
    <dialog ref={ref} onClose={onClose} onCancel={(e) => cancelDialog(e, onClose)} className="drawer">
      {shown && (
        <div className="flex h-full flex-col">
          <header className="flex items-start justify-between gap-3 border-b border-hairline px-6 pt-5 pb-4">
            <div className="min-w-0">
              <h2 className="text-title-lg">{title}</h2>
              {description && <p className="mt-1 truncate text-caption text-ink-subtle">{description}</p>}
            </div>
            <button type="button" onClick={onClose} className="pressable -mr-2 rounded-md p-1 text-ink-subtle hover:bg-surface-3 hover:text-ink" aria-label={t("common.close")}>
              <IconClose />
            </button>
          </header>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">{children}</div>
          {footer && <footer className="flex justify-end gap-2 border-t border-hairline px-6 py-3">{footer}</footer>}
        </div>
      )}
    </dialog>
  );
}

// ---------- 表格 ----------
/** 兼容旧调用；样式由 .tbl 统一给。数字列加 className="num"。 */
export const thCls = "";
export const tdCls = "";
export function Table({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cx("tbl-wrap overflow-x-auto", className)}>
      <table className="tbl">{children}</table>
    </div>
  );
}

export function DescList({ items, className }: { items: Array<[string, ReactNode]>; className?: string }) {
  return (
    <dl className={cx("grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-body", className)}>
      {items.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="whitespace-nowrap text-ink-subtle">{k}</dt>
          <dd className="min-w-0">{v}</dd>
        </div>
      ))}
    </dl>
  );
}

/** 任务标题链接：一行、超出省略；ID 不再印在列表里（只在详情页面包屑和侧栏的"ID"行）。 */
export function TaskLink({ id, title, className, inline = false }: { id: string; title: string; className?: string; inline?: boolean }) {
  if (inline) {
    return (
      <Link href={`/tasks/${encodeURIComponent(id)}/`} className={cx("hover:text-accent-hover", className)}>
        {title}
      </Link>
    );
  }
  return (
    <span className={cx("block min-w-0", className)}>
      <Link href={`/tasks/${encodeURIComponent(id)}/`} className="block truncate font-medium text-ink hover:text-accent-hover" title={title}>
        {title}
      </Link>
    </span>
  );
}

/** 详情侧栏里的 ID 行：代码字体 + 复制。 */
export function IdLine({ id }: { id: string }) {
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-x-1 gap-y-0.5">
      <Code className="min-w-0 truncate text-caption" title={id}>{id}</Code>
      <CopyButton text={id} size="sm" variant="ghost" plain />
    </span>
  );
}

export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="eyebrow inline-flex h-5 min-w-5 items-center justify-center rounded-xs border border-hairline-strong bg-surface-2 px-1.5 text-ink-muted">{children}</kbd>;
}

// ---------- 页头统计芯片（点击即过滤） ----------
export interface StatChip {
  key: string;
  label: string;
  value: number | string;
  tone?: "danger" | "warning" | "accent";
}
// ---------- 状态指示灯 ----------
export type LedTone = "neutral" | "accent" | "success" | "online" | "warning" | "danger" | "dark";
const LED: Record<LedTone, string> = { neutral: "bg-neutral", accent: "bg-accent led-active", success: "bg-success", online: "bg-success led-online", warning: "bg-warning", danger: "bg-danger", dark: "bg-hairline-tertiary" };
/** 5. 8px 圆点。accent（进行中）带 accent-glow 微光并以 2s 周期呼吸；online（在线 Agent）success 常亮微光；其他不发光。 */
export function StatusLED({ tone = "neutral", className }: { tone?: LedTone; className?: string }) {
  return <span className={cx("led", LED[tone], className)} aria-hidden="true" />;
}
/**
 * 统计条：一行 32px 高的芯片。数字 14px/500，后面跟说明色的标签；语义芯片前有 6px 色点。
 * total 给出时第一枚是"全部"（选中即清空筛选）；点芯片即过滤，再点一次取消。同一组件用于所有页面。
 */
export function StatChips({ items, value, onChange, total, className }: { items: StatChip[]; value: string | null; onChange?: (key: string | null) => void; total?: number | string; className?: string }) {
  const chip = (key: string | null, label: string, val: number | string, tone?: StatChip["tone"]) => {
    const active = value === key;
    return (
      <button
        key={key ?? "__all"}
        type="button"
        aria-pressed={active}
        onClick={() => onChange?.(active && key !== null ? null : key)}
        className={cx(
          "inline-flex h-8 items-center gap-2 rounded-md border px-2.5 text-body whitespace-nowrap",
          onChange ? "pressable" : "cursor-default",
          active ? "border-accent-border bg-accent-bg text-accent-hover" : "border-hairline bg-surface-1 text-ink hover:bg-surface-2",
        )}
      >
        {tone && <StatusLED tone={tone} />}
        <span className="font-medium tabular-nums"><Readout value={val} /></span>
        <span className={active ? "text-accent-hover" : "text-ink-subtle"}>{label}</span>
      </button>
    );
  };
  return (
    <div className={cx("flex flex-wrap gap-2", className)} role="group">
      {total !== undefined && chip(null, t("common.all"), total)}
      {items.map((c) => chip(c.key, c.label, c.value, c.tone))}
    </div>
  );
}

/** 紧凑 KPI 面板：一个 20px/500 的数字 + 12px 说明，没有标题栏，但保留 HUD 角标；数字变化时淡入。给首页侧栏的小面板用。 */
export function KpiPanel({ label, value, caption, href, className }: { label: ReactNode; value: ReactNode; caption?: ReactNode; href?: string; className?: string }) {
  const body = (
    <>
      <HudCorners />
      <span className="block text-caption text-ink-subtle">{label}</span>
      <span className="mt-1 block text-title-lg tabular-nums text-ink"><Readout value={value} /></span>
      {caption && <span className="mt-0.5 block text-caption text-ink-subtle">{caption}</span>}
    </>
  );
  const cls = cx("hud block rounded-lg border border-hairline bg-surface-1 px-4 py-3 shadow-panel", className);
  if (href) {
    return (
      <Link href={href} className={cx(cls, "transition-colors hover:bg-surface-2")}>
        {body}
      </Link>
    );
  }
  return <div className={cls}>{body}</div>;
}

/** 分段控件（日/周/月、分组、主题）。block = 占满一行、各段等宽（侧边栏页脚的主题切换）。 */
export function Segmented<T extends string>({ value, options, onChange, size = "md", className, label, block = false, "aria-label": ariaLabel }: { value: T; options: Array<[T, string]>; onChange: (v: T) => void; size?: "sm" | "md"; className?: string; label?: string; block?: boolean; "aria-label"?: string }) {
  return (
    <span className={cx(block ? "flex w-full" : "inline-flex", "items-center gap-2", className)}>
      {label && <span className="text-caption text-ink-subtle">{label}</span>}
      <span className={cx("overflow-hidden rounded-md border border-hairline-strong bg-surface-1", block ? "flex w-full" : "inline-flex", size === "sm" ? "h-7" : "h-8")} role="radiogroup" aria-label={ariaLabel}>
        {options.map(([k, title]) => (
          <button key={k} type="button" role="radio" aria-checked={value === k} onClick={() => onChange(k)} className={cx("pressable px-2.5 text-[13px] leading-none first:rounded-l-md last:rounded-r-md", block && "min-w-0 flex-1 truncate", value === k ? "bg-accent-bg font-medium text-accent-hover" : "text-ink-subtle hover:bg-surface-2 hover:text-ink")}>
            {title}
          </button>
        ))}
      </span>
    </span>
  );
}
