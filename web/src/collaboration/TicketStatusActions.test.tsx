import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { CollaborationPage } from "./CollaborationPage";
import {
  CollaborationRequestError,
  type CollaborationClient,
  type CollaborationView,
  type TicketCurrent,
} from "./api";
vi.mock("./TicketComponents", () => ({ TicketComponents: () => null }));
vi.mock("../document/DocumentPage", () => ({ CreateDocument: () => null }));
const current: TicketCurrent = {
  ref: { type: "ticket", id: "tkt_one" },
  project: { type: "project", id: "prj_one" },
  title: "执行事项",
  status: "open",
  statusRevision: 1,
  createdBy: { kind: "user", id: "usr_one" },
  createdAt: "2026-10-13T01:00:00Z",
  updatedAt: "2026-10-13T01:00:00Z",
};
function view(
  status: TicketCurrent["status"] = "open",
  statusRevision = 1,
  changeStatus = true,
): CollaborationView {
  return {
    current: { ...current, status, statusRevision },
    capabilities: { changeStatus },
    relations: [],
    timeline: [],
  };
}
function setup(change = vi.fn(), load = vi.fn().mockResolvedValue(view())) {
  const client: CollaborationClient = {
    loadView: load,
    changeTicketStatus: change,
    proposeDecision: vi.fn(),
    acceptDecision: vi.fn(),
    createTicket: vi.fn(),
  };
  const expired = vi.fn(),
    id = vi.fn().mockReturnValue("web:status-one");
  const ui = render(
    <CollaborationPage
      workspaceID="wrk_main"
      entityType="ticket"
      entityID="tkt_one"
      client={client}
      createOperationID={id}
      onSessionExpired={expired}
    />,
  );
  return { client, load, change, expired, id, ...ui };
}
async function button(name: string) {
  return screen.findByRole("button", { name });
}
describe("Ticket status actions", () => {
  it("requires explicit confirmation and reloads authoritative current after applied result", async () => {
    const change = vi.fn().mockResolvedValue({
      ticket: current.ref,
      appliedStatus: "done",
      appliedStatusRevision: 2,
    });
    const load = vi
      .fn()
      .mockResolvedValueOnce(view())
      .mockResolvedValue(view("open", 3));
    setup(change, load);
    fireEvent.click(await button("标记完成"));
    expect(change).not.toHaveBeenCalled();
    fireEvent.click(await button("确认标记完成"));
    expect(await screen.findByText("当前：待开始 · 状态版本 3")).toBeDefined();
    expect(screen.getByText(/已确认原操作：已完成（版本 2）/)).toBeDefined();
    expect(change).toHaveBeenCalledWith(
      "wrk_main",
      "tkt_one",
      {
        clientOperationID: "web:status-one",
        baseStatusRevision: 1,
        toStatus: "done",
        confirmed: true,
      },
      expect.any(AbortSignal),
    );
  });
  it("retains unknown payload through refresh and never infers own success from matching state", async () => {
    const change = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("connection lost"))
      .mockResolvedValue({
        ticket: current.ref,
        appliedStatus: "in-progress",
        appliedStatusRevision: 2,
      });
    const load = vi
      .fn()
      .mockResolvedValueOnce(view())
      .mockResolvedValue(view("in-progress", 2));
    const { id } = setup(change, load);
    fireEvent.click(await button("开始处理"));
    await button("重试原状态操作");
    fireEvent.click(await button("刷新工单状态"));
    expect(await screen.findByText("当前：处理中 · 状态版本 2")).toBeDefined();
    expect(screen.queryByRole("button", { name: "标记完成" })).toBeNull();
    fireEvent.click(await button("重试原状态操作"));
    await waitFor(() => expect(change).toHaveBeenCalledTimes(2));
    expect(change.mock.calls[0][2]).toEqual(change.mock.calls[1][2]);
    expect(id).toHaveBeenCalledOnce();
  });
  it("clears stale confirmation on conflict and permits a fresh selection only after read", async () => {
    const change = vi
      .fn()
      .mockRejectedValue(
        new CollaborationRequestError("conflict", { status: 409 }),
      );
    setup(
      change,
      vi.fn().mockResolvedValueOnce(view()).mockResolvedValue(view("done", 2)),
    );
    fireEvent.click(await button("开始处理"));
    await button("重新打开");
    expect(screen.getByText(/本次未应用/)).toBeDefined();
    expect(screen.queryByRole("button", { name: "重试原状态操作" })).toBeNull();
    fireEvent.click(await button("重新打开"));
    expect(change).toHaveBeenCalledOnce();
    await button("确认重新打开");
  });
  it("hides writes after permission downgrade and removes uncertain commands", async () => {
    const change = vi.fn().mockRejectedValue(new TypeError("lost"));
    setup(
      change,
      vi
        .fn()
        .mockResolvedValueOnce(view())
        .mockResolvedValue(view("open", 1, false)),
    );
    fireEvent.click(await button("开始处理"));
    await button("重试原状态操作");
    fireEvent.focus(window);
    expect(await screen.findByText(/当前工单只读/)).toBeDefined();
    expect(screen.queryByRole("button", { name: "重试原状态操作" })).toBeNull();
  });
  it.each([401, 404])(
    "clears readable content and commands on %s",
    async (status) => {
      const { expired } = setup(
        vi
          .fn()
          .mockRejectedValue(
            new CollaborationRequestError("unavailable", { status }),
          ),
      );
      fireEvent.click(await button("开始处理"));
      expect(await screen.findByRole("alert")).toBeDefined();
      expect(screen.queryByText("执行事项")).toBeNull();
      expect(expired).toHaveBeenCalledTimes(status === 401 ? 1 : 0);
    },
  );
  it("aborts an in-flight action on refresh and ignores its late success", async () => {
    let finish!: (value: unknown) => void;
    const change = vi.fn().mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    setup(
      change,
      vi
        .fn()
        .mockResolvedValueOnce(view())
        .mockResolvedValue(view("canceled", 2)),
    );
    fireEvent.click(await button("开始处理"));
    fireEvent.click(await button("刷新工单状态"));
    expect(await screen.findByText("当前：已取消 · 状态版本 2")).toBeDefined();
    expect(change.mock.calls[0][3].aborted).toBe(true);
    await act(async () =>
      finish({
        ticket: current.ref,
        appliedStatus: "in-progress",
        appliedStatusRevision: 2,
      }),
    );
    expect(screen.queryByText(/已确认原操作/)).toBeNull();
    await button("重试原状态操作");
  });
  it("revalidates on restored history and clears the old confirmation", async () => {
    const { load } = setup();
    fireEvent.click(await button("取消工单"));
    await button("确认取消工单");
    fireEvent(window, new PageTransitionEvent("pageshow", { persisted: true }));
    await button("取消工单");
    expect(load).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("button", { name: "确认取消工单" })).toBeNull();
  });
});
