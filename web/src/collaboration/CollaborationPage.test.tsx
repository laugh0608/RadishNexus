import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { CollaborationPage } from "./CollaborationPage";
import {
  CollaborationRequestError,
  type CollaborationClient,
  type CollaborationView,
  type DecisionCurrent,
  type ThreadCurrent,
} from "./api";

const now = "2026-09-02T03:04:05Z";

const thread: ThreadCurrent = {
  ref: { type: "thread", id: "thr_source" },
  project: { type: "project", id: "prj_main" },
  originChannel: {
    ref: { type: "channel", id: "chn_main" },
    title: "Project Channel",
  },
  title: "Choose the collaboration boundary",
  visibility: "restricted",
  createdBy: { kind: "user", id: "usr_contributor" },
  createdAt: now,
  updatedAt: now,
};

const proposedDecision: DecisionCurrent = {
  ref: { type: "decision", id: "dec_choice" },
  project: { type: "project", id: "prj_main" },
  question: "Adopt the collaboration boundary?",
  status: "proposed",
  outcome: null,
  rationale: null,
  proposer: { kind: "user", id: "usr_contributor" },
  deciders: [],
  decidedAt: null,
  createdAt: now,
  updatedAt: now,
};

const acceptedDecision: DecisionCurrent = {
  ...proposedDecision,
  status: "accepted",
  outcome: "Use the Session boundary.",
  rationale: "It preserves current authority.",
  deciders: [{ kind: "user", id: "usr_decider" }],
  decidedAt: now,
};

const threadView: CollaborationView<ThreadCurrent> = {
  current: thread,
  relations: [
    {
      visibility: "readable",
      relationType: "started-from",
      target: {
        ref: { type: "message", id: "msg_source" },
        title: "Message",
      },
    },
  ],
  timeline: [],
};

const decisionView: CollaborationView<DecisionCurrent> = {
  current: proposedDecision,
  relations: [{ visibility: "restricted" }],
  timeline: [],
};

describe("CollaborationPage", () => {
  it("keeps one operation ID across an ambiguous Decision proposal retry", async () => {
    const proposeDecision = vi
      .fn()
      .mockRejectedValueOnce(
        new CollaborationRequestError(
          "无法连接到 RadishNexus；保持表单不变后可安全重试。",
        ),
      )
      .mockResolvedValueOnce({
        decision: proposedDecision,
        sourceThread: { type: "thread", id: "thr_source" },
        created: false,
      });
    const createOperationID = vi.fn().mockReturnValue("web:stable-proposal");
    renderPage("thread", "thr_source", testClient({ proposeDecision }), {
      createOperationID,
    });

    expect(
      await screen.findByRole("heading", {
        name: "Choose the collaboration boundary",
      }),
    ).toBeDefined();
    expect(screen.getByText("Message")).toBeDefined();
    expect(screen.queryByText("Source Message body")).toBeNull();

    fireEvent.change(screen.getByLabelText("Decision 问题"), {
      target: { value: "Adopt the collaboration boundary?" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "创建 Proposed Decision" }),
    );
    expect(
      await screen.findByText(
        "无法连接到 RadishNexus；保持表单不变后可安全重试。",
      ),
    ).toBeDefined();

    fireEvent.click(
      screen.getByRole("button", { name: "创建 Proposed Decision" }),
    );
    expect(await screen.findByText("Decision 重试已确认")).toBeDefined();
    expect(
      screen
        .getByRole("link", { name: "打开 canonical Decision" })
        .getAttribute("href"),
    ).toBe("/workspaces/wrk_main/decisions/dec_choice");
    expect(createOperationID).toHaveBeenCalledOnce();
    expect(proposeDecision).toHaveBeenNthCalledWith(
      1,
      "wrk_main",
      "thr_source",
      {
        clientOperationID: "web:stable-proposal",
        question: "Adopt the collaboration boundary?",
      },
    );
    expect(proposeDecision).toHaveBeenNthCalledWith(
      2,
      "wrk_main",
      "thr_source",
      {
        clientOperationID: "web:stable-proposal",
        question: "Adopt the collaboration boundary?",
      },
    );
  });

  it("requires explicit human confirmation before accepting a Decision", async () => {
    const acceptDecision = vi.fn().mockResolvedValue(acceptedDecision);
    const createTicket = vi.fn().mockResolvedValue({
      created: true,
      ticket: {
        ref: { type: "ticket", id: "tkt_work" },
        project: { type: "project", id: "prj_main" },
        title: "Implement collaboration UI",
        status: "open",
        createdBy: { kind: "user", id: "usr_decider" },
        createdAt: now,
        updatedAt: now,
      },
      sourceDecision: { type: "decision", id: "dec_choice" },
    });
    const createOperationID = vi
      .fn()
      .mockReturnValueOnce("web:accept")
      .mockReturnValueOnce("web:ticket");
    renderPage(
      "decision",
      "dec_choice",
      testClient({
        loadView: vi.fn().mockResolvedValue(decisionView),
        acceptDecision,
        createTicket,
      }),
      { createOperationID },
    );

    expect(
      await screen.findByRole("heading", {
        name: "Adopt the collaboration boundary?",
      }),
    ).toBeDefined();
    expect(screen.getByText("受限 evidence")).toBeDefined();
    fireEvent.change(screen.getByLabelText("明确结论"), {
      target: { value: "Use the Session boundary." },
    });
    fireEvent.change(screen.getByLabelText("理由"), {
      target: { value: "It preserves current authority." },
    });
    expect(
      (
        screen.getByRole("button", {
          name: "接受 Decision",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(
      screen.getByLabelText("我正在以当前用户身份明确接受这个 Decision。"),
    );
    fireEvent.click(screen.getByRole("button", { name: "接受 Decision" }));

    expect(await screen.findByText("Accepted outcome")).toBeDefined();
    expect(screen.getByText("Use the Session boundary.")).toBeDefined();
    expect(acceptDecision).toHaveBeenCalledWith("wrk_main", "dec_choice", {
      clientOperationID: "web:accept",
      outcome: "Use the Session boundary.",
      rationale: "It preserves current authority.",
      confirmed: true,
    });

    fireEvent.change(screen.getByLabelText("Ticket 标题"), {
      target: { value: "Implement collaboration UI" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建 Ticket" }));
    expect(await screen.findByText("Ticket 已创建")).toBeDefined();
    expect(
      screen
        .getByRole("link", { name: "打开 canonical Ticket" })
        .getAttribute("href"),
    ).toBe("/workspaces/wrk_main/tickets/tkt_work");
  });

  it("removes the rendered object and drafts when a write proves access revoked", async () => {
    const proposeDecision = vi
      .fn()
      .mockRejectedValue(
        new CollaborationRequestError(
          "该协作对象不存在，或你已没有读取它的权限。",
          { code: "not_found", status: 404 },
        ),
      );
    renderPage("thread", "thr_source", testClient({ proposeDecision }));
    await screen.findByText("Choose the collaboration boundary");
    fireEvent.change(screen.getByLabelText("Decision 问题"), {
      target: { value: "Draft question" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "创建 Proposed Decision" }),
    );

    expect(
      await screen.findByRole("heading", { name: "无法读取这个协作对象" }),
    ).toBeDefined();
    expect(screen.queryByText("Choose the collaboration boundary")).toBeNull();
    expect(screen.queryByDisplayValue("Draft question")).toBeNull();
  });

  it("returns control to the shell when Session expires", async () => {
    const onSessionExpired = vi.fn();
    render(
      <CollaborationPage
        client={testClient({
          loadView: vi.fn().mockRejectedValue(
            new CollaborationRequestError("当前会话已失效，请重新登录。", {
              code: "unauthenticated",
              status: 401,
            }),
          ),
        })}
        entityID="dec_choice"
        entityType="decision"
        onSessionExpired={onSessionExpired}
        workspaceID="wrk_main"
      />,
    );
    await waitFor(() => expect(onSessionExpired).toHaveBeenCalledOnce());
  });
});

function renderPage(
  entityType: "thread" | "decision" | "ticket",
  entityID: string,
  client: CollaborationClient,
  options: { createOperationID?: () => string } = {},
): void {
  render(
    <CollaborationPage
      client={client}
      createOperationID={options.createOperationID}
      entityID={entityID}
      entityType={entityType}
      onSessionExpired={vi.fn()}
      workspaceID="wrk_main"
    />,
  );
}

function testClient(
  overrides: Partial<CollaborationClient> = {},
): CollaborationClient {
  return {
    loadView: vi.fn().mockResolvedValue(threadView),
    proposeDecision: vi.fn(),
    acceptDecision: vi.fn(),
    createTicket: vi.fn(),
    ...overrides,
  };
}
