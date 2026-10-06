import { afterEach, expect, it, vi } from "vitest";
import { repositoryConfigurationClient as api } from "./repository-configuration-api";

const repository = {
  ref: { type: "repository", id: "rep_a" },
  name: "Source",
  provider: "gitea",
  provider_origin: "https://git.example.test",
  external_id: "123",
  web_url: "https://git.example.test/team/source",
  default_branch: "main",
};
const response = (data: unknown) =>
  new Response(JSON.stringify({ data }), {
    headers: { "Content-Type": "application/json" },
  });
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("reads mapping metadata without fetching the external URL and requires the requested identity", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(response({ ...repository, capabilities: {} }))
    .mockResolvedValueOnce(
      response({
        ...repository,
        ref: { type: "repository", id: "rep_wrong" },
        capabilities: {},
      }),
    );
  vi.stubGlobal("fetch", fetch);
  expect(await api.read("wrk_main", "rep_a")).toMatchObject({
    id: "rep_a",
    externalID: "123",
    webURL: repository.web_url,
  });
  expect(fetch.mock.calls[0]).toEqual([
    "/api/v1/workspaces/wrk_main/repositories/rep_a/configuration",
    expect.objectContaining({ credentials: "same-origin", cache: "no-store" }),
  ]);
  await expect(api.read("wrk_main", "rep_a")).rejects.toThrow("契约");
  expect(fetch).toHaveBeenCalledTimes(2);
});

it("rejects unsafe URLs, credentials and unexpected private fields in projections", async () => {
  for (const bad of [
    { ...repository, web_url: "javascript:alert(1)" },
    {
      ...repository,
      web_url: "https://user:pass@git.example.test/team/source",
    },
    { ...repository, web_url: `${repository.web_url}?token=hidden` },
    { ...repository, web_url: `${repository.web_url}#` },
    { ...repository, web_url: "https://other.example.test/team/source" },
    { ...repository, secret: "hidden" },
    { ...repository, provider: "unknown" },
  ]) {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(response({ items: [bad], next_cursor: null })),
    );
    await expect(api.list("wrk_main")).rejects.toThrow("契约");
  }
});

it("validates ordered association targets and never accepts a mismatched unlink receipt", async () => {
  const link = { link_id: "lnk_a", target: repository, can_unlink: true };
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(response({ items: [link], next_cursor: null })),
  );
  const page = await api.componentRepositories("wrk_main", "cmp_main");
  expect(page.items[0]).toMatchObject({
    id: "rep_a",
    linkID: "lnk_a",
    canUnlink: true,
  });
  for (const items of [
    [link, link],
    [{ ...link, can_unlink: "yes" }],
    [{ ...link, link_id: "rep_wrong" }],
  ]) {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(response({ items, next_cursor: null })),
    );
    await expect(
      api.componentRepositories("wrk_main", "cmp_main"),
    ).rejects.toThrow("契约");
  }
});

it("sends confirmed commands to exact scoped routes with CSRF and validates the applied relation", async () => {
  // Test-only CSRF data; no provider credential or real session is used.
  const csrf = "c".repeat(43);
  vi.spyOn(document, "cookie", "get").mockReturnValue(
    `__Host-radishnexus-csrf=${csrf}`,
  );
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(response({ link_id: "lnk_a", applied: true }))
    .mockResolvedValueOnce(response({ link_id: "lnk_wrong", applied: true }));
  vi.stubGlobal("fetch", fetch);
  const body = {
    client_operation_id: "link",
    repository_id: "rep_a",
    confirmed: true,
  };
  expect(await api.link("wrk_main", "cmp_main", body)).toBe("lnk_a");
  expect(fetch.mock.calls[0]).toEqual([
    "/api/v1/workspaces/wrk_main/components/cmp_main/repositories",
    expect.objectContaining({
      method: "POST",
      body: JSON.stringify(body),
      headers: expect.objectContaining({ "X-CSRF-Token": csrf }),
    }),
  ]);
  await expect(
    api.unlink("wrk_main", "cmp_main", "lnk_a", {
      client_operation_id: "unlink",
      confirmed: true,
    }),
  ).rejects.toThrow("契约");
  expect(fetch.mock.calls[1]![0]).toBe(
    "/api/v1/workspaces/wrk_main/components/cmp_main/repository-links/lnk_a",
  );
});
