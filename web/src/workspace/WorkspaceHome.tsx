import { useState, type FormEvent } from "react";
import type { SessionContext } from "../auth/api";
import { channelPagePath } from "../channel/api";
import {
  collaborationPagePath,
  type CollaborationEntityType,
} from "../collaboration/api";
import { deploymentNexusViewPagePath } from "../nexus-view/api";
import type { DiscoveryClient } from "./api";
import { ProjectBrowser } from "./ProjectBrowser";

export function WorkspaceHome({
  session,
  navigate,
  client,
  onSessionExpired,
}: {
  client: DiscoveryClient;
  onSessionExpired: () => void;
  session: SessionContext;
  navigate: (path: string) => void;
}) {
  const [workspaceID, setWorkspaceID] = useState(
    session.workspaces[0]?.id ?? "",
  );
  const [deploymentID, setDeploymentID] = useState("");
  const [channelID, setChannelID] = useState("");
  const [collaborationType, setCollaborationType] =
    useState<CollaborationEntityType>("thread");
  const [collaborationID, setCollaborationID] = useState("");
  const [error, setError] = useState<string | null>(null);

  const openDeployment = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const path = deploymentNexusViewPagePath(workspaceID, deploymentID.trim());
    if (path === null) {
      setError("请选择 Workspace，并输入以 dpl_ 开头的有效 Deployment ID。");
      return;
    }
    setError(null);
    navigate(path);
  };

  const openChannel = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const path = channelPagePath(workspaceID, channelID.trim());
    if (path === null) {
      setError("请选择 Workspace，并输入以 chn_ 开头的有效 Channel ID。");
      return;
    }
    setError(null);
    navigate(path);
  };

  const openCollaboration = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const path = collaborationPagePath(
      workspaceID,
      collaborationType,
      collaborationID.trim(),
    );
    if (path === null) {
      setError(
        "请选择 Workspace、协作对象类型，并输入匹配 thr_ / dec_ / tkt_ 的稳定 ID。",
      );
      return;
    }
    setError(null);
    navigate(path);
  };

  return (
    <main className="shell-home workspace-home">
      <section className="shell-welcome" aria-labelledby="shell-home-title">
        <p className="section-kicker">Current workspace context</p>
        <h1 id="shell-home-title">欢迎回来，{session.user.displayName}</h1>
        <p>从工作区中的项目开始，查看频道并继续协作。</p>
      </section>
      <section
        className="workspace-launcher"
        aria-labelledby="deployment-launcher-title"
      >
        <div>
          <p className="section-kicker">Workspace</p>
          <h2 id="deployment-launcher-title">进入当前工作上下文</h2>
          <p>这里只展示你当前可访问的项目与频道。</p>
        </div>
        <label className="workspace-selector">
          <span>Workspace</span>
          <select
            disabled={session.workspaces.length === 0}
            onChange={(event) => setWorkspaceID(event.target.value)}
            required
            value={workspaceID}
          >
            {session.workspaces.map((workspace) => (
              <option key={workspace.id} value={workspace.id}>
                {workspace.name} · {workspace.role}
              </option>
            ))}
          </select>
        </label>
        {workspaceID === "" ? (
          <p>你还没有加入工作区。请在“账户与邀请”中接受邀请。</p>
        ) : (
          <ProjectBrowser
            key={workspaceID}
            workspaceID={workspaceID}
            client={client}
            navigate={navigate}
            onSessionExpired={onSessionExpired}
          />
        )}
        <details className="known-object-launchers">
          <summary>按 ID 打开对象</summary>
          <div className="resource-launchers">
            <form onSubmit={openChannel}>
              <p>Channel</p>
              <label>
                <span>Channel ID</span>
                <input
                  autoComplete="off"
                  disabled={session.workspaces.length === 0}
                  onChange={(event) => setChannelID(event.target.value)}
                  placeholder="chn_…"
                  required
                  value={channelID}
                />
              </label>
              <button
                className="primary-button"
                disabled={session.workspaces.length === 0}
                type="submit"
              >
                打开 Channel
              </button>
            </form>
            <form onSubmit={openDeployment}>
              <p>Deployment</p>
              <label>
                <span>Deployment ID</span>
                <input
                  autoComplete="off"
                  disabled={session.workspaces.length === 0}
                  onChange={(event) => setDeploymentID(event.target.value)}
                  placeholder="dpl_…"
                  required
                  value={deploymentID}
                />
              </label>
              <button
                className="secondary-button"
                disabled={session.workspaces.length === 0}
                type="submit"
              >
                打开 Nexus View
              </button>
            </form>
            <form onSubmit={openCollaboration}>
              <p>Collaboration</p>
              <label>
                <span>协作对象类型</span>
                <select
                  disabled={session.workspaces.length === 0}
                  onChange={(event) =>
                    setCollaborationType(
                      event.target.value as CollaborationEntityType,
                    )
                  }
                  value={collaborationType}
                >
                  <option value="thread">Thread</option>
                  <option value="decision">Decision</option>
                  <option value="ticket">Ticket</option>
                </select>
              </label>
              <label>
                <span>协作对象 ID</span>
                <input
                  autoComplete="off"
                  disabled={session.workspaces.length === 0}
                  onChange={(event) => setCollaborationID(event.target.value)}
                  placeholder="thr_… / dec_… / tkt_…"
                  required
                  value={collaborationID}
                />
              </label>
              <button
                className="secondary-button"
                disabled={session.workspaces.length === 0}
                type="submit"
              >
                打开协作对象
              </button>
            </form>
          </div>
        </details>
        {error === null ? null : (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
      </section>
    </main>
  );
}
