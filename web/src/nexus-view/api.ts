import type {
  DeploymentStatus,
  NexusRelation,
  NexusTimelineItem,
  NexusViewData,
} from "./model";

interface APIEntityRef {
  type: string;
  id: string;
}

interface APIVisibleEntity {
  ref: APIEntityRef;
  title: string;
}

interface APIDeploymentCurrent {
  ref: APIEntityRef;
  status: DeploymentStatus;
  startedAt: string | null;
  completedAt: string;
  recordedAt: string;
  environment: APIVisibleEntity;
  ciRun: APIVisibleEntity;
}

type APIDeploymentRelation =
  | { visibility: "restricted" }
  | {
      visibility: "readable";
      relationType: "deploys";
      target: APIVisibleEntity;
    };

type APIDeploymentSubject =
  | { visibility: "restricted" }
  | { visibility: "readable"; entity: APIVisibleEntity };

interface APIDeploymentTimelineItem {
  id: string;
  activityType: "deployment.recorded";
  actor: { kind: "user"; id: string };
  occurredAt: string;
  status: DeploymentStatus;
  subjects: readonly APIDeploymentSubject[];
}

export interface DeploymentNexusViewAPIData {
  current: APIDeploymentCurrent;
  relations: readonly APIDeploymentRelation[];
  timeline: readonly APIDeploymentTimelineItem[];
}

export interface DeploymentNexusViewLocation {
  workspaceID: string;
  deploymentID: string;
}

export type DeploymentNexusViewLoader = (
  workspaceID: string,
  deploymentID: string,
  signal?: AbortSignal,
) => Promise<NexusViewData>;

export class DeploymentNexusViewLoadError extends Error {
  readonly status?: number;
  readonly userMessage: string;

  constructor(userMessage: string, status?: number, options?: ErrorOptions) {
    super(userMessage, options);
    this.name = "DeploymentNexusViewLoadError";
    this.status = status;
    this.userMessage = userMessage;
  }
}

const statusPresentation: Record<
  DeploymentStatus,
  { label: string; action: string; summary: string }
> = {
  succeeded: {
    label: "部署成功",
    action: "已记录为成功",
    summary:
      "目标 Environment 的完成态部署事实已被明确记录为成功；该记录保留来源 CI Run，但不表示 RadishNexus 执行了外部部署。",
  },
  failed: {
    label: "部署失败",
    action: "已记录为失败",
    summary:
      "目标 Environment 的部署结果已被明确记录为失败；失败事实不会改写来源 CI Run，也不会被表现为部署成功。",
  },
  canceled: {
    label: "部署已取消",
    action: "已记录为取消",
    summary:
      "目标 Environment 的部署结果已被明确记录为取消；该事实不会改写来源 CI Run，也不会被表现为部署成功。",
  },
};

const entityIDPrefixes: Readonly<Record<string, string>> = {
  deployment: "dpl_",
  environment: "env_",
  "ci-run": "cir_",
};

export function deploymentNexusViewLocation(
  pathname: string,
): DeploymentNexusViewLocation | null {
  const match = /^\/workspaces\/([^/]+)\/deployments\/([^/]+)\/?$/u.exec(
    pathname,
  );
  if (match === null) {
    return null;
  }

  try {
    const workspaceID = decodeURIComponent(match[1] ?? "");
    const deploymentID = decodeURIComponent(match[2] ?? "");
    if (
      !validPathID(workspaceID, "wrk_") ||
      !validPathID(deploymentID, "dpl_")
    ) {
      return null;
    }
    return { workspaceID, deploymentID };
  } catch {
    return null;
  }
}

export function deploymentNexusViewPagePath(
  workspaceID: string,
  deploymentID: string,
): string | null {
  if (!validPathID(workspaceID, "wrk_") || !validPathID(deploymentID, "dpl_")) {
    return null;
  }
  return `/workspaces/${encodeURIComponent(workspaceID)}/deployments/${encodeURIComponent(deploymentID)}`;
}

export async function loadDeploymentNexusViewData(
  workspaceID: string,
  deploymentID: string,
  signal?: AbortSignal,
): Promise<NexusViewData> {
  if (!validPathID(workspaceID, "wrk_") || !validPathID(deploymentID, "dpl_")) {
    throw new DeploymentNexusViewLoadError(
      "Deployment 地址无效，无法读取上下文。",
    );
  }

  let response: Response;
  try {
    response = await fetch(
      `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/deployments/${encodeURIComponent(deploymentID)}/nexus-view`,
      {
        method: "GET",
        headers: { Accept: "application/json" },
        credentials: "same-origin",
        cache: "no-store",
        signal,
      },
    );
  } catch (error) {
    if (isAbortError(error)) {
      throw error;
    }
    throw new DeploymentNexusViewLoadError(
      "无法连接到 RadishNexus，请检查网络后重试。",
      undefined,
      { cause: error },
    );
  }

  if (!response.ok) {
    throw new DeploymentNexusViewLoadError(
      response.status === 401
        ? "当前会话已失效，请重新登录后再试。"
        : response.status === 404
          ? "该 Deployment 不存在，或你没有读取它的权限。"
          : "服务没有返回可用的 Nexus View，请稍后重试。",
      response.status,
    );
  }

  const contentType = response.headers.get("Content-Type") ?? "";
  if (!contentType.toLowerCase().includes("application/json")) {
    throw new DeploymentNexusViewLoadError(
      "服务返回了无法识别的 Nexus View，请稍后重试。",
      response.status,
    );
  }

  let payload: unknown;
  try {
    payload = await response.json();
  } catch (error) {
    throw new DeploymentNexusViewLoadError(
      "服务返回了无法解析的 Nexus View，请稍后重试。",
      response.status,
      { cause: error },
    );
  }

  try {
    const data = parseDeploymentNexusViewResponse(payload);
    if (data.current.ref.id !== deploymentID) {
      throw new TypeError(
        "Current Deployment does not match the requested Deployment",
      );
    }
    return toDeploymentNexusViewData(data);
  } catch (error) {
    throw new DeploymentNexusViewLoadError(
      "服务返回的 Nexus View 不符合当前契约，请稍后重试。",
      response.status,
      { cause: error },
    );
  }
}

export function parseDeploymentNexusViewResponse(
  payload: unknown,
): DeploymentNexusViewAPIData {
  const envelope = record(payload, "response");
  const data = record(envelope.data, "response.data");
  const current = parseCurrent(data.current);
  const relations = array(data.relations, "response.data.relations").map(
    parseRelation,
  );
  const timeline = array(data.timeline, "response.data.timeline").map(
    parseTimelineItem,
  );

  if (relations.length !== 1 || timeline.length !== 1) {
    throw new TypeError(
      "Deployment Nexus View must contain one Relation and one Timeline item",
    );
  }

  for (const relation of relations) {
    if (
      relation.visibility === "readable" &&
      (relation.target.ref.id !== current.ciRun.ref.id ||
        relation.target.title !== current.ciRun.title)
    ) {
      throw new TypeError("readable relation does not match Current CI Run");
    }
  }
  for (const item of timeline) {
    if (item.status !== current.status || item.subjects.length !== 2) {
      throw new TypeError("Timeline does not match Current");
    }
    const expectedSubjects = [current.environment, current.ciRun];
    for (const [index, subject] of item.subjects.entries()) {
      const expected = expectedSubjects[index];
      if (
        subject.visibility === "readable" &&
        (expected === undefined ||
          subject.entity.ref.type !== expected.ref.type ||
          subject.entity.ref.id !== expected.ref.id ||
          subject.entity.title !== expected.title)
      ) {
        throw new TypeError("Timeline subject does not match Current");
      }
    }
  }

  return { current, relations, timeline };
}

export function toDeploymentNexusViewData(
  data: DeploymentNexusViewAPIData,
): NexusViewData {
  const current = data.current;
  const presentation = statusPresentation[current.status];
  const relations: NexusRelation[] = data.relations.map((relation) => {
    if (relation.visibility === "restricted") {
      return { visibility: "restricted" };
    }
    return {
      visibility: "readable",
      entityRef: canonicalRef(relation.target.ref),
      entityType: "ci-run",
      entityTypeLabel: "CI Run",
      relationType: relation.relationType,
      relationLabel: "来源构建",
      title: relation.target.title,
      summary:
        "这是本次 Deployment 的明确来源；CI Run 完成态本身不会自动产生 Deployment。",
    };
  });
  const timeline: NexusTimelineItem[] = data.timeline.map((item) => ({
    visibility: "readable",
    id: item.id,
    action: `${current.environment.title} Deployment ${statusPresentation[item.status].action}`,
    detail: timelineDetail(item.subjects),
    actorLabel: `成员 ${item.actor.id}`,
    sourceLabel: item.activityType,
    occurredAt: item.occurredAt,
    occurredAtLabel: formatTimestamp(item.occurredAt),
  }));

  return {
    current: {
      entityRef: canonicalRef(current.ref),
      entityType: "deployment",
      eyebrow: `Deployment · ${current.environment.title}`,
      status: current.status,
      statusLabel: presentation.label,
      summary: presentation.summary,
      environment: {
        entityRef: canonicalRef(current.environment.ref),
        name: current.environment.title,
      },
      ciRun: {
        entityRef: canonicalRef(current.ciRun.ref),
        name: current.ciRun.title,
      },
      startedAt: current.startedAt,
      startedAtLabel:
        current.startedAt === null
          ? "未记录"
          : formatTimestamp(current.startedAt),
      completedAt: current.completedAt,
      completedAtLabel: formatTimestamp(current.completedAt),
      recordedAt: current.recordedAt,
      recordedAtLabel: formatTimestamp(current.recordedAt),
    },
    relations,
    timeline,
  };
}

function parseCurrent(value: unknown): APIDeploymentCurrent {
  const current = record(value, "current");
  return {
    ref: parseRef(current.ref, "current.ref", "deployment"),
    status: deploymentStatus(current.status, "current.status"),
    startedAt: nullableTimestamp(current.started_at, "current.started_at"),
    completedAt: timestamp(current.completed_at, "current.completed_at"),
    recordedAt: timestamp(current.recorded_at, "current.recorded_at"),
    environment: parseVisibleEntity(
      current.environment,
      "current.environment",
      "environment",
    ),
    ciRun: parseVisibleEntity(current.ci_run, "current.ci_run", "ci-run"),
  };
}

function parseRelation(value: unknown, index: number): APIDeploymentRelation {
  const path = `relations[${index}]`;
  const relation = record(value, path);
  const visibility = string(relation.visibility, `${path}.visibility`);
  if (visibility === "restricted") {
    return { visibility };
  }
  if (visibility !== "readable") {
    throw new TypeError(`${path}.visibility is invalid`);
  }
  if (relation.relation_type !== "deploys") {
    throw new TypeError(`${path}.relation_type is invalid`);
  }
  return {
    visibility,
    relationType: "deploys",
    target: parseVisibleEntity(relation.target, `${path}.target`, "ci-run"),
  };
}

function parseTimelineItem(
  value: unknown,
  index: number,
): APIDeploymentTimelineItem {
  const path = `timeline[${index}]`;
  const item = record(value, path);
  if (item.activity_type !== "deployment.recorded") {
    throw new TypeError(`${path}.activity_type is invalid`);
  }
  const actor = record(item.actor, `${path}.actor`);
  if (actor.kind !== "user") {
    throw new TypeError(`${path}.actor.kind is invalid`);
  }
  const actorID = scopedID(actor.id, `${path}.actor.id`, "usr_");
  return {
    id: scopedID(item.id, `${path}.id`, "evt_"),
    activityType: "deployment.recorded",
    actor: { kind: "user", id: actorID },
    occurredAt: timestamp(item.occurred_at, `${path}.occurred_at`),
    status: deploymentStatus(item.status, `${path}.status`),
    subjects: array(item.subjects, `${path}.subjects`).map(parseSubject),
  };
}

function parseSubject(value: unknown, index: number): APIDeploymentSubject {
  const path = `subjects[${index}]`;
  const subject = record(value, path);
  const visibility = string(subject.visibility, `${path}.visibility`);
  if (visibility === "restricted") {
    return { visibility };
  }
  if (visibility !== "readable") {
    throw new TypeError(`${path}.visibility is invalid`);
  }
  return {
    visibility,
    entity: parseVisibleEntity(subject.entity, `${path}.entity`),
  };
}

function parseVisibleEntity(
  value: unknown,
  path: string,
  expectedType?: string,
): APIVisibleEntity {
  const entity = record(value, path);
  return {
    ref: parseRef(entity.ref, `${path}.ref`, expectedType),
    title: nonEmptyString(entity.title, `${path}.title`),
  };
}

function parseRef(
  value: unknown,
  path: string,
  expectedType?: string,
): APIEntityRef {
  const ref = record(value, path);
  const type = string(ref.type, `${path}.type`);
  if (expectedType !== undefined && type !== expectedType) {
    throw new TypeError(`${path}.type is invalid`);
  }
  const prefix = entityIDPrefixes[type];
  if (prefix === undefined) {
    throw new TypeError(`${path}.type is unsupported`);
  }
  return { type, id: scopedID(ref.id, `${path}.id`, prefix) };
}

function timelineDetail(subjects: readonly APIDeploymentSubject[]): string {
  const visibleTitles = subjects.flatMap((subject) =>
    subject.visibility === "readable" ? [subject.entity.title] : [],
  );
  if (visibleTitles.length === subjects.length && visibleTitles.length > 0) {
    return `${visibleTitles.join(" 与 ")} 已作为权限过滤后的上下文进入统一时间线。`;
  }
  return "这项 Deployment 事实已按当前权限投影到统一时间线；受限对象不会向客户端暴露身份线索。";
}

function canonicalRef(ref: APIEntityRef): string {
  return `entity://${ref.type}/${ref.id}`;
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

function validPathID(value: string, prefix: string): boolean {
  if (
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

function deploymentStatus(value: unknown, path: string): DeploymentStatus {
  if (value !== "succeeded" && value !== "failed" && value !== "canceled") {
    throw new TypeError(`${path} is invalid`);
  }
  return value;
}

function nullableTimestamp(value: unknown, path: string): string | null {
  return value === null ? null : timestamp(value, path);
}

function timestamp(value: unknown, path: string): string {
  const result = string(value, path);
  if (
    !/^\d{4}-\d{2}-\d{2}T/u.test(result) ||
    Number.isNaN(Date.parse(result))
  ) {
    throw new TypeError(`${path} is not an RFC 3339 timestamp`);
  }
  return result;
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

function nonEmptyString(value: unknown, path: string): string {
  const result = string(value, path);
  if (result.trim() === "") {
    throw new TypeError(`${path} must not be empty`);
  }
  return result;
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}
