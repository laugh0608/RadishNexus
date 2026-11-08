import { AuthRequestError } from "../auth/api";
import { parseTicketPriority } from "../collaboration/ticket-priority";
import { parseTicketStatus } from "../collaboration/ticket-status";
import { object, requireID } from "./api";
import { parsePage, query, request, text } from "./configuration-api";

export function myTicketsPath(workspaceID: string) {
  requireID(workspaceID, "wrk_");
  return `/workspaces/${encodeURIComponent(workspaceID)}/my-tickets`;
}

export function myTicketsLocation(pathname: string) {
  const match = /^\/workspaces\/([^/]+)\/my-tickets\/?$/u.exec(pathname);
  if (!match) return null;
  try {
    return { workspaceID: requireID(decodeURIComponent(match[1]!), "wrk_") };
  } catch {
    return null;
  }
}

export async function readMyTickets(
  workspaceID: string,
  after?: string,
  signal?: AbortSignal,
) {
  return parsePage(
    await request(
      workspaceID,
      `/my-tickets${query(after)}`,
      "GET",
      undefined,
      signal,
    ),
    (value) => {
      const row = object(value, [
        "ref",
        "title",
        "status",
        "priority",
        "project",
      ]);
      const ref = object(row.ref, ["type", "id"]);
      const project = object(row.project, ["id", "name"]);
      const status = parseTicketStatus(row.status);
      if (
        ref.type !== "ticket" ||
        (status !== "open" && status !== "in-progress")
      )
        throw new AuthRequestError("我的工单响应不符合当前契约。");
      return {
        id: requireID(ref.id, "tkt_"),
        title: text(row.title),
        status,
        priority: parseTicketPriority(row.priority),
        project: {
          id: requireID(project.id, "prj_"),
          name: text(project.name),
        },
      };
    },
    after,
  );
}
