import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { RepositoryCIRuns } from "./RepositoryCIRuns";
import { AuthRequestError } from "../auth/api";

afterEach(cleanup);
it("clears checkout facts during refresh and permission loss, ", async () => {
  const row = {
    id: "cir_one",
    status: "succeeded" as const,
    completedAt: "2026-10-13T01:00:00Z",
    revision: { algorithm: "sha1" as const, oid: "a".repeat(40) },
  };
  const first = { items: [row], nextCursor: null };
  const load = vi
    .fn()
    .mockResolvedValueOnce(first)
    .mockRejectedValueOnce(
      new AuthRequestError("不可读取", { code: "not_found", status: 404 }),
    );
  render(
    <RepositoryCIRuns
      workspaceID="wrk_main"
      repositoryID="rep_source"
      onSessionExpired={vi.fn()}
      load={load}
    />,
  );
  expect(await screen.findByText(row.revision.oid)).toBeTruthy();
  expect(
    screen.getByRole("link", { name: "查看构建 cir_one" }).getAttribute("href"),
  ).toBe("/workspaces/wrk_main/ci-runs/cir_one");
  fireEvent.click(screen.getByRole("button", { name: "刷新" }));
  expect(screen.queryByText(row.revision.oid)).toBeNull();
  await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
  expect(screen.queryByRole("link", { name: "查看构建 cir_one" })).toBeNull();
});
