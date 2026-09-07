import { useEffect, useState, type FormEvent } from "react";
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

interface PendingAcceptance {
  id: string;
  outcome: string;
  rationale: string;
}

interface PendingTicket {
  id: string;
  title: string;
}

export function CollaborationPage({
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
  const [outcome, setOutcome] = useState("");
  const [rationale, setRationale] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [ticketTitle, setTicketTitle] = useState("");
  const [pendingProposal, setPendingProposal] =
    useState<PendingProposal | null>(null);
  const [pendingAcceptance, setPendingAcceptance] =
    useState<PendingAcceptance | null>(null);
  const [pendingTicket, setPendingTicket] = useState<PendingTicket | null>(
    null,
  );
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

  useEffect(() => {
    const controller = new AbortController();
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
            onSessionExpired();
            return;
          }
          setState({ status: "error", message: collaborationError(error) });
        },
      );
    return () => controller.abort();
  }, [client, entityID, entityType, onSessionExpired, reloadKey, workspaceID]);

  const revokeVisibleState = (error: unknown): boolean => {
    if (isExpiredSession(error)) {
      onSessionExpired();
      return true;
    }
    if (error instanceof CollaborationRequestError && error.status === 404) {
      setQuestion("");
      setOutcome("");
      setRationale("");
      setConfirmed(false);
      setTicketTitle("");
      setPendingProposal(null);
      setPendingAcceptance(null);
      setPendingTicket(null);
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

  const acceptDecision = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (entityType !== "decision" || state.status !== "ready" || !confirmed) {
      return;
    }
    setSubmitting("acceptance");
    setActionError(null);
    setActionNotice(null);
    let operation = pendingAcceptance;
    if (
      operation === null ||
      operation.outcome !== outcome ||
      operation.rationale !== rationale
    ) {
      try {
        operation = { id: createOperationID(), outcome, rationale };
        setPendingAcceptance(operation);
      } catch (error) {
        setActionError(collaborationError(error));
        setSubmitting(null);
        return;
      }
    }
    try {
      const accepted = await client.acceptDecision(workspaceID, entityID, {
        clientOperationID: operation.id,
        outcome: operation.outcome,
        rationale: operation.rationale,
        confirmed: true,
      });
      setState((current) =>
        current.status === "ready"
          ? {
              status: "ready",
              view: { ...current.view, current: accepted },
            }
          : current,
      );
      setOutcome("");
      setRationale("");
      setConfirmed(false);
      setPendingAcceptance(null);
      setActionNotice(
        "Decision 已由当前用户明确接受；服务端仍按当前 evidence 权限完成确认。",
      );
    } catch (error) {
      if (!revokeVisibleState(error)) {
        setActionError(collaborationError(error));
      }
    } finally {
      setSubmitting(null);
    }
  };

  const createTicket = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (entityType !== "decision" || state.status !== "ready") {
      return;
    }
    setSubmitting("ticket");
    setActionError(null);
    setActionNotice(null);
    let operation = pendingTicket;
    if (operation === null || operation.title !== ticketTitle) {
      try {
        operation = { id: createOperationID(), title: ticketTitle };
        setPendingTicket(operation);
      } catch (error) {
        setActionError(collaborationError(error));
        setSubmitting(null);
        return;
      }
    }
    try {
      const result = await client.createTicket(workspaceID, entityID, {
        clientOperationID: operation.id,
        title: operation.title,
      });
      setTicketResult(result);
      setTicketTitle("");
      setPendingTicket(null);
      setActionNotice(
        result.created
          ? "Ticket 已创建，并以 implements 关系保留当前 Decision。"
          : "已确认此前重试创建的同一 Ticket。",
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
          <section
            className="collaboration-panel"
            aria-labelledby="relations-title"
          >
            <div className="channel-section-heading">
              <div>
                <p className="section-kicker">Structured context</p>
                <h2 id="relations-title">来源关系</h2>
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
            <TimelineList timeline={timeline} />
          </section>
        </div>

        <aside
          className="collaboration-actions"
          aria-labelledby="actions-title"
        >
          <p className="section-kicker">Authoritative action</p>
          <h2 id="actions-title">推进协作</h2>
          <p>
            写入会由服务端重新检查当前角色与
            evidence；网络失败可保持表单后安全重试。
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
            <DecisionActions
              confirmed={confirmed}
              decision={current}
              outcome={outcome}
              rationale={rationale}
              submitting={submitting}
              ticketTitle={ticketTitle}
              onAccept={acceptDecision}
              onCreateTicket={createTicket}
              onConfirmedChange={setConfirmed}
              onOutcomeChange={(value) => {
                setOutcome(value);
                setActionNotice(null);
                if (pendingAcceptance?.outcome !== value) {
                  setPendingAcceptance(null);
                }
              }}
              onRationaleChange={(value) => {
                setRationale(value);
                setActionNotice(null);
                if (pendingAcceptance?.rationale !== value) {
                  setPendingAcceptance(null);
                }
              }}
              onTicketTitleChange={(value) => {
                setTicketTitle(value);
                setActionNotice(null);
                if (pendingTicket?.title !== value) {
                  setPendingTicket(null);
                }
              }}
            />
          ) : (
            <div className="collaboration-complete">
              <strong>Ticket 已进入执行上下文</strong>
              <p>
                当前切片只读展示 open Ticket；状态流转将在后续工作流合同中建立。
              </p>
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

function DecisionActions({
  decision,
  outcome,
  rationale,
  confirmed,
  ticketTitle,
  submitting,
  onAccept,
  onCreateTicket,
  onOutcomeChange,
  onRationaleChange,
  onConfirmedChange,
  onTicketTitleChange,
}: {
  decision: DecisionCurrent;
  outcome: string;
  rationale: string;
  confirmed: boolean;
  ticketTitle: string;
  submitting: "proposal" | "acceptance" | "ticket" | null;
  onAccept: (event: FormEvent<HTMLFormElement>) => Promise<void>;
  onCreateTicket: (event: FormEvent<HTMLFormElement>) => Promise<void>;
  onOutcomeChange: (value: string) => void;
  onRationaleChange: (value: string) => void;
  onConfirmedChange: (value: boolean) => void;
  onTicketTitleChange: (value: string) => void;
}) {
  if (decision.status === "proposed") {
    return (
      <form onSubmit={(event) => void onAccept(event)}>
        <label>
          <span>明确结论</span>
          <textarea
            disabled={submitting !== null}
            onChange={(event) => onOutcomeChange(event.target.value)}
            required
            rows={4}
            value={outcome}
          />
        </label>
        <label>
          <span>理由</span>
          <textarea
            disabled={submitting !== null}
            onChange={(event) => onRationaleChange(event.target.value)}
            required
            rows={5}
            value={rationale}
          />
        </label>
        <label className="confirmation-control">
          <input
            checked={confirmed}
            disabled={submitting !== null}
            onChange={(event) => onConfirmedChange(event.target.checked)}
            required
            type="checkbox"
          />
          <span>我正在以当前用户身份明确接受这个 Decision。</span>
        </label>
        <button
          className="primary-button"
          disabled={
            submitting !== null ||
            outcome.trim() === "" ||
            rationale.trim() === "" ||
            !confirmed
          }
          type="submit"
        >
          {submitting === "acceptance" ? "正在确认…" : "接受 Decision"}
        </button>
      </form>
    );
  }
  return (
    <>
      <div className="accepted-decision">
        <span>Accepted outcome</span>
        <strong>{decision.outcome}</strong>
        <p>{decision.rationale}</p>
      </div>
      <form onSubmit={(event) => void onCreateTicket(event)}>
        <label>
          <span>Ticket 标题</span>
          <input
            disabled={submitting !== null}
            onChange={(event) => onTicketTitleChange(event.target.value)}
            placeholder="把已接受结论转成可执行工作"
            required
            value={ticketTitle}
          />
        </label>
        <button
          className="primary-button"
          disabled={submitting !== null || ticketTitle.trim() === ""}
          type="submit"
        >
          {submitting === "ticket" ? "正在创建…" : "创建 Ticket"}
        </button>
      </form>
    </>
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
      <p className="collaboration-empty">当前对象没有可展示的来源关系。</p>
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
          <li key={`${relation.relationType}/${relation.target.ref.id}`}>
            <span>{relation.relationType}</span>
            <strong>{relation.target.title}</strong>
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
    target.type === "channel"
      ? channelPagePath(workspaceID, target.id)
      : target.type === "thread" ||
          target.type === "decision" ||
          target.type === "ticket"
        ? collaborationPagePath(workspaceID, target.type, target.id)
        : null;
  return href === null ? null : <a href={href}>打开来源对象</a>;
}

function TimelineList({
  timeline,
}: {
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
          <strong>{item.activityType}</strong>
          <span>
            {item.actor.id} · {item.status}
          </span>
          <SubjectSummary subjects={item.subjects} />
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
  return isThreadCurrent(current) ? current.visibility : current.status;
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
      : "这个 Decision 已由明确用户确认，可继续形成实施 Ticket。";
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
