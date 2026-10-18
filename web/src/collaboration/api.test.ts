import { afterEach, describe, expect, it, vi } from "vitest";
import {
  CollaborationRequestError,
  browserCollaborationClient,
  collaborationLocation,
  collaborationPagePath,
  parseCollaborationView,
} from "./api";

const csrfToken = "c".repeat(43);
const now = "2026-09-02T03:04:05.123Z";

const threadCurrent = {
  ref: { type: "thread", id: "thr_source" },
  project: { type: "project", id: "prj_main" },
  origin_channel: {
    ref: { type: "channel", id: "chn_main" },
    title: "Project Channel",
  },
  title: "Choose the collaboration boundary",
  visibility: "restricted",
  created_by: { kind: "user", id: "usr_contributor" },
  created_at: now,
  updated_at: now,
};

const proposedDecision = {
  ref: { type: "decision", id: "dec_choice" },
  project: { type: "project", id: "prj_main" },
  question: "Adopt the collaboration boundary?",
  status: "proposed",
  outcome: null,
  rationale: null,
  proposer: { kind: "user", id: "usr_contributor" },
  deciders: [],
  decided_at: null,
  created_at: now,
  updated_at: now,
};

const acceptedDecision = {
  ...proposedDecision,
  status: "accepted",
  outcome: "Use the Session boundary.",
  rationale: "It preserves current authority.",
  deciders: [{ kind: "user", id: "usr_decider" }],
  decided_at: now,
};

const ticketCurrent = {
  ref: { type: "ticket", id: "tkt_work" },
  project: { type: "project", id: "prj_main" },
  title: "Implement collaboration UI",
  status: "open",
  status_revision: 1,
  created_by: { kind: "user", id: "usr_contributor" },
  created_at: now,
  updated_at: now,
};

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(document, "cookie");
});

describe("Collaboration API adapter", () => {
  it("recognizes only canonical collaboration page locations", () => {
    expect(
      collaborationLocation("/workspaces/wrk_main/threads/thr_source"),
    ).toEqual({
      workspaceID: "wrk_main",
      entityType: "thread",
      entityID: "thr_source",
    });
    expect(
      collaborationLocation("/workspaces/wrk_main/decisions/dec_choice/"),
    ).toEqual({
      workspaceID: "wrk_main",
      entityType: "decision",
      entityID: "dec_choice",
    });
    expect(
      collaborationLocation("/workspaces/wrk_main/threads/not-a-thread"),
    ).toBeNull();
    expect(
      collaborationLocation(
        "/workspaces/wrk_main/decisions/dec_choice/settings",
      ),
    ).toBeNull();
    expect(collaborationPagePath("wrk_main", "ticket", "tkt_work")).toBe(
      "/workspaces/wrk_main/tickets/tkt_work",
    );
    expect(collaborationPagePath("wrong", "ticket", "tkt_work")).toBeNull();
  });

  it("loads a messaging-origin Thread without accepting Message body", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        data: {
          current: threadCurrent,
          relations: [
            {
              visibility: "readable",
              direction: "outgoing",
              relation_type: "started-from",
              target: {
                ref: { type: "message", id: "msg_source" },
                title: "Message",
              },
            },
          ],
          timeline: [],
        },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const view = await browserCollaborationClient.loadView(
      "wrk_main",
      "thread",
      "thr_source",
    );

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/workspaces/wrk_main/threads/thr_source/nexus-view",
      expect.objectContaining({
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
      }),
    );
    expect(view.current).toMatchObject({
      ref: { type: "thread", id: "thr_source" },
      originChannel: { title: "Project Channel" },
    });
    expect(JSON.stringify(view)).not.toMatch(/body|client_operation|receipt/iu);
  });

  it("proposes a Decision with CSRF and distinguishes creation from retry", async () => {
    setCSRFCookie();
    const payload = {
      data: {
        decision: proposedDecision,
        source_thread: { type: "thread", id: "thr_source" },
      },
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(payload, 201))
      .mockResolvedValueOnce(jsonResponse(payload, 200));
    vi.stubGlobal("fetch", fetchMock);
    const input = {
      clientOperationID: "web:stable-proposal",
      question: "Adopt the collaboration boundary?",
    };

    const created = await browserCollaborationClient.proposeDecision(
      "wrk_main",
      "thr_source",
      input,
    );
    const retried = await browserCollaborationClient.proposeDecision(
      "wrk_main",
      "thr_source",
      input,
    );

    expect(created.created).toBe(true);
    expect(retried.created).toBe(false);
    expect(created.decision.ref.id).toBe("dec_choice");
    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/v1/workspaces/wrk_main/threads/thr_source/decisions",
      expect.objectContaining({
        method: "POST",
        credentials: "same-origin",
        cache: "no-store",
        headers: expect.objectContaining({ "X-CSRF-Token": csrfToken }),
        body: JSON.stringify({
          client_operation_id: "web:stable-proposal",
          question: "Adopt the collaboration boundary?",
        }),
      }),
    );
  });

  it("requires explicit acceptance and creates a structured Ticket", async () => {
    setCSRFCookie();
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: acceptedDecision }))
      .mockResolvedValueOnce(
        jsonResponse(
          {
            data: {
              ticket: ticketCurrent,
              source_decision: { type: "decision", id: "dec_choice" },
            },
          },
          201,
        ),
      );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      browserCollaborationClient.acceptDecision("wrk_main", "dec_choice", {
        clientOperationID: "web:accept",
        outcome: "Use the Session boundary.",
        rationale: "It preserves current authority.",
        confirmed: false as true,
      }),
    ).rejects.toMatchObject({ code: "invalid", status: 400 });

    const decision = await browserCollaborationClient.acceptDecision(
      "wrk_main",
      "dec_choice",
      {
        clientOperationID: "web:accept",
        outcome: "Use the Session boundary.",
        rationale: "It preserves current authority.",
        confirmed: true,
      },
    );
    const ticket = await browserCollaborationClient.createTicket(
      "wrk_main",
      "dec_choice",
      {
        clientOperationID: "web:ticket",
        title: "Implement collaboration UI",
      },
    );

    expect(decision.status).toBe("accepted");
    expect(ticket).toMatchObject({
      created: true,
      ticket: { ref: { id: "tkt_work" } },
      sourceDecision: { id: "dec_choice" },
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("accepts incoming results and rejects direction, source and duplicate drift", () => {
    const incoming = {
      visibility: "readable",
      direction: "incoming",
      relation_type: "implements",
      target: { ref: { type: "ticket", id: "tkt_result" }, title: "Result" },
    };
    const parse = (relations: unknown[]) =>
      parseCollaborationView(
        {
          data: {
            current: proposedDecision,
            relations,
            timeline: [],
          },
        },
        "decision",
        "dec_choice",
      );
    expect(
      parse([{ visibility: "restricted" }, incoming]).relations[1],
    ).toMatchObject({
      direction: "incoming",
      target: { ref: { id: "tkt_result" } },
    });
    for (const invalid of [
      { ...incoming, direction: undefined },
      { ...incoming, direction: "outgoing" },
      { ...incoming, relation_type: "derived-from" },
      {
        ...incoming,
        target: { ref: { type: "thread", id: "thr_wrong" }, title: "Wrong" },
      },
      { visibility: "restricted", direction: "incoming" },
    ])
      expect(() => parse([{ visibility: "restricted" }, invalid])).toThrow();
    expect(() => parse([incoming])).toThrow(/source relation/iu);
    expect(() =>
      parse([{ visibility: "restricted" }, incoming, incoming]),
    ).toThrow(/duplicate/iu);
  });

  it("keeps restricted evidence opaque and rejects leaked or extra fields", () => {
    const safe = {
      data: {
        current: proposedDecision,
        relations: [{ visibility: "restricted" }],
        timeline: [
          {
            id: "evt_proposed",
            activity_type: "decision.proposed",
            actor: { kind: "user", id: "usr_contributor" },
            occurred_at: now,
            status: "proposed",
            subjects: [{ visibility: "restricted" }],
          },
        ],
      },
    };
    const parsed = parseCollaborationView(safe, "decision", "dec_choice");
    expect(parsed.relations).toEqual([{ visibility: "restricted" }]);
    expect(JSON.stringify(parsed.relations)).not.toMatch(
      /type|id|title|time/iu,
    );

    expect(() =>
      parseCollaborationView(
        {
          data: {
            ...safe.data,
            relations: [
              {
                visibility: "restricted",
                target: { type: "thread", id: "thr_private" },
              },
            ],
          },
        },
        "decision",
        "dec_choice",
      ),
    ).toThrow(/unexpected/iu);
    expect(() =>
      parseCollaborationView(
        {
          data: {
            ...safe.data,
            current: { ...proposedDecision, receipt: "must-not-enter" },
          },
        },
        "decision",
        "dec_choice",
      ),
    ).toThrow(/unexpected/iu);
  });

  it("maps revoked access and refuses writes without CSRF", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(
            { error: { code: "not_found", message: "must be ignored" } },
            404,
          ),
        ),
    );
    await expect(
      browserCollaborationClient.loadView("wrk_main", "decision", "dec_choice"),
    ).rejects.toMatchObject({
      code: "not_found",
      status: 404,
      userMessage: "该协作对象不存在，或你已没有读取它的权限。",
    } satisfies Partial<CollaborationRequestError>);

    await expect(
      browserCollaborationClient.createTicket("wrk_main", "dec_choice", {
        clientOperationID: "web:ticket",
        title: "Implement collaboration UI",
      }),
    ).rejects.toMatchObject({ code: "csrf_failed", status: 403 });
  });
});

function setCSRFCookie(): void {
  Object.defineProperty(document, "cookie", {
    configurable: true,
    value: `__Host-radishnexus-csrf=${csrfToken}`,
  });
}

function jsonResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: {
      get: (name: string) =>
        name.toLowerCase() === "content-type" ? "application/json" : null,
    } as Headers,
    json: async () => payload,
  } as Response;
}

it("strictly parses all Ticket states, versions and status history", () => {
  const data = {
    current: { ...ticketCurrent, status_revision: 2 },
    capabilities: { change_status: true },
    relations: [
      {
        visibility: "readable",
        direction: "outgoing",
        relation_type: "implements",
        target: {
          ref: { type: "decision", id: "dec_source" },
          title: "Source",
        },
      },
    ],
    timeline: [
      {
        id: "evt_state",
        activity_type: "ticket.status-changed",
        actor: { kind: "user", id: "usr_member" },
        occurred_at: now,
        subjects: [],
        from_status: "open",
        to_status: "done",
        from_status_revision: 1,
        to_status_revision: 2,
      },
    ],
  };
  for (const status of ["open", "in-progress", "done", "canceled"])
    expect(
      parseCollaborationView(
        { data: { ...data, current: { ...data.current, status } } },
        "ticket",
        "tkt_work",
      ).current,
    ).toMatchObject({ status, statusRevision: 2 });
  for (const bad of [
    { ...data, current: { ...data.current, status_revision: 0 } },
    { ...data, current: { ...data.current, status_revision: 1.5 } },
    { ...data, current: { ...data.current, status_revision: 2147483648 } },
    { ...data, current: { ...data.current, status: "unknown" } },
    { ...data, capabilities: { change_status: "yes" } },
    { ...data, timeline: [{ ...data.timeline[0], to_status: "open" }] },
    { ...data, timeline: [{ ...data.timeline[0], to_status_revision: 3 }] },
    {
      ...data,
      timeline: [
        { ...data.timeline[0], subjects: [{ visibility: "restricted" }] },
      ],
    },
  ])
    expect(() =>
      parseCollaborationView({ data: bad }, "ticket", "tkt_work"),
    ).toThrow();
});
it("sends the immutable status command and validates its original applied receipt", async () => {
  Object.defineProperty(document, "cookie", {
    configurable: true,
    value: `__Host-radishnexus-csrf=${csrfToken}`,
  });
  const input = {
    clientOperationID: "web:status",
    baseStatusRevision: 3,
    toStatus: "done" as const,
    confirmed: true as const,
  };
  const data = {
    ticket: { type: "ticket", id: "tkt_work" },
    applied: true,
    applied_status: "done",
    applied_status_revision: 4,
  };
  const fetch = vi.fn().mockResolvedValue(jsonResponse({ data }));
  vi.stubGlobal("fetch", fetch);
  expect(
    await browserCollaborationClient.changeTicketStatus(
      "wrk_main",
      "tkt_work",
      input,
    ),
  ).toMatchObject({ appliedStatus: "done", appliedStatusRevision: 4 });
  expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({
    client_operation_id: "web:status",
    base_status_revision: 3,
    to_status: "done",
    confirmed: true,
  });
  for (const bad of [
    { ...data, applied: false },
    { ...data, applied_status: "open" },
    { ...data, applied_status_revision: 5 },
    { ...data, event_id: "evt_internal" },
  ]) {
    fetch.mockResolvedValue(jsonResponse({ data: bad }));
    await expect(
      browserCollaborationClient.changeTicketStatus(
        "wrk_main",
        "tkt_work",
        input,
      ),
    ).rejects.toThrow(/契约/);
  }
});
it("returns current Ticket state on create retry but rejects non-open first creation", async () => {
  Object.defineProperty(document, "cookie", {
    configurable: true,
    value: `__Host-radishnexus-csrf=${csrfToken}`,
  });
  const data = {
    ticket: { ...ticketCurrent, status: "canceled", status_revision: 6 },
    source_decision: { type: "decision", id: "dec_choice" },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ data }, 200)),
  );
  expect(
    (
      await browserCollaborationClient.createTicket("wrk_main", "dec_choice", {
        clientOperationID: "web:create",
        title: ticketCurrent.title,
      })
    ).ticket,
  ).toMatchObject({ status: "canceled", statusRevision: 6 });
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ data }, 201)),
  );
  await expect(
    browserCollaborationClient.createTicket("wrk_main", "dec_choice", {
      clientOperationID: "web:create",
      title: ticketCurrent.title,
    }),
  ).rejects.toThrow(/契约/);
});
