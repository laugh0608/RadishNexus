import { useCallback, useState } from "react";
import { ComponentTickets } from "../collaboration/TicketComponents";
import { componentPagePath } from "../collaboration/ticket-component-api";
import { ObjectDetail } from "./DeliveryConfigurationPanel";
import { deliveryConfigurationClient } from "./delivery-configuration-api";
import {
  RepositoryBrowser,
  type RepositoryContext,
} from "./RepositoryConfigurationPanel";

export function ComponentPage(
  props: RepositoryContext & { componentID: string; userID: string },
) {
  const [unavailable, setUnavailable] = useState(false),
    [repository, setRepository] = useState("");
  const revoke = useCallback(() => {
    setUnavailable(true);
    setRepository("");
  }, []);
  return (
    <main className="configuration-panel" aria-label="软件组件详情">
      <a href="/">返回工作台</a>
      <h1>软件组件</h1>
      <p>工作区：{props.workspaceName}</p>
      {unavailable ? (
        <>
          <p role="alert">当前组件不可读取，请检查工作区与访问权限。</p>
          <button type="button" onClick={() => setUnavailable(false)}>
            重新读取组件
          </button>
        </>
      ) : (
        <>
          <ObjectDetail
            {...props}
            kind="component"
            id={props.componentID}
            client={deliveryConfigurationClient}
            onUnavailable={revoke}
            onOpenRepository={setRepository}
            componentContent={(value) => (
              <ComponentTickets
                workspaceID={props.workspaceID}
                componentID={value.id}
                componentName={value.name}
                onSessionExpired={props.onSessionExpired}
                onUnavailable={revoke}
              />
            )}
          />
          {repository ? (
            <RepositoryBrowser
              {...props}
              key={repository}
              initialID={repository}
              onOpenComponent={(id) =>
                window.location.assign(componentPagePath(props.workspaceID, id))
              }
            />
          ) : null}
        </>
      )}
    </main>
  );
}
