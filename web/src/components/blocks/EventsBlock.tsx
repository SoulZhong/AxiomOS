"use client";
import { api } from "@/lib/api";
import { useLoad } from "@/lib/hooks";
import { t } from "@/lib/i18n";
import { EventList } from "@/components/EventList";
import { IconLog } from "@/components/icons";
import { ListSkeleton } from "@/components/ui";
import { BlockPanel, type BlockProps } from "./BlockPanel";

/** 最近动态：当前范围的动态流，EventList 自己每 30s 轮询。 */
export function EventsBlock({ index, title, noLink }: BlockProps) {
  const events = useLoad(() => api.events.list({ limit: 20 }), []);
  const rows = events.data ?? [];
  return (
    <BlockPanel
      index={index}
      noLink={noLink}
      icon={<IconLog />}
      title={title ?? t("block.events")}
      telemetry={events.data ? t("panel.rows", { n: rows.length }) : undefined}
      loading={events.loading && !events.data}
      error={events.error}
      onRetry={events.reload}
      empty={!!events.data && rows.length === 0}
      emptyText={t("block.events.empty")}
      skeleton={<ListSkeleton rows={5} />}
    >
      <EventList events={rows} />
    </BlockPanel>
  );
}
