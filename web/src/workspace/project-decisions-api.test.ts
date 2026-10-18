import { afterEach, describe, expect, it, vi } from "vitest";
import {
  projectDecisionsClient,
  projectDecisionsLocation,
  projectDecisionsPath,
} from "./project-decisions-api";

const decision = {
  ref: { type: "decision", id: "dec_one" },
  title: "Work",
  status: "proposed",
};
function response(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
afterEach(() => vi.unstubAllGlobals());

describe("Project Decision API", () => {
  it("uses a scoped, no-store GET and preserves only public fields", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(response({ items: [decision], next_cursor: null }));
    vi.stubGlobal("fetch", fetch);
    const signal = new AbortController().signal;
    expect(
      await projectDecisionsClient.decisions(
        "wrk_main",
        "prj_main",
        "opaque",
        signal,
      ),
    ).toEqual({
      items: [{ id: "dec_one", title: "Work", status: "proposed" }],
      nextCursor: null,
    });
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/workspaces/wrk_main/projects/prj_main/decisions?limit=25&after=opaque",
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
      { items: [{ ...decision, status: "unknown" }], next_cursor: null },
      {
        items: [{ ...decision, ref: { type: "ticket", id: "tkt_one" } }],
        next_cursor: null,
      },
      ...["", "   ", "\0", "\ud800", "\udfff"].map((title) => ({
        items: [{ ...decision, title }],
        next_cursor: null,
      })),
      ...["rejected", "superseded", "open", null, 1].map((status) => ({
        items: [{ ...decision, status }],
        next_cursor: null,
      })),
      {
        items: [{ ...decision, ref: { type: "decision", id: "dec_" } }],
        next_cursor: null,
      },
      {
        items: [
          {
            ...decision,
            ref: { type: "decision", id: "dec_one", workspace: "wrk_main" },
          },
        ],
        next_cursor: null,
      },
      { items: [{ ...decision, question: "duplicate" }], next_cursor: null },
      { items: [{ ...decision, evidence: [] }], next_cursor: null },
      { items: [{ ...decision, title: 4 }], next_cursor: null },
      { items: [{ ...decision, source_thread: "private" }], next_cursor: null },
      { items: [decision, decision], next_cursor: null },
      { items: [decision], next_cursor: "next" },
      { items: [], next_cursor: null, total: 8 },
      { items: [], next_cursor: "next" },
      { items: [decision], next_cursor: "" },
      { items: [decision], next_cursor: "next=" },
      { items: [decision], next_cursor: "a".repeat(1025) },
      {
        items: Array.from({ length: 26 }, (_, i) => ({
          ...decision,
          ref: { type: "decision", id: `dec_${String(i).padStart(2, "0")}` },
        })),
        next_cursor: null,
      },
      {
        items: [
          { ...decision, ref: { type: "decision", id: "dec_z" } },
          decision,
        ],
        next_cursor: null,
      },
    ]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(data)));
      await expect(
        projectDecisionsClient.decisions("wrk_main", "prj_main"),
      ).rejects.toThrow(/契约/);
    }
  });
  it("accepts 25 items and refuses repeated cursors and unsafe routes", async () => {
    const items = Array.from({ length: 25 }, (_, n) => ({
      ...decision,
      ref: { type: "decision", id: `dec_${String(n).padStart(2, "0")}` },
    }));
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(response({ items, next_cursor: "next" })),
    );
    expect(
      (await projectDecisionsClient.decisions("wrk_main", "prj_main"))
        .nextCursor,
    ).toBe("next");
    await expect(
      projectDecisionsClient.decisions("wrk_main", "prj_main", "next"),
    ).rejects.toThrow();
    expect(projectDecisionsPath("wrk_main", "prj_main")).toBe(
      "/workspaces/wrk_main/projects/prj_main/decisions",
    );
    expect(
      projectDecisionsLocation(
        "/workspaces/wrk_main/projects/prj_main/decisions/",
      ),
    ).toEqual({ workspaceID: "wrk_main", projectID: "prj_main" });
    for (const path of [
      "/workspaces/wrk_main/projects/prj_main/decisions/new",
      "/workspaces/wrk_main/projects/dec_wrong/decisions",
      "/workspaces/wrk_main/projects/prj_%2F/decisions",
    ])
      expect(projectDecisionsLocation(path)).toBeNull();
    expect(() => projectDecisionsPath("wrk_../", "prj_main")).toThrow();
  });
  it("keeps long questions and both statuses verbatim without loading evidence", async () => {
    const title =
      "  中文 😀 <script>alert(1)</script> **问题**\n" + "长问题".repeat(1000);
    const fetch = vi.fn().mockResolvedValue(
      response({
        items: [{ ...decision, title, status: "accepted" }],
        next_cursor: null,
      }),
    );
    vi.stubGlobal("fetch", fetch);
    expect(
      await projectDecisionsClient.decisions("wrk_main", "prj_main"),
    ).toEqual({
      items: [{ id: "dec_one", title, status: "accepted" }],
      nextCursor: null,
    });
    expect(fetch).toHaveBeenCalledOnce();
  });
  it.each(["", "next=", "a".repeat(1025), "with space"])(
    "rejects invalid cursor %s before fetching",
    async (after) => {
      const fetch = vi.fn();
      vi.stubGlobal("fetch", fetch);
      await expect(
        projectDecisionsClient.decisions("wrk_main", "prj_main", after),
      ).rejects.toThrow();
      expect(fetch).not.toHaveBeenCalled();
    },
  );
  it("preserves Session and unreadable failures", async () => {
    for (const status of [401, 404, 500]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response({}, status)));
      await expect(
        projectDecisionsClient.decisions("wrk_main", "prj_main"),
      ).rejects.toMatchObject({ status });
    }
  });
});
