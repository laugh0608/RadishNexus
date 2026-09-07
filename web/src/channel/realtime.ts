import {
  ChannelRequestError,
  parseChannelMessageEnvelope,
  type ChannelMessage,
} from "./api";

export interface ChannelRealtimeHandlers {
  onReady: () => void;
  onMessageCreated: (message: ChannelMessage) => void;
  onResyncRequired: () => void;
  onAccessRevoked: () => void;
  onConnectionError: () => void;
  onContractError: (error: ChannelRequestError) => void;
}

export interface ChannelRealtimeConnection {
  close(): void;
}

export interface ChannelRealtimeClient {
  connect(
    workspaceID: string,
    channelID: string,
    handlers: ChannelRealtimeHandlers,
  ): ChannelRealtimeConnection;
}

interface EventSourceLike {
  close(): void;
  addEventListener(
    type: string,
    listener: (event: MessageEvent<string>) => void,
  ): void;
  onerror: ((event: Event) => unknown) | null;
}

type EventSourceFactory = (
  url: string,
  options: EventSourceInit,
) => EventSourceLike;

export function createBrowserChannelRealtimeClient(
  eventSourceFactory: EventSourceFactory = (url, options) =>
    new EventSource(url, options),
): ChannelRealtimeClient {
  return {
    connect(workspaceID, channelID, handlers) {
      const path = channelEventsPath(workspaceID, channelID);
      let closed = false;
      let source: EventSourceLike;
      try {
        source = eventSourceFactory(path, { withCredentials: true });
      } catch (error) {
        throw new ChannelRequestError(
          "无法建立 Channel 实时连接，请检查浏览器与网络后重试。",
          { cause: error },
        );
      }

      const close = () => {
        if (closed) {
          return;
        }
        closed = true;
        source.close();
      };
      const guarded = (
        event: MessageEvent<string>,
        parse: (value: MessageEvent<string>) => void,
      ) => {
        if (closed) {
          return;
        }
        try {
          parse(event);
        } catch (error) {
          close();
          handlers.onContractError(
            new ChannelRequestError(
              "服务返回的实时消息不符合当前契约，请重新读取 Channel。",
              { cause: error },
            ),
          );
        }
      };

      source.addEventListener("ready", (event) =>
        guarded(event, (value) => {
          requireCursor(value.lastEventId, "ready.id");
          parseEmptyData(value.data, "ready.data");
          handlers.onReady();
        }),
      );
      source.addEventListener("message.created", (event) =>
        guarded(event, (value) => {
          requireCursor(value.lastEventId, "message.created.id");
          handlers.onMessageCreated(
            parseChannelMessageEnvelope(
              parseJSON(value.data, "message.created.data"),
              channelID,
            ),
          );
        }),
      );
      source.addEventListener("resync-required", (event) =>
        guarded(event, (value) => {
          parseEmptyData(value.data, "resync-required.data");
          close();
          handlers.onResyncRequired();
        }),
      );
      source.addEventListener("access-revoked", (event) =>
        guarded(event, (value) => {
          parseEmptyData(value.data, "access-revoked.data");
          close();
          handlers.onAccessRevoked();
        }),
      );
      source.onerror = () => {
        if (!closed) {
          handlers.onConnectionError();
        }
      };
      return { close };
    },
  };
}

export const browserChannelRealtimeClient =
  createBrowserChannelRealtimeClient();

export function channelEventsPath(
  workspaceID: string,
  channelID: string,
): string {
  if (!validPathID(workspaceID, "wrk_") || !validPathID(channelID, "chn_")) {
    throw new ChannelRequestError("Channel 地址无效，无法建立实时连接。", {
      code: "invalid",
      status: 400,
    });
  }
  return `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/channels/${encodeURIComponent(channelID)}/events`;
}

function parseEmptyData(value: string, path: string): void {
  const parsed = parseJSON(value, path);
  if (
    typeof parsed !== "object" ||
    parsed === null ||
    Array.isArray(parsed) ||
    Object.keys(parsed).length !== 0
  ) {
    throw new TypeError(`${path} must be an empty object`);
  }
}

function parseJSON(value: string, path: string): unknown {
  try {
    return JSON.parse(value) as unknown;
  } catch (error) {
    throw new TypeError(`${path} must be valid JSON`, { cause: error });
  }
}

function requireCursor(value: string, path: string): void {
  if (!validCursor(value)) {
    throw new TypeError(`${path} is invalid`);
  }
}

function validCursor(value: string): boolean {
  return (
    value.length >= 1 && value.length <= 512 && /^[A-Za-z0-9_-]+$/u.test(value)
  );
}

function validPathID(value: string, prefix: string): boolean {
  if (
    value.length <= prefix.length ||
    value.length > 128 ||
    !value.startsWith(prefix)
  ) {
    return false;
  }
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (
      codePoint <= 0x20 ||
      codePoint > 0x7f ||
      character === "/" ||
      character === "?" ||
      character === "#"
    ) {
      return false;
    }
  }
  return true;
}
