import { afterEach, expect, it, vi } from "vitest";
import { deliveryConfigurationClient as api } from "./delivery-configuration-api";
const response = (data: unknown) =>
  new Response(JSON.stringify({ data }), {
    headers: { "Content-Type": "application/json" },
  });
const environment = {
  ref: { type: "environment", id: "env_stage" },
  key: "stage",
  name: "Stage",
  classification: "staging",
  status: "active",
  owner_team_id: "tem_main",
};
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
it("discovers safe objects and rejects authorization provenance on the ordinary list", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(
      response({ items: [environment], next_cursor: null }),
    )
    .mockResolvedValueOnce(
      response({
        items: [{ ...environment, authorization_id: "dpa_hidden" }],
        next_cursor: null,
      }),
    );
  vi.stubGlobal("fetch", fetch);
  expect((await api.list("wrk_main", "environment")).items[0]).toMatchObject({
    id: "env_stage",
    canGrant: false,
  });
  expect(fetch).toHaveBeenCalledWith(
    "/api/v1/workspaces/wrk_main/environments?limit=25",
    expect.objectContaining({ credentials: "same-origin", cache: "no-store" }),
  );
  await expect(api.list("wrk_main", "environment")).rejects.toThrow("契约");
});
it("checks configuration capability invariants and requested identity", async () => {
  for (const row of [
    {
      ...environment,
      classification: "production",
      capabilities: {
        can_manage_authorizations: true,
        can_grant: true,
        can_revoke: true,
      },
    },
    {
      ...environment,
      status: "archived",
      capabilities: {
        can_manage_authorizations: true,
        can_grant: true,
        can_revoke: true,
      },
    },
    {
      ...environment,
      ref: { type: "environment", id: "env_wrong" },
      capabilities: {
        can_manage_authorizations: false,
        can_grant: false,
        can_revoke: false,
      },
    },
  ]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(row)));
    await expect(
      api.read("wrk_main", "environment", "env_stage"),
    ).rejects.toThrow("契约");
  }
});
it("accepts a first grant candidate but rejects absent authorization on the existing-grants list", async () => {
  const member = {
    user: { id: "usr_owner", display_name: "Owner" },
    eligible: true,
    authorization: null,
  };
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValueOnce(response(member))
      .mockResolvedValueOnce(response({ items: [member], next_cursor: null })),
  );
  expect(
    (await api.authorization("wrk_main", "env_stage", "usr_owner"))
      .authorization,
  ).toBeNull();
  await expect(api.authorizations("wrk_main", "env_stage")).rejects.toThrow(
    "契约",
  );
});
it("rejects target mismatches and private member fields", async () => {
  for (const row of [
    {
      user: { id: "usr_other", display_name: "Other" },
      eligible: true,
      authorization: null,
    },
    {
      user: {
        id: "usr_owner",
        display_name: "Owner",
        email: "fixture@example.test",
      },
      eligible: true,
      authorization: null,
    },
    {
      user: { id: "usr_owner", display_name: "Owner" },
      eligible: false,
      authorization: null,
    },
  ]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(row)));
    await expect(
      api.authorization("wrk_main", "env_stage", "usr_owner"),
    ).rejects.toThrow("契约");
  }
});
