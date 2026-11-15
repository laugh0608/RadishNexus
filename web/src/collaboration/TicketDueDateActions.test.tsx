import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useTicketDueDateActions } from "./TicketDueDateActions";
import type { TicketDueDateClient } from "./ticket-due-date-api";

const initial = {
  dueDate: "2032-02-29",
  revision: 2,
  statusRevision: 1,
  canSetDueDate: true,
};
const onUnavailable = vi.fn();
const onSessionExpired = vi.fn();
function Harness({
  client,
  onRefresh,
  versionKey,
}: {
  client: TicketDueDateClient;
  onRefresh: () => void;
  versionKey: string;
}) {
  return useTicketDueDateActions({
    workspaceID: "wrk_one",
    ticketID: "tkt_one",
    enabled: true,
    ready: true,
    versionKey,
    unavailable: false,
    client,
    onRefresh,
    onUnavailable,
    onSessionExpired,
    createOperationID: () => "due-operation",
  });
}

it("confirms explicit clearing and retries the same operation after an uncertain response", async () => {
  const client = {
    read: vi.fn().mockResolvedValue(initial),
    set: vi
      .fn()
      .mockRejectedValueOnce(new TypeError("connection ended"))
      .mockResolvedValueOnce({ dueDate: null, revision: 3 }),
  };
  const onRefresh = vi.fn();
  const { rerender } = render(
    <Harness client={client} onRefresh={onRefresh} versionKey="first" />,
  );
  expect(await screen.findByText("当前截止日期：2032-02-29")).toBeDefined();
  fireEvent.click(screen.getByRole("button", { name: "清除截止日期" }));
  expect(client.set).not.toHaveBeenCalled();
  expect(screen.getByText(/从“2032-02-29”改为“未设置”/u)).toBeDefined();
  fireEvent.click(screen.getByRole("button", { name: "确认截止日期" }));
  await screen.findByText(/操作结果尚未确认/u);
  expect(client.set.mock.calls[0]?.[2]).toEqual({
    due_date: null,
    confirmed: true,
    client_operation_id: "due-operation",
    base_due_date_revision: 2,
    base_status_revision: 1,
  });

  // Another write can change the visible date and status before an exact retry.
  client.read.mockResolvedValue({
    ...initial,
    dueDate: "2040-06-15",
    revision: 4,
    statusRevision: 2,
    canSetDueDate: false,
  });
  rerender(
    <Harness client={client} onRefresh={onRefresh} versionKey="later" />,
  );
  await screen.findByText("当前截止日期：2040-06-15");
  fireEvent.click(screen.getByRole("button", { name: "重试原截止日期操作" }));
  await waitFor(() => expect(onRefresh).toHaveBeenCalledOnce());
  expect(client.set.mock.calls[1]?.[2]).toEqual(client.set.mock.calls[0]?.[2]);
  expect(screen.getByText("当前截止日期：2040-06-15")).toBeDefined();
});

it("prepares a changed date without converting through a time zone", async () => {
  const client = {
    read: vi.fn().mockResolvedValue(initial),
    set: vi.fn().mockResolvedValue({ dueDate: "0001-01-01", revision: 3 }),
  };
  render(<Harness client={client} onRefresh={vi.fn()} versionKey="first" />);
  const date = await screen.findByLabelText("选择截止日期");
  fireEvent.change(date, { target: { value: "0001-01-01" } });
  fireEvent.click(screen.getByRole("button", { name: "设置截止日期" }));
  expect(screen.getByText(/从“2032-02-29”改为“0001-01-01”/u)).toBeDefined();
  fireEvent.click(screen.getByRole("button", { name: "确认截止日期" }));
  await waitFor(() => expect(client.set).toHaveBeenCalledOnce());
  expect(client.set.mock.calls[0]?.[2]?.due_date).toBe("0001-01-01");
});
