import { useEffect, useState, type FormEvent } from "react";
import { AuthRequestError, type SessionContext } from "./api";
import { identityErrorMessage as message } from "./identity-api";
import type {
  AccountDetails,
  IdentityClient,
  Invitation,
  LoginMethods,
} from "./identity-api";

export function IdentityPanel({
  client,
  session,
  navigate,
  onSignedOut,
  onSession,
}: {
  client: IdentityClient;
  session: SessionContext;
  navigate: (url: string) => void;
  onSignedOut: () => void;
  onSession: (session: SessionContext) => void;
}) {
  const [account, setAccount] = useState<AccountDetails | null>(null);
  const [methods, setMethods] = useState<LoginMethods | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmUnlink, setConfirmUnlink] = useState(false);
  const [invitation, setInvitation] = useState<Invitation | null>(null);
  const [invitationToken, setInvitationToken] = useState("");
  const owners = session.workspaces.filter(
    (workspace) => workspace.role === "owner",
  );
  const [workspaceID, setWorkspaceID] = useState(owners[0]?.id ?? "");
  useEffect(() => {
    const controller = new AbortController();
    void Promise.all([
      client.account(controller.signal),
      client.methods(controller.signal),
    ]).then(
      ([account, methods]) => {
        if (!controller.signal.aborted) {
          setAccount(account);
          setMethods(methods);
        }
      },
      (error) => {
        if (!controller.signal.aborted) {
          if (error instanceof AuthRequestError && error.status === 401)
            onSignedOut();
          else setError(message(error));
        }
      },
    );
    return () => controller.abort();
  }, [client, onSignedOut]);
  const perform = async (action: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await action();
    } catch (error) {
      if (error instanceof AuthRequestError && error.status === 401)
        onSignedOut();
      else setError(message(error));
    } finally {
      setBusy(false);
    }
  };
  const accept = (event: FormEvent) => {
    event.preventDefault();
    void perform(async () => {
      const next = await client.acceptInvitation({ invitationToken }, true);
      setInvitationToken("");
      onSession(next);
    });
  };
  return (
    <main className="workspace-home">
      <section className="identity-section">
        <h1>账户与成员邀请</h1>
        {account ? (
          <>
            <h2>{account.user.displayName}</h2>
            <p>
              本地密码：{account.local ? "已设置" : "未设置"} · Radish：
              {account.radishLinked ? "已绑定" : "未绑定"}
            </p>
            {!account.recentAuthentication ? (
              <p>修改登录方式前，请退出并重新登录。</p>
            ) : null}
            {methods?.radish && !account.radishLinked ? (
              <button
                type="button"
                disabled={busy || !account.recentAuthentication}
                onClick={() =>
                  void perform(async () =>
                    navigate(await client.start({ mode: "link" })),
                  )
                }
              >
                绑定 Radish
              </button>
            ) : null}
            {account.radishLinked ? (
              <>
                <button
                  type="button"
                  disabled={
                    busy || !account.local || !account.recentAuthentication
                  }
                  onClick={() => setConfirmUnlink(!confirmUnlink)}
                >
                  解绑 Radish
                </button>
                {!account.local ? (
                  <p>Radish 是唯一登录方式，当前不能解绑。</p>
                ) : null}
                {confirmUnlink ? (
                  <div>
                    <p>
                      解绑后，此账户的所有 Nexus
                      会话将退出。你仍可使用本地邮箱与密码登录。
                    </p>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() =>
                        void perform(async () => {
                          await client.unlink();
                          onSignedOut();
                        })
                      }
                    >
                      确认解绑并退出所有会话
                    </button>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => setConfirmUnlink(false)}
                    >
                      取消
                    </button>
                  </div>
                ) : null}
              </>
            ) : null}
          </>
        ) : (
          <p>正在读取账户…</p>
        )}
        {owners.length ? (
          <section aria-label="邀请成员">
            <h2>邀请成员</h2>
            <p>
              邀请码有效期为 24 小时，仅可使用一次，接受后获得普通成员身份。
            </p>
            <label>
              邀请到 Workspace
              <select
                value={workspaceID}
                disabled={busy}
                onChange={(event) => {
                  setWorkspaceID(event.target.value);
                  setInvitation(null);
                }}
              >
                {owners.map((workspace) => (
                  <option key={workspace.id} value={workspace.id}>
                    {workspace.name}
                  </option>
                ))}
              </select>
            </label>
            <button
              type="button"
              disabled={busy || !workspaceID}
              onClick={() =>
                void perform(async () => {
                  setInvitation(null);
                  setInvitation(await client.createInvitation(workspaceID));
                })
              }
            >
              生成邀请码
            </button>
            {invitation ? (
              <div>
                <label>
                  新邀请码
                  <input
                    readOnly
                    type="password"
                    value={invitation.token}
                    autoComplete="off"
                  />
                </label>
                <p>仅在本次页面显示，请私下交给受邀成员。</p>
                <button
                  type="button"
                  onClick={() =>
                    void perform(async () => {
                      await navigator.clipboard.writeText(invitation.token);
                    })
                  }
                >
                  复制邀请码
                </button>
                <button type="button" onClick={() => setInvitation(null)}>
                  隐藏邀请码
                </button>
              </div>
            ) : null}
          </section>
        ) : null}
        <section>
          <h2>加入其他 Workspace</h2>
          <p>使用当前账户接受邀请，不会创建第二个账户。</p>
          <form className="auth-form" onSubmit={accept}>
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
            <button type="submit" disabled={busy}>
              接受邀请
            </button>
          </form>
        </section>
        {error ? <p role="alert">{error}</p> : null}
        <a href="/">返回工作区</a>
      </section>
    </main>
  );
}
