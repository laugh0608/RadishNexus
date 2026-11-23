import { AuthRequestError } from "../auth/api";
import { object, requireID } from "../workspace/api";
import {
  parsePage,
  query,
  request,
  text,
} from "../workspace/configuration-api";

export async function listChannelThreads(
  workspace: string,
  channel: string,
  after?: string,
  signal?: AbortSignal,
) {
  requireID(channel, "chn_");
  const data = await request(
    workspace,
    `/channels/${encodeURIComponent(channel)}/threads${query(after)}`,
    "GET",
    undefined,
    signal,
  );
  return parsePage(
    data,
    (value) => {
      const row = object(value, ["ref", "title"]);
      const ref = object(row.ref, ["type", "id"]);
      if (ref.type !== "thread")
        throw new AuthRequestError("讨论列表响应不符合当前契约。");
      return { id: requireID(ref.id, "thr_"), title: text(row.title) };
    },
    after,
  );
}
