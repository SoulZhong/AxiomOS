"use client";
import { api } from "@/lib/api";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { IconSprint } from "@/components/icons";
import { Odometer } from "@/components/instruments/Odometer";
import { Sparkline } from "@/components/instruments/Sparkline";
import { Panel, Skeleton } from "@/components/ui";

/** 迭代速度：最近 5 个已结束迭代各自完成的工作量（迷你折线）与平均值（滚动数字）。 */
export function VelocityPanel({ team, index }: { team?: string; index?: number }) {
  const v = useLoad(() => api.sprints.velocity({ team: team || undefined }), [team]);
  const data = v.data;
  const avg = data ? (Number.isInteger(data.average_points) ? String(data.average_points) : data.average_points.toFixed(1)) : "0";
  return (
    <Panel index={index} icon={<IconSprint />} title={t("sprints.velocity")} telemetry={data ? t("sprints.velocityCaption", { n: data.sprints.length }) : undefined}>
      {!data ? (
        <div className="space-y-2" aria-busy="true"><Skeleton className="h-6 w-24" /><Skeleton className="h-2.5 w-48" /></div>
      ) : data.sprints.length === 0 ? (
        <p className="text-body text-ink-subtle">{t("sprints.velocityEmpty")}</p>
      ) : (
        <div className="sp-velocity">
          <div>
            <span className="sp-velocity-value"><Odometer value={avg} /><span className="sp-velocity-unit">{t("sprints.pointsUnit")}</span></span>
            <span className="mt-1 block text-caption text-ink-subtle sm:hidden">{t("sprints.velocityCaption", { n: data.sprints.length })}</span>
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-x-6 gap-y-2">
            <Sparkline points={data.sprints.map((s) => s.points_done)} width={160} height={36} label={t("sprints.velocity")} />
            <div className="sp-velocity-list">
              {data.sprints.map((s) => (
                <span key={s.id} title={t("sprints.velocityPoints", { name: s.name, n: s.points_done })}>{s.name} <b>{s.points_done}</b></span>
              ))}
            </div>
          </div>
        </div>
      )}
    </Panel>
  );
}
