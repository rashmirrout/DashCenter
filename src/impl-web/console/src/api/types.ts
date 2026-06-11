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
  dpu_id?: string;
  primary_ip?: string;
  secondary_ips?: string[];
}

/* ── ACL Policy ────────────────────────────────────────────── */

export interface AclRule {
  priority: number;
  action: 'ALLOW' | 'DENY';
  direction: 'IN' | 'OUT';
  protocol?: number;
  src_prefixes?: string[];
  dst_prefixes?: string[];
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
  eni_names: string[];
  default_action: 'ALLOW' | 'DENY';
  rules: AclRule[];
}

/* ── Route Policy ──────────────────────────────────────────── */

export interface RoutePolicyRule {
  priority: number;
  action: 'PERMIT' | 'DENY';
  prefixes?: string[];
  community?: string[];
  as_path_regex?: string;
}

export interface RoutePolicySpec {
  metadata: ObjectMeta;
  eni_names: string[];
  direction: 'IN' | 'OUT';
  rules: RoutePolicyRule[];
}

/* ── Vnet Mapping ──────────────────────────────────────────── */

export interface VnetMappingSpec {
  metadata: ObjectMeta;
  vnet_name: string;
  overlay_ip: string;
  underlay_ip: string;
  mac_address: string;
  action?: string;
}

/* ── Service Tunnel ────────────────────────────────────────── */

export interface ServiceTunnelSpec {
  metadata: ObjectMeta;
  source_vnet: string;
  destination_vnet: string;
  tunnel_type: string;
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
  connected_dpus: number;
  uptime_seconds: number;
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

export interface EniPlacement {
  eni_name: string;
  vnet_name: string;
  dpu_id: string;
  underlay_ip: string;
}

/* ── BFF Aggregation types ─────────────────────────────────── */

export interface FleetSummary {
  total_dpus: number;
  healthy_dpus: number;
  degraded_dpus: number;
  disconnected_dpus: number;
  total_enis: number;
  total_vnets: number;
  total_acl_policies: number;
  total_route_policies: number;
  total_service_tunnels: number;
  total_ha_sets: number;
  dpu_states: Record<string, number>;
  drift_count: number;
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
  dpus: DpuCapacityEntry[];
  fleet_totals: {
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
  dpu_id: string;
  state: DpuState;
  eni_count: number;
  eni_max: number;
  route_count: number;
  route_max: number;
  acl_rule_count: number;
  acl_rule_max: number;
  flow_count: number;
  flow_max: number;
  pps_current: number;
  pps_max: number;
  bps_current: number;
  bps_max: number;
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

/* ── Simulate Flow ─────────────────────────────────────────── */

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