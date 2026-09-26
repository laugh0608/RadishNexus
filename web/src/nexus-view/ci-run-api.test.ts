import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ciRunLocation,
  ciRunPagePath,
  loadCIRun,
  parseCIRunResponse,
} from "./ci-run-api";
const fixture = () => {
  const component = { ref: { type: "component", id: "cmp_api" }, title: "API" };
  return {
    data: {
      current: {
        ref: { type: "ci-run", id: "cir_build" },
        status: "succeeded",
        started_at: null as string | null,
        completed_at: "2026-09-26T01:00:00Z",
        recorded_at: "2026-09-26T01:00:01Z",
        updated_at: "2026-09-26T01:00:01Z",
        component,
      },
      relations: [],
      timeline: [
        {
          id: "evt_build",
          activity_type: "ci-run.recorded",
          actor: { kind: "plugin" },
          occurred_at: "2026-09-26T01:00:00Z",
          status: "succeeded",
          subjects: [
            { visibility: "readable", entity: structuredClone(component) },
          ],
        },
      ],
    },
  };
};
afterEach(() => vi.unstubAllGlobals());
describe("CI Run public contract", () => {
  it("parses terminal facts and a missing start without inventing provenance", () => {
    const data = parseCIRunResponse(fixture());
    expect(data.current.entityType).toBe("ci-run");
    expect(data.current).toMatchObject({
      startedAt: null,
      startedAtLabel: "未提供",
      statusLabel: "构建成功",
    });
    expect(data.timeline).toHaveLength(1);
    expect(data.relations).toEqual([]);
  });
  it("rejects leaks, malformed times, scope drift and inconsistent events", () => {
    const mutations: ((f: ReturnType<typeof fixture>) => void)[] = [
      (f) => Object.assign(f.data.current, { source_id: "secret" }),
      (f) => Object.assign(f.data.timeline[0]!.actor, { id: "source-secret" }),
      (f) => Object.assign(f.data.current.component.ref, { extra: "secret" }),
      (f) => {
        f.data.current.status = "running";
      },
      (f) => {
        f.data.current.started_at = "2026-09-26T01:00:00.000000002Z";
        f.data.current.completed_at = "2026-09-26T01:00:00.000000001Z";
        f.data.timeline[0]!.occurred_at = f.data.current.completed_at;
      },
      (f) => {
        f.data.current.started_at = "2026-09-26T01:00:02Z";
      },
      (f) => {
        f.data.current.completed_at = "2026-02-30T01:00:00Z";
      },
      (f) => {
        f.data.current.updated_at = "2026-09-26T01:00:02Z";
      },
      (f) => {
        f.data.timeline[0]!.occurred_at = "2026-09-26T01:00:01Z";
      },
      (f) => {
        f.data.timeline[0]!.subjects[0]!.entity.title = "drift";
      },
      (f) => {
        f.data.timeline[0]!.subjects[0]!.visibility = "restricted";
      },
      (f) => {
        f.data.timeline = [];
      },
    ];
    for (const mutate of mutations) {
      const f = fixture();
      mutate(f);
      expect(() => parseCIRunResponse(f)).toThrow();
    }
  });
  it("validates canonical routes and uses encoded same-origin no-store GET", async () => {
    expect(ciRunPagePath("wrk_main", "cir_build")).toBe(
      "/workspaces/wrk_main/ci-runs/cir_build",
    );
    expect(ciRunLocation("/workspaces/wrk_main/ci-runs/cir_build/")).toEqual({
      workspaceID: "wrk_main",
      ciRunID: "cir_build",
    });
    for (const path of [
      "/workspaces/wrk_main/ci-runs/cir_%2Fbad",
      "/workspaces/wrk_main/ci-runs/dpl_bad",
      "/workspaces/wrk_main/ci-runs/%ZZ",
    ])
      expect(ciRunLocation(path)).toBeNull();
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(fixture()), {
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);
    const signal = new AbortController().signal;
    await loadCIRun("wrk_main", "cir_build", signal);
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/workspaces/wrk_main/ci-runs/cir_build/nexus-view",
      {
        method: "GET",
        headers: { Accept: "application/json" },
        credentials: "same-origin",
        cache: "no-store",
        signal,
      },
    );
  });
  it("preserves HTTP errors, rejects wrong object and never substitutes fixture", async () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    for (const status of [401, 404, 500]) {
      fetch.mockResolvedValueOnce(new Response("{}", { status }));
      await expect(loadCIRun("wrk_main", "cir_build")).rejects.toMatchObject({
        status,
      });
    }
    fetch.mockResolvedValueOnce(
      new Response(JSON.stringify(fixture()), {
        headers: { "Content-Type": "application/json" },
      }),
    );
    await expect(loadCIRun("wrk_main", "cir_other")).rejects.toThrow("协议");
    fetch.mockResolvedValueOnce(new Response("<html></html>"));
    await expect(loadCIRun("wrk_main", "cir_build")).rejects.toThrow("协议");
  });
});
