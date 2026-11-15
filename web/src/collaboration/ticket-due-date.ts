export function isTicketDueDate(value: unknown): value is string | null {
  if (value === null) return true;
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/u.test(value))
    return false;
  const [year, month, day] = value.split("-").map(Number) as [
    number,
    number,
    number,
  ];
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return year > 0 && day > 0 && day <= (days[month - 1] ?? 0);
}

export function parseTicketDueDate(value: unknown): string | null {
  if (!isTicketDueDate(value)) throw new TypeError("Invalid Ticket due date");
  return value;
}

export function ticketDueDateLabel(value: string | null): string {
  return value ?? "未设置";
}
