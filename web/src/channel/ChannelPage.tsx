import { useEffect, useState, type FormEvent } from "react";
import {
  ChannelRequestError,
  browserChannelMessageClient,
  newClientOperationID,
  type ChannelMessage,
  type ChannelMessageClient,
  type StartedThread,
  type ThreadVisibility,
} from "./api";

interface ChannelPageProps {
  workspaceID: string;
  channelID: string;
  client?: ChannelMessageClient;
  createOperationID?: () => string;
  onSessionExpired: () => void;
}

type ChannelPageState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | {
      status: "ready";
      messages: readonly ChannelMessage[];
      olderCursor: string | null;
    };

interface PendingMessageOperation {
  body: string;
  id: string;
}

interface ThreadDraft {
  messageID: string;
  title: string;
  visibility: ThreadVisibility;
}

export function ChannelPage({
  workspaceID,
  channelID,
  client = browserChannelMessageClient,
  createOperationID = newClientOperationID,
  onSessionExpired,
}: ChannelPageProps) {
  const [reloadKey, setReloadKey] = useState(0);
  const [state, setState] = useState<ChannelPageState>({ status: "loading" });
  const [olderLoading, setOlderLoading] = useState(false);
  const [olderError, setOlderError] = useState<string | null>(null);
  const [messageBody, setMessageBody] = useState("");
  const [messageSending, setMessageSending] = useState(false);
  const [messageError, setMessageError] = useState<string | null>(null);
  const [messageNotice, setMessageNotice] = useState<string | null>(null);
  const [pendingMessage, setPendingMessage] =
    useState<PendingMessageOperation | null>(null);
  const [threadDraft, setThreadDraft] = useState<ThreadDraft | null>(null);
  const [threadSending, setThreadSending] = useState(false);
  const [threadError, setThreadError] = useState<string | null>(null);
  const [startedThreads, setStartedThreads] = useState<
    Readonly<Record<string, StartedThread>>
  >({});

  useEffect(() => {
    const controller = new AbortController();
    void client
      .listMessages(workspaceID, channelID, undefined, controller.signal)
      .then(
        (page) => {
          if (!controller.signal.aborted) {
            setState({ status: "ready", ...page });
          }
        },
        (error: unknown) => {
          if (controller.signal.aborted) {
            return;
          }
          if (isExpiredSession(error)) {
            onSessionExpired();
            return;
          }
          setState({ status: "error", message: channelErrorMessage(error) });
        },
      );
    return () => controller.abort();
  }, [channelID, client, onSessionExpired, reloadKey, workspaceID]);

  const revokeVisibleState = (error: unknown): boolean => {
    if (isExpiredSession(error)) {
      onSessionExpired();
      return true;
    }
    if (error instanceof ChannelRequestError && error.status === 404) {
      setMessageBody("");
      setPendingMessage(null);
      setThreadDraft(null);
      setStartedThreads({});
      setState({ status: "error", message: error.userMessage });
      return true;
    }
    return false;
  };

  const loadOlder = async () => {
    if (state.status !== "ready" || state.olderCursor === null) {
      return;
    }
    setOlderLoading(true);
    setOlderError(null);
    try {
      const page = await client.listMessages(
        workspaceID,
        channelID,
        state.olderCursor,
      );
      const combined = [...page.messages, ...state.messages];
      if (
        new Set(combined.map((message) => message.id)).size !== combined.length
      ) {
        throw new ChannelRequestError(
          "历史分页返回了重复 Message，请刷新 Channel 后重试。",
        );
      }
      setState({
        status: "ready",
        messages: combined,
        olderCursor: page.olderCursor,
      });
    } catch (error) {
      if (!revokeVisibleState(error)) {
        setOlderError(channelErrorMessage(error));
      }
    } finally {
      setOlderLoading(false);
    }
  };

  const sendMessage = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (state.status !== "ready") {
      return;
    }
    setMessageSending(true);
    setMessageError(null);
    setMessageNotice(null);
    let operation = pendingMessage;
    if (operation === null || operation.body !== messageBody) {
      try {
        operation = { body: messageBody, id: createOperationID() };
        setPendingMessage(operation);
      } catch (error) {
        setMessageError(channelErrorMessage(error));
        setMessageSending(false);
        return;
      }
    }
    try {
      const outcome = await client.createMessage(workspaceID, channelID, {
        clientOperationID: operation.id,
        body: operation.body,
      });
      setState((current) => {
        if (
          current.status !== "ready" ||
          current.messages.some((message) => message.id === outcome.message.id)
        ) {
          return current;
        }
        return {
          ...current,
          messages: [...current.messages, outcome.message],
        };
      });
      setMessageBody("");
      setPendingMessage(null);
      setMessageNotice(
        outcome.created
          ? "Message 已发送并进入 Channel 历史。"
          : "已确认此前重试写入的同一 Message。",
      );
    } catch (error) {
      if (!revokeVisibleState(error)) {
        setMessageError(channelErrorMessage(error));
      }
    } finally {
      setMessageSending(false);
    }
  };

  const startThread = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (state.status !== "ready" || threadDraft === null) {
      return;
    }
    setThreadSending(true);
    setThreadError(null);
    try {
      const thread = await client.startThread(
        workspaceID,
        channelID,
        threadDraft.messageID,
        { title: threadDraft.title, visibility: threadDraft.visibility },
      );
      setStartedThreads((current) => ({
        ...current,
        [threadDraft.messageID]: thread,
      }));
      setThreadDraft(null);
    } catch (error) {
      if (!revokeVisibleState(error)) {
        setThreadError(channelErrorMessage(error));
      }
    } finally {
      setThreadSending(false);
    }
  };

  if (state.status === "loading") {
    return (
      <main className="channel-layout" aria-busy="true">
        <section className="channel-state-panel">
          <p className="section-kicker">Canonical Channel history</p>
          <h1>正在读取 Channel</h1>
          <p role="status">正在按当前 Session 和 Channel 权限恢复消息。</p>
        </section>
      </main>
    );
  }

  if (state.status === "error") {
    return (
      <main className="channel-layout">
        <section className="channel-state-panel">
          <p className="section-kicker">Channel unavailable</p>
          <h1>无法读取这个 Channel</h1>
          <p role="alert">{state.message}</p>
          <button
            className="primary-button"
            onClick={() => {
              setState({ status: "loading" });
              setOlderError(null);
              setReloadKey((key) => key + 1);
            }}
            type="button"
          >
            重新读取
          </button>
        </section>
      </main>
    );
  }

  return (
    <main className="channel-layout">
      <section className="channel-hero" aria-labelledby="channel-title">
        <div>
          <p className="section-kicker">Canonical Channel history</p>
          <h1 id="channel-title">Channel 消息</h1>
          <p>
            这里展示按当前权限读取的消息历史；翻页和写入都会重新确认访问权限。
          </p>
        </div>
        <div className="channel-identity">
          <span>Channel</span>
          <code>entity://channel/{channelID}</code>
          <small>{state.messages.length} 条已载入</small>
        </div>
      </section>

      <section className="channel-workspace">
        <div
          className="message-history"
          aria-labelledby="message-history-title"
        >
          <div className="channel-section-heading">
            <div>
              <p className="section-kicker">History</p>
              <h2 id="message-history-title">消息历史</h2>
            </div>
            {state.olderCursor === null ? null : (
              <button
                className="secondary-button"
                disabled={olderLoading || messageSending || threadSending}
                onClick={() => void loadOlder()}
                type="button"
              >
                {olderLoading ? "正在读取…" : "读取更早消息"}
              </button>
            )}
          </div>
          {olderError === null ? null : (
            <p className="inline-error" role="alert">
              {olderError}
            </p>
          )}
          {state.messages.length === 0 ? (
            <div className="channel-empty">
              <strong>这个 Channel 还没有 Message</strong>
              <p>发送第一条正文后，它会成为可追溯的权威讨论记录。</p>
            </div>
          ) : (
            <ol className="message-list">
              {state.messages.map((message) => {
                const createdThread = startedThreads[message.id];
                const editingThread = threadDraft?.messageID === message.id;
                return (
                  <li key={message.id}>
                    <article className="message-card">
                      <header>
                        <div>
                          <strong>{message.authorID}</strong>
                          <time dateTime={message.createdAt}>
                            {formatTimestamp(message.createdAt)}
                          </time>
                        </div>
                        <code>{message.id}</code>
                      </header>
                      <p className="message-body">{message.body}</p>
                      {message.threadID === null ? null : (
                        <p className="message-thread-ref">
                          Thread 回复 · <code>{message.threadID}</code>
                        </p>
                      )}
                      {createdThread === undefined ? (
                        <button
                          className="text-button"
                          disabled={
                            threadSending || messageSending || olderLoading
                          }
                          onClick={() => {
                            setThreadError(null);
                            setThreadDraft(
                              editingThread
                                ? null
                                : {
                                    messageID: message.id,
                                    title: "",
                                    visibility: "project",
                                  },
                            );
                          }}
                          type="button"
                        >
                          {editingThread
                            ? "取消发起 Thread"
                            : "从此消息发起 Thread"}
                        </button>
                      ) : (
                        <div className="thread-result" role="status">
                          <span>Thread 已创建</span>
                          <strong>{createdThread.title}</strong>
                          <code>{createdThread.id}</code>
                        </div>
                      )}
                      {editingThread && threadDraft !== null ? (
                        <form
                          className="thread-form"
                          onSubmit={(event) => void startThread(event)}
                        >
                          <label>
                            <span>Thread 标题</span>
                            <input
                              disabled={threadSending}
                              onChange={(event) =>
                                setThreadDraft({
                                  ...threadDraft,
                                  title: event.target.value,
                                })
                              }
                              required
                              value={threadDraft.title}
                            />
                          </label>
                          <label>
                            <span>可见性</span>
                            <select
                              disabled={threadSending}
                              onChange={(event) =>
                                setThreadDraft({
                                  ...threadDraft,
                                  visibility: event.target
                                    .value as ThreadVisibility,
                                })
                              }
                              value={threadDraft.visibility}
                            >
                              <option value="project">Project</option>
                              <option value="restricted">Restricted</option>
                            </select>
                          </label>
                          {threadError === null ? null : (
                            <p className="inline-error" role="alert">
                              {threadError}
                            </p>
                          )}
                          <button
                            className="secondary-button"
                            disabled={
                              threadSending ||
                              messageSending ||
                              olderLoading ||
                              threadDraft.title.trim() === ""
                            }
                            type="submit"
                          >
                            {threadSending ? "正在创建…" : "创建 Thread"}
                          </button>
                        </form>
                      ) : null}
                    </article>
                  </li>
                );
              })}
            </ol>
          )}
        </div>

        <aside className="message-composer" aria-labelledby="composer-title">
          <p className="section-kicker">Authoritative write</p>
          <h2 id="composer-title">发送 Message</h2>
          <p>网络中断后可直接重试；页面会复用同一次发送标识，避免重复消息。</p>
          <form onSubmit={(event) => void sendMessage(event)}>
            <label>
              <span>正文</span>
              <textarea
                disabled={messageSending}
                onChange={(event) => {
                  const body = event.target.value;
                  setMessageBody(body);
                  setMessageNotice(null);
                  if (pendingMessage?.body !== body) {
                    setPendingMessage(null);
                  }
                }}
                placeholder="记录一条不可变的讨论事实…"
                required
                rows={6}
                value={messageBody}
              />
            </label>
            {messageError === null ? null : (
              <p className="inline-error" role="alert">
                {messageError}
              </p>
            )}
            {messageNotice === null ? null : (
              <p className="inline-notice" role="status">
                {messageNotice}
              </p>
            )}
            <button
              className="primary-button"
              disabled={
                messageSending ||
                threadSending ||
                olderLoading ||
                messageBody.trim() === ""
              }
              type="submit"
            >
              {messageSending ? "正在发送…" : "发送到 Channel"}
            </button>
          </form>
        </aside>
      </section>
    </main>
  );
}

function channelErrorMessage(error: unknown): string {
  return error instanceof ChannelRequestError
    ? error.userMessage
    : "消息服务暂不可用，请稍后重试。";
}

function isExpiredSession(error: unknown): boolean {
  return error instanceof ChannelRequestError && error.status === 401;
}

function formatTimestamp(value: string): string {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(new Date(value));
}
