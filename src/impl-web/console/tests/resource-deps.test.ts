/* ═══════════════════════════════════════════════════════════════
 * resource-deps.test.ts — unit tests for the dependency module
 * ═══════════════════════════════════════════════════════════════ */

import { describe, it, expect } from "vitest";
import {
  DEPENDENCY_GRAPH,
  RESOURCE_CREATION_ORDER,
  KIND_LABELS,
  KIND_LABELS_PLURAL,
  emptySnapshot,
  getDependencies,
  getDependents,
  validateDependencies,
  rollupDependents,
  rollupAllDependents,
  upstreamKinds,
  downstreamKinds,
  type ResourceSnapshot,
} from "@/lib/resource-deps";
import type {
  AclPolicySpec,
  EniSpec,
  HaSetSpec,
  RoutePolicySpec,
  ServiceTunnelSpec,
  VnetMappingSpec,
  VnetSpec,
} from "@/api/types";

/* ── Test fixtures ──────────────────────────────────────────── */

function makeVnet(name: string, vni = 100): VnetSpec {
  return {
    metadata: { namespace: "default", name },
    vni,
    address_space: [],
  };
}

function makeEni(name: string, vnetName: string): EniSpec {
  return {
    metadata: { namespace: "default", name },
    vnet_name: vnetName,
    mac_address: "00:00:00:00:00:01",
    underlay_ip: "10.0.0.1",
  };
}

function makeTunnel(name: string): ServiceTunnelSpec {
  return {
    metadata: { namespace: "default", name },
    local_underlay_ip: "10.255.0.10",
    remote_underlay_ip: "10.255.0.20",
    vni: 8001,
  };
}

function makeMapping(
  name: string,
  vnetName: string,
  action: "vnet_encap" | "service_tunnel" = "vnet_encap",
  tunnelName?: string,
): VnetMappingSpec {
  return {
    metadata: { namespace: "default", name },
    vnet_name: vnetName,
    ip_address: "192.168.1.1",
    underlay_ip: "10.0.0.1",
    mac_address: "00:00:00:00:00:01",
    action,
    params: tunnelName ? { tunnel: tunnelName } : undefined,
  };
}

function makeAcl(name: string, eniNames: string[]): AclPolicySpec {
  return {
    metadata: { namespace: "default", name },
    stage: "inbound",
    eni_names: eniNames,
    rules: [{ priority: 100, action: "allow" }],
  };
}

function makeRoute(
  name: string,
  eniNames: string[],
  routes: Array<{
    prefix: string;
    next_hop_type?: string;
    next_hop_target?: string;
    ecmp_members?: Array<{ next_hop_type: string; next_hop_target?: string; weight?: number }>;
  }> = [],
): RoutePolicySpec {
  return {
    metadata: { namespace: "default", name },
    eni_names: eniNames,
    routes,
  };
}

function makeHaSet(name: string, dpus: string[]): HaSetSpec {
  return {
    metadata: { namespace: "default", name },
    mode: "active_standby",
    member_dpu_ids: dpus,
  };
}

/** Build a realistic, fully-wired snapshot like the bootstrap test setup. */
function fullSnapshot(): ResourceSnapshot {
  return {
    vnets: [makeVnet("vnet-prod"), makeVnet("vnet-staging")],
    "service-tunnels": [makeTunnel("st-egress"), makeTunnel("st-scrub")],
    enis: [
      makeEni("eni-prod-01", "vnet-prod"),
      makeEni("eni-prod-02", "vnet-prod"),
      makeEni("eni-stg-01", "vnet-staging"),
    ],
    "vnet-mappings": [
      makeMapping("map-prod-01", "vnet-prod"),
      makeMapping("map-prod-st", "vnet-prod", "service_tunnel", "st-egress"),
    ],
    "acl-policies": [
      makeAcl("acl-prod", ["eni-prod-01", "eni-prod-02"]),
      makeAcl("acl-stg", ["eni-stg-01"]),
    ],
    "route-policies": [
      makeRoute("rp-prod", ["eni-prod-01"], [
        { prefix: "0.0.0.0/0", next_hop_type: "service_tunnel", next_hop_target: "st-egress" },
        { prefix: "10.0.0.0/8", next_hop_type: "vnet", next_hop_target: "vnet-prod" },
      ]),
      makeRoute("rp-ecmp", ["eni-prod-02"], [
        {
          prefix: "198.51.100.0/24",
          ecmp_members: [
            { next_hop_type: "service_tunnel", next_hop_target: "st-egress", weight: 50 },
            { next_hop_type: "service_tunnel", next_hop_target: "st-scrub", weight: 50 },
          ],
        },
      ]),
    ],
    "ha-sets": [makeHaSet("ha-prod", ["dpu-01", "dpu-02"])],
  };
}

/* ── DAG structure tests ─────────────────────────────────── */

describe("DEPENDENCY_GRAPH", () => {
  it("has an entry for every resource kind", () => {
    for (const k of RESOURCE_CREATION_ORDER) {
      expect(DEPENDENCY_GRAPH[k]).toBeDefined();
    }
  });

  it("declares vnets and service-tunnels as roots", () => {
    expect(DEPENDENCY_GRAPH.vnets).toEqual([]);
    expect(DEPENDENCY_GRAPH["service-tunnels"]).toEqual([]);
    expect(DEPENDENCY_GRAPH["ha-sets"]).toEqual([]);
  });

  it("declares ENI as dependent on vnets", () => {
    expect(DEPENDENCY_GRAPH.enis).toContain("vnets");
  });

  it("declares vnet-mappings as dependent on vnets and tunnels", () => {
    expect(DEPENDENCY_GRAPH["vnet-mappings"]).toContain("vnets");
    expect(DEPENDENCY_GRAPH["vnet-mappings"]).toContain("service-tunnels");
  });

  it("declares acl-policies as dependent on enis", () => {
    expect(DEPENDENCY_GRAPH["acl-policies"]).toContain("enis");
  });

  it("declares route-policies as dependent on enis, vnets, tunnels", () => {
    expect(DEPENDENCY_GRAPH["route-policies"]).toContain("enis");
    expect(DEPENDENCY_GRAPH["route-policies"]).toContain("vnets");
    expect(DEPENDENCY_GRAPH["route-policies"]).toContain("service-tunnels");
  });
});

describe("RESOURCE_CREATION_ORDER", () => {
  it("places sources before sinks (topological)", () => {
    // vnets/tunnels are roots — should be in the first half
    const vnetIdx = RESOURCE_CREATION_ORDER.indexOf("vnets");
    const eniIdx = RESOURCE_CREATION_ORDER.indexOf("enis");
    const aclIdx = RESOURCE_CREATION_ORDER.indexOf("acl-policies");
    const routeIdx = RESOURCE_CREATION_ORDER.indexOf("route-policies");

    expect(vnetIdx).toBeLessThan(eniIdx);
    expect(eniIdx).toBeLessThan(aclIdx);
    expect(eniIdx).toBeLessThan(routeIdx);
  });

  it("places tunnels before mappings", () => {
    const tunIdx = RESOURCE_CREATION_ORDER.indexOf("service-tunnels");
    const mapIdx = RESOURCE_CREATION_ORDER.indexOf("vnet-mappings");
    expect(tunIdx).toBeLessThan(mapIdx);
  });
});

describe("KIND_LABELS / KIND_LABELS_PLURAL", () => {
  it("has labels for all kinds", () => {
    for (const k of RESOURCE_CREATION_ORDER) {
      expect(KIND_LABELS[k]).toBeTruthy();
      expect(KIND_LABELS_PLURAL[k]).toBeTruthy();
    }
  });
});

/* ── Forward dependency extraction ──────────────────────── */

describe("getDependencies", () => {
  it("returns empty for vnets (root)", () => {
    expect(getDependencies("vnets", makeVnet("v1"))).toEqual([]);
  });

  it("returns empty for service-tunnels (root)", () => {
    expect(getDependencies("service-tunnels", makeTunnel("t1"))).toEqual([]);
  });

  it("returns empty for ha-sets (no CRUD deps)", () => {
    expect(getDependencies("ha-sets", makeHaSet("ha1", ["dpu-01"]))).toEqual([]);
  });

  it("returns the vnet dep for an ENI", () => {
    const deps = getDependencies("enis", makeEni("eni1", "vnet-prod"));
    expect(deps).toHaveLength(1);
    expect(deps[0]).toEqual({ kind: "vnets", name: "vnet-prod", field: "vnet_name" });
  });

  it("skips empty vnet_name on ENI", () => {
    const deps = getDependencies("enis", makeEni("eni1", ""));
    expect(deps).toEqual([]);
  });

  it("returns vnet dep for vnet_encap mapping", () => {
    const m = makeMapping("m1", "vnet-prod", "vnet_encap");
    const deps = getDependencies("vnet-mappings", m);
    expect(deps).toHaveLength(1);
    expect(deps[0]!.kind).toBe("vnets");
  });

  it("returns vnet+tunnel deps for service_tunnel mapping", () => {
    const m = makeMapping("m1", "vnet-prod", "service_tunnel", "st-egress");
    const deps = getDependencies("vnet-mappings", m);
    expect(deps).toHaveLength(2);
    expect(deps.find((d) => d.kind === "vnets")?.name).toBe("vnet-prod");
    expect(deps.find((d) => d.kind === "service-tunnels")?.name).toBe("st-egress");
  });

  it("does not emit tunnel dep when action is vnet_encap (even if params.tunnel set)", () => {
    const m = makeMapping("m1", "vnet-prod", "vnet_encap", "st-egress");
    const deps = getDependencies("vnet-mappings", m);
    expect(deps.filter((d) => d.kind === "service-tunnels")).toHaveLength(0);
  });

  it("returns all eni deps for ACL policy", () => {
    const acl = makeAcl("a1", ["eni-1", "eni-2", "eni-3"]);
    const deps = getDependencies("acl-policies", acl);
    expect(deps).toHaveLength(3);
    expect(deps.every((d) => d.kind === "enis")).toBe(true);
  });

  it("returns eni + single-hop next_hop deps for route policy", () => {
    const rp = makeRoute("rp1", ["eni-1"], [
      { prefix: "0.0.0.0/0", next_hop_type: "vnet", next_hop_target: "vnet-prod" },
      { prefix: "10.0.0.0/8", next_hop_type: "service_tunnel", next_hop_target: "st-1" },
      { prefix: "172.16.0.0/12", next_hop_type: "drop" }, // no target
    ]);
    const deps = getDependencies("route-policies", rp);
    // 1 eni + 1 vnet + 1 tunnel = 3
    expect(deps).toHaveLength(3);
    expect(deps.find((d) => d.kind === "enis")).toBeDefined();
    expect(deps.find((d) => d.kind === "vnets")).toBeDefined();
    expect(deps.find((d) => d.kind === "service-tunnels")).toBeDefined();
  });

  it("returns ecmp_members deps for route policy", () => {
    const rp = makeRoute("rp1", ["eni-1"], [
      {
        prefix: "0.0.0.0/0",
        ecmp_members: [
          { next_hop_type: "service_tunnel", next_hop_target: "st-1", weight: 50 },
          { next_hop_type: "service_tunnel", next_hop_target: "st-2", weight: 50 },
          { next_hop_type: "vnet", next_hop_target: "vnet-prod", weight: 100 },
        ],
      },
    ]);
    const deps = getDependencies("route-policies", rp);
    // 1 eni + 2 tunnels + 1 vnet = 4
    expect(deps).toHaveLength(4);
    expect(deps.filter((d) => d.kind === "service-tunnels")).toHaveLength(2);
    expect(deps.filter((d) => d.kind === "vnets")).toHaveLength(1);
  });
});

/* ── Reverse dependency lookup ──────────────────────────── */

describe("getDependents", () => {
  it("finds ENI dependents of a vnet", () => {
    const snap = fullSnapshot();
    const deps = getDependents("vnets", "vnet-prod", snap);
    // 2 ENIs + 2 mappings + 1 route-policy = 5 dependents that name vnet-prod
    const enis = deps.filter((d) => d.kind === "enis");
    expect(enis).toHaveLength(2);
    expect(enis.map((d) => d.name).sort()).toEqual(["eni-prod-01", "eni-prod-02"]);
  });

  it("finds mapping dependents of a vnet", () => {
    const snap = fullSnapshot();
    const deps = getDependents("vnets", "vnet-prod", snap);
    const maps = deps.filter((d) => d.kind === "vnet-mappings");
    expect(maps).toHaveLength(2);
  });

  it("finds route-policy dependents of a vnet (via next_hop_target)", () => {
    const snap = fullSnapshot();
    const deps = getDependents("vnets", "vnet-prod", snap);
    const routes = deps.filter((d) => d.kind === "route-policies");
    expect(routes).toHaveLength(1);
    expect(routes[0]!.name).toBe("rp-prod");
  });

  it("finds mapping + route-policy dependents of a service-tunnel", () => {
    const snap = fullSnapshot();
    const deps = getDependents("service-tunnels", "st-egress", snap);
    // 1 mapping (map-prod-st) + 1 route single-hop (rp-prod) + 1 ecmp (rp-ecmp) = 3
    expect(deps.length).toBeGreaterThanOrEqual(3);
    expect(deps.some((d) => d.kind === "vnet-mappings")).toBe(true);
    expect(deps.some((d) => d.kind === "route-policies")).toBe(true);
  });

  it("finds ACL + route-policy dependents of an ENI", () => {
    const snap = fullSnapshot();
    const deps = getDependents("enis", "eni-prod-01", snap);
    expect(deps.some((d) => d.kind === "acl-policies")).toBe(true);
    expect(deps.some((d) => d.kind === "route-policies")).toBe(true);
  });

  it("returns empty for unreferenced resources", () => {
    const snap = fullSnapshot();
    const deps = getDependents("vnets", "no-such-vnet", snap);
    expect(deps).toEqual([]);
  });

  it("includes the field path in each dependent", () => {
    const snap = fullSnapshot();
    const deps = getDependents("service-tunnels", "st-egress", snap);
    const ecmpDep = deps.find((d) => d.field.includes("ecmp_members"));
    expect(ecmpDep).toBeDefined();
  });
});

/* ── Validation ──────────────────────────────────────────── */

describe("validateDependencies", () => {
  it("returns no issues when all references exist", () => {
    const snap = fullSnapshot();
    const eni = makeEni("eni-new", "vnet-prod");
    const issues = validateDependencies("enis", eni, snap);
    expect(issues).toEqual([]);
  });

  it("returns an error when ENI references a missing vnet", () => {
    const snap = fullSnapshot();
    const eni = makeEni("eni-new", "vnet-nonexistent");
    const issues = validateDependencies("enis", eni, snap);
    expect(issues).toHaveLength(1);
    expect(issues[0]!.severity).toBe("error");
    expect(issues[0]!.message).toContain("vnet-nonexistent");
  });

  it("returns multiple errors when ACL references missing ENIs", () => {
    const snap = fullSnapshot();
    const acl = makeAcl("a-new", ["eni-real", "eni-fake-1", "eni-fake-2"]);
    // Add eni-real so we have a mix
    snap.enis.push(makeEni("eni-real", "vnet-prod"));
    const issues = validateDependencies("acl-policies", acl, snap);
    expect(issues).toHaveLength(2);
    expect(issues.map((i) => i.ref.name).sort()).toEqual(["eni-fake-1", "eni-fake-2"]);
  });

  it("validates service_tunnel mappings against tunnel existence", () => {
    const snap = fullSnapshot();
    const m = makeMapping("m-new", "vnet-prod", "service_tunnel", "st-fake");
    const issues = validateDependencies("vnet-mappings", m, snap);
    expect(issues).toHaveLength(1);
    expect(issues[0]!.ref.kind).toBe("service-tunnels");
  });

  it("returns no issues for a self-contained vnet", () => {
    const snap = emptySnapshot();
    const v = makeVnet("v-new");
    const issues = validateDependencies("vnets", v, snap);
    expect(issues).toEqual([]);
  });
});

/* ── Rollups ─────────────────────────────────────────────── */

describe("rollupDependents", () => {
  it("counts dependents by kind", () => {
    const snap = fullSnapshot();
    const info = rollupDependents("vnets", "vnet-prod", snap);
    expect(info.byKind.enis).toBe(2);
    expect(info.byKind["vnet-mappings"]).toBe(2);
    expect(info.byKind["route-policies"]).toBe(1);
    // total = 5
    expect(info.totalDependents).toBeGreaterThanOrEqual(5);
  });

  it("returns zero counts for unreferenced resources", () => {
    const snap = fullSnapshot();
    const info = rollupDependents("vnets", "vnet-orphan", snap);
    expect(info.totalDependents).toBe(0);
    expect(info.byKind).toEqual({});
  });

  it("can include the full dependent list", () => {
    const snap = fullSnapshot();
    const info = rollupDependents("vnets", "vnet-prod", snap, true);
    expect(info.dependents).toBeDefined();
    expect(info.dependents!.length).toBe(info.totalDependents);
  });

  it("omits the full list by default", () => {
    const snap = fullSnapshot();
    const info = rollupDependents("vnets", "vnet-prod", snap);
    expect(info.dependents).toBeUndefined();
  });
});

describe("rollupAllDependents", () => {
  it("returns a map keyed by resource name", () => {
    const snap = fullSnapshot();
    const map = rollupAllDependents("vnets", snap);
    expect(map.has("vnet-prod")).toBe(true);
    expect(map.has("vnet-staging")).toBe(true);
    expect(map.get("vnet-prod")!.totalDependents).toBeGreaterThan(0);
  });

  it("handles empty snapshots gracefully", () => {
    const snap = emptySnapshot();
    const map = rollupAllDependents("vnets", snap);
    expect(map.size).toBe(0);
  });
});

/* ── Graph helpers ───────────────────────────────────────── */

describe("upstreamKinds / downstreamKinds", () => {
  it("upstreamKinds matches DEPENDENCY_GRAPH", () => {
    expect(upstreamKinds("enis")).toEqual(["vnets"]);
    expect(upstreamKinds("vnets")).toEqual([]);
  });

  it("downstreamKinds finds reverse edges", () => {
    const down = downstreamKinds("vnets");
    expect(down).toContain("enis");
    expect(down).toContain("vnet-mappings");
    expect(down).toContain("route-policies");
  });

  it("downstreamKinds of ha-sets is empty", () => {
    expect(downstreamKinds("ha-sets")).toEqual([]);
  });

  it("downstreamKinds of route-policies is empty (route-policies are leaves)", () => {
    expect(downstreamKinds("route-policies")).toEqual([]);
  });
});

/* ── Empty snapshot ─────────────────────────────────────── */

describe("emptySnapshot", () => {
  it("returns a snapshot with empty arrays for every kind", () => {
    const snap = emptySnapshot();
    for (const k of RESOURCE_CREATION_ORDER) {
      expect(snap[k]).toEqual([]);
    }
  });
});