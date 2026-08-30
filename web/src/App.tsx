import { useEffect, useState, type ReactNode } from "react";
import { NexusView } from "./nexus-view/NexusView";
import {
  DeploymentNexusViewLoadError,
  deploymentNexusViewLocation,
  loadDeploymentNexusViewData,
  type DeploymentNexusViewLoader,
} from "./nexus-view/api";
import {
  failedDeploymentNexusViewFixture,
  succeededDeploymentNexusViewFixture,
} from "./nexus-view/fixture";
import type { NexusViewState } from "./nexus-view/model";

type PrototypeMode = "succeeded" | "failed" | "loading" | "error";

const prototypeStates: Record<PrototypeMode, NexusViewState> = {
  succeeded: { status: "ready", data: succeededDeploymentNexusViewFixture },
  failed: { status: "ready", data: failedDeploymentNexusViewFixture },
  loading: { status: "loading" },
  error: {
    status: "error",
    message:
      "服务没有返回可用的 Nexus View。请稍后重试；已载入的页面不会被当作成功。",
  },
};

const prototypeModes: readonly { id: PrototypeMode; label: string }[] = [
  { id: "succeeded", label: "成功" },
  { id: "failed", label: "失败" },
  { id: "loading", label: "加载" },
  { id: "error", label: "错误" },
];

interface AppProps {
  pathname?: string;
  loadDeployment?: DeploymentNexusViewLoader;
}

export function App({
  pathname = window.location.pathname,
  loadDeployment = loadDeploymentNexusViewData,
}: AppProps) {
  const location = deploymentNexusViewLocation(pathname);
  if (location !== null) {
    return (
      <LiveDeploymentApp
        key={`${location.workspaceID}/${location.deploymentID}`}
        workspaceID={location.workspaceID}
        deploymentID={location.deploymentID}
        loadDeployment={loadDeployment}
      />
    );
  }

  return <PrototypeApp />;
}

function PrototypeApp() {
  const [mode, setMode] = useState<PrototypeMode>("succeeded");

  return (
    <div className="app-shell">
      <AppHeader note="静态代表原型 · 非真实工作区数据">
        <div className="state-switcher" aria-label="原型状态检视">
          <span>状态检视</span>
          <div>
            {prototypeModes.map((item) => (
              <button
                aria-pressed={mode === item.id}
                key={item.id}
                onClick={() => setMode(item.id)}
                type="button"
              >
                {item.label}
              </button>
            ))}
          </div>
        </div>
      </AppHeader>

      <NexusView
        state={prototypeStates[mode]}
        onRetry={() => setMode("succeeded")}
      />

      <AppFooter />
    </div>
  );
}

function LiveDeploymentApp({
  workspaceID,
  deploymentID,
  loadDeployment,
}: {
  workspaceID: string;
  deploymentID: string;
  loadDeployment: DeploymentNexusViewLoader;
}) {
  const [requestKey, setRequestKey] = useState(0);
  const [state, setState] = useState<NexusViewState>({ status: "loading" });

  useEffect(() => {
    const controller = new AbortController();
    void loadDeployment(workspaceID, deploymentID, controller.signal).then(
      (data) => {
        if (!controller.signal.aborted) {
          setState({ status: "ready", data });
        }
      },
      (error: unknown) => {
        if (!controller.signal.aborted) {
          setState({ status: "error", message: loadErrorMessage(error) });
        }
      },
    );
    return () => controller.abort();
  }, [deploymentID, loadDeployment, requestKey, workspaceID]);

  return (
    <div className="app-shell">
      <AppHeader note="真实 API · 当前权限过滤" />
      <NexusView
        state={state}
        onRetry={() => {
          setState({ status: "loading" });
          setRequestKey((key) => key + 1);
        }}
      />
      <AppFooter />
    </div>
  );
}

function AppHeader({ note, children }: { note: string; children?: ReactNode }) {
  return (
    <header className="prototype-header">
      <a
        className="brand-lockup"
        href="#nexus-current-title"
        aria-label="RadishNexus Nexus View"
      >
        <span className="brand-mark" aria-hidden="true">
          R
        </span>
        <span>
          <strong>RadishNexus</strong>
          <small>Context stays connected.</small>
        </span>
      </a>

      <div className="prototype-note">
        <span aria-hidden="true" />
        {note}
      </div>

      {children}
    </header>
  );
}

function AppFooter() {
  return (
    <footer className="prototype-footer">
      <span>Representative slice / M0</span>
      <span>Deployment · Environment · CI Run · Timeline</span>
    </footer>
  );
}

function loadErrorMessage(error: unknown): string {
  if (error instanceof DeploymentNexusViewLoadError) {
    return error.userMessage;
  }
  return "服务没有返回可用的 Nexus View，请稍后重试。";
}
