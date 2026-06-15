/* ═══════════════════════════════════════════════════════════════
 * dashw zod schemas — REWRITTEN to match the real dashd API.
 *
 * Aligned with `src/api/types.ts` after the A+ shape-alignment work.
 * Every schema below mirrors the **wire format** dashd actually
 * accepts/returns, not the older aspirational types that pre-A+
 * code used.
 *
 * Key shape changes from the previous version of this file:
 *
 *   • Vnet: dropped `address_space` (derived from mappings/ENIs)
 *   • AclPolicy: `stage` (not `direction`); no `default_action`
 *   • AclRule: lowercase action enum; arrays of strings for
 *              src_prefixes / dst_prefixes / src_ports / dst_ports
 *              / protocols; description field
 *   • RoutePolicy: `routes[]` (not `rules[]`)
 *   • RouteEntry: prefix + next_hop_type/target + optional
 *                 ecmp_members[]
 *   • VnetMapping: `ip_address` (not `overlay_ip`); `action` enum
 *                  with optional `params.tunnel`
 *   • ServiceTunnel: local_underlay_ip / remote_underlay_ip / vni
 *                    / params (not source_vnet/destination_vnet)
 *   • HaSet: members[] min 2, validated for unique dpu_id
 *
 * The schemas here drive form validation in the new `/resources`
 * page (A-IF). They also re-export through `RESOURCE_SCHEMAS`
 * for any dynamic-form consumer.
 * ═══════════════════════════════════════════════════════════════ */

import { z } from "zod";

/* ── Primitive validators ───────────────────────────────────── */

/** RFC 1123-ish: lowercase alphanumeric + dashes, 1..253 chars,
 *  doesn't start or end with a dash. dashd accepts most strings
 *  but the convention across the fleet is dns-safe. */
const dnsName = z
  .string()
  .min(1, "Name required")
  .max(253)
  .regex(
    /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/,
    "Lowercase alphanumeric + dashes; must start/end with alphanumeric",
  );

const ipv4 = z
  .string()
  .regex(
    /^(\d{1,3}\.){3}\d{1,3}$/,
    "Invalid IPv4 address (expected e.g. 10.0.1.11)",
  );

const ipv4Cidr = z
  .string()
  .regex(
    /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/,
    "Invalid CIDR (expected e.g. 10.0.0.0/24)",
  );

const macAddress = z
  .string()
  .regex(
    /^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$/,
    "Invalid MAC (expected xx:xx:xx:xx:xx:xx)",
  );

/** Port spec is a string: either a single port "443" or a range
 *  "7777-7800". dashd accepts both forms; rejecting numbers here
 *  matches what we see in `deploy/test-setup/05-full-console/manifest/`. */
const portSpec = z
  .string()
  .regex(/^\d+(-\d+)?$/, "Port must be a number or range like 7777-7800")
  .refine(
    (s) => {
      const [a, b] = s.split("-").map((x) => Number.parseInt(x, 10));
      const aOk = a !== undefined && Number.isFinite(a) && a >= 0 && a <= 65535;
      if (b === undefined) return aOk;
      const bOk = Number.isFinite(b) && b >= 0 && b <= 65535;
      return aOk && bOk && a! <= b!;
    },
    { message: "Ports must be 0–65535 and start ≤ end" },
  );

/** Protocols may be names (`tcp`/`udp`/`icmp`) OR numeric strings
 *  (`"6"`, `"17"`). dashd is permissive. */
const protocol = z
  .string()
  .min(1)
  .refine(
    (s) => /^(tcp|udp|icmp|esp|gre|ah|sctp|\d{1,3})$/i.test(s),
    "Use `tcp`/`udp`/`icmp` or a numeric protocol",
  );

/* ── Common metadata ────────────────────────────────────────── */

export const objectMetaSchema = z.object({
  namespace: z.string().min(1, "Namespace required"),
  name: dnsName,
  labels: z.record(z.string()).optional(),
  annotations: z.record(z.string()).optional(),
  // `generation` may arrive as a number or as a string depending on
  // the upstream path (clone uses the wire value as-is; create
  // omits it entirely). Coercion is harmless for numeric inputs.
  generation: z.coerce.number().int().optional(),
});

export type ObjectMetaInput = z.infer<typeof objectMetaSchema>;

/* ── Vnet ───────────────────────────────────────────────────── */
/* Real dashd shape (per `manifest/00-vnets.yaml`):
 *   metadata.name + spec.vni + (optional) labels / gw_mac
 *
 * NOTE: no `address_space` — it's derived from vnet-mappings
 * (overlay CIDRs) + ENI underlay IPs. The list/detail views
 * compute it via `inferVnetOverlayCidrs` / `inferVnetUnderlayCidrs`. */

export const vnetSchema = z.object({
  metadata: objectMetaSchema,
  // VNI comes from <input type="number">. Form fields coerce on
  // change, but `z.coerce.number()` is a defensive guard against
  // clone/edit paths that may carry the value through as a string.
  vni: z.coerce
    .number()
    .int()
    .min(1, "VNI must be ≥ 1")
    .max(16_777_215, "VNI must be ≤ 16,777,215"),
  gw_mac: macAddress.optional().or(z.literal("")),
});

export type VnetInput = z.infer<typeof vnetSchema>;

/* ── ENI ────────────────────────────────────────────────────── */
/* Real dashd shape (per `manifest/02-enis.yaml`):
 *   spec.vnet_name + spec.mac_address + spec.underlay_ip
 *   + spec.admin_state ("up"/"down") + spec.placement_hint_dpu_ids[]
 *   + (optional) labels */

export const eniSchema = z.object({
  metadata: objectMetaSchema,
  vnet_name: dnsName,
  mac_address: macAddress,
  underlay_ip: ipv4,
  admin_state: z.enum(["up", "down"]).optional(),
  placement_hint_dpu_ids: z
    .array(z.string().min(1))
    .min(1, "At least one DPU is required as a placement hint")
    .optional(),
  resimulate_flows: z.boolean().optional(),
});

export type EniInput = z.infer<typeof eniSchema>;

/* ── ACL Policy ─────────────────────────────────────────────── */
/* Real dashd shape (per `manifest/05-acl-policies.yaml`):
 *   spec.stage ("inbound"/"outbound")
 *   spec.eni_names[]
 *   spec.rules[] — each rule has:
 *     - priority (1..65535)
 *     - action ("allow"/"deny"/"allow_and_continue")
 *     - description (optional)
 *     - src_prefixes[]   (optional)
 *     - dst_prefixes[]   (optional)
 *     - src_ports[]      (optional, string format)
 *     - dst_ports[]      (optional, string format)
 *     - protocols[]      (optional, "tcp"/"udp"/"icmp"/numeric)
 *   The dashd wire format does NOT include `default_action`. */

export const aclRuleSchema = z
  .object({
    priority: z.coerce
      .number()
      .int()
      .min(1, "Priority must be ≥ 1")
      .max(65535, "Priority must be ≤ 65535"),
    action: z.enum(["allow", "deny", "allow_and_continue"]),
    description: z.string().optional(),
    src_prefixes: z.array(ipv4Cidr).optional(),
    dst_prefixes: z.array(ipv4Cidr).optional(),
    src_ports: z.array(portSpec).optional(),
    dst_ports: z.array(portSpec).optional(),
    protocols: z.array(protocol).optional(),
  })
  .refine(
    (r) =>
      (r.src_prefixes?.length ?? 0) +
        (r.dst_prefixes?.length ?? 0) +
        (r.src_ports?.length ?? 0) +
        (r.dst_ports?.length ?? 0) +
        (r.protocols?.length ?? 0) >
      0,
    {
      message:
        "Each ACL rule must constrain at least one of: src/dst prefixes, src/dst ports, or protocols",
    },
  );

export type AclRuleInput = z.infer<typeof aclRuleSchema>;

export const aclPolicySchema = z.object({
  metadata: objectMetaSchema,
  stage: z.enum(["inbound", "outbound"]),
  eni_names: z
    .array(dnsName)
    .min(1, "At least one ENI must be bound to this policy"),
  rules: z
    .array(aclRuleSchema)
    .min(1, "At least one rule is required")
    .refine(
      (rules) => {
        const seen = new Set<number>();
        for (const r of rules) {
          if (seen.has(r.priority)) return false;
          seen.add(r.priority);
        }
        return true;
      },
      { message: "Rule priorities must be unique" },
    ),
});

export type AclPolicyInput = z.infer<typeof aclPolicySchema>;

/* ── Route Policy ───────────────────────────────────────────── */
/* Real dashd shape (per `manifest/10-advanced-routes.yaml`):
 *   spec.eni_names[]
 *   spec.routes[] — each route has:
 *     - prefix (CIDR)
 *     - next_hop_type ("vnet"/"service_tunnel"/"drop")
 *     - next_hop_target (name; required if type != "drop")
 *     - metric (optional)
 *     - description (optional)
 *     - ecmp_members[] (optional; when present, replaces
 *       single next-hop with a weighted fan-out)
 *
 *   Each ecmp_member has: next_hop_type + next_hop_target + weight */

export const ecmpMemberSchema = z.object({
  next_hop_type: z.enum(["vnet", "service_tunnel"]),
  next_hop_target: dnsName,
  weight: z.coerce
    .number()
    .int()
    .min(1, "ECMP weight must be ≥ 1")
    .max(255, "ECMP weight must be ≤ 255"),
});

export type EcmpMemberInput = z.infer<typeof ecmpMemberSchema>;

export const routeEntrySchema = z
  .object({
    prefix: ipv4Cidr,
    next_hop_type: z.enum(["vnet", "service_tunnel", "drop"]).optional(),
    next_hop_target: dnsName.optional().or(z.literal("")),
    metric: z.coerce.number().int().min(0).max(65535).optional(),
    description: z.string().optional(),
    ecmp_members: z.array(ecmpMemberSchema).min(2).optional(),
  })
  .refine(
    (r) => {
      // If ECMP is used, must NOT also have a single next_hop_target,
      // and `next_hop_type` is implicit.
      if (r.ecmp_members && r.ecmp_members.length > 0) return true;
      // Single-hop: require next_hop_type. `drop` doesn't need target.
      if (!r.next_hop_type) return false;
      if (r.next_hop_type === "drop") return true;
      return !!r.next_hop_target;
    },
    {
      message:
        "Either set next_hop_type (and next_hop_target unless `drop`) OR provide ≥2 ecmp_members",
    },
  );

export type RouteEntryInput = z.infer<typeof routeEntrySchema>;

export const routePolicySchema = z.object({
  metadata: objectMetaSchema,
  eni_names: z.array(dnsName).min(1, "At least one ENI is required"),
  routes: z.array(routeEntrySchema).min(1, "At least one route is required"),
});

export type RoutePolicyInput = z.infer<typeof routePolicySchema>;

/* ── Vnet Mapping ───────────────────────────────────────────── */
/* Real dashd shape (per `manifest/03-vnet-mappings.yaml`):
 *   spec.vnet_name
 *   spec.ip_address    (overlay IP — note the field name is
 *                       `ip_address`, not `overlay_ip`)
 *   spec.underlay_ip
 *   spec.mac_address
 *   spec.action        ("vnet_encap" | "service_tunnel")
 *   spec.params        (optional; for action=service_tunnel,
 *                       params.tunnel names the ServiceTunnel) */

export const vnetMappingSchema = z
  .object({
    metadata: objectMetaSchema,
    vnet_name: dnsName,
    ip_address: ipv4, // overlay IP
    underlay_ip: ipv4,
    mac_address: macAddress,
    action: z.enum(["vnet_encap", "service_tunnel"]),
    params: z.record(z.string()).optional(),
  })
  .refine(
    (m) =>
      m.action !== "service_tunnel" ||
      (m.params && typeof m.params.tunnel === "string" && m.params.tunnel.length > 0),
    {
      message:
        "When action=service_tunnel, params.tunnel must name an existing ServiceTunnel",
      path: ["params", "tunnel"],
    },
  );

export type VnetMappingInput = z.infer<typeof vnetMappingSchema>;

/* ── Service Tunnel ─────────────────────────────────────────── */
/* Real dashd shape (per `manifest/01-service-tunnels.yaml`):
 *   spec.local_underlay_ip
 *   spec.remote_underlay_ip
 *   spec.vni
 *   spec.params (key/value map: action, mtu, nat_pool, …) */

export const serviceTunnelSchema = z.object({
  metadata: objectMetaSchema,
  local_underlay_ip: ipv4,
  remote_underlay_ip: ipv4,
  // Coerced for the same reason as `vnetSchema.vni`.
  vni: z.coerce.number().int().min(1).max(16_777_215),
  params: z.record(z.string()).optional(),
});

export type ServiceTunnelInput = z.infer<typeof serviceTunnelSchema>;

/* ── HA Set ─────────────────────────────────────────────────── */
/* Real dashd write-shape (per `manifest/06-ha-sets.yaml` and the
 * bootstrap.py probe at scripts/debug-spa-shape.py test 2):
 *
 *   spec.mode                  : "active_standby" | "active_active"
 *   spec.member_dpu_ids[]      : flat list of DPU IDs (min 2)
 *   spec.virtual_ip            : optional VIP
 *   spec.flow_sync_endpoints[] : optional sync targets
 *   spec.labels                : optional kv map
 *
 * The earlier `scope` + `members[].dpu_id+role` shape was a
 * misreading of the proto — dashd silently dropped both fields on
 * write. The HA *role* isn't tracked per-member by dashd; the
 * `mode` is set at the HaSet level and dashd resolves leader at
 * runtime via the cluster controller. */

const HA_MODES = ["active_standby", "active_active"] as const;

/** Optional sync-endpoint format check: `udp://host:port` or
 *  `tcp://host:port`. dashd is permissive but we lint the form. */
const flowSyncEndpoint = z
  .string()
  .regex(
    /^(udp|tcp):\/\/[^\s:]+:\d{1,5}$/,
    "Endpoint must be `udp://host:port` or `tcp://host:port`",
  );

export const haSetSchema = z
  .object({
    metadata: objectMetaSchema,
    mode: z.enum(HA_MODES),
    member_dpu_ids: z
      .array(z.string().min(1, "DPU ID required"))
      .min(2, "HA Set requires at least 2 member DPUs"),
    virtual_ip: ipv4.optional().or(z.literal("")),
    flow_sync_endpoints: z.array(flowSyncEndpoint).optional(),
  })
  .refine(
    (s) => new Set(s.member_dpu_ids).size === s.member_dpu_ids.length,
    {
      message: "Each member DPU id must be unique",
      path: ["member_dpu_ids"],
    },
  );

export type HaSetInput = z.infer<typeof haSetSchema>;

/* ── Legacy alias retained for old call-sites that import it ─── */
export const haSetMemberSchema = z.object({
  dpu_id: z.string().min(1, "DPU ID required"),
  role: z.enum(["ACTIVE", "STANDBY", "ACTIVE_ACTIVE", "WITNESS"]),
});

export type HaSetMemberInput = z.infer<typeof haSetMemberSchema>;

/* ── Simulate Request (legacy — kept for compatibility) ─────── */

export const simulateRequestSchema = z.object({
  vnet_name: z.string().min(1, "Vnet name required"),
  src_ip: ipv4,
  dst_ip: ipv4,
  protocol: z.number().int().min(0).max(255),
  src_port: z.number().int().min(0).max(65535).optional(),
  dst_port: z.number().int().min(0).max(65535).optional(),
  direction: z.enum(["IN", "OUT"]),
  eni_name: z.string().optional(),
});

export type SimulateRequestInput = z.infer<typeof simulateRequestSchema>;

/* ── Schema Registry ────────────────────────────────────────── */
/* Keyed by the URL/router slug (matches `RESOURCE_KINDS` in
 * `lib/constants.ts`). Used by `RESOURCE_DEFS` in the new
 * `/resources` page to pair forms with their validator. */

export const RESOURCE_SCHEMAS = {
  vnets: vnetSchema,
  enis: eniSchema,
  "acl-policies": aclPolicySchema,
  "route-policies": routePolicySchema,
  "vnet-mappings": vnetMappingSchema,
  "service-tunnels": serviceTunnelSchema,
  // Keep aligned with `RESOURCE_KINDS` in `lib/constants.ts` and
  // the URL slug dashd actually routes (`/v1/{ns}/ha-sets/...`).
  "ha-sets": haSetSchema,
} as const;

export type ResourceSchemaKey = keyof typeof RESOURCE_SCHEMAS;

/** Returns the zod schema for a given resource kind, or undefined
 *  if the kind is unrecognised. */
export function schemaForKind(kind: string) {
  return RESOURCE_SCHEMAS[kind as ResourceSchemaKey];
}