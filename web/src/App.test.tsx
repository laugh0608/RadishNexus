import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { App } from "./App";
import { DeploymentNexusViewLoadError } from "./nexus-view/api";
import { succeededDeploymentNexusViewFixture } from "./nexus-view/fixture";

describe("App prototype state controls", () => {
  it("allows keyboard-operable inspection of succeeded, failed, loading, and error states", () => {
    render(<App />);

    expect(screen.getByText("部署成功")).toBeDefined();

    const failedButton = screen.getByRole("button", { name: "失败" });
    fireEvent.click(failedButton);
    expect(failedButton.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText("部署失败")).toBeDefined();

    const loadingButton = screen.getByRole("button", { name: "加载" });
    fireEvent.click(loadingButton);
    expect(loadingButton.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("status")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "错误" }));
    expect(screen.getByRole("alert")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "重新载入" }));
    expect(screen.getByText("部署成功")).toBeDefined();
  });
});

describe("App live Deployment route", () => {
  it("loads a canonical Deployment page through the typed adapter boundary", async () => {
    const loader = vi
      .fn()
      .mockResolvedValue(succeededDeploymentNexusViewFixture);

    render(
      <App
        pathname="/workspaces/wrk_radish/deployments/dpl_release_42"
        loadDeployment={loader}
      />,
    );

    expect(screen.getByRole("status")).toBeDefined();
    expect(screen.getByText("真实 API · 当前权限过滤")).toBeDefined();
    expect(screen.queryByLabelText("原型状态检视")).toBeNull();
    expect(await screen.findByText("部署成功")).toBeDefined();
    expect(loader).toHaveBeenCalledWith(
      "wrk_radish",
      "dpl_release_42",
      expect.any(AbortSignal),
    );
  });

  it("shows a safe failure and retries the same scoped read", async () => {
    const loader = vi
      .fn()
      .mockRejectedValueOnce(
        new DeploymentNexusViewLoadError(
          "该 Deployment 不存在，或你没有读取它的权限。",
          404,
        ),
      )
      .mockResolvedValueOnce(succeededDeploymentNexusViewFixture);

    render(
      <App
        pathname="/workspaces/wrk_radish/deployments/dpl_release_42"
        loadDeployment={loader}
      />,
    );

    expect(
      await screen.findByText("该 Deployment 不存在，或你没有读取它的权限。"),
    ).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "重新载入" }));

    await waitFor(() => expect(loader).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("部署成功")).toBeDefined();
  });
});
