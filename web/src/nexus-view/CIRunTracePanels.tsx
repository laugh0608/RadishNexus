import { useCallback } from "react";
import { TraceList } from "../collaboration/TicketCIRuns";
import { collaborationPagePath } from "../collaboration/api";
import { ticketStatusLabels } from "../collaboration/ticket-status";
import { usePage } from "../workspace/configuration-hooks";
import { deploymentNexusViewPagePath } from "./api";
import { buildStatusLabels, ciRunTraceClient } from "./ci-run-trace-api";

export function CIRunTracePanels({
  workspace,
  run,
  onSessionExpired,
}: {
  workspace: string;
  run: string;
  onSessionExpired: () => void;
}) {
  const loadTickets = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      ciRunTraceClient.runTickets(workspace, run, after, signal),
    [workspace, run],
  );
  const loadDeployments = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      ciRunTraceClient.deployments(workspace, run, after, signal),
    [workspace, run],
  );
  const tickets = usePage(loadTickets, onSessionExpired);
  const deployments = usePage(loadDeployments, onSessionExpired);
  return (
    <>
      <section className="collaboration-panel" aria-label="关联工单">
        <h2>关联工单</h2>
        <TraceList
          state={tickets}
          empty="该构建暂无可读的关联工单。"
          row={(ticket) => (
            <>
              <a href={collaborationPagePath(workspace, "ticket", ticket.id)!}>
                {ticket.title}
              </a>{" "}
              · {ticketStatusLabels[ticket.status]}
            </>
          )}
        />
      </section>
      <section
        className="collaboration-panel"
        aria-label="已记录的 staging 部署"
      >
        <h2>已记录的 staging 部署</h2>
        <TraceList
          state={deployments}
          empty="该构建暂无可读的 staging 部署记录。"
          row={(deployment) => (
            <>
              <a href={deploymentNexusViewPagePath(workspace, deployment.id)!}>
                {deployment.environment.name}
              </a>{" "}
              · {buildStatusLabels[deployment.status]} ·{" "}
              <time dateTime={deployment.completedAt}>
                {new Date(deployment.completedAt).toLocaleString()}
              </time>
            </>
          )}
        />
      </section>
    </>
  );
}
