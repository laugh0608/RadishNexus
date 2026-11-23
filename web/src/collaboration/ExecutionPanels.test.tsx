import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AuthRequestError } from "../auth/api";
import { MessageWorkButton, ExecutionDiscussions } from "./ExecutionPanels";
import { executionClient } from "./execution-api";
import { useExecutionWrite } from "./execution-write";
import { projectTicketsClient } from "../workspace/project-tickets-api";
import { browserDiscoveryClient } from "../workspace/api";
import * as threadDiscovery from "../channel/channel-threads-api";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const onSessionExpired = vi.fn();

function MessageHarness({ revalidating }: { revalidating: boolean }) {
  const write = useExecutionWrite({ onSessionExpired });
  if (revalidating) return <p>正在核对页面权限</p>;
  return (
    <MessageWorkButton
      workspace="wrk_one"
      channel="chn_one"
      message="msg_source"
      write={write}
      onSessionExpired={onSessionExpired}
    />
  );
}

it("confirms the title and retries the original operation after reauthorization", async () => {
  let committed = false;
  const list = vi
    .spyOn(executionClient, "list")
    .mockImplementation(async () => ({
      items: committed
        ? [{ id: "tkt_new", title: "Synthetic work", canUnlink: false }]
        : [],
      nextCursor: null,
      canWrite: true,
    }));
  const create = vi
    .spyOn(executionClient, "create")
    .mockRejectedValueOnce(new AuthRequestError("response lost"))
    .mockImplementationOnce(async () => {
      committed = true;
      return "tkt_new";
    });
  const view = render(<MessageHarness revalidating={false} />);
  expect(list).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "此消息的工单" }));
  fireEvent.change(await screen.findByLabelText("工单标题"), {
    target: { value: "Synthetic work" },
  });
  fireEvent.click(screen.getByRole("button", { name: "创建工单" }));
  expect(create).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "确认创建工单" }));
  await screen.findByText("结果尚未确认，可重试原操作。");
  view.rerender(<MessageHarness revalidating={true} />);
  expect(screen.queryByText("Synthetic work")).toBeNull();
  view.rerender(<MessageHarness revalidating={false} />);
  fireEvent.click(await screen.findByRole("button", { name: /重试原操作/u }));
  await waitFor(() => expect(create).toHaveBeenCalledTimes(2));
  expect(create.mock.calls[0]?.[3]).toEqual(create.mock.calls[1]?.[3]);
  expect(
    (await screen.findByRole("link", { name: "Synthetic work" })).getAttribute(
      "href",
    ),
  ).toBe("/workspaces/wrk_one/tickets/tkt_new");
});

function DiscussionHarness({
  kind = "thread",
}: {
  kind?: "thread" | "ticket";
}) {
  const write = useExecutionWrite({ onSessionExpired });
  return (
    <ExecutionDiscussions
      workspace="wrk_one"
      kind={kind}
      id={kind === "thread" ? "thr_one" : "tkt_selected"}
      project="prj_one"
      onSessionExpired={onSessionExpired}
      write={write}
      onChanged={() => {}}
    />
  );
}

it("uses Project discovery to link a work item and removes the exact relation", async () => {
  let linked = false;
  vi.spyOn(executionClient, "list").mockImplementation(async () => ({
    items: linked
      ? [
          {
            id: "tkt_selected",
            title: "Selectable work",
            linkID: "lnk_one",
            canUnlink: true,
          },
        ]
      : [],
    nextCursor: null,
    canWrite: true,
  }));
  vi.spyOn(projectTicketsClient, "tickets").mockResolvedValue({
    items: [
      {
        id: "tkt_selected",
        title: "Selectable work",
        status: "open",
        assignee: null,
        priority: "unset",
        dueDate: null,
        classification: { ticketType: "unset" as const, labels: [] },
      },
    ],
    nextCursor: null,
  });
  const write = vi
    .spyOn(executionClient, "discussion")
    .mockImplementation(async (_workspace, _ticket, _thread, link) => {
      linked = link === undefined;
    });
  render(<DiscussionHarness />);
  fireEvent.click(await screen.findByRole("button", { name: "关联项目工单" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Selectable work" }),
  );
  expect(write).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "确认关联工单" }));
  await screen.findByRole("link", { name: "Selectable work" });
  expect(write.mock.calls[0]?.slice(0, 4)).toEqual([
    "wrk_one",
    "tkt_selected",
    "thr_one",
    undefined,
  ]);
  fireEvent.click(screen.getByRole("button", { name: "解除关联" }));
  fireEvent.click(screen.getByRole("button", { name: "确认解除关联" }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
  expect(write.mock.calls[1]?.[3]).toBe("lnk_one");
  await waitFor(() =>
    expect(screen.queryByRole("link", { name: "Selectable work" })).toBeNull(),
  );
});

function threadDirectory() {
  vi.spyOn(browserDiscoveryClient, "channels").mockResolvedValue({
    items: [
      { id: "chn_one", title: "活跃频道", status: "active" },
      { id: "chn_archived", title: "归档频道", status: "archived" },
    ],
    nextCursor: null,
  });
  vi.spyOn(threadDiscovery, "listChannelThreads").mockResolvedValue({
    items: [
      { id: "thr_existing", title: "已有讨论" },
      { id: "thr_one", title: "候选讨论" },
    ],
    nextCursor: null,
  });
}

async function chooseThread() {
  fireEvent.click(await screen.findByRole("button", { name: "关联已有讨论" }));
  expect(
    await screen.findByRole("button", { name: "归档频道（已归档）" }),
  ).toHaveProperty("disabled", true);
  fireEvent.click(screen.getByRole("button", { name: "活跃频道" }));
  fireEvent.click(await screen.findByRole("button", { name: "候选讨论" }));
}

it("confirms the two endpoints from the Ticket and retries the exact command after a lost reply", async () => {
  threadDirectory();
  vi.spyOn(executionClient, "list").mockImplementation(
    async (_workspace, target) => ({
      items:
        target.kind === "ticket"
          ? [
              {
                id: "thr_existing",
                title: "已有讨论",
                linkID: "lnk_existing",
                canUnlink: true,
              },
            ]
          : [],
      canWrite: true,
      nextCursor: null,
    }),
  );
  const link = vi
    .spyOn(executionClient, "discussion")
    .mockRejectedValueOnce(new AuthRequestError("lost reply"))
    .mockResolvedValue(undefined);
  render(<DiscussionHarness kind="ticket" />);
  fireEvent.click(await screen.findByRole("button", { name: "关联已有讨论" }));
  fireEvent.click(await screen.findByRole("button", { name: "活跃频道" }));
  expect(
    await screen.findByRole("button", { name: "已有讨论（已关联）" }),
  ).toHaveProperty("disabled", true);
  fireEvent.click(screen.getByRole("button", { name: "候选讨论" }));
  const confirm = await screen.findByRole("button", { name: "确认关联讨论" });
  expect(link).not.toHaveBeenCalled();
  fireEvent.click(confirm);
  fireEvent.click(
    await screen.findByRole("button", { name: "重试原操作 · 关联执行讨论" }),
  );
  await screen.findByText("操作已确认。");
  expect(link).toHaveBeenCalledTimes(2);
  expect(link.mock.calls[0]!.slice(0, 4)).toEqual([
    "wrk_one",
    "tkt_selected",
    "thr_one",
    undefined,
  ]);
  expect(link.mock.calls[1]!.slice(0, 5)).toEqual(
    link.mock.calls[0]!.slice(0, 5),
  );
});

it.each(["denied", "duplicate", "archived"] as const)(
  "blocks confirmation when the selected Thread read reports %s",
  async (scenario) => {
    threadDirectory();
    vi.spyOn(executionClient, "list").mockImplementation(
      async (_workspace, target) => {
        if (target.kind === "thread" && scenario === "denied")
          throw new AuthRequestError("不可访问", { status: 404 });
        return {
          items:
            target.kind === "thread" && scenario === "duplicate"
              ? [
                  {
                    id: "tkt_selected",
                    title: "工单",
                    canUnlink: true,
                    linkID: "lnk_one",
                  },
                ]
              : [],
          nextCursor: null,
          canWrite: target.kind === "ticket" || scenario !== "archived",
        };
      },
    );
    const link = vi.spyOn(executionClient, "discussion");
    render(<DiscussionHarness kind="ticket" />);
    await chooseThread();
    await screen.findByText(
      {
        denied: "无法读取所选讨论，请重新选择。",
        duplicate: "该讨论已关联本工单。",
        archived: "当前无法新增这条讨论关联。",
      }[scenario],
    );
    expect(screen.queryByText(/候选讨论/)).toBeNull();
    expect(screen.queryByRole("button", { name: "确认关联讨论" })).toBeNull();
    expect(link).not.toHaveBeenCalled();
  },
);
