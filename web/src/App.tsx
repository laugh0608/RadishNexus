import {
  useCallback,
  useEffect,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { NexusView } from "./nexus-view/NexusView";
import {
  AuthRequestError,
  browserAuthClient,
  type AuthClient,
  type LoginCredentials,
  type SessionContext,
} from "./auth/api";
import { ChannelPage } from "./channel/ChannelPage";
import {
  browserChannelMessageClient,
  channelLocation,
  channelPagePath,
  type ChannelMessageClient,
} from "./channel/api";
import { CollaborationPage } from "./collaboration/CollaborationPage";
import {
  browserCollaborationClient,
  collaborationLocation,
  collaborationPagePath,
  type CollaborationClient,
  type CollaborationEntityType,
} from "./collaboration/api";
import {
  DeploymentNexusViewLoadError,
  deploymentNexusViewLocation,
  deploymentNexusViewPagePath,
  loadDeploymentNexusViewData,
  type DeploymentNexusViewLoader,
} from "./nexus-view/api";
import {
  failedDeploymentNexusViewFixture,
  succeededDeploymentNexusViewFixture,
} from "./nexus-view/fixture";
import type { NexusViewState } from "./nexus-view/model";

type PrototypeMode = "succeeded" | "failed" | "loading" | "error";

type AppRoute =
  | { kind: "home" }
  | { kind: "prototype" }
  | { kind: "deployment"; workspaceID: string; deploymentID: string }
  | { kind: "channel"; workspaceID: string; channelID: string }
  | {
      kind: "collaboration";
      workspaceID: string;
      entityType: CollaborationEntityType;
      entityID: string;
    }
  | { kind: "not-found" };

type AuthenticationState =
  | { status: "loading" }
  | { status: "failed"; message: string }
  | { status: "signed-out" }
  | { status: "signed-in"; session: SessionContext };

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
  authClient?: AuthClient;
  channelClient?: ChannelMessageClient;
  collaborationClient?: CollaborationClient;
  loadDeployment?: DeploymentNexusViewLoader;
  navigate?: (path: string) => void;
}

export function App({
  pathname = window.location.pathname,
  authClient = browserAuthClient,
  channelClient = browserChannelMessageClient,
  collaborationClient = browserCollaborationClient,
  loadDeployment = loadDeploymentNexusViewData,
  navigate = (path) => window.location.assign(path),
}: AppProps) {
  const route = appRoute(pathname);
  if (route.kind === "prototype") {
    return <PrototypeApp />;
  }
  return (
    <AuthenticatedApp
      route={route}
      authClient={authClient}
      channelClient={channelClient}
      collaborationClient={collaborationClient}
      loadDeployment={loadDeployment}
      navigate={navigate}
    />
  );
}

function appRoute(pathname: string): AppRoute {
  if (pathname === "/" || pathname === "") {
    return { kind: "home" };
  }
  if (pathname === "/prototype/nexus-view") {
    return { kind: "prototype" };
  }
  const deployment = deploymentNexusViewLocation(pathname);
  if (deployment !== null) {
    return { kind: "deployment", ...deployment };
  }
  const channel = channelLocation(pathname);
  if (channel !== null) {
    return { kind: "channel", ...channel };
  }
  const collaboration = collaborationLocation(pathname);
  return collaboration === null
    ? { kind: "not-found" }
    : { kind: "collaboration", ...collaboration };
}

function AuthenticatedApp({
  route,
  authClient,
  channelClient,
  collaborationClient,
  loadDeployment,
  navigate,
}: {
  route: Exclude<AppRoute, { kind: "prototype" }>;
  authClient: AuthClient;
  channelClient: ChannelMessageClient;
  collaborationClient: CollaborationClient;
  loadDeployment: DeploymentNexusViewLoader;
  navigate: (path: string) => void;
}) {
  const [bootstrapKey, setBootstrapKey] = useState(0);
  const [authentication, setAuthentication] = useState<AuthenticationState>({
    status: "loading",
  });

  useEffect(() => {
    const controller = new AbortController();
    void authClient.resolveSession(controller.signal).then(
      (session) => {
        if (!controller.signal.aborted) {
          setAuthentication({ status: "signed-in", session });
        }
      },
      (error: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        if (error instanceof AuthRequestError && error.status === 401) {
          setAuthentication({ status: "signed-out" });
          return;
        }
        setAuthentication({
          status: "failed",
          message: authErrorMessage(error),
        });
      },
    );
    return () => controller.abort();
  }, [authClient, bootstrapKey]);

  const requireLogin = useCallback(() => {
    setAuthentication({ status: "signed-out" });
  }, []);

  if (authentication.status === "loading") {
    return (
      <ShellState
        title="正在确认登录状态"
        message="正在安全读取当前 Session。"
      />
    );
  }
  if (authentication.status === "failed") {
    return (
      <ShellState
        title="无法确认登录状态"
        message={authentication.message}
        actionLabel="重新检查"
        onAction={() => {
          setAuthentication({ status: "loading" });
          setBootstrapKey((key) => key + 1);
        }}
      />
    );
  }
  if (authentication.status === "signed-out") {
    return (
      <LoginView
        onLogin={async (credentials, signal) => {
          const session = await authClient.login(credentials, signal);
          setAuthentication({ status: "signed-in", session });
        }}
      />
    );
  }
  return (
    <SignedInShell
      route={route}
      session={authentication.session}
      authClient={authClient}
      channelClient={channelClient}
      collaborationClient={collaborationClient}
      loadDeployment={loadDeployment}
      navigate={navigate}
      onSignedOut={requireLogin}
    />
  );
}

function LoginView({
  onLogin,
}: {
  onLogin: (
    credentials: LoginCredentials,
    signal?: AbortSignal,
  ) => Promise<void>;
}) {
  const [loginName, setLoginName] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      await onLogin({ loginName, password });
      setPassword("");
    } catch (submitError) {
      setError(authErrorMessage(submitError));
      setSubmitting(false);
    }
  };

  return (
    <div className="app-shell">
      <AppHeader note="安全登录 · 服务端 Session" brandHref="/" />
      <main className="auth-layout">
        <section className="auth-card" aria-labelledby="login-title">
          <p className="section-kicker">Authenticated Web Shell</p>
          <h1 id="login-title">登录 RadishNexus</h1>
          <p className="auth-card__intro">
            使用实例本地账号进入。密码只发送到当前 HTTPS
            origin，不会写入浏览器存储。
          </p>
          <form className="auth-form" onSubmit={(event) => void submit(event)}>
            <label>
              <span>登录名</span>
              <input
                autoComplete="username"
                autoFocus
                disabled={submitting}
                onChange={(event) => setLoginName(event.target.value)}
                required
                value={loginName}
              />
            </label>
            <label>
              <span>密码</span>
              <input
                autoComplete="current-password"
                disabled={submitting}
                onChange={(event) => setPassword(event.target.value)}
                required
                type="password"
                value={password}
              />
            </label>
            {error === null ? null : (
              <p className="form-error" role="alert">
                {error}
              </p>
            )}
            <button
              className="primary-button auth-submit"
              disabled={submitting}
              type="submit"
            >
              {submitting ? "正在登录…" : "登录"}
            </button>
          </form>
        </section>
      </main>
      <AppFooter label="Authenticated Web Shell / M1" />
    </div>
  );
}

function SignedInShell({
  route,
  session,
  authClient,
  channelClient,
  collaborationClient,
  loadDeployment,
  navigate,
  onSignedOut,
}: {
  route: Exclude<AppRoute, { kind: "prototype" }>;
  session: SessionContext;
  authClient: AuthClient;
  channelClient: ChannelMessageClient;
  collaborationClient: CollaborationClient;
  loadDeployment: DeploymentNexusViewLoader;
  navigate: (path: string) => void;
  onSignedOut: () => void;
}) {
  const [loggingOut, setLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState<string | null>(null);
  const currentWorkspace =
    route.kind === "deployment" ||
    route.kind === "channel" ||
    route.kind === "collaboration"
      ? session.workspaces.find(
          (workspace) => workspace.id === route.workspaceID,
        )
      : undefined;

  const logout = async () => {
    setLoggingOut(true);
    setLogoutError(null);
    try {
      await authClient.logout();
      onSignedOut();
    } catch (error) {
      if (error instanceof AuthRequestError && error.status === 401) {
        onSignedOut();
        return;
      }
      setLogoutError(authErrorMessage(error));
      setLoggingOut(false);
    }
  };

  return (
    <div className="app-shell">
      <AppHeader
        note={
          route.kind === "deployment" ||
          route.kind === "channel" ||
          route.kind === "collaboration"
            ? `真实 API · ${currentWorkspace?.name ?? "当前权限过滤"}`
            : "Authenticated Web Shell"
        }
        brandHref="/"
      >
        <div className="account-controls">
          <span>
            <small>已登录</small>
            <strong>{session.user.displayName}</strong>
          </span>
          <button
            disabled={loggingOut}
            onClick={() => void logout()}
            type="button"
          >
            {loggingOut ? "正在退出…" : "退出登录"}
          </button>
        </div>
      </AppHeader>

      {logoutError === null ? null : (
        <p className="shell-alert" role="alert">
          {logoutError}
        </p>
      )}

      {route.kind === "home" ? (
        <WorkspaceHome session={session} navigate={navigate} />
      ) : route.kind === "deployment" ? (
        <LiveDeploymentApp
          key={`${route.workspaceID}/${route.deploymentID}`}
          workspaceID={route.workspaceID}
          deploymentID={route.deploymentID}
          loadDeployment={loadDeployment}
          onSessionExpired={onSignedOut}
        />
      ) : route.kind === "channel" ? (
        <ChannelPage
          key={`${route.workspaceID}/${route.channelID}`}
          workspaceID={route.workspaceID}
          channelID={route.channelID}
          client={channelClient}
          onSessionExpired={onSignedOut}
        />
      ) : route.kind === "collaboration" ? (
        <CollaborationPage
          key={`${route.workspaceID}/${route.entityType}/${route.entityID}`}
          workspaceID={route.workspaceID}
          entityType={route.entityType}
          entityID={route.entityID}
          client={collaborationClient}
          onSessionExpired={onSignedOut}
        />
      ) : (
        <NotFoundView />
      )}

      <AppFooter label="Authenticated Web Shell / M1" />
    </div>
  );
}

function WorkspaceHome({
  session,
  navigate,
}: {
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
    <main className="shell-home">
      <section className="shell-welcome" aria-labelledby="shell-home-title">
        <p className="section-kicker">Current workspace context</p>
        <h1 id="shell-home-title">欢迎回来，{session.user.displayName}</h1>
        <p>
          Session 不固定 Workspace。每次打开业务对象时，服务端都会按当前
          membership 重新验证权限。
        </p>
      </section>
      <section
        className="workspace-launcher"
        aria-labelledby="deployment-launcher-title"
      >
        <div>
          <p className="section-kicker">Known object launchers</p>
          <h2 id="deployment-launcher-title">进入当前工作上下文</h2>
          <p>
            当前尚未开放对象列表；请使用已知稳定 ID 进入权限过滤后的正式页面。
          </p>
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
        {error === null ? null : (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
      </section>
    </main>
  );
}

function LiveDeploymentApp({
  workspaceID,
  deploymentID,
  loadDeployment,
  onSessionExpired,
}: {
  workspaceID: string;
  deploymentID: string;
  loadDeployment: DeploymentNexusViewLoader;
  onSessionExpired: () => void;
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
        if (controller.signal.aborted) {
          return;
        }
        if (
          error instanceof DeploymentNexusViewLoadError &&
          error.status === 401
        ) {
          onSessionExpired();
          return;
        }
        setState({ status: "error", message: deploymentErrorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [deploymentID, loadDeployment, onSessionExpired, requestKey, workspaceID]);

  return (
    <NexusView
      state={state}
      onRetry={() => {
        setState({ status: "loading" });
        setRequestKey((key) => key + 1);
      }}
    />
  );
}

function NotFoundView() {
  return (
    <main className="nexus-layout">
      <section className="state-panel">
        <span className="state-panel__mark" aria-hidden="true">
          ?
        </span>
        <p className="section-kicker">Page not found</p>
        <h1>这个页面不存在</h1>
        <p>请返回 Web Shell，并通过稳定对象路径继续。</p>
        <a className="primary-button button-link" href="/">
          返回 Web Shell
        </a>
      </section>
    </main>
  );
}

function ShellState({
  title,
  message,
  actionLabel,
  onAction,
}: {
  title: string;
  message: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <div className="app-shell">
      <AppHeader note="Authenticated Web Shell" brandHref="/" />
      <main className="nexus-layout" aria-busy={onAction === undefined}>
        <section className="state-panel">
          <p className="section-kicker">Session bootstrap</p>
          <h1>{title}</h1>
          <p role={onAction === undefined ? "status" : "alert"}>{message}</p>
          {actionLabel === undefined || onAction === undefined ? null : (
            <button className="primary-button" onClick={onAction} type="button">
              {actionLabel}
            </button>
          )}
        </section>
      </main>
      <AppFooter label="Authenticated Web Shell / M1" />
    </div>
  );
}

function PrototypeApp() {
  const [mode, setMode] = useState<PrototypeMode>("succeeded");

  return (
    <div className="app-shell">
      <AppHeader
        note="静态代表原型 · 非真实工作区数据"
        brandHref="/prototype/nexus-view"
      >
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
      <AppFooter label="Representative slice / M0" />
    </div>
  );
}

function AppHeader({
  note,
  brandHref,
  children,
}: {
  note: string;
  brandHref: string;
  children?: ReactNode;
}) {
  return (
    <header className="prototype-header">
      <a className="brand-lockup" href={brandHref} aria-label="RadishNexus">
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

function AppFooter({ label }: { label: string }) {
  return (
    <footer className="prototype-footer">
      <span>{label}</span>
      <span>Channel · Message · Thread · Decision · Delivery</span>
    </footer>
  );
}

function authErrorMessage(error: unknown): string {
  return error instanceof AuthRequestError
    ? error.userMessage
    : "认证服务暂不可用，请稍后重试。";
}

function deploymentErrorMessage(error: unknown): string {
  return error instanceof DeploymentNexusViewLoadError
    ? error.userMessage
    : "服务没有返回可用的 Nexus View，请稍后重试。";
}
