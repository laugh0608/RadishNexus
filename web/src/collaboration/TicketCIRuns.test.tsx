import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ciRunTraceClient,
  type TicketRun,
} from "../nexus-view/ci-run-trace-api";
import { TicketCIRuns } from "./TicketCIRuns";

const run: TicketRun = {
  id: "cir_first",
  linkID: "lnk_first",
  canUnlink: true,
  status: "failed",
  completedAt: "2026-11-15T01:02:03Z",
  component: { id: "cmp_service", name: "Synthetic service" },
};
const page = (items: TicketRun[], canLink = true) => ({
  items,
  nextCursor: null,
  canLink,
});

afterEach(() => vi.restoreAllMocks());

describe("Ticket build trace", () => {
  it("retries an uncertain unlink with the same operation and exact link", async () => {
    vi.spyOn(ciRunTraceClient, "ticketRuns")
      .mockResolvedValueOnce(page([run]))
      .mockResolvedValue(page([]));
    const write = vi
      .spyOn(ciRunTraceClient, "write")
      .mockRejectedValueOnce(new TypeError("unknown result"))
      .mockResolvedValueOnce(undefined);
    render(
      <TicketCIRuns
        workspace="wrk_test"
        ticket="tkt_first"
        onSessionExpired={vi.fn()}
        onUnavailable={vi.fn()}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "解除关联" }));
    fireEvent.click(screen.getByRole("button", { name: "确认解除构建" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "重试原操作 · 解除构建关联" }),
    );
    await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
    const [first, retry] = write.mock.calls;
    expect(first!.slice(0, 4)).toEqual([
      "wrk_test",
      "tkt_first",
      { link: "lnk_first" },
      expect.any(String),
    ]);
    expect(retry!.slice(0, 4)).toEqual(first!.slice(0, 4));
    expect(await screen.findByText("该工单暂无可读的构建关联。")).toBeTruthy();
  });

  it("drops late data after switching Tickets and hides actions for a read-only page", async () => {
    let resolveFirst: (value: ReturnType<typeof page>) => void = () => {};
    const stale = new Promise<ReturnType<typeof page>>((resolve) => {
      resolveFirst = resolve;
    });
    const load = vi
      .spyOn(ciRunTraceClient, "ticketRuns")
      .mockReturnValueOnce(stale)
      .mockResolvedValueOnce(
        page([{ ...run, id: "cir_second", canUnlink: false }], false),
      );
    const expired = vi.fn();
    const unavailable = vi.fn();
    const view = render(
      <TicketCIRuns
        key="first"
        workspace="wrk_test"
        ticket="tkt_first"
        onSessionExpired={expired}
        onUnavailable={unavailable}
      />,
    );
    await waitFor(() => expect(load).toHaveBeenCalledTimes(1));
    view.rerender(
      <TicketCIRuns
        key="second"
        workspace="wrk_test"
        ticket="tkt_second"
        onSessionExpired={expired}
        onUnavailable={unavailable}
      />,
    );
    expect(
      await screen.findByRole("link", { name: "cir_second" }),
    ).toBeTruthy();
    expect(load.mock.calls[0]![3].aborted).toBe(true);
    await act(async () => resolveFirst(page([run])));
    expect(screen.queryByRole("link", { name: "cir_first" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: /选择构建|解除关联/ }),
    ).toBeNull();
  });
});
