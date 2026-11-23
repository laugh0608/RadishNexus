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
  originalEvidenceIDs: readonly string[];
  onSelect: (id: string) => void;
  onUnavailable: () => void;
  onSessionExpired: () => void;
}

export function ReviewEvidencePicker(props: Props) {
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
      <legend>{channel ? "选择补充讨论" : "选择频道"}</legend>
      {channelLost && <p role="status">频道已不可访问，请重新选择。</p>}
      {channel && (
        <button type="button" onClick={() => setChannel(null)}>
          更换频道
        </button>
      )}
      <EvidenceChoices
        key={channel?.id ?? projectID}
        load={load}
        onSessionExpired={props.onSessionExpired}
        channel={channel}
        originalEvidenceIDs={props.originalEvidenceIDs}
        select={(item) => {
          if (channel) props.onSelect(item.id);
          else {
            setChannelLost(false);
            setChannel(item);
          }
        }}
      />
    </fieldset>
  );
}

function EvidenceChoices({
  load,
  onSessionExpired,
  channel,
  originalEvidenceIDs,
  select,
}: {
  load: (
    after: string | undefined,
    signal: AbortSignal,
  ) => Promise<{ items: readonly Choice[]; nextCursor: string | null }>;
  onSessionExpired: () => void;
  channel: Choice | null;
  originalEvidenceIDs: readonly string[];
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
          const original =
            channel !== null && originalEvidenceIDs.includes(item.id);
          return (
            <button
              type="button"
              disabled={original}
              onClick={() => select(item)}
            >
              {item.title}
              {original
                ? "（原讨论）"
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
