import { describe, expect, it } from "vitest";
import type { ChannelMessage } from "./api";
import { channelPageReducer, type ChannelPageState } from "./state";

const historyMessage: ChannelMessage = {
  id: "msg_history",
  channelID: "chn_main",
  threadID: null,
  authorID: "usr_author",
  body: "Canonical history.",
  createdAt: "2026-09-03T03:00:00Z",
};

const liveMessage: ChannelMessage = {
  ...historyMessage,
  id: "msg_live",
  body: "Buffered live message.",
  createdAt: "2026-09-03T03:01:00Z",
};

describe("Channel realtime reducer", () => {
  it("waits for ready, buffers live events during history, then deduplicates the merge", () => {
    let state: ChannelPageState = { status: "connecting" };
    state = channelPageReducer(state, { type: "ready" });
    state = channelPageReducer(state, {
      type: "message-created",
      message: liveMessage,
    });
    state = channelPageReducer(state, {
      type: "history-loaded",
      page: {
        messages: [historyMessage, liveMessage],
        olderCursor: "older_cursor",
      },
    });

    expect(state).toEqual({
      status: "ready",
      messages: [historyMessage, liveMessage],
      olderCursor: "older_cursor",
    });
  });

  it("deduplicates replay and local write results but rejects projection drift", () => {
    let state: ChannelPageState = {
      status: "ready",
      messages: [historyMessage],
      olderCursor: null,
    };
    state = channelPageReducer(state, {
      type: "message-created",
      message: historyMessage,
    });
    expect(state.status === "ready" ? state.messages : []).toHaveLength(1);

    state = channelPageReducer(state, {
      type: "message-created",
      message: { ...historyMessage, body: "Drifted body." },
    });
    expect(state).toEqual({
      status: "error",
      message: "实时消息与 canonical history 不一致，请重新读取 Channel。",
    });
  });

  it("clears authoritative content on resync and rejects overlapping older pages", () => {
    let state: ChannelPageState = {
      status: "ready",
      messages: [historyMessage],
      olderCursor: "older_cursor",
    };
    state = channelPageReducer(state, { type: "connect" });
    expect(state).toEqual({ status: "connecting" });

    state = channelPageReducer(state, { type: "ready" });
    state = channelPageReducer(state, {
      type: "history-loaded",
      page: { messages: [historyMessage], olderCursor: "older_cursor" },
    });
    state = channelPageReducer(state, {
      type: "older-loaded",
      page: { messages: [historyMessage], olderCursor: null },
    });
    expect(state).toEqual({
      status: "error",
      message: "历史分页返回了重复或冲突的 Message，请重新读取 Channel。",
    });
  });

  it("fails closed if a business event arrives before ready", () => {
    expect(
      channelPageReducer(
        { status: "connecting" },
        { type: "message-created", message: liveMessage },
      ),
    ).toEqual({
      status: "error",
      message: "实时消息与 canonical history 不一致，请重新读取 Channel。",
    });
  });
});
