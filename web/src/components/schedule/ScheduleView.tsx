"use client";
import Link from "next/link";
import { useMemo, useState, type ReactNode } from "react";
import { api, type ScheduleItem } from "@/lib/api";
import { addDays, diffDays, fmtDate, monthName, startOfWeek, toISODate, today } from "@/lib/format";
import { useLoad } from "@/lib/hooks";
import { t, useLocale, type Key } from "@/lib/i18n";
import { usePersisted } from "@/lib/usePersisted";
import { useSession } from "@/components/AppShell";
import { IconChevronLeft, IconChevronRight } from "@/components/icons";
import { Button, Empty, ErrorBox, ListSkeleton, Segmented, Select, cx } from "@/components/ui";

/*
 * 日程（ADR 0032，DESIGN.md §27）：目标、任务、里程碑与外部日历的一种看法。
 * 三档粒度写死：日 / 周 / 月；周从周一起。四类各有画法：
 *   任务 = 按状态着色的条（只有截止日的画成一枚旗标）；目标 = 淡一级的底条；里程碑 = 菱形；会议 = 带来源的块（别人的只画「忙」）。
 * 「我的 / 团队」切换：团队按成员一人一行，只看周与日。这里不能新建、不能拖动——改日期去任务页、路线图或外部日历。
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
  const teams = session?.teams ?? [];
  const team = teamID || teams[0]?.id || "";
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
          {who === "team" && teams.length > 1 && (
            <Select value={team} onChange={(e) => setTeamID(e.target.value)} aria-label={t("schedule.teamPick")} className="w-[160px]">
              {teams.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
            </Select>
          )}
          {teams.length > 0 && (
            <Segmented size="sm" value={who} options={[["me", t("schedule.scope.mine")], ["team", t("schedule.scope.team")]]} onChange={setWho} aria-label={t("schedule.scope")} />
          )}
          <Segmented size="sm" value={effScale} options={SCALES.map((k) => [k, t(`schedule.view.${k}` as Key)])} onChange={setScale} aria-label={t("schedule.scaleLabel")} />
        </div>
      </div>
      {data.error && !data.data ? (
        <ErrorBox message={data.error} onRetry={data.reload} />
      ) : !data.data ? (
        <div className="sc-skeleton"><ListSkeleton rows={6} /></div>
      ) : items.length === 0 && who === "me" ? (
        <>
          <Legend sources={data.data.sources} />
          <Empty text={t(effScale === "day" ? "schedule.emptyDay" : effScale === "week" ? "schedule.emptyWeek" : "schedule.emptyMonth")} />
        </>
      ) : who === "team" && effScale === "month" ? (
        <>
          <Legend sources={data.data.sources} />
          <TeamMonth anchor={anchor} items={items} members={members} meID={meID} onPickDay={(d) => { setAnchor(d); setScale("day"); }} />
        </>
      ) : who === "team" ? (
        <>
          <Legend sources={data.data.sources} />
          <TeamGrid range={range} items={items} members={members} meID={meID} />
        </>
      ) : effScale === "month" ? (
        <>
          <Legend sources={data.data.sources} />
          <MonthGrid range={range} anchor={anchor} items={items} meID={meID} />
        </>
      ) : (
        <>
          <Legend sources={data.data.sources} />
          <WeekGrid range={range} items={items} meID={meID} />
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

/** 一枚日程项：任务条 / 目标底条 / 里程碑菱形 / 会议块。compact 时只留一行 */
function Chip({ it, day, meID, compact }: { it: ScheduleItem; day: string; meID: string; compact?: boolean }) {
  const href = hrefOf(it);
  const [a, b] = daysOf(it);
  const first = a === day;
  const last = b === day;
  const label = it.kind === "task" && it.number ? `#${it.number} ${it.title}` : it.title;
  const tip = it.kind === "task" ? `#${it.number} ${it.title}${it.state ? " · " + it.state.title : ""}` : it.kind === "event" && it.starts_at && !it.all_day ? `${timeText(it)} ${it.title}` : it.title;
  const body: ReactNode = (
    <>
      {it.kind === "milestone" && <i className="sc-diamond" aria-hidden="true" />}
      {it.kind === "task" && it.deadline && <i className="sc-flag" aria-hidden="true" />}
      {it.kind === "event" && !it.all_day && !compact && <span className="sc-time">{timeText(it)}</span>}
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
function Legend({ sources }: { sources: string[] }) {
  return (
    <div className="sc-legend" aria-hidden="true">
      <span><i className="sc-swatch sc-chip-accent" />{t("schedule.legend.task")}</span>
      <span><i className="sc-swatch sc-chip-goal" />{t("schedule.legend.goal")}</span>
      <span><i className="sc-diamond" />{t("schedule.legend.milestone")}</span>
      <span><i className="sc-swatch sc-chip-event" />{t("schedule.legend.event")}{sources.length > 0 && <span className="text-ink-tertiary">（{sources.map(providerShort).join(" · ")}）</span>}</span>
    </div>
  );
}

// ---------- 周 / 日 ----------

function WeekGrid({ range, items, meID }: { range: { from: Date; to: Date }; items: ScheduleItem[]; meID: string }) {
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
            {list.map((it) => <Chip key={it.kind + it.id} it={it} day={iso} meID={meID} />)}
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
                  <Chip it={it} day={iso} meID={meID} />
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

function MonthGrid({ range, anchor, items, meID }: { range: { from: Date; to: Date }; anchor: Date; items: ScheduleItem[]; meID: string }) {
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
            {shown.map((it) => <Chip key={it.kind + it.id} it={it} day={iso} meID={meID} compact />)}
            {list.length > MAX && <span className="sc-more">+{list.length - MAX}</span>}
          </div>
        );
      })}
    </div>
  );
}

// ---------- 团队：一人一行 ----------

function TeamGrid({ range, items, members, meID }: { range: { from: Date; to: Date }; items: ScheduleItem[]; members: Array<{ id: string; name: string }>; meID: string }) {
  const days = useMemo(() => Array.from({ length: diffDays(range.from, range.to) + 1 }, (_, i) => addDays(range.from, i)), [range]);
  const todayISO = toISODate(today());
  if (members.length === 0) return <Empty text={t("schedule.emptyTeam")} />;
  return (
    <div className="sc-team" style={{ "--cols": days.length } as React.CSSProperties}>
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
      {members.map((m) => (
        <MemberRow key={m.id} member={m} days={days} items={items.filter((it) => it.member_id === m.id)} meID={meID} todayISO={todayISO} />
      ))}
    </div>
  );
}

function MemberRow({ member, days, items, meID, todayISO }: { member: { id: string; name: string }; days: Date[]; items: ScheduleItem[]; meID: string; todayISO: string }) {
  return (
    <>
      <div className="sc-member">
        <span className="truncate">{member.name}</span>
        {member.id === meID && <span className="sc-me">{t("schedule.me")}</span>}
      </div>
      {days.map((d) => {
        const iso = toISODate(d);
        const list = items.filter((it) => coversDay(it, iso));
        const sorted = [...list].sort((x, y) => (timed(x) ? (x.starts_at ?? "") : "").localeCompare(timed(y) ? (y.starts_at ?? "") : ""));
        return (
          <div key={iso} className={cx("sc-band sc-team-cell", iso === todayISO && "sc-today")}>
            {sorted.map((it) => <Chip key={it.kind + it.id} it={it} day={iso} meID={meID} compact={days.length > 1} />)}
          </div>
        );
      })}
    </>
  );
}

// ---------- 团队 · 月：一人一行，每格只画负载 ----------

/** 团队月视图不放标题：一格里按类别画小点（最多三枚），多了写数字；点一格跳到那一天的团队日视图。 */
function TeamMonth({ anchor, items, members, meID, onPickDay }: { anchor: Date; items: ScheduleItem[]; members: Array<{ id: string; name: string }>; meID: string; onPickDay: (d: Date) => void }) {
  const year = anchor.getFullYear();
  const month = anchor.getMonth();
  const days = useMemo(() => {
    const first = new Date(year, month, 1);
    const count = new Date(year, month + 1, 0).getDate();
    return Array.from({ length: count }, (_, i) => addDays(first, i));
  }, [year, month]);
  const todayISO = toISODate(today());
  if (members.length === 0) return <Empty text={t("schedule.emptyTeam")} />;
  return (
    <div className="sc-team sc-team-month" style={{ "--cols": days.length } as React.CSSProperties}>
      <div className="sc-head sc-corner" />
      {days.map((d) => {
        const iso = toISODate(d);
        return (
          <div key={iso} className={cx("sc-head sc-head-tiny", iso === todayISO && "sc-today", (d.getDay() === 0 || d.getDay() === 6) && "sc-weekend")}>
            <span className="sc-dn">{d.getDate()}</span>
          </div>
        );
      })}
      {members.map((m) => (
        <TeamMonthRow key={m.id} member={m} days={days} items={items.filter((it) => it.member_id === m.id)} meID={meID} todayISO={todayISO} onPickDay={onPickDay} />
      ))}
    </div>
  );
}

function TeamMonthRow({ member, days, items, meID, todayISO, onPickDay }: { member: { id: string; name: string }; days: Date[]; items: ScheduleItem[]; meID: string; todayISO: string; onPickDay: (d: Date) => void }) {
  return (
    <>
      <div className="sc-member">
        <span className="truncate">{member.name}</span>
        {member.id === meID && <span className="sc-me">{t("schedule.me")}</span>}
      </div>
      {days.map((d) => {
        const iso = toISODate(d);
        const list = items.filter((it) => coversDay(it, iso) && it.kind !== "goal");
        const n = list.length;
        const kinds = Array.from(new Set(list.map((it) => it.kind)));
        const tip = n ? `${fmtDate(iso)} · ${t("schedule.loadTip", { n })}` : fmtDate(iso);
        return (
          <button key={iso} type="button" className={cx("sc-load", iso === todayISO && "sc-today", n >= 4 && "sc-load-heavy", (d.getDay() === 0 || d.getDay() === 6) && "sc-weekend")} title={tip} aria-label={tip} onClick={() => onPickDay(d)}>
            {n === 0 ? null : n <= 3 ? kinds.slice(0, 3).map((k) => <i key={k} className={cx("sc-dot", `sc-dot-${k}`)} aria-hidden="true" />) : <span className="sc-load-n">{n}</span>}
          </button>
        );
      })}
    </>
  );
}
