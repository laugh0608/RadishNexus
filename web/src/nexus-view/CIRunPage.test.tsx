import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { CIRunPage } from "./CIRunPage";
import { AuthRequestError } from "../auth/api";
import { succeededCIRunNexusViewFixture as data } from "./fixture";
import type { NexusViewData } from "./model";
const noop = () => {};
describe("canonical CI Run", () => {
  it("clears stale content before focus recheck and handles revocation", async () => {
    const load = vi
      .fn()
      .mockResolvedValueOnce(data)
      .mockRejectedValueOnce(
        new AuthRequestError("CI Run 不可用", { status: 404 }),
      );
    render(
      <CIRunPage
        workspaceID="wrk_main"
        ciRunID="cir_build"
        load={load}
        onSessionExpired={noop}
      />,
    );
    await screen.findByRole("heading", { name: "CI Run" });
    fireEvent(window, new Event("focus"));
    expect(screen.queryByRole("heading", { name: "CI Run" })).toBeNull();
    await screen.findByText("CI Run 不可用");
    expect(screen.queryByText(data.current.component.name)).toBeNull();
  });
  it("ignores aborted responses when a newer refresh finishes and allows retry", async () => {
    let resolve!: (v: NexusViewData) => void;
    const load = vi
      .fn()
      .mockImplementationOnce(
        () =>
          new Promise<NexusViewData>((r) => {
            resolve = r;
          }),
      )
      .mockRejectedValueOnce(new AuthRequestError("暂时失败"))
      .mockResolvedValue(data);
    render(
      <CIRunPage
        workspaceID="wrk_main"
        ciRunID="cir_build"
        load={load}
        onSessionExpired={noop}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "读取最新构建" }));
    await screen.findByText("暂时失败");
    await act(async () => resolve(data));
    expect(screen.queryByRole("heading", { name: "CI Run" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重新载入" }));
    await screen.findByRole("heading", { name: "CI Run" });
  });
  it("expires the session on 401", async () => {
    const expired = vi.fn();
    render(
      <CIRunPage
        workspaceID="wrk_main"
        ciRunID="cir_build"
        load={vi
          .fn()
          .mockRejectedValue(new AuthRequestError("expired", { status: 401 }))}
        onSessionExpired={expired}
      />,
    );
    await waitFor(() => expect(expired).toHaveBeenCalledOnce());
  });
});
