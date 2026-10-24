import { afterEach, expect, it, vi } from "vitest";
import {
  managementClient as api,
  managementOperations,
  rememberManagementOperation,
} from "./management-api";
const operation = "12345678-1234-4234-8234-123456789abc";
const scope = {
  workspace: "wrk_main",
  kind: "project" as const,
  id: "prj_main",
};
const proposal = {
  target_user_id: "usr_member",
  role: "admin",
  actor_after_role: "",
};
const plan = {
  ...proposal,
  scope_kind: "project",
  scope_id: "prj_main",
  action: "role",
  actor_id: "usr_owner",
  target_name: "Member",
  before: "viewer",
  after: "admin",
  actor_role: "admin",
  target_workspace_role: "member",
  target_status: "active",
  scope_revision: "9007199254740993",
  actor_lifecycle_revision: "1",
  target_lifecycle_revision: "1",
  actor_role_revision: "1",
  target_role_revision: "1",
  parent_project_revision: "9007199254740993",
  parent_channel_revision: "0",
  policy: "membership-management-v1",
  digest: "a".repeat(64),
};
const receipt = {
  result: "applied",
  workspace_id: "wrk_main",
  client_operation_id: operation,
  action: "management.project.role",
  scope_id: "prj_main",
  target_user_id: "usr_member",
  occurred_at: "2026-10-24T00:00:00Z",
};
const respond = (data: unknown) =>
  new Response(JSON.stringify({ data }), {
    headers: { "Content-Type": "application/json" },
  });
function install(data: unknown) {
  vi.spyOn(document, "cookie", "get").mockReturnValue(
    `__Host-radishnexus-csrf=${"c".repeat(43)}`,
  );
  const fetch = vi.fn().mockResolvedValue(respond(data));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  sessionStorage.clear();
});
it("binds preview to actor, scope, request and exact bigint revisions", async () => {
  install(plan);
  expect(
    (await api.preview(scope, "role", proposal, "usr_owner")).scope_revision,
  ).toBe("9007199254740993");
  for (const bad of [
    { ...plan, actor_id: "usr_other" },
    { ...plan, scope_id: "prj_other" },
    { ...plan, role: "viewer" },
    { ...plan, scope_revision: 2 },
    { ...plan, target_lifecycle_revision: "0" },
    { ...plan, scope_revision: "9223372036854775808" },
    { ...plan, private_count: 1 },
  ]) {
    install(bad);
    await expect(
      api.preview(scope, "role", proposal, "usr_owner"),
    ).rejects.toThrow();
  }
});
it("sends only the confirmed request and validates receipt coordinates", async () => {
  const fetch = install(receipt);
  const input = {
    ...proposal,
    client_operation_id: operation,
    plan_digest: plan.digest,
    confirmed: true as const,
  };
  expect(await api.apply(scope, "role", input)).toMatchObject({
    result: "applied",
    operationID: operation,
  });
  expect(fetch).toHaveBeenCalledWith(
    "/api/v1/workspaces/wrk_main/projects/prj_main/management/role",
    expect.objectContaining({
      method: "POST",
      body: JSON.stringify(input),
      credentials: "same-origin",
      cache: "no-store",
    }),
  );
  for (const bad of [
    { ...receipt, target_user_id: "usr_other" },
    { ...receipt, scope_id: "prj_other" },
    { ...receipt, action: "management.project.remove" },
  ]) {
    install(bad);
    await expect(api.apply(scope, "role", input)).rejects.toThrow();
  }
});
it("uses global-session result endpoint and rejects invitation or private result fields", async () => {
  const fetch = install({
    result: "not_observed",
    workspace_id: "wrk_main",
    client_operation_id: operation,
  });
  expect(await api.result("wrk_main", operation)).toEqual({
    result: "not_observed",
    operationID: operation,
  });
  expect(fetch.mock.calls[0][0]).toBe(
    `/api/v1/auth/management-results/wrk_main/${operation}`,
  );
  for (const bad of [
    { ...receipt, workspace_id: "wrk_other" },
    { ...receipt, client_operation_id: "different" },
    { ...receipt, private_ids: ["thr_hidden"] },
    {
      ...receipt,
      invitation: { token: "a".repeat(43), expires_at: receipt.occurred_at },
    },
  ]) {
    install(bad);
    await expect(api.result("wrk_main", operation)).rejects.toThrow();
  }
});
it("persists only actor-scoped bounded receipt coordinates", () => {
  rememberManagementOperation("usr_owner", "wrk_main", operation);
  rememberManagementOperation("usr_owner", "wrk_main", operation);
  expect(managementOperations("usr_owner")).toEqual([
    { workspace: "wrk_main", operation },
  ]);
  expect(managementOperations("usr_other")).toEqual([]);
  expect(
    JSON.parse(sessionStorage.getItem("radishnexus.management.usr_owner")!)[0],
  ).toEqual({ workspace: "wrk_main", operation });
});
