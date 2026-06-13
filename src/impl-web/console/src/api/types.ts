/* ═══════════════════════════════════════════════════════════════
 * dashw TypeScript types — mirrors dashd proto + BFF aggregation
 * ═══════════════════════════════════════════════════════════════ */

import type { DpuState } from '@/lib/constants';

/* ── Common ────────────────────────────────────────────────── */

export interface ObjectMeta {
  namespace: string;
  name: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  generation?: number;
  uid?: string;
}

export interface TargetRef {
  kind: string;
  namespace: string;
  name: string;
}

export interface ListResponse<T> {
  items: T[];
}

/* ── Vnet ──────────────────────────────────────────────────── */

export interface VnetSpec {
  metadata: ObjectMeta;
  vni: number;
  address_space: string[];
  gw_mac?: string;
  state?: string;
}

/* ── ENI ───────────────────────────────────────────────────── */

export interface EniSpec {
  metadata: ObjectMeta;
  vnet_name: string;
  mac_address: string;
  underlay_ip: string;
  admin_state?: string;
  /** dashd uses `placement_hint_dpu_ids: []`. */
  placement_hint_dpu_ids?: string[];
  resimulate_flows?: boolean;
  labels?: Record<string, string>;
  /** Legacy fields. */
  dpu_id?: string;
  primary_ip?: string;
  secondary_ips?: string[];
}

/* ── ACL Policy ────────────────────────────────────────────── */

export interface AclRule {
  priority: number;
  /** dashd uses lowercase action strings: `allow`, `deny`, `allow_and_continue`. */
  action: string;
  description?: string;
  src_prefixes?: string[];
  dst_prefixes?: string[];
  /** dashd returns port strings like `"443"` or `"7777-7800"`. */
  src_ports?: string[];
  dst_ports?: string[];
  /** Protocol names (`tcp`, `udp`, `icmp`) or numeric strings (`"6"`, `"17"`). */
  protocols?: string[];
  /** Legacy fields retained for compatibility. */
  direction?: "IN" | "OUT";
  protocol?: number;
  src_port_range?: PortRange;
  dst_port_range?: PortRange;
  terminating?: boolean;
}

export interface PortRange {
  start: number;
  end: number;
}

export interface AclPolicySpec {
  metadata: ObjectMeta;
  /** dashd uses `stage: "inbound" | "outbound"`. */
  stage?: "inbound" | "outbound";
  eni_names: string[];
  rules: AclRule[];
  labels?: Record<string, string>;
  /** Legacy: not present in current API. */
  default_action?: "ALLOW" | "DENY";
}

/* ── Route Policy ──────────────────────────────────────────── */

export interface RouteEcmpMember {
  next_hop_type: string;
  next_hop_target?: string;
  weight?: number;
}

export interface RouteEntry {
  prefix: string;
  /** `vnet`, `service_tunnel`, `drop`, etc. */
  next_hop_type?: string;
  /** Name of the target vnet/tunnel. */
  next_hop_target?: string;
  metric?: number;
  /** When set, the route fans out across these next-hops. */
  ecmp_members?: RouteEcmpMember[];
}

/** Legacy alias retained for compatibility. */
export type RoutePolicyRule = RouteEntry;

export interface RoutePolicySpec {
  metadata: ObjectMeta;
  eni_names: string[];
  /** dashd uses `routes` (not `rules`). */
  routes?: RouteEntry[];
  labels?: Record<string, string>;
  /** Legacy fields. */
  direction?: "IN" | "OUT";
  rules?: RouteEntry[];
}

/* ── Vnet Mapping ──────────────────────────────────────────── */

export interface VnetMappingSpec {
  metadata: ObjectMeta;
  vnet_name: string;
  /** dashd uses `ip_address`; legacy alias `overlay_ip` retained. */
  ip_address?: string;
  overlay_ip?: string;
  underlay_ip: string;
  mac_address: string;
  /** `vnet_encap`, `service_tunnel`, etc. */
  action?: string;
  /** `params.tunnel` when action == `service_tunnel`. */
  params?: Record<string, string>;
  labels?: Record<string, string>;
}

/* ── Service Tunnel ────────────────────────────────────────── */

export interface ServiceTunnelSpec {
  metadata: ObjectMeta;
  /** dashd shape. */
  local_underlay_ip?: string;
  remote_underlay_ip?: string;
  vni?: number;
  /** `action`, `mtu`, `nat_pool`, etc. */
  params?: Record<string, string>;
  labels?: Record<string, string>;
  /** Legacy fields retained for compatibility. */
  source_vnet?: string;
  destination_vnet?: string;
  tunnel_type?: string;
  encap_type?: string;
  bidirectional?: boolean;
}

/* ── HA Set ─────────────────────────────────────────────────── */

export interface HaSetSpec {
  metadata: ObjectMeta;
  scope: string;
  members: HaSetMember[];
  virtual_ip?: string;
}

export interface HaSetMember {
  dpu_id: string;
  role: string;
}

/* ── Inventory ─────────────────────────────────────────────── */

export interface DpuIdentity {
  dpu_id: string;
  appliance_id?: string;
  slot?: number;
  serial_number?: string;
}

export interface DpuCapabilities {
  ipv6?: boolean;
  service_tunnel?: boolean;
  ecmp?: boolean;
  fast_path?: boolean;
  fast_path_icmp_redirection?: boolean;
  trusted_vni?: boolean;
  ha_active_active?: boolean;
  ha_active_standby?: boolean;
  flow_sync?: boolean;
  gnmi_telemetry?: boolean;
  flow_resimulation?: boolean;
  eni_live_migration?: boolean;
  dash_api_schema_version?: string;
}

export interface DpuCapacityLimits {
  max_enis?: number;
  max_routes_per_eni?: number;
  max_acl_rules_per_eni?: number;
  max_vnet_mappings?: number;
  max_flows?: number;
  max_pps?: number;
  max_bps?: number;
}

export interface DpuRecord {
  identity: DpuIdentity;
  capabilities?: DpuCapabilities;
  capacity_limits?: DpuCapacityLimits;
  zone?: string;
  tier?: string;
  labels?: Record<string, string>;
}

/* ── Admin API types ───────────────────────────────────────── */

export interface DashdHealthResponse {
  status: string;
  leader: boolean;
  leader_id?: string;
  member_id?: string;
  cluster_size?: number;
  /** dashd /admin/health returns the dpus array (not a count). */
  dpus?: Array<{ id: string; state: string; last_seen?: string }>;
  /** Optional aggregate-style fields (BFF may add these). */
  connected_dpus?: number;
  uptime_seconds?: number;
  version?: string;
}

export interface LeaderInfo {
  leader_id: string;
  is_leader: boolean;
  member_id: string;
  cluster_size: number;
}

export interface DriftItem {
  target_ref: TargetRef;
  dpu_id: string;
  field: string;
  declared_value: string;
  observed_value: string;
}

export interface DpuHealthEntry {
  dpu_id: string;
  state: DpuState;
  last_heartbeat?: string;
  connected_at?: string;
  eni_count?: number;
  address?: string;
}

export interface EniPlacementSlot {
  dpu_id: string;
  observed?: boolean;
}

export interface EniPlacement {
  /** Actual API uses `name`; legacy alias `eni_name` retained. */
  name?: string;
  eni_name?: string;
  vnet_name?: string;
  mac_address?: string;
  underlay_ip?: string;
  admin_state?: string;
  /** Modern field: an ENI may be present on multiple DPUs (HA). */
  placements?: EniPlacementSlot[];
  /** Legacy single-DPU placement. */
  dpu_id?: string;
}

/* ── BFF Aggregation types ─────────────────────────────────── */

export interface FleetSummary {
  /* BFF aggregation field names */
  timestamp?: string;
  cluster_healthy?: boolean;
  leader_node?: string;
  is_leader?: boolean;
  dpu_count?: number;
  eni_count?: number;
  vnet_count?: number;
  dpus_by_state?: Record<string, number>;
  dpus?: Array<{ id: string; state: string; last_seen?: string }>;
  /* Legacy/alias field names (for compatibility) */
  total_dpus?: number;
  healthy_dpus?: number;
  degraded_dpus?: number;
  disconnected_dpus?: number;
  total_enis?: number;
  total_vnets?: number;
  total_acl_policies?: number;
  total_route_policies?: number;
  total_service_tunnels?: number;
  total_ha_sets?: number;
  dpu_states?: Record<string, number>;
  drift_count?: number;
  acl_policy_count?: number;
  route_policy_count?: number;
  service_tunnel_count?: number;
  ha_set_count?: number;
}

export interface DpuDetail {
  dpu_id: string;
  state: DpuState;
  health: DpuHealthEntry;
  enis: EniSpec[];
  policies: {
    acl: AclPolicySpec[];
    route: RoutePolicySpec[];
  };
  capacity: {
    eni_count: number;
    eni_max: number;
    route_count: number;
    route_max: number;
    acl_rule_count: number;
    acl_rule_max: number;
    flow_count: number;
    flow_max: number;
  };
  drift_items: DriftItem[];
  counters?: CounterReport;
}

export interface TopologyNode {
  id: string;
  type: 'dpu' | 'vnet' | 'eni';
  label: string;
  state?: DpuState;
  metadata?: Record<string, unknown>;
}

export interface TopologyEdge {
  source: string;
  target: string;
  label?: string;
}

export interface TopologyGraph {
  nodes: TopologyNode[];
  edges: TopologyEdge[];
}

export interface VnetDetail {
  spec: VnetSpec;
  enis: EniSpec[];
  mappings: VnetMappingSpec[];
  routes: RoutePolicySpec[];
  tunnels: ServiceTunnelSpec[];
  eni_count: number;
  mapping_count: number;
}

export interface VnetCanvasData {
  vnet: VnetSpec;
  overlay_nodes: CanvasNode[];
  underlay_nodes: CanvasNode[];
  edges: CanvasEdge[];
  tunnels: TunnelInfo[];
}

export interface CanvasNode {
  id: string;
  type: 'dpu' | 'eni';
  label: string;
  x: number;
  y: number;
  state?: DpuState;
  metadata?: Record<string, unknown>;
}

export interface CanvasEdge {
  id: string;
  source: string;
  target: string;
  type: 'eni-connector' | 'tunnel' | 'layer-divider';
  animated?: boolean;
  label?: string;
}

export interface TunnelInfo {
  id: string;
  source_vnet: string;
  destination_vnet: string;
  tunnel_type: string;
  overlay_port: string;
  underlay_port: string;
}

export interface CapacityStats {
  timestamp?: string;
  dpus?: DpuCapacityEntry[];
  per_dpu?: DpuCapacityEntry[];
  fleet?: {
    total_dpus?: number;
    total_enis?: number;
    max_enis?: number;
    total_routes?: number;
    max_routes?: number;
    total_acl_rules?: number;
    max_acl_rules?: number;
    total_flows?: number;
    max_flows?: number;
  };
  fleet_totals?: {
    total_enis: number;
    max_enis: number;
    total_routes: number;
    max_routes: number;
    total_acl_rules: number;
    max_acl_rules: number;
    total_flows: number;
    max_flows: number;
  };
}

export interface DpuCapacityEntry {
  /** Actual API uses `id`; legacy alias `dpu_id` retained for compatibility. */
  id?: string;
  dpu_id?: string;
  state: DpuState | string;
  /** Actual API fields (snake_case used/max/pct). */
  enis_used?: number;
  enis_max?: number;
  enis_pct?: number;
  routes_used?: number;
  routes_max?: number;
  routes_pct?: number;
  acl_rules_used?: number;
  acl_rules_max?: number;
  acl_rules_pct?: number;
  flows_used?: number;
  flows_max?: number;
  flows_pct?: number;
  /** Legacy field names (BFF aggregation may use these). */
  eni_count?: number;
  eni_max?: number;
  route_count?: number;
  route_max?: number;
  acl_rule_count?: number;
  acl_rule_max?: number;
  flow_count?: number;
  flow_max?: number;
  pps_current?: number;
  pps_max?: number;
  bps_current?: number;
  bps_max?: number;
}

/* ── Service Topology (BFF aggregated) ─────────────────────── */

export interface ServiceTopologyResponse {
  timestamp: string;
  cluster: ClusterInfo;
  appliances: ApplianceTopInfo[];
  zones: ZoneTopInfo[];
  summary: TopologySummary;
}

export interface ClusterInfo {
  healthy: boolean;
  leader_id: string;
  node_count: number;
  nodes: ClusterNodeInfo[];
}

export interface ClusterNodeInfo {
  addr: string;
  node_id: string;
  status: string;
  is_leader: boolean;
  leader_id: string;
  dpu_count: number;
  latency_ms: string;
}

export interface ApplianceTopInfo {
  id: string;
  zone?: string;
  tier?: string;
  dpus: DpuTopInfo[];
}

export interface DpuTopInfo {
  id: string;
  slot: number;
  state: string;
  last_seen?: string;
  eni_count: number;
  enis?: EniTopInfo[];
}

export interface EniTopInfo {
  name: string;
  vnet_name?: string;
  mac_address?: string;
  admin_state?: string;
}

export interface ZoneTopInfo {
  zone: string;
  appliance_count: number;
  dpu_count: number;
  eni_count: number;
}

export interface TopologySummary {
  total_nodes: number;
  total_appliances: number;
  total_dpus: number;
  total_enis: number;
  healthy_dpus: number;
  degraded_dpus: number;
  offline_dpus: number;
}

/* ── Counter Report (Phase B — stubbed types for Phase A) ── */

export interface CounterReport {
  dpu_id: string;
  timestamp: string;
  tcp_syn_rx?: number;
  tcp_syn_tx?: number;
  tcp_fin_rx?: number;
  tcp_retransmits?: number;
  udp_rx?: number;
  udp_tx?: number;
  drop_acl_in?: number;
  drop_acl_out?: number;
  drop_route_miss?: number;
  drop_no_eni?: number;
  drop_no_vnet?: number;
  drop_ttl?: number;
  drop_malformed?: number;
  slow_path_packets?: number;
  flow_created?: number;
  flow_deleted?: number;
  flow_active?: number;
  flow_aged_out?: number;
  ha_sync_sent?: number;
  ha_sync_received?: number;
  ha_sync_failed?: number;
  ha_split_brain_detected?: number;
  encap_vxlan?: number;
  encap_nvgre?: number;
  service_tunnel_tx?: number;
  service_tunnel_rx?: number;
}

/* ── Simulate Flow (legacy) ────────────────────────────────── */

export interface SimulateRequest {
  vnet_name: string;
  src_ip: string;
  dst_ip: string;
  protocol: number;
  src_port?: number;
  dst_port?: number;
  direction: 'IN' | 'OUT';
  eni_name?: string;
}

export interface SimulateResult {
  verdict: 'ALLOW' | 'DENY' | 'DROP';
  matched_rules: MatchedRule[];
  stages: SimulateStage[];
}

export interface MatchedRule {
  policy_name: string;
  rule_priority: number;
  action: string;
}

export interface SimulateStage {
  name: string;
  result: 'PASS' | 'DENY' | 'SKIP';
  matched_rule?: MatchedRule;
  detail?: string;
}

/* ── PE-1 Diagnostics API ──────────────────────────────────── */

/** POST /v1/diagnostics/trace-flow */
export interface TraceFlowRequest {
  flow: TraceFlowInput;
}

export interface TraceFlowInput {
  direction: number; // 1=INBOUND, 2=OUTBOUND
  eni_name: string;
  src_ip: string;
  dst_ip: string;
  src_port?: number;
  dst_port?: number;
  protocol: string; // "tcp", "udp", "icmp"
  vni?: string;
}

export interface TraceFlowResponse {
  verdict: number; // 3=ENCAP, 5=DROP_NO_MAPPING, 6=DROP_ACL, etc.
  trace: string[];
  matched_acl_rule?: TraceMatchedAcl;
  matched_route?: TraceMatchedRoute;
  matched_vnet_mapping?: TraceMatchedMapping;
}

export interface TraceMatchedAcl {
  policy_name: string;
  priority: number;
  action: string;
}

export interface TraceMatchedRoute {
  policy_name: string;
  prefix: string;
  next_hop_type: string;
  next_hop_target: string;
}

export interface TraceMatchedMapping {
  vnet_name: string;
  ip_address: string;
  action: string;
}

/** Verdict enum helpers */
export const TRACE_VERDICTS: Record<number, { label: string; color: string }> = {
  1: { label: 'ALLOW', color: 'green' },
  3: { label: 'ENCAP', color: 'green' },
  4: { label: 'DROP_NO_ROUTE', color: 'amber' },
  5: { label: 'DROP_NO_MAPPING', color: 'amber' },
  6: { label: 'DROP_ACL', color: 'red' },
  7: { label: 'DROP_ADMIN_DOWN', color: 'red' },
};

/** POST /v1/diagnostics/explain-match */
export interface ExplainMatchRequest {
  subject: number; // 1=ACL, 2=ROUTE, 3=VNET_MAPPING
  flow: TraceFlowInput;
}

export const EXPLAIN_SUBJECTS = {
  ACL: 1,
  ROUTE: 2,
  VNET_MAPPING: 3,
} as const;

export interface ExplainMatchResponse {
  candidates: ExplainCandidate[];
  selected_candidate_id: string;
}

export interface ExplainCandidate {
  candidate_id: string;
  matched?: boolean;
  reason: string;
  priority?: number;
}

/** POST /v1/diagnostics/acl-hit-stats */
export interface AclHitStatsRequest {
  zero_hits_only?: boolean;
  dpu_id?: string;
  namespace?: string;
}

export interface AclHitStatsResponse {
  items: AclHitStatsItem[];
}

export interface AclHitStatsItem {
  dpu_id: string;
  namespace: string;
  policy_name: string;
  stage: string;
  rules: AclHitStatsRule[];
  sampled_at?: Record<string, unknown>;
}

export interface AclHitStatsRule {
  priority: number;
  action: string;
  hits?: number;
}

/** POST /v1/diagnostics/explain-drift */
export interface ExplainDriftRequest {
  name_ref: { kind: string; namespace: string; name: string };
  dpu_id: string;
}

export interface ExplainDriftResponse {
  remediation: string;
  suggested_action?: string;
  details?: string;
}

/** POST /v1/diagnostics/trigger-resimulation */
export interface TriggerResimRequest {
  dpu_ids?: string[];
  eni_names?: string[];
}

export interface TriggerResimResponse {
  resimulated_count: number;
  errors?: string[];
}

/* ── Audit ─────────────────────────────────────────────────── */

export interface AuditEntry {
  id: string;
  timestamp: string;
  action: string;
  resource_kind: string;
  resource_name: string;
  namespace: string;
  txn_id?: string;
  operator_id?: string;
  detail?: string;
  result?: string;
}

/* ── WebSocket Frame (Phase B) ─────────────────────────────── */

export interface WSFrame<T = unknown> {
  type: string;
  seq: number;
  timestamp: string;
  payload: T;
}

export interface WSError {
  code: number;
  message: string;
}

/* ── Reconcile ─────────────────────────────────────────────── */

export interface ReconcileRequest {
  dpu_ids?: string[];
  namespace?: string;
  kind?: string;
}

export interface ReconcileResult {
  reconciled_count: number;
  errors: string[];
  details?: string;
}

/* ═══════════════════════════════════════════════════════════════
 * EniDetail — pre-joined view from /api/console/eni/{ns}/{name}/detail
 * (mirrors aggregation.EniDetail in src/impl-go/console)
 * ═══════════════════════════════════════════════════════════════ */

export interface EniDetail {
  namespace: string;
  name: string;
  identity: EniDetailIdentity;
  /** Parent Vnet projection; omitted when the vnet fetch failed. */
  vnet?: EniDetailVnetSummary;
  placement: EniDetailPlacement;
  /** HaSet that contains at least one placement DPU; null when none. */
  ha_set?: EniDetailHaSetSummary | null;

  vnet_mappings_reachable: VnetMappingSpec[];
  acls_inbound: AclPolicySpec[];
  acls_outbound: AclPolicySpec[];
  route_policies: RoutePolicySpec[];
  service_tunnels: ServiceTunnelSpec[];

  counters: EniDetailCounters;
  /** Best-effort warnings for partial failures (e.g. "vnet fetch failed"). */
  warnings?: string[];
}

export interface EniDetailIdentity {
  vnet_name: string;
  mac_address?: string;
  underlay_ip?: string;
  admin_state?: string;
  generation?: number;
  labels?: Record<string, string>;
}

export interface EniDetailVnetSummary {
  name: string;
  vni: number;
  gw_mac?: string;
  state?: string;
}

export interface EniDetailPlacement {
  dpu_ids: string[];
  /** True iff dpu_ids.length > 1 (ENI present on multiple DPUs via HA). */
  ha_active_active: boolean;
  slots?: EniDetailPlacementSlot[];
}

export interface EniDetailPlacementSlot {
  dpu_id: string;
  observed: boolean;
}

export interface EniDetailHaSetSummary {
  name: string;
  scope?: string;
  virtual_ip?: string;
  member_dpu_ids: string[];
  members_by_role?: Record<string, string>;
}

export interface EniDetailCounters {
  acl_inbound: number;
  acl_outbound: number;
  routes: number;
  mappings: number;
  tunnels: number;
  placements: number;
  /** Reserved for Phase B per-rule counters. */
  rule_hits?: number;
}

/* ── dashctl Command Registry types ────────────────────────── */

export interface CommandDef {
  verb: string;
  kind: string;
  description: string;
  category: string;
  flags: CommandFlag[];
  examples: string[];
  apiMethod: string;
  apiPath: string;
}

export interface CommandFlag {
  name: string;
  short?: string;
  type: 'string' | 'number' | 'boolean';
  required?: boolean;
  description: string;
  default?: string;
}