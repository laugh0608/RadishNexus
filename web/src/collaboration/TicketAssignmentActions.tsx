import { useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { newCollaborationOperationID } from "./api";
import {
  assigneeLabel,
  ticketAssignmentClient,
  type Assignment,
  type AssignmentInput,
  type AssigneeCandidate,
  type TicketAssignmentClient,
} from "./ticket-assignee-api";

type Operation = { input: AssignmentInput; label: string };
// Command state stays mounted while the parent reauthorizes or refreshes its view.
export function useTicketAssignmentActions({
  workspaceID,
  ticketID,
  enabled,
  ready,
  versionKey,
  unavailable,
  onRefresh,
  onUnavailable,
  onSessionExpired,
  client = ticketAssignmentClient,
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
  client?: TicketAssignmentClient;
  createOperationID?: () => string;
}) {
  const [assignment, setAssignment] = useState<Assignment | null>(null);
  const [candidates, setCandidates] = useState<AssigneeCandidate[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [selfID, setSelfID] = useState("");
  const [selected, setSelected] = useState("");
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
    setSelected("");
    setAssignment(null);
    setCandidates([]);
    setNext(null);
    setSelfID("");
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
        const page = value.canAssign
          ? await client.candidates(
              workspaceID,
              ticketID,
              undefined,
              controller.signal,
            )
          : null;
        if (controller.signal.aborted) return;
        if (page && page.projectRevision !== value.projectRevision) {
          setMessage("项目成员已变化，请刷新负责人后重新选择。");
          return;
        }
        setAssignment(value);
        setCandidates(page?.items ?? []);
        setNext(page?.nextCursor ?? null);
        setSelfID(page?.selfID ?? "");
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
              ? "当前没有分配权限，请刷新工单。"
              : status === 409
                ? "工单或成员已变化，请刷新负责人后重新选择。"
                : "负责人读取失败，请重试。",
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
  const loadMore = async () => {
    if (!next || !assignment || activeRead.current || busy) return;
    const controller = new AbortController();
    activeRead.current = controller;
    setLoading(true);
    try {
      const page = await client.candidates(
        workspaceID,
        ticketID,
        next,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      const last = candidates.at(-1)?.id ?? "";
      if (
        page.projectRevision !== assignment.projectRevision ||
        page.selfID !== selfID ||
        page.items.some((item) => item.id <= last)
      ) {
        setMessage("候选成员已变化，请重新选择。");
        setReload((k) => k + 1);
        return;
      }
      setCandidates((items) => [...items, ...page.items]);
      setNext(page.nextCursor);
    } catch (error) {
      if (controller.signal.aborted) return;
      const status =
        error instanceof AuthRequestError ? error.status : undefined;
      if (status === 401 || status === 404) {
        setPending(null);
        onUnavailable();
        if (status === 401) onSessionExpired();
      } else if (status === 403 || status === 409) {
        setCandidates([]);
        setConfirmation(null);
        setSelected("");
        onRefresh();
      } else setMessage("候选成员加载失败，可重试加载更多。");
    } finally {
      if (activeRead.current === controller) {
        activeRead.current = null;
        setLoading(false);
      }
    }
  };
  const prepare = (candidate: AssigneeCandidate | null) => {
    if (
      !assignment?.canAssign ||
      pending ||
      busy ||
      loading ||
      (candidate?.id ?? null) === (assignment.assignee?.id ?? null)
    )
      return;
    try {
      setConfirmation({
        label: candidate
          ? `${candidate.displayName} · ${candidate.id}`
          : "未分配",
        input: {
          client_operation_id: createOperationID(),
          base_assignment_revision: assignment.revision,
          base_status_revision: assignment.statusRevision,
          assignee_user_id: candidate?.id ?? null,
          target_lifecycle_revision: candidate?.lifecycleRevision ?? "0",
          project_management_revision: assignment.projectRevision,
          confirmed: true,
        },
      });
      setMessage(null);
    } catch {
      setMessage("无法生成操作标识，请重试。");
    }
  };
  const submit = async (operation: Operation) => {
    if (!assignment || activeWrite.current || loading) return;
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
      setMessage("负责人操作已确认，下方以重新读取的当前结果为准。");
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
            ? "负责人、状态或成员权限已变化。请重读后重新选择，本次不会覆盖他人的修改。"
            : "当前请求未被接受，请重读后再试。",
        );
        onRefresh();
      } else
        setMessage(
          "操作结果尚未确认。请重试原操作；当前负责人相同不代表这次操作已成功。",
        );
    } finally {
      if (activeWrite.current === controller) {
        activeWrite.current = null;
        setBusy(false);
      }
    }
  };
  const self = candidates.find((v) => v.id === selfID);
  return (
    <section
      className="ticket-assignment-actions"
      aria-labelledby="ticket-assignment-title"
    >
      <h3 id="ticket-assignment-title">工单负责人</h3>
      {message && <p role="status">{message}</p>}
      {loading && <p role="status">正在读取负责人…</p>}
      {assignment && <p>当前：{assigneeLabel(assignment.assignee)}</p>}
      {pending ? (
        <div>
          <p>待确认操作：{pending.label}</p>
          <button
            type="button"
            disabled={busy || loading || !assignment}
            onClick={() => void submit(pending)}
          >
            {busy ? "正在提交…" : "重试原负责人操作"}
          </button>
        </div>
      ) : confirmation ? (
        <div>
          <p>
            确认将负责人从“{assigneeLabel(assignment?.assignee ?? null)}
            {assignment?.assignee ? ` · ${assignment.assignee.id}` : ""}”改为“
            {confirmation.label}”？分配不会授予项目权限。
          </p>
          <button
            type="button"
            disabled={busy || loading}
            onClick={() => void submit(confirmation)}
          >
            确认负责人
          </button>
          <button type="button" onClick={() => setConfirmation(null)}>
            返回选择
          </button>
        </div>
      ) : assignment?.canAssign ? (
        <div>
          <label htmlFor="ticket-assignee">选择负责人</label>
          <select
            id="ticket-assignee"
            value={selected}
            disabled={loading}
            onChange={(e) => setSelected(e.target.value)}
          >
            <option value="">请选择成员</option>
            {candidates.map((c) => (
              <option
                key={c.id}
                value={c.id}
                disabled={c.id === assignment.assignee?.id}
              >
                {c.displayName}
                {candidates.some(
                  (other) =>
                    other.id !== c.id && other.displayName === c.displayName,
                )
                  ? ` · ${c.id}`
                  : ""}
              </option>
            ))}
          </select>
          {selected && (
            <p>
              已选成员标识：<code>{selected}</code>
            </p>
          )}
          {candidates.length === 0 && <p>当前没有可分配成员。</p>}
          <button
            type="button"
            disabled={!selected || loading}
            onClick={() => {
              const candidate = candidates.find((c) => c.id === selected);
              if (candidate) prepare(candidate);
            }}
          >
            设置负责人
          </button>
          {self && self.id !== assignment.assignee?.id && (
            <button
              type="button"
              disabled={loading}
              onClick={() => prepare(self)}
            >
              由我负责
            </button>
          )}
          {assignment.assignee && (
            <button
              type="button"
              disabled={loading}
              onClick={() => prepare(null)}
            >
              清空负责人
            </button>
          )}
          {next && (
            <button
              type="button"
              disabled={loading}
              onClick={() => void loadMore()}
            >
              加载更多候选成员
            </button>
          )}
        </div>
      ) : assignment ? (
        <p>
          当前不能变更负责人。已完成或已取消的工单须先重新打开；归档项目和只读成员不能分配。
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
        刷新负责人
      </button>
    </section>
  );
}
