import { useCallback, useState } from "react";
import { OperationStatus } from "./ExecutionPanels";
import { useExecutionRead } from "./useExecutionRead";
import { useExecutionWrite } from "./execution-write";
import {
  ClassificationText,
  TicketClassificationFields,
} from "./TicketClassificationFields";
import {
  ticketClassificationClient,
  type ClassificationInput,
} from "./ticket-classification-api";

export function TicketClassificationPanel({
  workspace,
  project,
  ticket,
  onSessionExpired,
  onUnavailable,
  onChanged,
}: {
  workspace: string;
  project: string;
  ticket: string;
  onSessionExpired: () => void;
  onUnavailable: () => void;
  onChanged: () => void;
}) {
  const write = useExecutionWrite({ onSessionExpired, onUnavailable });
  const revision = write.revision;
  const clear = write.clear;
  const load = useCallback(
    async (signal: AbortSignal) => {
      void revision;
      const current = await ticketClassificationClient.read(
        workspace,
        ticket,
        signal,
      );
      if (!signal.aborted && !current.canSet) clear();
      return current;
    },
    [workspace, ticket, revision, clear],
  );
  const { data, error } = useExecutionRead(load, onSessionExpired, true, () => {
    write.clear();
    onUnavailable();
  });
  return (
    <section aria-label="工单分类">
      <h3>工单分类</h3>
      {data ? (
        <>
          <p>
            <ClassificationText value={data} />
          </p>
          {data.canSet && !write.pending && (
            <ClassificationEditor
              key={data.revision}
              current={data}
              workspace={workspace}
              project={project}
              onSessionExpired={onSessionExpired}
              onConfirm={(value) => {
                write.start("保存工单分类", async (operation, signal) => {
                  await ticketClassificationClient.set(
                    workspace,
                    ticket,
                    {
                      ...value,
                      base_classification_revision: data.revision,
                      client_operation_id: operation,
                      confirmed: true,
                    },
                    signal,
                  );
                  if (!signal.aborted) onChanged();
                });
              }}
            />
          )}
        </>
      ) : (
        <p role="status">{error ?? "正在读取工单分类…"}</p>
      )}
      <OperationStatus write={write} />
    </section>
  );
}

function ClassificationEditor({
  current,
  onConfirm,
  ...scope
}: {
  current: Awaited<ReturnType<typeof ticketClassificationClient.read>>;
  workspace: string;
  project: string;
  onSessionExpired: () => void;
  onConfirm: (value: ClassificationInput) => void;
}) {
  const original = {
    ticket_type: current.ticketType,
    label_ids: current.labels.map((label) => label.id),
  };
  const [draft, setDraft] = useState<ClassificationInput>(original);
  const changed =
    draft.ticket_type !== original.ticket_type ||
    draft.label_ids.join(",") !== original.label_ids.join(",");
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        if (changed) onConfirm(draft);
      }}
    >
      <TicketClassificationFields
        {...scope}
        value={draft}
        onChange={setDraft}
        existing={current.labels}
      />
      <button type="submit" disabled={!changed}>
        确认保存分类
      </button>
    </form>
  );
}
