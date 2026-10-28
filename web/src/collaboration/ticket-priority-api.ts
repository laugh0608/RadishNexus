import { AuthRequestError } from "../auth/api";
import { object, requireID } from "../workspace/api";
import { request } from "../workspace/configuration-api";
import {
  maxTicketStatusRevision,
  parseTicketStatusRevision,
} from "./ticket-status";
import { parseTicketPriority, type TicketPriority } from "./ticket-priority";

export interface Priority {
  priority: TicketPriority;
  revision: number;
  statusRevision: number;
  canSetPriority: boolean;
}
export interface PriorityInput {
  client_operation_id: string;
  base_priority_revision: number;
  base_status_revision: number;
  priority: TicketPriority;
  confirmed: true;
}
const invalid = () =>
  new AuthRequestError("优先级服务响应不符合当前契约，请重新读取。");
function path(ticket: string) {
  return `/tickets/${encodeURIComponent(requireID(ticket, "tkt_"))}/priority`;
}
export const ticketPriorityClient = {
  async read(
    workspace: string,
    ticket: string,
    signal?: AbortSignal,
  ): Promise<Priority> {
    const v = object(
      await request(workspace, path(ticket), "GET", undefined, signal),
      ["priority", "priority_revision", "status_revision", "can_set_priority"],
    );
    const revision = parseTicketStatusRevision(v.priority_revision);
    if (
      typeof v.can_set_priority !== "boolean" ||
      (v.can_set_priority && revision === maxTicketStatusRevision)
    )
      throw invalid();
    return {
      priority: parseTicketPriority(v.priority),
      revision,
      statusRevision: parseTicketStatusRevision(v.status_revision),
      canSetPriority: v.can_set_priority,
    };
  },
  async set(
    workspace: string,
    ticket: string,
    input: PriorityInput,
    signal?: AbortSignal,
  ) {
    const v = object(
      await request(workspace, path(ticket), "POST", { ...input }, signal),
      ["ticket", "applied", "applied_priority", "applied_priority_revision"],
    );
    const ref = object(v.ticket, ["type", "id"]);
    const priority = parseTicketPriority(v.applied_priority),
      revision = parseTicketStatusRevision(v.applied_priority_revision);
    if (
      ref.type !== "ticket" ||
      requireID(ref.id, "tkt_") !== ticket ||
      v.applied !== true ||
      revision !== input.base_priority_revision + 1 ||
      priority !== input.priority
    )
      throw invalid();
    return { priority, revision };
  },
};
export type TicketPriorityClient = typeof ticketPriorityClient;
