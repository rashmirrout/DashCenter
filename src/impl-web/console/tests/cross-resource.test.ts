import { describe, it, expect } from "vitest";
import {
  buildCrossResourceIndex,
  dpuCrossCounts,
  eniCrossCounts,
  inferVnetOverlayCidrs,
  inferVnetUnderlayCidrs,
} from "../src/lib/cross-resource";
import type {
  AclPolicySpec,
  EniPlacement,
  EniSpec,
  RoutePolicySpec,
  ServiceTunnelSpec,
  VnetMappingSpec,
} from "../src/api/types";

function eni(name: string, vnet: string, ip = "10.0.0.1", dpuHints: string[] = []): EniSpec {
  return {
    metadata: { namespace: "default", name },
    vnet_name: vnet,
    mac_address: "aa:bb:cc:00:00:00",
    underlay_ip: ip,
    placement_hint_dpu_ids: dpuHints,
  };
}
function placement(name: string, vnet: string, dpus: string[]): EniPlacement {
  return {
    name,
    vnet_name: vnet,
    placements: dpus.map((dpu_id) => ({ dpu_id, observed: true })),
  };
}
function acl(name: string, enis: string[]): AclPolicySpec {
  return {
    metadata: { namespace: "default", name },
    eni_names: enis,
    rules: [],
  };
}
function route(name: string, enis: string[], tunnels: string[] = []): RoutePolicySpec {
  return {
    metadata: { namespace: "default", name },
    eni_names: enis,
    routes: tunnels.map((t) => ({
      prefix: "0.0.0.0/0",
      next_hop_type: "service_tunnel",
      next_hop_target: t,
    })),
  };
}
function tunnel(name: string): ServiceTunnelSpec {
  return { metadata: { namespace: "default", name } };
}
function mapping(name: string, vnet: string, overlay: string): VnetMappingSpec {
  return {
    metadata: { namespace: "default", name },
    vnet_name: vnet,
    ip_address: overlay,
    underlay_ip: "10.0.0.1",
    mac_address: "aa:bb:cc:dd:ee:ff",
  };
}

describe("cross-resource · buildCrossResourceIndex", () => {
  it("returns empty maps for empty input", () => {
    const idx = buildCrossResourceIndex({});
    expect(idx.enisByDpu.size).toBe(0);
    expect(idx.aclsByEni.size).toBe(0);
    expect(idx.routesByEni.size).toBe(0);
    expect(idx.tunnelsByEni.size).toBe(0);
  });

  it("places ENIs by placement (authoritative)", () => {
    const idx = buildCrossResourceIndex({
      enis: [eni("e1", "v1")],
      placements: [placement("e1", "v1", ["d1", "d2"])],
    });
    expect(idx.enisByDpu.get("d1")?.length).toBe(1);
    expect(idx.enisByDpu.get("d2")?.length).toBe(1);
  });

  it("falls back to placement_hint_dpu_ids when no placements", () => {
    const idx = buildCrossResourceIndex({
      enis: [eni("e1", "v1", "10.0.0.1", ["d3"])],
    });
    expect(idx.enisByDpu.get("d3")?.length).toBe(1);
  });

  it("indexes acls by ENI", () => {
    const idx = buildCrossResourceIndex({
      acls: [acl("acl-1", ["e1", "e2"]), acl("acl-2", ["e1"])],
    });
    expect(idx.aclsByEni.get("e1")?.length).toBe(2);
    expect(idx.aclsByEni.get("e2")?.length).toBe(1);
  });

  it("links tunnels to ENIs via route next-hop", () => {
    const idx = buildCrossResourceIndex({
      routes: [route("rp", ["e1"], ["st-egress"])],
      tunnels: [tunnel("st-egress")],
    });
    expect(idx.tunnelsByEni.get("e1")?.length).toBe(1);
  });

  it("links tunnels to ENIs via vnet-mapping service_tunnel action", () => {
    const idx = buildCrossResourceIndex({
      enis: [eni("e1", "shared-egress"), eni("e2", "shared-egress")],
      tunnels: [tunnel("st-egress")],
      vnetMappings: [
        {
          metadata: { namespace: "default", name: "m1" },
          vnet_name: "shared-egress",
          underlay_ip: "10.0.0.1",
          mac_address: "aa:bb:cc:dd:ee:ff",
          action: "service_tunnel",
          params: { tunnel: "st-egress" },
        },
      ],
    });
    expect(idx.tunnelsByEni.get("e1")?.length).toBe(1);
    expect(idx.tunnelsByEni.get("e2")?.length).toBe(1);
  });

  it("aggregates mappings by vnet", () => {
    const idx = buildCrossResourceIndex({
      vnetMappings: [
        mapping("m1", "v1", "10.1.1.1"),
        mapping("m2", "v1", "10.1.1.2"),
        mapping("m3", "v2", "10.2.2.2"),
      ],
    });
    expect(idx.mappingsByVnet.get("v1")?.length).toBe(2);
    expect(idx.mappingsByVnet.get("v2")?.length).toBe(1);
  });
});

describe("cross-resource · dpuCrossCounts", () => {
  it("zero counts for unknown DPU", () => {
    const idx = buildCrossResourceIndex({});
    const c = dpuCrossCounts(idx, "missing");
    expect(c.eniCount).toBe(0);
    expect(c.vnetCount).toBe(0);
    expect(c.aclCount).toBe(0);
    expect(c.routeCount).toBe(0);
    expect(c.tunnelCount).toBe(0);
  });

  it("counts cross-resources per DPU", () => {
    const idx = buildCrossResourceIndex({
      enis: [eni("e1", "v1"), eni("e2", "v2")],
      placements: [
        placement("e1", "v1", ["d1"]),
        placement("e2", "v2", ["d1"]),
      ],
      acls: [acl("a1", ["e1", "e2"])],
      routes: [route("r1", ["e1"], ["t1"])],
      tunnels: [tunnel("t1")],
    });
    const c = dpuCrossCounts(idx, "d1");
    expect(c.eniCount).toBe(2);
    expect(c.vnetCount).toBe(2);
    expect(c.aclCount).toBe(1);
    expect(c.routeCount).toBe(1);
    expect(c.tunnelCount).toBe(1);
  });
});

describe("cross-resource · eniCrossCounts", () => {
  it("returns zeros for an unknown ENI", () => {
    const idx = buildCrossResourceIndex({});
    const c = eniCrossCounts(idx, "missing");
    expect(c.aclCount).toBe(0);
    expect(c.routeCount).toBe(0);
    expect(c.tunnelCount).toBe(0);
  });

  it("returns per-ENI counts", () => {
    const idx = buildCrossResourceIndex({
      acls: [acl("a1", ["e1"]), acl("a2", ["e1"])],
      routes: [route("r1", ["e1"], ["t1"])],
      tunnels: [tunnel("t1")],
    });
    const c = eniCrossCounts(idx, "e1");
    expect(c.aclCount).toBe(2);
    expect(c.routeCount).toBe(1);
    expect(c.tunnelCount).toBe(1);
  });
});

describe("cross-resource · inferVnet…Cidrs", () => {
  it("groups underlay IPs to /24s", () => {
    const cidrs = inferVnetUnderlayCidrs([
      eni("e1", "v1", "10.0.1.11"),
      eni("e2", "v1", "10.0.1.12"),
      eni("e3", "v1", "10.0.2.5"),
    ]);
    expect(cidrs.sort()).toEqual(["10.0.1.0/24", "10.0.2.0/24"]);
  });

  it("ignores malformed IPs", () => {
    const cidrs = inferVnetUnderlayCidrs([
      eni("e1", "v1", "not-an-ip"),
      eni("e2", "v1", "10.0.0.1"),
    ]);
    expect(cidrs).toEqual(["10.0.0.0/24"]);
  });

  it("groups overlay IPs from mappings", () => {
    const cidrs = inferVnetOverlayCidrs([
      mapping("m1", "v1", "192.168.1.10"),
      mapping("m2", "v1", "192.168.1.11"),
      mapping("m3", "v1", "192.168.2.1"),
    ]);
    expect(cidrs.sort()).toEqual(["192.168.1.0/24", "192.168.2.0/24"]);
  });

  it("returns [] for empty inputs", () => {
    expect(inferVnetUnderlayCidrs([])).toEqual([]);
    expect(inferVnetOverlayCidrs([])).toEqual([]);
  });
});