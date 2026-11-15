import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AuthRequestError } from "../auth/api";
import type { BuildCandidate } from "../nexus-view/ci-run-trace-api";
import { ThreadDelivery } from "./ThreadDelivery";
import {
  threadDeliveryClient,
  type DeliveryTicket,
} from "./thread-delivery-api";

const ticket: DeliveryTicket = {
  id: "tkt_first",
  title: "First delivery",
  status: "open",
  origins: ["message-origin", "execution-discussion"],
};
const other = { ...ticket, id: "tkt_second", title: "Second delivery" };
const run: BuildCandidate = {
  id: "cir_first",
  status: "succeeded",
  completedAt: "2026-11-15T01:02:03Z",
  component: { id: "cmp_service", name: "Synthetic service" },
};
const page = <T,>(items: T[], nextCursor: string | null = null) => ({
  items,
  nextCursor,
});

function renderDelivery() {
  return render(
    <ThreadDelivery
      workspace="wrk_test"
      thread="thr_test"
      onUnavailable={vi.fn()}
      onSessionExpired={vi.fn()}
    />,
  );
}

afterEach(() => vi.restoreAllMocks());

it("pages each level independently and aborts a pending deployment when the Ticket page changes", async () => {
  const tickets = vi
    .spyOn(threadDeliveryClient, "tickets")
    .mockResolvedValueOnce(page([ticket], "ticket-next"))
    .mockResolvedValueOnce(page([other]));
  const builds = vi
    .spyOn(threadDeliveryClient, "runs")
    .mockResolvedValueOnce(page([{ ...run, id: "cir_earlier" }], "run-next"))
    .mockResolvedValueOnce(page([run]));
  type Deployments = Awaited<
    ReturnType<typeof threadDeliveryClient.deployments>
  >;
  let settle: (value: Deployments) => void = () => {};
  const pending = new Promise<Deployments>((resolve) => {
    settle = resolve;
  });
  const deployments = vi
    .spyOn(threadDeliveryClient, "deployments")
    .mockResolvedValueOnce(
      page(
        [
          {
            id: "dpl_first",
            status: "failed",
            completedAt: run.completedAt,
            environment: { id: "env_first", name: "Staging A" },
          },
        ],
        "deployment-next",
      ),
    )
    .mockReturnValueOnce(pending);
  renderDelivery();
  fireEvent.click(await screen.findByRole("button", { name: "展开构建" }));
  const runsPanel = await screen.findByRole("region", {
    name: /First delivery的构建/,
  });
  fireEvent.click(
    await within(runsPanel).findByRole("button", { name: "下一页" }),
  );
  await within(runsPanel).findByRole("link", { name: "cir_first" });
  expect(builds.mock.calls[1]!.slice(0, 4)).toEqual([
    "wrk_test",
    "thr_test",
    ticket.id,
    "run-next",
  ]);
  expect(tickets).toHaveBeenCalledTimes(1);
  expect(deployments).not.toHaveBeenCalled();
  fireEvent.click(within(runsPanel).getByRole("button", { name: "展开部署" }));
  const deploymentPanel = await within(runsPanel).findByRole("region", {
    name: "cir_first 的部署",
  });
  fireEvent.click(
    await within(deploymentPanel).findByRole("button", { name: "下一页" }),
  );
  await waitFor(() => expect(deployments).toHaveBeenCalledTimes(2));
  expect(deployments.mock.calls[1]!.slice(0, 5)).toEqual([
    "wrk_test",
    "thr_test",
    ticket.id,
    run.id,
    "deployment-next",
  ]);
  expect(builds).toHaveBeenCalledTimes(2);
  fireEvent.click(screen.getByRole("button", { name: "下一页" }));
  await screen.findByRole("link", { name: other.title });
  expect(tickets.mock.calls[1]![2]).toBe("ticket-next");
  expect(deployments.mock.calls[1]![5].aborted).toBe(true);
  await act(async () =>
    settle(
      page([
        {
          id: "dpl_stale",
          status: "succeeded",
          completedAt: run.completedAt,
          environment: { id: "env_stale", name: "Stale staging" },
        },
      ]),
    ),
  );
  expect(screen.queryByText("Stale staging")).toBeNull();
  expect(screen.queryByRole("region", { name: /的构建/ })).toBeNull();
});

it("rechecks the parent after the child loses its path", async () => {
  vi.spyOn(threadDeliveryClient, "tickets").mockResolvedValue(page([ticket]));
  const builds = vi
    .spyOn(threadDeliveryClient, "runs")
    .mockResolvedValueOnce(page([run]))
    .mockResolvedValueOnce(page([]));
  vi.spyOn(threadDeliveryClient, "deployments").mockRejectedValueOnce(
    new AuthRequestError("已失去路径", { status: 404 }),
  );
  renderDelivery();
  fireEvent.click(await screen.findByRole("button", { name: "展开构建" }));
  fireEvent.click(await screen.findByRole("button", { name: "展开部署" }));
  await screen.findByText("该工单暂无可读的构建关联。");
  expect(builds).toHaveBeenCalledTimes(2);
  expect(builds.mock.calls[1]![3]).toBeUndefined();
  expect(screen.queryByRole("link", { name: "cir_first" })).toBeNull();
  expect(screen.getByRole("link", { name: ticket.title })).toBeTruthy();
});
