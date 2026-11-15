import { useCallback, useState } from "react";
import { deploymentNexusViewPagePath } from "../nexus-view/api";
import { ciRunPagePath } from "../nexus-view/ci-run-api";
import {
  buildStatusLabels,
  type BuildCandidate,
} from "../nexus-view/ci-run-trace-api";
import { usePage } from "../workspace/configuration-hooks";
import { collaborationPagePath } from "./api";
import { requestStatus } from "./execution-write";
import {
  threadDeliveryClient,
  deliveryOriginLabels,
  type DeliveryTicket,
} from "./thread-delivery-api";
import { ticketStatusLabels } from "./ticket-status";
import { TraceList } from "./TicketCIRuns";

type ThreadScope = {
  workspace: string;
  thread: string;
  onSessionExpired: () => void;
  onUnavailable: () => void;
};

type TicketScope = ThreadScope & { ticket: string };

function pathUnavailable(error: unknown, signal: AbortSignal) {
  const status = requestStatus(error);
  return !signal.aborted && (status === 403 || status === 404);
}

export function ThreadDelivery({
  workspace,
  thread,
  onSessionExpired,
  onUnavailable,
}: ThreadScope) {
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      try {
        return await threadDeliveryClient.tickets(
          workspace,
          thread,
          after,
          signal,
        );
      } catch (error) {
        if (pathUnavailable(error, signal)) onUnavailable();
        throw error;
      }
    },
    [workspace, thread, onUnavailable],
  );
  // The enclosing Thread handles focus, visibility and history and remounts the
  // entire tree on a fresh read.
  const tickets = usePage(load, onSessionExpired, { refreshOnFocus: false });
  return (
    <section className="collaboration-panel" aria-label="执行与交付">
      <h2>执行与交付</h2>
      {tickets.page && (
        <button type="button" onClick={tickets.refresh}>
          重新读取执行与交付
        </button>
      )}
      <TraceList
        state={tickets}
        empty="当前讨论暂无可读的交付工单。"
        row={(item) => (
          <DeliveryTicketRow
            workspace={workspace}
            thread={thread}
            item={item}
            onSessionExpired={onSessionExpired}
            onUnavailable={tickets.refresh}
          />
        )}
      />
    </section>
  );
}

function DeliveryTicketRow({
  item,
  ...scope
}: ThreadScope & { item: DeliveryTicket }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <>
      <a href={collaborationPagePath(scope.workspace, "ticket", item.id)!}>
        {item.title}
      </a>
      <p>
        {ticketStatusLabels[item.status]} ·{" "}
        {item.origins.map((origin) => deliveryOriginLabels[origin]).join("、")}
      </p>
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((visible) => !visible)}
      >
        {expanded ? "收起构建" : "展开构建"}
      </button>
      {expanded && (
        <DeliveryRuns {...scope} ticket={item.id} title={item.title} />
      )}
    </>
  );
}

function DeliveryRuns({
  workspace,
  thread,
  ticket,
  title,
  onSessionExpired,
  onUnavailable,
}: TicketScope & { title: string }) {
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      try {
        return await threadDeliveryClient.runs(
          workspace,
          thread,
          ticket,
          after,
          signal,
        );
      } catch (error) {
        if (pathUnavailable(error, signal)) onUnavailable();
        throw error;
      }
    },
    [workspace, thread, ticket, onUnavailable],
  );
  const runs = usePage(load, onSessionExpired, { refreshOnFocus: false });
  return (
    <section aria-label={`${title}的构建`}>
      <TraceList
        state={runs}
        empty="该工单暂无可读的构建关联。"
        row={(run) => (
          <DeliveryRunRow
            workspace={workspace}
            thread={thread}
            ticket={ticket}
            run={run}
            onSessionExpired={onSessionExpired}
            onUnavailable={runs.refresh}
          />
        )}
      />
    </section>
  );
}

function DeliveryRunRow({
  run,
  ...scope
}: TicketScope & { run: BuildCandidate }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <>
      <a href={ciRunPagePath(scope.workspace, run.id)!}>{run.id}</a>
      <p>
        {run.component.name} · {buildStatusLabels[run.status]} ·{" "}
        <time dateTime={run.completedAt}>
          {new Date(run.completedAt).toLocaleString()}
        </time>
      </p>
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((visible) => !visible)}
      >
        {expanded ? "收起部署" : "展开部署"}
      </button>
      {expanded && <DeliveryDeployments {...scope} run={run.id} />}
    </>
  );
}

function DeliveryDeployments({
  workspace,
  thread,
  ticket,
  run,
  onSessionExpired,
  onUnavailable,
}: TicketScope & { run: string }) {
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      try {
        return await threadDeliveryClient.deployments(
          workspace,
          thread,
          ticket,
          run,
          after,
          signal,
        );
      } catch (error) {
        if (pathUnavailable(error, signal)) onUnavailable();
        throw error;
      }
    },
    [workspace, thread, ticket, run, onUnavailable],
  );
  const deployments = usePage(load, onSessionExpired, {
    refreshOnFocus: false,
  });
  return (
    <section aria-label={`${run} 的部署`}>
      <TraceList
        state={deployments}
        empty="该构建暂无可读的部署记录。"
        row={(deployment) => (
          <>
            <a href={deploymentNexusViewPagePath(workspace, deployment.id)!}>
              {deployment.environment.name}
            </a>
            <p>
              {buildStatusLabels[deployment.status]} ·{" "}
              <time dateTime={deployment.completedAt}>
                {new Date(deployment.completedAt).toLocaleString()}
              </time>
            </p>
          </>
        )}
      />
    </section>
  );
}
