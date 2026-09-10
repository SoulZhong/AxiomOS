"use client";
import { t } from "@/lib/i18n";
import { PageHeader } from "@/components/ui";
import { MeTab } from "../settings/MeTab";

/**
 * 个人设置（DESIGN.md §20）：每个成员自己的页面，从侧栏底部自己的名字进，不在组织设置里。
 * 偏好、通知、外部日历（含订阅链接与对外订阅源）、代码平台身份都在这一页；组织设置只留给管理员。
 */
export default function MePage() {
  return (
    <div>
      <PageHeader title={t("nav.settingsMine")} description={t("settings.descriptionMine")} />
      <MeTab />
    </div>
  );
}
