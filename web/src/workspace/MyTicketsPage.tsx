import { ticketDueDateLabel } from "../collaboration/ticket-due-date";
import { useCallback, useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { collaborationPagePath } from "../collaboration/api";
import { ticketPriorityLabels } from "../collaboration/ticket-priority";
import { ticketStatusLabels } from "../collaboration/ticket-status";
import { readMyTickets } from "./my-tickets-api";

type PageState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; page: Awaited<ReturnType<typeof readMyTickets>> };

export function MyTicketsPage({
  workspaceID,
  onSessionExpired,
}: {
  workspaceID: string;
  onSessionExpired: () => void;
}) {
  const [state, setState] = useState<PageState>({ status: "loading" });
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const activeRequest = useRef<AbortController | null>(null);
  const clear = useCallback(() => {
    activeRequest.current?.abort();
    setState({ status: "loading" });
  }, []);
  const changePage = useCallback(
    (next: (string | undefined)[]) => {
      clear();
      setCursors(next);
    },
    [clear],
  );
  const refresh = useCallback(() => changePage([undefined]), [changePage]);

  useEffect(() => {
    const controller = new AbortController();
    activeRequest.current = controller;
    void readMyTickets(workspaceID, cursors.at(-1), controller.signal).then(
      (page) => {
        if (!controller.signal.aborted) setState({ status: "ready", page });
      },
      (error: unknown) => {
        if (controller.signal.aborted) return;
        const status =
          error instanceof AuthRequestError ? error.status : undefined;
        if (status === 401) {
          onSessionExpired();
          return;
        }
        setState({
          status: "error",
          message:
            status === 403 || status === 404
              ? "当前无法访问此工作区的工单。"
              : "无法读取我的工单，请重试。",
        });
      },
    );
    return () => controller.abort();
  }, [cursors, onSessionExpired, workspaceID]);

  useEffect(() => {
    const visible = () => {
      if (document.visibilityState === "visible") refresh();
    };
    const pageShow = (event: PageTransitionEvent) => {
      if (event.persisted) refresh();
    };
    window.addEventListener("focus", refresh);
    window.addEventListener("pageshow", pageShow);
    window.addEventListener("pagehide", clear);
    document.addEventListener("visibilitychange", visible);
    return () => {
      window.removeEventListener("focus", refresh);
      window.removeEventListener("pageshow", pageShow);
      window.removeEventListener("pagehide", clear);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [clear, refresh]);

  return (
    <main className="configuration-panel project-tickets" aria-label="我的工单">
      <a href="/">返回工作台</a>
      <h1>我的工单</h1>
      <p>当前工作区中由你负责的待处理工单。</p>
      <button type="button" onClick={refresh}>
        刷新工单列表
      </button>
      {state.status === "loading" ? (
        <p role="status">正在读取我的工单…</p>
      ) : state.status === "error" ? (
        <p role="alert">{state.message}</p>
      ) : (
        <>
          {state.page.items.length === 0 ? (
            <p>这一页没有待处理工单。</p>
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
                  <p>
                    {ticket.project.name} · {ticketStatusLabels[ticket.status]}{" "}
                    · 优先级 {ticketPriorityLabels[ticket.priority]}
                    {" · 截止日期 "}
                    {ticketDueDateLabel(ticket.dueDate)}
                  </p>
                </li>
              ))}
            </ul>
          )}
          <nav className="discovery-pagination" aria-label="我的工单分页">
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
