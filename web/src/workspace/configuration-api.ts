import { AuthRequestError, csrfTokenFromCookie } from "../auth/api";
import { object, requireID, validCursor } from "./api";

export type ProjectRole = "viewer" | "contributor" | "decider" | "admin";
export interface ConfigurationObject {
  id: string;
  kind: "project" | "channel";
  name: string;
  key?: string;
  projectID?: string;
  visibility: "workspace" | "project" | "restricted";
  status: "active" | "archived";
  canManage: boolean;
}
export interface ConfigurationMember {
  id: string;
  name: string;
  role?: ProjectRole;
  eligible: boolean;
}
export interface Team {
  id: string;
  name: string;
}
export interface ConfigurationPage<T> {
  items: readonly T[];
  nextCursor: string | null;
}

const error = () =>
  new AuthRequestError("配置服务响应不符合当前契约，请刷新或联系管理员。");
function text(value: unknown): string {
  if (typeof value !== "string" || !value.trim() || value.includes("\0"))
    throw error();
  return value;
}
function parseObject(
  value: unknown,
  kind: "project" | "channel",
): ConfigurationObject {
  const row = object(value, [
    "ref",
    "name",
    "visibility",
    "status",
    "capabilities",
    kind === "project" ? "key" : "project",
  ]);
  const ref = object(row.ref, ["type", "id"]);
  if (ref.type !== kind) throw error();
  const capabilities = object(row.capabilities, ["manage"]);
  if (
    row.visibility !== "workspace" &&
    row.visibility !== "project" &&
    row.visibility !== "restricted"
  )
    throw error();
  if (
    typeof capabilities.manage !== "boolean" ||
    (row.status !== "active" && row.status !== "archived") ||
    (row.visibility !== "restricted" &&
      row.visibility !== (kind === "project" ? "workspace" : "project"))
  )
    throw error();
  const result: ConfigurationObject = {
    id: requireID(ref.id, kind === "project" ? "prj_" : "chn_"),
    kind,
    name: text(row.name),
    status: row.status,
    visibility: row.visibility,
    canManage: capabilities.manage,
  };
  if (kind === "project") result.key = text(row.key);
  else {
    const project = object(row.project, ["type", "id"]);
    if (project.type !== "project") throw error();
    result.projectID = requireID(project.id, "prj_");
  }
  return result;
}
async function request(
  workspace: string,
  suffix: string,
  method = "GET",
  body?: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<unknown> {
  requireID(workspace, "wrk_");
  const headers: Record<string, string> = { Accept: "application/json" };
  if (method !== "GET") {
    headers["Content-Type"] = "application/json";
    const csrf = csrfTokenFromCookie(document.cookie);
    if (!csrf)
      throw new AuthRequestError("会话验证信息已失效，请重新登录。", {
        status: 401,
      });
    headers["X-CSRF-Token"] = csrf;
  }
  let response: Response;
  try {
    response = await fetch(
      `/api/v1/workspaces/${encodeURIComponent(workspace)}${suffix}`,
      {
        method,
        credentials: "same-origin",
        cache: "no-store",
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        signal,
      },
    );
  } catch (cause) {
    if (signal?.aborted) throw cause;
    throw new AuthRequestError("请求结果尚未确认，可保持原内容重试。");
  }
  if (!response.ok)
    throw new AuthRequestError(
      response.status === 401
        ? "会话已失效，请重新登录。"
        : response.status === 403
          ? "当前没有此项配置权限。"
          : response.status === 404
            ? "对象或成员已不可访问，请刷新。"
            : response.status === 409
              ? "配置已变化或请求存在冲突，请刷新后重新操作。"
              : "配置未完成，请检查输入后重试。",
      { status: response.status },
    );
  if (!response.headers.get("Content-Type")?.includes("application/json"))
    throw error();
  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    throw error();
  }
  return object(payload, ["data"]).data;
}
function query(after?: string) {
  if (after !== undefined && !validCursor(after)) throw error();
  const q = new URLSearchParams({ limit: "25" });
  if (after) q.set("after", after);
  return `?${q}`;
}
function parsePage<T extends { id: string }>(
  value: unknown,
  parse: (value: unknown) => T,
  after?: string,
): ConfigurationPage<T> {
  const data = object(value, ["items", "next_cursor"]);
  if (!Array.isArray(data.items) || data.items.length > 25) throw error();
  const items = data.items.map(parse);
  if (items.some((item, index) => index > 0 && item.id <= items[index - 1]!.id))
    throw error();
  const next = data.next_cursor;
  if (
    next !== null &&
    (!validCursor(next) || items.length !== 25 || next === after)
  )
    throw error();
  return { items, nextCursor: next };
}
export const configurationClient = {
  async teams(
    workspace: string,
    after?: string,
    signal?: AbortSignal,
  ): Promise<ConfigurationPage<Team>> {
    return parsePage(
      await request(
        workspace,
        `/teams${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (v) => {
        const row = object(v, ["id", "name"]);
        return { id: requireID(row.id, "tem_"), name: text(row.name) };
      },
      after,
    );
  },
  async members(
    workspace: string,
    kind: "workspace" | "project" | "channel",
    scope: string,
    after?: string,
    signal?: AbortSignal,
  ): Promise<ConfigurationPage<ConfigurationMember>> {
    if (kind !== "workspace")
      requireID(scope, kind === "project" ? "prj_" : "chn_");
    const suffix =
      kind === "workspace"
        ? "/members"
        : `/${kind}s/${encodeURIComponent(scope)}/members`;
    return parsePage(
      await request(
        workspace,
        `${suffix}${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (v) => {
        const row = object(
          v,
          kind === "workspace"
            ? ["id", "display_name"]
            : kind === "project"
              ? ["user", "role", "eligible"]
              : ["user", "eligible"],
        );
        const user =
          kind === "workspace" ? row : object(row.user, ["id", "display_name"]);
        if (kind !== "workspace" && typeof row.eligible !== "boolean")
          throw error();
        const member: ConfigurationMember = {
          id: requireID(user.id, "usr_"),
          name: text(user.display_name),
          eligible: kind === "workspace" || row.eligible === true,
        };
        if (kind === "project") {
          if (
            row.role !== "viewer" &&
            row.role !== "contributor" &&
            row.role !== "decider" &&
            row.role !== "admin"
          )
            throw error();
          member.role = row.role;
        }
        return member;
      },
      after,
    );
  },
  async read(
    workspace: string,
    kind: "project" | "channel",
    id: string,
    signal?: AbortSignal,
  ) {
    requireID(id, kind === "project" ? "prj_" : "chn_");
    const result = parseObject(
      await request(
        workspace,
        `/${kind}s/${encodeURIComponent(id)}/configuration`,
        "GET",
        undefined,
        signal,
      ),
      kind,
    );
    if (result.id !== id) throw error();
    return result;
  },
  async createTeam(workspace: string, body: Record<string, unknown>) {
    const row = object(await request(workspace, "/teams", "POST", body), [
      "id",
      "name",
    ]);
    return { id: requireID(row.id, "tem_"), name: text(row.name) };
  },
  async createProject(workspace: string, body: Record<string, unknown>) {
    return parseObject(
      await request(workspace, "/projects", "POST", body),
      "project",
    );
  },
  async createChannel(
    workspace: string,
    project: string,
    body: Record<string, unknown>,
  ) {
    requireID(project, "prj_");
    const result = parseObject(
      await request(
        workspace,
        `/projects/${encodeURIComponent(project)}/channels`,
        "POST",
        body,
      ),
      "channel",
    );
    if (result.projectID !== project) throw error();
    return result;
  },
  async member(
    workspace: string,
    kind: "project" | "channel",
    scope: string,
    user: string,
    method: "PUT" | "DELETE",
    body: Record<string, unknown>,
  ) {
    requireID(scope, kind === "project" ? "prj_" : "chn_");
    requireID(user, "usr_");
    const row = object(
      await request(
        workspace,
        `/${kind}s/${encodeURIComponent(scope)}/members/${encodeURIComponent(user)}`,
        method,
        body,
      ),
      ["user_id", "applied"],
    );
    if (row.user_id !== user || row.applied !== true) throw error();
  },
};
