"use client";
import type { ExecutorRef, Participant } from "@/lib/api";
import { t } from "@/lib/i18n";
import { roleTitle } from "@/lib/terms";
import { Avatar, ExecutorName, Table, Tag, cx } from "@/components/ui";
import { InlineStatusMark } from "./InlineField";
import { InlineSelect, type InlineOption } from "./InlineSelect";
import type { InlineSaveState } from "./useInlineSave";

/** 执行者（成员 + Agent）→ 下拉选项：头像 + 名字，Agent 带后缀。 */
export function executorOptions(executors: ExecutorRef[]): InlineOption[] {
  return executors.map((e) => ({ value: e.id, label: e.name, hint: e.kind === "agent" ? "Agent" : undefined, icon: <Avatar name={e.name} kind={e.kind} size={16} />, keywords: e.kind }));
}

/*
 * 参与角色（DESIGN.md §15）：任务类型定义的位置 → 执行者。表格保持原样（位置 / 要求角色 / 执行者），
 * 执行者那一格本身是选人控件；每个位置一行一个请求，右侧对勾 / 行下原因。
 */
export function InlineParticipants({ participants, executors, pendingSlot, onChange, editable, stateOf, roles }: {
  participants: Record<string, Participant>;
  executors: ExecutorRef[];
  pendingSlot: string | null;
  onChange: (slot: string, executorId: string | null) => void;
  editable: boolean;
  stateOf: (slot: string) => InlineSaveState;
  roles?: Record<string, string>;
}) {
  const options = executorOptions(executors);
  return (
    <Table>
      <thead><tr><th>{t("task.slot")}</th><th>{t("task.requiredRole")}</th><th>{t("task.executor")}</th></tr></thead>
      <tbody>
        {Object.entries(participants).map(([slot, p]) => {
          const st = stateOf(slot);
          return (
            <tr key={slot} className="inl-row" data-saving={st.status === "saving" ? "" : undefined}>
              <td>{p.title}{pendingSlot === slot && <Tag tone="warning" className="ml-2">{t("task.pendingSlot")}</Tag>}</td>
              <td>{roleTitle(p.role, roles)}</td>
              <td>
                <span className="flex items-center gap-2">
                  <InlineSelect
                    value={p.executor?.id ?? null}
                    options={options}
                    onChange={(id) => onChange(slot, id)}
                    editable={editable}
                    nullable
                    nullLabel={t("task.vacant")}
                    display={<ExecutorName executor={p.executor} empty={t("task.vacant")} />}
                    ariaLabel={p.title}
                    className={cx("min-w-0", editable && "-my-1")}
                  />
                  <InlineStatusMark status={st.status} />
                </span>
                {st.status === "error" && st.error && <p className="mt-1 text-caption text-danger" role="alert">{st.error}</p>}
              </td>
            </tr>
          );
        })}
      </tbody>
    </Table>
  );
}
