import { TicketClassificationFields } from "./TicketClassificationFields";
import { ProjectThreadPicker } from "./ProjectThreadPicker";
import {
  emptyClassification,
  initialClassification,
} from "./ticket-classification-api";
import { useCallback, useState, type ReactNode } from "react";
import { useExecutionRead } from "./useExecutionRead";
import { browserChannelMessageClient } from "../channel/api";
import { requestStatus, type ExecutionWrite } from "./execution-write";
import { projectTicketsClient } from "../workspace/project-tickets-api";
import {
  executionClient,
  executionMessageLink,
  executionMessageLocation,
} from "./execution-api";

export function OperationStatus({
  write,
  target,
}: {
  write: ExecutionWrite;
  target?: string;
}) {
  return (
    <>
      {write.notice && <p role="status">{write.notice}</p>}
      {write.pending && write.pending.target === target && (
        <button type="button" disabled={write.busy} onClick={write.retry}>
          {write.busy ? "正在提交…" : `重试原操作 · ${write.pending.label}`}
        </button>
      )}
    </>
  );
}

function workPath(workspace: string, kind: "ticket" | "thread", id: string) {
  return `/workspaces/${encodeURIComponent(workspace)}/${kind}s/${encodeURIComponent(id)}`;
}

export function MessageWorkButton({
  workspace,
  channel,
  message,
  onSessionExpired,
  write,
}: {
  workspace: string;
  channel: string;
  message: string;
  onSessionExpired: () => void;
  write: ExecutionWrite;
}) {
  const [open, setOpen] = useState(() => write.pending?.target === message);
  const visible = open || write.pending?.target === message;
  return (
    <>
      <button
        type="button"
        className="text-button"
        aria-expanded={visible}
        disabled={write.pending?.target === message}
        onClick={() => setOpen((value) => !value)}
      >
        {visible ? "收起此消息的工单" : "此消息的工单"}
      </button>
      {visible && (
        <MessageWork
          workspace={workspace}
          channel={channel}
          message={message}
          onSessionExpired={onSessionExpired}
          write={write}
        />
      )}
    </>
  );
}

function MessageWork({
  workspace,
  channel,
  message,
  onSessionExpired,
  write,
}: {
  workspace: string;
  channel: string;
  message: string;
  onSessionExpired: () => void;
  write: ExecutionWrite;
}) {
  const writeRevision = write.revision;
  const [after, setAfter] = useState<string>();
  const [reload, setReload] = useState(0);
  const [title, setTitle] = useState("");
  const [classification, setClassification] = useState(emptyClassification);
  const [review, setReview] = useState(false);
  const [created, setCreated] = useState<string | null>(null);
  const load = useCallback(
    (signal: AbortSignal) => {
      void reload;
      void writeRevision;
      return executionClient.list(
        workspace,
        { kind: "message", channel, id: message },
        after,
        signal,
      );
    },
    [workspace, channel, message, after, reload, writeRevision],
  );
  const { data, error } = useExecutionRead(load, onSessionExpired, true, () => {
    if (write.pending?.target === message) write.clear();
    setCreated(null);
    setTitle("");
    setClassification(emptyClassification);
    setReview(false);
  });
  return (
    <section className="thread-form" aria-label="消息工单">
      {error ? (
        <p role="status">{error}</p>
      ) : data ? (
        <>
          {data.items.length === 0 && <p>此消息还没有可访问的工单。</p>}
          <ul>
            {data.items.map((row) => (
              <li key={row.id}>
                <a href={workPath(workspace, "ticket", row.id)}>{row.title}</a>
              </li>
            ))}
          </ul>
          {data.nextCursor && (
            <button type="button" onClick={() => setAfter(data.nextCursor!)}>
              下一页工单
            </button>
          )}
          {after && (
            <button type="button" onClick={() => setAfter(undefined)}>
              返回工单首页
            </button>
          )}
          {data.canWrite &&
            !write.pending &&
            (review ? (
              <div>
                <p>确认创建“{title}”？标题会向当前 Project 成员显示。</p>
                <button
                  type="button"
                  onClick={() => {
                    write.start(
                      "从消息创建工单",
                      async (operationID, signal) => {
                        const ticket = await executionClient.create(
                          workspace,
                          channel,
                          message,
                          {
                            title,
                            classification:
                              initialClassification(classification),
                            client_operation_id: operationID,
                            confirmed: true,
                          },
                          signal,
                        );
                        if (signal.aborted) return;
                        setCreated(ticket);
                        setReview(false);
                        setTitle("");
                        setClassification(emptyClassification);
                        setAfter(undefined);
                        setReload((value) => value + 1);
                      },
                      message,
                    );
                  }}
                >
                  确认创建工单
                </button>
                <button type="button" onClick={() => setReview(false)}>
                  返回编辑
                </button>
              </div>
            ) : (
              <form
                onSubmit={(event) => {
                  event.preventDefault();
                  if (title.trim()) setReview(true);
                }}
              >
                <label>
                  工单标题
                  <input
                    value={title}
                    required
                    maxLength={200}
                    onChange={(event) => setTitle(event.target.value)}
                  />
                </label>
                <TicketClassificationFields
                  workspace={workspace}
                  channel={channel}
                  onSessionExpired={onSessionExpired}
                  value={classification}
                  onChange={setClassification}
                />
                <button type="submit">创建工单</button>
              </form>
            ))}
          {created && (
            <a href={workPath(workspace, "ticket", created)}>打开新工单</a>
          )}
        </>
      ) : (
        <p role="status">正在读取工单…</p>
      )}
      {data?.canWrite && <OperationStatus write={write} target={message} />}
    </section>
  );
}

export function LocatedExecutionMessage({
  workspace,
  channel,
  thread,
  renderDiscussion,
  onSessionExpired,
  write,
}: {
  workspace: string;
  channel: string;
  thread?: string;
  renderDiscussion?: (messageID: string) => ReactNode;
  write: ExecutionWrite;
  onSessionExpired: () => void;
}) {
  const messageID = executionMessageLocation(window.location.search);
  const load = useCallback(
    async (signal: AbortSignal) => {
      const message = await executionClient.message(
        workspace,
        channel,
        messageID!,
        signal,
      );
      if (thread !== undefined && message.threadID !== thread)
        throw new TypeError("Wrong Thread");
      if (thread === undefined && message.threadID !== null) {
        // A Thread source relation identifies the channel and message. Only the
        // authorized message read identifies the reply's canonical parent.
        const path = executionMessageLink(workspace, {
          channelID: channel,
          messageID: message.id,
          threadID: message.threadID,
        });
        if (!path) throw new TypeError("Invalid reply location");
        if (!signal.aborted) window.location.replace(path);
        return null;
      }
      return message;
    },
    [workspace, channel, thread, messageID],
  );
  const { data, error } = useExecutionRead(
    load,
    onSessionExpired,
    messageID !== null,
  );
  if (messageID === null) return null;
  return (
    <section aria-label="原消息" className="message-card">
      {error ? (
        <p role="status">{error}</p>
      ) : data ? (
        <>
          <h3>原消息</h3>
          <time dateTime={data.createdAt}>
            {new Date(data.createdAt).toLocaleString()}
          </time>
          <p className="message-body">{data.body}</p>
          {renderDiscussion?.(data.id)}
          <MessageWorkButton
            workspace={workspace}
            channel={channel}
            message={data.id}
            onSessionExpired={onSessionExpired}
            write={write}
          />
        </>
      ) : (
        <p role="status">正在定位消息…</p>
      )}
    </section>
  );
}

export function ExecutionDiscussions({
  workspace,
  kind,
  id,
  project,
  onSessionExpired,
  write,
  onChanged,
}: {
  workspace: string;
  kind: "thread" | "ticket";
  id: string;
  project: string;
  onSessionExpired: () => void;
  write: ExecutionWrite;
  onChanged: () => void;
}) {
  const writeRevision = write.revision;
  const [after, setAfter] = useState<string>();
  const [reload, setReload] = useState(0);
  const [choose, setChoose] = useState(false);
  const [candidateCursor, setCandidateCursor] = useState<string>();
  const [selected, setSelected] = useState<{
    id: string;
    title: string;
    link?: string;
  } | null>(null);
  const load = useCallback(
    (signal: AbortSignal) => {
      void reload;
      void writeRevision;
      return executionClient.list(workspace, { kind, id }, after, signal);
    },
    [workspace, kind, id, after, reload, writeRevision],
  );
  const candidatesLoad = useCallback(
    (signal: AbortSignal) =>
      projectTicketsClient.tickets(workspace, project, candidateCursor, signal),
    [workspace, project, candidateCursor],
  );
  const { data, error } = useExecutionRead(load, onSessionExpired, true, () => {
    write.clear();
    setSelected(null);
    setChoose(false);
  });
  const candidates = useExecutionRead(
    candidatesLoad,
    onSessionExpired,
    kind === "thread" && choose && data?.canWrite === true,
  );
  const targetKind = kind === "thread" ? "ticket" : "thread";
  const changeRelationPage = (cursor?: string) => {
    setSelected(null);
    setChoose(false);
    setAfter(cursor);
  };
  const confirm = () => {
    if (!selected) return;
    setSelected(null);
    setChoose(false);
    write.start(
      selected.link
        ? "解除讨论关联"
        : kind === "ticket"
          ? "关联执行讨论"
          : "关联工单",
      async (operationID, signal) => {
        try {
          await executionClient.discussion(
            workspace,
            kind === "ticket" ? id : selected.id,
            kind === "ticket" ? selected.id : id,
            selected.link,
            { client_operation_id: operationID, confirmed: true },
            signal,
          );
        } catch (error) {
          const status = requestStatus(error);
          if (
            !signal.aborted &&
            (status === 403 || status === 404 || status === 409)
          )
            onChanged();
          throw error;
        }
        if (signal.aborted) return;
        setSelected(null);
        setChoose(false);
        setAfter(undefined);
        setReload((value) => value + 1);
        onChanged();
      },
    );
  };
  return (
    <section
      className="collaboration-panel"
      aria-label={kind === "thread" ? "关联工单" : "执行讨论"}
    >
      <h2>{kind === "thread" ? "关联工单" : "执行讨论"}</h2>
      {error ? (
        <p role="status">{error}</p>
      ) : data ? (
        <>
          {data.items.length === 0 && <p>当前没有可访问的关联。</p>}
          <ul>
            {data.items.map((row) => (
              <li key={row.id}>
                <a href={workPath(workspace, targetKind, row.id)}>
                  {row.title}
                </a>
                {row.canUnlink && row.linkID && (
                  <button
                    type="button"
                    disabled={!!write.pending}
                    onClick={() => {
                      setChoose(false);
                      setSelected({
                        id: row.id,
                        title: row.title,
                        link: row.linkID,
                      });
                    }}
                  >
                    解除关联
                  </button>
                )}
              </li>
            ))}
          </ul>
          {data.nextCursor && (
            <button
              type="button"
              onClick={() => changeRelationPage(data.nextCursor!)}
            >
              下一页关联
            </button>
          )}
          {after && (
            <button type="button" onClick={() => changeRelationPage()}>
              返回关联首页
            </button>
          )}
          {data.canWrite &&
            !write.pending &&
            (kind === "thread" || !selected) && (
              <button
                type="button"
                aria-expanded={choose}
                onClick={() => {
                  setSelected(null);
                  setCandidateCursor(undefined);
                  setChoose((value) => !value);
                }}
              >
                {kind === "thread"
                  ? choose
                    ? "收起工单选择"
                    : "关联项目工单"
                  : choose
                    ? "收起讨论选择"
                    : "关联已有讨论"}
              </button>
            )}
          {kind === "ticket" && choose && data.canWrite && !write.pending && (
            <ProjectThreadPicker
              workspaceID={workspace}
              projectID={project}
              purpose="execution"
              disabledThreadIDs={data.items.map((item) => item.id)}
              onSessionExpired={onSessionExpired}
              onUnavailable={onChanged}
              onSelect={(thread) => {
                setSelected(thread);
                setChoose(false);
              }}
            />
          )}
          {choose && candidates.error && (
            <p role="status">{candidates.error}</p>
          )}
          {choose && candidates.data && (
            <>
              <ul>
                {candidates.data.items.map((row) => (
                  <li key={row.id}>
                    <button
                      type="button"
                      disabled={!!write.pending}
                      onClick={() =>
                        setSelected({ id: row.id, title: row.title })
                      }
                    >
                      {row.title}
                    </button>
                  </li>
                ))}
              </ul>
              {candidates.data.nextCursor && (
                <button
                  type="button"
                  onClick={() =>
                    setCandidateCursor(candidates.data!.nextCursor!)
                  }
                >
                  下一页项目工单
                </button>
              )}
              {candidateCursor && (
                <button
                  type="button"
                  onClick={() => setCandidateCursor(undefined)}
                >
                  返回项目工单首页
                </button>
              )}
            </>
          )}
          {selected &&
            kind === "ticket" &&
            !selected.link &&
            !write.pending &&
            data.canWrite && (
              <ConfirmExecutionThread
                key={selected.id}
                workspace={workspace}
                ticket={id}
                thread={selected}
                onSessionExpired={onSessionExpired}
                onConfirm={confirm}
                onBack={() => {
                  setSelected(null);
                  setChoose(true);
                }}
              />
            )}
          {selected &&
            (kind === "thread" || selected.link) &&
            !write.pending &&
            (selected.link
              ? data.items.some(
                  (row) => row.linkID === selected.link && row.canUnlink,
                )
              : data.canWrite) && (
              <div>
                <p>
                  {selected.link ? "确认解除" : "确认关联"}“{selected.title}”？
                </p>
                <button type="button" onClick={confirm}>
                  {selected.link ? "确认解除关联" : "确认关联工单"}
                </button>
                <button type="button" onClick={() => setSelected(null)}>
                  返回
                </button>
              </div>
            )}
          <button
            type="button"
            onClick={() => {
              changeRelationPage();
              setReload((value) => value + 1);
            }}
          >
            刷新关联
          </button>
        </>
      ) : (
        <p role="status">正在读取关联…</p>
      )}
      {!error && <OperationStatus write={write} />}
    </section>
  );
}

function ConfirmExecutionThread({
  workspace,
  ticket,
  thread,
  onSessionExpired,
  onConfirm,
  onBack,
}: {
  workspace: string;
  ticket: string;
  thread: { id: string; title: string };
  onSessionExpired: () => void;
  onConfirm: () => void;
  onBack: () => void;
}) {
  const load = useCallback(
    (signal: AbortSignal) =>
      executionClient.list(
        workspace,
        { kind: "thread", id: thread.id },
        undefined,
        signal,
      ),
    [workspace, thread.id],
  );
  const { data, error } = useExecutionRead(load, onSessionExpired);
  return (
    <div>
      {error ? (
        <p role="alert">无法读取所选讨论，请重新选择。</p>
      ) : !data ? (
        <p role="status">正在核对讨论…</p>
      ) : data.items.some((row) => row.id === ticket) ? (
        <p>该讨论已关联本工单。</p>
      ) : !data.canWrite ? (
        <p>当前无法新增这条讨论关联。</p>
      ) : (
        <>
          <p>确认关联“{thread.title}”？</p>
          <button type="button" onClick={onConfirm}>
            确认关联讨论
          </button>
        </>
      )}
      <button type="button" onClick={onBack}>
        重新选择讨论
      </button>
    </div>
  );
}

export function ExecutionReplies({
  workspace,
  channel,
  thread,
  onSessionExpired,
  write,
  messageWrite,
  onChanged,
}: {
  workspace: string;
  channel: string;
  thread: string;
  onSessionExpired: () => void;
  write: ExecutionWrite;
  messageWrite: ExecutionWrite;
  onChanged: () => void;
}) {
  const [before, setBefore] = useState<string>();
  const [reload, setReload] = useState(0);
  const [body, setBody] = useState("");
  const load = useCallback(
    (signal: AbortSignal) => {
      void reload;
      return executionClient.replies(
        workspace,
        thread,
        channel,
        before,
        signal,
      );
    },
    [workspace, thread, channel, before, reload],
  );
  const { data, error } = useExecutionRead(load, onSessionExpired, true, () => {
    write.clear();
    messageWrite.clear();
    setBody("");
  });
  return (
    <section className="collaboration-panel" aria-label="讨论回复">
      <h2>讨论回复</h2>
      <LocatedExecutionMessage
        workspace={workspace}
        channel={channel}
        thread={thread}
        onSessionExpired={onSessionExpired}
        write={messageWrite}
      />
      {error ? (
        <p role="status">{error}</p>
      ) : data ? (
        <>
          {data.messages.length === 0 && <p>当前还没有回复。</p>}
          <ol className="message-list">
            {data.messages.map((message) => (
              <li className="message-card" key={message.id}>
                <strong>{message.authorID}</strong>{" "}
                <time dateTime={message.createdAt}>
                  {new Date(message.createdAt).toLocaleString()}
                </time>
                <p className="message-body">{message.body}</p>
                <MessageWorkButton
                  workspace={workspace}
                  channel={channel}
                  message={message.id}
                  onSessionExpired={onSessionExpired}
                  write={messageWrite}
                />
              </li>
            ))}
          </ol>
          {data.olderCursor && (
            <button type="button" onClick={() => setBefore(data.olderCursor!)}>
              读取更早回复
            </button>
          )}
          {before && (
            <button type="button" onClick={() => setBefore(undefined)}>
              返回最新回复
            </button>
          )}
          {data.canReply && !write.pending && (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                if (!body.trim()) return;
                write.start("发送回复", async (operationID, signal) => {
                  const out = await browserChannelMessageClient.createMessage(
                    workspace,
                    channel,
                    { threadID: thread, body, clientOperationID: operationID },
                    signal,
                  );
                  if (out.message.threadID !== thread)
                    throw new TypeError("Unexpected reply receipt");
                  if (signal.aborted) return;
                  setBody("");
                  setBefore(undefined);
                  setReload((value) => value + 1);
                  onChanged();
                });
              }}
            >
              <label>
                回复正文
                <textarea
                  value={body}
                  required
                  onChange={(event) => setBody(event.target.value)}
                />
              </label>
              <button type="submit">发送回复</button>
            </form>
          )}
          <button type="button" onClick={() => setReload((value) => value + 1)}>
            刷新回复
          </button>
        </>
      ) : (
        <p role="status">正在读取回复…</p>
      )}
      {!error && <OperationStatus write={write} />}
    </section>
  );
}
