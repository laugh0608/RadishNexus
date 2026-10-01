import { AuthRequestError } from "../auth/api";
import { object, requireID } from "./api";
import { request, text, query, parsePage } from "./configuration-api";

export type DeliveryKind = "component" | "environment";
export const componentTypes = [
  "service",
  "web",
  "client",
  "library",
  "data-pipeline",
  "infrastructure",
  "other",
] as const;
export interface DeliveryObject {
  id: string;
  kind: DeliveryKind;
  key: string;
  name: string;
  ownerTeamID: string | null;
  category: string;
  status: string;
  canManage: boolean;
  canGrant: boolean;
  canRevoke: boolean;
}
export interface AuthorizationState {
  id: string;
  status: "active" | "revoked";
}
export interface AuthorizationMember {
  id: string;
  name: string;
  eligible: boolean;
  authorization: AuthorizationState | null;
}
const invalid = () =>
  new AuthRequestError("交付配置响应不符合当前契约，请刷新或联系管理员。");

function parseDeliveryObject(
  value: unknown,
  kind: DeliveryKind,
  detail: boolean,
): DeliveryObject {
  const row = object(value, [
    "ref",
    "key",
    "name",
    "owner_team_id",
    ...(kind === "component"
      ? ["type", "lifecycle"]
      : ["classification", "status"]),
    ...(detail ? ["capabilities"] : []),
  ]);
  const ref = object(row.ref, ["type", "id"]);
  if (ref.type !== kind) throw invalid();
  const category = kind === "component" ? row.type : row.classification;
  const status = kind === "component" ? row.lifecycle : row.status;
  const categories: readonly unknown[] =
    kind === "component"
      ? componentTypes
      : ["development", "staging", "production", "other"];
  const states =
    kind === "component"
      ? ["planned", "active", "deprecated", "retired"]
      : ["active", "archived"];
  if (
    typeof category !== "string" ||
    !categories.includes(category) ||
    typeof status !== "string" ||
    !states.includes(status)
  )
    throw invalid();
  const ownerTeamID =
    row.owner_team_id === null ? null : requireID(row.owner_team_id, "tem_");
  if (ownerTeamID === null && (kind !== "component" || status !== "planned"))
    throw invalid();
  let canManage = false,
    canGrant = false,
    canRevoke = false;
  if (detail) {
    const cap = object(
      row.capabilities,
      kind === "component"
        ? []
        : ["can_manage_authorizations", "can_grant", "can_revoke"],
    );
    if (kind === "environment") {
      if (
        [cap.can_manage_authorizations, cap.can_grant, cap.can_revoke].some(
          (v) => typeof v !== "boolean",
        )
      )
        throw invalid();
      canManage = cap.can_manage_authorizations === true;
      canGrant = cap.can_grant === true;
      canRevoke = cap.can_revoke === true;
      if (
        (canManage && category !== "staging") ||
        canGrant !== (canManage && status === "active") ||
        canRevoke !== canManage
      )
        throw invalid();
    }
  }
  return {
    id: requireID(ref.id, kind === "component" ? "cmp_" : "env_"),
    kind,
    key: text(row.key),
    name: text(row.name),
    category,
    status,
    ownerTeamID,
    canManage,
    canGrant,
    canRevoke,
  };
}
function parseAuthorization(
  value: unknown,
  allowEmpty: boolean,
): AuthorizationMember {
  const row = object(value, ["user", "eligible", "authorization"]);
  const user = object(row.user, ["id", "display_name"]);
  if (typeof row.eligible !== "boolean") throw invalid();
  let authorization: AuthorizationState | null = null;
  if (row.authorization !== null) {
    const auth = object(row.authorization, ["id", "status"]);
    if (auth.status !== "active" && auth.status !== "revoked") throw invalid();
    authorization = { id: requireID(auth.id, "dpa_"), status: auth.status };
  } else if (!allowEmpty || !row.eligible) throw invalid();
  return {
    id: requireID(user.id, "usr_"),
    name: text(user.display_name),
    eligible: row.eligible,
    authorization,
  };
}
function environmentPath(environment: string) {
  return `/environments/${encodeURIComponent(requireID(environment, "env_"))}/deployment-authorizations`;
}
export const deliveryConfigurationClient = {
  async list(
    workspace: string,
    kind: DeliveryKind,
    after?: string,
    signal?: AbortSignal,
  ) {
    return parsePage(
      await request(
        workspace,
        `/${kind}s${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (v) => parseDeliveryObject(v, kind, false),
      after,
    );
  },
  async read(
    workspace: string,
    kind: DeliveryKind,
    id: string,
    signal?: AbortSignal,
  ) {
    requireID(id, kind === "component" ? "cmp_" : "env_");
    const result = parseDeliveryObject(
      await request(
        workspace,
        `/${kind}s/${encodeURIComponent(id)}/configuration`,
        "GET",
        undefined,
        signal,
      ),
      kind,
      true,
    );
    if (result.id !== id) throw invalid();
    return result;
  },
  async create(
    workspace: string,
    kind: DeliveryKind,
    body: Record<string, unknown>,
  ) {
    return parseDeliveryObject(
      await request(workspace, `/${kind}s`, "POST", body),
      kind,
      true,
    );
  },
  async authorizations(
    workspace: string,
    environment: string,
    after?: string,
    signal?: AbortSignal,
  ) {
    return parsePage(
      await request(
        workspace,
        `${environmentPath(environment)}${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (v) => parseAuthorization(v, false),
      after,
    );
  },
  async authorization(
    workspace: string,
    environment: string,
    user: string,
    signal?: AbortSignal,
  ) {
    const result = parseAuthorization(
      await request(
        workspace,
        `${environmentPath(environment)}/${encodeURIComponent(requireID(user, "usr_"))}`,
        "GET",
        undefined,
        signal,
      ),
      true,
    );
    if (result.id !== user) throw invalid();
    return result;
  },
  async changeAuthorization(
    workspace: string,
    environment: string,
    user: string,
    method: "PUT" | "DELETE",
    body: Record<string, unknown>,
  ) {
    const row = object(
      await request(
        workspace,
        `${environmentPath(environment)}/${encodeURIComponent(requireID(user, "usr_"))}`,
        method,
        body,
      ),
      ["user_id", "applied"],
    );
    if (row.user_id !== user || row.applied !== true) throw invalid();
  },
};
export type DeliveryConfigurationClient = typeof deliveryConfigurationClient;
