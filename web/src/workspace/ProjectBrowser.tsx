import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { AuthRequestError } from "../auth/api";
import { channelPagePath } from "../channel/api";
import type { DiscoveryClient, DiscoveryItem, DiscoveryPage } from "./api";

type PageState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; page: DiscoveryPage };

export function ProjectBrowser({
  workspaceID,
  client,
  navigate,
  onSessionExpired,
}: {
  workspaceID: string;
  client: DiscoveryClient;
  navigate: (path: string) => void;
  onSessionExpired: () => void;
}) {
  const [selected, setSelected] = useState<DiscoveryItem | null>(null);
  const [generation, setGeneration] = useState(0);
  const refresh = useCallback(() => {
    setSelected(null);
    setGeneration((value) => value + 1);
  }, []);
  useEffect(() => {
    const onVisibility = () => {
      if (document.visibilityState === "visible") refresh();
    };
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [refresh]);
  const loadProjects = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      client.projects(workspaceID, after, signal),
    [client, workspaceID],
  );

  return (
    <section className="project-browser" aria-label="项目与频道">
      <div className="discovery-heading">
        <div>
          <h2>项目与频道</h2>
          <p>选择项目，继续你的讨论与协作。</p>
        </div>
        <button type="button" onClick={refresh}>
          刷新项目列表
        </button>
      </div>
      <div className="discovery-columns" key={generation}>
        <DiscoveryList
          label="项目"
          load={loadProjects}
          onSessionExpired={onSessionExpired}
          onPageChange={() => setSelected(null)}
          renderItem={(item) => (
            <button
              type="button"
              aria-pressed={selected?.id === item.id}
              onClick={() => setSelected(item)}
            >
              <strong>{item.title}</strong>
              {item.status === "archived" ? (
                <span className="discovery-status">已归档</span>
              ) : null}
            </button>
          )}
        />
        {selected === null ? (
          <div className="discovery-placeholder">
            选择一个项目，查看可访问的频道。
          </div>
        ) : (
          <ProjectChannels
            key={selected.id}
            workspaceID={workspaceID}
            project={selected}
            onUnavailable={refresh}
            client={client}
            navigate={navigate}
            onSessionExpired={onSessionExpired}
          />
        )}
      </div>
    </section>
  );
}

function ProjectChannels({
  workspaceID,
  project,
  client,
  navigate,
  onSessionExpired,
  onUnavailable,
}: {
  workspaceID: string;
  project: DiscoveryItem;
  client: DiscoveryClient;
  navigate: (path: string) => void;
  onSessionExpired: () => void;
  onUnavailable: () => void;
}) {
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      client.channels(workspaceID, project.id, after, signal),
    [client, project.id, workspaceID],
  );
  return (
    <DiscoveryList
      label={`${project.title}的频道`}
      load={load}
      onSessionExpired={onSessionExpired}
      onUnavailable={onUnavailable}
      renderItem={(item) => (
        <button
          type="button"
          onClick={() => {
            const path = channelPagePath(workspaceID, item.id);
            if (path !== null) navigate(path);
          }}
        >
          <strong>{item.title}</strong>
          {item.status === "archived" || project.status === "archived" ? (
            <span className="discovery-status">已归档 · 可浏览</span>
          ) : (
            <span>打开频道 →</span>
          )}
        </button>
      )}
    />
  );
}

function DiscoveryList({
  label,
  load,
  onSessionExpired,
  onPageChange,
  onUnavailable,
  renderItem,
}: {
  label: string;
  load: (
    after: string | undefined,
    signal: AbortSignal,
  ) => Promise<DiscoveryPage>;
  onSessionExpired: () => void;
  onPageChange?: () => void;
  onUnavailable?: () => void;
  renderItem: (item: DiscoveryItem) => ReactNode;
}) {
  const [state, setState] = useState<PageState>({ status: "loading" });
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [revision, setRevision] = useState(0);
  const activeRequest = useRef<AbortController | null>(null);
  const cursor = cursors[cursors.length - 1];
  useEffect(() => {
    const controller = new AbortController();
    activeRequest.current = controller;
    void load(cursor, controller.signal).then(
      (page) => {
        if (!controller.signal.aborted) setState({ status: "ready", page });
      },
      (error: unknown) => {
        if (controller.signal.aborted) return;
        if (error instanceof AuthRequestError && error.status === 401) {
          onSessionExpired();
          return;
        }
        if (
          error instanceof AuthRequestError &&
          error.status === 404 &&
          onUnavailable
        ) {
          onUnavailable();
          return;
        }
        setState({
          status: "error",
          message:
            error instanceof AuthRequestError
              ? error.userMessage
              : "内容加载失败，请稍后重试。",
        });
      },
    );
    return () => controller.abort();
  }, [cursor, load, onSessionExpired, onUnavailable, revision]);
  const changePage = (next: (string | undefined)[]) => {
    activeRequest.current?.abort();
    setState({ status: "loading" });
    onPageChange?.();
    setCursors(next);
    setRevision((value) => value + 1);
  };
  return (
    <section className="discovery-list" aria-label={label}>
      <h3>{label}</h3>
      {state.status === "loading" ? (
        <p role="status">正在加载{label}…</p>
      ) : state.status === "error" ? (
        <div>
          <p role="alert">{state.message}</p>
          <button type="button" onClick={() => changePage([undefined])}>
            重新加载{label}
          </button>
        </div>
      ) : (
        <>
          {state.page.items.length === 0 ? (
            <p className="discovery-empty">
              当前没有可访问的{label === "项目" ? "项目" : "频道"}。
            </p>
          ) : (
            <ul>
              {state.page.items.map((item) => (
                <li key={item.id}>{renderItem(item)}</li>
              ))}
            </ul>
          )}
          <div className="discovery-pagination">
            <button
              type="button"
              disabled={cursors.length === 1}
              onClick={() => changePage(cursors.slice(0, -1))}
            >
              上一页{label === "项目" ? "项目" : "频道"}
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
              下一页{label === "项目" ? "项目" : "频道"}
            </button>
          </div>
        </>
      )}
    </section>
  );
}
