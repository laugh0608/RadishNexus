import type { ReactNode } from "react";

export function PageControls({
  page,
}: {
  page: {
    page?: { nextCursor: string | null };
    hasPrevious: boolean;
    next: () => void;
    previous: () => void;
    refresh: () => void;
  };
}) {
  return (
    <div className="configuration-actions">
      <button type="button" onClick={page.refresh}>
        刷新
      </button>
      <button
        type="button"
        disabled={!page.hasPrevious || !page.page}
        onClick={page.previous}
      >
        上一页
      </button>
      <button
        type="button"
        disabled={!page.page?.nextCursor}
        onClick={page.next}
      >
        下一页
      </button>
    </div>
  );
}
export function Feedback({
  busy,
  message,
}: {
  busy: boolean;
  message: string | null;
}) {
  return <p role="status">{busy ? "正在保存…" : message}</p>;
}
export function Field({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <label>
      <span>{label}</span>
      {children}
    </label>
  );
}
