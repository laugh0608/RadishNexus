import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AuthRequestError } from "../auth/api";
import { ManagementPanel } from "./ManagementPanel";
import { managementClient as api } from "./management-api";
vi.mock("./management-api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./management-api")>()),
  managementClient: {
    members: vi.fn(),
    preview: vi.fn(),
    apply: vi.fn(),
    result: vi.fn(),
    audit: vi.fn(),
    responsibilities: vi.fn(),
  },
}));
const scope = {
  workspace: "wrk_main",
  kind: "workspace" as const,
  id: "wrk_main",
};
const page = {
  items: [
    {
      id: "usr_member",
      displayName: "Member",
      status: "active",
      role: "member",
      revision: "1",
      eligible: true,
    },
  ],
  nextCursor: null,
  canManage: true,
  selfRole: "owner",
  revision: "1",
};
const plan = {
  scope_kind: "workspace",
  scope_id: "wrk_main",
  action: "suspend",
  target_user_id: "usr_member",
  role: "",
  actor_after_role: "",
  actor_id: "usr_owner",
  target_name: "Member",
  before: "active",
  after: "suspended",
  actor_role: "owner",
  target_workspace_role: "member",
  target_status: "active",
  scope_revision: "1",
  actor_lifecycle_revision: "1",
  target_lifecycle_revision: "1",
  actor_role_revision: "1",
  target_role_revision: "1",
  parent_project_revision: "0",
  parent_channel_revision: "0",
  policy: "membership-management-v1",
  digest: "a".repeat(64),
} as const;
function show() {
  return render(
    <ManagementPanel
      scope={scope}
      userID="usr_owner"
      onSessionExpired={vi.fn()}
    />,
  );
}
async function open() {
  fireEvent.click(screen.getByRole("button", { name: "工作区成员与退出" }));
  await screen.findByText("Member");
}
async function preview() {
  fireEvent.click(screen.getByRole("button", { name: "暂停成员" }));
  await screen.findByRole("button", { name: "确认暂停成员" });
}
beforeEach(() => {
  vi.mocked(api.members).mockResolvedValue(page);
  vi.mocked(api.preview).mockResolvedValue(plan);
});
afterEach(() => {
  vi.clearAllMocks();
  sessionStorage.clear();
});
it("requires explicit preview confirmation and preserves the original uncertain operation", async () => {
  vi.mocked(api.apply).mockRejectedValue(
    new AuthRequestError("提交结果未确认", { status: 503 }),
  );
  show();
  await open();
  await preview();
  expect(
    (screen.getByRole("button", { name: "确认暂停成员" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  expect(api.apply).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "确认暂停成员" }));
  await screen.findByText("提交结果未确认");
  const original = vi.mocked(api.apply).mock.calls[0]![2];
  fireEvent.focus(window);
  await waitFor(() => expect(api.members).toHaveBeenCalledTimes(2));
  vi.mocked(api.result).mockResolvedValue({
    result: "not_observed",
    operationID: original.client_operation_id,
  });
  fireEvent.click(screen.getByRole("button", { name: "查询原操作结果" }));
  await screen.findByText(/这不证明正在执行的请求已回滚/);
  fireEvent.click(screen.getByRole("button", { name: "以原内容重试" }));
  await waitFor(() => expect(api.apply).toHaveBeenCalledTimes(2));
  expect(vi.mocked(api.apply).mock.calls[1]![2]).toEqual(original);
  const saved = sessionStorage.getItem("radishnexus.management.usr_owner")!;
  expect(saved).toContain(original.client_operation_id);
  expect(saved).not.toContain("plan_digest");
  expect(saved).not.toContain("usr_member");
});
it("clears stale confirmation after a version conflict", async () => {
  vi.mocked(api.apply).mockRejectedValue(
    new AuthRequestError("成员状态已变化", { status: 409 }),
  );
  show();
  await open();
  await preview();
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "确认暂停成员" }));
  await screen.findByText("成员状态已变化");
  expect(screen.queryByRole("button", { name: "确认暂停成员" })).toBeNull();
  expect(screen.queryByRole("button", { name: "以原内容重试" })).toBeNull();
});
it("clears private roster on restoration failure and ignores a late unmounted request", async () => {
  const mounted = show();
  await open();
  vi.mocked(api.members).mockRejectedValueOnce(
    new AuthRequestError("不可读取", { status: 404 }),
  );
  fireEvent.focus(window);
  await screen.findByText("不可读取");
  expect(screen.queryByText("Member")).toBeNull();
  let finish!: (value: typeof page) => void;
  vi.mocked(api.members).mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve;
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "重新读取成员" }));
  mounted.unmount();
  await act(async () => finish(page));
  expect(screen.queryByText("Member")).toBeNull();
});
