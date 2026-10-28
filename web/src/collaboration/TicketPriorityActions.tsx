import { useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { newCollaborationOperationID } from "./api";
import {
  ticketPriorityClient,
  type Priority,
  type PriorityInput,
  type TicketPriorityClient,
} from "./ticket-priority-api";
import {
  ticketPriorityLabels,
  parseTicketPriority,
  type TicketPriority,
} from "./ticket-priority";

type Operation = { input: PriorityInput; label: string };
// Command state stays mounted while the parent reauthorizes or refreshes its view.
export function useTicketPriorityActions({
  workspaceID,
  ticketID,
  enabled,
  ready,
  versionKey,
  unavailable,
  onRefresh,
  onUnavailable,
  onSessionExpired,
  client = ticketPriorityClient,
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
  client?: TicketPriorityClient;
  createOperationID?: () => string;
}) {
  const [priority, setPriority] = useState<Priority | null>(null);
  const [selected, setSelected] = useState<TicketPriority>("unset");
  const [confirmation, setConfirmation] = useState<Operation | null>(null);
  const [pending, setPending] = useState<Operation | null>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(enabled && ready && !unavailable);
  const [message, setMessage] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const activeRead = useRef<AbortController | null>(null),
    activeWrite = useRef<AbortController | null>(null);
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
    setSelected("unset");
    setPriority(null);
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
    activeRead.current = controller;
    void (async () => {
      try {
        const value = await client.read(
          workspaceID,
          ticketID,
          controller.signal,
        );
        if (controller.signal.aborted) return;
        setPriority(value);
        setSelected(value.priority);
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
                ? "工单已变化，请刷新优先级后重新选择。"
                : "优先级读取失败，请重试。",
          );
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false);
          activeRead.current = null;
        }
      }
    })();
    return () => {
      controller.abort();
      activeRead.current?.abort();
      activeRead.current = null;
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
  const prepare = () => {
    if (
      !priority?.canSetPriority ||
      pending ||
      busy ||
      loading ||
      selected === priority.priority
    )
      return;
    try {
      setConfirmation({
        label: ticketPriorityLabels[selected],
        input: {
          client_operation_id: createOperationID(),
          base_priority_revision: priority.revision,
          base_status_revision: priority.statusRevision,
          priority: selected,
          confirmed: true,
        },
      });
      setMessage(null);
    } catch {
      setMessage("无法生成操作标识，请重试。");
    }
  };
  const submit = async (operation: Operation) => {
    if (!priority || activeWrite.current || loading) return;
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
      setMessage("优先级操作已确认，下方以重新读取的当前结果为准。");
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
            ? "优先级、状态或权限已变化。请重读后重新选择，本次不会覆盖他人的修改。"
            : "当前请求未被接受，请重读后再试。",
        );
        onRefresh();
      } else
        setMessage(
          "操作结果尚未确认。请重试原操作；当前优先级相同不代表这次操作已成功。",
        );
    } finally {
      if (activeWrite.current === controller) {
        activeWrite.current = null;
        setBusy(false);
      }
    }
  };
  return (
    <section
      className="ticket-priority-actions"
      aria-labelledby="ticket-priority-title"
    >
      <h3 id="ticket-priority-title">工单优先级</h3>
      {message && <p role="status">{message}</p>}
      {loading && <p role="status">正在读取优先级…</p>}
      {priority && <p>当前优先级：{ticketPriorityLabels[priority.priority]}</p>}
      {pending ? (
        <div>
          <p>待确认优先级：{pending.label}</p>
          <button
            type="button"
            disabled={busy || loading || !priority}
            onClick={() => void submit(pending)}
          >
            {busy ? "正在提交优先级…" : "重试原优先级操作"}
          </button>
        </div>
      ) : confirmation ? (
        <div>
          <p>
            确认将优先级从“
            {priority ? ticketPriorityLabels[priority.priority] : ""}”改为“
            {confirmation.label}”？
          </p>
          <button
            type="button"
            disabled={busy || loading}
            onClick={() => void submit(confirmation)}
          >
            确认优先级
          </button>
          <button type="button" onClick={() => setConfirmation(null)}>
            返回优先级选择
          </button>
        </div>
      ) : priority?.canSetPriority ? (
        <div>
          <label htmlFor="ticket-priority">选择优先级</label>
          <select
            id="ticket-priority"
            value={selected}
            disabled={loading}
            onChange={(e) => setSelected(parseTicketPriority(e.target.value))}
          >
            {Object.entries(ticketPriorityLabels).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
          <button
            type="button"
            disabled={loading || selected === priority.priority}
            onClick={prepare}
          >
            设置优先级
          </button>
        </div>
      ) : priority ? (
        <p>
          当前不能变更优先级。已完成或已取消的工单须先重新打开；归档项目和只读成员不能修改。
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
        刷新优先级
      </button>
    </section>
  );
}
