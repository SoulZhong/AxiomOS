"use client";
import { t } from "@/lib/i18n";
import { IconChevronLeft, IconChevronRight } from "@/components/icons";
import { Button, Segmented } from "@/components/ui";
import { SCALES, scaleTitle, type Scale } from "./scales";
import type { GanttScale } from "./useGanttScale";

/*
 * 甘特图工具栏里与"行是什么"无关的两组控件（任务甘特图与目标甘特图共用）：
 *   - GanttZoomControls：六档刻度 + −/+ + 适应全部
 *   - GanttNavControls：上一段 / 今天 / 下一段 + 延长 / 缩短 + 全屏
 */

/**
 * scales 是这张图开放的档位（由细到粗），默认六档全开；路线图只传周 / 月 / 季度 / 年（ADR 0022）。
 * onScale 让调用方在切档时顺便记住选择，不传就直接切。
 */
export function GanttZoomControls({ g, onFit, canFit, scales = SCALES, onScale }: { g: GanttScale; onFit: () => void; canFit: boolean; scales?: readonly Scale[]; onScale?: (s: Scale) => void }) {
  const set = onScale ?? g.setScale;
  return (
    <>
      <Segmented size="sm" label={t("gantt.zoom")} value={g.scale} options={scales.map((z) => [z, scaleTitle(z)])} onChange={set} aria-label={t("gantt.zoom")} />
      <span className="gantt-btns" title={t("gantt.zoomTip")}>
        <Button size="sm" variant="ghost" onClick={() => set(scales[Math.min(scales.length - 1, scales.indexOf(g.scale) + 1)])} disabled={g.scale === scales[scales.length - 1]} aria-label={t("gantt.zoomOut")} title={t("gantt.zoomOut")}>−</Button>
        <Button size="sm" variant="ghost" onClick={() => set(scales[Math.max(0, scales.indexOf(g.scale) - 1)])} disabled={g.scale === scales[0]} aria-label={t("gantt.zoomIn")} title={t("gantt.zoomIn")}>+</Button>
        <Button size="sm" variant="ghost" onClick={onFit} disabled={!canFit}>{t("gantt.fit")}</Button>
      </span>
    </>
  );
}

export function GanttNavControls({ g, fs }: { g: GanttScale; fs: { active: boolean; toggle: () => void } }) {
  return (
    <>
      <Button size="sm" icon={<IconChevronLeft />} onClick={() => g.shift(-1)}>{t(`gantt.nav.prev.${g.scale}`)}</Button>
      <Button size="sm" onClick={g.goToday}>{t("gantt.today")}</Button>
      <Button size="sm" onClick={() => g.shift(1)}>{t(`gantt.nav.next.${g.scale}`)}<IconChevronRight /></Button>
      <Button size="sm" variant="ghost" onClick={g.canShorten ? g.shorten : g.extend}>{g.canShorten ? t("gantt.shorten") : t("gantt.extend")}</Button>
      <span className="gantt-sep" aria-hidden="true" />
      <Button size="sm" variant={fs.active ? "default" : "ghost"} onClick={fs.toggle} title={t("gantt.fullscreenTip")} aria-pressed={fs.active}>
        {fs.active ? t("gantt.exitFullscreen") : t("gantt.fullscreen")}
      </Button>
    </>
  );
}
