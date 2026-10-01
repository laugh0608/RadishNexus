import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AuthRequestError } from "../auth/api";
import { EnvironmentAuthorizationEditor } from "./EnvironmentAuthorizationEditor";
import { DeliveryConfigurationPanel } from "./DeliveryConfigurationPanel";
import { configurationClient } from "./configuration-api";
import type { DeliveryObject } from "./delivery-configuration-api";

const environment: DeliveryObject = {
  id: "env_stage",
  kind: "environment",
  key: "stage",
  name: "Stage",
  category: "staging",
  status: "active",
  ownerTeamID: "tem_main",
  canManage: true,
  canGrant: true,
  canRevoke: true,
};
function client() {
  return {
    list: vi.fn().mockResolvedValue({ items: [environment], nextCursor: null }),
    read: vi.fn().mockResolvedValue(environment),
    create: vi.fn().mockResolvedValue(environment),
    authorizations: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
    authorization: vi.fn().mockResolvedValue({
      id: "usr_owner",
      name: "Owner",
      eligible: true,
      authorization: null,
    }),
    changeAuthorization: vi.fn().mockResolvedValue(undefined),
  };
}
function setupEditor(c = client()) {
  const props = {
    workspaceID: "wrk_main",
    workspaceName: "Main",
    environment,
    userID: "usr_owner",
    currentUserID: "usr_owner",
    client: c,
    onChanged: vi.fn(),
    onSessionExpired: vi.fn(),
    onUnavailable: vi.fn(),
  };
  return { ...render(<EnvironmentAuthorizationEditor {...props} />), props, c };
}
afterEach(() => vi.restoreAllMocks());
it("requires an explicit self grant, freezes an uncertain request and retries with the same identity", async () => {
  const c = client();
  c.changeAuthorization.mockRejectedValueOnce(new AuthRequestError("未知结果"));
  const { props } = setupEditor(c);
  await screen.findByText("Owner · usr_owner · 你本人");
  expect(
    (screen.getByRole("button", { name: "授予记录权" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "授予记录权" }));
  await screen.findByText("未知结果");
  expect(c.changeAuthorization.mock.calls[0]![4]).toMatchObject({
    expected_authorization: null,
    confirmed: true,
  });
  fireEvent.focus(window);
  expect(c.authorization).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button", { name: "按原请求重试" }));
  await waitFor(() => expect(props.onChanged).toHaveBeenCalledTimes(1));
  expect(c.changeAuthorization.mock.calls[1]).toEqual(
    c.changeAuthorization.mock.calls[0],
  );
  expect(c.authorization).toHaveBeenCalledTimes(2);
});
it("prevents duplicate submissions and routes current permission failures to scope clearing", async () => {
  const c = client();
  let reject!: (value: unknown) => void;
  c.changeAuthorization.mockImplementation(
    () =>
      new Promise((_, fail) => {
        reject = fail;
      }),
  );
  const { props } = setupEditor(c);
  await screen.findByRole("checkbox");
  fireEvent.click(screen.getByRole("checkbox"));
  const button = screen.getByRole("button", { name: "授予记录权" });
  fireEvent.click(button);
  fireEvent.click(button);
  expect(c.changeAuthorization).toHaveBeenCalledTimes(1);
  await act(async () =>
    reject(new AuthRequestError("配置权已撤销", { status: 403 })),
  );
  expect(props.onUnavailable).toHaveBeenCalledOnce();
  expect(props.onChanged).not.toHaveBeenCalled();
});
it("cleans an archived environment grant and retains the exact observed generation", async () => {
  const c = client();
  const observed = { id: "dpa_second", status: "active" };
  c.authorization.mockResolvedValue({
    id: "usr_owner",
    name: "Owner",
    eligible: false,
    authorization: observed,
  });
  render(
    <EnvironmentAuthorizationEditor
      workspaceID="wrk_main"
      workspaceName="Main"
      environment={{ ...environment, status: "archived", canGrant: false }}
      userID="usr_owner"
      currentUserID="usr_owner"
      client={c}
      onChanged={vi.fn()}
      onSessionExpired={vi.fn()}
      onUnavailable={vi.fn()}
    />,
  );
  await screen.findByRole("checkbox");
  fireEvent.click(screen.getByRole("checkbox"));
  expect(
    (screen.getByRole("button", { name: "授予记录权" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "撤销记录权" }));
  await waitFor(() => expect(c.changeAuthorization).toHaveBeenCalledOnce());
  expect(c.changeAuthorization.mock.calls[0]!.slice(0, 4)).toEqual([
    "wrk_main",
    "env_stage",
    "usr_owner",
    "DELETE",
  ]);
  expect(c.changeAuthorization.mock.calls[0]![4]).toMatchObject({
    expected_authorization: observed,
  });
});
it("does not load privileged directories for ordinary members", async () => {
  const c = client();
  c.read.mockResolvedValue({
    ...environment,
    canManage: false,
    canGrant: false,
    canRevoke: false,
  });
  const members = vi.spyOn(configurationClient, "members");
  render(
    <DeliveryConfigurationPanel
      workspaceID="wrk_main"
      workspaceName="Main"
      userID="usr_reader"
      isOwner={false}
      client={c}
      onSessionExpired={vi.fn()}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "组件与环境" }));
  fireEvent.change(screen.getByLabelText("交付配置类型"), {
    target: { value: "environment" },
  });
  fireEvent.click(await screen.findByRole("button", { name: "Stage · stage" }));
  await screen.findByText("授权管理由当前 Workspace owner 操作。");
  expect(c.authorizations).not.toHaveBeenCalled();
  expect(members).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: "创建 staging Environment" }),
  ).toBeNull();
});
it("discards late authorization reads when the selected member is unmounted", async () => {
  const c = client();
  let resolve!: (value: unknown) => void;
  c.authorization.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  const { unmount } = setupEditor(c);
  const signal = c.authorization.mock.calls[0]![3] as AbortSignal;
  unmount();
  expect(signal.aborted).toBe(true);
  await act(async () =>
    resolve({
      id: "usr_owner",
      name: "Old Member",
      eligible: true,
      authorization: null,
    }),
  );
  expect(screen.queryByText("Old Member")).toBeNull();
});
