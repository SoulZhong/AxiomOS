"use client";
import { useState } from "react";
import { api, type MyNotifications, type QuietHours } from "@/lib/api";
import { errorMessage, useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useToast } from "@/components/toast";
import { Button, Checkbox, ConsequenceDialog, Empty, ErrorBox, Input, ListSkeleton, Panel, Switch, Tag, Tip, cx } from "@/components/ui";
import { DeliveriesPanel } from "./NotificationsTab";

/*
 * 个人设置 → 通知（ADR 0019 第三层，DESIGN.md §22）：一张小矩阵——行是事项，列是组织开着的通道，格子是勾选框。
 * 只能在组织允许的范围内选：组织没允许的事项这里根本不出现（后端已经裁掉）。
 * 某个通道要绑 IM 身份而我没绑时，那一列的格子不可选，鼠标停上去说清去哪儿绑（句子来自接口）。
 * 改一格立刻 PUT（只发这一行），安静时段与「恢复默认」同理。下面是我最近收到的外发记录。
 */
export function MeNotifications({ index = 2 }: { index?: number }) {
  const toast = useToast();
  const cfg = useLoad(() => api.me.notifications.get(), []);
  const [busy, setBusy] = useState<string | null>(null);
  const [resetting, setResetting] = useState(false);
  const [version, setVersion] = useState(0);
  // 保存后先用返回值画（少一次往返），任务重新加载时清掉
  const [local, setLocal] = useState<MyNotifications | null>(null);
  const [seen, setSeen] = useState(cfg.data);
  if (seen !== cfg.data) { setSeen(cfg.data); setLocal(null); }
  const data = local ?? cfg.data;

  const apply = (d: MyNotifications) => { setLocal(d); setVersion((v) => v + 1); };
  const save = async (key: string, body: Parameters<typeof api.me.notifications.update>[0]) => {
    setBusy(key);
    // 保存中不禁用整张表：勾一格立刻见效，连着勾几格也不会被挡住
    try { apply(await api.me.notifications.update(body)); toast.ok(t("notify.me.saved")); }
    catch (e) { toast.fail(errorMessage(e)); cfg.reload(); }
    finally { setBusy(null); }
  };
  const reset = async () => {
    setBusy("reset");
    try { apply(await api.me.notifications.reset()); toast.ok(t("notify.me.resetDone")); setResetting(false); }
    catch (e) { toast.fail(errorMessage(e)); }
    finally { setBusy(null); }
  };

  if (cfg.loading && !data) return <Panel index={index} title={t("notify.me.title")}><ListSkeleton rows={4} /></Panel>;
  if (cfg.error && !data) return <ErrorBox message={cfg.error} onRetry={cfg.reload} />;
  if (!data) return null;

  const kinds = data.kinds.filter((k) => data.allowed_kinds.includes(k.key));
  const channels = data.available_channels;
  const changed = Object.values(data.sources).some((s) => s === "personal") || !!data.quiet_hours;
  const toggleCell = (kind: string, channel: string, on: boolean) => {
    const cur = data.rules[kind] ?? [];
    const next = on ? [...cur.filter((c) => c !== channel), channel] : cur.filter((c) => c !== channel);
    // 先按点的结果画（勾选框立刻响应），服务端返回后以返回值为准
    setLocal({ ...data, rules: { ...data.rules, [kind]: next }, sources: { ...data.sources, [kind]: "personal" } });
    void save(`${kind}:${channel}`, { rules: { [kind]: next } });
  };

  return (
    <>
      <Panel
        index={index}
        title={t("notify.me.title")}
        telemetry={busy ? t("common.saving") : undefined}
        actions={<Button size="sm" variant="ghost" disabled={!!busy || !changed} onClick={() => setResetting(true)}>{t("notify.me.reset")}</Button>}
      >
        <p className="mb-3 max-w-[640px] text-body text-ink-muted">{t("notify.me.description")}</p>
        {channels.length === 0 ? (
          <Empty text={t("notify.me.noChannels")} illustration={false} className="py-6" />
        ) : (
          <div className="max-w-[640px] overflow-x-auto">
            <table className="w-full border-collapse text-body" data-notify-matrix>
              <thead>
                <tr className="border-b border-hairline">
                  <th className="py-1.5 pr-3 text-left font-normal text-ink-subtle">{t("notify.me.kind")}</th>
                  {channels.map((c) => (
                    <th key={c.key} className="w-[104px] px-2 py-1.5 text-center font-normal">
                      <Tip tip={c.im && !c.bound ? c.hint ?? null : null}>
                        <span className={cx("inline-flex items-center gap-1", c.im && !c.bound ? "text-ink-subtle" : "text-ink")}>
                          {c.title}
                          {c.im && !c.bound && <Tag>!</Tag>}
                        </span>
                      </Tip>
                    </th>
                  ))}
                  <th className="w-[80px] py-1.5 pl-2 text-right font-normal text-ink-subtle">{t("notify.me.sourceCol")}</th>
                </tr>
              </thead>
              <tbody>
                {kinds.map((k) => {
                  const on = data.rules[k.key] ?? [];
                  return (
                    <tr key={k.key} className="border-b border-hairline last:border-0" data-kind={k.key}>
                      <td className="py-1.5 pr-3">
                        <span className="flex flex-wrap items-center gap-2">
                          {k.title}
                          {on.length === 0 && <span className="text-caption text-ink-subtle">{t("notify.me.inApp")}</span>}
                        </span>
                      </td>
                      {channels.map((c) => {
                        const blocked = c.im && !c.bound;
                        return (
                          <td key={c.key} className="px-2 py-1.5 text-center">
                            {blocked ? (
                              <Tip tip={c.hint ?? null}><span className="text-ink-tertiary" aria-label={c.hint ?? c.title}>—</span></Tip>
                            ) : (
                              <Checkbox
                                className="justify-center"
                                label={<span className="sr-only">{`${k.title} · ${c.title}`}</span>}
                                checked={on.includes(c.key)}
                                onChange={(e) => toggleCell(k.key, c.key, e.target.checked)}
                              />
                            )}
                          </td>
                        );
                      })}
                      <td className="py-1.5 pl-2 text-right text-caption text-ink-subtle">{data.sources[k.key] === "personal" ? t("notify.me.source.personal") : t("notify.me.source.default")}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {/* 要绑 IM 身份才能收到的通道：把"为什么这一列不能选"写在表下面，不只藏在气泡里 */}
            {channels.filter((c) => c.im && !c.bound && c.hint).map((c) => (
              <p key={c.key} className="mt-2 text-caption text-warning" data-unbound={c.key}>{c.hint}</p>
            ))}
          </div>
        )}
        <QuietHoursRow value={data.quiet_hours} busy={!!busy} onSave={(q) => void save("quiet", { quiet_hours: q })} />
      </Panel>
      <DeliveriesPanel
        key={version}
        index={index + 1}
        title={t("notify.me.deliveries")}
        empty={t("notify.me.noDeliveries")}
        load={() => api.me.notifications.deliveries(20)}
        deps={[version]}
      />
      <ConsequenceDialog
        open={resetting}
        title={t("notify.me.resetTitle")}
        effects={[t("notify.me.resetEffect.personal"), t("notify.me.resetEffect.default")]}
        danger={false}
        confirmLabel={t("notify.me.reset")}
        busy={busy === "reset"}
        onConfirm={() => void reset()}
        onClose={() => setResetting(false)}
      />
    </>
  );
}

/** 安静时段：一个开关加两个时刻；关掉就是 null。 */
function QuietHoursRow({ value, busy, onSave }: { value: QuietHours | null; busy: boolean; onSave: (q: QuietHours | null) => void }) {
  const [from, setFrom] = useState(value?.from ?? "22:00");
  const [to, setTo] = useState(value?.to ?? "08:00");
  const [error, setError] = useState<string | null>(null);
  const on = !!value;
  const commit = (f: string, tt: string) => {
    if (!f || !tt) return;
    if (f === tt) { setError(t("notify.me.quietSame")); return; }
    setError(null);
    onSave({ from: f, to: tt });
  };
  return (
    <div className="mt-4 max-w-[640px] border-t border-hairline pt-4" data-quiet-hours={on ? "" : undefined}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <Switch checked={on} disabled={busy} onChange={(v) => (v ? commit(from, to) : onSave(null))} label={t("notify.me.quietOn")} />
        <span className="text-body">{t("notify.me.quiet")}</span>
        <span className={cx("flex items-center gap-2", !on && "opacity-50")}>
          <span className="text-body text-ink-subtle">{t("notify.me.quietFrom")}</span>
          <Input type="time" value={from} disabled={!on || busy} onChange={(e) => { setFrom(e.target.value); commit(e.target.value, to); }} aria-label={t("notify.me.quietFrom")} className="w-[132px]" />
          <span className="text-body text-ink-subtle">{t("notify.me.quietTo")}</span>
          <Input type="time" value={to} disabled={!on || busy} onChange={(e) => { setTo(e.target.value); commit(from, e.target.value); }} aria-label={t("notify.me.quietTo")} className="w-[132px]" />
        </span>
      </div>
      <p className="mt-1.5 text-caption text-ink-subtle">{t("notify.me.quietHint")}</p>
      {error && <p className="mt-1 text-caption text-danger" role="alert">{error}</p>}
    </div>
  );
}
