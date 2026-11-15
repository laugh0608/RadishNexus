import { AuthRequestError } from "../auth/api";
import { object, requireID } from "../workspace/api";
import {
  parsePage,
  query,
  request,
  text,
} from "../workspace/configuration-api";
import { parseTicketStatusRevision } from "./ticket-status";

export const ticketTypeLabels = {
  unset: "未分类",
  requirement: "需求",
  task: "任务",
  defect: "缺陷",
};
export type TicketType = keyof typeof ticketTypeLabels;
export interface ClassificationInput {
  ticket_type: TicketType;
  label_ids: string[];
}
export const emptyClassification: ClassificationInput = {
  ticket_type: "unset",
  label_ids: [],
};

export function initialClassification(value: ClassificationInput) {
  return value.ticket_type === "unset" && value.label_ids.length === 0
    ? undefined
    : value;
}

export function parseTicketType(value: unknown): TicketType {
  if (typeof value !== "string" || !Object.hasOwn(ticketTypeLabels, value))
    throw new AuthRequestError("工单类型无效。");
  return value as TicketType;
}

function parseLabel(value: unknown, directory = false) {
  const row = object(
    value,
    directory
      ? ["ref", "name", "status", "revision"]
      : ["ref", "name", "status"],
  );
  const ref = object(row.ref, ["type", "id"]);
  const name = text(row.name);
  if (
    ref.type !== "ticket-label" ||
    (row.status !== "active" && row.status !== "archived") ||
    name.trim() !== name ||
    [...name].length > 40 ||
    /[\p{Cc}\u2028\u2029]/u.test(name)
  )
    throw new AuthRequestError("工单标签响应无效。");
  return { id: requireID(ref.id, "lbl_"), name, status: row.status };
}

function parseEntry(value: unknown) {
  const row = object(value, ["ref", "name", "status", "revision"]);
  return {
    ...parseLabel(row, true),
    revision: parseTicketStatusRevision(row.revision),
  };
}

export function parseClassification(value: unknown) {
  const row = object(value, ["ticket_type", "labels"]);
  if (!Array.isArray(row.labels))
    throw new AuthRequestError("工单标签响应无效。");
  const labels = row.labels.map((label) => parseLabel(label));
  if (
    labels.some(
      (label, index) => index > 0 && label.id <= labels[index - 1]!.id,
    )
  )
    throw new AuthRequestError("工单标签响应无效。");
  return { ticketType: parseTicketType(row.ticket_type), labels };
}

export type ClassificationSummary = ReturnType<typeof parseClassification>;
export type TicketLabel = ClassificationSummary["labels"][number];
export type LabelEntry = ReturnType<typeof parseEntry>;

function directoryPath(project: string) {
  return `/projects/${encodeURIComponent(requireID(project, "prj_"))}/ticket-labels`;
}

function classificationPath(ticket: string) {
  return `/tickets/${encodeURIComponent(requireID(ticket, "tkt_"))}/classification`;
}

export const ticketClassificationClient = {
  async read(workspace: string, ticket: string, signal: AbortSignal) {
    const row = object(
      await request(
        workspace,
        classificationPath(ticket),
        "GET",
        undefined,
        signal,
      ),
      ["ticket_type", "labels", "classification_revision", "can_set"],
    );
    const revision = parseTicketStatusRevision(row.classification_revision);
    if (typeof row.can_set !== "boolean")
      throw new AuthRequestError("工单分类响应无效。");
    return {
      ...parseClassification({
        ticket_type: row.ticket_type,
        labels: row.labels,
      }),
      revision,
      canSet: row.can_set,
    };
  },
  async set(
    workspace: string,
    ticket: string,
    input: ClassificationInput & {
      base_classification_revision: number;
      client_operation_id: string;
      confirmed: true;
    },
    signal: AbortSignal,
  ) {
    const row = object(
      await request(
        workspace,
        classificationPath(ticket),
        "POST",
        { ...input },
        signal,
      ),
      ["classification_revision", "applied"],
    );
    if (
      row.applied !== true ||
      parseTicketStatusRevision(row.classification_revision) !==
        input.base_classification_revision + 1
    )
      throw new AuthRequestError("工单分类写入结果尚未确认。");
  },
  async labels(
    workspace: string,
    project: string,
    status: "active" | "all",
    after: string | undefined,
    signal: AbortSignal,
  ) {
    const data = object(
      await request(
        workspace,
        `${directoryPath(project)}${query(after)}&status=${status}`,
        "GET",
        undefined,
        signal,
      ),
      ["items", "next_cursor", "can_manage"],
    );
    if (typeof data.can_manage !== "boolean")
      throw new AuthRequestError("标签目录响应无效。");
    const page = parsePage(
      { items: data.items, next_cursor: data.next_cursor },
      parseEntry,
      after,
    );
    if (
      status === "active" &&
      page.items.some((label) => label.status !== "active")
    )
      throw new AuthRequestError("标签目录响应无效。");
    return { ...page, canManage: data.can_manage };
  },
  async writeLabel(
    workspace: string,
    project: string,
    change:
      | { action: "create"; name: string }
      | { action: "rename" | "archive"; label: LabelEntry; name: string },
    operation: string,
    signal: AbortSignal,
  ) {
    const base = directoryPath(project);
    const path =
      change.action === "create"
        ? base
        : `${base}/${encodeURIComponent(requireID(change.label.id, "lbl_"))}/${change.action}`;
    const row = parseEntry(
      await request(
        workspace,
        path,
        "POST",
        {
          client_operation_id: operation,
          confirmed: true,
          ...(change.action === "create"
            ? {}
            : { base_revision: change.label.revision }),
          ...(change.action === "archive" ? {} : { name: change.name }),
        },
        signal,
      ),
    );
    if (
      row.status !== (change.action === "archive" ? "archived" : "active") ||
      (change.action !== "create" && row.id !== change.label.id) ||
      row.revision !==
        (change.action === "create" ? 1 : change.label.revision + 1) ||
      row.name !==
        (change.action === "archive" ? change.label.name : change.name.trim())
    )
      throw new AuthRequestError("标签写入结果尚未确认。");
  },
};
