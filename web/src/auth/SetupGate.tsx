import { AppHeader } from "../AppHeader";
import {
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import {
  type SetupClient,
  type SetupInput,
  type SetupStatus,
} from "./setup-api";
import { AuthRequestError } from "./api";

// Mounted only after the Session endpoint confirms unauthenticated. A failed
// setup read cannot silently expose a login or open registration fallback.
export function SetupGate({
  client,
  children,
}: {
  client: SetupClient;
  children: ReactNode;
}) {
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{
    revision: number;
    client: SetupClient;
    status?: SetupStatus;
    error?: string;
  }>();
  const [finished, setFinished] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    client.status(controller.signal).then(
      (status) => {
        if (!controller.signal.aborted) setState({ revision, client, status });
      },
      () => {
        if (!controller.signal.aborted)
          setState({
            revision,
            client,
            error: "无法确认实例初始化状态，请稍后重试。",
          });
      },
    );
    return () => controller.abort();
  }, [client, revision]);
  const current =
    state?.revision === revision && state.client === client ? state : undefined;
  if (current?.status === "complete")
    return (
      <>
        {finished ? (
          <p className="setup-success" role="status">
            管理员已创建，请使用刚设置的邮箱和密码登录。
          </p>
        ) : null}
        {children}
      </>
    );
  return (
    <div className="app-shell">
      <AppHeader note="首次初始化" brandHref="/" />
      <main className="auth-layout">
        <section className="auth-card" aria-labelledby="setup-title">
          <p className="section-kicker">首次使用</p>
          <h1 id="setup-title">初始化 RadishNexus</h1>
          {!current ? (
            <p role="status">正在确认初始化状态…</p>
          ) : current.error ? (
            <p role="alert">{current.error}</p>
          ) : current.status === "unavailable" ? (
            <p>
              实例尚未初始化。请部署者配置一次性初始化码，或使用运维初始化命令。
            </p>
          ) : null}
          {current?.status === "required" ? (
            <SetupForm
              key={revision}
              client={client}
              onComplete={() => {
                setFinished(true);
                setRevision((v) => v + 1);
              }}
              onRecheck={() => setRevision((v) => v + 1)}
            />
          ) : (
            <button
              className="secondary-button setup-recheck"
              type="button"
              onClick={() => setRevision((v) => v + 1)}
            >
              重新检查状态
            </button>
          )}
        </section>
      </main>
    </div>
  );
}
const empty: SetupInput = {
  setup_code: "",
  email: "",
  display_name: "",
  password: "",
  workspace_name: "",
};
function SetupForm({
  client,
  onComplete,
  onRecheck,
}: {
  client: SetupClient;
  onComplete: () => void;
  onRecheck: () => void;
}) {
  const [input, setInput] = useState<SetupInput>(empty);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [uncertain, setUncertain] = useState(false);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (request.current || uncertain) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setError(undefined);
    try {
      await client.complete(input, controller.signal);
      if (!controller.signal.aborted) {
        setInput(empty);
        onComplete();
      }
    } catch (e) {
      if (!controller.signal.aborted) {
        setInput((v) => ({ ...v, password: "", setup_code: "" }));
        setError(
          e instanceof AuthRequestError
            ? e.userMessage
            : "无法确认初始化结果，请重新检查状态。",
        );
        setUncertain(
          !(e instanceof AuthRequestError) ||
            e.status === undefined ||
            e.status >= 500 ||
            e.status === 409,
        );
      }
    } finally {
      if (!controller.signal.aborted) {
        request.current = null;
        setBusy(false);
      }
    }
  };
  return (
    <>
      <p className="auth-card__intro">
        创建第一个工作区及其管理员。初始化码由部署者提供；完成后此入口将关闭。
      </p>
      <form className="auth-form" onSubmit={(event) => void submit(event)}>
        {(
          [
            {
              key: "setup_code",
              label: "初始化码",
              type: "password",
              autoComplete: "off",
              max: 43,
            },
            {
              key: "email",
              label: "管理员邮箱",
              type: "email",
              autoComplete: "username",
              max: 254,
            },
            {
              key: "display_name",
              label: "管理员称呼",
              type: "text",
              autoComplete: "nickname",
              max: 100,
            },
            {
              key: "password",
              label: "管理员密码",
              type: "password",
              autoComplete: "new-password",
              max: 128,
            },
            {
              key: "workspace_name",
              label: "工作区名称",
              type: "text",
              autoComplete: "organization",
              max: 100,
            },
          ] as const
        ).map((field) => (
          <label key={field.key}>
            <span>{field.label}</span>
            <input
              type={field.type}
              autoComplete={field.autoComplete}
              required
              maxLength={field.max}
              minLength={
                field.key === "password"
                  ? 15
                  : field.key === "setup_code"
                    ? 43
                    : 1
              }
              disabled={busy || uncertain}
              value={input[field.key]}
              onChange={(e) =>
                setInput((v) => ({ ...v, [field.key]: e.target.value }))
              }
            />
          </label>
        ))}
        <p className="auth-card__intro">
          密码需 15–128
          字。首位管理员负责工作区配置，私密项目和频道仍需明确授权。
        </p>
        {error ? (
          <p role="alert" className="form-error">
            {error}
          </p>
        ) : null}
        <button
          className="primary-button auth-submit"
          type="submit"
          disabled={busy || uncertain}
        >
          {busy ? "正在创建…" : "创建管理员与工作区"}
        </button>
      </form>
      <button
        className="secondary-button setup-recheck"
        type="button"
        disabled={busy}
        onClick={onRecheck}
      >
        重新检查状态
      </button>
    </>
  );
}
