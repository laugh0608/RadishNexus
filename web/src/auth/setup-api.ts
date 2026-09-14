import { AuthRequestError } from "./api";

export type SetupStatus = "required" | "unavailable" | "complete";
export interface SetupInput {
  setup_code: string;
  email: string;
  display_name: string;
  password: string;
  workspace_name: string;
}
export interface SetupClient {
  status(signal?: AbortSignal): Promise<SetupStatus>;
  complete(input: SetupInput, signal?: AbortSignal): Promise<void>;
}
export const browserSetupClient: SetupClient = {
  status: (signal) => request(undefined, signal),
  complete: async (input, signal) => {
    if ((await request(input, signal)) !== "complete") throw contractError();
  },
};
function contractError() {
  return new AuthRequestError(
    "初始化服务返回了无法识别的响应，请重新检查状态。",
  );
}
async function request(
  input?: SetupInput,
  signal?: AbortSignal,
): Promise<SetupStatus> {
  let response: Response;
  try {
    response = await fetch("/api/v1/setup", {
      method: input ? "POST" : "GET",
      credentials: "same-origin",
      cache: "no-store",
      headers: {
        Accept: "application/json",
        ...(input ? { "Content-Type": "application/json" } : {}),
      },
      ...(input ? { body: JSON.stringify(input) } : {}),
      signal,
    });
  } catch (cause) {
    throw new AuthRequestError("无法确认初始化结果，请重新检查状态。", {
      cause,
    });
  }
  if (!response.ok) {
    const message =
      response.status === 403
        ? "初始化码不可用，请向部署者核实。"
        : response.status === 409
          ? "实例已完成初始化，请检查状态后登录。"
          : response.status === 429
            ? "请求过于频繁，请稍后重试。"
            : response.status === 400
              ? "请检查输入：名称为 1–100 字，密码为 15–128 字。"
              : "初始化服务暂时不可用，请重新检查状态。";
    throw new AuthRequestError(message, { status: response.status });
  }
  if (
    response.status !== (input ? 201 : 200) ||
    !response.headers
      .get("Content-Type")
      ?.toLowerCase()
      .includes("application/json")
  )
    throw contractError();
  let data: unknown;
  try {
    data = await response.json();
  } catch {
    throw contractError();
  }
  if (
    typeof data !== "object" ||
    data === null ||
    Array.isArray(data) ||
    Object.keys(data).length !== 1 ||
    !("status" in data) ||
    typeof data.status !== "string" ||
    !["required", "unavailable", "complete"].includes(String(data.status))
  )
    throw contractError();
  return data.status as SetupStatus;
}
