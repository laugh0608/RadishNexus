import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { FoundationCreate, ConfigurationLauncher } from "./ConfigurationPanel";
import {
  configurationClient as api,
  type ConfigurationObject,
} from "./configuration-api";
import { AuthRequestError } from "../auth/api";

const project: ConfigurationObject = {
  id: "prj_created",
  kind: "project",
  name: "Project",
  key: "project",
  status: "active",
  visibility: "restricted",
  canManage: true,
};
afterEach(() => vi.restoreAllMocks());
it("creates a Project only after explicit initial administrator selection and preserves ambiguous retry identity", async () => {
  vi.spyOn(api, "teams").mockResolvedValue({
    items: [{ id: "tem_main", name: "Team" }],
    nextCursor: null,
  });
  const create = vi
    .spyOn(api, "createProject")
    .mockRejectedValueOnce(new AuthRequestError("请求结果尚未确认"))
    .mockResolvedValue(project);
  const created = vi.fn();
  render(
    <FoundationCreate
      workspaceID="wrk_main"
      userID="usr_owner"
      onCreated={created}
      onSessionExpired={vi.fn()}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "创建团队与项目" }));
  await screen.findByRole("option", { name: "Team · tem_main" });
  fireEvent.change(screen.getByLabelText("责任团队"), {
    target: { value: "tem_main" },
  });
  fireEvent.change(screen.getByLabelText("项目名称"), {
    target: { value: "Project" },
  });
  fireEvent.change(screen.getByLabelText("项目短标识"), {
    target: { value: "project" },
  });
  expect(
    (screen.getByRole("button", { name: "创建项目" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "创建项目" }));
  await screen.findByText("请求结果尚未确认");
  fireEvent.click(screen.getByRole("button", { name: "创建项目" }));
  await waitFor(() => expect(created).toHaveBeenCalledWith(project));
  expect(create.mock.calls[0]![1]).toEqual(create.mock.calls[1]![1]);
  expect(create.mock.calls[0]![1]).toMatchObject({
    initial_admin_user_id: "usr_owner",
    owner_team_id: "tem_main",
    visibility: "restricted",
  });
});
it("shows readonly configuration without loading a privileged member directory", async () => {
  vi.spyOn(api, "read").mockResolvedValue({ ...project, canManage: false });
  const members = vi.spyOn(api, "members");
  render(
    <ConfigurationLauncher
      workspaceID="wrk_main"
      userID="usr_viewer"
      kind="project"
      id="prj_created"
      onSessionExpired={vi.fn()}
      navigate={vi.fn()}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "项目配置" }));
  await screen.findByText("当前没有此项配置权限。");
  expect(members).not.toHaveBeenCalled();
});
it("clears member data when a refreshed configuration is no longer accessible", async () => {
  vi.spyOn(api, "read")
    .mockResolvedValueOnce(project)
    .mockRejectedValue(new AuthRequestError("项目已不可访问", { status: 404 }));
  vi.spyOn(api, "members").mockResolvedValue({
    items: [
      { id: "usr_member", name: "Member", role: "contributor", eligible: true },
    ],
    nextCursor: null,
  });
  render(
    <ConfigurationLauncher
      workspaceID="wrk_main"
      userID="usr_owner"
      kind="project"
      id="prj_created"
      onSessionExpired={vi.fn()}
      navigate={vi.fn()}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "项目配置" }));
  await screen.findByText("Member · usr_member");
  fireEvent.click(screen.getByRole("button", { name: "刷新配置" }));
  await screen.findByText("项目已不可访问");
  expect(screen.queryByText("Member · usr_member")).toBeNull();
});
