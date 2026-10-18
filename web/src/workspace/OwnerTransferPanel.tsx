import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { AuthRequestError } from "../auth/api";
import { Field } from "./configuration-ui";
import type { ConfigurationPage } from "./configuration-api";
import {
  ownerTransferClient,
  validOwnerOperationID,
  type OwnerTransferClient,
  type Ownership,
  type OwnerCandidate,
  type OwnerTransferRequest,
  type OwnerTransferResult,
} from "./owner-transfer-api";

interface Props {
  workspaceID: string;
  userID: string;
  onSessionExpired: () => void;
  onChanged: () => Promise<void>;
  client?: OwnerTransferClient;
}
export function OwnerTransferPanel(props: Props) {
  const [open, setOpen] = useState(false);
  const [mounted, setMounted] = useState(false);
  return (
    <section className="foundation-configuration" aria-label="工作区管理权交接">
      <button
        type="button"
        className="secondary-button"
        aria-expanded={open}
        onClick={() => {
          setMounted(true);
          setOpen(!open);
        }}
      >
        管理权交接与结果查询
      </button>
      {mounted ? (
        <div hidden={!open}>
          <OwnerTransferEditor
            {...props}
            client={props.client ?? ownerTransferClient}
          />
        </div>
      ) : null}
    </section>
  );
}
function OwnerTransferEditor({
  workspaceID,
  userID,
  onSessionExpired,
  onChanged,
  client,
}: Props & { client: OwnerTransferClient }) {
  const [ownership, setOwnership] = useState<Ownership | null>(null);
  const [page, setPage] = useState<ConfigurationPage<OwnerCandidate> | null>(
    null,
  );
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [selected, setSelected] = useState<OwnerCandidate | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(false);
  const [pending, setPending] = useState<OwnerTransferRequest | null>(null);
  const [unresolvedQueries, setUnresolvedQueries] = useState<string[]>([]);
  const [queryID, setQueryID] = useState("");
  const [result, setResult] = useState<OwnerTransferResult | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const actions = useRef(false);
  const active = useRef(true);
  const feedback = useRef<HTMLParagraphElement>(null);
  const readController = useRef<AbortController | null>(null);
  const writeController = useRef<AbortController | null>(null);
  const callbacks = useRef({ onSessionExpired, onChanged });
  useEffect(() => {
    callbacks.current = { onSessionExpired, onChanged };
  }, [onSessionExpired, onChanged]);
  const refresh = useCallback(
    async (after?: string) => {
      readController.current?.abort();
      const controller = new AbortController();
      readController.current = controller;
      setLoading(true);
      setOwnership(null);
      setPage(null);
      setSelected(null);
      setConfirmed(false);
      try {
        const value = await client.ownership(
          workspaceID,
          userID,
          controller.signal,
        );
        if (controller.signal.aborted || !active.current) return;
        setOwnership(value);
        if (value.canTransfer) {
          const candidates = await client.candidates(
            workspaceID,
            userID,
            after,
            controller.signal,
          );
          if (controller.signal.aborted || !active.current) return;
          setPage(candidates);
        }
        await callbacks.current.onChanged();
      } catch (error) {
        if (controller.signal.aborted || !active.current) return;
        setOwnership(null);
        setPage(null);
        if (error instanceof AuthRequestError && error.status === 401)
          callbacks.current.onSessionExpired();
        else
          setMessage(
            error instanceof Error
              ? error.message
              : "无法确认当前管理权限，请刷新。",
          );
      } finally {
        if (!controller.signal.aborted && active.current) setLoading(false);
      }
    },
    [client, workspaceID, userID],
  );
  useEffect(() => {
    active.current = true;
    const initial = new AbortController();
    queueMicrotask(() => {
      if (!initial.signal.aborted) void refresh();
    });
    const restore = () => {
      if (!actions.current) {
        setCursors([undefined]);
        void refresh();
      }
    };
    const visible = () => {
      if (document.visibilityState === "visible") restore();
    };
    window.addEventListener("focus", restore);
    window.addEventListener("pageshow", restore);
    document.addEventListener("visibilitychange", visible);
    return () => {
      initial.abort();
      active.current = false;
      readController.current?.abort();
      writeController.current?.abort();
      window.removeEventListener("focus", restore);
      window.removeEventListener("pageshow", restore);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [refresh]);
  const finish = async (out: OwnerTransferResult) => {
    setResult(out);
    if (out.result === "applied") {
      setUnresolvedQueries((current) =>
        current.filter((id) => id !== out.operationID),
      );
      setPending((current) =>
        current?.client_operation_id === out.operationID ? null : current,
      );
      setMessage(
        "该操作已完成。以下是当时的交接结果，当前权限以刷新后的工作区为准。",
      );
      setCursors([undefined]);
      await refresh();
    } else
      setMessage(
        "当前未观察到成功记录，不能据此判断已回滚；原请求可能仍在处理。请稍后查询，或确认原请求结束后用原请求重试。",
      );
    feedback.current?.focus();
  };
  const execute = async (input: OwnerTransferRequest) => {
    if (actions.current) return;
    actions.current = true;
    setBusy(true);
    setPending(input);
    setQueryID(input.client_operation_id);
    setMessage(null);
    setResult(null);
    const controller = new AbortController();
    writeController.current = controller;
    try {
      const out = await client.transfer(
        workspaceID,
        userID,
        input,
        controller.signal,
      );
      if (!active.current || controller.signal.aborted) return;
      await finish(out);
    } catch (error) {
      if (!active.current || controller.signal.aborted) return;
      const status =
        error instanceof AuthRequestError ? error.status : undefined;
      if (status === 401) {
        callbacks.current.onSessionExpired();
        return;
      }
      if (status !== undefined && status < 500) {
        setPending(null);
        setConfirmed(false);
        setSelected(null);
        setMessage(
          error instanceof Error
            ? error.message
            : "交接请求被拒绝，请刷新并核对。",
        );
        setCursors([undefined]);
        await refresh();
      } else
        setMessage(
          "交接结果尚未确认。请复制操作 ID 并查询结果；保留原请求重试，不要重新发起另一笔交接。",
        );
      feedback.current?.focus();
    } finally {
      actions.current = false;
      if (active.current) setBusy(false);
    }
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (
      !ownership?.canTransfer ||
      !ownership.recent ||
      !selected ||
      !confirmed ||
      pending ||
      unresolvedQueries.length > 0 ||
      loading
    )
      return;
    void execute({
      client_operation_id: crypto.randomUUID(),
      target_user_id: selected.id,
      expected_actor_role_revision: ownership.revision,
      expected_target_role_revision: selected.revision,
      confirmed: true,
    });
  };
  const lookup = async (event: FormEvent) => {
    event.preventDefault();
    if (actions.current || !validOwnerOperationID(queryID.trim())) return;
    const operationID = queryID.trim();
    setUnresolvedQueries((current) =>
      current.includes(operationID) ? current : [...current, operationID],
    );
    actions.current = true;
    setBusy(true);
    setMessage(null);
    setResult(null);
    const controller = new AbortController();
    writeController.current = controller;
    try {
      const out = await client.status(
        workspaceID,
        userID,
        operationID,
        controller.signal,
      );
      if (active.current && !controller.signal.aborted) await finish(out);
    } catch (error) {
      if (!active.current || controller.signal.aborted) return;
      if (error instanceof AuthRequestError && error.status === 401) {
        callbacks.current.onSessionExpired();
        return;
      }
      if (
        error instanceof AuthRequestError &&
        (error.status === 403 || error.status === 404)
      ) {
        setOwnership(null);
        setPage(null);
        setSelected(null);
        setConfirmed(false);
      }
      setMessage(
        error instanceof Error ? error.message : "无法查询，请保留原操作 ID。",
      );
      feedback.current?.focus();
    } finally {
      actions.current = false;
      if (active.current) setBusy(false);
    }
  };
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(
        pending?.client_operation_id ?? queryID,
      );
      setMessage("操作 ID 已复制。刷新或重新登录后，可粘贴查询本人结果。");
    } catch {
      setMessage("无法自动复制，请手动复制操作 ID。");
    }
  };
  return (
    <div className="configuration-panel">
      <h3>交接 Workspace 管理权</h3>
      <p>
        只交接当前工作区的 owner 角色。已有项目管理员、私密对象和 Deployment
        显式权限不会自动转移。
      </p>
      <button
        type="button"
        disabled={busy || loading}
        onClick={() => {
          setCursors([undefined]);
          void refresh();
        }}
      >
        刷新交接权限与成员
      </button>
      {loading ? (
        <p role="status">正在核对当前权限…</p>
      ) : ownership && !ownership.canTransfer ? (
        <p>你当前是普通成员，可以查询本人此前的交接结果。</p>
      ) : null}
      {ownership?.canTransfer ? (
        <form onSubmit={submit}>
          {!ownership.recent ? (
            <p>交接前请退出并重新登录，再核对接收人并确认。</p>
          ) : null}
          <fieldset
            disabled={
              busy ||
              loading ||
              pending !== null ||
              unresolvedQueries.length > 0
            }
          >
            <Field label="接收管理权的成员">
              <select
                value={selected?.id ?? ""}
                required
                onChange={(event) => {
                  setSelected(
                    page?.items.find(
                      (item) => item.id === event.target.value,
                    ) ?? null,
                  );
                  setConfirmed(false);
                }}
              >
                <option value="">请选择已有成员</option>
                {page?.items.map((candidate) => (
                  <option key={candidate.id} value={candidate.id}>
                    {candidate.displayName} · {candidate.id}
                  </option>
                ))}
              </select>
            </Field>
            {page?.items.length === 0 ? (
              <p>
                当前没有可接任的普通成员。可先通过“账户与邀请”邀请成员加入。
              </p>
            ) : null}
            <div className="configuration-actions">
              <button
                type="button"
                disabled={cursors.length < 2 || !page}
                onClick={() => {
                  const next = cursors.slice(0, -1);
                  setCursors(next);
                  void refresh(next[next.length - 1]);
                }}
              >
                上一页成员
              </button>
              <button
                type="button"
                disabled={!page?.nextCursor}
                onClick={() => {
                  if (page?.nextCursor) {
                    setCursors([...cursors, page.nextCursor]);
                    void refresh(page.nextCursor);
                  }
                }}
              >
                下一页成员
              </button>
            </div>
            {selected ? (
              <p>
                接收人：{selected.displayName} · {selected.id}
              </p>
            ) : null}
            <label className="configuration-checkbox">
              <input
                type="checkbox"
                checked={confirmed}
                required
                onChange={(event) => setConfirmed(event.target.checked)}
              />
              我确认与接收人完成沟通：我将成为普通成员，对方将成为 Workspace
              owner；我在本工作区此前未兑换的邀请将失效。
            </label>
            <button
              type="submit"
              disabled={!ownership.recent || !confirmed || !selected}
            >
              确认交接管理权
            </button>
          </fieldset>
        </form>
      ) : null}
      {pending ? (
        <div>
          <p>
            待确认操作 ID：<code>{pending.client_operation_id}</code>
          </p>
          <button
            type="button"
            disabled={busy}
            onClick={() => void execute(pending)}
          >
            用原请求重试
          </button>
        </div>
      ) : null}
      {unresolvedQueries.length > 0 ? (
        <p>
          以下操作尚未确认，当前不能发起新的交接。请保留并查询原操作 ID：
          {unresolvedQueries.join("、")}
        </p>
      ) : null}
      <form onSubmit={(event) => void lookup(event)}>
        <h3>查询本人交接结果</h3>
        <Field label="交接操作 ID">
          <input
            autoComplete="off"
            value={queryID}
            disabled={busy}
            onChange={(event) => {
              setQueryID(event.target.value);
              setResult(null);
            }}
            placeholder="粘贴此前复制的操作 ID"
          />
        </Field>
        <div className="configuration-actions">
          <button
            type="submit"
            disabled={busy || !validOwnerOperationID(queryID.trim())}
          >
            查询结果
          </button>
          <button
            type="button"
            disabled={
              busy ||
              !validOwnerOperationID(
                (pending?.client_operation_id ?? queryID).trim(),
              )
            }
            onClick={() => void copy()}
          >
            复制操作 ID
          </button>
        </div>
      </form>
      <p role="status" tabIndex={-1} ref={feedback}>
        {busy ? "正在处理，请保留操作 ID…" : message}
      </p>
      {result?.result === "applied" ? (
        <div>
          <p>
            历史交接已完成：{result.actorID} → {result.targetID}
          </p>
          <p>完成时间：{result.occurredAt}</p>
        </div>
      ) : null}
    </div>
  );
}
