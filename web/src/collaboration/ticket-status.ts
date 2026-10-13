export type TicketStatus = "open" | "in-progress" | "done" | "canceled";
export const maxTicketStatusRevision = 2147483647;
export const ticketStatusLabels: Readonly<Record<TicketStatus, string>> = {
  open: "待开始",
  "in-progress": "处理中",
  done: "已完成",
  canceled: "已取消",
};
export const ticketStatusTargets: Readonly<
  Record<TicketStatus, readonly TicketStatus[]>
> = {
  open: ["in-progress", "done", "canceled"],
  "in-progress": ["open", "done", "canceled"],
  done: ["open"],
  canceled: ["open"],
};
export function parseTicketStatus(value: unknown): TicketStatus {
  if (
    value !== "open" &&
    value !== "in-progress" &&
    value !== "done" &&
    value !== "canceled"
  )
    throw new TypeError("工单状态不符合当前契约。");
  return value;
}
export function parseTicketStatusRevision(value: unknown): number {
  if (
    typeof value !== "number" ||
    !Number.isInteger(value) ||
    value < 1 ||
    value > maxTicketStatusRevision
  )
    throw new TypeError("工单状态版本不符合当前契约。");
  return value;
}
export function ticketStatusAction(
  from: TicketStatus,
  to: TicketStatus,
): string {
  return to === "open"
    ? from === "done" || from === "canceled"
      ? "重新打开"
      : "返回待开始"
    : to === "in-progress"
      ? "开始处理"
      : to === "done"
        ? "标记完成"
        : "取消工单";
}
