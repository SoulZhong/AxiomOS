"use client";
import { useCallback, useState } from "react";
import { api, type BlockKey, type LayoutBlock, type Workspace } from "@/lib/api";
import { errorMessage, setQueryParams, useLoad, useQueryParam } from "@/lib/hooks";
import { t, useLocale } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { WorkspaceGrid } from "@/components/blocks/WorkspaceGrid";
import { ScheduleView } from "@/components/schedule/ScheduleView";
import { IconEdit } from "@/components/icons";
import { useToast } from "@/components/toast";
import { Button, Empty, ErrorBox, ListSkeleton, PageHeader, Panel, Tabs } from "@/components/ui";

/*
 * 我的工作 = 工作台（DESIGN.md §12 / §13，ADR 0015 补记二、补记四）：
 * - 整页就是一张 12 栏网格：GET /workspace 给出已解析的区块坐标与宽高（个人微调 → 角色并集 → 默认），WorkspaceGrid 排进网格。
 * - 「待我处理」（inbox：GET /inbox，只看本人、不受范围影响）与「组织概况」（readouts）自补记四起也是区块，默认在最上面，
 *   和别的区块一样可以拖动、改大小、移除；移除后从「添加区块」找回。页面上不再有布局之外的固定区域。
 * - 移除了「待我处理」的人仍能从侧栏「我的工作」的角标看到条数（同一个 GET /inbox/count，30s 轮询）。
 * 页头「编辑布局」把整页切成编辑态：顶部一条固定条（保存 / 取消 / 恢复角色默认），区块可以直接拖动位置、拖边框改大小，
 * 头部右侧出现 ×，网格下方「添加区块」。
 * 保存前所有改动只在本地（draft）；保存 = PUT /workspace/me，成功后按服务端返回的画并提示，失败保留编辑态并把后端的句子放进 Toast。
 */
/** 「我的工作」的两个页签：工作台（默认）与日程（ADR 0032）。?tab=schedule 记在地址栏，不记本地。 */
const HOME_TABS = ["workspace", "schedule"] as const;
type HomeTab = (typeof HOME_TABS)[number];

export default function HomePage() {
  const { session } = useSession();
  const qTab = useQueryParam("tab");
  const tab: HomeTab = qTab === "schedule" ? "schedule" : "workspace";
  const switchTab = useCallback((k: HomeTab) => setQueryParams({ tab: k === "workspace" ? null : k }), []);
  const { locale } = useLocale();
  const toast = useToast();
  const ws = useLoad(() => api.workspace.get(), []);
  // 目录只在第一次进编辑态时取（区块标题、说明、默认尺寸与最小宽度）
  const [wanted, setWanted] = useState(false);
  const catalog = useLoad(() => (wanted ? api.workspace.catalog() : Promise.resolve(null)), [wanted]);
  // 编辑中的布局；null = 不在编辑
  const [draft, setDraft] = useState<LayoutBlock[] | null>(null);
  // 乐观更新：保存后先按本地这一份画，服务端回来再换；ws 重新取数后（切范围等）自动失效
  const [override, setOverride] = useState<{ base: Workspace | null; value: Workspace } | null>(null);
  const [busy, setBusy] = useState(false);
  const workspace = override && override.base === ws.data ? override.value : ws.data;
  const editing = draft !== null;
  // ?edit=1（快速命令「编辑工作台布局」）：布局一到就进编辑态，参数随即清掉
  const qEdit = useQueryParam("edit");
  const [seenEdit, setSeenEdit] = useState<string | null>(null);
  if (qEdit !== seenEdit) {
    setSeenEdit(qEdit);
  }
  if (qEdit && workspace && draft === null) {
    setDraft(workspace.blocks.map(({ key, x, y, w, h }) => ({ key, x, y, w, h })));
    setWanted(true);
    setQueryParams({ edit: null });
  }

  const roleTitle = (name: string) => session?.roles.find((r) => r.name === name)?.title ?? name;
  const joiner = locale === "en-US" ? ", " : "、";
  const sourceText = workspace
    ? workspace.source === "personal"
      ? t("workspace.sourcePersonal")
      : workspace.source === "roles"
        ? t("workspace.sourceRoles", { roles: workspace.roles_used.map(roleTitle).join(joiner) })
        : t("workspace.sourceDefault")
    : "";
  const titles = Object.fromEntries((workspace?.blocks ?? []).map((b) => [b.key, b.title])) as Partial<Record<BlockKey, string>>;

  const beginEdit = () => {
    if (!workspace) return;
    setWanted(true);
    setDraft(workspace.blocks.map(({ key, x, y, w, h }) => ({ key, x, y, w, h })));
  };
  const cancel = () => setDraft(null);

  const save = async () => {
    if (!workspace || !draft) return;
    const prev = workspace;
    const titleOf = (k: BlockKey) => catalog.data?.blocks.find((b) => b.key === k)?.title ?? prev.blocks.find((b) => b.key === k)?.title ?? t(`block.${k}`);
    setOverride({ base: ws.data, value: { ...prev, source: "personal", blocks: draft.map((b) => ({ ...b, title: titleOf(b.key) })) } });
    setBusy(true);
    try {
      const saved = await api.workspace.saveMine(draft);
      setOverride({ base: ws.data, value: saved });
      setDraft(null);
      toast.ok(t("workspace.saved"));
    } catch (e) {
      // 失败：留在编辑态，改动还在 draft 里；页面回到保存前的样子
      setOverride({ base: ws.data, value: prev });
      toast.fail(t("workspace.saveFailed", { reason: errorMessage(e) }));
    } finally {
      setBusy(false);
    }
  };

  const reset = async () => {
    setBusy(true);
    try {
      await api.workspace.resetMine();
      setDraft(null);
      setOverride(null);
      ws.reload();
      toast.ok(t("workspace.resetDone"));
    } catch (e) {
      toast.fail(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div>
      <PageHeader
        title={session ? t("home.greeting", { name: session.member.name }) : t("home.title")}
        description={tab === "schedule" ? t("schedule.description") : editing ? sourceText : t("home.description")}
        actions={
          tab === "workspace" && workspace?.can_customize !== false && !editing ? (
            <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={beginEdit} disabled={!workspace}>
              {t("workspace.edit")}
            </Button>
          ) : undefined
        }
      />
      {!editing && (
        <Tabs value={tab} label={t("home.tabs")} items={HOME_TABS.map((k) => ({ key: k, label: t(`home.tab.${k}`) }))} onChange={switchTab} className="mb-4" />
      )}
      {tab === "schedule" ? <ScheduleView /> : (
      <>

      {editing && (
        <div className="ws-editbar mb-4 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg border border-hairline bg-surface-1 px-3 py-2 shadow-panel" role="toolbar" aria-label={t("workspace.editing")}>
          <span className="text-body font-medium text-ink">{t("workspace.editing")}</span>
          <span className="hidden min-w-0 truncate text-caption text-ink-subtle lg:inline" title={t("workspace.keyboardHint")}>
            {t("workspace.resizeHint")}
          </span>
          <span className="ml-auto flex items-center gap-2">
            {workspace?.source === "personal" && (
              <Button size="sm" variant="ghost" disabled={busy} onClick={() => void reset()}>
                {t("workspace.resetRole")}
              </Button>
            )}
            <Button size="sm" disabled={busy} onClick={cancel}>
              {t("workspace.cancel")}
            </Button>
            <Button size="sm" variant="primary" disabled={busy || draft.length === 0} onClick={() => void save()}>
              {busy ? t("workspace.saving") : t("workspace.save")}
            </Button>
          </span>
        </div>
      )}

      {ws.error && !workspace ? (
        <ErrorBox message={ws.error} onRetry={ws.reload} />
      ) : !workspace ? (
        <div className="ws-grid-skeleton">
          {[12, 12, 6, 6].map((w, i) => (
            <div key={i} className="ws-skeleton" style={{ "--w": w } as React.CSSProperties}>
              <Panel>
                <ListSkeleton rows={w === 12 ? (i === 1 ? 1 : 3) : 5} />
              </Panel>
            </div>
          ))}
        </div>
      ) : editing ? (
        <WorkspaceGrid blocks={draft} catalog={catalog.data} titles={titles} editing onChange={setDraft} />
      ) : workspace.blocks.length === 0 ? (
        <Panel>
          <Empty
            text={t("workspace.empty")}
            action={
              <Button size="sm" icon={<IconEdit />} onClick={beginEdit}>
                {t("workspace.edit")}
              </Button>
            }
          />
        </Panel>
      ) : (
        <WorkspaceGrid blocks={workspace.blocks} catalog={catalog.data} titles={titles} />
      )}
      </>
      )}
    </div>
  );
}
