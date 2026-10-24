import { ManagementPanel } from "./ManagementPanel";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { AuthRequestError } from "../auth/api";
import {
  configurationClient as api,
  type ConfigurationObject,
} from "./configuration-api";
import { useAction, usePage } from "./configuration-hooks";
import { PageControls, Feedback, Field } from "./configuration-ui";
import { channelPagePath } from "../channel/api";

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
          {kind === "project" || object.visibility === "restricted" ? (
            <ManagementPanel
              scope={{ workspace: workspaceID, kind, id }}
              userID={userID}
              onSessionExpired={onSessionExpired}
              onChanged={onChanged}
              onLeft={() => navigate("/")}
            />
          ) : (
            <p>此频道沿用项目权限，无独立成员列表。</p>
          )}
          {kind === "project" && object.canManage ? (
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
            将我明确加入此私密频道并成为初始局部管理者；创建后由我添加其他成员。
          </label>
        ) : null}
        <button type="submit">创建并打开频道</button>
      </fieldset>
      <Feedback busy={action.busy} message={action.message} />
    </form>
  );
}
