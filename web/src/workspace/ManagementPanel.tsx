import { useCallback, useEffect, useRef, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { Field } from "./configuration-ui";
import {
  managementClient as api,
  managementOperations,
  rememberManagementOperation,
  type ManagementResponsibility,
  type ManagementAction,
  type ManagementAudit,
  type ManagementInput,
  type ManagementMember,
  type ManagementMembers,
  type ManagementPlan,
  type ManagementProposal,
  type ManagementResult,
  type ManagementScope,
} from "./management-api";
import type { ConfigurationPage } from "./configuration-api";

const labels: Record<ManagementAction, string> = {
  add: "添加成员",
  role: "变更角色",
  remove: "正式移除",
  handoff: "交接管理责任",
  leave: "主动退出",
  suspend: "暂停成员",
  resume: "恢复成员",
  reinvite: "签发再次加入邀请",
};
const roleLabels: Record<string, string> = {
  owner: "工作区 owner",
  admin: "项目管理员",
  manager: "局部管理者",
  member: "成员",
  viewer: "只读",
  contributor: "协作者",
  decider: "决策者",
  active: "有效",
  suspended: "已暂停",
  removed: "已退出",
  "": "无显式角色",
};
function message(error: unknown): string {
  return error instanceof Error
    ? error.message
    : "无法完成成员操作，请重新读取。";
}
interface Props {
  scope: ManagementScope;
  userID: string;
  onSessionExpired: () => void;
  onChanged?: () => void | Promise<void>;
  onLeft?: () => void;
}

export function ManagementPanel(props: Props) {
  const [open, setOpen] = useState(false);
  return (
    <section
      className="foundation-configuration management-panel"
      aria-label="成员生命周期与管理交接"
    >
      <button
        type="button"
        className="secondary-button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        {props.scope.kind === "workspace"
          ? "工作区成员与退出"
          : "成员与管理交接"}
      </button>
      {open ? (
        <ManagementEditor
          key={`${props.userID}:${props.scope.workspace}:${props.scope.id}`}
          {...props}
        />
      ) : null}
    </section>
  );
}
function ManagementEditor({
  scope,
  userID,
  onSessionExpired,
  onChanged,
  onLeft,
}: Props) {
  const [page, setPage] = useState<ManagementMembers | null>(null),
    [candidates, setCandidates] = useState<ManagementMembers | null>(null);
  const [responsibilities, setResponsibilities] =
    useState<ConfigurationPage<ManagementResponsibility> | null>(null);
  const [audit, setAudit] = useState<ConfigurationPage<ManagementAudit> | null>(
    null,
  );
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]),
    [candidateCursors, setCandidateCursors] = useState<(string | undefined)[]>([
      undefined,
    ]);
  const [error, setError] = useState<string | null>(null),
    [busy, setBusy] = useState(false),
    [plan, setPlan] = useState<ManagementPlan | null>(null),
    [confirmed, setConfirmed] = useState(false);
  const [pending, setPending] = useState<{
      action: ManagementAction;
      input: ManagementInput;
    } | null>(null),
    [result, setResult] = useState<ManagementResult | null>(null);
  const [target, setTarget] = useState(""),
    [role, setRole] = useState("contributor"),
    [afterRole, setAfterRole] = useState(
      scope.kind === "project" ? "viewer" : "member",
    );
  const active = useRef(true),
    action = useRef(false),
    read = useRef<AbortController | null>(null),
    write = useRef<AbortController | null>(null),
    feedback = useRef<HTMLDivElement>(null);
  const callbacks = useRef({ onSessionExpired, onChanged, onLeft });
  useEffect(() => {
    callbacks.current = { onSessionExpired, onChanged, onLeft };
  }, [onSessionExpired, onChanged, onLeft]);
  const fail = useCallback((e: unknown) => {
    if (e instanceof AuthRequestError && e.status === 401) {
      callbacks.current.onSessionExpired();
      return;
    }
    setError(message(e));
    if (
      e instanceof AuthRequestError &&
      (e.status === 403 || e.status === 404)
    ) {
      setPage(null);
      setCandidates(null);
      setAudit(null);
      setResponsibilities(null);
      setPlan(null);
      setConfirmed(false);
    }
  }, []);
  const refresh = useCallback(
    async (after?: string) => {
      read.current?.abort();
      const c = new AbortController();
      read.current = c;
      setPage(null);
      setCandidates(null);
      setAudit(null);
      setResponsibilities(null);
      setPlan(null);
      setConfirmed(false);
      setTarget("");
      try {
        const p = await api.members(
          { workspace: scope.workspace, kind: scope.kind, id: scope.id },
          false,
          after,
          c.signal,
        );
        if (active.current && !c.signal.aborted) setPage(p);
      } catch (e) {
        if (active.current && !c.signal.aborted) fail(e);
      }
    },
    [scope.workspace, scope.kind, scope.id, fail],
  );
  useEffect(() => {
    active.current = true;
    const initial = new AbortController();
    queueMicrotask(() => {
      if (!initial.signal.aborted) void refresh();
    });
    const restore = () => {
      if (!action.current) {
        setCursors([undefined]);
        setCandidateCursors([undefined]);
        setResult(null);
        void refresh();
      }
    };
    const visible = () => {
      if (document.visibilityState === "visible") restore();
    };
    window.addEventListener("focus", restore);
    window.addEventListener("pageshow", restore);
    document.addEventListener("visibilitychange", visible);
    return () => {
      active.current = false;
      initial.abort();
      read.current?.abort();
      write.current?.abort();
      window.removeEventListener("focus", restore);
      window.removeEventListener("pageshow", restore);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [refresh]);
  const run = async (fn: (signal: AbortSignal) => Promise<void>) => {
    if (action.current) return;
    action.current = true;
    setBusy(true);
    setError(null);
    const c = new AbortController();
    write.current = c;
    try {
      await fn(c.signal);
    } catch (e) {
      if (active.current && !c.signal.aborted) fail(e);
    } finally {
      action.current = false;
      if (active.current && !c.signal.aborted) {
        setBusy(false);
        feedback.current?.focus();
      }
    }
  };
  const preview = (which: ManagementAction, input: ManagementProposal) =>
    void run(async (signal) => {
      setPlan(null);
      setConfirmed(false);
      setResult(null);
      const p = await api.preview(scope, which, input, userID, signal);
      if (active.current && !signal.aborted) setPlan(p);
    });
  const propose = (which: ManagementAction, id: string, nextRole = "") =>
    preview(which, {
      target_user_id: id,
      role: nextRole,
      actor_after_role: which === "handoff" ? afterRole : "",
    });
  const acceptResult = async (
    out: ManagementResult,
    signal: AbortSignal,
    departed = false,
  ) => {
    if (!active.current || signal.aborted) return;
    setResult(out);
    if (out.result === "applied") {
      setPending(null);
      setPlan(null);
      setConfirmed(false);
      await callbacks.current.onChanged?.();
      if (!active.current || signal.aborted) return;
      const left =
        out.action.endsWith(".leave") ||
        departed ||
        (out.action.endsWith(".handoff") &&
          pending?.input.actor_after_role === "");
      if (left) {
        callbacks.current.onLeft?.();
        return;
      }
      await refresh(cursors.at(-1));
    }
  };
  const execute = (p: { action: ManagementAction; input: ManagementInput }) =>
    void run(async (signal) => {
      // Save the known receipt coordinate before the request can commit.
      rememberManagementOperation(
        userID,
        scope.workspace,
        p.input.client_operation_id,
      );
      setPending(p);
      try {
        const out = await api.apply(scope, p.action, p.input, signal);
        await acceptResult(
          out,
          signal,
          p.action === "handoff" && p.input.actor_after_role === "",
        );
      } catch (e) {
        if (
          e instanceof AuthRequestError &&
          (e.status === 400 || e.status === 409)
        ) {
          setPending(null);
          setPlan(null);
          setConfirmed(false);
        }
        throw e;
      }
    });
  const loadCandidates = (after?: string) =>
    void run(async (signal) => {
      setCandidates(null);
      setTarget("");
      const p = await api.members(scope, true, after, signal);
      if (active.current && !signal.aborted) setCandidates(p);
    });
  const loadAudit = (after?: string) =>
    void run(async (signal) => {
      setAudit(null);
      const p = await api.audit(scope, after, signal);
      if (active.current && !signal.aborted) setAudit(p);
    });
  return (
    <div className="configuration-panel">
      <div ref={feedback} tabIndex={-1}>
        {error ? <p role="alert">{error}</p> : null}
        {busy ? <p role="status">正在核对成员与权限…</p> : null}
        {result ? <ManagementResultView result={result} /> : null}
      </div>
      <p>
        {scope.kind === "workspace"
          ? "暂停保留尚未撤销的协作权限，恢复会重新启用这些权限；部署权限需要另行授予。正式退出清除协作授权，历史内容保留。"
          : scope.kind === "project"
            ? "项目管理员可增补与交接。撤销项目角色会清除下属私密授权；工作区可见项目仍保留只读基线。"
            : "局部管理者只管理这里的成员，不获得项目角色或业务写权限。交接对象必须已加入，并且能读取父级。"}
      </p>
      <button
        type="button"
        disabled={busy}
        onClick={() => void refresh(cursors.at(-1))}
      >
        重新读取成员
      </button>
      {!page && !error ? <p role="status">正在读取当前权限…</p> : null}
      {page ? (
        <>
          <p>
            我的角色：{roleLabels[page.selfRole] ?? page.selfRole}
            {!page.canManage ? "；仅显示本人成员信息。" : ""}
          </p>
          <ul className="configuration-members">
            {page.items.map((m) => (
              <ManagementMemberRow
                key={`${m.id}:${m.role}:${m.status}`}
                member={m}
                scope={scope}
                self={m.id === userID}
                canManage={page.canManage}
                disabled={busy || pending !== null}
                propose={propose}
              />
            ))}
          </ul>
          {page.items.length === 0 ? <p>当前页没有成员。</p> : null}
          <div className="configuration-pagination">
            <button
              type="button"
              disabled={busy || cursors.length < 2}
              onClick={() => {
                const c = cursors.slice(0, -1);
                setCursors(c);
                void refresh(c.at(-1));
              }}
            >
              上一页成员
            </button>
            <button
              type="button"
              disabled={busy || !page.nextCursor}
              onClick={() => {
                if (page.nextCursor) {
                  setCursors([...cursors, page.nextCursor]);
                  void refresh(page.nextCursor);
                }
              }}
            >
              下一页成员
            </button>
          </div>
          {page.selfRole &&
          !(scope.kind === "workspace" && page.selfRole === "owner") ? (
            <button
              type="button"
              disabled={busy || pending !== null}
              onClick={() => propose("leave", userID)}
            >
              退出
              {scope.kind === "workspace"
                ? "工作区"
                : scope.kind === "project"
                  ? "项目角色"
                  : "当前私密协作"}
            </button>
          ) : null}
          {scope.kind === "workspace" && page.selfRole === "owner" ? (
            <p>退出前请先在“管理权交接与结果查询”中完成 owner 交接。</p>
          ) : null}
          {page.canManage && scope.kind !== "workspace" ? (
            <>
              <button
                type="button"
                disabled={busy || pending !== null}
                onClick={() => {
                  setCandidateCursors([undefined]);
                  loadCandidates();
                }}
              >
                选择新增成员
              </button>
              {candidates ? (
                <fieldset disabled={busy || pending !== null}>
                  <legend>添加成员</legend>
                  <Field label="候选成员">
                    <select
                      value={target}
                      onChange={(e) => setTarget(e.target.value)}
                    >
                      <option value="">选择已有父级权限的成员</option>
                      {candidates.items.map((m) => (
                        <option key={m.id} value={m.id}>
                          {m.displayName} · {m.id}
                        </option>
                      ))}
                    </select>
                  </Field>
                  {scope.kind === "project" ? (
                    <Field label="初始项目角色">
                      <select
                        value={role}
                        onChange={(e) => setRole(e.target.value)}
                      >
                        <option value="viewer">只读 viewer</option>
                        <option value="contributor">协作者 contributor</option>
                        <option value="decider">决策者 decider</option>
                      </select>
                    </Field>
                  ) : null}
                  <button
                    type="button"
                    disabled={!target}
                    onClick={() =>
                      propose(
                        "add",
                        target,
                        scope.kind === "project" ? role : "",
                      )
                    }
                  >
                    核对添加成员
                  </button>
                  <button
                    type="button"
                    disabled={candidateCursors.length < 2}
                    onClick={() => {
                      const c = candidateCursors.slice(0, -1);
                      setCandidateCursors(c);
                      loadCandidates(c.at(-1));
                    }}
                  >
                    上一页候选
                  </button>
                  <button
                    type="button"
                    disabled={!candidates.nextCursor}
                    onClick={() => {
                      if (candidates.nextCursor) {
                        setCandidateCursors([
                          ...candidateCursors,
                          candidates.nextCursor,
                        ]);
                        loadCandidates(candidates.nextCursor);
                      }
                    }}
                  >
                    下一页候选
                  </button>
                  {candidates.items.length === 0 ? (
                    <p>没有当前可添加的候选成员。</p>
                  ) : null}
                </fieldset>
              ) : null}
              <fieldset disabled={busy || pending !== null}>
                <legend>主动交接</legend>
                <Field label="接任成员">
                  <select
                    value={target}
                    onChange={(e) => setTarget(e.target.value)}
                  >
                    <option value="">选择当前列表中已加入的有效成员</option>
                    {page.items
                      .filter((m) => m.id !== userID && m.eligible)
                      .map((m) => (
                        <option key={m.id} value={m.id}>
                          {m.displayName} · {m.id}
                        </option>
                      ))}
                  </select>
                </Field>
                <Field label="交接后我的身份">
                  <select
                    value={afterRole}
                    onChange={(e) => setAfterRole(e.target.value)}
                  >
                    {scope.kind === "project" ? (
                      <>
                        <option value="viewer">保留只读角色</option>
                        <option value="contributor">保留协作者角色</option>
                        <option value="decider">保留决策者角色</option>
                      </>
                    ) : (
                      <option value="member">保留普通成员身份</option>
                    )}
                    <option value="">退出此作用域</option>
                  </select>
                </Field>
                <button
                  type="button"
                  disabled={
                    !page.items.some(
                      (m) => m.id === target && m.eligible && m.id !== userID,
                    )
                  }
                  onClick={() => propose("handoff", target)}
                >
                  核对交接
                </button>
              </fieldset>
            </>
          ) : null}
          {page.canManage ? (
            <button type="button" disabled={busy} onClick={() => loadAudit()}>
              查看本作用域管理审计
            </button>
          ) : null}
        </>
      ) : null}
      {scope.kind === "workspace" ? (
        <section>
          <button
            type="button"
            disabled={busy}
            onClick={() =>
              void run(async (signal) => {
                const p = await api.responsibilities(
                  scope.workspace,
                  undefined,
                  signal,
                );
                if (active.current && !signal.aborted) setResponsibilities(p);
              })
            }
          >
            我的待交接职责
          </button>
          {responsibilities ? (
            <>
              <p>仅显示本人当前有权读取和管理的对象，包括归档对象。</p>
              <ul>
                {responsibilities.items.map((item) => (
                  <li key={item.id}>
                    <strong>{item.name}</strong> · {item.kind} ·{" "}
                    {roleLabels[item.role]}
                    <ManagementPanel
                      scope={{
                        workspace: scope.workspace,
                        kind: item.kind,
                        id: item.id,
                      }}
                      userID={userID}
                      onSessionExpired={onSessionExpired}
                      onChanged={() => refresh()}
                    />
                  </li>
                ))}
              </ul>
              {responsibilities.items.length === 0 ? (
                <p>没有当前可见的管理责任。</p>
              ) : null}
              <button
                type="button"
                disabled={busy || !responsibilities.nextCursor}
                onClick={() =>
                  void run(async (signal) => {
                    const p = await api.responsibilities(
                      scope.workspace,
                      responsibilities.nextCursor ?? undefined,
                      signal,
                    );
                    if (active.current && !signal.aborted)
                      setResponsibilities(p);
                  })
                }
              >
                下一页职责
              </button>
            </>
          ) : null}
        </section>
      ) : null}
      {plan && !pending ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (confirmed)
              execute({
                action: plan.action,
                input: {
                  target_user_id: plan.target_user_id,
                  role: plan.role,
                  actor_after_role: plan.actor_after_role,
                  client_operation_id: crypto.randomUUID(),
                  plan_digest: plan.digest,
                  confirmed: true,
                },
              });
          }}
        >
          <fieldset disabled={busy}>
            <legend>确认{labels[plan.action]}</legend>
            <p>
              操作者：{userID}；目标：{plan.target_name} · {plan.target_user_id}
            </p>
            <p>
              {roleLabels[plan.before] ?? plan.before} →{" "}
              {roleLabels[plan.after] ?? plan.after}
            </p>
            {plan.action === "handoff" ? (
              <p>
                我的新身份：
                {plan.actor_after_role
                  ? roleLabels[plan.actor_after_role]
                  : "退出"}
                。保留的私密成员身份和项目管理员派生能力仍按当前权限生效。
              </p>
            ) : null}
            <p>
              {plan.action === "resume"
                ? "恢复尚未撤销的协作权限，包括仍保留的 owner、项目管理员和局部管理身份；部署权限需另行授予。目标须重新登录。"
                : plan.action === "suspend"
                  ? "本工作区立即停止新访问，撤销部署授权。可能暂时无人管理部分协作对象；不会自动转交私密内容。"
                  : plan.action === "reinvite"
                    ? "仅此既有账户可以兑换；重新加入从普通成员和空显式授权开始。邀请令牌仅显示一次，请由你自行传递。"
                    : plan.action === "remove" || plan.action === "leave"
                      ? "清除本作用域及下属窄授权；最后管理责任尚未交接时，整单拒绝。保留历史作者与内容。"
                      : "变更仅作用于所选角色和明确交接；不会赋予未加入的私密内容权限。"}
            </p>
            <p>系统在提交时核对全部交接条件；不可读作用域不显示明细或数量。</p>
            <label className="configuration-checkbox">
              <input
                type="checkbox"
                checked={confirmed}
                onChange={(e) => setConfirmed(e.target.checked)}
              />
              我已核对目标、权限变化和保留的能力
            </label>
            <button type="submit" disabled={!confirmed}>
              确认{labels[plan.action]}
            </button>
            <button
              type="button"
              onClick={() => {
                setPlan(null);
                setConfirmed(false);
              }}
            >
              取消
            </button>
          </fieldset>
        </form>
      ) : null}
      {pending ? (
        <div className="management-pending">
          <p>
            待确认操作：<code>{pending.input.client_operation_id}</code>
            。请先查询结果，再决定是否重试。
          </p>
          <button
            type="button"
            disabled={busy}
            onClick={() =>
              void run(async (signal) => {
                await acceptResult(
                  await api.result(
                    scope.workspace,
                    pending.input.client_operation_id,
                    signal,
                  ),
                  signal,
                );
              })
            }
          >
            查询原操作结果
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => execute(pending)}
          >
            以原内容重试
          </button>
          <p>刷新或离开页面后，可在“账户与邀请”查询已保存的操作编号。</p>
        </div>
      ) : null}
      {audit ? (
        <section aria-label="本作用域管理审计">
          <h4>管理审计</h4>
          <ul>
            {audit.items.map((item) => (
              <li key={item.id}>
                {item.action} · {roleLabels[item.before] ?? item.before} →{" "}
                {roleLabels[item.after] ?? item.after}
                <br />
                操作者 {item.actorID}；目标 {item.targetID}；
                {new Date(item.occurredAt).toLocaleString()}
              </li>
            ))}
          </ul>
          {audit.items.length === 0 ? <p>暂无管理记录。</p> : null}
          <button
            type="button"
            disabled={busy || !audit.nextCursor}
            onClick={() => loadAudit(audit.nextCursor ?? undefined)}
          >
            下一页审计
          </button>
        </section>
      ) : null}
    </div>
  );
}

function ManagementMemberRow({
  member: m,
  scope,
  self,
  canManage,
  disabled,
  propose,
}: {
  member: ManagementMember;
  scope: ManagementScope;
  self: boolean;
  canManage: boolean;
  disabled: boolean;
  propose: (a: ManagementAction, id: string, role?: string) => void;
}) {
  const [role, setRole] = useState(m.role);
  return (
    <li>
      <p>
        <strong>{m.displayName}</strong> · {m.id} · {roleLabels[m.status]} ·{" "}
        {roleLabels[m.role]}
      </p>
      {!m.eligible ? <p>当前不具备新增权限条件。</p> : null}
      {canManage && !self ? (
        <fieldset disabled={disabled}>
          {scope.kind === "workspace" ? (
            <>
              {m.status === "active" ? (
                <button type="button" onClick={() => propose("suspend", m.id)}>
                  暂停成员
                </button>
              ) : null}
              {m.status === "suspended" ? (
                <button type="button" onClick={() => propose("resume", m.id)}>
                  恢复成员
                </button>
              ) : null}
              {m.status !== "removed" && m.role !== "owner" ? (
                <button type="button" onClick={() => propose("remove", m.id)}>
                  正式移除
                </button>
              ) : null}
              {m.status === "removed" ? (
                <button type="button" onClick={() => propose("reinvite", m.id)}>
                  再次邀请
                </button>
              ) : null}
              {m.role === "owner" ? (
                <p>owner 正式退出前须先主动交接。</p>
              ) : null}
            </>
          ) : (
            <>
              <Field label={`调整 ${m.displayName}（${m.id}）的角色`}>
                <select value={role} onChange={(e) => setRole(e.target.value)}>
                  {(scope.kind === "project"
                    ? ["viewer", "contributor", "decider", "admin"]
                    : ["member", "manager"]
                  ).map((r) => (
                    <option
                      key={r}
                      value={r}
                      disabled={
                        !m.eligible && (r === "admin" || r === "manager")
                      }
                    >
                      {roleLabels[r]} · {r}
                    </option>
                  ))}
                </select>
              </Field>
              <button
                type="button"
                disabled={role === m.role}
                onClick={() => propose("role", m.id, role)}
              >
                核对角色变更
              </button>
              <button type="button" onClick={() => propose("remove", m.id)}>
                移除成员及下属授权
              </button>
            </>
          )}
        </fieldset>
      ) : null}
    </li>
  );
}
export function ManagementResultView({ result }: { result: ManagementResult }) {
  return result.result === "not_observed" ? (
    <p role="status">
      尚未观察到已提交结果；这不证明正在执行的请求已回滚。保留原操作，稍后再查。
    </p>
  ) : (
    <div role="status">
      <p>
        操作已完成 · {result.action} ·{" "}
        {new Date(result.occurredAt).toLocaleString()}
      </p>
      <p>
        操作编号：<code>{result.operationID}</code>
      </p>
      {result.invitation ? (
        <>
          <p>
            再次加入邀请仅显示一次，有效至{" "}
            {new Date(result.invitation.expiresAt).toLocaleString()}
            。仅目标账户登录后可兑换。
          </p>
          <label>
            再次加入邀请令牌
            <input
              readOnly
              value={result.invitation.token}
              onFocus={(e) => e.target.select()}
            />
          </label>
        </>
      ) : result.action.endsWith(".reinvite") ? (
        <p>
          邀请已签发。历史结果不返回令牌；若未收到原响应，可明确签发一个新邀请。
        </p>
      ) : null}
    </div>
  );
}
export function ManagementReceipts({
  userID,
  onSessionExpired,
}: {
  userID: string;
  onSessionExpired: () => void;
}) {
  const [open, setOpen] = useState(false),
    [result, setResult] = useState<ManagementResult | null>(null),
    [error, setError] = useState<string | null>(null),
    [busy, setBusy] = useState(false);
  const [items, setItems] = useState(() => managementOperations(userID));
  const read = useRef<AbortController | null>(null);
  useEffect(
    () => () => {
      read.current?.abort();
    },
    [userID],
  );
  return (
    <section className="identity-section">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => {
          read.current?.abort();
          setBusy(false);
          setResult(null);
          setError(null);
          setItems(managementOperations(userID));
          setOpen(!open);
        }}
      >
        成员操作结果查询
      </button>
      {open ? (
        <>
          <p>
            仅查询本浏览器已保存的本人操作编号；退出工作区后仍可查看固定结果，不恢复访问权限。
          </p>
          {items.length === 0 ? (
            <p>没有已保存的操作。</p>
          ) : (
            <ul>
              {items.map((item) => (
                <li key={item.operation}>
                  <code>{item.operation}</code>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      read.current?.abort();
                      const controller = new AbortController();
                      read.current = controller;
                      setResult(null);
                      setBusy(true);
                      setError(null);
                      void api
                        .result(
                          item.workspace,
                          item.operation,
                          controller.signal,
                        )
                        .then(
                          (value) => {
                            if (!controller.signal.aborted) setResult(value);
                          },
                          (e) => {
                            if (controller.signal.aborted) return;
                            if (
                              e instanceof AuthRequestError &&
                              e.status === 401
                            )
                              onSessionExpired();
                            else setError(message(e));
                          },
                        )
                        .finally(() => {
                          if (!controller.signal.aborted) setBusy(false);
                        });
                    }}
                  >
                    查询结果
                  </button>
                </li>
              ))}
            </ul>
          )}
          {error ? <p role="alert">{error}</p> : null}
          {result ? <ManagementResultView result={result} /> : null}
        </>
      ) : null}
    </section>
  );
}
