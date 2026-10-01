import { afterEach, describe, expect, it, vi } from "vitest";
import { browserDiscoveryClient } from "./api";

const item = {
  ref: { type: "project", id: "prj_main" },
  title: "Project",
  status: "active",
};
function response(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
afterEach(() => vi.unstubAllGlobals());

describe("discovery API contract", () => {
  it("uses same-origin no-store requests and canonical Channel scope", async () => {
    const fetch = vi.fn().mockResolvedValue(
      response({
        data: {
          items: [{ ...item, ref: { type: "channel", id: "chn_main" } }],
          next_cursor: null,
        },
      }),
    );
    vi.stubGlobal("fetch", fetch);
    const controller = new AbortController();
    expect(
      await browserDiscoveryClient.channels(
        "wrk_main",
        "prj_main",
        "opaque_cursor",
        controller.signal,
      ),
    ).toEqual({
      items: [{ id: "chn_main", title: "Project", status: "active" }],
      nextCursor: null,
    });
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/workspaces/wrk_main/projects/prj_main/channels?limit=25&after=opaque_cursor",
      expect.objectContaining({
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
        signal: controller.signal,
      }),
    );
  });
  it("rejects mismatched references, hidden metadata, duplicates and invalid pagination", async () => {
    for (const data of [
      {
        items: [{ ...item, ref: { type: "channel", id: "chn_wrong" } }],
        next_cursor: null,
      },
      { items: [item, item], next_cursor: null },
      {
        items: [{ ...item, owner_email: "private@example.test" }],
        next_cursor: null,
      },
      { items: [{ ...item, title: "" }], next_cursor: null },
      { items: [{ ...item, status: "unknown" }], next_cursor: null },
      { items: [item], next_cursor: "opaque_cursor" },
      { items: [], next_cursor: "https://example.test/" },
      { items: [], next_cursor: null, hidden_count: 5 },
      { items: null, next_cursor: null },
    ]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response({ data })));
      await expect(
        browserDiscoveryClient.projects("wrk_main"),
      ).rejects.toMatchObject({ userMessage: expect.stringContaining("契约") });
    }
  });
  it("accepts full pages with an opaque next cursor", async () => {
    const items = Array.from({ length: 25 }, (_, index) => ({
      ...item,
      ref: { type: "project", id: `prj_${index}` },
    }));
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          response({ data: { items, next_cursor: "next_page" } }),
        ),
    );
    expect((await browserDiscoveryClient.projects("wrk_main")).nextCursor).toBe(
      "next_page",
    );
    await expect(
      browserDiscoveryClient.projects("wrk_main", "next_page"),
    ).rejects.toMatchObject({ userMessage: expect.stringContaining("契约") });
  });
  it("preserves authentication failures and refuses unsafe location input", async () => {
    const fetch = vi.fn().mockResolvedValue(response({}, 401));
    vi.stubGlobal("fetch", fetch);
    await expect(
      browserDiscoveryClient.projects("wrk_main"),
    ).rejects.toMatchObject({ status: 401 });
    fetch.mockClear();
    expect(() => browserDiscoveryClient.projects("wrk_../other")).toThrow();
    expect(() =>
      browserDiscoveryClient.channels("wrk_main", "prj_/bad"),
    ).toThrow();
    expect(fetch).not.toHaveBeenCalled();
  });
});
