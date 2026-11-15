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

function DiscussionHarness() {
  const write = useExecutionWrite({ onSessionExpired });
  return (
    <ExecutionDiscussions
      workspace="wrk_one"
      kind="thread"
      id="thr_one"
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
