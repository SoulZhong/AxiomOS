"use client";
import { useState } from "react";
import { api, type BlockKey, type Workspace, type WorkspaceBlock } from "@/lib/api";
import { errorMessage, useLoad } from "@/lib/hooks";
import { t, useLocale } from "@/lib/i18n";
import { useSession } from "@/components/AppShell";
import { blockRegistry, type BlockEntry, type BlockSpan } from "@/components/blocks/registry";
import { WorkspaceEditor } from "@/components/blocks/WorkspaceEditor";
import { IconEdit } from "@/components/icons";
import { ShipStatus } from "@/components/ship-status/ShipStatus";
import { useToast } from "@/components/toast";
import { Button, Empty, ErrorBox, ListSkeleton, PageHeader, Panel, cx } from "@/components/ui";

/*
 * 首页 = 工作台（ADR 0015）：GET /workspace 给出已解析的区块顺序（个人微调 → 角色并集 → 默认），
 * 这里只按顺序从 blockRegistry 取组件排进两列网格（≥1200 两列，通栏区块占两列），不再有写死的按身份自适应规则。
 * 右上角「调整首页」打开抽屉：勾选 / 排序 → PUT /workspace/me（乐观更新 + Toast）；「恢复角色默认」→ DELETE /workspace/me。
 */
export default function HomePage() {
  const { session } = useSession();
  const { locale } = useLocale();
  const toast = useToast();
  const ws = useLoad(() => api.workspace.get(), []);
  const [editing, setEditing] = useState(false);
  const [wanted, setWanted] = useState(false);
  // 目录只在第一次打开抽屉时取
  const catalog = useLoad(() => (wanted ? api.workspace.catalog() : Promise.resolve(null)), [wanted]);
  // 乐观更新：保存后先按本地这一份画，服务端回来再换；ws 重新取数后（切范围等）自动失效
  const [override, setOverride] = useState<{ base: Workspace | null; value: Workspace } | null>(null);
  const [busy, setBusy] = useState(false);
  const workspace = override && override.base === ws.data ? override.value : ws.data;

  const roleTitle = (name: string) => session?.roles.find((r) => r.name === name)?.title ?? name;
  const joiner = locale === "en-US" ? ", " : "、";
  const sourceText = workspace
    ? workspace.source === "personal"
      ? t("workspace.sourcePersonal")
      : workspace.source === "roles"
        ? t("workspace.sourceRoles", { roles: workspace.roles_used.map(roleTitle).join(joiner) })
        : t("workspace.sourceDefault")
    : "";

  const openEditor = () => {
    setWanted(true);
    setEditing(true);
  };

  const save = async (blocks: BlockKey[]) => {
    if (!workspace) return;
    const prev = workspace;
    const titleOf = (k: BlockKey) => catalog.data?.blocks.find((b) => b.key === k)?.title ?? prev.blocks.find((b) => b.key === k)?.title ?? t(`block.${k}`);
    setOverride({ base: ws.data, value: { ...prev, source: "personal", blocks: blocks.map((key) => ({ key, title: titleOf(key) })) } });
    setEditing(false);
    setBusy(true);
    try {
      const saved = await api.workspace.saveMine(blocks);
      setOverride({ base: ws.data, value: saved });
      toast.ok(t("workspace.saved"));
    } catch (e) {
      setOverride({ base: ws.data, value: prev });
      toast.fail(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const reset = async () => {
    setBusy(true);
    try {
      await api.workspace.resetMine();
      setEditing(false);
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
        description={t("home.description")}
        actions={
          workspace?.can_customize !== false ? (
            <Button size="sm" variant="ghost" icon={<IconEdit />} onClick={openEditor} disabled={!workspace}>
              {t("workspace.customize")}
            </Button>
          ) : undefined
        }
      />

      {/* 组织概况读数条（DESIGN.md 舰内 v3 §2）：不是区块，始终在最上面 */}
      <ShipStatus className="mb-4" />

      {ws.error && !workspace ? (
        <ErrorBox message={ws.error} onRetry={ws.reload} />
      ) : !workspace ? (
        <div className="grid items-start gap-4 xl:grid-cols-2">
          <Panel className="xl:col-span-2">
            <ListSkeleton rows={3} />
          </Panel>
          <Panel>
            <ListSkeleton rows={5} />
          </Panel>
          <Panel>
            <ListSkeleton rows={5} />
          </Panel>
        </div>
      ) : workspace.blocks.length === 0 ? (
        <Panel>
          <Empty
            text={t("workspace.empty")}
            action={
              <Button size="sm" icon={<IconEdit />} onClick={openEditor}>
                {t("workspace.customize")}
              </Button>
            }
          />
        </Panel>
      ) : (
        <div className="grid items-start gap-4 xl:grid-cols-2">
          {layoutOf(workspace).map(({ block, entry, span }, i) => {
            const Block = entry.component;
            return (
              <div key={block.key} className={cx("min-w-0", span === "full" && "xl:col-span-2")}>
                <Block index={i + 1} title={block.title || undefined} />
              </div>
            );
          })}
        </div>
      )}

      <WorkspaceEditor
        open={editing}
        onClose={() => setEditing(false)}
        title={t("workspace.customize")}
        description={t("workspace.editorHint")}
        caption={sourceText}
        catalog={catalog.data}
        value={workspace?.blocks.map((b) => b.key) ?? []}
        onSave={save}
        onReset={workspace?.source === "personal" ? reset : undefined}
        resetLabel={t("workspace.resetRole")}
        busy={busy}
      />
    </div>
  );
}

/**
 * 把区块排进两列：半栏区块两两并排；一个半栏区块后面紧跟的是通栏区块（或已到末尾）时，它自己也铺成通栏，
 * 这样顺序严格按配置来，页面上也不会留下半边空白。
 */
function layoutOf(ws: Workspace): Array<{ block: WorkspaceBlock; entry: BlockEntry; span: BlockSpan }> {
  const known = ws.blocks.flatMap((block) => (blockRegistry[block.key] ? [{ block, entry: blockRegistry[block.key] }] : []));
  const out: Array<{ block: WorkspaceBlock; entry: BlockEntry; span: BlockSpan }> = [];
  for (let i = 0; i < known.length; i++) {
    const cur = known[i];
    const next = known[i + 1];
    if (cur.entry.span === "half" && next?.entry.span === "half") {
      out.push({ ...cur, span: "half" }, { ...next, span: "half" });
      i++;
    } else {
      out.push({ ...cur, span: "full" });
    }
  }
  return out;
}
