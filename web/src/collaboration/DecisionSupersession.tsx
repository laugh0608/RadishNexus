import { useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import {
  projectDecisionsClient,
  type ProjectDecision,
} from "../workspace/project-decisions-api";
import {
  CollaborationRequestError,
  collaborationPagePath,
  validRejectionReason,
  type CollaborationClient,
  type CollaborationView,
  type DecisionCurrent,
  type SupersedeDecisionInput,
} from "./api";

type Pending = { oldID: string; input: SupersedeDecisionInput };
const labels = {
  proposed: "待确认",
  accepted: "已确认",
  rejected: "已拒绝",
  superseded: "已被替代",
};

export function useDecisionSupersession({
  workspaceID,
  current,
  canSupersede,
  canRetry,
  unavailable,
  client,
  createOperationID,
  onRefresh,
  onUnavailable,
  onSessionExpired,
}: {
  workspaceID: string;
  current?: DecisionCurrent;
  canSupersede: boolean;
  canRetry: boolean;
  unavailable: boolean;
  client: CollaborationClient;
  createOperationID: () => string;
  onRefresh: () => void;
  onUnavailable: () => void;
  onSessionExpired: () => void;
}) {
  const [choosing, setChoosing] = useState(false),
    [oldID, setOldID] = useState<string | null>(null);
  const [old, setOld] = useState<CollaborationView<DecisionCurrent> | null>(
      null,
    ),
    [reason, setReason] = useState(""),
    [confirmed, setConfirmed] = useState(false);
  const [rows, setRows] = useState<readonly ProjectDecision[] | null>(null),
    [cursors, setCursors] = useState<(string | undefined)[]>([undefined]),
    [next, setNext] = useState<string | null>(null);
  const [pending, setPending] = useState<Pending | null>(null),
    [busy, setBusy] = useState(false),
    [message, setMessage] = useState<string | null>(null);
  const write = useRef<AbortController | null>(null);
  const status = (error: unknown) =>
    error instanceof CollaborationRequestError ||
    error instanceof AuthRequestError
      ? error.status
      : undefined;
  const [listRequest, setListRequest] = useState({
    current,
    choosing,
    cursors,
    pending,
  });
  if (
    listRequest.current !== current ||
    listRequest.choosing !== choosing ||
    listRequest.cursors !== cursors ||
    listRequest.pending !== pending
  ) {
    setListRequest({ current, choosing, cursors, pending });
    setRows(null);
    setNext(null);
  }
  const [oldRequest, setOldRequest] = useState({ current, oldID });
  if (oldRequest.current !== current || oldRequest.oldID !== oldID) {
    setOldRequest({ current, oldID });
    setOld(null);
    setConfirmed(false);
  }
  useEffect(() => {
    const controller = new AbortController();
    if (!current || !choosing || pending) return () => controller.abort();
    void projectDecisionsClient
      .decisions(
        workspaceID,
        current.project.id,
        cursors[cursors.length - 1],
        controller.signal,
      )
      .then(
        (page) => {
          if (!controller.signal.aborted) {
            setRows(page.items);
            setNext(page.nextCursor);
          }
        },
        (error: unknown) => {
          if (controller.signal.aborted) return;
          setMessage("无法读取项目决策，请重新读取双方当前状态。");
          if (status(error) === 401) {
            onUnavailable();
            onSessionExpired();
          }
        },
      );
    return () => controller.abort();
  }, [
    current,
    choosing,
    cursors,
    pending,
    workspaceID,
    onUnavailable,
    onSessionExpired,
  ]);
  useEffect(() => {
    const controller = new AbortController();
    if (!current || !oldID) return () => controller.abort();
    void client
      .loadView(workspaceID, "decision", oldID, controller.signal)
      .then(
        (view) => {
          if (controller.signal.aborted) return;
          if (
            !("question" in view.current) ||
            view.current.project.id !== current.project.id ||
            view.current.ref.id !== oldID
          ) {
            setMessage("双方当前状态不符合替代要求。");
            setPending(null);
            setReason("");
            return;
          }
          setOld(view as CollaborationView<DecisionCurrent>);
        },
        (error: unknown) => {
          if (controller.signal.aborted) return;
          setPending(null);
          setReason("");
          setMessage("旧结论当前不可读取，请重新选择。");
          if (status(error) === 401) {
            onUnavailable();
            onSessionExpired();
          }
        },
      );
    return () => controller.abort();
  }, [current, oldID, client, workspaceID, onUnavailable, onSessionExpired]);
  useEffect(
    () => () => {
      write.current?.abort();
      write.current = null;
    },
    [current, old, unavailable, canRetry],
  );
  const [observed, setObserved] = useState({
    current,
    old,
    unavailable,
    canRetry,
  });
  if (
    observed.current !== current ||
    observed.old !== old ||
    observed.unavailable !== unavailable ||
    observed.canRetry !== canRetry
  ) {
    setObserved({ current, old, unavailable, canRetry });
    setConfirmed(false);
    setBusy(false);
    if (
      unavailable ||
      (current && !canRetry) ||
      (old && old.capabilities?.retryDecision !== true)
    ) {
      setPending(null);
      setReason("");
      setChoosing(false);
      setMessage(null);
    }
  }
  const submit = async (retry?: Pending) => {
    if (
      !current ||
      !old ||
      write.current ||
      (!retry &&
        (!canSupersede ||
          old.current.status !== "accepted" ||
          old.capabilities?.retryDecision !== true ||
          !confirmed ||
          pending))
    )
      return;
    let operation: Pending;
    try {
      operation = retry ?? {
        oldID: old.current.ref.id,
        input: {
          clientOperationID: createOperationID(),
          replacementDecisionID: current.ref.id,
          baseDecisionRevision: old.current.decisionRevision,
          baseReplacementRevision: current.decisionRevision,
          supersessionReason: reason,
          confirmed: true,
        },
      };
    } catch {
      setMessage("无法生成操作标识，请重试。");
      return;
    }
    const controller = new AbortController();
    write.current = controller;
    setPending(operation);
    setBusy(true);
    setConfirmed(false);
    setMessage(null);
    try {
      await client.supersedeDecision(
        workspaceID,
        operation.oldID,
        operation.input,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      setPending(null);
      setReason("");
      setChoosing(false);
      setMessage("原替代操作已确认；双方结论和原执行来源已保留。");
      onRefresh();
    } catch (error) {
      if (controller.signal.aborted) return;
      const code = status(error);
      if (code === 401 || code === 404) {
        setPending(null);
        setReason("");
        setOld(null);
        setMessage(null);
        onUnavailable();
        if (code === 401) onSessionExpired();
      } else if (code !== undefined && code >= 400 && code < 500) {
        setPending(null);
        setMessage(
          code === 409
            ? "双方结论或权限已变化，请重新读取后确认。"
            : "当前请求未被接受，请核对当前权限。",
        );
        onRefresh();
      } else
        setMessage(
          "替代结果尚未确认。只能重试原操作，不能改变双方结论、版本或理由。",
        );
    } finally {
      if (write.current === controller) {
        write.current = null;
        setBusy(false);
      }
    }
  };
  if (!current || (!canSupersede && !pending && !message)) return null;
  return (
    <section className="decision-actions" aria-label="替代旧结论">
      <h3>替代旧结论</h3>
      {message && <p role="status">{message}</p>}
      {pending ? (
        <>
          <p>待确认原替代：{pending.input.supersessionReason}</p>
          <button disabled={busy || !old} onClick={() => void submit(pending)}>
            {busy ? "正在提交替代…" : "重试原替代操作"}
          </button>
        </>
      ) : (
        canSupersede && (
          <>
            <p>
              用本条已确认结论替代同一项目的另一条已确认结论。已有工单与文档保留原来源和状态。
            </p>
            <button
              onClick={() => {
                setChoosing(true);
                setCursors([undefined]);
                setMessage(null);
              }}
            >
              选择旧结论
            </button>
            {choosing && (
              <div aria-label="项目决策选择">
                {rows === null ? (
                  <p>正在读取项目决策…</p>
                ) : (
                  <ul>
                    {rows.map((row) => (
                      <li key={row.id}>
                        <span>
                          {row.title} · {labels[row.status]}
                        </span>
                        <button
                          disabled={
                            row.status !== "accepted" ||
                            row.id === current.ref.id
                          }
                          onClick={() => {
                            setOldID(row.id);
                            setConfirmed(false);
                            setReason("");
                            setMessage(null);
                          }}
                        >
                          选择 {row.title}
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
                {cursors.length > 1 && (
                  <button onClick={() => setCursors(cursors.slice(0, -1))}>
                    上一页结论
                  </button>
                )}
                {next && (
                  <button onClick={() => setCursors([...cursors, next])}>
                    下一页结论
                  </button>
                )}
              </div>
            )}
            {oldID && !old && <p>正在重新读取旧结论…</p>}
            {old && (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void submit();
                }}
              >
                <div className="decision-comparison">
                  <article>
                    <h4>新结论：{current.question}</h4>
                    <p>{current.outcome}</p>
                    <p>{current.rationale}</p>
                    <p>当前结论版本 {current.decisionRevision}</p>
                  </article>
                  <article>
                    <h4>旧结论：{old.current.question}</h4>
                    <p>{old.current.outcome}</p>
                    <p>{old.current.rationale}</p>
                    <p>当前结论版本 {old.current.decisionRevision}</p>
                    <ul>
                      {old.relations.map((relation, index) =>
                        relation.visibility === "restricted" ? (
                          <li key={`restricted-${index}`}>受限来源</li>
                        ) : relation.relationType === "derived-from" ? (
                          <li key={relation.target.ref.id}>
                            <a
                              href={collaborationPagePath(
                                workspaceID,
                                "thread",
                                relation.target.ref.id,
                              )!}
                              target="_blank"
                              rel="noopener noreferrer"
                            >
                              查看旧结论来源：{relation.target.title}
                            </a>
                          </li>
                        ) : null,
                      )}
                    </ul>
                  </article>
                </div>
                <p>
                  请核对双方详情中的来源。替代理由将向有权读取双方结论的人公开，请勿复制私密材料正文。
                </p>
                {old.current.status !== "accepted" ||
                old.capabilities?.retryDecision !== true ? (
                  <p>旧结论当前不具备替代条件，请重新选择或核对来源权限。</p>
                ) : (
                  <>
                    <label>
                      <span>替代理由</span>
                      <textarea
                        required
                        rows={4}
                        value={reason}
                        onChange={(e) => {
                          setReason(e.target.value);
                          setConfirmed(false);
                        }}
                      />
                    </label>
                    <p>本次替代理由：{reason || "尚未填写"}</p>
                    <label className="confirmation-control">
                      <input
                        type="checkbox"
                        checked={confirmed}
                        onChange={(e) => setConfirmed(e.target.checked)}
                      />
                      <span>
                        我已核对双方结论与来源，明确将旧结论标为已被替代。
                      </span>
                    </label>
                    <button
                      className="primary-button"
                      disabled={!confirmed || !validRejectionReason(reason)}
                      type="submit"
                    >
                      确认替代旧结论
                    </button>
                  </>
                )}
              </form>
            )}
          </>
        )
      )}
    </section>
  );
}
