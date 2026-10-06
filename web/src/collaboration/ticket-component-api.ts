import { AuthRequestError } from "../auth/api";
import { object, requireID } from "../workspace/api";
import {
  request,
  text,
  query,
  parsePage,
} from "../workspace/configuration-api";
import { parseDeliveryObject } from "../workspace/delivery-configuration-api";

export interface TicketSummary {
  id: string;
  title: string;
  projectID: string;
  status: "open";
}
export interface TicketComponentAssociation<T> {
  id: string;
  linkID: string;
  target: T;
  canUnlink: boolean;
}
const invalid = () =>
  new AuthRequestError("事项与组件关联响应不符合当前契约，请刷新后重试。");
function association<T extends { id: string }>(
  value: unknown,
  parse: (v: unknown) => T,
): TicketComponentAssociation<T> {
  const row = object(value, ["link_id", "target", "can_unlink"]);
  if (typeof row.can_unlink !== "boolean") throw invalid();
  const target = parse(row.target);
  return {
    id: target.id,
    linkID: requireID(row.link_id, "lnk_"),
    target,
    canUnlink: row.can_unlink,
  };
}
function ticket(value: unknown): TicketSummary {
  const row = object(value, ["ref", "title", "project", "status"]),
    ref = object(row.ref, ["type", "id"]),
    project = object(row.project, ["type", "id"]);
  if (
    ref.type !== "ticket" ||
    project.type !== "project" ||
    row.status !== "open"
  )
    throw invalid();
  return {
    id: requireID(ref.id, "tkt_"),
    title: text(row.title),
    projectID: requireID(project.id, "prj_"),
    status: "open",
  };
}
function ticketPath(id: string) {
  return `/tickets/${encodeURIComponent(requireID(id, "tkt_"))}`;
}
function applied(value: unknown, expected?: string) {
  const row = object(value, ["link_id", "applied"]),
    id = requireID(row.link_id, "lnk_");
  if (row.applied !== true || (expected !== undefined && expected !== id))
    throw invalid();
  return id;
}
export function componentPagePath(workspace: string, id: string) {
  return `/workspaces/${encodeURIComponent(requireID(workspace, "wrk_"))}/components/${encodeURIComponent(requireID(id, "cmp_"))}`;
}
export function componentLocation(
  path: string,
): { workspaceID: string; componentID: string } | null {
  const m = /^\/workspaces\/([^/]+)\/components\/([^/]+)\/?$/u.exec(path);
  if (!m) return null;
  try {
    return {
      workspaceID: requireID(decodeURIComponent(m[1]!), "wrk_"),
      componentID: requireID(decodeURIComponent(m[2]!), "cmp_"),
    };
  } catch {
    return null;
  }
}
export const ticketComponentClient = {
  async components(
    workspace: string,
    id: string,
    after?: string,
    signal?: AbortSignal,
  ) {
    const row = object(
      await request(
        workspace,
        `${ticketPath(id)}/components${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      ["items", "next_cursor", "capabilities"],
    );
    const cap = object(row.capabilities, ["can_link"]);
    if (typeof cap.can_link !== "boolean") throw invalid();
    const page = parsePage(
      { items: row.items, next_cursor: row.next_cursor },
      (v) => association(v, (c) => parseDeliveryObject(c, "component", false)),
      after,
    );
    if (page.items.some((l) => l.canUnlink !== cap.can_link)) throw invalid();
    return { ...page, canLink: cap.can_link };
  },
  async tickets(
    workspace: string,
    id: string,
    after?: string,
    signal?: AbortSignal,
  ) {
    return parsePage(
      await request(
        workspace,
        `/components/${encodeURIComponent(requireID(id, "cmp_"))}/tickets${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (v) => association(v, ticket),
      after,
    );
  },
  async link(workspace: string, id: string, body: Record<string, unknown>) {
    return applied(
      await request(workspace, `${ticketPath(id)}/components`, "POST", body),
    );
  },
  async unlink(
    workspace: string,
    id: string,
    linkID: string,
    body: Record<string, unknown>,
  ) {
    return applied(
      await request(
        workspace,
        `${ticketPath(id)}/component-links/${encodeURIComponent(requireID(linkID, "lnk_"))}`,
        "DELETE",
        body,
      ),
      linkID,
    );
  },
};
export type TicketComponentClient = typeof ticketComponentClient;
