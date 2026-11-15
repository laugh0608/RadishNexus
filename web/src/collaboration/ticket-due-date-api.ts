import { AuthRequestError } from "../auth/api";
import { object, requireID } from "../workspace/api";
import { request } from "../workspace/configuration-api";
import {
  maxTicketStatusRevision,
  parseTicketStatusRevision,
} from "./ticket-status";
import { parseTicketDueDate } from "./ticket-due-date";

export interface DueDate {
  dueDate: string | null;
  revision: number;
  statusRevision: number;
  canSetDueDate: boolean;
}
export interface DueDateInput {
  client_operation_id: string;
  base_due_date_revision: number;
  base_status_revision: number;
  due_date: string | null;
  confirmed: true;
}
const invalid = () =>
  new AuthRequestError("截止日期服务响应不符合当前契约，请重新读取。");
function path(ticket: string) {
  return `/tickets/${encodeURIComponent(requireID(ticket, "tkt_"))}/due-date`;
}
export const ticketDueDateClient = {
  async read(
    workspace: string,
    ticket: string,
    signal?: AbortSignal,
  ): Promise<DueDate> {
    const v = object(
      await request(workspace, path(ticket), "GET", undefined, signal),
      ["due_date", "due_date_revision", "status_revision", "can_set_due_date"],
    );
    const revision = parseTicketStatusRevision(v.due_date_revision);
    if (
      typeof v.can_set_due_date !== "boolean" ||
      (v.can_set_due_date && revision === maxTicketStatusRevision)
    )
      throw invalid();
    return {
      dueDate: parseTicketDueDate(v.due_date),
      revision,
      statusRevision: parseTicketStatusRevision(v.status_revision),
      canSetDueDate: v.can_set_due_date,
    };
  },
  async set(
    workspace: string,
    ticket: string,
    input: DueDateInput,
    signal?: AbortSignal,
  ) {
    const v = object(
      await request(workspace, path(ticket), "POST", { ...input }, signal),
      ["ticket", "applied", "applied_due_date", "applied_due_date_revision"],
    );
    const ref = object(v.ticket, ["type", "id"]);
    const dueDate = parseTicketDueDate(v.applied_due_date),
      revision = parseTicketStatusRevision(v.applied_due_date_revision);
    if (
      ref.type !== "ticket" ||
      requireID(ref.id, "tkt_") !== ticket ||
      v.applied !== true ||
      revision !== input.base_due_date_revision + 1 ||
      dueDate !== input.due_date
    )
      throw invalid();
    return { dueDate, revision };
  },
};
export type TicketDueDateClient = typeof ticketDueDateClient;
