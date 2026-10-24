import { useEffect, useRef, useState } from "react";

/** Only the authorized source projection is queried; no package identity,
 * original member directory, or historical ACL is exposed to readers. */
export function ContextSourceNotice({
  workspaceID,
  entityType,
  entityID,
  onSessionExpired,
  onUnavailable,
}: {
  workspaceID: string;
  entityType: string;
  entityID: string;
  onSessionExpired: () => void;
  onUnavailable?: () => void;
}) {
  const scope = `${workspaceID}/${entityType}/${entityID}`;
  const [state, setState] = useState<{
    scope: string;
    value: "native" | "imported" | "unknown";
  }>({ scope, value: "unknown" });
  const callbacks = useRef({ onSessionExpired, onUnavailable });
  useEffect(() => {
    callbacks.current = { onSessionExpired, onUnavailable };
  }, [onSessionExpired, onUnavailable]);
  useEffect(() => {
    const controller = new AbortController();
    async function read() {
      try {
        const response = await fetch(
          `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/context-source/${encodeURIComponent(entityType)}/${encodeURIComponent(entityID)}`,
          {
            credentials: "same-origin",
            cache: "no-store",
            signal: controller.signal,
            headers: { Accept: "application/json" },
          },
        );
        if (controller.signal.aborted) return;
        if (response.status === 401) callbacks.current.onSessionExpired();
        if (response.status === 403 || response.status === 404)
          callbacks.current.onUnavailable?.();
        if (!response.ok) throw new Error("source unavailable");
        const envelope: unknown = await response.json();
        if (
          !envelope ||
          typeof envelope !== "object" ||
          Array.isArray(envelope) ||
          Object.keys(envelope).join() !== "data"
        )
          throw new Error("invalid source");
        const data = (envelope as { data: unknown }).data;
        if (!data || typeof data !== "object" || Array.isArray(data))
          throw new Error("invalid source");
        const source = data as Record<string, unknown>;
        if (source.imported === false && Object.keys(source).length === 1) {
          if (!controller.signal.aborted) setState({ scope, value: "native" });
        } else if (
          source.imported === true &&
          Object.keys(source).length === 2 &&
          typeof source.applied_at === "string" &&
          Number.isFinite(Date.parse(source.applied_at))
        ) {
          if (!controller.signal.aborted)
            setState({ scope, value: "imported" });
        } else throw new Error("invalid source");
      } catch {
        if (!controller.signal.aborted) setState({ scope, value: "unknown" });
      }
    }
    void read();
    return () => controller.abort();
  }, [workspaceID, entityType, entityID, scope]);
  if (state.scope !== scope || state.value === "unknown")
    return <p className="context-source-notice">来源信息尚未确认。</p>;
  if (state.value === "native") return null;
  return (
    <aside className="context-source-notice" aria-label="导入来源">
      <strong>包含导入历史</strong> ·
      历史状态、作者与时间来自迁移包，不代表本实例用户刚完成确认。历史作者不可登录；文内旧链接可能仍指向原上下文。
    </aside>
  );
}
