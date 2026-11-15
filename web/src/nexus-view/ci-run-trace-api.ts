import { AuthRequestError } from "../auth/api";
import { object, requireID } from "../workspace/api";
import {
  parsePage,
  query,
  request,
  text,
} from "../workspace/configuration-api";
import { parseTicketStatus } from "../collaboration/ticket-status";

const invalid = () => new AuthRequestError("构建关联响应不符合当前契约。");

export const buildStatusLabels = {
  succeeded: "成功",
  failed: "失败",
  canceled: "已取消",
};

function terminal(value: unknown): keyof typeof buildStatusLabels {
  if (value !== "succeeded" && value !== "failed" && value !== "canceled")
    throw invalid();
  return value;
}

function ref(value: unknown, kind: string, prefix: string) {
  const row = object(value, ["type", "id"]);
  if (row.type !== kind) throw invalid();
  return requireID(row.id, prefix);
}

function named(value: unknown, kind: string, prefix: string) {
  const row = object(value, ["ref", "name"]);
  return { id: ref(row.ref, kind, prefix), name: text(row.name) };
}

function instant(value: unknown) {
  if (
    typeof value !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z$/u.test(value) ||
    !Number.isFinite(Date.parse(value))
  )
    throw invalid();
  return value;
}

function instantKey(value: string) {
  const parts = value.slice(0, -1).split(".");
  return `${parts[0]}.${(parts[1] ?? "").padEnd(9, "0")}`;
}

function build(row: Record<string, unknown>) {
  return {
    id: ref(row.ref, "ci-run", "cir_"),
    status: terminal(row.status),
    completedAt: instant(row.completed_at),
    component: named(row.component, "component", "cmp_"),
  };
}

function ticketPath(ticket: string) {
  return `/tickets/${encodeURIComponent(requireID(ticket, "tkt_"))}`;
}

function runPath(run: string) {
  return `/ci-runs/${encodeURIComponent(requireID(run, "cir_"))}`;
}

export const ciRunTraceClient = {
  async candidates(
    workspace: string,
    component: string,
    after: string | undefined,
    signal: AbortSignal,
  ) {
    return parsePage(
      await request(
        workspace,
        `/components/${encodeURIComponent(requireID(component, "cmp_"))}/ci-runs${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (value) => {
        const row = build(
          object(value, ["ref", "status", "completed_at", "component"]),
        );
        if (row.component.id !== component) throw invalid();
        return row;
      },
      after,
    );
  },
  async ticketRuns(
    workspace: string,
    ticket: string,
    after: string | undefined,
    signal: AbortSignal,
  ) {
    const data = object(
      await request(
        workspace,
        `${ticketPath(ticket)}/ci-runs${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      ["items", "next_cursor", "capabilities"],
    );
    const canLink = object(data.capabilities, ["can_link"]).can_link;
    if (typeof canLink !== "boolean") throw invalid();
    const page = parsePage(
      { items: data.items, next_cursor: data.next_cursor },
      (value) => {
        const row = object(value, [
          "ref",
          "status",
          "completed_at",
          "component",
          "link",
          "can_unlink",
        ]);
        if (row.can_unlink !== canLink) throw invalid();
        return {
          ...build(row),
          linkID: ref(row.link, "entity-link", "lnk_"),
          canUnlink: canLink,
        };
      },
      after,
    );
    return { ...page, canLink };
  },
  async history(
    workspace: string,
    ticket: string,
    after: string | undefined,
    signal: AbortSignal,
  ) {
    return parsePage(
      await request(
        workspace,
        `${ticketPath(ticket)}/ci-run-link-history${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (value) => {
        const row = object(value, [
          "ref",
          "ci_run",
          "link",
          "actor",
          "occurred_at",
          "relation_state",
        ]);
        if (row.relation_state !== "active" && row.relation_state !== "removed")
          throw invalid();
        return {
          id: ref(row.ref, "event", "evt_"),
          runID: ref(row.ci_run, "ci-run", "cir_"),
          linkID: ref(row.link, "entity-link", "lnk_"),
          actor: ref(row.actor, "user", "usr_"),
          at: instant(row.occurred_at),
          state: row.relation_state,
        };
      },
      after,
      (previous, current) =>
        instantKey(current.at) < instantKey(previous.at) ||
        (current.at === previous.at && current.id < previous.id),
    );
  },
  async runTickets(
    workspace: string,
    run: string,
    after: string | undefined,
    signal: AbortSignal,
  ) {
    return parsePage(
      await request(
        workspace,
        `${runPath(run)}/tickets${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (value) => {
        const row = object(value, ["ref", "title", "status", "project"]);
        return {
          id: ref(row.ref, "ticket", "tkt_"),
          status: parseTicketStatus(row.status),
          title: text(row.title),
          projectID: ref(row.project, "project", "prj_"),
        };
      },
      after,
    );
  },
  async deployments(
    workspace: string,
    run: string,
    after: string | undefined,
    signal: AbortSignal,
  ) {
    return parsePage(
      await request(
        workspace,
        `${runPath(run)}/deployments${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (value) => {
        const row = object(value, [
          "ref",
          "status",
          "completed_at",
          "environment",
        ]);
        return {
          id: ref(row.ref, "deployment", "dpl_"),
          status: terminal(row.status),
          completedAt: instant(row.completed_at),
          environment: named(row.environment, "environment", "env_"),
        };
      },
      after,
    );
  },
  async write(
    workspace: string,
    ticket: string,
    change: { run: string } | { link: string },
    operation: string,
    signal: AbortSignal,
  ) {
    const unlink = "link" in change;
    const path = unlink
      ? `/ci-run-links/${encodeURIComponent(requireID(change.link, "lnk_"))}`
      : "/ci-runs";
    const data = object(
      await request(
        workspace,
        ticketPath(ticket) + path,
        unlink ? "DELETE" : "POST",
        {
          client_operation_id: operation,
          confirmed: true,
          ...(unlink ? {} : { ci_run_id: requireID(change.run, "cir_") }),
        },
        signal,
      ),
      ["applied", "link_id"],
    );
    const id = requireID(data.link_id, "lnk_");
    if (data.applied !== true || (unlink && id !== change.link))
      throw invalid();
  },
};

export type BuildCandidate = ReturnType<typeof build>;
export type TicketRun = Awaited<
  ReturnType<typeof ciRunTraceClient.ticketRuns>
>["items"][number];
