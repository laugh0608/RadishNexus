import { csrfTokenFromCookie } from "../auth/api";

export type CollaborationEntityType = "thread" | "decision" | "ticket";
export type ThreadVisibility = "project" | "restricted";
export type DecisionStatus = "proposed" | "accepted";

export interface EntityRef<T extends string = string> {
  type: T;
  id: string;
}

export interface UserActor {
  kind: "user";
  id: string;
}

export interface VisibleEntity<T extends string = string> {
  ref: EntityRef<T>;
  title: string;
}

export interface ThreadCurrent {
  ref: EntityRef<"thread">;
  project: EntityRef<"project">;
  originChannel: VisibleEntity<"channel"> | null;
  title: string;
  visibility: ThreadVisibility;
  createdBy: UserActor;
  createdAt: string;
  updatedAt: string;
}

export interface DecisionCurrent {
  ref: EntityRef<"decision">;
  project: EntityRef<"project">;
  question: string;
  status: DecisionStatus;
  outcome: string | null;
  rationale: string | null;
  proposer: UserActor;
  deciders: readonly UserActor[];
  decidedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface TicketCurrent {
  ref: EntityRef<"ticket">;
  project: EntityRef<"project">;
  title: string;
  status: "open";
  createdBy: UserActor;
  createdAt: string;
  updatedAt: string;
}

export type CollaborationCurrent =
  ThreadCurrent | DecisionCurrent | TicketCurrent;

export type CollaborationRelation =
  | { visibility: "restricted" }
  | {
      visibility: "readable";
      relationType: "started-from" | "derived-from" | "implements";
      target: VisibleEntity;
    };

export type CollaborationSubject =
  | { visibility: "restricted" }
  | { visibility: "readable"; entity: VisibleEntity };

export interface CollaborationTimelineItem {
  id: string;
  activityType: "decision.proposed" | "decision.accepted" | "ticket.created";
  actor: UserActor;
  occurredAt: string;
  status: "proposed" | "accepted" | "open";
  subjects: readonly CollaborationSubject[];
}

export interface CollaborationView<
  T extends CollaborationCurrent = CollaborationCurrent,
> {
  current: T;
  relations: readonly CollaborationRelation[];
  timeline: readonly CollaborationTimelineItem[];
}

export interface ProposeDecisionInput {
  clientOperationID: string;
  question: string;
}

export interface ProposeDecisionOutcome {
  decision: DecisionCurrent;
  sourceThread: EntityRef<"thread">;
  created: boolean;
}

export interface AcceptDecisionInput {
  clientOperationID: string;
  outcome: string;
  rationale: string;
  confirmed: true;
}

export interface CreateTicketInput {
  clientOperationID: string;
  title: string;
}

export interface CreateTicketOutcome {
  ticket: TicketCurrent;
  sourceDecision: EntityRef<"decision">;
  created: boolean;
}

export interface CollaborationClient {
  loadView(
    workspaceID: string,
    entityType: CollaborationEntityType,
    entityID: string,
    signal?: AbortSignal,
  ): Promise<CollaborationView>;
  proposeDecision(
    workspaceID: string,
    threadID: string,
    input: ProposeDecisionInput,
    signal?: AbortSignal,
  ): Promise<ProposeDecisionOutcome>;
  acceptDecision(
    workspaceID: string,
    decisionID: string,
    input: AcceptDecisionInput,
    signal?: AbortSignal,
  ): Promise<DecisionCurrent>;
  createTicket(
    workspaceID: string,
    decisionID: string,
    input: CreateTicketInput,
    signal?: AbortSignal,
  ): Promise<CreateTicketOutcome>;
}

export interface CollaborationLocation {
  workspaceID: string;
  entityType: CollaborationEntityType;
  entityID: string;
}

export class CollaborationRequestError extends Error {
  readonly code?: string;
  readonly status?: number;
  readonly userMessage: string;

  constructor(
    userMessage: string,
    options?: { code?: string; status?: number; cause?: unknown },
  ) {
    super(userMessage, { cause: options?.cause });
    this.name = "CollaborationRequestError";
    this.code = options?.code;
    this.status = options?.status;
    this.userMessage = userMessage;
  }
}

const entityPrefixes: Readonly<Record<string, string>> = {
  project: "prj_",
  channel: "chn_",
  message: "msg_",
  thread: "thr_",
  decision: "dec_",
  ticket: "tkt_",
};

const pageSegments: Readonly<Record<CollaborationEntityType, string>> = {
  thread: "threads",
  decision: "decisions",
  ticket: "tickets",
};

const segmentTypes: Readonly<Record<string, CollaborationEntityType>> = {
  threads: "thread",
  decisions: "decision",
  tickets: "ticket",
};

export const browserCollaborationClient: CollaborationClient = {
  loadView,
  proposeDecision,
  acceptDecision,
  createTicket,
};

export function collaborationLocation(
  pathname: string,
): CollaborationLocation | null {
  const match =
    /^\/workspaces\/([^/]+)\/(threads|decisions|tickets)\/([^/]+)\/?$/u.exec(
      pathname,
    );
  if (match === null) {
    return null;
  }
  try {
    const workspaceID = decodeURIComponent(match[1] ?? "");
    const entityType = segmentTypes[match[2] ?? ""];
    const entityID = decodeURIComponent(match[3] ?? "");
    if (
      entityType === undefined ||
      !validPathID(workspaceID, "wrk_") ||
      !validEntityID(entityType, entityID)
    ) {
      return null;
    }
    return { workspaceID, entityType, entityID };
  } catch {
    return null;
  }
}

export function collaborationPagePath(
  workspaceID: string,
  entityType: CollaborationEntityType,
  entityID: string,
): string | null {
  if (
    !validPathID(workspaceID, "wrk_") ||
    !validEntityID(entityType, entityID)
  ) {
    return null;
  }
  return `/workspaces/${encodeURIComponent(workspaceID)}/${pageSegments[entityType]}/${encodeURIComponent(entityID)}`;
}

export function newCollaborationOperationID(): string {
  if (typeof globalThis.crypto?.randomUUID !== "function") {
    throw new CollaborationRequestError(
      "当前浏览器无法生成安全的操作标识，请更新浏览器后重试。",
    );
  }
  return `web:${globalThis.crypto.randomUUID()}`;
}

async function loadView(
  workspaceID: string,
  entityType: CollaborationEntityType,
  entityID: string,
  signal?: AbortSignal,
): Promise<CollaborationView> {
  requireLocation(workspaceID, entityType, entityID);
  const response = await collaborationFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/${pageSegments[entityType]}/${encodeURIComponent(entityID)}/nexus-view`,
    {
      method: "GET",
      headers: { Accept: "application/json" },
      credentials: "same-origin",
      cache: "no-store",
      signal,
    },
  );
  if (response.status !== 200) {
    throw contractError("协作服务返回了意外的读取状态。", response.status);
  }
  const payload = await responseJSON(response, "协作 Nexus View");
  try {
    return parseCollaborationView(payload, entityType, entityID);
  } catch (error) {
    throw contractError(
      "服务返回的协作对象不符合当前契约，请稍后重试。",
      response.status,
      error,
    );
  }
}

async function proposeDecision(
  workspaceID: string,
  threadID: string,
  input: ProposeDecisionInput,
  signal?: AbortSignal,
): Promise<ProposeDecisionOutcome> {
  requireLocation(workspaceID, "thread", threadID);
  requireOperation(input.clientOperationID, input.question, "Decision 问题");
  const response = await collaborationFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/threads/${encodeURIComponent(threadID)}/decisions`,
    writeRequest(
      {
        client_operation_id: input.clientOperationID,
        question: input.question,
      },
      signal,
    ),
  );
  if (response.status !== 200 && response.status !== 201) {
    throw contractError("Decision 服务返回了意外的创建状态。", response.status);
  }
  const payload = await responseJSON(response, "Decision 提案");
  try {
    const envelope = exactRecord(payload, "response", ["data"]);
    const data = exactRecord(envelope.data, "response.data", [
      "decision",
      "source_thread",
    ]);
    const decision = parseDecisionCurrent(
      data.decision,
      "response.data.decision",
    );
    const sourceThread = parseRef(
      data.source_thread,
      "response.data.source_thread",
      "thread",
    );
    if (sourceThread.id !== threadID) {
      throw new TypeError("created Decision source does not match Thread");
    }
    return { decision, sourceThread, created: response.status === 201 };
  } catch (error) {
    throw contractError(
      "服务返回的 Decision 提案不符合当前契约，请稍后重试。",
      response.status,
      error,
    );
  }
}

async function acceptDecision(
  workspaceID: string,
  decisionID: string,
  input: AcceptDecisionInput,
  signal?: AbortSignal,
): Promise<DecisionCurrent> {
  requireLocation(workspaceID, "decision", decisionID);
  requireOperation(input.clientOperationID, input.outcome, "Decision 结论");
  requireText(input.rationale, "Decision 理由");
  if (input.confirmed !== true) {
    throw new CollaborationRequestError("接受 Decision 前必须明确人工确认。", {
      code: "invalid",
      status: 400,
    });
  }
  const response = await collaborationFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/decisions/${encodeURIComponent(decisionID)}/acceptance`,
    writeRequest(
      {
        client_operation_id: input.clientOperationID,
        outcome: input.outcome,
        rationale: input.rationale,
        confirmed: true,
      },
      signal,
    ),
  );
  if (response.status !== 200) {
    throw contractError("Decision 服务返回了意外的确认状态。", response.status);
  }
  const payload = await responseJSON(response, "Decision acceptance");
  try {
    const envelope = exactRecord(payload, "response", ["data"]);
    const decision = parseDecisionCurrent(envelope.data, "response.data");
    if (decision.ref.id !== decisionID || decision.status !== "accepted") {
      throw new TypeError(
        "accepted Decision does not match requested Decision",
      );
    }
    return decision;
  } catch (error) {
    throw contractError(
      "服务返回的 Decision acceptance 不符合当前契约，请稍后重试。",
      response.status,
      error,
    );
  }
}

async function createTicket(
  workspaceID: string,
  decisionID: string,
  input: CreateTicketInput,
  signal?: AbortSignal,
): Promise<CreateTicketOutcome> {
  requireLocation(workspaceID, "decision", decisionID);
  requireOperation(input.clientOperationID, input.title, "Ticket 标题");
  const response = await collaborationFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/decisions/${encodeURIComponent(decisionID)}/tickets`,
    writeRequest(
      {
        client_operation_id: input.clientOperationID,
        title: input.title,
      },
      signal,
    ),
  );
  if (response.status !== 200 && response.status !== 201) {
    throw contractError("Ticket 服务返回了意外的创建状态。", response.status);
  }
  const payload = await responseJSON(response, "Ticket 创建");
  try {
    const envelope = exactRecord(payload, "response", ["data"]);
    const data = exactRecord(envelope.data, "response.data", [
      "ticket",
      "source_decision",
    ]);
    const ticket = parseTicketCurrent(data.ticket, "response.data.ticket");
    const sourceDecision = parseRef(
      data.source_decision,
      "response.data.source_decision",
      "decision",
    );
    if (sourceDecision.id !== decisionID) {
      throw new TypeError("created Ticket source does not match Decision");
    }
    return { ticket, sourceDecision, created: response.status === 201 };
  } catch (error) {
    throw contractError(
      "服务返回的 Ticket 创建结果不符合当前契约，请稍后重试。",
      response.status,
      error,
    );
  }
}

export function parseCollaborationView(
  payload: unknown,
  expectedType: CollaborationEntityType,
  expectedID: string,
): CollaborationView {
  const envelope = exactRecord(payload, "response", ["data"]);
  const data = exactRecord(envelope.data, "response.data", [
    "current",
    "relations",
    "timeline",
  ]);
  const current = parseCurrent(data.current, expectedType);
  if (current.ref.id !== expectedID) {
    throw new TypeError("Current object does not match requested object");
  }
  const relations = array(data.relations, "response.data.relations").map(
    (relation, index) => parseRelation(relation, index, expectedType),
  );
  const timeline = array(data.timeline, "response.data.timeline").map(
    (item, index) => parseTimelineItem(item, index, expectedType),
  );
  validateViewShape(current, relations, timeline);
  return { current, relations, timeline };
}

function parseCurrent(
  value: unknown,
  expectedType: CollaborationEntityType,
): CollaborationCurrent {
  switch (expectedType) {
    case "thread":
      return parseThreadCurrent(value, "response.data.current");
    case "decision":
      return parseDecisionCurrent(value, "response.data.current");
    case "ticket":
      return parseTicketCurrent(value, "response.data.current");
  }
}

function parseThreadCurrent(value: unknown, path: string): ThreadCurrent {
  const current = exactRecord(value, path, [
    "ref",
    "project",
    "origin_channel",
    "title",
    "visibility",
    "created_by",
    "created_at",
    "updated_at",
  ]);
  const visibility = string(current.visibility, `${path}.visibility`);
  if (visibility !== "project" && visibility !== "restricted") {
    throw new TypeError(`${path}.visibility is invalid`);
  }
  return {
    ref: parseRef(current.ref, `${path}.ref`, "thread"),
    project: parseRef(current.project, `${path}.project`, "project"),
    originChannel:
      current.origin_channel === null
        ? null
        : parseVisibleEntity(
            current.origin_channel,
            `${path}.origin_channel`,
            "channel",
          ),
    title: nonEmptyText(current.title, `${path}.title`),
    visibility,
    createdBy: parseActor(current.created_by, `${path}.created_by`),
    createdAt: timestamp(current.created_at, `${path}.created_at`),
    updatedAt: timestamp(current.updated_at, `${path}.updated_at`),
  };
}

function parseDecisionCurrent(value: unknown, path: string): DecisionCurrent {
  const current = exactRecord(value, path, [
    "ref",
    "project",
    "question",
    "status",
    "outcome",
    "rationale",
    "proposer",
    "deciders",
    "decided_at",
    "created_at",
    "updated_at",
  ]);
  const status = string(current.status, `${path}.status`);
  if (status !== "proposed" && status !== "accepted") {
    throw new TypeError(`${path}.status is invalid`);
  }
  const outcome = nullableText(current.outcome, `${path}.outcome`);
  const rationale = nullableText(current.rationale, `${path}.rationale`);
  const deciders = array(current.deciders, `${path}.deciders`).map(
    (actor, index) => parseActor(actor, `${path}.deciders[${index}]`),
  );
  const decidedAt = nullableTimestamp(current.decided_at, `${path}.decided_at`);
  if (
    (status === "proposed" &&
      (outcome !== null ||
        rationale !== null ||
        deciders.length !== 0 ||
        decidedAt !== null)) ||
    (status === "accepted" &&
      (outcome === null ||
        rationale === null ||
        deciders.length === 0 ||
        decidedAt === null))
  ) {
    throw new TypeError(`${path} acceptance fields do not match status`);
  }
  return {
    ref: parseRef(current.ref, `${path}.ref`, "decision"),
    project: parseRef(current.project, `${path}.project`, "project"),
    question: nonEmptyText(current.question, `${path}.question`),
    status,
    outcome,
    rationale,
    proposer: parseActor(current.proposer, `${path}.proposer`),
    deciders,
    decidedAt,
    createdAt: timestamp(current.created_at, `${path}.created_at`),
    updatedAt: timestamp(current.updated_at, `${path}.updated_at`),
  };
}

function parseTicketCurrent(value: unknown, path: string): TicketCurrent {
  const current = exactRecord(value, path, [
    "ref",
    "project",
    "title",
    "status",
    "created_by",
    "created_at",
    "updated_at",
  ]);
  if (current.status !== "open") {
    throw new TypeError(`${path}.status is invalid`);
  }
  return {
    ref: parseRef(current.ref, `${path}.ref`, "ticket"),
    project: parseRef(current.project, `${path}.project`, "project"),
    title: nonEmptyText(current.title, `${path}.title`),
    status: "open",
    createdBy: parseActor(current.created_by, `${path}.created_by`),
    createdAt: timestamp(current.created_at, `${path}.created_at`),
    updatedAt: timestamp(current.updated_at, `${path}.updated_at`),
  };
}

function parseRelation(
  value: unknown,
  index: number,
  expectedType: CollaborationEntityType,
): CollaborationRelation {
  const path = `response.data.relations[${index}]`;
  const relation = record(value, path);
  if (relation.visibility === "restricted") {
    exactKeys(relation, path, ["visibility"]);
    if (expectedType !== "decision") {
      throw new TypeError(`${path} cannot be restricted for ${expectedType}`);
    }
    return { visibility: "restricted" };
  }
  exactKeys(relation, path, ["visibility", "relation_type", "target"]);
  if (relation.visibility !== "readable") {
    throw new TypeError(`${path}.visibility is invalid`);
  }
  const expectedRelation =
    expectedType === "thread"
      ? "started-from"
      : expectedType === "decision"
        ? "derived-from"
        : "implements";
  const expectedTarget =
    expectedType === "thread"
      ? "message"
      : expectedType === "decision"
        ? "thread"
        : "decision";
  if (relation.relation_type !== expectedRelation) {
    throw new TypeError(`${path}.relation_type is invalid`);
  }
  return {
    visibility: "readable",
    relationType: expectedRelation,
    target: parseVisibleEntity(
      relation.target,
      `${path}.target`,
      expectedTarget,
    ),
  };
}

function parseTimelineItem(
  value: unknown,
  index: number,
  expectedType: CollaborationEntityType,
): CollaborationTimelineItem {
  const path = `response.data.timeline[${index}]`;
  const item = exactRecord(value, path, [
    "id",
    "activity_type",
    "actor",
    "occurred_at",
    "status",
    "subjects",
  ]);
  const activityType = string(item.activity_type, `${path}.activity_type`);
  const status = string(item.status, `${path}.status`);
  const valid =
    expectedType === "decision"
      ? (activityType === "decision.proposed" && status === "proposed") ||
        (activityType === "decision.accepted" && status === "accepted")
      : expectedType === "ticket"
        ? activityType === "ticket.created" && status === "open"
        : false;
  if (!valid) {
    throw new TypeError(`${path} does not match ${expectedType} Timeline`);
  }
  return {
    id: scopedID(item.id, `${path}.id`, "evt_"),
    activityType: activityType as CollaborationTimelineItem["activityType"],
    actor: parseActor(item.actor, `${path}.actor`),
    occurredAt: timestamp(item.occurred_at, `${path}.occurred_at`),
    status: status as CollaborationTimelineItem["status"],
    subjects: array(item.subjects, `${path}.subjects`).map(
      (subject, subjectIndex) =>
        parseSubject(subject, `${path}.subjects[${subjectIndex}]`),
    ),
  };
}

function parseSubject(value: unknown, path: string): CollaborationSubject {
  const subject = record(value, path);
  if (subject.visibility === "restricted") {
    exactKeys(subject, path, ["visibility"]);
    return { visibility: "restricted" };
  }
  exactKeys(subject, path, ["visibility", "entity"]);
  if (subject.visibility !== "readable") {
    throw new TypeError(`${path}.visibility is invalid`);
  }
  return {
    visibility: "readable",
    entity: parseVisibleEntity(subject.entity, `${path}.entity`),
  };
}

function validateViewShape(
  current: CollaborationCurrent,
  relations: readonly CollaborationRelation[],
  timeline: readonly CollaborationTimelineItem[],
): void {
  if (isThreadCurrent(current)) {
    if (timeline.length !== 0) {
      throw new TypeError("Thread Timeline must be empty in this contract");
    }
    if (current.originChannel === null && relations.length !== 0) {
      throw new TypeError("project-origin Thread has an unexpected relation");
    }
    if (
      current.originChannel !== null &&
      (relations.length !== 1 ||
        relations[0]?.visibility !== "readable" ||
        relations[0].relationType !== "started-from" ||
        relations[0].target.title !== "Message")
    ) {
      throw new TypeError("messaging-origin Thread is missing its safe source");
    }
    return;
  }
  if (relations.length !== 1) {
    throw new TypeError(`${current.ref.type} must contain one source relation`);
  }
  if (
    current.ref.type === "decision" &&
    timeline.some(
      (item) =>
        item.activityType !== "decision.proposed" &&
        item.activityType !== "decision.accepted",
    )
  ) {
    throw new TypeError("Decision Timeline contains an unexpected item");
  }
  if (
    current.ref.type === "ticket" &&
    timeline.some((item) => item.activityType !== "ticket.created")
  ) {
    throw new TypeError("Ticket Timeline contains an unexpected item");
  }
}

function isThreadCurrent(
  current: CollaborationCurrent,
): current is ThreadCurrent {
  return current.ref.type === "thread";
}

function parseVisibleEntity<T extends string>(
  value: unknown,
  path: string,
  expectedType: T,
): VisibleEntity<T>;
function parseVisibleEntity(
  value: unknown,
  path: string,
  expectedType?: string,
): VisibleEntity;
function parseVisibleEntity(
  value: unknown,
  path: string,
  expectedType?: string,
): VisibleEntity {
  const entity = exactRecord(value, path, ["ref", "title"]);
  return {
    ref: parseRef(entity.ref, `${path}.ref`, expectedType),
    title: nonEmptyText(entity.title, `${path}.title`),
  };
}

function parseActor(value: unknown, path: string): UserActor {
  const actor = exactRecord(value, path, ["kind", "id"]);
  if (actor.kind !== "user") {
    throw new TypeError(`${path}.kind is invalid`);
  }
  return { kind: "user", id: scopedID(actor.id, `${path}.id`, "usr_") };
}

function parseRef<T extends string>(
  value: unknown,
  path: string,
  expectedType?: T,
): EntityRef<T> {
  const ref = exactRecord(value, path, ["type", "id"]);
  const type = string(ref.type, `${path}.type`);
  if (expectedType !== undefined && type !== expectedType) {
    throw new TypeError(`${path}.type is invalid`);
  }
  const prefix = entityPrefixes[type];
  if (prefix === undefined) {
    throw new TypeError(`${path}.type is unsupported`);
  }
  return {
    type: type as T,
    id: scopedID(ref.id, `${path}.id`, prefix),
  };
}

async function collaborationFetch(
  path: string,
  init: RequestInit,
): Promise<Response> {
  let response: Response;
  try {
    response = await fetch(path, init);
  } catch (error) {
    if (isAbortError(error)) {
      throw error;
    }
    throw new CollaborationRequestError(
      "无法连接到 RadishNexus；保持表单不变后可安全重试。",
      { cause: error },
    );
  }
  if (!response.ok) {
    throw await publicResponseError(response);
  }
  return response;
}

function writeRequest(payload: unknown, signal?: AbortSignal): RequestInit {
  const csrfToken = csrfTokenFromCookie(document.cookie);
  if (csrfToken === null) {
    throw new CollaborationRequestError("安全校验失败，请刷新页面后重试。", {
      code: "csrf_failed",
      status: 403,
    });
  }
  return {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json; charset=utf-8",
      "X-CSRF-Token": csrfToken,
    },
    credentials: "same-origin",
    cache: "no-store",
    body: JSON.stringify(payload),
    signal,
  };
}

async function responseJSON(
  response: Response,
  label: string,
): Promise<unknown> {
  const contentType = response.headers.get("Content-Type") ?? "";
  if (!contentType.toLowerCase().includes("application/json")) {
    throw contractError(
      `${label}服务返回了无法识别的响应，请稍后重试。`,
      response.status,
    );
  }
  try {
    return await response.json();
  } catch (error) {
    throw contractError(
      `${label}服务返回了无法解析的响应，请稍后重试。`,
      response.status,
      error,
    );
  }
}

async function publicResponseError(
  response: Response,
): Promise<CollaborationRequestError> {
  let code: string | undefined;
  try {
    const payload = record(await response.json(), "error response");
    const error = record(payload.error, "error response.error");
    code = string(error.code, "error response.error.code");
  } catch {
    code = undefined;
  }
  return new CollaborationRequestError(
    collaborationErrorMessage(response.status, code),
    { code, status: response.status },
  );
}

function collaborationErrorMessage(status: number, code?: string): string {
  if (code === "unauthenticated" || status === 401) {
    return "当前会话已失效，请重新登录。";
  }
  if (code === "csrf_failed") {
    return "安全校验失败，请刷新页面后重试。";
  }
  if (
    code === "invalid_origin" ||
    code === "secure_transport_required" ||
    code === "invalid_proxy_chain"
  ) {
    return "当前站点的安全入口配置无效，请联系实例管理员。";
  }
  if (code === "forbidden" || status === 403) {
    return "当前角色不能执行该协作动作，或已无法读取全部 evidence。";
  }
  if (code === "not_found" || status === 404) {
    return "该协作对象不存在，或你已没有读取它的权限。";
  }
  if (code === "conflict" || status === 409) {
    return "该操作与当前状态或此前重试内容冲突，请重新读取后确认。";
  }
  if (code === "invalid" || status === 400 || status === 413) {
    return "请求内容不符合当前协作契约，请检查后重试。";
  }
  return "协作服务暂不可用，请稍后重试。";
}

function requireLocation(
  workspaceID: string,
  entityType: CollaborationEntityType,
  entityID: string,
): void {
  if (
    !validPathID(workspaceID, "wrk_") ||
    !validEntityID(entityType, entityID)
  ) {
    throw new CollaborationRequestError("协作对象地址无效，无法继续。", {
      code: "invalid",
      status: 400,
    });
  }
}

function requireOperation(
  operationID: string,
  value: string,
  label: string,
): void {
  if (!validOperationID(operationID)) {
    throw new CollaborationRequestError("操作标识无效，请刷新页面后重试。", {
      code: "invalid",
      status: 400,
    });
  }
  requireText(value, label);
}

function requireText(value: string, label: string): void {
  if (!validText(value)) {
    throw new CollaborationRequestError(`${label}不能为空或包含无效字符。`, {
      code: "invalid",
      status: 400,
    });
  }
}

function validEntityID(type: CollaborationEntityType, id: string): boolean {
  return validPathID(id, entityPrefixes[type] ?? "");
}

function validOperationID(value: string): boolean {
  return (
    value.length >= 1 && value.length <= 128 && /^[\x21-\x7e]+$/u.test(value)
  );
}

function validText(value: string): boolean {
  return value.trim() !== "" && !value.includes("\0") && wellFormed(value);
}

function wellFormed(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (next < 0xdc00 || next > 0xdfff) {
        return false;
      }
      index += 1;
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      return false;
    }
  }
  return true;
}

function validPathID(value: string, prefix: string): boolean {
  if (
    prefix === "" ||
    value.length <= prefix.length ||
    value.length > 128 ||
    !value.startsWith(prefix)
  ) {
    return false;
  }
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (
      codePoint <= 0x20 ||
      codePoint > 0x7f ||
      character === "/" ||
      character === "?" ||
      character === "#"
    ) {
      return false;
    }
  }
  return true;
}

function scopedID(value: unknown, path: string, prefix: string): string {
  const id = string(value, path);
  if (!validPathID(id, prefix)) {
    throw new TypeError(`${path} is invalid`);
  }
  return id;
}

function timestamp(value: unknown, path: string): string {
  const result = string(value, path);
  if (
    !/^\d{4}-\d{2}-\d{2}T.*Z$/u.test(result) ||
    Number.isNaN(Date.parse(result))
  ) {
    throw new TypeError(`${path} is not a UTC RFC 3339 timestamp`);
  }
  return result;
}

function nullableTimestamp(value: unknown, path: string): string | null {
  return value === null ? null : timestamp(value, path);
}

function nullableText(value: unknown, path: string): string | null {
  return value === null ? null : nonEmptyText(value, path);
}

function nonEmptyText(value: unknown, path: string): string {
  const result = string(value, path);
  if (!validText(result)) {
    throw new TypeError(`${path} must be valid non-empty text`);
  }
  return result;
}

function exactRecord(
  value: unknown,
  path: string,
  keys: readonly string[],
): Record<string, unknown> {
  const result = record(value, path);
  exactKeys(result, path, keys);
  return result;
}

function exactKeys(
  value: Record<string, unknown>,
  path: string,
  keys: readonly string[],
): void {
  const actual = Object.keys(value).sort();
  const expected = [...keys].sort();
  if (
    actual.length !== expected.length ||
    actual.some((key, index) => key !== expected[index])
  ) {
    throw new TypeError(`${path} contains unexpected or missing fields`);
  }
}

function record(value: unknown, path: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new TypeError(`${path} must be an object`);
  }
  return value as Record<string, unknown>;
}

function array(value: unknown, path: string): unknown[] {
  if (!Array.isArray(value)) {
    throw new TypeError(`${path} must be an array`);
  }
  return value;
}

function string(value: unknown, path: string): string {
  if (typeof value !== "string") {
    throw new TypeError(`${path} must be a string`);
  }
  return value;
}

function contractError(
  message: string,
  status?: number,
  cause?: unknown,
): CollaborationRequestError {
  return new CollaborationRequestError(message, { status, cause });
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}
