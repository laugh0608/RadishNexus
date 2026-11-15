import type { ClassificationInput } from "./ticket-classification-api";
import { channelPagePath, parseChannelMessageEnvelope } from "../channel/api";
import { object, requireID } from "../workspace/api";
import {
  parsePage,
  query,
  request,
  text,
} from "../workspace/configuration-api";
import { parseTicketStatus } from "./ticket-status";

const messagePath = (channel: string, message: string) =>
  `/channels/${encodeURIComponent(requireID(channel, "chn_"))}/messages/${encodeURIComponent(requireID(message, "msg_"))}`;
const threadPath = (thread: string) =>
  `/threads/${encodeURIComponent(requireID(thread, "thr_"))}`;
const ticketPath = (ticket: string) =>
  `/tickets/${encodeURIComponent(requireID(ticket, "tkt_"))}`;

function id(value: unknown, kind: string, prefix: string) {
  const ref = object(value, ["type", "id"]);
  if (ref.type !== kind) throw new TypeError("Invalid execution reference");
  return requireID(ref.id, prefix);
}

export function parseExecutionMessage(value: unknown) {
  const data = object(value, ["ref", "channel", "thread"]);
  return {
    messageID: id(data.ref, "message", "msg_"),
    channelID: id(data.channel, "channel", "chn_"),
    threadID: data.thread === null ? null : id(data.thread, "thread", "thr_"),
  };
}

export function executionMessageLink(
  workspace: string,
  location: ReturnType<typeof parseExecutionMessage>,
) {
  const page =
    location.threadID === null
      ? channelPagePath(workspace, location.channelID)
      : `/workspaces/${encodeURIComponent(workspace)}${threadPath(location.threadID)}`;
  return page === null
    ? null
    : `${page}?message=${encodeURIComponent(location.messageID)}`;
}

export function executionMessageLocation(search: string): string | null {
  const values = new URLSearchParams(search).getAll("message");
  try {
    return values.length === 1 ? requireID(values[0], "msg_") : null;
  } catch {
    return null;
  }
}

interface Confirmation {
  client_operation_id: string;
  confirmed: true;
}

export type ExecutionListRow = {
  id: string;
  title: string;
  linkID?: string;
  canUnlink: boolean;
};

export const executionClient = {
  async message(
    workspace: string,
    channel: string,
    message: string,
    signal: AbortSignal,
  ) {
    const data = await request(
      workspace,
      messagePath(channel, message),
      "GET",
      undefined,
      signal,
    );
    const row = parseChannelMessageEnvelope({ data }, channel);
    if (row.id !== message) throw new TypeError("Unexpected Message");
    return row;
  },
  async list(
    workspace: string,
    target:
      | { kind: "message"; channel: string; id: string }
      | { kind: "thread" | "ticket"; id: string },
    after?: string,
    signal?: AbortSignal,
  ) {
    const path =
      target.kind === "message"
        ? `${messagePath(target.channel, target.id)}/tickets`
        : target.kind === "thread"
          ? `${threadPath(target.id)}/tickets`
          : `${ticketPath(target.id)}/discussions`;
    const raw = object(
      await request(workspace, path + query(after), "GET", undefined, signal),
      ["items", "next_cursor", "capabilities"],
    );
    const capability = target.kind === "message" ? "can_create" : "can_link";
    const canWrite = object(raw.capabilities, [capability])[capability];
    if (typeof canWrite !== "boolean")
      throw new TypeError("Invalid execution capability");
    const page = parsePage<ExecutionListRow>(
      { items: raw.items, next_cursor: raw.next_cursor },
      (value) => {
        const row = object(value, [
          "ref",
          "title",
          ...(target.kind === "ticket" ? ["channel"] : ["status", "project"]),
          ...(target.kind === "message" ? [] : ["link_id", "can_unlink"]),
        ]);
        if (target.kind === "ticket") id(row.channel, "channel", "chn_");
        else {
          id(row.project, "project", "prj_");
          parseTicketStatus(row.status);
        }
        if (target.kind !== "message" && typeof row.can_unlink !== "boolean")
          throw new TypeError("Invalid discussion capability");
        return {
          id:
            target.kind === "ticket"
              ? id(row.ref, "thread", "thr_")
              : id(row.ref, "ticket", "tkt_"),
          title: text(row.title),
          canUnlink: row.can_unlink === true,
          ...(target.kind === "message"
            ? {}
            : { linkID: requireID(row.link_id, "lnk_") }),
        };
      },
      after,
    );
    return { ...page, canWrite };
  },
  async create(
    workspace: string,
    channel: string,
    message: string,
    input: Confirmation & {
      title: string;
      classification?: ClassificationInput;
    },
    signal: AbortSignal,
  ) {
    const out = object(
      await request(
        workspace,
        `${messagePath(channel, message)}/tickets`,
        "POST",
        { ...input },
        signal,
      ),
      ["ticket", "source_message"],
    );
    const source = parseExecutionMessage(out.source_message);
    const ticket = object(out.ticket, [
      "ref",
      "project",
      "title",
      "status",
      "status_revision",
      "created_by",
      "created_at",
      "updated_at",
    ]);
    if (
      source.channelID !== channel ||
      source.messageID !== message ||
      ticket.title !== input.title.trim()
    )
      throw new TypeError("Invalid Message Ticket receipt");
    return id(ticket.ref, "ticket", "tkt_");
  },
  async discussion(
    workspace: string,
    ticket: string,
    thread: string,
    link: string | undefined,
    input: Confirmation,
    signal: AbortSignal,
  ) {
    const suffix =
      link === undefined
        ? "/discussions"
        : `/discussion-links/${encodeURIComponent(requireID(link, "lnk_"))}`;
    const data = object(
      await request(
        workspace,
        ticketPath(ticket) + suffix,
        link === undefined ? "POST" : "DELETE",
        {
          ...input,
          ...(link === undefined
            ? { thread_id: requireID(thread, "thr_") }
            : {}),
        },
        signal,
      ),
      ["link", "applied"],
    );
    const appliedLink = id(data.link, "entity-link", "lnk_");
    if (data.applied !== true || (link !== undefined && appliedLink !== link))
      throw new TypeError("Invalid discussion receipt");
  },
  async replies(
    workspace: string,
    thread: string,
    channel: string,
    before: string | undefined,
    signal: AbortSignal,
  ) {
    const suffix =
      before === undefined ? "" : `?before=${encodeURIComponent(before)}`;
    const raw = object(
      await request(
        workspace,
        `${threadPath(thread)}/messages${suffix}`,
        "GET",
        undefined,
        signal,
      ),
      ["channel", "messages", "older_cursor", "can_reply"],
    );
    if (
      id(raw.channel, "channel", "chn_") !== channel ||
      !Array.isArray(raw.messages) ||
      raw.messages.length > 50 ||
      typeof raw.can_reply !== "boolean"
    )
      throw new TypeError("Invalid Thread message page");
    const messages = raw.messages.map((data) =>
      parseChannelMessageEnvelope({ data }, channel),
    );
    if (
      messages.some((message) => message.threadID !== thread) ||
      new Set(messages.map((message) => message.id)).size !== messages.length
    )
      throw new TypeError("Unexpected Thread reply");
    const cursor = raw.older_cursor;
    if (
      cursor !== null &&
      (typeof cursor !== "string" ||
        !cursor.startsWith(`${thread}.`) ||
        cursor === before ||
        messages.length !== 50)
    )
      throw new TypeError("Invalid Thread message cursor");
    return { messages, olderCursor: cursor, canReply: raw.can_reply };
  },
};
