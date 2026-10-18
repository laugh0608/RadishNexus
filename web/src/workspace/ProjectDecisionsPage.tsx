import { useCallback, useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { collaborationPagePath } from "../collaboration/api";
import {
  projectDecisionsClient,
  type ProjectDecisionsClient,
} from "./project-decisions-api";

interface Props {
  workspaceID: string;
  projectID: string;
  onSessionExpired: () => void;
  client?: ProjectDecisionsClient;
}

type PageState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | {
      status: "ready";
      project: Awaited<ReturnType<ProjectDecisionsClient["project"]>>;
      page: Awaited<ReturnType<ProjectDecisionsClient["decisions"]>>;
    };

export function ProjectDecisionsPage(props: Props) {
  return (
    <ProjectDecisions
      key={`${props.workspaceID}/${props.projectID}`}
      {...props}
    />
  );
}

function ProjectDecisions({
  workspaceID,
  projectID,
  onSessionExpired,
  client = projectDecisionsClient,
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
      client.decisions(workspaceID, projectID, cursor, controller.signal),
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
              : "无法读取项目决策，请重试。",
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
    <main
      className="configuration-panel project-decisions"
      aria-label="项目决策"
    >
      <a href="/">返回工作台</a>
      <h1>项目决策</h1>
      <button type="button" onClick={refresh}>
        刷新决策列表
      </button>
      {state.status === "loading" ? (
        <p role="status">正在读取项目决策…</p>
      ) : state.status === "error" ? (
        <div>
          <p role="alert">{state.message}</p>
          <button type="button" onClick={refresh}>
            重新读取决策
          </button>
        </div>
      ) : (
        <>
          <h2>{state.project.name}</h2>
          {state.project.status === "archived" ? (
            <p>项目已归档 · 可浏览历史决策</p>
          ) : null}
          {state.page.items.length === 0 ? (
            <p>当前项目没有可显示的决策。</p>
          ) : (
            <ul className="configuration-list">
              {state.page.items.map((decision) => (
                <li key={decision.id}>
                  <a
                    href={collaborationPagePath(
                      workspaceID,
                      "decision",
                      decision.id,
                    )!}
                  >
                    {decision.title}
                  </a>
                  <p>{decision.status === "proposed" ? "待确认" : "已确认"}</p>
                </li>
              ))}
            </ul>
          )}
          <nav className="discovery-pagination" aria-label="决策分页">
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
