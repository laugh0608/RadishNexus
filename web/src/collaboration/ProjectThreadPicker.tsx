import { useCallback, useState } from "react";
import { AuthRequestError } from "../auth/api";
import { listChannelThreads } from "../channel/channel-threads-api";
import { browserDiscoveryClient } from "../workspace/api";
import { usePage } from "../workspace/configuration-hooks";
import { TraceList } from "./TicketCIRuns";

type Choice = { id: string; title: string; status?: "active" | "archived" };
interface Props {
  workspaceID: string;
  projectID: string;
  disabledThreadIDs: readonly string[];
  purpose: "review" | "execution";
  onSelect: (thread: { id: string; title: string }) => void;
  onUnavailable: () => void;
  onSessionExpired: () => void;
}

export function ProjectThreadPicker(props: Props) {
  const [channel, setChannel] = useState<Choice | null>(null);
  const [channelLost, setChannelLost] = useState(false);
  const { workspaceID, projectID, onUnavailable } = props;
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      try {
        return channel
          ? await listChannelThreads(workspaceID, channel.id, after, signal)
          : await browserDiscoveryClient.channels(
              workspaceID,
              projectID,
              after,
              signal,
            );
      } catch (error) {
        if (
          !signal.aborted &&
          error instanceof AuthRequestError &&
          error.status === 404
        ) {
          if (channel) {
            setChannelLost(true);
            setChannel(null);
          } else onUnavailable();
        }
        throw error;
      }
    },
    [channel, workspaceID, projectID, onUnavailable],
  );
  return (
    <fieldset>
      <legend>
        {channel
          ? props.purpose === "review"
            ? "选择补充讨论"
            : "选择执行讨论"
          : "选择频道"}
      </legend>
      {channelLost && <p role="status">频道已不可访问，请重新选择。</p>}
      {channel && (
        <button type="button" onClick={() => setChannel(null)}>
          更换频道
        </button>
      )}
      <ThreadPickerPage
        key={channel?.id ?? projectID}
        load={load}
        onSessionExpired={props.onSessionExpired}
        channel={channel}
        purpose={props.purpose}
        disabledThreadIDs={props.disabledThreadIDs}
        select={(item) => {
          if (channel) props.onSelect(item);
          else {
            setChannelLost(false);
            setChannel(item);
          }
        }}
      />
    </fieldset>
  );
}

function ThreadPickerPage({
  load,
  onSessionExpired,
  channel,
  disabledThreadIDs,
  purpose,
  select,
}: {
  load: (
    after: string | undefined,
    signal: AbortSignal,
  ) => Promise<{ items: readonly Choice[]; nextCursor: string | null }>;
  onSessionExpired: () => void;
  channel: Choice | null;
  disabledThreadIDs: readonly string[];
  purpose: Props["purpose"];
  select: (item: Choice) => void;
}) {
  const page = usePage(load, onSessionExpired);
  return (
    <>
      {channel && page.page && <p>{channel.title}</p>}
      <TraceList
        state={page}
        empty={channel ? "本页没有可读讨论。" : "本页没有可读频道。"}
        row={(item) => {
          const excluded =
            channel !== null && disabledThreadIDs.includes(item.id);
          return (
            <button
              type="button"
              disabled={
                excluded ||
                (purpose === "execution" && item.status === "archived")
              }
              onClick={() => select(item)}
            >
              {item.title}
              {excluded
                ? purpose === "review"
                  ? "（原讨论）"
                  : "（已关联）"
                : item.status === "archived"
                  ? "（已归档）"
                  : ""}
            </button>
          );
        }}
      />
    </>
  );
}
