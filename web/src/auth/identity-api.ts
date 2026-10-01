import {
  AuthRequestError,
  csrfTokenFromCookie,
  parseSessionContext,
  type SessionContext,
} from "./api";

export interface LoginMethods {
  local: true;
  radish: boolean;
  registration: false;
}
export interface AccountDetails {
  user: { id: string; displayName: string };
  local: boolean;
  radishLinked: boolean;
  recentAuthentication: boolean;
}
export interface Invitation {
  id: string;
  token: string;
  expiresAt: string;
}
export interface IdentityClient {
  methods(signal?: AbortSignal): Promise<LoginMethods>;
  account(signal?: AbortSignal): Promise<AccountDetails>;
  start(input: {
    mode: "login" | "link";
    invitationToken?: string;
    displayName?: string;
  }): Promise<string>;
  createInvitation(workspaceID: string): Promise<Invitation>;
  acceptInvitation(
    input: {
      invitationToken: string;
      email?: string;
      password?: string;
      displayName?: string;
    },
    signedIn: boolean,
  ): Promise<SessionContext>;
  unlink(): Promise<void>;
}

export const browserIdentityClient: IdentityClient = {
  methods: async (signal) => {
    const value = object(
      await request("/api/v1/auth/methods", "GET", undefined, false, signal),
      ["local", "radish", "registration"],
    );
    if (value.local !== true || value.registration !== false)
      throw contractError();
    return { local: true, radish: boolean(value.radish), registration: false };
  },
  account: async (signal) => {
    const value = object(
      await request("/api/v1/auth/account", "GET", undefined, false, signal),
      ["user", "local", "radish_linked", "recent_authentication"],
    );
    const user = object(value.user, ["id", "display_name"]);
    if (
      typeof user.id !== "string" ||
      !/^usr_[A-Za-z0-9_-]+$/u.test(user.id) ||
      typeof user.display_name !== "string" ||
      !user.display_name.trim()
    )
      throw contractError();
    return {
      user: { id: user.id, displayName: user.display_name },
      local: boolean(value.local),
      radishLinked: boolean(value.radish_linked),
      recentAuthentication: boolean(value.recent_authentication),
    };
  },
  start: async (input) => {
    const value = object(
      await request(
        "/api/v1/auth/oidc/start",
        "POST",
        {
          mode: input.mode,
          ...(input.invitationToken
            ? {
                invitation_token: input.invitationToken,
                display_name: input.displayName,
              }
            : {}),
        },
        input.mode === "link",
      ),
      ["authorization_url"],
    );
    if (typeof value.authorization_url !== "string") throw contractError();
    let url: URL;
    try {
      url = new URL(value.authorization_url);
    } catch {
      throw contractError();
    }
    if (url.protocol !== "https:" || url.username || url.password || url.hash)
      throw contractError();
    return url.href;
  },
  createInvitation: async (workspaceID) => {
    const value = object(
      await request(
        `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/invitations`,
        "POST",
        {},
        true,
      ),
      ["id", "invitation_token", "expires_at"],
    );
    if (
      typeof value.id !== "string" ||
      !/^inv_[A-Za-z0-9_-]+$/u.test(value.id) ||
      typeof value.invitation_token !== "string" ||
      !/^[A-Za-z0-9_-]{43}$/u.test(value.invitation_token) ||
      typeof value.expires_at !== "string" ||
      Number.isNaN(Date.parse(value.expires_at))
    )
      throw contractError();
    return {
      id: value.id,
      token: value.invitation_token,
      expiresAt: value.expires_at,
    };
  },
  acceptInvitation: async (input, signedIn) =>
    parseSessionContext(
      await request(
        "/api/v1/auth/invitations/accept",
        "POST",
        {
          invitation_token: input.invitationToken,
          ...(signedIn
            ? {}
            : {
                email: input.email,
                password: input.password,
                display_name: input.displayName,
              }),
        },
        signedIn,
      ),
    ),
  unlink: async () => {
    await request("/api/v1/auth/external-identity", "DELETE", undefined, true);
  },
};

async function request(
  path: string,
  method: string,
  body: unknown,
  csrf: boolean,
  signal?: AbortSignal,
): Promise<unknown> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (csrf) {
    const token = csrfTokenFromCookie(document.cookie);
    if (token === null)
      throw new AuthRequestError("安全校验失败，请刷新页面后重试。", {
        status: 403,
        code: "csrf_failed",
      });
    headers["X-CSRF-Token"] = token;
  }
  let response: Response;
  try {
    response = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: "same-origin",
      cache: "no-store",
      signal,
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError")
      throw error;
    throw new AuthRequestError("无法连接账户服务，请稍后重试。");
  }
  if (!response.ok) {
    let code = "";
    try {
      const payload: unknown = await response.json();
      if (
        typeof payload === "object" &&
        payload !== null &&
        "error" in payload &&
        typeof payload.error === "object" &&
        payload.error !== null &&
        "code" in payload.error &&
        typeof payload.error.code === "string"
      )
        code = payload.error.code;
    } catch {
      /* The status still carries the transport failure. */
    }
    const messages: Record<string, string> = {
      invitation_invalid: "邀请已失效或不可用，请联系工作区管理员。",
      identity_conflict:
        "无法创建或关联账户；已有账户请先登录后再接受邀请或绑定。",
      recent_authentication_required: "请退出并重新登录，然后再操作登录方式。",
      last_login_method: "至少需要保留一种可用的登录方式。",
      oidc_unavailable: "Radish 登录暂不可用，你仍可使用本地账户。",
      oidc_failed: "Radish 登录失败，请重试。",
      rate_limited: "操作过于频繁，请稍后重试。",
      invalid: "请检查邮箱、展示名和密码；密码需为 15–128 个字符。",
      forbidden: "你目前没有执行此操作的权限。",
    };
    throw new AuthRequestError(
      response.status === 401
        ? "会话已失效，请重新登录。"
        : (messages[code] ?? "账户服务暂不可用，请稍后重试。"),
      { code, status: response.status },
    );
  }
  if (response.status === 204) return undefined;
  if (!response.headers.get("Content-Type")?.includes("application/json"))
    throw contractError();
  try {
    return await response.json();
  } catch {
    throw contractError();
  }
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
function boolean(value: unknown): boolean {
  if (typeof value !== "boolean") throw contractError();
  return value;
}
function contractError(): AuthRequestError {
  return new AuthRequestError("账户服务响应不符合当前契约，请联系管理员。");
}

export function identityErrorMessage(error: unknown): string {
  return error instanceof AuthRequestError
    ? error.userMessage
    : "账户操作失败，请稍后重试。";
}
