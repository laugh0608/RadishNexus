import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ticketAssignmentClient,
  parseAssignee,
  type AssignmentInput,
} from "./ticket-assignee-api";
function response(data: unknown) {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
const read = {
  assignee: null,
  assignment_revision: 1,
  status_revision: 1,
  can_assign: true,
  project_management_revision: "1",
};
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
describe("Ticket assignment contract", () => {
  it("reads minimal identity and refuses private fields and invalid state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(read)));
    expect(
      (await ticketAssignmentClient.read("wrk_a", "tkt_a")).assignee,
    ).toBeNull();
    for (const value of [
      { ...read, assignee: undefined },
      { ...read, assignment_revision: 0 },
      { ...read, can_assign: "true" },
      { ...read, project_management_revision: "01" },
      { ...read, project_management_revision: "9223372036854775808" },
      {
        ...read,
        assignee: {
          id: "usr_a",
          display_name: "Member",
          eligible: true,
          imported: true,
        },
      },
      {
        ...read,
        assignee: {
          id: "usr_a",
          display_name: "Member",
          eligible: true,
          imported: false,
          email: "private@example.test",
        },
      },
    ]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(value)));
      await expect(
        ticketAssignmentClient.read("wrk_a", "tkt_a"),
      ).rejects.toThrow();
    }
  });
  it("rejects unordered candidates and keeps bigint revisions as strings", async () => {
    const row = {
      id: "usr_a",
      display_name: "Member",
      lifecycle_revision: "9223372036854775807",
    };
    const page = {
      items: [row],
      next_cursor: null,
      project_management_revision: "1",
      self_id: "usr_a",
    };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(page)));
    expect(
      (await ticketAssignmentClient.candidates("wrk_a", "tkt_a")).items[0]
        ?.lifecycleRevision,
    ).toBe(row.lifecycle_revision);
    for (const value of [
      { ...page, items: [row, row] },
      { ...page, next_cursor: "nonempty" },
      { ...page, items: [{ ...row, lifecycle_revision: 1 }] },
    ]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(value)));
      await expect(
        ticketAssignmentClient.candidates("wrk_a", "tkt_a"),
      ).rejects.toThrow();
    }
  });
  it("verifies the applied result against the original operation", async () => {
    // The cookie name is shared with the existing browser session contract.
    vi.spyOn(document, "cookie", "get").mockReturnValue(
      `__Host-radishnexus-csrf=${"c".repeat(43)}`,
    );
    const in_: AssignmentInput = {
      client_operation_id: "operation",
      base_assignment_revision: 1,
      base_status_revision: 2,
      assignee_user_id: null,
      target_lifecycle_revision: "0",
      project_management_revision: "3",
      confirmed: true,
    };
    const fetch = vi.fn().mockResolvedValue(
      response({
        applied: true,
        applied_assignment_revision: 2,
        applied_assignee_user_id: null,
      }),
    );
    vi.stubGlobal("fetch", fetch);
    await expect(
      ticketAssignmentClient.set("wrk_a", "tkt_a", in_),
    ).resolves.toEqual({ revision: 2, assigneeID: null });
    fetch.mockResolvedValue(
      response({
        applied: true,
        applied_assignment_revision: 3,
        applied_assignee_user_id: null,
      }),
    );
    await expect(
      ticketAssignmentClient.set("wrk_a", "tkt_a", in_),
    ).rejects.toThrow();
  });
  it("never treats historical identities as eligible", () => {
    expect(() =>
      parseAssignee({
        id: "usr_a",
        display_name: "History",
        eligible: true,
        imported: true,
      }),
    ).toThrow();
  });
});
