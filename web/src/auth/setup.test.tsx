import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SetupGate } from "./SetupGate";
import { browserSetupClient, type SetupClient } from "./setup-api";
import { AuthRequestError } from "./api";

afterEach(() => vi.unstubAllGlobals());
function show(client: SetupClient) {
  render(
    <SetupGate client={client}>
      <h1>正式登录</h1>
    </SetupGate>,
  );
}
function fill() {
  for (const [label, value] of [
    ["初始化码", "A".repeat(43)],
    ["管理员邮箱", "first@example.test"],
    ["管理员称呼", "First"],
    ["管理员密码", "synthetic password here"],
    ["工作区名称", "Workspace"],
  ])
    fireEvent.change(screen.getByLabelText(label!), { target: { value } });
}
it("creates once and clears credentials before showing login", async () => {
  const client: SetupClient = {
    status: vi
      .fn()
      .mockResolvedValueOnce("required")
      .mockResolvedValue("complete"),
    complete: vi.fn().mockResolvedValue(undefined),
  };
  show(client);
  await screen.findByLabelText("初始化码");
  fill();
  const button = screen.getByRole("button", { name: "创建管理员与工作区" });
  fireEvent.click(button);
  fireEvent.click(button);
  await screen.findByRole("heading", { name: "正式登录" });
  expect(client.complete).toHaveBeenCalledTimes(1);
  expect(screen.queryByLabelText("管理员密码")).toBeNull();
  expect(screen.queryByLabelText("初始化码")).toBeNull();
});
it("does not retry ambiguous writes and rechecks authoritative state", async () => {
  const client: SetupClient = {
    status: vi
      .fn()
      .mockResolvedValueOnce("required")
      .mockResolvedValue("complete"),
    complete: vi.fn().mockRejectedValue(new AuthRequestError("结果不明确")),
  };
  show(client);
  await screen.findByLabelText("初始化码");
  fill();
  fireEvent.click(screen.getByRole("button", { name: "创建管理员与工作区" }));
  await screen.findByText("结果不明确");
  expect((screen.getByLabelText("管理员密码") as HTMLInputElement).value).toBe(
    "",
  );
  expect(
    (
      screen.getByRole("button", {
        name: "创建管理员与工作区",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "重新检查状态" }));
  await screen.findByRole("heading", { name: "正式登录" });
  expect(client.complete).toHaveBeenCalledTimes(1);
});
it("fails closed on status errors and unavailable configuration", async () => {
  const client: SetupClient = {
    status: vi
      .fn()
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValue("unavailable"),
    complete: vi.fn(),
  };
  show(client);
  await screen.findByRole("alert");
  expect(screen.queryByText("正式登录")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "重新检查状态" }));
  await screen.findByText(/请部署者配置一次性初始化码/);
  expect(screen.queryByLabelText("初始化码")).toBeNull();
  expect(client.complete).not.toHaveBeenCalled();
});
it("ignores a late status from an obsolete client", async () => {
  let resolve!: (value: "required") => void;
  const old: SetupClient = {
    status: () =>
      new Promise((r) => {
        resolve = r;
      }),
    complete: vi.fn(),
  };
  const current: SetupClient = {
    status: async () => "complete",
    complete: vi.fn(),
  };
  const view = render(
    <SetupGate client={old}>
      <h1>正式登录</h1>
    </SetupGate>,
  );
  view.rerender(
    <SetupGate client={current}>
      <h1>正式登录</h1>
    </SetupGate>,
  );
  await screen.findByText("正式登录");
  resolve("required");
  await waitFor(() => expect(screen.queryByLabelText("初始化码")).toBeNull());
});
it("strictly validates status and sends credentials only in same-origin POST body", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      new Response(
        JSON.stringify({ status: "required", setup_code: "must not expose" }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ status: "complete" }), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
    );
  vi.stubGlobal("fetch", fetcher);
  await expect(browserSetupClient.status()).rejects.toThrow("无法识别");
  await browserSetupClient.complete({
    setup_code: "synthetic",
    email: "first@example.test",
    password: "synthetic password",
    display_name: "First",
    workspace_name: "Workspace",
  });
  expect(fetcher.mock.calls[1]![0]).toBe("/api/v1/setup");
  expect(fetcher.mock.calls[1]![1]).toMatchObject({
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
  });
  expect(fetcher.mock.calls[1]![1].headers).not.toHaveProperty("Authorization");
});

it("rejects an array masquerading as a setup status", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ status: ["required"] }), {
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
  await expect(browserSetupClient.status()).rejects.toThrow("无法识别");
});
