"use client";
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type KeyboardEvent, type ReactNode, type Ref } from "react";
import { GridLayout, moveElement, useContainerWidth, verticalCompactor, type EventCallback, type Layout, type LayoutItem, type ResizeHandleAxis } from "react-grid-layout";
import { GRID_COLS, GRID_GAP_PX, GRID_MAX_H, GRID_ROW_PX, type BlockKey, type LayoutBlock, type WorkspaceCatalog } from "@/lib/api";
import { t } from "@/lib/i18n";
import { IconClose, IconPlus } from "@/components/icons";
import { Button, Dialog, ListSkeleton, cx } from "@/components/ui";
import { BlockFrameContext } from "./BlockPanel";
import { blockRegistry } from "./registry";

/*
 * 工作台的 12 栏网格（DESIGN.md §13，ADR 0015 补记三）。「我的工作」与组织设置的角色布局编辑器共用。
 * - 区块带坐标 {x, y, w, h}：行高单位 120px、间距 16px，宽 min_w…12、高 1…6 的任意整数。
 * - 排布、拖动、吸附、碰撞让位与向上压实交给 react-grid-layout（v2：GridLayout + useContainerWidth + verticalCompactor），
 *   样式全部用本系统的 token 覆盖（globals.css 的 .react-grid-* / .ws-* 规则）。
 * - 只读：同一张网格，禁用拖动与缩放；有空洞的旧布局在挂载时被压实。
 * - 编辑（editing）：按住区块任意位置拖动即移动（编辑态下区块内容不响应点击，链接 / 按钮 / 输入框除外）；
 *   右边缘、下边缘、右下角是缩放把手（悬停显现 2px 强调色）；到 min_w 时停住并在右边缘闪一下（危险色，200ms，一次）。
 *   每块只剩右上角的 ×；网格下方一枚 3 栏宽的「添加区块」瓦片打开目录，新块放到最下方空位。
 * - 键盘：区块聚焦后方向键移一格、Shift + 方向键改一格大小、Delete 移除；结果用 aria-live 播报。
 * - 窄于 1024px：单栏按 y 排序纵向堆叠，不能拖动（编辑态给一句说明，× 与添加仍可用）。
 * 所有改动通过 onChange 交给调用方，保存与否由调用方决定。
 */

const WIDE = "(min-width: 1024px)";
const subscribeWide = (cb: () => void) => {
  const mq = window.matchMedia(WIDE);
  mq.addEventListener("change", cb);
  return () => mq.removeEventListener("change", cb);
};
/** ≥ 1024px 时网格才是 12 栏；更窄时全部单栏 */
const useWide = () => useSyncExternalStore(subscribeWide, () => window.matchMedia(WIDE).matches, () => true);

/** 目录（接口）优先，没有目录时用 registry 的兜底 */
export function sizeRules(catalog: WorkspaceCatalog | null, key: BlockKey): { minW: number; defaultW: number; defaultH: number } {
  const def = catalog?.blocks.find((b) => b.key === key);
  const entry = blockRegistry[key];
  return { minW: def?.min_w ?? entry?.minW ?? 3, defaultW: def?.default_w ?? entry?.defaultW ?? 12, defaultH: def?.default_h ?? entry?.defaultH ?? 2 };
}

/** h 行区块的像素高：h × 120 + (h − 1) × 16 */
export const blockHeightPx = (h: number) => h * GRID_ROW_PX + (h - 1) * GRID_GAP_PX;

const RESIZE_HANDLES: ResizeHandleAxis[] = ["e", "s", "se"];
/** 编辑态下这些元素上按下不算拖动（RGL 自己还会加上缩放把手） */
const DRAG_CANCEL = "a,button,input,select,textarea,[data-no-drag]";
const GRID_CONFIG = { cols: GRID_COLS, rowHeight: GRID_ROW_PX, margin: [GRID_GAP_PX, GRID_GAP_PX] as const, containerPadding: [0, 0] as const, maxRows: Infinity };

const sameItem = (a: LayoutBlock, b: LayoutItem) => a.x === b.x && a.y === b.y && a.w === b.w && a.h === b.h;

export function WorkspaceGrid({
  blocks,
  catalog,
  titles,
  editing = false,
  onChange,
  className,
}: {
  blocks: LayoutBlock[];
  catalog: WorkspaceCatalog | null;
  /** 后端按语言给的区块名（GET /workspace 的 title）；目录里的标题优先，其次这里，再退回字典 */
  titles?: Partial<Record<BlockKey, string>>;
  editing?: boolean;
  onChange?: (blocks: LayoutBlock[]) => void;
  className?: string;
}) {
  const wide = useWide();
  const [picking, setPicking] = useState(false);
  const [announce, setAnnounce] = useState("");
  const [flash, setFlash] = useState<BlockKey | null>(null);
  const titleOf = useCallback(
    (k: BlockKey) => catalog?.blocks.find((b) => b.key === k)?.title || titles?.[k] || t(`block.${k}`),
    [catalog, titles],
  );
  const known = useMemo(() => blocks.filter((b) => blockRegistry[b.key]), [blocks]);
  const minWOf = useCallback((k: BlockKey) => sizeRules(catalog, k).minW, [catalog]);

  // ---- 与 react-grid-layout 之间的换算 ----
  const layout = useMemo<LayoutItem[]>(
    () => known.map((b) => ({ i: b.key, x: b.x, y: b.y, w: b.w, h: b.h, minW: Math.min(minWOf(b.key), GRID_COLS), maxW: GRID_COLS, minH: 1, maxH: GRID_MAX_H })),
    [known, minWOf],
  );
  // 事件回调里读"最新一份"布局：提交后同步进 ref（不在渲染期写 ref）
  const blocksRef = useRef(blocks);
  const layoutRef = useRef(layout);
  useEffect(() => {
    blocksRef.current = blocks;
    layoutRef.current = layout;
  }, [blocks, layout]);
  const change = useCallback((next: LayoutBlock[]) => onChange?.(next), [onChange]);
  /** RGL 给回的布局 → 我们的区块数组（保持原顺序；目录里没有的键丢掉）。没有实际变化时不触发 onChange。 */
  const applyLayout = useCallback(
    (next: Layout) => {
      const cur = blocksRef.current;
      const out: LayoutBlock[] = [];
      let changed = cur.some((b) => !blockRegistry[b.key]);
      for (const b of cur) {
        const l = next.find((x) => x.i === b.key);
        if (!l) continue;
        if (!sameItem(b, l)) changed = true;
        out.push({ key: b.key, x: l.x, y: l.y, w: l.w, h: l.h });
      }
      if (changed) change(out);
    },
    [change],
  );

  // ---- 到 min_w 时边缘闪一下（一次缩放过程只闪一次） ----
  const flashTimer = useRef(0);
  const flashMin = useCallback((key: BlockKey) => {
    setFlash(key);
    window.clearTimeout(flashTimer.current);
    flashTimer.current = window.setTimeout(() => setFlash(null), 240);
  }, []);
  useEffect(() => () => window.clearTimeout(flashTimer.current), []);
  const resizeSession = useRef<{ key: BlockKey; node: HTMLElement; flashed: boolean; onMove: (e: PointerEvent) => void } | null>(null);
  const endResizeSession = useCallback(() => {
    const s = resizeSession.current;
    if (s) window.removeEventListener("pointermove", s.onMove);
    resizeSession.current = null;
  }, []);
  useEffect(() => endResizeSession, [endResizeSession]);
  const onResizeStart = useCallback<EventCallback>(
    (_layout, _old, item, _ph, _e, node) => {
      endResizeSession();
      if (!item || !node) return;
      const key = item.i as BlockKey;
      const onMove = (e: PointerEvent) => {
        const s = resizeSession.current;
        if (!s || s.flashed) return;
        const cur = layoutRef.current.find((l) => l.i === key);
        if (!cur || cur.w > minWOf(key)) return;
        // 已经最窄了还往里拖（指针进到右边缘以内半栏以上）：闪一下并播报
        const rect = s.node.getBoundingClientRect();
        if (e.clientX < rect.right - 24) {
          s.flashed = true;
          flashMin(key);
          setAnnounce(t("workspace.minWReached", { title: titleOf(key) }));
        }
      };
      resizeSession.current = { key, node, flashed: false, onMove };
      window.addEventListener("pointermove", onMove);
    },
    [endResizeSession, flashMin, minWOf, titleOf],
  );
  const onResize = useCallback<EventCallback>(
    (_layout, old, item) => {
      const s = resizeSession.current;
      if (!s || !item || s.flashed) return;
      if (old && old.w > item.w && item.w === minWOf(item.i as BlockKey)) {
        s.flashed = true;
        flashMin(item.i as BlockKey);
        setAnnounce(t("workspace.minWReached", { title: titleOf(item.i as BlockKey) }));
      }
    },
    [flashMin, minWOf, titleOf],
  );
  const onResizeStop = useCallback<EventCallback>(() => endResizeSession(), [endResizeSession]);

  // ---- 编辑操作 ----
  const remove = (key: BlockKey) => {
    change(verticalCompactor.compact(layoutRef.current.filter((l) => l.i !== key), GRID_COLS).map(fromItem));
    setAnnounce(t("workspace.removed", { title: titleOf(key) }));
  };
  const add = (key: BlockKey) => {
    const rules = sizeRules(catalog, key);
    const bottom = layoutRef.current.reduce((m, l) => Math.max(m, l.y + l.h), 0);
    const next = [...layoutRef.current, { i: key, x: 0, y: bottom, w: rules.defaultW, h: rules.defaultH }];
    change(verticalCompactor.compact(next, GRID_COLS).map(fromItem));
    setAnnounce(t("workspace.added", { title: titleOf(key) }));
  };
  /** 键盘：方向键移一格（碰到别人就让位，再压实），Shift + 方向键改一格大小 */
  const onCellKey = (e: KeyboardEvent<HTMLDivElement>, key: BlockKey) => {
    if (e.target !== e.currentTarget) return;
    const cur = layoutRef.current;
    const item = cur.find((l) => l.i === key);
    if (!item) return;
    const title = titleOf(key);
    if (e.key === "Delete" || e.key === "Backspace") {
      remove(key);
      e.preventDefault();
      return;
    }
    const dx = e.key === "ArrowLeft" ? -1 : e.key === "ArrowRight" ? 1 : 0;
    const dy = e.key === "ArrowUp" ? -1 : e.key === "ArrowDown" ? 1 : 0;
    if (!dx && !dy) return;
    e.preventDefault();
    if (e.shiftKey) {
      const minW = minWOf(key);
      const w = Math.max(minW, Math.min(GRID_COLS - item.x, item.w + dx));
      const h = Math.max(1, Math.min(GRID_MAX_H, item.h + dy));
      if (w === item.w && h === item.h) {
        if (dx < 0 && item.w === minW) {
          flashMin(key);
          setAnnounce(t("workspace.minWReached", { title }));
        }
        return;
      }
      const next = verticalCompactor.compact(cur.map((l) => (l.i === key ? { ...l, w, h } : l)), GRID_COLS);
      change(next.map(fromItem));
      setAnnounce(t("workspace.resized", { title, w, h }));
      return;
    }
    const x = Math.max(0, Math.min(GRID_COLS - item.w, item.x + dx));
    const y = Math.max(0, item.y + dy);
    if (x === item.x && y === item.y) return;
    const moved = verticalCompactor.compact(moveElement(cur, item, x, y, true, false, "vertical", GRID_COLS), GRID_COLS);
    change(moved.map(fromItem));
    const after = moved.find((l) => l.i === key)!;
    setAnnounce(t("workspace.moved", { title, col: after.x + 1, row: after.y + 1 }));
  };

  // ---- 宽度：容器实测；窄屏单栏时不用 RGL ----
  const { width, containerRef } = useContainerWidth({ initialWidth: 1200 });
  const resizeHandle = useCallback(
    (axis: ResizeHandleAxis, ref: Ref<HTMLElement>) => <span ref={ref as Ref<HTMLSpanElement>} className={`react-resizable-handle react-resizable-handle-${axis} ws-rz ws-rz-${axis}`} aria-hidden="true" />,
    [],
  );
  const dragConfig = useMemo(() => ({ enabled: editing, cancel: DRAG_CANCEL, threshold: 3 }), [editing]);
  const resizeConfig = useMemo(() => ({ enabled: editing, handles: RESIZE_HANDLES, handleComponent: resizeHandle }), [editing, resizeHandle]);

  const placed = new Set(blocks.map((b) => b.key));
  const available = catalog?.blocks.filter((b) => !placed.has(b.key) && blockRegistry[b.key]) ?? [];
  // 阅读顺序（先行后栏）：窄屏按它堆叠，序号 01、02… 也按它编，与视觉一致
  const stacked = useMemo(() => [...known].sort((a, b) => a.y - b.y || a.x - b.x), [known]);
  const indexOf = (key: BlockKey) => stacked.findIndex((b) => b.key === key) + 1;

  const renderBlock = (b: LayoutBlock) => {
    const Block = blockRegistry[b.key].component;
    const title = titleOf(b.key);
    const removeBtn = editing ? (
      <button
        type="button"
        className="pressable inline-flex h-7 w-7 items-center justify-center rounded-md text-ink-subtle hover:bg-surface-3 hover:text-danger"
        aria-label={t("workspace.remove", { title })}
        title={t("workspace.remove", { title })}
        onClick={() => remove(b.key)}
      >
        <IconClose />
      </button>
    ) : undefined;
    return (
      <BlockFrameContext.Provider value={{ editing, remove: removeBtn }}>
        <Block index={indexOf(b.key)} title={title} w={b.w} h={b.h} compact={wide && b.w <= 4} dense={b.h === 1} />
      </BlockFrameContext.Provider>
    );
  };

  return (
    <>
      <div ref={containerRef} className={cx("ws-wrap", className)} data-editing={editing || undefined}>
        {wide ? (
          <GridLayout
            width={width}
            layout={layout}
            gridConfig={GRID_CONFIG}
            dragConfig={dragConfig}
            resizeConfig={resizeConfig}
            compactor={verticalCompactor}
            onLayoutChange={applyLayout}
            onResizeStart={onResizeStart}
            onResize={onResize}
            onResizeStop={onResizeStop}
            className="ws-grid"
          >
            {known.map((b) => (
              <div
                key={b.key}
                className="ws-cell"
                data-editing={editing || undefined}
                data-flash={flash === b.key || undefined}
                tabIndex={editing ? 0 : undefined}
                aria-label={editing ? t("workspace.blockLabel", { title: titleOf(b.key), col: b.x + 1, row: b.y + 1, w: b.w, h: b.h }) : undefined}
                onKeyDown={editing ? (e) => onCellKey(e, b.key) : undefined}
              >
                {renderBlock(b)}
              </div>
            ))}
          </GridLayout>
        ) : (
          <div className="ws-stack">
            {stacked.map((b) => (
              <div key={b.key} className="ws-cell" data-editing={editing || undefined} style={{ height: blockHeightPx(b.h) }}>
                {renderBlock(b)}
              </div>
            ))}
          </div>
        )}
        {editing && (
          <div className="mt-4 flex flex-wrap items-center gap-4">
            <button type="button" className="ws-add" onClick={() => setPicking(true)}>
              <IconPlus />
              <span className="text-body">{t("workspace.add")}</span>
            </button>
            {!wide && <p className="min-w-0 flex-1 text-caption text-ink-subtle">{t("workspace.narrowEdit")}</p>}
          </div>
        )}
      </div>
      {editing && (
        <p className="sr-only" role="status" aria-live="polite">
          {announce}
        </p>
      )}
      {editing && (
        <Dialog
          open={picking}
          onClose={() => setPicking(false)}
          title={t("workspace.add")}
          width={520}
          footer={
            <Button variant="primary" onClick={() => setPicking(false)}>
              {t("workspace.done")}
            </Button>
          }
        >
          <p className="text-caption text-ink-subtle">{t("workspace.addHint")}</p>
          {!catalog ? (
            <ListSkeleton rows={4} />
          ) : available.length === 0 ? (
            <p className="py-6 text-center text-body text-ink-subtle">{t("workspace.allPlaced")}</p>
          ) : (
            <ul className="divide-y divide-hairline rounded-md border border-hairline">
              {available.map((d) => (
                <li key={d.key}>
                  <button type="button" className="pressable flex w-full items-start gap-3 px-3 py-2.5 text-left hover:bg-surface-3" onClick={() => add(d.key)}>
                    <span className="mt-0.5 inline-flex shrink-0 text-ink-subtle">
                      <IconPlus />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block text-body text-ink">{d.title}</span>
                      {d.description && <span className="block text-caption text-ink-subtle">{d.description}</span>}
                    </span>
                    <span className="eyebrow shrink-0 self-center text-ink-tertiary">{t("workspace.addSize", { w: d.default_w, h: d.default_h })}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Dialog>
      )}
    </>
  );
}

const fromItem = (l: LayoutItem): LayoutBlock => ({ key: l.i as BlockKey, x: l.x, y: l.y, w: l.w, h: l.h });

/** 「宽×高」小读数，组织设置的区块芯片与预设清单用 */
export function SizeChip({ block, className }: { block: LayoutBlock; className?: string }): ReactNode {
  return <span className={cx("font-mono text-[11px] tabular-nums text-ink-tertiary", className)}>{t("workspace.sizeChip", { w: block.w, h: block.h })}</span>;
}
