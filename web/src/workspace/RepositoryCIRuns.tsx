import { useCallback } from "react";
import { ciRunPagePath } from "../nexus-view/ci-run-api";
import { formatTimestamp } from "../nexus-view/api";
import { loadRepositoryCIRuns } from "./repository-ci-runs-api";
import { usePage } from "./configuration-hooks";
import { PageControls } from "./configuration-ui";

export function RepositoryCIRuns({
  workspaceID,
  repositoryID,
  onSessionExpired,
  load = loadRepositoryCIRuns,
}: {
  workspaceID: string;
  repositoryID: string;
  onSessionExpired: () => void;
  load?: typeof loadRepositoryCIRuns;
}) {
  const read = useCallback(
    (after?: string, signal?: AbortSignal) =>
      load(workspaceID, repositoryID, after, signal),
    [load, workspaceID, repositoryID],
  );
  const page = usePage(read, onSessionExpired);
  return (
    <section className="configuration-panel" aria-label="来源构建">
      <h3>来源构建</h3>
      <p>列出已记录此仓库 checkout revision 的构建；不代表已经部署。</p>
      <PageControls page={page} />
      {page.error ? (
        <p role="alert">{page.error}</p>
      ) : !page.page ? (
        <p role="status">正在读取来源构建…</p>
      ) : page.page.items.length === 0 ? (
        <p>暂无可读取的来源构建。</p>
      ) : (
        <ul className="configuration-list">
          {page.page.items.map((run) => (
            <li key={run.id}>
              <a href={ciRunPagePath(workspaceID, run.id)!}>
                查看构建 {run.id}
              </a>
              <p>
                {run.status === "succeeded"
                  ? "构建成功"
                  : run.status === "failed"
                    ? "构建失败"
                    : "构建已取消"}{" "}
                · {formatTimestamp(run.completedAt)}
              </p>
              <code className="checkout-oid">{run.revision.oid}</code>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
