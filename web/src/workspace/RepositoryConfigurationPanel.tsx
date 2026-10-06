import { useCallback, useEffect, useState, type FormEvent } from "react";
import { AuthRequestError } from "../auth/api";
import { useAction, usePage } from "./configuration-hooks";
import { Field, Feedback, PageControls } from "./configuration-ui";
import type { DeliveryObject } from "./delivery-configuration-api";
import {
  repositoryConfigurationClient,
  type RepositoryConfigurationClient,
  type RepositoryObject,
  type RepositoryAssociation,
} from "./repository-configuration-api";

export interface RepositoryContext {
  workspaceID: string;
  workspaceName: string;
  isOwner: boolean;
  onSessionExpired: () => void;
  repositoryClient?: RepositoryConfigurationClient;
}

export function RepositoryBrowser(
  props: RepositoryContext & {
    initialID?: string;
    onOpenComponent: (id: string) => void;
  },
) {
  const { workspaceID, onSessionExpired } = props;
  const client = props.repositoryClient ?? repositoryConfigurationClient;
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      client.list(workspaceID, after, signal),
    [client, workspaceID],
  );
  const page = usePage(load, onSessionExpired);
  const [selected, setSelected] = useState(props.initialID ?? "");
  const [creating, setCreating] = useState(false);
  const refresh = page.refresh;
  const unavailable = useCallback(() => {
    setSelected("");
    setCreating(false);
    refresh();
  }, [refresh]);
  return (
    <section aria-label="代码库映射">
      <h3>代码库映射</h3>
      <p>工作区：{props.workspaceName}。映射信息对当前工作区的活跃成员可见。</p>
      <PageControls page={page} />
      {page.error ? (
        <p role="alert">{page.error}</p>
      ) : !page.page ? (
        <p role="status">正在加载代码库…</p>
      ) : page.page.items.length === 0 ? (
        <p>尚未建立代码库映射。</p>
      ) : (
        <ul className="configuration-members">
          {page.page.items.map((r) => (
            <li key={r.id}>
              <button
                type="button"
                onClick={() => {
                  setSelected(r.id);
                  setCreating(false);
                }}
              >
                {r.name}
              </button>
              <span>
                {r.provider} · {r.providerOrigin} · {r.externalID}
              </span>
            </li>
          ))}
        </ul>
      )}
      {props.isOwner && !page.error ? (
        <button
          type="button"
          onClick={() => {
            setCreating(!creating);
            setSelected("");
          }}
        >
          {creating ? "关闭代码库表单" : "创建 Repository"}
        </button>
      ) : null}
      {creating && !page.error ? (
        <CreateRepository
          {...props}
          client={client}
          onUnavailable={unavailable}
          onCreated={(r) => {
            setSelected(r.id);
            setCreating(false);
            refresh();
          }}
        />
      ) : null}
      {selected && !page.error ? (
        <RepositoryDetail
          key={selected}
          {...props}
          id={selected}
          client={client}
          onUnavailable={unavailable}
        />
      ) : null}
    </section>
  );
}

function CreateRepository(
  props: RepositoryContext & {
    client: RepositoryConfigurationClient;
    onUnavailable: () => void;
    onCreated: (r: RepositoryObject) => void;
  },
) {
  const action = useAction(props.onSessionExpired, props.onUnavailable);
  const [values, setValues] = useState({
    name: "",
    provider: "",
    provider_origin: "",
    external_id: "",
    web_url: "",
    default_branch: "",
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    void action.run(
      "repository.create",
      values,
      (body) => props.client.create(props.workspaceID, body),
      props.onCreated,
    );
  };
  return (
    <form
      className="configuration-panel"
      aria-label="创建代码库映射"
      onSubmit={submit}
    >
      <h4>创建代码库映射</h4>
      <p>
        请核对外部仓库的稳定 ID
        和地址。创建后本批暂不支持修改映射信息；不会验证连接或授予外部代码访问权。
      </p>
      <fieldset disabled={action.busy || action.uncertain}>
        <Field label="代码库名称">
          <input
            required
            maxLength={120}
            value={values.name}
            onChange={(e) => setValues({ ...values, name: e.target.value })}
          />
        </Field>
        <Field label="代码托管服务">
          <select
            required
            value={values.provider}
            onChange={(e) => setValues({ ...values, provider: e.target.value })}
          >
            <option value="">请选择</option>
            <option value="github">GitHub</option>
            <option value="gitlab">GitLab</option>
            <option value="gitea">Gitea</option>
          </select>
        </Field>
        <Field label="服务地址（HTTPS origin）">
          <input
            required
            type="url"
            maxLength={512}
            placeholder="https://git.example.test"
            value={values.provider_origin}
            onChange={(e) =>
              setValues({ ...values, provider_origin: e.target.value })
            }
          />
        </Field>
        <p>
          服务地址只含主机和可选端口。同一地址下以不同路径区分的多个独立实例暂不支持。
        </p>
        <Field label="外部稳定 ID">
          <input
            required
            maxLength={255}
            value={values.external_id}
            onChange={(e) =>
              setValues({ ...values, external_id: e.target.value })
            }
          />
        </Field>
        <Field label="仓库浏览地址">
          <input
            required
            type="url"
            maxLength={2048}
            value={values.web_url}
            onChange={(e) => setValues({ ...values, web_url: e.target.value })}
          />
        </Field>
        <Field label="默认分支">
          <input
            required
            maxLength={255}
            value={values.default_branch}
            onChange={(e) =>
              setValues({ ...values, default_branch: e.target.value })
            }
          />
        </Field>
      </fieldset>
      {action.uncertain ? (
        <p>结果尚未确认，输入已保留。请重试原请求以确认结果。</p>
      ) : null}
      <button type="submit" disabled={action.busy}>
        {action.uncertain ? "重试原创建请求" : "创建映射"}
      </button>
      <Feedback {...action} />
    </form>
  );
}

function RepositoryDetail(
  props: RepositoryContext & {
    id: string;
    client: RepositoryConfigurationClient;
    onUnavailable: () => void;
    onOpenComponent: (id: string) => void;
  },
) {
  const { workspaceID, id, client, onSessionExpired, onUnavailable } = props;
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{
    value?: RepositoryObject;
    error?: string;
    revision: number;
  }>({ revision: -1 });
  useEffect(() => {
    const refresh = () => setRevision((r) => r + 1);
    window.addEventListener("focus", refresh);
    return () => window.removeEventListener("focus", refresh);
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    void client.read(workspaceID, id, controller.signal).then(
      (value) => {
        if (!controller.signal.aborted) setState({ value, revision });
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
            error: e instanceof Error ? e.message : "无法读取代码库。",
            revision,
          });
      },
    );
    return () => controller.abort();
  }, [workspaceID, id, client, revision, onSessionExpired, onUnavailable]);
  const value = state.value;
  return (
    <section className="configuration-panel" aria-label="代码库详情">
      <button type="button" onClick={() => setRevision((r) => r + 1)}>
        刷新代码库详情
      </button>
      {!value ? (
        <p
          role={state.revision === revision && state.error ? "alert" : "status"}
        >
          {state.revision === revision && state.error
            ? state.error
            : "正在读取代码库…"}
        </p>
      ) : (
        <>
          <h4>{value.name}</h4>
          <p>
            {value.provider} · {value.providerOrigin} · {value.externalID}
          </p>
          <p>默认分支：{value.defaultBranch}（人工登记）</p>
          <a
            href={value.webURL}
            target="_blank"
            rel="noopener noreferrer"
            referrerPolicy="no-referrer"
          >
            打开外部仓库 ↗
          </a>
          <RepositoryComponents {...props} repository={value} />
        </>
      )}
    </section>
  );
}

function RepositoryComponents(
  props: RepositoryContext & {
    repository: RepositoryObject;
    client: RepositoryConfigurationClient;
    onUnavailable: () => void;
    onOpenComponent: (id: string) => void;
  },
) {
  const { client, workspaceID, repository, onSessionExpired } = props;
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      client.repositoryComponents(workspaceID, repository.id, after, signal),
    [client, workspaceID, repository.id],
  );
  const page = usePage(load, onSessionExpired);
  const [removing, setRemoving] =
    useState<RepositoryAssociation<DeliveryObject> | null>(null);
  return (
    <section aria-label="使用此仓库的组件">
      <h5>使用此仓库的组件</h5>
      <PageControls page={page} />
      {page.error ? (
        <p role="alert">{page.error}</p>
      ) : !page.page ? (
        <p role="status">正在读取关联…</p>
      ) : page.page.items.length === 0 ? (
        <p>尚未关联组件。可从组件详情选择此仓库。</p>
      ) : (
        <ul className="configuration-members">
          {page.page.items.map((link) => (
            <li key={link.linkID}>
              <button
                type="button"
                onClick={() => props.onOpenComponent(link.target.id)}
              >
                {link.target.name} · {link.target.key}
              </button>
              {link.canUnlink ? (
                <button type="button" onClick={() => setRemoving(link)}>
                  解除关联
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      {removing && !page.error ? (
        <UnlinkRepository
          {...props}
          key={removing.linkID}
          componentID={removing.target.id}
          componentName={removing.target.name}
          repositoryName={repository.name}
          linkID={removing.linkID}
          onCancel={() => setRemoving(null)}
          onChanged={() => {
            setRemoving(null);
            page.refresh();
          }}
        />
      ) : null}
    </section>
  );
}

export function ComponentRepositories(
  props: RepositoryContext & {
    component: DeliveryObject;
    onOpenRepository: (id: string) => void;
    onUnavailable: () => void;
  },
) {
  const client = props.repositoryClient ?? repositoryConfigurationClient;
  const { workspaceID, component, onSessionExpired } = props;
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      client.componentRepositories(workspaceID, component.id, after, signal),
    [client, workspaceID, component.id],
  );
  const page = usePage(load, onSessionExpired);
  const [removing, setRemoving] =
    useState<RepositoryAssociation<RepositoryObject> | null>(null);
  return (
    <section aria-label="组件代码库">
      <h5>关联代码库</h5>
      <PageControls page={page} />
      {page.error ? (
        <p role="alert">{page.error}</p>
      ) : !page.page ? (
        <p role="status">正在读取关联…</p>
      ) : (
        <>
          {page.page.items.length === 0 ? (
            <p>此组件尚未关联代码库。</p>
          ) : (
            <ul className="configuration-members">
              {page.page.items.map((link) => (
                <li key={link.linkID}>
                  <button
                    type="button"
                    onClick={() => props.onOpenRepository(link.target.id)}
                  >
                    {link.target.name} · {link.target.provider}
                  </button>
                  {link.canUnlink ? (
                    <button type="button" onClick={() => setRemoving(link)}>
                      解除关联
                    </button>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      {component.canLinkRepository && !page.error ? (
        <LinkRepository {...props} client={client} onChanged={page.refresh} />
      ) : !page.error ? (
        <p>新关联由工作区 owner 为 active 组件配置。</p>
      ) : null}
      {removing && !page.error ? (
        <UnlinkRepository
          {...props}
          key={removing.linkID}
          client={client}
          componentID={component.id}
          componentName={component.name}
          repositoryName={removing.target.name}
          linkID={removing.linkID}
          onCancel={() => setRemoving(null)}
          onChanged={() => {
            setRemoving(null);
            page.refresh();
          }}
        />
      ) : null}
    </section>
  );
}

function LinkRepository(
  props: RepositoryContext & {
    component: DeliveryObject;
    client: RepositoryConfigurationClient;
    onChanged: () => void;
    onUnavailable: () => void;
  },
) {
  const { client, workspaceID, onSessionExpired } = props;
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      client.list(workspaceID, after, signal),
    [client, workspaceID],
  );
  const candidates = usePage(load, onSessionExpired);
  const [selected, setSelected] = useState<RepositoryObject | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const action = useAction(onSessionExpired, props.onUnavailable);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!selected || !confirmed) return;
    void action.run(
      `component.repository.link:${props.component.id}`,
      { repository_id: selected.id, confirmed: true },
      (body) => client.link(workspaceID, props.component.id, body),
      () => {
        setSelected(null);
        setConfirmed(false);
        props.onChanged();
      },
    );
  };
  return (
    <form
      className="configuration-panel"
      aria-label="关联已有代码库"
      onSubmit={submit}
    >
      <h5>关联已有代码库</h5>
      <fieldset disabled={action.busy || action.uncertain}>
        <PageControls page={candidates} />
        {candidates.error ? (
          <p role="alert">{candidates.error}</p>
        ) : !candidates.page ? (
          <p role="status">正在加载候选仓库…</p>
        ) : candidates.page.items.length === 0 ? (
          <p>暂无候选仓库，请先在代码库配置中创建映射。</p>
        ) : (
          <Field label="选择代码库">
            <select
              value={
                candidates.page.items.some((r) => r.id === selected?.id)
                  ? selected?.id
                  : ""
              }
              onChange={(e) => {
                setSelected(
                  candidates.page?.items.find((r) => r.id === e.target.value) ??
                    null,
                );
                setConfirmed(false);
              }}
            >
              <option value="">请选择</option>
              {candidates.page.items.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.name} · {r.providerOrigin} · {r.externalID}
                </option>
              ))}
            </select>
          </Field>
        )}
        {selected ? (
          <>
            <p>
              将「{props.component.name}」关联到「{selected.name}
              」。这只登记代码库关系，不创建构建或部署。
            </p>
            <label>
              <input
                type="checkbox"
                checked={confirmed}
                onChange={(e) => setConfirmed(e.target.checked)}
              />
              确认关联这两个对象
            </label>
          </>
        ) : null}
      </fieldset>
      {action.uncertain ? <p>结果尚未确认，请重试原关联请求。</p> : null}
      <button
        type="submit"
        disabled={
          action.busy ||
          !selected ||
          !confirmed ||
          (!action.uncertain &&
            (!!candidates.error ||
              !candidates.page ||
              !candidates.page.items.some((r) => r.id === selected.id)))
        }
      >
        {action.uncertain ? "重试原关联请求" : "确认关联"}
      </button>
      <Feedback {...action} />
    </form>
  );
}

function UnlinkRepository(
  props: RepositoryContext & {
    client: RepositoryConfigurationClient;
    componentID: string;
    componentName: string;
    repositoryName: string;
    linkID: string;
    onChanged: () => void;
    onCancel: () => void;
    onUnavailable: () => void;
  },
) {
  const [confirmed, setConfirmed] = useState(false);
  const action = useAction(props.onSessionExpired, props.onUnavailable);
  return (
    <form
      aria-label={`解除 ${props.repositoryName} 关联`}
      onSubmit={(e) => {
        e.preventDefault();
        if (!confirmed) return;
        void action.run(
          `component.repository.unlink:${props.componentID}:${props.linkID}`,
          { confirmed: true },
          (body) =>
            props.client.unlink(
              props.workspaceID,
              props.componentID,
              props.linkID,
              body,
            ),
          () => {
            setConfirmed(false);
            props.onChanged();
          },
        );
      }}
    >
      <p>
        解除「{props.componentName}」与「{props.repositoryName}
        」的当前关联。两端对象和历史来源会保留，可重新建立关联。
      </p>
      <label>
        <input
          type="checkbox"
          disabled={action.busy || action.uncertain}
          checked={confirmed}
          onChange={(e) => setConfirmed(e.target.checked)}
        />
        确认解除这一条关联
      </label>
      <button type="submit" disabled={action.busy || !confirmed}>
        {action.uncertain ? "重试原解除请求" : "确认解除"}
      </button>
      <button
        type="button"
        disabled={action.busy || action.uncertain}
        onClick={props.onCancel}
      >
        取消
      </button>
      <Feedback {...action} />
    </form>
  );
}
