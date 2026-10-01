import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IdentityLogin } from "./IdentityLogin";
import { IdentityPanel } from "./IdentityPanel";
import { AuthRequestError, type SessionContext } from "./api";
import { browserIdentityClient, type IdentityClient } from "./identity-api";

const session: SessionContext = {
  user: { id: "usr_member", displayName: "Member" },
  workspaces: [{ id: "wrk_main", name: "Main", role: "member" }],
  expiresAt: "2026-09-11T12:00:00Z",
};
function client(overrides: Partial<IdentityClient> = {}): IdentityClient {
  return {
    methods: vi
      .fn()
      .mockResolvedValue({ local: true, radish: true, registration: false }),
    account: vi.fn().mockResolvedValue({
      user: session.user,
      local: false,
      radishLinked: true,
      recentAuthentication: true,
    }),
    start: vi
      .fn()
      .mockResolvedValue("https://radish.example.test/connect/authorize"),
    createInvitation: vi
      .fn()
      .mockRejectedValue(new Error("not implemented by fixture")),
    acceptInvitation: vi.fn().mockResolvedValue(session),
    unlink: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}
afterEach(() => vi.unstubAllGlobals());

describe("identity user flows", () => {
  it("uses Radish only when configured and never persists credentials", async () => {
    const identity = client();
    const navigate = vi.fn();
    const storage = vi.spyOn(Storage.prototype, "setItem");
    render(
      <IdentityLogin
        client={identity}
        navigate={navigate}
        onSession={vi.fn()}
      />,
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "使用 Radish 登录" }),
    );
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith(
        "https://radish.example.test/connect/authorize",
      ),
    );
    expect(identity.start).toHaveBeenCalledWith({ mode: "login" });
    expect(storage).not.toHaveBeenCalled();
    storage.mockRestore();
  });
  it("accepts an invitation with independent display name and email", async () => {
    const identity = client({
      methods: vi
        .fn()
        .mockResolvedValue({ local: true, radish: false, registration: false }),
    });
    const onSession = vi.fn();
    render(
      <IdentityLogin
        client={identity}
        navigate={vi.fn()}
        onSession={onSession}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "我有邀请码" }));
    fireEvent.change(screen.getByLabelText("邀请码"), {
      target: { value: "A".repeat(43) },
    });
    fireEvent.change(screen.getByLabelText("展示名"), {
      target: { value: "Member" },
    });
    fireEvent.change(screen.getByLabelText("注册邮箱"), {
      target: { value: "member@example.test" },
    });
    fireEvent.change(screen.getByLabelText("设置密码"), {
      target: { value: "member fixture password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建账户并加入" }));
    await waitFor(() => expect(onSession).toHaveBeenCalledWith(session));
    expect(identity.acceptInvitation).toHaveBeenCalledWith(
      {
        invitationToken: "A".repeat(43),
        email: "member@example.test",
        password: "member fixture password",
        displayName: "Member",
      },
      false,
    );
    expect(
      screen.queryByRole("button", { name: "使用 Radish 登录" }),
    ).toBeNull();
  });
  it("prevents removing the sole login method and hides member invitation creation", async () => {
    const identity = client();
    render(
      <IdentityPanel
        client={identity}
        session={session}
        navigate={vi.fn()}
        onSignedOut={vi.fn()}
        onSession={vi.fn()}
      />,
    );
    const unlink = await screen.findByRole("button", { name: "解绑 Radish" });
    expect((unlink as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole("button", { name: "生成邀请码" })).toBeNull();
    fireEvent.click(unlink);
    expect(identity.unlink).not.toHaveBeenCalled();
  });
  it("requires explicit confirmation before unlinking and ending sessions", async () => {
    const identity = client({
      account: vi.fn().mockResolvedValue({
        user: session.user,
        local: true,
        radishLinked: true,
        recentAuthentication: true,
      }),
    });
    const signedOut = vi.fn();
    render(
      <IdentityPanel
        client={identity}
        session={session}
        navigate={vi.fn()}
        onSignedOut={signedOut}
        onSession={vi.fn()}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "解绑 Radish" }));
    expect(identity.unlink).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "确认解绑并退出所有会话" }),
    );
    await waitFor(() => expect(signedOut).toHaveBeenCalledOnce());
  });
  it("does not turn an invitation failure into a signed-in state", async () => {
    const identity = client({
      acceptInvitation: vi.fn().mockRejectedValue(
        new AuthRequestError("邀请已失效", {
          status: 403,
          code: "invitation_invalid",
        }),
      ),
    });
    const onSession = vi.fn();
    render(
      <IdentityPanel
        client={identity}
        session={session}
        navigate={vi.fn()}
        onSignedOut={vi.fn()}
        onSession={onSession}
      />,
    );
    await screen.findByRole("heading", { name: "Member" });
    fireEvent.change(screen.getByLabelText("邀请码"), {
      target: { value: "A".repeat(43) },
    });
    fireEvent.click(screen.getByRole("button", { name: "接受邀请" }));
    expect((await screen.findByRole("alert")).textContent).toContain(
      "邀请已失效",
    );
    expect(onSession).not.toHaveBeenCalled();
  });
});

describe("identity strict consumer", () => {
  it("rejects capability or private-field drift", async () => {
    for (const payload of [
      { local: true, radish: true, registration: true },
      {
        local: true,
        radish: true,
        registration: false,
        client_secret: "private",
      },
      { local: true, radish: "yes", registration: false },
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify(payload), {
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );
      await expect(browserIdentityClient.methods()).rejects.toThrow("契约");
    }
  });
  it("rejects unsafe authorization URLs", async () => {
    for (const authorization_url of [
      "javascript:alert(1)",
      "http://radish.example.test/connect/authorize",
      "https://private@radish.example.test/connect/authorize",
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify({ authorization_url }), {
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );
      await expect(
        browserIdentityClient.start({ mode: "login" }),
      ).rejects.toThrow("契约");
    }
  });
});
