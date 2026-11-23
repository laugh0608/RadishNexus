import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ChannelThreads } from "./ChannelThreads";
import { listChannelThreads } from "./channel-threads-api";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
const item = (id: string) => ({
  ref: { type: "thread", id },
  title: "讨论 " + id,
});
const first = Array.from({ length: 25 }, (_, i) =>
  item("thr_" + String(i).padStart(2, "0")),
);
function response(items: unknown[], next: string | null = null) {
  return new Response(JSON.stringify({ data: { items, next_cursor: next } }), {
    headers: { "Content-Type": "application/json" },
  });
}
function openList() {
  const props = {
    workspaceID: "wrk_main",
    channelID: "chn_main",
    onUnavailable: vi.fn(),
    onSessionExpired: vi.fn(),
  };
  render(<ChannelThreads {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "频道讨论" }));
  return props;
}
it("discards old rows while paging and rereads the previous page after revocation", async () => {
  let resolve!: (value: Response) => void;
  const next = new Promise<Response>((r) => {
    resolve = r;
  });
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(response(first, "opaque"))
    .mockReturnValueOnce(next)
    .mockResolvedValueOnce(response([]));
  vi.stubGlobal("fetch", fetch);
  openList();
  const link = await screen.findByRole("link", { name: "讨论 thr_00" });
  expect(link.getAttribute("href")).toBe("/workspaces/wrk_main/threads/thr_00");
  fireEvent.click(screen.getByRole("button", { name: "下一页" }));
  expect(screen.queryByRole("link")).toBeNull();
  expect(fetch.mock.calls[1]?.[0]).toBe(
    "/api/v1/workspaces/wrk_main/channels/chn_main/threads?limit=25&after=opaque",
  );
  await act(async () => resolve(response([item("thr_last")])));
  expect(
    await screen.findByRole("link", { name: "讨论 thr_last" }),
  ).toBeDefined();
  fireEvent.click(screen.getByRole("button", { name: "上一页" }));
  expect(await screen.findByText("本页没有可读讨论。")).toBeDefined();
  expect(fetch.mock.calls[2]?.[0]).toBe(fetch.mock.calls[0]?.[0]);
  expect(screen.queryByRole("link")).toBeNull();
});
it("ignores a late response from a closed list after reopening it", async () => {
  let resolve!: (r: Response) => void;
  const fetch = vi
    .fn()
    .mockReturnValueOnce(
      new Promise<Response>((r) => {
        resolve = r;
      }),
    )
    .mockResolvedValueOnce(response([]));
  vi.stubGlobal("fetch", fetch);
  openList();
  const toggle = screen.getByRole("button", { name: "频道讨论" });
  fireEvent.click(toggle);
  expect(fetch.mock.calls[0]?.[1].signal.aborted).toBe(true);
  fireEvent.click(toggle);
  await screen.findByText("本页没有可读讨论。");
  await act(async () => resolve(response(first)));
  expect(screen.queryByRole("link")).toBeNull();
});
it.each([401, 404, 503])(
  "drops previous results on a %i during paging",
  async (status) => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(response(first, "opaque"))
        .mockResolvedValueOnce(new Response(null, { status })),
    );
    const props = openList();
    await screen.findByRole("link", { name: "讨论 thr_24" });
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    if (status === 401)
      await waitFor(() =>
        expect(props.onSessionExpired).toHaveBeenCalledOnce(),
      );
    else if (status === 404)
      await waitFor(() => expect(props.onUnavailable).toHaveBeenCalledOnce());
    else await screen.findByRole("alert");
    expect(screen.queryByRole("link")).toBeNull();
  },
);
it("rejects private metadata, non-Thread references and unordered results", async () => {
  for (const rows of [
    [{ ...item("thr_a"), source: "msg_private" }],
    [{ ...item("thr_a"), ref: { type: "ticket", id: "tkt_a" } }],
    [item("thr_b"), item("thr_a")],
  ]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(rows)));
    await expect(listChannelThreads("wrk_main", "chn_main")).rejects.toThrow();
  }
});
