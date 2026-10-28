import { afterEach, describe, it, expect, vi } from "vitest";
import {
  ticketPriorityClient,
  type PriorityInput,
} from "./ticket-priority-api";
const read = {
  priority: "unset",
  priority_revision: 1,
  status_revision: 1,
  can_set_priority: true,
};
function response(data: unknown) {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
describe("Ticket priority contract", () => {
  it("requires exact current fields and bounded versions", async () => {
    const fetch = vi.fn().mockResolvedValue(response(read));
    vi.stubGlobal("fetch", fetch);
    expect(await ticketPriorityClient.read("wrk_a", "tkt_a")).toEqual({
      priority: "unset",
      revision: 1,
      statusRevision: 1,
      canSetPriority: true,
    });
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/workspaces/wrk_a/tickets/tkt_a/priority",
      expect.objectContaining({
        method: "GET",
        cache: "no-store",
        credentials: "same-origin",
      }),
    );
    for (const value of [
      { ...read, priority: undefined },
      { ...read, priority: null },
      { ...read, priority: "p0" },
      { ...read, priority_revision: 0 },
      { ...read, status_revision: 1.5 },
      { ...read, priority_revision: 2147483648 },
      { ...read, priority_revision: 2147483647 },
      { ...read, can_set_priority: "true" },
      { ...read, private: "secret" },
    ]) {
      fetch.mockResolvedValue(response(value));
      await expect(
        ticketPriorityClient.read("wrk_a", "tkt_a"),
      ).rejects.toThrow();
    }
  });
  it("binds applied facts to the original object, value and revision", async () => {
    vi.spyOn(document, "cookie", "get").mockReturnValue(
      `__Host-radishnexus-csrf=${"c".repeat(43)}`,
    );
    const input: PriorityInput = {
      client_operation_id: "operation",
      base_priority_revision: 1,
      base_status_revision: 2,
      priority: "normal",
      confirmed: true,
    };
    const applied = {
      ticket: { type: "ticket", id: "tkt_a" },
      applied: true,
      applied_priority: "normal",
      applied_priority_revision: 2,
    };
    const fetch = vi.fn().mockResolvedValue(response(applied));
    vi.stubGlobal("fetch", fetch);
    await expect(
      ticketPriorityClient.set("wrk_a", "tkt_a", input),
    ).resolves.toEqual({ priority: "normal", revision: 2 });
    expect(JSON.parse(fetch.mock.calls[0]![1].body)).toEqual(input);
    for (const value of [
      { ...applied, ticket: { type: "ticket", id: "tkt_other" } },
      { ...applied, applied: false },
      { ...applied, applied_priority: "high" },
      { ...applied, applied_priority_revision: 3 },
      { ...applied, event_id: "evt_private" },
    ]) {
      fetch.mockResolvedValue(response(value));
      await expect(
        ticketPriorityClient.set("wrk_a", "tkt_a", input),
      ).rejects.toThrow();
    }
  });
});
