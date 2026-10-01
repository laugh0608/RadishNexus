import { AuthRequestError, csrfTokenFromCookie } from "../auth/api";
import { validPathID } from "./api";

export type StagingTarget = {
  ref: { type: "environment"; id: string };
  name: string;
  key: string;
  classification: "staging";
};
export type StagingTargets = {
  items: StagingTarget[];
  next_cursor: string | null;
};
export type StagingInput = {
  client_operation_id: string;
  environment_id: string;
  status: "succeeded" | "failed" | "canceled";
  started_at: string | null;
  completed_at: string;
  confirmed: true;
};
export type StagingResult = {
  deployment: { type: "deployment"; id: string };
  duplicate: boolean;
};
export type StagingClient = {
  targets(
    workspace: string,
    ciRun: string,
    after?: string,
    signal?: AbortSignal,
  ): Promise<StagingTargets>;
  record(
    workspace: string,
    ciRun: string,
    input: StagingInput,
  ): Promise<StagingResult>;
};
function fields(v: unknown, names: string[]): Record<string, unknown> {
  if (!v || typeof v !== "object" || Array.isArray(v))
    throw new TypeError("expected object");
  const r = v as Record<string, unknown>;
  if (Object.keys(r).length !== names.length || names.some((n) => !(n in r)))
    throw new TypeError("unexpected fields");
  return r;
}
function ref(
  v: unknown,
  type: "environment" | "deployment",
  prefix: string,
): string {
  const r = fields(v, ["type", "id"]);
  if (r.type !== type || typeof r.id !== "string" || !validPathID(r.id, prefix))
    throw new TypeError("invalid ref");
  return r.id;
}
export function parseStagingTargets(v: unknown): StagingTargets {
  const r = fields(fields(v, ["data"]).data, ["items", "next_cursor"]);
  if (
    !Array.isArray(r.items) ||
    r.items.length > 50 ||
    (r.next_cursor !== null &&
      (typeof r.next_cursor !== "string" ||
        !r.next_cursor ||
        r.next_cursor.length > 1024))
  )
    throw new TypeError("invalid page");
  let previous = "";
  const items = r.items.map((value: unknown) => {
    const t = fields(value, ["ref", "name", "key", "classification"]),
      id = ref(t.ref, "environment", "env_");
    if (
      id <= previous ||
      typeof t.name !== "string" ||
      !t.name.trim() ||
      typeof t.key !== "string" ||
      !t.key.trim() ||
      t.classification !== "staging"
    )
      throw new TypeError("invalid target");
    previous = id;
    return {
      ref: { type: "environment" as const, id },
      name: t.name,
      key: t.key,
      classification: "staging" as const,
    };
  });
  if (r.next_cursor !== null && items.length === 0)
    throw new TypeError("empty continuation");
  return { items, next_cursor: r.next_cursor as string | null };
}
export function parseStagingResult(v: unknown): StagingResult {
  const r = fields(fields(v, ["data"]).data, ["deployment", "duplicate"]),
    id = ref(r.deployment, "deployment", "dpl_");
  if (typeof r.duplicate !== "boolean")
    throw new TypeError("invalid duplicate");
  return { deployment: { type: "deployment", id }, duplicate: r.duplicate };
}
function path(workspace: string, ciRun: string) {
  if (!validPathID(workspace, "wrk_") || !validPathID(ciRun, "cir_"))
    throw new AuthRequestError("构建地址无效。");
  return `/api/v1/workspaces/${encodeURIComponent(workspace)}/ci-runs/${encodeURIComponent(ciRun)}`;
}
async function request(
  url: string,
  input?: StagingInput,
  signal?: AbortSignal,
): Promise<unknown> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (input) {
    const csrf = csrfTokenFromCookie(document.cookie);
    if (!csrf)
      throw new AuthRequestError("会话校验不可用，请重新登录。", {
        status: 401,
      });
    headers["Content-Type"] = "application/json";
    headers["X-CSRF-Token"] = csrf;
  }
  let response: Response;
  try {
    response = await fetch(url, {
      method: input ? "POST" : "GET",
      headers,
      credentials: "same-origin",
      cache: "no-store",
      body: input ? JSON.stringify(input) : undefined,
      signal,
    });
  } catch (error) {
    if (signal?.aborted) throw error;
    throw new AuthRequestError(
      input
        ? "未能确认记录结果；请保留本次请求并重试。"
        : "无法读取可选环境，请重试。",
    );
  }
  if (!response.ok)
    throw new AuthRequestError(
      response.status === 401
        ? "会话已失效，请重新登录。"
        : response.status === 403
          ? "当前没有记录该环境的权限，或会话校验失败。"
          : response.status === 404
            ? "构建或环境不可用。"
            : response.status === 409
              ? "记录冲突：该构建可能已有环境记录，或当前目标状态不允许记录。"
              : response.status === 400
                ? "记录内容无效，请检查确认信息与时间。"
                : "未能确认记录结果，请重试。",
      { status: response.status },
    );
  if (
    !response.headers
      .get("Content-Type")
      ?.toLowerCase()
      .includes("application/json")
  )
    throw new AuthRequestError("服务响应格式不正确，尚不能确认结果。");
  try {
    return await response.json();
  } catch {
    throw new AuthRequestError("服务响应格式不正确，尚不能确认结果。");
  }
}
export const stagingClient: StagingClient = {
  async targets(workspace, ciRun, after, signal) {
    const value = await request(
      path(workspace, ciRun) +
        "/staging-targets" +
        (after ? `?after=${encodeURIComponent(after)}` : ""),
      undefined,
      signal,
    );
    try {
      return parseStagingTargets(value);
    } catch {
      throw new AuthRequestError("环境列表不符合当前协议，请重新读取。");
    }
  },
  async record(workspace, ciRun, input) {
    const value = await request(
      path(workspace, ciRun) + "/staging-deployments",
      input,
    );
    try {
      return parseStagingResult(value);
    } catch {
      throw new AuthRequestError(
        "记录响应不符合当前协议，尚不能确认结果；请重试原请求。",
      );
    }
  },
};
