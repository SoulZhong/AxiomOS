"use client";
import { useState, type FormEvent } from "react";
import { api, type Delivery, type NotifyChannel, type NotifyHealth, type OrgNotifications } from "@/lib/api";
import { fmtDateTime } from "@/lib/format";
import { errorMessage, useLoad } from "@/lib/hooks";
import { t, type Key } from "@/lib/i18n";
import { IconNotify } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Checkbox, Empty, ErrorBox, ListSkeleton, Panel, Select, Switch, Table, Tag, cx, type Tone } from "@/components/ui";
import { CredentialField } from "./wizard";

/*
 * 组织设置 → 通知（ADR 0019，DESIGN.md §22）：三层里的中间那层——组织决定「用哪些通道」与「哪些事项能往外发」。
 * 一个通道一张卡：能不能用（已接入 / 未接入 + 一句为什么）、开关、按提供方声明渲染的凭据字段（与 IM 集成同一套控件）、
 * 接入前要准备什么、最近好不好（连续失败几次 + 最后一次的原话），右上角「发送测试」当场给一句结果。
 * 下面是允许外发的事项（勾一下就存）与最近投递（可按通道 / 状态筛）。
 * 提供方是数据不是分支：这个文件里没有平台名，字段、前置条件、提示句全部来自接口。
 */
const HEALTH_TONE = (h: NotifyHealth): Tone => (h.status === "degraded" ? "danger" : "success");

export function NotificationsTab() {
  const cfg = useLoad(() => api.org.notifications.get(), []);
  const [filter, setFilter] = useState<{ channel: string; status: string }>({ channel: "", status: "" });
  const data = cfg.data;

  if (cfg.loading && !data) return <Panel index={1} title={t("settings.tab.notifications")}><ListSkeleton rows={4} /></Panel>;
  if (cfg.error && !data) return <ErrorBox message={cfg.error} onRetry={cfg.reload} />;
  if (!data) return null;
  const channels = data.channel_order.map((k) => data.channels[k]).filter(Boolean);

  return (
    <div className="space-y-4">
      <Panel index={1} title={t("settings.tab.notifications")}>
        <p className="mb-4 max-w-[640px] text-body text-ink-subtle">{t("settings.notify.description")}</p>
        <div className="max-w-[760px] space-y-3">
          {channels.map((c) => <ChannelCard key={c.key} channel={c} onSaved={cfg.reload} />)}
        </div>
      </Panel>
      <KindsPanel data={data} onSaved={cfg.reload} />
      <DeliveriesPanel
        channels={channels}
        filter={filter}
        onFilter={setFilter}
        title={t("settings.notify.deliveries")}
        load={() => api.org.notifications.deliveries({ limit: 50, channel: filter.channel || undefined, status: filter.status || undefined })}
        deps={[filter.channel, filter.status]}
        showRecipient
      />
    </div>
  );
}

/** 一个通道一张卡。凭据字段与 IM 集成同一套控件：保密字段已设置时只显示「已设置」与「重设」。 */
function ChannelCard({ channel: c, onSaved }: { channel: NotifyChannel; onSaved: () => void }) {
  const toast = useToast();
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(c.fields.map((f) => [f.key, f.secret ? "" : c.config[f.key] ?? ""])));
  const [resetting, setResetting] = useState<Record<string, boolean>>(() => Object.fromEntries(c.fields.filter((f) => f.secret).map((f) => [f.key, !c.secrets_set[f.key]])));
  const [busy, setBusy] = useState<"save" | "test" | "enable" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tested, setTested] = useState<{ ok: boolean; message: string } | null>(null);

  const put = async (body: Parameters<typeof api.org.notifications.save>[0], ok: string) => {
    try { await api.org.notifications.save(body); toast.ok(ok); onSaved(); return true; }
    catch (e) { const m = errorMessage(e); setError(m); toast.fail(m); return false; }
  };
  const toggle = async (on: boolean) => {
    setBusy("enable"); setError(null);
    await put({ channels: { [c.key]: { enabled: on } } }, t("settings.notify.saved", { name: c.title }));
    setBusy(null);
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    for (const f of c.fields) {
      const v = (values[f.key] ?? "").trim();
      if (f.optional) continue;
      if (!f.secret && !v) { setError(t("settings.directory.needField", { title: f.title })); return; }
      if (f.secret && !c.secrets_set[f.key] && !v) { setError(t("settings.directory.needSecretField", { title: f.title })); return; }
    }
    const config: Record<string, string> = {};
    for (const f of c.fields) {
      const v = (values[f.key] ?? "").trim();
      if (!f.secret) config[f.key] = v;
      else if (resetting[f.key] && v) config[f.key] = v; // 省略保密字段 = 沿用已保存的
    }
    setBusy("save");
    await put({ channels: { [c.key]: { config } } }, t("settings.notify.saved", { name: c.title }));
    setBusy(null);
  };
  const test = async () => {
    setBusy("test"); setError(null); setTested(null);
    try { const r = await api.org.notifications.test(c.key); setTested({ ok: r.ok, message: r.message }); onSaved(); }
    catch (e) { setTested({ ok: false, message: errorMessage(e) }); }
    finally { setBusy(null); }
  };

  const h = c.health;
  return (
    <section className="rounded-md border border-hairline bg-surface-2 p-4" data-channel={c.key} data-available={c.available ? "" : undefined}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <span className={cx("h-2 w-2 shrink-0 rounded-full", c.available ? (h.status === "degraded" ? "bg-danger" : "bg-success") : "bg-neutral")} aria-hidden="true" />
        <span className="text-title font-medium">{c.title}</span>
        <Tag tone={c.configured ? "success" : "neutral"}>{c.configured ? t("settings.notify.state.on") : t("settings.notify.state.off")}</Tag>
        {c.im && <Tag>IM</Tag>}
        <span className="ml-auto flex items-center gap-3">
          <span className="flex items-center gap-2 text-body text-ink-muted">
            <Switch checked={c.enabled} disabled={busy !== null} onChange={(v) => void toggle(v)} label={t("settings.notify.enable")} />
            {t("settings.notify.enable")}
          </span>
          <Button size="sm" icon={<IconNotify />} disabled={busy !== null || !c.available} onClick={() => void test()}>{busy === "test" ? t("settings.notify.testing") : t("settings.notify.test")}</Button>
        </span>
      </div>
      {c.hint && <p className="mt-2 text-body text-ink-muted">{c.hint}</p>}
      {!c.enabled && !c.hint && <p className="mt-2 text-body text-ink-muted">{t("settings.notify.enableOff")}</p>}

      {c.prerequisites.length > 0 && !c.configured && (
        <>
          <div className="eyebrow mt-3 text-ink-subtle">{t("settings.directory.prerequisites")}</div>
          <ol className="mt-1 list-decimal space-y-1 pl-5 text-body text-ink-muted">
            {c.prerequisites.map((x, i) => <li key={i}>{x}</li>)}
          </ol>
        </>
      )}

      {c.fields.length > 0 && (
        <form onSubmit={submit} className="mt-4 space-y-3">
          <div className="grid gap-4 sm:grid-cols-2">
            {c.fields.map((f) => (
              <CredentialField
                key={f.key}
                field={f}
                value={values[f.key] ?? ""}
                onChange={(v) => setValues((d) => ({ ...d, [f.key]: v }))}
                isSet={!!c.secrets_set[f.key]}
                resetting={!!resetting[f.key]}
                onReset={(on) => { setResetting((r) => ({ ...r, [f.key]: on })); if (!on) setValues((d) => ({ ...d, [f.key]: "" })); }}
              />
            ))}
          </div>
          {error && <p className="text-caption text-danger" role="alert">{error}</p>}
          <Button type="submit" variant="primary" size="sm" disabled={busy !== null}>{busy === "save" ? t("common.saving") : t("settings.notify.save")}</Button>
        </form>
      )}
      {c.fields.length === 0 && error && <p className="mt-2 text-caption text-danger" role="alert">{error}</p>}

      <div className="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1 text-caption text-ink-subtle" data-health={h.status}>
        {h.status === "degraded"
          ? <><Tag tone={HEALTH_TONE(h)}>{t("settings.notify.health.bad", { n: h.streak })}</Tag>{h.last_error && <span className="text-ink-muted">{t("settings.notify.health.error", { error: h.last_error })}</span>}</>
          : <span>{h.last_at ? t("settings.notify.health.ok") : t("settings.notify.health.none")}</span>}
        {h.last_at && <span className="telemetry">{t("settings.notify.health.at", { time: fmtDateTime(h.last_at) })}</span>}
      </div>
      {tested && (
        <p className={cx("mt-2 rounded-sm px-2 py-1 text-body", tested.ok ? "bg-success-bg text-success" : "bg-danger-bg text-danger")} role="status" data-test-result>
          {tested.message}
        </p>
      )}
    </section>
  );
}

/** 允许外发的事项：勾一下立刻存（整体替换，与后端一样按目录顺序） */
function KindsPanel({ data, onSaved }: { data: OrgNotifications; onSaved: () => void }) {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const allowed = new Set(data.allowed_kinds);
  const toggle = async (key: string, on: boolean) => {
    const next = data.kinds.map((k) => k.key).filter((k) => (k === key ? on : allowed.has(k)));
    setBusy(true);
    try { await api.org.notifications.save({ allowed_kinds: next }); toast.ok(t("settings.notify.kindsSaved")); onSaved(); }
    catch (e) { toast.fail(errorMessage(e)); }
    finally { setBusy(false); }
  };
  return (
    <Panel index={2} title={t("settings.notify.kinds")}>
      <p className="mb-3 max-w-[640px] text-body text-ink-subtle">{t("settings.notify.kindsHint")}</p>
      <ul className="flex max-w-[640px] flex-wrap gap-x-6 gap-y-2" data-allowed-kinds>
        {data.kinds.map((k) => (
          <li key={k.key}>
            <Checkbox label={k.title} checked={allowed.has(k.key)} disabled={busy} onChange={(e) => void toggle(k.key, e.target.checked)} />
          </li>
        ))}
      </ul>
    </Panel>
  );
}

const DELIVERY_TONE: Record<Delivery["status"], Tone> = { queued: "accent", sent: "success", failed: "danger", skipped: "neutral" };
const DELIVERY_STATUSES: Delivery["status"][] = ["queued", "sent", "failed", "skipped"];

/** 最近投递：组织侧多一列收件人，并可按通道 / 状态筛。个人侧同一张表。 */
export function DeliveriesPanel({ channels, filter, onFilter, title, load, deps, showRecipient, index = 3, empty }: {
  channels?: Array<{ key: string; title: string }>;
  filter?: { channel: string; status: string };
  onFilter?: (f: { channel: string; status: string }) => void;
  title: string;
  load: () => Promise<Delivery[]>;
  deps: unknown[];
  showRecipient?: boolean;
  index?: number;
  empty?: string;
}) {
  const rows = useLoad(load, deps);
  const list = rows.data ?? [];
  return (
    <Panel
      index={index}
      title={title}
      telemetry={rows.data ? t("panel.rows", { n: list.length }) : undefined}
      padded={false}
      actions={onFilter && filter && (
        <span className="flex items-center gap-2">
          <Select value={filter.channel} onChange={(e) => onFilter({ ...filter, channel: e.target.value })} aria-label={t("settings.notify.col.channel")} className="!h-7 !w-[132px]">
            <option value="">{t("settings.notify.allChannels")}</option>
            {(channels ?? []).map((c) => <option key={c.key} value={c.key}>{c.title}</option>)}
          </Select>
          <Select value={filter.status} onChange={(e) => onFilter({ ...filter, status: e.target.value })} aria-label={t("settings.notify.col.status")} className="!h-7 !w-[112px]">
            <option value="">{t("settings.notify.allStatus")}</option>
            {DELIVERY_STATUSES.map((s) => <option key={s} value={s}>{t(`settings.notify.status.${s}` as Key)}</option>)}
          </Select>
        </span>
      )}
    >
      {rows.loading && !rows.data ? <div className="p-4"><ListSkeleton rows={3} /></div>
        : rows.error ? <div className="p-4"><ErrorBox message={rows.error} onRetry={rows.reload} /></div>
        : !list.length ? <Empty text={empty ?? t("settings.notify.noDeliveries")} illustration={false} className="py-6" />
        : (
          <Table>
            <thead>
              <tr>
                <th className="w-[150px]">{t("settings.notify.col.time")}</th>
                <th className="w-[220px]">{t("settings.notify.col.kind")}</th>
                <th className="w-[100px]">{t("settings.notify.col.channel")}</th>
                {showRecipient && <th className="w-[110px]">{t("settings.notify.col.recipient")}</th>}
                <th className="w-[96px]">{t("settings.notify.col.status")}</th>
                <th className="w-full min-w-[200px]">{t("settings.notify.col.error")}</th>
              </tr>
            </thead>
            <tbody>
              {list.map((d) => (
                <tr key={d.id} data-delivery={d.id} data-status={d.status}>
                  <td className="telemetry whitespace-nowrap">{fmtDateTime(d.sent_at ?? d.created_at)}</td>
                  <td className="max-w-[220px]">
                    <span className="block truncate" title={d.title}>{d.title || d.kind_title}</span>
                    <span className="text-caption text-ink-subtle">{d.kind_title}</span>
                  </td>
                  <td className="whitespace-nowrap text-ink-muted">{d.channel_title}</td>
                  {showRecipient && <td className="max-w-[110px] truncate whitespace-nowrap">{d.recipient?.name ?? "—"}</td>}
                  <td className="whitespace-nowrap">
                    <Tag tone={DELIVERY_TONE[d.status]}>{d.status_title}</Tag>
                    {d.attempts > 1 && <span className="ml-1.5 text-caption text-ink-subtle">{t("settings.notify.attempts", { n: d.attempts })}</span>}
                  </td>
                  <td className="text-ink-muted">{d.error || <span className="text-ink-subtle">—</span>}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
    </Panel>
  );
}
