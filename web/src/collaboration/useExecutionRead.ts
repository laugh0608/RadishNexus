import { useEffect, useEffectEvent, useState } from "react";
import { requestStatus } from "./execution-write";

export function useExecutionRead<T>(
  load: (signal: AbortSignal) => Promise<T>,
  onSessionExpired: () => void,
  enabled = true,
  onDenied?: () => void,
) {
  const [state, setState] = useState<{
    load: typeof load;
    data?: T;
    error?: string;
  }>({ load });
  const handleDenied = useEffectEvent(() => onDenied?.());
  useEffect(() => {
    if (!enabled) return;
    let controller: AbortController | undefined;
    const forget = () => {
      controller?.abort();
      setState({ load });
    };
    const read = () => {
      controller?.abort();
      const current = new AbortController();
      controller = current;
      void load(current.signal).then(
        (data) => {
          if (!current.signal.aborted) setState({ load, data });
        },
        (error) => {
          if (current.signal.aborted) return;
          const status = requestStatus(error);
          if (status === 401 || status === 403 || status === 404)
            handleDenied();
          if (status === 401) onSessionExpired();
          setState({ load, error: "当前内容无法读取，请刷新后重试。" });
        },
      );
    };
    const refresh = () => {
      forget();
      if (document.visibilityState === "visible") read();
    };
    const restore = (event: PageTransitionEvent) => {
      if (event.persisted) refresh();
    };
    read();
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    window.addEventListener("pagehide", forget);
    window.addEventListener("pageshow", restore);
    return () => {
      controller?.abort();
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
      window.removeEventListener("pagehide", forget);
      window.removeEventListener("pageshow", restore);
    };
  }, [enabled, load, onSessionExpired]);
  return enabled && state.load === load ? state : { load };
}
