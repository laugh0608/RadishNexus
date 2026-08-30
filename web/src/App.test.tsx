import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./App";

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
