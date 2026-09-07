import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ChannelRequestError,
  browserChannelMessageClient,
  channelLocation,
  channelPagePath,
} from "./api";

const csrfToken = "c".repeat(43);

const messagePayload = {
  ref: { type: "message", id: "msg_2" },
  channel: { type: "channel", id: "chn_main" },
  thread: null,
  author: { kind: "user", id: "usr_author" },
  body: "Authoritative body.",
  created_at: "2026-09-01T03:04:05.123Z",
};

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(document, "cookie");
});

describe("Channel Message API adapter", () => {
  it("recognizes only canonical Channel page locations", () => {
    expect(channelLocation("/workspaces/wrk_main/channels/chn_main")).toEqual({
      workspaceID: "wrk_main",
      channelID: "chn_main",
    });
    expect(
      channelLocation("/workspaces/wrk_main/channels/not-a-channel"),
    ).toBeNull();
    expect(
      channelLocation("/workspaces/wrk_main/channels/chn_main/messages"),
    ).toBeNull();
    expect(channelPagePath("wrk_main", "chn_main")).toBe(
      "/workspaces/wrk_main/channels/chn_main",
    );
    expect(channelPagePath("wrong", "chn_main")).toBeNull();
  });

  it("loads and validates a canonical Message page with opaque pagination", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        data: {
          messages: [
            {
              ...messagePayload,
              ref: { type: "message", id: "msg_1" },
              body: "Older body.",
            },
            messagePayload,
          ],
          older_cursor: "opaque_cursor_1",
        },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const page = await browserChannelMessageClient.listMessages(
      "wrk_main",
      "chn_main",
      "opaque_cursor_0",
    );

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/workspaces/wrk_main/channels/chn_main/messages?limit=50&before=opaque_cursor_0",
      expect.objectContaining({
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
      }),
    );
    expect(page).toEqual({
      messages: [
        expect.objectContaining({ id: "msg_1", body: "Older body." }),
        expect.objectContaining({
          id: "msg_2",
          authorID: "usr_author",
          threadID: null,
        }),
      ],
      olderCursor: "opaque_cursor_1",
    });
  });

  it("sends exact Message input with readable CSRF and distinguishes creation from retry", async () => {
    setCSRFCookie();
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: messagePayload }, 201))
      .mockResolvedValueOnce(jsonResponse({ data: messagePayload }, 200));
    vi.stubGlobal("fetch", fetchMock);

    const input = {
      clientOperationID: "web:stable-operation",
      body: "Authoritative body.",
    };
    const created = await browserChannelMessageClient.createMessage(
      "wrk_main",
      "chn_main",
      input,
    );
    const retried = await browserChannelMessageClient.createMessage(
      "wrk_main",
      "chn_main",
      input,
    );

    expect(created.created).toBe(true);
    expect(retried.created).toBe(false);
    expect(created.message.id).toBe(retried.message.id);
    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/v1/workspaces/wrk_main/channels/chn_main/messages",
      expect.objectContaining({
        method: "POST",
        credentials: "same-origin",
        cache: "no-store",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/json; charset=utf-8",
          "X-CSRF-Token": csrfToken,
        },
        body: JSON.stringify({
          client_operation_id: "web:stable-operation",
          body: "Authoritative body.",
        }),
      }),
    );
  });

  it("starts a Thread only from the requested Channel and Message", async () => {
    setCSRFCookie();
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        {
          data: {
            ref: { type: "thread", id: "thr_created" },
            channel: { type: "channel", id: "chn_main" },
            source_message: { type: "message", id: "msg_source" },
            title: "Investigate latency",
            visibility: "restricted",
            created_at: "2026-09-01T03:05:00Z",
          },
        },
        201,
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const thread = await browserChannelMessageClient.startThread(
      "wrk_main",
      "chn_main",
      "msg_source",
      { title: "Investigate latency", visibility: "restricted" },
    );

    expect(thread).toEqual({
      id: "thr_created",
      channelID: "chn_main",
      sourceMessageID: "msg_source",
      title: "Investigate latency",
      visibility: "restricted",
      createdAt: "2026-09-01T03:05:00Z",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/workspaces/wrk_main/channels/chn_main/messages/msg_source/threads",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          title: "Investigate latency",
          visibility: "restricted",
        }),
      }),
    );
  });

  it("maps revoked access and refuses writes without CSRF", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(
            { error: { code: "not_found", message: "must be ignored" } },
            404,
          ),
        ),
    );
    await expect(
      browserChannelMessageClient.listMessages("wrk_main", "chn_main"),
    ).rejects.toMatchObject({
      code: "not_found",
      status: 404,
      userMessage: "该 Channel 或来源 Message 不存在，或你已没有读取权限。",
    } satisfies Partial<ChannelRequestError>);

    await expect(
      browserChannelMessageClient.createMessage("wrk_main", "chn_main", {
        clientOperationID: "web:operation",
        body: "body",
      }),
    ).rejects.toMatchObject({
      code: "csrf_failed",
      status: 403,
    } satisfies Partial<ChannelRequestError>);
  });

  it("keeps transport-security failures distinct from role denial", async () => {
    setCSRFCookie();
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(
            { error: { code: "invalid_origin", message: "must be ignored" } },
            403,
          ),
        ),
    );

    await expect(
      browserChannelMessageClient.createMessage("wrk_main", "chn_main", {
        clientOperationID: "web:operation",
        body: "body",
      }),
    ).rejects.toMatchObject({
      code: "invalid_origin",
      status: 403,
      userMessage: "当前站点的安全入口配置无效，请联系实例管理员。",
    } satisfies Partial<ChannelRequestError>);
  });

  it("fails closed on drifted refs, duplicate IDs and unexpected statuses", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(
          jsonResponse({
            data: {
              messages: [
                {
                  ...messagePayload,
                  channel: { type: "channel", id: "chn_other" },
                },
              ],
              older_cursor: null,
            },
          }),
        )
        .mockResolvedValueOnce(
          jsonResponse({
            data: {
              messages: [messagePayload, messagePayload],
              older_cursor: null,
            },
          }),
        )
        .mockResolvedValueOnce(
          jsonResponse({ data: { messages: [], older_cursor: null } }, 201),
        ),
    );

    for (let index = 0; index < 3; index += 1) {
      await expect(
        browserChannelMessageClient.listMessages("wrk_main", "chn_main"),
      ).rejects.toMatchObject({
        status: index === 2 ? 201 : 200,
      } satisfies Partial<ChannelRequestError>);
    }
  });
});

function setCSRFCookie(): void {
  Object.defineProperty(document, "cookie", {
    configurable: true,
    value: `__Host-radishnexus-csrf=${csrfToken}`,
  });
}

function jsonResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: {
      get: (name: string) =>
        name.toLowerCase() === "content-type" ? "application/json" : null,
    } as Headers,
    json: async () => payload,
  } as Response;
}
