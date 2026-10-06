import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AuthRequestError } from "../auth/api";
import {
  ComponentRepositories,
  RepositoryBrowser,
} from "./RepositoryConfigurationPanel";
import type { RepositoryObject } from "./repository-configuration-api";
import type { DeliveryObject } from "./delivery-configuration-api";

const repository: RepositoryObject = {
  id: "rep_source",
  kind: "repository",
  name: "Source",
  provider: "gitea",
  providerOrigin: "https://git.example.test",
  externalID: "123",
  webURL: "https://git.example.test/team/source",
  defaultBranch: "main",
};
const component: DeliveryObject = {
  id: "cmp_service",
  kind: "component",
  key: "service",
  name: "Service",
  category: "service",
  status: "active",
  ownerTeamID: "tem_main",
  canManage: true,
  canGrant: false,
  canRevoke: false,
  canLinkRepository: true,
};
function client() {
  return {
    list: vi.fn().mockResolvedValue({ items: [repository], nextCursor: null }),
    read: vi.fn().mockResolvedValue(repository),
    create: vi.fn().mockResolvedValue(repository),
    componentRepositories: vi
      .fn()
      .mockResolvedValue({ items: [], nextCursor: null }),
    repositoryComponents: vi
      .fn()
      .mockResolvedValue({ items: [], nextCursor: null }),
    link: vi.fn().mockResolvedValue("lnk_first"),
    unlink: vi.fn().mockResolvedValue("lnk_first"),
  };
}
function context(c = client()) {
  return {
    workspaceID: "wrk_main",
    workspaceName: "Main",
    isOwner: true,
    repositoryClient: c,
    onSessionExpired: vi.fn(),
    onUnavailable: vi.fn(),
    onOpenComponent: vi.fn(),
    onOpenRepository: vi.fn(),
  };
}
afterEach(() => vi.restoreAllMocks());
it("retains a confirmed link request across focus refresh and retries its exact operation", async () => {
  const c = client();
  c.link.mockRejectedValueOnce(new AuthRequestError("结果未知"));
  render(<ComponentRepositories {...context(c)} component={component} />);
  fireEvent.change(await screen.findByLabelText("选择代码库"), {
    target: { value: repository.id },
  });
  expect(
    (screen.getByRole("button", { name: "确认关联" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "确认关联" }));
  await screen.findByText("结果未知");
  fireEvent.focus(window);
  await waitFor(() => expect(c.componentRepositories).toHaveBeenCalledTimes(2));
  fireEvent.click(
    await screen.findByRole("button", { name: "重试原关联请求" }),
  );
  await waitFor(() => expect(c.link).toHaveBeenCalledTimes(2));
  expect(c.link.mock.calls[1]).toEqual(c.link.mock.calls[0]);
  expect(c.link.mock.calls[0]![2]).toMatchObject({
    repository_id: repository.id,
    confirmed: true,
  });
});
it("retains the exact removed generation even if refresh shows a new relation", async () => {
  const c = client();
  c.componentRepositories.mockResolvedValue({
    items: [
      {
        id: repository.id,
        linkID: "lnk_first",
        target: repository,
        canUnlink: true,
      },
    ],
    nextCursor: null,
  });
  c.unlink.mockRejectedValueOnce(new AuthRequestError("解除结果未知"));
  render(<ComponentRepositories {...context(c)} component={component} />);
  fireEvent.click(await screen.findByRole("button", { name: "解除关联" }));
  fireEvent.click(screen.getByLabelText("确认解除这一条关联"));
  fireEvent.click(screen.getByRole("button", { name: "确认解除" }));
  await screen.findByText("解除结果未知");
  c.componentRepositories.mockResolvedValue({
    items: [
      {
        id: repository.id,
        linkID: "lnk_second",
        target: repository,
        canUnlink: true,
      },
    ],
    nextCursor: null,
  });
  fireEvent.focus(window);
  await waitFor(() => expect(c.componentRepositories).toHaveBeenCalledTimes(2));
  fireEvent.click(
    await screen.findByRole("button", { name: "重试原解除请求" }),
  );
  await waitFor(() => expect(c.unlink).toHaveBeenCalledTimes(2));
  expect(c.unlink.mock.calls[1]).toEqual(c.unlink.mock.calls[0]);
  expect(c.unlink.mock.calls[1]![2]).toBe("lnk_first");
});
it("freezes an uncertain mapping and clears inaccessible details after refresh", async () => {
  const c = client();
  c.create.mockRejectedValueOnce(new AuthRequestError("创建结果未知"));
  render(<RepositoryBrowser {...context(c)} />);
  fireEvent.click(
    await screen.findByRole("button", { name: "创建 Repository" }),
  );
  for (const [label, value] of [
    ["代码库名称", "Source"],
    ["代码托管服务", "gitea"],
    ["服务地址（HTTPS origin）", repository.providerOrigin],
    ["外部稳定 ID", "123"],
    ["仓库浏览地址", repository.webURL],
    ["默认分支", "main"],
  ]) {
    fireEvent.change(screen.getByLabelText(label!), { target: { value } });
  }
  fireEvent.click(screen.getByRole("button", { name: "创建映射" }));
  await screen.findByText("创建结果未知");
  expect(
    screen.getByLabelText("代码库名称").closest("fieldset")!.disabled,
  ).toBe(true);
  fireEvent.focus(window);
  fireEvent.click(
    await screen.findByRole("button", { name: "重试原创建请求" }),
  );
  const link = await screen.findByRole("link", { name: "打开外部仓库 ↗" });
  expect(link.getAttribute("rel")).toBe("noopener noreferrer");
  expect(link.getAttribute("referrerpolicy")).toBe("no-referrer");
  expect(c.create.mock.calls[1]).toEqual(c.create.mock.calls[0]);
  c.list.mockRejectedValue(new AuthRequestError("无权读取", { status: 404 }));
  fireEvent.focus(window);
  await screen.findByText("无权读取");
  expect(screen.queryByRole("link")).toBeNull();
});
it("uses read-only member capabilities and discards late detail responses", async () => {
  const c = client(),
    props = context(c);
  c.repositoryComponents.mockResolvedValue({
    items: [
      {
        id: component.id,
        linkID: "lnk_first",
        target: { ...component, canManage: false, canLinkRepository: false },
        canUnlink: false,
      },
    ],
    nextCursor: null,
  });
  const result = render(
    <RepositoryBrowser {...props} isOwner={false} initialID={repository.id} />,
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "Service · service" }),
  );
  expect(props.onOpenComponent).toHaveBeenCalledWith(component.id);
  expect(screen.queryByRole("button", { name: "解除关联" })).toBeNull();
  expect(screen.queryByRole("button", { name: "创建 Repository" })).toBeNull();
  let resolve!: (r: RepositoryObject) => void;
  c.read.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  fireEvent.click(screen.getByRole("button", { name: "刷新代码库详情" }));
  const signal = c.read.mock.calls.at(-1)![2] as AbortSignal;
  result.unmount();
  expect(signal.aborted).toBe(true);
  await act(async () => resolve({ ...repository, name: "Late" }));
  expect(screen.queryByText("Late")).toBeNull();
});
