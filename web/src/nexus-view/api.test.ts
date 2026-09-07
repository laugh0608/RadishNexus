import { afterEach, describe, expect, it, vi } from "vitest";
import {
  DeploymentNexusViewLoadError,
  deploymentNexusViewLocation,
  loadDeploymentNexusViewData,
  parseDeploymentNexusViewResponse,
} from "./api";

const responsePayload = {
  data: {
    current: {
      ref: { type: "deployment", id: "dpl_release_42" },
      status: "succeeded",
      started_at: null,
      completed_at: "2026-08-30T02:24:27Z",
      recorded_at: "2026-08-30T02:24:31Z",
      environment: {
        ref: { type: "environment", id: "env_staging" },
        title: "Staging",
      },
      ci_run: {
        ref: { type: "ci-run", id: "cir_release_42" },
        title: "Release build 42",
      },
    },
    relations: [
      {
        visibility: "readable",
        relation_type: "deploys",
        target: {
          ref: { type: "ci-run", id: "cir_release_42" },
          title: "Release build 42",
        },
      },
    ],
    timeline: [
      {
        id: "evt_deployment_recorded_42",
        activity_type: "deployment.recorded",
        actor: { kind: "user", id: "usr_reader" },
        occurred_at: "2026-08-30T02:24:27Z",
        status: "succeeded",
        subjects: [
          {
            visibility: "readable",
            entity: {
              ref: { type: "environment", id: "env_staging" },
              title: "Staging",
            },
          },
          { visibility: "restricted" },
        ],
      },
    ],
  },
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Deployment Nexus View API adapter", () => {
  it("recognizes only canonical Deployment page locations", () => {
    expect(
      deploymentNexusViewLocation(
        "/workspaces/wrk_radish/deployments/dpl_release_42",
      ),
    ).toEqual({
      workspaceID: "wrk_radish",
      deploymentID: "dpl_release_42",
    });
    expect(deploymentNexusViewLocation("/workspaces/wrk_radish")).toBeNull();
    expect(
      deploymentNexusViewLocation(
        "/workspaces/wrk_radish//deployments/dpl_release_42",
      ),
    ).toBeNull();
    expect(
      deploymentNexusViewLocation(
        "/workspaces/not-a-workspace/deployments/dpl_release_42",
      ),
    ).toBeNull();
  });

  it("loads the same-origin no-store endpoint and maps the public DTO", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(responsePayload));
    vi.stubGlobal("fetch", fetchMock);

    const result = await loadDeploymentNexusViewData(
      "wrk_radish",
      "dpl_release_42",
    );

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/workspaces/wrk_radish/deployments/dpl_release_42/nexus-view",
      expect.objectContaining({
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
        headers: { Accept: "application/json" },
      }),
    );
    expect(result.current).toMatchObject({
      entityRef: "entity://deployment/dpl_release_42",
      entityType: "deployment",
      status: "succeeded",
      statusLabel: "部署成功",
      startedAt: null,
      startedAtLabel: "未记录",
      environment: {
        entityRef: "entity://environment/env_staging",
        name: "Staging",
      },
      ciRun: {
        entityRef: "entity://ci-run/cir_release_42",
        name: "Release build 42",
      },
    });
    expect(result.relations[0]).toMatchObject({
      visibility: "readable",
      entityRef: "entity://ci-run/cir_release_42",
    });
    expect(result.timeline[0]).toMatchObject({
      visibility: "readable",
      actorLabel: "成员 usr_reader",
      sourceLabel: "deployment.recorded",
    });
    expect(JSON.stringify(result)).not.toMatch(
      /authorization|jenkins|receipt|digest|secret|external/iu,
    );
  });

  it("keeps not-found authorization ambiguity in a safe user message", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({}, 404)));

    await expect(
      loadDeploymentNexusViewData("wrk_radish", "dpl_missing"),
    ).rejects.toMatchObject({
      status: 404,
      userMessage: "该 Deployment 不存在，或你没有读取它的权限。",
    } satisfies Partial<DeploymentNexusViewLoadError>);
  });

  it("rejects malformed or drifted public projections", () => {
    expect(() =>
      parseDeploymentNexusViewResponse({
        ...responsePayload,
        data: {
          ...responsePayload.data,
          current: { ...responsePayload.data.current, status: "deployed" },
        },
      }),
    ).toThrow(/current\.status/u);

    expect(() =>
      parseDeploymentNexusViewResponse({
        ...responsePayload,
        data: {
          ...responsePayload.data,
          relations: [
            {
              ...responsePayload.data.relations[0],
              target: {
                ref: { type: "ci-run", id: "cir_different" },
                title: "Different build",
              },
            },
          ],
        },
      }),
    ).toThrow(/does not match/u);
  });

  it("rejects a successful response for a different Deployment", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse({
          ...responsePayload,
          data: {
            ...responsePayload.data,
            current: {
              ...responsePayload.data.current,
              ref: { type: "deployment", id: "dpl_different" },
            },
          },
        }),
      ),
    );

    await expect(
      loadDeploymentNexusViewData("wrk_radish", "dpl_release_42"),
    ).rejects.toMatchObject({
      userMessage: "服务返回的 Nexus View 不符合当前契约，请稍后重试。",
    } satisfies Partial<DeploymentNexusViewLoadError>);
  });
});

function jsonResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: {
      get: (name: string) =>
        name.toLowerCase() === "content-type" ? "application/json" : null,
    } as Headers,
    json: async () => payload,
  } as Response;
}
