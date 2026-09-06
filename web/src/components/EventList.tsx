"use client";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { api, type Event } from "@/lib/api";
import { fmtDate, fmtDateTime, parseDate } from "@/lib/format";
import { eventSummary } from "@/lib/fieldChange";
import { eventTitle } from "@/lib/terms";
import { t } from "@/lib/i18n";
import { IconAgent, IconApprove, IconBacklog, IconBlocks, IconComment, IconEdit, IconExternal, IconGoal, IconMilestone, IconRun, IconTask, IconTaskActive, IconUsage } from "./icons";
import { Avatar, Button, Empty, TaskLink, cx } from "./ui";

/*
 * 舰桥日志（web/DESIGN.md「舰内系统 v3」§3）：动态流的仪表化呈现。
 * 顶行 `● 实时 · 最近同步 12s 前`；每行 = 等宽时间戳（hh:mm:ss）+ 事件类型 LED 色点 + 执行者芯片 + 正文；跨天插一条日期线。
 * 语义不变：只增不改、按时间倒序、默认 10 条 + 「查看全部 (N)」、同一执行者 5 分钟内的连续动作合并（芯片只在组首出现）、
 * 本任务页不重复任务名后缀。
 * 自己轮询：标签页可见时每 30s 拉一次 GET /events（默认与 currentTaskId 同范围），有新事件时 LED 闪一次、新行 200ms 淡入。
 */

const LIMIT = 10;
const GROUP_WINDOW_MS = 5 * 60 * 1000;
const POLL_MS = 30_000;

/** 事件类型 → LED 色：开始 / 结束 / 用量 = accent；新建 = success；移除、退回 = warning；其余中性。 */
type Led = "accent" | "success" | "warning" | "neutral";
const LED_OF: Partial<Record<Event["kind"], Led>> = {
  RunStarted: "accent",
  RunEnded: "accent",
  UsageReported: "accent",
  TaskTransitioned: "accent",
  TaskCreated: "success",
  GoalCreated: "success",
  MilestoneCreated: "success",
  MilestoneReached: "success",
  MilestoneDeleted: "warning",
  AgentRegistered: "success",
  RelationRemoved: "warning",
  ProposalCreated: "warning",
  ProposalApproved: "success",
  TaskSentToBacklog: "warning",
  AgentRemoved: "warning",
};
const LED_CLS: Record<Led, string> = { accent: "bg-accent", success: "bg-success", warning: "bg-warning", neutral: "bg-hairline-tertiary" };
const LED_INK: Record<Led, string> = { accent: "text-accent-hover", success: "text-success", warning: "text-warning", neutral: "text-ink-subtle" };
/** 事件类型 → 签名图标（提案 §二：动态 = 磁带，行内按类型换成对应概念的图标；没有明显对应的仍用 LED 色点）。 */
function kindIcon(kind: Event["kind"]): ReactNode {
  const p = { size: 12 } as const;
  switch (kind) {
    case "RunStarted":
    case "RunEnded":
      return <IconRun {...p} />;
    case "UsageReported":
      return <IconUsage {...p} />;
    case "TaskCreated":
      return <IconTask {...p} />;
    case "TaskTransitioned":
      return <IconTaskActive {...p} />;
    case "TaskClaimed":
    case "TaskSentToBacklog":
      return <IconBacklog {...p} />;
    case "TasksLinked":
    case "RelationRemoved":
      return <IconBlocks {...p} />;
    case "GoalCreated":
    case "GoalUpdated":
    case "GoalDeleted":
      return <IconGoal {...p} />;
    case "MilestoneCreated":
    case "MilestoneUpdated":
    case "MilestoneReached":
    case "MilestoneUnreached":
    case "MilestoneDeleted":
      return <IconMilestone {...p} />;
    case "AgentRegistered":
    case "AgentUpdated":
    case "AgentRemoved":
      return <IconAgent {...p} />;
    case "CommentAdded":
    case "NoteAdded":
      return <IconComment size={12} />;
    case "ProposalCreated":
    case "ProposalApproved":
    case "ProposalRejected":
      return <IconApprove {...p} />;
    case "ArtifactAttached":
      return <IconExternal size={12} />;
    // 就地编辑（DESIGN.md §15）：每个字段一条，铅笔；句子由服务端给（eventSummary 优先用 summary）
    case "TaskFieldChanged":
    case "GoalFieldChanged":
      return <IconEdit {...p} />;
    default:
      return null;
  }
}

interface Group {
  key: string;
  actor: Event["actor"];
  events: Event[];
}

/** 同一执行者 5 分钟内的连续动态并成一组：芯片只在组首。 */
function groupEvents(events: Event[]): Group[] {
  const groups: Group[] = [];
  for (const e of events) {
    const last = groups[groups.length - 1];
    const prev = last?.events[last.events.length - 1];
    const sameActor = last && (last.actor?.id ?? null) === (e.actor?.id ?? null);
    const a = prev ? parseDate(prev.created_at)?.getTime() : undefined;
    const b = parseDate(e.created_at)?.getTime();
    const close = a !== undefined && b !== undefined && Math.abs(a - b) <= GROUP_WINDOW_MS;
    if (last && sameActor && close) {
      last.events.push(e);
    } else {
      groups.push({ key: e.id, actor: e.actor, events: [e] });
    }
  }
  return groups;
}

const clock = (iso: string) => {
  const d = parseDate(iso);
  if (!d) return "--:--:--";
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}:${String(d.getSeconds()).padStart(2, "0")}`;
};
const dayKey = (iso: string) => {
  const d = parseDate(iso);
  return d ? `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}` : "";
};

/** 页面可见性（轮询只在可见时跑）。 */
function useVisible() {
  const [visible, setVisible] = useState(true);
  useEffect(() => {
    const on = () => setVisible(document.visibilityState === "visible");
    on();
    document.addEventListener("visibilitychange", on);
    return () => document.removeEventListener("visibilitychange", on);
  }, []);
  return visible;
}

export interface EventPoll {
  task?: string;
  limit?: number;
  /** 拉回来的事件再过一遍（目标详情页只留本目标的）。 */
  filter?: (e: Event) => boolean;
}

/**
 * events：父组件已经加载的一批（首屏不等轮询）。poll：轮询范围，默认与 currentTaskId 同范围；传 false 关闭轮询。
 * onNew：轮询拉到挂载之后产生的新事件时回调（过滤之前的原始行；任务详情页据此在「自动解除前置」时重新加载任务，播断链）。
 */

/** 动态行左边已经有执行者芯片，句子开头再重复一遍名字就多余了：去掉与芯片相同的开头。 */
function withoutActor(text: string, actor?: string): string {
  if (!actor || !text.startsWith(actor)) return text;
  const rest = text.slice(actor.length);
  if (rest === "" || /^[\s，,：:]/.test(rest)) return rest.replace(/^[\s，,：:]+/, "");
  return text;
}

export function EventList({ events, showTask = true, currentTaskId, poll, onNew }: { events: Event[]; showTask?: boolean; currentTaskId?: string | null; poll?: EventPoll | false; onNew?: (added: Event[]) => void }) {
  const [expanded, setExpanded] = useState(false);
  const [live, setLive] = useState<Event[]>(events);
  // null = 刚拿到父组件的一批，还没走过一秒（渲染期不能 Date.now()，由秒针补上）
  const [syncedAt, setSyncedAt] = useState<number | null>(null);
  const [blink, setBlink] = useState(0);
  const [now, setNow] = useState<number>(() => Date.now());
  const [fresh, setFresh] = useState<Set<string>>(() => new Set());
  const visible = useVisible();

  // 父组件重新加载（reload）时以它为准；这批不算"新"，不淡入。
  // 按 id 序列比较（调用方每次渲染传新数组也不会反复重置），变化时在渲染期直接换状态（React 的 "adjusting state during render"）。
  const signature = events.map((e) => e.id).join("\n");
  const [prevSignature, setPrevSignature] = useState(signature);
  if (prevSignature !== signature) {
    setPrevSignature(signature);
    setLive(events);
    setFresh(new Set());
    setSyncedAt(null);
  }

  // 轮询里要和"当前这批"比对，用 ref 镜像一份最新的 live（在 effect 里写，不在渲染期碰 ref）
  const liveRef = useRef(live);
  useEffect(() => {
    liveRef.current = live;
  }, [live]);

  const pollCfg: EventPoll | false = poll === undefined ? { task: currentTaskId ?? undefined, limit: Math.max(events.length, 20) } : poll;
  const polling = !!pollCfg;
  const pollTask = pollCfg ? pollCfg.task : undefined;
  const pollLimit = pollCfg ? pollCfg.limit : undefined;
  const filter = pollCfg ? pollCfg.filter : undefined;
  const filterRef = useRef(filter);
  const onNewRef = useRef(onNew);
  const mountedAt = useRef(0);
  const seenRaw = useRef<Set<string>>(new Set());
  useEffect(() => {
    mountedAt.current = Date.now();
  }, []);
  useEffect(() => {
    filterRef.current = filter;
    onNewRef.current = onNew;
  }, [filter, onNew]);

  useEffect(() => {
    if (!polling || !visible) return;
    let alive = true;
    const tick = async () => {
      try {
        let rows = await api.events.list({ task: pollTask, limit: pollLimit });
        if (!alive) return;
        const since = mountedAt.current - 1000;
        const rawAdded = rows.filter((e) => !seenRaw.current.has(e.id) && (parseDate(e.created_at)?.getTime() ?? 0) >= since);
        for (const e of rows) seenRaw.current.add(e.id);
        if (rawAdded.length) onNewRef.current?.(rawAdded);
        const f = filterRef.current;
        if (f) rows = rows.filter(f);
        const known = new Set(liveRef.current.map((e) => e.id));
        const added = rows.filter((e) => !known.has(e.id));
        if (added.length) {
          setFresh(new Set(added.map((e) => e.id)));
          setLive(rows);
          setBlink((b) => b + 1);
        }
        setSyncedAt(Date.now());
      } catch {
        // 轮询失败不打扰：保留上一批，等下一拍
      }
    };
    const id = window.setInterval(() => void tick(), POLL_MS);
    return () => {
      alive = false;
      window.clearInterval(id);
    };
  }, [visible, polling, pollTask, pollLimit]);

  // "最近同步 12s 前"每秒走一格；标签页隐藏时停
  useEffect(() => {
    if (!visible) return;
    const id = window.setInterval(() => {
      const ms = Date.now();
      setNow(ms);
      setSyncedAt((s) => s ?? ms);
    }, 1000);
    return () => window.clearInterval(id);
  }, [visible]);

  const secs = syncedAt === null ? 0 : Math.max(0, Math.round((now - syncedAt) / 1000));
  const synced = secs < 60 ? t("log.syncedAgo", { n: secs }) : t("log.syncedMinutes", { n: Math.round(secs / 60) });
  const head = (
    <div className="log-head">
      <span key={blink} className="log-led" data-blink={blink > 0 ? "" : undefined} aria-hidden="true" />
      <span className="eyebrow text-ink-subtle">
        {t("log.live")} · <span className="text-telemetry tabular-nums normal-case">{synced}</span>
      </span>
    </div>
  );

  if (!live.length) {
    return (
      <div>
        {head}
        <Empty text={t("events.empty")} illustration={false} className="py-6" />
      </div>
    );
  }
  const shown = expanded ? live : live.slice(0, LIMIT);
  const rows: ReactNode[] = [];
  let lastDay = "";
  for (const g of groupEvents(shown)) {
    g.events.forEach((e, j) => {
      const day = dayKey(e.created_at);
      if (day !== lastDay) {
        lastDay = day;
        rows.push(
          <li key={`day-${e.id}`} className="log-day" aria-hidden="true">
            {fmtDate(e.created_at)}
          </li>,
        );
      }
      const suffix = showTask && e.task_id && e.task_title && e.task_id !== currentTaskId;
      const led = LED_OF[e.kind] ?? "neutral";
      const icon = kindIcon(e.kind);
      rows.push(
        <li key={e.id} className="log-row" data-new={fresh.has(e.id) ? "" : undefined}>
          <time dateTime={e.created_at} title={fmtDateTime(e.created_at)} className="log-ts">
            {clock(e.created_at)}
          </time>
          {icon ? (
            <span className={cx("log-kind", LED_INK[led])} title={eventTitle(e.kind)} aria-hidden="true">{icon}</span>
          ) : (
            <span className={cx("log-dot", LED_CLS[led])} title={eventTitle(e.kind)} aria-hidden="true" />
          )}
          <span className="min-w-0 text-body text-ink">
            {j === 0 && (
              <span className="log-chip" data-kind={g.actor ? g.actor.kind : "system"}>
                {g.actor && <Avatar name={g.actor.name} kind={g.actor.kind} size={16} />}
                {g.actor ? g.actor.name : t("common.system")}
              </span>
            )}
            {g.events.length > 1 && <span className="mr-1.5 text-caption text-ink-subtle">{eventTitle(e.kind)}</span>}
            {withoutActor(eventSummary(e), g.actor?.name)}
            {suffix && (
              <span className="ml-2 text-ink-muted">
                · <TaskLink id={e.task_id!} title={e.task_title!} inline className="text-ink-muted" />
              </span>
            )}
          </span>
        </li>,
      );
    });
  }
  return (
    <div>
      {head}
      <ol>{rows}</ol>
      {live.length > LIMIT && (
        <div className="mt-3 border-t border-hairline pt-3">
          <Button variant="ghost" size="sm" onClick={() => setExpanded((v) => !v)} aria-expanded={expanded}>
            {expanded ? t("events.showLess") : t("events.showAll", { n: live.length })}
          </Button>
        </div>
      )}
    </div>
  );
}
