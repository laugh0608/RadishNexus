import { AuthRequestError } from "../auth/api";
import { object, requireID, validCursor } from "../workspace/api";
import { request, text } from "../workspace/configuration-api";
import {
  parseTicketStatusRevision,
  maxTicketStatusRevision,
} from "./ticket-status";

export interface TicketAssignee {
  id: string;
  displayName: string;
  eligible: boolean;
  imported: boolean;
}
export interface Assignment {
  assignee: TicketAssignee | null;
  revision: number;
  statusRevision: number;
  canAssign: boolean;
  projectRevision: string;
}
export interface AssigneeCandidate {
  id: string;
  displayName: string;
  lifecycleRevision: string;
}
export interface AssignmentInput {
  client_operation_id: string;
  base_assignment_revision: number;
  base_status_revision: number;
  assignee_user_id: string | null;
  target_lifecycle_revision: string;
  project_management_revision: string;
  confirmed: true;
}
const invalid = () =>
  new AuthRequestError("负责人服务响应不符合当前契约，请重新读取。");
function version(value: unknown): string {
  if (
    typeof value !== "string" ||
    !/^[1-9][0-9]*$/u.test(value) ||
    value.length > 19 ||
    BigInt(value) > 9223372036854775807n
  )
    throw invalid();
  return value;
}
export function parseAssignee(value: unknown): TicketAssignee | null {
  if (value === null) return null;
  const v = object(value, ["id", "display_name", "eligible", "imported"]);
  if (
    typeof v.eligible !== "boolean" ||
    typeof v.imported !== "boolean" ||
    (v.imported && v.eligible)
  )
    throw invalid();
  return {
    id: requireID(v.id, "usr_"),
    displayName: text(v.display_name),
    eligible: v.eligible,
    imported: v.imported,
  };
}
export function assigneeLabel(v: TicketAssignee | null): string {
  return v === null
    ? "未分配"
    : `${v.displayName}${v.imported ? " · 历史负责人，待重新分配" : v.eligible ? "" : " · 当前不可执行"}`;
}
function path(ticket: string) {
  return `/tickets/${encodeURIComponent(requireID(ticket, "tkt_"))}/assignee`;
}
export const ticketAssignmentClient = {
  async read(
    workspace: string,
    ticket: string,
    signal?: AbortSignal,
  ): Promise<Assignment> {
    const v = object(
      await request(workspace, path(ticket), "GET", undefined, signal),
      [
        "assignee",
        "assignment_revision",
        "status_revision",
        "can_assign",
        "project_management_revision",
      ],
    );
    const revision = parseTicketStatusRevision(v.assignment_revision);
    if (
      typeof v.can_assign !== "boolean" ||
      (v.can_assign && revision === maxTicketStatusRevision)
    )
      throw invalid();
    return {
      assignee: parseAssignee(v.assignee),
      revision,
      statusRevision: parseTicketStatusRevision(v.status_revision),
      canAssign: v.can_assign,
      projectRevision: version(v.project_management_revision),
    };
  },
  async candidates(
    workspace: string,
    ticket: string,
    after?: string,
    signal?: AbortSignal,
  ) {
    if (after && !validCursor(after)) throw invalid();
    const v = object(
      await request(
        workspace,
        `${path(ticket)}/candidates?limit=25${after ? `&after=${encodeURIComponent(after)}` : ""}`,
        "GET",
        undefined,
        signal,
      ),
      ["items", "next_cursor", "project_management_revision", "self_id"],
    );
    if (
      !Array.isArray(v.items) ||
      v.items.length > 25 ||
      (v.next_cursor !== null &&
        (!validCursor(v.next_cursor) ||
          v.next_cursor === after ||
          v.items.length !== 25))
    )
      throw invalid();
    let previous = "";
    const items = v.items.map((value): AssigneeCandidate => {
      const row = object(value, ["id", "display_name", "lifecycle_revision"]);
      const id = requireID(row.id, "usr_");
      if (id <= previous) throw invalid();
      previous = id;
      return {
        id,
        displayName: text(row.display_name),
        lifecycleRevision: version(row.lifecycle_revision),
      };
    });
    return {
      items,
      nextCursor: v.next_cursor as string | null,
      projectRevision: version(v.project_management_revision),
      selfID: requireID(v.self_id, "usr_"),
    };
  },
  async set(
    workspace: string,
    ticket: string,
    input: AssignmentInput,
    signal?: AbortSignal,
  ) {
    const v = object(
      await request(workspace, path(ticket), "POST", { ...input }, signal),
      ["applied", "applied_assignment_revision", "applied_assignee_user_id"],
    );
    const id =
      v.applied_assignee_user_id === null
        ? null
        : requireID(v.applied_assignee_user_id, "usr_");
    const revision = parseTicketStatusRevision(v.applied_assignment_revision);
    if (
      v.applied !== true ||
      revision !== input.base_assignment_revision + 1 ||
      id !== input.assignee_user_id
    )
      throw invalid();
    return { revision, assigneeID: id };
  },
};
export type TicketAssignmentClient = typeof ticketAssignmentClient;
