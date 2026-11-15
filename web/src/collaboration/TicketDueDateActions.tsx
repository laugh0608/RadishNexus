import { useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { newCollaborationOperationID } from "./api";
import {
  ticketDueDateClient,
  type DueDate,
  type DueDateInput,
  type TicketDueDateClient,
} from "./ticket-due-date-api";
import { isTicketDueDate, ticketDueDateLabel } from "./ticket-due-date";

type Operation = { input: DueDateInput; label: string };
// Command state stays mounted while the parent reauthorizes or refreshes its view.
export function useTicketDueDateActions({
  workspaceID,
  ticketID,
  enabled,
  ready,
  versionKey,
  unavailable,
  onRefresh,
  onUnavailable,
  onSessionExpired,
  client = ticketDueDateClient,
  createOperationID = newCollaborationOperationID,
}: {
  workspaceID: string;
  ticketID: string;
  enabled: boolean;
  ready: boolean;
  versionKey: string;
  unavailable: boolean;
  onRefresh: () => void;
  onUnavailable: () => void;
  onSessionExpired: () => void;
  client?: TicketDueDateClient;
  createOperationID?: () => string;
}) {
  const [snapshot, setSnapshot] = useState<DueDate | null>(null);
  const [selected, setSelected] = useState("");
  const [confirmation, setConfirmation] = useState<Operation | null>(null);
  const [pending, setPending] = useState<Operation | null>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(enabled && ready && !unavailable);
  const [message, setMessage] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const activeWrite = useRef<AbortController | null>(null);
  const scope = {
    workspaceID,
    ticketID,
    enabled,
    ready,
    versionKey,
    unavailable,
    reload,
    client,
  };
  const [observed, setObserved] = useState(scope);
  if (
    Object.keys(scope).some(
      (key) =>
        scope[key as keyof typeof scope] !==
        observed[key as keyof typeof scope],
    )
  ) {
    setObserved(scope);
    setBusy(false);
    setConfirmation(null);
    setSelected("");
    setSnapshot(null);
    setLoading(enabled && ready && !unavailable);
    if (
      unavailable ||
      observed.workspaceID !== workspaceID ||
      observed.ticketID !== ticketID
    ) {
      setPending(null);
      setMessage(null);
    }
  }
  useEffect(() => {
    activeWrite.current?.abort();
    activeWrite.current = null;
    if (!enabled || !ready || unavailable) return;
    const controller = new AbortController();
    void (async () => {
      try {
        const value = await client.read(
          workspaceID,
          ticketID,
          controller.signal,
        );
        if (controller.signal.aborted) return;
        setSnapshot(value);
        setSelected(value.dueDate ?? "");
      } catch (error) {
        if (controller.signal.aborted) return;
        const status =
          error instanceof AuthRequestError ? error.status : undefined;
        if (status === 401 || status === 404) {
          setPending(null);
          onUnavailable();
          if (status === 401) onSessionExpired();
        } else
          setMessage(
            status === 403
              ? "当前没有修改权限，请刷新工单。"
              : status === 409
                ? "工单已变化，请刷新截止日期后重新选择。"
                : "截止日期读取失败，请重试。",
          );
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      }
    })();
    return () => {
      controller.abort();
      activeWrite.current?.abort();
      activeWrite.current = null;
    };
  }, [
    workspaceID,
    ticketID,
    enabled,
    ready,
    versionKey,
    unavailable,
    reload,
    client,
    onUnavailable,
    onSessionExpired,
  ]);
  const prepare = (date: string | null) => {
    if (
      !snapshot?.canSetDueDate ||
      pending ||
      busy ||
      loading ||
      !isTicketDueDate(date) ||
      date === snapshot.dueDate
    )
      return;
    try {
      setConfirmation({
        label: ticketDueDateLabel(date),
        input: {
          client_operation_id: createOperationID(),
          base_due_date_revision: snapshot.revision,
          base_status_revision: snapshot.statusRevision,
          due_date: date,
          confirmed: true,
        },
      });
      setMessage(null);
    } catch {
      setMessage("无法生成操作标识，请重试。");
    }
  };
  const submit = async (operation: Operation) => {
    if (!snapshot || activeWrite.current || loading) return;
    const controller = new AbortController();
    activeWrite.current = controller;
    setBusy(true);
    setPending(operation);
    setConfirmation(null);
    setMessage(null);
    try {
      await client.set(
        workspaceID,
        ticketID,
        operation.input,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      setPending(null);
      setMessage("截止日期操作已确认，下方以重新读取的当前结果为准。");
      onRefresh();
    } catch (error) {
      if (controller.signal.aborted) return;
      const status =
        error instanceof AuthRequestError ? error.status : undefined;
      if (status === 401 || status === 404) {
        setPending(null);
        setMessage(null);
        onUnavailable();
        if (status === 401) onSessionExpired();
      } else if (status !== undefined && status >= 400 && status < 500) {
        setPending(null);
        setMessage(
          status === 409
            ? "截止日期、状态或权限已变化。请重读后重新选择。"
            : "当前请求未被接受，请重读后再试。",
        );
        onRefresh();
      } else setMessage("操作结果尚未确认。请重试原操作以核对回执。");
    } finally {
      if (activeWrite.current === controller) {
        activeWrite.current = null;
        setBusy(false);
      }
    }
  };
  return (
    <section
      className="ticket-due-date-actions"
      aria-labelledby="ticket-due-date-title"
    >
      <h3 id="ticket-due-date-title">工单截止日期</h3>
      {message && <p role="status">{message}</p>}
      {loading && <p role="status">正在读取截止日期…</p>}
      {snapshot && <p>当前截止日期：{ticketDueDateLabel(snapshot.dueDate)}</p>}
      {pending ? (
        <div>
          <p>待确认截止日期：{pending.label}</p>
          <button
            type="button"
            disabled={busy || loading || !snapshot}
            onClick={() => void submit(pending)}
          >
            {busy ? "正在提交截止日期…" : "重试原截止日期操作"}
          </button>
        </div>
      ) : confirmation ? (
        <div>
          <p>
            确认将截止日期从“
            {snapshot ? ticketDueDateLabel(snapshot.dueDate) : ""}”改为“
            {confirmation.label}”？
          </p>
          <button
            type="button"
            disabled={busy || loading}
            onClick={() => void submit(confirmation)}
          >
            确认截止日期
          </button>
          <button type="button" onClick={() => setConfirmation(null)}>
            返回截止日期选择
          </button>
        </div>
      ) : snapshot?.canSetDueDate ? (
        <div>
          <label htmlFor="ticket-due-date">选择截止日期</label>
          <input
            id="ticket-due-date"
            type="date"
            min="0001-01-01"
            max="9999-12-31"
            value={selected}
            onChange={(event) => setSelected(event.target.value)}
          />
          <button
            type="button"
            disabled={
              !selected ||
              !isTicketDueDate(selected) ||
              selected === snapshot.dueDate
            }
            onClick={() => prepare(selected)}
          >
            设置截止日期
          </button>
          <button
            type="button"
            disabled={snapshot.dueDate === null}
            onClick={() => prepare(null)}
          >
            清除截止日期
          </button>
        </div>
      ) : snapshot ? (
        <p>
          当前不能变更截止日期。已完成或已取消的工单须先重新打开；归档项目和只读成员不能修改。
        </p>
      ) : null}
      <button
        type="button"
        disabled={busy}
        onClick={() => {
          setMessage(null);
          setReload((k) => k + 1);
        }}
      >
        刷新截止日期
      </button>
    </section>
  );
}
