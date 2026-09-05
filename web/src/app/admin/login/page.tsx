"use client";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { api, MOCK } from "@/lib/api";
import { errorMessage } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { useAdmin } from "@/components/AdminShell";
import { BrandCanvas } from "@/components/BrandCanvas";
import { sceneEmit, sceneFormProps } from "@/components/space/sceneBus";
import { Button, Field, Input } from "@/components/ui";

export default function AdminLoginPage() {
  const router = useRouter();
  const { refresh } = useAdmin();
  const [email, setEmail] = useState(MOCK ? "admin@axiomos.example" : "");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    sceneEmit("submit");
    try {
      await api.admin.login(email, password);
      await sceneEmit("success");
      refresh();
      router.replace("/admin/");
    } catch (err) { sceneEmit("failure"); setError(errorMessage(err)); } finally { setBusy(false); }
  };

  return (
    <BrandCanvas
      eyebrow={t("bridge.console")}
      headline={<>{t("admin.login.headlinePre")}<span className="text-accent-hover">{t("admin.login.headlineKey")}</span>{t("admin.login.headlinePost")}</>}
      subtitle={t("admin.login.tagline")}
    >
      <form onSubmit={submit} {...sceneFormProps()}>
        <h2 className="text-title-lg text-ink">{t("admin.login.title")}</h2>
        <div className="mt-5 space-y-4">
          <Field eyebrow label={t("common.email")}><Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="username" required /></Field>
          <Field eyebrow label={t("common.password")} error={error}><Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" /></Field>
          <Button type="submit" variant="primary" className="w-full" disabled={busy}>{busy ? t("login.busy") : t("login.submit")}</Button>
          {MOCK && <p className="text-caption text-ink-subtle">{t("login.mockHint")}</p>}
        </div>
      </form>
    </BrandCanvas>
  );
}
