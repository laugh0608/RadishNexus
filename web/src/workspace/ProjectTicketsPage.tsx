import {
  ClassificationText,
  LabelPicker,
} from "../collaboration/TicketClassificationFields";
import {
  parseTicketType,
  ticketTypeLabels,
} from "../collaboration/ticket-classification-api";
import { TicketLabelDirectory } from "./TicketLabelDirectory";
import { ticketDueDateLabel } from "../collaboration/ticket-due-date";
import {
  ticketPriorityLabels,
  type TicketPriority,
} from "../collaboration/ticket-priority";
import { assigneeLabel } from "../collaboration/ticket-assignee-api";
import {
  ticketStatusLabels,
  type TicketStatus,
} from "../collaboration/ticket-status";
import { useCallback, useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { collaborationPagePath } from "../collaboration/api";
import {
  projectTicketsClient,
  type ProjectTicket,
  type ProjectTicketsClient,
  type TicketFilters,
} from "./project-tickets-api";
import { usePage } from "./configuration-hooks";

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
      page: Awaited<ReturnType<ProjectTicketsClient["tickets"]>> | null;
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
  const [titleQuery, setTitleQuery] = useState("");
  const [showLabels, setShowLabels] = useState(false);
  const [showDirectory, setShowDirectory] = useState(false);
  const [filters, setFilters] = useState<TicketFilters>({});
  const [view, setView] = useState<"list" | "board">("list");
  const [revision, setRevision] = useState(0);
  const activeRequest = useRef<AbortController | null>(null);
  const cursor = cursors[cursors.length - 1];
  const changePage = useCallback((next: (string | undefined)[]) => {
    activeRequest.current?.abort();
    setState({ status: "loading" });
    setCursors(next);
    setRevision((value) => value + 1);
  }, []);
  const changeFilters = (next: TicketFilters) => {
    setFilters(next);
    changePage([undefined]);
  };
  const refresh = useCallback(() => changePage([undefined]), [changePage]);
  const changeView = (next: typeof view) => {
    if (next === view) return;
    setView(next);
    refresh();
  };
  const unavailable = useCallback(() => {
    activeRequest.current?.abort();
    setShowLabels(false);
    setShowDirectory(false);
    setState({ status: "error", message: "该项目已不可访问，请返回工作台。" });
  }, []);
  const sessionExpired = useCallback(() => {
    setState({ status: "loading" });
    onSessionExpired();
  }, [onSessionExpired]);

  useEffect(() => {
    const controller = new AbortController();
    activeRequest.current = controller;
    // The heading and list share a request generation. Neither survives a
    // denied/failed read of the other, nor a late response after navigation.
    void Promise.all([
      client.project(workspaceID, projectID, controller.signal),
      view === "list"
        ? client.tickets(
            workspaceID,
            projectID,
            cursor,
            controller.signal,
            filters,
          )
        : null,
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
  }, [
    client,
    cursor,
    filters,
    onSessionExpired,
    projectID,
    revision,
    view,
    workspaceID,
  ]);

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
      <div className="configuration-actions" role="group" aria-label="工单视图">
        <button
          type="button"
          aria-pressed={view === "list"}
          onClick={() => changeView("list")}
        >
          列表
        </button>
        <button
          type="button"
          aria-pressed={view === "board"}
          onClick={() => changeView("board")}
        >
          状态看板
        </button>
      </div>
      <button type="button" onClick={refresh}>
        刷新当前筛选
      </button>
      <fieldset className="ticket-filters">
        <legend>筛选项目工单</legend>
        {view === "list" ? (
          <label>
            状态
            <select
              value={filters.status ?? ""}
              onChange={(e) =>
                changeFilters({
                  ...filters,
                  status:
                    e.target.value === ""
                      ? undefined
                      : (e.target.value as TicketFilters["status"]),
                })
              }
            >
              <option value="">全部状态</option>
              <option value="active">未结束</option>
              {Object.entries(ticketStatusLabels).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </label>
        ) : null}
        <label>
          负责人
          <select
            value={filters.assignee ?? ""}
            onChange={(e) =>
              changeFilters({
                ...filters,
                assignee:
                  e.target.value === ""
                    ? undefined
                    : (e.target.value as TicketFilters["assignee"]),
              })
            }
          >
            <option value="">全部负责人</option>
            <option value="me">由我负责</option>
            <option value="unassigned">未分配</option>
          </select>
        </label>
        <label>
          优先级
          <select
            value={filters.priority ?? ""}
            onChange={(e) =>
              changeFilters({
                ...filters,
                priority:
                  e.target.value === ""
                    ? undefined
                    : (e.target.value as TicketPriority),
              })
            }
          >
            <option value="">全部优先级</option>
            {Object.entries(ticketPriorityLabels).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label>
          类型
          <select
            value={filters.type ?? ""}
            onChange={(e) =>
              changeFilters({
                ...filters,
                type: e.target.value
                  ? parseTicketType(e.target.value)
                  : undefined,
              })
            }
          >
            <option value="">全部类型</option>
            {Object.entries(ticketTypeLabels).map(([type, label]) => (
              <option key={type} value={type}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <form
          role="search"
          onSubmit={(event) => {
            event.preventDefault();
            if ([...titleQuery.trim()].length <= 80)
              changeFilters({ ...filters, q: titleQuery.trim() || undefined });
          }}
        >
          <label>
            工单标题
            <input
              type="search"
              value={titleQuery}
              onChange={(event) => setTitleQuery(event.target.value)}
            />
          </label>
          <button disabled={[...titleQuery.trim()].length > 80} type="submit">
            查找工单
          </button>
          {[...titleQuery.trim()].length > 80 && (
            <p role="status">标题查询最多 80 个字符。</p>
          )}
        </form>
        <button
          type="button"
          aria-expanded={showLabels}
          disabled={state.status === "error"}
          onClick={() => setShowLabels((value) => !value)}
        >
          按标签筛选
        </button>
        {showLabels && state.status !== "error" && (
          <LabelPicker
            workspace={workspaceID}
            project={projectID}
            onSessionExpired={onSessionExpired}
            includeArchived
            selected={filters.labels?.split(",") ?? []}
            onChange={(ids) =>
              changeFilters({ ...filters, labels: ids.join(",") || undefined })
            }
          />
        )}
        <button
          type="button"
          onClick={() => {
            setTitleQuery("");
            changeFilters({});
          }}
        >
          重置筛选
        </button>
      </fieldset>
      <p>
        {view === "board"
          ? "各状态独立翻页。打开工单详情可继续处理。"
          : "按稳定标识排序。工单变化后可刷新当前筛选，重新读取首页。"}
      </p>
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
          <button
            type="button"
            aria-expanded={showDirectory}
            onClick={() => setShowDirectory((value) => !value)}
          >
            项目标签目录
          </button>
          {showDirectory && (
            <TicketLabelDirectory
              workspace={workspaceID}
              project={projectID}
              onSessionExpired={onSessionExpired}
              onUnavailable={unavailable}
            />
          )}
          {state.project.status === "archived" ? (
            <p>项目已归档 · 可浏览历史工单</p>
          ) : null}
          {state.page === null ? (
            <div className="ticket-board">
              {(Object.keys(ticketStatusLabels) as TicketStatus[]).map(
                (status) => (
                  <TicketBoardColumn
                    key={status}
                    status={status}
                    workspaceID={workspaceID}
                    projectID={projectID}
                    filters={filters}
                    client={client}
                    onUnavailable={unavailable}
                    onSessionExpired={sessionExpired}
                  />
                ),
              )}
            </div>
          ) : (
            <>
              <TicketRows workspaceID={workspaceID} items={state.page.items} />
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
                    const next = state.page?.nextCursor;
                    if (next) changePage([...cursors, next]);
                  }}
                >
                  下一页
                </button>
              </nav>
            </>
          )}
        </>
      )}
    </main>
  );
}

function TicketRows({
  workspaceID,
  items,
  showStatus = true,
}: {
  workspaceID: string;
  items: readonly ProjectTicket[];
  showStatus?: boolean;
}) {
  if (items.length === 0) return <p>当前条件下没有可显示的工单。</p>;
  return (
    <ul className="configuration-list">
      {items.map((ticket) => (
        <li key={ticket.id}>
          <a href={collaborationPagePath(workspaceID, "ticket", ticket.id)!}>
            {ticket.title}
          </a>
          {showStatus ? <p>{ticketStatusLabels[ticket.status]}</p> : null}
          <p>优先级：{ticketPriorityLabels[ticket.priority]}</p>
          <p>截止日期：{ticketDueDateLabel(ticket.dueDate)}</p>
          <p>负责人：{assigneeLabel(ticket.assignee)}</p>
          <p>
            <ClassificationText value={ticket.classification} />
          </p>
        </li>
      ))}
    </ul>
  );
}

function TicketBoardColumn({
  status,
  workspaceID,
  projectID,
  filters,
  client,
  onUnavailable,
  onSessionExpired,
}: Required<Props> & {
  status: TicketStatus;
  filters: TicketFilters;
  onUnavailable: () => void;
}) {
  const label = ticketStatusLabels[status];
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      try {
        return await client.tickets(workspaceID, projectID, after, signal, {
          ...filters,
          status,
        });
      } catch (error) {
        if (
          !signal.aborted &&
          error instanceof AuthRequestError &&
          (error.status === 403 || error.status === 404)
        ) {
          onUnavailable();
        }
        throw error;
      }
    },
    [filters, client, onUnavailable, projectID, status, workspaceID],
  );
  const column = usePage(load, onSessionExpired);
  return (
    <section className="ticket-board-column" aria-label={label}>
      <h3>{label}</h3>
      {column.error !== undefined ? (
        <>
          <p role="alert">无法读取{label}工单，请重试。</p>
          <button type="button" onClick={column.refresh}>
            重新读取
          </button>
        </>
      ) : column.page ? (
        <>
          <TicketRows
            workspaceID={workspaceID}
            items={column.page.items}
            showStatus={false}
          />
          <nav className="discovery-pagination" aria-label={`${label}工单分页`}>
            <button
              type="button"
              disabled={!column.hasPrevious}
              onClick={column.previous}
            >
              上一页
            </button>
            <button
              type="button"
              disabled={column.page.nextCursor === null}
              onClick={column.next}
            >
              下一页
            </button>
          </nav>
        </>
      ) : (
        <p role="status">正在读取{label}工单…</p>
      )}
    </section>
  );
}
