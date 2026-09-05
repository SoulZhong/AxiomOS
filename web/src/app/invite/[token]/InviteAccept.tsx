"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { api } from "@/lib/api";
import { errorMessage, useLoad, useRouteId } from "@/lib/hooks";
import { normalizeLocale, setLocale, t } from "@/lib/i18n";
import { BrandCanvas } from "@/components/BrandCanvas";
import { sceneEmit, sceneFormProps } from "@/components/space/sceneBus";
import { Button, ErrorBox, Field, Input, Skeleton } from "@/components/ui";

/** 公开的邀请接受页：GET /invitations/{token} 看组织与邮箱，填姓名和密码后 POST accept，成功即已登录，去首页。 */
export function InviteAccept() {
  const token = useRouteId();
  const router = useRouter();
  const info = useLoad(() => (token ? api.invitations.get(token) : Promise.reject(new Error(t("invite.invalid")))), [token]);
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [prefilled, setPrefilled] = useState(false);
  if (info.data && !prefilled) { setPrefilled(true); setName(info.data.name ?? ""); }

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true); setError(null);
    sceneEmit("submit");
    try {
      const s = await api.invitations.accept(token, { name: name.trim(), password });
      await sceneEmit("success");
      const l = normalizeLocale(s.member.locale);
      if (l) setLocale(l);
      router.replace("/");
    } catch (err) { sceneEmit("failure"); setError(errorMessage(err)); } finally { setBusy(false); }
  };

  return (
    <BrandCanvas>
      <h2 className="text-title-lg text-ink">{t("invite.title")}</h2>
      {!token || info.loading ? (
        <div className="mt-4 space-y-3" aria-busy="true">
          <Skeleton className="w-3/4" />
          <Skeleton className="h-8" />
          <Skeleton className="h-8" />
        </div>
      ) : info.error || !info.data ? (
        <div className="mt-4"><ErrorBox message={info.error ?? t("invite.invalid")} onRetry={info.reload} /></div>
      ) : info.data.accepted ? (
        <>
          <p className="mt-3 text-body text-ink-muted">{t("invite.accepted")}</p>
          <Link href="/login/" className="mt-5 inline-flex"><Button variant="primary">{t("invite.goLogin")}</Button></Link>
        </>
      ) : info.data.expired ? (
        <p className="mt-3 text-body text-danger">{t("invite.expired")}</p>
      ) : (
        <form onSubmit={submit} className="mt-1" {...sceneFormProps()}>
          <p className="mb-5 text-body text-ink-muted">{t("invite.intro", { org: info.data.organization_name })}</p>
          <div className="space-y-4">
            <Field eyebrow label={t("invite.email")}><Input value={info.data.email} disabled /></Field>
            <Field eyebrow label={t("invite.name")}><Input value={name} onChange={(e) => setName(e.target.value)} required autoFocus autoComplete="name" /></Field>
            <Field eyebrow label={t("invite.password")} hint={t("invite.passwordHint")} error={error}><Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoComplete="new-password" /></Field>
            <Button type="submit" variant="primary" className="w-full" disabled={busy}>{busy ? t("invite.busy") : t("invite.submit")}</Button>
          </div>
        </form>
      )}
    </BrandCanvas>
  );
}
