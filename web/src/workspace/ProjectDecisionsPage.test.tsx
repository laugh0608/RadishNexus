import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AuthRequestError } from "../auth/api";
import { ProjectDecisionsPage } from "./ProjectDecisionsPage";
import type { ProjectDecisionsClient } from "./project-decisions-api";

const project = {
  id: "prj_main",
  kind: "project" as const,
  name: "Private project",
  key: "MAIN",
  visibility: "restricted" as const,
  status: "active" as const,
  canManage: false,
};
const page = {
  items: [
    { id: "dec_first", title: "First work", status: "proposed" as const },
  ],
  nextCursor: "next_page",
};
const props = {
  workspaceID: "wrk_main",
  projectID: "prj_main",
  onSessionExpired: vi.fn(),
};
function client(): ProjectDecisionsClient {
  return {
    project: vi.fn().mockResolvedValue(project),
    decisions: vi.fn().mockResolvedValue(page),
  };
}
function pending<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

describe("Project Decisions page", () => {
  it("shows long questions as plain text and labels both states without write actions", async () => {
    const api = client();
    const title =
      "<script>alert(1)</script> **中文问题**\n" + "长问题".repeat(500);
    vi.mocked(api.decisions).mockResolvedValue({
      items: [
        { id: "dec_a", title, status: "proposed" },
        { id: "dec_b", title: "已确认的问题", status: "accepted" },
      ],
      nextCursor: null,
    });
    const view = render(<ProjectDecisionsPage {...props} client={api} />);
    const link = await screen.findByRole("link", { name: /<script>alert/ });
    expect(link.textContent).toBe(title);
    expect(link.querySelector("script, strong")).toBeNull();
    expect(screen.getByText("待确认")).toBeDefined();
    expect(screen.getByText("已确认")).toBeDefined();
    expect(view.container.querySelector("form, input, select")).toBeNull();
    expect(
      screen.queryByRole("button", { name: /接受|拒绝|创建|确认/ }),
    ).toBeNull();
  });
  it.each(["project", "decisions"] as const)(
    "clears both reads when %s fails before the other returns late",
    async (failed) => {
      const api = client();
      const lateProject = pending<typeof project>();
      const latePage = pending<typeof page>();
      vi.mocked(api.project).mockResolvedValueOnce(project);
      vi.mocked(api.decisions).mockResolvedValueOnce(page);
      if (failed === "project") {
        vi.mocked(api.project).mockRejectedValue(
          new AuthRequestError("denied", { status: 404 }),
        );
        vi.mocked(api.decisions).mockReturnValue(latePage.promise);
      } else {
        vi.mocked(api.project).mockReturnValue(lateProject.promise);
        vi.mocked(api.decisions).mockRejectedValue(new Error("unavailable"));
      }
      render(<ProjectDecisionsPage {...props} client={api} />);
      await screen.findByText("First work");
      fireEvent.click(screen.getByRole("button", { name: "刷新决策列表" }));
      await screen.findByRole("alert");
      await act(async () => {
        lateProject.resolve(project);
        latePage.resolve(page);
      });
      expect(screen.queryByText(project.name)).toBeNull();
      expect(screen.queryByText("First work")).toBeNull();
      expect(screen.queryByText("当前项目没有可显示的决策。")).toBeNull();
      const signal = vi.mocked(api.project).mock.calls.at(-1)![2]!;
      expect(signal.aborted).toBe(true);
    },
  );
  it("handles simultaneous failed reads and retries with a fresh generation", async () => {
    const api = client();
    vi.mocked(api.project)
      .mockRejectedValueOnce(new Error("context failed"))
      .mockResolvedValue(project);
    vi.mocked(api.decisions)
      .mockRejectedValueOnce(new Error("list failed"))
      .mockResolvedValue(page);
    render(<ProjectDecisionsPage {...props} client={api} />);
    expect((await screen.findByRole("alert")).textContent).toBe(
      "无法读取项目决策，请重试。",
    );
    expect(screen.queryByText(project.name)).toBeNull();
    expect(screen.queryByText("First work")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重新读取决策" }));
    await screen.findByText("First work");
    expect(api.project).toHaveBeenCalledTimes(2);
    expect(api.decisions).toHaveBeenCalledTimes(2);
  });
  it.each([
    { workspaceID: "wrk_other", projectID: "prj_main" },
    { workspaceID: "wrk_main", projectID: "prj_other" },
  ])(
    "resets second-page state on scope change to $workspaceID/$projectID",
    async (scope) => {
      const api = client();
      const late = pending<typeof page>();
      vi.mocked(api.decisions)
        .mockResolvedValueOnce(page)
        .mockReturnValueOnce(late.promise)
        .mockResolvedValue({ items: [], nextCursor: null });
      const view = render(<ProjectDecisionsPage {...props} client={api} />);
      await screen.findByText("First work");
      fireEvent.click(screen.getByRole("button", { name: "下一页" }));
      const oldSignal = vi.mocked(api.decisions).mock.calls.at(-1)![3]!;
      view.rerender(
        <ProjectDecisionsPage {...props} {...scope} client={api} />,
      );
      await screen.findByText("当前项目没有可显示的决策。");
      await act(async () => late.resolve(page));
      expect(oldSignal.aborted).toBe(true);
      expect(screen.queryByText("First work")).toBeNull();
      expect(screen.getByText("第 1 页")).toBeDefined();
      expect(api.decisions).toHaveBeenLastCalledWith(
        scope.workspaceID,
        scope.projectID,
        undefined,
        expect.any(AbortSignal),
      );
    },
  );
  it("rechecks each page and starts at the first page after access is restored", async () => {
    const api = client();
    vi.mocked(api.decisions)
      .mockResolvedValueOnce(page)
      .mockRejectedValueOnce(new AuthRequestError("revoked", { status: 404 }))
      .mockResolvedValue(page);
    render(<ProjectDecisionsPage {...props} client={api} />);
    await screen.findByText("First work");
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.queryByText(project.name)).toBeNull();
    await screen.findByRole("alert");
    expect(screen.queryByText("First work")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重新读取决策" }));
    await screen.findByText("First work");
    expect(api.decisions).toHaveBeenLastCalledWith(
      "wrk_main",
      "prj_main",
      undefined,
      expect.any(AbortSignal),
    );
    expect(screen.getByText("第 1 页")).toBeDefined();
  });
  it("clears private content on pagehide, ignores late responses and reauthorizes BFCache at page one", async () => {
    const api = client();
    const late = pending<typeof page>();
    vi.mocked(api.decisions)
      .mockResolvedValueOnce(page)
      .mockReturnValueOnce(late.promise)
      .mockRejectedValue(new AuthRequestError("revoked", { status: 404 }));
    render(<ProjectDecisionsPage {...props} client={api} />);
    await screen.findByText("First work");
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    act(() =>
      window.dispatchEvent(
        new PageTransitionEvent("pagehide", { persisted: true }),
      ),
    );
    await act(async () => late.resolve(page));
    expect(screen.queryByText(project.name)).toBeNull();
    expect(screen.queryByText("First work")).toBeNull();
    act(() =>
      window.dispatchEvent(
        new PageTransitionEvent("pageshow", { persisted: true }),
      ),
    );
    await screen.findByRole("alert");
    expect(screen.queryByText(project.name)).toBeNull();
    expect(api.decisions).toHaveBeenLastCalledWith(
      "wrk_main",
      "prj_main",
      undefined,
      expect.any(AbortSignal),
    );
  });
  it("aborts both reads on leaving the page and rereads on reentry", async () => {
    const api = client();
    const first = render(<ProjectDecisionsPage {...props} client={api} />);
    await screen.findByText("First work");
    const signal = vi.mocked(api.decisions).mock.calls[0]![3]!;
    first.unmount();
    expect(signal.aborted).toBe(true);
    vi.mocked(api.decisions).mockResolvedValue({ items: [], nextCursor: null });
    render(<ProjectDecisionsPage {...props} client={api} />);
    await screen.findByText("当前项目没有可显示的决策。");
    expect(screen.queryByText("First work")).toBeNull();
    expect(api.decisions).toHaveBeenCalledTimes(2);
  });

  it("opens details, pages and refreshes to the first page with current Project context", async () => {
    const api = client();
    vi.mocked(api.project).mockResolvedValue({
      ...project,
      status: "archived",
    });
    vi.mocked(api.decisions)
      .mockResolvedValueOnce(page)
      .mockResolvedValueOnce({
        items: [{ id: "dec_second", title: "Second work", status: "proposed" }],
        nextCursor: null,
      })
      .mockResolvedValue(page);
    render(<ProjectDecisionsPage {...props} client={api} />);
    expect(
      (await screen.findByRole("link", { name: "First work" })).getAttribute(
        "href",
      ),
    ).toBe("/workspaces/wrk_main/decisions/dec_first");
    expect(screen.getByText(/项目已归档/)).toBeDefined();
    expect(
      (screen.getByRole("button", { name: "上一页" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.queryByText("Private project")).toBeNull();
    await screen.findByText("Second work");
    expect(api.decisions).toHaveBeenLastCalledWith(
      "wrk_main",
      "prj_main",
      "next_page",
      expect.any(AbortSignal),
    );
    expect(
      (screen.getByRole("button", { name: "下一页" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "上一页" }));
    await screen.findByText("First work");
    fireEvent.click(screen.getByRole("button", { name: "刷新决策列表" }));
    await screen.findByText("First work");
    expect(api.decisions).toHaveBeenLastCalledWith(
      "wrk_main",
      "prj_main",
      undefined,
      expect.any(AbortSignal),
    );
  });
  it("shows an empty list and retries errors without keeping Project metadata", async () => {
    const api = client();
    vi.mocked(api.decisions)
      .mockResolvedValueOnce({ items: [], nextCursor: null })
      .mockRejectedValueOnce(
        new AuthRequestError("unavailable", { status: 404 }),
      )
      .mockResolvedValue(page);
    render(<ProjectDecisionsPage {...props} client={api} />);
    await screen.findByText("当前项目没有可显示的决策。");
    fireEvent.click(screen.getByRole("button", { name: "刷新决策列表" }));
    await screen.findByRole("alert");
    expect(screen.queryByText(project.name)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重新读取决策" }));
    await screen.findByText("First work");
  });
  it("clears both reads when Project context fails and ignores late list results", async () => {
    const api = client();
    const late = pending<typeof page>();
    vi.mocked(api.project)
      .mockResolvedValueOnce(project)
      .mockRejectedValue(new AuthRequestError("revoked", { status: 404 }));
    vi.mocked(api.decisions)
      .mockResolvedValueOnce(page)
      .mockReturnValue(late.promise);
    render(<ProjectDecisionsPage {...props} client={api} />);
    await screen.findByText("First work");
    fireEvent.click(screen.getByRole("button", { name: "刷新决策列表" }));
    await screen.findByRole("alert");
    await act(async () => late.resolve(page));
    expect(screen.queryByText("First work")).toBeNull();
    expect(screen.queryByText(project.name)).toBeNull();
  });
  it("aborts old Project/Workspace responses and resets pagination", async () => {
    const api = client();
    const late = pending<typeof page>();
    vi.mocked(api.decisions)
      .mockReturnValueOnce(late.promise)
      .mockResolvedValue({
        items: [{ id: "dec_other", title: "Other work", status: "proposed" }],
        nextCursor: null,
      });
    const view = render(<ProjectDecisionsPage {...props} client={api} />);
    vi.mocked(api.project).mockResolvedValue({
      ...project,
      id: "prj_other",
      name: "Other project",
    });
    view.rerender(
      <ProjectDecisionsPage
        {...props}
        workspaceID="wrk_other"
        projectID="prj_other"
        client={api}
      />,
    );
    await screen.findByText("Other work");
    await act(async () => late.resolve(page));
    expect(screen.queryByText("First work")).toBeNull();
    expect(screen.queryByText(project.name)).toBeNull();
    expect(api.decisions).toHaveBeenLastCalledWith(
      "wrk_other",
      "prj_other",
      undefined,
      expect.any(AbortSignal),
    );
  });
  it.each(["focus", "visibilitychange", "pageshow", "popstate"])(
    "rechecks permission on %s and does not restore cached rows",
    async (eventName) => {
      const api = client();
      vi.mocked(api.decisions)
        .mockResolvedValueOnce(page)
        .mockResolvedValueOnce({ ...page, nextCursor: null })
        .mockRejectedValue(new AuthRequestError("revoked", { status: 404 }));
      render(<ProjectDecisionsPage {...props} client={api} />);
      await screen.findByText("First work");
      fireEvent.click(screen.getByRole("button", { name: "下一页" }));
      await screen.findByText("第 2 页");
      act(() => {
        if (eventName === "visibilitychange")
          document.dispatchEvent(new Event(eventName));
        else if (eventName === "pageshow")
          window.dispatchEvent(
            new PageTransitionEvent(eventName, { persisted: true }),
          );
        else window.dispatchEvent(new Event(eventName));
      });
      await screen.findByRole("alert");
      expect(screen.queryByText("First work")).toBeNull();
      expect(screen.queryByText(project.name)).toBeNull();
      expect(api.decisions).toHaveBeenLastCalledWith(
        "wrk_main",
        "prj_main",
        undefined,
        expect.any(AbortSignal),
      );
    },
  );
  it.each(["project", "decisions"] as const)(
    "clears private data before reporting expired Session from %s",
    async (failed) => {
      const api = client();
      const expired = vi.fn();
      if (failed === "decisions") {
        vi.mocked(api.decisions)
          .mockResolvedValueOnce(page)
          .mockRejectedValue(new AuthRequestError("expired", { status: 401 }));
      } else {
        vi.mocked(api.project)
          .mockResolvedValueOnce(project)
          .mockRejectedValue(new AuthRequestError("expired", { status: 401 }));
      }
      render(
        <ProjectDecisionsPage
          {...props}
          client={api}
          onSessionExpired={expired}
        />,
      );
      await screen.findByText("First work");
      fireEvent.click(screen.getByRole("button", { name: "刷新决策列表" }));
      await waitFor(() => expect(expired).toHaveBeenCalledOnce());
      expect(screen.queryByText("First work")).toBeNull();
      expect(screen.queryByText(project.name)).toBeNull();
    },
  );
});
