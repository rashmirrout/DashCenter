import { describe, it, expect } from "vitest";

import {
  capacityRows,
  connectedDpuCount,
  dpuEntryId,
  eniPlacementCountsByDpu,
  entryAclMax,
  entryAclUsed,
  entryEniMax,
  entryEniUsed,
  entryFlowMax,
  entryFlowUsed,
  entryRouteMax,
  entryRouteUsed,
  fleetCapacity,
  fleetDegradedDpus,
  fleetDisconnectedDpus,
  fleetDpuCount,
  fleetDpuStates,
  fleetEniCount,
  fleetHealthyDpus,
  fleetVnetCount,
  placementDpuIds,
  placementEniName,
  sumCapacity,
} from "../src/lib/api-helpers";
import type {
  CapacityStats,
  DashdHealthResponse,
  DpuCapacityEntry,
  EniPlacement,
  FleetSummary,
} from "../src/api/types";

describe("api-helpers · dpuEntryId", () => {
  it("returns dpu_id when present", () => {
    expect(dpuEntryId({ dpu_id: "dpu-1", id: "x" })).toBe("dpu-1");
  });
  it("falls back to id when dpu_id is missing", () => {
    expect(dpuEntryId({ id: "dpu-2" })).toBe("dpu-2");
  });
  it("returns empty string when both are missing", () => {
    expect(dpuEntryId({})).toBe("");
  });
});

describe("api-helpers · entry capacity selectors", () => {
  const legacy: DpuCapacityEntry = {
    dpu_id: "d1",
    state: "DPU_STATE_UP",
    eni_count: 5,
    eni_max: 10,
    route_count: 7,
    route_max: 14,
    acl_rule_count: 11,
    acl_rule_max: 22,
    flow_count: 13,
    flow_max: 26,
  };
  const current: DpuCapacityEntry = {
    id: "d2",
    state: "DPU_STATE_UP",
    enis_used: 3,
    enis_max: 8,
    routes_used: 9,
    routes_max: 18,
    acl_rules_used: 15,
    acl_rules_max: 30,
    flows_used: 21,
    flows_max: 42,
  };

  it("prefers legacy fields when both present", () => {
    const mixed: DpuCapacityEntry = { ...current, ...legacy };
    expect(entryEniUsed(mixed)).toBe(legacy.eni_count);
    expect(entryRouteMax(mixed)).toBe(legacy.route_max);
  });

  it("reads current snake_case fields", () => {
    expect(entryEniUsed(current)).toBe(3);
    expect(entryEniMax(current)).toBe(8);
    expect(entryRouteUsed(current)).toBe(9);
    expect(entryRouteMax(current)).toBe(18);
    expect(entryAclUsed(current)).toBe(15);
    expect(entryAclMax(current)).toBe(30);
    expect(entryFlowUsed(current)).toBe(21);
    expect(entryFlowMax(current)).toBe(42);
  });

  it("returns 0 when nothing present", () => {
    const empty: DpuCapacityEntry = { state: "UNKNOWN" };
    expect(entryEniUsed(empty)).toBe(0);
    expect(entryEniMax(empty)).toBe(0);
    expect(entryRouteUsed(empty)).toBe(0);
    expect(entryRouteMax(empty)).toBe(0);
    expect(entryAclUsed(empty)).toBe(0);
    expect(entryAclMax(empty)).toBe(0);
    expect(entryFlowUsed(empty)).toBe(0);
    expect(entryFlowMax(empty)).toBe(0);
  });
});

describe("api-helpers · sumCapacity", () => {
  it("returns 0 for undefined or empty", () => {
    expect(sumCapacity(undefined, entryEniUsed)).toBe(0);
    expect(sumCapacity([], entryEniUsed)).toBe(0);
  });
  it("sums across rows", () => {
    const rows: DpuCapacityEntry[] = [
      { state: "UP", enis_used: 2 },
      { state: "UP", enis_used: 3 },
      { state: "UP", eni_count: 5 },
    ];
    expect(sumCapacity(rows, entryEniUsed)).toBe(10);
  });
});

describe("api-helpers · capacityRows", () => {
  it("returns [] for undefined", () => {
    expect(capacityRows(undefined)).toEqual([]);
  });
  it("prefers per_dpu over dpus", () => {
    const cs: CapacityStats = {
      per_dpu: [{ state: "UP", id: "a" }],
      dpus: [{ state: "UP", id: "b" }],
    };
    expect(capacityRows(cs).map(dpuEntryId)).toEqual(["a"]);
  });
  it("falls back to dpus when per_dpu absent", () => {
    const cs: CapacityStats = { dpus: [{ state: "UP", id: "b" }] };
    expect(capacityRows(cs).map(dpuEntryId)).toEqual(["b"]);
  });
});

describe("api-helpers · fleetCapacity", () => {
  it("uses fleet object when present", () => {
    const cs: CapacityStats = {
      fleet: {
        total_enis: 100,
        max_enis: 500,
        total_routes: 200,
        max_routes: 600,
        total_acl_rules: 300,
        max_acl_rules: 700,
        total_flows: 400,
        max_flows: 800,
      },
    };
    const cap = fleetCapacity(cs);
    expect(cap.enisUsed).toBe(100);
    expect(cap.enisMax).toBe(500);
    expect(cap.routesUsed).toBe(200);
    expect(cap.routesMax).toBe(600);
    expect(cap.aclRulesUsed).toBe(300);
    expect(cap.aclRulesMax).toBe(700);
    expect(cap.flowsUsed).toBe(400);
    expect(cap.flowsMax).toBe(800);
  });

  it("falls back to per_dpu sums when fleet missing maxes", () => {
    const cs: CapacityStats = {
      fleet: { total_enis: 7 },
      per_dpu: [
        {
          state: "UP",
          enis_used: 3,
          enis_max: 10,
          routes_used: 4,
          routes_max: 20,
          acl_rules_used: 5,
          acl_rules_max: 30,
          flows_used: 6,
          flows_max: 40,
        },
        {
          state: "UP",
          enis_used: 4,
          enis_max: 12,
          routes_used: 5,
          routes_max: 22,
          acl_rules_used: 6,
          acl_rules_max: 32,
          flows_used: 7,
          flows_max: 42,
        },
      ],
    };
    const cap = fleetCapacity(cs);
    expect(cap.enisUsed).toBe(7); // fleet.total_enis wins
    expect(cap.enisMax).toBe(22); // sum 10+12
    expect(cap.routesUsed).toBe(9);
    expect(cap.routesMax).toBe(42);
    expect(cap.aclRulesUsed).toBe(11);
    expect(cap.aclRulesMax).toBe(62);
    expect(cap.flowsUsed).toBe(13);
    expect(cap.flowsMax).toBe(82);
  });

  it("returns all zeros for undefined input", () => {
    const cap = fleetCapacity(undefined);
    expect(cap.enisUsed).toBe(0);
    expect(cap.enisMax).toBe(0);
    expect(cap.routesUsed).toBe(0);
    expect(cap.routesMax).toBe(0);
    expect(cap.aclRulesUsed).toBe(0);
    expect(cap.aclRulesMax).toBe(0);
    expect(cap.flowsUsed).toBe(0);
    expect(cap.flowsMax).toBe(0);
  });
});

describe("api-helpers · connectedDpuCount", () => {
  it("returns 0 when no health", () => {
    expect(connectedDpuCount(undefined)).toBe(0);
  });
  it("uses explicit connected_dpus when present", () => {
    const hd: DashdHealthResponse = {
      status: "ok",
      leader: true,
      connected_dpus: 5,
    };
    expect(connectedDpuCount(hd)).toBe(5);
  });
  it("uses dpus.length when connected_dpus missing", () => {
    const hd: DashdHealthResponse = {
      status: "ok",
      leader: true,
      dpus: [
        { id: "a", state: "DPU_STATE_UP" },
        { id: "b", state: "DPU_STATE_UP" },
      ],
    };
    expect(connectedDpuCount(hd)).toBe(2);
  });
  it("returns 0 when neither is present", () => {
    const hd: DashdHealthResponse = { status: "ok", leader: true };
    expect(connectedDpuCount(hd)).toBe(0);
  });
});

describe("api-helpers · fleet summary selectors", () => {
  it("returns 0 for undefined", () => {
    expect(fleetDpuCount(undefined)).toBe(0);
    expect(fleetEniCount(undefined)).toBe(0);
    expect(fleetVnetCount(undefined)).toBe(0);
    expect(fleetDpuStates(undefined)).toEqual({});
    expect(fleetHealthyDpus(undefined)).toBe(0);
    expect(fleetDegradedDpus(undefined)).toBe(0);
    expect(fleetDisconnectedDpus(undefined)).toBe(0);
  });

  it("prefers current field names", () => {
    const fs: FleetSummary = {
      dpu_count: 10,
      eni_count: 41,
      vnet_count: 14,
      dpus_by_state: {
        DPU_STATE_UP: 8,
        DPU_STATE_DEGRADED: 1,
        DPU_STATE_DISCONNECTED: 1,
      },
    };
    expect(fleetDpuCount(fs)).toBe(10);
    expect(fleetEniCount(fs)).toBe(41);
    expect(fleetVnetCount(fs)).toBe(14);
    expect(fleetDpuStates(fs)).toEqual(fs.dpus_by_state);
    expect(fleetHealthyDpus(fs)).toBe(8);
    expect(fleetDegradedDpus(fs)).toBe(1);
    expect(fleetDisconnectedDpus(fs)).toBe(1);
  });

  it("falls back to dpus.length for dpu_count", () => {
    const fs: FleetSummary = {
      dpus: [
        { id: "a", state: "DPU_STATE_UP" },
        { id: "b", state: "DPU_STATE_UP" },
        { id: "c", state: "DPU_STATE_UP" },
      ],
    };
    expect(fleetDpuCount(fs)).toBe(3);
  });

  it("falls back to legacy total_* fields", () => {
    const fs: FleetSummary = {
      total_dpus: 5,
      total_enis: 20,
      total_vnets: 7,
      healthy_dpus: 4,
      degraded_dpus: 1,
      disconnected_dpus: 0,
    };
    expect(fleetDpuCount(fs)).toBe(5);
    expect(fleetEniCount(fs)).toBe(20);
    expect(fleetVnetCount(fs)).toBe(7);
    expect(fleetHealthyDpus(fs)).toBe(4);
    expect(fleetDegradedDpus(fs)).toBe(1);
    expect(fleetDisconnectedDpus(fs)).toBe(0);
  });

  it("uses dpu_states alias when dpus_by_state absent", () => {
    const fs: FleetSummary = {
      dpu_states: { READY: 3, DRAINING: 1, DISCONNECTED: 1 },
    };
    expect(fleetDpuStates(fs)).toEqual(fs.dpu_states);
    expect(fleetHealthyDpus(fs)).toBe(3);
    expect(fleetDegradedDpus(fs)).toBe(1);
    expect(fleetDisconnectedDpus(fs)).toBe(1);
  });
});

describe("api-helpers · placementDpuIds", () => {
  it("returns [] for undefined", () => {
    expect(placementDpuIds(undefined)).toEqual([]);
  });
  it("returns [] for empty", () => {
    expect(placementDpuIds({})).toEqual([]);
  });
  it("reads modern placements[] shape", () => {
    const p: EniPlacement = {
      name: "eni-1",
      placements: [
        { dpu_id: "dpu-a", observed: true },
        { dpu_id: "dpu-b" },
      ],
    };
    expect(placementDpuIds(p)).toEqual(["dpu-a", "dpu-b"]);
  });
  it("falls back to legacy dpu_id field", () => {
    const p: EniPlacement = { eni_name: "eni-1", dpu_id: "dpu-c" };
    expect(placementDpuIds(p)).toEqual(["dpu-c"]);
  });
  it("ignores empty dpu_id strings in placements", () => {
    const p: EniPlacement = {
      name: "eni-1",
      placements: [{ dpu_id: "" }, { dpu_id: "dpu-x" }],
    };
    expect(placementDpuIds(p)).toEqual(["dpu-x"]);
  });
});

describe("api-helpers · eniPlacementCountsByDpu", () => {
  it("returns {} for undefined", () => {
    expect(eniPlacementCountsByDpu(undefined)).toEqual({});
  });
  it("returns {} for empty array", () => {
    expect(eniPlacementCountsByDpu([])).toEqual({});
  });
  it("counts modern shape correctly", () => {
    const list: EniPlacement[] = [
      { name: "eni-1", placements: [{ dpu_id: "dpu-a" }] },
      { name: "eni-2", placements: [{ dpu_id: "dpu-a" }] },
      { name: "eni-3", placements: [{ dpu_id: "dpu-b" }] },
    ];
    expect(eniPlacementCountsByDpu(list)).toEqual({
      "dpu-a": 2,
      "dpu-b": 1,
    });
  });
  it("counts an ENI hosted on multiple DPUs (HA) on both", () => {
    const list: EniPlacement[] = [
      {
        name: "eni-ha",
        placements: [
          { dpu_id: "dpu-a", observed: true },
          { dpu_id: "dpu-b", observed: true },
        ],
      },
    ];
    expect(eniPlacementCountsByDpu(list)).toEqual({
      "dpu-a": 1,
      "dpu-b": 1,
    });
  });
  it("handles legacy single-DPU placements", () => {
    const list: EniPlacement[] = [
      { eni_name: "eni-1", dpu_id: "dpu-c" },
      { eni_name: "eni-2", dpu_id: "dpu-c" },
    ];
    expect(eniPlacementCountsByDpu(list)).toEqual({ "dpu-c": 2 });
  });
});

describe("api-helpers · placementEniName", () => {
  it("returns modern `name`", () => {
    expect(placementEniName({ name: "eni-1" })).toBe("eni-1");
  });
  it("falls back to legacy `eni_name`", () => {
    expect(placementEniName({ eni_name: "eni-legacy" })).toBe("eni-legacy");
  });
  it("returns empty for neither", () => {
    expect(placementEniName({})).toBe("");
  });
});
