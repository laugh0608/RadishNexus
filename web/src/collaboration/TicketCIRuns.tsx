import { useCallback, useState, type ReactNode } from "react";
import { useExecutionWrite, requestStatus } from "./execution-write";
import { OperationStatus } from "./ExecutionPanels";
import { usePage } from "../workspace/configuration-hooks";
import { deliveryConfigurationClient } from "../workspace/delivery-configuration-api";
import {
  buildStatusLabels,
  ciRunTraceClient,
  type BuildCandidate,
  type TicketRun,
} from "../nexus-view/ci-run-trace-api";
import { ciRunPagePath } from "../nexus-view/ci-run-api";

type Scope = {
  workspace: string;
  ticket: string;
  onSessionExpired: () => void;
};

export function TicketCIRuns({
  workspace,
  ticket,
  onSessionExpired,
  onUnavailable,
}: Scope & { onUnavailable: () => void }) {
  const [picking, setPicking] = useState(false);
  const [history, setHistory] = useState(false);
  const write = useExecutionWrite({ onSessionExpired });
  const clear = write.clear;
  const [review, setReview] = useState<{
    build: BuildCandidate;
    link?: string;
  } | null>(null);
  const revision = write.revision;
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      void revision;
      try {
        const page = await ciRunTraceClient.ticketRuns(
          workspace,
          ticket,
          after,
          signal,
        );
        if (!signal.aborted && !page.canLink) {
          clear();
          setPicking(false);
          setReview(null);
        }
        return page;
      } catch (error) {
        if (
          !signal.aborted &&
          [401, 403, 404].includes(requestStatus(error) ?? 0)
        ) {
          clear();
          setPicking(false);
          setReview(null);
          onUnavailable();
        }
        throw error;
      }
    },
    [workspace, ticket, clear, onUnavailable, revision],
  );
  const runs = usePage<
    TicketRun,
    Awaited<ReturnType<typeof ciRunTraceClient.ticketRuns>>
  >(load, onSessionExpired);
  const save = () => {
    if (!review || !runs.page?.canLink) return;
    const change = review.link
      ? { link: review.link }
      : { run: review.build.id };
    setPicking(false);
    setReview(null);
    write.start(
      review.link ? "解除构建关联" : "关联构建",
      async (operation, signal) => {
        await ciRunTraceClient.write(
          workspace,
          ticket,
          change,
          operation,
          signal,
        );
        if (!signal.aborted) runs.refresh();
      },
    );
  };
  const canEdit = runs.page?.canLink && !write.pending;
  return (
    <section className="collaboration-panel" aria-label="关联构建">
      <h2>关联构建</h2>
      <TraceList
        state={runs}
        empty="该工单暂无可读的构建关联。"
        row={(run) => (
          <>
            <a href={ciRunPagePath(workspace, run.id)!}>{run.id}</a>
            <BuildDetails build={run} />
            {canEdit && run.canUnlink && (
              <button
                type="button"
                onClick={() => {
                  setPicking(false);
                  setReview({ build: run, link: run.linkID });
                }}
              >
                解除关联
              </button>
            )}
          </>
        )}
      />
      {canEdit && !review && (
        <button
          type="button"
          aria-expanded={picking}
          onClick={() => setPicking((open) => !open)}
        >
          选择构建
        </button>
      )}
      {canEdit && picking && !review && (
        <BuildPicker
          workspace={workspace}
          onSessionExpired={onSessionExpired}
          onSelect={(build) => setReview({ build })}
        />
      )}
      {canEdit && review && (
        <section aria-label="确认构建关联">
          <p>
            {review.link ? "确认解除" : "确认关联"} {review.build.id}
          </p>
          <BuildDetails build={review.build} />
          <button type="button" onClick={save}>
            {review.link ? "确认解除构建" : "确认关联构建"}
          </button>
          <button type="button" onClick={() => setReview(null)}>
            返回
          </button>
        </section>
      )}
      {canEdit || runs.page?.canLink ? <OperationStatus write={write} /> : null}
      {runs.page && (
        <button
          type="button"
          aria-expanded={history}
          onClick={() => setHistory((open) => !open)}
        >
          构建关联历史
        </button>
      )}
      {runs.page && history && (
        <BuildHistory
          key={revision}
          workspace={workspace}
          ticket={ticket}
          onSessionExpired={onSessionExpired}
        />
      )}
    </section>
  );
}

function BuildPicker({
  workspace,
  onSessionExpired,
  onSelect,
}: Omit<Scope, "ticket"> & { onSelect: (build: BuildCandidate) => void }) {
  const [component, setComponent] = useState<{
    id: string;
    label: string;
  } | null>(null);
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      deliveryConfigurationClient.list(workspace, "component", after, signal),
    [workspace],
  );
  const components = usePage(load, onSessionExpired);
  return component ? (
    <section aria-label="选择 CI Run">
      <h3>{component.label}</h3>
      <button type="button" onClick={() => setComponent(null)}>
        更换组件
      </button>
      <BuildCandidates
        key={component.id}
        workspace={workspace}
        component={component.id}
        onSessionExpired={onSessionExpired}
        onSelect={onSelect}
      />
    </section>
  ) : (
    <section aria-label="选择组件">
      <h3>选择组件</h3>
      <TraceList
        state={components}
        empty="当前没有可读的组件。"
        row={(entry) => (
          <button
            type="button"
            onClick={() => setComponent({ id: entry.id, label: entry.name })}
          >
            {entry.name}
          </button>
        )}
      />
    </section>
  );
}

function BuildCandidates({
  workspace,
  component,
  onSessionExpired,
  onSelect,
}: Omit<Scope, "ticket"> & {
  component: string;
  onSelect: (build: BuildCandidate) => void;
}) {
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      ciRunTraceClient.candidates(workspace, component, after, signal),
    [workspace, component],
  );
  const runs = usePage(load, onSessionExpired);
  return (
    <TraceList
      state={runs}
      empty="该组件暂无可选构建。"
      row={(run) => (
        <>
          <button type="button" onClick={() => onSelect(run)}>
            选择 {run.id}
          </button>
          <BuildDetails build={run} />
        </>
      )}
    />
  );
}

function BuildHistory({ workspace, ticket, onSessionExpired }: Scope) {
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      ciRunTraceClient.history(workspace, ticket, after, signal),
    [workspace, ticket],
  );
  const history = usePage(load, onSessionExpired);
  return (
    <section aria-label="构建关联历史">
      <TraceList
        state={history}
        empty="该工单暂无可读的构建关联历史。"
        row={(event) => (
          <>
            {event.state === "active" ? "关联" : "解除"}{" "}
            <a href={ciRunPagePath(workspace, event.runID)!}>{event.runID}</a>{" "}
            <time dateTime={event.at}>
              {new Date(event.at).toLocaleString()}
            </time>{" "}
            <span>{event.actor}</span>
          </>
        )}
      />
    </section>
  );
}

function BuildDetails({ build }: { build: BuildCandidate }) {
  return (
    <p>
      {build.component.name} · {buildStatusLabels[build.status]} ·{" "}
      <time dateTime={build.completedAt}>
        {new Date(build.completedAt).toLocaleString()}
      </time>
    </p>
  );
}

export function TraceList<T extends { id: string }>({
  state,
  empty,
  row,
}: {
  state: {
    page?: { items: readonly T[]; nextCursor: string | null };
    error?: string;
    hasPrevious: boolean;
    previous: () => void;
    next: () => void;
    refresh: () => void;
  };
  empty: string;
  row: (item: T) => ReactNode;
}) {
  if (state.error)
    return (
      <>
        <p role="status">{state.error}</p>
        <button type="button" onClick={state.refresh}>
          重新读取
        </button>
      </>
    );
  if (!state.page) return <p role="status">正在读取…</p>;
  return (
    <>
      {state.page.items.length ? (
        <ul className="configuration-list">
          {state.page.items.map((item) => (
            <li key={item.id}>{row(item)}</li>
          ))}
        </ul>
      ) : (
        <p>{empty}</p>
      )}
      {state.hasPrevious && (
        <button type="button" onClick={state.previous}>
          上一页
        </button>
      )}
      {state.page.nextCursor && (
        <button type="button" onClick={state.next}>
          下一页
        </button>
      )}
    </>
  );
}
