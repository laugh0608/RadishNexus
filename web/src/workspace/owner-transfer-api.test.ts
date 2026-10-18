import { afterEach, expect, it, vi } from "vitest";
import { ownerTransferClient as api } from "./owner-transfer-api";
const operation = "12345678-1234-4234-8234-123456789abc";
const result = {
  result: "applied",
  workspace_id: "wrk_main",
  client_operation_id: operation,
  actor_user_id: "usr_owner",
  target_user_id: "usr_member",
  audit_id: "cfa_handoff",
  occurred_at: "2026-10-18T01:00:00Z",
  actor_role_revision: "9007199254740994",
  target_role_revision: "2",
};
const response = (data: unknown) =>
  new Response(JSON.stringify({ data }), {
    headers: { "Content-Type": "application/json" },
  });
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
it("keeps bigint revisions exact and sends the original operation with CSRF", async () => {
  const csrf = "c".repeat(43);
  vi.spyOn(document, "cookie", "get").mockReturnValue(
    `__Host-radishnexus-csrf=${csrf}`,
  );
  const fetch = vi.fn().mockResolvedValue(response(result));
  vi.stubGlobal("fetch", fetch);
  const body = {
    client_operation_id: operation,
    target_user_id: "usr_member",
    expected_actor_role_revision: "9007199254740993",
    expected_target_role_revision: "1",
    confirmed: true as const,
  };
  expect(await api.transfer("wrk_main", "usr_owner", body)).toMatchObject({
    actorRevision: "9007199254740994",
    operationID: operation,
  });
  expect(fetch.mock.calls[0]).toEqual([
    "/api/v1/workspaces/wrk_main/owner-transfers",
    expect.objectContaining({
      method: "POST",
      body: JSON.stringify(body),
      credentials: "same-origin",
      cache: "no-store",
      headers: expect.objectContaining({ "X-CSRF-Token": csrf }),
    }),
  ]);
});
it("rejects projected identity drift, ambiguous versions and unexpected private fields", async () => {
  for (const bad of [
    { ...result, actor_user_id: "usr_other" },
    { ...result, workspace_id: "wrk_other" },
    { ...result, client_operation_id: "other" },
    { ...result, target_user_id: "usr_owner" },
    { ...result, actor_role_revision: 9007199254740994 },
    { ...result, actor_role_revision: "01" },
    { ...result, actor_role_revision: "9223372036854775808" },
    { ...result, email: "hidden@example.test" },
  ]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(bad)));
    await expect(
      api.status("wrk_main", "usr_owner", operation),
    ).rejects.toThrow("契约");
  }
});
it("requires coherent current role and filters candidate projection metadata", async () => {
  const good = {
    self: { id: "usr_owner", role: "owner", role_revision: "1" },
    can_transfer: true,
    recent_authentication: false,
  };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(good)));
  expect(await api.ownership("wrk_main", "usr_owner")).toMatchObject({
    recent: false,
    canTransfer: true,
  });
  for (const bad of [
    { ...good, can_transfer: false },
    { ...good, self: { ...good.self, id: "usr_other" } },
    { ...good, self: { ...good.self, role_revision: "0" } },
  ]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(bad)));
    await expect(api.ownership("wrk_main", "usr_owner")).rejects.toThrow(
      "契约",
    );
  }
  for (const items of [
    [{ id: "usr_owner", display_name: "Owner", role_revision: "1" }],
    [
      {
        id: "usr_a",
        display_name: "Same",
        role_revision: "1",
        email: "hidden@example.test",
      },
    ],
    [
      { id: "usr_b", display_name: "Same", role_revision: "1" },
      { id: "usr_a", display_name: "Same", role_revision: "1" },
    ],
  ]) {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(response({ items, next_cursor: null })),
    );
    await expect(api.candidates("wrk_main", "usr_owner")).rejects.toThrow(
      "契约",
    );
  }
});
it("accepts not_observed only as a minimal historical query result", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      response({
        result: "not_observed",
        workspace_id: "wrk_main",
        client_operation_id: operation,
      }),
    ),
  );
  expect(await api.status("wrk_main", "usr_owner", operation)).toEqual({
    result: "not_observed",
    operationID: operation,
  });
});
