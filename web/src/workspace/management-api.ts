import { AuthRequestError } from "../auth/api";
import { object, requireID } from "./api";
import {
  parsePage,
  query,
  request,
  requestPath,
  text,
} from "./configuration-api";
import { validOwnerOperationID } from "./owner-transfer-api";

export type ManagementKind = "workspace" | "project" | "channel" | "thread";
export type ManagementAction =
  | "add"
  | "role"
  | "remove"
  | "handoff"
  | "leave"
  | "suspend"
  | "resume"
  | "reinvite";
export interface ManagementScope {
  workspace: string;
  kind: ManagementKind;
  id: string;
}
export interface ManagementProposal {
  target_user_id: string;
  role: string;
  actor_after_role: string;
}
export interface ManagementPlan extends ManagementProposal {
  scope_kind: ManagementKind;
  scope_id: string;
  action: ManagementAction;
  actor_id: string;
  target_name: string;
  before: string;
  after: string;
  actor_role: string;
  target_workspace_role: string;
  target_status: string;
  scope_revision: string;
  actor_lifecycle_revision: string;
  target_lifecycle_revision: string;
  actor_role_revision: string;
  target_role_revision: string;
  parent_project_revision: string;
  parent_channel_revision: string;
  policy: string;
  digest: string;
}
export interface ManagementInput extends ManagementProposal {
  client_operation_id: string;
  plan_digest: string;
  confirmed: true;
}
export interface ManagementMember {
  id: string;
  displayName: string;
  role: string;
  status: string;
  revision: string;
  eligible: boolean;
}
export interface ManagementMembers {
  items: readonly ManagementMember[];
  nextCursor: string | null;
  canManage: boolean;
  selfRole: string;
  revision: string;
}
export interface ManagementResponsibility {
  id: string;
  kind: "project" | "channel" | "thread";
  name: string;
  role: string;
}
export interface ManagementAudit {
  id: string;
  actorID: string;
  targetID: string;
  action: string;
  before: string;
  after: string;
  occurredAt: string;
}
export type ManagementResult =
  | { result: "not_observed"; operationID: string }
  | {
      result: "applied";
      operationID: string;
      action: string;
      scopeID: string;
      targetID: string;
      occurredAt: string;
      invitation?: { token: string; expiresAt: string };
    };
const invalid = () =>
  new AuthRequestError("成员管理响应不符合当前合同，请查询原操作结果。");
const roles = [
  "",
  "owner",
  "member",
  "viewer",
  "contributor",
  "decider",
  "admin",
  "manager",
];
function revision(v: unknown, zero = false): string {
  if (
    typeof v !== "string" ||
    !/^(0|[1-9][0-9]{0,18})$/.test(v) ||
    BigInt(v) > 9223372036854775807n ||
    (!zero && v === "0")
  )
    throw invalid();
  return v;
}
function role(v: unknown): string {
  if (typeof v !== "string" || !roles.includes(v)) throw invalid();
  return v;
}
function state(v: unknown): string {
  if (
    typeof v !== "string" ||
    ![...roles, "active", "suspended", "removed"].includes(v)
  )
    throw invalid();
  return v;
}
function timestamp(v: unknown): string {
  const s = text(v);
  if (!s.endsWith("Z") || !Number.isFinite(Date.parse(s))) throw invalid();
  return s;
}
function path(s: ManagementScope): string {
  requireID(s.workspace, "wrk_");
  requireID(
    s.id,
    { workspace: "wrk_", project: "prj_", channel: "chn_", thread: "thr_" }[
      s.kind
    ],
  );
  if (s.kind === "workspace") {
    if (s.id !== s.workspace) throw invalid();
    return "/management";
  }
  return `/${s.kind}s/${encodeURIComponent(s.id)}/management`;
}
function member(value: unknown): ManagementMember {
  const r = object(value, [
    "id",
    "display_name",
    "status",
    "role",
    "lifecycle_revision",
    "eligible",
  ]);
  if (
    !["active", "suspended", "removed"].includes(String(r.status)) ||
    typeof r.eligible !== "boolean"
  )
    throw invalid();
  return {
    id: requireID(r.id, "usr_"),
    displayName: text(r.display_name),
    status: String(r.status),
    role: role(r.role),
    revision: revision(r.lifecycle_revision),
    eligible: r.eligible,
  };
}
function result(
  value: unknown,
  workspace: string,
  operation: string,
  allowInvitation: boolean,
): ManagementResult {
  if (typeof value !== "object" || !value) throw invalid();
  const notObserved = "result" in value && value.result === "not_observed";
  const hasInvitation = "invitation" in value;
  const r = object(
    value,
    notObserved
      ? ["result", "workspace_id", "client_operation_id"]
      : [
          "result",
          "workspace_id",
          "client_operation_id",
          "action",
          "scope_id",
          "target_user_id",
          "occurred_at",
          ...(hasInvitation ? ["invitation"] : []),
        ],
  );
  if (r.workspace_id !== workspace || r.client_operation_id !== operation)
    throw invalid();
  if (notObserved) return { result: "not_observed", operationID: operation };
  if (
    r.result !== "applied" ||
    typeof r.action !== "string" ||
    !/^management\.(workspace|project|channel|thread)\.(add|role|remove|handoff|leave|suspend|resume|reinvite)$/.test(
      r.action,
    )
  )
    throw invalid();
  const kind = r.action.split(".")[1] as ManagementKind;
  const out: ManagementResult = {
    result: "applied",
    operationID: operation,
    action: r.action,
    scopeID: requireID(
      r.scope_id,
      { workspace: "wrk_", project: "prj_", channel: "chn_", thread: "thr_" }[
        kind
      ],
    ),
    targetID: requireID(r.target_user_id, "usr_"),
    occurredAt: timestamp(r.occurred_at),
  };
  if (hasInvitation) {
    if (!allowInvitation || r.action !== "management.workspace.reinvite")
      throw invalid();
    const i = object(r.invitation, ["token", "expires_at"]);
    if (typeof i.token !== "string" || !/^[-_A-Za-z0-9]{43}$/.test(i.token))
      throw invalid();
    out.invitation = { token: i.token, expiresAt: timestamp(i.expires_at) };
  }
  return out;
}
export const managementClient = {
  async responsibilities(
    workspace: string,
    after?: string,
    signal?: AbortSignal,
  ) {
    return parsePage(
      await request(
        workspace,
        `/management/responsibilities${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (value): ManagementResponsibility => {
        const r = object(value, ["id", "kind", "name", "role"]);
        if (r.kind !== "project" && r.kind !== "channel" && r.kind !== "thread")
          throw invalid();
        return {
          id: requireID(
            r.id,
            { project: "prj_", channel: "chn_", thread: "thr_" }[r.kind],
          ),
          kind: r.kind,
          name: text(r.name),
          role: role(r.role),
        };
      },
      after,
    );
  },
  async members(
    scope: ManagementScope,
    candidates = false,
    after?: string,
    signal?: AbortSignal,
  ): Promise<ManagementMembers> {
    const r = object(
      await request(
        scope.workspace,
        `${path(scope)}/${candidates ? "candidates" : "members"}${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      ["items", "next_cursor", "can_manage", "self_role", "scope_revision"],
    );
    if (typeof r.can_manage !== "boolean") throw invalid();
    return {
      ...parsePage(
        { items: r.items, next_cursor: r.next_cursor },
        member,
        after,
      ),
      canManage: r.can_manage,
      selfRole: role(r.self_role),
      revision: revision(r.scope_revision),
    };
  },
  async preview(
    scope: ManagementScope,
    action: ManagementAction,
    input: ManagementProposal,
    actor: string,
    signal?: AbortSignal,
  ): Promise<ManagementPlan> {
    const r = object(
      await request(
        scope.workspace,
        `${path(scope)}/${action}/preview`,
        "POST",
        { ...input },
        signal,
      ),
      [
        "scope_kind",
        "scope_id",
        "action",
        "target_user_id",
        "role",
        "actor_after_role",
        "actor_id",
        "target_name",
        "before",
        "after",
        "actor_role",
        "target_workspace_role",
        "target_status",
        "scope_revision",
        "actor_lifecycle_revision",
        "target_lifecycle_revision",
        "actor_role_revision",
        "target_role_revision",
        "parent_project_revision",
        "parent_channel_revision",
        "policy",
        "digest",
      ],
    );
    if (
      r.scope_kind !== scope.kind ||
      r.scope_id !== scope.id ||
      r.action !== action ||
      r.actor_id !== actor ||
      r.target_user_id !== input.target_user_id ||
      r.role !== input.role ||
      r.actor_after_role !== input.actor_after_role ||
      r.policy !== "membership-management-v1" ||
      typeof r.digest !== "string" ||
      !/^[0-9a-f]{64}$/.test(r.digest)
    )
      throw invalid();
    text(r.target_name);
    state(r.before);
    state(r.after);
    role(r.actor_role);
    role(r.target_workspace_role);
    state(r.target_status);
    for (const k of [
      "scope_revision",
      "actor_lifecycle_revision",
      "target_lifecycle_revision",
      "actor_role_revision",
      "target_role_revision",
    ])
      revision(r[k]);
    for (const k of ["parent_project_revision", "parent_channel_revision"])
      revision(r[k], true);
    return r as unknown as ManagementPlan;
  },
  async apply(
    scope: ManagementScope,
    action: ManagementAction,
    input: ManagementInput,
    signal?: AbortSignal,
  ): Promise<ManagementResult> {
    if (!validOwnerOperationID(input.client_operation_id)) throw invalid();
    const out = result(
      await request(
        scope.workspace,
        `${path(scope)}/${action}`,
        "POST",
        { ...input },
        signal,
      ),
      scope.workspace,
      input.client_operation_id,
      true,
    );
    if (
      out.result !== "applied" ||
      out.action !== `management.${scope.kind}.${action}` ||
      out.scopeID !== scope.id ||
      out.targetID !== input.target_user_id
    )
      throw invalid();
    return out;
  },
  async result(
    workspace: string,
    operation: string,
    signal?: AbortSignal,
  ): Promise<ManagementResult> {
    requireID(workspace, "wrk_");
    if (!validOwnerOperationID(operation)) throw invalid();
    return result(
      await requestPath(
        `/api/v1/auth/management-results/${encodeURIComponent(workspace)}/${operation}`,
        "GET",
        undefined,
        signal,
      ),
      workspace,
      operation,
      false,
    );
  },
  async audit(scope: ManagementScope, after?: string, signal?: AbortSignal) {
    return parsePage(
      await request(
        scope.workspace,
        `${path(scope)}/audit${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (value): ManagementAudit => {
        const r = object(value, [
          "id",
          "actor_id",
          "target_user_id",
          "action",
          "before",
          "after",
          "occurred_at",
        ]);
        if (
          typeof r.action !== "string" ||
          !r.action.startsWith(`management.${scope.kind}.`)
        )
          throw invalid();
        return {
          id: requireID(r.id, "cfa_"),
          actorID: requireID(r.actor_id, "usr_"),
          targetID: requireID(r.target_user_id, "usr_"),
          action: r.action,
          before: state(r.before),
          after: state(r.after),
          occurredAt: timestamp(r.occurred_at),
        };
      },
      after,
    );
  },
};

// Persist only known receipt coordinates, never plans, identities or tokens.
export function rememberManagementOperation(
  actor: string,
  workspace: string,
  operation: string,
): void {
  const items = managementOperations(actor).filter(
    (x) => x.operation !== operation,
  );
  items.push({ workspace, operation });
  sessionStorage.setItem(
    `radishnexus.management.${actor}`,
    JSON.stringify(items.slice(-50)),
  );
}
export function managementOperations(
  actor: string,
): { workspace: string; operation: string }[] {
  const raw = sessionStorage.getItem(`radishnexus.management.${actor}`);
  if (!raw) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  return parsed
    .filter(
      (x): x is { workspace: string; operation: string } =>
        typeof x === "object" &&
        x !== null &&
        typeof x.workspace === "string" &&
        /^wrk_[A-Za-z0-9_-]+$/.test(x.workspace) &&
        typeof x.operation === "string" &&
        validOwnerOperationID(x.operation),
    )
    .slice(-50);
}
