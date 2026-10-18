import { AuthRequestError } from "../auth/api";
import { object, requireID } from "./api";
import {
  configurationClient,
  parsePage,
  query,
  request,
  text,
} from "./configuration-api";

export interface ProjectDecision {
  id: string;
  title: string;
  status: "proposed" | "accepted";
}

export function projectDecisionsPath(workspaceID: string, projectID: string) {
  requireID(workspaceID, "wrk_");
  requireID(projectID, "prj_");
  return `/workspaces/${encodeURIComponent(workspaceID)}/projects/${encodeURIComponent(projectID)}/decisions`;
}

export function projectDecisionsLocation(pathname: string) {
  const match =
    /^\/workspaces\/([^/]+)\/projects\/([^/]+)\/decisions\/?$/u.exec(pathname);
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

export const projectDecisionsClient = {
  project: (workspaceID: string, projectID: string, signal?: AbortSignal) =>
    configurationClient.read(workspaceID, "project", projectID, signal),
  async decisions(
    workspaceID: string,
    projectID: string,
    after?: string,
    signal?: AbortSignal,
  ) {
    requireID(projectID, "prj_");
    return parsePage<ProjectDecision>(
      await request(
        workspaceID,
        `/projects/${encodeURIComponent(projectID)}/decisions${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (value) => {
        const row = object(value, ["ref", "title", "status"]);
        const ref = object(row.ref, ["type", "id"]);
        if (ref.type !== "decision")
          throw new AuthRequestError("决策服务响应不符合当前契约。");
        if (row.status !== "proposed" && row.status !== "accepted")
          throw new AuthRequestError("决策服务响应不符合当前契约。");
        const title = text(row.title);
        // JSON may contain unpaired UTF-16 surrogates, which cannot represent
        // the server's required valid UTF-8 question text.
        for (const character of title) {
          const code = character.codePointAt(0)!;
          if (code >= 0xd800 && code <= 0xdfff)
            throw new AuthRequestError("决策服务响应不符合当前契约。");
        }
        return {
          id: requireID(ref.id, "dec_"),
          title,
          status: row.status,
        };
      },
      after,
    );
  },
};

export type ProjectDecisionsClient = typeof projectDecisionsClient;
