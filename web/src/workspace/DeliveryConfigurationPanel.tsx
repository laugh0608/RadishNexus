import { useCallback, useEffect, useState, type FormEvent } from "react";
import { AuthRequestError } from "../auth/api";
import { configurationClient } from "./configuration-api";
import {
  deliveryConfigurationClient,
  componentTypes,
  type DeliveryConfigurationClient,
  type DeliveryKind,
  type DeliveryObject,
} from "./delivery-configuration-api";
import { usePage, useAction } from "./configuration-hooks";
import { PageControls, Feedback, Field } from "./configuration-ui";
import { EnvironmentAuthorizationEditor } from "./EnvironmentAuthorizationEditor";

interface Context {
  workspaceID: string;
  workspaceName: string;
  userID: string;
  isOwner: boolean;
  onSessionExpired: () => void;
  client: DeliveryConfigurationClient;
}

export function DeliveryConfigurationPanel(
  props: Omit<Context, "client"> & { client?: DeliveryConfigurationClient },
) {
  const [open, setOpen] = useState(false);
  return (
    <section className="foundation-configuration" aria-label="组件与环境">
      <button
        type="button"
        className="secondary-button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        组件与环境
      </button>
      {open ? (
        <DeliveryBrowser
          {...props}
          client={props.client ?? deliveryConfigurationClient}
        />
      ) : null}
    </section>
  );
}

function DeliveryBrowser(props: Context) {
  const [kind, setKind] = useState<DeliveryKind>("component");
  return (
    <div className="configuration-panel">
      <Field label="交付配置类型">
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value as DeliveryKind)}
        >
          <option value="component">Component · 软件组件</option>
          <option value="environment">Environment · 部署环境</option>
        </select>
      </Field>
      <ObjectBrowser key={kind} {...props} kind={kind} />
    </div>
  );
}
function ObjectBrowser(props: Context & { kind: DeliveryKind }) {
  const { client, workspaceID, kind, onSessionExpired, isOwner } = props;
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      client.list(workspaceID, kind, after, signal),
    [client, workspaceID, kind],
  );
  const page = usePage(load, onSessionExpired);
  const [selected, setSelected] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const refreshObjects = page.refresh;
  const unavailable = useCallback(() => {
    setSelected(null);
    refreshObjects();
  }, [refreshObjects]);
  return (
    <>
      <h3>{kind === "component" ? "软件组件" : "部署环境"}</h3>
      <p>
        工作区：{props.workspaceName}
        。责任团队只表示归属；环境记录权须单独授予。
      </p>
      <PageControls page={page} />
      {page.error ? (
        <p role="alert">{page.error}</p>
      ) : !page.page ? (
        <p role="status">正在加载…</p>
      ) : page.page.items.length === 0 ? (
        <p>当前没有{kind === "component" ? "组件" : "环境"}。</p>
      ) : (
        <ul className="configuration-members">
          {page.page.items.map((item) => (
            <li key={item.id}>
              <button
                type="button"
                onClick={() => {
                  setSelected(item.id);
                  setCreating(false);
                }}
              >
                {item.name} · {item.key}
              </button>
              <span>
                {item.category} · {item.status}
              </span>
            </li>
          ))}
        </ul>
      )}
      {isOwner ? (
        <button
          type="button"
          onClick={() => {
            setCreating(!creating);
            setSelected(null);
          }}
        >
          {creating
            ? "关闭创建表单"
            : kind === "component"
              ? "创建 Component"
              : "创建 staging Environment"}
        </button>
      ) : null}
      {creating && !page.error ? (
        <CreateObject
          {...props}
          onUnavailable={() => {
            setCreating(false);
            page.refresh();
          }}
          onCreated={(object) => {
            setCreating(false);
            setSelected(object.id);
            page.refresh();
          }}
        />
      ) : null}
      {selected && !page.error ? (
        <ObjectDetail
          key={selected}
          {...props}
          id={selected}
          onUnavailable={unavailable}
        />
      ) : null}
    </>
  );
}
function CreateObject({
  workspaceID,
  kind,
  client,
  onCreated,
  onSessionExpired,
  onUnavailable,
}: Context & {
  kind: DeliveryKind;
  onUnavailable: () => void;
  onCreated: (object: DeliveryObject) => void;
}) {
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      configurationClient.teams(workspaceID, after, signal),
    [workspaceID],
  );
  const teams = usePage(load, onSessionExpired);
  const action = useAction(onSessionExpired, onUnavailable);
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [team, setTeam] = useState("");
  const [type, setType] = useState("");
  const submit = (event: FormEvent) => {
    event.preventDefault();
    void action.run(
      `${kind}.create`,
      {
        name,
        key,
        owner_team_id: team,
        ...(kind === "component" ? { type } : { classification: "staging" }),
      },
      (body) => client.create(workspaceID, kind, body),
      onCreated,
    );
  };
  return (
    <form
      onSubmit={submit}
      className="configuration-panel"
      aria-label="创建交付对象"
    >
      <h4>{kind === "component" ? "创建软件组件" : "创建 staging 环境"}</h4>
      <p>
        创建后不会自动授权任何成员。
        {kind === "environment"
          ? "环境分类固定为 staging。"
          : "组件将进入 active 生命周期。"}
      </p>
      {teams.error ? <p role="alert">{teams.error}</p> : null}
      <fieldset disabled={action.busy || !teams.page}>
        <Field label="名称">
          <input
            required
            maxLength={120}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field label="标识 key">
          <input
            required
            pattern={"[a-z][a-z0-9\\-]{0,31}"}
            maxLength={32}
            value={key}
            onChange={(e) => setKey(e.target.value)}
          />
        </Field>
        <Field label="责任团队">
          <select
            required
            value={team}
            onChange={(e) => setTeam(e.target.value)}
          >
            <option value="">请选择责任团队</option>
            {team && !teams.page?.items.some((t) => t.id === team) ? (
              <option value={team}>已选择团队 · {team}</option>
            ) : null}
            {teams.page?.items.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name} · {t.id}
              </option>
            ))}
          </select>
        </Field>
        {kind === "component" ? (
          <Field label="组件类型">
            <select
              required
              value={type}
              onChange={(e) => setType(e.target.value)}
            >
              <option value="">请选择组件类型</option>
              {componentTypes.map((value) => (
                <option key={value}>{value}</option>
              ))}
            </select>
          </Field>
        ) : null}
        <button
          type="submit"
          disabled={!team || (kind === "component" && !type)}
        >
          确认创建
        </button>
      </fieldset>
      <PageControls page={teams} />
      {teams.page?.items.length === 0 ? (
        <p>请先通过“创建团队与项目”中的入口建立责任团队。</p>
      ) : null}
      <Feedback {...action} />
    </form>
  );
}
function ObjectDetail(
  props: Context & {
    kind: DeliveryKind;
    id: string;
    onUnavailable: () => void;
  },
) {
  const { client, workspaceID, kind, id, onSessionExpired, onUnavailable } =
    props;
  const [state, setState] = useState<{
    value?: DeliveryObject;
    error?: string;
    revision: number;
  }>({ revision: -1 });
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    void client.read(workspaceID, kind, id, controller.signal).then(
      (data) => {
        if (!controller.signal.aborted) setState({ value: data, revision });
      },
      (e) => {
        if (controller.signal.aborted) return;
        if (e instanceof AuthRequestError && e.status === 401)
          onSessionExpired();
        else if (
          e instanceof AuthRequestError &&
          (e.status === 403 || e.status === 404)
        )
          onUnavailable();
        else
          setState({
            error: e instanceof Error ? e.message : "无法读取配置。",
            revision,
          });
      },
    );
    return () => controller.abort();
  }, [
    client,
    workspaceID,
    kind,
    id,
    revision,
    onSessionExpired,
    onUnavailable,
  ]);
  const value = state.revision === revision ? state.value : undefined;
  const error = state.revision === revision ? state.error : undefined;
  if (!value)
    return (
      <div>
        <p role={error ? "alert" : "status"}>{error ?? "正在读取对象配置…"}</p>
        <button type="button" onClick={() => setRevision((v) => v + 1)}>
          刷新对象配置
        </button>
      </div>
    );
  return (
    <section className="configuration-panel" aria-label="交付对象详情">
      <h4>{value.name}</h4>
      <p>
        {value.key} · {value.category} · {value.status}
      </p>
      <p>稳定 ID：{value.id}</p>
      <p>责任团队：{value.ownerTeamID ?? "尚未指定"}</p>
      {kind === "environment" ? (
        <>
          <p>
            创建环境不会执行部署。历史部署记录保持可读；只有明确获授权的成员才能记录新的外部部署结果。
          </p>
          {props.isOwner ? (
            <AuthorizationManager {...props} environment={value} />
          ) : (
            <p>授权管理由当前 Workspace owner 操作。</p>
          )}
        </>
      ) : (
        <p>Repository、构建来源与交付关系由后续配置接入。</p>
      )}
    </section>
  );
}
function AuthorizationManager(
  props: Context & { environment: DeliveryObject; onUnavailable: () => void },
) {
  const { client, workspaceID, environment, onSessionExpired } = props;
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      client.authorizations(workspaceID, environment.id, after, signal),
    [client, workspaceID, environment.id],
  );
  const candidatesLoad = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      configurationClient.members(
        workspaceID,
        "workspace",
        workspaceID,
        after,
        signal,
      ),
    [workspaceID],
  );
  const grants = usePage(load, onSessionExpired);
  const candidates = usePage(candidatesLoad, onSessionExpired);
  const [selected, setSelected] = useState("");
  const [candidate, setCandidate] = useState("");
  const refreshGrants = grants.refresh;
  const changed = useCallback(() => refreshGrants(), [refreshGrants]);
  return (
    <section aria-label="环境授权管理">
      <h4>环境授权管理</h4>
      {!environment.canGrant ? (
        <p>
          此环境当前不允许授予记录权。
          {environment.canRevoke ? "已有授权仍可撤销。" : "仅展示已有授权。"}
        </p>
      ) : null}
      <PageControls page={grants} />
      {grants.error ? (
        <p role="alert">{grants.error}</p>
      ) : !grants.page ? (
        <p role="status">正在加载授权…</p>
      ) : grants.page.items.length === 0 ? (
        <p>尚未授予任何成员记录权。</p>
      ) : (
        <ul className="configuration-members">
          {grants.page.items.map((m) => (
            <li key={m.id}>
              <span>
                {m.name} · {m.id} ·{" "}
                {m.authorization?.status === "active" ? "已授予" : "已撤销"}
                {!m.eligible ? " · 当前不可授予" : ""}
              </span>
              {environment.canRevoke ? (
                <button type="button" onClick={() => setSelected(m.id)}>
                  查看并配置授权
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      {environment.canGrant && !grants.error ? (
        <section aria-label="选择授权成员">
          <Field label="授予记录权的成员">
            <select
              value={candidate}
              onChange={(e) => setCandidate(e.target.value)}
            >
              <option value="">请选择工作区成员</option>
              {candidates.page?.items.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name} · {m.id}
                  {m.id === props.userID ? " · 你本人" : ""}
                </option>
              ))}
            </select>
          </Field>
          <PageControls page={candidates} />
          {candidates.error ? <p role="alert">{candidates.error}</p> : null}
          <button
            type="button"
            disabled={
              !candidate ||
              !candidates.page?.items.some((m) => m.id === candidate)
            }
            onClick={() => setSelected(candidate)}
          >
            检查所选成员授权
          </button>
        </section>
      ) : null}
      {selected && !grants.error ? (
        <EnvironmentAuthorizationEditor
          key={selected}
          workspaceID={workspaceID}
          workspaceName={props.workspaceName}
          environment={environment}
          userID={selected}
          currentUserID={props.userID}
          client={client}
          onChanged={changed}
          onSessionExpired={onSessionExpired}
          onUnavailable={props.onUnavailable}
        />
      ) : null}
    </section>
  );
}
