import { useEffect, useRef, useState } from "react";
import {
  CollaborationRequestError,
  validRejectionReason,
  type CollaborationClient,
  type DecisionCurrent,
} from "./api";

type Pending = {
  id: string;
  action: "accept" | "reject";
  outcome: string;
  rationale: string;
  reason: string;
};

// The pending request stays above page loading; refresh never replaces its payload.
export function useDecisionActions({
  workspaceID,
  decisionID,
  current,
  canDecide,
  unavailable,
  client,
  createOperationID,
  onRefresh,
  onUnavailable,
  onSessionExpired,
}: {
  workspaceID: string;
  decisionID: string;
  current?: DecisionCurrent;
  canDecide: boolean;
  unavailable: boolean;
  client: CollaborationClient;
  createOperationID: () => string;
  onRefresh: () => void;
  onUnavailable: () => void;
  onSessionExpired: () => void;
}) {
  const [action, setAction] = useState<"accept" | "reject">("accept");
  const [outcome, setOutcome] = useState("");
  const [rationale, setRationale] = useState("");
  const [reason, setReason] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [pending, setPending] = useState<Pending | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const active = useRef<AbortController | null>(null);
  const [observed, setObserved] = useState({ current, canDecide, unavailable });
  if (
    observed.current !== current ||
    observed.canDecide !== canDecide ||
    observed.unavailable !== unavailable
  ) {
    setObserved({ current, canDecide, unavailable });
    setConfirmed(false);
    setBusy(false);
    if (unavailable || (current?.status === "proposed" && !canDecide)) {
      setPending(null);
      setOutcome("");
      setRationale("");
      setReason("");
      setMessage(null);
    }
  }
  useEffect(
    () => () => {
      active.current?.abort();
      active.current = null;
    },
    [current, canDecide, unavailable],
  );

  const submit = async (retry?: Pending) => {
    if (
      !current ||
      active.current ||
      (!retry && (!canDecide || !confirmed || pending))
    )
      return;
    let operation: Pending;
    try {
      operation = retry ?? {
        id: createOperationID(),
        action,
        outcome,
        rationale,
        reason,
      };
    } catch {
      setMessage("无法生成操作标识，请重试。");
      return;
    }
    const controller = new AbortController();
    active.current = controller;
    setPending(operation);
    setBusy(true);
    setConfirmed(false);
    setMessage(null);
    try {
      if (operation.action === "accept")
        await client.acceptDecision(
          workspaceID,
          decisionID,
          {
            clientOperationID: operation.id,
            outcome: operation.outcome,
            rationale: operation.rationale,
            confirmed: true,
          },
          controller.signal,
        );
      else
        await client.rejectDecision(
          workspaceID,
          decisionID,
          {
            clientOperationID: operation.id,
            rejectionReason: operation.reason,
            confirmed: true,
          },
          controller.signal,
        );
      if (controller.signal.aborted) return;
      setPending(null);
      setOutcome("");
      setRationale("");
      setReason("");
      setMessage(
        operation.action === "accept"
          ? "Decision 已由当前用户明确接受。"
          : "Decision 已明确拒绝，理由和来源已保留。",
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
      } else if (status !== undefined && status >= 400 && status < 500) {
        setPending(null);
        setMessage(
          status === 409
            ? "决定或权限已变化，请查看最新状态后重新确认。"
            : "请求未被接受，正在重新读取当前权限。",
        );
        onRefresh();
      } else
        setMessage(
          "操作结果尚未确认。请重试原操作；不能修改请求或改为另一种决定。",
        );
    } finally {
      if (active.current === controller) {
        active.current = null;
        setBusy(false);
      }
    }
  };

  const changeText = (set: (value: string) => void, value: string) => {
    set(value);
    setConfirmed(false);
  };
  return (
    <section className="decision-actions" aria-label="Decision 决定">
      {message && <p role="status">{message}</p>}
      {pending ? (
        <div>
          <p>
            待确认原操作：
            {pending.action === "accept" ? "接受 Decision" : "拒绝 Decision"}
          </p>
          <p>
            {pending.action === "accept"
              ? `${pending.outcome}：${pending.rationale}`
              : pending.reason}
          </p>
          <button
            type="button"
            disabled={busy || !current}
            onClick={() => void submit(pending)}
          >
            {busy ? "正在提交…" : "重试原决定操作"}
          </button>
        </div>
      ) : current?.status === "proposed" && canDecide ? (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <fieldset>
            <legend>决定动作</legend>
            <label>
              <input
                type="radio"
                name="decision-action"
                checked={action === "accept"}
                onChange={() => {
                  setAction("accept");
                  setConfirmed(false);
                }}
              />
              接受提案
            </label>
            <label>
              <input
                type="radio"
                name="decision-action"
                checked={action === "reject"}
                onChange={() => {
                  setAction("reject");
                  setConfirmed(false);
                }}
              />
              拒绝提案
            </label>
          </fieldset>
          {action === "accept" ? (
            <>
              <label>
                <span>明确结论</span>
                <textarea
                  required
                  rows={4}
                  value={outcome}
                  onChange={(e) => changeText(setOutcome, e.target.value)}
                />
              </label>
              <label>
                <span>理由</span>
                <textarea
                  required
                  rows={5}
                  value={rationale}
                  onChange={(e) => changeText(setRationale, e.target.value)}
                />
              </label>
            </>
          ) : (
            <label>
              <span>拒绝理由</span>
              <textarea
                required
                rows={5}
                value={reason}
                onChange={(e) => changeText(setReason, e.target.value)}
              />
              <span>
                理由将向此 Decision 的读者公开，请用来源引用指向私密材料。
              </span>
            </label>
          )}
          <p>确认问题：{current.question}</p>
          {action === "reject" && <p>本次拒绝理由：{reason || "尚未填写"}</p>}
          <label className="confirmation-control">
            <input
              type="checkbox"
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            <span>
              我正在以当前用户身份明确{action === "accept" ? "接受" : "拒绝"}
              这个 Decision。
            </span>
          </label>
          <button
            className="primary-button"
            type="submit"
            disabled={
              !confirmed ||
              (action === "accept"
                ? !outcome.trim() || !rationale.trim()
                : !validRejectionReason(reason))
            }
          >
            {action === "accept" ? "接受 Decision" : "拒绝 Decision"}
          </button>
        </form>
      ) : current?.status === "proposed" ? (
        <p>当前仅可读取提案；接受和拒绝需要决策权限及全部来源可读。</p>
      ) : null}
      {current?.status === "rejected" && (
        <div className="decision-result">
          <h3>已拒绝</h3>
          <p>{current.rejectionReason}</p>
          <p>
            拒绝人：{current.deciders.map((actor) => actor.id).join("、")} ·
            决定时间：{current.decidedAt}
          </p>
        </div>
      )}
    </section>
  );
}
