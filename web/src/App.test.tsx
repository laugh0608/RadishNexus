import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./App";

describe("App prototype state controls", () => {
  it("allows keyboard-operable inspection of loading, empty, and error states", () => {
    render(<App />);

    const loadingButton = screen.getByRole("button", { name: "加载" });
    fireEvent.click(loadingButton);
    expect(loadingButton.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("status")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "空态" }));
    expect(
      screen.getByRole("heading", { name: "暂无关联上下文" }),
    ).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "错误" }));
    expect(screen.getByRole("alert")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "重新载入" }));
    expect(screen.getByRole("heading", { name: "Relations" })).toBeDefined();
  });
});
