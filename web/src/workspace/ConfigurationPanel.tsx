import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { AuthRequestError } from "../auth/api";
import {
  configurationClient as api,
  type ConfigurationObject,
  type ConfigurationPage,
  type ConfigurationMember,
  type ProjectRole,
} from "./configuration-api";
import { channelPagePath } from "../channel/api";

function useAction(onSessionExpired: () => void, onUnavailable?: () => void) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const pending = useRef(false);
  const alive = useRef(true);
  const retry = useRef<{ signature: string; id: string } | null>(null);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const run = async <T,>(
    kind: string,
    payload: Record<string, unknown>,
    call: (body: Record<string, unknown>) => Promise<T>,
    done: (result: T) => void,
  ) => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setMessage(null);
    const signature = JSON.stringify({ kind, payload });
    if (retry.current?.signature !== signature)
      retry.current = { signature, id: crypto.randomUUID() };
    try {
      const result = await call({
        ...payload,
        client_operation_id: retry.current.id,
      });
      retry.current = null;
      if (alive.current) {
        setMessage("已保存。");
        done(result);
      }
    } catch (error) {
      if (!alive.current) return;
      if (error instanceof AuthRequestError && error.status === 401) {
        onSessionExpired();
        return;
      }
      if (
        error instanceof AuthRequestError &&
        error.status !== undefined &&
        error.status < 500
      )
        retry.current = null;
      if (
        error instanceof AuthRequestError &&
        (error.status === 403 || error.status === 404)
      )
        onUnavailable?.();
      setMessage(
        error instanceof Error ? error.message : "操作未完成，请重试。",
      );
    } finally {
      pending.current = false;
      if (alive.current) setBusy(false);
    }
  };
  return { busy, message, run };
}
function usePage<T>(
  load: (
    after: string | undefined,
    signal: AbortSignal,
  ) => Promise<ConfigurationPage<T>>,
  onSessionExpired: () => void,
) {
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{
    page?: ConfigurationPage<T>;
    error?: string;
    loader?: typeof load;
    cursor?: string;
    revision?: number;
  }>({});
  const cursor = cursors[cursors.length - 1];
  useEffect(() => {
    const controller = new AbortController();
    void load(cursor, controller.signal).then(
      (page) => {
        if (!controller.signal.aborted)
          setState({ page, loader: load, cursor, revision });
      },
      (error) => {
        if (controller.signal.aborted) return;
        if (error instanceof AuthRequestError && error.status === 401) {
          onSessionExpired();
          return;
        }
        setState({
          error: error instanceof Error ? error.message : "无法加载配置。",
          loader: load,
          cursor,
          revision,
        });
      },
    );
    return () => controller.abort();
  }, [load, cursor, revision, onSessionExpired]);
  const refresh = useCallback(() => {
    setState({});
    setCursors([undefined]);
    setRevision((v) => v + 1);
  }, []);
  useEffect(() => {
    const focus = () => refresh();
    window.addEventListener("focus", focus);
    return () => window.removeEventListener("focus", focus);
  }, [refresh]);
  const current =
    state.loader === load &&
    state.cursor === cursor &&
    state.revision === revision
      ? state
      : {};
  return {
    ...current,
    refresh,
    next: () => {
      if (current.page?.nextCursor) {
        setState({});
        setCursors((v) => [...v, current.page!.nextCursor!]);
      }
    },
    previous: () => {
      setState({});
      setCursors((v) => v.slice(0, -1));
    },
    hasPrevious: cursors.length > 1,
  };
}
function PageControls({
  page,
}: {
  page: {
    page?: { nextCursor: string | null };
    hasPrevious: boolean;
    next: () => void;
    previous: () => void;
    refresh: () => void;
  };
}) {
  return (
    <div className="configuration-actions">
      <button type="button" onClick={page.refresh}>
        刷新
      </button>
      <button
        type="button"
        disabled={!page.hasPrevious || !page.page}
        onClick={page.previous}
      >
        上一页
      </button>
      <button
        type="button"
        disabled={!page.page?.nextCursor}
        onClick={page.next}
      >
        下一页
      </button>
    </div>
  );
}
function Feedback({
  busy,
  message,
}: {
  busy: boolean;
  message: string | null;
}) {
  return <p role="status">{busy ? "正在保存…" : message}</p>;
}
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label>
      <span>{label}</span>
      {children}
    </label>
  );
}

export function FoundationCreate({
  workspaceID,
  userID,
  onCreated,
  onSessionExpired,
}: {
  workspaceID: string;
  userID: string;
  onCreated: (project: ConfigurationObject) => void;
  onSessionExpired: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <section className="foundation-configuration">
      <button
        className="secondary-button"
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        创建团队与项目
      </button>
      {open ? (
        <CreateProjectForm
          workspaceID={workspaceID}
          userID={userID}
          onCreated={onCreated}
          onSessionExpired={onSessionExpired}
        />
      ) : null}
    </section>
  );
}
function CreateProjectForm({
  workspaceID,
  userID,
  onCreated,
  onSessionExpired,
}: {
  workspaceID: string;
  userID: string;
  onCreated: (project: ConfigurationObject) => void;
  onSessionExpired: () => void;
}) {
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      api.teams(workspaceID, after, signal),
    [workspaceID],
  );
  const teams = usePage(load, onSessionExpired);
  const action = useAction(onSessionExpired);
  const [teamName, setTeamName] = useState("");
  const [teamID, setTeamID] = useState("");
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [visibility, setVisibility] = useState("restricted");
  const [confirmed, setConfirmed] = useState(false);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!confirmed) return;
    void action.run(
      "project.create",
      {
        name,
        key,
        owner_team_id: teamID,
        visibility,
        initial_admin_user_id: userID,
      },
      (body) => api.createProject(workspaceID, body),
      onCreated,
    );
  };
  return (
    <div className="configuration-panel">
      <h3>建立第一个协作项目</h3>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void action.run(
            "team.create",
            { name: teamName },
            (body) => api.createTeam(workspaceID, body),
            (team) => {
              setTeamID(team.id);
              setTeamName("");
              teams.refresh();
            },
          );
        }}
      >
        <fieldset disabled={action.busy}>
          <Field label="新责任团队名称">
            <input
              required
              maxLength={120}
              value={teamName}
              onChange={(e) => setTeamName(e.target.value)}
            />
          </Field>
          <button type="submit">创建责任团队</button>
        </fieldset>
      </form>
      {teams.error ? <p role="alert">{teams.error}</p> : null}
      <form onSubmit={submit}>
        <fieldset disabled={action.busy}>
          <Field label="责任团队">
            <select
              required
              value={teamID}
              onChange={(e) => setTeamID(e.target.value)}
            >
              <option value="">选择团队</option>
              {teamID && !teams.page?.items.some((t) => t.id === teamID) ? (
                <option value={teamID}>已选择的团队 · {teamID}</option>
              ) : null}
              {teams.page?.items.map((team) => (
                <option key={team.id} value={team.id}>
                  {team.name} · {team.id}
                </option>
              ))}
            </select>
          </Field>
          <PageControls page={teams} />
          <Field label="项目名称">
            <input
              required
              maxLength={120}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <Field label="项目短标识">
            <input
              required
              pattern={"[a-z][a-z0-9\\-]{0,31}"}
              maxLength={32}
              placeholder="例如 auth-service"
              value={key}
              onChange={(e) => setKey(e.target.value)}
            />
          </Field>
          <Field label="项目可见范围">
            <select
              value={visibility}
              onChange={(e) => setVisibility(e.target.value)}
            >
              <option value="restricted">仅明确加入的成员</option>
              <option value="workspace">工作区成员可浏览</option>
            </select>
          </Field>
          <label className="configuration-checkbox">
            <input
              type="checkbox"
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
              required
            />
            我将成为这个新项目的初始管理员，负责配置成员权限。
          </label>
          <button type="submit" disabled={!confirmed || !teamID}>
            创建项目
          </button>
        </fieldset>
      </form>
      <Feedback busy={action.busy} message={action.message} />
    </div>
  );
}

export function ConfigurationLauncher({
  workspaceID,
  userID,
  kind,
  id,
  onSessionExpired,
  onChanged,
  navigate,
}: {
  workspaceID: string;
  userID: string;
  kind: "project" | "channel";
  id: string;
  onSessionExpired: () => void;
  onChanged?: () => void;
  navigate: (path: string) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}>
        {kind === "project" ? "项目配置" : "频道成员"}
      </button>
      {open ? (
        <ConfigurationEditor
          key={id}
          workspaceID={workspaceID}
          userID={userID}
          kind={kind}
          id={id}
          onSessionExpired={onSessionExpired}
          onChanged={onChanged}
          navigate={navigate}
        />
      ) : null}
    </div>
  );
}
function ConfigurationEditor({
  workspaceID,
  userID,
  kind,
  id,
  onSessionExpired,
  onChanged,
  navigate,
}: {
  workspaceID: string;
  userID: string;
  kind: "project" | "channel";
  id: string;
  onSessionExpired: () => void;
  onChanged?: () => void;
  navigate: (path: string) => void;
}) {
  const [revision, setRevision] = useState(0);
  const requestKey = `${workspaceID}:${kind}:${id}:${revision}`;
  const [result, setResult] = useState<{
    key: string;
    object?: ConfigurationObject;
    error?: string;
  } | null>(null);
  const object = result?.key === requestKey ? result.object : null;
  const error = result?.key === requestKey ? result.error : null;
  useEffect(() => {
    const controller = new AbortController();
    void api.read(workspaceID, kind, id, controller.signal).then(
      (o) => {
        if (!controller.signal.aborted)
          setResult({ key: requestKey, object: o });
      },
      (e) => {
        if (controller.signal.aborted) return;
        if (e instanceof AuthRequestError && e.status === 401) {
          onSessionExpired();
          return;
        }
        setResult({
          key: requestKey,
          error: e instanceof Error ? e.message : "无法读取配置。",
        });
      },
    );
    return () => controller.abort();
  }, [workspaceID, kind, id, requestKey, onSessionExpired]);
  useEffect(() => {
    const refresh = () => setRevision((v) => v + 1);
    window.addEventListener("focus", refresh);
    return () => window.removeEventListener("focus", refresh);
  }, []);
  return (
    <section
      className="configuration-panel"
      aria-label={kind === "project" ? "项目配置面板" : "频道成员配置面板"}
    >
      {error ? <p role="alert">{error}</p> : null}
      <button type="button" onClick={() => setRevision((v) => v + 1)}>
        刷新配置
      </button>
      {!object && !error ? <p role="status">正在加载配置…</p> : null}
      {object ? (
        <>
          <h3>{object.name}</h3>
          {object.canManage ? (
            <>
              <MemberEditor
                key={`${id}:${revision}`}
                workspaceID={workspaceID}
                currentUserID={userID}
                object={object}
                onSessionExpired={onSessionExpired}
              />
              {kind === "project" ? (
                <CreateChannel
                  workspaceID={workspaceID}
                  projectID={id}
                  userID={userID}
                  onSessionExpired={onSessionExpired}
                  onCreated={(channel) => {
                    onChanged?.();
                    const path = channelPagePath(workspaceID, channel.id);
                    if (path) navigate(path);
                  }}
                />
              ) : null}
            </>
          ) : (
            <p>
              {object.status === "archived"
                ? "已归档，当前只可浏览。"
                : kind === "channel" && object.visibility === "project"
                  ? "此频道沿用项目权限，无独立成员列表。"
                  : "当前没有此项配置权限。"}
            </p>
          )}
        </>
      ) : null}
    </section>
  );
}
function CreateChannel({
  workspaceID,
  projectID,
  userID,
  onSessionExpired,
  onCreated,
}: {
  workspaceID: string;
  projectID: string;
  userID: string;
  onSessionExpired: () => void;
  onCreated: (channel: ConfigurationObject) => void;
}) {
  const [name, setName] = useState("");
  const [visibility, setVisibility] = useState("project");
  const [confirmed, setConfirmed] = useState(false);
  const action = useAction(onSessionExpired);
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (visibility === "restricted" && !confirmed) return;
        void action.run(
          "channel.create",
          {
            name,
            visibility,
            member_user_ids: visibility === "restricted" ? [userID] : [],
          },
          (body) => api.createChannel(workspaceID, projectID, body),
          onCreated,
        );
      }}
    >
      <fieldset disabled={action.busy}>
        <legend>创建频道</legend>
        <Field label="频道名称">
          <input
            required
            maxLength={120}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field label="频道可见范围">
          <select
            value={visibility}
            onChange={(e) => setVisibility(e.target.value)}
          >
            <option value="project">项目成员可浏览</option>
            <option value="restricted">仅明确加入的成员</option>
          </select>
        </Field>
        {visibility === "restricted" ? (
          <label className="configuration-checkbox">
            <input
              type="checkbox"
              required
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            将我明确加入此私密频道；创建后由我添加其他成员。
          </label>
        ) : null}
        <button type="submit">创建并打开频道</button>
      </fieldset>
      <Feedback busy={action.busy} message={action.message} />
    </form>
  );
}
function MemberEditor({
  workspaceID,
  currentUserID,
  object,
  onSessionExpired,
}: {
  workspaceID: string;
  currentUserID: string;
  object: ConfigurationObject;
  onSessionExpired: () => void;
}) {
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      api.members(workspaceID, object.kind, object.id, after, signal),
    [workspaceID, object.kind, object.id],
  );
  const members = usePage(load, onSessionExpired);
  const loadCandidates = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      api.members(
        workspaceID,
        object.kind === "project" ? "workspace" : "project",
        object.kind === "project" ? workspaceID : object.projectID!,
        after,
        signal,
      ),
    [workspaceID, object.kind, object.projectID],
  );
  const candidates = usePage(loadCandidates, onSessionExpired);
  const action = useAction(onSessionExpired);
  const [user, setUser] = useState("");
  const [role, setRole] = useState("contributor");
  const refresh = () => {
    members.refresh();
    candidates.refresh();
    setUser("");
  };
  const add = (event: FormEvent) => {
    event.preventDefault();
    if (!user) return;
    void action.run(
      `add:${object.kind}:${user}`,
      object.kind === "project"
        ? { expected_role: null, role }
        : { expected_member: false },
      (body) =>
        api.member(workspaceID, object.kind, object.id, user, "PUT", body),
      refresh,
    );
  };
  return (
    <div>
      <p>
        {object.kind === "project"
          ? "仅配置普通成员角色。撤销项目角色会清除其在本项目下的私密频道和讨论授权；工作区可见项目仍保留只读基线。管理员交接尚未开放。"
          : "添加频道成员不会改变项目角色。移除成员会同时清理其在此频道下的私密讨论授权；重新加入需要重新授权。"}
      </p>
      {members.error ? <p role="alert">{members.error}</p> : null}
      {!members.page && !members.error ? <p>正在加载成员…</p> : null}
      <ul className="configuration-members">
        {members.page?.items.map((member) => (
          <MemberRow
            key={`${member.id}:${member.role ?? "member"}`}
            workspaceID={workspaceID}
            object={object}
            member={member}
            currentUserID={currentUserID}
            onChanged={refresh}
            onSessionExpired={onSessionExpired}
          />
        ))}
      </ul>
      {members.page?.items.length === 0 ? <p>当前没有成员。</p> : null}
      <PageControls page={members} />
      <form onSubmit={add}>
        <fieldset disabled={action.busy || !members.page || !candidates.page}>
          <legend>添加成员</legend>
          <Field label="选择成员">
            <select
              required
              value={user}
              onChange={(e) => setUser(e.target.value)}
            >
              <option value="">
                选择已加入{object.kind === "project" ? "工作区" : "项目"}的成员
              </option>
              {candidates.page?.items
                .filter(
                  (m) =>
                    m.eligible &&
                    !members.page?.items.some(
                      (existing) => existing.id === m.id,
                    ),
                )
                .map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name} · {m.id}
                  </option>
                ))}
            </select>
          </Field>
          {object.kind === "project" ? (
            <Field label="授予项目角色">
              <select value={role} onChange={(e) => setRole(e.target.value)}>
                <option value="viewer">viewer · 只读</option>
                <option value="contributor">contributor · 可参与协作</option>
                <option value="decider">decider · 可确认决策</option>
              </select>
            </Field>
          ) : null}
          {role === "decider" && object.kind === "project" ? (
            <p>此角色可人工确认有权读取证据的 Decision。</p>
          ) : null}
          <button type="submit" disabled={!user}>
            明确添加成员
          </button>
        </fieldset>
      </form>
      {candidates.error ? <p role="alert">{candidates.error}</p> : null}
      <PageControls page={candidates} />
      <Feedback busy={action.busy} message={action.message} />
    </div>
  );
}
function MemberRow({
  workspaceID,
  currentUserID,
  object,
  member,
  onChanged,
  onSessionExpired,
}: {
  workspaceID: string;
  currentUserID: string;
  object: ConfigurationObject;
  member: ConfigurationMember;
  onChanged: () => void;
  onSessionExpired: () => void;
}) {
  const [role, setRole] = useState<ProjectRole>(member.role ?? "viewer");
  const action = useAction(onSessionExpired, onChanged);
  return (
    <li>
      <p>
        {member.name} · {member.id}
        {member.eligible ? "" : " · 当前不可授予新权限"}
      </p>
      {member.role === "admin" || member.id === currentUserID ? (
        <p>admin · 管理权交接尚未开放</p>
      ) : (
        <fieldset disabled={action.busy}>
          {object.kind === "project" ? (
            <>
              <Field label={`调整 ${member.name} 的角色`}>
                <select
                  value={role}
                  onChange={(e) => setRole(e.target.value as ProjectRole)}
                >
                  <option value="viewer">viewer · 只读</option>
                  <option value="contributor">contributor · 可参与协作</option>
                  <option value="decider">decider · 可确认决策</option>
                </select>
              </Field>
              {role === "decider" ? (
                <p>可人工确认有权读取证据的 Decision。</p>
              ) : null}
              <button
                type="button"
                disabled={!member.eligible || role === member.role}
                onClick={() =>
                  void action.run(
                    `role:${member.id}`,
                    { expected_role: member.role, role },
                    (body) =>
                      api.member(
                        workspaceID,
                        object.kind,
                        object.id,
                        member.id,
                        "PUT",
                        body,
                      ),
                    onChanged,
                  )
                }
              >
                保存角色
              </button>
            </>
          ) : null}
          <button
            type="button"
            onClick={() =>
              void action.run(
                `remove:${member.id}`,
                object.kind === "project"
                  ? { expected_role: member.role }
                  : { expected_member: true },
                (body) =>
                  api.member(
                    workspaceID,
                    object.kind,
                    object.id,
                    member.id,
                    "DELETE",
                    body,
                  ),
                onChanged,
              )
            }
          >
            {object.kind === "project"
              ? "撤销角色及从属私密授权"
              : "移除成员及从属私密授权"}
          </button>
        </fieldset>
      )}
      <Feedback busy={action.busy} message={action.message} />
    </li>
  );
}
