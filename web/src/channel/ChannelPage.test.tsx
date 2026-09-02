import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ChannelPage } from "./ChannelPage";
import {
  ChannelRequestError,
  type ChannelMessage,
  type ChannelMessageClient,
} from "./api";

const olderMessage: ChannelMessage = {
  id: "msg_older",
  channelID: "chn_main",
  threadID: null,
  authorID: "usr_author",
  body: "Older authoritative body.",
  createdAt: "2026-09-01T03:00:00Z",
};

const newerMessage: ChannelMessage = {
  id: "msg_newer",
  channelID: "chn_main",
  threadID: null,
  authorID: "usr_author",
  body: "Newer authoritative body.",
  createdAt: "2026-09-01T03:01:00Z",
};

describe("ChannelPage", () => {
  it("renders canonical history and prepends an older page", async () => {
    const client = testClient({
      listMessages: vi
        .fn()
        .mockResolvedValueOnce({
          messages: [newerMessage],
          olderCursor: "older_cursor",
        })
        .mockResolvedValueOnce({
          messages: [olderMessage],
          olderCursor: null,
        }),
    });
    renderChannel(client);

    expect(await screen.findByText("Newer authoritative body.")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "读取更早消息" }));

    expect(await screen.findByText("Older authoritative body.")).toBeDefined();
    expect(client.listMessages).toHaveBeenNthCalledWith(
      2,
      "wrk_main",
      "chn_main",
      "older_cursor",
    );
    const bodies = screen
      .getAllByText(/authoritative body\./u)
      .map((element) => element.textContent);
    expect(bodies).toEqual([
      "Older authoritative body.",
      "Newer authoritative body.",
    ]);
  });

  it("shows an explicit empty state", async () => {
    renderChannel(testClient());
    expect(
      await screen.findByText("这个 Channel 还没有 Message"),
    ).toBeDefined();
  });

  it("keeps one idempotency key across an ambiguous send retry", async () => {
    const createMessage = vi
      .fn()
      .mockRejectedValueOnce(
        new ChannelRequestError("无法连接到 RadishNexus，请检查网络后重试。"),
      )
      .mockResolvedValueOnce({ message: newerMessage, created: false });
    const client = testClient({ createMessage });
    const createOperationID = vi.fn().mockReturnValue("web:stable-retry");
    renderChannel(client, { createOperationID });
    await screen.findByText("这个 Channel 还没有 Message");

    fireEvent.change(screen.getByLabelText("正文"), {
      target: { value: "Newer authoritative body." },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送到 Channel" }));
    expect(
      await screen.findByText("无法连接到 RadishNexus，请检查网络后重试。"),
    ).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "发送到 Channel" }));
    expect(
      await screen.findByText("已确认此前重试写入的同一 Message。"),
    ).toBeDefined();
    expect(screen.getByText("Newer authoritative body.")).toBeDefined();
    expect(createOperationID).toHaveBeenCalledOnce();
    expect(createMessage).toHaveBeenNthCalledWith(1, "wrk_main", "chn_main", {
      clientOperationID: "web:stable-retry",
      body: "Newer authoritative body.",
    });
    expect(createMessage).toHaveBeenNthCalledWith(2, "wrk_main", "chn_main", {
      clientOperationID: "web:stable-retry",
      body: "Newer authoritative body.",
    });
  });

  it("creates a structured Thread source without copying Message body", async () => {
    const startThread = vi.fn().mockResolvedValue({
      id: "thr_created",
      channelID: "chn_main",
      sourceMessageID: "msg_newer",
      title: "Investigate latency",
      visibility: "restricted",
      createdAt: "2026-09-01T03:02:00Z",
    });
    renderChannel(
      testClient({
        listMessages: vi.fn().mockResolvedValue({
          messages: [newerMessage],
          olderCursor: null,
        }),
        startThread,
      }),
    );
    await screen.findByText("Newer authoritative body.");

    fireEvent.click(
      screen.getByRole("button", { name: "从此消息发起 Thread" }),
    );
    fireEvent.change(screen.getByLabelText("Thread 标题"), {
      target: { value: "Investigate latency" },
    });
    fireEvent.change(screen.getByLabelText("可见性"), {
      target: { value: "restricted" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建 Thread" }));

    expect(await screen.findByText("Thread 已创建")).toBeDefined();
    expect(screen.getByText("thr_created")).toBeDefined();
    expect(
      screen
        .getByRole("link", { name: "打开 canonical Thread" })
        .getAttribute("href"),
    ).toBe("/workspaces/wrk_main/threads/thr_created");
    expect(startThread).toHaveBeenCalledWith(
      "wrk_main",
      "chn_main",
      "msg_newer",
      { title: "Investigate latency", visibility: "restricted" },
    );
    expect(startThread.mock.calls[0]).not.toContain(
      "Newer authoritative body.",
    );
  });

  it("clears previously rendered bodies when a later request proves access revoked", async () => {
    const client = testClient({
      listMessages: vi
        .fn()
        .mockResolvedValueOnce({
          messages: [newerMessage],
          olderCursor: "older_cursor",
        })
        .mockRejectedValueOnce(
          new ChannelRequestError(
            "该 Channel 或来源 Message 不存在，或你已没有读取权限。",
            { code: "not_found", status: 404 },
          ),
        ),
    });
    renderChannel(client);
    await screen.findByText("Newer authoritative body.");
    fireEvent.click(screen.getByRole("button", { name: "读取更早消息" }));

    expect(
      await screen.findByRole("heading", { name: "无法读取这个 Channel" }),
    ).toBeDefined();
    expect(screen.queryByText("Newer authoritative body.")).toBeNull();
  });

  it("returns control to the shell when Session expires", async () => {
    const onSessionExpired = vi.fn();
    render(
      <ChannelPage
        channelID="chn_main"
        client={testClient({
          listMessages: vi.fn().mockRejectedValue(
            new ChannelRequestError("当前会话已失效，请重新登录。", {
              code: "unauthenticated",
              status: 401,
            }),
          ),
        })}
        onSessionExpired={onSessionExpired}
        workspaceID="wrk_main"
      />,
    );
    await waitFor(() => expect(onSessionExpired).toHaveBeenCalledOnce());
  });
});

function renderChannel(
  client: ChannelMessageClient,
  options: { createOperationID?: () => string } = {},
): void {
  render(
    <ChannelPage
      channelID="chn_main"
      client={client}
      createOperationID={options.createOperationID}
      onSessionExpired={vi.fn()}
      workspaceID="wrk_main"
    />,
  );
}

function testClient(
  overrides: Partial<ChannelMessageClient> = {},
): ChannelMessageClient {
  return {
    listMessages: vi.fn().mockResolvedValue({
      messages: [],
      olderCursor: null,
    }),
    createMessage: vi.fn(),
    startThread: vi.fn(),
    ...overrides,
  };
}
