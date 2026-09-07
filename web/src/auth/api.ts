export interface SessionUser {
  id: string;
  displayName: string;
}

export interface SessionWorkspace {
  id: string;
  name: string;
  role: "owner" | "member";
}

export interface SessionContext {
  user: SessionUser;
  workspaces: readonly SessionWorkspace[];
  expiresAt: string;
}

export interface LoginCredentials {
  loginName: string;
  password: string;
}

export interface AuthClient {
  resolveSession(signal?: AbortSignal): Promise<SessionContext>;
  login(
    credentials: LoginCredentials,
    signal?: AbortSignal,
  ): Promise<SessionContext>;
  logout(signal?: AbortSignal): Promise<void>;
}

export class AuthRequestError extends Error {
  readonly code?: string;
  readonly status?: number;
  readonly userMessage: string;

  constructor(
    userMessage: string,
    options?: { code?: string; status?: number; cause?: unknown },
  ) {
    super(userMessage, { cause: options?.cause });
    this.name = "AuthRequestError";
    this.code = options?.code;
    this.status = options?.status;
    this.userMessage = userMessage;
  }
}

export const browserAuthClient: AuthClient = {
  resolveSession: (signal) => requestSession("/api/v1/auth/session", signal),
  login: (credentials, signal) => login(credentials, signal),
  logout: (signal) => logout(signal),
};

async function login(
  credentials: LoginCredentials,
  signal?: AbortSignal,
): Promise<SessionContext> {
  let response: Response;
  try {
    response = await fetch("/api/v1/auth/sessions", {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json; charset=utf-8",
      },
      credentials: "same-origin",
      cache: "no-store",
      body: JSON.stringify({
        login_name: credentials.loginName,
        password: credentials.password,
      }),
      signal,
    });
  } catch (error) {
    throw networkError(error);
  }
  return sessionFromResponse(response);
}

async function requestSession(
  path: string,
  signal?: AbortSignal,
): Promise<SessionContext> {
  let response: Response;
  try {
    response = await fetch(path, {
      method: "GET",
      headers: { Accept: "application/json" },
      credentials: "same-origin",
      cache: "no-store",
      signal,
    });
  } catch (error) {
    throw networkError(error);
  }
  return sessionFromResponse(response);
}

async function logout(signal?: AbortSignal): Promise<void> {
  const csrfToken = csrfTokenFromCookie(document.cookie);
  if (csrfToken === null) {
    throw new AuthRequestError("安全校验失败，请刷新页面后重试。", {
      code: "csrf_failed",
      status: 403,
    });
  }

  let response: Response;
  try {
    response = await fetch("/api/v1/auth/session", {
      method: "DELETE",
      headers: {
        Accept: "application/json",
        "X-CSRF-Token": csrfToken,
      },
      credentials: "same-origin",
      cache: "no-store",
      signal,
    });
  } catch (error) {
    throw networkError(error);
  }
  if (response.status !== 204) {
    throw await responseError(response);
  }
}

async function sessionFromResponse(
  response: Response,
): Promise<SessionContext> {
  if (!response.ok) {
    throw await responseError(response);
  }
  const contentType = response.headers.get("Content-Type") ?? "";
  if (!contentType.toLowerCase().includes("application/json")) {
    throw new AuthRequestError("认证服务返回了无法识别的响应，请稍后重试。", {
      status: response.status,
    });
  }

  let payload: unknown;
  try {
    payload = await response.json();
  } catch (error) {
    throw new AuthRequestError("认证服务返回了无法解析的响应，请稍后重试。", {
      status: response.status,
      cause: error,
    });
  }
  try {
    return parseSessionContext(payload);
  } catch (error) {
    throw new AuthRequestError("认证服务响应不符合当前契约，请稍后重试。", {
      status: response.status,
      cause: error,
    });
  }
}

export function parseSessionContext(payload: unknown): SessionContext {
  const session = record(payload, "session");
  const user = record(session.user, "session.user");
  const workspaces = array(session.workspaces, "session.workspaces").map(
    (value, index): SessionWorkspace => {
      const workspace = record(value, `session.workspaces[${index}]`);
      const role = string(workspace.role, `session.workspaces[${index}].role`);
      if (role !== "owner" && role !== "member") {
        throw new TypeError(`session.workspaces[${index}].role is invalid`);
      }
      return {
        id: scopedID(workspace.id, `session.workspaces[${index}].id`, "wrk_"),
        name: nonEmptyString(
          workspace.name,
          `session.workspaces[${index}].name`,
        ),
        role,
      };
    },
  );
  if (
    new Set(workspaces.map((workspace) => workspace.id)).size !==
    workspaces.length
  ) {
    throw new TypeError("session.workspaces contains duplicate IDs");
  }
  return {
    user: {
      id: scopedID(user.id, "session.user.id", "usr_"),
      displayName: nonEmptyString(
        user.display_name,
        "session.user.display_name",
      ),
    },
    workspaces,
    expiresAt: timestamp(session.expires_at, "session.expires_at"),
  };
}

export function csrfTokenFromCookie(cookie: string): string | null {
  for (const item of cookie.split(";")) {
    const [name, ...valueParts] = item.trim().split("=");
    if (name !== "__Host-radishnexus-csrf") {
      continue;
    }
    const value = valueParts.join("=");
    if (!/^[A-Za-z0-9_-]{43}$/u.test(value)) {
      return null;
    }
    return value;
  }
  return null;
}

async function responseError(response: Response): Promise<AuthRequestError> {
  let code: string | undefined;
  try {
    const payload = record(await response.json(), "error response");
    const error = record(payload.error, "error response.error");
    code = string(error.code, "error response.error.code");
  } catch {
    code = undefined;
  }
  return new AuthRequestError(authErrorMessage(response.status, code), {
    code,
    status: response.status,
  });
}

function authErrorMessage(status: number, code?: string): string {
  if (code === "invalid_credentials") {
    return "登录名或密码不正确。";
  }
  if (code === "rate_limited" || status === 429) {
    return "登录尝试过于频繁，请稍后再试。";
  }
  if (code === "unauthenticated" || status === 401) {
    return "当前会话已失效，请重新登录。";
  }
  if (
    code === "invalid_origin" ||
    code === "secure_transport_required" ||
    code === "invalid_proxy_chain"
  ) {
    return "当前站点的安全入口配置无效，请联系实例管理员。";
  }
  if (code === "csrf_failed") {
    return "安全校验失败，请刷新页面后重试。";
  }
  return "认证服务暂不可用，请稍后重试。";
}

function networkError(error: unknown): AuthRequestError {
  if (isAbortError(error)) {
    throw error;
  }
  return new AuthRequestError("无法连接到 RadishNexus，请检查网络后重试。", {
    cause: error,
  });
}

function scopedID(value: unknown, path: string, prefix: string): string {
  const id = string(value, path);
  if (
    id.length <= prefix.length ||
    id.length > 128 ||
    !id.startsWith(prefix) ||
    !/^[\x21-\x7e]+$/u.test(id) ||
    /[/?#]/u.test(id)
  ) {
    throw new TypeError(`${path} is invalid`);
  }
  return id;
}

function timestamp(value: unknown, path: string): string {
  const result = string(value, path);
  if (
    !/^\d{4}-\d{2}-\d{2}T/u.test(result) ||
    Number.isNaN(Date.parse(result))
  ) {
    throw new TypeError(`${path} is not an RFC 3339 timestamp`);
  }
  return result;
}

function record(value: unknown, path: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new TypeError(`${path} must be an object`);
  }
  return value as Record<string, unknown>;
}

function array(value: unknown, path: string): unknown[] {
  if (!Array.isArray(value)) {
    throw new TypeError(`${path} must be an array`);
  }
  return value;
}

function string(value: unknown, path: string): string {
  if (typeof value !== "string") {
    throw new TypeError(`${path} must be a string`);
  }
  return value;
}

function nonEmptyString(value: unknown, path: string): string {
  const result = string(value, path);
  if (result.trim() === "") {
    throw new TypeError(`${path} must not be empty`);
  }
  return result;
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}
