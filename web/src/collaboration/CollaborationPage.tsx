import { useDecisionSupersession } from "./DecisionSupersession";
import { useDecisionTicketCreation } from "./DecisionTicketCreation";
import { useDecisionActions } from "./DecisionActions";
import { useTicketPriorityActions } from "./TicketPriorityActions";
import { ticketPriorityLabels } from "./ticket-priority";
import { useTicketAssignmentActions } from "./TicketAssignmentActions";
import { ManagementPanel } from "../workspace/ManagementPanel";
import { ContextSourceNotice } from "../ContextSourceNotice";
import { projectDecisionsPath } from "../workspace/project-decisions-api";
import { useTicketStatusActions } from "./TicketStatusActions";
import { ticketStatusLabels } from "./ticket-status";
import { TicketComponents } from "./TicketComponents";
import { componentPagePath } from "./ticket-component-api";
import { projectTicketsPath } from "../workspace/project-tickets-api";
import { CreateDocument } from "../document/DocumentPage";
import { documentPath } from "../document/api";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { channelPagePath } from "../channel/api";
import {
  CollaborationRequestError,
  browserCollaborationClient,
  collaborationPagePath,
  newCollaborationOperationID,
  type CollaborationClient,
  type CollaborationEntityType,
  type CollaborationRelation,
  type CollaborationTimelineItem,
  type CollaborationView,
  type DecisionCurrent,
  type ThreadCurrent,
  type ProposeDecisionOutcome,
  type CreateTicketOutcome,
} from "./api";

interface CollaborationPageProps {
  userID?: string;
  navigate?: (path: string) => void;
  workspaceID: string;
  entityType: CollaborationEntityType;
  entityID: string;
  client?: CollaborationClient;
  createOperationID?: () => string;
  onSessionExpired: () => void;
}

type PageState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; view: CollaborationView };

interface PendingProposal {
  id: string;
  question: string;
}

export function CollaborationPage(props: CollaborationPageProps) {
  return (
    <CollaborationPageContent
      key={`${props.workspaceID}/${props.entityType}/${props.entityID}`}
      {...props}
    />
  );
}

function CollaborationPageContent({
  userID,
  navigate,
  workspaceID,
  entityType,
  entityID,
  client = browserCollaborationClient,
  createOperationID = newCollaborationOperationID,
  onSessionExpired,
}: CollaborationPageProps) {
  const [reloadKey, setReloadKey] = useState(0);
  const [state, setState] = useState<PageState>({ status: "loading" });
  const [question, setQuestion] = useState("");
  const [pendingProposal, setPendingProposal] =
    useState<PendingProposal | null>(null);
  const [proposalResult, setProposalResult] =
    useState<ProposeDecisionOutcome | null>(null);
  const [ticketResult, setTicketResult] = useState<CreateTicketOutcome | null>(
    null,
  );
  const [submitting, setSubmitting] = useState<
    "proposal" | "acceptance" | "ticket" | null
  >(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionNotice, setActionNotice] = useState<string | null>(null);

  const activeRead = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    activeRead.current = controller;
    void client
      .loadView(workspaceID, entityType, entityID, controller.signal)
      .then(
        (view) => {
          if (!controller.signal.aborted) {
            setState({ status: "ready", view });
          }
        },
        (error: unknown) => {
          if (controller.signal.aborted) {
            return;
          }
          if (isExpiredSession(error)) {
            setState({ status: "error", message: "登录已失效，请重新登录。" });
            onSessionExpired();
            return;
          }
          setState({ status: "error", message: collaborationError(error) });
        },
      );
    return () => controller.abort();
  }, [client, entityID, entityType, onSessionExpired, reloadKey, workspaceID]);

  const relationChanged = useCallback(() => {
    activeRead.current?.abort();
    setState({ status: "loading" });
    setReloadKey((k) => k + 1);
  }, []);
  const relationUnavailable = useCallback(
    () =>
      setState({ status: "error", message: "当前权限已变化，请重新读取。" }),
    [],
  );
  useEffect(() => {
    const visible = () => {
      if (document.visibilityState === "visible") relationChanged();
    };
    const restored = (event: PageTransitionEvent) => {
      if (event.persisted) relationChanged();
    };
    window.addEventListener("focus", relationChanged);
    document.addEventListener("visibilitychange", visible);
    window.addEventListener("pageshow", restored);
    window.addEventListener("popstate", relationChanged);
    window.addEventListener("pagehide", relationChanged);
    return () => {
      window.removeEventListener("focus", relationChanged);
      document.removeEventListener("visibilitychange", visible);
      window.removeEventListener("pageshow", restored);
      window.removeEventListener("popstate", relationChanged);
      window.removeEventListener("pagehide", relationChanged);
    };
  }, [relationChanged]);
  const decisionActions = useDecisionActions({
    workspaceID,
    decisionID: entityID,
    current:
      state.status === "ready" && isDecisionCurrent(state.view.current)
        ? state.view.current
        : undefined,
    canDecide:
      state.status === "ready" && state.view.capabilities?.accept === true,
    canRetry:
      state.status === "ready" &&
      state.view.capabilities?.retryDecision === true,
    unavailable: state.status === "error",
    client,
    createOperationID,
    onRefresh: relationChanged,
    onUnavailable: relationUnavailable,
    onSessionExpired,
  });
  const decisionCurrent =
    state.status === "ready" && isDecisionCurrent(state.view.current)
      ? state.view.current
      : undefined;
  const decisionTicketCreation = useDecisionTicketCreation({
    workspaceID,
    current: decisionCurrent,
    canCreate:
      state.status === "ready" &&
      state.view.capabilities?.createTicket === true,
    canRetry:
      state.status === "ready" &&
      state.view.capabilities?.retryTicketCreation === true,
    unavailable: state.status === "error",
    client,
    createOperationID,
    onRefresh: relationChanged,
    onUnavailable: relationUnavailable,
    onSessionExpired,
    onCreated: setTicketResult,
  });
  const decisionSupersession = useDecisionSupersession({
    workspaceID,
    current: decisionCurrent,
    canSupersede:
      state.status === "ready" && state.view.capabilities?.supersede === true,
    canRetry:
      state.status === "ready" &&
      state.view.capabilities?.retryDecision === true,
    unavailable: state.status === "error",
    client,
    createOperationID,
    onRefresh: relationChanged,
    onUnavailable: relationUnavailable,
    onSessionExpired,
  });
  const ticketStatusActions = useTicketStatusActions({
    workspaceID,
    ticketID: entityID,
    client,
    createOperationID,
    current:
      state.status === "ready" && "statusRevision" in state.view.current
        ? state.view.current
        : undefined,
    canChange:
      state.status === "ready" &&
      state.view.capabilities?.changeStatus === true,
    unavailable: state.status === "error",
    onRefresh: relationChanged,
    onUnavailable: relationUnavailable,
    onSessionExpired,
  });
  const ticketAssignmentActions = useTicketAssignmentActions({
    workspaceID,
    ticketID: entityID,
    enabled: entityType === "ticket",
    ready: state.status === "ready",
    versionKey: state.status === "ready" ? state.view.current.updatedAt : "",
    unavailable: state.status === "error",
    onRefresh: relationChanged,
    onUnavailable: relationUnavailable,
    onSessionExpired,
    createOperationID,
  });
  const ticketPriorityActions = useTicketPriorityActions({
    workspaceID,
    ticketID: entityID,
    enabled: entityType === "ticket",
    ready: state.status === "ready",
    versionKey: state.status === "ready" ? state.view.current.updatedAt : "",
    unavailable: state.status === "error",
    onRefresh: relationChanged,
    onUnavailable: relationUnavailable,
    onSessionExpired,
    createOperationID,
  });
  const revokeVisibleState = (error: unknown): boolean => {
    if (isExpiredSession(error)) {
      onSessionExpired();
      return true;
    }
    if (error instanceof CollaborationRequestError && error.status === 404) {
      setQuestion("");
      setPendingProposal(null);
      setProposalResult(null);
      setTicketResult(null);
      setState({ status: "error", message: error.userMessage });
      return true;
    }
    return false;
  };

  const proposeDecision = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (entityType !== "thread" || state.status !== "ready") {
      return;
    }
    setSubmitting("proposal");
    setActionError(null);
    setActionNotice(null);
    let operation = pendingProposal;
    if (operation === null || operation.question !== question) {
      try {
        operation = { id: createOperationID(), question };
        setPendingProposal(operation);
      } catch (error) {
        setActionError(collaborationError(error));
        setSubmitting(null);
        return;
      }
    }
    try {
      const result = await client.proposeDecision(workspaceID, entityID, {
        clientOperationID: operation.id,
        question: operation.question,
      });
      setProposalResult(result);
      setQuestion("");
      setPendingProposal(null);
      setActionNotice(
        result.created
          ? "Proposed Decision 已创建，并保留当前 Thread 作为 evidence。"
          : "已确认此前重试创建的同一 Decision。",
      );
    } catch (error) {
      if (!revokeVisibleState(error)) {
        setActionError(collaborationError(error));
      }
    } finally {
      setSubmitting(null);
    }
  };

  if (state.status === "loading") {
    return (
      <main className="collaboration-layout" aria-busy="true">
        <section className="collaboration-state-panel">
          <p className="section-kicker">Canonical collaboration</p>
          <h1>正在读取协作对象</h1>
          <p role="status">
            正在按当前 Session、Project 与 evidence 权限重新解析。
          </p>
        </section>
      </main>
    );
  }

  if (state.status === "error") {
    return (
      <main className="collaboration-layout">
        <section className="collaboration-state-panel">
          <p className="section-kicker">Collaboration unavailable</p>
          <h1>无法读取这个协作对象</h1>
          <p role="alert">{state.message}</p>
          <button
            className="primary-button"
            onClick={() => {
              setState({ status: "loading" });
              setActionError(null);
              setActionNotice(null);
              setReloadKey((key) => key + 1);
            }}
            type="button"
          >
            重新读取
          </button>
        </section>
      </main>
    );
  }

  const { current, relations, timeline } = state.view;
  return (
    <main className="collaboration-layout">
      <ContextSourceNotice
        workspaceID={workspaceID}
        entityType={entityType}
        entityID={entityID}
        onSessionExpired={onSessionExpired}
        onUnavailable={() =>
          setState({ status: "error", message: "当前无法访问这个协作对象。" })
        }
      />
      {userID &&
      isThreadCurrent(current) &&
      current.visibility === "restricted" ? (
        <ManagementPanel
          scope={{ workspace: workspaceID, kind: "thread", id: entityID }}
          userID={userID}
          onSessionExpired={onSessionExpired}
          onChanged={() => setReloadKey((k) => k + 1)}
          onLeft={() => navigate?.("/")}
        />
      ) : null}
      <section
        className="collaboration-hero"
        aria-labelledby="collaboration-title"
      >
        <div>
          <p className="section-kicker">Canonical {current.ref.type}</p>
          <h1 id="collaboration-title">{currentTitle(current)}</h1>
          <p>{currentSummary(current)}</p>
        </div>
        <div className="collaboration-identity">
          <span>{current.ref.type}</span>
          <code>
            entity://{current.ref.type}/{current.ref.id}
          </code>
          <strong>{currentStatus(current)}</strong>
        </div>
        <dl className="collaboration-meta">
          <div>
            <dt>Project</dt>
            <dd>
              <code>{current.project.id}</code>
              {current.ref.type === "decision" ? (
                <a href={projectDecisionsPath(workspaceID, current.project.id)}>
                  查看项目决策
                </a>
              ) : null}
              {entityType === "ticket" ? (
                <a href={projectTicketsPath(workspaceID, current.project.id)}>
                  查看项目工单
                </a>
              ) : null}
            </dd>
          </div>
          <div>
            <dt>Created by</dt>
            <dd>
              <code>{currentActor(current)}</code>
            </dd>
          </div>
          <div>
            <dt>Created</dt>
            <dd>
              <time dateTime={current.createdAt}>
                {formatTimestamp(current.createdAt)}
              </time>
            </dd>
          </div>
          <div>
            <dt>Updated</dt>
            <dd>
              <time dateTime={current.updatedAt}>
                {formatTimestamp(current.updatedAt)}
              </time>
            </dd>
          </div>
        </dl>
      </section>

      <section className="collaboration-columns">
        <div className="collaboration-context">
          {entityType === "ticket" ? (
            <TicketComponents
              workspaceID={workspaceID}
              ticketID={entityID}
              ticketTitle={"title" in current ? current.title : ""}
              onSessionExpired={onSessionExpired}
              onChanged={relationChanged}
              onUnavailable={relationUnavailable}
            />
          ) : null}
          <section
            className="collaboration-panel"
            aria-labelledby="relations-title"
          >
            <div className="channel-section-heading">
              <div>
                <p className="section-kicker">Structured context</p>
                <h2 id="relations-title">来源与后续结果</h2>
              </div>
              <span className="panel-count">{relations.length}</span>
            </div>
            <RelationList workspaceID={workspaceID} relations={relations} />
          </section>
          <section
            className="collaboration-panel"
            aria-labelledby="timeline-title"
          >
            <div className="channel-section-heading">
              <div>
                <p className="section-kicker">Permission-filtered history</p>
                <h2 id="timeline-title">Activity</h2>
              </div>
              <span className="panel-count">{timeline.length}</span>
            </div>
            <TimelineList workspaceID={workspaceID} timeline={timeline} />
          </section>
        </div>

        <aside
          className="collaboration-actions"
          aria-labelledby="actions-title"
        >
          <p className="section-kicker">Authoritative action</p>
          <h2 id="actions-title">推进协作</h2>
          <p>
            {entityType === "ticket"
              ? "状态操作会重新检查当前项目权限；结果未知时可重试原操作。"
              : "写入会由服务端重新检查当前角色与 evidence；网络失败可保持表单后安全重试。"}
          </p>
          {isThreadCurrent(current) ? (
            <form onSubmit={(event) => void proposeDecision(event)}>
              <label>
                <span>Decision 问题</span>
                <textarea
                  disabled={submitting !== null}
                  onChange={(event) => {
                    const value = event.target.value;
                    setQuestion(value);
                    setActionNotice(null);
                    if (pendingProposal?.question !== value) {
                      setPendingProposal(null);
                    }
                  }}
                  placeholder="这个 Thread 需要确认什么决定？"
                  required
                  rows={5}
                  value={question}
                />
              </label>
              <button
                className="primary-button"
                disabled={submitting !== null || question.trim() === ""}
                type="submit"
              >
                {submitting === "proposal"
                  ? "正在提案…"
                  : "创建 Proposed Decision"}
              </button>
            </form>
          ) : isDecisionCurrent(current) ? (
            <>
              {decisionActions}
              {(current.status === "accepted" ||
                current.status === "superseded") && (
                <div className="accepted-decision">
                  <span>
                    {current.status === "superseded"
                      ? "原接受结论 · 已被替代"
                      : "已确认结论"}
                  </span>
                  <strong>{current.outcome}</strong>
                  <p>{current.rationale}</p>
                </div>
              )}
              {decisionTicketCreation}
              {decisionSupersession}
            </>
          ) : (
            <div className="collaboration-complete">
              <strong>Ticket 已进入执行上下文</strong>
              {ticketStatusActions}
              {ticketAssignmentActions}
              {ticketPriorityActions}
              <CreateDocument
                workspaceID={workspaceID}
                ticketID={entityID}
                projectID={current.project.id}
                onSessionExpired={onSessionExpired}
                onRevoked={() =>
                  setState({
                    status: "error",
                    message: "当前权限已变化，请重新读取。",
                  })
                }
              />
            </div>
          )}
          {actionError === null ? null : (
            <p className="inline-error" role="alert">
              {actionError}
            </p>
          )}
          {actionNotice === null ? null : (
            <p className="inline-notice" role="status">
              {actionNotice}
            </p>
          )}
          {proposalResult === null ? null : (
            <CreatedObjectLink
              created={proposalResult.created}
              href={collaborationPagePath(
                workspaceID,
                "decision",
                proposalResult.decision.ref.id,
              )}
              id={proposalResult.decision.ref.id}
              label="Decision"
              title={proposalResult.decision.question}
            />
          )}
          {ticketResult === null ? null : (
            <CreatedObjectLink
              created={ticketResult.created}
              href={collaborationPagePath(
                workspaceID,
                "ticket",
                ticketResult.ticket.ref.id,
              )}
              id={ticketResult.ticket.ref.id}
              label="Ticket"
              title={ticketResult.ticket.title}
            />
          )}
        </aside>
      </section>
    </main>
  );
}

function RelationList({
  workspaceID,
  relations,
}: {
  workspaceID: string;
  relations: readonly CollaborationRelation[];
}) {
  if (relations.length === 0) {
    return (
      <p className="collaboration-empty">当前对象没有可展示的关联对象。</p>
    );
  }
  return (
    <ul className="collaboration-relation-list">
      {relations.map((relation, index) =>
        relation.visibility === "restricted" ? (
          <li className="collaboration-restricted" key={`restricted-${index}`}>
            <span aria-hidden="true">◇</span>
            <div>
              <strong>受限 evidence</strong>
              <p>
                当前权限只能确认存在一项来源，不会暴露类型、ID、标题或时间。
              </p>
            </div>
          </li>
        ) : (
          <li
            key={`${relation.direction}/${relation.relationType}/${relation.target.ref.id}`}
          >
            <span>
              {relation.target.ref.type === "document"
                ? "设计文档"
                : relation.direction === "incoming"
                  ? "后续结果"
                  : "来源"}{" "}
              · {relation.relationType}
            </span>
            <strong>{relation.target.title}</strong>
            {relation.supersession && (
              <div className="supersession-reason">
                <p>{relation.supersession.reason}</p>
                <p>
                  替代人：{relation.supersession.actor.id} ·{" "}
                  {formatTimestamp(relation.supersession.occurredAt)}
                </p>
              </div>
            )}
            <code>
              entity://{relation.target.ref.type}/{relation.target.ref.id}
            </code>
            <RelationLink workspaceID={workspaceID} relation={relation} />
          </li>
        ),
      )}
    </ul>
  );
}

function RelationLink({
  workspaceID,
  relation,
}: {
  workspaceID: string;
  relation: Extract<CollaborationRelation, { visibility: "readable" }>;
}) {
  const target = relation.target.ref;
  const href =
    target.type === "component"
      ? componentPagePath(workspaceID, target.id)
      : target.type === "document"
        ? documentPath(workspaceID, target.id)
        : target.type === "channel"
          ? channelPagePath(workspaceID, target.id)
          : target.type === "thread" ||
              target.type === "decision" ||
              target.type === "ticket"
            ? collaborationPagePath(workspaceID, target.type, target.id)
            : null;
  return href === null ? null : (
    <a href={href}>
      {target.type === "component"
        ? "打开关联组件"
        : target.type === "document"
          ? "打开关联文档"
          : relation.direction === "incoming"
            ? "打开后续对象"
            : "打开来源对象"}
    </a>
  );
}

function TimelineList({
  workspaceID,
  timeline,
}: {
  workspaceID: string;
  timeline: readonly CollaborationTimelineItem[];
}) {
  if (timeline.length === 0) {
    return (
      <p className="collaboration-empty">
        当前尚无已投影 Activity；权威对象与来源关系仍由数据库保存。
      </p>
    );
  }
  return (
    <ol className="collaboration-timeline-list">
      {timeline.map((item) => (
        <li key={item.id}>
          <time dateTime={item.occurredAt}>
            {formatTimestamp(item.occurredAt)}
          </time>
          <strong>
            {item.activityType}
            {item.imported ? " · 导入历史" : ""}
          </strong>
          <span>
            {item.actor.id} ·{" "}
            {"relationState" in item
              ? item.relationState === "active"
                ? "已关联组件"
                : "已解除组件关联"
              : "fromStatus" in item
                ? `${ticketStatusLabels[item.fromStatus]} → ${ticketStatusLabels[item.toStatus]}（版本 ${item.fromRevision} → ${item.toRevision}）`
                : "assignment" in item
                  ? `${item.assignment.fromUserID ?? "未分配"} → ${item.assignment.toUserID ?? "未分配"}（负责人版本 ${item.assignment.fromRevision} → ${item.assignment.toRevision}）`
                  : "priority" in item
                    ? `${ticketPriorityLabels[item.priority.from]} → ${ticketPriorityLabels[item.priority.to]}（优先级版本 ${item.priority.fromRevision} → ${item.priority.toRevision}）`
                    : "supersession" in item
                      ? `${item.supersession.side === "superseded" ? "已被替代" : "替代了旧结论"}（版本 ${item.supersession.fromRevision} → ${item.supersession.toRevision}）`
                      : item.status}
          </span>
          <SubjectSummary subjects={item.subjects} />
          {item.subjects.map((subject) =>
            subject.visibility === "readable" &&
            subject.entity.ref.type === "component" ? (
              <a
                key={subject.entity.ref.id}
                href={componentPagePath(workspaceID, subject.entity.ref.id)}
              >
                打开组件 {subject.entity.title}
              </a>
            ) : null,
          )}
        </li>
      ))}
    </ol>
  );
}

function SubjectSummary({
  subjects,
}: {
  subjects: CollaborationTimelineItem["subjects"];
}) {
  if (subjects.length === 0) {
    return null;
  }
  return (
    <p>
      {subjects
        .map((subject, index) =>
          subject.visibility === "restricted"
            ? `受限对象 ${index + 1}`
            : subject.entity.title,
        )
        .join(" · ")}
    </p>
  );
}

function CreatedObjectLink({
  href,
  label,
  title,
  id,
  created,
}: {
  href: string | null;
  label: string;
  title: string;
  id: string;
  created: boolean;
}) {
  return (
    <div className="created-object" role="status">
      <span>{created ? `${label} 已创建` : `${label} 重试已确认`}</span>
      <strong>{title}</strong>
      <code>{id}</code>
      {href === null ? null : <a href={href}>打开 canonical {label}</a>}
    </div>
  );
}

function currentTitle(current: CollaborationView["current"]): string {
  return isDecisionCurrent(current) ? current.question : current.title;
}

function currentStatus(current: CollaborationView["current"]): string {
  return isThreadCurrent(current)
    ? current.visibility
    : "statusRevision" in current
      ? ticketStatusLabels[current.status]
      : current.status;
}

function currentActor(current: CollaborationView["current"]): string {
  return isDecisionCurrent(current)
    ? current.proposer.id
    : current.createdBy.id;
}

function currentSummary(current: CollaborationView["current"]): string {
  if (isThreadCurrent(current)) {
    return current.originChannel === null
      ? "这是 Project 内的权威讨论上下文，可继续形成 Proposed Decision。"
      : `由 ${current.originChannel.title} 中的一条 Message 发起；页面不复制 Source Message 正文。`;
  }
  if (isDecisionCurrent(current)) {
    return current.status === "proposed"
      ? "这是待人工确认的 Decision 草案；自动摘要不能替代接受动作。"
      : current.status === "superseded"
        ? "这个结论已被替代；原接受事实与已有执行来源保持可追溯。"
        : current.status === "accepted"
          ? "这个 Decision 已由明确用户确认，可继续形成实施 Ticket。"
          : "这个提案已被明确拒绝；理由、决定主体和原始来源保留。";
  }
  return "这是从 Accepted Decision 形成的最小执行对象，来源通过 implements 关系保留。";
}

function isThreadCurrent(
  current: CollaborationView["current"],
): current is ThreadCurrent {
  return current.ref.type === "thread";
}

function isDecisionCurrent(
  current: CollaborationView["current"],
): current is DecisionCurrent {
  return current.ref.type === "decision";
}

function collaborationError(error: unknown): string {
  return error instanceof CollaborationRequestError
    ? error.userMessage
    : "协作服务暂不可用，请稍后重试。";
}

function isExpiredSession(error: unknown): boolean {
  return error instanceof CollaborationRequestError && error.status === 401;
}

function formatTimestamp(value: string): string {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(new Date(value));
}
