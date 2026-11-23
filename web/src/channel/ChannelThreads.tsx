import { useCallback, useEffect, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { collaborationPagePath } from "../collaboration/api";
import { usePage } from "../workspace/configuration-hooks";
import { PageControls } from "../workspace/configuration-ui";
import { listChannelThreads } from "./channel-threads-api";

interface Props {
  workspaceID: string;
  channelID: string;
  onUnavailable: () => void;
  onSessionExpired: () => void;
}

export function ChannelThreads(props: Props) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const close = () => setOpen(false);
    const onVisibility = () => {
      if (document.hidden) close();
    };
    window.addEventListener("pagehide", close);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("pagehide", close);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);
  return (
    <section className="configuration-panel" aria-label="频道讨论">
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}>
        频道讨论
      </button>
      {open && <ThreadList {...props} />}
    </section>
  );
}

function ThreadList({
  workspaceID,
  channelID,
  onUnavailable,
  onSessionExpired,
}: Props) {
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      try {
        return await listChannelThreads(workspaceID, channelID, after, signal);
      } catch (error) {
        if (
          !signal.aborted &&
          error instanceof AuthRequestError &&
          error.status === 404
        )
          onUnavailable();
        throw error;
      }
    },
    [workspaceID, channelID, onUnavailable],
  );
  const page = usePage(load, onSessionExpired);
  return (
    <>
      {page.error ? (
        <p role="alert">无法读取频道讨论，请刷新重试。</p>
      ) : !page.page ? (
        <p role="status">正在读取频道讨论…</p>
      ) : page.page.items.length === 0 ? (
        <p>本页没有可读讨论。</p>
      ) : (
        <ul>
          {page.page.items.map((item) => (
            <li key={item.id}>
              <a
                href={
                  collaborationPagePath(workspaceID, "thread", item.id) ??
                  undefined
                }
              >
                {item.title}
              </a>
            </li>
          ))}
        </ul>
      )}
      <PageControls page={page} />
    </>
  );
}
