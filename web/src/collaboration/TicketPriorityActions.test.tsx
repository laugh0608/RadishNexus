import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { AuthRequestError } from "../auth/api";
import { useTicketPriorityActions } from "./TicketPriorityActions";
import { type TicketPriorityClient } from "./ticket-priority-api";
const priority = {
  priority: "unset" as const,
  revision: 1,
  statusRevision: 1,
  canSetPriority: true,
};
function client(): TicketPriorityClient {
  return {
    read: vi.fn().mockResolvedValue(priority),
    set: vi.fn().mockResolvedValue({ priority: "high", revision: 2 }),
  };
}
const fixed = {
  workspaceID: "wrk_one",
  ticketID: "tkt_one",
  enabled: true,
  ready: true,
  versionKey: "first",
  unavailable: false,
  onRefresh: vi.fn(),
  onUnavailable: vi.fn(),
  onSessionExpired: vi.fn(),
  createOperationID: () => "priority-operation",
};
function Harness(
  props: Partial<typeof fixed> & { client: TicketPriorityClient },
) {
  return useTicketPriorityActions({ ...fixed, ...props });
}
function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { resolve, promise };
}
async function prepare() {
  fireEvent.change(
    await screen.findByRole("combobox", { name: "选择优先级" }),
    { target: { value: "high" } },
  );
  fireEvent.click(screen.getByRole("button", { name: "设置优先级" }));
}
describe("Ticket priority actions", () => {
  it("requires confirmation and keeps the original operation across unknown outcome and reauthorization", async () => {
    const api = client();
    vi.mocked(api.set).mockRejectedValueOnce(new AuthRequestError("unknown"));
    const refresh = vi.fn();
    const view = render(<Harness client={api} onRefresh={refresh} />);
    await prepare();
    expect(api.set).not.toHaveBeenCalled();
    expect(screen.getByText(/从“未设置”改为“高”/)).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "确认优先级" }));
    await screen.findByText(/操作结果尚未确认/);
    const original = vi.mocked(api.set).mock.calls[0]![2];
    expect(original).toEqual({
      client_operation_id: "priority-operation",
      base_priority_revision: 1,
      base_status_revision: 1,
      priority: "high",
      confirmed: true,
    });
    view.rerender(
      <Harness client={api} ready={false} versionKey="" onRefresh={refresh} />,
    );
    vi.mocked(api.read).mockResolvedValue({
      ...priority,
      priority: "low",
      revision: 4,
      statusRevision: 3,
      canSetPriority: false,
    });
    view.rerender(
      <Harness client={api} versionKey="later" onRefresh={refresh} />,
    );
    await screen.findByText("当前优先级：低");
    fireEvent.click(screen.getByRole("button", { name: "重试原优先级操作" }));
    await waitFor(() => expect(api.set).toHaveBeenCalledTimes(2));
    expect(vi.mocked(api.set).mock.calls[1]![2]).toEqual(original);
    await waitFor(() => expect(refresh).toHaveBeenCalledOnce());
  });
  it("clears conflicting confirmation and rereads without retrying automatically", async () => {
    const api = client();
    vi.mocked(api.set).mockRejectedValue(
      new AuthRequestError("conflict", { status: 409 }),
    );
    const refresh = vi.fn();
    render(<Harness client={api} onRefresh={refresh} />);
    await prepare();
    fireEvent.click(screen.getByRole("button", { name: "确认优先级" }));
    await screen.findByText(/不会覆盖他人的修改/);
    expect(refresh).toHaveBeenCalledOnce();
    expect(
      screen.queryByRole("button", { name: "重试原优先级操作" }),
    ).toBeNull();
    expect(api.set).toHaveBeenCalledTimes(1);
  });
  it.each([401, 404])(
    "revokes state after %s without retaining a pending write",
    async (status) => {
      const api = client();
      vi.mocked(api.set).mockRejectedValue(
        new AuthRequestError("revoked", { status }),
      );
      const unavailable = vi.fn(),
        expired = vi.fn();
      const view = render(
        <Harness
          client={api}
          onUnavailable={unavailable}
          onSessionExpired={expired}
        />,
      );
      await prepare();
      fireEvent.click(screen.getByRole("button", { name: "确认优先级" }));
      await waitFor(() => expect(unavailable).toHaveBeenCalledOnce());
      expect(expired).toHaveBeenCalledTimes(status === 401 ? 1 : 0);
      view.rerender(
        <Harness
          client={api}
          unavailable
          onUnavailable={unavailable}
          onSessionExpired={expired}
        />,
      );
      expect(screen.queryByText("当前优先级：未设置")).toBeNull();
      expect(
        screen.queryByRole("button", { name: "重试原优先级操作" }),
      ).toBeNull();
    },
  );
  it("shows read-only priority and disables a same-value operation", async () => {
    const api = client();
    const view = render(<Harness client={api} />);
    const button = await screen.findByRole("button", { name: "设置优先级" });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    vi.mocked(api.read).mockResolvedValue({
      ...priority,
      canSetPriority: false,
    });
    view.rerender(<Harness client={api} versionKey="readonly" />);
    await screen.findByText(/当前不能变更优先级/);
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(api.set).not.toHaveBeenCalled();
  });
  it("ignores late read and write results after Workspace switch", async () => {
    const api = client();
    const write = deferred<{ priority: "high"; revision: number }>();
    vi.mocked(api.set).mockReturnValue(write.promise);
    const refresh = vi.fn();
    const view = render(<Harness client={api} onRefresh={refresh} />);
    await prepare();
    fireEvent.click(screen.getByRole("button", { name: "确认优先级" }));
    const signal = vi.mocked(api.set).mock.calls[0]![3];
    view.rerender(
      <Harness
        client={api}
        workspaceID="wrk_two"
        ticketID="tkt_two"
        onRefresh={refresh}
      />,
    );
    await screen.findByRole("combobox");
    expect(signal?.aborted).toBe(true);
    await act(async () => write.resolve({ priority: "high", revision: 2 }));
    expect(refresh).not.toHaveBeenCalled();
    expect(screen.queryByText(/操作已确认/)).toBeNull();
    const read = deferred<typeof priority>();
    vi.mocked(api.read).mockReturnValueOnce(read.promise);
    view.rerender(
      <Harness
        client={api}
        workspaceID="wrk_three"
        ticketID="tkt_three"
        onRefresh={refresh}
      />,
    );
    view.rerender(<Harness client={api} unavailable onRefresh={refresh} />);
    await act(async () => read.resolve(priority));
    expect(screen.queryByRole("combobox")).toBeNull();
  });
  it("clears prepared confirmation during reauthorization even when versions stay equal", async () => {
    const api = client();
    const view = render(<Harness client={api} />);
    await prepare();
    view.rerender(<Harness client={api} ready={false} versionKey="" />);
    expect(screen.queryByRole("button", { name: "确认优先级" })).toBeNull();
    view.rerender(<Harness client={api} />);
    await screen.findByRole("combobox");
    expect(screen.queryByRole("button", { name: "确认优先级" })).toBeNull();
  });
  it("allows a failed read to be retried without inventing a default priority", async () => {
    const api = client();
    vi.mocked(api.read).mockRejectedValueOnce(new AuthRequestError("down"));
    render(<Harness client={api} />);
    await screen.findByText("优先级读取失败，请重试。");
    expect(screen.queryByText("当前优先级：未设置")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "刷新优先级" }));
    await screen.findByText("当前优先级：未设置");
  });
});
