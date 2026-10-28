import { AuthRequestError } from "../auth/api";

export const ticketPriorityLabels = {
  unset: "未设置",
  low: "低",
  normal: "普通",
  high: "高",
  urgent: "紧急",
} as const;
export type TicketPriority = keyof typeof ticketPriorityLabels;
export function parseTicketPriority(value: unknown): TicketPriority {
  if (typeof value !== "string" || !Object.hasOwn(ticketPriorityLabels, value))
    throw new AuthRequestError("工单优先级不符合当前契约，请重新读取。");
  return value as TicketPriority;
}
