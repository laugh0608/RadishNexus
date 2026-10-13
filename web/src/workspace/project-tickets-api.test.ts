import { afterEach, describe, expect, it, vi } from "vitest";
import {
  projectTicketsClient,
  projectTicketsLocation,
  projectTicketsPath,
} from "./project-tickets-api";

const ticket = {
  ref: { type: "ticket", id: "tkt_one" },
  title: "Work",
  status: "open",
};
function response(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
afterEach(() => vi.unstubAllGlobals());

describe("Project Ticket API", () => {
  it("uses a scoped, no-store GET and preserves only public fields", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(response({ items: [ticket], next_cursor: null }));
    vi.stubGlobal("fetch", fetch);
    const signal = new AbortController().signal;
    expect(
      await projectTicketsClient.tickets(
        "wrk_main",
        "prj_main",
        "opaque",
        signal,
      ),
    ).toEqual({
      items: [{ id: "tkt_one", title: "Work", status: "open" }],
      nextCursor: null,
    });
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/workspaces/wrk_main/projects/prj_main/tickets?limit=25&after=opaque",
      expect.objectContaining({
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
        signal,
      }),
    );
  });
  it("rejects unsupported states, private metadata and invalid pagination", async () => {
    for (const data of [
      { items: [{ ...ticket, status: "done" }], next_cursor: null },
      { items: [{ ...ticket, status: "in-progress" }], next_cursor: null },
      { items: [{ ...ticket, status: "canceled" }], next_cursor: null },
      {
        items: [{ ...ticket, ref: { type: "decision", id: "dec_one" } }],
        next_cursor: null,
      },
      { items: [{ ...ticket, title: "\0" }], next_cursor: null },
      { items: [{ ...ticket, source_thread: "private" }], next_cursor: null },
      { items: [ticket, ticket], next_cursor: null },
      { items: [ticket], next_cursor: "next" },
      { items: [], next_cursor: null, total: 8 },
      {
        items: [{ ...ticket, ref: { type: "ticket", id: "tkt_z" } }, ticket],
        next_cursor: null,
      },
    ]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(data)));
      await expect(
        projectTicketsClient.tickets("wrk_main", "prj_main"),
      ).rejects.toThrow(/契约/);
    }
  });
  it("accepts 25 items and refuses repeated cursors and unsafe routes", async () => {
    const items = Array.from({ length: 25 }, (_, n) => ({
      ...ticket,
      ref: { type: "ticket", id: `tkt_${String(n).padStart(2, "0")}` },
    }));
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(response({ items, next_cursor: "next" })),
    );
    expect(
      (await projectTicketsClient.tickets("wrk_main", "prj_main")).nextCursor,
    ).toBe("next");
    await expect(
      projectTicketsClient.tickets("wrk_main", "prj_main", "next"),
    ).rejects.toThrow();
    expect(projectTicketsPath("wrk_main", "prj_main")).toBe(
      "/workspaces/wrk_main/projects/prj_main/tickets",
    );
    expect(
      projectTicketsLocation("/workspaces/wrk_main/projects/prj_main/tickets/"),
    ).toEqual({ workspaceID: "wrk_main", projectID: "prj_main" });
    for (const path of [
      "/workspaces/wrk_main/projects/prj_main/tickets/new",
      "/workspaces/wrk_main/projects/tkt_wrong/tickets",
      "/workspaces/wrk_main/projects/prj_%2F/tickets",
    ])
      expect(projectTicketsLocation(path)).toBeNull();
    expect(() => projectTicketsPath("wrk_../", "prj_main")).toThrow();
  });
  it("preserves Session and unreadable failures", async () => {
    for (const status of [401, 404, 500]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response({}, status)));
      await expect(
        projectTicketsClient.tickets("wrk_main", "prj_main"),
      ).rejects.toMatchObject({ status });
    }
  });
});
