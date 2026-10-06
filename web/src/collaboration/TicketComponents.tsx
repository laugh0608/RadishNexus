import { useCallback, useEffect, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { useAction, usePage } from "../workspace/configuration-hooks";
import { Field, Feedback, PageControls } from "../workspace/configuration-ui";
import {
  deliveryConfigurationClient,
  type DeliveryObject,
} from "../workspace/delivery-configuration-api";
import { collaborationPagePath } from "./api";
import {
  componentPagePath,
  ticketComponentClient,
  type TicketComponentClient,
  type TicketComponentAssociation,
  type TicketSummary,
} from "./ticket-component-api";

interface Context {
  workspaceID: string;
  onSessionExpired: () => void;
  client?: TicketComponentClient;
}
export function TicketComponents(
  props: Context & {
    ticketID: string;
    ticketTitle: string;
    onChanged: () => void;
    onUnavailable: () => void;
  },
) {
  const { workspaceID, ticketID, onSessionExpired, onUnavailable } = props,
    client = props.client ?? ticketComponentClient;
  const [canLink, setCanLink] = useState(false);
  const [removing, setRemoving] =
    useState<TicketComponentAssociation<DeliveryObject> | null>(null);
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      let page;
      try {
        page = await client.components(workspaceID, ticketID, after, signal);
      } catch (error) {
        if (
          !signal.aborted &&
          error instanceof AuthRequestError &&
          (error.status === 403 || error.status === 404)
        )
          onUnavailable();
        throw error;
      }
      if (!signal.aborted) {
        setCanLink(page.canLink);
        if (!page.canLink) setRemoving(null);
      }
      return page;
    },
    [client, workspaceID, ticketID, onUnavailable],
  );
  const page = usePage(load, onSessionExpired);
  const changed = () => {
    setRemoving(null);
    page.refresh();
    props.onChanged();
  };
  return (
    <section className="collaboration-panel" aria-label="涉及组件">
      <h2>涉及组件</h2>
      <p>登记此事项涉及的软件组件；关联不表示事项已构建或部署。</p>
      <PageControls page={page} />
      {page.error ? (
        <p role="alert">{page.error}</p>
      ) : !page.page ? (
        <p role="status">正在读取组件关联…</p>
      ) : page.page.items.length === 0 ? (
        <p>尚未关联组件。</p>
      ) : (
        <ul className="configuration-members">
          {page.page.items.map((link) => (
            <li key={link.linkID}>
              <a href={componentPagePath(workspaceID, link.target.id)}>
                {link.target.name} · {link.target.key}
              </a>
              <span> · {link.target.status} </span>
              {link.canUnlink ? (
                <button type="button" onClick={() => setRemoving(link)}>
                  解除组件关联
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      {canLink && !page.error ? (
        <LinkComponent
          {...props}
          client={client}
          onChanged={changed}
          onUnavailable={() => {
            setCanLink(false);
            setRemoving(null);
            page.refresh();
            props.onChanged();
          }}
        />
      ) : !page.error && page.page ? (
        <p>当前仅可读取关联；新增或解除需要事项所属活跃项目的协作权限。</p>
      ) : null}
      {removing && canLink && !page.error ? (
        <UnlinkComponent
          key={removing.linkID}
          {...props}
          client={client}
          linkID={removing.linkID}
          componentName={removing.target.name}
          onChanged={changed}
          onCancel={() => setRemoving(null)}
          onUnavailable={() => {
            setCanLink(false);
            setRemoving(null);
            page.refresh();
            props.onChanged();
          }}
        />
      ) : null}
    </section>
  );
}
function LinkComponent(
  props: Context & {
    client: TicketComponentClient;
    ticketID: string;
    ticketTitle: string;
    onChanged: () => void;
    onUnavailable: () => void;
  },
) {
  const load = useCallback(
    (after: string | undefined, signal: AbortSignal) =>
      deliveryConfigurationClient.list(
        props.workspaceID,
        "component",
        after,
        signal,
      ),
    [props.workspaceID],
  );
  const candidates = usePage(load, props.onSessionExpired),
    action = useAction(props.onSessionExpired, props.onUnavailable);
  const [selected, setSelected] = useState<DeliveryObject | null>(null),
    [confirmed, setConfirmed] = useState(false);
  return (
    <form
      className="configuration-panel"
      aria-label="关联组件"
      onSubmit={(e) => {
        e.preventDefault();
        if (!selected || !confirmed) return;
        void action.run(
          `ticket.component.link:${props.ticketID}`,
          { component_id: selected.id, confirmed: true },
          (body) => props.client.link(props.workspaceID, props.ticketID, body),
          () => {
            setSelected(null);
            setConfirmed(false);
            props.onChanged();
          },
        );
      }}
    >
      <h3>关联组件</h3>
      <fieldset disabled={action.busy || action.uncertain}>
        <PageControls page={candidates} />
        {candidates.error ? (
          <p role="alert">{candidates.error}</p>
        ) : !candidates.page ? (
          <p role="status">正在加载候选组件…</p>
        ) : candidates.page.items.length === 0 ? (
          <p>暂无组件，请先从工作台创建软件组件。</p>
        ) : (
          <Field label="选择组件">
            <select
              value={
                candidates.page.items.some((c) => c.id === selected?.id)
                  ? selected?.id
                  : ""
              }
              onChange={(e) => {
                setSelected(
                  candidates.page?.items.find((c) => c.id === e.target.value) ??
                    null,
                );
                setConfirmed(false);
              }}
            >
              <option value="">请选择</option>
              {candidates.page.items.map((c) => (
                <option
                  key={c.id}
                  value={c.id}
                  disabled={c.status === "retired"}
                >
                  {c.name} · {c.key} · {c.status}
                </option>
              ))}
            </select>
          </Field>
        )}
        {selected ? (
          <>
            <p>
              将事项「{props.ticketTitle}」关联到组件「{selected.name}」。
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
      {action.uncertain ? (
        <p>结果尚未确认，已保留原请求。请重试以确认结果。</p>
      ) : null}
      <button
        type="submit"
        disabled={
          action.busy ||
          !selected ||
          !confirmed ||
          (!action.uncertain &&
            (!candidates.page ||
              !!candidates.error ||
              !candidates.page.items.some(
                (c) => c.id === selected.id && c.status !== "retired",
              )))
        }
      >
        {action.uncertain ? "重试原关联请求" : "确认关联组件"}
      </button>
      <Feedback {...action} />
    </form>
  );
}
function UnlinkComponent(
  props: Context & {
    client: TicketComponentClient;
    ticketID: string;
    ticketTitle: string;
    componentName: string;
    linkID: string;
    onChanged: () => void;
    onCancel: () => void;
    onUnavailable: () => void;
  },
) {
  const [confirmed, setConfirmed] = useState(false),
    action = useAction(props.onSessionExpired, props.onUnavailable);
  const { client, workspaceID, ticketID, onUnavailable, onSessionExpired } =
    props;
  useEffect(() => {
    let controller: AbortController | undefined;
    const verify = () => {
      controller?.abort();
      const current = new AbortController();
      controller = current;
      void client
        .components(workspaceID, ticketID, undefined, current.signal)
        .then(
          (page) => {
            if (!current.signal.aborted && !page.canLink) onUnavailable();
          },
          (error) => {
            if (current.signal.aborted) return;
            if (error instanceof AuthRequestError && error.status === 401)
              onSessionExpired();
            else if (
              error instanceof AuthRequestError &&
              (error.status === 403 || error.status === 404)
            )
              onUnavailable();
          },
        );
    };
    verify();
    window.addEventListener("focus", verify);
    return () => {
      controller?.abort();
      window.removeEventListener("focus", verify);
    };
  }, [client, workspaceID, ticketID, onUnavailable, onSessionExpired]);
  return (
    <form
      aria-label="解除事项与组件关联"
      onSubmit={(e) => {
        e.preventDefault();
        if (!confirmed) return;
        void action.run(
          `ticket.component.unlink:${props.ticketID}:${props.linkID}`,
          { confirmed: true },
          (body) =>
            props.client.unlink(
              props.workspaceID,
              props.ticketID,
              props.linkID,
              body,
            ),
          props.onChanged,
        );
      }}
    >
      <p>
        解除事项「{props.ticketTitle}」与组件「{props.componentName}
        」的这一条关联。两端对象和历史记录会保留。
      </p>
      <label>
        <input
          type="checkbox"
          checked={confirmed}
          disabled={action.busy || action.uncertain}
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
export function ComponentTickets(
  props: Context & {
    componentID: string;
    componentName: string;
    onUnavailable: () => void;
  },
) {
  const { workspaceID, componentID, onSessionExpired, onUnavailable } = props,
    client = props.client ?? ticketComponentClient;
  const [removing, setRemoving] =
    useState<TicketComponentAssociation<TicketSummary> | null>(null);
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      try {
        return await client.tickets(workspaceID, componentID, after, signal);
      } catch (e) {
        if (
          !signal.aborted &&
          e instanceof AuthRequestError &&
          (e.status === 403 || e.status === 404)
        )
          onUnavailable();
        throw e;
      }
    },
    [client, workspaceID, componentID, onUnavailable],
  );
  const page = usePage(load, onSessionExpired);
  return (
    <section className="configuration-panel" aria-label="涉及此组件的事项">
      <h2>涉及此组件的事项</h2>
      <p>仅显示当前有权读取的事项。</p>
      <PageControls page={page} />
      {page.error ? (
        <p role="alert">{page.error}</p>
      ) : !page.page ? (
        <p role="status">正在读取事项…</p>
      ) : page.page.items.length === 0 ? (
        <p>暂无可读的关联事项。</p>
      ) : (
        <ul className="configuration-members">
          {page.page.items.map((link) => (
            <li key={link.linkID}>
              <a
                href={
                  collaborationPagePath(
                    workspaceID,
                    "ticket",
                    link.target.id,
                  ) ?? undefined
                }
              >
                {link.target.title}
              </a>
              <span>
                {" · "}
                {link.target.status} · {link.target.projectID}{" "}
              </span>
              {link.canUnlink ? (
                <button type="button" onClick={() => setRemoving(link)}>
                  解除事项关联
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      {removing && !page.error ? (
        <UnlinkComponent
          {...props}
          key={removing.linkID}
          client={client}
          ticketID={removing.target.id}
          ticketTitle={removing.target.title}
          linkID={removing.linkID}
          onCancel={() => setRemoving(null)}
          onChanged={() => {
            setRemoving(null);
            page.refresh();
          }}
          onUnavailable={() => {
            setRemoving(null);
            page.refresh();
          }}
        />
      ) : null}
    </section>
  );
}
