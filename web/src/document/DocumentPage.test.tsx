import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { DocumentPage, CreateDocument } from "./DocumentPage";
import {
  documentClient,
  DocumentError,
  parseView,
  type DocumentClient,
  type DocumentView,
} from "./api";
import { MarkdownView } from "./MarkdownView";
const view = (revision = 1): DocumentView => ({
  current: {
    ref: { type: "document", id: "doc_test" },
    project: { type: "project", id: "prj_test" },
    revision,
    title: `标题 ${revision}`,
    body_markdown: `正文 ${revision}`,
    format_version: "nexus-markdown-v1",
    created_by: { kind: "user", id: "usr_test" },
    created_at: "2026-09-26T01:00:00Z",
    restored_from_revision: null,
    view: {
      format: "nexus-markdown-view-v1",
      nodes: [
        {
          kind: "paragraph",
          children: [{ kind: "text", text: `正文 ${revision}` }],
        },
      ],
    },
    rendering_failure: null,
  },
  relations: [],
  timeline: [],
});
function mockClient(): DocumentClient {
  return {
    ...documentClient,
    read: vi.fn().mockResolvedValue(view()),
    save: vi.fn().mockResolvedValue({
      ref: { type: "document", id: "doc_test" },
      applied_revision: 2,
    }),
    history: vi.fn().mockResolvedValue({ items: [], next_cursor: null }),
  };
}
const noop = () => {};
async function edit() {
  fireEvent.click(await screen.findByRole("button", { name: "编辑 Markdown" }));
  fireEvent.change(screen.getByLabelText("Markdown 正文"), {
    target: { value: "本地草稿😀" },
  });
}
describe("Document editing", () => {
  it("keeps draft on conflict and requires explicit reapplication", async () => {
    const client = mockClient();
    vi.mocked(client.save).mockRejectedValueOnce(
      new DocumentError("冲突", 409, 2),
    );
    vi.mocked(client.read)
      .mockResolvedValueOnce(view())
      .mockResolvedValue(view(2));
    render(
      <DocumentPage
        workspaceID="wrk_test"
        documentID="doc_test"
        client={client}
        onSessionExpired={noop}
      />,
    );
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "保存新版本" }));
    await screen.findByText("请比较最新版本与本地草稿");
    expect(
      (screen.getByLabelText("Markdown 正文") as HTMLTextAreaElement).value,
    ).toBe("本地草稿😀");
    expect(client.save).toHaveBeenCalledTimes(1);
    fireEvent.click(
      screen.getByRole("button", { name: "基于最新版本重新应用草稿" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "保存新版本" }));
    await waitFor(() => expect(client.save).toHaveBeenCalledTimes(2));
    const calls = vi.mocked(client.save).mock.calls;
    expect(calls[0]![2].base_revision).toBe(1);
    expect(calls[1]![2].base_revision).toBe(2);
    expect(calls[1]![2].client_operation_id).not.toBe(
      calls[0]![2].client_operation_id,
    );
  });
  it("retries uncertain commits with exactly the same input", async () => {
    const client = mockClient();
    vi.mocked(client.save).mockRejectedValueOnce(new DocumentError("网络中断"));
    render(
      <DocumentPage
        workspaceID="wrk_test"
        documentID="doc_test"
        client={client}
        onSessionExpired={noop}
      />,
    );
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "保存新版本" }));
    fireEvent.click(await screen.findByRole("button", { name: "重试原操作" }));
    await waitFor(() => expect(client.save).toHaveBeenCalledTimes(2));
    expect(vi.mocked(client.save).mock.calls[0]![2]).toEqual(
      vi.mocked(client.save).mock.calls[1]![2],
    );
  });
  it("clears source, draft and history when access is revoked", async () => {
    const client = mockClient();
    vi.mocked(client.read)
      .mockResolvedValueOnce(view())
      .mockRejectedValue(new DocumentError("权限撤销", 404));
    render(
      <DocumentPage
        workspaceID="wrk_test"
        documentID="doc_test"
        client={client}
        onSessionExpired={noop}
      />,
    );
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "读取最新版本" }));
    await screen.findByText("文档不可用");
    expect(screen.queryByDisplayValue("本地草稿😀")).toBeNull();
    expect(screen.queryByText("正文 1")).toBeNull();
  });
  it("ignores out-of-order reads", async () => {
    const client = mockClient();
    let resolve!: (v: DocumentView) => void;
    vi.mocked(client.read)
      .mockImplementationOnce(
        () =>
          new Promise((r) => {
            resolve = r;
          }),
      )
      .mockResolvedValue(view(2));
    render(
      <DocumentPage
        workspaceID="wrk_test"
        documentID="doc_test"
        client={client}
        onSessionExpired={noop}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "重新读取" }));
    await screen.findByRole("heading", { name: "标题 2" });
    await act(async () => resolve(view(1)));
    expect(screen.queryByRole("heading", { name: "标题 1" })).toBeNull();
  });
  it("shows Project visibility before creating and preserves uncertain creation", async () => {
    const client = {
      ...mockClient(),
      create: vi
        .fn()
        .mockRejectedValueOnce(new DocumentError("结果不明"))
        .mockResolvedValue({
          ref: { type: "document", id: "doc_created" },
          applied_revision: 1,
        }),
    };
    render(
      <CreateDocument
        workspaceID="wrk_test"
        ticketID="tkt_test"
        projectID="prj_test"
        client={client}
        onSessionExpired={noop}
      />,
    );
    expect(screen.getByText(/文档及全部历史对当前 Project/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText("文档标题"), {
      target: { value: "设计" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建设计文档" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "重试原创建操作" }),
    );
    await screen.findByRole("link", { name: "打开设计文档" });
    expect(client.create.mock.calls[0]).toEqual(client.create.mock.calls[1]);
  });
});
describe("safe display projection", () => {
  it("rejects extra props, unknown nodes, unsafe URLs and versions", () => {
    for (const node of [
      { kind: "script" },
      { kind: "paragraph", onClick: "bad" },
      { kind: "link", url: "javascript:alert(1)" },
      { kind: "link", url: "https://example.test/%0a" },
      { kind: "heading", level: 7 },
    ])
      expect(() =>
        parseView({ format: "nexus-markdown-view-v1", nodes: [node] }),
      ).toThrow();
    expect(() => parseView({ format: "unknown", nodes: [] })).toThrow();
  });
  it("renders text safely and only opens links on a click", () => {
    const projection = parseView({
      format: "nexus-markdown-view-v1",
      nodes: [
        {
          kind: "paragraph",
          children: [
            { kind: "text", text: "<img src=x onerror=alert(1)>" },
            {
              kind: "link",
              url: "https://example.test",
              children: [{ kind: "text", text: "外链" }],
            },
          ],
        },
      ],
    });
    const { container } = render(<MarkdownView view={projection} />);
    expect(container.querySelector("img")).toBeNull();
    const link = screen.getByRole("link", { name: "外链" });
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
    expect(link.getAttribute("referrerpolicy")).toBe("no-referrer");
  });
});
