import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AuthRequestError } from "../auth/api";
import { ProjectTicketsPage } from "./ProjectTicketsPage";
import type { ProjectTicketsClient } from "./project-tickets-api";

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
  items: [{ id: "tkt_first", title: "First work", status: "open" as const }],
  nextCursor: "next_page",
};
const props = {
  workspaceID: "wrk_main",
  projectID: "prj_main",
  onSessionExpired: vi.fn(),
};
function client(): ProjectTicketsClient {
  return {
    project: vi.fn().mockResolvedValue(project),
    tickets: vi.fn().mockResolvedValue(page),
  };
}
function pending<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

describe("Project Tickets page", () => {
  it("opens details, pages and refreshes to the first page with current Project context", async () => {
    const api = client();
    vi.mocked(api.project).mockResolvedValue({
      ...project,
      status: "archived",
    });
    vi.mocked(api.tickets)
      .mockResolvedValueOnce(page)
      .mockResolvedValueOnce({
        items: [{ id: "tkt_second", title: "Second work", status: "open" }],
        nextCursor: null,
      })
      .mockResolvedValue(page);
    render(<ProjectTicketsPage {...props} client={api} />);
    expect(
      (await screen.findByRole("link", { name: "First work" })).getAttribute(
        "href",
      ),
    ).toBe("/workspaces/wrk_main/tickets/tkt_first");
    expect(screen.getByText(/项目已归档/)).toBeDefined();
    expect(
      (screen.getByRole("button", { name: "上一页" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.queryByText("Private project")).toBeNull();
    await screen.findByText("Second work");
    expect(api.tickets).toHaveBeenLastCalledWith(
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
    fireEvent.click(screen.getByRole("button", { name: "刷新工单列表" }));
    await screen.findByText("First work");
    expect(api.tickets).toHaveBeenLastCalledWith(
      "wrk_main",
      "prj_main",
      undefined,
      expect.any(AbortSignal),
    );
  });
  it("shows an empty list and retries errors without keeping Project metadata", async () => {
    const api = client();
    vi.mocked(api.tickets)
      .mockResolvedValueOnce({ items: [], nextCursor: null })
      .mockRejectedValueOnce(
        new AuthRequestError("unavailable", { status: 404 }),
      )
      .mockResolvedValue(page);
    render(<ProjectTicketsPage {...props} client={api} />);
    await screen.findByText("当前项目没有可显示的工单。");
    fireEvent.click(screen.getByRole("button", { name: "刷新工单列表" }));
    await screen.findByRole("alert");
    expect(screen.queryByText(project.name)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重新读取工单" }));
    await screen.findByText("First work");
  });
  it("clears both reads when Project context fails and ignores late list results", async () => {
    const api = client();
    const late = pending<typeof page>();
    vi.mocked(api.project)
      .mockResolvedValueOnce(project)
      .mockRejectedValue(new AuthRequestError("revoked", { status: 404 }));
    vi.mocked(api.tickets)
      .mockResolvedValueOnce(page)
      .mockReturnValue(late.promise);
    render(<ProjectTicketsPage {...props} client={api} />);
    await screen.findByText("First work");
    fireEvent.click(screen.getByRole("button", { name: "刷新工单列表" }));
    await screen.findByRole("alert");
    await act(async () => late.resolve(page));
    expect(screen.queryByText("First work")).toBeNull();
    expect(screen.queryByText(project.name)).toBeNull();
  });
  it("aborts old Project/Workspace responses and resets pagination", async () => {
    const api = client();
    const late = pending<typeof page>();
    vi.mocked(api.tickets)
      .mockReturnValueOnce(late.promise)
      .mockResolvedValue({
        items: [{ id: "tkt_other", title: "Other work", status: "open" }],
        nextCursor: null,
      });
    const view = render(<ProjectTicketsPage {...props} client={api} />);
    vi.mocked(api.project).mockResolvedValue({
      ...project,
      id: "prj_other",
      name: "Other project",
    });
    view.rerender(
      <ProjectTicketsPage
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
    expect(api.tickets).toHaveBeenLastCalledWith(
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
      vi.mocked(api.tickets)
        .mockResolvedValueOnce(page)
        .mockRejectedValue(new AuthRequestError("revoked", { status: 404 }));
      render(<ProjectTicketsPage {...props} client={api} />);
      await screen.findByText("First work");
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
    },
  );
  it("clears private data before reporting an expired Session", async () => {
    const api = client();
    const expired = vi.fn();
    vi.mocked(api.tickets)
      .mockResolvedValueOnce(page)
      .mockRejectedValue(new AuthRequestError("expired", { status: 401 }));
    render(
      <ProjectTicketsPage {...props} client={api} onSessionExpired={expired} />,
    );
    await screen.findByText("First work");
    fireEvent.click(screen.getByRole("button", { name: "刷新工单列表" }));
    await waitFor(() => expect(expired).toHaveBeenCalledOnce());
    expect(screen.queryByText("First work")).toBeNull();
    expect(screen.queryByText(project.name)).toBeNull();
  });
});
