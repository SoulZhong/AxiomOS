"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { errorMessage, useRouteId } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { Button, Empty, ErrorBox, Panel } from "@/components/ui";

/**
 * `/task-by-number/123/`：按可读序号找到任务后 replace 到 `/tasks/{id}/`（DESIGN.md §20）。
 * 快速命令里输入 `#123` / `123`、外部链接里写序号都走这里；找不到时显示后端那句「没有序号为 N 的任务。」。
 */
export function ByNumber() {
  const router = useRouter();
  const raw = useRouteId();
  const n = raw ? raw.replace(/^#/, "") : null;
  const [error, setError] = useState<{ n: string; message: string } | null>(null);
  useEffect(() => {
    if (!n) return;
    let alive = true;
    api.tasks.byNumber(n).then(
      (task) => alive && router.replace(`/tasks/${encodeURIComponent(task.id)}/`),
      (e: unknown) => alive && setError({ n, message: errorMessage(e) }),
    );
    return () => {
      alive = false;
    };
  }, [n, router]);
  if (!n || !/^\d+$/.test(n)) return <Panel><Empty text={t("task.badNumber")} action={<Link href="/tasks/" className="inline-flex"><Button tabIndex={-1}>{t("task.breadcrumb")}</Button></Link>} /></Panel>;
  if (error && error.n === n) return <Panel><ErrorBox message={error.message} /><p className="mt-3"><Link href="/tasks/" className="text-caption text-accent-hover hover:underline">{t("task.breadcrumb")}</Link></p></Panel>;
  return <p className="text-caption text-ink-subtle" role="status">{t("task.openingNumber", { n })}</p>;
}
