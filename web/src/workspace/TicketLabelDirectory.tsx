import { useCallback, useState } from "react";
import { OperationStatus } from "../collaboration/ExecutionPanels";
import { useExecutionWrite } from "../collaboration/execution-write";
import {
  ticketClassificationClient,
  type LabelEntry,
} from "../collaboration/ticket-classification-api";
import { usePage } from "./configuration-hooks";
import { AuthRequestError } from "../auth/api";

type Change =
  | { action: "create"; name: string }
  | { action: "rename" | "archive"; label: LabelEntry; name: string };

export function TicketLabelDirectory({
  workspace,
  project,
  onSessionExpired,
  onUnavailable,
}: {
  workspace: string;
  project: string;
  onSessionExpired: () => void;
  onUnavailable: () => void;
}) {
  const write = useExecutionWrite({ onSessionExpired, onUnavailable });
  const clear = write.clear;
  const revision = write.revision;
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      void revision;
      try {
        const result = await ticketClassificationClient.labels(
          workspace,
          project,
          "all",
          after,
          signal,
        );
        if (!result.canManage && !signal.aborted) clear();
        return result;
      } catch (error) {
        if (
          !signal.aborted &&
          error instanceof AuthRequestError &&
          [401, 403, 404].includes(error.status ?? 0)
        ) {
          clear();
          onUnavailable();
        }
        throw error;
      }
    },
    [workspace, project, revision, clear, onUnavailable],
  );
  const directory = usePage<
    LabelEntry,
    Awaited<ReturnType<typeof ticketClassificationClient.labels>>
  >(load, onSessionExpired);
  const page = directory.page;
  function apply(change: Change) {
    write.start(
      change.action === "archive" ? "归档项目标签" : "保存项目标签",
      async (operation, signal) => {
        await ticketClassificationClient.writeLabel(
          workspace,
          project,
          change,
          operation,
          signal,
        );
        if (!signal.aborted) directory.refresh();
      },
    );
  }
  return (
    <section aria-label="项目标签目录">
      <h3>项目标签目录</h3>
      {page ? (
        <>
          {page.canManage && !write.pending && <LabelForm onConfirm={apply} />}
          <ul>
            {page.items.map((label) => (
              <li key={label.id}>
                {label.name}
                {label.status === "archived" && "（已归档）"}
                {page.canManage &&
                  label.status === "active" &&
                  !write.pending && (
                    <LabelForm
                      key={label.revision}
                      label={label}
                      onConfirm={apply}
                    />
                  )}
              </li>
            ))}
          </ul>
          {directory.hasPrevious && (
            <button type="button" onClick={directory.previous}>
              上一页目录
            </button>
          )}
          {page.nextCursor && (
            <button type="button" onClick={directory.next}>
              下一页目录
            </button>
          )}
        </>
      ) : directory.error ? (
        <>
          <p role="status">{directory.error}</p>
          <button type="button" onClick={directory.refresh}>
            重读目录
          </button>
        </>
      ) : (
        <p role="status">正在读取标签目录…</p>
      )}
      <OperationStatus write={write} />
    </section>
  );
}

function LabelForm({
  label,
  onConfirm,
}: {
  label?: LabelEntry;
  onConfirm: (change: Change) => void;
}) {
  const [name, setName] = useState(label?.name ?? "");
  const [editing, setEditing] = useState(!label);
  const [archiving, setArchiving] = useState(false);
  if (!editing && !archiving)
    return (
      <>
        <button type="button" onClick={() => setEditing(true)}>
          改名
        </button>
        <button type="button" onClick={() => setArchiving(true)}>
          归档
        </button>
      </>
    );
  if (archiving && label)
    return (
      <>
        <p>归档后工单保留已有标签，成员仍可按该标签筛选。</p>
        <button
          type="button"
          onClick={() =>
            onConfirm({ action: "archive", label, name: label.name })
          }
        >
          确认归档 {label.name}
        </button>
        <button type="button" onClick={() => setArchiving(false)}>
          取消
        </button>
      </>
    );
  const trimmed = name.trim();
  const valid =
    trimmed.length > 0 &&
    [...trimmed].length <= 40 &&
    trimmed !== label?.name &&
    !/[\p{Cc}\u2028\u2029]/u.test(trimmed);
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        if (valid)
          onConfirm(
            label
              ? { action: "rename", label, name: trimmed }
              : { action: "create", name: trimmed },
          );
      }}
    >
      <label>
        {label ? `将 ${label.name} 改名为` : "新标签名称"}
        <input
          value={name}
          onChange={(event) => setName(event.target.value)}
          required
        />
      </label>
      <button disabled={!valid} type="submit">
        {label ? "确认改名" : "确认创建标签"}
      </button>
      {label && (
        <button type="button" onClick={() => setEditing(false)}>
          取消
        </button>
      )}
    </form>
  );
}
