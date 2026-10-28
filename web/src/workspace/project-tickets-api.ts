import {
  parseTicketPriority,
  type TicketPriority,
} from "../collaboration/ticket-priority";
import {
  parseAssignee,
  type TicketAssignee,
} from "../collaboration/ticket-assignee-api";
import {
  parseTicketStatus,
  type TicketStatus,
} from "../collaboration/ticket-status";
import { AuthRequestError } from "../auth/api";
import { object, requireID } from "./api";
import {
  configurationClient,
  parsePage,
  query,
  request,
  text,
} from "./configuration-api";

export interface TicketFilters {
  status?: "active" | TicketStatus;
  assignee?: "me" | "unassigned";
  priority?: TicketPriority;
}
function filterQuery(filters: TicketFilters) {
  const parts: string[] = [];
  for (const [key, value] of Object.entries(filters)) {
    if (value === undefined) continue;
    if (
      (key === "status" && (value === "active" || parseTicketStatus(value))) ||
      (key === "assignee" && (value === "me" || value === "unassigned")) ||
      (key === "priority" && parseTicketPriority(value))
    )
      parts.push(`${key}=${encodeURIComponent(value)}`);
    else throw new AuthRequestError("工单筛选条件无效。");
  }
  return parts.length ? `&${parts.join("&")}` : "";
}
export interface ProjectTicket {
  priority: TicketPriority;
  assignee: TicketAssignee | null;
  id: string;
  title: string;
  status: TicketStatus;
}

export function projectTicketsPath(workspaceID: string, projectID: string) {
  requireID(workspaceID, "wrk_");
  requireID(projectID, "prj_");
  return `/workspaces/${encodeURIComponent(workspaceID)}/projects/${encodeURIComponent(projectID)}/tickets`;
}

export function projectTicketsLocation(pathname: string) {
  const match = /^\/workspaces\/([^/]+)\/projects\/([^/]+)\/tickets\/?$/u.exec(
    pathname,
  );
  if (!match) return null;
  try {
    return {
      workspaceID: requireID(decodeURIComponent(match[1]!), "wrk_"),
      projectID: requireID(decodeURIComponent(match[2]!), "prj_"),
    };
  } catch {
    return null;
  }
}

export const projectTicketsClient = {
  project: (workspaceID: string, projectID: string, signal?: AbortSignal) =>
    configurationClient.read(workspaceID, "project", projectID, signal),
  async tickets(
    workspaceID: string,
    projectID: string,
    after?: string,
    signal?: AbortSignal,
    filters: TicketFilters = {},
  ) {
    requireID(projectID, "prj_");
    return parsePage<ProjectTicket>(
      await request(
        workspaceID,
        `/projects/${encodeURIComponent(projectID)}/tickets${query(after)}${filterQuery(filters)}`,
        "GET",
        undefined,
        signal,
      ),
      (value) => {
        const row = object(value, [
          "ref",
          "title",
          "status",
          "assignee",
          "priority",
        ]);
        const ref = object(row.ref, ["type", "id"]);
        if (ref.type !== "ticket")
          throw new AuthRequestError("工单服务响应不符合当前契约。");
        return {
          id: requireID(ref.id, "tkt_"),
          assignee: parseAssignee(row.assignee),
          priority: parseTicketPriority(row.priority),
          title: text(row.title),
          status: parseTicketStatus(row.status),
        };
      },
      after,
    );
  },
};

export type ProjectTicketsClient = typeof projectTicketsClient;
