import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { AuthRequestError } from "../auth/api";
import { useTicketAssignmentActions } from "./TicketAssignmentActions";
import { type TicketAssignmentClient } from "./ticket-assignee-api";

const assignment = {
  assignee: null,
  revision: 1,
  statusRevision: 1,
  canAssign: true,
  projectRevision: "7",
};
const candidates = {
  items: [
    {
      id: "usr_me",
      displayName: "成员甲",
      lifecycleRevision: "9223372036854775807",
    },
  ],
  nextCursor: null,
  projectRevision: "7",
  selfID: "usr_me",
};
function client(): TicketAssignmentClient {
  return {
    read: vi.fn().mockResolvedValue(assignment),
    candidates: vi.fn().mockResolvedValue(candidates),
    set: vi.fn().mockResolvedValue({ revision: 2, assigneeID: "usr_me" }),
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
  createOperationID: () => "assignment-operation",
};
function Harness(
  props: Partial<typeof fixed> & { client: TicketAssignmentClient },
) {
  return useTicketAssignmentActions({ ...fixed, ...props });
}
function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { resolve, promise };
}

describe("Ticket assignment actions", () => {
  it("confirms self assignment and uses exact original operation after unknown result and refresh", async () => {
    const api = client();
    vi.mocked(api.set).mockRejectedValueOnce(new AuthRequestError("unknown"));
    const onRefresh = vi.fn();
    const view = render(<Harness client={api} onRefresh={onRefresh} />);
    fireEvent.click(await screen.findByRole("button", { name: "由我负责" }));
    expect(api.set).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "确认负责人" }));
    await screen.findByText(/操作结果尚未确认/);
    const first = vi.mocked(api.set).mock.calls[0]![2];
    expect(first).toMatchObject({
      client_operation_id: "assignment-operation",
      base_assignment_revision: 1,
      base_status_revision: 1,
      assignee_user_id: "usr_me",
      target_lifecycle_revision: "9223372036854775807",
      project_management_revision: "7",
      confirmed: true,
    });
    view.rerender(
      <Harness
        client={api}
        ready={false}
        versionKey=""
        onRefresh={onRefresh}
      />,
    );
    vi.mocked(api.read).mockResolvedValue({
      ...assignment,
      revision: 4,
      statusRevision: 2,
      canAssign: false,
    });
    view.rerender(
      <Harness client={api} versionKey="later" onRefresh={onRefresh} />,
    );
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "重试原负责人操作",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    fireEvent.click(screen.getByRole("button", { name: "重试原负责人操作" }));
    await waitFor(() => expect(api.set).toHaveBeenCalledTimes(2));
    expect(vi.mocked(api.set).mock.calls[1]![2]).toEqual(first);
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });
  it("clears old confirmation after conflict and permits choosing again", async () => {
    const api = client();
    vi.mocked(api.set).mockRejectedValueOnce(
      new AuthRequestError("conflict", { status: 409 }),
    );
    const refresh = vi.fn();
    const view = render(<Harness client={api} onRefresh={refresh} />);
    fireEvent.click(await screen.findByRole("button", { name: "由我负责" }));
    fireEvent.click(screen.getByRole("button", { name: "确认负责人" }));
    await screen.findByText(/不会覆盖他人的修改/);
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(
      screen.queryByRole("button", { name: "重试原负责人操作" }),
    ).toBeNull();
    view.rerender(
      <Harness client={api} versionKey="next" onRefresh={refresh} />,
    );
    expect(
      await screen.findByRole("button", { name: "由我负责" }),
    ).toBeDefined();
  });
  it("shows historical responsibility, confirms clearing, and hides candidate data after losing access", async () => {
    const api = client();
    vi.mocked(api.read).mockResolvedValue({
      ...assignment,
      assignee: {
        id: "usr_history",
        displayName: "历史成员",
        eligible: false,
        imported: true,
      },
    });
    const view = render(<Harness client={api} />);
    await screen.findByText(/历史负责人，待重新分配/);
    fireEvent.click(screen.getByRole("button", { name: "清空负责人" }));
    fireEvent.click(screen.getByRole("button", { name: "确认负责人" }));
    await waitFor(() => expect(api.set).toHaveBeenCalled());
    expect(vi.mocked(api.set).mock.calls[0]![2]).toMatchObject({
      assignee_user_id: null,
      target_lifecycle_revision: "0",
    });
    view.rerender(<Harness client={api} unavailable={true} />);
    expect(screen.queryByText(/历史成员/)).toBeNull();
    expect(screen.queryByRole("combobox")).toBeNull();
  });
  it("does not accept a late next page after a scope refresh", async () => {
    const api = client();
    const first = { ...candidates, nextCursor: "more" };
    vi.mocked(api.candidates).mockResolvedValueOnce(first);
    const late = deferred<typeof candidates>();
    vi.mocked(api.candidates)
      .mockReturnValueOnce(late.promise)
      .mockResolvedValue({ ...candidates, items: [] });
    const view = render(<Harness client={api} />);
    fireEvent.click(
      await screen.findByRole("button", { name: "加载更多候选成员" }),
    );
    const signal = vi.mocked(api.candidates).mock.calls[1]![3];
    view.rerender(<Harness client={api} versionKey="refresh" />);
    await screen.findByText("当前没有可分配成员。");
    expect(signal?.aborted).toBe(true);
    await act(async () =>
      late.resolve({
        ...candidates,
        items: [
          { id: "usr_z", displayName: "旧页成员", lifecycleRevision: "1" },
        ],
      }),
    );
    expect(screen.queryByText(/旧页成员/)).toBeNull();
  });
  it("does not offer candidates on a closed or read-only Ticket", async () => {
    const api = client();
    vi.mocked(api.read).mockResolvedValue({ ...assignment, canAssign: false });
    render(<Harness client={api} />);
    await screen.findByText(/当前不能变更负责人/);
    expect(api.candidates).not.toHaveBeenCalled();
  });
});
