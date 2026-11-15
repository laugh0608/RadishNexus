import { AuthRequestError } from "../auth/api";
import {
  parseBuildCandidate,
  parseTraceDeployment,
} from "../nexus-view/ci-run-trace-api";
import { object, requireID } from "../workspace/api";
import {
  parsePage,
  query,
  request,
  text,
} from "../workspace/configuration-api";
import { parseTicketStatus } from "./ticket-status";

const decisionOrigin = "decision-origin";
const messageOrigin = "message-origin";
const discussion = "execution-discussion";

export const deliveryOriginLabels = {
  [decisionOrigin]: "从 Decision 建单",
  [messageOrigin]: "从讨论回复建单",
  [discussion]: "执行讨论",
};

function parseTicket(value: unknown) {
  const row = object(value, ["ref", "title", "status", "origins"]);
  const ref = object(row.ref, ["type", "id"]);
  if (ref.type !== "ticket" || !Array.isArray(row.origins))
    throw new AuthRequestError("执行与交付响应不符合当前契约。");
  const [first, second] = row.origins;
  const creation = first === decisionOrigin || first === messageOrigin;
  if (
    (row.origins.length !== 1 || (!creation && first !== discussion)) &&
    (row.origins.length !== 2 || !creation || second !== discussion)
  )
    throw new AuthRequestError("执行与交付响应不符合当前契约。");
  return {
    id: requireID(ref.id, "tkt_"),
    title: text(row.title),
    status: parseTicketStatus(row.status),
    origins: row.origins as (keyof typeof deliveryOriginLabels)[],
  };
}

export type DeliveryTicket = ReturnType<typeof parseTicket>;

function ticketPath(thread: string) {
  return `/threads/${encodeURIComponent(requireID(thread, "thr_"))}/delivery-tickets`;
}

function runPath(thread: string, ticket: string) {
  return `${ticketPath(thread)}/${encodeURIComponent(requireID(ticket, "tkt_"))}/ci-runs`;
}

async function deliveryPage<T extends { id: string }>(
  workspace: string,
  path: string,
  parse: (value: unknown) => T,
  after: string | undefined,
  signal: AbortSignal,
) {
  return parsePage(
    await request(workspace, path + query(after), "GET", undefined, signal),
    parse,
    after,
  );
}

export const threadDeliveryClient = {
  tickets(
    workspace: string,
    thread: string,
    after: string | undefined,
    signal: AbortSignal,
  ) {
    return deliveryPage(
      workspace,
      ticketPath(thread),
      parseTicket,
      after,
      signal,
    );
  },
  runs(
    workspace: string,
    thread: string,
    ticket: string,
    after: string | undefined,
    signal: AbortSignal,
  ) {
    return deliveryPage(
      workspace,
      runPath(thread, ticket),
      parseBuildCandidate,
      after,
      signal,
    );
  },
  deployments(
    workspace: string,
    thread: string,
    ticket: string,
    run: string,
    after: string | undefined,
    signal: AbortSignal,
  ) {
    const path = `${runPath(thread, ticket)}/${encodeURIComponent(requireID(run, "cir_"))}/deployments`;
    return deliveryPage(workspace, path, parseTraceDeployment, after, signal);
  },
};
