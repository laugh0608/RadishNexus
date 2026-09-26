import { useEffect, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { loadCIRun, type CIRunLoader } from "./ci-run-api";
import { NexusView } from "./NexusView";
import type { NexusViewState } from "./model";

export function CIRunPage({
  workspaceID,
  ciRunID,
  onSessionExpired,
  load = loadCIRun,
}: {
  workspaceID: string;
  ciRunID: string;
  onSessionExpired: () => void;
  load?: CIRunLoader;
}) {
  const [state, setState] = useState<NexusViewState>({ status: "loading" });
  const [requestKey, setRequestKey] = useState(0);
  useEffect(() => {
    let controller: AbortController;
    const refresh = () => {
      controller?.abort();
      const current = new AbortController();
      controller = current;
      setState({ status: "loading" });
      void load(workspaceID, ciRunID, current.signal).then(
        (data) => {
          if (!current.signal.aborted) setState({ status: "ready", data });
        },
        (error: unknown) => {
          if (current.signal.aborted) return;
          if (error instanceof AuthRequestError && error.status === 401) {
            onSessionExpired();
            return;
          }
          setState({
            status: "error",
            message:
              error instanceof AuthRequestError
                ? error.userMessage
                : "CI Run 读取失败，请重试。",
          });
        },
      );
    };
    refresh();
    window.addEventListener("focus", refresh);
    return () => {
      controller.abort();
      window.removeEventListener("focus", refresh);
    };
  }, [workspaceID, ciRunID, load, onSessionExpired, requestKey]);
  return (
    <div className="ci-run-page">
      <header className="document-toolbar">
        <nav aria-label="构建位置">
          <a href="/">工作区</a>
          <span aria-hidden="true">/</span>
          <span>CI Run</span>
        </nav>
        <button type="button" onClick={() => setRequestKey((key) => key + 1)}>
          读取最新构建
        </button>
      </header>
      <NexusView
        state={state}
        onRetry={() => setRequestKey((key) => key + 1)}
      />
    </div>
  );
}
