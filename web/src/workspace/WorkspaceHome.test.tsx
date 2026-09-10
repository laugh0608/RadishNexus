import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AuthRequestError, type SessionContext } from "../auth/api";
import type { DiscoveryClient, DiscoveryItem, DiscoveryPage } from "./api";
import { WorkspaceHome } from "./WorkspaceHome";

const session: SessionContext = {
  user: { id: "usr_reader", displayName: "Reader" },
  workspaces: [
    { id: "wrk_main", name: "Main", role: "member" },
    { id: "wrk_other", name: "Other", role: "member" },
  ],
  expiresAt: "2026-09-11T00:00:00Z",
};
const project: DiscoveryItem = {
  id: "prj_main",
  title: "研发项目",
  status: "active",
};
const channel: DiscoveryItem = {
  id: "chn_main",
  title: "日常讨论",
  status: "active",
};
function page(
  items: DiscoveryItem[],
  nextCursor: string | null = null,
): DiscoveryPage {
  return { items, nextCursor };
}
function client(overrides: Partial<DiscoveryClient> = {}): DiscoveryClient {
  return {
    projects: vi.fn().mockResolvedValue(page([project])),
    channels: vi.fn().mockResolvedValue(page([channel])),
    ...overrides,
  };
}
function pending<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe("Workspace discovery", () => {
  it("opens a readable Channel without entering IDs", async () => {
    const navigate = vi.fn();
    const api = client();
    render(
      <WorkspaceHome
        session={session}
        client={api}
        navigate={navigate}
        onSessionExpired={vi.fn()}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "研发项目" }));
    fireEvent.click(
      await screen.findByRole("button", { name: /日常讨论.*打开频道 →/u }),
    );
    expect(navigate).toHaveBeenCalledWith(
      "/workspaces/wrk_main/channels/chn_main",
    );
    expect(api.channels).toHaveBeenCalledWith(
      "wrk_main",
      "prj_main",
      undefined,
      expect.any(AbortSignal),
    );
  });

  it("clears the old workspace and ignores its late response", async () => {
    const late = pending<DiscoveryPage>();
    const projects = vi
      .fn()
      .mockReturnValueOnce(late.promise)
      .mockResolvedValue(
        page([{ ...project, id: "prj_other", title: "另一个项目" }]),
      );
    render(
      <WorkspaceHome
        session={session}
        client={client({ projects })}
        navigate={vi.fn()}
        onSessionExpired={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByLabelText("Workspace"), {
      target: { value: "wrk_other" },
    });
    expect(
      await screen.findByRole("button", { name: "另一个项目" }),
    ).toBeDefined();
    await act(async () => late.resolve(page([project])));
    expect(screen.queryByRole("button", { name: "研发项目" })).toBeNull();
    const firstSignal = projects.mock.calls[0]?.[2] as AbortSignal | undefined;
    expect(firstSignal?.aborted).toBe(true);
  });

  it("clears old channels when selecting another project", async () => {
    const late = pending<DiscoveryPage>();
    const channels = vi
      .fn()
      .mockReturnValueOnce(late.promise)
      .mockResolvedValue(page([{ ...channel, title: "另一个频道" }]));
    const api = client({
      projects: async () =>
        page([project, { ...project, id: "prj_other", title: "另一个项目" }]),
      channels,
    });
    render(
      <WorkspaceHome
        session={session}
        client={api}
        navigate={vi.fn()}
        onSessionExpired={vi.fn()}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "研发项目" }));
    fireEvent.click(screen.getByRole("button", { name: "另一个项目" }));
    await screen.findByRole("button", { name: /另一个频道.*打开频道 →/u });
    await act(async () => late.resolve(page([channel])));
    expect(screen.queryByText("日常讨论")).toBeNull();
  });

  it("replaces pages and re-fetches previous pages instead of keeping old results", async () => {
    const projects = vi
      .fn()
      .mockResolvedValueOnce(page([project], "opaque_cursor"))
      .mockResolvedValueOnce(
        page([{ ...project, id: "prj_second", title: "第二页项目" }]),
      )
      .mockResolvedValue(page([]));
    render(
      <WorkspaceHome
        session={session}
        client={client({ projects })}
        navigate={vi.fn()}
        onSessionExpired={vi.fn()}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "研发项目" }));
    await screen.findByText("日常讨论");
    fireEvent.click(screen.getByRole("button", { name: "下一页项目" }));
    expect(screen.queryByText("日常讨论")).toBeNull();
    expect(screen.queryByText("研发项目")).toBeNull();
    await screen.findByRole("button", { name: "第二页项目" });
    expect(projects).toHaveBeenLastCalledWith(
      "wrk_main",
      "opaque_cursor",
      expect.any(AbortSignal),
    );
    fireEvent.click(screen.getByRole("button", { name: "上一页项目" }));
    await screen.findByText("当前没有可访问的项目。");
    expect(screen.queryByText("研发项目")).toBeNull();
  });

  it("clears prior content before focus revalidation and shows failure honestly", async () => {
    const projects = vi
      .fn()
      .mockResolvedValueOnce(page([project]))
      .mockRejectedValue(new AuthRequestError("访问已撤销", { status: 404 }));
    render(
      <WorkspaceHome
        session={session}
        client={client({ projects })}
        navigate={vi.fn()}
        onSessionExpired={vi.fn()}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "研发项目" }));
    await screen.findByText("日常讨论");
    fireEvent(window, new Event("focus"));
    expect(screen.queryByText("日常讨论")).toBeNull();
    expect(screen.queryByText("研发项目")).toBeNull();
    expect((await screen.findByRole("alert")).textContent).toContain(
      "访问已撤销",
    );
    projects.mockResolvedValue(page([]));
    fireEvent.click(screen.getByRole("button", { name: "重新加载项目" }));
    await screen.findByText("当前没有可访问的项目。");
  });

  it("refreshes parent discovery if the selected project becomes inaccessible", async () => {
    const projects = vi
      .fn()
      .mockResolvedValueOnce(page([project]))
      .mockResolvedValue(page([]));
    render(
      <WorkspaceHome
        session={session}
        client={client({
          projects,
          channels: async () => {
            throw new AuthRequestError("不可访问", { status: 404 });
          },
        })}
        navigate={vi.fn()}
        onSessionExpired={vi.fn()}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "研发项目" }));
    await screen.findByText("当前没有可访问的项目。");
    expect(screen.queryByText("研发项目")).toBeNull();
  });

  it("returns expired sessions to authentication", async () => {
    const expired = vi.fn();
    render(
      <WorkspaceHome
        session={session}
        client={client({
          projects: async () => {
            throw new AuthRequestError("过期", { status: 401 });
          },
        })}
        navigate={vi.fn()}
        onSessionExpired={expired}
      />,
    );
    await waitFor(() => expect(expired).toHaveBeenCalledOnce());
  });

  it("shows archived objects as readable and guides users without a workspace", async () => {
    const rendered = render(
      <WorkspaceHome
        session={session}
        client={client({
          projects: async () => page([{ ...project, status: "archived" }]),
        })}
        navigate={vi.fn()}
        onSessionExpired={vi.fn()}
      />,
    );
    fireEvent.click(
      await screen.findByRole("button", { name: /研发项目.*已归档/u }),
    );
    await screen.findByRole("button", { name: /日常讨论.*已归档 · 可浏览/u });
    rendered.unmount();
    const api = client();
    render(
      <WorkspaceHome
        session={{ ...session, workspaces: [] }}
        client={api}
        navigate={vi.fn()}
        onSessionExpired={vi.fn()}
      />,
    );
    expect(
      screen.getByText("你还没有加入工作区。请在“账户与邀请”中接受邀请。"),
    ).toBeDefined();
    expect(api.projects).not.toHaveBeenCalled();
  });
});
