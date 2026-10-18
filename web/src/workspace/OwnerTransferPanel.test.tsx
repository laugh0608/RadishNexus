import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AuthRequestError } from "../auth/api";
import { OwnerTransferPanel } from "./OwnerTransferPanel";
const operation = "12345678-1234-4234-8234-123456789abc";
const member = { id: "usr_member", displayName: "Same Name", revision: "1" };
const owner = {
  userID: "usr_owner",
  role: "owner" as const,
  revision: "1",
  canTransfer: true,
  recent: true,
};
function client() {
  return {
    ownership: vi.fn().mockResolvedValue(owner),
    candidates: vi.fn().mockResolvedValue({
      items: [member, { ...member, id: "usr_another" }],
      nextCursor: null,
    }),
    transfer: vi
      .fn()
      .mockRejectedValue(new AuthRequestError("unconfirmed", { status: 503 })),
    status: vi
      .fn()
      .mockResolvedValue({ result: "not_observed", operationID: operation }),
  };
}
function props(c = client()) {
  return {
    workspaceID: "wrk_main",
    userID: "usr_owner",
    onSessionExpired: vi.fn(),
    onChanged: vi.fn().mockResolvedValue(undefined),
    client: c,
  };
}
async function open() {
  fireEvent.click(screen.getByRole("button", { name: "管理权交接与结果查询" }));
  await screen.findByLabelText("接收管理权的成员");
  await waitFor(() =>
    expect(
      screen.getByRole("option", { name: "Same Name · usr_member" }),
    ).toBeTruthy(),
  );
}
async function submit() {
  fireEvent.change(screen.getByLabelText("接收管理权的成员"), {
    target: { value: member.id },
  });
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "确认交接管理权" }));
  await screen.findByText(/交接结果尚未确认/);
}
afterEach(() => vi.restoreAllMocks());
it("requires explicit confirmation, distinguishes names by ID and retains the exact uncertain request across focus and close", async () => {
  const c = client();
  render(<OwnerTransferPanel {...props(c)} />);
  await open();
  expect(
    (
      screen.getByRole("button", {
        name: "确认交接管理权",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  await submit();
  await waitFor(() =>
    expect(document.activeElement?.textContent).toContain("交接结果尚未确认"),
  );
  const original = c.transfer.mock.calls[0]![2];
  fireEvent.focus(window);
  await waitFor(() => expect(c.ownership).toHaveBeenCalledTimes(2));
  fireEvent.click(screen.getByRole("button", { name: "管理权交接与结果查询" }));
  fireEvent.click(screen.getByRole("button", { name: "管理权交接与结果查询" }));
  fireEvent.click(screen.getByRole("button", { name: "用原请求重试" }));
  await waitFor(() => expect(c.transfer).toHaveBeenCalledTimes(2));
  expect(c.transfer.mock.calls[1]![2]).toEqual(original);
  expect(
    (
      screen.getByRole("button", {
        name: "确认交接管理权",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(window.localStorage.length).toBe(0);
  expect(window.sessionStorage.length).toBe(0);
});
it("does not interpret not_observed as rollback and can query copied history after remount", async () => {
  const c = client();
  const p = props(c);
  const mounted = render(<OwnerTransferPanel {...p} />);
  await open();
  await submit();
  const id = c.transfer.mock.calls[0]![2].client_operation_id;
  c.status.mockResolvedValue({ result: "not_observed", operationID: id });
  fireEvent.click(screen.getByRole("button", { name: "查询结果" }));
  await screen.findByText(/不能据此判断已回滚/);
  expect(screen.getByRole("button", { name: "用原请求重试" })).toBeTruthy();
  mounted.unmount();
  c.ownership.mockResolvedValue({
    ...owner,
    role: "member",
    canTransfer: false,
    revision: "2",
  });
  c.status.mockResolvedValue({
    result: "applied",
    operationID: id,
    actorID: "usr_owner",
    targetID: "usr_member",
    auditID: "cfa_transfer",
    occurredAt: "2026-10-18T01:00:00Z",
    actorRevision: "2",
    targetRevision: "2",
  });
  render(<OwnerTransferPanel {...p} />);
  fireEvent.click(screen.getByRole("button", { name: "管理权交接与结果查询" }));
  await screen.findByText(/你当前是普通成员/);
  fireEvent.change(screen.getByLabelText("交接操作 ID"), {
    target: { value: id },
  });
  fireEvent.click(screen.getByRole("button", { name: "查询结果" }));
  await screen.findByText(/历史交接已完成/);
  expect(screen.queryByRole("button", { name: "用原请求重试" })).toBeNull();
  expect(p.onChanged).toHaveBeenCalled();
});
it.each(["not_observed", "network failure"])(
  "blocks new transfers when a pasted operation remains unresolved: %s",
  async (outcome) => {
    const c = client();
    if (outcome === "network failure")
      c.status.mockRejectedValueOnce(new AuthRequestError("查询失败"));
    render(<OwnerTransferPanel {...props(c)} />);
    await open();
    fireEvent.change(screen.getByLabelText("接收管理权的成员"), {
      target: { value: member.id },
    });
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.change(screen.getByLabelText("交接操作 ID"), {
      target: { value: operation },
    });
    fireEvent.click(screen.getByRole("button", { name: "查询结果" }));
    await screen.findByText(
      outcome === "not_observed" ? /不能据此判断已回滚/ : "查询失败",
    );
    const select = screen.getByLabelText("接收管理权的成员");
    expect(select.closest("fieldset")!.disabled).toBe(true);
    fireEvent.submit(select.closest("form")!);
    expect(c.transfer).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "用原请求重试" })).toBeNull();
    fireEvent.change(screen.getByLabelText("交接操作 ID"), {
      target: { value: "" },
    });
    fireEvent.focus(window);
    await waitFor(() => expect(c.ownership).toHaveBeenCalledTimes(2));
    expect(screen.getByText(/以下操作尚未确认/).textContent).toContain(
      operation,
    );
    expect(
      screen.getByLabelText("接收管理权的成员").closest("fieldset")!.disabled,
    ).toBe(true);
    const applied = {
      result: "applied",
      operationID: "22345678-1234-4234-8234-123456789abc",
      actorID: "usr_owner",
      targetID: "usr_member",
      auditID: "cfa_transfer",
      occurredAt: "2026-10-18T01:00:00Z",
      actorRevision: "2",
      targetRevision: "2",
    };
    c.status.mockResolvedValue(applied);
    fireEvent.change(screen.getByLabelText("交接操作 ID"), {
      target: { value: applied.operationID },
    });
    fireEvent.click(screen.getByRole("button", { name: "查询结果" }));
    await screen.findByText(/历史交接已完成/);
    expect(screen.getByText(/以下操作尚未确认/).textContent).toContain(
      operation,
    );
    c.status.mockResolvedValue({ ...applied, operationID: operation });
    fireEvent.change(screen.getByLabelText("交接操作 ID"), {
      target: { value: operation },
    });
    fireEvent.click(screen.getByRole("button", { name: "查询结果" }));
    await waitFor(() =>
      expect(screen.queryByText(/以下操作尚未确认/)).toBeNull(),
    );
    await waitFor(() =>
      expect(
        screen.getByLabelText("接收管理权的成员").closest("fieldset")!.disabled,
      ).toBe(false),
    );
    expect(c.transfer).not.toHaveBeenCalled();
  },
);

it("clears stale candidates and confirmation on pageshow; conflict requires fresh selection", async () => {
  const c = client();
  c.transfer.mockRejectedValue(
    new AuthRequestError("页面版本已过期", { status: 409 }),
  );
  render(<OwnerTransferPanel {...props(c)} />);
  await open();
  fireEvent.change(screen.getByLabelText("接收管理权的成员"), {
    target: { value: member.id },
  });
  fireEvent.click(screen.getByRole("checkbox"));
  await act(async () => window.dispatchEvent(new Event("pageshow")));
  await waitFor(() => expect(c.ownership).toHaveBeenCalledTimes(2));
  await waitFor(() =>
    expect((screen.getByRole("checkbox") as HTMLInputElement).checked).toBe(
      false,
    ),
  );
  fireEvent.change(screen.getByLabelText("接收管理权的成员"), {
    target: { value: member.id },
  });
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "确认交接管理权" }));
  await screen.findByText("页面版本已过期");
  expect(screen.queryByRole("button", { name: "用原请求重试" })).toBeNull();
  expect(c.transfer).toHaveBeenCalledTimes(1);
});
it("shows reauthentication and clears management data after loss of owner role", async () => {
  const c = client();
  c.ownership.mockResolvedValue({ ...owner, recent: false });
  render(<OwnerTransferPanel {...props(c)} />);
  await open();
  await screen.findByText(/退出并重新登录/);
  expect(
    (
      screen.getByRole("button", {
        name: "确认交接管理权",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  c.ownership.mockResolvedValue({
    ...owner,
    role: "member",
    canTransfer: false,
    revision: "2",
  });
  fireEvent.focus(window);
  await screen.findByText(/你当前是普通成员/);
  expect(screen.queryByLabelText("接收管理权的成员")).toBeNull();
  expect(screen.queryByText("Same Name · usr_member")).toBeNull();
  expect(c.transfer).not.toHaveBeenCalled();
});
it("does not retain an operation across account-key remount and ignores late unmounted reads", async () => {
  const c = client();
  const p = props(c);
  const view = render(<OwnerTransferPanel key="usr_owner" {...p} />);
  await open();
  await submit();
  view.rerender(
    <OwnerTransferPanel key="usr_other" {...p} userID="usr_other" />,
  );
  fireEvent.click(screen.getByRole("button", { name: "管理权交接与结果查询" }));
  await screen.findByLabelText("交接操作 ID");
  expect((screen.getByLabelText("交接操作 ID") as HTMLInputElement).value).toBe(
    "",
  );
  expect(screen.queryByRole("button", { name: "用原请求重试" })).toBeNull();
});
