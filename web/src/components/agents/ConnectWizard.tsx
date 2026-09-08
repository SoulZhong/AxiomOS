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
 * 「接入 Agent」向导对话框（DESIGN.md §21）：① 选运行环境 → ② **把链接贴给 Agent**（首选，ADR 0024；
 * 下面依次是「或者在终端里自己执行」的 curl 一行与折叠的「或者手工配置」——原来的注册令牌路径）
 * → ③ 等待批准：Agent 或终端会给出验证码和网址，这里也可以直接输入验证码跳到批准页 /agents/connect/?code=。
 * 设备码绑定的是 Agent 身份，批准的人就是所有者（ADR 0018）。
 */
export function ConnectWizard({ open, onClose, onManual }: { open: boolean; onClose: () => void; /** 「或者手工配置」：打开原来的注册抽屉 */ onManual: () => void }) {
  const router = useRouter();
  const [step, setStep] = useState<0 | 1>(0);
  const [termOpen, setTermOpen] = useState(false);
  const [client, setClient] = useState<DeviceClient>("claude-code");
  const [manualOpen, setManualOpen] = useState(false);
  const [code, setCode] = useState("");
  const [seenOpen, setSeenOpen] = useState(open);
  if (seenOpen !== open) {
    setSeenOpen(open);
    if (open) { setStep(0); setManualOpen(false); setTermOpen(false); setCode(""); }
  }
  const command = `curl -fsSL '${api.agentAuth.scriptUrl(client)}' | sh`;
  // 贴给 Agent 的链接（ADR 0024）：带上选好的运行环境，说明书里就只给那一种客户端的配置写法
  const prompt = api.agentAuth.onboardPrompt(client);
  const goCode = (e: FormEvent) => {
    e.preventDefault();
    const c = code.trim();
    if (!c) return;
    onClose();
    router.push(`/agents/connect/?code=${encodeURIComponent(c)}`);
  };
  const stepKey = (["link", "approve"] as const)[step];

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t("agents.connect")}
      width={640}
      footer={
        <>
          {step > 0 ? <Button variant="ghost" onClick={() => setStep(0)}>{t("connect.back")}</Button> : <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>}
          {step < 1 ? <Button variant="primary" onClick={() => setStep(1)}>{t("connect.gotCode")}</Button> : <Button variant="primary" form="connect-code-form" type="submit" disabled={!code.trim()}>{t("connect.openApprove")}</Button>}
        </>
      }
    >
      <WizardSteps current={stepKey} className="mb-4" />
      {step === 0 && (
        <div className="space-y-3" data-wizard-step="link">
          {/* 首选也是唯一的主路径：把链接贴给 Agent，由它带着人走完（ADR 0024）。
              运行环境只是给链接加个 ?client=，降为链接下面的一排小选项，不再单占一步。 */}
          <p className="text-caption text-ink-muted">{t("connect.pasteHint")}</p>
          <CopyLine text={prompt} />
          <ol className="flex flex-col gap-1">
            {([t("onboard.next1"), t("onboard.next2"), t("onboard.next3"), t("onboard.next4")] as const).map((line, i) => (
              <li key={i} className="flex items-baseline gap-2 text-caption text-ink-subtle">
                <span className="eyebrow shrink-0 text-ink-tertiary">{String(i + 1).padStart(2, "0")}</span>
                <span className="min-w-0">{line}</span>
              </li>
            ))}
          </ol>
          <div className="flex flex-wrap items-center gap-2 border-t border-hairline pt-3">
            <span className="text-caption text-ink-subtle">{t("connect.clientPick")}</span>
            {DEVICE_CLIENTS.map((c) => (
              <button key={c} type="button" role="radio" aria-checked={client === c} onClick={() => setClient(c)} data-client={c}
                className={cx("pressable rounded-sm border px-2 py-1 text-caption", client === c ? "border-accent bg-accent-bg text-accent-hover" : "border-hairline bg-surface-1 text-ink-muted hover:border-hairline-strong")}>
                {clientTitle(c)}
              </button>
            ))}
          </div>
          <div className="border-t border-hairline pt-3" data-terminal="">
            <button type="button" className="pressable inline-flex items-center gap-1 text-caption text-ink-muted hover:text-ink" onClick={() => setTermOpen((v) => !v)} aria-expanded={termOpen}>
              {termOpen ? <IconChevronDown size={14} /> : <IconChevronRight size={14} />}
              {t("connect.orTerminal")}
            </button>
            {termOpen && (
              <div className="mt-2 space-y-2">
                <p className="text-caption text-ink-muted">{t("connect.commandHint", { client: clientTitle(client) })}</p>
                <CopyLine text={command} />
                <p className="text-caption text-ink-subtle">{t("connect.commandNote")}</p>
              </div>
            )}
          </div>
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
      {step === 1 && (
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
