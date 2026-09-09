"use client";
import { t } from "@/lib/i18n";
import { PageHeader } from "@/components/ui";
import { ScheduleView } from "@/components/schedule/ScheduleView";

/** 「我的日程」（ADR 0032）：侧栏第二个入口，与「我的工作」同级。 */
export default function SchedulePage() {
  return (
    <div>
      <PageHeader title={t("nav.schedule")} description={t("schedule.description")} />
      <ScheduleView />
    </div>
  );
}
