import { useCallback, useState } from "react";
import { configurationClient } from "../workspace/configuration-api";
import { usePage } from "../workspace/configuration-hooks";
import {
  parseTicketType,
  ticketClassificationClient,
  ticketTypeLabels,
  type ClassificationInput,
  type ClassificationSummary,
  type TicketLabel,
} from "./ticket-classification-api";

export type LabelScope = {
  workspace: string;
  project?: string;
  channel?: string;
  onSessionExpired: () => void;
};

export function ClassificationText({
  value,
}: {
  value: ClassificationSummary;
}) {
  return (
    <>
      {ticketTypeLabels[value.ticketType]}
      {value.labels.map((label) => (
        <span key={label.id}>
          {" "}
          · {label.name}
          {label.status === "archived" ? "（已归档）" : ""}
        </span>
      ))}
    </>
  );
}

export function LabelPicker({
  workspace,
  project,
  channel,
  onSessionExpired,
  selected,
  onChange,
  includeArchived = false,
}: LabelScope & {
  selected: readonly string[];
  onChange: (ids: string[]) => void;
  includeArchived?: boolean;
}) {
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      const scope =
        project ??
        (channel &&
          (
            await configurationClient.read(
              workspace,
              "channel",
              channel,
              signal,
            )
          ).projectID);
      if (!scope) throw new TypeError("缺少标签目录所属的项目。");
      return ticketClassificationClient.labels(
        workspace,
        scope,
        includeArchived ? "all" : "active",
        after,
        signal,
      );
    },
    [workspace, project, channel, includeArchived],
  );
  const page = usePage(load, onSessionExpired);
  return (
    <fieldset>
      <legend>
        {includeArchived ? "按标签筛选（全部匹配）" : "选择项目标签"}
      </legend>
      {selected.length > 0 && <span>已选 {selected.length} 个标签</span>}
      {page.error ? (
        <>
          <p role="status">{page.error}</p>
          <button type="button" onClick={page.refresh}>
            重读标签
          </button>
        </>
      ) : page.page ? (
        <>
          {page.page.items.length === 0 && <p>当前没有可选标签。</p>}
          {page.page.items.map((label) => (
            <label key={label.id}>
              <input
                type="checkbox"
                checked={selected.includes(label.id)}
                onChange={(e) =>
                  onChange(
                    e.target.checked
                      ? [...selected, label.id].sort()
                      : selected.filter((id) => id !== label.id),
                  )
                }
              />
              {label.name}
              {label.status === "archived" ? "（已归档）" : ""}
            </label>
          ))}
          {page.hasPrevious && (
            <button type="button" onClick={page.previous}>
              上一页标签
            </button>
          )}
          {page.page.nextCursor && (
            <button type="button" onClick={page.next}>
              下一页标签
            </button>
          )}
        </>
      ) : (
        <p role="status">正在读取标签…</p>
      )}
    </fieldset>
  );
}

export function TicketClassificationFields({
  value,
  onChange,
  existing = [],
  ...scope
}: LabelScope & {
  value: ClassificationInput;
  onChange: (value: ClassificationInput) => void;
  existing?: readonly TicketLabel[];
}) {
  const [showLabels, setShowLabels] = useState(false);
  return (
    <>
      <label>
        工单类型
        <select
          value={value.ticket_type}
          onChange={(e) =>
            onChange({ ...value, ticket_type: parseTicketType(e.target.value) })
          }
        >
          {Object.entries(ticketTypeLabels).map(([type, label]) => (
            <option key={type} value={type}>
              {label}
            </option>
          ))}
        </select>
      </label>
      {existing
        .filter(
          (label) =>
            label.status === "archived" && value.label_ids.includes(label.id),
        )
        .map((label) => (
          <button
            key={label.id}
            type="button"
            onClick={() =>
              onChange({
                ...value,
                label_ids: value.label_ids.filter((id) => id !== label.id),
              })
            }
          >
            移除 {label.name}（已归档）
          </button>
        ))}
      <button
        type="button"
        aria-expanded={showLabels}
        onClick={() => setShowLabels((show) => !show)}
      >
        项目标签
        {value.label_ids.length > 0 ? `（${value.label_ids.length}）` : ""}
      </button>
      {showLabels && (
        <LabelPicker
          {...scope}
          selected={value.label_ids}
          onChange={(label_ids) => onChange({ ...value, label_ids })}
        />
      )}
    </>
  );
}
