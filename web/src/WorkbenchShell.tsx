import { useRef, type ReactNode } from "react";

/** Shared navigation uses session data only; object permissions stay in each page. */
export function WorkbenchShell({
  workspaceName,
  userName,
  accountActive,
  loggingOut,
  logoutError,
  onLogout,
  children,
}: {
  workspaceName?: string;
  userName: string;
  accountActive: boolean;
  loggingOut: boolean;
  logoutError: string | null;
  onLogout: () => void;
  children: ReactNode;
}) {
  const drawer = useRef<HTMLDialogElement>(null);
  const menuButton = useRef<HTMLButtonElement>(null);
  const navigation = (
    <>
      <a className="workbench-brand" href="/">
        RadishNexus
      </a>
      <a className="workbench-workspace" href="/">
        <span>{workspaceName ?? "选择工作区"}</span> <span>切换</span>
      </a>
      <nav aria-label="工作台导航">
        <a href="/">工作区</a>
        <a href="/account" aria-current={accountActive ? "page" : undefined}>
          账户与邀请
        </a>
      </nav>
      <div className="workbench-account">
        <span>{userName}</span>
        <button type="button" disabled={loggingOut} onClick={onLogout}>
          {loggingOut ? "正在退出…" : "退出登录"}
        </button>
      </div>
    </>
  );
  return (
    <div className="workbench">
      <a className="skip-link" href="#workbench-content">
        跳到主要内容
      </a>
      <aside className="workbench-sidebar" aria-label="工作台">
        {navigation}
      </aside>
      <header className="workbench-mobile-bar">
        <button
          ref={menuButton}
          type="button"
          aria-label="打开导航"
          aria-haspopup="dialog"
          onClick={() => drawer.current?.showModal()}
        >
          <span aria-hidden="true">☰</span>
        </button>
        <a className="workbench-brand" href="/">
          RadishNexus
        </a>
        <a href="/account" aria-label="账户与邀请">
          账户
        </a>
      </header>
      <dialog
        className="workbench-drawer"
        ref={drawer}
        aria-label="工作台导航"
        onClose={() => menuButton.current?.focus({ preventScroll: true })}
        onKeyDown={(event) => {
          if (event.key !== "Tab") return;
          const controls = event.currentTarget.querySelectorAll<HTMLElement>(
            "a[href], button:not(:disabled)",
          );
          const first = controls[0];
          const last = controls[controls.length - 1];
          if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last?.focus();
          } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first?.focus();
          }
        }}
        onClick={(event) => {
          if (
            !event.defaultPrevented &&
            event.target instanceof Element &&
            event.target.closest("a[href]")
          )
            drawer.current?.close();
        }}
      >
        <button
          className="drawer-close"
          type="button"
          autoFocus
          onClick={() => drawer.current?.close()}
        >
          关闭导航
        </button>
        {navigation}
      </dialog>
      <div className="workbench-content" id="workbench-content" tabIndex={-1}>
        {logoutError ? (
          <p className="shell-alert" role="alert">
            {logoutError}
          </p>
        ) : null}
        {children}
      </div>
    </div>
  );
}
