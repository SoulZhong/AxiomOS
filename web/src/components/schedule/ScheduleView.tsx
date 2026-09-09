"use client";
import Link from "next/link";
import { useMemo, useState, type ReactNode } from "react";
import { api, type ScheduleItem } from "@/lib/api";
import { addDays, diffDays, fmtDate, monthName, startOfWeek, toISODate, today } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { t, useLocale, type Key } from "@/lib/i18n";
import { usePersisted } from "@/lib/usePersisted";
import { useSession } from "@/components/AppShell";
import { SCOPE_ALL, useScopeState } from "@/lib/useScope";
import { IconChevronLeft, IconChevronRight } from "@/components/icons";
import { Button, ErrorBox, ListSkeleton, Segmented, Select, cx } from "@/components/ui";

/*
 * 日程（ADR 0032，DESIGN.md §27）：目标、任务、里程碑与外部日历的一种看法。
 * 三档粒度写死：日 / 周 / 月；周从周一起。四类各有画法：
 *   任务 = 按状态着色的条（只有截止日的画成一枚旗标）；目标 = 淡一级的底条；里程碑 = 菱形；会议 = 带来源的块（别人的只画「忙」）。
 * 「我的 / 团队」切换：团队视图是整个团队的安排合在一张表里（每条前面带谁的名字），团队从组织树上任选一个节点，
 * 默认是自己所属的最近那个团队。这里不能新建、不能拖动——改日期去任务页、路线图或外部日历。
 */

type Scale = "day" | "week" | "month";
type Who = "me" | "team";
const SCALES: Scale[] = ["day", "week", "month"];
const isScale = (v: unknown): v is Scale => typeof v === "string" && (SCALES as string[]).includes(v);

const HOUR_START = 7;
const HOUR_END = 21;
const HOUR_H = 40;

export function ScheduleView() {
  const { session } = useSession();
  const { locale } = useLocale();
  const [scale, setScale] = usePersisted<Scale>("axiomos.schedule.scale", "week", (v) => (isScale(v) ? v : undefined));
  const [who, setWho] = useState<Who>("me");
  const [teamID, setTeamID] = useState<string>("");
  const [anchor, setAnchor] = useState<Date>(() => today());
  // 组织树上能看到的团队（带层级缩进，来自会话的范围档位）；没有档位信息时退回自己所在的团队
  const { options } = useScopeState();
  const tree = useMemo(() => {
    const teams = options.filter((o) => o.id !== SCOPE_ALL);
    const base = Math.min(...teams.map((o) => o.depth));
    const fromScope = teams.map((o) => ({ id: o.id as string, name: o.title, depth: o.depth - base }));
    if (fromScope.length > 0) return fromScope;
    return (session?.teams ?? []).map((x) => ({ id: x.id, name: x.name, depth: 0 }));
  }, [options, session?.teams]);
  // 默认团队：自己直接所属的、层级最深的那个（组织树上离自己最近的节点）；不属于任何团队就取树上第一个
  const defaultTeam = useMemo(() => {
    const mine = (session?.my_team_ids ?? []).filter((id) => tree.some((x) => x.id === id));
    const depthOf = (id: string) => tree.find((x) => x.id === id)?.depth ?? -1;
    return [...mine].sort((a, b) => depthOf(b) - depthOf(a))[0] ?? tree[0]?.id ?? "";
  }, [session?.my_team_ids, tree]);
  const team = teamID || defaultTeam;
  const effScale: Scale = scale;

  const range = useMemo(() => rangeOf(effScale, anchor), [effScale, anchor]);
  const whoParam = who === "team" ? team : "me";
  const data = useLoad(() => api.schedule.get({ from: toISODate(range.from), to: toISODate(range.to), who: whoParam || undefined }), [range.from.getTime(), range.to.getTime(), whoParam]);

  const shift = (dir: -1 | 1) => setAnchor((d) => (effScale === "day" ? addDays(d, dir) : effScale === "week" ? addDays(d, 7 * dir) : new Date(d.getFullYear(), d.getMonth() + dir, 1)));
  const title = titleOf(effScale, range, locale);
  const items = data.data?.items ?? [];
  const members = data.data?.members ?? [];
  const meID = session?.member.id ?? "";

  return (
    <div className="sc" data-scale={effScale} data-who={who}>
      <div className="sc-toolbar">
        <div className="sc-nav">
          <Button size="sm" variant="ghost" icon={<IconChevronLeft />} onClick={() => shift(-1)} aria-label={t("schedule.prev")} />
          <Button size="sm" onClick={() => setAnchor(today())}>{t("schedule.today")}</Button>
          <Button size="sm" variant="ghost" icon={<IconChevronRight />} onClick={() => shift(1)} aria-label={t("schedule.next")} />
          <h2 className="sc-title">{title}</h2>
        </div>
        <div className="sc-tools">
          {who === "team" && tree.length > 1 && (
            <Select value={team} onChange={(e) => setTeamID(e.target.value)} aria-label={t("schedule.teamPick")} className="w-[200px]">
              {tree.map((x) => <option key={x.id} value={x.id}>{"\u00a0\u00a0".repeat(x.depth)}{x.name}</option>)}
            </Select>
          )}
          {tree.length > 0 && (
            <Segmented size="sm" value={who} options={[["me", t("schedule.scope.mine")], ["team", t("schedule.scope.team")]]} onChange={setWho} aria-label={t("schedule.scope")} />
          )}
          <Segmented size="sm" value={effScale} options={SCALES.map((k) => [k, t(`schedule.view.${k}` as Key)])} onChange={setScale} aria-label={t("schedule.scaleLabel")} />
        </div>
      </div>
      {data.error && !data.data ? (
        <ErrorBox message={data.error} onRetry={data.reload} />
      ) : !data.data ? (
        <div className="sc-skeleton"><ListSkeleton rows={6} /></div>
      ) : (
        <>
          <Legend sources={data.data.sources} members={who === "team" ? members : undefined} />
          {/* 没有安排也照样画格子——日历本身就是信息（今天在哪、周末在哪）；空只提示一句 */}
          {items.length === 0 && <p className="sc-empty" role="status">{t(who === "team" && members.length === 0 ? "schedule.emptyTeam" : effScale === "day" ? "schedule.emptyDay" : effScale === "week" ? "schedule.emptyWeek" : "schedule.emptyMonth")}</p>}
          {effScale === "month"
            ? <MonthGrid range={range} anchor={anchor} items={items} meID={meID} names={who === "team" ? nameMap(members) : null} />
            : <WeekGrid range={range} items={items} meID={meID} names={who === "team" ? nameMap(members) : null} />}
        </>
      )}
    </div>
  );
}

// ---------- 区间与标题 ----------

function rangeOf(scale: Scale, anchor: Date): { from: Date; to: Date } {
  if (scale === "day") return { from: anchor, to: anchor };
  if (scale === "week") {
    const from = startOfWeek(anchor);
    return { from, to: addDays(from, 6) };
  }
  const first = new Date(anchor.getFullYear(), anchor.getMonth(), 1);
  const last = new Date(anchor.getFullYear(), anchor.getMonth() + 1, 0);
  return { from: startOfWeek(first), to: addDays(startOfWeek(last), 6) };
}

function titleOf(scale: Scale, range: { from: Date; to: Date }, locale: string): string {
  if (scale === "day") return fmtDate(toISODate(range.from), true) + " " + t(`gantt.weekday.${range.from.getDay()}` as Key);
  if (scale === "week") return `${fmtDate(toISODate(range.from), true)} – ${fmtDate(toISODate(range.to), range.to.getFullYear() !== range.from.getFullYear())}`;
  const mid = addDays(range.from, 10);
  return locale === "en-US" ? `${monthName(mid.getMonth() + 1)} ${mid.getFullYear()}` : `${mid.getFullYear()}年${monthName(mid.getMonth() + 1)}`;
}

// ---------- 分桶 ----------

/** 一条日程项落在哪些天（含首尾）；带时刻的会议只落在开始那天 */
function daysOf(it: ScheduleItem): [string, string] {
  if (it.start_date && it.end_date) return [it.start_date, it.end_date];
  const d = it.date ?? it.start_date ?? "";
  return [d, d];
}
function coversDay(it: ScheduleItem, day: string): boolean {
  const [a, b] = daysOf(it);
  return a <= day && day <= b;
}
function timed(it: ScheduleItem): boolean {
  return it.kind === "event" && !it.all_day && !!it.starts_at;
}

// ---------- 画法 ----------

function hrefOf(it: ScheduleItem): string | null {
  switch (it.kind) {
    case "task": return `/tasks/${encodeURIComponent(it.id)}/`;
    case "goal": return `/goals/${encodeURIComponent(it.id)}/`;
    case "milestone": return it.goal_id ? `/goals/${encodeURIComponent(it.goal_id)}/` : null;
    case "event": return it.url || null;
  }
}

function toneOf(it: ScheduleItem): string {
  if (it.kind === "task") {
    if (it.overdue) return "danger";
    const l = it.state?.label;
    return l === "active" ? "accent" : l === "waiting" ? "warning" : l === "terminal_success" ? "success" : "neutral";
  }
  if (it.kind === "milestone") return it.milestone_status === "reached" ? "success" : it.milestone_status === "overdue" ? "danger" : "neutral";
  if (it.kind === "goal") return "goal";
  return it.title_hidden ? "busy" : "event";
}

function timeText(it: ScheduleItem): string {
  if (!it.starts_at) return "";
  const s = new Date(it.starts_at);
  const e = it.ends_at ? new Date(it.ends_at) : null;
  const hm = (d: Date) => `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  return e ? `${hm(s)}–${hm(e)}` : hm(s);
}

function nameMap(members: Array<{ id: string; name: string }>): Record<string, string> {
  return Object.fromEntries(members.map((m) => [m.id, m.name]));
}

/** 一枚日程项：任务条 / 目标底条 / 里程碑菱形 / 会议块。compact 时只留一行；names 非空（团队视图）时前面带谁的名字 */
function Chip({ it, day, meID, compact, names }: { it: ScheduleItem; day: string; meID: string; compact?: boolean; names?: Record<string, string> | null }) {
  const href = hrefOf(it);
  const [a, b] = daysOf(it);
  const first = a === day;
  const last = b === day;
  const who = names ? it.member_name ?? names[it.member_id] ?? "" : "";
  const label = it.kind === "task" && it.number ? `#${it.number} ${it.title}` : it.title;
  const tip = (who ? who + " · " : "") + (it.kind === "task" ? `#${it.number} ${it.title}${it.state ? " · " + it.state.title : ""}` : it.kind === "event" && it.starts_at && !it.all_day ? `${timeText(it)} ${it.title}` : it.title);
  const body: ReactNode = (
    <>
      {it.kind === "milestone" && <i className="sc-diamond" aria-hidden="true" />}
      {it.kind === "task" && it.deadline && <i className="sc-flag" aria-hidden="true" />}
      {it.kind === "event" && !it.all_day && !compact && <span className="sc-time">{timeText(it)}</span>}
      {who && <span className="sc-who">{who}</span>}
      <span className="sc-chip-label">{label}</span>
      {it.kind === "event" && it.provider && !it.title_hidden && !compact && <span className="sc-src">{providerShort(it.provider)}</span>}
    </>
  );
  const cls = cx("sc-chip", `sc-chip-${toneOf(it)}`, `sc-kind-${it.kind}`, !first && "sc-chip-cont", !last && "sc-chip-more", it.member_id !== meID && "sc-chip-theirs");
  if (!href) return <span className={cls} title={tip}>{body}</span>;
  const external = it.kind === "event";
  return external
    ? <a className={cls} href={href} target="_blank" rel="noreferrer" title={tip}>{body}</a>
    : <Link className={cls} href={href} title={tip}>{body}</Link>;
}

function providerShort(p: string): string {
  return t(`schedule.source.${p}` as Key) === `schedule.source.${p}` ? p : t(`schedule.source.${p}` as Key);
}

/** 图例：四类画法 + 接上的外部日历来源 */
function Legend({ sources, members }: { sources: string[]; members?: Array<{ id: string; name: string }> }) {
  return (
    <div className="sc-legend" aria-hidden="true">
      {members && <span className="sc-legend-members">{t("schedule.teamMembers", { n: members.length })}{members.length > 0 && <span className="text-ink-tertiary">（{members.slice(0, 8).map((m) => m.name).join("、")}{members.length > 8 ? "…" : ""}）</span>}</span>}
      <span><i className="sc-swatch sc-chip-accent" />{t("schedule.legend.task")}</span>
      <span><i className="sc-swatch sc-chip-goal" />{t("schedule.legend.goal")}</span>
      <span><i className="sc-diamond" />{t("schedule.legend.milestone")}</span>
      <span><i className="sc-swatch sc-chip-event" />{t("schedule.legend.event")}{sources.length > 0 && <span className="text-ink-tertiary">（{sources.map(providerShort).join(" · ")}）</span>}</span>
    </div>
  );
}

// ---------- 周 / 日 ----------

function WeekGrid({ range, items, meID, names }: { range: { from: Date; to: Date }; items: ScheduleItem[]; meID: string; names: Record<string, string> | null }) {
  const days = useMemo(() => Array.from({ length: diffDays(range.from, range.to) + 1 }, (_, i) => addDays(range.from, i)), [range]);
  const todayISO = toISODate(today());
  const hours = Array.from({ length: HOUR_END - HOUR_START }, (_, i) => HOUR_START + i);
  const allDay = (d: string) => items.filter((it) => !timed(it) && coversDay(it, d));
  const timedOf = (d: string) => items.filter((it) => timed(it) && coversDay(it, d));
  return (
    <div className="sc-week" style={{ "--cols": days.length } as React.CSSProperties}>
      <div className="sc-head sc-corner" />
      {days.map((d) => {
        const iso = toISODate(d);
        return (
          <div key={iso} className={cx("sc-head", iso === todayISO && "sc-today", (d.getDay() === 0 || d.getDay() === 6) && "sc-weekend")}>
            <span className="sc-wd">{t(`gantt.weekday.${d.getDay()}` as Key)}</span>
            <span className="sc-dn">{d.getDate()}</span>
          </div>
        );
      })}
      <div className="sc-band-label">{t("schedule.allDay")}</div>
      {days.map((d) => {
        const iso = toISODate(d);
        const list = allDay(iso);
        return (
          <div key={iso} className={cx("sc-band", iso === todayISO && "sc-today")}>
            {list.map((it) => <Chip key={it.kind + it.id} it={it} day={iso} meID={meID} names={names} />)}
          </div>
        );
      })}
      <div className="sc-hours">
        {hours.map((h) => <div key={h} className="sc-hour" style={{ height: HOUR_H }}><span>{String(h).padStart(2, "0")}:00</span></div>)}
      </div>
      {days.map((d) => {
        const iso = toISODate(d);
        return (
          <div key={iso} className={cx("sc-col", iso === todayISO && "sc-today")} style={{ height: hours.length * HOUR_H }}>
            {hours.map((h) => <div key={h} className="sc-line" style={{ top: (h - HOUR_START) * HOUR_H }} />)}
            {timedOf(iso).map((it) => {
              const s = new Date(it.starts_at!);
              const e = it.ends_at ? new Date(it.ends_at) : new Date(s.getTime() + 3600000);
              const top = Math.max(0, (s.getHours() + s.getMinutes() / 60 - HOUR_START) * HOUR_H);
              const bottom = Math.min(hours.length * HOUR_H, (e.getHours() + e.getMinutes() / 60 - HOUR_START) * HOUR_H);
              const h = Math.max(18, bottom - top);
              return (
                <div key={it.id} className="sc-timed" style={{ top, height: h }}>
                  <Chip it={it} day={iso} meID={meID} names={names} />
                </div>
              );
            })}
            {iso === todayISO && <NowLine />}
          </div>
        );
      })}
    </div>
  );
}

function NowLine() {
  const n = new Date();
  const y = (n.getHours() + n.getMinutes() / 60 - HOUR_START) * HOUR_H;
  if (y < 0 || y > (HOUR_END - HOUR_START) * HOUR_H) return null;
  return <div className="sc-now" style={{ top: y }} aria-hidden="true" />;
}

// ---------- 月 ----------

function MonthGrid({ range, anchor, items, meID, names }: { range: { from: Date; to: Date }; anchor: Date; items: ScheduleItem[]; meID: string; names: Record<string, string> | null }) {
  const days = useMemo(() => Array.from({ length: diffDays(range.from, range.to) + 1 }, (_, i) => addDays(range.from, i)), [range]);
  const todayISO = toISODate(today());
  const MAX = 4;
  return (
    <div className="sc-month">
      {[1, 2, 3, 4, 5, 6, 0].map((wd) => <div key={wd} className="sc-head">{t(`gantt.weekday.${wd}` as Key)}</div>)}
      {days.map((d) => {
        const iso = toISODate(d);
        const list = items.filter((it) => coversDay(it, iso));
        const shown = list.slice(0, MAX);
        return (
          <div key={iso} className={cx("sc-cell", iso === todayISO && "sc-today", d.getMonth() !== anchor.getMonth() && "sc-other", (d.getDay() === 0 || d.getDay() === 6) && "sc-weekend")}>
            <span className="sc-dn">{d.getDate() === 1 ? `${monthName(d.getMonth() + 1)}${d.getDate()}` : d.getDate()}</span>
            {shown.map((it) => <Chip key={it.kind + it.id} it={it} day={iso} meID={meID} compact names={names} />)}
            {list.length > MAX && <span className="sc-more">+{list.length - MAX}</span>}
          </div>
        );
      })}
    </div>
  );
}
