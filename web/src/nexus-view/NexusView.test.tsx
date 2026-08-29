import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NexusView } from "./NexusView";
import {
  decisionNexusViewFixture,
  emptyDecisionNexusViewFixture,
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
    expect(screen.getByText("thread:thr_01JZ7CONTEXT")).toBeDefined();
    expect(screen.getByText("ticket:tic_01JZ7IMPLEMENT")).toBeDefined();
    expect(screen.getAllByText("受限对象")).toHaveLength(1);
    expect(screen.getAllByText("受限动态")).toHaveLength(1);
    expect(screen.queryByText(/restricted:|secret:/iu)).toBeNull();
  });
});
