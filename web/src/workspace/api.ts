import { AuthRequestError } from "../auth/api";

export interface DiscoveryItem {
  id: string;
  title: string;
  status: "active" | "archived";
}
export interface DiscoveryPage {
  items: readonly DiscoveryItem[];
  nextCursor: string | null;
}
export interface DiscoveryClient {
  projects(
    workspaceID: string,
    after?: string,
    signal?: AbortSignal,
  ): Promise<DiscoveryPage>;
  channels(
    workspaceID: string,
    projectID: string,
    after?: string,
    signal?: AbortSignal,
  ): Promise<DiscoveryPage>;
}

export const browserDiscoveryClient: DiscoveryClient = {
  projects: (workspaceID, after, signal) => {
    requireID(workspaceID, "wrk_");
    return load(
      `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/projects`,
      "project",
      after,
      signal,
    );
  },
  channels: (workspaceID, projectID, after, signal) => {
    requireID(workspaceID, "wrk_");
    requireID(projectID, "prj_");
    return load(
      `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/projects/${encodeURIComponent(projectID)}/channels`,
      "channel",
      after,
      signal,
    );
  },
};

async function load(
  path: string,
  kind: "project" | "channel",
  after?: string,
  signal?: AbortSignal,
): Promise<DiscoveryPage> {
  const query = new URLSearchParams({ limit: "25" });
  if (after !== undefined) {
    if (!validCursor(after)) throw contractError();
    query.set("after", after);
  }
  let response: Response;
  try {
    response = await fetch(`${path}?${query}`, {
      method: "GET",
      credentials: "same-origin",
      cache: "no-store",
      headers: { Accept: "application/json" },
      signal,
    });
  } catch (error) {
    if (
      signal?.aborted ||
      (error instanceof DOMException && error.name === "AbortError")
    )
      throw error;
    throw new AuthRequestError("无法加载工作区内容，请稍后重试。");
  }
  if (!response.ok) {
    throw new AuthRequestError(
      response.status === 401
        ? "会话已失效，请重新登录。"
        : response.status === 404
          ? "该工作区或项目已不可访问，请刷新项目列表。"
          : "无法加载工作区内容，请稍后重试。",
      { status: response.status },
    );
  }
  if (!response.headers.get("Content-Type")?.includes("application/json"))
    throw contractError();
  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    throw contractError();
  }
  const data = object(object(payload, ["data"]).data, ["items", "next_cursor"]);
  if (!Array.isArray(data.items) || data.items.length > 25)
    throw contractError();
  const items = data.items.map((value: unknown): DiscoveryItem => {
    const item = object(value, ["ref", "title", "status"]);
    const ref = object(item.ref, ["type", "id"]);
    if (ref.type !== kind) throw contractError();
    const id = requireID(ref.id, kind === "project" ? "prj_" : "chn_");
    if (
      typeof item.title !== "string" ||
      !item.title.trim() ||
      item.title.includes("\0") ||
      (item.status !== "active" && item.status !== "archived")
    )
      throw contractError();
    return { id, title: item.title, status: item.status };
  });
  if (new Set(items.map((item) => item.id)).size !== items.length)
    throw contractError();
  const nextCursor = data.next_cursor;
  if (
    nextCursor !== null &&
    (!validCursor(nextCursor) || items.length !== 25 || nextCursor === after)
  )
    throw contractError();
  return { items, nextCursor };
}

function object(
  value: unknown,
  keys: readonly string[],
): Record<string, unknown> {
  if (
    typeof value !== "object" ||
    value === null ||
    Array.isArray(value) ||
    Object.keys(value).length !== keys.length ||
    keys.some((key) => !(key in value))
  )
    throw contractError();
  return value as Record<string, unknown>;
}
function requireID(value: unknown, prefix: string): string {
  if (
    typeof value !== "string" ||
    value.length > 128 ||
    !value.startsWith(prefix) ||
    !/^[A-Za-z0-9_-]+$/u.test(value.slice(prefix.length))
  )
    throw contractError();
  return value;
}
function validCursor(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length <= 1024 &&
    /^[A-Za-z0-9_-]+$/u.test(value)
  );
}
function contractError(): AuthRequestError {
  return new AuthRequestError("工作区服务响应不符合当前契约，请联系管理员。");
}
