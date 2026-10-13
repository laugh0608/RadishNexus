import { ticketStatusLabels } from "../collaboration/ticket-status";
import { useCallback, useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { collaborationPagePath } from "../collaboration/api";
import {
  projectTicketsClient,
  type ProjectTicketsClient,
} from "./project-tickets-api";

interface Props {
  workspaceID: string;
  projectID: string;
  onSessionExpired: () => void;
  client?: ProjectTicketsClient;
}

type PageState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | {
      status: "ready";
      project: Awaited<ReturnType<ProjectTicketsClient["project"]>>;
      page: Awaited<ReturnType<ProjectTicketsClient["tickets"]>>;
    };

export function ProjectTicketsPage(props: Props) {
  return (
    <ProjectTickets
      key={`${props.workspaceID}/${props.projectID}`}
      {...props}
    />
  );
}

function ProjectTickets({
  workspaceID,
  projectID,
  onSessionExpired,
  client = projectTicketsClient,
}: Props) {
  const [state, setState] = useState<PageState>({ status: "loading" });
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [revision, setRevision] = useState(0);
  const activeRequest = useRef<AbortController | null>(null);
  const cursor = cursors[cursors.length - 1];
  const changePage = useCallback((next: (string | undefined)[]) => {
    activeRequest.current?.abort();
    setState({ status: "loading" });
    setCursors(next);
    setRevision((value) => value + 1);
  }, []);
  const refresh = useCallback(() => changePage([undefined]), [changePage]);

  useEffect(() => {
    const controller = new AbortController();
    activeRequest.current = controller;
    // The heading and list share a request generation. Neither survives a
    // denied/failed read of the other, nor a late response after navigation.
    void Promise.all([
      client.project(workspaceID, projectID, controller.signal),
      client.tickets(workspaceID, projectID, cursor, controller.signal),
    ]).then(
      ([project, page]) => {
        if (!controller.signal.aborted)
          setState({ status: "ready", project, page });
      },
      (error: unknown) => {
        if (controller.signal.aborted) return;
        controller.abort();
        setState({
          status: "error",
          message:
            error instanceof AuthRequestError && error.status === 404
              ? "该项目已不可访问，请返回工作台。"
              : "无法读取项目工单，请重试。",
        });
        if (error instanceof AuthRequestError && error.status === 401)
          onSessionExpired();
      },
    );
    return () => controller.abort();
  }, [client, cursor, onSessionExpired, projectID, revision, workspaceID]);

  useEffect(() => {
    const visibility = () => {
      if (document.visibilityState === "visible") refresh();
    };
    const pageShow = (event: PageTransitionEvent) => {
      if (event.persisted) refresh();
    };
    const pageHide = () => {
      activeRequest.current?.abort();
      setState({ status: "loading" });
    };
    window.addEventListener("focus", refresh);
    window.addEventListener("pageshow", pageShow);
    window.addEventListener("pagehide", pageHide);
    window.addEventListener("popstate", refresh);
    document.addEventListener("visibilitychange", visibility);
    return () => {
      window.removeEventListener("focus", refresh);
      window.removeEventListener("pageshow", pageShow);
      window.removeEventListener("pagehide", pageHide);
      window.removeEventListener("popstate", refresh);
      document.removeEventListener("visibilitychange", visibility);
    };
  }, [refresh]);

  return (
    <main className="configuration-panel project-tickets" aria-label="项目工单">
      <a href="/">返回工作台</a>
      <h1>项目工单</h1>
      <button type="button" onClick={refresh}>
        刷新工单列表
      </button>
      {state.status === "loading" ? (
        <p role="status">正在读取项目工单…</p>
      ) : state.status === "error" ? (
        <div>
          <p role="alert">{state.message}</p>
          <button type="button" onClick={refresh}>
            重新读取工单
          </button>
        </div>
      ) : (
        <>
          <h2>{state.project.name}</h2>
          {state.project.status === "archived" ? (
            <p>项目已归档 · 可浏览历史工单</p>
          ) : null}
          {state.page.items.length === 0 ? (
            <p>当前项目没有可显示的工单。</p>
          ) : (
            <ul className="configuration-list">
              {state.page.items.map((ticket) => (
                <li key={ticket.id}>
                  <a
                    href={collaborationPagePath(
                      workspaceID,
                      "ticket",
                      ticket.id,
                    )!}
                  >
                    {ticket.title}
                  </a>
                  <p>{ticketStatusLabels[ticket.status]}</p>
                </li>
              ))}
            </ul>
          )}
          <nav className="discovery-pagination" aria-label="工单分页">
            <button
              type="button"
              disabled={cursors.length === 1}
              onClick={() => changePage(cursors.slice(0, -1))}
            >
              上一页
            </button>
            <span>第 {cursors.length} 页</span>
            <button
              type="button"
              disabled={state.page.nextCursor === null}
              onClick={() => {
                if (state.page.nextCursor !== null)
                  changePage([...cursors, state.page.nextCursor]);
              }}
            >
              下一页
            </button>
          </nav>
        </>
      )}
    </main>
  );
}
