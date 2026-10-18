import { AuthRequestError } from "../auth/api";
import { object, requireID } from "./api";
import {
  request,
  query,
  parsePage,
  text,
  type ConfigurationPage,
} from "./configuration-api";

export interface Ownership {
  userID: string;
  role: "owner" | "member";
  revision: string;
  canTransfer: boolean;
  recent: boolean;
}
export interface OwnerCandidate {
  id: string;
  displayName: string;
  revision: string;
}
export interface OwnerTransferRequest {
  client_operation_id: string;
  target_user_id: string;
  expected_actor_role_revision: string;
  expected_target_role_revision: string;
  confirmed: true;
}
export type OwnerTransferResult =
  | { result: "not_observed"; operationID: string }
  | {
      result: "applied";
      operationID: string;
      actorID: string;
      targetID: string;
      auditID: string;
      occurredAt: string;
      actorRevision: string;
      targetRevision: string;
    };
const invalid = () =>
  new AuthRequestError("交接服务响应不符合当前契约，请查询原操作结果。");
export function validOwnerOperationID(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
    value,
  );
}
function revision(value: unknown): string {
  if (
    typeof value !== "string" ||
    !/^[1-9][0-9]{0,18}$/.test(value) ||
    BigInt(value) > 9223372036854775807n
  )
    throw invalid();
  return value;
}
function result(
  value: unknown,
  workspace: string,
  actor: string,
  operation: string,
): OwnerTransferResult {
  const kind =
    typeof value === "object" && value !== null && "result" in value
      ? value.result
      : undefined;
  const row = object(
    value,
    kind === "not_observed"
      ? ["result", "workspace_id", "client_operation_id"]
      : [
          "result",
          "workspace_id",
          "client_operation_id",
          "actor_user_id",
          "target_user_id",
          "audit_id",
          "occurred_at",
          "actor_role_revision",
          "target_role_revision",
        ],
  );
  if (row.workspace_id !== workspace || row.client_operation_id !== operation)
    throw invalid();
  if (row.result === "not_observed")
    return { result: "not_observed", operationID: operation };
  if (
    row.result !== "applied" ||
    row.actor_user_id !== actor ||
    row.target_user_id === actor ||
    typeof row.occurred_at !== "string" ||
    !Number.isFinite(Date.parse(row.occurred_at)) ||
    !row.occurred_at.endsWith("Z")
  )
    throw invalid();
  const ar = revision(row.actor_role_revision),
    tr = revision(row.target_role_revision);
  if (BigInt(ar) < 2n || BigInt(tr) < 2n) throw invalid();
  return {
    result: "applied",
    operationID: operation,
    actorID: actor,
    targetID: requireID(row.target_user_id, "usr_"),
    auditID: requireID(row.audit_id, "cfa_"),
    occurredAt: row.occurred_at,
    actorRevision: ar,
    targetRevision: tr,
  };
}
export interface OwnerTransferClient {
  ownership(
    workspace: string,
    actor: string,
    signal?: AbortSignal,
  ): Promise<Ownership>;
  candidates(
    workspace: string,
    actor: string,
    after?: string,
    signal?: AbortSignal,
  ): Promise<ConfigurationPage<OwnerCandidate>>;
  transfer(
    workspace: string,
    actor: string,
    input: OwnerTransferRequest,
    signal?: AbortSignal,
  ): Promise<OwnerTransferResult>;
  status(
    workspace: string,
    actor: string,
    operation: string,
    signal?: AbortSignal,
  ): Promise<OwnerTransferResult>;
}
export const ownerTransferClient: OwnerTransferClient = {
  async ownership(workspace, actor, signal) {
    const row = object(
      await request(workspace, "/ownership", "GET", undefined, signal),
      ["self", "can_transfer", "recent_authentication"],
    );
    const self = object(row.self, ["id", "role", "role_revision"]);
    if (
      self.id !== actor ||
      (self.role !== "owner" && self.role !== "member") ||
      row.can_transfer !== (self.role === "owner") ||
      typeof row.recent_authentication !== "boolean"
    )
      throw invalid();
    return {
      userID: actor,
      role: self.role,
      revision: revision(self.role_revision),
      canTransfer: row.can_transfer,
      recent: row.recent_authentication,
    };
  },
  async candidates(workspace, actor, after, signal) {
    return parsePage(
      await request(
        workspace,
        `/owner-transfer-candidates${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (value) => {
        const row = object(value, ["id", "display_name", "role_revision"]);
        if (row.id === actor) throw invalid();
        return {
          id: requireID(row.id, "usr_"),
          displayName: text(row.display_name),
          revision: revision(row.role_revision),
        };
      },
      after,
    );
  },
  async transfer(workspace, actor, input, signal) {
    if (
      !validOwnerOperationID(input.client_operation_id) ||
      input.target_user_id === actor ||
      input.confirmed !== true
    )
      throw invalid();
    requireID(input.target_user_id, "usr_");
    revision(input.expected_actor_role_revision);
    revision(input.expected_target_role_revision);
    const parsed = result(
      await request(
        workspace,
        "/owner-transfers",
        "POST",
        { ...input },
        signal,
      ),
      workspace,
      actor,
      input.client_operation_id,
    );
    if (
      parsed.result !== "applied" ||
      parsed.targetID !== input.target_user_id ||
      BigInt(parsed.actorRevision) !==
        BigInt(input.expected_actor_role_revision) + 1n ||
      BigInt(parsed.targetRevision) !==
        BigInt(input.expected_target_role_revision) + 1n
    )
      throw invalid();
    return parsed;
  },
  async status(workspace, actor, operation, signal) {
    if (!validOwnerOperationID(operation)) throw invalid();
    return result(
      await request(
        workspace,
        `/owner-transfers/${operation}`,
        "GET",
        undefined,
        signal,
      ),
      workspace,
      actor,
      operation,
    );
  },
};
