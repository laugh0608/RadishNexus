import { AuthRequestError } from "../auth/api";
import {
  array,
  canonicalRef,
  formatTimestamp,
  parseVisibleEntity,
  record,
  scopedID,
  validPathID,
} from "./api";
import type { CIRunStatus, NexusViewData } from "./model";

export function ciRunPagePath(
  workspaceID: string,
  ciRunID: string,
): string | null {
  return validPathID(workspaceID, "wrk_") && validPathID(ciRunID, "cir_")
    ? `/workspaces/${encodeURIComponent(workspaceID)}/ci-runs/${encodeURIComponent(ciRunID)}`
    : null;
}
export function ciRunLocation(
  path: string,
): { workspaceID: string; ciRunID: string } | null {
  const match = /^\/workspaces\/([^/]+)\/ci-runs\/([^/]+)\/?$/u.exec(path);
  if (!match) return null;
  try {
    const workspaceID = decodeURIComponent(match[1] ?? "");
    const ciRunID = decodeURIComponent(match[2] ?? "");
    return ciRunPagePath(workspaceID, ciRunID)
      ? { workspaceID, ciRunID }
      : null;
  } catch {
    return null;
  }
}
export type CIRunLoader = (
  workspaceID: string,
  ciRunID: string,
  signal?: AbortSignal,
) => Promise<NexusViewData>;
export const loadCIRun: CIRunLoader = async (workspaceID, ciRunID, signal) => {
  if (!ciRunPagePath(workspaceID, ciRunID))
    throw new AuthRequestError("CI Run 地址无效。");
  let response: Response;
  try {
    response = await fetch(
      `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/ci-runs/${encodeURIComponent(ciRunID)}/nexus-view`,
      {
        method: "GET",
        headers: { Accept: "application/json" },
        credentials: "same-origin",
        cache: "no-store",
        signal,
      },
    );
  } catch (error) {
    if (signal?.aborted) throw error;
    throw new AuthRequestError("无法连接服务，请重试读取 CI Run。");
  }
  if (!response.ok)
    throw new AuthRequestError(
      response.status === 401
        ? "会话已失效，请重新登录。"
        : response.status === 404
          ? "CI Run 不可用，请返回工作区查看可访问的内容。"
          : "CI Run 读取失败，请稍后重试。",
      { status: response.status },
    );
  try {
    if (
      !response.headers
        .get("Content-Type")
        ?.toLowerCase()
        .includes("application/json")
    )
      throw new TypeError("expected JSON");
    const data = parseCIRunResponse(await response.json());
    if (data.current.entityRef !== `entity://ci-run/${ciRunID}`)
      throw new TypeError("CI Run scope mismatch");
    return data;
  } catch {
    throw new AuthRequestError("CI Run 响应不符合当前协议，请重试。", {
      status: response.status,
    });
  }
};
function fields(
  value: unknown,
  keys: readonly string[],
): Record<string, unknown> {
  const result = record(value, "CI Run");
  if (
    Object.keys(result).length !== keys.length ||
    keys.some((key) => !(key in result))
  )
    throw new TypeError("unexpected CI Run fields");
  return result;
}
function time(value: unknown): string {
  if (
    typeof value !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/u.test(value) ||
    !Number.isFinite(Date.parse(value))
  )
    throw new TypeError("invalid UTC time");
  // Reject normalized invalid dates; preserve the server's nanosecond precision.
  if (new Date(value).toISOString().slice(0, 19) !== value.slice(0, 19))
    throw new TypeError("invalid calendar time");
  return value;
}
function utcOrder(value: string): string {
  return (
    value.slice(0, 19) +
    "." +
    value.slice(19, -1).replace(".", "").padEnd(9, "0")
  );
}

function component(value: unknown) {
  const v = fields(value, ["ref", "title"]);
  fields(v.ref, ["type", "id"]);
  return parseVisibleEntity(value, "component", "component");
}
const labels: Record<CIRunStatus, string> = {
  succeeded: "构建成功",
  failed: "构建失败",
  canceled: "构建已取消",
};
export function parseCIRunResponse(payload: unknown): NexusViewData {
  const envelope = fields(payload, ["data"]);
  const data = fields(envelope.data, ["current", "relations", "timeline"]);
  const c = fields(data.current, [
    "ref",
    "status",
    "started_at",
    "completed_at",
    "recorded_at",
    "updated_at",
    "component",
  ]);
  const ref = fields(c.ref, ["type", "id"]);
  if (ref.type !== "ci-run") throw new TypeError("invalid CI Run type");
  const id = scopedID(ref.id, "current.ref.id", "cir_");
  if (
    c.status !== "succeeded" &&
    c.status !== "failed" &&
    c.status !== "canceled"
  )
    throw new TypeError("invalid CI Run status");
  const status = c.status;
  const started = c.started_at === null ? null : time(c.started_at),
    completed = time(c.completed_at),
    recorded = time(c.recorded_at),
    updated = time(c.updated_at);
  if (
    updated !== recorded ||
    (started !== null && utcOrder(started) > utcOrder(completed))
  )
    throw new TypeError("inconsistent CI Run times");
  const subject = component(c.component);
  const relations = array(data.relations, "relations"),
    timeline = array(data.timeline, "timeline");
  if (relations.length !== 0 || timeline.length !== 1)
    throw new TypeError("unexpected CI Run context");
  const item = fields(timeline[0], [
    "id",
    "activity_type",
    "actor",
    "occurred_at",
    "status",
    "subjects",
  ]);
  const actor = fields(item.actor, ["kind"]);
  const subjects = array(item.subjects, "subjects");
  if (
    actor.kind !== "plugin" ||
    item.activity_type !== "ci-run.recorded" ||
    item.status !== status ||
    time(item.occurred_at) !== completed ||
    subjects.length !== 1
  )
    throw new TypeError("inconsistent CI Run event");
  const visible = fields(subjects[0], ["visibility", "entity"]);
  const eventSubject = component(visible.entity);
  if (
    visible.visibility !== "readable" ||
    eventSubject.ref.id !== subject.ref.id ||
    eventSubject.title !== subject.title
  )
    throw new TypeError("inconsistent CI Run subject");
  return {
    current: {
      entityType: "ci-run",
      entityRef: `entity://ci-run/${id}`,
      eyebrow: `CI Run · ${subject.title}`,
      status,
      statusLabel: labels[status],
      summary: "记录外部流水线已经完成的构建事实。构建成功不代表已经部署。",
      component: { entityRef: canonicalRef(subject.ref), name: subject.title },
      startedAt: started,
      startedAtLabel: started === null ? "未提供" : formatTimestamp(started),
      completedAt: completed,
      completedAtLabel: formatTimestamp(completed),
      recordedAt: recorded,
      recordedAtLabel: formatTimestamp(recorded),
      updatedAt: updated,
      updatedAtLabel: formatTimestamp(updated),
    },
    relations: [],
    timeline: [
      {
        visibility: "readable",
        id: scopedID(item.id, "timeline.id", "evt_"),
        action: labels[status],
        detail: `${subject.title} 的构建完成事实已记录。`,
        actorLabel: "受控自动化",
        sourceLabel: "ci-run.recorded",
        occurredAt: completed,
        occurredAtLabel: formatTimestamp(completed),
      },
    ],
  };
}
