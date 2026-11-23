import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AuthRequestError } from "../auth/api";
import * as threads from "../channel/channel-threads-api";
import { browserDiscoveryClient } from "../workspace/api";
import { ProjectThreadPicker } from "./ProjectThreadPicker";

const firstChannel = {
  id: "chn_one",
  title: "频道一",
  status: "active",
} as const;
const otherChannel = {
  id: "chn_two",
  title: "频道二",
  status: "active",
} as const;
function setup() {
  const props = {
    workspaceID: "wrk_one",
    projectID: "prj_one",
    purpose: "review" as const,
    disabledThreadIDs: ["thr_original"],
    onSelect: vi.fn(),
    onUnavailable: vi.fn(),
    onSessionExpired: vi.fn(),
  };
  render(<ProjectThreadPicker {...props} />);
  return props;
}
const button = (name: string) => screen.findByRole("button", { name });
afterEach(() => vi.restoreAllMocks());

it("scopes both cursors to the current list and discards a late page after changing channels", async () => {
  const channels = vi
    .spyOn(browserDiscoveryClient, "channels")
    .mockResolvedValueOnce({
      items: [firstChannel],
      nextCursor: "channels_next",
    })
    .mockResolvedValueOnce({ items: [otherChannel], nextCursor: null })
    .mockResolvedValue({ items: [firstChannel], nextCursor: null });
  let finish!: (
    value: Awaited<ReturnType<typeof threads.listChannelThreads>>,
  ) => void;
  const read = vi
    .spyOn(threads, "listChannelThreads")
    .mockResolvedValueOnce({
      items: [{ id: "thr_original", title: "既有来源" }],
      nextCursor: "threads_next",
    })
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    )
    .mockResolvedValue({
      items: [{ id: "thr_visible", title: "可选讨论" }],
      nextCursor: null,
    });
  const props = setup();
  fireEvent.click(await button("下一页"));
  fireEvent.click(await button("频道二"));
  expect(await button("既有来源（原讨论）")).toHaveProperty("disabled", true);
  expect(channels.mock.calls[1]?.slice(0, 3)).toEqual([
    "wrk_one",
    "prj_one",
    "channels_next",
  ]);
  expect(read.mock.calls[0]?.slice(0, 3)).toEqual([
    "wrk_one",
    "chn_two",
    undefined,
  ]);
  fireEvent.click(await button("下一页"));
  const oldPage = read.mock.lastCall!;
  expect(oldPage.slice(0, 3)).toEqual(["wrk_one", "chn_two", "threads_next"]);
  fireEvent.click(await button("更换频道"));
  fireEvent.click(await button("频道一"));
  expect(oldPage[3]?.aborted).toBe(true);
  await act(async () =>
    finish({
      items: [{ id: "thr_late", title: "旧频道标题" }],
      nextCursor: null,
    }),
  );
  expect(screen.queryByText("旧频道标题")).toBeNull();
  expect(read.mock.lastCall?.slice(0, 3)).toEqual([
    "wrk_one",
    "chn_one",
    undefined,
  ]);
  fireEvent.click(await button("可选讨论"));
  expect(props.onSelect).toHaveBeenCalledExactlyOnceWith({
    id: "thr_visible",
    title: "可选讨论",
  });
});

it("rereads the channel directory on channel loss, and propagates project loss", async () => {
  const denied = new AuthRequestError("不可读取", { status: 404 });
  const channels = vi
    .spyOn(browserDiscoveryClient, "channels")
    .mockResolvedValueOnce({ items: [firstChannel], nextCursor: null })
    .mockRejectedValue(denied);
  vi.spyOn(threads, "listChannelThreads").mockRejectedValue(denied);
  const props = setup();
  fireEvent.click(await button("频道一"));
  await waitFor(() => expect(props.onUnavailable).toHaveBeenCalledOnce());
  expect(channels).toHaveBeenCalledTimes(2);
  expect(screen.queryByText("频道一")).toBeNull();
  expect(props.onSelect).not.toHaveBeenCalled();
});
