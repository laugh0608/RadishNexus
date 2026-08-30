import { afterEach, describe, expect, it, vi } from "vitest";
import {
  AuthRequestError,
  browserAuthClient,
  csrfTokenFromCookie,
  parseSessionContext,
} from "./api";

const sessionPayload = {
  user: { id: "usr_admin", display_name: "Radish Admin" },
  workspaces: [
    { id: "wrk_main", name: "Main Workspace", role: "owner" },
    { id: "wrk_docs", name: "Docs", role: "member" },
  ],
  expires_at: "2026-08-31T02:24:31Z",
};

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(document, "cookie");
});

describe("authentication API", () => {
  it("resolves and validates the current Session context", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(sessionPayload));
    vi.stubGlobal("fetch", fetchMock);

    const session = await browserAuthClient.resolveSession();

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/auth/session",
      expect.objectContaining({
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
      }),
    );
    expect(session).toEqual({
      user: { id: "usr_admin", displayName: "Radish Admin" },
      workspaces: [
        { id: "wrk_main", name: "Main Workspace", role: "owner" },
        { id: "wrk_docs", name: "Docs", role: "member" },
      ],
      expiresAt: "2026-08-31T02:24:31Z",
    });
  });

  it("posts credentials only in the controlled JSON login body", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(sessionPayload, 201));
    vi.stubGlobal("fetch", fetchMock);

    await browserAuthClient.login({
      loginName: "admin",
      password: "correct horse battery staple",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/auth/sessions",
      expect.objectContaining({
        method: "POST",
        credentials: "same-origin",
        cache: "no-store",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/json; charset=utf-8",
        },
        body: JSON.stringify({
          login_name: "admin",
          password: "correct horse battery staple",
        }),
      }),
    );
  });

  it("maps public authentication errors without exposing server detail", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(
            { error: { code: "invalid_credentials", message: "ignored" } },
            401,
          ),
        ),
    );

    await expect(
      browserAuthClient.login({ loginName: "admin", password: "wrong" }),
    ).rejects.toMatchObject({
      code: "invalid_credentials",
      status: 401,
      userMessage: "登录名或密码不正确。",
    } satisfies Partial<AuthRequestError>);
  });

  it("uses the readable CSRF cookie for logout", async () => {
    const csrfToken = "a".repeat(43);
    Object.defineProperty(document, "cookie", {
      configurable: true,
      value: `theme=dark; __Host-radishnexus-csrf=${csrfToken}`,
    });
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await browserAuthClient.logout();

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/auth/session",
      expect.objectContaining({
        method: "DELETE",
        credentials: "same-origin",
        headers: {
          Accept: "application/json",
          "X-CSRF-Token": csrfToken,
        },
      }),
    );
    expect(csrfTokenFromCookie(document.cookie)).toBe(csrfToken);
  });

  it("rejects missing CSRF and drifted Session responses", async () => {
    await expect(browserAuthClient.logout()).rejects.toMatchObject({
      code: "csrf_failed",
      status: 403,
    } satisfies Partial<AuthRequestError>);

    expect(() =>
      parseSessionContext({
        ...sessionPayload,
        workspaces: [
          sessionPayload.workspaces[0],
          { ...sessionPayload.workspaces[0], name: "Duplicate" },
        ],
      }),
    ).toThrow(/duplicate/iu);
  });
});

function jsonResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: {
      get: (name: string) =>
        name.toLowerCase() === "content-type" ? "application/json" : null,
    } as Headers,
    json: async () => payload,
  } as Response;
}

function emptyResponse(status: number): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: { get: () => null } as unknown as Headers,
  } as Response;
}
