import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { projectDecisionsClient } from "../workspace/project-decisions-api";
import { useDecisionSupersession } from "./DecisionSupersession";
import { useDecisionTicketCreation } from "./DecisionTicketCreation";
import {
  CollaborationRequestError,
  type CollaborationClient,
  type CollaborationView,
  type DecisionCurrent,
} from "./api";

const at = "2026-11-02T01:00:00Z",
  later = "2026-11-02T01:00:01Z";
const current: DecisionCurrent = {
  decisionRevision: 2,
  ref: { type: "decision", id: "dec_new" },
  project: { type: "project", id: "prj_main" },
  question: "New conclusion",
  status: "accepted",
  outcome: "New choice",
  rationale: "New evidence",
  rejectionReason: null,
  proposer: { kind: "user", id: "usr_one" },
  deciders: [{ kind: "user", id: "usr_one" }],
  decidedAt: at,
  createdAt: at,
  updatedAt: at,
};
const old: DecisionCurrent = {
  ...current,
  ref: { type: "decision", id: "dec_old" },
  question: "Old conclusion",
  outcome: "Old choice",
};
const capabilities = {
  accept: false,
  reject: false,
  createTicket: true,
  supersede: true,
  retryDecision: true,
  retryTicketCreation: true,
};
const oldView: CollaborationView<DecisionCurrent> = {
  current: old,
  capabilities,
  relations: [
    {
      visibility: "readable",
      direction: "outgoing",
      relationType: "derived-from",
      target: {
        ref: { type: "thread", id: "thr_source" },
        title: "Original evidence",
      },
    },
  ],
  timeline: [],
};
function client(
  overrides: Partial<CollaborationClient> = {},
): CollaborationClient {
  return {
    loadView: vi.fn().mockResolvedValue(oldView),
    supersedeDecision: vi.fn(),
    acceptDecision: vi.fn(),
    rejectDecision: vi.fn(),
    proposeDecision: vi.fn(),
    createTicket: vi.fn(),
    changeTicketStatus: vi.fn(),
    ...overrides,
  };
}
type SupProps = Parameters<typeof useDecisionSupersession>[0];
function Subject(props: SupProps) {
  return useDecisionSupersession(props);
}
function setup(api = client()) {
  const props: SupProps = {
    workspaceID: "wrk_main",
    current,
    canSupersede: true,
    canRetry: true,
    unavailable: false,
    client: api,
    createOperationID: vi.fn().mockReturnValue("web:supersession-stable"),
    onRefresh: vi.fn(),
    onUnavailable: vi.fn(),
    onSessionExpired: vi.fn(),
  };
  return { props, ...render(<Subject {...props} />) };
}
async function chooseAndConfirm() {
  fireEvent.click(await screen.findByRole("button", { name: "选择旧结论" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "选择 Old conclusion" }),
  );
  await screen.findByRole("heading", { name: "旧结论：Old conclusion" });
  fireEvent.change(screen.getByLabelText("替代理由"), {
    target: { value: "New evidence changes the conclusion" },
  });
  fireEvent.click(screen.getByRole("checkbox"));
}
beforeEach(() =>
  vi.spyOn(projectDecisionsClient, "decisions").mockResolvedValue({
    items: [
      { id: "dec_old", title: "Old conclusion", status: "accepted" },
      { id: "dec_proposed", title: "Pending conclusion", status: "proposed" },
      { id: "dec_rejected", title: "Rejected conclusion", status: "rejected" },
      {
        id: "dec_superseded",
        title: "Replaced conclusion",
        status: "superseded",
      },
    ],
    nextCursor: null,
  }),
);
afterEach(() => vi.restoreAllMocks());

describe("Decision supersession", () => {
  it("compares both conclusions, keeps all list states, and invalidates edited confirmation", async () => {
    const api = client();
    setup(api);
    await chooseAndConfirm();
    expect(
      screen.getByRole("button", { name: "选择 Pending conclusion" }),
    ).toHaveProperty("disabled", true);
    expect(
      screen.getByRole("button", { name: "选择 Rejected conclusion" }),
    ).toHaveProperty("disabled", true);
    expect(
      screen.getByRole("button", { name: "选择 Replaced conclusion" }),
    ).toHaveProperty("disabled", true);
    expect(
      screen.getByRole("link", { name: "查看旧结论来源：Original evidence" }),
    ).toHaveProperty("pathname", "/workspaces/wrk_main/threads/thr_source");
    fireEvent.change(screen.getByLabelText("替代理由"), {
      target: { value: "Changed after confirmation" },
    });
    expect(screen.getByRole("checkbox")).toHaveProperty("checked", false);
    expect(
      screen.getByRole("button", { name: "确认替代旧结论" }),
    ).toHaveProperty("disabled", true);
    expect(api.supersedeDecision).not.toHaveBeenCalled();
  });
  it("retains the original pair, revisions, reason and operation after an unknown committed result", async () => {
    const write = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("lost reply"))
      .mockResolvedValue({});
    const load = vi.fn().mockResolvedValue(oldView);
    const { props, rerender } = setup(
      client({ supersedeDecision: write, loadView: load }),
    );
    await chooseAndConfirm();
    fireEvent.click(screen.getByRole("button", { name: "确认替代旧结论" }));
    await screen.findByText(/替代结果尚未确认/);
    load.mockResolvedValue({
      ...oldView,
      current: {
        ...old,
        status: "superseded",
        decisionRevision: 3,
        updatedAt: later,
      },
      capabilities: { ...capabilities, createTicket: false, supersede: false },
    });
    rerender(
      <Subject
        {...props}
        current={{ ...current, decisionRevision: 3, updatedAt: later }}
        canSupersede={false}
      />,
    );
    const retry = await screen.findByRole("button", { name: "重试原替代操作" });
    await waitFor(() => expect(retry).toHaveProperty("disabled", false));
    fireEvent.click(retry);
    await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
    expect(write.mock.calls[0]!.slice(0, 3)).toEqual(
      write.mock.calls[1]!.slice(0, 3),
    );
    expect(write.mock.calls[1]![2]).toMatchObject({
      baseDecisionRevision: 2,
      baseReplacementRevision: 2,
      clientOperationID: "web:supersession-stable",
    });
    expect(props.createOperationID).toHaveBeenCalledOnce();
    expect(props.onRefresh).toHaveBeenCalledOnce();
  });
  it("clears private draft and confirmation after old evidence permission is revoked", async () => {
    const load = vi.fn().mockResolvedValue(oldView);
    const { props, rerender } = setup(client({ loadView: load }));
    await chooseAndConfirm();
    load.mockResolvedValue({
      ...oldView,
      capabilities: { ...capabilities, retryDecision: false, supersede: false },
      relations: [{ visibility: "restricted" }],
    });
    rerender(<Subject {...props} current={{ ...current }} />);
    await screen.findByText(/旧结论当前不具备替代条件/);
    expect(screen.queryByLabelText("替代理由")).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(document.body.textContent).not.toContain(
      "New evidence changes the conclusion",
    );
  });
  it("aborts an in-flight request on revalidation and ignores its late successful response", async () => {
    let finish!: (v: unknown) => void;
    const write = vi.fn().mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const { props, rerender } = setup(client({ supersedeDecision: write }));
    await chooseAndConfirm();
    fireEvent.click(screen.getByRole("button", { name: "确认替代旧结论" }));
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    rerender(<Subject {...props} current={{ ...current }} />);
    await waitFor(() => expect(write.mock.calls[0]![3].aborted).toBe(true));
    await act(async () => finish({}));
    expect(props.onRefresh).not.toHaveBeenCalled();
    await screen.findByRole("button", { name: "重试原替代操作" });
  });
});

type TicketProps = Parameters<typeof useDecisionTicketCreation>[0];
function Ticket(props: TicketProps) {
  return useDecisionTicketCreation(props);
}
describe("Ticket creation after supersession", () => {
  it("replays the original creation after the source becomes superseded", async () => {
    const result = {
      created: false,
      sourceDecision: old.ref,
      ticket: {
        ref: { type: "ticket", id: "tkt_one" },
        project: old.project,
        title: "Original work",
        status: "open",
        statusRevision: 1,
        createdBy: old.proposer,
        createdAt: at,
        updatedAt: at,
      },
    };
    const create = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("lost reply"))
      .mockResolvedValue(result);
    const props: TicketProps = {
      workspaceID: "wrk_main",
      current: old,
      canCreate: true,
      canRetry: true,
      unavailable: false,
      client: client({ createTicket: create }),
      createOperationID: vi.fn().mockReturnValue("web:original-ticket"),
      onRefresh: vi.fn(),
      onUnavailable: vi.fn(),
      onSessionExpired: vi.fn(),
      onCreated: vi.fn(),
    };
    const { rerender } = render(<Ticket {...props} />);
    fireEvent.change(screen.getByLabelText("Ticket 标题"), {
      target: { value: "Original work" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建 Ticket" }));
    await screen.findByText(/工单创建结果尚未确认/);
    rerender(
      <Ticket
        {...props}
        current={{
          ...old,
          status: "superseded",
          decisionRevision: 3,
          updatedAt: later,
        }}
        canCreate={false}
      />,
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "重试原工单创建" }),
    );
    await waitFor(() => expect(create).toHaveBeenCalledTimes(2));
    expect(create.mock.calls[0]!.slice(0, 3)).toEqual(
      create.mock.calls[1]!.slice(0, 3),
    );
    expect(props.createOperationID).toHaveBeenCalledOnce();
    expect(props.onCreated).toHaveBeenCalledWith(result);
  });
  it.each([401, 404])(
    "clears an unavailable creation on %s",
    async (status) => {
      const onUnavailable = vi.fn(),
        onSessionExpired = vi.fn();
      render(
        <Ticket
          workspaceID="wrk_main"
          current={old}
          canCreate
          canRetry
          unavailable={false}
          client={client({
            createTicket: vi
              .fn()
              .mockRejectedValue(
                new CollaborationRequestError("denied", { status }),
              ),
          })}
          createOperationID={() => "web:denied"}
          onRefresh={vi.fn()}
          onUnavailable={onUnavailable}
          onSessionExpired={onSessionExpired}
          onCreated={vi.fn()}
        />,
      );
      fireEvent.change(screen.getByLabelText("Ticket 标题"), {
        target: { value: "Work" },
      });
      fireEvent.click(screen.getByRole("button", { name: "创建 Ticket" }));
      await waitFor(() => expect(onUnavailable).toHaveBeenCalledOnce());
      expect(onSessionExpired).toHaveBeenCalledTimes(status === 401 ? 1 : 0);
      expect(
        screen.queryByRole("button", { name: "重试原工单创建" }),
      ).toBeNull();
    },
  );
});
