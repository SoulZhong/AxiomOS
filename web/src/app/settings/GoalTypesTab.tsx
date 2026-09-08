"use client";
import Link from "next/link";
import { useMemo, useState, type FormEvent } from "react";
import { api, GOAL_DATE_PRECISIONS, type GoalDefaultPrecision, type GoalType, type GoalTypeInput } from "@/lib/api";
import { errorMessage, useAction, useLoad } from "@/lib/hooks";
import { t, type Key } from "@/lib/i18n";
import { usePointerDrag } from "@/components/board/usePointerDrag";
import { GOAL_TYPE_ICON_NAMES, GoalTypeIcon, IconGrip, IconPlus, IconTrash } from "@/components/icons";
import { InlineSelect, InlineStatusMark, InlineTextInput, useInlineSaves, type InlineOption } from "@/components/inline";
import { useToast } from "@/components/toast";
import { Button, ConsequenceDialog, Empty, ErrorBox, Input, Panel, Table, TableSkeleton, Tag, cx } from "@/components/ui";

/*
 * 组织设置 · 目标类型（ADR 0023）：组织自己维护的一张词表，与能力标签、价格表同级。
 *
 * 这一页最重要的一句话写在标题下面：**类型只用来分类和显示**——不决定流程、权限或成本归口。
 * ADR 0023 明确否决了「给目标类型配流程或必填字段」，所以这里除了颜色、图标、次序，
 * 唯一带行为的字段是「默认时间粒度」，而它只是新建目标时的**预填**，不是限制。
 * 想在这一页加字段之前，先回去读那份 ADR 的「被否决的」一节。
 *
 * 交互都是就地的（DESIGN.md 的就地编辑）：点名字改名、色点与图标是两个下拉、粒度是一个下拉，
 * 一行一个请求、行尾一枚状态位。次序靠拖左边的把手（也能用键盘上下移）。
 * 颜色只给设计规范里的那一组，不给自由填色号；图标只给现成的那几枚线性图标，不接 emoji。
 */

/** 颜色只能从设计规范的这一组里挑（DESIGN.md「唯一强调色 + 语义色」），不开自由色号。 */
const COLORS: Array<{ key: string; hex: string }> = [
  { key: "accent", hex: "#5e6ad2" },
  { key: "success", hex: "#27a644" },
  { key: "warning", hex: "#d9a53b" },
  { key: "danger", hex: "#e5484d" },
  { key: "neutral", hex: "#8a8f98" },
];
const DEFAULT_COLOR = COLORS[0].hex;
const DEFAULT_ICON = "target";
const colorName = (hex: string) => {
  const hit = COLORS.find((c) => c.hex.toLowerCase() === hex.toLowerCase());
  return hit ? t(`goalType.color.${hit.key}` as Key) : t("goalType.color.custom");
};

const Dot = ({ color, size = 8 }: { color: string; size?: number }) => (
  <i className="inline-block shrink-0 rounded-full" style={{ width: size, height: size, background: color || DEFAULT_COLOR }} aria-hidden="true" />
);

const colorOptions = (): InlineOption[] => COLORS.map((c) => ({ value: c.hex, label: t(`goalType.color.${c.key}` as Key), icon: <Dot color={c.hex} /> }));
const iconOptions = (color: string): InlineOption[] =>
  GOAL_TYPE_ICON_NAMES.map((n) => ({ value: n, label: t(`goalType.icon.${n}` as Key), icon: <GoalTypeIcon name={n} size={14} style={{ color }} /> }));
const precisionOptions = (): InlineOption[] => GOAL_DATE_PRECISIONS.map((p) => ({ value: p, label: t(`date_precision.${p}` as Key) }));

export function GoalTypesTab() {
  const types = useLoad(() => api.org.goalTypes.list(), []);
  const saves = useInlineSaves();
  const [deactivating, setDeactivating] = useState<GoalType | null>(null);
  const [removing, setRemoving] = useState<GoalType | null>(null);
  const [refused, setRefused] = useState<string | null>(null);
  const { busy, run } = useAction();
  // 拖动排序时的临时次序：拖完写回服务端，重新取数之前先按它画（否则行会跳回去）
  const [order, setOrder] = useState<string[] | null>(null);

  const list = useMemo(() => {
    const rows = types.data ?? [];
    if (!order) return rows;
    const by = new Map(rows.map((x) => [x.id, x]));
    const out = order.map((id) => by.get(id)).filter((x): x is GoalType => !!x);
    return out.length === rows.length ? out : rows;
  }, [types.data, order]);

  const patch = (ty: GoalType, p: Partial<GoalTypeInput>) => saves.run(ty.id, () => api.org.goalTypes.update(ty.id, p), () => types.reload());

  // 拖动排序：没有批量排序接口，一次拖动就把次序变了的行各发一条 PATCH { sort }（次序即 1..n）
  const commitOrder = async (ids: string[]) => {
    setOrder(ids);
    const by = new Map((types.data ?? []).map((x) => [x.id, x]));
    const moved = ids.map((id, i) => ({ ty: by.get(id), sort: i + 1 })).filter((x): x is { ty: GoalType; sort: number } => !!x.ty && x.ty.sort !== x.sort);
    if (!moved.length) return;
    for (const { ty, sort } of moved) {
      const ok = await saves.run(ty.id, () => api.org.goalTypes.update(ty.id, { sort }));
      if (!ok) break;
    }
    types.reload();
  };
  const moveTo = (id: string, target: string | null): string[] | null => {
    const ids = list.map((x) => x.id);
    const from = ids.indexOf(id);
    const to = target ? ids.indexOf(target) : -1;
    if (from < 0 || to < 0 || from === to) return null;
    const next = [...ids];
    next.splice(from, 1);
    next.splice(to, 0, id);
    return next;
  };
  const { drag, start } = usePointerDrag({ dropSelector: "[data-drop]", onDrop: (id, target) => { const next = moveTo(id, target); if (next) void commitOrder(next); } });
  // 拖动过程中就按落点画，松手前就看得出结果
  const preview = drag?.over ? moveTo(drag.id, drag.over) : null;
  const shown = useMemo(() => {
    if (!preview) return list;
    const by = new Map(list.map((x) => [x.id, x]));
    return preview.map((id) => by.get(id)).filter((x): x is GoalType => !!x);
  }, [list, preview]);
  const moveBy = (id: string, delta: number) => {
    const ids = list.map((x) => x.id);
    const i = ids.indexOf(id);
    const j = i + delta;
    if (i < 0 || j < 0 || j >= ids.length) return;
    void commitOrder(moveTo(id, ids[j]) ?? ids);
  };

  const remove = async (ty: GoalType) => {
    setRefused(null);
    try {
      await api.org.goalTypes.remove(ty.id);
      setRemoving(null);
      types.reload();
    } catch (e) {
      // 还有目标在用：服务端给的是一整句理由，原样摆进对话框，并把「改成停用」摆成下一步
      setRefused(errorMessage(e));
    }
  };
  const deactivate = async (ty: GoalType, active: boolean) => {
    if (await run(ty.id, () => api.org.goalTypes.update(ty.id, { active }))) {
      setDeactivating(null);
      setRemoving(null);
      setRefused(null);
      types.reload();
    }
  };

  const cols = 6;
  return (
    <Panel
      index={1}
      title={t("settings.tab.goal-types")}
      padded={false}
      between={<p className="border-b border-hairline px-4 py-2.5 text-body text-ink-subtle">{t("settings.goalTypes.description")}</p>}
      telemetry={types.data ? `${types.data.length}` : undefined}
    >
      {types.loading && !types.data ? (
        <TableSkeleton rows={3} cols={cols} />
      ) : types.error ? (
        <div className="p-4"><ErrorBox message={types.error} onRetry={types.reload} /></div>
      ) : (
        <Table>
          <thead>
            <tr>
              <th className="w-0" aria-label={t("settings.goalTypes.reorder")} />
              <th>{t("settings.goalTypes.col.name")}</th>
              <th>{t("settings.goalTypes.col.color")}</th>
              <th>{t("settings.goalTypes.col.icon")}</th>
              <th>{t("settings.goalTypes.col.precision")}</th>
              <th className="num">{t("settings.goalTypes.col.goals")}</th>
              <th className="actions w-0" aria-label={t("common.actions")} />
            </tr>
          </thead>
          <tbody>
            {!shown.length && (
              <tr><td colSpan={cols + 1} className="!p-0"><Empty text={t("settings.goalTypes.empty")} /></td></tr>
            )}
            {shown.map((ty) => {
              const st = saves.get(ty.id);
              return (
                <tr key={ty.id} data-drop={ty.id} data-dragging={drag?.id === ty.id ? "" : undefined} className={cx(drag?.id === ty.id && "opacity-50", !ty.active && "text-ink-subtle")}>
                  <td className="w-0 !pr-0">
                    <span
                      role="button"
                      tabIndex={0}
                      className="gt-grip"
                      title={t("settings.goalTypes.reorderHint")}
                      aria-label={t("settings.goalTypes.reorder")}
                      onPointerDown={(e) => start(e, ty.id)}
                      onKeyDown={(e) => {
                        if (e.key === "ArrowUp") { e.preventDefault(); moveBy(ty.id, -1); }
                        else if (e.key === "ArrowDown") { e.preventDefault(); moveBy(ty.id, 1); }
                      }}
                    >
                      <IconGrip size={14} />
                    </span>
                  </td>
                  <td className="min-w-[180px]">
                    <span className="flex items-center gap-2">
                      <Dot color={ty.color} />
                      <InlineTextInput value={ty.name} editable ariaLabel={t("settings.goalTypes.rename")} onChange={(name) => void patch(ty, { name })} className="min-w-0 flex-1" />
                      {!ty.active && <Tag>{t("settings.goalTypes.inactive")}</Tag>}
                      <InlineStatusMark status={st.status} />
                    </span>
                    {st.status === "error" && st.error && <p className="mt-1 text-caption text-danger" role="alert">{st.error}</p>}
                  </td>
                  <td>
                    <InlineSelect
                      portal
                      value={ty.color}
                      options={colorOptions()}
                      editable
                      ariaLabel={t("settings.goalTypes.col.color")}
                      display={<span className="inline-flex items-center gap-1.5"><Dot color={ty.color} /><span className="inl-text">{colorName(ty.color)}</span></span>}
                      onChange={(color) => void patch(ty, { color: color ?? DEFAULT_COLOR })}
                    />
                  </td>
                  <td>
                    <InlineSelect
                      portal
                      value={ty.icon || DEFAULT_ICON}
                      options={iconOptions(ty.color)}
                      editable
                      ariaLabel={t("settings.goalTypes.col.icon")}
                      display={<span className="inline-flex items-center gap-1.5"><GoalTypeIcon name={ty.icon || DEFAULT_ICON} size={14} style={{ color: ty.color || DEFAULT_COLOR }} /><span className="inl-text">{t(`goalType.icon.${ty.icon || DEFAULT_ICON}` as Key)}</span></span>}
                      onChange={(icon) => void patch(ty, { icon: icon ?? DEFAULT_ICON })}
                    />
                  </td>
                  <td>
                    <InlineSelect
                      portal
                      value={ty.default_precision || null}
                      options={precisionOptions()}
                      editable
                      nullable
                      nullLabel={t("settings.goalTypes.precisionNone")}
                      ariaLabel={t("settings.goalTypes.col.precision")}
                      onChange={(p) => void patch(ty, { default_precision: (p ?? "") as GoalDefaultPrecision })}
                    />
                  </td>
                  <td className="num">
                    {ty.goal_count > 0 ? (
                      <Link href={`/goals/?type=${encodeURIComponent(ty.id)}`} className="tabular-nums text-ink-muted hover:text-accent-hover" title={t("settings.goalTypes.viewGoals")} aria-label={t("settings.goalTypes.viewGoals")}>
                        {ty.goal_count}
                      </Link>
                    ) : (
                      <span className="text-ink-subtle">0</span>
                    )}
                  </td>
                  <td className="actions">
                    <span className="row-actions inline-flex gap-1">
                      <Button size="sm" variant="ghost" disabled={busy === ty.id} onClick={() => (ty.active ? setDeactivating(ty) : void deactivate(ty, true))}>
                        {ty.active ? t("settings.goalTypes.deactivate") : t("settings.goalTypes.reactivate")}
                      </Button>
                      <Button size="sm" variant="ghost" className="text-danger hover:!text-danger" icon={<IconTrash />} onClick={() => { setRefused(null); setRemoving(ty); }}>{t("common.delete")}</Button>
                    </span>
                  </td>
                </tr>
              );
            })}
          </tbody>
          <tfoot>
            <tr>
              <td colSpan={cols + 1}>
                <NewTypeForm onCreated={() => { setOrder(null); types.reload(); }} />
              </td>
            </tr>
          </tfoot>
        </Table>
      )}

      <ConsequenceDialog
        open={!!deactivating}
        danger={false}
        title={deactivating ? t("settings.goalTypes.deactivateTitle", { name: deactivating.name }) : ""}
        effects={[t("settings.goalTypes.deactivateEffect")]}
        note={t("settings.goalTypes.deactivateNote")}
        confirmLabel={t("settings.goalTypes.deactivate")}
        busy={!!busy}
        onConfirm={() => deactivating && void deactivate(deactivating, false)}
        onClose={() => setDeactivating(null)}
      />

      {/*
        删除：服务端才知道还有没有目标在用它。被拒时不关对话框——把服务端那一整句原样摆上来，
        底下改说「改成停用」会发生什么，主按钮也跟着换成「改成停用」，旁边留一条去看那些目标的链接。
      */}
      <ConsequenceDialog
        open={!!removing}
        title={removing ? t("settings.goalTypes.deleteTitle", { name: removing.name }) : ""}
        subject={refused ? <span className="text-danger">{refused}</span> : undefined}
        effects={
          refused
            ? [t("settings.goalTypes.deactivateEffect"),
               removing ? <Link key="see" href={`/goals/?type=${encodeURIComponent(removing.id)}`} className="text-accent hover:text-accent-hover">{t("settings.goalTypes.viewGoals")}</Link> : null]
            : [t("settings.goalTypes.deleteEffect.free"), t("settings.goalTypes.deleteEffect.history"), t("settings.goalTypes.deleteEffect.noUndo")]
        }
        confirmLabel={refused ? t("settings.goalTypes.switchToDeactivate") : t("common.delete")}
        danger={!refused}
        busy={!!busy}
        onConfirm={() => removing && (refused ? void deactivate(removing, false) : void remove(removing))}
        onClose={() => { setRemoving(null); setRefused(null); }}
      />
    </Panel>
  );
}

/** 「新建类型」就在清单末尾一行：名字 + 颜色 + 图标 + 默认时间粒度，不另开抽屉。 */
function NewTypeForm({ onCreated }: { onCreated: () => void }) {
  const toast = useToast();
  const [name, setName] = useState("");
  const [color, setColor] = useState(DEFAULT_COLOR);
  const [icon, setIcon] = useState(DEFAULT_ICON);
  const [precision, setPrecision] = useState<GoalDefaultPrecision>("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const created = await api.org.goalTypes.create({ name: name.trim(), color, icon, default_precision: precision });
      toast.ok(t("toast.created", { name: created.name }));
      setName("");
      setColor(DEFAULT_COLOR);
      setIcon(DEFAULT_ICON);
      setPrecision("");
      onCreated();
    } catch (err) {
      const m = errorMessage(err);
      setError(m);
      toast.fail(m);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
      <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("settings.goalTypes.namePlaceholder")} aria-label={t("settings.goalTypes.col.name")} maxLength={20} required className="w-[220px]" />
      <InlineSelect portal value={color} options={colorOptions()} editable ariaLabel={t("settings.goalTypes.col.color")} display={<span className="inline-flex items-center gap-1.5"><Dot color={color} /><span className="inl-text">{colorName(color)}</span></span>} onChange={(v) => setColor(v ?? DEFAULT_COLOR)} />
      <InlineSelect portal value={icon} options={iconOptions(color)} editable ariaLabel={t("settings.goalTypes.col.icon")} display={<span className="inline-flex items-center gap-1.5"><GoalTypeIcon name={icon} size={14} style={{ color }} /><span className="inl-text">{t(`goalType.icon.${icon}` as Key)}</span></span>} onChange={(v) => setIcon(v ?? DEFAULT_ICON)} />
      <InlineSelect portal value={precision || null} options={precisionOptions()} editable nullable nullLabel={t("settings.goalTypes.precisionNone")} ariaLabel={t("settings.goalTypes.col.precision")} onChange={(v) => setPrecision((v ?? "") as GoalDefaultPrecision)} />
      <Button type="submit" size="sm" variant="primary" icon={<IconPlus />} disabled={busy || !name.trim()}>{t("settings.goalTypes.new")}</Button>
      </div>
      <p className={cx("text-caption", error ? "text-danger" : "text-ink-tertiary")} role={error ? "alert" : undefined}>{error ?? t("settings.goalTypes.precisionHint")}</p>
    </form>
  );
}
