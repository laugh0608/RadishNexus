import { useEffect, useState, type FormEvent } from "react";
import { type SessionContext } from "./api";
import {
  identityErrorMessage as message,
  type IdentityClient,
  type LoginMethods,
} from "./identity-api";

export function IdentityLogin({
  client,
  navigate,
  onSession,
}: {
  client: IdentityClient;
  navigate: (url: string) => void;
  onSession: (session: SessionContext) => void;
}) {
  const [methods, setMethods] = useState<LoginMethods | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [invited, setInvited] = useState(false);
  const [invitationToken, setInvitationToken] = useState("");
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void client.methods(controller.signal).then(
      (value) => {
        if (!controller.signal.aborted) setMethods(value);
      },
      (error) => {
        if (!controller.signal.aborted) setError(message(error));
      },
    );
    return () => controller.abort();
  }, [client]);
  const oidc = async () => {
    setBusy(true);
    setError(null);
    try {
      navigate(
        await client.start({
          mode: "login",
          ...(invited ? { invitationToken, displayName } : {}),
        }),
      );
    } catch (error) {
      setError(message(error));
      setBusy(false);
    }
  };
  const accept = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const session = await client.acceptInvitation(
        { invitationToken, email, password, displayName },
        false,
      );
      setPassword("");
      setInvitationToken("");
      onSession(session);
    } catch (error) {
      setError(message(error));
      setBusy(false);
    }
  };
  return (
    <section className="identity-section" aria-label="其他登录方式与邀请">
      {methods?.radish && !invited ? (
        <button type="button" disabled={busy} onClick={() => void oidc()}>
          使用 Radish 登录
        </button>
      ) : null}
      <p>首次加入团队需要管理员邀请。已有账户可登录后接受邀请。</p>
      <button
        type="button"
        disabled={busy}
        aria-expanded={invited}
        onClick={() => {
          setInvited(!invited);
          setPassword("");
          setInvitationToken("");
          setError(null);
        }}
      >
        {invited ? "收起邀请" : "我有邀请码"}
      </button>
      {invited ? (
        <form className="auth-form" onSubmit={(event) => void accept(event)}>
          <label>
            邀请码
            <input
              type="password"
              autoComplete="off"
              value={invitationToken}
              disabled={busy}
              onChange={(event) => setInvitationToken(event.target.value)}
              required
            />
          </label>
          <label>
            展示名
            <input
              autoComplete="nickname"
              maxLength={100}
              value={displayName}
              disabled={busy}
              onChange={(event) => setDisplayName(event.target.value)}
              required
            />
          </label>
          <p>展示名会显示给与你协作的成员，邮箱只用于本地登录。</p>
          <label>
            注册邮箱
            <input
              type="email"
              autoComplete="username"
              value={email}
              disabled={busy}
              onChange={(event) => setEmail(event.target.value)}
              required
            />
          </label>
          <label>
            设置密码
            <input
              type="password"
              autoComplete="new-password"
              minLength={15}
              maxLength={128}
              value={password}
              disabled={busy}
              onChange={(event) => setPassword(event.target.value)}
              required
            />
          </label>
          <p>密码需为 15–128 个字符。</p>
          <button type="submit" disabled={busy}>
            {busy ? "正在处理…" : "创建账户并加入"}
          </button>
          {methods?.radish ? (
            <button
              type="button"
              disabled={busy || !invitationToken || !displayName.trim()}
              onClick={() => void oidc()}
            >
              使用 Radish 接受邀请
            </button>
          ) : null}
        </form>
      ) : null}
      {error ? <p role="alert">{error}</p> : null}
    </section>
  );
}
