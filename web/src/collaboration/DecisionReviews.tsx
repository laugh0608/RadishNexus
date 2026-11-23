import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import {
  CollaborationRequestError,
  collaborationLocation,
  collaborationPagePath,
  validReviewReason,
  type CollaborationClient,
  type DecisionCurrent,
  type DecisionReviewPage,
  type ReviewConclusion,
  type ReviewDecisionInput,
} from "./api";
import { ProjectThreadPicker } from "./ProjectThreadPicker";

const conclusionLabels: Record<ReviewConclusion, string> = {
  still_applicable: "仍适用",
  reconsider: "需重新评估",
};

interface Options {
  workspaceID: string;
  current: DecisionCurrent | undefined;
  originalEvidenceIDs: readonly string[];
  canReview: boolean;
  canRetry: boolean;
  unavailable: boolean;
  client: CollaborationClient;
  createOperationID: () => string;
  onRefresh: () => void;
  onUnavailable: () => void;
  onSessionExpired: () => void;
}

export function useDecisionReviews({
  workspaceID,
  current,
  originalEvidenceIDs,
  canReview,
  canRetry,
  unavailable,
  client,
  createOperationID,
  onRefresh,
  onUnavailable,
  onSessionExpired,
}: Options) {
  const decisionID = current?.ref.id;
  const [page, setPage] = useState<DecisionReviewPage | null>(null);
  const [cursorPage, setCursorPage] = useState(false);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const [historyCurrent, setHistoryCurrent] = useState(current);
  const historyRead = useRef<AbortController | null>(null);

  if (historyCurrent !== current) {
    setHistoryCurrent(current);
    setPage(null);
    setHistoryError(null);
    setCursorPage(false);
  }

  const loadPage = useCallback(
    (id: string, cursor: string | null, controller: AbortController) => {
      void client
        .loadDecisionReviews(workspaceID, id, cursor, controller.signal)
        .then(
          (data) => {
            if (!controller.signal.aborted) setPage(data);
          },
          (error: unknown) => {
            if (controller.signal.aborted) return;
            if (
              error instanceof CollaborationRequestError &&
              error.status === 401
            ) {
              onUnavailable();
              onSessionExpired();
            } else if (
              error instanceof CollaborationRequestError &&
              error.status === 404
            ) {
              onUnavailable();
            } else {
              setHistoryError("无法读取复核历史，请重新读取。");
            }
          },
        );
    },
    [client, onSessionExpired, onUnavailable, workspaceID],
  );

  useEffect(() => {
    if (!decisionID) return;
    const controller = new AbortController();
    historyRead.current = controller;
    loadPage(decisionID, null, controller);
    return () => historyRead.current?.abort();
  }, [current, decisionID, loadPage]);

  const [open, setOpen] = useState(false);
  const [conclusion, setConclusion] = useState<ReviewConclusion | "">("");
  const [reason, setReason] = useState("");
  const [link, setLink] = useState("");
  const [browsing, setBrowsing] = useState(false);
  const [evidence, setEvidence] = useState<{
    id: string;
    title: string;
  } | null>(null);
  const [preview, setPreview] = useState<{ id: string } | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const [pending, setPending] = useState<ReviewDecisionInput | null>(null);
  const [writing, setWriting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const action = useRef<AbortController | null>(null);
  const [actionScope, setActionScope] = useState({
    current,
    canRetry,
    unavailable,
  });

  function clearDraft() {
    setOpen(false);
    setConclusion("");
    setReason("");
    setLink("");
    setBrowsing(false);
    setEvidence(null);
    setPreview(null);
    setConfirmed(false);
    setPending(null);
  }

  if (
    actionScope.current !== current ||
    actionScope.canRetry !== canRetry ||
    actionScope.unavailable !== unavailable
  ) {
    setActionScope({ current, canRetry, unavailable });
    setBrowsing(false);
    setConfirmed(false);
    setWriting(false);
    if (unavailable || (current !== undefined && !canRetry)) {
      clearDraft();
      setError(null);
      setNotice(null);
    } else {
      setEvidence(null);
    }
  }
  useEffect(
    () => () => action.current?.abort(),
    [canRetry, current, unavailable],
  );

  const extraEvidenceID = pending?.additionalEvidence?.id ?? preview?.id;
  const checkingEvidence = preview !== null && evidence === null;
  const canRetryEvidence =
    pending?.additionalEvidence == null ||
    evidence?.id === pending.additionalEvidence.id;
  // A fresh Decision snapshot must also recheck an already-selected private source.
  useEffect(() => {
    const id = extraEvidenceID;
    if (!current || !canRetry || !id) return;
    const controller = new AbortController();
    void client.loadView(workspaceID, "thread", id, controller.signal).then(
      (view) => {
        if (controller.signal.aborted) return;
        if (
          "visibility" in view.current &&
          view.current.ref.id === id &&
          view.current.project.id === current.project.id
        ) {
          setEvidence({ id, title: view.current.title });
        } else {
          clearDraft();
          setError("补充证据的访问范围已变化。");
        }
      },
      (cause: unknown) => {
        if (controller.signal.aborted) return;
        clearDraft();
        setError("无法重新读取补充证据。");
        if (cause instanceof CollaborationRequestError) {
          if (cause.status === 401) onSessionExpired();
          else if (cause.status === 403 || cause.status === 404) onRefresh();
        }
      },
    );
    return () => controller.abort();
  }, [
    canRetry,
    client,
    current,
    extraEvidenceID,
    preview,
    onRefresh,
    onSessionExpired,
    workspaceID,
  ]);

  function previewEvidence(value: string) {
    const id = threadIDFromLink(value, workspaceID);
    setConfirmed(false);
    if (!id) {
      setError("请粘贴当前工作区的 Thread 页面链接。");
      return;
    }
    if (originalEvidenceIDs.includes(id)) {
      setError("这条讨论已是原证据，请选择其他补充讨论。");
      return;
    }
    setEvidence(null);
    setPreview({ id });
    setError(null);
  }

  async function submit(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    if (!current || writing) return;
    let operation = pending;
    if (!operation) {
      if (
        !canReview ||
        browsing ||
        !confirmed ||
        conclusion === "" ||
        !validReviewReason(reason) ||
        (link.trim() !== "" && !evidence)
      )
        return;
      let operationID: string;
      try {
        operationID = createOperationID();
      } catch {
        setError("无法生成复核操作号，请重新确认。");
        return;
      }
      operation = {
        clientOperationID: operationID,
        baseDecisionRevision: current.decisionRevision,
        conclusion,
        reason,
        additionalEvidence: evidence
          ? { type: "thread", id: evidence.id }
          : null,
        confirmed: true,
      };
    } else if (!canRetry || !canRetryEvidence) {
      return;
    }
    const controller = new AbortController();
    action.current?.abort();
    action.current = controller;
    setPending(operation);
    setWriting(true);
    setError(null);
    setNotice(null);
    try {
      await client.reviewDecision(
        workspaceID,
        current.ref.id,
        operation,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      clearDraft();
      setNotice("已确认复核记录。");
      onRefresh();
    } catch (cause) {
      if (controller.signal.aborted) return;
      if (
        cause instanceof CollaborationRequestError &&
        cause.status !== undefined &&
        cause.status >= 400 &&
        cause.status < 500
      ) {
        clearDraft();
        setError(cause.message);
        if (cause.status === 401) onSessionExpired();
        else if (cause.status === 404) onUnavailable();
        else if (cause.status === 403 || cause.status === 409) onRefresh();
      } else {
        setError("复核结果暂未确认，可以重试原操作。");
      }
    } finally {
      if (!controller.signal.aborted) setWriting(false);
    }
  }

  const readPage = (cursor: string | null) => {
    if (!decisionID) return;
    historyRead.current?.abort();
    const controller = new AbortController();
    historyRead.current = controller;
    setPage(null);
    setHistoryError(null);
    setCursorPage(cursor !== null);
    loadPage(decisionID, cursor, controller);
  };

  return {
    history: (
      <section
        className="collaboration-panel"
        aria-labelledby="decision-reviews"
      >
        <h2 id="decision-reviews">复核历史</h2>
        {historyError ? (
          <>
            <p role="alert">{historyError}</p>
            <button type="button" onClick={() => readPage(null)}>
              重新读取复核历史
            </button>
          </>
        ) : page === null ? (
          <p role="status">正在读取复核历史…</p>
        ) : page.items.length === 0 ? (
          <p>暂无可读复核记录。</p>
        ) : (
          <ol className="collaboration-timeline-list">
            {page.items.map((record) => (
              <li key={record.eventID}>
                <strong>
                  {conclusionLabels[record.conclusion]} · 版本{" "}
                  {record.decisionRevision}
                </strong>
                <p>{record.reason}</p>
                <p>
                  <code>{record.actor.id}</code>
                  {" · "}
                  <time dateTime={record.occurredAt}>
                    {new Date(record.occurredAt).toLocaleString()}
                  </time>
                  {record.imported ? " · 导入历史" : ""}
                </p>
                <ul>
                  {record.originalEvidence.map((ref) => (
                    <li key={ref.id}>
                      <a
                        href={
                          collaborationPagePath(
                            workspaceID,
                            "thread",
                            ref.id,
                          ) ?? undefined
                        }
                      >
                        原讨论
                      </a>
                    </li>
                  ))}
                  {record.additionalEvidence && (
                    <li>
                      <a
                        href={
                          collaborationPagePath(
                            workspaceID,
                            "thread",
                            record.additionalEvidence.id,
                          ) ?? undefined
                        }
                      >
                        补充讨论
                      </a>
                    </li>
                  )}
                </ul>
              </li>
            ))}
          </ol>
        )}
        {page?.nextAfter && (
          <button type="button" onClick={() => readPage(page.nextAfter)}>
            查看更早的复核
          </button>
        )}
        {cursorPage && page && (
          <button type="button" onClick={() => readPage(null)}>
            返回最新复核
          </button>
        )}
      </section>
    ),
    action: (
      <>
        {error && (
          <p role="alert" className="inline-error">
            {error}
          </p>
        )}
        {notice && (
          <p role="status" className="inline-notice">
            {notice}
          </p>
        )}
        {pending && canRetry ? (
          <button
            className="primary-button"
            type="button"
            disabled={writing || !canRetryEvidence}
            onClick={() => void submit()}
          >
            {writing ? "正在记录…" : "重试原复核"}
          </button>
        ) : canReview && current ? (
          open ? (
            <form
              className="decision-actions"
              onSubmit={(event) => void submit(event)}
            >
              <h3>复核当前结论 · 版本 {current.decisionRevision}</h3>
              <label>
                复核结论
                <select
                  required
                  value={conclusion}
                  onChange={(event) => {
                    const value = event.currentTarget.value;
                    setConclusion(
                      value === "still_applicable" || value === "reconsider"
                        ? value
                        : "",
                    );
                    setConfirmed(false);
                  }}
                >
                  <option value="">请选择</option>
                  <option value="still_applicable">仍适用</option>
                  <option value="reconsider">需重新评估</option>
                </select>
              </label>
              <label>
                复核说明
                <textarea
                  required
                  rows={4}
                  value={reason}
                  onChange={(event) => {
                    setReason(event.currentTarget.value);
                    setConfirmed(false);
                  }}
                />
              </label>
              <p>
                原讨论会随记录保留。可以从项目频道选择一条补充讨论，也可以粘贴讨论链接。
              </p>
              <button
                type="button"
                aria-expanded={browsing}
                onClick={() => {
                  setBrowsing(!browsing);
                  setConfirmed(false);
                  if (browsing && preview) {
                    setEvidence(null);
                    setPreview({ id: preview.id });
                  }
                  setError(null);
                }}
              >
                {browsing ? "返回链接输入" : "从频道选择讨论"}
              </button>
              {browsing ? (
                <ProjectThreadPicker
                  workspaceID={workspaceID}
                  projectID={current.project.id}
                  purpose="review"
                  disabledThreadIDs={originalEvidenceIDs}
                  onSelect={({ id }) => {
                    const path = collaborationPagePath(
                      workspaceID,
                      "thread",
                      id,
                    )!;
                    setLink(path);
                    setBrowsing(false);
                    previewEvidence(path);
                  }}
                  onUnavailable={onUnavailable}
                  onSessionExpired={onSessionExpired}
                />
              ) : (
                <>
                  <label>
                    补充 Thread 链接（可选）
                    <input
                      value={link}
                      onChange={(event) => {
                        setLink(event.currentTarget.value);
                        setError(null);
                        setEvidence(null);
                        setPreview(null);
                        setConfirmed(false);
                      }}
                    />
                  </label>
                  {link.trim() !== "" && (
                    <button
                      type="button"
                      disabled={checkingEvidence}
                      onClick={() => previewEvidence(link)}
                    >
                      {checkingEvidence ? "正在读取讨论…" : "核对补充讨论"}
                    </button>
                  )}
                  {evidence && <p>补充讨论 · {evidence.title}</p>}
                </>
              )}
              <label>
                <input
                  type="checkbox"
                  disabled={browsing || (link.trim() !== "" && !evidence)}
                  checked={confirmed}
                  onChange={(event) =>
                    setConfirmed(event.currentTarget.checked)
                  }
                />
                确认按所示版本保存本次人工复核
              </label>
              <button
                className="primary-button"
                type="submit"
                disabled={
                  !confirmed ||
                  browsing ||
                  conclusion === "" ||
                  !validReviewReason(reason) ||
                  checkingEvidence ||
                  (link.trim() !== "" && evidence === null)
                }
              >
                保存复核
              </button>
              <button
                type="button"
                onClick={() => {
                  action.current?.abort();
                  setError(null);
                  clearDraft();
                }}
              >
                取消复核
              </button>
            </form>
          ) : (
            <button
              type="button"
              onClick={() => {
                setOpen(true);
                setNotice(null);
                setError(null);
              }}
            >
              记录人工复核
            </button>
          )
        ) : null}
      </>
    ),
  };
}

function threadIDFromLink(value: string, workspaceID: string): string | null {
  try {
    const url = new URL(value.trim(), window.location.origin);
    if (
      url.origin !== window.location.origin ||
      url.username ||
      url.password ||
      url.search ||
      url.hash
    ) {
      return null;
    }
    const location = collaborationLocation(url.pathname);
    return location?.workspaceID === workspaceID &&
      location.entityType === "thread"
      ? location.entityID
      : null;
  } catch {
    return null;
  }
}
