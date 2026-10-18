import { useCallback, useState } from "react";
import {
  RepositoryDetail,
  type RepositoryContext,
} from "./RepositoryConfigurationPanel";
import { repositoryConfigurationClient } from "./repository-configuration-api";
import { componentPagePath } from "../collaboration/ticket-component-api";

export function RepositoryPage(
  props: RepositoryContext & { repositoryID: string },
) {
  const [unavailable, setUnavailable] = useState(false);
  const revoke = useCallback(() => setUnavailable(true), []);
  return (
    <main className="configuration-panel" aria-label="代码库页面">
      <a href="/">返回工作台</a>
      <h1>代码库</h1>
      <p>工作区：{props.workspaceName}</p>
      {unavailable ? (
        <>
          <p role="alert">当前代码库不可读取，请检查工作区与访问权限。</p>
          <button type="button" onClick={() => setUnavailable(false)}>
            重新读取代码库
          </button>
        </>
      ) : (
        <RepositoryDetail
          {...props}
          id={props.repositoryID}
          client={props.repositoryClient ?? repositoryConfigurationClient}
          onUnavailable={revoke}
          onOpenComponent={(id) =>
            window.location.assign(componentPagePath(props.workspaceID, id))
          }
        />
      )}
    </main>
  );
}
