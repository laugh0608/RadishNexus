import { csrfTokenFromCookie } from "../auth/api";

export const format = "nexus-markdown-v1";
export type Ref = { type: string; id: string };
export type Node = {
  kind: string;
  text?: string;
  url?: string;
  level?: number;
  ordered?: boolean;
  start?: number;
  children?: Node[];
};
export type View = { format: "nexus-markdown-view-v1"; nodes: Node[] };
export type Revision = {
  ref: Ref;
  project: Ref;
  revision: number;
  title: string;
  body_markdown: string;
  format_version: string;
  created_by: { kind: "user"; id: string };
  created_at: string;
  restored_from_revision: number | null;
  view: View | null;
  rendering_failure: { code: string; offset: number } | null;
};
export type Relation =
  | { visibility: "restricted" }
  | {
      visibility: "readable";
      direction: "incoming";
      relation_type: "relates-to";
      target: { ref: Ref; title: string };
    };
export type Timeline = {
  id: string;
  activity_type: string;
  actor: { kind: "user"; id: string };
  occurred_at: string;
  revision: number;
  restored_from_revision: number | null;
  subjects: (
    | { visibility: "restricted" }
    | { visibility: "readable"; entity: { ref: Ref; title: string } }
  )[];
};
export type DocumentView = {
  current: Revision;
  relations: Relation[];
  timeline: Timeline[];
};
export type ListItem = {
  ref: Ref;
  title: string;
  revision: number;
  created_at: string;
  updated_at: string;
};
export type HistoryItem = Pick<
  Revision,
  "revision" | "title" | "created_by" | "created_at" | "restored_from_revision"
>;
export type Page<T> = { items: T[]; next_cursor: string | null };
export type WriteInput = {
  client_operation_id: string;
  title?: string;
  body_markdown?: string;
  format_version?: string;
  base_revision?: number;
  restore_revision?: number;
  confirmed?: true;
};
export type Result = { ref: Ref; applied_revision: number };
export class DocumentError extends Error {
  constructor(
    message: string,
    readonly status?: number,
    readonly currentRevision?: number,
  ) {
    super(message);
  }
}
export function documentPath(workspace: string, id: string) {
  return `/workspaces/${encodeURIComponent(workspace)}/documents/${encodeURIComponent(id)}`;
}
export function documentsPath(workspace: string, id: string) {
  return `/workspaces/${encodeURIComponent(workspace)}/projects/${encodeURIComponent(id)}/documents`;
}
export function documentLocation(path: string) {
  const m =
    /^\/workspaces\/(wrk_[A-Za-z0-9_-]+)\/(?:documents\/(doc_[A-Za-z0-9_-]+)|projects\/(prj_[A-Za-z0-9_-]+)\/documents)\/?$/u.exec(
      path,
    );
  return m
    ? { workspaceID: m[1]!, documentID: m[2] ?? null, projectID: m[3] ?? null }
    : null;
}
function record(v: unknown): Record<string, unknown> {
  if (!v || typeof v !== "object" || Array.isArray(v))
    throw new TypeError("无效响应对象");
  return v as Record<string, unknown>;
}
function keys(
  v: Record<string, unknown>,
  required: string[],
  optional: string[] = [],
) {
  if (
    required.some((k) => !(k in v)) ||
    Object.keys(v).some((k) => !required.includes(k) && !optional.includes(k))
  )
    throw new TypeError("响应字段不匹配");
}
function str(v: unknown): string {
  if (typeof v !== "string") throw new TypeError("无效文本");
  return v;
}
function integer(v: unknown, min = 1, max = 2147483647): number {
  if (typeof v !== "number" || !Number.isInteger(v) || v < min || v > max)
    throw new TypeError("无效整数");
  return v;
}
function list(v: unknown): unknown[] {
  if (!Array.isArray(v)) throw new TypeError("无效列表");
  return v;
}
function ref(v: unknown, type: string): Ref {
  const o = record(v);
  keys(o, ["type", "id"]);
  const id = str(o.id);
  const prefix = (
    { document: "doc_", project: "prj_", ticket: "tkt_" } as Record<
      string,
      string
    >
  )[type];
  if (
    o.type !== type ||
    !prefix ||
    !id.startsWith(prefix) ||
    !/^[-_A-Za-z0-9]+$/u.test(id)
  )
    throw new TypeError("无效引用");
  return { type, id };
}
function actor(v: unknown): { kind: "user"; id: string } {
  const o = record(v);
  keys(o, ["kind", "id"]);
  if (o.kind !== "user" || !/^usr_[-_A-Za-z0-9]+$/u.test(str(o.id)))
    throw new TypeError("无效操作者");
  return { kind: "user", id: str(o.id) };
}
function time(v: unknown) {
  const s = str(v);
  if (!/^\d{4}-\d\d-\d\dT/u.test(s) || Number.isNaN(Date.parse(s)))
    throw new TypeError("无效时间");
  return s;
}
function nullableRevision(v: unknown) {
  return v === null ? null : integer(v);
}
export function validURL(value: string): boolean {
  try {
    const u = new URL(value);
    return (
      /^https?:\/\//u.test(value) &&
      !!u.hostname &&
      !u.username &&
      !u.password &&
      !/[\s\p{Cc}\\]/u.test(value) &&
      !/[\p{Cc}\\]/u.test(decodeURIComponent(value))
    );
  } catch {
    return false;
  }
}
export function parseView(value: unknown): View {
  const v = record(value);
  keys(v, ["format", "nodes"]);
  if (v.format !== "nexus-markdown-view-v1")
    throw new TypeError("未知展示格式");
  let count = 0;
  function node(value: unknown, depth: number): Node {
    if (++count > 20000 || depth > 32) throw new TypeError("展示超限");
    const n = record(value),
      kind = str(n.kind);
    const container = [
      "paragraph",
      "group",
      "heading",
      "quote",
      "list",
      "item",
      "emphasis",
      "strong",
      "link",
    ].includes(kind);
    const textual = ["text", "code", "code_block"].includes(kind);
    if (!container && !textual && kind !== "break" && kind !== "rule")
      throw new TypeError("未知展示节点");
    keys(
      n,
      ["kind"],
      [
        ...(container ? ["children"] : []),
        ...(textual ? ["text"] : []),
        ...(kind === "link" ? ["url"] : []),
        ...(kind === "heading" ? ["level"] : []),
        ...(kind === "list" ? ["ordered", "start"] : []),
      ],
    );
    const result: Node = { kind };
    if (textual) result.text = n.text === undefined ? "" : str(n.text);
    if (container)
      result.children =
        n.children === undefined
          ? []
          : list(n.children).map((v) => node(v, depth + 1));
    if (kind === "heading") result.level = integer(n.level, 1, 6);
    if (kind === "link") {
      result.url = str(n.url);
      if (!validURL(result.url)) throw new TypeError("链接不安全");
    }
    if (kind === "list") {
      if (n.ordered !== undefined && n.ordered !== true)
        throw new TypeError("列表类型无效");
      result.ordered = n.ordered === true;
      if (result.ordered)
        result.start =
          n.start === undefined ? 0 : integer(n.start, 0, 999999999);
      else if (n.start !== undefined) throw new TypeError("无序列表起点无效");
    }
    return result;
  }
  return {
    format: "nexus-markdown-view-v1",
    nodes: list(v.nodes).map((v) => node(v, 1)),
  };
}
function parseRevision(value: unknown): Revision {
  const v = record(value);
  keys(v, [
    "ref",
    "project",
    "revision",
    "title",
    "body_markdown",
    "format_version",
    "created_by",
    "created_at",
    "restored_from_revision",
    "view",
    "rendering_failure",
  ]);
  const revision = integer(v.revision),
    restored = nullableRevision(v.restored_from_revision);
  if (restored !== null && restored >= revision)
    throw new TypeError("恢复来源无效");
  let failure: Revision["rendering_failure"] = null;
  if (v.rendering_failure !== null) {
    const f = record(v.rendering_failure);
    keys(f, ["code", "offset"]);
    failure = { code: str(f.code), offset: integer(f.offset, 0) };
  }
  if ((v.view === null) === (failure === null))
    throw new TypeError("展示状态无效");
  return {
    ref: ref(v.ref, "document"),
    project: ref(v.project, "project"),
    revision,
    title: str(v.title),
    body_markdown: str(v.body_markdown),
    format_version: str(v.format_version),
    created_by: actor(v.created_by),
    created_at: time(v.created_at),
    restored_from_revision: restored,
    view: v.view === null ? null : parseView(v.view),
    rendering_failure: failure,
  };
}
function visible(value: unknown) {
  const v = record(value);
  keys(v, ["ref", "title"]);
  return { ref: ref(v.ref, "ticket"), title: str(v.title) };
}
function parseDocument(value: unknown): DocumentView {
  const v = record(value);
  keys(v, ["current", "relations", "timeline"]);
  return {
    current: parseRevision(v.current),
    relations: list(v.relations).map((value) => {
      const r = record(value);
      if (r.visibility === "restricted") {
        keys(r, ["visibility"]);
        return { visibility: "restricted" };
      }
      keys(r, ["visibility", "direction", "relation_type", "target"]);
      if (
        r.visibility !== "readable" ||
        r.direction !== "incoming" ||
        r.relation_type !== "relates-to"
      )
        throw new TypeError("无效文档关系");
      return {
        visibility: "readable",
        direction: "incoming",
        relation_type: "relates-to",
        target: visible(r.target),
      };
    }),
    timeline: list(v.timeline).map((value) => {
      const t = record(value);
      keys(t, [
        "id",
        "activity_type",
        "actor",
        "occurred_at",
        "revision",
        "restored_from_revision",
        "subjects",
      ]);
      if (
        t.activity_type !== "document.created" &&
        t.activity_type !== "document.revised"
      )
        throw new TypeError("未知文档活动");
      return {
        id: str(t.id),
        activity_type: t.activity_type,
        actor: actor(t.actor),
        occurred_at: time(t.occurred_at),
        revision: integer(t.revision),
        restored_from_revision: nullableRevision(t.restored_from_revision),
        subjects: list(t.subjects).map((value) => {
          const s = record(value);
          if (s.visibility === "restricted") {
            keys(s, ["visibility"]);
            return { visibility: "restricted" as const };
          }
          keys(s, ["visibility", "entity"]);
          if (s.visibility !== "readable") throw new TypeError("无效活动对象");
          return { visibility: "readable" as const, entity: visible(s.entity) };
        }),
      };
    }),
  };
}
function parsePage<T>(value: unknown, parse: (v: unknown) => T): Page<T> {
  const p = record(value);
  keys(p, ["items", "next_cursor"]);
  return {
    items: list(p.items).map(parse),
    next_cursor: p.next_cursor === null ? null : str(p.next_cursor),
  };
}
function history(value: unknown): HistoryItem {
  const v = record(value);
  keys(v, [
    "revision",
    "title",
    "created_by",
    "created_at",
    "restored_from_revision",
  ]);
  return {
    revision: integer(v.revision),
    title: str(v.title),
    created_by: actor(v.created_by),
    created_at: time(v.created_at),
    restored_from_revision: nullableRevision(v.restored_from_revision),
  };
}
function listItem(value: unknown): ListItem {
  const v = record(value);
  keys(v, ["ref", "title", "revision", "created_at", "updated_at"]);
  return {
    ref: ref(v.ref, "document"),
    title: str(v.title),
    revision: integer(v.revision),
    created_at: time(v.created_at),
    updated_at: time(v.updated_at),
  };
}
function result(value: unknown): Result {
  const v = record(value);
  keys(v, ["ref", "applied_revision"]);
  return {
    ref: ref(v.ref, "document"),
    applied_revision: integer(v.applied_revision),
  };
}
async function request<T>(
  path: string,
  parse: (v: unknown) => T,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    const token = csrfTokenFromCookie(document.cookie);
    if (!token) throw new DocumentError("安全校验失败，请重新登录。", 403);
    headers["X-CSRF-Token"] = token;
  }
  let response: Response;
  try {
    response = await fetch(path, {
      method: body === undefined ? "GET" : "POST",
      credentials: "same-origin",
      cache: "no-store",
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
    });
  } catch (e) {
    if (signal?.aborted) throw e;
    throw new DocumentError("请求结果尚未确认，请保留草稿并重试原操作。");
  }
  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    throw new DocumentError("响应无法验证，请重试原操作。");
  }
  if (!response.ok) {
    const error = record(record(payload).error);
    let message =
      response.status === 409
        ? "版本发生变化或重试输入冲突，请读取最新版本。"
        : response.status === 401
          ? "会话已失效。"
          : response.status === 403 || response.status === 404
            ? "当前无法访问或修改这个文档。"
            : "文档请求失败，请检查输入后重试。";
    if (error.diagnostic) {
      const d = record(error.diagnostic);
      message = `Markdown 无法处理：${str(d.code)}（字节 ${integer(d.offset, 0)}）。`;
    }
    throw new DocumentError(
      message,
      response.status,
      typeof error.current_revision === "number"
        ? integer(error.current_revision)
        : undefined,
    );
  }
  try {
    const envelope = record(payload);
    keys(envelope, ["data"]);
    return parse(envelope.data);
  } catch {
    throw new DocumentError("响应不符合文档协议，请重试原操作。");
  }
}
const base = (w: string) => `/api/v1/workspaces/${encodeURIComponent(w)}`;
const after = (cursor?: string) =>
  cursor ? `?after=${encodeURIComponent(cursor)}` : "";
export const documentClient = {
  read: (w: string, id: string, signal?: AbortSignal) =>
    request(
      `${base(w)}/documents/${encodeURIComponent(id)}/nexus-view`,
      parseDocument,
      undefined,
      signal,
    ),
  revision: (w: string, id: string, n: number, signal?: AbortSignal) =>
    request(
      `${base(w)}/documents/${encodeURIComponent(id)}/revisions/${n}`,
      parseRevision,
      undefined,
      signal,
    ),
  list: (w: string, project: string, cursor?: string, signal?: AbortSignal) =>
    request(
      `${base(w)}/projects/${encodeURIComponent(project)}/documents${after(cursor)}`,
      (v) => parsePage(v, listItem),
      undefined,
      signal,
    ),
  history: (w: string, id: string, cursor?: string, signal?: AbortSignal) =>
    request(
      `${base(w)}/documents/${encodeURIComponent(id)}/revisions${after(cursor)}`,
      (v) => parsePage(v, history),
      undefined,
      signal,
    ),
  create: (w: string, ticket: string, input: WriteInput) =>
    request(
      `${base(w)}/tickets/${encodeURIComponent(ticket)}/documents`,
      result,
      input,
    ),
  save: (w: string, id: string, input: WriteInput) =>
    request(
      `${base(w)}/documents/${encodeURIComponent(id)}/revisions`,
      result,
      input,
    ),
  restore: (w: string, id: string, input: WriteInput) =>
    request(
      `${base(w)}/documents/${encodeURIComponent(id)}/restorations`,
      result,
      input,
    ),
  preview: (w: string, project: string, body: string, signal?: AbortSignal) =>
    request(
      `${base(w)}/projects/${encodeURIComponent(project)}/document-preview`,
      parseView,
      { body_markdown: body, format_version: format },
      signal,
    ),
};
export type DocumentClient = typeof documentClient;
