import { useCallback, useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import type { ConfigurationPage } from "./configuration-api";

export function useAction(
  onSessionExpired: () => void,
  onUnavailable?: () => void,
) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [uncertain, setUncertain] = useState(false);
  const pending = useRef(false);
  const alive = useRef(true);
  const retry = useRef<{ signature: string; id: string } | null>(null);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const run = async <T>(
    kind: string,
    payload: Record<string, unknown>,
    call: (body: Record<string, unknown>) => Promise<T>,
    done: (result: T) => void,
  ) => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setMessage(null);
    const signature = JSON.stringify({ kind, payload });
    if (retry.current?.signature !== signature)
      retry.current = { signature, id: crypto.randomUUID() };
    try {
      const result = await call({
        ...payload,
        client_operation_id: retry.current.id,
      });
      retry.current = null;
      if (alive.current) {
        setUncertain(false);
        setMessage("已保存。");
        done(result);
      }
    } catch (error) {
      if (!alive.current) return;
      setUncertain(
        !(
          error instanceof AuthRequestError &&
          error.status !== undefined &&
          error.status < 500
        ),
      );
      if (error instanceof AuthRequestError && error.status === 401) {
        onSessionExpired();
        return;
      }
      if (
        error instanceof AuthRequestError &&
        error.status !== undefined &&
        error.status < 500
      )
        retry.current = null;
      if (
        error instanceof AuthRequestError &&
        (error.status === 403 || error.status === 404)
      )
        onUnavailable?.();
      setMessage(
        error instanceof Error ? error.message : "操作未完成，请重试。",
      );
    } finally {
      pending.current = false;
      if (alive.current) setBusy(false);
    }
  };
  return { busy, message, uncertain, run };
}
export function usePage<T>(
  load: (
    after: string | undefined,
    signal: AbortSignal,
  ) => Promise<ConfigurationPage<T>>,
  onSessionExpired: () => void,
) {
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{
    page?: ConfigurationPage<T>;
    error?: string;
    loader?: typeof load;
    cursor?: string;
    revision?: number;
  }>({});
  const cursor = cursors[cursors.length - 1];
  useEffect(() => {
    const controller = new AbortController();
    void load(cursor, controller.signal).then(
      (page) => {
        if (!controller.signal.aborted)
          setState({ page, loader: load, cursor, revision });
      },
      (error) => {
        if (controller.signal.aborted) return;
        if (error instanceof AuthRequestError && error.status === 401) {
          onSessionExpired();
          return;
        }
        setState({
          error: error instanceof Error ? error.message : "无法加载配置。",
          loader: load,
          cursor,
          revision,
        });
      },
    );
    return () => controller.abort();
  }, [load, cursor, revision, onSessionExpired]);
  const refresh = useCallback(() => {
    setState({});
    setCursors([undefined]);
    setRevision((v) => v + 1);
  }, []);
  useEffect(() => {
    const focus = () => refresh();
    window.addEventListener("focus", focus);
    return () => window.removeEventListener("focus", focus);
  }, [refresh]);
  const current =
    state.loader === load &&
    state.cursor === cursor &&
    state.revision === revision
      ? state
      : {};
  return {
    ...current,
    refresh,
    next: () => {
      if (current.page?.nextCursor) {
        setState({});
        setCursors((v) => [...v, current.page!.nextCursor!]);
      }
    },
    previous: () => {
      setState({});
      setCursors((v) => v.slice(0, -1));
    },
    hasPrevious: cursors.length > 1,
  };
}
