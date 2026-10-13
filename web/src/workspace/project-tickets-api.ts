import { AuthRequestError } from "../auth/api";
import { object, requireID } from "./api";
import {
  configurationClient,
  parsePage,
  query,
  request,
  text,
} from "./configuration-api";

export interface ProjectTicket {
  id: string;
  title: string;
  status: "open";
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
  ) {
    requireID(projectID, "prj_");
    return parsePage<ProjectTicket>(
      await request(
        workspaceID,
        `/projects/${encodeURIComponent(projectID)}/tickets${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (value) => {
        const row = object(value, ["ref", "title", "status"]);
        const ref = object(row.ref, ["type", "id"]);
        if (ref.type !== "ticket" || row.status !== "open")
          throw new AuthRequestError("工单服务响应不符合当前契约。");
        return {
          id: requireID(ref.id, "tkt_"),
          title: text(row.title),
          status: "open",
        };
      },
      after,
    );
  },
};

export type ProjectTicketsClient = typeof projectTicketsClient;
