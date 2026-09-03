import { describe, expect, it, vi } from "vitest";
import {
  channelEventsPath,
  createBrowserChannelRealtimeClient,
  type ChannelRealtimeHandlers,
} from "./realtime";

const messageEnvelope = {
  data: {
    ref: { type: "message", id: "msg_live" },
    channel: { type: "channel", id: "chn_main" },
    thread: null,
    author: { kind: "user", id: "usr_author" },
    body: "Live authoritative body.",
    created_at: "2026-09-03T03:04:05.123Z",
  },
};

describe("Channel realtime adapter", () => {
  it("opens only the scoped credentialed SSE endpoint and parses known events", () => {
    const source = new FakeEventSource();
    const factory = vi.fn().mockReturnValue(source);
    const handlers = testHandlers();
    const connection = createBrowserChannelRealtimeClient(factory).connect(
      "wrk_main",
      "chn_main",
      handlers,
    );

    expect(factory).toHaveBeenCalledWith(
      "/api/v1/workspaces/wrk_main/channels/chn_main/events",
      { withCredentials: true },
    );
    source.emit("ready", {}, "cursor_ready");
    source.emit("message.created", messageEnvelope, "cursor_message");

    expect(handlers.onReady).toHaveBeenCalledOnce();
    expect(handlers.onMessageCreated).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "msg_live",
        channelID: "chn_main",
        body: "Live authoritative body.",
      }),
    );
    expect(source.close).not.toHaveBeenCalled();

    connection.close();
    connection.close();
    expect(source.close).toHaveBeenCalledOnce();
  });

  it("closes control events before handing them to the state machine", () => {
    const resyncSource = new FakeEventSource();
    const resyncHandlers = testHandlers();
    createBrowserChannelRealtimeClient(() => resyncSource).connect(
      "wrk_main",
      "chn_main",
      resyncHandlers,
    );
    resyncSource.emit("resync-required", {}, "inherited_cursor");
    expect(resyncSource.close).toHaveBeenCalledOnce();
    expect(resyncHandlers.onResyncRequired).toHaveBeenCalledOnce();

    const revokedSource = new FakeEventSource();
    const revokedHandlers = testHandlers();
    createBrowserChannelRealtimeClient(() => revokedSource).connect(
      "wrk_main",
      "chn_main",
      revokedHandlers,
    );
    revokedSource.emit("access-revoked", {}, "inherited_cursor");
    expect(revokedSource.close).toHaveBeenCalledOnce();
    expect(revokedHandlers.onAccessRevoked).toHaveBeenCalledOnce();
  });

  it("fails closed on invalid cursor, control data or Message scope", () => {
    for (const emitInvalid of [
      (source: FakeEventSource) => source.emit("ready", {}, "bad cursor"),
      (source: FakeEventSource) =>
        source.emit("access-revoked", { reason: "secret" }, ""),
      (source: FakeEventSource) =>
        source.emit(
          "message.created",
          {
            data: {
              ...messageEnvelope.data,
              channel: { type: "channel", id: "chn_other" },
            },
          },
          "cursor_message",
        ),
    ]) {
      const source = new FakeEventSource();
      const handlers = testHandlers();
      createBrowserChannelRealtimeClient(() => source).connect(
        "wrk_main",
        "chn_main",
        handlers,
      );

      emitInvalid(source);

      expect(source.close).toHaveBeenCalledOnce();
      expect(handlers.onContractError).toHaveBeenCalledWith(
        expect.objectContaining({
          userMessage: "服务返回的实时消息不符合当前契约，请重新读取 Channel。",
        }),
      );
    }
  });

  it("leaves transport errors to native EventSource reconnection", () => {
    const source = new FakeEventSource();
    const handlers = testHandlers();
    createBrowserChannelRealtimeClient(() => source).connect(
      "wrk_main",
      "chn_main",
      handlers,
    );

    source.onerror?.(new Event("error"));

    expect(handlers.onConnectionError).toHaveBeenCalledOnce();
    expect(source.close).not.toHaveBeenCalled();
  });

  it("rejects malformed route IDs before opening EventSource", () => {
    expect(channelEventsPath("wrk_main", "chn_main")).toBe(
      "/api/v1/workspaces/wrk_main/channels/chn_main/events",
    );
    expect(() => channelEventsPath("wrong", "chn_main")).toThrow(
      "Channel 地址无效",
    );
  });
});

class FakeEventSource {
  readonly close = vi.fn();
  onerror: ((event: Event) => unknown) | null = null;
  private readonly listeners = new Map<
    string,
    (event: MessageEvent<string>) => void
  >();

  addEventListener(
    type: string,
    listener: (event: MessageEvent<string>) => void,
  ): void {
    this.listeners.set(type, listener);
  }

  emit(type: string, data: unknown, lastEventId: string): void {
    this.listeners.get(type)?.(
      new MessageEvent(type, {
        data: JSON.stringify(data),
        lastEventId,
      }),
    );
  }
}

function testHandlers(): ChannelRealtimeHandlers {
  return {
    onReady: vi.fn(),
    onMessageCreated: vi.fn(),
    onResyncRequired: vi.fn(),
    onAccessRevoked: vi.fn(),
    onConnectionError: vi.fn(),
    onContractError: vi.fn(),
  };
}
