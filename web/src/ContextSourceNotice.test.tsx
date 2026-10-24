import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ContextSourceNotice } from "./ContextSourceNotice";

const props = {
  workspaceID: "wrk_main",
  entityType: "document",
  entityID: "doc_one",
  onSessionExpired: vi.fn(),
  onUnavailable: vi.fn(),
};
const response = (data: unknown, status = 200) =>
  new Response(JSON.stringify({ data }), { status });
afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});
it("distinguishes imported, native and malformed source without exposing private metadata", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(
      response({ imported: true, applied_at: "2026-10-24T00:00:00Z" }),
    )
    .mockResolvedValueOnce(response({ imported: false }))
    .mockResolvedValueOnce(
      response({ imported: false, package_id: "private" }),
    );
  vi.stubGlobal("fetch", fetch);
  const view = render(<ContextSourceNotice {...props} />);
  expect(await screen.findByLabelText("导入来源")).toBeTruthy();
  expect(fetch.mock.calls[0][1]).toMatchObject({
    credentials: "same-origin",
    cache: "no-store",
  });
  view.rerender(<ContextSourceNotice {...props} entityID="doc_two" />);
  await waitFor(() =>
    expect(screen.queryByText("来源信息尚未确认。")).toBeNull(),
  );
  expect(screen.queryByLabelText("导入来源")).toBeNull();
  view.rerender(<ContextSourceNotice {...props} entityID="doc_three" />);
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(3));
  expect(screen.getByText("来源信息尚未确认。")).toBeTruthy();
  expect(screen.queryByText("private")).toBeNull();
});
it("does not apply a stale source result after navigation", async () => {
  let complete!: (value: Response) => void;
  const fetch = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          complete = resolve;
        }),
    )
    .mockResolvedValueOnce(response({ imported: false }));
  vi.stubGlobal("fetch", fetch);
  const view = render(<ContextSourceNotice {...props} />);
  view.rerender(<ContextSourceNotice {...props} entityID="doc_two" />);
  await waitFor(() =>
    expect(screen.queryByText("来源信息尚未确认。")).toBeNull(),
  );
  await act(async () =>
    complete(response({ imported: true, applied_at: "2026-10-24T00:00:00Z" })),
  );
  expect(screen.queryByLabelText("导入来源")).toBeNull();
  expect(fetch.mock.calls[0][1].signal.aborted).toBe(true);
});
it.each([401, 403, 404])(
  "propagates denied source status %s without treating it as native",
  async (status) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response({}, status)));
    render(<ContextSourceNotice {...props} />);
    await waitFor(() =>
      expect(
        status === 401 ? props.onSessionExpired : props.onUnavailable,
      ).toHaveBeenCalledOnce(),
    );
    expect(screen.queryByLabelText("导入来源")).toBeNull();
    expect(screen.getByText("来源信息尚未确认。")).toBeTruthy();
  },
);
