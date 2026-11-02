import { useEffect, useRef, useState } from "react";
import {
  CollaborationRequestError,
  type CollaborationClient,
  type DecisionCurrent,
  type CreateTicketOutcome,
} from "./api";

export function useDecisionTicketCreation({
  workspaceID,
  current,
  canCreate,
  canRetry,
  unavailable,
  client,
  createOperationID,
  onRefresh,
  onUnavailable,
  onSessionExpired,
  onCreated,
}: {
  workspaceID: string;
  current?: DecisionCurrent;
  canCreate: boolean;
  canRetry: boolean;
  unavailable: boolean;
  client: CollaborationClient;
  createOperationID: () => string;
  onRefresh: () => void;
  onUnavailable: () => void;
  onSessionExpired: () => void;
  onCreated: (result: CreateTicketOutcome) => void;
}) {
  const [title, setTitle] = useState(""),
    [pending, setPending] = useState<{ id: string; title: string } | null>(
      null,
    ),
    [busy, setBusy] = useState(false),
    [message, setMessage] = useState<string | null>(null);
  const active = useRef<AbortController | null>(null);
  const [observed, setObserved] = useState({ current, canRetry, unavailable });
  if (
    observed.current !== current ||
    observed.canRetry !== canRetry ||
    observed.unavailable !== unavailable
  ) {
    setObserved({ current, canRetry, unavailable });
    setBusy(false);
    if (unavailable || (current && !canRetry)) {
      setPending(null);
      setTitle("");
      setMessage(null);
    }
  }
  useEffect(
    () => () => {
      active.current?.abort();
      active.current = null;
    },
    [current, canRetry, unavailable],
  );
  const submit = async (retry?: { id: string; title: string }) => {
    if (
      !current ||
      active.current ||
      (!retry && (!canCreate || pending || !title.trim()))
    )
      return;
    let operation: { id: string; title: string };
    try {
      operation = retry ?? { id: createOperationID(), title };
    } catch {
      setMessage("无法生成操作标识，请重试。");
      return;
    }
    const controller = new AbortController();
    active.current = controller;
    setPending(operation);
    setBusy(true);
    setMessage(null);
    try {
      const result = await client.createTicket(
        workspaceID,
        current.ref.id,
        { clientOperationID: operation.id, title: operation.title },
        controller.signal,
      );
      if (controller.signal.aborted) return;
      setPending(null);
      setTitle("");
      setMessage(
        result.created
          ? "Ticket 已创建，并保留创建时的已确认来源。"
          : "原工单创建已确认，保留同一 Ticket。",
      );
      onCreated(result);
      onRefresh();
    } catch (error) {
      if (controller.signal.aborted) return;
      const status =
        error instanceof CollaborationRequestError ? error.status : undefined;
      if (status === 401 || status === 404) {
        setPending(null);
        setMessage(null);
        onUnavailable();
        if (status === 401) onSessionExpired();
      } else if (status !== undefined && status >= 400 && status < 500) {
        setPending(null);
        setMessage("当前请求未被接受，请重新核对结论与权限。");
        onRefresh();
      } else setMessage("工单创建结果尚未确认。只能重试原标题与原操作。");
    } finally {
      if (active.current === controller) {
        active.current = null;
        setBusy(false);
      }
    }
  };
  return (
    <section aria-label="从结论创建工单">
      {message && <p role="status">{message}</p>}
      {pending ? (
        <>
          <p>待确认原工单标题：{pending.title}</p>
          <button
            disabled={busy || !current}
            onClick={() => void submit(pending)}
          >
            {busy ? "正在创建…" : "重试原工单创建"}
          </button>
        </>
      ) : (
        canCreate && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            <label>
              <span>Ticket 标题</span>
              <input
                required
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="把已接受结论转成可执行工作"
              />
            </label>
            <button
              className="primary-button"
              disabled={!title.trim()}
              type="submit"
            >
              创建 Ticket
            </button>
          </form>
        )
      )}
    </section>
  );
}
