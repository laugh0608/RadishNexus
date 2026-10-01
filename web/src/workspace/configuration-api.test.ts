import { afterEach, expect, it, vi } from "vitest";
import { configurationClient as api } from "./configuration-api";

const response = (data: unknown) =>
  new Response(JSON.stringify({ data }), {
    headers: { "Content-Type": "application/json" },
  });
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
it("keeps configuration directories scoped and rejects unexpected private fields", async () => {
  const fetch = vi.fn().mockResolvedValue(
    response({
      items: [
        {
          id: "usr_member",
          display_name: "Member",
          email: "private@example.test",
        },
      ],
      next_cursor: null,
    }),
  );
  vi.stubGlobal("fetch", fetch);
  await expect(
    api.members("wrk_main", "workspace", "wrk_main"),
  ).rejects.toThrow("契约");
  expect(fetch).toHaveBeenCalledWith(
    "/api/v1/workspaces/wrk_main/members?limit=25",
    expect.objectContaining({ credentials: "same-origin", cache: "no-store" }),
  );
});
it("rejects mismatched configuration scopes and duplicate member entries", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      response({
        ref: { type: "project", id: "prj_wrong" },
        key: "project",
        name: "Project",
        status: "active",
        visibility: "restricted",
        capabilities: { manage: true },
      }),
    ),
  );
  await expect(api.read("wrk_main", "project", "prj_main")).rejects.toThrow(
    "契约",
  );
  const member = { id: "usr_member", display_name: "Member" };
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        response({ items: [member, member], next_cursor: null }),
      ),
  );
  await expect(
    api.members("wrk_main", "workspace", "wrk_main"),
  ).rejects.toThrow("契约");
});
