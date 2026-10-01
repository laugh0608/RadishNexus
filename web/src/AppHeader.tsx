import type { ReactNode } from "react";

export function AppHeader({
  note,
  brandHref,
  children,
}: {
  note: string;
  brandHref: string;
  children?: ReactNode;
}) {
  return (
    <header className="prototype-header">
      <a className="brand-lockup" href={brandHref} aria-label="RadishNexus">
        <span className="brand-mark" aria-hidden="true">
          R
        </span>
        <span>
          <strong>RadishNexus</strong>
          <small>Context stays connected.</small>
        </span>
      </a>
      <div className="prototype-note">
        <span aria-hidden="true" />
        {note}
      </div>
      {children}
    </header>
  );
}
