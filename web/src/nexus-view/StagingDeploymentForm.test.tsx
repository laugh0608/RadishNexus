import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { StagingDeploymentForm } from "./StagingDeploymentForm";
import { AuthRequestError } from "../auth/api";
import { type StagingClient, type StagingResult } from "./staging-api";
const target = {
  ref: { type: "environment" as const, id: "env_stage" },
  name: "Staging",
  key: "STAGE",
  classification: "staging" as const,
};
const result: StagingResult = {
  deployment: { type: "deployment", id: "dpl_one" },
  duplicate: false,
};
function setup(client: StagingClient, available = true) {
  const navigate = vi.fn(),
    expired = vi.fn();
  const props = {
    workspaceID: "wrk_main",
    ciRunID: "cir_build",
    available,
    navigate,
    onSessionExpired: expired,
    client,
  };
  const view = render(<StagingDeploymentForm {...props} />);
  return { ...view, navigate, expired, props };
}
async function fill() {
  fireEvent.click(
    screen.getByRole("button", { name: "记录 staging 部署结果" }),
  );
  await screen.findByRole("option", { name: "Staging（STAGE）" });
  fireEvent.change(screen.getByLabelText("目标环境"), {
    target: { value: "env_stage" },
  });
  fireEvent.change(screen.getByLabelText("部署结果"), {
    target: { value: "failed" },
  });
  fireEvent.change(screen.getByLabelText("完成时间"), {
    target: { value: "2026-01-01T12:00" },
  });
}
function client() {
  return {
    targets: vi.fn().mockResolvedValue({ items: [target], next_cursor: null }),
    record: vi.fn().mockResolvedValue(result),
  };
}
describe("staging recording", () => {
  it("requires explicit result and confirmation, resets confirmation on changes, navigates after success", async () => {
    const c = client(),
      { navigate } = setup(c);
    await fill();
    expect(
      (screen.getByRole("button", { name: "确认记录" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.change(screen.getByLabelText("部署结果"), {
      target: { value: "canceled" },
    });
    expect((screen.getByRole("checkbox") as HTMLInputElement).checked).toBe(
      false,
    );
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "确认记录" }));
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith(
        "/workspaces/wrk_main/deployments/dpl_one",
      ),
    );
    expect(c.record.mock.calls[0]![2]).toMatchObject({
      status: "canceled",
      confirmed: true,
      started_at: null,
    });
  });
  it("freezes an uncertain operation across focus rechecks and reuses exact payload", async () => {
    const c = client();
    c.record.mockRejectedValueOnce(new AuthRequestError("未知结果"));
    const { rerender, props } = setup(c);
    await fill();
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "确认记录" }));
    await screen.findByText("未知结果");
    expect(screen.queryByLabelText("部署结果")).toBeNull();
    rerender(<StagingDeploymentForm {...props} available={false} />);
    expect(screen.queryByRole("button", { name: "重试原请求" })).toBeNull();
    rerender(<StagingDeploymentForm {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "重试原请求" }));
    await waitFor(() => expect(c.record).toHaveBeenCalledTimes(2));
    expect(c.record.mock.calls[1]![2]).toEqual(c.record.mock.calls[0]![2]);
  });
  it("blocks duplicate clicks during sending and expires sessions", async () => {
    const c = client();
    let reject!: (e: unknown) => void;
    c.record.mockImplementation(
      () =>
        new Promise((_, r) => {
          reject = r;
        }),
    );
    const { expired } = setup(c);
    await fill();
    fireEvent.click(screen.getByRole("checkbox"));
    const b = screen.getByRole("button", { name: "确认记录" });
    fireEvent.click(b);
    fireEvent.click(b);
    expect(c.record).toHaveBeenCalledOnce();
    await act(async () =>
      reject(new AuthRequestError("expired", { status: 401 })),
    );
    expect(expired).toHaveBeenCalledOnce();
  });
  it("clears selection and confirmation while paging and handles revoked targets", async () => {
    const c = client();
    c.targets
      .mockResolvedValueOnce({ items: [target], next_cursor: "next" })
      .mockResolvedValue({ items: [], next_cursor: null });
    setup(c);
    await fill();
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "下一页环境" }));
    await screen.findByText("当前没有可选的已授权 staging 环境。");
    expect((screen.getByRole("checkbox") as HTMLInputElement).checked).toBe(
      false,
    );
    expect(c.targets.mock.calls[1]![2]).toBe("next");
    expect(c.record).not.toHaveBeenCalled();
  });
  it("does not offer actions for unavailable source and cancels stale reads", async () => {
    const c = client();
    const view = setup(c, false);
    expect(screen.queryByText("记录 staging 部署结果")).toBeNull();
    expect(c.targets).not.toHaveBeenCalled();
    view.rerender(<StagingDeploymentForm {...view.props} available />);
    await fill();
    view.rerender(<StagingDeploymentForm {...view.props} available={false} />);
    expect(screen.queryByLabelText("目标环境")).toBeNull();
  });
  it("shows load failure and allows retry without defaulting to success", async () => {
    const c = client();
    c.targets.mockRejectedValueOnce(new Error("读取失败"));
    setup(c);
    fireEvent.click(
      screen.getByRole("button", { name: "记录 staging 部署结果" }),
    );
    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "重新读取环境" }));
    await screen.findByRole("option", { name: "Staging（STAGE）" });
    expect((screen.getByLabelText("部署结果") as HTMLSelectElement).value).toBe(
      "",
    );
    expect(c.record).not.toHaveBeenCalled();
  });
  it("ignores a late write result after unmount", async () => {
    const c = client();
    let resolve!: (r: StagingResult) => void;
    c.record.mockImplementation(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );
    const { unmount, navigate } = setup(c);
    await fill();
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "确认记录" }));
    unmount();
    await act(async () => resolve(result));
    expect(navigate).not.toHaveBeenCalled();
  });
});
