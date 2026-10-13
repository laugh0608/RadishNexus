import { object, requireID } from "./api";
import { parsePage, query, request } from "./configuration-api";
import { parseCIRunTime, parseGitRevision } from "../nexus-view/ci-run-api";
import { validPathID } from "../nexus-view/api";
import type { CIRunStatus } from "../nexus-view/model";

export function repositoryPagePath(
  workspace: string,
  repository: string,
): string {
  return `/workspaces/${encodeURIComponent(requireID(workspace, "wrk_"))}/repositories/${encodeURIComponent(requireID(repository, "rep_"))}`;
}
export function repositoryLocation(
  path: string,
): { workspaceID: string; repositoryID: string } | null {
  const m = /^\/workspaces\/([^/]+)\/repositories\/([^/]+)\/?$/u.exec(path);
  if (!m) return null;
  try {
    const workspaceID = decodeURIComponent(m[1] ?? ""),
      repositoryID = decodeURIComponent(m[2] ?? "");
    return validPathID(workspaceID, "wrk_") && validPathID(repositoryID, "rep_")
      ? { workspaceID, repositoryID }
      : null;
  } catch {
    return null;
  }
}
export async function loadRepositoryCIRuns(
  workspace: string,
  repository: string,
  after?: string,
  signal?: AbortSignal,
) {
  requireID(repository, "rep_");
  return parsePage(
    await request(
      workspace,
      `/repositories/${encodeURIComponent(repository)}/ci-runs${query(after)}`,
      "GET",
      undefined,
      signal,
    ),
    (value) => {
      const row = object(value, ["ref", "status", "completed_at", "revision"]);
      const ref = object(row.ref, ["type", "id"]);
      if (
        ref.type !== "ci-run" ||
        (row.status !== "succeeded" &&
          row.status !== "failed" &&
          row.status !== "canceled")
      )
        throw new TypeError("invalid Repository CI Run");
      return {
        id: requireID(ref.id, "cir_"),
        status: row.status as CIRunStatus,
        completedAt: parseCIRunTime(row.completed_at),
        revision: parseGitRevision(row.revision),
      };
    },
    after,
  );
}
