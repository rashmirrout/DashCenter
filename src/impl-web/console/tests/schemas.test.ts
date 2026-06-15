/* ═══════════════════════════════════════════════════════════════
 * Tests for the A-IF-rewritten zod schemas.
 *
 * These tests target the REAL dashd API shapes (post-A+), not the
 * pre-A+ aspirational shapes the old tests in this file used to
 * exercise. The fixtures are derived from
 * `deploy/test-setup/05-full-console/manifest/*.yaml`.
 *
 * Updated in A-IF1-G1.
 * ═══════════════════════════════════════════════════════════════ */

import { describe, it, expect } from "vitest";
import {
  vnetSchema,
  eniSchema,
  aclPolicySchema,
  routePolicySchema,
  vnetMappingSchema,
  serviceTunnelSchema,
  haSetSchema,
  simulateRequestSchema,
  RESOURCE_SCHEMAS,
} from "../src/lib/schemas";

/* ── Vnet ────────────────────────────────────────────────── */

describe("vnetSchema", () => {
  const valid = {
    metadata: { namespace: "default", name: "vnet-prod" },
    vni: 1000,
  };

  it("accepts a minimal valid vnet", () => {
    expect(vnetSchema.safeParse(valid).success).toBe(true);
  });

  it("accepts a vnet with gw_mac and labels", () => {
    const v = {
      ...valid,
      gw_mac: "aa:bb:cc:00:00:01",
      metadata: { ...valid.metadata, labels: { tenant: "bank" } },
    };
    expect(vnetSchema.safeParse(v).success).toBe(true);
  });

  it("rejects missing vni", () => {
    expect(
      vnetSchema.safeParse({ ...valid, vni: undefined }).success,
    ).toBe(false);
  });

  it("rejects vni out of range", () => {
    expect(vnetSchema.safeParse({ ...valid, vni: 0 }).success).toBe(false);
    expect(vnetSchema.safeParse({ ...valid, vni: 16777216 }).success).toBe(false);
  });

  it("rejects invalid gw_mac when provided", () => {
    expect(
      vnetSchema.safeParse({ ...valid, gw_mac: "not-a-mac" }).success,
    ).toBe(false);
  });

  it("rejects invalid name", () => {
    expect(
      vnetSchema.safeParse({
        ...valid,
        metadata: { ...valid.metadata, name: "Bad_Name!" },
      }).success,
    ).toBe(false);
  });

  it("does NOT require address_space (it's derived now)", () => {
    // Schema accepts payloads without address_space and ignores unknown keys.
    expect(vnetSchema.safeParse(valid).success).toBe(true);
  });
});

/* ── ENI ────────────────────────────────────────────────── */

describe("eniSchema", () => {
  const valid = {
    metadata: { namespace: "default", name: "eni-bank-web-01" },
    vnet_name: "vnet-prod",
    mac_address: "aa:bb:cc:dd:ee:ff",
    underlay_ip: "10.0.0.1",
    admin_state: "up" as const,
    placement_hint_dpu_ids: ["dpu-sim-01"],
  };

  it("accepts a valid eni", () => {
    expect(eniSchema.safeParse(valid).success).toBe(true);
  });

  it("accepts admin_state down", () => {
    expect(
      eniSchema.safeParse({ ...valid, admin_state: "down" as const }).success,
    ).toBe(true);
  });

  it("rejects invalid admin_state", () => {
    // Bypass the enum so we test runtime rejection of arbitrary strings.
    expect(
      eniSchema.safeParse({
        ...valid,
        admin_state: "UP" as unknown as "up" | "down",
      }).success,
    ).toBe(false);
  });

  it("rejects invalid mac", () => {
    expect(eniSchema.safeParse({ ...valid, mac_address: "bad" }).success).toBe(
      false,
    );
  });

  it("rejects invalid ip", () => {
    expect(eniSchema.safeParse({ ...valid, underlay_ip: "not-ip" }).success).toBe(
      false,
    );
  });
});

/* ── ACL Policy ─────────────────────────────────────────── */

describe("aclPolicySchema", () => {
  const valid = {
    metadata: { namespace: "default", name: "acl-bank-web-inbound" },
    stage: "inbound" as const,
    eni_names: ["eni-bank-web-01"],
    rules: [
      {
        priority: 100,
        action: "allow" as const,
        src_prefixes: ["0.0.0.0/0"],
        dst_ports: ["443"],
        protocols: ["tcp"],
        description: "https from internet",
      },
    ],
  };

  it("accepts a valid policy", () => {
    expect(aclPolicySchema.safeParse(valid).success).toBe(true);
  });

  it("accepts allow_and_continue action", () => {
    const v = {
      ...valid,
      rules: [{ ...valid.rules[0]!, action: "allow_and_continue" as const }],
    };
    expect(aclPolicySchema.safeParse(v).success).toBe(true);
  });

  it("rejects empty eni_names", () => {
    expect(
      aclPolicySchema.safeParse({ ...valid, eni_names: [] }).success,
    ).toBe(false);
  });

  it("rejects empty rules", () => {
    expect(
      aclPolicySchema.safeParse({ ...valid, rules: [] }).success,
    ).toBe(false);
  });

  it("rejects a rule with no constraints", () => {
    const empty = {
      ...valid,
      rules: [{ priority: 100, action: "allow" as const }],
    };
    expect(aclPolicySchema.safeParse(empty).success).toBe(false);
  });

  it("rejects duplicate priorities", () => {
    const dup = {
      ...valid,
      rules: [
        { ...valid.rules[0]!, priority: 100 },
        { ...valid.rules[0]!, priority: 100 },
      ],
    };
    expect(aclPolicySchema.safeParse(dup).success).toBe(false);
  });

  it("validates port spec format", () => {
    const okRange = {
      ...valid,
      rules: [{ ...valid.rules[0]!, dst_ports: ["7777-7800"] }],
    };
    expect(aclPolicySchema.safeParse(okRange).success).toBe(true);

    const badPort = {
      ...valid,
      rules: [{ ...valid.rules[0]!, dst_ports: ["abc"] }],
    };
    expect(aclPolicySchema.safeParse(badPort).success).toBe(false);

    const reversedRange = {
      ...valid,
      rules: [{ ...valid.rules[0]!, dst_ports: ["8080-80"] }],
    };
    expect(aclPolicySchema.safeParse(reversedRange).success).toBe(false);
  });

  it("rejects invalid stage", () => {
    expect(
      aclPolicySchema.safeParse({
        ...valid,
        stage: "ingress" as unknown as "inbound" | "outbound",
      }).success,
    ).toBe(false);
  });
});

/* ── Route Policy ───────────────────────────────────────── */

describe("routePolicySchema", () => {
  const validSingle = {
    metadata: { namespace: "default", name: "rp-1" },
    eni_names: ["eni-1"],
    routes: [
      {
        prefix: "0.0.0.0/0",
        next_hop_type: "vnet" as const,
        next_hop_target: "vnet-egress",
        metric: 100,
      },
    ],
  };

  it("accepts a valid single-hop policy", () => {
    expect(routePolicySchema.safeParse(validSingle).success).toBe(true);
  });

  it("accepts drop next-hop without a target", () => {
    const drop = {
      ...validSingle,
      routes: [
        {
          prefix: "198.18.0.0/15",
          next_hop_type: "drop" as const,
          metric: 5,
        },
      ],
    };
    expect(routePolicySchema.safeParse(drop).success).toBe(true);
  });

  it("rejects vnet next-hop without a target", () => {
    const bad = {
      ...validSingle,
      routes: [
        {
          prefix: "0.0.0.0/0",
          next_hop_type: "vnet" as const,
        },
      ],
    };
    expect(routePolicySchema.safeParse(bad).success).toBe(false);
  });

  it("accepts an ECMP fan-out", () => {
    const ecmp = {
      ...validSingle,
      routes: [
        {
          prefix: "198.51.100.0/24",
          ecmp_members: [
            { next_hop_type: "service_tunnel" as const, next_hop_target: "st-1", weight: 50 },
            { next_hop_type: "service_tunnel" as const, next_hop_target: "st-2", weight: 30 },
          ],
          metric: 15,
        },
      ],
    };
    expect(routePolicySchema.safeParse(ecmp).success).toBe(true);
  });

  it("rejects ECMP with only one member", () => {
    const ecmp = {
      ...validSingle,
      routes: [
        {
          prefix: "198.51.100.0/24",
          ecmp_members: [
            { next_hop_type: "service_tunnel" as const, next_hop_target: "st-1", weight: 50 },
          ],
        },
      ],
    };
    expect(routePolicySchema.safeParse(ecmp).success).toBe(false);
  });

  it("rejects empty eni_names", () => {
    expect(
      routePolicySchema.safeParse({ ...validSingle, eni_names: [] }).success,
    ).toBe(false);
  });
});

/* ── Vnet Mapping ───────────────────────────────────────── */

describe("vnetMappingSchema", () => {
  const validEncap = {
    metadata: { namespace: "default", name: "map-bank-web-01" },
    vnet_name: "vnet-prod",
    ip_address: "192.168.11.1",
    underlay_ip: "10.0.1.11",
    mac_address: "aa:bb:cc:01:00:01",
    action: "vnet_encap" as const,
  };

  it("accepts a valid vnet_encap mapping", () => {
    expect(vnetMappingSchema.safeParse(validEncap).success).toBe(true);
  });

  it("accepts a service_tunnel mapping with params.tunnel", () => {
    const v = {
      ...validEncap,
      action: "service_tunnel" as const,
      params: { tunnel: "st-internet-egress" },
    };
    expect(vnetMappingSchema.safeParse(v).success).toBe(true);
  });

  it("rejects a service_tunnel mapping without params.tunnel", () => {
    const v = { ...validEncap, action: "service_tunnel" as const };
    expect(vnetMappingSchema.safeParse(v).success).toBe(false);
  });

  it("rejects invalid action", () => {
    expect(
      vnetMappingSchema.safeParse({
        ...validEncap,
        action: "unknown" as unknown as "vnet_encap" | "service_tunnel",
      }).success,
    ).toBe(false);
  });
});

/* ── Service Tunnel ─────────────────────────────────────── */

describe("serviceTunnelSchema", () => {
  const valid = {
    metadata: { namespace: "default", name: "st-internet-egress" },
    local_underlay_ip: "10.0.0.1",
    remote_underlay_ip: "203.0.113.1",
    vni: 200,
    params: { action: "nat", nat_pool: "203.0.113.0/26" },
  };

  it("accepts a valid tunnel", () => {
    expect(serviceTunnelSchema.safeParse(valid).success).toBe(true);
  });

  it("rejects invalid local underlay IP", () => {
    expect(
      serviceTunnelSchema.safeParse({ ...valid, local_underlay_ip: "bad" })
        .success,
    ).toBe(false);
  });
});

/* ── HA Set ─────────────────────────────────────────────── */
/* Rewritten to match dashd's actual write-shape (mode + flat
 * member_dpu_ids[]). The earlier scope/members[].role shape was
 * silently dropped by dashd; see scripts/debug-spa-shape.py
 * test 2 for the empirical evidence. */

describe("haSetSchema", () => {
  const valid = {
    metadata: { namespace: "default", name: "ha-bank-prod" },
    mode: "active_standby" as const,
    member_dpu_ids: ["dpu-sim-01", "dpu-sim-02"],
  };

  it("accepts 2 member DPUs", () => {
    expect(haSetSchema.safeParse(valid).success).toBe(true);
  });

  it("accepts the optional virtual_ip + flow_sync_endpoints", () => {
    const v = {
      ...valid,
      virtual_ip: "10.0.0.100",
      flow_sync_endpoints: [
        "udp://dpu-sim-01:4789",
        "udp://dpu-sim-02:4789",
      ],
    };
    expect(haSetSchema.safeParse(v).success).toBe(true);
  });

  it("rejects less than 2 member DPUs", () => {
    expect(
      haSetSchema.safeParse({ ...valid, member_dpu_ids: ["dpu-sim-01"] })
        .success,
    ).toBe(false);
  });

  it("rejects duplicate member DPU ids", () => {
    expect(
      haSetSchema.safeParse({
        ...valid,
        member_dpu_ids: ["dpu-sim-01", "dpu-sim-01"],
      }).success,
    ).toBe(false);
  });

  it("rejects an invalid mode value", () => {
    expect(
      haSetSchema.safeParse({
        ...valid,
        mode: "rolling" as unknown as "active_standby" | "active_active",
      }).success,
    ).toBe(false);
  });

  it("rejects a malformed flow-sync endpoint", () => {
    expect(
      haSetSchema.safeParse({
        ...valid,
        flow_sync_endpoints: ["http://nope"],
      }).success,
    ).toBe(false);
  });
});

/* ── Simulate request (legacy compatibility) ───────────── */

describe("simulateRequestSchema", () => {
  it("accepts a valid request", () => {
    const v = {
      vnet_name: "vnet-prod",
      src_ip: "10.0.0.1",
      dst_ip: "10.0.0.2",
      protocol: 6,
      direction: "IN" as const,
    };
    expect(simulateRequestSchema.safeParse(v).success).toBe(true);
  });

  it("rejects invalid IPs", () => {
    const v = {
      vnet_name: "vnet-prod",
      src_ip: "bad",
      dst_ip: "10.0.0.2",
      protocol: 6,
      direction: "IN" as const,
    };
    expect(simulateRequestSchema.safeParse(v).success).toBe(false);
  });
});

/* ── Registry ─────────────────────────────────────────── */

describe("RESOURCE_SCHEMAS registry", () => {
  it("has all 7 kinds (slugs match dashd's router)", () => {
    // The `ha-sets` slug (NOT `ha`) is the canonical name — dashd's
    // router returns HTTP 405 for `/v1/{ns}/ha/{name}` on PUT but
    // accepts `/v1/{ns}/ha-sets/{name}`. The slug was corrected as
    // part of the A-IF wire-format fix.
    expect(Object.keys(RESOURCE_SCHEMAS).sort()).toEqual(
      [
        "acl-policies",
        "enis",
        "ha-sets",
        "route-policies",
        "service-tunnels",
        "vnet-mappings",
        "vnets",
      ].sort(),
    );
  });
});
