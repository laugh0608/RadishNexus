import { useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import type {
  AuthorizationMember,
  DeliveryConfigurationClient,
  DeliveryObject,
} from "./delivery-configuration-api";

export function EnvironmentAuthorizationEditor({
  workspaceID,
  workspaceName,
  environment,
  userID,
  currentUserID,
  client,
  onChanged,
  onSessionExpired,
  onUnavailable,
}: {
  workspaceID: string;
  workspaceName: string;
  environment: DeliveryObject;
  userID: string;
  currentUserID: string;
  client: DeliveryConfigurationClient;
  onChanged: () => void;
  onSessionExpired: () => void;
  onUnavailable: () => void;
}) {
  const [member, setMember] = useState<AuthorizationMember | null>(null);
  const [revision, setRevision] = useState(0);
  const [confirmed, setConfirmed] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<{
    method: "PUT" | "DELETE";
    body: Record<string, unknown>;
  } | null>(null);
  const pendingRef = useRef(pending);
  const busyRef = useRef(false);
  const alive = useRef(true);
  const reader = useRef<AbortController | null>(null);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      reader.current?.abort();
    };
  }, []);
  useEffect(() => {
    const load = () => {
      if (pendingRef.current) return;
      reader.current?.abort();
      const controller = new AbortController();
      reader.current = controller;
      setMember(null);
      setConfirmed(false);
      void client
        .authorization(workspaceID, environment.id, userID, controller.signal)
        .then(
          (value) => {
            if (!controller.signal.aborted) setMember(value);
          },
          (error) => {
            if (controller.signal.aborted) return;
            if (error instanceof AuthRequestError && error.status === 401)
              onSessionExpired();
            else if (
              error instanceof AuthRequestError &&
              (error.status === 403 || error.status === 404)
            )
              onUnavailable();
            else
              setMessage(
                error instanceof Error
                  ? error.message
                  : "无法读取当前授权，请刷新。",
              );
          },
        );
    };
    load();
    window.addEventListener("focus", load);
    return () => {
      reader.current?.abort();
      window.removeEventListener("focus", load);
    };
  }, [
    client,
    workspaceID,
    environment.id,
    userID,
    revision,
    onSessionExpired,
    onUnavailable,
  ]);
  const send = async (operation: NonNullable<typeof pending>) => {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setMessage(null);
    reader.current?.abort();
    pendingRef.current = operation;
    setPending(operation);
    try {
      await client.changeAuthorization(
        workspaceID,
        environment.id,
        userID,
        operation.method,
        operation.body,
      );
      if (!alive.current) return;
      pendingRef.current = null;
      setPending(null);
      setConfirmed(false);
      setMessage("请求已处理，正在重新读取当前授权。");
      setRevision((v) => v + 1);
      onChanged();
    } catch (error) {
      if (!alive.current) return;
      if (error instanceof AuthRequestError && error.status === 401) {
        onSessionExpired();
        return;
      }
      if (
        error instanceof AuthRequestError &&
        (error.status === 403 || error.status === 404)
      ) {
        onUnavailable();
        return;
      }
      if (
        error instanceof AuthRequestError &&
        error.status !== undefined &&
        error.status < 500
      ) {
        pendingRef.current = null;
        setPending(null);
        setConfirmed(false);
        setRevision((v) => v + 1);
      }
      setMessage(
        error instanceof Error
          ? error.message
          : "结果尚未确认，请按原请求重试。",
      );
    } finally {
      busyRef.current = false;
      if (alive.current) setBusy(false);
    }
  };
  const begin = (method: "PUT" | "DELETE") => {
    if (!confirmed || !member || pendingRef.current) return;
    void send({
      method,
      body: {
        client_operation_id: crypto.randomUUID(),
        expected_authorization: member.authorization,
        confirmed: true,
      },
    });
  };
  return (
    <section className="configuration-panel" aria-label="单成员环境授权">
      <h4>确认环境记录权</h4>
      <p>
        {workspaceName} · {environment.name}（{environment.key} / staging）
      </p>
      {member ? (
        <>
          <p>
            {member.name} · {member.id}
            {member.id === currentUserID ? " · 你本人" : ""}
          </p>
          <p>
            当前授权：
            {member.authorization?.status === "active"
              ? "已授予"
              : member.authorization?.status === "revoked"
                ? "已撤销"
                : "尚未授予"}
            。{!member.eligible ? "该成员当前不可被授予权限。" : ""}
          </p>
          <p>
            授予允许此成员记录外部已完成的 staging
            部署结果。撤销将阻止后续记录，保留已有部署历史的读取。
          </p>
          <fieldset disabled={busy || pending !== null}>
            <label className="configuration-checkbox">
              <input
                type="checkbox"
                checked={confirmed}
                onChange={(e) => setConfirmed(e.target.checked)}
              />
              我已确认目标环境、成员与授权影响
            </label>
            <div className="configuration-actions">
              <button
                type="button"
                disabled={
                  !confirmed ||
                  !environment.canGrant ||
                  !member.eligible ||
                  member.authorization?.status === "active"
                }
                onClick={() => begin("PUT")}
              >
                授予记录权
              </button>
              <button
                type="button"
                disabled={
                  !confirmed ||
                  !environment.canRevoke ||
                  member.authorization?.status !== "active"
                }
                onClick={() => begin("DELETE")}
              >
                撤销记录权
              </button>
            </div>
          </fieldset>
        </>
      ) : (
        <p role="status">正在读取当前授权…</p>
      )}
      {pending && !busy ? (
        <div>
          <p role="alert">
            结果尚未确认，原请求可能已经写入。重试会使用同一请求；放弃仅清除本地待确认请求。
          </p>
          <div className="configuration-actions">
            <button type="button" onClick={() => void send(pending)}>
              按原请求重试
            </button>
            <button
              type="button"
              onClick={() => {
                pendingRef.current = null;
                setPending(null);
                setConfirmed(false);
                setRevision((v) => v + 1);
                onChanged();
              }}
            >
              放弃本次待确认请求并刷新
            </button>
          </div>
        </div>
      ) : null}
      <p role="status">{busy ? "正在处理授权…" : message}</p>
      {!pending && !busy ? (
        <button type="button" onClick={() => setRevision((v) => v + 1)}>
          重新读取授权
        </button>
      ) : null}
    </section>
  );
}
