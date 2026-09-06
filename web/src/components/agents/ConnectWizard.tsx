"use client";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { api, DEVICE_CLIENTS, type DeviceClient } from "@/lib/api";
import { t } from "@/lib/i18n";
import { IconChevronDown, IconChevronRight, IconKey } from "@/components/icons";
import { Button, CopyLine, Dialog, Input, cx } from "@/components/ui";
import { WizardSteps } from "./WizardSteps";

/** 运行环境的界面名（与后端 client_title 一致；Agent 行的「运行环境」列也用它） */
export const clientTitle = (c: string | null | undefined): string => (c === "claude-code" ? "Claude Code" : c === "cursor" ? "Cursor" : c === "codex" ? "OpenAI Codex" : c === "custom" ? t("connect.client.custom") : c || "—");

/**
 * 「接入 Agent」向导对话框（DESIGN.md §21）：① 选运行环境 → ② 在终端执行一条命令（复制；「或者手工配置」走原来的注册令牌路径）
 * → ③ 等待批准：终端会打印验证码和网址，这里也可以直接输入验证码跳到批准页 /agents/connect/?code=。
 * 设备码绑定的是 Agent 身份，批准的人就是所有者（ADR 0018）。
 */
export function ConnectWizard({ open, onClose, onManual }: { open: boolean; onClose: () => void; /** 「或者手工配置」：打开原来的注册抽屉 */ onManual: () => void }) {
  const router = useRouter();
  const [step, setStep] = useState<0 | 1 | 2>(0);
  const [client, setClient] = useState<DeviceClient>("claude-code");
  const [manualOpen, setManualOpen] = useState(false);
  const [code, setCode] = useState("");
  const [seenOpen, setSeenOpen] = useState(open);
  if (seenOpen !== open) {
    setSeenOpen(open);
    if (open) { setStep(0); setManualOpen(false); setCode(""); }
  }
  const command = `curl -fsSL '${api.agentAuth.scriptUrl(client)}' | sh`;
  const goCode = (e: FormEvent) => {
    e.preventDefault();
    const c = code.trim();
    if (!c) return;
    onClose();
    router.push(`/agents/connect/?code=${encodeURIComponent(c)}`);
  };
  const stepKey = (["client", "command", "approve"] as const)[step];

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t("agents.connect")}
      width={640}
      footer={
        <>
          {step > 0 ? <Button variant="ghost" onClick={() => setStep((s) => (s - 1) as 0 | 1 | 2)}>{t("connect.back")}</Button> : <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>}
          {step < 2 ? <Button variant="primary" onClick={() => setStep((s) => (s + 1) as 0 | 1 | 2)}>{t("connect.next")}</Button> : <Button variant="primary" form="connect-code-form" type="submit" disabled={!code.trim()}>{t("connect.openApprove")}</Button>}
        </>
      }
    >
      <WizardSteps current={stepKey} className="mb-4" />
      {step === 0 && (
        <div data-wizard-step="client">
          <p className="mb-3 text-caption text-ink-muted">{t("connect.clientHint")}</p>
          <div className="grid gap-2 sm:grid-cols-2" role="radiogroup" aria-label={t("connect.step.client")}>
            {DEVICE_CLIENTS.map((c) => (
              <button key={c} type="button" role="radio" aria-checked={client === c} onClick={() => setClient(c)} className={cx("pressable flex flex-col items-start gap-1 rounded-md border px-3 py-2.5 text-left", client === c ? "border-accent bg-accent-bg" : "border-hairline bg-surface-1 hover:border-hairline-strong")} data-client={c}>
                <span className={cx("text-body font-medium", client === c ? "text-accent-hover" : "text-ink")}>{clientTitle(c)}</span>
                <span className="text-caption text-ink-subtle">{t(`connect.client.${c}.hint`)}</span>
              </button>
            ))}
          </div>
        </div>
      )}
      {step === 1 && (
        <div className="space-y-3" data-wizard-step="command">
          <p className="text-caption text-ink-muted">{t("connect.commandHint", { client: clientTitle(client) })}</p>
          <CopyLine text={command} />
          <p className="text-caption text-ink-subtle">{t("connect.commandNote")}</p>
          <div className="border-t border-hairline pt-3">
            <button type="button" className="pressable inline-flex items-center gap-1 text-caption text-ink-muted hover:text-ink" onClick={() => setManualOpen((v) => !v)} aria-expanded={manualOpen}>
              {manualOpen ? <IconChevronDown size={14} /> : <IconChevronRight size={14} />}
              {t("connect.manual")}
            </button>
            {manualOpen && (
              <div className="mt-2 space-y-2 text-caption text-ink-subtle" data-manual="">
                <p>{t("connect.manualHint")}</p>
                <Button size="sm" icon={<IconKey />} onClick={() => { onClose(); onManual(); }}>{t("agents.register")}</Button>
              </div>
            )}
          </div>
        </div>
      )}
      {step === 2 && (
        <form id="connect-code-form" onSubmit={goCode} className="space-y-3" data-wizard-step="approve">
          <p className="text-caption text-ink-muted">{t("connect.waitHint")}</p>
          <pre className="telemetry overflow-x-auto rounded-md border border-hairline bg-code-bg px-3 py-2 text-caption whitespace-pre-wrap">{t("connect.terminalSample", { url: `${api.agentAuth.scriptUrl(client).replace(/\/api\/v1\/.*$/, "")}/agents/connect/?code=ABCD-1234` })}</pre>
          <label className="block">
            <span className="mb-1 block text-caption text-ink-subtle">{t("connect.codeField")}</span>
            <Input value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} placeholder="ABCD-1234" autoFocus className="w-48 font-mono" aria-label={t("connect.codeField")} />
          </label>
        </form>
      )}
    </Dialog>
  );
}
