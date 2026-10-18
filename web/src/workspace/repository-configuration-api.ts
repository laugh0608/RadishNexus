import { AuthRequestError } from "../auth/api";
import { object, requireID } from "./api";
import { request, text, query, parsePage } from "./configuration-api";
import { parseDeliveryObject } from "./delivery-configuration-api";

export interface RepositoryObject {
  id: string;
  kind: "repository";
  name: string;
  provider: "github" | "gitlab" | "gitea";
  providerOrigin: string;
  externalID: string;
  webURL: string;
  defaultBranch: string;
}
export interface RepositoryAssociation<T> {
  id: string;
  linkID: string;
  target: T;
  canUnlink: boolean;
}
const invalid = () =>
  new AuthRequestError("代码库配置响应不符合当前契约，请刷新或联系管理员。");

function parseRepository(value: unknown, detail: boolean): RepositoryObject {
  const row = object(value, [
    "ref",
    "name",
    "provider",
    "provider_origin",
    "external_id",
    "web_url",
    "default_branch",
    ...(detail ? ["capabilities"] : []),
  ]);
  const ref = object(row.ref, ["type", "id"]);
  if (
    ref.type !== "repository" ||
    !["github", "gitlab", "gitea"].includes(String(row.provider))
  )
    throw invalid();
  if (detail) object(row.capabilities, []);
  const webURL = text(row.web_url),
    providerOrigin = text(row.provider_origin);
  // This is a rendering boundary, not an alternative write contract. Never
  // render an unsafe protocol or a URL carrying credentials from a bad response.
  let url: URL, origin: URL;
  try {
    url = new URL(webURL);
    origin = new URL(providerOrigin);
  } catch {
    throw invalid();
  }
  if (
    url.protocol !== "https:" ||
    url.username ||
    url.password ||
    url.origin !== providerOrigin ||
    origin.href !== `${providerOrigin}/` ||
    url.pathname === "/" ||
    /[\\?#\s]/.test(webURL) ||
    Array.from(webURL).some(
      (c) => c.charCodeAt(0) < 32 || c.charCodeAt(0) === 127,
    ) ||
    url.href !== webURL
  )
    throw invalid();
  return {
    id: requireID(ref.id, "rep_"),
    kind: "repository",
    name: text(row.name),
    provider: row.provider as RepositoryObject["provider"],
    providerOrigin,
    externalID: text(row.external_id),
    webURL,
    defaultBranch: text(row.default_branch),
  };
}

function parseAssociation<T extends { id: string }>(
  value: unknown,
  parse: (v: unknown) => T,
): RepositoryAssociation<T> {
  const row = object(value, ["link_id", "target", "can_unlink"]);
  if (typeof row.can_unlink !== "boolean") throw invalid();
  const target = parse(row.target);
  return {
    id: target.id,
    linkID: requireID(row.link_id, "lnk_"),
    target,
    canUnlink: row.can_unlink,
  };
}
function repositoryPath(id: string) {
  return `/repositories/${encodeURIComponent(requireID(id, "rep_"))}`;
}
function componentPath(id: string) {
  return `/components/${encodeURIComponent(requireID(id, "cmp_"))}`;
}
function applied(value: unknown, expected?: string) {
  const row = object(value, ["link_id", "applied"]);
  const id = requireID(row.link_id, "lnk_");
  if (row.applied !== true || (expected !== undefined && id !== expected))
    throw invalid();
  return id;
}

export const repositoryConfigurationClient = {
  async list(workspace: string, after?: string, signal?: AbortSignal) {
    return parsePage(
      await request(
        workspace,
        `/repositories${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (v) => parseRepository(v, false),
      after,
    );
  },
  async read(workspace: string, id: string, signal?: AbortSignal) {
    const result = parseRepository(
      await request(
        workspace,
        `${repositoryPath(id)}/configuration`,
        "GET",
        undefined,
        signal,
      ),
      true,
    );
    if (result.id !== id) throw invalid();
    return result;
  },
  async create(workspace: string, body: Record<string, unknown>) {
    return parseRepository(
      await request(workspace, "/repositories", "POST", body),
      false,
    );
  },
  async componentRepositories(
    workspace: string,
    component: string,
    after?: string,
    signal?: AbortSignal,
  ) {
    return parsePage(
      await request(
        workspace,
        `${componentPath(component)}/repositories${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (v) => parseAssociation(v, (r) => parseRepository(r, false)),
      after,
    );
  },
  async repositoryComponents(
    workspace: string,
    repository: string,
    after?: string,
    signal?: AbortSignal,
  ) {
    return parsePage(
      await request(
        workspace,
        `${repositoryPath(repository)}/components${query(after)}`,
        "GET",
        undefined,
        signal,
      ),
      (v) =>
        parseAssociation(v, (c) => parseDeliveryObject(c, "component", false)),
      after,
    );
  },
  async link(
    workspace: string,
    component: string,
    body: Record<string, unknown>,
  ) {
    return applied(
      await request(
        workspace,
        `${componentPath(component)}/repositories`,
        "POST",
        body,
      ),
    );
  },
  async unlink(
    workspace: string,
    component: string,
    link: string,
    body: Record<string, unknown>,
  ) {
    requireID(link, "lnk_");
    return applied(
      await request(
        workspace,
        `${componentPath(component)}/repository-links/${encodeURIComponent(link)}`,
        "DELETE",
        body,
      ),
      link,
    );
  },
};
export type RepositoryConfigurationClient =
  typeof repositoryConfigurationClient;
