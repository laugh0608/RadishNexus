import { afterEach, describe, expect, it, vi } from "vitest";
import {
  parseStagingResult,
  parseStagingTargets,
  stagingClient,
} from "./staging-api";
const item = {
  ref: { type: "environment", id: "env_stage" },
  name: "Staging",
  key: "STAGE",
  classification: "staging",
};
const payload = { data: { items: [item], next_cursor: null } };
afterEach(() => vi.unstubAllGlobals());
describe("staging API", () => {
  it("rejects projection drift, duplicate IDs, production and sensitive fields", () => {
    expect(parseStagingTargets(payload).items).toHaveLength(1);
    for (const items of [
      [{ ...item, authorization_id: "private" }],
      [{ ...item, classification: "production" }],
      [item, item],
      [{ ...item, ref: { type: "environment", id: "dpl_wrong" } }],
    ])
      expect(() =>
        parseStagingTargets({ data: { items, next_cursor: null } }),
      ).toThrow();
    expect(() =>
      parseStagingResult({
        data: {
          deployment: { type: "deployment", id: "dpl_one" },
          duplicate: "true",
        },
      }),
    ).toThrow();
  });
  it("uses same-origin no-store and sends the confirmed request with CSRF", async () => {
    Object.defineProperty(document, "cookie", {
      configurable: true,
      value: "__Host-radishnexus-csrf=" + "a".repeat(43),
    });
    const fetcher = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          data: {
            deployment: { type: "deployment", id: "dpl_one" },
            duplicate: true,
          },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetcher);
    // Cookie name is supplied by the existing session contract.
    const input = {
      client_operation_id: "op",
      environment_id: "env_stage",
      status: "failed" as const,
      started_at: null,
      completed_at: "2026-01-01T00:00:00Z",
      confirmed: true as const,
    };
    await stagingClient.record("wrk_main", "cir_one", input);
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/workspaces/wrk_main/ci-runs/cir_one/staging-deployments",
      expect.objectContaining({
        method: "POST",
        credentials: "same-origin",
        cache: "no-store",
        body: JSON.stringify(input),
        headers: expect.objectContaining({ "X-CSRF-Token": "a".repeat(43) }),
      }),
    );
  });
  it("does not retry a network failure or accept malformed success", async () => {
    const fetcher = vi.fn().mockRejectedValue(new TypeError("offline"));
    vi.stubGlobal("fetch", fetcher);
    await expect(
      stagingClient.targets("wrk_main", "cir_one"),
    ).rejects.toThrow();
    expect(fetcher).toHaveBeenCalledOnce();
    fetcher.mockResolvedValue(
      new Response("{}", { headers: { "Content-Type": "application/json" } }),
    );
    await expect(stagingClient.targets("wrk_main", "cir_one")).rejects.toThrow(
      "协议",
    );
  });
});
