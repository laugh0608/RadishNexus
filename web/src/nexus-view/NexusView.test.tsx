import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NexusView } from "./NexusView";
import {
  decisionNexusViewFixture,
  emptyDecisionNexusViewFixture,
  failedCIRunNexusViewFixture,
  failedDeploymentNexusViewFixture,
  succeededCIRunNexusViewFixture,
  succeededDeploymentNexusViewFixture,
} from "./fixture";

describe("NexusView", () => {
  it("renders a loading state without presenting stale content", () => {
    render(<NexusView state={{ status: "loading" }} />);

    expect(screen.getByRole("status").textContent).toContain(
      "正在加载 Nexus View",
    );
    expect(screen.queryByRole("heading", { level: 1 })).toBeNull();
  });

  it("renders a recoverable error state", () => {
    const onRetry = vi.fn();
    render(
      <NexusView
        state={{ status: "error", message: "读取失败，请稍后重试。" }}
        onRetry={onRetry}
      />,
    );

    expect(screen.getByRole("alert").textContent).toContain("读取失败");
    fireEvent.click(screen.getByRole("button", { name: "重新载入" }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it("renders explicit empty states for relations and timeline", () => {
    render(
      <NexusView
        state={{ status: "ready", data: emptyDecisionNexusViewFixture }}
      />,
    );

    expect(
      screen.getByRole("heading", { name: "暂无关联上下文" }),
    ).toBeDefined();
    expect(screen.getByRole("heading", { name: "暂无可见动态" })).toBeDefined();
  });

  it("renders readable context and a generic restricted placeholder", () => {
    render(
      <NexusView state={{ status: "ready", data: decisionNexusViewFixture }} />,
    );

    expect(
      screen.getByRole("heading", {
        name: "首期认证先采用本地账号，并为 OIDC 保留验证边界",
      }),
    ).toBeDefined();
    expect(screen.getByRole("heading", { name: "Relations" })).toBeDefined();
    expect(screen.getByRole("heading", { name: "Timeline" })).toBeDefined();
    expect(screen.getByText("entity://thread/thr_01JZ7CONTEXT")).toBeDefined();
    expect(
      screen.getByText("entity://ticket/tkt_01JZ7IMPLEMENT"),
    ).toBeDefined();
    expect(screen.getAllByText("受限对象")).toHaveLength(1);
    expect(screen.getAllByText("受限动态")).toHaveLength(1);
    expect(screen.queryByText(/restricted:|secret:/iu)).toBeNull();
  });

  it("renders the succeeded CI Run safe projection with exactly one Timeline item", () => {
    const { container } = render(
      <NexusView
        state={{ status: "ready", data: succeededCIRunNexusViewFixture }}
      />,
    );

    expect(screen.getByRole("heading", { name: "CI Run" })).toBeDefined();
    expect(screen.getByText("构建成功")).toBeDefined();
    expect(screen.getByText("Identity Service")).toBeDefined();
    expect(screen.getByText("entity://component/cmp_identity")).toBeDefined();
    expect(
      screen.getByText("entity://ci-run/cir_01K3RADISHNEXUS"),
    ).toBeDefined();
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByText("受控自动化 · ci-run.recorded")).toBeDefined();

    const renderedText = container.textContent ?? "";
    expect(renderedText).not.toMatch(
      /jenkins|source[_ -]?id|external|receipt|digest|https?:\/\//iu,
    );
  });

  it("renders a failed CI Run without implying a Deployment", () => {
    const { container } = render(
      <NexusView
        state={{ status: "ready", data: failedCIRunNexusViewFixture }}
      />,
    );

    expect(screen.getByText("构建失败")).toBeDefined();
    expect(screen.getByText("CI Run 已记录为失败")).toBeDefined();
    expect(container.textContent).toContain("不会自动创建 Deployment");
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
  });

  it("keeps prohibited CI source fields out of the representative fixtures", () => {
    const fixtures = JSON.stringify([
      succeededCIRunNexusViewFixture,
      failedCIRunNexusViewFixture,
    ]);

    expect(fixtures).not.toMatch(
      /jenkins|sourceId|externalRunKey|deliveryId|receipt|digest|secret|url/iu,
    );
  });

  it("renders an explicit staging Deployment without implying execution", () => {
    const { container } = render(
      <NexusView
        state={{ status: "ready", data: succeededDeploymentNexusViewFixture }}
      />,
    );

    expect(screen.getByRole("heading", { name: "Deployment" })).toBeDefined();
    expect(screen.getByText("部署成功")).toBeDefined();
    expect(screen.getByText("Staging")).toBeDefined();
    expect(screen.getByText("entity://environment/env_staging")).toBeDefined();
    expect(
      screen.getAllByText("entity://ci-run/cir_01K3RADISHNEXUS"),
    ).toHaveLength(2);
    expect(
      screen.getByText("entity://deployment/dpl_01K3RADISHNEXUS"),
    ).toBeDefined();
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByText("萝卜 · deployment.recorded")).toBeDefined();
    expect(container.textContent).toContain(
      "不表示 RadishNexus 执行了外部部署",
    );
  });

  it("keeps a failed Deployment distinct from its succeeded source build", () => {
    const { container } = render(
      <NexusView
        state={{ status: "ready", data: failedDeploymentNexusViewFixture }}
      />,
    );

    expect(screen.getByText("部署失败")).toBeDefined();
    expect(screen.getByText("staging Deployment 已记录为失败")).toBeDefined();
    expect(container.textContent).toContain("来源构建已经成功");
    expect(container.textContent).toContain("CI Run 仍保持自己的成功构建事实");
  });

  it("keeps authorization and CI source fields out of Deployment fixtures", () => {
    const fixtures = JSON.stringify([
      succeededDeploymentNexusViewFixture,
      failedDeploymentNexusViewFixture,
    ]);

    expect(fixtures).not.toMatch(
      /authorization|jenkins|sourceId|externalRunKey|deliveryId|receipt|digest|secret|url/iu,
    );
  });
});
