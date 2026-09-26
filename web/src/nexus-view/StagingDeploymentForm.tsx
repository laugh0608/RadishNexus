import { useEffect, useRef, useState, type FormEvent } from "react";
import { AuthRequestError } from "../auth/api";
import {
  stagingClient,
  type StagingClient,
  type StagingInput,
  type StagingTarget,
} from "./staging-api";
import "./staging.css";

export function StagingDeploymentForm({
  workspaceID,
  ciRunID,
  available,
  navigate,
  onSessionExpired,
  client = stagingClient,
}: {
  workspaceID: string;
  ciRunID: string;
  available: boolean;
  navigate: (path: string) => void;
  onSessionExpired: () => void;
  client?: StagingClient;
}) {
  const [open, setOpen] = useState(false),
    [items, setItems] = useState<StagingTarget[]>([]),
    [next, setNext] = useState<string | null>(null);
  const [after, setAfter] = useState<string | undefined>(),
    [reload, setReload] = useState(0),
    [loading, setLoading] = useState(false);
  const [environment, setEnvironment] = useState(""),
    [status, setStatus] = useState(""),
    [started, setStarted] = useState(""),
    [completed, setCompleted] = useState(""),
    [confirmed, setConfirmed] = useState(false);
  const [error, setError] = useState(""),
    [pending, setPending] = useState<StagingInput | null>(null),
    [busy, setBusy] = useState(false);
  const [targetLabel, setTargetLabel] = useState("");
  const inFlight = useRef(false),
    alive = useRef(true),
    heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    if (open) heading.current?.focus();
  }, [open]);
  useEffect(() => {
    if (!open || !available || pending) return;
    const controller = new AbortController();
    const refresh = () => {
      setItems([]);
      setNext(null);
      setEnvironment("");
      setConfirmed(false);
      setLoading(true);
      setError("");
      void client.targets(workspaceID, ciRunID, after, controller.signal).then(
        (page) => {
          if (controller.signal.aborted) return;
          if (page.next_cursor === after && after) {
            setError("环境分页没有前进，请重新读取。");
            setLoading(false);
            return;
          }
          setItems(page.items);
          setNext(page.next_cursor);
          setLoading(false);
        },
        (e: unknown) => {
          if (controller.signal.aborted) return;
          setLoading(false);
          if (e instanceof AuthRequestError && e.status === 401) {
            onSessionExpired();
            return;
          }
          setError(e instanceof Error ? e.message : "环境读取失败。");
        },
      );
    };
    refresh();
    return () => controller.abort();
  }, [
    workspaceID,
    ciRunID,
    client,
    open,
    available,
    after,
    reload,
    pending,
    onSessionExpired,
  ]);
  function change(update: () => void) {
    update();
    setConfirmed(false);
    setError("");
  }
  function toUTC(value: string): string | null {
    if (!value) return null;
    const d = new Date(value);
    return Number.isFinite(d.getTime()) ? d.toISOString() : null;
  }
  const start = toUTC(started),
    end = toUTC(completed);
  const target = items.find((i) => i.ref.id === environment);
  const valid =
    !!target &&
    (status === "succeeded" || status === "failed" || status === "canceled") &&
    !!end &&
    (!started || !!start) &&
    (!start || !end || start <= end);
  async function send(input: StagingInput) {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const result = await client.record(workspaceID, ciRunID, input);
      if (alive.current)
        navigate(
          `/workspaces/${encodeURIComponent(workspaceID)}/deployments/${encodeURIComponent(result.deployment.id)}`,
        );
    } catch (e) {
      if (!alive.current) return;
      if (e instanceof AuthRequestError && e.status === 401) {
        onSessionExpired();
        return;
      }
      setError(
        e instanceof Error ? e.message : "无法确认记录结果，请重试原请求。",
      );
    } finally {
      inFlight.current = false;
      if (alive.current) setBusy(false);
    }
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    if (
      !available ||
      pending ||
      !valid ||
      !confirmed ||
      !end ||
      inFlight.current
    )
      return;
    const input: StagingInput = {
      client_operation_id: crypto.randomUUID(),
      environment_id: environment,
      status: status as StagingInput["status"],
      started_at: start,
      completed_at: end,
      confirmed: true,
    };
    setPending(input);
    setTargetLabel(`${target!.name}（${target!.key}）`);
    void send(input);
  }
  function close() {
    if (busy) return;
    setOpen(false);
    setPending(null);
    setConfirmed(false);
    setError("");
    setAfter(undefined);
    setEnvironment("");
    setStatus("");
    setStarted("");
    setCompleted("");
  }
  if (!available) return null;
  if (!open)
    return (
      <section className="staging-entry">
        <p>外部部署已结束后，可在这里记录它的结果。</p>
        <button type="button" onClick={() => setOpen(true)}>
          记录 staging 部署结果
        </button>
      </section>
    );
  return (
    <section className="staging-record" aria-labelledby="staging-title">
      <h2 id="staging-title" tabIndex={-1} ref={heading}>
        记录 staging 部署结果
      </h2>
      <p>
        记录外部已经结束的部署；本操作不会启动部署。记录确认后不可修改，同一构建在同一环境只能记录一次。
      </p>
      <p className="staging-source">来源构建：{ciRunID}</p>
      {error && <p role="alert">{error}</p>}
      {pending ? (
        <>
          <dl>
            <dt>目标环境</dt>
            <dd>{targetLabel}</dd>
            <dt>结果</dt>
            <dd>{labels[pending.status]}</dd>
            <dt>开始时间</dt>
            <dd>{displayTime(pending.started_at)}</dd>
            <dt>完成时间</dt>
            <dd>{displayTime(pending.completed_at)}</dd>
          </dl>
          <p role="status">
            {busy
              ? "正在记录，请稍候。"
              : "本次请求已冻结，结果可能已写入。重试会使用同一请求，避免重复记录。"}
          </p>
          {!busy && (
            <button type="button" onClick={() => void send(pending)}>
              重试原请求
            </button>
          )}
          <p>关闭或刷新页面将丢失本地重试材料，不会撤销可能已写入的记录。</p>
          <button type="button" disabled={busy} onClick={close}>
            放弃本地重试并关闭
          </button>
        </>
      ) : (
        <form onSubmit={submit}>
          {loading ? (
            <p role="status">正在读取已授权环境…</p>
          ) : items.length === 0 && !error ? (
            <p>当前没有可选的已授权 staging 环境。</p>
          ) : null}
          <label>
            目标环境
            <select
              value={environment}
              disabled={loading}
              required
              onChange={(e) => change(() => setEnvironment(e.target.value))}
            >
              <option value="">请选择环境</option>
              {items.map((i) => (
                <option key={i.ref.id} value={i.ref.id}>
                  {i.name}（{i.key}）
                </option>
              ))}
            </select>
          </label>
          <div className="staging-actions">
            <button
              type="button"
              disabled={loading}
              onClick={() => {
                setAfter(undefined);
                setReload((n) => n + 1);
              }}
            >
              重新读取环境
            </button>
            {next && (
              <button
                type="button"
                disabled={loading}
                onClick={() => setAfter(next)}
              >
                下一页环境
              </button>
            )}
          </div>
          <label>
            部署结果
            <select
              value={status}
              required
              onChange={(e) => change(() => setStatus(e.target.value))}
            >
              <option value="">请选择实际结果</option>
              <option value="succeeded">成功</option>
              <option value="failed">失败</option>
              <option value="canceled">取消</option>
            </select>
          </label>
          <div className="staging-times">
            <label>
              开始时间（可选）
              <input
                type="datetime-local"
                step="0.001"
                value={started}
                onChange={(e) => change(() => setStarted(e.target.value))}
              />
            </label>
            <label>
              完成时间
              <input
                type="datetime-local"
                step="0.001"
                required
                value={completed}
                onChange={(e) => change(() => setCompleted(e.target.value))}
              />
            </label>
          </div>
          <p>
            按本地时区 {Intl.DateTimeFormat().resolvedOptions().timeZone}{" "}
            输入，完成时间不能早于开始时间。
          </p>
          {target && status && end && (
            <dl aria-label="记录确认信息">
              <dt>目标环境</dt>
              <dd>
                {target.name}（{target.key}）
              </dd>
              <dt>结果</dt>
              <dd>{labels[status]}</dd>
              <dt>开始时间</dt>
              <dd>{displayTime(start)}</dd>
              <dt>完成时间</dt>
              <dd>{displayTime(end)}</dd>
            </dl>
          )}
          <label className="staging-confirm">
            <input
              type="checkbox"
              checked={confirmed}
              disabled={!valid}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            我确认这是外部已结束的部署结果
          </label>
          <div className="staging-actions">
            <button type="submit" disabled={!valid || !confirmed || busy}>
              确认记录
            </button>
            <button type="button" onClick={close}>
              取消
            </button>
          </div>
        </form>
      )}
    </section>
  );
}
const labels: Record<string, string> = {
  succeeded: "成功",
  failed: "失败",
  canceled: "取消",
};
function displayTime(value: string | null) {
  return value
    ? new Intl.DateTimeFormat(undefined, {
        dateStyle: "medium",
        timeStyle: "long",
      }).format(new Date(value))
    : "未提供";
}
