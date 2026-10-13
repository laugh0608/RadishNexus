import { useEffect, useRef, useState } from "react";
import {
  CollaborationRequestError,
  type ChangeTicketStatusInput,
  type CollaborationClient,
  type TicketCurrent,
} from "./api";
import {
  ticketStatusAction,
  ticketStatusLabels,
  ticketStatusTargets,
  type TicketStatus,
} from "./ticket-status";

// Keep uncertain commands above the loading view so a refresh cannot change their base.
export function useTicketStatusActions({
  workspaceID,
  ticketID,
  current,
  canChange,
  unavailable,
  client,
  createOperationID,
  onRefresh,
  onUnavailable,
  onSessionExpired,
}: {
  workspaceID: string;
  ticketID: string;
  current?: TicketCurrent;
  canChange: boolean;
  unavailable: boolean;
  client: CollaborationClient;
  createOperationID: () => string;
  onRefresh: () => void;
  onUnavailable: () => void;
  onSessionExpired: () => void;
}) {
  const [confirmation, setConfirmation] = useState<TicketStatus | null>(null);
  const [pending, setPending] = useState<ChangeTicketStatusInput | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const active = useRef<AbortController | null>(null);
  const [observed, setObserved] = useState({ current, canChange, unavailable });
  if (
    observed.current !== current ||
    observed.canChange !== canChange ||
    observed.unavailable !== unavailable
  ) {
    setObserved({ current, canChange, unavailable });
    setBusy(false);
    setConfirmation(null);
    if (unavailable || (current && !canChange)) {
      setPending(null);
      setMessage(null);
    }
  }
  useEffect(() => {
    return () => {
      active.current?.abort();
      active.current = null;
    };
  }, [current, canChange, unavailable]);

  const submit = async (to: TicketStatus, retry?: ChangeTicketStatusInput) => {
    if (!current || !canChange || active.current || (pending && !retry)) return;
    let operation: ChangeTicketStatusInput;
    try {
      operation = retry ?? {
        clientOperationID: createOperationID(),
        baseStatusRevision: current.statusRevision,
        toStatus: to,
        confirmed: true,
      };
    } catch {
      setMessage("无法生成操作标识，请重新尝试。");
      return;
    }
    const controller = new AbortController();
    active.current = controller;
    setPending(operation);
    setBusy(true);
    setConfirmation(null);
    setMessage(null);
    try {
      const result = await client.changeTicketStatus(
        workspaceID,
        ticketID,
        operation,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      setPending(null);
      setMessage(
        `已确认原操作：${ticketStatusLabels[result.appliedStatus]}（版本 ${result.appliedStatusRevision}）。下方以重新读取的当前状态为准。`,
      );
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
      } else if (status === 403 || status === 409) {
        setPending(null);
        setMessage(
          status === 409
            ? "状态或权限已变化，本次未应用。请查看最新状态后重新选择。"
            : "当前已无状态修改权限，正在重新读取。",
        );
        onRefresh();
      } else if (status !== undefined && status >= 400 && status < 500) {
        setPending(null);
        setMessage("请求未被接受，请重新读取后再试。");
        onRefresh();
      } else {
        setMessage(
          "操作结果尚未确认。请使用原操作重试；读取到相同状态也不代表本次操作已成功。",
        );
      }
    } finally {
      if (active.current === controller) {
        active.current = null;
        setBusy(false);
      }
    }
  };

  return (
    <section aria-labelledby="ticket-status-title">
      <h3 id="ticket-status-title">工单状态</h3>
      {message && <p role="status">{message}</p>}
      {current && (
        <p>
          当前：{ticketStatusLabels[current.status]} · 状态版本{" "}
          {current.statusRevision}
        </p>
      )}
      <p>标记完成只记录执行事项的完成，不代表测试、CI 或部署通过。</p>
      {!canChange ? (
        <p>当前工单只读；归档项目不允许修改状态。</p>
      ) : pending ? (
        <div>
          <p>
            待确认：{ticketStatusLabels[pending.toStatus]}（基于版本{" "}
            {pending.baseStatusRevision}）
          </p>
          <button
            type="button"
            disabled={busy}
            onClick={() => void submit(pending.toStatus, pending)}
          >
            {busy ? "正在提交…" : "重试原状态操作"}
          </button>
        </div>
      ) : confirmation && current ? (
        <div>
          <p>
            确认将工单从“{ticketStatusLabels[current.status]}”改为“
            {ticketStatusLabels[confirmation]}”？
          </p>
          <button
            className="primary-button"
            type="button"
            onClick={() => void submit(confirmation)}
          >
            确认{ticketStatusAction(current.status, confirmation)}
          </button>
          <button type="button" onClick={() => setConfirmation(null)}>
            返回选择
          </button>
        </div>
      ) : current ? (
        <div className="ticket-status-buttons">
          {ticketStatusTargets[current.status].map((to) => (
            <button
              type="button"
              key={to}
              onClick={() => {
                setMessage(null);
                if (
                  to === "done" ||
                  to === "canceled" ||
                  current.status === "done" ||
                  current.status === "canceled"
                )
                  setConfirmation(to);
                else void submit(to);
              }}
            >
              {ticketStatusAction(current.status, to)}
            </button>
          ))}
        </div>
      ) : null}
      <button type="button" onClick={onRefresh}>
        刷新工单状态
      </button>
    </section>
  );
}
