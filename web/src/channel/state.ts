import type { ChannelMessage, ChannelMessagePage } from "./api";

export type ChannelPageState =
  | { status: "connecting" }
  | {
      status: "loading-history";
      bufferedMessages: readonly ChannelMessage[];
    }
  | { status: "error"; message: string }
  | {
      status: "ready";
      messages: readonly ChannelMessage[];
      olderCursor: string | null;
    };

export type ChannelPageAction =
  | { type: "connect" }
  | { type: "ready" }
  | { type: "message-created"; message: ChannelMessage }
  | { type: "history-loaded"; page: ChannelMessagePage }
  | { type: "older-loaded"; page: ChannelMessagePage }
  | { type: "failed"; message: string };

const contractMessage =
  "实时消息与 canonical history 不一致，请重新读取 Channel。";

export function channelPageReducer(
  state: ChannelPageState,
  action: ChannelPageAction,
): ChannelPageState {
  switch (action.type) {
    case "connect":
      return { status: "connecting" };
    case "ready":
      return state.status === "connecting"
        ? { status: "loading-history", bufferedMessages: [] }
        : state;
    case "message-created":
      if (state.status === "loading-history") {
        const bufferedMessages = appendMessage(
          state.bufferedMessages,
          action.message,
        );
        return bufferedMessages === null
          ? { status: "error", message: contractMessage }
          : { ...state, bufferedMessages };
      }
      if (state.status === "ready") {
        const messages = appendMessage(state.messages, action.message);
        return messages === null
          ? { status: "error", message: contractMessage }
          : { ...state, messages };
      }
      return state.status === "connecting"
        ? { status: "error", message: contractMessage }
        : state;
    case "history-loaded": {
      if (state.status !== "loading-history") {
        return state;
      }
      const messages = mergeInitialHistory(
        action.page.messages,
        state.bufferedMessages,
      );
      return messages === null
        ? { status: "error", message: contractMessage }
        : {
            status: "ready",
            messages,
            olderCursor: action.page.olderCursor,
          };
    }
    case "older-loaded": {
      if (state.status !== "ready") {
        return state;
      }
      const messages = prependOlderPage(action.page.messages, state.messages);
      return messages === null
        ? {
            status: "error",
            message: "历史分页返回了重复或冲突的 Message，请重新读取 Channel。",
          }
        : {
            status: "ready",
            messages,
            olderCursor: action.page.olderCursor,
          };
    }
    case "failed":
      return { status: "error", message: action.message };
  }
}

function mergeInitialHistory(
  history: readonly ChannelMessage[],
  buffered: readonly ChannelMessage[],
): readonly ChannelMessage[] | null {
  let result: readonly ChannelMessage[] = [...history];
  for (const message of buffered) {
    const merged = appendMessage(result, message);
    if (merged === null) {
      return null;
    }
    result = merged;
  }
  return result;
}

function prependOlderPage(
  older: readonly ChannelMessage[],
  current: readonly ChannelMessage[],
): readonly ChannelMessage[] | null {
  const ids = new Set(current.map((message) => message.id));
  if (older.some((message) => ids.has(message.id))) {
    return null;
  }
  return [...older, ...current];
}

function appendMessage(
  messages: readonly ChannelMessage[],
  candidate: ChannelMessage,
): readonly ChannelMessage[] | null {
  const existing = messages.find((message) => message.id === candidate.id);
  if (existing === undefined) {
    return [...messages, candidate];
  }
  return sameMessage(existing, candidate) ? messages : null;
}

function sameMessage(left: ChannelMessage, right: ChannelMessage): boolean {
  return (
    left.id === right.id &&
    left.channelID === right.channelID &&
    left.threadID === right.threadID &&
    left.authorID === right.authorID &&
    left.body === right.body &&
    left.createdAt === right.createdAt
  );
}
