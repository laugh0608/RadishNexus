import { WorkspaceHome } from "./workspace/WorkspaceHome";
import { browserDiscoveryClient, type DiscoveryClient } from "./workspace/api";
import {
  useCallback,
  useEffect,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { IdentityLogin } from "./auth/IdentityLogin";
import { IdentityPanel } from "./auth/IdentityPanel";
import {
  browserIdentityClient,
  type IdentityClient,
} from "./auth/identity-api";
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
  type ChannelMessageClient,
} from "./channel/api";
import {
  browserChannelRealtimeClient,
  type ChannelRealtimeClient,
} from "./channel/realtime";
import { CollaborationPage } from "./collaboration/CollaborationPage";
import {
  browserCollaborationClient,
  collaborationLocation,
  type CollaborationClient,
  type CollaborationEntityType,
} from "./collaboration/api";
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

type AppRoute =
  | { kind: "home" }
  | { kind: "account" }
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
  identityClient?: IdentityClient;
  discoveryClient?: DiscoveryClient;
  channelClient?: ChannelMessageClient;
  channelRealtimeClient?: ChannelRealtimeClient;
  collaborationClient?: CollaborationClient;
  loadDeployment?: DeploymentNexusViewLoader;
  navigate?: (path: string) => void;
}

export function App({
  pathname = window.location.pathname,
  authClient = browserAuthClient,
  identityClient = browserIdentityClient,
  discoveryClient = browserDiscoveryClient,
  channelClient = browserChannelMessageClient,
  channelRealtimeClient = browserChannelRealtimeClient,
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
      discoveryClient={discoveryClient}
      identityClient={identityClient}
      channelClient={channelClient}
      channelRealtimeClient={channelRealtimeClient}
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
  if (pathname === "/account") return { kind: "account" };
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
  identityClient,
  discoveryClient,
  channelClient,
  channelRealtimeClient,
  collaborationClient,
  loadDeployment,
  navigate,
}: {
  route: Exclude<AppRoute, { kind: "prototype" }>;
  authClient: AuthClient;
  identityClient: IdentityClient;
  discoveryClient: DiscoveryClient;
  channelClient: ChannelMessageClient;
  channelRealtimeClient: ChannelRealtimeClient;
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
        identityClient={identityClient}
        navigate={navigate}
        onSession={(session) =>
          setAuthentication({ status: "signed-in", session })
        }
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
      discoveryClient={discoveryClient}
      identityClient={identityClient}
      channelClient={channelClient}
      channelRealtimeClient={channelRealtimeClient}
      collaborationClient={collaborationClient}
      loadDeployment={loadDeployment}
      navigate={navigate}
      onSignedOut={requireLogin}
      onSession={(session) =>
        setAuthentication({ status: "signed-in", session })
      }
    />
  );
}

function LoginView({
  onLogin,
  identityClient,
  navigate,
  onSession,
}: {
  identityClient: IdentityClient;
  navigate: (url: string) => void;
  onSession: (session: SessionContext) => void;
  onLogin: (
    credentials: LoginCredentials,
    signal?: AbortSignal,
  ) => Promise<void>;
}) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      await onLogin({ email, password });
      setPassword("");
    } catch (submitError) {
      setError(authErrorMessage(submitError));
      setSubmitting(false);
    }
  };

  return (
    <div className="app-shell">
      <AppHeader note="团队协作与研发上下文" brandHref="/" />
      <main className="auth-layout">
        <section className="auth-card" aria-labelledby="login-title">
          <p className="section-kicker">欢迎回来</p>
          <h1 id="login-title">登录 RadishNexus</h1>
          <p className="auth-card__intro">
            使用邮箱登录，继续团队中的讨论与工作。
          </p>
          <form className="auth-form" onSubmit={(event) => void submit(event)}>
            <label>
              <span>邮箱</span>
              <input
                type="email"
                autoComplete="username"
                autoFocus
                disabled={submitting}
                onChange={(event) => setEmail(event.target.value)}
                required
                value={email}
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
          <IdentityLogin
            client={identityClient}
            navigate={navigate}
            onSession={onSession}
          />
        </section>
      </main>
      <AppFooter label="RadishNexus" />
    </div>
  );
}

function SignedInShell({
  route,
  session,
  authClient,
  identityClient,
  discoveryClient,
  channelClient,
  channelRealtimeClient,
  collaborationClient,
  loadDeployment,
  navigate,
  onSignedOut,
  onSession,
}: {
  route: Exclude<AppRoute, { kind: "prototype" }>;
  session: SessionContext;
  authClient: AuthClient;
  identityClient: IdentityClient;
  discoveryClient: DiscoveryClient;
  channelClient: ChannelMessageClient;
  channelRealtimeClient: ChannelRealtimeClient;
  collaborationClient: CollaborationClient;
  loadDeployment: DeploymentNexusViewLoader;
  navigate: (path: string) => void;
  onSignedOut: () => void;
  onSession: (session: SessionContext) => void;
}) {
  const [loggingOut, setLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState<string | null>(null);
  const probeSession = useCallback(
    async (signal?: AbortSignal) => {
      await authClient.resolveSession(signal);
    },
    [authClient],
  );
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
            : "欢迎回来"
        }
        brandHref="/"
      >
        <div className="account-controls">
          <a href="/account">账户与邀请</a>
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

      {route.kind === "account" ? (
        <IdentityPanel
          client={identityClient}
          session={session}
          navigate={navigate}
          onSignedOut={onSignedOut}
          onSession={onSession}
        />
      ) : route.kind === "home" ? (
        <WorkspaceHome
          session={session}
          navigate={navigate}
          client={discoveryClient}
          onSessionExpired={onSignedOut}
        />
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
          probeSession={probeSession}
          realtimeClient={channelRealtimeClient}
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

      <AppFooter label="欢迎回来 / M1" />
    </div>
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
      <AppHeader note="欢迎回来" brandHref="/" />
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
      <AppFooter label="欢迎回来 / M1" />
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
