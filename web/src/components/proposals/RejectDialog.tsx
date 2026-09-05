"use client";
import { useState } from "react";
import type { Proposal } from "@/lib/api";
import { t } from "@/lib/i18n";
import { Button, Dialog, Field, Textarea } from "@/components/ui";

/** 转舵 240ms（globals.css F.）：确认按钮上的舵轮转四分之一圈，动效放完这一行才翻牌；reduced-motion 时立即翻。 */
export const HELM_MS = 240;
/** 拒绝理由至少这么多字：它是写给 Agent 看的完整句子。 */
export const MIN_REASON = 6;

/**
 * 拒绝一条待确认操作：理由是完整句子，Agent 会读它，所以不做乐观省略——写完才提交。
 * 待确认操作页与「待我处理」共用（DESIGN.md §12）。
 */
export function RejectDialog({ proposal, busy, onClose, onSubmit }: { proposal: Proposal | null; busy: boolean; onClose: () => void; onSubmit: (p: Proposal, reason: string) => Promise<boolean> }) {
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  // 换了一条（或关掉重开）就把输入清空：渲染期比较，不用 effect
  const id = proposal?.id ?? null;
  const [seen, setSeen] = useState(id);
  if (seen !== id) {
    setSeen(id);
    setReason("");
    setError(null);
  }
  const submit = async () => {
    if (!proposal) return;
    if (reason.trim().length < MIN_REASON) {
      setError(t("proposals.reasonTooShort"));
      return;
    }
    // 失败时后端那句完整的理由已经由 Toast 说了，这里只把对话框留着让人改
    await onSubmit(proposal, reason.trim());
  };
  return (
    <Dialog
      open={!!proposal}
      onClose={onClose}
      title={t("proposals.rejectTitle")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="danger" disabled={busy} onClick={() => void submit()}>{t("proposals.reject")}</Button>
        </>
      }
    >
      {proposal && <p className="text-ink-muted">{proposal.summary}</p>}
      <Field label={t("proposals.reasonLabel")} error={error}>
        <Textarea value={reason} onChange={(e) => { setReason(e.target.value); setError(null); }} placeholder={t("proposals.reasonPlaceholder")} autoFocus />
      </Field>
      {/* 说明始终在场：写错了要提示，但"这句话是给 Agent 看的"这件事不能因为报错就消失 */}
      <p className="text-caption text-ink-subtle">{t("proposals.rejectHint", { agent: proposal?.agent.name })}</p>
    </Dialog>
  );
}
