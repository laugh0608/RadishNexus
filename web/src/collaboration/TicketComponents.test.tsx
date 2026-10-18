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
  deliveryConfigurationClient,
  type DeliveryObject,
} from "../workspace/delivery-configuration-api";
import { ComponentTickets, TicketComponents } from "./TicketComponents";

const component: DeliveryObject = {
  id: "cmp_service",
  kind: "component",
  key: "service",
  name: "Service",
  category: "service",
  status: "active",
  ownerTeamID: "tem_main",
  canManage: false,
  canGrant: false,
  canRevoke: false,
  canLinkRepository: false,
};
const link = {
  id: component.id,
  linkID: "lnk_first",
  target: component,
  canUnlink: true,
};
function client() {
  return {
    components: vi
      .fn()
      .mockResolvedValue({ items: [link], nextCursor: null, canLink: true }),
    tickets: vi.fn().mockResolvedValue({
      items: [
        {
          id: "tkt_work",
          linkID: "lnk_first",
          target: {
            id: "tkt_work",
            title: "Private work",
            projectID: "prj_main",
            status: "open",
          },
          canUnlink: true,
        },
      ],
      nextCursor: null,
    }),
    link: vi.fn().mockResolvedValue("lnk_first"),
    unlink: vi.fn().mockResolvedValue("lnk_first"),
  };
}
function context(c = client()) {
  vi.spyOn(deliveryConfigurationClient, "list").mockResolvedValue({
    items: [
      component,
      { ...component, id: "cmp_retired", name: "Retired", status: "retired" },
    ],
    nextCursor: null,
  });
  return {
    workspaceID: "wrk_main",
    ticketID: "tkt_work",
    ticketTitle: "Work",
    client: c,
    onSessionExpired: vi.fn(),
    onChanged: vi.fn(),
    onUnavailable: vi.fn(),
  };
}
afterEach(() => vi.restoreAllMocks());
it("uses stable Component links, excludes retired choices and confirms exact retry across focus", async () => {
  const c = client(),
    props = context(c);
  c.link.mockRejectedValueOnce(new AuthRequestError("结果未知"));
  render(<TicketComponents {...props} />);
  expect(
    (
      await screen.findByRole("link", { name: "Service · service" })
    ).getAttribute("href"),
  ).toBe("/workspaces/wrk_main/components/cmp_service");
  expect(
    (
      (await screen.findByRole("option", {
        name: "Retired · service · retired",
      })) as HTMLOptionElement
    ).disabled,
  ).toBe(true);
  fireEvent.change(screen.getByLabelText("选择组件"), {
    target: { value: component.id },
  });
  expect(
    (screen.getByRole("button", { name: "确认关联组件" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  fireEvent.click(screen.getByLabelText("确认关联这两个对象"));
  fireEvent.click(screen.getByRole("button", { name: "确认关联组件" }));
  await screen.findByText("结果未知");
  expect(screen.getByLabelText("选择组件").closest("fieldset")!.disabled).toBe(
    true,
  );
  fireEvent.focus(window);
  await waitFor(() => expect(c.components).toHaveBeenCalledTimes(2));
  fireEvent.click(
    await screen.findByRole("button", { name: "重试原关联请求" }),
  );
  await waitFor(() => expect(c.link).toHaveBeenCalledTimes(2));
  expect(c.link.mock.calls[1]).toEqual(c.link.mock.calls[0]);
  expect(c.link.mock.calls[0]![2]).toMatchObject({
    component_id: component.id,
    confirmed: true,
  });
  expect(props.onChanged).toHaveBeenCalledOnce();
});
it("holds the old link identity when refresh sees a reconnected generation", async () => {
  const c = client();
  c.unlink.mockRejectedValueOnce(new AuthRequestError("解除结果未知"));
  render(<TicketComponents {...context(c)} />);
  fireEvent.click(await screen.findByRole("button", { name: "解除组件关联" }));
  fireEvent.click(screen.getByLabelText("确认解除这一条关联"));
  fireEvent.click(screen.getByRole("button", { name: "确认解除" }));
  await screen.findByText("解除结果未知");
  c.components.mockResolvedValue({
    items: [{ ...link, linkID: "lnk_second" }],
    nextCursor: null,
    canLink: true,
  });
  fireEvent.focus(window);
  fireEvent.click(
    await screen.findByRole("button", { name: "重试原解除请求" }),
  );
  await waitFor(() => expect(c.unlink).toHaveBeenCalledTimes(2));
  expect(c.unlink.mock.calls[1]).toEqual(c.unlink.mock.calls[0]);
  expect(c.unlink.mock.calls[1]![2]).toBe("lnk_first");
});
it("closes write forms after losing contribute access while retaining readable context", async () => {
  const c = client();
  render(<TicketComponents {...context(c)} />);
  fireEvent.change(await screen.findByLabelText("选择组件"), {
    target: { value: component.id },
  });
  c.components.mockResolvedValue({
    items: [{ ...link, canUnlink: false }],
    nextCursor: null,
    canLink: false,
  });
  fireEvent.focus(window);
  await waitFor(() =>
    expect(screen.queryByRole("form", { name: "关联组件" })).toBeNull(),
  );
  expect(screen.getByRole("link", { name: "Service · service" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "解除组件关联" })).toBeNull();
});
it("clears inaccessible reverse targets including a pending unlink form", async () => {
  const c = client(),
    props = context(c);
  render(
    <ComponentTickets
      {...props}
      componentID={component.id}
      componentName={component.name}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: "解除事项关联" }));
  expect(screen.getByRole("form")).toBeTruthy();
  c.tickets.mockResolvedValue({ items: [], nextCursor: null });
  c.components.mockRejectedValue(
    new AuthRequestError("不可读取", { status: 404 }),
  );
  fireEvent.focus(window);
  await waitFor(() => expect(screen.queryByText(/Private work/)).toBeNull());
  expect(screen.queryByRole("form")).toBeNull();
});
it("notifies the parent on lost Ticket read access and ignores responses after unmount", async () => {
  const c = client(),
    props = context(c);
  const ui = render(<TicketComponents {...props} />);
  await screen.findByLabelText("选择组件");
  c.components.mockRejectedValueOnce(
    new AuthRequestError("不可读取", { status: 404 }),
  );
  fireEvent.focus(window);
  await waitFor(() => expect(props.onUnavailable).toHaveBeenCalled());
  expect(screen.queryByRole("link")).toBeNull();
  let resolve!: (value: unknown) => void;
  c.components.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  fireEvent.focus(window);
  const signal = c.components.mock.calls.at(-1)![3] as AbortSignal;
  ui.unmount();
  expect(signal.aborted).toBe(true);
  await act(async () =>
    resolve({ items: [link], canLink: true, nextCursor: null }),
  );
  expect(screen.queryByText("Service")).toBeNull();
});
