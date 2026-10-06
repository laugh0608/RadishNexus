import { afterEach, expect, it, vi } from "vitest";
import {
  ticketComponentClient as api,
  componentLocation,
  componentPagePath,
} from "./ticket-component-api";
import { parseCollaborationView } from "./api";
const component = {
  ref: { type: "component", id: "cmp_one" },
  name: "Service",
  key: "service",
  type: "service",
  lifecycle: "active",
  owner_team_id: "tem_main",
};
const response = (data: unknown) =>
  new Response(JSON.stringify({ data }), {
    headers: { "Content-Type": "application/json" },
  });
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
it("strictly parses association pages and capabilities", async () => {
  const row = { link_id: "lnk_one", target: component, can_unlink: true };
  const fetch = vi.fn().mockResolvedValue(
    response({
      items: [row],
      next_cursor: null,
      capabilities: { can_link: true },
    }),
  );
  vi.stubGlobal("fetch", fetch);
  expect(await api.components("wrk_main", "tkt_one")).toMatchObject({
    canLink: true,
    items: [{ linkID: "lnk_one", target: { id: "cmp_one" } }],
  });
  for (const page of [
    { items: [row, row], next_cursor: null, capabilities: { can_link: true } },
    { items: [row], next_cursor: null, capabilities: { can_link: false } },
    {
      items: [{ ...row, target: { ...component, secret: "hidden" } }],
      next_cursor: null,
      capabilities: { can_link: true },
    },
  ]) {
    fetch.mockResolvedValue(response(page));
    await expect(api.components("wrk_main", "tkt_one")).rejects.toThrow("契约");
  }
  fetch.mockResolvedValue(
    response({
      items: [
        {
          link_id: "lnk_one",
          target: {
            ref: { type: "ticket", id: "tkt_one" },
            title: "Work",
            status: "open",
            project: { type: "project", id: "prj_main" },
          },
          can_unlink: false,
        },
      ],
      next_cursor: null,
    }),
  );
  expect((await api.tickets("wrk_main", "cmp_one")).items[0]!.target).toEqual({
    id: "tkt_one",
    title: "Work",
    status: "open",
    projectID: "prj_main",
  });
});
it("uses exact command paths with CSRF and refuses another unlink generation", async () => {
  const csrf = "c".repeat(43);
  vi.spyOn(document, "cookie", "get").mockReturnValue(
    `__Host-radishnexus-csrf=${csrf}`,
  );
  const fetch = vi
    .fn()
    .mockResolvedValue(response({ link_id: "lnk_one", applied: true }));
  vi.stubGlobal("fetch", fetch);
  const body = {
    client_operation_id: "retry",
    component_id: "cmp_one",
    confirmed: true,
  };
  await api.link("wrk_main", "tkt_one", body);
  expect(fetch.mock.calls[0]).toEqual([
    "/api/v1/workspaces/wrk_main/tickets/tkt_one/components",
    expect.objectContaining({
      method: "POST",
      body: JSON.stringify(body),
      headers: expect.objectContaining({ "X-CSRF-Token": csrf }),
    }),
  ]);
  await expect(
    api.unlink("wrk_main", "tkt_one", "lnk_other", {
      client_operation_id: "unlink",
      confirmed: true,
    }),
  ).rejects.toThrow("契约");
  expect(componentPagePath("wrk_main", "cmp_one")).toBe(
    "/workspaces/wrk_main/components/cmp_one",
  );
  expect(componentLocation("/workspaces/wrk_main/components/cmp_one")).toEqual({
    workspaceID: "wrk_main",
    componentID: "cmp_one",
  });
  expect(
    componentLocation("/workspaces/wrk_main/components/tkt_wrong"),
  ).toBeNull();
});
it("adds relation-state events without accepting Ticket status or loosening original Decision source", () => {
  const now = "2026-10-06T08:00:00Z",
    actor = { kind: "user", id: "usr_member" };
  const source = {
    visibility: "readable",
    direction: "outgoing",
    relation_type: "implements",
    target: { ref: { type: "decision", id: "dec_source" }, title: "Source" },
  };
  const relation = {
    visibility: "readable",
    direction: "outgoing",
    relation_type: "affects",
    target: { ref: component.ref, title: component.name },
  };
  const event = {
    id: "evt_link",
    activity_type: "ticket.component-linked",
    actor,
    occurred_at: now,
    relation_state: "active",
    subjects: [{ visibility: "readable", entity: relation.target }],
  };
  const data = {
    current: {
      ref: { type: "ticket", id: "tkt_one" },
      project: { type: "project", id: "prj_main" },
      title: "Work",
      status: "open",
      created_by: actor,
      created_at: now,
      updated_at: now,
    },
    relations: [source, relation],
    timeline: [event],
  };
  expect(
    parseCollaborationView({ data }, "ticket", "tkt_one").timeline[0],
  ).toMatchObject({
    activityType: "ticket.component-linked",
    relationState: "active",
  });
  for (const bad of [
    { ...event, status: "open" },
    { ...event, relation_state: "removed" },
    { ...event, subjects: [] },
    { ...event, subjects: [{ visibility: "readable", entity: source.target }] },
  ])
    expect(() =>
      parseCollaborationView(
        { data: { ...data, timeline: [bad] } },
        "ticket",
        "tkt_one",
      ),
    ).toThrow();
  expect(() =>
    parseCollaborationView(
      { data: { ...data, relations: [relation] } },
      "ticket",
      "tkt_one",
    ),
  ).toThrow();
});
