import { csrfTokenFromCookie } from "../auth/api";

export interface ChannelMessage {
  id: string;
  channelID: string;
  threadID: string | null;
  authorID: string;
  body: string;
  createdAt: string;
}

export interface ChannelMessagePage {
  messages: readonly ChannelMessage[];
  olderCursor: string | null;
}

export interface CreateMessageInput {
  clientOperationID: string;
  body: string;
  threadID?: string;
}

export interface CreateMessageOutcome {
  message: ChannelMessage;
  created: boolean;
}

export type ThreadVisibility = "project" | "restricted";

export interface StartedThread {
  id: string;
  channelID: string;
  sourceMessageID: string;
  title: string;
  visibility: ThreadVisibility;
  createdAt: string;
}

export interface StartThreadInput {
  title: string;
  visibility: ThreadVisibility;
}

export interface ChannelMessageClient {
  listMessages(
    workspaceID: string,
    channelID: string,
    before?: string,
    signal?: AbortSignal,
  ): Promise<ChannelMessagePage>;
  createMessage(
    workspaceID: string,
    channelID: string,
    input: CreateMessageInput,
    signal?: AbortSignal,
  ): Promise<CreateMessageOutcome>;
  startThread(
    workspaceID: string,
    channelID: string,
    messageID: string,
    input: StartThreadInput,
    signal?: AbortSignal,
  ): Promise<StartedThread>;
}

export interface ChannelLocation {
  workspaceID: string;
  channelID: string;
}

export class ChannelRequestError extends Error {
  readonly code?: string;
  readonly status?: number;
  readonly userMessage: string;

  constructor(
    userMessage: string,
    options?: { code?: string; status?: number; cause?: unknown },
  ) {
    super(userMessage, { cause: options?.cause });
    this.name = "ChannelRequestError";
    this.code = options?.code;
    this.status = options?.status;
    this.userMessage = userMessage;
  }
}

export const browserChannelMessageClient: ChannelMessageClient = {
  listMessages,
  createMessage,
  startThread,
};

export function channelLocation(pathname: string): ChannelLocation | null {
  const match = /^\/workspaces\/([^/]+)\/channels\/([^/]+)\/?$/u.exec(pathname);
  if (match === null) {
    return null;
  }
  try {
    const workspaceID = decodeURIComponent(match[1] ?? "");
    const channelID = decodeURIComponent(match[2] ?? "");
    return validPathID(workspaceID, "wrk_") && validPathID(channelID, "chn_")
      ? { workspaceID, channelID }
      : null;
  } catch {
    return null;
  }
}

export function channelPagePath(
  workspaceID: string,
  channelID: string,
): string | null {
  if (!validPathID(workspaceID, "wrk_") || !validPathID(channelID, "chn_")) {
    return null;
  }
  return `/workspaces/${encodeURIComponent(workspaceID)}/channels/${encodeURIComponent(channelID)}`;
}

export function newClientOperationID(): string {
  if (typeof globalThis.crypto?.randomUUID !== "function") {
    throw new ChannelRequestError(
      "当前浏览器无法生成安全的发送标识，请更新浏览器后重试。",
    );
  }
  return `web:${globalThis.crypto.randomUUID()}`;
}

async function listMessages(
  workspaceID: string,
  channelID: string,
  before?: string,
  signal?: AbortSignal,
): Promise<ChannelMessagePage> {
  requirePathIDs(workspaceID, channelID);
  if (before !== undefined && !validCursor(before)) {
    throw new ChannelRequestError("消息分页位置无效，请刷新 Channel。", {
      code: "invalid",
      status: 400,
    });
  }
  const query = new URLSearchParams({ limit: "50" });
  if (before !== undefined) {
    query.set("before", before);
  }
  const response = await channelFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/channels/${encodeURIComponent(channelID)}/messages?${query.toString()}`,
    {
      method: "GET",
      headers: { Accept: "application/json" },
      credentials: "same-origin",
      cache: "no-store",
      signal,
    },
  );
  if (response.status !== 200) {
    throw contractError("消息服务返回了意外的成功状态。", response.status);
  }
  const payload = await responseJSON(response, "消息历史");
  try {
    return parseMessagePageResponse(payload, channelID);
  } catch (error) {
    throw contractError(
      "服务返回的消息历史不符合当前契约，请稍后重试。",
      response.status,
      error,
    );
  }
}

async function createMessage(
  workspaceID: string,
  channelID: string,
  input: CreateMessageInput,
  signal?: AbortSignal,
): Promise<CreateMessageOutcome> {
  requirePathIDs(workspaceID, channelID);
  if (
    !validOperationID(input.clientOperationID) ||
    !validMessageBody(input.body)
  ) {
    throw new ChannelRequestError(
      "消息必须包含正文，并且 UTF-8 大小不能超过 16 KiB。",
      { code: "invalid", status: 400 },
    );
  }
  if (input.threadID !== undefined && !validPathID(input.threadID, "thr_")) {
    throw new ChannelRequestError("回复目标 Thread 无效。", {
      code: "invalid",
      status: 400,
    });
  }
  const response = await channelFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/channels/${encodeURIComponent(channelID)}/messages`,
    writeRequest(
      {
        client_operation_id: input.clientOperationID,
        body: input.body,
        ...(input.threadID === undefined ? {} : { thread_id: input.threadID }),
      },
      signal,
    ),
  );
  if (response.status !== 200 && response.status !== 201) {
    throw contractError("消息服务返回了意外的创建状态。", response.status);
  }
  const payload = await responseJSON(response, "消息写入");
  try {
    return {
      message: parseMessageEnvelope(payload, channelID),
      created: response.status === 201,
    };
  } catch (error) {
    throw contractError(
      "服务返回的消息写入结果不符合当前契约，请稍后重试。",
      response.status,
      error,
    );
  }
}

async function startThread(
  workspaceID: string,
  channelID: string,
  messageID: string,
  input: StartThreadInput,
  signal?: AbortSignal,
): Promise<StartedThread> {
  requirePathIDs(workspaceID, channelID);
  if (!validPathID(messageID, "msg_") || input.title.trim() === "") {
    throw new ChannelRequestError("请选择有效 Message 并填写 Thread 标题。", {
      code: "invalid",
      status: 400,
    });
  }
  if (input.visibility !== "project" && input.visibility !== "restricted") {
    throw new ChannelRequestError("Thread 可见性无效。", {
      code: "invalid",
      status: 400,
    });
  }
  const response = await channelFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/channels/${encodeURIComponent(channelID)}/messages/${encodeURIComponent(messageID)}/threads`,
    writeRequest({ title: input.title, visibility: input.visibility }, signal),
  );
  if (response.status !== 201) {
    throw contractError("Thread 服务返回了意外的创建状态。", response.status);
  }
  const payload = await responseJSON(response, "Thread 写入");
  try {
    return parseThreadEnvelope(payload, channelID, messageID);
  } catch (error) {
    throw contractError(
      "服务返回的 Thread 写入结果不符合当前契约，请稍后重试。",
      response.status,
      error,
    );
  }
}

async function channelFetch(
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
    throw new ChannelRequestError(
      "无法连接到 RadishNexus，请检查网络后重试。",
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
    throw new ChannelRequestError("安全校验失败，请刷新页面后重试。", {
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
): Promise<ChannelRequestError> {
  let code: string | undefined;
  try {
    const payload = record(await response.json(), "error response");
    const error = record(payload.error, "error response.error");
    code = string(error.code, "error response.error.code");
  } catch {
    code = undefined;
  }
  return new ChannelRequestError(channelErrorMessage(response.status, code), {
    code,
    status: response.status,
  });
}

function channelErrorMessage(status: number, code?: string): string {
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
    return "你可以读取这个 Channel，但当前角色不能执行该操作。";
  }
  if (code === "not_found" || status === 404) {
    return "该 Channel 或来源 Message 不存在，或你已没有读取权限。";
  }
  if (code === "conflict" || status === 409) {
    return "该操作与当前状态冲突；请刷新 Channel 后确认现有结果。";
  }
  if (code === "invalid" || status === 400 || status === 413) {
    return "请求内容不符合当前消息契约，请检查后重试。";
  }
  return "消息服务暂不可用，请稍后重试。";
}

function parseMessagePageResponse(
  payload: unknown,
  expectedChannelID: string,
): ChannelMessagePage {
  const envelope = record(payload, "response");
  const data = record(envelope.data, "response.data");
  const messages = array(data.messages, "response.data.messages").map(
    (message, index) =>
      parseMessage(
        message,
        `response.data.messages[${index}]`,
        expectedChannelID,
      ),
  );
  if (new Set(messages.map((message) => message.id)).size !== messages.length) {
    throw new TypeError("response.data.messages contains duplicate IDs");
  }
  const olderCursor = nullableCursor(
    data.older_cursor,
    "response.data.older_cursor",
  );
  if (messages.length === 0 && olderCursor !== null) {
    throw new TypeError("empty Message page must not have an older cursor");
  }
  return { messages, olderCursor };
}

function parseMessageEnvelope(
  payload: unknown,
  expectedChannelID: string,
): ChannelMessage {
  const envelope = record(payload, "response");
  return parseMessage(envelope.data, "response.data", expectedChannelID);
}

function parseMessage(
  value: unknown,
  path: string,
  expectedChannelID: string,
): ChannelMessage {
  const message = record(value, path);
  const ref = parseRef(message.ref, `${path}.ref`, "message");
  const channel = parseRef(message.channel, `${path}.channel`, "channel");
  if (channel.id !== expectedChannelID) {
    throw new TypeError(`${path}.channel does not match requested Channel`);
  }
  const thread =
    message.thread === null
      ? null
      : parseRef(message.thread, `${path}.thread`, "thread").id;
  const author = record(message.author, `${path}.author`);
  if (author.kind !== "user") {
    throw new TypeError(`${path}.author.kind is invalid`);
  }
  const body = nonEmptyString(message.body, `${path}.body`);
  if (!validMessageBody(body)) {
    throw new TypeError(`${path}.body is invalid`);
  }
  return {
    id: ref.id,
    channelID: channel.id,
    threadID: thread,
    authorID: scopedID(author.id, `${path}.author.id`, "usr_"),
    body,
    createdAt: timestamp(message.created_at, `${path}.created_at`),
  };
}

function parseThreadEnvelope(
  payload: unknown,
  expectedChannelID: string,
  expectedMessageID: string,
): StartedThread {
  const envelope = record(payload, "response");
  const thread = record(envelope.data, "response.data");
  const ref = parseRef(thread.ref, "response.data.ref", "thread");
  const channel = parseRef(thread.channel, "response.data.channel", "channel");
  const source = parseRef(
    thread.source_message,
    "response.data.source_message",
    "message",
  );
  if (channel.id !== expectedChannelID || source.id !== expectedMessageID) {
    throw new TypeError("created Thread does not match requested source");
  }
  const visibility = string(thread.visibility, "response.data.visibility");
  if (visibility !== "project" && visibility !== "restricted") {
    throw new TypeError("response.data.visibility is invalid");
  }
  return {
    id: ref.id,
    channelID: channel.id,
    sourceMessageID: source.id,
    title: nonEmptyString(thread.title, "response.data.title"),
    visibility,
    createdAt: timestamp(thread.created_at, "response.data.created_at"),
  };
}

function parseRef(
  value: unknown,
  path: string,
  expectedType: "channel" | "message" | "thread",
): { type: string; id: string } {
  const ref = record(value, path);
  if (ref.type !== expectedType) {
    throw new TypeError(`${path}.type is invalid`);
  }
  const prefix =
    expectedType === "channel"
      ? "chn_"
      : expectedType === "message"
        ? "msg_"
        : "thr_";
  return {
    type: expectedType,
    id: scopedID(ref.id, `${path}.id`, prefix),
  };
}

function requirePathIDs(workspaceID: string, channelID: string): void {
  if (!validPathID(workspaceID, "wrk_") || !validPathID(channelID, "chn_")) {
    throw new ChannelRequestError("Channel 地址无效，无法读取消息。", {
      code: "invalid",
      status: 400,
    });
  }
}

function validOperationID(value: string): boolean {
  return (
    value.length >= 1 && value.length <= 128 && /^[\x21-\x7e]+$/u.test(value)
  );
}

function validMessageBody(value: string): boolean {
  return (
    value.trim() !== "" &&
    !value.includes("\0") &&
    new TextEncoder().encode(value).byteLength <= 16 * 1024
  );
}

function validCursor(value: string): boolean {
  return (
    value.length >= 1 && value.length <= 512 && /^[A-Za-z0-9_-]+$/u.test(value)
  );
}

function nullableCursor(value: unknown, path: string): string | null {
  if (value === null) {
    return null;
  }
  const cursor = string(value, path);
  if (!validCursor(cursor)) {
    throw new TypeError(`${path} is invalid`);
  }
  return cursor;
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

function contractError(
  message: string,
  status?: number,
  cause?: unknown,
): ChannelRequestError {
  return new ChannelRequestError(message, { status, cause });
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}
