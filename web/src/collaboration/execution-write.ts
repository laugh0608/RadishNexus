import { useCallback, useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { ChannelRequestError, newClientOperationID } from "../channel/api";

type Boundary = {
  onSessionExpired: () => void;
  onUnavailable?: () => void;
};

export function requestStatus(error: unknown) {
  return error instanceof AuthRequestError ||
    error instanceof ChannelRequestError
    ? error.status
    : undefined;
}

type Operation = {
  label: string;
  target?: string;
  run: (signal: AbortSignal) => Promise<void>;
};

export function useExecutionWrite({
  onSessionExpired,
  onUnavailable,
}: Boundary) {
  const [pending, setPending] = useState<Operation | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  const active = useRef<AbortController | null>(null);
  useEffect(() => () => active.current?.abort(), []);
  const clear = useCallback(() => {
    active.current?.abort();
    active.current = null;
    setBusy(false);
    setPending(null);
    setNotice(null);
  }, []);
  async function apply(op: Operation) {
    if (active.current) return;
    const controller = new AbortController();
    active.current = controller;
    setPending(op);
    setBusy(true);
    setNotice(null);
    try {
      await op.run(controller.signal);
      if (controller.signal.aborted) return;
      setPending(null);
      setNotice("操作已确认。");
      setRevision((value) => value + 1);
    } catch (error) {
      if (controller.signal.aborted) return;
      const status = requestStatus(error);
      if (status === 401 || status === 404) {
        setPending(null);
        onUnavailable?.();
        if (status === 401) onSessionExpired();
        setNotice("当前对象或会话已不可访问。");
        setRevision((value) => value + 1);
      } else if (status !== undefined && status >= 400 && status < 500) {
        setPending(null);
        setNotice("操作未应用，请刷新并核对当前权限或关系。");
        setRevision((value) => value + 1);
      } else {
        setNotice("结果尚未确认，可重试原操作。");
      }
    } finally {
      if (active.current === controller) {
        active.current = null;
        setBusy(false);
      }
    }
  }
  function start(
    label: string,
    run: (operationID: string, signal: AbortSignal) => Promise<void>,
    target?: string,
  ) {
    if (pending || active.current) return;
    try {
      const operationID = newClientOperationID();
      void apply({ label, target, run: (signal) => run(operationID, signal) });
    } catch {
      setNotice("无法生成操作标识，请重试。");
    }
  }
  return {
    revision,
    busy,
    pending,
    notice,
    start,
    clear,
    retry: () => {
      if (pending) void apply(pending);
    },
  };
}

export type ExecutionWrite = ReturnType<typeof useExecutionWrite>;
