import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { App } from "./App";
import {
  AuthRequestError,
  type AuthClient,
  type SessionContext,
} from "./auth/api";
import type { ChannelMessageClient } from "./channel/api";
import type { ChannelRealtimeClient } from "./channel/realtime";
import type {
  CollaborationClient,
  CollaborationView,
  ThreadCurrent,
} from "./collaboration/api";
import { DeploymentNexusViewLoadError } from "./nexus-view/api";
import { succeededDeploymentNexusViewFixture } from "./nexus-view/fixture";

const session: SessionContext = {
  user: { id: "usr_admin", displayName: "Radish Admin" },
  workspaces: [
    { id: "wrk_main", name: "Main Workspace", role: "owner" },
    { id: "wrk_docs", name: "Docs", role: "member" },
  ],
  expiresAt: "2026-08-31T02:24:31Z",
};

const threadView: CollaborationView<ThreadCurrent> = {
  current: {
    ref: { type: "thread", id: "thr_discussion" },
    project: { type: "project", id: "prj_main" },
    originChannel: null,
    title: "Canonical Thread",
    visibility: "project",
    createdBy: { kind: "user", id: "usr_admin" },
    createdAt: "2026-09-02T03:00:00Z",
    updatedAt: "2026-09-02T03:00:00Z",
  },
  relations: [],
  timeline: [],
};

describe("App prototype state controls", () => {
  it("keeps the representative states on an explicit non-product route", () => {
    render(<App pathname="/prototype/nexus-view" />);

    expect(screen.getByText("部署成功")).toBeDefined();
    const failedButton = screen.getByRole("button", { name: "失败" });
    fireEvent.click(failedButton);
    expect(failedButton.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText("部署失败")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "加载" }));
    expect(screen.getByRole("status")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "错误" }));
    fireEvent.click(screen.getByRole("button", { name: "重新载入" }));
    expect(screen.getByText("部署成功")).toBeDefined();
  });
});

describe("authenticated Web Shell", () => {
  it("shows login for an unauthenticated Session and enters the selected Workspace", async () => {
    const navigate = vi.fn();
    const auth = testAuthClient({
      resolveSession: vi.fn().mockRejectedValue(
        new AuthRequestError("当前会话已失效，请重新登录。", {
          code: "unauthenticated",
          status: 401,
        }),
      ),
      login: vi.fn().mockResolvedValue(session),
    });
    render(<App pathname="/" authClient={auth} navigate={navigate} />);

    expect(
      await screen.findByRole("heading", { name: "登录 RadishNexus" }),
    ).toBeDefined();
    fireEvent.change(screen.getByLabelText("登录名"), {
      target: { value: "admin" },
    });
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "correct horse battery staple" },
    });
    fireEvent.click(screen.getByRole("button", { name: "登录" }));

    expect(
      await screen.findByRole("heading", { name: "欢迎回来，Radish Admin" }),
    ).toBeDefined();
    expect(auth.login).toHaveBeenCalledWith(
      {
        loginName: "admin",
        password: "correct horse battery staple",
      },
      undefined,
    );

    fireEvent.change(screen.getByLabelText("Workspace"), {
      target: { value: "wrk_docs" },
    });
    fireEvent.change(screen.getByLabelText("Deployment ID"), {
      target: { value: "dpl_release_42" },
    });
    fireEvent.click(screen.getByRole("button", { name: "打开 Nexus View" }));
    expect(navigate).toHaveBeenCalledWith(
      "/workspaces/wrk_docs/deployments/dpl_release_42",
    );
  });

  it("does not send invalid object IDs into product navigation", async () => {
    const navigate = vi.fn();
    render(
      <App pathname="/" authClient={testAuthClient()} navigate={navigate} />,
    );
    await screen.findByRole("heading", { name: "欢迎回来，Radish Admin" });
    fireEvent.change(screen.getByLabelText("Deployment ID"), {
      target: { value: "not-a-deployment" },
    });
    fireEvent.click(screen.getByRole("button", { name: "打开 Nexus View" }));

    expect(screen.getByRole("alert").textContent).toContain("dpl_");
    expect(navigate).not.toHaveBeenCalled();
  });

  it("opens a known Channel from the selected Workspace", async () => {
    const navigate = vi.fn();
    render(
      <App pathname="/" authClient={testAuthClient()} navigate={navigate} />,
    );
    await screen.findByRole("heading", { name: "欢迎回来，Radish Admin" });
    fireEvent.change(screen.getByLabelText("Workspace"), {
      target: { value: "wrk_docs" },
    });
    fireEvent.change(screen.getByLabelText("Channel ID"), {
      target: { value: "chn_team" },
    });
    fireEvent.click(screen.getByRole("button", { name: "打开 Channel" }));

    expect(navigate).toHaveBeenCalledWith(
      "/workspaces/wrk_docs/channels/chn_team",
    );
  });

  it("opens a known collaboration object from the selected Workspace", async () => {
    const navigate = vi.fn();
    render(
      <App pathname="/" authClient={testAuthClient()} navigate={navigate} />,
    );
    await screen.findByRole("heading", { name: "欢迎回来，Radish Admin" });
    fireEvent.change(screen.getByLabelText("Workspace"), {
      target: { value: "wrk_docs" },
    });
    fireEvent.change(screen.getByLabelText("协作对象类型"), {
      target: { value: "thread" },
    });
    fireEvent.change(screen.getByLabelText("协作对象 ID"), {
      target: { value: "thr_discussion" },
    });
    fireEvent.click(screen.getByRole("button", { name: "打开协作对象" }));

    expect(navigate).toHaveBeenCalledWith(
      "/workspaces/wrk_docs/threads/thr_discussion",
    );
  });

  it("loads a canonical Channel only after Session bootstrap", async () => {
    const channelClient = testChannelClient({
      listMessages: vi.fn().mockResolvedValue({
        messages: [
          {
            id: "msg_1",
            channelID: "chn_team",
            threadID: null,
            authorID: "usr_admin",
            body: "Canonical Channel body.",
            createdAt: "2026-09-01T03:00:00Z",
          },
        ],
        olderCursor: null,
      }),
    });
    render(
      <App
        pathname="/workspaces/wrk_main/channels/chn_team"
        authClient={testAuthClient()}
        channelClient={channelClient}
        channelRealtimeClient={testRealtimeClient()}
      />,
    );

    expect(await screen.findByText("Canonical Channel body.")).toBeDefined();
    expect(screen.getByText("真实 API · Main Workspace")).toBeDefined();
    expect(channelClient.listMessages).toHaveBeenCalledWith(
      "wrk_main",
      "chn_team",
      undefined,
      expect.any(AbortSignal),
    );
  });

  it("loads a canonical Deployment only after Session bootstrap", async () => {
    const loader = vi
      .fn()
      .mockResolvedValue(succeededDeploymentNexusViewFixture);
    render(
      <App
        pathname="/workspaces/wrk_main/deployments/dpl_release_42"
        authClient={testAuthClient()}
        loadDeployment={loader}
      />,
    );

    expect(await screen.findByText("部署成功")).toBeDefined();
    expect(screen.getByText("真实 API · Main Workspace")).toBeDefined();
    expect(screen.queryByLabelText("原型状态检视")).toBeNull();
    expect(loader).toHaveBeenCalledWith(
      "wrk_main",
      "dpl_release_42",
      expect.any(AbortSignal),
    );
  });

  it("loads a canonical collaboration object only after Session bootstrap", async () => {
    const collaborationClient = testCollaborationClient();
    render(
      <App
        pathname="/workspaces/wrk_main/threads/thr_discussion"
        authClient={testAuthClient()}
        collaborationClient={collaborationClient}
      />,
    );

    expect(
      await screen.findByRole("heading", { name: "Canonical Thread" }),
    ).toBeDefined();
    expect(screen.getByText("真实 API · Main Workspace")).toBeDefined();
    expect(collaborationClient.loadView).toHaveBeenCalledWith(
      "wrk_main",
      "thread",
      "thr_discussion",
      expect.any(AbortSignal),
    );
  });

  it("shows a safe Deployment failure and retries the same scoped read", async () => {
    const loader = vi
      .fn()
      .mockRejectedValueOnce(
        new DeploymentNexusViewLoadError(
          "该 Deployment 不存在，或你没有读取它的权限。",
          404,
        ),
      )
      .mockResolvedValueOnce(succeededDeploymentNexusViewFixture);
    render(
      <App
        pathname="/workspaces/wrk_main/deployments/dpl_release_42"
        authClient={testAuthClient()}
        loadDeployment={loader}
      />,
    );

    expect(
      await screen.findByText("该 Deployment 不存在，或你没有读取它的权限。"),
    ).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "重新载入" }));
    await waitFor(() => expect(loader).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("部署成功")).toBeDefined();
  });

  it("returns to login when the business read reports an expired Session", async () => {
    const loader = vi
      .fn()
      .mockRejectedValue(
        new DeploymentNexusViewLoadError(
          "当前会话已失效，请重新登录后再试。",
          401,
        ),
      );
    render(
      <App
        pathname="/workspaces/wrk_main/deployments/dpl_release_42"
        authClient={testAuthClient()}
        loadDeployment={loader}
      />,
    );

    expect(
      await screen.findByRole("heading", { name: "登录 RadishNexus" }),
    ).toBeDefined();
  });

  it("logs out through the Session client without changing the current route", async () => {
    const auth = testAuthClient();
    render(<App pathname="/" authClient={auth} />);
    await screen.findByRole("heading", { name: "欢迎回来，Radish Admin" });
    fireEvent.click(screen.getByRole("button", { name: "退出登录" }));

    expect(
      await screen.findByRole("heading", { name: "登录 RadishNexus" }),
    ).toBeDefined();
    expect(auth.logout).toHaveBeenCalledOnce();
  });

  it("keeps the Session visible when logout cannot prove CSRF", async () => {
    const auth = testAuthClient({
      logout: vi.fn().mockRejectedValue(
        new AuthRequestError("安全校验失败，请刷新页面后重试。", {
          code: "csrf_failed",
          status: 403,
        }),
      ),
    });
    render(<App pathname="/" authClient={auth} />);
    await screen.findByRole("heading", { name: "欢迎回来，Radish Admin" });
    fireEvent.click(screen.getByRole("button", { name: "退出登录" }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      "安全校验失败",
    );
    expect(
      screen.getByRole("heading", { name: "欢迎回来，Radish Admin" }),
    ).toBeDefined();
  });
});

function testAuthClient(overrides: Partial<AuthClient> = {}): AuthClient {
  return {
    resolveSession: vi.fn().mockResolvedValue(session),
    login: vi.fn().mockResolvedValue(session),
    logout: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

function testChannelClient(
  overrides: Partial<ChannelMessageClient> = {},
): ChannelMessageClient {
  return {
    listMessages: vi.fn().mockResolvedValue({
      messages: [],
      olderCursor: null,
    }),
    createMessage: vi.fn(),
    startThread: vi.fn(),
    ...overrides,
  };
}

function testRealtimeClient(): ChannelRealtimeClient {
  return {
    connect: vi.fn((_workspaceID, _channelID, handlers) => {
      handlers.onReady();
      return { close: vi.fn() };
    }),
  };
}

function testCollaborationClient(
  overrides: Partial<CollaborationClient> = {},
): CollaborationClient {
  return {
    loadView: vi.fn().mockResolvedValue(threadView),
    proposeDecision: vi.fn(),
    acceptDecision: vi.fn(),
    createTicket: vi.fn(),
    ...overrides,
  };
}
